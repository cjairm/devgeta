// Tests for Step 6 of the "Repo Discovery Scan + Window Layouts" cycle:
// building a multi-pane tmux window from a Layout, and the failure/rollback
// semantics when a tmux call fails partway through building one.

package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// pinPaneShell makes paneShell's answer deterministic for a test and returns
// it. The shell is interpolated into every created pane's command, so a test
// asserting an exact command must not inherit whichever $SHELL the machine
// running it happens to have.
//
// An empty $SHELL is not a usable candidate, and the mocked tmux answer for
// "show-options -gv default-shell" is never an absolute path either, so
// resolveShell lands on its posixShell floor - which POSIX guarantees exists.
func pinPaneShell(t *testing.T) string {
	t.Helper()
	t.Setenv("SHELL", "")
	return posixShell
}

// paneCommandArg returns the pane shell-command argument carried by the first
// recorded tmux call whose verb is verb, and whether it carried one at all.
//
// All four pane-creating wrappers end their fixed argument list with
// "-c <workdir>" and append the pane's command after it (see
// tmux.appendPaneCommand), so anything past that pair is the command tmux will
// exec as the new pane's process. A false result means the call created a pane
// with NO command - the shell layout's shape, where tmux just starts the pane's
// shell. It is also what every pane-creating call looked like before ADR-0020,
// so "false" is the assertion that catches a command silently going missing.
func paneCommandArg(calls []commands.CommandParams, verb string) (string, bool) {
	for _, c := range calls {
		if len(c.Args) == 0 || c.Args[0] != verb {
			continue
		}
		return paneCommandOf(c)
	}
	return "", false
}

// paneCommandOf is paneCommandArg for one already-selected call - see there for
// why the argument after "-c <workdir>" is the pane's command.
func paneCommandOf(call commands.CommandParams) (string, bool) {
	for i := 0; i < len(call.Args)-1; i++ {
		if call.Args[i] == "-c" && i+2 < len(call.Args) {
			return call.Args[i+2], true
		}
	}
	return "", false
}

// paneCreatingCalls returns the recorded tmux calls that bring a pane into
// existence, in call order - the four verbs ADR-0020 makes responsible for
// carrying their pane's command. A layout's panes are built strictly in order,
// so the Nth of these calls is the call that created the Nth pane.
func paneCreatingCalls(calls []commands.CommandParams) []commands.CommandParams {
	var out []commands.CommandParams
	for _, c := range calls {
		if len(c.Args) == 0 {
			continue
		}
		switch c.Args[0] {
		case "new-window", "new-session", "split-window":
			out = append(out, c)
		}
	}
	return out
}

// assertNoSendKeys fails if any recorded tmux call is a send-keys. This is the
// assertion that would fail if a pane's command quietly went back to being
// TYPED into the pane instead of exec'd at its creation - the 1024-byte pty
// input queue that silently swallowed a long --prompt, ADR-0020.
func assertNoSendKeys(t *testing.T, mockBase *commands.MockBaseCommand) {
	t.Helper()
	if slices.Contains(tmuxCommandOrder(mockBase), "send-keys") {
		t.Errorf(
			"a create path must type nothing into a pane - every pane's command is "+
				"exec'd at creation (ADR-0020), calls: %+v",
			mockBase.ExecCommandCalls,
		)
	}
}

func newLayoutTestWM(mockGitBase, mockTmuxBase *commands.MockBaseCommand) *WorktreeManager {
	return &WorktreeManager{
		Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
		Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
		Base: commands.NewMockBaseCommand(),
	}
}

// TestCreateMultiPaneLayoutCallOrder proves a 2-pane layout drives the mocked
// tmux calls in the exact order buildWindowPanes documents: resolve the pane
// shell, create the window with pane 0's command as its process, capture pane
// 0's id, split pane 1 off with ITS command, then reselect pane 0 by the
// captured id (never by index - see ActivePaneID's doc comment for why:
// devgeta's own tmux.conf sets pane-base-index to 1).
//
// Two calls that used to be here are gone, and both are ADR-0020's doing: the
// two send-keys that typed pane 0's and pane 1's commands into their panes. Each
// command now rides the call that CREATES its pane, so there is no second step
// and no pty in the path. One call is new: "show-options -gv default-shell",
// paneShell's second shell candidate.
func TestCreateMultiPaneLayoutCallOrder(t *testing.T) {
	shell := pinPaneShell(t)
	repoRoot := t.TempDir()

	mockGitBase := commands.NewMockBaseCommand()
	mockGitBase.SetExecCommandResults(
		commands.ExecCommandResult(repoRoot+"\n", "", nil), // rev-parse --show-toplevel
		commands.ExecCommandResult("", "", nil),            // everything else succeeds/empty
	)
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),      // worktreeState: list-windows (no window)
		commands.ExecCommandResult("", "", nil),      // paneShell: show-options (no answer)
		commands.ExecCommandResult("", "", nil),      // CreateWindow: new-window (pane 0 + its cmd)
		commands.ExecCommandResult("%67\n", "", nil), // ActivePaneID: display-message
		commands.ExecCommandResult("", "", nil),      // SplitWindow (pane 1 + its cmd)
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
		"show-options", // paneShell's tmux default-shell candidate
		"new-window",
		"display-message",
		"split-window",
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

	// Each pane's command must ride the call that CREATED that pane - pane 0's
	// on new-window, pane 1's on the split - and go to the right one of the two.
	// Building panes strictly in order is what makes the split's command land in
	// the pane just created (split-window splits the active pane and makes the
	// new one active), so a swap here would be a real misplacement, not a
	// cosmetic one.
	wantPane0 := interactivePaneCommand("pane0-cmd", shell)
	if got, ok := paneCommandArg(mockTmuxBase.ExecCommandCalls, "new-window"); !ok ||
		got != wantPane0 {
		t.Errorf("new-window carried %q (present=%v), want pane 0's command %q",
			got, ok, wantPane0)
	}
	wantPane1 := interactivePaneCommand("pane1-cmd", shell)
	if got, ok := paneCommandArg(mockTmuxBase.ExecCommandCalls, "split-window"); !ok ||
		got != wantPane1 {
		t.Errorf("split-window carried %q (present=%v), want pane 1's command %q",
			got, ok, wantPane1)
	}

	assertNoSendKeys(t, mockTmuxBase)
}

// TestCreateSinglePaneLayoutSkipsReselect proves a 1-pane layout never issues
// ActivePaneID/SelectPane calls: with only one pane, it's already the active
// one, so no reselect is needed - this also keeps the single-pane call count
// identical to the pre-Layout single-coder behavior.
func TestCreateSinglePaneLayoutSkipsReselect(t *testing.T) {
	shell := pinPaneShell(t)
	repoRoot := t.TempDir()

	mockGitBase := commands.NewMockBaseCommand()
	mockGitBase.SetExecCommandResults(
		commands.ExecCommandResult(repoRoot+"\n", "", nil),
		commands.ExecCommandResult("", "", nil),
	)
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil), // worktreeState: list-windows
		commands.ExecCommandResult("", "", nil), // paneShell: show-options (no answer)
		commands.ExecCommandResult("", "", nil), // CreateWindow: new-window (pane 0 + its cmd)
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

	// The send-keys that used to follow new-window is gone: pane 0's command is
	// now new-window's own trailing argument (asserted below), so a one-pane
	// layout takes exactly one tmux call to build.
	wantOrder := []string{"list-windows", "show-options", "new-window"}
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

	want := interactivePaneCommand("stub-cmd", shell)
	if got, ok := paneCommandArg(mockTmuxBase.ExecCommandCalls, "new-window"); !ok ||
		got != want {
		t.Errorf("new-window carried %q (present=%v), want %q", got, ok, want)
	}
	assertNoSendKeys(t, mockTmuxBase)
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
		commands.ExecCommandResult("", "", nil),                // show-options (paneShell)
		commands.ExecCommandResult("", "", nil),                // new-window
		commands.ExecCommandResult("%1\n", "", nil),            // display-message
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
	// Pin WHICH call failed. The mocked results are consumed in call order, so
	// an off-by-one in this queue would fail the build somewhere else entirely
	// (the pane-0 id query, say) and the rollback assertions below would still
	// pass - for a reason this test is not about.
	if !strings.Contains(err.Error(), "failed to split window") {
		t.Fatalf("expected the SPLIT to be what failed, got: %v", err)
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
		commands.ExecCommandResult("", "", nil),                // show-options (paneShell)
		commands.ExecCommandResult("", "", nil),                // has-session
		commands.ExecCommandResult("", "", nil),                // new-window
		commands.ExecCommandResult("%3\n", "", nil),            // display-message
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
	// Same reason as the Create-path test above: pin which call failed, so an
	// off-by-one in the queue cannot make the rollback assertions pass for a
	// failure this test is not about.
	if !strings.Contains(err.Error(), "failed to split window") {
		t.Fatalf("expected the SPLIT to be what failed, got: %v", err)
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
					commands.ExecCommandResult("", "", nil),     // show-options (paneShell)
					commands.ExecCommandResult("", "", nil),     // new-window (pane 0 + its cmd)
					commands.ExecCommandResult("%3\n", "", nil), // display-message (pane 0 id)
					commands.ExecCommandResult("", "", nil),     // split-window (pane 1 + its cmd)
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
					commands.ExecCommandResult("", "", nil),     // show-options (paneShell)
					commands.ExecCommandResult("", "", nil),     // has-session
					commands.ExecCommandResult("", "", nil),     // new-window (pane 0 + its cmd)
					commands.ExecCommandResult("%3\n", "", nil), // display-message (pane 0 id)
					commands.ExecCommandResult("", "", nil),     // split-window (pane 1 + its cmd)
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

// TestShellLayoutTypesNothingIntoThePane covers the "shell" layout end to end
// at the tmux level: you get a window and the shell tmux already started in
// the worktree directory, and devgeta gives it no command at all.
//
// "No command" now means new-window is issued with NO trailing shell-command
// argument - the exact argument list this call had before commands were passed
// at creation. That is what leaves the pane with the shell tmux would have
// started anyway; a recipe wrapped around an empty command would instead make
// the pane run an empty statement first. The old failure mode this also still
// guards is a bare Enter sent into whatever pane happened to be active.
func TestShellLayoutTypesNothingIntoThePane(t *testing.T) {
	shell, err := ResolveLayout("shell", "", nil)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	t.Run("create builds the window and sends no keys", func(t *testing.T) {
		pinPaneShell(t)
		repoRoot := t.TempDir()

		mockGitBase := commands.NewMockBaseCommand()
		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(repoRoot+"\n", "", nil), // rev-parse --show-toplevel
			commands.ExecCommandResult("", "", nil),            // everything else
		)
		mockTmuxBase := commands.NewMockBaseCommand()
		mockTmuxBase.SetExecCommandResults(commands.ExecCommandResult("", "", nil))

		wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

		wtPath := filepath.Join(
			paths.Paths.Data.Root, "devgeta", "worktrees",
			filepath.Base(repoRoot), "plain-test",
		)
		t.Cleanup(func() {
			if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		if err := wm.Create("plain-test", shell, true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		order := tmuxCommandOrder(mockTmuxBase)
		if !slices.Contains(order, "new-window") {
			t.Errorf("expected the window to be created, calls: %v", order)
		}
		assertNoSendKeys(t, mockTmuxBase)
		if got, ok := paneCommandArg(mockTmuxBase.ExecCommandCalls, "new-window"); ok {
			t.Errorf(
				"the shell layout's pane must be created with NO command argument, "+
					"new-window carried %q",
				got,
			)
		}
		// One pane means no split and no reselect either - there is nothing to
		// split off and nothing to come back to.
		if slices.Contains(order, "split-window") {
			t.Errorf("a one-pane layout must not split, calls: %v", order)
		}
	})

	t.Run("repairing a live window is a no-op", func(t *testing.T) {
		repoSlug := "myrepo"
		windowName := GetWindowName(repoSlug, "plain-test")

		mockTmuxBase := commands.NewMockBaseCommand()
		mockTmuxBase.SetExecCommandResults(
			// WindowSession: the window is already live.
			commands.ExecCommandResult(
				TmuxSessionName(repoSlug)+"\t"+windowName+"\n", "", nil,
			),
		)
		wm := newLayoutTestWM(commands.NewMockBaseCommand(), mockTmuxBase)

		if err := wm.ensureWindow(repoSlug, windowName, "/tmp/wt", shell); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		order := tmuxCommandOrder(mockTmuxBase)
		if slices.Contains(order, "send-keys") {
			t.Errorf(
				"repairing a live shell window must send nothing (the shell is already there), calls: %v",
				order,
			)
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
			{Command: "x", check: func() (string, error) {
				return "", errors.New("tool missing")
			}},
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

// testReviewCommand builds the TYPED review command for key the same way
// reviewerPane does (lookupBuiltinReviewer, then reviewCommandFor), so the
// send-keys assertions below compare against the production string rather than a
// second copy of it. It probes nothing: reviewCommandFor is the probe-free half of
// reviewerPane.
func testReviewCommand(t *testing.T, key string) string {
	t.Helper()
	reviewer, err := lookupBuiltinReviewer(key)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	return reviewCommandFor(&OpenCodeCoder{}, reviewer)
}

// TestLaunchReviewNoLiveWindowUsesEnsureWindowCreatePath proves that, when
// the worktree's window doesn't exist yet, LaunchReviewInRepo drives the same
// create-if-missing path ensureWindow's own no-window branch uses
// (createWindowWithLayout), with a one-pane layout whose only pane's command
// is the review command - never a split (there's nothing to split yet), and
// never a second WindowSession lookup (LaunchReviewInRepo's own check already
// established the window is missing, so it calls createWindowWithLayout
// directly instead of routing back through ensureWindow).
//
// This is a CREATE path, so the review command is exec'd as the pane's process
// by new-window itself: the send-keys that used to follow it is gone, and a
// show-options (paneShell's tmux default-shell candidate) precedes the create.
func TestLaunchReviewNoLiveWindowUsesEnsureWindowCreatePath(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name == "opencode" })
	shell := pinPaneShell(t)

	repoSlug := "myrepo"
	name := "feat"

	mockGitBase := commands.NewMockBaseCommand()
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil), // WindowSession (LaunchReviewInRepo's own check)
		commands.ExecCommandResult("", "", nil), // show-options (paneShell, no answer)
		commands.ExecCommandResult("", "", nil), // HasSession -> true (nil err)
		commands.ExecCommandResult("", "", nil), // CreateWindowInSession: new-window + review cmd
	)

	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	if err := wm.LaunchReviewInRepo(repoSlug, name, "code"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantOrder := []string{"list-windows", "show-options", "has-session", "new-window"}
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

	// new-window itself carries the review command. The probe here is stubbed to
	// find opencode WITHOUT resolving a path, so the pane takes ADR-0020's
	// interactive fallback - whose inner script is the same reviewer command the
	// idle-shell-reuse branch types, `--agent` and all. A review that silently
	// lost its agent flag would launch a plain coder session that looks identical
	// from outside, so the exact string is pinned rather than a substring.
	paneCmd, ok := paneCommandArg(mockTmuxBase.ExecCommandCalls, "new-window")
	if !ok {
		t.Fatalf("expected new-window to carry the review command, calls: %+v",
			mockTmuxBase.ExecCommandCalls)
	}
	want := interactivePaneCommand(testReviewCommand(t, "code"), shell)
	if paneCmd != want {
		t.Errorf("new-window carried %q, want %q", paneCmd, want)
	}
	assertNoSendKeys(t, mockTmuxBase)
}

// TestLaunchReviewReusesAnIdleShellPane covers the other half of the rule the
// split enforces: "never type into a pane that is running something" is not
// "always split". A pane sitting at a shell prompt is running nothing, so the
// review goes there and no second pane appears - which is the whole point of
// creating a worktree with the shell layout (an empty window, on purpose) and
// then pressing R.
//
// This branch is also the one place in the review path that still uses
// send-keys, and it must keep doing so: the pane already exists and is running
// the user's interactive shell, so there is no pane creation to exec the command
// as (ADR-0020 part 4 - the test for membership is "is this pane new?"). The
// payload is therefore the TYPED form, pane.Command, which is what
// testReviewCommand builds. No show-options appears in these sequences either:
// this branch needs no shell, so paneShell is never reached.
func TestLaunchReviewReusesAnIdleShellPane(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name == "opencode" })

	repoSlug := "myrepo"
	name := "feat"
	windowName := GetWindowName(repoSlug, name)
	session := "some-session"

	reviewCmd := testReviewCommand(t, "code")

	t.Run("single idle pane is reused, nothing is split", func(t *testing.T) {
		mockTmuxBase := commands.NewMockBaseCommand()
		mockTmuxBase.SetExecCommandResults(
			commands.ExecCommandResult(session+"\t"+windowName+"\n", "", nil), // WindowSession
			commands.ExecCommandResult("%7\n", "", nil),                       // ActivePaneID
			commands.ExecCommandResult(
				windowName+"\t%7\tzsh\n",
				"",
				nil,
			), // PanesInWindow: idle
			commands.ExecCommandResult("", "", nil), // SendKeysToPane
		)
		wm := newLayoutTestWM(commands.NewMockBaseCommand(), mockTmuxBase)

		if err := wm.LaunchReviewInRepo(repoSlug, name, "code"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		order := tmuxCommandOrder(mockTmuxBase)
		if slices.Contains(order, "split-window") {
			t.Errorf("an idle shell pane must be reused, not split beside, calls: %v", order)
		}
		// Reusing the pane that was already active means there is no focus to
		// put back - a select-pane here would be a call with nothing to do.
		if slices.Contains(order, "select-pane") {
			t.Errorf(
				"no focus restore is needed when the reused pane was already active, calls: %v",
				order,
			)
		}
		target, found := sendKeysTarget(mockTmuxBase.ExecCommandCalls, reviewCmd)
		if !found {
			t.Fatalf("expected the review command to be sent, calls: %+v",
				mockTmuxBase.ExecCommandCalls)
		}
		if target != "%7" {
			t.Errorf("expected the review sent to pane %%7 by id, got target %q", target)
		}
	})

	t.Run("idle pane is preferred over the active busy one, focus restored", func(t *testing.T) {
		// A shell-layout worktree the user added a pane to: pane 0 is the
		// original idle shell, pane 1 is running a dev server and is active.
		mockTmuxBase := commands.NewMockBaseCommand()
		mockTmuxBase.SetExecCommandResults(
			commands.ExecCommandResult(session+"\t"+windowName+"\n", "", nil), // WindowSession
			commands.ExecCommandResult(
				"%9\n",
				"",
				nil,
			), // ActivePaneID: the busy one
			commands.ExecCommandResult(
				windowName+"\t%8\tzsh\n"+windowName+"\t%9\tnpm\n", "", nil,
			), // PanesInWindow: %8 idle, %9 busy
			commands.ExecCommandResult("", "", nil), // SendKeysToPane
			commands.ExecCommandResult("", "", nil), // SelectPane (restore focus to %9)
		)
		wm := newLayoutTestWM(commands.NewMockBaseCommand(), mockTmuxBase)

		if err := wm.LaunchReviewInRepo(repoSlug, name, "code"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		order := tmuxCommandOrder(mockTmuxBase)
		if slices.Contains(order, "split-window") {
			t.Errorf("expected the idle pane to be reused, calls: %v", order)
		}
		target, found := sendKeysTarget(mockTmuxBase.ExecCommandCalls, reviewCmd)
		if !found || target != "%8" {
			t.Errorf("expected the review sent to the idle pane %%8, got %q (found=%v)",
				target, found)
		}
		if !slices.Contains(order, "select-pane") {
			t.Errorf("expected focus restored to the previously active pane, calls: %v", order)
		}
	})

	t.Run("a failed send never kills the pane it did not create", func(t *testing.T) {
		mockTmuxBase := commands.NewMockBaseCommand()
		mockTmuxBase.SetExecCommandResults(
			commands.ExecCommandResult(session+"\t"+windowName+"\n", "", nil), // WindowSession
			commands.ExecCommandResult("%7\n", "", nil),                       // ActivePaneID
			commands.ExecCommandResult(
				windowName+"\t%7\tzsh\n",
				"",
				nil,
			), // PanesInWindow: idle
			commands.ExecCommandResult(
				"",
				"",
				errors.New("boom"),
			), // SendKeysToPane fails
		)
		wm := newLayoutTestWM(commands.NewMockBaseCommand(), mockTmuxBase)

		if err := wm.LaunchReviewInRepo(repoSlug, name, "code"); err == nil {
			t.Fatal("expected an error when the send fails")
		}

		order := tmuxCommandOrder(mockTmuxBase)
		if slices.Contains(order, "kill-pane") {
			t.Errorf(
				"killing a reused pane would destroy the user's own shell, calls: %v",
				order,
			)
		}
	})
}

// TestLaunchReviewLiveWindowSplitsNewPane proves that, when the worktree's
// window already exists and its pane is BUSY, LaunchReviewInRepo never touches
// that pane - it splits a new pane whose PROCESS is the review command, then
// restores focus to the original pane.
//
// The pane is reported as "2.1.222" (a real Claude Code pane_current_command -
// tmux's automatic-rename picking up the versioned binary directory, see
// ADR-0008), which is exactly the case an allowlist of idle shells must treat
// as busy without recognizing the agent at all.
//
// This branch CREATES a pane, so ADR-0020 part 3 governs it even though the
// window is already live - the test is "is this pane new?", not "is this window
// live?". Three calls are therefore gone from the sequence below: the
// ActivePaneID that used to read the new pane's id, the send-keys that typed the
// command into it, and (implicitly) any need to target that pane at all. One is
// new: show-options, paneShell's tmux default-shell candidate, which this branch
// needs because it builds a created pane's command.
func TestLaunchReviewLiveWindowSplitsNewPane(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name == "opencode" })
	shell := pinPaneShell(t)

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
		commands.ExecCommandResult(
			windowName+"\t%1\t2.1.222\n",
			"",
			nil,
		), // PanesInWindow: the one pane is running an agent, not a shell
		commands.ExecCommandResult("", "", nil), // show-options (paneShell, no answer)
		commands.ExecCommandResult("", "", nil), // SplitWindow (new pane + the review cmd)
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
		"list-panes",   // the idle-pane check that decides reuse vs split
		"show-options", // paneShell, needed only because a pane is being created
		"split-window",
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

	// The review command is the new pane's process, carried by the split itself.
	// That also retires the hazard the old send-keys had to work around: there is
	// no window between "the pane exists" and "the command arrives" in which
	// focus could shift and land the review in the coder's pane, and no target to
	// get wrong.
	paneCmd, ok := paneCommandArg(mockTmuxBase.ExecCommandCalls, "split-window")
	if !ok {
		t.Fatalf("expected split-window to carry the review command, calls: %+v",
			mockTmuxBase.ExecCommandCalls)
	}
	want := interactivePaneCommand(testReviewCommand(t, "code"), shell)
	if paneCmd != want {
		t.Errorf("split-window carried %q, want %q", paneCmd, want)
	}
	assertNoSendKeys(t, mockTmuxBase)

	// Focus must be restored to the coder's pane (%1), captured before the
	// split - not left on the new review pane, which split-window makes active.
	if !callsContain(mockTmuxBase.ExecCommandCalls, "select-pane", "%1") {
		t.Errorf("expected select-pane to restore focus to coder pane %%1, calls: %+v",
			mockTmuxBase.ExecCommandCalls)
	}
}

// TestLaunchReviewLiveWindowFailedSplitLeavesTheUsersWindowAlone proves the
// blast radius of a failed review launch in a live window: nothing of the user's
// is touched - not the window (which holds their coder pane), not that pane, and
// not the worktree (R never creates one, so there's none to roll back).
//
// It used to assert that a send-keys failing AFTER a successful split killed the
// pane the split had just added. That failure mode no longer exists: the review
// command is an argument of the split, so split-window either creates the pane
// with its command already running or fails having created nothing. The
// remaining failure is the split itself, and the assertion is that it cleans up
// nothing - because there is nothing of devgeta's making to clean up, and
// everything else in that window belongs to the user.
func TestLaunchReviewLiveWindowFailedSplitLeavesTheUsersWindowAlone(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name == "opencode" })
	pinPaneShell(t)

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
		commands.ExecCommandResult(
			windowName+"\t%1\tnvim\n",
			"",
			nil,
		), // PanesInWindow: busy pane, so this is the split path
		commands.ExecCommandResult("", "", nil),                // show-options (paneShell)
		commands.ExecCommandResult("", "", errors.New("boom")), // SplitWindow fails
	)

	wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

	err := wm.LaunchReviewInRepo(repoSlug, name, "code")
	if err == nil {
		t.Fatal("expected an error when the split fails")
	}
	if !strings.Contains(err.Error(), "failed to split window") {
		t.Fatalf("expected the SPLIT to be what failed, got: %v", err)
	}

	// A failed split created no pane, so there is nothing to kill. This is
	// stronger than the old assertion, not weaker: killing anything here would
	// mean killing a pane devgeta did not create.
	for _, c := range mockTmuxBase.ExecCommandCalls {
		if len(c.Args) > 0 && c.Args[0] == "kill-pane" {
			t.Errorf("a failed split creates no pane, so nothing may be killed, calls: %+v",
				mockTmuxBase.ExecCommandCalls)
		}
	}

	// Must never touch the window or the worktree either.
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
// key's error from lookupBuiltinReviewer propagates before any git or tmux state
// is touched.
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
// LaunchReviewInRepo checks that the opencode BINARY resolves before touching
// tmux at all - mirroring TestCreateValidateLayoutFailsBeforeAnyTmuxCall's shape
// for the analogous check on the create path. A user whose default layout is
// claude/claude-nvim has never needed opencode, so pressing R without this
// pre-flight check would build a window/pane whose command prints "command not
// found" while the dashboard still reports "review started", with no error
// surfaced anywhere.
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
	setShellCommandExistsFn(t, func(name string) bool { return name == "opencode" })
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
		commands.ExecCommandResult("", "", nil), // show-options (paneShell)
		commands.ExecCommandResult("", "", nil), // HasSession -> true
		commands.ExecCommandResult("", "", nil), // CreateWindowInSession: new-window + review cmd
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

// TestLaunchReviewProbesExactlyOnce covers the whole LaunchReviewInRepo call, not
// just reviewerPane, and it asserts ONE - not "at least one".
//
// The review path is the one launch that does its own preflight rather than going
// through validateLayout, so it is the one where a second probe could hide: at
// pane-command build time, or in a Layout the create branch assembles. Both of its
// branches are exercised here from the same constructor, so a re-probe added to
// either fails.
//
// Two probes would not just be slow (up to two 5-second timeouts per pane); they
// are two observations of a changing system and can DISAGREE, which would mean the
// check verified something other than what ran - the property ADR-0020 exists to
// establish.
func TestLaunchReviewProbesExactlyOnce(t *testing.T) {
	repoSlug := "myrepo"
	name := "feat"
	windowName := GetWindowName(repoSlug, name)
	session := "some-session"

	tests := []struct {
		name string
		tmux func() *commands.MockBaseCommand
	}{
		{
			name: "no live window: the create branch",
			tmux: func() *commands.MockBaseCommand {
				mockTmuxBase := commands.NewMockBaseCommand()
				mockTmuxBase.SetExecCommandResults(
					commands.ExecCommandResult("", "", nil), // WindowSession: no window
					commands.ExecCommandResult("", "", nil), // show-options (paneShell)
					commands.ExecCommandResult("", "", nil), // HasSession
					commands.ExecCommandResult("", "", nil), // new-window + the review cmd
				)
				return mockTmuxBase
			},
		},
		{
			name: "live window with a busy pane: the split branch",
			tmux: func() *commands.MockBaseCommand {
				mockTmuxBase := commands.NewMockBaseCommand()
				mockTmuxBase.SetExecCommandResults(
					commands.ExecCommandResult(
						session+"\t"+windowName+"\n", "", nil,
					), // WindowSession: exists
					commands.ExecCommandResult("%1\n", "", nil), // ActivePaneID
					commands.ExecCommandResult(
						windowName+"\t%1\t2.1.222\n", "", nil,
					), // PanesInWindow: busy
					commands.ExecCommandResult("", "", nil), // show-options (paneShell)
					commands.ExecCommandResult("", "", nil), // SplitWindow + the review cmd
					commands.ExecCommandResult("", "", nil), // SelectPane
				)
				return mockTmuxBase
			},
		},
		{
			name: "live window with an idle shell pane: the reuse branch",
			tmux: func() *commands.MockBaseCommand {
				mockTmuxBase := commands.NewMockBaseCommand()
				mockTmuxBase.SetExecCommandResults(
					commands.ExecCommandResult(
						session+"\t"+windowName+"\n", "", nil,
					), // WindowSession: exists
					commands.ExecCommandResult("%7\n", "", nil), // ActivePaneID
					commands.ExecCommandResult(
						windowName+"\t%7\tzsh\n", "", nil,
					), // PanesInWindow: idle
					commands.ExecCommandResult("", "", nil), // SendKeysToPane
				)
				return mockTmuxBase
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probes := 0
			setShellCommandLookupPathFn(
				t,
				func(lookupName string) (string, commands.ShellLookupResult) {
					probes++
					if lookupName == "opencode" {
						return "/opt/homebrew/bin/opencode", commands.ShellLookupFound
					}
					return "", commands.ShellLookupNotFound
				},
			)

			mockTmuxBase := tt.tmux()
			wm := newLayoutTestWM(commands.NewMockBaseCommand(), mockTmuxBase)

			if err := wm.LaunchReviewInRepo(repoSlug, name, "code"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if probes != 1 {
				t.Errorf("expected exactly 1 opencode probe for a review launch, got %d", probes)
			}
		})
	}
}

// longTestPrompt is a --prompt value well past the 1024-byte tty input queue
// limit that ADR-0020 measured. Every byte past that limit was silently
// discarded by the terminal driver when the command was TYPED into the pane -
// the tail of the command AND the trailing Enter - while `tmux send-keys` still
// exited 0. The result was a window that looked correctly created, an AI coder
// sitting at an empty session, and `dg wt create` reporting success.
//
// It is a plausible task description rather than filler, because it also has to
// survive shell quoting: it contains apostrophes, quotes, and shell
// metacharacters that a naive assembly would break on.
var longTestPrompt = strings.Repeat(
	"Fix the payment worker's flaky retry: it re-enqueues on a 5xx but doesn't "+
		"back off, so a slow downstream turns into a storm (see \"incident 1082\" "+
		"& the $RETRY_BUDGET note). ",
	8,
)

// TestLongPromptSurvivesToThePaneCommandOnEveryCreatePath is this cycle's
// regression test. A --prompt far larger than the pty input queue could hold has
// to reach the pane INTACT, on every one of the three tmux calls that can create
// a window's first pane:
//
//	new-window   (current session)  - CreateWindow
//	new-window   (named session)    - CreateWindowInSession
//	new-session  (first worktree for a repo) - CreateSessionWithWindow
//
// The third is the one worth spelling out: it reads as session setup rather than
// pane setup, so it is easy to leave behind - and it is the path taken the FIRST
// time a worktree is created for a repo, i.e. the case a user hits most. If it
// alone kept typing, the bug would survive exactly where it hurts (ADR-0020).
//
// The window builders are called directly rather than through Create/CreateAt so
// each case's tmux queue stays about pane creation instead of worktree plumbing;
// the create-path coverage through Create/CreateAt lives in the tests above.
func TestLongPromptSurvivesToThePaneCommandOnEveryCreatePath(t *testing.T) {
	if len(longTestPrompt) <= 1023 {
		t.Fatalf(
			"setup: this test is meaningless unless the prompt exceeds the 1023-byte "+
				"send-keys limit, got %d bytes",
			len(longTestPrompt),
		)
	}

	const claudePath = "/Users/dev/.local/bin/claude"
	repoSlug := "myrepo"
	windowName := GetWindowName(repoSlug, "feat")
	wtPath := "/tmp/wt/myrepo/feat"

	tests := []struct {
		name string
		// hasSession drives createWindowWithLayout's branch; ignored by the
		// current-session case, which has no session to look for.
		build func(t *testing.T, wm *WorktreeManager, layout Layout) error
		verb  string
	}{
		{
			name: "CreateWindow: the current session",
			build: func(_ *testing.T, wm *WorktreeManager, layout Layout) error {
				return wm.buildWindowFromLayout(windowName, wtPath, layout)
			},
			verb: "new-window",
		},
		{
			name: "CreateWindowInSession: the repo session already exists",
			build: func(t *testing.T, wm *WorktreeManager, layout Layout) error {
				// HasSession is ExecuteCommand-based: a nil error means "exists".
				wm.Tmux.Base.(*commands.MockBaseCommand).SetExecCommandResults(
					commands.ExecCommandResult("", "", nil),
				)
				return wm.createWindowWithLayout(repoSlug, windowName, wtPath, layout)
			},
			verb: "new-window",
		},
		{
			name: "CreateSessionWithWindow: the first worktree for a repo",
			build: func(t *testing.T, wm *WorktreeManager, layout Layout) error {
				// show-options answers first (harmlessly, with a non-absolute
				// value), then has-session must FAIL so the session is created.
				wm.Tmux.Base.(*commands.MockBaseCommand).SetExecCommandResults(
					commands.ExecCommandResult("", "", nil), // show-options
					commands.ExecCommandResult("", "no such session", os.ErrNotExist),
					commands.ExecCommandResult("", "", nil), // new-session
				)
				return wm.createWindowWithLayout(repoSlug, windowName, wtPath, layout)
			},
			verb: "new-session",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell := pinPaneShell(t)
			// A Found probe carrying a resolved path, so the pane takes the
			// exec recipe - the production shape now that the probed token is
			// the binary (ADR-0020's 2026-08-07 amendment).
			setShellCommandLookupPathFn(
				t,
				func(name string) (string, commands.ShellLookupResult) {
					if name == "claude" {
						return claudePath, commands.ShellLookupFound
					}
					return "", commands.ShellLookupNotFound
				},
			)

			layout, err := ResolveLayout("claude", "", nil)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			layout, err = layout.WithPrompt(longTestPrompt)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			layout, err = layout.EnsureInstalled()
			if err != nil {
				t.Fatalf("setup: %v", err)
			}

			mockTmuxBase := commands.NewMockBaseCommand()
			mockTmuxBase.SetExecCommandResults(commands.ExecCommandResult("", "", nil))
			wm := newLayoutTestWM(commands.NewMockBaseCommand(), mockTmuxBase)

			if err := tt.build(t, wm, layout); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			paneCmd, ok := paneCommandArg(mockTmuxBase.ExecCommandCalls, tt.verb)
			if !ok {
				t.Fatalf("expected %s to carry the pane's command, calls: %+v",
					tt.verb, mockTmuxBase.ExecCommandCalls)
			}

			// The whole prompt, in ONE argv element, as one shell-quoted word.
			// The expectation goes through shellSingleQuote because the prompt
			// deliberately contains apostrophes, which the command line has to
			// escape - so the raw text is NOT a substring of a correct command,
			// and asserting the raw text would only pass for an unquoted (broken)
			// one. A truncation at the old 1024-byte boundary fails here, and so
			// does any attempt to split the payload up to sneak under that limit,
			// which ADR-0020 rules out.
			if !strings.Contains(paneCmd, shellSingleQuote(longTestPrompt)) {
				t.Errorf(
					"the prompt did not survive into %s's pane command (%d bytes carried, "+
						"%d bytes of prompt): %q",
					tt.verb, len(paneCmd), len(longTestPrompt), paneCmd,
				)
			}
			// ...and it is the coder's argument, exec'd from the probe's own
			// resolved path, with the pane still keeping a shell afterward.
			if !strings.Contains(paneCmd, shellSingleQuote(claudePath)) {
				t.Errorf("expected the probe's resolved path %q in %q", claudePath, paneCmd)
			}
			if !strings.HasSuffix(paneCmd, "; exec "+shellSingleQuote(shell)) {
				t.Errorf("expected the pane to keep a shell after the command exits, got %q",
					paneCmd)
			}

			assertNoSendKeys(t, mockTmuxBase)
		})
	}
}

// TestCreatePathsNeverTypeIntoAPane sweeps the layout shapes a create can
// produce and asserts the property that used to be violated everywhere: on a
// create path, devgeta issues NO send-keys at all. Every pane's command is an
// argument of the tmux call that created that pane.
//
// The layouts are chosen for the four pane kinds that reach creationCommand by
// different routes: a devgeta-owned coder pane (with a prompt), the editor pane
// beside it, a user-authored --pane value, and a pane with no command at all.
func TestCreatePathsNeverTypeIntoAPane(t *testing.T) {
	repoSlug := "myrepo"
	windowName := GetWindowName(repoSlug, "feat")
	wtPath := "/tmp/wt/myrepo/feat"

	tests := []struct {
		name string
		// build returns the resolved layout to create.
		build func(t *testing.T) Layout
		// wantInPaneCommand is the distinctive substring expected in the command
		// carried by each pane's creating call, in pane order. An empty string
		// means that pane must be created with NO command argument.
		wantInPaneCommand []string
	}{
		{
			name: "coder pane with a prompt, plus the editor pane",
			build: func(t *testing.T) Layout {
				return resolvedLayoutForTest(t, "claude-nvim", "explain issue 1082")
			},
			wantInPaneCommand: []string{"explain issue 1082", nvimCommand},
		},
		{
			name: "coder pane plus a user-authored --pane value",
			build: func(t *testing.T) Layout {
				layout := resolvedLayoutForTest(t, "claude", "")
				layout, err := layout.WithExtraPanes([]string{"cd api && make dev"})
				if err != nil {
					t.Fatalf("setup: %v", err)
				}
				return layout
			},
			// The --pane value stays a command line, unparsed and unsplit
			// (ADR-0011), inside the interactive recipe.
			wantInPaneCommand: []string{"claude", "cd api && make dev"},
		},
		{
			name: "the shell layout: a pane with no command",
			build: func(t *testing.T) Layout {
				return resolvedLayoutForTest(t, "shell", "")
			},
			wantInPaneCommand: []string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pinPaneShell(t)
			layout := tt.build(t)

			mockTmuxBase := commands.NewMockBaseCommand()
			// One repeated result serves every call: show-options answers with a
			// non-absolute value (dropped as a shell candidate), ActivePaneID
			// answers with a pane id, and everything else succeeds.
			mockTmuxBase.SetExecCommandResults(commands.ExecCommandResult("%1\n", "", nil))
			wm := newLayoutTestWM(commands.NewMockBaseCommand(), mockTmuxBase)

			if err := wm.buildWindowFromLayout(windowName, wtPath, layout); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertNoSendKeys(t, mockTmuxBase)

			created := paneCreatingCalls(mockTmuxBase.ExecCommandCalls)
			if len(created) != len(tt.wantInPaneCommand) {
				t.Fatalf("expected %d pane-creating calls, got %d: %+v",
					len(tt.wantInPaneCommand), len(created), created)
			}
			for i, want := range tt.wantInPaneCommand {
				got, ok := paneCommandOf(created[i])
				if want == "" {
					if ok {
						t.Errorf("pane %d must be created with no command, got %q", i+1, got)
					}
					continue
				}
				if !ok {
					t.Errorf("pane %d was created with no command, want one containing %q",
						i+1, want)
					continue
				}
				if !strings.Contains(got, want) {
					t.Errorf("pane %d's command %q does not contain %q", i+1, got, want)
				}
			}
		})
	}
}

// resolvedLayoutForTest resolves a built-in layout, optionally retargets its
// coder pane with prompt, and runs the install probes - the same three steps
// validateLayout drives on a real create, so the returned layout carries the one
// probe's resolution its panes' created commands are built from.
//
// The probe is stubbed to find every tool but resolve NO path, which selects
// ADR-0020's interactive fallback. That is the deliberate choice here: this test
// is about which tmux call carries a command, and the fallback is the recipe that
// keeps the pane's command human-readable in the assertions.
func resolvedLayoutForTest(t *testing.T, name, prompt string) Layout {
	t.Helper()
	setShellCommandExistsFn(t, func(string) bool { return true })

	layout, err := ResolveLayout(name, "", nil)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	layout, err = layout.WithPrompt(prompt)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	layout, err = layout.EnsureInstalled()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	return layout
}
