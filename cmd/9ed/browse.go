// Directory browsing (backlog item 2): "files are cards of a
// directory" — one level up from the segmented-cards-of-a-file idea 9ed
// already builds everything else on. Bare `9ed` or `9ed <directory>`
// (see run()) opens this instead of erroring with "usage: 9ed <file>".
//
// A small, separate tui.Model, not a mode folded into the main editor's
// model — that struct is deeply wired to "there is exactly one open
// file already" (path/src/cards/edited etc.), and directory state has
// nothing to do with any of it. runBrowse runs its own tui.App first;
// once a file is chosen (or the user quits without choosing), it
// returns and run() proceeds exactly as it always has.
//
// Deliberately descend-only: no parent/".." navigation. 9sh's default
// namespace only exposes /local bound to the directory it was started
// in (see nsopen.go's package comment) — there's no "above the initial
// root" representable through it at all. Allowing "up" only in the
// OS-fallback path (no namespace reachable) would make the feature
// behave differently depending on invisible runtime context, so it's
// left out in both.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sandgorgon/tui/cell"
	"github.com/sandgorgon/tui/input"
	"github.com/sandgorgon/tui/layout"
	"github.com/sandgorgon/tui/style"
	"github.com/sandgorgon/tui/tui"
	"github.com/sandgorgon/tui/widget"
)

// dirEntry is listDir's uniform result, whether it came from 9sh's
// namespace (nsListDir, see nsopen.go) or plain os.ReadDir.
type dirEntry struct {
	name  string
	isDir bool
}

// listDir lists path's entries, preferring 9sh's namespace and falling
// back to plain os.ReadDir — the same "best effort, correctness
// preserved either way" shape readFileNS already uses for a single
// file. Sorted directories first, then alphabetically within each
// group — the conventional file-picker order.
func listDir(path string) ([]dirEntry, error) {
	var entries []dirEntry
	if stats, ok := nsListDir(path); ok {
		entries = make([]dirEntry, len(stats))
		for i, st := range stats {
			entries[i] = dirEntry{name: st.Name, isDir: st.Mode.IsDir()}
		}
	} else {
		des, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}
		entries = make([]dirEntry, len(des))
		for i, de := range des {
			entries[i] = dirEntry{name: de.Name(), isDir: de.IsDir()}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].isDir != entries[j].isDir {
			return entries[i].isDir
		}
		return entries[i].name < entries[j].name
	})
	return entries, nil
}

// browseResult is how browseModel hands its outcome back to runBrowse
// once tui.App.Run returns — App has no exported way to read back a
// Model's final state, so the model writes into this pointer itself
// (same goroutine, right before requesting Quit) instead.
type browseResult struct {
	path   string
	chosen bool
}

// browseModel is the directory browser's own tui.Model.
type browseModel struct {
	root    string // fixed; never navigated above (see package comment)
	cwd     string // root or a descendant — the directory currently listed
	entries []dirEntry
	cursor  int
	err     string // last listDir error, if any
	theme   style.Theme
	result  *browseResult
}

func newBrowseModel(root string, result *browseResult) *browseModel {
	m := &browseModel{
		root:   root,
		cwd:    root,
		theme:  style.Default(style.DetectAppearance(os.Getenv)),
		result: result,
	}
	m.reload()
	return m
}

// reload re-lists m.cwd, resetting the cursor — called on construction
// and every descent into a subdirectory.
func (m *browseModel) reload() {
	entries, err := listDir(m.cwd)
	if err != nil {
		m.err = err.Error()
		m.entries = nil
		return
	}
	m.err = ""
	m.entries = entries
	m.cursor = 0
}

func (m *browseModel) Init() tui.Cmd { return nil }

type browseNavMsg int

const (
	browseUp browseNavMsg = iota
	browseDown
)

type browseEnterMsg struct{}

// listEvent is the List widget's onEvent — j/k and the arrow keys move,
// Enter opens the selected entry. No typeahead filter, no insert/flag/
// note commands: this view only ever does one thing.
func (m *browseModel) listEvent(e input.Event) tui.Msg {
	ke, ok := e.(input.KeyEvent)
	if !ok {
		return nil
	}
	switch {
	case ke.Rune == 'k' || ke.Key == input.KeyUp:
		return browseUp
	case ke.Rune == 'j' || ke.Key == input.KeyDown:
		return browseDown
	case ke.Key == input.KeyEnter:
		return browseEnterMsg{}
	}
	return nil
}

func (m *browseModel) Update(msg tui.Msg) (tui.Model, tui.Cmd) {
	switch v := msg.(type) {
	case browseNavMsg:
		switch v {
		case browseUp:
			m.cursor = max(m.cursor-1, 0)
		case browseDown:
			m.cursor = min(m.cursor+1, max(len(m.entries)-1, 0))
		}

	case browseEnterMsg:
		if len(m.entries) == 0 {
			return m, nil
		}
		entry := m.entries[m.cursor]
		next := filepath.Join(m.cwd, entry.name)
		if entry.isDir {
			m.cwd = next
			m.reload()
			return m, nil
		}
		m.result.path = next
		m.result.chosen = true
		return m, tui.Quit()

	case input.KeyEvent:
		if v.Mod&input.ModCtrl != 0 && v.Rune == 'c' {
			return m, tui.Quit()
		}
		if v.Rune == 'q' {
			return m, tui.Quit()
		}
	}
	return m, nil
}

func (m *browseModel) View() tui.Node {
	titles := make([]string, len(m.entries))
	for i, e := range m.entries {
		name := e.name
		if e.isDir {
			name += "/"
		}
		titles[i] = name
	}
	list := widget.List(titles, m.cursor, widget.ListOptions{Theme: m.theme}, m.listEvent)

	status := fmt.Sprintf("%s  (%d entries)  —  j/k: move   enter: open   q: quit", m.cwd, len(m.entries))
	if m.err != "" {
		status = fmt.Sprintf("%s  —  error: %s   q: quit", m.cwd, m.err)
	}
	help := widget.StatusBar([]widget.Segment{{Text: status, Style: cell.Style{Fg: cell.ANSIColor(8)}}}, nil, nil, cell.Style{Bg: m.theme.Border})

	return tui.Box(layout.Vertical,
		tui.Child(layout.Fill(1), list),
		tui.Child(layout.Length(1), help),
	).Margin(1)
}

// runBrowse runs the directory browser rooted at root (a bare `9ed`
// invocation passes ".", `9ed <dir>` passes dir — see run()), returning
// the chosen file's path, or ok=false if the user quit without picking
// one — the latter is a clean exit, not an error.
func runBrowse(root string) (path string, ok bool) {
	result := &browseResult{}
	app := tui.NewApp(newBrowseModel(root, result), 80, 24)
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "9ed:", err)
		return "", false
	}
	return result.path, result.chosen
}
