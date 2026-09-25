// The catalogue as the extension ships it, composed as the server composes it.

import { readFileSync } from 'node:fs';
import * as path from 'node:path';
import { composeCatalogue } from '../../src/server/catalogue';

const dir = path.resolve(__dirname, '..', '..', 'catalogue');
const read = (name: string) => readFileSync(path.join(dir, name), 'utf8');

/** The composed catalogue document, or the one MOLANG_CATALOGUE names. */
export function shippedCatalogue(): string {
  if (process.env.MOLANG_CATALOGUE) return readFileSync(process.env.MOLANG_CATALOGUE, 'utf8');
  return composeCatalogue(read('catalogue.json'), read('math.json'), read('namespaces.json'));
}
