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
	"fmt"
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

// toggleReplaceFieldMsg (Ctrl+R) switches typing focus between the
// search pattern and the replacement text on the same '/' line — see
// searchKeyEvent's own comment for why this can't be Tab.
type toggleReplaceFieldMsg struct{}
type replaceInputMsg struct{ r rune }
type replaceBackspaceMsg struct{}

// searchKeyEvent is listEvent's entire key-handling while
// model.searching is true. Movement uses the plain arrow keys, not j/k,
// since letters are query text now. Ctrl+R, not Tab, switches between
// the pattern and replacement fields: List (always focused here) isn't
// a tui.RawKeyClaimer, so a bare Tab is claimed by the App itself to
// move focus among focusable widgets (tui/app.go's handleInput) and
// never reaches Update at all — confirmed by reading that dispatch path
// directly rather than assuming Tab would just work here the way it
// does inside a claimed widget like TextArea.
func (m *model) searchKeyEvent(ke input.KeyEvent) tui.Msg {
	switch {
	case ke.Key == input.KeyEsc && ke.Mod == 0:
		return cancelSearchMsg{}
	case ke.Key == input.KeyEnter:
		return commitSearchMsg{}
	case ke.Mod&input.ModCtrl != 0 && ke.Rune == 'r':
		return toggleReplaceFieldMsg{}
	case ke.Key == input.KeyBackspace:
		if m.enteringReplacement {
			return replaceBackspaceMsg{}
		}
		return searchBackspaceMsg{}
	case ke.Key == input.KeyUp:
		return searchMoveMsg{delta: -1}
	case ke.Key == input.KeyDown:
		return searchMoveMsg{delta: 1}
	case ke.Key == input.KeyNone && ke.Rune != 0 && ke.Mod&(input.ModCtrl|input.ModAlt) == 0:
		if m.enteringReplacement {
			return replaceInputMsg{r: ke.Rune}
		}
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

// setJumpTarget sets gotoLineCursor/gotoLineCard to jump the cursor to
// offset in card idx's body, and bumps jumpGen so editView's TextArea
// remounts even when idx == m.cursor already. Moving to a *different*
// card gets a remount for free (its Span differs, so its Key differs —
// see editView), but TextAreaOptions.InitialCursor is documented as
// read only once at mount, so repositioning within the *same*
// already-mounted card (search's Ctrl+N/Ctrl+P landing on another match
// in the card you're already reading) needs a manufactured remount to
// take effect at all — the same "InitialCursor won't just apply itself"
// constraint the jump/return cursor-restore saga (see jumpCard's doc
// comment) fought for a different reason.
func (m *model) setJumpTarget(idx, offset int) {
	m.gotoLineCursor = &offset
	m.gotoLineCard = idx
	m.jumpGen++
}

// liveCursorPos returns the current card's most recently known cursor
// offset: m.cursorPos if the TextArea has reported one via
// OnCursorChange, else the explicit gotoLineCursor target if this is
// the card it was just set for (the first frame after a jump, before
// any cursor-change event has arrived), else 0.
func (m *model) liveCursorPos() int {
	if pos, ok := m.cursorPos[m.cursor]; ok {
		return pos
	}
	if m.gotoLineCursor != nil && m.gotoLineCard == m.cursor {
		return *m.gotoLineCursor
	}
	return 0
}

// jumpToMatch moves the cursor to the next (delta > 0) or previous
// (delta < 0) occurrence of m.activeSearch, searching forward/backward
// from the live cursor position within the current card first, then
// wrapping around through the rest of the file — vim's "search wraps"
// convention, since the point of "find next" is exhausting every
// occurrence without hand-navigating cards. The wraparound walk's own
// bound (len(m.cards) steps, not len(m.cards)-1) deliberately includes
// coming back around to the current card itself: with exactly one match
// in the whole file, repeated Ctrl+N cycles back onto that same match
// rather than silently doing nothing. A no-op with no active search, an
// uncompilable one (can't happen once committed, but defensive), or an
// empty deck.
func (m *model) jumpToMatch(delta int) {
	if m.activeSearch == "" || len(m.cards) == 0 {
		return
	}
	re, ok := searchRegexp(m.activeSearch)
	if !ok {
		return
	}
	from := m.liveCursorPos()
	if delta > 0 {
		if matches := bodyMatches(re, m.cardBody(m.cursor)); len(matches) > 0 {
			if span, ok := nextMatchFrom(matches, from+1); ok {
				m.setJumpTarget(m.cursor, span[0])
				return
			}
		}
		for i := 1; i <= len(m.cards); i++ {
			idx := (m.cursor + i) % len(m.cards)
			if matches := bodyMatches(re, m.cardBody(idx)); len(matches) > 0 {
				m.cursor = idx
				m.setJumpTarget(idx, matches[0][0])
				return
			}
		}
		return
	}
	if matches := bodyMatches(re, m.cardBody(m.cursor)); len(matches) > 0 {
		if span, ok := prevMatchBefore(matches, from); ok {
			m.setJumpTarget(m.cursor, span[0])
			return
		}
	}
	for i := 1; i <= len(m.cards); i++ {
		idx := (m.cursor - i + len(m.cards)) % len(m.cards)
		if matches := bodyMatches(re, m.cardBody(idx)); len(matches) > 0 {
			m.cursor = idx
			m.setJumpTarget(idx, matches[len(matches)-1][0])
			return
		}
	}
}

// searchHelpLine renders the '/' status line: both fields (pattern and
// replacement), with the one currently receiving typed characters
// bracketed so Ctrl+R's effect is visible; a match count, or "invalid
// pattern" instead of "0 matches" when the regexp doesn't compile yet
// (still being typed) — distinguishing "no matches" from "not a valid
// pattern yet" was flagged as worth keeping in the design discussion,
// since the two look identical to filteredIndices (both return zero
// results) but mean different things to the person typing. Enter's
// label switches between "go" and "replace" depending on whether a
// replacement has been typed at all.
func (m *model) searchHelpLine(indices []int) string {
	pattern, replacement := m.query, m.replaceWith
	if m.enteringReplacement {
		replacement = "[" + replacement + "]"
	} else {
		pattern = "[" + pattern + "]"
	}
	status := fmt.Sprintf("%d match%s", len(indices), pluralS(len(indices)))
	if _, ok := searchRegexp(m.query); !ok {
		status = "invalid pattern"
	}
	action := "go"
	if m.replaceWith != "" {
		action = "replace"
	}
	return fmt.Sprintf("/%s  →  %s  (%s)  —  enter: %s   ^r: switch field   esc: cancel",
		pattern, replacement, status, action)
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
