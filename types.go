package tarn

import "fmt"

// Checker is a gradual static checker: everything annotated is enforced,
// everything else is `Any`. Tool calls are always strictly typed.
type Checker struct {
	file    string
	tools   map[string]*ToolDecl // "ns.name"
	modules map[string]*ModuleTypes
	globals map[string]*Type
	errs    []*Error
}

// ModuleTypes is the exported type environment of a checked module.
type ModuleTypes struct {
	Funcs map[string]*Type
}

type scope struct {
	vars   map[string]*Type
	parent *scope
	ret    *Type
	inLoop bool
}

func (s *scope) lookup(n string) (*Type, bool) {
	for c := s; c != nil; c = c.parent {
		if t, ok := c.vars[n]; ok {
			return t, true
		}
	}
	return nil, false
}

// Compatible reports whether a value of type `have` can be used where
// `want` is expected. Any matches everything.
func Compatible(want, have *Type) bool {
	if want == nil || have == nil || want.Kind == "Any" || have.Kind == "Any" {
		return true
	}
	if want.Kind != have.Kind {
		return false
	}
	switch want.Kind {
	case "List", "Map", "Task":
		return Compatible(want.Elem, have.Elem)
	case "Fn":
		if len(want.Params) != len(have.Params) {
			return false
		}
		for i := range want.Params {
			if !Compatible(want.Params[i], have.Params[i]) {
				return false
			}
		}
		return Compatible(want.Ret, have.Ret)
	}
	return true
}

func fnType(params []Param, ret *Type) *Type {
	t := &Type{Kind: "Fn", Ret: ret}
	for _, p := range params {
		t.Params = append(t.Params, p.Type)
	}
	return t
}

// Check type-checks a module given the types of its imported modules.
func Check(m *Module, imports map[string]*ModuleTypes) (*ModuleTypes, []*Error) {
	c := &Checker{file: m.File, tools: map[string]*ToolDecl{}, modules: imports, globals: map[string]*Type{}}
	for _, t := range m.Tools {
		c.tools[t.NS+"."+t.Name] = t
	}
	for name, sig := range builtinSigs {
		c.globals[name] = sig
	}
	exported := &ModuleTypes{Funcs: map[string]*Type{}}
	for _, f := range m.Funcs {
		if _, dup := exported.Funcs[f.Name]; dup {
			c.err(f.P, "function %q is defined twice", f.Name)
		}
		ft := fnType(f.Params, f.Ret)
		exported.Funcs[f.Name] = ft
		c.globals[f.Name] = ft
	}
	for _, imp := range m.Imports {
		c.globals[importAlias(imp)] = &Type{Kind: "Module"}
	}
	top := &scope{vars: c.globals, ret: TNil_}
	for _, s := range m.Init {
		c.stmt(top, s)
	}
	for _, f := range m.Funcs {
		c.fn(top, f.Params, f.Ret, f.Body, f.P)
	}
	return exported, c.errs
}

func (c *Checker) err(p Pos, f string, a ...any) {
	c.errs = append(c.errs, &Error{Pos: p, Msg: fmt.Sprintf(f, a...), File: c.file})
}

func (c *Checker) fn(parent *scope, params []Param, ret *Type, body *Block, p Pos) {
	s := &scope{vars: map[string]*Type{}, parent: parent, ret: ret}
	for _, pr := range params {
		if _, dup := s.vars[pr.Name]; dup {
			c.err(pr.P, "duplicate parameter %q", pr.Name)
		}
		s.vars[pr.Name] = pr.Type
	}
	c.block(s, body)
	if ret.Kind != "Nil" && ret.Kind != "Any" && !returns(body) {
		c.err(p, "function returns %s but not every path returns a value", ret)
	}
}

// returns reports whether a block returns on every path.
func returns(b *Block) bool {
	for _, s := range b.Stmts {
		switch st := s.(type) {
		case *ReturnStmt:
			return true
		case *IfStmt:
			if st.Else != nil && returns(st.Then) && returns(st.Else) {
				return true
			}
		}
	}
	return false
}

func (c *Checker) block(parent *scope, b *Block) {
	s := &scope{vars: map[string]*Type{}, parent: parent, ret: parent.ret, inLoop: parent.inLoop}
	for _, st := range b.Stmts {
		c.stmt(s, st)
	}
}

func (c *Checker) stmt(s *scope, st Stmt) {
	switch st := st.(type) {
	case *LetStmt:
		t := c.expr(s, st.Val)
		if st.Type != nil {
			if !Compatible(st.Type, t) {
				c.err(st.Val.pos(), "cannot initialise %s with a value of type %s", st.Type, t)
			}
			t = st.Type
		}
		if _, dup := s.vars[st.Name]; dup {
			c.err(st.P, "%q is already declared in this scope", st.Name)
		}
		s.vars[st.Name] = t
	case *AssignStmt:
		v := c.expr(s, st.Val)
		switch tg := st.Target.(type) {
		case *Ident:
			t, ok := s.lookup(tg.Name)
			if !ok {
				c.err(tg.P, "unknown variable %q", tg.Name)
				return
			}
			if !Compatible(t, v) {
				c.err(st.Val.pos(), "cannot assign %s to %q of type %s", v, tg.Name, t)
			}
		case *Index:
			ct := c.expr(s, tg.X)
			it := c.expr(s, tg.I)
			switch ct.Kind {
			case "List":
				c.want(tg.I.pos(), TInt_, it, "list index")
				c.want(st.Val.pos(), ct.Elem, v, "list element")
			case "Map":
				c.want(tg.I.pos(), TStr_, it, "map key")
				c.want(st.Val.pos(), ct.Elem, v, "map value")
			case "Any":
			default:
				c.err(tg.P, "cannot index into %s", ct)
			}
		}
	case *ExprStmt:
		c.expr(s, st.X)
	case *IfStmt:
		c.want(st.Cond.pos(), TBool_, c.expr(s, st.Cond), "condition")
		c.block(s, st.Then)
		if st.Else != nil {
			c.block(s, st.Else)
		}
	case *WhileStmt:
		c.want(st.Cond.pos(), TBool_, c.expr(s, st.Cond), "condition")
		inner := &scope{vars: map[string]*Type{}, parent: s, ret: s.ret, inLoop: true}
		c.block(inner, st.Body)
	case *ForStmt:
		it := c.expr(s, st.Iter)
		var vt *Type
		switch it.Kind {
		case "List":
			vt = it.Elem
		case "Map", "Str":
			vt = TStr_
		case "Any":
			vt = TAny_
		default:
			c.err(st.Iter.pos(), "cannot iterate over %s", it)
			vt = TAny_
		}
		inner := &scope{vars: map[string]*Type{st.Var: vt}, parent: s, ret: s.ret, inLoop: true}
		c.block(inner, st.Body)
	case *ReturnStmt:
		if st.Val == nil {
			if s.ret.Kind != "Nil" && s.ret.Kind != "Any" {
				c.err(st.P, "return without a value in a function returning %s", s.ret)
			}
			return
		}
		t := c.expr(s, st.Val)
		if s.ret.Kind == "Nil" {
			c.err(st.Val.pos(), "returning a value from a function that returns Nil")
		} else if !Compatible(s.ret, t) {
			c.err(st.Val.pos(), "return type mismatch: expected %s, found %s", s.ret, t)
		}
	case *BreakStmt:
		if !s.inLoop {
			c.err(st.P, "break outside a loop")
		}
	case *ContinueStmt:
		if !s.inLoop {
			c.err(st.P, "continue outside a loop")
		}
	case *Block:
		c.block(s, st)
	}
}

func (c *Checker) want(p Pos, want, have *Type, what string) {
	if !Compatible(want, have) {
		c.err(p, "%s must be %s, found %s", what, want, have)
	}
}

func (c *Checker) expr(s *scope, e Expr) *Type {
	switch e := e.(type) {
	case *IntLit:
		return TInt_
	case *StrLit:
		return TStr_
	case *BoolLit:
		return TBool_
	case *NilLit:
		return TNil_
	case *Ident:
		t, ok := s.lookup(e.Name)
		if !ok {
			c.err(e.P, "unknown name %q", e.Name)
			return TAny_
		}
		return t
	case *ListLit:
		var el *Type
		for _, it := range e.Items {
			t := c.expr(s, it)
			if el == nil {
				el = t
			} else if !Compatible(el, t) {
				el = TAny_
			}
		}
		if el == nil {
			el = TAny_
		}
		return ListOf(el)
	case *MapLit:
		var el *Type
		for i := range e.Keys {
			c.want(e.Keys[i].pos(), TStr_, c.expr(s, e.Keys[i]), "map key")
			t := c.expr(s, e.Vals[i])
			if el == nil {
				el = t
			} else if !Compatible(el, t) {
				el = TAny_
			}
		}
		if el == nil {
			el = TAny_
		}
		return MapOf(el)
	case *Unary:
		t := c.expr(s, e.X)
		if e.Op == "-" {
			c.want(e.P, TInt_, t, "operand of unary -")
			return TInt_
		}
		c.want(e.P, TBool_, t, "operand of !")
		return TBool_
	case *Binary:
		l := c.expr(s, e.L)
		r := c.expr(s, e.R)
		switch e.Op {
		case "+":
			if l.Kind == "Any" || r.Kind == "Any" {
				return TAny_
			}
			if l.Kind == r.Kind && (l.Kind == "Int" || l.Kind == "Str" || l.Kind == "List") {
				return l
			}
			c.err(e.P, "cannot add %s and %s", l, r)
			return TAny_
		case "-", "*", "/", "%":
			c.want(e.L.pos(), TInt_, l, "operand of "+e.Op)
			c.want(e.R.pos(), TInt_, r, "operand of "+e.Op)
			return TInt_
		case "<", "<=", ">", ">=":
			if l.Kind != "Any" && r.Kind != "Any" && !(l.Kind == r.Kind && (l.Kind == "Int" || l.Kind == "Str")) {
				c.err(e.P, "cannot compare %s and %s", l, r)
			}
			return TBool_
		case "==", "!=":
			if !Compatible(l, r) {
				c.err(e.P, "cannot compare %s with %s", l, r)
			}
			return TBool_
		case "&&", "||":
			c.want(e.L.pos(), TBool_, l, "operand of "+e.Op)
			c.want(e.R.pos(), TBool_, r, "operand of "+e.Op)
			return TBool_
		}
	case *Index:
		x := c.expr(s, e.X)
		i := c.expr(s, e.I)
		switch x.Kind {
		case "List":
			c.want(e.I.pos(), TInt_, i, "list index")
			return x.Elem
		case "Map":
			c.want(e.I.pos(), TStr_, i, "map key")
			return x.Elem
		case "Str":
			c.want(e.I.pos(), TInt_, i, "string index")
			return TStr_
		case "Any":
			return TAny_
		}
		c.err(e.P, "cannot index into %s", x)
		return TAny_
	case *Field:
		if id, ok := e.X.(*Ident); ok {
			if t, ok := c.tools[id.Name+"."+e.Name]; ok {
				return fnType(t.Params, t.Ret)
			}
			if mt, ok := c.modules[id.Name]; ok {
				if ft, ok := mt.Funcs[e.Name]; ok {
					return ft
				}
				c.err(e.P, "module %q has no function %q", id.Name, e.Name)
				return TAny_
			}
			if _, isMod := s.lookup(id.Name); !isMod {
				c.err(e.P, "unknown tool or module %q (declare it with `tool %s.%s(...)` or import a module)", id.Name, id.Name, e.Name)
				return TAny_
			}
		}
		c.err(e.P, "field access is only valid on tools and modules")
		return TAny_
	case *Call:
		ft := c.expr(s, e.Fn)
		args := make([]*Type, len(e.Args))
		for i, a := range e.Args {
			args[i] = c.expr(s, a)
		}
		if ft.Kind == "Any" {
			return TAny_
		}
		if ft.Kind != "Fn" {
			c.err(e.P, "cannot call a value of type %s", ft)
			return TAny_
		}
		// Builtins with variadic/polymorphic signatures use Params == nil.
		if ft.Params != nil {
			if len(ft.Params) != len(args) {
				c.err(e.P, "expected %d argument(s), got %d", len(ft.Params), len(args))
			}
			for i := 0; i < len(args) && i < len(ft.Params); i++ {
				if !Compatible(ft.Params[i], args[i]) {
					c.err(e.Args[i].pos(), "argument %d must be %s, found %s", i+1, ft.Params[i], args[i])
				}
			}
		}
		if ft.Ret == nil {
			return TAny_
		}
		return ft.Ret
	case *FnLit:
		c.fn(s, e.Params, e.Ret, e.Body, e.P)
		return fnType(e.Params, e.Ret)
	case *Spawn:
		t := c.expr(s, e.X)
		return TaskOf(t)
	case *Await:
		t := c.expr(s, e.X)
		switch {
		case t.Kind == "Task":
			return t.Elem
		case t.Kind == "List" && t.Elem.Kind == "Task":
			return ListOf(t.Elem.Elem)
		case t.Kind == "Any" || t.Kind == "List" && t.Elem.Kind == "Any":
			return TAny_
		}
		c.err(e.P, "await needs a Task or List[Task], found %s", t)
		return TAny_
	}
	return TAny_
}

// builtinSigs: nil Params means "any arguments" (polymorphic); nil Ret means Any.
var builtinSigs = map[string]*Type{
	"print":      {Kind: "Fn", Ret: TNil_},
	"len":        {Kind: "Fn", Ret: TInt_},
	"str":        {Kind: "Fn", Ret: TStr_},
	"int":        {Kind: "Fn", Params: []*Type{TAny_}, Ret: TInt_},
	"type":       {Kind: "Fn", Params: []*Type{TAny_}, Ret: TStr_},
	"push":       {Kind: "Fn", Ret: TNil_},
	"pop":        {Kind: "Fn"},
	"keys":       {Kind: "Fn", Ret: ListOf(TStr_)},
	"values":     {Kind: "Fn"},
	"has":        {Kind: "Fn", Ret: TBool_},
	"range":      {Kind: "Fn", Ret: ListOf(TInt_)},
	"join":       {Kind: "Fn", Ret: TStr_},
	"split":      {Kind: "Fn", Params: []*Type{TStr_, TStr_}, Ret: ListOf(TStr_)},
	"contains":   {Kind: "Fn", Ret: TBool_},
	"upper":      {Kind: "Fn", Params: []*Type{TStr_}, Ret: TStr_},
	"lower":      {Kind: "Fn", Params: []*Type{TStr_}, Ret: TStr_},
	"trim":       {Kind: "Fn", Params: []*Type{TStr_}, Ret: TStr_},
	"map":        {Kind: "Fn"},
	"filter":     {Kind: "Fn"},
	"reduce":     {Kind: "Fn"},
	"sort":       {Kind: "Fn"},
	"slice":      {Kind: "Fn"},
	"all":        {Kind: "Fn"},
	"assert":     {Kind: "Fn", Ret: TNil_},
	"error":      {Kind: "Fn", Params: []*Type{TStr_}, Ret: TNil_},
	"min":        {Kind: "Fn", Params: []*Type{TInt_, TInt_}, Ret: TInt_},
	"max":        {Kind: "Fn", Params: []*Type{TInt_, TInt_}, Ret: TInt_},
	"abs":        {Kind: "Fn", Params: []*Type{TInt_}, Ret: TInt_},
	"json":       {Kind: "Fn", Ret: TStr_},
	"parse_json": {Kind: "Fn", Params: []*Type{TStr_}},
	"par_map":    {Kind: "Fn"},
}

func importAlias(i *ImportStmt) string {
	if i.Alias != "" {
		return i.Alias
	}
	p := i.Path
	for k := len(p) - 1; k >= 0; k-- {
		if p[k] == '/' || p[k] == '\\' {
			p = p[k+1:]
			break
		}
	}
	for k := 0; k < len(p); k++ {
		if p[k] == '.' {
			return p[:k]
		}
	}
	return p
}
