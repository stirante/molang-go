import { describe, expect, it } from 'vitest';
import {
  compilePattern,
  isCommandOrEvent,
  JsonPathProvider,
  looksLikeMolang,
  matchPattern,
  optionsFor,
  type PathCatalogue,
} from '../../src/server/embedding';

describe('path patterns', () => {
  const m = (pattern: string, path: (string | number)[]) => matchPattern(compilePattern(pattern), path);

  it('matches keys, wildcards and indices', () => {
    expect(m('a/*/b', ['a', 'x', 'b'])).toBe(true);
    expect(m('a/*/b', ['a', 0, 'b'])).toBe(false);
    expect(m('a/[*]/b', ['a', 3, 'b'])).toBe(true);
    expect(m('a/[]/b', ['a', 3, 'b'])).toBe(true);
    expect(m('a/[1]', ['a', 1])).toBe(true);
    expect(m('a/[1]', ['a', 2])).toBe(false);
    expect(m('a/b', ['a', 'b', 'c'])).toBe(false);
    expect(m('a~1b', ['a/b'])).toBe(true);
  });

  it('matches ** across any number of levels, none included', () => {
    expect(m('events/**/set_property/*', ['events', 'e', 'sequence', 0, 'set_property', 'p'])).toBe(true);
    expect(m('events/**/set_property/*', ['events', 'set_property', 'p'])).toBe(true);
    expect(m('events/**/set_property/*', ['events', 'set_property'])).toBe(false);
  });
});

describe('what counts as Molang', () => {
  it('tells commands and events from Molang', () => {
    expect(isCommandOrEvent('/say hi')).toBe(true);
    expect(isCommandOrEvent('@s minecraft:start')).toBe(true);
    expect(isCommandOrEvent('v.x = 1;')).toBe(false);
    expect(isCommandOrEvent('v.a / 2')).toBe(false);
  });

  it('tells a plain string from an expression', () => {
    for (const plain of ['unrolled', 'minecraft:pig', 'rolled_up', 'red-ish', '1.5']) expect(looksLikeMolang(plain), plain).toBe(false);
    for (const expr of ["q.property('a:b')", 'v.x', "math.random(0, 1) > 0.5 ? 'a' : 'b'", '(1)']) {
      expect(looksLikeMolang(expr), expr).toBe(true);
    }
  });

  it('narrows string_or_molang to the field it is', () => {
    expect(optionsFor('string_or_molang', 'description/properties/*/default')?.allowedQueries).toEqual(['query.had_component_group']);
    expect(optionsFor('string_or_molang', 'events/**/set_property/*')?.allowedQueries).toEqual(['query.has_property', 'query.property']);
    expect(optionsFor('worldgen', 'x')?.querySet).toBe('world_gen');
    expect(optionsFor('tag_filter', 'x')?.querySet).toBe('tags');
    expect(optionsFor('variable_name', 'x')).toBeUndefined();
  });
});

const doc = (text: string, languageId = 'json') => ({ uri: 'file:///x.json', languageId, getText: () => text });

// Built line by line so every backslash is plainly the file's own: the é
// and \" inside the transition's Molang string are escapes in the JSON, not in
// this test.
export const controller = [
  '{',
  '  "format_version": "1.10.0",',
  '  // comments are fine in pack JSON',
  '  "animation_controllers": {',
  '    "controller.animation.test": {',
  '      "states": {',
  '        "default": {',
  '          "animations": ["idle", { "walk": "q.is_moving" }],',
  '          "transitions": [{ "attack": "v.x == \'caf\\u00e9 \\"x\\"\' && q.is_baby" }, { "b": 1 }],',
  '          "on_entry": ["v.count = 0;", "/say hello", "@s minecraft:reset"],',
  '          "variables": { "speed": { "input": "q.ground_speed" } },',
  '          "blend_transition": 0.2',
  '        }',
  '      }',
  '    }',
  '  }',
  '}',
].join('\n');

describe('JsonPathProvider', () => {
  const provider = new JsonPathProvider();

  it('finds the Molang in an animation controller, and only the Molang', () => {
    const regions = provider.provideRegions(doc(controller))!;
    expect(regions.map((r) => r.text)).toEqual([
      'q.is_moving',
      "v.x == 'café \"x\"' && q.is_baby",
      'v.count = 0;',
      'q.ground_speed',
    ]);
    expect(regions[1].label).toBe('animation_controllers/controller.animation.test/states/default/transitions/[0]/attack');
    expect(regions[1].kind).toBe('boolean');
    expect(regions[1].options.querySet).toBe('default');
  });

  it('maps region offsets back through the escapes', () => {
    const r = provider.provideRegions(doc(controller))![1];
    const value = r.text;
    const amp = value.indexOf('&&');
    expect(controller.slice(r.toHost(amp), r.toHost(amp + 2))).toBe('&&');
    const e = value.indexOf('é');
    expect(controller.slice(r.toHost(e), r.toHost(e + 1))).toBe('\\u00e9');
    expect(controller.slice(r.toHost(0), r.toHost(value.length))).toBe("v.x == 'caf\\u00e9 \\\"x\\\"' && q.is_baby");
    expect(r.fromHost(r.toHost(amp))).toBe(amp);
    expect(r.fromHost(r.hostStart - 1)).toBeUndefined();
  });

  it('leaves documents without a known root key, and non-JSON, to others', () => {
    expect(provider.provideRegions(doc('{"minecraft:item": {}}'))).toBeUndefined();
    expect(provider.provideRegions(doc('{"x": "animation_controllers"}'))).toBeUndefined();
    expect(provider.provideRegions(doc(controller, 'molang'))).toBeUndefined();
  });

  it('survives a document being typed', () => {
    const broken = controller.slice(0, controller.indexOf('"on_entry"'));
    const regions = provider.provideRegions(doc(broken))!;
    expect(regions.map((r) => r.text)).toEqual(['q.is_moving', "v.x == 'café \"x\"' && q.is_baby"]);
  });
});

describe('JsonPathProvider with a catalogue', () => {
  const catalogue: PathCatalogue = {
    fileTypes: [
      {
        rootKey: 'minecraft:client_entity',
        paths: [
          { path: 'description/scripts/pre_animation/[*]', kind: 'general', accepts: ['string'], joined: true },
          { path: 'description/scripts/variables/*', kind: 'variable_name', target: 'key' },
          { path: 'description/scripts/scale', kind: 'number', accepts: ['string', 'number'] },
        ],
      },
      {
        rootKey: 'minecraft:entity',
        paths: [
          { path: 'description/properties/*/default', kind: 'string_or_molang', accepts: ['string', 'number', 'boolean'] },
          { path: 'events/**/set_property/*', kind: 'string_or_molang', accepts: ['string', 'number', 'boolean'] },
          { path: 'components/minecraft:thing/value', kind: 'number', accepts: ['string', 'object'] },
          { path: 'components/minecraft:new/value', kind: 'number', formatVersion: { min: '1.21.0' } },
        ],
      },
    ],
  };
  const provider = new JsonPathProvider(catalogue);

  it('joins the strings of a joined array into one program', () => {
    const text = JSON.stringify(
      {
        format_version: '1.10.0',
        'minecraft:client_entity': {
          description: {
            scripts: {
              variables: { 'variable.x': 'public' },
              pre_animation: ['v.melee ? {', "  v.s = 'é';", '  v.t = 1;', '};'],
              scale: 1,
            },
          },
        },
      },
      null,
      2,
    ).replace(/é/, '\\u00e9');
    const regions = provider.provideRegions(doc(text))!;
    expect(regions).toHaveLength(1);
    const r = regions[0];
    expect(r.text).toBe("v.melee ? {\n  v.s = 'é';\n  v.t = 1;\n};");
    expect(r.label).toBe('minecraft:client_entity/description/scripts/pre_animation');
    // Every offset maps into the right element, through its escapes.
    for (const needle of ['{', 'v.s', 'é', 'v.t', '};']) {
      const at = r.text.indexOf(needle);
      const host = r.toHost(at);
      expect(r.fromHost(host), needle).toBe(at);
    }
    expect(text.slice(r.toHost(r.text.indexOf('é')), r.toHost(r.text.indexOf('é') + 1))).toBe('\\u00e9');
    expect(text.slice(r.toHost(r.text.indexOf('};')), r.toHost(r.text.length))).toBe('};');
    // The line end between two elements is not in the document; it maps to
    // the end of the element before it.
    const nl = r.text.indexOf('\n');
    expect(text[r.toHost(nl)]).toBe('"');
    expect(r.fromHost(text.indexOf('"public"'))).toBeUndefined();
  });

  it('reads only expressions in a string_or_molang field, with the field its queries', () => {
    const text = JSON.stringify({
      'minecraft:entity': {
        description: {
          properties: {
            'a:state': { default: 'unrolled' },
            'a:size': { default: "q.had_component_group('big') ? 2 : 1" },
            'a:flag': { default: true },
          },
        },
        events: { e: { sequence: [{ set_property: { 'a:size': "q.property('a:size') + 1", 'a:state': 'rolled' } }] } },
        components: {
          'minecraft:thing': { value: { expression: 'v.x * 2', version: 1 } },
          'minecraft:new': { value: 'v.y' },
        },
      },
    });
    const regions = provider.provideRegions(doc(text))!;
    expect(regions.map((r) => [r.text, r.options.allowedQueries?.join(',') ?? r.options.querySet])).toEqual([
      ["q.had_component_group('big') ? 2 : 1", 'query.had_component_group'],
      ["q.property('a:size') + 1", 'query.has_property,query.property'],
      ['v.x * 2', 'default'],
      ['v.y', 'default'],
    ]);
  });

  it("honours a path's format_version range", () => {
    const old = JSON.stringify({ format_version: '1.20.0', 'minecraft:entity': { components: { 'minecraft:new': { value: 'v.y' } } } });
    expect(provider.provideRegions(doc(old))).toEqual([]);
  });
});
