// The catalogue of query and math functions, namespaces and operators, as
// the extension ships it: catalogue/catalogue.json (the queries), with
// math.json and namespaces.json beside it, composed into one document by
// composeCatalogue. The Go side reads that document for its checks
// (bridge.Catalogue); this side reads it for completion, hover and
// signature help. Every field is optional, so a partial catalogue works as
// far as it goes.

export interface CatalogueArg {
  name: string;
  type?: string;
  optional?: boolean;
  description?: string;
  /** Every form the argument accepts, in words: enum strings, conversions. */
  accepts?: string[];
  /** The value used when the argument is left out. */
  default?: unknown;
  variadic?: boolean;
}

/** How far an entry has been checked in the game, as the catalogue publishes it. */
export interface CatalogueVerification {
  status: 'verified' | 'partial' | 'documented' | 'unobservable' | 'pending';
  game: string;
  label: string;
}

export interface CatalogueFunction {
  name: string;
  args?: CatalogueArg[];
  variadic?: boolean;
  minArgs?: number;
  maxArgs?: number;
  returns?: string;
  deprecated?: { replacement?: string | null; note?: string } | null;
  versionGate?: { since?: string; until?: string } | null;
  /** default, tags or world_gen; absent is default. */
  querySet?: string;
  contexts?: string[] | null;
  clientOnly?: boolean;
  /** A value rather than a function: math.pi. */
  constant?: boolean;
  /** One line, for lists. */
  summary?: string;
  /** Markdown. */
  description?: string;
  /**
   * Markdown, at most four lines: the call and what it returns, then the
   * facts most likely to bite. Shown instead of description, example and
   * notes when present.
   */
  hover?: string;
  /** client, server or both, possibly with a note. */
  side?: string;
  /** The entry's page on the documentation site. */
  docs?: string;
  verification?: CatalogueVerification;
  example?: string;
  notes?: string[];
}

export interface KnownName {
  name: string;
  /** Where the game supplies it, in words. */
  contexts?: string[];
  description?: string;
}

export interface CatalogueNamespace {
  name: string;
  /** For a short spelling: the namespace it stands for. */
  aliasOf?: string;
  summary?: string;
  description?: string;
  example?: string;
  notes?: string[];
  assignable?: boolean;
  /** The context.* names the game supplies somewhere. */
  knownNames?: KnownName[];
}

export interface CatalogueOperator {
  token: string;
  summary?: string;
  description?: string;
  example?: string;
  notes?: string[];
}

export interface CatalogueContext {
  id: string;
  name?: string;
  variables?: string[];
}

export interface CatalogueDocument {
  gameVersion?: string;
  partial?: boolean;
  queries?: CatalogueFunction[];
  math?: CatalogueFunction[];
  namespaces?: CatalogueNamespace[];
  operators?: CatalogueOperator[];
  contexts?: CatalogueContext[] | Record<string, string | Omit<CatalogueContext, 'id'>>;
}

export class Catalogue {
  readonly queries = new Map<string, CatalogueFunction>();
  readonly math = new Map<string, CatalogueFunction>();
  readonly namespaces = new Map<string, CatalogueNamespace>();
  readonly operators = new Map<string, CatalogueOperator>();
  readonly contexts = new Map<string, CatalogueContext>();
  readonly partial: boolean;
  readonly gameVersion: string;

  constructor(doc: CatalogueDocument = {}) {
    for (const q of doc.queries ?? []) this.queries.set(q.name.toLowerCase(), q);
    for (const m of doc.math ?? []) this.math.set(m.name.toLowerCase(), m);
    for (const n of doc.namespaces ?? []) this.namespaces.set(n.name, n);
    for (const o of doc.operators ?? []) this.operators.set(o.token, o);
    const ctx = doc.contexts ?? [];
    if (Array.isArray(ctx)) {
      for (const c of ctx) this.contexts.set(c.id, c);
    } else {
      for (const [id, c] of Object.entries(ctx)) {
        this.contexts.set(id, typeof c === 'string' ? { id, name: c } : { ...c, id });
      }
    }
    this.partial = doc.partial ?? false;
    this.gameVersion = doc.gameVersion ?? '';
  }

  static parse(json: string): Catalogue {
    return new Catalogue(JSON.parse(json) as CatalogueDocument);
  }

  lookup(namespace: 'query' | 'math', name: string): CatalogueFunction | undefined {
    return (namespace === 'query' ? this.queries : this.math).get(name.toLowerCase());
  }

  /** The namespace's own entry, not its short spelling's. */
  namespace(name: string): CatalogueNamespace | undefined {
    const n = this.namespaces.get(name);
    return n?.aliasOf ? (this.namespaces.get(n.aliasOf) ?? n) : n;
  }

  /** A context.* name the game supplies, if the catalogue knows it. */
  contextName(name: string): KnownName | undefined {
    const lower = name.toLowerCase();
    return this.namespace('context')?.knownNames?.find((k) => k.name.toLowerCase() === lower);
  }
}

/**
 * The catalogue document from its three shipped files: the queries, the math
 * table (a list) and the namespace and operator notes ({namespaces,
 * operators}). The companions are optional; given, they replace whatever the
 * queries file carries of the same.
 */
export function composeCatalogue(catalogue: string, math?: string, namespaces?: string): string {
  const doc = JSON.parse(catalogue) as CatalogueDocument;
  if (math) doc.math = JSON.parse(math) as CatalogueFunction[];
  if (namespaces) {
    const ns = JSON.parse(namespaces) as { namespaces?: CatalogueNamespace[]; operators?: CatalogueOperator[] };
    if (ns.namespaces) doc.namespaces = ns.namespaces;
    if (ns.operators) doc.operators = ns.operators;
  }
  return JSON.stringify(doc);
}

/** Markdown for a namespace or operator entry. */
export function entryDocs(e: { summary?: string; description?: string; example?: string; notes?: string[] }): string {
  const lines: string[] = [];
  const text = e.description || e.summary;
  if (text) lines.push(text);
  if (e.example) lines.push('```molang\n' + e.example + '\n```');
  if (e.notes?.length) lines.push(e.notes.map((n) => `- ${n}`).join('\n'));
  return lines.join('\n\n');
}

const QUERY_SETS = ['default', 'tags', 'world_gen'];

/** The query set f is in. As bridge.Function.querySet. */
export function querySetOf(f: CatalogueFunction): string {
  if (f.querySet) return f.querySet;
  if (f.contexts?.length === 1 && QUERY_SETS.includes(f.contexts[0])) return f.contexts[0];
  return 'default';
}

/** The number of arguments f accepts; max -1 is unbounded. As bridge.Function.ArgRange. */
export function argRange(f: CatalogueFunction): { min: number; max: number } {
  const args = f.args ?? [];
  let min = args.filter((a) => !a.optional).length;
  let max = f.variadic ? -1 : args.length;
  if (typeof f.minArgs === 'number') min = f.minArgs;
  if (typeof f.maxArgs === 'number') max = f.maxArgs;
  return { min, max };
}

/** `query.name(axis: number, ...names: string): number` */
export function signatureLabel(namespace: string, f: CatalogueFunction): string {
  const args = f.args ?? [];
  const parts = args.map((a, i) => {
    const rest = f.variadic && i === args.length - 1 ? '...' : '';
    const opt = a.optional ? '?' : '';
    const type = a.type ? `: ${a.type}` : '';
    return `${rest}${a.name}${opt}${type}`;
  });
  const returns = f.returns && f.returns !== 'unknown' ? `: ${f.returns}` : '';
  if (namespace === 'math' && (f.constant || f.name === 'pi')) return `math.${f.name}${returns}`;
  // A query with no arguments is usually written without parentheses.
  if (namespace === 'query' && args.length === 0) return `query.${f.name}${returns}`;
  return `${namespace}.${f.name}(${parts.join(', ')})${returns}`;
}

export function functionDocs(namespace: string, f: CatalogueFunction, catalogue: Catalogue): string {
  const lines: string[] = [];
  if (f.deprecated) {
    const repl = f.deprecated.replacement ? ` Use \`${f.deprecated.replacement}\` instead.` : '';
    lines.push(`**Deprecated.**${repl}${f.deprecated.note ? ' ' + f.deprecated.note : ''}`);
  }
  if (f.hover) {
    // Hover lines are separate facts; a Markdown hard break keeps them apart.
    lines.push(f.hover.split('\n').join('  \n'));
  } else {
    const text = f.description || f.summary;
    if (text) lines.push(text);
    if (f.example) lines.push('```molang\n' + f.example + '\n```');
    if (f.notes?.length) lines.push(f.notes.map((n) => `- ${n}`).join('\n'));
  }
  const set = querySetOf(f);
  if (set === 'world_gen') lines.push('World generation expressions only.');
  if (set === 'tags') lines.push('Item and block tag expressions only.');
  if (f.versionGate?.until) lines.push(`Removed in ${f.versionGate.until}.`);
  if (f.versionGate?.since) lines.push(`Added in ${f.versionGate.since}.`);
  if (f.clientOnly) lines.push('Client only.');
  if (namespace === 'query' && catalogue.partial && !lines.length) {
    lines.push('_Not yet described._');
  }
  if (f.docs) lines.push(`[Documentation →](${f.docs})`);
  return lines.join('\n\n');
}

/** Markdown for one argument in signature help: what it is, what it accepts, its default. */
export function argDocs(a: CatalogueArg): string {
  const parts: string[] = [];
  if (a.description) parts.push(a.description);
  if (a.accepts?.length) parts.push('Accepts:\n' + a.accepts.map((x) => `- ${x}`).join('\n'));
  if (a.default !== undefined && a.default !== null) {
    parts.push(`Default: \`${typeof a.default === 'string' ? a.default : JSON.stringify(a.default)}\``);
  }
  return parts.join('\n\n');
}
