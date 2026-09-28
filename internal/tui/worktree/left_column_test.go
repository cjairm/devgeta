package tuiworktree

// Tests for the left column's shape in the mockups
// (docs/plans/cycles/2026-09-28-ws-agents-section.md, ADR-0056): a header
// per open section, a rule between them, a folded section as a bar on the
// column's last line, and the right pane following the agents cursor.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/cjairm/devgeta/internal/apps/tmux"
)

func leftLines(m Model) []string {
	return strings.Split(ansi.Strip(m.renderLeft(40)), "\n")
}

func TestRenderLeft_BothOpenHasHeadersAndRule(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	lines := leftLines(m)

	if !strings.HasPrefix(lines[0], " SPACES") {
		t.Fatalf("line 0 = %q, want the SPACES header", lines[0])
	}
	header, ok := m.agentsHeaderRow()
	if !ok {
		t.Fatalf("expected an AGENTS header row")
	}
	if !strings.HasPrefix(lines[header-1], "────") {
		t.Errorf("line %d = %q, want the rule above AGENTS", header-1, lines[header-1])
	}
	if !strings.HasPrefix(lines[header], " AGENTS") {
		t.Errorf("line %d = %q, want the AGENTS header (agentsHeaderRow)", header, lines[header])
	}
}

func TestRenderLeft_NoAgentsIsOnePlainList(t *testing.T) {
	m := makeTestModel(nil)
	m.statuses = twoWorktreeTwoAgentsModel(t).statuses[:2]
	m.rebuildRows()
	out := ansi.Strip(m.renderLeft(40))

	for _, chrome := range []string{"SPACES", "AGENTS", "────"} {
		if strings.Contains(out, chrome) {
			t.Errorf("with no agents the column has no %q, got:\n%s", chrome, out)
		}
	}
}

// Agent rows are two lines each; the bar must still land on the column's
// last line when the open section above it is agents.
func TestRenderLeft_FoldedBarIsTheLastLine(t *testing.T) {
	for _, tc := range []struct {
		name         string
		agentsFolded bool
		wantBar      string
		wantHeader   string
	}{
		{"agents folded", true, "▸ AGENTS", " SPACES"},
		{"spaces folded", false, "▸ SPACES", " AGENTS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := twoWorktreeTwoAgentsModel(t)
			m.height = 20
			m.agentsFolded = tc.agentsFolded
			m.spacesFolded = !tc.agentsFolded
			lines := leftLines(m)

			if len(lines) != m.height-2 {
				t.Fatalf("got %d lines, want the full column (%d)", len(lines), m.height-2)
			}
			if !strings.Contains(lines[len(lines)-1], tc.wantBar) {
				t.Errorf("last line = %q, want the %q bar", lines[len(lines)-1], tc.wantBar)
			}
			if !strings.HasPrefix(lines[0], tc.wantHeader) {
				t.Errorf("line 0 = %q, want the open section's %q header", lines[0], tc.wantHeader)
			}
		})
	}
}

func TestRenderLeft_AgentsFoldedBarCountsEachState(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.statuses[2].Panes[0].State = "blocked"
	m.rebuildRows()
	m.agentsFolded = true
	lines := leftLines(m)
	bar := lines[len(lines)-1]

	for _, want := range []string{"AGENTS  2", "!1", "●1", "a"} {
		if !strings.Contains(bar, want) {
			t.Errorf("bar = %q, want it to contain %q", bar, want)
		}
	}
}

func TestRenderSectionHeader_ShowsFilteredCount(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	if got := ansi.Strip(m.renderSectionHeader("AGENTS", 1, 4, false, 40)); !strings.HasSuffix(
		got, "1 of 4",
	) {
		t.Errorf("header = %q, want it to end in \"1 of 4\"", got)
	}
	if got := ansi.Strip(m.renderSectionHeader("AGENTS", 4, 4, false, 40)); !strings.HasSuffix(
		got, " 4",
	) || strings.Contains(got, "of") {
		t.Errorf("header = %q, want a bare count when nothing is filtered out", got)
	}
}

// The agents cursor, not the spaces cursor, decides the diff: here the
// spaces cursor sits on a plain worktree while the agent's worktree is the
// one whose diff is loaded.
func TestRenderRight_AgentRowShowsItsWorktreeDiff(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	for i := range m.statuses {
		m.statuses[i].Path = "/wt/" + m.statuses[i].Name
	}
	m.rebuildRows()
	m.section = sectionAgents
	m.agentCursor = 0
	m.diffPath = "/wt/agents-host"
	m.diffContent = "AGENT-DIFF"

	if got := ansi.Strip(m.renderRight(60)); !strings.Contains(got, "AGENT-DIFF") {
		t.Errorf("right pane = %q, want the agent's worktree diff", got)
	}
}

func TestRenderRight_AgentOutsideAWorktreeShowsNothing(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.agentRows[0].isWorktree = false
	m.section = sectionAgents
	m.agentCursor = 0
	m.diffContent = "STALE-DIFF"

	if got := m.renderRight(60); got != "" {
		t.Errorf("right pane = %q, want nothing for an agent with no worktree", got)
	}
}

func TestRenderHint_AgentsSectionShowsTheTmuxMoveKeys(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.paneMoveKeys = tmux.PaneMoveKeys{Left: "ctrl+h", Down: "ctrl+j", Up: "ctrl+k", Right: "M-l"}
	m.section = sectionAgents

	got := ansi.Strip(m.renderHint(120))
	for _, want := range []string{"^k spaces", "M-l diff", "a fold agents", "w fold spaces"} {
		if !strings.Contains(got, want) {
			t.Errorf("hint = %q, want it to contain %q", got, want)
		}
	}

	m.spacesFolded = true
	got = ansi.Strip(m.renderHint(120))
	if strings.Contains(got, "^k") {
		t.Errorf("hint = %q, want no up-to-spaces key while spaces is folded", got)
	}
	if strings.Contains(got, "fold agents") {
		t.Errorf("hint = %q, want no fold-agents key while agents is the last open section", got)
	}
}
