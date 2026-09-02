# tui: TextArea's handleKey treats Ctrl+Up/Down as plain Up/Down, unlike every other Ctrl-modified movement key

**Status:** Resolved in `tui` v0.4.0 — `Ctrl+Up`/`Ctrl+Down` (and,
extended to the same gap, `Ctrl+PgUp`/`Ctrl+PgDown`) are now an
explicit no-op case in `handleKey`, checked before the plain
movement case, so they're genuinely left unclaimed rather than
falling through. Adopted in 9ed and re-verified live: a fresh
baseline-vs-jump byte comparison came back identical — this was the
last of the two gaps blocking cross-card cursor restoration, and the
feature is now shipped (see `cmd/9ed/insert.go`'s `jumpCard`).

**Issue:** https://github.com/sandgorgon/tui/issues/20

**Repo:** github.com/sandgorgon/tui
**Origin:** surfaced re-attempting 9ed's "restore cursor position across a cross-card jump" feature after tui v0.3.1 fixed tui#18's ordering race (`OnCursorChange` now resolves synchronously). The race was genuinely gone, confirmed by re-running the same instrumented reproduction — but a second, independent bug immediately surfaced right behind it, in the exact same feature.

## Problem: `Up`/`Down` have no `ctrl`-guarded case, unlike `Left`/`Right`/`Home`/`End`

`widget/textarea.go`'s `handleKey`:

```go
func (w *textAreaWidget) handleKey(ke input.KeyEvent) bool {
	shift := ke.Mod&input.ModShift != 0
	ctrl := ke.Mod&input.ModCtrl != 0
	switch {
	case ctrl && ke.Key == input.KeyLeft:
		w.moveTo(wordBoundary(w.buf, w.cursor, -1), shift)
	case ctrl && ke.Key == input.KeyRight:
		w.moveTo(wordBoundary(w.buf, w.cursor, 1), shift)
	case ctrl && ke.Key == input.KeyHome:
		w.moveTo(0, shift)
	case ctrl && ke.Key == input.KeyEnd:
		w.moveTo(len(w.buf), shift)

	case ke.Key == input.KeyLeft:
		w.moveHorizontal(-1, shift)
	case ke.Key == input.KeyRight:
		w.moveHorizontal(1, shift)
	case ke.Key == input.KeyUp:
		w.moveVertical(-1, shift)
	case ke.Key == input.KeyDown:
		w.moveVertical(1, shift)
	...
```

Every other directional key gets an explicit `ctrl &&`-guarded variant with its own semantics (word-boundary jump for Left/Right, buffer start/end for Home/End), checked *before* the plain variant. `Up`/`Down` have no such case at all — so `ctrl && ke.Key == input.KeyUp` falls straight through to the generic `case ke.Key == input.KeyUp:` and is treated as plain `Up`, moving the widget's own cursor. Same for `Down`.

## Why this matters: it's not just an unclaimed key, it actively corrupts caller state

A host app might reasonably expect (9ed's own `cmd/9ed/main.go` did, in a comment that turned out to be wrong) that an unhandled modifier combination is simply *ignored* by the widget — since `App.HandleInput` always delivers a raw key to **both** the top-level `Update` *and* the currently-focused widget's `HandleEvent` (that's how host-level global keybindings like this coexist with widget-level ones at all), a host using `Ctrl+Up`/`Ctrl+Down` for its own purpose relies on the widget not *also* reacting to it.

9ed's cross-card jump (`Ctrl+↑`/`Ctrl+↓`) is exactly this shape: the keystroke is fully handled at the app level (switching which card is focused, remounting a different `TextArea`), and the widget silently treating the same keystroke as `Up`/`Down` too was harmless *only* because nothing previously read the consequence back out. Once `OnCursorChange` (tui#15/v0.3.0, race fixed in tui#18/v0.3.1) makes cursor position an observable, trackable value, this stops being harmless: the *destination* card's freshly-mounted widget — already correctly initialized via `InitialCursor` to a restored position — immediately consumes the very same `Ctrl+Up`/`Ctrl+Down` keystroke that triggered the jump as an extra vertical move, silently overwriting the just-restored position by exactly one line before the user ever sees it.

Confirmed directly, not assumed: instrumented both `jumpCard` and the `OnCursorChange`-driven `Update` case, and captured the message sequence for `Up`×3 → `Ctrl+↓` → `Ctrl+↑`:

```
DEBUG cursorMovedMsg offset=88 recorded-under-cursor=1
DEBUG cursorMovedMsg offset=86 recorded-under-cursor=1
DEBUG cursorMovedMsg offset=56 recorded-under-cursor=1   <- true position on leaving the card
DEBUG jumpCard delta=1 from-cursor=1 cursorPos[from]=56
DEBUG jumpCard delta=-1 from-cursor=2 cursorPos[from]=0
DEBUG cursorMovedMsg offset=44 recorded-under-cursor=1   <- spurious: fires with NO new KeyEvent of its own
```

The last line has no raw `KeyEvent` logged immediately before it (every other `cursorMovedMsg` does) — it's the freshly-remounted card's widget reacting to the *same* `Ctrl+↑` that triggered the jump back, moving up one further line (56→44) from the just-restored position.

## Proposed

Give `Up`/`Down` the same treatment `Left`/`Right`/`Home`/`End` already get — an explicit `ctrl`-guarded case checked first, so the fallthrough to plain vertical movement never happens for a Ctrl-modified press. The exact semantic is a smaller question than closing the gap itself:

- Simplest: a `ctrl && (ke.Key == input.KeyUp || ke.Key == input.KeyDown)` case that does nothing (`return false`), leaving the keystroke genuinely unclaimed — matches what 9ed's own (incorrect) assumption already expected, and is the minimal, safe default for a key combo this library doesn't otherwise assign meaning to.
- Alternative, if there's an appetite for it: something scroll- or paragraph-oriented (`Ctrl+Up`/`Ctrl+Down` commonly means "scroll without moving the cursor" or "jump a paragraph" in other editors) — a real feature, not just a fix, and a separate design decision from just closing this gap.

## Why this is general-purpose, not 9ed-specific

Any `tui`-based app that assigns `Ctrl+Up`/`Ctrl+Down` to something of its own — switching buffers/tabs/panes, reordering something, any global navigation — inherits this exact same silent double-handling the moment it also happens to use a focused `TextArea`. It was invisible before `OnCursorChange` existed (nothing observed a `TextArea`'s cursor from outside); now that a host can legitimately track and act on that value, this asymmetry between `Left`/`Right`/`Home`/`End` and `Up`/`Down` is a real correctness gap, not a cosmetic one.
