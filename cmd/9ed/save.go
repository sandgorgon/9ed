package main

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/sandgorgon/tui/tui"

	"github.com/sandgorgon/9ed/deck"
)

// reassemble rebuilds the whole file from cards, in order: each card's
// current text (its edited override, or its original src span if
// untouched), concatenated. This is only correct because every
// Segmenter guarantees cards cover [0, len(src)) exactly, contiguously,
// non-overlapping (see deck.Segmenter's doc comment) — reassembly never
// has to reason about gaps or overlaps, just walk the deck in order.
func (m *model) reassemble() []byte {
	var buf bytes.Buffer
	buf.Grow(len(m.src))
	for i, c := range m.cards {
		content := m.src[c.Span[0]:c.Span[1]]
		if v, ok := m.edited[i]; ok {
			content = []byte(v)
		}
		buf.Write(content)
		// A card's original span always ends at the next card's start,
		// which (for every current Segmenter) is a line boundary — so
		// content ending without '\n' only happens after an edit that
		// stripped the trailing newline (e.g. the user's last keystroke
		// left the cursor mid-line at the card's end). Without this,
		// that missing newline silently glues the next card's first
		// line onto this one — observed live: an edited var-block card
		// missing its trailing '\n' swallowed the following card
		// entirely into a comment once reassembled and re-parsed.
		if i < len(m.cards)-1 && len(content) > 0 && content[len(content)-1] != '\n' {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes()
}

// saveDoneMsg reports the outcome of a save Cmd (see saveCmd): either
// the freshly written+resegmented state to adopt, or an error to
// surface without touching any existing state.
type saveDoneMsg struct {
	src   []byte
	cards []deck.Card
	err   error
}

// saveCmd reassembles the current deck (synchronously — cheap, and
// Update is the only place it's safe to read m's fields) and returns a
// Cmd that does the actual disk I/O on its own goroutine, per
// tui.Model's contract that Update/View must never block on I/O
// directly.
func (m *model) saveCmd() tui.Cmd {
	path := m.path
	seg := m.seg
	newSrc := m.reassemble()
	return func() tui.Msg {
		if err := atomicWrite(path, newSrc); err != nil {
			return saveDoneMsg{err: err}
		}
		return saveDoneMsg{src: newSrc, cards: seg.Segment(newSrc)}
	}
}

// atomicWrite writes data to path by writing a temp file in the same
// directory (so the final rename is atomic — same filesystem) and
// renaming it into place, rather than truncating path directly: a
// crash or a full disk mid-write never leaves path partially
// overwritten. The temp file's permissions are set to match path's
// existing mode (falling back to 0644 for a brand-new file) so Save
// never silently strips e.g. an executable bit. No incremental writes
// ever happen outside this function — every 9ed Save is exactly one of
// these calls, matching the "no incremental disk writes" design
// decision.
func atomicWrite(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".9ed-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
