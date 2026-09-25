# Molang

Molang support for Minecraft Bedrock add-on creators: the mistakes the game
would refuse, shown as you type, with completion and documentation for every
query and math function. It works in `.molang` files and in the Molang inside
your pack's JSON (animation controllers, client entities, render controllers,
particles, entities, features and more), on the desktop and on vscode.dev.

![A query the game does not know, underlined, with the content log's own message](media/diagnostics.png)

## What it does

**Errors in the game's own words.** Every syntax error in an expression, not
only the first; unknown queries and math functions; the wrong number of
arguments; deprecated queries; and what a field does not allow, such as a
query that only works in world generation used in an entity, or an
assignment in a block condition. The messages are the ones the content log
would print, so a search for one finds the same thing you would see in game.

**Completion** of namespaces, queries and math functions with their
signatures and documentation, the variables and temps the file already uses,
and what can follow `->`.

![Completing a query, with its documentation](media/completion.png)

**Hover** for queries and math functions, and for variables: where the file
writes them and how often it reads them. **Signature help** inside calls.

![Hover on a math function](media/hover.png)

**Molang in JSON.** Strings in pack JSON are recognised by where they are,
the way the game reads them: which queries a field can use and which
operations it allows. Escapes such as `\"` are handled, so an error points at
the right character.

![A syntax error inside an animation controller's on_entry](media/json.png)

Also: semantic highlighting, a grammar for `.molang` files, **formatting**
of `.molang` files, a **Molang: Minify** command, and an outline of the
variables a file uses.

### Files it reads

- `.molang` files.
- Resource pack JSON: animation controllers, animations, render
  controllers, client entities, attachables, particles, geometry and entity
  sounds.
- Behavior pack JSON: entities, blocks, items, biomes, feature rules,
  features and processor lists.

JSON files are recognised by their root key (`animation_controllers`,
`minecraft:client_entity`, ...), whatever folder they are in.

## `.molang` files

Molang has no comments, so the convention the community's tools share
applies: `#` starts a comment to the end of the line. `#{ ... }` is a jsonte
template, not a comment; it is left for jsonte.

Formatting and minifying rebuild the file from its syntax, which keeps no
comments, so a file with comments or templates is left as it is.

## With Blockception's extension

[Blockception's Minecraft Bedrock Development extension](https://marketplace.visualstudio.com/items?itemName=BlockceptionLtd.blockceptionvscodeminecraftbedrockdevelopmentextension)
covers far more of an add-on than Molang, and it has Molang support of its
own. The two can be installed together, and this extension is built for
that:

- Diagnostics and hovers from both are shown. Where both report the same
  mistake you will see it twice; Blockception can be told to stop reporting
  its Molang codes in a project's `.mcattributes`
  (`diagnostic.disable.<code>=true`).
- Highlighting cannot be shared, so in JSON this extension leaves it to
  Blockception while Blockception is installed
  (`molang.json.semanticTokens`).
- Completion in JSON is left to Blockception while its own JSON completion is
  on, so the list is not doubled (`molang.json.completion`).
- `.molang` files that Blockception claims get this extension's features too.

What this extension adds alongside it is the checking: an expression is read
by the same rules the game loads it with, and what it reports is what the
game would refuse.

## In the browser

On [vscode.dev](https://vscode.dev) and github.dev everything works as on
the desktop, except `molang.catalogue.path`: a web extension cannot read a
file by path, so the shipped catalogue is always used there.

## Settings

| Setting | |
| --- | --- |
| `molang.catalogue.path` | A `catalogue.json` to use instead of the shipped one. |
| `molang.diagnostics.unknownQueries` | Severity for a query that does not resolve; `default` is an error, as the game refuses the expression. |
| `molang.json.enabled` | Molang features inside JSON files. |
| `molang.json.completion` | `auto`, `on` or `off`. |
| `molang.json.semanticTokens` | `auto`, `on` or `off`. |

## How it works

The analysis is [molang-go](https://github.com/stirante/molang-go), a Molang
implementation in Go, compiled to WebAssembly and run inside the extension:
nothing is sent anywhere, and there is nothing else to install. The query
catalogue ships with the extension and names the game version it describes.

Found an expression the game accepts and this extension refuses, or the
other way round? That is a bug; please
[open an issue](https://github.com/stirante/molang-go/issues) with the
expression and where it was written.

## License

MIT; see [LICENSE](LICENSE). Not affiliated with Mojang or Microsoft.

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

`node scripts/make-icon.mjs` redraws the icon and `node scripts/screenshots.mjs`
retakes the pictures above, from the built extension.
