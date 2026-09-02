package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandgorgon/tui/input"

	"github.com/sandgorgon/9ed/deck"
	"github.com/sandgorgon/9ed/notes"
)

func noteTestModel() *model {
	cards := []deck.Card{
		{Kind: "preamble", Title: "package foo"},
		{Kind: "func", Title: "func Foo() error"},
	}
	return &model{path: "f.go", cards: cards, notesFile: notes.New(), view: &bufferView{}}
}

func TestEditNoteMsg(t *testing.T) {
	t.Run("enters note mode on a real card", func(t *testing.T) {
		m := noteTestModel()
		m.cursor = 1
		mm, _ := m.Update(editNoteMsg{})
		m = mm.(*model)
		if !m.noteEditing {
			t.Error("expected noteEditing = true")
		}
	})

	t.Run("refuses to enter note mode on an unsaved insert placeholder", func(t *testing.T) {
		m := noteTestModel()
		m.cards = append(m.cards, deck.Card{Kind: newCardKind, Title: ""})
		m.cursor = 2
		mm, _ := m.Update(editNoteMsg{})
		m = mm.(*model)
		if m.noteEditing {
			t.Error("expected noteEditing to stay false on a newCardKind placeholder")
		}
	})

	t.Run("no-op with zero cards", func(t *testing.T) {
		m := &model{notesFile: notes.New()}
		mm, _ := m.Update(editNoteMsg{})
		m = mm.(*model)
		if m.noteEditing {
			t.Error("expected noteEditing to stay false with no cards")
		}
	})
}

func TestToggleFlagMsg(t *testing.T) {
	t.Run("toggles a flag on the current card and marks it dirty", func(t *testing.T) {
		m := noteTestModel()
		m.cursor = 1
		mm, _ := m.Update(toggleFlagMsg{flag: flagTodo})
		m = mm.(*model)

		if !m.notesFile.HasFlag("func", "func Foo() error", flagTodo) {
			t.Error("expected flagTodo to be set")
		}
		if !m.noteEdited[1] {
			t.Error("expected noteEdited[1] = true")
		}
		if !m.isDirty(1) {
			t.Error("expected isDirty(1) = true after a flag toggle")
		}
	})

	t.Run("toggling twice clears it again", func(t *testing.T) {
		m := noteTestModel()
		m.cursor = 1
		mm, _ := m.Update(toggleFlagMsg{flag: flagTodo})
		m = mm.(*model)
		mm, _ = m.Update(toggleFlagMsg{flag: flagTodo})
		m = mm.(*model)

		if m.notesFile.HasFlag("func", "func Foo() error", flagTodo) {
			t.Error("expected flagTodo to be cleared after toggling twice")
		}
	})

	t.Run("refuses to toggle on an unsaved insert placeholder", func(t *testing.T) {
		m := noteTestModel()
		m.cards = append(m.cards, deck.Card{Kind: newCardKind, Title: ""})
		m.cursor = 2
		mm, _ := m.Update(toggleFlagMsg{flag: flagTodo})
		m = mm.(*model)

		if m.noteEdited[2] {
			t.Error("expected no toggle to have happened on a newCardKind placeholder")
		}
	})

	t.Run("a card with a flag and no note survives an empty Note-mode visit", func(t *testing.T) {
		m := noteTestModel()
		m.cursor = 1
		mm, _ := m.Update(toggleFlagMsg{flag: flagTodo})
		m = mm.(*model)

		// Open Note mode, type nothing, leave — must not wipe the flag
		// just because the body is (and stays) empty.
		m.noteEditing = true
		mm, _ = m.Update(input.KeyEvent{Key: input.KeyEsc})
		m = mm.(*model)

		if !m.notesFile.HasFlag("func", "func Foo() error", flagTodo) {
			t.Error("expected flagTodo to survive an empty Note-mode visit")
		}
	})
}

func TestNoteChangedMsg(t *testing.T) {
	m := noteTestModel()
	m.cursor = 1
	m.noteEditing = true

	mm, _ := m.Update(noteChangedMsg{value: "why this func exists"})
	m = mm.(*model)

	if body, ok := m.notesFile.Get("func", "func Foo() error"); !ok || body != "why this func exists" {
		t.Errorf("notesFile.Get after edit = %q, %v, want %q, true", body, ok, "why this func exists")
	}
	if !m.noteEdited[1] {
		t.Error("expected noteEdited[1] = true")
	}
	if !m.isDirty(1) {
		t.Error("expected isDirty(1) = true after a note edit")
	}
	if got := m.dirtyMark(); got != " [unsaved]" {
		t.Errorf("dirtyMark() = %q, want %q", got, " [unsaved]")
	}
}

func TestFinalizeNoteEditDropsAnEmptiedNote(t *testing.T) {
	m := noteTestModel()
	m.cursor = 1
	m.notesFile.Set("func", "func Foo() error", "will be cleared")
	m.noteEditing = true

	// Type it down to nothing, then Esc — mirrors what a real TextArea
	// OnChange sequence ending empty looks like.
	mm, _ := m.Update(noteChangedMsg{value: ""})
	m = mm.(*model)
	mm, _ = m.Update(input.KeyEvent{Key: input.KeyEsc})
	m = mm.(*model)

	if m.noteEditing {
		t.Error("expected noteEditing = false after Esc")
	}
	if _, ok := m.notesFile.Get("func", "func Foo() error"); ok {
		t.Error("expected the emptied note to be deleted, not kept as a blank entry")
	}
	// The deletion itself is still a change Save needs to persist.
	if !m.noteEdited[1] {
		t.Error("expected noteEdited[1] to stay true — the deletion still needs saving")
	}
}

func TestFinalizeNoteEditKeepsAnUntouchedNote(t *testing.T) {
	m := noteTestModel()
	m.cursor = 1
	m.notesFile.Set("func", "func Foo() error", "existing note")
	m.noteEditing = true

	// Esc with no noteChangedMsg in between — nothing was typed.
	mm, _ := m.Update(input.KeyEvent{Key: input.KeyEsc})
	m = mm.(*model)

	if body, ok := m.notesFile.Get("func", "func Foo() error"); !ok || body != "existing note" {
		t.Errorf("notesFile.Get after untouched Esc = %q, %v, want %q, true", body, ok, "existing note")
	}
}

func TestNoteEditingKeyEventOtherThanEscIsSwallowed(t *testing.T) {
	m := noteTestModel()
	m.cursor = 1
	m.noteEditing = true

	mm, _ := m.Update(input.KeyEvent{Rune: 'q'})
	m = mm.(*model)
	if !m.noteEditing {
		t.Error("'q' while note-editing must not quit or leave note mode — it's a literal character for the TextArea")
	}
}

// TestSaveCmdWritesSidecarOnlyWhenANoteChanged exercises saveCmd's real
// disk I/O (via atomicWrite), not just reassemble — the sidecar-writing
// half is new behavior with no prior test coverage of the file-writing
// path at all.
func TestSaveCmdWritesSidecarOnlyWhenANoteChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	src := []byte("package foo\n\nfunc Foo() error {\n\treturn nil\n}\n")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	cards := deck.GoSegmenter{}.Segment(src)

	t.Run("no note touched: no sidecar file written", func(t *testing.T) {
		m := newModel(path, src, deck.GoSegmenter{}, cards, nil, nil)
		msg := m.saveCmd()()
		if sd, ok := msg.(saveDoneMsg); !ok || sd.err != nil {
			t.Fatalf("saveCmd() = %#v, want a successful saveDoneMsg", msg)
		}
		if _, err := os.Stat(notes.SidecarPath(path)); !os.IsNotExist(err) {
			t.Errorf("expected no sidecar file, stat error = %v", err)
		}
	})

	t.Run("a note touched: sidecar file written with the marshaled content", func(t *testing.T) {
		m := newModel(path, src, deck.GoSegmenter{}, cards, nil, nil)
		m.cursor = 1
		m.noteEditing = true
		mm, _ := m.Update(noteChangedMsg{value: "why this exists"})
		m = mm.(*model)

		msg := m.saveCmd()()
		if sd, ok := msg.(saveDoneMsg); !ok || sd.err != nil {
			t.Fatalf("saveCmd() = %#v, want a successful saveDoneMsg", msg)
		}

		got, err := os.ReadFile(notes.SidecarPath(path))
		if err != nil {
			t.Fatalf("reading sidecar: %v", err)
		}
		want := m.notesFile.Marshal()
		if string(got) != string(want) {
			t.Errorf("sidecar on disk = %q, want %q", got, want)
		}
	})
}
