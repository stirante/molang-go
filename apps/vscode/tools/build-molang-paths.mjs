#!/usr/bin/env node
// Builds apps/vscode/data/molang-paths.json: where Molang is written inside
// Bedrock add-on JSON, per file type, with the kind of Molang each place takes.
//
// Three inputs, merged in this order:
//
//   1. The Blockception schemas (what VS Code users get from the Blockception
//      extension). A node is Molang when it has `"format": "molang"` or a
//      title starting "Molang" ("Molang Number", "Molang Boolean",
//      "Molang Color"). Weaker, and recorded as such in the sources file: a
//      string-typed node with no marker whose description calls it Molang
//      ("A Molang expression defining ...").
//   2. Mojang's published JSON schemas (bedrock-samples, metadata/json_schemas).
//      A node is Molang when it `$ref`s `Expression Node.json`,
//      `Molang string.json` or `Molang Expression.json`.
//   3. apps/vscode/data/molang-paths.overrides.json, hand-curated, applied last:
//      it adds what no schema marks, removes what a schema marks wrongly, and
//      patches kinds and accepted types.
//
// Usage (from anywhere):
//
//   node apps/vscode/tools/build-molang-paths.mjs \
//     [--blockception <dir with behavior/ and resource/>] \
//     [--mojang <bedrock-samples/metadata/json_schemas>] \
//     [--out <file>] [--sources <file>] [--raw <file>]
//
// Without --blockception the newest installed Blockception extension under
// ~/.vscode/extensions is used. Without --mojang, $BEDROCK_SAMPLES is tried.
// A missing input is skipped with a warning, so the catalogue can be rebuilt
// from either schema set alone; the overrides always apply.

import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const dataDir = path.resolve(here, '..', 'data');

// ---------------------------------------------------------------------------
// arguments

function parseArgs(argv) {
  const a = {};
  for (let i = 0; i < argv.length; i++) {
    const k = argv[i];
    if (!k.startsWith('--')) throw new Error(`unexpected argument ${k}`);
    a[k.slice(2)] = argv[++i];
  }
  return a;
}
const args = parseArgs(process.argv.slice(2));

function findBlockception() {
  const ext = path.join(os.homedir(), '.vscode', 'extensions');
  if (!fs.existsSync(ext)) return null;
  const dirs = fs.readdirSync(ext)
    .filter((d) => d.startsWith('blockceptionltd.blockceptionvscodeminecraftbedrockdevelopmentextension-'))
    .sort((x, y) => cmpVersion(x.split('-').pop(), y.split('-').pop()));
  if (!dirs.length) return null;
  return path.join(ext, dirs[dirs.length - 1], 'minecraft-bedrock-schemas');
}

function findMojang() {
  const s = process.env.BEDROCK_SAMPLES;
  if (!s) return null;
  const p = path.join(s, 'metadata', 'json_schemas');
  return fs.existsSync(p) ? p : null;
}

const bcDir = args.blockception ?? findBlockception();
const mjDir = args.mojang ?? findMojang();
const outFile = args.out ?? path.join(dataDir, 'molang-paths.json');
const sourcesFile = args.sources ?? path.join(dataDir, 'molang-paths.sources.md');
const overridesFile = path.join(dataDir, 'molang-paths.overrides.json');

// ---------------------------------------------------------------------------
// helpers

function cmpVersion(a, b) {
  const pa = String(a).split('.').map((n) => parseInt(n, 10));
  const pb = String(b).split('.').map((n) => parseInt(n, 10));
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = pa[i] ?? 0, y = pb[i] ?? 0;
    if (Number.isNaN(x) || Number.isNaN(y)) return String(a).localeCompare(String(b));
    if (x !== y) return x - y;
  }
  return 0;
}

function listJson(dir) {
  const out = [];
  (function walk(d) {
    for (const e of fs.readdirSync(d, { withFileTypes: true })) {
      const p = path.join(d, e.name);
      if (e.isDirectory()) walk(p);
      else if (e.name.endsWith('.json')) out.push(p);
    }
  })(dir);
  return out.sort();
}

// A path segment in the catalogue's pattern language: literal keys escape
// `~` and `/` the JSON-pointer way.
function escSeg(k) { return k.replace(/~/g, '~0').replace(/\//g, '~1'); }

const readJson = (f) => JSON.parse(fs.readFileSync(f, 'utf8'));

// ---------------------------------------------------------------------------
// the schema walker
//
// Walks a JSON schema from a root node, following properties,
// patternProperties, additionalProperties, items (single and tuple),
// prefixItems, $ref, anyOf/oneOf/allOf and if/then/else. Every node the
// `marker` callback recognises is recorded with the instance path that reaches
// it and is not descended into. The union of all branches is taken: that
// over-approximates when branches are mutually exclusive, which is the right
// error for "may this string be Molang".

const MAX_DEPTH = 40;

//
// A definition reached again while it is still being expanded (a recursive
// schema: nested filters, event sequences) is not expanded a second time, so
// recursive structures contribute one level of paths; the overrides spell out
// the recursive ones with `**`. Each definition's relative hits are memoised,
// which keeps the walk linear in the size of the schema set.

function walkSchema({ rootNode, rootFile, resolveRef, marker, onHit }) {
  const onStack = new Set();
  const memo = new Map();
  const sibKey = (sib) => (sib ? [...sib].sort().join(',') : '');
  // Returns [{path: [...relative segments], m, file}].
  function collect(node, file, sib, depth) {
    if (!node || typeof node !== 'object' || Array.isArray(node) || depth > MAX_DEPTH) return [];
    const m = marker(node, file, sib);
    if (m) return [{ path: [], m, file }];
    const out = [];
    const under = (seg, hits) => { for (const h of hits) out.push({ ...h, path: [seg, ...h.path] }); };
    if (node.$ref) {
      const r = resolveRef(node.$ref, file);
      if (r) {
        const mm = marker(r.node, r.file, sib, node.$ref);
        if (mm) out.push({ path: [], m: mm, file: r.file });
        else {
          const id = r.file + '#' + r.id + '|' + sibKey(sib);
          if (memo.has(id)) out.push(...memo.get(id));
          else if (!onStack.has(id)) {
            onStack.add(id);
            const res = collect(r.node, r.file, sib, depth + 1);
            onStack.delete(id);
            memo.set(id, res);
            out.push(...res);
          }
        }
      }
    }
    for (const comb of ['anyOf', 'oneOf']) {
      if (Array.isArray(node[comb])) {
        const types = node[comb].map(branchType);
        node[comb].forEach((b, i) => {
          const others = new Set(types.filter((_, j) => j !== i).flat());
          out.push(...collect(b, file, others, depth + 1));
        });
      }
    }
    if (Array.isArray(node.allOf)) node.allOf.forEach((b) => out.push(...collect(b, file, sib, depth + 1)));
    for (const k of ['if', 'then', 'else']) if (node[k]) out.push(...collect(node[k], file, sib, depth + 1));
    if (node.properties) {
      for (const [k, v] of Object.entries(node.properties)) under(escSeg(k), collect(v, file, null, depth + 1));
    }
    if (node.patternProperties) {
      for (const v of Object.values(node.patternProperties)) under('*', collect(v, file, null, depth + 1));
    }
    if (node.additionalProperties && typeof node.additionalProperties === 'object') {
      under('*', collect(node.additionalProperties, file, null, depth + 1));
    }
    if (Array.isArray(node.prefixItems)) node.prefixItems.forEach((it, i) => under(`[${i}]`, collect(it, file, null, depth + 1)));
    if (Array.isArray(node.items)) node.items.forEach((it, i) => under(`[${i}]`, collect(it, file, null, depth + 1)));
    else if (node.items && typeof node.items === 'object') under('[*]', collect(node.items, file, null, depth + 1));
    if (node.additionalItems && typeof node.additionalItems === 'object') under('[*]', collect(node.additionalItems, file, null, depth + 1));
    // Deduplicate: branches of one union often lead to the same place.
    const seen = new Set();
    return out.filter((h) => {
      const k = h.path.join('/') + '|' + JSON.stringify(h.m);
      if (seen.has(k)) return false;
      seen.add(k);
      return true;
    });
  }
  for (const h of collect(rootNode, rootFile, null, 0)) onHit(h.path, h.m, h.file);
}

function branchType(b) {
  if (!b || typeof b !== 'object') return [];
  if (typeof b.type === 'string') return [b.type];
  if (Array.isArray(b.type)) return b.type;
  return [];
}

// ---------------------------------------------------------------------------
// source 1: Blockception bundles (one self-contained file per document type,
// refs rewritten to #/definitions/X)

function blockceptionHits(dir) {
  const hits = [];
  for (const file of listJson(dir)) {
    const doc = readJson(file);
    const rel = path.relative(dir, file).replace(/\\/g, '/');
    const pack = rel.startsWith('behavior/') ? 'behavior' : rel.startsWith('resource/') ? 'resource' : null;
    if (!pack) continue;
    const resolveRef = (ref, f) => {
      if (!ref.startsWith('#/')) return null;
      let n = doc;
      for (const s of ref.slice(2).split('/')) n = n?.[decodeURIComponent(s).replace(/~1/g, '/').replace(/~0/g, '~')];
      return n ? { node: n, file: f, id: ref } : null;
    };
    const marker = (node, _f, sib) => {
      let title = typeof node.title === 'string' ? node.title : '';
      if (node.format !== 'molang' && !/^Molang( |$)/.test(title)) {
        // Weaker signal: a string-typed field whose description calls it
        // Molang but that carries no marker.
        if (!describedAsMolang(node)) return null;
        title = 'described';
      }
      const accepts = new Set(['string']);
      const alts = [...branchesOf(node), ...(sib ? [...sib] : [])];
      for (const t of alts) {
        if (t === 'number' || t === 'integer') accepts.add('number');
        if (t === 'boolean') accepts.add('boolean');
      }
      if (title === 'Molang Number') accepts.add('number');
      if (title === 'Molang Boolean') accepts.add('boolean');
      return { title: title || 'Molang', accepts: [...accepts] };
    };
    walkSchema({
      rootNode: doc, rootFile: rel, resolveRef, marker,
      onHit: (p, m) => {
        if (p.length < 1) return;
        hits.push({ source: 'blockception', schema: rel, pack, rootKey: unesc(p[0]), path: p.slice(1), marker: m });
      },
    });
  }
  return hits;
}

// "A Molang expression defining ...", "Can be a number or a Molang
// expression", "... in Molang". Descriptions that only mention Molang in
// passing (a variable NAME, "set a Molang variable") do not count.
function describedAsMolang(node) {
  const d = typeof node.description === 'string' ? node.description : '';
  if (!/\bmolang\b/i.test(d)) return false;
  if (/molang (variable|query) name|name of the molang|molang variable to|molang variables? (that|which) |sets? .*molang variable/i.test(d)) return false;
  if (node.properties || node.items) return false;
  return branchesOf(node).includes('string');
}

function branchesOf(node) {
  const out = [];
  for (const c of ['anyOf', 'oneOf']) if (Array.isArray(node[c])) node[c].forEach((b) => out.push(...branchType(b)));
  out.push(...branchType(node));
  return out;
}

function unesc(s) { return s.replace(/~1/g, '/').replace(/~0/g, '~'); }

// ---------------------------------------------------------------------------
// source 2: Mojang's schemas (many files, relative percent-encoded $refs,
// one directory per format version)

// Document roots, and the root key their content sits under. `null` means the
// document itself carries the root key as a property.
const MOJANG_ROOTS = [
  { re: /^client\/biome\/([^/]+)\/Client Biome Document\.json$/, rootKey: null, pack: 'resource' },
  { re: /^client\/block\/([^/]+)\/Culling\.json$/, rootKey: 'minecraft:block_culling_rules', pack: 'resource' },
  { re: /^client\/entity\/(beta)\/AtomicClientEntityDocument\.json$/, rootKey: 'minecraft:client_entity', pack: 'resource' },
  { re: /^client\/particles\/([^/]+)\/Particle Effect Data\.json$/, rootKey: 'particle_effect', pack: 'resource' },
  { re: /^client_server\/dimension\/([^/]+)\/Dimensions\.json$/, rootKey: null, pack: 'behavior' },
  { re: /^client_server\/spawn\/([^/]+)\/Spawn Rules\.json$/, rootKey: 'minecraft:spawn_rules', pack: 'behavior' },
  { re: /^server\/spawn\/([^/]+)\/Spawn Rules\.json$/, rootKey: 'minecraft:spawn_rules', pack: 'behavior' },
  { re: /^client_server\/world\/([^/]+)\/Feature Rule\.json$/, rootKey: 'minecraft:feature_rules', pack: 'behavior' },
  { re: /^client_server\/world\/([^/]+)\/Processor List\.json$/, rootKey: 'minecraft:processor_list', pack: 'behavior' },
  { re: /^client_server\/world\/([^/]+)\/Structure Set\.json$/, rootKey: 'minecraft:structure_set', pack: 'behavior' },
  { re: /^client_server\/world\/([^/]+)\/JigsawStructure\.json$/, rootKey: 'minecraft:jigsaw', pack: 'behavior' },
  { re: /^server\/world\/([^/]+)\/Template Pool\.json$/, rootKey: 'minecraft:template_pool', pack: 'behavior' },
  { re: /^client_server\/structure\/([^/]+)\/Jigsaw Structure Metadata( File)?\.json$/, rootKey: null, pack: 'behavior' },
  { re: /^server\/biome\/([^/]+)\/Biome Document\.json$/, rootKey: null, pack: 'behavior' },
  { re: /^server\/block\/([^/]+)\/Blocks\.json$/, rootKey: 'minecraft:block', pack: 'behavior' },
  { re: /^server\/entity\/([^/]+)\/ActorDocument\.json$/, rootKey: 'minecraft:entity', pack: 'behavior' },
  { re: /^server\/item\/([^/]+)\/ItemDocument\.json$/, rootKey: 'minecraft:item', pack: 'behavior' },
  { re: /^server\/crafting_catalog\/([^/]+)\/Crafting Catalog Document\.json$/, rootKey: 'minecraft:crafting_items_catalog', pack: 'behavior' },
  { re: /^server\/voxel_shapes\/([^/]+)\/VoxelShapeFile\.json$/, rootKey: 'minecraft:voxel_shape', pack: 'behavior' },
  { re: /^server\/world\/([^/]+)\/PoiDocment\.json$/, rootKey: 'minecraft:poi_block', pack: 'behavior' },
  { re: /^server\/sound\/([^/]+)\/Server Sound Definition Document\.json$/, rootKey: null, pack: 'behavior' },
  { re: /^client_server\/entity\/([^/]+)\/Trade Table\.json$/, rootKey: null, pack: 'behavior' },
];

const MOJANG_MARKERS = {
  'Expression Node.json': { title: 'Molang expression', accepts: ['string', 'number', 'object'] },
  'Molang string.json': { title: 'Molang string', accepts: ['string', 'object'] },
  'Molang Expression.json': { title: 'Molang Expression', accepts: ['string', 'number', 'boolean'] },
};

function mojangHits(dir) {
  const hits = [];
  const cache = new Map();
  const load = (f) => { if (!cache.has(f)) cache.set(f, readJson(path.join(dir, f))); return cache.get(f); };
  const resolveRef = (ref, fromRel) => {
    const [filePart, frag] = ref.split('#');
    const target = filePart
      ? path.posix.normalize(path.posix.join(path.posix.dirname(fromRel), decodeURIComponent(filePart)))
      : fromRel;
    if (!fs.existsSync(path.join(dir, target))) return null;
    let n = load(target);
    if (frag) for (const s of frag.replace(/^\//, '').split('/').filter(Boolean)) n = n?.[unesc(decodeURIComponent(s))];
    return n ? { node: n, file: target, id: frag || '' } : null;
  };
  const marker = (node, file, _sib, viaRef) => {
    if (viaRef) {
      const base = decodeURIComponent(viaRef.split('#')[0].split('/').pop());
      if (MOJANG_MARKERS[base]) return MOJANG_MARKERS[base];
    }
    const base = path.posix.basename(file);
    if (node === cache.get(file) && MOJANG_MARKERS[base]) return MOJANG_MARKERS[base];
    return null;
  };
  const files = listJson(dir).map((f) => path.relative(dir, f).replace(/\\/g, '/'));
  for (const rel of files) {
    const root = MOJANG_ROOTS.find((r) => r.re.test(rel));
    if (!root) continue;
    const version = rel.match(root.re)[1];
    const doc = load(rel);
    walkSchema({
      rootNode: doc, rootFile: rel, resolveRef, marker,
      onHit: (p, m) => {
        let rootKey = root.rootKey, rest = p;
        if (rootKey === null) {
          if (!p.length) return;
          rootKey = unesc(p[0]); rest = p.slice(1);
        }
        hits.push({ source: 'mojang', schema: rel, version, pack: root.pack, rootKey, path: rest, marker: m });
      },
    });
  }
  return hits;
}

// ---------------------------------------------------------------------------
// file types
//
// A document is identified by its root key: the one top-level key besides
// `format_version`. Features have one root key per feature type.

const FEATURE_KEYS = [
  'minecraft:aggregate_feature', 'minecraft:beards_and_shavers_feature', 'minecraft:cave_carver_feature',
  'minecraft:conditional_list', 'minecraft:fossil_feature', 'minecraft:geode_feature',
  'minecraft:growing_plant_feature', 'minecraft:hell_cave_carver_feature', 'minecraft:multi_block_feature', 'minecraft:multiface_feature',
  'minecraft:nether_cave_carver_feature', 'minecraft:ore_feature', 'minecraft:partially_exposed_blob_feature',
  'minecraft:rect_layout', 'minecraft:scan_surface', 'minecraft:scatter_feature', 'minecraft:search_feature',
  'minecraft:sculk_patch_feature', 'minecraft:sequence_feature', 'minecraft:single_block_feature',
  'minecraft:snap_to_surface_feature', 'minecraft:structure_template_feature', 'minecraft:surface_relative_threshold_feature',
  'minecraft:tree_feature', 'minecraft:underwater_cave_carver_feature', 'minecraft:vegetation_patch_feature',
  'minecraft:weighted_random_feature',
];

const FILE_TYPES = {
  'minecraft:client_entity': { pack: 'resource', name: 'Client entity' },
  'minecraft:attachable': { pack: 'resource', name: 'Attachable' },
  'render_controllers': { pack: 'resource', name: 'Render controllers' },
  'animations': { pack: 'both', name: 'Animations', note: 'RP actor animations and BP entity animations share the root key; RP-only paths are marked pack "resource".' },
  'animation_controllers': { pack: 'both', name: 'Animation controllers', note: 'RP and BP share the root key; paths only one side accepts are marked with their pack.' },
  'particle_effect': { pack: 'resource', name: 'Particle effect' },
  'minecraft:geometry': { pack: 'resource', name: 'Geometry' },
  'minecraft:entity': { pack: 'behavior', name: 'Entity' },
  'minecraft:block': { pack: 'behavior', name: 'Block' },
  'minecraft:item': { pack: 'behavior', name: 'Item' },
  'minecraft:feature_rules': { pack: 'behavior', name: 'Feature rule' },
  'minecraft:biome': { pack: 'behavior', name: 'Biome' },
  'minecraft:processor_list': { pack: 'behavior', name: 'Processor list' },
  'minecraft:spawn_rules': { pack: 'behavior', name: 'Spawn rules' },
  'minecraft:client_biome': { pack: 'resource', name: 'Client biome' },
  'minecraft:voxel_shape': { pack: 'behavior', name: 'Voxel shape' },
  'minecraft:jigsaw': { pack: 'behavior', name: 'Jigsaw structure' },
  'minecraft:template_pool': { pack: 'behavior', name: 'Template pool' },
  'minecraft:structure_set': { pack: 'behavior', name: 'Structure set' },
  'entity_sounds': { pack: 'resource', name: 'Sounds (sounds.json, entity_sounds)', note: 'sounds.json carries several root keys (block_sounds, entity_sounds, individual_event_sounds, interactive_sounds); only entity_sounds holds Molang.' },
};
for (const k of FEATURE_KEYS) FILE_TYPES[k] = { pack: 'behavior', name: 'Feature (' + k.slice(10) + ')', group: 'feature' };

// Documents looked at and found to carry no Molang (listed in the sources
// file so the absence is a finding, not an oversight).
const NO_MOLANG = [
  ['minecraft:spawn_rules', 'conditions are filters and fixed-value components; no schema marks Molang, none in the 60 vanilla files'],
  ['loot tables (root key pools)', 'functions and conditions take fixed values and ranges'],
  ['trade tables (root key tiers)', 'fixed values and ranges'],
  ['minecraft:camera_preset', 'no schema marks Molang; not present in bedrock-samples'],
  ['minecraft:fog_settings', 'fixed values'],
  ['minecraft:client_biome', 'fixed values'],
  ['minecraft:atmosphere_settings, minecraft:lighting_settings, minecraft:color_grading_settings, minecraft:water_settings', 'keyframed fixed values (object keys are times, not Molang)'],
  ['minecraft:block_culling_rules, minecraft:dimension, minecraft:voxel_shape', 'fixed values'],
  ['recipes, item catalog, texture sets, flipbook/terrain/item texture lists, sound_definitions, music_definitions, UI', 'fixed values or resource names'],
  ['minecraft:jigsaw, minecraft:structure_set, minecraft:template_pool', 'fixed values; the block predicates of processor lists do carry tag filters and are catalogued'],
];

// ---------------------------------------------------------------------------
// kinds

const KINDS = {
  general: 'Any Molang: statements, assignments, queries of the owning context.',
  boolean: 'A condition; the result is read as true when non-zero.',
  number: 'A value; the result is read as a float.',
  color: 'One colour channel, 0..1.',
  render_resource_ref: 'A render-controller resource reference: Geometry.x, Material.x, Texture.x or Array.x[expr].',
  particle: 'Particle-effect Molang: the variable.emitter_* / variable.particle_* built-ins, the effect\'s own variables and curves; entity queries resolve only when the effect is attached to an entity.',
  block_state: 'Block-permutation Molang: only query.block_state / query.block_property and constants.',
  tag_filter: 'The "tags" member of a block or item descriptor: Molang over the described block\'s or item\'s tags (query.any_tag, query.all_tags, ...).',
  worldgen: 'World-generation Molang (features, feature rules, biomes): the worldgen query set, no side effects.',
  animation: 'Animation Molang evaluated per frame for the animated actor (query.anim_time, query.life_time, ...).',
  event_response: 'A timeline / on_entry / on_exit entry: Molang, or a slash command (`/...`), or an entity event (`@s event`), which are not Molang.',
  string_or_molang: 'Either a plain string value (an enum property value, a name) or a Molang expression.',
  variable_name: 'A Molang variable name used as an object key (variable.x / v.x), declared rather than evaluated.',
  array_name: 'A render-controller array name used as an object key (Array.x).',
};

// Assigns the kind for a schema-derived path. Paths are segment arrays below
// the root key. Overrides can still patch the result.
function classify(rootKey, p, marker) {
  const s = p.join('/');
  const last = p[p.length - 1];
  const ft = FILE_TYPES[rootKey];
  if (last === 'tags') return 'tag_filter';
  if (ft?.group === 'feature' || rootKey === 'minecraft:feature_rules' || rootKey === 'minecraft:biome') return 'worldgen';
  if (rootKey === 'minecraft:processor_list' || rootKey === 'minecraft:jigsaw' || rootKey === 'minecraft:template_pool' || rootKey === 'minecraft:structure_set') return 'worldgen';
  if (rootKey === 'particle_effect') return 'particle';
  if (rootKey === 'minecraft:block') {
    if (/bone_visibility/.test(s) || /^permutations\/\[\*\]\/condition$/.test(s)) return 'block_state';
  }
  if (rootKey === 'render_controllers') {
    if (/^\*\/(geometry|textures\/\[\*\]|materials\/\[\*\]\/\*)$/.test(s)) return 'render_resource_ref';
    if (/^\*\/(part_visibility\/\[\*\]\/\*)$/.test(s)) return 'boolean';
    if (/_color\/[rgba]$|^\*\/color\/[rgba]$/.test(s)) return 'color';
    return 'number';
  }
  if (rootKey === 'animations') {
    if (/\/timeline\//.test(s)) return 'event_response';
    if (/pre_effect_script/.test(s)) return 'general';
    return 'animation';
  }
  if (rootKey === 'animation_controllers') {
    if (/\/(on_entry|on_exit)\//.test(s)) return 'event_response';
    if (/\/transitions\//.test(s)) return 'boolean';
    if (/pre_effect_script/.test(s)) return 'general';
    return 'number';
  }
  if (rootKey === 'minecraft:client_entity' || rootKey === 'minecraft:attachable') {
    if (/^description\/scripts\/(initialize|pre_animation)\//.test(s)) return 'general';
  }
  if (marker.accepts.includes('boolean') && !marker.accepts.includes('number')) return 'boolean';
  if (marker.title === 'Molang Color') return 'color';
  if (marker.accepts.includes('number')) return 'number';
  return 'general';
}

// ---------------------------------------------------------------------------
// merge

function key(rootKey, p, target) { return `${rootKey}\u0000${p}\u0000${target ?? 'value'}`; }

const entries = new Map();
const provenance = new Map(); // key -> Set of source labels
const versions = new Map(); // key -> Set of mojang schema versions

function add(e, label, ver) {
  const k = key(e.rootKey, e.path, e.target);
  const cur = entries.get(k);
  if (!cur) entries.set(k, e);
  else {
    const acc = new Set([...(cur.accepts ?? []), ...(e.accepts ?? [])]);
    cur.accepts = orderAccepts([...acc]);
  }
  if (!provenance.has(k)) provenance.set(k, new Set());
  provenance.get(k).add(label);
  if (ver) { if (!versions.has(k)) versions.set(k, new Set()); versions.get(k).add(ver); }
}

function orderAccepts(a) {
  const order = ['string', 'number', 'boolean', 'object'];
  return order.filter((x) => a.includes(x));
}

const stats = { blockception: 0, mojang: 0 };
const skipped = new Map();

function ingest(hits) {
  for (const h of hits) {
    if (!FILE_TYPES[h.rootKey]) { skipped.set(h.rootKey, (skipped.get(h.rootKey) ?? 0) + 1); continue; }
    stats[h.source]++;
    const p = h.path.join('/');
    const kind = classify(h.rootKey, h.path, h.marker);
    const e = { rootKey: h.rootKey, path: p, kind, accepts: orderAccepts(h.marker.accepts), _title: h.marker.title };
    // Where a file type exists in both packs, remember which side the schema
    // came from; RP-only / BP-only is decided after all sources are in.
    e._packs = new Set([h.pack]);
    const k = key(h.rootKey, p);
    add(e, h.source === 'blockception' ? `${h.marker.title === 'described' ? 'blockception-described' : 'blockception'}:${h.schema}` : `mojang:${h.schema.replace(/\/[^/]+\/[^/]+$/, '')}`, h.source === 'mojang' ? h.version : null);
    entries.get(k)._packs.add(h.pack);
  }
}

if (bcDir && fs.existsSync(bcDir)) ingest(blockceptionHits(bcDir));
else console.warn('build-molang-paths: Blockception schemas not found; skipping (pass --blockception)');
if (mjDir && fs.existsSync(mjDir)) ingest(mojangHits(mjDir));
else console.warn('build-molang-paths: Mojang schemas not found; skipping (pass --mojang or set BEDROCK_SAMPLES)');

if (args.raw) {
  const raw = [...entries.entries()].map(([k, e]) => ({ ...e, title: e._title, _packs: [...e._packs], sources: [...provenance.get(k)], versions: [...(versions.get(k) ?? [])] }));
  fs.writeFileSync(args.raw, JSON.stringify({ skipped: Object.fromEntries(skipped), raw }, null, 1));
}

// ---------------------------------------------------------------------------
// overrides, applied last
//
//   "remove": [{rootKey, path, target?, reason}]              drop a schema-derived entry
//   "patch":  [{rootKey, path, target?, set: {...}, reason}]  change fields of entries
//   "add":    [{rootKey, path, kind, accepts?, target?, joined?, pack?, formatVersion?, note?, reason}]
//   "notMolang", "knownParseGaps": read by check-molang-paths.mjs only.
//
// Shorthands, expanded before anything is matched:
//   "rootKeys": [...] instead of "rootKey"; "#feature" stands for every feature root key.
//   "paths": [...] instead of "path".
//   A path starting "@components/" stands for both places a component can sit:
//     minecraft:entity  components/... and component_groups/*/...
//     minecraft:block   components/... and permutations/[*]/components/...
//   In remove/patch a path ending in "**" matches every entry under that prefix.

const overrides = readJson(overridesFile);

function expand(ov) {
  const rootKeys = (ov.rootKeys ?? [ov.rootKey]).flatMap((r) => (r === '#feature' ? FEATURE_KEYS : [r]));
  const out = [];
  for (const rootKey of rootKeys) {
    for (const p of ov.paths ?? [ov.path]) {
      let variants = [p];
      if (p.startsWith('@components/')) {
        const rest = p.slice('@components/'.length);
        if (rootKey === 'minecraft:entity') variants = [`components/${rest}`, `component_groups/*/${rest}`];
        else if (rootKey === 'minecraft:block') variants = [`components/${rest}`, `permutations/[*]/components/${rest}`];
        else throw new Error(`@components/ is not defined for ${rootKey}`);
      }
      for (const v of variants) out.push({ ...ov, rootKey, path: v });
    }
  }
  return out;
}

const matches = (ov, e) => {
  if (ov.rootKey !== e.rootKey) return false;
  if ((ov.target ?? 'value') !== (e.target ?? 'value')) return false;
  if (ov.path.endsWith('**')) return e.path.startsWith(ov.path.slice(0, -2));
  return ov.path === e.path;
};
const usedOverride = new Set();
for (const [i, group] of (overrides.remove ?? []).entries()) {
  for (const ov of expand(group)) {
    for (const [k, e] of entries) if (matches(ov, e)) { entries.delete(k); usedOverride.add('remove' + i); }
  }
}
for (const [i, group] of (overrides.patch ?? []).entries()) {
  for (const ov of expand(group)) {
    for (const [k, e] of entries) {
      if (!matches(ov, e)) continue;
      Object.assign(e, ov.set);
      provenance.get(k).add('curated');
      usedOverride.add('patch' + i);
    }
  }
}
for (const [i, group] of (overrides.add ?? []).entries()) {
  for (const ov of expand(group)) {
    const { reason, rootKeys, paths, ...rest } = ov;
    void reason; void rootKeys; void paths;
    const e = { ...rest, accepts: orderAccepts(ov.accepts ?? ['string']) };
    const k = key(e.rootKey, e.path, e.target);
    if (entries.has(k)) {
      // Already schema-marked: the override still wins on the fields it names.
      const cur = entries.get(k);
      Object.assign(cur, { ...e, accepts: orderAccepts([...new Set([...(cur.accepts ?? []), ...e.accepts])]) });
      provenance.get(k).add('curated');
    } else {
      e._packs = new Set();
      entries.set(k, e);
      provenance.set(k, new Set(['curated']));
    }
    usedOverride.add('add' + i);
  }
}
for (const [kind, list] of [['remove', overrides.remove], ['patch', overrides.patch], ['add', overrides.add]]) {
  (list ?? []).forEach((ov, i) => {
    if (!usedOverride.has(kind + i)) console.warn(`build-molang-paths: override ${kind}[${i}] (${ov.rootKey ?? ov.rootKeys} ${ov.path ?? ov.paths}) matched nothing`);
  });
}

// ---------------------------------------------------------------------------
// collapse: a tuple index path `a/[0]` is dropped when `a/[*]` exists with the
// same kind and target (Blockception spells vectors out per index, Mojang's
// schemas use one items schema); its provenance moves to the survivor.

for (const [k, e] of [...entries]) {
  if (!/\[\d+\]/.test(e.path)) continue;
  const general = e.path.replace(/\[\d+\]/g, '[*]');
  const gk = key(e.rootKey, general, e.target);
  const g = entries.get(gk);
  if (!g || g.kind !== e.kind) continue;
  g.accepts = orderAccepts([...new Set([...g.accepts, ...e.accepts])]);
  for (const s of provenance.get(k)) provenance.get(gk).add(s);
  for (const v of versions.get(k) ?? []) { if (!versions.has(gk)) versions.set(gk, new Set()); versions.get(gk).add(v); }
  if (e._packs && g._packs) for (const pk of e._packs) g._packs.add(pk);
  entries.delete(k);
}

// ---------------------------------------------------------------------------
// output

const byType = new Map();
for (const [k, e] of entries) {
  if (!byType.has(e.rootKey)) byType.set(e.rootKey, []);
  byType.get(e.rootKey).push([k, e]);
}

const segOrder = (a, b) => a.path.localeCompare(b.path) || (a.target ?? 'value').localeCompare(b.target ?? 'value');
const fileTypes = [];
for (const rootKey of Object.keys(FILE_TYPES)) {
  const list = byType.get(rootKey);
  if (!list?.length) continue;
  const ft = FILE_TYPES[rootKey];
  const paths = list.map(([, e]) => e).sort(segOrder).map((e) => {
    const o = { path: e.path };
    if (e.target && e.target !== 'value') o.target = e.target;
    o.kind = e.kind;
    if (o.target !== 'key') o.accepts = e.accepts;
    if (ft.pack === 'both') {
      const packs = e.pack ? new Set([e.pack]) : e._packs;
      if (packs.size === 1) o.pack = [...packs][0];
    }
    if (e.formatVersion) o.formatVersion = e.formatVersion;
    if (e.joined) o.joined = true;
    if (e.note) o.note = e.note;
    return o;
  });
  const t = { rootKey, name: ft.name, pack: ft.pack };
  if (ft.group) t.group = ft.group;
  if (ft.note) t.note = ft.note;
  t.paths = paths;
  fileTypes.push(t);
}

const out = {
  $comment: 'Generated by apps/vscode/tools/build-molang-paths.mjs from public JSON schemas and molang-paths.overrides.json. Do not edit by hand; edit the overrides and regenerate. Provenance: molang-paths.sources.md.',
  version: 1,
  pattern: {
    about: 'Paths are relative to the value of the document\'s root key. Segments are separated by "/". A literal key escapes "~" as "~0" and "/" as "~1".',
    '*': 'any object key',
    '[*]': 'any array index',
    '[N]': 'array index N',
    '**': 'zero or more segments of any kind',
    target: '"value" (default): the string value at the path is Molang. "key": the object keys matched by the last segment are Molang names.',
    accepts: 'JSON types the field takes: "string" is Molang source; "number"/"boolean" are literal stand-ins for a constant expression; "object" is the {"expression": string, "version": int} object form, whose "expression" member is Molang.',
    pack: 'Present only on file types found in both packs: the pack whose documents accept this path. Absent means both.',
    formatVersion: '{"min": "x.y.z"} and/or {"max": "x.y.z"} (inclusive min, exclusive max) of the document\'s format_version for which the path is Molang.',
    joined: 'true on an array-element pattern whose strings the game concatenates, in array order, into ONE expression: an element may open a block that a later element closes, so parse the concatenation, never the elements one by one.',
  },
  kinds: KINDS,
  fileTypes,
};

// One path entry per line: the file is read by people reviewing a diff as
// much as by the extension.
const json = JSON.stringify(out, null, 2).replace(/\{\n\s+"path": [\s\S]*?\n\s+\}/g, (m) => JSON.stringify(JSON.parse(m)).replace(/","/g, '", "').replace(/":/g, '": ').replace(/,"/g, ', "'));
fs.writeFileSync(outFile, json + '\n');

// ---------------------------------------------------------------------------
// sources

const lines = [];
lines.push('# Where the Molang path catalogue comes from');
lines.push('');
lines.push('Generated by `apps/vscode/tools/build-molang-paths.mjs` next to `molang-paths.json`. Every path in the');
lines.push('catalogue is listed here with the public source(s) it was derived from:');
lines.push('');
lines.push('- **BC**: derived from the Blockception schemas (github.com/Blockception/Minecraft-bedrock-json-schemas, as');
lines.push('  bundled in the Blockception VS Code extension): nodes with `"format": "molang"` or a title starting "Molang".');
lines.push('  **BC (description)**: a string-typed Blockception node with no marker whose description calls it Molang');
lines.push('  ("A Molang expression defining ...").');
lines.push('- **MJ**: derived from Mojang\'s published JSON schemas (github.com/Mojang/bedrock-samples, `metadata/json_schemas`):');
lines.push('  nodes referencing `Expression Node.json`, `Molang string.json` or `Molang Expression.json`. The versions listed');
lines.push('  are the schema directories that mark the path.');
lines.push('- **curated**: `molang-paths.overrides.json`, checked against the vanilla files in bedrock-samples by');
lines.push('  `apps/vscode/tools/check-molang-paths.mjs`. Each curated entry carries its reason in the overrides file.');
lines.push('');
lines.push(`Inputs of this build: Blockception schemas ${bcDir ? 'present' : 'absent'}, Mojang schemas ${mjDir ? 'present' : 'absent'}; ${stats.blockception} Blockception and ${stats.mojang} Mojang schema hits.`);
lines.push('');
for (const t of fileTypes) {
  lines.push(`## ${t.rootKey} (${t.name})`);
  lines.push('');
  lines.push('| path | kind | sources |');
  lines.push('|---|---|---|');
  for (const p of t.paths) {
    const k = key(t.rootKey, p.path, p.target);
    const src = [...(provenance.get(k) ?? [])];
    const bc = src.some((s) => s.startsWith('blockception:'));
    const bcd = src.some((s) => s.startsWith('blockception-described:'));
    const mj = src.some((s) => s.startsWith('mojang:'));
    const parts = [];
    if (bc) parts.push('BC');
    else if (bcd) parts.push('BC (description)');
    if (mj) parts.push('MJ ' + [...(versions.get(k) ?? [])].sort(cmpVersion).join(', '));
    if (src.includes('curated')) parts.push('curated');
    lines.push(`| \`${p.path}\`${p.target === 'key' ? ' (key)' : ''} | ${p.kind} | ${parts.join('; ')} |`);
  }
  lines.push('');
}
lines.push('## File types without Molang');
lines.push('');
lines.push('Checked and left out of the catalogue: neither schema set marks a Molang field in them and the golden check');
lines.push('finds no Molang-looking string in their vanilla files.');
lines.push('');
for (const [what, why] of NO_MOLANG) lines.push(`- ${what}: ${why}`);
lines.push('');
lines.push('## Hand-curated entries and their reasons');
lines.push('');
for (const [what, list] of [['added', overrides.add], ['patched', overrides.patch], ['removed', overrides.remove]]) {
  for (const ov of list ?? []) {
    const where = (ov.rootKeys ?? [ov.rootKey]).join(', ') + ' ' + (ov.paths ?? [ov.path]).map((p) => '`' + p + '`').join(', ');
    lines.push(`- ${what} ${where}${ov.target === 'key' ? ' (key)' : ''}: ${ov.reason}`);
  }
}
lines.push('');
lines.push('## Molang-looking strings that are not Molang');
lines.push('');
lines.push('Places in the vanilla files where a string contains something like `query.x` or `v.x` but the field is not');
lines.push('evaluated as Molang. The golden check accepts these without a catalogue entry.');
lines.push('');
for (const n of overrides.notMolang ?? []) lines.push(`- ${n.rootKey} \`${n.path}\`${n.target === 'key' ? ' (key)' : ''}: ${n.reason}`);
lines.push('');
if (skipped.size) {
  lines.push('## Schema hits outside the catalogued file types');
  lines.push('');
  for (const [rk, n] of skipped) lines.push(`- \`${rk}\`: ${n}`);
  lines.push('');
}
fs.writeFileSync(sourcesFile, lines.join('\n'));

const nPaths = fileTypes.reduce((n, t) => n + t.paths.length, 0);
console.log(`build-molang-paths: ${fileTypes.length} file types, ${nPaths} path patterns -> ${path.relative(process.cwd(), outFile)}`);
