package task

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/reviewjournal"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	"github.com/cjairm/devgeta/pkg/logger"
)

// taskWorktreePath returns worktree.GetWorktreeBasePath()/<repoSlug>/<flat-name>
// — the exact same location `dg wt` uses (internal/tooling/worktree's
// unexported worktreePath) — so a worktree created by worktree-start is
// immediately visible to `dg wt list`, and one created/managed by `dg wt` is
// visible here. See the design decision recorded in
// docs/plans/cycles/2026-07-22-agent-task-expansion.md (Slice C).
func taskWorktreePath(repoSlug, name string) string {
	return filepath.Join(worktree.GetWorktreeBasePath(), repoSlug, worktree.FlattenName(name))
}

// WorktreeStart creates a new git worktree with a new branch, in the same
// base path `dg wt` uses. It refuses to run from a dirty tree so nothing is
// left half set up. When base is empty, the new branch is based on the
// repo's freshly-fetched default branch (reusing Git.CreateWorktreeIn's
// local/remote-branch-reuse logic verbatim); when base is given explicitly,
// the branch is created fresh from exactly that ref.
func (tm *TaskManager) WorktreeStart(name, base string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("worktree-start: name is required")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("worktree-start: %w", err)
	}

	dirty, err := tm.Git.IsWorktreeDirty(cwd)
	if err != nil {
		return "", fmt.Errorf("worktree-start: %w", err)
	}
	if dirty {
		return "", fmt.Errorf(
			"worktree-start: refusing to start from a dirty tree; commit or stash your changes first",
		)
	}

	repoRoot, err := tm.Git.GetRepoRootIn(cwd)
	if err != nil {
		return "", fmt.Errorf("worktree-start: not in a git repository: %w", err)
	}

	if err := tm.Git.FetchOrigin(); err != nil {
		return "", fmt.Errorf("worktree-start: %w", err)
	}

	repoSlug := filepath.Base(repoRoot)
	wtPath := taskWorktreePath(repoSlug, name)

	if _, statErr := os.Stat(wtPath); statErr == nil {
		return "", fmt.Errorf("worktree-start: %s already exists", wtPath)
	}

	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return "", fmt.Errorf("worktree-start: failed to create worktree directory: %w", err)
	}

	ref := base
	if base == "" {
		// Label the implicit base for the confirmation line. CreateWorktreeIn
		// may actually reuse an existing local/remote branch instead (see its
		// doc comment); the label still communicates the common case clearly.
		ref = "origin/" + tm.Git.DefaultBranchIn(repoRoot)
		if err := tm.Git.CreateWorktreeIn(repoRoot, wtPath, name); err != nil {
			return "", fmt.Errorf("worktree-start: %w", err)
		}
	} else {
		if err := tm.Git.CreateWorktreeAtBaseIn(repoRoot, wtPath, name, base); err != nil {
			return "", fmt.Errorf("worktree-start: %w", err)
		}
	}

	return fmt.Sprintf("Created worktree %s (branch %s, base %s)", wtPath, name, ref), nil
}

// WorktreeFinish tears down a worktree via exactly one of merge or discard.
// Target resolution is deterministic: an explicit name wins; otherwise cwd
// resolves to the linked worktree it's inside; otherwise the command errors
// and lists the worktrees it found rather than guessing from the main
// checkout.
//
// merge: refuses if either the worktree or the main checkout has uncommitted
// changes (no --force escape — see worktreeFinishMerge), rebases the
// worktree's branch onto the default branch if diverged, fast-forward-merges
// it into the default branch from the main checkout, then removes the
// worktree and deletes the branch (safe only because the fast-forward already
// made it fully merged).
//
// discard: refuses on a dirty worktree unless force is set, then removes the
// worktree and deletes the branch unconditionally.
func (tm *TaskManager) WorktreeFinish(name string, merge, discard, force bool) (string, error) {
	if merge == discard {
		return "", fmt.Errorf("worktree-finish: exactly one of --merge or --discard is required")
	}

	wtPath, branch, err := tm.resolveWorktreeTarget(name)
	if err != nil {
		return "", fmt.Errorf("worktree-finish: %w", err)
	}

	var out string
	if discard {
		out, err = tm.worktreeFinishDiscard(wtPath, branch, force)
	} else {
		out, err = tm.worktreeFinishMerge(wtPath, branch)
	}
	if err != nil {
		return "", fmt.Errorf("worktree-finish: %w", err)
	}
	return out, nil
}

// worktreeFinishDiscard removes the worktree and its branch unconditionally,
// refusing first when the worktree has uncommitted changes and force wasn't
// passed.
func (tm *TaskManager) worktreeFinishDiscard(wtPath, branch string, force bool) (string, error) {
	if !force {
		dirty, err := tm.Git.IsWorktreeDirty(wtPath)
		if err != nil {
			return "", err
		}
		if dirty {
			return "", fmt.Errorf(
				"%s has uncommitted changes; use --force to discard anyway", wtPath,
			)
		}
	}

	// Resolved BEFORE the removal: afterwards wtPath is gone, and the journal's
	// location can only be resolved by running git somewhere that still exists.
	mainWorktree, mainErr := tm.Git.GetMainWorktree(wtPath)

	if err := tm.Git.RemoveWorktree(wtPath, true, branch); err != nil {
		if !force {
			return "", fmt.Errorf("failed to discard %s: %w", wtPath, err)
		}
		return tm.forceDiscardFallback(wtPath, branch, err)
	}

	if mainErr == nil {
		tm.dropReviewJournal(mainWorktree, branch)
	}
	return fmt.Sprintf("Discarded worktree %s (branch %s deleted)", wtPath, branch), nil
}

// forceDiscardFallback handles the one case RemoveWorktree can't: `git
// worktree remove` refuses when the worktree has modified or untracked
// files, and RemoveWorktree deliberately never passes --force through (that
// refusal is the safety net for every other caller, e.g. worktree-finish
// --merge). --force here means the caller explicitly wants this thrown away
// regardless, so remove the directory directly, prune the now-stale git
// metadata, and force-delete the branch from the main checkout —
// RemoveWorktree never reached its own branch-delete step, since `worktree
// remove` failed before that.
func (tm *TaskManager) forceDiscardFallback(
	wtPath, branch string,
	removeErr error,
) (string, error) {
	mainWorktree, mainErr := tm.Git.GetMainWorktree(wtPath)
	if mainErr != nil {
		return "", fmt.Errorf("failed to discard %s: %w", wtPath, removeErr)
	}

	if err := os.RemoveAll(wtPath); err != nil {
		return "", fmt.Errorf("failed to discard %s: %w", wtPath, err)
	}
	if err := tm.Git.PruneWorktreesAt(mainWorktree); err != nil {
		return "", fmt.Errorf(
			"removed %s but failed to prune stale worktree metadata: %w", wtPath, err,
		)
	}
	if branch != "" {
		if err := tm.Git.ExecuteCommandAt(mainWorktree, "branch", "-D", branch); err != nil {
			return "", fmt.Errorf(
				"removed %s but failed to delete branch %q: %w", wtPath, branch, err,
			)
		}
		tm.dropReviewJournal(mainWorktree, branch)
	}

	return fmt.Sprintf("Discarded worktree %s (branch %s deleted)", wtPath, branch), nil
}

// worktreeFinishMerge rebases (if needed), fast-forward-merges from the main
// checkout, then removes the worktree and deletes its branch. Every step is
// sequenced so a failure partway through leaves inspectable state and an
// actionable message: nothing is removed until the fast-forward merge has
// actually landed the branch's commits on the default branch.
func (tm *TaskManager) worktreeFinishMerge(wtPath, branch string) (string, error) {
	dirty, err := tm.Git.IsWorktreeDirty(wtPath)
	if err != nil {
		return "", err
	}
	if dirty {
		return "", fmt.Errorf(
			"%s has uncommitted changes; commit or stash your changes first", wtPath,
		)
	}

	defaultBranch := tm.Git.DefaultBranchIn(wtPath)

	mainWorktree, err := tm.Git.GetMainWorktree(wtPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve main worktree: %w", err)
	}

	mainBranch, err := tm.Git.CurrentBranchIn(mainWorktree)
	if err != nil {
		return "", fmt.Errorf("failed to check main checkout's branch: %w", err)
	}
	if mainBranch != defaultBranch {
		return "", fmt.Errorf(
			"main checkout %s is on %q, not %q; check out %q there first",
			mainWorktree, mainBranch, defaultBranch, defaultBranch,
		)
	}

	mainDirty, err := tm.Git.IsWorktreeDirty(mainWorktree)
	if err != nil {
		return "", err
	}
	if mainDirty {
		return "", fmt.Errorf(
			"main checkout %s has uncommitted changes; commit or stash them there first",
			mainWorktree,
		)
	}

	// merge-base --is-ancestor exits non-zero when defaultBranch is NOT an
	// ancestor of the branch's HEAD, i.e. the branch has diverged (default
	// gained commits since the branch point) and needs a rebase before it can
	// fast-forward-merge.
	if ancestorErr := tm.Git.ExecuteCommandAt(
		wtPath, "merge-base", "--is-ancestor", defaultBranch, "HEAD",
	); ancestorErr != nil {
		if err := tm.Git.ExecuteCommandAt(wtPath, "rebase", defaultBranch); err != nil {
			return "", fmt.Errorf(
				"%s diverged from %s and rebase failed: %w"+
					" (resolve conflicts in %s, or run `git -C %s rebase --abort`)",
				branch, defaultBranch, err, wtPath, wtPath,
			)
		}
	}

	if err := tm.Git.ExecuteCommandAt(mainWorktree, "merge", "--ff-only", branch); err != nil {
		return "", fmt.Errorf(
			"fast-forward merge of %s into %s failed: %w (worktree left in place at %s)",
			branch, defaultBranch, err, wtPath,
		)
	}

	if err := tm.Git.RemoveWorktree(wtPath, true, branch); err != nil {
		// RemoveWorktree fails in two distinct sub-cases that must not share a
		// message: `worktree remove` itself failing (the worktree is genuinely
		// still there) vs. `worktree remove` succeeding and only the following
		// `branch -D` failing (the worktree is already gone). Only the latter
		// carries git.ErrBranchDeleteFailed — anything else means removal
		// itself never completed.
		//
		// This used to match on a substring of RemoveWorktree's message, which
		// silently stopped working the moment that wording was reworded: the
		// merge path then reported "worktree still at <path>" for a worktree
		// git had already deleted. errors.Is cannot rot that way.
		if errors.Is(err, git.ErrBranchDeleteFailed) {
			return "", fmt.Errorf(
				"merged %s into %s and removed the worktree, but failed to delete branch %s: %w",
				branch, defaultBranch, branch, err,
			)
		}
		return "", fmt.Errorf(
			"merged %s into %s, but failed to remove worktree/delete branch: %w"+
				" (worktree still at %s)",
			branch, defaultBranch, err, wtPath,
		)
	}

	tm.dropReviewJournal(mainWorktree, branch)
	return fmt.Sprintf("Merged %s into %s; removed worktree %s", branch, defaultBranch, wtPath), nil
}

// dropReviewJournal deletes branch's review journal once the branch itself is
// gone (ADR-0012). Every path that deletes a worktree's branch must call this,
// or the journal outlives the work it describes: worktree-finish deletes the
// branch through Git.RemoveWorktree directly rather than through
// WorktreeManager.removeByRepo, so it does not inherit that path's cleanup and
// needs its own.
//
// repoDir must be a path inside the repo that still exists after the removal —
// the main checkout, never the deleted worktree — because resolving the
// journal's location runs git there.
//
// Best-effort: the worktree and branch are already gone by the time this runs,
// so failing the command over a leftover text file would report failure for an
// operation that succeeded. A survivor is self-correcting —
// `devgeta task review-notes --prune` drops journals whose branch no longer
// exists, which this one now satisfies.
func (tm *TaskManager) dropReviewJournal(repoDir, branch string) {
	if branch == "" {
		return
	}
	if err := reviewjournal.New(tm.Git).Delete(repoDir, branch); err != nil {
		logger.L().Debugw(
			"worktree-finish: could not delete the branch's review journal",
			"branch", branch,
			"error", err,
		)
	}
}

// resolveWorktreeTarget implements worktree-finish's deterministic target
// selection: an explicit name wins; otherwise cwd resolving inside a linked
// worktree wins; otherwise it errors listing the worktrees it found. It never
// falls back to guessing from a main checkout.
func (tm *TaskManager) resolveWorktreeTarget(name string) (wtPath, branch string, err error) {
	if name != "" {
		wtPath, err = tm.findWorktreePath(name)
		if err != nil {
			return "", "", err
		}
		branch, err = tm.branchForWorktree(wtPath)
		return wtPath, branch, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", "", err
	}

	repoRoot, rootErr := tm.Git.GetRepoRootIn(cwd)
	if rootErr != nil {
		return "", "", fmt.Errorf(
			"not inside a git repository; specify <name>.%s", tm.availableWorktreesNote(),
		)
	}

	mainWorktree, mainErr := tm.Git.GetMainWorktree(cwd)
	if mainErr != nil {
		return "", "", fmt.Errorf("failed to resolve main worktree: %w", mainErr)
	}

	if config.CanonicalRepoPath(repoRoot) == config.CanonicalRepoPath(mainWorktree) {
		return "", "", fmt.Errorf(
			"not inside a linked worktree (this is the main checkout); specify <name>.%s",
			tm.availableWorktreesNote(),
		)
	}

	wtPath = repoRoot
	branch, err = tm.branchForWorktree(wtPath)
	return wtPath, branch, err
}

// findWorktreePath resolves an explicit worktree name to its full path by
// scanning the centralized worktree base path (the same one `dg wt` uses)
// for a repo slug containing it.
func (tm *TaskManager) findWorktreePath(name string) (string, error) {
	base := worktree.GetWorktreeBasePath()
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("no worktree named %q found; no worktrees exist yet", name)
		}
		return "", err
	}

	flat := worktree.FlattenName(name)
	var matches []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(base, e.Name(), flat)
		if _, statErr := os.Stat(candidate); statErr == nil {
			matches = append(matches, candidate)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no worktree named %q found.%s", name, tm.availableWorktreesNote())
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf(
			"multiple worktrees named %q found (%s); remove the ambiguity and retry",
			name, strings.Join(matches, ", "),
		)
	}
}

// branchForWorktree resolves the branch checked out at wtPath via `git
// worktree list --porcelain`, run from wtPath itself so it works regardless
// of the process's actual working directory.
func (tm *TaskManager) branchForWorktree(wtPath string) (string, error) {
	return tm.Git.BranchForWorktree(wtPath)
}

// availableWorktreesNote renders the "never guess, list what's available"
// half of a target-resolution error.
func (tm *TaskManager) availableWorktreesNote() string {
	names := listAvailableWorktrees()
	if len(names) == 0 {
		return " No worktrees exist yet."
	}
	return " Available worktrees: " + strings.Join(names, ", ")
}

// listAvailableWorktrees walks the centralized worktree base path
// (repo-slug/flat-name directories) for display in an error message only —
// it does not query git or tmux, so it stays cheap and dependency-free; `dg
// wt list` remains the one command that reports live git/tmux status.
func listAvailableWorktrees() []string {
	base := worktree.GetWorktreeBasePath()
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var names []string
	for _, repoEntry := range entries {
		if !repoEntry.IsDir() {
			continue
		}
		repoDir := filepath.Join(base, repoEntry.Name())
		wtEntries, err := os.ReadDir(repoDir)
		if err != nil {
			continue
		}
		for _, wtEntry := range wtEntries {
			if !wtEntry.IsDir() {
				continue
			}
			names = append(names, repoEntry.Name()+"/"+wtEntry.Name())
		}
	}
	return names
}
