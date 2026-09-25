import { describe, expect, it } from 'vitest';
import { decodeJsonString, rawToValue, valueToRaw } from '../../src/server/jsonString';

// Raw JSON text is written with doubled backslashes throughout: '\\"' is the
// two characters \ and ", as they stand between a JSON string's quotes.

describe('decodeJsonString', () => {
  it('decodes every escape JSON has', () => {
    const raw = 'a\\"b\\\\c\\/d\\be\\ff\\ng\\rh\\ti\\u00e9j';
    expect(decodeJsonString(raw).value).toBe('a"b\\c/d\be\ff\ng\rh\tiéj');
  });

  it('maps each value unit to the escape that produced it', () => {
    const raw = "v.x = 'caf\\u00e9' + \\\"";
    const d = decodeJsonString(raw);
    expect(d.value).toBe(`v.x = 'café' + "`);
    const e = d.value.indexOf('é');
    expect(d.map[e]).toBe(raw.indexOf('\\u00e9'));
    // The unit after the escape starts after all six characters of it.
    expect(d.map[e + 1]).toBe(raw.indexOf('\\u00e9') + 6);
    expect(d.map[d.value.length]).toBe(raw.length);
  });

  it('keeps a surrogate pair as two units, each mapped to its own escape', () => {
    const raw = "'\\ud83d\\ude00' ;";
    const d = decodeJsonString(raw);
    expect(d.value).toBe("'😀' ;");
    expect(d.map.slice(0, 4)).toEqual([0, 1, 7, 13]);
  });

  it('passes a raw non-ASCII character through one unit to one unit', () => {
    const raw = "'😀é' ;";
    const d = decodeJsonString(raw);
    expect(d.value).toBe(raw);
    expect(d.map).toEqual([...Array(raw.length + 1).keys()]);
  });

  it('takes a broken escape literally rather than failing', () => {
    expect(decodeJsonString('a\\qb\\u12').value).toBe('a\\qb\\u12');
    expect(decodeJsonString('trailing\\').value).toBe('trailing\\');
  });

  it('maps ranges both ways', () => {
    const raw = 'q.a(\\"x\\") + ;';
    const d = decodeJsonString(raw);
    expect(d.value).toBe('q.a("x") + ;');
    const semi = d.value.indexOf(';');
    expect(valueToRaw(d, semi)).toBe(raw.indexOf(';'));
    expect(valueToRaw(d, semi + 1)).toBe(raw.length);
    expect(rawToValue(d, raw.indexOf(';'))).toBe(semi);
    // A cursor inside \" is on the quote it produces.
    expect(rawToValue(d, raw.indexOf('\\"') + 1)).toBe(d.value.indexOf('"'));
    expect(rawToValue(d, raw.length)).toBe(d.value.length);
  });
});
