// Quick fixes for the diagnostics the service reports.
//
// Each fix is worked out again from the diagnostic the editor hands back --
// its code, message and range -- and the document's analysis, rather than
// carried along with the diagnostic: the analysis is cached, and the
// diagnostic is only a key into it.

import type { TextDocument } from 'vscode-languageserver-textdocument';
import { CodeActionKind, Range, TextEdit, type CodeAction, type Diagnostic } from 'vscode-languageserver-types';
import type { BridgeRef, CanonicalNamespace } from './bridge';
import { argRange, type Catalogue } from './catalogue';
import { resolvable } from './completion';
import { BYTE_ORDER_MARK, COMMENT_SPACE } from './molangFile';
import type { MolangRegion } from './regions';

/** What a quick fix needs of the document's analysis. */
export interface ActionInput {
  doc: TextDocument;
  catalogue: Catalogue;
  regions: readonly { region: MolangRegion; result: { refs: BridgeRef[] } }[];
}

/** Every namespace's full name. */
const FULL_NAMES: readonly CanonicalNamespace[] = [
  'query',
  'math',
  'variable',
  'temp',
  'context',
  'array',
  'geometry',
  'material',
  'texture',
];

/**
 * Short spellings people write that the language does not have. `a.` is
 * the one that matters most: it reads like array's short form, and every
 * other tool accepts it, but the game does not.
 */
const MISSPELT_NAMESPACES: Record<string, CanonicalNamespace> = {
  a: 'array',
  arr: 'array',
  m: 'math',
  ctx: 'context',
  var: 'variable',
  tmp: 'temp',
};

export function codeActions(input: ActionInput, diagnostics: readonly Diagnostic[]): CodeAction[] {
  const out: CodeAction[] = [];
  for (const d of diagnostics) {
    if (d.source !== 'molang') continue;
    out.push(...fixes(input, d));
  }
  return out;
}

function fixes(input: ActionInput, d: Diagnostic): CodeAction[] {
  const { doc } = input;
  const fix = (title: string, edit: TextEdit, preferred = false): CodeAction => ({
    title,
    kind: CodeActionKind.QuickFix,
    diagnostics: [d],
    isPreferred: preferred || undefined,
    edit: { changes: { [doc.uri]: [edit] } },
  });

  switch (d.code) {
    case COMMENT_SPACE:
      return [fix("Add a space after the '#'", TextEdit.insert(doc.positionAt(doc.offsetAt(d.range.start) + 1), ' '), true)];
    case BYTE_ORDER_MARK:
      // The mark is not in the text the editor holds, so no text edit can
      // take it out; the file's encoding has to change.
      return [
        {
          title: 'Save without a byte order mark (choose UTF-8)',
          kind: CodeActionKind.QuickFix,
          diagnostics: [d],
          command: { title: 'Change file encoding', command: 'workbench.action.editor.changeEncoding' },
        },
      ];
  }

  const hit = locate(input, doc.offsetAt(d.range.start));
  if (!hit) return [];
  const { region, refs, at } = hit;
  const hostRange = (start: number, end: number) =>
    Range.create(doc.positionAt(region.toHost(start)), doc.positionAt(region.toHost(end)));

  if (d.code === 'query-deprecated') {
    const ref = refs.find((r) => r.start === at && r.namespace === 'query');
    const f = ref && input.catalogue.lookup('query', ref.name);
    const replacement = f?.deprecated?.replacement?.replace(/^(query|q)\./i, '');
    if (!ref || !f || !replacement) return [];
    const to = input.catalogue.lookup('query', replacement);
    // Preferred only where the arguments carry over as they are.
    const same = !!to && JSON.stringify(argRange(f)) === JSON.stringify(argRange(to));
    return [fix(`Replace with ${ref.written}.${replacement}`, TextEdit.replace(hostRange(ref.nameStart, ref.nameEnd), replacement), same)];
  }

  if (d.code === 'unknown-query') {
    const ref = refs.find((r) => r.start === at && r.namespace === 'query');
    if (!ref) return [];
    const names = suggestQueries(ref.name, input.catalogue, region.options);
    return names.map((n, i) =>
      fix(`Did you mean ${ref.written}.${n}?`, TextEdit.replace(hostRange(ref.nameStart, ref.nameEnd), n), i === 0 && names.length === 1),
    );
  }

  if (d.code === 'syntax') {
    const message = typeof d.message === 'string' ? d.message : d.message.value;
    const ident = /^unknown identifier '([^']+)'/.exec(message)?.[1];
    if (ident && region.text[at + ident.length] === '.') {
      const to = suggestNamespace(ident);
      if (!to) return [];
      return [fix(`Change '${ident}.' to '${to}.'`, TextEdit.replace(hostRange(at, at + ident.length), to), true)];
    }
    if (message.includes("must end with a ';'")) {
      return [fix("Add the missing ';'", TextEdit.insert(doc.positionAt(region.toHost(at)), ';'), true)];
    }
    if (message.includes("'\\ufeff'") && region.text[at] === '\uFEFF') {
      return [fix('Remove the byte order mark', TextEdit.del(hostRange(at, at + 1)), true)];
    }
  }
  return [];
}

function locate(input: ActionInput, offset: number) {
  for (const { region, result } of input.regions) {
    const at = region.fromHost(offset);
    if (at !== undefined) return { region, refs: result.refs, at };
  }
  return undefined;
}

/**
 * The queries a mistyped name most likely meant: the nearest by edit
 * distance among those the field can name, at most three, none further
 * than a third of the name's length. Deprecated ones come after the rest.
 */
export function suggestQueries(name: string, catalogue: Catalogue, options: MolangRegion['options']): string[] {
  const lower = name.toLowerCase();
  const limit = Math.max(1, Math.floor(lower.length / 3));
  const found: { name: string; d: number; old: boolean }[] = [];
  for (const f of catalogue.queries.values()) {
    if (!resolvable(f, options)) continue;
    const d = editDistance(lower, f.name.toLowerCase(), limit);
    if (d <= limit) found.push({ name: f.name, d, old: !!f.deprecated });
  }
  found.sort((x, y) => Number(x.old) - Number(y.old) || x.d - y.d || x.name.localeCompare(y.name));
  return found.slice(0, 3).map((f) => f.name);
}

/** The namespace a spelling the language lacks was meant as, if it is clear. */
export function suggestNamespace(written: string): CanonicalNamespace | undefined {
  const lower = written.toLowerCase();
  if (MISSPELT_NAMESPACES[lower]) return MISSPELT_NAMESPACES[lower];
  if (lower.length < 3) return undefined;
  let best: CanonicalNamespace | undefined;
  let bestD = 3;
  let tie = false;
  for (const n of FULL_NAMES) {
    const d = editDistance(lower, n, 2);
    if (d < bestD) {
      best = n;
      bestD = d;
      tie = false;
    } else if (d === bestD) {
      tie = true;
    }
  }
  return best && !tie ? best : undefined;
}

/**
 * Edit distance with adjacent transpositions (optimal string alignment),
 * giving up early -- returning limit + 1 -- once the answer is past limit.
 */
export function editDistance(a: string, b: string, limit = Infinity): number {
  if (Math.abs(a.length - b.length) > limit) return limit + 1;
  let prev2: number[] = [];
  let prev = Array.from({ length: b.length + 1 }, (_, j) => j);
  for (let i = 1; i <= a.length; i++) {
    const cur = [i];
    let rowMin = i;
    for (let j = 1; j <= b.length; j++) {
      const cost = a[i - 1] === b[j - 1] ? 0 : 1;
      let v = Math.min(prev[j] + 1, cur[j - 1] + 1, prev[j - 1] + cost);
      if (i > 1 && j > 1 && a[i - 1] === b[j - 2] && a[i - 2] === b[j - 1]) v = Math.min(v, prev2[j - 2] + 1);
      cur.push(v);
      rowMin = Math.min(rowMin, v);
    }
    if (rowMin > limit) return limit + 1;
    prev2 = prev;
    prev = cur;
  }
  return prev[b.length];
}
