// Command 9ed is a segmented/card TUI text editor: `9ed <file>` decomposes
// the file into structurally meaningful cards (see package deck) instead
// of editing it as an undifferentiated block of lines. Nav mode navigates
// the card list; Enter focuses the current card in Edit mode for direct
// text editing, Esc returns to Nav. Ctrl+S reassembles every card back
// into the file and writes it atomically; 't' toggles the light/dark
// theme.
//
// Every running buffer also serves its own state over 9P (see fs9p.go) —
// a Unix-domain-socket server scriptable from kyu or any shell — and,
// under a 9sh session started with -listen-unix, opens and saves through
// that session's namespace instead of raw OS calls (see nsopen.go).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/sandgorgon/tui/cell"
	"github.com/sandgorgon/tui/input"
	"github.com/sandgorgon/tui/layout"
	"github.com/sandgorgon/tui/style"
	"github.com/sandgorgon/tui/tui"
	"github.com/sandgorgon/tui/widget"

	"github.com/sandgorgon/9ed/deck"
)

// version is overridden at build time via -ldflags "-X main.version=vX.Y.Z"
// (see .github/workflows/release.yml), matching 9sh's own convention —
// "dev" for a plain `go build`/`go run`.
var version = "dev"

const helpText = `usage: 9ed <file>
       9ed -h | --help
       9ed -version | --version

9ed decomposes <file> into structurally meaningful cards (see package
deck) instead of editing it as an undifferentiated block of lines.

Nav mode:
  j/k, ↑/↓     move
  gg / G       first / last card
  {n}G         goto line n
  PgUp/PgDn    page up/down
  /            filter cards
  enter        edit current card
  o / O        insert card below / above
  t            toggle light/dark theme
  ^s           save
  q, ^c        quit

Edit mode:
  esc          back to Nav
  ^↑ / ^↓      jump to previous / next card, staying in Edit
  ^s           save
  ^c           quit
`

func main() {
	os.Exit(run())
}

// run holds everything that needs a deferred cleanup to actually fire
// — os.Exit does not run deferred calls, so main itself must never
// call it directly once there's a defer in scope (see serveBuffer's
// stop).
func run() int {
	if len(os.Args) == 2 && (os.Args[1] == "-version" || os.Args[1] == "--version") {
		fmt.Println("9ed " + version)
		return 0
	}
	if len(os.Args) == 2 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		fmt.Print(helpText)
		return 0
	}
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: 9ed <file>")
		return 1
	}
	path := os.Args[1]

	// Prefer 9sh's namespace when one is reachable (see nsopen.go): it
	// honors any rebind the user has set up at /local, which raw OS
	// calls would silently bypass. Outside 9sh, or under a 9sh that
	// wasn't started with -listen-unix, nsReadFile always reports
	// ok=false and this falls back to the plain os.ReadFile 9ed has
	// always used.
	src, ok := nsReadFile(path)
	if !ok {
		var err error
		src, err = os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "9ed:", err)
			return 1
		}
	}
	seg := segmenterFor(path)

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

// segmenterFor picks a deck.Segmenter by file extension, falling back to
// deck.PlainSegmenter (one whole-file card) for anything it doesn't
// recognize — the same "no structure found" shape every other Segmenter
// already degrades to, just applied unconditionally instead of refusing
// to open the file at all.
func segmenterFor(path string) deck.Segmenter {
	switch filepath.Ext(path) {
	case ".go":
		return deck.GoSegmenter{}
	case ".md", ".markdown":
		return deck.MarkdownSegmenter{}
	case ".sh", ".bash":
		return deck.BashSegmenter{}
	case ".c", ".h", ".cc", ".cpp", ".cxx", ".hh", ".hpp", ".hxx":
		return deck.CSegmenter{}
	case ".hs":
		return deck.HaskellSegmenter{}
	case ".kyu":
		return deck.KyuSegmenter{}
	default:
		return deck.PlainSegmenter{}
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

	// pendingG is true right after a lone 'g' in Nav mode, waiting to
	// see if a second 'g' completes vim's "go to first card" — reset by
	// every other Update branch that represents a real alternate
	// action (see the navG case for why that reset can't just live
	// unconditionally at the top of Update instead), so an unrelated
	// 'g' somewhere later never falsely completes it.
	pendingG bool

	// pendingCount accumulates digit runes typed before 'G' — vim's
	// "{count}G" jumps to line {count} instead of the last card (see
	// goto.go) — reset the same way, and for the same reason, as
	// pendingG; see cancelPendingNav.
	pendingCount string

	// searching/query/preSearchCursor are the Nav-mode typeahead filter
	// (see search.go): '/' starts it, preSearchCursor is m.cursor at
	// that point so Esc can restore it, and query is what's been typed
	// so far.
	searching       bool
	query           string
	preSearchCursor int

	// gotoLineCursor/gotoLineCard (see goto.go's goToLine) are the rune
	// offset within a card's body the cursor should open at, and which
	// card that applies to — editView only honors gotoLineCursor when
	// gotoLineCard == cursor, so every other edit-mode-entry/transition
	// path (enterEditMsg, insertMsg, jumpCard, Esc) clears gotoLineCursor
	// to nil, or a later plain Enter reopening the same card would
	// incorrectly reapply a stale line target instead of the normal
	// default cursor position.
	gotoLineCursor *int
	gotoLineCard   int

	// edited holds a card's current text once it diverges from its
	// original src[Span[0]:Span[1]] slice — sparse, since most cards in
	// a session are never touched. Keyed by index into cards. Cleared
	// on a successful Save, since src/cards are resynced to it then.
	edited map[int]string

	saveErr string // last save's error, if any; cleared by the next successful save

	// theme is the active color theme, initially style.DefaultDark or
	// style.DefaultLight per style.DetectAppearance's $COLORFGBG-based
	// guess (see newModel), and flippable at runtime with 't' (see the
	// input.KeyEvent case in Update) — every render reads m.theme rather
	// than calling style.DefaultDark() directly, so the toggle actually
	// takes effect.
	theme style.Theme

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
	theme := style.Default(style.DetectAppearance(os.Getenv))
	return &model{path: path, src: src, seg: seg, cards: cards, theme: theme, view: &bufferView{}, writes: writes}
}

// toggleTheme flips between the light and dark defaults — a plain swap,
// not a 3-way cycle back through auto-detection: once the user has
// picked one for this session, that choice sticks until they toggle
// again, regardless of what $COLORFGBG says.
func (m *model) toggleTheme() {
	if m.theme.Appearance == style.Light {
		m.theme = style.DefaultDark()
	} else {
		m.theme = style.DefaultLight()
	}
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
		return cell.Style{Fg: m.theme.Error}
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
	navG        // a lone 'g' — see model.pendingG
	navLast     // 'G'
	navPageUp   // PgUp
	navPageDown // PgDn
)

// navPageSize is how far PgUp/PgDn move the cursor — a fixed amount, not
// tied to the real viewport height: List already scrolls to keep the
// cursor row visible, so an exact match isn't needed for it to look and
// feel like a page jump.
const navPageSize = 10

type enterEditMsg struct{}
type editChangedMsg struct{ value string }

func (m *model) Update(msg tui.Msg) (tui.Model, tui.Cmd) {
	switch v := msg.(type) {
	case navMsg:
		switch v {
		case navUp:
			m.cancelPendingNav()
			if m.cursor > 0 {
				m.cursor--
			}
		case navDown:
			m.cancelPendingNav()
			if m.cursor < len(m.cards)-1 {
				m.cursor++
			}
		case navG:
			// Every keypress reaches Update twice — once as the raw
			// input.KeyEvent (tui/app.go's HandleInput dispatches it
			// unconditionally, synchronously) and once, asynchronously,
			// as whatever Msg the focused widget's onEvent produced
			// (see listEvent) — so pendingG can't just be reset
			// unconditionally at the top of Update: by the time a
			// second 'g' press's own navG arrives, its *own* raw
			// KeyEvent dispatch would already have reset the flag the
			// first 'g' just set. Resetting it individually in every
			// *other* branch below (never in the raw KeyEvent case's
			// harmless fall-through, which is exactly what a lone 'g'
			// or 'j'/'k'/'o'/'O'/Enter's redundant raw echo hits)
			// avoids that. A pending count doesn't combine with 'g'
			// either way ("2g" isn't a sequence 9ed recognizes), so
			// it's simply dropped here.
			m.pendingCount = ""
			if m.pendingG {
				m.cursor = 0
				m.pendingG = false
			} else {
				m.pendingG = true
			}
		case navLast:
			// A pending count makes 'G' jump to that line instead of
			// the last card — vim's "{count}G" — see goto.go.
			if m.pendingCount != "" {
				n, _ := strconv.Atoi(m.pendingCount)
				m.pendingCount = ""
				m.goToLine(n)
				break
			}
			m.cancelPendingNav()
			m.cursor = max(len(m.cards)-1, 0)
		case navPageUp:
			m.cancelPendingNav()
			m.cursor = max(m.cursor-navPageSize, 0)
		case navPageDown:
			m.cancelPendingNav()
			m.cursor = min(m.cursor+navPageSize, max(len(m.cards)-1, 0))
		}

	case navDigitMsg:
		// Mirrors navG: a digit while pendingG was true cancels the
		// g-sequence rather than combining with it ("g2" isn't
		// meaningful either).
		m.pendingG = false
		m.pendingCount += string(rune(v))

	case enterEditMsg:
		m.cancelPendingNav()
		m.gotoLineCursor = nil
		if len(m.cards) > 0 {
			m.editing = true
		}

	case editChangedMsg:
		m.setEdited(m.cursor, v.value)
		m.view.publish(m.path, m.src, m.cards, m.edited)

	case insertMsg:
		m.cancelPendingNav()
		m.gotoLineCursor = nil
		idx, pos := 0, 0
		if len(m.cards) > 0 {
			idx, pos = m.cursor+1, m.cards[m.cursor].Span[1]
			if v == insertAbove {
				idx, pos = m.cursor, m.cards[m.cursor].Span[0]
			}
		}
		m.insertCard(idx, pos)
		m.cursor = idx
		m.editing = true
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

	case startSearchMsg:
		m.cancelPendingNav()
		m.searching = true
		m.query = ""
		m.preSearchCursor = m.cursor

	case searchInputMsg:
		m.query += string(v.r)
		m.snapCursorToFiltered()

	case searchBackspaceMsg:
		if m.query != "" {
			r := []rune(m.query)
			m.query = string(r[:len(r)-1])
		}
		m.snapCursorToFiltered()

	case searchMoveMsg:
		f := m.filteredIndices()
		p := max(slices.Index(f, m.cursor), 0)
		if next := p + v.delta; next >= 0 && next < len(f) {
			m.cursor = f[next]
		}

	case cancelSearchMsg:
		m.searching = false
		m.cursor = m.preSearchCursor

	case commitSearchMsg:
		m.searching = false
		if len(m.filteredIndices()) > 0 {
			m.editing = true
		}

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
			m.cancelPendingNav()
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
				// Backing out of a still-empty inserted card (see
				// insertMsg) removes it rather than leaving a blank
				// "new" row until the next Save — a UX nicety, not a
				// correctness requirement: Save's resegmentation would
				// drop it either way, since an untouched zero-width
				// span reassembles to nothing.
				m.abandonEmptyInsert()
				m.gotoLineCursor = nil
				m.editing = false
				return m, nil
			}
			// Ctrl+Up/Down jump to the previous/next card's body
			// without leaving Edit mode — confirmed free of TextArea's
			// own key claims (widget/textarea.go's handleKey has no
			// Ctrl+Up/Down case), so, like plain Esc, they reach here
			// unclaimed.
			if v.Mod&input.ModCtrl != 0 && v.Key == input.KeyDown {
				m.jumpCard(1)
				return m, nil
			}
			if v.Mod&input.ModCtrl != 0 && v.Key == input.KeyUp {
				m.jumpCard(-1)
				return m, nil
			}
			return m, nil // never fall through to the 'q' check below:
			// 'q' must be an ordinary character while editing text.
		}
		if v.Rune == 't' {
			m.toggleTheme()
			return m, nil
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
	indices := make([]int, len(m.cards))
	for i := range indices {
		indices[i] = i
	}
	if m.searching {
		indices = m.filteredIndices()
	}
	titles := make([]string, len(indices))
	cursorInList := 0
	for pos, i := range indices {
		c := m.cards[i]
		mark := " "
		if _, ok := m.edited[i]; ok {
			mark = "*" // edited-but-unsaved indicator
		}
		titles[pos] = fmt.Sprintf("%s%-9s %s", mark, c.Kind, c.Title)
		if i == m.cursor {
			cursorInList = pos
		}
	}

	list := widget.List(titles, cursorInList, widget.ListOptions{Theme: m.theme}, m.listEvent)

	var help tui.Node
	if m.searching {
		help = tui.Text(fmt.Sprintf("/%s  (%d match%s)  —  enter: go   esc: cancel", m.query, len(indices), pluralS(len(indices))), m.helpStyle())
	} else {
		help = tui.Text(m.statusLine(fmt.Sprintf("%s%s  (%d cards)  —  j/k: move   gg/G: first/last   PgUp/PgDn: page   {n}G: goto line   /: filter   enter: edit   o/O: insert   ^s: save   t: theme   q: quit",
			m.path, m.dirtyMark(), len(m.cards))), m.helpStyle())
	}

	return tui.Box(layout.Vertical,
		tui.Child(layout.Fill(1), list),
		tui.Child(layout.Length(1), help),
	).Margin(1)
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

func (m *model) editView() tui.Node {
	theme := m.theme
	card := m.cards[m.cursor]
	body := m.cardBody(m.cursor)

	var highlights []widget.StyleSpan
	if card.Kind != "" && filepath.Ext(m.path) == ".go" {
		highlights = goHighlights(body, theme)
	}

	// gotoLineCursor only applies to the specific card it was computed
	// for (see goto.go's goToLine) — every other edit-mode-entry path
	// clears it, but the card-match check here is the actual guard.
	var initialCursor *int
	if m.gotoLineCursor != nil && m.gotoLineCard == m.cursor {
		initialCursor = m.gotoLineCursor
	}

	// Gutter shows the file-absolute line number (not restarting at 1
	// per card) — the only version actually useful for correlating with
	// go build/grep -n/stack trace output. lineIdx is 0-based within
	// this card's own body (confirmed reading tui's gutterWidth/
	// paintGutterRow), so it's offset by the card's own starting line,
	// computed once here rather than per visible row.
	firstLine := cardFirstLine(m.src, card.Span[0])
	gutter := func(lineIdx int) (string, cell.Style) {
		return strconv.Itoa(firstLine + lineIdx), theme.MutedText()
	}

	textarea := widget.TextArea(widget.TextAreaOptions{
		Theme:         theme,
		Value:         body,
		Highlights:    highlights,
		InitialCursor: initialCursor,
		Gutter:        gutter,
		OnChange:      func(v string) tui.Msg { return editChangedMsg{value: v} },
		// A ReleaseKey distinct from plain Esc — see the input.KeyEvent
		// case in Update for why plain Esc must NOT be this widget's
		// configured release key.
		ReleaseKey: input.KeyEvent{Key: input.KeyEsc, Mod: input.ModCtrl},
	}).Key(card.Span)
	// Keyed by the card's own Span, not m.cursor — TextArea's Value is
	// only applied at mount (tui's reconciler otherwise matches by tree
	// position alone, per docs/DESIGN.md §3.1, and this Node's tree
	// position never changes between cards), so without a distinguishing
	// key a cross-card jump (Ctrl+Up/Down, see jumpCard) would reuse the
	// previous card's retained widget instance and keep showing its
	// content. Keying by m.cursor's raw index isn't quite enough on its
	// own: jumpCard's abandon-during-forward-jump case (see its own doc
	// comment) can leave the cursor's *value* unchanged even though the
	// card actually shown there is now a different one — abandoning
	// removes a card ahead of it in the same slice, so whatever shifts
	// into m.cursor's old slot is new content at the same index. Span is
	// unique per currently-live card (the coverage invariant guarantees
	// no two cards overlap), so it changes exactly when the shown card
	// does, covering that case too.
	help := tui.Text(m.statusLine(fmt.Sprintf("%s%s  [%s]  —  esc: back to nav   ^up/^down: prev/next card   ^s: save   ^c: quit", m.path, m.dirtyMark(), card.Kind)),
		m.helpStyle())

	return tui.Box(layout.Vertical,
		tui.Child(layout.Fill(1), textarea),
		tui.Child(layout.Length(1), help),
	).Margin(1)
}

// listEvent is the List widget's onEvent for Nav mode. While searching
// (see search.go), every key means something different — all of
// j/k/g/G/o/O/digits become query text — so that case is split off into
// its own searchKeyEvent entirely, checked first.
func (m *model) listEvent(e input.Event) tui.Msg {
	ke, ok := e.(input.KeyEvent)
	if !ok {
		return nil
	}
	if m.searching {
		return m.searchKeyEvent(ke)
	}
	switch {
	case ke.Key == input.KeyUp || ke.Rune == 'k':
		return navUp
	case ke.Key == input.KeyDown || ke.Rune == 'j':
		return navDown
	case ke.Key == input.KeyEnter:
		return enterEditMsg{}
	case ke.Rune == 'o':
		return insertBelow
	case ke.Rune == 'O':
		return insertAbove
	case ke.Rune == '/':
		return startSearchMsg{}
	case ke.Rune >= '0' && ke.Rune <= '9':
		return navDigitMsg(ke.Rune)
	case ke.Rune == 'g':
		return navG
	case ke.Rune == 'G':
		return navLast
	case ke.Key == input.KeyPgUp:
		return navPageUp
	case ke.Key == input.KeyPgDown:
		return navPageDown
	}
	return nil
}

// cancelPendingNav resets pendingG and pendingCount — see navG's own
// case in Update for why this can't just happen unconditionally at the
// top of Update instead (the M10 double-dispatch bug).
func (m *model) cancelPendingNav() {
	m.pendingG = false
	m.pendingCount = ""
}
