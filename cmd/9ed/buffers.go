// The cross-instance buffer picker (backlog item 8, reframed): 9ed
// treats a buffer as a running process, not a slot inside one process
// juggling several files (see namespace.go's serveBuffer — "two 9ed
// instances on the same file are two distinct buffers"). The
// infrastructure for finding *other* running buffers already existed
// (serveBuffer's own discovery file, written "for a future
// cross-instance picker" that was never built) — this file is that
// picker.
//
// 'b' from Nav mode lists every other 9ed buffer currently running for
// this user (discoverBuffers, reading runtimeDir() — no 9sh involved:
// that directory, the PID-based naming, and the Unix sockets are all
// plain OS-level 9ed infrastructure, present identically whether or
// not a 9sh session is in the picture). Selecting one dials its own 9P
// socket directly (the same way a real 9P client — kyu, a future
// unix-socket-aware 9pc — would) to show its live /tag, and from there
// a line number can be written to its /goto (see plumb.go, item 3) to
// jump its cursor remotely. Fire-and-forget: the result is only ever
// visible in *that* buffer's own terminal, a different process
// entirely — there's no way to "switch into" it from here, by design
// (see project memory: true multi-buffer-in-one-process was considered
// and explicitly not chosen).

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	p9 "github.com/sandgorgon/9p"
	"github.com/sandgorgon/9p/client"

	"github.com/sandgorgon/tui/input"
	"github.com/sandgorgon/tui/layout"
	"github.com/sandgorgon/tui/tui"
	"github.com/sandgorgon/tui/widget"
)

// discoveredBuffer is one entry from runtimeDir()'s discovery files.
type discoveredBuffer struct {
	pid  int
	path string
}

// discoverBuffers lists every other running 9ed buffer for this user,
// sorted by path. Best-effort: a runtimeDir that doesn't exist yet (no
// 9ed has ever run this boot) is an empty list, not an error; a
// discovery file that vanishes between the directory listing and
// reading it (that buffer just exited) is silently skipped, same
// reasoning. selfPID is excluded so a buffer never lists itself.
func discoverBuffers(selfPID int) []discoveredBuffer {
	dir := runtimeDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []discoveredBuffer
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".sock") {
			continue
		}
		pid, err := strconv.Atoi(name)
		if err != nil || pid == selfPID {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		out = append(out, discoveredBuffer{pid: pid, path: strings.TrimSpace(string(data))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

// dialBuffer connects directly to another 9ed buffer's own 9P server —
// a different target than nsopen.go's dialNamespace, which dials 9sh's
// namespace instead; the two are conceptually distinct (one buffer's
// whole filesystem root vs. a path within 9sh's /local), so this is its
// own small dial rather than a forced shared abstraction.
func dialBuffer(pid int) (*client.Client, *client.Fid, error) {
	sock := filepath.Join(runtimeDir(), strconv.Itoa(pid)+".sock")
	c, err := client.Dial("unix", sock)
	if err != nil {
		return nil, nil, err
	}
	root, err := c.Attach("9ed", "")
	if err != nil {
		c.Close()
		return nil, nil, err
	}
	return c, root, nil
}

// bufferInspectedMsg is inspectBufferCmd's result.
type bufferInspectedMsg struct {
	pid int
	tag string
	err error
}

// inspectBufferCmd dials pid's buffer and reads its /tag — run as a
// tui.Cmd (its own goroutine, per tui's runCmd), not called inline from
// Update, since a wedged peer's RPC could otherwise hang indefinitely;
// the same reasoning p9WriteMsg/p9GotoMsg's channel-based commit
// pattern already applies on the *server* side of this codebase,
// applied here on the *client* side instead.
func inspectBufferCmd(pid int) tui.Cmd {
	return func() tui.Msg {
		c, root, err := dialBuffer(pid)
		if err != nil {
			return bufferInspectedMsg{pid: pid, err: err}
		}
		defer c.Close()
		f, err := root.Walk("tag")
		if err != nil {
			return bufferInspectedMsg{pid: pid, err: err}
		}
		defer f.Clunk()
		file, err := f.OpenFile(p9.OREAD)
		if err != nil {
			return bufferInspectedMsg{pid: pid, err: err}
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			return bufferInspectedMsg{pid: pid, err: err}
		}
		return bufferInspectedMsg{pid: pid, tag: strings.TrimSpace(string(data))}
	}
}

// bufferPlumbedMsg is plumbBufferCmd's result.
type bufferPlumbedMsg struct {
	pid  int
	line int
	err  error
}

// plumbBufferCmd dials pid's buffer and writes line to its /goto — same
// "own goroutine via tui.Cmd" reasoning as inspectBufferCmd. A bare
// line number is always accepted by /goto regardless of which file it
// names (see plumb.go's parseGotoRequest) — there is no file-mismatch
// concern here the way a CLI "path:line" would have, since the picker
// already dialed the specific buffer the user chose.
func plumbBufferCmd(pid, line int) tui.Cmd {
	return func() tui.Msg {
		c, root, err := dialBuffer(pid)
		if err != nil {
			return bufferPlumbedMsg{pid: pid, line: line, err: err}
		}
		defer c.Close()
		f, err := root.Walk("goto")
		if err != nil {
			return bufferPlumbedMsg{pid: pid, line: line, err: err}
		}
		defer f.Clunk()
		file, err := f.OpenFile(p9.OWRITE)
		if err != nil {
			return bufferPlumbedMsg{pid: pid, line: line, err: err}
		}
		if _, err := file.Write([]byte(strconv.Itoa(line))); err != nil {
			file.Close()
			return bufferPlumbedMsg{pid: pid, line: line, err: err}
		}
		if err := file.Close(); err != nil {
			return bufferPlumbedMsg{pid: pid, line: line, err: err}
		}
		return bufferPlumbedMsg{pid: pid, line: line}
	}
}

// bufferStatus is the picker's view of one inspected buffer — nil until
// an inspectBufferCmd resolves.
type bufferStatus struct {
	tag string // /tag's content, e.g. "foo.go 3-cards unsaved"
	err string // set instead of tag if the dial/read failed
}

// fmtBufferEntry renders one list-view line: the path, and the pid in
// parens for disambiguation (two buffers can share a path — see
// serveBuffer's own comment — though that's rare in practice).
func fmtBufferEntry(b discoveredBuffer) string {
	return fmt.Sprintf("%s  (pid %d)", b.path, b.pid)
}

// startBufferPickerMsg is produced by listEvent on 'b' in Nav mode.
type startBufferPickerMsg struct{}
type bufferPickerUpMsg struct{}
type bufferPickerDownMsg struct{}
type bufferPickerEnterMsg struct{}

// bufferPickerBackMsg is 'q' from the list view, backing out of the
// picker to Nav — Esc's inspect-view counterpart ("back to the list")
// is handled directly in Update's raw-key case instead, since the
// inspect view mounts no focused widget for a Msg to come from (see
// bufferPickerKeyEvent's own comment on why this function only covers
// the list view at all).
type bufferPickerBackMsg struct{}

// bufferPickerKeyEvent is the list-view List widget's onEvent while
// model.pickingBuffers is true and no buffer is being inspected yet —
// mirrors searchKeyEvent's role for m.searching. It does NOT cover the
// inspect sub-view (typing a line number, Enter to plumb, Esc to go
// back): pickerView deliberately mounts no focused widget there (same
// reasoning as replaceView — see its own doc comment), so those keys
// are handled directly in Update's raw input.KeyEvent case instead,
// the only place they can reach.
func (m *model) bufferPickerKeyEvent(ke input.KeyEvent) tui.Msg {
	switch {
	// Deliberately 'q' only, NOT Esc, to leave the list — found live in
	// tmux, root-caused by reading tui/app.go's Dispatch: it calls
	// Update then render() synchronously, *before* handleInput checks
	// for a focused widget, so Esc-from-inspect (which clears
	// m.bufferInspect and, as a side effect, causes this very List to
	// mount for the first time within that same event) gets delivered
	// a *second* time to the List that transition just created — which,
	// if this case matched Esc too, would immediately close the picker
	// right back out again, one frame after the user asked to go back
	// to the list instead. Nav mode's own listEvent has no bare-Esc
	// case at all, which is exactly why that transition (editView back
	// to navView) never exhibited this failure mode — the accidental
	// redelivery lands on a key nothing there interprets.
	case ke.Rune == 'q':
		return bufferPickerBackMsg{}
	case ke.Key == input.KeyUp || ke.Rune == 'k':
		return bufferPickerUpMsg{}
	case ke.Key == input.KeyDown || ke.Rune == 'j':
		return bufferPickerDownMsg{}
	case ke.Key == input.KeyEnter:
		return bufferPickerEnterMsg{}
	}
	return nil
}

// pickerView renders the buffer picker: a plain list of other running
// buffers, or (once one's been entered) its live tag and a line-number
// prompt to plumb into it. No TextArea, same reasoning as replaceView —
// digits here are model-level field text, not something a focused
// widget should also be free to interpret.
func (m *model) pickerView() tui.Node {
	theme := m.theme

	if m.bufferInspect != nil {
		b := m.bufferList[m.bufferCursor]
		status := m.bufferInspect.tag
		if m.bufferInspect.err != "" {
			status = "unreachable: " + m.bufferInspect.err
		}
		header := tui.Text(fmt.Sprintf("pid %d: %s", b.pid, status), theme.MutedText())

		prompt := fmt.Sprintf("jump to line: %s", m.plumbLine)
		if m.plumbResult != "" {
			prompt += "   " + m.plumbResult
		}
		help := tui.Text(prompt+"  —  enter: jump   esc: back", m.helpStyle())

		return tui.Box(layout.Vertical,
			tui.Child(layout.Length(1), header),
			tui.Child(layout.Fill(1), tui.Box(layout.Vertical)),
			tui.Child(layout.Length(1), help),
		).Margin(1)
	}

	titles := make([]string, len(m.bufferList))
	for i, b := range m.bufferList {
		titles[i] = fmtBufferEntry(b)
	}
	list := widget.List(titles, m.bufferCursor, widget.ListOptions{Theme: theme}, func(e input.Event) tui.Msg {
		ke, ok := e.(input.KeyEvent)
		if !ok {
			return nil
		}
		return m.bufferPickerKeyEvent(ke)
	})

	status := fmt.Sprintf("other 9ed buffers: %d", len(m.bufferList))
	if m.bufferLoading {
		status += "   connecting..."
	}
	help := tui.Text(status+"  —  j/k: move   enter: inspect   q: back", theme.MutedText())

	return tui.Box(layout.Vertical,
		tui.Child(layout.Fill(1), list),
		tui.Child(layout.Length(1), help),
	).Margin(1)
}
