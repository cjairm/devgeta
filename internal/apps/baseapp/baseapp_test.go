package baseapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/paths"
)

func init() { testutil.InitLogger() }

func TestReinstall_UninstallSucceeds(t *testing.T) {
	uninstalled := false
	installed := false

	err := Reinstall(
		func() error { installed = true; return nil },
		func() error { uninstalled = true; return nil },
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !uninstalled {
		t.Error("expected uninstall to be called")
	}
	if !installed {
		t.Error("expected install to be called")
	}
}

func TestReinstall_UninstallReturnsNotSupported(t *testing.T) {
	installed := false

	err := Reinstall(
		func() error { installed = true; return nil },
		func() error { return fmt.Errorf("wrapped: %w", apps.ErrUninstallNotSupported) },
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !installed {
		t.Error("expected install to still be called when uninstall is not supported")
	}
}

func TestReinstall_UninstallReturnsOtherError(t *testing.T) {
	installErr := errors.New("some uninstall failure")
	installed := false

	err := Reinstall(
		func() error { installed = true; return nil },
		func() error { return installErr },
	)
	if !errors.Is(err, installErr) {
		t.Fatalf("expected uninstall error to propagate, got: %v", err)
	}
	if installed {
		t.Error("install should not be called when uninstall returns a real error")
	}
}

func TestReinstall_InstallReturnsError(t *testing.T) {
	installErr := errors.New("install failed")

	err := Reinstall(
		func() error { return installErr },
		func() error { return apps.ErrUninstallNotSupported },
	)
	if !errors.Is(err, installErr) {
		t.Fatalf("expected install error to propagate, got: %v", err)
	}
}

func writeFileTree(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSyncSharedParts(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Embedded shared source with two parts.
	writeFileTree(t, filepath.Join(src, "skills", "demo", "SKILL.md"), "new skill")
	writeFileTree(t, filepath.Join(src, "commands", "c.md"), "a command")

	oldShared := paths.Paths.App.Configs.Shared
	t.Cleanup(func() { paths.Paths.App.Configs.Shared = oldShared })
	paths.Paths.App.Configs.Shared = src

	// Destination already has a stale skill and a hand-edited general config file.
	writeFileTree(t, filepath.Join(dst, "skills", "stale", "SKILL.md"), "old")
	writeFileTree(t, filepath.Join(dst, "settings.json"), "user edits")

	if err := SyncSharedParts(dst, []string{"skills"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// New skill is synced in.
	if got, err := os.ReadFile(filepath.Join(dst, "skills", "demo", "SKILL.md")); err != nil ||
		string(got) != "new skill" {
		t.Fatalf("skill not synced: got %q err %v", got, err)
	}
	// Stale skill is removed — full mirror, not a merge.
	if _, err := os.Stat(filepath.Join(dst, "skills", "stale")); !os.IsNotExist(err) {
		t.Fatal("expected stale skill to be removed")
	}
	// General config outside the selected parts is untouched.
	if got, err := os.ReadFile(filepath.Join(dst, "settings.json")); err != nil ||
		string(got) != "user edits" {
		t.Fatalf("settings.json should be untouched: got %q err %v", got, err)
	}
	// An unselected part is never created.
	if _, err := os.Stat(filepath.Join(dst, "commands")); !os.IsNotExist(err) {
		t.Fatal("unselected part 'commands' should not be synced")
	}
}
