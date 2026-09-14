package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// resetArchiveFlags saves the current archive flag/seam values and restores
// them after the test, so tests can freely set what they need.
func resetArchiveFlags(t *testing.T) {
	t.Helper()
	origDryRun := archiveDryRunFlag
	origGzip := archiveGzipFlag
	origNoSkip := archiveNoSkipFlag
	origNoVerify := archiveNoVerifyFlag
	origMacMetadata := archiveMacMetadataFlag
	origYes := archiveYesFlag
	origGOOS := archiveGOOS
	origNow := archiveNow
	t.Cleanup(func() {
		archiveDryRunFlag = origDryRun
		archiveGzipFlag = origGzip
		archiveNoSkipFlag = origNoSkip
		archiveNoVerifyFlag = origNoVerify
		archiveMacMetadataFlag = origMacMetadata
		archiveYesFlag = origYes
		archiveGOOS = origGOOS
		archiveNow = origNow
	})
	archiveDryRunFlag = false
	archiveGzipFlag = false
	archiveNoSkipFlag = false
	archiveNoVerifyFlag = false
	archiveMacMetadataFlag = false
	archiveYesFlag = false
}

func TestRunArchiveRejectsNonDirectorySource(t *testing.T) {
	resetArchiveFlags(t)
	root := t.TempDir()
	file := filepath.Join(root, "notadir.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	destDir := t.TempDir()

	if err := runArchive(archiveCmd, []string{file, destDir}); err == nil {
		t.Fatal("expected an error for a non-directory source")
	}
}

func TestRunArchiveRejectsMissingDestination(t *testing.T) {
	resetArchiveFlags(t)
	source := t.TempDir()

	if err := runArchive(
		archiveCmd,
		[]string{source, filepath.Join(source, "does-not-exist")},
	); err == nil {
		t.Fatal("expected an error for a missing destination")
	}
}

func TestRunArchiveRejectsDestinationInsideSource(t *testing.T) {
	resetArchiveFlags(t)
	source := t.TempDir()
	dest := filepath.Join(source, "sub")
	if err := os.Mkdir(dest, 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if err := runArchive(archiveCmd, []string{source, dest}); err == nil {
		t.Fatal("expected an error when the destination is inside the source")
	}
}

func TestRunArchiveRejectsMacMetadataOnNonDarwin(t *testing.T) {
	resetArchiveFlags(t)
	archiveMacMetadataFlag = true
	archiveGOOS = "linux"
	source := t.TempDir()
	destDir := t.TempDir()

	err := runArchive(archiveCmd, []string{source, destDir})
	if err == nil {
		t.Fatal("expected --mac-metadata to be rejected on a non-darwin GOOS")
	}
}

func TestRunArchiveRejectsExistingArchiveName(t *testing.T) {
	resetArchiveFlags(t)
	archiveYesFlag = true
	source := t.TempDir()
	destDir := t.TempDir()
	fixedNow := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	archiveNow = func() time.Time { return fixedNow }

	existing := filepath.Join(destDir, filepath.Base(source)+"-2026-09-13.tar.zst")
	if err := os.WriteFile(existing, []byte("already here"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := runArchive(archiveCmd, []string{source, destDir}); err == nil {
		t.Fatal("expected an error when the final archive name already exists")
	}
}

func TestRunArchiveRefusesUnreadableFiles(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission bits do not block reads")
	}
	resetArchiveFlags(t)
	archiveYesFlag = true
	source := t.TempDir()
	secret := filepath.Join(source, "secret.txt")
	if err := os.WriteFile(secret, []byte("shh"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(secret, 0o000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(secret, 0o644) })
	destDir := t.TempDir()

	if err := runArchive(archiveCmd, []string{source, destDir}); err == nil {
		t.Fatal("expected an error when an unreadable file is found")
	}

	remaining, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected nothing written to destDir, found %v", remaining)
	}
}

func TestRunArchiveDryRunWritesNothing(t *testing.T) {
	resetArchiveFlags(t)
	archiveDryRunFlag = true
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	destDir := t.TempDir()

	if err := runArchive(archiveCmd, []string{source, destDir}); err != nil {
		t.Fatalf("runArchive: %v", err)
	}

	remaining, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected nothing written in --dry-run, found %v", remaining)
	}
}

func TestRunArchiveRefusesWithoutYesWhenNonInteractive(t *testing.T) {
	resetArchiveFlags(t)
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	destDir := t.TempDir()

	if err := runArchive(archiveCmd, []string{source, destDir}); err == nil {
		t.Fatal("expected an error without --yes in a non-interactive session")
	}

	remaining, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected nothing written, found %v", remaining)
	}
}

func TestRunArchiveWritesAndVerifiesWithYes(t *testing.T) {
	resetArchiveFlags(t)
	archiveYesFlag = true
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	destDir := t.TempDir()

	if err := runArchive(archiveCmd, []string{source, destDir}); err != nil {
		t.Fatalf("runArchive: %v", err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	wantSuffixes := []string{".tar.zst", ".sha256", ".skipped.txt", ".tar.zst.sha256"}
	for _, suffix := range wantSuffixes {
		found := false
		for _, n := range names {
			if filepath.Ext(n) != "" && len(n) >= len(suffix) && n[len(n)-len(suffix):] == suffix {
				found = true
			}
		}
		if !found {
			t.Errorf("expected a file ending in %q in destDir, got %v", suffix, names)
		}
	}
}

func TestRunArchiveNoVerifySkipsVerification(t *testing.T) {
	resetArchiveFlags(t)
	archiveYesFlag = true
	archiveNoVerifyFlag = true
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	destDir := t.TempDir()

	if err := runArchive(archiveCmd, []string{source, destDir}); err != nil {
		t.Fatalf("runArchive: %v", err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".sha256" &&
			filepath.Ext(e.Name()[:len(e.Name())-len(".sha256")]) == ".zst" {
			t.Errorf("expected no archive-hash sidecar file with --no-verify, found %q", e.Name())
		}
	}
}

func TestArchiveVerifySubcommandSucceeds(t *testing.T) {
	resetArchiveFlags(t)
	archiveYesFlag = true
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	destDir := t.TempDir()
	if err := runArchive(archiveCmd, []string{source, destDir}); err != nil {
		t.Fatalf("runArchive: %v", err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var archivePath string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".zst" {
			archivePath = filepath.Join(destDir, e.Name())
		}
	}
	if archivePath == "" {
		t.Fatalf("no .tar.zst found in %v", entries)
	}

	if err := runArchiveVerify(archiveVerifyCmd, []string{archivePath}); err != nil {
		t.Fatalf("runArchiveVerify: %v", err)
	}
}
