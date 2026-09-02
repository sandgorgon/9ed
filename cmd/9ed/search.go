// Nav-mode typeahead filter (M11, extended to full-text body search as
// backlog item 1): '/' starts it; every key while searching is handled
// by searchKeyEvent instead of listEvent's normal switch, since letters
// that are otherwise single-key commands (j/k/g/G/o/O) are now query
// text. A card matches if the query matches its Title *or* its body —
// title-only matching was the M11-era gap: `/` could find "the function
// named X" but never "the function that calls X". Matching is
// case-insensitive regexp (regexp.Compile with a "(?i)" prefix), not
// fuzzy/subsequence scoring — a plain literal query still behaves like
// the old substring search (Go's regexp matches anywhere in the string
// by default), while also allowing real patterns when needed.

package main

import (
	"regexp"
	"slices"

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

// searchRegexp compiles query as a case-insensitive regexp — the same
// default sensitivity the old plain-substring filter always used. ok is
// false for a pattern that fails to compile (e.g. an unbalanced "(" typed
// mid-query, still incomplete); callers treat that the same as "matches
// nothing" rather than surfacing regexp.Compile's own error, since the
// query is usually mid-edit, not finished, whenever this happens.
func searchRegexp(query string) (re *regexp.Regexp, ok bool) {
	re, err := regexp.Compile("(?i)" + query)
	return re, err == nil
}

// filteredIndices returns the indices into m.cards whose Title or body
// matches query, or every index if query is empty — the state right
// after '/' before anything's been typed. An invalid in-progress regexp
// matches nothing rather than everything or panicking.
func (m *model) filteredIndices() []int {
	if m.query == "" {
		idxs := make([]int, len(m.cards))
		for i := range idxs {
			idxs[i] = i
		}
		return idxs
	}
	re, ok := searchRegexp(m.query)
	if !ok {
		return nil
	}
	var idxs []int
	for i, c := range m.cards {
		if re.MatchString(c.Title) || re.MatchString(m.cardBody(i)) {
			idxs = append(idxs, i)
		}
	}
	return idxs
}

// bodyMatches returns every non-overlapping match of re in body as
// [start,end) rune-offset pairs, in order — the unit
// widget.TextArea.Highlights/InitialCursor both use. re.FindAllStringIndex
// reports byte offsets, translated via highlight.go's byteToRuneOffsets
// for correctness on non-ASCII content. A zero-width match (e.g. the
// pattern "a*" against a body with no "a") is dropped: it isn't a
// meaningful place to jump to or highlight, and would otherwise let a
// degenerate pattern "match" at every position.
func bodyMatches(re *regexp.Regexp, body string) [][2]int {
	byteMatches := re.FindAllStringIndex(body, -1)
	if len(byteMatches) == 0 {
		return nil
	}
	byteToRune := byteToRuneOffsets(body)
	var spans [][2]int
	for _, bm := range byteMatches {
		if bm[0] == bm[1] {
			continue
		}
		spans = append(spans, [2]int{byteToRune[bm[0]], byteToRune[bm[1]]})
	}
	return spans
}

// nextMatchFrom returns the first of matches (sorted ascending by Start,
// as bodyMatches produces them) with Start >= from, for jumping forward
// from a live cursor position. ok is false past the last match.
func nextMatchFrom(matches [][2]int, from int) (span [2]int, ok bool) {
	for _, sp := range matches {
		if sp[0] >= from {
			return sp, true
		}
	}
	return [2]int{}, false
}

// prevMatchBefore returns the last of matches with Start < from, for
// jumping backward from a live cursor position. Walks matches in
// reverse since they're sorted ascending. ok is false before the first
// match.
func prevMatchBefore(matches [][2]int, from int) (span [2]int, ok bool) {
	for i := len(matches) - 1; i >= 0; i-- {
		if matches[i][0] < from {
			return matches[i], true
		}
	}
	return [2]int{}, false
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
