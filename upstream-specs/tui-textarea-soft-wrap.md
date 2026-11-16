# tui: `TextArea` has no soft wrap; clicks ignore the gutter

**Status:** Resolved in `tui` v0.13.0 — new `TextAreaOptions.SoftWrap`,
plus the gutter-click fix below. 9ed bumped to v0.13.0 and turns it on
from `editView` (toggled by `w` / `Ctrl+W`; see the README's "Line
wrapping") and always in `noteView`.

**Repo:** github.com/sandgorgon/tui
**Origin:** 9ed's Edit mode on Markdown files: a paragraph is usually one
long line, and `TextArea` only scrolled it sideways, so it could never be
read at once.

## Problem

1. `TextArea` deliberately scrolled long lines horizontally (one screen
   row per buffer line, to keep cursor math exact) and had no way to opt
   into wrapping.
2. `setCursorFromMouse` never subtracted the `Gutter`'s width, so with
   line numbers on (as 9ed always has in Edit mode) every click landed
   the gutter's width to the right of where it was aimed.

## Resolution (as shipped)

- `SoftWrap bool`, default off. Display only — the buffer and every rune
  offset are unchanged. Word-wraps after a space/tab, mid-word only for a
  word wider than the row; the last column is kept for the end-of-line
  cursor or one hanging space.
- While wrapped, `Up`/`Down`/`PageUp`/`PageDown` move by screen row,
  `Home`/`End` by whole line, and `Gutter` text appears on a line's first
  row only (continuation rows blank, in its style).
- Clicks subtract the gutter's width, wrapped or not.
