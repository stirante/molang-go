// Reading a .molang file.
//
// Molang itself has no comments: a field in a pack is one expression and the
// game reads all of it. A .molang file is a tooling convention, and the tools
// that produce and consume one agree on `#` to the end of the line --
// Blockception's language configuration, bridge.'s tokenizer and jsonte's
// stripper all do. jsonte adds one more thing: `#{ expr }` is a template it
// expands before the Molang ever reaches the game, not a comment.
//
// So before a .molang file is analysed:
//
//   - a `#` not followed by `{`, outside a string, starts a comment that runs
//     to the end of the line. It is replaced by spaces.
//   - `#{ ... }` is a template. It is replaced by a stand-in of the same
//     length that parses where the template stands: underscores after a `.`
//     or a name (`v.#{name}` reads as a variable name), a 0 anywhere else
//     (`v.x = #{value};` reads as a number).
//
// Everything is replaced character for character, so every offset in the
// result is the same offset in the file and nothing has to be mapped back.

export interface Span {
  start: number;
  end: number;
}

export interface StrippedMolang {
  /** The file with comments and templates blanked, the same length. */
  code: string;
  comments: Span[];
  templates: Span[];
}

export function stripMolangFile(text: string): StrippedMolang {
  const out: string[] = [];
  const comments: Span[] = [];
  const templates: Span[] = [];
  let inString = false;
  let i = 0;
  while (i < text.length) {
    const c = text[i];
    if (c === '\n' || c === '\r') {
      // A string does not carry over a line end here. The language allows
      // it, but a file where it happens is far more often one with a quote
      // left open, and ending the string at the line keeps the comments
      // below it comments.
      inString = false;
      out.push(c);
      i++;
      continue;
    }
    if (inString) {
      if (c === "'") inString = false;
      out.push(c);
      i++;
      continue;
    }
    if (c === "'") {
      inString = true;
      out.push(c);
      i++;
      continue;
    }
    if (c !== '#') {
      out.push(c);
      i++;
      continue;
    }
    if (text[i + 1] === '{') {
      const end = templateEnd(text, i + 2);
      templates.push({ start: i, end });
      out.push(standIn(text, i, end));
      i = end;
      continue;
    }
    let end = i;
    while (end < text.length && text[end] !== '\n' && text[end] !== '\r') end++;
    comments.push({ start: i, end });
    out.push(' '.repeat(end - i));
    i = end;
  }
  return { code: out.join(''), comments, templates };
}

/** The offset just past the `}` closing a template whose body starts at from. */
function templateEnd(text: string, from: number): number {
  let depth = 1;
  let quote = '';
  for (let i = from; i < text.length; i++) {
    const c = text[i];
    if (quote) {
      if (c === quote) quote = '';
      continue;
    }
    if (c === "'" || c === '"') quote = c;
    else if (c === '{') depth++;
    else if (c === '}' && --depth === 0) return i + 1;
  }
  return text.length;
}

function standIn(text: string, start: number, end: number): string {
  const before = start > 0 ? text[start - 1] : '';
  const inName = before === '.' || /[A-Za-z0-9_]/.test(before);
  let s = '';
  for (let i = start; i < end; i++) {
    const c = text[i];
    // Line ends stay, so a template spanning lines does not move them.
    if (c === '\n' || c === '\r') s += c;
    else if (inName) s += '_';
    else s += s === '' ? '0' : ' ';
  }
  return s;
}

/** Whether offset falls inside one of spans. */
export function inSpans(spans: readonly Span[], offset: number): boolean {
  for (const s of spans) {
    if (offset >= s.start && offset < s.end) return true;
  }
  return false;
}

/** Whether [start, end) overlaps one of spans. */
export function overlapsSpans(spans: readonly Span[], start: number, end: number): boolean {
  for (const s of spans) {
    if (start < s.end && end > s.start) return true;
    if (start === end && start >= s.start && start < s.end) return true;
  }
  return false;
}

/** Something about a .molang file itself, beyond the Molang in it. */
export interface FileFinding extends Span {
  severity: 'error' | 'warning' | 'information' | 'hint';
  code: string;
  message: string;
}

export const COMMENT_SPACE = 'comment-space';
export const BYTE_ORDER_MARK = 'byte-order-mark';

/**
 * What goes wrong with a .molang file on its way to the game.
 *
 * jsonte, which is what turns .molang files into pack JSON, strips a
 * comment only when it starts with `# ` -- a hash and a space. Every tool
 * here reads `#note` as a comment too, but jsonte leaves it in, and the game
 * then refuses the expression. `#{` is a template, not a comment.
 *
 * A byte order mark is invisible in the editor, which takes it off when it
 * reads the file (withByteOrderMark says the file on disk has one) or shows
 * it as the first character. jsonte's loadText copies it into the JSON it
 * builds, where it is the first character of the expression, and the game
 * refuses that expression -- in the case this was learnt from, in every
 * entity that used it, badly enough to hang the client on world load.
 */
export function fileFindings(text: string, withByteOrderMark = false): FileFinding[] {
  const out: FileFinding[] = [];
  if (withByteOrderMark && !text.startsWith('\uFEFF')) {
    out.push({
      start: 0,
      end: 0,
      severity: 'warning',
      code: BYTE_ORDER_MARK,
      message:
        'The file is saved with a byte order mark. jsonte copies it into the JSON it builds, and the game refuses the expression it starts. Save it as UTF-8 without one.',
    });
  }
  for (const c of stripMolangFile(text).comments) {
    const next = text[c.start + 1];
    if (next === ' ') continue;
    out.push({
      start: c.start,
      end: Math.min(c.end, c.start + 1),
      severity: 'warning',
      code: COMMENT_SPACE,
      message: "jsonte removes only comments that start with '# ', so this one would reach the game. Add a space after the #.",
    });
  }
  return out;
}
