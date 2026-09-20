package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/tooling/appstate"
)

// newEmptyStubPorter is a destination machine: the profile directories
// exist but hold none of the state a bundle carries.
func newEmptyStubPorter(t *testing.T, profileKeys ...string) *stubPorter {
	t.Helper()
	base := filepath.Join(t.TempDir(), "Brave-Browser")
	roots := map[string]string{}
	for _, key := range profileKeys {
		root := filepath.Join(base, key)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		roots[key] = root
	}
	return &stubPorter{
		name:  "brave",
		roots: roots,
		groups: []apps.StateGroup{
			{Name: "bookmarks", Paths: []string{"Bookmarks"}, Default: true, Why: "plain JSON"},
			{Name: "tabs", Paths: []string{"Sessions"}, Default: true, Why: "the open tabs"},
			{Name: "history", Paths: []string{"History"}, Default: false, Why: "browsing record"},
		},
	}
}

// exportForImport writes a real bundle with `dg export` itself, so the
// import tests read exactly the bytes the export command produces.
func exportForImport(t *testing.T, groups ...string) string {
	t.Helper()
	source := newStubPorter(t, "Default")
	resetStateFlags(t, source)
	exportGroupFlag = groups
	destDir := t.TempDir()
	if err := runExport(exportCmd, []string{"brave", destDir}); err != nil {
		t.Fatalf("export fixture: %v", err)
	}
	return filepath.Join(destDir, "brave-state-2026-09-15"+appstate.BundleExt)
}

func TestRunImportRestoresTheBundle(t *testing.T) {
	bundle := exportForImport(t)
	dest := newEmptyStubPorter(t, "Default")
	report := resetStateFlags(t, dest)

	if err := runImport(importCmd, []string{"brave", bundle}); err != nil {
		t.Fatalf("runImport: %v", err)
	}

	for _, rel := range []string{"Bookmarks", filepath.Join("Sessions", "Session_1")} {
		if _, err := os.Stat(filepath.Join(dest.roots["Default"], rel)); err != nil {
			t.Errorf("%s was not restored: %v", rel, err)
		}
	}
	// Nothing was replaced on this fresh machine, so there is no undo to
	// offer and no reason to mention one.
	if strings.Contains(report.String(), appstate.BackupSuffix) {
		t.Errorf("the import offered an undo for a profile that had nothing:\n%s", report.String())
	}
}

func TestRunImportRefusesWithoutForceWhenStateExists(t *testing.T) {
	bundle := exportForImport(t, "bookmarks")
	dest := newStubPorter(t, "Default") // already has state
	resetStateFlags(t, dest)
	before, err := os.ReadFile(filepath.Join(dest.roots["Default"], "Bookmarks"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	err = runImport(importCmd, []string{"brave", bundle})
	if err == nil {
		t.Fatal("expected a refusal without --force, got nil")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error %q does not name the flag that would allow it", err)
	}

	after, err := os.ReadFile(filepath.Join(dest.roots["Default"], "Bookmarks"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(before) != string(after) {
		t.Error("the refused import changed the profile anyway")
	}
}

func TestRunImportWithForceReplacesAndBacksUp(t *testing.T) {
	bundle := exportForImport(t, "bookmarks")
	dest := newStubPorter(t, "Default")
	report := resetStateFlags(t, dest)
	importForceFlag = true

	if err := runImport(importCmd, []string{"brave", bundle}); err != nil {
		t.Fatalf("runImport --force: %v", err)
	}

	backup := filepath.Join(dest.roots["Default"], "Bookmarks"+appstate.BackupSuffix)
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("expected a backup beside the replaced file: %v", err)
	}
	// An undo nobody was told about is not one, so the suffix and the paths
	// are printed whenever something was actually replaced.
	out := report.String()
	if !strings.Contains(out, appstate.BackupSuffix) || !strings.Contains(out, "Bookmarks") {
		t.Errorf("the import never says where the undo is:\n%s", out)
	}
}

func TestRunImportPassesTheGroupFlagThrough(t *testing.T) {
	bundle := exportForImport(t, "bookmarks", "history")
	dest := newEmptyStubPorter(t, "Default")
	resetStateFlags(t, dest)
	importGroupFlag = []string{"bookmarks"}

	if err := runImport(importCmd, []string{"brave", bundle}); err != nil {
		t.Fatalf("runImport --group bookmarks: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest.roots["Default"], "Bookmarks")); err != nil {
		t.Errorf("Bookmarks was not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest.roots["Default"], "History")); !os.IsNotExist(err) {
		t.Error("History was restored although --group did not name it")
	}
}

func TestRunImportPassesTheProfileFlagThrough(t *testing.T) {
	source := newStubPorter(t, "Default", "Profile 1")
	resetStateFlags(t, source)
	exportGroupFlag = []string{"bookmarks"}
	exportDir := t.TempDir()
	if err := runExport(exportCmd, []string{"brave", exportDir}); err != nil {
		t.Fatalf("export fixture: %v", err)
	}
	bundle := filepath.Join(exportDir, "brave-state-2026-09-15"+appstate.BundleExt)

	dest := newEmptyStubPorter(t, "Default", "Profile 1")
	resetStateFlags(t, dest)
	importProfileFlag = []string{"Profile 1"}

	if err := runImport(importCmd, []string{"brave", bundle}); err != nil {
		t.Fatalf("runImport --profile: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest.roots["Profile 1"], "Bookmarks")); err != nil {
		t.Errorf("Profile 1's Bookmarks was not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest.roots["Default"], "Bookmarks")); !os.IsNotExist(err) {
		t.Error("Default was restored although --profile did not name it")
	}
}

func TestRunImportRefusesABundleNamedForAnotherApp(t *testing.T) {
	bundle := exportForImport(t, "bookmarks")
	renamed := filepath.Join(filepath.Dir(bundle), "chrome-state-2026-09-15"+appstate.BundleExt)
	if err := os.Rename(bundle, renamed); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	dest := newEmptyStubPorter(t, "Default")
	resetStateFlags(t, dest)
	importForceFlag = true

	err := runImport(importCmd, []string{"brave", renamed})
	if err == nil {
		t.Fatal("expected a refusal for a bundle named for another app, got nil")
	}
	if !strings.Contains(err.Error(), "brave-state-") {
		t.Errorf("error %q does not say what the name must be", err)
	}
}

// TestRunImportDrawsNoMeterForARefusal: several of the import's checks
// refuse before the bundle is read at all, and a meter started up front
// prints "Verifying 0 B" directly above an error that has nothing to do
// with verification.
func TestRunImportDrawsNoMeterForARefusal(t *testing.T) {
	bundle := exportForImport(t, "bookmarks")
	renamed := filepath.Join(filepath.Dir(bundle), "chrome-state-2026-09-15"+appstate.BundleExt)
	if err := os.Rename(bundle, renamed); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	dest := newEmptyStubPorter(t, "Default")
	resetStateFlags(t, dest)
	var meter strings.Builder
	archiveProgressOut = &meter

	if err := runImport(importCmd, []string{"brave", renamed}); err == nil {
		t.Fatal("expected a refusal, got nil")
	}
	if meter.String() != "" {
		t.Errorf("a refusal that read nothing drew a progress meter:\n%q", meter.String())
	}
}

func TestRunImportRefusesAMissingBundle(t *testing.T) {
	dest := newEmptyStubPorter(t, "Default")
	resetStateFlags(t, dest)

	missing := filepath.Join(t.TempDir(), "brave-state-2026-09-15"+appstate.BundleExt)
	if err := runImport(importCmd, []string{"brave", missing}); err == nil {
		t.Fatal("expected an error for a bundle that is not there, got nil")
	}
}
