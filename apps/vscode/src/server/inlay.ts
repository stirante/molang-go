// Parameter names before the arguments of query and math calls:
// math.clamp(value: v.x, min: 0, max: 1). Off unless asked for; the names
// are the catalogue's.

import type { BridgeRef } from './bridge';
import type { Catalogue } from './catalogue';

export interface ParameterHint {
  /** Region offset of the argument the name goes before. */
  at: number;
  name: string;
}

export function parameterHints(text: string, refs: readonly BridgeRef[], catalogue: Catalogue): ParameterHint[] {
  const out: ParameterHint[] = [];
  for (const r of refs) {
    if (!r.call || r.args <= 0 || (r.namespace !== 'query' && r.namespace !== 'math')) continue;
    const f = catalogue.lookup(r.namespace, r.name);
    const params = f?.args ?? [];
    if (!params.length) continue;
    let open = r.nameEnd;
    while (open < text.length && /\s/.test(text[open])) open++;
    if (text[open] !== '(') continue;
    argumentStarts(text, open).forEach(({ start, arg }, i) => {
      const p = params[i] ?? (f?.variadic ? params[params.length - 1] : undefined);
      if (!p) return;
      // Nothing to add when the argument already says it: math.clamp(value, ...).
      const lower = arg.toLowerCase();
      const name = p.name.toLowerCase();
      if (lower === name || lower.endsWith(`.${name}`)) return;
      out.push({ at: start, name: p.name });
    });
  }
  return out;
}

/**
 * Where each argument of the call whose '(' is at open starts, past its
 * leading whitespace, with its text; empty arguments are left out.
 */
export function argumentStarts(text: string, open: number): { start: number; arg: string }[] {
  const out: { start: number; arg: string }[] = [];
  let depth = 0;
  let from = open + 1;
  const push = (end: number) => {
    let s = from;
    while (s < end && /\s/.test(text[s])) s++;
    const arg = text.slice(s, end).trim();
    if (arg) out.push({ start: s, arg });
  };
  for (let i = open + 1; i < text.length; i++) {
    const c = text[i];
    if (c === "'") {
      const close = text.indexOf("'", i + 1);
      if (close < 0) break;
      i = close;
    } else if (c === '(' || c === '[' || c === '{') {
      depth++;
    } else if (c === ')' || c === ']' || c === '}') {
      if (depth === 0) {
        push(i);
        return out;
      }
      depth--;
    } else if (c === ',' && depth === 0) {
      push(i);
      from = i + 1;
    }
  }
  push(text.length);
  return out;
}
