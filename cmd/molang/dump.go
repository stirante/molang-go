package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"molang-go/ast"
)

// node is a printable description of one AST node. Both the indented outline
// and the JSON form are built from it, so the two can never drift apart and
// describe different trees.
type node struct {
	Kind     string         `json:"kind"`
	Attrs    map[string]any `json:"attrs,omitempty"`
	Children []child        `json:"children,omitempty"`
}

type child struct {
	Name string `json:"name,omitempty"`
	Node *node  `json:"node"`
}

func reportAST(prog *ast.Program, opts *options, out io.Writer) error {
	root := describeProgram(prog)
	if opts.jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(root)
	}
	writeRoot(out, root)
	return nil
}

// head is the one-line description of a node: the field it fills, its kind,
// and whatever attributes it carries.
func head(n *node, label string) string {
	h := n.Kind
	if label != "" {
		h = label + ": " + n.Kind
	}
	if len(n.Attrs) > 0 {
		h += "  " + formatAttrs(n.Attrs)
	}
	return h
}

// writeRoot prints the root flush left; its children start the indentation.
func writeRoot(out io.Writer, n *node) {
	if n == nil {
		return
	}
	fmt.Fprintln(out, head(n, ""))
	for i, c := range n.Children {
		writeOutline(out, c.Node, "", i == len(n.Children)-1, c.Name)
	}
}

// writeOutline draws the tree with the usual box characters, prefixing a
// child with the field it fills so that a ternary's three branches are
// distinguishable at a glance.
func writeOutline(out io.Writer, n *node, prefix string, last bool, label string) {
	if n == nil {
		return
	}
	connector, indent := "├─ ", "│  "
	if last {
		connector, indent = "└─ ", "   "
	}
	fmt.Fprintf(out, "%s%s%s\n", prefix, connector, head(n, label))

	for i, c := range n.Children {
		writeOutline(out, c.Node, prefix+indent, i == len(n.Children)-1, c.Name)
	}
}

func formatAttrs(a map[string]any) string {
	// Stable order: the few keys used here read best in this sequence.
	order := []string{"op", "namespace", "member", "name", "value", "text", "hasSemicolon"}
	s := ""
	for _, k := range order {
		v, ok := a[k]
		if !ok {
			continue
		}
		if s != "" {
			s += " "
		}
		s += fmt.Sprintf("%s=%v", k, v)
	}
	return s
}

// ---------------------------------------------------------------------

func describeProgram(p *ast.Program) *node {
	n := &node{Kind: "Program", Attrs: map[string]any{"hasSemicolon": p.HasSemicolon}}
	for _, s := range p.Stmts {
		n.Children = append(n.Children, child{Node: describeStmt(s)})
	}
	return n
}

func describeStmt(s ast.Stmt) *node {
	switch t := s.(type) {
	case *ast.ExprStmt:
		return &node{Kind: "ExprStmt", Children: kids(child{Node: describeExpr(t.X)})}
	case *ast.ReturnStmt:
		return &node{Kind: "ReturnStmt", Children: kids(child{Node: describeExpr(t.Value)})}
	case *ast.BreakStmt:
		return &node{Kind: "BreakStmt"}
	case *ast.ContinueStmt:
		return &node{Kind: "ContinueStmt"}
	case *ast.CondBlockStmt:
		return describeCondBlock(t)
	case *ast.LoopStmt:
		return &node{Kind: "LoopStmt", Children: kids(
			child{Name: "count", Node: describeExpr(t.Count)},
			child{Name: "body", Node: describeBlock(t.Body)},
		)}
	case *ast.ForEachStmt:
		return &node{
			Kind:  "ForEachStmt",
			Attrs: map[string]any{"name": "array." + t.Array},
			Children: kids(
				child{Name: "var", Node: describeExpr(t.Var)},
				child{Name: "body", Node: describeBlock(t.Body)},
			),
		}
	case nil:
		return nil
	default:
		return &node{Kind: fmt.Sprintf("%T", s)}
	}
}

func describeCondBlock(t *ast.CondBlockStmt) *node {
	if t == nil {
		return nil
	}
	n := &node{Kind: "CondBlockStmt", Children: kids(
		child{Name: "cond", Node: describeExpr(t.Cond)},
		child{Name: "body", Node: describeBlock(t.Body)},
	)}
	if t.Else != nil {
		n.Children = append(n.Children, child{Name: "else", Node: describeCondBlock(t.Else)})
	}
	return n
}

func describeBlock(b *ast.Block) *node {
	if b == nil {
		return nil
	}
	n := &node{Kind: "Block"}
	for _, s := range b.Stmts {
		n.Children = append(n.Children, child{Node: describeStmt(s)})
	}
	return n
}

func describeExpr(e ast.Expr) *node {
	switch t := e.(type) {
	case nil:
		return nil
	case *ast.NumberLit:
		return &node{Kind: "NumberLit", Attrs: map[string]any{
			"value": strconv.FormatFloat(t.Value, 'g', -1, 64)}}
	case *ast.BoolLit:
		return &node{Kind: "BoolLit", Attrs: map[string]any{"value": t.Value}}
	case *ast.StringLit:
		return &node{Kind: "StringLit", Attrs: map[string]any{"text": t.Value}}
	case *ast.Ident:
		return &node{Kind: "Ident", Attrs: map[string]any{
			"namespace": t.Namespace.String(), "member": t.Member}}
	case *ast.ThisExpr:
		return &node{Kind: "This"}
	case *ast.UnaryExpr:
		return &node{Kind: "UnaryExpr", Attrs: map[string]any{"op": unaryOpName(t.Op)},
			Children: kids(child{Node: describeExpr(t.X)})}
	case *ast.BinaryExpr:
		return &node{Kind: "BinaryExpr", Attrs: map[string]any{"op": binaryOpName(t.Op)},
			Children: kids(
				child{Name: "lhs", Node: describeExpr(t.X)},
				child{Name: "rhs", Node: describeExpr(t.Y)},
			)}
	case *ast.AssignExpr:
		return &node{Kind: "AssignExpr", Children: kids(
			child{Name: "target", Node: describeExpr(t.Target)},
			child{Name: "value", Node: describeExpr(t.Value)},
		)}
	case *ast.CallExpr:
		n := &node{Kind: "CallExpr", Children: kids(child{Name: "callee", Node: describeExpr(t.Callee)})}
		for i, a := range t.Args {
			n.Children = append(n.Children, child{Name: "arg" + strconv.Itoa(i), Node: describeExpr(a)})
		}
		return n
	case *ast.TernaryExpr:
		n := &node{Kind: "TernaryExpr", Children: kids(
			child{Name: "cond", Node: describeExpr(t.Cond)},
			child{Name: "then", Node: describeExpr(t.Then)},
		)}
		if t.Else != nil {
			n.Children = append(n.Children, child{Name: "else", Node: describeExpr(t.Else)})
		}
		return n
	case *ast.ArrowExpr:
		return &node{Kind: "ArrowExpr", Children: kids(
			child{Name: "entity", Node: describeExpr(t.Entity)},
			child{Name: "read", Node: describeExpr(t.Read)},
		)}
	case *ast.ArrayAccess:
		return &node{Kind: "ArrayAccess", Attrs: map[string]any{"name": "array." + t.Name},
			Children: kids(child{Name: "index", Node: describeExpr(t.Index)})}
	case *ast.CondBlockStmt:
		return describeCondBlock(t)
	default:
		return &node{Kind: fmt.Sprintf("%T", e)}
	}
}

func kids(cs ...child) []child {
	out := make([]child, 0, len(cs))
	for _, c := range cs {
		if c.Node != nil {
			out = append(out, c)
		}
	}
	return out
}

// The ast package has no String() for its operator enums -- it does not need
// one, since the printer works from the tree shape. These tables exist for
// the dump alone.
func unaryOpName(op ast.UnaryOp) string {
	switch op {
	case ast.Neg:
		return "-"
	case ast.LNot:
		return "!"
	}
	return "?"
}

func binaryOpName(op ast.BinaryOp) string {
	switch op {
	case ast.Add:
		return "+"
	case ast.Sub:
		return "-"
	case ast.Mul:
		return "*"
	case ast.Div:
		return "/"
	case ast.CmpLt:
		return "<"
	case ast.CmpLe:
		return "<="
	case ast.CmpGt:
		return ">"
	case ast.CmpGe:
		return ">="
	case ast.CmpEq:
		return "=="
	case ast.CmpNe:
		return "!="
	case ast.LAnd:
		return "&&"
	case ast.LOr:
		return "||"
	case ast.NullCoalesce:
		return "??"
	}
	return "?"
}
