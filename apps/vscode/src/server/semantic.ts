// Semantic tokens in the protocol's encoding. The type and modifier names
// are the ones the bridge reports (VS Code's standard names), so themes
// colour them with no custom scopes.

export const TOKEN_TYPES = ['namespace', 'function', 'variable', 'property', 'keyword', 'number', 'string', 'operator'];
export const TOKEN_MODIFIERS = ['defaultLibrary', 'readonly', 'modification', 'deprecated'];

export interface HostToken {
  /** Document offsets, end exclusive. */
  start: number;
  end: number;
  type: string;
  mods?: string[];
}

export interface LineIndex {
  positionAt(offset: number): { line: number; character: number };
}

/**
 * Encodes tokens as the protocol's relative integer runs. A token that
 * crosses a line end (a string literal can) is split per line, since a
 * semantic token cannot span lines. Tokens of unknown type are dropped.
 */
export function encodeTokens(tokens: readonly HostToken[], doc: LineIndex, text: string): number[] {
  type Piece = { line: number; char: number; length: number; type: number; mods: number };
  const pieces: Piece[] = [];
  for (const t of tokens) {
    const type = TOKEN_TYPES.indexOf(t.type);
    if (type < 0 || t.end <= t.start) continue;
    let mods = 0;
    for (const m of t.mods ?? []) {
      const bit = TOKEN_MODIFIERS.indexOf(m);
      if (bit >= 0) mods |= 1 << bit;
    }
    let s = t.start;
    while (s < t.end) {
      let e = s;
      while (e < t.end && text[e] !== '\n' && text[e] !== '\r') e++;
      if (e > s) {
        const p = doc.positionAt(s);
        pieces.push({ line: p.line, char: p.character, length: e - s, type, mods });
      }
      s = e;
      while (s < t.end && (text[s] === '\n' || text[s] === '\r')) s++;
    }
  }
  pieces.sort((a, b) => a.line - b.line || a.char - b.char);
  const data: number[] = [];
  let line = 0;
  let char = 0;
  for (const p of pieces) {
    const dl = p.line - line;
    const dc = dl === 0 ? p.char - char : p.char;
    data.push(dl, dc, p.length, p.type, p.mods);
    line = p.line;
    char = p.char;
  }
  return data;
}
