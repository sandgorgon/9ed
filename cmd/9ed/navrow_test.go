package main

import (
	"strings"
	"testing"

	"github.com/sandgorgon/tui/tui"

	"github.com/sandgorgon/9ed/deck"
	"github.com/sandgorgon/9ed/notes"
)

func TestFitNavRow(t *testing.T) {
	cases := []struct {
		name, prefix, title, badges string
		width                       int
		want                        string
		truncated                   bool
	}{
		{"unknown width leaves the row whole", "  func      ", "func Long()", "  ⚑", 0, "  func      func Long()  ⚑", false},
		{"a row that fits is untouched", "p ", "title", " B", 9, "p title B", false},
		{"only the title shrinks; badges survive", "p ", "abcdefghij", " B", 9, "p abcd… B", true},
		{"no badges", "p ", "abcdefghij", "", 6, "p abc…", true},
		{"wide runes are measured in columns", "p ", "日本語のタイトル", "", 9, "p 日本語…", true},
		{"a wide rune that won't fit isn't split", "p ", "日本語のタイトル", "", 8, "p 日本…", true},
		{"too narrow even for prefix+badges leaves it to tui's clip", "prefix ", "title", " badges", 5, "prefix title badges", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, truncated := fitNavRow(c.prefix, c.title, c.badges, c.width)
			if got != c.want || truncated != c.truncated {
				t.Errorf("fitNavRow = %q, %v; want %q, %v", got, truncated, c.want, c.truncated)
			}
			if c.truncated && displayWidth(got) > c.width {
				t.Errorf("row %q is %d columns, wider than %d", got, displayWidth(got), c.width)
			}
		})
	}
}

func navRowTestModel(width int) *model {
	long := "func (m *model) aVeryLongMethodNameThatWillNotFit(argumentOne string, argumentTwo int) error"
	cards := []deck.Card{
		{Kind: "preamble", Title: "package foo"},
		{Kind: "func", Title: long},
	}
	m := &model{path: "f.go", cards: cards, notesFile: notes.New(), view: &bufferView{}, width: width}
	m.notesFile.ToggleFlag("func", long, flagTodo)
	return m
}

func TestNavViewFitsRowsToWidth(t *testing.T) {
	const w = 60
	m := navRowTestModel(w)

	t.Run("a long title is shortened and its badge stays visible", func(t *testing.T) {
		app := tui.NewApp(m, w, 10)
		screen := app.Buffer().String()
		if !strings.Contains(screen, "…  "+todoGlyph) {
			t.Errorf("expected an ellipsized title followed by the todo badge, got:\n%s", screen)
		}
	})

	t.Run("status line shows the full title only when the highlighted row was shortened", func(t *testing.T) {
		m.cursor = 0
		if got := m.navStatusText(false); !strings.Contains(got, "j/k: move") {
			t.Errorf("unshortened row: expected key hints, got %q", got)
		}
		m.cursor = 1
		if got := m.navStatusText(true); !strings.HasSuffix(got, m.cards[1].Title) {
			t.Errorf("shortened row: expected the full title, got %q", got)
		}
	})

	t.Run("resizeMsg updates the width", func(t *testing.T) {
		mm, _ := m.Update(resizeMsg{cols: 120})
		if got := mm.(*model).width; got != 120 {
			t.Errorf("width = %d, want 120", got)
		}
	})
}
