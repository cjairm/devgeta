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
	"os"
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

// ReviewJournalSnapshotEnvVar names the child-only environment variable
// review-run sets on a spawned reviewer so it reads the journal as it stood
// at round start, not the live file (ADR-0017 §4). Defined once here and
// referenced everywhere else so a later task's writer and this reader always
// agree on the name.
const ReviewJournalSnapshotEnvVar = "DEVGETA_REVIEW_JOURNAL_SNAPSHOT"

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

// verifyRev checks ONCE, before any entry is touched, that rev names a commit
// this repository actually has — the failure ADR-0023's flow makes most likely,
// since it fetches refs/pull/<n>/head and a skipped or failed fetch leaves
// every later lookup with nothing to resolve against — and returns the commit
// SHA it resolved to. A blank rev stays blank: that is working-tree mode, which
// resolves nothing and must behave exactly as it did before --rev existed.
//
// It is a per-command check, not a per-entry one, for two reasons. Per entry it
// would repeat one answer N times. And on the read path it is the only place
// the problem CAN be reported at all: Verdict returns a bare string, so an
// unverified bad rev would surface as "every entry is stale" — a silent lie
// about the findings rather than an error about the revision.
//
// Callers use the RESOLVED sha from here, not the string the user typed. Every
// stamp ADR-0023 writes is supposed to name an immutable commit, and a ref name
// is not one: `--rev refs/pull/213/head` recorded verbatim describes a
// different commit after the next fetch, and two ticks of the same review stamp
// the same text for two different states, so the entries cannot be compared. A
// caller that already passes a full SHA — pr-review-target does — resolves to
// itself and is unaffected.
func (tm *TaskManager) verifyRev(rev string) (string, error) {
	if strings.TrimSpace(rev) == "" {
		return "", nil
	}
	sha, err := tm.Git.ResolveCommit(rev)
	if err != nil {
		return "", fmt.Errorf(
			"--rev %s does not name a commit in this repository — fetch it first "+
				"(for a pull request: git fetch origin refs/pull/<n>/head): %w",
			rev, err,
		)
	}
	return sha, nil
}

// ReviewNotes prints branch's journal with each entry's freshness resolved
// against the current working tree, or against rev when one is given.
//
// rev is what makes the freshness signal mean something for a review of code
// that is not checked out (ADR-0023 §4): pass the revision under review NOW —
// a pull request's current head — and [STALE] means "the pull request changed
// this file since the finding was written". Against the working tree of an
// unrelated checkout it would mean nothing but "you are not on that branch".
func (tm *TaskManager) ReviewNotes(
	branch, rev string,
	showPath, prune bool,
) (string, error) {
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

	// Only the freshness-resolving read below cares about the revision, so it is
	// verified here rather than on entry: --prune deletes journals and --path
	// prints a filename, and neither looks at an entry's content stamp. Cobra
	// already refuses those flags alongside --rev, so this spares nobody but a
	// direct programmatic caller — from a rev-parse that could not have changed
	// the answer.
	rev, err = tm.verifyRev(rev)
	if err != nil {
		return "", err
	}
	jm = reviewjournal.NewAtRev(tm.Git, rev)

	j, err := loadJournalForDisplay(jm, target)
	if err != nil {
		return "", err
	}
	if len(j.Entries) == 0 {
		return fmt.Sprintf(noNotesFmt, target), nil
	}
	return renderNotes(jm, j), nil
}

// loadJournalForDisplay is the ONLY read path a reviewer's `review-notes`
// call goes through, and the only place ReviewJournalSnapshotEnvVar is read.
// Writes (ReviewNoteOpen, ReviewNoteSettle, Ratify, Reopen, --path, --prune)
// never call this — they always hit the live journal, which is the point:
// round-start isolation is a display-side illusion, not a second store
// (ADR-0017 §4).
//
// When the variable is unset, empty, or names a file that cannot be read,
// this falls back to the live journal — never an error. Step 1 of the
// snapshot sequence (owned elsewhere) always writes the snapshot before a
// round starts, including an empty one when no journal exists yet, so a
// missing or unreadable file here is an anomaly (someone deleted it, a bug),
// not the normal first-review path. Falling back to live state is the right
// response to that anomaly: it costs that one reviewer its isolation for the
// round, which is recoverable. The alternative — treating a missing snapshot
// as an empty journal — would hide every already-settled entry and send the
// reviewer back around the re-raise circle the journal exists to break
// (ADR-0012), which is not recoverable in the same way. Losing isolation is
// the safe failure mode; losing history is not.
func loadJournalForDisplay(
	jm *reviewjournal.Manager,
	branch string,
) (*reviewjournal.Journal, error) {
	path := os.Getenv(ReviewJournalSnapshotEnvVar)
	if strings.TrimSpace(path) == "" {
		return jm.Load("", branch)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return jm.Load("", branch)
	}
	return reviewjournal.Parse(branch, data), nil
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
			case reviewjournal.FreshnessStanding:
				sb.WriteString(" [STANDING: expires when the reason below stops holding," +
					" not when the file changes — re-read the reason before overriding it]")
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
//
// rev, when given, is the revision the cited path is stamped at instead of the
// working tree — required when reviewing a commit that is not checked out,
// where the cited file may be absent from the checkout or hold unrelated
// content. The path must exist at that revision, exactly as it must exist in
// the working tree otherwise (ADR-0012 §3).
func (tm *TaskManager) ReviewNoteOpen(branch, rev, cite, note string) (string, error) {
	if strings.TrimSpace(note) == "" {
		return "", fmt.Errorf("--note is required and cannot be empty")
	}
	rev, err := tm.verifyRev(rev)
	if err != nil {
		return "", err
	}
	target, err := tm.journalBranch(branch)
	if err != nil {
		return "", err
	}
	if target == "" {
		return "", fmt.Errorf("%s", noBranchSentinel)
	}
	id, err := reviewjournal.NewAtRev(tm.Git, rev).Open("", target, cite, note)
	if err != nil {
		return "", err
	}
	return "Noted " + id, nil
}

// ReviewNoteSettle settles an entry. With an id it closes that open entry (the
// cite carries over, so an answer can never retarget the question it closes);
// without one it records an exchange that was never open. rev stamps the
// conclusion at that revision instead of the working tree, as in ReviewNoteOpen.
func (tm *TaskManager) ReviewNoteSettle(
	branch, rev, id, resolution, cite, note string,
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
	rev, err := tm.verifyRev(rev)
	if err != nil {
		return "", err
	}
	target, err := tm.journalBranch(branch)
	if err != nil {
		return "", err
	}
	if target == "" {
		return "", fmt.Errorf("%s", noBranchSentinel)
	}

	jm := reviewjournal.NewAtRev(tm.Git, rev)
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
