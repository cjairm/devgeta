package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cjairm/devgeta/pkg/logger"
	"golang.org/x/sys/unix"
)

// lockPollInterval is how often Lock retries a non-blocking flock attempt while
// waiting out its timeout. unix.Flock has no built-in timeout and LOCK_EX
// blocks forever, so polling with LOCK_EX|LOCK_NB is used instead of a blocking
// call on a goroutine: a goroutine stuck in a blocking syscall past the timeout
// cannot be cancelled and would leak for as long as the other holder keeps the
// lock.
const lockPollInterval = 25 * time.Millisecond

// WithLock runs fn holding an exclusive advisory lock on lockPath, creating the
// lock file and its parent directory if needed, and releases the lock however
// fn ends. fn's error is returned unchanged, so a caller can wrap its whole
// load-mutate-save cycle in this and keep its own error paths.
//
// It takes a callback rather than returning a release function on purpose. A
// released-by-the-caller API cannot survive a caller that ignores the returned
// value: the lock lives on an open file descriptor held by an *os.File, and
// once nothing references that file the runtime's finalizer closes it — which
// releases the flock and hands it to somebody else while the "holder" is still
// working. That is demonstrable, not theoretical (drop the reference, call
// runtime.GC(), and the lock is gone), and it fails silently in exactly the
// direction the lock exists to prevent. A scope the caller cannot leave holding
// the lock makes that mistake unavailable rather than merely documented
// (CLAUDE.md §4).
//
// It is what makes a read-modify-write cycle over a file safe between separate
// devgeta PROCESSES. WriteFileAtomic alone is not enough: it guarantees a
// reader never sees a half-written file, but two processes that each load,
// mutate, and save can still interleave so the later save silently discards the
// earlier one's change. Holding this across the whole cycle is what closes that
// window, so acquire it before the load, not around the write.
//
// unix.Flock(LOCK_EX) is chosen over an O_CREATE|O_EXCL lockfile because the
// kernel releases a flock when the holding process exits — including when it is
// killed — so a crashed holder cannot wedge every later run, and there is no
// stale-lock age heuristic to get wrong. The flock belongs to the open file
// description, not to the process, so it also serializes two goroutines in one
// process that each opened the file: nested acquisition of the same lock on one
// call path self-deadlocks into a timeout, and callers must not do it.
//
// timeout bounds the wait so a wedged holder (stopped under a debugger,
// deadlocked) produces an actionable error instead of hanging. lockPath should
// be a sidecar, never the file being protected: an atomic save replaces that
// file by rename, so its inode changes on every write and a lock held on it
// would be silently orphaned the moment another writer's save landed.
func WithLock(lockPath string, timeout time.Duration, fn func() error) error {
	unlock, err := acquire(lockPath, timeout)
	if err != nil {
		return err
	}
	defer func() {
		if unlockErr := unlock(); unlockErr != nil {
			logger.L().Warnw("failed to release lock", "path", lockPath, "error", unlockErr)
		}
	}()
	return fn()
}

// acquire is WithLock's body up to the point the lock is held, split out only
// so the several failure exits can return rather than nest. Nothing outside
// this file may call it: a returned release function is precisely the shape
// WithLock exists to keep away from callers.
func acquire(lockPath string, timeout time.Duration) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), DirPermission); err != nil {
		return nil, fmt.Errorf("failed to create the directory for lock file %s: %w", lockPath, err)
	}

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, FilePermission)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file %s: %w", lockPath, err)
	}

	deadline := time.Now().Add(timeout)
	fd := int(f.Fd())
	for {
		flockErr := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if flockErr == nil {
			break
		}
		if !errors.Is(flockErr, unix.EWOULDBLOCK) {
			// Closing after a real flock error; the more informative error
			// below (which names the lock path and the flock failure) is what
			// is returned, so a close failure on the way out is not actionable
			// on its own.
			_ = f.Close()
			return nil, fmt.Errorf("failed to lock %s: %w", lockPath, flockErr)
		}
		if time.Now().After(deadline) {
			// Same as above: closing after a timeout error, where the timeout
			// error itself is what is returned to the caller.
			_ = f.Close()
			return nil, fmt.Errorf(
				"timed out after %s waiting for the lock %s; "+
					"another devgeta process may be stuck holding it",
				timeout, lockPath,
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
