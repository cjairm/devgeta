package task

import (
	"fmt"
	"strings"

	git_app "github.com/cjairm/devgeta/internal/apps/git"
)

// BranchDiffResult is BranchDiffAt's payload: the rendered diff plus totals
// for the included (non-excluded) files, for display in a stat line.
// BaseBranch/BaseSHA identify what the diff compares against so callers can
// label it, and FileStats carries per-file counts for per-file headers.
type BranchDiffResult struct {
	Content    string
	Files      int
	Added      int
	Removed    int
	BaseBranch string
	BaseSHA    string // short merge-base hash
	FileStats  []FileStat
}

// FileStat is one included file's numstat counts.
type FileStat struct {
	Path    string
	Added   int
	Removed int
	Binary  bool
}

// BranchDiffAt returns the diff of the worktree at dir against its
// merge-base with the default branch — committed AND uncommitted work —
// with the same lockfile exclusions and notes as BranchDiff. Output keeps
// git's ANSI colors (--color=always): this exists for the `dg ws` diff
// pane, which shows everything the worktree would merge. Untracked files
// are invisible to `git diff`, so they are listed by name at the end and
// counted in Files.
func BranchDiffAt(g *git_app.Git, dir string) (BranchDiffResult, error) {
	defaultBranch := g.DefaultBranchIn(dir)
	baseOut, err := g.RunCapture("-C", dir, "merge-base", "origin/"+defaultBranch, "HEAD")
	if err != nil {
		return BranchDiffResult{}, fmt.Errorf("branch-diff: %w", err)
	}
	base := strings.TrimSpace(baseOut)
	rangeLabel := defaultBranch + "..worktree"

	args := append(
		[]string{"-C", dir, "diff", "--color=always", base, "--", "."},
		exclusionPathspecs()...,
	)
	diff, err := g.RunCapture(args...)
	if err != nil {
		return BranchDiffResult{}, fmt.Errorf("branch-diff: %w", err)
	}

	numstatOut, err := g.RunCapture("-C", dir, "diff", "--numstat", "--no-renames", base)
	if err != nil {
		return BranchDiffResult{}, fmt.Errorf("branch-diff: %w", err)
	}
	changes, err := parseNumstat(numstatOut)
	if err != nil {
		return BranchDiffResult{}, fmt.Errorf("branch-diff: %w", err)
	}
	included, excluded := partitionExcluded(changes)

	shortBase := base
	if len(shortBase) > 7 {
		shortBase = shortBase[:7]
	}
	res := BranchDiffResult{
		Content:    formatBranchDiff(rangeLabel, diff, excluded),
		Files:      len(included),
		BaseBranch: defaultBranch,
		BaseSHA:    shortBase,
	}
	for _, f := range included {
		res.Added += f.Added
		res.Removed += f.Removed
		res.FileStats = append(res.FileStats, FileStat{
			Path:    f.Path,
			Added:   f.Added,
			Removed: f.Removed,
			Binary:  f.Binary,
		})
	}

	if untracked := untrackedFiles(g, dir); len(untracked) > 0 {
		res.Content += "\nUntracked files:\n  " + strings.Join(untracked, "\n  ")
		res.Files += len(untracked)
	}
	return res, nil
}

// untrackedFiles lists files git doesn't track yet in the worktree at dir.
// Best-effort: a status failure just yields an empty list, since the diff
// itself already rendered.
func untrackedFiles(g *git_app.Git, dir string) []string {
	out, err := g.RunCapture("-C", dir, "status", "--porcelain")
	if err != nil {
		return nil
	}
	var untracked []string
	for line := range strings.SplitSeq(out, "\n") {
		if path, ok := strings.CutPrefix(line, "?? "); ok {
			untracked = append(untracked, path)
		}
	}
	return untracked
}

// BranchDiff returns the merge-base diff against the default branch, with
// lockfile-style noise excluded by default (see exclusions.go) and called
// out in a trailing notes line so nothing is silently hidden.
//
// It does not fetch: review-scope is the orient-and-fetch step, and
// branch-diff is follow-up retrieval within the same review session.
// Re-fetching per file pull would be wasteful and could shift the
// merge-base mid-session if origin moved between calls.
//
// file, when non-empty, bypasses exclusions and returns only that file's
// diff — an explicit request wins over the default noise filter.
func (tm *TaskManager) BranchDiff(file string) (string, error) {
	defaultBranch := tm.Git.DefaultBranch()
	base, err := tm.mergeBase(defaultBranch)
	if err != nil {
		return "", fmt.Errorf("branch-diff: %w", err)
	}
	rangeSpec := base + "...HEAD"

	if file != "" {
		return tm.branchDiffFile(rangeSpec, file)
	}
	return tm.branchDiffAll(rangeSpec)
}

// branchDiffFile returns file's diff over rangeSpec, without exclusions.
// file is passed as its own argv element (never shell-interpolated), so it
// needs no escaping.
func (tm *TaskManager) branchDiffFile(rangeSpec, file string) (string, error) {
	diff, err := tm.Git.RunCapture("diff", rangeSpec, "--", file)
	if err != nil {
		return "", fmt.Errorf("branch-diff: %w", err)
	}
	if strings.TrimSpace(diff) == "" {
		return fmt.Sprintf("No changes for %s in %s.", file, rangeSpec), nil
	}
	return diff, nil
}

// branchDiffAll returns the full diff over rangeSpec with the default
// exclusion patterns applied in a single `git diff` invocation, plus a
// trailing note for every excluded file that actually changed.
func (tm *TaskManager) branchDiffAll(rangeSpec string) (string, error) {
	args := append([]string{"diff", rangeSpec, "--", "."}, exclusionPathspecs()...)
	diff, err := tm.Git.RunCapture(args...)
	if err != nil {
		return "", fmt.Errorf("branch-diff: %w", err)
	}

	numstatOut, err := tm.Git.RunCapture("diff", "--numstat", "--no-renames", rangeSpec)
	if err != nil {
		return "", fmt.Errorf("branch-diff: %w", err)
	}
	changes, err := parseNumstat(numstatOut)
	if err != nil {
		return "", fmt.Errorf("branch-diff: %w", err)
	}
	_, excluded := partitionExcluded(changes)

	return formatBranchDiff(rangeSpec, diff, excluded), nil
}

// formatBranchDiff renders the diff payload plus an exclusion-notes line.
// When the filtered diff is empty but some files were excluded, a sentinel
// takes the diff's place so the payload is never empty; when nothing
// changed at all (nothing to exclude either), a distinct sentinel says so.
func formatBranchDiff(rangeSpec, diff string, excluded []fileChange) string {
	trimmed := strings.TrimSpace(diff)

	var b strings.Builder
	switch {
	case trimmed != "":
		b.WriteString(diff)
	case len(excluded) > 0:
		fmt.Fprintf(
			&b,
			"No reviewable changes in %s (all changes excluded — see notes below).",
			rangeSpec,
		)
	default:
		fmt.Fprintf(&b, "No changes in %s.", rangeSpec)
	}

	if note := formatExclusionNotes(excluded, "dg task branch-diff --file <path>"); note != "" {
		b.WriteString("\n")
		b.WriteString(note)
	}

	return b.String()
}
