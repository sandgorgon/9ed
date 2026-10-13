// Standalone (not running under 9sh) namespace story, per the design:
// 9ed always runs a real 9P server; standalone, /mnt/9ed isn't a
// literal mountpoint (no FUSE, no unprivileged OS mount) — it's a
// Unix-domain-socket address plus a discovery file naming it, meant to
// be dialed (e.g. by a future 9p CLI client), not mounted.

package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/sandgorgon/9p/server"
)

// runtimeDir is where every running 9ed buffer's socket and discovery
// file live: $XDG_RUNTIME_DIR/9ed if set (the systemd-managed per-user
// runtime directory, already 0700 and tmpfs-backed on most Linux
// systems), else a per-uid fallback under the OS temp dir.
func runtimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "9ed")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("9ed-%d", os.Getuid()))
}

// serveBuffer starts view's 9P server on a fresh Unix domain socket
// named by this process's PID (a buffer identifies a running process,
// not a file — two 9ed instances on the same file are two distinct
// buffers) and writes a discovery file alongside it naming which file
// this buffer is editing, for a future cross-instance picker (M12) to
// enumerate without dialing every socket. Returns a cleanup func that
// removes both and stops the server; the caller is responsible for
// calling it before exit.
func serveBuffer(view *bufferView, path string, writes chan<- p9WriteMsg, gotos chan<- p9GotoMsg) (stop func(), err error) {
	dir := runtimeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("runtime dir: %w", err)
	}

	bufid := strconv.Itoa(os.Getpid())
	sockPath := filepath.Join(dir, bufid+".sock")
	discoveryPath := filepath.Join(dir, bufid)

	os.Remove(sockPath) // clear a stale socket left by an earlier, uncleaned exit under this same (reused) PID

	l, err := net.Listen("unix", sockPath)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}
	if err := os.Chmod(sockPath, 0o600); err != nil {
		l.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	if err := os.WriteFile(discoveryPath, []byte(discoveryContent(path)), 0o600); err != nil {
		l.Close()
		return nil, fmt.Errorf("write discovery file: %w", err)
	}

	srv := &server.Server{FS: &bufferFS{view: view, writes: writes, gotos: gotos}}
	go srv.Serve(l) // its error, once stop() closes l, is expected (net.ErrClosed) and has nowhere useful to go

	return func() {
		l.Close()
		os.Remove(sockPath)
		os.Remove(discoveryPath)
	}, nil
}

// updateDiscovery rewrites this buffer's discovery file (see
// serveBuffer) with path — called when following a cross-file link
// replaces the open file, so another instance's buffer picker lists
// what's actually open. Best-effort: a failure leaves the old path
// advertised, which is stale, not broken.
func updateDiscovery(path string) {
	_ = os.WriteFile(filepath.Join(runtimeDir(), strconv.Itoa(os.Getpid())), []byte(discoveryContent(path)), 0o600)
}

// discoveryContent is a discovery file's whole content: the path on
// line 1 (see advertisedPath) and, only under 9sh, the namespace
// socket on line 2 — the identity that says which namespace the path
// is to be read in. Outside 9sh the file is the single path line it
// has always been.
func discoveryContent(path string) string {
	out := advertisedPath(path) + "\n"
	if ns := os.Getenv(nsSockEnv); ns != "" {
		out += ns + "\n"
	}
	return out
}

// advertisedPath is what a discovery file says this buffer has open:
// the absolute OS path when there's no 9sh namespace in play, so
// another instance (a different cwd) can tell whether it's the same
// file — see findOpenBuffer. Under 9sh a relative path is rooted at
// /local, not the OS cwd (see nsopen.go), so it's left as given rather
// than resolved against a cwd that doesn't apply.
func advertisedPath(path string) string {
	if os.Getenv(nsSockEnv) != "" {
		return path
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
