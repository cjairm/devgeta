// Tests for the review-launch paths, LaunchReviewInRepo and
// launchReviewInLiveWindow: the create-if-missing path, the idle-shell reuse,
// the live-window split, and the pre-tmux validation failures.

package worktree

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
)

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
	// find opencode WITHOUT resolving a path, so the pane takes ADR-0021's
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
// as (ADR-0021 part 4 - the test for membership is "is this pane new?"). The
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
// This branch CREATES a pane, so ADR-0021 part 3 governs it even though the
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
// check verified something other than what ran - the property ADR-0021 exists to
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
