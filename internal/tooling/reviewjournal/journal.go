// The journal model and its on-disk markdown form (ADR-0012 §1).
//
// One file per branch, human-readable and hand-editable. Only this package
// writes it (the task commands route here), so the renderer defines the
// canonical shape and the parser is tolerant of hand edits inside an entry's
// text: any line it does not recognize as an entry head, a stamp, or a section
// header is kept as continuation text of the current entry — a hand edit can
// reword a note, never corrupt the file.

package reviewjournal

import (
	"fmt"
	"regexp"
	"strings"
)

// Resolutions a settled entry can carry. Kept as strings (not an enum type)
// because they travel through a CLI flag and a markdown file; ValidResolution
// is the single gate.
const (
	ResolutionRejected = "rejected"
	ResolutionAnswered = "answered"
	ResolutionFixed    = "fixed"
)

// ValidResolution reports whether s is one of rejected/answered/fixed.
func ValidResolution(s string) bool {
	return s == ResolutionRejected || s == ResolutionAnswered || s == ResolutionFixed
}

// AgentNotePrefix marks a settle note as the coding agent's own provisional
// rejection, never a human's (ADR-0017 §6). It is the single source of truth
// for the literal — code, tests, and docs must reference this constant
// rather than restate it, so the prefix cannot drift between the writer
// (the loop settling `rejected`) and the reader (Manager.Ratify stripping
// it).
const AgentNotePrefix = "agent: "

// Entry is one remembered exchange. Open entries have no Resolution; settled
// ones carry it plus the answer text. Cite is the human-facing location
// ("store.go:12" or empty for a design-level question); Blob is the hash of
// the cited FILE's content as reviewed (empty when Cite is empty), and Head
// is the short SHA at write time — context for humans, never the staleness
// signal (ADR-0012 §2).
type Entry struct {
	ID         string
	Resolution string // "" = open
	Cite       string
	Note       string
	Answer     string
	Blob       string
	Head       string
}

// Open reports whether the entry is still awaiting an answer.
func (e Entry) Open() bool { return e.Resolution == "" }

// CitedFile returns the file part of Cite ("store.go:12" -> "store.go"), or
// "" for a pathless entry.
func (e Entry) CitedFile() string {
	if e.Cite == "" {
		return ""
	}
	file, _, _ := strings.Cut(e.Cite, ":")
	return file
}

// Journal is one branch's review memory.
type Journal struct {
	Branch     string
	Base       string
	LastReview string
	Entries    []Entry
}

// nextID returns the next unused "n<seq>" id. Ids are never reused within a
// journal: the sequence is derived from the highest ever assigned, not from
// the entry count, so settling or hand-deleting entries cannot recycle an id
// into meaning a different exchange.
func (j *Journal) nextID() string {
	maxSeq := 0
	for _, e := range j.Entries {
		var seq int
		if _, err := fmt.Sscanf(e.ID, "n%d", &seq); err == nil && seq > maxSeq {
			maxSeq = seq
		}
	}
	return fmt.Sprintf("n%d", maxSeq+1)
}

// find returns a pointer to the entry with the given id, or nil.
func (j *Journal) find(id string) *Entry {
	for i := range j.Entries {
		if j.Entries[i].ID == id {
			return &j.Entries[i]
		}
	}
	return nil
}

// findOrErr is find with the "no such entry" error every settle/ratify/reopen
// refusal starts from, so the three write paths that look an id up don't
// each restate the message.
func (j *Journal) findOrErr(id string) (*Entry, error) {
	e := j.find(id)
	if e == nil {
		return nil, fmt.Errorf("no entry %s in the journal for branch %s", id, j.Branch)
	}
	return e, nil
}

// --- rendering ---

// Render produces the canonical on-disk markdown.
func (j *Journal) Render() string {
	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "branch: %s\n", j.Branch)
	if j.Base != "" {
		fmt.Fprintf(&sb, "base: %s\n", j.Base)
	}
	if j.LastReview != "" {
		fmt.Fprintf(&sb, "last_review: %s\n", j.LastReview)
	}
	sb.WriteString("---\n")

	writeEntry := func(e Entry) {
		sb.WriteString("- ")
		if e.Resolution != "" {
			fmt.Fprintf(&sb, "**%s** ", e.Resolution)
		}
		fmt.Fprintf(&sb, "[%s]", e.ID)
		if e.Cite != "" {
			fmt.Fprintf(&sb, " `%s`", e.Cite)
		}
		fmt.Fprintf(&sb, " — %s\n", strings.ReplaceAll(e.Note, "\n", "\n  "))
		if e.Answer != "" {
			fmt.Fprintf(&sb, "  answer: %s\n", strings.ReplaceAll(e.Answer, "\n", "\n  "))
		}
		if e.Blob != "" {
			fmt.Fprintf(&sb, "  (blob %s, head %s)\n", e.Blob, e.Head)
		} else if e.Head != "" {
			fmt.Fprintf(&sb, "  (head %s)\n", e.Head)
		}
	}

	settled := make([]Entry, 0, len(j.Entries))
	open := make([]Entry, 0, len(j.Entries))
	for _, e := range j.Entries {
		if e.Open() {
			open = append(open, e)
		} else {
			settled = append(settled, e)
		}
	}
	if len(settled) > 0 {
		sb.WriteString("\n## Settled\n\n")
		for _, e := range settled {
			writeEntry(e)
		}
	}
	if len(open) > 0 {
		sb.WriteString("\n## Open\n\n")
		for _, e := range open {
			writeEntry(e)
		}
	}
	return sb.String()
}

// --- parsing ---

// entryHead matches the first line of an entry:
//
//   - **rejected** [n1] `client.go:42` — note...
//   - [n4] — a pathless open question...
var entryHead = regexp.MustCompile(
	"^- (?:\\*\\*(rejected|answered|fixed)\\*\\* )?\\[(n\\d+)\\](?: `([^`]*)`)? — (.*)$",
)

// stampLine matches the trailing stamp: "(blob X, head Y)" or "(head Y)".
var stampLine = regexp.MustCompile(`^\((?:blob ([0-9a-f]+), )?head ([0-9a-f]+)\)$`)

// Parse reads a journal from its on-disk markdown form. A missing or empty
// input yields an empty journal (callers pass the branch, which is the
// identity — the frontmatter's copy is informational).
func Parse(branch string, data []byte) *Journal {
	j := &Journal{Branch: branch}
	lines := strings.Split(string(data), "\n")
	i := 0

	// Frontmatter.
	if i < len(lines) && strings.TrimSpace(lines[i]) == "---" {
		i++
		for ; i < len(lines) && strings.TrimSpace(lines[i]) != "---"; i++ {
			key, val, ok := strings.Cut(lines[i], ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "base":
				j.Base = strings.TrimSpace(val)
			case "last_review":
				j.LastReview = strings.TrimSpace(val)
			}
		}
		i++ // consume the closing ---
	}

	var cur *Entry
	flush := func() {
		if cur != nil {
			cur.Note = strings.TrimSpace(cur.Note)
			cur.Answer = strings.TrimSpace(cur.Answer)
			j.Entries = append(j.Entries, *cur)
			cur = nil
		}
	}

	for ; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") || trimmed == "" && cur == nil {
			// Section headers carry no state the entries don't already have
			// (Open() is derived from Resolution), so they only delimit.
			continue
		}
		if m := entryHead.FindStringSubmatch(line); m != nil {
			flush()
			cur = &Entry{Resolution: m[1], ID: m[2], Cite: m[3], Note: m[4]}
			continue
		}
		if cur == nil {
			continue // prose outside any entry (hand-added) is ignored
		}
		if m := stampLine.FindStringSubmatch(trimmed); m != nil {
			cur.Blob, cur.Head = m[1], m[2]
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "answer: "); ok {
			cur.Answer = rest
			continue
		}
		if trimmed == "" {
			flush()
			continue
		}
		// Continuation of whichever text block is being built.
		if cur.Answer != "" {
			cur.Answer += "\n" + trimmed
		} else {
			cur.Note += "\n" + trimmed
		}
	}
	flush()
	return j
}
