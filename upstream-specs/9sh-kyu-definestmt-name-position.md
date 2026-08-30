# 9sh: ast.DefineStmt has no position for its own leading identifier

**Repo:** github.com/sandgorgon/9sh
**Origin:** surfaced while writing 9ed's `KyuSegmenter` (`deck/kyu.go`) — a real,
AST-based card segmenter for `.kyu` files, the same spirit as 9ed's existing
`GoSegmenter` (`go/parser`/`go/ast`).

## Problem

Every `kyu/ast` node position-consuming code needs is reachable except one:
`ast.DefineStmt`'s `Tok` field is stamped with the `:=` token
(`kyu/parser/parser.go`'s `parseDefineStmt`: `name := p.cur; p.next(); tok := p.cur`
captures `tok` *after* consuming the leading identifier), not the identifier itself.
`DefineStmt.Name` is already just a bare `string` — there is no `token.Token` or
position anywhere on the node for where that name starts.

Contrast with the sibling statement kinds, which don't have this problem:
- `AssignStmt.Tok` is *also* stamped with the operator (`=`), but `AssignStmt.Target`
  is itself an `Expr` (`*Ident` or `*FieldAccess`) carrying its own `Tok` — so a
  caller can always recover the statement's true start via `Target`.
- `BindStmt.Tok` is captured *before* `p.next()` consumes `'bind'`, so it already is
  the statement's leading token.

`DefineStmt` is the one place a caller has no AST field to consult at all for "where
does this statement actually begin."

## Why this matters

Any tool doing byte- or position-accurate reconstruction from a kyu `Program` — an
editor segmenting a file into cards (9ed), a formatter, a linter reporting a
statement's span, source-map generation — needs every statement's true start.
`DefineStmt` is also one of kyu's most common statement forms (`name := expr`), so
this isn't an edge case affecting a rarely-used node.

9ed's `KyuSegmenter` currently works around this by re-deriving the start via a
backward byte-scan from the `:=` token's offset, skipping whitespace and matching
`len(Name)` bytes against `Name` (`deck/kyu.go`'s `kyuDefineStart`) — correct today
because kyu's grammar only recognizes a `DefineStmt` when an `IDENT` is immediately
followed by `DEFINE`, but redundant with information the parser already had in hand
at `parseDefineStmt` and threw away, and it's the kind of scan a consumer shouldn't
have to reimplement per language tool.

## Proposed

Store the identifier's own token instead of (or in addition to) the `:=` token —
e.g.:

```go
type DefineStmt struct {
	NameTok token.Token // the identifier's own token
	Tok     token.Token // ':=' — kept for callers that specifically want the operator
	Name    string
	Val     Expr
}
```

`parseDefineStmt` already holds this value locally as `name` before overwriting
`tok` — it's a one-line change to keep it:

```go
func (p *Parser) parseDefineStmt() ast.Stmt {
	name := p.cur
	p.next() // consume ident
	tok := p.cur
	p.next() // consume :=
	val := p.parseValueExpr()
	return &ast.DefineStmt{NameTok: name, Tok: tok, Name: name.Literal, Val: val}
}
```

Adding a field is additive/non-breaking for existing callers that only read `Tok`
or `Name`.

## Why this is general-purpose, not 9ed-specific

Any tool built on `kyu/ast` that needs a `DefineStmt`'s true source position hits
this same gap — it's a property of the AST shape itself, not something specific to
how 9ed happens to use it.
