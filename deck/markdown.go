package deck

import (
	"bytes"
	"strconv"
	"strings"
)

// MarkdownSegmenter segments Markdown source into one card per ATX heading
// line (# through ######), plus a leading "preamble" card for any content
// before the first heading — or, if the source has no headings at all, one
// "preamble" card covering the whole file. Heading detection is suspended
// inside fenced code blocks (``` or ~~~) so a '#' comment in a fenced
// shell/Python sample isn't mistaken for a heading.
type MarkdownSegmenter struct{}

func (MarkdownSegmenter) Segment(src []byte) []Card {
	if len(src) == 0 {
		return nil
	}
	type heading struct {
		start int
		level int
		title string
	}
	var headings []heading

	inFence := false
	for pos := 0; pos < len(src); {
		nl := bytes.IndexByte(src[pos:], '\n')
		var line []byte
		var next int
		if nl < 0 {
			line, next = src[pos:], len(src)
		} else {
			line, next = src[pos:pos+nl], pos+nl+1
		}
		// Trim a trailing \r for matching only — offsets below are
		// computed from pos/next, never from line's length, so CRLF
		// source doesn't throw off byte accounting.
		text := strings.TrimRight(string(line), "\r")

		switch {
		case isFenceDelimiter(text):
			inFence = !inFence
		case !inFence:
			if title, level, ok := markdownHeadingTitle(text); ok {
				headings = append(headings, heading{start: pos, level: level, title: title})
			}
		}
		pos = next
	}

	if len(headings) == 0 {
		return []Card{{Title: firstLine(src), Span: [2]int{0, len(src)}, Kind: "preamble"}}
	}

	var cards []Card
	if headings[0].start > 0 {
		cards = append(cards, Card{
			Title: firstLine(src[:headings[0].start]),
			Span:  [2]int{0, headings[0].start},
			Kind:  "preamble",
		})
	}
	for i, h := range headings {
		end := len(src)
		if i+1 < len(headings) {
			end = headings[i+1].start
		}
		// Name = Title: a heading's own text is the closest thing
		// Markdown has to a defined identifier — prose elsewhere can
		// refer back to a section by the same words ("see the
		// Configuration section"), so it's the natural cross-reference
		// target, phrase-length rather than a single token like the
		// other segmenters' Names. A bare heading's empty Title yields
		// an empty Name too, which is already Card.Name's "no name"
		// value — no special-casing needed.
		//
		// Kind is "H1".."H6", not a flat "heading" — the level is
		// structurally meaningful (Nav mode's Kind column and Edit
		// mode's status line show it directly), and it's also what
		// notes/flags key against (see notes.Sidecar), so a heading's
		// level changing on a later edit (promoting "## Foo" to "# Foo")
		// is treated as becoming a different card identity, the same as
		// a renamed func or a moved struct would be elsewhere in this
		// package — not a special case.
		cards = append(cards, Card{Title: h.title, Span: [2]int{h.start, end}, Kind: "H" + strconv.Itoa(h.level), Name: h.title})
	}
	return cards
}

// markdownHeadingTitle reports whether line is an ATX heading (1-6 leading
// '#'s followed by a space/tab, or nothing but the '#'s themselves) and,
// if so, its level (the number of leading '#'s) and trimmed title text
// with the leading #s and whitespace removed.
func markdownHeadingTitle(line string) (title string, level int, ok bool) {
	i := 0
	for i < len(line) && i < 6 && line[i] == '#' {
		i++
	}
	if i == 0 {
		return "", 0, false
	}
	if i == len(line) {
		return "", i, true // a bare "#".."######" line: a heading with an empty title
	}
	if line[i] != ' ' && line[i] != '\t' {
		return "", 0, false // a 7th '#', or a non-heading run of '#'s glued to text
	}
	return strings.TrimSpace(line[i:]), i, true
}

// isFenceDelimiter reports whether line opens or closes a fenced code
// block: at least three backticks or tildes, optionally indented, of the
// same character.
func isFenceDelimiter(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) < 3 {
		return false
	}
	c := trimmed[0]
	if c != '`' && c != '~' {
		return false
	}
	// Any line starting with 3+ of the same fence char toggles the fence
	// state, whether it's an opening fence with a language tag after it
	// (e.g. "```go") or a bare closing fence.
	return strings.HasPrefix(trimmed, strings.Repeat(string(c), 3))
}

// firstLine returns the trimmed first line of b, or "" if b is empty.
func firstLine(b []byte) string {
	if nl := bytes.IndexByte(b, '\n'); nl >= 0 {
		b = b[:nl]
	}
	return strings.TrimSpace(string(b))
}
