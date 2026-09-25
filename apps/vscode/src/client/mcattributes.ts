// Blockception's per-project switches, in the project's .mcattributes: one
// `key=value` per line, `#` to the end of a line a comment. A diagnostic code
// is silenced by `diagnostic.disable.<code>=true`, looked up by exact key --
// no prefixes, no wildcards -- so every code has to be listed on its own.
//
// Nothing here touches VS Code, so it can be tested on its own.

/**
 * Blockception's Molang diagnostic codes that this extension's diagnostics
 * duplicate: syntax errors, unknown and misused functions, argument counts,
 * deprecation, namespaces it does not know.
 *
 * Its syntax errors are coded `molang.<something>`, where the something is
 * either a fixed name (error.string.unterminated) or the token the error is
 * at: an operator missing an operand is `molang.+`, `molang.??`. The
 * operators are listed; a code holding `=` or `#` cannot be written as a key
 * at all (the line is split at its first `=`, and `#` starts a comment), so
 * `molang.=` and `molang.==` stay. An unknown function is
 * `molang.function.<scope>.<name>`, a code per name, which no fixed list
 * covers.
 *
 * Its naming lints (molang.variable.naming), its optimisation hints
 * (molang.optimization.*) and its project-wide checks for variables no file
 * defines (molang.variable.undefined and the like) are left on: this
 * extension says nothing of the kind, so they duplicate nothing.
 */
export const BLOCKCEPTION_MOLANG_CODES: readonly string[] = [
  'molang.diagnoser.syntax',
  'molang.error.unknown',
  'molang.error.string.unterminated',
  'molang.error.character.unexpected',
  'molang.??',
  'molang.?',
  'molang.!',
  'molang.+',
  'molang.-',
  'molang.*',
  'molang./',
  'molang.&&',
  'molang.||',
  'molang.<',
  'molang.>',
  'molang.function.arguments',
  'molang.function.arguments.type',
  'molang.function.deprecated',
  'molang.function.scope',
  'molang.function.wrong_pack_type',
  'molang.identifier.invalid',
  'molang.identifier.scope',
];

export const MCATTRIBUTES = '.mcattributes';

/**
 * The keys an .mcattributes file sets, read the way Blockception reads it:
 * split at the first `=`, key and value kept as they are (so `a = b` sets
 * "a " to " b"), except that a line with a comment is trimmed once the
 * comment is cut.
 */
export function parseMcattributes(text: string): Map<string, string> {
  const out = new Map<string, string>();
  for (let line of text.split(/\r?\n/)) {
    const hash = line.indexOf('#');
    if (hash >= 0) line = line.slice(0, hash).trim();
    const eq = line.indexOf('=');
    if (eq > 0) out.set(line.slice(0, eq), line.slice(eq + 1));
  }
  return out;
}

/**
 * The lines to append to an .mcattributes file so Blockception stops
 * reporting codes, leaving out any it already has. A code the file already
 * sets, to anything, is left alone: `=false` is someone's decision to keep
 * it. Running it again adds nothing.
 */
export function mcattributesAdditions(existing: string, codes: readonly string[] = BLOCKCEPTION_MOLANG_CODES): string[] {
  const set = parseMcattributes(existing);
  return codes.filter((c) => !set.has(`diagnostic.disable.${c}`)).map((c) => `diagnostic.disable.${c}=true`);
}

/**
 * The text to insert at the end of existing to add lines: in the file's own
 * line ending, starting on a fresh line, ending with a line end.
 */
export function mcattributesAppend(existing: string, lines: readonly string[]): string {
  if (!lines.length) return '';
  const eol = existing.includes('\r\n') ? '\r\n' : '\n';
  const lead = existing.length && !existing.endsWith('\n') ? eol : '';
  return lead + lines.join(eol) + eol;
}
