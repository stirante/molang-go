// The extension: starts the language server and adds what the protocol has
// no request for -- minifying, and the settlement with Blockception, which
// needs the workspace's files and other extensions' settings. All language
// features are the server's.

import * as vscode from 'vscode';
import { LanguageClient, TransportKind, type LanguageClientOptions, type ServerOptions } from 'vscode-languageclient/node';
import { blockceptionState, registerBlockceptionCoexistence } from './client/blockception';

let client: LanguageClient | undefined;

export async function activate(context: vscode.ExtensionContext) {
  const offerSilencing = registerBlockceptionCoexistence(context);
  const module = context.asAbsolutePath('dist/server.js');
  const serverOptions: ServerOptions = {
    run: { module, transport: TransportKind.ipc },
    debug: { module, transport: TransportKind.ipc, options: { execArgv: ['--nolazy', '--inspect=6009'] } },
  };
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
      settings: vscode.workspace.getConfiguration().get('molang'),
      environment: blockceptionState(),
    },
  };
  client = new LanguageClient('molang', 'Molang', serverOptions, clientOptions);
  await client.start();

  const sendEnvironment = () => client?.sendNotification('molang/environment', blockceptionState());
  context.subscriptions.push(
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

export function deactivate() {
  return client?.stop();
}
