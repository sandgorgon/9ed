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
- `cmd/9ed`: Save (Ctrl+S) — atomic whole-file write (temp file + rename,
  original permissions preserved), reassembling cards in order; no
  incremental per-card writes. A failed save surfaces in the status line
  instead of crashing. Reassembly inserts a newline between cards when an
  edit strips one, so a card's edit can never silently glue onto the next
  card's first line (bug caught interactively, now a regression test).
