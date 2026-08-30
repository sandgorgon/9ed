package main

import (
	"testing"

	"github.com/sandgorgon/tui/input"

	"github.com/sandgorgon/9ed/deck"
)

func TestLineOffset(t *testing.T) {
	src := []byte("aaa\nbbb\nccc\n")
	tests := []struct {
		line int
		want int
	}{
		{1, 0},
		{2, 4},
		{3, 8},
		{0, 0},   // clamped to line 1
		{-5, 0},  // clamped to line 1
		{99, 12}, // beyond EOF, clamped to len(src)
	}
	for _, tt := range tests {
		if got := lineOffset(src, tt.line); got != tt.want {
			t.Errorf("lineOffset(src, %d) = %d, want %d", tt.line, got, tt.want)
		}
	}
}

func TestCardContaining(t *testing.T) {
	cards := []deck.Card{
		{Span: [2]int{0, 4}},
		{Span: [2]int{4, 8}},
		{Span: [2]int{8, 12}},
	}
	tests := []struct {
		offset int
		want   int
	}{
		{0, 0},
		{3, 0},
		{4, 1},
		{7, 1},
		{8, 2},
		{11, 2},
		{12, 2}, // at len(src): past every half-open span, clamps to the last card
		{100, 2},
	}
	for _, tt := range tests {
		if got := cardContaining(cards, tt.offset); got != tt.want {
			t.Errorf("cardContaining(cards, %d) = %d, want %d", tt.offset, got, tt.want)
		}
	}
}

func TestGoToLine(t *testing.T) {
	src := []byte("aaa\nbbb\nccc\n")
	cards := []deck.Card{
		{Span: [2]int{0, 4}},
		{Span: [2]int{4, 8}},
		{Span: [2]int{8, 12}},
	}

	t.Run("jumps to the card containing the line and opens Edit mode", func(t *testing.T) {
		m := &model{src: src, cards: cards, view: &bufferView{}}
		m.goToLine(2)
		if m.cursor != 1 || !m.editing {
			t.Errorf("cursor=%d editing=%v, want cursor=1 editing=true", m.cursor, m.editing)
		}
	})

	t.Run("no-op on an empty deck", func(t *testing.T) {
		m := &model{src: src, cards: nil, view: &bufferView{}}
		m.goToLine(2)
		if m.editing {
			t.Error("expected editing to stay false on an empty deck")
		}
	})
}

// TestCountG covers Update's "{count}G" handling end to end, including
// the M10-style double-dispatch shape (each keypress reaches Update as
// both the raw KeyEvent and the corresponding Msg — see
// TestGoToFirstLast's equivalent gg regression test in main_test.go).
// The fixture is 20 lines across 4 cards of 5 lines each (5 bytes/line,
// "N\n" padded to a fixed width) so a genuine two-digit count lands
// unambiguously in a specific, non-last card.
func TestCountG(t *testing.T) {
	newTestModel := func() *model {
		var src []byte
		var cards []deck.Card
		for range 4 {
			start := len(src)
			for range 5 {
				src = append(src, []byte("xx\n")...)
			}
			cards = append(cards, deck.Card{Span: [2]int{start, len(src)}})
		}
		return &model{path: "f", src: src, cards: cards, view: &bufferView{}}
	}

	t.Run("bare G still goes to the last card when no count is pending", func(t *testing.T) {
		m := newTestModel()
		mm, _ := m.Update(navLast)
		m = mm.(*model)
		if m.cursor != 3 || m.editing {
			t.Errorf("cursor=%d editing=%v, want cursor=3 editing=false (plain nav, not goToLine)", m.cursor, m.editing)
		}
	})

	t.Run("a multi-digit count then G jumps to that line's card", func(t *testing.T) {
		m := newTestModel()
		pressDigit := func(r rune) {
			mm, _ := m.Update(input.KeyEvent{Rune: r})
			m = mm.(*model)
			mm, _ = m.Update(navDigitMsg(r))
			m = mm.(*model)
		}
		pressDigit('1')
		pressDigit('2') // pendingCount == "12" — line 12 is 5 lines into the
		// third card (lines 11-15), i.e. cards[2].
		mm, _ := m.Update(input.KeyEvent{Rune: 'G'})
		m = mm.(*model)
		mm, _ = m.Update(navLast)
		m = mm.(*model)

		if m.cursor != 2 || !m.editing {
			t.Errorf("cursor=%d editing=%v, want cursor=2 editing=true (line 12 is in the third card)", m.cursor, m.editing)
		}
		if m.pendingCount != "" {
			t.Errorf("pendingCount = %q, want cleared after G consumed it", m.pendingCount)
		}
	})

	t.Run("an unrelated key in between cancels a pending count", func(t *testing.T) {
		m := newTestModel()
		mm, _ := m.Update(navDigitMsg('9'))
		m = mm.(*model)
		mm, _ = m.Update(navDown) // unrelated — cancels pendingCount
		m = mm.(*model)
		mm, _ = m.Update(navLast) // bare G now, not "9G"
		m = mm.(*model)
		if m.cursor != 3 || m.editing {
			t.Errorf("cursor=%d editing=%v, want cursor=3 editing=false (a fresh bare G, not \"9G\")", m.cursor, m.editing)
		}
	})
}
