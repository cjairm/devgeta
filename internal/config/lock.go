package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/logger"
	"golang.org/x/sys/unix"
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

// lockPollInterval is how often acquireLock retries a non-blocking flock
// attempt while waiting out lockAcquireTimeout. unix.Flock has no built-in
// timeout and LOCK_EX blocks forever, so polling with LOCK_EX|LOCK_NB is used
// instead of a blocking call on a goroutine: a goroutine stuck in a blocking
// syscall past the timeout can't be cancelled and would leak for as long as
// the other holder keeps the lock.
var lockPollInterval = 25 * time.Millisecond

func getGlobalConfigLockFilePath() string {
	return filepath.Join(filepath.Dir(getGlobalConfigFilePath()), globalConfigLockFile)
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
func Update(fn func(gc *GlobalConfig) error) error {
	unlock, err := acquireLock()
	if err != nil {
		return err
	}
	defer func() {
		if unlockErr := unlock(); unlockErr != nil {
			logger.L().Warnw("config: failed to release lock", "error", unlockErr)
		}
	}()

	gc := &GlobalConfig{}
	if err := gc.Load(); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("config: failed to load: %w", err)
		}
		// No config file yet: gc is already the zero value here (Load's
		// os.ReadFile failed before yaml.Unmarshal ever ran, so nothing wrote
		// to it), so it's already the right starting point. Do not write
		// anything here — the only write for this Update happens below,
		// after fn runs, so it's the first write and it already contains
		// fn's change. See the Update doc comment for the race this avoids.
	}
	if err := fn(gc); err != nil {
		return err
	}
	if err := gc.Save(); err != nil {
		return fmt.Errorf("config: failed to save: %w", err)
	}
	return nil
}

// acquireLock opens (creating if needed) the sidecar lock file and blocks
// until it holds an exclusive flock on it, or lockAcquireTimeout elapses. On
// success it returns a function that releases the lock and closes the file
// descriptor; the caller must call it exactly once.
//
// unix.Flock(LOCK_EX) is chosen over an O_CREATE|O_EXCL lockfile because the
// OS releases a flock automatically when the holding process exits —
// including on crash — so there is no stale-lock case to detect or clean up.
func acquireLock() (func() error, error) {
	lockPath := getGlobalConfigLockFilePath()
	if err := os.MkdirAll(filepath.Dir(lockPath), files.DirPermission); err != nil {
		return nil, fmt.Errorf(
			"config: failed to create directory for lock file %s: %w",
			lockPath,
			err,
		)
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, files.FilePermission)
	if err != nil {
		return nil, fmt.Errorf("config: failed to open lock file %s: %w", lockPath, err)
	}

	deadline := time.Now().Add(lockAcquireTimeout)
	fd := int(f.Fd())
	for {
		flockErr := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if flockErr == nil {
			break
		}
		if !errors.Is(flockErr, unix.EWOULDBLOCK) {
			// Closing after a real flock error; the more informative error
			// below (which names the lock path and the flock failure) is
			// what's returned, so a close failure on the way out is not
			// actionable on its own.
			_ = f.Close()
			return nil, fmt.Errorf("config: failed to lock %s: %w", lockPath, flockErr)
		}
		if time.Now().After(deadline) {
			// Same as above: closing after a timeout error, where the
			// timeout error itself is what's returned to the caller.
			_ = f.Close()
			return nil, fmt.Errorf(
				"config: timed out after %s waiting for the config lock (%s); "+
					"another devgeta process may be stuck holding it",
				lockAcquireTimeout, lockPath,
			)
		}
		time.Sleep(lockPollInterval)
	}

	return func() error {
		unlockErr := unix.Flock(fd, unix.LOCK_UN)
		closeErr := f.Close()
		return errors.Join(unlockErr, closeErr)
	}, nil
}
