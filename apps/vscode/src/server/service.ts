// The language features, over regions. Nothing here knows about the
// connection, the file system or WebAssembly: it takes a document and an
// engine and answers in protocol types, so it runs the same in the Node
// server, a future browser server, and the unit tests.

import type { TextDocument } from 'vscode-languageserver-textdocument';
import {
  CodeActionKind,
  DiagnosticSeverity,
  DiagnosticTag,
  MarkupKind,
  Range,
  SymbolKind,
  TextEdit,
  type CodeAction,
  type CompletionItem,
  type Diagnostic,
  type DocumentSymbol,
  type Hover,
  type SignatureHelp,
} from 'vscode-languageserver-types';
import type { AnalyzeOptions, AnalyzeResult, BridgeRef, CanonicalNamespace, FormatOptions, MolangEngine } from './bridge';
import { argRange, entryDocs, functionDocs, signatureLabel, type Catalogue } from './catalogue';
import { completionContext, completionItems, type KnownName } from './completion';
import { JSON_LANGUAGE_IDS } from './embedding';
import { decodeJsonString } from './jsonString';
import { inSpans, overlapsSpans, stripMolangFile } from './molangFile';
import { MOLANG_LANGUAGE_IDS, type MolangRegion, type MolangRegionProvider } from './regions';
import { encodeTokens, type HostToken } from './semantic';
import { findCall } from './signature';

export type Toggle = 'auto' | 'on' | 'off';

export interface Settings {
  unknownQueries: 'default' | 'error' | 'warning' | 'information' | 'hint' | 'off';
  json: { enabled: boolean; completion: Toggle; semanticTokens: Toggle };
  /** indentSize null follows the editor's tab size and tabs setting. */
  format: { indentSize: number | null; lineWidth: number };
}

export const defaultSettings: Settings = {
  unknownQueries: 'default',
  json: { enabled: true, completion: 'auto', semanticTokens: 'auto' },
  format: { indentSize: null, lineWidth: 100 },
};

/**
 * What the client knows about its surroundings that the server cannot see:
 * other extensions. Blockception's own Molang support runs on the same pack
 * JSON, and some features of two extensions merge badly (see settings).
 */
export interface Environment {
  blockceptionActive: boolean;
  /** Blockception's own JSON completion switch, BC-MC.Completion.JSON. */
  blockceptionJsonCompletion: boolean;
}

interface RegionAnalysis {
  region: MolangRegion;
  result: AnalyzeResult;
}

interface Occurrence {
  start: number; // document offsets
  end: number;
  write: boolean;
}

export interface DocumentAnalysis {
  version: number;
  isJson: boolean;
  regions: RegionAnalysis[];
  /** Every variable/temp/context/array occurrence, by "namespace.name". */
  occurrences: Map<string, { namespace: CanonicalNamespace; name: string; at: Occurrence[] }>;
}

const severities: Record<string, DiagnosticSeverity> = {
  error: DiagnosticSeverity.Error,
  warning: DiagnosticSeverity.Warning,
  information: DiagnosticSeverity.Information,
  hint: DiagnosticSeverity.Hint,
};

const ownState = new Set<CanonicalNamespace>(['variable', 'temp', 'context', 'array']);

export class MolangService {
  settings: Settings = defaultSettings;
  environment: Environment = { blockceptionActive: false, blockceptionJsonCompletion: false };
  private readonly cache = new Map<string, DocumentAnalysis>();

  constructor(
    readonly engine: MolangEngine,
    public catalogue: Catalogue,
    private readonly providers: readonly MolangRegionProvider[],
  ) {}

  /** Drops cached analyses, after the catalogue or settings change. */
  invalidate(uri?: string) {
    if (uri) this.cache.delete(uri);
    else this.cache.clear();
  }

  handles(doc: TextDocument): boolean {
    return MOLANG_LANGUAGE_IDS.includes(doc.languageId) || JSON_LANGUAGE_IDS.includes(doc.languageId);
  }

  analyze(doc: TextDocument): DocumentAnalysis | undefined {
    const cached = this.cache.get(doc.uri);
    if (cached && cached.version === doc.version) return cached;
    const isJson = JSON_LANGUAGE_IDS.includes(doc.languageId);
    if (isJson && !this.settings.json.enabled) return undefined;
    let regions: MolangRegion[] | undefined;
    for (const p of this.providers) {
      regions = p.provideRegions(doc);
      if (regions) break;
    }
    if (!regions) return undefined;
    const analysis: DocumentAnalysis = { version: doc.version, isJson, regions: [], occurrences: new Map() };
    for (const region of regions) {
      const result = this.engine.analyze(region.text, this.optionsFor(region));
      analysis.regions.push({ region, result });
      for (const r of result.refs) {
        if (!ownState.has(r.namespace)) continue;
        const key = `${r.namespace}.${r.name.toLowerCase()}`;
        let entry = analysis.occurrences.get(key);
        if (!entry) {
          entry = { namespace: r.namespace, name: r.name, at: [] };
          analysis.occurrences.set(key, entry);
        }
        entry.at.push({ start: region.toHost(r.start), end: region.toHost(r.end), write: !!r.write });
      }
    }
    this.cache.set(doc.uri, analysis);
    return analysis;
  }

  private optionsFor(region: MolangRegion): AnalyzeOptions {
    const opts: AnalyzeOptions = { ...region.options };
    if (this.settings.unknownQueries !== 'default') opts.unknownQueries = this.settings.unknownQueries;
    return opts;
  }

  diagnostics(doc: TextDocument): Diagnostic[] {
    const a = this.analyze(doc);
    if (!a) return [];
    const out: Diagnostic[] = [];
    for (const { region, result } of a.regions) {
      for (const d of result.diagnostics) {
        // A template stands in for something the pack's build supplies;
        // whatever the stand-in trips over is not the author's Molang.
        if (overlapsSpans(region.inert, d.start, d.end)) continue;
        out.push({
          range: Range.create(doc.positionAt(region.toHost(d.start)), doc.positionAt(region.toHost(d.end))),
          severity: severities[d.severity] ?? DiagnosticSeverity.Error,
          source: 'molang',
          code: d.code,
          message: d.message,
          tags: d.tags?.includes('deprecated') ? [DiagnosticTag.Deprecated] : undefined,
        });
      }
    }
    return out;
  }

  /** The region containing a document offset, and the offset within it. */
  private locate(a: DocumentAnalysis, offset: number): { ra: RegionAnalysis; at: number } | undefined {
    for (const ra of a.regions) {
      const at = ra.region.fromHost(offset);
      if (at === undefined) continue;
      if (inSpans(ra.region.inert, at)) return undefined;
      return { ra, at };
    }
    return undefined;
  }

  completionEnabled(doc: TextDocument): boolean {
    if (!JSON_LANGUAGE_IDS.includes(doc.languageId)) return true;
    const mode = this.settings.json.completion;
    if (mode === 'auto') return !(this.environment.blockceptionActive && this.environment.blockceptionJsonCompletion);
    return mode === 'on';
  }

  semanticTokensEnabled(doc: TextDocument): boolean {
    if (!JSON_LANGUAGE_IDS.includes(doc.languageId)) return true;
    const mode = this.settings.json.semanticTokens;
    if (mode === 'auto') return !this.environment.blockceptionActive;
    return mode === 'on';
  }

  completion(doc: TextDocument, offset: number): CompletionItem[] {
    if (!this.completionEnabled(doc)) return [];
    const a = this.analyze(doc);
    const hit = a && this.locate(a, offset);
    if (!a || !hit) return [];
    const { ra, at } = hit;
    const ctx = completionContext(ra.region.text, at);
    if (ctx.kind === 'none') return [];
    const names: KnownName[] = [];
    for (const o of a.occurrences.values()) {
      names.push({
        namespace: o.namespace,
        name: o.name,
        reads: o.at.filter((x) => !x.write).length,
        writes: o.at.filter((x) => x.write).length,
      });
    }
    const items = completionItems({ context: ctx, catalogue: this.catalogue, names, options: ra.region.options });
    const range = Range.create(doc.positionAt(ra.region.toHost(ctx.start)), doc.positionAt(ra.region.toHost(ctx.end)));
    return items.map((item) => ({
      ...item,
      textEdit: TextEdit.replace(range, item.insertText ?? item.label),
      insertText: undefined,
      filterText: item.label,
    }));
  }

  hover(doc: TextDocument, offset: number): Hover | null {
    const a = this.analyze(doc);
    const hit = a && this.locate(a, offset);
    if (!a || !hit) return null;
    const { ra, at } = hit;
    const ref = ra.result.refs.find((r) => at >= r.start && at < r.end);
    if (!ref) return this.tokenHover(doc, ra, at);
    const range = Range.create(doc.positionAt(ra.region.toHost(ref.start)), doc.positionAt(ra.region.toHost(ref.end)));
    const value = at < ref.nameStart ? this.namespaceHover(ref) : this.memberHover(doc, a, ref);
    return value ? { contents: { kind: MarkupKind.Markdown, value }, range } : null;
  }

  private namespaceHover(ref: BridgeRef): string {
    const entry = this.catalogue.namespace(ref.namespace);
    return [`\`${ref.namespace}\` namespace`, entry && entryDocs(entry)].filter(Boolean).join('\n\n');
  }

  /** An operator or keyword: `->`, `??`, `? :`, loop, return, this... */
  private tokenHover(doc: TextDocument, ra: RegionAnalysis, at: number): Hover | null {
    const t = ra.result.tokens.find((x) => at >= x.start && at < x.end && (x.type === 'operator' || x.type === 'keyword'));
    if (!t) return null;
    let text = ra.region.text.slice(t.start, t.end).toLowerCase();
    if (text === '?' || text === ':') text = '? :';
    const entry = this.catalogue.operators.get(text) ?? (text === 'this' ? this.catalogue.namespaces.get('this') : undefined);
    if (!entry) return null;
    const range = Range.create(doc.positionAt(ra.region.toHost(t.start)), doc.positionAt(ra.region.toHost(t.end)));
    const value = [code(text), entryDocs(entry)].filter(Boolean).join('\n\n');
    return { contents: { kind: MarkupKind.Markdown, value }, range };
  }

  private memberHover(doc: TextDocument, a: DocumentAnalysis, ref: BridgeRef): string {
    if (ref.namespace === 'query' || ref.namespace === 'math') {
      // query.spellcolor.b is a member of what query.spellcolor returns.
      const f =
        this.catalogue.lookup(ref.namespace, ref.name) ??
        (ref.name.includes('.') ? this.catalogue.lookup(ref.namespace, ref.name.split('.')[0]) : undefined);
      if (!f) {
        const what = ref.namespace === 'query' ? 'the catalogue' : 'the math library';
        return code(`${ref.namespace}.${ref.name}`) + `\n\nNot in ${what}.`;
      }
      const docs = functionDocs(ref.namespace, f, this.catalogue);
      return code(signatureLabel(ref.namespace, f)) + (docs ? `\n\n${docs}` : '');
    }
    const head = code(`${ref.namespace}.${ref.name}`);
    const o = a.occurrences.get(`${ref.namespace}.${ref.name.toLowerCase()}`);
    if (!o) return head;
    const lines = (list: Occurrence[]) => {
      const nums = [...new Set(list.map((x) => doc.positionAt(x.start).line + 1))];
      const shown = nums.slice(0, 12).join(', ');
      return nums.length > 12 ? `${shown} and ${nums.length - 12} more` : shown;
    };
    const writes = o.at.filter((x) => x.write);
    const reads = o.at.filter((x) => !x.write);
    const parts = [head];
    if (ref.namespace === 'context') {
      const known = this.catalogue.contextName(ref.name);
      if (known?.description) parts.push(known.description);
      if (known?.contexts?.length) parts.push(`Supplied in: ${known.contexts.join('; ')}.`);
      if (!known) parts.push('Supplied by the game; read-only.');
    } else if (writes.length) {
      parts.push(`Written on line${writes.length > 1 ? 's' : ''} ${lines(writes)}.`);
    } else if (ref.namespace === 'variable' || ref.namespace === 'temp') {
      parts.push('Not written in this file.');
    }
    if (reads.length) parts.push(`Read ${reads.length}× (line${reads.length > 1 ? 's' : ''} ${lines(reads)}).`);
    return parts.join('\n\n');
  }

  signatureHelp(doc: TextDocument, offset: number): SignatureHelp | null {
    const a = this.analyze(doc);
    const hit = a && this.locate(a, offset);
    if (!a || !hit) return null;
    const call = findCall(hit.ra.region.text, hit.at);
    if (!call || (call.namespace !== 'query' && call.namespace !== 'math')) return null;
    const f = this.catalogue.lookup(call.namespace, call.name);
    if (!f) return null;
    const args = f.args ?? [];
    const { max } = argRange(f);
    // The label is built here rather than taken from signatureLabel, which
    // leaves the parentheses off a query without arguments; inside a call
    // they are being written. Parameters are given as offsets into it.
    let label = `${call.namespace}.${f.name}(`;
    const parameters = args.map((p, i) => {
      const text = `${f.variadic && i === args.length - 1 ? '...' : ''}${p.name}${p.optional ? '?' : ''}${p.type ? `: ${p.type}` : ''}`;
      if (i > 0) label += ', ';
      const start = label.length;
      label += text;
      return {
        label: [start, label.length] as [number, number],
        documentation: p.description ? { kind: MarkupKind.Markdown, value: p.description } : undefined,
      };
    });
    label += ')' + (f.returns && f.returns !== 'unknown' ? `: ${f.returns}` : '');
    let active = call.argument;
    if (f.variadic && args.length) active = Math.min(active, args.length - 1);
    // Past the last parameter of a fixed list, nothing is highlighted: the
    // diagnostics say the call has too many.
    const beyond = max >= 0 && active >= args.length;
    return {
      signatures: [
        {
          label,
          documentation: { kind: MarkupKind.Markdown, value: functionDocs(call.namespace, f, this.catalogue) },
          parameters,
          activeParameter: beyond ? undefined : active,
        },
      ],
      activeSignature: 0,
      activeParameter: beyond ? undefined : active,
    };
  }

  semanticTokens(doc: TextDocument): number[] | null {
    if (!this.semanticTokensEnabled(doc)) return null;
    const a = this.analyze(doc);
    if (!a) return null;
    const tokens: HostToken[] = [];
    for (const { region, result } of a.regions) {
      for (const t of result.tokens) {
        if (overlapsSpans(region.inert, t.start, t.end)) continue;
        tokens.push({ start: region.toHost(t.start), end: region.toHost(t.end), type: t.type, mods: t.mods });
      }
    }
    return encodeTokens(tokens, doc, doc.getText());
  }

  documentSymbols(doc: TextDocument): DocumentSymbol[] {
    // The JSON outline belongs to the JSON language features.
    if (!MOLANG_LANGUAGE_IDS.includes(doc.languageId)) return [];
    const a = this.analyze(doc);
    if (!a) return [];
    const out: DocumentSymbol[] = [];
    for (const o of a.occurrences.values()) {
      const first = o.at.find((x) => x.write) ?? o.at[0];
      const range = Range.create(doc.positionAt(first.start), doc.positionAt(first.end));
      const writes = o.at.filter((x) => x.write).length;
      const reads = o.at.length - writes;
      out.push({
        name: `${o.namespace}.${o.name}`,
        detail: [writes && `${writes} write${writes > 1 ? 's' : ''}`, reads && `${reads} read${reads > 1 ? 's' : ''}`]
          .filter(Boolean)
          .join(', '),
        kind: o.namespace === 'context' ? SymbolKind.Constant : o.namespace === 'array' ? SymbolKind.Array : SymbolKind.Variable,
        range,
        selectionRange: range,
      });
    }
    out.sort((x, y) => x.range.start.line - y.range.start.line || x.range.start.character - y.range.start.character);
    return out;
  }

  /** The formatting options for a .molang document, from the editor's and ours. */
  private layout(editor?: EditorFormatting): FormatOptions {
    const indent = this.settings.format.indentSize ?? editor?.tabSize ?? 4;
    return {
      indentSize: indent,
      useTabs: editor ? !editor.insertSpaces && this.settings.format.indentSize == null : false,
      lineWidth: this.settings.format.lineWidth,
      comments: true,
      templates: true,
    };
  }

  /**
   * The whole .molang document formatted over several lines, comments and
   * templates kept, or minified. Minifying has nowhere to put a comment, so
   * a file with one is refused rather than stripped of it.
   */
  print(doc: TextDocument, how: 'format' | 'minify', editor?: EditorFormatting): { text: string } | { error: string } {
    if (!MOLANG_LANGUAGE_IDS.includes(doc.languageId)) return { error: 'Only .molang documents can be printed.' };
    const text = doc.getText();
    if (!stripMolangFile(text).code.trim()) return { error: 'The file has no Molang.' };
    if (how === 'minify' && stripMolangFile(text).comments.length) {
      return { error: 'This file has comments, which minifying would remove.' };
    }
    const r = this.engine.formatSource(text, { ...this.layout(editor), style: how === 'format' ? 'layout' : 'minify' });
    if (!r.ok) return { error: `The file does not parse: ${r.error ?? 'unknown error'}` };
    // Keep a final newline if the file had one.
    return { text: /\r?\n$/.test(text) ? r.text + '\n' : r.text };
  }

  formatEdits(doc: TextDocument, editor?: EditorFormatting): TextEdit[] | { error: string } {
    // JSON is the JSON formatter's; its Molang has the code actions.
    if (!MOLANG_LANGUAGE_IDS.includes(doc.languageId)) return [];
    const r = this.print(doc, 'format', editor);
    if ('error' in r) return r;
    if (r.text === doc.getText()) return [];
    return [TextEdit.replace(Range.create(doc.positionAt(0), doc.positionAt(doc.getText().length)), r.text)];
  }

  /**
   * Formats the top-level statements of a .molang document that range
   * touches, each whole, with their comments.
   */
  rangeFormatEdits(doc: TextDocument, range: Range, editor?: EditorFormatting): TextEdit[] | { error: string } {
    if (!MOLANG_LANGUAGE_IDS.includes(doc.languageId)) return [];
    const text = doc.getText();
    if (!stripMolangFile(text).code.trim()) return [];
    const r = this.engine.formatSource(text, {
      ...this.layout(editor),
      rangeStart: doc.offsetAt(range.start),
      rangeEnd: doc.offsetAt(range.end),
    });
    if (!r.ok) return { error: `The file does not parse: ${r.error ?? 'unknown error'}` };
    if (r.start === r.end || text.slice(r.start, r.end) === r.text) return [];
    return [TextEdit.replace(Range.create(doc.positionAt(r.start), doc.positionAt(r.end)), r.text)];
  }

  /**
   * "Format Molang in this string" and "Minify Molang in this string" for the
   * JSON string at range. Molang in JSON is one line, so it is formatted in
   * the one-line style. A region joined from several strings -- an array
   * the game reads as one program -- has no single string to rewrite, and
   * gets neither.
   */
  codeActions(doc: TextDocument, range: Range): CodeAction[] {
    if (!JSON_LANGUAGE_IDS.includes(doc.languageId)) return [];
    const a = this.analyze(doc);
    const hit = a && this.locate(a, doc.offsetAt(range.start));
    if (!hit) return [];
    const { region } = hit.ra;
    if (doc.offsetAt(range.end) > region.hostEnd) return [];
    const raw = doc.getText().slice(region.hostStart, region.hostEnd);
    if (decodeJsonString(raw).value !== region.text) return [];
    const out: CodeAction[] = [];
    for (const [style, title] of [
      ['oneLine', 'Format Molang in this string'],
      ['minify', 'Minify Molang in this string'],
    ] as const) {
      const r = this.engine.formatSource(region.text, {
        style,
        templates: true,
        optionalSemicolons: region.options.optionalSemicolons,
      });
      if (!r.ok || r.text === region.text) continue;
      const escaped = JSON.stringify(r.text).slice(1, -1);
      const edit = TextEdit.replace(Range.create(doc.positionAt(region.hostStart), doc.positionAt(region.hostEnd)), escaped);
      out.push({ title, kind: CodeActionKind.RefactorRewrite, edit: { changes: { [doc.uri]: [edit] } } });
    }
    return out;
  }
}

/** The editor's own formatting options, as a formatting request carries them. */
export interface EditorFormatting {
  tabSize: number;
  insertSpaces: boolean;
}

function code(s: string): string {
  return '```molang\n' + s + '\n```';
}

