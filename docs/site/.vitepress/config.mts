// VitePress configuration for the molang-go documentation site.
//
// Everything under queries/ except index.md, and everything under generated/,
// is written by tools/generate.mjs before each dev or build run, from
// data/queries.json and the extension's catalogue files; see that script.
import { defineConfig } from 'vitepress'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const siteDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const repoDir = path.resolve(siteDir, '..', '..')

/** GitHub Pages serves a project site under /<repo>/. The extension's links
 * carry the full origin plus this base (tools/catalogue/rules.mjs, DOCS_BASE),
 * so a change here is a change there too; check-links.mjs compares the two. */
const base = process.env.DOCS_BASE ?? '/molang-go/'

const data = JSON.parse(fs.readFileSync(path.join(siteDir, 'data', 'queries.json'), 'utf-8'))
const sidebarFile = path.join(siteDir, 'generated', 'sidebar.json')
const querySidebar = fs.existsSync(sidebarFile) ? JSON.parse(fs.readFileSync(sidebarFile, 'utf-8')) : []

// Code fences marked `molang` are highlighted with the extension's own grammar.
const molangGrammar = {
  ...JSON.parse(fs.readFileSync(path.join(repoDir, 'apps', 'vscode', 'syntaxes', 'molang.tmLanguage.json'), 'utf-8')),
  name: 'molang',
}

export default defineConfig({
  title: 'molang-go',
  description: `Molang for Minecraft Bedrock: the language, and every query, documented against ${data.gameVersion}.`,
  base,
  lang: 'en',
  lastUpdated: true,

  // queries/is_baby.md is served at /queries/is_baby: no .html, no trailing
  // slash. That is the route contract the extension's links rely on.
  cleanUrls: true,

  srcExclude: ['generated/**', 'node_modules/**', 'README.md', 'data/**'],
  ignoreDeadLinks: false,
  sitemap: { hostname: 'https://stirante.github.io' + base },

  markdown: {
    languages: [molangGrammar as any],
    headers: { level: [2, 3] },
  },

  themeConfig: {
    outline: { level: [2, 3], label: 'On this page' },
    search: { provider: 'local' },
    socialLinks: [{ icon: 'github', link: 'https://github.com/stirante/molang-go' }],
    footer: {
      message: `The query reference is a statement about Minecraft Bedrock ${data.gameVersion}; each page says how far it has been verified in game.`,
    },
    nav: [
      { text: 'Molang', link: '/molang', activeMatch: '^/molang' },
      { text: 'Queries', link: '/queries/', activeMatch: '^/queries/' },
      { text: 'Math', link: '/math', activeMatch: '^/math' },
    ],
    sidebar: {
      '/queries/': [{ text: 'Queries', items: [{ text: 'All queries', link: '/queries/' }] }, ...querySidebar],
      '/': [
        {
          text: 'Molang',
          items: [
            { text: 'Overview', link: '/molang' },
            { text: 'Namespaces', link: '/molang#namespaces' },
            { text: 'Operators and keywords', link: '/molang#operators-and-keywords' },
            { text: 'Math functions', link: '/math' },
            { text: 'Query reference', link: '/queries/' },
          ],
        },
      ],
    },
  },
})
