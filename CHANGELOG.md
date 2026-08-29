# Changelog

## Unreleased

- Project scaffolding: `go.mod`, dependency pins, CI, license.
- `deck` package: `Card`/`Segmenter`, plus `MarkdownSegmenter` and `GoSegmenter`
  (`go/parser`/`go/ast`), each covering `[0, len(src))` exactly.
- `cmd/9ed`: Nav-mode shell — read-only, navigable card list for a `.go`/`.md`
  file, via `tui`'s `widget.List`.
- `cmd/9ed`: Edit mode — Enter focuses the current card in `widget.TextArea`,
  Esc returns to Nav mode; Go cards get syntax highlighting via `go/scanner`.
  Edits are in-memory only (an `edited` unsaved-marker shows in Nav mode);
  Save lands in M4.
