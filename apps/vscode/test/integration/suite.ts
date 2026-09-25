// The integration suite: the extension in a real VS Code, over the fixtures
// folder, driven through the same commands the editor uses. A test runner of
// a dozen lines rather than a framework, since these run in order and the
// only output wanted is which failed and why.

import * as assert from 'node:assert/strict';
import * as path from 'node:path';
import * as vscode from 'vscode';

const tests: [string, () => Promise<void>][] = [];
const test = (name: string, f: () => Promise<void>) => tests.push([name, f]);

export async function run(): Promise<void> {
  const failures: string[] = [];
  for (const [name, f] of tests) {
    const t0 = Date.now();
    try {
      await f();
      console.log(`  ok   ${name} (${Date.now() - t0} ms)`);
    } catch (e) {
      console.log(`  FAIL ${name}: ${(e as Error).stack ?? e}`);
      failures.push(name);
    }
  }
  if (failures.length) throw new Error(`${failures.length} of ${tests.length} failed: ${failures.join(', ')}`);
}

const fixtures = path.resolve(__dirname, '..', '..', 'test', 'integration', 'fixtures');
const open = async (name: string) => {
  const doc = await vscode.workspace.openTextDocument(path.join(fixtures, name));
  await vscode.window.showTextDocument(doc);
  return doc;
};

/** Waits for diagnostics from this extension on doc. */
async function diagnostics(doc: vscode.TextDocument, timeout = 20000): Promise<vscode.Diagnostic[]> {
  const t0 = Date.now();
  for (;;) {
    const ours = vscode.languages.getDiagnostics(doc.uri).filter((d) => d.source === 'molang');
    if (ours.length) return ours;
    if (Date.now() - t0 > timeout) return ours;
    await new Promise((r) => setTimeout(r, 50));
  }
}

const at = (doc: vscode.TextDocument, needle: string, add = 0) => doc.positionAt(doc.getText().indexOf(needle) + add);

// A checkout with core.autocrlf gives the fixtures CRLF line ends, and the
// editor writes a document's own line end whatever an edit's text says.
const lf = (doc: vscode.TextDocument) => doc.getText().replace(/\r\n/g, '\n');

test('activates, and the server comes up', async () => {
  const t0 = Date.now();
  const doc = await open('errors.molang');
  const ds = await diagnostics(doc);
  assert.ok(ds.length > 0, 'no diagnostics arrived');
  console.log(`       first diagnostics ${Date.now() - t0} ms after opening the first document`);
});

test('reports every syntax error in a .molang file, and none in comments', async () => {
  const doc = await open('errors.molang');
  const ds = await diagnostics(doc);
  const got = ds.map((d) => `${d.range.start.line}:${d.range.start.character} ${doc.getText(d.range)} ${d.message}`);
  assert.deepEqual(got, ['1:6 ; unexpected token ;', '3:6 ) unexpected token )']);
});

test('completes queries after q.', async () => {
  const doc = await open('clean.molang');
  const list = await vscode.commands.executeCommand<vscode.CompletionList>(
    'vscode.executeCompletionItemProvider',
    doc.uri,
    at(doc, 'q.is_baby', 2),
  );
  const labels = list.items.map((i) => (typeof i.label === 'string' ? i.label : i.label.label));
  assert.ok(labels.includes('is_baby'), labels.join(', '));
});

test('hovers a query', async () => {
  const doc = await open('clean.molang');
  const hovers = await vscode.commands.executeCommand<vscode.Hover[]>('vscode.executeHoverProvider', doc.uri, at(doc, 'is_baby', 1));
  const text = hovers.flatMap((h) => h.contents.map((c) => (typeof c === 'string' ? c : c.value))).join('\n');
  assert.match(text, /query\.is_baby/);
});

test('offers signature help in a math call', async () => {
  const doc = await open('clean.molang');
  const help = await vscode.commands.executeCommand<vscode.SignatureHelp>(
    'vscode.executeSignatureHelpProvider',
    doc.uri,
    at(doc, 'math.clamp(', 'math.clamp('.length),
  );
  assert.equal(help.signatures[0].label, 'math.clamp(value: number, min: number, max: number): number');
});

test('colours tokens', async () => {
  const doc = await open('clean.molang');
  const tokens = await vscode.commands.executeCommand<vscode.SemanticTokens>('vscode.provideDocumentSemanticTokens', doc.uri);
  assert.ok(tokens && tokens.data.length > 0);
});

test('formats a .molang file', async () => {
  const doc = await open('clean.molang');
  const before = doc.getText();
  await vscode.commands.executeCommand('editor.action.formatDocument');
  assert.equal(
    lf(doc),
    'variable.speed = query.is_baby ? 1 : 2; temp.x = math.clamp(variable.speed, 0, 1); return temp.x;\n',
  );
  await vscode.commands.executeCommand('undo');
  assert.equal(doc.getText(), before);
});

test('does not format a file with comments, which the printer would drop', async () => {
  const doc = await open('errors.molang');
  const before = doc.getText();
  await vscode.commands.executeCommand('editor.action.formatDocument');
  assert.equal(doc.getText(), before);
});

test('minifies a .molang file', async () => {
  const doc = await open('clean.molang');
  await vscode.commands.executeCommand('molang.minify');
  assert.equal(lf(doc), 'v.speed=q.is_baby?1:2;t.x=math.clamp(v.speed,0,1);return t.x;\n');
  await vscode.commands.executeCommand('undo');
});

test('reports Molang errors inside animation controller JSON, through escapes', async () => {
  const doc = await open('test.animation_controllers.json');
  const ds = await diagnostics(doc);
  const got = ds.filter((d) => d.severity !== vscode.DiagnosticSeverity.Hint).map((d) => `${doc.getText(d.range)} ${d.message}`);
  assert.deepEqual(got, [
    '; unexpected token ;',
    "math.nope unknown math function 'math.nope'",
  ]);
});

test('completes inside a JSON string', async () => {
  const doc = await open('test.animation_controllers.json');
  const list = await vscode.commands.executeCommand<vscode.CompletionList>(
    'vscode.executeCompletionItemProvider',
    doc.uri,
    at(doc, 'q.is_moving', 2),
  );
  const labels = list.items.map((i) => (typeof i.label === 'string' ? i.label : i.label.label));
  assert.ok(labels.includes('is_moving'), labels.join(', '));
});

/** Diagnostics from this extension on doc once one with code has arrived. */
async function diagnosticsWith(doc: vscode.TextDocument, code: string, timeout = 20000): Promise<vscode.Diagnostic[]> {
  const t0 = Date.now();
  for (;;) {
    const ours = vscode.languages.getDiagnostics(doc.uri).filter((d) => d.source === 'molang');
    if (ours.some((d) => d.code === code) || Date.now() - t0 > timeout) return ours;
    await new Promise((r) => setTimeout(r, 50));
  }
}

test('offers quick fixes, and applying one can be undone', async () => {
  const doc = await open('fixes.molang');
  const ds = await diagnosticsWith(doc, 'query-deprecated');
  const titles: string[] = [];
  for (const d of ds) {
    const actions = await vscode.commands.executeCommand<vscode.CodeAction[]>('vscode.executeCodeActionProvider', doc.uri, d.range);
    titles.push(...actions.map((a) => a.title));
  }
  for (const want of [
    "Add a space after the '#'",
    "Change 'm.' to 'math.'",
    'Did you mean q.is_baby?',
    'Replace with q.block_face',
    "Add the missing ';'",
  ]) {
    assert.ok(titles.includes(want), `no "${want}" in ${titles.join(' | ')}; diagnostics ${ds.map((d) => d.code).join(', ')}`);
  }
  const before = doc.getText();
  const [fix] = (
    await vscode.commands.executeCommand<vscode.CodeAction[]>('vscode.executeCodeActionProvider', doc.uri, ds.find((d) => d.code === 'syntax' && /unknown identifier/.test(d.message))!.range)
  ).filter((a) => a.title === "Change 'm.' to 'math.'");
  assert.ok(await vscode.workspace.applyEdit(fix.edit!));
  assert.match(doc.getText(), /v\.a = math\.sin/);
  await vscode.commands.executeCommand('undo');
  assert.equal(doc.getText(), before);
});

test('warns about a byte order mark the editor does not show', async () => {
  const doc = await open('bom.molang');
  assert.ok(!doc.getText().startsWith(String.fromCharCode(0xfeff)), 'the editor kept the mark in the text');
  const ds = await diagnosticsWith(doc, 'byte-order-mark');
  assert.deepEqual(
    ds.map((d) => d.code),
    ['byte-order-mark'],
  );
});

test('goes to a variable written in another field of the same JSON file, and finds every use', async () => {
  const doc = await open('test.entity.json');
  const at = (needle: string, add: number) => doc.positionAt(doc.getText().indexOf(needle) + add);
  const defs = await vscode.commands.executeCommand<(vscode.Location | vscode.LocationLink)[]>(
    'vscode.executeDefinitionProvider',
    doc.uri,
    at('"v.speed > 1"', 4),
  );
  const lines = defs.map((d) => ('range' in d ? d.range : d.targetRange).start.line).sort();
  assert.deepEqual(lines, [6, 7]);
  const refs = await vscode.commands.executeCommand<vscode.Location[]>('vscode.executeReferenceProvider', doc.uri, at('"v.speed > 1"', 4));
  assert.equal(refs.length, 5);
  // A temp belongs to its own expression.
  const temps = await vscode.commands.executeCommand<vscode.Location[]>('vscode.executeReferenceProvider', doc.uri, at('t.x = 1', 2));
  assert.equal(temps.length, 1);
});

test('renames a variable across the fields of a JSON file', async () => {
  const doc = await open('test.entity.json');
  const pos = doc.positionAt(doc.getText().indexOf('variable.speed') + 10);
  const edit = await vscode.commands.executeCommand<vscode.WorkspaceEdit>('vscode.executeDocumentRenameProvider', doc.uri, pos, 'pace');
  const edits = edit.get(doc.uri);
  assert.equal(edits.length, 5);
  assert.ok(edits.every((e) => e.newText === 'pace' && doc.getText(e.range).toLowerCase() === 'speed'));
});

test('hovers a JSON variable with the fields that write and read it', async () => {
  const doc = await open('test.entity.json');
  const hovers = await vscode.commands.executeCommand<vscode.Hover[]>(
    'vscode.executeHoverProvider',
    doc.uri,
    doc.positionAt(doc.getText().indexOf('"v.speed > 1"') + 4),
  );
  const text = hovers.flatMap((h) => h.contents.map((c) => (typeof c === 'string' ? c : c.value))).join('\n');
  assert.match(text, /Written on line 7 in `scripts\/initialize`; line 8 in `scripts\/pre_animation`\./);
});

test('shows parameter names when they are turned on', async () => {
  const doc = await open('test.entity.json');
  const range = new vscode.Range(0, 0, doc.lineCount, 0);
  const config = vscode.workspace.getConfiguration('molang');
  const hints = () => vscode.commands.executeCommand<vscode.InlayHint[]>('vscode.executeInlayHintProvider', doc.uri, range);
  assert.deepEqual(await hints(), []);
  await config.update('inlayHints.parameterNames', true, vscode.ConfigurationTarget.Global);
  try {
    let labels: string[] = [];
    const t0 = Date.now();
    while (!labels.length && Date.now() - t0 < 10000) {
      labels = (await hints()).map((h) => (typeof h.label === 'string' ? h.label : h.label.map((p) => p.value).join('')));
      if (!labels.length) await new Promise((r) => setTimeout(r, 100));
    }
    assert.deepEqual(labels, ['value:', 'min:', 'max:']);
  } finally {
    await config.update('inlayHints.parameterNames', undefined, vscode.ConfigurationTarget.Global);
  }
});
