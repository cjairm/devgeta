package tuiworktree

// Tests for Step 8 of docs/plans/cycles/2026-09-28-ws-agents-section.md:
// "Split and scroll" - +/- keys and dragging the AGENTS header line adjust
// the height split between spaces and agents (ADR-0056), reusing the
// vertical divider's "write the saved state on release only" pattern
// (ADR-0050).

import (
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// splitTestModel builds a model where both sections overflow the column -
// 30 worktrees and 12 agents in 30 lines - since that is the only case the
// split decides anything (a section that fits gives its slack to the other),
// so +/- and drag have visible room to move within.
func splitTestModel(t *testing.T) Model {
	t.Helper()
	var statuses []worktree.WorktreeStatus
	for i := range 30 {
		statuses = append(statuses, worktree.WorktreeStatus{
			Repo: "devgeta", Name: "wt-" + strconv.Itoa(i),
		})
	}
	agentHost := worktree.WorktreeStatus{Repo: "devgeta", Name: "agents-host"}
	for i := range 12 {
		agentHost = withAgentPane(agentHost, agentPane(
			"%"+strconv.Itoa(i+1), strconv.Itoa(i+1), "claude", "claude", "busy",
		))
	}
	statuses = append(statuses, agentHost)
	m := makeTestModel(statuses)
	m.height = 30
	if len(m.agentRows) != 12 {
		t.Fatalf("test setup: expected 12 agent rows, got %d", len(m.agentRows))
	}
	return m
}

func TestComputeLeftLayout_ShortSpacesGiveAgentsTheRest(t *testing.T) {
	m := splitTestModel(t)
	m.statuses = m.statuses[len(m.statuses)-1:] // only the agents' own worktree
	m.rebuildRows()
	for _, r := range m.rows { // fold its 12 pane rows, leaving repo + worktree
		if r.kind == rowWorktree {
			m.collapsed[rowKey(r)] = true
		}
	}
	m.rebuildRows()
	if len(m.rows) != 2 {
		t.Fatalf("test setup: expected 2 spaces rows, got %d", len(m.rows))
	}

	layout := m.computeLeftLayout()
	if layout.spacesHeight != len(m.rows) {
		t.Errorf("spacesHeight = %d, want every spaces row (%d)", layout.spacesHeight, len(m.rows))
	}
	body := m.splitBodyLines()
	if want := (body - len(m.rows)) / 2; layout.agentsVisible != want {
		t.Errorf("agentsVisible = %d, want the rest of the column (%d)", layout.agentsVisible, want)
	}
}

func TestComputeLeftLayout_ShortAgentsGiveSpacesTheRest(t *testing.T) {
	m := splitTestModel(t)
	m.statuses[len(m.statuses)-1].Panes = m.statuses[len(m.statuses)-1].Panes[:1]
	m.rebuildRows()

	layout := m.computeLeftLayout()
	if layout.agentsVisible != 1 {
		t.Errorf("agentsVisible = %d, want the one agent", layout.agentsVisible)
	}
	if want := m.splitBodyLines() - 2; layout.spacesHeight != want {
		t.Errorf("spacesHeight = %d, want the rest of the column (%d)", layout.spacesHeight, want)
	}
}

func TestAdjustSplit_PlusIncreasesVisibleAgentRows(t *testing.T) {
	m := splitTestModel(t)
	before := m.computeLeftLayout().agentsVisible

	got, _ := m.handleKey(tea.KeyPressMsg{Code: '+'})
	m = got.(Model)

	after := m.computeLeftLayout().agentsVisible
	if after != before+1 {
		t.Errorf("expected agentsVisible to grow by 1 (from %d), got %d", before, after)
	}
}

func TestAdjustSplit_MinusDecreasesVisibleAgentRows(t *testing.T) {
	m := splitTestModel(t)
	// Start from a known split so - has room to decrease.
	m.split = 3
	before := m.computeLeftLayout().agentsVisible

	got, _ := m.handleKey(tea.KeyPressMsg{Code: '-'})
	m = got.(Model)

	after := m.computeLeftLayout().agentsVisible
	if after != before-1 {
		t.Errorf("expected agentsVisible to shrink by 1 (from %d), got %d", before, after)
	}
}

func TestAdjustSplit_ClampsAtTheBoundLeavingSpacesOneRow(t *testing.T) {
	m := splitTestModel(t)
	bound := m.maxSplit()

	for range bound + 10 {
		got, _ := m.handleKey(tea.KeyPressMsg{Code: '+'})
		m = got.(Model)
	}

	layout := m.computeLeftLayout()
	if layout.agentsVisible > bound {
		t.Errorf("agentsVisible = %d, want clamped at bound %d", layout.agentsVisible, bound)
	}
	if layout.spacesHeight < 1 {
		t.Errorf("expected spaces to keep at least 1 row, got spacesHeight=%d", layout.spacesHeight)
	}
}

func TestAdjustSplit_ClampsAtMinimumOne(t *testing.T) {
	m := splitTestModel(t)

	for range 20 {
		got, _ := m.handleKey(tea.KeyPressMsg{Code: '-'})
		m = got.(Model)
	}

	layout := m.computeLeftLayout()
	if layout.agentsVisible < 1 {
		t.Errorf(
			"expected agents to keep at least 1 row, got agentsVisible=%d",
			layout.agentsVisible,
		)
	}
}

func TestAdjustSplit_NoOpWhenASectionIsFolded(t *testing.T) {
	m := splitTestModel(t)
	m.agentsFolded = true
	beforeSplit := m.split

	got, _ := m.handleKey(tea.KeyPressMsg{Code: '+'})
	m = got.(Model)

	if m.split != beforeSplit {
		t.Errorf("expected + to be a no-op while agents is folded, split changed to %d", m.split)
	}
}

func TestAgentsHeaderRow_OkWhenBothSectionsOpen(t *testing.T) {
	m := splitTestModel(t)
	_, ok := m.agentsHeaderRow()
	if !ok {
		t.Errorf("expected agentsHeaderRow to report ok=true when both sections are open")
	}
}

func TestAgentsHeaderRow_NotOkWhenFolded(t *testing.T) {
	m := splitTestModel(t)
	m.agentsFolded = true
	if _, ok := m.agentsHeaderRow(); ok {
		t.Errorf("expected agentsHeaderRow to report ok=false while agents is folded")
	}
	m.agentsFolded = false
	m.spacesFolded = true
	if _, ok := m.agentsHeaderRow(); ok {
		t.Errorf("expected agentsHeaderRow to report ok=false while spaces is folded")
	}
}

func TestSplitDrag_ClickAtHeaderRowStartsDragging(t *testing.T) {
	m := splitTestModel(t)
	row, ok := m.agentsHeaderRow()
	if !ok {
		t.Fatalf("test setup: expected a draggable header row")
	}

	got, _ := m.Update(tea.MouseClickMsg{X: 0, Y: row, Button: tea.MouseLeft})
	m = got.(Model)

	if !m.splitDragging {
		t.Errorf("expected a click on the header row to start the split drag")
	}
}

func TestSplitDrag_ClickElsewhereDoesNotStartDragging(t *testing.T) {
	m := splitTestModel(t)
	row, ok := m.agentsHeaderRow()
	if !ok {
		t.Fatalf("test setup: expected a draggable header row")
	}

	got, _ := m.Update(tea.MouseClickMsg{X: 0, Y: row + 3, Button: tea.MouseLeft})
	m = got.(Model)

	if m.splitDragging {
		t.Errorf("expected a click away from the header row not to start the split drag")
	}
}

func TestSplitDrag_MotionUpwardIncreasesAgentRows(t *testing.T) {
	m := splitTestModel(t)
	row, _ := m.agentsHeaderRow()
	before := m.computeLeftLayout().agentsVisible

	got, _ := m.Update(tea.MouseClickMsg{X: 0, Y: row, Button: tea.MouseLeft})
	m = got.(Model)
	got, _ = m.Update(tea.MouseMotionMsg{X: 0, Y: row - 2}) // dragging the header UP
	m = got.(Model)

	after := m.computeLeftLayout().agentsVisible
	if after <= before {
		t.Errorf("expected dragging the header up to increase agentsVisible (was %d), got %d",
			before, after)
	}
}

func TestSplitDrag_SavesViewStateOnceOnReleaseNotDuringMotion(t *testing.T) {
	m := splitTestModel(t)
	row, _ := m.agentsHeaderRow()
	var calls int
	m.setGlobalOptionFn = func(_, _ string) error {
		calls++
		return nil
	}

	got, _ := m.Update(tea.MouseClickMsg{X: 0, Y: row, Button: tea.MouseLeft})
	m = got.(Model)
	got, _ = m.Update(tea.MouseMotionMsg{X: 0, Y: row - 1})
	m = got.(Model)
	got, _ = m.Update(tea.MouseMotionMsg{X: 0, Y: row - 2})
	m = got.(Model)
	if calls != 0 {
		t.Fatalf("expected no save while the drag is still in motion, got %d calls", calls)
	}

	got, _ = m.Update(tea.MouseReleaseMsg{})
	m = got.(Model)
	if calls != 1 {
		t.Errorf("expected exactly one save when the drag ends, got %d calls", calls)
	}

	got, _ = m.Update(tea.MouseReleaseMsg{})
	m = got.(Model)
	if calls != 1 {
		t.Errorf("expected a release with no active drag not to save again, got %d calls", calls)
	}
}
