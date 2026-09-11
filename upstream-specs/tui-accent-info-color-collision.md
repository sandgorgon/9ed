# tui: `Theme.Accent` and `Theme.Info` are the same color in both default themes

**Status:** Resolved in `tui` v0.8.1 — `Info` now gets its own distinct
indigo/blue-violet in both `DefaultDark`/`DefaultLight`, with a
`TestAccentAndInfoAreDistinct` regression test. 9ed bumped to v0.8.1 and
reverted the `Error` workaround described below back to `Info` in
`cmd/9ed/highlight.go`.

**Issue:** https://github.com/sandgorgon/tui/issues/40 (closed)

**Repo:** github.com/sandgorgon/tui
**Origin:** surfaced while expanding 9ed's syntax highlighting
(`cmd/9ed/highlight.go`) to use more of `style.Theme`'s semantic roles for
better color separation between token categories. `Accent` and `Info` were
picked as two of the new roles on the assumption that every field on `Theme`
is a distinct color — the doc comments imply exactly that, describing each
field as its own semantic role.

## Problem

`style/theme.go`'s `DefaultDark` and `DefaultLight` both set `Accent` and
`Info` to the identical RGB value:

```go
// DefaultDark
Accent: cell.RGBColor(86, 182, 194),
...
Info:   cell.RGBColor(86, 182, 194),

// DefaultLight
Accent: cell.RGBColor(19, 124, 134),
...
Info:   cell.RGBColor(19, 124, 134),
```

Every other pair of fields on `Theme` is distinct in both defaults (checked
by dumping all eight non-chrome roles for both themes). `Accent`/`Info`
appear to be the one accidental duplicate — nothing in either field's doc
comment says they're meant to be the same color, and a consumer coloring
two different semantic categories with `Accent` and `Info` (9ed's Markdown
highlighter already does this: `codeblock`/`code` get `Accent`, `link` gets
`Info`) gets them rendered identically without any indication why.

## Impact on 9ed

- Markdown's existing `mdGroupStyle` (`cmd/9ed/highlight.go`) colors fenced/
  inline code with `Accent` and links with `Info` — in both default themes
  these look the same, even though the code intends them as two distinct
  categories.
- While adding new syntax-highlighting categories (type names, constants,
  builtin functions/preprocessor directives) for more color separation, 9ed
  worked around this by using `Error` instead of `Info` for the third new
  role, since `Error` is confirmed distinct from every other role already
  in use. That's a usable workaround, but it means `Info` is currently not
  safe to reach for as "the next distinct color" without checking first.

## Proposed

Either:
1. Give `Info` its own distinct value in `DefaultDark`/`DefaultLight` (and
   add it to `style/theme_test.go`'s existing distinctness checks, which
   already assert e.g. Success/Warning/Error stay separated under
   colorblindness simulation and ANSI-16 downsampling — the same class of
   check would have caught this), or
2. If the duplication is intentional (e.g. `Info` was added later as an
   alias for `Accent` for naming-clarity reasons at call sites), document
   that on both fields' doc comments so a consumer doesn't assume
   independence the way 9ed did here.

## Why this is general-purpose, not 9ed-specific

Any `tui`-based consumer that picks theme roles by reading `Theme`'s field
list and doc comments — the intended way to consume it, per `style/theme.go`'s
own "a widget picks a color by role" framing — has no way to discover this
collision short of diffing the actual RGB values themselves, the way this
spec did.
