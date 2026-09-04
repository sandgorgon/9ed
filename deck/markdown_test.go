package deck

import (
	"os"
	"testing"
)

func TestMarkdownSegmenter(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []Card // Span omitted from the comparison here; checked via assertCoverage instead
	}{
		{
			name: "no headings",
			src:  "just some text\nno headings at all\n",
			want: []Card{{Kind: "preamble", Title: "just some text"}},
		},
		{
			name: "empty file",
			src:  "",
			want: nil,
		},
		{
			name: "heading at start, no preamble",
			src:  "# Title\n\nbody text\n",
			want: []Card{{Kind: "H1", Title: "Title", Name: "Title"}},
		},
		{
			name: "preamble then headings",
			src:  "intro text\n\n# One\nbody one\n\n## Two\nbody two\n",
			want: []Card{
				{Kind: "preamble", Title: "intro text"},
				{Kind: "H1", Title: "One", Name: "One"},
				{Kind: "H2", Title: "Two", Name: "Two"},
			},
		},
		{
			name: "bare heading, empty title",
			src:  "###\nbody\n",
			want: []Card{{Kind: "H3", Title: ""}},
		},
		{
			name: "hash inside fenced code block is not a heading",
			src:  "# Real Heading\n\n```python\n# not a heading\n```\n\nmore text\n",
			want: []Card{{Kind: "H1", Title: "Real Heading", Name: "Real Heading"}},
		},
		{
			name: "seven hashes is not a heading",
			src:  "####### too many\n",
			want: []Card{{Kind: "preamble", Title: "####### too many"}},
		},
		{
			name: "CRLF line endings",
			src:  "intro\r\n# Heading\r\nbody\r\n",
			want: []Card{
				{Kind: "preamble", Title: "intro"},
				{Kind: "H1", Title: "Heading", Name: "Heading"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := []byte(tt.src)
			cards := MarkdownSegmenter{}.Segment(src)
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

func TestMarkdownSegmenterGolden(t *testing.T) {
	src, err := os.ReadFile("testdata/markdown/sample.md")
	if err != nil {
		t.Fatal(err)
	}
	cards := MarkdownSegmenter{}.Segment(src)
	assertCoverage(t, src, cards)
	golden(t, "markdown_sample", cards)
}
