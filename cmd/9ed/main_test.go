package main

import (
	"testing"

	"github.com/sandgorgon/tui/input"
	"github.com/sandgorgon/tui/tui"

	"github.com/sandgorgon/9ed/deck"
)

func TestSegmenterForUnknownExtension(t *testing.T) {
	if _, ok := segmenterFor("notes.xyz").(deck.PlainSegmenter); !ok {
		t.Errorf("segmenterFor(%q) = %T, want deck.PlainSegmenter", "notes.xyz", segmenterFor("notes.xyz"))
	}
	if _, ok := segmenterFor("main.go").(deck.GoSegmenter); !ok {
		t.Error("segmenterFor(\"main.go\") should still return GoSegmenter, not the fallback")
	}
}

// newNavTestModel builds a model with n cards, none of them backed by
// real content — fine for these tests, which only ever move m.cursor
// and never read a card's body.
func newNavTestModel(n int) *model {
	cards := make([]deck.Card, n)
	for i := range cards {
		cards[i] = deck.Card{Kind: "func"}
	}
	return &model{path: "f.go", cards: cards, view: &bufferView{}}
}

func TestGoToFirstLast(t *testing.T) {
	t.Run("G jumps straight to the last card", func(t *testing.T) {
		m := newNavTestModel(5)
		m.cursor = 1
		mm, _ := m.Update(navLast)
		m = mm.(*model)
		if m.cursor != 4 {
			t.Errorf("cursor = %d, want 4", m.cursor)
		}
	})

	t.Run("a single 'g' does nothing by itself", func(t *testing.T) {
		m := newNavTestModel(5)
		m.cursor = 3
		mm, _ := m.Update(navG)
		m = mm.(*model)
		if m.cursor != 3 {
			t.Errorf("cursor = %d, want unchanged 3", m.cursor)
		}
		if !m.pendingG {
			t.Error("expected pendingG = true after a lone 'g'")
		}
	})

	t.Run("gg jumps to the first card", func(t *testing.T) {
		m := newNavTestModel(5)
		m.cursor = 3
		mm, _ := m.Update(navG)
		m = mm.(*model)
		mm, _ = m.Update(navG)
		m = mm.(*model)
		if m.cursor != 0 {
			t.Errorf("cursor = %d, want 0", m.cursor)
		}
		if m.pendingG {
			t.Error("expected pendingG = false after gg completes")
		}
	})

	t.Run("an unrelated message in between cancels the pending g", func(t *testing.T) {
		m := newNavTestModel(5)
		m.cursor = 3
		mm, _ := m.Update(navG)
		m = mm.(*model)
		mm, _ = m.Update(navDown) // unrelated — cancels pendingG
		m = mm.(*model)
		mm, _ = m.Update(navG) // this 'g' must NOT combine with the earlier one
		m = mm.(*model)
		if m.cursor != 4 || !m.pendingG {
			t.Errorf("cursor = %d, pendingG = %v, want 4 and true (a fresh lone g, not a completed gg)", m.cursor, m.pendingG)
		}
	})

	// TestGoToFirstLast's other subtests call Update(navG) directly,
	// simulating only the Msg tui.App.Dispatch's HandleInput hands the
	// focused List's onEvent-produced Msg to. In the real app, every
	// keypress ALSO reaches Update synchronously as the raw
	// input.KeyEvent first (tui/app.go's HandleInput dispatches it
	// unconditionally, before/regardless of the focused widget's own
	// HandleEvent) — this regression-tests that a naive "reset pendingG
	// unconditionally at the top of Update" doesn't reintroduce the bug
	// caught interactively (tmux verification, M10): the second 'g'
	// press's own raw-KeyEvent echo would cancel the first 'g' before
	// its navG Msg ever arrived, so gg could never complete.
	t.Run("gg still completes when each keypress reaches Update as both a raw KeyEvent and navG", func(t *testing.T) {
		m := newNavTestModel(5)
		m.cursor = 3
		press := func(r rune, nm navMsg) {
			mm, _ := m.Update(input.KeyEvent{Rune: r})
			m = mm.(*model)
			mm, _ = m.Update(nm)
			m = mm.(*model)
		}
		press('g', navG)
		press('g', navG)
		if m.cursor != 0 {
			t.Errorf("cursor = %d, want 0 (gg should have completed)", m.cursor)
		}
	})
}

func TestPageUpDown(t *testing.T) {
	t.Run("PageDown moves forward by navPageSize, bounded at the last card", func(t *testing.T) {
		m := newNavTestModel(100)
		m.cursor = 0
		mm, _ := m.Update(navPageDown)
		m = mm.(*model)
		if m.cursor != navPageSize {
			t.Errorf("cursor = %d, want %d", m.cursor, navPageSize)
		}

		m.cursor = 95
		mm, _ = m.Update(navPageDown)
		m = mm.(*model)
		if m.cursor != 99 {
			t.Errorf("cursor = %d, want bounded at 99", m.cursor)
		}
	})

	t.Run("PageUp moves backward by navPageSize, bounded at the first card", func(t *testing.T) {
		m := newNavTestModel(100)
		m.cursor = 50
		mm, _ := m.Update(navPageUp)
		m = mm.(*model)
		if m.cursor != 50-navPageSize {
			t.Errorf("cursor = %d, want %d", m.cursor, 50-navPageSize)
		}

		m.cursor = 3
		mm, _ = m.Update(navPageUp)
		m = mm.(*model)
		if m.cursor != 0 {
			t.Errorf("cursor = %d, want bounded at 0", m.cursor)
		}
	})
}

// TestCardFirstLine covers the line-number math editView's Gutter closure
// relies on to show file-absolute line numbers — tested directly rather
// than needing to render through tui to verify.
func TestCardFirstLine(t *testing.T) {
	src := []byte("aaa\nbbb\nccc\nddd\n")
	tests := []struct {
		cardStart int
		want      int
	}{
		{0, 1},  // first card starts on line 1
		{4, 2},  // "bbb..." starts on line 2
		{8, 3},  // "ccc..." starts on line 3
		{12, 4}, // "ddd..." starts on line 4
		{16, 5}, // right at EOF — one line past the last real content
	}
	for _, tt := range tests {
		if got := cardFirstLine(src, tt.cardStart); got != tt.want {
			t.Errorf("cardFirstLine(src, %d) = %d, want %d", tt.cardStart, got, tt.want)
		}
	}
}

func TestListEventNavKeys(t *testing.T) {
	tests := []struct {
		ev   input.KeyEvent
		want tui.Msg
	}{
		{input.KeyEvent{Rune: 'g'}, navG},
		{input.KeyEvent{Rune: 'G'}, navLast},
		{input.KeyEvent{Key: input.KeyPgUp}, navPageUp},
		{input.KeyEvent{Key: input.KeyPgDown}, navPageDown},
	}
	m := &model{}
	for _, tt := range tests {
		if got := m.listEvent(tt.ev); got != tt.want {
			t.Errorf("listEvent(%+v) = %v, want %v", tt.ev, got, tt.want)
		}
	}
}
