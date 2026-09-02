# tui: TextArea needs a way to read back the live cursor position

**Status:** Filed, not yet resolved.

**Issue:** https://github.com/sandgorgon/tui/issues/15

**Repo:** github.com/sandgorgon/tui
**Origin:** surfaced designing 9ed's "restore cursor position across a
cross-card jump" feature (a segmented/card editor built on `tui`) —
needs a capability `tui` doesn't currently expose, the read-side
counterpart of the gap `tui-cursor-offset-and-numeric-gutter.md`
already closed for the write side.

## Problem: `TextArea`'s cursor position is entirely opaque to its caller

`widget.TextAreaOptions` (`widget/textarea.go`) has `InitialCursor
*int` (added in `tui` v0.2.0, resolving
`tui-cursor-offset-and-numeric-gutter.md`) — a *write-only* option,
consulted once at mount, in `Reconcile`'s `if !w.mounted` branch. There
is no way to go the other direction: `textAreaWidget.cursor` is a
private field, never surfaced through `TextAreaOptions`, a callback, or
any exported method. Confirmed reading the whole widget file: no
`OnCursorChange`-shaped option exists anywhere in `TextAreaOptions`,
and nothing in `textAreaWidget`'s exported surface (`Focusable`,
`SetFocused`, `WantsRawTab`, `ReleaseKey`, `HandleEvent`, `Paint`)
exposes it either.

9ed hits this trying to make a cross-card jump (`Ctrl+↑`/`Ctrl+↓` in
Edit mode, `cmd/9ed/insert.go`'s `jumpCard`) remember where the cursor
was in the card being left, so jumping back later restores it instead
of always landing at the card's default start position. `jumpCard`
itself is a pure `model.cursor` (card index) mutation with no I/O — the
missing piece is entirely on the `tui` side: by the time 9ed's `Update`
runs to handle the jump, the `TextArea` being left is about to be
unmounted (`cmd/9ed/main.go`'s `editView` keys the widget by the card's
own `Span`, so a different card at that tree position always remounts
fresh — see that file's own comment on why), and there is no signal
9ed could have captured beforehand telling it where the cursor was.

This is the same category of gap `InitialCursor` closed for placing a
cursor, just facing the other way: `tui` lets a caller *set* an initial
position but never lets it *read* the current one back, whether to
persist it across a remount (9ed's case), to implement a "jump to
matching bracket" style feature relative to the current position, or
simply to show a caller-rendered "line N, col M" status indicator
outside the widget itself (9ed's own status line currently can't do
this at all).

## Proposed

Add a way for a caller to observe `TextArea`'s live cursor position, in
whichever shape fits `tui`'s existing patterns best — two natural
options, not mutually exclusive:

1. **A change callback**, mirroring the existing `OnChange func(value
   string) tui.Msg`: `OnCursorChange func(offset int) tui.Msg`, called
   whenever the cursor moves (a keystroke, a mouse click, any of
   `handleKey`'s movement branches) — the same "the widget tells the
   app, the app decides what to do with it" shape `OnChange` already
   establishes, letting a caller update its own state (`model.cursor`
   position, a status-line indicator) without polling.
2. **An exported query method** on whatever `tui.Node`/handle
   `TextArea(...)` returns, e.g. `CursorOffset() int` — useful for a
   caller that only wants the position at a specific moment (like
   9ed's jump-away case) rather than a running subscription to every
   movement.

Either unblocks 9ed's specific need; a callback additionally unblocks a
live position indicator, which an exported method alone wouldn't (that
still needs *something* to trigger a re-render/re-query on every
movement).

## Why this is general-purpose, not 9ed-specific

Any `tui`-based editor that remounts or replaces a `TextArea` instance
across some navigation event (switching buffers, tabs, cross-reference
jumps) hits this same gap the moment it wants continuity across that
transition — `InitialCursor` alone can restore a position once you
already know what it was, but nothing in `TextArea`'s current API can
ever tell a caller what a position *was* in the first place. A visible
"line N, col M" status indicator — closer to a baseline expectation for
a code-editing widget than an unusual ask — hits the identical gap.
