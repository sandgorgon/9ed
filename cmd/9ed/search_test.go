package main

import (
	"testing"

	"github.com/sandgorgon/tui/input"

	"github.com/sandgorgon/9ed/deck"
)

// newBodySearchTestModel segments a real Go snippet with GoSegmenter so
// Span/body content are internally consistent (rather than hand-picked
// byte offsets that could silently drift from src) — for exercising
// title-or-body matching, where the earlier title-only fixture's
// zero-Span cards (always an empty body) can't help.
func newBodySearchTestModel() *model {
	src := []byte("package p\n\nfunc Walrus() {}\n\nfunc Beta() {\n\tWalrus()\n}\n")
	cards := deck.GoSegmenter{}.Segment(src) // 0: preamble, 1: Walrus, 2: Beta
	return &model{path: "f.go", src: src, cards: cards, view: &bufferView{}}
}

// newSearchTestModel's titles are chosen so "wal" (and its prefixes)
// cleanly picks out exactly the three Walrus/Walnut cards, interleaved
// with two that never match "w" at all — avoiding coincidental overlaps
// (e.g. most short English words contain 'a').
func newSearchTestModel() *model {
	cards := []deck.Card{
		{Title: "Walrus", Kind: "func"},  // 0
		{Title: "Beta", Kind: "func"},    // 1
		{Title: "Walnut", Kind: "func"},  // 2
		{Title: "Gamma", Kind: "func"},   // 3
		{Title: "Walrus2", Kind: "func"}, // 4
	}
	return &model{path: "f.go", src: []byte("x"), cards: cards, view: &bufferView{}}
}

func TestFilteredIndices(t *testing.T) {
	m := newSearchTestModel()

	t.Run("empty query returns every index", func(t *testing.T) {
		m.query = ""
		if got := m.filteredIndices(); len(got) != 5 {
			t.Errorf("filteredIndices() = %v, want all 5 indices", got)
		}
	})

	t.Run("case-insensitive substring match", func(t *testing.T) {
		m.query = "WAL"
		got := m.filteredIndices()
		want := []int{0, 2, 4}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			t.Errorf("filteredIndices() = %v, want %v (Walrus, Walnut, Walrus2)", got, want)
		}
	})

	t.Run("no matches returns an empty slice", func(t *testing.T) {
		m.query = "zzz"
		if got := m.filteredIndices(); len(got) != 0 {
			t.Errorf("filteredIndices() = %v, want empty", got)
		}
	})

	t.Run("matches a card whose body, not title, contains the query", func(t *testing.T) {
		m := newBodySearchTestModel() // 0: preamble, 1: Walrus, 2: Beta (calls Walrus())
		m.query = "Walrus"
		got := m.filteredIndices()
		want := []int{1, 2}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("filteredIndices() = %v, want %v (Walrus by title, Beta by body reference)", got, want)
		}
	})

	t.Run("regexp syntax is honored, not treated as a literal string", func(t *testing.T) {
		m := newBodySearchTestModel()
		m.query = "^func Beta"
		got := m.filteredIndices()
		if len(got) != 1 || got[0] != 2 {
			t.Errorf("filteredIndices() = %v, want [2] (only Beta's body starts with \"func Beta\")", got)
		}
	})

	t.Run("an invalid in-progress regexp matches nothing, not everything", func(t *testing.T) {
		m := newBodySearchTestModel()
		m.query = "Wal(rus" // unbalanced paren — still being typed
		if got := m.filteredIndices(); len(got) != 0 {
			t.Errorf("filteredIndices() = %v, want empty for an invalid pattern", got)
		}
	})
}

func TestBodyMatches(t *testing.T) {
	re, ok := searchRegexp("Walrus")
	if !ok {
		t.Fatal("searchRegexp(\"Walrus\") failed to compile")
	}

	t.Run("finds every occurrence, in order", func(t *testing.T) {
		body := "Walrus lives near another Walrus"
		got := bodyMatches(re, body)
		want := [][2]int{{0, 6}, {26, 32}}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("bodyMatches() = %v, want %v", got, want)
		}
	})

	t.Run("no matches returns nil", func(t *testing.T) {
		if got := bodyMatches(re, "no such word here"); got != nil {
			t.Errorf("bodyMatches() = %v, want nil", got)
		}
	})

	t.Run("byte offsets are translated to rune offsets for non-ASCII content", func(t *testing.T) {
		// "café " is 5 runes but 6 bytes (é is 2 bytes) — Walrus starting
		// right after it must land at rune offset 5, not byte offset 6.
		body := "café Walrus"
		got := bodyMatches(re, body)
		want := [][2]int{{5, 11}}
		if len(got) != 1 || got[0] != want[0] {
			t.Errorf("bodyMatches() = %v, want %v (rune offsets)", got, want)
		}
	})

	t.Run("zero-width matches are dropped", func(t *testing.T) {
		degenerate, ok := searchRegexp("z*") // matches the empty string everywhere
		if !ok {
			t.Fatal("searchRegexp(\"z*\") failed to compile")
		}
		if got := bodyMatches(degenerate, "abc"); got != nil {
			t.Errorf("bodyMatches() = %v, want nil (all matches were zero-width)", got)
		}
	})
}

func TestNextPrevMatchFrom(t *testing.T) {
	matches := [][2]int{{2, 5}, {10, 13}, {20, 23}}

	t.Run("nextMatchFrom finds the first match at or after from", func(t *testing.T) {
		if got, ok := nextMatchFrom(matches, 6); !ok || got != matches[1] {
			t.Errorf("nextMatchFrom(6) = %v, %v, want %v, true", got, ok, matches[1])
		}
	})

	t.Run("nextMatchFrom includes a match starting exactly at from", func(t *testing.T) {
		if got, ok := nextMatchFrom(matches, 10); !ok || got != matches[1] {
			t.Errorf("nextMatchFrom(10) = %v, %v, want %v, true", got, ok, matches[1])
		}
	})

	t.Run("nextMatchFrom past the last match is not ok", func(t *testing.T) {
		if _, ok := nextMatchFrom(matches, 24); ok {
			t.Error("nextMatchFrom(24) ok = true, want false")
		}
	})

	t.Run("prevMatchBefore finds the last match strictly before from", func(t *testing.T) {
		if got, ok := prevMatchBefore(matches, 15); !ok || got != matches[1] {
			t.Errorf("prevMatchBefore(15) = %v, %v, want %v, true", got, ok, matches[1])
		}
	})

	t.Run("prevMatchBefore excludes a match starting exactly at from", func(t *testing.T) {
		if got, ok := prevMatchBefore(matches, 10); !ok || got != matches[0] {
			t.Errorf("prevMatchBefore(10) = %v, %v, want %v, true", got, ok, matches[0])
		}
	})

	t.Run("prevMatchBefore before the first match is not ok", func(t *testing.T) {
		if _, ok := prevMatchBefore(matches, 2); ok {
			t.Error("prevMatchBefore(2) ok = true, want false")
		}
	})
}

func TestSnapCursorToFiltered(t *testing.T) {
	t.Run("stays put when the cursor is still a match", func(t *testing.T) {
		m := newSearchTestModel()
		m.query, m.cursor = "wal", 4
		m.snapCursorToFiltered()
		if m.cursor != 4 {
			t.Errorf("cursor = %d, want unchanged 4", m.cursor)
		}
	})

	t.Run("snaps to the first match when the cursor no longer matches", func(t *testing.T) {
		m := newSearchTestModel()
		m.query, m.cursor = "wal", 1 // Beta, not a match
		m.snapCursorToFiltered()
		if m.cursor != 0 {
			t.Errorf("cursor = %d, want 0 (first match, Walrus)", m.cursor)
		}
	})

	t.Run("leaves the cursor alone with zero matches", func(t *testing.T) {
		m := newSearchTestModel()
		m.query, m.cursor = "zzz", 3
		m.snapCursorToFiltered()
		if m.cursor != 3 {
			t.Errorf("cursor = %d, want unchanged 3 (nothing to snap to)", m.cursor)
		}
	})
}

func TestSearchLifecycle(t *testing.T) {
	t.Run("'/' starts a search, remembering the pre-search cursor", func(t *testing.T) {
		m := newSearchTestModel()
		m.cursor = 3
		mm, _ := m.Update(startSearchMsg{})
		m = mm.(*model)
		if !m.searching || m.query != "" || m.preSearchCursor != 3 {
			t.Errorf("searching=%v query=%q preSearchCursor=%d", m.searching, m.query, m.preSearchCursor)
		}
	})

	t.Run("typing narrows the cursor into the filtered set", func(t *testing.T) {
		m := newSearchTestModel()
		m.cursor = 1 // Beta — not a "w" match
		mm, _ := m.Update(startSearchMsg{})
		m = mm.(*model)
		mm, _ = m.Update(searchInputMsg{r: 'w'})
		m = mm.(*model)
		if m.query != "w" || m.cursor != 0 {
			t.Errorf("after 'w': query=%q cursor=%d, want query=\"w\" cursor=0 (first w-match, Walrus)", m.query, m.cursor)
		}
		mm, _ = m.Update(searchInputMsg{r: 'a'})
		m = mm.(*model)
		if m.query != "wa" || m.cursor != 0 {
			t.Errorf("after 'wa': query=%q cursor=%d, want query=\"wa\" cursor=0 (still a match, stays put)", m.query, m.cursor)
		}
	})

	t.Run("backspace shrinks the query", func(t *testing.T) {
		m := newSearchTestModel()
		mm, _ := m.Update(startSearchMsg{})
		m = mm.(*model)
		mm, _ = m.Update(searchInputMsg{r: 'a'})
		m = mm.(*model)
		mm, _ = m.Update(searchInputMsg{r: 'l'})
		m = mm.(*model)
		mm, _ = m.Update(searchBackspaceMsg{})
		m = mm.(*model)
		if m.query != "a" {
			t.Errorf("query = %q, want \"a\" after one backspace", m.query)
		}
	})

	t.Run("Up/Down move within the filtered set, bounded", func(t *testing.T) {
		m := newSearchTestModel() // cursor defaults to 0 (Walrus, already a "wa" match)
		mm, _ := m.Update(startSearchMsg{})
		m = mm.(*model)
		mm, _ = m.Update(searchInputMsg{r: 'w'})
		m = mm.(*model)
		mm, _ = m.Update(searchInputMsg{r: 'a'}) // matches Walrus(0), Walnut(2), Walrus2(4)
		m = mm.(*model)

		mm, _ = m.Update(searchMoveMsg{delta: 1})
		m = mm.(*model)
		if m.cursor != 2 {
			t.Errorf("cursor = %d, want 2 (Walnut, next match after Walrus)", m.cursor)
		}
		mm, _ = m.Update(searchMoveMsg{delta: 1})
		m = mm.(*model)
		if m.cursor != 4 {
			t.Errorf("cursor = %d, want 4 (Walrus2)", m.cursor)
		}
		mm, _ = m.Update(searchMoveMsg{delta: 1}) // already at the last match
		m = mm.(*model)
		if m.cursor != 4 {
			t.Errorf("cursor = %d, want unchanged 4 (bounded, no wraparound)", m.cursor)
		}
	})

	t.Run("Esc restores the pre-search cursor and exits search mode", func(t *testing.T) {
		m := newSearchTestModel()
		m.cursor = 1
		mm, _ := m.Update(startSearchMsg{})
		m = mm.(*model)
		mm, _ = m.Update(searchInputMsg{r: 'a'})
		m = mm.(*model)
		mm, _ = m.Update(cancelSearchMsg{})
		m = mm.(*model)
		if m.searching || m.cursor != 1 {
			t.Errorf("searching=%v cursor=%d, want searching=false cursor=1 (restored)", m.searching, m.cursor)
		}
	})

	t.Run("Enter on a match exits search and enters Edit mode", func(t *testing.T) {
		m := newSearchTestModel()
		mm, _ := m.Update(startSearchMsg{})
		m = mm.(*model)
		mm, _ = m.Update(searchInputMsg{r: 'b'}) // matches only Beta(1)
		m = mm.(*model)
		mm, _ = m.Update(commitSearchMsg{})
		m = mm.(*model)
		if m.searching || !m.editing || m.cursor != 1 {
			t.Errorf("searching=%v editing=%v cursor=%d, want searching=false editing=true cursor=1 (Beta)", m.searching, m.editing, m.cursor)
		}
	})

	t.Run("Enter with zero matches is a no-op, staying in search mode", func(t *testing.T) {
		m := newSearchTestModel()
		mm, _ := m.Update(startSearchMsg{})
		m = mm.(*model)
		mm, _ = m.Update(searchInputMsg{r: 'z'})
		m = mm.(*model)
		mm, _ = m.Update(commitSearchMsg{})
		m = mm.(*model)
		if m.editing {
			t.Error("expected Enter with zero matches not to enter Edit mode")
		}
	})
}

// TestSearchSwallowsGlobalKeys regression-tests a bug caught live in
// tmux: every keypress reaches Update as a raw input.KeyEvent *and* as
// whatever Msg the focused List's onEvent produces (tui always delivers
// both — see TestGoToFirstLast's "gg still completes" test for the same
// pattern elsewhere). Before this guard existed, typing a query
// containing 'q' also hit Update's bottom-of-switch "bare 'q' quits"
// check via the raw-KeyEvent path, and 't' likewise toggled the theme —
// both fired mid-keystroke while composing a search query.
func TestSearchSwallowsGlobalKeys(t *testing.T) {
	t.Run("'q' while searching does not quit", func(t *testing.T) {
		m := newSearchTestModel()
		m.searching = true
		_, cmd := m.Update(input.KeyEvent{Rune: 'q'})
		if cmd != nil {
			t.Error("Update returned a non-nil Cmd for 'q' while searching, want nil (no quit)")
		}
	})

	t.Run("'t' while searching does not toggle the theme", func(t *testing.T) {
		m := newSearchTestModel()
		m.searching = true
		before := m.theme
		mm, _ := m.Update(input.KeyEvent{Rune: 't'})
		m = mm.(*model)
		if m.theme != before {
			t.Error("theme changed after 't' while searching, want unchanged")
		}
	})
}

func TestListEventSearchRouting(t *testing.T) {
	m := &model{}
	if got := m.listEvent(input.KeyEvent{Rune: '/'}); got != (startSearchMsg{}) {
		t.Errorf("listEvent('/') = %#v, want startSearchMsg{}", got)
	}

	m.searching = true
	if got := m.listEvent(input.KeyEvent{Rune: 'g'}); got != (searchInputMsg{r: 'g'}) {
		t.Errorf("while searching, listEvent('g') = %#v, want searchInputMsg{r: 'g'} (not navG)", got)
	}
	if got := m.listEvent(input.KeyEvent{Rune: '3'}); got != (searchInputMsg{r: '3'}) {
		t.Errorf("while searching, listEvent('3') = %#v, want searchInputMsg{r: '3'} (not navDigitMsg)", got)
	}
	if got := m.listEvent(input.KeyEvent{Key: input.KeyEsc}); got != (cancelSearchMsg{}) {
		t.Errorf("while searching, listEvent(Esc) = %#v, want cancelSearchMsg{}", got)
	}
}
