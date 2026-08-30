package main

import (
	"strings"
	"testing"

	"github.com/sandgorgon/9ed/deck"
)

func TestReassembleRoundTrip(t *testing.T) {
	src := []byte("package foo\n\nvar A = 1\n\nvar B = 2\n")
	m := newModel("f.go", src, deck.GoSegmenter{}, deck.GoSegmenter{}.Segment(src), nil)

	if got := string(m.reassemble()); got != string(src) {
		t.Fatalf("unedited reassemble = %q, want %q", got, src)
	}
}

// TestReassembleInsertsMissingNewline is a regression test for a bug
// caught interactively (M4, tmux verification): editing a card so its
// new content no longer ends in '\n' silently glued the next card's
// first line onto it once reassembled, changing that next card's
// meaning entirely (a var declaration swallowed into a comment). See
// reassemble's comment for the fix.
func TestReassembleInsertsMissingNewline(t *testing.T) {
	src := []byte("package foo\n\nvar A = 1\n\nvar B = 2\n")
	cards := deck.GoSegmenter{}.Segment(src)
	m := newModel("f.go", src, deck.GoSegmenter{}, cards, nil)

	// Find the "var A" card and edit it to drop its trailing newline —
	// exactly what typing into TextArea without pressing Enter produces.
	idx := -1
	for i, c := range cards {
		if strings.Contains(c.Title, "var A") {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no card titled \"var A\" in %+v", cards)
	}
	edited := strings.TrimSuffix(m.cardBody(idx), "\n")
	m.setEdited(idx, edited)

	got := m.reassemble()
	if !strings.Contains(string(got), "\nvar B = 2\n") {
		t.Fatalf("reassemble() = %q — \"var B\" got glued onto the previous line instead of staying on its own", got)
	}

	// The result should also still segment back into the same shape:
	// "var B" must survive as its own card, not vanish into a comment.
	newCards := deck.GoSegmenter{}.Segment(got)
	found := false
	for _, c := range newCards {
		if strings.Contains(c.Title, "var B") {
			found = true
		}
	}
	if !found {
		t.Fatalf("re-segmenting reassemble()'s output lost the \"var B\" card entirely: %+v", newCards)
	}
}

// TestReassembleInsertedCard is the concrete scenario insertCard (M9)
// exists for: a new function typed into a card inserted between two
// existing ones reassembles correctly and re-segments into three real
// decls, the new one landing in between, not swallowed by either
// neighbor.
func TestReassembleInsertedCard(t *testing.T) {
	src := []byte("package foo\n\nfunc A() {}\n\nfunc B() {}\n")
	cards := deck.GoSegmenter{}.Segment(src) // [0]=preamble, [1]=A, [2]=B
	m := newModel("f.go", src, deck.GoSegmenter{}, cards, nil)

	m.insertCard(2, cards[1].Span[1]) // between A and B, matching insertBelow on A
	m.setEdited(2, "func New() {}\n")

	got := m.reassemble()
	newCards := deck.GoSegmenter{}.Segment(got)
	if len(newCards) != 4 {
		t.Fatalf("re-segmented into %d cards, want 4 (preamble, A, New, B): %+v", len(newCards), newCards)
	}
	if !strings.Contains(newCards[1].Title, "func A") || !strings.Contains(newCards[2].Title, "func New") || !strings.Contains(newCards[3].Title, "func B") {
		t.Fatalf("cards out of order or wrong: %+v", newCards)
	}
}
