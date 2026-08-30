package main

import (
	"testing"

	"github.com/sandgorgon/tui/input"

	"github.com/sandgorgon/9ed/deck"
)

func TestInsertCard(t *testing.T) {
	base := func() []deck.Card {
		return []deck.Card{
			{Title: "A", Span: [2]int{0, 5}, Kind: "func"},
			{Title: "B", Span: [2]int{5, 10}, Kind: "func"},
		}
	}

	t.Run("into an empty deck", func(t *testing.T) {
		m := &model{cards: nil}
		m.insertCard(0, 0)
		if len(m.cards) != 1 || m.cards[0].Kind != newCardKind || m.cards[0].Span != [2]int{0, 0} {
			t.Fatalf("cards = %+v", m.cards)
		}
	})

	t.Run("at the front", func(t *testing.T) {
		m := &model{cards: base()}
		m.insertCard(0, 0)
		if len(m.cards) != 3 || m.cards[0].Kind != newCardKind || m.cards[1].Title != "A" || m.cards[2].Title != "B" {
			t.Fatalf("cards = %+v", m.cards)
		}
	})

	t.Run("in the middle, between two cards", func(t *testing.T) {
		m := &model{cards: base()}
		m.insertCard(1, 5)
		if len(m.cards) != 3 || m.cards[0].Title != "A" || m.cards[1].Kind != newCardKind || m.cards[1].Span != [2]int{5, 5} || m.cards[2].Title != "B" {
			t.Fatalf("cards = %+v", m.cards)
		}
	})

	t.Run("at the end", func(t *testing.T) {
		m := &model{cards: base()}
		m.insertCard(2, 10)
		if len(m.cards) != 3 || m.cards[2].Kind != newCardKind || m.cards[2].Span != [2]int{10, 10} {
			t.Fatalf("cards = %+v", m.cards)
		}
	})

	t.Run("shifts edited keys at or above the insertion point", func(t *testing.T) {
		m := &model{cards: base(), edited: map[int]string{0: "edited A", 1: "edited B"}}
		m.insertCard(1, 5)
		if m.edited[0] != "edited A" {
			t.Errorf("edited[0] = %q, want unchanged", m.edited[0])
		}
		if _, ok := m.edited[1]; ok {
			t.Errorf("edited[1] should have shifted away, still present: %q", m.edited[1])
		}
		if m.edited[2] != "edited B" {
			t.Errorf("edited[2] = %q, want shifted-up \"edited B\"", m.edited[2])
		}
	})
}

func TestRemoveCard(t *testing.T) {
	base := func() []deck.Card {
		return []deck.Card{
			{Title: "A", Span: [2]int{0, 5}, Kind: "func"},
			{Title: "new", Span: [2]int{5, 5}, Kind: newCardKind},
			{Title: "B", Span: [2]int{5, 10}, Kind: "func"},
		}
	}

	t.Run("removes the target and closes the gap", func(t *testing.T) {
		m := &model{cards: base()}
		m.removeCard(1)
		if len(m.cards) != 2 || m.cards[0].Title != "A" || m.cards[1].Title != "B" {
			t.Fatalf("cards = %+v", m.cards)
		}
	})

	t.Run("drops an edited entry at the removed index and shifts later ones down", func(t *testing.T) {
		m := &model{cards: base(), edited: map[int]string{0: "edited A", 1: "stray", 2: "edited B"}}
		m.removeCard(1)
		if m.edited[0] != "edited A" {
			t.Errorf("edited[0] = %q, want unchanged", m.edited[0])
		}
		if v, ok := m.edited[1]; !ok || v != "edited B" {
			t.Errorf("edited[1] = %q, ok=%v, want shifted-down \"edited B\"", v, ok)
		}
		if _, ok := m.edited[2]; ok {
			t.Error("edited[2] should be gone after the shift")
		}
	})

	t.Run("insertCard then removeCard round-trips to the original deck", func(t *testing.T) {
		m := &model{cards: []deck.Card{
			{Title: "A", Span: [2]int{0, 5}, Kind: "func"},
			{Title: "B", Span: [2]int{5, 10}, Kind: "func"},
		}}
		before := append([]deck.Card(nil), m.cards...)
		m.insertCard(1, 5)
		m.removeCard(1)
		if len(m.cards) != len(before) || m.cards[0] != before[0] || m.cards[1] != before[1] {
			t.Fatalf("round-trip: got %+v, want %+v", m.cards, before)
		}
	})
}

// TestUpdateInsertMsg exercises Update's insertMsg case directly: cursor
// placement relative to insertBelow/insertAbove, dropping straight into
// Edit mode, and the empty-deck case.
func TestUpdateInsertMsg(t *testing.T) {
	newTestModel := func(cards []deck.Card) *model {
		return &model{path: "f.go", src: []byte("aaaaabbbbb"), cards: cards, view: &bufferView{}}
	}
	cards := func() []deck.Card {
		return []deck.Card{
			{Title: "A", Span: [2]int{0, 5}, Kind: "func"},
			{Title: "B", Span: [2]int{5, 10}, Kind: "func"},
		}
	}

	t.Run("insertBelow lands right after the cursor", func(t *testing.T) {
		m := newTestModel(cards())
		m.cursor = 0
		mm, _ := m.Update(insertBelow)
		m = mm.(*model)
		if !m.editing {
			t.Error("expected editing = true")
		}
		if m.cursor != 1 || m.cards[1].Kind != newCardKind {
			t.Fatalf("cursor=%d cards=%+v", m.cursor, m.cards)
		}
		if len(m.cards) != 3 || m.cards[0].Title != "A" || m.cards[2].Title != "B" {
			t.Fatalf("cards = %+v", m.cards)
		}
	})

	t.Run("insertAbove lands right on the cursor, pushing the old card down", func(t *testing.T) {
		m := newTestModel(cards())
		m.cursor = 1
		mm, _ := m.Update(insertAbove)
		m = mm.(*model)
		if m.cursor != 1 || m.cards[1].Kind != newCardKind {
			t.Fatalf("cursor=%d cards=%+v", m.cursor, m.cards)
		}
		if m.cards[0].Title != "A" || m.cards[2].Title != "B" {
			t.Fatalf("cards = %+v", m.cards)
		}
	})

	t.Run("insertAbove on card i lands the same place as insertBelow on card i-1", func(t *testing.T) {
		mAbove := newTestModel(cards())
		mAbove.cursor = 1
		mmAbove, _ := mAbove.Update(insertAbove)
		mAbove = mmAbove.(*model)

		mBelow := newTestModel(cards())
		mBelow.cursor = 0
		mmBelow, _ := mBelow.Update(insertBelow)
		mBelow = mmBelow.(*model)

		if mAbove.cards[1].Span != mBelow.cards[1].Span {
			t.Errorf("insertAbove(1).Span = %v, insertBelow(0).Span = %v, want equal", mAbove.cards[1].Span, mBelow.cards[1].Span)
		}
	})

	t.Run("on an empty deck, both insert the first card at position 0", func(t *testing.T) {
		m := newTestModel(nil)
		mm, _ := m.Update(insertBelow)
		m = mm.(*model)
		if len(m.cards) != 1 || m.cards[0].Span != [2]int{0, 0} || m.cursor != 0 || !m.editing {
			t.Fatalf("cards=%+v cursor=%d editing=%v", m.cards, m.cursor, m.editing)
		}
	})
}

var esc = input.KeyEvent{Key: input.KeyEsc}

// TestUpdateEscAbandonsEmptyInsert covers the Esc-handling addition:
// leaving an untouched inserted card removes it, but leaving one that's
// actually been typed into does not.
func TestUpdateEscAbandonsEmptyInsert(t *testing.T) {
	cards := []deck.Card{
		{Title: "A", Span: [2]int{0, 5}, Kind: "func"},
		{Title: "B", Span: [2]int{5, 10}, Kind: "func"},
	}

	t.Run("untouched insert is removed on Esc", func(t *testing.T) {
		m := &model{path: "f.go", src: []byte("aaaaabbbbb"), cards: append([]deck.Card(nil), cards...), view: &bufferView{}}
		mm, _ := m.Update(insertBelow)
		m = mm.(*model)
		if len(m.cards) != 3 {
			t.Fatalf("expected the insert to land, got %+v", m.cards)
		}

		mm, _ = m.Update(esc)
		m = mm.(*model)
		if m.editing {
			t.Error("expected editing = false after Esc")
		}
		if len(m.cards) != 2 || m.cards[0].Title != "A" || m.cards[1].Title != "B" {
			t.Fatalf("expected the untouched insert to be removed, got %+v", m.cards)
		}
	})

	t.Run("a typed-into insert survives Esc", func(t *testing.T) {
		m := &model{path: "f.go", src: []byte("aaaaabbbbb"), cards: append([]deck.Card(nil), cards...), view: &bufferView{}}
		mm, _ := m.Update(insertBelow)
		m = mm.(*model)

		mm, _ = m.Update(editChangedMsg{value: "func New() {}\n"})
		m = mm.(*model)

		mm, _ = m.Update(esc)
		m = mm.(*model)
		if m.editing {
			t.Error("expected editing = false after Esc")
		}
		if len(m.cards) != 3 || m.cardBody(1) != "func New() {}\n" {
			t.Fatalf("expected the typed-into insert to survive, got cards=%+v edited=%+v", m.cards, m.edited)
		}
	})
}

func TestListEventInsertKeys(t *testing.T) {
	if got := listEvent(input.KeyEvent{Rune: 'o'}); got != insertBelow {
		t.Errorf("listEvent('o') = %v, want insertBelow", got)
	}
	if got := listEvent(input.KeyEvent{Rune: 'O'}); got != insertAbove {
		t.Errorf("listEvent('O') = %v, want insertAbove", got)
	}
}
