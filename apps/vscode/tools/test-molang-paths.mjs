#!/usr/bin/env node
// Runs both checks on the Molang path catalogue:
//
//   1. freshness: rebuilds molang-paths.json and molang-paths.sources.md from
//      the schemas into a temporary directory and compares them with the
//      committed files. Skipped (with a note) when either schema set is not on
//      this machine, since a partial rebuild would differ for that reason
//      alone.
//   2. golden: check-molang-paths.mjs over the vanilla packs.
//
// Usage:
//   node apps/vscode/tools/test-molang-paths.mjs --samples <bedrock-samples> [--blockception <dir>]
// or set BEDROCK_SAMPLES. The samples checkout needs behavior_pack/,
// resource_pack/ and, for the freshness step, metadata/json_schemas/.

import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const dataDir = path.resolve(here, '..', 'data');

const args = {};
for (let i = 2; i < process.argv.length; i++) {
  const k = process.argv[i];
  if (!k.startsWith('--')) throw new Error(`unexpected argument ${k}`);
  args[k.slice(2)] = process.argv[++i];
}
const samples = args.samples ?? process.env.BEDROCK_SAMPLES;
if (!samples) {
  console.error('test-molang-paths: pass --samples <bedrock-samples checkout> or set BEDROCK_SAMPLES');
  process.exit(2);
}

let failed = false;
const node = (script, extra) => spawnSync(process.execPath, [path.join(here, script), ...extra], { stdio: 'inherit' });

// 1. freshness
const mojang = path.join(samples, 'metadata', 'json_schemas');
const bcArgs = args.blockception ? ['--blockception', args.blockception] : [];
if (!fs.existsSync(mojang)) {
  console.log(`freshness: skipped (no ${mojang})`);
} else {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'molang-paths-'));
  const out = path.join(tmp, 'molang-paths.json');
  const src = path.join(tmp, 'molang-paths.sources.md');
  const r = spawnSync(process.execPath, [path.join(here, 'build-molang-paths.mjs'), '--mojang', mojang, ...bcArgs, '--out', out, '--sources', src], { encoding: 'utf8' });
  if (r.status !== 0) {
    console.error(r.stdout + r.stderr);
    failed = true;
  } else if (/schemas not found/.test(r.stderr)) {
    console.log('freshness: skipped (Blockception schemas not found; pass --blockception)');
  } else {
    for (const [name, file] of [['molang-paths.json', out], ['molang-paths.sources.md', src]]) {
      const same = fs.readFileSync(file, 'utf8') === fs.readFileSync(path.join(dataDir, name), 'utf8');
      console.log(`freshness: ${name} ${same ? 'up to date' : 'STALE -- rerun build-molang-paths.mjs'}`);
      if (!same) failed = true;
    }
  }
  fs.rmSync(tmp, { recursive: true, force: true });
}

// 2. golden
const g = node('check-molang-paths.mjs', ['--samples', samples]);
if (g.status !== 0) failed = true;

process.exit(failed ? 1 : 0);
