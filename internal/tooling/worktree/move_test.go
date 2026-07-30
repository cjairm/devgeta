package worktree

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
)

// newMoveWM builds a WorktreeManager wired to the given mock git/tmux bases,
// capturing every WarnFn call into warnings (nil disables capture, matching
// the WorktreeManager.warn fallback-to-utils.PrintWarning behavior being
// irrelevant to these tests either way).
func newMoveWM(
	mockGitBase, mockTmuxBase *commands.MockBaseCommand,
	warnings *[]string,
) *WorktreeManager {
	return &WorktreeManager{
		Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
		Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
		Base: commands.NewMockBaseCommand(),
		WarnFn: func(msg string) {
			if warnings != nil {
				*warnings = append(*warnings, msg)
			}
		},
	}
}

// exitError builds a real *exec.ExitError by actually running a trivial
// subprocess (sh -c "exit N") - the standard way to construct one in Go,
// since exec.ExitError's fields are unexported. This never touches git or
// tmux; it only synthesizes a realistic non-zero-exit error to inject into
// MockBaseCommand, so isRealWorktreeAt's "git couldn't even run here, fall
// back to a directory check" branch is exercised against the same error
// shape ExecCommand really returns for a failed `-C <path>` invocation.
func exitError(t *testing.T, code int) error {
	t.Helper()
	err := exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run()
	if err == nil {
		t.Fatalf("expected a non-nil error for exit code %d", code)
	}
	return err
}

// findMoveCall locates the single `git worktree move` invocation among
// mockGitBase's recorded calls, failing the test if none (or more than one)
// is found - the move-specific check every "did the git move actually
// happen with the right args" assertion needs.
func findMoveCall(t *testing.T, mockGitBase *commands.MockBaseCommand) commands.CommandParams {
	t.Helper()
	var found []commands.CommandParams
	for _, call := range mockGitBase.ExecCommandCalls {
		for _, a := range call.Args {
			if a == "move" {
				found = append(found, call)
				break
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one 'git worktree move' call, found %d: %+v", len(found), found)
	}
	return found[0]
}

// mkdirAllForTest creates path and registers a t.Cleanup to remove it
// afterward. This matters specifically for a path under
// paths.Paths.Data.Root (the shared worktree location, computed by
// sharedWorktreePath): unlike a path rooted under t.TempDir() (auto-cleaned
// by the testing framework when the subtest ends), paths.Paths.Data.Root is
// the process-wide go-test auto-sandbox described in CLAUDE.md - one
// directory shared by every subtest in this binary, not isolated per
// subtest. Left uncleaned, a directory created here under one subtest's
// repo slug can collide with a LATER subtest whose own repo slug
// (deterministically derived from that subtest's own t.TempDir() call
// count) happens to match, making that later subtest see a leftover
// directory as if it were its own real worktree.
func mkdirAllForTest(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("setup: failed to create %s: %v", path, err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(path); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})
}

// hasMoveCall reports whether mockGitBase recorded any `git worktree move`
// call - used by no-op/refusal tests to assert the move-specific git call
// was never made, without failing the test outright the way findMoveCall
// would.
func hasMoveCall(mockGitBase *commands.MockBaseCommand) bool {
	for _, call := range mockGitBase.ExecCommandCalls {
		for _, a := range call.Args {
			if a == "move" {
				return true
			}
		}
	}
	return false
}

func TestWorktreeMove(t *testing.T) {
	t.Run(
		"moves shared to in-repo, destination matches inRepoWorktreePath exactly",
		func(t *testing.T) {
			cleanup := testutil.SetupIsolatedPaths(t)
			defer cleanup()

			repoRoot := t.TempDir()
			repoSlug := filepath.Base(repoRoot)
			name := "feature-test"

			fromPath := sharedWorktreePath(repoSlug, name)
			mkdirAllForTest(t, fromPath)

			mockGitBase := commands.NewMockBaseCommand()
			mockTmuxBase := commands.NewMockBaseCommand()
			wm := newMoveWM(mockGitBase, mockTmuxBase, nil)

			mockGitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					repoRoot+"\n",
					"",
					nil,
				), // 1 GetRepoRoot
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 2 ListWorktreesAt (worktreeStateIn) - WtExists via real dir
				commands.ExecCommandResult(
					"",
					"not a git repository",
					exitError(t, 128),
				), // 3 ListWorktreesAt(shared candidate) -> falls back to os.Stat
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 4 IsWorktreeDirty - clean
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 5 IsPathIgnored - ignored (exit 0)
				commands.ExecCommandResult(
					"worktree "+repoRoot+"\n",
					"",
					nil,
				), // 6 GetMainWorktree (inside MoveWorktree)
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 7 worktree move
			)
			mockTmuxBase.SetExecCommandResults(
				commands.ExecCommandResult("", "", nil), // 1 WindowSession - no window
				commands.ExecCommandResult("", "", nil), // 2 PanesInWindow - no panes
			)

			moved, err := wm.Move(name, config.WorktreeLocationInRepo, false)
			if err != nil {
				t.Fatalf("Move failed: %v", err)
			}
			if !moved {
				t.Fatal("expected moved=true")
			}

			expectedTo := inRepoWorktreePath(repoRoot, name)
			moveCall := findMoveCall(t, mockGitBase)
			if got := moveCall.Args[len(moveCall.Args)-1]; got != expectedTo {
				t.Errorf("expected destination %q, got %q", expectedTo, got)
			}
			if got := moveCall.Args[len(moveCall.Args)-2]; got != fromPath {
				t.Errorf("expected source %q, got %q", fromPath, got)
			}
		},
	)

	t.Run(
		"moves in-repo to shared, destination matches sharedWorktreePath exactly",
		func(t *testing.T) {
			cleanup := testutil.SetupIsolatedPaths(t)
			defer cleanup()
			setWorktreeLocation(t, config.WorktreeLocationInRepo)

			repoRoot := t.TempDir()
			repoSlug := filepath.Base(repoRoot)
			name := "feature-test"

			fromPath := inRepoWorktreePath(repoRoot, name)
			mkdirAllForTest(t, fromPath)

			mockGitBase := commands.NewMockBaseCommand()
			mockTmuxBase := commands.NewMockBaseCommand()
			wm := newMoveWM(mockGitBase, mockTmuxBase, nil)

			mockGitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					repoRoot+"\n",
					"",
					nil,
				), // 1 GetRepoRoot
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 2 ListWorktreesAt (worktreeStateIn) - WtExists via real dir (in-repo, matches config)
				commands.ExecCommandResult(
					"",
					"not a git repository",
					exitError(t, 128),
				), // 3 ListWorktreesAt(shared candidate) -> falls back, doesn't exist
				commands.ExecCommandResult(
					"",
					"not a git repository",
					exitError(t, 128),
				), // 4 ListWorktreesAt(in-repo candidate) -> falls back, exists
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 5 IsWorktreeDirty - clean
				commands.ExecCommandResult(
					"worktree "+repoRoot+"\n",
					"",
					nil,
				), // 6 GetMainWorktree
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 7 worktree move
			)
			mockTmuxBase.SetExecCommandResults(
				commands.ExecCommandResult("", "", nil), // 1 WindowSession - no window
				commands.ExecCommandResult("", "", nil), // 2 PanesInWindow - no panes
			)

			moved, err := wm.Move(name, config.WorktreeLocationShared, false)
			if err != nil {
				t.Fatalf("Move failed: %v", err)
			}
			if !moved {
				t.Fatal("expected moved=true")
			}

			expectedTo := sharedWorktreePath(repoSlug, name)
			moveCall := findMoveCall(t, mockGitBase)
			if got := moveCall.Args[len(moveCall.Args)-1]; got != expectedTo {
				t.Errorf("expected destination %q, got %q", expectedTo, got)
			}
			if got := moveCall.Args[len(moveCall.Args)-2]; got != fromPath {
				t.Errorf("expected source %q, got %q", fromPath, got)
			}
		},
	)

	// This is the actual migration case the command exists for: config was
	// just changed to in-repo, but the worktree (and a stale tmux window)
	// are still sitting at the shared location. A bare move (no --to) must
	// discover the worktree's REAL current path (via currentWorktreePath's
	// direct disk/git check), not the location worktreePath would compute
	// from the (now-changed) config - which would point at a destination
	// that doesn't exist yet, not the source to move FROM.
	t.Run("bare move with no --to brings a worktree into the configured location, "+
		"even when config changed before the worktree moved", func(t *testing.T) {
		cleanup := testutil.SetupIsolatedPaths(t)
		defer cleanup()
		setWorktreeLocation(t, config.WorktreeLocationInRepo)

		repoRoot := t.TempDir()
		repoSlug := filepath.Base(repoRoot)
		name := "feature-test"
		windowName := GetWindowName(repoSlug, name)

		// The worktree is still physically at the shared path - config says
		// in-repo, but nothing has moved it there yet.
		fromPath := sharedWorktreePath(repoSlug, name)
		mkdirAllForTest(t, fromPath)

		mockGitBase := commands.NewMockBaseCommand()
		mockTmuxBase := commands.NewMockBaseCommand()
		wm := newMoveWM(mockGitBase, mockTmuxBase, nil)

		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(repoRoot+"\n", "", nil), // 1 GetRepoRoot
			// 2 ListWorktreesAt at the CONFIG-computed (in-repo, wrong) path
			// inside worktreeStateIn - errors, no match; combined with
			// os.Stat also failing there (never created), WtExists stays
			// false. Resolution still succeeds via WindowExists below (a
			// stale window left over from before the config change).
			commands.ExecCommandResult("", "not a git repository", exitError(t, 128)),
			commands.ExecCommandResult(
				"",
				"not a git repository",
				exitError(t, 128),
			), // 3 ListWorktreesAt(shared candidate) -> falls back, exists
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // 4 IsWorktreeDirty - clean
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // 5 IsPathIgnored - ignored
			commands.ExecCommandResult(
				"worktree "+repoRoot+"\n",
				"",
				nil,
			), // 6 GetMainWorktree
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // 7 worktree move
		)
		mockTmuxBase.SetExecCommandResults(
			// 1 WindowSession - a stale window still exists, pointing at the
			// old (shared) path.
			commands.ExecCommandResult("stalesession\t"+windowName+"\n", "", nil),
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // 2 PanesInWindow - empty (not under test here)
		)

		moved, err := wm.Move(name, "", false)
		if err != nil {
			t.Fatalf("Move failed: %v", err)
		}
		if !moved {
			t.Fatal("expected moved=true")
		}

		expectedTo := inRepoWorktreePath(repoRoot, name)
		moveCall := findMoveCall(t, mockGitBase)
		if got := moveCall.Args[len(moveCall.Args)-1]; got != expectedTo {
			t.Errorf("expected destination %q, got %q", expectedTo, got)
		}
		if got := moveCall.Args[len(moveCall.Args)-2]; got != fromPath {
			t.Errorf("expected source %q, got %q", fromPath, got)
		}
	})

	t.Run(
		"already at target location is a no-op: plain message, exit 0, no git move call",
		func(t *testing.T) {
			cleanup := testutil.SetupIsolatedPaths(t)
			defer cleanup()

			repoRoot := t.TempDir()
			repoSlug := filepath.Base(repoRoot)
			name := "feature-test"

			fromPath := sharedWorktreePath(repoSlug, name)
			mkdirAllForTest(t, fromPath)

			mockGitBase := commands.NewMockBaseCommand()
			mockTmuxBase := commands.NewMockBaseCommand()
			wm := newMoveWM(mockGitBase, mockTmuxBase, nil)

			mockGitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					repoRoot+"\n",
					"",
					nil,
				), // 1 GetRepoRoot
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 2 ListWorktreesAt (worktreeStateIn)
				commands.ExecCommandResult(
					"",
					"not a git repository",
					exitError(t, 128),
				), // 3 ListWorktreesAt(shared candidate)
			)
			mockTmuxBase.SetExecCommandResults(
				commands.ExecCommandResult("", "", nil), // 1 WindowSession - no window
			)

			moved, err := wm.Move(name, config.WorktreeLocationShared, false)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if moved {
				t.Error("expected moved=false for a no-op")
			}
			if hasMoveCall(mockGitBase) {
				t.Error("expected no 'git worktree move' call for a no-op")
			}
			if got := mockGitBase.GetExecCommandCallCount(); got != 3 {
				t.Errorf("expected exactly 3 git calls (resolution only), got %d", got)
			}
			if got := mockTmuxBase.GetExecCommandCallCount(); got != 1 {
				t.Errorf(
					"expected exactly 1 tmux call (no retargeting attempted), got %d",
					got,
				)
			}
		},
	)

	t.Run("dirty worktree without --force is refused, nothing moved", func(t *testing.T) {
		cleanup := testutil.SetupIsolatedPaths(t)
		defer cleanup()

		repoRoot := t.TempDir()
		repoSlug := filepath.Base(repoRoot)
		name := "feature-test"

		fromPath := sharedWorktreePath(repoSlug, name)
		mkdirAllForTest(t, fromPath)

		mockGitBase := commands.NewMockBaseCommand()
		mockTmuxBase := commands.NewMockBaseCommand()
		wm := newMoveWM(mockGitBase, mockTmuxBase, nil)

		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				repoRoot+"\n",
				"",
				nil,
			), // 1 GetRepoRoot
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // 2 ListWorktreesAt (worktreeStateIn)
			commands.ExecCommandResult(
				"",
				"not a git repository",
				exitError(t, 128),
			), // 3 ListWorktreesAt(shared candidate)
			commands.ExecCommandResult(
				"M file.go\n",
				"",
				nil,
			), // 4 IsWorktreeDirty - dirty
		)
		mockTmuxBase.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil), // 1 WindowSession - no window
		)

		moved, err := wm.Move(name, config.WorktreeLocationInRepo, false)
		if err == nil {
			t.Fatal("expected an error for a dirty worktree without --force")
		}
		if moved {
			t.Error("expected moved=false when refused")
		}
		if !strings.Contains(err.Error(), "has uncommitted changes; use --force to move anyway") {
			t.Errorf("expected an actionable dirty-worktree message, got: %v", err)
		}
		if hasMoveCall(mockGitBase) {
			t.Error("expected no 'git worktree move' call when refused")
		}
	})

	t.Run(
		"IsWorktreeDirty failure is propagated as an error, not treated as clean",
		func(t *testing.T) {
			cleanup := testutil.SetupIsolatedPaths(t)
			defer cleanup()

			repoRoot := t.TempDir()
			repoSlug := filepath.Base(repoRoot)
			name := "feature-test"

			fromPath := sharedWorktreePath(repoSlug, name)
			mkdirAllForTest(t, fromPath)

			mockGitBase := commands.NewMockBaseCommand()
			mockTmuxBase := commands.NewMockBaseCommand()
			wm := newMoveWM(mockGitBase, mockTmuxBase, nil)

			mockGitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					repoRoot+"\n",
					"",
					nil,
				), // 1 GetRepoRoot
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 2 ListWorktreesAt (worktreeStateIn)
				commands.ExecCommandResult(
					"",
					"not a git repository",
					exitError(t, 128),
				), // 3 ListWorktreesAt(shared candidate)
				commands.ExecCommandResult(
					"",
					"fatal: unable to read current working directory",
					exitError(t, 128),
				), // 4 IsWorktreeDirty - transient git failure, not "clean"
			)
			mockTmuxBase.SetExecCommandResults(
				commands.ExecCommandResult("", "", nil), // 1 WindowSession - no window
			)

			moved, err := wm.Move(name, config.WorktreeLocationInRepo, false)
			if err == nil {
				t.Fatal("expected an error when IsWorktreeDirty itself fails, not a silent proceed")
			}
			if moved {
				t.Error("expected moved=false when the dirty check errors")
			}
			if hasMoveCall(mockGitBase) {
				t.Error(
					"expected no 'git worktree move' call when the dirty check errors",
				)
			}
		},
	)

	t.Run(
		"dirty worktree with --force proceeds, skipping the dirty check entirely",
		func(t *testing.T) {
			cleanup := testutil.SetupIsolatedPaths(t)
			defer cleanup()

			repoRoot := t.TempDir()
			repoSlug := filepath.Base(repoRoot)
			name := "feature-test"

			fromPath := sharedWorktreePath(repoSlug, name)
			mkdirAllForTest(t, fromPath)

			mockGitBase := commands.NewMockBaseCommand()
			mockTmuxBase := commands.NewMockBaseCommand()
			wm := newMoveWM(mockGitBase, mockTmuxBase, nil)

			mockGitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					repoRoot+"\n",
					"",
					nil,
				), // 1 GetRepoRoot
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 2 ListWorktreesAt (worktreeStateIn)
				commands.ExecCommandResult(
					"",
					"not a git repository",
					exitError(t, 128),
				), // 3 ListWorktreesAt(shared candidate)
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 4 IsPathIgnored - ignored (dirty check skipped by --force)
				commands.ExecCommandResult(
					"worktree "+repoRoot+"\n",
					"",
					nil,
				), // 5 GetMainWorktree
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 6 worktree move
			)
			mockTmuxBase.SetExecCommandResults(
				commands.ExecCommandResult("", "", nil), // 1 WindowSession - no window
				commands.ExecCommandResult("", "", nil), // 2 PanesInWindow - no panes
			)

			moved, err := wm.Move(name, config.WorktreeLocationInRepo, true)
			if err != nil {
				t.Fatalf("Move failed: %v", err)
			}
			if !moved {
				t.Fatal("expected moved=true")
			}
			// Exactly 6 calls proves IsWorktreeDirty was never invoked at all
			// with --force, not merely that a dirty result was overridden.
			if got := mockGitBase.GetExecCommandCallCount(); got != 6 {
				t.Errorf("expected exactly 6 git calls (no dirty check), got %d", got)
			}
		},
	)

	t.Run(
		"--to in-repo warns when .claude/worktrees is not gitignored, but still moves",
		func(t *testing.T) {
			cleanup := testutil.SetupIsolatedPaths(t)
			defer cleanup()

			repoRoot := t.TempDir()
			repoSlug := filepath.Base(repoRoot)
			name := "feature-test"

			fromPath := sharedWorktreePath(repoSlug, name)
			mkdirAllForTest(t, fromPath)

			mockGitBase := commands.NewMockBaseCommand()
			mockTmuxBase := commands.NewMockBaseCommand()
			var warnings []string
			wm := newMoveWM(mockGitBase, mockTmuxBase, &warnings)

			mockGitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					repoRoot+"\n",
					"",
					nil,
				), // 1 GetRepoRoot
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 2 ListWorktreesAt (worktreeStateIn)
				commands.ExecCommandResult(
					"",
					"not a git repository",
					exitError(t, 128),
				), // 3 ListWorktreesAt(shared candidate)
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 4 IsWorktreeDirty - clean
				commands.ExecCommandResult(
					"",
					"",
					exitError(t, 1),
				), // 5 IsPathIgnored - NOT ignored (exit 1)
				commands.ExecCommandResult(
					"worktree "+repoRoot+"\n",
					"",
					nil,
				), // 6 GetMainWorktree
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 7 worktree move
			)
			mockTmuxBase.SetExecCommandResults(
				commands.ExecCommandResult("", "", nil), // 1 WindowSession - no window
				commands.ExecCommandResult("", "", nil), // 2 PanesInWindow - no panes
			)

			moved, err := wm.Move(name, config.WorktreeLocationInRepo, false)
			if err != nil {
				t.Fatalf("Move failed: %v", err)
			}
			if !moved {
				t.Fatal("expected moved=true (warning must not refuse the move)")
			}
			if len(warnings) != 1 {
				t.Fatalf("expected exactly 1 warning, got %d: %+v", len(warnings), warnings)
			}
			if !strings.Contains(warnings[0], "worktrees") ||
				!strings.Contains(warnings[0], "gitignore") {
				t.Errorf("expected a gitignore-related warning, got: %q", warnings[0])
			}
		},
	)

	t.Run(
		"--to in-repo prints no warning when .claude/worktrees is already gitignored",
		func(t *testing.T) {
			cleanup := testutil.SetupIsolatedPaths(t)
			defer cleanup()

			repoRoot := t.TempDir()
			repoSlug := filepath.Base(repoRoot)
			name := "feature-test"

			fromPath := sharedWorktreePath(repoSlug, name)
			mkdirAllForTest(t, fromPath)

			mockGitBase := commands.NewMockBaseCommand()
			mockTmuxBase := commands.NewMockBaseCommand()
			var warnings []string
			wm := newMoveWM(mockGitBase, mockTmuxBase, &warnings)

			mockGitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					repoRoot+"\n",
					"",
					nil,
				), // 1 GetRepoRoot
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 2 ListWorktreesAt (worktreeStateIn)
				commands.ExecCommandResult(
					"",
					"not a git repository",
					exitError(t, 128),
				), // 3 ListWorktreesAt(shared candidate)
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 4 IsWorktreeDirty - clean
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 5 IsPathIgnored - ignored (exit 0)
				commands.ExecCommandResult(
					"worktree "+repoRoot+"\n",
					"",
					nil,
				), // 6 GetMainWorktree
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 7 worktree move
			)
			mockTmuxBase.SetExecCommandResults(
				commands.ExecCommandResult("", "", nil), // 1 WindowSession - no window
				commands.ExecCommandResult("", "", nil), // 2 PanesInWindow - no panes
			)

			moved, err := wm.Move(name, config.WorktreeLocationInRepo, false)
			if err != nil {
				t.Fatalf("Move failed: %v", err)
			}
			if !moved {
				t.Fatal("expected moved=true")
			}
			if len(warnings) != 0 {
				t.Errorf("expected no warnings, got: %+v", warnings)
			}
		},
	)

	t.Run("all panes idle: cd is sent to every pane at the correct new path", func(t *testing.T) {
		cleanup := testutil.SetupIsolatedPaths(t)
		defer cleanup()

		repoRoot := t.TempDir()
		repoSlug := filepath.Base(repoRoot)
		name := "feature-test"
		windowName := GetWindowName(repoSlug, name)

		fromPath := sharedWorktreePath(repoSlug, name)
		mkdirAllForTest(t, fromPath)

		mockGitBase := commands.NewMockBaseCommand()
		mockTmuxBase := commands.NewMockBaseCommand()
		wm := newMoveWM(mockGitBase, mockTmuxBase, nil)

		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				repoRoot+"\n",
				"",
				nil,
			), // 1 GetRepoRoot
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // 2 ListWorktreesAt (worktreeStateIn)
			commands.ExecCommandResult(
				"",
				"not a git repository",
				exitError(t, 128),
			), // 3 ListWorktreesAt(shared candidate)
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // 4 IsWorktreeDirty - clean
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // 5 IsPathIgnored - ignored
			commands.ExecCommandResult(
				"worktree "+repoRoot+"\n",
				"",
				nil,
			), // 6 GetMainWorktree
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // 7 worktree move
		)
		mockTmuxBase.SetExecCommandResults(
			// 1 WindowSession - the window exists
			commands.ExecCommandResult("mysession\t"+windowName+"\n", "", nil),
			// 2 PanesInWindow - two idle shell panes
			commands.ExecCommandResult(
				windowName+"\t%1\tzsh\n"+windowName+"\t%2\tbash\n", "", nil,
			),
			commands.ExecCommandResult("", "", nil), // 3 SendKeysToPane %1
			commands.ExecCommandResult("", "", nil), // 4 SendKeysToPane %2
		)

		moved, err := wm.Move(name, config.WorktreeLocationInRepo, false)
		if err != nil {
			t.Fatalf("Move failed: %v", err)
		}
		if !moved {
			t.Fatal("expected moved=true")
		}

		toPath := inRepoWorktreePath(repoRoot, name)
		wantCmd := "cd '" + toPath + "'"

		sendKeysCalls := sendKeysToPaneCalls(mockTmuxBase)
		if len(sendKeysCalls) != 2 {
			t.Fatalf(
				"expected 2 send-keys calls (one per pane), got %d: %+v",
				len(sendKeysCalls),
				sendKeysCalls,
			)
		}
		gotPanes := map[string]bool{}
		for _, call := range sendKeysCalls {
			paneID := call.Args[2]
			cmdArg := call.Args[3]
			gotPanes[paneID] = true
			if cmdArg != wantCmd {
				t.Errorf("pane %s: expected cd command %q, got %q", paneID, wantCmd, cmdArg)
			}
		}
		if !gotPanes["%1"] || !gotPanes["%2"] {
			t.Errorf("expected both %%1 and %%2 to receive cd, got %+v", gotPanes)
		}
	})

	t.Run("one busy pane blocks retargeting for the whole window, names the busy pane, "+
		"and still reports the move as successful", func(t *testing.T) {
		cleanup := testutil.SetupIsolatedPaths(t)
		defer cleanup()

		repoRoot := t.TempDir()
		repoSlug := filepath.Base(repoRoot)
		name := "feature-test"
		windowName := GetWindowName(repoSlug, name)

		fromPath := sharedWorktreePath(repoSlug, name)
		mkdirAllForTest(t, fromPath)

		mockGitBase := commands.NewMockBaseCommand()
		mockTmuxBase := commands.NewMockBaseCommand()
		var warnings []string
		wm := newMoveWM(mockGitBase, mockTmuxBase, &warnings)

		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(
				repoRoot+"\n",
				"",
				nil,
			), // 1 GetRepoRoot
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // 2 ListWorktreesAt (worktreeStateIn)
			commands.ExecCommandResult(
				"",
				"not a git repository",
				exitError(t, 128),
			), // 3 ListWorktreesAt(shared candidate)
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // 4 IsWorktreeDirty - clean
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // 5 IsPathIgnored - ignored
			commands.ExecCommandResult(
				"worktree "+repoRoot+"\n",
				"",
				nil,
			), // 6 GetMainWorktree
			commands.ExecCommandResult(
				"",
				"",
				nil,
			), // 7 worktree move
		)
		mockTmuxBase.SetExecCommandResults(
			// 1 WindowSession - the window exists
			commands.ExecCommandResult("mysession\t"+windowName+"\n", "", nil),
			// 2 PanesInWindow - one idle shell, one busy pane (nvim)
			commands.ExecCommandResult(
				windowName+"\t%1\tzsh\n"+windowName+"\t%2\tnvim\n", "", nil,
			),
		)

		moved, err := wm.Move(name, config.WorktreeLocationInRepo, false)
		if err != nil {
			t.Fatalf(
				"Move failed (the git-level move must succeed regardless of tmux state): %v",
				err,
			)
		}
		if !moved {
			t.Fatal("expected moved=true - a busy pane must not fail the move itself")
		}

		if got := len(sendKeysToPaneCalls(mockTmuxBase)); got != 0 {
			t.Errorf(
				"expected 0 send-keys calls when any pane is busy (no partial retarget), got %d",
				got,
			)
		}
		if len(warnings) != 1 {
			t.Fatalf("expected exactly 1 warning, got %d: %+v", len(warnings), warnings)
		}
		if !strings.Contains(warnings[0], "%2") || !strings.Contains(warnings[0], "nvim") {
			t.Errorf("expected the warning to name the busy pane and command, got: %q", warnings[0])
		}
	})

	t.Run(
		"no tmux window exists: move succeeds, retargeting is silently skipped",
		func(t *testing.T) {
			cleanup := testutil.SetupIsolatedPaths(t)
			defer cleanup()

			repoRoot := t.TempDir()
			repoSlug := filepath.Base(repoRoot)
			name := "feature-test"

			fromPath := sharedWorktreePath(repoSlug, name)
			mkdirAllForTest(t, fromPath)

			mockGitBase := commands.NewMockBaseCommand()
			mockTmuxBase := commands.NewMockBaseCommand()
			var warnings []string
			wm := newMoveWM(mockGitBase, mockTmuxBase, &warnings)

			mockGitBase.SetExecCommandResults(
				commands.ExecCommandResult(
					repoRoot+"\n",
					"",
					nil,
				), // 1 GetRepoRoot
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 2 ListWorktreesAt (worktreeStateIn)
				commands.ExecCommandResult(
					"",
					"not a git repository",
					exitError(t, 128),
				), // 3 ListWorktreesAt(shared candidate)
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 4 IsWorktreeDirty - clean
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 5 IsPathIgnored - ignored
				commands.ExecCommandResult(
					"worktree "+repoRoot+"\n",
					"",
					nil,
				), // 6 GetMainWorktree
				commands.ExecCommandResult(
					"",
					"",
					nil,
				), // 7 worktree move
			)
			mockTmuxBase.SetExecCommandResults(
				commands.ExecCommandResult("", "", nil), // 1 WindowSession - no window
				commands.ExecCommandResult("", "", nil), // 2 PanesInWindow - no panes at all
			)

			moved, err := wm.Move(name, config.WorktreeLocationInRepo, false)
			if err != nil {
				t.Fatalf("Move failed: %v", err)
			}
			if !moved {
				t.Fatal("expected moved=true")
			}
			if got := mockTmuxBase.GetExecCommandCallCount(); got != 2 {
				t.Errorf(
					"expected exactly 2 tmux calls (WindowSession + PanesInWindow, nothing sent), got %d",
					got,
				)
			}
			if len(warnings) != 0 {
				t.Errorf(
					"expected no warnings when there is no window to retarget, got: %+v",
					warnings,
				)
			}
		},
	)

	t.Run("worktree not found anywhere returns a plain error", func(t *testing.T) {
		cleanup := testutil.SetupIsolatedPaths(t)
		defer cleanup()

		mockGitBase := commands.NewMockBaseCommand()
		mockTmuxBase := commands.NewMockBaseCommand()
		wm := newMoveWM(mockGitBase, mockTmuxBase, nil)

		mockGitBase.SetExecCommandResult(
			"",
			"fatal: not a git repository",
			errors.New("exit status 128"),
		)
		mockTmuxBase.SetExecCommandResult("", "", nil)

		moved, err := wm.Move("does-not-exist", "", false)
		if err == nil {
			t.Fatal("expected an error for a worktree that doesn't exist anywhere")
		}
		if moved {
			t.Error("expected moved=false")
		}
		if !strings.Contains(err.Error(), "no such worktree") {
			t.Errorf("expected a 'no such worktree' message, got: %v", err)
		}
	})
}

// TestIsRealWorktreeAt exercises isRealWorktreeAt's authoritative branch -
// when w.Git.ListWorktreesAt succeeds, its answer is trusted outright
// (ADR-0010), never falling through to the os.Stat fallback. Every other
// test in this file that reaches isRealWorktreeAt (via currentWorktreePath)
// forces ListWorktreesAt to fail so it falls through to that fallback
// branch instead - this is the only coverage of the "git answered"
// path itself.
func TestIsRealWorktreeAt(t *testing.T) {
	t.Run("path present in git's own worktree list is trusted", func(t *testing.T) {
		mockGitBase := commands.NewMockBaseCommand()
		wm := newMoveWM(mockGitBase, commands.NewMockBaseCommand(), nil)

		// Deliberately a path that does not exist on disk: if isRealWorktreeAt
		// fell through to the os.Stat fallback here, it would wrongly return
		// false. Only trusting git's porcelain answer makes this pass.
		candidate := "/nonexistent/repo-slug/feature-test"
		porcelain := "worktree " + candidate + "\nHEAD abc123\nbranch refs/heads/feature-test\n\n"
		mockGitBase.SetExecCommandResult(porcelain, "", nil)

		if !wm.isRealWorktreeAt(candidate) {
			t.Error("expected isRealWorktreeAt to trust git's worktree list and return true")
		}
	})

	t.Run("git runs fine but candidate path is absent from its list", func(t *testing.T) {
		mockGitBase := commands.NewMockBaseCommand()
		wm := newMoveWM(mockGitBase, commands.NewMockBaseCommand(), nil)

		other := "/some/repo-slug/other-worktree"
		porcelain := "worktree " + other + "\nHEAD abc123\nbranch refs/heads/other\n\n"
		mockGitBase.SetExecCommandResult(porcelain, "", nil)

		if wm.isRealWorktreeAt("/some/repo-slug/feature-test") {
			t.Error(
				"expected isRealWorktreeAt to return false when git ran fine " +
					"but doesn't know this path",
			)
		}
	})

	// Important Finding 2 from the final whole-branch review:
	// isRealWorktreeAt used to compare its input path against git's reported
	// wt.Path with raw string equality. `git worktree list --porcelain`
	// reports symlink-resolved absolute paths, while path is built via plain
	// filepath.Join (sharedWorktreePath/inRepoWorktreePath do no symlink
	// resolution) - so on a system where the data root (or $HOME) is itself
	// a symlink (macOS's /tmp -> /private/tmp being the most common
	// real-world trigger), the two representations differ and a raw ==
	// comparison would wrongly return false for a worktree that genuinely
	// exists. This constructs that exact mismatch: candidate reaches the
	// worktree through a symlinked parent directory, while git's mocked
	// answer reports the fully resolved (symlink-free) path - the shape a
	// real git worktree list --porcelain would report.
	t.Run(
		"git's symlink-resolved answer still matches an unresolved candidate path",
		func(t *testing.T) {
			realParent := t.TempDir()
			resolvedPath := filepath.Join(realParent, "feature-test")
			if err := os.MkdirAll(resolvedPath, 0o755); err != nil {
				t.Fatalf("setup: failed to create %s: %v", resolvedPath, err)
			}

			symlinkParent := filepath.Join(t.TempDir(), "linked-parent")
			if err := os.Symlink(realParent, symlinkParent); err != nil {
				t.Skipf("symlinks not supported in this environment: %v", err)
			}
			candidate := filepath.Join(symlinkParent, "feature-test")

			mockGitBase := commands.NewMockBaseCommand()
			wm := newMoveWM(mockGitBase, commands.NewMockBaseCommand(), nil)

			// git reports the fully symlink-resolved path, not the symlinked one
			// candidate is built through.
			porcelain := "worktree " + resolvedPath +
				"\nHEAD abc123\nbranch refs/heads/feature-test\n\n"
			mockGitBase.SetExecCommandResult(porcelain, "", nil)

			if !wm.isRealWorktreeAt(candidate) {
				t.Error(
					"expected isRealWorktreeAt to canonicalize both sides and match " +
						"despite the symlink, but it returned false",
				)
			}
		},
	)
}

// sendKeysToPaneCalls filters mockTmuxBase's recorded calls down to
// send-keys invocations targeting a specific pane id (Args[1] == "-t" and
// Args[2] starting with "%") - i.e. SendKeysToPane calls, as opposed to
// WindowSession/PanesInWindow's list-* queries.
func sendKeysToPaneCalls(mockTmuxBase *commands.MockBaseCommand) []commands.CommandParams {
	var calls []commands.CommandParams
	for _, call := range mockTmuxBase.ExecCommandCalls {
		if len(call.Args) >= 3 && call.Args[0] == "send-keys" && call.Args[1] == "-t" &&
			strings.HasPrefix(call.Args[2], "%") {
			calls = append(calls, call)
		}
	}
	return calls
}
