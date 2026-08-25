package commands

import (
	"os/exec"
	"testing"
)

// TestListingFrom_PopulatesOnce is a white-box test of the cache primitive
// itself: listingFrom must run newCmd on the first call and never again for
// a slot that stays populated. macos_test.go and debian_test.go cover the
// same guarantee end-to-end through IsPackageInstalled/IsDesktopAppInstalled;
// this isolates it from the platform-specific probe methods.
func TestListingFrom_PopulatesOnce(t *testing.T) {
	t.Cleanup(InvalidateInstalledPackageCache)
	InvalidateInstalledPackageCache()

	calls := 0
	newCmd := func() *exec.Cmd {
		calls++
		return exec.Command("printf", "pkg-a\npkg-b")
	}

	var slot cachedListing
	if _, err := listingFrom(&slot, newCmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly one populate call, got %d", calls)
	}
	if !slot.populated {
		t.Fatalf("expected the slot to be marked populated")
	}

	if _, err := listingFrom(&slot, newCmd); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected the cached slot to avoid a second populate call, got %d calls", calls)
	}
}

// TestListingFrom_PropagatesCommandError covers the failure path: a listing
// command that fails must neither mark the slot populated nor cache a
// half-read result, so the next probe retries rather than answering from a
// broken cache.
func TestListingFrom_PropagatesCommandError(t *testing.T) {
	t.Cleanup(InvalidateInstalledPackageCache)
	InvalidateInstalledPackageCache()

	var slot cachedListing
	newCmd := func() *exec.Cmd { return exec.Command("false") }

	if _, err := listingFrom(&slot, newCmd); err == nil {
		t.Fatalf("expected an error from a failing listing command")
	}
	if slot.populated {
		t.Fatalf("expected the slot to stay unpopulated after a failed populate")
	}
}

// TestInvalidateInstalledPackageCache_DropsEveryListing verifies the
// exported reset seam ADR-0029 calls for drops all three cached listings
// together — not just the one a given test happened to populate — since
// invalidation has no way to know which platform's methods are in play.
func TestInvalidateInstalledPackageCache_DropsEveryListing(t *testing.T) {
	t.Cleanup(InvalidateInstalledPackageCache)

	populated := cachedListing{populated: true, lines: [][]byte{[]byte("x")}}
	installedListingCache.mu.Lock()
	installedListingCache.macFormulae = populated
	installedListingCache.macCasks = populated
	installedListingCache.debianDpkg = populated
	installedListingCache.mu.Unlock()

	InvalidateInstalledPackageCache()

	installedListingCache.mu.Lock()
	defer installedListingCache.mu.Unlock()
	if installedListingCache.macFormulae.populated ||
		installedListingCache.macCasks.populated ||
		installedListingCache.debianDpkg.populated {
		t.Fatalf(
			"expected every listing to be cleared, got macFormulae.populated=%v macCasks.populated=%v debianDpkg.populated=%v",
			installedListingCache.macFormulae.populated,
			installedListingCache.macCasks.populated,
			installedListingCache.debianDpkg.populated,
		)
	}
}
