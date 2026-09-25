// What the extension does once it has a language client: the client options,
// the environment it reports to the server, the commands the protocol has no
// request for, the Molang the JSON schemas of open documents mark, and the
// settlement with Blockception -- the last two need the workspace's files and
// other extensions' settings, which only the extension host can read. All
// language features are the server's.
//
// Shared by the desktop entry (src/extension.ts, a Node server over IPC) and
// the web entry (src/web/extension.ts, a server in a Web Worker); the two
// differ only in how the client reaches its server.

import * as vscode from 'vscode';
import { RequestType, type BaseLanguageClient, type LanguageClientOptions } from 'vscode-languageclient';
import { blockceptionState, registerBlockceptionCoexistence } from './blockception';
import { registerRegionsCommand } from './regionsCommand';
import { SchemaIndex } from './schemaIndex';

export type ClientFactory = (options: LanguageClientOptions) => BaseLanguageClient;

let client: BaseLanguageClient | undefined;

const ByteOrderMarkRequest = new RequestType<{ uri: string }, boolean, void>('molang/byteOrderMark');

export async function activateWith(context: vscode.ExtensionContext, create: ClientFactory, extra: object = {}) {
  const offerSilencing = registerBlockceptionCoexistence(context);
  const clientOptions: LanguageClientOptions = {
    // bc-minecraft-molang is Blockception's id for .molang files: when the
    // user's file associations give .molang to it, these features still
    // apply.
    documentSelector: [
      { language: 'molang' },
      { language: 'bc-minecraft-molang' },
      { language: 'json' },
      { language: 'jsonc' },
    ],
    synchronize: { configurationSection: 'molang' },
    middleware: {
      handleDiagnostics(uri, diagnostics, next) {
        if (uri.path.toLowerCase().endsWith('.json')) offerSilencing(uri, diagnostics.length);
        next(uri, diagnostics);
      },
    },
    initializationOptions: {
      // A plain copy: the configuration object is a proxy, which a Web
      // Worker's postMessage cannot clone.
      settings: JSON.parse(JSON.stringify(vscode.workspace.getConfiguration().get('molang') ?? {})),
      environment: blockceptionState(),
      ...extra,
    },
  };
  client = create(clientOptions);
  // The web server has no file system to look for a byte order mark with,
  // which the editor takes off a file's text; it asks here.
  client.onRequest(ByteOrderMarkRequest, async ({ uri }) => {
    const bytes = await vscode.workspace.fs.readFile(vscode.Uri.parse(uri));
    return bytes.length >= 3 && bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf;
  });
  await client.start();

  const schemas = new SchemaIndex(
    (p) => void client?.sendNotification('molang/schemaPaths', p),
    (m) => client?.outputChannel.appendLine(`molang: ${m}`),
  );
  schemas.refreshAll();

  const sendEnvironment = () => client?.sendNotification('molang/environment', blockceptionState());
  context.subscriptions.push(
    schemas,
    registerRegionsCommand(() => client, schemas),
    vscode.extensions.onDidChange(sendEnvironment),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration('BC-MC')) sendEnvironment();
    }),
    // A plain command rather than a text editor command, so the edit is
    // finished by the time the command's promise settles.
    vscode.commands.registerCommand('molang.minify', async () => {
      const editor = vscode.window.activeTextEditor;
      if (!editor) return;
      const doc = editor.document;
      const result = await client!.sendRequest<{ text: string } | { error: string }>('molang/print', {
        uri: doc.uri.toString(),
        how: 'minify',
      });
      if ('error' in result) {
        vscode.window.showInformationMessage(`Molang: not minified. ${result.error}`);
        return;
      }
      const all = new vscode.Range(doc.positionAt(0), doc.positionAt(doc.getText().length));
      await editor.edit((b) => b.replace(all, result.text));
    }),
  );
}

export function deactivateClient() {
  return client?.stop();
}
