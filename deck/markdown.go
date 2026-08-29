package deck

import (
	"bytes"
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
			if title, ok := markdownHeadingTitle(text); ok {
				headings = append(headings, heading{start: pos, title: title})
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
		cards = append(cards, Card{Title: h.title, Span: [2]int{h.start, end}, Kind: "heading"})
	}
	return cards
}

// markdownHeadingTitle reports whether line is an ATX heading (1-6 leading
// '#'s followed by a space/tab, or nothing but the '#'s themselves) and,
// if so, its trimmed title text with the leading #s and whitespace
// removed.
func markdownHeadingTitle(line string) (title string, ok bool) {
	i := 0
	for i < len(line) && i < 6 && line[i] == '#' {
		i++
	}
	if i == 0 {
		return "", false
	}
	if i == len(line) {
		return "", true // a bare "#".."######" line: a heading with an empty title
	}
	if line[i] != ' ' && line[i] != '\t' {
		return "", false // a 7th '#', or a non-heading run of '#'s glued to text
	}
	return strings.TrimSpace(line[i:]), true
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
