// Runs the web smoke tests: the extension's browser build in VS Code for the
// Web, in a headless Chromium, over the integration fixtures. This is the
// build vscode.dev and github.dev load, so it is what proves the language
// server comes up in a Web Worker and finds its module and catalogue by
// fetch. `npm run test:web` builds everything first.
//
// The first run downloads a VS Code for the Web build into .vscode-test-web;
// the browser is Playwright's Chromium (npx playwright install chromium).

import * as path from 'node:path';
import { runTests } from '@vscode/test-web';

async function main() {
  // This file is bundled to dist/test/runWeb.js.
  const root = path.resolve(__dirname, '..', '..');
  try {
    await runTests({
      browserType: 'chromium',
      headless: !process.argv.includes('--show'),
      extensionDevelopmentPath: root,
      extensionTestsPath: path.join(__dirname, 'web', 'suite.js'),
      folderPath: path.join(root, 'test', 'integration', 'fixtures'),
      quality: 'stable',
    });
  } catch (e) {
    console.error('web tests failed:', e);
    process.exit(1);
  }
}

void main();
