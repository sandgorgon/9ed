// Namespace-aware file I/O: when 9ed runs as a job under a 9sh that was
// started with `-listen-unix`, 9sh exports the socket path as
// $_9SH_UNIX_SOCK to every job it spawns (mirrors SSH_AUTH_SOCK's
// discovery pattern — see 9sh's cmd/9sh/main.go bootstrap and
// remote.ListenUnix). Dialing it and walking /local/<path-relative-to-cwd>
// gives 9ed the same view of that file 9sh itself has — honoring any
// rebind the user has set up at /local — instead of 9ed's own raw OS
// calls, which would silently bypass it.
//
// 9sh does *not* project its namespace onto the OS filesystem for
// spawned children (it's a purely in-process 9P construct, package ns),
// so this dial is the only way in; there's no ambient inheritance to
// rely on just because 9ed happens to have been launched from within a
// 9sh session. Every function here is a best-effort enhancement: any
// failure (no socket, dial refused, path outside cwd, walk/open error)
// returns ok=false and the caller falls back to plain os.ReadFile/
// atomicWrite — running outside 9sh, or under a 9sh started without
// -listen-unix, must keep behaving exactly like today.

package main

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	p9 "github.com/sandgorgon/9p"
	"github.com/sandgorgon/9p/client"
)

// nsSockEnv names the environment variable 9sh exports when started
// with -listen-unix. See 9sh's cmd/9sh/main.go bootstrap.
const nsSockEnv = "_9SH_UNIX_SOCK"

// readFileNS reads path, preferring 9sh's namespace (see nsReadFile)
// when one is reachable and falling back to plain os.ReadFile otherwise
// — the shared best-effort read both the source file and its .9an
// sidecar (see notes.SidecarPath) go through, so a sidecar honors the
// same /local rebind a source read does.
func readFileNS(path string) ([]byte, error) {
	if data, ok := nsReadFile(path); ok {
		return data, nil
	}
	return os.ReadFile(path)
}

// nsRelPath expresses path relative to the process's working directory,
// reporting ok=false if it can't (a Getwd/Abs failure) or if it lies
// outside cwd entirely (9sh's default namespace only binds cwd at
// /local — see cmd/9sh/main.go's bootstrap — not the whole OS root, so
// there's nothing to walk to for a path elsewhere).
func nsRelPath(path string) (rel string, ok bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	rel, err = filepath.Rel(cwd, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// dialNamespace dials 9sh's namespace socket (from $_9SH_UNIX_SOCK) and
// attaches to it, returning the client and its root Fid. Both nsReadFile
// and nsSaveFile call this independently and close the client when
// done, rather than holding one open for 9ed's whole lifetime — Save
// happens rarely enough that a fresh dial each time is simpler than
// tracking whether a cached connection (or 9sh itself) is still alive.
func dialNamespace() (*client.Client, *client.Fid, error) {
	sock := os.Getenv(nsSockEnv)
	if sock == "" {
		return nil, nil, fmt.Errorf("nsopen: %s not set", nsSockEnv)
	}
	c, err := client.Dial("unix", sock)
	if err != nil {
		return nil, nil, err
	}
	root, err := c.Attach("9ed", "")
	if err != nil {
		c.Close()
		return nil, nil, err
	}
	return c, root, nil
}

// nsReadFile attempts to read path by walking /local/<rel> in 9sh's
// namespace. ok is false whenever the namespace path doesn't apply at
// all (see the package doc comment); the caller must fall back to
// os.ReadFile in that case.
func nsReadFile(path string) (data []byte, ok bool) {
	rel, within := nsRelPath(path)
	if !within {
		return nil, false
	}
	c, root, err := dialNamespace()
	if err != nil {
		return nil, false
	}
	defer c.Close()

	f, err := root.Walk(append([]string{"local"}, strings.Split(rel, "/")...)...)
	if err != nil {
		return nil, false
	}
	defer f.Clunk()
	file, err := f.OpenFile(p9.OREAD)
	if err != nil {
		return nil, false
	}
	defer file.Close()

	data, err = io.ReadAll(file)
	if err != nil {
		return nil, false
	}
	return data, true
}

// nsListDir attempts to list path's entries by walking /local/<rel> in
// 9sh's namespace and reading it as a directory (see browse.go's
// listDir, which falls back to plain os.ReadDir when ok is false, for
// the same reasons nsReadFile's is). rel == "." (path is the namespace
// root itself, i.e. cwd) needs no further Walk elements — Walk with
// zero names is 9P's own "stay where you are," so appending a literal
// "." element would ask to walk into a child named ".", which doesn't
// exist as a real directory entry over 9P.
func nsListDir(path string) (entries []p9.Stat, ok bool) {
	rel, within := nsRelPath(path)
	if !within {
		return nil, false
	}
	c, root, err := dialNamespace()
	if err != nil {
		return nil, false
	}
	defer c.Close()

	elems := []string{"local"}
	if rel != "." {
		elems = append(elems, strings.Split(rel, "/")...)
	}
	f, err := root.Walk(elems...)
	if err != nil {
		return nil, false
	}
	defer f.Clunk()
	dir, err := f.OpenFile(p9.OREAD)
	if err != nil {
		return nil, false
	}
	defer dir.Close()

	entries, err = dir.ReadDir()
	if err != nil {
		return nil, false
	}
	return entries, true
}

// nsSaveFile attempts to atomically write data to path through 9sh's
// namespace: create a temp file alongside the target under /local, then
// rename it into place via WStat. dirfs (the usual backing for /local —
// see 9sh's cmd/9sh/main.go bootstrap) implements a Name-only WStat as
// a plain os.Rename, which already replaces an existing destination
// atomically on POSIX — the same guarantee save.go's atomicWrite relies
// on for the plain-OS path, just carried over 9P instead of a direct
// syscall. ok is false for the same reasons nsReadFile's is, plus any
// failure partway through the write/rename; the caller must fall back
// to atomicWrite in that case.
func nsSaveFile(path string, data []byte) (ok bool) {
	rel, within := nsRelPath(path)
	if !within {
		return false
	}
	c, root, err := dialNamespace()
	if err != nil {
		return false
	}
	defer c.Close()

	elems := append([]string{"local"}, strings.Split(rel, "/")...)
	dirElems, name := elems[:len(elems)-1], elems[len(elems)-1]

	dirFid, err := root.Walk(dirElems...)
	if err != nil {
		return false
	}

	perm := p9.Mode(0o644)
	if existing, err := dirFid.Walk(name); err == nil {
		if st, err := existing.Stat(); err == nil {
			perm = st.Mode & p9.DMPerm
		}
		existing.Clunk()
	}

	tmpName := fmt.Sprintf(".9ed-%d", rand.Int63())
	// CreateFile repositions dirFid itself onto the newly created file
	// (Tcreate's wire semantics — see client.Fid.Create's doc), so
	// dirFid is reused below for both the write and the WStat rename;
	// every exit path clunks it exactly once, via either Remove
	// (cleaning up the half-written temp file, mirroring atomicWrite's
	// own deferred os.Remove) or the final Close.
	tmpFile, err := dirFid.CreateFile(tmpName, perm, p9.OWRITE)
	if err != nil {
		dirFid.Clunk()
		return false
	}
	if _, err := tmpFile.Write(data); err != nil {
		dirFid.Remove()
		return false
	}
	if err := dirFid.WStat(p9.Stat{
		Mode:   p9.Mode(^uint32(0)), // don't touch
		Length: ^uint64(0),          // don't touch
		Name:   name,
	}); err != nil {
		dirFid.Remove()
		return false
	}
	return tmpFile.Close() == nil
}
