// Molang lives in two kinds of place: .molang files, which are all Molang,
// and strings inside pack JSON, which are Molang only at certain paths. The
// language features are written once, against a MolangRegion -- a piece of
// Molang source with a way back to the document it came from -- and a
// MolangRegionProvider finds the regions of a document. Adding a place
// Molang can live is adding a provider; nothing downstream changes.

import type { AnalyzeOptions } from './bridge';
import { stripMolangFile, type Span } from './molangFile';

/**
 * What kind of Molang a field holds. The grammar is the same everywhere; the
 * kind decides what an expression may name and do there. A schema can say
 * that a string is Molang but never which kind, so every source of regions
 * has to supply it. The names are the path catalogue's
 * (data/molang-paths.json, "kinds"), plus two that narrow one of its kinds
 * by field (see embedding.optionsFor).
 */
export type MolangKind =
  | 'general'
  | 'boolean'
  | 'number'
  | 'color'
  | 'animation'
  | 'render_resource_ref'
  | 'particle'
  | 'event_response'
  | 'string_or_molang'
  | 'block_state'
  | 'tag_filter'
  | 'worldgen'
  // An entity property's default: query.had_component_group only.
  | 'property_default'
  // A set_property event response: query.has_property and query.property only.
  | 'set_property';

const defaultSet: AnalyzeOptions = { querySet: 'default' };

/**
 * The analysis options each kind implies: which query set it resolves, or
 * the fixed list that replaces the set, and which operations it refuses.
 * Every field outside world generation and tag filters resolves the default
 * set; four fields resolve a fixed list instead, and refuse side effects.
 */
export const kindOptions: Record<MolangKind, AnalyzeOptions> = {
  general: defaultSet,
  boolean: defaultSet,
  number: defaultSet,
  color: defaultSet,
  animation: defaultSet,
  render_resource_ref: defaultSet,
  particle: defaultSet,
  event_response: defaultSet,
  string_or_molang: defaultSet,
  block_state: { allowedQueries: ['query.block_state'], restrict: 'no_side_effects_or_random' },
  tag_filter: { querySet: 'tags' },
  worldgen: { querySet: 'world_gen' },
  property_default: { allowedQueries: ['query.had_component_group'], restrict: 'no_side_effects' },
  set_property: { allowedQueries: ['query.has_property', 'query.property'] },
};

export interface MolangRegion {
  /** The Molang source analysed: the decoded value, comments blanked. */
  readonly text: string;
  readonly kind: MolangKind;
  readonly options: AnalyzeOptions;
  /** Document offsets of the region's text: inside the quotes for JSON. */
  readonly hostStart: number;
  readonly hostEnd: number;
  /** Region offset -> document offset. */
  toHost(offset: number): number;
  /** Document offset -> region offset, or undefined when outside the region. */
  fromHost(offset: number): number | undefined;
  /**
   * Parts of the region that are not Molang at all -- comments, templates --
   * where nothing is offered and nothing reported.
   */
  readonly inert: readonly Span[];
  /** Where the region is, for messages: a JSON path, or undefined. */
  readonly label?: string;
}

/** The minimum of a document a provider needs. */
export interface RegionSource {
  readonly uri: string;
  readonly languageId: string;
  getText(): string;
}

export interface MolangRegionProvider {
  readonly id: string;
  /**
   * The Molang regions of doc, in document order, or undefined when this
   * provider has nothing to say about the document -- which lets the next
   * provider try. An empty array means "this is mine, and it has no Molang".
   */
  provideRegions(doc: RegionSource): MolangRegion[] | undefined;
}

export const MOLANG_LANGUAGE_IDS = ['molang', 'bc-minecraft-molang'];

/** A whole .molang document is one region, comments and templates blanked. */
export class MolangFileProvider implements MolangRegionProvider {
  readonly id = 'molang-file';

  provideRegions(doc: RegionSource): MolangRegion[] | undefined {
    if (!MOLANG_LANGUAGE_IDS.includes(doc.languageId)) return undefined;
    const text = doc.getText();
    const stripped = stripMolangFile(text);
    return [
      {
        text: stripped.code,
        kind: 'general',
        // A .molang file could be for any field, so no query set is
        // assumed: every query resolves.
        options: {},
        hostStart: 0,
        hostEnd: text.length,
        toHost: (o) => o,
        fromHost: (o) => (o >= 0 && o <= text.length ? o : undefined),
        // A comment runs to the end of its line, so a cursor there -- just
        // after the last character typed -- is still in it.
        inert: [...stripped.comments.map((c) => ({ start: c.start, end: c.end + 1 })), ...stripped.templates],
      },
    ];
  }
}
