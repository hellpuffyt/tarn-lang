package tarn

import "strconv"

type parser struct {
	toks []Token
	i    int
	file string
}

// Parse parses one module.
func Parse(file, src string) (*Module, error) {
	toks, err := Lex(src)
	if err != nil {
		e := err.(*Error)
		e.File = file
		return nil, e
	}
	p := &parser{toks: toks, file: file}
	m := &Module{File: file}
	if err := p.module(m); err != nil {
		if e, ok := err.(*Error); ok {
			e.File = file
		}
		return nil, err
	}
	return m, nil
}

func (p *parser) peek() Token { return p.toks[p.i] }
func (p *parser) next() Token {
	t := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}
func (p *parser) is(kind TokKind, text string) bool {
	t := p.peek()
	return t.Kind == kind && (text == "" || t.Text == text)
}
func (p *parser) isOp(s string) bool { return p.is(TOp, s) }
func (p *parser) isKw(s string) bool { return p.is(TKeyword, s) }
func (p *parser) skipNewlines() {
	for p.peek().Kind == TNewline {
		p.next()
	}
}
func (p *parser) expectOp(s string) (Token, error) {
	if !p.isOp(s) {
		return Token{}, errAt(p.peek().Pos, "expected %q, found %s", s, describe(p.peek()))
	}
	return p.next(), nil
}
func (p *parser) expectIdent() (Token, error) {
	if p.peek().Kind != TIdent {
		return Token{}, errAt(p.peek().Pos, "expected identifier, found %s", describe(p.peek()))
	}
	return p.next(), nil
}
func (p *parser) endStmt() error {
	t := p.peek()
	if t.Kind == TNewline || t.Kind == TEOF || t.Kind == TOp && t.Text == "}" {
		return nil
	}
	return errAt(t.Pos, "expected end of statement, found %s", describe(t))
}

func describe(t Token) string {
	switch t.Kind {
	case TEOF:
		return "end of file"
	case TNewline:
		return "end of line"
	case TStr:
		return "string literal"
	case TInt:
		return "number " + t.Text
	}
	return "`" + t.Text + "`"
}

func (p *parser) module(m *Module) error {
	for {
		p.skipNewlines()
		t := p.peek()
		if t.Kind == TEOF {
			return nil
		}
		switch {
		case p.isKw("import"):
			p.next()
			if p.peek().Kind != TStr {
				return errAt(p.peek().Pos, "import needs a quoted path")
			}
			path := p.next().Text
			alias := ""
			if p.isKw("as") {
				p.next()
				id, err := p.expectIdent()
				if err != nil {
					return err
				}
				alias = id.Text
			}
			m.Imports = append(m.Imports, &ImportStmt{t.Pos, path, alias})
		case p.isKw("tool"):
			p.next()
			ns, err := p.expectIdent()
			if err != nil {
				return err
			}
			if _, err := p.expectOp("."); err != nil {
				return err
			}
			name, err := p.expectIdent()
			if err != nil {
				return err
			}
			params, ret, err := p.signature()
			if err != nil {
				return err
			}
			if ret == nil {
				ret = TNil_
			}
			m.Tools = append(m.Tools, &ToolDecl{t.Pos, ns.Text, name.Text, params, ret})
		case p.isKw("fn") && p.toks[p.i+1].Kind == TIdent:
			p.next()
			name := p.next()
			params, ret, err := p.signature()
			if err != nil {
				return err
			}
			body, err := p.block()
			if err != nil {
				return err
			}
			if ret == nil {
				ret = TNil_
			}
			m.Funcs = append(m.Funcs, &FnDecl{P: t.Pos, Name: name.Text, Params: params, Ret: ret, Body: body})
			continue // the closing brace terminates the declaration
		default:
			s, err := p.stmt()
			if err != nil {
				return err
			}
			m.Init = append(m.Init, s)
		}
		if err := p.endStmt(); err != nil {
			return err
		}
	}
}

func (p *parser) signature() ([]Param, *Type, error) {
	if _, err := p.expectOp("("); err != nil {
		return nil, nil, err
	}
	var params []Param
	for !p.isOp(")") {
		id, err := p.expectIdent()
		if err != nil {
			return nil, nil, err
		}
		var ty *Type = TAny_
		if p.isOp(":") {
			p.next()
			ty, err = p.typ()
			if err != nil {
				return nil, nil, err
			}
		}
		params = append(params, Param{id.Text, ty, id.Pos})
		if !p.isOp(",") {
			break
		}
		p.next()
	}
	if _, err := p.expectOp(")"); err != nil {
		return nil, nil, err
	}
	var ret *Type
	if p.isOp("->") {
		p.next()
		t, err := p.typ()
		if err != nil {
			return nil, nil, err
		}
		ret = t
	}
	return params, ret, nil
}

func (p *parser) typ() (*Type, error) {
	t := p.peek()
	if t.Kind != TIdent {
		return nil, errAt(t.Pos, "expected a type, found %s", describe(t))
	}
	p.next()
	switch t.Text {
	case "Int":
		return TInt_, nil
	case "Str":
		return TStr_, nil
	case "Bool":
		return TBool_, nil
	case "Nil":
		return TNil_, nil
	case "Any":
		return TAny_, nil
	case "List", "Map", "Task":
		if _, err := p.expectOp("["); err != nil {
			return nil, err
		}
		el, err := p.typ()
		if err != nil {
			return nil, err
		}
		if _, err := p.expectOp("]"); err != nil {
			return nil, err
		}
		return &Type{Kind: t.Text, Elem: el}, nil
	case "Fn":
		if _, err := p.expectOp("("); err != nil {
			return nil, err
		}
		var ps []*Type
		for !p.isOp(")") {
			pt, err := p.typ()
			if err != nil {
				return nil, err
			}
			ps = append(ps, pt)
			if !p.isOp(",") {
				break
			}
			p.next()
		}
		if _, err := p.expectOp(")"); err != nil {
			return nil, err
		}
		ret := TNil_
		if p.isOp("->") {
			p.next()
			r, err := p.typ()
			if err != nil {
				return nil, err
			}
			ret = r
		}
		return &Type{Kind: "Fn", Params: ps, Ret: ret}, nil
	}
	return nil, errAt(t.Pos, "unknown type %q (Int, Str, Bool, Nil, Any, List[T], Map[T], Task[T], Fn(..) -> T)", t.Text)
}

func (p *parser) block() (*Block, error) {
	open, err := p.expectOp("{")
	if err != nil {
		return nil, err
	}
	b := &Block{P: open.Pos}
	for {
		p.skipNewlines()
		if p.isOp("}") {
			p.next()
			return b, nil
		}
		if p.peek().Kind == TEOF {
			return nil, errAt(open.Pos, "block is never closed")
		}
		s, err := p.stmt()
		if err != nil {
			return nil, err
		}
		b.Stmts = append(b.Stmts, s)
		if err := p.endStmt(); err != nil {
			return nil, err
		}
	}
}

func (p *parser) stmt() (Stmt, error) {
	t := p.peek()
	switch {
	case p.isKw("let"):
		p.next()
		id, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		var ty *Type
		if p.isOp(":") {
			p.next()
			if ty, err = p.typ(); err != nil {
				return nil, err
			}
		}
		if _, err := p.expectOp("="); err != nil {
			return nil, err
		}
		v, err := p.expr()
		if err != nil {
			return nil, err
		}
		return &LetStmt{t.Pos, id.Text, ty, v}, nil
	case p.isKw("if"):
		return p.ifStmt()
	case p.isKw("for"):
		p.next()
		id, err := p.expectIdent()
		if err != nil {
			return nil, err
		}
		if !p.isKw("in") {
			return nil, errAt(p.peek().Pos, "expected `in`")
		}
		p.next()
		it, err := p.expr()
		if err != nil {
			return nil, err
		}
		body, err := p.block()
		if err != nil {
			return nil, err
		}
		return &ForStmt{t.Pos, id.Text, it, body}, nil
	case p.isKw("while"):
		p.next()
		c, err := p.expr()
		if err != nil {
			return nil, err
		}
		body, err := p.block()
		if err != nil {
			return nil, err
		}
		return &WhileStmt{t.Pos, c, body}, nil
	case p.isKw("return"):
		p.next()
		if p.peek().Kind == TNewline || p.isOp("}") || p.peek().Kind == TEOF {
			return &ReturnStmt{t.Pos, nil}, nil
		}
		v, err := p.expr()
		if err != nil {
			return nil, err
		}
		return &ReturnStmt{t.Pos, v}, nil
	case p.isKw("break"):
		p.next()
		return &BreakStmt{t.Pos}, nil
	case p.isKw("continue"):
		p.next()
		return &ContinueStmt{t.Pos}, nil
	case p.isKw("import") || p.isKw("tool"):
		return nil, errAt(t.Pos, "%s is only allowed at the top level", t.Text)
	}
	e, err := p.expr()
	if err != nil {
		return nil, err
	}
	if p.isOp("=") {
		p.next()
		switch e.(type) {
		case *Ident, *Index:
		default:
			return nil, errAt(e.pos(), "cannot assign to this expression")
		}
		v, err := p.expr()
		if err != nil {
			return nil, err
		}
		return &AssignStmt{t.Pos, e, v}, nil
	}
	return &ExprStmt{t.Pos, e}, nil
}

func (p *parser) ifStmt() (Stmt, error) {
	t := p.next() // if
	c, err := p.expr()
	if err != nil {
		return nil, err
	}
	then, err := p.block()
	if err != nil {
		return nil, err
	}
	s := &IfStmt{P: t.Pos, Cond: c, Then: then}
	if p.isKw("else") {
		p.next()
		if p.isKw("if") {
			inner, err := p.ifStmt()
			if err != nil {
				return nil, err
			}
			s.Else = &Block{P: inner.spos(), Stmts: []Stmt{inner}}
		} else {
			s.Else, err = p.block()
			if err != nil {
				return nil, err
			}
		}
	}
	return s, nil
}

// Precedence: or < and < == != < < <= > >= < + - < * / % < unary < postfix.
var binPrec = map[string]int{"or": 1, "||": 1, "and": 2, "&&": 2, "==": 3, "!=": 3, "<": 4, "<=": 4, ">": 4, ">=": 4, "+": 5, "-": 5, "*": 6, "/": 6, "%": 6}

func (p *parser) expr() (Expr, error) { return p.binary(1) }

func (p *parser) binary(min int) (Expr, error) {
	l, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.Kind != TOp && t.Kind != TKeyword {
			return l, nil
		}
		prec, ok := binPrec[t.Text]
		if !ok || prec < min {
			return l, nil
		}
		p.next()
		r, err := p.binary(prec + 1)
		if err != nil {
			return nil, err
		}
		op := t.Text
		if op == "and" {
			op = "&&"
		} else if op == "or" {
			op = "||"
		}
		l = &Binary{t.Pos, op, l, r}
	}
}

func (p *parser) unary() (Expr, error) {
	t := p.peek()
	if p.isOp("-") || p.isOp("!") || p.isKw("not") {
		p.next()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		op := t.Text
		if op == "not" {
			op = "!"
		}
		return &Unary{t.Pos, op, x}, nil
	}
	if p.isKw("spawn") {
		p.next()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		if _, ok := x.(*Call); !ok {
			return nil, errAt(t.Pos, "spawn needs a function call")
		}
		return &Spawn{t.Pos, x}, nil
	}
	if p.isKw("await") {
		p.next()
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return &Await{t.Pos, x}, nil
	}
	return p.postfix()
}

func (p *parser) postfix() (Expr, error) {
	e, err := p.primary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		switch {
		case p.isOp("("):
			p.next()
			var args []Expr
			p.skipNewlines()
			for !p.isOp(")") {
				a, err := p.expr()
				if err != nil {
					return nil, err
				}
				args = append(args, a)
				p.skipNewlines()
				if !p.isOp(",") {
					break
				}
				p.next()
				p.skipNewlines()
			}
			if _, err := p.expectOp(")"); err != nil {
				return nil, err
			}
			e = &Call{t.Pos, e, args}
		case p.isOp("["):
			p.next()
			idx, err := p.expr()
			if err != nil {
				return nil, err
			}
			if _, err := p.expectOp("]"); err != nil {
				return nil, err
			}
			e = &Index{t.Pos, e, idx}
		case p.isOp("."):
			p.next()
			id, err := p.expectIdent()
			if err != nil {
				return nil, err
			}
			e = &Field{t.Pos, e, id.Text}
		default:
			return e, nil
		}
	}
}

func (p *parser) primary() (Expr, error) {
	t := p.next()
	switch t.Kind {
	case TInt:
		v, err := strconv.ParseInt(t.Text, 10, 64)
		if err != nil {
			return nil, errAt(t.Pos, "integer literal out of range")
		}
		return &IntLit{t.Pos, v}, nil
	case TStr:
		return &StrLit{t.Pos, t.Text}, nil
	case TIdent:
		return &Ident{t.Pos, t.Text}, nil
	case TKeyword:
		switch t.Text {
		case "true":
			return &BoolLit{t.Pos, true}, nil
		case "false":
			return &BoolLit{t.Pos, false}, nil
		case "nil":
			return &NilLit{t.Pos}, nil
		case "fn":
			params, ret, err := p.signature()
			if err != nil {
				return nil, err
			}
			body, err := p.block()
			if err != nil {
				return nil, err
			}
			if ret == nil {
				ret = TNil_
			}
			return &FnLit{t.Pos, params, ret, body}, nil
		}
	case TOp:
		switch t.Text {
		case "(":
			p.skipNewlines()
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			p.skipNewlines()
			if _, err := p.expectOp(")"); err != nil {
				return nil, err
			}
			return e, nil
		case "[":
			l := &ListLit{P: t.Pos}
			p.skipNewlines()
			for !p.isOp("]") {
				e, err := p.expr()
				if err != nil {
					return nil, err
				}
				l.Items = append(l.Items, e)
				p.skipNewlines()
				if !p.isOp(",") {
					break
				}
				p.next()
				p.skipNewlines()
			}
			if _, err := p.expectOp("]"); err != nil {
				return nil, err
			}
			return l, nil
		case "{":
			m := &MapLit{P: t.Pos}
			p.skipNewlines()
			for !p.isOp("}") {
				k, err := p.expr()
				if err != nil {
					return nil, err
				}
				if _, err := p.expectOp(":"); err != nil {
					return nil, err
				}
				v, err := p.expr()
				if err != nil {
					return nil, err
				}
				m.Keys = append(m.Keys, k)
				m.Vals = append(m.Vals, v)
				p.skipNewlines()
				if !p.isOp(",") {
					break
				}
				p.next()
				p.skipNewlines()
			}
			if _, err := p.expectOp("}"); err != nil {
				return nil, err
			}
			return m, nil
		}
	}
	return nil, errAt(t.Pos, "expected an expression, found %s", describe(t))
}
