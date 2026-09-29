package tuiworktree

// d on an agent row closes that agent's pane, with the same two-press
// confirm every other destructive d uses. Before this, d in the agents
// section did nothing: it only ever looked at the spaces cursor.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func agentsCursorModel(t *testing.T) Model {
	t.Helper()
	m := twoWorktreeTwoAgentsModel(t)
	m.section = sectionAgents
	m.agentCursor = 1
	return m
}

func pressD(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	got, cmd := m.handleKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	return got.(Model), cmd
}

func TestCloseAgent_SecondDClosesOnlyThatPane(t *testing.T) {
	m := agentsCursorModel(t)
	var killed []string
	m.killPaneFn = func(id string) error {
		killed = append(killed, id)
		return nil
	}
	m.killSessionFn = func(string) error {
		t.Fatal("d on an agent row must never kill a session")
		return nil
	}
	m.removeFn = func(_, _ string, _ bool) error {
		t.Fatal("d on an agent row must never delete a worktree")
		return nil
	}
	want := m.agentRows[1].pane.PaneID

	m, cmd := pressD(t, m)
	if cmd != nil || m.pendingCloseAgent != want {
		t.Fatalf(
			"first d: pending=%q cmd=%v, want armed on %s with no command",
			m.pendingCloseAgent,
			cmd,
			want,
		)
	}
	if hint := ansi.Strip(m.renderHint(120)); !strings.Contains(hint, "press d again to close") {
		t.Errorf("armed hint = %q", hint)
	}

	m, cmd = pressD(t, m)
	if cmd == nil {
		t.Fatal("second d returned no command")
	}
	if _, ok := cmd().(agentPaneClosedMsg); !ok {
		t.Errorf("close command did not report the pane closed")
	}
	if len(killed) != 1 || killed[0] != want {
		t.Errorf("killed %v, want only %s", killed, want)
	}
}

func TestCloseAgent_AnyOtherKeyDisarms(t *testing.T) {
	m := agentsCursorModel(t)
	m.killPaneFn = func(string) error {
		t.Fatal("no pane may close after the confirm was cancelled")
		return nil
	}

	m, _ = pressD(t, m)
	got, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	m = got.(Model)
	if m.pendingCloseAgent != "" {
		t.Fatalf("pendingCloseAgent = %q after another key, want disarmed", m.pendingCloseAgent)
	}
	m, _ = pressD(t, m) // arms again rather than confirming
	if m.pendingCloseAgent == "" {
		t.Errorf("d after a cancel should arm again")
	}
}

func TestCloseAgent_ClosedMessageRescans(t *testing.T) {
	m := agentsCursorModel(t)
	before := m.sessionGen

	got, cmd := m.Update(agentPaneClosedMsg{label: "devgeta/agents-host"})
	m = got.(Model)
	if cmd == nil || m.sessionGen != before+1 {
		t.Errorf(
			"sessionGen %d -> %d, cmd=%v; want a bump and an immediate rescan",
			before,
			m.sessionGen,
			cmd,
		)
	}
	if !strings.Contains(m.status, "devgeta/agents-host") {
		t.Errorf("status = %q, want it to name the closed agent", m.status)
	}
}
