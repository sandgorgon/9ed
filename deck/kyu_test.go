package deck

import (
	"os"
	"testing"
)

func TestKyuSegmenter(t *testing.T) {
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
			name: "unparseable source falls back to one preamble card",
			src:  ")))",
			want: []Card{{Kind: "preamble", Title: ")))"}},
		},
		{
			name: "comment-only file falls back to one preamble card",
			src:  "# just a comment, no statements\n",
			want: []Card{{Kind: "preamble", Title: "# just a comment, no statements"}},
		},
		{
			name: "single define",
			src:  "x := 5\n",
			want: []Card{{Kind: "define", Title: "x := 5"}},
		},
		{
			name: "leading comment becomes a preamble card",
			src:  "# a header comment\n\nx := 5\n",
			want: []Card{
				{Kind: "preamble", Title: "# a header comment"},
				{Kind: "define", Title: "x := 5"},
			},
		},
		{
			name: "define then field assign",
			src:  "x := 0\n\njob.ctl = \"stop\"\n",
			want: []Card{
				{Kind: "define", Title: "x := 0"},
				{Kind: "assign", Title: "job.ctl = \"stop\""},
			},
		},
		{
			name: "extra whitespace before := doesn't affect the identifier's start",
			src:  "x   :=   5\n",
			want: []Card{{Kind: "define", Title: "x   :=   5"}},
		},
		{
			name: "bind statement",
			src:  "bind /a, /b\n",
			want: []Card{{Kind: "bind", Title: "bind /a, /b"}},
		},
		{
			name: "bare expr statement",
			src:  "1 + 2 * 3\n",
			want: []Card{{Kind: "expr", Title: "1 + 2 * 3"}},
		},
		{
			name: "pipe/closure/field-access expr statement starts at its leftmost ident",
			src:  "jobs | where { |j| j.status == \"running\" }\n",
			want: []Card{{Kind: "expr", Title: "jobs | where { |j| j.status == \"running\" }"}},
		},
		{
			name: "three statements in order",
			src:  "a := 1\n\nb := a + 1\n\nbind /x, /y\n",
			want: []Card{
				{Kind: "define", Title: "a := 1"},
				{Kind: "define", Title: "b := a + 1"},
				{Kind: "bind", Title: "bind /x, /y"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := []byte(tt.src)
			cards := KyuSegmenter{}.Segment(src)
			assertCoverage(t, src, cards)

			if len(cards) != len(tt.want) {
				t.Fatalf("got %d cards, want %d: %+v", len(cards), len(tt.want), cards)
			}
			for i, c := range cards {
				if c.Kind != tt.want[i].Kind || c.Title != tt.want[i].Title {
					t.Errorf("card %d = {Kind: %q, Title: %q}, want {Kind: %q, Title: %q}",
						i, c.Kind, c.Title, tt.want[i].Kind, tt.want[i].Title)
				}
			}
		})
	}
}

func TestKyuSegmenterGolden(t *testing.T) {
	src, err := os.ReadFile("testdata/kyu/sample.kyu")
	if err != nil {
		t.Fatal(err)
	}
	cards := KyuSegmenter{}.Segment(src)
	assertCoverage(t, src, cards)
	golden(t, "kyu_sample", cards)
}
