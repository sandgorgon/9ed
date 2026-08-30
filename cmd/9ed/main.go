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
	os.Exit(run())
}

// run holds everything that needs a deferred cleanup to actually fire
// — os.Exit does not run deferred calls, so main itself must never
// call it directly once there's a defer in scope (see serveBuffer's
// stop).
func run() int {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: 9ed <file>")
		return 1
	}
	path := os.Args[1]

	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "9ed:", err)
		return 1
	}
	seg, err := segmenterFor(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "9ed:", err)
		return 1
	}

	writes := make(chan p9WriteMsg)
	m := newModel(path, src, seg, seg.Segment(src), writes)
	m.view.publish(m.path, m.src, m.cards, m.edited)

	// The 9P surface is best-effort, not required to edit: a runtime
	// dir we can't create/listen on (e.g. an unwritable $XDG_RUNTIME_DIR)
	// degrades to "no scripting API this session," not a refusal to edit.
	if stop, err := serveBuffer(m.view, path, writes); err != nil {
		fmt.Fprintln(os.Stderr, "9ed: warning: 9p server disabled:", err)
	} else {
		defer stop()
	}

	app := tui.NewApp(m, 80, 24)
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "9ed:", err)
		return 1
	}
	return 0
}

// segmenterFor picks a deck.Segmenter by file extension.
func segmenterFor(path string) (deck.Segmenter, error) {
	switch filepath.Ext(path) {
	case ".go":
		return deck.GoSegmenter{}, nil
	case ".md", ".markdown":
		return deck.MarkdownSegmenter{}, nil
	case ".sh", ".bash":
		return deck.BashSegmenter{}, nil
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".hh", ".hpp", ".hxx":
		return deck.CSegmenter{}, nil
	case ".hs":
		return deck.HaskellSegmenter{}, nil
	case ".kyu":
		return deck.KyuSegmenter{}, nil
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
	src   []byte // original file content, never mutated except by a completed Save
	seg   deck.Segmenter
	cards []deck.Card

	cursor  int
	editing bool

	// edited holds a card's current text once it diverges from its
	// original src[Span[0]:Span[1]] slice — sparse, since most cards in
	// a session are never touched. Keyed by index into cards. Cleared
	// on a successful Save, since src/cards are resynced to it then.
	edited map[int]string

	saveErr string // last save's error, if any; cleared by the next successful save

	// view publishes path/src/cards/edited for the 9P server (its own
	// goroutine — see bufferview.go/fs9p.go) to read safely; called
	// after Update handles anything that changes one of those fields.
	// cursor/editing/saveErr are pure UI state, not published.
	view *bufferView

	// writes is the receive side of a cardBodyFile's Close-time commit
	// (see fs9p.go's p9WriteMsg) — Init's waitForP9Write Cmd is the
	// only reader, on tui's single event-loop goroutine, same as every
	// other model mutation.
	writes chan p9WriteMsg
}

func newModel(path string, src []byte, seg deck.Segmenter, cards []deck.Card, writes chan p9WriteMsg) *model {
	return &model{path: path, src: src, seg: seg, cards: cards, view: &bufferView{}, writes: writes}
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

// dirtyMark is the whole-deck "unsaved" indicator for the status line —
// separate from, and in addition to, the per-card '*' markers in
// navView, which show *which* cards changed rather than just whether
// anything did.
func (m *model) dirtyMark() string {
	if len(m.edited) > 0 {
		return " [unsaved]"
	}
	return ""
}

// helpStyle renders the status line in the theme's Error color after a
// failed save, Muted otherwise.
func (m *model) helpStyle() cell.Style {
	if m.saveErr != "" {
		return cell.Style{Fg: style.DefaultDark().Error}
	}
	return cell.Style{Fg: cell.ANSIColor(8)}
}

// statusLine appends the last save error (if any) to rest, so a failed
// save is visible without a dedicated status area.
func (m *model) statusLine(rest string) string {
	if m.saveErr != "" {
		return rest + "  save failed: " + m.saveErr
	}
	return rest
}

func (m *model) Init() tui.Cmd { return waitForP9Write(m.writes) }

// waitForP9Write is the long-running "listen for an external write"
// Cmd tui's docs/GUIDE.md calls for: it blocks on its own goroutine
// (never Update's) until a cardBodyFile.Close sends a request, returns
// it as the Msg Update's p9WriteMsg case applies, and — since Update
// reschedules it — is always listening again by the time the next
// write can arrive.
func waitForP9Write(writes <-chan p9WriteMsg) tui.Cmd {
	return func() tui.Msg {
		req, ok := <-writes
		if !ok {
			return nil
		}
		return req
	}
}

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
		m.view.publish(m.path, m.src, m.cards, m.edited)

	case p9WriteMsg:
		if v.cardIdx < 0 || v.cardIdx >= len(m.cards) {
			v.result <- fmt.Errorf("no such card %d", v.cardIdx)
			return m, waitForP9Write(m.writes)
		}
		m.setEdited(v.cardIdx, string(v.content))
		m.view.publish(m.path, m.src, m.cards, m.edited)
		v.result <- nil
		return m, waitForP9Write(m.writes)

	case saveDoneMsg:
		if v.err != nil {
			m.saveErr = v.err.Error()
			break
		}
		m.saveErr = ""
		m.src, m.cards, m.edited = v.src, v.cards, nil
		if m.cursor >= len(m.cards) {
			m.cursor = max(len(m.cards)-1, 0)
		}
		m.view.publish(m.path, m.src, m.cards, m.edited)

	case input.KeyEvent:
		// Ctrl+C is a global "get me out of here" regardless of mode.
		if v.Mod&input.ModCtrl != 0 && v.Rune == 'c' {
			return m, tui.Quit()
		}
		// Ctrl+S saves from either mode — confirmed safe to let TextArea
		// also see it: handleKey's literal-insert case explicitly
		// excludes ModCtrl, and nothing else in its switch matches 's',
		// so it's a no-op there, never a stray inserted character.
		if v.Mod&input.ModCtrl != 0 && v.Rune == 's' {
			return m, m.saveCmd()
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
	help := tui.Text(m.statusLine(fmt.Sprintf("%s%s  (%d cards)  —  j/k or ↑/↓: move   enter: edit   ^s: save   q: quit",
		m.path, m.dirtyMark(), len(m.cards))), m.helpStyle())

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
	help := tui.Text(m.statusLine(fmt.Sprintf("%s%s  [%s]  —  esc: back to nav   ^s: save", m.path, m.dirtyMark(), card.Kind)),
		m.helpStyle())

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
