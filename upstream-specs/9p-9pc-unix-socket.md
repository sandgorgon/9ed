# 9p: add a -net flag to 9pc for Unix domain socket dialing

**Status:** Resolved in `9p` v0.6.0 — `cmd/9pc` gained a `-net` flag (default
`tcp`, passing through to `client.Dial`), exactly as proposed below.

**Repo:** github.com/sandgorgon/9p
**Origin:** surfaced while designing 9ed (a segmented/card editor); 9ed's standalone
(non-9sh) namespace design relies on scripting against a 9P server over a Unix domain
socket rather than TCP.

## Problem

`p9/cmd/9pc/main.go` hardcodes `client.Dial("tcp", *addr)`. The underlying
`client.Dial(network, addr string, opts ...Option) (*Client, error)` (`client/client.go`)
is already a direct passthrough to `net.Dial(network, addr)` — confirmed by reading the
implementation — so `client.Dial("unix", "/path/to/sock")` already works today at the
library level. The CLI just never exposes that option: there's currently no way to
point `9pc` at a Unix socket without writing a small custom client wrapper.

## Why this matters

A local, single-machine 9P server has no reason to open a TCP port — a Unix domain
socket is faster, stays local, and its file permissions (e.g. `0600`) are a simpler and
more natural access boundary than binding to `localhost`. For 9ed: a standalone (not
running under 9sh) instance listens on a Unix socket by convention
(`$XDG_RUNTIME_DIR/9ed/<bufid>.sock`), and the whole point of leaning on 9P as the
editor's scripting API is that any shell script — kyu or otherwise — should be able to
do `9pc ls /cards`-style calls against it directly, without 9ed needing to ship or
maintain its own separate CLI client.

## Proposed

- Add a `-net` flag to `9pc`, default `"tcp"`, accepting `"unix"` (and passing through
  whatever else `net.Dial`/`client.Dial` already accepts — no need to enumerate values
  in the flag itself).
- When `-net unix`, `-addr` is interpreted as a filesystem path instead of a
  `host:port` pair.
- No changes needed in `p9/client` — `Dial`'s `network` parameter is already a plain
  passthrough, verified by reading `client.Dial`'s implementation.

## Why this is general-purpose, not 9ed-specific

Any local single-machine 9P server benefits from being scriptable over a Unix socket
without requiring a TCP port — this is a small, natural gap in an existing tool
(`9pc` already does `ls`/`cat`/`get`/`put`), not new scope.
