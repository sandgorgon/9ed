# 9vcs: selective (partial) record, keyed by line identity

**Repo:** github.com/sandgorgon/9vcs
**Origin:** surfaced while designing 9ed (a segmented/card editor) against 9vcs's actual
patch model — not a git-staging request; see "why this isn't git `add -p`" below.

## Problem

`9vcs record` always diffs the entire working tree against the current HEAD and folds
the whole delta into one patch (`cmd/9vcs/workingtree.go`, `cmd/9vcs/record.go`). There
is no way to record a subset of pending changes — e.g. one finished function, leaving
other in-progress edits in the same file (or other files) still pending. This is a
direct consequence of 9vcs's documented design ("the working tree itself is the staging
area" — README) — there's no index to stage a subset into ahead of time, which is fine,
but it also means there's currently no way to *select* a subset at record time either.

## Why this isn't "just add git's staging area"

Git's partial-commit story (`git add -p`) works by selecting hunks — contiguous
byte/line ranges — which is fragile: hunks shift as other hunks are staged/unstaged,
and "the same hunk" isn't a stable concept across edits. 9vcs doesn't have that problem
by construction: `objstore/patches/diff.go`'s `Diff(old []Line, new []string) (ops
[]LineOp, newIndex []Line)` already produces ops anchored to a stable per-line identity
(`Line.ID`, assigned by `newLineID` and surviving edits to *other* lines — see the
package doc comment on `Line`). A selection of "the ops touching these line IDs" is a
well-defined, order-independent subset of a diff in a way a byte-offset hunk selection
isn't. Darcs (patch theory, from which 9vcs draws) already does interactive per-hunk
`record` for exactly this reason; 9vcs is well-positioned to do the identity-keyed
version properly.

## Proposed

- `9vcs record` gains a selection mode:
  - `9vcs record -p` — interactive, prompts per-hunk/per-op like `darcs record`.
  - `9vcs record --lines <path>:<id>[,<id>...]` — programmatic, for tools that want to
    select a set of line IDs directly without a TTY (e.g. an editor recording just the
    ops belonging to one on-screen unit of a file).
- Underneath both: partition `Diff`'s `ops []LineOp` into *selected* (folded into the
  new patch) and *unselected* (left as still-pending working-tree changes — still shows
  up in a subsequent `9vcs status`/`9vcs diff`, exactly as if that part had never been
  touched by `record`).
- A library-level entry point that takes a set of selected op/line IDs and returns the
  resulting `*patches.Patch`, so both the CLI and external callers can use it directly
  (this depends on the working-tree-diff orchestration being externally importable —
  see the companion spec, "9vcs: export working-tree-diff orchestration as a library").
- Unchanged constraint, inherited from 9vcs's existing model: selection only ever
  operates on ops computed from the current *working tree* content on disk — there's
  nothing to select that isn't already written to a file. No index, no staging ahead of
  time.

## Open questions for the maintainer

- Interaction with an in-progress merge/conflict — presumably disallowed, the same way
  whole-tree `record` already requires a clean merge state.
- Whether one selective record can span multiple files in a single patch (consistent
  with whole-tree record today) or should be restricted to one file per invocation.

## Why this is general-purpose, not 9ed-specific

Darcs-style interactive/selective record is a well-established feature request in the
patch-theory VCS space independent of any editor. Any 9vcs user working on multiple
unrelated changes within one file benefits from this.
