// The language service over the real WebAssembly module, as the server runs
// it. Needs `npm run build` first (npm test does it), for dist/molang.wasm and
// the wasm_exec.js copied beside the bridge.

import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import * as path from 'node:path';
import { TextDocument } from 'vscode-languageserver-textdocument';
import { DiagnosticSeverity } from 'vscode-languageserver-types';
import { beforeAll, describe, expect, it } from 'vitest';
import { MolangBridge } from '../../src/server/bridge';
import { Catalogue } from '../../src/server/catalogue';
import { JsonPathProvider } from '../../src/server/embedding';
import { MolangFileProvider } from '../../src/server/regions';
import { TOKEN_TYPES } from '../../src/server/semantic';
import { MolangService } from '../../src/server/service';
import { controller } from './embedding.test';
import { shippedCatalogue } from './shipped';

const root = path.resolve(__dirname, '..', '..');
let service: MolangService;

beforeAll(async () => {
  const t0 = performance.now();
  const bridge = await MolangBridge.load(readFileSync(path.join(root, 'dist', 'molang.wasm')));
  const json = shippedCatalogue();
  expect(bridge.setCatalogue(json).ok).toBe(true);
  service = new MolangService(bridge, Catalogue.parse(json), [new MolangFileProvider(), new JsonPathProvider()]);
  console.log(`bridge loaded in ${Math.round(performance.now() - t0)} ms`);
});

let version = 0;
const molang = (text: string) => TextDocument.create(`file:///t${++version}.molang`, 'molang', 1, text);
const json = (text: string) => TextDocument.create(`file:///t${++version}.json`, 'json', 1, text);

/** The document text each diagnostic covers, with its code. */
function covered(doc: TextDocument) {
  return service.diagnostics(doc).map((d) => [d.code, doc.getText(d.range)]);
}

describe('.molang documents', () => {
  it('reports every syntax error, and nothing in comments or templates', () => {
    const doc = molang(['# the ; in this comment is not Molang', 'v.a = ;', 'v.b = #{value};', 'v.c = );', ''].join('\n'));
    expect(covered(doc)).toEqual([
      ['syntax', ';'],
      ['syntax', ')'],
    ]);
  });

  it('says nothing about an empty file', () => {
    expect(service.diagnostics(molang(''))).toEqual([]);
    expect(service.diagnostics(molang('# only a comment\n'))).toEqual([]);
  });

  it('places a missing final semicolon right after the last token', () => {
    const doc = molang('v.a = 1 # set it\n');
    const [d] = service.diagnostics(doc);
    expect(d.message).toBe("complex expressions (contains either '=' or ';') must end with a ';'");
    expect(d.range.start).toEqual({ line: 0, character: 7 });
  });

  it('positions diagnostics after non-ASCII text correctly', () => {
    const doc = molang("t.s = 'zażółć 😀'; math.nope(1);");
    const [d] = service.diagnostics(doc);
    expect(doc.getText(d.range)).toBe('math.nope');
    expect(d.code).toBe('unknown-math');
  });

  it('hovers queries, math and variables', () => {
    const text = 'v.speed = q.ground_speed;\nt.x = math.clamp(v.speed, 0, 1);\nreturn t.x;';
    const doc = molang(text);
    const hover = (needle: string, add = 1) => {
      const h = service.hover(doc, text.indexOf(needle) + add);
      return h && (h.contents as { value: string }).value;
    };
    expect(hover('ground_speed')).toContain('query.ground_speed: number');
    expect(hover('clamp')).toContain('math.clamp(value: number, min: number, max: number): number');
    expect(hover('v.speed, 0', 3)).toContain('Written on line 1.');
    expect(hover('t.x;', 2)).toContain('Written on line 2.');
    expect(hover('q.ground', 0)).toContain('`query` namespace');
  });

  it('hovers operators, keywords and the context names the game supplies', () => {
    const text = 'v.a = v.b ?? c.is_first_person; loop(2, { v.a = this; });';
    const doc = molang(text);
    const hover = (needle: string) => {
      const h = service.hover(doc, text.indexOf(needle));
      return h && (h.contents as { value: string }).value;
    };
    expect(hover('??')).toMatch(/never set/);
    expect(hover('loop')).toContain('```molang\nloop');
    expect(hover('this')).toMatch(/keyword/);
    expect(hover('is_first_person')).toMatch(/first-person/);
  });

  it("completes the document's own variables and the catalogue's queries", () => {
    const text = 'v.speed = 1; v.';
    const labels = service.completion(molang(text), text.length).map((i) => i.label);
    expect(labels).toEqual(['speed']);
    const q = 'q.';
    expect(service.completion(molang(q), 2).map((i) => i.label)).toContain('is_baby');
    // Nothing inside a comment.
    expect(service.completion(molang('# q.'), 4)).toEqual([]);
  });

  it('offers signature help inside a call', () => {
    const text = 'math.lerp(0, ';
    const help = service.signatureHelp(molang(text), text.length)!;
    expect(help.signatures[0].label).toBe('math.lerp(start: number, end: number, t: number): number');
    expect(help.activeParameter).toBe(1);
  });

  it('colours tokens, and not inside comments or templates', () => {
    const text = 'v.x = #{v}; # q.y\nreturn v.x;';
    const doc = molang(text);
    const data = service.semanticTokens(doc)!;
    const tokens: string[] = [];
    let line = 0;
    let ch = 0;
    for (let i = 0; i < data.length; i += 5) {
      line += data[i];
      ch = data[i] === 0 ? ch + data[i + 1] : data[i + 1];
      const start = doc.offsetAt({ line, character: ch });
      tokens.push(`${text.slice(start, start + data[i + 2])}:${TOKEN_TYPES[data[i + 3]]}`);
    }
    expect(tokens).toEqual([
      'v:namespace',
      'x:variable',
      '=:operator',
      'return:keyword',
      'v:namespace',
      'x:variable',
    ]);
  });

  it('formats and minifies, but not a file with comments', () => {
    const doc = molang('v.a=q.is_baby?1:2;\n');
    expect(service.print(doc, 'format')).toEqual({ text: 'variable.a = query.is_baby ? 1 : 2;\n' });
    expect(service.print(doc, 'minify')).toEqual({ text: 'v.a=q.is_baby?1:2;\n' });
    expect(service.print(molang('v.a = 1; # keep me'), 'format')).toHaveProperty('error');
    expect(service.print(molang('v.a = ;'), 'format')).toHaveProperty('error');
  });

  it('outlines the variables', () => {
    const symbols = service.documentSymbols(molang('v.a = 1; t.b = v.a + c.item; return t.b;'));
    expect(symbols.map((s) => `${s.name} ${s.detail}`)).toEqual([
      'variable.a 1 write, 1 read',
      'temp.b 1 write, 1 read',
      'context.item 1 read',
    ]);
  });
});

describe('Molang in animation controller JSON', () => {
  it('maps diagnostics back through JSON escapes', () => {
    const text = controller.replace('&& q.is_baby', '&& ;').replace('v.count = 0;', 'v.count = math.nope(1);');
    const doc = json(text);
    expect(covered(doc)).toEqual([
      ['syntax', ';'],
      ['unknown-math', 'math.nope'],
    ]);
  });

  it('reports nothing about the valid controller', () => {
    expect(service.diagnostics(json(controller)).filter((d) => d.severity !== DiagnosticSeverity.Hint)).toEqual([]);
  });

  it('completes inside a JSON string, replacing only the name', () => {
    const text = controller.replace('q.is_moving', 'q.is_mo');
    const doc = json(text);
    const at = text.indexOf('q.is_mo') + 'q.is_mo'.length;
    const items = service.completion(doc, at);
    const item = items.find((i) => i.label === 'is_moving')!;
    const edit = item.textEdit as { range: { start: unknown; end: unknown }; newText: string };
    expect(doc.getText(edit.range as never)).toBe('is_mo');
    expect(edit.newText).toBe('is_moving');
    // Variables used anywhere in the file are offered in every region.
    const v = controller.replace('q.ground_speed', 'v.');
    const vd = json(v);
    expect(service.completion(vd, v.indexOf('"v."') + 3).map((i) => i.label)).toEqual(['x', 'count']);
  });

  it('hovers through escapes', () => {
    const doc = json(controller);
    const h = service.hover(doc, controller.indexOf('q.is_baby') + 3)!;
    expect(doc.getText(h.range!)).toBe('q.is_baby');
  });

  it('stands aside from Blockception where two providers do not mix', () => {
    const doc = json(controller);
    const before = service.environment;
    try {
      service.environment = { blockceptionActive: true, blockceptionJsonCompletion: true };
      expect(service.semanticTokens(doc)).toBeNull();
      expect(service.completion(doc, controller.indexOf('q.is_moving') + 2)).toEqual([]);
      // Diagnostics and hover merge, so they stay.
      expect(service.hover(doc, controller.indexOf('q.is_baby') + 3)).not.toBeNull();
      // .molang documents are unaffected.
      expect(service.semanticTokens(molang('v.a'))).not.toBeNull();
    } finally {
      service.environment = before;
    }
  });
});

describe("a JSON file's Molang version", () => {
  const at = (version: string | undefined) =>
    JSON.stringify({
      ...(version ? { format_version: version } : {}),
      animation_controllers: { 'controller.animation.v': { states: { default: { transitions: [{ b: 'q.is_feeling_happy' }] } } } },
    });
  const codes = (text: string) => service.diagnostics(json(text)).map((d) => [d.code, d.message]);

  it('gates queries by the format_version', () => {
    // is_feeling_happy resolves only in files read below 1.20.50.
    expect(codes(at('1.21.0'))).toEqual([['query-version', expect.stringContaining('removed in 1.20.50')]]);
    expect(codes(at('1.20.40')).filter(([c]) => c === 'query-version')).toEqual([]);
    expect(codes(at(undefined)).filter(([c]) => c === 'query-version')).toEqual([]);
  });

  it('skips the gates when told to ignore versions', () => {
    const before = service.settings;
    try {
      service.settings = { ...before, versionSource: 'ignore' };
      service.invalidate();
      expect(codes(at('1.21.0')).filter(([c]) => c === 'query-version')).toEqual([]);
    } finally {
      service.settings = before;
      service.invalidate();
    }
  });
});

// Every Molang string in Mojang's vanilla packs must analyse without an
// error or a warning. Point MOLANG_VANILLA at a directory of vanilla pack
// JSON (a bedrock-samples checkout, say) to run it -- it is not run
// otherwise, since the files are not part of this repository -- and
// MOLANG_PATHS at a path catalogue to check every file type it covers;
// without one only animation controllers are covered.
const vanilla = process.env.MOLANG_VANILLA;
describe.runIf(vanilla && existsSync(vanilla))('vanilla packs', () => {
  it('analyse without an error or a warning', () => {
    const pathsFile = process.env.MOLANG_PATHS;
    const provider = pathsFile ? new JsonPathProvider(JSON.parse(readFileSync(pathsFile, 'utf8'))) : new JsonPathProvider();
    const s = new MolangService(service.engine, service.catalogue, [provider]);
    const files: string[] = [];
    const walk = (dir: string) => {
      for (const f of readdirSync(dir)) {
        const p = path.join(dir, f);
        if (statSync(p).isDirectory()) walk(p);
        else if (p.endsWith('.json')) files.push(p);
      }
    };
    walk(vanilla!);
    expect(files.length).toBeGreaterThan(0);
    let regions = 0;
    let covered = 0;
    const problems: string[] = [];
    for (const f of files) {
      const doc = TextDocument.create(`file:///${f}`, 'json', 1, readFileSync(f, 'utf8'));
      const n = s.analyze(doc)?.regions.length ?? 0;
      regions += n;
      if (n) covered++;
      for (const d of s.diagnostics(doc)) {
        if (d.severity === DiagnosticSeverity.Hint) continue;
        problems.push(`${path.basename(f)}:${d.range.start.line + 1}: ${d.code} ${d.message} [${doc.getText(d.range)}]`);
      }
    }
    console.log(`${files.length} files, ${covered} with Molang, ${regions} Molang regions`);
    expect(problems).toEqual([]);
  });
});
