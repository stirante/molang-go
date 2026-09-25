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
| `molang.json.enabled` | Molang features inside JSON files. |
| `molang.json.completion` | `auto`, `on` or `off`. |
| `molang.json.semanticTokens` | `auto`, `on` or `off`. |

## Building

From `apps/vscode`, with Go and Node installed:

```
npm install
npm run build        # the WebAssembly module and the bundles
npm test             # unit tests, over the real module
npm run test:integration   # in a downloaded VS Code; opens a window
npm run test:web     # the web build in VS Code for the Web, headless Chromium
npm run package      # molang.vsix
```
