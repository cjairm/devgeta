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

	"github.com/cjairm/devgeta/internal/apps/git"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

// RepoCandidates returns a ranked, deduped list of candidate repo paths for a
// repo picker: the cursor repo first (the repo the caller's cursor is pointing
// at), then the repo containing the process's current working directory, then
// stored recent repos in most-recently-used order, then repos found by scanning
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

	// Cursor before cwd, deliberately. The cursor is where the user just put
	// it; the cwd is wherever the dashboard happened to be launched from. When
	// both resolve - the common case, since `dg ws` is usually run from inside
	// a repo - ranking cwd first meant the top candidate never changed no
	// matter which row the cursor was on, which read as the cursor being
	// ignored entirely.
	if cursorRoot := w.cursorRepoRoot(cursorRepoSlug); cursorRoot != "" {
		raw = append(raw, cursorRoot)
	}

	if cwdRoot := w.cwdRepoRoot(); cwdRoot != "" {
		raw = append(raw, cwdRoot)
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

// cursorRepoRoot resolves the root of the repo whose slug is cursorRepoSlug.
// Returns "" (no error) when the slug is empty or unresolvable — the cursor
// repo is simply skipped as a candidate source in that case, rather than
// treated as a failure.
//
// This answers a different question than enumerateWorktrees() (worktree.go)
// can answer: "what is this specific slug's root", independent of whether
// that repo currently has *any* linked worktrees. A repo whose only worktree
// is its own main checkout (zero linked worktrees) produces zero rows from
// enumerateWorktrees() — correctly, per List()'s contract — but
// cursorRepoRoot must still be able to resolve its root. So this builds on
// forEachKnownRepo (worktree.go) — the same group/anchor/dedup/early-exit
// walk enumerateWorktrees uses — stopping the moment a group's resolved main
// root matches the target slug, rather than collecting every worktree row
// the way enumerateWorktrees does.
// A tmux session name is accepted as well as a repo slug, because the caller's
// cursor can be sitting on a session row and a session is the only thing such
// a row knows about itself. The two are not interchangeable strings:
// TmuxSessionName rewrites ".", ":" and whitespace to "_", so the repo
// "my.tools" owns the session "my_tools" and a direct comparison would miss
// it. Matching each known repo's own session name against the target is the
// only direction that works, since the rewrite cannot be undone (several
// slugs can map to one session name, and "_" is a legal character in a repo
// name to begin with).
//
// An exact slug match is preferred over a session match, so a repo literally
// named like the target always wins over one that merely maps to it.
func (w *WorktreeManager) cursorRepoRoot(cursorRepoSlug string) string {
	if cursorRepoSlug == "" {
		return ""
	}
	var found, viaSession string
	w.forEachKnownRepo(func(mainRoot string, _ []git.WorktreeInfo) bool {
		slug := filepath.Base(mainRoot)
		if slug == cursorRepoSlug {
			found = mainRoot
			return false
		}
		if viaSession == "" && TmuxSessionName(slug) == cursorRepoSlug {
			viaSession = mainRoot
		}
		return true
	})
	if found != "" {
		return found
	}
	return viaSession
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
