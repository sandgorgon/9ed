// Go to a line number (M11/M12): vim's "{count}G" — a bare 'G' still
// means "last card" (see main.go's navG/navLast), but a digit sequence
// typed first (accumulated in model.pendingCount, see navDigitMsg)
// redirects it to goToLine instead, which both opens the card containing
// that line and (M12, once tui v0.2.0's TextAreaOptions.InitialCursor
// existed — see upstream-specs/tui-cursor-offset-and-numeric-gutter.md)
// places the cursor at the exact line within it.

package main

import (
	"bytes"
	"unicode/utf8"

	"github.com/sandgorgon/9ed/deck"
)

// navDigitMsg is produced by listEvent on a digit rune in Nav mode; its
// value is the digit itself.
type navDigitMsg rune

// goToLine moves the cursor to (and opens Edit mode on) the card
// containing line n (1-based), a no-op on an empty deck. Also records
// where within that card's body the line starts (model.gotoLineCursor/
// gotoLineCard), which editView consults to seed the TextArea's initial
// cursor position on its next mount — see main.go's editView and the
// plan's design notes on why two fields, and why every other edit-mode-
// entry path has to clear them.
func (m *model) goToLine(n int) {
	if len(m.cards) == 0 {
		return
	}
	offset := lineOffset(m.src, n)
	idx := cardContaining(m.cards, offset)
	m.cursor = idx
	m.editing = true

	// cardBody(idx) may be an edited override, not the original src
	// slice offset was computed against — clamping keeps the slice (and
	// therefore the rune count) valid regardless; if the card has
	// unsaved edits this session, the landing spot is best-effort, not
	// exact, an accepted narrow edge case.
	body := m.cardBody(idx)
	rel := min(max(offset-m.cards[idx].Span[0], 0), len(body))
	runeOffset := utf8.RuneCountInString(body[:rel])
	m.setJumpTarget(idx, runeOffset)
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

// cardFirstLine returns the file-absolute (1-based) line number of the
// byte at cardStart — used to seed editView's Gutter so line numbers are
// file-absolute, not restarting at 1 per card.
func cardFirstLine(src []byte, cardStart int) int {
	return 1 + bytes.Count(src[:cardStart], []byte("\n"))
}
