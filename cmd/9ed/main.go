// Command 9ed is the segmented/card editor. M3 adds Edit mode: Enter
// focuses the current card in a widget.TextArea (with Go cards syntax
// highlighted), Esc returns to Nav mode. Edits live only in memory —
// Save (M4) is what reassembles and writes the file.
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

	app := tui.NewApp(newModel(path, src, seg.Segment(src)), 80, 24)
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "9ed:", err)
		os.Exit(1)
	}
}

// segmenterFor picks a deck.Segmenter by file extension. Only Go and
// Markdown exist as of M2/M3 — the rest (Bash, C/C++, Haskell, kyu)
// land in M7.
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

// model is the editor's business state: which mode (Nav/Edit), which
// card is selected, and any edited card content. Everything else
// (TextArea's own cursor/selection/undo-redo, List's scroll offset) is
// ephemeral widget-retained state, not tracked here — see
// tui/docs/DESIGN.md §3.1.
type model struct {
	path  string
	src   []byte // original file content, never mutated
	cards []deck.Card

	cursor  int
	editing bool

	// edited holds a card's current text once it diverges from its
	// original src[Span[0]:Span[1]] slice — sparse, since most cards in
	// a session are never touched. Keyed by index into cards.
	edited map[int]string
}

func newModel(path string, src []byte, cards []deck.Card) *model {
	return &model{path: path, src: src, cards: cards}
}

// cardBody returns card i's current text: the edited version if one
// exists, otherwise its original span out of src.
func (m *model) cardBody(i int) string {
	if v, ok := m.edited[i]; ok {
		return v
	}
	c := m.cards[i]
	return string(m.src[c.Span[0]:c.Span[1]])
}

func (m *model) setEdited(i int, value string) {
	if m.edited == nil {
		m.edited = make(map[int]string)
	}
	m.edited[i] = value
}

func (m *model) Init() tui.Cmd { return nil }

// navMsg/enterEditMsg are produced by the list's onEvent (see
// listEvent); editChangedMsg is produced by the textarea's OnChange
// (see editView) — never by Update's own global-key branch, so a
// focus-scoped keypress is never handled twice (the same split
// tui's examples/todo uses).
type navMsg int

const (
	navUp navMsg = iota
	navDown
)

type enterEditMsg struct{}
type editChangedMsg struct{ value string }

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

	case enterEditMsg:
		if len(m.cards) > 0 {
			m.editing = true
		}

	case editChangedMsg:
		m.setEdited(m.cursor, v.value)

	case input.KeyEvent:
		// Ctrl+C is a global "get me out of here" regardless of mode.
		if v.Mod&input.ModCtrl != 0 && v.Rune == 'c' {
			return m, tui.Quit()
		}
		if m.editing {
			// Plain Esc (unmodified) is Edit->Nav. It deliberately is
			// NOT TextArea's own ReleaseKey (see editView) — the App
			// swallows whatever key IS configured as ReleaseKey before
			// Update ever sees it (tui/tui/app.go's rawKeyClaim/
			// HandleInput), so 9ed's own mode transition needs Esc to
			// reach here untouched.
			if v.Key == input.KeyEsc && v.Mod == 0 {
				m.editing = false
			}
			return m, nil // never fall through to the 'q' check below:
			// 'q' must be an ordinary character while editing text.
		}
		if v.Rune == 'q' {
			return m, tui.Quit()
		}
	}
	return m, nil
}

func (m *model) View() tui.Node {
	if m.editing {
		return m.editView()
	}
	return m.navView()
}

func (m *model) navView() tui.Node {
	titles := make([]string, len(m.cards))
	for i, c := range m.cards {
		mark := " "
		if _, ok := m.edited[i]; ok {
			mark = "*" // edited-but-unsaved indicator
		}
		titles[i] = fmt.Sprintf("%s%-9s %s", mark, c.Kind, c.Title)
	}

	list := widget.List(titles, m.cursor, widget.ListOptions{Theme: style.DefaultDark()}, listEvent)
	help := tui.Text(fmt.Sprintf("%s  (%d cards)  —  j/k or ↑/↓: move   enter: edit   q: quit", m.path, len(m.cards)),
		cell.Style{Fg: cell.ANSIColor(8)})

	return tui.Box(layout.Vertical,
		tui.Child(layout.Fill(1), list),
		tui.Child(layout.Length(1), help),
	).Margin(1)
}

func (m *model) editView() tui.Node {
	theme := style.DefaultDark()
	card := m.cards[m.cursor]
	body := m.cardBody(m.cursor)

	var highlights []widget.StyleSpan
	if card.Kind != "" && filepath.Ext(m.path) == ".go" {
		highlights = goHighlights(body, theme)
	}

	textarea := widget.TextArea(widget.TextAreaOptions{
		Theme:      theme,
		Value:      body,
		Highlights: highlights,
		OnChange:   func(v string) tui.Msg { return editChangedMsg{value: v} },
		// A ReleaseKey distinct from plain Esc — see the input.KeyEvent
		// case in Update for why plain Esc must NOT be this widget's
		// configured release key.
		ReleaseKey: input.KeyEvent{Key: input.KeyEsc, Mod: input.ModCtrl},
	})
	help := tui.Text(fmt.Sprintf("%s  [%s]  —  esc: back to nav", m.path, card.Kind),
		cell.Style{Fg: cell.ANSIColor(8)})

	return tui.Box(layout.Vertical,
		tui.Child(layout.Fill(1), textarea),
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
	case ke.Key == input.KeyEnter:
		return enterEditMsg{}
	}
	return nil
}
