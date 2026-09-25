// Molang inside pack JSON, found by path.
//
// A pack file is identified by its ROOT KEY -- `animation_controllers`,
// `minecraft:client_entity` -- not by the folder it sits in, because packs
// are laid out every way there is (regolith's packs/RP, jsonte output, a
// flat test folder). Under a root key, a list of path patterns names the
// strings that are Molang, each with the kind of Molang it is.
//
// The catalogue of those paths is data, in the format of
// data/molang-paths.json (see its "pattern" section): patterns are relative
// to the value of the root key, "/"-separated, with wildcards
//
//   key      an object member with that name (`~1` for `/`, `~0` for `~`)
//   *        any object member
//   [*]      any array element ([] is the same)
//   [N]      array element N
//   **       any number of levels, including none
//
// This is the shape a curated catalogue takes and the shape a walk over a
// JSON schema produces, so both can feed this one provider. When the
// extension ships no catalogue file, the built-in entries below -- animation
// controllers only -- keep the seam working.
//
// What each entry adds to "this string is Molang":
//
//   - kind: which Molang (see regions.ts), which decides how it is read.
//   - accepts: the JSON types the field takes. Only strings are source;
//     a number or boolean there is a literal, and "object" is Mojang's
//     {"expression": "...", "version": N} form, whose expression is the
//     Molang.
//   - target "key": the object's keys are Molang names (variable names
//     being declared), not expressions. Not analysed as expressions.
//   - joined: the game concatenates the array's strings, in order, into one
//     program -- vanilla opens a block in one element and closes it several
//     elements later -- so they are one region, never one per element.
//   - formatVersion: the document's format_version range the path holds in.
//
// And per kind: an event_response may be a /command or an @s event instead
// of Molang, and a string_or_molang field may be a plain string ("unrolled")
// rather than an expression; neither is analysed then.

import { parseTree, type Node } from 'jsonc-parser';
import type { AnalyzeOptions } from './bridge';
import { decodeJsonString, rawToValue, valueToRaw, type DecodedJsonString } from './jsonString';
import { kindOptions, type MolangKind, type MolangRegion, type MolangRegionProvider, type RegionSource } from './regions';

export interface PathEntry {
  path: string;
  kind: string;
  accepts?: string[];
  target?: 'value' | 'key';
  joined?: boolean;
  pack?: string;
  formatVersion?: { min?: string; max?: string };
}

export interface FileType {
  rootKey: string;
  name?: string;
  pack?: string;
  paths: PathEntry[];
}

export interface PathCatalogue {
  version?: number;
  fileTypes: FileType[];
}

/** The fallback: animation controllers, as the full catalogue lists them. */
export const builtinPaths: PathCatalogue = {
  version: 1,
  fileTypes: [
    {
      rootKey: 'animation_controllers',
      paths: [
        { path: '*/states/*/animations/[*]/*', kind: 'number', accepts: ['string', 'number'] },
        { path: '*/states/*/on_entry/[*]', kind: 'event_response', accepts: ['string'] },
        { path: '*/states/*/on_exit/[*]', kind: 'event_response', accepts: ['string'] },
        { path: '*/states/*/particle_effects/[*]/pre_effect_script', kind: 'general', accepts: ['string'] },
        { path: '*/states/*/transitions/[*]/*', kind: 'boolean', accepts: ['string', 'number'] },
        { path: '*/states/*/variables/*/input', kind: 'number', accepts: ['string', 'number'] },
      ],
    },
  ],
};

type Segment =
  | { t: 'key'; key: string }
  | { t: 'anyKey' }
  | { t: 'anyIndex' }
  | { t: 'index'; index: number }
  | { t: 'deep' };

export function compilePattern(path: string): Segment[] {
  return path
    .split('/')
    .filter((s) => s !== '')
    .map((s): Segment => {
      if (s === '**') return { t: 'deep' };
      if (s === '*') return { t: 'anyKey' };
      if (s === '[]' || s === '[*]') return { t: 'anyIndex' };
      const m = /^\[(\d+)\]$/.exec(s);
      if (m) return { t: 'index', index: Number(m[1]) };
      return { t: 'key', key: s.replace(/~1/g, '/').replace(/~0/g, '~') };
    });
}

type PathPart = string | number;

export function matchPattern(pattern: readonly Segment[], path: readonly PathPart[]): boolean {
  const go = (pi: number, xi: number): boolean => {
    if (pi === pattern.length) return xi === path.length;
    const seg = pattern[pi];
    if (seg.t === 'deep') {
      for (let k = xi; k <= path.length; k++) if (go(pi + 1, k)) return true;
      return false;
    }
    if (xi === path.length) return false;
    const part = path[xi];
    switch (seg.t) {
      case 'key':
        return typeof part === 'string' && part === seg.key && go(pi + 1, xi + 1);
      case 'anyKey':
        return typeof part === 'string' && go(pi + 1, xi + 1);
      case 'anyIndex':
        return typeof part === 'number' && go(pi + 1, xi + 1);
      case 'index':
        return part === seg.index && go(pi + 1, xi + 1);
    }
  };
  return go(0, 0);
}

/** Whether an event_response string is a command or an event, not Molang. */
export function isCommandOrEvent(value: string): boolean {
  const v = value.trimStart();
  return v.startsWith('/') || /^@s\s/.test(v);
}

/**
 * Whether a string_or_molang value is an expression rather than a plain
 * string such as an enum value ("unrolled") or an identifier
 * ("minecraft:pig"): it names something in a Molang namespace, or uses an
 * operator or bracket no plain value has.
 */
export function looksLikeMolang(value: string): boolean {
  return (
    /\b(?:query|q|variable|v|temp|t|context|c|math|array|geometry|material|texture)\.[A-Za-z_]/i.test(value) ||
    /[()?=<>!&|+*/;'[\]{}]/.test(value)
  );
}

/** The analysis options for kind at path, where the path narrows the kind. */
export function optionsFor(kind: string, path: string): AnalyzeOptions | undefined {
  if (kind === 'string_or_molang') {
    // The two string_or_molang fields each resolve a fixed list of queries.
    if (/(^|\/)properties\/[^/]+\/default$/.test(path)) return kindOptions.property_default;
    if (/(^|\/)set_property(\/|$)/.test(path)) return kindOptions.set_property;
    return kindOptions.general;
  }
  return kindOptions[kind as MolangKind];
}

export const JSON_LANGUAGE_IDS = ['json', 'jsonc', 'json5'];

interface CompiledEntry extends PathEntry {
  segments: Segment[];
}

interface CompiledType {
  rootKey: string;
  entries: CompiledEntry[];
}

export class JsonPathProvider implements MolangRegionProvider {
  readonly id = 'json-path';
  private readonly byRoot = new Map<string, CompiledType>();

  constructor(catalogue: PathCatalogue = builtinPaths) {
    for (const ft of catalogue.fileTypes) {
      const entries = ft.paths
        // A key names a variable being declared; it is not an expression.
        .filter((e) => e.target !== 'key')
        .map((e) => ({ ...e, segments: compilePattern(e.path) }));
      const existing = this.byRoot.get(ft.rootKey);
      if (existing) existing.entries.push(...entries);
      else this.byRoot.set(ft.rootKey, { rootKey: ft.rootKey, entries });
    }
  }

  provideRegions(doc: RegionSource): MolangRegion[] | undefined {
    if (!JSON_LANGUAGE_IDS.includes(doc.languageId)) return undefined;
    const text = doc.getText();
    // Cheap test first: most JSON a user opens is not a pack file at all.
    let mentioned = false;
    for (const root of this.byRoot.keys()) {
      if (text.includes(`"${root}"`)) {
        mentioned = true;
        break;
      }
    }
    if (!mentioned) return undefined;
    const tree = parseTree(text, [], { allowTrailingComma: true, disallowComments: false });
    if (!tree || tree.type !== 'object') return undefined;
    let formatVersion: string | undefined;
    const roots: { type: CompiledType; node: Node }[] = [];
    for (const prop of tree.children ?? []) {
      const [k, v] = prop.children ?? [];
      if (!k || !v) continue;
      if (k.value === 'format_version' && typeof v.value === 'string') formatVersion = v.value;
      const type = typeof k.value === 'string' ? this.byRoot.get(k.value) : undefined;
      if (type) roots.push({ type, node: v });
    }
    if (roots.length === 0) return undefined;

    // Regions in document order. A joined array is one entry, placed where
    // its first string is, gathering its strings as the walk meets them.
    type Group = { entry: CompiledEntry; label: string; parts: Node[] };
    const items: ({ region: MolangRegion } | { group: Group })[] = [];
    const groups = new Map<Node, Group>();
    for (const { type, node } of roots) {
      const entries = type.entries.filter((e) => inVersion(e, formatVersion));
      walk(node, [], (n, path) => {
        const entry = entries.find((e) => matchPattern(e.segments, path));
        if (!entry) return;
        const str = sourceNode(n, entry);
        if (!str) return;
        const label = `${type.rootKey}/${path.map((p) => (typeof p === 'number' ? `[${p}]` : p)).join('/')}`;
        if (entry.joined && n.parent?.type === 'array') {
          let group = groups.get(n.parent);
          if (!group) {
            group = { entry, label: label.replace(/\/\[\d+\]$/, ''), parts: [] };
            groups.set(n.parent, group);
            items.push({ group });
          }
          group.parts.push(str);
          return;
        }
        const region = stringRegion(text, str, entry, label);
        if (region) items.push({ region });
      });
    }
    const regions: MolangRegion[] = [];
    for (const item of items) {
      const region = 'region' in item ? item.region : joinedRegion(text, item.group.parts, item.group.entry, item.group.label);
      if (region) regions.push(region);
    }
    return regions;
  }
}

/** Whether entry holds for a document at formatVersion. */
function inVersion(entry: PathEntry, formatVersion: string | undefined): boolean {
  const range = entry.formatVersion;
  if (!range || !formatVersion) return true;
  if (range.min && compareVersions(formatVersion, range.min) < 0) return false;
  if (range.max && compareVersions(formatVersion, range.max) >= 0) return false;
  return true;
}

export function compareVersions(a: string, b: string): number {
  const pa = a.split('.').map((x) => parseInt(x, 10) || 0);
  const pb = b.split('.').map((x) => parseInt(x, 10) || 0);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const d = (pa[i] ?? 0) - (pb[i] ?? 0);
    if (d) return Math.sign(d);
  }
  return 0;
}

/** The string node holding the Molang at a matched node, if there is one. */
function sourceNode(node: Node, entry: PathEntry): Node | undefined {
  if (node.type === 'string') return node;
  if (node.type === 'object' && (entry.accepts ?? ['string']).includes('object')) {
    const expr = node.children?.find((p) => p.children?.[0]?.value === 'expression')?.children?.[1];
    if (expr?.type === 'string') return expr;
  }
  // A number or boolean where Molang is accepted is a literal: nothing to
  // analyse.
  return undefined;
}

function walk(node: Node, path: PathPart[], visit: (node: Node, path: PathPart[]) => void) {
  visit(node, path);
  if (node.type === 'object') {
    for (const prop of node.children ?? []) {
      const [k, v] = prop.children ?? [];
      if (k && v && typeof k.value === 'string') walk(v, [...path, k.value], visit);
    }
  } else if (node.type === 'array') {
    (node.children ?? []).forEach((child, i) => walk(child, [...path, i], visit));
  }
}

interface Piece {
  /** Offsets of this string's content in the region text and the document. */
  valueStart: number;
  valueEnd: number;
  hostStart: number;
  hostEnd: number;
  decoded: DecodedJsonString;
}

function decodePiece(text: string, node: Node, valueStart: number): Piece {
  // node.offset/length include the quotes.
  const hostStart = node.offset + 1;
  const hostEnd = Math.max(hostStart, node.offset + node.length - 1);
  const decoded = decodeJsonString(text.slice(hostStart, hostEnd));
  return { valueStart, valueEnd: valueStart + decoded.value.length, hostStart, hostEnd, decoded };
}

/** Whether a string at this entry is Molang to analyse. */
function isMolangValue(value: string, entry: PathEntry): boolean {
  if (entry.kind === 'event_response' && isCommandOrEvent(value)) return false;
  if (entry.kind === 'string_or_molang' && !looksLikeMolang(value)) return false;
  return true;
}

function stringRegion(text: string, node: Node, entry: PathEntry, label: string): MolangRegion | undefined {
  const options = optionsFor(entry.kind, entry.path);
  if (!options) return undefined;
  const piece = decodePiece(text, node, 0);
  if (!isMolangValue(piece.decoded.value, entry)) return undefined;
  return piecesRegion([piece], piece.decoded.value, entry.kind as MolangKind, options, label);
}

/**
 * One region over the strings of a joined array. Their values are joined
 * with a line end between them, which the region maps to the end of the
 * string before it: whitespace, so tokens in neighbouring strings cannot
 * run together, and a position no diagnostic lands in on its own.
 */
function joinedRegion(text: string, parts: Node[], entry: PathEntry, label: string): MolangRegion | undefined {
  const options = optionsFor(entry.kind, entry.path);
  if (!options || parts.length === 0) return undefined;
  const pieces: Piece[] = [];
  let value = '';
  for (const node of parts) {
    if (pieces.length) value += '\n';
    const piece = decodePiece(text, node, value.length);
    pieces.push(piece);
    value += piece.decoded.value;
  }
  return piecesRegion(pieces, value, entry.kind as MolangKind, options, label);
}

function piecesRegion(pieces: Piece[], value: string, kind: MolangKind, options: AnalyzeOptions, label: string): MolangRegion {
  const first = pieces[0];
  const last = pieces[pieces.length - 1];
  return {
    text: value,
    kind,
    options,
    hostStart: first.hostStart,
    hostEnd: last.hostEnd,
    toHost: (o) => {
      let p = first;
      for (const q of pieces) if (q.valueStart <= o) p = q;
      if (o > p.valueEnd) return p.hostEnd;
      return p.hostStart + valueToRaw(p.decoded, o - p.valueStart);
    },
    fromHost: (h) => {
      for (const p of pieces) {
        if (h >= p.hostStart && h <= p.hostEnd) return p.valueStart + rawToValue(p.decoded, h - p.hostStart);
      }
      return undefined;
    },
    inert: [],
    label,
  };
}
