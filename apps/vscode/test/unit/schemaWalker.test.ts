import { existsSync, readdirSync, readFileSync } from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { pathToFileURL } from 'node:url';
import { describe, expect, it } from 'vitest';
import {
  externalRefs,
  findMolangPaths,
  hitsToFileTypes,
  refMarker,
  SchemaSet,
  schemaKey,
} from '../../src/client/schemaWalker';

/** The Molang paths of a one-document schema, as "a/b/c accepts" strings. */
function paths(schema: unknown, extra: Record<string, unknown> = {}): string[] {
  const set = new SchemaSet();
  const base = 'file:///schemas/root.json';
  set.add(base, schema);
  for (const [uri, doc] of Object.entries(extra)) set.add(uri, doc);
  return findMolangPaths(schema, base, set.resolve)
    .map((h) => `${h.path.join('/')} ${[...h.accepts].sort().join(',')}`)
    .sort();
}

describe('the schema walker', () => {
  it('recognises every marker', () => {
    const got = paths({
      type: 'object',
      properties: {
        a: { type: 'string', format: 'molang' },
        b: { title: 'Molang Number', anyOf: [{ type: 'string' }, { type: 'number' }] },
        c: { title: 'Molang Boolean', anyOf: [{ type: 'string' }, { type: 'boolean' }] },
        d: { title: 'Molang expression' },
        e: { $ref: '../common/legacy/Expression%20Node.json' },
        f: { $ref: 'Molang string.json#' },
        g: { $ref: '../molang/embedded.json' },
        h: { type: 'object', properties: { expression: { type: 'string' }, version: { type: 'integer' } } },
        plain: { type: 'string', title: 'Name', description: 'Not Molang.' },
        molangish: { type: 'string', title: 'Uses molang elsewhere' },
      },
    });
    expect(got).toEqual([
      'a string',
      'b number,string',
      'c boolean,string',
      'd string',
      'e number,object,string',
      'f object,string',
      'g boolean,number,string',
      'h object',
    ]);
  });

  it('follows local references, definitions and $defs, and the combinators', () => {
    const got = paths({
      definitions: { molang: { type: 'string', format: 'molang' }, list: { type: 'array', items: { $ref: '#/definitions/molang' } } },
      $defs: { cond: { title: 'Molang Boolean' } },
      properties: {
        list: { $ref: '#/definitions/list' },
        tuple: { items: [{ type: 'string' }, { $ref: '#/definitions/molang' }] },
        map: { patternProperties: { '^v\\.': { $ref: '#/definitions/molang' } } },
        more: { additionalProperties: { $ref: '#/$defs/cond' } },
        either: { anyOf: [{ type: 'number' }, { properties: { x: { $ref: '#/definitions/molang' } } }] },
        one: { oneOf: [{ properties: { y: { $ref: '#/definitions/molang' } } }] },
        all: { allOf: [{ properties: { z: { $ref: '#/definitions/molang' } } }] },
        cond: { if: { properties: { k: { const: 1 } } }, then: { properties: { t: { $ref: '#/definitions/molang' } } } },
        'a/b': { $ref: '#/definitions/molang' },
      },
    });
    expect(got).toEqual([
      'all/z string',
      'a~1b string',
      'cond/t string',
      'either/x string',
      'list/[*] string',
      'map/* string',
      'more/* boolean,string',
      'one/y string',
      'tuple/[1] string',
    ]);
  });

  it('terminates on reference cycles and still reports what the cycle reaches', () => {
    // A filter that nests itself, and two definitions that refer to each
    // other: the shape of entity filters and event sequences.
    const schema = {
      definitions: {
        filter: {
          properties: {
            test: { type: 'string' },
            value: { format: 'molang' },
            all_of: { type: 'array', items: { $ref: '#/definitions/filter' } },
          },
        },
        a: { properties: { next: { $ref: '#/definitions/b' }, m: { format: 'molang' } } },
        b: { properties: { back: { $ref: '#/definitions/a' } } },
        self: { $ref: '#/definitions/self' },
      },
      properties: {
        filters: { $ref: '#/definitions/filter' },
        chain: { $ref: '#/definitions/a' },
        loop: { $ref: '#/definitions/self' },
      },
    };
    expect(paths(schema)).toEqual(['chain/m string', 'filters/value string']);
  });

  it('merges the types of branches that reach one place', () => {
    const got = paths({
      properties: { x: { anyOf: [{ type: 'string', format: 'molang' }, { properties: { expression: {}, version: {} } }] } },
    });
    expect(got).toEqual(['x object,string']);
  });

  it('follows references into other documents, by URI and by $id', () => {
    const other = { definitions: { m: { type: 'string', format: 'molang' } } };
    const byId = { $id: 'https://example.com/molang.json', title: 'Molang' };
    const got = paths(
      { properties: { x: { $ref: 'common/other.json#/definitions/m' }, y: { $ref: 'https://example.com/molang.json' } } },
      { 'file:///schemas/common/other.json': other, 'file:///schemas/ids.json': { definitions: { z: byId } } },
    );
    expect(got).toEqual(['x string', 'y string']);
  });

  it('lists the other documents a schema refers to, but not the marker files', () => {
    const doc = { a: { $ref: 'x.json#/a' }, b: { $ref: '#/local' }, c: [{ $ref: '../y%20z.json' }], d: { $ref: 'Expression Node.json' } };
    expect(externalRefs(doc, 'file:///s/sub/root.json').sort()).toEqual(['file:///s/sub/x.json', 'file:///s/y%20z.json']);
    expect(refMarker('../../common/legacy/Molang%20string.json')).toEqual(['string', 'object']);
  });

  it('keys documents the same however their URI is spelled', () => {
    expect(schemaKey('file:///H%3A/a%20b/c.json#/x')).toBe(schemaKey('file:///h:/a b/c.json'));
  });

  it('groups paths by the root key', () => {
    const fts = hitsToFileTypes([
      { path: ['minecraft:entity', 'events', '*', 'x'], accepts: ['string'] },
      { path: ['a~1b', 'y'], accepts: ['string', 'object'] },
      { path: ['*', 'z'], accepts: ['string'] },
      { path: ['format_version'], accepts: ['string'] },
      { path: ['[*]', 'q'], accepts: ['string'] },
    ]);
    expect(fts).toEqual([
      { rootKey: 'minecraft:entity', paths: [{ path: 'events/*/x', kind: 'general', accepts: ['string'] }] },
      { rootKey: 'a/b', paths: [{ path: 'y', kind: 'general', accepts: ['string', 'object'] }] },
      { rootKey: '*', paths: [{ path: 'z', kind: 'general', accepts: ['string'] }] },
    ]);
  });
});

// The real thing, when the Blockception extension is installed here: its
// entity schema is the largest and most recursive of the set.
function blockceptionEntitySchema(): string | undefined {
  const dir = path.join(os.homedir(), '.vscode', 'extensions');
  if (!existsSync(dir)) return undefined;
  const ext = readdirSync(dir)
    .filter((d) => d.startsWith('blockceptionltd.'))
    .sort()
    .reverse()
    .map((d) => path.join(dir, d, 'minecraft-bedrock-schemas', 'behavior', 'entities', 'entities.json'))
    .find((f) => existsSync(f));
  return ext;
}

const bcEntities = blockceptionEntitySchema();

describe.runIf(bcEntities)('the Blockception entity schema', () => {
  it('yields the entity Molang paths, quickly', () => {
    const file = bcEntities!;
    const uri = pathToFileURL(file).href;
    const doc = JSON.parse(readFileSync(file, 'utf8'));
    const set = new SchemaSet();
    set.add(uri, doc);
    const t0 = performance.now();
    const fts = hitsToFileTypes(findMolangPaths(doc, uri, set.resolve));
    const ms = performance.now() - t0;
    const entity = fts.find((f) => f.rootKey === 'minecraft:entity');
    expect(entity, JSON.stringify(fts.map((f) => f.rootKey))).toBeDefined();
    const got = new Set(entity!.paths.map((p) => p.path));
    // Fields the schema has long marked, one through a title marker and
    // one through a reference shared by many components.
    for (const p of ['description/properties/*/default', 'component_groups/*/minecraft:ageable/drop_items/[*]/tags']) {
      expect(got.has(p), p).toBe(true);
    }
    expect(got.size).toBeGreaterThan(150);
    expect(ms).toBeLessThan(2000);
    console.log(`Blockception entities.json: ${got.size} Molang paths in ${Math.round(ms)} ms`);
  });
});
