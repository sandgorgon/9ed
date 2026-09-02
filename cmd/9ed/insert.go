// Nav-mode 'o'/'O' (M9): insert a new, empty card and drop straight into
// Edit mode on it — the "add a function between two others" gesture. A
// card is a view computed by a Segmenter, never stored, so a synthetic
// zero-width card (Span [pos,pos)) is safe to splice into m.cards
// directly: reassemble already treats an empty, unedited span as a no-op
// (see save.go), cardBody already falls through to src[pos:pos] == "",
// and fs9p.go already iterates m.cards generically. The one real cost is
// that edited is keyed by raw index, so an insertion has to shift every
// key at or above the insertion point.

package main

import "github.com/sandgorgon/9ed/deck"

// newCardKind is the placeholder Card.Kind for a card inserted by
// insertCard, before Save's resegmentation gives it a real one.
const newCardKind = "new"

// insertMsg is produced by listEvent (see main.go) on 'o'/'O' in Nav
// mode.
type insertMsg int

const (
	insertBelow insertMsg = iota // 'o': after the cursor
	insertAbove                  // 'O': before the cursor
)

// insertCard splices a new empty card (Span [pos,pos), Kind newCardKind)
// into m.cards at idx, shifting every later card — and every index-keyed
// per-card map's keys at or above idx — up by one.
func (m *model) insertCard(idx, pos int) {
	m.cards = append(m.cards, deck.Card{})
	copy(m.cards[idx+1:], m.cards[idx:])
	m.cards[idx] = deck.Card{Span: [2]int{pos, pos}, Kind: newCardKind}

	m.edited = shiftKeysForInsert(m.edited, idx)
	m.noteEdited = shiftKeysForInsert(m.noteEdited, idx)
}

// shiftKeysForInsert reindexes an index-keyed per-card map to account
// for insertCard splicing a new card in at idx: every key at or above
// idx moves up by one, the same shift m.cards itself just got. Shared
// by every such map (m.edited, m.noteEdited) — each needs identical
// reindexing, just over a different value type. A nil/empty m is
// returned unchanged rather than allocating a fresh empty map.
func shiftKeysForInsert[V any](m map[int]V, idx int) map[int]V {
	if len(m) == 0 {
		return m
	}
	shifted := make(map[int]V, len(m))
	for i, v := range m {
		if i >= idx {
			i++
		}
		shifted[i] = v
	}
	return shifted
}

// isEmptyInsert reports whether m.cards[i] is an untouched card from
// insertCard — never edited, so leaving it (via Esc or a cross-card
// jump — see abandonEmptyInsert/jumpCard) should abandon it rather than
// leave a blank "new" row behind.
func (m *model) isEmptyInsert(i int) bool {
	return m.cards[i].Kind == newCardKind && m.cardBody(i) == ""
}

// abandonEmptyInsert removes the current card if isEmptyInsert(m.cursor)
// — used directly by Esc, and by jumpCard before it moves the cursor
// elsewhere.
func (m *model) abandonEmptyInsert() {
	if !m.isEmptyInsert(m.cursor) {
		return
	}
	m.removeCard(m.cursor)
	if m.cursor >= len(m.cards) {
		m.cursor = max(len(m.cards)-1, 0)
	}
	m.view.publish(m.path, m.src, m.cards, m.edited)
}

// jumpCard moves the cursor by delta while staying in Edit mode —
// bounded, not wrapping, matching navUp/navDown. If the card being left
// is an untouched insert, abandonEmptyInsert removes it first; a
// forward jump (delta > 0) then returns immediately rather than moving
// again, since removing a card shifts everything after it down by one,
// so the cursor (left unchanged by the removal, barring the removed
// card having been last) already points at what was originally the
// *next* card — applying +1 on top of that would overshoot by one. A
// backward jump doesn't have this problem: removal never moves anything
// before the removed index, so the normal -1 from that same unchanged
// cursor still lands correctly on what was originally at idx-1.
//
// Restoring the outgoing card's cursor position on a later return has
// been attempted twice (tui v0.3.0's OnCursorChange, then again after
// v0.3.1) and reverted both times after live testing — not for a lack
// of trying, and not a 9ed design choice:
//  1. v0.3.0: OnCursorChange delivered asynchronously (tui's Cmd/
//     channel pipeline), so this synchronous cursor-index change could
//     run — and the next card's TextArea could mount — before the last
//     pending position update for the card being left had arrived.
//     Filed tui#18; fixed in tui v0.3.1 (Run now resolves a
//     widget-sourced Cmd synchronously before advancing to the next
//     input event).
//  2. v0.3.1, re-tested: the same race is gone, but a second, distinct
//     bug surfaced immediately behind it — confirmed by instrumenting
//     both sides and reading widget/textarea.go's handleKey directly:
//     unlike Ctrl+Left/Right/Home/End (each has an explicit ctrl-
//     guarded case checked first), Ctrl+Up/Down have no such guard, so
//     they fall through to the plain Up/Down case and move the
//     widget's own cursor too. Since the raw KeyEvent that triggers
//     this jump is *also* delivered to the focused widget (tui always
//     does both), the freshly-remounted destination card's widget
//     immediately "eats" that same keystroke as an extra vertical
//     move right after mounting with the correct restored position —
//     overwriting it before the user ever sees it. See
//     upstream-specs/tui-textarea-ctrl-updown-not-claimed.md.
//
// Don't re-attempt this without checking that spec's status first.
func (m *model) jumpCard(delta int) {
	m.gotoLineCursor = nil
	abandoning := m.isEmptyInsert(m.cursor)
	if abandoning {
		m.abandonEmptyInsert()
		if delta > 0 {
			return
		}
	}
	next := m.cursor + delta
	if next < 0 || next >= len(m.cards) {
		return
	}
	m.cursor = next
}

// removeCard splices m.cards[idx] out, shifting every later card — and
// every index-keyed per-card map's keys above idx — down by one,
// dropping any entry at idx. Only used to silently undo an insertCard
// the user backed out of without typing anything (see Update's Esc
// handling) — deleting a card with real content instead happens by
// emptying its body and letting Save's resegmentation drop it, not
// through this.
func (m *model) removeCard(idx int) {
	m.cards = append(m.cards[:idx], m.cards[idx+1:]...)
	m.edited = shiftKeysForRemove(m.edited, idx)
	m.noteEdited = shiftKeysForRemove(m.noteEdited, idx)
}

// shiftKeysForRemove is shiftKeysForInsert's inverse for removeCard:
// drops the entry at idx entirely (its card is gone) and moves every
// key above idx down by one.
func shiftKeysForRemove[V any](m map[int]V, idx int) map[int]V {
	if len(m) == 0 {
		return m
	}
	shifted := make(map[int]V, len(m))
	for i, v := range m {
		switch {
		case i == idx:
			continue
		case i > idx:
			i--
		}
		shifted[i] = v
	}
	return shifted
}
