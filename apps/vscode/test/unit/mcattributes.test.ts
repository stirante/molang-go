import { describe, expect, it } from 'vitest';
import {
  BLOCKCEPTION_MOLANG_CODES,
  mcattributesAdditions,
  mcattributesAppend,
  parseMcattributes,
} from '../../src/client/mcattributes';

describe('.mcattributes', () => {
  it('reads keys as Blockception does', () => {
    const m = parseMcattributes('a=b\r\n  c = d\n# e=f\ng=h # note\n=x\n');
    expect([...m]).toEqual([
      ['a', 'b'],
      ['  c ', ' d'],
      ['g', 'h'],
    ]);
  });

  it('adds every code to an empty file, and nothing the second time', () => {
    const lines = mcattributesAdditions('');
    expect(lines).toHaveLength(BLOCKCEPTION_MOLANG_CODES.length);
    expect(lines).toContain('diagnostic.disable.molang.error.string.unterminated=true');
    const after = mcattributesAppend('', lines);
    expect(mcattributesAdditions(after)).toEqual([]);
  });

  it('leaves a code the file already decides alone, and keeps the rest of the file', () => {
    const existing = 'diagnostic.disable.molang.function.deprecated=false\r\nfoo=bar';
    const lines = mcattributesAdditions(existing);
    expect(lines).not.toContain('diagnostic.disable.molang.function.deprecated=true');
    const text = mcattributesAppend(existing, lines);
    // A fresh line, in the file's own line ending.
    expect(text.startsWith('\r\ndiagnostic.disable.')).toBe(true);
    expect(text.endsWith('=true\r\n')).toBe(true);
    expect(text.replace(/\r\n/g, '').includes('\n')).toBe(false);
  });

  it('never writes a code its parser could not read back', () => {
    for (const c of BLOCKCEPTION_MOLANG_CODES) {
      expect(c).not.toMatch(/[=#]/);
      expect(parseMcattributes(`diagnostic.disable.${c}=true`).get(`diagnostic.disable.${c}`)).toBe('true');
    }
  });
});
