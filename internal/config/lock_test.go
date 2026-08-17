package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cjairm/devgeta/pkg/files"
	"github.com/stretchr/testify/assert"
)

func TestGetGlobalConfigLockFilePath_IsSidecarNotConfigFile(t *testing.T) {
	setupIsolatedConfigPaths(t)

	lockPath := getGlobalConfigLockFilePath()
	configPath := GlobalConfigFilePath()

	assert.NotEqual(
		t,
		configPath,
		lockPath,
		"the lock must live on its own sidecar file, never on global_config.yaml itself",
	)
	assert.Equal(
		t,
		filepath.Dir(configPath),
		filepath.Dir(lockPath),
		"the lock file must sit next to the config file",
	)
	assert.Equal(t, "global_config.lock", filepath.Base(lockPath))
}

func TestUpdate_LoadsMutatesAndSaves(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	gc.CurrentFont = "before"
	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	err := Update(func(gc *GlobalConfig) error {
		assert.Equal(
			t,
			"before",
			gc.CurrentFont,
			"Update must hand fn a freshly loaded config, not a zero value",
		)
		gc.CurrentFont = "after"
		return nil
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	loaded := &GlobalConfig{}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	assert.Equal(t, "after", loaded.CurrentFont, "Update's mutation must be persisted")
}

func TestUpdate_FnErrorSkipsSaveAndPropagates(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	gc.CurrentFont = "unchanged"
	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	sentinel := errors.New("boom")
	err := Update(func(gc *GlobalConfig) error {
		gc.CurrentFont = "should-not-persist"
		return sentinel
	})
	assert.ErrorIs(t, err, sentinel, "Update must propagate fn's error unchanged")

	loaded := &GlobalConfig{}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	assert.Equal(t, "unchanged", loaded.CurrentFont, "an fn error must prevent Save from running")
}

func TestUpdate_ReleasesLockForNextCall(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := Update(
		func(gc *GlobalConfig) error { gc.CurrentFont = "one"; return nil },
	); err != nil {
		t.Fatalf("first Update failed: %v", err)
	}
	if err := Update(
		func(gc *GlobalConfig) error { gc.CurrentFont = "two"; return nil },
	); err != nil {
		t.Fatalf("second Update failed: %v", err)
	}

	loaded := &GlobalConfig{}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	assert.Equal(
		t,
		"two",
		loaded.CurrentFont,
		"a prior Update must fully release the lock so a later one is not blocked",
	)
}

// TestUpdate_ConcurrentUpdatesBothLand is the lost-update regression test:
// without a lock held across the whole load-mutate-save cycle, two
// goroutines that each Load(), mutate, then Save() can have the later Save()
// silently discard the earlier goroutine's change. Every one of n concurrent
// Update calls appending its own unique package name must survive to the
// final saved config.
func TestUpdate_ConcurrentUpdatesBothLand(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pkg := fmt.Sprintf("pkg-%d", i)
			errCh <- Update(func(gc *GlobalConfig) error {
				// Hold the lock a little longer than instant so a second
				// Update is forced to actually wait on the flock, rather
				// than happening to run after this one finishes purely by
				// goroutine-scheduling luck.
				time.Sleep(5 * time.Millisecond)
				gc.Installed.Packages = append(gc.Installed.Packages, pkg)
				return nil
			})
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("Update returned error: %v", err)
		}
	}

	loaded := &GlobalConfig{}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	assert.Len(
		t,
		loaded.Installed.Packages,
		n,
		"every concurrent Update must land; a shorter list means one Update's Save overwrote another's",
	)
	seen := make(map[string]bool, n)
	for _, p := range loaded.Installed.Packages {
		seen[p] = true
	}
	for i := 0; i < n; i++ {
		assert.True(t, seen[fmt.Sprintf("pkg-%d", i)], "pkg-%d is missing from the saved config", i)
	}
}

// TestUpdate_ConcurrentFirstRunUpdatesBothLand is the round-2 regression
// test: it starts from NO config file at all (never calling Create() or
// otherwise pre-creating global_config.yaml) and runs two concurrent Update
// calls, each setting a different field.
//
// Before this fix, every caller worked around Load()'s missing-file error by
// calling gc.Create() before Update. Create() delegates to Reset(), which
// writes a blank file OUTSIDE the lock. On a machine with no config file yet,
// two callers could each observe the file absent, each start writing a blank
// file via Create(), and whichever blank write landed last would silently
// wipe out the other caller's already-saved Update - the lock never helped
// because the destructive write happened before anyone took it. A
// concurrency test that pre-creates the file (as
// TestUpdate_ConcurrentUpdatesBothLand does) cannot catch this: it only
// exercises the already-safe load-mutate-save path, not the missing-file
// initialization path where the bug lived.
//
// Update now absorbs missing-file initialization itself, inside the lock, so
// both fields set here must survive regardless of how the two goroutines
// interleave.
func TestUpdate_ConcurrentFirstRunUpdatesBothLand(t *testing.T) {
	setupIsolatedConfigPaths(t)

	configPath := GlobalConfigFilePath()
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf(
			"precondition failed: config file must not exist yet, stat returned: %v",
			err,
		)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- Update(func(gc *GlobalConfig) error {
			// Hold the lock a little longer than instant so the second
			// Update is forced to actually wait on the flock, exercising the
			// real first-run race rather than happening to serialize purely
			// by goroutine-scheduling luck.
			time.Sleep(5 * time.Millisecond)
			gc.CurrentFont = "font-a"
			return nil
		})
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- Update(func(gc *GlobalConfig) error {
			time.Sleep(5 * time.Millisecond)
			gc.CurrentTheme = "theme-b"
			return nil
		})
	}()

	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("Update returned error: %v", err)
		}
	}

	loaded := &GlobalConfig{}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	assert.Equal(
		t,
		"font-a",
		loaded.CurrentFont,
		"the first Update's field must survive the concurrent first-run save",
	)
	assert.Equal(
		t,
		"theme-b",
		loaded.CurrentTheme,
		"the second Update's field must survive the concurrent first-run save",
	)
}

// TestUpdate_MalformedConfigReturnsErrorWithoutClobberingFile covers the
// plan's "malformed config" test requirement (docs/plans/cycles/2026-07-29-dg-config-command.md
// §4 In Scope, §6 Verification Plan): a corrupt global_config.yaml must
// produce a clear error, not a panic, from Update - and Update must not
// touch the bad file on disk, since Update's os.IsNotExist branch only
// covers a missing file, not a present-but-unparsable one. That distinction
// lives in Update's yaml.Unmarshal error path (via gc.Load(), see
// fromFile.go), which is not os.IsNotExist and so returns immediately
// without ever reaching Save().
func TestUpdate_MalformedConfigReturnsErrorWithoutClobberingFile(t *testing.T) {
	setupIsolatedConfigPaths(t)

	configPath := GlobalConfigFilePath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	malformed := []byte("worktree: [this is not valid yaml: :::\n")
	if err := os.WriteFile(configPath, malformed, 0o644); err != nil {
		t.Fatalf("failed to write malformed config file: %v", err)
	}

	ran := false
	err := Update(func(gc *GlobalConfig) error {
		ran = true
		return nil
	})

	assert.Error(t, err, "Update must return a clear error on malformed YAML, not panic")
	assert.False(t, ran, "fn must never run when the config can't be parsed")

	after, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("failed to read config after failed Update: %v", readErr)
	}
	assert.Equal(
		t,
		malformed,
		after,
		"a failed Update must leave the corrupt file byte-for-byte unchanged, not truncate or overwrite it",
	)
}

// TestUpdate_TimesOutWithActionableErrorWhenLockHeld simulates a wedged
// holder (e.g. a crashed or stuck devgeta process that never released the
// flock) by taking the lock directly and never releasing it for the
// duration of the test, then asserting Update fails fast with a clear error
// instead of hanging.
func TestUpdate_TimesOutWithActionableErrorWhenLockHeld(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	origTimeout := lockAcquireTimeout
	lockAcquireTimeout = 100 * time.Millisecond
	t.Cleanup(func() {
		lockAcquireTimeout = origTimeout
	})

	// The wedged holder is a goroutine parked inside the lock rather than a
	// lock handed back to the test: files.WithLock deliberately offers no way
	// to hold a lock outside a scope, because a caller that dropped the handle
	// would silently lose the lock to the garbage collector. A second flock on
	// the same file conflicts even from the same process, so this is a faithful
	// stand-in for another devgeta stuck holding it.
	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		if err := files.WithLock(getGlobalConfigLockFilePath(), time.Minute, func() error {
			close(held)
			<-release
			return nil
		}); err != nil {
			t.Errorf("the stand-in holder failed to take the lock: %v", err)
		}
	}()
	<-held
	t.Cleanup(func() { close(release) })

	ran := false
	start := time.Now()
	err := Update(func(gc *GlobalConfig) error {
		ran = true
		return nil
	})
	elapsed := time.Since(start)

	assert.Error(
		t,
		err,
		"Update must fail rather than hang when the lock is already held elsewhere",
	)
	assert.Contains(
		t,
		err.Error(),
		"timed out",
		"the error must name what happened so it is actionable",
	)
	assert.False(t, ran, "fn must never run if the lock could not be acquired")
	assert.Less(
		t,
		elapsed,
		2*time.Second,
		"Update must return at (or shortly after) the configured timeout, not hang",
	)
}
