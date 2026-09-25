// Finding the Molang in a JSON schema: the paths, from the document root, at
// which the schema says a string is Molang.
//
// No marker is shared across the schema sets Bedrock authors use, so several
// are recognised:
//
//   "format": "molang"                Blockception, on string-typed Molang.
//   a title starting "Molang"         Blockception's "Molang Number" /
//                                     "Molang Boolean" / "Molang Color",
//                                     Mojang's "Molang expression".
//   a $ref to Expression Node.json,   Mojang's published schemas, whose
//   Molang string.json or             Molang is a reference to one of these
//   Molang Expression.json            files rather than a property of the node.
//   a $ref to .../molang/embedded.json   bridge.'s schemas.
//   {"expression", "version"}         the object form of a Molang field,
//                                     whose "expression" member is the source.
//
// The walk follows $ref (local pointers, other documents, $id), allOf /
// anyOf / oneOf / if-then-else, properties, patternProperties,
// additionalProperties, items, prefixItems and additionalItems, and takes
// the union of every branch: "may this string be Molang" is better answered
// yes by one branch too many than no by one too few. A definition reached
// again while it is being expanded -- a recursive schema: nested filters,
// event sequences -- is not expanded a second time, so recursion contributes
// one level of paths; the curated catalogue spells out the recursive places
// with "**".
//
// Plain data in, plain data out: the loading of schemas, which needs the
// editor, is the caller's (schemaIndex.ts), so the walk is tested alone.

import type { FileType, PathEntry } from '../server/embedding';

export interface SchemaHit {
  /** Path segments from the document root, in the catalogue's pattern language. */
  path: string[];
  /** JSON types the field takes, as in the path catalogue. */
  accepts: string[];
}

/** A node found by a reference: the node, the document it is in, and a key for it. */
export interface Resolved {
  node: unknown;
  base: string;
  id: string;
}

export type ResolveRef = (ref: string, base: string) => Resolved | undefined;

// Deep enough for any real schema, shallow enough that a schema built to
// recurse through fresh nodes cannot hang the editor.
const MAX_DEPTH = 48;

const REF_MARKERS: Record<string, string[]> = {
  'Expression Node.json': ['string', 'number', 'object'],
  'Molang string.json': ['string', 'object'],
  'Molang Expression.json': ['string', 'number', 'boolean'],
};

type Obj = Record<string, unknown>;
const isObj = (x: unknown): x is Obj => !!x && typeof x === 'object' && !Array.isArray(x);

/** The types a Molang field takes when this node marks one, else undefined. */
export function nodeMarker(node: Obj): string[] | undefined {
  const title = typeof node.title === 'string' ? node.title : '';
  if (node.format === 'molang' || /^molang\b/i.test(title)) {
    const accepts = new Set(['string']);
    for (const t of typesOf(node)) {
      if (t === 'number' || t === 'integer') accepts.add('number');
      if (t === 'boolean') accepts.add('boolean');
    }
    if (/^molang number/i.test(title)) accepts.add('number');
    if (/^molang boolean/i.test(title)) accepts.add('boolean');
    return [...accepts];
  }
  const props = isObj(node.properties) ? node.properties : undefined;
  if (props && 'expression' in props && 'version' in props && Object.keys(props).length <= 3) {
    return ['object'];
  }
  return undefined;
}

/** The types a Molang field takes when a reference to ref marks one. */
export function refMarker(ref: string): string[] | undefined {
  const file = ref.split('#')[0];
  let decoded = file;
  try {
    decoded = decodeURIComponent(file);
  } catch {
    // A malformed escape: compare it as written.
  }
  const parts = decoded.split('/');
  const base = parts[parts.length - 1];
  if (REF_MARKERS[base]) return REF_MARKERS[base];
  if (base === 'embedded.json' && parts[parts.length - 2] === 'molang') return ['string', 'number', 'boolean'];
  return undefined;
}

function typesOf(node: Obj): string[] {
  const out: string[] = [];
  const add = (n: unknown) => {
    if (!isObj(n)) return;
    if (typeof n.type === 'string') out.push(n.type);
    else if (Array.isArray(n.type)) out.push(...n.type.filter((t): t is string => typeof t === 'string'));
  };
  add(node);
  for (const c of ['anyOf', 'oneOf']) if (Array.isArray(node[c])) (node[c] as unknown[]).forEach(add);
  return out;
}

/** A literal key as a pattern segment: "~" and "/" escaped the JSON-pointer way. */
export function escapeSegment(key: string): string {
  return key.replace(/~/g, '~0').replace(/\//g, '~1');
}

/**
 * Every path at which the schema root (in the document base) marks Molang.
 * Paths that several branches reach are reported once, with the union of
 * the types each branch accepts.
 */
export function findMolangPaths(root: unknown, base: string, resolve: ResolveRef): SchemaHit[] {
  const onStack = new Set<string>();
  const memo = new Map<string, SchemaHit[]>();

  const collect = (node: unknown, base: string, depth: number): SchemaHit[] => {
    if (!isObj(node) || depth > MAX_DEPTH) return [];
    const m = nodeMarker(node);
    if (m) return [{ path: [], accepts: m }];
    const out: SchemaHit[] = [];
    const under = (seg: string, hits: SchemaHit[]) => {
      for (const h of hits) out.push({ path: [seg, ...h.path], accepts: h.accepts });
    };
    const next = (n: unknown) => collect(n, base, depth + 1);

    if (typeof node.$ref === 'string') {
      const rm = refMarker(node.$ref);
      if (rm) out.push({ path: [], accepts: rm });
      else {
        const r = resolve(node.$ref, base);
        if (r) {
          const cached = memo.get(r.id);
          if (cached) out.push(...cached);
          else if (!onStack.has(r.id)) {
            onStack.add(r.id);
            const res = collect(r.node, r.base, depth + 1);
            onStack.delete(r.id);
            memo.set(r.id, res);
            out.push(...res);
          }
        }
      }
    }
    for (const c of ['allOf', 'anyOf', 'oneOf']) {
      if (Array.isArray(node[c])) for (const b of node[c] as unknown[]) out.push(...next(b));
    }
    for (const k of ['if', 'then', 'else']) out.push(...next(node[k]));
    if (isObj(node.properties)) {
      for (const [k, v] of Object.entries(node.properties)) under(escapeSegment(k), next(v));
    }
    if (isObj(node.patternProperties)) {
      for (const v of Object.values(node.patternProperties)) under('*', next(v));
    }
    under('*', next(node.additionalProperties));
    if (Array.isArray(node.prefixItems)) (node.prefixItems as unknown[]).forEach((it, i) => under(`[${i}]`, next(it)));
    if (Array.isArray(node.items)) (node.items as unknown[]).forEach((it, i) => under(`[${i}]`, next(it)));
    else under('[*]', next(node.items));
    under('[*]', next(node.additionalItems));
    return merge(out);
  };

  return merge(collect(root, base, 0));
}

function merge(hits: SchemaHit[]): SchemaHit[] {
  const byPath = new Map<string, SchemaHit>();
  for (const h of hits) {
    const k = h.path.join('/');
    const seen = byPath.get(k);
    if (!seen) byPath.set(k, { path: h.path, accepts: [...h.accepts] });
    else for (const a of h.accepts) if (!seen.accepts.includes(a)) seen.accepts.push(a);
  }
  return [...byPath.values()];
}

/**
 * Hits grouped into the path catalogue's shape: the first segment is the
 * document's root key, the rest the path under it. A root the schema leaves
 * open (patternProperties at the top) becomes the root key "*". Every entry
 * is kind "general": a schema says that a string is Molang, never which kind.
 */
export function hitsToFileTypes(hits: SchemaHit[]): FileType[] {
  const byRoot = new Map<string, PathEntry[]>();
  for (const h of hits) {
    if (h.path.length < 2) continue;
    const [first, ...rest] = h.path;
    // An array at the top is not a pack document.
    if (first.startsWith('[')) continue;
    const rootKey = first === '*' ? '*' : first.replace(/~1/g, '/').replace(/~0/g, '~');
    let list = byRoot.get(rootKey);
    if (!list) byRoot.set(rootKey, (list = []));
    list.push({ path: rest.join('/'), kind: 'general', accepts: h.accepts });
  }
  return [...byRoot].map(([rootKey, paths]) => ({ rootKey, paths }));
}

/**
 * The document-level references of a schema document: those naming another
 * document, resolved against base, so the caller can load them before the
 * walk (which does not wait for anything).
 */
export function externalRefs(doc: unknown, base: string): string[] {
  const out = new Set<string>();
  const visit = (n: unknown) => {
    if (Array.isArray(n)) n.forEach(visit);
    else if (isObj(n)) {
      for (const [k, v] of Object.entries(n)) {
        if (k === '$ref' && typeof v === 'string') {
          const file = v.split('#')[0];
          if (file && !refMarker(v)) {
            const abs = resolveUri(file, base);
            if (abs) out.add(abs);
          }
        } else visit(v);
      }
    }
  };
  visit(doc);
  return [...out];
}

/** ref resolved against base, or undefined when it cannot be. */
export function resolveUri(ref: string, base: string): string | undefined {
  try {
    return new URL(ref, base).href;
  } catch {
    return undefined;
  }
}

/**
 * The key a schema document is stored under: the URI with its fragment
 * dropped and percent-escapes decoded, so "file:///h%3A/x%20y.json" and
 * "file:///h:/x y.json" are the same document. Windows drive letters differ
 * in case between VS Code and URL resolution too.
 */
export function schemaKey(uri: string): string {
  let u = uri.split('#')[0];
  try {
    u = decodeURIComponent(u);
  } catch {
    // Keep it as written.
  }
  return u.replace(/^file:\/\/\/([A-Za-z]):/, (_, d: string) => `file:///${d.toLowerCase()}:`);
}

/**
 * A set of loaded schema documents, and the resolver the walk uses over
 * them: document by URI, or by a $id any of them declares; then the JSON
 * pointer of the fragment.
 */
export class SchemaSet {
  private readonly docs = new Map<string, unknown>();
  private readonly ids = new Map<string, { node: unknown; base: string }>();

  add(uri: string, doc: unknown) {
    const key = schemaKey(uri);
    this.docs.set(key, doc);
    const index = (n: unknown) => {
      if (Array.isArray(n)) n.forEach(index);
      else if (isObj(n)) {
        if (typeof n.$id === 'string' && !n.$id.startsWith('#')) {
          const abs = resolveUri(n.$id, uri);
          if (abs) this.ids.set(schemaKey(abs), { node: n, base: uri });
          this.ids.set(n.$id, { node: n, base: uri });
        }
        for (const v of Object.values(n)) index(v);
      }
    };
    index(doc);
  }

  has(uri: string): boolean {
    return this.docs.has(schemaKey(uri));
  }

  readonly resolve: ResolveRef = (ref, base) => {
    const hash = ref.indexOf('#');
    const file = hash < 0 ? ref : ref.slice(0, hash);
    const frag = hash < 0 ? '' : ref.slice(hash + 1);
    let doc: unknown;
    let docBase = base;
    let docKey = schemaKey(base);
    if (!file) doc = this.docs.get(docKey);
    else {
      const abs = resolveUri(file, base);
      const byId = this.ids.get(file) ?? (abs ? this.ids.get(schemaKey(abs)) : undefined);
      if (abs && this.docs.has(schemaKey(abs))) {
        docKey = schemaKey(abs);
        doc = this.docs.get(docKey);
        docBase = abs;
      } else if (byId) {
        // Several $id nodes can share a document: the key is the id.
        docKey = `$id:${file}`;
        doc = byId.node;
        docBase = byId.base;
      }
    }
    if (doc === undefined) return undefined;
    let node: unknown = doc;
    if (frag.startsWith('/')) {
      for (const raw of frag.slice(1).split('/')) {
        let s = raw;
        try {
          s = decodeURIComponent(raw);
        } catch {
          // As written.
        }
        s = s.replace(/~1/g, '/').replace(/~0/g, '~');
        node = isObj(node) || Array.isArray(node) ? (node as Record<string, unknown>)[s] : undefined;
        if (node === undefined) return undefined;
      }
    } else if (frag) {
      // A named anchor: not used by any Bedrock schema set.
      return undefined;
    }
    return { node, base: docBase, id: `${docKey}#${frag}` };
  };
}
