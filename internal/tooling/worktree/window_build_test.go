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
// shell. It is also what every pane-creating call looked like before ADR-0021,
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
// existence, in call order - the four verbs ADR-0021 makes responsible for
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
// input queue that silently swallowed a long --prompt, ADR-0021.
func assertNoSendKeys(t *testing.T, mockBase *commands.MockBaseCommand) {
	t.Helper()
	if slices.Contains(tmuxCommandOrder(mockBase), "send-keys") {
		t.Errorf(
			"a create path must type nothing into a pane - every pane's command is "+
				"exec'd at creation (ADR-0021), calls: %+v",
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
// Two calls that used to be here are gone, and both are ADR-0021's doing: the
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

	if err := wm.Create("feature-test", "", twoPaneLayout, true); err != nil {
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

	if err := wm.Create("feature-test", "", stubLayout, true); err != nil {
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

// TestPaneShellCandidateLadder pins WHICH candidates paneShell hands
// resolveShell, and in which order: the user's $SHELL first, then tmux's
// default-shell (ADR-0021's ladder, with resolveShell's /bin/sh floor behind
// both).
//
// resolveShell itself is covered as a pure function in layout_test.go, but with
// its candidates handed in directly - so nothing there notices if paneShell
// stops supplying $SHELL, or supplies the two rungs the other way round. Every
// other test in this file neutralizes both rungs on purpose (pinPaneShell empties
// $SHELL and the mocked show-options never answers with an absolute path) so it
// can assert an exact pane command, which means every one of them lands on the
// floor. This test is the one that reads the ladder itself, and it matters
// because the shell is interpolated into every created pane's command - twice in
// the interactive recipe.
//
// Both subtests go through a real create so the assertion is on the command tmux
// is actually given, not on paneShell's return value: what the ladder is for is
// deciding what runs in the pane.
func TestPaneShellCandidateLadder(t *testing.T) {
	// runCreate builds a one-pane window with the given mocked tmux
	// show-options answer and returns new-window's pane-command argument plus
	// the tmux call order.
	runCreate := func(t *testing.T, tmuxDefaultShell string) (string, []string) {
		t.Helper()
		repoRoot := t.TempDir()

		mockGitBase := commands.NewMockBaseCommand()
		mockGitBase.SetExecCommandResults(
			commands.ExecCommandResult(repoRoot+"\n", "", nil), // rev-parse --show-toplevel
			commands.ExecCommandResult("", "", nil),            // everything else
		)
		mockTmuxBase := commands.NewMockBaseCommand()
		mockTmuxBase.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil), // worktreeState: list-windows
			// paneShell: show-options -gv default-shell
			commands.ExecCommandResult(tmuxDefaultShell+"\n", "", nil),
			commands.ExecCommandResult("", "", nil), // CreateWindow: new-window
		)

		wm := newLayoutTestWM(mockGitBase, mockTmuxBase)

		wtPath := filepath.Join(
			paths.Paths.Data.Root, "devgeta", "worktrees",
			filepath.Base(repoRoot), "feature-test",
		)
		t.Cleanup(func() {
			if err := os.RemoveAll(filepath.Dir(wtPath)); err != nil {
				t.Logf("cleanup: %v", err)
			}
		})

		if err := wm.Create("feature-test", "", stubLayout, true); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		paneCmd, ok := paneCommandArg(mockTmuxBase.ExecCommandCalls, "new-window")
		if !ok {
			t.Fatalf("new-window carried no pane command, calls: %+v",
				mockTmuxBase.ExecCommandCalls)
		}
		// Every git and tmux call this create makes goes through a
		// MockBaseCommand, so nothing here executes. (testutil's
		// VerifyNoRealCommands is not the check for that: it asserts ZERO
		// recorded calls, which is a different property and one a create
		// deliberately violates.)
		return paneCmd, tmuxCommandOrder(mockTmuxBase)
	}

	t.Run("a usable $SHELL wins over tmux's default-shell", func(t *testing.T) {
		envShell := usableShellFixture(t, "env-zsh")
		tmuxShell := usableShellFixture(t, "tmux-bash")
		t.Setenv("SHELL", envShell)

		paneCmd, order := runCreate(t, tmuxShell)

		if want := interactivePaneCommand("stub-cmd", envShell); paneCmd != want {
			t.Errorf("new-window carried %q, want %q ($SHELL is the first rung)", paneCmd, want)
		}
		if strings.Contains(paneCmd, tmuxShell) {
			t.Errorf("pane command used tmux's default-shell %q over $SHELL: %q",
				tmuxShell, paneCmd)
		}
		// The tmux query is deliberately unconditional - not skipped just
		// because $SHELL turned out usable - so the tmux calls a create issues
		// don't depend on the machine's environment. The ordered call-sequence
		// tests in this file assert that sequence, so dropping the query here
		// would break them on some machines and not others.
		if !slices.Contains(order, "show-options") {
			t.Errorf("paneShell must query tmux's default-shell unconditionally, calls: %v", order)
		}
	})

	t.Run("tmux's default-shell is used when $SHELL is unusable", func(t *testing.T) {
		tmuxShell := usableShellFixture(t, "tmux-bash")
		t.Setenv("SHELL", "")

		paneCmd, _ := runCreate(t, tmuxShell)

		if want := interactivePaneCommand("stub-cmd", tmuxShell); paneCmd != want {
			t.Errorf("new-window carried %q, want %q (tmux's default-shell is the second rung)",
				paneCmd, want)
		}
		if strings.Contains(paneCmd, posixShell) {
			t.Errorf("pane command fell to the %q floor with a usable tmux default-shell: %q",
				posixShell, paneCmd)
		}
	})
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

	err := wm.Create("feature-test", "", twoPaneLayout, true)
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

	err := wm.CreateAt(repoRoot, "feature-test", "", twoPaneLayout, true)
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
				return wm.Create("feature-test", "", twoPaneLayout, true)
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
				return wm.CreateAt(repoRoot, "feature-test", "", twoPaneLayout, true)
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

		if err := wm.Create("plain-test", "", shell, true); err != nil {
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

	err := wm.Create("feature-test", "", failingLayout, true)
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

// longTestPrompt is a --prompt value well past the 1024-byte tty input queue
// limit that ADR-0021 measured. Every byte past that limit was silently
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
// alone kept typing, the bug would survive exactly where it hurts (ADR-0021).
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
			// the binary (ADR-0021's 2026-08-07 amendment).
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
			// which ADR-0021 rules out.
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
// ADR-0021's interactive fallback. That is the deliberate choice here: this test
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
