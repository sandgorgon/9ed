package deck

import (
	"os"
	"testing"
)

func TestBashSegmenter(t *testing.T) {
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
			name: "no functions at all falls back to one preamble card",
			src:  "#!/bin/bash\necho hi\n",
			want: []Card{{Kind: "preamble", Title: "#!/bin/bash"}},
		},
		{
			name: "single function, brace on the same line",
			src:  "#!/bin/bash\n\ngreet() {\n\techo hi\n}\n",
			want: []Card{
				{Kind: "preamble", Title: "#!/bin/bash"},
				{Kind: "func", Title: "greet() {"},
			},
		},
		{
			name: "function keyword form without parens",
			src:  "function greet {\n\techo hi\n}\n",
			want: []Card{{Kind: "func", Title: "function greet {"}},
		},
		{
			name: "brace on its own next line",
			src:  "greet()\n{\n\techo hi\n}\n",
			want: []Card{{Kind: "func", Title: "greet()"}},
		},
		{
			name: "leading comment block attaches to the following function",
			src:  "# greet prints a hello.\n# it takes no arguments.\ngreet() {\n\techo hi\n}\n",
			want: []Card{{Kind: "func", Title: "greet() {"}},
		},
		{
			name: "indented function inside another function is not top-level",
			src:  "outer() {\n\tinner() {\n\t\techo hi\n\t}\n\tinner\n}\n",
			want: []Card{{Kind: "func", Title: "outer() {"}},
		},
		{
			name: "two functions in sequence",
			src:  "a() {\n\techo a\n}\n\nb() {\n\techo b\n}\n",
			want: []Card{
				{Kind: "func", Title: "a() {"},
				{Kind: "func", Title: "b() {"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := []byte(tt.src)
			cards := BashSegmenter{}.Segment(src)
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

func TestBashSegmenterGolden(t *testing.T) {
	src, err := os.ReadFile("testdata/bash/sample.sh")
	if err != nil {
		t.Fatal(err)
	}
	cards := BashSegmenter{}.Segment(src)
	assertCoverage(t, src, cards)
	golden(t, "bash_sample", cards)
}
