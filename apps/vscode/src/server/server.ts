// The language server, wired to a connection. Platform-free: the connection,
// the document store and the two things read from disk (the WebAssembly
// module and the catalogue) come from the entry point, so a browser entry can
// supply them from fetch() instead of the file system.

import { ErrorCodes, ResponseError, type Connection, type InitializeResult, type TextDocuments } from 'vscode-languageserver';
import type { TextEdit } from 'vscode-languageserver-types';
import type { TextDocument } from 'vscode-languageserver-textdocument';
import type { MolangEngine } from './bridge';
import { Catalogue } from './catalogue';
import { builtinPaths, composePaths, JsonPathProvider, type FileType, type PathCatalogue } from './embedding';
import { MOLANG_LANGUAGE_IDS, MolangFileProvider } from './regions';
import { TOKEN_MODIFIERS, TOKEN_TYPES } from './semantic';
import { MolangService, defaultSettings, type Environment, type RegionInfo, type Settings } from './service';

export interface ServerHost {
  connection: Connection;
  documents: TextDocuments<TextDocument>;
  loadEngine(): Promise<MolangEngine>;
  /** The catalogue document at path, or the shipped one when path is empty. */
  loadCatalogue(path: string): Promise<string>;
  /**
   * The shipped catalogue of Molang paths in pack JSON
   * (data/molang-paths.json), or undefined when there is none and the
   * built-in entries are to be used.
   */
  loadPaths(): Promise<string | undefined>;
  /**
   * The curated overrides beside it (data/molang-paths.overrides.json),
   * whose removals hold schema-driven paths back too; undefined for none.
   */
  loadPathOverrides?(): Promise<string | undefined>;
  /** Milliseconds since the server process started, for the startup log. */
  uptime(): number;
  /**
   * The client's initialization options, before anything is loaded: how a
   * host learns what it has no other way to know, such as the web client's
   * extension location.
   */
  initialize?(options: InitializationOptions): void;
  /**
   * Whether the file behind uri starts with a UTF-8 byte order mark. The
   * editor takes the mark off when it reads the file, so the document's
   * text cannot say. Absent where there is no file system to ask.
   */
  hasByteOrderMark?(uri: string): Promise<boolean>;
}

/** The client's settings, as the molang configuration section. */
interface ClientSettings {
  catalogue?: { path?: string };
  diagnostics?: { unknownQueries?: Settings['unknownQueries'] };
  versionSource?: Settings['versionSource'];
  json?: Partial<Settings['json']> & { schemaDetection?: boolean };
  format?: Partial<Settings['format']>;
  inlayHints?: Partial<Settings['inlayHints']>;
}

export interface InitializationOptions {
  settings?: ClientSettings;
  environment?: Environment;
  /** Where the extension is, as a URL; sent by the web client only. */
  extensionUri?: string;
}

/** Requests and notifications beyond the protocol. */
export const PrintRequest = 'molang/print';
export interface PrintParams {
  uri: string;
  how: 'format' | 'minify';
}
export type PrintResponse = { text?: string; error?: string };
export const EnvironmentNotification = 'molang/environment';

/**
 * From the client: the Molang paths the JSON schemas applied to a document
 * mark, found by walking them in the extension host (client/schemaIndex.ts),
 * which is where the associations and the schemas can be read. An empty
 * fileTypes clears them.
 */
export const SchemaPathsNotification = 'molang/schemaPaths';
export interface SchemaPathsParams {
  uri: string;
  fileTypes: FileType[];
  schemas: string[];
}

/**
 * To the client, from the web server, which has no file system: whether the
 * file behind uri starts with a UTF-8 byte order mark.
 */
export const ByteOrderMarkRequest = 'molang/byteOrderMark';

/** The Molang regions of a document, for the regions command. */
export const RegionsRequest = 'molang/regions';
export interface RegionsResponse {
  regions: RegionInfo[];
}

// TextDocumentSyncKind.Incremental, spelled out: the protocol package's
// runtime values come with a Node transport attached.
const INCREMENTAL_SYNC = 2;
// CodeActionKind.QuickFix and RefactorRewrite, likewise.
const CODE_ACTION_QUICKFIX = 'quickfix';
const CODE_ACTION_REWRITE = 'refactor.rewrite';

export function startServer(host: ServerHost) {
  const { connection, documents } = host;
  let service: Promise<MolangService> | undefined;
  let settings = defaultSettings;
  let schemaDetection = true;
  let paths: JsonPathProvider | undefined;
  let cataloguePath = '';
  let environment: Environment = { blockceptionActive: false, blockceptionJsonCompletion: false };
  let canRefreshTokens = false;
  let canRefreshHints = false;

  const applySettings = (s: ClientSettings | undefined) => {
    const { schemaDetection: detect, ...json } = s?.json ?? {};
    settings = {
      unknownQueries: s?.diagnostics?.unknownQueries ?? defaultSettings.unknownQueries,
      versionSource: s?.versionSource ?? defaultSettings.versionSource,
      json: { ...defaultSettings.json, ...json },
      format: { ...defaultSettings.format, ...(s?.format ?? {}) },
      inlayHints: { ...defaultSettings.inlayHints, ...(s?.inlayHints ?? {}) },
    };
    schemaDetection = detect !== false;
    if (paths) paths.useSchemas = schemaDetection;
    const path = s?.catalogue?.path ?? '';
    const reload = path !== cataloguePath;
    cataloguePath = path;
    return reload;
  };

  const loadCatalogue = async (engine: MolangEngine): Promise<Catalogue> => {
    let json: string;
    try {
      json = await host.loadCatalogue(cataloguePath);
    } catch (e) {
      connection.window.showWarningMessage(`Molang: cannot read the catalogue at ${cataloguePath}: ${e}. Using the shipped one.`);
      json = await host.loadCatalogue('');
    }
    const summary = engine.setCatalogue(json);
    if (!summary.ok) {
      connection.window.showWarningMessage(`Molang: the catalogue does not load: ${summary.error}`);
      return new Catalogue();
    }
    const kind = summary.partial ? 'partial catalogue' : `catalogue for ${summary.gameVersion || 'an unstated version'}`;
    connection.console.info(`molang: ${kind}, ${summary.queries} queries, ${summary.math} math functions`);
    return Catalogue.parse(json);
  };

  const loadPaths = async (): Promise<PathCatalogue> => {
    try {
      const json = await host.loadPaths();
      if (json) {
        const paths = composePaths(json, await host.loadPathOverrides?.());
        const count = paths.fileTypes.reduce((n, ft) => n + ft.paths.length, 0);
        connection.console.info(`molang: ${count} Molang paths in ${paths.fileTypes.length} JSON file types`);
        return paths;
      }
    } catch (e) {
      connection.console.error(`molang: the Molang path catalogue does not load: ${e}`);
    }
    return builtinPaths;
  };

  connection.onInitialize((params): InitializeResult => {
    const init = (params.initializationOptions ?? {}) as InitializationOptions;
    applySettings(init.settings);
    if (init.environment) environment = init.environment;
    host.initialize?.(init);
    canRefreshTokens = !!params.capabilities.workspace?.semanticTokens?.refreshSupport;
    canRefreshHints = !!params.capabilities.workspace?.inlayHint?.refreshSupport;
    service = (async () => {
      const t0 = host.uptime();
      const engine = await host.loadEngine();
      const t1 = host.uptime();
      const catalogue = await loadCatalogue(engine);
      paths = new JsonPathProvider(await loadPaths());
      paths.useSchemas = schemaDetection;
      const s = new MolangService(engine, catalogue, [new MolangFileProvider(), paths]);
      s.settings = settings;
      s.environment = environment;
      connection.console.info(
        `molang: ready ${Math.round(host.uptime())} ms after start (WebAssembly ${Math.round(t1 - t0)} ms)`,
      );
      return s;
    })();
    service.catch((e) => connection.console.error(`molang: failed to start: ${e?.stack ?? e}`));
    return {
      capabilities: {
        textDocumentSync: INCREMENTAL_SYNC,
        completionProvider: { triggerCharacters: ['.', '>'] },
        hoverProvider: true,
        signatureHelpProvider: { triggerCharacters: ['(', ','], retriggerCharacters: [','] },
        semanticTokensProvider: { legend: { tokenTypes: TOKEN_TYPES, tokenModifiers: TOKEN_MODIFIERS }, full: true },
        documentSymbolProvider: true,
        documentFormattingProvider: true,
        documentRangeFormattingProvider: true,
        codeActionProvider: { codeActionKinds: [CODE_ACTION_QUICKFIX, CODE_ACTION_REWRITE] },
        definitionProvider: true,
        referencesProvider: true,
        renameProvider: { prepareProvider: true },
        inlayHintProvider: true,
      },
      serverInfo: { name: 'molang' },
    };
  });

  // Diagnostics are published a moment after the last change rather than on
  // every keystroke; each run analyses every region of the document.
  const pending = new Map<string, ReturnType<typeof setTimeout>>();
  const validate = (doc: TextDocument, delay = 150) => {
    clearTimeout(pending.get(doc.uri));
    pending.set(
      doc.uri,
      setTimeout(async () => {
        pending.delete(doc.uri);
        const s = await service!;
        let current = documents.get(doc.uri);
        if (!current || !s.handles(current)) return;
        const byteOrderMark = MOLANG_LANGUAGE_IDS.includes(current.languageId)
          ? await host.hasByteOrderMark?.(current.uri).catch(() => false)
          : false;
        // The document may have moved on while the file was read.
        current = documents.get(doc.uri);
        if (!current) return;
        connection.sendDiagnostics({
          uri: current.uri,
          version: current.version,
          diagnostics: s.diagnostics(current, { byteOrderMark }),
        });
      }, delay),
    );
  };
  const revalidateAll = async () => {
    const s = await service!;
    s.settings = settings;
    s.environment = environment;
    s.invalidate();
    for (const doc of documents.all()) validate(doc, 0);
    if (canRefreshTokens) connection.languages.semanticTokens.refresh();
    if (canRefreshHints) connection.languages.inlayHint.refresh();
  };

  // A document just opened is checked at once; only edits wait for a pause.
  const opened = new Set<string>();
  documents.onDidOpen((e) => opened.add(e.document.uri));
  documents.onDidChangeContent((e) => validate(e.document, opened.delete(e.document.uri) ? 0 : 150));
  // A file's encoding changes on save, not on edit.
  documents.onDidSave((e) => validate(e.document, 0));
  documents.onDidClose(async (e) => {
    clearTimeout(pending.get(e.document.uri));
    (await service)?.invalidate(e.document.uri);
    paths?.setSchemaPaths(e.document.uri, [], []);
    connection.sendDiagnostics({ uri: e.document.uri, diagnostics: [] });
  });

  connection.onDidChangeConfiguration(async (params) => {
    const reload = applySettings((params.settings as { molang?: ClientSettings })?.molang);
    if (reload) {
      const s = await service!;
      // The engine keeps its old catalogue if the new one fails to load,
      // and so does the service.
      s.catalogue = await loadCatalogue(s.engine);
    }
    await revalidateAll();
  });

  connection.onNotification(EnvironmentNotification, async (env: Environment) => {
    environment = env;
    await revalidateAll();
  });

  // Diagnostics never wait for schemas: a document is checked against the
  // catalogue as soon as it opens, and again when (if) its schemas add paths.
  connection.onNotification(SchemaPathsNotification, async (p: SchemaPathsParams) => {
    const s = await service!;
    if (!paths?.setSchemaPaths(p.uri, p.fileTypes, p.schemas)) return;
    s.invalidate(p.uri);
    const doc = documents.get(p.uri);
    if (doc) validate(doc, 0);
    if (canRefreshTokens) connection.languages.semanticTokens.refresh();
  });

  const withDoc = async <T>(uri: string, empty: T, f: (s: MolangService, doc: TextDocument) => T): Promise<T> => {
    const s = await service!;
    const doc = documents.get(uri);
    return doc && s.handles(doc) ? f(s, doc) : empty;
  };

  connection.onCompletion((p) =>
    withDoc(p.textDocument.uri, [], (s, doc) => s.completion(doc, doc.offsetAt(p.position))),
  );
  connection.onHover((p) => withDoc(p.textDocument.uri, null, (s, doc) => s.hover(doc, doc.offsetAt(p.position))));
  connection.onSignatureHelp((p) =>
    withDoc(p.textDocument.uri, null, (s, doc) => s.signatureHelp(doc, doc.offsetAt(p.position))),
  );
  connection.languages.semanticTokens.on((p) =>
    withDoc(p.textDocument.uri, { data: [] }, (s, doc) => {
      const data = s.semanticTokens(doc);
      // No result rather than an empty one where this extension stands
      // aside, so another provider's tokens can apply.
      return data ? { data } : (null as unknown as { data: number[] });
    }),
  );
  connection.onDocumentSymbol((p) => withDoc(p.textDocument.uri, [], (s, doc) => s.documentSymbols(doc)));
  const edits = (r: TextEdit[] | { error: string }) => {
    if ('error' in r) {
      connection.window.showInformationMessage(`Molang: not formatted. ${r.error}`);
      return [];
    }
    return r;
  };
  connection.onDocumentFormatting((p) => withDoc(p.textDocument.uri, [], (s, doc) => edits(s.formatEdits(doc, p.options))));
  connection.onDocumentRangeFormatting((p) =>
    withDoc(p.textDocument.uri, [], (s, doc) => edits(s.rangeFormatEdits(doc, p.range, p.options))),
  );
  connection.onRequest(RegionsRequest, (p: { uri: string }) =>
    withDoc<RegionsResponse>(p.uri, { regions: [] }, (s, doc) => ({ regions: s.regions(doc) })),
  );
  connection.onCodeAction((p) =>
    withDoc(p.textDocument.uri, [], (s, doc) => s.codeActions(doc, p.range, p.context.diagnostics, p.context.only)),
  );
  connection.onDefinition((p) => withDoc(p.textDocument.uri, [], (s, doc) => s.definition(doc, doc.offsetAt(p.position))));
  connection.onReferences((p) =>
    withDoc(p.textDocument.uri, [], (s, doc) =>
      s.references(doc, doc.offsetAt(p.position), p.context.includeDeclaration),
    ),
  );
  connection.onPrepareRename((p) =>
    withDoc(p.textDocument.uri, null, (s, doc) => s.prepareRename(doc, doc.offsetAt(p.position))),
  );
  connection.onRenameRequest(async (p) => {
    const r = await withDoc(p.textDocument.uri, null, (s, doc) => s.rename(doc, doc.offsetAt(p.position), p.newName));
    if (r && 'error' in r) throw new ResponseError(ErrorCodes.InvalidRequest, r.error as string);
    return r;
  });
  connection.languages.inlayHint.on((p) => withDoc(p.textDocument.uri, [], (s, doc) => s.inlayHints(doc, p.range)));
  connection.onRequest(PrintRequest, (p: PrintParams): Promise<PrintResponse> =>
    withDoc<PrintResponse>(p.uri, { error: 'Not a Molang document.' }, (s, doc) => s.print(doc, p.how)),
  );
}
