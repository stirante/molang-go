// Runs the integration suite inside a real VS Code, downloaded on first use
// into .vscode-test. `npm run test:integration` builds everything first.
//
// It opens a window: VS Code has no headless mode. On a machine without a
// display, run it under a virtual one (xvfb-run on Linux).

import * as path from 'node:path';
import { runTests } from '@vscode/test-electron';

async function main() {
  // This file is bundled to dist/test/runIntegration.js.
  const root = path.resolve(__dirname, '..', '..');
  try {
    await runTests({
      extensionDevelopmentPath: root,
      extensionTestsPath: path.join(__dirname, 'suite.js'),
      launchArgs: [path.join(root, 'test', 'integration', 'fixtures'), '--disable-extensions', '--skip-welcome'],
    });
  } catch (e) {
    console.error('integration tests failed:', e);
    process.exit(1);
  }
}

void main();
