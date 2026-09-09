// Namespace-aware file I/O: when 9ed runs as a job under a 9sh that was
// started with `-listen-unix`, 9sh exports the socket path as
// $_9SH_UNIX_SOCK to every job it spawns (mirrors SSH_AUTH_SOCK's
// discovery pattern — see 9sh's cmd/9sh/main.go bootstrap and
// remote.ListenUnix). Dialing it gives 9ed the same view of a file 9sh
// itself has, instead of 9ed's own raw OS calls, which would silently
// bypass any rebind the user has set up.
//
// Resolution never consults the OS working directory. cwd is a
// convenience for a plain local-filesystem program's relative paths;
// a 9sh native program is dispatched through the namespace, which has
// its own root, so leaning on cwd here would silently reintroduce a
// second, inconsistent notion of "relative to what." Instead:
//
//   - An absolute argv path is tried as a literal namespace path first
//     (split on "/", walked from the namespace root as given). If that
//     doesn't resolve, the namespace doesn't claim it, and it's opened
//     as a real absolute OS path instead.
//   - A non-absolute argv path is always rooted at /local, 9sh's own
//     bind for the job's working directory (see cmd/9sh/main.go's
//     bootstrap) — never the process's actual OS cwd. If that doesn't
//     resolve, there's no cwd fallback to fall to (see above), so it's
//     a hard failure.
//
// Either way, once the parent directory of the target resolves in the
// namespace but the final component doesn't, that's not a failure —
// it means the location is real but the file itself doesn't exist yet,
// which readFileNS reports as a wrapped fs.ErrNotExist so callers that
// already special-case "no such file, start a new buffer" don't need a
// second code path for the namespace case.
//
// 9sh does *not* project its namespace onto the OS filesystem for
// spawned children (it's a purely in-process 9P construct, package ns),
// so this dial is the only way in; there's no ambient inheritance to
// rely on just because 9ed happens to have been launched from within a
// 9sh session.

package main

import (
	"fmt"
	"io"
	"io/fs"
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
// same /local rebind a source read does. A path the namespace claims
// but that doesn't (yet) exist there comes back as a wrapped
// fs.ErrNotExist, exactly like os.ReadFile's own not-exist error, so a
// caller that treats "file doesn't exist" as "start a new buffer"
// doesn't need to special-case which source the error came from.
func readFileNS(path string) ([]byte, error) {
	if data, found, err := nsReadFile(path); found {
		return data, err
	}
	return os.ReadFile(path)
}

// splitElems splits an argv path into the sequence of names to Walk
// from wherever the caller starts (the namespace root for an absolute
// path, /local for a relative one — see nsReadFile/nsSaveFile/
// nsListDir), after normalizing it with filepath.Clean so "./foo" and
// "foo" resolve identically. A path that cleans down to "/" or "."
// yields nil: the starting point itself, not a name under it.
func splitElems(path string) []string {
	clean := filepath.ToSlash(filepath.Clean(path))
	trimmed := strings.Trim(clean, "/")
	if trimmed == "" || trimmed == "." {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// nsPathElems resolves path to the full sequence of names to Walk from
// the namespace root: path's own elements when absolute, or /local's
// when not (see the package doc comment for why cwd never enters into
// this).
func nsPathElems(path string) []string {
	if filepath.IsAbs(path) {
		return splitElems(path)
	}
	return append([]string{"local"}, splitElems(path)...)
}

// splitParent splits elems into its parent directory's elements and its
// final component, reporting ok=false when elems is empty — the
// namespace root (or /local) itself, which isn't a file to open or
// overwrite.
func splitParent(elems []string) (dir []string, name string, ok bool) {
	if len(elems) == 0 {
		return nil, "", false
	}
	return elems[:len(elems)-1], elems[len(elems)-1], true
}

// dialNamespace dials 9sh's namespace socket (from $_9SH_UNIX_SOCK) and
// attaches to it, returning the client and its root Fid. Every ns*
// function here calls this independently and closes the client when
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

// nsReadFile attempts to read path through 9sh's namespace (see the
// package doc comment for how path resolves). found reports whether
// the namespace has anything to say about path at all; when found is
// false, the caller must fall back to plain os.ReadFile — either no
// namespace is reachable at all, or path was absolute and the
// namespace doesn't claim it, so it should be read as a real OS path
// instead.
//
// When found is true, err distinguishes a successful read (err == nil)
// from "the location is real but this file doesn't exist there yet"
// (err wraps fs.ErrNotExist) from a genuine failure walking or reading
// it. A relative path whose parent doesn't even resolve is the latter,
// not a case that falls back to the real filesystem — that fallback
// would have to go through cwd, which this package deliberately never
// does (see the package doc comment).
func nsReadFile(path string) (data []byte, found bool, err error) {
	c, root, dialErr := dialNamespace()
	if dialErr != nil {
		return nil, false, nil
	}
	defer c.Close()

	dir, name, ok := splitParent(nsPathElems(path))
	if !ok {
		return nil, false, nil // the root/`/local` itself, not a file
	}

	dirFid, walkErr := root.Walk(dir...)
	if walkErr != nil {
		if filepath.IsAbs(path) {
			return nil, false, nil // not a namespace path; try the real fs
		}
		return nil, true, fmt.Errorf("nsopen: %s: %w", path, walkErr)
	}
	defer dirFid.Clunk()

	f, walkErr := dirFid.Walk(name)
	if walkErr != nil {
		return nil, true, fmt.Errorf("nsopen: %s: %w", path, fs.ErrNotExist)
	}
	defer f.Clunk()

	file, err := f.OpenFile(p9.OREAD)
	if err != nil {
		return nil, true, fmt.Errorf("nsopen: %s: %w", path, err)
	}
	defer file.Close()

	data, err = io.ReadAll(file)
	if err != nil {
		return nil, true, fmt.Errorf("nsopen: %s: %w", path, err)
	}
	return data, true, nil
}

// nsListDir attempts to list path's entries by walking it in 9sh's
// namespace and reading it as a directory. Unlike nsReadFile/
// nsSaveFile, a Walk failure here always falls back to plain
// os.ReadDir regardless of whether path was absolute or relative:
// browsing is exploratory rather than a specific location the user
// named, so degrading to the OS view (see browse.go's listDir) is the
// more useful behavior than a hard failure.
func nsListDir(path string) (entries []p9.Stat, ok bool) {
	c, root, err := dialNamespace()
	if err != nil {
		return nil, false
	}
	defer c.Close()

	f, err := root.Walk(nsPathElems(path)...)
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
// namespace: create a temp file alongside the target (see nsReadFile
// for how the target resolves, and the package doc comment for why a
// missing final component isn't itself a failure), then rename it into
// place via WStat. dirfs (the usual backing for /local — see 9sh's
// cmd/9sh/main.go bootstrap) implements a Name-only WStat as a plain
// os.Rename, which already replaces an existing destination atomically
// on POSIX — the same guarantee save.go's atomicWriteOS relies on for
// the plain-OS path, just carried over 9P instead of a direct syscall.
//
// found and err follow nsReadFile's contract: found=false means "not
// applicable, fall back to atomicWriteOS" (no namespace reachable, or
// an absolute path the namespace doesn't claim); found=true means the
// namespace resolved the target's directory and committed to writing
// there, so any err from that point on — including a relative path
// whose directory doesn't resolve at all — is a real failure to
// surface, not something to retry against the real filesystem.
func nsSaveFile(path string, data []byte) (found bool, err error) {
	c, root, dialErr := dialNamespace()
	if dialErr != nil {
		return false, nil
	}
	defer c.Close()

	dir, name, ok := splitParent(nsPathElems(path))
	if !ok {
		return false, nil // the root/`/local` itself isn't a file to overwrite
	}

	dirFid, walkErr := root.Walk(dir...)
	if walkErr != nil {
		if filepath.IsAbs(path) {
			return false, nil // not a namespace path; try the real fs
		}
		return true, fmt.Errorf("nsopen: %s: %w", path, walkErr)
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
	// (cleaning up the half-written temp file, mirroring atomicWriteOS's
	// own deferred os.Remove) or the final Close.
	tmpFile, err := dirFid.CreateFile(tmpName, perm, p9.OWRITE)
	if err != nil {
		dirFid.Clunk()
		return true, fmt.Errorf("nsopen: %s: %w", path, err)
	}
	if _, err := tmpFile.Write(data); err != nil {
		dirFid.Remove()
		return true, fmt.Errorf("nsopen: %s: %w", path, err)
	}
	if err := dirFid.WStat(p9.Stat{
		Mode:   p9.Mode(^uint32(0)), // don't touch
		Length: ^uint64(0),          // don't touch
		Name:   name,
	}); err != nil {
		dirFid.Remove()
		return true, fmt.Errorf("nsopen: %s: %w", path, err)
	}
	if err := tmpFile.Close(); err != nil {
		return true, fmt.Errorf("nsopen: %s: %w", path, err)
	}
	return true, nil
}
