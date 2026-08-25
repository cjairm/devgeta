package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

// TestLoad_AfterSaveReturnsNewValueWithoutRereadingFile is the first of
// ADR-0030's two headline behaviors: a Save() followed by a Load() in the
// same process must return the new value without touching the file's bytes
// again. Asserting on the value alone would also pass for a naive
// drop-on-write cache that re-parses on every miss, so this asserts on the
// disk-read counter (readGlobalConfigFile's diskReadCount) instead.
func TestLoad_AfterSaveReturnsNewValueWithoutRereadingFile(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	gc.CurrentFont = "before"
	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	gc.CurrentFont = "after"
	if err := gc.Save(); err != nil {
		t.Fatalf("second Save failed: %v", err)
	}

	before := diskReadCount.Load()
	loaded := &GlobalConfig{}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	after := diskReadCount.Load()

	assert.Equal(t, "after", loaded.CurrentFont, "Load must return the value Save just wrote")
	assert.Equal(
		t,
		before,
		after,
		"Load must be served from the cache after a Save, not re-read the file",
	)
}

// TestLoad_StillSucceedsAfterFileMadeUnreadable is the stronger version of
// the same behavior: it proves the cache hit really never touches the
// file's bytes, not merely that it's fast. chmod 0000 leaves the file's stat
// (mtime, size) unchanged - which is exactly the cache's key - but any
// attempt to open and read its content fails. If Load() only avoided
// re-reading "most of the time," this would surface it.
func TestLoad_StillSucceedsAfterFileMadeUnreadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: file permissions do not restrict reads")
	}
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	gc.CurrentFont = "cached-value"
	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	path := GlobalConfigFilePath()
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("failed to make config file unreadable: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(path, 0o644)
	})

	loaded := &GlobalConfig{}
	err := loaded.Load()
	assert.NoError(
		t,
		err,
		"Load must succeed from the cache even though the file can no longer be opened",
	)
	assert.Equal(t, "cached-value", loaded.CurrentFont)
}

// TestLoad_MaybeInstallShapedLoopParsesOnce reproduces MaybeInstall's own
// pattern - one Load() per item, a Save() whenever the item was not already
// tracked - over N items, and proves the document is parsed once rather
// than once per item. This is the scenario ADR-0030 says a drop-on-write
// cache gets backwards: dropping the entry on every Save() would clear it on
// every item during exactly the first-run `dg install` this step exists to
// speed up.
//
// The seed file is written directly via os.WriteFile rather than via
// Create()/Save(), which would prime the cache for free (Reset() writes the
// file without ever reading it) and hide what this test is meant to prove:
// starting cold, the loop's first Load() is the only disk read.
func TestLoad_MaybeInstallShapedLoopParsesOnce(t *testing.T) {
	setupIsolatedConfigPaths(t)

	configPath := GlobalConfigFilePath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("app_path: \"\"\n"), 0o644); err != nil {
		t.Fatalf("failed to seed config file: %v", err)
	}

	before := diskReadCount.Load()
	const n = 20
	for i := 0; i < n; i++ {
		item := &GlobalConfig{}
		if err := item.Load(); err != nil {
			t.Fatalf("Load failed on item %d: %v", i, err)
		}
		item.AddToInstalled(fmt.Sprintf("pkg-%d", i), "package")
		if err := item.Save(); err != nil {
			t.Fatalf("Save failed on item %d: %v", i, err)
		}
	}
	after := diskReadCount.Load()

	assert.Equal(
		t,
		before+1,
		after,
		"a Load-then-Save loop over %d items must parse the file exactly once, not once per item",
		n,
	)
}

// TestLoad_MutatingOneResultDoesNotAffectTheNext is the ownership half of
// the cache contract: RemoveFromInstalled, AddToFailed, and a Shortcuts
// write all mutate a loaded *GlobalConfig in place, and none of that must
// reach the cache. If Load() ever handed out the cached pointer or a
// shallow copy, this would corrupt the cache the moment any one of these
// ran - see cloneGlobalConfig's doc comment for the exact reproduction.
func TestLoad_MutatingOneResultDoesNotAffectTheNext(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	gc.Installed.TerminalTools = []string{"git", "tmux", "neovim", "fzf", "bat"}
	gc.Shortcuts = map[string]string{"k1": "v1"}
	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	first := &GlobalConfig{}
	if err := first.Load(); err != nil {
		t.Fatalf("first Load failed: %v", err)
	}
	first.RemoveFromInstalled("tmux", "terminal_tool")
	first.AddToFailed("rust", "dev_language", "boom", 1)
	first.Shortcuts["k2"] = "v2"

	second := &GlobalConfig{}
	if err := second.Load(); err != nil {
		t.Fatalf("second Load failed: %v", err)
	}

	assert.Equal(
		t,
		[]string{"git", "tmux", "neovim", "fzf", "bat"},
		second.Installed.TerminalTools,
		"mutating one Load() result's slice must not affect a later Load()",
	)
	assert.Empty(
		t,
		second.FailedInstallations,
		"mutating one Load() result's failed-installations slice must not affect a later Load()",
	)
	assert.Equal(
		t,
		map[string]string{"k1": "v1"},
		second.Shortcuts,
		"mutating one Load() result's Shortcuts map must not affect a later Load()",
	)
}

// TestLoad_OutOfBandWriteIsPickedUp proves the stat half of the cache key:
// a write that does not go through this package at all - a different
// process, a hand-edited file - is still detected on the next Load() as
// long as it changes the file's size or mtime.
func TestLoad_OutOfBandWriteIsPickedUp(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	gc.CurrentFont = "in-process-value"
	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	first := &GlobalConfig{}
	if err := first.Load(); err != nil {
		t.Fatalf("first Load failed: %v", err)
	}
	assert.Equal(t, "in-process-value", first.CurrentFont)

	// An out-of-band writer: raw os.WriteFile, not through Save(). Different
	// content length guarantees a different size even if mtime resolution
	// happens to collide.
	outOfBand := &GlobalConfig{}
	*outOfBand = *first
	outOfBand.CurrentFont = "out-of-band-value-that-is-longer"
	raw, err := yaml.Marshal(outOfBand)
	if err != nil {
		t.Fatalf("failed to marshal out-of-band config: %v", err)
	}
	if err := os.WriteFile(GlobalConfigFilePath(), raw, 0o644); err != nil {
		t.Fatalf("failed to write out-of-band config: %v", err)
	}

	second := &GlobalConfig{}
	if err := second.Load(); err != nil {
		t.Fatalf("second Load failed: %v", err)
	}
	assert.Equal(
		t,
		"out-of-band-value-that-is-longer",
		second.CurrentFont,
		"an out-of-band write with a different size must be picked up on the next Load()",
	)
}

// TestConcurrentLoadAndSave_NoDataRace exercises globalConfigCache.mu under
// real contention from bare Load() and Save() calls - the access pattern
// ADR-0030 says the ~95 non-Update() call sites use, which bypass lock.go's
// sidecar file lock entirely. The two existing concurrency tests
// (TestUpdate_ConcurrentUpdatesBothLand,
// TestUpdate_ConcurrentFirstRunUpdatesBothLand) both go through Update(),
// which serializes on that file lock before either goroutine ever touches
// the cache - so neither one contends globalConfigCache.mu at all, and a
// mutex bug in lookupGlobalConfigCache, storeGlobalConfigCache, or
// writeGlobalConfigFile could exist without either test noticing.
//
// This test drives Load()/Save() directly with no Update() in the loop, run
// under `go test -race`. Concurrent unordered Save()s racing for "last
// write wins" on the file's content is an accepted, expected outcome of
// this access pattern - ADR-0030 is explicit that only Update() serializes
// writes - so this test does not assert on which goroutine's value survives,
// only that every call completes without error and the race detector
// reports nothing.
func TestConcurrentLoadAndSave_NoDataRace(t *testing.T) {
	setupIsolatedConfigPaths(t)

	seed := &GlobalConfig{}
	if err := seed.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	const goroutines = 8
	const itersPerGoroutine = 50

	var wg sync.WaitGroup
	errCh := make(chan error, goroutines*itersPerGoroutine*2)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < itersPerGoroutine; i++ {
				loaded := &GlobalConfig{}
				if err := loaded.Load(); err != nil {
					errCh <- fmt.Errorf("goroutine %d: Load failed on iter %d: %w", id, i, err)
					continue
				}
				loaded.CurrentFont = fmt.Sprintf("font-%d-%d", id, i)
				if err := loaded.Save(); err != nil {
					errCh <- fmt.Errorf("goroutine %d: Save failed on iter %d: %w", id, i, err)
				}
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}
