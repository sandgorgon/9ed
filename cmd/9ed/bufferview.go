package main

import (
	"maps"
	"sync"

	"github.com/sandgorgon/9ed/deck"
)

// bufferView is the thread-safe seam between the TUI's single-
// threaded Model (mutated only on tui's event-loop goroutine, per its
// Update contract) and the 9P server (its own goroutine per
// connection, see fs9p.go). The event loop calls publish after every
// Update; the 9P side only ever reads via snapshot — neither side
// touches the other's data structures directly.
type bufferView struct {
	mu     sync.RWMutex
	path   string
	src    []byte
	cards  []deck.Card
	edited map[int]string
}

// publish replaces the snapshot wholesale. edited is copied (it's a
// map, mutated in place by setEdited — src and cards are always
// wholesale-replaced by the model, never mutated in place, so those
// are safe to store by reference).
func (b *bufferView) publish(path string, src []byte, cards []deck.Card, edited map[int]string) {
	cp := make(map[int]string, len(edited))
	maps.Copy(cp, edited)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.path, b.src, b.cards, b.edited = path, src, cards, cp
}

// snapshot is a point-in-time, safe-to-read-without-locking copy of
// the fields the 9P filesystem needs.
type snapshot struct {
	path   string
	src    []byte
	cards  []deck.Card
	edited map[int]string
}

func (b *bufferView) snapshot() snapshot {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return snapshot{path: b.path, src: b.src, cards: b.cards, edited: b.edited}
}

// cardBody returns card i's current text under s, the same fallback
// rule as model.cardBody: an edited override if one exists, otherwise
// the original span out of src.
func (s snapshot) cardBody(i int) string {
	if v, ok := s.edited[i]; ok {
		return v
	}
	if i < 0 || i >= len(s.cards) {
		return ""
	}
	c := s.cards[i]
	return string(s.src[c.Span[0]:c.Span[1]])
}
