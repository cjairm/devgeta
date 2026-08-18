package task

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/internal/tooling/reviewjournal"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// exitError builds a real *exec.ExitError with the given exit code, mirroring
// internal/apps/git/git_test.go's helper of the same name: Git.IsAncestor and
// Git.MergeTreeConflicts distinguish their expected non-zero exits (1, or 1
// with empty stdout) from a genuine failure by errors.As-ing for
// *exec.ExitError, so a mocked error has to be a real one for those branches
// to exercise correctly — a plain fmt.Errorf would always fall through to the
// "real error" branch instead.
func exitError(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
	if err == nil {
		t.Fatalf("expected a non-nil error for exit code %d", code)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *exec.ExitError, got %T: %v", err, err)
	}
	return exitErr
}

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
		_, err := tm.WorktreeFinish("x", false, false, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("both merge and discard", func(t *testing.T) {
		_, err := tm.WorktreeFinish("x", true, true, false, false)
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

		_, err := tm.WorktreeFinish("does-not-exist", true, false, false, false)
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

		out, err := tm.WorktreeFinish("add-retry", false, true, false, false)
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

		_, err := tm.WorktreeFinish("", true, false, false, false)
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

		_, err := tm.WorktreeFinish("", true, false, false, false)
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

		out, err := tm.WorktreeFinish("", false, true, false, false)
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

		_, err := tm.WorktreeFinish("spike", false, true, false, false)
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

		out, err := tm.WorktreeFinish("spike", false, true, false, true)
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

			out, err := tm.WorktreeFinish("spike", false, true, false, true)
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

			_, err := tm.WorktreeFinish("spike", false, true, false, true)
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

// newMergeFixture builds a TaskManager and a real wtPath directory named
// "add-retry" for the merge-path tests below (TestWorktreeFinish_Merge and
// TestWorktreeFinish_MergeJournalGate), which both script the merge path's git
// calls against the same fictional main checkout at "/main".
func newMergeFixture(
	t *testing.T,
) (tm *TaskManager, gitBase *commands.MockBaseCommand, wtPath string) {
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

func TestWorktreeFinish_Merge(t *testing.T) {
	newFixture := newMergeFixture

	t.Run("not diverged: straight fast-forward merge and removal", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			), // ListWorktreesAt(wtPath) for branchForWorktree
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // IsWorktreeDirty(wtPath) -> clean
			commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn(wtPath)
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree(wtPath)
			commands.ExecCommandResult("main\n", "", nil), // branch --show-current at /main
			commands.ExecCommandResult("", "", nil),       // IsWorktreeDirty(mainWorktree) -> clean
			commands.ExecCommandResult(
				"/main/.git\n", "", nil,
			), // openBlockingFindings -> CommonDirIn(mainWorktree) (empty journal)
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

		out, err := tm.WorktreeFinish("add-retry", true, false, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Merged add-retry into main") {
			t.Errorf("unexpected confirmation: %q", out)
		}

		// The rebase must NOT have been called: 13 calls total, no extra rebase
		// call. The 7th is the journal-gate probe's location lookup
		// (openBlockingFindings), the 13th is dropReviewJournal's — the branch
		// was just deleted, so its remembered review exchanges go with it.
		if len(gitBase.ExecCommandCalls) != 13 {
			t.Fatalf("expected 13 git calls (no rebase), got %d: %+v",
				len(gitBase.ExecCommandCalls), gitBase.ExecCommandCalls)
		}
	})

	t.Run("diverged: rebases before the fast-forward merge", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			), // ListWorktreesAt
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // IsWorktreeDirty(wtPath) -> clean
			commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree
			commands.ExecCommandResult(
				"main\n",
				"",
				nil,
			), // branch --show-current
			commands.ExecCommandResult("", "", nil), // IsWorktreeDirty(mainWorktree) -> clean
			commands.ExecCommandResult(
				"/main/.git\n", "", nil,
			), // openBlockingFindings -> CommonDirIn(mainWorktree) (empty journal)
			commands.ExecCommandResult(
				"",
				"not an ancestor",
				exitError(t, 1),
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

		out, err := tm.WorktreeFinish("add-retry", true, false, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Merged add-retry into main") {
			t.Errorf("unexpected confirmation: %q", out)
		}

		calls := gitBase.ExecCommandCalls
		// 14 = the 12 merge-path calls, plus the journal-gate probe's location
		// lookup (openBlockingFindings), plus dropReviewJournal's own lookup.
		if len(calls) != 14 {
			t.Fatalf("expected 14 git calls (with rebase), got %d: %+v", len(calls), calls)
		}
		assertCmd(t, calls[8], "git", "-C", wtPath, "rebase", "main")
	})

	t.Run("refuses to merge a dirty worktree", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			), // ListWorktreesAt
			commands.ExecCommandResult(
				"?? scratch.go\n",
				"",
				nil,
			), // IsWorktreeDirty(wtPath) -> dirty
		)

		_, err := tm.WorktreeFinish("add-retry", true, false, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "commit or stash") {
			t.Errorf("expected error to name the fix, got: %v", err)
		}

		// Refused before anything else ran: the default branch was never even
		// resolved, let alone the main checkout touched.
		if len(gitBase.ExecCommandCalls) != 2 {
			t.Fatalf(
				"expected exactly 2 git calls (nothing beyond the dirty check), got %d",
				len(gitBase.ExecCommandCalls),
			)
		}
	})

	t.Run("refuses to merge into a dirty main checkout", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			), // ListWorktreesAt
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // IsWorktreeDirty(wtPath) -> clean
			commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree
			commands.ExecCommandResult("main\n", "", nil), // branch --show-current
			commands.ExecCommandResult(
				" M file.go\n",
				"",
				nil,
			), // IsWorktreeDirty(mainWorktree) -> dirty
		)

		_, err := tm.WorktreeFinish("add-retry", true, false, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "/main") {
			t.Errorf("expected error to name the main checkout's path, got: %v", err)
		}
		if !strings.Contains(err.Error(), "commit or stash") {
			t.Errorf("expected error to name the fix, got: %v", err)
		}

		// Refused right after the branch guard: merge-base was never reached.
		if len(gitBase.ExecCommandCalls) != 6 {
			t.Fatalf(
				"expected exactly 6 git calls (refused right after the branch guard), got %d",
				len(gitBase.ExecCommandCalls),
			)
		}
	})

	t.Run("main checkout on the wrong branch refuses before touching anything", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			),
			commands.ExecCommandResult("", "", nil), // IsWorktreeDirty(wtPath) -> clean
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("some-other-branch\n", "", nil), // branch --show-current
		)

		_, err := tm.WorktreeFinish("add-retry", true, false, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "some-other-branch") {
			t.Errorf("expected error to name the main checkout's actual branch, got: %v", err)
		}

		if len(gitBase.ExecCommandCalls) != 5 {
			t.Fatalf(
				"expected exactly 5 git calls (nothing touched), got %d",
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
			commands.ExecCommandResult("", "", nil), // IsWorktreeDirty(wtPath) -> clean
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("main\n", "", nil),
			commands.ExecCommandResult("", "", nil), // IsWorktreeDirty(mainWorktree) -> clean
			commands.ExecCommandResult(
				"/main/.git\n", "", nil,
			), // openBlockingFindings -> CommonDirIn(mainWorktree) (empty journal)
			commands.ExecCommandResult("", "not an ancestor", exitError(t, 1)),
			commands.ExecCommandResult("", "CONFLICT", fmt.Errorf("exit 1")), // rebase fails
		)

		_, err := tm.WorktreeFinish("add-retry", true, false, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "rebase --abort") {
			t.Errorf("expected guidance mentioning rebase --abort, got: %v", err)
		}
		// Nothing beyond the rebase attempt should have run.
		if len(gitBase.ExecCommandCalls) != 9 {
			t.Fatalf("expected exactly 9 git calls, got %d", len(gitBase.ExecCommandCalls))
		}
	})

	t.Run("fast-forward failure leaves the worktree in place", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			),
			commands.ExecCommandResult("", "", nil), // IsWorktreeDirty(wtPath) -> clean
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("main\n", "", nil),
			commands.ExecCommandResult("", "", nil), // IsWorktreeDirty(mainWorktree) -> clean
			commands.ExecCommandResult(
				"/main/.git\n", "", nil,
			), // openBlockingFindings -> CommonDirIn(mainWorktree) (empty journal)
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

		_, err := tm.WorktreeFinish("add-retry", true, false, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "worktree left in place") {
			t.Errorf("expected error to say the worktree was left in place, got: %v", err)
		}
		if len(gitBase.ExecCommandCalls) != 9 {
			t.Fatalf(
				"expected exactly 9 git calls (no removal attempted), got %d",
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
			commands.ExecCommandResult("", "", nil), // IsWorktreeDirty(wtPath) -> clean
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("main\n", "", nil),
			commands.ExecCommandResult("", "", nil), // IsWorktreeDirty(mainWorktree) -> clean
			commands.ExecCommandResult(
				"/main/.git\n", "", nil,
			), // openBlockingFindings -> CommonDirIn(mainWorktree) (empty journal)
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

		_, err := tm.WorktreeFinish("add-retry", true, false, false, false)
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
				commands.ExecCommandResult("", "", nil), // IsWorktreeDirty(wtPath) -> clean
				commands.ExecCommandResult("origin/main\n", "", nil),
				commands.ExecCommandResult(
					"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				),
				commands.ExecCommandResult("main\n", "", nil),
				commands.ExecCommandResult("", "", nil), // IsWorktreeDirty(mainWorktree) -> clean
				commands.ExecCommandResult(
					"/main/.git\n", "", nil,
				), // openBlockingFindings -> CommonDirIn(mainWorktree) (empty journal)
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

			_, err := tm.WorktreeFinish("add-retry", true, false, false, false)
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

// writeJournalAt writes a journal file for branch straight to disk at the
// location reviewjournal.Manager.PathFor would resolve from commonDir —
// commonDir/devgeta/review/<encoded-branch>.md — so a test can seed a
// journal's exact entries without going through the Manager's own git calls,
// which would consume from these tests' ordered mock results.
func writeJournalAt(t *testing.T, commonDir, branch string, entries ...reviewjournal.Entry) {
	t.Helper()
	dir := filepath.Join(commonDir, "devgeta", "review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	j := &reviewjournal.Journal{Branch: branch, Entries: entries}
	path := filepath.Join(dir, reviewjournal.EncodeBranch(branch)+".md")
	if err := os.WriteFile(path, []byte(j.Render()), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

// TestWorktreeFinish_MergeJournalGate covers openBlockingFindings and its
// wiring into worktreeFinishMerge (ADR-0027, §8 Q3 of
// docs/plans/cycles/2026-08-17-finish-work-command.md). Every subtest reaches
// the merge path with a clean worktree, a clean main checkout on the default
// branch — so the journal probe is the only thing left to decide whether the
// merge proceeds.
func TestWorktreeFinish_MergeJournalGate(t *testing.T) {
	t.Run("a non-stale open finding refuses before any rebase or merge call", func(t *testing.T) {
		tm, gitBase, wtPath := newMergeFixture(t)
		commonDir := t.TempDir()
		writeJournalAt(t, commonDir, "add-retry", reviewjournal.Entry{
			ID:   "n1",
			Cite: "somefile.go:10",
			Note: "write is not atomic",
			Blob: "aaa1111",
			Head: "abc123",
		})
		if err := os.WriteFile(
			filepath.Join(wtPath, "somefile.go"),
			[]byte("v1\n"),
			0o644,
		); err != nil {
			t.Fatalf("setup: %v", err)
		}

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			), // ListWorktreesAt
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // IsWorktreeDirty(wtPath) -> clean
			commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree
			commands.ExecCommandResult("main\n", "", nil), // branch --show-current
			commands.ExecCommandResult("", "", nil),       // IsWorktreeDirty(mainWorktree) -> clean
			commands.ExecCommandResult(
				commonDir+"\n", "", nil,
			), // openBlockingFindings -> CommonDirIn(mainWorktree)
			commands.ExecCommandResult(
				"aaa1111\n", "", nil,
			), // HashObjectIn(wtPath, somefile.go) -> matches: fresh
		)

		_, err := tm.WorktreeFinish("add-retry", true, false, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "n1") {
			t.Errorf("expected error to name the finding id, got: %v", err)
		}
		if !strings.Contains(err.Error(), "review-note --settle") {
			t.Errorf("expected error to name the fix, got: %v", err)
		}

		// Refused right after the journal probe: merge-base was never reached.
		if len(gitBase.ExecCommandCalls) != 8 {
			t.Fatalf(
				"expected exactly 8 git calls (refused before merge-base), got %d: %+v",
				len(gitBase.ExecCommandCalls), gitBase.ExecCommandCalls,
			)
		}
	})

	t.Run("a stale open finding does not block", func(t *testing.T) {
		tm, gitBase, wtPath := newMergeFixture(t)
		commonDir := t.TempDir()
		writeJournalAt(t, commonDir, "add-retry", reviewjournal.Entry{
			ID:   "n1",
			Cite: "somefile.go:10",
			Note: "write is not atomic",
			Blob: "aaa1111",
			Head: "abc123",
		})
		if err := os.WriteFile(
			filepath.Join(wtPath, "somefile.go"),
			[]byte("v2\n"),
			0o644,
		); err != nil {
			t.Fatalf("setup: %v", err)
		}

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			), // ListWorktreesAt
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // IsWorktreeDirty(wtPath) -> clean
			commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree
			commands.ExecCommandResult("main\n", "", nil), // branch --show-current
			commands.ExecCommandResult("", "", nil),       // IsWorktreeDirty(mainWorktree) -> clean
			commands.ExecCommandResult(
				commonDir+"\n", "", nil,
			), // openBlockingFindings -> CommonDirIn(mainWorktree)
			commands.ExecCommandResult(
				"zzz9999\n", "", nil,
			), // HashObjectIn(wtPath, somefile.go) -> mismatch: stale
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
			), // dropReviewJournal -> CommonDirIn
		)

		out, err := tm.WorktreeFinish("add-retry", true, false, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Merged add-retry into main") {
			t.Errorf("unexpected confirmation: %q", out)
		}
	})

	t.Run("a settled finding does not block", func(t *testing.T) {
		tm, gitBase, wtPath := newMergeFixture(t)
		commonDir := t.TempDir()
		writeJournalAt(t, commonDir, "add-retry", reviewjournal.Entry{
			ID:         "n1",
			Resolution: reviewjournal.ResolutionFixed,
			Cite:       "somefile.go:10",
			Note:       "write is not atomic",
			Answer:     "made the write atomic",
			Blob:       "aaa1111",
			Head:       "abc123",
		})

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			), // ListWorktreesAt
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // IsWorktreeDirty(wtPath) -> clean
			commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree
			commands.ExecCommandResult("main\n", "", nil), // branch --show-current
			commands.ExecCommandResult("", "", nil),       // IsWorktreeDirty(mainWorktree) -> clean
			commands.ExecCommandResult(
				commonDir+"\n", "", nil,
			), // openBlockingFindings -> CommonDirIn(mainWorktree)
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
			), // dropReviewJournal -> CommonDirIn
		)

		out, err := tm.WorktreeFinish("add-retry", true, false, false, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "Merged add-retry into main") {
			t.Errorf("unexpected confirmation: %q", out)
		}

		// A settled entry is never handed to Verdict at all, so the only
		// extra call over the empty-journal case is the journal's own load.
		if len(gitBase.ExecCommandCalls) != 13 {
			t.Fatalf("expected 13 git calls, got %d: %+v",
				len(gitBase.ExecCommandCalls), gitBase.ExecCommandCalls)
		}
	})

	t.Run("a pathless open finding blocks", func(t *testing.T) {
		tm, gitBase, wtPath := newMergeFixture(t)
		commonDir := t.TempDir()
		writeJournalAt(t, commonDir, "add-retry", reviewjournal.Entry{
			ID:   "n1",
			Note: "should this feature fetch, or stay local-only?",
		})

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
			), // ListWorktreesAt
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // IsWorktreeDirty(wtPath) -> clean
			commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree
			commands.ExecCommandResult("main\n", "", nil), // branch --show-current
			commands.ExecCommandResult("", "", nil),       // IsWorktreeDirty(mainWorktree) -> clean
			commands.ExecCommandResult(
				commonDir+"\n", "", nil,
			), // openBlockingFindings -> CommonDirIn(mainWorktree)
		)

		_, err := tm.WorktreeFinish("add-retry", true, false, false, false)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "n1") {
			t.Errorf("expected error to name the finding id, got: %v", err)
		}

		// A pathless (dateless) entry is never handed to citeBlob, so there is
		// no hash-object call — the probe blocks off the journal load alone.
		if len(gitBase.ExecCommandCalls) != 7 {
			t.Fatalf(
				"expected exactly 7 git calls (no hash-object call for a pathless entry), got %d: %+v",
				len(gitBase.ExecCommandCalls),
				gitBase.ExecCommandCalls,
			)
		}
	})

	// Regression test: this is the exact bug shape the brief calls out.
	// Verdict must be judged against wtPath — the checkout that actually
	// holds the branch's own fix — never against mainWorktree. An entry
	// citing a file the feature branch itself changed has its stamped blob
	// match what's NOW at wtPath (the fix), so it must still block. If the
	// implementation ever judged Verdict against mainWorktree instead, the
	// main checkout's unrelated (pre-fix) copy of the file would almost
	// always compare unequal to the stamped blob and read [STALE], silently
	// letting the finding through.
	t.Run(
		"regression: a finding citing a file the branch changed is judged against wtPath, not the main checkout",
		func(t *testing.T) {
			tm, gitBase, wtPath := newMergeFixture(t)
			commonDir := t.TempDir()
			writeJournalAt(t, commonDir, "add-retry", reviewjournal.Entry{
				ID:   "n2",
				Cite: "fix.go:5",
				Note: "off-by-one in the loop bound",
				Blob: "cafe1234",
				Head: "abc123",
			})
			if err := os.WriteFile(
				filepath.Join(wtPath, "fix.go"),
				[]byte("fixed content\n"),
				0o644,
			); err != nil {
				t.Fatalf("setup: %v", err)
			}

			gitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
				), // ListWorktreesAt
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // IsWorktreeDirty(wtPath) -> clean
				commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn
				commands.ExecCommandResult(
					"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				), // GetMainWorktree
				commands.ExecCommandResult("main\n", "", nil), // branch --show-current
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // IsWorktreeDirty(mainWorktree) -> clean
				commands.ExecCommandResult(
					commonDir+"\n", "", nil,
				), // openBlockingFindings -> CommonDirIn(mainWorktree)
				commands.ExecCommandResult(
					"cafe1234\n", "", nil,
				), // HashObjectIn(wtPath, fix.go) -> matches: still fresh
			)

			_, err := tm.WorktreeFinish("add-retry", true, false, false, false)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "n2") {
				t.Errorf("expected error to name the finding id, got: %v", err)
			}

			calls := gitBase.ExecCommandCalls
			if len(calls) != 8 {
				t.Fatalf("expected exactly 8 git calls, got %d: %+v", len(calls), calls)
			}
			// The load-bearing assertion: the hash-object call's directory must
			// be wtPath, never mainWorktree ("/main") or the journal's own
			// commonDir.
			assertCmd(t, calls[7], "git", "-C", wtPath, "hash-object", "--", "fix.go")
		},
	)
}

// TestWorktreeFinish_MergeIsAncestorUnanswerable covers Part 0's new refusal:
// Git.IsAncestor failing outright (not just answering "not an ancestor", exit
// 1) must refuse the merge before any rebase or merge call — treating "can't
// tell" as "assume diverged" would risk rebasing when the real answer might
// have been "already an ancestor, nothing to do".
func TestWorktreeFinish_MergeIsAncestorUnanswerable(t *testing.T) {
	tm, gitBase, wtPath := newMergeFixture(t)

	gitBase.SetExecCommandResults(
		commands.ExecCommandResult(
			"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
		), // ListWorktreesAt
		commands.ExecCommandResult("", "", nil),              // IsWorktreeDirty(wtPath) -> clean
		commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn
		commands.ExecCommandResult(
			"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
		), // GetMainWorktree
		commands.ExecCommandResult("main\n", "", nil), // branch --show-current
		commands.ExecCommandResult("", "", nil),       // IsWorktreeDirty(mainWorktree) -> clean
		commands.ExecCommandResult(
			"/main/.git\n", "", nil,
		), // openBlockingFindings -> CommonDirIn(mainWorktree) (empty journal)
		commands.ExecCommandResult(
			"", "fatal: Not a valid object name main", exitError(t, 128),
		), // merge-base --is-ancestor -> genuine failure, not "not an ancestor"
	)

	_, err := tm.WorktreeFinish("add-retry", true, false, false, false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cannot determine whether") {
		t.Errorf("expected the new unanswerable-divergence refusal, got: %v", err)
	}

	// Refused right after the probe: no rebase, no merge --ff-only attempted.
	if len(gitBase.ExecCommandCalls) != 8 {
		t.Fatalf(
			"expected exactly 8 git calls (no rebase or merge attempted), got %d: %+v",
			len(gitBase.ExecCommandCalls), gitBase.ExecCommandCalls,
		)
	}
}

// TestWorktreeFinishCheck covers worktree-finish --check's read-only
// readiness report (Part 1-2). Every subtest reaches the same gather order
// worktreeFinishCheck uses: DefaultBranchIn, IsWorktreeDirty(wtPath),
// GetMainWorktree, CurrentBranchIn(mainWorktree), IsWorktreeDirty(mainWorktree),
// the ahead/behind rev-list, IsAncestor, MergeTreeConflicts,
// journalOpenFindings (CommonDirIn + one HashObjectIn per cited open entry),
// then the doc-status sweep (merge-base, changedFiles' diff --numstat,
// untrackedFiles' ls-files). Nothing here short-circuits — every field is
// gathered even once a blocking condition is known, since the report's whole
// value is showing the complete picture; only "ready:" reflects the
// first-blocking-reason order.
func TestWorktreeFinishCheck(t *testing.T) {
	newFixture := newMergeFixture

	t.Run("clean, non-diverged, no findings, no changed markdown -> ready", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)
		commonDir := t.TempDir()

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn(wtPath)
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // IsWorktreeDirty(wtPath) -> clean
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			), // GetMainWorktree(wtPath)
			commands.ExecCommandResult("main\n", "", nil), // CurrentBranchIn(mainWorktree)
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // IsWorktreeDirty(mainWorktree) -> clean
			commands.ExecCommandResult(
				"0\t3\n",
				"",
				nil,
			), // rev-list --left-right --count -> behind 0, ahead 3
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // IsAncestor -> ancestor (rebase not needed)
			commands.ExecCommandResult("deadbeef\n", "", nil), // MergeTreeConflicts -> clean merge
			commands.ExecCommandResult(
				commonDir+"\n", "", nil,
			), // journalOpenFindings -> CommonDirIn(mainWorktree) (empty journal)
			commands.ExecCommandResult("basecommit\n", "", nil), // doc-status sweep: merge-base
			commands.ExecCommandResult("", "", nil),             // changedFiles: diff --numstat
			commands.ExecCommandResult("", "", nil),             // untrackedFiles: ls-files
		)

		report, ready, err := tm.worktreeFinishCheck(wtPath, "add-retry")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ready {
			t.Errorf("expected ready, got report:\n%s", report)
		}
		for _, want := range []string{
			"worktree: " + wtPath,
			"branch: add-retry",
			"default: main",
			"dirty: no",
			"ahead: 3  behind: 0",
			"main-checkout: main (clean)",
			"rebase: not needed",
			"conflicts: none (advisory, does not block)",
			"journal-open: 0",
			"journal-stale-open: 0",
			"ready: yes",
		} {
			if !strings.Contains(report, want) {
				t.Errorf("expected report to contain %q, got:\n%s", want, report)
			}
		}
		if strings.Contains(report, "doc-status:") {
			t.Errorf("expected no doc-status lines, got:\n%s", report)
		}

		if len(gitBase.ExecCommandCalls) != 12 {
			t.Fatalf(
				"expected 12 git calls, got %d: %+v",
				len(gitBase.ExecCommandCalls),
				gitBase.ExecCommandCalls,
			)
		}
	})

	t.Run(
		"dirty wtPath -> not ready, but every field is still gathered and reported",
		func(t *testing.T) {
			tm, gitBase, wtPath := newFixture(t)
			commonDir := t.TempDir()

			gitBase.SetExecCommandResults(
				commands.ExecCommandResult("origin/main\n", "", nil),
				commands.ExecCommandResult(
					"?? scratch.go\n",
					"",
					nil,
				), // IsWorktreeDirty(wtPath) -> dirty
				commands.ExecCommandResult(
					"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				),
				commands.ExecCommandResult("main\n", "", nil),
				commands.ExecCommandResult("", "", nil), // IsWorktreeDirty(mainWorktree) -> clean
				commands.ExecCommandResult("0\t3\n", "", nil),
				commands.ExecCommandResult("", "", nil), // IsAncestor -> ancestor
				commands.ExecCommandResult("deadbeef\n", "", nil),
				commands.ExecCommandResult(commonDir+"\n", "", nil),
				commands.ExecCommandResult("basecommit\n", "", nil),
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult("", "", nil),
			)

			report, ready, err := tm.worktreeFinishCheck(wtPath, "add-retry")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ready {
				t.Fatalf("expected not ready, got:\n%s", report)
			}
			if !strings.Contains(report, "dirty: yes") {
				t.Errorf("expected dirty: yes, got:\n%s", report)
			}
			if !strings.Contains(report, "ready: no — refusing to merge a dirty worktree") {
				t.Errorf("expected the dirty-worktree reason, got:\n%s", report)
			}
			// Every other field is still fully computed and reported — this is a
			// report, not an early exit.
			for _, want := range []string{
				"main-checkout: main (clean)",
				"rebase: not needed",
				"conflicts: none (advisory, does not block)",
				"journal-open: 0",
			} {
				if !strings.Contains(report, want) {
					t.Errorf("expected report to still contain %q, got:\n%s", want, report)
				}
			}
			if len(gitBase.ExecCommandCalls) != 12 {
				t.Fatalf(
					"expected 12 git calls (no short-circuit), got %d: %+v",
					len(gitBase.ExecCommandCalls), gitBase.ExecCommandCalls,
				)
			}
		},
	)

	t.Run("main checkout on a non-default branch -> not ready", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)
		commonDir := t.TempDir()

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult(
				"some-other-branch\n",
				"",
				nil,
			), // CurrentBranchIn -> mismatch
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // IsWorktreeDirty(mainWorktree) -> clean
			commands.ExecCommandResult("0\t3\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("deadbeef\n", "", nil),
			commands.ExecCommandResult(commonDir+"\n", "", nil),
			commands.ExecCommandResult("basecommit\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		report, ready, err := tm.worktreeFinishCheck(wtPath, "add-retry")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ready {
			t.Fatalf("expected not ready, got:\n%s", report)
		}
		if !strings.Contains(report, `is on "some-other-branch", not "main"`) {
			t.Errorf("expected the branch-mismatch reason, got:\n%s", report)
		}
		if len(gitBase.ExecCommandCalls) != 12 {
			t.Fatalf("expected 12 git calls, got %d", len(gitBase.ExecCommandCalls))
		}
	})

	t.Run("main checkout dirty (on the default branch) -> not ready", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)
		commonDir := t.TempDir()

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("main\n", "", nil),
			commands.ExecCommandResult(
				" M file.go\n",
				"",
				nil,
			), // IsWorktreeDirty(mainWorktree) -> dirty
			commands.ExecCommandResult("0\t3\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("deadbeef\n", "", nil),
			commands.ExecCommandResult(commonDir+"\n", "", nil),
			commands.ExecCommandResult("basecommit\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		report, ready, err := tm.worktreeFinishCheck(wtPath, "add-retry")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ready {
			t.Fatalf("expected not ready, got:\n%s", report)
		}
		if !strings.Contains(report, "main-checkout: main (dirty)") {
			t.Errorf("expected main-checkout: main (dirty), got:\n%s", report)
		}
		if !strings.Contains(report, "has uncommitted changes; commit or stash them there first") {
			t.Errorf("expected the main-dirty reason, got:\n%s", report)
		}
	})

	t.Run(
		"both branch mismatch and main dirty -> the branch mismatch surfaces first",
		func(t *testing.T) {
			tm, gitBase, wtPath := newFixture(t)
			commonDir := t.TempDir()

			gitBase.SetExecCommandResults(
				commands.ExecCommandResult("origin/main\n", "", nil),
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult(
					"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				),
				commands.ExecCommandResult("some-other-branch\n", "", nil), // mismatch
				commands.ExecCommandResult(" M file.go\n", "", nil),        // AND dirty
				commands.ExecCommandResult("0\t3\n", "", nil),
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult("deadbeef\n", "", nil),
				commands.ExecCommandResult(commonDir+"\n", "", nil),
				commands.ExecCommandResult("basecommit\n", "", nil),
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult("", "", nil),
			)

			report, ready, err := tm.worktreeFinishCheck(wtPath, "add-retry")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ready {
				t.Fatalf("expected not ready, got:\n%s", report)
			}
			if !strings.Contains(report, `is on "some-other-branch", not "main"`) {
				t.Errorf(
					"expected the branch-mismatch reason to win over the dirty one, got:\n%s",
					report,
				)
			}
			if strings.Contains(report, "ready: no — main checkout") &&
				strings.Contains(report, "has uncommitted changes") {
				t.Errorf("the dirty reason must not surface first, got:\n%s", report)
			}
		},
	)

	t.Run("a blocking (dateless) open journal finding -> not ready, naming it", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)
		commonDir := t.TempDir()
		writeJournalAt(t, commonDir, "add-retry", reviewjournal.Entry{
			ID:   "n1",
			Note: "should this feature fetch, or stay local-only?",
		})

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("main\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("0\t3\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("deadbeef\n", "", nil),
			commands.ExecCommandResult(
				commonDir+"\n",
				"",
				nil,
			), // journalOpenFindings -> CommonDirIn
			// A dateless entry needs no HashObjectIn call (Verdict short-circuits).
			commands.ExecCommandResult("basecommit\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		report, ready, err := tm.worktreeFinishCheck(wtPath, "add-retry")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ready {
			t.Fatalf("expected not ready, got:\n%s", report)
		}
		if !strings.Contains(report, "journal-open: 1 (n1)") {
			t.Errorf("expected journal-open: 1 (n1), got:\n%s", report)
		}
		if !strings.Contains(
			report,
			"ready: no — refusing to merge add-retry: open review-journal finding(s) n1",
		) {
			t.Errorf("expected the reason to name n1, got:\n%s", report)
		}
	})

	t.Run("a stale open finding only -> ready, reported as advisory", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)
		commonDir := t.TempDir()
		writeJournalAt(t, commonDir, "add-retry", reviewjournal.Entry{
			ID:   "n1",
			Cite: "somefile.go:10",
			Note: "write is not atomic",
			Blob: "aaa1111",
			Head: "abc123",
		})
		if err := os.WriteFile(
			filepath.Join(wtPath, "somefile.go"),
			[]byte("v2\n"),
			0o644,
		); err != nil {
			t.Fatalf("setup: %v", err)
		}

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("main\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("0\t3\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("deadbeef\n", "", nil),
			commands.ExecCommandResult(
				commonDir+"\n",
				"",
				nil,
			), // journalOpenFindings -> CommonDirIn
			commands.ExecCommandResult(
				"zzz9999\n",
				"",
				nil,
			), // HashObjectIn(wtPath, somefile.go) -> mismatch: stale
			commands.ExecCommandResult("basecommit\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		report, ready, err := tm.worktreeFinishCheck(wtPath, "add-retry")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ready {
			t.Fatalf("expected ready (a stale finding must not block), got:\n%s", report)
		}
		if !strings.Contains(report, "journal-open: 0") {
			t.Errorf("expected journal-open: 0, got:\n%s", report)
		}
		if !strings.Contains(report, "journal-stale-open: 1 (n1 — advisory, does not block)") {
			t.Errorf("expected the stale-open advisory line, got:\n%s", report)
		}
	})

	t.Run("Git.IsAncestor erroring -> rebase: unknown(...), not ready", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)
		commonDir := t.TempDir()

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("main\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("0\t3\n", "", nil),
			commands.ExecCommandResult(
				"", "fatal: Not a valid object name main", exitError(t, 128),
			), // IsAncestor -> genuine failure
			commands.ExecCommandResult("deadbeef\n", "", nil),
			commands.ExecCommandResult(commonDir+"\n", "", nil),
			commands.ExecCommandResult("basecommit\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		report, ready, err := tm.worktreeFinishCheck(wtPath, "add-retry")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ready {
			t.Fatalf("expected not ready, got:\n%s", report)
		}
		if !strings.Contains(report, "rebase: unknown (") {
			t.Errorf("expected rebase: unknown(...), got:\n%s", report)
		}
		if !strings.Contains(
			report,
			"ready: no — cannot determine whether add-retry needs a rebase against main",
		) {
			t.Errorf("expected the unanswerable-divergence reason, got:\n%s", report)
		}
	})

	// The realistic combination the plan calls out as reachable, not
	// hypothetical: a repo with no LOCAL branch named defaultBranch (only
	// feature branches ever checked out, or DefaultBranchIn's "main" fallback
	// in a repo whose default is actually "trunk"). rev-list and IsAncestor
	// both probe that exact ref, so both fail together — unlike the previous
	// subtest's mock (rev-list succeeds, only IsAncestor fails), which can't
	// really happen since the two commands resolve the same ref. The
	// ahead/behind gather must degrade to "unknown" instead of aborting the
	// whole report, so the designed rebase:/ready: handling (from IsAncestor's
	// own failure) can still render.
	t.Run(
		"rev-list and IsAncestor both fail on the same unresolvable ref -> report still renders",
		func(t *testing.T) {
			tm, gitBase, wtPath := newFixture(t)
			commonDir := t.TempDir()

			gitBase.SetExecCommandResults(
				commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn(wtPath)
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // IsWorktreeDirty(wtPath) -> clean
				commands.ExecCommandResult(
					"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				),
				commands.ExecCommandResult("main\n", "", nil), // CurrentBranchIn(mainWorktree)
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // IsWorktreeDirty(mainWorktree) -> clean
				commands.ExecCommandResult(
					"",
					"fatal: ambiguous argument 'main...HEAD': unknown revision or path not in the working tree.",
					exitError(t, 128),
				), // rev-list --left-right --count -> genuine failure, no local "main"
				commands.ExecCommandResult(
					"", "fatal: Not a valid object name main", exitError(t, 128),
				), // IsAncestor -> the SAME underlying condition
				commands.ExecCommandResult(
					"deadbeef\n",
					"",
					nil,
				), // MergeTreeConflicts -> clean merge
				commands.ExecCommandResult(
					commonDir+"\n",
					"",
					nil,
				), // journalOpenFindings -> CommonDirIn
				commands.ExecCommandResult("basecommit\n", "", nil), // doc-status sweep: merge-base
				commands.ExecCommandResult("", "", nil),             // changedFiles: diff --numstat
				commands.ExecCommandResult("", "", nil),             // untrackedFiles: ls-files
			)

			report, ready, err := tm.worktreeFinishCheck(wtPath, "add-retry")
			if err != nil {
				t.Fatalf(
					"expected the report to still render despite the unanswerable probe, got error: %v",
					err,
				)
			}
			if ready {
				t.Fatalf("expected not ready, got:\n%s", report)
			}
			if !strings.Contains(report, "ahead: unknown  behind: unknown (") {
				t.Errorf("expected the degraded ahead/behind line, got:\n%s", report)
			}
			if !strings.Contains(report, "rebase: unknown (") {
				t.Errorf("expected rebase: unknown(...), got:\n%s", report)
			}
			if !strings.Contains(
				report,
				"ready: no — cannot determine whether add-retry needs a rebase against main",
			) {
				t.Errorf("expected the unanswerable-divergence reason, got:\n%s", report)
			}
		},
	)

	t.Run(
		"Git.MergeTreeConflicts erroring -> conflicts: unknown(...), ready unaffected",
		func(t *testing.T) {
			tm, gitBase, wtPath := newFixture(t)
			commonDir := t.TempDir()

			gitBase.SetExecCommandResults(
				commands.ExecCommandResult("origin/main\n", "", nil),
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult(
					"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				),
				commands.ExecCommandResult("main\n", "", nil),
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult("0\t3\n", "", nil),
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult(
					"", "fatal: not a valid object", exitError(t, 128),
				), // MergeTreeConflicts -> genuine failure
				commands.ExecCommandResult(commonDir+"\n", "", nil),
				commands.ExecCommandResult("basecommit\n", "", nil),
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult("", "", nil),
			)

			report, ready, err := tm.worktreeFinishCheck(wtPath, "add-retry")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !ready {
				t.Fatalf(
					"expected ready (an unknown conflict prediction must not block), got:\n%s",
					report,
				)
			}
			if !strings.Contains(report, "conflicts: unknown (git merge-tree failed:") ||
				!strings.Contains(report, "— advisory, does not block)") {
				t.Errorf("expected the unknown-conflicts advisory line, got:\n%s", report)
			}
		},
	)

	t.Run(
		"Git.MergeTreeConflicts predicting conflicts -> listed, ready unaffected",
		func(t *testing.T) {
			tm, gitBase, wtPath := newFixture(t)
			commonDir := t.TempDir()

			gitBase.SetExecCommandResults(
				commands.ExecCommandResult("origin/main\n", "", nil),
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult(
					"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				),
				commands.ExecCommandResult("main\n", "", nil),
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult("0\t3\n", "", nil),
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult(
					"treeoid1234\nconflict/path.go\n\nCONFLICT (content): Merge conflict in conflict/path.go\n",
					"",
					exitError(t, 1),
				), // MergeTreeConflicts -> predicted conflict
				commands.ExecCommandResult(commonDir+"\n", "", nil),
				commands.ExecCommandResult("basecommit\n", "", nil),
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult("", "", nil),
			)

			report, ready, err := tm.worktreeFinishCheck(wtPath, "add-retry")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !ready {
				t.Fatalf("expected ready (predicted conflicts are advisory only), got:\n%s", report)
			}
			if !strings.Contains(
				report,
				"conflicts: 1 (conflict/path.go — advisory, does not block)",
			) {
				t.Errorf("expected the conflicts listing line, got:\n%s", report)
			}
		},
	)

	t.Run("changed markdown fixtures", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)
		commonDir := t.TempDir()

		if err := os.MkdirAll(filepath.Join(wtPath, "docs"), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(
			filepath.Join(wtPath, "docs", "marked.md"), []byte("**Status:** Draft\n"), 0o644,
		); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(
			filepath.Join(wtPath, "docs", "nomarker.md"),
			[]byte("# Title\n\nJust prose, no marker here.\n"),
			0o644,
		); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(
			filepath.Join(wtPath, "code.go"), []byte("package x\n"), 0o644,
		); err != nil {
			t.Fatalf("setup: %v", err)
		}
		if err := os.WriteFile(
			filepath.Join(wtPath, "docs", "untracked.md"), []byte("**Status:** PROPOSED\n"), 0o644,
		); err != nil {
			t.Fatalf("setup: %v", err)
		}
		// docs/deleted.md is intentionally NOT written: it appears in the
		// numstat output (the branch's history touched it) but no longer
		// exists in the working tree, as if the branch (or an uncommitted
		// edit) deleted it.

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("main\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("0\t3\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("deadbeef\n", "", nil),
			commands.ExecCommandResult(commonDir+"\n", "", nil),
			commands.ExecCommandResult("basecommit\n", "", nil), // doc-status sweep: merge-base
			commands.ExecCommandResult(
				"5\t0\tdocs/marked.md\n0\t3\tdocs/nomarker.md\n2\t1\tcode.go\n0\t4\tdocs/deleted.md\n",
				"",
				nil,
			), // changedFiles: diff --numstat
			commands.ExecCommandResult(
				"docs/untracked.md\x00",
				"",
				nil,
			), // untrackedFiles: ls-files -z
		)

		report, ready, err := tm.worktreeFinishCheck(wtPath, "add-retry")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ready {
			t.Fatalf("expected ready, got:\n%s", report)
		}
		for _, want := range []string{
			"doc-status: docs/marked.md: Draft",
			"doc-status: docs/nomarker.md: no status marker",
			"doc-status: docs/untracked.md: PROPOSED",
		} {
			if !strings.Contains(report, want) {
				t.Errorf("expected report to contain %q, got:\n%s", want, report)
			}
		}
		for _, notWant := range []string{"code.go", "docs/deleted.md"} {
			if strings.Contains(report, "doc-status: "+notWant) {
				t.Errorf("expected no doc-status line for %q, got:\n%s", notWant, report)
			}
		}
	})

	t.Run("--check makes no fetch call", func(t *testing.T) {
		tm, gitBase, wtPath := newFixture(t)
		commonDir := t.TempDir()

		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("origin/main\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult(
				"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
			),
			commands.ExecCommandResult("main\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("0\t3\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("deadbeef\n", "", nil),
			commands.ExecCommandResult(commonDir+"\n", "", nil),
			commands.ExecCommandResult("basecommit\n", "", nil),
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		if _, _, err := tm.worktreeFinishCheck(wtPath, "add-retry"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		for _, call := range gitBase.ExecCommandCalls {
			for _, arg := range call.Args {
				if arg == "fetch" {
					t.Fatalf("--check must never fetch, got a fetch call: %+v", call)
				}
			}
		}
	})
}

// TestWorktreeFinish_Check covers WorktreeFinish's own check=true branch —
// every other test in this file calls tm.worktreeFinishCheck directly, so
// nothing previously exercised the mapping WorktreeFinish itself performs:
// not ready -> ("", &CheckNotReadyError{Report: report}), ready ->
// (report, nil). Reuses the same mocked-sequence shape as
// TestWorktreeFinishCheck's "clean, ready" and "dirty" subtests, with one
// extra leading call: resolveWorktreeTarget's branchForWorktree resolution
// (ListWorktreesAt(wtPath)), which worktreeFinishCheck's own tests skip by
// calling it directly with wtPath/branch already resolved.
func TestWorktreeFinish_Check(t *testing.T) {
	newFixture := newMergeFixture

	t.Run(
		"clean and ready -> returns the report as the string result, nil error",
		func(t *testing.T) {
			tm, gitBase, wtPath := newFixture(t)
			commonDir := t.TempDir()

			gitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
				), // resolveWorktreeTarget -> branchForWorktree -> ListWorktreesAt(wtPath)
				commands.ExecCommandResult("origin/main\n", "", nil), // DefaultBranchIn(wtPath)
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // IsWorktreeDirty(wtPath) -> clean
				commands.ExecCommandResult(
					"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				), // GetMainWorktree(wtPath)
				commands.ExecCommandResult("main\n", "", nil), // CurrentBranchIn(mainWorktree)
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // IsWorktreeDirty(mainWorktree) -> clean
				commands.ExecCommandResult("0\t3\n", "", nil), // rev-list --left-right --count
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // IsAncestor -> ancestor (rebase not needed)
				commands.ExecCommandResult(
					"deadbeef\n",
					"",
					nil,
				), // MergeTreeConflicts -> clean merge
				commands.ExecCommandResult(
					commonDir+"\n", "", nil,
				), // journalOpenFindings -> CommonDirIn(mainWorktree) (empty journal)
				commands.ExecCommandResult("basecommit\n", "", nil), // doc-status sweep: merge-base
				commands.ExecCommandResult("", "", nil),             // changedFiles: diff --numstat
				commands.ExecCommandResult("", "", nil),             // untrackedFiles: ls-files
			)

			out, err := tm.WorktreeFinish("add-retry", false, false, true, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out, "ready: yes") {
				t.Errorf("expected the report to be returned as the string result, got: %q", out)
			}
			if !strings.Contains(out, "worktree: "+wtPath) {
				t.Errorf("expected the full report, got: %q", out)
			}

			if len(gitBase.ExecCommandCalls) != 13 {
				t.Fatalf(
					"expected 13 git calls, got %d: %+v",
					len(gitBase.ExecCommandCalls),
					gitBase.ExecCommandCalls,
				)
			}
		},
	)

	t.Run(
		"dirty wtPath and not ready -> empty string result, *CheckNotReadyError carrying the full report",
		func(t *testing.T) {
			tm, gitBase, wtPath := newFixture(t)
			commonDir := t.TempDir()

			gitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					"worktree "+wtPath+"\nHEAD abc123\nbranch refs/heads/add-retry\n\n", "", nil,
				), // resolveWorktreeTarget -> branchForWorktree -> ListWorktreesAt(wtPath)
				commands.ExecCommandResult("origin/main\n", "", nil),
				commands.ExecCommandResult(
					"?? scratch.go\n",
					"",
					nil,
				), // IsWorktreeDirty(wtPath) -> dirty
				commands.ExecCommandResult(
					"worktree /main\nHEAD def456\nbranch refs/heads/main\n\n", "", nil,
				),
				commands.ExecCommandResult("main\n", "", nil),
				commands.ExecCommandResult("", "", nil), // IsWorktreeDirty(mainWorktree) -> clean
				commands.ExecCommandResult("0\t3\n", "", nil),
				commands.ExecCommandResult("", "", nil), // IsAncestor -> ancestor
				commands.ExecCommandResult("deadbeef\n", "", nil),
				commands.ExecCommandResult(commonDir+"\n", "", nil),
				commands.ExecCommandResult("basecommit\n", "", nil),
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult("", "", nil),
			)

			out, err := tm.WorktreeFinish("add-retry", false, false, true, false)
			if out != "" {
				t.Errorf("expected an empty string result on a not-ready check, got: %q", out)
			}
			if err == nil {
				t.Fatal("expected a non-nil error so the process exits non-zero")
			}
			var notReady *CheckNotReadyError
			if !errors.As(err, &notReady) {
				t.Fatalf("expected a *CheckNotReadyError, got %T: %v", err, err)
			}
			if !strings.Contains(notReady.Report, "ready: no —") {
				t.Errorf("expected the report to say not ready, got:\n%s", notReady.Report)
			}
			if !strings.Contains(notReady.Report, "dirty: yes") {
				t.Errorf("expected the full report, got:\n%s", notReady.Report)
			}

			if len(gitBase.ExecCommandCalls) != 13 {
				t.Fatalf(
					"expected 13 git calls, got %d: %+v",
					len(gitBase.ExecCommandCalls), gitBase.ExecCommandCalls,
				)
			}
		},
	)
}
