# Changelog

## Unreleased

- `cmd/9ed`: gave Nav mode's status bar and Edit mode's line-number
  gutter their own background color (`theme.Border`) instead of plain
  unstyled text — the status bar now renders through `tui`'s
  `widget.StatusBar` (already built into `tui`, just unused until now)
  so its background fills the whole row width instead of stopping at
  the last painted glyph, and the gutter picks up the same tint via
  its `Gutter` closure, so both read as one consistent "this is UI
  chrome, not document content" panel. Nav mode's list rows also tint
  by their most prominent badge (`needs-review` → Warning, `todo` →
  Accent, a reference → Info, a note → Secondary) via
  `ListOptions.RowStyles` — `tui`'s `List` is row-granular by design
  (confirmed in `tui`'s own `docs/proposals/text-region-styling.md`:
  "List being row-granular already needs no sub-row span concept"), so
  individual badge glyphs within one row can't each carry their own
  color; whole-row tinting by the most important badge is the closest
  a row-granular `List` can get to "badges read as distinct."
- `cmd/9ed`: syntax highlighting extends from Go-only to C/C++ and
  Bash, via a new regex-based tokenizer (`cLangRe`/`bashLangRe` in
  `highlight.go`) reusing the same four semantic color roles
  (keyword/comment/string/number) `goHighlights` already used for Go —
  a "good enough heuristic," not a real grammar, matching
  `CSegmenter`'s and `BashSegmenter`'s own existing tolerance for that
  trade-off on non-Go languages. Markdown headings display their
  actual level (`H1`..`H6`) in Nav mode's Kind column and Edit mode's
  status line instead of a flat "heading" — see `deck`'s own
  `MarkdownSegmenter`, which now produces that Kind directly.
- Bumped `tui` v0.4.0 → v0.4.1, which fixed
  [`tui#22`](https://github.com/sandgorgon/tui/issues/22) — filed
  after the gutter-coloring work above showed a real defect:
  `TextArea`'s `Gutter` padding/separator columns painted with a
  hardcoded `theme.MutedText()` instead of the caller's own per-row
  style, leaving a one-column seam of default background through an
  otherwise Border-tinted gutter panel. `paintGutterRow` now derives
  that style from `gutter`'s own return value instead; no change
  needed on this side beyond the version bump. Re-verified live in
  tmux: the seam is gone.

## 0.3.0 - 2026-09-02

- `cmd/9ed`: `/` now searches card bodies, not just titles, and matches
  as a case-insensitive regexp instead of a plain substring — the
  project's own "most conspicuous gap" from the original feature
  backlog. `Enter` on a body match lands the cursor there (not the
  default end-of-body position) and highlights every match in the card;
  `Ctrl+N`/`Ctrl+P` then walk forward/backward across every occurrence in
  the file, wrapping at the ends. Landing on a *second* match within an
  already-open card needed a new mechanism — `tui`'s
  `TextAreaOptions.InitialCursor` is read only once at mount, so a
  same-card cursor move (unlike a cross-card one, which naturally
  remounts) needed a manufactured remount via a new `jumpGen` counter
  folded into the widget's `Key`.
- `cmd/9ed`: added whole-file find-and-replace. `Ctrl+R` while searching
  switches to a second field for replacement text; `Enter` with one
  typed starts a confirm-each-match walk (`y`/`n`/`a`/`q`, Vim's
  `:s///gc` convention) across every card with a match, rather than a
  single "replace everything" confirmation — chosen since a replace
  can't be undone once you leave a card (undo is per-card and
  ephemeral). The walk re-scans each card's live body on every step
  rather than caching match offsets, so an in-progress replacement's
  length change can never invalidate a later match.
- `cmd/9ed`: fixed a real bug found while starting the above — every
  keystroke reaches `Update` as a raw `input.KeyEvent` *and* as
  whatever `Msg` the focused `List`'s `onEvent` converts it to, and
  Update's bottom-of-switch "bare `t` toggles theme, bare `q` quits"
  checks had no `m.searching` guard. Typing a query containing `q`
  quit 9ed outright mid-keystroke; one containing `t` silently flipped
  the theme. Confirmed live in tmux before and after the fix.

- `cmd/9ed`: bare `9ed`, or `9ed <dir>`, now opens a directory browser
  ("files are cards of a directory") instead of erroring with
  "usage: 9ed <file>". Descend-only, no parent/`..` navigation, since
  9sh's default namespace only exposes `/local` bound to its own start
  directory — "above" wherever browsing began isn't representable
  through it. Listing prefers 9sh's namespace (new `nsListDir`) and
  falls back to plain `os.ReadDir`, matching the read/save fallback
  shape `nsopen.go` already uses. Also rewrote the CLI's `-h`/`--help`
  text, which had fallen behind the previous entry's search/replace
  work.

- `cmd/9ed`: added Acme-style plumbing. `9ed foo.go:42` opens at a
  specific line, the `path:line` convention compilers/grep already use.
  A new writable `/goto` (sibling to `/tag`/`/cards` in the 9P surface)
  lets an already-running 9ed be plumbed into from outside — a bare
  line number jumps within the open file, `path:line` additionally
  requires `path` to name that file, since 9ed is still single-buffer
  and genuinely can't switch files; a mismatch is rejected with a clear
  error rather than silently ignored. Both reuse the existing
  `goToLine`; `/goto` mirrors `/cards/<n>/body`'s accumulate-then-
  commit-on-`Close` shape exactly.

- `cmd/9ed`: added `u` in Nav mode to revert a card's unsaved body
  edits (item 6, "undo is per-card and ephemeral"). `TextArea`'s own
  undo/redo has no exported hook to extend past one mounted widget
  instance, so this lives in Nav mode instead, one step back only, and
  resets on Save like `m.edited` itself — a session convenience, not
  version history. Turned out to need no new state at all: `cardBody`'s
  existing no-entry fallback already *is* "how this card looked before
  any edits this session," so reverting is just clearing the entry.

- `cmd/9ed`: added a cross-instance buffer picker (`b` from Nav mode) —
  item 8, reframed around 9ed's actual "a buffer is a process" model
  rather than conventional multi-buffer-in-one-process. Lists every
  other running 9ed buffer (reading the shared per-user runtime
  directory each instance already wrote a discovery file into, unused
  infrastructure from as far back as M12); selecting one dials its own
  9P socket to show live `/tag` state, then a typed line number writes
  to its `/goto` to jump it remotely. Found and fixed two real bugs via
  live tmux testing, both from the same root cause (`tui`'s `Dispatch`
  calls `Update` then `render()` *before* checking for a focused
  widget, so a keystroke causing a "no widget" → "a widget" transition
  gets redelivered to that same freshly-mounted widget): the picker's
  own Esc-from-inspect briefly closed the whole picker right back out
  (fixed by making `q`, not Esc, the list view's only "leave" key), and
  a pre-existing one in item 1's replace feature, where dismissing the
  replace-done screen with "any key" could silently trigger a real Nav
  action (Enter opening a card, a digit feeding `{n}G`) immediately
  after — restricted to Esc only, the one key Nav's own key handling is
  guaranteed to ignore if redelivered.

## 0.2.0 - 2026-09-02

- `cmd/9ed`: cross-card jump (`Ctrl+↑`/`Ctrl+↓` in Edit mode) now
  restores each card's cursor position on return, instead of always
  landing at the default start position. Third attempt at this
  feature, and the two that got it working: bumped `tui` to v0.4.0,
  which fixed `tui#20` — `Ctrl+Up`/`Ctrl+Down` (and, extended to the
  same gap, `Ctrl+PgUp`/`Ctrl+PgDown`) are now an explicit no-op case
  in `TextArea.handleKey`, rather than falling through to plain
  vertical movement (see below for what that was corrupting).
  Re-verified with the same methodology that caught both earlier
  bugs — a byte-exact comparison between a no-jump baseline and a
  with-jump run of the identical keystrokes — and confirmed identical
  this time. Also fixed two now-stale code comments in `cmd/9ed`
  written during the earlier failed attempts, which had described the
  bugs as fixed/absent before they actually were.
- Bumped `tui` v0.3.0 → v0.3.1, which fixed `tui#18` (the async-
  ordering race noted below) — `Run` now resolves a widget-sourced
  `Cmd` (e.g. `OnCursorChange`'s callback) synchronously before
  advancing to the next input event. Re-verified live with the exact
  same reproduction that caught the original race: confirmed genuinely
  fixed. Re-landing the cursor-restore feature this was blocking then
  surfaced a *second*, independent bug immediately behind it: unlike
  `Ctrl+Left`/`Right`/`Home`/`End`, `tui`'s `TextArea.handleKey` has no
  `ctrl`-guarded case for `Up`/`Down`, so `Ctrl+↑`/`Ctrl+↓` fall
  through to plain vertical movement — meaning the destination card's
  freshly-mounted widget (already correctly restored via
  `InitialCursor`) immediately "eats" the very same jump keystroke as
  an extra move, overwriting the just-restored position before it's
  ever seen. Confirmed by instrumenting both sides again and reading
  `handleKey` directly, not assumed. Reverted the feature a second
  time; filed [`tui#20`](https://github.com/sandgorgon/tui/issues/20)
  requesting the same Ctrl-guard treatment `Left`/`Right`/`Home`/`End`
  already get, cross-referenced on `tui#18`. New local spec:
  `upstream-specs/tui-textarea-ctrl-updown-not-claimed.md`; corrected
  a stale, now-demonstrably-wrong `cmd/9ed/main.go` comment that had
  claimed `handleKey` has "no Ctrl+Up/Down case" at all.
- Bumped `tui` v0.2.0 → v0.3.0, which added `TextArea.OnCursorChange`
  (resolving `tui#15`, filed by this project). Wired it up to restore
  cursor position across a cross-card jump (`jumpCard`,
  `cmd/9ed/insert.go`) — then backed it out after driving it live in
  tmux (not just building and assuming it worked) surfaced a real,
  consistently reproducible race: `OnCursorChange` only delivers
  asynchronously through `tui`'s Cmd/channel pipeline, so `jumpCard`'s
  synchronous cursor-index change can run, and the next card's
  `TextArea` can mount, before the last pending position update for
  the card being left has arrived — restoring a stale position
  instead of the true last one, off by exactly however much movement
  was still in flight. Confirmed with an instrumented run capturing
  the actual message-arrival order: a card's true final cursor offset
  arrived *after* both halves of the jump had already completed and
  the widget had already remounted using a stale value. Filed
  [`tui#18`](https://github.com/sandgorgon/tui/issues/18) requesting
  the exported-query-method half of `tui#15`'s original proposal
  (`tui#15` only took the callback half) — that's the only way to
  read cursor position synchronously, avoiding the race entirely; a
  callback fundamentally can't. Not reattempted here; see
  `upstream-specs/tui-textarea-cursor-readback-sync.md`.
  - A real, independent bug got caught and fixed as a side effect of
    this work regardless: `m.noteEdited` (added for Note mode) was
    never being reindexed by `insertCard`/`removeCard` the way
    `m.edited` already was, so inserting a card while another had an
    unsaved note/flag change would silently misattribute that
    dirty-tracking to the wrong card afterward. Fixed by extracting
    `shiftKeysForInsert`/`shiftKeysForRemove` (Go generics, shared by
    both maps) rather than tripling the inline shifting logic a third
    map would otherwise have needed.
- Card annotations: markdown notes and two user-authored badges
  (`todo`/`needs-review`), the sharpest gap identified in a feature
  evaluation pass against `v0.1.2` — "no cross-card visual context"
  resolved by metadata (notes/badges), not layout, after checking the
  actual model first rather than defaulting to a conventional
  split-pane fix: 9ed already treats "leave Nav, edit one thing
  full-screen, Esc to return" as the one way to focus on something,
  and the whole feature stays inside that shape rather than fighting
  it. Built bottom-up, each piece committed and tmux-verified
  separately:
  - `deck.Card` gains `Name` — the single identifier a card
    unambiguously defines (a func's name, a single-spec type/var/
    const's name, a Markdown heading's own phrase-length text), empty
    when a card defines none or more than one (an import block, a
    grouped `var (...)` block) — populated in all six segmenters.
  - `deck.References(src, cards) [][]int` — a lexical (not semantic)
    whole-word/whole-phrase scan: for each card with a `Name`, which
    other cards mention it. Only `GoSegmenter` has a real parser, so a
    uniform semantic cross-reference mechanism across all six
    segmenters was never on the table. Two cards sharing the same
    `Name` are never counted as referencing each other — each one's
    own declaration line contains its own name, which is also the
    other's, producing a match with no real reference behind it;
    caught by this feature's own tests before it shipped, not assumed
    correct going in.
  - New `notes` package: parses/writes a `.9an` sidecar per source
    file (`foo.go` → `foo.go.9an`, *appended*, never replacing the
    extension — two source files sharing a basename with different
    extensions must not collide). Format is deliberately plain
    markdown — a `# kind: title` heading per annotated card, an
    optional `flags: a, b` line right under it, then the note body —
    so the sidecar file is itself valid markdown, meant to be
    committed alongside the source, not personal scratch. Keyed by
    `(Kind, Title)`, not card index, since 9ed already supports
    inserting a card mid-file (`o`/`O`), which shifts every index
    after it. Flags deliberately live on their own line rather than
    packed into the heading (`# func: foo() []int { [todo]`) — a Go
    title routinely contains `[`/`]` itself (a slice return type),
    which would make bracket-delimited flags ambiguous to parse back
    out.
  - `cmd/9ed` wiring: `run()` loads a file's `.9an` sidecar
    best-effort (same fallback dial `nsReadFile` already gives the
    source read) and computes `deck.References` at startup and again
    after every successful Save — never on every keystroke, matching
    `References`' own load/save-time design. Nav mode's list line
    gained badges: `✎` (has a note), `↩ N` (referenced by N other
    cards), `⚑`/`⚠` (`todo`/`needs-review`, user-toggled) — flags
    render first (a deliberate signal), then the two passive
    system-derived ones. The existing edited-but-unsaved indicator is
    now `●` (was `*`), and a note edit or flag toggle sets it too, the
    same as a body edit — one dirty concept, not two.
  - New **Note mode** (`n` from Nav) edits the current card's note
    full-screen — reusing `editView`'s exact shape (same `TextArea`
    widget, Esc back to Nav) rather than a new UI paradigm, since a
    note is conceptually no different from a card's own body: one
    thing, full focus. No highlighting or line-number gutter — both
    are about correlating with source structure a note has none of.
    `f`/`r` toggle the two flags directly from Nav, no picker UI — the
    flag vocabulary is small and fixed, and 9ed has no popup/modal
    concept anywhere to have needed one. Ctrl+S now also writes the
    `.9an` sidecar, but only when a note or flag actually changed this
    session — a file that's never had one annotated gets no `.9an`
    written for it. Leaving Note mode (or toggling a flag off) with
    neither a note body nor any flag left drops the sidecar entry
    entirely rather than persisting an empty header. A freshly
    inserted, not-yet-saved card can't take a note or a flag at all —
    its placeholder `(Kind, Title)` is about to change on the next
    Save and would immediately orphan either one.
  - Verified live in tmux throughout, including the full round trip:
    enter Note mode, type, Esc, toggle both flags, see all badges
    render with correct spacing, Ctrl+S, and a fresh reopen showing
    exactly the saved state, with the `.9an` file's on-disk content
    checked directly.
- Investigated jump/return polish (Ctrl+↑/↓ cross-card jump in Edit
  mode, kept in scope from the annotations design pass but deprioritized
  behind it) and found the "know what's worth jumping to" half is
  already substantially covered by the new reference badges above. The
  remaining half — restoring cursor position across a jump-away-and-
  back — is blocked on a missing `tui` capability, not a 9ed design
  choice: `TextAreaOptions.InitialCursor` (M12) is write-only, and
  nothing in `TextArea`'s API lets a caller read the current cursor
  position back out to persist it across the remount `jumpCard`
  triggers. Wrote up
  `upstream-specs/tui-textarea-cursor-readback.md` proposing an
  `OnCursorChange` callback or an exported `CursorOffset()` accessor —
  the read-side counterpart of the gap
  `tui-cursor-offset-and-numeric-gutter.md` already closed for the
  write side — filed as
  [`tui#15`](https://github.com/sandgorgon/tui/issues/15).
- Bumped `9sh` v0.3.1 → v0.4.0. Caught and fixed a real gap it exposed
  in `KyuSegmenter`: `v0.4.0` added three kyu AST node kinds
  (`WhileExpr`/`BreakExpr`/`ContinueExpr` for the new `while`/`break`/
  `continue`, `PassthroughStmt` for `$cmd`, `UnbindStmt` for `unbind`)
  that `kyuStmtTok`/`kyuExprTok`'s type switches weren't exhaustive
  over — Go doesn't warn on a non-exhaustive type switch, so each
  unhandled node silently fell through to a zero-value token,
  collapsing that card's `Span` to byte offset 0 rather than erroring.
  Would have shipped a quiet segmentation-corruption bug for any kyu
  file using the new syntax; fixed in the same commit, before it ever
  reached a release.

## 0.1.2 - 2026-08-31

- `cmd/9ed`: added `-h`/`--help`, printing usage plus the full Nav-mode
  and Edit-mode keybinding list — previously the only recognized flag
  was `-version`/`--version`, and running with no/wrong args just
  printed a one-line `usage: 9ed <file>` to stderr. Also fixed the
  Edit-mode status line itself, which advertised only `esc: back to
  nav` and `^s: save` while silently supporting `^up`/`^down` (jump to
  the previous/next card without leaving Edit, see `jumpCard`) and the
  global `^c: quit` — both real bindings, neither previously documented
  anywhere in the UI. Verified live in tmux: the Edit-mode status line
  renders the added text at full terminal width without truncation.

## 0.1.1 - 2026-08-31

- `cmd/9ed`: fixed the package doc `go doc`/pkg.go.dev showed for this
  command — six files (`fs9p.go`, `goto.go`, `insert.go`, `namespace.go`,
  `nsopen.go`, `search.go`) each carry an explanatory header comment
  with no blank line before `package main`, so every one of them was a
  candidate for Go's doc tooling to pick as "the" package doc; it picked
  `fs9p.go`'s (alphabetically first), never `main.go`'s real one — caught
  by checking the freshly-indexed pkg.go.dev page directly, not just
  local `go doc`. Fixed by adding a blank line before each `package
  main`, which disqualifies a comment from being attached as the
  package doc while leaving it as an ordinary file comment; `deck/deck.go`
  had no such conflict to begin with.
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
