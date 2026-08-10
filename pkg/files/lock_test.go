package files_test

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cjairm/devgeta/pkg/files"
)

// lockHolderEnv names the environment variable that turns a re-execution of
// this test binary into a lock holder. It carries the path to lock.
const lockHolderEnv = "DEVGETA_TEST_LOCK_HOLDER_PATH"

// TestLockHolderSubprocess is not a test of its own — it is the child process
// TestLockSurvivesAKilledHolder starts. Without the environment variable set it
// does nothing, which is what happens on an ordinary `go test` run.
//
// The child takes the lock, announces it on stdout, and then blocks forever
// waiting to be killed. It must never release the lock itself: the whole point
// is to prove the KERNEL releases it when the process dies.
func TestLockHolderSubprocess(t *testing.T) {
	path := os.Getenv(lockHolderEnv)
	if path == "" {
		t.Skip("not the lock-holder child")
	}
	if err := files.WithLock(path, 5*time.Second, func() error {
		if _, err := os.Stdout.WriteString("locked\n"); err != nil {
			return err
		}
		// Sleeping rather than blocking on a channel forever: an unreachable
		// receive trips the runtime's deadlock detector, which panics and exits
		// the process — releasing the lock the parent is about to prove a KILL
		// releases, so the test would pass without testing anything. The parent
		// kills this long before the sleep ends.
		time.Sleep(10 * time.Minute)
		return nil
	}); err != nil {
		t.Fatalf("child failed to hold the lock: %v", err)
	}
}

// TestLockSurvivesAKilledHolder is the crash-safety guarantee, and the reason
// Lock uses flock rather than an O_EXCL lockfile: a holder that dies without
// unlocking — SIGKILL, an OOM kill, a `kill -9` on a wedged review — must not
// leave every later run locked out forever. An O_EXCL lockfile would, unless
// something guessed at when a leftover file is stale, and guessing wrong there
// either wedges the tool or hands two writers the same lock.
//
// It has to be a real second PROCESS, killed for real, because that is the
// exact thing being claimed. Nothing in-process can stand in for it: closing
// the file descriptor is the ORDERLY release, which is what the test would
// accidentally prove instead. The child is this same test binary re-executed —
// no external tool is involved, and it touches nothing but its own temp file.
func TestLockSurvivesAKilledHolder(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "sub", "held.lock")

	child := exec.Command(os.Args[0], "-test.run=^TestLockHolderSubprocess$")
	child.Env = append(os.Environ(), lockHolderEnv+"="+lockPath)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := child.Start(); err != nil {
		t.Fatalf("failed to start the lock holder: %v", err)
	}
	killed := false
	t.Cleanup(func() {
		if !killed {
			if err := child.Process.Kill(); err != nil {
				t.Logf("failed to kill the lock holder: %v", err)
			}
		}
		// The child is killed, so Wait always reports a signal; it is called
		// only to reap the process, and its error is that expected signal.
		_ = child.Wait()
	})

	// Wait for the child to say it holds the lock, rather than sleeping a
	// guessed interval: a slow machine would otherwise kill it before it ever
	// took the lock and the test would pass without testing anything.
	line := make(chan string, 1)
	go func() {
		r := bufio.NewReader(stdout)
		s, readErr := r.ReadString('\n')
		if readErr != nil {
			close(line)
			return
		}
		line <- s
	}()
	select {
	case s, ok := <-line:
		if !ok || s != "locked\n" {
			t.Fatalf("the lock holder did not report holding the lock (got %q)", s)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for the lock holder to take the lock")
	}

	// While it lives, the lock is genuinely exclusive — otherwise the release
	// asserted below would prove nothing.
	if err := tryLock(lockPath, 100*time.Millisecond); err == nil {
		t.Fatal("took a lock another process was holding")
	}

	if err := child.Process.Kill(); err != nil {
		t.Fatalf("failed to kill the lock holder: %v", err)
	}
	killed = true
	if err := child.Wait(); err == nil {
		t.Fatal("expected the killed child to report a signal")
	}

	// The generous timeout is not a wait for staleness to expire — there is no
	// such concept here — it only absorbs the moment between the kill and the
	// kernel closing the dead process's descriptors.
	if err := tryLock(lockPath, 10*time.Second); err != nil {
		t.Fatalf("a killed holder wedged the lock: %v", err)
	}
}

// tryLock reports whether the lock can be taken within timeout, releasing it
// again immediately. Contention is what these tests assert on, so "could it be
// taken" is the whole question and holding it afterwards is not wanted.
func tryLock(path string, timeout time.Duration) error {
	return files.WithLock(path, timeout, func() error { return nil })
}

// TestLockIsExclusiveUntilReleased covers the ordinary path both callers depend
// on: while one holder has it nobody else gets in, and the moment it is
// released the next caller does. The timeout has to produce an error rather
// than a hang, which is what makes a wedged holder a report instead of a
// stalled command.
func TestLockIsExclusiveUntilReleased(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "review", "journals.lock")

	// A second flock conflicts with the first even from the same process — the
	// lock belongs to the open file description, not to the process — so the
	// contention can be shown without a second process here.
	var elapsed time.Duration
	if err := files.WithLock(lockPath, time.Second, func() error {
		start := time.Now()
		contended := tryLock(lockPath, 100*time.Millisecond)
		elapsed = time.Since(start)
		if contended == nil {
			return errors.New("a second caller took the lock while it was held")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("the contended attempt hung for %s instead of timing out", elapsed)
	}

	if err := tryLock(lockPath, time.Second); err != nil {
		t.Fatalf("the lock was not released when its scope ended: %v", err)
	}
}

// TestLockCreatesItsDirectory pins the first-run case: the lock is taken before
// anything the caller protects has been written, so the directory holding it
// may not exist yet. A caller that had to create it first would be creating it
// outside the lock, which is the same unsynchronized first write the lock
// exists to prevent.
func TestLockCreatesItsDirectory(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "a", "b", "c.lock")

	if err := files.WithLock(lockPath, time.Second, func() error {
		_, statErr := os.Stat(lockPath)
		return statErr
	}); err != nil {
		t.Fatalf("expected the lock file and its directory to be created: %v", err)
	}
}
