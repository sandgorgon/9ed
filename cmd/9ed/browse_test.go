package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgorgon/tui/input"
	"github.com/sandgorgon/tui/tui"
)

// newBrowseTestDir builds:
//
//	root/
//	  zeta.txt
//	  alpha.txt
//	  beta/
//	    inner.txt
//
// so listDir's dirs-first-then-alpha sort has both a case to prove
// (beta/ before alpha.txt/zeta.txt despite the alphabet) and a case to
// disprove a lazy "just sort names" implementation (alpha.txt before
// zeta.txt within the files group).
func newBrowseTestDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "zeta.txt"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "alpha.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "beta", "inner.txt"), []byte("i"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestListDir(t *testing.T) {
	root := newBrowseTestDir(t)

	got, err := listDir(root)
	if err != nil {
		t.Fatalf("listDir: %v", err)
	}
	want := []dirEntry{
		{name: "beta", isDir: true},
		{name: "alpha.txt", isDir: false},
		{name: "zeta.txt", isDir: false},
	}
	if len(got) != len(want) {
		t.Fatalf("listDir() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestListDirError(t *testing.T) {
	if _, err := listDir(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Error("listDir on a missing directory returned nil error, want one")
	}
}

func TestBrowseModelNavigation(t *testing.T) {
	root := newBrowseTestDir(t)
	m := newBrowseModel(root, &browseResult{})

	if len(m.entries) != 3 {
		t.Fatalf("entries = %v, want 3", m.entries)
	}

	mm, _ := m.Update(browseDown)
	m = mm.(*browseModel)
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}

	mm, _ = m.Update(browseDown)
	m = mm.(*browseModel)
	mm, _ = m.Update(browseDown) // already at the last entry
	m = mm.(*browseModel)
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2 (bounded at the last entry)", m.cursor)
	}

	mm, _ = m.Update(browseUp)
	m = mm.(*browseModel)
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1", m.cursor)
	}
}

func TestBrowseModelDescend(t *testing.T) {
	root := newBrowseTestDir(t)
	m := newBrowseModel(root, &browseResult{})
	// entries[0] is "beta" (dirs sort first).
	if !m.entries[0].isDir || m.entries[0].name != "beta" {
		t.Fatalf("entries[0] = %v, want the beta directory", m.entries[0])
	}

	mm, cmd := m.Update(browseEnterMsg{})
	m = mm.(*browseModel)
	if cmd != nil {
		t.Error("Update returned a non-nil Cmd descending into a directory, want nil (no quit)")
	}
	if m.cwd != filepath.Join(root, "beta") {
		t.Errorf("cwd = %q, want %q", m.cwd, filepath.Join(root, "beta"))
	}
	if len(m.entries) != 1 || m.entries[0].name != "inner.txt" {
		t.Errorf("entries = %v, want just inner.txt", m.entries)
	}
	if m.root != root {
		t.Errorf("root = %q, want unchanged %q", m.root, root)
	}
}

func TestBrowseModelChooseFile(t *testing.T) {
	root := newBrowseTestDir(t)
	m := newBrowseModel(root, &browseResult{})
	m.cursor = 1 // alpha.txt (dirs-first sort puts beta at 0)
	if m.entries[1].name != "alpha.txt" {
		t.Fatalf("entries[1] = %v, want alpha.txt", m.entries[1])
	}

	mm, cmd := m.Update(browseEnterMsg{})
	m = mm.(*browseModel)
	if cmd == nil {
		t.Fatal("Update returned a nil Cmd choosing a file, want tui.Quit()")
	}
	if !m.result.chosen || m.result.path != filepath.Join(root, "alpha.txt") {
		t.Errorf("result = %+v, want chosen=true path=%q", m.result, filepath.Join(root, "alpha.txt"))
	}
}

func TestBrowseModelEnterOnEmptyDirIsNoOp(t *testing.T) {
	root := t.TempDir() // no entries
	m := newBrowseModel(root, &browseResult{})
	mm, cmd := m.Update(browseEnterMsg{})
	m = mm.(*browseModel)
	if cmd != nil || m.result.chosen {
		t.Errorf("Update on an empty directory produced cmd=%v result=%+v, want no-op", cmd, m.result)
	}
}

func TestBrowseModelQuitKeys(t *testing.T) {
	t.Run("q quits without choosing", func(t *testing.T) {
		m := newBrowseModel(t.TempDir(), &browseResult{})
		_, cmd := m.Update(input.KeyEvent{Rune: 'q'})
		if cmd == nil {
			t.Error("Update('q') returned nil Cmd, want tui.Quit()")
		}
	})

	t.Run("Ctrl+C quits without choosing", func(t *testing.T) {
		m := newBrowseModel(t.TempDir(), &browseResult{})
		_, cmd := m.Update(input.KeyEvent{Mod: input.ModCtrl, Rune: 'c'})
		if cmd == nil {
			t.Error("Update(Ctrl+C) returned nil Cmd, want tui.Quit()")
		}
	})
}

func TestBrowseKeyEventRouting(t *testing.T) {
	m := &browseModel{}
	cases := []struct {
		ke   input.KeyEvent
		want tui.Msg
	}{
		{input.KeyEvent{Rune: 'j'}, browseDown},
		{input.KeyEvent{Key: input.KeyDown}, browseDown},
		{input.KeyEvent{Rune: 'k'}, browseUp},
		{input.KeyEvent{Key: input.KeyUp}, browseUp},
	}
	for _, c := range cases {
		if got := m.listEvent(c.ke); got != c.want {
			t.Errorf("listEvent(%+v) = %#v, want %#v", c.ke, got, c.want)
		}
	}
	if got, ok := m.listEvent(input.KeyEvent{Key: input.KeyEnter}).(browseEnterMsg); !ok {
		t.Errorf("listEvent(Enter) = %#v, want browseEnterMsg", got)
	}
}
