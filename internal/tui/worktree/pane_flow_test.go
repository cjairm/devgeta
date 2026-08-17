package tuiworktree

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// paneQualifyingStatuses returns a single worktree with 2 stateful panes -
// the minimum that makes buildRows emit rowPane children (see
// qualifiesForPaneRows) - with Session/Window/PaneID populated the way a real
// PaneStates() scan would, so handleSwitchToPane has real values to pass
// through switchToPaneFn.
func paneQualifyingStatuses() []worktree.WorktreeStatus {
	return []worktree.WorktreeStatus{
		{
			Name:       "feature-a",
			Repo:       "repo-a",
			TmuxWindow: "wt-feature-a",
			Panes: []tmux.PaneState{
				{
					Session:        "repo-a",
					Window:         "wt-feature-a",
					PaneID:         "%1",
					PaneIndex:      "0",
					CurrentCommand: "claude",
					State:          worktree.AgentStateBusy,
				},
				{
					Session:        "repo-a",
					Window:         "wt-feature-a",
					PaneID:         "%2",
					PaneIndex:      "1",
					CurrentCommand: "zsh",
					State:          worktree.AgentStateIdle,
				},
			},
		},
	}
}

// cursorToPane points m.cursor at the rowPane row whose PaneID matches id.
func cursorToPane(m *Model, id string) {
	m.rebuildRows()
	for i, r := range m.rows {
		if r.kind == rowPane && r.pane.PaneID == id {
			m.cursor = i
			return
		}
	}
}

// --- selectedPane ---

func TestSelectedPaneOnPaneRow(t *testing.T) {
	m := makeTestModel(paneQualifyingStatuses())
	cursorToPane(&m, "%2")

	sel, ok := m.selectedPane()
	if !ok {
		t.Fatal("expected selectedPane to return ok=true on a rowPane row")
	}
	if sel.PaneID != "%2" {
		t.Errorf("expected pane %%2, got %q", sel.PaneID)
	}
}

func TestSelectedPaneOnWorktreeRowIsFalse(t *testing.T) {
	m := makeTestModel(testStatuses()) // cursor starts on repo-a/feature-a, a rowWorktree
	if _, ok := m.selectedPane(); ok {
		t.Error("expected selectedPane to return ok=false on a rowWorktree row")
	}
}

// --- enter on a pane row (switch) ---

func TestEnterOnPaneRowSwitchesAndQuits(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := makeTestModel(paneQualifyingStatuses())
	cursorToPane(&m, "%2")

	var gotSession, gotWindow, gotPaneID string
	m.switchToPaneFn = func(session, window, paneID string) error {
		gotSession, gotWindow, gotPaneID = session, window, paneID
		return nil
	}
	var clearedPaneID string
	m.clearAgentStateForPaneFn = func(paneID string) error {
		clearedPaneID = paneID
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = m2.(Model)
	if cmd == nil {
		t.Fatal("expected a command from enter on a pane row")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg on successful switch, got %T: %+v", msg, msg)
	}
	if gotSession != "repo-a" || gotWindow != "wt-feature-a" || gotPaneID != "%2" {
		t.Errorf(
			"expected switchToPaneFn called with (repo-a, wt-feature-a, %%2), got (%q, %q, %q)",
			gotSession, gotWindow, gotPaneID,
		)
	}
	if clearedPaneID != "%2" {
		t.Errorf("expected clearAgentStateForPaneFn called with %%2, got %q", clearedPaneID)
	}
}

func TestEnterOnPaneRowClearFailureStillSwitchesAndQuits(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := makeTestModel(paneQualifyingStatuses())
	cursorToPane(&m, "%2")

	m.clearAgentStateForPaneFn = func(_ string) error {
		return fmt.Errorf("no such option")
	}
	switched := false
	m.switchToPaneFn = func(_, _, _ string) error {
		switched = true
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = m2.(Model)
	if cmd == nil {
		t.Fatal("expected a command from enter on a pane row")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf(
			"a failed best-effort state clear must not stop the switch, got %T: %+v",
			msg,
			msg,
		)
	}
	if !switched {
		t.Error("expected switchToPaneFn to still be called after a clear failure")
	}
}

func TestEnterOnPaneRowSwitchFailureShowsStatusAndDoesNotQuit(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := makeTestModel(paneQualifyingStatuses())
	cursorToPane(&m, "%2")

	m.switchToPaneFn = func(_, _, _ string) error {
		return fmt.Errorf("no such pane")
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = m2.(Model)
	if cmd == nil {
		t.Fatal("expected a command from enter on a pane row")
	}
	msg := cmd()
	m3, _ := m2.(Model).Update(msg)
	m4 := m3.(Model)
	if _, ok := msg.(tea.QuitMsg); ok {
		t.Error("a failed switch must not quit the TUI")
	}
	if !strings.Contains(m4.status, "switch failed") {
		t.Errorf("expected a 'switch failed' status, got %q", m4.status)
	}
}

// --- d/D/r/R stay inert on a pane row (selectedStatus deliberately reports
// ok=false there; only selectedPath resolves through to the parent for diff
// bookkeeping) ---

func TestDeleteRepairSessionDeleteKickReviewInertOnPaneRow(t *testing.T) {
	m := makeTestModel(paneQualifyingStatuses())
	cursorToPane(&m, "%2")

	removeCalled := false
	m.removeFn = func(_, _ string, _ bool) error {
		removeCalled = true
		return nil
	}
	removeSessionCalled := false
	m.removeSessionFn = func(_, _ string) error {
		removeSessionCalled = true
		return nil
	}
	repairCalled := false
	m.repairFn = func(_, _ string, _ worktree.Layout) error {
		repairCalled = true
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: 'd'})
	m3 := m2.(Model)
	if cmd != nil {
		t.Error("d on a pane row should return no command")
	}
	if removeCalled {
		t.Error("d on a pane row must not call removeFn")
	}
	if m3.pendingDelete != "" {
		t.Error("d on a pane row must not arm pendingDelete")
	}

	m4, cmd2 := m3.Update(tea.KeyPressMsg{Code: 'D'})
	m5 := m4.(Model)
	if cmd2 != nil {
		t.Error("D on a pane row should return no command")
	}
	if removeSessionCalled {
		t.Error("D on a pane row must not call removeSessionFn")
	}
	if m5.pendingSessionDelete != "" {
		t.Error("D on a pane row must not arm pendingSessionDelete")
	}

	m6, cmd3 := m5.Update(tea.KeyPressMsg{Code: 'r'})
	m7 := m6.(Model)
	if cmd3 != nil {
		t.Error("r on a pane row should return no command")
	}
	if repairCalled {
		t.Error("r on a pane row must not call repairFn")
	}

	m8, cmd4 := m7.Update(tea.KeyPressMsg{Code: 'R'})
	m9 := m8.(Model)
	if cmd4 != nil {
		t.Error("R on a pane row should return no command")
	}
	if m9.reviewMode != reviewNone {
		t.Error("R on a pane row must not open the kick-a-review picker")
	}
}

func TestEnterOnPaneRowOutsideTmuxShowsGuardMessage(t *testing.T) {
	t.Setenv("TMUX", "")
	m := makeTestModel(paneQualifyingStatuses())
	cursorToPane(&m, "%2")

	switchCalled := false
	m.switchToPaneFn = func(_, _, _ string) error {
		switchCalled = true
		return nil
	}
	clearCalled := false
	m.clearAgentStateForPaneFn = func(_ string) error {
		clearCalled = true
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if cmd != nil {
		t.Error("enter outside tmux on a pane row should return no command")
	}
	if switchCalled || clearCalled {
		t.Error("switchToPaneFn and clearAgentStateForPaneFn must not be called outside tmux")
	}
	if !strings.Contains(m3.status, "not inside tmux") {
		t.Errorf(
			"expected the same not-inside-tmux guard message as handleAttach, got %q",
			m3.status,
		)
	}
}
