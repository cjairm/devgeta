package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cjairm/devgeta/pkg/files"
)

// globalConfigLockFile is the sidecar lock's filename, next to
// global_config.yaml. It must never be the config file itself: Save()
// replaces global_config.yaml by rename (see WriteFileAtomic), so the
// config file's inode changes on every save and a lock held on it would be
// silently orphaned the moment another writer's Save() completes.
const globalConfigLockFile = "global_config.lock"

// lockAcquireTimeout bounds how long acquireLock blocks waiting for the
// sidecar lock before giving up. A wedged holder (crashed mid-debug,
// deadlocked, etc.) must produce an actionable timeout error instead of
// hanging Update forever. It is a var, not a const, so tests can shorten it
// to keep the timeout test fast; production code never changes it.
var lockAcquireTimeout = 10 * time.Second

func getGlobalConfigLockFilePath() string {
	return filepath.Join(filepath.Dir(GlobalConfigFilePath()), globalConfigLockFile)
}

// Update runs fn against a freshly loaded config while holding an exclusive
// lock, then saves. Load and Save inside one lock is the point: Save alone is
// atomic (temp file + rename) but not lost-update-safe, so a plain
// Load/mutate/Save pair can silently discard a concurrent writer's change —
// the later Save() simply overwrites the earlier one with a copy that never
// saw it. Holding the lock across the whole load-mutate-save cycle closes
// that window: only one Update at a time can be between its Load and its
// Save.
//
// Update also owns first-run initialization: if no config file exists yet,
// Load's os.IsNotExist is treated as "start from a zero-value GlobalConfig{}"
// rather than an error. Callers must NOT call Create() first to work around
// a missing file — a pre-Update Create() writes a blank file (via Reset())
// OUTSIDE the lock, so two concurrent first-run callers can interleave: both
// see the file absent, both start writing a blank file, and whichever blank
// write lands last silently wipes out the other caller's Update, lock or no
// lock. Handling the missing-file case here, inside the lock, means exactly
// one write happens per Update call and it already contains fn's change.
//
// If fn returns an error, the config is not saved and Update returns that
// error unchanged.
//
// The locking mechanism itself is files.WithLock — the same one the review
// journal's writes use, because it is the same problem and CLAUDE.md's reuse
// rule puts it in one place rather than two that could drift. What stays here
// is only what is specific to the config: WHICH file is locked, how long a
// caller waits for it, and a "config:" prefix so an error names which lock
// timed out.
func Update(fn func(gc *GlobalConfig) error) error {
	// inside is what happened WITHIN the lock, kept apart from what WithLock
	// itself returns so the two can be told apart afterwards: the body's errors
	// (fn's above all) must reach the caller exactly as they are, while a lock
	// failure arrives bare and is the only one that needs saying which lock.
	var inside error
	err := files.WithLock(getGlobalConfigLockFilePath(), lockAcquireTimeout, func() error {
		inside = updateLocked(fn)
		return inside
	})
	if err != nil {
		if inside != nil {
			return inside
		}
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

// updateLocked is Update's load-mutate-save cycle, which runs only with the
// lock held.
func updateLocked(fn func(gc *GlobalConfig) error) error {
	gc := &GlobalConfig{}
	// readGlobalConfigFile, not gc.Load(): Update's whole purpose is to
	// guarantee this read is fresh under the lock. Going through Load()'s
	// cache would let two concurrent Update calls both hit an entry
	// populated before either acquired the lock, both mutate from that same
	// stale base, and the second Save() below would silently discard the
	// first Update's change - the lost-update bug this lock exists to
	// prevent, reintroduced through the cache instead of a race on the file.
	// Save() at the end of this function still refreshes the cache like any
	// other write - only the read bypasses it.
	if err := readGlobalConfigFile(gc); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("config: failed to load: %w", err)
		}
		// No config file yet: gc is already the zero value here (Load's
		// os.ReadFile failed before yaml.Unmarshal ever ran, so nothing wrote
		// to it), so it's already the right starting point. Do not write
		// anything here — the only write for this Update happens below, after
		// fn runs, so it's the first write and it already contains fn's
		// change. See the Update doc comment for the race this avoids.
	}
	if err := fn(gc); err != nil {
		return err
	}
	if err := gc.Save(); err != nil {
		return fmt.Errorf("config: failed to save: %w", err)
	}
	return nil
}
