// The language service over the real WebAssembly module, as the server runs
// it. Needs `npm run build` first (npm test does it), for dist/molang.wasm and
// the wasm_exec.js copied beside the bridge.

import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import * as path from 'node:path';
import { TextDocument } from 'vscode-languageserver-textdocument';
import { DiagnosticSeverity, type Location, type TextEdit, type WorkspaceEdit } from 'vscode-languageserver-types';
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

  it('formats over several lines, keeping comments and templates', () => {
    const doc = molang('v.a=q.is_baby?1:2;\n');
    expect(service.print(doc, 'format')).toEqual({ text: 'variable.a = query.is_baby ? 1 : 2;\n' });
    expect(service.print(doc, 'minify')).toEqual({ text: 'v.a=q.is_baby?1:2;\n' });
    const commented = molang('# why\nv.a=1;  # keep me\n\n\nv.#{name}=q.x?{v.b=#{value};}:0;\n');
    expect(service.print(commented, 'format')).toEqual({
      text: '# why\nvariable.a = 1; # keep me\n\nvariable.#{name} = query.x ? {\n    variable.b = #{value};\n} : 0;\n',
    });
    // The editor's indentation, unless the setting names one.
    expect(service.print(molang('loop(2,{v.a=1;});'), 'format', { tabSize: 2, insertSpaces: true })).toEqual({
      text: 'loop(2, {\n  variable.a = 1;\n});',
    });
    expect(service.print(molang('loop(2,{v.a=1;});'), 'format', { tabSize: 4, insertSpaces: false })).toEqual({
      text: 'loop(2, {\n\tvariable.a = 1;\n});',
    });
    // Minifying has nowhere to put a comment.
    expect(service.print(commented, 'minify')).toHaveProperty('error');
    expect(service.print(molang('v.a = ;'), 'format')).toHaveProperty('error');
  });

  it('keeps to the line width setting', () => {
    const before = service.settings;
    try {
      service.settings = { ...before, format: { indentSize: 2, lineWidth: 30 } };
      expect(service.print(molang('v.x = q.v == 1 ? 10 : q.v == 2 ? 20 : 30;'), 'format')).toEqual({
        text: 'variable.x = query.v == 1 ? 10\n  : query.v == 2 ? 20\n  : 30;',
      });
    } finally {
      service.settings = before;
    }
  });

  it('formats the statements a range touches, and nothing else', () => {
    const text = 'v.a=1;\n# note\nv.b=2;   # b\nv.c=3;\n';
    const doc = molang(text);
    const at = doc.positionAt(text.indexOf('v.b') + 1);
    const edits = service.rangeFormatEdits(doc, { start: at, end: at }) as { range: never; newText: string }[];
    expect(edits.map((e) => [doc.getText(e.range), e.newText])).toEqual([['v.b=2;   # b', 'variable.b = 2; # b']]);
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
  it('formats and minifies the Molang in one string, on one line', () => {
    const text = controller.replace('"q.is_moving"', '"q.is_moving && !q.is_baby"');
    const doc = json(text);
    const at = doc.positionAt(text.indexOf('q.is_moving') + 2);
    const actions = service.codeActions(doc, { start: at, end: at });
    const edit = (i: number) => actions[i].edit!.changes![doc.uri][0];
    expect(actions.map((a) => a.title)).toEqual(['Format Molang in this string', 'Minify Molang in this string']);
    expect([doc.getText(edit(0).range), edit(0).newText]).toEqual([
      'q.is_moving && !q.is_baby',
      'query.is_moving && !query.is_baby',
    ]);
    expect(edit(1).newText).toBe('q.is_moving&&!q.is_baby');
    // Written back escaped.
    const esc = doc.positionAt(text.indexOf('caf') + 1);
    const [format] = service.codeActions(doc, { start: esc, end: esc });
    expect(format.edit!.changes![doc.uri][0].newText).toBe(`variable.x == 'café \\"x\\"' && query.is_baby`);
    // Nothing outside Molang.
    const outside = doc.positionAt(text.indexOf('blend_transition'));
    expect(service.codeActions(doc, { start: outside, end: outside })).toEqual([]);
  });


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

/** The text of doc after applying edits. */
function applyEdits(doc: TextDocument, edits: readonly TextEdit[]): string {
  return TextDocument.applyEdits(doc, [...edits]);
}

/** Every quick fix offered for the document's diagnostics, as title -> resulting text. */
function fixesFor(doc: TextDocument) {
  const out: Record<string, string> = {};
  for (const a of service.quickFixes(doc, service.diagnostics(doc))) {
    const edits = a.edit?.changes?.[doc.uri];
    out[a.title] = edits ? applyEdits(doc, edits) : `command ${a.command?.command}`;
  }
  return out;
}

describe('quick fixes', () => {
  it('replaces a deprecated query with its replacement', () => {
    expect(fixesFor(molang('return q.cardinal_block_face_placed_on;'))).toEqual({
      'Replace with q.block_face': 'return q.block_face;',
    });
  });

  it("suggests the queries a mistyped name most likely meant, from the field's set", () => {
    expect(fixesFor(molang('q.is_bayb'))).toEqual({ 'Did you mean q.is_baby?': 'q.is_baby' });
    // In JSON, among the queries the field can name.
    const text = controller.replace('q.is_moving', 'q.is_movin');
    const fixes = fixesFor(json(text));
    expect(fixes['Did you mean q.is_moving?']).toBe(controller);
  });

  it('fixes a namespace the language does not have', () => {
    expect(fixesFor(molang('return a.list[0];'))).toEqual({ "Change 'a.' to 'array.'": 'return array.list[0];' });
    expect(fixesFor(molang('return m.sin(1);'))).toEqual({ "Change 'm.' to 'math.'": 'return math.sin(1);' });
  });

  it('adds a missing semicolon', () => {
    expect(fixesFor(molang('v.a = 1; v.b = 2'))).toEqual({ "Add the missing ';'": 'v.a = 1; v.b = 2;' });
    expect(fixesFor(molang('v.a = 1 # note\n'))).toEqual({ "Add the missing ';'": 'v.a = 1; # note\n' });
  });

  it("adds the space jsonte needs after a comment's #", () => {
    expect(fixesFor(molang('#note\nv.a = 1;'))).toEqual({ "Add a space after the '#'": '# note\nv.a = 1;' });
  });

  it('removes a byte order mark from the text, or offers to change the encoding', () => {
    const bom = String.fromCharCode(0xfeff);
    expect(fixesFor(molang(bom + 'v.a = 1;'))).toEqual({ 'Remove the byte order mark': 'v.a = 1;' });
    const doc = molang('v.a = 1;');
    const ds = service.diagnostics(doc, { byteOrderMark: true });
    expect(ds.map((d) => d.code)).toEqual(['byte-order-mark']);
    expect(service.quickFixes(doc, ds).map((a) => a.command?.command)).toEqual(['workbench.action.editor.changeEncoding']);
  });
});

/** A client entity whose variable is set in one field and read in others. */
const clientEntity = JSON.stringify(
  {
    format_version: '1.10.0',
    'minecraft:client_entity': {
      description: {
        identifier: 'test:thing',
        scripts: {
          initialize: ['variable.speed = 0; t.x = 1;'],
          pre_animation: ['v.speed = v.speed + 1; t.x = v.speed;'],
          animate: [{ walk: 'v.Speed > 1 && q.target->v.speed' }],
        },
      },
    },
  },
  null,
  2,
);

describe('variables as symbols', () => {
  let full: MolangService;
  beforeAll(() => {
    const paths = JSON.parse(readFileSync(path.join(root, 'data', 'molang-paths.json'), 'utf8'));
    full = new MolangService(service.engine, service.catalogue, [new MolangFileProvider(), new JsonPathProvider(paths)]);
  });
  const texts = (doc: TextDocument, ls: Location[]) => ls.map((l) => `${l.range.start.line}:${doc.getText(l.range)}`);

  it('finds definitions and references within a .molang file', () => {
    const text = 'v.a = 1;\nt.b = v.a;\nreturn variable.A + t.b;';
    const doc = molang(text);
    expect(texts(doc, service.definition(doc, text.indexOf('variable.A') + 9))).toEqual(['0:a']);
    expect(texts(doc, service.references(doc, text.indexOf('v.a;'), true))).toEqual(['0:a', '1:a', '2:A']);
    expect(texts(doc, service.references(doc, text.indexOf('v.a;'), false))).toEqual(['1:a', '2:A']);
    // Nothing for a query.
    expect(service.definition(molang('q.is_baby'), 3)).toEqual([]);
  });

  it('links a variable across the fields of a JSON file, but a temp only within its expression', () => {
    const doc = json(clientEntity);
    const at = clientEntity.indexOf('v.Speed') + 3;
    const refs = full.references(doc, at, true);
    // initialize, pre_animation (three), animate; not the other entity's through ->.
    expect(refs.map((r) => doc.getText(r.range))).toEqual(['speed', 'speed', 'speed', 'speed', 'Speed']);
    expect(full.definition(doc, at).length).toBe(2);
    const temps = full.references(doc, clientEntity.indexOf('t.x = 1') + 2, true);
    expect(temps.map((r) => r.range.start.line)).toEqual([temps[0].range.start.line]);
    // The other entity's variable has no symbol here.
    expect(full.references(doc, clientEntity.indexOf('->v.speed') + 4, true)).toEqual([]);
  });

  it('renames every occurrence, keeping each spelling of the namespace', () => {
    const doc = json(clientEntity);
    const at = clientEntity.indexOf('v.Speed') + 3;
    expect(full.prepareRename(doc, at)!.placeholder).toBe('Speed');
    const edit = full.rename(doc, at, 'velocity') as WorkspaceEdit;
    const result = applyEdits(doc, edit.changes![doc.uri]);
    expect(result).toContain('variable.velocity = 0');
    expect(result).toContain('v.velocity = v.velocity + 1; t.x = v.velocity;');
    expect(result).toContain('v.velocity > 1 && q.target->v.speed');
    expect(full.rename(doc, at, '1abc')).toHaveProperty('error');
    // "v.name" as the new name means the name.
    const e2 = full.rename(doc, at, 'v.pace') as WorkspaceEdit;
    expect(e2.changes![doc.uri][0].newText).toBe('pace');
  });

  it('says where a variable is written and read, field by field', () => {
    const doc = json(clientEntity);
    const h = full.hover(doc, clientEntity.indexOf('v.Speed') + 3)!;
    const value = (h.contents as { value: string }).value;
    expect(value).toMatch(/Written on line \d+ in `scripts\/initialize`; line \d+ in `scripts\/pre_animation`\./);
    expect(value).toMatch(/Read 3× \(line \d+ in `scripts\/pre_animation`; line \d+ in `scripts\/animate\/walk`\)\./);
    const other = full.hover(doc, clientEntity.indexOf('->v.speed') + 4)!;
    expect((other.contents as { value: string }).value).toContain("Another entity's");
  });
});

describe('inlay hints', () => {
  it('names parameters when asked to', () => {
    const text = "return math.clamp(v.x, 0, v.max) + q.is_item_name_any('slot.weapon.mainhand', 0, 'a', 'b');";
    const doc = molang(text);
    const whole = { start: doc.positionAt(0), end: doc.positionAt(text.length) };
    expect(service.inlayHints(doc, whole)).toEqual([]);
    const before = service.settings;
    try {
      service.settings = { ...before, inlayHints: { parameterNames: true } };
      const hints = service.inlayHints(doc, whole).map((h) => {
        const at = doc.offsetAt(h.position);
        return `${h.label}${text.slice(at, at + 3)}`;
      });
      // v.max already says max.
      expect(hints).toEqual(['value:v.x', 'min:0, ', "slot_name:'sl", 'slot_index:0, ', "item:'a'", "item:'b'"]);
    } finally {
      service.settings = before;
    }
  });
});
