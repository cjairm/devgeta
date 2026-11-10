package worktree

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

// CreatedWorktree is one repo's result from CreateMany.
type CreatedWorktree struct {
	Repo   string
	Path   string
	Branch string
	// Window is empty when no window was requested or its launch failed.
	Window string
}

// multiTarget is one repo CreateMany works on.
type multiTarget struct {
	root          string
	slug          string
	path          string
	branchCreated bool // false when an existing local branch is adopted
}

// CreateMany creates the same worktree and branch in every repo of repoPaths
// ("." = current repo), optionally followed by a tmux window per repo.
//
// It runs in two phases because they fail differently:
//
//  1. Every worktree is created and nothing is launched. A failure rolls back
//     everything this call made, including the failing repo's partial state,
//     so the caller never ends up with the branch in some repos only. No coder
//     has started yet, so nothing can be lost.
//  2. Windows are launched (skipped when noWindow). A coder may already be
//     editing or committing by then, so a failed launch never removes a
//     worktree or branch; the failures are reported together and `dg wt repair`
//     is the fix.
//
// The returned results hold every repo created, even alongside a non-nil error
// from phase 2.
func (w *WorktreeManager) CreateMany(
	repoPaths []string,
	name, base string,
	layout Layout,
	force, noWindow bool,
) ([]CreatedWorktree, error) {
	if !noWindow {
		var err error
		if layout, err = validateLayout(layout); err != nil {
			return nil, err
		}
	}

	targets, err := w.validateTargets(repoPaths, name)
	if err != nil {
		return nil, err
	}
	roots := make([]string, len(targets))
	for i, t := range targets {
		roots[i] = t.root
	}

	// Checks and phase 1 run under the create lock: rollback trusts "the branch
	// was absent at check time" to mean this call created it, which only holds
	// if no other create of the same branch can run in between.
	err = withCreateLocks(roots, name, func() error {
		for i := range targets {
			if err := w.checkTarget(&targets[i], name, base, force); err != nil {
				return err
			}
		}
		for i, t := range targets {
			if err := w.addWorktree(t.root, t.path, name, base); err != nil {
				err = fmt.Errorf("%s: %w", t.slug, err)
				// targets[:i+1] includes the failing repo: CreateWorktreeIn can fail
				// after the worktree already exists (gitfile normalization).
				return errors.Join(err, w.rollback(targets[:i+1], name))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	results := make([]CreatedWorktree, 0, len(targets))
	for _, t := range targets {
		results = append(results, CreatedWorktree{Repo: t.slug, Path: t.path, Branch: name})
	}

	// Phase 2.
	var launchErrs []error
	for i, t := range targets {
		if !noWindow {
			windowName := GetWindowName(t.slug, name)
			if err := w.ensureWindow(t.slug, windowName, t.path, layout); err != nil {
				launchErrs = append(launchErrs, fmt.Errorf(
					"%s: window %s failed (%w); worktree kept at %s, run `devgeta worktree repair %s` from that repo",
					t.slug,
					windowName,
					err,
					t.path,
					name,
				))
				continue
			}
			results[i].Window = windowName
		}
		w.recordRepoUsed(t.root)
	}
	return results, errors.Join(launchErrs...)
}

// validateTargets resolves every path to a repo root and rejects two paths
// that would share a repo, a worktree path or a tmux window.
func (w *WorktreeManager) validateTargets(repoPaths []string, name string) ([]multiTarget, error) {
	seen := map[string]string{}
	// Window names are sanitized ("api.app" and "api_app" give the same one) and
	// the shared worktree path keys on the repo's base name only, so two
	// distinct repos can still land on one window or one directory.
	seenWindow := map[string]string{}
	seenPath := map[string]string{}
	var targets []multiTarget
	for _, p := range repoPaths {
		root, err := w.Git.GetRepoRootIn(paths.ExpandHome(p))
		if err != nil {
			return nil, fmt.Errorf("no git repository at %s: %w", p, err)
		}
		if first, dup := seen[root]; dup {
			return nil, fmt.Errorf(
				"--repo %s and --repo %s are the same repository (%s)",
				first,
				p,
				root,
			)
		}
		seen[root] = p

		slug := filepath.Base(root)
		wtPath := worktreePathIn(root, name)
		windowName := GetWindowName(slug, name)
		if first, dup := seenPath[wtPath]; dup {
			return nil, fmt.Errorf("--repo %s and --repo %s collide on worktree path %s", first, p, wtPath)
		}
		if first, dup := seenWindow[windowName]; dup {
			return nil, fmt.Errorf("--repo %s and --repo %s collide on tmux window %s", first, p, windowName)
		}
		seenPath[wtPath], seenWindow[windowName] = p, p

		targets = append(targets, multiTarget{root: root, slug: slug, path: wtPath})
	}
	return targets, nil
}

// checkTarget runs one repo's pre-create checks and records whether the branch
// already exists, which is how rollback tells a branch this call created from
// one it adopted. Callers hold the create lock, so that record stays true until
// phase 1 ends.
func (w *WorktreeManager) checkTarget(t *multiTarget, name, base string, force bool) error {
	if err := w.precheckCreate(t.root, name, force); err != nil {
		return fmt.Errorf("%s: %w", t.slug, err)
	}
	exists, err := w.Git.BranchExistsIn(t.root, name)
	if err != nil {
		return fmt.Errorf("%s: cannot check branch %s: %w", t.slug, name, err)
	}
	if exists && base != "" {
		return fmt.Errorf(
			"%s: branch %s already exists, so --base cannot apply; omit --base to adopt it",
			t.slug, name,
		)
	}
	t.branchCreated = !exists
	return nil
}

// rollback removes every worktree path that exists among targets, and every
// branch this call created whether or not its directory survived: git can
// create the branch, fail the checkout and remove the directory itself.
// Adopted branches are never deleted. It returns what it could not clean up,
// naming what is left on disk.
func (w *WorktreeManager) rollback(targets []multiTarget, name string) error {
	var errs []error
	for _, t := range targets {
		if _, err := os.Stat(t.path); err == nil {
			if err := w.Git.RemoveWorktree(t.path, false, ""); err != nil {
				errs = append(errs, fmt.Errorf(
					"rollback of %s failed, still on disk: %s (branch %s): %w",
					t.slug, t.path, name, err,
				))
				continue
			}
		}
		if !t.branchCreated {
			continue
		}
		exists, err := w.Git.BranchExistsIn(t.root, name)
		if err == nil && exists {
			err = w.Git.DeleteBranchIn(t.root, name, true)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf(
				"rollback of %s: branch %s may still exist: %w", t.slug, name, err,
			))
		}
	}
	return errors.Join(errs...)
}

// createLockTimeout bounds how long a create waits for another create that
// touches the same branch or directory. A var so tests can shorten it.
var createLockTimeout = time.Minute

// createLockPath is the lock file for creating branch name in repoRoot.
func createLockPath(repoRoot, name string) string {
	return lockFileFor("branch\x00" + repoRoot + "\x00" + name)
}

// createPathLockPath is the lock file for a worktree directory. Distinct branch
// names can share one ("feat/auth" and "feat-auth" both flatten to feat-auth),
// so the branch lock alone does not keep two creates off the same directory.
func createPathLockPath(wtPath string) string {
	return lockFileFor("path\x00" + wtPath)
}

func lockFileFor(key string) string {
	sum := sha256.Sum256([]byte(key))
	return paths.GetStateDir("devgeta", "worktree-locks", hex.EncodeToString(sum[:8])+".lock")
}

// withCreateLocks runs fn holding, for every repo in roots, the lock on branch
// name and the lock on its worktree directory. Locks are taken in sorted order
// so two creates over overlapping sets cannot deadlock.
func withCreateLocks(roots []string, name string, fn func() error) error {
	var lockPaths []string
	for _, root := range roots {
		lockPaths = append(lockPaths, createLockPath(root, name), createPathLockPath(worktreePathIn(root, name)))
	}
	slices.Sort(lockPaths)
	return withLocks(slices.Compact(lockPaths), fn)
}

func withLocks(lockPaths []string, fn func() error) error {
	if len(lockPaths) == 0 {
		return fn()
	}
	return files.WithLock(lockPaths[0], createLockTimeout, func() error {
		return withLocks(lockPaths[1:], fn)
	})
}
