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
// into m.cards at idx, shifting every later card — and every edited key
// at or above idx — up by one.
func (m *model) insertCard(idx, pos int) {
	m.cards = append(m.cards, deck.Card{})
	copy(m.cards[idx+1:], m.cards[idx:])
	m.cards[idx] = deck.Card{Span: [2]int{pos, pos}, Kind: newCardKind}

	if len(m.edited) == 0 {
		return
	}
	shifted := make(map[int]string, len(m.edited))
	for i, v := range m.edited {
		if i >= idx {
			i++
		}
		shifted[i] = v
	}
	m.edited = shifted
}

// removeCard splices m.cards[idx] out, shifting every later card — and
// every edited key above idx — down by one, dropping any entry at idx.
// Only used to silently undo an insertCard the user backed out of
// without typing anything (see Update's Esc handling) — deleting a card
// with real content instead happens by emptying its body and letting
// Save's resegmentation drop it, not through this.
func (m *model) removeCard(idx int) {
	m.cards = append(m.cards[:idx], m.cards[idx+1:]...)
	if len(m.edited) == 0 {
		return
	}
	shifted := make(map[int]string, len(m.edited))
	for i, v := range m.edited {
		switch {
		case i == idx:
			continue
		case i > idx:
			i--
		}
		shifted[i] = v
	}
	m.edited = shifted
}
