package tuiworktree

// Tests for Step 9 of docs/plans/cycles/2026-09-28-ws-agents-section.md
// (ADR-0057): the dashboard's own pane-move keys, read from tmux
// (tmux.RootPaneMoveKeys, wired via m.paneMoveKeys) rather than hard-coded.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	apptmux "github.com/cjairm/devgeta/internal/apps/tmux"
)

// withPaneMoveKeys sets m's pane-move keys directly, bypassing the real
// tmux.RootPaneMoveKeys call newModel would otherwise wire.
func withPaneMoveKeys(m Model, keys apptmux.PaneMoveKeys) Model {
	m.paneMoveKeys = keys
	return m
}

var testPaneMoveKeys = apptmux.PaneMoveKeys{
	Left: "ctrl+h", Down: "ctrl+j", Up: "ctrl+k", Right: "ctrl+l",
}

func selectPaneDirectionSpy(calls *[]string) func(string) error {
	return func(dir string) error {
		*calls = append(*calls, dir)
		return nil
	}
}

// pressPaneMoveKey runs key through handleKey and, if it returned a command
// (the edge hand-off's tea.Cmd), executes it too - handleKey alone only
// arms the command; running it is what actually invokes
// m.selectPaneDirectionFn, mirroring how bubbletea's own runtime would.
func pressPaneMoveKey(m Model, msg tea.KeyPressMsg) Model {
	got, cmd := m.handleKey(msg)
	next := got.(Model)
	if cmd != nil {
		cmd()
	}
	return next
}

func TestPaneMoveKey_DownFromSpacesGoesToAgentsWhenAvailable(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m = withPaneMoveKeys(m, testPaneMoveKeys)
	m.section = sectionSpaces
	var calls []string
	m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})

	if m.section != sectionAgents {
		t.Errorf("expected section to move to agents, got %v", m.section)
	}
	if len(calls) != 0 {
		t.Errorf("expected no edge hand-off, got %v", calls)
	}
}

func TestPaneMoveKey_DownFromSpacesIsEdgeWhenNoAgents(t *testing.T) {
	m := makeTestModel(testStatuses())
	m = withPaneMoveKeys(m, testPaneMoveKeys)
	var calls []string
	m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})

	if len(calls) != 1 || calls[0] != "D" {
		t.Errorf("expected an edge hand-off select-pane -D, got %v", calls)
	}
}

func TestPaneMoveKey_DownFromSpacesIsEdgeWhenAgentsFolded(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m = withPaneMoveKeys(m, testPaneMoveKeys)
	m.section = sectionSpaces
	m.agentsFolded = true
	var calls []string
	m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})

	if m.section != sectionSpaces {
		t.Errorf(
			"expected section to stay spaces (agents is folded, counts as absent), got %v",
			m.section,
		)
	}
	if len(calls) != 1 || calls[0] != "D" {
		t.Errorf("expected an edge hand-off select-pane -D, got %v", calls)
	}
}

func TestPaneMoveKey_UpFromAgentsGoesToSpaces(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m = withPaneMoveKeys(m, testPaneMoveKeys)
	m.section = sectionAgents
	var calls []string
	m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})

	if m.section != sectionSpaces {
		t.Errorf("expected section to move to spaces, got %v", m.section)
	}
	if len(calls) != 0 {
		t.Errorf("expected no edge hand-off, got %v", calls)
	}
}

func TestPaneMoveKey_UpFromAgentsIsEdgeWhenSpacesFolded(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m = withPaneMoveKeys(m, testPaneMoveKeys)
	m.section = sectionAgents
	m.spacesFolded = true
	var calls []string
	m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})

	if m.section != sectionAgents {
		t.Errorf(
			"expected section to stay agents (spaces is folded, counts as absent), got %v",
			m.section,
		)
	}
	if len(calls) != 1 || calls[0] != "U" {
		t.Errorf("expected an edge hand-off select-pane -U, got %v", calls)
	}
}

func TestPaneMoveKey_UpFromSpacesIsEdge(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m = withPaneMoveKeys(m, testPaneMoveKeys)
	m.section = sectionSpaces
	var calls []string
	m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})

	if len(calls) != 1 || calls[0] != "U" {
		t.Errorf("expected an edge hand-off select-pane -U, got %v", calls)
	}
}

func TestPaneMoveKey_DownFromAgentsIsEdge(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m = withPaneMoveKeys(m, testPaneMoveKeys)
	m.section = sectionAgents
	var calls []string
	m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})

	if len(calls) != 1 || calls[0] != "D" {
		t.Errorf("expected an edge hand-off select-pane -D, got %v", calls)
	}
}

func TestPaneMoveKey_RightEntersDiffWhenContentExists(t *testing.T) {
	m := makeTestModel(testStatuses())
	m = withPaneMoveKeys(m, testPaneMoveKeys)
	m.diffContent = "some diff"
	var calls []string
	m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})

	if !m.diffFocused {
		t.Errorf("expected right to focus the diff pane")
	}
	if len(calls) != 0 {
		t.Errorf("expected right from the list never to edge hand-off, got %v", calls)
	}
}

func TestPaneMoveKey_RightNoOpWithoutDiffContent(t *testing.T) {
	m := makeTestModel(testStatuses())
	m = withPaneMoveKeys(m, testPaneMoveKeys)
	m.diffContent = ""
	var calls []string
	m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl})

	if m.diffFocused {
		t.Errorf("expected no diff focus with no content to show")
	}
	if len(calls) != 0 {
		t.Errorf("expected right from the list never to edge hand-off, got %v", calls)
	}
}

func TestPaneMoveKey_LeftFromListIsAlwaysEdge(t *testing.T) {
	m := makeTestModel(testStatuses())
	m = withPaneMoveKeys(m, testPaneMoveKeys)
	var calls []string
	m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})

	if len(calls) != 1 || calls[0] != "L" {
		t.Errorf("expected an edge hand-off select-pane -L, got %v", calls)
	}
}

func TestPaneMoveKey_LeftFromDiffPaneReturnsToList(t *testing.T) {
	m := makeTestModel(testStatuses())
	m = withPaneMoveKeys(m, testPaneMoveKeys)
	m.diffContent = "some diff"
	m.diffFocused = true
	var calls []string
	m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})

	if m.diffFocused {
		t.Errorf("expected left from the diff pane to return to the list")
	}
	if len(calls) != 0 {
		t.Errorf("expected no edge hand-off, got %v", calls)
	}
}

func TestPaneMoveKey_DownUpRightFromDiffPaneAreAlwaysEdges(t *testing.T) {
	for _, tc := range []struct {
		key  rune
		want string
	}{
		{'j', "D"},
		{'k', "U"},
		{'l', "R"},
	} {
		m := makeTestModel(testStatuses())
		m = withPaneMoveKeys(m, testPaneMoveKeys)
		m.diffContent = "some diff"
		m.diffFocused = true
		var calls []string
		m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

		m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: tc.key, Mod: tea.ModCtrl})

		if !m.diffFocused {
			t.Errorf("key %q: expected to stay in the diff pane", tc.key)
		}
		if len(calls) != 1 || calls[0] != tc.want {
			t.Errorf("key %q: expected an edge hand-off select-pane -%s, got %v",
				tc.key, tc.want, calls)
		}
	}
}

func TestPaneMoveKey_NeverInterceptedWhileFilterActive(t *testing.T) {
	m := makeTestModel(testStatuses())
	m = withPaneMoveKeys(m, testPaneMoveKeys)
	m.filter.Active = true
	var calls []string
	m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})

	if len(calls) != 0 {
		t.Errorf(
			"expected ctrl+h while filtering to reach the input, never select-pane, got %v",
			calls,
		)
	}
}

func TestPaneMoveKey_UsesTheLoadedKeysNotAHardcodedDefault(t *testing.T) {
	m := makeTestModel(testStatuses())
	m = withPaneMoveKeys(m, apptmux.PaneMoveKeys{
		Left: "alt+h", Down: "alt+j", Up: "alt+k", Right: "alt+l",
	})
	var calls []string
	m.selectPaneDirectionFn = selectPaneDirectionSpy(&calls)

	// ctrl+h must NOT be treated as a move key when the loaded config bound
	// alt+h instead.
	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if len(calls) != 0 {
		t.Fatalf("expected ctrl+h to be inert when only alt+h is bound, got %v", calls)
	}

	m = pressPaneMoveKey(m, tea.KeyPressMsg{Code: 'h', Mod: tea.ModAlt})
	if len(calls) != 1 || calls[0] != "L" {
		t.Errorf("expected alt+h to trigger the edge hand-off, got %v", calls)
	}
}
