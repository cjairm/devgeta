package task

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// uniqueRepoSlug returns a repo slug that is unique to this test, so tests
// that create real directories under worktree.GetWorktreeBasePath() (which
// resolves under go test's sandboxed paths.Paths.Data.Root, shared across the
// whole test binary) never collide with each other's fixtures.
func uniqueRepoSlug(t *testing.T) string {
	t.Helper()
	return "repo-" + filepath.Base(t.TempDir())
}

// chdir temporarily changes the process's working directory to dir for the
// duration of the test, restoring the original on cleanup. Needed to exercise
// resolveWorktreeTarget's cwd-based resolution path (os.Getwd() cannot be
// mocked through the Git app).
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Logf("failed to restore cwd: %v", err)
		}
	})
}

func TestWorktreeStart(t *testing.T) {
	t.Run("requires name", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()

		_, err := tm.WorktreeStart("", "")
		if err == nil {
			t.Fatal("expected error for empty name")
		}
		testutil.VerifyNoRealCommands(t, gitBase)
	})

	t.Run("refuses to start from a dirty tree", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResult("?? untracked.go\n", "", nil)

		_, err := tm.WorktreeStart("add-retry", "")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "dirty tree") {
			t.Errorf("expected dirty-tree error, got: %v", err)
		}
		if len(gitBase.ExecCommandCalls) != 1 {
			t.Fatalf(
				"expected exactly 1 git call (the dirty check), got %d",
				len(gitBase.ExecCommandCalls),
			)
		}
	})

	t.Run("errors when not in a git repository", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // status --porcelain (clean)
			commands.ExecCommandResult(
				"",
				"fatal: not a git repository",
				fmt.Errorf("exit 128"),
			), // rev-parse --show-toplevel
		)

		_, err := tm.WorktreeStart("add-retry", "")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not in a git repository") {
			t.Errorf("expected not-in-a-git-repository error, got: %v", err)
		}
	})

	t.Run("explicit --base hand-rolls a single worktree add", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		repoSlug := uniqueRepoSlug(t)
		repoRoot := "/fake/repos/" + repoSlug
		t.Cleanup(func() {
			_ = os.RemoveAll(filepath.Join(worktree.GetWorktreeBasePath(), repoSlug))
		})

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),            // status --porcelain (clean)
			commands.ExecCommandResult(repoRoot+"\n", "", nil), // rev-parse --show-toplevel
			commands.ExecCommandResult("", "", nil),            // fetch origin
			commands.ExecCommandResult("", "", nil),            // worktree add -b name path base
		)

		out, err := tm.WorktreeStart("hotfix-123", "origin/release-2.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		wantPath := filepath.Join(worktree.GetWorktreeBasePath(), repoSlug, "hotfix-123")
		if !strings.Contains(out, wantPath) {
			t.Errorf("expected output to contain %q, got %q", wantPath, out)
		}
		if !strings.Contains(out, "base origin/release-2.0") {
			t.Errorf("expected output to name the explicit base, got %q", out)
		}

		calls := gitBase.ExecCommandCalls
		if len(calls) != 4 {
			t.Fatalf("expected 4 git calls, got %d", len(calls))
		}
		last := calls[3]
		if last.Command != "git" {
			t.Fatalf("expected git command, got %q", last.Command)
		}
		wantArgs := []string{
			"-C", repoRoot, "worktree", "add", "-b", "hotfix-123", wantPath, "origin/release-2.0",
		}
		if len(last.Args) != len(wantArgs) {
			t.Fatalf("expected args %v, got %v", wantArgs, last.Args)
		}
		for i, a := range wantArgs {
			if last.Args[i] != a {
				t.Errorf("arg[%d]: expected %q, got %q", i, a, last.Args[i])
			}
		}
	})

	t.Run("errors when the worktree path already exists", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		repoSlug := uniqueRepoSlug(t)
		repoRoot := "/fake/repos/" + repoSlug
		wtPath := filepath.Join(worktree.GetWorktreeBasePath(), repoSlug, "add-retry")
		if err := os.MkdirAll(wtPath, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Cleanup(func() {
			_ = os.RemoveAll(filepath.Join(worktree.GetWorktreeBasePath(), repoSlug))
		})

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult(repoRoot+"\n", "", nil),
			commands.ExecCommandResult("", "", nil), // fetch origin
		)

		_, err := tm.WorktreeStart("add-retry", "")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "already exists") {
			t.Errorf("expected already-exists error, got: %v", err)
		}
	})

	t.Run(
		"default base reuses CreateWorktreeIn for a fresh branch off origin default",
		func(t *testing.T) {
			tm, gitBase, _ := newTaskSetup()
			repoSlug := uniqueRepoSlug(t)
			repoRoot := "/fake/repos/" + repoSlug
			t.Cleanup(func() {
				_ = os.RemoveAll(filepath.Join(worktree.GetWorktreeBasePath(), repoSlug))
			})

			gitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 1: status --porcelain (clean)
				commands.ExecCommandResult(
					repoRoot+"\n",
					"",
					nil,
				), // 2: rev-parse --show-toplevel
				commands.ExecCommandResult("", "", nil),              // 3: fetch origin (explicit)
				commands.ExecCommandResult("origin/main\n", "", nil), // 4: symbolic-ref (label)
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 5: fetch origin (inside CreateWorktreeIn, ignored)
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 6: branch --list add-retry (doesn't exist)
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 7: branch -r --list origin/add-retry (doesn't exist)
				commands.ExecCommandResult(
					"origin/main\n",
					"",
					nil,
				), // 8: symbolic-ref (inside CreateWorktreeIn)
				commands.ExecCommandResult(
					"origin/main\n",
					"",
					nil,
				), // 9: branch -r --list origin/main (exists)
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 10: worktree add path -b add-retry origin/main
			)

			out, err := tm.WorktreeStart("add-retry", "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			wantPath := filepath.Join(worktree.GetWorktreeBasePath(), repoSlug, "add-retry")
			if !strings.Contains(out, wantPath) {
				t.Errorf("expected output to contain %q, got %q", wantPath, out)
			}
			if !strings.Contains(out, "base origin/main") {
				t.Errorf("expected output to name origin/main as the base, got %q", out)
			}

			calls := gitBase.ExecCommandCalls
			if len(calls) != 10 {
				t.Fatalf("expected 10 git calls, got %d: %+v", len(calls), calls)
			}
			last := calls[9]
			assertCmd(t, last, "git",
				"-C", repoRoot, "worktree", "add", wantPath, "-b", "add-retry", "origin/main")
		},
	)
}

func TestWorktreeFinish_FlagInterplay(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()

	t.Run("neither merge nor discard", func(t *testing.T) {
		_, err := tm.WorktreeFinish("x", false, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("both merge and discard", func(t *testing.T) {
		_, err := tm.WorktreeFinish("x", true, true, false)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	testutil.VerifyNoRealCommands(t, gitBase)
}

func TestWorktreeFinish_TargetResolution(t *testing.T) {
	t.Run("explicit name not found lists available worktrees", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		otherSlug := uniqueRepoSlug(t)
		otherDir := filepath.Join(worktree.GetWorktreeBasePath(), otherSlug, "other-task")
		if err := os.MkdirAll(otherDir, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Cleanup(func() {
			_ = os.RemoveAll(filepath.Join(worktree.GetWorktreeBasePath(), otherSlug))
		})

		_, err := tm.WorktreeFinish("does-not-exist", true, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "does-not-exist") {
			t.Errorf("expected error to name the missing worktree, got: %v", err)
		}
		if !strings.Contains(err.Error(), otherSlug+"/other-task") {
			t.Errorf("expected error to list available worktrees, got: %v", err)
		}
		testutil.VerifyNoRealCommands(t, gitBase)
	})

	t.Run("explicit name found resolves branch", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		repoSlug := uniqueRepoSlug(t)
		wtPath := filepath.Join(worktree.GetWorktreeBasePath(), repoSlug, "add-retry")
		if err := os.MkdirAll(wtPath, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Cleanup(func() {
			_ = os.RemoveAll(filepath.Join(worktree.GetWorktreeBasePath(), repoSlug))
		})

		// worktree list --porcelain output for branchForWorktree, followed by
		// the discard path's dirty check and removal.
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			), // ListWorktreesAt(wtPath)
			commands.ExecCommandResult("", "", nil), // IsWorktreeDirty(wtPath) -> clean
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree, resolved BEFORE removal (wtPath is gone after)
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // RemoveWorktree -> its own GetMainWorktree
			commands.ExecCommandResult("", "", nil), // worktree remove
			commands.ExecCommandResult("", "", nil), // branch -D
			commands.ExecCommandResult(
				"/main/.git\n", "", nil,
			), // dropReviewJournal -> rev-parse --git-common-dir (ADR-0012)
		)

		out, err := tm.WorktreeFinish("add-retry", false, true, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "add-retry") || !strings.Contains(out, wtPath) {
			t.Errorf("unexpected confirmation: %q", out)
		}
	})

	t.Run("no name, not inside a git repository", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		tmpDir := t.TempDir()
		chdir(t, tmpDir)

		gitBase.SetExecCommandResult("", "fatal: not a git repository", fmt.Errorf("exit 128"))

		_, err := tm.WorktreeFinish("", true, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not inside a git repository") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("no name, cwd is the main checkout", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		tmpDir := t.TempDir()
		chdir(t, tmpDir)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(tmpDir+"\n", "", nil), // GetRepoRootIn
			commands.ExecCommandResult(
				"worktree "+tmpDir+"\nHEAD abc123\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree -> same path as repoRoot
		)

		_, err := tm.WorktreeFinish("", true, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "main checkout") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("no name, cwd resolves to a linked worktree", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		tmpDir := t.TempDir()
		chdir(t, tmpDir)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				tmpDir+"\n",
				"",
				nil,
			), // GetRepoRootIn -> the linked worktree itself
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree -> different path
			commands.ExecCommandResult(
				"worktree "+tmpDir+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			), // ListWorktreesAt(tmpDir) for branchForWorktree
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // IsWorktreeDirty(tmpDir) -> clean (discard path)
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree, resolved BEFORE removal (wtPath is gone after)
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // RemoveWorktree -> its own GetMainWorktree
			commands.ExecCommandResult("", "", nil), // worktree remove
			commands.ExecCommandResult("", "", nil), // branch -D
			commands.ExecCommandResult(
				"/main/.git\n", "", nil,
			), // dropReviewJournal -> rev-parse --git-common-dir (ADR-0012)
		)

		out, err := tm.WorktreeFinish("", false, true, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "add-retry") {
			t.Errorf("expected confirmation to name the resolved branch, got %q", out)
		}
	})
}

func TestWorktreeFinish_Discard(t *testing.T) {
	t.Run("refuses on a dirty worktree without --force", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		repoSlug := uniqueRepoSlug(t)
		wtPath := filepath.Join(worktree.GetWorktreeBasePath(), repoSlug, "spike")
		if err := os.MkdirAll(wtPath, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Cleanup(func() {
			_ = os.RemoveAll(filepath.Join(worktree.GetWorktreeBasePath(), repoSlug))
		})

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/spike\n\n", "", nil,
			), // ListWorktreesAt
			commands.ExecCommandResult("?? scratch.go\n", "", nil), // IsWorktreeDirty -> dirty
		)

		_, err := tm.WorktreeFinish("spike", false, true, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "--force") {
			t.Errorf("expected error to mention --force, got: %v", err)
		}
	})

	t.Run("--force discards a dirty worktree", func(t *testing.T) {
		tm, gitBase, _ := newTaskSetup()
		repoSlug := uniqueRepoSlug(t)
		wtPath := filepath.Join(worktree.GetWorktreeBasePath(), repoSlug, "spike")
		if err := os.MkdirAll(wtPath, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Cleanup(func() {
			_ = os.RemoveAll(filepath.Join(worktree.GetWorktreeBasePath(), repoSlug))
		})

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/spike\n\n", "", nil,
			), // ListWorktreesAt
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree, resolved BEFORE removal (wtPath is gone after)
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // RemoveWorktree -> its own GetMainWorktree
			commands.ExecCommandResult("", "", nil), // worktree remove
			commands.ExecCommandResult("", "", nil), // branch -D
			commands.ExecCommandResult(
				"/main/.git\n", "", nil,
			), // dropReviewJournal -> rev-parse --git-common-dir (ADR-0012)
		)

		out, err := tm.WorktreeFinish("spike", false, true, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Discarded") || !strings.Contains(out, "spike") {
			t.Errorf("unexpected confirmation: %q", out)
		}
	})

	t.Run(
		"--force falls back to a direct removal when git worktree remove still refuses",
		func(t *testing.T) {
			// Regression test: `git worktree remove` refuses on modified/untracked
			// files with no way to force it through RemoveWorktree (see
			// forceDiscardFallback's doc comment). Caught manually: --force did
			// nothing until this fallback was added.
			tm, gitBase, _ := newTaskSetup()
			repoSlug := uniqueRepoSlug(t)
			wtPath := filepath.Join(worktree.GetWorktreeBasePath(), repoSlug, "spike")
			if err := os.MkdirAll(wtPath, 0o755); err != nil {
				t.Fatalf("setup: %v", err)
			}
			mainWorktree := filepath.Join(worktree.GetWorktreeBasePath(), repoSlug, "main-checkout")
			t.Cleanup(func() {
				_ = os.RemoveAll(filepath.Join(worktree.GetWorktreeBasePath(), repoSlug))
			})

			gitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/spike\n\n", "", nil,
				), // ListWorktreesAt
				commands.ExecCommandResult(
					"worktree "+mainWorktree+"\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				), // GetMainWorktree, resolved BEFORE removal (wtPath is gone after)
				commands.ExecCommandResult(
					"worktree "+mainWorktree+"\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				), // RemoveWorktree -> its own GetMainWorktree
				commands.ExecCommandResult(
					"",
					"contains modified or untracked files, use --force to delete it",
					fmt.Errorf("exit 1"),
				), // worktree remove fails — the refusal this test exists for
				commands.ExecCommandResult(
					"worktree "+mainWorktree+"\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				), // forceDiscardFallback -> GetMainWorktree
				commands.ExecCommandResult("", "", nil), // worktree prune
				commands.ExecCommandResult("", "", nil), // branch -D
				commands.ExecCommandResult(
					"/main/.git\n", "", nil,
				), // dropReviewJournal -> rev-parse --git-common-dir (ADR-0012)
			)

			out, err := tm.WorktreeFinish("spike", false, true, true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out, "Discarded") || !strings.Contains(out, "spike") {
				t.Errorf("unexpected confirmation: %q", out)
			}
			if _, statErr := os.Stat(wtPath); !os.IsNotExist(statErr) {
				t.Error("expected the worktree directory to be removed by the fallback")
			}

			calls := gitBase.ExecCommandCalls
			assertCmd(t, calls[5], "git", "-C", mainWorktree, "worktree", "prune")
			assertCmd(t, calls[6], "git", "-C", mainWorktree, "branch", "-D", "spike")
		},
	)

	t.Run(
		"--force fallback reports the real os.RemoveAll failure, not the stale removeErr",
		func(t *testing.T) {
			// Regression test for the bug where forceDiscardFallback's
			// os.RemoveAll branch wrapped removeErr (the original `git worktree
			// remove` failure) instead of the RemoveAll failure that actually
			// just happened. Forces a real RemoveAll failure via a read-only
			// worktree directory containing a file, so RemoveAll cannot unlink
			// it, and asserts the surfaced error is about THAT failure, not the
			// stale "modified or untracked files" text from the original
			// `worktree remove` failure.
			tm, gitBase, _ := newTaskSetup()
			repoSlug := uniqueRepoSlug(t)
			wtPath := filepath.Join(worktree.GetWorktreeBasePath(), repoSlug, "spike")
			if err := os.MkdirAll(wtPath, 0o755); err != nil {
				t.Fatalf("setup: %v", err)
			}
			lockedFile := filepath.Join(wtPath, "locked.txt")
			if err := os.WriteFile(lockedFile, []byte("x"), 0o644); err != nil {
				t.Fatalf("setup: %v", err)
			}
			// Remove write permission on wtPath itself so RemoveAll cannot
			// unlink locked.txt from inside it.
			if err := os.Chmod(wtPath, 0o555); err != nil {
				t.Fatalf("setup: %v", err)
			}
			t.Cleanup(func() {
				_ = os.Chmod(wtPath, 0o755)
				_ = os.RemoveAll(filepath.Join(worktree.GetWorktreeBasePath(), repoSlug))
			})
			mainWorktree := filepath.Join(worktree.GetWorktreeBasePath(), repoSlug, "main-checkout")

			gitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/spike\n\n", "", nil,
				), // ListWorktreesAt
				commands.ExecCommandResult(
					"worktree "+mainWorktree+"\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				), // RemoveWorktree -> GetMainWorktree
				commands.ExecCommandResult(
					"",
					"contains modified or untracked files, use --force to delete it",
					fmt.Errorf("exit 1"),
				), // worktree remove fails (this is the stale removeErr)
				commands.ExecCommandResult(
					"worktree "+mainWorktree+"\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				), // forceDiscardFallback -> GetMainWorktree
			)

			_, err := tm.WorktreeFinish("spike", false, true, true)
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), "modified or untracked files") {
				t.Errorf(
					"expected the stale removeErr NOT to appear in the error, got: %v", err,
				)
			}
			if !strings.Contains(err.Error(), "locked.txt") {
				t.Errorf(
					"expected the real os.RemoveAll failure (mentioning locked.txt) to appear, got: %v",
					err,
				)
			}
		},
	)
}

func TestWorktreeFinish_Merge(t *testing.T) {
	newFixture := func(t *testing.T) (tm *TaskManager, gitBase *commands.MockBaseCommand, wtPath string) {
		t.Helper()
		tm, gitBase, _ = newTaskSetup()
		repoSlug := uniqueRepoSlug(t)
		wtPath = filepath.Join(worktree.GetWorktreeBasePath(), repoSlug, "add-retry")
		if err := os.MkdirAll(wtPath, 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		t.Cleanup(func() {
			_ = os.RemoveAll(filepath.Join(worktree.GetWorktreeBasePath(), repoSlug))
		})
		return tm, gitBase, wtPath
	}

	t.Run("not diverged: straight fast-forward merge and removal", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			), // ListWorktreesAt(wtPath) for branchForWorktree
			commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn(wtPath)
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree(wtPath)
			commands.ExecCommandResult("main\n", "", nil), // branch --show-current at /main
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // merge-base --is-ancestor -> ancestor (no rebase)
			commands.ExecCommandResult("", "", nil), // merge --ff-only
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // RemoveWorktree -> GetMainWorktree
			commands.ExecCommandResult("", "", nil), // worktree remove
			commands.ExecCommandResult("", "", nil), // branch -D
			commands.ExecCommandResult(
				"/main/.git\n", "", nil,
			), // dropReviewJournal -> rev-parse --git-common-dir (ADR-0012)
		)

		out, err := tm.WorktreeFinish("add-retry", true, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Merged add-retry into main") {
			t.Errorf("unexpected confirmation: %q", out)
		}

		// The rebase must NOT have been called: 10 calls total, no extra rebase
		// call. The 10th is the review journal's location lookup — the branch
		// was just deleted, so its remembered review exchanges go with it.
		if len(gitBase.ExecCommandCalls) != 10 {
			t.Fatalf("expected 10 git calls (no rebase), got %d: %+v",
				len(gitBase.ExecCommandCalls), gitBase.ExecCommandCalls)
		}
	})

	t.Run("diverged: rebases before the fast-forward merge", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			), // ListWorktreesAt
			commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree
			commands.ExecCommandResult(
				"main\n",
				"",
				nil,
			), // branch --show-current
			commands.ExecCommandResult(
				"",
				"not an ancestor",
				fmt.Errorf("exit 1"),
			), // merge-base --is-ancestor -> diverged
			commands.ExecCommandResult("", "", nil), // rebase main
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // merge --ff-only
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // RemoveWorktree -> GetMainWorktree
			commands.ExecCommandResult("", "", nil), // worktree remove
			commands.ExecCommandResult("", "", nil), // branch -D
			commands.ExecCommandResult(
				"/main/.git\n", "", nil,
			), // dropReviewJournal -> rev-parse --git-common-dir (ADR-0012)
		)

		out, err := tm.WorktreeFinish("add-retry", true, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Merged add-retry into main") {
			t.Errorf("unexpected confirmation: %q", out)
		}

		calls := gitBase.ExecCommandCalls
		// 11 = the 10 merge-path calls plus the review journal's location lookup.
		if len(calls) != 11 {
			t.Fatalf("expected 11 git calls (with rebase), got %d: %+v", len(calls), calls)
		}
		assertCmd(t, calls[5], "git", "-C", wtPath, "rebase", "main")
	})

	t.Run("main checkout on the wrong branch refuses before touching anything", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			),
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("some-other-branch\n", "", nil), // branch --show-current
		)

		_, err := tm.WorktreeFinish("add-retry", true, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "some-other-branch") {
			t.Errorf("expected error to name the main checkout's actual branch, got: %v", err)
		}

		if len(gitBase.ExecCommandCalls) != 4 {
			t.Fatalf(
				"expected exactly 4 git calls (nothing touched), got %d",
				len(gitBase.ExecCommandCalls),
			)
		}
	})

	t.Run("rebase failure leaves state intact with actionable guidance", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			),
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("main\n", "", nil),
			commands.ExecCommandResult("", "not an ancestor", fmt.Errorf("exit 1")),
			commands.ExecCommandResult("", "CONFLICT", fmt.Errorf("exit 1")), // rebase fails
		)

		_, err := tm.WorktreeFinish("add-retry", true, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "rebase --abort") {
			t.Errorf("expected guidance mentioning rebase --abort, got: %v", err)
		}
		// Nothing beyond the rebase attempt should have run.
		if len(gitBase.ExecCommandCalls) != 6 {
			t.Fatalf("expected exactly 6 git calls, got %d", len(gitBase.ExecCommandCalls))
		}
	})

	t.Run("fast-forward failure leaves the worktree in place", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			),
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("main\n", "", nil),
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // ancestor, no rebase needed
			commands.ExecCommandResult(
				"",
				"not possible to fast-forward",
				fmt.Errorf("exit 1"),
			), // merge --ff-only fails
		)

		_, err := tm.WorktreeFinish("add-retry", true, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "worktree left in place") {
			t.Errorf("expected error to say the worktree was left in place, got: %v", err)
		}
		if len(gitBase.ExecCommandCalls) != 6 {
			t.Fatalf(
				"expected exactly 6 git calls (no removal attempted), got %d",
				len(gitBase.ExecCommandCalls),
			)
		}
	})

	t.Run("removal failure after a successful merge says so explicitly", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			),
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("main\n", "", nil),
			commands.ExecCommandResult("", "", nil), // ancestor
			commands.ExecCommandResult("", "", nil), // merge --ff-only succeeds
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // RemoveWorktree -> GetMainWorktree
			commands.ExecCommandResult(
				"",
				"still has modifications",
				fmt.Errorf("exit 1"),
			), // worktree remove fails
		)

		_, err := tm.WorktreeFinish("add-retry", true, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "merged add-retry into main") {
			t.Errorf("expected error to confirm the merge already landed, got: %v", err)
		}
		if !strings.Contains(err.Error(), "worktree still at") {
			t.Errorf("expected error to say the worktree is still present, got: %v", err)
		}
	})

	t.Run(
		"branch delete failure after a successful worktree removal doesn't claim the worktree still exists",
		func(t *testing.T) {
			// Regression test: RemoveWorktree can fail because `worktree remove`
			// itself failed (worktree genuinely still there) OR because
			// `worktree remove` succeeded and only the following `branch -D`
			// failed (worktree already gone). This sub-case must not get the
			// "(worktree still at %s)" message — the worktree isn't there
			// anymore.
			tm, gitBase, wtPath := newFixture(t)

			gitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
				),
				commands.ExecCommandResult("origin/main\n", "", nil),
				commands.ExecCommandResult(
					"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				),
				commands.ExecCommandResult("main\n", "", nil),
				commands.ExecCommandResult("", "", nil), // ancestor
				commands.ExecCommandResult("", "", nil), // merge --ff-only succeeds
				commands.ExecCommandResult(
					"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				), // RemoveWorktree -> GetMainWorktree
				commands.ExecCommandResult("", "", nil), // worktree remove SUCCEEDS
				commands.ExecCommandResult(
					"",
					"error: branch 'add-retry' not found",
					fmt.Errorf("exit 1"),
				), // branch -D fails
			)

			_, err := tm.WorktreeFinish("add-retry", true, false, false)
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), "worktree still at") {
				t.Errorf(
					"expected error NOT to falsely claim the worktree still exists, got: %v", err,
				)
			}
			if !strings.Contains(err.Error(), "removed the worktree") {
				t.Errorf(
					"expected error to confirm the worktree was already removed, got: %v", err,
				)
			}
			if !strings.Contains(err.Error(), "failed to delete branch") {
				t.Errorf("expected error to say branch deletion failed, got: %v", err)
			}
		},
	)
}
