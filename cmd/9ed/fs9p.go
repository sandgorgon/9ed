// This file implements 9ed's 9P-as-editor-API surface: a running
// buffer, served read-only (for now — M5) as:
//
//	/tag                status line: path, card count, dirty state
//	/cards/<n>/title
//	/cards/<n>/body
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
			return &objFile{name: "body", content: d.cardField(cardBody)}, nil
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

func qidPath(key string) uint64 {
	sum := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint64(sum[:8])
}
