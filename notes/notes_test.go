package notes

import "testing"

func TestSidecarPath(t *testing.T) {
	if got := SidecarPath("foo.go"); got != "foo.go.9an" {
		t.Errorf("SidecarPath(%q) = %q, want %q", "foo.go", got, "foo.go.9an")
	}
	if got := SidecarPath("parser.py"); got != "parser.py.9an" {
		t.Errorf("SidecarPath(%q) = %q, want %q", "parser.py", got, "parser.py.9an")
	}
}

func TestParse(t *testing.T) {
	src := "# func: func Foo() error\n" +
		"Why this exists...\n" +
		"\n" +
		"# type: type Bar struct\n" +
		"Invariant: never nil.\n"

	s := Parse([]byte(src))
	if got := s.Len(); got != 2 {
		t.Fatalf("Len() = %d, want 2", got)
	}
	if body, ok := s.Get("func", "func Foo() error"); !ok || body != "Why this exists..." {
		t.Errorf("Get(func, ...) = %q, %v, want %q, true", body, ok, "Why this exists...")
	}
	if body, ok := s.Get("type", "type Bar struct"); !ok || body != "Invariant: never nil." {
		t.Errorf("Get(type, ...) = %q, %v, want %q, true", body, ok, "Invariant: never nil.")
	}
	if _, ok := s.Get("func", "no such title"); ok {
		t.Error("Get for a missing card should report ok=false")
	}
}

func TestParseEmpty(t *testing.T) {
	if got := Parse(nil).Len(); got != 0 {
		t.Errorf("Parse(nil).Len() = %d, want 0", got)
	}
	if got := Parse([]byte("")).Len(); got != 0 {
		t.Errorf("Parse(\"\").Len() = %d, want 0", got)
	}
}

func TestParseIgnoresContentBeforeFirstHeading(t *testing.T) {
	src := "some stray preamble text\nmore of it\n\n# func: Foo\nnote body\n"
	s := Parse([]byte(src))
	if got := s.Len(); got != 1 {
		t.Fatalf("Len() = %d, want 1", got)
	}
	if body, ok := s.Get("func", "Foo"); !ok || body != "note body" {
		t.Errorf("Get(func, Foo) = %q, %v, want %q, true", body, ok, "note body")
	}
}

func TestParseEmptyTitle(t *testing.T) {
	src := "# heading: \nbody for a bare heading\n"
	s := Parse([]byte(src))
	if body, ok := s.Get("heading", ""); !ok || body != "body for a bare heading" {
		t.Errorf("Get(heading, \"\") = %q, %v, want %q, true", body, ok, "body for a bare heading")
	}
}

func TestParsePreservesInternalBlankLinesButTrimsEdges(t *testing.T) {
	src := "# func: Foo\n" +
		"\n" +
		"first paragraph\n" +
		"\n" +
		"second paragraph\n" +
		"\n" +
		"\n"
	s := Parse([]byte(src))
	want := "first paragraph\n\nsecond paragraph"
	if body, _ := s.Get("func", "Foo"); body != want {
		t.Errorf("Get(func, Foo) = %q, want %q", body, want)
	}
}

func TestSetUpdatesInPlacePreservingOrder(t *testing.T) {
	s := New()
	s.Set("func", "A", "note A")
	s.Set("func", "B", "note B")
	s.Set("func", "A", "note A, revised")

	got := string(s.Marshal())
	want := "# func: A\nnote A, revised\n\n# func: B\nnote B\n"
	if got != want {
		t.Errorf("Marshal() =\n%q\nwant\n%q", got, want)
	}
}

func TestDelete(t *testing.T) {
	s := New()
	s.Set("func", "A", "note A")
	s.Set("func", "B", "note B")
	s.Set("func", "C", "note C")

	if !s.Delete("func", "B") {
		t.Fatal("Delete(func, B) = false, want true")
	}
	if s.Delete("func", "B") {
		t.Error("Delete(func, B) a second time = true, want false")
	}
	if s.Len() != 2 {
		t.Fatalf("Len() = %d, want 2", s.Len())
	}
	// Order and indexing after a mid-list delete must both still work.
	if body, ok := s.Get("func", "C"); !ok || body != "note C" {
		t.Errorf("Get(func, C) after deleting B = %q, %v, want %q, true", body, ok, "note C")
	}
	want := "# func: A\nnote A\n\n# func: C\nnote C\n"
	if got := string(s.Marshal()); got != want {
		t.Errorf("Marshal() =\n%q\nwant\n%q", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	src := "# func: func Foo() error\n" +
		"Why this exists...\n" +
		"\n" +
		"# type: type Bar struct\n" +
		"Invariant: never nil.\n"

	s := Parse([]byte(src))
	if got := string(s.Marshal()); got != src {
		t.Errorf("round trip mismatch:\ngot:\n%q\nwant:\n%q", got, src)
	}
}

func TestNilSidecarIsSafeToRead(t *testing.T) {
	var s *Sidecar
	if got := s.Len(); got != 0 {
		t.Errorf("nil.Len() = %d, want 0", got)
	}
	if body, ok := s.Get("func", "Foo"); ok || body != "" {
		t.Errorf("nil.Get(func, Foo) = %q, %v, want \"\", false", body, ok)
	}
}

func TestMarshalEmptySidecar(t *testing.T) {
	if got := string(New().Marshal()); got != "" {
		t.Errorf("Marshal() on an empty Sidecar = %q, want \"\"", got)
	}
}
