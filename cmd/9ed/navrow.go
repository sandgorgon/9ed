// Nav mode's list rows are one line per card by design — j/k, PgUp/
// PgDn, mouse click→card mapping, and tui's row-granular widget.List
// all assume it — so a row that's wider than the terminal is never
// wrapped. Left alone, though, tui just clips it at the right edge,
// which drops the badges (flags, note, reference count) first since
// they come last. fitNavRow instead shrinks only the title, with an
// ellipsis, so the badges always stay on screen; navView then shows the
// highlighted card's full title on the status line whenever its row
// was shortened (see navStatusText).
//
// The model learns the terminal width itself (see resizeMsg): tui's
// App.Resize repaints at the new size but never tells Update about it,
// and a View has to build List's row strings before it's painted.
package main

import (
	"os"

	"github.com/sandgorgon/tui/cell"
	"github.com/sandgorgon/tui/term"
	"github.com/sandgorgon/tui/tui"
)

const ellipsis = "…"

// navListChrome is how many columns of the terminal width a Nav row
// can't use: navView's Box Margin(1) on each side, plus List's own
// two-column "> "/". " cursor marker.
const navListChrome = 2 + 2

// resizeMsg carries the terminal's new column count after a SIGWINCH.
type resizeMsg struct{ cols int }

// waitForResize turns one resize notification into a resizeMsg — the
// same block-on-a-channel Cmd shape as waitForP9Write/waitForP9Goto, and
// the same nil-channel behavior (blocks forever) for a model built
// without one, e.g. in tests, which then simply never truncate.
func waitForResize(resizes <-chan struct{}) tui.Cmd {
	return func() tui.Msg {
		if _, ok := <-resizes; !ok {
			return nil
		}
		return resizeMsg{cols: terminalCols()}
	}
}

// terminalCols reports stdout's current width, or 0 (meaning "unknown,
// don't truncate") if it can't be determined.
func terminalCols() int {
	size, err := term.GetSize(os.Stdout)
	if err != nil {
		return 0
	}
	return size.Cols
}

// displayWidth measures s in terminal columns using tui's own
// (internal) wcwidth table, by way of Painter.Text's return value, so
// wide runes are counted exactly as widget.List will paint them. The
// 1x1 scratch buffer is irrelevant: Text reports the columns it
// advanced regardless of clipping.
func displayWidth(s string) int {
	return cell.NewPainter(cell.NewBuffer(1, 1)).Text(0, 0, s, cell.Style{})
}

// fitNavRow joins prefix+title+badges, shortening only title (with a
// trailing ellipsis) so the whole row fits in width columns. It reports
// whether title was shortened. width <= 0 means unknown: no fitting.
// If even prefix+badges alone don't fit there's no sensible shortening,
// so the row is returned whole and tui's clip takes over — a terminal
// that narrow has bigger problems.
func fitNavRow(prefix, title, badges string, width int) (row string, truncated bool) {
	full := prefix + title + badges
	if width <= 0 || displayWidth(full) <= width {
		return full, false
	}
	room := width - displayWidth(prefix) - displayWidth(badges) - displayWidth(ellipsis)
	if room < 0 {
		return full, false
	}
	used := 0
	cut := 0
	for i, r := range title {
		w := displayWidth(string(r))
		if used+w > room {
			break
		}
		used += w
		cut = i + len(string(r))
	}
	return prefix + title[:cut] + ellipsis + badges, true
}
