// The review-notes / review-note tasks: an agent's read and write access to
// the per-branch review journal (ADR-0012).
//
// These are the ONLY way a reviewer touches the journal. The agents stay
// read-only on the filesystem (their `edit: deny` permission is unchanged) and
// the blob/head stamping, staleness, and atomic writes all stay here in tested
// Go rather than being described in three prompts.

package task

import (
	"fmt"
	"strings"

	"github.com/cjairm/devgeta/internal/tooling/reviewjournal"
)

// Sentinels callers match verbatim (see docs/guides/task-design.md: an empty
// result always gets a sentinel, because empty stdout is ambiguous to an agent).
const (
	noBranchSentinel = "No branch — review notes are keyed by branch."
	noNotesFmt       = "No review notes for branch %s."
	nothingPrunedMsg = "No review journals to prune."
)

// journalBranch resolves the branch a journal belongs to: the explicit
// --branch when given, else the current one. A detached HEAD has no branch, so
// there is no journal — reported as the sentinel rather than invented from a
// SHA, which would create journals nothing ever cleans (ADR-0012 §5).
func (tm *TaskManager) journalBranch(branch string) (string, error) {
	if strings.TrimSpace(branch) != "" {
		return branch, nil
	}
	current, err := tm.Git.CurrentBranchIn("")
	if err != nil {
		return "", fmt.Errorf("review notes: %w", err)
	}
	if strings.TrimSpace(current) == "" {
		return "", nil
	}
	return current, nil
}

// ReviewNotes prints branch's journal with each entry's freshness resolved
// against the current working tree.
func (tm *TaskManager) ReviewNotes(branch string, showPath, prune bool) (string, error) {
	jm := reviewjournal.New(tm.Git)

	if prune {
		removed, err := jm.Prune("")
		if err != nil {
			return "", err
		}
		if len(removed) == 0 {
			return nothingPrunedMsg, nil
		}
		var sb strings.Builder
		for _, b := range removed {
			fmt.Fprintf(&sb, "Pruned %s\n", b)
		}
		return strings.TrimRight(sb.String(), "\n"), nil
	}

	target, err := tm.journalBranch(branch)
	if err != nil {
		return "", err
	}
	if target == "" {
		return noBranchSentinel, nil
	}

	if showPath {
		return jm.PathFor("", target)
	}

	j, err := jm.Load("", target)
	if err != nil {
		return "", err
	}
	if len(j.Entries) == 0 {
		return fmt.Sprintf(noNotesFmt, target), nil
	}
	return renderNotes(jm, j), nil
}

// renderNotes formats the journal for an agent: labeled plain text, one entry
// per block, freshness already decided so the reader never re-derives it.
// Markdown scaffolding is deliberately absent (task-design.md output rule 1).
func renderNotes(jm *reviewjournal.Manager, j *reviewjournal.Journal) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "branch: %s\n", j.Branch)
	if j.Base != "" {
		fmt.Fprintf(&sb, "base: %s\n", j.Base)
	}
	if j.LastReview != "" {
		fmt.Fprintf(&sb, "last-review: %s\n", j.LastReview)
	}

	emit := func(title string, want bool) {
		first := true
		for _, e := range j.Entries {
			if e.Open() != want {
				continue
			}
			if first {
				fmt.Fprintf(&sb, "\n%s\n", title)
				first = false
			}
			sb.WriteString("- " + e.ID)
			if e.Resolution != "" {
				sb.WriteString(" " + e.Resolution)
			}
			if e.Cite != "" {
				sb.WriteString(" " + e.Cite)
			}
			switch jm.Verdict("", e) {
			case reviewjournal.FreshnessStale:
				sb.WriteString(" [STALE: the cited file changed since this was judged" +
					" — re-check before trusting it]")
			case reviewjournal.FreshnessFresh:
				sb.WriteString(" [fresh]")
			}
			fmt.Fprintf(&sb, "\n  %s\n", indentBlock(e.Note))
			if e.Answer != "" {
				fmt.Fprintf(&sb, "  answer: %s\n", indentBlock(e.Answer))
			}
		}
	}
	emit("settled:", false)
	emit("open:", true)
	return strings.TrimRight(sb.String(), "\n")
}

// indentBlock keeps a multi-line note readable under its entry.
func indentBlock(s string) string {
	return strings.ReplaceAll(s, "\n", "\n  ")
}

// ReviewNoteOpen records an open question or finding, echoing its new id so the
// caller can settle it later without re-reading the journal (task-design.md
// mutation rule: verb + target, one line).
func (tm *TaskManager) ReviewNoteOpen(branch, cite, note string) (string, error) {
	if strings.TrimSpace(note) == "" {
		return "", fmt.Errorf("--note is required and cannot be empty")
	}
	target, err := tm.journalBranch(branch)
	if err != nil {
		return "", err
	}
	if target == "" {
		return "", fmt.Errorf("%s", noBranchSentinel)
	}
	id, err := reviewjournal.New(tm.Git).Open("", target, cite, note)
	if err != nil {
		return "", err
	}
	return "Noted " + id, nil
}

// ReviewNoteSettle settles an entry. With an id it closes that open entry (the
// cite and its blob stamp carry over, so an answer can never retarget the
// question it closes); without one it records an exchange that was never open.
func (tm *TaskManager) ReviewNoteSettle(
	branch, id, resolution, cite, note string,
) (string, error) {
	if !reviewjournal.ValidResolution(resolution) {
		return "", fmt.Errorf(
			"--as must be rejected, answered, or fixed (got %q)",
			resolution,
		)
	}
	if strings.TrimSpace(note) == "" {
		return "", fmt.Errorf("--note is required and cannot be empty")
	}
	target, err := tm.journalBranch(branch)
	if err != nil {
		return "", err
	}
	if target == "" {
		return "", fmt.Errorf("%s", noBranchSentinel)
	}

	jm := reviewjournal.New(tm.Git)
	if strings.TrimSpace(id) != "" {
		if cite != "" {
			return "", fmt.Errorf(
				"--at cannot be combined with --settle %s: the cited path carries over "+
					"from the open entry, so a settle never retargets the question it closes",
				id,
			)
		}
		if err := jm.SettleByID("", target, id, resolution, note); err != nil {
			return "", err
		}
		return fmt.Sprintf("Settled %s (%s)", id, resolution), nil
	}

	newID, err := jm.SettleDirect("", target, resolution, cite, note)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Settled %s (%s)", newID, resolution), nil
}

// ReviewNoteRatify accepts an agent's provisional rejection as a human
// decision (ADR-0017 §6): it strips the agent's provenance prefix from the
// entry's settle note, leaving an ordinary human rejection. The manager
// refuses every other state — open, settled fixed/answered, or an
// already-ratified rejection — with the actual state named.
func (tm *TaskManager) ReviewNoteRatify(branch, id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("--ratify requires --id")
	}
	target, err := tm.journalBranch(branch)
	if err != nil {
		return "", err
	}
	if target == "" {
		return "", fmt.Errorf("%s", noBranchSentinel)
	}
	if err := reviewjournal.New(tm.Git).Ratify("", target, id); err != nil {
		return "", err
	}
	return fmt.Sprintf("Ratified %s", id), nil
}

// ReviewNoteReopen returns a settled entry to open under the same id
// (ADR-0012: an open entry is re-raised, never duplicated), dropping the
// resolution note but keeping the original finding text so the next round
// asks it again exactly as it was first asked.
func (tm *TaskManager) ReviewNoteReopen(branch, id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("--reopen requires --id")
	}
	target, err := tm.journalBranch(branch)
	if err != nil {
		return "", err
	}
	if target == "" {
		return "", fmt.Errorf("%s", noBranchSentinel)
	}
	if err := reviewjournal.New(tm.Git).Reopen("", target, id); err != nil {
		return "", err
	}
	return fmt.Sprintf("Reopened %s", id), nil
}
