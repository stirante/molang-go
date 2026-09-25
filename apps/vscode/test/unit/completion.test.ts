import { describe, expect, it } from 'vitest';
import type { AnalyzeOptions } from '../../src/server/bridge';
import { Catalogue } from '../../src/server/catalogue';
import { completionContext, completionItems, type KnownName } from '../../src/server/completion';
import { findCall } from '../../src/server/signature';

import { shippedCatalogue } from './shipped';

const catalogue = Catalogue.parse(shippedCatalogue());

/** Context at the | in s. */
function at(s: string) {
  const offset = s.indexOf('|');
  return completionContext(s.replace('|', ''), offset);
}

describe('completionContext', () => {
  it('offers namespaces and keywords at the start of a name', () => {
    expect(at('|')).toMatchObject({ kind: 'root', start: 0, end: 0, prefix: '' });
    expect(at('1 + qu|')).toMatchObject({ kind: 'root', start: 4, end: 6, prefix: 'qu' });
  });

  it('knows the namespace after every spelling of it', () => {
    for (const [src, ns] of [
      ['q.|', 'query'],
      ['query.is_|', 'query'],
      ['Query.|', 'query'],
      ['math.|', 'math'],
      ['v.|', 'variable'],
      ['variable.|', 'variable'],
      ['t.x = temp.|', 'temp'],
      ['c.|', 'context'],
      ['array.|', 'array'],
      ['texture.|', 'texture'],
    ] as const) {
      expect(at(src), src).toMatchObject({ kind: 'member', namespace: ns });
    }
  });

  it('replaces the whole name, including what follows the cursor', () => {
    expect(at('q.is_b|aby + 1')).toMatchObject({ kind: 'member', start: 2, end: 9, prefix: 'is_b' });
  });

  it('keeps the dots of a variable name in the prefix', () => {
    expect(at('v.st.hei|')).toMatchObject({ kind: 'member', namespace: 'variable', start: 2, prefix: 'st.hei' });
  });

  it('notices an arrow before the name', () => {
    expect(at('v.e -> |')).toMatchObject({ kind: 'root', afterArrow: true });
    expect(at('c.other->q.|')).toMatchObject({ kind: 'member', namespace: 'query', afterArrow: true });
    expect(at('v.a > |')).toMatchObject({ kind: 'root', afterArrow: false });
  });

  it('offers nothing in a string, a number or an unknown namespace', () => {
    expect(at("q.x == 'q.|")).toEqual({ kind: 'none' });
    expect(at('1.|')).toEqual({ kind: 'none' });
    expect(at('0.5|')).toEqual({ kind: 'none' });
    expect(at('foo.|')).toEqual({ kind: 'none' });
    // a. is not an alias the game has.
    expect(at('a.|')).toEqual({ kind: 'none' });
    // After a closed string it is Molang again.
    expect(at("'a' == q.|")).toMatchObject({ kind: 'member', namespace: 'query' });
  });
});

describe('completionItems', () => {
  const names: KnownName[] = [
    { namespace: 'variable', name: 'speed', reads: 2, writes: 1 },
    { namespace: 'temp', name: 'x', reads: 1, writes: 0 },
  ];
  const items = (src: string) => completionItems({ context: at(src), catalogue, names });

  it('lists queries with their signatures, calling only the ones that need arguments', () => {
    const list = items('q.|');
    const baby = list.find((i) => i.label === 'is_baby')!;
    expect(baby.insertText).toBe('is_baby');
    expect(baby.detail).toBe('query.is_baby: number');
    const any = list.find((i) => i.label === 'is_item_name_any')!;
    expect(any.insertText).toBe('is_item_name_any(${1:slot_name}, ${2:item_name})');
    expect(any.detail).toBe('query.is_item_name_any(slot_name: string, slot_index?: number, ...item_name: string): number');
  });

  it('always calls a math function, but never math.pi', () => {
    const list = items('math.|');
    expect(list.find((i) => i.label === 'clamp')!.insertText).toBe('clamp(${1:value}, ${2:min}, ${3:max})');
    expect(list.find((i) => i.label === 'pi')!.insertText).toBe('pi');
    expect(list.length).toBe(catalogue.math.size);
  });

  it('offers the context names the game supplies, saying where', () => {
    const first = items('c.|').find((i) => i.label === 'is_first_person')!;
    expect(first.labelDetails?.description).toBe('attachables');
  });

  it('documents namespaces and keywords', () => {
    const root = items('|');
    expect(root.find((i) => i.label === 'q')!.detail).toBe('short for query');
    expect(root.find((i) => i.label === 'q')!.labelDetails?.description).toMatch(/Read-only values/);
    expect(root.find((i) => i.label === 'loop')!.labelDetails?.description).toBe('Repeat a block a given number of times');
  });

  it("offers the document's own names in their namespace", () => {
    expect(items('v.|').map((i) => i.label)).toEqual(['speed']);
    expect(items('t.|').map((i) => i.label)).toEqual(['x']);
    // math.bitshift is a macro of molang-go's, not a game function.
    expect(items('math.|').map((i) => i.label)).not.toContain('bitshift');
    expect(items('v.|')[0].detail).toBe('variable.speed — written 1×, read 2× in this file');
  });

  it('offers only variables and queries after an arrow', () => {
    expect(
      items('v.e->|')
        .map((i) => i.label)
        .sort(),
    ).toEqual(['q', 'query', 'v', 'variable']);
    expect(items('v.e->t.|')).toEqual([]);
  });

  it('offers only the queries the field can name', () => {
    const cat = new Catalogue({
      queries: [
        { name: 'is_baby' },
        { name: 'noise', querySet: 'world_gen' },
        { name: 'any_tag', contexts: ['tags'] },
        { name: 'block_state' },
      ],
    });
    const list = (options?: AnalyzeOptions) =>
      completionItems({ context: at('q.|'), catalogue: cat, names: [], options }).map((i) => i.label);
    expect(list()).toEqual(['is_baby', 'noise', 'any_tag', 'block_state']);
    expect(list({ querySet: 'default' })).toEqual(['is_baby', 'block_state']);
    expect(list({ querySet: 'world_gen' })).toEqual(['noise']);
    expect(list({ querySet: 'tags' })).toEqual(['any_tag']);
    expect(list({ allowedQueries: ['query.block_state'] })).toEqual(['block_state']);
  });
});

describe('findCall', () => {
  const call = (s: string) => findCall(s.replace('|', ''), s.indexOf('|'));

  it('finds the innermost call and the argument the cursor is in', () => {
    expect(call('math.clamp(|')).toMatchObject({ namespace: 'math', name: 'clamp', argument: 0 });
    expect(call('math.clamp(v.x, |')).toMatchObject({ argument: 1 });
    expect(call('math.clamp(math.sin(1), 0, |')).toMatchObject({ name: 'clamp', argument: 2 });
    expect(call('math.clamp(math.sin(|), 0, 1)')).toMatchObject({ name: 'sin', argument: 0 });
    expect(call("q.is_item_name_any('a,b', |")).toMatchObject({ namespace: 'query', argument: 1 });
  });

  it('is nowhere outside a call, or directly inside a bracket that is not one', () => {
    expect(call('math.clamp(1, 2, 3) + |')).toBeUndefined();
    expect(call('(1 + |')).toBeUndefined();
    expect(call('array.a[|')).toBeUndefined();
    expect(call('loop(3, {|')).toBeUndefined();
    expect(call('math.abs(array.a[|')).toBeUndefined();
  });
});
