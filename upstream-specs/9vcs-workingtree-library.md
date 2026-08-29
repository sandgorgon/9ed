# 9vcs: export working-tree-diff orchestration as a library

**Repo:** github.com/sandgorgon/9vcs
**Origin:** surfaced while designing 9ed (a segmented/card editor); this is a
prerequisite for the companion "selective record" spec, but stands on its own.

## Problem

The logic that computes "what changed in the working tree relative to the last-recorded
state" is unexported and lives inside the `cmd/9vcs` main package:

- `changedFiles(r *repo, base patches.Index) (map[string]patches.FileChange, error)` —
  `cmd/9vcs/workingtree.go`
- `materialize(roots ...patches.Hash) (patches.Index, error)`, `workingFiles()`, ref
  resolution — `cmd/9vcs/repo.go`
- `splitLines`/`joinLines` and related working-tree assembly helpers —
  `cmd/9vcs/workingtree.go`

There is currently no way for an external Go program to compute a working-tree diff,
resolve a ref to its recorded line graph, or build a patch, without shelling out to the
`9vcs` binary and parsing its text output.

## Impact

Blocks any external tool wanting real integration rather than a subprocess call. For
9ed specifically: a live per-card diff-against-HEAD as the user types is a local,
synchronous, offline computation that shouldn't need to shell out to a separate process
on every keystroke; building a selective-record patch (see the companion spec) also
needs this directly, not through a CLI round trip.

## Proposed

- Extract the repo-state / working-tree-diff orchestration currently in
  `cmd/9vcs/repo.go` and `cmd/9vcs/workingtree.go` into an importable package — either a
  new `github.com/sandgorgon/9vcs/repo` package, or promoted into the existing
  `objstore/patches` package, whichever fits the maintainer's intended package
  boundaries better.
- Minimum surface an external consumer needs:
  - Open a repo at a given filesystem path.
  - Resolve a ref/HEAD to a `patches.Index`.
  - Compute the current working-tree diff for one path against an index, returning
    `[]patches.LineOp` (what `changedFiles` + `Diff` already produce internally today).
  - Given a subset of ops, produce a new `*patches.Patch` (feeds directly into the
    selective-record spec).
- `cmd/9vcs` itself becomes a thin consumer of this new package — no CLI behavior
  change, just moving the boundary so the same logic is reachable from outside the
  binary.

## Why this is general-purpose, not 9ed-specific

Any external tool wanting tighter-than-subprocess integration with 9vcs — an editor, an
IDE plugin, a CI tool inspecting working-tree state without invoking a binary per
check — needs this. It's a standard "extract the library from the CLI" request, not
specific to 9ed's design.
