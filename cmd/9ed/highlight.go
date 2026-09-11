package main

import (
	"go/scanner"
	"go/token"
	"regexp"
	"slices"
	"unicode/utf8"

	"github.com/sandgorgon/9sh/kyu/lexer"
	ktoken "github.com/sandgorgon/9sh/kyu/token"
	"github.com/sandgorgon/tui/cell"
	"github.com/sandgorgon/tui/style"
	"github.com/sandgorgon/tui/widget"
)

// highlightsFor dispatches to a syntax highlighter by the source file's
// own extension (the same table segmenterFor uses), or nil for any
// extension with none — anything segmenterFor falls back to
// deck.PlainSegmenter for. Go and kyu keep their own real-tokenizer
// highlighters (goHighlights via go/scanner, kyuHighlights via 9sh's
// own kyu/lexer) since both ship an exact tokenizer already; C/C++,
// Bash, Haskell, and Markdown use the shared regex engine below, per
// this project's existing tolerance for a "good enough" heuristic
// rather than a real grammar for those languages — see CSegmenter's,
// BashSegmenter's, and HaskellSegmenter's own doc comments making the
// same trade-off for structural segmentation.
func highlightsFor(ext string, body string, theme style.Theme) []widget.StyleSpan {
	switch ext {
	case ".go":
		return goHighlights(body, theme)
	case ".kyu":
		return kyuHighlights(body, theme)
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".hh", ".hpp", ".hxx":
		return regexHighlights(body, theme, cLangRe, regexGroupStyle)
	case ".sh", ".bash":
		return regexHighlights(body, theme, bashLangRe, regexGroupStyle)
	case ".hs":
		return regexHighlights(body, theme, haskellLangRe, regexGroupStyle)
	case ".md", ".markdown":
		return regexHighlights(body, theme, mdLangRe, mdGroupStyle)
	default:
		return nil
	}
}

// cLangRe tokenizes C/C++ well enough for coloring, not for correctness
// — same "good enough heuristic" the CSegmenter comment already accepts
// for this codebase's non-Go languages. Comment and string alternatives
// are listed ahead of keyword/number/constant/type/preprocessor in the
// top-level alternation deliberately: regexHighlights relies on Go's
// regexp package resolving alternation leftmost-first (Perl-like, not
// POSIX longest-match), so a keyword spelled out inside a string literal
// or a comment is matched by the earlier, wider alternative first and
// never separately matches the keyword group. constant and type are
// disjoint word sets from keyword, so their position relative to it
// doesn't matter the same way. preprocessor colors an entire `#...`
// directive line as one span (including e.g. a `#define`'s macro body)
// rather than separately highlighting tokens inside it — the same
// "whole construct, one color" simplification mdLangRe's codeblock group
// already makes.
var cLangRe = regexp.MustCompile(
	`(?P<comment>//[^\n]*|(?s:/\*.*?\*/))` +
		`|(?P<string>"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*')` +
		`|(?P<preprocessor>(?m:^[ \t]*#[ \t]*\w+[^\n]*))` +
		`|(?P<number>\b0[xX][0-9a-fA-F]+\b|\b\d+(?:\.\d+)?[uUlLfF]*\b)` +
		`|(?P<constant>\b(?:true|false|NULL|nullptr)\b)` +
		`|(?P<type>\b(?:size_t|ssize_t|ptrdiff_t|wchar_t|intptr_t|uintptr_t|FILE|va_list|bool|int8_t|int16_t|int32_t|int64_t|uint8_t|uint16_t|uint32_t|uint64_t)\b)` +
		`|(?P<keyword>\b(?:auto|break|case|char|const|continue|default|do|double|else|enum|extern|float|for|goto|if|inline|int|long|register|restrict|return|short|signed|sizeof|static|struct|switch|typedef|union|unsigned|void|volatile|while|_Bool|_Complex|_Imaginary|class|namespace|public|private|protected|template|typename|virtual|override|new|delete|this|using|friend|operator|try|catch|throw|explicit|mutable|constexpr|static_cast|dynamic_cast|const_cast|reinterpret_cast)\b)`,
)

// bashLangRe tokenizes Bash/POSIX-shell well enough for coloring — the
// same heuristic trade-off as cLangRe, matching BashSegmenter's own
// "not a real shell grammar" disclaimer (it doesn't track heredocs
// either, so a keyword-like word inside one can still get colored).
// variable is listed ahead of string so a bare $VAR/${VAR} still gets
// colored outside of a quoted string; a $VAR that appears *inside* a
// double-quoted string is left as part of that (earlier-matching) string
// span rather than separately colored — the same "string wins whatever
// it contains" behavior comment/string already get in cLangRe. builtin
// lists common command names (echo, cd, test, ...) that aren't already
// in the keyword list below; the existing keyword list's own local/
// export/declare/... (arguably builtins themselves) are left alone
// rather than reclassified, to keep this change additive only.
var bashLangRe = regexp.MustCompile(
	`(?P<comment>#[^\n]*)` +
		`|(?P<string>"(?:\\.|[^"\\])*"|'[^']*')` +
		`|(?P<variable>\$\{[^}\n]*\}|\$[A-Za-z_][A-Za-z0-9_]*|\$[0-9@*#?$!_-])` +
		`|(?P<number>\b\d+\b)` +
		`|(?P<builtin>\b(?:echo|printf|read|cd|pwd|pushd|popd|dirs|test|let|set|shopt|alias|unalias|type|command|kill|wait|jobs|fg|bg|ulimit|umask|getopts)\b)` +
		`|(?P<keyword>\b(?:if|then|elif|else|fi|for|while|until|do|done|case|esac|function|in|select|time|coproc|return|break|continue|local|export|readonly|declare|typeset|unset|shift|exit|trap|eval|exec|source)\b)`,
)

// haskellLangRe tokenizes Haskell well enough for coloring, the same
// "good enough heuristic" tolerance cLangRe/bashLangRe already accept
// for their languages — not a real Haskell grammar, matching
// HaskellSegmenter's own disclaimer. Block comments ({- -}) don't
// nest here even though Haskell's do (see HaskellSegmenter's
// hsUpdateBlockCommentDepth, which tracks nesting for boundary
// detection); a char literal like 'a' is deliberately not matched as a
// string — a single quote is also a valid trailing character in a
// Haskell identifier (see HaskellSegmenter's hsIsIdentByte), so trying
// to recognize 'a' as a char literal risks misreading an identifier
// like x' as the start of an unterminated one instead.
//
// pragma ({-# ... #-}) is listed first, ahead of the general block
// comment: a pragma is lexically a subset of the block-comment pattern
// (both are {- ... -} delimited), so per this file's leftmost-first
// alternation rule (see cLangRe's own doc comment) it has to be tried
// before the wider comment alternative or that alternative always wins
// first and the pragma is never colored distinctly. type matches any
// capitalized identifier — Haskell's own naming convention for both type
// and data constructors, not a guess — and is listed after keyword,
// which is fine since the two sets are disjoint (Haskell's keywords are
// all lowercase).
var haskellLangRe = regexp.MustCompile(
	`(?P<pragma>(?s:\{-#.*?#-\}))` +
		`|(?P<comment>--[^\n]*|(?s:\{-.*?-\}))` +
		`|(?P<string>"(?:\\.|[^"\\])*")` +
		`|(?P<number>\b0[xX][0-9a-fA-F]+\b|\b\d+(?:\.\d+)?\b)` +
		`|(?P<keyword>\b(?:module|import|qualified|as|hiding|type|data|newtype|class|instance|deriving|where|let|in|case|of|do|if|then|else|forall|infixl|infixr|infix)\b)` +
		`|(?P<type>\b[A-Z][A-Za-z0-9_']*\b)`,
)

// mdBacktick and mdTripleBacktick hold literal backtick runs — a Go raw
// string literal (the style cLangRe/bashLangRe/haskellLangRe use) can't
// contain a backtick itself, since it's the raw-string delimiter, so
// mdLangRe is built by concatenating these interpreted-string fragments
// in with its otherwise-raw regex pieces.
const (
	mdBacktick       = "`"
	mdTripleBacktick = "```"
)

// mdLangRe tokenizes Markdown's inline constructs well enough for
// coloring, the same "good enough heuristic" tolerance the other
// regex-based languages already accept, not a CommonMark parser:
// nesting, escaped delimiters, and a tilde-fenced block that closes
// with a mismatched backtick fence (or vice versa) aren't tracked.
// (?m) is set globally so ^/$ in the heading/blockquote/codeblock/
// listmarker/hr groups mean line, not string, boundaries; (?s:...) is
// scoped to just the codeblock alternative so '.' matches a newline
// there without affecting the other groups' own "no newline" character
// classes. Alternatives are ordered so a fenced/inline code span, once
// matched, wins over a heading/blockquote marker that happens to appear
// inside it — see cLangRe's own doc comment on why leftmost-first
// alternation order matters for this package's regex highlighters. bold/
// italic are listed ahead of listmarker/hr for the same reason: a line
// like "*emphasis on its own line*" would otherwise misread its leading
// "*" as a bullet, but since italic only matches when a closing "*"
// follows on the same line, a bare bullet ("* item", no closing "*")
// still falls through to listmarker. image is listed ahead of link, but
// the two can't actually collide — image is anchored on a leading "!"
// link's pattern doesn't have, so whichever comes first, the two never
// compete for the same starting position; ordering them this way just
// mirrors how a reader thinks of them (image as a variant of link).
var mdLangRe = regexp.MustCompile(`(?m)` +
	`(?P<codeblock>(?s:^` + mdTripleBacktick + `[^\n]*\n.*?\n` + mdTripleBacktick + `[ \t]*$|^~~~[^\n]*\n.*?\n~~~[ \t]*$))` +
	`|(?P<code>` + mdBacktick + `[^` + mdBacktick + `\n]+` + mdBacktick + `)` +
	`|(?P<bold>\*\*[^*\n]+\*\*|__[^_\n]+__)` +
	`|(?P<italic>\*[^*\n]+\*|_[^_\n]+_)` +
	`|(?P<strike>~~[^~\n]+~~)` +
	`|(?P<heading>^#{1,6} [^\n]*)` +
	`|(?P<image>!\[[^\]\n]*\]\([^)\n]*\))` +
	`|(?P<link>\[[^\]\n]*\]\([^)\n]*\))` +
	`|(?P<blockquote>^>[^\n]*)` +
	`|(?P<listmarker>^[ \t]*(?:[-*+]|\d+[.)])[ \t]+)` +
	`|(?P<hr>^[ \t]*(?:-{3,}|\*{3,}|_{3,})[ \t]*$)`,
)

// regexHighlights runs spec's combined regex over body once and returns
// one StyleSpan per match, styled by whichever named capture group
// matched via styleFn — regexGroupStyle for cLangRe/bashLangRe/
// haskellLangRe (the same semantic roles styleFor already maps for Go,
// reused so those languages read consistently), mdGroupStyle for
// mdLangRe (Markdown's own inline-construct roles, which don't fit that
// scheme). Byte offsets from the regexp package are translated to the
// rune offsets widget.TextArea.Highlights expects, same as goHighlights.
func regexHighlights(body string, theme style.Theme, spec *regexp.Regexp, styleFn func(string, style.Theme) (cell.Style, bool)) []widget.StyleSpan {
	if body == "" {
		return nil
	}
	names := spec.SubexpNames()
	byteToRune := byteToRuneOffsets(body)
	var spans []widget.StyleSpan
	for _, m := range spec.FindAllStringSubmatchIndex(body, -1) {
		for gi := 1; gi < len(names); gi++ {
			start, end := m[2*gi], m[2*gi+1]
			if start < 0 {
				continue
			}
			st, ok := styleFn(names[gi], theme)
			if !ok {
				break
			}
			spans = append(spans, widget.StyleSpan{
				Start: byteToRune[start],
				End:   byteToRune[end],
				Style: st,
			})
			break
		}
	}
	return spans
}

// regexGroupStyle maps a regexLangRe named group to the same semantic
// roles styleFor uses for Go tokens, extended with the type/constant/
// builtin/variable roles styleFor's predeclared-identifier lookups add
// for Go — "preprocessor" (C), "builtin" (Bash), and "pragma" (Haskell)
// all share the Error role since each flags a "special, not user code"
// construct; "variable" (Bash) reuses the Primary role "type" uses in
// C/Haskell, since no single regex-language spec here emits both groups,
// so there's no collision. Error, not Info, deliberately: style.Theme's
// DefaultDark/DefaultLight define Accent and Info as the exact same RGB
// value (see upstream-specs/tui-accent-info-color-collision.md), so a
// constant (Accent) and a builtin/preprocessor/pragma (would-be Info)
// would render identically — Error is the nearest other role this
// package doesn't already use for a code-language role, and (per
// DefaultDark's own doc comment) is contrast-checked and
// colorblind-separated the same as Success/Warning.
func regexGroupStyle(name string, theme style.Theme) (cell.Style, bool) {
	switch name {
	case "comment":
		return cell.Style{Fg: theme.Muted}, true
	case "string":
		return cell.Style{Fg: theme.Success}, true
	case "number":
		return cell.Style{Fg: theme.Warning}, true
	case "keyword":
		return cell.Style{Fg: theme.Secondary, Attr: cell.AttrBold}, true
	case "type", "variable":
		return cell.Style{Fg: theme.Primary}, true
	case "constant":
		return cell.Style{Fg: theme.Accent}, true
	case "preprocessor", "builtin", "pragma":
		return cell.Style{Fg: theme.Error}, true
	default:
		return cell.Style{}, false
	}
}

// mdGroupStyle maps an mdLangRe named group to a Markdown-specific
// semantic role — deliberately not regexGroupStyle's roles, since
// Markdown's inline constructs (emphasis, headings, links, quotes)
// don't map onto keyword/comment/string/number/type/constant/builtin.
// Bold, italic, and strike leave Fg at its zero value (see style.Theme's
// own doc comment on Foreground/Background: a well-behaved TUI leaves
// plain text alone) so those pure text-decoration constructs only ever
// add an attribute, never recolor the text. image reuses link's
// underline but with Success instead of Info so the two read as
// related-but-distinct, the same way link and blockquote each get their
// own color; listmarker and hr get Warning/Secondary — the two theme
// roles this function otherwise leaves idle — so every one of
// mdLangRe's ten groups now has its own distinct look.
func mdGroupStyle(name string, theme style.Theme) (cell.Style, bool) {
	switch name {
	case "codeblock", "code":
		return cell.Style{Fg: theme.Accent}, true
	case "bold":
		return cell.Style{Attr: cell.AttrBold}, true
	case "italic":
		return cell.Style{Attr: cell.AttrItalic}, true
	case "strike":
		return cell.Style{Attr: cell.AttrStrikethrough}, true
	case "heading":
		return cell.Style{Fg: theme.Primary, Attr: cell.AttrBold}, true
	case "link":
		return cell.Style{Fg: theme.Info, Underline: cell.UnderlineSingle}, true
	case "image":
		return cell.Style{Fg: theme.Success, Underline: cell.UnderlineSingle}, true
	case "blockquote":
		return cell.Style{Fg: theme.Muted, Attr: cell.AttrItalic}, true
	case "listmarker":
		return cell.Style{Fg: theme.Warning}, true
	case "hr":
		return cell.Style{Fg: theme.Secondary}, true
	default:
		return cell.Style{}, false
	}
}

// goHighlights tokenizes body — a single card's own text, standalone,
// not the whole file — with go/scanner and returns one StyleSpan per
// styled token, in *rune* offsets relative to body itself (what
// widget.TextArea.Highlights expects; go/scanner reports byte offsets,
// so a byte->rune translation happens below for correctness on any
// non-ASCII content in a string literal or comment).
func goHighlights(body string, theme style.Theme) []widget.StyleSpan {
	if body == "" {
		return nil
	}
	byteToRune := byteToRuneOffsets(body)

	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(body))
	var s scanner.Scanner
	s.Init(file, []byte(body), nil, scanner.ScanComments)

	var spans []widget.StyleSpan
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		cellStyle, ok := styleFor(tok, lit, theme)
		if !ok {
			continue
		}
		n := len(lit)
		if n == 0 {
			n = len(tok.String())
		}
		startByte := file.Offset(pos)
		endByte := min(startByte+n, len(body)) // defensive: a malformed/truncated card body shouldn't panic
		spans = append(spans, widget.StyleSpan{
			Start: byteToRune[startByte],
			End:   byteToRune[endByte],
			Style: cellStyle,
		})
	}
	return spans
}

// goPredeclaredTypes, goPredeclaredConstants, and goBuiltinFuncs are Go's
// fixed predeclared-identifier sets (https://go.dev/ref/spec#Predeclared_identifiers)
// — a static lookup against a spec-fixed list, not a heuristic, so it
// carries none of the "good enough" caveat the regex-based languages'
// keyword lists do. A local that shadows one of these (e.g. `len := 3`)
// is still colored as the builtin, same trade-off widely accepted
// elsewhere (e.g. chroma's Go lexer) — go/scanner alone can't tell a
// shadowed use from the real one without a type-checker.
var (
	goPredeclaredTypes = map[string]bool{
		"bool": true, "byte": true, "complex64": true, "complex128": true,
		"error": true, "float32": true, "float64": true, "int": true,
		"int8": true, "int16": true, "int32": true, "int64": true,
		"rune": true, "string": true, "uint": true, "uint8": true,
		"uint16": true, "uint32": true, "uint64": true, "uintptr": true,
		"any": true,
	}
	goPredeclaredConstants = map[string]bool{
		"true": true, "false": true, "nil": true, "iota": true,
	}
	goBuiltinFuncs = map[string]bool{
		"append": true, "cap": true, "clear": true, "close": true,
		"complex": true, "copy": true, "delete": true, "imag": true,
		"len": true, "make": true, "max": true, "min": true, "new": true,
		"panic": true, "print": true, "println": true, "real": true,
		"recover": true,
	}
)

// styleFor maps a Go token kind (and, for IDENT, its literal text) to a
// theme color, reusing the theme's semantic roles rather than hardcoding
// colors — keywords get the theme's Secondary accent, comments Muted,
// string/char literals Success, numeric literals Warning, predeclared
// types Primary, predeclared constants Accent, builtin functions Error
// (not Info — see regexGroupStyle's own doc comment on why: Accent and
// Info are the same RGB value in both of style.Theme's default themes,
// so a builtin function would render identically to a constant if it
// used Info instead). Punctuation, ordinary identifiers, and
// (deliberately) SEMICOLON — whose literal can be an auto-inserted "\n"
// rather than real source text — get no override.
func styleFor(tok token.Token, lit string, theme style.Theme) (cell.Style, bool) {
	switch {
	case tok.IsKeyword():
		return cell.Style{Fg: theme.Secondary, Attr: cell.AttrBold}, true
	case tok == token.COMMENT:
		return cell.Style{Fg: theme.Muted}, true
	case tok == token.STRING || tok == token.CHAR:
		return cell.Style{Fg: theme.Success}, true
	case tok == token.INT || tok == token.FLOAT || tok == token.IMAG:
		return cell.Style{Fg: theme.Warning}, true
	case tok == token.IDENT && goPredeclaredTypes[lit]:
		return cell.Style{Fg: theme.Primary}, true
	case tok == token.IDENT && goPredeclaredConstants[lit]:
		return cell.Style{Fg: theme.Accent}, true
	case tok == token.IDENT && goBuiltinFuncs[lit]:
		return cell.Style{Fg: theme.Error}, true
	default:
		return cell.Style{}, false
	}
}

// kyuStyleFor maps a kyu token kind to one of styleFor's same semantic
// roles: kyu's real control-flow keywords (if/else/while/break/continue/
// bind/unbind) get the keyword role; TRUE/FALSE/NULL — values, not
// control flow — get the constant role instead, splitting them out from
// keyword the same way styleFor does for Go's true/false/nil; a PATH
// literal gets the string role — it's as much a literal value as a
// quoted string, just without quotes; INT/FLOAT/DURATION get the number
// role. STRING itself is deliberately not handled here — see
// kyuHighlights, which styles it directly since its span needs
// recomputing from raw source rather than trusting the token's own
// (decoded) Literal length. Everything else (operators, punctuation,
// IDENT) gets no override, same as Go's punctuation/identifiers — kyu's
// lexer has no separate builtin-function token kind (external commands
// go through IDENT/PERCENT instead), so no builtin role is possible here.
func kyuStyleFor(kind ktoken.Kind, theme style.Theme) (cell.Style, bool) {
	switch kind {
	case ktoken.IF, ktoken.ELSE, ktoken.BIND, ktoken.UNBIND, ktoken.WHILE, ktoken.BREAK, ktoken.CONTINUE:
		return cell.Style{Fg: theme.Secondary, Attr: cell.AttrBold}, true
	case ktoken.TRUE, ktoken.FALSE, ktoken.NULL:
		return cell.Style{Fg: theme.Accent}, true
	case ktoken.PATH:
		return cell.Style{Fg: theme.Success}, true
	case ktoken.INT, ktoken.FLOAT, ktoken.DURATION:
		return cell.Style{Fg: theme.Warning}, true
	default:
		return cell.Style{}, false
	}
}

// kyuHighlights tokenizes body — a single card's own text, standalone,
// not the whole file, same as goHighlights — with 9sh's real kyu/lexer
// (the same lexer KyuSegmenter's own kyu/parser runs on) rather than a
// regex heuristic: kyu is this project's own dependency with an exact
// tokenizer already available, the same reason goHighlights uses
// go/scanner instead of a regex for Go.
//
// The lexer discards '#' comments as it scans rather than emitting them
// as tokens (see lexer.Lexer.skipSpaceAndComments), so they're
// recovered in a second pass, kyuCommentHighlights, over the raw text —
// excluding any '#' that pass finds inside a STRING token's own span,
// since the real lexer already proved that's not a comment at all (it
// only ever looks for a comment between tokens, never mid-string).
func kyuHighlights(body string, theme style.Theme) []widget.StyleSpan {
	if body == "" {
		return nil
	}
	byteToRune := byteToRuneOffsets(body)
	lineStarts := kyuHLLineStarts(body)

	var spans []widget.StyleSpan
	var stringSpans [][2]int
	lx := lexer.New(body)
	for {
		tok := lx.Next()
		if tok.Kind == ktoken.EOF {
			break
		}
		start := kyuHLOffset(body, lineStarts, tok.Line, tok.Col)
		if tok.Kind == ktoken.STRING {
			end := kyuStringSpanEnd(body, start)
			stringSpans = append(stringSpans, [2]int{start, end})
			spans = append(spans, widget.StyleSpan{
				Start: byteToRune[start],
				End:   byteToRune[end],
				Style: cell.Style{Fg: theme.Success},
			})
			continue
		}
		st, ok := kyuStyleFor(tok.Kind, theme)
		if !ok {
			continue
		}
		end := min(start+len(tok.Literal), len(body))
		spans = append(spans, widget.StyleSpan{
			Start: byteToRune[start],
			End:   byteToRune[end],
			Style: st,
		})
	}

	spans = append(spans, kyuCommentHighlights(body, theme, stringSpans, byteToRune)...)
	slices.SortFunc(spans, func(a, b widget.StyleSpan) int { return a.Start - b.Start })
	return spans
}

// kyuCommentRe finds every '#'-to-end-of-line run in raw kyu source —
// see kyuHighlights on why the real lexer can't be asked for these
// directly, and why filtering out matches that land inside a STRING
// span (via stringSpans) recovers exactly the same comments the lexer
// itself would have skipped.
var kyuCommentRe = regexp.MustCompile(`#[^\n]*`)

func kyuCommentHighlights(body string, theme style.Theme, stringSpans [][2]int, byteToRune []int) []widget.StyleSpan {
	var spans []widget.StyleSpan
	st := cell.Style{Fg: theme.Muted}
	for _, m := range kyuCommentRe.FindAllStringIndex(body, -1) {
		if kyuInsideAnySpan(m[0], stringSpans) {
			continue
		}
		spans = append(spans, widget.StyleSpan{Start: byteToRune[m[0]], End: byteToRune[m[1]], Style: st})
	}
	return spans
}

func kyuInsideAnySpan(pos int, spans [][2]int) bool {
	for _, sp := range spans {
		if pos >= sp[0] && pos < sp[1] {
			return true
		}
	}
	return false
}

// kyuHLLineStarts returns the byte offset of the start of each line in
// body — mirrors deck.KyuSegmenter's own line-start table (see
// deck/kyu.go's kyuLineStarts), duplicated here rather than exported
// across the package boundary for what's a two-line helper.
func kyuHLLineStarts(body string) []int {
	starts := []int{0}
	for i := 0; i < len(body); i++ {
		if body[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// kyuHLOffset converts a 1-based (line, col) position from
// kyu/token.Token — col counted in runes from the line's start,
// matching kyu/lexer's own tracking, not bytes — into a byte offset
// into body. Mirrors deck.KyuSegmenter's own kyuOffset.
func kyuHLOffset(body string, lineStarts []int, line, col int) int {
	if line < 1 || line > len(lineStarts) {
		return 0
	}
	pos := lineStarts[line-1]
	for i := 1; i < col && pos < len(body); i++ {
		_, size := utf8.DecodeRuneInString(body[pos:])
		pos += size
	}
	return pos
}

// kyuStringSpanEnd returns the byte offset just past a STRING token's
// closing quote, given start (the byte offset of its opening '"') — the
// span is recomputed from raw body rather than trusted from the
// token's own Literal, since lexer.lexString's Literal is the *decoded*
// string value (escape sequences resolved), so its length doesn't
// match the source span whenever the string contains an escape. Walks
// the same backslash-then-one-rune escape rule lexString itself uses;
// an unterminated string (no closing quote before body ends, which can
// happen transiently while a card is mid-edit) runs to len(body).
func kyuStringSpanEnd(body string, start int) int {
	i := start + 1 // past the opening quote
	for i < len(body) {
		switch body[i] {
		case '"':
			return i + 1
		case '\\':
			i++
			if i >= len(body) {
				return len(body)
			}
			_, size := utf8.DecodeRuneInString(body[i:])
			i += size
		default:
			_, size := utf8.DecodeRuneInString(body[i:])
			i += size
		}
	}
	return len(body)
}

// searchMatchStyle is the shared "this is a search match" look — an
// inverted Accent/Background pair, the same "reversed" convention a
// terminal find bar uses — used both for editView's Highlights (via
// searchHighlights) and replaceView's inline match display, so a match
// looks the same whether you're just reading it or being asked to
// replace it.
func searchMatchStyle(theme style.Theme) cell.Style {
	return cell.Style{Fg: theme.Background, Bg: theme.Accent}
}

// searchHighlights returns one StyleSpan per occurrence of re in body,
// styled with searchMatchStyle — see mergeHighlights for combining
// these with goHighlights on the same TextArea.
func searchHighlights(re *regexp.Regexp, body string, theme style.Theme) []widget.StyleSpan {
	matches := bodyMatches(re, body)
	if len(matches) == 0 {
		return nil
	}
	st := searchMatchStyle(theme)
	spans := make([]widget.StyleSpan, len(matches))
	for i, sp := range matches {
		spans[i] = widget.StyleSpan{Start: sp[0], End: sp[1], Style: st}
	}
	return spans
}

// mergeHighlights combines base (e.g. goHighlights' syntax colors) with
// overlay (e.g. searchHighlights' match highlight) into the single
// sorted, non-overlapping span list widget.TextArea.Highlights requires
// — its own doc comment is explicit that overlapping spans produce
// undefined results, so simply concatenating the two lists isn't safe
// whenever a match falls inside (or spans across) a syntax token, which
// is the common case (searching for an identifier that's also
// highlighted as such). overlay wins: each base span is clipped to drop
// whatever portion an overlay span already covers, keeping only the
// leftover fragments before/after/around it.
func mergeHighlights(base, overlay []widget.StyleSpan) []widget.StyleSpan {
	if len(overlay) == 0 {
		return base
	}
	if len(base) == 0 {
		return overlay
	}
	out := make([]widget.StyleSpan, 0, len(base)+len(overlay))
	for _, b := range base {
		start := b.Start
		for _, o := range overlay {
			if o.End <= start || o.Start >= b.End {
				continue
			}
			if o.Start > start {
				out = append(out, widget.StyleSpan{Start: start, End: o.Start, Style: b.Style})
			}
			start = max(start, o.End)
		}
		if start < b.End {
			out = append(out, widget.StyleSpan{Start: start, End: b.End, Style: b.Style})
		}
	}
	out = append(out, overlay...)
	slices.SortFunc(out, func(a, c widget.StyleSpan) int { return a.Start - c.Start })
	return out
}

// byteToRuneOffsets returns, for every byte offset in s that starts a
// rune (plus len(s) itself), the corresponding rune offset — the
// translation StyleSpan needs since go/scanner positions are byte
// offsets but TextArea's buffer (and so its Highlights) is rune-indexed.
func byteToRuneOffsets(s string) []int {
	idx := make([]int, len(s)+1)
	rc := 0
	for i := range s {
		idx[i] = rc
		rc++
	}
	idx[len(s)] = rc
	return idx
}
