// The desktop entry: the language server is a Node process, reached over IPC.

import type * as vscode from 'vscode';
import { LanguageClient, TransportKind, type ServerOptions } from 'vscode-languageclient/node';
import { activateWith, deactivateClient } from './client/activate';

export function activate(context: vscode.ExtensionContext) {
  const module = context.asAbsolutePath('dist/server.js');
  const serverOptions: ServerOptions = {
    run: { module, transport: TransportKind.ipc },
    debug: { module, transport: TransportKind.ipc, options: { execArgv: ['--nolazy', '--inspect=6009'] } },
  };
  return activateWith(context, (options) => new LanguageClient('molang', 'Molang', serverOptions, options));
}

export const deactivate = deactivateClient;
