package main

import (
	"testing"

	"github.com/sandgorgon/9ed/deck"
)

// TestUpdateP9WriteMsg exercises model.Update's p9WriteMsg case
// directly — the piece fs9p_test.go's TestFS9PWrite can't reach, since
// its runP9WriteConsumer simulates Update's behavior rather than
// calling the real thing. Covers both the normal-commit path and the
// defensive out-of-range guard (a card index that Walk once found
// valid but no longer is by the time Close's commit lands — e.g. a
// Save resegmented the deck in between).
func TestUpdateP9WriteMsg(t *testing.T) {
	src := []byte("package foo\n\nfunc F() {}\n")
	cards := deck.GoSegmenter{}.Segment(src) // [0]=preamble, [1]=func
	writes := make(chan p9WriteMsg, 1)
	m := newModel("f.go", src, deck.GoSegmenter{}, cards, writes, nil)

	result := make(chan error, 1)
	mm, _ := m.Update(p9WriteMsg{cardIdx: 1, content: []byte("func F() { /* new */ }"), result: result})
	m = mm.(*model)

	if err := <-result; err != nil {
		t.Fatalf("write to a valid card index: unexpected error %v", err)
	}
	if got := m.cardBody(1); got != "func F() { /* new */ }" {
		t.Errorf("cardBody(1) after write = %q", got)
	}
	if got := m.view.snapshot().cardBody(1); got != "func F() { /* new */ }" {
		t.Errorf("view snapshot not updated: cardBody(1) = %q", got)
	}

	result2 := make(chan error, 1)
	if _, cmd := m.Update(p9WriteMsg{cardIdx: 99, content: []byte("x"), result: result2}); cmd == nil {
		t.Error("expected Update to reschedule waitForP9Write even after an out-of-range write")
	}
	if err := <-result2; err == nil {
		t.Error("expected an error writing an out-of-range card index, got nil")
	}
}

// TestUpdateP9GotoMsg exercises model.Update's p9GotoMsg case directly
// — the model-level effect TestFS9PGoto (fs9p_test.go) deliberately
// doesn't cover, the same split TestUpdateP9WriteMsg draws for writes.
// Confirms it actually calls goToLine (cursor/editing/gotoLineCursor
// all land, not just "some Cmd got returned"), applies regardless of
// current mode, and never fails (an out-of-range line just clamps,
// like {n}G's own behavior — see goto.go's lineOffset).
func TestUpdateP9GotoMsg(t *testing.T) {
	src := []byte("package foo\n\nfunc F() {}\n\nfunc G() {}\n")
	cards := deck.GoSegmenter{}.Segment(src) // [0]=preamble, [1]=F, [2]=G
	gotos := make(chan p9GotoMsg, 1)
	m := newModel("f.go", src, deck.GoSegmenter{}, cards, nil, gotos)

	result := make(chan error, 1)
	mm, cmd := m.Update(p9GotoMsg{line: 5, result: result}) // line 5 is "func G() {}"
	m = mm.(*model)

	if err := <-result; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd == nil {
		t.Error("expected Update to reschedule waitForP9Goto")
	}
	if !m.editing || m.cursor != 2 {
		t.Errorf("editing=%v cursor=%d, want editing=true cursor=2 (func G)", m.editing, m.cursor)
	}
	if m.gotoLineCursor == nil || m.gotoLineCard != 2 {
		t.Errorf("gotoLineCursor=%v gotoLineCard=%d, want a non-nil offset into card 2", m.gotoLineCursor, m.gotoLineCard)
	}

	t.Run("applies even while already editing a different card", func(t *testing.T) {
		m.cursor, m.editing = 1, true // pretend the user is mid-edit elsewhere
		result2 := make(chan error, 1)
		mm, _ := m.Update(p9GotoMsg{line: 5, result: result2})
		m = mm.(*model)
		if err := <-result2; err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.cursor != 2 {
			t.Errorf("cursor = %d, want 2 (plumb request applied regardless of current mode)", m.cursor)
		}
	})

	t.Run("an out-of-range line clamps rather than erroring", func(t *testing.T) {
		result3 := make(chan error, 1)
		mm, _ := m.Update(p9GotoMsg{line: 999, result: result3})
		m = mm.(*model)
		if err := <-result3; err != nil {
			t.Errorf("expected nil error for an out-of-range line (clamps), got %v", err)
		}
	})
}
