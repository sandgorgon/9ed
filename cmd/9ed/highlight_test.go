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
