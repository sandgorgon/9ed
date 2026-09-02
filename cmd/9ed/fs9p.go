// This file implements 9ed's 9P-as-editor-API surface: a running
// buffer, served as:
//
//	/tag                status line: path, card count, dirty state
//	/goto               write-only; jump to a line (plumb.go, item 3)
//	/cards/<n>/title
//	/cards/<n>/body     writable as of M8 — see cardBodyFile
//	/cards/<n>/lang     the card's Kind (e.g. "func", "heading") — the
//	                    per-card structural role, not the file's
//	                    language (which is constant across every card
//	                    in one buffer and so isn't worth a field here)
//
// Modeled directly on 9vcs's vcsfs package (server.FileSystem +
// server.File, a dirFile/objFile split, hash-derived Qid.Path) — the
// one real difference is that vcsfs's objects are immutable and
// content-addressed, so it loads content once at Walk time; a card's
// body can change out from under a connected client, so objFile here
// snapshots content at Open instead, and nothing is cached longer than
// one open fid's lifetime.

package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"

	p9 "github.com/sandgorgon/9p"
	"github.com/sandgorgon/9p/server"
)

// bufferFS is one running buffer exposed over 9P.
type bufferFS struct {
	view *bufferView

	// writes carries a cardBodyFile's Close-time commit into the tui
	// event loop (see p9WriteMsg's doc comment) — nil for a bufferFS
	// that only ever needs to serve reads (tests that don't exercise
	// the write path), in which case /cards/<n>/body's Open(OWRITE)
	// itself reports the error rather than leaving Close to fail on a
	// nil channel send.
	writes chan<- p9WriteMsg

	// gotos is /goto's Close-time commit, the same shape as writes —
	// see p9GotoMsg's doc comment (plumb.go) and gotoFile below. Also
	// nil-safe the same way.
	gotos chan<- p9GotoMsg
}

// p9GotoMsg is gotoFile's Close-time "jump to this line" request —
// mirrors p9WriteMsg exactly (see its own doc comment for why result is
// unbuffered): sent to bufferFS.gotos and, on the other end, both the
// tui.Msg a listening Cmd turns it into for Update to apply (see
// main.go's waitForP9Goto/Update) and the channel element type.
type p9GotoMsg struct {
	line   int
	result chan<- error
}

// p9WriteMsg is cardBodyFile's Close-time "commit this card's new
// body" request: sent to bufferFS.writes and, on the other end, both
// the tui.Msg a listening Cmd turns it into for Update to apply (see
// main.go's waitForP9Write/Update) and the channel element type — one
// type serves both roles since tui.Msg is `any`. result is unbuffered
// and always read by the same Cmd goroutine that just sent the
// request, immediately after sending it, so an unbuffered chan error
// never risks blocking Update.
type p9WriteMsg struct {
	cardIdx int
	content []byte
	result  chan<- error
}

func (fs *bufferFS) Attach(ctx context.Context, uname, aname string) (server.File, error) {
	return &dirFile{fs: fs, kind: kindRoot}, nil
}

type dirKind int

const (
	kindRoot dirKind = iota
	kindCards
	kindCard
)

// dirFile is every directory in the tree: the root, /cards, and each
// /cards/<n>. cardIdx is only meaningful when kind == kindCard.
type dirFile struct {
	fs      *bufferFS
	kind    dirKind
	cardIdx int
}

func (d *dirFile) name() string {
	switch d.kind {
	case kindCards:
		return "cards"
	case kindCard:
		return strconv.Itoa(d.cardIdx)
	default:
		return "/"
	}
}

func (d *dirFile) Qid() p9.Qid {
	return p9.Qid{Type: p9.QTDIR, Path: qidPath(fmt.Sprintf("dir/%d/%d", d.kind, d.cardIdx))}
}

func (d *dirFile) Stat(ctx context.Context) (p9.Stat, error) {
	return p9.Stat{Qid: d.Qid(), Mode: p9.DMDIR | 0o555, Name: d.name()}, nil
}

func (d *dirFile) WStat(ctx context.Context, st p9.Stat) error {
	return fmt.Errorf("fs9p: %s is read-only", d.name())
}

func (d *dirFile) Walk(ctx context.Context, name string) (server.File, error) {
	switch d.kind {
	case kindRoot:
		switch name {
		case "tag":
			return &objFile{name: "tag", content: d.fs.tagContent}, nil
		case "goto":
			return &gotoFile{fs: d.fs}, nil
		case "cards":
			return &dirFile{fs: d.fs, kind: kindCards}, nil
		}

	case kindCards:
		n, err := strconv.Atoi(name)
		if err != nil || n < 0 || n >= len(d.fs.view.snapshot().cards) {
			return nil, fmt.Errorf("fs9p: no such card %q", name)
		}
		return &dirFile{fs: d.fs, kind: kindCard, cardIdx: n}, nil

	case kindCard:
		switch name {
		case "title":
			return &objFile{name: "title", content: d.cardField(cardTitle)}, nil
		case "body":
			return &cardBodyFile{fs: d.fs, cardIdx: d.cardIdx, content: d.cardField(cardBody)}, nil
		case "lang":
			return &objFile{name: "lang", content: d.cardField(cardLang)}, nil
		}
	}
	return nil, fmt.Errorf("fs9p: no such file %q", name)
}

type cardFieldKind int

const (
	cardTitle cardFieldKind = iota
	cardBody
	cardLang
)

// cardField returns a content func for this dirFile's card index and
// the requested field, snapshotting the buffer once, at Open time
// (see objFile.Open) — never at Walk time, since Walk can happen well
// before a client actually reads.
func (d *dirFile) cardField(field cardFieldKind) func() []byte {
	fs, idx := d.fs, d.cardIdx
	return func() []byte {
		s := fs.view.snapshot()
		if idx < 0 || idx >= len(s.cards) {
			return nil // the card vanished (a save resegmented the deck) between Walk and Open
		}
		switch field {
		case cardTitle:
			return []byte(s.cards[idx].Title)
		case cardLang:
			return []byte(s.cards[idx].Kind)
		default: // cardBody
			return []byte(s.cardBody(idx))
		}
	}
}

func (fs *bufferFS) tagContent() []byte {
	s := fs.view.snapshot()
	dirty := ""
	if len(s.edited) > 0 {
		dirty = " unsaved"
	}
	return fmt.Appendf(nil, "%s %d-cards%s\n", s.path, len(s.cards), dirty)
}

func (d *dirFile) Open(ctx context.Context, mode p9.Mode) error {
	if mode != p9.OREAD {
		return fmt.Errorf("fs9p: %s is read-only", d.name())
	}
	return nil
}

func (d *dirFile) Create(ctx context.Context, name string, perm p9.Mode, mode p9.Mode) (server.File, error) {
	return nil, fmt.Errorf("fs9p: %s is read-only", d.name())
}

func (d *dirFile) Read(ctx context.Context, offset int64, p []byte) (int, error) {
	var entries []p9.Stat
	switch d.kind {
	case kindRoot:
		entries = []p9.Stat{
			{Qid: p9.Qid{Type: p9.QTFILE}, Mode: 0o444, Name: "tag"},
			{Qid: p9.Qid{Type: p9.QTFILE}, Mode: 0o222, Name: "goto"},
			{Qid: p9.Qid{Type: p9.QTDIR}, Mode: p9.DMDIR | 0o555, Name: "cards"},
		}
	case kindCards:
		s := d.fs.view.snapshot()
		entries = make([]p9.Stat, len(s.cards))
		for i := range s.cards {
			entries[i] = p9.Stat{Qid: p9.Qid{Type: p9.QTDIR}, Mode: p9.DMDIR | 0o555, Name: strconv.Itoa(i)}
		}
	case kindCard:
		for _, name := range [...]string{"title", "body", "lang"} {
			entries = append(entries, p9.Stat{Qid: p9.Qid{Type: p9.QTFILE}, Mode: 0o444, Name: name})
		}
	}
	return server.MarshalDir(entries, offset, p)
}

func (d *dirFile) Write(ctx context.Context, offset int64, p []byte) (int, error) {
	return 0, fmt.Errorf("fs9p: %s is read-only", d.name())
}

func (d *dirFile) Remove(ctx context.Context) error {
	return fmt.Errorf("fs9p: %s is read-only", d.name())
}

func (d *dirFile) Close() error { return nil }

// objFile is a read-only leaf. content is called once, at Open, and
// cached for that fid's lifetime — see the package doc comment on why
// that differs from vcsfs's eager-at-Walk loading.
type objFile struct {
	name    string
	content func() []byte

	data []byte
}

func (f *objFile) Qid() p9.Qid { return p9.Qid{Type: p9.QTFILE, Path: qidPath("file/" + f.name)} }

func (f *objFile) Stat(ctx context.Context) (p9.Stat, error) {
	return p9.Stat{Qid: f.Qid(), Mode: 0o444, Length: uint64(len(f.data)), Name: f.name}, nil
}

func (f *objFile) WStat(ctx context.Context, st p9.Stat) error {
	return fmt.Errorf("fs9p: %s is read-only", f.name)
}

func (f *objFile) Walk(ctx context.Context, name string) (server.File, error) {
	return nil, fmt.Errorf("fs9p: %s is not a directory", f.name)
}

func (f *objFile) Open(ctx context.Context, mode p9.Mode) error {
	if mode != p9.OREAD {
		return fmt.Errorf("fs9p: %s is read-only", f.name)
	}
	f.data = f.content()
	return nil
}

func (f *objFile) Create(ctx context.Context, name string, perm p9.Mode, mode p9.Mode) (server.File, error) {
	return nil, fmt.Errorf("fs9p: %s is read-only", f.name)
}

func (f *objFile) Read(ctx context.Context, offset int64, p []byte) (int, error) {
	if offset >= int64(len(f.data)) {
		return 0, io.EOF
	}
	return copy(p, f.data[offset:]), nil
}

func (f *objFile) Write(ctx context.Context, offset int64, p []byte) (int, error) {
	return 0, fmt.Errorf("fs9p: %s is read-only", f.name)
}

func (f *objFile) Remove(ctx context.Context) error {
	return fmt.Errorf("fs9p: %s is read-only", f.name)
}

func (f *objFile) Close() error { return nil }

// cardBodyFile is /cards/<n>/body: readable exactly like objFile
// (content snapshotted at Open(OREAD)), but Open(OWRITE)/Open(ORDWR)
// also arms a write buffer that Close commits as the card's whole new
// body. There's no partial-write story — opening for write always
// starts from an empty buffer regardless of OTRUNC, so a client doing
// the obvious `9pc put localfile /cards/N/body` (which opens OWRITE,
// no OTRUNC, per 9p's own cmd/9pc) replaces the body outright, the
// same "whole snapshot, not an incremental patch" model the read side
// already uses.
type cardBodyFile struct {
	fs      *bufferFS
	cardIdx int
	content func() []byte // snapshot func for reads, same as objFile's

	data      []byte // read snapshot, taken at Open(OREAD)/Open(ORDWR)
	writeBuf  []byte // accumulated at Open(OWRITE)/Open(ORDWR), committed at Close
	writeMode bool
}

func (f *cardBodyFile) Qid() p9.Qid {
	return p9.Qid{Type: p9.QTFILE, Path: qidPath(fmt.Sprintf("file/body/%d", f.cardIdx))}
}

func (f *cardBodyFile) Stat(ctx context.Context) (p9.Stat, error) {
	return p9.Stat{Qid: f.Qid(), Mode: 0o644, Length: uint64(len(f.content())), Name: "body"}, nil
}

func (f *cardBodyFile) WStat(ctx context.Context, st p9.Stat) error {
	return fmt.Errorf("fs9p: body: WStat not supported")
}

func (f *cardBodyFile) Walk(ctx context.Context, name string) (server.File, error) {
	return nil, fmt.Errorf("fs9p: body is not a directory")
}

func (f *cardBodyFile) Open(ctx context.Context, mode p9.Mode) error {
	switch mode & 3 {
	case p9.OREAD:
		f.data = f.content()
	case p9.OWRITE, p9.ORDWR:
		if f.fs.writes == nil {
			return fmt.Errorf("fs9p: body: write support unavailable")
		}
		if mode&3 == p9.ORDWR {
			f.data = f.content()
		}
		f.writeMode = true
		f.writeBuf = nil
	default:
		return fmt.Errorf("fs9p: body: unsupported open mode %v", mode)
	}
	return nil
}

func (f *cardBodyFile) Create(ctx context.Context, name string, perm p9.Mode, mode p9.Mode) (server.File, error) {
	return nil, fmt.Errorf("fs9p: body is not a directory")
}

func (f *cardBodyFile) Read(ctx context.Context, offset int64, p []byte) (int, error) {
	if offset >= int64(len(f.data)) {
		return 0, io.EOF
	}
	return copy(p, f.data[offset:]), nil
}

// Write grows writeBuf to cover [offset, offset+len(p)) — the standard
// WriteAt-style semantics a real file gives a client that writes
// sequentially from 0, which is what both io.Copy (cmd/9pc's put) and
// a client writing the whole new body in one Twrite already do.
func (f *cardBodyFile) Write(ctx context.Context, offset int64, p []byte) (int, error) {
	if !f.writeMode {
		return 0, fmt.Errorf("fs9p: body: not open for writing")
	}
	end := offset + int64(len(p))
	if end > int64(len(f.writeBuf)) {
		grown := make([]byte, end)
		copy(grown, f.writeBuf)
		f.writeBuf = grown
	}
	copy(f.writeBuf[offset:end], p)
	return len(p), nil
}

func (f *cardBodyFile) Remove(ctx context.Context) error {
	return fmt.Errorf("fs9p: body: remove not supported")
}

// Close commits an armed write buffer as the card's new body, blocking
// until the tui event loop has actually applied it (see p9WriteMsg) —
// so a script's `9pc put ...; echo done` only prints done once the
// edit is really visible, not just handed off. A plain read-only
// Close (the common case) is a no-op, matching objFile.
func (f *cardBodyFile) Close() error {
	if !f.writeMode {
		return nil
	}
	result := make(chan error)
	f.fs.writes <- p9WriteMsg{cardIdx: f.cardIdx, content: f.writeBuf, result: result}
	return <-result
}

// gotoFile is /goto: write-only, accumulating a write buffer (parsed by
// parseGotoRequest, plumb.go) that Close commits as a jump request —
// the same accumulate-then-commit-on-Close shape as cardBodyFile, minus
// the read side (there's nothing meaningful to read back from a
// fire-and-forget control file).
type gotoFile struct {
	fs *bufferFS

	writeBuf  []byte
	writeMode bool
}

func (f *gotoFile) Qid() p9.Qid {
	return p9.Qid{Type: p9.QTFILE, Path: qidPath("file/goto")}
}

func (f *gotoFile) Stat(ctx context.Context) (p9.Stat, error) {
	return p9.Stat{Qid: f.Qid(), Mode: 0o222, Name: "goto"}, nil
}

func (f *gotoFile) WStat(ctx context.Context, st p9.Stat) error {
	return fmt.Errorf("fs9p: goto: WStat not supported")
}

func (f *gotoFile) Walk(ctx context.Context, name string) (server.File, error) {
	return nil, fmt.Errorf("fs9p: goto is not a directory")
}

func (f *gotoFile) Open(ctx context.Context, mode p9.Mode) error {
	switch mode & 3 {
	case p9.OWRITE, p9.ORDWR:
		if f.fs.gotos == nil {
			return fmt.Errorf("fs9p: goto: write support unavailable")
		}
		f.writeMode = true
		f.writeBuf = nil
	default:
		return fmt.Errorf("fs9p: goto: write-only")
	}
	return nil
}

func (f *gotoFile) Create(ctx context.Context, name string, perm p9.Mode, mode p9.Mode) (server.File, error) {
	return nil, fmt.Errorf("fs9p: goto is not a directory")
}

func (f *gotoFile) Read(ctx context.Context, offset int64, p []byte) (int, error) {
	return 0, fmt.Errorf("fs9p: goto: write-only")
}

// Write accumulates writeBuf, same WriteAt-style semantics as
// cardBodyFile.Write — see its own comment.
func (f *gotoFile) Write(ctx context.Context, offset int64, p []byte) (int, error) {
	if !f.writeMode {
		return 0, fmt.Errorf("fs9p: goto: not open for writing")
	}
	end := offset + int64(len(p))
	if end > int64(len(f.writeBuf)) {
		grown := make([]byte, end)
		copy(grown, f.writeBuf)
		f.writeBuf = grown
	}
	copy(f.writeBuf[offset:end], p)
	return len(p), nil
}

func (f *gotoFile) Remove(ctx context.Context) error {
	return fmt.Errorf("fs9p: goto: remove not supported")
}

// Close parses the written request and, unless it names a file other
// than the one already open — 9ed is single-buffer, so it genuinely
// can't act on that rather than silently ignoring it — commits it as a
// p9GotoMsg and blocks until the tui event loop has actually applied it
// (same blocking-Close contract as cardBodyFile.Close).
func (f *gotoFile) Close() error {
	if !f.writeMode {
		return nil
	}
	target, line, err := parseGotoRequest(string(f.writeBuf))
	if err != nil {
		return err
	}
	if target != "" {
		open := f.fs.view.snapshot().path
		if !samePath(target, open) {
			return fmt.Errorf("fs9p: goto: %q is not the open file (%q) — 9ed is single-buffer, can't switch files", target, open)
		}
	}
	result := make(chan error)
	f.fs.gotos <- p9GotoMsg{line: line, result: result}
	return <-result
}

func qidPath(key string) uint64 {
	sum := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint64(sum[:8])
}
