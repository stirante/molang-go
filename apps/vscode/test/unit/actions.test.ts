import { describe, expect, it } from 'vitest';
import { editDistance, suggestNamespace } from '../../src/server/actions';
import { argumentStarts } from '../../src/server/inlay';
import { fileFindings } from '../../src/server/molangFile';

describe('edit distance', () => {
  it('counts an adjacent swap as one edit', () => {
    expect(editDistance('is_baby', 'is_bayb')).toBe(1);
    expect(editDistance('kitten', 'sitting')).toBe(3);
    expect(editDistance('', 'abc')).toBe(3);
  });
  it('gives up past the limit', () => {
    expect(editDistance('abcdef', 'uvwxyz', 2)).toBe(3);
  });
});

describe('namespace spellings', () => {
  it('knows the short forms the language lacks', () => {
    expect(suggestNamespace('a')).toBe('array');
    expect(suggestNamespace('m')).toBe('math');
    expect(suggestNamespace('M')).toBe('math');
  });
  it('corrects near misses of a full name, and guesses at nothing else', () => {
    expect(suggestNamespace('mth')).toBe('math');
    expect(suggestNamespace('varible')).toBe('variable');
    expect(suggestNamespace('qeury')).toBe('query');
    expect(suggestNamespace('x')).toBeUndefined();
    expect(suggestNamespace('foo')).toBeUndefined();
  });
});

describe('call arguments', () => {
  it('finds where each argument starts, past brackets and strings', () => {
    const text = "math.clamp( v.x , math.min(1, 2), 'a,b')";
    const starts = argumentStarts(text, text.indexOf('('));
    expect(starts.map((s) => s.arg)).toEqual(['v.x', 'math.min(1, 2)', "'a,b'"]);
    expect(starts[0].start).toBe(text.indexOf('v.x'));
  });
  it('reads an unclosed call to the end', () => {
    expect(argumentStarts('q.x(1, ', 3).map((s) => s.arg)).toEqual(['1']);
  });
});

describe('.molang file findings', () => {
  it('warns about a comment jsonte would not strip', () => {
    const text = '# fine\n#bad\nv.a = #{t};\nv.b = 1; #\n';
    const found = fileFindings(text).map((f) => [f.code, text.slice(f.start, f.start + 4)]);
    expect(found).toEqual([
      ['comment-space', '#bad'],
      ['comment-space', '#\n'],
    ]);
  });
  it('ignores a # in a string', () => {
    expect(fileFindings("v.a = '#x';")).toEqual([]);
  });
  it('reports a byte order mark only when the file on disk has one', () => {
    expect(fileFindings('v.a = 1;')).toEqual([]);
    expect(fileFindings('v.a = 1;', true).map((f) => f.code)).toEqual(['byte-order-mark']);
  });
});
