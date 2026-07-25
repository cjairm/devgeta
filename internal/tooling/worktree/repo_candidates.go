// Repo discovery and validation for the worktree create-flow's repo picker:
// ranking candidate repos from the cwd, the cursor repo, the recent-repos
// store, and zoxide, plus validating a free-typed repo path at selection
// time.

package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

// RepoCandidates returns a ranked, deduped list of candidate repo paths for a
// repo picker: the repo containing the process's current working directory
// first (so `n` suggests the repo you're already sitting in), then the
// cursor repo (the repo owning cursorRepoSlug's worktrees), then stored
// recent repos in most-recently-used order, then repos found by scanning
// Worktree.SearchPaths (opt-in — empty by default, so scanning contributes
// nothing until a user configures it), then zoxide's tracked directories when
// zoxide is installed. Every candidate is canonicalized before deduping
// (config.CanonicalRepoPath — the same contract every source must use) so the
// same repo is never offered twice regardless of which source produced it.
//
// A failure in one source never blanks the others: the recent-repos config
// may not exist yet on a fresh install, and zoxide may error transiently, but
// either case should still leave the caller with whatever candidates the
// remaining sources found.
func (w *WorktreeManager) RepoCandidates(cursorRepoSlug string) ([]string, error) {
	var raw []string

	if cwdRoot := w.cwdRepoRoot(); cwdRoot != "" {
		raw = append(raw, cwdRoot)
	}

	if cursorRoot := w.cursorRepoRoot(cursorRepoSlug); cursorRoot != "" {
		raw = append(raw, cursorRoot)
	}

	gc := &config.GlobalConfig{}
	if err := gc.Load(); err == nil {
		for _, r := range gc.Worktree.PrunedRecentRepos() {
			raw = append(raw, r.Path)
		}

		// Filesystem scan is opt-in: SearchPaths is empty until a user
		// configures it, and scanRepos returns nothing for an empty slice, so
		// this is zero behavior change for anyone who hasn't set it up.
		raw = append(raw, scanRepos(gc.Worktree.SearchPaths, gc.Worktree.ScanDepth)...)
	}

	if _, err := cmd.LookPathFn("zoxide"); err == nil {
		if zPaths, zErr := w.zoxideCandidates(); zErr == nil {
			raw = append(raw, zPaths...)
		}
	}

	seen := make(map[string]bool, len(raw))
	candidates := make([]string, 0, len(raw))
	for _, p := range raw {
		// Re-canonicalizing here is redundant for PrunedRecentRepos() entries
		// (already canonical when written to the store) but intentional: it's
		// cheap defense against any future candidate source added to raw above
		// that isn't pre-canonicalized, rather than an oversight.
		canonical := config.CanonicalRepoPath(p)
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		candidates = append(candidates, canonical)
	}
	return candidates, nil
}

// cwdRepoRoot resolves the main repo root for the process's current working
// directory, so `n` suggests "the repo you're sitting in" first when dg wt
// ui was launched from inside one — ranked ahead of the cursor repo, recent
// repos, and zoxide. Uses the same GetMainWorktree resolution as
// cursorRepoRoot (rather than a plain rev-parse --show-toplevel) so cwd
// being a linked worktree still resolves to its main repo root, matching
// what create actually needs. Returns "" (no error) when cwd can't be read
// or isn't inside a git repo at all — the cwd source is simply skipped, the
// same as every other candidate source in RepoCandidates.
func (w *WorktreeManager) cwdRepoRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	root, err := w.Git.GetMainWorktree(cwd)
	if err != nil {
		return ""
	}
	return root
}

// cursorRepoRoot resolves the root of the repo that owns cursorRepoSlug's
// worktrees, by reading any one worktree directory under the slug (all
// worktrees under a slug belong to the same repo) and resolving its main
// worktree root via git.GetMainWorktree. Returns "" (no error) when the slug
// is empty or has no worktrees on disk — the cursor repo is simply skipped as
// a candidate source in that case, rather than treated as a failure.
func (w *WorktreeManager) cursorRepoRoot(cursorRepoSlug string) string {
	if cursorRepoSlug == "" {
		return ""
	}
	repoDir := filepath.Join(GetWorktreeBasePath(), cursorRepoSlug)
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		wtPath := filepath.Join(repoDir, e.Name())
		if root, err := w.Git.GetMainWorktree(wtPath); err == nil {
			return root
		}
	}
	return ""
}

// zoxideCandidates runs `zoxide query -l` to list zoxide's tracked
// directories, for offering as repo picker candidates beyond what devgeta
// itself has recorded. Called only after the caller confirms zoxide is
// installed (via commands.LookPathFn), so a query failure here is a genuine
// error rather than "zoxide isn't installed".
func (w *WorktreeManager) zoxideCandidates() ([]string, error) {
	stdout, _, err := w.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Zoxide,
		Args:    []string{"query", "-l"},
	})
	if err != nil {
		return nil, err
	}
	var out []string
	for line := range strings.SplitSeq(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out, nil
}

// ValidateRepoPath validates a free-typed repo path at selection time so the
// picker can reject it immediately with a meaningful message rather than
// waiting until create: it must exist, be a directory, and be a git
// repository. Returns the repository's actual root, which may differ from
// path when path is a subdirectory of the repo rather than its root — the
// resolved root is what a caller should actually use to create a worktree.
func (w *WorktreeManager) ValidateRepoPath(path string) (string, error) {
	canonical := config.CanonicalRepoPath(path)

	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s", canonical)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", canonical)
	}

	root, err := w.Git.GetRepoRootIn(canonical)
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s: %w", canonical, err)
	}
	return root, nil
}

// ValidateDirPath is ValidateRepoPath's session counterpart: it validates a
// picked or free-typed folder for a standalone tmux session, which — unlike a
// worktree — is deliberately not tied to a git repo, so the only requirements
// are that the path exists and is a directory. It does NOT require (or resolve
// to) a repo root, so a plain folder like ~/Downloads is accepted as-is.
// Returns the canonicalized path, which is what a caller should hand to
// CreateSession as the session's working directory.
func (w *WorktreeManager) ValidateDirPath(path string) (string, error) {
	canonical := config.CanonicalRepoPath(path)

	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("path does not exist: %s", canonical)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory: %s", canonical)
	}
	return canonical, nil
}
