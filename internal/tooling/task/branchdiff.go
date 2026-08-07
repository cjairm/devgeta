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

// atDir prefixes git args with `-C <dir>` when dir names one, and returns them
// untouched when it does not — mirroring what the git app's own helpers do for
// its dir-aware methods. Without this, a "" dir would send git a literal empty
// `-C ""`: harmless to git, but it puts a meaningless argument in front of
// every command this file runs, which is the kind of noise that later gets
// copied into a new call site as if it meant something.
func atDir(dir string, args ...string) []string {
	if dir == "" {
		return args
	}
	return append([]string{"-C", dir}, args...)
}

// worktreeDiff is everything a branch would merge, as read from one worktree:
// the rendered diff, the per-file counts behind it, and the untracked files
// that no `git diff` can see. Both branch-diff renderings are built from this
// one gather, so the `dg ws` diff pane and `dg task branch-diff` can never
// disagree about what the branch changes.
type worktreeDiff struct {
	defaultBranch string
	base          string
	rangeLabel    string
	diff          string
	included      []fileChange
	excluded      []fileChange
	untracked     []string
}

// collectWorktreeDiff gathers the diff of the worktree at dir ("" = current
// directory) against its merge-base with the default branch — committed AND
// uncommitted work — with the default lockfile-style exclusions applied to the
// rendered diff. color asks git for ANSI colors, which the `dg ws` diff pane
// wants and a machine-read payload does not.
//
// Comparing against the merge-base with two dots (`git diff <base>`, not
// `<base>...HEAD`) is what pulls uncommitted work in: the two-dot form diffs a
// commit against the working tree, so staged and unstaged edits are included
// exactly as a reviewer would see them after a commit. See ADR-0019.
func collectWorktreeDiff(g *git_app.Git, dir string, color bool) (worktreeDiff, error) {
	defaultBranch := g.DefaultBranchIn(dir)
	baseOut, err := g.RunCapture(atDir(dir, "merge-base", "origin/"+defaultBranch, "HEAD")...)
	if err != nil {
		return worktreeDiff{}, fmt.Errorf("branch-diff: %w", err)
	}
	base := strings.TrimSpace(baseOut)

	args := atDir(dir, "diff")
	if color {
		args = append(args, "--color=always")
	}
	args = append(args, base, "--", ".")
	diff, err := g.RunCapture(append(args, exclusionPathspecs()...)...)
	if err != nil {
		return worktreeDiff{}, fmt.Errorf("branch-diff: %w", err)
	}

	numstatOut, err := g.RunCapture(atDir(dir, "diff", "--numstat", "--no-renames", base)...)
	if err != nil {
		return worktreeDiff{}, fmt.Errorf("branch-diff: %w", err)
	}
	changes, err := parseNumstat(numstatOut)
	if err != nil {
		return worktreeDiff{}, fmt.Errorf("branch-diff: %w", err)
	}
	included, excluded := partitionExcluded(changes)

	return worktreeDiff{
		defaultBranch: defaultBranch,
		base:          base,
		rangeLabel:    defaultBranch + "..worktree",
		diff:          diff,
		included:      included,
		excluded:      excluded,
		untracked:     untrackedFiles(g, dir),
	}, nil
}

// BranchDiffAt returns the diff of the worktree at dir against its
// merge-base with the default branch — committed AND uncommitted work —
// with the same lockfile exclusions and notes as BranchDiff. Output keeps
// git's ANSI colors: this exists for the `dg ws` diff pane, which shows
// everything the worktree would merge. Untracked files are invisible to
// `git diff`, so they are listed by name at the end and counted in Files.
func BranchDiffAt(g *git_app.Git, dir string) (BranchDiffResult, error) {
	data, err := collectWorktreeDiff(g, dir, true)
	if err != nil {
		return BranchDiffResult{}, err
	}

	shortBase := data.base
	if len(shortBase) > 7 {
		shortBase = shortBase[:7]
	}
	res := BranchDiffResult{
		Content: formatBranchDiff(
			data.rangeLabel,
			data.diff,
			data.excluded,
		) + untrackedNote(
			data.untracked,
		),
		Files:      len(data.included) + len(data.untracked),
		BaseBranch: data.defaultBranch,
		BaseSHA:    shortBase,
	}
	for _, f := range data.included {
		res.Added += f.Added
		res.Removed += f.Removed
		res.FileStats = append(res.FileStats, FileStat{
			Path:    f.Path,
			Added:   f.Added,
			Removed: f.Removed,
			Binary:  f.Binary,
		})
	}
	return res, nil
}

// untrackedNote renders the trailing untracked-files block both branch-diff
// renderings append, with a leading newline, or "" when nothing is untracked.
// Untracked files carry no diff at all, so naming them is the only way a
// reader learns they are part of what the branch would merge.
func untrackedNote(untracked []string) string {
	if len(untracked) == 0 {
		return ""
	}
	return "\nUntracked files (no diff — read them directly):\n  " +
		strings.Join(untracked, "\n  ")
}

// untrackedFiles lists files git doesn't track yet in the worktree at dir,
// limited to pathspec when any is given ("is THIS path untracked?" — used to
// answer a single --file request). Best-effort: a failure yields an empty list,
// since the diff itself already rendered.
//
// `git ls-files --others --exclude-standard` is the question asked, rather than
// filtering `git status --porcelain` output, for two reasons that both come
// down to letting git own path handling:
//
//   - **With a pathspec, git resolves the caller's path itself.** A caller can
//     name the same file as `docs/new.md`, `./docs/new.md`, an absolute path, or
//     `new.md` from inside `docs/` — git accepts all of them. Comparing a
//     caller's string against a listing matched only the one spelling git
//     happened to print, so every other form was reported as "no changes" for a
//     file that is entirely new work (journal n1, round 2).
//   - **It lists files, not collapsed directories.** `status --porcelain` reports
//     a wholly-untracked directory as one `newdir/` entry; a reviewer needs the
//     files inside it, which are what they would have to read.
//
// `-z` is required, not a preference: without it git quotes and C-escapes any
// path with "unusual" characters — a space, a quote, a tab, a non-ASCII byte —
// so `docs/my draft.md` comes back as the 20-byte string `"docs/my draft.md"`,
// which matches nothing and cannot be pasted back into any command. With `-z`
// each path is verbatim and NUL-terminated, which is also why the split below is
// on "\x00" rather than "\n": a path may legally contain a newline.
func untrackedFiles(g *git_app.Git, dir string, pathspec ...string) []string {
	args := atDir(dir, "ls-files", "--others", "--exclude-standard", "-z")
	if len(pathspec) > 0 {
		args = append(append(args, "--"), pathspec...)
	}
	out, err := g.RunCapture(args...)
	if err != nil {
		return nil
	}
	var untracked []string
	for path := range strings.SplitSeq(out, "\x00") {
		if path != "" {
			untracked = append(untracked, path)
		}
	}
	return untracked
}

// BranchDiff returns everything the current branch would merge, diffed
// against its merge-base with the default branch: its commits AND whatever is
// still uncommitted in the working tree, with lockfile-style noise excluded by
// default (see exclusions.go) and called out in a trailing notes line so
// nothing is silently hidden. Untracked files carry no diff, so they are
// listed by name at the end.
//
// Including uncommitted work is the point, not a side effect: a review covers
// the branch's state, so work in progress can be reviewed without committing
// it first (ADR-0019). It is the same gather the `dg ws` diff pane uses
// (BranchDiffAt), so the two can never disagree.
//
// It does not fetch: review-scope is the orient-and-fetch step, and
// branch-diff is follow-up retrieval within the same review session.
// Re-fetching per file pull would be wasteful and could shift the
// merge-base mid-session if origin moved between calls.
//
// file, when non-empty, bypasses exclusions and returns only that file's
// diff — an explicit request wins over the default noise filter.
func (tm *TaskManager) BranchDiff(file string) (string, error) {
	// --file is the per-file path a reviewer walks a large branch with, one
	// call per file, so it deliberately does NOT run the full gather: it needs
	// the merge-base and nothing else, and paying for the whole-branch diff,
	// numstat, and status on every file would multiply that cost by the number
	// of files.
	if file != "" {
		return tm.branchDiffFile(file)
	}
	data, err := collectWorktreeDiff(tm.Git, "", false)
	if err != nil {
		return "", err
	}
	return formatBranchDiff(data.rangeLabel, data.diff, data.excluded) +
		untrackedNote(data.untracked), nil
}

// branchDiffFile returns file's diff against the merge-base, without
// exclusions. file is passed as its own argv element (never
// shell-interpolated), so it needs no escaping.
//
// An untracked file gets its own answer rather than the "no changes" sentinel:
// `git diff` cannot see it at all, so an empty result there means "read the
// whole file", not "nothing changed" — the reader is a reviewer deciding
// whether to look, and those two answers point opposite ways. The question is
// put to git as a pathspec, so `file` is matched however the caller spelled it
// (`docs/new.md`, `./docs/new.md`, an absolute path, or a path relative to a
// subdirectory), and it is asked only when the diff came back empty — the one
// case that cannot be answered without it.
func (tm *TaskManager) branchDiffFile(file string) (string, error) {
	defaultBranch := tm.Git.DefaultBranch()
	base, err := tm.mergeBase(defaultBranch)
	if err != nil {
		return "", fmt.Errorf("branch-diff: %w", err)
	}
	rangeLabel := defaultBranch + "..worktree"

	diff, err := tm.Git.RunCapture("diff", base, "--", file)
	if err != nil {
		return "", fmt.Errorf("branch-diff: %w", err)
	}
	if strings.TrimSpace(diff) != "" {
		return diff, nil
	}
	if len(untrackedFiles(tm.Git, "", file)) > 0 {
		return fmt.Sprintf(
			"%s is untracked, so it has no diff in %s — its whole content is the change; "+
				"read the file directly.",
			file,
			rangeLabel,
		), nil
	}
	return fmt.Sprintf("No changes for %s in %s.", file, rangeLabel), nil
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
