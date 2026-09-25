// Variables and temps as symbols: where one is written, where it is read,
// and what renaming it has to change.
//
// The two have different reach. A variable belongs to the entity and lives
// between expressions: set in a client entity's scripts/initialize, read in
// an animation controller of the same file. A temp lives for one expression
// only, so a temp.x in one JSON string and a temp.x in the next are
// unrelated. And a name read through `->` (q.parent->v.x) is another
// entity's variable, which this file's v.x says nothing about.

import type { CanonicalNamespace } from './bridge';

export interface Occurrence {
  /** Document offsets of the whole reference, `v.speed`. */
  start: number;
  end: number;
  /** Document offsets of the name alone, `speed`. */
  nameStart: number;
  nameEnd: number;
  write: boolean;
  /** Index of the region it is in. */
  region: number;
  /** Read through `->`: another entity's. */
  arrow: boolean;
}

export interface NameEntry {
  namespace: CanonicalNamespace;
  /** As first written. */
  name: string;
  at: Occurrence[];
}

/** The namespaces whose names link to each other in a document. */
export const LINKED_NAMESPACES: ReadonlySet<CanonicalNamespace> = new Set(['variable', 'temp']);

/**
 * The variable or temp named at a document offset, and the occurrence
 * there. The end is included, so a cursor just after the name finds it.
 */
export function occurrenceAt(
  names: ReadonlyMap<string, NameEntry>,
  offset: number,
): { entry: NameEntry; occ: Occurrence } | undefined {
  for (const entry of names.values()) {
    if (!LINKED_NAMESPACES.has(entry.namespace)) continue;
    const occ = entry.at.find((o) => offset >= o.start && offset <= o.end);
    if (occ) return occ.arrow ? undefined : { entry, occ };
  }
  return undefined;
}

/** Every occurrence that is the same variable or temp as occ, occ included. */
export function sameSymbol(entry: NameEntry, occ: Occurrence): Occurrence[] {
  return entry.at.filter((o) => !o.arrow && (entry.namespace !== 'temp' || o.region === occ.region));
}

/** Whether s can be a variable or temp name. */
export function isName(s: string): boolean {
  return /^[A-Za-z_][A-Za-z0-9_]*$/.test(s);
}
