# Changelog

## 0.1.0

First release.

- Diagnostics for Molang in `.molang` files and in pack JSON, in the words
  the game's content log uses: syntax errors, unknown queries and math
  functions, argument counts, deprecated queries, and operations a field
  does not allow.
- Completion, hover, signature help and semantic highlighting.
- Formatting of `.molang` files, whole or a selection, keeping comments,
  blank lines and jsonte templates; Format and Minify Molang in this string
  code actions in pack JSON; and a Molang: Minify command.
- Molang found through the JSON schemas applied to a file, where the path
  catalogue has no entry, and a Molang: Show Molang regions in this file
  command that shows what found each string.
- Queries checked against the versions they exist in, at a pack file's
  `format_version`.
- Works alongside Blockception's extension, and on vscode.dev and
  github.dev.
