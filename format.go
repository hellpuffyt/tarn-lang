package tarn

import (
	"fmt"
	"strconv"
	"strings"
)

// Format pretty-prints a module in canonical style (4-space indent, one
// statement per line, spaces around binary operators). Formatting is
// idempotent and preserves semantics: format(parse(format(x))) == format(x).
func Format(m *Module) string {
	f := &formatter{}
	for _, imp := range m.Imports {
		f.line("import %q%s", imp.Path, alias(imp))
	}
	if len(m.Imports) > 0 {
		f.blank()
	}
	for _, t := range m.Tools {
		f.line("tool %s.%s%s", t.NS, t.Name, sig(t.Params, t.Ret))
	}
	if len(m.Tools) > 0 {
		f.blank()
	}
	for _, s := range m.Init {
		f.stmt(s)
	}
	if len(m.Init) > 0 {
		f.blank()
	}
	for i, fn := range m.Funcs {
		if i > 0 {
			f.blank()
		}
		f.line("fn %s%s {", fn.Name, sig(fn.Params, fn.Ret))
		f.indent++
		f.stmts(fn.Body)
		f.indent--
		f.line("}")
	}
	return strings.TrimRight(f.b.String(), "\n") + "\n"
}

func alias(i *ImportStmt) string {
	if i.Alias == "" {
		return ""
	}
	return " as " + i.Alias
}

func sig(params []Param, ret *Type) string {
	ps := make([]string, len(params))
	for i, p := range params {
		if p.Type.Kind == "Any" {
			ps[i] = p.Name
		} else {
			ps[i] = p.Name + ": " + p.Type.String()
		}
	}
	s := "(" + strings.Join(ps, ", ") + ")"
	if ret != nil && ret.Kind != "Nil" {
		s += " -> " + ret.String()
	}
	return s
}

type formatter struct {
	b      strings.Builder
	indent int
	blankP bool
}

func (f *formatter) line(format string, a ...any) {
	f.b.WriteString(strings.Repeat("    ", f.indent))
	fmt.Fprintf(&f.b, format, a...)
	f.b.WriteByte('\n')
	f.blankP = false
}

func (f *formatter) blank() {
	if !f.blankP {
		f.b.WriteByte('\n')
		f.blankP = true
	}
}

func (f *formatter) stmts(b *Block) {
	for _, s := range b.Stmts {
		f.stmt(s)
	}
}

func (f *formatter) blockInline(b *Block) {
	f.indent++
	f.stmts(b)
	f.indent--
}

func (f *formatter) stmt(s Stmt) {
	switch s := s.(type) {
	case *LetStmt:
		if s.Type != nil {
			f.line("let %s: %s = %s", s.Name, s.Type, expr(s.Val))
		} else {
			f.line("let %s = %s", s.Name, expr(s.Val))
		}
	case *AssignStmt:
		f.line("%s = %s", expr(s.Target), expr(s.Val))
	case *ExprStmt:
		f.line("%s", expr(s.X))
	case *ReturnStmt:
		if s.Val == nil {
			f.line("return")
		} else {
			f.line("return %s", expr(s.Val))
		}
	case *BreakStmt:
		f.line("break")
	case *ContinueStmt:
		f.line("continue")
	case *WhileStmt:
		f.line("while %s {", expr(s.Cond))
		f.blockInline(s.Body)
		f.line("}")
	case *ForStmt:
		f.line("for %s in %s {", s.Var, expr(s.Iter))
		f.blockInline(s.Body)
		f.line("}")
	case *IfStmt:
		f.ifChain(s, "if")
	case *Block:
		f.line("{")
		f.blockInline(s)
		f.line("}")
	}
}

func (f *formatter) ifChain(s *IfStmt, kw string) {
	f.line("%s %s {", kw, expr(s.Cond))
	f.blockInline(s.Then)
	if s.Else == nil {
		f.line("}")
		return
	}
	if len(s.Else.Stmts) == 1 {
		if inner, ok := s.Else.Stmts[0].(*IfStmt); ok {
			f.ifChain(inner, "} else if")
			return
		}
	}
	f.line("} else {")
	f.blockInline(s.Else)
	f.line("}")
}

var precOf = map[string]int{"||": 1, "&&": 2, "==": 3, "!=": 3, "<": 4, "<=": 4, ">": 4, ">=": 4, "+": 5, "-": 5, "*": 6, "/": 6, "%": 6}

func expr(e Expr) string {
	return exprPrec(e, 0)
}

func exprPrec(e Expr, outer int) string {
	switch e := e.(type) {
	case *IntLit:
		return strconv.FormatInt(e.V, 10)
	case *StrLit:
		return strconv.Quote(e.V)
	case *BoolLit:
		if e.V {
			return "true"
		}
		return "false"
	case *NilLit:
		return "nil"
	case *Ident:
		return e.Name
	case *ListLit:
		parts := make([]string, len(e.Items))
		for i, it := range e.Items {
			parts[i] = expr(it)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *MapLit:
		parts := make([]string, len(e.Keys))
		for i := range e.Keys {
			parts[i] = expr(e.Keys[i]) + ": " + expr(e.Vals[i])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *Unary:
		return e.Op + exprPrec(e.X, 7)
	case *Binary:
		p := precOf[e.Op]
		s := exprPrec(e.L, p) + " " + e.Op + " " + exprPrec(e.R, p+1)
		if p < outer {
			return "(" + s + ")"
		}
		return s
	case *Call:
		args := make([]string, len(e.Args))
		for i, a := range e.Args {
			args[i] = expr(a)
		}
		return exprPrec(e.Fn, 8) + "(" + strings.Join(args, ", ") + ")"
	case *Index:
		return exprPrec(e.X, 8) + "[" + expr(e.I) + "]"
	case *Field:
		return exprPrec(e.X, 8) + "." + e.Name
	case *FnLit:
		f := &formatter{indent: 1}
		f.stmts(e.Body)
		body := f.b.String()
		if body == "" {
			return "fn" + sig(e.Params, e.Ret) + " {}"
		}
		return "fn" + sig(e.Params, e.Ret) + " {\n" + body + "}"
	case *Spawn:
		return "spawn " + exprPrec(e.X, 7)
	case *Await:
		return "await " + exprPrec(e.X, 7)
	}
	return "?"
}
