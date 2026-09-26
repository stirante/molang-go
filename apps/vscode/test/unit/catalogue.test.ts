// The shipped catalogue against the rules its build enforces
// (tools/catalogue/rules.mjs): public text only, every query once, a hover of
// at most four lines, a docs link that follows the site's route contract. The
// build refuses to write a catalogue that breaks one; this catches a hand edit
// of the committed files that does.

import { readFileSync } from 'node:fs';
import * as path from 'node:path';
import { describe, expect, it } from 'vitest';
import { Catalogue, functionDocs } from '../../src/server/catalogue';
import { docsUrl, provenanceProblems, queryListProblems, textRules } from '../../../../tools/catalogue/rules.mjs';
import { shippedCatalogue } from './shipped';

const repo = path.resolve(__dirname, '..', '..', '..', '..');
const readJson = (...p: string[]) => JSON.parse(readFileSync(path.join(repo, ...p), 'utf8'));

describe('the shipped catalogue', () => {
  const catalogue = readJson('apps', 'vscode', 'catalogue', 'catalogue.json');
  const docs = readJson('docs', 'site', 'data', 'queries.json');

  it('passes the provenance and shape rules', () => {
    expect(queryListProblems(catalogue.queries, { label: 'catalogue.json' })).toEqual([]);
    expect(provenanceProblems(readJson('apps', 'vscode', 'catalogue', 'math.json'), 'math.json')).toEqual([]);
    expect(provenanceProblems(readJson('apps', 'vscode', 'catalogue', 'namespaces.json'), 'namespaces.json')).toEqual([]);
  });

  it('matches the docs site data query for query', () => {
    const names = catalogue.queries.map((q: { name: string }) => q.name);
    expect(queryListProblems(docs.queries, { names, label: 'queries.json' })).toEqual([]);
  });

  it('catches what the rules are for', () => {
    const planted = [{ ...catalogue.queries[0], verification: { status: 'verified', game: '1.26.60', label: 'x', inGame: 'measured' } }];
    expect(queryListProblems(planted).join('\n')).toMatch(/inGame/);
    const long = [{ ...catalogue.queries[0], hover: 'a\nb\nc\nd\ne' }];
    expect(queryListProblems(long).join('\n')).toMatch(/at most 4/);
  });

  // The public-text rules live outside the repository (see rules.mjs), so this
  // runs only where that file is present; elsewhere it is reported as skipped
  // rather than passing without having checked anything.
  it.skipIf(!textRules())('catches planted text the public-text rules are for', () => {
    const rules = textRules()!;
    for (const sample of rules.mustCatch) expect(provenanceProblems({ note: sample })).not.toEqual([]);
    for (const key of rules.forbiddenKeys) expect(provenanceProblems({ [key]: 'x' })).not.toEqual([]);
  });

  it('shows the concise hover and a link to the query page', () => {
    const c = Catalogue.parse(shippedCatalogue());
    const f = c.lookup('query', 'is_item_name_any')!;
    const md = functionDocs('query', f, c);
    expect(md).toContain(f.hover!.split('\n')[0]);
    expect(md).toContain(`[Documentation →](${docsUrl('is_item_name_any')})`);
    expect(md).not.toContain(f.description!);
  });
});
