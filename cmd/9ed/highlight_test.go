package main

import (
	"testing"

	"github.com/sandgorgon/tui/cell"
	"github.com/sandgorgon/tui/style"
	"github.com/sandgorgon/tui/widget"
)

var (
	styleA = cell.Style{Fg: cell.ANSIColor(1)}
	styleB = cell.Style{Fg: cell.ANSIColor(2)}
)

func TestMergeHighlights(t *testing.T) {
	t.Run("no overlay returns base unchanged", func(t *testing.T) {
		base := []widget.StyleSpan{{Start: 0, End: 5, Style: styleA}}
		got := mergeHighlights(base, nil)
		if len(got) != 1 || got[0] != base[0] {
			t.Errorf("mergeHighlights() = %v, want base unchanged", got)
		}
	})

	t.Run("no base returns overlay unchanged", func(t *testing.T) {
		overlay := []widget.StyleSpan{{Start: 0, End: 5, Style: styleB}}
		got := mergeHighlights(nil, overlay)
		if len(got) != 1 || got[0] != overlay[0] {
			t.Errorf("mergeHighlights() = %v, want overlay unchanged", got)
		}
	})

	t.Run("an overlay span fully inside a base span splits it into before/after fragments", func(t *testing.T) {
		base := []widget.StyleSpan{{Start: 0, End: 10, Style: styleA}}
		overlay := []widget.StyleSpan{{Start: 3, End: 6, Style: styleB}}
		got := mergeHighlights(base, overlay)
		want := []widget.StyleSpan{
			{Start: 0, End: 3, Style: styleA},
			{Start: 3, End: 6, Style: styleB},
			{Start: 6, End: 10, Style: styleA},
		}
		if len(got) != len(want) {
			t.Fatalf("mergeHighlights() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("span %d = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("an overlay span exactly covering a base span drops it entirely", func(t *testing.T) {
		base := []widget.StyleSpan{{Start: 0, End: 5, Style: styleA}}
		overlay := []widget.StyleSpan{{Start: 0, End: 5, Style: styleB}}
		got := mergeHighlights(base, overlay)
		want := []widget.StyleSpan{{Start: 0, End: 5, Style: styleB}}
		if len(got) != 1 || got[0] != want[0] {
			t.Errorf("mergeHighlights() = %v, want %v", got, want)
		}
	})

	t.Run("an overlay span straddling two base spans clips both", func(t *testing.T) {
		base := []widget.StyleSpan{
			{Start: 0, End: 5, Style: styleA},
			{Start: 5, End: 10, Style: styleA},
		}
		overlay := []widget.StyleSpan{{Start: 3, End: 7, Style: styleB}}
		got := mergeHighlights(base, overlay)
		want := []widget.StyleSpan{
			{Start: 0, End: 3, Style: styleA},
			{Start: 3, End: 7, Style: styleB},
			{Start: 7, End: 10, Style: styleA},
		}
		if len(got) != len(want) {
			t.Fatalf("mergeHighlights() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("span %d = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run("output stays sorted by Start even with multiple base and overlay spans", func(t *testing.T) {
		base := []widget.StyleSpan{
			{Start: 0, End: 2, Style: styleA},
			{Start: 10, End: 12, Style: styleA},
		}
		overlay := []widget.StyleSpan{
			{Start: 5, End: 7, Style: styleB},
		}
		got := mergeHighlights(base, overlay)
		for i := 1; i < len(got); i++ {
			if got[i-1].Start > got[i].Start {
				t.Errorf("spans not sorted by Start: %v", got)
				break
			}
		}
	})
}

func TestSearchHighlights(t *testing.T) {
	theme := style.DefaultDark()

	t.Run("no matches returns nil", func(t *testing.T) {
		re, _ := searchRegexp("zzz")
		if got := searchHighlights(re, "no such word", theme); got != nil {
			t.Errorf("searchHighlights() = %v, want nil", got)
		}
	})

	t.Run("one span per match, styled as an inverted Accent/Background pair", func(t *testing.T) {
		re, _ := searchRegexp("cat")
		got := searchHighlights(re, "cat sat on the cat mat", theme)
		want := cell.Style{Fg: theme.Background, Bg: theme.Accent}
		if len(got) != 2 {
			t.Fatalf("searchHighlights() = %v, want 2 spans", got)
		}
		for _, sp := range got {
			if sp.Style != want {
				t.Errorf("span style = %v, want %v", sp.Style, want)
			}
		}
	})
}

func TestHighlightsFor(t *testing.T) {
	theme := style.DefaultDark()

	t.Run("an extension with no highlighter returns nil", func(t *testing.T) {
		if got := highlightsFor(".md", "# Title", theme); got != nil {
			t.Errorf("highlightsFor(.md) = %v, want nil", got)
		}
	})

	t.Run(".go still dispatches to goHighlights", func(t *testing.T) {
		got := highlightsFor(".go", "package foo", theme)
		want := goHighlights("package foo", theme)
		if len(got) != len(want) || len(got) == 0 {
			t.Fatalf("highlightsFor(.go) = %v, want goHighlights() output %v", got, want)
		}
	})

	t.Run(".c dispatches to the C regex highlighter", func(t *testing.T) {
		got := highlightsFor(".c", "int x;", theme)
		if len(got) != 1 || got[0].Style.Fg != theme.Secondary {
			t.Errorf("highlightsFor(.c) = %v, want one keyword span for \"int\"", got)
		}
	})

	t.Run(".sh dispatches to the Bash regex highlighter", func(t *testing.T) {
		got := highlightsFor(".sh", "if true; then :; fi", theme)
		if len(got) == 0 {
			t.Fatal("highlightsFor(.sh) = nil, want spans for if/then/fi")
		}
	})
}

func TestRegexHighlightsCSpec(t *testing.T) {
	theme := style.DefaultDark()

	t.Run("comments and strings win over a keyword-looking substring inside them", func(t *testing.T) {
		// Every keyword-looking word here ("if", "return", "for") only
		// appears inside the string literal or the trailing comment,
		// never as real code — a correct combined-regex match (see
		// cLangRe's own doc comment on alternation order) never produces
		// a keyword-styled span for this body at all.
		body := `x = "if return for"; // more if and for here`
		got := regexHighlights(body, theme, cLangRe)
		for _, sp := range got {
			if sp.Style.Fg == theme.Secondary {
				t.Errorf("unexpected keyword-styled span %v: keywords here only appear inside a comment/string", sp)
			}
		}
	})

	t.Run("a real keyword outside any comment/string is highlighted", func(t *testing.T) {
		got := regexHighlights("return 0;", theme, cLangRe)
		if len(got) < 1 {
			t.Fatal("regexHighlights() = nil, want at least a span for \"return\"")
		}
		found := false
		for _, sp := range got {
			if sp.Style.Fg == theme.Secondary {
				found = true
			}
		}
		if !found {
			t.Errorf("no keyword-styled span found in %v", got)
		}
	})
}

func TestRegexHighlightsBashSpec(t *testing.T) {
	theme := style.DefaultDark()
	got := regexHighlights(`if [ -f "$f" ]; then echo hi; fi # comment`, theme, bashLangRe)
	var sawKeyword, sawComment bool
	for _, sp := range got {
		switch sp.Style.Fg {
		case theme.Secondary:
			sawKeyword = true
		case theme.Muted:
			sawComment = true
		}
	}
	if !sawKeyword {
		t.Error("no keyword-styled span found for if/then/fi")
	}
	if !sawComment {
		t.Error("no comment-styled span found for \"# comment\"")
	}
}
