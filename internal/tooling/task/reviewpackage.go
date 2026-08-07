package task

import (
	"fmt"
	"strings"
)

// reviewPackageCommit is one commit row in a review package's commit list.
type reviewPackageCommit struct {
	SHA     string
	Date    string
	Subject string
}

// reviewPackageData is the orchestration result handed to formatReviewPackage.
// Base/Head are the raw refs the caller passed in (not resolved SHAs), so the
// rendered range and the printed --file follow-up hint match what the caller
// typed.
type reviewPackageData struct {
	Base string
	Head string

	Commits []reviewPackageCommit

	Included []fileChange
	Excluded []fileChange

	Diff string
}

// ReviewPackage verifies base and head both resolve to real commits, then
// returns a one-call review package for an arbitrary base..head range: the
// commit list (SHA + date + subject), a noise-filtered stat table with
// exclusion receipts, and the full -U10 diff of the included files. Unlike
// ReviewScope/BranchDiff, this range is not tied to the current branch's
// default-branch merge-base — base and head are exactly what the caller
// passed in, so it also covers reviewing a range that isn't checked out.
//
// file, when non-empty, bypasses the range/stat gathering and returns only
// that file's -U10 diff — the same escape hatch BranchDiff offers via --file.
func (tm *TaskManager) ReviewPackage(base, head, file string) (string, error) {
	if err := tm.verifyRef(base); err != nil {
		return "", fmt.Errorf("review-package: %w", err)
	}
	if err := tm.verifyRef(head); err != nil {
		return "", fmt.Errorf("review-package: %w", err)
	}

	rangeSpec := base + ".." + head

	if file != "" {
		return tm.reviewPackageFile(rangeSpec, base, head, file)
	}
	return tm.reviewPackageAll(rangeSpec, base, head)
}

// verifyRef confirms ref resolves to a real commit before any other work
// happens, so a typo'd base/head fails fast with a message naming which ref
// was the problem, rather than surfacing as an opaque git error deeper in
// the log/diff calls.
func (tm *TaskManager) verifyRef(ref string) error {
	if _, err := tm.Git.RunCapture("rev-parse", "--verify", ref); err != nil {
		return fmt.Errorf("unrecognized ref %q (rev-parse --verify failed): %w", ref, err)
	}
	return nil
}

// reviewPackageAll gathers the commit list and noise-filtered stat table for
// rangeSpec, then the -U10 diff of the included (non-excluded) files only —
// skipped entirely when nothing is included, so a range that touches only
// lockfiles doesn't pay for a diff call whose output is discarded.
func (tm *TaskManager) reviewPackageAll(rangeSpec, base, head string) (string, error) {
	commitsOut, err := tm.Git.RunCapture("log", "--format=%h%x09%as%x09%s", "--reverse", rangeSpec)
	if err != nil {
		return "", fmt.Errorf("review-package: %w", err)
	}
	commits, err := parseReviewPackageCommitLog(commitsOut)
	if err != nil {
		return "", fmt.Errorf("review-package: %w", err)
	}

	files, err := fileChanges(tm.Git, rangeSpec)
	if err != nil {
		return "", fmt.Errorf("review-package: %w", err)
	}
	included, excluded := partitionExcluded(files)

	var diff string
	if len(included) > 0 {
		args := append([]string{"diff", "-U10", rangeSpec, "--", "."}, exclusionPathspecs()...)
		diff, err = tm.Git.RunCapture(args...)
		if err != nil {
			return "", fmt.Errorf("review-package: %w", err)
		}
	}

	return formatReviewPackage(reviewPackageData{
		Base:     base,
		Head:     head,
		Commits:  commits,
		Included: included,
		Excluded: excluded,
		Diff:     diff,
	}), nil
}

// reviewPackageFile returns file's -U10 diff over rangeSpec, without
// exclusions — file is passed as its own argv element (never
// shell-interpolated), so it needs no escaping.
func (tm *TaskManager) reviewPackageFile(rangeSpec, base, head, file string) (string, error) {
	diff, err := tm.Git.RunCapture("diff", "-U10", rangeSpec, "--", file)
	if err != nil {
		return "", fmt.Errorf("review-package: %w", err)
	}
	if strings.TrimSpace(diff) == "" {
		return fmt.Sprintf("No changes for %s in %s..%s.", file, base, head), nil
	}
	return diff, nil
}

// parseReviewPackageCommitLog parses `git log --format=%h%x09%as%x09%s`
// output (short SHA, author date, subject, tab-separated) into
// reviewPackageCommit entries. Named distinctly from scope.go's parseCommitLog
// (which parses a different, unit/record-separator-delimited format that also
// carries commit bodies) — the two review flows gather commit metadata via
// different git log formats and were built independently, so this stays its
// own function rather than forcing a shared parser across two different wire
// formats.
func parseReviewPackageCommitLog(raw string) ([]reviewPackageCommit, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out []reviewPackageCommit
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("malformed commit log line: %q", line)
		}
		out = append(out, reviewPackageCommit{SHA: parts[0], Date: parts[1], Subject: parts[2]})
	}
	return out, nil
}

// formatReviewPackage renders reviewPackageData as the compact, LLM-oriented
// review package: range, commit list, per-file stat table with exclusion
// receipts, and a fenced diff payload. Output is payload-only (see
// task-design.md) — the ` ```diff ` fence is the one place markdown earns
// its tokens, per the same guide's stated exception.
func formatReviewPackage(d reviewPackageData) string {
	var b strings.Builder
	fmt.Fprintf(&b, "range: %s..%s\n", d.Base, d.Head)

	b.WriteString("commits:\n")
	if len(d.Commits) == 0 {
		b.WriteString("No commits in range.\n")
	} else {
		for _, c := range d.Commits {
			fmt.Fprintf(&b, "- %s %s %s\n", c.SHA, c.Date, c.Subject)
		}
	}

	fmt.Fprintf(&b, "files (%d):\n", len(d.Included))
	if len(d.Included) == 0 {
		b.WriteString("No file changes in range.\n")
	} else {
		b.WriteString(formatFileStats(d.Included))
		b.WriteString("\n")
	}

	hint := fmt.Sprintf("dg task review-package %s %s --file <path>", d.Base, d.Head)
	if note := formatExclusionNotes(d.Excluded, hint); note != "" {
		b.WriteString(note)
		b.WriteString("\n")
	}

	if strings.TrimSpace(d.Diff) != "" {
		b.WriteString("\n```diff\n")
		b.WriteString(d.Diff)
		if !strings.HasSuffix(d.Diff, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```")
	}

	return strings.TrimRight(b.String(), "\n")
}
