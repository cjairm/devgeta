// Tests for Step 6 of the "Repo Discovery Scan + Window Layouts" cycle:
// building a multi-pane tmux window from a Layout, and the failure/rollback
// semantics when a tmux call fails partway through building one.

package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/paths"
)

// twoPaneLayout is a 2-pane Layout literal (no install checkers, so
// EnsureInstalled no-ops), for exercising the multi-pane window-build path
// without touching the real system.
var twoPaneLayout = Layout{
	Name: "two-pane",
	Panes: []Pane{
		{Command: "pane0-cmd"},
		{Command: "pane1-cmd", Split: "vertical"},
	},
}

// tmuxCommandOrder flattens the recorded tmux ExecCommand calls down to just
// their leading verb (e.g. "new-window", "split-window"), so a test can
// assert the exact sequence a multi-pane build issues without also pinning
// every argument.
func tmuxCommandOrder(mockBase *commands.MockBaseCommand) []string {
	var out []string
	for _, c := range mockBase.ExecCommandCalls {
		if len(c.Args) > 0 {
			out = append(out, c.Args[0])
		} else {
			out = append(out, "")
		}
	}
	return out
}

func newLayoutTestWM(mockGitBase, mockTmuxBase *commands.MockBaseCommand) *WorktreeManager {
	return &WorktreeManager{
		Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
		Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
		Base: commands.NewMockBaseCommand(),
	}
}

// TestCreateMultiPaneLayoutCallOrder proves a 2-pane layout drives the mocked
// tmux calls in the exact order buildWindowPanes documents: create the
// window, capture pane 0's id, launch pane 0, split, launch pane 1, then
// reselect pane 0 by the captured id (never by index - see ActivePaneID's
// doc comment for why: devgeta's own tmux.conf sets pane-base-index to 1).
func TestCreateMultiPaneLayoutCallOrder(t *testing.T) {
	repoRoot := t.TempDir()

	mockGitBase := commands.NewMockBaseCommand()
	mockGitBase.SetExecCommandResults(
		commands.ExecCommandResult(repoRoot+"\n", "", nil), // rev-parse --show-toplevel
		commands.ExecCommandResult("", "", nil),            // everything else succeeds/empty
	)
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),      // worktreeState: list-windows (no window)
		commands.ExecCommandResult("", "", nil),      // CreateWindow: new-window
		commands.ExecCommandResult("%67\n", "", nil), // ActivePaneID: display-message
		commands.ExecCommandResult("", "", nil),      // SendKeysToWindow (pane 0)
		commands.ExecCommandResult("", "", nil),      // SplitWindow
		commands.ExecCommandResult("", "", nil),      // SendKeysToWindow (pane 1)
		commands.ExecCommandResult("", "", nil),      // SelectPane
	)

	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	repoSlug := filepath.Base(repoRoot)
	wtPath := filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, "feature-test")
	t.Cleanup(func() {
		if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	if err := wm.Create("feature-test", twoPaneLayout, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantOrder := []string{
		"list-windows", // worktreeState's WindowSession lookup
		"new-window",
		"display-message",
		"send-keys",
		"split-window",
		"send-keys",
		"select-pane",
	}
	gotOrder := tmuxCommandOrder(mockTmuxBase)
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("expected %d tmux calls, got %d: %v", len(wantOrder), len(gotOrder), gotOrder)
	}
	for i, want := range wantOrder {
		if gotOrder[i] != want {
			t.Errorf(
				"call %d: expected %q, got %q (full order: %v)",
				i,
				want,
				gotOrder[i],
				gotOrder,
			)
		}
	}

	// The final select-pane must target pane 0's captured id, not some other
	// pane or a bare index (which would silently select nothing/the wrong
	// pane under devgeta's own pane-base-index=1 tmux.conf).
	last := mockTmuxBase.ExecCommandCalls[len(mockTmuxBase.ExecCommandCalls)-1]
	found := false
	for _, arg := range last.Args {
		if arg == "%67" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected final select-pane to target pane0 id %%67, got %v", last.Args)
	}
}

// TestCreateSinglePaneLayoutSkipsReselect proves a 1-pane layout never issues
// ActivePaneID/SelectPane calls: with only one pane, it's already the active
// one, so no reselect is needed - this also keeps the single-pane call count
// identical to the pre-Layout single-coder behavior.
func TestCreateSinglePaneLayoutSkipsReselect(t *testing.T) {
	repoRoot := t.TempDir()

	mockGitBase := commands.NewMockBaseCommand()
	mockGitBase.SetExecCommandResults(
		commands.ExecCommandResult(repoRoot+"\n", "", nil),
		commands.ExecCommandResult("", "", nil),
	)
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil), // worktreeState: list-windows
		commands.ExecCommandResult("", "", nil), // CreateWindow: new-window
		commands.ExecCommandResult("", "", nil), // SendKeysToWindow
	)

	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	repoSlug := filepath.Base(repoRoot)
	wtPath := filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, "feature-test")
	t.Cleanup(func() {
		if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	if err := wm.Create("feature-test", stubLayout, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantOrder := []string{"list-windows", "new-window", "send-keys"}
	gotOrder := tmuxCommandOrder(mockTmuxBase)
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("expected %d tmux calls (no pane-id/reselect), got %d: %v",
			len(wantOrder), len(gotOrder), gotOrder)
	}
	for i, want := range wantOrder {
		if gotOrder[i] != want {
			t.Errorf("call %d: expected %q, got %q", i, want, gotOrder[i])
		}
	}
}

// TestCreateMultiPaneMidBuildFailureRollsBack proves that when a tmux call
// fails partway through building a multi-pane window (here, the split for
// pane 1), the partially built window is killed and the worktree is rolled
// back - never a window with some panes up alongside a worktree that's still
// there.
func TestCreateMultiPaneMidBuildFailureRollsBack(t *testing.T) {
	repoRoot := t.TempDir()

	mockGitBase := commands.NewMockBaseCommand()
	mockGitBase.SetExecCommandResults(
		commands.ExecCommandResult(repoRoot+"\n", "", nil), // rev-parse --show-toplevel
		// Repeats for everything else, including the rollback's RemoveWorktree:
		// GetMainWorktree parses this stdout for a "worktree <path>" line, so it
		// must look like real `git worktree list --porcelain` output for the
		// rollback's "worktree remove" call to actually be reached.
		commands.ExecCommandResult("worktree "+repoRoot+"\n", "", nil),
	)
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),                // list-windows (state)
		commands.ExecCommandResult("", "", nil),                // new-window
		commands.ExecCommandResult("%1\n", "", nil),            // display-message
		commands.ExecCommandResult("", "", nil),                // send-keys (pane 0)
		commands.ExecCommandResult("", "", errors.New("boom")), // split-window fails
		commands.ExecCommandResult("", "", nil),                // list-windows (kill)
		commands.ExecCommandResult("", "", nil),                // kill-window
	)

	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	repoSlug := filepath.Base(repoRoot)
	wtPath := filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, "feature-test")
	t.Cleanup(func() {
		if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	err := wm.Create("feature-test", twoPaneLayout, true)
	if err == nil {
		t.Fatal("expected an error when a mid-build tmux call fails")
	}

	sawKillWindow := false
	for _, c := range mockTmuxBase.ExecCommandCalls {
		if len(c.Args) > 0 && c.Args[0] == "kill-window" {
			sawKillWindow = true
		}
	}
	if !sawKillWindow {
		t.Errorf("expected kill-window after a mid-build failure, calls: %v",
			tmuxCommandOrder(mockTmuxBase))
	}

	sawWorktreeRemove := false
	for _, c := range mockGitBase.ExecCommandCalls {
		for _, arg := range c.Args {
			if arg == "remove" {
				sawWorktreeRemove = true
			}
		}
	}
	if !sawWorktreeRemove {
		t.Errorf(
			"expected the worktree to be rolled back, git calls: %+v",
			mockGitBase.ExecCommandCalls,
		)
	}
}

// TestCreateAtMultiPaneFailureKillsWindowNotSession proves the repo-session
// (CreateAt) path's mid-build failure kills only the window, never the
// shared repo-slug session - other worktrees' windows may already live there.
func TestCreateAtMultiPaneFailureKillsWindowNotSession(t *testing.T) {
	repoRoot := t.TempDir()

	mockGitBase := commands.NewMockBaseCommand()
	mockGitBase.SetExecCommandResults(
		commands.ExecCommandResult(repoRoot+"\n", "", nil), // rev-parse --show-toplevel
		// Repeats for everything else, including the rollback's RemoveWorktree:
		// GetMainWorktree parses this stdout for a "worktree <path>" line, so it
		// must look like real `git worktree list --porcelain` output for the
		// rollback's "worktree remove" call to actually be reached.
		commands.ExecCommandResult("worktree "+repoRoot+"\n", "", nil),
	)
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),                // list-windows (state)
		commands.ExecCommandResult("", "", nil),                // list-windows (ensure)
		commands.ExecCommandResult("", "", nil),                // has-session
		commands.ExecCommandResult("", "", nil),                // new-window
		commands.ExecCommandResult("%3\n", "", nil),            // display-message
		commands.ExecCommandResult("", "", nil),                // send-keys (pane 0)
		commands.ExecCommandResult("", "", errors.New("boom")), // split-window fails
		commands.ExecCommandResult("", "", nil),                // list-windows (kill)
		commands.ExecCommandResult("", "", nil),                // kill-window
	)

	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	repoSlug := filepath.Base(repoRoot)
	wtPath := filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, "feature-test")
	t.Cleanup(func() {
		if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	err := wm.CreateAt(repoRoot, "feature-test", twoPaneLayout, true)
	if err == nil {
		t.Fatal("expected an error when a mid-build tmux call fails")
	}

	sawKillWindow := false
	sawKillSession := false
	for _, c := range mockTmuxBase.ExecCommandCalls {
		if len(c.Args) == 0 {
			continue
		}
		switch c.Args[0] {
		case "kill-window":
			sawKillWindow = true
		case "kill-session":
			sawKillSession = true
		}
	}
	if !sawKillWindow {
		t.Errorf("expected kill-window after a mid-build failure, calls: %v",
			tmuxCommandOrder(mockTmuxBase))
	}
	if sawKillSession {
		t.Errorf("must never kill the shared repo-slug session, calls: %v",
			tmuxCommandOrder(mockTmuxBase))
	}

	sawWorktreeRemove := false
	for _, c := range mockGitBase.ExecCommandCalls {
		for _, arg := range c.Args {
			if arg == "remove" {
				sawWorktreeRemove = true
			}
		}
	}
	if !sawWorktreeRemove {
		t.Errorf(
			"expected the worktree to be rolled back, git calls: %+v",
			mockGitBase.ExecCommandCalls,
		)
	}
}

// TestCreateValidateLayoutFailsBeforeAnyTmuxCall proves the per-pane install
// check runs (and can fail) before any git or tmux state is touched - the
// common "tool missing" case must fail before the window (or worktree)
// exists.
func TestCreateValidateLayoutFailsBeforeAnyTmuxCall(t *testing.T) {
	failingLayout := Layout{
		Name:         "broken",
		Panes:        []Pane{{Command: "x"}},
		paneCheckers: []func() error{func() error { return errors.New("tool missing") }},
	}

	mockGitBase := commands.NewMockBaseCommand()
	mockTmuxBase := commands.NewMockBaseCommand()
	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	err := wm.Create("feature-test", failingLayout, true)
	if err == nil {
		t.Fatal("expected an error from the failing pane checker")
	}

	testutil.VerifyNoRealCommands(t, mockGitBase)
	testutil.VerifyNoRealCommands(t, mockTmuxBase)
}

// TestRepairExistingWindowOnlyResendsPaneZero proves repairing a window that
// already exists never re-splits/rebuilds the layout's later panes - only
// pane 0's command is relaunched. There is no way to tell from here whether
// an existing window's later panes already match the layout's shape, so
// re-splitting on every repair would risk duplicating panes.
func TestRepairExistingWindowOnlyResendsPaneZero(t *testing.T) {
	repoSlug := "myrepo"
	name := "feat"
	windowName := GetWindowName(repoSlug, name)

	mockGitBase := commands.NewMockBaseCommand()
	mockGitBase.SetExecCommandResult("", "", nil)
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		// repoSlugForWorktree's GetRepoRoot fails (not in a repo), forcing
		// findRepoForWorktree's disk search - irrelevant to tmux, no tmux
		// call yet. First real tmux call is ensureWindow's WindowSession.
		commands.ExecCommandResult(
			"some-session\t"+windowName+"\n", "", nil,
		), // WindowSession: window already exists
		commands.ExecCommandResult("", "", nil), // SendKeysToWindowInSession
	)

	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	wtPath := wm.worktreePath(repoSlug, name)
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	if err := wm.RepairInRepo(repoSlug, name, twoPaneLayout); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantOrder := []string{"list-windows", "send-keys"}
	gotOrder := tmuxCommandOrder(mockTmuxBase)
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf(
			"expected only a WindowSession lookup + send-keys (no split-window), got %v",
			gotOrder,
		)
	}
	for i, want := range wantOrder {
		if gotOrder[i] != want {
			t.Errorf("call %d: expected %q, got %q", i, want, gotOrder[i])
		}
	}

	last := mockTmuxBase.GetLastExecCommandCall()
	if last == nil {
		t.Fatal("expected a send-keys call")
	}
	foundCmd := false
	for _, arg := range last.Args {
		if arg == "pane0-cmd" {
			foundCmd = true
		}
	}
	if !foundCmd {
		t.Errorf("expected pane 0's command sent, got %v", last.Args)
	}
}
