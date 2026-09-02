package main

import (
	"testing"

	"github.com/sandgorgon/tui/input"
	"github.com/sandgorgon/tui/tui"

	"github.com/sandgorgon/9ed/deck"
	"github.com/sandgorgon/9ed/notes"
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

func TestCursorMovedMsg(t *testing.T) {
	m := newNavTestModel(3)
	m.cursor = 1

	mm, _ := m.Update(cursorMovedMsg{offset: 5})
	m = mm.(*model)
	if got := m.cursorPos[1]; got != 5 {
		t.Errorf("cursorPos[1] = %d, want 5", got)
	}

	// A later move on a different card records under that card's own
	// index, not overwriting card 1's remembered position.
	m.cursor = 2
	mm, _ = m.Update(cursorMovedMsg{offset: 9})
	m = mm.(*model)
	if got := m.cursorPos[1]; got != 5 {
		t.Errorf("cursorPos[1] = %d, want unchanged 5", got)
	}
	if got := m.cursorPos[2]; got != 9 {
		t.Errorf("cursorPos[2] = %d, want 9", got)
	}
}

func TestSaveDoneMsgClearsCursorPos(t *testing.T) {
	m := newNavTestModel(2)
	m.cursorPos = map[int]int{0: 3, 1: 7}

	mm, _ := m.Update(saveDoneMsg{src: []byte("x"), cards: []deck.Card{{Kind: "func"}}})
	m = mm.(*model)
	if m.cursorPos != nil {
		t.Errorf("cursorPos = %v, want nil after a successful save (indices realign on resegment)", m.cursorPos)
	}
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

func TestCardBadges(t *testing.T) {
	cards := []deck.Card{
		{Kind: "func", Title: "func Foo() error", Name: "Foo"},
		{Kind: "func", Title: "func Bar() error", Name: "Bar"},
	}

	t.Run("no note, no references: no badge", func(t *testing.T) {
		m := &model{cards: cards}
		if got := m.cardBadges(0); got != "" {
			t.Errorf("cardBadges(0) = %q, want \"\"", got)
		}
	})

	t.Run("a nil notesFile behaves as no notes, not a panic", func(t *testing.T) {
		m := &model{cards: cards} // notesFile left at its zero value (nil)
		if got := m.cardBadges(0); got != "" {
			t.Errorf("cardBadges(0) = %q, want \"\"", got)
		}
	})

	t.Run("a note attached to this card's (Kind, Title) shows the note glyph", func(t *testing.T) {
		sc := notes.New()
		sc.Set("func", "func Foo() error", "why this exists")
		m := &model{cards: cards, notesFile: sc}
		want := "  " + noteGlyph
		if got := m.cardBadges(0); got != want {
			t.Errorf("cardBadges(0) = %q, want %q", got, want)
		}
		if got := m.cardBadges(1); got != "" {
			t.Errorf("cardBadges(1) = %q, want \"\" (no note attached to card 1)", got)
		}
	})

	t.Run("references show the ref glyph and count", func(t *testing.T) {
		m := &model{cards: cards, refs: [][]int{nil, {0}}}
		want := "  " + refGlyph + " 1"
		if got := m.cardBadges(1); got != want {
			t.Errorf("cardBadges(1) = %q, want %q", got, want)
		}
		if got := m.cardBadges(0); got != "" {
			t.Errorf("cardBadges(0) = %q, want \"\" (nothing references card 0)", got)
		}
	})

	t.Run("a refs slice shorter than cards (e.g. right after an insert) is a safe no-badge, not a panic", func(t *testing.T) {
		m := &model{cards: cards, refs: [][]int{{1}}} // len 1, but cards has 2
		if got := m.cardBadges(1); got != "" {
			t.Errorf("cardBadges(1) = %q, want \"\"", got)
		}
	})

	t.Run("both badges together", func(t *testing.T) {
		sc := notes.New()
		sc.Set("func", "func Foo() error", "note text")
		m := &model{cards: cards, notesFile: sc, refs: [][]int{nil, {0}}}
		want := "  " + noteGlyph
		if got := m.cardBadges(0); got != want {
			t.Errorf("cardBadges(0) = %q, want %q", got, want)
		}
	})

	t.Run("a flagged card shows the matching glyph", func(t *testing.T) {
		sc := notes.New()
		sc.ToggleFlag("func", "func Foo() error", flagTodo)
		m := &model{cards: cards, notesFile: sc}
		want := "  " + todoGlyph
		if got := m.cardBadges(0); got != want {
			t.Errorf("cardBadges(0) = %q, want %q", got, want)
		}
		if got := m.cardBadges(1); got != "" {
			t.Errorf("cardBadges(1) = %q, want \"\" (not flagged)", got)
		}
	})

	t.Run("flags render before note/refs badges, both flags together", func(t *testing.T) {
		sc := notes.New()
		sc.ToggleFlag("func", "func Foo() error", flagNeedsReview)
		sc.ToggleFlag("func", "func Foo() error", flagTodo)
		sc.Set("func", "func Foo() error", "a note too")
		m := &model{cards: cards, notesFile: sc, refs: [][]int{{1}, nil}}
		want := "  " + todoGlyph + "  " + needsReviewGlyph + "  " + noteGlyph + "  " + refGlyph + " 1"
		if got := m.cardBadges(0); got != want {
			t.Errorf("cardBadges(0) = %q, want %q", got, want)
		}
	})
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
