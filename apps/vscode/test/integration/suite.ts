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
    doc.getText(),
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
  assert.equal(doc.getText(), 'v.speed=q.is_baby?1:2;t.x=math.clamp(v.speed,0,1);return t.x;\n');
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

test('reads as Molang what the file\'s own schema marks, where the catalogue has nothing', async () => {
  const doc = await open('custom.json');
  const ds = await diagnostics(doc);
  const got = ds.map((d) => `${doc.getText(d.range)} ${d.message}`);
  assert.deepEqual(got, ['; unexpected token ;']);
});

test("gates queries by the file's format_version", async () => {
  const doc = await open('gated.animation_controllers.json');
  const ds = await diagnostics(doc);
  const got = ds.map((d) => `${doc.getText(d.range)} ${d.code}`);
  assert.deepEqual(got, ['q.is_feeling_happy query-version']);
});

test('lists the Molang regions of a file', async () => {
  await open('custom.json');
  // The command ends in a pick list; close it once it is up.
  const done = vscode.commands.executeCommand('molang.showRegions');
  await new Promise((r) => setTimeout(r, 1000));
  await vscode.commands.executeCommand('workbench.action.closeQuickOpen');
  await done;
});
