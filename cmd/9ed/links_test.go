package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sandgorgon/9p/server"

	"github.com/sandgorgon/9ed/deck"
)

func TestLinkNamesFile(t *testing.T) {
	abs, _ := filepath.Abs("proj/deck/golang.go")
	tests := []struct {
		link string
		want bool
	}{
		{"golang.go", true},
		{"./golang.go", true},
		{"deck/golang.go", true},      // repo-root-relative suffix
		{"proj/deck/golang.go", true}, // longer suffix
		{abs, true},
		{"eck/golang.go", false}, // not a whole-component suffix
		{"other.go", false},
		{"../deck/golang.go", true}, // resolves back to the file
		{"../golang.go", false},
		{"~/golang.go", false},
		{"/golang.go", false},
	}
	for _, tt := range tests {
		if got := linkNamesFile(tt.link, "proj/deck/golang.go"); got != tt.want {
			t.Errorf("linkNamesFile(%q) = %v, want %v", tt.link, got, tt.want)
		}
	}
}

func newLinkTestModel() *model {
	src := []byte("package p\n\n" +
		"// Walrus follows Beta (@see Beta), see f.go:5, other.go:2,\n" +
		"// https://example.com/x and @see Missing.\n" +
		"func Walrus() {}\n\n" +
		"func Beta() {}\n")
	cards := deck.GoSegmenter{}.Segment(src) // 0: preamble, 1: Walrus, 2: Beta
	return &model{path: "f.go", src: src, cards: cards, view: &bufferView{}, cursor: 1}
}

func TestLinkPicker(t *testing.T) {
	m := newLinkTestModel()
	m.Update(startLinkPickerMsg{})
	if !m.pickingLinks || len(m.linkList) != 5 {
		t.Fatalf("pickingLinks=%v links=%d, want true and 5", m.pickingLinks, len(m.linkList))
	}
	byText := map[string]linkEntry{}
	for _, e := range m.linkList {
		byText[e.link.Text] = e
	}

	if e := byText["@see Beta"]; e.action != linkToCard || e.card != 2 {
		t.Errorf("@see Beta = %+v, want a jump to card 2", e)
	}
	if e := byText["f.go:5"]; e.action != linkToLine || e.line != 5 {
		t.Errorf("f.go:5 = %+v, want a jump to line 5 (it names the open file)", e)
	}
	if e := byText["other.go:2"]; e.action != linkToFile {
		t.Errorf("other.go:2 = %+v, want a cross-file link", e)
	}
	for _, text := range []string{"https://example.com/x", "@see Missing"} {
		if e := byText[text]; e.action != linkNoAction || e.note == "" {
			t.Errorf("%s = %+v, want unfollowable with a note", text, e)
		}
	}
}

func TestFollowLink(t *testing.T) {
	t.Run("an anchor moves the cursor and closes the picker", func(t *testing.T) {
		m := newLinkTestModel()
		m.pickingLinks = true
		m.linkList = []linkEntry{{action: linkToCard, card: 2}}
		m.followLink()
		if m.pickingLinks || m.cursor != 2 || m.editing {
			t.Errorf("pickingLinks=%v cursor=%d editing=%v, want false, 2, false", m.pickingLinks, m.cursor, m.editing)
		}
	})

	t.Run("a line opens the card containing it", func(t *testing.T) {
		m := newLinkTestModel()
		m.pickingLinks = true
		m.linkList = []linkEntry{{action: linkToLine, line: 7}}
		m.followLink()
		if m.pickingLinks || m.cursor != 2 || !m.editing {
			t.Errorf("pickingLinks=%v cursor=%d editing=%v, want false, 2 (Beta), true", m.pickingLinks, m.cursor, m.editing)
		}
	})

	t.Run("an unfollowable link stays open and says why", func(t *testing.T) {
		m := newLinkTestModel()
		m.pickingLinks = true
		m.linkList = []linkEntry{{action: linkNoAction, note: "another file"}}
		m.followLink()
		if !m.pickingLinks || m.linkStatus != "another file" || m.cursor != 1 {
			t.Errorf("pickingLinks=%v status=%q cursor=%d, want open, the note, unmoved", m.pickingLinks, m.linkStatus, m.cursor)
		}
	})
}

func TestLinkPickerSeesUnsavedEdits(t *testing.T) {
	m := newLinkTestModel()
	m.setEdited(2, "// @see Walrus\nfunc Beta() {}\n")
	m.cursor = 2
	m.Update(startLinkPickerMsg{})
	if len(m.linkList) != 1 || m.linkList[0].card != 1 {
		t.Errorf("linkList = %+v, want one link to card 1 from the unsaved edit", m.linkList)
	}
}

// writeTree creates files (name -> content) under a fresh temp dir and
// returns the dir.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// openTestModel builds a model for dir/rel the way run() would.
func openTestModel(t *testing.T, dir, rel string) *model {
	t.Helper()
	withRuntimeDir(t) // never see a real running 9ed's discovery files
	path := filepath.Join(dir, rel)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	seg := segmenterFor(path)
	m := newModel(path, src, seg, seg.Segment(src), nil, nil)
	m.cursor = 1
	return m
}

func followFirst(m *model, want linkAction) {
	m.Update(startLinkPickerMsg{})
	for i, e := range m.linkList {
		if e.action == want {
			m.linkCursor = i
			break
		}
	}
	m.followLink()
}

func TestCrossFileLink(t *testing.T) {
	files := map[string]string{
		"a/cmd/main.go":     "package main\n\n// see deck/g.go:5 and @see lib.go:Beta\nfunc Main() {}\n",
		"a/deck/g.go":       "package deck\n\nfunc One() {}\n\nfunc Two() {}\n\nfunc Three() {}\n",
		"a/cmd/lib.go":      "package main\n\nfunc Alpha() {}\n\nfunc Beta() {}\n",
		"a/cmd/main.go.9an": "",
	}
	delete(files, "a/cmd/main.go.9an")

	t.Run("opens a repo-root-relative path in place, at the line, and Ctrl+O returns", func(t *testing.T) {
		dir := writeTree(t, files)
		m := openTestModel(t, dir, "a/cmd/main.go")
		origPath := m.path

		m.Update(startLinkPickerMsg{})
		for i, e := range m.linkList {
			if e.link.Text == "deck/g.go:5" {
				m.linkCursor = i
			}
		}
		m.followLink()

		if m.pickingLinks || m.path != filepath.Join(dir, "a/deck/g.go") {
			t.Fatalf("pickingLinks=%v path=%q, want closed picker on deck/g.go (status %q)", m.pickingLinks, m.path, m.linkStatus)
		}
		if got := m.cursor; m.cards[got].Name != "Two" {
			t.Errorf("landed on card %q, want Two (line 5)", m.cards[got].Name)
		}
		if len(m.backStack) != 1 {
			t.Fatalf("backStack = %+v, want one frame", m.backStack)
		}

		m.editing = false
		m.goBack()
		if m.path != origPath || m.cursor != 1 || len(m.backStack) != 0 {
			t.Errorf("after back: path=%q cursor=%d stack=%d, want %q, 1, 0", m.path, m.cursor, len(m.backStack), origPath)
		}
	})

	t.Run("lands on a named card via file:Name", func(t *testing.T) {
		dir := writeTree(t, files)
		m := openTestModel(t, dir, "a/cmd/main.go")
		m.Update(startLinkPickerMsg{})
		for i, e := range m.linkList {
			if e.link.Text == "@see lib.go:Beta" {
				m.linkCursor = i
			}
		}
		m.followLink()
		if m.path != filepath.Join(dir, "a/cmd/lib.go") || m.cards[m.cursor].Name != "Beta" {
			t.Errorf("path=%q card=%q, want lib.go on Beta (status %q)", m.path, m.cards[m.cursor].Name, m.linkStatus)
		}
	})

	t.Run("refused while there are unsaved changes", func(t *testing.T) {
		dir := writeTree(t, files)
		m := openTestModel(t, dir, "a/cmd/main.go")
		orig := m.path
		m.setEdited(1, "// see deck/g.go:5\nfunc Main() {}\n")
		followFirst(m, linkToFile)
		if m.path != orig || !m.pickingLinks || m.linkStatus == "" || len(m.backStack) != 0 {
			t.Errorf("path=%q picking=%v status=%q stack=%d, want unchanged, open, a message, empty", m.path, m.pickingLinks, m.linkStatus, len(m.backStack))
		}
	})

	t.Run("a missing file or card leaves everything as it was", func(t *testing.T) {
		dir := writeTree(t, map[string]string{
			"main.go": "package p\n\n// see nope.go:1, @see lib.go:Missing\nfunc F() {}\n",
			"lib.go":  "package p\n\nfunc Alpha() {}\n",
		})
		m := openTestModel(t, dir, "main.go")
		orig := m.path
		m.Update(startLinkPickerMsg{})
		for i := range m.linkList {
			m.linkCursor = i
			m.followLink()
			if m.path != orig || !m.pickingLinks || m.linkStatus == "" || len(m.backStack) != 0 {
				t.Errorf("%s: path=%q picking=%v status=%q stack=%d", m.linkList[i].link.Text, m.path, m.pickingLinks, m.linkStatus, len(m.backStack))
			}
			m.linkStatus = ""
		}
	})

	t.Run("openFile resets per-file state but keeps the register", func(t *testing.T) {
		dir := writeTree(t, files)
		m := openTestModel(t, dir, "a/cmd/main.go")
		m.register = "yanked"
		m.activeSearch, m.cursorPos = "x", map[int]int{1: 3}
		followFirst(m, linkToFile)
		if m.activeSearch != "" || m.cursorPos != nil || m.edited != nil {
			t.Errorf("stale per-file state: search=%q cursorPos=%v edited=%v", m.activeSearch, m.cursorPos, m.edited)
		}
		if m.register != "yanked" {
			t.Errorf("register = %q, want it kept across files", m.register)
		}
	})

	t.Run("back with an empty stack is a notice, not a change", func(t *testing.T) {
		dir := writeTree(t, files)
		m := openTestModel(t, dir, "a/cmd/main.go")
		orig := m.path
		m.goBack()
		if m.path != orig || m.notice == "" {
			t.Errorf("path=%q notice=%q, want unchanged with a notice", m.path, m.notice)
		}
	})
}

// startPeer stands in for another running 9ed that has path open: a
// real 9P server on <runtimeDir>/<pid>.sock plus its discovery file.
// Returns the channel its /goto lines arrive on. pid is fake — nothing
// checks liveness beyond dialing the socket.
func startPeer(t *testing.T, pid int, path, ns string) <-chan int {
	t.Helper()
	// A Unix socket path is capped near 108 bytes, which t.TempDir()
	// under a long subtest name exceeds — so use a short runtime dir.
	short, err := os.MkdirTemp("", "h")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(short) })
	t.Setenv("XDG_RUNTIME_DIR", short)
	dir := runtimeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", filepath.Join(dir, strconv.Itoa(pid)+".sock"))
	if err != nil {
		t.Fatal(err)
	}
	gotos := make(chan p9GotoMsg)
	srv := &server.Server{FS: &bufferFS{view: &bufferView{}, gotos: gotos}}
	go srv.Serve(l)
	t.Cleanup(func() { l.Close() })
	// Not writeDiscovery: that also drops a placeholder .sock file,
	// which would clobber the real listener's socket.
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)), []byte(path+"\n"+nsLine(ns)), 0o600); err != nil {
		t.Fatal(err)
	}
	return runP9GotoConsumer(t, gotos)
}

func TestHandoffToOpenInstance(t *testing.T) {
	files := map[string]string{
		"main.go": "package main\n\n// see lib.go:4 and @see lib.go:Beta\nfunc Main() {}\n",
		"lib.go":  "package main\n\nfunc Alpha() {}\n\nfunc Beta() {}\n",
	}
	pick := func(m *model, text string) {
		m.Update(startLinkPickerMsg{})
		for i, e := range m.linkList {
			if e.link.Text == text {
				m.linkCursor = i
			}
		}
	}

	t.Run("hands a file:line link to the instance that has it open", func(t *testing.T) {
		dir := writeTree(t, files)
		m := openTestModel(t, dir, "main.go")
		orig := m.path
		got := startPeer(t, 424242, filepath.Join(dir, "lib.go"), "")

		pick(m, "lib.go:4")
		cmd := m.followLink()
		if cmd == nil {
			t.Fatalf("followLink returned no Cmd, want a hand-off (status %q)", m.linkStatus)
		}
		msg, ok := cmd().(linkHandoffMsg)
		if !ok || msg.err != nil {
			t.Fatalf("hand-off result = %#v, want success", msg)
		}
		if line := <-got; line != 4 {
			t.Errorf("peer /goto got line %d, want 4", line)
		}

		m.finishHandoff(msg)
		if m.path != orig || len(m.backStack) != 0 || m.pickingLinks || m.notice == "" {
			t.Errorf("path=%q stack=%d picking=%v notice=%q, want unchanged file, no frame, closed picker, a notice", m.path, len(m.backStack), m.pickingLinks, m.notice)
		}
	})

	t.Run("a card link becomes the card's first line, and unsaved changes don't block it", func(t *testing.T) {
		dir := writeTree(t, files)
		m := openTestModel(t, dir, "main.go")
		got := startPeer(t, 424242, filepath.Join(dir, "lib.go"), "")

		m.setEdited(1, "// see lib.go:4 and @see lib.go:Beta\nfunc Main() {}\n")
		pick(m, "@see lib.go:Beta")
		cmd := m.followLink()
		if cmd == nil {
			t.Fatalf("no hand-off Cmd, status %q", m.linkStatus)
		}
		cmd()
		if line := <-got; line != 5 {
			t.Errorf("peer /goto got line %d, want 5 (func Beta)", line)
		}
		if !m.hasUnsavedChanges() {
			t.Error("hand-off discarded this instance's unsaved edit")
		}
	})

	t.Run("an unreachable peer falls back to opening in place", func(t *testing.T) {
		dir := writeTree(t, files)
		m := openTestModel(t, dir, "main.go")
		writeDiscovery(t, runtimeDir(), 424243, filepath.Join(dir, "lib.go")) // no socket behind it

		pick(m, "lib.go:4")
		cmd := m.followLink()
		if cmd == nil {
			t.Fatalf("no hand-off Cmd, status %q", m.linkStatus)
		}
		msg := cmd().(linkHandoffMsg)
		if msg.err == nil {
			t.Fatal("hand-off to a socketless peer succeeded, want an error")
		}
		m.finishHandoff(msg)
		if m.path != filepath.Join(dir, "lib.go") || len(m.backStack) != 1 || m.notice == "" {
			t.Errorf("path=%q stack=%d notice=%q, want lib.go opened here with a frame and a notice", m.path, len(m.backStack), m.notice)
		}
	})

	t.Run("a relative advertised path is never matched", func(t *testing.T) {
		dir := writeTree(t, files)
		m := openTestModel(t, dir, "main.go")
		writeDiscovery(t, runtimeDir(), 424244, "lib.go")
		if pid, ok := findOpenBuffer(filepath.Join(dir, "lib.go")); ok {
			t.Errorf("findOpenBuffer matched pid %d on a relative path", pid)
		}
		_ = m
	})
}

func TestLocateLinkFileSkipsDirectories(t *testing.T) {
	parent := t.TempDir()
	cwd := filepath.Join(parent, "local")
	for name, content := range map[string]string{
		"pkg/main.go": "package pkg\n",
		"pkg/util.go": "package pkg\n",
		"top.md":      "# top\n",
		"util/keep":   "x", // makes "util" a directory
	} {
		p := filepath.Join(cwd, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(cwd)
	startTestNamespace(t, parent)
	m := &model{path: "pkg/main.go"}

	if p, _, err := m.locateLinkFile("util.go"); err != nil || p != "pkg/util.go" {
		t.Errorf("util.go = (%q, %v), want pkg/util.go", p, err)
	}
	if p, _, err := m.locateLinkFile("top.md"); err != nil || p != "top.md" {
		t.Errorf("top.md = (%q, %v), want top.md (found by climbing to /local)", p, err)
	}
	// "util" names a directory one level up; it must not open as a file.
	if p, data, err := m.locateLinkFile("util"); err == nil {
		t.Errorf("util = (%q, %d bytes), want not-found: it's a directory", p, len(data))
	}
}

// nsLine is a discovery file's optional second line.
func nsLine(ns string) string {
	if ns == "" {
		return ""
	}
	return ns + "\n"
}

func writePeerFile(t *testing.T, pid int, content string) {
	t.Helper()
	dir := runtimeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryNamespaceIdentity(t *testing.T) {
	t.Run("a discovery file carries the namespace only under 9sh", func(t *testing.T) {
		t.Setenv(nsSockEnv, "")
		abs, _ := filepath.Abs("x.go")
		if got, want := discoveryContent("x.go"), abs+"\n"; got != want {
			t.Errorf("outside 9sh: %q, want the unchanged single absolute-path line %q", got, want)
		}
		t.Setenv(nsSockEnv, "/tmp/9sh-1.sock")
		if got, want := discoveryContent("x.go"), "x.go\n/tmp/9sh-1.sock\n"; got != want {
			t.Errorf("under 9sh: %q, want path as given plus the socket %q", got, want)
		}
	})

	t.Run("discoverBuffers reads both formats", func(t *testing.T) {
		withRuntimeDir(t)
		writePeerFile(t, 101, "/a.go\n")
		writePeerFile(t, 102, "/b.go\n/tmp/9sh-1.sock\n")
		got := map[int]discoveredBuffer{}
		for _, b := range discoverBuffers(1) {
			got[b.pid] = b
		}
		if b := got[101]; b.path != "/a.go" || b.ns != "" {
			t.Errorf("old format = %+v, want path /a.go, no ns", b)
		}
		if b := got[102]; b.path != "/b.go" || b.ns != "/tmp/9sh-1.sock" {
			t.Errorf("new format = %+v, want path /b.go and its ns", b)
		}
	})

	const mine = "/tmp/9sh-mine.sock"
	tests := []struct {
		name     string
		ourNS    string
		peerFile string
		target   string
		want     bool
	}{
		{"outside 9sh, same absolute path", "", "/w/x.go\n", "/w/x.go", true},
		{"outside 9sh, relative target never matches", "", "x.go\n", "x.go", false},
		{"outside 9sh, relative peer never matches", "", "x.go\n", "/w/x.go", false},
		{"under 9sh, same namespace and path", mine, "/p/x.go\n" + mine + "\n", "/p/x.go", true},
		{"under 9sh, a different namespace", mine, "/p/x.go\n/tmp/9sh-other.sock\n", "/p/x.go", false},
		{"under 9sh, a peer with no namespace (old or non-9sh)", mine, "/p/x.go\n", "/p/x.go", false},
		{"outside 9sh, a peer that advertises a namespace", "", "/p/x.go\n" + mine + "\n", "/p/x.go", false},
		{"under 9sh, relative paths compare cleaned, in-namespace", mine, "./sub/x.go\n" + mine + "\n", "sub/x.go", true},
		{"under 9sh, different relative file", mine, "sub/y.go\n" + mine + "\n", "sub/x.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withRuntimeDir(t)
			t.Setenv(nsSockEnv, tt.ourNS)
			writePeerFile(t, 777, tt.peerFile)
			pid, ok := findOpenBuffer(tt.target)
			if ok != tt.want || (ok && pid != 777) {
				t.Errorf("findOpenBuffer(%q) = (%d, %v), want match=%v", tt.target, pid, ok, tt.want)
			}
		})
	}
}

// TestHandoffUnderNamespace drives the whole link follow against a real
// namespace server, with relative paths as 9sh actually produces them.
func TestHandoffUnderNS(t *testing.T) {
	parent := t.TempDir()
	cwd := filepath.Join(parent, "local")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(cwd, "main.go"), []byte("package main\n\n// see lib.go:4\nfunc Main() {}\n"), 0o644)
	os.WriteFile(filepath.Join(cwd, "lib.go"), []byte("package main\n\nfunc A() {}\n\nfunc B() {}\n"), 0o644)
	t.Chdir(cwd)
	startTestNamespace(t, parent)
	ns := os.Getenv(nsSockEnv)

	newModelNS := func() *model {
		src, err := readFileNS("main.go")
		if err != nil {
			t.Fatal(err)
		}
		seg := segmenterFor("main.go")
		m := newModel("main.go", src, seg, seg.Segment(src), nil, nil)
		m.cursor = 1
		m.Update(startLinkPickerMsg{})
		if len(m.linkList) != 1 || m.linkList[0].action != linkToFile {
			t.Fatalf("linkList = %+v, want one cross-file link", m.linkList)
		}
		return m
	}

	t.Run("same namespace: handed off, relative path and all", func(t *testing.T) {
		m := newModelNS()
		got := startPeer(t, 424250, "lib.go", ns)
		cmd := m.followLink()
		if cmd == nil {
			t.Fatalf("no hand-off Cmd, status %q", m.linkStatus)
		}
		if msg := cmd().(linkHandoffMsg); msg.err != nil {
			t.Fatalf("hand-off failed: %v", msg.err)
		}
		if line := <-got; line != 4 {
			t.Errorf("peer got line %d, want 4", line)
		}
		if m.path != "main.go" {
			t.Errorf("path = %q, want unchanged", m.path)
		}
	})

	t.Run("another session's namespace: not handed off, opened here", func(t *testing.T) {
		m := newModelNS()
		got := startPeer(t, 424251, "lib.go", "/tmp/9sh-someone-else.sock")
		if cmd := m.followLink(); cmd != nil {
			t.Fatalf("got a hand-off Cmd to a peer in another namespace")
		}
		select {
		case line := <-got:
			t.Errorf("the other session's instance was plumbed (line %d)", line)
		default:
		}
		if m.path != "lib.go" || len(m.backStack) != 1 {
			t.Errorf("path=%q stack=%d, want lib.go opened in place", m.path, len(m.backStack))
		}
	})
}
