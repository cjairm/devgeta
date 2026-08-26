package devgeta

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cjairm/devgeta/pkg/buildinfo"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
)

// The extracted embedded config tree is committed through a symlink pointer,
// not by rewriting a directory in place.
//
// Layout under paths.Paths.App.Root:
//
//	configs                -> configs-<stamp>   (the pointer; what everything reads)
//	configs-<stamp>/        the extracted tree for one build
//	configs.legacy          transient, migration only (see reconcileLegacyMigration)
//
// Why a pointer: `rename(2)` refuses to replace a non-empty directory (and on
// APFS even an empty one), so there is no atomic directory-over-directory swap.
// Renaming a symlink over a symlink *is* atomic, so a reader always resolves
// `configs` to one complete tree or the other — never to a missing or partial
// one, which is what CLAUDE.md §4 ("state must be atomic") requires.
//
// FORWARD CONSTRAINT for anyone adding code that reads this tree:
// os.Stat/os.ReadDir/os.ReadFile follow the pointer transparently, so ordinary
// reads need no change. filepath.Walk/WalkDir do NOT: they lstat their root, so
// a walker rooted at `configs` would visit the link itself and silently descend
// into nothing. A walker must be rooted at the resolved target instead.
var (
	// configsPointerName is the stable path every reader uses.
	configsPointerName = constants.App.Dir.Configs

	// stampedPrefix prefixes each build's extracted tree. The stamp lives in
	// the directory name rather than in a side-car file so "is this tree
	// current?" is a readlink-and-compare that cannot drift from the tree it
	// describes.
	stampedPrefix = configsPointerName + "-"

	// legacyName holds the pre-migration real `configs` directory while the
	// one-time migration swaps a pointer into its place.
	legacyName = configsPointerName + ".legacy"

	// tempPointerPrefix names the throwaway symlink that gets renamed over the
	// pointer. It deliberately does NOT match stampedDirPattern, so the debris
	// sweep never treats an in-flight swap as garbage.
	tempPointerPrefix = configsPointerName + ".tmp-"
)

// stampedDirPattern matches exactly the directory names stampedDirName can
// produce. Removal is gated on it, so it must stay in sync with sanitizeStamp.
var stampedDirPattern = regexp.MustCompile(`^` + stampedPrefix + `[A-Za-z0-9._-]+$`)

// tempPointerPattern matches exactly the names newTempPointerPath can
// produce: tempPointerPrefix followed by the creating process's pid and an
// attempt index. sweepStaleTempPointers is gated on it the same way
// sweepStaleExtracts is gated on stampedDirPattern, so it can only ever
// collect a temp pointer — never an arbitrary path that happens to sit
// under the app root.
//
// No liveness or age check gates removal, matching sweepStaleExtracts'
// own precedent right above: a concurrent second devgeta process racing this
// sweep is pre-existing exposure this file already accepts elsewhere (see
// TestSwapPointer_ReaderNeverSeesAnAbsentOrPartialTree's doc comment), not
// something this sweep newly introduces or needs to newly guard against —
// the window between swapPointer's os.Symlink and its os.Rename is a
// handful of syscalls, and a temp pointer's name is unique per attempt
// (pid + index), so this sweep and a concurrent swapPointer can only ever
// collide on the exact same in-flight name in that narrow window.
var tempPointerPattern = regexp.MustCompile(
	`^` + regexp.QuoteMeta(tempPointerPrefix) + `[0-9]+-[0-9]+$`,
)

// sanitizeStamp reduces a build-info field to the character set
// stampedDirPattern accepts, so a stamp can never introduce a path separator
// or a name the removal guard would later refuse to clean up.
func sanitizeStamp(s string) string {
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, s)
	if mapped == "" {
		return "unknown"
	}
	return mapped
}

// buildStamp identifies the running binary's embedded config content.
//
// Accepted trade-off (deliberate, not an oversight): a plain `go build` with no
// ldflags leaves buildinfo.Version == "dev" and Commit == "unknown", so two
// different local builds share one stamp and InstallIfStale treats the second
// as already current. `dg configure --force` re-extracts unconditionally and is
// the repair path for exactly that case.
func buildStamp() string {
	return sanitizeStamp(buildinfo.Version) + "-" + sanitizeStamp(buildinfo.Commit)
}

// stampedDirName is the basename of the current build's extracted tree.
func stampedDirName() string { return stampedPrefix + buildStamp() }

func appRoot() string        { return paths.Paths.App.Root }
func pointerPath() string    { return filepath.Join(appRoot(), configsPointerName) }
func legacyPath() string     { return filepath.Join(appRoot(), legacyName) }
func stampedDirPath() string { return filepath.Join(appRoot(), stampedDirName()) }

// resolveManagedTarget resolves path and returns it only if it is a config tree
// devgeta itself created: a directory sitting directly under
// paths.Paths.App.Root whose name matches stampedDirPattern.
//
// This guard is load-bearing, not defensive padding. paths.Paths.App.Root is an
// ordinary user-writable directory, so without it a resolve-then-RemoveAll of
// the pointer turns a harmless link cleanup into an arbitrary-directory delete
// driven by whatever anything else on the machine pointed `configs` at
// (CLAUDE.md §4: "user input must always be validated before use, especially
// paths").
func resolveManagedTarget(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", path, err)
	}
	resolved = filepath.Clean(resolved)

	// The app root is resolved too, so a root reached through a symlinked
	// component (a $TMPDIR under /var on macOS, a symlinked $HOME) still
	// compares equal instead of failing the check.
	root, err := filepath.EvalSymlinks(appRoot())
	if err != nil {
		return "", fmt.Errorf("failed to resolve app root %s: %w", appRoot(), err)
	}
	root = filepath.Clean(root)

	if filepath.Dir(resolved) != root {
		return "", fmt.Errorf(
			"refusing to remove %s: resolves to %s, which is not directly under the app root %s",
			path, resolved, root,
		)
	}
	if !stampedDirPattern.MatchString(filepath.Base(resolved)) {
		return "", fmt.Errorf(
			"refusing to remove %s: resolves to %s, which is not a devgeta-managed config extract",
			path, resolved,
		)
	}
	return resolved, nil
}

// removeManagedTarget removes an extracted tree after validating it.
func removeManagedTarget(path string) error {
	resolved, err := resolveManagedTarget(path)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(resolved); err != nil {
		return fmt.Errorf("failed to remove config extract %s: %w", resolved, err)
	}
	return nil
}

// removeConfigsPointer removes the `configs` entry and, when it is safe to do
// so, the tree it points at.
//
//   - Missing: nothing to do.
//   - Not a symlink: the pre-migration shape (or a directory a caller made by
//     hand). Removed directly, exactly as before this protocol existed.
//   - A symlink: the target is removed only if resolveManagedTarget accepts it.
//     Otherwise only the link goes, the target is left untouched, and the reason
//     is logged — an unexpected target is never guessed at.
func removeConfigsPointer(pointer string) error {
	info, err := os.Lstat(pointer)
	if err != nil {
		if os.IsNotExist(err) {
			logger.L().Debugw("Configs pointer not found", "path", pointer)
			return nil
		}
		return fmt.Errorf("failed to inspect configs pointer %s: %w", pointer, err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		logger.L().Debugw("Removing legacy configs directory", "path", pointer)
		if err := os.RemoveAll(pointer); err != nil {
			return fmt.Errorf("failed to remove configs directory %s: %w", pointer, err)
		}
		return nil
	}

	if err := removeManagedTarget(pointer); err != nil {
		logger.L().Warnw(
			"Configs pointer does not resolve to a devgeta-managed extract; removing only the link and leaving its target alone",
			"path", pointer,
			"reason", err,
		)
	}
	if err := os.Remove(pointer); err != nil {
		return fmt.Errorf("failed to remove configs pointer %s: %w", pointer, err)
	}
	return nil
}

// newTempPointerPath returns an unused path for the throwaway symlink used to
// commit a swap. It is unique per attempt so debris from an interrupted earlier
// attempt can never block a retry.
func newTempPointerPath(root string) (string, error) {
	for i := 0; i < 100; i++ {
		candidate := filepath.Join(root, fmt.Sprintf("%s%d-%d", tempPointerPrefix, os.Getpid(), i))
		if _, err := os.Lstat(candidate); err != nil {
			if os.IsNotExist(err) {
				return candidate, nil
			}
			return "", fmt.Errorf("failed to inspect %s: %w", candidate, err)
		}
		// A leftover from an interrupted attempt: clear it and reuse the name.
		if err := os.Remove(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("failed to find an unused temporary pointer name under %s", root)
}

// swapPointer atomically points `configs` at target. The caller must have
// ensured `configs` is either absent or already a symlink — a symlink cannot be
// renamed over a real directory.
func swapPointer(root, pointer, target string) error {
	tmp, err := newTempPointerPath(root)
	if err != nil {
		return err
	}
	// A relative link keeps the pointer valid if the whole app root is moved,
	// and makes the staleness check a plain readlink comparison.
	if err := os.Symlink(filepath.Base(target), tmp); err != nil {
		return fmt.Errorf("failed to create temporary configs pointer %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, pointer); err != nil {
		if rmErr := os.Remove(tmp); rmErr != nil {
			logger.L().
				Warnw("Failed to clean up temporary configs pointer", "path", tmp, "error", rmErr)
		}
		return fmt.Errorf("failed to commit configs pointer %s: %w", pointer, err)
	}
	return nil
}

// commitPointer publishes target as the live config tree.
//
// When `configs` is absent or already a symlink this is the single atomic
// rename of swapPointer. When it is still a real directory — every install that
// predates this protocol — the directory is first renamed aside to
// `configs.legacy`, which is what makes the one transition that cannot be a
// single syscall *recoverable*: between the rename and the swap there is an
// instant with no `configs`, but the complete old tree is still on disk under
// `configs.legacy`, and reconcileLegacyMigration resumes from there.
func commitPointer(root, pointer, target string) error {
	info, err := os.Lstat(pointer)
	switch {
	case err != nil && os.IsNotExist(err):
		return swapPointer(root, pointer, target)
	case err != nil:
		return fmt.Errorf("failed to inspect configs pointer %s: %w", pointer, err)
	case info.Mode()&os.ModeSymlink != 0:
		return swapPointer(root, pointer, target)
	}

	// Migration steps 2-4: real directory -> pointer.
	legacy := legacyPath()
	if err := os.Rename(pointer, legacy); err != nil {
		return fmt.Errorf("failed to move the existing configs directory aside: %w", err)
	}
	if err := swapPointer(root, pointer, target); err != nil {
		// Put the old tree back so the install is left usable; if even that
		// fails, reconcileLegacyMigration recovers it on the next run.
		if undoErr := os.Rename(legacy, pointer); undoErr != nil {
			logger.L().Warnw(
				"Failed to restore the previous configs directory after a failed pointer swap; the next run will recover it",
				"legacy", legacy,
				"error", undoErr,
			)
		}
		return err
	}
	if err := os.RemoveAll(legacy); err != nil {
		logger.L().
			Warnw("Failed to remove the migrated configs directory", "path", legacy, "error", err)
	}
	return nil
}

// reconcileLegacyMigration resumes a one-time migration that was interrupted
// partway through, and must run before anything else inspects the tree.
//
// The migration is: (1) extract to configs-<stamp>, (2) rename configs ->
// configs.legacy, (3) symlink configs-<stamp> into place as configs,
// (4) remove configs.legacy. Only steps 2 and 3 leave an observable
// intermediate state, and `configs.legacy` existing is what marks one. On
// return, `configs.legacy` no longer exists and `configs` is either absent, a
// real directory (which the caller then migrates), or the live pointer.
func reconcileLegacyMigration() error {
	legacy := legacyPath()
	if _, err := os.Lstat(legacy); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to inspect %s: %w", legacy, err)
	}

	pointer := pointerPath()
	info, err := os.Lstat(pointer)
	switch {
	case err != nil && os.IsNotExist(err):
		// Interrupted between step 2 and step 3: `configs.legacy` holds the
		// only copy of the tree. Put it back and let the caller migrate again.
		logger.L().
			Infow("Resuming an interrupted configs migration: restoring the previous tree", "from", legacy, "to", pointer)
		if err := os.Rename(legacy, pointer); err != nil {
			return fmt.Errorf("failed to restore %s from %s: %w", pointer, legacy, err)
		}
		return nil

	case err != nil:
		return fmt.Errorf("failed to inspect configs pointer %s: %w", pointer, err)

	case info.Mode()&os.ModeSymlink != 0:
		// Interrupted between step 3 and step 4: the pointer is already live,
		// so `configs.legacy` is a leftover copy of the superseded tree.
		logger.L().
			Infow("Resuming an interrupted configs migration: dropping the superseded tree", "path", legacy)
		if err := os.RemoveAll(legacy); err != nil {
			return fmt.Errorf("failed to remove %s: %w", legacy, err)
		}
		return nil

	default:
		// `configs` is a real directory again while `configs.legacy` still
		// exists. A rename is atomic, so the single-process migration cannot
		// produce this; it takes outside interference (an older devgeta binary
		// re-extracting into `configs`, or a tree restored by hand). Either
		// way `configs` is authoritative and `configs.legacy` is redundant:
		// drop it and let the caller migrate from step 1.
		logger.L().
			Infow("Dropping a redundant configs.legacy left beside a real configs directory", "path", legacy)
		if err := os.RemoveAll(legacy); err != nil {
			return fmt.Errorf("failed to remove %s: %w", legacy, err)
		}
		return nil
	}
}

// sweepStaleExtracts removes every stamped extract under root except keep
// (pass "" to remove them all). It recovers debris an interrupted extract or a
// failed post-swap removal left behind — that recovery is the whole reason the
// extract directories carry a stamp in their name. Failures are logged, never
// fatal: leftover bytes are not worth failing an otherwise good install over.
func sweepStaleExtracts(root, keep string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.L().
				Warnw("Failed to scan the app root for stale config extracts", "path", root, "error", err)
		}
		return
	}
	for _, entry := range entries {
		// ReadDir lstats, so a symlink is not reported as a directory: only
		// real extract directories are swept, never anything's link.
		if !entry.IsDir() || !stampedDirPattern.MatchString(entry.Name()) {
			continue
		}
		if keep != "" && entry.Name() == keep {
			continue
		}
		path := filepath.Join(root, entry.Name())
		logger.L().Debugw("Removing stale config extract", "path", path)
		if err := removeManagedTarget(path); err != nil {
			logger.L().Warnw("Failed to remove stale config extract", "path", path, "error", err)
		}
	}
}

// sweepStaleTempPointers removes orphaned configs.tmp-* symlinks: debris
// left when a process was killed between swapPointer's os.Symlink and its
// os.Rename. Nothing else ever collects these — sweepStaleExtracts
// deliberately excludes them (see tempPointerPrefix's doc comment) so it can
// never mistake an in-flight swap for garbage — and without this, Uninstall's
// "remove the app root if empty" branch can never fire while one lingers.
// Scoped separately from sweepStaleExtracts because it validates a
// completely different shape (a symlink matching tempPointerPattern, not a
// directory matching stampedDirPattern) — see tempPointerPattern's own doc
// comment for why no age or liveness check is needed on top of that.
func sweepStaleTempPointers(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.L().Warnw(
				"Failed to scan the app root for orphaned temp config pointers",
				"path", root,
				"error", err,
			)
		}
		return
	}
	for _, entry := range entries {
		if !tempPointerPattern.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		// Lstat, not Stat: a temp pointer is a symlink and its target may
		// itself have already been swept, but the link entry is what this
		// sweep is validating and removing.
		info, err := os.Lstat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.L().
					Warnw("Failed to inspect temp config pointer", "path", path, "error", err)
			}
			continue
		}
		// Validated-removal discipline, same spirit as resolveManagedTarget:
		// only ever remove an entry that is itself a symlink directly under
		// root and matches the temp-pointer name pattern, never a directory
		// or file that merely happens to share the name.
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		logger.L().Debugw("Removing orphaned temp config pointer", "path", path)
		if err := os.Remove(path); err != nil {
			logger.L().
				Warnw("Failed to remove orphaned temp config pointer", "path", path, "error", err)
		}
	}
}

// pointerIsCurrent reports whether `configs` already points at this build's
// extract and that extract is really there.
func pointerIsCurrent(pointer string) bool {
	// A leftover configs.legacy means a migration is unfinished; the tree may
	// look right but still needs the repair pass, so never skip on it.
	if _, err := os.Lstat(legacyPath()); err == nil {
		return false
	}

	info, err := os.Lstat(pointer)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	dest, err := os.Readlink(pointer)
	if err != nil || filepath.Base(dest) != stampedDirName() {
		return false
	}
	// The name matching is not enough: the target must actually exist, or a
	// swept-away or partially-removed tree would read as current.
	target, err := os.Stat(pointer)
	return err == nil && target.IsDir()
}
