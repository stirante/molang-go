// Living alongside Blockception's Bedrock extension, which has Molang
// support of its own in .molang files and in every string of pack JSON it
// thinks looks like Molang.
//
// Diagnostics, completion and hover from two extensions merge, so both
// extensions' results show. Semantic tokens do not merge -- one provider's
// tokens win outright, decided by activation order -- so for JSON the
// server stands aside while Blockception is active (molang.json.semanticTokens
// "auto"), and completion in JSON is left to Blockception while its own JSON
// completion is on (molang.json.completion "auto").
//
// Blockception can be told, per project, to stop reporting individual
// diagnostic codes: `diagnostic.disable.<code>=true` lines in the project's
// .mcattributes. That is the way to silence its duplicate Molang diagnostics
// without touching anyone's settings; the codes and the lines are here, for
// the one-click action that will offer it.

import * as vscode from 'vscode';

export const BLOCKCEPTION_ID = 'blockceptionltd.blockceptionvscodeminecraftbedrockdevelopmentextension';

export interface BlockceptionState {
  blockceptionActive: boolean;
  blockceptionJsonCompletion: boolean;
}

export function blockceptionState(): BlockceptionState {
  const ext = vscode.extensions.getExtension(BLOCKCEPTION_ID);
  // Installed and enabled counts, not only activated: it activates on the
  // same files this extension does, and whichever activates first must not
  // decide the outcome.
  const active = !!ext;
  const completion = vscode.workspace.getConfiguration('BC-MC').get<boolean>('Completion.JSON', true);
  return { blockceptionActive: active, blockceptionJsonCompletion: active && completion !== false };
}

/**
 * Blockception's Molang diagnostic codes that this extension's diagnostics
 * duplicate.
 */
export const BLOCKCEPTION_MOLANG_CODES = [
  'molang.diagnoser.syntax',
  'molang.function.arguments',
  'molang.function.arguments.type',
  'molang.function.deprecated',
  'molang.function.scope',
  'molang.function.wrong_pack_type',
  'molang.identifier.invalid',
  'molang.identifier.scope',
  'molang.error.unknown',
];

/**
 * The lines to append to an .mcattributes file so Blockception stops
 * reporting codes, leaving out any it already has. The file's own content
 * is taken as it is; nothing else in it changes.
 */
export function mcattributesAdditions(existing: string, codes: readonly string[] = BLOCKCEPTION_MOLANG_CODES): string[] {
  const present = new Set(
    existing
      .split(/\r?\n/)
      .map((l) => l.trim())
      .filter((l) => l.startsWith('diagnostic.disable.'))
      .map((l) => l.slice('diagnostic.disable.'.length).split('=')[0].trim()),
  );
  return codes.filter((c) => !present.has(c)).map((c) => `diagnostic.disable.${c}=true`);
}
