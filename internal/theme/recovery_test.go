package theme

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/paths"
)

func init() {
	testutil.InitLogger()
}

// isolateManifestPaths points every path Manifest()/RecoverInterrupted touch
// at a fresh temp tree and restores the originals on cleanup. paths.Paths is
// a plain value (every field a string or a struct of strings), so saving and
// restoring the whole var is safe and covers every root the manifest spans -
// the trap CLAUDE.md's testing-patterns guide calls out for tests that
// isolate some but not all of the paths an operation touches.
func isolateManifestPaths(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	orig := paths.Paths
	t.Cleanup(func() { paths.Paths = orig })

	paths.Paths.Config.Root = filepath.Join(root, "config")
	paths.Paths.Config.Alacritty = filepath.Join(root, "alacritty")
	paths.Paths.Config.Ghostty = filepath.Join(root, "ghostty")
	paths.Paths.Config.OpenCode = filepath.Join(root, "opencode")
	paths.Paths.Home.Root = filepath.Join(root, "home")
	paths.Paths.Config.Nvim = filepath.Join(root, "nvim")
	paths.Paths.Config.Claude = filepath.Join(root, "claude")
	paths.Paths.Config.I3 = filepath.Join(root, "i3")

	config.ResetGlobalConfigCacheForTest()
	t.Cleanup(config.ResetGlobalConfigCacheForTest)

	return root
}

func setPendingTheme(t *testing.T, name string) {
	t.Helper()
	if err := config.Update(func(gc *config.GlobalConfig) error {
		gc.PendingTheme = name
		return nil
	}); err != nil {
		t.Fatalf("failed to set pending_theme: %v", err)
	}
}

func currentPendingTheme(t *testing.T) string {
	t.Helper()
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatalf("failed to load global config: %v", err)
	}
	return gc.PendingTheme
}

func TestBackupManifestPath_ExistingFile_RenamesAside(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "file.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := BackupPath(p); err != nil {
		t.Fatalf("BackupPath error: %v", err)
	}

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("expected original path to be gone, err=%v", err)
	}
	data, err := os.ReadFile(p + backupSuffix)
	if err != nil {
		t.Fatalf("expected backup file: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("backup content = %q, want hello", data)
	}
}

func TestBackupManifestPath_MissingFile_CreatesAbsentMarker(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "file.txt")

	if err := BackupPath(p); err != nil {
		t.Fatalf("BackupPath error: %v", err)
	}

	if _, err := os.Stat(p + absentSuffix); err != nil {
		t.Errorf("expected absent marker: %v", err)
	}
}

func TestRestoreManifestPath_FromBackup(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "file.txt")
	if err := os.WriteFile(p, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := BackupPath(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("new content"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RestorePath(p); err != nil {
		t.Fatalf("RestorePath error: %v", err)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("expected restored file: %v", err)
	}
	if string(data) != "original" {
		t.Errorf("restored content = %q, want original", data)
	}
	if _, err := os.Stat(p + backupSuffix); !os.IsNotExist(err) {
		t.Errorf("expected backup to be consumed, err=%v", err)
	}
}

func TestRestoreManifestPath_FromAbsentMarker_DeletesPath(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "file.txt")
	if err := BackupPath(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("newly created"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := RestorePath(p); err != nil {
		t.Fatalf("RestorePath error: %v", err)
	}

	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("expected path to be absent again, err=%v", err)
	}
	if _, err := os.Stat(p + absentSuffix); !os.IsNotExist(err) {
		t.Errorf("expected marker to be consumed, err=%v", err)
	}
}

func TestDiscardManifestBackup_RemovesBackupAndMarker(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "file.txt")
	if err := os.WriteFile(p+backupSuffix, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := DiscardBackup(p); err != nil {
		t.Fatalf("DiscardBackup error: %v", err)
	}

	if _, err := os.Stat(p + backupSuffix); !os.IsNotExist(err) {
		t.Errorf("expected backup to be removed, err=%v", err)
	}
}

func TestRecoverInterrupted_NothingToDo_ReturnsEmpty(t *testing.T) {
	isolateManifestPaths(t)

	msg, err := RecoverInterrupted()
	if err != nil {
		t.Fatalf("RecoverInterrupted error: %v", err)
	}
	if msg != "" {
		t.Errorf("expected empty message, got %q", msg)
	}
}

func TestRecoverInterrupted_PendingWithBackups_Restores(t *testing.T) {
	isolateManifestPaths(t)

	alacrittyPath := filepath.Join(paths.Paths.Config.Alacritty, "alacritty.toml")
	if err := os.MkdirAll(filepath.Dir(alacrittyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alacrittyPath, []byte("old-theme-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	setPendingTheme(t, "tokyonight")
	if err := BackupPath(alacrittyPath); err != nil {
		t.Fatal(err)
	}
	// Simulate the half-applied new theme's write.
	if err := os.WriteFile(alacrittyPath, []byte("new-theme-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	msg, err := RecoverInterrupted()
	if err != nil {
		t.Fatalf("RecoverInterrupted error: %v", err)
	}
	if msg == "" {
		t.Error("expected a non-empty recovery message")
	}

	data, err := os.ReadFile(alacrittyPath)
	if err != nil {
		t.Fatalf("expected restored file: %v", err)
	}
	if string(data) != "old-theme-content" {
		t.Errorf("content = %q, want old-theme-content", data)
	}
	if pending := currentPendingTheme(t); pending != "" {
		t.Errorf("expected pending_theme cleared, got %q", pending)
	}
	if _, err := os.Stat(alacrittyPath + backupSuffix); !os.IsNotExist(err) {
		t.Errorf("expected backup consumed, err=%v", err)
	}
}

func TestRecoverInterrupted_PendingNoBackups_ClearsPending(t *testing.T) {
	isolateManifestPaths(t)
	setPendingTheme(t, "tokyonight")

	msg, err := RecoverInterrupted()
	if err != nil {
		t.Fatalf("RecoverInterrupted error: %v", err)
	}
	_ = msg

	if pending := currentPendingTheme(t); pending != "" {
		t.Errorf("expected pending_theme cleared, got %q", pending)
	}
}

func TestRecoverInterrupted_NoPendingWithBackups_Discards(t *testing.T) {
	isolateManifestPaths(t)

	alacrittyPath := filepath.Join(paths.Paths.Config.Alacritty, "alacritty.toml")
	if err := os.MkdirAll(filepath.Dir(alacrittyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// The committed case: current_theme already the new theme, backups still
	// on disk from a crash between commit and cleanup.
	if err := config.Update(func(gc *config.GlobalConfig) error {
		gc.CurrentTheme = "tokyonight"
		gc.PendingTheme = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alacrittyPath, []byte("new-theme-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A backup left behind, as if the pre-crash content had been renamed
	// aside during the (already-committed) switch.
	if err := os.WriteFile(
		alacrittyPath+backupSuffix,
		[]byte("old-theme-content"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	msg, err := RecoverInterrupted()
	if err != nil {
		t.Fatalf("RecoverInterrupted error: %v", err)
	}
	if msg == "" {
		t.Error("expected a non-empty cleanup message")
	}

	data, err := os.ReadFile(alacrittyPath)
	if err != nil {
		t.Fatalf("expected the deployed (new) file to remain: %v", err)
	}
	if string(data) != "new-theme-content" {
		t.Errorf(
			"content = %q, want new-theme-content (must not roll back a committed switch)",
			data,
		)
	}
	if _, err := os.Stat(alacrittyPath + backupSuffix); !os.IsNotExist(err) {
		t.Errorf("expected backup discarded, err=%v", err)
	}

	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatal(err)
	}
	if gc.CurrentTheme != "tokyonight" {
		t.Errorf("current_theme = %q, want tokyonight (must not be touched)", gc.CurrentTheme)
	}
}
