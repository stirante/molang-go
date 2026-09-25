// What to offer at a cursor, worked out from the text before it.
//
// The analysis tokens are no help here: completion is asked for in the middle
// of typing, where the source is at its most broken, and the question is only
// ever about the few characters just behind the cursor -- which namespace, if
// any, the name being typed belongs to, and whether it follows an `->`.

import {
  CompletionItemKind,
  CompletionItemTag,
  InsertTextFormat,
  MarkupKind,
  type CompletionItem,
} from 'vscode-languageserver-types';
import type { AnalyzeOptions, CanonicalNamespace } from './bridge';
import {
  argRange,
  entryDocs,
  functionDocs,
  querySetOf,
  signatureLabel,
  type Catalogue,
  type CatalogueFunction,
} from './catalogue';

/** Every spelling the language accepts, with what it means. */
export const NAMESPACE_ALIASES: Record<string, CanonicalNamespace> = {
  query: 'query',
  q: 'query',
  math: 'math',
  variable: 'variable',
  v: 'variable',
  temp: 'temp',
  t: 'temp',
  context: 'context',
  c: 'context',
  array: 'array',
  geometry: 'geometry',
  material: 'material',
  texture: 'texture',
};

export const KEYWORDS = ['return', 'loop', 'for_each', 'break', 'continue', 'this', 'true', 'false'];

export type CompletionContext =
  | { kind: 'none' }
  | {
      kind: 'root';
      /** Region offsets of the name being typed, which completion replaces. */
      start: number;
      end: number;
      prefix: string;
      afterArrow: boolean;
    }
  | {
      kind: 'member';
      namespace: CanonicalNamespace;
      written: string;
      start: number;
      end: number;
      prefix: string;
      afterArrow: boolean;
    };

const nameChar = /[A-Za-z0-9_]/;

/**
 * Where the cursor is, for completion. text is the region's Molang source
 * (comments already blanked), offset the cursor in it.
 */
export function completionContext(text: string, offset: number): CompletionContext {
  // Inside a string literal nothing is Molang. Strings have no escapes, so
  // counting quotes is enough.
  let quotes = 0;
  for (let i = 0; i < offset; i++) if (text[i] === "'") quotes++;
  if (quotes % 2 === 1) return { kind: 'none' };

  let start = offset;
  while (start > 0 && (nameChar.test(text[start - 1]) || text[start - 1] === '.')) start--;
  let end = offset;
  while (end < text.length && nameChar.test(text[end])) end++;
  const chain = text.slice(start, offset);
  // A number: `1.`, `0.5` -- nothing to offer.
  if (/^[0-9.]/.test(chain)) return { kind: 'none' };

  let before = start;
  while (before > 0 && /\s/.test(text[before - 1])) before--;
  const afterArrow = before >= 2 && text[before - 1] === '>' && text[before - 2] === '-';

  const dot = chain.indexOf('.');
  if (dot < 0) {
    return { kind: 'root', start, end, prefix: chain, afterArrow };
  }
  const written = chain.slice(0, dot);
  const namespace = NAMESPACE_ALIASES[written.toLowerCase()];
  if (!namespace) return { kind: 'none' };
  return {
    kind: 'member',
    namespace,
    written,
    start: start + dot + 1,
    end,
    prefix: chain.slice(dot + 1),
    afterArrow,
  };
}

/** A name the document already uses, for offering it again. */
export interface KnownName {
  namespace: CanonicalNamespace;
  /** As first written. */
  name: string;
  reads: number;
  writes: number;
}

export interface CompletionInput {
  context: CompletionContext;
  catalogue: Catalogue;
  /** Names used anywhere in the document. */
  names: readonly KnownName[];
  /** How the region is read; decides which queries it can name. */
  options?: AnalyzeOptions;
}

/**
 * Items to offer, without ranges: the caller places them at the context's
 * start..end in document coordinates. sortText starts with "0" so the
 * document's own names and the catalogue sort ahead of anything another
 * extension merges in, and detail says where an item comes from.
 */
export function completionItems(input: CompletionInput): CompletionItem[] {
  const { context, catalogue } = input;
  if (context.kind === 'none') return [];
  if (context.kind === 'root') return rootItems(context.afterArrow, catalogue);

  const items: CompletionItem[] = [];
  const ns = context.namespace;
  if (context.afterArrow && ns !== 'variable' && ns !== 'query') return [];
  if (ns === 'query' || ns === 'math') {
    const table = ns === 'query' ? catalogue.queries : catalogue.math;
    for (const f of table.values()) {
      if (ns === 'query' && !resolvable(f, input.options)) continue;
      const { min } = argRange(f);
      const args = f.args ?? [];
      let insertText = f.name;
      let format: InsertTextFormat = InsertTextFormat.PlainText;
      // Math functions are always called; queries only when they need
      // arguments -- `q.is_baby` is how everyone writes a query that takes
      // none, and an optional argument is the author's choice to add.
      // The placeholders are the arguments that must be written, which for
      // a query with an optional one in the middle are not simply the first
      // few: q.is_item_name_any(slot, [index,] names...).
      const required = ns === 'math' ? args : args.filter((a) => !a.optional);
      const callArgs = ns === 'math' ? (f.constant || f.name === 'pi' ? -1 : args.length) : min > 0 ? required.length : -1;
      if (callArgs >= 0) {
        const placeholders = required.slice(0, callArgs).map((a, i) => `\${${i + 1}:${a.name}}`);
        insertText = `${f.name}(${placeholders.join(', ')})`;
        format = InsertTextFormat.Snippet;
      }
      items.push({
        label: f.name,
        kind: CompletionItemKind.Function,
        detail: signatureLabel(ns, f),
        labelDetails: f.summary ? { description: f.summary } : undefined,
        documentation: { kind: MarkupKind.Markdown, value: functionDocs(ns, f, catalogue) },
        insertText,
        insertTextFormat: format,
        tags: f.deprecated ? [CompletionItemTag.Deprecated] : undefined,
        sortText: `0${f.deprecated ? 'z' : 'a'}${f.name}`,
        // Trigger signature help once the parentheses are in.
        command: format === InsertTextFormat.Snippet && callArgs > 0
          ? { title: 'Signature help', command: 'editor.action.triggerParameterHints' }
          : undefined,
      });
    }
    return items;
  }

  const seen = new Set<string>();
  for (const n of input.names) {
    if (n.namespace !== ns) continue;
    const key = n.name.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    items.push({
      label: n.name,
      kind: ns === 'context' ? CompletionItemKind.Constant : CompletionItemKind.Variable,
      detail: `${ns}.${n.name} — ${usage(n)} in this file`,
      sortText: `0a${key}`,
    });
  }
  if (ns === 'context') {
    // The names the game supplies somewhere, each saying where.
    for (const k of catalogue.namespace('context')?.knownNames ?? []) {
      if (seen.has(k.name.toLowerCase())) continue;
      seen.add(k.name.toLowerCase());
      items.push({
        label: k.name,
        kind: CompletionItemKind.Constant,
        detail: `context.${k.name}`,
        labelDetails: k.contexts?.length ? { description: k.contexts.join('; ') } : undefined,
        documentation: k.description ? { kind: MarkupKind.Markdown, value: k.description } : undefined,
        sortText: `0b${k.name}`,
      });
    }
  }
  return items;
}

/**
 * Whether the region can name query f: in the field's allow-list if it has
 * one, in its query set if it has one. As the bridge decides it, so what is
 * offered is what then passes.
 */
export function resolvable(f: CatalogueFunction, options: AnalyzeOptions | undefined): boolean {
  const allowed = options?.allowedQueries;
  if (allowed?.length) {
    return allowed.some((q) => q.replace(/^(query|q)\./, '').toLowerCase() === f.name.toLowerCase());
  }
  return !options?.querySet || querySetOf(f) === options.querySet;
}

function usage(n: KnownName): string {
  const parts: string[] = [];
  if (n.writes) parts.push(`written ${n.writes}×`);
  if (n.reads) parts.push(`read ${n.reads}×`);
  return parts.join(', ') || 'named';
}

function rootItems(afterArrow: boolean, catalogue: Catalogue): CompletionItem[] {
  // After `->` only another entity's variable or query can follow.
  const names = afterArrow
    ? ['query', 'q', 'variable', 'v']
    : Object.keys(NAMESPACE_ALIASES);
  const items: CompletionItem[] = names.map((name) => {
    const canonical = NAMESPACE_ALIASES[name];
    const entry = catalogue.namespace(canonical);
    return {
      label: name,
      kind: CompletionItemKind.Module,
      detail: name === canonical ? `${canonical} namespace` : `short for ${canonical}`,
      labelDetails: entry?.summary ? { description: entry.summary } : undefined,
      documentation: entry ? { kind: MarkupKind.Markdown, value: entryDocs(entry) } : undefined,
      insertText: `${name}.`,
      // Straight on to the members.
      command: { title: 'Suggest', command: 'editor.action.triggerSuggest' },
      sortText: `0a${name.length > 1 ? 0 : 1}${name}`,
    };
  });
  if (!afterArrow) {
    for (const k of KEYWORDS) {
      const entry = catalogue.operators.get(k) ?? catalogue.namespaces.get(k);
      items.push({
        label: k,
        kind: CompletionItemKind.Keyword,
        labelDetails: entry?.summary ? { description: entry.summary } : undefined,
        documentation: entry ? { kind: MarkupKind.Markdown, value: entryDocs(entry) } : undefined,
        sortText: `0c${k}`,
      });
    }
  }
  return items;
}
