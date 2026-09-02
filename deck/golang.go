package deck

import (
	"go/ast"
	"go/parser"
	"go/token"
)

// GoSegmenter segments Go source into one card per top-level declaration
// (a func, or an import/type/var/const block), each including its
// immediately preceding doc comment in its span if one is attached, plus
// a leading "preamble" card covering the package clause and anything
// before the first declaration. Uses go/parser + go/ast (stdlib) for
// real, syntax-aware boundaries rather than a line-based heuristic.
//
// A source file that fails to parse, or has no top-level declarations at
// all, becomes a single "preamble" card covering the whole file — the
// same fallback shape MarkdownSegmenter uses for a heading-less document.
type GoSegmenter struct{}

func (GoSegmenter) Segment(src []byte) []Card {
	if len(src) == 0 {
		return nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil || file == nil || len(file.Decls) == 0 {
		return []Card{{Title: firstLine(src), Span: [2]int{0, len(src)}, Kind: "preamble"}}
	}

	type decl struct {
		spanStart int // start of the card's span; includes a Doc comment, if any
		title     string
		kind      string
		name      string // single defined identifier, if unambiguous; see Card.Name
	}
	decls := make([]decl, 0, len(file.Decls))
	for _, d := range file.Decls {
		declPos := fset.Position(d.Pos()).Offset
		spanStart, kind, name := declPos, "decl", ""

		switch n := d.(type) {
		case *ast.FuncDecl:
			kind = "func"
			if n.Name != nil {
				name = n.Name.Name
			}
			if n.Doc != nil {
				spanStart = fset.Position(n.Doc.Pos()).Offset
			}
		case *ast.GenDecl:
			if n.Doc != nil {
				spanStart = fset.Position(n.Doc.Pos()).Offset
			}
			switch n.Tok {
			case token.IMPORT:
				kind = "import"
			case token.TYPE:
				kind = "type"
			case token.VAR:
				kind = "var"
			case token.CONST:
				kind = "const"
			}
			// Name only for a single spec defining a single identifier
			// — a grouped block ("var (\n A = 1\n B = 2\n)") has no one
			// name to attribute cross-references to, so it's left
			// empty rather than picking the first spec arbitrarily.
			if len(n.Specs) == 1 {
				switch spec := n.Specs[0].(type) {
				case *ast.TypeSpec:
					name = spec.Name.Name
				case *ast.ValueSpec:
					if len(spec.Names) == 1 {
						name = spec.Names[0].Name
					}
				}
			}
		}
		// Title always comes from the declaration's own first line
		// (declPos), never from spanStart — a multi-line doc comment
		// shouldn't push the signature line out of the nav-mode label.
		decls = append(decls, decl{spanStart: spanStart, title: firstLine(src[declPos:]), kind: kind, name: name})
	}

	var cards []Card
	if decls[0].spanStart > 0 {
		cards = append(cards, Card{
			Title: firstLine(src),
			Span:  [2]int{0, decls[0].spanStart},
			Kind:  "preamble",
		})
	}
	for i, d := range decls {
		end := len(src)
		if i+1 < len(decls) {
			end = decls[i+1].spanStart
		}
		cards = append(cards, Card{Title: d.title, Span: [2]int{d.spanStart, end}, Kind: d.kind, Name: d.name})
	}
	return cards
}
