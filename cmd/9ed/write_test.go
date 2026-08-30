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
	m := newModel("f.go", src, deck.GoSegmenter{}, cards, writes)

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
