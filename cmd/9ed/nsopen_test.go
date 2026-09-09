package main

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"reflect"
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

func TestNsPathElems(t *testing.T) {
	cases := []struct {
		path string
		want []string
	}{
		{"note.md", []string{"local", "note.md"}},
		{"./note.md", []string{"local", "note.md"}},
		{"sub/note.md", []string{"local", "sub", "note.md"}},
		{".", []string{"local"}},
		{"/config/config.ky", []string{"config", "config.ky"}},
		{"/", nil},
	}
	for _, c := range cases {
		got := nsPathElems(c.path)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("nsPathElems(%q) = %v, want %v", c.path, got, c.want)
		}
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
		if _, found, _ := nsReadFile("note.md"); found {
			t.Error("got found=true with no $_9SH_UNIX_SOCK set, want false")
		}
	})

	startTestNamespace(t, parent)

	t.Run("reads through the namespace", func(t *testing.T) {
		data, found, err := nsReadFile("note.md")
		if !found || err != nil {
			t.Fatalf("got (found=%v, err=%v), want (true, nil)", found, err)
		}
		if string(data) != "hello from namespace\n" {
			t.Errorf("got %q", data)
		}
	})

	t.Run("relative path whose parent doesn't resolve hard-fails, no cwd fallback", func(t *testing.T) {
		// "nosuchdir" has no entry under /local at all, so the parent
		// Walk itself fails — proving there's no fallback to a real
		// cwd-relative read for a relative path (see the package doc
		// comment: a 9sh native program never leans on cwd). Note this
		// is different from a relative path using ".." to walk to a real
		// sibling of /local within the namespace itself (dirfs backs
		// /local with an actual directory tree, so that "escape" stays
		// entirely inside the namespace) — only a parent that doesn't
		// exist anywhere the namespace can reach is the hard-fail case.
		if _, found, err := nsReadFile("nosuchdir/elsewhere.md"); !found || err == nil {
			t.Errorf("got (found=%v, err=%v), want (true, non-nil)", found, err)
		}
	})

	t.Run("absolute path outside the namespace falls back", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "elsewhere.md")
		if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, found, _ := nsReadFile(outside); found {
			t.Error("got found=true for an absolute path the namespace doesn't claim, want false")
		}
	})

	t.Run("nonexistent relative file reports ErrNotExist, not a fallback", func(t *testing.T) {
		_, found, err := nsReadFile("missing.md")
		if !found {
			t.Fatal("got found=false for a location the namespace does claim (/local), want true")
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("got err=%v, want fs.ErrNotExist", err)
		}
	})
}

func TestNsReadFileAbsPath(t *testing.T) {
	// A bind that lives somewhere other than /local — startTestNamespace
	// serves parent itself as the namespace root, so an absolute path
	// naming a sibling of "local" proves the walk isn't forced through
	// /local the way a relative path is.
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(parent, "elsewhere"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "elsewhere", "note.md"), []byte("hello from elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(parent, "local"))
	startTestNamespace(t, parent)

	data, found, err := nsReadFile("/elsewhere/note.md")
	if !found || err != nil {
		t.Fatalf("got (found=%v, err=%v), want (true, nil)", found, err)
	}
	if string(data) != "hello from elsewhere\n" {
		t.Errorf("got %q", data)
	}
}

func TestNsSaveFileRootRejected(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(parent, "local"))
	startTestNamespace(t, parent)

	if found, _ := nsSaveFile("/", []byte("x")); found {
		t.Error("got found=true saving to the namespace root, want false")
	}
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
		if found, err := nsSaveFile("new.md", []byte("saved via namespace\n")); !found || err != nil {
			t.Fatalf("got (found=%v, err=%v), want (true, nil)", found, err)
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
		if found, err := nsSaveFile("existing.md", []byte("new content\n")); !found || err != nil {
			t.Fatalf("got (found=%v, err=%v), want (true, nil)", found, err)
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
		if found, err := nsSaveFile("clean.md", []byte("x")); !found || err != nil {
			t.Fatalf("got (found=%v, err=%v), want (true, nil)", found, err)
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

	t.Run("absolute path outside the namespace falls back", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "elsewhere.md")
		if found, _ := nsSaveFile(outside, []byte("x")); found {
			t.Error("got found=true for an absolute path the namespace doesn't claim, want false")
		}
	})

	t.Run("relative path whose parent doesn't resolve hard-fails, no cwd fallback", func(t *testing.T) {
		if found, err := nsSaveFile("nosuchdir/elsewhere.md", []byte("x")); !found || err == nil {
			t.Errorf("got (found=%v, err=%v), want (true, non-nil)", found, err)
		}
	})
}
