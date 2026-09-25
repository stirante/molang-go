// Which JSON schemas VS Code applies to a document, decided the way VS Code's
// own JSON language features decide it, so the Molang this extension finds
// by schema is the Molang of the schema the user sees validating the file.
//
// VS Code exposes no API for "the schema of this document", so its rules are
// repeated here:
//
//   - A document with a "$schema" property gets that schema and no other.
//   - Otherwise every association whose file patterns match applies: each
//     installed extension's contributes.jsonValidation, and the json.schemas
//     setting at every level.
//   - A pattern is a glob tested against the whole document URI, with "**/"
//     put in front of it; "!" makes it an exclusion; patterns are tried in
//     order and the last one that matches decides.
//   - An extension's pattern that starts with neither a scheme, "/" nor "!"
//     gets a "/" first (then dropped again with the leading "/" rule), and
//     an extension's "./" URL is relative to the extension.
//   - A setting's association made in a workspace folder applies only to
//     documents in that folder, and its relative URL is relative to it.
//
// Nothing here touches the editor, so it is tested alone; schemaIndex.ts
// gathers the inputs.

export interface Association {
  fileMatch: string[];
  /** The schema's URI, or undefined for a schema given inline. */
  uri?: string;
  /** An inline schema, from the json.schemas setting. */
  schema?: unknown;
  /** Only documents under this folder URI match, when set. */
  folderUri?: string;
}

/**
 * A glob as VS Code's JSON language service compiles it: extended syntax
 * ({a,b}, [abc], ?) and globstar ("**" spans directories, "*" does not).
 */
export function globToRegExp(glob: string): RegExp {
  let re = '';
  let inGroup = false;
  for (let i = 0; i < glob.length; i++) {
    const c = glob[i];
    switch (c) {
      case '/':
      case '$':
      case '^':
      case '+':
      case '.':
      case '(':
      case ')':
      case '=':
      case '!':
      case '|':
        re += '\\' + c;
        break;
      case '?':
        re += '.';
        break;
      case '[':
      case ']':
        re += c;
        break;
      case '{':
        inGroup = true;
        re += '(';
        break;
      case '}':
        inGroup = false;
        re += ')';
        break;
      case ',':
        re += inGroup ? '|' : '\\,';
        break;
      case '*': {
        const prev = glob[i - 1];
        let stars = 1;
        while (glob[i + 1] === '*') {
          stars++;
          i++;
        }
        const next = glob[i + 1];
        const globstar =
          stars > 1 &&
          (prev === '/' || prev === undefined || prev === '{' || prev === ',') &&
          (next === '/' || next === undefined || next === ',' || next === '}');
        if (globstar) {
          if (next === '/') i++;
          else if (prev === '/' && re.endsWith('\\/')) re = re.slice(0, -2);
          re += '((?:[^/]*(?:\\/|$))*)';
        } else {
          re += '([^/]*)';
        }
        break;
      }
      default:
        re += c;
    }
  }
  return new RegExp('^' + re + '$');
}

interface CompiledAssociation {
  globs: { re: RegExp; include: boolean }[];
  folderUri?: string;
}

function compile(a: Association): CompiledAssociation {
  const globs: CompiledAssociation['globs'] = [];
  for (let p of a.fileMatch) {
    const include = !p.startsWith('!');
    if (!include) p = p.slice(1);
    if (!p) continue;
    if (p.startsWith('/')) p = p.slice(1);
    globs.push({ re: globToRegExp('**/' + p), include });
  }
  let folderUri = a.folderUri;
  if (folderUri && !folderUri.endsWith('/')) folderUri += '/';
  return { globs, folderUri };
}

/**
 * Whether an association applies to a document. documentUri is the URI as
 * VS Code prints it for matching: unencoded (Uri.toString(true)), with no
 * query or fragment.
 */
export function associationMatches(a: Association, documentUri: string): boolean {
  const c = compile(a);
  if (c.folderUri && !documentUri.startsWith(c.folderUri)) return false;
  let match = false;
  for (const g of c.globs) if (g.re.test(documentUri)) match = g.include;
  return match;
}

/**
 * An extension's jsonValidation contributions as associations: patterns
 * normalised and "./" URLs made absolute against the extension's URI.
 */
export function extensionAssociations(contributions: unknown, extensionUri: string): Association[] {
  if (!Array.isArray(contributions)) return [];
  const out: Association[] = [];
  for (const jv of contributions) {
    if (!jv || typeof jv !== 'object') continue;
    let { fileMatch, url } = jv as { fileMatch?: unknown; url?: unknown };
    if (typeof fileMatch === 'string') fileMatch = [fileMatch];
    if (!Array.isArray(fileMatch) || typeof url !== 'string') continue;
    const uri = url.startsWith('./') ? joinUri(extensionUri, url.slice(2)) : url;
    const patterns = fileMatch
      .filter((f): f is string => typeof f === 'string')
      .map((f) => {
        if (f.startsWith('%')) {
          return f
            .replace(/%APP_SETTINGS_HOME%/, '/User')
            .replace(/%MACHINE_SETTINGS_HOME%/, '/Machine')
            .replace(/%APP_WORKSPACES_HOME%/, '/Workspaces');
        }
        return /^(\w+:\/\/|\/|!)/.test(f) ? f : '/' + f;
      });
    out.push({ fileMatch: patterns, uri });
  }
  return out;
}

/** One json.schemas setting entry. */
export interface SettingSchema {
  fileMatch?: unknown;
  url?: unknown;
  schema?: unknown;
}

/**
 * The json.schemas setting at one level as associations. base resolves a
 * relative URL (the workspace folder, or the workspace), and folderUri
 * limits a folder's entries to that folder.
 */
export function settingAssociations(entries: unknown, base?: string, folderUri?: string): Association[] {
  if (!Array.isArray(entries)) return [];
  const out: Association[] = [];
  for (const e of entries as SettingSchema[]) {
    if (!e || typeof e !== 'object' || !Array.isArray(e.fileMatch)) continue;
    const fileMatch = e.fileMatch.filter((f): f is string => typeof f === 'string');
    if (typeof e.url === 'string') {
      let uri: string | undefined = e.url;
      if (uri.startsWith('.') || uri.startsWith('/')) {
        // Relative to where the setting was made, "/" included; without a
        // place to resolve against, the association cannot be followed.
        uri = base ? joinUri(base, uri.replace(/^\.?\//, '')) : undefined;
      }
      if (uri) out.push({ fileMatch, uri, folderUri });
    } else if (e.schema && typeof e.schema === 'object') {
      out.push({ fileMatch, schema: e.schema, folderUri });
    }
  }
  return out;
}

/** base (a folder URI) joined with a relative path. */
export function joinUri(base: string, rel: string): string {
  const b = base.endsWith('/') ? base : base + '/';
  try {
    return new URL(rel, b).href;
  } catch {
    return b + rel;
  }
}

/** A "$schema" value resolved against the document's URI. */
export function resolveUriRelative(ref: string, documentUri: string): string | undefined {
  try {
    return new URL(ref, documentUri).href;
  } catch {
    return undefined;
  }
}

/** The document's own "$schema", read from its text without a full parse. */
export function documentSchema(text: string): string | undefined {
  // At the top level in practice; a "$schema" deeper in is rare enough that
  // the cheap read is worth it.
  const m = /"\$schema"\s*:\s*"((?:[^"\\]|\\.)*)"/.exec(text);
  return m ? m[1].replace(/\\\//g, '/') : undefined;
}
