package tuiworktree

// The cursor never sits in a section you can't see (ADR-0056, ADR-0057):
// walking stops at a folded section, and any fold, load or rebuild leaves it
// in an open one. Before this, up from the first agent with spaces folded
// moved an invisible cursor through the folded spaces list.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestMoveCursor_UpFromFirstAgentStopsWhenSpacesFolded(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.spacesFolded = true
	m.section = sectionAgents
	m.agentCursor = 0

	for range 5 {
		got, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
		m = got.(Model)
	}
	if m.section != sectionAgents || m.agentCursor != 0 {
		t.Fatalf(
			"section=%v agentCursor=%d, want to stay on the first agent",
			m.section,
			m.agentCursor,
		)
	}

	got, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	m = got.(Model)
	if m.agentCursor != 1 {
		t.Errorf(
			"agentCursor = %d after one down, want 1 (no hidden presses to undo)",
			m.agentCursor,
		)
	}
}

func TestMoveCursor_DownFromLastSpaceStopsWhenAgentsFolded(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.agentsFolded = true
	indices := m.navigableIndices()
	m.cursor = indices[len(indices)-1]

	got, _ := m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	m = got.(Model)
	if m.section != sectionSpaces || m.cursor != indices[len(indices)-1] {
		t.Errorf("section=%v cursor=%d, want to stay on the last space", m.section, m.cursor)
	}
}

// The saved state can land before the first scan has any agent rows; the
// cursor must still end up in agents once they arrive.
func TestSettleSection_SpacesFoldedBeforeAgentsArrive(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	statuses := m.statuses
	m.statuses = statuses[:2] // no agents yet
	m.rebuildRows()

	got, _ := m.Update(viewStateMsg{ok: true, state: viewStateV1{V: 1, SpacesFolded: true}})
	m = got.(Model)
	if m.section != sectionSpaces {
		t.Fatalf("with no agents the cursor has nowhere else to be, got section=%v", m.section)
	}

	m.statuses = statuses
	m.rebuildRows()
	if m.section != sectionAgents {
		t.Errorf("section = %v once agents arrive, want agents (spaces is folded)", m.section)
	}
}

func TestPlaceCursorOnActive_LandsOnTheAgentYouCameFrom(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	for i := range m.statuses[2].Panes {
		m.statuses[2].Panes[i].Session = "devgeta"
		m.statuses[2].Panes[i].Window = "wt-agents-host"
	}
	m.statuses[2].Panes[0].State = "blocked" // sorts first, so the match isn't row 0 by luck
	m.statuses[2].Panes[1].Window = "other"
	m.rebuildRows()
	m.currentSessionFn = func() (string, bool) { return "devgeta", true }
	m.originWindowFn = func() (string, bool) { return "other", true }
	m.loaded = true
	m.sessionsLoaded = true

	m.placeCursorOnActive()

	if got := m.agentRows[m.agentCursor].pane.PaneID; got != "%2" {
		t.Errorf("agent cursor on %s, want %%2 (the agent in the window you came from)", got)
	}
}
