// Cut/copy/paste: a single internal register (model.register) shared
// by two granularities — Nav mode's whole-card 'y'/'x'/'p'/'P' and
// Edit mode's text-selection Ctrl+C/Ctrl+X/Ctrl+V — so copying a card
// then pasting a selection (or the reverse) round-trips through the
// same slot, the way vim's default register serves both yank/delete
// and put regardless of what kind of motion produced it.
//
// Keybinding choice: Ctrl+C was 9ed's global "quit" (see Update's raw
// KeyEvent case) until this feature — the whole reason it exists is a
// user reporting they'd lost Ctrl+C-to-copy to it. Rather than give
// Edit mode's copy some other, non-standard key, quit moved to Ctrl+Q
// instead (confirmed free: no terminal-driver meaning survives it,
// since Ctrl+S already works as "save" rather than XOFF, meaning
// software flow control is already off and Ctrl+Q carries no leftover
// XON meaning either; and TextArea's own handleKey has no case for
// 'q' at all). That frees the actual standard trio — Ctrl+C copy,
// Ctrl+X cut, Ctrl+V paste — confirmed unclaimed end to end before
// using them, not assumed: handleKey has no case for 'c' or 'x' with
// Ctrl held (only 'z' undo and 'y' redo touch Ctrl+letter), and 'v'
// with Ctrl held falls through to the plain literal-insert case only
// when Mod excludes Ctrl — which it doesn't here — so none of the
// three ever reaches TextArea as a stray inserted character or an
// unrelated action.
//
// Register-to-system-clipboard: copies/cuts also best-effort mirror
// the register to the OS clipboard via OSC 52 (setRegister) — write
// only. OSC 52's read direction is disabled by default in most
// terminals (an arbitrary program snooping the clipboard is a real
// security concern), so paste always comes back from the internal
// register, never a clipboard read — reliable in every terminal, at
// the cost of not seeing something another application put on the
// clipboard.
//
// Known limitation, not yet fixable locally: Edit mode's selection
// cut/paste (cutSelection/pasteAtCursor below) can't apply their edit
// to the live, mounted TextArea the way a real keystroke would —
// TextArea's Value is read once at mount (see its own doc comment),
// and tui.App.Dispatch (what any Cmd/Msg a Model.Update returns
// ultimately drives) only ever calls Model.Update again; it never
// forwards to the focused widget's HandleEvent the way a real
// terminal-sourced input.Event does via HandleInput. So there is no
// way for 9ed's own code to push text into an already-mounted
// TextArea while going through its internal editBuffer.insertString/
// removeSelectionNoUndo — the same calls a real keystroke uses, which
// DO record proper undo/redo. Instead, both functions rewrite
// m.edited directly and force a fresh mount (bumping m.jumpGen, the
// same trick search's Ctrl+N/Ctrl+P already uses for a same-card
// jump — see editView's Key comment), which means: a fresh TextArea
// instance, with an EMPTY undo/redo history — Ctrl+Z cannot undo the
// cut/paste itself, or anything typed in that card before it, once
// the remount happens. Nav mode's 'u' (revertCard) still recovers
// from this — it discards the card's entire m.edited entry back to
// session start, cut/paste included — just not as a single
// fine-grained undo step. Confirmed by reading tui/app.go's
// Dispatch/HandleInput directly, the same discipline tui#15/#18/#20/
// #42 were each run down with: this is a real capability gap in tui
// (no way for a host Model to apply a widget-internal, undo-tracked
// edit to a live-focused widget from outside a real keystroke), not
// a workaround waiting to be found — worth its own upstream spec if
// finer-grained undo across these commands ever becomes worth having.
package main

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/sandgorgon/tui/input"
)

// setRegister stores text in 9ed's own register and best-effort
// mirrors it to the system clipboard via OSC 52 — see this file's own
// doc comment for why paste never reads it back.
func (m *model) setRegister(text string) {
	m.register = text
	fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(text)))
}

// clearEditSelection drops any tracked selection — called whenever
// Edit mode (re)starts on a (possibly different) card. Necessary
// because a freshly mounted TextArea always begins with no selection
// of its own, but OnSelectionChange only fires on a *change*: without
// this, a selection left over from the previous card (or a previous
// visit to this one) would keep reading as active until the user made
// a new selection to overwrite it.
func (m *model) clearEditSelection() {
	m.hasSel = false
	m.selStart, m.selEnd = 0, 0
}

// clampRange orders and clamps a raw (a,b) pair — selStart/selEnd
// arrive already ordered from OnSelectionChange, but re-ordering
// defensively here costs nothing and keeps every caller from having
// to think about it — into a valid [start,end] within [0,n].
func clampRange(a, b, n int) (start, end int) {
	if a > b {
		a, b = b, a
	}
	return max(0, min(a, n)), max(0, min(b, n))
}

// setCursorPos records card i's cursor at offset pos, the same
// lazy-init convention setEdited already uses for m.edited.
func (m *model) setCursorPos(i, pos int) {
	if m.cursorPos == nil {
		m.cursorPos = make(map[int]int)
	}
	m.cursorPos[i] = pos
}

// copySelection copies the current card's active selection, if any,
// into the register. A no-op with nothing selected — matches every
// mainstream editor's "copy with no selection does nothing", leaving
// "copy the whole card" to Nav mode's 'y'.
func (m *model) copySelection() {
	if !m.hasSel {
		return
	}
	body := []rune(m.cardBody(m.cursor))
	start, end := clampRange(m.selStart, m.selEnd, len(body))
	if start >= end {
		return
	}
	m.setRegister(string(body[start:end]))
}

// cutSelection is copySelection plus removing the selected range from
// the card's body — see this file's doc comment for the remount/
// undo-history trade-off this requires.
func (m *model) cutSelection() {
	if !m.hasSel {
		return
	}
	body := []rune(m.cardBody(m.cursor))
	start, end := clampRange(m.selStart, m.selEnd, len(body))
	if start >= end {
		return
	}
	m.setRegister(string(body[start:end]))
	m.setEdited(m.cursor, string(body[:start])+string(body[end:]))
	m.setCursorPos(m.cursor, start)
	m.clearEditSelection()
	m.jumpGen++
	m.view.publish(m.path, m.src, m.cards, m.edited)
}

// pasteAtCursor inserts the register's content at the current cursor
// position, replacing the active selection first if there is one —
// mirroring editBuffer.insertString's own "typing over a selection
// replaces it" convention. A no-op with an empty register, matching
// vim's "put" from an empty register. Same remount trade-off as
// cutSelection.
func (m *model) pasteAtCursor() {
	if m.register == "" {
		return
	}
	body := []rune(m.cardBody(m.cursor))
	start, end := m.cursorPos[m.cursor], m.cursorPos[m.cursor]
	if m.hasSel {
		start, end = clampRange(m.selStart, m.selEnd, len(body))
	}
	start = max(0, min(start, len(body)))
	end = max(0, min(end, len(body)))
	m.setEdited(m.cursor, string(body[:start])+m.register+string(body[end:]))
	m.setCursorPos(m.cursor, start+len([]rune(m.register)))
	m.clearEditSelection()
	m.jumpGen++
	m.view.publish(m.path, m.src, m.cards, m.edited)
}

// editClipboardKeyEvent handles Edit mode's Ctrl+C/Ctrl+X/Ctrl+V — see
// this file's doc comment for why Ctrl+C is available here at all
// (quit moved to Ctrl+Q). Called from Update's raw KeyEvent case,
// inside the m.editing branch; reports whether it claimed the key.
func (m *model) editClipboardKeyEvent(ke input.KeyEvent) bool {
	if ke.Mod != input.ModCtrl {
		return false
	}
	switch ke.Rune {
	case 'c':
		m.copySelection()
	case 'x':
		m.cutSelection()
	case 'v':
		m.pasteAtCursor()
	default:
		return false
	}
	return true
}

// copyCardMsg/cutCardMsg are listEvent's ('y'/'x') Nav-mode
// counterparts to copySelection/cutSelection — whole-card granularity
// instead of a text selection, sharing the same register.
type copyCardMsg struct{}
type cutCardMsg struct{}

// pasteCardMsg is listEvent's ('p'/'P') Nav-mode counterpart to
// pasteAtCursor — mirrors insertMsg's insertBelow/insertAbove shape
// exactly, since pasting a card is "insertCard, pre-filled" (see
// Update's handling).
type pasteCardMsg int

const (
	pasteBelow pasteCardMsg = iota // 'p': after the cursor, mirrors insertBelow
	pasteAbove                     // 'P': before the cursor, mirrors insertAbove
)

// selectionChangedMsg is produced by editView's TextArea
// OnSelectionChange (tui v0.9.0, github.com/sandgorgon/tui/issues/42)
// whenever the active selection's range changes.
type selectionChangedMsg struct {
	start, end int
	ok         bool
}
