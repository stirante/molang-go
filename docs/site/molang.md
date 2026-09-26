# Molang overview

Molang is the expression language Minecraft Bedrock add-ons use wherever a value has to be computed while the game runs: animation and render controller logic, particle curves, entity properties, block and item conditions, and world generation. An expression is a string in a pack's JSON (or a `.molang` file, for tools that read them), and the game parses it when it loads the pack.

Every value is a number (true is `1`, false is `0`), a string, or, for some queries, a structure or an array. Names are case-insensitive. A simple expression is a single value, such as `q.health * 0.5`; a complex one is a sequence of statements, each ending in `;`, and returns what its `return` statement gives.

This page lists the namespaces an expression can name and the operators and keywords it can use. The [query reference](/queries/) has a page for each `query.` name, and [math functions](/math) lists the `math.` library.

## Namespaces

<!--@include: ./generated/namespaces.md-->

## Operators and keywords

<!--@include: ./generated/operators.md-->
