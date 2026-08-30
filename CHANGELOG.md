# Changelog

## Unreleased

- `deck` package: `BashSegmenter`, `CSegmenter`, `HaskellSegmenter`, and
  `KyuSegmenter` (M7) — deck segmentation now covers all six languages the
  README commits to. Bash/C-C++/Haskell are "good enough" structural
  heuristics (brace/column-based, not a real grammar), documented as such
  in each package doc comment; `KyuSegmenter` is real AST-based
  segmentation via `9sh/kyu/parser`, the same spirit as `GoSegmenter`.
  `cmd/9ed`'s `segmenterFor` now dispatches `.sh`/`.bash`,
  `.c`/`.h`/`.cc`/`.cpp`/`.cxx`/`.hh`/`.hpp`/`.hxx`, `.hs`, and `.kyu` to
  them. Added `github.com/sandgorgon/9sh` v0.2.0 as a dependency.
  `KyuSegmenter` reconstructs byte-accurate spans from kyu's (line, rune-
  column) token positions and, for the handful of AST nodes whose stored
  token is an infix/postfix operator rather than the expression's own
  leftmost token, recurses to the true leftmost operand; `DefineStmt` is
  the one node with no recoverable position at all (its `Tok` is `:=`,
  and `Name` is a bare string), reconstructed by a backward text scan —
  see `upstream-specs/9sh-kyu-definestmt-name-position.md`, filed against
  `9sh` to add a `NameTok` field instead.
- Dependencies: `9p` v0.5.0 → v0.7.0, `tui` v0.1.9 → v0.1.13 — both
  additive-only (new `client.Fid.OpenFile`/`CreateFile`, `cmd/9pc -net` flag,
  `App.SetFocus`/`FocusMsg`); no changes needed in 9ed's own code.
  `upstream-specs/9p-9pc-unix-socket.md`'s filed gap is resolved as of
  `9p` v0.6.0's `-net` flag.
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
- `cmd/9ed`: 9P server surface (M5) — every running buffer serves
  `/tag` and `/cards/<n>/{title,body,lang}` read-only over a Unix domain
  socket (`$XDG_RUNTIME_DIR/9ed/<pid>.sock`, 0600) plus a discovery file
  naming the edited path, best-effort (a failure to start degrades to no
  9P surface, not a refusal to edit). `bufferView` is the thread-safe
  seam publishing the live deck for the server's own goroutine to read,
  since it runs concurrently with tui's single-threaded event loop —
  verified under `-race`, plus a real end-to-end test dialing the actual
  9P wire protocol (`client.Dial("unix", ...)`) rather than calling the
  filesystem methods directly.
