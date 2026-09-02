package deck

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// update controls whether golden writes fixtures instead of comparing
// against them — `go test ./... -update`. Same convention as
// github.com/sandgorgon/tui's internal/testutil.Golden.
var update = flag.Bool("update", false, "update golden fixtures instead of comparing against them")

// assertCoverage checks the invariant every Segmenter must hold: cards
// are non-empty (unless src is empty), ordered, contiguous, non-
// overlapping, and together cover [0, len(src)) exactly — so
// concatenating card spans in order always reconstructs src byte for
// byte. This is the property a later Save depends on.
func assertCoverage(t *testing.T, src []byte, cards []Card) {
	t.Helper()
	if len(src) == 0 {
		return
	}
	if len(cards) == 0 {
		t.Fatalf("assertCoverage: empty src produced 0 cards for %d-byte input", len(src))
	}
	if cards[0].Span[0] != 0 {
		t.Fatalf("assertCoverage: first card starts at %d, want 0", cards[0].Span[0])
	}
	for i, c := range cards {
		if c.Span[0] >= c.Span[1] {
			t.Fatalf("assertCoverage: card %d (%q) has empty/inverted span %v", i, c.Title, c.Span)
		}
		if i+1 < len(cards) && c.Span[1] != cards[i+1].Span[0] {
			t.Fatalf("assertCoverage: card %d ends at %d, card %d starts at %d — gap or overlap", i, c.Span[1], i+1, cards[i+1].Span[0])
		}
	}
	if last := cards[len(cards)-1].Span[1]; last != len(src) {
		t.Fatalf("assertCoverage: last card ends at %d, want len(src)=%d", last, len(src))
	}
}

// golden compares a text dump of cards against testdata/golden/<name>.golden.
func golden(t *testing.T, name string, cards []Card) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".golden")
	got := dumpCards(cards)

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("golden(%s): mkdir: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("golden(%s): write fixture: %v", name, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden(%s): read fixture (run `go test -update` to create it): %v", name, err)
	}
	if got != string(want) {
		t.Errorf("golden(%s): mismatch (run `go test -update` to refresh)\n--- want ---\n%s--- got ---\n%s", name, want, got)
	}
}

func dumpCards(cards []Card) string {
	var b strings.Builder
	for _, c := range cards {
		fmt.Fprintf(&b, "%-9s name=%-12s [%d,%d) %s\n", c.Kind, strconv.Quote(c.Name), c.Span[0], c.Span[1], strconv.Quote(c.Title))
	}
	return b.String()
}
