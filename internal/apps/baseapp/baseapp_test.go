package baseapp

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestMaintainScratchDir(t *testing.T) {
	t.Run("creates the scratch dir when absent", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CACHE_HOME", "")

		if err := MaintainScratchDir(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := filepath.Join(home, ".cache", "devgeta", "scratch")
		info, err := os.Stat(want)
		if err != nil || !info.IsDir() {
			t.Fatalf("expected %q to exist as a directory: %v", want, err)
		}
	})

	t.Run("prunes a stale subdirectory but keeps a fresh one", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CACHE_HOME", "")

		root := filepath.Join(home, ".cache", "devgeta", "scratch")
		stale := filepath.Join(root, "task-stale")
		fresh := filepath.Join(root, "task-fresh")
		writeFileTree(t, filepath.Join(stale, "x.txt"), "old")
		writeFileTree(t, filepath.Join(fresh, "x.txt"), "new")

		old := time.Now().Add(-25 * time.Hour)
		if err := os.Chtimes(stale, old, old); err != nil {
			t.Fatalf("failed to backdate %s: %v", stale, err)
		}

		if err := MaintainScratchDir(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Errorf("expected stale scratch dir to be pruned, stat err=%v", err)
		}
		if _, err := os.Stat(fresh); err != nil {
			t.Errorf("expected fresh scratch dir to survive, stat err=%v", err)
		}
	})

	// The scratch root is granted to the agent, so a user may keep something
	// of their own in there. Pruning is bounded by the same ownership rule
	// --clean enforces: only paths.ScratchAllocPrefix directories are ours
	// to delete, however old anything else is.
	t.Run("leaves an old directory that devgeta did not allocate", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CACHE_HOME", "")

		root := filepath.Join(home, ".cache", "devgeta", "scratch")
		theirs := filepath.Join(root, "my-own-notes")
		staleOurs := filepath.Join(root, "task-stale")
		writeFileTree(t, filepath.Join(theirs, "keep.txt"), "user data")
		writeFileTree(t, filepath.Join(staleOurs, "x.txt"), "ours")

		old := time.Now().Add(-72 * time.Hour)
		for _, d := range []string{theirs, staleOurs} {
			if err := os.Chtimes(d, old, old); err != nil {
				t.Fatalf("failed to backdate %s: %v", d, err)
			}
		}

		if err := MaintainScratchDir(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := os.Stat(filepath.Join(theirs, "keep.txt")); err != nil {
			t.Errorf(
				"expected an unprefixed user directory to survive pruning, stat err=%v — "+
					"pruning must not exceed what devgeta allocated",
				err,
			)
		}
		if _, err := os.Stat(staleOurs); !os.IsNotExist(err) {
			t.Errorf("expected the stale task-* dir to be pruned, stat err=%v", err)
		}
	})

	t.Run("leaves an old symlink in the root alone", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CACHE_HOME", "")

		root := filepath.Join(home, ".cache", "devgeta", "scratch")
		outside := filepath.Join(home, "outside")
		writeFileTree(t, filepath.Join(outside, "precious.txt"), "data")
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatalf("failed to create scratch root: %v", err)
		}
		link := filepath.Join(root, "task-link")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatalf("failed to create symlink: %v", err)
		}

		if err := MaintainScratchDir(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := os.Lstat(link); err != nil {
			t.Errorf("expected the symlink to survive pruning, lstat err=%v", err)
		}
		if _, err := os.Stat(filepath.Join(outside, "precious.txt")); err != nil {
			t.Errorf("expected the symlink target to be untouched, stat err=%v", err)
		}
	})

	t.Run("leaves plain files in the root alone", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_CACHE_HOME", "")

		root := filepath.Join(home, ".cache", "devgeta", "scratch")
		stray := filepath.Join(root, "not-a-dir.txt")
		writeFileTree(t, stray, "leftover")
		old := time.Now().Add(-48 * time.Hour)
		if err := os.Chtimes(stray, old, old); err != nil {
			t.Fatalf("failed to backdate %s: %v", stray, err)
		}

		if err := MaintainScratchDir(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if _, err := os.Stat(stray); err != nil {
			t.Errorf("expected a stray file (not a directory) to survive pruning, stat err=%v", err)
		}
	})
}
