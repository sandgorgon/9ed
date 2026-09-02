package deck

import (
	"unicode/utf8"

	kast "github.com/sandgorgon/9sh/kyu/ast"
	"github.com/sandgorgon/9sh/kyu/parser"
	ktoken "github.com/sandgorgon/9sh/kyu/token"
)

// KyuSegmenter segments kyu source into one card per top-level statement
// (a `name := expr` define, a `target = expr` assign, a `bind`/`unbind`
// namespace verb, a `$cmd` passthrough, or a bare expression statement —
// which itself may be a `while`/`break`/`continue`), plus a leading
// "preamble" card for any content before the first statement — or, if the
// source fails to parse or has no top-level statements at all, one
// "preamble" card covering the whole file. Uses 9sh's kyu/parser (a real
// AST, not a line-based heuristic) for boundaries, the same spirit as
// GoSegmenter.
//
// kyu's lexer tracks each token's (line, rune-column), not a byte offset,
// so card spans are reconstructed by walking src's own line-start table —
// see kyuOffset. Most AST nodes' Tok is their own leftmost token, but a
// handful of parser.go's postfix/infix constructors (BinaryExpr, PipeExpr,
// FieldAccess, Call, ErrCheck, Background) stamp Tok with the *operator*
// token instead, so kyuExprTok recurses to the true leftmost operand
// rather than trusting Tok directly. ast.DefineStmt uses NameTok (9sh
// v0.2.1+) rather than its own Tok (the ':=' token) for the same reason —
// see upstream-specs/9sh-kyu-definestmt-name-position.md, the gap this
// was filed against and resolved.
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
		name      string
	}
	stmts := make([]stmt, 0, len(prog.Stmts))
	for _, s := range prog.Stmts {
		tok, kind := kyuStmtTok(s)
		start := kyuOffset(src, lineStarts, tok.Line, tok.Col)
		// Only "define" gets a Name: it's the one statement kind that
		// actually introduces a new identifier. "assign" mutates an
		// existing one (and its Target can be an arbitrary FieldAccess
		// chain, not a single name); "bind"/"unbind"/"passthrough"/
		// "expr" don't define anything at all — see Card.Name.
		name := ""
		if d, ok := s.(*kast.DefineStmt); ok {
			name = d.Name
		}
		stmts = append(stmts, stmt{spanStart: start, title: firstLine(src[start:]), kind: kind, name: name})
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
		cards = append(cards, Card{Title: s.title, Span: [2]int{s.spanStart, end}, Kind: s.kind, Name: s.name})
	}
	return cards
}

// kyuStmtTok returns s's own boundary token (the token whose (line, col)
// marks the statement's start, after resolving through kyuExprTok where s
// wraps an Expr) and the Card.Kind it should produce.
func kyuStmtTok(s kast.Stmt) (ktoken.Token, string) {
	switch n := s.(type) {
	case *kast.DefineStmt:
		return n.NameTok, "define"
	case *kast.AssignStmt:
		return kyuExprTok(n.Target), "assign"
	case *kast.BindStmt:
		return n.Tok, "bind"
	case *kast.UnbindStmt:
		return n.Tok, "unbind"
	case *kast.PassthroughStmt:
		return n.Tok, "passthrough"
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
	case *kast.WhileExpr:
		return n.Tok
	case *kast.BreakExpr:
		return n.Tok
	case *kast.ContinueExpr:
		return n.Tok
	default:
		return ktoken.Token{}
	}
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
