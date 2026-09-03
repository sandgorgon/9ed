package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sandgorgon/tui/input"
)

// withRuntimeDir points runtimeDir() at a temp directory for the
// duration of the test by setting $XDG_RUNTIME_DIR — the only input
// runtimeDir() actually reads — and restores it after.
func withRuntimeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, had := os.LookupEnv("XDG_RUNTIME_DIR")
	os.Setenv("XDG_RUNTIME_DIR", dir)
	t.Cleanup(func() {
		if had {
			os.Setenv("XDG_RUNTIME_DIR", old)
		} else {
			os.Unsetenv("XDG_RUNTIME_DIR")
		}
	})
	return filepath.Join(dir, "9ed")
}

func writeDiscovery(t *testing.T, dir string, pid int, path string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)), []byte(path+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A same-named .sock file should never be mistaken for a discovery
	// entry — create one to prove discoverBuffers actually filters it.
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".sock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverBuffers(t *testing.T) {
	dir := withRuntimeDir(t)

	t.Run("no runtime dir yet is an empty list, not an error", func(t *testing.T) {
		if got := discoverBuffers(999); got != nil {
			t.Errorf("discoverBuffers() = %v, want nil", got)
		}
	})

	writeDiscovery(t, dir, 100, "foo.go")
	writeDiscovery(t, dir, 200, "bar.go")
	writeDiscovery(t, dir, 300, "baz.go")

	t.Run("lists every buffer except self, sorted by path", func(t *testing.T) {
		got := discoverBuffers(200) // exclude pid 200 (bar.go)
		want := []discoveredBuffer{{pid: 300, path: "baz.go"}, {pid: 100, path: "foo.go"}}
		if len(got) != len(want) {
			t.Fatalf("discoverBuffers(200) = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("entry %d = %v, want %v", i, got[i], want[i])
			}
		}
	})

	t.Run(".sock files are never mistaken for discovery entries", func(t *testing.T) {
		got := discoverBuffers(999)
		for _, b := range got {
			if b.path == "" {
				t.Errorf("got an entry with an empty path (likely misread a .sock file): %+v", b)
			}
		}
		if len(got) != 3 {
			t.Errorf("discoverBuffers(999) = %v, want exactly the 3 real discovery files", got)
		}
	})
}

func TestFmtBufferEntry(t *testing.T) {
	got := fmtBufferEntry(discoveredBuffer{pid: 42, path: "foo.go"})
	want := "foo.go  (pid 42)"
	if got != want {
		t.Errorf("fmtBufferEntry() = %q, want %q", got, want)
	}
}

func TestBufferPickerKeyEvent(t *testing.T) {
	t.Run("list view: j/k/enter/q route correctly", func(t *testing.T) {
		m := &model{}
		cases := []struct {
			ke   input.KeyEvent
			want any
		}{
			{input.KeyEvent{Rune: 'j'}, bufferPickerDownMsg{}},
			{input.KeyEvent{Key: input.KeyDown}, bufferPickerDownMsg{}},
			{input.KeyEvent{Rune: 'k'}, bufferPickerUpMsg{}},
			{input.KeyEvent{Key: input.KeyUp}, bufferPickerUpMsg{}},
			{input.KeyEvent{Key: input.KeyEnter}, bufferPickerEnterMsg{}},
			{input.KeyEvent{Rune: 'q'}, bufferPickerBackMsg{}},
		}
		for _, c := range cases {
			if got := m.bufferPickerKeyEvent(c.ke); got != c.want {
				t.Errorf("bufferPickerKeyEvent(%+v) = %#v, want %#v", c.ke, got, c.want)
			}
		}
	})

	t.Run("Esc is deliberately NOT mapped to leaving the list", func(t *testing.T) {
		// Regression guard for a bug found live in tmux: if Esc mapped
		// to bufferPickerBackMsg here too, the inspect view's own Esc
		// handling (Update's raw KeyEvent case, which clears
		// m.bufferInspect and returns to this list) would cause THIS
		// widget to mount for the first time within that same event —
		// and tui redelivers the same keystroke to a widget that just
		// became focused (Dispatch calls render() before handleInput
		// checks for one). That redelivered Esc would immediately hit
		// this case and close the picker right back out. See
		// bufferPickerKeyEvent's own doc comment for the full mechanism.
		m := &model{}
		if got := m.bufferPickerKeyEvent(input.KeyEvent{Key: input.KeyEsc}); got != nil {
			t.Errorf("bufferPickerKeyEvent(Esc) = %#v, want nil", got)
		}
	})
}

// TestBufferPickerInspectRawKeys exercises the inspect sub-view's key
// handling the way it's actually reached: as a raw input.KeyEvent
// through Update directly, since pickerView mounts no focused widget
// there to convert it via an onEvent callback first (see
// bufferPickerKeyEvent's doc comment, and the test right above this
// one). Caught live in tmux before this existed: Esc appeared to do
// nothing at all, because the handling had been written as dead code
// behind a widget callback that inspect mode never actually uses.
func TestBufferPickerInspectRawKeys(t *testing.T) {
	newInspecting := func() *model {
		return &model{
			pickingBuffers: true,
			bufferList:     []discoveredBuffer{{pid: 42, path: "foo.go"}},
			bufferInspect:  &bufferStatus{tag: "foo.go 1-cards"},
		}
	}

	t.Run("digits accumulate into plumbLine", func(t *testing.T) {
		m := newInspecting()
		mm, _ := m.Update(input.KeyEvent{Rune: '4'})
		m = mm.(*model)
		mm, _ = m.Update(input.KeyEvent{Rune: '2'})
		m = mm.(*model)
		if m.plumbLine != "42" {
			t.Errorf("plumbLine = %q, want \"42\"", m.plumbLine)
		}
	})

	t.Run("backspace shrinks plumbLine", func(t *testing.T) {
		m := newInspecting()
		m.plumbLine = "42"
		mm, _ := m.Update(input.KeyEvent{Key: input.KeyBackspace})
		m = mm.(*model)
		if m.plumbLine != "4" {
			t.Errorf("plumbLine = %q, want \"4\"", m.plumbLine)
		}
	})

	t.Run("Esc clears inspect state and steps back to the list", func(t *testing.T) {
		m := newInspecting()
		m.plumbLine, m.plumbResult = "42", "some result"
		mm, _ := m.Update(input.KeyEvent{Key: input.KeyEsc})
		m = mm.(*model)
		if m.bufferInspect != nil || m.plumbLine != "" || m.plumbResult != "" {
			t.Errorf("after Esc: bufferInspect=%v plumbLine=%q plumbResult=%q, want all cleared", m.bufferInspect, m.plumbLine, m.plumbResult)
		}
		if !m.pickingBuffers {
			t.Error("Esc from inspect also left the picker entirely, want just back to the list")
		}
	})

	t.Run("Enter with a valid line dispatches a plumb Cmd", func(t *testing.T) {
		m := newInspecting()
		m.plumbLine = "5"
		_, cmd := m.Update(input.KeyEvent{Key: input.KeyEnter})
		if cmd == nil {
			t.Error("expected a non-nil Cmd for Enter with a valid line number")
		}
	})

	t.Run("Enter with a non-numeric line reports an error, no Cmd", func(t *testing.T) {
		m := newInspecting()
		m.plumbLine = "abc"
		mm, cmd := m.Update(input.KeyEvent{Key: input.KeyEnter})
		m = mm.(*model)
		if cmd != nil {
			t.Error("expected nil Cmd for a non-numeric plumb line")
		}
		if m.plumbResult == "" {
			t.Error("expected plumbResult to explain the rejection")
		}
	})

	t.Run("Enter while unreachable is a no-op", func(t *testing.T) {
		m := newInspecting()
		m.bufferInspect.err = "connection refused"
		m.plumbLine = "5"
		if _, cmd := m.Update(input.KeyEvent{Key: input.KeyEnter}); cmd != nil {
			t.Error("expected nil Cmd when the inspected buffer is unreachable")
		}
	})
}

func TestBufferPickerUpdate(t *testing.T) {
	dir := withRuntimeDir(t)
	writeDiscovery(t, dir, 100, "foo.go")
	writeDiscovery(t, dir, 200, "bar.go")

	t.Run("startBufferPickerMsg populates the list, excluding self", func(t *testing.T) {
		m := &model{}
		mm, _ := m.Update(startBufferPickerMsg{})
		m = mm.(*model)
		if !m.pickingBuffers || len(m.bufferList) != 2 {
			t.Fatalf("pickingBuffers=%v bufferList=%v, want true and 2 entries", m.pickingBuffers, m.bufferList)
		}
	})

	t.Run("navigation is bounded", func(t *testing.T) {
		m := &model{bufferList: []discoveredBuffer{{pid: 1}, {pid: 2}}}
		mm, _ := m.Update(bufferPickerUpMsg{}) // already at 0
		m = mm.(*model)
		if m.bufferCursor != 0 {
			t.Errorf("cursor = %d, want 0 (bounded)", m.bufferCursor)
		}
		mm, _ = m.Update(bufferPickerDownMsg{})
		m = mm.(*model)
		mm, _ = m.Update(bufferPickerDownMsg{}) // already at the last entry
		m = mm.(*model)
		if m.bufferCursor != 1 {
			t.Errorf("cursor = %d, want 1 (bounded at the last entry)", m.bufferCursor)
		}
	})

	t.Run("Enter on an empty list is a no-op", func(t *testing.T) {
		m := &model{pickingBuffers: true}
		if _, cmd := m.Update(bufferPickerEnterMsg{}); cmd != nil {
			t.Error("expected nil Cmd for Enter on an empty buffer list")
		}
	})

	t.Run("bufferInspectedMsg records an error distinctly from a live tag", func(t *testing.T) {
		m := &model{}
		mm, _ := m.Update(bufferInspectedMsg{pid: 1, tag: "foo.go 1-cards"})
		m = mm.(*model)
		if m.bufferInspect == nil || m.bufferInspect.err != "" || m.bufferInspect.tag != "foo.go 1-cards" {
			t.Errorf("bufferInspect = %+v, want tag set, err empty", m.bufferInspect)
		}

		mm, _ = m.Update(bufferInspectedMsg{pid: 1, err: errPlaceholder})
		m = mm.(*model)
		if m.bufferInspect == nil || m.bufferInspect.err == "" {
			t.Errorf("bufferInspect = %+v, want a non-empty err", m.bufferInspect)
		}
	})

	t.Run("bufferPickerBackMsg (list view's 'q') leaves the picker entirely", func(t *testing.T) {
		m := &model{pickingBuffers: true}
		mm, _ := m.Update(bufferPickerBackMsg{})
		m = mm.(*model)
		if m.pickingBuffers {
			t.Error("pickingBuffers still true after bufferPickerBackMsg, want false")
		}
	})
}

// TestPickerSwallowsGlobalKeys regression-tests the same dual-dispatch
// hazard TestSearchSwallowsGlobalKeys (search_test.go) already caught
// for m.searching: pickerView's own List isn't a tui.RawKeyClaimer
// either, so every raw KeyEvent it sees also reaches Update directly.
// Without the m.pickingBuffers guard, 'q' backing out of the picker
// would also hit the bare-'q'-quits check and exit 9ed entirely.
func TestPickerSwallowsGlobalKeys(t *testing.T) {
	t.Run("'q' while picking buffers does not quit", func(t *testing.T) {
		m := &model{pickingBuffers: true}
		_, cmd := m.Update(input.KeyEvent{Rune: 'q'})
		if cmd != nil {
			t.Error("Update returned a non-nil Cmd for 'q' while picking buffers, want nil (no quit)")
		}
	})

	t.Run("'t' while picking buffers does not toggle the theme", func(t *testing.T) {
		m := &model{pickingBuffers: true}
		before := m.theme
		mm, _ := m.Update(input.KeyEvent{Rune: 't'})
		m = mm.(*model)
		if m.theme != before {
			t.Error("theme changed after 't' while picking buffers, want unchanged")
		}
	})
}

// errPlaceholder is a stand-in error for tests that only care that
// bufferInspectedMsg.err is non-nil, not its exact text.
var errPlaceholder = &placeholderErr{}

type placeholderErr struct{}

func (*placeholderErr) Error() string { return "placeholder error" }
