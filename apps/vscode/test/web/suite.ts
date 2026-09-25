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
