//go:build js && wasm

// Command molang-wasm is molang-go compiled to WebAssembly for editors: the
// language intelligence behind the VS Code extension in apps/vscode, and
// usable from any JavaScript host that can run Go's wasm_exec.js.
//
//	GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o molang.wasm ./cmd/molang-wasm
//
// Everything it does is in package bridge, which is plain Go and tested
// natively; this file only moves strings across. Standard Go rather than
// TinyGo, because the analysis has to be the library's own code path --
// the same parser, the same rules, the same wording as the tests pin.
//
// # API
//
// Running the module (new Go().run(instance)) defines one global object,
// globalThis.molangBridge, and then waits for calls. Every function takes
// and returns strings, JSON where structured, and never throws: a failure
// comes back as {"error": "..."}.
//
//	molangBridge.version -> string
//
// The bridge API version, "1". It changes only when a result shape changes
// incompatibly.
//
//	molangBridge.setCatalogue(catalogueJSON) -> {ok, error?, gameVersion, queries, math, contexts, partial?}
//
// Loads the catalogue of queries, math functions and contexts: the
// catalogue.json format the extension ships. Until one is loaded, queries are
// not checked at all. A document that fails to load leaves the previous
// catalogue in place.
//
//	molangBridge.analyze(source, optionsJSON) -> Result
//
// Reads one expression and reports everything an editor shows about it.
// optionsJSON may be "" or "null" for the defaults:
//
//	{
//	  "querySet": "default",            // or "tags", "world_gen": the set the field resolves; "" = any
//	  "allowedQueries": ["query.block_state"],  // a field's fixed allow-list, in place of the set
//	  "version": "1.20.40",             // the version the file is read at, for version gates; "" = skip
//	  "context": "client_entity",       // catalogue context id, for its restrictions; "" = unknown
//	  "restrict": "no_side_effects",    // or "no_side_effects_or_random"; the engine's own restrictions
//	  "disallowedOps": ["Assignment '='"],  // more operations to refuse
//	  "optionalSemicolons": false,      // fragments: lift the game's ';' rules
//	  "unknownQueries": "warning"       // severity of a name that does not resolve; "off" to skip
//	}
//
// See bridge.Options for what each means.
//
// The Result:
//
//	{
//	  "ok": true,                       // no error-severity diagnostic
//	  "diagnostics": [{start, end, byteStart, byteEnd, severity, code, message, tags?}],
//	  "tokens":      [{start, end, type, mods?}],
//	  "refs":        [{namespace, written, name, start, end, nameStart, nameEnd,
//	                   write?, call?, args, callEnd?, arrow?}],
//	  "symbols":     [{namespace, name, reads: [{start, end}], writes: [{start, end}]}]
//	}
//
// OFFSETS ARE UTF-16 CODE UNITS into source, end exclusive -- JavaScript
// string indices, which is what VS Code positions count. byteStart/byteEnd
// give a diagnostic's range in UTF-8 bytes as well. The two differ after the
// first character outside ASCII, which in Molang can only be in a string
// literal.
//
// diagnostics: severity is error, warning, information or hint. code is
// "syntax" for the parser -- every syntax error, not just the first, each in
// the game's own wording -- or one of unknown-math, math-arity,
// math-not-called, unknown-query, query-context (outside the field's query
// set or allow-list), query-version, query-arity, query-deprecated,
// op-not-allowed, compile. An empty or all-whitespace source has none.
//
// tokens: semantic tokens with VS Code's standard type names (namespace,
// function, variable, property, keyword, number, string, operator) and
// modifiers (defaultLibrary, readonly, modification, deprecated). A string
// token can span lines; the host splits it if it must.
//
// refs: every namespaced name as written, found from the tokens, so present
// even when the source does not parse. namespace is canonical (query, math,
// variable, temp, context, array, geometry, material, texture); written is
// the spelling ("q"). args is the argument count of a call, -1 when the name
// is not called or its list is not closed.
//
// symbols: the variables, temps, context names and arrays named, lower-cased
// as scopes key them, with where each is read and written. With a clean
// parse the set is molang.References'.
//
//	molangBridge.format(source, optionsJSON) -> {ok, text?, error?}
//	molangBridge.minify(source, optionsJSON) -> {ok, text?, error?}
//
// printer.Format and printer.Minify. A source that does not parse is not
// printed; error is the first syntax error's message. Only
// optionalSemicolons is read from the options.
package main

import (
	"syscall/js"

	"github.com/stirante/molang-go/cmd/molang-wasm/bridge"
)

// apiVersion is molangBridge.version. See the API notes above.
const apiVersion = "1"

func main() {
	a := &bridge.Analyzer{}
	api := js.Global().Get("Object").New()
	api.Set("version", apiVersion)
	for _, method := range []string{"setCatalogue", "analyze", "format", "minify"} {
		method := method
		api.Set(method, js.FuncOf(func(_ js.Value, args []js.Value) any {
			// The arguments go to Call as one JSON array of strings, so the
			// glue never has to know a method's shape. Anything that is not
			// a string -- an omitted options argument -- is null.
			list := js.Global().Get("Array").New()
			for _, v := range args {
				if v.Type() == js.TypeString {
					list.Call("push", v)
				} else {
					list.Call("push", js.Null())
				}
			}
			return a.Call(method, js.Global().Get("JSON").Call("stringify", list).String())
		}))
	}
	js.Global().Set("molangBridge", api)
	// Keep the Go program alive for the calls to come: returning from main
	// would end it, and every function above with it.
	select {}
}
