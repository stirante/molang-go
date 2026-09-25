// The web smoke tests. They run in the editor's web extension host, where
// there is neither node:assert nor a file system: the fixtures are the
// workspace folder test-web mounts, read through the editor's own API.
//
// A handful of checks, not the integration suite again: the features are the
// same code on both builds, so what these guard is the web build's own
// wiring -- the worker starts, the module and both catalogues arrive by
// fetch, and requests get answers.

import * as vscode from 'vscode';

const tests: [string, () => Promise<void>][] = [];
const test = (name: string, f: () => Promise<void>) => tests.push([name, f]);

function check(ok: unknown, message: string): asserts ok {
  if (!ok) throw new Error(message);
}

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

const open = async (name: string) => {
  const folder = vscode.workspace.workspaceFolders?.[0];
  check(folder, 'no workspace folder: the fixtures are not mounted');
  const doc = await vscode.workspace.openTextDocument(vscode.Uri.joinPath(folder.uri, name));
  await vscode.window.showTextDocument(doc);
  return doc;
};

async function diagnostics(doc: vscode.TextDocument, timeout = 30000): Promise<vscode.Diagnostic[]> {
  const t0 = Date.now();
  for (;;) {
    const ours = vscode.languages.getDiagnostics(doc.uri).filter((d) => d.source === 'molang');
    if (ours.length || Date.now() - t0 > timeout) return ours;
    await new Promise((r) => setTimeout(r, 50));
  }
}

const at = (doc: vscode.TextDocument, needle: string, add = 0) => doc.positionAt(doc.getText().indexOf(needle) + add);

const labels = (list: vscode.CompletionList) => list.items.map((i) => (typeof i.label === 'string' ? i.label : i.label.label));

// Activated by hand rather than by opening a file, so a failure to start
// (the worker refusing a message, say) is reported as itself instead of as
// diagnostics that never arrive.
test('activates', async () => {
  const ext = vscode.extensions.getExtension('stirante.molang');
  check(ext, 'the extension is not loaded');
  await ext.activate();
});

test('reports the syntax errors in a .molang file', async () => {
  const doc = await open('errors.molang');
  const got = (await diagnostics(doc)).map((d) => `${d.range.start.line}:${d.range.start.character} ${d.message}`);
  const want = ['1:6 unexpected token ;', '3:6 unexpected token )'];
  check(JSON.stringify(got) === JSON.stringify(want), `got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
});

test('completes queries after q., from the shipped catalogue', async () => {
  const doc = await open('clean.molang');
  const list = await vscode.commands.executeCommand<vscode.CompletionList>(
    'vscode.executeCompletionItemProvider',
    doc.uri,
    at(doc, 'q.is_baby', 2),
  );
  check(labels(list).includes('is_baby'), `no is_baby in ${labels(list).join(', ')}`);
});

test('finds Molang in pack JSON, by the shipped path catalogue', async () => {
  const doc = await open('test.animation_controllers.json');
  const got = (await diagnostics(doc)).filter((d) => d.severity !== vscode.DiagnosticSeverity.Hint).map((d) => d.message);
  check(got.includes("unknown math function 'math.nope'"), `got ${JSON.stringify(got)}`);
});

async function diagnosticsWith(doc: vscode.TextDocument, code: string, timeout = 30000): Promise<vscode.Diagnostic[]> {
  const t0 = Date.now();
  for (;;) {
    const ours = vscode.languages.getDiagnostics(doc.uri).filter((d) => d.source === 'molang');
    if (ours.some((d) => d.code === code) || Date.now() - t0 > timeout) return ours;
    await new Promise((r) => setTimeout(r, 50));
  }
}

// formatSource is its own bridge call; this is the module in the worker
// answering it.
test('formats a .molang file', async () => {
  const doc = await open('clean.molang');
  const edits = await vscode.commands.executeCommand<vscode.TextEdit[]>('vscode.executeFormatDocumentProvider', doc.uri, {
    tabSize: 4,
    insertSpaces: true,
  });
  // The editor narrows the edit to what changes; applied, it is the file.
  const edit = new vscode.WorkspaceEdit();
  edit.set(doc.uri, edits ?? []);
  check(await vscode.workspace.applyEdit(edit), 'the edit was not applied');
  const text = doc.getText().replace(/\r\n/g, '\n');
  await vscode.commands.executeCommand('undo');
  const want = 'variable.speed = query.is_baby ? 1 : 2;\ntemp.x = math.clamp(variable.speed, 0, 1);\nreturn temp.x;\n';
  check(text === want, `got ${JSON.stringify(text)}`);
});

test('offers the string actions in pack JSON', async () => {
  const doc = await open('test.animation_controllers.json');
  await diagnostics(doc);
  // The document was closed and reopened by the tests before, and a request
  // made while the editor is still settling it is answered empty or
  // cancelled; ask until it is not.
  let titles: string[] = [];
  let last: unknown;
  for (const t0 = Date.now(); !titles.length && Date.now() - t0 < 10000; ) {
    try {
      const actions = await vscode.commands.executeCommand<vscode.CodeAction[]>(
        'vscode.executeCodeActionProvider',
        doc.uri,
        new vscode.Range(at(doc, 'q.is_moving', 2), at(doc, 'q.is_moving', 2)),
      );
      titles = actions.map((a) => a.title);
    } catch (e) {
      last = e;
    }
    if (!titles.length) await new Promise((r) => setTimeout(r, 100));
  }
  check(titles.length, `no code actions; last error ${last}`);
  check(titles.includes('Format Molang in this string'), `got ${titles.join(', ')}`);
});

// The one check that needs the file's bytes, which the worker asks the
// client for.
test('warns about a byte order mark', async () => {
  const doc = await open('bom.molang');
  const got = (await diagnosticsWith(doc, 'byte-order-mark')).map((d) => d.code);
  check(got.includes('byte-order-mark'), `got ${JSON.stringify(got)}`);
});

// Schemas are read through the editor's file system, which on the web is
// the mounted folder.
test("reads as Molang what the file's own schema marks", async () => {
  const doc = await open('custom.json');
  const got = (await diagnostics(doc)).map((d) => `${doc.getText(d.range)} ${d.message}`);
  check(JSON.stringify(got) === JSON.stringify(['; unexpected token ;']), `got ${JSON.stringify(got)}`);
});
