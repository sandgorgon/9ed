# tui: TextArea.OnCursorChange has a structural race against a synchronous cursor-index change elsewhere in the app

**Status:** Resolved in `tui` v0.3.1 — `Run` now resolves a
widget-sourced `Cmd` synchronously before advancing to the next input
event. Re-verified live with the same reproduction this spec's own
evidence came from: the race is genuinely gone. Re-landing the actual
9ed feature this was blocking surfaced a second, independent bug right
behind it — see
[`tui-textarea-ctrl-updown-not-claimed.md`](tui-textarea-ctrl-updown-not-claimed.md)
(tui#20).

**Issue:** https://github.com/sandgorgon/tui/issues/18

**Repo:** github.com/sandgorgon/tui
**Origin:** surfaced actually wiring up 9ed's "restore cursor position across a cross-card jump" feature using `OnCursorChange` (added in v0.3.0, resolving tui#15) — driven live in tmux, not just built and assumed correct, per this project's own verification habit. tui#15's own PR description explicitly named this as the road not taken: "Went with a callback... rather than an exported query method, since no widget in this library returns a handle to query." This spec is the concrete case where that tradeoff bites.

## Problem: the callback's delivery is asynchronous, but the consumer's own state change can be synchronous

`OnCursorChange`'s value is computed synchronously inside `handleKey`/`HandleEvent` — the offset itself is never stale. What's asynchronous is *delivery*: `HandleEvent` returns a `Cmd` wrapping the already-computed `Msg`, and `tui/app.go`'s `Run()` loop executes every `Cmd` on its own goroutine (`runCmd`), sending the result back through `msgCh` for a *later* iteration of the main `select` loop to apply via `Dispatch`.

9ed's `jumpCard` (`cmd/9ed/insert.go`, bound to `Ctrl+↑`/`Ctrl+↓`) does the opposite: it's a synchronous `model.cursor` (which *card* is active) mutation, applied directly inside `Update` in response to the raw `input.KeyEvent`, which `HandleInput` dispatches to `Update` immediately (`a.Dispatch(Msg(e))`), in the very same call that later re-renders the next card's fresh `TextArea`.

Nothing orders these two paths relative to each other. If a user navigates within a card (arrow keys, moving `OnCursorChange`'s reported offset) and then immediately presses the jump key, the jump's synchronous `Update` call can run — and the next card's `TextArea` can mount — *before* the last pending `OnCursorChange` `Msg` for the card being left has been delivered through `msgCh` and applied. The result: 9ed's own tracked "last known cursor position" for the card being left is stale by however many movements were still in flight, off by exactly that much once restored.

## Confirmed with a live, instrumented run

Not a theoretical concern — reproduced directly, with logging added at both the `OnCursorChange`-driven `Update` case and at `jumpCard`'s own entry, for the sequence Enter card → `Up` ×3 → jump away → jump back:

```
DEBUG cursorMovedMsg offset=88 recorded-under-cursor=1
DEBUG cursorMovedMsg offset=86 recorded-under-cursor=1
DEBUG cursorMovedMsg offset=56 recorded-under-cursor=1
DEBUG jumpCard delta=1 from-cursor=1 cursorPos[from]=56      <- jump fires with the STALE value
DEBUG jumpCard delta=-1 from-cursor=2 cursorPos[from]=0
DEBUG cursorMovedMsg offset=44 recorded-under-cursor=1        <- the TRUE final position, arrives too late
```

The third `Up`'s true resulting offset (44) doesn't arrive until *after* both the away-jump and the return-jump have already completed and the card has already been remounted using the stale value (56) as `InitialCursor` — a difference of exactly one line's worth of runes in the test file. This wasn't an occasional flake across repeated attempts with the same shape (several hundred milliseconds between each keystroke, well beyond ordinary goroutine-scheduling latency) — it reproduced the same way each time, meaning this isn't a narrow theoretical edge case but something likely to bite on completely ordinary usage: navigate, then jump, is the entire point of the feature this was built for.

## Why the exported-method half of tui#15's original proposal would actually fix this

tui#15 (and this repo's own `tui-textarea-cursor-readback.md`) proposed two options and only one was taken:

1. ~~A change callback~~ — implemented, has the race above.
2. **An exported query method** (e.g. `CursorOffset() int`) — not implemented, but this is exactly what avoids the race: called synchronously, at the exact moment `jumpCard` is about to leave a card (before triggering any remount), it would return the widget's *true, current* cursor position with no async round trip involved at all. The PR's stated reason for skipping it — "no widget in this library returns a handle to query" — is a real API-shape gap, not a reason the capability isn't needed: `TextArea(opts)` would need to return something queryable (a handle/interface alongside or instead of a bare `tui.Node`) for this to be possible at all, which is more invasive than adding one more callback but is what actually closes the gap.

## Proposed

Give `TextArea(...)` (or a variant of it) a way to be queried synchronously for its current cursor offset — the concrete shape is more of an open question than tui#15's original two-line ask, since it touches how every widget constructor returns a handle, not just `TextAreaOptions`'s field list:

- A `TextArea(...)` variant returning `(tui.Node, *TextAreaHandle)` (or similar) alongside the `Node`, with `TextAreaHandle.CursorOffset() int` reading live internal state — the minimal-surface-area version, opt-in per call site.
- Or a broader pattern (out of this spec's scope to prescribe) if there's an appetite for widgets generally returning queryable handles rather than bare `Node`s.

Whichever shape, the core requirement is the same: a caller synchronously in the middle of `Update` (not through a `Cmd`/`Msg` round trip) must be able to ask "what is this widget's cursor position right now" and get an always-current answer, no async delivery lag possible.

## Why this is general-purpose, not 9ed-specific

Any `tui`-based app that needs to *react to* a navigation event by synchronously consulting a widget's current state (not just observe changes as they stream in) hits the same class of gap `OnCursorChange` alone can't close — a callback is the right shape for "keep me updated," but the wrong shape for "tell me right now, before I do something synchronous." 9ed's cross-card jump is one instance; any editor with tabs/panes/buffer-switching that wants to preserve position across a switch has the identical need.
