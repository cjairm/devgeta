// Tests for Step 6 of the "Repo Discovery Scan + Window Layouts" cycle:
// building a multi-pane tmux window from a Layout, and the failure/rollback
// semantics when a tmux call fails partway through building one.

package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
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

// TestCreateNeverMovesTheClient is the regression test for the bug that made
// worktree.attach_after_create: false impossible to honor. create() used to
// end with an unconditional WindowSession + SwitchToWindow, so the attached
// client was already sitting in the new window by the time the `dg ws`
// dashboard read the setting and decided to stay put. From the user's side
// the dashboard "closed itself" on every create.
//
// Going to the new window is now the caller's call (FollowWindow), so no
// create path may issue a switch-client or select-window of its own. Both
// entry points are covered: Create (current session) and CreateAt (the
// repo-slug session, which is the path the dashboard takes).
//
// The `dg ws` test for the setting (TestCreateSuccessStaysInDashboardWhen
// AttachAfterCreateFalse) could never have caught this — it mocks createFn,
// so the manager's switch never ran in it. This is the layer that has to
// hold the line.
func TestCreateNeverMovesTheClient(t *testing.T) {
	// A real TMUX value is what the old code required to switch at all, so
	// leaving it unset would let this test pass against the very bug it
	// exists to catch.
	t.Setenv("TMUX", "/private/tmp/tmux-501/default,2221,9")

	// Each case queues its own tmux results because the two paths differ by
	// one call (CreateAt consults has-session for the repo-slug session).
	//
	// The queues matter: the mock repeats its LAST result forever, and the old
	// switch only fired when a WindowSession lookup FOUND the window. So each
	// queue ends on a "<session>\t<window>" line — the format SessionWindows
	// parses — leaving a found window waiting for any lookup the create path
	// still makes after building the window. With the bug present that lookup
	// happens and the switch follows; with it gone the line is never consumed.
	// An empty trailing result would make this test pass against the very bug
	// it exists to catch (it did, before this comment existed).
	cases := []struct {
		name   string
		tmux   func(mock *commands.MockBaseCommand, found string)
		create func(wm *WorktreeManager, repoRoot string) error
	}{
		{
			name: "Create",
			tmux: func(mock *commands.MockBaseCommand, found string) {
				mock.SetExecCommandResults(
					commands.ExecCommandResult("", "", nil),     // list-windows (state)
					commands.ExecCommandResult("", "", nil),     // new-window
					commands.ExecCommandResult("%3\n", "", nil), // display-message (pane 0 id)
					commands.ExecCommandResult("", "", nil),     // send-keys (pane 0)
					commands.ExecCommandResult("", "", nil),     // split-window
					commands.ExecCommandResult("", "", nil),     // send-keys (pane 1)
					commands.ExecCommandResult("", "", nil),     // select-pane
					commands.ExecCommandResult(found, "", nil),  // any further lookup: found
				)
			},
			create: func(wm *WorktreeManager, _ string) error {
				return wm.Create("feature-test", twoPaneLayout, true)
			},
		},
		{
			name: "CreateAt",
			tmux: func(mock *commands.MockBaseCommand, found string) {
				mock.SetExecCommandResults(
					commands.ExecCommandResult("", "", nil),     // list-windows (state)
					commands.ExecCommandResult("", "", nil),     // list-windows (ensureWindow)
					commands.ExecCommandResult("", "", nil),     // has-session
					commands.ExecCommandResult("", "", nil),     // new-window
					commands.ExecCommandResult("%3\n", "", nil), // display-message (pane 0 id)
					commands.ExecCommandResult("", "", nil),     // send-keys (pane 0)
					commands.ExecCommandResult("", "", nil),     // split-window
					commands.ExecCommandResult("", "", nil),     // send-keys (pane 1)
					commands.ExecCommandResult("", "", nil),     // select-pane
					commands.ExecCommandResult(found, "", nil),  // any further lookup: found
				)
			},
			create: func(wm *WorktreeManager, repoRoot string) error {
				return wm.CreateAt(repoRoot, "feature-test", twoPaneLayout, true)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := t.TempDir()

			mockGitBase := commands.NewMockBaseCommand()
			mockGitBase.SetExecCommandResults(
				commands.ExecCommandResult(repoRoot+"\n", "", nil), // rev-parse --show-toplevel
				commands.ExecCommandResult("", "", nil),            // everything else
			)
			mockTmuxBase := commands.NewMockBaseCommand()
			windowName := GetWindowName(filepath.Base(repoRoot), "feature-test")
			tc.tmux(mockTmuxBase, TmuxSessionName(filepath.Base(repoRoot))+"\t"+windowName+"\n")

			wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

			repoSlug := filepath.Base(repoRoot)
			wtPath := filepath.Join(
				paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, "feature-test",
			)
			t.Cleanup(func() {
				if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
					t.Logf("cleanup: %v", err)
				}
			})

			if err := tc.create(wm, repoRoot); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, verb := range tmuxCommandOrder(mockTmuxBase) {
				if verb == "switch-client" || verb == "select-window" {
					t.Errorf(
						"%s issued %q: creating a worktree must not move the client, calls: %v",
						tc.name, verb, tmuxCommandOrder(mockTmuxBase),
					)
				}
			}
		})
	}
}

// TestFollowWindowSwitchesToTheWindowsSession covers the explicit counterpart:
// the step a caller that DOES want to land in the new window opts into, and
// the two cases where it cannot and says so rather than failing silently.
func TestFollowWindowSwitchesToTheWindowsSession(t *testing.T) {
	t.Run("switches to the session the window lives in", func(t *testing.T) {
		t.Setenv("TMUX", "/private/tmp/tmux-501/default,2221,9")

		mockTmuxBase := commands.NewMockBaseCommand()
		mockTmuxBase.SetExecCommandResults(
			// SessionWindows' format is "<session>\t<window>" per line.
			commands.ExecCommandResult("wt-myrepo\twt-myrepo-feat\n", "", nil), // WindowSession
			commands.ExecCommandResult("", "", nil),                            // switch-client
			commands.ExecCommandResult("", "", nil),                            // select-window
		)
		wm := newLayoutTestWM(commands.NewMockBaseCommand(), mockTmuxBase)

		if err := wm.FollowWindow("wt-myrepo-feat"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		order := tmuxCommandOrder(mockTmuxBase)
		if !slices.Contains(order, "switch-client") {
			t.Errorf("expected a switch-client, calls: %v", order)
		}
	})

	t.Run("errors outside tmux without touching tmux", func(t *testing.T) {
		t.Setenv("TMUX", "")

		mockTmuxBase := commands.NewMockBaseCommand()
		wm := newLayoutTestWM(commands.NewMockBaseCommand(), mockTmuxBase)

		if err := wm.FollowWindow("wt-myrepo-feat"); err == nil {
			t.Error("expected an error when there is no tmux client to move")
		}
		if len(mockTmuxBase.ExecCommandCalls) != 0 {
			t.Errorf("expected no tmux calls, got: %v", tmuxCommandOrder(mockTmuxBase))
		}
	})

	t.Run("errors when the window is not there", func(t *testing.T) {
		t.Setenv("TMUX", "/private/tmp/tmux-501/default,2221,9")

		mockTmuxBase := commands.NewMockBaseCommand()
		mockTmuxBase.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil), // WindowSession: no match
		)
		wm := newLayoutTestWM(commands.NewMockBaseCommand(), mockTmuxBase)

		if err := wm.FollowWindow("wt-myrepo-gone"); err == nil {
			t.Error("expected an error when the window does not exist")
		}
		if slices.Contains(tmuxCommandOrder(mockTmuxBase), "switch-client") {
			t.Errorf("must not switch to a window that isn't there, calls: %v",
				tmuxCommandOrder(mockTmuxBase))
		}
	})
}

// TestCreateValidateLayoutFailsBeforeAnyTmuxCall proves the per-pane install
// check runs (and can fail) before any git or tmux state is touched - the
// common "tool missing" case must fail before the window (or worktree)
// exists.
func TestCreateValidateLayoutFailsBeforeAnyTmuxCall(t *testing.T) {
	failingLayout := Layout{
		Name: "broken",
		Panes: []Pane{
			{Command: "x", check: func() error { return errors.New("tool missing") }},
		},
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

	wtPath, err := wm.worktreePath(repoSlug, name)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
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

// callsContain reports whether any recorded ExecCommand call's Args contains
// arg — used below to look for a specific tmux target/pane id anywhere in
// the recorded calls, without pinning exact argument positions.
func callsContain(calls []commands.CommandParams, verb, arg string) bool {
	for _, c := range calls {
		if len(c.Args) == 0 || c.Args[0] != verb {
			continue
		}
		for _, a := range c.Args {
			if a == arg {
				return true
			}
		}
	}
	return false
}

// sendKeysTarget returns the "-t" target argument of the recorded send-keys
// call whose keys argument equals wantKeys, and whether such a call was
// found. Args are ["send-keys", "-t", target, keys, "Enter"] for both
// SendKeysToPane and SendKeysToWindowInSession, so this pins down exactly
// what the call targeted - a pane id (e.g. "%2") vs. a "session:window"
// string - which callsContain's substring-anywhere check cannot distinguish.
func sendKeysTarget(calls []commands.CommandParams, wantKeys string) (string, bool) {
	for _, c := range calls {
		if len(c.Args) < 4 || c.Args[0] != "send-keys" || c.Args[1] != "-t" {
			continue
		}
		if c.Args[3] == wantKeys {
			return c.Args[2], true
		}
	}
	return "", false
}

// TestLaunchReviewNoLiveWindowUsesEnsureWindowCreatePath proves that, when
// the worktree's window doesn't exist yet, LaunchReviewInRepo drives the same
// create-if-missing path ensureWindow's own no-window branch uses
// (createWindowWithLayout), with a one-pane layout whose only pane's command
// is the review command - never a split (there's nothing to split yet), and
// never a second WindowSession lookup (LaunchReviewInRepo's own check already
// established the window is missing, so it calls createWindowWithLayout
// directly instead of routing back through ensureWindow).
func TestLaunchReviewNoLiveWindowUsesEnsureWindowCreatePath(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name == "oc" })

	repoSlug := "myrepo"
	name := "feat"

	mockGitBase := commands.NewMockBaseCommand()
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil), // WindowSession (LaunchReviewInRepo's own check)
		commands.ExecCommandResult("", "", nil), // HasSession -> true (nil err)
		commands.ExecCommandResult("", "", nil), // CreateWindowInSession: new-window
		commands.ExecCommandResult("", "", nil), // SendKeysToWindowInSession (the one review pane)
	)

	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	if err := wm.LaunchReviewInRepo(repoSlug, name, "code"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantOrder := []string{"list-windows", "has-session", "new-window", "send-keys"}
	gotOrder := tmuxCommandOrder(mockTmuxBase)
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("expected %v, got %v", wantOrder, gotOrder)
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

	reviewCmd, err := ReviewCommand("code")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if !callsContain(mockTmuxBase.ExecCommandCalls, "send-keys", reviewCmd) {
		t.Errorf("expected send-keys to carry the review command %q, calls: %+v",
			reviewCmd, mockTmuxBase.ExecCommandCalls)
	}
}

// TestLaunchReviewLiveWindowSplitsNewPane proves that, when the worktree's
// window already exists, LaunchReviewInRepo never sends the review command
// into the window's existing (coder) pane - it splits a new pane and sends
// the command there instead, then restores focus to the original pane.
func TestLaunchReviewLiveWindowSplitsNewPane(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name == "oc" })

	repoSlug := "myrepo"
	name := "feat"
	windowName := GetWindowName(repoSlug, name)
	session := "some-session"

	mockGitBase := commands.NewMockBaseCommand()
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		commands.ExecCommandResult(session+"\t"+windowName+"\n", "", nil), // WindowSession: exists
		commands.ExecCommandResult(
			"%1\n",
			"",
			nil,
		), // ActivePaneID (coder pane, pre-split)
		commands.ExecCommandResult("", "", nil), // SplitWindow
		commands.ExecCommandResult(
			"%2\n",
			"",
			nil,
		), // ActivePaneID (new pane, post-split)
		commands.ExecCommandResult(
			"",
			"",
			nil,
		), // SendKeysToPane (review cmd)
		commands.ExecCommandResult(
			"",
			"",
			nil,
		), // SelectPane (restore focus)
	)

	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	if err := wm.LaunchReviewInRepo(repoSlug, name, "code"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantOrder := []string{
		"list-windows",
		"display-message",
		"split-window",
		"display-message",
		"send-keys",
		"select-pane",
	}
	gotOrder := tmuxCommandOrder(mockTmuxBase)
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("expected %v, got %v", wantOrder, gotOrder)
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

	reviewCmd, err := ReviewCommand("code")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// The review command's send-keys must target the new pane id (%2)
	// directly - never "session:window", which tmux would resolve to
	// whatever pane happens to be active at send time, possibly the coder's
	// pane if focus shifted in the gap since the split.
	target, found := sendKeysTarget(mockTmuxBase.ExecCommandCalls, reviewCmd)
	if !found {
		t.Fatalf("expected a send-keys call carrying the review command %q, calls: %+v",
			reviewCmd, mockTmuxBase.ExecCommandCalls)
	}
	if target != "%2" {
		t.Errorf("expected send-keys to target the new pane %%2 directly, got target %q "+
			"(calls: %+v)", target, mockTmuxBase.ExecCommandCalls)
	}

	// Focus must be restored to the coder's pane (%1), captured before the
	// split - not left on the new review pane (%2).
	if !callsContain(mockTmuxBase.ExecCommandCalls, "select-pane", "%1") {
		t.Errorf("expected select-pane to restore focus to coder pane %%1, calls: %+v",
			mockTmuxBase.ExecCommandCalls)
	}
}

// TestLaunchReviewLiveWindowFailureAfterSplitKillsOnlyNewPane proves the
// rollback semantics that make this launch safe: when sending the review
// command fails after a successful split, only the pane that was just split
// off is killed - never the window (which holds the user's existing coder
// pane) and never the worktree (R never creates one, so there's none to roll
// back).
func TestLaunchReviewLiveWindowFailureAfterSplitKillsOnlyNewPane(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name == "oc" })

	repoSlug := "myrepo"
	name := "feat"
	windowName := GetWindowName(repoSlug, name)
	session := "some-session"

	mockGitBase := commands.NewMockBaseCommand()
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		commands.ExecCommandResult(session+"\t"+windowName+"\n", "", nil), // WindowSession: exists
		commands.ExecCommandResult(
			"%1\n",
			"",
			nil,
		), // ActivePaneID (coder pane, pre-split)
		commands.ExecCommandResult("", "", nil), // SplitWindow succeeds
		commands.ExecCommandResult(
			"%2\n",
			"",
			nil,
		), // ActivePaneID (new pane, post-split)
		commands.ExecCommandResult(
			"",
			"",
			errors.New("boom"),
		), // SendKeysToPane fails
		commands.ExecCommandResult("", "", nil), // KillPane rollback
		commands.ExecCommandResult(
			"",
			"",
			nil,
		), // SelectPane (best-effort restore)
	)

	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	err := wm.LaunchReviewInRepo(repoSlug, name, "code")
	if err == nil {
		t.Fatal("expected an error when send-keys fails after a successful split")
	}

	reviewCmd, cmdErr := ReviewCommand("code")
	if cmdErr != nil {
		t.Fatalf("setup: %v", cmdErr)
	}

	// Even on the failing path, the attempted send-keys must have been
	// pane-targeted (%2), not "session:window" - proving the fix applies
	// before the failure, not just in the (already-passing) success case.
	target, found := sendKeysTarget(mockTmuxBase.ExecCommandCalls, reviewCmd)
	if !found {
		t.Fatalf("expected a send-keys call carrying the review command %q, calls: %+v",
			reviewCmd, mockTmuxBase.ExecCommandCalls)
	}
	if target != "%2" {
		t.Errorf("expected the failing send-keys to target the new pane %%2 directly, got "+
			"target %q (calls: %+v)", target, mockTmuxBase.ExecCommandCalls)
	}

	if !callsContain(mockTmuxBase.ExecCommandCalls, "kill-pane", "%2") {
		t.Errorf("expected kill-pane targeting the new pane %%2, calls: %+v",
			mockTmuxBase.ExecCommandCalls)
	}

	// The coder's original pane (%1) must never be killed - only the new
	// pane (%2) this call added should be rolled back.
	if callsContain(mockTmuxBase.ExecCommandCalls, "kill-pane", "%1") {
		t.Errorf("must never kill-pane the coder's original pane %%1, calls: %+v",
			mockTmuxBase.ExecCommandCalls)
	}

	// Must never touch the window or the worktree - only the pane this call
	// added.
	for _, c := range mockTmuxBase.ExecCommandCalls {
		if len(c.Args) > 0 && c.Args[0] == "kill-window" {
			t.Errorf("must never kill-window, calls: %+v", mockTmuxBase.ExecCommandCalls)
		}
	}
	for _, c := range mockGitBase.ExecCommandCalls {
		for _, arg := range c.Args {
			if arg == "remove" {
				t.Errorf(
					"must never remove the worktree, git calls: %+v",
					mockGitBase.ExecCommandCalls,
				)
			}
		}
	}
}

// TestLaunchReviewUnknownKeyFailsBeforeAnyTmuxCall proves an unknown reviewer
// key's error from ReviewCommand propagates before any git or tmux state is
// touched.
func TestLaunchReviewUnknownKeyFailsBeforeAnyTmuxCall(t *testing.T) {
	mockGitBase := commands.NewMockBaseCommand()
	mockTmuxBase := commands.NewMockBaseCommand()
	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	err := wm.LaunchReviewInRepo("myrepo", "feat", "not-a-real-reviewer")
	if err == nil {
		t.Fatal("expected an error for an unknown reviewer key")
	}

	testutil.VerifyNoRealCommands(t, mockGitBase)
	testutil.VerifyNoRealCommands(t, mockTmuxBase)
}

// TestLaunchReviewFailsBeforeAnyTmuxCallWhenOpenCodeMissing proves
// LaunchReviewInRepo checks that "oc" resolves before touching tmux at all -
// mirroring TestCreateValidateLayoutFailsBeforeAnyTmuxCall's shape for the
// analogous check on the create path. A user whose default layout is
// claude/claude-nvim has never needed oc, so pressing R without this
// pre-flight check would build a window/pane whose command prints "oc:
// command not found" while the dashboard still reports "review started",
// with no error surfaced anywhere.
func TestLaunchReviewFailsBeforeAnyTmuxCallWhenOpenCodeMissing(t *testing.T) {
	setShellCommandExistsFn(t, func(string) bool { return false })

	mockGitBase := commands.NewMockBaseCommand()
	mockTmuxBase := commands.NewMockBaseCommand()
	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	err := wm.LaunchReviewInRepo("myrepo", "feat", "code")
	if err == nil {
		t.Fatal("expected an error when oc is not installed")
	}

	testutil.VerifyNoRealCommands(t, mockGitBase)
	testutil.VerifyNoRealCommands(t, mockTmuxBase)
}

// TestLaunchReviewInRepoUsesRealLocationNotConfigured proves LaunchReviewInRepo
// resolves the worktree's real on-disk path via realWorktreePathOrConfigured
// (git-verified), not the config-derived worktreePath - the same bug class
// 472dbaf closed for removeByRepo/Repair/RepairInRepo. worktree.location is
// left at "shared" (the default) while the worktree is PHYSICALLY REAL at the
// in-repo shape instead - exactly what `dg wt move --to in-repo` produces if
// the global default isn't also changed (docs/migrations/v1-to-v2.md
// recommends exactly this flow). Before this fix, LaunchReviewInRepo resolved
// wtPath via the config-derived worktreePath, which would have computed the
// never-created shared path here - launching the reviewer agent's window
// rooted at a directory that doesn't hold the worktree at all.
func TestLaunchReviewInRepoUsesRealLocationNotConfigured(t *testing.T) {
	cleanupPaths := testutil.SetupIsolatedPaths(t)
	defer cleanupPaths()
	setShellCommandExistsFn(t, func(name string) bool { return name == "oc" })
	t.Chdir(t.TempDir()) // cwd is NOT the owning repo, forcing resolution via recent-repos

	repoRoot := t.TempDir()
	repoSlug := filepath.Base(repoRoot)
	name := "feature-review"
	setWorktreeLocationAndRecentRepos(t, config.WorktreeLocationShared, []config.RecentRepo{
		{Path: repoRoot, LastUsed: time.Now()},
	})

	wtPath := inRepoWorktreePath(repoRoot, name)
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	sharedPath := sharedWorktreePath(repoSlug, name)

	notInRepo := commands.ExecCommandResult("", "fatal: not a git repository", os.ErrNotExist)
	mockGitBase := commands.NewMockBaseCommand()
	mockGitBase.SetExecCommandResults(
		notInRepo, // 1: cursorRepoRoot's cwdRepoRoot
		// 2+: the recent-repos anchor - truthful listing (reports the
		// worktree only at the in-repo path), repeated after for
		// currentWorktreePath's shared and in-repo candidate checks.
		commands.ExecCommandResult(
			worktreePorcelain(repoRoot, [2]string{wtPath, "feature-review-branch"}), "", nil,
		),
	)

	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil), // WindowSession: no live window
		commands.ExecCommandResult("", "", nil), // HasSession -> true
		commands.ExecCommandResult("", "", nil), // CreateWindowInSession: new-window
		commands.ExecCommandResult("", "", nil), // SendKeysToWindowInSession
	)
	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	if err := wm.LaunchReviewInRepo(repoSlug, name, "code"); err != nil {
		t.Fatalf(
			"LaunchReviewInRepo failed to find the real in-repo worktree "+
				"(would have targeted the never-created shared path pre-fix): %v",
			err,
		)
	}

	if !callsContain(mockTmuxBase.ExecCommandCalls, "new-window", wtPath) {
		t.Errorf(
			"expected new-window to be rooted at the real in-repo path %q, calls: %+v",
			wtPath,
			mockTmuxBase.ExecCommandCalls,
		)
	}
	if callsContain(mockTmuxBase.ExecCommandCalls, "new-window", sharedPath) {
		t.Error(
			"new-window was rooted at the never-created shared path - " +
				"resolved via configured location, not verified reality",
		)
	}
}
