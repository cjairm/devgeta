package commands

import (
	"os/exec"
	"sync"
)

// installedListingCache is the process-wide "what's already installed"
// state ADR-0029 requires: one full package-manager listing captured lazily
// on first probe and reused by every later probe in the same process,
// instead of a fresh listing per package — or worse, a targeted
// `brew list <pkg>` probe, measured 6x slower than listing everything (see
// the ADR's "Considered and rejected" section) — for every package
// MaybeInstall checks.
//
// It has to live here, at package scope, rather than as a field on
// MacOSCommand/DebianCommand: NewCommand() (factory.go) returns a fresh
// instance on every call — roughly one per app's own constructor, ~58 call
// sites — so a struct field would repopulate for nearly every one and save
// almost none of the cost this cache exists to remove (ADR-0029, "The cache
// is process-wide, not per-Command-instance").
//
// mu guards every field below, both the lazy populate in listingFrom and the
// drop in InvalidateInstalledPackageCache: package-level state is reachable
// from any goroutine (ADR-0029, "Concurrency").
var installedListingCache struct {
	mu sync.Mutex

	macFormulae cachedListing // `brew list`
	macCasks    cachedListing // `brew list --cask`
	debianDpkg  cachedListing // `dpkg -l`
}

// cachedListing is one package-manager listing: whether it has been
// populated yet, and if so, its stdout split into lines — the same shape
// findPackageInBrewOutput and findPackageInDpkgOutput already scan.
type cachedListing struct {
	populated bool
	lines     [][]byte
}

// InvalidateInstalledPackageCache drops every cached listing, unconditionally.
// The next probe against any of them repopulates lazily.
//
// Two callers, by design (ADR-0029):
//
//   - Production: the eight mutation-seam methods — InstallPackage,
//     InstallDesktopApp, UninstallPackage, UninstallDesktopApp on both
//     MacOSCommand and DebianCommand — call this on every ATTEMPTED
//     mutation, success or failure. A failed brew/apt call can still have
//     changed package state, and the cost of assuming it might (one extra
//     ~1.6s listing) is far cheaper than the cost of a stale "already
//     installed" answer letting MaybeInstall silently skip a package that
//     was in fact just uninstalled earlier in the same run.
//   - Tests: this cache is package-level state that outlives any one test
//     in the same binary. Any test that populates it, or exercises a
//     mutation-seam method, must call this via t.Cleanup — before and/or
//     after — in the spirit of the paths.Paths.* save/restore rule
//     (CLAUDE.md §6) and the way SetRootContext's own doc comment asks its
//     callers to restore the default; otherwise one test's cached listing
//     silently answers another's probe.
func InvalidateInstalledPackageCache() {
	installedListingCache.mu.Lock()
	defer installedListingCache.mu.Unlock()
	installedListingCache.macFormulae = cachedListing{}
	installedListingCache.macCasks = cachedListing{}
	installedListingCache.debianDpkg = cachedListing{}
}

// listingFrom returns slot's cached lines, populating it first by running
// newCmd() if it has not been populated yet (or has just been invalidated).
// newCmd is invoked at most once per population — never while the cache is
// already warm — which is the entire saving ADR-0029 measures: one
// `brew list` (or `--cask`, or `dpkg -l`) per process instead of one per
// package probed.
func listingFrom(slot *cachedListing, newCmd func() *exec.Cmd) ([][]byte, error) {
	installedListingCache.mu.Lock()
	defer installedListingCache.mu.Unlock()
	if slot.populated {
		return slot.lines, nil
	}
	lines, err := runListingCommand(newCmd())
	if err != nil {
		return nil, err
	}
	slot.lines = lines
	slot.populated = true
	return slot.lines, nil
}
