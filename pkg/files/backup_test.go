package files

import (
	"os"
	"path/filepath"
	"testing"
)

// testSuffixes is a caller's suffix pair. The parameter exists because two
// callers need the same primitives under different names: `dg theme set`
// marks its own in-flight switch, and `dg import` marks its own.
var testSuffixes = BackupSuffixes{Backup: ".test-backup", Absent: ".test-absent"}

func TestBackupPathRenamesAnExistingFileAside(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	if err := os.WriteFile(p, []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := BackupPath(p, testSuffixes); err != nil {
		t.Fatalf("BackupPath: %v", err)
	}

	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Errorf("expected %s to have been renamed aside, got err=%v", p, err)
	}
	content, err := os.ReadFile(p + testSuffixes.Backup)
	if err != nil {
		t.Fatalf("reading the backup: %v", err)
	}
	if string(content) != "original" {
		t.Errorf("backup holds %q, want %q", content, "original")
	}
}

// TestBackupPathMarksAnAbsentPath: a rename-aside cannot record "there was
// nothing here", and the rollback of an added file has to be a delete.
func TestBackupPathMarksAnAbsentPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nested", "config")

	if err := BackupPath(p, testSuffixes); err != nil {
		t.Fatalf("BackupPath: %v", err)
	}

	if _, err := os.Lstat(p + testSuffixes.Absent); err != nil {
		t.Errorf("expected an absent marker beside %s: %v", p, err)
	}
}

func TestRestorePathPutsTheBackupBack(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	if err := os.WriteFile(p, []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := BackupPath(p, testSuffixes); err != nil {
		t.Fatalf("BackupPath: %v", err)
	}
	if err := os.WriteFile(p, []byte("replacement"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := RestorePath(p, testSuffixes); err != nil {
		t.Fatalf("RestorePath: %v", err)
	}

	content, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("reading the restored file: %v", err)
	}
	if string(content) != "original" {
		t.Errorf("restored content is %q, want %q", content, "original")
	}
	if _, err := os.Lstat(p + testSuffixes.Backup); !os.IsNotExist(err) {
		t.Errorf("the backup sibling outlived the restore")
	}
}

func TestRestorePathDeletesWhatWasAdded(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	if err := BackupPath(p, testSuffixes); err != nil {
		t.Fatalf("BackupPath: %v", err)
	}
	if err := os.WriteFile(p, []byte("added"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := RestorePath(p, testSuffixes); err != nil {
		t.Fatalf("RestorePath: %v", err)
	}

	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Errorf("expected %s to be gone again, got err=%v", p, err)
	}
	if _, err := os.Lstat(p + testSuffixes.Absent); !os.IsNotExist(err) {
		t.Errorf("the absent marker outlived the restore")
	}
}

func TestRestorePathLeavesAnUnbackedPathAlone(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")
	if err := os.WriteFile(p, []byte("untouched"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := RestorePath(p, testSuffixes); err != nil {
		t.Fatalf("RestorePath: %v", err)
	}

	content, err := os.ReadFile(p)
	if err != nil || string(content) != "untouched" {
		t.Errorf("RestorePath disturbed a path nothing had backed up: %q, %v", content, err)
	}
}

func TestBackupRoundTripOnADirectory(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Sessions")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p, "Session_1"), []byte("tabs"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := BackupPath(p, testSuffixes); err != nil {
		t.Fatalf("BackupPath: %v", err)
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p, "Session_9"), []byte("other"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := RestorePath(p, testSuffixes); err != nil {
		t.Fatalf("RestorePath: %v", err)
	}

	if _, err := os.Lstat(filepath.Join(p, "Session_1")); err != nil {
		t.Errorf("the original directory did not come back: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(p, "Session_9")); !os.IsNotExist(err) {
		t.Errorf("the replacement directory survived the restore")
	}
}

func TestDiscardBackupRemovesBothSiblings(t *testing.T) {
	dir := t.TempDir()
	withBackup := filepath.Join(dir, "a")
	withMarker := filepath.Join(dir, "b")
	if err := os.WriteFile(withBackup, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := BackupPath(withBackup, testSuffixes); err != nil {
		t.Fatalf("BackupPath: %v", err)
	}
	if err := BackupPath(withMarker, testSuffixes); err != nil {
		t.Fatalf("BackupPath: %v", err)
	}

	for _, p := range []string{withBackup, withMarker} {
		if err := DiscardBackup(p, testSuffixes); err != nil {
			t.Fatalf("DiscardBackup(%s): %v", p, err)
		}
		if HasBackup(p, testSuffixes) {
			t.Errorf("%s still has a backup sibling after DiscardBackup", p)
		}
	}
}

func TestHasBackup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config")

	if HasBackup(p, testSuffixes) {
		t.Error("HasBackup is true for a path with no siblings")
	}
	if err := BackupPath(p, testSuffixes); err != nil {
		t.Fatalf("BackupPath: %v", err)
	}
	if !HasBackup(p, testSuffixes) {
		t.Error("HasBackup is false right after BackupPath left an absent marker")
	}
}
