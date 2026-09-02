package deck

import (
	"os"
	"testing"
)

func TestGoSegmenter(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []Card
	}{
		{
			name: "empty file",
			src:  "",
			want: nil,
		},
		{
			name: "package clause only",
			src:  "package foo\n",
			want: []Card{{Kind: "preamble", Title: "package foo"}},
		},
		{
			name: "unparseable source falls back to one preamble card",
			src:  "this is not valid go{{{\n",
			want: []Card{{Kind: "preamble", Title: "this is not valid go{{{"}},
		},
		{
			name: "package, import, and a func",
			src: "package foo\n\n" +
				"import \"fmt\"\n\n" +
				"func Hello() {\n\tfmt.Println(\"hi\")\n}\n",
			want: []Card{
				{Kind: "preamble", Title: "package foo"},
				{Kind: "import", Title: "import \"fmt\""},
				{Kind: "func", Title: "func Hello() {", Name: "Hello"},
			},
		},
		{
			name: "doc comment is included in the func's span but not its title",
			src: "package foo\n\n" +
				"// Hello prints a greeting.\nfunc Hello() {}\n",
			want: []Card{
				{Kind: "preamble", Title: "package foo"},
				{Kind: "func", Title: "func Hello() {}", Name: "Hello"},
			},
		},
		{
			name: "type, var, const blocks each get their own kind",
			src: "package foo\n\n" +
				"type T int\n\n" +
				"var V = 1\n\n" +
				"const C = 2\n",
			want: []Card{
				{Kind: "preamble", Title: "package foo"},
				{Kind: "type", Title: "type T int", Name: "T"},
				{Kind: "var", Title: "var V = 1", Name: "V"},
				{Kind: "const", Title: "const C = 2", Name: "C"},
			},
		},
		{
			name: "a method's Name is just the method, no receiver prefix",
			src: "package foo\n\n" +
				"func (r *T) Bar() {}\n",
			want: []Card{
				{Kind: "preamble", Title: "package foo"},
				{Kind: "func", Title: "func (r *T) Bar() {}", Name: "Bar"},
			},
		},
		{
			name: "grouped var block has no single Name",
			src: "package foo\n\n" +
				"var (\n\tA = 1\n\tB = 2\n)\n",
			want: []Card{
				{Kind: "preamble", Title: "package foo"},
				{Kind: "var", Title: "var (", Name: ""},
			},
		},
		{
			name: "multi-name single spec has no single Name",
			src: "package foo\n\n" +
				"var A, B = 1, 2\n",
			want: []Card{
				{Kind: "preamble", Title: "package foo"},
				{Kind: "var", Title: "var A, B = 1, 2", Name: ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := []byte(tt.src)
			cards := GoSegmenter{}.Segment(src)
			assertCoverage(t, src, cards)

			if len(cards) != len(tt.want) {
				t.Fatalf("got %d cards, want %d: %+v", len(cards), len(tt.want), cards)
			}
			for i, c := range cards {
				if c.Kind != tt.want[i].Kind || c.Title != tt.want[i].Title || c.Name != tt.want[i].Name {
					t.Errorf("card %d = {Kind: %q, Title: %q, Name: %q}, want {Kind: %q, Title: %q, Name: %q}",
						i, c.Kind, c.Title, c.Name, tt.want[i].Kind, tt.want[i].Title, tt.want[i].Name)
				}
			}
		})
	}
}

func TestGoSegmenterGolden(t *testing.T) {
	src, err := os.ReadFile("testdata/golang/sample.go")
	if err != nil {
		t.Fatal(err)
	}
	cards := GoSegmenter{}.Segment(src)
	assertCoverage(t, src, cards)
	golden(t, "golang_sample", cards)
}
