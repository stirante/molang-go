// The rules every shipped catalogue file obeys, shared by the places that
// check them: build.mjs (which refuses to write a file that breaks one), the
// extension's unit tests (which check the committed files) and the docs site's
// checker (which checks the data its pages are generated from).
//
// Two kinds of rule:
//
//   - Public text only. The catalogue is written from working notes that are
//     never published, and nothing about how an entry was established may
//     reach a shipped file. The text patterns and the keys that must never
//     ship are not in this repository: a list of what must not appear would
//     itself say it. They are read from the file MOLANG_PROVENANCE_RULES
//     names, or from provenance-rules.json beside the notes (see notesDir).
//     Where that file is absent -- a fresh clone, CI -- the text rules are
//     skipped and textRules() says so; building the catalogue from the notes
//     requires it.
//   - Shape. Every query exactly once, a hover of at most four lines, a docs
//     URL that follows the route contract (docsUrl), and a verification field
//     that is the small public one and nothing more. These always run.
import { execFileSync } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');

/** Where the working notes live: research/catalogue in this checkout or, from
 * a linked worktree, in the main checkout; MOLANG_RESEARCH overrides both. */
export function notesDir() {
  if (process.env.MOLANG_RESEARCH) return path.resolve(process.env.MOLANG_RESEARCH);
  const local = path.join(repo, 'research', 'catalogue');
  if (fs.existsSync(local)) return local;
  try {
    const common = execFileSync('git', ['rev-parse', '--path-format=absolute', '--git-common-dir'], { cwd: repo, encoding: 'utf8' }).trim();
    return path.join(path.dirname(common), 'research', 'catalogue');
  } catch {
    return local;
  }
}

let loaded;
/** The public-text rules, or null when the rules file is not present. */
export function textRules() {
  if (loaded !== undefined) return loaded;
  const file = process.env.MOLANG_PROVENANCE_RULES || path.join(notesDir(), 'provenance-rules.json');
  if (!fs.existsSync(file)) return (loaded = null);
  const raw = JSON.parse(fs.readFileSync(file, 'utf8'));
  loaded = {
    file,
    banned: raw.banned.map((b) => new RegExp(b.pattern, b.flags)),
    forbiddenKeys: raw.forbiddenKeys,
    mustCatch: raw.mustCatch ?? [],
  };
  return loaded;
}

/** Where the documentation site is served. The route contract: a query's page
 * is always DOCS_ORIGIN + DOCS_BASE + 'queries/' + name, so anything that
 * knows a query's name can link to its page without a table. */
export const DOCS_ORIGIN = 'https://stirante.github.io';
export const DOCS_BASE = '/molang-go/';
export const docsUrl = (name) => `${DOCS_ORIGIN}${DOCS_BASE}queries/${name}`;

/** The public verification field: one of four states, the game version the
 * text is a statement about, and the sentence the editor and the site show. */
export const VERIFICATION_STATES = ['verified', 'partial', 'documented', 'unobservable', 'pending'];
export const VERIFICATION_KEYS = ['status', 'game', 'label'];

/** Every problem with the public-text rule in value, as "where: what". */
export function provenanceProblems(value, where = '') {
  const out = [];
  const rules = textRules();
  if (!rules) return out;
  const { banned, forbiddenKeys } = rules;
  const walk = (v, path) => {
    if (typeof v === 'string') {
      for (const re of banned) {
        const m = re.exec(v);
        if (m) out.push(`${path}: "${m[0]}" in ${JSON.stringify(v.slice(Math.max(0, m.index - 40), m.index + 40))}`);
      }
    } else if (Array.isArray(v)) {
      v.forEach((x, i) => walk(x, `${path}[${i}]`));
    } else if (v && typeof v === 'object') {
      for (const [k, x] of Object.entries(v)) {
        if (forbiddenKeys.includes(k)) out.push(`${path}.${k}: key not allowed in a shipped file`);
        walk(k, `${path}{key}`);
        walk(x, `${path}.${k}`);
      }
    }
  };
  walk(value, where);
  return out;
}

/** Problems with one entry's public verification field. */
export function verificationProblems(v, where) {
  if (!v || typeof v !== 'object') return [`${where}.verification: missing`];
  const out = [];
  for (const k of Object.keys(v)) if (!VERIFICATION_KEYS.includes(k)) out.push(`${where}.verification.${k}: not a public field`);
  if (!VERIFICATION_STATES.includes(v.status)) out.push(`${where}.verification.status: ${JSON.stringify(v.status)}`);
  if (typeof v.game !== 'string' || !/^\d+\.\d+\.\d+(\.\d+)?$/.test(v.game)) out.push(`${where}.verification.game: ${JSON.stringify(v.game)}`);
  if (typeof v.label !== 'string' || !v.label) out.push(`${where}.verification.label: missing`);
  return out;
}

/**
 * Every rule, over a shipped query list (catalogue.json's queries or the docs
 * data's). names, when given, is the exact set that must be present.
 */
export function queryListProblems(queries, { names, label = 'queries' } = {}) {
  const out = [];
  const seen = new Map();
  for (const [i, q] of queries.entries()) {
    const where = `${label}[${i}] ${q.name}`;
    seen.set(q.name, (seen.get(q.name) ?? 0) + 1);
    if (typeof q.hover !== 'string' || !q.hover.trim()) out.push(`${where}.hover: missing`);
    else if (q.hover.split('\n').length > 4) out.push(`${where}.hover: ${q.hover.split('\n').length} lines, at most 4`);
    if (q.docs !== docsUrl(q.name)) out.push(`${where}.docs: ${JSON.stringify(q.docs)}, the route contract says ${docsUrl(q.name)}`);
    out.push(...verificationProblems(q.verification, where));
  }
  for (const [n, c] of seen) if (c > 1) out.push(`${label}: query.${n} appears ${c} times`);
  if (names) {
    for (const n of names) if (!seen.has(n)) out.push(`${label}: query.${n} is missing`);
    for (const n of seen.keys()) if (!names.includes(n)) out.push(`${label}: query.${n} is not in the query list`);
  }
  out.push(...provenanceProblems(queries, label));
  return out;
}
