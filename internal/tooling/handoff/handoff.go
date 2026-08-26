// The handoff note model and its on-disk markdown form (ADR-0032).
//
// A durable, per-branch checkpoint a session writes deliberately before
// ending, so a fresh session can read what came before instead of the
// rejected alternative of growing one session forever (ADR-0032 §4). Storage
// is internal/tooling/branchstore, shared with reviewjournal; this package is
// only the note's shape: front matter (branch, updated, head) plus a free
// markdown body, mirroring reviewjournal.Journal's own front-matter
// convention.

package handoff

import (
	"errors"
	"fmt"
	"strings"
)

// MaxBytes bounds the rendered note, front matter included (ADR-0032 §2).
// Measuring the rendered file rather than the body alone means front matter
// cannot be used to slip past the cap.
const MaxBytes = 8 * 1024

// ErrNoteTooLarge is Render's refusal when the rendered note would exceed
// MaxBytes. The caller must leave whatever was stored before untouched —
// silent truncation is the failure ADR-0032 §2 names outright.
var ErrNoteTooLarge = errors.New("handoff note exceeds the maximum size")

// Note is a per-branch handoff checkpoint. Updated and Head are stamped by
// the caller (the write path in `dg task handoff`), not by this package —
// Note itself has no clock and no git dependency, only shape.
type Note struct {
	Branch  string
	Updated string
	Head    string
	Body    string
}

// Render produces the on-disk markdown form: front matter, then the free
// body. It refuses with ErrNoteTooLarge, returning no content at all, when
// the RENDERED file — front matter included — would exceed MaxBytes.
func (n *Note) Render() (string, error) {
	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "branch: %s\n", n.Branch)
	if n.Updated != "" {
		fmt.Fprintf(&sb, "updated: %s\n", n.Updated)
	}
	if n.Head != "" {
		fmt.Fprintf(&sb, "head: %s\n", n.Head)
	}
	sb.WriteString("---\n")
	sb.WriteString(n.Body)

	rendered := sb.String()
	if len(rendered) > MaxBytes {
		return "", ErrNoteTooLarge
	}
	return rendered, nil
}

// Parse reads a note from its on-disk markdown form. A missing or empty
// input yields an empty note for branch — the frontmatter's own branch copy
// is informational, the same convention as reviewjournal.Parse: the caller's
// branch (the file's identity) always wins.
func Parse(branch string, data []byte) *Note {
	n := &Note{Branch: branch}
	lines := strings.Split(string(data), "\n")
	i := 0

	if i < len(lines) && strings.TrimSpace(lines[i]) == "---" {
		i++
		for ; i < len(lines) && strings.TrimSpace(lines[i]) != "---"; i++ {
			key, val, ok := strings.Cut(lines[i], ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(key) {
			case "updated":
				n.Updated = strings.TrimSpace(val)
			case "head":
				n.Head = strings.TrimSpace(val)
			}
		}
		if i < len(lines) {
			i++ // skip the closing "---"
		}
	}
	n.Body = strings.Join(lines[i:], "\n")
	return n
}
