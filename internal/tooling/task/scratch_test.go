package task

import (
	"os"
	"path/filepath"
	"testing"
)

func newScratchTestManager(t *testing.T) (*TaskManager, string) {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", "")
	return &TaskManager{}, tmpDir
}

func TestScratch(t *testing.T) {
	t.Run("returns a fresh path each call", func(t *testing.T) {
		tm, _ := newScratchTestManager(t)

		first, err := tm.Scratch()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		info, err := os.Stat(first)
		if err != nil || !info.IsDir() {
			t.Fatalf("expected %q to be a directory: %v", first, err)
		}

		second, err := tm.Scratch()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if first == second {
			t.Errorf("expected distinct paths, got %q twice", first)
		}
	})

	t.Run("succeeds with the scratch root deleted beforehand", func(t *testing.T) {
		tm, home := newScratchTestManager(t)

		root := filepath.Join(home, ".cache", "devgeta", "scratch")
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatalf("failed to pre-create scratch root: %v", err)
		}
		if err := os.RemoveAll(root); err != nil {
			t.Fatalf("failed to remove scratch root: %v", err)
		}

		dir, err := tm.Scratch()
		if err != nil {
			t.Fatalf("expected allocation to self-heal a missing root, got error: %v", err)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("expected %q to exist as a directory: %v", dir, err)
		}
	})
}

func TestScratchClean(t *testing.T) {
	t.Run("removes its own directory", func(t *testing.T) {
		tm, _ := newScratchTestManager(t)

		dir, err := tm.Scratch()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := tm.ScratchClean(dir); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("expected %q to be removed, stat err=%v", dir, err)
		}
	})

	t.Run("refuses the scratch root itself", func(t *testing.T) {
		tm, home := newScratchTestManager(t)
		if _, err := tm.Scratch(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		root := filepath.Join(home, ".cache", "devgeta", "scratch")

		if err := tm.ScratchClean(root); err == nil {
			t.Error("expected an error cleaning the scratch root itself")
		}
		if _, err := os.Stat(root); err != nil {
			t.Errorf("expected the root to still exist, got stat err=%v", err)
		}
	})

	t.Run("refuses a grandchild", func(t *testing.T) {
		tm, _ := newScratchTestManager(t)
		dir, err := tm.Scratch()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		grandchild := filepath.Join(dir, "nested")
		if err := os.MkdirAll(grandchild, 0o755); err != nil {
			t.Fatalf("failed to create grandchild: %v", err)
		}

		if err := tm.ScratchClean(grandchild); err == nil {
			t.Error("expected an error cleaning a grandchild of the scratch root")
		}
		if _, err := os.Stat(grandchild); err != nil {
			t.Errorf("expected the grandchild to still exist, got stat err=%v", err)
		}
	})

	t.Run("refuses a directory beside the root", func(t *testing.T) {
		tm, home := newScratchTestManager(t)
		if _, err := tm.Scratch(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		sibling := filepath.Join(home, ".cache", "devgeta", "other")
		if err := os.MkdirAll(sibling, 0o755); err != nil {
			t.Fatalf("failed to create sibling: %v", err)
		}

		if err := tm.ScratchClean(sibling); err == nil {
			t.Error("expected an error cleaning a directory beside the scratch root")
		}
		if _, err := os.Stat(sibling); err != nil {
			t.Errorf("expected the sibling to still exist, got stat err=%v", err)
		}
	})

	t.Run("refuses a direct child lacking the allocation prefix", func(t *testing.T) {
		tm, home := newScratchTestManager(t)
		if _, err := tm.Scratch(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		unprefixed := filepath.Join(home, ".cache", "devgeta", "scratch", "not-allocated")
		if err := os.MkdirAll(unprefixed, 0o755); err != nil {
			t.Fatalf("failed to create unprefixed dir: %v", err)
		}

		if err := tm.ScratchClean(unprefixed); err == nil {
			t.Error("expected an error cleaning an unprefixed child")
		}
		if _, err := os.Stat(unprefixed); err != nil {
			t.Errorf("expected it to still exist, got stat err=%v", err)
		}
	})

	t.Run("refuses a .. escape", func(t *testing.T) {
		tm, home := newScratchTestManager(t)
		dir, err := tm.Scratch()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		etc := filepath.Join(home, "etc-outside-scratch")
		if err := os.MkdirAll(etc, 0o755); err != nil {
			t.Fatalf("failed to create escape target: %v", err)
		}
		escape := filepath.Join(dir, "..", "..", "etc-outside-scratch")

		if err := tm.ScratchClean(escape); err == nil {
			t.Error("expected an error for a .. escape")
		}
		if _, err := os.Stat(etc); err != nil {
			t.Errorf("expected the escape target to still exist, got stat err=%v", err)
		}
	})

	t.Run("refuses a symlink pointing outside the root", func(t *testing.T) {
		tm, home := newScratchTestManager(t)
		root := filepath.Join(home, ".cache", "devgeta", "scratch")
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatalf("failed to create scratch root: %v", err)
		}
		outside := filepath.Join(home, "outside-target")
		if err := os.MkdirAll(outside, 0o755); err != nil {
			t.Fatalf("failed to create outside target: %v", err)
		}
		link := filepath.Join(root, "task-symlinkescape")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatalf("failed to create symlink: %v", err)
		}

		if err := tm.ScratchClean(link); err == nil {
			t.Error("expected an error for a symlink pointing outside the scratch root")
		}
		if _, err := os.Stat(outside); err != nil {
			t.Errorf("expected the outside target to still exist, got stat err=%v", err)
		}
	})

	t.Run("refuses a relative path", func(t *testing.T) {
		tm, _ := newScratchTestManager(t)
		dir, err := tm.Scratch()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		rel := filepath.Base(dir) // e.g. "task-928919772", not absolute

		if err := tm.ScratchClean(rel); err == nil {
			t.Error("expected an error for a relative path")
		}
	})

	// A prefixed symlink is refused whether or not it resolves. An earlier
	// version fell back to the lexical path when EvalSymlinks failed, so a
	// live symlink out of the root was refused but a broken or looping one
	// silently passed every check and got unlinked.
	t.Run("refuses a broken symlink even though it carries the prefix", func(t *testing.T) {
		tm, home := newScratchTestManager(t)
		root := filepath.Join(home, ".cache", "devgeta", "scratch")
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatalf("failed to create scratch root: %v", err)
		}
		link := filepath.Join(root, "task-broken")
		if err := os.Symlink(filepath.Join(home, "does-not-exist"), link); err != nil {
			t.Fatalf("failed to create broken symlink: %v", err)
		}

		if err := tm.ScratchClean(link); err == nil {
			t.Error("expected an error for a broken symlink")
		}
		if _, err := os.Lstat(link); err != nil {
			t.Errorf("expected the symlink itself to survive, lstat err=%v", err)
		}
	})

	t.Run("refuses a looping symlink even though it carries the prefix", func(t *testing.T) {
		tm, home := newScratchTestManager(t)
		root := filepath.Join(home, ".cache", "devgeta", "scratch")
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatalf("failed to create scratch root: %v", err)
		}
		a := filepath.Join(root, "task-loopa")
		b := filepath.Join(root, "task-loopb")
		if err := os.Symlink(b, a); err != nil {
			t.Fatalf("failed to create symlink a: %v", err)
		}
		if err := os.Symlink(a, b); err != nil {
			t.Fatalf("failed to create symlink b: %v", err)
		}

		if err := tm.ScratchClean(a); err == nil {
			t.Error("expected an error for a looping symlink")
		}
		if _, err := os.Lstat(a); err != nil {
			t.Errorf("expected the symlink itself to survive, lstat err=%v", err)
		}
	})

	t.Run("is idempotent for a target that no longer exists", func(t *testing.T) {
		tm, _ := newScratchTestManager(t)
		dir, err := tm.Scratch()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := tm.ScratchClean(dir); err != nil {
			t.Fatalf("unexpected error on first clean: %v", err)
		}

		// A command's cleanup step may run twice (retry, or a cleanup that
		// also fires on an error path) — the second call must not fail it.
		if err := tm.ScratchClean(dir); err != nil {
			t.Errorf("expected a repeated clean to succeed, got: %v", err)
		}
	})
}
