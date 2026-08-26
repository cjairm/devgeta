// Package contextreport measures what actually loads into an agent's
// session before the first prompt — the base-context cost devgeta cannot
// otherwise show a user (docs/plans/cycles/2026-08-25-token-and-context-efficiency.md,
// Step 7). Reported per agent, never merged: Claude Code and OpenCode load
// different trees, and one combined number would describe neither
// (cycle doc Step 7, "Rules the implementation must follow").
package contextreport

import (
	"fmt"
	"strings"
)

// Item is one file counted toward a Row's total: its resolved absolute
// path and the number of bytes actually charged against base context (not
// always the file's full size — see each row's Note for what was counted
// and why).
type Item struct {
	Path  string
	Bytes int
}

// Row is one layer of base context (memory, settings, skills, ...). Note
// states which precedence/counting rule was applied, per cycle doc Step 7:
// "Say which rule was applied per row."
type Row struct {
	Layer string
	Items []Item
	Note  string
}

// TotalBytes sums this row's items.
func (r Row) TotalBytes() int {
	total := 0
	for _, it := range r.Items {
		total += it.Bytes
	}
	return total
}

// Report is one agent's full base-context measurement.
type Report struct {
	Agent string
	Rows  []Row
}

// TotalBytes sums every row. Rows explicitly marked informational-only
// (their bytes already counted under another row, e.g. Hooks under
// Settings) report zero-byte Items on purpose, so this sum never
// double-counts.
func (r Report) TotalBytes() int {
	total := 0
	for _, row := range r.Rows {
		total += row.TotalBytes()
	}
	return total
}

// bytesPerTokenEstimate is a character-based approximation, not a real
// tokenizer (cycle doc Step 7: "Label the token figure an estimate in the
// output, with the method named"). 4 bytes/token is the commonly cited
// rule of thumb for English prose and code mixed together; it is not
// claimed to be more precise than that.
const bytesPerTokenEstimate = 4

// EstimatedTokens divides TotalBytes by bytesPerTokenEstimate. Always
// present the result as an estimate — see the constant's doc comment.
func (r Report) EstimatedTokens() int {
	return r.TotalBytes() / bytesPerTokenEstimate
}

// Render produces the human-readable report text `dg task context-report`
// prints: one section per agent, one line per row (byte count, item count,
// and the row's own note), a total, and the estimated-token figure
// explicitly labeled as an estimate with its method named (cycle doc Step 7).
func (r *Report) Render() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s base context\n", r.Agent)
	fmt.Fprintf(&sb, "%s\n", strings.Repeat("-", len(r.Agent)+len(" base context")))
	for _, row := range r.Rows {
		fmt.Fprintf(
			&sb,
			"  %-32s %8d bytes (%d files)\n",
			row.Layer,
			row.TotalBytes(),
			len(row.Items),
		)
		if row.Note != "" {
			fmt.Fprintf(&sb, "    %s\n", row.Note)
		}
	}
	fmt.Fprintf(&sb, "\n  Total: %d bytes\n", r.TotalBytes())
	fmt.Fprintf(
		&sb,
		"  Estimated tokens: ~%d (character-based estimate, %d bytes/token — not a real tokenizer)\n",
		r.EstimatedTokens(),
		bytesPerTokenEstimate,
	)
	return sb.String()
}
