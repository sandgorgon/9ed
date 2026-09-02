package deck

import "bytes"

// CSegmenter segments C/C++ source into one card per top-level construct —
// a function definition, a struct/class/union/enum/namespace, a simple
// declaration/prototype/typedef, or a contiguous run of preprocessor
// directives — using brace-depth tracking rather than a real parser. This
// is a "good enough" structural heuristic, not a real C/C++ grammar: it
// doesn't distinguish a genuine top-level construct from, say, a stray
// unbalanced brace a naive scanner might miscount, and a multi-line
// `#define ... \` macro is only recognized one physical line at a time.
// It does track // and /* */ comments and "…"/'…' literals well enough
// that braces and semicolons inside them are never mistaken for
// structural ones — see cTopLevelBounds's cBound.hadBlock, which records
// whether a *real* '{' (never one inside a comment/string) was seen in
// each card, since classifyCCard can't just re-scan a card's raw bytes
// for '{' without the same risk.
type CSegmenter struct{}

func (CSegmenter) Segment(src []byte) []Card {
	if len(src) == 0 {
		return nil
	}
	bounds := cTopLevelBounds(src)
	if len(bounds) == 0 {
		return []Card{{Title: firstLine(src), Span: [2]int{0, len(src)}, Kind: "preamble"}}
	}
	// Fold a gap's blank lines forward into the card that precedes it —
	// mirroring GoSegmenter, whose card boundaries are the next decl's
	// own (whitespace-free) start position — so a card's Title is always
	// its own first real line, never a blank line the previous card left
	// behind.
	for i := range bounds {
		bounds[i].offset = cSkipSpace(src, bounds[i].offset)
	}

	var cards []Card
	start := 0
	for _, b := range bounds {
		if b.offset <= start {
			continue // a boundary immediately following another; the tokenizer avoids this in practice
		}
		body := src[start:b.offset]
		title := cFirstRealLine(body)
		kind := classifyCCard(title, b.hadBlock)
		cards = append(cards, Card{Title: string(title), Span: [2]int{start, b.offset}, Kind: kind, Name: cCardName(title, kind)})
		start = b.offset
	}
	// Trailing content after the last trigger (trailing whitespace, a
	// comment, a dangling unterminated statement) has no trigger of its
	// own to close it out — glue it onto the last real card's span,
	// mirroring how GoSegmenter/MarkdownSegmenter fold trailing content
	// into the previous card rather than growing an empty final one.
	if start < len(src) && len(cards) > 0 {
		cards[len(cards)-1].Span[1] = len(src)
	}
	return mergeAdjacentPreprocessorCards(cards)
}

// mergeAdjacentPreprocessorCards collapses a run of consecutive
// "preprocessor" cards (cTopLevelBounds emits one boundary per physical
// directive line) into a single card spanning the whole run — an
// `#include` block reads as one card, not one per line, the same way
// GoSegmenter's import block is one card rather than one per import.
func mergeAdjacentPreprocessorCards(cards []Card) []Card {
	merged := cards[:0:0]
	for _, c := range cards {
		if n := len(merged); n > 0 && merged[n-1].Kind == "preprocessor" && c.Kind == "preprocessor" {
			merged[n-1].Span[1] = c.Span[1]
			continue
		}
		merged = append(merged, c)
	}
	return merged
}

// cBound marks the end of one top-level card: offset is where the next
// card begins, and hadBlock reports whether a real (non-comment/string)
// '{' was opened anywhere within the card that ends here.
type cBound struct {
	offset   int
	hadBlock bool
}

// cTopLevelBounds scans src and returns, in order, the end of every
// top-level card: right after a top-level (depth-0) ';', right after a
// top-level '}' (absorbing a trailing "Name;" first, so
// `typedef struct {...} Point;` and `struct S {...};` end at their
// closing ';' rather than splitting it into its own near-empty card),
// and right after each preprocessor directive's line. Trailing content
// after the last trigger (a dangling declaration, trailing comment,
// whitespace) has no bound of its own — Segment folds it into the last
// card instead.
func cTopLevelBounds(src []byte) []cBound {
	var bounds []cBound
	depth := 0
	atLineStart := true
	lineHasDirective := false
	sawBlock := false
	n := len(src)
	for i := 0; i < n; {
		c := src[i]
		switch {
		case c == '/' && i+1 < n && src[i+1] == '/':
			for i < n && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && src[i+1] == '*':
			i += 2
			for i+1 < n && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			if i+1 < n {
				i += 2
			} else {
				i = n
			}
			atLineStart = false
		case c == '"' || c == '\'':
			quote := c
			i++
			for i < n && src[i] != quote {
				if src[i] == '\\' && i+1 < n {
					i += 2
				} else {
					i++
				}
			}
			if i < n {
				i++
			}
			atLineStart = false
		case c == '\n':
			if depth == 0 && lineHasDirective {
				bounds = append(bounds, cBound{i + 1, sawBlock})
				lineHasDirective = false
				sawBlock = false
			}
			atLineStart = true
			i++
		case c == '#' && atLineStart && depth == 0:
			lineHasDirective = true
			atLineStart = false
			i++
		case c == '{':
			depth++
			sawBlock = true
			atLineStart = false
			i++
		case c == '}':
			i++
			if depth > 0 {
				depth--
			}
			atLineStart = false
			if depth == 0 {
				j := cSkipSpace(src, i)
				if j < n && cIsIdentStart(src[j]) {
					k := j
					for k < n && cIsIdentCont(src[k]) {
						k++
					}
					j = cSkipSpace(src, k)
				}
				if j < n && src[j] == ';' {
					i = j + 1
				}
				bounds = append(bounds, cBound{i, sawBlock})
				sawBlock = false
			}
		case c == ';':
			i++
			atLineStart = false
			if depth == 0 {
				bounds = append(bounds, cBound{i, sawBlock})
				sawBlock = false
			}
		default:
			if c != ' ' && c != '\t' && c != '\r' {
				atLineStart = false
			}
			i++
		}
	}
	return bounds
}

func cSkipSpace(src []byte, i int) int {
	for i < len(src) {
		switch src[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

func cIsIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func cIsIdentCont(b byte) bool {
	return cIsIdentStart(b) || (b >= '0' && b <= '9')
}

// cFirstRealLine returns the first line of body that isn't blank and
// isn't a `//` line comment — skipping past a card's attached leading
// comment (see Segment) the same way GoSegmenter's Title comes from a
// decl's own first line, never its Doc comment. Returns nil if body is
// nothing but blank lines and/or `//` comments.
func cFirstRealLine(body []byte) []byte {
	rest := body
	for {
		rest = bytes.TrimLeft(rest, " \t\r\n")
		if len(rest) == 0 {
			return nil
		}
		nl := bytes.IndexByte(rest, '\n')
		line := rest
		if nl >= 0 {
			line = rest[:nl]
		}
		line = bytes.TrimRight(line, " \t\r")
		if bytes.HasPrefix(line, []byte("//")) {
			if nl < 0 {
				return nil
			}
			rest = rest[nl+1:]
			continue
		}
		return line
	}
}

var cTypeKeywords = [][]byte{[]byte("struct"), []byte("class"), []byte("union"), []byte("enum"), []byte("namespace")}

// classifyCCard picks a card's Kind from its own first real code line
// (nil when a card is entirely blank lines/comments, e.g. a trailing
// comment with nothing after it): a run of preprocessor lines, a
// struct/class/union/enum/namespace (checked among the line's first few
// words so a leading "typedef"/"static"/"export" qualifier doesn't hide
// the keyword), a construct with a real block body (a function
// definition, most commonly — hadBlock comes from the tokenizer, which
// already excluded comments and string/char literals, rather than
// re-scanning raw bytes for '{' here), or a plain
// declaration/prototype/statement.
func classifyCCard(line []byte, hadBlock bool) string {
	if line == nil {
		return "preamble"
	}
	if bytes.HasPrefix(line, []byte("#")) {
		return "preprocessor"
	}
	fields := bytes.Fields(line)
	if len(fields) > 3 {
		fields = fields[:3]
	}
	for _, f := range fields {
		for _, kw := range cTypeKeywords {
			if bytes.Equal(f, kw) {
				return string(kw)
			}
		}
	}
	if hadBlock {
		return "func"
	}
	return "decl"
}

// cCardName extracts a card's defined identifier from its own first
// real code line, when kind makes that unambiguous — see Card.Name.
// "decl", "preprocessor", and "preamble" cards get no Name: a plain
// declaration/prototype line is too varied a shape to reliably pick one
// identifier out of, and a macro's own name is left for a later pass.
func cCardName(line []byte, kind string) string {
	switch kind {
	case "func":
		return cFuncName(line)
	case "struct", "class", "union", "enum", "namespace":
		return cContainerName(line, kind)
	}
	return ""
}

// cFuncName returns the identifier immediately before the parameter
// list's '(' on a function definition's line — e.g. "bar" from
// "int bar(int x) {", or "method" from "MyClass::method(int x) {" (the
// "::" scope is naturally excluded, since ':' isn't an identifier
// character, matching GoSegmenter's choice to name a method without its
// receiver). Returns "" for an operator overload (its symbolic name
// isn't a plain identifier) or when '(' isn't on this first line at all.
func cFuncName(line []byte) string {
	p := bytes.IndexByte(line, '(')
	if p < 0 {
		return ""
	}
	end := p
	for end > 0 && (line[end-1] == ' ' || line[end-1] == '\t') {
		end--
	}
	start := end
	for start > 0 && cIsIdentCont(line[start-1]) {
		start--
	}
	if start == end || !cIsIdentStart(line[start]) {
		return ""
	}
	return string(line[start:end])
}

// cContainerName returns the name following a struct/class/union/enum/
// namespace keyword on line — e.g. "Foo" from "struct Foo {" or "Color"
// from "enum class Color {" (a second keyword, "class", is skipped).
// Returns "" for an anonymous container, e.g. "typedef struct {" with
// its name (if any) trailing on a later line this function never sees —
// a known gap, not a wrong guess: no Name beats a misattributed one.
func cContainerName(line []byte, kind string) string {
	fields := bytes.Fields(line)
	kwIdx := -1
	for i, f := range fields {
		if string(f) == kind {
			kwIdx = i
			break
		}
	}
	if kwIdx < 0 {
		return ""
	}
	for _, f := range fields[kwIdx+1:] {
		isKeyword := false
		for _, kw := range cTypeKeywords {
			if bytes.Equal(f, kw) {
				isKeyword = true
				break
			}
		}
		if isKeyword {
			continue
		}
		return cLeadingIdent(f)
	}
	return ""
}

// cLeadingIdent returns the leading run of identifier characters in b,
// or "" if b doesn't start with one (e.g. "{" for an anonymous
// container, or ":" for a base-class list with no space before it).
func cLeadingIdent(b []byte) string {
	if len(b) == 0 || !cIsIdentStart(b[0]) {
		return ""
	}
	i := 1
	for i < len(b) && cIsIdentCont(b[i]) {
		i++
	}
	return string(b[:i])
}
