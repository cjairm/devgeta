package tuiworktree

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// --- R opens the picker ---

func TestKickReviewOpensPickerCodeFirst(t *testing.T) {
	m := makeTestModel(testStatuses()) // cursor starts on repo-a/feature-a

	m2, cmd := m.Update(tea.KeyPressMsg{Code: 'R'})
	m3 := m2.(Model)

	if cmd != nil {
		t.Error("R should return no command (only opens the picker)")
	}
	if m3.reviewMode != reviewPick {
		t.Fatalf("R should enter reviewPick mode, got mode=%d", m3.reviewMode)
	}
	if m3.reviewPicker == nil {
		t.Fatal("expected a review picker to be built")
	}
	if m3.reviewRepo != "repo-a" || m3.reviewWorktreeName != "feature-a" {
		t.Errorf(
			"expected reviewRepo/reviewWorktreeName captured from cursor row, got repo=%q name=%q",
			m3.reviewRepo, m3.reviewWorktreeName,
		)
	}

	sel, ok := m3.reviewPicker.Selected()
	if !ok || sel.Command != "code" {
		t.Errorf("expected 'code' pre-selected first, got %+v ok=%v", sel, ok)
	}
}

// --- R is a no-op on repo-header / session rows ---

func TestKickReviewNoopOnRepoHeaderRow(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.cursor = 0 // repo-a header row (see TestBuildRowsGrouping)
	if m.rows[m.cursor].kind != rowRepo {
		t.Fatalf(
			"test setup: expected cursor on a repo-header row, got kind=%d",
			m.rows[m.cursor].kind,
		)
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: 'R'})
	m3 := m2.(Model)

	if cmd != nil {
		t.Error("R on a repo-header row should return no command")
	}
	if m3.reviewMode != reviewNone {
		t.Errorf("R on a repo-header row must not change reviewMode, got %d", m3.reviewMode)
	}
	if m3.reviewPicker != nil {
		t.Error("R on a repo-header row must not build a review picker")
	}
}

func TestKickReviewNoopOnSessionRow(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	cursorToSession(&m, "notes")

	m2, cmd := m.Update(tea.KeyPressMsg{Code: 'R'})
	m3 := m2.(Model)

	if cmd != nil {
		t.Error("R on a session row should return no command")
	}
	if m3.reviewMode != reviewNone {
		t.Errorf("R on a session row must not change reviewMode, got %d", m3.reviewMode)
	}
	if m3.reviewPicker != nil {
		t.Error("R on a session row must not build a review picker")
	}
}

// --- R is a no-op while a launch is already in flight ---

func TestKickReviewIgnoredWhileLaunching(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.reviewLaunching = true

	m2, cmd := m.Update(tea.KeyPressMsg{Code: 'R'})
	m3 := m2.(Model)

	if cmd != nil {
		t.Error("R while a review launch is in flight should return no command")
	}
	if m3.reviewMode != reviewNone {
		t.Errorf(
			"R while a review launch is in flight must not change reviewMode, got %d",
			m3.reviewMode,
		)
	}
	if m3.reviewPicker != nil {
		t.Error("R while a review launch is in flight must not build a review picker")
	}
}

// --- selecting an item dispatches the launch ---

func TestReviewPickSelectedCallsLaunchReviewFn(t *testing.T) {
	launchCalled := false
	var gotRepo, gotName, gotKey string
	m := makeTestModel(testStatuses())
	m.launchReviewFn = func(repo, name, reviewerKey string) error {
		launchCalled = true
		gotRepo = repo
		gotName = name
		gotKey = reviewerKey
		return nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'R'})
	m3 := m2.(Model)

	m4, cmd := m3.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m5 := m4.(Model)

	if m5.reviewMode != reviewNone {
		t.Error(
			"selecting an item should leave reviewPick mode immediately, before the async launch resolves",
		)
	}
	if !m5.reviewLaunching {
		t.Error(
			"selecting an item should set reviewLaunching=true before the async launch resolves",
		)
	}
	if m5.status == "" {
		t.Error("selecting an item should set an in-progress status")
	}
	if cmd == nil {
		t.Fatal("expected a command after selecting an item")
	}

	msg := cmd()

	if !launchCalled {
		t.Fatal("expected launchReviewFn to be called")
	}
	if gotRepo != "repo-a" || gotName != "feature-a" || gotKey != "code" {
		t.Errorf(
			"launchReviewFn called with wrong args: repo=%q name=%q key=%q",
			gotRepo, gotName, gotKey,
		)
	}

	launched, ok := msg.(reviewLaunchedMsg)
	if !ok {
		t.Fatalf("expected reviewLaunchedMsg after a successful launchReviewFn, got %T", msg)
	}

	m6, cmd2 := m5.Update(launched)
	m7 := m6.(Model)
	if cmd2 != nil {
		t.Error("processing reviewLaunchedMsg should return no command")
	}
	if m7.reviewLaunching {
		t.Error("reviewLaunching should be cleared once reviewLaunchedMsg is processed")
	}
	if m7.status == "" {
		t.Error("expected a final status after reviewLaunchedMsg is processed")
	}
}

// --- esc cancels the picker ---

func TestReviewPickEscCancels(t *testing.T) {
	launchCalled := false
	m := makeTestModel(testStatuses())
	m.launchReviewFn = func(_, _, _ string) error {
		launchCalled = true
		return nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'R'})
	m3 := m2.(Model)
	if m3.reviewMode != reviewPick {
		t.Fatal("expected reviewPick mode before esc")
	}

	m4, cmd := m3.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m5 := m4.(Model)

	if m5.reviewMode != reviewNone {
		t.Error("esc should return reviewMode to reviewNone")
	}
	if m5.reviewPicker != nil {
		t.Error("esc should clear the review picker")
	}
	if launchCalled {
		t.Error("esc must not call launchReviewFn")
	}
	if cmd != nil {
		t.Error("esc should return no command")
	}
}

// --- launch failure ---

func TestReviewLaunchFailureSetsStatusAndClearsGuard(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.launchReviewFn = func(_, _, _ string) error {
		return fmt.Errorf("tmux window not found")
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'R'})
	m3 := m2.(Model)

	m4, cmd := m3.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m5 := m4.(Model)
	if !m5.reviewLaunching {
		t.Fatal("expected reviewLaunching=true immediately after dispatch")
	}
	if cmd == nil {
		t.Fatal("expected a command after selecting an item")
	}

	msg := cmd()
	launched, ok := msg.(reviewLaunchedMsg)
	if !ok {
		t.Fatalf("expected reviewLaunchedMsg on launch failure, got %T", msg)
	}
	if string(launched) != "review failed: tmux window not found" {
		t.Errorf("expected %q, got %q", "review failed: tmux window not found", string(launched))
	}

	m6, _ := m5.Update(launched)
	m7 := m6.(Model)
	if m7.reviewLaunching {
		t.Error("reviewLaunching should be cleared even after a failed launch")
	}
	if m7.status != "review failed: tmux window not found" {
		t.Errorf("expected final status to surface the failure, got %q", m7.status)
	}
}

var _ = worktree.ReviewerChoice{} // ensure package import stays used if choices type is referenced elsewhere later
