#!/usr/bin/env node
// build.mjs -- the query catalogue, from the working notes to the files that
// ship. One command rebuilds everything that is derived from the notes:
//
//   node tools/catalogue/build.mjs [--research <dir>]
//
// writes
//
//   apps/vscode/catalogue/catalogue.json   what the extension (and the Go
//                                          bridge) reads: every field they read
//                                          before, plus hover, args[].accepts /
//                                          default, docs and verification
//   apps/vscode/catalogue/math.json        copied from the notes when changed
//   apps/vscode/catalogue/namespaces.json  copied from the notes when changed
//   docs/site/data/queries.json            the long form the documentation
//                                          site generates one page per query from
//
// and writes nothing unless every rule holds (see rules.mjs, plus: each
// example parses with molang-go, each related query exists). The notes are
// read, never written.
//
//   node tools/catalogue/build.mjs --check
//
// needs no notes: it runs the same rules over the committed files. CI runs it.
//
// The notes live in research/catalogue/ (git-ignored). From a linked worktree
// they are found in the main checkout; --research or MOLANG_RESEARCH names
// another place.
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { docsUrl, notesDir, provenanceProblems, queryListProblems, textRules } from './rules.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, '..', '..');
const argv = process.argv.slice(2);
const checkOnly = argv.includes('--check');

const OUT = {
  catalogue: path.join(repo, 'apps', 'vscode', 'catalogue', 'catalogue.json'),
  math: path.join(repo, 'apps', 'vscode', 'catalogue', 'math.json'),
  namespaces: path.join(repo, 'apps', 'vscode', 'catalogue', 'namespaces.json'),
  docs: path.join(repo, 'docs', 'site', 'data', 'queries.json'),
};

/** Vanilla excerpts molang-go refuses, quoted as the samples have them. Each is
 * shown on the site verbatim, so each is listed here with the reason instead
 * of failing the build. A new refusal fails it. */
const VANILLA_UNPARSED = {
  'surface_particle_color|variable.dig_particle_color = query.surface_particle_color':
    "an assignment without the closing ';', which molang-go requires",
  'surface_particle_texture_coordinate|variable.dig_particle_texture_coordinate = query.surface_particle_texture_coordinate':
    "an assignment without the closing ';', which molang-go requires",
  'surface_particle_texture_size|variable.dig_particle_texture_size = query.surface_particle_texture_size':
    "an assignment without the closing ';', which molang-go requires",
};

const CONTEXTS = ['client_entity', 'attachable', 'particle_entity_bound', 'particle_world', 'block', 'server_entity', 'item', 'world_gen'];

function fail(problems, what) {
  console.error(`catalogue: ${problems.length} problem(s) ${what}:\n`);
  for (const p of problems.slice(0, 200)) console.error('  ' + p);
  if (problems.length > 200) console.error(`  ... and ${problems.length - 200} more`);
  process.exit(1);
}

/** The notes files that are inputs to what ships, and nothing else: an
 * allow-list, so a notes file added later is never read by accident. */
const NOTES_INPUTS = /^(catalogue|math|namespaces|pilot|b\d\d)\.json$/;

/** Reads a notes file, refusing any that is not a listed input. */
function readNotes(file) {
  if (!NOTES_INPUTS.test(path.basename(file))) throw new Error(`refusing to read ${file}: not a catalogue input`);
  return JSON.parse(fs.readFileSync(file, 'utf8'));
}

function researchDir() {
  const i = argv.indexOf('--research');
  if (i >= 0) return path.resolve(argv[i + 1]);
  return notesDir();
}

// ---------------------------------------------------------------------------
// Parsing: every example through molang-go's own parser, by the helper the
// path catalogue already uses (apps/vscode/tools/molangcheck).

/** The Molang in a vanilla excerpt. Excerpts are quoted from pack JSON, so
 * one may be a whole `"key": "expr"` line, an array of strings, or a bare
 * expression; keys are not Molang. */
function molangIn(text) {
  const t = text.trim();
  if (!/^[{"\[]/.test(t) && !/"\s*:\s*[\["{]/.test(t)) return [t];
  const out = [];
  const re = /"((?:[^"\\]|\\.)*)"(\s*:)?/g;
  let m;
  while ((m = re.exec(t))) {
    if (m[2]) continue;
    out.push(JSON.parse(`"${m[1]}"`));
  }
  // A trailing string the excerpt cuts short: `"(... ) - this` with no end quote.
  if (!out.length && t.startsWith('"')) out.push(t.replace(/^"|",?$/g, ''));
  return out;
}

function parseProblems(queries) {
  const items = [];
  const unparsedSeen = new Set();
  for (const q of queries) {
    if (q.example) items.push({ file: q.name, pointer: 'example', kind: 'example', text: q.example });
    for (const [i, e] of (q.vanillaUsage?.examples ?? []).entries()) {
      for (const text of molangIn(e.expr)) items.push({ file: q.name, pointer: `vanillaUsage.examples[${i}]`, kind: 'vanilla', text });
    }
  }
  const input = items.map((x) => JSON.stringify(x)).join('\n') + '\n';
  const out = execFileSync('go', ['run', './apps/vscode/tools/molangcheck'], { cwd: repo, input, encoding: 'utf8', maxBuffer: 1 << 26 });
  const problems = [];
  let summary;
  for (const line of out.split('\n')) {
    if (!line.trim()) continue;
    const r = JSON.parse(line);
    if (r.summary) { summary = r.summary; continue; }
    const key = `${r.file}|${r.text}`;
    if (r.kind === 'vanilla' && VANILLA_UNPARSED[key]) { unparsedSeen.add(key); continue; }
    problems.push(`query.${r.file} ${r.pointer}: ${JSON.stringify(r.text)} does not parse: ${r.error}`);
  }
  for (const key of Object.keys(VANILLA_UNPARSED)) {
    if (!unparsedSeen.has(key)) problems.push(`VANILLA_UNPARSED lists ${JSON.stringify(key)}, which no longer fails (or is gone): remove it`);
  }
  return { problems, parsed: summary?.parsed ?? items.length, known: unparsedSeen.size };
}

// ---------------------------------------------------------------------------
// --check: the committed files, no notes needed.

if (checkOnly) {
  const cat = JSON.parse(fs.readFileSync(OUT.catalogue, 'utf8'));
  const docs = JSON.parse(fs.readFileSync(OUT.docs, 'utf8'));
  const names = cat.queries.map((q) => q.name);
  const problems = [
    ...queryListProblems(cat.queries, { label: 'catalogue.json' }),
    ...queryListProblems(docs.queries, { names, label: 'queries.json' }),
    ...provenanceProblems(JSON.parse(fs.readFileSync(OUT.math, 'utf8')), 'math.json'),
    ...provenanceProblems(JSON.parse(fs.readFileSync(OUT.namespaces, 'utf8')), 'namespaces.json'),
    ...provenanceProblems({ ...cat, queries: undefined }, 'catalogue.json'),
    ...provenanceProblems({ ...docs, queries: undefined }, 'queries.json'),
  ];
  const parse = parseProblems(docs.queries);
  problems.push(...parse.problems);
  if (problems.length) fail(problems, 'in the committed catalogue');
  console.log(`catalogue: ${cat.queries.length} queries, ${parse.parsed} examples parsed (${parse.known} known vanilla refusals), every rule holds` +
    (textRules() ? '' : ' (public-text rules not present here, so only the shape rules ran)'));
  process.exit(0);
}

// ---------------------------------------------------------------------------
// The build.

const research = researchDir();
const deepDir = path.join(research, 'deep');
if (!fs.existsSync(deepDir)) {
  console.error(`catalogue: no notes at ${research} (use --research <dir> or MOLANG_RESEARCH)`);
  process.exit(2);
}

// Building from the notes is the one place the public-text rules must be
// present: this is where notes text becomes shipped text.
if (!argv.includes('--research') && !process.env.MOLANG_RESEARCH) process.env.MOLANG_RESEARCH = research;
if (!textRules()) {
  console.error(`catalogue: no public-text rules (provenance-rules.json beside the notes, or MOLANG_PROVENANCE_RULES); refusing to build without them`);
  process.exit(2);
}

const short = readNotes(path.join(research, 'catalogue.json'));
const batchFiles = ['pilot.json', ...fs.readdirSync(deepDir).filter((f) => /^b\d\d\.json$/.test(f)).sort()];
const deep = new Map();
const problems = [];
let gameVersion = short.gameVersion;
for (const f of batchFiles) {
  const b = readNotes(path.join(deepDir, f));
  if (b.schemaVersion !== 'deep-0.1') problems.push(`${f}: schemaVersion ${b.schemaVersion}, this build reads deep-0.1`);
  for (const q of b.queries) {
    if (deep.has(q.name)) problems.push(`query.${q.name} is in ${deep.get(q.name).file} and ${f}`);
    deep.set(q.name, { file: f, q });
  }
}
const names = short.queries.map((q) => q.name).sort();
for (const n of names) if (!deep.has(n)) problems.push(`query.${n}: in catalogue.json, in no batch`);
for (const n of deep.keys()) if (!names.includes(n)) problems.push(`query.${n}: in a batch, not in catalogue.json`);
if (problems.length) fail(problems, 'in the notes');

// Rewordings (public-text.json), applied before anything reads the text.
const fixes = JSON.parse(fs.readFileSync(path.join(here, 'public-text.json'), 'utf8')).fixes;
const sha = (s) => createHash('sha256').update(s).digest('hex').slice(0, 16);
for (const fix of fixes) {
  const entry = deep.get(fix.query)?.q;
  const keys = fix.path.split('.');
  let parent = entry;
  for (const k of keys.slice(0, -1)) parent = parent?.[k];
  const last = keys[keys.length - 1];
  const was = parent?.[last];
  if (typeof was !== 'string') {
    problems.push(`public-text.json: query.${fix.query} ${fix.path} is not a text field (any more): remove the fix`);
    continue;
  }
  if (sha(was) !== fix.was) {
    problems.push(`public-text.json: query.${fix.query} ${fix.path} changed in the notes (sha ${sha(was)}, fix is for ${fix.was || 'nothing yet'}): re-read it, then update or remove the fix`);
    continue;
  }
  parent[last] = fix.text;
}
if (problems.length) fail(problems, 'in public-text.json');

// ---------------------------------------------------------------------------
// Public fields.

/** The four public states, from the notes' verification record. The record
 * itself never ships; only the state, the version, and a fixed sentence. */
function publicVerification(v, side) {
  const game = v.measuredBuild || v.build || gameVersion;
  const ig = String(v.inGame ?? '');
  if (/^not-needed/.test(ig)) {
    return { status: 'documented', game, label: `Documented for ${game}; a plain flag or copy with nothing to measure in game` };
  }
  if (/^measured/.test(ig)) {
    // What the in-game evaluator could not reach on the server side limits
    // the scope of a measurement, not how much of the client side it covered.
    const serverPart = /server half|client-only evaluator|behavior-pack value|server value/i;
    const rest = ig.replace(/[^;)(]*(server half|client-only evaluator|behavior-pack value|server value)[^;)(]*/gi, '');
    const partial = (v.open?.length ?? 0) > 0 ||
      /in part|smoke test only|outside entity rendering only|not measured|not reached|unmeasured|not tested|refusal only|argument handling only|blocked/i.test(rest);
    const clientOnly = serverPart.test(ig) && !/^client/.test(side);
    if (partial) return { status: 'partial', game, label: `Partly verified in game on ${game}; some cases are not measured yet` };
    return { status: 'verified', game, label: clientOnly ? `Verified in game on ${game} (client side)` : `Verified in game on ${game}` };
  }
  // A lasting "blocked" is not a check still to come: the case cannot be seen
  // from an in-game probe on the client at all (a server-only value, world
  // generation, a particle or attachable context, content that needs its own
  // pack). Calling it pending would promise a verification that is not coming.
  if (/^blocked/.test(ig)) {
    return { status: 'unobservable', game, label: `Documented for ${game}; this behaviour cannot be observed from the client in game, so it is not verified there` };
  }
  return { status: 'pending', game, label: `Documented for ${game}; not yet verified in game` };
}

const semver = /^\d+\.\d+\.\d+$/;
/** behaviourChangesByVersion, in one shape: [{since, until, change}]. The
 * notes have written it several ways; a new one stops the build. */
function behaviourChanges(list, name) {
  if (!list) return null;
  return list.map((c) => {
    let since = c.fromSemVersion ?? c.fromVersion ?? null;
    let until = c.toSemVersionExclusive ?? c.toVersionExclusive ?? c.before ?? null;
    if (typeof c.molangVersion === 'string') {
      const m = /^(<|>=)\s*(\S+)$/.exec(c.molangVersion);
      if (!m) throw new Error(`query.${name}: behaviourChangesByVersion molangVersion ${c.molangVersion}`);
      if (m[1] === '<') until = m[2];
      else since = m[2];
    }
    const change = c.change ?? c.behaviour;
    if (typeof change !== 'string' || (since && !semver.test(since)) || (until && !semver.test(until)) || (!since && !until)) {
      throw new Error(`query.${name}: behaviourChangesByVersion entry not understood: ${JSON.stringify(c)}`);
    }
    return { since, until, change };
  });
}

function stripKeys(o, keys) {
  if (!o) return o;
  const out = {};
  for (const [k, v] of Object.entries(o)) if (!keys.includes(k) && v !== undefined) out[k] = v;
  return out;
}

/** `query.name(slot_name, [slot_index], item...)`; a query without arguments as `query.name`. */
function signature(q) {
  if (!q.args.length) return `query.${q.name}`;
  const parts = q.args.map((a, i) => {
    const rest = a.variadic || (q.variadic && i === q.args.length - 1) ? '...' : '';
    return a.optional ? `[${a.name}${rest}]` : `${a.name}${rest}`;
  });
  return `query.${q.name}(${parts.join(', ')})`;
}

const confidence = { CONFIRMED: 'high', PARTIAL: 'medium', INFERRED: 'low' };
const shortByName = new Map(short.queries.map((q) => [q.name, q]));

const shipped = [];
const long = [];
for (const name of names) {
  const q = deep.get(name).q;
  const verification = publicVerification(q.verification ?? {}, String(q.side));
  const deprecated = q.deprecated ? stripKeys(q.deprecated, ['source']) : null;
  const versionGate = q.versionGate ? stripKeys(q.versionGate, ['source']) : null;
  const changes = behaviourChanges(q.behaviourChangesByVersion, name);
  const argsShort = q.args.map((a) => {
    const o = { name: a.name, type: a.type, optional: !!a.optional, description: a.description };
    if (a.variadic) o.variadic = true;
    if (a.accepts?.length) o.accepts = a.accepts;
    if (a.default !== undefined && a.default !== null) o.default = a.default;
    return o;
  });
  const argsLong = q.args.map((a, i) => ({ ...argsShort[i], onInvalid: a.onInvalid ?? null }));

  for (const c of q.appliesTo?.contexts ?? []) if (!CONTEXTS.includes(c)) problems.push(`query.${name}: unknown context ${c}`);
  for (const r of q.related ?? []) if (!r.startsWith('query.') || !deep.has(r.slice(6))) problems.push(`query.${name}: related ${r} is not a query`);

  shipped.push({
    name,
    querySet: q.querySet,
    summary: q.summary,
    hover: q.hover,
    description: q.description,
    args: argsShort,
    minArgs: q.minArgs,
    maxArgs: q.maxArgs,
    variadic: q.variadic,
    returns: q.returns,
    side: q.side,
    example: q.example,
    notes: q.notes ?? [],
    deprecated,
    versionGate,
    behaviourChangesByVersion: changes,
    experimental: q.experimental ?? null,
    clientOnly: !!q.clientOnly,
    confidence: confidence[q.verification?.static] ?? shortByName.get(name)?.confidence ?? 'medium',
    verification,
    docs: docsUrl(name),
  });
  long.push({
    name,
    querySet: q.querySet,
    signature: signature(q),
    summary: q.summary,
    hover: q.hover,
    description: q.description,
    long: q.long,
    args: argsLong,
    minArgs: q.minArgs,
    maxArgs: q.maxArgs,
    variadic: q.variadic,
    returns: q.returns,
    formula: q.formula ?? null,
    units: q.units ?? null,
    range: q.range ?? null,
    appliesTo: q.appliesTo,
    whenUnavailable: q.whenUnavailable ?? [],
    side: q.side,
    timing: q.timing,
    example: q.example,
    vanillaUsage: q.vanillaUsage,
    pitfalls: q.pitfalls ?? [],
    related: q.related ?? [],
    notes: q.notes ?? [],
    deprecated,
    versionGate,
    behaviourChangesByVersion: changes,
    experimental: q.experimental ?? null,
    clientOnly: !!q.clientOnly,
    verification,
    docs: docsUrl(name),
  });
}

const math = readNotes(path.join(research, 'math.json'));
const namespaces = readNotes(path.join(research, 'namespaces.json'));
const catalogueDoc = { gameVersion, queries: shipped };
const docsDoc = { gameVersion, queries: long };

problems.push(
  ...queryListProblems(shipped, { names, label: 'catalogue.json' }),
  ...queryListProblems(long, { names, label: 'queries.json' }),
  ...provenanceProblems(math, 'math.json'),
  ...provenanceProblems(namespaces, 'namespaces.json'),
);
const parse = parseProblems(long);
problems.push(...parse.problems);
if (problems.length) fail(problems, 'in the built catalogue; nothing was written');

// What changed for the extension's checks, against the catalogue being replaced.
const before = fs.existsSync(OUT.catalogue) ? JSON.parse(fs.readFileSync(OUT.catalogue, 'utf8')) : { queries: [] };
const beforeBy = new Map(before.queries.map((q) => [q.name, q]));
const checked = ['querySet', 'minArgs', 'maxArgs', 'variadic', 'returns', 'clientOnly'];
const changed = [];
for (const q of shipped) {
  const b = beforeBy.get(q.name);
  if (!b) { changed.push(`query.${q.name}: new`); continue; }
  for (const k of checked) if (JSON.stringify(b[k]) !== JSON.stringify(q[k])) changed.push(`query.${q.name} ${k}: ${JSON.stringify(b[k])} -> ${JSON.stringify(q[k])}`);
  if (JSON.stringify(b.deprecated?.replacement ?? null) !== JSON.stringify(q.deprecated?.replacement ?? null)) changed.push(`query.${q.name} deprecated.replacement: ${JSON.stringify(b.deprecated?.replacement ?? null)} -> ${JSON.stringify(q.deprecated?.replacement ?? null)}`);
  if (JSON.stringify(b.versionGate ? { since: b.versionGate.since, until: b.versionGate.until } : null) !== JSON.stringify(q.versionGate ? { since: q.versionGate.since, until: q.versionGate.until } : null)) changed.push(`query.${q.name} versionGate changed`);
}

const write = (file, doc) => {
  const text = JSON.stringify(doc, null, 2) + '\n';
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const old = fs.existsSync(file) ? fs.readFileSync(file, 'utf8') : null;
  if (old === text) return false;
  // Copied files keep their formatting while their content is the same.
  if (old !== null && JSON.stringify(JSON.parse(old)) === JSON.stringify(doc)) return false;
  fs.writeFileSync(file + '.tmp', text);
  fs.renameSync(file + '.tmp', file);
  return true;
};
const wrote = Object.entries({ catalogue: catalogueDoc, math, namespaces, docs: docsDoc })
  .filter(([k, doc]) => write(OUT[k], doc))
  .map(([k]) => path.relative(repo, OUT[k]).replace(/\\/g, '/'));

const count = (k) => shipped.filter((q) => q.verification.status === k).length;
console.log(`catalogue: ${shipped.length} queries from ${batchFiles.length} batch files (${research})`);
console.log(`  verification: ${count('verified')} verified, ${count('partial')} partly verified, ${count('documented')} documented (nothing to measure), ${count('unobservable')} not observable in game, ${count('pending')} pending`);
console.log(`  examples: ${parse.parsed} parsed with molang-go, ${parse.known} known vanilla refusals`);
console.log(`  provenance and shape rules: pass`);
if (changed.length) {
  console.log(`  ${changed.length} change(s) the extension's checks will see:`);
  for (const c of changed) console.log('    ' + c);
}
console.log(wrote.length ? `  wrote ${wrote.join(', ')}` : '  no file changed');
