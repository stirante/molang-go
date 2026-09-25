// The web entry, for vscode.dev and github.dev: the language server runs in a
// Web Worker, and reads the module and the catalogue by fetching them from
// the extension's own location, since a worker has no file system.
//
// The worker is told that location rather than working it out from its own
// script URL: the editor may load a worker script from a blob or a data URL,
// and then nothing relative to it resolves.

import * as vscode from 'vscode';
import { LanguageClient } from 'vscode-languageclient/browser';
import { activateWith, deactivateClient } from '../client/activate';

export function activate(context: vscode.ExtensionContext) {
  const main = vscode.Uri.joinPath(context.extensionUri, 'dist', 'web', 'server.js');
  return activateWith(
    context,
    (options) => new LanguageClient('molang', 'Molang', new Worker(main.toString(true)), options),
    { extensionUri: context.extensionUri.toString() },
  );
}

export const deactivate = deactivateClient;
