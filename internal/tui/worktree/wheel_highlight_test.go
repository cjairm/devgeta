package tuiworktree

// From the mockups: the wheel scrolls whichever section or pane is under the
// pointer, and a filter highlights the text each row matched on.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func wheel(t *testing.T, m Model, button tea.MouseButton, x, y int) Model {
	t.Helper()
	got, _ := m.Update(tea.MouseWheelMsg{X: x, Y: y, Button: button})
	return got.(Model)
}

func TestWheel_OverAgentsMovesTheAgentCursorAndStopsAtTheEnd(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	header, ok := m.agentsHeaderRow()
	if !ok {
		t.Fatal("test setup: expected an agents header")
	}

	for range 5 {
		m = wheel(t, m, tea.MouseWheelDown, 2, header+1)
	}
	if m.section != sectionAgents || m.agentCursor != len(m.agentRows)-1 {
		t.Errorf(
			"section=%v agentCursor=%d, want agents, stopped on the last agent",
			m.section,
			m.agentCursor,
		)
	}
	m = wheel(t, m, tea.MouseWheelUp, 2, header+1)
	if m.agentCursor != len(m.agentRows)-2 {
		t.Errorf("agentCursor = %d after wheel up, want %d", m.agentCursor, len(m.agentRows)-2)
	}
}

func TestWheel_OverSpacesStaysInSpaces(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.section = sectionAgents
	indices := m.navigableIndices()
	m.cursor = indices[0]

	for range 20 {
		m = wheel(t, m, tea.MouseWheelDown, 2, 1)
	}
	if m.section != sectionSpaces || m.cursor != indices[len(indices)-1] {
		t.Errorf("section=%v cursor=%d, want spaces, stopped on its last row %d",
			m.section, m.cursor, indices[len(indices)-1])
	}
}

func TestWheel_OverTheDiffScrollsIt(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.diffContent = strings.Repeat("line\n", 200)
	cursorBefore := m.cursor

	m = wheel(t, m, tea.MouseWheelDown, m.leftPaneWidth+5, 5)
	if m.diffScroll != wheelDiffLines {
		t.Errorf("diffScroll = %d, want %d", m.diffScroll, wheelDiffLines)
	}
	if m.cursor != cursorBefore {
		t.Errorf("wheel over the diff moved the list cursor")
	}
	for range 5 {
		m = wheel(t, m, tea.MouseWheelUp, m.leftPaneWidth+5, 5)
	}
	if m.diffScroll != 0 {
		t.Errorf("diffScroll = %d, want clamped at 0", m.diffScroll)
	}
}

func TestFilter_HighlightsTheMatchInSpaceAndAgentRows(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.filter.InsertText("agents")
	m.rebuildRows()
	mark := m.palette.Match.Render("agents")

	out := m.renderLeft(60)
	if n := strings.Count(out, mark); n < 3 {
		t.Errorf(
			"found %d highlighted matches, want the worktree row and both agent rows:\n%q",
			n,
			out,
		)
	}

	m.agentsFolded = true
	m.rebuildRows()
	for _, r := range m.agentRows {
		if strings.Contains(m.renderAgentRow(r, 60, false), mark) {
			t.Errorf("a folded section isn't searched, so nothing in it may be highlighted")
		}
	}
}
