#!/usr/bin/env node
// generate.mjs -- the reference pages, from the catalogue data. Runs before
// every `vitepress dev` and `vitepress build` (predev / prebuild), so the
// pages are build output and never committed:
//
//   data/queries.json                     -> queries/<name>.md (one per query)
//                                            generated/query-index.json (the index page's list)
//                                            generated/sidebar.json
//   ../../apps/vscode/catalogue/math.json  -> generated/math.md
//   ../../apps/vscode/catalogue/namespaces.json -> generated/namespaces.md, generated/operators.md
//
// The inputs are committed and are themselves written by
// tools/catalogue/build.mjs at the repository root, which is where the rules
// about their content are enforced. This script only lays them out, following
// the page shape in the catalogue's schema: title, signature, facts line,
// badges, the long text, then the generated blocks (arguments, when it can't
// answer, where it works, vanilla usage, pitfalls, related).
//
// Route contract: query.<name> is served at /queries/<name>. The extension
// links there by name alone; tools/check-links.mjs holds the two sides to it.
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const siteDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const repoDir = path.resolve(siteDir, '..', '..')
const readJson = (...p) => JSON.parse(fs.readFileSync(path.join(...p), 'utf8'))

const data = readJson(siteDir, 'data', 'queries.json')
const math = readJson(repoDir, 'apps', 'vscode', 'catalogue', 'math.json')
const ns = readJson(repoDir, 'apps', 'vscode', 'catalogue', 'namespaces.json')
const names = new Set(data.queries.map((q) => q.name))

const CONTEXT_NAMES = {
  client_entity: 'client entities (resource pack)',
  attachable: 'attachables',
  particle_entity_bound: 'particles attached to an entity',
  particle_world: 'particles in the world',
  block: 'blocks',
  server_entity: 'server entities (behavior pack)',
  item: 'item tag expressions',
  world_gen: 'world generation',
}
const SET_NAMES = { default: 'Entity, block and particle queries', tags: 'Tag expression queries', world_gen: 'World generation queries' }

// ---------------------------------------------------------------------------
// Text from the data goes into Markdown that Vue compiles, so outside code a
// `<` would open a tag and `{{` an interpolation. Code spans and fences are
// left alone (VitePress already keeps Vue out of them).

function escapeProse(s) {
  return s.replace(/</g, '&lt;').replace(/\{\{/g, '&#123;&#123;').replace(/\}\}/g, '&#125;&#125;')
}

/** Applies f to the prose of a Markdown text, not to its code spans or fences. */
function mapProse(md, f) {
  const out = []
  let inFence = false
  for (const line of md.split('\n')) {
    if (/^\s*(```|~~~)/.test(line)) {
      inFence = !inFence
      out.push(line)
      continue
    }
    if (inFence) {
      out.push(line)
      continue
    }
    out.push(line.split(/(`+[^`]*`+)/).map((part, i) => (i % 2 ? f(part, true) : f(part, false))).join(''))
  }
  return out.join('\n')
}

/** Prose escaped, and `query.x` code spans linked to x's page. */
function render(md, self) {
  return mapProse(md, (part, code) => {
    if (!code) return escapeProse(part)
    const m = /^`(?:query|q)\.([a-z0-9_]+)`$/.exec(part)
    if (m && names.has(m[1]) && m[1] !== self) return `[${part}](/queries/${m[1]})`
    return part
  })
}

/** For a table cell: one line, pipes escaped. */
const cell = (s, self) => render(String(s), self).replace(/\|/g, '\\|').replace(/\n+/g, '<br>')

const yaml = (v) => JSON.stringify(v)

// ---------------------------------------------------------------------------
// One page per query.

function factsLine(q) {
  const parts = [`returns \`${q.returns}\``, `side: ${escapeProse(q.side)}`, `set: \`${q.querySet}\``]
  if (q.timing?.updates) parts.push(`updates: ${escapeProse(q.timing.updates)}${q.timing.interpolated ? ', smoothed between ticks' : ''}`)
  return parts.join(' · ')
}

function queryPage(q) {
  const L = []
  const fm = {
    title: `query.${q.name}`,
    description: q.summary,
    query: q.name,
    querySet: q.querySet,
    side: q.side,
    returns: q.returns,
    clientOnly: q.clientOnly,
    deprecated: !!q.deprecated,
    since: q.versionGate?.since ?? null,
    until: q.versionGate?.until ?? null,
    verification: q.verification,
  }
  L.push('---')
  for (const [k, v] of Object.entries(fm)) L.push(`${k}: ${yaml(v)}`)
  L.push('---', '')
  L.push(`# query.${q.name}`, '')
  L.push('<QueryBadges />', '')
  L.push('```molang', q.signature, '```', '')
  L.push(factsLine(q), '')
  const units = [q.units && `**Units:** ${escapeProse(q.units)}`, q.range && `**Range:** ${escapeProse(q.range)}`].filter(Boolean)
  if (units.length) L.push(units.join(' · '), '')

  if (q.deprecated) {
    const r = q.deprecated.replacement
    const repl = r ? ` Use ${names.has(r.replace(/^query\./, '')) ? `[\`${r}\`](./${r.replace(/^query\./, '')})` : `\`${r}\``} instead.` : ''
    const note = q.deprecated.note ? ' ' + escapeProse(q.deprecated.note) + (/[.!]$/.test(q.deprecated.note) ? '' : '.') : ''
    L.push('::: warning Deprecated', `This query is deprecated.${repl}${note}`, ':::', '')
  }
  if (q.versionGate?.until || q.versionGate?.since) {
    const g = q.versionGate
    const text = [
      g.until && `Resolves only in files whose Molang version is below **${g.until}**; from ${g.until} on, the name is an unknown query.`,
      g.since && `Resolves only in files whose Molang version is **${g.since}** or later.`,
    ].filter(Boolean).join(' ')
    L.push('::: info Version gate', text, ':::', '')
  }

  L.push(render(q.long, q.name), '')
  if (q.formula) L.push(`**Formula:** ${render(q.formula, q.name)}`, '')

  if (q.example) L.push('## Example', '', '```molang', q.example, '```', '')

  if (q.args.length) {
    L.push('## Arguments', '', '| argument | type | accepts | default | if invalid |', '|---|---|---|---|---|')
    for (const a of q.args) {
      const name = `\`${a.name}\`${a.optional ? ' (optional)' : ''}${a.variadic ? ' (repeats)' : ''}`
      const accepts = [a.description && cell(a.description, q.name), ...(a.accepts ?? []).map((x) => '• ' + cell(x, q.name))].filter(Boolean).join('<br>')
      const def = a.default === undefined ? '-' : `\`${typeof a.default === 'string' ? a.default : JSON.stringify(a.default)}\``
      L.push(`| ${name} | \`${a.type ?? 'any'}\` | ${accepts || '-'} | ${def} | ${a.onInvalid ? cell(a.onInvalid, q.name) : '-'} |`)
    }
    L.push('')
  }

  if (q.behaviourChangesByVersion?.length) {
    L.push('## Changes between versions', '', 'Which behaviour a file gets depends on its Molang version (the `format_version` it is read at).', '')
    L.push('| Molang version | behaviour |', '|---|---|')
    for (const c of q.behaviourChangesByVersion) {
      const range = c.since && c.until ? `${c.since} to below ${c.until}` : c.since ? `${c.since} and later` : `below ${c.until}`
      L.push(`| ${range} | ${cell(c.change, q.name)} |`)
    }
    L.push('')
  }

  if (q.whenUnavailable.length) {
    L.push("## When it can't answer", '', '| situation | result | content log |', '|---|---|---|')
    for (const w of q.whenUnavailable) {
      const value = typeof w.value === 'number' ? `\`${w.value}\`` : cell(w.value, q.name)
      L.push(`| ${cell(w.when, q.name)} | ${value} | ${w.logs ? 'yes' : 'no'} |`)
    }
    L.push('')
  }

  L.push('## Where it works', '')
  const ctx = (q.appliesTo?.contexts ?? []).map((c) => CONTEXT_NAMES[c] ?? c)
  L.push(ctx.length ? `**Works in:** ${ctx.join(', ')}.` : '**Works in:** -')
  if (q.appliesTo?.entities) L.push('', `**Entities:** ${render(q.appliesTo.entities, q.name)}`)
  if (q.appliesTo?.fields?.length) L.push('', `**Only in:** ${q.appliesTo.fields.map((f) => `\`${f}\``).join(', ')}`)
  L.push('')

  L.push('## In vanilla', '')
  const vu = q.vanillaUsage ?? { count: 0, fileCount: 0, examples: [] }
  if (!vu.count) L.push('Not used in the vanilla resource and behavior packs.', '')
  else {
    L.push(`Used ${vu.count} time${vu.count === 1 ? '' : 's'} in ${vu.fileCount} file${vu.fileCount === 1 ? '' : 's'} of the vanilla packs.`, '')
    for (const e of vu.examples) {
      // Excerpts are quoted from pack JSON: a whole `"key": "..."` line reads better as JSON.
      const lang = /^\s*["{\[]/.test(e.expr) ? 'json' : 'molang'
      L.push(`\`${e.file}\``, '', '```' + lang, e.expr, '```', '')
    }
  }

  if (q.pitfalls.length) {
    L.push('## Pitfalls', '')
    for (const p of q.pitfalls) L.push(`- ${render(p, q.name).replace(/\n+/g, ' ')}`)
    L.push('')
  }

  if (q.related.length) {
    L.push('## Related', '')
    for (const r of q.related) {
      const n = r.replace(/^query\./, '')
      const other = data.queries.find((x) => x.name === n)
      L.push(`- [\`query.${n}\`](./${n})${other ? ' - ' + escapeProse(other.summary) : ''}`)
    }
    L.push('')
  }

  L.push(`<p class="mg-verified mg-verified--${q.verification.status}">${escapeProse(q.verification.label)}.</p>`, '')
  return L.join('\n')
}

// ---------------------------------------------------------------------------
// Math, namespaces, operators: fragments included by math.md and molang.md.

function mathSignature(f) {
  if (f.constant || !f.args) return `math.${f.name}`
  return `math.${f.name}(${f.args.map((a) => (a.optional ? `[${a.name}]` : a.name)).join(', ')})`
}

function mathFragment() {
  const L = []
  for (const f of [...math].sort((a, b) => a.name.localeCompare(b.name))) {
    L.push(`### math.${f.name}`, '', '```molang', mathSignature(f), '```', '')
    L.push(`returns \`${f.returns}\``, '')
    L.push(render(f.description || f.summary, ''), '')
    if (f.args?.length) {
      L.push('| argument | type | meaning |', '|---|---|---|')
      for (const a of f.args) L.push(`| \`${a.name}\`${a.optional ? ' (optional)' : ''} | \`${a.type ?? 'number'}\` | ${cell(a.description ?? '', '')} |`)
      L.push('')
    }
    if (f.example) L.push('```molang', f.example, '```', '')
    for (const n of f.notes ?? []) L.push(`- ${render(n, '')}`)
    if (f.notes?.length) L.push('')
  }
  return L.join('\n')
}

function namespacesFragment() {
  const L = []
  const aliases = new Map()
  for (const n of ns.namespaces) if (n.aliasOf) aliases.set(n.aliasOf, [...(aliases.get(n.aliasOf) ?? []), n.name])
  for (const n of ns.namespaces) {
    if (n.aliasOf) continue
    L.push(`### ${n.name}`, '')
    const short = aliases.get(n.name)
    const facts = [short && `short form: ${short.map((s) => `\`${s}.\``).join(', ')}`, n.assignable !== undefined && (n.assignable ? 'assignable' : 'read-only')].filter(Boolean)
    if (facts.length) L.push(`<span class="mg-facts">${facts.join(' · ')}</span>`, '')
    L.push(render(n.description || n.summary || '', ''), '')
    if (n.example) L.push('```molang', n.example, '```', '')
    for (const x of n.notes ?? []) L.push(`- ${render(x, '')}`)
    if (n.notes?.length) L.push('')
    if (n.name === 'query') L.push(`Every query has its own page: see the [query reference](/queries/).`, '')
    if (n.knownNames?.length) {
      L.push('| name | supplied in | meaning |', '|---|---|---|')
      for (const k of n.knownNames) L.push(`| \`${n.name}.${k.name}\` | ${cell((k.contexts ?? []).join('; '), '')} | ${cell(k.description ?? '', '')} |`)
      L.push('')
    }
  }
  return L.join('\n')
}

const OPERATOR_IDS = {
  '->': 'op-arrow', '??': 'op-null-coalescing', '?:': 'op-binary-conditional', '? :': 'op-ternary', loop: 'op-loop',
  for_each: 'op-for-each', break: 'op-break', continue: 'op-continue', return: 'op-return', '{}': 'op-braces', ';': 'op-semicolon', "'...'": 'op-strings',
}

function operatorsFragment() {
  const L = []
  ns.operators.forEach((o, i) => {
    L.push(`### \`${o.token}\` {#${OPERATOR_IDS[o.token] ?? `op-${i}`}}`, '')
    if (o.summary) L.push(`*${escapeProse(o.summary)}*`, '')
    if (o.description) L.push(render(o.description, ''), '')
    if (o.example) L.push('```molang', o.example, '```', '')
    for (const x of o.notes ?? []) L.push(`- ${render(x, '')}`)
    if (o.notes?.length) L.push('')
  })
  return L.join('\n')
}

// ---------------------------------------------------------------------------
// Write. The queries directory holds only generated pages besides index.md,
// so stale pages (a query that left the catalogue) are removed first.

const queriesDir = path.join(siteDir, 'queries')
const genDir = path.join(siteDir, 'generated')
fs.mkdirSync(queriesDir, { recursive: true })
fs.mkdirSync(genDir, { recursive: true })
for (const f of fs.readdirSync(queriesDir)) if (f.endsWith('.md') && f !== 'index.md') fs.rmSync(path.join(queriesDir, f))

for (const q of data.queries) fs.writeFileSync(path.join(queriesDir, `${q.name}.md`), queryPage(q))
fs.writeFileSync(path.join(genDir, 'math.md'), mathFragment())
fs.writeFileSync(path.join(genDir, 'namespaces.md'), namespacesFragment())
fs.writeFileSync(path.join(genDir, 'operators.md'), operatorsFragment())

const index = data.queries.map((q) => ({
  name: q.name,
  summary: q.summary,
  set: q.querySet,
  side: q.side.split(' ')[0],
  deprecated: !!q.deprecated,
  gate: q.versionGate ? { since: q.versionGate.since ?? null, until: q.versionGate.until ?? null } : null,
  clientOnly: q.clientOnly,
  verification: q.verification.status,
}))
fs.writeFileSync(path.join(genDir, 'query-index.json'), JSON.stringify({ gameVersion: data.gameVersion, sets: SET_NAMES, queries: index }))

// The sidebar: one group per query set, the large default set split by first letter.
const sidebar = []
for (const [set, title] of Object.entries(SET_NAMES)) {
  const qs = data.queries.filter((q) => q.querySet === set)
  if (!qs.length) continue
  const item = (q) => ({ text: q.name, link: `/queries/${q.name}` })
  if (qs.length <= 30) {
    sidebar.push({ text: title, collapsed: true, items: qs.map(item) })
    continue
  }
  const letters = new Map()
  for (const q of qs) {
    const l = q.name[0].toUpperCase()
    letters.set(l, [...(letters.get(l) ?? []), q])
  }
  sidebar.push({ text: title, collapsed: false, items: [...letters].map(([l, list]) => ({ text: l, collapsed: true, items: list.map(item) })) })
}
fs.writeFileSync(path.join(genDir, 'sidebar.json'), JSON.stringify(sidebar))

console.log(`docs/site: ${data.queries.length} query pages, ${math.length} math functions, ${ns.namespaces.length} namespaces, ${ns.operators.length} operators`)
