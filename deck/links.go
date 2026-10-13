package deck

import (
	"go/scanner"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Link is one navigable reference found in a card's text: another file,
// a line in a file, a card within a file, or an external URL. Links is
// the scanner; resolving a Link against the open file's cards, or
// opening another file, is the caller's job — a Link is only what the
// text said.
//
// Exactly one of three shapes applies:
//
//   - URL != "": an external reference (anything with a "scheme://").
//     Path, Line, and Anchor are empty.
//   - Path != "": a file reference, relative to the file the link was
//     found in, optionally with a Line (1-based) or an Anchor.
//   - Path == "" and URL == "": a reference within the current file,
//     naming a card by Anchor (a heading slug, or a symbol's Card.Name).
//
// Anchor is raw text as written ("install-guide", "Walrus"), not
// normalized — matching it against Card.Name or a heading slug is the
// resolver's concern, since which of those is meant depends on the
// target file's type.
type Link struct {
	Text   string // the matched source text, e.g. "[docs](a.md#x)" or "@see foo.go:42"
	Span   [2]int // [start, end) byte offsets of Text within src
	URL    string
	Path   string
	Line   int
	Anchor string
}

// Links scans each card's text for navigable references, per the policy
// a "link" means here:
//
// Syntax-shaped references — "path.ext:LINE", "path.ext#anchor",
// Markdown "[text](target)", and "http(s)://…" URLs — are recognized
// anywhere in a comment, for languages whose comment syntax is known
// (ext: ".go", C-family, ".sh"/".bash", ".hs", kyu), and anywhere in
// the text of a Markdown or unrecognized file, which has no comments
// to restrict to. Code and string literals are never scanned, so an
// ordinary `import "a/b.go:3"`-shaped string isn't a link.
//
// A bare symbol is a link only when announced by an explicit marker —
// "@see Name" or "ref: Name" — so a prose word in a comment never
// becomes a jump target by coincidence. The marker's target may also be
// any of the syntax-shaped forms above, plus "path.ext:Name" (a card
// within another file).
//
// The result is parallel to cards: result[i] lists card i's links in
// source order, nil if it has none. A link belongs to the card
// containing its first byte.
func Links(src []byte, cards []Card, ext string) [][]Link {
	out := make([][]Link, len(cards))
	if len(src) == 0 || len(cards) == 0 {
		return out
	}
	var all []Link
	for _, r := range commentRanges(src, strings.ToLower(ext)) {
		all = append(all, scanLinks(src, r[0], r[1])...)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Span[0] < all[j].Span[0] })
	ci := 0
	for _, l := range all {
		for ci < len(cards) && l.Span[0] >= cards[ci].Span[1] {
			ci++
		}
		if ci >= len(cards) {
			break
		}
		if l.Span[0] >= cards[ci].Span[0] {
			out[ci] = append(out[ci], l)
		}
	}
	return out
}

var (
	// path component charset shared by the bare path patterns. A bare
	// path must carry an extension (".go"), so "time:10" or "std::x"
	// never read as file:line.
	linkPathPat = `(?:\.{0,2}/|~/)?(?:[\w@+~-]+/)*[\w@+~-]+(?:\.[\w@+~-]+)*\.[A-Za-z][\w-]*`

	mdLinkRe    = regexp.MustCompile(`\[[^\]\n]*\]\(([^)\s]+)\)`)
	markerRe    = regexp.MustCompile(`(?:@see\b|\bref:)[ \t]+(\S+)`)
	urlRe       = regexp.MustCompile("https?://[^\\s<>\"'`]+")
	fileLineRe  = regexp.MustCompile(`(?:^|[^\w/.~@+-])(` + linkPathPat + `):([1-9][0-9]*)(?::[0-9]+)?`)
	fileAnchRe  = regexp.MustCompile(`(?:^|[^\w/.~@+-])(` + linkPathPat + `)#([\w-]+)`)
	schemeRe    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)
	extSuffixRe = regexp.MustCompile(`\.[a-z0-9]+$`)
	identRe     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*$`)
)

// scanLinks finds links within src[start:end). Patterns are tried in
// priority order and a later pattern can't claim bytes an earlier one
// already did, so the URL inside "[x](https://…)" is one link, not two.
func scanLinks(src []byte, start, end int) []Link {
	text := string(src[start:end])
	var links []Link
	taken := func(a, b int) bool {
		for _, l := range links {
			if a < l.Span[1] && b > l.Span[0] {
				return true
			}
		}
		return false
	}
	add := func(a, b int, l Link) {
		if taken(start+a, start+b) {
			return
		}
		l.Span = [2]int{start + a, start + b}
		l.Text = text[a:b]
		links = append(links, l)
	}

	for _, m := range mdLinkRe.FindAllStringSubmatchIndex(text, -1) {
		if l, ok := classifyTarget(text[m[2]:m[3]], false); ok {
			add(m[0], m[1], l)
		}
	}
	for _, m := range markerRe.FindAllStringSubmatchIndex(text, -1) {
		tok := trimTarget(text[m[2]:m[3]])
		if l, ok := classifyTarget(tok, true); ok {
			add(m[0], m[2]+len(tok)+leadingTrimmed(text[m[2]:m[3]]), l)
		}
	}
	for _, m := range urlRe.FindAllStringIndex(text, -1) {
		u := strings.TrimRight(text[m[0]:m[1]], ".,;:!?)]}")
		if len(u) > len("https://") {
			add(m[0], m[0]+len(u), Link{URL: u})
		}
	}
	for _, m := range fileLineRe.FindAllStringSubmatchIndex(text, -1) {
		line, _ := strconv.Atoi(text[m[4]:m[5]])
		add(m[2], m[1], Link{Path: text[m[2]:m[3]], Line: line})
	}
	for _, m := range fileAnchRe.FindAllStringSubmatchIndex(text, -1) {
		add(m[2], m[1], Link{Path: text[m[2]:m[3]], Anchor: text[m[4]:m[5]]})
	}
	return links
}

// trimTarget strips the punctuation a marker's token tends to carry in
// prose: a trailing sentence mark, or wrapping quotes/backticks.
func trimTarget(s string) string {
	s = strings.TrimLeft(s, "`'\"(")
	return strings.TrimRight(s, "`'\".,;:)")
}

// leadingTrimmed is how many bytes trimTarget's TrimLeft removed, so the
// token's end offset can be recovered from its trimmed length.
func leadingTrimmed(s string) int {
	return len(s) - len(strings.TrimLeft(s, "`'\"("))
}

// classifyTarget interprets a link target written by hand. bareIsSymbol
// picks what a target with no "/", ":", or "#" means: a marker's
// ("@see Walrus") is a card name, a Markdown link's ("[x](LICENSE)") is
// a file.
func classifyTarget(t string, bareIsSymbol bool) (Link, bool) {
	if t == "" {
		return Link{}, false
	}
	if schemeRe.MatchString(t) {
		return Link{URL: t}, true
	}
	if strings.HasPrefix(t, "#") {
		if len(t) == 1 {
			return Link{}, false
		}
		return Link{Anchor: t[1:]}, true
	}
	if i := strings.Index(t, "#"); i > 0 {
		if i == len(t)-1 {
			return Link{Path: t[:i]}, true
		}
		return Link{Path: t[:i], Anchor: t[i+1:]}, true
	}
	if i := strings.LastIndex(t, ":"); i > 0 && i < len(t)-1 {
		path, rest := t[:i], t[i+1:]
		if n, err := strconv.Atoi(rest); err == nil {
			if n > 0 {
				return Link{Path: path, Line: n}, true
			}
			return Link{}, false
		}
		if looksLikePath(path) && identRe.MatchString(rest) {
			return Link{Path: path, Anchor: rest}, true
		}
		return Link{}, false
	}
	if !bareIsSymbol || looksLikePath(t) {
		return Link{Path: t}, true
	}
	if identRe.MatchString(t) {
		return Link{Anchor: t}, true
	}
	return Link{}, false
}

// looksLikePath distinguishes "other.md" or "a/b" from a symbol like
// "Walrus" or "T.Method": a path has a "/" or a lowercase extension. A
// lowercase-method selector ("t.run") reads as a path — an accepted
// heuristic miss; spell it "path:name" or use the heading form.
func looksLikePath(s string) bool {
	return strings.Contains(s, "/") || extSuffixRe.MatchString(s)
}

// commentRanges returns the [start, end) byte ranges of src that may
// contain links: its comments, for a language whose comment syntax is
// known, or all of src for Markdown and anything unrecognized.
func commentRanges(src []byte, ext string) [][2]int {
	switch ext {
	case ".go":
		return goCommentRanges(src)
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".hh", ".hpp", ".hxx":
		return cCommentRanges(src)
	case ".sh", ".bash", ".kyu", ".ky":
		return hashCommentRanges(src)
	case ".hs":
		return haskellCommentRanges(src)
	default:
		return [][2]int{{0, len(src)}}
	}
}

// goCommentRanges uses go/scanner, which tokenizes strings and runes
// correctly and keeps working past a syntax error — so comments are
// still found in a file GoSegmenter gave up on.
func goCommentRanges(src []byte) [][2]int {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, scanner.ScanComments)
	var out [][2]int
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			return out
		}
		if tok == token.COMMENT {
			off := file.Offset(pos)
			out = append(out, [2]int{off, min(off+len(lit), len(src))})
		}
	}
}

// cCommentRanges scans // and /* */ comments, skipping string and char
// literals so a "//" inside one doesn't start a comment.
func cCommentRanges(src []byte) [][2]int {
	var out [][2]int
	for i := 0; i < len(src); {
		switch {
		case src[i] == '"' || src[i] == '\'':
			i = skipQuoted(src, i, src[i], true, false)
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '/':
			j := i
			for j < len(src) && src[j] != '\n' {
				j++
			}
			out = append(out, [2]int{i, j})
			i = j
		case src[i] == '/' && i+1 < len(src) && src[i+1] == '*':
			j := i + 2
			for j+1 < len(src) && !(src[j] == '*' && src[j+1] == '/') {
				j++
			}
			j = min(j+2, len(src))
			out = append(out, [2]int{i, j})
			i = j
		default:
			i++
		}
	}
	return out
}

// hashCommentRanges scans '#' comments for shell-family syntax: a '#'
// starts one only at the start of a word, outside quotes, so "$#",
// "${#x}", and "a#b" aren't comments.
func hashCommentRanges(src []byte) [][2]int {
	var out [][2]int
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '"' || c == '\'':
			i = skipQuoted(src, i, c, c == '"', true)
		case c == '\\':
			i += 2
		case c == '#' && (i == 0 || strings.IndexByte(" \t\r\n;&|(", src[i-1]) >= 0):
			j := i
			for j < len(src) && src[j] != '\n' {
				j++
			}
			out = append(out, [2]int{i, j})
			i = j
		default:
			i++
		}
	}
	return out
}

// haskellCommentRanges scans "--" line comments (only when the dashes
// aren't part of a longer operator like "-->") and nesting "{- -}"
// block comments, skipping string literals. A single quote is left
// alone: it's as often a prime in an identifier (foldl') as a char
// literal, and a char literal can't hide a comment opener anyway.
func haskellCommentRanges(src []byte) [][2]int {
	const symbols = "!#$%&*+./<=>?@\\^|-~:"
	var out [][2]int
	for i := 0; i < len(src); {
		switch {
		case src[i] == '"':
			i = skipQuoted(src, i, '"', true, false)
		case src[i] == '{' && i+1 < len(src) && src[i+1] == '-':
			depth, j := 1, i+2
			for j < len(src) && depth > 0 {
				switch {
				case src[j] == '{' && j+1 < len(src) && src[j+1] == '-':
					depth++
					j += 2
				case src[j] == '-' && j+1 < len(src) && src[j+1] == '}':
					depth--
					j += 2
				default:
					j++
				}
			}
			out = append(out, [2]int{i, min(j, len(src))})
			i = min(j, len(src))
		case src[i] == '-' && i+1 < len(src) && src[i+1] == '-':
			j := i
			for j < len(src) && src[j] == '-' {
				j++
			}
			prevSym := i > 0 && strings.IndexByte(symbols, src[i-1]) >= 0
			nextSym := j < len(src) && strings.IndexByte(symbols, src[j]) >= 0
			if prevSym || nextSym {
				i = j
				continue
			}
			for j < len(src) && src[j] != '\n' {
				j++
			}
			out = append(out, [2]int{i, j})
			i = j
		default:
			i++
		}
	}
	return out
}

// skipQuoted returns the index just past the literal opened by quote at
// src[i]. escapes says whether a backslash escapes the next byte (true
// except for shell single quotes); multiline says whether the literal
// may span newlines (shell only — C and Haskell literals can't, so an
// unterminated one ends at its line rather than swallowing the rest of
// the file's comments).
func skipQuoted(src []byte, i int, quote byte, escapes, multiline bool) int {
	for j := i + 1; j < len(src); j++ {
		switch {
		case escapes && src[j] == '\\':
			j++
		case src[j] == quote:
			return j + 1
		case src[j] == '\n' && !multiline:
			return j
		}
	}
	return len(src)
}

// ResolveAnchor returns the index of the card a link's Anchor names:
// the first card whose Name or Title equals anchor exactly, else whose
// Name matches it after slugging both ("install-guide" finds the
// heading "Install Guide"). Only Name is slugged, not Title — a Go
// func's Title is a whole signature line, which no anchor is meant to
// spell. ok is false when nothing matches; duplicates resolve to the
// first, in card order.
func ResolveAnchor(cards []Card, anchor string) (idx int, ok bool) {
	if anchor == "" {
		return 0, false
	}
	for i, c := range cards {
		if c.Name == anchor || c.Title == anchor {
			return i, true
		}
	}
	want := slugify(anchor)
	for i, c := range cards {
		if c.Name != "" && slugify(c.Name) == want {
			return i, true
		}
	}
	return 0, false
}

// slugify lowercases s, turns spaces into hyphens, and drops everything
// except letters, digits, '_' and '-' — the usual Markdown-renderer
// heading-anchor rule.
func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == ' ' || r == '-':
			b.WriteByte('-')
		case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}
