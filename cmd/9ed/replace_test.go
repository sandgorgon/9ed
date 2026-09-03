package main

import (
	"strings"
	"testing"

	"github.com/sandgorgon/tui/input"

	"github.com/sandgorgon/9ed/deck"
)

// newReplaceTestModel segments a 4-card file (0: preamble, 1: Walrus —
// 1 self-reference, 2: Beta — 2 calls, 3: Gamma — 1 call) so a replace
// walk has to cross card boundaries and handle a multi-match card, not
// just the single-match single-card case.
func newReplaceTestModel() *model {
	src := []byte("package p\n\nfunc Walrus() {}\n\nfunc Beta() {\n\tWalrus()\n\tWalrus()\n}\n\nfunc Gamma() {\n\tWalrus()\n}\n")
	cards := deck.GoSegmenter{}.Segment(src)
	return &model{path: "f.go", src: src, cards: cards, view: &bufferView{}}
}

func TestStartReplace(t *testing.T) {
	t.Run("finds every candidate card and lands on the first match", func(t *testing.T) {
		m := newReplaceTestModel()
		m.query, m.replaceWith = "Walrus", "Seal"
		m.startReplace()

		if !m.replacing || m.searching {
			t.Fatalf("replacing=%v searching=%v, want replacing=true searching=false", m.replacing, m.searching)
		}
		want := []int{1, 2, 3}
		if len(m.replaceCandidates) != len(want) {
			t.Fatalf("replaceCandidates = %v, want %v", m.replaceCandidates, want)
		}
		for i := range want {
			if m.replaceCandidates[i] != want[i] {
				t.Errorf("replaceCandidates[%d] = %d, want %d", i, m.replaceCandidates[i], want[i])
			}
		}
		if m.cursor != 1 {
			t.Errorf("cursor = %d, want 1 (first candidate, Walrus)", m.cursor)
		}
		re, _ := searchRegexp("Walrus")
		wantSpan := bodyMatches(re, m.cardBody(1))[0]
		if m.replacePending != wantSpan {
			t.Errorf("replacePending = %v, want %v", m.replacePending, wantSpan)
		}
	})

	t.Run("no candidates leaves search mode active, replacing false", func(t *testing.T) {
		m := newReplaceTestModel()
		m.searching = true
		m.query, m.replaceWith = "NoSuchWord", "Seal"
		m.startReplace()
		if m.replacing {
			t.Error("replacing = true, want false (nothing to replace)")
		}
		if !m.searching {
			t.Error("searching = false, want true (stays in search mode, mirrors zero-match Enter)")
		}
	})
}

func TestAcceptReplace(t *testing.T) {
	m := newReplaceTestModel()
	m.query, m.replaceWith = "Walrus", "Seal"
	m.startReplace()

	m.acceptReplace() // card 1's only match

	if m.replaceCount != 1 {
		t.Errorf("replaceCount = %d, want 1", m.replaceCount)
	}
	if strings.Contains(m.cardBody(1), "Walrus") || !strings.Contains(m.cardBody(1), "Seal") {
		t.Errorf("cardBody(1) = %q, want \"Walrus\" replaced with \"Seal\"", m.cardBody(1))
	}
	// Card 1 had exactly one match, so acceptReplace's own advanceReplace
	// call should have moved on to card 2 (Beta, two matches) already.
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2 (advanced to the next candidate)", m.cursor)
	}
}

func TestSkipReplace(t *testing.T) {
	m := newReplaceTestModel()
	m.query, m.replaceWith = "Walrus", "Seal"
	m.startReplace()

	m.skipReplace()

	if m.replaceSkipped != 1 {
		t.Errorf("replaceSkipped = %d, want 1", m.replaceSkipped)
	}
	if !strings.Contains(m.cardBody(1), "Walrus") {
		t.Errorf("cardBody(1) = %q, want unchanged (skipped, not replaced)", m.cardBody(1))
	}
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2 (advanced past the skipped match)", m.cursor)
	}
}

func TestReplaceAllRemaining(t *testing.T) {
	m := newReplaceTestModel()
	m.query, m.replaceWith = "Walrus", "Seal"
	m.startReplace()

	m.replaceAllRemaining()

	if !m.replaceDone {
		t.Fatal("replaceDone = false, want true after replacing every remaining match")
	}
	if m.replaceCount != 4 || m.replaceSkipped != 0 {
		t.Errorf("replaceCount=%d replaceSkipped=%d, want 4 and 0 (1 in Walrus, 2 in Beta, 1 in Gamma)", m.replaceCount, m.replaceSkipped)
	}
	for _, idx := range []int{1, 2, 3} {
		if strings.Contains(m.cardBody(idx), "Walrus") {
			t.Errorf("cardBody(%d) still contains \"Walrus\": %q", idx, m.cardBody(idx))
		}
	}
	if got := strings.Count(m.cardBody(2), "Seal"); got != 2 {
		t.Errorf("cardBody(2) has %d \"Seal\"s, want 2 (Beta's two calls)", got)
	}
}

func TestAcceptReplaceDoesNotReMatchItsOwnInsertion(t *testing.T) {
	// A replacement that itself contains the pattern (foo -> foofoo)
	// must not be re-matched as a "new" occurrence — ordinary
	// non-overlapping substitution semantics.
	src := []byte("package p\n\nfunc F() {\n\tfoo()\n}\n")
	cards := deck.GoSegmenter{}.Segment(src)
	m := &model{path: "f.go", src: src, cards: cards, view: &bufferView{}}
	m.query, m.replaceWith = "foo", "foofoo"
	m.startReplace()

	m.acceptReplace()

	if !m.replaceDone {
		t.Fatalf("replaceDone = false after the only match, replaceCount=%d — replacement was re-matched", m.replaceCount)
	}
	if m.replaceCount != 1 {
		t.Errorf("replaceCount = %d, want 1", m.replaceCount)
	}
}

func TestLineAround(t *testing.T) {
	body := "first line\nsecond line has TARGET here\nthird line"
	start := strings.Index(body, "TARGET")
	end := start + len("TARGET")

	before, match, after := lineAround(body, start, end)

	if before != "second line has " {
		t.Errorf("before = %q, want %q", before, "second line has ")
	}
	if match != "TARGET" {
		t.Errorf("match = %q, want %q", match, "TARGET")
	}
	if after != " here" {
		t.Errorf("after = %q, want %q", after, " here")
	}
}

func TestReplaceFieldRouting(t *testing.T) {
	t.Run("toggleReplaceFieldMsg flips enteringReplacement", func(t *testing.T) {
		m := &model{}
		mm, _ := m.Update(toggleReplaceFieldMsg{})
		m = mm.(*model)
		if !m.enteringReplacement {
			t.Error("enteringReplacement = false, want true after one toggle")
		}
		mm, _ = m.Update(toggleReplaceFieldMsg{})
		m = mm.(*model)
		if m.enteringReplacement {
			t.Error("enteringReplacement = true, want false after a second toggle")
		}
	})

	t.Run("replaceInputMsg/replaceBackspaceMsg edit replaceWith, not query", func(t *testing.T) {
		m := &model{enteringReplacement: true, query: "pattern"}
		mm, _ := m.Update(replaceInputMsg{r: 'x'})
		m = mm.(*model)
		mm, _ = m.Update(replaceInputMsg{r: 'y'})
		m = mm.(*model)
		if m.replaceWith != "xy" || m.query != "pattern" {
			t.Errorf("replaceWith=%q query=%q, want replaceWith=\"xy\" query unchanged", m.replaceWith, m.query)
		}
		mm, _ = m.Update(replaceBackspaceMsg{})
		m = mm.(*model)
		if m.replaceWith != "x" {
			t.Errorf("replaceWith = %q, want \"x\" after backspace", m.replaceWith)
		}
	})

	t.Run("startSearchMsg resets the replacement field", func(t *testing.T) {
		m := &model{enteringReplacement: true, replaceWith: "leftover"}
		mm, _ := m.Update(startSearchMsg{})
		m = mm.(*model)
		if m.enteringReplacement || m.replaceWith != "" {
			t.Errorf("enteringReplacement=%v replaceWith=%q, want both reset", m.enteringReplacement, m.replaceWith)
		}
	})
}

func TestSearchKeyEventReplaceRouting(t *testing.T) {
	m := &model{searching: true}
	if got, ok := m.searchKeyEvent(input.KeyEvent{Mod: input.ModCtrl, Rune: 'r'}).(toggleReplaceFieldMsg); !ok {
		t.Errorf("searchKeyEvent(Ctrl+R) = %#v, want toggleReplaceFieldMsg", got)
	}

	m.enteringReplacement = true
	if got, ok := m.searchKeyEvent(input.KeyEvent{Rune: 'x'}).(replaceInputMsg); !ok || got.r != 'x' {
		t.Errorf("while entering replacement, searchKeyEvent('x') = %#v, want replaceInputMsg{r: 'x'}", got)
	}
}

// TestReplaceDoneDismissal regression-tests a bug found live in tmux
// while chasing an analogous one in the buffer picker (see
// bufferPickerKeyEvent's own comment for the full mechanism): tui's
// Dispatch calls Update then render() *before* checking for a focused
// widget, so whatever key ends m.replaceDone here — transitioning from
// replaceView's 0 focusables to navView's List — gets redelivered to
// that freshly-mounted List within the same event. The original "any
// key continues" dismissal meant Enter, a digit, or 'o' would silently
// trigger a real Nav action (entering Edit mode, a {n}G digit, an
// insert) immediately after the summary screen closed. Only Esc is
// safe to redeliver, since Nav's own listEvent has no bare-Esc case.
func TestReplaceDoneDismissal(t *testing.T) {
	newDoneModel := func() *model {
		return &model{replacing: true, replaceDone: true, cards: []deck.Card{{Title: "X", Kind: "func"}}}
	}

	t.Run("Esc ends the flow", func(t *testing.T) {
		m := newDoneModel()
		mm, _ := m.Update(input.KeyEvent{Key: input.KeyEsc})
		m = mm.(*model)
		if m.replacing || m.replaceDone {
			t.Errorf("replacing=%v replaceDone=%v, want both false after Esc", m.replacing, m.replaceDone)
		}
	})

	t.Run("any other key leaves the flow exactly as it was", func(t *testing.T) {
		for _, ke := range []input.KeyEvent{{Rune: 'j'}, {Rune: 'o'}, {Key: input.KeyEnter}, {Rune: '5'}} {
			m := newDoneModel()
			mm, _ := m.Update(ke)
			m = mm.(*model)
			if !m.replacing || !m.replaceDone {
				t.Errorf("key %+v ended the replace-done flow; want only Esc to", ke)
			}
		}
	})
}
