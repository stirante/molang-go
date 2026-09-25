#!/usr/bin/env node
// Golden check for apps/vscode/data/molang-paths.json against the vanilla
// packs in bedrock-samples (github.com/Mojang/bedrock-samples).
//
// For every JSON document in behavior_pack/ and resource_pack/:
//   - classify it by root key and pick the catalogue's patterns for it;
//   - hand every string the catalogue calls Molang to molang-go's parser
//     (apps/vscode/tools/molangcheck) and collect what it refuses;
//   - find strings that LOOK like Molang (a q./query./v./variable./t./temp./
//     c./context./math. accessor) at places the catalogue does not cover.
//
// An uncovered candidate is either a missing catalogue entry or something
// that is not Molang; the latter are listed with a reason under "notMolang"
// in molang-paths.overrides.json. A parse failure is either a wrong path/kind
// in the catalogue or a gap in molang-go; the latter are listed under
// "knownParseGaps" in the same file. Anything unexplained fails the check.
//
// Usage:
//   node apps/vscode/tools/check-molang-paths.mjs [--samples <bedrock-samples>]
//        [--catalogue <molang-paths.json>] [--report <out.json>] [--verbose]
// Without --samples, $BEDROCK_SAMPLES is used.

import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, '..', '..', '..');
const dataDir = path.resolve(here, '..', 'data');

const args = {};
for (let i = 2; i < process.argv.length; i++) {
  const k = process.argv[i];
  if (k === '--verbose') args.verbose = true;
  else if (k.startsWith('--')) args[k.slice(2)] = process.argv[++i];
  else throw new Error(`unexpected argument ${k}`);
}
const samples = args.samples ?? process.env.BEDROCK_SAMPLES;
if (!samples || !fs.existsSync(path.join(samples, 'resource_pack'))) {
  console.error('check-molang-paths: pass --samples <bedrock-samples checkout> or set BEDROCK_SAMPLES');
  process.exit(2);
}
const catalogue = JSON.parse(fs.readFileSync(args.catalogue ?? path.join(dataDir, 'molang-paths.json'), 'utf8'));
const overrides = JSON.parse(fs.readFileSync(path.join(dataDir, 'molang-paths.overrides.json'), 'utf8'));

// ---------------------------------------------------------------------------
// lenient JSON: vanilla files carry // and /* */ comments and the odd
// trailing comma.

function stripJsonc(text) {
  let out = '';
  let i = 0, inStr = false;
  while (i < text.length) {
    const c = text[i];
    if (inStr) {
      out += c;
      if (c === '\\') { out += text[i + 1] ?? ''; i += 2; continue; }
      if (c === '"') inStr = false;
      i++;
      continue;
    }
    if (c === '"') { inStr = true; out += c; i++; continue; }
    if (c === '/' && text[i + 1] === '/') { while (i < text.length && text[i] !== '\n') i++; continue; }
    if (c === '/' && text[i + 1] === '*') { i += 2; while (i < text.length && !(text[i] === '*' && text[i + 1] === '/')) i++; i += 2; continue; }
    out += c;
    i++;
  }
  return out.replace(/,(\s*[}\]])/g, '$1').replace(/^﻿/, '');
}

// ---------------------------------------------------------------------------
// patterns

function parsePattern(p) {
  return p === '' ? [] : p.split('/').map((s) => {
    if (s === '*') return { any: 'key' };
    if (s === '**') return { any: 'deep' };
    if (s === '[*]') return { any: 'index' };
    const m = s.match(/^\[(\d+)\]$/);
    if (m) return { index: Number(m[1]) };
    return { key: s.replace(/~1/g, '/').replace(/~0/g, '~') };
  });
}

// segs: [{key}|{index}]
function matchSegs(pat, segs, pi = 0, si = 0) {
  if (pi === pat.length) return si === segs.length;
  const p = pat[pi];
  if (p.any === 'deep') {
    for (let k = si; k <= segs.length; k++) if (matchSegs(pat, segs, pi + 1, k)) return true;
    return false;
  }
  if (si === segs.length) return false;
  const s = segs[si];
  const ok = p.any === 'key' ? s.key !== undefined
    : p.any === 'index' ? s.index !== undefined
      : p.index !== undefined ? s.index === p.index
        : s.key === p.key;
  return ok && matchSegs(pat, segs, pi + 1, si + 1);
}

function cmpVersion(a, b) {
  const pa = String(a).split('.').map(Number), pb = String(b).split('.').map(Number);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = pa[i] ?? 0, y = pb[i] ?? 0;
    if (x !== y) return x - y;
  }
  return 0;
}

const types = new Map();
for (const t of catalogue.fileTypes) {
  types.set(t.rootKey, t.paths.map((p) => ({ ...p, segs: parsePattern(p.path), hits: 0 })));
}

function inRange(entry, fv) {
  if (!entry.formatVersion || !fv) return true;
  if (entry.formatVersion.min && cmpVersion(fv, entry.formatVersion.min) < 0) return false;
  if (entry.formatVersion.max && cmpVersion(fv, entry.formatVersion.max) >= 0) return false;
  return true;
}

const ptr = (segs) => '/' + segs.map((s) => (s.key !== undefined ? s.key.replace(/~/g, '~0').replace(/\//g, '~1') : s.index)).join('/');
// For grouping the report: array indices become [*], and keys that are data
// rather than schema (identifiers with '.' or ':', keys of objects inside
// arrays, keys under known name->value maps) become *.
const DATA_MAPS = new Set(['part_visibility', 'materials', 'item', 'variables', 'curves', 'set_property', 'events', 'animations', 'states', 'bones', 'timeline', 'properties', 'entities', 'component_groups', 'arrays', 'textures', 'geometries']);
const generic = (segs) => segs.map((s, i) => {
  if (s.key === undefined) return '[*]';
  const prev = segs[i - 1];
  if (i === 0 && /[.:]/.test(s.key)) return '*';
  if (prev && (prev.index !== undefined || DATA_MAPS.has(prev.key))) return '*';
  if (/[.:*]/.test(s.key) && !/^minecraft:/.test(s.key)) return '*';
  return s.key.replace(/~/g, '~0').replace(/\//g, '~1');
}).join('/');

// ---------------------------------------------------------------------------
// walk

const LOOKS_LIKE_MOLANG = /(^|[^A-Za-z0-9_:.])(q|query|v|variable|t|temp|c|context|math|array|geometry|texture|material)\.[A-Za-z_]/i;

// Keys that are Molang NAMES, not expressions: checked for shape here rather
// than parsed.
const NAME_SHAPES = {
  variable_name: /^(v|variable)\.[A-Za-z_][A-Za-z0-9_]*$/i,
  array_name: /^array\.[A-Za-z_][A-Za-z0-9_]*$/i,
};
const badNames = [];
const isCommandOrEvent = (s) => /^\s*\//.test(s) || /^\s*@s\s/.test(s);

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

const toParse = [];
const uncovered = [];
const unknownRoots = new Map();
const nonMolangSkipped = { commandOrEvent: 0, plainValue: 0 };
// A string_or_molang value that is one bare word is the plain-string form
// (an enum property value); Molang would need a namespace, a literal or an
// operator.
const isPlainWord = (s) => /^[A-Za-z_][A-Za-z0-9_]*$/.test(s) && !/^(true|false|this)$/i.test(s);
let docs = 0, typed = 0, names = 0;

const notMolang = (overrides.notMolang ?? []).map((n) => ({ ...n, segs: parsePattern(n.path) }));

for (const pack of ['behavior_pack', 'resource_pack']) {
  const packKind = pack === 'behavior_pack' ? 'behavior' : 'resource';
  for (const file of listJson(path.join(samples, pack))) {
    const rel = path.relative(samples, file).replace(/\\/g, '/');
    let doc;
    try { doc = JSON.parse(stripJsonc(fs.readFileSync(file, 'utf8'))); } catch (e) {
      console.warn(`check-molang-paths: cannot parse ${rel}: ${e.message}`);
      continue;
    }
    docs++;
    if (!doc || typeof doc !== 'object' || Array.isArray(doc)) continue;
    const fv = typeof doc.format_version === 'string' ? doc.format_version : null;
    const roots = Object.keys(doc).filter((k) => k !== 'format_version' && k !== '$schema');
    for (const rootKey of roots) {
      const entries = (types.get(rootKey) ?? []).filter((e) => (!e.pack || e.pack === packKind) && inRange(e, fv));
      if (types.has(rootKey)) typed++;
      else unknownRoots.set(rootKey, (unknownRoots.get(rootKey) ?? 0) + 1);
      const valueEntries = entries.filter((e) => (e.target ?? 'value') === 'value');
      const keyEntries = entries.filter((e) => e.target === 'key');

      const visit = (node, segs) => {
        // Keys that are Molang names.
        if (node && typeof node === 'object' && !Array.isArray(node)) {
          for (const k of Object.keys(node)) {
            const ks = [...segs, { key: k }];
            const ke = keyEntries.find((e) => matchSegs(e.segs, ks));
            if (ke) {
              ke.hits++;
              const shape = NAME_SHAPES[ke.kind];
              if (shape) { names++; if (!shape.test(k)) badNames.push({ file: rel, pointer: ptr(ks) + ' (key)', kind: ke.kind, text: k }); }
              else toParse.push({ file: rel, pointer: ptr(ks) + ' (key)', kind: ke.kind, text: k, rootKey, pattern: ke.path });
            }
            else if (LOOKS_LIKE_MOLANG.test(k)) consider(k, ks, true);
          }
        }
        const e = valueEntries.find((x) => matchSegs(x.segs, segs));
        if (e) {
          if (typeof node === 'string') {
            e.hits++;
            if (e.kind === 'event_response' && isCommandOrEvent(node)) { nonMolangSkipped.commandOrEvent++; return; }
            if (e.kind === 'string_or_molang' && isPlainWord(node)) { nonMolangSkipped.plainValue++; return; }
            const item = { file: rel, pointer: ptr(segs), kind: e.kind, text: node, rootKey, pattern: e.path };
            if (e.joined) item.joinKey = rel + '\u0000' + ptr(segs.slice(0, -1));
            toParse.push(item);
            return;
          }
          if (node && typeof node === 'object' && !Array.isArray(node) && e.accepts.includes('object') && typeof node.expression === 'string') {
            e.hits++;
            toParse.push({ file: rel, pointer: ptr([...segs, { key: 'expression' }]), kind: e.kind, text: node.expression, rootKey, pattern: e.path });
            return;
          }
          if (typeof node === 'number' || typeof node === 'boolean') { e.hits++; return; }
        }
        if (typeof node === 'string') { if (LOOKS_LIKE_MOLANG.test(node)) consider(node, segs, false); return; }
        if (Array.isArray(node)) node.forEach((v, i) => visit(v, [...segs, { index: i }]));
        else if (node && typeof node === 'object') for (const [k, v] of Object.entries(node)) visit(v, [...segs, { key: k }]);
      };
      const consider = (text, segs, isKey) => {
        const n = notMolang.find((x) => (x.rootKey === rootKey || x.rootKey === '*') && (x.target === 'key') === isKey && matchSegs(x.segs, segs));
        uncovered.push({ file: rel, rootKey, pointer: ptr(segs) + (isKey ? ' (key)' : ''), generic: generic(segs) + (isKey ? ' (key)' : ''), text, explained: n?.reason ?? null });
      };
      visit(doc[rootKey], []);
    }
  }
}

// Joined arrays are one program: parse the concatenation, reported at the
// array's pointer.
{
  const joined = new Map();
  const rest = [];
  for (const t of toParse) {
    if (!t.joinKey) { rest.push(t); continue; }
    if (!joined.has(t.joinKey)) {
      const parent = t.pointer.replace(/\/\d+$/, '');
      joined.set(t.joinKey, { ...t, pointer: parent + ' (joined)', text: '', parts: 0 });
      rest.push(joined.get(t.joinKey));
    }
    const j = joined.get(t.joinKey);
    j.text += t.text;
    j.parts++;
  }
  toParse.length = 0;
  toParse.push(...rest);
}

// ---------------------------------------------------------------------------
// parse with molang-go

const input = toParse.map((t) => JSON.stringify({ file: t.file, pointer: t.pointer, kind: t.kind, text: t.text })).join('\n') + '\n';
const run = spawnSync('go', ['run', './apps/vscode/tools/molangcheck'], { cwd: repoRoot, input, maxBuffer: 1 << 28, encoding: 'utf8' });
if (run.status !== 0) {
  console.error(run.stderr);
  process.exit(2);
}
const results = run.stdout.trim().split('\n').filter(Boolean).map((l) => JSON.parse(l));
const summary = results.pop().summary;
const byPtr = new Map(toParse.map((t) => [t.file + '\u0000' + t.pointer, t]));
const knownGaps = (overrides.knownParseGaps ?? []).map((g) => ({ ...g, re: new RegExp(g.textRe) }));
const failures = results.map((f) => {
  const t = byPtr.get(f.file + '\u0000' + f.pointer);
  const gap = knownGaps.find((g) => g.re.test(f.text));
  return { ...f, rootKey: t?.rootKey, pattern: t?.pattern, gap: gap?.id ?? null };
});

// ---------------------------------------------------------------------------
// report

const group = (list, keyOf) => {
  const m = new Map();
  for (const x of list) {
    const k = keyOf(x);
    if (!m.has(k)) m.set(k, []);
    m.get(k).push(x);
  }
  return [...m.entries()].sort((a, b) => b[1].length - a[1].length);
};

const unexplained = uncovered.filter((u) => !u.explained);
const explained = uncovered.filter((u) => u.explained);
const gapFailures = failures.filter((f) => f.gap);
const catFailures = failures.filter((f) => !f.gap);

const lines = [];
lines.push(`documents: ${docs} (${typed} with a catalogued root key)`);
lines.push(`Molang strings extracted: ${summary.parsed} (joined arrays count once; skipped ${nonMolangSkipped.commandOrEvent} commands/events in event_response fields and ${nonMolangSkipped.plainValue} plain values in string_or_molang fields)`);
lines.push(`Molang names in keys (variables, arrays): ${names}, ${badNames.length} malformed`);
lines.push(`parse failures: ${summary.failed} (${gapFailures.length} known molang-go gaps, ${catFailures.length} unexplained)`);
lines.push(`Molang-looking strings outside the catalogue: ${uncovered.length} (${explained.length} explained as not Molang, ${unexplained.length} unexplained)`);
lines.push('');
if (unexplained.length) {
  lines.push('UNEXPLAINED uncovered candidates (root key, generic path, count, example):');
  for (const [k, l] of group(unexplained, (u) => u.rootKey + ' ' + u.generic)) lines.push(`  ${k}  x${l.length}  e.g. ${JSON.stringify(l[0].text).slice(0, 100)}  (${l[0].file})`);
  lines.push('');
}
if (badNames.length) {
  lines.push('MALFORMED names in Molang-name keys:');
  for (const b of badNames) lines.push(`  ${b.file} ${b.pointer} [${b.kind}] ${JSON.stringify(b.text)}`);
  lines.push('');
}
if (catFailures.length) {
  lines.push('UNEXPLAINED parse failures (catalogue path or molang-go gap?):');
  for (const f of catFailures) lines.push(`  ${f.file} ${f.pointer} [${f.kind}] ${JSON.stringify(f.text).slice(0, 120)}\n      ${f.error}`);
  lines.push('');
}
if (gapFailures.length) {
  lines.push('Known molang-go parse gaps on vanilla strings:');
  for (const [k, l] of group(gapFailures, (f) => f.gap)) lines.push(`  ${k}: ${l.length}  e.g. ${l[0].file} ${l[0].pointer} ${JSON.stringify(l[0].text).slice(0, 100)}`);
  lines.push('');
}
if (explained.length && args.verbose) {
  lines.push('Explained (not Molang):');
  for (const [k, l] of group(explained, (u) => u.rootKey + ' ' + u.generic)) lines.push(`  ${k}  x${l.length}: ${l[0].explained}`);
  lines.push('');
}
if (unknownRoots.size && args.verbose) {
  lines.push('Root keys with no catalogue entry (ui/texture files excluded): ' + [...unknownRoots.entries()].filter(([k]) => /^minecraft:|^[a-z_]+$/.test(k) && k.length > 5).sort((a, b) => b[1] - a[1]).slice(0, 40).map(([k, n]) => `${k} (${n})`).join(', '));
  lines.push('');
}
if (args.verbose) {
  const unused = [];
  for (const [rk, list] of types) for (const e of list) if (!e.hits) unused.push(`${rk} ${e.path}${e.target === 'key' ? ' (key)' : ''}`);
  lines.push(`Catalogue patterns with no vanilla instance: ${unused.length}`);
  for (const u of unused) lines.push('  ' + u);
}
console.log(lines.join('\n'));

if (args.report) {
  fs.writeFileSync(args.report, JSON.stringify({ summary, uncovered, failures }, null, 1));
}
process.exit(unexplained.length || catFailures.length || badNames.length ? 1 : 0);
