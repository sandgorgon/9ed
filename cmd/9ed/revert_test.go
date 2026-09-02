package main

import (
	"testing"

	"github.com/sandgorgon/tui/input"

	"github.com/sandgorgon/9ed/deck"
)

func newRevertTestModel() *model {
	src := []byte("package p\n\nfunc Walrus() {}\n\nfunc Beta() {}\n")
	cards := deck.GoSegmenter{}.Segment(src) // 0: preamble, 1: Walrus, 2: Beta
	return &model{path: "f.go", src: src, cards: cards, view: &bufferView{}}
}

func TestRevertCard(t *testing.T) {
	t.Run("restores the original body and clears the dirty entry", func(t *testing.T) {
		m := newRevertTestModel()
		original := m.cardBody(1)
		m.setEdited(1, "func Walrus() { /* changed */ }")
		if m.cardBody(1) == original {
			t.Fatal("setup: setEdited didn't actually change cardBody")
		}

		m.revertCard(1)

		if got := m.cardBody(1); got != original {
			t.Errorf("cardBody(1) after revert = %q, want original %q", got, original)
		}
		if _, dirty := m.edited[1]; dirty {
			t.Error("m.edited[1] still present after revert, want cleared")
		}
	})

	t.Run("a clean card is a no-op", func(t *testing.T) {
		m := newRevertTestModel()
		m.revertCard(1) // never edited
		if _, dirty := m.edited[1]; dirty {
			t.Error("revertCard created a dirty entry on a clean card")
		}
	})

	t.Run("leaves a note-only edit untouched — scoped to body, not the sidecar", func(t *testing.T) {
		m := newRevertTestModel()
		m.noteEdited = map[int]bool{1: true}
		m.revertCard(1)
		if !m.noteEdited[1] {
			t.Error("revertCard cleared a note-only dirty mark, want it untouched")
		}
	})

	t.Run("only affects the requested card", func(t *testing.T) {
		m := newRevertTestModel()
		m.setEdited(1, "changed 1")
		m.setEdited(2, "changed 2")
		m.revertCard(1)
		if _, dirty := m.edited[1]; dirty {
			t.Error("card 1 still dirty after revert")
		}
		if got := m.cardBody(2); got != "changed 2" {
			t.Errorf("cardBody(2) = %q, want unaffected \"changed 2\"", got)
		}
	})
}

func TestRevertMsgRouting(t *testing.T) {
	t.Run("listEvent routes 'u' to revertMsg in Nav mode", func(t *testing.T) {
		m := &model{}
		if _, ok := m.listEvent(input.KeyEvent{Rune: 'u'}).(revertMsg); !ok {
			t.Errorf("listEvent('u') did not return revertMsg")
		}
	})

	t.Run("Update applies revertMsg to the current cursor card", func(t *testing.T) {
		m := newRevertTestModel()
		m.cursor = 1
		m.setEdited(1, "changed")
		mm, _ := m.Update(revertMsg{})
		m = mm.(*model)
		if _, dirty := m.edited[1]; dirty {
			t.Error("Update(revertMsg{}) left card 1 dirty")
		}
	})

	t.Run("Update on an empty deck is a no-op, not a panic", func(t *testing.T) {
		m := &model{}
		if _, cmd := m.Update(revertMsg{}); cmd != nil {
			t.Error("expected nil Cmd for revertMsg on an empty deck")
		}
	})
}
