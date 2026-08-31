# 9sh: no way to bind a local Unix-socket 9P server into the namespace

**Status:** Resolved in `9sh` v0.3.1 — `dial(addr)` now accepts a
Unix-socket path directly (issue #2), exactly as proposed below. `9sh`
also shipped `-listen-unix`/`$_9SH_UNIX_SOCK` in the same release (a
separate, `9sh`-side addition, github.com/sandgorgon/9sh#3, not proposed
here) — together these are what `cmd/9ed/nsopen.go` actually consumes;
see the README's "Namespace-aware file I/O" section.

**Issue:** https://github.com/sandgorgon/9sh/issues/2

**Repo:** github.com/sandgorgon/9sh
**Origin:** surfaced deciding whether 9ed (a segmented/card editor, itself
built on `9p`) should adopt `9auth`/TLS to make a running buffer reachable
from another machine. It shouldn't — that's `9sh`'s job, as the namespace/
network gateway — but the specific piece that would let it work is missing.

## Problem

`9sh` already has a complete, working pattern for making a namespace
reachable across machines: `dial(addr)` (`kyu/eval/builtins.go`'s `biDial`)
opens a mutual-TLS connection via `remote.Dial` (`remote/remote.go`), wraps
the result as a `value.MountHandle`, and `bind h, /n/host` (`kyu/eval/
namespace.go`) detects that kind and routes it to `ns.Namespace.BindFS`
(`ns/ns.go:122`) instead of the ordinary `BindPath` used for reshaping
paths already in the namespace. `9sh -listen host:port` then re-exports
the *entire* local namespace — including anything bound in this way — back
out over that same TLS/`9auth` mechanism (`cmd/9sh/main.go`'s `bootstrap`,
`remote.Listen`).

That whole path is TCP-only, though: `remote.Dial`'s `dial` (`remote/
remote.go`) hardcodes `d.DialContext(ctx, "tcp", addr)` before the TLS
handshake. There is no Unix-domain-socket transport anywhere in the
package (confirmed by grep — no `"unix"`/`net.UnixConn` reference exists
in `9sh` at all), and no builtin exposes one. The one piece that *would*
make this trivial to wire up already exists but is unreachable from
outside the package: `clientFS` (`remote/client_fs.go`) adapts any
already-attached `*client.Fid` root into a `server.FileSystem` — exactly
what `BindFS` wants — but it's only ever constructed from a TLS-dialed
connection inside `remote.dial`.

Compare `9sh`'s own `/local` bootstrap bind (`cmd/9sh/main.go`'s
`bootstrap`): `dirfs.New(cwd)` (a plain, local, in-process
`server.FileSystem` — no network, no TLS) gets `BindFS`'d directly. A
locally-run 9P server reached over a Unix socket sits in exactly the same
trust category as a local directory — the socket's own file permissions
(9ed's, for example, is `0600`) are the boundary, the same reasoning
`9p`'s own `-net unix` gap (already resolved: `9p`#3) and `9pc` share —
but today there's no equivalent bootstrap (or kyu-level) path for "dial a
local Unix-socket 9P server and bind it in," only "dial a remote TLS peer"
or "bind a local OS directory."

## Why this matters

Plan 9's actual model is that individual programs never need to know
about networking at all — the namespace/mount layer handles that
transparently, and a program just serves its own state locally. 9ed
already follows this: it serves `/cards/<n>/...` and `/tag` over a
`0600` Unix socket and has no reason to ever link `9auth` or open a TCP
port itself (see `upstream-specs/9p-9pc-unix-socket.md`'s own reasoning
for why a local single-machine 9P server has no reason to open a TCP
port). For a 9ed buffer to become reachable from another machine — or
just from a different `9sh` session on the same machine — the missing
step is entirely on `9sh`'s side: something has to dial that local socket
and graft it into a namespace `9sh -listen` can then re-export. Nothing
in 9ed changes for this to work.

## Proposed

1. A Unix-socket counterpart to `remote.Dial`, e.g. `remote.DialUnix(ctx
   context.Context, path string) (*Conn, error)` — `net.Dial("unix",
   path)` directly, no TLS handshake (the socket's own permissions are
   already the trust boundary, matching `dirfs`'s local-directory
   treatment), then the same `client.NewClient`/`AttachContext(ctx,
   "9sh", "")`/`clientFS` wrapping `dial` already does post-handshake.
2. A matching kyu builtin, e.g. `dialUnix(path)` (mirroring `biDial`
   exactly), returning a `value.MountHandle{Addr: path, FS: conn.FS()}` —
   `bind`'s existing `MountHandle`-routing logic needs no changes at all,
   so `bind dialUnix("/run/user/1000/9ed/12345.sock"), "/n/9ed"` works the
   same way `bind dial("host:port"), "/n/host"` already does.

Both are small and additive: `DialUnix` is `Dial` minus the TLS
handshake, sharing everything else; the builtin is `biDial` with a
different dial call underneath. No changes needed to `bind`, `BindFS`,
`clientFS`, or `-listen` — they're already fully general over *any*
`server.FileSystem`, regardless of how it was obtained.

## Why this is general-purpose, not 9ed-specific

Any locally-run 9P server that wants to be scriptable from a `9sh`
namespace — or reachable from another machine via `9sh -listen`, without
that program ever linking `9auth` or opening a TCP port itself — hits
this same gap. It's a property of `remote`'s dial path being TCP-only,
not something specific to 9ed's own buffer server.
