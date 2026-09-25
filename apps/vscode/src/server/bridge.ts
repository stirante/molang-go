// The typed face of cmd/molang-wasm. The shapes here are the ones documented
// at the top of cmd/molang-wasm/main.go, which is the reference; keep the two
// in step.
//
// Nothing here touches Node or the DOM. Where the module bytes come from is
// the caller's business (a file in the Node server, a fetch in a web
// extension), which is the seam that keeps a vscode.dev build possible.

import './generated/wasm_exec.js';

export interface AnalyzeOptions {
  /** The query set the field resolves names in; absent accepts any. */
  querySet?: 'default' | 'tags' | 'world_gen';
  /** A field's fixed list of queries, in place of the set. */
  allowedQueries?: string[];
  /** The version the file is read at, for version gates; absent skips them. */
  version?: string;
  /** Catalogue context id, for its restrictions; absent is unknown. */
  context?: string;
  /** One of the engine's own operation restrictions. */
  restrict?: 'no_side_effects' | 'no_side_effects_or_random';
  disallowedOps?: string[];
  optionalSemicolons?: boolean;
  unknownQueries?: 'error' | 'warning' | 'information' | 'hint' | 'off';
}

export type Severity = 'error' | 'warning' | 'information' | 'hint';

export interface BridgeDiagnostic {
  /** UTF-16 offsets into the analysed source, end exclusive. */
  start: number;
  end: number;
  byteStart: number;
  byteEnd: number;
  severity: Severity;
  code: string;
  message: string;
  tags?: string[];
}

export interface BridgeToken {
  start: number;
  end: number;
  type: string;
  mods?: string[];
}

export interface BridgeRef {
  namespace: CanonicalNamespace;
  written: string;
  name: string;
  start: number;
  end: number;
  nameStart: number;
  nameEnd: number;
  write?: boolean;
  call?: boolean;
  args: number;
  callEnd?: number;
  arrow?: boolean;
}

export interface Span {
  start: number;
  end: number;
}

export interface BridgeSymbol {
  namespace: CanonicalNamespace;
  name: string;
  reads: Span[];
  writes: Span[];
}

export interface AnalyzeResult {
  ok: boolean;
  diagnostics: BridgeDiagnostic[];
  tokens: BridgeToken[];
  refs: BridgeRef[];
  symbols: BridgeSymbol[];
}

export interface PrintResult {
  ok: boolean;
  text?: string;
  error?: string;
}

/** formatSource's options. See cmd/molang-wasm/main.go. */
export interface FormatOptions {
  /** 'layout' (several lines, comments kept; the default), 'oneLine' or 'minify'. */
  style?: 'layout' | 'oneLine' | 'minify';
  indentSize?: number;
  useTabs?: boolean;
  lineWidth?: number;
  /** `#` to the end of a line is a comment. */
  comments?: boolean;
  /** jsonte's `#{ ... }` is a template, kept as written. */
  templates?: boolean;
  optionalSemicolons?: boolean;
  /** UTF-16 offsets: format only the top-level statements this touches. */
  rangeStart?: number;
  rangeEnd?: number;
}

/** text replaces [start, end) of the source, in UTF-16 offsets. */
export interface FormatSourceResult {
  ok: boolean;
  text: string;
  start: number;
  end: number;
  error?: string;
}

export interface CatalogueSummary {
  ok: boolean;
  error?: string;
  gameVersion?: string;
  queries: number;
  math: number;
  contexts: number;
  partial?: boolean;
}

export type CanonicalNamespace =
  | 'query'
  | 'math'
  | 'variable'
  | 'temp'
  | 'context'
  | 'array'
  | 'geometry'
  | 'material'
  | 'texture';

/** What the language service needs from the bridge; tests substitute it. */
export interface MolangEngine {
  setCatalogue(json: string): CatalogueSummary;
  analyze(source: string, options?: AnalyzeOptions): AnalyzeResult;
  format(source: string, options?: AnalyzeOptions): PrintResult;
  minify(source: string, options?: AnalyzeOptions): PrintResult;
  formatSource(source: string, options?: FormatOptions): FormatSourceResult;
}

interface RawBridge {
  version: string;
  setCatalogue(json: string): string;
  analyze(source: string, options: string): string;
  format(source: string, options: string): string;
  minify(source: string, options: string): string;
  formatSource(source: string, options: string): string;
}

declare const Go: new () => {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
};

/** The bridge API version this code was written against. */
export const BRIDGE_VERSION = '1';

export class MolangBridge implements MolangEngine {
  private constructor(private readonly raw: RawBridge) {}

  /**
   * Starts the Go program in the module and connects to it. The module
   * registers its API synchronously during run(), before main blocks, so the
   * global is there as soon as run() returns control.
   */
  static async load(module: BufferSource | WebAssembly.Module): Promise<MolangBridge> {
    const go = new Go();
    const instance =
      module instanceof WebAssembly.Module
        ? await WebAssembly.instantiate(module, go.importObject)
        : (await WebAssembly.instantiate(module, go.importObject)).instance;
    // Not awaited: the promise settles only when the Go program exits,
    // which it does not do while the API is in use.
    void go.run(instance);
    const raw = (globalThis as { molangBridge?: RawBridge }).molangBridge;
    if (!raw) {
      throw new Error('molang: the WebAssembly module did not register its API');
    }
    if (raw.version !== BRIDGE_VERSION) {
      throw new Error(`molang: bridge API version ${raw.version}, expected ${BRIDGE_VERSION}`);
    }
    return new MolangBridge(raw);
  }

  setCatalogue(json: string): CatalogueSummary {
    return parse(this.raw.setCatalogue(json));
  }

  analyze(source: string, options: AnalyzeOptions = {}): AnalyzeResult {
    return parse(this.raw.analyze(source, JSON.stringify(options)));
  }

  format(source: string, options: AnalyzeOptions = {}): PrintResult {
    return parse(this.raw.format(source, JSON.stringify(options)));
  }

  minify(source: string, options: AnalyzeOptions = {}): PrintResult {
    return parse(this.raw.minify(source, JSON.stringify(options)));
  }

  formatSource(source: string, options: FormatOptions = {}): FormatSourceResult {
    return parse(this.raw.formatSource(source, JSON.stringify(options)));
  }
}

function parse<T>(json: string): T {
  const value = JSON.parse(json) as T & { error?: string };
  // Every method's result may instead be {error}, the bridge's report of a
  // call it could not make sense of. A result type with its own error field
  // (PrintResult, CatalogueSummary) carries ok alongside it.
  if (value && typeof value === 'object' && 'error' in value && !('ok' in value)) {
    throw new Error(`molang bridge: ${value.error}`);
  }
  return value;
}
