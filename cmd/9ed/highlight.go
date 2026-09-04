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

// highlightsFor dispatches to a syntax highlighter by the source file's
// own extension (the same table segmenterFor uses), or nil for any
// extension with none — currently Markdown, Kyu, and anything
// segmenterFor falls back to deck.PlainSegmenter for. Go keeps its own
// go/scanner-based goHighlights (exact, since Go ships a real
// tokenizer); C/C++ and Bash use the shared regex engine below, per
// this project's existing tolerance for a "good enough" heuristic
// rather than a real grammar for those languages — see CSegmenter's and
// BashSegmenter's own doc comments making the same trade-off for
// structural segmentation.
func highlightsFor(ext string, body string, theme style.Theme) []widget.StyleSpan {
	switch ext {
	case ".go":
		return goHighlights(body, theme)
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".hh", ".hpp", ".hxx":
		return regexHighlights(body, theme, cLangRe)
	case ".sh", ".bash":
		return regexHighlights(body, theme, bashLangRe)
	default:
		return nil
	}
}

// cLangRe tokenizes C/C++ well enough for coloring, not for correctness
// — same "good enough heuristic" the CSegmenter comment already accepts
// for this codebase's non-Go languages. Comment and string alternatives
// are listed ahead of keyword/number in the top-level alternation
// deliberately: regexHighlights relies on Go's regexp package resolving
// alternation leftmost-first (Perl-like, not POSIX longest-match), so a
// keyword spelled out inside a string literal or a comment is matched
// by the earlier, wider alternative first and never separately matches
// the keyword group.
var cLangRe = regexp.MustCompile(
	`(?P<comment>//[^\n]*|(?s:/\*.*?\*/))` +
		`|(?P<string>"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*')` +
		`|(?P<number>\b0[xX][0-9a-fA-F]+\b|\b\d+(?:\.\d+)?[uUlLfF]*\b)` +
		`|(?P<keyword>\b(?:auto|break|case|char|const|continue|default|do|double|else|enum|extern|float|for|goto|if|inline|int|long|register|restrict|return|short|signed|sizeof|static|struct|switch|typedef|union|unsigned|void|volatile|while|_Bool|_Complex|_Imaginary|class|namespace|public|private|protected|template|typename|virtual|override|new|delete|this|true|false|nullptr|using|friend|operator|try|catch|throw|explicit|mutable|constexpr|static_cast|dynamic_cast|const_cast|reinterpret_cast)\b)`,
)

// bashLangRe tokenizes Bash/POSIX-shell well enough for coloring — the
// same heuristic trade-off as cLangRe, matching BashSegmenter's own
// "not a real shell grammar" disclaimer (it doesn't track heredocs
// either, so a keyword-like word inside one can still get colored).
var bashLangRe = regexp.MustCompile(
	`(?P<comment>#[^\n]*)` +
		`|(?P<string>"(?:\\.|[^"\\])*"|'[^']*')` +
		`|(?P<number>\b\d+\b)` +
		`|(?P<keyword>\b(?:if|then|elif|else|fi|for|while|until|do|done|case|esac|function|in|select|time|coproc|return|break|continue|local|export|readonly|declare|typeset|unset|shift|exit|trap|eval|exec|source)\b)`,
)

// regexHighlights runs spec's combined regex over body once and returns
// one StyleSpan per match, styled by which of the four named groups
// (comment/string/number/keyword) matched — the same four semantic
// roles styleFor already maps for Go, reused here so every highlighted
// language reads consistently. Byte offsets from the regexp package are
// translated to the rune offsets widget.TextArea.Highlights expects,
// same as goHighlights.
func regexHighlights(body string, theme style.Theme, spec *regexp.Regexp) []widget.StyleSpan {
	if body == "" {
		return nil
	}
	names := spec.SubexpNames()
	byteToRune := byteToRuneOffsets(body)
	var spans []widget.StyleSpan
	for _, m := range spec.FindAllStringSubmatchIndex(body, -1) {
		for gi := 1; gi < len(names); gi++ {
			start, end := m[2*gi], m[2*gi+1]
			if start < 0 {
				continue
			}
			st, ok := regexGroupStyle(names[gi], theme)
			if !ok {
				break
			}
			spans = append(spans, widget.StyleSpan{
				Start: byteToRune[start],
				End:   byteToRune[end],
				Style: st,
			})
			break
		}
	}
	return spans
}

// regexGroupStyle maps a regexLangRe named group to the same semantic
// roles styleFor uses for Go tokens.
func regexGroupStyle(name string, theme style.Theme) (cell.Style, bool) {
	switch name {
	case "comment":
		return cell.Style{Fg: theme.Muted}, true
	case "string":
		return cell.Style{Fg: theme.Success}, true
	case "number":
		return cell.Style{Fg: theme.Warning}, true
	case "keyword":
		return cell.Style{Fg: theme.Secondary, Attr: cell.AttrBold}, true
	default:
		return cell.Style{}, false
	}
}

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
