// Whole-file find-and-replace (the second half of backlog item 1, see
// project memory): typing a replacement into '/' search's second field
// (Ctrl+R, see search.go) and pressing Enter starts a confirm-each-match
// walk across every card whose body matches the pattern — Vim's
// ":s///gc" convention (y/n/a/q), chosen over a single "replace
// everything" confirmation since a replace can't be undone once you
// leave a card (undo is per-card and ephemeral — see the "no full-text
// search/replace" / "undo doesn't survive leaving a card" backlog
// items; this feature intentionally doesn't fix that one).
//
// The walk never pre-computes a fixed list of match offsets across the
// whole operation: each step re-scans the *current* body fresh via
// bodyMatches, so a replacement that changes the body's length never
// invalidates a stale offset for a later match in the same card — it
// simply isn't computed until its turn.

package main

import (
	"fmt"

	"github.com/sandgorgon/tui/cell"
	"github.com/sandgorgon/tui/layout"
	"github.com/sandgorgon/tui/tui"
)

// startReplace begins the confirm-replace walk for m.query/m.replaceWith
// (called from commitSearchMsg once both are known non-empty and a
// candidate exists). Candidates are fixed up front — which *cards* have
// a match — but each card's own matches are always found fresh, not
// cached, for the reason in this file's top comment. A no-op, leaving
// search mode active, if nothing actually has a body match (mirrors
// commitSearchMsg's existing "zero matches" no-op for a plain search).
func (m *model) startReplace() {
	re, ok := searchRegexp(m.query)
	if !ok {
		return
	}
	var candidates []int
	for i := range m.cards {
		if len(bodyMatches(re, m.cardBody(i))) > 0 {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return
	}
	m.searching = false
	m.replacing = true
	m.replaceDone = false
	m.activeSearch = m.query
	m.replaceCandidates = candidates
	m.replaceCandPos = 0
	m.replaceFrom = 0
	m.replaceCount = 0
	m.replaceSkipped = 0
	m.advanceReplace()
}

// advanceReplace finds the next pending match at or after m.replaceFrom
// in the candidate card at m.replaceCandPos, moving on to later
// candidates (each restarting its own search from offset 0) as the
// current one is exhausted, and setting m.cursor/m.replacePending to
// the one it lands on. Sets m.replaceDone once every candidate is
// exhausted — the walk's normal end, not an error.
func (m *model) advanceReplace() {
	re, ok := searchRegexp(m.activeSearch)
	if !ok {
		m.replaceDone = true
		return
	}
	for m.replaceCandPos < len(m.replaceCandidates) {
		idx := m.replaceCandidates[m.replaceCandPos]
		if span, ok := nextMatchFrom(bodyMatches(re, m.cardBody(idx)), m.replaceFrom); ok {
			m.cursor = idx
			m.replacePending = span
			return
		}
		m.replaceCandPos++
		m.replaceFrom = 0
	}
	m.replaceDone = true
}

// acceptReplace rewrites the pending match with m.replaceWith ('y'), and
// advances. Resuming from just past the *inserted replacement* (not the
// original match) means a replacement that itself contains the pattern
// (e.g. "foo" -> "foofoo") is never re-matched — ordinary non-overlapping
// substitution semantics, the same guarantee sed/vim give.
func (m *model) acceptReplace() {
	idx := m.replaceCandidates[m.replaceCandPos]
	runes := []rune(m.cardBody(idx))
	start, end := m.replacePending[0], m.replacePending[1]
	newBody := string(runes[:start]) + m.replaceWith + string(runes[end:])
	m.setEdited(idx, newBody)
	m.replaceCount++
	m.replaceFrom = start + len([]rune(m.replaceWith))
	m.view.publish(m.path, m.src, m.cards, m.edited)
	m.advanceReplace()
}

// skipReplace leaves the pending match untouched ('n') and advances,
// resuming just past it so the same match isn't offered again.
func (m *model) skipReplace() {
	m.replaceSkipped++
	m.replaceFrom = m.replacePending[1]
	m.advanceReplace()
}

// replaceAllRemaining accepts the pending match and every match after
// it without asking again ('a') — a bounded loop: acceptReplace's own
// advanceReplace call sets m.replaceDone once nothing is left.
func (m *model) replaceAllRemaining() {
	for !m.replaceDone {
		m.acceptReplace()
	}
}

// lineAround splits body's line containing [start, end) into the text
// before the match, the match itself, and the text after — for
// replaceView's one-line-of-context display. Operates on []rune
// throughout to stay consistent with bodyMatches' rune-offset contract.
func lineAround(body string, start, end int) (before, match, after string) {
	runes := []rune(body)
	lineStart := start
	for lineStart > 0 && runes[lineStart-1] != '\n' {
		lineStart--
	}
	lineEnd := end
	for lineEnd < len(runes) && runes[lineEnd] != '\n' {
		lineEnd++
	}
	return string(runes[lineStart:start]), string(runes[start:end]), string(runes[end:lineEnd])
}

// replaceView renders the confirm-replace walk: while a match is
// pending, one line of context with the match itself picked out, plus a
// y/n/a/q prompt; once m.replaceDone, a closing summary instead.
//
// Deliberately no TextArea here, unlike editView: replace's y/n/a/q
// keys need to be *fully* claimed by Update (see its m.replacing case)
// and never reach a focused widget, but tui always delivers a raw key
// to both Update and the focused widget with no per-key way to
// suppress just some of them for one but not the other (confirmed
// reading tui/app.go's handleInput — the same fact search.go's own
// comment on Ctrl+R relies on). A focused TextArea would insert 'y' as
// literal text the instant Update also handled it as "yes". Rendering
// the context as plain, unfocused Text sidesteps the problem entirely:
// with no focusable widget in this frame's tree, every key reaches
// Update alone.
func (m *model) replaceView() tui.Node {
	theme := m.theme

	if m.replaceDone {
		// Not pluralS (built for "match"/"matches", i.e. an "es" suffix —
		// wrong for "replaced"/"skipped", which only ever need "d");
		// phrased to avoid needing a count-sensitive noun at all.
		summary := fmt.Sprintf("Replace done: %d replaced, %d skipped. Press any key to continue.",
			m.replaceCount, m.replaceSkipped)
		return tui.Box(layout.Vertical,
			tui.Child(layout.Fill(1), tui.Box(layout.Vertical)),
			tui.Child(layout.Length(1), tui.Text(summary, theme.MutedText())),
		).Margin(1)
	}

	card := m.cards[m.cursor]
	before, match, after := lineAround(m.cardBody(m.cursor), m.replacePending[0], m.replacePending[1])

	header := tui.Text(fmt.Sprintf("%s %s  (%d replaced, %d skipped)", card.Kind, card.Title, m.replaceCount, m.replaceSkipped),
		theme.MutedText())
	line := tui.Box(layout.Horizontal,
		tui.Child(layout.Length(len([]rune(before))), tui.Text(before, cell.Style{})),
		tui.Child(layout.Length(len([]rune(match))), tui.Text(match, searchMatchStyle(theme))),
		tui.Child(layout.Fill(1), tui.Text(after, cell.Style{})),
	)
	prompt := tui.Text(fmt.Sprintf("replace %q with %q?  y: yes   n: no   a: all remaining   q: stop", m.query, m.replaceWith),
		m.helpStyle())

	return tui.Box(layout.Vertical,
		tui.Child(layout.Length(1), header),
		tui.Child(layout.Fill(1), line),
		tui.Child(layout.Length(1), prompt),
	).Margin(1)
}
