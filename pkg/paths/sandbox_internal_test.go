package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests live in package paths rather than paths_test because the sandbox
// guard and its sweep are unexported by design: nothing outside this package
// should be able to reach them, least of all to disable them.

// isUnder reports whether path sits inside root. Both sides are produced from
// the same strings this package derives its paths from, so a prefix comparison
// is exact here and does not need symlink resolution.
//
// Both sides are cleaned first because one of the roots callers pass is
// os.TempDir(), which on macOS returns $TMPDIR verbatim — trailing slash and
// all ("/var/folders/../T/"). Appending a separator to that yields "T//", which
// no real path is ever a prefix of, so the check failed for a sandbox that was
// correctly placed.
func isUnder(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

// TestSandboxIsActive is the load-bearing assertion of this file. If it fails,
// tests across the whole repository are running against the real user's home
// directory, and every other guarantee in CLAUDE.md about test isolation is
// void.
func TestSandboxIsActive(t *testing.T) {
	if testSandbox == "" {
		t.Fatal(
			"test sandbox is not active: every test in this repository is resolving " +
				"paths against the real user home directory",
		)
	}
	info, err := os.Stat(testSandbox)
	if err != nil {
		t.Fatalf("expected the sandbox directory to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected %q to be a directory", testSandbox)
	}
	if !isUnder(testSandbox, os.TempDir()) {
		t.Errorf("expected the sandbox %q to live under the temp dir %q", testSandbox, os.TempDir())
	}
	if !strings.HasPrefix(filepath.Base(testSandbox), testSandboxPrefix) {
		t.Errorf("expected the sandbox %q to carry the prefix %q", testSandbox, testSandboxPrefix)
	}
}

// TestSandboxHoldsItsOwnLock proves this process claimed its sandbox. It is
// also what keeps the sweep tests below honest: sweepAbandonedSandboxes returns
// immediately when the lock is missing, so without this assertion those tests
// could pass vacuously on a filesystem where flock does not work.
func TestSandboxHoldsItsOwnLock(t *testing.T) {
	if testSandboxLock == nil {
		t.Fatal("expected this process to hold an flock on its own sandbox")
	}
	if _, err := claimSandbox(testSandbox); err == nil {
		t.Error(
			"expected a second claim on the live sandbox to fail; a sweep in another " +
				"process would be free to delete this test run's HOME",
		)
	}
}

func TestSandboxRedirectsHomeAndEveryXDGRoot(t *testing.T) {
	want := map[string]string{
		"HOME":            testSandbox,
		"XDG_CONFIG_HOME": filepath.Join(testSandbox, ".config"),
		"XDG_DATA_HOME":   filepath.Join(testSandbox, ".local", "share"),
		"XDG_STATE_HOME":  filepath.Join(testSandbox, ".local", "state"),
		"XDG_CACHE_HOME":  filepath.Join(testSandbox, ".cache"),
	}
	for key, wantValue := range want {
		if got := os.Getenv(key); got != wantValue {
			t.Errorf("%s: expected %q, got %q", key, wantValue, got)
		}
	}
}

// TestDerivedPathsResolveInsideSandbox covers the package-level values, which
// are computed once during initialization. They are the ones a forgetful test
// reaches for without isolating anything, so they are exactly what the sandbox
// exists to contain.
func TestDerivedPathsResolveInsideSandbox(t *testing.T) {
	// System-scoped paths (/Applications, /usr/share/fonts) are deliberately
	// absent: they are not derived from the home directory and never resolve
	// into the sandbox.
	cases := map[string]string{
		"Paths.Home.Root":     Paths.Home.Root,
		"Paths.Config.Root":   Paths.Config.Root,
		"Paths.Data.Root":     Paths.Data.Root,
		"Paths.Cache.Root":    Paths.Cache.Root,
		"Paths.App.Root":      Paths.App.Root,
		"Paths.Config.Claude": Paths.Config.Claude,
		"Paths.Config.Nvim":   Paths.Config.Nvim,
		"Paths.User.Fonts":    Paths.User.Fonts,
		"Files.ShellConfig":   Files.ShellConfig,
		"Files.ZshEnv":        Files.ZshEnv,
		"GetStateDir()":       GetStateDir(),
		"userHome()":          userHome(),
		"ExpandHome(\"~\")":   ExpandHome("~"),
	}
	for name, got := range cases {
		if !isUnder(got, testSandbox) {
			t.Errorf("%s resolved to %q, which is outside the sandbox %q", name, got, testSandbox)
		}
	}
}

// sweepRoot points os.TempDir() at a throwaway directory so a sweep test can
// only ever see the candidates it created itself.
func sweepRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("TMPDIR", root)
	if os.TempDir() != root {
		t.Fatalf("expected os.TempDir() to follow TMPDIR, got %q", os.TempDir())
	}
	return root
}

// plantSandbox creates a populated directory that looks like a sandbox and
// backdates it by age. The contents go in before the backdating because adding
// an entry bumps the parent directory's mtime.
func plantSandbox(t *testing.T, root, name string, age time.Duration) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, ".config", "devgeta"), 0o700); err != nil {
		t.Fatalf("failed to create %q: %v", dir, err)
	}
	stateFile := filepath.Join(dir, ".config", "devgeta", "global_config.yaml")
	if err := os.WriteFile(stateFile, []byte("installed: []\n"), 0o600); err != nil {
		t.Fatalf("failed to populate %q: %v", dir, err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(dir, when, when); err != nil {
		t.Fatalf("failed to backdate %q: %v", dir, err)
	}
	return dir
}

func assertExists(t *testing.T, dir, why string) {
	t.Helper()
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("expected %q to survive the sweep (%s): %v", dir, why, err)
	}
}

func assertGone(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected %q to be swept, but it is still present (err=%v)", dir, err)
	}
}

func TestSweepRemovesAbandonedSandbox(t *testing.T) {
	root := sweepRoot(t)
	abandoned := plantSandbox(t, root, testSandboxPrefix+"abandoned", 2*sandboxSweepMinAge)

	sweepAbandonedSandboxes(testSandbox)

	assertGone(t, abandoned)
}

// TestSweepKeepsLockedSandbox is the case that matters most: deleting a live
// test process's HOME mid-run is far worse than leaking the directory.
func TestSweepKeepsLockedSandbox(t *testing.T) {
	root := sweepRoot(t)
	live := plantSandbox(t, root, testSandboxPrefix+"live", 2*sandboxSweepMinAge)

	// Stands in for the owning process. flock belongs to the open file
	// description rather than the process, so a separate open here conflicts
	// exactly as another process's would.
	lock, err := claimSandbox(live)
	if err != nil {
		t.Fatalf("failed to simulate a live owner: %v", err)
	}
	t.Cleanup(func() {
		if err := lock.Close(); err != nil {
			t.Logf("failed to release the simulated owner's lock: %v", err)
		}
	})

	sweepAbandonedSandboxes(testSandbox)

	assertExists(t, live, "a live owner holds its flock")
}

func TestSweepKeepsRecentSandbox(t *testing.T) {
	root := sweepRoot(t)
	recent := plantSandbox(t, root, testSandboxPrefix+"recent", sandboxSweepMinAge/2)

	sweepAbandonedSandboxes(testSandbox)

	assertExists(t, recent, "younger than the grace period")
}

// TestSweepKeepsOwnSandbox pins the explicit self-exclusion. The age and lock
// checks already cover this process's sandbox, so this guards the redundancy
// rather than the outcome.
func TestSweepKeepsOwnSandbox(t *testing.T) {
	root := sweepRoot(t)
	own := plantSandbox(t, root, testSandboxPrefix+"own", 2*sandboxSweepMinAge)

	sweepAbandonedSandboxes(own)

	assertExists(t, own, "it is the caller's own sandbox")
}

func TestSweepIgnoresEntriesWithoutThePrefix(t *testing.T) {
	root := sweepRoot(t)
	foreign := plantSandbox(t, root, "some-other-tool-cache", 2*sandboxSweepMinAge)
	nearMiss := plantSandbox(t, root, "devgeta-test-sandbo", 2*sandboxSweepMinAge)

	sweepAbandonedSandboxes(testSandbox)

	assertExists(t, foreign, "it does not carry the sandbox prefix")
	assertExists(t, nearMiss, "its name only resembles the sandbox prefix")
}

// TestSweepStopsAtBudget pins the bound that keeps a large backlog from turning
// package initialization into a long stall.
func TestSweepStopsAtBudget(t *testing.T) {
	root := sweepRoot(t)
	const extra = 5
	for i := range sandboxSweepBudget + extra {
		plantSandbox(t, root, fmt.Sprintf("%s%03d", testSandboxPrefix, i), 2*sandboxSweepMinAge)
	}

	sweepAbandonedSandboxes(testSandbox)

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to read the sweep root: %v", err)
	}
	if len(entries) != extra {
		t.Errorf(
			"expected the sweep to stop after %d deletions leaving %d, got %d remaining",
			sandboxSweepBudget, extra, len(entries),
		)
	}
}

// TestSweepDoesNothingWithoutOwnLock covers the rule that keeps the sweep safe
// on a filesystem where flock does not work: a process that could not claim its
// own sandbox must never delete anybody else's.
func TestSweepDoesNothingWithoutOwnLock(t *testing.T) {
	root := sweepRoot(t)
	abandoned := plantSandbox(t, root, testSandboxPrefix+"abandoned", 2*sandboxSweepMinAge)

	held := testSandboxLock
	testSandboxLock = nil
	t.Cleanup(func() { testSandboxLock = held })

	sweepAbandonedSandboxes(testSandbox)

	assertExists(t, abandoned, "this process holds no lock of its own")
}
