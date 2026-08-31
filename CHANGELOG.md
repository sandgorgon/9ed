# Changelog

## Unreleased

- Public-repo hygiene: bumped `.github/workflows/ci.yml`'s
  `actions/checkout`/`actions/setup-go` to their latest majors (v7),
  clearing the Node 20 deprecation warning CI had been logging. Added
  `.github/workflows/release.yml` (tag-triggered, builds
  `linux/amd64`, `linux/arm64`, `darwin/arm64`, `darwin/amd64` binaries
  and publishes a GitHub Release with `.tar.gz`/`.sha256` assets),
  mirroring `9sh`'s own release workflow — the only sibling repo that,
  like 9ed, ships a binary rather than a library. That needed a
  `-version`/`--version` flag (`main.go`), set at release-build time via
  `-ldflags -X main.version=...`, printing `"dev"` for a plain
  `go build`/`go run`. README gained a CI badge and an Install section
  (prebuilt binary from Releases, or build from source) in the same
  shape as `9sh`'s. Deliberately skipped a `CONTRIBUTING.md`: `9p` and
  `9auth` have one, but both declare hard project-specific invariants
  (`9p`: stdlib-only) that warrant it; `9sh` — the closer analog, also a
  binary tool, not a library — has none, and 9ed doesn't practice the
  Gitflow branching model `9p`'s describes, so copying it would
  document a workflow this repo doesn't actually follow.

## 0.1.0 - 2026-08-30

- `cmd/9ed`: a runtime light/dark theme toggle (`t`, Nav mode). `tui`'s
  `style` package already ships everything this needed —
  `style.DefaultDark`/`DefaultLight`, `style.DetectAppearance` (a
  `$COLORFGBG`-based guess) — 9ed just wasn't using any of it: every
  render called `style.DefaultDark()` directly instead of reading shared
  state. `model.theme` is now the single source every render (`navView`'s
  `widget.List`, `editView`'s `widget.TextArea` and Go-highlight theme,
  `helpStyle`'s error color) reads from, seeded at startup via
  `style.Default(style.DetectAppearance(os.Getenv))` and flipped in place
  by `toggleTheme` — a plain swap between the two defaults, not a 3-way
  cycle back through auto-detection, so a session's choice sticks once
  made. Verified live in tmux: `t` visibly swaps the border/highlight
  colors in both Nav and Edit mode, matching `DefaultDark`'s and
  `DefaultLight`'s RGB values exactly.
- Namespace-aware file I/O (`cmd/9ed/nsopen.go`): when `9sh` is reachable
  via `$_9SH_UNIX_SOCK` (set only when it's started with `-listen-unix`),
  9ed's file open (`run()`, `main.go`) and Save (`atomicWrite`, `save.go`)
  now dial into its namespace and walk `/local/<path-relative-to-cwd>`
  there instead of going straight to the OS — honoring any bind the user
  has set up at `/local` rather than silently bypassing it. Save
  reproduces `atomicWrite`'s crash-safe temp-file+rename trick over 9P
  (create a temp file alongside the target, then a Name-only `WStat`
  rename — `dirfs`'s own `WStat` already implements that as a plain
  `os.Rename`, so the same atomic-replace guarantee holds). Both fall
  back to the existing plain OS path whenever the namespace doesn't apply
  — no `9sh`, a path outside cwd, or any failure along the way — so
  running standalone (or under a `9sh` not listening) is unchanged. This
  is the consumer side of the gap the entry below identified; see
  [`upstream-specs/9sh-bind-local-unix-socket-9p-server.md`](upstream-specs/9sh-bind-local-unix-socket-9p-server.md)
  for how it closed. Verified live in tmux: rebinding `/local`
  (`bind /local/sub, /local`) in a running `9sh` session changes both
  what a freshly opened 9ed buffer reads and where Ctrl+S writes, while
  the un-rebound file on disk stays untouched.
- Bumped `9sh` to v0.3.1, which shipped the Unix-socket
  `dial`/`-listen-unix`/`$_9SH_UNIX_SOCK` support the entry above relies
  on.
- Refined the earlier `9auth` conclusion: checked `9sh`'s actual `remote`/
  `ns` packages (not assumed) and confirmed 9ed should never adopt
  `9auth`/TLS directly — `9sh` already has a complete TCP+TLS+9auth
  "network gateway" pattern (`dial`/`bind`/`-listen`) that would make a
  9ed buffer remotely reachable with zero changes to 9ed itself, once one
  narrow gap closes on `9sh`'s side: `remote.Dial` is TCP-only, so there's
  no way today to dial a local Unix-socket 9P server (like 9ed's) and
  `BindFS` it into a namespace for `-listen` to re-export. Filed
  `upstream-specs/9sh-bind-local-unix-socket-9p-server.md`
  (github.com/sandgorgon/9sh#2) proposing `remote.DialUnix` (no TLS
  handshake — the socket's own permissions are already the trust
  boundary, same as `9sh`'s existing `/local` `dirfs` bootstrap bind) plus
  a matching `dialUnix(path)` kyu builtin; `bind`'s existing `MountHandle`
  routing needs no changes, since it's already fully general over any
  `server.FileSystem`.
- `cmd/9ed`: precise go-to-line and line numbers (M12), the two items
  left blocked on `tui` after M11. `{count}G` now places the cursor
  exactly on that line (not just at the top of its card) via `tui`
  v0.2.0's new `TextAreaOptions.InitialCursor` — `goToLine` (`goto.go`)
  computes the target as a *rune* offset within the card's own body
  (`InitialCursor` is rune-based; `Span` is byte-based), correctly
  handling multi-byte runes ahead of the target line via
  `utf8.RuneCountInString` rather than reusing the raw byte offset.
  Edit mode also always shows line numbers now, via `TextAreaOptions`'
  new `Gutter` — file-absolute (matching what `go build`/`grep -n`/a
  stack trace reports), computed once per render from the card's own
  starting line (`cardFirstLine`), not restarting at 1 per card.
  The one real design wrinkle: `InitialCursor` only takes effect at a
  fresh `TextArea` mount, and `model.cursor` alone can't distinguish "I
  just arrived here via go-to-line" from "I'm re-opening this same card
  normally" — so `model.gotoLineCursor`/`gotoLineCard` are cleared by
  every *other* edit-mode-entry/transition path (`enterEditMsg`,
  `insertMsg`, `jumpCard`, Esc), the same discipline
  `pendingG`/`pendingCount` already established. Verified live in tmux:
  `5G` lands the cursor exactly on line 5 (confirmed by typing a marker
  character and watching where it landed), the gutter shows correct
  file-absolute numbers, and both staleness paths (a plain `Enter`
  re-opening the same card, and `Ctrl+Up`/`Ctrl+Down` away and back)
  correctly fall back to the default cursor position instead of
  reapplying the earlier line target.
- Dependencies: `tui` v0.1.13 → v0.2.0, `9p` v0.7.1, `9sh` v0.2.1 — all
  three resolve issues filed against them this session, all purely
  additive/fix-only:
  - `tui` v0.2.0 adds `TextAreaOptions.InitialCursor *int` and a
    multi-character `Gutter func(lineIdx int) (string, cell.Style)`,
    exactly as proposed in
    `upstream-specs/tui-cursor-offset-and-numeric-gutter.md`
    (github.com/sandgorgon/tui#11) — unblocks precise go-to-line and
    showing line numbers, still not implemented in 9ed yet.
  - `9p` v0.7.1 fixes `cmd/9pc put` to `Open` an existing file before
    falling back to Walk+`Create` (github.com/sandgorgon/9p#6) — no
    9ed-side change needed, this only affects the `9pc` CLI.
  - `9sh` v0.2.1 adds `ast.DefineStmt.NameTok` (github.com/sandgorgon/9sh#1)
    — `deck/kyu.go`'s `kyuDefineStart` backward-text-scan workaround is
    gone; `KyuSegmenter` now reads the identifier's real position
    directly, the same as every other statement kind already did.
- `cmd/9ed`: Nav-mode typeahead filter and go-to-line (M11). `/` starts a
  filter — every card whose title doesn't case-insensitively contain the
  typed query drops out of the list live, `Up`/`Down` move within the
  filtered set (not `j`/`k`, now query text), `Enter` jumps straight into
  Edit mode on the match, `Esc` restores the pre-search cursor. Vim's
  `{count}G` now works too — a digit sequence typed before `G` jumps to
  that file line's containing card and opens it (`G` alone is unaffected,
  still "last card"); this is the *coarse* half of "go to a line number"
  from the running list — it opens the right card, not the exact line
  within it, since (confirmed reading `widget.TextAreaOptions`)
  `TextArea` has no way to set an initial cursor position. Filed
  `upstream-specs/tui-cursor-offset-and-numeric-gutter.md` against `tui`
  (github.com/sandgorgon/tui#11) for that and for "show line numbers"
  (the existing unaccepted `Gutter` proposal in `tui`'s own repo is
  single-rune, not a multi-digit column) — both stay blocked until that
  lands upstream, not attempted here.
  Designed the M10 double-dispatch bug fix in from the start this time
  (a multi-digit count needs to survive several keypresses the same way
  `gg` does): `pendingG`/`pendingCount` now share one `cancelPendingNav`
  helper, called from every `Update` branch that's a real alternate
  action, never from the raw-`KeyEvent` fall-through. Verified live in
  tmux: the filtered list narrowing as you type, arrow-key movement
  within it, `Esc`/`Enter` outcomes, zero-match display, `11G` landing on
  the card actually containing line 11, and a stray digit interrupted by
  an unrelated key not leaking into a later bare `G`.
- `cmd/9ed`: four navigation/ergonomics items (M10) — an unknown file
  extension now opens as a single `"text"` card (`deck.PlainSegmenter`)
  instead of 9ed refusing to open the file at all; `gg`/`G` jump to the
  first/last card in Nav mode; `PgUp`/`PgDn` move the cursor by a fixed
  amount; `Ctrl+Up`/`Ctrl+Down` jump to the previous/next card's full
  body from *inside* Edit mode (bounded, not wrapping), replacing the
  Esc→j/k→Enter round-trip — this is also how reading through a file
  works now: Enter once, then page forward through every card.
  Two real bugs surfaced and got fixed during tmux verification, not
  caught by unit tests alone (both now covered by regression tests too):
  (1) every keypress reaches `Update` *twice* — once as the raw
  `input.KeyEvent` (`tui`'s `App.HandleInput` dispatches it
  unconditionally) and once as whatever `Msg` the focused widget's
  `onEvent` produced — so resetting `gg`'s pending-first-press flag
  unconditionally at the top of `Update` canceled the first `g` before
  the second press's own `navG` ever arrived; fixed by resetting it
  individually in every branch that's a genuine alternate action,
  never in the raw-`KeyEvent` case's harmless fall-through a lone
  `g`/`j`/`k`/`o`/`O`/Enter's redundant echo hits. (2) the cross-card
  jump's `editView` needs an explicit `.Key()` so `tui`'s reconciler
  mounts a fresh `TextArea` per card instead of reusing the retained
  instance (`Value` only applies at mount) — keying by `m.cursor`
  wasn't quite enough: abandoning an untouched inserted card during a
  *forward* jump (see M9) can leave the cursor's raw index unchanged
  even though the card now shown there is different (removing a card
  shifts what follows into its old slot), so it's keyed by the card's
  own `Span` instead, which is unique per live card by construction.
- `cmd/9ed`: Nav-mode `o`/`O` (M9) — insert a new, empty card and drop
  straight into Edit mode on it, vim-style: `o` after the cursor, `O`
  before it. Fixes the previous only way to add a card (type it into the
  tail of a neighbor's body and let the next Save's resegmentation split
  it back out) being invisible in Nav mode until Save — the new card is a
  real row, and a real `/cards/<n>/...` over 9P, from the first keystroke.
  A card is a view computed by a `Segmenter`, never stored, so a
  synthetic zero-width card (`Span [pos,pos)`, `Kind "new"`) slots into
  `model.cards` with no changes to `reassemble`, `cardBody`, or `fs9p.go`
  — an untouched one reassembles to nothing and simply isn't there after
  the next resegmentation. The one real cost was `edited map[int]string`
  being keyed by raw index: `insertCard`/`removeCard` (new
  `cmd/9ed/insert.go`) shift every key at or above the insertion point.
  Backing out with Esc before typing anything removes the phantom card
  immediately rather than leaving a blank row until the next Save (a UX
  nicety — Save's resegmentation would drop it either way). Verified live
  in tmux: `o`/`O` placement relative to the cursor, a typed function
  correctly resegmenting into its own card on Save, and an abandoned
  empty insert vanishing cleanly on Esc.
- Investigated `9auth` integration (the last item on the open-work list)
  and concluded it isn't actionable yet, rather than forcing it in: `9auth`
  provides TLS fingerprint-based peer trust for a *network*-exposed
  server, and its only real consumer, `9vcs` (`cmd/9vcs/serve.go`/
  `sync.go`), uses it exclusively for TCP remote push/pull — never for
  local filesystem access. 9ed's 9P server is deliberately Unix-socket-
  only, standalone-only (see `upstream-specs/9p-9pc-unix-socket.md`'s own
  rationale for why); there's no existing or planned feature exposing a
  buffer's 9P server over a network for `9auth`'s trust model to
  authenticate. Revisit once a real "remote buffer" feature is scoped,
  not before.
- `cmd/9ed`: write-side 9P surface (M8) — `/cards/<n>/body` is now
  writable (`title`/`lang`/`tag` stay read-only, matching what the
  existing TextArea-based edit flow already lets a user change). Opening
  for write always starts from an empty buffer regardless of `OTRUNC`
  (there's no partial-write story, same "whole snapshot" model the read
  side already uses), so the plain `client.Open(path, p9.OWRITE)` +
  `Write` + `Close` idiom — what a real `9pc` doing
  `9pc put local /cards/N/body` would produce, once
  upstream-specs/9p-9pc-put-overwrite-existing-file.md is fixed —
  replaces a card's body outright. A write only actually lands once
  `Close` returns: it blocks on a round trip through the tui event loop
  (`p9WriteMsg`/`waitForP9Write`, the same long-running-Cmd-listening-on-
  a-channel pattern `tui`'s docs/GUIDE.md calls for), which is the only
  goroutine allowed to touch `model` — applies the exact same
  `setEdited`/`view.publish` path an interactive TextArea edit already
  goes through, so Nav mode's `*` marker and a later Save behave
  identically whether the edit came from a keystroke or a script.
  Verified against a real running 9ed instance over its actual Unix
  socket (`client.Open`/`Write`/`Close`, not just `go test`): the edit
  appears live in a tmux-driven Nav-mode view, and Ctrl+S persists it to
  disk correctly. `9pc put` itself can't exercise this yet — see
  upstream-specs/9p-9pc-put-overwrite-existing-file.md, a gap filed
  against `9p` after `9pc put` failed on an existing file while the
  underlying `Open(OWRITE)` it should fall back to worked fine directly.
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
