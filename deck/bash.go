package deck

import (
	"bytes"
	"regexp"
)

// BashSegmenter segments Bash/POSIX-shell source into one card per
// top-level function definition — `name() {`, `function name {`, or
// `function name() {` (brace on the same line, or alone on the next
// non-blank line, both common styles) — unindented (column 0) so a
// nested function inside another function's body isn't mistaken for a
// top-level one, plus a leading "preamble" card for the shebang line,
// top-level statements, and anything else before the first function. If
// the source has no top-level function definitions at all, it becomes
// one "preamble" card covering the whole file, the same fallback shape
// as MarkdownSegmenter/GoSegmenter. This is a "good enough" structural
// heuristic, not a real shell grammar — it doesn't track quoting or
// heredocs, so a function-like line inside a heredoc body would be
// (mis)detected the same as a real one.
type BashSegmenter struct{}

// bashFuncRe matches an unindented Bash function definition line whose
// '{' is on the same line: `function name`, `function name()`, or bare
// `name()`, each followed (with optional whitespace) by '{'. Exactly one
// of the two name groups is non-empty depending on which form matched.
var bashFuncRe = regexp.MustCompile(`^(?:function\s+[A-Za-z_][A-Za-z0-9_]*(?:\s*\(\s*\))?|[A-Za-z_][A-Za-z0-9_]*\s*\(\s*\))\s*\{`)

// bashFuncHeaderRe matches the same forms as bashFuncRe but with nothing
// after the name/parens — the brace-on-its-own-next-line style; Segment
// only treats a header match as a function def once it's confirmed the
// next non-blank line is a lone '{'.
var bashFuncHeaderRe = regexp.MustCompile(`^(?:function\s+[A-Za-z_][A-Za-z0-9_]*(?:\s*\(\s*\))?|[A-Za-z_][A-Za-z0-9_]*\s*\(\s*\))\s*$`)

func (BashSegmenter) Segment(src []byte) []Card {
	if len(src) == 0 {
		return nil
	}
	type fn struct {
		spanStart int // includes an attached leading '#' comment block, if any
		declStart int // the def line itself, for Title
	}
	var fns []fn

	pos := 0
	for pos < len(src) {
		lineStart := pos
		line, next := bashLine(src, pos)

		isFunc := bashFuncRe.Match(line)
		if !isFunc && bashFuncHeaderRe.Match(bytes.TrimRight(line, "\r")) {
			if brace, ok := bashNextNonBlankLine(src, next); ok && bytes.Equal(brace, []byte("{")) {
				isFunc = true
			}
		}
		if isFunc {
			fns = append(fns, fn{spanStart: bashAttachedCommentStart(src, lineStart), declStart: lineStart})
		}
		pos = next
	}

	if len(fns) == 0 {
		return []Card{{Title: firstLine(src), Span: [2]int{0, len(src)}, Kind: "preamble"}}
	}

	var cards []Card
	if fns[0].spanStart > 0 {
		cards = append(cards, Card{
			Title: firstLine(src),
			Span:  [2]int{0, fns[0].spanStart},
			Kind:  "preamble",
		})
	}
	for i, f := range fns {
		end := len(src)
		if i+1 < len(fns) {
			end = fns[i+1].spanStart
		}
		cards = append(cards, Card{Title: firstLine(src[f.declStart:]), Span: [2]int{f.spanStart, end}, Kind: "func"})
	}
	return cards
}

// bashLine returns the line starting at pos (excluding its trailing
// '\n') and the offset of the following line.
func bashLine(src []byte, pos int) (line []byte, next int) {
	if nl := bytes.IndexByte(src[pos:], '\n'); nl >= 0 {
		return src[pos : pos+nl], pos + nl + 1
	}
	return src[pos:], len(src)
}

// bashNextNonBlankLine returns the first non-blank line at or after pos,
// trimmed of surrounding whitespace, or ok=false if only blank lines
// remain before EOF.
func bashNextNonBlankLine(src []byte, pos int) (line []byte, ok bool) {
	for pos < len(src) {
		l, next := bashLine(src, pos)
		if t := bytes.TrimSpace(l); len(t) > 0 {
			return t, true
		}
		pos = next
	}
	return nil, false
}

// bashAttachedCommentStart walks backward from declStart (the byte
// offset of a function definition's own line) over an unbroken run of
// unindented '#' comment lines immediately above it — no blank line in
// between — and returns where that run begins, so a function's own doc
// comment travels with it into its card's span, the same as GoSegmenter's
// Doc attachment. Returns declStart unchanged if there's no such run
// (including when declStart is 0, or the line above is blank or the
// shebang).
func bashAttachedCommentStart(src []byte, declStart int) int {
	start := declStart
	for start > 0 {
		// Find the previous line: [prevStart, start) is the line just
		// above, with its trailing '\n' at start-1.
		prevStart := bytes.LastIndexByte(src[:start-1], '\n') + 1 // 0 if no earlier '\n'
		line := bytes.TrimRight(src[prevStart:start-1], "\r")
		if len(line) == 0 || line[0] != '#' || (len(line) > 1 && line[1] == '!') {
			break // blank line, non-comment line, or a shebang — stop attaching
		}
		start = prevStart
	}
	return start
}
