// Package notes reads and writes 9ed's per-file annotation sidecar: one
// markdown file per source file, holding a user-authored note for any
// subset of its cards, keyed by (Kind, Title) rather than card index —
// see Sidecar's own doc comment for why.
package notes

import (
	"regexp"
	"sort"
	"strings"
)

// SidecarExt is the suffix appended to a source file's own name to get
// its sidecar's path — "foo.go" -> "foo.go.9an" — never a replacement
// of the source's extension, which would collide whenever two source
// files share a basename with different extensions (parser.go and
// parser.py, say).
const SidecarExt = ".9an"

// SidecarPath returns the sidecar path for a source file at path.
func SidecarPath(path string) string {
	return path + SidecarExt
}

// key identifies which card a note belongs to.
type key struct{ Kind, Title string }

// entry is one note, in the order it appears in the sidecar file. Flags
// is a set (the vocabulary — which names mean what — is entirely up to
// the caller; this package only stores strings) and may be non-empty
// even when Body is "": a flagged-but-otherwise-blank card is a real,
// meaningful state, not a placeholder.
type entry struct {
	Kind, Title string
	Body        string
	Flags       map[string]bool
}

// Sidecar holds every note for one source file, preserving the order
// notes appear in the file on disk (so a hand-edited ordering survives
// a round trip through Parse/Marshal).
//
// Keyed by a card's (Kind, Title), not its index into the source file's
// card list: an index shifts under card insertion/deletion (9ed already
// supports inserting a card mid-file), which would silently reattach a
// note to the wrong card. Title can go stale on a rename (the note is
// then orphaned, matching how a stale comment behaves elsewhere) — an
// accepted tradeoff, not a bug, given deck.Card has no identity that
// survives a reparse to key against instead.
type Sidecar struct {
	entries []entry
	index   map[key]int // key -> position in entries
}

// New returns an empty Sidecar.
func New() *Sidecar {
	return &Sidecar{index: map[key]int{}}
}

// Get returns the note body for the card identified by (kind, title),
// and whether one exists at all. A nil *Sidecar (a model's zero value,
// before any file's sidecar has been loaded) behaves as an empty one —
// every lookup simply reports ok=false, rather than every caller having
// to guard against a not-yet-loaded Sidecar itself.
func (s *Sidecar) Get(kind, title string) (body string, ok bool) {
	if s == nil {
		return "", false
	}
	i, ok := s.index[key{kind, title}]
	if !ok {
		return "", false
	}
	return s.entries[i].Body, true
}

// Set records body as the note for the card identified by (kind,
// title), overwriting any existing note for it in place (preserving its
// original position and any flags already set on it) or appending a new
// entry at the end. An empty body still records an entry — use Delete
// to actually remove one.
func (s *Sidecar) Set(kind, title, body string) {
	var flags map[string]bool
	if i, ok := s.index[key{kind, title}]; ok {
		flags = s.entries[i].Flags
	}
	s.setEntry(kind, title, body, flags)
}

// setEntry is Set's and Parse's shared insert-or-update-in-place logic.
func (s *Sidecar) setEntry(kind, title, body string, flags map[string]bool) {
	k := key{kind, title}
	if i, ok := s.index[k]; ok {
		s.entries[i].Body = body
		s.entries[i].Flags = flags
		return
	}
	s.index[k] = len(s.entries)
	s.entries = append(s.entries, entry{Kind: kind, Title: title, Body: body, Flags: flags})
}

// HasFlag reports whether the card identified by (kind, title) has flag
// set. The vocabulary of flag names is entirely up to the caller — this
// package has no notion of which names are meaningful. Nil-safe, like
// Get.
func (s *Sidecar) HasFlag(kind, title, flag string) bool {
	if s == nil {
		return false
	}
	i, ok := s.index[key{kind, title}]
	if !ok {
		return false
	}
	return s.entries[i].Flags[flag]
}

// ToggleFlag flips flag on or off for the card identified by (kind,
// title) — creating a new, body-less entry for it if none existed yet.
// If the result has neither the note body nor any flag left at all, the
// entry is removed entirely, the same "don't persist nothing" choice
// Delete makes explicitly and Set makes implicitly for an emptied body.
func (s *Sidecar) ToggleFlag(kind, title, flag string) {
	k := key{kind, title}
	i, ok := s.index[k]
	if !ok {
		s.index[k] = len(s.entries)
		s.entries = append(s.entries, entry{Kind: kind, Title: title, Flags: map[string]bool{flag: true}})
		return
	}
	e := &s.entries[i]
	if e.Flags == nil {
		e.Flags = map[string]bool{}
	}
	if e.Flags[flag] {
		delete(e.Flags, flag)
	} else {
		e.Flags[flag] = true
	}
	if e.Body == "" && len(e.Flags) == 0 {
		s.deleteAt(i)
	}
}

// Annotated reports whether the card identified by (kind, title) has
// any content at all — a non-empty note body, or at least one flag.
// Nil-safe, like Get.
func (s *Sidecar) Annotated(kind, title string) bool {
	if s == nil {
		return false
	}
	i, ok := s.index[key{kind, title}]
	if !ok {
		return false
	}
	e := s.entries[i]
	return e.Body != "" || len(e.Flags) > 0
}

// Delete removes the note for the card identified by (kind, title), if
// one exists, and reports whether it did.
func (s *Sidecar) Delete(kind, title string) bool {
	i, ok := s.index[key{kind, title}]
	if !ok {
		return false
	}
	s.deleteAt(i)
	return true
}

// deleteAt is Delete's and ToggleFlag's shared removal logic: splice
// entries[i] out and reindex everything after it.
func (s *Sidecar) deleteAt(i int) {
	k := key{s.entries[i].Kind, s.entries[i].Title}
	s.entries = append(s.entries[:i], s.entries[i+1:]...)
	delete(s.index, k)
	for j := i; j < len(s.entries); j++ {
		e := s.entries[j]
		s.index[key{e.Kind, e.Title}] = j
	}
}

// Len reports how many notes s holds. Nil-safe, like Get.
func (s *Sidecar) Len() int {
	if s == nil {
		return 0
	}
	return len(s.entries)
}

// sidecarHeading matches a top-level "# kind: title" heading line —
// kind is a single run of non-space characters (every deck.Card Kind
// value across every segmenter is one bare word), title is everything
// after the required ": " separator, up to end of line, and may itself
// be empty (a card whose own Title is "").
var sidecarHeading = regexp.MustCompile(`^# (\S+): (.*)$`)

// sidecarFlags matches the optional "flags: a, b, c" line directly
// under a heading. Kept off the heading line itself deliberately: a
// card's Title is frequently a source line that already contains '['/
// ']' (a Go slice return type, say — "func Foo() []int {"), so bracket-
// delimited flags there would be genuinely ambiguous to parse back out,
// not just ugly.
var sidecarFlags = regexp.MustCompile(`^flags: (.+)$`)

// Parse reads a sidecar file's contents. The format is deliberately
// plain markdown: each note is a section starting with a "# kind:
// title" heading (matching sidecarHeading), an optional "flags: ..."
// line directly under it (matching sidecarFlags), then its body, up to
// the next such heading or end of file — see Sidecar's own doc comment
// for why (kind, title) rather than index. Any content before the first
// heading is ignored (a sidecar is expected to open with one); a body's
// surrounding blank lines are trimmed, but its own internal blank lines
// and formatting are preserved verbatim. A body line that happens to
// look like its own "# kind: title" heading is indistinguishable from a
// real one and starts a new entry — a known, accepted ambiguity, the
// same class of imprecision the segmenters' own heuristics already
// carry. A sidecar written before flags existed has no "flags:" line at
// all, which parses exactly as before — nothing to migrate.
func Parse(data []byte) *Sidecar {
	s := New()
	lines := strings.Split(string(data), "\n")

	var kind, title string
	var body []string
	var flags map[string]bool
	haveEntry, atEntryStart := false, false
	flush := func() {
		if haveEntry {
			s.setEntry(kind, title, strings.Join(trimBlankEdges(body), "\n"), flags)
		}
	}

	for _, line := range lines {
		if m := sidecarHeading.FindStringSubmatch(line); m != nil {
			flush()
			kind, title = m[1], m[2]
			body, flags = nil, nil
			haveEntry, atEntryStart = true, true
			continue
		}
		if atEntryStart {
			atEntryStart = false
			if m := sidecarFlags.FindStringSubmatch(line); m != nil {
				flags = make(map[string]bool)
				for f := range strings.SplitSeq(m[1], ",") {
					if f = strings.TrimSpace(f); f != "" {
						flags[f] = true
					}
				}
				continue
			}
		}
		if haveEntry {
			body = append(body, line)
		}
	}
	flush()
	return s
}

// trimBlankEdges drops leading and trailing all-whitespace lines from
// lines, keeping any blank lines in between untouched.
func trimBlankEdges(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}

// Marshal serializes s back to its on-disk format, in the order its
// notes were added (Parse preserves the order read from disk; Set/
// ToggleFlag append a genuinely new note at the end) — one "# kind:
// title" heading per note, an optional "flags: ..." line (flag names
// sorted for deterministic output, regardless of map iteration or the
// order they were toggled in), then its body, and a single blank line
// separating it from the next entry. The result always ends in exactly
// one trailing newline, and is itself valid markdown (see Parse).
func (s *Sidecar) Marshal() []byte {
	var b strings.Builder
	for i, e := range s.entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("# ")
		b.WriteString(e.Kind)
		b.WriteString(": ")
		b.WriteString(e.Title)
		b.WriteByte('\n')
		if len(e.Flags) > 0 {
			names := make([]string, 0, len(e.Flags))
			for f := range e.Flags {
				names = append(names, f)
			}
			sort.Strings(names)
			b.WriteString("flags: ")
			b.WriteString(strings.Join(names, ", "))
			b.WriteByte('\n')
		}
		if e.Body != "" {
			b.WriteString(e.Body)
			b.WriteByte('\n')
		}
	}
	return []byte(b.String())
}
