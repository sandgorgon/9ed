package main

import (
	"testing"

	"github.com/sandgorgon/tui/input"

	"github.com/sandgorgon/9ed/deck"
)

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
