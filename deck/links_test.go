package deck

import (
	"reflect"
	"testing"
)

// linkTexts flattens a Links result to each link's Text, for the
// tests that only care which spans were recognized.
func linkTexts(ls []Link) []string {
	var out []string
	for _, l := range ls {
		out = append(out, l.Text)
	}
	return out
}

func TestLinksGoComments(t *testing.T) {
	src := []byte(`package p

// see deck/golang.go:27 and https://example.com/x.
func A() {
	s := "b/c.go:3 is only a string"
	_ = s // @see Walrus
	/* ref: other.md#install, and [d](docs/x.md) */
}
`)
	cards := GoSegmenter{}.Segment(src)
	got := Links(src, cards, ".go")

	var all []Link
	for _, ls := range got {
		all = append(all, ls...)
	}
	want := []Link{
		{Text: "deck/golang.go:27", Path: "deck/golang.go", Line: 27},
		{Text: "https://example.com/x", URL: "https://example.com/x"},
		{Text: "@see Walrus", Anchor: "Walrus"},
		{Text: "ref: other.md#install", Path: "other.md", Anchor: "install"},
		{Text: "[d](docs/x.md)", Path: "docs/x.md"},
	}
	if len(all) != len(want) {
		t.Fatalf("got %d links %v, want %d", len(all), linkTexts(all), len(want))
	}
	for i := range want {
		g := all[i]
		g.Span = [2]int{}
		if !reflect.DeepEqual(g, want[i]) {
			t.Errorf("link %d = %+v, want %+v", i, g, want[i])
		}
		if text := string(src[all[i].Span[0]:all[i].Span[1]]); text != all[i].Text {
			t.Errorf("link %d Span covers %q, Text is %q", i, text, all[i].Text)
		}
	}
}

func TestLinksBelongToTheirCard(t *testing.T) {
	src, spans := spanned(
		"package p\n",
		"// a.go:1\nfunc A() {}\n",
		"// b.go:2\nfunc B() {}\n",
	)
	cards := []Card{
		{Kind: "preamble", Span: spans[0]},
		{Kind: "func", Name: "A", Span: spans[1]},
		{Kind: "func", Name: "B", Span: spans[2]},
	}
	got := Links(src, cards, ".go")
	if got[0] != nil {
		t.Errorf("preamble links = %v, want none", got[0])
	}
	if len(got[1]) != 1 || got[1][0].Path != "a.go" {
		t.Errorf("card A links = %+v, want one to a.go", got[1])
	}
	if len(got[2]) != 1 || got[2][0].Path != "b.go" || got[2][0].Line != 2 {
		t.Errorf("card B links = %+v, want one to b.go:2", got[2])
	}
}

func TestLinksMarkdownScansWholeBody(t *testing.T) {
	src := []byte("# T\n\nSee [guide](#install-guide), [o](other.md#x), `main.go:12`.\n")
	cards := MarkdownSegmenter{}.Segment(src)
	got := Links(src, cards, ".md")
	var all []Link
	for _, ls := range got {
		all = append(all, ls...)
	}
	want := []Link{
		{Text: "[guide](#install-guide)", Anchor: "install-guide"},
		{Text: "[o](other.md#x)", Path: "other.md", Anchor: "x"},
		{Text: "main.go:12", Path: "main.go", Line: 12},
	}
	if len(all) != len(want) {
		t.Fatalf("got %v, want %d links", linkTexts(all), len(want))
	}
	for i := range want {
		g := all[i]
		g.Span = [2]int{}
		if !reflect.DeepEqual(g, want[i]) {
			t.Errorf("link %d = %+v, want %+v", i, g, want[i])
		}
	}
}

func TestLinksMarkdownLinkDoesNotDoubleCountItsURL(t *testing.T) {
	src := []byte("[x](https://a.example/p.go:3) and https://b.example/\n")
	got := Links(src, PlainSegmenter{}.Segment(src), ".txt")[0]
	if want := []string{"[x](https://a.example/p.go:3)", "https://b.example/"}; !reflect.DeepEqual(linkTexts(got), want) {
		t.Errorf("links = %v, want %v", linkTexts(got), want)
	}
}

func TestLinksCommentAwareness(t *testing.T) {
	tests := []struct {
		name string
		ext  string
		src  string
		want []string
	}{
		{"c line comment", ".c", "int x; // a.c:1\n", []string{"a.c:1"}},
		{"c block comment", ".c", "/* see b.h:2 */ int y;\n", []string{"b.h:2"}},
		{"c string is not a comment", ".c", "char *s = \"// c.c:3\"; // d.c:4\n", []string{"d.c:4"}},
		{"c unterminated string ends at the line", ".c", "char *s = \"oops;\n// e.c:5\n", []string{"e.c:5"}},
		{"bash comment", ".sh", "echo hi # a.sh:1\n", []string{"a.sh:1"}},
		{"bash quoted hash is not a comment", ".sh", "echo \"# b.sh:2\" '# c.sh:3'\n", nil},
		{"bash $# is not a comment", ".sh", "echo $# a.sh:9\n", nil},
		{"bash a#b is not a comment", ".sh", "echo a#b.sh:4\n", nil},
		{"haskell line comment", ".hs", "x = 1 -- a.hs:1\n", []string{"a.hs:1"}},
		{"haskell block comment nests", ".hs", "{- {- a.hs:2 -} b.hs:3 -} x = 1\n", []string{"a.hs:2", "b.hs:3"}},
		{"haskell operator is not a comment", ".hs", "x = a --> b.hs:5\n", nil},
		{"haskell string", ".hs", "s = \"-- c.hs:6\"\n", nil},
		{"kyu uses hash comments", ".kyu", "x := 1 # a.kyu:7\n", []string{"a.kyu:7"}},
		{"unknown ext scans everything", ".py", "s = 'a.py:1'  # b.py:2\n", []string{"a.py:1", "b.py:2"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := []byte(tt.src)
			cards := []Card{{Kind: "x", Span: [2]int{0, len(src)}}}
			got := linkTexts(Links(src, cards, tt.ext)[0])
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("links = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLinksMarkerTargets(t *testing.T) {
	tests := []struct {
		text string
		want Link
	}{
		{"// @see Walrus", Link{Anchor: "Walrus"}},
		{"// @see Walrus.", Link{Anchor: "Walrus"}},
		{"// ref: `Walrus`", Link{Anchor: "Walrus"}},
		{"// @see foo.go", Link{Path: "foo.go"}},
		{"// @see ../x/foo.go:Beta", Link{Path: "../x/foo.go", Anchor: "Beta"}},
		{"// @see foo.go:42", Link{Path: "foo.go", Line: 42}},
		{"// @see #heading", Link{Anchor: "heading"}},
		{"// @see https://e.com/a", Link{URL: "https://e.com/a"}},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			src := []byte(tt.text + "\n")
			got := Links(src, []Card{{Span: [2]int{0, len(src)}}}, ".go")[0]
			if len(got) != 1 {
				t.Fatalf("links = %v, want exactly one", linkTexts(got))
			}
			g := got[0]
			g.Span, g.Text = [2]int{}, ""
			if !reflect.DeepEqual(g, tt.want) {
				t.Errorf("link = %+v, want %+v", g, tt.want)
			}
		})
	}
}

func TestLinksDoNotFireOnProse(t *testing.T) {
	for _, text := range []string{
		"// see Walrus and the time:10 mark, std::x, issue #38",
		"// version 1.2.3:4 and 10.0.0.1:8080",
		"// a bare see: Walrus is not a marker",
		"// mailto:someone@example.com",
	} {
		src := []byte(text + "\n")
		if got := Links(src, []Card{{Span: [2]int{0, len(src)}}}, ".go")[0]; len(got) != 0 {
			t.Errorf("%q: links = %v, want none", text, linkTexts(got))
		}
	}
}

func TestLinksGoSurvivesSyntaxError(t *testing.T) {
	src := []byte("import {\n// a.go:1\n}\n")
	cards := GoSegmenter{}.Segment(src)
	got := Links(src, cards, ".go")
	if len(got) != 1 || len(got[0]) != 1 || got[0][0].Path != "a.go" {
		t.Errorf("links = %+v, want one to a.go despite the parse error", got)
	}
}

func TestLinksEmpty(t *testing.T) {
	if got := Links(nil, nil, ".go"); len(got) != 0 {
		t.Errorf("Links(nil) = %v, want empty", got)
	}
}

func TestResolveAnchor(t *testing.T) {
	cards := []Card{
		{Kind: "preamble", Title: "# Doc"},
		{Kind: "H2", Title: "Install Guide", Name: "Install Guide"},
		{Kind: "H2", Title: "Config & Env", Name: "Config & Env"},
		{Kind: "func", Title: "func Walrus() {", Name: "Walrus"},
		{Kind: "H2", Title: "Install Guide", Name: "Install Guide"},
	}
	tests := []struct {
		anchor string
		want   int
		ok     bool
	}{
		{"Walrus", 3, true},
		{"Install Guide", 1, true},   // exact, first duplicate wins
		{"install-guide", 1, true},   // slug
		{"config--env", 2, true},     // '&' dropped, spaces kept as two hyphens
		{"WALRUS", 3, true},          // slug is case-insensitive
		{"func Walrus() {", 3, true}, // exact Title
		{"func-walrus", 0, false},    // Title is never slugged
		{"missing", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, ok := ResolveAnchor(cards, tt.anchor)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ResolveAnchor(%q) = %d, %v; want %d, %v", tt.anchor, got, ok, tt.want, tt.ok)
		}
	}
}
