// The Node entry point of the language server: the connection over IPC, and
// the module and catalogue read from the extension's own files. Everything
// else is in server.ts, which a browser entry would share.
//
// IPC rather than stdio matters here. Go's wasm_exec.js sends anything the
// Go program prints to console.log, and on stdio that would be written into
// the protocol stream.

import { existsSync } from 'node:fs';
import { readFile } from 'node:fs/promises';
import * as path from 'node:path';
import { createConnection, ProposedFeatures, TextDocuments } from 'vscode-languageserver/node';
import { TextDocument } from 'vscode-languageserver-textdocument';
import { MolangBridge } from './bridge';
import { composeCatalogue } from './catalogue';
import { startServer } from './server';

const connection = createConnection(ProposedFeatures.all);
const documents = new TextDocuments(TextDocument);

startServer({
  connection,
  documents,
  // The files sit next to the bundle: dist/server.js, dist/molang.wasm,
  // and catalogue/ and data/ beside dist.
  loadEngine: async () => MolangBridge.load(await readFile(path.join(__dirname, 'molang.wasm'))),
  // math.json and namespaces.json are read from beside the catalogue, a
  // custom one included, when they are there.
  loadCatalogue: async (file) => {
    const main = file || path.join(__dirname, '..', 'catalogue', 'catalogue.json');
    const beside = async (name: string) => {
      const p = path.join(path.dirname(main), name);
      return existsSync(p) ? readFile(p, 'utf8') : undefined;
    };
    return composeCatalogue(await readFile(main, 'utf8'), await beside('math.json'), await beside('namespaces.json'));
  },
  loadPaths: async () => {
    const file = path.join(__dirname, '..', 'data', 'molang-paths.json');
    return existsSync(file) ? readFile(file, 'utf8') : undefined;
  },
  uptime: () => performance.now(),
});

documents.listen(connection);
connection.listen();
