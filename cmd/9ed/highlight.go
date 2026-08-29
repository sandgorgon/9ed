package main

import (
	"go/scanner"
	"go/token"

	"github.com/sandgorgon/tui/cell"
	"github.com/sandgorgon/tui/style"
	"github.com/sandgorgon/tui/widget"
)

// goHighlights tokenizes body — a single card's own text, standalone,
// not the whole file — with go/scanner and returns one StyleSpan per
// styled token, in *rune* offsets relative to body itself (what
// widget.TextArea.Highlights expects; go/scanner reports byte offsets,
// so a byte->rune translation happens below for correctness on any
// non-ASCII content in a string literal or comment).
func goHighlights(body string, theme style.Theme) []widget.StyleSpan {
	if body == "" {
		return nil
	}
	byteToRune := byteToRuneOffsets(body)

	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(body))
	var s scanner.Scanner
	s.Init(file, []byte(body), nil, scanner.ScanComments)

	var spans []widget.StyleSpan
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		cellStyle, ok := styleFor(tok, theme)
		if !ok {
			continue
		}
		n := len(lit)
		if n == 0 {
			n = len(tok.String())
		}
		startByte := file.Offset(pos)
		endByte := min(startByte+n, len(body)) // defensive: a malformed/truncated card body shouldn't panic
		spans = append(spans, widget.StyleSpan{
			Start: byteToRune[startByte],
			End:   byteToRune[endByte],
			Style: cellStyle,
		})
	}
	return spans
}

// styleFor maps a Go token kind to a theme color, reusing the theme's
// semantic roles rather than hardcoding colors — keywords get the
// theme's Secondary accent, comments Muted, string/char literals
// Success, numeric literals Warning. Punctuation, identifiers, and
// (deliberately) SEMICOLON — whose literal can be an auto-inserted "\n"
// rather than real source text — get no override.
func styleFor(tok token.Token, theme style.Theme) (cell.Style, bool) {
	switch {
	case tok.IsKeyword():
		return cell.Style{Fg: theme.Secondary, Attr: cell.AttrBold}, true
	case tok == token.COMMENT:
		return cell.Style{Fg: theme.Muted}, true
	case tok == token.STRING || tok == token.CHAR:
		return cell.Style{Fg: theme.Success}, true
	case tok == token.INT || tok == token.FLOAT || tok == token.IMAG:
		return cell.Style{Fg: theme.Warning}, true
	default:
		return cell.Style{}, false
	}
}

// byteToRuneOffsets returns, for every byte offset in s that starts a
// rune (plus len(s) itself), the corresponding rune offset — the
// translation StyleSpan needs since go/scanner positions are byte
// offsets but TextArea's buffer (and so its Highlights) is rune-indexed.
func byteToRuneOffsets(s string) []int {
	idx := make([]int, len(s)+1)
	rc := 0
	for i := range s {
		idx[i] = rc
		rc++
	}
	idx[len(s)] = rc
	return idx
}
