package config

import (
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cjairm/devgeta/pkg/files"
	"gopkg.in/yaml.v3"
)

// globalConfigCache is the process-wide "parse global_config.yaml once per
// run" state ADR-0030 requires. It generalizes the memoization the worktree
// manager used to keep to itself (internal/tooling/worktree/worktree.go's
// now-removed configCache) so every one of the ~95 non-test Load() call
// sites shares it, instead of each package re-reading and re-parsing the
// same file.
//
// The key is {path, modTime, size}, promoted unchanged from the worktree
// manager's version: path is part of it because paths.Paths.Config.Root is
// repointed per test case, so an entry cached under one root can never be
// served under another - a bare process-global singleton would not survive
// that.
//
// mu guards every field: this is package-level state reachable from any
// goroutine (mirrors internal/commands/installed_cache.go's
// installedListingCache, ADR-0029's sibling contract for the same reason).
// It is also held across the write itself in writeGlobalConfigFile, not just
// around the store - see that function's doc comment for why.
var globalConfigCache struct {
	mu sync.Mutex

	valid   bool
	path    string
	modTime time.Time
	size    int64
	doc     *GlobalConfig
}

// diskReadCount counts calls to readGlobalConfigFile - the only place this
// package actually touches the file's bytes for a read. It exists purely so
// tests can assert the cache avoids re-reading the file, rather than only
// asserting the returned value is correct: a value-only assertion would also
// pass for a naive drop-on-write cache that re-parses on every miss. It is
// never read by production logic.
var diskReadCount atomic.Int64

// ResetGlobalConfigCacheForTest drops the process-wide cache unconditionally
// and zeroes the disk-read counter. Package-level cache state persists
// across every test in the same binary, so any test that populates the
// cache (directly, or by calling Load/Save/Update) must call this via
// t.Cleanup - otherwise one test's cached document can silently answer
// another's Load(). setupIsolatedConfigPaths (fromFile_test.go) already
// calls this on every test's cleanup, so most tests in this package get it
// for free.
func ResetGlobalConfigCacheForTest() {
	globalConfigCache.mu.Lock()
	defer globalConfigCache.mu.Unlock()
	invalidateGlobalConfigCacheLocked()
	diskReadCount.Store(0)
}

// invalidateGlobalConfigCacheLocked drops the cached entry. Callers must
// already hold globalConfigCache.mu.
func invalidateGlobalConfigCacheLocked() {
	globalConfigCache.valid = false
	globalConfigCache.path = ""
	globalConfigCache.modTime = time.Time{}
	globalConfigCache.size = 0
	globalConfigCache.doc = nil
}

// lookupGlobalConfigCache returns an independent deep copy of the cached
// document if it is valid and keyed under exactly {path, modTime, size}, or
// nil on a miss. The deep copy (not the cached pointer, not a shallow struct
// copy) is load-bearing: Load() has always handed callers a struct they own
// outright, and several callers (RemoveFromInstalled, AddToFailed, a
// Shortcuts write) mutate that struct in place. Handing out shared memory
// here would let one caller's in-place mutation corrupt what every other
// caller in the process sees next, before that caller ever reaches a
// Save() - see cloneGlobalConfig's doc comment for the reproduction this
// guards against.
func lookupGlobalConfigCache(path string, modTime time.Time, size int64) *GlobalConfig {
	globalConfigCache.mu.Lock()
	defer globalConfigCache.mu.Unlock()
	if !globalConfigCache.valid ||
		globalConfigCache.path != path ||
		globalConfigCache.size != size ||
		!globalConfigCache.modTime.Equal(modTime) {
		return nil
	}
	return cloneGlobalConfig(globalConfigCache.doc)
}

// storeGlobalConfigCache populates the cache with a deep copy of doc, keyed
// on {path, modTime, size}. Used by Load() on a miss (keyed on the stat
// taken just before the read, per readGlobalConfigFile's caller) - writes
// use writeGlobalConfigFile instead, which needs the write and the
// post-write stat inside the same critical section.
func storeGlobalConfigCache(path string, modTime time.Time, size int64, doc *GlobalConfig) {
	clone := cloneGlobalConfig(doc)
	globalConfigCache.mu.Lock()
	defer globalConfigCache.mu.Unlock()
	globalConfigCache.valid = true
	globalConfigCache.path = path
	globalConfigCache.modTime = modTime
	globalConfigCache.size = size
	globalConfigCache.doc = clone
}

// readGlobalConfigFile reads and unmarshals the global config file directly
// from disk into gc, with no cache involved at all. Load() adds the cache
// check on top of this; lock.go's updateLocked calls this directly instead
// of going through Load(), because Update's entire purpose is to guarantee
// its read is fresh under the exclusive sidecar lock. If updateLocked went
// through Load()'s cache, two concurrent Update calls could both hit a cache
// entry populated before either acquired the lock, both mutate from that
// same stale base, and the second Save() would silently discard the first
// Update's change - exactly the lost-update bug the lock exists to prevent,
// reintroduced through the cache instead of through a race on the file. See
// TestUpdate_ReadBypassesStaleCache in lock_test.go for the regression test.
func readGlobalConfigFile(gc *GlobalConfig) error {
	diskReadCount.Add(1)
	data, err := os.ReadFile(GlobalConfigFilePath())
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, gc)
}

// writeGlobalConfigFile atomically writes data to path and, on success,
// refreshes the cache with a deep copy of doc keyed on the file's
// post-write stat - the refresh-on-write contract ADR-0030 requires instead
// of a drop-on-write cache (which would clear the entry on every item's
// Save() during a first-run `dg install` and defeat the whole point).
//
// The cache mutex is held across the write and the stat, not just around
// the store: without that, two concurrent writers' write-then-stat could
// interleave (writer A writes, writer B writes and stats, writer A then
// stats and would store A's document keyed under B's later stat) and
// corrupt the cache with a document keyed on the wrong file state. Holding
// the mutex across the whole sequence makes a concurrent reader see either
// the pre-write entry or the post-write one, never a mismatched pair.
//
// If the write itself fails, the cache is left alone - nothing on disk
// changed, so nothing cached is now wrong. If the write succeeds but the
// post-write stat fails, the entry is dropped rather than stored keyed on a
// guess; the next Load() re-reads from disk. Save(), Reset(), and (via
// Reset()) Create() all go through this one function so every write path
// upholds the same contract.
func writeGlobalConfigFile(path string, data []byte, doc *GlobalConfig) error {
	globalConfigCache.mu.Lock()
	defer globalConfigCache.mu.Unlock()

	if err := files.WriteFileAtomic(path, data, files.FilePermission); err != nil {
		return err
	}

	fi, err := os.Stat(path)
	if err != nil {
		invalidateGlobalConfigCacheLocked()
		return nil
	}

	globalConfigCache.valid = true
	globalConfigCache.path = path
	globalConfigCache.modTime = fi.ModTime()
	globalConfigCache.size = fi.Size()
	globalConfigCache.doc = cloneGlobalConfig(doc)
	return nil
}
