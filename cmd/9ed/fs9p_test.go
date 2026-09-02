package main

import (
	"fmt"
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
// see sandgorgon/9p#3) is what will actually talk to this. writes/gotos
// may be nil for a test that only exercises reads.
func startTestServer(t *testing.T, view *bufferView, writes chan<- p9WriteMsg, gotos chan<- p9GotoMsg) *client.Client {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "test.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &server.Server{FS: &bufferFS{view: view, writes: writes, gotos: gotos}}
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

// runP9GotoConsumer drives gotos the same way main.go's Update
// (p9GotoMsg case) does at the level fs9p_test.go cares about — ack the
// request, and forward its line onto received (buffered, so the test
// can inspect it after gotoFile.Close has already returned and
// consumed the original request off gotos) — without needing a full
// tui.App/model event loop. The model-level effect (goToLine actually
// moving the cursor) is covered separately by TestUpdateP9GotoMsg in
// goto_test.go, the same split TestFS9PWrite/TestUpdateP9WriteMsg
// already use. Runs until the test ends (t.Cleanup stops it by closing
// gotos).
func runP9GotoConsumer(t *testing.T, gotos chan p9GotoMsg) <-chan int {
	t.Helper()
	received := make(chan int, 8)
	go func() {
		for req := range gotos {
			received <- req.line
			req.result <- nil
		}
	}()
	t.Cleanup(func() { close(gotos) })
	return received
}

// runP9WriteConsumer drives writes the same way main.go's Update
// (p9WriteMsg case) does — apply the edit to edited, republish, ack —
// without needing a full tui.App/model event loop in the test. Runs
// until the test ends (t.Cleanup stops it by closing writes).
func runP9WriteConsumer(t *testing.T, view *bufferView, src []byte, cards []deck.Card, writes chan p9WriteMsg) {
	t.Helper()
	edited := make(map[int]string)
	go func() {
		for req := range writes {
			if req.cardIdx < 0 || req.cardIdx >= len(cards) {
				req.result <- fmt.Errorf("no such card %d", req.cardIdx)
				continue
			}
			edited[req.cardIdx] = string(req.content)
			view.publish(view.snapshot().path, src, cards, edited)
			req.result <- nil
		}
	}()
	t.Cleanup(func() { close(writes) })
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

	c := startTestServer(t, view, nil, nil)

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
		t.Error("expected an error opening a card body for write with no write consumer wired up (writes: nil), got nil")
	}
}

// TestFS9PWrite exercises M8's write path end-to-end over the real 9P
// wire protocol: open /cards/<n>/body for write, write new content,
// close (which blocks on the commit — see cardBodyFile.Close), and
// confirm the edit is visible both over 9P and in the edited map a
// real Update would have produced.
func TestFS9PWrite(t *testing.T) {
	src := []byte("package foo\n\nfunc F() {}\n")
	cards := deck.GoSegmenter{}.Segment(src) // [0]=preamble, [1]=func "func F() {}"
	view := &bufferView{}
	view.publish("f.go", src, cards, nil)

	writes := make(chan p9WriteMsg)
	runP9WriteConsumer(t, view, src, cards, writes)
	c := startTestServer(t, view, writes, nil)

	f, err := c.Open("/cards/1/body", p9.OWRITE)
	if err != nil {
		t.Fatalf("open for write: %v", err)
	}
	newBody := "func F() { /* edited over 9P */ }\n"
	if _, err := f.Write([]byte(newBody)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close (commit): %v", err)
	}

	if got := readFile(t, c, "/cards/1/body"); got != newBody {
		t.Errorf("/cards/1/body after write = %q, want %q", got, newBody)
	}
	if got := readFile(t, c, "/tag"); got != "f.go 2-cards unsaved\n" {
		t.Errorf("/tag after write = %q, want a dirty marker", got)
	}
}

// TestFS9PGoto exercises /goto (item 3, plumbing) end-to-end over the
// real 9P wire protocol: a bare line number always commits; a
// "path:line" naming the file already open commits too; one naming a
// *different* file is rejected with a clear error rather than silently
// ignored (confirmed with the user — see plumb.go's package comment)
// and never reaches the consumer at all.
func TestFS9PGoto(t *testing.T) {
	src := []byte("package foo\n\nfunc F() {}\n")
	cards := deck.GoSegmenter{}.Segment(src)
	view := &bufferView{}
	view.publish("f.go", src, cards, nil)

	gotos := make(chan p9GotoMsg, 1)
	received := runP9GotoConsumer(t, gotos)
	c := startTestServer(t, view, nil, gotos)

	writeGoto := func(content string) error {
		f, err := c.Open("/goto", p9.OWRITE)
		if err != nil {
			t.Fatalf("open /goto for write: %v", err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("write %q: %v", content, err)
		}
		return f.Close()
	}

	t.Run("a bare line number commits", func(t *testing.T) {
		if err := writeGoto("3"); err != nil {
			t.Errorf("Close() = %v, want nil", err)
		}
		select {
		case line := <-received:
			if line != 3 {
				t.Errorf("line = %d, want 3", line)
			}
		default:
			t.Error("consumer never received the request")
		}
	})

	t.Run("path:line naming the open file commits", func(t *testing.T) {
		if err := writeGoto("f.go:3"); err != nil {
			t.Errorf("Close() = %v, want nil", err)
		}
		select {
		case line := <-received:
			if line != 3 {
				t.Errorf("line = %d, want 3", line)
			}
		default:
			t.Error("consumer never received the request")
		}
	})

	t.Run("path:line naming a different file is rejected, not ignored", func(t *testing.T) {
		if err := writeGoto("other.go:3"); err == nil {
			t.Error("Close() = nil, want an error (mismatched file)")
		}
		select {
		case line := <-received:
			t.Errorf("consumer received line %d, want none — mismatched file should be rejected before it's ever sent", line)
		default:
		}
	})

	t.Run("garbage content is rejected", func(t *testing.T) {
		if err := writeGoto("not a line or path:line"); err == nil {
			t.Error("Close() = nil, want a parse error")
		}
	})

	t.Run("reading /goto fails — it's write-only", func(t *testing.T) {
		if _, err := c.Open("/goto", p9.OREAD); err == nil {
			t.Error("Open(OREAD) = nil, want an error")
		}
	})
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
