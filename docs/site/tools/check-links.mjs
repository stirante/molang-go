#!/usr/bin/env node
// check-links.mjs -- every link on the site resolves, and the route contract holds.
//
// Usage:  node docs/site/tools/check-links.mjs [--verify-dist]
//
// Run after tools/generate.mjs (npm run build does that first). Checks:
//
//   1. Site pages -> site pages. Every Markdown link on every page (includes expanded,
//      generated query pages included): a relative or site-absolute route names a page, and
//      its #anchor is one that page defines under VitePress's slug rule. Plus the literal
//      links in .vitepress/config.mts.
//   2. The route contract. query.<name> is served at /queries/<name>, and the extension links
//      there by name alone. So: every query in apps/vscode/catalogue/catalogue.json has a page
//      at queries/<name>.md and a `docs` field that is exactly that URL (rules.mjs docsUrl),
//      there is no page for a query the catalogue does not have, and the site's base is the
//      one the URLs carry.
//   3. Product -> site. Every https://stirante.github.io/molang-go/... URL in a tracked file
//      outside docs/ (git ls-files) names a page, and its anchor exists.
//
//   --verify-dist   after `vitepress build`, also compare every anchor this script computed
//                   with the ids VitePress wrote into .vitepress/dist, which proves the slug
//                   port in lib/anchors.mjs.
import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { expandIncludes, pageAnchors } from './lib/anchors.mjs'
import { DOCS_BASE, DOCS_ORIGIN, docsUrl } from '../../../tools/catalogue/rules.mjs'

const toolsDir = path.dirname(fileURLToPath(import.meta.url))
const siteDir = path.resolve(toolsDir, '..')
const repoRoot = path.resolve(siteDir, '..', '..')
const verifyDist = process.argv.includes('--verify-dist')

const SKIP_DIRS = ['node_modules', '.vitepress', 'generated', 'tools', 'data']
const EXCLUDED = new Set(['README.md'])

function listPages(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, entry.name)
    if (entry.isDirectory()) {
      if (!SKIP_DIRS.includes(entry.name)) listPages(p, out)
    } else if (entry.name.endsWith('.md') && !(dir === siteDir && EXCLUDED.has(entry.name))) {
      out.push(p)
    }
  }
  return out
}

/** A site route (`queries/is_baby`, `queries/`, `molang`) to the page file that produces it. */
function routeToFile(route) {
  const clean = route.replace(/^\//, '').replace(/\/$/, '').replace(/\.html$/, '').replace(/\.md$/, '')
  const candidates = clean === '' ? ['index.md'] : [`${clean}.md`, path.join(clean, 'index.md')]
  for (const c of candidates) {
    const f = path.join(siteDir, c)
    if (fs.existsSync(f)) return f
  }
  return undefined
}

const problems = []
const anchorCache = new Map()
const anchorsFor = (file) => {
  if (!anchorCache.has(file)) anchorCache.set(file, pageAnchors(file))
  return anchorCache.get(file)
}
let linkCount = 0

function checkLink(page, raw, line) {
  const where = `${path.relative(repoRoot, page)}:${line}`
  const target = raw.trim().replace(/^<|>$/g, '')
  if (target.startsWith('mailto:') || /^https?:\/\//.test(target)) return
  linkCount++
  const [pathPart, anchor] = target.split('#')
  if (pathPart === '') {
    if (!anchorsFor(page).has(anchor)) problems.push(`${where}  #${anchor}  -- no heading on this page has that anchor`)
    return
  }
  let file
  if (pathPart.startsWith('/')) {
    file = routeToFile(pathPart)
    if (!file) return problems.push(`${where}  ${target}  -- no page produces that route`)
  } else {
    const resolved = path.resolve(path.dirname(page), pathPart)
    if (pathPart.endsWith('.md')) file = resolved
    else if (fs.existsSync(resolved) && fs.statSync(resolved).isDirectory()) file = path.join(resolved, 'index.md')
    else if (fs.existsSync(resolved)) return
    else file = `${resolved}.md`
    if (!fs.existsSync(file)) return problems.push(`${where}  ${target}  -- file does not exist`)
  }
  if (anchor && !anchorsFor(file).has(anchor)) problems.push(`${where}  ${target}  -- ${path.relative(siteDir, file)} has no anchor "${anchor}"`)
}

// 1. Site pages.
const pages = listPages(siteDir)
if (!fs.existsSync(path.join(siteDir, 'generated'))) {
  console.error('docs/site: generated/ is missing; run `npm run generate` (or `npm run build`) first')
  process.exit(1)
}
for (const page of pages) {
  const raw = fs.readFileSync(page, 'utf-8')
  for (const m of raw.matchAll(/<!--\s*@include:\s*([^\s>]+)\s*-->/g)) {
    if (!fs.existsSync(path.resolve(path.dirname(page), m[1]))) problems.push(`${path.relative(repoRoot, page)}  @include ${m[1]}  -- file does not exist`)
  }
  const { text } = expandIncludes(page, raw)
  let inFence = false
  text.split('\n').forEach((lineText, i) => {
    if (/^\s*(```|~~~)/.test(lineText)) inFence = !inFence
    if (inFence) return
    for (const m of lineText.matchAll(/\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g)) checkLink(page, m[1], i + 1)
  })
}
const config = fs.readFileSync(path.join(siteDir, '.vitepress', 'config.mts'), 'utf-8')
for (const m of config.matchAll(/link:\s*'(\/[^'#]*)(?:#([^']*))?'/g)) {
  linkCount++
  const file = routeToFile(m[1])
  if (!file) problems.push(`.vitepress/config.mts  link ${m[1]}  -- no page produces that route`)
  else if (m[2] && !anchorsFor(file).has(m[2])) problems.push(`.vitepress/config.mts  link ${m[1]}#${m[2]}  -- no such anchor`)
}

// 2. The route contract.
const configBase = /DOCS_BASE \?\? '([^']+)'/.exec(config)?.[1]
if (configBase !== DOCS_BASE) problems.push(`.vitepress/config.mts base ${configBase} differs from tools/catalogue/rules.mjs DOCS_BASE ${DOCS_BASE}`)
const catalogue = JSON.parse(fs.readFileSync(path.join(repoRoot, 'apps', 'vscode', 'catalogue', 'catalogue.json'), 'utf-8'))
const names = new Set(catalogue.queries.map((q) => q.name))
for (const q of catalogue.queries) {
  if (q.docs !== docsUrl(q.name)) problems.push(`catalogue.json query.${q.name}: docs ${q.docs}, the contract says ${docsUrl(q.name)}`)
  if (!fs.existsSync(path.join(siteDir, 'queries', `${q.name}.md`))) problems.push(`catalogue.json query.${q.name}: no page queries/${q.name}.md`)
}
for (const f of fs.readdirSync(path.join(siteDir, 'queries'))) {
  const n = f.replace(/\.md$/, '')
  if (f.endsWith('.md') && f !== 'index.md' && !names.has(n)) problems.push(`queries/${f}: no such query in the catalogue`)
}

// 3. Product -> site.
const siteUrl = new RegExp(`${DOCS_ORIGIN.replace(/[.]/g, '\\.')}${DOCS_BASE}([^\\s"'\`)>#]*)(?:#([^\\s"'\`)>]*))?`, 'g')
const tracked = execFileSync('git', ['ls-files'], { cwd: repoRoot, encoding: 'utf8' }).split('\n').filter((f) => f && !f.startsWith('docs/'))
let productLinks = 0
for (const f of tracked) {
  const abs = path.join(repoRoot, f)
  if (!fs.existsSync(abs) || fs.statSync(abs).size > 8 << 20) continue
  const text = fs.readFileSync(abs, 'utf8')
  if (!text.includes(DOCS_ORIGIN + DOCS_BASE)) continue
  for (const m of text.matchAll(siteUrl)) {
    productLinks++
    const route = m[1].replace(/[.,;:!?*_]+$/, '')
    const file = routeToFile(route)
    if (!file) problems.push(`${f}  ${m[0]}  -- no page produces that route`)
    else if (m[2] && !anchorsFor(file).has(m[2])) problems.push(`${f}  ${m[0]}  -- no such anchor`)
  }
}

// --verify-dist.
if (verifyDist) {
  const dist = path.join(siteDir, '.vitepress', 'dist')
  if (!fs.existsSync(dist)) problems.push('--verify-dist: .vitepress/dist does not exist; run the build first')
  else {
    let checked = 0
    for (const page of pages) {
      const route = path.relative(siteDir, page).replace(/\\/g, '/').replace(/\.md$/, '')
      const html = [path.join(dist, `${route}.html`), path.join(dist, route, 'index.html')].find((f) => fs.existsSync(f))
      if (!html) {
        problems.push(`--verify-dist: no built HTML for ${route}`)
        continue
      }
      const built = new Set([...fs.readFileSync(html, 'utf-8').matchAll(/\sid="([^"]+)"/g)].map((m) => m[1]))
      for (const id of anchorsFor(page)) {
        checked++
        if (!built.has(id)) problems.push(`--verify-dist: ${route} -- computed anchor "${id}" is not an id in the built HTML`)
      }
    }
    console.log(`docs/site: --verify-dist compared ${checked} computed anchors against the built HTML`)
  }
}

if (problems.length) {
  console.error(`docs/site: ${problems.length} problem(s):\n`)
  for (const p of problems.slice(0, 200)) console.error('  ' + p)
  process.exit(1)
}
console.log(`docs/site: ${linkCount} links across ${pages.length} pages, ${names.size} query routes, ${productLinks} product links: all resolving`)
