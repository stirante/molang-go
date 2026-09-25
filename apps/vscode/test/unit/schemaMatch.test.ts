import { describe, expect, it } from 'vitest';
import {
  associationMatches,
  documentSchema,
  extensionAssociations,
  globToRegExp,
  resolveUriRelative,
  settingAssociations,
} from '../../src/client/schemaMatch';

describe('globs as VS Code compiles them', () => {
  it('keeps * within a segment and lets ** span them', () => {
    expect(globToRegExp('**/a/*.json').test('file:///x/y/a/b.json')).toBe(true);
    expect(globToRegExp('**/a/*.json').test('file:///x/a/b/c.json')).toBe(false);
    expect(globToRegExp('**/a/**/*.json').test('file:///x/a/b/c/d.json')).toBe(true);
    expect(globToRegExp('**/a/**/*.json').test('file:///x/a/d.json')).toBe(true);
  });

  it('reads braces, classes and ?', () => {
    const re = globToRegExp('**/*.{json,jsonc}');
    expect(re.test('file:///p/x.jsonc')).toBe(true);
    expect(re.test('file:///p/x.json5')).toBe(false);
    expect(globToRegExp('**/v[12].json').test('file:///v2.json')).toBe(true);
    expect(globToRegExp('**/a?.json').test('file:///ab.json')).toBe(true);
  });
});

describe('associations', () => {
  // Two of Blockception's own entity patterns, and its exclusion.
  const [entities] = extensionAssociations(
    [
      {
        fileMatch: ['*BP*/entities/*.{json,jsonc,json5}', 'behavior_packs/*/entities/**/*.{json,jsonc,json5}', '!*loot_tables*'],
        url: './minecraft-bedrock-schemas/behavior/entities/entities.json',
      },
    ],
    'file:///c%3A/ext/blockception',
  );

  it("resolves an extension's ./ URL against the extension", () => {
    expect(entities.uri).toBe('file:///c%3A/ext/blockception/minecraft-bedrock-schemas/behavior/entities/entities.json');
    expect(entities.fileMatch[0]).toBe('/*BP*/entities/*.{json,jsonc,json5}');
    expect(entities.fileMatch[2]).toBe('!*loot_tables*');
  });

  it('matches by folder convention, wherever the pack sits', () => {
    expect(associationMatches(entities, 'file:///h:/work/packs/MyBP/entities/pig.json')).toBe(true);
    expect(associationMatches(entities, 'file:///h:/work/behavior_packs/p/entities/sub/pig.json')).toBe(true);
    expect(associationMatches(entities, 'file:///h:/work/packs/MyRP/entities/pig.json')).toBe(false);
  });

  it('lets the last matching pattern decide, exclusions included', () => {
    expect(associationMatches(entities, 'file:///h:/w/MyBP/entities/loot_tables.json')).toBe(false);
    const a = { fileMatch: ['!/x/*.json', '/x/keep.json'] };
    expect(associationMatches(a, 'file:///p/x/keep.json')).toBe(true);
    expect(associationMatches(a, 'file:///p/x/other.json')).toBe(false);
  });

  it("limits a folder's setting to that folder, and resolves its relative URL there", () => {
    const [a] = settingAssociations([{ fileMatch: ['*.entity.json'], url: './schemas/entity.json' }], 'file:///h%3A/proj', 'file:///h:/proj');
    expect(a.uri).toBe('file:///h%3A/proj/schemas/entity.json');
    expect(associationMatches(a, 'file:///h:/proj/pig.entity.json')).toBe(true);
    expect(associationMatches(a, 'file:///h:/other/pig.entity.json')).toBe(false);
    const [inline] = settingAssociations([{ fileMatch: ['x.json'], schema: { format: 'molang' } }]);
    expect(inline.schema).toEqual({ format: 'molang' });
    // A relative URL with nowhere to resolve it is dropped.
    expect(settingAssociations([{ fileMatch: ['x.json'], url: './s.json' }])).toEqual([]);
  });

  it("reads a document's own $schema and resolves it against the document", () => {
    expect(documentSchema('{ "$schema": "../schemas/e.json", "format_version": "1.21.0" }')).toBe('../schemas/e.json');
    expect(documentSchema('{ "format_version": "1.21.0" }')).toBeUndefined();
    expect(resolveUriRelative('../schemas/e.json', 'file:///h%3A/p/BP/pig.json')).toBe('file:///h%3A/p/schemas/e.json');
    expect(resolveUriRelative('https://example.com/e.json', 'file:///x.json')).toBe('https://example.com/e.json');
  });
});
