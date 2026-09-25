// Package bridge is the editor-facing surface of molang-go: analysis,
// formatting and minifying, as JSON in and JSON out.
//
// It exists separately from cmd/molang-wasm so that everything the
// WebAssembly build does is plain Go, built and tested natively with
// `go test`. The js/wasm main package is a few lines of glue that hands
// strings across; the API it exposes is documented there, and every type
// here is what that API sends.
package bridge

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	molang "github.com/stirante/molang-go"
	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/eval"
	"github.com/stirante/molang-go/lexer"
	"github.com/stirante/molang-go/parser"
	"github.com/stirante/molang-go/printer"
	"github.com/stirante/molang-go/token"
)

// Options says how to read one expression. The zero value is a complete
// expression in an unknown context, which is the strictest reading that
// makes no assumption about where it is written.
type Options struct {
	// QuerySet is the query set the field resolves names against:
	// "default", "tags" (item and block descriptor tag expressions) or
	// "world_gen" (features, feature rules, biome surface adjustments).
	// Empty means unknown, and any set's queries are accepted.
	QuerySet string `json:"querySet,omitempty"`

	// AllowedQueries, when not empty, is the only queries the field
	// resolves, in place of a query set. Four fields have one: an entity
	// property's default (had_component_group), a block permutation's
	// condition and bone_visibility (block_state), and a set_property event
	// response (has_property, property).
	AllowedQueries []string `json:"allowedQueries,omitempty"`

	// Version is the version the file's Molang is read at -- its
	// format_version or its pack's min_engine_version, depending on the
	// file type. It decides the catalogue's version gates. Empty skips them.
	Version string `json:"version,omitempty"`

	// Context is the catalogue context id the expression is written in. It
	// selects the context's own restrictions from the catalogue (see
	// Context.DisallowedOps). Empty means unknown.
	Context string `json:"context,omitempty"`

	// Restrict names one of the engine's own operation restrictions for
	// fields that refuse side effects, independent of the catalogue:
	//
	//	"no_side_effects"           assignment refused (entity property
	//	                            defaults)
	//	"no_side_effects_or_random" assignment, math.random and
	//	                            math.random_integer refused (block
	//	                            permutation conditions, bone_visibility)
	//
	// See molang.SideEffectOps for why die_roll is in neither.
	Restrict string `json:"restrict,omitempty"`

	// DisallowedOps adds operations to refuse, spelled as the catalogue's
	// Context.DisallowedOps are.
	DisallowedOps []string `json:"disallowedOps,omitempty"`

	// OptionalSemicolons lifts the game's semicolon rules, for fragments
	// that are not whole expressions. See lexer.Extensions.
	OptionalSemicolons bool `json:"optionalSemicolons,omitempty"`

	// UnknownQueries is the severity for a query name that does not
	// resolve: one the catalogue does not list, one outside the field's
	// query set or allow-list, one gated out by Version. "error",
	// "warning", "information", "hint" or "off"; empty means "warning".
	//
	// A warning and not an error, though the game logs an error and the
	// expression does not load, because this package does not itself
	// model which names resolve -- the verdict rests on the catalogue and
	// on the host's account of the field. A catalogue marked partial turns
	// a name it lacks into a hint: missing from a list known to be
	// incomplete is a question, not a finding.
	UnknownQueries string `json:"unknownQueries,omitempty"`
}

// Span is a range in UTF-16 code units, end exclusive.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// Diagnostic is one problem with the expression.
type Diagnostic struct {
	Start int `json:"start"` // UTF-16
	End   int `json:"end"`
	// ByteStart and ByteEnd are the same range in UTF-8 bytes, for hosts
	// that index the source as Go does.
	ByteStart int    `json:"byteStart"`
	ByteEnd   int    `json:"byteEnd"`
	Severity  string `json:"severity"` // error, warning, information, hint
	// Code says which check fired: syntax (the parser, in the game's own
	// wording), unknown-math, math-arity, math-not-called, unknown-query,
	// query-arity, query-deprecated, query-context, query-version,
	// op-not-allowed, compile.
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Tags    []string `json:"tags,omitempty"` // "deprecated"
}

// Token is one semantically coloured token. Type and Mods use VS Code's
// standard semantic token names.
type Token struct {
	Start int      `json:"start"`
	End   int      `json:"end"`
	Type  string   `json:"type"`
	Mods  []string `json:"mods,omitempty"`
}

// Ref is one namespaced name as written.
type Ref struct {
	Namespace string `json:"namespace"` // canonical: query, math, variable, temp, context, array, geometry, material, texture
	Written   string `json:"written"`   // the namespace as spelled: "q", "query"
	Name      string `json:"name"`      // the member, as spelled
	Start     int    `json:"start"`     // the whole name
	End       int    `json:"end"`
	NameStart int    `json:"nameStart"` // the member alone
	NameEnd   int    `json:"nameEnd"`
	Write     bool   `json:"write,omitempty"`
	Call      bool   `json:"call,omitempty"`
	Args      int    `json:"args"`              // -1 when not a call, or the list is not closed
	CallEnd   int    `json:"callEnd,omitempty"` // just past ')'
	Arrow     bool   `json:"arrow,omitempty"`   // read through `->`
}

// Symbol is one variable, temp, context name or array the expression names,
// with every place it is read and written.
type Symbol struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"` // lower-cased, as scopes key it
	Reads     []Span `json:"reads"`
	Writes    []Span `json:"writes"`
}

// Result is what Analyze reports.
type Result struct {
	// OK is true when nothing of error severity was found: the expression
	// parses and passes every load-time check this package knows.
	OK          bool         `json:"ok"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Tokens      []Token      `json:"tokens"`
	Refs        []Ref        `json:"refs"`
	Symbols     []Symbol     `json:"symbols"`
}

// Analyzer holds what persists between calls: the catalogue. The zero value
// has none, and everything that does not need one still works.
type Analyzer struct {
	Catalogue *Catalogue
}

// Analyze reads src and reports everything an editor shows about it.
func (a *Analyzer) Analyze(src string, opts Options) Result {
	ext := lexer.Extensions{OptionalSemicolons: opts.OptionalSemicolons}
	u16 := newUTF16Index(src)
	toks, _ := lexer.TokenizeAll(src, ext)
	refs := scanRefs(src, toks)
	res := Result{Diagnostics: []Diagnostic{}, Tokens: []Token{}, Refs: []Ref{}, Symbols: []Symbol{}}

	d := &diagnoser{src: src, u16: u16, toks: toks}

	// An empty source is a file just created or a field just cleared.
	// The parser refuses it ("empty expression"), and the game would too,
	// but saying so while the author is about to type is noise.
	var prog *ast.Program
	if strings.TrimSpace(src) != "" {
		var errs []*parser.Error
		prog, errs = parser.ParseAll(src, ext)
		for _, e := range errs {
			start, end := d.tokenRange(e.Pos)
			d.add(start, end, "error", "syntax", e.Msg)
		}
	}

	a.checkRefs(d, refs, opts, prog != nil)
	if prog != nil {
		a.checkOps(d, prog, refs, opts)
		// The parser has applied every rule but the math table's; Compile
		// applies that too. It is run as a net under checkRefs, which
		// reports the same problems with positions, so it only speaks when
		// the two disagree.
		if !d.has("unknown-math", "math-arity", "math-not-called") {
			if _, err := eval.Compile(prog); err != nil {
				d.add(0, len(src), "error", "compile", err.Error())
			}
		}
	}

	// Lists are never null in the JSON, so a JavaScript caller can iterate
	// them without checking.
	if d.out != nil {
		res.Diagnostics = d.out
	}
	res.OK = true
	for _, x := range res.Diagnostics {
		if x.Severity == "error" {
			res.OK = false
		}
	}
	for _, st := range semanticTokens(src, toks, refs, a.Catalogue) {
		res.Tokens = append(res.Tokens, Token{Start: u16.at(st.start), End: u16.at(st.end), Type: st.typ, Mods: st.mods})
	}
	for _, r := range refs {
		out := Ref{
			Namespace: r.ns.String(), Written: r.written, Name: r.name,
			Start: u16.at(r.start), End: u16.at(r.end),
			NameStart: u16.at(r.nameStart), NameEnd: u16.at(r.nameEnd),
			Write: r.write, Call: r.call, Args: -1, Arrow: r.arrow,
		}
		if r.call {
			out.Args = r.argc
			if r.argc >= 0 {
				out.CallEnd = u16.at(r.callEnd)
			}
		}
		res.Refs = append(res.Refs, out)
	}
	res.Symbols = symbols(prog, refs, u16)
	return res
}

// diagnoser collects diagnostics, converting byte ranges as it goes.
type diagnoser struct {
	src  string
	u16  utf16Index
	toks []token.Token
	out  []Diagnostic
}

func (d *diagnoser) add(start, end int, severity, code, msg string, tags ...string) {
	d.out = append(d.out, Diagnostic{
		Start: d.u16.at(start), End: d.u16.at(end), ByteStart: start, ByteEnd: end,
		Severity: severity, Code: code, Message: msg, Tags: tags,
	})
}

func (d *diagnoser) has(codes ...string) bool {
	for _, x := range d.out {
		for _, c := range codes {
			if x.Code == c {
				return true
			}
		}
	}
	return false
}

// tokenRange widens a parse error's offset to the token that starts there,
// which is what the parser was looking at when it gave up. An offset at the
// end of the source -- where a missing `;` is reported -- becomes an empty
// range just after the last token, which is where the `;` belongs, rather
// than after whatever whitespace trails it.
func (d *diagnoser) tokenRange(pos int) (int, int) {
	i := sort.Search(len(d.toks), func(i int) bool { return d.toks[i].Pos >= pos })
	if i < len(d.toks) && d.toks[i].Pos == pos && d.toks[i].Kind != token.EOF {
		return pos, tokEnd(d.src, d.toks[i])
	}
	if pos >= len(d.src) || (i < len(d.toks) && d.toks[i].Kind == token.EOF && strings.TrimSpace(d.src[pos:]) == "") {
		last := len(d.toks) - 2 // before EOF
		if last >= 0 {
			e := tokEnd(d.src, d.toks[last])
			return e, e
		}
		return len(d.src), len(d.src)
	}
	_, size := utf8.DecodeRuneInString(d.src[pos:])
	return pos, pos + size
}

// checkRefs reports what is wrong with the names themselves: math functions
// the game does not have or called with the wrong count, and queries that do
// not resolve where they are written, or are called with a count the
// catalogue says is wrong.
//
// Argument counts are only checked when the source parsed. In a source that
// does not, a count is as likely to be counting the mistake as the author's
// intent, and the syntax error already says where to look. And a wrong count
// for a query is a warning: the game does not check it when the expression
// loads -- the query itself complains, or not, when it runs.
func (a *Analyzer) checkRefs(d *diagnoser, refs []ref, opts Options, parsed bool) {
	unresolvedSev := opts.UnknownQueries
	if unresolvedSev == "" {
		unresolvedSev = "warning"
	}
	allowed := map[string]bool{}
	for _, q := range opts.AllowedQueries {
		allowed[strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(q, "query."), "q."))] = true
	}
	for i := range refs {
		r := &refs[i]
		switch r.ns {
		case ast.Math:
			a.checkMath(d, r, parsed)
		case ast.Query:
			a.checkQuery(d, r, opts, parsed, allowed, unresolvedSev)
		}
	}
}

// unresolvedMessage is the game's content-log wording for a query name it
// cannot resolve, whatever the reason; why is added after it.
func unresolvedMessage(name, why string) string {
	msg := fmt.Sprintf("Failed to resolve query query.%s. Either the query does not exist or it is not supported in this context.", name)
	if why != "" {
		msg += " (" + why + ")"
	}
	return msg
}

func (a *Analyzer) checkQuery(d *diagnoser, r *ref, opts Options, parsed bool, allowed map[string]bool, sev string) {
	report := func(code, msg string) {
		if sev != "off" {
			d.add(r.start, r.end, sev, code, msg)
		}
	}
	name := r.lowerName()
	// Inside a query's arguments the field's own rules do not apply; see
	// scanRefs.
	if len(allowed) > 0 && !r.inQueryArg && !allowed[name] {
		report("query-context", unresolvedMessage(r.name, "this field allows only "+strings.Join(opts.AllowedQueries, ", ")))
		return
	}
	if !a.Catalogue.hasQueries() {
		return
	}
	f := a.Catalogue.query(r.name)
	if f == nil {
		if a.Catalogue.Partial {
			if opts.UnknownQueries == "" || opts.UnknownQueries == "hint" {
				d.add(r.start, r.end, "hint", "unknown-query", fmt.Sprintf("query.%s is not in the catalogue, which is incomplete", r.name))
			} else {
				report("unknown-query", fmt.Sprintf("query.%s is not in the catalogue", r.name))
			}
			return
		}
		report("unknown-query", unresolvedMessage(r.name, ""))
		return
	}
	// The set the name must be in: the field's own, or none when an
	// allow-list (checked above) replaces it -- except in a query's
	// arguments, which the game reads as default-set Molang wherever they
	// are.
	set := opts.QuerySet
	switch {
	case r.inQueryArg && (set != "" || len(allowed) > 0):
		set = SetDefault
	case len(allowed) > 0:
		set = ""
	}
	if fs := f.querySet(); set != "" && fs != set {
		report("query-context", unresolvedMessage(r.name, fmt.Sprintf("query.%s belongs to %s expressions", r.name, querySetNames[fs])))
		return
	}
	if !f.resolvesAt(opts.Version) {
		why := ""
		if g := f.VersionGate; g.Until != "" && compareVersions(opts.Version, g.Until) >= 0 {
			why = fmt.Sprintf("query.%s was removed in %s", r.name, g.Until)
		} else {
			why = fmt.Sprintf("query.%s was added in %s", r.name, g.Since)
		}
		report("query-version", unresolvedMessage(r.name, why))
		return
	}
	if f.Deprecated != nil {
		msg := fmt.Sprintf("query.%s is deprecated", r.name)
		if f.Deprecated.Replacement != nil && *f.Deprecated.Replacement != "" {
			msg += "; use " + *f.Deprecated.Replacement + " instead"
		}
		if f.Deprecated.Note != "" {
			msg += ". " + f.Deprecated.Note
		}
		d.add(r.start, r.end, "hint", "query-deprecated", msg, "deprecated")
	}
	if parsed {
		n := 0
		if r.call {
			n = r.argc
		}
		if min, max := f.ArgRange(); n >= 0 && (n < min || (max >= 0 && n > max)) {
			end := r.end
			if r.call && r.callEnd > 0 {
				end = r.callEnd
			}
			d.add(r.start, end, "warning", "query-arity",
				fmt.Sprintf("query.%s takes %s, found %d", r.name, argCount(min, max), n))
		}
	}
}

// checkMath repeats eval.Compile's math-table checks with positions, in its
// words. The game refuses all three.
func (a *Analyzer) checkMath(d *diagnoser, r *ref, parsed bool) {
	member := r.lowerName()
	end := r.end
	if r.call && r.callEnd > 0 {
		end = r.callEnd
	}
	if member == "pi" {
		if r.call && r.argc > 0 && parsed {
			d.add(r.start, end, "error", "math-arity", "math.pi takes no arguments")
		}
		return
	}
	arity, ok := eval.MathArity[member]
	if !ok {
		d.add(r.start, r.end, "error", "unknown-math", fmt.Sprintf("unknown math function 'math.%s'", r.name))
		return
	}
	if !r.call {
		d.add(r.start, r.end, "error", "math-not-called", fmt.Sprintf("math.%s must be called, e.g. math.%s(...)", r.name, r.name))
		return
	}
	if !parsed || r.argc < 0 || r.argc == arity {
		return
	}
	op, _ := ast.MathOp(member)
	if arity == 1 {
		d.add(r.start, end, "error", "math-arity",
			fmt.Sprintf("Malformed %s expression. It has %d children but should have between %d and %d", op, r.argc, arity, arity))
		return
	}
	d.add(r.start, end, "error", "math-arity",
		fmt.Sprintf("Unexpected number of parameters to %s function - expected %d, found %d.", op, arity, r.argc))
}

func argCount(min, max int) string {
	plural := func(n int) string {
		if n == 1 {
			return "1 argument"
		}
		return fmt.Sprintf("%d arguments", n)
	}
	switch {
	case max < 0:
		return fmt.Sprintf("at least %s", plural(min))
	case min == max:
		return plural(min)
	}
	return fmt.Sprintf("%d to %s", min, plural(max))
}

// disallowed is the set of operations opts and the catalogue refuse.
func (a *Analyzer) disallowed(opts Options) (ast.OpSet, error) {
	var set ast.OpSet
	switch opts.Restrict {
	case "":
	case "no_side_effects":
		set = set.Union(molang.SideEffectOps(false))
	case "no_side_effects_or_random":
		set = set.Union(molang.SideEffectOps(true))
	default:
		return set, fmt.Errorf("unknown restriction %q", opts.Restrict)
	}
	names := append([]string(nil), opts.DisallowedOps...)
	if c := a.Catalogue.context(opts.Context); c != nil {
		names = append(names, c.DisallowedOps...)
	}
	for _, n := range names {
		op, ok := lookupOp(n)
		if !ok {
			return set, fmt.Errorf("unknown operation %q", n)
		}
		set.Add(op)
	}
	return set, nil
}

// checkOps applies the context's operation restrictions, the check the
// engine makes after parsing (see molang.CheckOps). The engine names only
// the first forbidden operation; every one is reported here, at each place
// it is used, so an author fixes them all in one pass.
func (a *Analyzer) checkOps(d *diagnoser, prog *ast.Program, refs []ref, opts Options) {
	set, err := a.disallowed(opts)
	if err != nil {
		d.add(0, 0, "warning", "op-not-allowed", "cannot apply the context's restrictions: "+err.Error())
		return
	}
	if set.Empty() {
		return
	}
	cerr, ok := molang.CheckOps(prog, set).(*molang.OpNotAllowedError)
	if !ok {
		return
	}
	for _, op := range cerr.Ops.Ops() {
		msg := (&molang.OpNotAllowedError{Op: op}).Error()
		spans := opSpans(op, d.src, d.toks, refs)
		if len(spans) == 0 {
			spans = [][2]int{{0, len(d.src)}}
		}
		for _, s := range spans {
			d.add(s[0], s[1], "error", "op-not-allowed", msg)
		}
	}
}

// opTokens maps the operations that are a single token to that token.
var opTokens = map[ast.Op]token.Kind{
	ast.OpAssignment: token.Assign, ast.OpConditional: token.Question,
	ast.OpConditionalElse: token.Colon, ast.OpNullCoalescing: token.Coalesce,
	ast.OpPointer: token.Arrow, ast.OpLoop: token.Loop, ast.OpForEach: token.ForEach,
	ast.OpBreak: token.Break, ast.OpContinue: token.Continue, ast.OpReturn: token.Return,
	ast.OpThis: token.This, ast.OpString: token.String, ast.OpSemicolon: token.Semi,
	ast.OpLogicalOr: token.Or, ast.OpLogicalAnd: token.And, ast.OpLogicalEqual: token.Eq,
	ast.OpLogicalNotEqual: token.Ne, ast.OpLessThan: token.Lt, ast.OpLessThanOrEqual: token.Le,
	ast.OpGreaterThan: token.Gt, ast.OpGreaterThanOrEqual: token.Ge, ast.OpMultiply: token.Star,
	ast.OpDivide: token.Slash, ast.OpLogicalNot: token.Not,
}

// nsOps maps the namespace operations to their namespace.
var nsOps = map[ast.Op]ast.Namespace{
	ast.OpQueryFunction: ast.Query, ast.OpArrayVariable: ast.Array, ast.OpArray: ast.Array,
	ast.OpContextVariable: ast.Context, ast.OpEntityVariable: ast.Variable,
	ast.OpTempVariable: ast.Temp, ast.OpGeometryVariable: ast.Geometry,
	ast.OpMaterialVariable: ast.Material, ast.OpTextureVariable: ast.Texture,
}

// opSpans finds where op is used, as byte ranges. An operation it cannot
// place returns nothing and is reported against the whole expression.
func opSpans(op ast.Op, src string, toks []token.Token, refs []ref) [][2]int {
	var out [][2]int
	if k, ok := opTokens[op]; ok {
		for _, t := range toks {
			if t.Kind == k {
				out = append(out, [2]int{t.Pos, tokEnd(src, t)})
			}
		}
		return out
	}
	if op == ast.OpAdd || op == ast.OpNegate {
		// `-` is the engine's Add when it follows an operand and a Negate
		// otherwise; `+` is always Add.
		for i, t := range toks {
			if t.Kind != token.Plus && t.Kind != token.Minus {
				continue
			}
			binary := t.Kind == token.Plus || (i > 0 && endsOperand(toks[i-1].Kind))
			if binary == (op == ast.OpAdd) {
				out = append(out, [2]int{t.Pos, tokEnd(src, t)})
			}
		}
		return out
	}
	for _, r := range refs {
		match := false
		if ns, ok := nsOps[op]; ok {
			match = r.ns == ns
		} else if r.ns == ast.Math {
			if op == ast.OpPi {
				match = strings.EqualFold(r.name, "pi")
			} else if m, ok := ast.MathOp(r.name); ok {
				match = m == op
			}
		}
		if match {
			end := r.end
			if r.call && r.callEnd > 0 {
				end = r.callEnd
			}
			out = append(out, [2]int{r.start, end})
		}
	}
	return out
}

func endsOperand(k token.Kind) bool {
	switch k {
	case token.Number, token.String, token.Ident, token.RParen, token.RBracket,
		token.This, token.True, token.False:
		return true
	}
	return false
}

// symbolNamespaces are the namespaces whose names are the expression's own
// state, and so worth listing: what it reads and writes.
var symbolNamespaces = map[ast.Namespace]bool{
	ast.Variable: true, ast.Temp: true, ast.Context: true, ast.Array: true,
}

// symbols lists the variables, temps, context names and arrays named, with
// where each is read and written.
//
// With a tree, the set of names is molang.References -- the package's own
// answer to what a program names -- and the scan only supplies positions.
// Without one, the scan is all there is, and it decides both.
func symbols(prog *ast.Program, refs []ref, u16 utf16Index) []Symbol {
	type key struct {
		ns   ast.Namespace
		name string
	}
	var order []key
	byKey := map[key]*Symbol{}
	add := func(ns ast.Namespace, name string) *Symbol {
		k := key{ns, strings.ToLower(name)}
		if s := byKey[k]; s != nil {
			return s
		}
		s := &Symbol{Namespace: ns.String(), Name: k.name, Reads: []Span{}, Writes: []Span{}}
		byKey[k] = s
		order = append(order, k)
		return s
	}
	if prog != nil {
		rf := molang.References(prog)
		for _, list := range []struct {
			ns    ast.Namespace
			names []string
		}{
			{ast.Variable, rf.VariableReads}, {ast.Variable, rf.VariableWrites},
			{ast.Temp, rf.TempReads}, {ast.Temp, rf.TempWrites},
			{ast.Context, rf.ContextReads}, {ast.Context, rf.Entities},
			{ast.Array, rf.Arrays},
		} {
			for _, n := range list.names {
				add(list.ns, n)
			}
		}
	}
	for _, r := range refs {
		if !symbolNamespaces[r.ns] {
			continue
		}
		var s *Symbol
		if prog != nil {
			s = byKey[key{r.ns, r.lowerName()}]
			if s == nil {
				continue
			}
		} else {
			s = add(r.ns, r.name)
		}
		sp := Span{Start: u16.at(r.start), End: u16.at(r.end)}
		if r.write {
			s.Writes = append(s.Writes, sp)
		} else {
			s.Reads = append(s.Reads, sp)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].ns != order[j].ns {
			return order[i].ns < order[j].ns
		}
		return order[i].name < order[j].name
	})
	out := make([]Symbol, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

// PrintResult is what Format and Minify report.
type PrintResult struct {
	OK    bool   `json:"ok"`
	Text  string `json:"text,omitempty"`
	Error string `json:"error,omitempty"`
}

// Format prints src with printer.Format. A source that does not parse is not
// printed: the printer works from the tree, and there is none.
func Format(src string, opts Options) PrintResult {
	return printWith(src, opts, printer.Format)
}

// Minify prints src with printer.Minify.
func Minify(src string, opts Options) PrintResult {
	return printWith(src, opts, printer.Minify)
}

func printWith(src string, opts Options, p func(*ast.Program) string) PrintResult {
	prog, err := parser.ParseWith(src, lexer.Extensions{OptionalSemicolons: opts.OptionalSemicolons})
	if err != nil {
		return PrintResult{Error: err.(*parser.Error).Msg}
	}
	return PrintResult{OK: true, Text: p(prog)}
}

// CatalogueSummary is what SetCatalogue reports back.
type CatalogueSummary struct {
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
	GameVersion string `json:"gameVersion,omitempty"`
	Queries     int    `json:"queries"`
	Math        int    `json:"math"`
	Contexts    int    `json:"contexts"`
	Partial     bool   `json:"partial,omitempty"`
}

// SetCatalogue replaces the catalogue. A document that fails to load leaves
// the previous one in place.
func (a *Analyzer) SetCatalogue(data string) CatalogueSummary {
	c, err := ParseCatalogue(data)
	if err != nil {
		return CatalogueSummary{Error: err.Error()}
	}
	a.Catalogue = c
	return CatalogueSummary{OK: true, GameVersion: c.GameVersion, Queries: len(c.Queries),
		Math: len(c.Math), Contexts: len(c.Contexts), Partial: c.Partial}
}

// Call is the whole API as one entry point: a method name and a JSON
// argument list in, a JSON result out. The WebAssembly glue calls nothing
// else, so it stays a few lines and this is where the API is tested.
//
// A panic is returned as {"error": ...} rather than propagated: in the
// WebAssembly build an unrecovered panic ends the Go program, and every
// later call from the editor would fail.
func (a *Analyzer) Call(method, args string) (out string) {
	defer func() {
		if r := recover(); r != nil {
			out = errorJSON(fmt.Sprintf("internal error in %s: %v", method, r))
		}
	}()
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return errorJSON("arguments must be a JSON array: " + err.Error())
	}
	str := func(i int) (string, error) {
		var s string
		if i >= len(raw) {
			return "", fmt.Errorf("%s: missing argument %d", method, i)
		}
		return s, json.Unmarshal(raw[i], &s)
	}
	// Options come as an object, or as a string holding one -- what a
	// JavaScript caller that has already stringified them passes. Absent,
	// null and "" are the defaults.
	opt := func(i int) (Options, error) {
		var o Options
		if i >= len(raw) || string(raw[i]) == "null" {
			return o, nil
		}
		b := []byte(raw[i])
		var s string
		if json.Unmarshal(b, &s) == nil {
			if s == "" {
				return o, nil
			}
			b = []byte(s)
		}
		return o, json.Unmarshal(b, &o)
	}
	src, err := str(0)
	if err != nil {
		return errorJSON(err.Error())
	}
	var result any
	switch method {
	case "setCatalogue":
		result = a.SetCatalogue(src)
	case "analyze", "format", "minify":
		o, err := opt(1)
		if err != nil {
			return errorJSON(err.Error())
		}
		switch method {
		case "analyze":
			result = a.Analyze(src, o)
		case "format":
			result = Format(src, o)
		default:
			result = Minify(src, o)
		}
	default:
		return errorJSON("unknown method " + method)
	}
	b, err := json.Marshal(result)
	if err != nil {
		return errorJSON(err.Error())
	}
	return string(b)
}

func errorJSON(msg string) string {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return string(b)
}
