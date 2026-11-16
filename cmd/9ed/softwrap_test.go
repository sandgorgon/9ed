package main

import (
	"strings"
	"testing"

	"github.com/sandgorgon/tui/input"
	"github.com/sandgorgon/tui/tui"

	"github.com/sandgorgon/9ed/deck"
	"github.com/sandgorgon/9ed/notes"
)

func TestSoftWrapDefault(t *testing.T) {
	for _, path := range []string{"a.md", "a.markdown", "a.txt", "README"} {
		if !softWrapDefault(segmenterFor(path)) {
			t.Errorf("%s: want soft wrap on by default (prose)", path)
		}
	}
	for _, path := range []string{"a.go", "a.sh", "a.c", "a.hs", "a.kyu"} {
		if softWrapDefault(segmenterFor(path)) {
			t.Errorf("%s: want soft wrap off by default (code)", path)
		}
	}
}

func TestNewModelSoftWrapFollowsFileType(t *testing.T) {
	if m := newModel("a.md", nil, deck.MarkdownSegmenter{}, nil, nil, nil); !m.softWrap {
		t.Error("a.md: softWrap = false, want true")
	}
	if m := newModel("a.go", nil, deck.GoSegmenter{}, nil, nil, nil); m.softWrap {
		t.Error("a.go: softWrap = true, want false")
	}
}

func softWrapTestModel() *model {
	src := []byte("# Title\n\nThis paragraph is one long line that is much wider than the narrow test terminal.\n")
	seg := deck.MarkdownSegmenter{}
	return &model{path: "f.md", src: src, seg: seg, cards: seg.Segment(src), notesFile: notes.New(), view: &bufferView{}, softWrap: true}
}

func TestSoftWrapToggleKeys(t *testing.T) {
	t.Run("w in Nav mode", func(t *testing.T) {
		m := softWrapTestModel()
		mm, _ := m.Update(input.KeyEvent{Rune: 'w'})
		m = mm.(*model)
		if m.softWrap || m.notice != "wrap: off" {
			t.Errorf("softWrap = %v, notice = %q; want off with a notice", m.softWrap, m.notice)
		}
		mm, _ = m.Update(input.KeyEvent{Rune: 'w'})
		if m = mm.(*model); !m.softWrap {
			t.Error("second w: softWrap = false, want it back on")
		}
	})

	t.Run("Ctrl+W in Edit mode", func(t *testing.T) {
		m := softWrapTestModel()
		m.editing = true
		mm, _ := m.Update(input.KeyEvent{Rune: 'w', Mod: input.ModCtrl})
		if m = mm.(*model); m.softWrap {
			t.Error("softWrap = true, want Ctrl+W to turn it off")
		}
	})

	t.Run("plain w while editing is text, not a toggle", func(t *testing.T) {
		m := softWrapTestModel()
		m.editing = true
		mm, _ := m.Update(input.KeyEvent{Rune: 'w'})
		if m = mm.(*model); !m.softWrap {
			t.Error("softWrap flipped by a plain 'w' typed in Edit mode")
		}
	})

	t.Run("w typed into a search query is not a toggle", func(t *testing.T) {
		m := softWrapTestModel()
		m.searching = true
		mm, _ := m.Update(input.KeyEvent{Rune: 'w'})
		if m = mm.(*model); !m.softWrap {
			t.Error("softWrap flipped by a 'w' typed into the search query")
		}
	})
}

func TestOpenFileResetsSoftWrapToFileTypeDefault(t *testing.T) {
	m := softWrapTestModel()
	m.softWrap = false // toggled off while on the Markdown file
	src := []byte("package foo\n")
	m.openFile("b.go", src, deck.GoSegmenter{}, deck.GoSegmenter{}.Segment(src))
	if m.softWrap {
		t.Error("b.go: softWrap = true, want the Go default (off)")
	}
	m.openFile("c.md", src, deck.MarkdownSegmenter{}, deck.MarkdownSegmenter{}.Segment(src))
	if !m.softWrap {
		t.Error("c.md: softWrap = false, want the Markdown default (on)")
	}
}

func TestEditViewSoftWrap(t *testing.T) {
	render := func(wrap bool) string {
		m := softWrapTestModel()
		m.softWrap = wrap
		m.editing = true
		m.cursor = 0
		for i, c := range m.cards {
			if strings.Contains(c.Title, "Title") {
				m.cursor = i
			}
		}
		zero := 0
		m.gotoLineCursor, m.gotoLineCard = &zero, m.cursor
		return tui.NewApp(m, 40, 10).Buffer().String()
	}

	// 40 columns, less the 1-column margins and the "N " gutter: the
	// line breaks after "line", and its continuation rows carry no
	// line number.
	got := render(true)
	for _, want := range []string{"3 This paragraph is one long line", "  that is much wider than the narrow", "  test terminal."} {
		if !strings.Contains(got, want) {
			t.Errorf("wrapped: missing row %q, got:\n%s", want, got)
		}
	}
	if got := render(false); strings.Contains(got, "test terminal.") {
		t.Errorf("unwrapped: want the long line cut at the edge (scrolling sideways), got:\n%s", got)
	}
}
