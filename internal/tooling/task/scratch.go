package task

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjairm/devgeta/pkg/paths"
)

// Scratch allocates a fresh, uniquely-named directory under devgeta's
// scratch root (ADR-0015) and returns its absolute path — the destination
// shipped commands write scratch files to instead of /tmp.
//
// Calls paths.EnsureScratchDir() first rather than assuming configure
// already created the root: the root lives under the cache directory
// specifically because a user or a cleaner may empty it at any time
// (ADR-0015 §1), so allocation has to be able to recreate it.
func (tm *TaskManager) Scratch() (string, error) {
	root, err := paths.EnsureScratchDir()
	if err != nil {
		return "", fmt.Errorf("scratch: %w", err)
	}
	dir, err := os.MkdirTemp(root, paths.ScratchAllocPrefix+"*")
	if err != nil {
		return "", fmt.Errorf("scratch: %w", err)
	}
	return dir, nil
}

// ScratchClean removes a directory allocated by Scratch.
//
// It accepts only a real directory that is a direct child of the scratch
// root and carries paths.ScratchAllocPrefix, checked after canonicalizing
// the given path. Refused: the root itself, a grandchild, a directory beside
// the root, a child lacking the prefix, a `..` escape, a relative path, and
// **any** symlink — resolvable or not, since os.MkdirTemp never allocates
// one. "Inside the root" alone is not the contract: it would let one
// invocation delete every concurrent session's directory at once
// (ADR-0015 §3).
//
// Idempotent: a target that no longer exists returns nil rather than an
// error, so a command's cleanup step can run on a retried or already-cleaned
// path without failing the command.
//
// Not covered: a session cleaning a sibling session's directory by guessing
// its random suffix. Closing that needs per-invocation ownership state, and
// the only parties able to do it are the same user's own agent sessions —
// the bound stops here deliberately, not by oversight.
func (tm *TaskManager) ScratchClean(target string) error {
	root, err := paths.EnsureScratchDir()
	if err != nil {
		return fmt.Errorf("scratch --clean: %w", err)
	}
	canonRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("scratch --clean: %w", err)
	}

	if !filepath.IsAbs(target) {
		return fmt.Errorf("scratch --clean: %q is not an allocated scratch directory", target)
	}
	cleaned := filepath.Clean(target)

	// Bounds are checked LEXICALLY first, before existence. filepath.Clean
	// has already collapsed any `..`, so an escape like
	// `<root>/task-x/../../elsewhere` resolves to a path that may well not
	// exist — and an out-of-bounds target must be an error whether or not
	// something is there, never a silent success from the not-exists branch
	// below. Accept either spelling of the root: what Scratch() printed is
	// built from the raw root, but a caller may equally hand back a fully
	// resolved path.
	lexRel, ok := scratchChildName(root, cleaned)
	if !ok {
		lexRel, ok = scratchChildName(canonRoot, cleaned)
	}
	if !ok {
		return fmt.Errorf("scratch --clean: %q is not an allocated scratch directory", target)
	}
	if err := validScratchChild(lexRel, target); err != nil {
		return err
	}

	// Classify before resolving. An earlier version fell back to the lexical
	// path whenever EvalSymlinks failed, which quietly let a broken or
	// looping symlink through — a live symlink out of the root was refused,
	// but an unresolvable one was not, so the contract held only for the
	// case that happened to resolve. (Its blast radius was smaller than it
	// looks: os.RemoveAll never follows a top-level symlink, so it unlinked
	// the link and left the target untouched — verified directly. It was
	// still an inconsistent contract.)
	lst, err := os.Lstat(cleaned)
	switch {
	case os.IsNotExist(err):
		// In-bounds but already gone (a retried or doubled cleanup).
		// Idempotent by design — see this function's doc comment.
		return nil
	case err != nil:
		return fmt.Errorf("scratch --clean: %w", err)
	case lst.Mode()&os.ModeSymlink != 0:
		// Scratch() allocates with os.MkdirTemp, which never produces a
		// symlink — so this was not allocated by us, whatever it points at.
		return fmt.Errorf(
			"scratch --clean: %q is a symlink, not a directory allocated by `devgeta task scratch`",
			target,
		)
	}

	// Re-check after resolution: the lexical pass above cannot see a
	// symlinked ANCESTOR that moves the target out of the root.
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return fmt.Errorf("scratch --clean: %w", err)
	}
	rel, ok := scratchChildName(canonRoot, resolved)
	if !ok {
		return fmt.Errorf("scratch --clean: %q is not under the scratch root", target)
	}
	if err := validScratchChild(rel, target); err != nil {
		return err
	}

	if err := os.RemoveAll(filepath.Join(canonRoot, rel)); err != nil {
		return fmt.Errorf("scratch --clean: %w", err)
	}
	return nil
}

// scratchChildName returns path's location relative to base, and false when
// path is base itself or sits outside it.
func scratchChildName(base, path string) (string, bool) {
	rel, err := filepath.Rel(base, path)
	if err != nil ||
		rel == "." ||
		rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// validScratchChild enforces the two remaining ownership rules on an
// already-in-bounds relative name: direct child, carrying the allocation
// prefix.
func validScratchChild(rel, target string) error {
	if strings.Contains(rel, string(filepath.Separator)) {
		return fmt.Errorf("scratch --clean: %q is not a direct child of the scratch root", target)
	}
	if !strings.HasPrefix(rel, paths.ScratchAllocPrefix) {
		return fmt.Errorf("scratch --clean: %q was not allocated by `devgeta task scratch`", target)
	}
	return nil
}
