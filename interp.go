package tarn

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ----- runtime values -----

// Values are: int64, string, bool, nil, *List, *Map, *Closure, *Task,
// *Builtin, *ToolRef, *ModuleVal.

type List struct{ Items []any }
type Map struct{ M map[string]any }

type Closure struct {
	Params []Param
	Body   *Block
	Env    *Env
	Name   string
}

type Builtin struct {
	Name string
	Fn   func(in *Interp, p Pos, args []any) (any, error)
}

type ToolRef struct{ Decl *ToolDecl }

type ModuleVal struct {
	Name string
	Env  *Env
}

// Task is a spawned call. Tasks run cooperatively: a task starts when
// something awaits it (or the scheduler drains), in spawn order, and runs
// to completion. That makes every Tarn program deterministic.
type Task struct {
	id     int
	fn     any
	args   []any
	done   bool
	result any
	err    error
}

type Env struct {
	vars   map[string]any
	parent *Env
}

func newEnv(parent *Env) *Env { return &Env{vars: map[string]any{}, parent: parent} }

func (e *Env) get(n string) (any, bool) {
	for c := e; c != nil; c = c.parent {
		if v, ok := c.vars[n]; ok {
			return v, true
		}
	}
	return nil, false
}

func (e *Env) set(n string, v any) bool {
	for c := e; c != nil; c = c.parent {
		if _, ok := c.vars[n]; ok {
			c.vars[n] = v
			return true
		}
	}
	return false
}

// ----- control flow signals -----

type ctrl int

const (
	ctrlNone ctrl = iota
	ctrlReturn
	ctrlBreak
	ctrlContinue
)

// ----- interpreter -----

// Interp runs modules. One Interp = one program run.
type Interp struct {
	Out     func(string)
	Tools   *ToolHost
	modules map[string]*ModuleVal // by absolute path
	types   map[string]*ModuleTypes
	queue   []*Task
	nextID  int
	depth   int
	// MaxDepth bounds recursion; MaxSteps bounds total statements (0 = unlimited).
	MaxDepth int
	MaxSteps int
	steps    int
}

func NewInterp() *Interp {
	return &Interp{
		Out:      func(s string) { os.Stdout.WriteString(s) },
		Tools:    NewToolHost(),
		modules:  map[string]*ModuleVal{},
		types:    map[string]*ModuleTypes{},
		MaxDepth: 2000,
	}
}

// RunFile loads, checks and runs a program: module init, then `main()`.
func (in *Interp) RunFile(path string) error {
	mod, err := in.load(path)
	if err != nil {
		return err
	}
	mainFn, ok := mod.Env.vars["main"]
	if !ok {
		return &Error{File: path, Msg: "no `main` function"}
	}
	_, err = in.call(Pos{}, mainFn, nil)
	if err != nil {
		return withFile(err, path)
	}
	return withFile(in.drain(), path)
}

// withFile attaches the program file to runtime errors that lack one.
func withFile(err error, file string) error {
	if e, ok := err.(*Error); ok && e.File == "" {
		e.File = file
	}
	return err
}

// RunSource runs a program given as text (REPL, tests).
func (in *Interp) RunSource(name, src string) error {
	m, err := Parse(name, src)
	if err != nil {
		return err
	}
	mv, err := in.instantiate(m, name)
	if err != nil {
		return err
	}
	if mainFn, ok := mv.Env.vars["main"]; ok {
		if _, err := in.call(Pos{}, mainFn, nil); err != nil {
			return withFile(err, name)
		}
	}
	return withFile(in.drain(), name)
}

// CheckModule type-checks a parsed module (loading imports for their
// types) without running it.
func (in *Interp) CheckModule(m *Module) (*ModuleTypes, []*Error) {
	abs, _ := filepath.Abs(m.File)
	imports := map[string]*ModuleTypes{}
	for _, imp := range m.Imports {
		p := imp.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(filepath.Dir(abs), p)
		}
		if _, err := in.load(p); err != nil {
			if e, ok := err.(*Error); ok {
				return nil, []*Error{e}
			}
			return nil, []*Error{{File: m.File, Pos: imp.P, Msg: err.Error()}}
		}
		absImp, _ := filepath.Abs(p)
		imports[importAlias(imp)] = in.types[absImp]
	}
	for _, t := range m.Tools {
		if err := in.Tools.declare(t); err != nil {
			return nil, []*Error{{File: m.File, Pos: t.P, Msg: err.Error()}}
		}
	}
	return Check(m, imports)
}

func (in *Interp) load(path string) (*ModuleVal, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if mv, ok := in.modules[abs]; ok {
		return mv, nil
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	m, err := Parse(path, string(src))
	if err != nil {
		return nil, err
	}
	return in.instantiate(m, abs)
}

// instantiate resolves imports, type-checks, and runs module init.
func (in *Interp) instantiate(m *Module, abs string) (*ModuleVal, error) {
	// Register before resolving imports so a cycle is detected, not recursed.
	mv := &ModuleVal{Name: m.File}
	in.modules[abs] = mv
	imports := map[string]*ModuleTypes{}
	importVals := map[string]*ModuleVal{}
	for _, imp := range m.Imports {
		p := imp.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(filepath.Dir(abs), p)
		}
		// Cycle guard: a module being loaded is registered before init runs.
		absImp, _ := filepath.Abs(p)
		if mv, ok := in.modules[absImp]; ok && mv.Env == nil {
			return nil, &Error{File: m.File, Pos: imp.P, Msg: "import cycle through " + imp.Path}
		}
		mv, err := in.load(p)
		if err != nil {
			return nil, err
		}
		alias := importAlias(imp)
		imports[alias] = in.types[absImp]
		importVals[alias] = mv
	}
	types, errs := Check(m, imports)
	if len(errs) > 0 {
		delete(in.modules, abs)
		return nil, errs[0]
	}
	in.types[abs] = types
	env := newEnv(nil)
	for name, b := range builtins {
		env.vars[name] = b
	}
	for alias, imv := range importVals {
		env.vars[alias] = imv
	}
	for _, t := range m.Tools {
		if err := in.Tools.declare(t); err != nil {
			return nil, &Error{File: m.File, Pos: t.P, Msg: err.Error()}
		}
	}
	for _, f := range m.Funcs {
		env.vars[f.Name] = &Closure{Params: f.Params, Body: f.Body, Env: env, Name: f.Name}
	}
	mv.Env = env
	for _, s := range m.Init {
		if _, c, err := in.stmt(env, s); err != nil {
			return nil, err
		} else if c != ctrlNone {
			return nil, &Error{File: m.File, Pos: s.spos(), Msg: "return/break/continue at module level"}
		}
	}
	return mv, nil
}

// ----- tasks -----

func (in *Interp) spawn(fn any, args []any) *Task {
	in.nextID++
	t := &Task{id: in.nextID, fn: fn, args: args}
	in.queue = append(in.queue, t)
	return t
}

func (in *Interp) runTask(t *Task) {
	if t.done {
		return
	}
	t.done = true
	t.result, t.err = in.call(Pos{}, t.fn, t.args)
}

// await runs queued tasks in spawn order until `t` has completed.
func (in *Interp) await(t *Task) (any, error) {
	for !t.done && len(in.queue) > 0 {
		next := in.queue[0]
		in.queue = in.queue[1:]
		in.runTask(next)
	}
	if !t.done { // not queued (already dequeued elsewhere): run directly
		in.runTask(t)
	}
	return t.result, t.err
}

// drain finishes every spawned-but-never-awaited task, so side effects
// (tool calls, prints) are never silently lost.
func (in *Interp) drain() error {
	for len(in.queue) > 0 {
		next := in.queue[0]
		in.queue = in.queue[1:]
		in.runTask(next)
		if next.err != nil {
			return next.err
		}
	}
	return nil
}

// ----- calls -----

func (in *Interp) call(p Pos, fn any, args []any) (any, error) {
	switch f := fn.(type) {
	case *Closure:
		if len(args) != len(f.Params) {
			return nil, errAt(p, "%s expects %d argument(s), got %d", f.Name, len(f.Params), len(args))
		}
		in.depth++
		if in.depth > in.MaxDepth {
			in.depth--
			return nil, errAt(p, "stack overflow (call depth %d)", in.MaxDepth)
		}
		env := newEnv(f.Env)
		for i, pr := range f.Params {
			if err := checkValue(pr.Type, args[i]); err != nil {
				in.depth--
				return nil, errAt(p, "argument %q: %v", pr.Name, err)
			}
			env.vars[pr.Name] = args[i]
		}
		v, c, err := in.block(env, f.Body)
		in.depth--
		if err != nil {
			return nil, err
		}
		if c == ctrlReturn {
			return v, nil
		}
		return nil, nil
	case *Builtin:
		return f.Fn(in, p, args)
	case *ToolRef:
		return in.Tools.Call(f.Decl, p, args)
	}
	return nil, errAt(p, "cannot call a value of type %s", typeName(fn))
}

// checkValue enforces declared parameter types at run time for values
// that came through `Any` (the checker cannot see those).
func checkValue(t *Type, v any) error {
	if t == nil || t.Kind == "Any" {
		return nil
	}
	got := typeName(v)
	switch t.Kind {
	case "Int", "Str", "Bool", "Nil":
		if got != t.Kind {
			return fmt.Errorf("expected %s, got %s", t, got)
		}
	case "List":
		l, ok := v.(*List)
		if !ok {
			return fmt.Errorf("expected %s, got %s", t, got)
		}
		for _, it := range l.Items {
			if err := checkValue(t.Elem, it); err != nil {
				return err
			}
		}
	case "Map":
		m, ok := v.(*Map)
		if !ok {
			return fmt.Errorf("expected %s, got %s", t, got)
		}
		for _, it := range m.M {
			if err := checkValue(t.Elem, it); err != nil {
				return err
			}
		}
	case "Fn":
		if got != "Fn" {
			return fmt.Errorf("expected %s, got %s", t, got)
		}
	case "Task":
		if got != "Task" {
			return fmt.Errorf("expected %s, got %s", t, got)
		}
	}
	return nil
}

func typeName(v any) string {
	switch v.(type) {
	case int64:
		return "Int"
	case string:
		return "Str"
	case bool:
		return "Bool"
	case nil:
		return "Nil"
	case *List:
		return "List"
	case *Map:
		return "Map"
	case *Closure, *Builtin, *ToolRef:
		return "Fn"
	case *Task:
		return "Task"
	case *ModuleVal:
		return "Module"
	}
	return "?"
}

// ----- statements -----

func (in *Interp) block(env *Env, b *Block) (any, ctrl, error) {
	inner := newEnv(env)
	for _, s := range b.Stmts {
		v, c, err := in.stmt(inner, s)
		if err != nil {
			return nil, ctrlNone, err
		}
		if c != ctrlNone {
			return v, c, nil
		}
	}
	return nil, ctrlNone, nil
}

func (in *Interp) stmt(env *Env, s Stmt) (any, ctrl, error) {
	in.steps++
	if in.MaxSteps > 0 && in.steps > in.MaxSteps {
		return nil, ctrlNone, errAt(s.spos(), "step limit reached (%d)", in.MaxSteps)
	}
	switch s := s.(type) {
	case *LetStmt:
		v, err := in.expr(env, s.Val)
		if err != nil {
			return nil, ctrlNone, err
		}
		if s.Type != nil {
			if err := checkValue(s.Type, v); err != nil {
				return nil, ctrlNone, errAt(s.P, "let %s: %v", s.Name, err)
			}
		}
		env.vars[s.Name] = v
	case *AssignStmt:
		v, err := in.expr(env, s.Val)
		if err != nil {
			return nil, ctrlNone, err
		}
		switch t := s.Target.(type) {
		case *Ident:
			if !env.set(t.Name, v) {
				return nil, ctrlNone, errAt(t.P, "unknown variable %q", t.Name)
			}
		case *Index:
			c, err := in.expr(env, t.X)
			if err != nil {
				return nil, ctrlNone, err
			}
			i, err := in.expr(env, t.I)
			if err != nil {
				return nil, ctrlNone, err
			}
			switch c := c.(type) {
			case *List:
				idx, ok := i.(int64)
				if !ok || idx < 0 || int(idx) >= len(c.Items) {
					return nil, ctrlNone, errAt(t.P, "index %v out of range (len %d)", i, len(c.Items))
				}
				c.Items[idx] = v
			case *Map:
				k, ok := i.(string)
				if !ok {
					return nil, ctrlNone, errAt(t.P, "map key must be Str")
				}
				c.M[k] = v
			default:
				return nil, ctrlNone, errAt(t.P, "cannot index into %s", typeName(c))
			}
		}
	case *ExprStmt:
		_, err := in.expr(env, s.X)
		return nil, ctrlNone, err
	case *IfStmt:
		c, err := in.expr(env, s.Cond)
		if err != nil {
			return nil, ctrlNone, err
		}
		b, ok := c.(bool)
		if !ok {
			return nil, ctrlNone, errAt(s.Cond.pos(), "condition must be Bool, got %s", typeName(c))
		}
		if b {
			return in.block(env, s.Then)
		} else if s.Else != nil {
			return in.block(env, s.Else)
		}
	case *WhileStmt:
		for {
			in.steps++
			if in.MaxSteps > 0 && in.steps > in.MaxSteps {
				return nil, ctrlNone, errAt(s.P, "step limit reached (%d)", in.MaxSteps)
			}
			c, err := in.expr(env, s.Cond)
			if err != nil {
				return nil, ctrlNone, err
			}
			if b, ok := c.(bool); !ok {
				return nil, ctrlNone, errAt(s.Cond.pos(), "condition must be Bool, got %s", typeName(c))
			} else if !b {
				break
			}
			v, ctl, err := in.block(env, s.Body)
			if err != nil {
				return nil, ctrlNone, err
			}
			if ctl == ctrlBreak {
				break
			}
			if ctl == ctrlReturn {
				return v, ctl, nil
			}
		}
	case *ForStmt:
		it, err := in.expr(env, s.Iter)
		if err != nil {
			return nil, ctrlNone, err
		}
		var items []any
		switch it := it.(type) {
		case *List:
			items = append(items, it.Items...)
		case *Map:
			for _, k := range sortedKeys(it) {
				items = append(items, k)
			}
		case string:
			for _, r := range it {
				items = append(items, string(r))
			}
		default:
			return nil, ctrlNone, errAt(s.Iter.pos(), "cannot iterate over %s", typeName(it))
		}
		for _, item := range items {
			in.steps++
			if in.MaxSteps > 0 && in.steps > in.MaxSteps {
				return nil, ctrlNone, errAt(s.P, "step limit reached (%d)", in.MaxSteps)
			}
			inner := newEnv(env)
			inner.vars[s.Var] = item
			v, ctl, err := in.block(inner, s.Body)
			if err != nil {
				return nil, ctrlNone, err
			}
			if ctl == ctrlBreak {
				break
			}
			if ctl == ctrlReturn {
				return v, ctl, nil
			}
		}
	case *ReturnStmt:
		if s.Val == nil {
			return nil, ctrlReturn, nil
		}
		v, err := in.expr(env, s.Val)
		if err != nil {
			return nil, ctrlNone, err
		}
		return v, ctrlReturn, nil
	case *BreakStmt:
		return nil, ctrlBreak, nil
	case *ContinueStmt:
		return nil, ctrlContinue, nil
	case *Block:
		return in.block(env, s)
	}
	return nil, ctrlNone, nil
}

func sortedKeys(m *Map) []string {
	ks := make([]string, 0, len(m.M))
	for k := range m.M {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// ----- expressions -----

func (in *Interp) expr(env *Env, e Expr) (any, error) {
	switch e := e.(type) {
	case *IntLit:
		return e.V, nil
	case *StrLit:
		return e.V, nil
	case *BoolLit:
		return e.V, nil
	case *NilLit:
		return nil, nil
	case *Ident:
		v, ok := env.get(e.Name)
		if !ok {
			return nil, errAt(e.P, "unknown name %q", e.Name)
		}
		return v, nil
	case *ListLit:
		l := &List{}
		for _, it := range e.Items {
			v, err := in.expr(env, it)
			if err != nil {
				return nil, err
			}
			l.Items = append(l.Items, v)
		}
		return l, nil
	case *MapLit:
		m := &Map{M: map[string]any{}}
		for i := range e.Keys {
			k, err := in.expr(env, e.Keys[i])
			if err != nil {
				return nil, err
			}
			ks, ok := k.(string)
			if !ok {
				return nil, errAt(e.Keys[i].pos(), "map key must be Str")
			}
			v, err := in.expr(env, e.Vals[i])
			if err != nil {
				return nil, err
			}
			m.M[ks] = v
		}
		return m, nil
	case *Unary:
		x, err := in.expr(env, e.X)
		if err != nil {
			return nil, err
		}
		switch e.Op {
		case "-":
			n, ok := x.(int64)
			if !ok {
				return nil, errAt(e.P, "unary - needs Int, got %s", typeName(x))
			}
			return -n, nil
		default:
			b, ok := x.(bool)
			if !ok {
				return nil, errAt(e.P, "! needs Bool, got %s", typeName(x))
			}
			return !b, nil
		}
	case *Binary:
		return in.binary(env, e)
	case *Index:
		x, err := in.expr(env, e.X)
		if err != nil {
			return nil, err
		}
		i, err := in.expr(env, e.I)
		if err != nil {
			return nil, err
		}
		switch x := x.(type) {
		case *List:
			idx, ok := i.(int64)
			if !ok {
				return nil, errAt(e.P, "list index must be Int")
			}
			if idx < 0 || int(idx) >= len(x.Items) {
				return nil, errAt(e.P, "index %d out of range (len %d)", idx, len(x.Items))
			}
			return x.Items[idx], nil
		case *Map:
			k, ok := i.(string)
			if !ok {
				return nil, errAt(e.P, "map key must be Str")
			}
			v, ok := x.M[k]
			if !ok {
				return nil, errAt(e.P, "missing key %q", k)
			}
			return v, nil
		case string:
			idx, ok := i.(int64)
			if !ok {
				return nil, errAt(e.P, "string index must be Int")
			}
			rs := []rune(x)
			if idx < 0 || int(idx) >= len(rs) {
				return nil, errAt(e.P, "index %d out of range (len %d)", idx, len(rs))
			}
			return string(rs[idx]), nil
		}
		return nil, errAt(e.P, "cannot index into %s", typeName(x))
	case *Field:
		if id, ok := e.X.(*Ident); ok {
			if d := in.Tools.lookup(id.Name + "." + e.Name); d != nil {
				return &ToolRef{Decl: d}, nil
			}
		}
		x, err := in.expr(env, e.X)
		if err != nil {
			return nil, err
		}
		mv, ok := x.(*ModuleVal)
		if !ok {
			return nil, errAt(e.P, "field access on %s", typeName(x))
		}
		v, ok := mv.Env.vars[e.Name]
		if !ok {
			return nil, errAt(e.P, "module has no member %q", e.Name)
		}
		return v, nil
	case *Call:
		fn, err := in.expr(env, e.Fn)
		if err != nil {
			return nil, err
		}
		args := make([]any, len(e.Args))
		for i, a := range e.Args {
			if args[i], err = in.expr(env, a); err != nil {
				return nil, err
			}
		}
		return in.call(e.P, fn, args)
	case *FnLit:
		return &Closure{Params: e.Params, Body: e.Body, Env: env, Name: "fn"}, nil
	case *Spawn:
		c := e.X.(*Call)
		fn, err := in.expr(env, c.Fn)
		if err != nil {
			return nil, err
		}
		args := make([]any, len(c.Args))
		for i, a := range c.Args {
			if args[i], err = in.expr(env, a); err != nil {
				return nil, err
			}
		}
		return in.spawn(fn, args), nil
	case *Await:
		x, err := in.expr(env, e.X)
		if err != nil {
			return nil, err
		}
		switch x := x.(type) {
		case *Task:
			return in.await(x)
		case *List:
			out := &List{}
			for _, it := range x.Items {
				t, ok := it.(*Task)
				if !ok {
					return nil, errAt(e.P, "await list must contain tasks, found %s", typeName(it))
				}
				v, err := in.await(t)
				if err != nil {
					return nil, err
				}
				out.Items = append(out.Items, v)
			}
			return out, nil
		}
		return nil, errAt(e.P, "await needs a Task, got %s", typeName(x))
	}
	return nil, errAt(e.pos(), "unsupported expression")
}

func (in *Interp) binary(env *Env, e *Binary) (any, error) {
	l, err := in.expr(env, e.L)
	if err != nil {
		return nil, err
	}
	// Short-circuit.
	if e.Op == "&&" || e.Op == "||" {
		lb, ok := l.(bool)
		if !ok {
			return nil, errAt(e.L.pos(), "%s needs Bool operands", e.Op)
		}
		if e.Op == "&&" && !lb {
			return false, nil
		}
		if e.Op == "||" && lb {
			return true, nil
		}
		r, err := in.expr(env, e.R)
		if err != nil {
			return nil, err
		}
		rb, ok := r.(bool)
		if !ok {
			return nil, errAt(e.R.pos(), "%s needs Bool operands", e.Op)
		}
		return rb, nil
	}
	r, err := in.expr(env, e.R)
	if err != nil {
		return nil, err
	}
	switch e.Op {
	case "==":
		return Equal(l, r), nil
	case "!=":
		return !Equal(l, r), nil
	}
	switch a := l.(type) {
	case int64:
		b, ok := r.(int64)
		if !ok {
			return nil, errAt(e.P, "cannot apply %s to Int and %s", e.Op, typeName(r))
		}
		switch e.Op {
		case "+":
			return a + b, nil
		case "-":
			return a - b, nil
		case "*":
			return a * b, nil
		case "/":
			if b == 0 {
				return nil, errAt(e.P, "division by zero")
			}
			return a / b, nil
		case "%":
			if b == 0 {
				return nil, errAt(e.P, "division by zero")
			}
			return a % b, nil
		case "<":
			return a < b, nil
		case "<=":
			return a <= b, nil
		case ">":
			return a > b, nil
		case ">=":
			return a >= b, nil
		}
	case string:
		b, ok := r.(string)
		if !ok {
			return nil, errAt(e.P, "cannot apply %s to Str and %s", e.Op, typeName(r))
		}
		switch e.Op {
		case "+":
			return a + b, nil
		case "<":
			return a < b, nil
		case "<=":
			return a <= b, nil
		case ">":
			return a > b, nil
		case ">=":
			return a >= b, nil
		}
	case *List:
		b, ok := r.(*List)
		if ok && e.Op == "+" {
			out := &List{Items: append(append([]any{}, a.Items...), b.Items...)}
			return out, nil
		}
	}
	return nil, errAt(e.P, "cannot apply %s to %s and %s", e.Op, typeName(l), typeName(r))
}

// Equal is structural equality for Tarn values.
func Equal(a, b any) bool {
	switch x := a.(type) {
	case *List:
		y, ok := b.(*List)
		if !ok || len(x.Items) != len(y.Items) {
			return false
		}
		for i := range x.Items {
			if !Equal(x.Items[i], y.Items[i]) {
				return false
			}
		}
		return true
	case *Map:
		y, ok := b.(*Map)
		if !ok || len(x.M) != len(y.M) {
			return false
		}
		for k, v := range x.M {
			if w, ok := y.M[k]; !ok || !Equal(v, w) {
				return false
			}
		}
		return true
	}
	return a == b
}

// Show renders a value the way `print` does.
func Show(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case string:
		return x
	case *List:
		parts := make([]string, len(x.Items))
		for i, it := range x.Items {
			parts[i] = showInner(it)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *Map:
		parts := []string{}
		for _, k := range sortedKeys(x) {
			parts = append(parts, fmt.Sprintf("%q: %s", k, showInner(x.M[k])))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case *Closure:
		return "<fn " + x.Name + ">"
	case *Builtin:
		return "<builtin " + x.Name + ">"
	case *ToolRef:
		return "<tool " + x.Decl.NS + "." + x.Decl.Name + ">"
	case *Task:
		return fmt.Sprintf("<task %d>", x.id)
	case *ModuleVal:
		return "<module " + x.Name + ">"
	}
	return fmt.Sprint(v)
}

func showInner(v any) string {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	return Show(v)
}
