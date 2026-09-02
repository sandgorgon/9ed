// Acme-style plumbing (backlog item 3): jump straight to a card given a
// "path:line" position, the convention compilers and grep already use
// for their own output. Two entry points, both reusing goto.go's
// existing goToLine — this feature is glue around it, not new
// navigation logic:
//
//   - `9ed foo.go:42` (see parseFileLine, used by run()) opens foo.go
//     and jumps straight to line 42, e.g. pasting a build error's own
//     "file:line" as the argument.
//   - a writable /goto control file (see gotoFile, sibling to /tag and
//     /cards in fs9p.go) lets an *already-running* 9ed be plumbed into
//     from outside — the actual "click a build error, jump straight
//     there" case, if something (a build script, another namespace
//     client) writes the position instead of a human retyping it.
//
// 9ed is still single-buffer (backlog item 8 isn't done), so neither
// entry point can ever switch files: the CLI form only works because
// opening *is* choosing the file, and /goto errors clearly, rather than
// silently ignoring the request, whenever the written path doesn't name
// the file already open — confirmed with the user rather than assumed,
// since silently ignoring would leave a caller unable to tell "jumped"
// from "ignored."

package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// parseFileLine splits a "path" or "path:line" argument — the
// convention grep/compilers use for a source position — into its
// parts. hasLine is false whenever there's no ":<positive integer>"
// suffix to find, in which case path is arg unchanged: a bare filename,
// or one that happens to contain a colon but no trailing number (an
// unusual filename, not a location) — so an ordinary path is never
// misparsed just because it contains ':' somewhere.
func parseFileLine(arg string) (path string, line int, hasLine bool) {
	i := strings.LastIndex(arg, ":")
	if i < 0 || i == len(arg)-1 {
		return arg, 0, false
	}
	n, err := strconv.Atoi(arg[i+1:])
	if err != nil || n <= 0 {
		return arg, 0, false
	}
	return arg[:i], n, true
}

// parseGotoRequest parses /goto's write content: either a bare line
// number ("42"), which always targets whatever file 9ed already has
// open, or "path:42" (parseFileLine's own grammar), which additionally
// requires path to name that same file — see gotoFile.Close, the only
// caller, for what happens when it doesn't.
func parseGotoRequest(content string) (path string, line int, err error) {
	s := strings.TrimSpace(content)
	if n, convErr := strconv.Atoi(s); convErr == nil && n > 0 {
		return "", n, nil
	}
	path, line, hasLine := parseFileLine(s)
	if !hasLine {
		return "", 0, fmt.Errorf("fs9p: goto: %q is not a line number or path:line", s)
	}
	return path, line, nil
}

// samePath reports whether a and b name the same file, comparing
// cleaned absolute paths rather than raw strings — so "foo.go" and
// "./foo.go" (or any other equivalent relative spelling) compare equal
// when they're actually the same file. Falls back to a raw string
// comparison if either fails to resolve (a Getwd failure), which can
// only ever under-match, never wrongly report a match.
func samePath(a, b string) bool {
	aAbs, aErr := filepath.Abs(a)
	bAbs, bErr := filepath.Abs(b)
	if aErr != nil || bErr != nil {
		return a == b
	}
	return aAbs == bAbs
}
