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

// TestScratchKeyValidation is the adversarial table for validateScratchKey
// (ADR-0033): a pure function, so this table IS the manual verification for
// this security boundary, per CLAUDE.md's table-driven-verification note.
func TestScratchKeyValidation(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"a plain word", "demo", false},
		{"letters, digits, and dashes", "issue-42-scope", false},
		{"underscores", "review_round_1", false},
		{"empty string", "", true},
		{"whitespace only", "   ", true},
		{"a single dot", ".", true},
		{"a double dot", "..", true},
		{"a leading slash", "/demo", true},
		{"a trailing slash", "demo/", true},
		{"an interior slash", "a/b", true},
		{"a parent escape", "../escape", true},
		{"a nested parent escape", "a/../b", true},
		{"a backslash", "a\\b", true},
		{"a null byte", "a\x00b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateScratchKey(tt.key)
			if tt.wantErr && err == nil {
				t.Errorf("validateScratchKey(%q): expected an error, got nil", tt.key)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateScratchKey(%q): unexpected error: %v", tt.key, err)
			}
		})
	}
}

func TestScratch(t *testing.T) {
	t.Run("returns a fresh path each call", func(t *testing.T) {
		tm, _ := newScratchTestManager(t)

		first, err := tm.Scratch("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		info, err := os.Stat(first)
		if err != nil || !info.IsDir() {
			t.Fatalf("expected %q to be a directory: %v", first, err)
		}

		second, err := tm.Scratch("")
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

		dir, err := tm.Scratch("")
		if err != nil {
			t.Fatalf("expected allocation to self-heal a missing root, got error: %v", err)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("expected %q to exist as a directory: %v", dir, err)
		}
	})
}

// TestScratchKeyed exercises Scratch("key") — the deterministic, re-derivable
// allocation path ADR-0033 adds. An empty key's behavior is covered by
// TestScratch above and must stay byte-for-byte the same.
func TestScratchKeyed(t *testing.T) {
	t.Run("same key twice returns the same path", func(t *testing.T) {
		tm, _ := newScratchTestManager(t)

		first, err := tm.Scratch("demo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		second, err := tm.Scratch("demo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if first != second {
			t.Errorf("expected the same path for the same key, got %q then %q", first, second)
		}
		if info, err := os.Stat(first); err != nil || !info.IsDir() {
			t.Fatalf("expected %q to be a directory: %v", first, err)
		}
	})

	t.Run("different keys return different paths", func(t *testing.T) {
		tm, _ := newScratchTestManager(t)

		a, err := tm.Scratch("alpha")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		b, err := tm.Scratch("beta")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if a == b {
			t.Errorf("expected distinct paths for distinct keys, got %q twice", a)
		}
	})

	t.Run("keyed dir carries the key prefix, not the allocation prefix", func(t *testing.T) {
		tm, home := newScratchTestManager(t)

		dir, err := tm.Scratch("demo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		root := filepath.Join(home, ".cache", "devgeta", "scratch")
		want := filepath.Join(root, "key-demo")
		if dir != want {
			t.Errorf("expected %q, got %q", want, dir)
		}
	})

	t.Run("an invalid key is refused and nothing is created", func(t *testing.T) {
		tm, home := newScratchTestManager(t)

		if _, err := tm.Scratch("../escape"); err == nil {
			t.Error("expected an error for an invalid key")
		}
		root := filepath.Join(home, ".cache", "devgeta", "scratch")
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("unexpected error reading root: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected nothing created under the root, found %v", entries)
		}
	})

	t.Run("refuses to reuse a symlink substituted at the keyed path", func(t *testing.T) {
		tm, home := newScratchTestManager(t)
		root := filepath.Join(home, ".cache", "devgeta", "scratch")
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatalf("failed to create scratch root: %v", err)
		}
		outside := filepath.Join(home, "outside-target")
		if err := os.MkdirAll(outside, 0o755); err != nil {
			t.Fatalf("failed to create outside target: %v", err)
		}
		link := filepath.Join(root, "key-demo")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatalf("failed to create symlink: %v", err)
		}

		if _, err := tm.Scratch("demo"); err == nil {
			t.Error("expected an error reusing a symlink at the keyed path")
		}
		// The symlink itself, and its target, must be left exactly as they were.
		if lst, err := os.Lstat(link); err != nil || lst.Mode()&os.ModeSymlink == 0 {
			t.Errorf("expected the symlink to survive untouched, lstat err=%v", err)
		}
	})

	t.Run("refuses to reuse a plain file substituted at the keyed path", func(t *testing.T) {
		tm, home := newScratchTestManager(t)
		root := filepath.Join(home, ".cache", "devgeta", "scratch")
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatalf("failed to create scratch root: %v", err)
		}
		stray := filepath.Join(root, "key-demo")
		if err := os.WriteFile(stray, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("failed to create stray file: %v", err)
		}

		if _, err := tm.Scratch("demo"); err == nil {
			t.Error("expected an error reusing a non-directory at the keyed path")
		}
	})
}

func TestScratchClean(t *testing.T) {
	t.Run("removes a keyed directory too", func(t *testing.T) {
		tm, _ := newScratchTestManager(t)

		dir, err := tm.Scratch("demo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := tm.ScratchClean(dir); err != nil {
			t.Fatalf("unexpected error cleaning a keyed dir: %v", err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("expected %q to be removed, stat err=%v", dir, err)
		}
	})

	t.Run("removes its own directory", func(t *testing.T) {
		tm, _ := newScratchTestManager(t)

		dir, err := tm.Scratch("")
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
		if _, err := tm.Scratch(""); err != nil {
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
		dir, err := tm.Scratch("")
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
		if _, err := tm.Scratch(""); err != nil {
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
		if _, err := tm.Scratch(""); err != nil {
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
		dir, err := tm.Scratch("")
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
		dir, err := tm.Scratch("")
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
		dir, err := tm.Scratch("")
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
