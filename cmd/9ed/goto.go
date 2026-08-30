// Go to a line number (M11): vim's "{count}G" — a bare 'G' still means
// "last card" (see main.go's navG/navLast), but a digit sequence typed
// first (accumulated in model.pendingCount, see navDigitMsg) redirects
// it to goToLine instead.
//
// This is the "coarse" version: it opens the card containing that line,
// not the exact line within it, since widget.TextAreaOptions has no way
// to set an initial cursor position — see
// upstream-specs/tui-cursor-offset-and-numeric-gutter.md, filed against
// tui to close that gap.
package main

import "github.com/sandgorgon/9ed/deck"

// navDigitMsg is produced by listEvent on a digit rune in Nav mode; its
// value is the digit itself.
type navDigitMsg rune

// goToLine moves the cursor to (and opens Edit mode on) the card
// containing line n (1-based), a no-op on an empty deck.
func (m *model) goToLine(n int) {
	if len(m.cards) == 0 {
		return
	}
	m.cursor = cardContaining(m.cards, lineOffset(m.src, n))
	m.editing = true
}

// lineOffset returns line n's (1-based) starting byte offset in src,
// clamped to len(src) if n is beyond EOF (and to line 1 if n < 1).
func lineOffset(src []byte, n int) int {
	if n <= 1 {
		return 0
	}
	line := 1
	for i, b := range src {
		if b == '\n' {
			line++
			if line == n {
				return i + 1
			}
		}
	}
	return len(src)
}

// cardContaining returns the index of the card whose Span contains
// offset, or the last card if offset is at or past every span — the
// half-open Span[1] boundary means offset == len(src) never falls inside
// any card's own [start,end) test.
func cardContaining(cards []deck.Card, offset int) int {
	for i, c := range cards {
		if offset >= c.Span[0] && offset < c.Span[1] {
			return i
		}
	}
	return len(cards) - 1
}
