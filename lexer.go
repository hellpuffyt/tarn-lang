// Package tarn implements the Tarn language: a small statically checked
// language for deterministic, capability-gated agent workflows.
package tarn

import (
	"fmt"
	"strings"
)

// Pos is a 1-based source position.
type Pos struct{ Line, Col int }

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Error is a diagnostic with a position.
type Error struct {
	Pos  Pos
	Msg  string
	File string
}

func (e *Error) Error() string {
	if e.File != "" {
		return fmt.Sprintf("%s:%s: %s", e.File, e.Pos, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.Pos, e.Msg)
}

func errAt(p Pos, f string, a ...any) *Error { return &Error{Pos: p, Msg: fmt.Sprintf(f, a...)} }

type TokKind int

const (
	TEOF TokKind = iota
	TIdent
	TInt
	TStr
	TKeyword
	TOp
	TNewline
)

type Token struct {
	Kind TokKind
	Text string
	Pos  Pos
}

var keywords = map[string]bool{
	"fn": true, "let": true, "if": true, "else": true, "for": true, "in": true, "while": true,
	"return": true, "true": true, "false": true, "nil": true, "spawn": true, "await": true,
	"tool": true, "import": true, "as": true, "and": true, "or": true, "not": true, "break": true, "continue": true,
}

var ops = []string{"->", "==", "!=", "<=", ">=", "&&", "||", "+", "-", "*", "/", "%", "<", ">", "=", "!", "(", ")", "{", "}", "[", "]", ",", ":", ".", "|"}

// Lex tokenizes source. Newlines are significant as statement terminators
// (like Go), so they are emitted as tokens and the parser decides.
func Lex(src string) ([]Token, error) {
	var toks []Token
	line, col := 1, 1
	i := 0
	adv := func(n int) {
		for k := 0; k < n; k++ {
			if src[i] == '\n' {
				line++
				col = 1
			} else {
				col++
			}
			i++
		}
	}
	for i < len(src) {
		c := src[i]
		p := Pos{line, col}
		switch {
		case c == '\n':
			toks = append(toks, Token{TNewline, "\n", p})
			adv(1)
		case c == ' ' || c == '\t' || c == '\r':
			adv(1)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				adv(1)
			}
		case c == '_' || isLetter(c):
			s := i
			for i < len(src) && (isLetter(src[i]) || isDigit(src[i]) || src[i] == '_') {
				adv(1)
			}
			w := src[s:i]
			if keywords[w] {
				toks = append(toks, Token{TKeyword, w, p})
			} else {
				toks = append(toks, Token{TIdent, w, p})
			}
		case isDigit(c):
			s := i
			for i < len(src) && (isDigit(src[i]) || src[i] == '_') {
				adv(1)
			}
			if i < len(src) && isLetter(src[i]) {
				return nil, errAt(p, "invalid suffix on number")
			}
			toks = append(toks, Token{TInt, strings.ReplaceAll(src[s:i], "_", ""), p})
		case c == '"':
			adv(1)
			var b strings.Builder
			for {
				if i >= len(src) || src[i] == '\n' {
					return nil, errAt(p, "unterminated string")
				}
				if src[i] == '"' {
					adv(1)
					break
				}
				if src[i] == '\\' && i+1 < len(src) {
					switch src[i+1] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case '"':
						b.WriteByte('"')
					case '\\':
						b.WriteByte('\\')
					default:
						return nil, errAt(Pos{line, col}, "unknown escape \\%c", src[i+1])
					}
					adv(2)
					continue
				}
				b.WriteByte(src[i])
				adv(1)
			}
			toks = append(toks, Token{TStr, b.String(), p})
		default:
			matched := false
			for _, op := range ops {
				if strings.HasPrefix(src[i:], op) {
					toks = append(toks, Token{TOp, op, p})
					adv(len(op))
					matched = true
					break
				}
			}
			if !matched {
				return nil, errAt(p, "unexpected character %q", c)
			}
		}
	}
	toks = append(toks, Token{TEOF, "", Pos{line, col}})
	return toks, nil
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
