// Builds the extension: the WebAssembly module from the Go source two
// directories up, then the client and server bundles.
//
//   node scripts/build.mjs               everything
//   node scripts/build.mjs --wasm-only   just the module and wasm_exec.js
//   node scripts/build.mjs --watch       rebundle the TypeScript on change
//   node scripts/build.mjs --integration also bundle the integration tests
//
// wasm_exec.js, the JavaScript half of Go's WebAssembly support, is copied
// from the Go installation that builds the module rather than kept in the
// repository: the two must come from the same Go release, and copying it at
// build time is what guarantees that.

import { execFileSync } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync, statSync } from 'node:fs';
import * as path from 'node:path';
import { fileURLToPath } from 'node:url';
import * as esbuild from 'esbuild';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(here, '..');
const repo = path.resolve(root, '..', '..');
const args = new Set(process.argv.slice(2));

function buildWasm() {
  const out = path.join(root, 'dist', 'molang.wasm');
  mkdirSync(path.dirname(out), { recursive: true });
  const env = { ...process.env, GOOS: 'js', GOARCH: 'wasm' };
  const t0 = Date.now();
  execFileSync('go', ['build', '-trimpath', '-ldflags=-s -w', '-o', out, './cmd/molang-wasm'], {
    cwd: repo,
    env,
    stdio: 'inherit',
  });
  const goroot = execFileSync('go', ['env', 'GOROOT'], { encoding: 'utf8' }).trim();
  // lib/wasm since Go 1.24; misc/wasm before it.
  const candidates = [path.join(goroot, 'lib', 'wasm', 'wasm_exec.js'), path.join(goroot, 'misc', 'wasm', 'wasm_exec.js')];
  const execJs = candidates.find((p) => existsSync(p));
  if (!execJs) throw new Error(`wasm_exec.js not found under ${goroot}`);
  const generated = path.join(root, 'src', 'server', 'generated');
  mkdirSync(generated, { recursive: true });
  copyFileSync(execJs, path.join(generated, 'wasm_exec.js'));
  const size = statSync(out).size;
  console.log(`molang.wasm: ${(size / 1024 / 1024).toFixed(2)} MB in ${Date.now() - t0} ms`);
}

const common = {
  bundle: true,
  // ESM builds first: jsonc-parser's UMD build loads its parts with a
  // require() esbuild cannot follow, and fails at run time.
  mainFields: ['module', 'main'],
  platform: 'node',
  format: 'cjs',
  target: 'node20',
  sourcemap: true,
  logLevel: 'info',
};

const bundles = [
  { ...common, entryPoints: [path.join(root, 'src', 'extension.ts')], outfile: path.join(root, 'dist', 'extension.js'), external: ['vscode'] },
  { ...common, entryPoints: [path.join(root, 'src', 'server', 'node.ts')], outfile: path.join(root, 'dist', 'server.js') },
];

const integration = [
  {
    ...common,
    entryPoints: [path.join(root, 'test', 'integration', 'runIntegration.ts')],
    outfile: path.join(root, 'dist', 'test', 'runIntegration.js'),
    external: ['@vscode/test-electron'],
  },
  {
    ...common,
    entryPoints: [path.join(root, 'test', 'integration', 'suite.ts')],
    outfile: path.join(root, 'dist', 'test', 'suite.js'),
    external: ['vscode'],
  },
];

if (args.has('--integration')) {
  for (const b of integration) await esbuild.build(b);
} else {
  buildWasm();
  if (!args.has('--wasm-only')) {
    if (args.has('--watch')) {
      for (const b of bundles) await (await esbuild.context(b)).watch();
    } else {
      for (const b of bundles) await esbuild.build({ ...b, minify: true });
    }
  }
}
