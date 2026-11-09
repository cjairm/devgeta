package task

import (
	"fmt"
	"strings"
	"sync"

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
// that no `git diff` can see. Both the diff pane and the stats row are built from this
// one gather, so they can never disagree about what the branch changes.
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
	diffArgs := append(args, exclusionPathspecs()...)

	// The rendered diff and the numstat are two reads of the same commit range;
	// neither mutates the repository and neither needs the other's output, so
	// they run together instead of one after the other — on a large branch that
	// halves the wait the `dg ws` diff pane shows. The merge-base above stays
	// sequential because it produces the `base` both of these need, and the
	// untracked listing below stays sequential too (see the return value).
	var (
		wg         sync.WaitGroup
		diff       string
		diffErr    error
		changes    []fileChange
		changesErr error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		diff, diffErr = g.RunCapture(diffArgs...)
	}()
	go func() {
		defer wg.Done()
		changes, changesErr = changedFiles(g, dir, base)
	}()
	wg.Wait()

	// Checked in a fixed order, not in the order they finished: when both fail
	// the caller must get the same error every run, and the rendered diff is
	// the pane's primary content so its failure is the one worth reporting.
	if diffErr != nil {
		return worktreeDiff{}, fmt.Errorf("branch-diff: %w", diffErr)
	}
	if changesErr != nil {
		return worktreeDiff{}, fmt.Errorf("branch-diff: %w", changesErr)
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

// changedFiles gathers the file-level changes between base and the working
// tree at dir (dir "" = current directory) via `git diff --numstat
// --no-renames <base>`, parsed into fileChange. It is the one place this
// two-dot numstat-plus-parse gather is implemented, so collectWorktreeDiff's
// origin/-anchored base and a later local-default-branch-anchored base (a
// document-status sweep) read the same shape of result without duplicating
// the gather.
func changedFiles(g *git_app.Git, dir, base string) ([]fileChange, error) {
	out, err := g.RunCapture(atDir(dir, "diff", "--numstat", "--no-renames", base)...)
	if err != nil {
		return nil, err
	}
	return parseNumstat(out)
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

// BranchStatsResult is BranchStatsAt's payload (ADR-0051): just the counts a
// dashboard row needs, no diff body and no per-file breakdown.
type BranchStatsResult struct {
	Files   int
	Added   int
	Removed int
}

// BranchStatsAt returns the diffstat counts only for the worktree at dir
// against its merge-base with defaultBranch (ADR-0051) - the ws dashboard's
// per-row number. It shares base resolution and changedFiles with
// collectWorktreeDiff, so a row's counts and the diff pane's header can
// never disagree, and skips the rendered diff and its color pass entirely.
//
// defaultBranch is a parameter, not resolved here, because it is a
// repo-level answer (DefaultBranchIn), not a per-worktree one: the caller
// resolves it once per repo per slow refresh and passes it to every one of
// that repo's worktrees, rather than paying for it again per worktree.
func BranchStatsAt(g *git_app.Git, dir, defaultBranch string) (BranchStatsResult, error) {
	baseOut, err := g.RunCapture(atDir(dir, "merge-base", "origin/"+defaultBranch, "HEAD")...)
	if err != nil {
		return BranchStatsResult{}, fmt.Errorf("branch-stats: %w", err)
	}
	base := strings.TrimSpace(baseOut)

	changes, err := changedFiles(g, dir, base)
	if err != nil {
		return BranchStatsResult{}, fmt.Errorf("branch-stats: %w", err)
	}
	included, _ := partitionExcluded(changes)
	untracked := untrackedFiles(g, dir)

	res := BranchStatsResult{Files: len(included) + len(untracked)}
	for _, f := range included {
		res.Added += f.Added
		res.Removed += f.Removed
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

	if note := formatExclusionNotes(excluded); note != "" {
		b.WriteString("\n")
		b.WriteString(note)
	}

	return b.String()
}
