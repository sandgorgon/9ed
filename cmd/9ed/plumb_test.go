package main

import "testing"

func TestParseFileLine(t *testing.T) {
	cases := []struct {
		arg      string
		wantPath string
		wantLine int
		wantHas  bool
	}{
		{"foo.go:42", "foo.go", 42, true},
		{"foo.go", "foo.go", 0, false},
		{"foo.go:", "foo.go:", 0, false},          // trailing colon, nothing after it
		{"foo.go:abc", "foo.go:abc", 0, false},     // not a number after the colon
		{"foo.go:0", "foo.go:0", 0, false},         // zero isn't a valid line number
		{"foo.go:-3", "foo.go:-3", 0, false},       // negative isn't valid either
		{"dir/sub/foo.go:7", "dir/sub/foo.go", 7, true},
		{"weird:name:9", "weird:name", 9, true}, // splits on the *last* colon
	}
	for _, c := range cases {
		path, line, hasLine := parseFileLine(c.arg)
		if path != c.wantPath || line != c.wantLine || hasLine != c.wantHas {
			t.Errorf("parseFileLine(%q) = (%q, %d, %v), want (%q, %d, %v)",
				c.arg, path, line, hasLine, c.wantPath, c.wantLine, c.wantHas)
		}
	}
}

func TestParseGotoRequest(t *testing.T) {
	t.Run("a bare line number has no path", func(t *testing.T) {
		path, line, err := parseGotoRequest(" 42 \n")
		if err != nil || path != "" || line != 42 {
			t.Errorf("parseGotoRequest(\" 42 \\n\") = (%q, %d, %v), want (\"\", 42, nil)", path, line, err)
		}
	})

	t.Run("path:line reports the path", func(t *testing.T) {
		path, line, err := parseGotoRequest("foo.go:7")
		if err != nil || path != "foo.go" || line != 7 {
			t.Errorf("parseGotoRequest(\"foo.go:7\") = (%q, %d, %v), want (\"foo.go\", 7, nil)", path, line, err)
		}
	})

	t.Run("garbage is an error", func(t *testing.T) {
		if _, _, err := parseGotoRequest("not a request"); err == nil {
			t.Error("parseGotoRequest on garbage returned nil error, want one")
		}
	})

	t.Run("zero or negative is an error, not a silently-accepted no-op line", func(t *testing.T) {
		if _, _, err := parseGotoRequest("0"); err == nil {
			t.Error("parseGotoRequest(\"0\") returned nil error, want one")
		}
	})
}

func TestSamePath(t *testing.T) {
	t.Run("identical strings match", func(t *testing.T) {
		if !samePath("foo.go", "foo.go") {
			t.Error("samePath(\"foo.go\", \"foo.go\") = false, want true")
		}
	})

	t.Run("an equivalent relative spelling matches", func(t *testing.T) {
		if !samePath("foo.go", "./foo.go") {
			t.Error("samePath(\"foo.go\", \"./foo.go\") = false, want true")
		}
	})

	t.Run("different files don't match", func(t *testing.T) {
		if samePath("foo.go", "bar.go") {
			t.Error("samePath(\"foo.go\", \"bar.go\") = true, want false")
		}
	})
}
