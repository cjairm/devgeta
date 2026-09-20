package cmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	origFreeBytes := archiveFreeBytes
	origProgressOut := archiveProgressOut
	t.Cleanup(func() {
		archiveDryRunFlag = origDryRun
		archiveGzipFlag = origGzip
		archiveNoSkipFlag = origNoSkip
		archiveNoVerifyFlag = origNoVerify
		archiveMacMetadataFlag = origMacMetadata
		archiveYesFlag = origYes
		archiveGOOS = origGOOS
		archiveNow = origNow
		archiveFreeBytes = origFreeBytes
		archiveProgressOut = origProgressOut
	})
	// Meters draw nowhere by default: a test that archives should not spray
	// progress lines into the test log.
	archiveProgressOut = io.Discard
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

// eachArchiveOutput runs fn once per file a full archive run writes, so a
// guard test covers every one of them rather than only the archive itself.
func eachArchiveOutput(t *testing.T, fn func(t *testing.T, label, filename string)) {
	t.Helper()
	for _, c := range []struct{ label, filename string }{
		{"the archive", "src-2026-09-13.tar.zst"},
		{"the manifest", "src-2026-09-13.sha256"},
		{"the skip report", "src-2026-09-13.skipped.txt"},
		{"the archive checksum", "src-2026-09-13.tar.zst.sha256"},
	} {
		t.Run(c.label, func(t *testing.T) { fn(t, c.label, c.filename) })
	}
}

func TestArchiveOutputPathsCoversEveryFileARunWrites(t *testing.T) {
	got := archiveOutputPaths("/dest", "src-2026-09-13", ".tar.zst")

	want := []string{
		"/dest/src-2026-09-13.tar.zst",
		"/dest/src-2026-09-13.sha256",
		"/dest/src-2026-09-13.skipped.txt",
		"/dest/src-2026-09-13.tar.zst.sha256",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d output paths, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("output path %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// A run must abort before creating anything if ANY of its outputs is already
// on the drive. The write phase renames its .partial files into place
// unconditionally, so a guard that only checked the archive would let a
// leftover manifest or skip report be replaced silently.
func TestRefuseIfAnyOutputExistsRefusesForEveryOutput(t *testing.T) {
	eachArchiveOutput(t, func(t *testing.T, label, filename string) {
		destDir := t.TempDir()
		existing := filepath.Join(destDir, filename)
		if err := os.WriteFile(existing, []byte("do not lose me"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		err := refuseIfAnyOutputExists(destDir, "src-2026-09-13", ".tar.zst")
		if err == nil {
			t.Fatalf("expected a refusal when %s already exists", label)
		}
		if !strings.Contains(err.Error(), filename) {
			t.Errorf("error %q should name the file in the way, %q", err, filename)
		}
		if !strings.Contains(err.Error(), destDir) {
			t.Errorf("error %q should name the directory it found them in", err)
		}

		data, rerr := os.ReadFile(existing)
		if rerr != nil {
			t.Fatalf("ReadFile: %v", rerr)
		}
		if string(data) != "do not lose me" {
			t.Errorf("%s was modified; it must be left exactly as found", label)
		}
	})
}

func TestRefuseIfAnyOutputExistsAllowsAnEmptyDestination(t *testing.T) {
	if err := refuseIfAnyOutputExists(t.TempDir(), "src-2026-09-13", ".tar.zst"); err != nil {
		t.Errorf("an empty destination should be accepted, got %v", err)
	}
}

func TestRefuseIfAnyOutputExistsIgnoresUnrelatedFiles(t *testing.T) {
	destDir := t.TempDir()
	for _, name := range []string{"holiday-photos.tar.zst", "src-2026-09-12.tar.zst", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(destDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	if err := refuseIfAnyOutputExists(destDir, "src-2026-09-13", ".tar.zst"); err != nil {
		t.Errorf("other archives on the drive must not block a run, got %v", err)
	}
}

// The regression this guards: an earlier run's sidecars survive after the
// archive itself is deleted, and the old guard only looked at the archive.
func TestRunArchiveRefusesWhenOnlySidecarsRemain(t *testing.T) {
	resetArchiveFlags(t)
	archiveYesFlag = true
	source := t.TempDir()
	destDir := t.TempDir()
	fixedNow := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	archiveNow = func() time.Time { return fixedNow }

	manifest := filepath.Join(destDir, filepath.Base(source)+"-2026-09-13.sha256")
	if err := os.WriteFile(manifest, []byte("an earlier run's manifest"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := runArchive(archiveCmd, []string{source, destDir}); err == nil {
		t.Fatal("expected a refusal when a previous run's manifest is still on the drive")
	}

	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "an earlier run's manifest" {
		t.Error("the existing manifest was overwritten; a refused run must touch nothing")
	}
}

func TestShortOnSpaceWarning(t *testing.T) {
	const gib = int64(1) << 30

	if got := shortOnSpaceWarning(100*gib, 50*gib); got != "" {
		t.Errorf("ample space should not warn, got %q", got)
	}
	if got := shortOnSpaceWarning(50*gib, 50*gib); got != "" {
		t.Errorf("exactly enough space should not warn, got %q", got)
	}

	warning := shortOnSpaceWarning(10*gib, 200*gib)
	if warning == "" {
		t.Fatal("a destination smaller than the source should warn")
	}
	for _, want := range []string{"200.0 GiB", "10.0 GiB", "discarded"} {
		if !strings.Contains(warning, want) {
			t.Errorf("warning %q is missing %q", warning, want)
		}
	}
}

// Short space is a warning, not a refusal: the archive is compressed, and by
// how much is unknowable until it is written.
func TestRunArchiveProceedsDespiteLowFreeSpace(t *testing.T) {
	resetArchiveFlags(t)
	archiveYesFlag = true
	archiveFreeBytes = func(string) (int64, error) { return 1, nil }
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	destDir := t.TempDir()

	if err := runArchive(archiveCmd, []string{source, destDir}); err != nil {
		t.Fatalf("a low free-space reading must warn, not refuse: %v", err)
	}
}

func TestRunArchiveSurvivesAnUnreadableFreeSpaceReading(t *testing.T) {
	resetArchiveFlags(t)
	archiveYesFlag = true
	archiveFreeBytes = func(string) (int64, error) { return 0, errors.New("statfs failed") }
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	destDir := t.TempDir()

	if err := runArchive(archiveCmd, []string{source, destDir}); err != nil {
		t.Fatalf("an unavailable free-space reading must not fail the run: %v", err)
	}
}

// The hint must never be a bare `tar -xf`: pasted in the wrong directory that
// unpacks a whole home folder over the user's working tree. It must name a
// destination and create it, since -C fails on a directory that isn't there.
func TestRestoreCommandExtractsIntoItsOwnDirectory(t *testing.T) {
	cases := []struct{ filename, want string }{
		{
			"Documents-2026-09-14.tar.zst",
			"mkdir -p Documents-2026-09-14 && tar --zstd -xf Documents-2026-09-14.tar.zst -C Documents-2026-09-14",
		},
		{
			"Documents-2026-09-14.tar.gz",
			"mkdir -p Documents-2026-09-14 && tar -xzf Documents-2026-09-14.tar.gz -C Documents-2026-09-14",
		},
	}
	for _, c := range cases {
		got := restoreCommand(c.filename)
		if got != c.want {
			t.Errorf("restoreCommand(%q) =\n  %q\nwant\n  %q", c.filename, got, c.want)
		}
		if !strings.Contains(got, " -C ") {
			t.Errorf(
				"restoreCommand(%q) = %q, must extract into a named directory",
				c.filename,
				got,
			)
		}
	}
}
