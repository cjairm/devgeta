package appstate

import (
	"archive/tar"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/tooling/archive"
	"github.com/klauspost/compress/zstd"
)

// bundleMembers lists a bundle's regular-file members, sorted — the closest
// a test gets to what `tar --zstd -tf` shows a user.
func bundleMembers(t *testing.T, bundlePath string) []string {
	t.Helper()
	f, err := os.Open(bundlePath)
	if err != nil {
		t.Fatalf("opening bundle: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	dec, err := zstd.NewReader(f)
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	t.Cleanup(dec.Close)

	var members []string
	tr := tar.NewReader(dec)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("reading bundle: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			members = append(members, hdr.Name)
		}
	}
	sort.Strings(members)
	return members
}

func TestBundleName(t *testing.T) {
	when := time.Date(2026, 9, 15, 13, 45, 0, 0, time.UTC)

	if got, want := BundleName("brave", when), "brave-state-2026-09-15"; got != want {
		t.Errorf("BundleName = %q, want %q", got, want)
	}
	// The "-state-" is what keeps a bundle apart from a `dg archive` of a
	// folder that happens to be called "brave".
	if got, want := BundlePrefix("brave"), "brave-state-"; got != want {
		t.Errorf("BundlePrefix = %q, want %q", got, want)
	}
	if !strings.HasPrefix(BundleName("brave", when), BundlePrefix("brave")) {
		t.Error("BundleName does not carry BundlePrefix")
	}
}

func TestExportWritesAVerifiableBundle(t *testing.T) {
	porter := newTestPorter(t, "Default")
	destDir := t.TempDir()

	result, err := Export(porter, destDir, "brave-state-2026-09-15", Selection{}, ExportOptions{})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	wantBundle := filepath.Join(destDir, "brave-state-2026-09-15"+BundleExt)
	if result.BundlePath != wantBundle {
		t.Errorf("BundlePath = %q, want %q", result.BundlePath, wantBundle)
	}
	for _, p := range []string{result.BundlePath, result.ManifestPath, result.ArchiveHashPath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}

	got := bundleMembers(t, result.BundlePath)
	want := []string{
		"Default/Bookmarks",
		"Default/Preferences",
		"Default/Sessions/Session_1",
		"Default/Sessions/Tabs_1",
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("bundle members:\n  got:  %v\n  want: %v", got, want)
	}

	// The bundle carries a manifest that the verifier accepts, which is the
	// whole reason import can refuse a damaged transfer before writing.
	if _, err := archive.Verify(result.BundlePath, archive.VerifyOptions{}); err != nil {
		t.Errorf("re-verifying the bundle: %v", err)
	}
}

func TestExportHoldsTwoProfilesSideBySide(t *testing.T) {
	porter := newTestPorter(t, "Default", "Profile 1")
	destDir := t.TempDir()

	result, err := Export(
		porter,
		destDir,
		"brave-state-2026-09-15",
		Selection{Groups: []string{"bookmarks"}},
		ExportOptions{},
	)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	got := bundleMembers(t, result.BundlePath)
	want := []string{"Default/Bookmarks", "Profile 1/Bookmarks"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("bundle members:\n  got:  %v\n  want: %v", got, want)
	}
}

func TestExportRefusesWhileTheAppIsRunning(t *testing.T) {
	porter := newTestPorter(t, "Default")
	porter.running = true
	destDir := t.TempDir()

	_, err := Export(porter, destDir, "brave-state-2026-09-15", Selection{}, ExportOptions{})
	if err == nil {
		t.Fatal("expected a refusal while the app is running, got nil")
	}
	for _, want := range []string{"brave", "quit"} {
		if !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	assertDirEmpty(t, destDir)
}

func TestExportReportsARunningCheckFailure(t *testing.T) {
	porter := newTestPorter(t, "Default")
	porter.runErr = errors.New("pgrep exploded")
	destDir := t.TempDir()

	_, err := Export(porter, destDir, "brave-state-2026-09-15", Selection{}, ExportOptions{})
	if err == nil {
		t.Fatal("expected an error when the running check fails, got nil")
	}
	assertDirEmpty(t, destDir)
}

// TestExportRefusesAnEmptySelection: a bundle with no members is not a
// successful export, it is a silent failure the user discovers on the new
// machine.
func TestExportRefusesAnEmptySelection(t *testing.T) {
	porter := newTestPorter(t, "Default")
	for _, name := range []string{"Bookmarks", "Preferences", "Sessions"} {
		if err := os.RemoveAll(filepath.Join(porter.roots["Default"], name)); err != nil {
			t.Fatalf("RemoveAll: %v", err)
		}
	}
	destDir := t.TempDir()

	_, err := Export(porter, destDir, "brave-state-2026-09-15", Selection{}, ExportOptions{})
	if err == nil {
		t.Fatal("expected a refusal for a selection with nothing in it, got nil")
	}
	assertDirEmpty(t, destDir)
}

// TestExportLeavesNothingBehindWhenTheWriteFails: every output is a .partial
// until the whole run succeeds, so a failure leaves the destination as it
// was. Here the "directory" is a file, so creating the first partial fails.
func TestExportLeavesNothingBehindWhenTheWriteFails(t *testing.T) {
	porter := newTestPorter(t, "Default")
	parent := t.TempDir()
	destDir := filepath.Join(parent, "not-a-directory")
	writeTestFile(t, destDir, "this is a file")

	_, err := Export(porter, destDir, "brave-state-2026-09-15", Selection{}, ExportOptions{})
	if err == nil {
		t.Fatal("expected an error writing into a path that is not a directory, got nil")
	}

	entries, readErr := os.ReadDir(parent)
	if readErr != nil {
		t.Fatalf("ReadDir: %v", readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "not-a-directory" {
		t.Errorf("failed export left files behind: %v", entries)
	}
}

func TestExportReportsProgress(t *testing.T) {
	porter := newTestPorter(t, "Default")
	destDir := t.TempDir()

	var written int64
	result, err := Export(porter, destDir, "brave-state-2026-09-15", Selection{}, ExportOptions{
		OnWriteProgress: func(n int64) { written += n },
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	if written == 0 {
		t.Error("the write meter was never advanced")
	}
	// The verify phase has no meter here: it is metered against the
	// bundle's size on disk, which the command layer knows and this one
	// does not. Its own progress is covered by cmd's verifyWithProgress.
	if result.ArchiveHashPath == "" {
		t.Error("Export did not verify what it wrote")
	}
}

func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected %s to be untouched, found: %v", dir, names)
	}
}
