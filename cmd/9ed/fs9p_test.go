package main

import (
	"io"
	"net"
	"path/filepath"
	"testing"

	p9 "github.com/sandgorgon/9p"
	"github.com/sandgorgon/9p/client"
	"github.com/sandgorgon/9p/server"

	"github.com/sandgorgon/9ed/deck"
)

// startTestServer starts a real bufferFS over a Unix socket and
// returns a connected, attached *client.Client — an end-to-end check
// over the actual 9P wire protocol (marshal/unmarshal, walk, open,
// read), not just calling bufferFS's methods directly in-process,
// since a real 9P client (kyu, or a future unix-socket-aware 9pc —
// see sandgorgon/9p#3) is what will actually talk to this.
func startTestServer(t *testing.T, view *bufferView) *client.Client {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "test.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &server.Server{FS: &bufferFS{view: view}}
	go srv.Serve(l)
	t.Cleanup(func() { l.Close() })

	c, err := client.Dial("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if _, err := c.Attach("glenda", ""); err != nil {
		t.Fatal(err)
	}
	return c
}

func readFile(t *testing.T, c *client.Client, path string) string {
	t.Helper()
	f, err := c.Open(path, p9.OREAD)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestFS9P(t *testing.T) {
	src := []byte("package foo\n\nfunc F() {}\n")
	cards := deck.GoSegmenter{}.Segment(src) // [0]=preamble "package foo", [1]=func "func F() {}"
	view := &bufferView{}
	view.publish("f.go", src, cards, nil)

	c := startTestServer(t, view)

	if got := readFile(t, c, "/cards/1/title"); got != "func F() {}" {
		t.Errorf("/cards/1/title = %q, want %q", got, "func F() {}")
	}
	if got := readFile(t, c, "/cards/1/lang"); got != "func" {
		t.Errorf("/cards/1/lang = %q, want %q", got, "func")
	}
	if got := readFile(t, c, "/cards/1/body"); got != "func F() {}\n" {
		t.Errorf("/cards/1/body = %q, want %q", got, "func F() {}\n")
	}
	if got := readFile(t, c, "/tag"); got != "f.go 2-cards\n" {
		t.Errorf("/tag = %q, want %q", got, "f.go 2-cards\n")
	}

	// A live edit is visible over 9P without reconnecting — the point
	// of publish() decoupling the server from a fixed snapshot.
	view.publish("f.go", src, cards, map[int]string{1: "func F() { /* edited */ }"})
	if got := readFile(t, c, "/tag"); got != "f.go 2-cards unsaved\n" {
		t.Errorf("/tag after edit = %q, want a dirty marker", got)
	}
	if got := readFile(t, c, "/cards/1/body"); got != "func F() { /* edited */ }" {
		t.Errorf("/cards/1/body after edit = %q", got)
	}

	if _, err := c.Open("/cards/99/title", p9.OREAD); err == nil {
		t.Error("expected an error opening a nonexistent card, got nil")
	}
	if _, err := c.Open("/cards/1/body", p9.OWRITE); err == nil {
		t.Error("expected an error opening a card body for write (M5 is read-only), got nil")
	}
}

// TestBufferViewConcurrentAccess exercises publish/snapshot from two
// goroutines under -race: the real scenario this seam exists for is
// tui's event-loop goroutine publishing while the 9P server's
// goroutine(s) read concurrently.
func TestBufferViewConcurrentAccess(t *testing.T) {
	view := &bufferView{}
	src := []byte("package foo\n\nfunc F() {}\n")
	cards := deck.GoSegmenter{}.Segment(src)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			view.publish("f.go", src, cards, map[int]string{0: "edited"})
		}
	}()
	for range 200 {
		s := view.snapshot()
		_ = s.cardBody(0)
	}
	<-done
}
