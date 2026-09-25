// Draws media/icon.png, the Marketplace listing's icon.
//
// Generated rather than drawn and dropped in, so that changing the icon is an
// edit to this file rather than a binary nobody can reproduce. The SVG below
// is rendered in Playwright's headless Chromium (already here for the web
// tests) and screenshotted at 128x128, the size the Marketplace asks for.
//
// What it draws: an M, for Molang, and the dot of `q.` after it -- the
// accessor every Molang expression is written with. Flat shapes and strokes
// no thinner than 12px at 128, because the editor's extension list shows the
// icon at 32 and a thin line there is a smudge; `--check` writes a 32px copy
// to the temp directory so that can be looked at. No Minecraft asset, font or
// texture, and nothing that imitates one: the mark is the language's, not the
// game's. No text either, so no font can change it between machines.
//
// Usage: node scripts/make-icon.mjs [--out <file>] [--check]

import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');
const outFlag = process.argv.indexOf('--out');
const outPath = outFlag === -1 ? path.join(root, 'media', 'icon.png') : path.resolve(process.argv[outFlag + 1] ?? '');
const check = process.argv.includes('--check');

const SIZE = 128;

const C = {
  tile: '#1f2430',
  letter: '#4ec9b0', // the editor's dark-theme colour for types
  dot: '#dcdcaa', // and for functions: the query the dot leads to
};

const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${SIZE}" height="${SIZE}" viewBox="0 0 ${SIZE} ${SIZE}">
  <rect x="0" y="0" width="128" height="128" rx="24" ry="24" fill="${C.tile}"/>
  <polyline points="22,96 22,34 52,70 82,34 82,96" fill="none" stroke="${C.letter}" stroke-width="15"
    stroke-linecap="round" stroke-linejoin="round"/>
  <circle cx="106" cy="90" r="11" fill="${C.dot}"/>
</svg>`;

const browser = await chromium.launch();
try {
  // `viewport`, not `viewportSize`: Playwright ignores option names it does
  // not know, and the misspelling leaves the page at 1280x720 with the
  // drawing in one corner. The written file is measured below for that
  // reason.
  const page = await browser.newPage({ viewport: { width: SIZE, height: SIZE }, deviceScaleFactor: 1 });
  await page.setContent(
    `<!doctype html><meta charset="utf-8"><style>html,body{margin:0;padding:0;width:${SIZE}px;height:${SIZE}px;background:transparent}svg{display:block}</style>${svg}`,
  );
  fs.mkdirSync(path.dirname(outPath), { recursive: true });
  await page.screenshot({ path: outPath, omitBackground: true });

  if (check) {
    // Into the temp directory, not media/: the check is not a shipped asset.
    const small = await browser.newPage({ viewport: { width: 32, height: 32 }, deviceScaleFactor: 1 });
    await small.setContent(
      `<!doctype html><meta charset="utf-8"><style>html,body{margin:0;padding:0;background:#f3f3f3}svg{display:block;width:32px;height:32px}</style>${svg}`,
    );
    const checkPath = path.join(os.tmpdir(), 'molang-icon-32.png');
    await small.screenshot({ path: checkPath });
    console.log(`wrote ${checkPath} (the 32px legibility check)`);
  }

  // Measured from the file rather than asked of the page, which reports
  // what it was told. A PNG's IHDR holds width and height as big-endian
  // uint32 at bytes 16 and 20.
  const written = fs.readFileSync(outPath);
  const w = written.readUInt32BE(16);
  const h = written.readUInt32BE(20);
  if (w !== SIZE || h !== SIZE) throw new Error(`make-icon: wrote ${w}x${h}, expected ${SIZE}x${SIZE}`);
  console.log(`wrote ${outPath} (${w}x${h})`);
} finally {
  await browser.close();
}
