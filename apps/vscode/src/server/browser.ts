// The browser entry point of the language server, run in a Web Worker by the
// web extension: the connection over the worker's messages, and the module
// and catalogue fetched from the extension's location, which the client
// sends in its initialization options.
//
// A custom catalogue (molang.catalogue.path) is a file path, which a worker
// cannot read; asking for one fails and the server falls back to the shipped
// catalogue with a warning, as it does for any catalogue it cannot read.

import { BrowserMessageReader, BrowserMessageWriter, createConnection, TextDocuments } from 'vscode-languageserver/browser';
import { TextDocument } from 'vscode-languageserver-textdocument';
import { MolangBridge } from './bridge';
import { composeCatalogue } from './catalogue';
import { startServer } from './server';

// The worker's own scope, typed as the Worker it talks to: the project is
// type-checked against the DOM library, which cannot be combined with the
// WebWorker one, and the two sides share the same message methods.
const port = self as unknown as Worker;
const connection = createConnection(new BrowserMessageReader(port), new BrowserMessageWriter(port));
const documents = new TextDocuments(TextDocument);

let base: string | undefined;
const url = (rel: string) => {
  if (!base) throw new Error('molang: the client did not say where the extension is');
  return new URL(rel, base.endsWith('/') ? base : base + '/').toString();
};
const fetchOk = async (rel: string) => {
  const res = await fetch(url(rel));
  if (!res.ok) throw new Error(`${rel}: ${res.status} ${res.statusText}`);
  return res;
};
const text = async (rel: string) => (await fetchOk(rel)).text();
const optional = (rel: string) => text(rel).catch(() => undefined);

startServer({
  connection,
  documents,
  initialize: (options) => {
    base = options.extensionUri;
  },
  // Bytes rather than instantiateStreaming: that needs the server to label
  // the module application/wasm, which not every host serving extension
  // files does.
  loadEngine: async () => MolangBridge.load(await (await fetchOk('dist/molang.wasm')).arrayBuffer()),
  loadCatalogue: async (file) => {
    if (file) throw new Error('a custom catalogue is a file path, which the web extension cannot read');
    const [main, math, namespaces] = await Promise.all([
      text('catalogue/catalogue.json'),
      optional('catalogue/math.json'),
      optional('catalogue/namespaces.json'),
    ]);
    return composeCatalogue(main, math, namespaces);
  },
  loadPaths: () => optional('data/molang-paths.json'),
  loadPathOverrides: () => optional('data/molang-paths.overrides.json'),
  uptime: () => performance.now(),
});

documents.listen(connection);
connection.listen();
