# 9p: 9pc put can't write to a file that already exists

**Issue:** https://github.com/sandgorgon/9p/issues/6

**Repo:** github.com/sandgorgon/9p
**Origin:** surfaced verifying 9ed's M8 write-side 9P surface (`/cards/<n>/body`
becoming writable) live against a real running 9ed instance, using `9pc put` as
the natural client-side idiom the project's own README already describes
("scripting a buffer... is a matter of reading and writing files").

## Problem

`cmd/9pc/main.go`'s `runPut` always calls `target.Create(name, 0644, p9.OWRITE)`
before opening the file for the actual write:

```go
if _, _, err := target.Create(name, 0644, p9.OWRITE); err != nil {
    target.Clunk()
    fatalf("create %s: %v", remote, err)
}
target.Clunk()

out, err := c.Open(remote, p9.OWRITE)
```

`runPut`'s own comment explains this is deliberate — `Create` is used "since the
file doesn't exist yet for `c.Open` to walk to" — but there's no fallback: if
`Create` fails because the file *already exists* (`Tcreate` on an existing name
is a protocol error, not a no-op), `runPut` aborts with `fatalf`, even though
the very next step (`c.Open(remote, p9.OWRITE)`) is exactly what's needed and
would have worked fine on its own. Confirmed against 9ed's `/cards/<n>/body`
(a file that always already exists — see `cmd/9ed/fs9p.go`'s `cardBodyFile`,
intentionally `Open`-writable, not `Create`-able): `9pc put local /cards/1/body`
fails with `create /cards/1/body: fs9p: 1 is read-only` — the parent directory
correctly rejects `Create` (9ed's cards aren't created/removed over 9P, only
edited in place) — while `client.Open(remote, p9.OWRITE)` used directly (bypassing
9pc) writes successfully and round-trips correctly.

## Why this matters

"Write new content to an existing file" (overwrite, the `cp`/`scp` mental model)
is at least as common a use case as "write to a brand-new path" — arguably more
so for scripting against a long-lived server's existing namespace, which is
exactly 9ed's use case. `9pc put` currently can only do the latter.

## Proposed

Try `Open(remote, p9.OWRITE)` first; only fall back to the existing
Walk-parent-and-`Create` dance if `Open` fails (file doesn't exist yet):

```go
func runPut(c *client.Client, root *client.Fid, local, remote string) {
	in, err := os.Open(local)
	if err != nil {
		fatalf("open %s: %v", local, err)
	}
	defer in.Close()

	out, err := c.Open(remote, p9.OWRITE)
	if err != nil {
		dir, name := path.Split(remote)
		if name == "" {
			fatalf("put: remote path %q has no file name", remote)
		}
		target, err := root.Walk(splitPath(dir)...)
		if err != nil {
			fatalf("walk %s: %v", dir, err)
		}
		_, _, err = target.Create(name, 0644, p9.OWRITE)
		target.Clunk()
		if err != nil {
			fatalf("create %s: %v", remote, err)
		}
		out, err = c.Open(remote, p9.OWRITE)
		if err != nil {
			fatalf("open %s: %v", remote, err)
		}
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		fatalf("put %s: %v", remote, err)
	}
}
```

No `client`/`server` package changes needed — both `Open` and `Create` already
exist and behave correctly; this is purely `9pc`'s own CLI logic picking the
wrong one first.

## Why this is general-purpose, not 9ed-specific

Any 9P server exposing files that already exist and are meant to be *edited*
rather than freshly created — which is the common case for anything modeling a
real filesystem, a config surface, or (as here) a scriptable editor buffer —
hits this same `9pc put` limitation. It's a property of `9pc`'s own control
flow, not of 9ed's particular filesystem.
