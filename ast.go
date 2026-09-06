package tarn

// ----- types -----

// Type is a Tarn static type. Any is the gradual escape hatch.
type Type struct {
	Kind   string // Int Str Bool Nil Any List Map Fn Task
	Elem   *Type  // List/Map/Task element
	Params []*Type
	Ret    *Type
}

var (
	TInt_  = &Type{Kind: "Int"}
	TStr_  = &Type{Kind: "Str"}
	TBool_ = &Type{Kind: "Bool"}
	TNil_  = &Type{Kind: "Nil"}
	TAny_  = &Type{Kind: "Any"}
)

func ListOf(t *Type) *Type { return &Type{Kind: "List", Elem: t} }
func MapOf(t *Type) *Type  { return &Type{Kind: "Map", Elem: t} }
func TaskOf(t *Type) *Type { return &Type{Kind: "Task", Elem: t} }

func (t *Type) String() string {
	switch t.Kind {
	case "List", "Map", "Task":
		return t.Kind + "[" + t.Elem.String() + "]"
	case "Fn":
		s := "Fn("
		for i, p := range t.Params {
			if i > 0 {
				s += ", "
			}
			s += p.String()
		}
		return s + ") -> " + t.Ret.String()
	}
	return t.Kind
}

// ----- expressions -----

type Expr interface{ pos() Pos }

type (
	IntLit struct {
		P Pos
		V int64
	}
	StrLit struct {
		P Pos
		V string
	}
	BoolLit struct {
		P Pos
		V bool
	}
	NilLit struct{ P Pos }
	Ident  struct {
		P    Pos
		Name string
	}
	ListLit struct {
		P     Pos
		Items []Expr
	}
	MapLit struct {
		P    Pos
		Keys []Expr
		Vals []Expr
	}
	Unary struct {
		P  Pos
		Op string
		X  Expr
	}
	Binary struct {
		P    Pos
		Op   string
		L, R Expr
	}
	Call struct {
		P    Pos
		Fn   Expr
		Args []Expr
	}
	Index struct {
		P    Pos
		X, I Expr
	}
	// Field is `a.b` — module member or tool namespace (`http.get`).
	Field struct {
		P    Pos
		X    Expr
		Name string
	}
	FnLit struct {
		P      Pos
		Params []Param
		Ret    *Type
		Body   *Block
	}
	Spawn struct {
		P Pos
		X Expr // must be a Call
	}
	Await struct {
		P Pos
		X Expr
	}
)

func (e *IntLit) pos() Pos  { return e.P }
func (e *StrLit) pos() Pos  { return e.P }
func (e *BoolLit) pos() Pos { return e.P }
func (e *NilLit) pos() Pos  { return e.P }
func (e *Ident) pos() Pos   { return e.P }
func (e *ListLit) pos() Pos { return e.P }
func (e *MapLit) pos() Pos  { return e.P }
func (e *Unary) pos() Pos   { return e.P }
func (e *Binary) pos() Pos  { return e.P }
func (e *Call) pos() Pos    { return e.P }
func (e *Index) pos() Pos   { return e.P }
func (e *Field) pos() Pos   { return e.P }
func (e *FnLit) pos() Pos   { return e.P }
func (e *Spawn) pos() Pos   { return e.P }
func (e *Await) pos() Pos   { return e.P }

// ----- statements -----

type Param struct {
	Name string
	Type *Type
	P    Pos
}

type Stmt interface{ spos() Pos }

type (
	Block struct {
		P     Pos
		Stmts []Stmt
	}
	LetStmt struct {
		P    Pos
		Name string
		Type *Type
		Val  Expr
	}
	AssignStmt struct {
		P      Pos
		Target Expr // Ident or Index
		Val    Expr
	}
	ExprStmt struct {
		P Pos
		X Expr
	}
	IfStmt struct {
		P    Pos
		Cond Expr
		Then *Block
		Else *Block
	}
	ForStmt struct {
		P    Pos
		Var  string
		Iter Expr
		Body *Block
	}
	WhileStmt struct {
		P    Pos
		Cond Expr
		Body *Block
	}
	ReturnStmt struct {
		P   Pos
		Val Expr
	}
	BreakStmt    struct{ P Pos }
	ContinueStmt struct{ P Pos }
	FnDecl       struct {
		P      Pos
		Name   string
		Params []Param
		Ret    *Type
		Body   *Block
		Pub    bool
	}
	// ToolDecl: `tool http.get(url: Str) -> Str` binds a typed host tool.
	ToolDecl struct {
		P      Pos
		NS     string
		Name   string
		Params []Param
		Ret    *Type
	}
	ImportStmt struct {
		P     Pos
		Path  string
		Alias string
	}
)

func (s *Block) spos() Pos        { return s.P }
func (s *LetStmt) spos() Pos      { return s.P }
func (s *AssignStmt) spos() Pos   { return s.P }
func (s *ExprStmt) spos() Pos     { return s.P }
func (s *IfStmt) spos() Pos       { return s.P }
func (s *ForStmt) spos() Pos      { return s.P }
func (s *WhileStmt) spos() Pos    { return s.P }
func (s *ReturnStmt) spos() Pos   { return s.P }
func (s *BreakStmt) spos() Pos    { return s.P }
func (s *ContinueStmt) spos() Pos { return s.P }
func (s *FnDecl) spos() Pos       { return s.P }
func (s *ToolDecl) spos() Pos     { return s.P }
func (s *ImportStmt) spos() Pos   { return s.P }

// Module is one parsed source file.
type Module struct {
	File    string
	Imports []*ImportStmt
	Tools   []*ToolDecl
	Funcs   []*FnDecl
	// Top-level statements run in order before main (module init).
	Init []Stmt
}
