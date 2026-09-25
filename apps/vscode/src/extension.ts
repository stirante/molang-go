// The extension: starts the language server, adds the commands the protocol
// has no request for, and finds the Molang the JSON schemas of open
// documents mark, which only the extension host can read. All language
// features are the server's.

import * as vscode from 'vscode';
import { LanguageClient, TransportKind, type LanguageClientOptions, type ServerOptions } from 'vscode-languageclient/node';
import { blockceptionState } from './client/blockception';
import { registerRegionsCommand } from './client/regionsCommand';
import { SchemaIndex } from './client/schemaIndex';

let client: LanguageClient | undefined;

export async function activate(context: vscode.ExtensionContext) {
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
    initializationOptions: {
      settings: vscode.workspace.getConfiguration().get('molang'),
      environment: blockceptionState(),
    },
  };
  client = new LanguageClient('molang', 'Molang', serverOptions, clientOptions);
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

export function deactivate() {
  return client?.stop();
}
