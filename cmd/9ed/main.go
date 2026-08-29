// Command 9ed is the segmented/card editor. M2 is Nav mode only: it
// segments the given file and renders its deck as a read-only,
// navigable list — proving segmentation -> render works end to end
// before Edit mode (M3) or Save (M4) exist.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sandgorgon/tui/cell"
	"github.com/sandgorgon/tui/input"
	"github.com/sandgorgon/tui/layout"
	"github.com/sandgorgon/tui/style"
	"github.com/sandgorgon/tui/tui"
	"github.com/sandgorgon/tui/widget"

	"github.com/sandgorgon/9ed/deck"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: 9ed <file>")
		os.Exit(1)
	}
	path := os.Args[1]

	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "9ed:", err)
		os.Exit(1)
	}
	seg, err := segmenterFor(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "9ed:", err)
		os.Exit(1)
	}

	app := tui.NewApp(newModel(path, seg.Segment(src)), 80, 24)
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "9ed:", err)
		os.Exit(1)
	}
}

// segmenterFor picks a deck.Segmenter by file extension. Only Go and
// Markdown exist as of M2 — the rest (Bash, C/C++, Haskell, kyu) land in
// M7.
func segmenterFor(path string) (deck.Segmenter, error) {
	switch filepath.Ext(path) {
	case ".go":
		return deck.GoSegmenter{}, nil
	case ".md", ".markdown":
		return deck.MarkdownSegmenter{}, nil
	default:
		return nil, fmt.Errorf("no segmenter for %q files yet", filepath.Ext(path))
	}
}

// model is Nav mode's state: which card is under the cursor. Read-only
// in M2 — there's no Edit mode, no dirty state, nothing to save yet.
type model struct {
	path   string
	cards  []deck.Card
	cursor int
}

func newModel(path string, cards []deck.Card) *model {
	return &model{path: path, cards: cards}
}

func (m *model) Init() tui.Cmd { return nil }

// navMsg is produced by the list's onEvent (see listEvent), not by
// Update's own global-key branch — the same split examples/todo uses,
// so a keypress that moves the cursor is never handled twice.
type navMsg int

const (
	navUp navMsg = iota
	navDown
)

func (m *model) Update(msg tui.Msg) (tui.Model, tui.Cmd) {
	switch v := msg.(type) {
	case navMsg:
		switch v {
		case navUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case navDown:
			if m.cursor < len(m.cards)-1 {
				m.cursor++
			}
		}
	case input.KeyEvent:
		if v.Rune == 'q' || (v.Mod&input.ModCtrl != 0 && v.Rune == 'c') {
			return m, tui.Quit()
		}
	}
	return m, nil
}

func (m *model) View() tui.Node {
	titles := make([]string, len(m.cards))
	for i, c := range m.cards {
		titles[i] = fmt.Sprintf("%-9s %s", c.Kind, c.Title)
	}

	list := widget.List(titles, m.cursor, widget.ListOptions{Theme: style.DefaultDark()}, listEvent)
	help := tui.Text(fmt.Sprintf("%s  (%d cards)  —  j/k or ↑/↓: move   q: quit", m.path, len(m.cards)),
		cell.Style{Fg: cell.ANSIColor(8)})

	return tui.Box(layout.Vertical,
		tui.Child(layout.Fill(1), list),
		tui.Child(layout.Length(1), help),
	).Margin(1)
}

func listEvent(e input.Event) tui.Msg {
	ke, ok := e.(input.KeyEvent)
	if !ok {
		return nil
	}
	switch {
	case ke.Key == input.KeyUp || ke.Rune == 'k':
		return navUp
	case ke.Key == input.KeyDown || ke.Rune == 'j':
		return navDown
	}
	return nil
}
