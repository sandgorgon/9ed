// Package deck decomposes a source file's bytes into an ordered sequence of
// cards — contiguous, non-overlapping byte spans corresponding to a
// language's top-level structural units (a Go func, a Markdown heading,
// ...). A Segmenter's output always covers [0, len(src)) exactly: every
// byte belongs to exactly one card, in order, so the source can be
// losslessly reassembled by concatenating card spans — the invariant a
// later Save needs to hold.
package deck

// Card is one structurally meaningful unit of a segmented file.
type Card struct {
	// Title is the card's nav-mode label — a function signature, a
	// heading's text, or similar. Not necessarily unique within a deck.
	Title string
	// Span is the card's [start, end) byte range into the source that
	// produced it.
	Span [2]int
	// Kind names the card's structural role (e.g. "func", "heading",
	// "preamble", "import"). Meaning is specific to the Segmenter that
	// produced the card.
	Kind string
	// Name is the single identifier this card defines, when it
	// unambiguously has one (e.g. a func's name) — empty when the card
	// defines no single name (a "preamble" or "import" card) or more
	// than one (a grouped var/const/type block with several specs).
	// Used for lexical cross-reference badges: another card's body is
	// searched for this as a whole word. A best-effort hint, not a
	// guaranteed-unique symbol — comments, string literals, and
	// unrelated identically-named identifiers all count as matches.
	Name string
}

// Segmenter decomposes src into an ordered, contiguous, non-overlapping
// sequence of Cards covering [0, len(src)) exactly.
type Segmenter interface {
	Segment(src []byte) []Card
}
