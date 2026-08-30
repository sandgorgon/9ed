# 9ed

A segmented/card TUI text editor: two modes (Nav/Edit) over a file decomposed
into structurally meaningful **cards** — a Go func, a Markdown heading, a
[`kyu`](https://github.com/sandgorgon/9sh) top-level statement, and so on.
Built entirely on stdlib plus four sibling Plan-9-flavored Go modules —
[`tui`](https://github.com/sandgorgon/tui),
[`9p`](https://github.com/sandgorgon/9p),
[`9sh`](https://github.com/sandgorgon/9sh),
[`9vcs`](https://github.com/sandgorgon/9vcs),
[`9auth`](https://github.com/sandgorgon/9auth) — no other dependencies.

Status: M0–M12 implemented — deck segmentation for all six target languages
(Markdown, Go, Bash, C/C++, Haskell, `kyu`, plus a plain-text fallback for
anything else), Nav/Edit mode shell with `o`/`O` card insertion,
`gg`/`G`/`PgUp`/`PgDn`/cross-card-jump navigation, a Nav-mode typeahead
filter, precise go-to-line, line numbers, atomic save, and a 9P server
surface for a running buffer with a writable `/cards/<n>/body`. `9auth`
integration is the only item still open; see
[`upstream-specs/`](upstream-specs/) for gaps this project has
already filed against its own dependencies.

```
go get github.com/sandgorgon/9ed
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

## Dependencies

| Module | Version |
|---|---|
| `github.com/sandgorgon/9p` | v0.7.1 |
| `github.com/sandgorgon/9vcs` | v0.1.3 |
| `github.com/sandgorgon/9auth` | v0.1.0 |
| `github.com/sandgorgon/tui` | v0.2.0 |
| `github.com/sandgorgon/9sh` | v0.2.1 |

## License

MIT — see [`LICENSE`](LICENSE).
