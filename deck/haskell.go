package deck

import "bytes"

// HaskellSegmenter segments Haskell source into one card per top-level
// item — a `module`/`import` line, a `type`/`data`/`newtype`/`class`/
// `instance` declaration, or a function binding — using Haskell's own
// layout rule: a top-level item starts at column 0, and everything
// indented under it (its body, guards, `where` clause, ...) belongs to
// the same card. Consecutive column-0 lines that share a function's
// leading identifier (`fact 0 = 1` / `fact n = ...`, multiple pattern-
// match equations for the same binding) stay in one card rather than
// splitting into one per equation. A leading contiguous run of `--`
// comment lines immediately above an item (no blank line in between)
// travels with it into its span, the same as GoSegmenter's Doc
// attachment — but a `{- ... -}` block comment (which nests) only
// suppresses false boundary detection inside it, it's never attached as
// a doc comment. Falls back to one "preamble" card covering the whole
// file if there are no column-0 items at all (e.g. a module that's
// nothing but a header comment). This is a "good enough" structural
// heuristic using the layout rule alone, not a real Haskell grammar —
// it doesn't understand `{-# LANGUAGE ... #-}` pragmas beyond their
// being a block comment for depth-tracking purposes.
type HaskellSegmenter struct{}

func (HaskellSegmenter) Segment(src []byte) []Card {
	if len(src) == 0 {
		return nil
	}
	type decl struct {
		spanStart int // includes an attached leading '--' comment block, if any
		declStart int // the item's own first line, for Title
		kind      string
		ident     string // leading identifier, for equation-grouping; "" if none
		name      string // Card.Name; see hsCardName
	}
	var decls []decl

	blockDepth := 0
	pos := 0
	for pos < len(src) {
		lineStart := pos
		line, next := bashLine(src, pos)

		startDepth := blockDepth
		blockDepth = hsUpdateBlockCommentDepth(line, blockDepth)

		if startDepth == 0 && len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			trimmed := bytes.TrimSpace(line)
			if len(trimmed) > 0 && !bytes.HasPrefix(trimmed, []byte("--")) && !bytes.HasPrefix(trimmed, []byte("{-")) {
				kind := hsClassify(trimmed)
				ident := hsLeadingIdent(trimmed)
				if n := len(decls); n > 0 && decls[n-1].kind == "decl" && kind == "decl" && ident != "" && decls[n-1].ident == ident {
					// Another equation of the same binding — stays in
					// the previous card; nothing to append.
				} else {
					decls = append(decls, decl{
						spanStart: hsAttachedCommentStart(src, lineStart),
						declStart: lineStart,
						kind:      kind,
						ident:     ident,
						name:      hsCardName(trimmed, kind, ident),
					})
				}
			}
		}
		pos = next
	}

	if len(decls) == 0 {
		return []Card{{Title: firstLine(src), Span: [2]int{0, len(src)}, Kind: "preamble"}}
	}

	var cards []Card
	if decls[0].spanStart > 0 {
		cards = append(cards, Card{
			Title: firstLine(src),
			Span:  [2]int{0, decls[0].spanStart},
			Kind:  "preamble",
		})
	}
	for i, d := range decls {
		end := len(src)
		if i+1 < len(decls) {
			end = decls[i+1].spanStart
		}
		cards = append(cards, Card{Title: firstLine(src[d.declStart:]), Span: [2]int{d.spanStart, end}, Kind: d.kind, Name: d.name})
	}
	return cards
}

// hsUpdateBlockCommentDepth scans line (which starts outside any string
// literal — Haskell string contents can't hold an unescaped newline, so
// a segmenter working line-at-a-time never has to worry about resuming
// mid-string) and returns the `{- -}` nesting depth in effect at the
// line's end, given depth at its start. A `--` seen while depth == 0
// starts a line comment that runs to the line's end, so nothing after it
// (including a literal "{-") opens a block comment.
func hsUpdateBlockCommentDepth(line []byte, depth int) int {
	i := 0
	for i < len(line) {
		switch {
		case depth == 0 && i+1 < len(line) && line[i] == '-' && line[i+1] == '-':
			return depth
		case i+1 < len(line) && line[i] == '{' && line[i+1] == '-':
			depth++
			i += 2
		case depth > 0 && i+1 < len(line) && line[i] == '-' && line[i+1] == '}':
			depth--
			i += 2
		default:
			i++
		}
	}
	return depth
}

func hsClassify(trimmed []byte) string {
	fields := bytes.Fields(trimmed)
	if len(fields) == 0 {
		return "decl"
	}
	switch string(fields[0]) {
	case "module", "import", "type", "data", "newtype", "class", "instance":
		return string(fields[0])
	default:
		return "decl"
	}
}

// hsLeadingIdent returns trimmed's leading Haskell identifier (letters,
// digits, underscore, and the trailing single-quote Haskell allows in an
// identifier, e.g. "x'"), or "" if trimmed doesn't start with one (an
// operator definition like `(<+>) x y = ...`, or a keyword line already
// classified as something other than "decl").
func hsLeadingIdent(trimmed []byte) string {
	i := 0
	for i < len(trimmed) && hsIsIdentByte(trimmed[i]) {
		i++
	}
	return string(trimmed[:i])
}

// hsCardName returns the identifier a card unambiguously defines, given
// its already-computed kind and leading identifier ident (from
// hsLeadingIdent(trimmed)) — see Card.Name. For "decl" (a function/
// value binding), ident already is the binding's own name. For "data"/
// "type"/"newtype"/"class", the name is the field right after the
// keyword — e.g. "Color" from "data Color = Red | Green | Blue". Empty
// for "module"/"import" (no single defined identifier) and "instance"
// (it associates an existing class with an existing type — naming
// either would be a guess, not an extraction).
func hsCardName(trimmed []byte, kind, ident string) string {
	switch kind {
	case "decl":
		return ident
	case "data", "type", "newtype", "class":
		fields := bytes.Fields(trimmed)
		if len(fields) < 2 {
			return ""
		}
		return hsLeadingIdent(fields[1])
	}
	return ""
}

func hsIsIdentByte(b byte) bool {
	return b == '_' || b == '\'' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// hsAttachedCommentStart walks backward from declStart (the byte offset
// of a top-level item's own line) over an unbroken run of column-0 '--'
// comment lines immediately above it — no blank line in between — and
// returns where that run begins. Returns declStart unchanged if there's
// no such run.
func hsAttachedCommentStart(src []byte, declStart int) int {
	start := declStart
	for start > 0 {
		prevStart := bytes.LastIndexByte(src[:start-1], '\n') + 1
		line := bytes.TrimRight(src[prevStart:start-1], "\r")
		if !bytes.HasPrefix(line, []byte("--")) {
			break
		}
		start = prevStart
	}
	return start
}
