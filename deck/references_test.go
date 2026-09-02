package deck

import (
	"reflect"
	"strings"
	"testing"
)

// spanned concatenates chunks into one source, returning the source and
// each chunk's [start, end) byte span in order — a convenience for
// building Cards whose Span lines up with real byte offsets without
// hand-counting them.
func spanned(chunks ...string) (src []byte, spans [][2]int) {
	pos := 0
	for _, c := range chunks {
		spans = append(spans, [2]int{pos, pos + len(c)})
		pos += len(c)
	}
	return []byte(strings.Join(chunks, "")), spans
}

func TestReferences(t *testing.T) {
	t.Run("a caller references the card it calls", func(t *testing.T) {
		src, spans := spanned(
			"func helper() {}\n",
			"func caller() {\n\thelper()\n}\n",
		)
		cards := []Card{
			{Kind: "func", Name: "helper", Span: spans[0]},
			{Kind: "func", Name: "caller", Span: spans[1]},
		}
		refs := References(src, cards)
		if got := refs[0]; !reflect.DeepEqual(got, []int{1}) {
			t.Errorf("refs[0] (helper's referencers) = %v, want [1]", got)
		}
		if got := refs[1]; got != nil {
			t.Errorf("refs[1] (caller's referencers) = %v, want nil (nothing calls caller)", got)
		}
	})

	t.Run("a card mentioning its own name isn't counted as its own referencer", func(t *testing.T) {
		src, spans := spanned(
			"func fact(n int) int {\n\tif n == 0 { return 1 }\n\treturn n * fact(n-1)\n}\n",
		)
		cards := []Card{{Kind: "func", Name: "fact", Span: spans[0]}}
		refs := References(src, cards)
		if refs[0] != nil {
			t.Errorf("refs[0] = %v, want nil — the only card is fact itself, self-reference excluded", refs[0])
		}
	})

	t.Run("a whole-word match doesn't fire on a substring", func(t *testing.T) {
		src, spans := spanned(
			"func Foo() {}\n",
			"func Bar() {\n\tFooBar()\n\tMyFoo()\n}\n",
		)
		cards := []Card{
			{Kind: "func", Name: "Foo", Span: spans[0]},
			{Kind: "func", Name: "Bar", Span: spans[1]},
		}
		refs := References(src, cards)
		if refs[0] != nil {
			t.Errorf("refs[0] (Foo's referencers) = %v, want nil — only FooBar/MyFoo appear, not a bare Foo", refs[0])
		}
	})

	t.Run("a card with no Name is still scanned as a referencer, never as a target", func(t *testing.T) {
		src, spans := spanned(
			"func helper() {}\n",
			"var _ = helper()\n", // a "var" card with a grouped/no-Name binding, calling helper
		)
		cards := []Card{
			{Kind: "func", Name: "helper", Span: spans[0]},
			{Kind: "var", Name: "", Span: spans[1]},
		}
		refs := References(src, cards)
		if got := refs[0]; !reflect.DeepEqual(got, []int{1}) {
			t.Errorf("refs[0] (helper's referencers) = %v, want [1]", got)
		}
		if refs[1] != nil {
			t.Errorf("refs[1] should be nil — a Name-less card is never a target: %v", refs[1])
		}
	})

	t.Run("two cards sharing a Name are both flagged when referenced", func(t *testing.T) {
		src, spans := spanned(
			"void Foo::run() {}\n",
			"void Bar::run() {}\n",
			"void caller() {\n\tobj.run();\n}\n",
		)
		cards := []Card{
			{Kind: "func", Name: "run", Span: spans[0]},
			{Kind: "func", Name: "run", Span: spans[1]},
			{Kind: "func", Name: "caller", Span: spans[2]},
		}
		refs := References(src, cards)
		if got := refs[0]; !reflect.DeepEqual(got, []int{2}) {
			t.Errorf("refs[0] = %v, want [2]", got)
		}
		if got := refs[1]; !reflect.DeepEqual(got, []int{2}) {
			t.Errorf("refs[1] = %v, want [2] — both same-named cards are ambiguous hits", got)
		}
	})

	t.Run("a Markdown heading's phrase-length Name matches as a whole phrase", func(t *testing.T) {
		src, spans := spanned(
			"# Getting Started\nsome intro text\n",
			"See Getting Started above for setup steps.\n",
		)
		cards := []Card{
			{Kind: "heading", Name: "Getting Started", Span: spans[0]},
			{Kind: "preamble", Name: "", Span: spans[1]},
		}
		refs := References(src, cards)
		if got := refs[0]; !reflect.DeepEqual(got, []int{1}) {
			t.Errorf("refs[0] = %v, want [1]", got)
		}
	})

	t.Run("no card has a Name: nil overall, no panics", func(t *testing.T) {
		src, spans := spanned("just text\n", "more text\n")
		cards := []Card{
			{Kind: "preamble", Span: spans[0]},
			{Kind: "preamble", Span: spans[1]},
		}
		if refs := References(src, cards); refs != nil {
			t.Errorf("refs = %v, want nil", refs)
		}
	})
}
