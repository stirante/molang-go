// "Molang: Show Molang regions in this file": where the extension reads
// Molang in the active document and what told it to -- the path catalogue or
// a JSON schema -- for when a string is checked that should not be, or one is
// not that should. The regions are outlined in the editor until the document
// changes, and listed; picking one selects it.

import * as vscode from 'vscode';
import type { BaseLanguageClient } from 'vscode-languageclient';
import type { SchemaIndex } from './schemaIndex';

interface RegionInfo {
  start: number;
  end: number;
  kind: string;
  source: 'catalogue' | 'schema' | 'file';
  label?: string;
  version?: string;
}

export function registerRegionsCommand(client: () => BaseLanguageClient | undefined, schemas: SchemaIndex): vscode.Disposable {
  const styles = {
    catalogue: vscode.window.createTextEditorDecorationType({
      border: '1px solid',
      borderColor: new vscode.ThemeColor('editorInfo.foreground'),
      overviewRulerColor: new vscode.ThemeColor('editorInfo.foreground'),
      overviewRulerLane: vscode.OverviewRulerLane.Right,
    }),
    schema: vscode.window.createTextEditorDecorationType({
      border: '1px dashed',
      borderColor: new vscode.ThemeColor('editorWarning.foreground'),
      overviewRulerColor: new vscode.ThemeColor('editorWarning.foreground'),
      overviewRulerLane: vscode.OverviewRulerLane.Right,
    }),
    file: vscode.window.createTextEditorDecorationType({
      border: '1px solid',
      borderColor: new vscode.ThemeColor('editorInfo.foreground'),
    }),
  };
  let decorated: vscode.TextEditor | undefined;
  const clear = () => {
    if (decorated) for (const s of Object.values(styles)) decorated.setDecorations(s, []);
    decorated = undefined;
  };

  const command = vscode.commands.registerCommand('molang.showRegions', async () => {
    const editor = vscode.window.activeTextEditor;
    const c = client();
    if (!editor || !c) return;
    const doc = editor.document;
    const { regions } = await c.sendRequest<{ regions: RegionInfo[] }>('molang/regions', { uri: doc.uri.toString() });
    const applied = schemas.schemasOf(doc.uri.toString());
    clear();
    const describe = (r: RegionInfo) =>
      [r.kind, r.source === 'file' ? '.molang file' : `from the ${r.source}`, r.version && `read at ${r.version}`]
        .filter(Boolean)
        .join(' · ');
    const byStyle = { catalogue: [] as vscode.DecorationOptions[], schema: [] as vscode.DecorationOptions[], file: [] as vscode.DecorationOptions[] };
    for (const r of regions) {
      byStyle[r.source].push({
        range: new vscode.Range(doc.positionAt(r.start), doc.positionAt(r.end)),
        hoverMessage: new vscode.MarkdownString(`**Molang region** ${r.label ? `\`${r.label}\`` : ''}\n\n${describe(r)}`),
      });
    }
    for (const [k, v] of Object.entries(byStyle)) editor.setDecorations(styles[k as keyof typeof styles], v);
    decorated = editor;

    const counts = (['catalogue', 'schema'] as const).map((s) => `${regions.filter((r) => r.source === s).length} ${s}`).join(', ');
    const schemaLine = applied?.schemas.length
      ? applied.schemas.map((s) => `${s.uri} (${s.error ? `not read: ${s.error}` : `${s.paths} Molang paths`})`).join('; ')
      : 'no JSON schema applies';
    type Item = vscode.QuickPickItem & { region?: RegionInfo };
    const items: Item[] = regions.map((r) => ({
      label: r.label ?? `offset ${r.start}`,
      description: describe(r),
      detail: doc.getText(new vscode.Range(doc.positionAt(r.start), doc.positionAt(r.end))).slice(0, 200),
      region: r,
    }));
    if (!items.length) items.push({ label: 'No Molang found in this file.' });
    items.push({ label: 'Schemas', kind: vscode.QuickPickItemKind.Separator }, { label: schemaLine });
    const pick = await vscode.window.showQuickPick(items, {
      title: `Molang regions: ${regions.length} (${counts})`,
      matchOnDescription: true,
      matchOnDetail: true,
    });
    if (pick?.region) {
      const range = new vscode.Range(doc.positionAt(pick.region.start), doc.positionAt(pick.region.end));
      editor.selection = new vscode.Selection(range.start, range.end);
      editor.revealRange(range, vscode.TextEditorRevealType.InCenterIfOutsideViewport);
    }
  });

  return vscode.Disposable.from(
    command,
    // Outlines drawn over the old text would be wrong after an edit.
    vscode.workspace.onDidChangeTextDocument((e) => {
      if (decorated && e.document === decorated.document) clear();
    }),
    ...Object.values(styles),
  );
}
