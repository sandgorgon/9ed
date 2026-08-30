package deck

import (
	"os"
	"testing"
)

func TestHaskellSegmenter(t *testing.T) {
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
			name: "header comment only falls back to one preamble card",
			src:  "-- just a header, no top-level items\n",
			want: []Card{{Kind: "preamble", Title: "-- just a header, no top-level items"}},
		},
		{
			name: "module and import lines",
			src:  "module Main where\n\nimport Data.List\n",
			want: []Card{
				{Kind: "module", Title: "module Main where"},
				{Kind: "import", Title: "import Data.List"},
			},
		},
		{
			name: "a function binding with an indented where clause stays one card",
			src:  "greet name = message\n  where\n    message = \"hi, \" ++ name\n",
			want: []Card{{Kind: "decl", Title: "greet name = message"}},
		},
		{
			name: "multiple equations of the same binding stay one card",
			src:  "fact 0 = 1\nfact n = n * fact (n - 1)\n",
			want: []Card{{Kind: "decl", Title: "fact 0 = 1"}},
		},
		{
			name: "a different binding after starts a new card",
			src:  "fact 0 = 1\nfact n = n * fact (n - 1)\n\ndouble x = x * 2\n",
			want: []Card{
				{Kind: "decl", Title: "fact 0 = 1"},
				{Kind: "decl", Title: "double x = x * 2"},
			},
		},
		{
			name: "leading comment attaches to the following binding",
			src:  "-- double doubles its argument.\ndouble x = x * 2\n",
			want: []Card{{Kind: "decl", Title: "double x = x * 2"}},
		},
		{
			name: "data, type, and class declarations",
			src:  "data Color = Red | Green | Blue\n\ntype Name = String\n\nclass Greet a where\n  greet :: a -> String\n",
			want: []Card{
				{Kind: "data", Title: "data Color = Red | Green | Blue"},
				{Kind: "type", Title: "type Name = String"},
				{Kind: "class", Title: "class Greet a where"},
			},
		},
		{
			name: "a block comment's contents don't start a false boundary",
			// The embedded "double x = x * 2" line inside the {- -} is
			// text, not a real decl — it must not itself start a card,
			// and unlike a leading '--' run, a block comment isn't
			// attached to what follows: it's its own "preamble" card.
			src: "{- this { is } not\ndouble x = x * 2\na real decl -}\ndouble x = x * 2\n",
			want: []Card{
				{Kind: "preamble", Title: "{- this { is } not"},
				{Kind: "decl", Title: "double x = x * 2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := []byte(tt.src)
			cards := HaskellSegmenter{}.Segment(src)
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

func TestHaskellSegmenterGolden(t *testing.T) {
	src, err := os.ReadFile("testdata/haskell/sample.hs")
	if err != nil {
		t.Fatal(err)
	}
	cards := HaskellSegmenter{}.Segment(src)
	assertCoverage(t, src, cards)
	golden(t, "haskell_sample", cards)
}
