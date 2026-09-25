// Takes the README's screenshots (media/*.png) of the web build in VS Code
// for the Web, driven by Playwright, over the small workspace in
// scripts/screenshots/. Pictures of the real extension rather than mock-ups,
// and reproducible: rerun this after a change the pictures show.
//
// The web build is used because it needs no display and no desktop install;
// its features are the desktop build's, which is the point of the shared
// server.
//
// Usage: npm run build, then node scripts/screenshots.mjs [--port 3100]

import { spawn } from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');
const media = path.join(root, 'media');
const portFlag = process.argv.indexOf('--port');
const port = portFlag === -1 ? 3100 : Number(process.argv[portFlag + 1]);

const server = spawn(
  process.execPath,
  [
    path.join(root, 'node_modules', '@vscode', 'test-web', 'out', 'server', 'index.js'),
    '--browserType=none',
    `--port=${port}`,
    '--quality=stable',
    `--extensionDevelopmentPath=${root}`,
    path.join(here, 'screenshots'),
  ],
  { stdio: ['ignore', 'pipe', 'inherit'] },
);
await new Promise((resolve, reject) => {
  server.stdout.on('data', (d) => {
    if (String(d).includes('Listening on')) resolve();
  });
  server.on('exit', (code) => reject(new Error(`test-web exited with ${code}`)));
});

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 960, height: 480 }, deviceScaleFactor: 2 });
  await page.goto(`http://localhost:${port}`);
  await page.waitForSelector('.monaco-workbench', { timeout: 120000 });
  await sleep(3000);
  // Focus the workbench first, or the key presses below go nowhere.
  await page.click('.monaco-workbench .part.statusbar');
  const key = (k) => page.keyboard.press(k);
  const command = async (name) => {
    await key('F1');
    await page.waitForSelector('.quick-input-widget:not([style*="display: none"])');
    await page.keyboard.type(name);
    await sleep(500);
    await key('Enter');
    await sleep(500);
  };
  // Keybindings rather than the command palette where there is one: the
  // palette's fuzzy match can pick a neighbouring command (a toggle for a
  // close, the theme marketplace for the theme picker).
  await key('Control+K');
  await key('Control+T');
  await page.waitForSelector('.quick-input-widget:not([style*="display: none"])');
  await page.keyboard.type('Dark Modern');
  await sleep(500);
  await key('Enter');
  await sleep(800);
  // Files are opened from the Explorer, which the workbench starts with:
  // VS Code for the Web has no file search over the mounted folder, so
  // Quick Open finds nothing. The side bar is hidden again after, so the
  // editor is the subject.
  let sideBar = true;
  const open = async (file) => {
    // One editor at a time, so no other tab's problems show in the picture.
    await key('Control+K');
    await key('Control+W');
    if (!sideBar) await key('Control+Shift+E');
    await page.getByRole('treeitem', { name: file, exact: true }).first().click();
    await sleep(300);
    await key('Control+B');
    sideBar = false;
    // Long enough for the server's first diagnostics.
    await sleep(2500);
    await page.click('.monaco-editor .view-lines');
  };
  const goTo = async (line, col) => {
    await key('Control+G');
    await sleep(200);
    await page.keyboard.type(`${line}:${col}`);
    await key('Enter');
    await sleep(300);
  };
  const shot = async (name) => {
    await sleep(800);
    // Below the title bar, which names the test harness rather than
    // anything a user would see.
    const top = (await page.locator('.part.titlebar').boundingBox())?.height ?? 0;
    const size = page.viewportSize();
    await page.screenshot({ path: path.join(media, name), clip: { x: 0, y: top, width: size.width, height: size.height - top } });
    console.log(`wrote media/${name}`);
  };
  const hover = async () => {
    await command('Show or Focus Hover');
    await sleep(600);
  };

  fs.mkdirSync(media, { recursive: true });

  await open('walk.molang');
  await goTo(4, 38);
  await hover();
  await shot('diagnostics.png');
  await key('Escape');

  await goTo(2, 17);
  await hover();
  await shot('hover.png');
  await key('Escape');

  await open('completion.molang');
  await key('Control+End');
  await key('Control+Space');
  await sleep(1200);
  await page.keyboard.type('modified_mo');
  await sleep(800);
  // Again, for the selected item's documentation beside the list.
  await key('Control+Space');
  await sleep(1000);
  await shot('completion.png');
  await key('Escape');

  await open('mob.animation_controllers.json');
  await goTo(12, 56);
  await hover();
  await shot('json.png');
} finally {
  await browser.close();
  server.kill();
}
