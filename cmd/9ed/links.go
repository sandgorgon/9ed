// Link navigation: 'l' from Nav mode lists the links in the current
// card (see deck.Links for what counts as one) and follows the selected
// one — a card named by anchor/symbol, a "path:line" in this file, or
// another file, which is opened *in place*: 9ed stays one process per
// buffer (see buffers.go), so following a cross-file link replaces
// this instance's file rather than spawning or switching to another.
// Each cross-file jump pushes a frame on m.backStack; Ctrl+O pops it.
// External URLs are listed (so the user can see they were recognized)
// but not opened.
//
// If another 9ed instance already has the target file open, the link is
// handed off to it instead (its /goto, the same path the buffer picker
// plumbs through): the jump shows up in *that* instance's terminal and
// this one stays exactly where it is — consistent with "a buffer is a
// process". If that instance turns out to be unreachable (a stale
// discovery file from a crash), the link is opened in place after all.
//
// Replacing the file is refused while there are unsaved changes: this
// is a single-buffer editor, so there's nowhere to keep them, and
// silently dropping them is worse than asking to save first. A hand-off
// replaces nothing, so it isn't subject to that.
//
// The picker mirrors the buffer picker's shape (buffers.go) — a plain
// List with the same 'q'-only exit, for the same Esc-redelivery reason
// bufferPickerKeyEvent documents — rather than inventing a second modal
// idiom.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sandgorgon/9ed/deck"
	"github.com/sandgorgon/9ed/notes"

	"github.com/sandgorgon/tui/input"
	"github.com/sandgorgon/tui/layout"
	"github.com/sandgorgon/tui/tui"
	"github.com/sandgorgon/tui/widget"
)

// startLinkPickerMsg is produced by listEvent on 'l' in Nav mode.
type startLinkPickerMsg struct{}
type linkPickerUpMsg struct{}
type linkPickerDownMsg struct{}
type linkPickerEnterMsg struct{}
type linkPickerBackMsg struct{}

// backLinkMsg is produced by listEvent on Ctrl+O in Nav mode: return to
// the file (and card) the last cross-file link was followed from.
type backLinkMsg struct{}

// fileFrame is one back-stack entry: where a cross-file link was
// followed from.
type fileFrame struct {
	path   string
	cursor int // card index
}

// linkAction is what following a linkEntry does.
type linkAction int

const (
	linkNoAction linkAction = iota // recognized, but can't be followed (see linkEntry.note)
	linkToCard                     // move Nav's cursor to linkEntry.card
	linkToLine                     // goToLine(linkEntry.line)
	linkToFile                     // open linkEntry.link.Path in place, then land per Line/Anchor
)

// linkEntry is one row of the picker: a deck.Link plus what following
// it would do, resolved once when the picker opens.
type linkEntry struct {
	link   deck.Link
	action linkAction
	card   int    // for linkToCard
	line   int    // for linkToLine
	note   string // for linkNoAction: why it can't be followed
}

// linkNamesFile reports whether a link's path names the open file. A
// path comment is as often written relative to the repository root
// ("deck/golang.go:27") as to the file's own directory, and 9ed has no
// idea where the root is — so a relative path matches if it's the
// file's own directory-relative path, or a trailing run of whole path
// components of the file's absolute path.
func linkNamesFile(linkPath, filePath string) bool {
	fileAbs, err := filepath.Abs(filePath)
	if err != nil {
		return false
	}
	if strings.HasPrefix(linkPath, "/") {
		return filepath.Clean(linkPath) == fileAbs
	}
	if strings.HasPrefix(linkPath, "~") {
		return false
	}
	rel := filepath.Clean(linkPath)
	if filepath.Join(filepath.Dir(fileAbs), rel) == fileAbs {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return false
	}
	return strings.HasSuffix(fileAbs, string(filepath.Separator)+rel)
}

// resolveLink decides what following l does in the open file.
func (m *model) resolveLink(l deck.Link) linkEntry {
	e := linkEntry{link: l}
	switch {
	case l.URL != "":
		e.note = "external URL — not opened from 9ed"
	case l.Path != "" && !linkNamesFile(l.Path, m.path):
		e.action = linkToFile
	case l.Line > 0:
		e.action, e.line = linkToLine, l.Line
	case l.Anchor != "":
		if idx, ok := deck.ResolveAnchor(m.cards, l.Anchor); ok {
			e.action, e.card = linkToCard, idx
		} else {
			e.note = fmt.Sprintf("no card named %q in this file", l.Anchor)
		}
	default:
		e.note = "this file"
	}
	return e
}

// currentCardLinks scans the selected card's *current* body (so
// unsaved edits count) with deck.Links.
func (m *model) currentCardLinks() []linkEntry {
	if len(m.cards) == 0 {
		return nil
	}
	body := []byte(m.cardBody(m.cursor))
	cards := []deck.Card{{Span: [2]int{0, len(body)}}}
	var out []linkEntry
	for _, l := range deck.Links(body, cards, filepath.Ext(m.path))[0] {
		out = append(out, m.resolveLink(l))
	}
	return out
}

// describeLink renders an entry's destination for the picker row.
func (m *model) describeLink(e linkEntry) string {
	switch e.action {
	case linkToCard:
		return fmt.Sprintf("card %d: %s", e.card+1, m.cards[e.card].Title)
	case linkToLine:
		return fmt.Sprintf("line %d", e.line)
	case linkToFile:
		dest := "open " + e.link.Path
		switch {
		case e.link.Line > 0:
			dest += fmt.Sprintf(" at line %d", e.link.Line)
		case e.link.Anchor != "":
			dest += " at " + e.link.Anchor
		}
		return dest
	}
	return "(" + e.note + ")"
}

// followLink acts on the selected entry: close the picker and move, or
// leave it open and say why not. The returned Cmd is non-nil only for a
// hand-off to another instance, whose result arrives as linkHandoffMsg.
func (m *model) followLink() tui.Cmd {
	if m.linkCursor < 0 || m.linkCursor >= len(m.linkList) {
		return nil
	}
	e := m.linkList[m.linkCursor]
	switch e.action {
	case linkToCard:
		m.pickingLinks = false
		m.cursor = e.card
		m.gotoLineCursor = nil
	case linkToLine:
		m.pickingLinks = false
		m.goToLine(e.line)
	case linkToFile:
		return m.followFileLink(e.link, true)
	default:
		m.linkStatus = e.note
	}
	return nil
}

// locateLinkFile finds the file a cross-file link's path names and
// reads it. An absolute path (or "~/…") is taken as written. A relative
// one is tried against the open file's directory and then each of its
// ancestors, nearest first — comments in a repository usually spell
// paths from the repo root, and 9ed doesn't know where that is.
//
// Under 9sh's namespace, the ancestor walk stays on the path as given
// (see nsopen.go: resolution never consults the OS cwd); otherwise it
// starts from the open file's absolute path so it can climb past the
// directory 9ed was launched in.
func (m *model) locateLinkFile(link string) (path string, src []byte, err error) {
	var cands []string
	switch {
	case filepath.IsAbs(link):
		cands = []string{filepath.Clean(link)}
	case strings.HasPrefix(link, "~/"):
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", nil, herr
		}
		cands = []string{filepath.Join(home, link[2:])}
	default:
		base := m.path
		if os.Getenv(nsSockEnv) == "" {
			if abs, aerr := filepath.Abs(m.path); aerr == nil {
				base = abs
			}
		}
		for dir := filepath.Dir(base); ; {
			cands = append(cands, filepath.Join(dir, link))
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	for _, c := range cands {
		if data, rerr := readFileNS(c); rerr == nil {
			return c, data, nil
		}
	}
	return "", nil, fmt.Errorf("%s: not found", link)
}

// findOpenBuffer reports another running 9ed that has path open.
//
// What "the same path" means depends on where the two instances live:
//
//   - Outside 9sh, a path is an OS path. Only absolute paths are
//     compared (a relative one means something different in every
//     process's cwd, so a match would be a coincidence).
//   - Under 9sh, a path is a *namespace* path, and means the same
//     file only to instances attached to the same namespace — /p is a
//     bind that another session may resolve differently. So the peer
//     must have advertised the same $_9SH_UNIX_SOCK as ours (a peer
//     with none — an older 9ed, or one run outside 9sh — never
//     matches). Within one namespace, relative paths are rooted at the
//     same /local for everyone (see nsopen.go: never the OS cwd), so
//     they compare by their cleaned spelling.
func findOpenBuffer(path string) (pid int, ok bool) {
	ns := os.Getenv(nsSockEnv)
	if ns == "" && !filepath.IsAbs(path) {
		return 0, false
	}
	for _, b := range discoverBuffers(os.Getpid()) {
		if b.ns != ns {
			continue
		}
		if ns != "" {
			if filepath.Clean(b.path) == filepath.Clean(path) {
				return b.pid, true
			}
		} else if filepath.IsAbs(b.path) && samePath(b.path, path) {
			return b.pid, true
		}
	}
	return 0, false
}

// linkHandoffMsg is linkHandoffCmd's result.
type linkHandoffMsg struct {
	link deck.Link
	pid  int
	line int
	err  error
}

// linkHandoffCmd plumbs line into pid's /goto (the same dial the buffer
// picker uses) from its own goroutine, so a wedged peer can't hang
// Update. The link rides along so a failure can fall back to opening
// it here.
func linkHandoffCmd(l deck.Link, pid, line int) tui.Cmd {
	return func() tui.Msg {
		r, _ := plumbBufferCmd(pid, line)().(bufferPlumbedMsg)
		return linkHandoffMsg{link: l, pid: pid, line: line, err: r.err}
	}
}

// followFileLink takes l to its file: hand off to an instance that
// already has it open (when allowHandoff), else replace this one's file
// in place and land where l says. Every failure — file not found,
// anchor not in that file, unsaved changes — is detected before
// anything is replaced, so a refused jump leaves the current file
// exactly as it was.
func (m *model) followFileLink(l deck.Link, allowHandoff bool) tui.Cmd {
	path, src, err := m.locateLinkFile(l.Path)
	if err != nil {
		m.linkStatus = err.Error()
		return nil
	}
	seg := segmenterFor(path)
	cards := seg.Segment(src)
	card := 0
	if l.Anchor != "" {
		idx, ok := deck.ResolveAnchor(cards, l.Anchor)
		if !ok {
			m.linkStatus = fmt.Sprintf("no card named %q in %s", l.Anchor, l.Path)
			return nil
		}
		card = idx
	}

	if allowHandoff {
		if pid, ok := findOpenBuffer(path); ok {
			line := l.Line
			if line == 0 {
				line = cardFirstLine(src, cards[card].Span[0])
			}
			m.linkStatus = fmt.Sprintf("handing off to pid %d…", pid)
			return linkHandoffCmd(l, pid, max(line, 1))
		}
	}

	if m.hasUnsavedChanges() {
		m.linkStatus = "unsaved changes — ^s to save, then follow"
		return nil
	}
	m.backStack = append(m.backStack, fileFrame{path: m.path, cursor: m.cursor})
	m.openFile(path, src, seg, cards)
	m.pickingLinks = false
	if l.Line > 0 {
		m.goToLine(l.Line)
	} else {
		m.cursor = card
	}
	return nil
}

// finishHandoff applies linkHandoffMsg: on success the other instance
// has moved and this one stays put; on failure the link is opened here
// instead, and the notice says the peer couldn't be reached.
func (m *model) finishHandoff(v linkHandoffMsg) {
	if v.err == nil {
		m.pickingLinks = false
		m.linkStatus = ""
		m.notice = fmt.Sprintf("opened in pid %d at line %d", v.pid, v.line)
		return
	}
	m.linkStatus = ""
	was := m.path
	m.followFileLink(v.link, false)
	if m.path != was {
		m.notice = fmt.Sprintf("pid %d unreachable — opened here", v.pid)
	} else if m.linkStatus != "" {
		m.linkStatus = fmt.Sprintf("pid %d unreachable; %s", v.pid, m.linkStatus)
	}
}

// goBack pops the back-stack and reopens the file it names, at the card
// the link was followed from. Like a forward jump it's refused while
// there are unsaved changes (reported via m.notice), and a file that
// has vanished or become unreadable leaves the stack and the current
// file untouched.
func (m *model) goBack() {
	if len(m.backStack) == 0 {
		m.notice = "nothing to go back to"
		return
	}
	if m.hasUnsavedChanges() {
		m.notice = "unsaved changes — ^s to save before going back"
		return
	}
	f := m.backStack[len(m.backStack)-1]
	src, err := readFileNS(f.path)
	if err != nil {
		m.notice = "can't go back: " + err.Error()
		return
	}
	m.backStack = m.backStack[:len(m.backStack)-1]
	seg := segmenterFor(f.path)
	cards := seg.Segment(src)
	m.openFile(f.path, src, seg, cards)
	m.cursor = min(f.cursor, max(len(cards)-1, 0))
}

// readNotes loads path's .9an sidecar, or an empty one — best-effort,
// like run()'s own startup read: no sidecar, or any read failure, just
// means no notes.
func readNotes(path string) *notes.Sidecar {
	if data, err := readFileNS(notes.SidecarPath(path)); err == nil {
		return notes.Parse(data)
	}
	return notes.New()
}

// openFile replaces the model's file with path/src/cards, resetting
// everything that was specific to the old one: edits, cursor memory,
// search, selection, notes, and the published 9P snapshot. What
// deliberately survives is the yank register — copying a card in one
// file and pasting it in another is a main use of following a link —
// and the theme.
func (m *model) openFile(path string, src []byte, seg deck.Segmenter, cards []deck.Card) {
	m.cancelPendingNav()
	m.path, m.src, m.seg, m.cards = path, src, seg, cards
	m.cursor = 0
	m.editing, m.noteEditing = false, false
	m.searching, m.query, m.activeSearch = false, "", ""
	m.edited, m.noteEdited, m.cursorPos = nil, nil, nil
	m.gotoLineCursor = nil
	m.jumpGen++
	m.saveErr = ""
	m.clearEditSelection()
	m.notesFile = readNotes(path)
	m.refs = deck.References(src, cards)
	m.view.publish(m.path, m.src, m.cards, m.edited)
	if m.onPathChange != nil {
		m.onPathChange(path)
	}
}

// linkPickerKeyEvent is the picker List's onEvent — same key set as
// bufferPickerKeyEvent, and deliberately 'q' only (not Esc) to leave,
// for the reason documented there.
func (m *model) linkPickerKeyEvent(ke input.KeyEvent) tui.Msg {
	switch {
	case ke.Rune == 'q':
		return linkPickerBackMsg{}
	case ke.Key == input.KeyUp || ke.Rune == 'k':
		return linkPickerUpMsg{}
	case ke.Key == input.KeyDown || ke.Rune == 'j':
		return linkPickerDownMsg{}
	case ke.Key == input.KeyEnter:
		return linkPickerEnterMsg{}
	}
	return nil
}

// linkPickerView renders the picker: one row per link in the current
// card, with where it points.
func (m *model) linkPickerView() tui.Node {
	titles := make([]string, len(m.linkList))
	for i, e := range m.linkList {
		text := strings.Join(strings.Fields(e.link.Text), " ")
		titles[i] = fmt.Sprintf("%-32s → %s", text, m.describeLink(e))
	}
	list := widget.List(titles, m.linkCursor, widget.ListOptions{Theme: m.theme, Frameless: true}, func(e input.Event) tui.Msg {
		ke, ok := e.(input.KeyEvent)
		if !ok {
			return nil
		}
		return m.linkPickerKeyEvent(ke)
	})

	status := fmt.Sprintf("links in this card: %d", len(m.linkList))
	if m.linkStatus != "" {
		status = m.linkStatus
	}
	help := m.statusBarNode(status+"  —  j/k: move   enter: follow   q: back", m.theme.ChromeText())

	return tui.Box(layout.Vertical,
		tui.Child(layout.Fill(1), list),
		tui.Child(layout.Length(1), help),
	).Margin(1)
}
