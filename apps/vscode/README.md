# Molang

Language support for [Molang](https://bedrock.dev/docs/stable/Molang), the
expression language of Minecraft Bedrock add-ons, in `.molang` files and in the
Molang inside pack JSON.

The analysis is [molang-go](https://github.com/stirante/molang-go) compiled to
WebAssembly: the same parser and load-time rules that library is tested
against, so a diagnostic here means the game would refuse the expression, in
the words its content log would use.

## Features

- **Diagnostics**: every syntax error in a document, not just the first;
  unknown math functions and wrong math argument counts; queries that do not
  resolve where they are written (outside the field's query set, or its fixed
  list of queries); missing query arguments; deprecated queries; operations a
  field refuses, such as assignment in a block condition.
- **Completion** of namespaces, query and math functions with their
  signatures, the variables and temps the file already uses, and what can
  follow `->`.
- **Hover** for queries and math functions, and for variables: where the file
  writes them and how often it reads them.
- **Signature help** inside query and math calls.
- **Semantic highlighting**, and a TextMate grammar for `.molang` files.
- **Formatting** of `.molang` files, and a **Molang: Minify** command.
- **Molang: Show Molang regions in this file**: where the extension reads
  Molang in the open file, outlined and listed, each with what found it.
- **Outline** of the variables a `.molang` file uses.

## `.molang` files

Molang has no comments, so the convention the ecosystem's tools share applies:
`#` starts a comment to the end of the line. `#{ ... }` is a jsonte template,
not a comment; it is left for jsonte and read as a value. Files the
Blockception extension claims (language `bc-minecraft-molang`) get the same
features.

Formatting and minifying print the file from its syntax tree, which keeps no
comments, so a file with comments or templates is left as it is.

## Molang in JSON

Molang strings in pack JSON are found by path under the document's root key
(`animation_controllers`, `minecraft:client_entity`, ...), whatever folder the
file is in, and read as the game reads that field: which queries it can name
and which operations it allows. JSON escapes are decoded first and every
position mapped back, so `\"` and `é` inside an expression do not throw
anything off.

### Molang the schemas mark

The JSON schemas VS Code applies to a file are read too: those other
extensions contribute (Blockception's, for one), those in the `json.schemas`
setting, and the file's own `$schema`, matched to the file by VS Code's own
rules. Wherever a schema marks a string as Molang -- `"format": "molang"`, a
title starting "Molang", a reference to Mojang's `Expression Node.json` or
`Molang string.json`, the `{"expression", "version"}` object form -- and the
shipped path catalogue has no entry for it, the string is read as Molang as
well. The catalogue always wins: a field it knows is read as the kind it
gives, and a field it has removed or declared not Molang stays that way. A
schema cannot say which kind of Molang a field holds, so these are read as
general Molang (world generation Molang in feature, feature rule and biome
files).

This never delays diagnostics: a file is checked against the catalogue as
it opens, and again if its schemas add anything. Schemas are read once and
kept; a remote one is fetched with a short timeout, not at all when
`json.schemaDownload.enable` is off. `molang.json.schemaDetection` turns the
whole thing off, and **Molang: Show Molang regions in this file** shows
which strings came from the catalogue and which from a schema.

### Versions

Some queries exist only in files read at certain versions: added in one,
removed in a later one. A pack JSON file's Molang is taken to be read at the
file's `format_version`, and a query outside its versions is reported as
not resolving. This is a best-effort rule, applied to behaviour-pack files
(entities, blocks, items, features, feature rules, biomes) and resource-pack
client files (client entities, attachables, animations, animation
controllers, render controllers, particles) alike. Geometry is the exception:
its `format_version` is the geometry format's own. A file without a
`format_version`, and every `.molang` file, is read at no version, so no
query is refused for its version. `molang.versionSource` set to `ignore`
turns the version checks off.

## Alongside Blockception

Blockception's extension has Molang support of its own. Diagnostics, completion
and hover from both extensions are shown together. Semantic highlighting cannot
be shared, so in JSON this extension leaves it to Blockception while that is
installed, and it leaves completion inside JSON to Blockception while
Blockception's own JSON completion is on. Both are settings:
`molang.json.semanticTokens` and `molang.json.completion`.

## Settings

| Setting | |
| --- | --- |
| `molang.catalogue.path` | A `catalogue.json` to use instead of the shipped one. |
| `molang.diagnostics.unknownQueries` | Severity for a query that does not resolve. |
| `molang.versionSource` | `format_version` (the default) or `ignore`. |
| `molang.json.enabled` | Molang features inside JSON files. |
| `molang.json.schemaDetection` | Also read the Molang JSON schemas mark. |
| `molang.json.completion` | `auto`, `on` or `off`. |
| `molang.json.semanticTokens` | `auto`, `on` or `off`. |

## Building

From `apps/vscode`, with Go and Node installed:

```
npm install
npm run build        # the WebAssembly module and the bundles
npm test             # unit tests, over the real module
npm run test:integration   # in a downloaded VS Code; opens a window
npm run package      # molang.vsix
```
