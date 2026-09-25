// Schema-driven Molang paths: for each JSON document open in the editor, the
// schemas VS Code applies to it (schemaMatch.ts), loaded and walked for
// Molang markers (schemaWalker.ts), sent to the server as path patterns that
// it merges under the curated catalogue (server/embedding.ts).
//
// This runs in the extension host because that is where the inputs are: the
// other extensions' contributions, the settings, and schemas served by
// another extension's content provider (regolith://...), which only
// openTextDocument can read.
//
// Nothing here holds diagnostics up. The server checks a document against
// the catalogue as soon as it opens; the schema paths arrive when they are
// ready and the document is checked again only if they add anything. A
// schema is loaded and walked once and kept, per URI: a local file until its
// modification time changes, a remote one for the session (a failed fetch is
// retried after a few minutes), anything else until it is reopened.

import * as vscode from 'vscode';
import { parse as parseJsonc } from 'jsonc-parser';
import type { FileType } from '../server/embedding';
import {
  associationMatches,
  documentSchema,
  extensionAssociations,
  settingAssociations,
  resolveUriRelative,
  type Association,
} from './schemaMatch';
import { externalRefs, findMolangPaths, hitsToFileTypes, SchemaSet, schemaKey } from './schemaWalker';

const JSON_LANGUAGES = ['json', 'jsonc'];
const HTTP_TIMEOUT_MS = 5000;
const HTTP_RETRY_MS = 5 * 60 * 1000;
// Documents one schema may pull in through its references: Mojang's schema
// set is split into many small files, but never this many for one document.
const MAX_SCHEMA_DOCUMENTS = 400;

export interface SchemaPathsParams {
  uri: string;
  fileTypes: FileType[];
  schemas: string[];
}

interface Loaded {
  doc?: unknown;
  stamp: string;
  failedAt?: number;
}

interface Walked {
  /** Every document the walk read, by key, with the stamp it had. */
  deps: Map<string, { uri: string; stamp: string }>;
  fileTypes: FileType[];
}

/** What the index knows about one open document, for the regions command. */
export interface DocumentSchemas {
  schemas: { uri: string; paths: number; error?: string }[];
}

export class SchemaIndex implements vscode.Disposable {
  private readonly loaded = new Map<string, Loaded>();
  private readonly walked = new Map<string, Promise<Walked>>();
  private readonly sent = new Map<string, string>();
  private readonly lastSchema = new Map<string, string | undefined>();
  private readonly perDocument = new Map<string, DocumentSchemas>();
  private readonly timers = new Map<string, ReturnType<typeof setTimeout>>();
  private readonly generation = new Map<string, number>();
  /** Schema documents this index opened itself, which are not pack files. */
  private readonly ownDocuments = new Set<string>();
  private readonly logged = new Set<string>();
  private associationsCache: Association[] | undefined;
  private readonly disposables: vscode.Disposable[] = [];

  constructor(
    private readonly send: (p: SchemaPathsParams) => void,
    private readonly log: (message: string) => void,
  ) {
    this.disposables.push(
      vscode.workspace.onDidOpenTextDocument((d) => this.schedule(d, 0)),
      vscode.workspace.onDidChangeTextDocument((e) => {
        // Only a change of "$schema" changes which schemas apply.
        const doc = e.document;
        if (this.lastSchema.has(doc.uri.toString()) && documentSchema(doc.getText()) !== this.lastSchema.get(doc.uri.toString())) {
          this.schedule(doc, 300);
        }
      }),
      vscode.workspace.onDidCloseTextDocument((d) => this.forget(d.uri.toString())),
      vscode.workspace.onDidSaveTextDocument((d) => this.schemaSaved(d.uri.toString())),
      vscode.workspace.onDidChangeConfiguration((e) => {
        if (e.affectsConfiguration('json.schemas') || e.affectsConfiguration('json.schemaDownload') || e.affectsConfiguration('molang.json')) {
          this.associationsCache = undefined;
          this.refreshAll();
        }
      }),
      vscode.extensions.onDidChange(() => {
        this.associationsCache = undefined;
        this.refreshAll();
      }),
    );
  }

  dispose() {
    for (const d of this.disposables) d.dispose();
    for (const t of this.timers.values()) clearTimeout(t);
  }

  refreshAll() {
    for (const doc of vscode.workspace.textDocuments) this.schedule(doc, 0);
  }

  /** The schemas applied to a document, as last computed. */
  schemasOf(uri: string): DocumentSchemas | undefined {
    return this.perDocument.get(uri);
  }

  private enabled(): boolean {
    const c = vscode.workspace.getConfiguration('molang.json');
    return c.get<boolean>('enabled', true) && c.get<boolean>('schemaDetection', true);
  }

  private schedule(doc: vscode.TextDocument, delay: number) {
    const uri = doc.uri.toString();
    if (!JSON_LANGUAGES.includes(doc.languageId) || this.ownDocuments.has(uri)) return;
    if (doc.uri.scheme === 'git' || doc.uri.scheme === 'output') return;
    clearTimeout(this.timers.get(uri));
    this.timers.set(
      uri,
      setTimeout(() => {
        this.timers.delete(uri);
        this.update(doc).catch((e) => this.log(`schemas for ${uri}: ${e?.stack ?? e}`));
      }, delay),
    );
  }

  private forget(uri: string) {
    clearTimeout(this.timers.get(uri));
    this.timers.delete(uri);
    this.sent.delete(uri);
    this.lastSchema.delete(uri);
    this.perDocument.delete(uri);
    this.generation.delete(uri);
    this.ownDocuments.delete(uri);
  }

  private schemaSaved(uri: string) {
    // The stamps catch a changed schema file the next time a document is
    // looked at; its save is the moment to look, so the documents using it
    // follow without being reopened.
    if (this.loaded.has(schemaKey(uri))) this.refreshAll();
  }

  private async update(doc: vscode.TextDocument) {
    const uri = doc.uri.toString();
    const gen = (this.generation.get(uri) ?? 0) + 1;
    this.generation.set(uri, gen);
    const text = doc.getText();
    const own = documentSchema(text);
    this.lastSchema.set(uri, own);
    if (!this.enabled()) {
      this.perDocument.delete(uri);
      this.sendIfChanged(uri, [], []);
      return;
    }

    const applied: { uri?: string; schema?: unknown; label: string }[] = [];
    if (own !== undefined) {
      // The document's own "$schema" replaces every association, as in
      // VS Code's JSON language features.
      const abs = resolveUriRelative(own, uri);
      if (abs) applied.push({ uri: abs, label: abs });
    } else {
      const matchUri = doc.uri.with({ query: '', fragment: '' }).toString(true);
      this.associations().forEach((a, i) => {
        if (!associationMatches(a, matchUri)) return;
        if (a.uri) applied.push({ uri: a.uri, label: a.uri });
        else applied.push({ schema: a.schema, label: `json.schemas setting (inline schema ${i + 1})` });
      });
    }

    const info: DocumentSchemas = { schemas: [] };
    const fileTypes: FileType[] = [];
    const used: string[] = [];
    for (const a of applied) {
      try {
        const w = a.uri ? await this.walk(a.uri) : walkInline(a.schema);
        const n = w.fileTypes.reduce((k, ft) => k + ft.paths.length, 0);
        info.schemas.push({ uri: a.label, paths: n });
        if (n) {
          fileTypes.push(...w.fileTypes);
          used.push(a.label);
        }
      } catch (e) {
        info.schemas.push({ uri: a.label, paths: 0, error: String((e as Error)?.message ?? e) });
      }
    }
    // A newer look at this document started while this one waited.
    if (this.generation.get(uri) !== gen) return;
    this.perDocument.set(uri, info);
    this.sendIfChanged(uri, fileTypes, used);
  }

  private sendIfChanged(uri: string, fileTypes: FileType[], schemas: string[]) {
    const json = JSON.stringify([fileTypes, schemas]);
    const before = this.sent.get(uri) ?? JSON.stringify([[], []]);
    if (before === json) return;
    this.sent.set(uri, json);
    this.send({ uri, fileTypes, schemas });
  }

  private associations(): Association[] {
    if (this.associationsCache) return this.associationsCache;
    const out: Association[] = [];
    for (const ext of vscode.extensions.all) {
      out.push(...extensionAssociations(ext.packageJSON?.contributes?.jsonValidation, ext.extensionUri.toString()));
    }
    const inspect = vscode.workspace.getConfiguration('json').inspect('schemas');
    // User settings: a relative URL there is relative to the settings file,
    // which an extension cannot see, so only absolute ones are followed.
    out.push(...settingAssociations(inspect?.globalValue));
    const folders = vscode.workspace.workspaceFolders ?? [];
    const wsFile = vscode.workspace.workspaceFile;
    const wsBase = wsFile && wsFile.scheme !== 'untitled' ? vscode.Uri.joinPath(wsFile, '..') : folders[0]?.uri;
    out.push(...settingAssociations(inspect?.workspaceValue, wsBase?.toString()));
    if (wsFile) {
      // In a multi-root workspace each folder may have its own.
      for (const f of folders) {
        const v = vscode.workspace.getConfiguration('json', f.uri).inspect('schemas')?.workspaceFolderValue;
        out.push(...settingAssociations(v, f.uri.toString(), f.uri.toString(true)));
      }
    }
    this.associationsCache = out;
    return out;
  }

  /** The Molang paths of the schema at uri, walked once and kept while its documents are unchanged. */
  private async walk(uri: string): Promise<Walked> {
    const key = schemaKey(uri);
    const cached = this.walked.get(key);
    if (cached) {
      const w = await cached.catch(() => undefined);
      if (w && (await this.unchanged(w))) return w;
      this.walked.delete(key);
    }
    const p = this.walkFresh(uri);
    this.walked.set(key, p);
    p.catch(() => this.walked.delete(key));
    return p;
  }

  private async unchanged(w: Walked): Promise<boolean> {
    for (const [, dep] of w.deps) {
      if (!dep.uri.startsWith('file:')) continue;
      if ((await fileStamp(dep.uri)) !== dep.stamp) return false;
    }
    return true;
  }

  private async walkFresh(rootUri: string): Promise<Walked> {
    const set = new SchemaSet();
    const deps = new Map<string, { uri: string; stamp: string }>();
    const queue = [rootUri];
    let rootDoc: unknown;
    while (queue.length && deps.size < MAX_SCHEMA_DOCUMENTS) {
      const u = queue.shift()!;
      if (set.has(u)) continue;
      const loaded = await this.load(u);
      if (u === rootUri) {
        if (loaded.doc === undefined) throw new Error(`not loaded`);
        rootDoc = loaded.doc;
      }
      if (loaded.doc === undefined) continue;
      set.add(u, loaded.doc);
      deps.set(schemaKey(u), { uri: u, stamp: loaded.stamp });
      queue.push(...externalRefs(loaded.doc, u));
    }
    const t0 = Date.now();
    const fileTypes = hitsToFileTypes(findMolangPaths(rootDoc, rootUri, set.resolve));
    const n = fileTypes.reduce((k, ft) => k + ft.paths.length, 0);
    this.log(`schema ${rootUri}: ${n} Molang paths from ${deps.size} document(s) in ${Date.now() - t0} ms`);
    return { deps, fileTypes };
  }

  /** A schema document, parsed; doc undefined when it cannot be had. */
  private async load(uri: string): Promise<Loaded> {
    const key = schemaKey(uri);
    const scheme = uri.slice(0, uri.indexOf(':'));
    const cached = this.loaded.get(key);
    if (scheme === 'file') {
      const stamp = await fileStamp(uri);
      if (cached && cached.stamp === stamp) return cached;
      const entry = await this.read(uri, stamp, async () => new TextDecoder().decode(await vscode.workspace.fs.readFile(vscode.Uri.parse(uri))));
      this.loaded.set(key, entry);
      return entry;
    }
    if (cached && (cached.doc !== undefined || Date.now() - (cached.failedAt ?? 0) < HTTP_RETRY_MS)) return cached;
    let entry: Loaded;
    if (scheme === 'vscode') {
      // VS Code's own schemas (settings, tasks, package.json): served to the
      // JSON language features alone, and never about Molang.
      entry = { stamp: '', failedAt: Number.MAX_SAFE_INTEGER };
    } else if (scheme === 'http' || scheme === 'https') {
      if (!vscode.workspace.getConfiguration('json').get<boolean>('schemaDownload.enable', true)) {
        entry = { stamp: '', failedAt: Date.now() };
      } else {
        entry = await this.read(uri, 'remote', () => fetchText(uri));
      }
    } else {
      // Another extension's content provider, or a virtual file system.
      entry = await this.read(uri, 'provided', async () => {
        const d = await vscode.workspace.openTextDocument(vscode.Uri.parse(uri));
        this.ownDocuments.add(d.uri.toString());
        return d.getText();
      });
    }
    this.loaded.set(key, entry);
    return entry;
  }

  private async read(uri: string, stamp: string, get: () => Promise<string>): Promise<Loaded> {
    try {
      const doc = parseJsonc(await get(), [], { allowTrailingComma: true });
      if (doc === undefined) throw new Error('not JSON');
      return { doc, stamp };
    } catch (e) {
      const msg = `schema ${uri} not read: ${(e as Error)?.message ?? e}`;
      if (!this.logged.has(msg)) {
        this.logged.add(msg);
        this.log(msg);
      }
      return { stamp, failedAt: Date.now() };
    }
  }
}

function walkInline(schema: unknown): Walked {
  const base = 'inline:settings';
  const set = new SchemaSet();
  set.add(base, schema);
  return { deps: new Map(), fileTypes: hitsToFileTypes(findMolangPaths(schema, base, set.resolve)) };
}

async function fileStamp(uri: string): Promise<string> {
  try {
    const st = await vscode.workspace.fs.stat(vscode.Uri.parse(uri));
    return `${st.mtime}:${st.size}`;
  } catch {
    return 'missing';
  }
}

async function fetchText(uri: string): Promise<string> {
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), HTTP_TIMEOUT_MS);
  try {
    const res = await fetch(uri, { signal: ctl.signal, headers: { 'Accept-Encoding': 'gzip, deflate' } });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return await res.text();
  } finally {
    clearTimeout(timer);
  }
}
