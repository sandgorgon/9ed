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
		if got := highlightsFor(".txt", "plain text", theme); got != nil {
			t.Errorf("highlightsFor(.txt) = %v, want nil", got)
		}
	})

	t.Run(".md dispatches to the Markdown regex highlighter", func(t *testing.T) {
		got := highlightsFor(".md", "# Title", theme)
		if len(got) != 1 || got[0].Style.Fg != theme.Primary {
			t.Errorf("highlightsFor(.md) = %v, want one heading span for \"# Title\"", got)
		}
	})

	t.Run(".hs dispatches to the Haskell regex highlighter", func(t *testing.T) {
		got := highlightsFor(".hs", "module Main where", theme)
		if len(got) == 0 {
			t.Fatal("highlightsFor(.hs) = nil, want spans for module/where")
		}
	})

	t.Run(".kyu dispatches to kyuHighlights", func(t *testing.T) {
		got := highlightsFor(".kyu", `x := "hi" # note`, theme)
		want := kyuHighlights(`x := "hi" # note`, theme)
		if len(got) != len(want) || len(got) == 0 {
			t.Fatalf("highlightsFor(.kyu) = %v, want kyuHighlights() output %v", got, want)
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

func TestGoHighlights(t *testing.T) {
	theme := style.DefaultDark()

	t.Run("a predeclared type, constant, and builtin each get their own role", func(t *testing.T) {
		got := goHighlights(`var x int = len(s); var ok bool = true`, theme)
		var sawType, sawConstant, sawBuiltin bool
		for _, sp := range got {
			switch sp.Style.Fg {
			case theme.Primary:
				sawType = true
			case theme.Accent:
				sawConstant = true
			case theme.Error:
				sawBuiltin = true
			}
		}
		if !sawType {
			t.Error("no type-styled span found for \"int\"/\"bool\"")
		}
		if !sawConstant {
			t.Error("no constant-styled span found for \"true\"")
		}
		if !sawBuiltin {
			t.Error("no builtin-styled span found for \"len\"")
		}
	})

	t.Run("a shadowed builtin is still colored as the builtin", func(t *testing.T) {
		// go/scanner alone can't tell a shadowing local from the real
		// predeclared identifier — see styleFor's own doc comment on this
		// accepted trade-off.
		got := goHighlights(`len := 3`, theme)
		var found bool
		for _, sp := range got {
			if sp.Style.Fg == theme.Error {
				found = true
			}
		}
		if !found {
			t.Error("no builtin-styled span found for shadowed \"len\"")
		}
	})
}

func TestRegexHighlightsCSpec(t *testing.T) {
	theme := style.DefaultDark()

	t.Run("a preprocessor directive, a stdlib type, and a constant each get their own role", func(t *testing.T) {
		got := regexHighlights("#include <stdio.h>\nsize_t n = 0;\nbool b = true;", theme, cLangRe, regexGroupStyle)
		var sawPreprocessor, sawType, sawConstant bool
		for _, sp := range got {
			switch sp.Style.Fg {
			case theme.Error:
				sawPreprocessor = true
			case theme.Primary:
				sawType = true
			case theme.Accent:
				sawConstant = true
			}
		}
		if !sawPreprocessor {
			t.Error("no preprocessor-styled span found for \"#include <stdio.h>\"")
		}
		if !sawType {
			t.Error("no type-styled span found for \"size_t\"/\"bool\"")
		}
		if !sawConstant {
			t.Error("no constant-styled span found for \"true\"")
		}
	})

	t.Run("comments and strings win over a keyword-looking substring inside them", func(t *testing.T) {
		// Every keyword-looking word here ("if", "return", "for") only
		// appears inside the string literal or the trailing comment,
		// never as real code — a correct combined-regex match (see
		// cLangRe's own doc comment on alternation order) never produces
		// a keyword-styled span for this body at all.
		body := `x = "if return for"; // more if and for here`
		got := regexHighlights(body, theme, cLangRe, regexGroupStyle)
		for _, sp := range got {
			if sp.Style.Fg == theme.Secondary {
				t.Errorf("unexpected keyword-styled span %v: keywords here only appear inside a comment/string", sp)
			}
		}
	})

	t.Run("a real keyword outside any comment/string is highlighted", func(t *testing.T) {
		got := regexHighlights("return 0;", theme, cLangRe, regexGroupStyle)
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
	got := regexHighlights(`if [ -f "$f" ]; then echo hi; fi # comment`, theme, bashLangRe, regexGroupStyle)
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

	t.Run("a bare variable and a builtin command each get their own role", func(t *testing.T) {
		got := regexHighlights(`echo "$HOME"; y=$x`, theme, bashLangRe, regexGroupStyle)
		var sawVariable, sawBuiltin bool
		for _, sp := range got {
			switch sp.Style.Fg {
			case theme.Primary:
				sawVariable = true
			case theme.Error:
				sawBuiltin = true
			}
		}
		if !sawVariable {
			t.Error("no variable-styled span found for \"$x\"")
		}
		if !sawBuiltin {
			t.Error("no builtin-styled span found for \"echo\"")
		}
	})

	t.Run("a variable inside a double-quoted string is left as part of the string span", func(t *testing.T) {
		got := regexHighlights(`echo "$HOME"`, theme, bashLangRe, regexGroupStyle)
		for _, sp := range got {
			if sp.Style.Fg == theme.Primary {
				t.Errorf("unexpected variable-styled span %v: \"$HOME\" here is inside a string", sp)
			}
		}
	})
}

func TestRegexHighlightsHaskellSpec(t *testing.T) {
	theme := style.DefaultDark()
	body := "-- comment\nfact 0 = 1\nfact n = n * fact (n - 1)\ns = \"hi\""
	got := regexHighlights(body, theme, haskellLangRe, regexGroupStyle)
	var sawComment, sawString bool
	for _, sp := range got {
		switch sp.Style.Fg {
		case theme.Muted:
			sawComment = true
		case theme.Success:
			sawString = true
		}
	}
	if !sawComment {
		t.Error("no comment-styled span found for \"-- comment\"")
	}
	if !sawString {
		t.Error("no string-styled span found for \"hi\"")
	}

	t.Run("a trailing single-quote identifier isn't mistaken for a char literal", func(t *testing.T) {
		got := regexHighlights("fact' n = n", theme, haskellLangRe, regexGroupStyle)
		for _, sp := range got {
			if sp.Style.Fg == theme.Success {
				t.Errorf("unexpected string-styled span %v: fact' has no char/string literal", sp)
			}
		}
	})

	t.Run("a pragma and a capitalized type/constructor name each get their own role", func(t *testing.T) {
		got := regexHighlights("{-# LANGUAGE OverloadedStrings #-}\ndata Maybe a = Nothing | Just a", theme, haskellLangRe, regexGroupStyle)
		var sawPragma, sawType bool
		for _, sp := range got {
			switch sp.Style.Fg {
			case theme.Error:
				sawPragma = true
			case theme.Primary:
				sawType = true
			}
		}
		if !sawPragma {
			t.Error("no pragma-styled span found for \"{-# LANGUAGE ... #-}\"")
		}
		if !sawType {
			t.Error("no type-styled span found for \"Maybe\"/\"Nothing\"/\"Just\"")
		}
	})

	t.Run("a pragma is not swallowed by the wider block-comment alternative", func(t *testing.T) {
		got := regexHighlights("{-# INLINE f #-}", theme, haskellLangRe, regexGroupStyle)
		if len(got) != 1 || got[0].Style.Fg != theme.Error {
			t.Errorf("regexHighlights(pragma) = %v, want one Error-styled span covering the whole pragma", got)
		}
	})
}

func TestMdGroupStyle(t *testing.T) {
	theme := style.DefaultDark()

	t.Run("a fenced code block is colored, not left plain", func(t *testing.T) {
		body := "# Title\n\n```go\nif x > 1 {\n}\n```\n"
		got := regexHighlights(body, theme, mdLangRe, mdGroupStyle)
		var sawHeading, sawCodeblock bool
		for _, sp := range got {
			switch sp.Style.Fg {
			case theme.Primary:
				sawHeading = true
			case theme.Accent:
				sawCodeblock = true
			}
		}
		if !sawHeading {
			t.Error("no heading-styled span found for \"# Title\"")
		}
		if !sawCodeblock {
			t.Error("no codeblock-styled span found for the fenced block")
		}
	})

	t.Run("bold and italic only add an attribute, never a color", func(t *testing.T) {
		got := regexHighlights("plain **bold** and *italic* text", theme, mdLangRe, mdGroupStyle)
		var sawBold, sawItalic bool
		for _, sp := range got {
			if sp.Style.Fg != cell.DefaultColor() {
				t.Errorf("unexpected non-zero Fg on span %v: bold/italic should leave color alone", sp)
			}
			switch sp.Style.Attr {
			case cell.AttrBold:
				sawBold = true
			case cell.AttrItalic:
				sawItalic = true
			}
		}
		if !sawBold {
			t.Error("no bold-attributed span found for \"**bold**\"")
		}
		if !sawItalic {
			t.Error("no italic-attributed span found for \"*italic*\"")
		}
	})

	t.Run("a link is styled and underlined", func(t *testing.T) {
		got := regexHighlights("see [docs](https://example.com) for more", theme, mdLangRe, mdGroupStyle)
		if len(got) != 1 || got[0].Style.Fg != theme.Info || got[0].Style.Underline != cell.UnderlineSingle {
			t.Errorf("regexHighlights(link) = %v, want one underlined Info-styled span", got)
		}
	})

	t.Run("an image is styled distinctly from a plain link", func(t *testing.T) {
		got := regexHighlights("![alt](img.png)", theme, mdLangRe, mdGroupStyle)
		if len(got) != 1 || got[0].Style.Fg != theme.Success || got[0].Style.Underline != cell.UnderlineSingle {
			t.Errorf("regexHighlights(image) = %v, want one underlined Success-styled span", got)
		}
	})

	t.Run("strikethrough only adds an attribute, never a color", func(t *testing.T) {
		got := regexHighlights("plain ~~gone~~ text", theme, mdLangRe, mdGroupStyle)
		if len(got) != 1 || got[0].Style.Fg != cell.DefaultColor() || got[0].Style.Attr != cell.AttrStrikethrough {
			t.Errorf("regexHighlights(strike) = %v, want one strikethrough-attributed span with no color", got)
		}
	})

	t.Run("a list marker and a horizontal rule each get their own role", func(t *testing.T) {
		body := "- first\n1. second\n\n---\n"
		got := regexHighlights(body, theme, mdLangRe, mdGroupStyle)
		var sawListmarker, sawHR bool
		for _, sp := range got {
			switch sp.Style.Fg {
			case theme.Warning:
				sawListmarker = true
			case theme.Secondary:
				sawHR = true
			}
		}
		if !sawListmarker {
			t.Error("no listmarker-styled span found for \"- \"/\"1. \"")
		}
		if !sawHR {
			t.Error("no hr-styled span found for \"---\"")
		}
	})
}

func TestKyuHighlights(t *testing.T) {
	theme := style.DefaultDark()

	t.Run("keyword, string, number, and comment all get styled", func(t *testing.T) {
		body := `if true { x := "hi" } # note` + "\n" + `n := 42`
		got := kyuHighlights(body, theme)
		var sawKeyword, sawString, sawNumber, sawComment bool
		for _, sp := range got {
			switch sp.Style.Fg {
			case theme.Secondary:
				sawKeyword = true
			case theme.Success:
				sawString = true
			case theme.Warning:
				sawNumber = true
			case theme.Muted:
				sawComment = true
			}
		}
		if !sawKeyword {
			t.Error("no keyword-styled span found for \"if\"/\"true\"")
		}
		if !sawString {
			t.Error("no string-styled span found for \"hi\"")
		}
		if !sawNumber {
			t.Error("no number-styled span found for 42")
		}
		if !sawComment {
			t.Error("no comment-styled span found for \"# note\"")
		}
	})

	t.Run("a '#' inside a string literal is not mistaken for a comment", func(t *testing.T) {
		got := kyuHighlights(`x := "a # b"`, theme)
		for _, sp := range got {
			if sp.Style.Fg == theme.Muted {
				t.Errorf("unexpected comment-styled span %v: the '#' here is inside a string", sp)
			}
		}
	})

	t.Run("a string span matches its exact raw source extent despite an escape", func(t *testing.T) {
		body := `x := "a\"b"`
		got := kyuHighlights(body, theme)
		var found bool
		for _, sp := range got {
			if sp.Style.Fg == theme.Success {
				found = true
				if sp.Start != 5 || sp.End != 11 {
					t.Errorf("string span = [%d,%d), want [5,11) covering the full quoted literal", sp.Start, sp.End)
				}
			}
		}
		if !found {
			t.Error("no string-styled span found")
		}
	})

	t.Run("empty body returns nil", func(t *testing.T) {
		if got := kyuHighlights("", theme); got != nil {
			t.Errorf("kyuHighlights(\"\") = %v, want nil", got)
		}
	})

	t.Run("true/false/null are constant-styled, distinct from a real control-flow keyword", func(t *testing.T) {
		got := kyuHighlights(`if true { x := false } y := null`, theme)
		var sawKeyword, sawConstant bool
		for _, sp := range got {
			switch sp.Style.Fg {
			case theme.Secondary:
				sawKeyword = true
			case theme.Accent:
				sawConstant = true
			}
		}
		if !sawKeyword {
			t.Error("no keyword-styled span found for \"if\"")
		}
		if !sawConstant {
			t.Error("no constant-styled span found for \"true\"/\"false\"/\"null\"")
		}
	})
}
