package task

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjairm/devgeta/pkg/paths"
)

// Scratch allocates a directory under devgeta's scratch root (ADR-0015) and
// returns its absolute path — the destination shipped commands write scratch
// files to instead of /tmp.
//
// An empty key keeps today's behavior byte-for-byte: a fresh, uniquely-named
// directory via os.MkdirTemp, unrelated to any prior call. A non-empty key
// (ADR-0033) is re-derivable instead of unique: the same key always maps to
// the same path, so a later, independent session can find a file an earlier
// one left there without either process telling the other a random suffix.
// That determinism is opt-in and only reachable by passing a key — the
// default path's isolation guarantee is unchanged.
//
// Calls paths.EnsureScratchDir() first rather than assuming configure
// already created the root: the root lives under the cache directory
// specifically because a user or a cleaner may empty it at any time
// (ADR-0015 §1), so allocation has to be able to recreate it.
func (tm *TaskManager) Scratch(key string) (string, error) {
	root, err := paths.EnsureScratchDir()
	if err != nil {
		return "", fmt.Errorf("scratch: %w", err)
	}
	if key == "" {
		dir, err := os.MkdirTemp(root, paths.ScratchAllocPrefix+"*")
		if err != nil {
			return "", fmt.Errorf("scratch: %w", err)
		}
		return dir, nil
	}

	if err := validateScratchKey(key); err != nil {
		return "", fmt.Errorf("scratch --key: %w", err)
	}
	dir := filepath.Join(root, paths.ScratchKeyPrefix+key)
	if err := reuseOrCreateKeyedScratchDir(root, dir); err != nil {
		return "", fmt.Errorf("scratch --key: %w", err)
	}
	return dir, nil
}

// reuseOrCreateKeyedScratchDir makes dir exist as a real directory under
// root, creating it on first use and reusing it on every later call with the
// same key — but only after proving what is already there is safe to reuse
// (ADR-0033).
//
// A symlink or a non-directory found at dir is refused outright, never
// resolved-and-judged: nothing this allocator ever creates is a symlink or a
// plain file, so either one found there was substituted by something else,
// and writing through it would write to whatever that something else points
// at. Containment under root is re-checked after resolving symlinks, the
// same defense ScratchClean applies on the delete side, so a symlinked
// ANCESTOR that quietly moved dir outside root is also caught.
func reuseOrCreateKeyedScratchDir(root, dir string) error {
	lst, err := os.Lstat(dir)
	switch {
	case os.IsNotExist(err):
		if mkErr := os.Mkdir(dir, 0o700); mkErr != nil {
			return fmt.Errorf("failed to create %q: %w", dir, mkErr)
		}
		return nil
	case err != nil:
		return fmt.Errorf("failed to stat %q: %w", dir, err)
	case lst.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%q is a symlink; refusing to reuse it as a scratch directory", dir)
	case !lst.IsDir():
		return fmt.Errorf("%q exists and is not a directory", dir)
	}

	canonRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("failed to resolve scratch root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve %q: %w", dir, err)
	}
	if _, ok := scratchChildName(canonRoot, resolved); !ok {
		return fmt.Errorf("%q is not contained under the scratch root", dir)
	}
	return nil
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

// scratchKeyDisallowedChars breaks a key's role as a single path element
// under the scratch root if present anywhere in it.
const scratchKeyDisallowedChars = "/\\\x00"

// validateScratchKey rejects any --key value that could not survive as a
// single, contained path element under the scratch root: path separators
// (forward and backward slash, so the rule is the same regardless of which
// one the host OS treats as significant), a null byte, the special entries
// "." and "..", and anything empty or made only of whitespace. It is a pure
// function so its adversarial table (TestScratchKeyValidation) doubles as
// the manual verification for this boundary, per ADR-0033.
//
// filepath.Base is checked in addition to the explicit character scan as a
// second, independent layer — the same defense-in-depth style
// validScratchChild and ScratchClean already use for the delete side of this
// same boundary: a key that passes the character scan but still would not
// round-trip through filepath.Base unchanged is refused too, rather than
// trusting one check alone to be complete.
func validateScratchKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("scratch key must not be empty or whitespace-only")
	}
	if key == "." || key == ".." {
		return fmt.Errorf("scratch key %q is not allowed", key)
	}
	if strings.ContainsAny(key, scratchKeyDisallowedChars) {
		return fmt.Errorf("scratch key %q must not contain a path separator", key)
	}
	if filepath.Base(key) != key {
		return fmt.Errorf("scratch key %q must be a single path element", key)
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
// already-in-bounds relative name: direct child, carrying either allocation
// prefix — paths.ScratchAllocPrefix (an unkeyed, unique allocation) or
// paths.ScratchKeyPrefix (a keyed, re-derivable one, ADR-0033) — so --clean
// works uniformly on both forms.
func validScratchChild(rel, target string) error {
	if strings.Contains(rel, string(filepath.Separator)) {
		return fmt.Errorf("scratch --clean: %q is not a direct child of the scratch root", target)
	}
	if !strings.HasPrefix(rel, paths.ScratchAllocPrefix) &&
		!strings.HasPrefix(rel, paths.ScratchKeyPrefix) {
		return fmt.Errorf("scratch --clean: %q was not allocated by `devgeta task scratch`", target)
	}
	return nil
}
