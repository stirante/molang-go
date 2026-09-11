# molang-go

A standalone, general-purpose [Molang](https://bedrock.dev/docs/stable/Molang)
implementation in Go: lexer, parser, AST, a closure-compiled evaluator, a
`Format`/`Minify` printer, and AST transforms (constant folding, macro
expansion).

It is deliberately not tied to any one consumer. The AST
(`github.com/stirante/molang-go/ast`) is the shared currency — the evaluator, printer, and
transforms all walk the same tree, so a single parse can be evaluated,
reformatted, minified, or rewritten without re-parsing. It grew out of, and
is validated against, a Minecraft Bedrock worldgen tool, but nothing in the
core packages knows that.

## Package layout

```
github.com/stirante/molang-go/
  token/      lexical token kinds
  lexer/      Molang source -> token stream
  ast/        the shared AST (Program, Stmt, Expr node types)
  parser/     tokens -> *ast.Program (recursive-descent / precedence climbing)
  eval/       *ast.Program -> compiled closure tree, RNG/Scope/Context, math.* table
  printer/    Format (readable) and Minify (shortest valid output)
  transform/  constant folding; macro registration + expansion (bitshift worked example)
  worldgen/   optional extension: query.noise/has_biome_tag/heightmap/above_top_solid
  mtrand/     MT19937 port (the engine's core RNG) — used by worldgen.Noise and as a
              reproducible eval.RNG for testing
  molang.go   thin convenience API tying Parse+Compile+Run together
  cmd/molang/ the command-line front end (see Usage)
```

`token`/`lexer`/`ast`/`parser`/`eval`/`printer`/`transform` are the core
library. `worldgen` and `mtrand` are optional, separate extensions — the
core ships no worldgen-specific query functions, by design (see
"What's implemented" below).

## What's implemented

**The whole language.** Arithmetic and comparison, both conditional forms,
`loop` with `break`/`continue`, `for_each`, statement sequences and `return`,
`??`, string literals, `array.<name>[index]`, the `geometry.`/`material.`/
`texture.` resource namespaces, `->` for reading another entity, and `this`.
The full `math.*` table too — including `sign`, `copy_sign`, `inverse_lerp`,
`die_roll(_integer)`, `hermite_blend`, `lerprotate`, `min_angle` and all
**30** `math.ease_*` functions, with the engine's own quirks: `t` is never
clamped, the `expo` curves have no endpoint special cases, and `sine`/
`elastic` read a 65536-entry quantised sine table rather than calling `sinf`
(`math.sin`/`math.cos` themselves do call it).

Where the language refuses something — arithmetic on a resource or an array
element, statements after a `return`, a chained `->` — this refuses it too. A
tool that accepts what the game rejects is worse than one that does less.

On top of the language: `Format`/`Minify` printing with precedence-correct
parenthesization, constant folding, macro expansion via a public `Registry`
(`bitshift(x, n)` ships as the worked example), `References` for what a
program names without running it, and a per-context operation allow-list
(see "What sets it apart").

Three things come from the host rather than being built in:

| | |
| --- | --- |
| `for_each`'s source | `array.<name>` only. Molang also allows an entity array; there are no entities here. What it does walk, it walks identically. |
| `->`'s target | `eval.Entity` — published variables and query functions. A host with richer objects hands over a view of them. |
| `this` | `eval.Context.This`, a number the host sets, because what it means belongs to the host. |

**Deliberately left out**, each a choice rather than a gap:

- **A broader optimizer** beyond `Minify` and constant folding. The
  `transform` package is shaped so one fits later without changing its
  public surface.
- **Renaming `temp.`/`variable.` members under `Minify`.** `temp.` is
  thread-local storage shared across a whole worldgen generation and
  `variable.` is shared along a placement chain, so packs pass values
  between separate files through them. Shortening a member name is only
  safe with whole-pack analysis. `Minify` shortens the namespace spelling
  and nothing else.
- **Worldgen query functions in the core** (`query.noise`, `has_biome_tag`,
  `heightmap`, `above_top_solid`). Real Bedrock queries, but specific to one
  game system. They live in the separate `worldgen` package — including a
  bit-exact `query.noise` port — wired in through `eval.Context.QueryFuncs`.

## What sets it apart

- **RNG is always caller-injected** (`eval.RNG`). Nothing here reaches for a
  package-level source of randomness, because the number and order of draws
  is part of the contract rather than an implementation detail.
- **Arithmetic is float32**, rounded at every step the engine rounds, so
  `0.1 + 0.2` is `0.30000001` rather than `0.3`. Division carries the
  engine's near-zero denominator guard on top of that, which is why `1 / 0`
  is `0` and not `Infinity`.
- **Evaluation is closure-compiled, not tree-walked.** `eval.Compile`
  resolves every node's shape once into a tree of Go closures; `Run` pays
  only for the closure calls. For the corpus's largest real expression,
  evaluating once is ~600x cheaper than parsing it.
- **Key presence in a scope bag is semantic.** An absent `temp.`/`variable.`/
  `context.` key is an *unresolved read*, not a zero: it diverts an enclosing
  `??`, and with nothing to catch it, **ends the program where it stands** —
  assignments and RNG draws after it never happen. A key present holding `0`
  is an ordinary zero. `query.` does not participate.
  `Context.ContinueOnUnresolvedRead` opts out of the abort and only the
  abort; `Context.OnUnresolvedRead` reports the reads that would have
  aborted. Both are off by default: say nothing and you get the engine.
- **Operations can be forbidden per context.** The engine decides what an
  expression may contain *after* parsing, by intersecting the operations it
  used with an allow-list belonging to the field it was written in. `ast.Op`,
  `ast.OpsUsed` and `CheckOps` model that, and `DisallowSideEffects` is the
  engine's own preset. It matters because parsing here is necessary but not
  sufficient: `math.max(v.a = 5, 3)` parses everywhere and loads only where
  assignment is allowed.

## Sharp edges

Molang surprises people in specific places, and this reproduces them rather
than smoothing them over. Each is documented at the code that implements it,
and the tests are the fastest way to see one.

- **Numbers only; booleans are 1/0.** No `%` operator — `math.mod` instead.
  `&&`/`||` normalize to 1/0 and short-circuit.
- **`??` is a try/catch over an unresolved read**, not NaN- or
  null-coalescing. `v.unset ?? 5` is 5; `v.x = 0; v.x ?? 5` is 0;
  `math.sqrt(-1) ?? 5` is NaN.
- **Falsiness is exactly zero.** `?`, `!`, `&&` and `||` all treat NaN as
  truthy, so `math.sqrt(-1) ? 111 : 222` is 111.
- **A trailing `;` throws the value away.** `1+1;` is 0 and `1+1` is 2;
  `temp.a=5;` is 0 and still assigns. A source with no top-level `;` parses
  as one bare expression, which is how most real single-field strings are
  written. `ast.Program.HasSemicolon` carries the distinction and the
  printers preserve it.
- **An assignment is an expression** yielding the assigned value, so
  `math.max(v.a = 5, 3)` is 5.
- **Member names may contain dots.** `variable.st.height` is one opaque key,
  not a nested-property access — Molang splits only on the first dot.
- **Calling a non-function member is not an error.** `query.<name>(...)` with
  no registered implementation evaluates its arguments left to right (so RNG
  draws keep their order), discards them, and returns the plain lookup.
- **`loop()` is capped at 1024 iterations here, and the game does not cap it
  at all.** Mojang's syntax guide says it does ("the maximum loop counter is
  1024 for safety reasons"); the shipped build disagrees. The loop-entry
  instruction stores the count unclamped, the back edge only compares it
  against zero, and there is no 1024 anywhere in the engine's Molang code as
  either an integer or a float. Confirmed by running
  `v.n = 0; loop(5000, { v.n = v.n + 1; }); return v.n;` in 1.26.50.24, which
  returns **5000**. So this clamp follows the documentation rather than the
  binary — the safer way round for a tool that previews expressions out of
  third-party packs, where a `loop(v.big, …)` typo hanging it is worse than a
  wrong number. Raise `eval.LoopCounterMax` to match the game instead.

## Differential validation

This library is checked against a corpus of **every distinct Molang
expression in a real, shipping add-on** — 1,560 of them, each pinned with its
result and the number of random draws it consumed, and each additionally
evaluated against five seeded scope snapshots for 9,360 pinned pairs in total.

**That corpus is not in this repository, and will not be.** The expressions
are a real project's authoring vocabulary and worldgen logic in plain text;
publishing them would publish the project. The tests, their fixtures and the
tool that regenerates them are kept locally and run before releases. A clone
therefore gets everything except that one baseline, and every other test —
including the ones that catch the interesting bugs — runs unchanged.

What the corpus is good for, and what it is not: it is a **regression**
baseline. A green run proves a change did not alter the result or the draw
count of 1,560 real expressions. It is not evidence that this library matches
the real game, because both sides of the comparison are this library. The
behaviours that ARE checked against the engine are documented where they are
implemented, each at the site it affects.

Two real bugs were found this way rather than reasoned about, which is the
argument for keeping such a corpus at all even unpublished:

- Molang member names may contain further dots after the namespace, so
  `v.foo.bar` is one name and not a field access on `v.foo`.
- A bare `{ ...statements }` block with no preceding `cond ?` is valid Molang
  and executes, which a hand-written test suite had not thought to try.

The very first generation of the corpus also found a crash: an expression
reading a variable nothing had set took the process down rather than
resolving to zero.


## Benchmarks

Measured on the corpus described above, so the benchmark harness is one of
the files kept local. The numbers are worth recording even though the harness
is not published:

```
BenchmarkParseSmall-12     1487482      1767 ns/op    14.15 MB/s    1000 B/op     14 allocs/op
BenchmarkParseLarge-12        1945   1063805 ns/op    29.15 MB/s  564715 B/op   5377 allocs/op
BenchmarkEvalSmall-12     38841739      60.46 ns/op                  16 B/op      1 allocs/op
BenchmarkEvalLarge-12      1391697      1755 ns/op                   48 B/op      4 allocs/op
BenchmarkCompileLarge-12      8851    262861 ns/op               141400 B/op   5053 allocs/op
```

"Small" is `3 + math.random(0, 1) * 4`; "Large" is a real 31,015-character
worst-case expression. Evaluating the large expression once (~1.8µs) is
roughly 600x cheaper than parsing it (~1.1ms) — the speed claim motivating the
closure-compiled evaluator (parse once, evaluate repeatedly) is measured, not
assumed.


## How this compares to the JavaScript implementations

Two other Molang implementations are in wide use, and both are JavaScript:

- **[bridge-core/molang](https://github.com/bridge-core/molang)** (npm
  `molang`, v2.0.1) — used by bridge. v2.
- **[JannisX11/MolangJS](https://github.com/JannisX11/MolangJS)** (npm
  `molangjs`, v1.7.0) — used by Blockbench and Snowstorm.

They are not really competitors: they are editor and preview libraries,
where an expression is evaluated to draw something on screen and being a
few ULPs off is invisible. This package exists to answer what the *engine*
would compute, draw for draw. The tables below are what that difference
costs and buys.

### Language surface

|                                     | molang-go | bridge | MolangJS |
| ----------------------------------- | :-------: | :----: | :------: |
| 30 × `math.ease_*`                  | yes       | **no** | yes      |
| `math.sign` / `copy_sign` / `inverse_lerp` | yes | **no** | yes      |
| `for_each`, `array.<name>[i]`       | yes       | yes    | **no**   |
| `->` (read another entity)          | yes       | yes    | **no**   |
| String literals                     | yes       | yes    | compare only |
| `geometry.`/`material.`/`texture.`  | yes       | as a plain name | **no** |
| `loop` / `break` / `continue`       | yes       | yes    | yes      |
| Format / Minify                     | yes       | yes    | **no**   |
| Constant folding                    | yes       | yes    | **no**   |
| User-defined functions (`f.foo()`)  | **macros only** | yes | **no** |
| Caller-injected RNG                 | yes       | **no** | **no**   |
| float32 arithmetic                  | yes       | **no** | **no**   |

"compare only" means MolangJS evaluates `'abc' == 'abc'` and reads a string
a host put in the scope, but `temp.s = 'foo'` does not store one, so the
comparison afterwards is false.

### Where the three disagree

Same expression, all three libraries, empty scope:

| expression                          | molang-go | bridge   | MolangJS |
| ----------------------------------- | --------- | -------- | -------- |
| `0.1 + 0.2`                         | 0.30000001| 0.3      | 0.3      |
| `1 / 0.0000001`                     | 0         | 1e7      | 1e7      |
| `1 / 0`                             | 0         | Infinity | Infinity |
| `math.sqrt(-1) ? 111 : 222`         | 111       | 222      | 222      |
| `1 + 1;`                            | 0         | 0        | 2        |
| `v.unset; t.x = 5; return t.x;`     | 0         | 5        | 5        |
| `math.sqrt(-1) ?? 5`                | NaN       | NaN      | 0        |
| `v.a.b = 7; return v.a.b;`          | 7         | 7        | 0        |
| `2 && 3`                            | 1         | 3        | 1        |
| `m.sin(90)`                         | rejected  | rejected | **0**    |

Every row where this package differs is a behaviour with a written reason
next to the code that implements it: float32 rounding and the near-zero
division guard in `eval`, the unresolved-read abort in
`eval/unresolved.go`, falsiness-is-exactly-zero in the conditional
operators, and the `m.` rejection in `ast.NamespaceAliases`. That is an
argument, not a proof — see **Differential validation** above for what this
package can and cannot claim about matching the game.

### Speed

bridge's own benchmark script (the `variable.hand_bob` vanilla expression),
100,000 iterations, same machine:

|                            | molang-go | bridge  | MolangJS |
| -------------------------- | --------- | ------- | -------- |
| parse + evaluate, cold     | **14.1 µs** | 17.4 µs | 30.2 µs |
| evaluate only (parsed once)| **0.45 µs** | 0.73 µs | 0.70 µs |

Cross-language micro-benchmarks deserve suspicion: V8's JIT gets a fully
warmed hot loop here, its nursery makes the short-lived AST nodes nearly
free to collect, and this package pays for float32 rounding at every step
that the other two skip. Treat anything under ~1.5x as noise. The
evaluate-only row is the one that matters for a caller doing what this
package is built for — parse once, evaluate per block.

Measured 2026-09-09 on Windows, Go 1.26.7, Node 22.20.0, Ryzen 5 5600X,
against `molang@2.0.1` and `molangjs@1.7.0`. Every table above is
reproducible from those versions; nothing in it is quoted from another
project's README.


## Usage

```go
import molang "github.com/stirante/molang-go"

prog, err := molang.Compile("temp.x = temp.x + 1; return temp.x;")
if err != nil { ... }

ctx := &molang.Context{RNG: myRNG, Scope: molang.NewScope()}
v := prog.Run(ctx) // 1
v = prog.Run(ctx)  // 2 -- same ctx, temp.x persists
```

```go
tree, _ := molang.Parse(src)
readable := printer.Format(tree)
shortest := printer.Minify(tree)

reg := transform.NewRegistryWithDefaults() // includes bitshift(x, n)
expanded, _ := reg.Expand(tree)            // macro calls -> core Molang
folded := transform.FoldConstants(expanded)
```

### From the command line

The binary is named for its directory, so it is `molang`, not `molang-go`:

```
go install github.com/stirante/molang-go/cmd/molang@latest
```

```
$ molang eval -scope v.x=10 'v.x * 2 + 1'
21
$ molang minify 'temp.a = 1 + 2 * 3; return temp.a;'
t.a=1+2*3;return t.a
```

**Stages chain by being named together, and a chain costs one parse** — the
tree is handed from stage to stage rather than printed and re-read:

```
$ molang expand,fold,minify 'math.bitshift(8 * 2, 2) + 1 - 1'
4
$ molang expand,fold,eval 'math.bitshift(64, 3)'
8
```

Ending a chain in something that is not Molang, as `eval` does there, is only
possible in this form. Every stage also reads stdin and writes stdout on its
own, so `molang expand | molang fold | molang minify` works too — but it pays
a print and a re-parse at each boundary, which on a 33 KB expression is 59 ms
against 36 ms for the comma form.

| | |
| --- | --- |
| `fmt` `minify` `fold` `expand` | source in, source out; chainable |
| `eval` | evaluate — `-scope`, `-seed` for a reproducible draw sequence, `-continue`, `-v` |
| `validate` | exit 1 if it would not load — `-no-side-effects`, `-no-random`, `-forbid` |
| `ast` | the syntax tree, as an outline or `-json` |
| `refs` `ops` | what the program names, and which operations it uses |
| `repl` | an interactive session that keeps one scope and one RNG across lines |

The REPL is the quickest way to meet the behaviour in **Sharp edges**:

```
> :set v.zero = 0
> v.zero ?? 99
0
> :unset v.zero
> v.zero ?? 99
99
```

### In a browser

Go compiles to WebAssembly, so a web editor is not out of reach — this
package builds and runs under `GOOS=js GOARCH=wasm` unmodified:

```
GOOS=js GOARCH=wasm go build -o molang.wasm .
```

A program that parses, evaluates, minifies and prints comes to **2.97 MB**,
or **0.84 MB** gzipped over the wire, and runs on Node's WASM runtime as-is.
That is real weight next to a JavaScript library measured in tens of
kilobytes — the Go runtime comes along with it — so it is worth paying only
when the answers have to match the engine rather than merely look right.

## Testing

```
go test ./...
```

That runs everything a clone has. The differential corpus and its benchmarks
are kept local (see **Differential validation**), so nothing here skips for a
missing fixture — if a test is present, it runs.

`language_test.go` and `mathlib_test.go` are the plain-language baseline:
precedence, associativity, literals, scopes, loops, and one or two ordinary
values for every function in the math library. They need no fixture and assume
nothing about the environment. `mathlib_test.go` also fails when a function is
added to the math table without a case, so that coverage cannot quietly rot.

The rest of the suite is weighted towards the places where Molang surprises
people — unresolved reads ending an expression, NaN counting as truthy, the
quantised sine table, float32 rounding at every step. Those tests are the ones
to read when a value looks wrong.


## License

MIT — see [LICENSE](LICENSE).
