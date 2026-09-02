package deck

import "regexp"

// References computes, for each card, which other cards mention its Name
// as a whole word (or, for a Markdown heading's phrase-length Name, whole
// phrase) somewhere in their own body — the data a system-derived
// "referenced by" badge is built from.
//
// This is lexical, not semantic: comments, string literals, and unrelated
// identically-named identifiers all count as hits, the same "good enough"
// bar the segmenters themselves already accept for Kind/Title (see e.g.
// BashSegmenter's and CSegmenter's own doc comments) — a badge is a hint,
// not a guarantee. It's computed in one pass over the whole deck (meant
// for a load/save-time refresh, not a per-keystroke one), not derived
// from any per-language semantic analysis, since only GoSegmenter has a
// real parser to begin with.
//
// The result is a slice parallel to cards: result[j] lists, in card
// order, the indices of cards whose body mentions cards[j].Name. It's nil
// for a card with an empty Name (nothing to search for) or with no
// referencers. A card is never counted as referencing itself.
func References(src []byte, cards []Card) [][]int {
	type target struct {
		idx int
		re  *regexp.Regexp
	}
	var targets []target
	compiled := make(map[string]*regexp.Regexp, len(cards))
	for j, c := range cards {
		if c.Name == "" {
			continue
		}
		re, ok := compiled[c.Name]
		if !ok {
			re = regexp.MustCompile(`\b` + regexp.QuoteMeta(c.Name) + `\b`)
			compiled[c.Name] = re
		}
		targets = append(targets, target{idx: j, re: re})
	}
	if len(targets) == 0 {
		return nil
	}

	refs := make([][]int, len(cards))
	for i, c := range cards {
		body := src[c.Span[0]:c.Span[1]]
		for _, t := range targets {
			if t.idx == i || cards[t.idx].Name == c.Name {
				// A card never references itself, and two cards that
				// happen to share a Name always "match" each other
				// this way — each one's own declaration line contains
				// its own name, which is also the other's — without
				// this being any real reference between them, so
				// that's excluded too rather than reported as noise.
				continue
			}
			if t.re.Match(body) {
				refs[t.idx] = append(refs[t.idx], i)
			}
		}
	}
	return refs
}
