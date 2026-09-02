# 9ed

[![CI](https://github.com/sandgorgon/9ed/actions/workflows/ci.yml/badge.svg?branch=master)](https://github.com/sandgorgon/9ed/actions/workflows/ci.yml?query=branch%3Amaster)

A segmented/card TUI text editor: two modes (Nav/Edit) over a file decomposed
into structurally meaningful **cards** — a Go func, a Markdown heading, a
[`kyu`](https://github.com/sandgorgon/9sh) top-level statement, and so on.
Built entirely on stdlib plus three sibling Plan-9-flavored Go modules —
[`tui`](https://github.com/sandgorgon/tui),
[`9p`](https://github.com/sandgorgon/9p),
[`9sh`](https://github.com/sandgorgon/9sh) — no other dependencies. Two more
modules in the same family, [`9vcs`](https://github.com/sandgorgon/9vcs) and
[`9auth`](https://github.com/sandgorgon/9auth), are deliberately *not*
dependencies — see [Why no `9auth`](#why-no-9auth) below.

Status (`v0.2.0`): M0–M12 implemented — deck
segmentation for all six target languages (Markdown, Go, Bash, C/C++,
Haskell, `kyu`, plus a plain-text fallback for anything else), Nav/Edit
mode shell with `o`/`O` card insertion, `gg`/`G`/`PgUp`/`PgDn`/cross-card-jump
navigation (restoring each card's cursor position across a jump), a
Nav-mode typeahead filter, precise go-to-line, line
numbers, atomic save, a runtime light/dark theme toggle (`t`), a 9P
server surface for a running buffer with a writable `/cards/<n>/body`,
namespace-aware open/save (see
[Namespace-aware file I/O](#namespace-aware-file-io) below), and card
annotations — markdown notes plus `todo`/`needs-review` badges (see
[Card annotations](#card-annotations) below). `9auth` is deliberately
not integrated directly; see [`upstream-specs/`](upstream-specs/) for
gaps this project has filed against its own dependencies along the
way.

## Install

Grab a prebuilt binary from the
[Releases page](https://github.com/sandgorgon/9ed/releases) — each
release has `.tar.gz`s for `linux/amd64`, `linux/arm64`,
`darwin/arm64`, and `darwin/amd64`, plus a `.sha256` to verify against:

```
curl -LO https://github.com/sandgorgon/9ed/releases/download/<tag>/9ed_<tag>_linux_amd64.tar.gz
tar xzf 9ed_<tag>_linux_amd64.tar.gz
```

That unpacks a directory with the `9ed` binary, `LICENSE`, and this
`README.md`. Put the binary somewhere on your `PATH`.

Or build from source:

```
git clone https://github.com/sandgorgon/9ed.git && cd 9ed
go build -o 9ed ./cmd/9ed
```

## What this is

- On disk, a file 9ed edits is always plain text — 9ed never invents a
  format of its own. The **deck** (a file's cards) is a view computed by a
  `Segmenter`, recomputed from the bytes, never stored.
- Every running buffer serves its own state over 9P
  (`github.com/sandgorgon/9p/server`) — `/cards/<n>/{title,body,lang}`,
  `/tag` — so scripting a buffer (from `kyu`, or any shell) is a matter of
  reading and writing files, not calling into an editor-specific API.
- Languages: Markdown, Go, Bash, C/C++, Haskell, and `kyu`. Go and `kyu` get
  real AST-based segmentation (`go/parser`/`go/ast` for Go, `9sh/kyu/parser`
  for `kyu`); the others use good-enough structural heuristics rather than a
  full parser — this project doesn't try to live in C/C++/Bash/Haskell as
  deeply as it does in Go and Haskell as target languages for actual use.

## Namespace-aware file I/O

Outside `9sh` — or under a `9sh` that wasn't started with `-listen-unix` —
9ed reads and writes files exactly like any normal editor: plain OS calls,
nothing namespace-specific happens.

`9sh` doesn't project its namespace onto the OS filesystem for spawned
children (it's a purely in-process 9P construct), so there's no ambient way
for a child process to see it. The one door in: when `9sh` is started with
`9sh -listen-unix <path>`, it exports that socket to every job it spawns as
`$_9SH_UNIX_SOCK` (mirroring `SSH_AUTH_SOCK`'s discovery pattern). 9ed dials
it, walks `/local/<path-relative-to-cwd>` in `9sh`'s namespace, and uses
that for both opening the file and every Save — so a bind the user has set
up at `/local` (redirecting it elsewhere, layering another directory over
it, ...) applies to whatever file 9ed has open too, not just to `9sh`
itself. A path outside cwd, or any failure along the way (no socket, dial
refused, the file isn't reachable there), transparently falls back to the
plain OS path — this is a best-effort enhancement, never a hard
requirement. Save reproduces the same crash-safe temp-file-then-rename
trick the plain OS path uses, just carried over 9P (`cmd/9ed/nsopen.go`)
instead of a direct syscall.

## Card annotations

Cards can carry two kinds of annotation, stored in a per-file sidecar
next to the source (`foo.go` → `foo.go.9an`) rather than inside the
source itself:

- **A markdown note** — press `n` from Nav mode to open the current
  card's note full-screen (Esc returns to Nav), the same shape as
  editing the card's own body.
- **Two fixed badges**, `todo` and `needs-review` — press `f`/`r` from
  Nav mode to toggle them directly, no picker UI.

Nav mode's list line also shows two badges 9ed computes on its own:
`✎` when a card has a note, and `↩ N` when N other cards mention that
card's name — a lexical, not semantic, cross-reference scan (only
`GoSegmenter` has a real parser; the others use structural heuristics),
so it's a hint, not a guarantee.

The `.9an` sidecar is itself plain markdown — a `# kind: title` heading
per annotated card, an optional `flags: ...` line, then the note body —
readable and diffable on its own, and meant to be committed alongside
the source, not personal scratch. Ctrl+S writes it alongside the
source, but only once something in it has actually changed.

## Why no `9auth`

9ed's own 9P server (`cmd/9ed/namespace.go`) is deliberately
Unix-domain-socket-only: a locally-run 9P server has no reason to open a
TCP port, and the socket's own file permissions (`0600`) are already the
right trust boundary — the same category a local directory sits in. Making
a running 9ed buffer reachable from another machine is `9sh`'s job, not
9ed's: `9sh` already has a complete `dial`/`bind`/`-listen` TLS+`9auth`
"network gateway" pattern that re-exports its whole namespace — anything
bound into it, a 9ed buffer included — with zero changes needed in 9ed
itself. See
[`upstream-specs/9sh-bind-local-unix-socket-9p-server.md`](upstream-specs/9sh-bind-local-unix-socket-9p-server.md)
for the (now-resolved, as of `9sh` v0.3.1) gap this uncovered.

## Dependencies

| Module | Version |
|---|---|
| `github.com/sandgorgon/9p` | v0.7.1 |
| `github.com/sandgorgon/9sh` | v0.4.0 |
| `github.com/sandgorgon/tui` | v0.4.0 |

`9vcs` and `9auth` are siblings in the same Plan-9-flavored family but are
not 9ed dependencies — see [Why no `9auth`](#why-no-9auth) above.

## License

MIT — see [`LICENSE`](LICENSE).
