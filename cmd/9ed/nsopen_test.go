package main

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandgorgon/9p/examples/dirfs"
	"github.com/sandgorgon/9p/server"
)

// startTestNamespace serves a dirfs rooted at parent over a Unix socket
// (standing in for 9sh's namespace socket — see nsopen.go's package doc)
// and sets $_9SH_UNIX_SOCK to it for the duration of the test. parent
// must already contain a "local" subdirectory: production 9sh binds its
// own cwd at /local (see 9sh's cmd/9sh/main.go bootstrap), so a
// dirfs-rooted-one-level-up-with-a-"local"-child reproduces exactly that
// layout without depending on 9sh's own ns package.
func startTestNamespace(t *testing.T, parent string) {
	t.Helper()
	fs, err := dirfs.New(parent)
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(t.TempDir(), "9sh.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &server.Server{FS: fs}
	go srv.Serve(l)
	t.Cleanup(func() { l.Close() })
	t.Setenv(nsSockEnv, sock)
}

func TestNsRelPath(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)

	if rel, ok := nsRelPath("note.md"); !ok || rel != "note.md" {
		t.Errorf("note.md: got (%q, %v), want (\"note.md\", true)", rel, ok)
	}
	if rel, ok := nsRelPath(filepath.Join(cwd, "sub", "note.md")); !ok || rel != "sub/note.md" {
		t.Errorf("sub/note.md: got (%q, %v), want (\"sub/note.md\", true)", rel, ok)
	}
	if _, ok := nsRelPath(filepath.Join(t.TempDir(), "elsewhere.md")); ok {
		t.Error("path outside cwd: got ok=true, want false")
	}
	if _, ok := nsRelPath(".."); ok {
		t.Error("'..' escaping cwd: got ok=true, want false")
	}
}

func TestNsReadFile(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(parent, "local")
	if err := os.WriteFile(filepath.Join(cwd, "note.md"), []byte("hello from namespace\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)

	t.Run("no socket set", func(t *testing.T) {
		if _, ok := nsReadFile("note.md"); ok {
			t.Error("got ok=true with no $_9SH_UNIX_SOCK set, want false")
		}
	})

	startTestNamespace(t, parent)

	t.Run("reads through the namespace", func(t *testing.T) {
		data, ok := nsReadFile("note.md")
		if !ok {
			t.Fatal("got ok=false, want true")
		}
		if string(data) != "hello from namespace\n" {
			t.Errorf("got %q", data)
		}
	})

	t.Run("path outside cwd falls back", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "elsewhere.md")
		if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := nsReadFile(outside); ok {
			t.Error("got ok=true for a path outside cwd, want false")
		}
	})

	t.Run("nonexistent file falls back", func(t *testing.T) {
		if _, ok := nsReadFile("missing.md"); ok {
			t.Error("got ok=true for a nonexistent file, want false")
		}
	})
}

func TestNsSaveFile(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(parent, "local")
	t.Chdir(cwd)
	startTestNamespace(t, parent)

	t.Run("writes a new file", func(t *testing.T) {
		if !nsSaveFile("new.md", []byte("saved via namespace\n")) {
			t.Fatal("nsSaveFile: got ok=false, want true")
		}
		got, err := os.ReadFile(filepath.Join(cwd, "new.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "saved via namespace\n" {
			t.Errorf("got %q", got)
		}
	})

	t.Run("overwrites and preserves mode", func(t *testing.T) {
		target := filepath.Join(cwd, "existing.md")
		if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if !nsSaveFile("existing.md", []byte("new content\n")) {
			t.Fatal("nsSaveFile: got ok=false, want true")
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "new content\n" {
			t.Errorf("got %q", got)
		}
		fi, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
		}
	})

	t.Run("no stray temp file left behind", func(t *testing.T) {
		if !nsSaveFile("clean.md", []byte("x")) {
			t.Fatal("nsSaveFile: got ok=false, want true")
		}
		entries, err := os.ReadDir(cwd)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".9ed-") {
				t.Errorf("stray temp file left behind: %s", e.Name())
			}
		}
	})

	t.Run("path outside cwd falls back", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "elsewhere.md")
		if nsSaveFile(outside, []byte("x")) {
			t.Error("got ok=true for a path outside cwd, want false")
		}
	})
}
