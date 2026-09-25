// Which call the cursor is in, and which argument, for signature help.
//
// A forward scan of the region up to the cursor with a stack of open
// brackets, skipping string literals. Like completion, this has to work on
// source that does not parse, so it reads characters rather than the parse.

import { NAMESPACE_ALIASES } from './completion';
import type { CanonicalNamespace } from './bridge';

export interface CallSite {
  namespace: CanonicalNamespace;
  name: string;
  /** Zero-based index of the argument the cursor is in. */
  argument: number;
  /** Region offset of the '('. */
  open: number;
}

interface Frame {
  call?: { namespace: CanonicalNamespace; name: string; open: number };
  commas: number;
}

export function findCall(text: string, offset: number): CallSite | undefined {
  const stack: Frame[] = [];
  let inString = false;
  for (let i = 0; i < offset && i < text.length; i++) {
    const c = text[i];
    if (inString) {
      if (c === "'") inString = false;
      continue;
    }
    switch (c) {
      case "'":
        inString = true;
        break;
      case '(':
        stack.push({ call: callee(text, i), commas: 0 });
        break;
      case '[':
      case '{':
        stack.push({ commas: 0 });
        break;
      case ')':
      case ']':
      case '}':
        stack.pop();
        break;
      case ',':
        if (stack.length) stack[stack.length - 1].commas++;
        break;
    }
  }
  // A cursor inside a string literal is still inside its argument.
  const top = stack[stack.length - 1];
  if (!top?.call) return undefined;
  return { ...top.call, argument: top.commas };
}

/** The `ns.name` written just before the '(' at open, if it is a call. */
function callee(text: string, open: number): Frame['call'] {
  let end = open;
  while (end > 0 && /\s/.test(text[end - 1])) end--;
  let start = end;
  while (start > 0 && /[A-Za-z0-9_.]/.test(text[start - 1])) start--;
  const chain = text.slice(start, end);
  const dot = chain.indexOf('.');
  if (dot <= 0) return undefined;
  const namespace = NAMESPACE_ALIASES[chain.slice(0, dot).toLowerCase()];
  if (namespace !== 'query' && namespace !== 'math') return undefined;
  return { namespace, name: chain.slice(dot + 1), open };
}
