// Nav-mode typeahead filter (M11): '/' starts it; every key while
// searching is handled by searchKeyEvent instead of listEvent's normal
// switch, since letters that are otherwise single-key commands (j/k/g/G/
// o/O) are now query text. Filtering is a plain case-insensitive
// substring match against Title — not fuzzy/subsequence scoring, which
// would be a lot more code for a "jump to the function named X" use case
// substring matching already serves well.

package main

import (
	"slices"
	"strings"

	"github.com/sandgorgon/tui/input"
	"github.com/sandgorgon/tui/tui"
)

type startSearchMsg struct{}
type searchInputMsg struct{ r rune }
type searchBackspaceMsg struct{}
type searchMoveMsg struct{ delta int }
type cancelSearchMsg struct{}
type commitSearchMsg struct{}

// searchKeyEvent is listEvent's entire key-handling while
// model.searching is true. Movement uses the plain arrow keys, not j/k,
// since letters are query text now.
func (m *model) searchKeyEvent(ke input.KeyEvent) tui.Msg {
	switch {
	case ke.Key == input.KeyEsc && ke.Mod == 0:
		return cancelSearchMsg{}
	case ke.Key == input.KeyEnter:
		return commitSearchMsg{}
	case ke.Key == input.KeyBackspace:
		return searchBackspaceMsg{}
	case ke.Key == input.KeyUp:
		return searchMoveMsg{delta: -1}
	case ke.Key == input.KeyDown:
		return searchMoveMsg{delta: 1}
	case ke.Key == input.KeyNone && ke.Rune != 0 && ke.Mod&(input.ModCtrl|input.ModAlt) == 0:
		return searchInputMsg{r: ke.Rune}
	}
	return nil
}

// filteredIndices returns the indices into m.cards whose Title contains
// query (case-insensitive), or every index if query is empty — the
// state right after '/' before anything's been typed.
func (m *model) filteredIndices() []int {
	if m.query == "" {
		idxs := make([]int, len(m.cards))
		for i := range idxs {
			idxs[i] = i
		}
		return idxs
	}
	q := strings.ToLower(m.query)
	var idxs []int
	for i, c := range m.cards {
		if strings.Contains(strings.ToLower(c.Title), q) {
			idxs = append(idxs, i)
		}
	}
	return idxs
}

// snapCursorToFiltered keeps m.cursor inside the current filtered set
// whenever the query changes: it stays put if still a match, otherwise
// jumps to the first match. A no-op with zero matches — m.cursor stays
// wherever it was, since there's nothing sensible to snap to.
func (m *model) snapCursorToFiltered() {
	f := m.filteredIndices()
	if len(f) == 0 {
		return
	}
	if !slices.Contains(f, m.cursor) {
		m.cursor = f[0]
	}
}
