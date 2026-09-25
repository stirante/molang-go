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
// Diagnostics are the one thing left doubled: the same syntax error twice,
// in two wordings. Blockception can be told, per project, to stop reporting
// individual diagnostic codes, in the project's .mcattributes (see
// mcattributes.ts). That is the way to silence its duplicates without
// touching anyone's settings or its other diagnostics, so it is what the
// "Let Molang handle Molang diagnostics" action writes -- offered once, when
// the two first both report on a JSON file, and never done unasked.

import * as vscode from 'vscode';
import { MCATTRIBUTES, mcattributesAdditions, mcattributesAppend } from './mcattributes';

export { BLOCKCEPTION_MOLANG_CODES, mcattributesAdditions } from './mcattributes';

export const BLOCKCEPTION_ID = 'blockceptionltd.blockceptionvscodeminecraftbedrockdevelopmentextension';

export const SILENCE_COMMAND = 'molang.blockception.silenceDuplicates';
const SILENCE_TITLE = 'Let Molang handle Molang diagnostics';
const DONT_ASK = 'molang.blockception.dontAsk';

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

/** Whether both extensions report Molang diagnostics in pack JSON. */
function bothReportJson(): boolean {
  if (!vscode.extensions.getExtension(BLOCKCEPTION_ID)) return false;
  const bc = vscode.workspace.getConfiguration('BC-MC');
  if (bc.get('Diagnostics.Enable') === false || bc.get('Diagnostics.Json') === false) return false;
  return vscode.workspace.getConfiguration('molang').get('json.enabled') !== false;
}

/** The .mcattributes of folder and its current text; undefined when there is none. */
async function readAttributes(folder: vscode.WorkspaceFolder): Promise<{ uri: vscode.Uri; text?: string }> {
  const uri = vscode.Uri.joinPath(folder.uri, MCATTRIBUTES);
  // An open editor's text, unsaved edits included, is what an edit applies to.
  const open = vscode.workspace.textDocuments.find((d) => d.uri.toString() === uri.toString());
  if (open) return { uri, text: open.getText() };
  try {
    return { uri, text: new TextDecoder().decode(await vscode.workspace.fs.readFile(uri)) };
  } catch {
    return { uri };
  }
}

/**
 * Adds the lines that silence Blockception's duplicate Molang diagnostics
 * to folder's .mcattributes, creating it if need be, as one WorkspaceEdit --
 * so it can be undone like any other edit -- and saves the file, which is
 * when Blockception reads it. Returns how many lines were added.
 */
export async function silenceDuplicates(folder: vscode.WorkspaceFolder): Promise<number> {
  const { uri, text } = await readAttributes(folder);
  const lines = mcattributesAdditions(text ?? '');
  if (!lines.length) return 0;
  const edit = new vscode.WorkspaceEdit();
  if (text === undefined) {
    edit.createFile(uri, { ignoreIfExists: true });
    edit.insert(uri, new vscode.Position(0, 0), mcattributesAppend('', lines));
  } else {
    const doc = await vscode.workspace.openTextDocument(uri);
    edit.insert(uri, doc.positionAt(doc.getText().length), mcattributesAppend(doc.getText(), lines));
  }
  if (!(await vscode.workspace.applyEdit(edit))) throw new Error(`the edit to ${uri.fsPath} was not applied`);
  await (await vscode.workspace.openTextDocument(uri)).save();
  return lines.length;
}

async function pickFolder(arg?: vscode.Uri): Promise<vscode.WorkspaceFolder | undefined> {
  const from = arg ?? vscode.window.activeTextEditor?.document.uri;
  const folder = from && vscode.workspace.getWorkspaceFolder(from);
  if (folder) return folder;
  const all = vscode.workspace.workspaceFolders ?? [];
  if (all.length <= 1) return all[0];
  return vscode.window.showWorkspaceFolderPick({ placeHolder: 'The project whose .mcattributes to change' });
}

/**
 * The command, the offer and the quick fix. Returns the function the
 * client calls with each JSON document it receives diagnostics for.
 */
export function registerBlockceptionCoexistence(context: vscode.ExtensionContext): (uri: vscode.Uri, count: number) => void {
  const run = async (arg?: vscode.Uri) => {
    const folder = await pickFolder(arg);
    if (!folder) {
      vscode.window.showInformationMessage('Molang: open the project folder first; .mcattributes belongs at its root.');
      return 0;
    }
    try {
      const added = await silenceDuplicates(folder);
      if (!arg) {
        vscode.window.showInformationMessage(
          added
            ? `Molang: Blockception's duplicate Molang diagnostics are off in ${folder.name} (${added} lines in .mcattributes).`
            : `Molang: ${folder.name}'s .mcattributes already has them.`,
        );
      }
      return added;
    } catch (e) {
      vscode.window.showWarningMessage(`Molang: cannot change .mcattributes: ${(e as Error).message ?? e}`);
      return 0;
    }
  };

  const offered = new Set<string>();
  const maybeOffer = async (uri: vscode.Uri, count: number) => {
    if (!count || !bothReportJson() || context.workspaceState.get(DONT_ASK)) return;
    const folder = vscode.workspace.getWorkspaceFolder(uri);
    if (!folder || offered.has(folder.uri.toString())) return;
    offered.add(folder.uri.toString());
    const { text } = await readAttributes(folder);
    if (!mcattributesAdditions(text ?? '').length) return;
    const never = "Don't ask again";
    const choice = await vscode.window.showInformationMessage(
      'Blockception also reports Molang errors in this file, so each one shows twice. ' +
        `Turn off Blockception's Molang diagnostics for ${folder.name}? This adds lines to its .mcattributes.`,
      SILENCE_TITLE,
      never,
    );
    if (choice === SILENCE_TITLE) await run(folder.uri);
    else if (choice === never) await context.workspaceState.update(DONT_ASK, true);
  };

  context.subscriptions.push(
    vscode.commands.registerCommand(SILENCE_COMMAND, run),
    vscode.languages.registerCodeActionsProvider(
      [{ language: 'json' }, { language: 'jsonc' }],
      {
        async provideCodeActions(doc, _range, ctx) {
          const ours = ctx.diagnostics.filter((d) => d.source === 'molang');
          if (!ours.length || !bothReportJson()) return [];
          const folder = vscode.workspace.getWorkspaceFolder(doc.uri);
          if (!folder) return [];
          const { text } = await readAttributes(folder);
          if (!mcattributesAdditions(text ?? '').length) return [];
          const action = new vscode.CodeAction(`${SILENCE_TITLE} (turn off Blockception's duplicates)`, vscode.CodeActionKind.QuickFix);
          action.diagnostics = ours;
          action.command = { title: SILENCE_TITLE, command: SILENCE_COMMAND, arguments: [folder.uri] };
          return [action];
        },
      },
      { providedCodeActionKinds: [vscode.CodeActionKind.QuickFix] },
    ),
  );
  return (uri, count) => void maybeOffer(uri, count);
}
