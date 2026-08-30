package deck

import (
	"unicode/utf8"

	kast "github.com/sandgorgon/9sh/kyu/ast"
	"github.com/sandgorgon/9sh/kyu/parser"
	ktoken "github.com/sandgorgon/9sh/kyu/token"
)

// KyuSegmenter segments kyu source into one card per top-level statement
// (a `name := expr` define, a `target = expr` assign, a `bind` namespace
// verb, or a bare expression statement), plus a leading "preamble" card
// for any content before the first statement — or, if the source fails to
// parse or has no top-level statements at all, one "preamble" card
// covering the whole file. Uses 9sh's kyu/parser (a real AST, not a
// line-based heuristic) for boundaries, the same spirit as GoSegmenter.
//
// kyu's lexer tracks each token's (line, rune-column), not a byte offset,
// so card spans are reconstructed by walking src's own line-start table —
// see kyuOffset. Most AST nodes' Tok is their own leftmost token, but a
// handful of parser.go's postfix/infix constructors (BinaryExpr, PipeExpr,
// FieldAccess, Call, ErrCheck, Background) stamp Tok with the *operator*
// token instead, so kyuExprTok recurses to the true leftmost operand
// rather than trusting Tok directly. ast.DefineStmt.Tok is a step further:
// it's the ':=' token, and — unlike AssignStmt, whose Target expr carries
// its own position — DefineStmt has nowhere to recover the leading
// identifier's position from at all; kyuDefineStart reconstructs it by
// scanning src backward for the identifier text. See
// upstream-specs/9sh-kyu-definestmt-name-position.md.
type KyuSegmenter struct{}

func (KyuSegmenter) Segment(src []byte) []Card {
	if len(src) == 0 {
		return nil
	}
	p := parser.New(string(src))
	prog := p.ParseProgram()
	if len(p.Errors()) > 0 || prog == nil || len(prog.Stmts) == 0 {
		return []Card{{Title: firstLine(src), Span: [2]int{0, len(src)}, Kind: "preamble"}}
	}

	lineStarts := kyuLineStarts(src)

	type stmt struct {
		spanStart int
		title     string
		kind      string
	}
	stmts := make([]stmt, 0, len(prog.Stmts))
	for _, s := range prog.Stmts {
		tok, kind := kyuStmtTok(s)
		start := kyuOffset(src, lineStarts, tok.Line, tok.Col)
		if def, ok := s.(*kast.DefineStmt); ok {
			start = kyuDefineStart(src, start, def.Name)
		}
		stmts = append(stmts, stmt{spanStart: start, title: firstLine(src[start:]), kind: kind})
	}

	var cards []Card
	if stmts[0].spanStart > 0 {
		cards = append(cards, Card{
			Title: firstLine(src),
			Span:  [2]int{0, stmts[0].spanStart},
			Kind:  "preamble",
		})
	}
	for i, s := range stmts {
		end := len(src)
		if i+1 < len(stmts) {
			end = stmts[i+1].spanStart
		}
		cards = append(cards, Card{Title: s.title, Span: [2]int{s.spanStart, end}, Kind: s.kind})
	}
	return cards
}

// kyuStmtTok returns s's own boundary token (the token whose (line, col)
// marks the statement's start, after resolving through kyuExprTok where s
// wraps an Expr) and the Card.Kind it should produce. For *kast.DefineStmt
// the returned token is ':=', not the statement's true start — the caller
// corrects that via kyuDefineStart.
func kyuStmtTok(s kast.Stmt) (ktoken.Token, string) {
	switch n := s.(type) {
	case *kast.DefineStmt:
		return n.Tok, "define"
	case *kast.AssignStmt:
		return kyuExprTok(n.Target), "assign"
	case *kast.BindStmt:
		return n.Tok, "bind"
	case *kast.ExprStmt:
		return kyuExprTok(n.X), "expr"
	default:
		return ktoken.Token{}, "stmt"
	}
}

// kyuExprTok returns e's leftmost token. Leaf/prefix nodes (Ident,
// literals, ExternalCall, UnaryExpr, IfExpr, AtHost, Closure, and the
// Record/Table/List literals) stamp Tok with their own leading token, so
// it's returned directly; the rest are postfix or infix constructors
// (parser.go's parseFieldAccess/parseCall/parseBinary/parsePipe/parseInfix's
// ErrCheck case) whose Tok is the operator — '.', '(', an infix operator,
// '|', '?' — not the expression's start, so those recurse into whichever
// operand sits to their left.
func kyuExprTok(e kast.Expr) ktoken.Token {
	switch n := e.(type) {
	case *kast.BinaryExpr:
		return kyuExprTok(n.Left)
	case *kast.PipeExpr:
		return kyuExprTok(n.Left)
	case *kast.FieldAccess:
		return kyuExprTok(n.Recv)
	case *kast.Call:
		return kyuExprTok(n.Fn)
	case *kast.ErrCheck:
		return kyuExprTok(n.X)
	case *kast.Background:
		return kyuExprTok(n.Call)
	case *kast.UnaryExpr:
		return n.Tok
	case *kast.Ident:
		return n.Tok
	case *kast.IntLit:
		return n.Tok
	case *kast.FloatLit:
		return n.Tok
	case *kast.StringLit:
		return n.Tok
	case *kast.BoolLit:
		return n.Tok
	case *kast.NullLit:
		return n.Tok
	case *kast.DurationLit:
		return n.Tok
	case *kast.PathLit:
		return n.Tok
	case *kast.RecordLit:
		return n.Tok
	case *kast.TableLit:
		return n.Tok
	case *kast.ListLit:
		return n.Tok
	case *kast.Closure:
		return n.Tok
	case *kast.ExternalCall:
		return n.Tok
	case *kast.IfExpr:
		return n.Tok
	case *kast.AtHost:
		return n.Tok
	default:
		return ktoken.Token{}
	}
}

// kyuDefineStart recovers a `name := expr` statement's true start —
// ast.DefineStmt has no field carrying it — by scanning src backward from
// assignOffset (the ':=' token's own offset) over whitespace, then
// checking that the len(name) bytes immediately before it spell name.
// kyu's grammar only recognizes a DefineStmt when an IDENT is immediately
// followed (lexically) by ':=', so that backward scan lands on exactly
// the identifier in every source the parser actually accepted; a mismatch
// (which real kyu source can't produce, but a defensive fallback still
// covers) returns assignOffset itself rather than guessing further.
func kyuDefineStart(src []byte, assignOffset int, name string) int {
	i := assignOffset
	for i > 0 && isKyuSpace(src[i-1]) {
		i--
	}
	start := i - len(name)
	if start >= 0 && string(src[start:i]) == name {
		return start
	}
	return assignOffset
}

func isKyuSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// kyuLineStarts returns the byte offset of the start of each line in src;
// kyuLineStarts(src)[n] is line n+1's start, matching kyu/token.Token's
// 1-based Line.
func kyuLineStarts(src []byte) []int {
	starts := []int{0}
	for i, b := range src {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// kyuOffset converts a 1-based (line, col) position — col counted in
// runes from the line's start, matching kyu/lexer's own tracking, not
// bytes — into a byte offset into src.
func kyuOffset(src []byte, lineStarts []int, line, col int) int {
	if line < 1 || line > len(lineStarts) {
		return 0
	}
	pos := lineStarts[line-1]
	for i := 1; i < col && pos < len(src); i++ {
		_, size := utf8.DecodeRune(src[pos:])
		pos += size
	}
	return pos
}
