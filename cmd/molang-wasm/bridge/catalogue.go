package bridge

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/stirante/molang-go/ast"
)

// Catalogue is what the host knows about the functions and contexts an
// expression can name. It arrives as the extension's catalogue.json, and its
// shape is that file's format; every field is optional, so a partial
// catalogue works as far as it goes and a newer one with fields this package
// does not read still loads.
//
// The language itself does not need it. Parsing, the load-time rules and the
// math table are all built in; what the catalogue adds is knowledge of the
// host -- which query functions exist and what they take -- which is exactly
// the part a standalone Molang implementation cannot know.
type Catalogue struct {
	GameVersion string `json:"gameVersion"`
	// Partial marks a catalogue that lists only some of the game's queries,
	// such as the hand-written one shipped before the real one exists. A
	// query missing from a partial catalogue says nothing about the query,
	// so it is not reported as unknown with the usual severity (see
	// Options.UnknownQueries).
	Partial    bool         `json:"partial"`
	Queries    []Function   `json:"queries"`
	Math       []Function   `json:"math"`
	Namespaces []Namespace  `json:"namespaces"`
	Contexts   ContextTable `json:"contexts"`

	queries  map[string]*Function
	math     map[string]*Function
	contexts map[string]*Context
}

// Function is one query.* or math.* entry.
type Function struct {
	Name         string          `json:"name"`
	Args         []Arg           `json:"args"`
	Variadic     bool            `json:"variadic"`
	MinArgs      *int            `json:"minArgs"`
	MaxArgs      *int            `json:"maxArgs"` // -1 is unbounded
	Returns      string          `json:"returns"`
	Deprecated   *Deprecation    `json:"deprecated"`
	VersionGate  json.RawMessage `json:"versionGate"`
	Experimental json.RawMessage `json:"experimental"`
	// Contexts lists the context ids the function is available in. Absent
	// or null means everywhere; an empty list means nowhere, which is
	// taken at its word.
	Contexts    *[]string `json:"contexts"`
	ClientOnly  bool      `json:"clientOnly"`
	Description string    `json:"description"`
	Notes       []string  `json:"notes"`
}

// Arg is one positional argument of a Function.
type Arg struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Optional bool   `json:"optional"`
}

// Deprecation says a function should no longer be used, and what instead.
type Deprecation struct {
	Replacement *string `json:"replacement"`
	Note        string  `json:"note"`
}

// Namespace documents one namespace spelling.
type Namespace struct {
	Name        string `json:"name"`
	Alias       string `json:"alias"`
	Description string `json:"description"`
}

// Context is one place an expression can be written. The extraction side's
// format for these is still settling, so the fields read here are the ones
// with an obvious meaning, and anything else is ignored.
type Context struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Variables are the context.* names the host publishes there.
	Variables []string `json:"variables"`
	// DisallowedOps are operations the context refuses, named as the
	// engine names them ("Assignment '='"), by their symbol ("=",
	// "math.random") or by their leading word ("assignment").
	DisallowedOps []string `json:"disallowedOps"`
}

// ContextTable accepts the contexts either as a list of objects carrying
// their id, or as an object keyed by id -- the format note describes the
// content ("id -> human name, ...") without pinning which.
type ContextTable []Context

func (t *ContextTable) UnmarshalJSON(b []byte) error {
	var list []Context
	if err := json.Unmarshal(b, &list); err == nil {
		*t = list
		return nil
	}
	var byID map[string]json.RawMessage
	if err := json.Unmarshal(b, &byID); err != nil {
		return fmt.Errorf("contexts: want a list or an object keyed by id: %w", err)
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var c Context
		// A bare string is just the human name.
		if err := json.Unmarshal(byID[id], &c.Name); err != nil {
			if err := json.Unmarshal(byID[id], &c); err != nil {
				return fmt.Errorf("contexts.%s: %w", id, err)
			}
		}
		c.ID = id
		*t = append(*t, c)
	}
	return nil
}

// ParseCatalogue reads a catalogue.json document.
func ParseCatalogue(data string) (*Catalogue, error) {
	var c Catalogue
	if err := json.Unmarshal([]byte(data), &c); err != nil {
		return nil, err
	}
	c.index()
	return &c, nil
}

func (c *Catalogue) index() {
	c.queries = map[string]*Function{}
	c.math = map[string]*Function{}
	c.contexts = map[string]*Context{}
	for i := range c.Queries {
		c.queries[strings.ToLower(c.Queries[i].Name)] = &c.Queries[i]
	}
	for i := range c.Math {
		c.math[strings.ToLower(c.Math[i].Name)] = &c.Math[i]
	}
	for i := range c.Contexts {
		c.contexts[c.Contexts[i].ID] = &c.Contexts[i]
	}
}

// query looks a query up by member name, case-insensitively as the game
// reads names.
func (c *Catalogue) query(name string) *Function {
	if c == nil {
		return nil
	}
	return c.queries[strings.ToLower(name)]
}

func (c *Catalogue) context(id string) *Context {
	if c == nil {
		return nil
	}
	return c.contexts[id]
}

// hasQueries reports whether the catalogue says anything about queries. With
// no query list at all, every query would be unknown, which is a statement
// about the catalogue rather than the expression.
func (c *Catalogue) hasQueries() bool { return c != nil && len(c.Queries) > 0 }

// ArgRange is the number of arguments f accepts, max -1 for unbounded. The
// explicit counts win; without them the argument list decides.
func (f *Function) ArgRange() (min, max int) {
	for _, a := range f.Args {
		if !a.Optional {
			min++
		}
	}
	max = len(f.Args)
	if f.Variadic {
		max = -1
	}
	if f.MinArgs != nil {
		min = *f.MinArgs
	}
	if f.MaxArgs != nil {
		max = *f.MaxArgs
	}
	return min, max
}

// available reports whether f may be named in context id. An unknown
// context, or a function with no context list, says nothing either way.
func (f *Function) available(id string) bool {
	if id == "" || f.Contexts == nil {
		return true
	}
	for _, c := range *f.Contexts {
		if c == id {
			return true
		}
	}
	return false
}

// opsByName resolves the spellings Context.DisallowedOps accepts.
var opsByName = func() map[string]ast.Op {
	m := map[string]ast.Op{}
	for i := 0; i < 256; i++ {
		op := ast.Op(i)
		name := op.String()
		if name == ast.Op(255).String() {
			continue
		}
		m[strings.ToLower(name)] = op
		if q := strings.IndexByte(name, '\''); q >= 0 {
			word := strings.ToLower(strings.TrimSpace(name[:q]))
			if _, taken := m[word]; !taken {
				m[word] = op
			}
			sym := strings.TrimSuffix(name[q+1:], "'")
			if _, taken := m[sym]; !taken && sym != "" {
				m[sym] = op
			}
		}
	}
	return m
}()

// lookupOp resolves one Context.DisallowedOps entry.
func lookupOp(name string) (ast.Op, bool) {
	op, ok := opsByName[strings.ToLower(strings.TrimSpace(name))]
	return op, ok
}
