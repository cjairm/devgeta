package tuiworktree

// Tests for Step 7 of docs/plans/cycles/2026-09-28-ws-agents-section.md:
// "Folding sections" - a/w toggle folds, the last open section can't fold,
// and folding the section the cursor is in moves it to the other one.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func pressKey(m Model, r rune) Model {
	got, _ := m.handleKey(tea.KeyPressMsg{Code: r})
	next, ok := got.(Model)
	if !ok {
		panic("handleKey did not return a Model")
	}
	return next
}

func TestToggleAgentsFold_FoldsAndUnfolds(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	if m.agentsFolded {
		t.Fatalf("test setup: expected agents to start open")
	}

	m = pressKey(m, 'a')
	if !m.agentsFolded {
		t.Errorf("expected 'a' to fold the agents section")
	}

	m = pressKey(m, 'a')
	if m.agentsFolded {
		t.Errorf("expected a second 'a' to unfold the agents section")
	}
}

func TestToggleSpacesFold_FoldsAndUnfolds(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	if m.spacesFolded {
		t.Fatalf("test setup: expected spaces to start open")
	}

	m = pressKey(m, 'w')
	if !m.spacesFolded {
		t.Errorf("expected 'w' to fold the spaces section")
	}

	m = pressKey(m, 'w')
	if m.spacesFolded {
		t.Errorf("expected a second 'w' to unfold the spaces section")
	}
}

func TestToggleFold_FoldingTheFocusedSectionMovesCursorToTheOther(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.section = sectionAgents
	m.agentCursor = 0

	m = pressKey(m, 'a')

	if !m.agentsFolded {
		t.Fatalf("expected agents to be folded")
	}
	if m.section != sectionSpaces {
		t.Errorf(
			"expected the cursor to move to spaces once agents (its own section) folds, got %v",
			m.section,
		)
	}
}

func TestToggleFold_FoldingSpacesWhileFocusedMovesCursorToAgents(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.section = sectionSpaces

	m = pressKey(m, 'w')

	if !m.spacesFolded {
		t.Fatalf("expected spaces to be folded")
	}
	if m.section != sectionAgents {
		t.Errorf(
			"expected the cursor to move to agents once spaces (its own section) folds, got %v",
			m.section,
		)
	}
}

func TestToggleFold_FoldingAnUnfocusedSectionLeavesCursorInPlace(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.section = sectionSpaces
	indices := m.navigableIndices()
	m.cursor = indices[0]

	m = pressKey(m, 'a') // fold agents, cursor is in spaces already

	if !m.agentsFolded {
		t.Fatalf("expected agents to be folded")
	}
	if m.section != sectionSpaces || m.cursor != indices[0] {
		t.Errorf("expected the cursor to stay put, got section=%v cursor=%d", m.section, m.cursor)
	}
}

func TestToggleFold_LastOpenSectionRefusesToFold(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m = pressKey(m, 'a') // fold agents
	if !m.agentsFolded {
		t.Fatalf("test setup: expected agents to be folded")
	}

	m = pressKey(m, 'w') // try to also fold spaces - the last open section

	if m.spacesFolded {
		t.Errorf("expected 'w' to refuse folding the last open section, but spaces got folded")
	}
	if !m.agentsFolded {
		t.Errorf("expected agents to remain folded (the refusal must not undo it)")
	}
}

func TestToggleFold_LastOpenSectionRefusesToFold_OtherDirection(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m = pressKey(m, 'w') // fold spaces
	if !m.spacesFolded {
		t.Fatalf("test setup: expected spaces to be folded")
	}

	m = pressKey(m, 'a') // try to also fold agents - the last open section

	if m.agentsFolded {
		t.Errorf("expected 'a' to refuse folding the last open section, but agents got folded")
	}
}

// TestToggleFold_SavesViewState confirms Step 10 wires a/w into
// saveViewState, the same way h/l/z already do for repo folds.
func TestToggleFold_SavesViewState(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	var calls int
	m.setGlobalOptionFn = func(_, _ string) error {
		calls++
		return nil
	}

	m = pressKey(m, 'a') // fold agents
	if calls != 1 {
		t.Errorf("expected 'a' to save view state once, got %d calls", calls)
	}

	m = pressKey(m, 'a') // unfold agents again
	if calls != 2 {
		t.Errorf("expected a second 'a' to save view state once more, got %d calls", calls)
	}
}

func TestRenderLeft_FoldedAgentsBarShowsPerStateCounts(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "a"},
			agentPane("%1", "1", "claude", "claude", "blocked"),
		),
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "b"},
			agentPane("%2", "1", "claude", "claude", "blocked"),
		),
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "c"},
			agentPane("%3", "1", "opencode", "opencode", "idle"),
		),
	}
	m := makeTestModel(statuses)
	m.agentsFolded = true

	out := ansi.Strip(m.renderLeft(60))
	var barLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "AGENTS") {
			barLine = line
		}
	}
	if barLine == "" {
		t.Fatalf("expected a folded AGENTS bar line, got:\n%s", out)
	}
	if !strings.Contains(barLine, "3") {
		t.Errorf("expected the total count 3, got %q", barLine)
	}
	if !strings.Contains(barLine, "!") || !strings.Contains(barLine, "2") {
		t.Errorf("expected the blocked count (! 2), got %q", barLine)
	}
	if !strings.Contains(barLine, "◆") || !strings.Contains(barLine, "1") {
		t.Errorf("expected the done count (◆ 1), got %q", barLine)
	}
	// No error/working/idle agents exist - their glyphs must not appear.
	if strings.Contains(barLine, "✕") || strings.Contains(barLine, "●") ||
		strings.Contains(barLine, "○") {
		t.Errorf("expected only non-zero state glyphs, got %q", barLine)
	}
	// No two-line agent rows must render while folded.
	if strings.Count(out, "·") > 0 {
		t.Errorf("expected no agent row content while folded, got:\n%s", out)
	}
}

func TestRenderLeft_FoldedAgentsBarWithNoAgents(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.agentsFolded = true

	out := ansi.Strip(m.renderLeft(60))
	var barLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "AGENTS") {
			barLine = line
		}
	}
	if barLine == "" {
		t.Fatalf("expected a folded AGENTS bar even with zero agents, got:\n%s", out)
	}
	if !strings.Contains(barLine, "0") {
		t.Errorf("expected the total count 0, got %q", barLine)
	}
}

func TestRenderLeft_FoldedSpacesBarShowsRepoAndSessionCounts(t *testing.T) {
	m := makeTestModel(testStatuses()) // repo-a, repo-b
	m.sessions = testSessions()        // notes, scratch
	m.rebuildRows()
	m.spacesFolded = true

	out := ansi.Strip(m.renderLeft(60))
	var barLine string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "SPACES") {
			barLine = line
		}
	}
	if barLine == "" {
		t.Fatalf("expected a folded SPACES bar line, got:\n%s", out)
	}
	if !strings.Contains(barLine, "2 repos") {
		t.Errorf("expected \"2 repos\", got %q", barLine)
	}
	if !strings.Contains(barLine, "2 sessions") {
		t.Errorf("expected \"2 sessions\", got %q", barLine)
	}
	// No worktree/session content must render while folded.
	if strings.Contains(out, "feature-a") {
		t.Errorf("expected no space row content while folded, got:\n%s", out)
	}
}

// TestRenderLeft_FoldedBarFocusColor confirms the folded bar's color
// still tracks m.section, mirroring the open header's own focus-color rule.
func TestRenderLeft_FoldedBarFocusColor(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.agentsFolded = true
	m.section = sectionSpaces
	unfocused := m.renderLeft(60)

	m.section = sectionAgents
	focused := m.renderLeft(60)

	if unfocused == focused {
		t.Errorf("expected the folded bar's color to differ when focused vs not")
	}
}
