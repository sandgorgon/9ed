package deck

import (
	"os"
	"testing"
)

func TestPlainSegmenter(t *testing.T) {
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
			name: "arbitrary text becomes one whole-file card",
			src:  "just some content\nno structure of any kind\n",
			want: []Card{{Kind: "text", Title: "just some content"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := []byte(tt.src)
			cards := PlainSegmenter{}.Segment(src)
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

func TestPlainSegmenterGolden(t *testing.T) {
	src, err := os.ReadFile("testdata/plain/sample.txt")
	if err != nil {
		t.Fatal(err)
	}
	cards := PlainSegmenter{}.Segment(src)
	assertCoverage(t, src, cards)
	golden(t, "plain_sample", cards)
}
