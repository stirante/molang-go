import { describe, expect, it } from 'vitest';
import { stripMolangFile } from '../../src/server/molangFile';

describe('stripMolangFile', () => {
  it('blanks # comments to the end of the line, keeping every offset', () => {
    const text = '# header\nv.a = 1; # set a\r\nreturn v.a;';
    const s = stripMolangFile(text);
    expect(s.code.length).toBe(text.length);
    expect(s.code).toBe('        \nv.a = 1;        \r\nreturn v.a;');
    expect(s.comments).toEqual([
      { start: 0, end: 8 },
      { start: 18, end: 25 },
    ]);
  });

  it('leaves # inside a string alone', () => {
    const text = "t.s = 'a#b'; # real";
    expect(stripMolangFile(text).code).toBe("t.s = 'a#b';       ");
  });

  it('does not carry an unclosed string past the line end', () => {
    const text = "t.s = 'open\n# still a comment";
    expect(stripMolangFile(text).comments).toEqual([{ start: 12, end: text.length }]);
  });

  it('reads #{...} as a template, with a stand-in that parses where it stands', () => {
    const text = 'v.x = #{value}; v.#{name} = 1; v.p#{suffix} = 2;';
    const s = stripMolangFile(text);
    expect(s.code.length).toBe(text.length);
    expect(s.comments).toEqual([]);
    expect(s.templates.map((t) => text.slice(t.start, t.end))).toEqual(['#{value}', '#{name}', '#{suffix}']);
    expect(s.code).toBe('v.x = 0       ; v._______ = 1; v.p_________ = 2;');
  });

  it('balances braces inside a template, and ignores them in its strings', () => {
    const text = "v.x = #{ f({a: '}'}) }; # c";
    const s = stripMolangFile(text);
    expect(s.templates).toEqual([{ start: 6, end: 22 }]);
    expect(s.comments).toEqual([{ start: 24, end: 27 }]);
  });
});
