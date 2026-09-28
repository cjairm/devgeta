package tuiworktree

// Tests for Step 5 of docs/plans/cycles/2026-09-28-ws-agents-section.md:
// "Two sections on screen" — the agents section joins spaces on the model's
// single left column, with its own cursor and rebuild-time relocation
// (mirroring rebuildRows' existing rowKey relocation for spaces rows).

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

func withAgentPane(s worktree.WorktreeStatus, p tmux.PaneState) worktree.WorktreeStatus {
	s.Panes = append(s.Panes, p)
	return s
}

func TestRebuildRows_PopulatesAgentRows(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "fix-stale-notify"},
			agentPane("%1", "1", "claude", "claude", "blocked"),
		),
	}
	m := makeTestModel(statuses)

	if len(m.agentRows) != 1 {
		t.Fatalf("expected 1 agent row, got %d: %+v", len(m.agentRows), m.agentRows)
	}
	if m.agentRows[0].pane.PaneID != "%1" {
		t.Errorf("agent row pane id = %q, want %%1", m.agentRows[0].pane.PaneID)
	}
}

func TestRebuildRows_RelocatesAgentCursorByPaneID(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		withAgentPane(
			worktree.WorktreeStatus{Repo: "zzz-last", Name: "wt"},
			agentPane("%target", "1", "claude", "claude", "busy"),
		),
	}
	m := makeTestModel(statuses)
	m.section = sectionAgents
	m.agentCursor = 0
	if m.agentRows[m.agentCursor].pane.PaneID != "%target" {
		t.Fatalf("test setup: expected cursor on %%target before rebuild")
	}

	// Add a repo that sorts BEFORE "zzz-last" and is more urgent (blocked),
	// so %target's position in the sorted agent list shifts from 0 to 1.
	m.statuses = append(m.statuses, withAgentPane(
		worktree.WorktreeStatus{Repo: "aaa-first", Name: "wt"},
		agentPane("%other", "1", "claude", "claude", "blocked"),
	))
	m.rebuildRows()

	if len(m.agentRows) != 2 {
		t.Fatalf("expected 2 agent rows after rebuild, got %d", len(m.agentRows))
	}
	if m.agentRows[m.agentCursor].pane.PaneID != "%target" {
		t.Errorf(
			"agentCursor (%d) should still point at %%target after rebuild shifted its "+
				"position, got pane %q",
			m.agentCursor, m.agentRows[m.agentCursor].pane.PaneID,
		)
	}
}

func TestRebuildRows_ClampsAgentCursorWhenAgentDisappears(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "a"},
			agentPane("%1", "1", "claude", "claude", "blocked"),
		),
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "b"},
			agentPane("%2", "1", "claude", "claude", "error"),
		),
	}
	m := makeTestModel(statuses)
	m.section = sectionAgents
	m.agentCursor = 1 // on %2, the second (last) row

	// %2's coder exits: its pane no longer qualifies as an agent.
	m.statuses[1].Panes[0] = agentPane("%2", "1", "", "zsh", "")
	m.rebuildRows()

	if len(m.agentRows) != 1 {
		t.Fatalf("expected 1 agent row after %%2 dropped out, got %d", len(m.agentRows))
	}
	if m.agentCursor != 0 {
		t.Errorf("agentCursor = %d, want clamped to 0 (the only remaining row)", m.agentCursor)
	}
}

// TestSelectors_GatedToSpacesSection confirms selectedStatus/selectedSession/
// selectedSessionName all report ok=false while the cursor is logically in
// the agents section, even though m.cursor (frozen at its last spaces
// position) still points at a perfectly valid rowWorktree/rowSession row -
// otherwise a mutating key like d/D/$/r/R would act on that stale, invisible
// spaces row instead of being inert, as the section split requires.
func TestSelectors_GatedToSpacesSection(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "a"},
			agentPane("%1", "1", "claude", "claude", "blocked"),
		),
	}
	m := makeTestModel(statuses)
	// Sanity: before entering agents, the cursor really does sit on a
	// selectable worktree row.
	if _, ok := m.selectedStatus(); !ok {
		t.Fatalf("test setup: expected a selected worktree row in spaces")
	}

	m.section = sectionAgents
	m.agentCursor = 0

	if _, ok := m.selectedStatus(); ok {
		t.Errorf("selectedStatus() = ok while in agents section, want false")
	}
	if _, ok := m.selectedSession(); ok {
		t.Errorf("selectedSession() = ok while in agents section, want false")
	}
	if _, ok := m.selectedSessionName(); ok {
		t.Errorf("selectedSessionName() = ok while in agents section, want false")
	}
}

// TestSelectedPane_ResolvesAgentRowsPane confirms selectedPane (the same
// function enter's dispatch already checks first) resolves to the selected
// agent's own pane while in the agents section - this is the ENTIRE
// mechanism behind "↵ on an agent row uses the pane-row switch" (ADR-0056):
// no change to the enter dispatch or to handleSwitchToPane is needed.
func TestSelectedPane_ResolvesAgentRowsPane(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "a"},
			tmux.PaneState{
				PaneID: "%1", PaneIndex: "1", Kind: "claude",
				CurrentCommand: "claude", State: "blocked",
				Session: "devgeta-main", Window: "wt-devgeta-a",
			},
		),
	}
	m := makeTestModel(statuses)
	m.section = sectionAgents
	m.agentCursor = 0

	sel, ok := m.selectedPane()
	if !ok {
		t.Fatalf("selectedPane() = false while an agent row is selected, want true")
	}
	if sel.PaneID != "%1" || sel.Session != "devgeta-main" || sel.Window != "wt-devgeta-a" {
		t.Errorf("selectedPane() = %+v, want the agent's own pane (id=%%1, session=devgeta-main, "+
			"window=wt-devgeta-a)", sel)
	}
}

// TestSelectedDiffStatus_ResolvesAgentsWorktree confirms the right pane's
// diff resolution follows an agent row: a worktree-sourced agent resolves to
// its WorktreeStatus (ADR-0056: "the right pane shows the agent's worktree
// diff"), while a repo-session or standalone-session agent resolves to
// ok=false, same as a plain session row does today.
func TestSelectedDiffStatus_ResolvesAgentsWorktree(t *testing.T) {
	wtStatus := worktree.WorktreeStatus{
		Repo: "devgeta", Name: "a", Path: "/repos/devgeta/a",
		Panes: []tmux.PaneState{agentPane("%1", "1", "claude", "claude", "blocked")},
	}
	m := makeTestModel([]worktree.WorktreeStatus{wtStatus})
	m.repoSessions = []worktree.RepoSessionStatus{
		{
			Repo: "devgeta", Name: "devgeta-main",
			Panes: []tmux.PaneState{agentPane("%2", "1", "opencode", "opencode", "busy")},
		},
	}
	m.rebuildRows()

	byPane := map[string]int{}
	for i, r := range m.agentRows {
		byPane[r.pane.PaneID] = i
	}

	m.section = sectionAgents
	m.agentCursor = byPane["%1"]
	sel, ok := m.selectedDiffStatus()
	if !ok || sel.Path != wtStatus.Path {
		t.Errorf("selectedDiffStatus() for the worktree agent = (%+v, %v), want (%+v, true)",
			sel, ok, wtStatus)
	}

	m.agentCursor = byPane["%2"]
	if _, ok := m.selectedDiffStatus(); ok {
		t.Errorf("selectedDiffStatus() for the repo-session agent = ok, want false (no diff)")
	}
}

// twoWorktreeTwoAgentsModel builds a model with two plain worktrees (no
// agents) so navigableIndices has a known, stable shape, plus a separate
// repo whose one worktree hosts two agent panes - giving the agents section
// exactly 2 rows for the cross-section walk tests below.
func twoWorktreeTwoAgentsModel(t *testing.T) Model {
	t.Helper()
	statuses := []worktree.WorktreeStatus{
		{Repo: "devgeta", Name: "a"},
		{Repo: "devgeta", Name: "b"},
		{
			Repo: "devgeta", Name: "agents-host",
			Panes: []tmux.PaneState{
				agentPane("%1", "1", "claude", "claude", "busy"),
				agentPane("%2", "2", "opencode", "opencode", "busy"),
			},
		},
	}
	m := makeTestModel(statuses)
	if len(m.agentRows) != 2 {
		t.Fatalf("test setup: expected 2 agent rows, got %d: %+v", len(m.agentRows), m.agentRows)
	}
	return m
}

// TestMoveCursor_NoAgents_PreservesWrapAround confirms moveCursor's existing
// spaces-only wrap-around behavior is untouched when there is no agents
// section to cross into - the exact regression model_test.go's own
// "k on first row should wrap to last" / "j on last row should wrap to
// first" tests already cover, restated here against the agents-aware code
// path directly so this file documents the invariant it depends on.
func TestMoveCursor_NoAgents_PreservesWrapAround(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Repo: "devgeta", Name: "a"},
		{Repo: "devgeta", Name: "b"},
	}
	m := makeTestModel(statuses)
	if len(m.agentRows) != 0 {
		t.Fatalf("test setup: expected no agent rows, got %d", len(m.agentRows))
	}
	indices := m.navigableIndices()
	first, last := indices[0], indices[len(indices)-1]

	m.cursor = first
	m.moveCursor(-1)
	if m.cursor != last || m.section != sectionSpaces {
		t.Errorf(
			"k on first row with no agents: cursor=%d section=%v, want wrap to last (%d), sectionSpaces",
			m.cursor,
			m.section,
			last,
		)
	}

	m.cursor = last
	m.moveCursor(1)
	if m.cursor != first || m.section != sectionSpaces {
		t.Errorf(
			"j on last row with no agents: cursor=%d section=%v, want wrap to first (%d), sectionSpaces",
			m.cursor,
			m.section,
			first,
		)
	}
}

// TestMoveCursor_CrossesIntoAgentsAtBottomOfSpaces confirms j from the last
// navigable spaces row enters the agents section at its first row, instead
// of wrapping back to the top of spaces.
func TestMoveCursor_CrossesIntoAgentsAtBottomOfSpaces(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	indices := m.navigableIndices()
	m.cursor = indices[len(indices)-1]
	m.section = sectionSpaces

	m.moveCursor(1)

	if m.section != sectionAgents {
		t.Fatalf(
			"expected section=sectionAgents after j past the last spaces row, got %v",
			m.section,
		)
	}
	if m.agentCursor != 0 {
		t.Errorf("agentCursor = %d, want 0 (the first agent)", m.agentCursor)
	}
}

// TestMoveCursor_CrossesBackToSpacesAtTopOfAgents confirms k from the first
// agent row exits back to spaces, landing on the last spaces row.
func TestMoveCursor_CrossesBackToSpacesAtTopOfAgents(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.section = sectionAgents
	m.agentCursor = 0

	m.moveCursor(-1)

	if m.section != sectionSpaces {
		t.Fatalf(
			"expected section=sectionSpaces after k from the first agent row, got %v",
			m.section,
		)
	}
	indices := m.navigableIndices()
	want := indices[len(indices)-1]
	if m.cursor != want {
		t.Errorf("cursor = %d, want %d (the last spaces row)", m.cursor, want)
	}
}

// TestMoveCursor_WalksWithinAgentsAndClampsAtBottom confirms j/k move between
// agent rows normally, and j at the LAST agent row clamps (stays put)
// instead of wrapping back to the top of spaces - "the cursor clamps within
// visible rows" (Step 5's own text).
func TestMoveCursor_WalksWithinAgentsAndClampsAtBottom(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.section = sectionAgents
	m.agentCursor = 0

	m.moveCursor(1)
	if m.section != sectionAgents || m.agentCursor != 1 {
		t.Fatalf(
			"after j: section=%v agentCursor=%d, want sectionAgents, 1",
			m.section,
			m.agentCursor,
		)
	}

	// At the last agent row: j must clamp, not wrap to spaces' top.
	m.moveCursor(1)
	if m.section != sectionAgents || m.agentCursor != 1 {
		t.Errorf(
			"j at the last agent row: section=%v agentCursor=%d, want clamped at sectionAgents, 1",
			m.section,
			m.agentCursor,
		)
	}
}

// TestMoveCursor_ClampsAtTopOfSpaces confirms k at the FIRST spaces row
// clamps (stays put) rather than wrapping to the bottom of agents - the top
// of the combined view has nowhere further up to go.
func TestMoveCursor_ClampsAtTopOfSpaces(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	indices := m.navigableIndices()
	m.cursor = indices[0]
	m.section = sectionSpaces

	m.moveCursor(-1)

	if m.section != sectionSpaces || m.cursor != indices[0] {
		t.Errorf(
			"k at the first spaces row: section=%v cursor=%d, want clamped at sectionSpaces, %d",
			m.section,
			m.cursor,
			indices[0],
		)
	}
}

// TestHandleKey_SpacesOnlyKeysAreInertInAgentsSection confirms the
// destructive/structural spaces keys (delete, rename, repair, review,
// new-session, new-worktree, fold) do nothing while the cursor is in the
// agents section, rather than acting on m.cursor's frozen, invisible spaces
// row.
func TestHandleKey_SpacesOnlyKeysAreInertInAgentsSection(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)
	m.section = sectionAgents
	m.agentCursor = 0

	for _, key := range []string{"d", "D", "F", "r", "R", "s", "$", "n", "N", "h", "l"} {
		before := m
		got, _ := m.handleKey(tea.KeyPressMsg{Code: rune(key[0])})
		next, ok := got.(Model)
		if !ok {
			t.Fatalf("key %q: handleKey did not return a Model", key)
		}
		if next.section != sectionAgents || next.agentCursor != 0 {
			t.Errorf("key %q: section=%v agentCursor=%d, want unchanged (sectionAgents, 0)",
				key, next.section, next.agentCursor)
		}
		if next.pendingDelete != before.pendingDelete ||
			next.pendingSessionDelete != before.pendingSessionDelete ||
			next.pendingForceDelete != before.pendingForceDelete {
			t.Errorf("key %q: armed a pending destructive action while in the agents section", key)
		}
	}
}

// --- Step 10: filter searches only the open sections (ADR-0056) ---

func TestFilter_SearchesBothSectionsWhenBothOpen(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "fix-stale-notify"},
			agentPane("%1", "1", "claude", "claude", "blocked"),
		),
	}
	m := makeTestModel(statuses)
	m.sessions = testSessions() // "scratch", "notes" - neither matches "stale"
	m.filter.Active = true
	m.filter.InsertText("stale")
	m.rebuildRows()

	if len(m.rows) == 0 {
		t.Errorf("expected the matching worktree row to survive the spaces filter")
	}
	for _, r := range m.rows {
		if r.kind == rowSession {
			t.Errorf("expected non-matching session rows filtered out of spaces, got %+v", r)
		}
	}
	if len(m.agentRows) != 1 {
		t.Errorf(
			"expected the matching agent row to survive the agents filter, got %d",
			len(m.agentRows),
		)
	}
}

func TestFilter_FoldedSectionIsNotSearchedAndKeepsNormalCounts(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "a"},
			agentPane("%1", "1", "claude", "claude", "blocked"),
		),
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "b"},
			agentPane("%2", "1", "opencode", "opencode", "idle"),
		),
	}
	m := makeTestModel(statuses)
	m.agentsFolded = true
	m.filter.Active = true
	// Matches NEITHER agent's label/kind/state word - if agents were being
	// searched, this would filter both of them away entirely.
	m.filter.InsertText("zzz-does-not-match-anything")
	m.rebuildRows()

	if len(m.agentRows) != 2 {
		t.Errorf(
			"expected the folded agents section to ignore the filter and keep its normal "+
				"count of 2, got %d",
			len(m.agentRows),
		)
	}
}

func TestRebuildRows_FallsBackToSpacesWhenAgentsSectionEmpties(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "a"},
			agentPane("%1", "1", "claude", "claude", "blocked"),
		),
	}
	m := makeTestModel(statuses)
	m.section = sectionAgents
	m.agentCursor = 0

	// The only agent's coder exits entirely.
	m.statuses[0].Panes[0] = agentPane("%1", "1", "", "zsh", "")
	m.rebuildRows()

	if len(m.agentRows) != 0 {
		t.Fatalf("expected 0 agent rows, got %d", len(m.agentRows))
	}
	if m.section != sectionSpaces {
		t.Errorf(
			"section = %v, want sectionSpaces once the agents section has nothing left to show",
			m.section,
		)
	}
}

// --- Rendering (Step 5's own test list: "rendering snapshots for A1 in the
// mockups") ---

// TestRenderLeft_AgentsHeaderShowsCountAndFocusColor confirms the AGENTS
// header renders with the total count, and that it's the SAME color as
// SelectedBar only while the cursor is in the agents section, SectionHead's
// dim color otherwise.
func TestRenderLeft_AgentsHeaderShowsCountAndFocusColor(t *testing.T) {
	m := twoWorktreeTwoAgentsModel(t)

	m.section = sectionSpaces
	unfocused := m.renderSectionHeader("AGENTS", 2, 2, m.section == sectionAgents, 40)
	if !strings.Contains(ansi.Strip(unfocused), "AGENTS") ||
		!strings.Contains(ansi.Strip(unfocused), "2") {
		t.Errorf("unfocused header = %q, want to contain AGENTS and count 2", ansi.Strip(unfocused))
	}
	wantUnfocusedColor, _, _ := strings.Cut(m.palette.SectionHead.Render("x"), "x")
	if !strings.HasPrefix(unfocused, wantUnfocusedColor) {
		t.Errorf("unfocused header does not use SectionHead's color: %q", unfocused)
	}

	m.section = sectionAgents
	focused := m.renderSectionHeader("AGENTS", 2, 2, m.section == sectionAgents, 40)
	wantFocusedColor, _, _ := strings.Cut(m.palette.SelectedBar.Render("x"), "x")
	if !strings.HasPrefix(focused, wantFocusedColor) {
		t.Errorf("focused header does not use SelectedBar's color: %q", focused)
	}
}

// TestRenderAgentRow_ContentPerState confirms each of the 5 display states
// renders its own glyph, state word, and coder name, with the location
// label on line 1 and ":"+pane index right-aligned.
func TestRenderAgentRow_ContentPerState(t *testing.T) {
	cases := []struct {
		name      string
		rawState  string
		wantGlyph string
		wantWord  string
	}{
		{"blocked", "blocked", "!", "blocked"},
		{"error", "error", "✕", "error"},
		{"done (idle raw state)", "idle", "◆", "done"},
		{"working (busy raw state)", "busy", "●", "working"},
		{"idle (unset raw state)", "", "○", "idle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := makeTestModel(nil)
			r := agentRow{
				pane:  agentPane("%1", "3", "claude", "claude", tc.rawState),
				state: tuicomponents.AgentRowStateFor(tc.rawState),
				label: "devgeta/fix-stale-notify",
				kind:  "claude",
			}
			out := ansi.Strip(m.renderAgentRow(r, 60, false))
			lines := strings.Split(out, "\n")
			if len(lines) != 2 {
				t.Fatalf("expected exactly 2 lines, got %d: %q", len(lines), out)
			}
			if !strings.Contains(lines[0], tc.wantGlyph) {
				t.Errorf("line 1 = %q, want glyph %q", lines[0], tc.wantGlyph)
			}
			if !strings.Contains(lines[0], "devgeta/fix-stale-notify") {
				t.Errorf("line 1 = %q, want the location label", lines[0])
			}
			if !strings.Contains(lines[0], ":3") {
				t.Errorf("line 1 = %q, want the pane index \":3\" right-aligned", lines[0])
			}
			if !strings.Contains(lines[1], tc.wantWord) {
				t.Errorf("line 2 = %q, want state word %q", lines[1], tc.wantWord)
			}
			if !strings.Contains(lines[1], "claude") {
				t.Errorf("line 2 = %q, want the coder name", lines[1])
			}
		})
	}
}

// TestRenderAgentRow_SelectionStripeOnBothLines confirms the soft-selection
// bar ("▌") appears on BOTH lines of a selected agent row - "the selection
// bar runs down both lines as one stripe" (ADR-0056).
func TestRenderAgentRow_SelectionStripeOnBothLines(t *testing.T) {
	m := makeTestModel(nil)
	r := agentRow{
		pane:  agentPane("%1", "1", "claude", "claude", "blocked"),
		state: tuicomponents.AgentRowBlocked,
		label: "devgeta/fix-stale-notify",
		kind:  "claude",
	}
	out := m.renderAgentRow(r, 60, true)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	for i, line := range lines {
		if !strings.Contains(line, "▌") {
			t.Errorf("line %d = %q, missing the selection stripe", i, line)
		}
	}
}

// TestRenderLeft_TwoSectionsA1Shape is a structural rendering test in the
// spirit of the mockups' A1 frame (both open, cursor on a blocked agent):
// spaces content, then the AGENTS header with the right total, then every
// agent in urgency order (blocked, done, working, idle), each as its own
// two-line pair with the right glyph/label/word/kind.
func TestRenderLeft_TwoSectionsA1Shape(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "fix-stale-notify"},
			tmux.PaneState{
				PaneID: "%1", PaneIndex: "1", Kind: "claude",
				CurrentCommand: "claude", State: "blocked",
			},
		),
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "feat-agents-section"},
			tmux.PaneState{
				PaneID: "%2", PaneIndex: "1", Kind: "opencode",
				CurrentCommand: "opencode", State: "idle",
			},
		),
		withAgentPane(
			worktree.WorktreeStatus{Repo: "dotfiles", Name: "main"},
			tmux.PaneState{
				PaneID: "%3", PaneIndex: "1", Kind: "claude",
				CurrentCommand: "claude", State: "busy",
			},
		),
	}
	m := makeTestModel(statuses)
	m.height = 60 // plenty of room: nothing needs scrolling in either section
	m.section = sectionAgents
	m.agentCursor = 0 // the blocked agent, per buildAgentRows' urgency sort

	out := ansi.Strip(m.renderLeft(60))
	lines := strings.Split(out, "\n")

	headerIdx := -1
	for i, l := range lines {
		if strings.Contains(l, "AGENTS") {
			headerIdx = i
			break
		}
	}
	if headerIdx == -1 {
		t.Fatalf("no AGENTS header found in rendered output:\n%s", out)
	}
	if !strings.Contains(lines[headerIdx], "3") {
		t.Errorf("AGENTS header = %q, want the total count 3", lines[headerIdx])
	}

	// Spaces content must all come BEFORE the header (both worktrees, since
	// nothing is scrolled off with this much height).
	spacesBlob := strings.Join(lines[:headerIdx], "\n")
	for _, want := range []string{"fix-stale-notify", "feat-agents-section", "main"} {
		if !strings.Contains(spacesBlob, want) {
			t.Errorf("spaces content missing %q:\n%s", want, spacesBlob)
		}
	}

	agentLines := lines[headerIdx+1:]
	if len(agentLines) != 6 { // 3 agents * 2 lines
		t.Fatalf(
			"expected 6 agent lines (3 agents), got %d:\n%s",
			len(agentLines),
			strings.Join(agentLines, "\n"),
		)
	}
	wantOrder := []struct{ label, word, kind string }{
		{"devgeta/fix-stale-notify", "blocked", "claude"},
		{"devgeta/feat-agents-section", "done", "opencode"},
		{"dotfiles/main", "working", "claude"},
	}
	for i, want := range wantOrder {
		line1, line2 := agentLines[i*2], agentLines[i*2+1]
		if !strings.Contains(line1, want.label) {
			t.Errorf("agent %d line 1 = %q, want label %q", i, line1, want.label)
		}
		if !strings.Contains(line2, want.word) {
			t.Errorf("agent %d line 2 = %q, want word %q", i, line2, want.word)
		}
		if !strings.Contains(line2, want.kind) {
			t.Errorf("agent %d line 2 = %q, want kind %q", i, line2, want.kind)
		}
	}
}
