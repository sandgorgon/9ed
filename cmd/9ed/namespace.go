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
	if err := os.WriteFile(discoveryPath, []byte(path+"\n"), 0o600); err != nil {
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
