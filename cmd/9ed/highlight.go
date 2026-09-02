package main

import (
	"go/scanner"
	"go/token"
	"regexp"
	"slices"

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

// searchMatchStyle is the shared "this is a search match" look — an
// inverted Accent/Background pair, the same "reversed" convention a
// terminal find bar uses — used both for editView's Highlights (via
// searchHighlights) and replaceView's inline match display, so a match
// looks the same whether you're just reading it or being asked to
// replace it.
func searchMatchStyle(theme style.Theme) cell.Style {
	return cell.Style{Fg: theme.Background, Bg: theme.Accent}
}

// searchHighlights returns one StyleSpan per occurrence of re in body,
// styled with searchMatchStyle — see mergeHighlights for combining
// these with goHighlights on the same TextArea.
func searchHighlights(re *regexp.Regexp, body string, theme style.Theme) []widget.StyleSpan {
	matches := bodyMatches(re, body)
	if len(matches) == 0 {
		return nil
	}
	st := searchMatchStyle(theme)
	spans := make([]widget.StyleSpan, len(matches))
	for i, sp := range matches {
		spans[i] = widget.StyleSpan{Start: sp[0], End: sp[1], Style: st}
	}
	return spans
}

// mergeHighlights combines base (e.g. goHighlights' syntax colors) with
// overlay (e.g. searchHighlights' match highlight) into the single
// sorted, non-overlapping span list widget.TextArea.Highlights requires
// — its own doc comment is explicit that overlapping spans produce
// undefined results, so simply concatenating the two lists isn't safe
// whenever a match falls inside (or spans across) a syntax token, which
// is the common case (searching for an identifier that's also
// highlighted as such). overlay wins: each base span is clipped to drop
// whatever portion an overlay span already covers, keeping only the
// leftover fragments before/after/around it.
func mergeHighlights(base, overlay []widget.StyleSpan) []widget.StyleSpan {
	if len(overlay) == 0 {
		return base
	}
	if len(base) == 0 {
		return overlay
	}
	out := make([]widget.StyleSpan, 0, len(base)+len(overlay))
	for _, b := range base {
		start := b.Start
		for _, o := range overlay {
			if o.End <= start || o.Start >= b.End {
				continue
			}
			if o.Start > start {
				out = append(out, widget.StyleSpan{Start: start, End: o.Start, Style: b.Style})
			}
			start = max(start, o.End)
		}
		if start < b.End {
			out = append(out, widget.StyleSpan{Start: start, End: b.End, Style: b.Style})
		}
	}
	out = append(out, overlay...)
	slices.SortFunc(out, func(a, c widget.StyleSpan) int { return a.Start - c.Start })
	return out
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
