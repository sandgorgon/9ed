# tui: TextArea needs an initial-cursor-offset option, and the gutter proposal needs a multi-character variant

**Status:** Resolved in `tui` v0.2.0 — `TextAreaOptions.InitialCursor
*int` and a multi-character `Gutter func(lineIdx int) (string,
cell.Style)`, exactly as proposed below. Not yet adopted in 9ed itself
(precise go-to-line, show line numbers) — that's separate follow-up work.

**Issue:** https://github.com/sandgorgon/tui/issues/11

**Repo:** github.com/sandgorgon/tui
**Origin:** surfaced designing 9ed's "go to a line number" and "show line numbers"
features (a segmented/card editor built on `tui`) — both need a capability
`tui` doesn't currently expose.

## Problem 1: no way to set `TextArea`'s initial cursor position

`widget.TextAreaOptions` (`widget/textarea.go`) has `Value string` — the field's
initial content, read once at mount — but nothing else position-related:

```go
type TextAreaOptions struct {
	Theme       style.Theme
	Placeholder string
	Value       string
	Highlights  []StyleSpan
	OnChange    func(value string) tui.Msg
	OnSubmit    func(value string) tui.Msg
	ReleaseKey  input.KeyEvent
}
```

Confirmed by reading `textAreaWidget.Reconcile`: `w.mount(w.opts.Value)` runs
only `if !w.mounted`, and there's no equivalent hook for cursor position at
all, mount or otherwise — a freshly-mounted `TextArea` always starts at
whatever `mount` itself defaults to (position 0), with no way for the caller
to say "start here instead."

9ed's own "go to a line number" currently only manages a *coarse* version —
open the card containing that line — because there's no way to also place the
cursor on the exact line within it once the `TextArea` mounts.

## Problem 2: the existing `Gutter` proposal is single-rune, not a numeric column

`docs/proposals/text-region-styling.md` already scopes a related, adjacent gap
(per-line/per-region styling) and proposes three candidate shapes, including:

> 3. **Minimal per-line gutter marker** — `Gutter func(lineIdx int) (rune, cell.Style)`
>    painted in a fixed-width column to the left of each line's normal
>    text... Sufficient for a binary/enum-state indicator (a colored dot per
>    line) if full-line or full-region recoloring isn't wanted.

That shape returns exactly one `rune` per line — enough for a diagnostic dot
or a diff marker, not enough for a line *number* ("245" is three characters).
Also checked whether 9ed could render its own line-number column next to
`TextArea` in the layout instead of needing any `tui` change at all: no —
`textAreaWidget.scrollRow` (its vertical scroll position) is a private field,
never exposed via `TextAreaOptions` or any getter, so a separately-rendered
gutter pane has no way to stay in sync with `TextArea`'s own scrolling once a
buffer is taller than the screen.

## Why this matters beyond one consumer

Both gaps are generic editor capabilities, not 9ed-specific:

- Jumping to (and highlighting) a specific position — a compiler error's
  `file:line:col`, a search result, a stack frame — is something essentially
  every text-editing TUI needs, and currently can't do with `TextArea` at all
  once the widget already exists on screen.
- A numeric line-number gutter is close to the default expectation for any
  code-editing widget; the existing `Gutter` proposal, once accepted, would
  cover diagnostics/diff markers but still leave this specific, extremely
  common case unaddressed.

## Proposed

1. Add a way to set (and, ideally, later update) `TextArea`'s cursor
   position — the minimal version is an option consulted at mount time
   alongside `Value`, e.g. `InitialCursor int` (a rune offset into `Value`,
   clamped to `[0, len(value)]`); consulted in `Reconcile`'s existing
   `if !w.mounted` branch right next to `w.mount(w.opts.Value)`.
2. Extend (or add alongside) the `Gutter` proposal in
   `docs/proposals/text-region-styling.md` a variant that returns a
   *string*, not a single rune — e.g. `Gutter func(lineIdx int) (string, cell.Style)`,
   painted right-aligned in a column sized to the widest value `Gutter`
   returns across the visible rows. A caller wanting the single-rune marker
   case (a diagnostic dot) is still trivially served by returning a
   one-character string; a numeric gutter just returns
   `strconv.Itoa(lineIdx+1)`.

Neither of these needs to block on the other — (1) unblocks "go to an exact
line," (2) unblocks "show line numbers," and 9ed only needs (2) to also cover
what `docs/proposals/text-region-styling.md` already scoped for diagnostics/
diff use cases, so the two are naturally handled by whoever picks up that
proposal next, not two unrelated asks.

## Why this is general-purpose, not 9ed-specific

Any `tui`-based editor (this repo's own `docs/proposals/text-region-styling.md`
already names a second real consumer, kaze) that needs to jump to or display a
specific line hits both of these same gaps — they're properties of `TextArea`'s
current API surface, not something specific to how 9ed happens to use it.
