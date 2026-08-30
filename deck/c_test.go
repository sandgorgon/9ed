package deck

import (
	"os"
	"testing"
)

func TestCSegmenter(t *testing.T) {
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
			name: "no boundary found falls back to one preamble card",
			src:  "int x\n", // no ';', no '{' — never closes a top-level construct
			want: []Card{{Kind: "preamble", Title: "int x"}},
		},
		{
			name: "simple declaration",
			src:  "int x;\n",
			want: []Card{{Kind: "decl", Title: "int x;"}},
		},
		{
			name: "function definition",
			src:  "int main() {\n\treturn 0;\n}\n",
			want: []Card{{Kind: "func", Title: "int main() {"}},
		},
		{
			name: "struct with trailing semicolon stays one card",
			src:  "struct Point {\n\tint x;\n\tint y;\n};\n",
			want: []Card{{Kind: "struct", Title: "struct Point {"}},
		},
		{
			name: "typedef struct with a name absorbs the trailing identifier and ;",
			src:  "typedef struct {\n\tint x;\n} Point;\n",
			want: []Card{{Kind: "struct", Title: "typedef struct {"}},
		},
		{
			name: "enum",
			src:  "enum Color { RED, GREEN, BLUE };\n",
			want: []Card{{Kind: "enum", Title: "enum Color { RED, GREEN, BLUE };"}},
		},
		{
			name: "consecutive #include lines merge into one preprocessor card",
			src:  "#include <stdio.h>\n#include <stdlib.h>\n\nint main() {\n\treturn 0;\n}\n",
			want: []Card{
				{Kind: "preprocessor", Title: "#include <stdio.h>"},
				{Kind: "func", Title: "int main() {"},
			},
		},
		{
			name: "braces inside a string or comment don't affect depth",
			src:  "const char *s = \"{ not a block }\";\n// a comment with { a brace }\nint x;\n",
			want: []Card{
				{Kind: "decl", Title: "const char *s = \"{ not a block }\";"},
				{Kind: "decl", Title: "int x;"}, // the attached leading comment isn't the title, same as GoSegmenter's Doc comment
			},
		},
		{
			name: "a leading // comment doesn't hijack the following struct's title or kind",
			src:  "// Point is a 2D coordinate.\ntypedef struct {\n\tint x;\n} Point;\n",
			want: []Card{{Kind: "struct", Title: "typedef struct {"}},
		},
		{
			name: "two functions in sequence",
			src:  "int a() {\n\treturn 1;\n}\n\nint b() {\n\treturn 2;\n}\n",
			want: []Card{
				{Kind: "func", Title: "int a() {"},
				{Kind: "func", Title: "int b() {"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := []byte(tt.src)
			cards := CSegmenter{}.Segment(src)
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

func TestCSegmenterGolden(t *testing.T) {
	src, err := os.ReadFile("testdata/c/sample.c")
	if err != nil {
		t.Fatal(err)
	}
	cards := CSegmenter{}.Segment(src)
	assertCoverage(t, src, cards)
	golden(t, "c_sample", cards)
}
