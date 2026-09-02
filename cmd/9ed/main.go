// Command 9ed is a segmented/card TUI text editor: `9ed <file>` decomposes
// the file into structurally meaningful cards (see package deck) instead
// of editing it as an undifferentiated block of lines. Nav mode navigates
// the card list; Enter focuses the current card in Edit mode for direct
// text editing, 'n' focuses it in Note mode instead to edit a markdown
// annotation for it (see package notes), Esc returns to Nav from either;
// 'f'/'r' directly toggle the todo/needs-review flags on the current
// card without leaving Nav. Ctrl+S reassembles every card back into the
// file and writes it atomically, alongside the file's .9an sidecar if
// any note or flag changed; 't' toggles the light/dark theme.
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
	"github.com/sandgorgon/9ed/notes"
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
  n            edit current card's note
  f            toggle 'todo' flag on current card
  r            toggle 'needs-review' flag on current card
  o / O        insert card below / above
  t            toggle light/dark theme
  ^s           save
  q, ^c        quit

Edit mode:
  esc          back to Nav
  ^↑ / ^↓      jump to previous / next card, staying in Edit
  ^s           save
  ^c           quit

Note mode (entered with 'n' from Nav):
  esc          back to Nav
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
	// ok=false and readFileNS falls back to the plain os.ReadFile 9ed
	// has always used.
	src, err := readFileNS(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "9ed:", err)
		return 1
	}
	seg := segmenterFor(path)

	writes := make(chan p9WriteMsg)
	m := newModel(path, src, seg, seg.Segment(src), writes)
	// The .9an sidecar is optional and best-effort — no such file (the
	// overwhelmingly common case today, since nothing writes one yet)
	// or any read failure just means no notes for this file, not an
	// error opening it.
	if sidecar, err := readFileNS(notes.SidecarPath(path)); err == nil {
		m.notesFile = notes.Parse(sidecar)
	}
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
// (TextArea's own selection/undo-redo, List's scroll offset) is
// ephemeral widget-retained state, not tracked here — see
// tui/docs/DESIGN.md §3.1 — with one deliberate exception: cursor
// position is mirrored into cursorPos via tui's OnCursorChange,
// specifically so it survives a remount (a cross-card jump) that would
// otherwise silently drop it. Two earlier attempts at this were made
// and reverted after live testing found two distinct tui bugs — see
// jumpCard's own doc comment in insert.go for the full history. Both
// are now fixed (tui v0.3.1 / v0.4.0); re-verified live before
// re-landing this a third time.
type model struct {
	path  string
	src   []byte // original file content, never mutated except by a completed Save
	seg   deck.Segmenter
	cards []deck.Card

	cursor      int
	editing     bool
	noteEditing bool // see noteView; mutually exclusive with editing

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

	// activeSearch is the last *committed* search pattern (m.query at the
	// moment Enter confirmed it), kept around after m.searching goes
	// false so Ctrl+N/Ctrl+P (see jumpToMatch) keep working while editing
	// — matching Vim's "n/N reuse the last search" convention. Left
	// untouched by Esc-cancelling an in-progress retype; only a new
	// committed search replaces it.
	activeSearch string

	// jumpGen forces editView's TextArea to remount on a same-card cursor
	// jump (search's Ctrl+N/Ctrl+P landing on another match in the card
	// already open) — see setJumpTarget's doc comment for why a plain
	// InitialCursor change isn't enough on its own.
	jumpGen int

	// gotoLineCursor/gotoLineCard (see goto.go's goToLine and search.go's
	// setJumpTarget) are the rune offset within a card's body the cursor
	// should open at, and which card that applies to — editView only
	// honors gotoLineCursor when gotoLineCard == cursor, so every other
	// edit-mode-entry/transition path (enterEditMsg, insertMsg, jumpCard,
	// Esc) clears gotoLineCursor to nil, or a later plain Enter reopening
	// the same card would incorrectly reapply a stale line target instead
	// of the normal default cursor position.
	gotoLineCursor *int
	gotoLineCard   int

	// cursorPos remembers each card's last-known cursor position (a
	// rune offset into its body, the same unit InitialCursor/
	// OnCursorChange both use), captured continuously via editView's
	// TextArea OnCursorChange — so leaving a card via jumpCard (or Esc
	// to Nav and back via Enter) and returning to it restores where you
	// were, instead of always landing at the default start position.
	// Keyed by card index, same convention and same insertCard/
	// removeCard-driven reindexing as m.edited (see shiftKeysForInsert/
	// shiftKeysForRemove) — deck.Card has no stabler identity within
	// one unsaved session. Cleared wholesale on a successful Save,
	// since indices are meaningless once the deck resegments; unlike
	// gotoLineCursor, never cleared by an individual mode transition —
	// this is meant to persist across exactly those.
	cursorPos map[int]int

	// edited holds a card's current text once it diverges from its
	// original src[Span[0]:Span[1]] slice — sparse, since most cards in
	// a session are never touched. Keyed by index into cards. Cleared
	// on a successful Save, since src/cards are resynced to it then.
	edited map[int]string

	saveErr string // last save's error, if any; cleared by the next successful save

	// notesFile holds this file's .9an sidecar (see package notes) —
	// always non-nil, empty when no sidecar exists yet. Loaded once at
	// startup (see run()); noteView's OnChange (see noteChangedMsg)
	// mutates it live, the same way editView's OnChange keeps m.edited
	// current — neither hits disk until Save.
	notesFile *notes.Sidecar

	// noteEdited marks which cards (by cursor index, same convention as
	// m.edited) have had their .9an entry touched this session — a note
	// edit or a flag toggle, either one — separate from m.edited because
	// neither affects reassemble/src at all, only whether Save needs to
	// rewrite the sidecar. Feeds the same dirty indicator m.edited does
	// (see isDirty), and is cleared alongside it on a successful Save.
	noteEdited map[int]bool

	// refs is deck.References(src, cards): for card i, the indices of
	// other cards whose body mentions cards[i].Name. Recomputed
	// whenever src/cards actually change (construction, a successful
	// Save) — never on every keystroke/insert, matching References'
	// own "load/save time, not live" design. Can run shorter than
	// cards immediately after an insertCard (which grows cards without
	// touching src) — every reader must bounds-check against len(refs)
	// rather than assume it tracks cards 1:1 at all times.
	refs [][]int

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
	return &model{
		path: path, src: src, seg: seg, cards: cards, theme: theme,
		view: &bufferView{}, writes: writes,
		notesFile: notes.New(),
		refs:      deck.References(src, cards),
	}
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

// isDirty reports whether card i has an unsaved change — either its
// body (m.edited) or its note (m.noteEdited). Both feed the same
// indicator: from the status line's point of view, either one means
// Ctrl+S has something to write.
func (m *model) isDirty(i int) bool {
	_, bodyDirty := m.edited[i]
	return bodyDirty || m.noteEdited[i]
}

// dirtyMark is the whole-deck "unsaved" indicator for the status line —
// separate from, and in addition to, the per-card dirty markers in
// navView, which show *which* cards changed rather than just whether
// anything did.
func (m *model) dirtyMark() string {
	if len(m.edited) > 0 || len(m.noteEdited) > 0 {
		return " [unsaved]"
	}
	return ""
}

// finalizeNoteEdit drops the current card's sidecar entry entirely if
// editing left its note empty AND it has no flag set either — rather
// than persisting a blank "# kind: title" header with nothing under it
// — mirroring abandonEmptyInsert's "don't keep an empty placeholder"
// choice for a card inserted and then left untouched. Checking Annotated
// rather than just an empty body matters once flags exist: a
// flagged-but-note-less card must survive leaving Note mode with an
// empty note, not have its flag silently wiped along with the blank
// body.
func (m *model) finalizeNoteEdit() {
	card := m.cards[m.cursor]
	if !m.notesFile.Annotated(card.Kind, card.Title) {
		m.notesFile.Delete(card.Kind, card.Title)
	}
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

// cursorMovedMsg is produced by editView's TextArea OnCursorChange
// whenever the cursor moves for any reason — a navigation key, a mouse
// click, or an edit that relocates it, not just on content change like
// editChangedMsg — so m.cursorPos can track it continuously, not just
// at the moment of leaving a card.
type cursorMovedMsg struct{ offset int }

// editNoteMsg is produced by listEvent on 'n' in Nav mode — the
// note-editing counterpart to enterEditMsg. noteChangedMsg is produced
// by noteView's TextArea OnChange, the counterpart to editChangedMsg.
type editNoteMsg struct{}
type noteChangedMsg struct{ value string }

// toggleFlagMsg is produced by listEvent on 'f'/'r' in Nav mode —
// toggling a user-authored badge (see flagTodo/flagNeedsReview) is a
// direct one-key action, not a mode to enter, unlike notes.
type toggleFlagMsg struct{ flag string }

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

	case cursorMovedMsg:
		if m.cursorPos == nil {
			m.cursorPos = make(map[int]int)
		}
		m.cursorPos[m.cursor] = v.offset

	case editNoteMsg:
		m.cancelPendingNav()
		m.gotoLineCursor = nil
		// A freshly inserted, not-yet-saved placeholder (see insert.go)
		// has no real (Kind, Title) yet — Save's resegmentation is what
		// gives it one, so a note attached now would key against a
		// value about to change and immediately orphan itself.
		if len(m.cards) > 0 && m.cards[m.cursor].Kind != newCardKind {
			m.noteEditing = true
		}

	case noteChangedMsg:
		card := m.cards[m.cursor]
		m.notesFile.Set(card.Kind, card.Title, v.value)
		if m.noteEdited == nil {
			m.noteEdited = make(map[int]bool)
		}
		m.noteEdited[m.cursor] = true

	case toggleFlagMsg:
		// Same orphaning risk as editNoteMsg's guard: a not-yet-saved
		// placeholder's (Kind, Title) is about to change on the next
		// Save.
		if len(m.cards) > 0 && m.cards[m.cursor].Kind != newCardKind {
			card := m.cards[m.cursor]
			m.notesFile.ToggleFlag(card.Kind, card.Title, v.flag)
			if m.noteEdited == nil {
				m.noteEdited = make(map[int]bool)
			}
			m.noteEdited[m.cursor] = true
		}

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
		if len(m.filteredIndices()) == 0 {
			break
		}
		m.editing = true
		m.activeSearch = m.query
		m.gotoLineCursor = nil // see setJumpTarget's fallback below
		if re, ok := searchRegexp(m.query); ok {
			if matches := bodyMatches(re, m.cardBody(m.cursor)); len(matches) > 0 {
				// A body match exists: land the cursor on it rather than
				// the TextArea's own default (end of Value) — e.g. a
				// title-only match (no body hit) falls through with
				// gotoLineCursor left nil above, same as before this
				// feature existed.
				m.setJumpTarget(m.cursor, matches[0][0])
			}
		}

	case saveDoneMsg:
		if v.err != nil {
			m.saveErr = v.err.Error()
			break
		}
		m.saveErr = ""
		m.src, m.cards, m.edited, m.noteEdited, m.cursorPos = v.src, v.cards, nil, nil, nil
		m.refs = deck.References(m.src, m.cards)
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
		if m.noteEditing {
			// Esc is the only way out of note-editing (entry is
			// Nav-only, so there's exactly one place to return to,
			// unlike Edit mode's cross-card jump — nothing else needed
			// here). Every other key must reach noteView's TextArea as
			// a literal character, same as m.editing below.
			if v.Key == input.KeyEsc && v.Mod == 0 {
				m.finalizeNoteEdit()
				m.noteEditing = false
				return m, nil
			}
			return m, nil
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
			// own key claims as of tui v0.4.0 (widget/textarea.go's
			// handleKey has an explicit no-op case for Ctrl+Up/Down,
			// fixing a real bug in earlier tui versions where they fell
			// through to plain Up/Down and moved the widget's own
			// cursor too — see jumpCard's own doc comment for the full
			// history), so, like plain Esc, they reach here unclaimed.
			if v.Mod&input.ModCtrl != 0 && v.Key == input.KeyDown {
				m.jumpCard(1)
				return m, nil
			}
			if v.Mod&input.ModCtrl != 0 && v.Key == input.KeyUp {
				m.jumpCard(-1)
				return m, nil
			}
			// Ctrl+N/Ctrl+P jump to the next/previous occurrence of the
			// active search (see search.go's jumpToMatch) — distinct from
			// Ctrl+Up/Down's cross-card jump above, so the two don't
			// collide: this can move within the same card, that never
			// does.
			if v.Mod&input.ModCtrl != 0 && v.Rune == 'n' {
				m.jumpToMatch(1)
				return m, nil
			}
			if v.Mod&input.ModCtrl != 0 && v.Rune == 'p' {
				m.jumpToMatch(-1)
				return m, nil
			}
			return m, nil // never fall through to the 'q' check below:
			// 'q' must be an ordinary character while editing text.
		}
		if m.searching {
			// Every raw KeyEvent is dispatched to Update *and* delivered
			// to the focused List (tui always does both — see app.go's
			// handleInput); the List's own onEvent (listEvent/
			// searchKeyEvent) already turns query characters into
			// searchInputMsg via that second path. Without this guard, a
			// query letter that happens to match one of the checks below
			// fires it too: confirmed live in tmux that searching for a
			// pattern containing 'q' quit the whole app, and one
			// containing 't' silently flipped the theme, mid-keystroke.
			return m, nil
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
	switch {
	case m.noteEditing:
		return m.noteView()
	case m.editing:
		return m.editView()
	default:
		return m.navView()
	}
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
		if m.isDirty(i) {
			mark = dirtyGlyph // unsaved body or note change
		}
		titles[pos] = fmt.Sprintf("%s %-9s %s%s", mark, c.Kind, c.Title, m.cardBadges(i))
		if i == m.cursor {
			cursorInList = pos
		}
	}

	list := widget.List(titles, cursorInList, widget.ListOptions{Theme: m.theme}, m.listEvent)

	var help tui.Node
	if m.searching {
		help = tui.Text(fmt.Sprintf("/%s  (%d match%s)  —  enter: go   esc: cancel", m.query, len(indices), pluralS(len(indices))), m.helpStyle())
	} else {
		help = tui.Text(m.statusLine(fmt.Sprintf("%s%s  (%d cards)  —  j/k: move   gg/G: first/last   PgUp/PgDn: page   {n}G: goto line   /: filter   enter: edit   n: note   f/r: flag   o/O: insert   ^s: save   t: theme   q: quit",
			m.path, m.dirtyMark(), len(m.cards))), m.helpStyle())
	}

	return tui.Box(layout.Vertical,
		tui.Child(layout.Fill(1), list),
		tui.Child(layout.Length(1), help),
	).Margin(1)
}

// Glyphs for Nav mode's list line — single-width in any monospace
// terminal font (tui's Painter already handles a genuinely wide rune
// correctly, per its own wcwidth-aware Text/SetCell, but these are
// picked to stay compact and universally renderable regardless).
const (
	dirtyGlyph = "●" // unsaved body or note change (replaces the old plain '*')
	noteGlyph  = "✎" // this card has a .9an note attached
	refGlyph   = "↩" // followed by a count: N other cards mention this one's Name

	todoGlyph        = "⚑" // this card is flagged flagTodo
	needsReviewGlyph = "⚠" // this card is flagged flagNeedsReview
)

// flagTodo and flagNeedsReview are the only user-authored badges 9ed
// knows about — a small, fixed vocabulary (package notes itself has no
// opinion on flag names; these are cmd/9ed's own choice). Toggled from
// Nav mode with 'f'/'r' (see listEvent), same guard against an unsaved
// insert placeholder that editNoteMsg uses, for the same orphaning
// reason.
const (
	flagTodo        = "todo"
	flagNeedsReview = "needs-review"
)

// cardBadges returns a compact, space-prefixed suffix noting card i's
// annotations for Nav mode's list line — the user-authored flags (see
// flagTodo/flagNeedsReview), then whether it has a note, then how many
// other cards mention it (see deck.References) — or "" when none apply.
// Purely informational: nothing here affects navigation or editing.
// Flags are listed first since they're a deliberate signal the user set
// ("pay attention to this"), ahead of the two passive, system-computed
// ones.
func (m *model) cardBadges(i int) string {
	c := m.cards[i]
	badges := ""
	if m.notesFile.HasFlag(c.Kind, c.Title, flagTodo) {
		badges += "  " + todoGlyph
	}
	if m.notesFile.HasFlag(c.Kind, c.Title, flagNeedsReview) {
		badges += "  " + needsReviewGlyph
	}
	// Get's ok reports "an entry exists," which is now true for a
	// flags-only, body-less entry too (see ToggleFlag) — the note glyph
	// specifically needs a real, non-empty body.
	if body, ok := m.notesFile.Get(c.Kind, c.Title); ok && body != "" {
		badges += "  " + noteGlyph
	}
	if i < len(m.refs) && len(m.refs[i]) > 0 {
		badges += fmt.Sprintf("  %s %d", refGlyph, len(m.refs[i]))
	}
	return badges
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

// editKey is editView's TextArea Key — see the .Key call's own comment
// for why both fields are needed.
type editKey struct {
	span [2]int
	gen  int
}

func (m *model) editView() tui.Node {
	theme := m.theme
	card := m.cards[m.cursor]
	body := m.cardBody(m.cursor)

	var highlights []widget.StyleSpan
	if card.Kind != "" && filepath.Ext(m.path) == ".go" {
		highlights = goHighlights(body, theme)
	}
	if m.activeSearch != "" {
		if re, ok := searchRegexp(m.activeSearch); ok {
			highlights = mergeHighlights(highlights, searchHighlights(re, body, theme))
		}
	}

	// gotoLineCursor (an explicit {n}G target — see goto.go's goToLine)
	// takes priority over a remembered cursorPos when both apply to
	// this card: a deliberate, specific request beats "restore where I
	// happened to be." Every other edit-mode-entry path clears
	// gotoLineCursor, but the card-match check here is the actual
	// guard. Otherwise, cursorPos restores the last-known position for
	// a card being revisited (jumpCard, or Esc-to-Nav-and-back)
	// instead of always defaulting to the start.
	var initialCursor *int
	switch {
	case m.gotoLineCursor != nil && m.gotoLineCard == m.cursor:
		initialCursor = m.gotoLineCursor
	default:
		if pos, ok := m.cursorPos[m.cursor]; ok {
			initialCursor = &pos
		}
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
		Theme:          theme,
		Value:          body,
		Highlights:     highlights,
		InitialCursor:  initialCursor,
		Gutter:         gutter,
		OnChange:       func(v string) tui.Msg { return editChangedMsg{value: v} },
		OnCursorChange: func(offset int) tui.Msg { return cursorMovedMsg{offset: offset} },
		// A ReleaseKey distinct from plain Esc — see the input.KeyEvent
		// case in Update for why plain Esc must NOT be this widget's
		// configured release key.
		ReleaseKey: input.KeyEvent{Key: input.KeyEsc, Mod: input.ModCtrl},
	}).Key(editKey{card.Span, m.jumpGen})
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
	// does, covering that case too. jumpGen is folded in on top of Span
	// for the one case Span alone can't distinguish: search's Ctrl+N/
	// Ctrl+P landing on a second match *within* the same card (see
	// setJumpTarget) — Span is unchanged there, so without jumpGen the
	// existing widget instance would be reused and never see the new
	// InitialCursor at all.
	help := tui.Text(m.statusLine(fmt.Sprintf("%s%s  [%s]  —  esc: back to nav   ^up/^down: prev/next card   ^s: save   ^c: quit", m.path, m.dirtyMark(), card.Kind)),
		m.helpStyle())

	return tui.Box(layout.Vertical,
		tui.Child(layout.Fill(1), textarea),
		tui.Child(layout.Length(1), help),
	).Margin(1)
}

// noteView renders the current card's .9an note full-screen, in place
// of its source body — the note-editing counterpart to editView, reusing
// the same TextArea widget and mode-swap shape rather than a new UI
// paradigm: 9ed already treats "leave Nav, edit one thing full-screen,
// Esc to return" as the one way to focus on something, and a note is no
// exception. No syntax highlighting or line-number gutter — both are
// about correlating with the source file's own structure, which a note,
// being about the card rather than part of it, has no need of.
func (m *model) noteView() tui.Node {
	theme := m.theme
	card := m.cards[m.cursor]
	body, _ := m.notesFile.Get(card.Kind, card.Title)

	textarea := widget.TextArea(widget.TextAreaOptions{
		Theme:    theme,
		Value:    body,
		OnChange: func(v string) tui.Msg { return noteChangedMsg{value: v} },
		// Same reasoning as editView's TextArea: must not be plain Esc,
		// or the m.noteEditing case in Update never sees it.
		ReleaseKey: input.KeyEvent{Key: input.KeyEsc, Mod: input.ModCtrl},
	}).Key(card.Span)

	help := tui.Text(m.statusLine(fmt.Sprintf("Note for: %s%s  —  esc: back   ^s: save", card.Title, m.dirtyMark())), m.helpStyle())

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
	case ke.Rune == 'n':
		return editNoteMsg{}
	case ke.Rune == 'f':
		return toggleFlagMsg{flag: flagTodo}
	case ke.Rune == 'r':
		return toggleFlagMsg{flag: flagNeedsReview}
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
