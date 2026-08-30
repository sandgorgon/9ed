package deck

// PlainSegmenter is the fallback for any file 9ed doesn't have a
// structural Segmenter for: the whole file becomes one "text" card — the
// same fallback shape every other Segmenter already uses when it finds
// no structure to split on (an unparseable Go file, a heading-less
// Markdown file, ...), just applied unconditionally instead of as a
// fallback path within a language-specific Segmenter.
type PlainSegmenter struct{}

func (PlainSegmenter) Segment(src []byte) []Card {
	if len(src) == 0 {
		return nil
	}
	return []Card{{Title: firstLine(src), Span: [2]int{0, len(src)}, Kind: "text"}}
}
