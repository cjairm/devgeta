package tuiworktree

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/internal/tooling/task"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

func init() { testutil.InitLogger() }

func makeTestModel(statuses []worktree.WorktreeStatus) Model {
	m := Model{
		collapsed:      map[string]bool{},
		palette:        tuicomponents.NewPalette(),
		leftPaneWidth:  minLeftPaneWidth,
		width:          120,
		height:         40,
		prTitles:       map[string]string{},
		prTitlePending: map[string]bool{},
	}
	m.diffFn = func(_ string) (task.BranchDiffResult, error) {
		return task.BranchDiffResult{Content: "diff content", Files: 1, Added: 5, Removed: 2}, nil
	}
	m.attachFn = func(_, _ string) error { return nil }
	m.removeFn = func(_, _ string, _ bool) error { return nil }
	m.removeSessionFn = func(_, _ string) error { return nil }
	m.repairFn = func(_, _ string, _ worktree.Layout) error { return nil }
	m.windowSessionFn = func(_ string) (string, bool) { return "", false }
	m.clearAgentStateFn = func(_ string) error { return nil }
	m.currentSessionFn = func() (string, bool) { return "", false }
	m.createSessionFn = func(_, _ string) error { return nil }
	m.switchToSessionFn = func(_ string) error { return nil }
	m.switchToPaneFn = func(_, _, _ string) error { return nil }
	m.clearAgentStateForPaneFn = func(_ string) error { return nil }
	m.killSessionFn = func(_ string) error { return nil }
	m.listSessionNamesFn = func() ([]string, error) { return nil, nil }
	m.repoCandidatesFn = func(_ string) ([]string, error) { return nil, nil }
	m.validateRepoPathFn = func(path string) (string, error) { return path, nil }
	m.validateSessionDirFn = func(path string) (string, error) { return path, nil }
	m.checkHookCompatibilityFn = func(_ string) []string { return nil }
	m.createFn = func(_, _, _ string) (string, error) { return "", nil }
	m.prTitleFn = func(_, _ string) string { return "" }
	m.launchReviewFn = func(_, _, _ string) error { return nil }
	m.statuses = statuses
	m.rebuildRows()
	return m
}

// flattenCmd runs cmd and recursively unwraps any tea.BatchMsg it produces,
// so tests can assert on the individual messages a tea.Batch yields without
// needing the full bubbletea runtime to fan them back into Update.
func flattenCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, flattenCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func testStatuses() []worktree.WorktreeStatus {
	return []worktree.WorktreeStatus{
		{
			Name:         "feature-a",
			Repo:         "repo-a",
			Path:         "/tmp/a",
			TmuxWindow:   "wt-feature-a",
			WindowActive: true,
		},
		{
			Name:         "feature-b",
			Repo:         "repo-a",
			Path:         "/tmp/b",
			TmuxWindow:   "wt-feature-b",
			WindowActive: false,
		},
		{
			Name:         "feature-x",
			Repo:         "repo-b",
			Path:         "/tmp/x",
			TmuxWindow:   "wt-feature-x",
			WindowActive: true,
		},
	}
}

func TestBuildRowsGrouping(t *testing.T) {
	statuses := testStatuses()
	rows := buildRows(statuses, nil, map[string]bool{}, "")
	// Should have: repo-a header, feature-a, feature-b, repo-b header, feature-x
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	if rows[0].kind != rowRepo || rows[0].repo != "repo-a" {
		t.Error("expected first row to be repo-a header")
	}
	if rows[1].kind != rowWorktree || rows[1].status.Name != "feature-a" {
		t.Error("expected second row to be feature-a")
	}
	if rows[3].kind != rowRepo || rows[3].repo != "repo-b" {
		t.Error("expected fourth row to be repo-b header")
	}
}

func testSessions() []worktree.SessionStatus {
	return []worktree.SessionStatus{
		{Name: "scratch", Attached: false},
		{Name: "notes", Attached: true},
	}
}

func TestBuildRowsRepoHeaderWorktreeCount(t *testing.T) {
	rows := buildRows(testStatuses(), nil, map[string]bool{}, "")
	if rows[0].kind != rowRepo || rows[0].repo != "repo-a" {
		t.Fatal("expected first row to be repo-a header")
	}
	if rows[0].worktreeCount != 2 {
		t.Errorf("expected repo-a header worktreeCount=2, got %d", rows[0].worktreeCount)
	}
	if rows[3].kind != rowRepo || rows[3].repo != "repo-b" {
		t.Fatal("expected fourth row to be repo-b header")
	}
	if rows[3].worktreeCount != 1 {
		t.Errorf("expected repo-b header worktreeCount=1, got %d", rows[3].worktreeCount)
	}
	// Collapsing a repo must not change its header's worktree count — the
	// count describes the repo's children, not what's currently rendered.
	collapsed := map[string]bool{"repo-a": true}
	rowsCollapsed := buildRows(testStatuses(), nil, collapsed, "")
	if rowsCollapsed[0].kind != rowRepo || rowsCollapsed[0].worktreeCount != 2 {
		t.Errorf("collapsed repo-a header should still report worktreeCount=2, got %d",
			rowsCollapsed[0].worktreeCount)
	}
}

// TestBuildRowsRepoHeaderAggregatesAgentState verifies the repo header row
// aggregates its children's AgentState via worktree.AggregateAgentState
// (blocked > error > idle > busy > "") so a collapsed repo still surfaces an
// urgent child state — ADR-0008 Step 6.
func TestBuildRowsRepoHeaderAggregatesAgentState(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a", AgentState: worktree.AgentStateBlocked},
		{Name: "feature-b", Repo: "repo-a", AgentState: worktree.AgentStateIdle},
		{Name: "feature-x", Repo: "repo-b", AgentState: ""},
	}
	rows := buildRows(statuses, nil, map[string]bool{}, "")
	if rows[0].kind != rowRepo || rows[0].repo != "repo-a" {
		t.Fatal("expected first row to be repo-a header")
	}
	if rows[0].agentState != worktree.AgentStateBlocked {
		t.Errorf(
			"expected repo-a header to aggregate to %q (blocked beats idle), got %q",
			worktree.AgentStateBlocked, rows[0].agentState,
		)
	}

	var repoBRow row
	for _, r := range rows {
		if r.kind == rowRepo && r.repo == "repo-b" {
			repoBRow = r
		}
	}
	if repoBRow.agentState != "" {
		t.Errorf(
			"expected repo-b header with no reporting children to aggregate to \"\", got %q",
			repoBRow.agentState,
		)
	}

	// Collapsing must not change the aggregation — it describes the repo's
	// children, not what's currently rendered.
	collapsed := map[string]bool{"repo-a": true}
	rowsCollapsed := buildRows(statuses, nil, collapsed, "")
	if rowsCollapsed[0].agentState != worktree.AgentStateBlocked {
		t.Errorf(
			"collapsed repo-a header should still aggregate to %q, got %q",
			worktree.AgentStateBlocked, rowsCollapsed[0].agentState,
		)
	}
}

func TestBuildRowsSessionsAppendedAsLeavesAfterRepos(t *testing.T) {
	rows := buildRows(testStatuses(), testSessions(), map[string]bool{}, "")
	// repo-a header, feature-a, feature-b, repo-b header, feature-x, then
	// sessions alpha-sorted: notes, scratch.
	if len(rows) != 7 {
		t.Fatalf("expected 7 rows (5 worktree rows + 2 sessions), got %d", len(rows))
	}
	for i := range 5 {
		if rows[i].kind == rowSession {
			t.Fatalf(
				"row %d: session rows must come after all repo groups, got session at index %d",
				i,
				i,
			)
		}
	}
	if rows[5].kind != rowSession || rows[5].session.Name != "notes" {
		t.Errorf("expected row 5 to be session 'notes' (alpha-sorted), got kind=%d name=%q",
			rows[5].kind, rows[5].session.Name)
	}
	if rows[6].kind != rowSession || rows[6].session.Name != "scratch" {
		t.Errorf("expected row 6 to be session 'scratch', got kind=%d name=%q",
			rows[6].kind, rows[6].session.Name)
	}
}

func TestBuildRowsSessionsWithNoWorktrees(t *testing.T) {
	// Sessions must appear even when there are zero repos/worktrees.
	rows := buildRows(nil, testSessions(), map[string]bool{}, "")
	if len(rows) != 2 {
		t.Fatalf("expected 2 session rows, got %d", len(rows))
	}
	for _, r := range rows {
		if r.kind != rowSession {
			t.Errorf("expected only session rows, got kind=%d", r.kind)
		}
		if r.repo != "" {
			t.Errorf("session row must not carry a repo, got %q", r.repo)
		}
	}
}

func TestBuildRowsSessionRowUnaffectedByCollapse(t *testing.T) {
	// Collapsing every repo must not hide or alter session rows — sessions
	// have no expand/collapse state of their own.
	collapsed := map[string]bool{"repo-a": true, "repo-b": true}
	rows := buildRows(testStatuses(), testSessions(), collapsed, "")
	var sessionCount int
	for _, r := range rows {
		if r.kind == rowSession {
			sessionCount++
		}
	}
	if sessionCount != 2 {
		t.Errorf("expected 2 session rows regardless of repo collapse state, got %d", sessionCount)
	}
}

func TestBuildRowsFilterMatchesSessionNames(t *testing.T) {
	// Judgment call: filter matches session names too, consistent with the
	// dashboard reading as one unified/filterable list.
	rows := buildRows(testStatuses(), testSessions(), map[string]bool{}, "notes")
	if len(rows) != 1 {
		t.Fatalf(
			"expected filter 'notes' to leave only the matching session row, got %d rows",
			len(rows),
		)
	}
	if rows[0].kind != rowSession || rows[0].session.Name != "notes" {
		t.Errorf(
			"expected the 'notes' session row, got kind=%d name=%q",
			rows[0].kind,
			rows[0].session.Name,
		)
	}
}

func TestLeafIndicesIncludesSessionRows(t *testing.T) {
	rows := buildRows(testStatuses(), testSessions(), map[string]bool{}, "")
	indices := leafIndices(rows)
	// 3 worktree rows + 2 session rows = 5 leaf rows.
	if len(indices) != 5 {
		t.Fatalf("expected 5 leaf indices (worktree+session), got %d", len(indices))
	}
	for _, i := range indices {
		if rows[i].kind == rowRepo {
			t.Errorf("leafIndices must not include a repo header row, got index %d", i)
		}
	}
}

func TestNavigableIndicesIncludesSessionRows(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	m.rebuildRows()
	indices := m.navigableIndices()
	for _, i := range indices {
		if m.rows[i].kind == rowSession {
			return
		}
	}
	t.Error("expected navigableIndices to include at least one session row")
}

func TestSelectedStatusFalseForSessionRow(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	m.rebuildRows()
	// Land the cursor on a session row.
	found := false
	for i, r := range m.rows {
		if r.kind == rowSession {
			m.cursor = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected at least one session row after rebuildRows")
	}
	if _, ok := m.selectedStatus(); ok {
		t.Error("selectedStatus must return false when cursor is on a session row")
	}
}

// TestRenderLeftSessionRowShowsName is a regression test for a bug caught in
// review: with rowSession falling through to the rowWorktree render branch,
// a populated m.sessions rendered as a blank name with a stray "└" connector
// (r.status is zero-valued for a session row). renderLeft now has its own
// (placeholder, pending Step 7) rowSession branch that renders r.session.Name.
func TestRenderLeftSessionRowShowsName(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	m.rebuildRows()

	out := ansi.Strip(m.renderLeft(40))
	if !strings.Contains(out, "notes") {
		t.Errorf("expected renderLeft output to contain session name %q, got:\n%s", "notes", out)
	}
	if !strings.Contains(out, "scratch") {
		t.Errorf("expected renderLeft output to contain session name %q, got:\n%s", "scratch", out)
	}
}

// TestBuildRowsPaneRowsForQualifyingWorktree verifies ADR-0008 Step 7: a
// worktree with 2+ panes reporting a non-empty agent state gets its panes
// revealed as rowPane children immediately after the worktree row, in scan
// order.
func TestBuildRowsPaneRowsForQualifyingWorktree(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Name:       "feature-a",
			Repo:       "repo-a",
			TmuxWindow: "wt-feature-a",
			Panes: []tmux.PaneState{
				{
					PaneID:         "%1",
					PaneIndex:      "0",
					CurrentCommand: "claude",
					State:          worktree.AgentStateBusy,
				},
				{
					PaneID:         "%2",
					PaneIndex:      "1",
					CurrentCommand: "zsh",
					State:          worktree.AgentStateIdle,
				},
			},
		},
	}
	rows := buildRows(statuses, nil, map[string]bool{}, "")
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows (repo header + worktree + 2 panes), got %d: %+v", len(rows), rows)
	}
	if rows[0].kind != rowRepo {
		t.Fatalf("expected row 0 to be the repo header, got %+v", rows[0])
	}
	if rows[1].kind != rowWorktree {
		t.Fatalf("expected row 1 to be the worktree, got %+v", rows[1])
	}
	if rows[2].kind != rowPane || rows[2].pane.PaneID != "%1" {
		t.Errorf("expected row 2 to be pane %%1 right after the worktree row, got %+v", rows[2])
	}
	if rows[3].kind != rowPane || rows[3].pane.PaneID != "%2" {
		t.Errorf("expected row 3 to be pane %%2, got %+v", rows[3])
	}
}

// TestBuildRowsPaneRowsForQualifyingSession mirrors
// TestBuildRowsPaneRowsForQualifyingWorktree for a standalone session parent.
func TestBuildRowsPaneRowsForQualifyingSession(t *testing.T) {
	sessions := []worktree.SessionStatus{
		{
			Name: "notes",
			Panes: []tmux.PaneState{
				{
					PaneID:         "%1",
					PaneIndex:      "0",
					CurrentCommand: "claude",
					State:          worktree.AgentStateBlocked,
				},
				{
					PaneID:         "%2",
					PaneIndex:      "1",
					CurrentCommand: "vim",
					State:          worktree.AgentStateError,
				},
			},
		},
	}
	rows := buildRows(nil, sessions, map[string]bool{}, "")
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (session + 2 panes), got %d: %+v", len(rows), rows)
	}
	if rows[0].kind != rowSession {
		t.Fatalf("expected row 0 to be the session, got %+v", rows[0])
	}
	if rows[1].kind != rowPane || rows[1].pane.PaneID != "%1" {
		t.Errorf("expected row 1 to be pane %%1 right after the session row, got %+v", rows[1])
	}
	if rows[2].kind != rowPane || rows[2].pane.PaneID != "%2" {
		t.Errorf("expected row 2 to be pane %%2, got %+v", rows[2])
	}
}

// TestBuildRowsPaneRowsIncludeAllPanesNotJustStateful verifies that once a
// parent qualifies (2+ stateful panes), ALL of its panes are emitted —
// including ones with an empty state — so the user sees full context of
// what's running where, not just the stateful subset.
func TestBuildRowsPaneRowsIncludeAllPanesNotJustStateful(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Name:       "feature-a",
			Repo:       "repo-a",
			TmuxWindow: "wt-feature-a",
			Panes: []tmux.PaneState{
				{
					PaneID:         "%1",
					PaneIndex:      "0",
					CurrentCommand: "claude",
					State:          worktree.AgentStateBusy,
				},
				{PaneID: "%2", PaneIndex: "1", CurrentCommand: "zsh", State: ""},
				{
					PaneID:         "%3",
					PaneIndex:      "2",
					CurrentCommand: "claude",
					State:          worktree.AgentStateIdle,
				},
			},
		},
	}
	rows := buildRows(statuses, nil, map[string]bool{}, "")
	var paneIDs []string
	for _, r := range rows {
		if r.kind == rowPane {
			paneIDs = append(paneIDs, r.pane.PaneID)
		}
	}
	want := []string{"%1", "%2", "%3"}
	if len(paneIDs) != len(want) {
		t.Fatalf("expected all 3 panes emitted (including the stateless one), got %v", paneIDs)
	}
	for i, id := range want {
		if paneIDs[i] != id {
			t.Errorf("pane[%d] = %q, want %q (scan order preserved)", i, paneIDs[i], id)
		}
	}
}

// TestBuildRowsNoPaneRowsBelowThreshold verifies the 2+ stateful-pane
// threshold: a parent with zero or one stateful pane emits no pane rows at
// all, for both worktree and session parents.
func TestBuildRowsNoPaneRowsBelowThreshold(t *testing.T) {
	t.Run("worktree with zero stateful panes", func(t *testing.T) {
		statuses := []worktree.WorktreeStatus{
			{
				Name:       "feature-a",
				Repo:       "repo-a",
				TmuxWindow: "wt-feature-a",
				Panes: []tmux.PaneState{
					{PaneID: "%1", State: ""},
				},
			},
		}
		rows := buildRows(statuses, nil, map[string]bool{}, "")
		for _, r := range rows {
			if r.kind == rowPane {
				t.Errorf("expected no pane rows with 0 stateful panes, got %+v", rows)
			}
		}
	})

	t.Run("session with one stateful pane", func(t *testing.T) {
		sessions := []worktree.SessionStatus{
			{
				Name: "notes",
				Panes: []tmux.PaneState{
					{PaneID: "%1", State: worktree.AgentStateBusy},
				},
			},
		}
		rows := buildRows(nil, sessions, map[string]bool{}, "")
		for _, r := range rows {
			if r.kind == rowPane {
				t.Errorf("expected no pane rows with only 1 stateful pane, got %+v", rows)
			}
		}
	})
}

// TestBuildRowsPaneRowsCollapsedByKey verifies that setting a qualifying
// parent's collapse key to true in the collapsed map suppresses its pane
// rows, for both the "worktree:<TmuxWindow>" and "session:<Name>" key
// schemes.
func TestBuildRowsPaneRowsCollapsedByKey(t *testing.T) {
	t.Run("worktree collapse key", func(t *testing.T) {
		statuses := []worktree.WorktreeStatus{
			{
				Name:       "feature-a",
				Repo:       "repo-a",
				TmuxWindow: "wt-feature-a",
				Panes: []tmux.PaneState{
					{PaneID: "%1", State: worktree.AgentStateBusy},
					{PaneID: "%2", State: worktree.AgentStateIdle},
				},
			},
		}
		collapsed := map[string]bool{"worktree:wt-feature-a": true}
		rows := buildRows(statuses, nil, collapsed, "")
		for _, r := range rows {
			if r.kind == rowPane {
				t.Errorf(
					"expected no pane rows when worktree:wt-feature-a is collapsed, got %+v",
					rows,
				)
			}
		}
	})

	t.Run("session collapse key", func(t *testing.T) {
		sessions := []worktree.SessionStatus{
			{
				Name: "notes",
				Panes: []tmux.PaneState{
					{PaneID: "%1", State: worktree.AgentStateBlocked},
					{PaneID: "%2", State: worktree.AgentStateError},
				},
			},
		}
		collapsed := map[string]bool{"session:notes": true}
		rows := buildRows(nil, sessions, collapsed, "")
		for _, r := range rows {
			if r.kind == rowPane {
				t.Errorf("expected no pane rows when session:notes is collapsed, got %+v", rows)
			}
		}
	})
}

// TestBuildRowsRepoAndSessionCollapseKeysDoNotCollide verifies ADR-0008's
// rationale for the "session:" prefix: a repo named "shared" and a standalone
// session also named "shared" use disjoint collapse-map keys (bare "shared"
// for the repo, "session:shared" for the session's pane expansion), so
// collapsing one can never affect the other.
func TestBuildRowsRepoAndSessionCollapseKeysDoNotCollide(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "shared", TmuxWindow: "wt-feature-a"},
	}
	sessions := []worktree.SessionStatus{
		{Name: "shared", Panes: qualifyingPanes()},
	}

	hasRepoWorktreeRow := func(rows []row) bool {
		for _, r := range rows {
			if r.kind == rowWorktree && r.repo == "shared" {
				return true
			}
		}
		return false
	}
	hasSessionPaneRows := func(rows []row) bool {
		for _, r := range rows {
			if r.kind == rowPane {
				return true
			}
		}
		return false
	}

	t.Run("collapsing the repo leaves the session's pane expansion untouched", func(t *testing.T) {
		collapsed := map[string]bool{"shared": true}
		rows := buildRows(statuses, sessions, collapsed, "")

		if hasRepoWorktreeRow(rows) {
			t.Error("expected repo 'shared' collapse to hide its worktree row, but it is present")
		}
		if !hasSessionPaneRows(rows) {
			t.Error(
				"expected session 'shared' pane rows to still be shown - " +
					"collapsing the repo key must not collapse the session key",
			)
		}
	})

	t.Run("collapsing the session leaves the repo's worktree row untouched", func(t *testing.T) {
		collapsed := map[string]bool{"session:shared": true}
		rows := buildRows(statuses, sessions, collapsed, "")

		if !hasRepoWorktreeRow(rows) {
			t.Error(
				"expected repo 'shared' worktree row to still be shown - " +
					"collapsing the session key must not collapse the repo key",
			)
		}
		if hasSessionPaneRows(rows) {
			t.Error(
				"expected session 'shared' pane rows to be hidden once session:shared is collapsed",
			)
		}
	})
}

// TestLeafIndicesIncludesPaneRows verifies pane rows are valid cursor landing
// spots after a rows rebuild.
func TestLeafIndicesIncludesPaneRows(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Name:       "feature-a",
			Repo:       "repo-a",
			TmuxWindow: "wt-feature-a",
			Panes: []tmux.PaneState{
				{PaneID: "%1", State: worktree.AgentStateBusy},
				{PaneID: "%2", State: worktree.AgentStateIdle},
			},
		},
	}
	rows := buildRows(statuses, nil, map[string]bool{}, "")
	indices := leafIndices(rows)
	// worktree row + 2 pane rows = 3 leaf rows (repo header excluded).
	if len(indices) != 3 {
		t.Fatalf("expected 3 leaf indices (worktree + 2 panes), got %d: %+v", len(indices), indices)
	}
	paneCount := 0
	for _, i := range indices {
		if rows[i].kind == rowPane {
			paneCount++
		}
	}
	if paneCount != 2 {
		t.Errorf("expected leafIndices to include both pane rows, got %d", paneCount)
	}
}

// TestNavigableIndicesIncludesPaneRows verifies j/k can reach pane rows.
func TestNavigableIndicesIncludesPaneRows(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Name:       "feature-a",
			Repo:       "repo-a",
			TmuxWindow: "wt-feature-a",
			Panes: []tmux.PaneState{
				{PaneID: "%1", State: worktree.AgentStateBusy},
				{PaneID: "%2", State: worktree.AgentStateIdle},
			},
		},
	}
	m := makeTestModel(statuses)
	indices := m.navigableIndices()
	found := false
	for _, i := range indices {
		if m.rows[i].kind == rowPane {
			found = true
		}
	}
	if !found {
		t.Error("expected navigableIndices to include at least one pane row")
	}
}

// TestRenderLeftPaneRowShowsIndexAndCommand verifies a pane row renders its
// pane index, a space, then its current command.
func TestRenderLeftPaneRowShowsIndexAndCommand(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Name:       "feature-a",
			Repo:       "repo-a",
			TmuxWindow: "wt-feature-a",
			Panes: []tmux.PaneState{
				{
					PaneID:         "%1",
					PaneIndex:      "0",
					CurrentCommand: "claude",
					State:          worktree.AgentStateBusy,
				},
				{
					PaneID:         "%2",
					PaneIndex:      "1",
					CurrentCommand: "zsh",
					State:          worktree.AgentStateIdle,
				},
			},
		},
	}
	m := makeTestModel(statuses)

	out := ansi.Strip(m.renderLeft(60))
	lines := strings.Split(out, "\n")

	var paneLine0, paneLine1 string
	for i, r := range m.rows {
		if r.kind == rowPane && r.pane.PaneID == "%1" {
			paneLine0 = lines[i]
		}
		if r.kind == rowPane && r.pane.PaneID == "%2" {
			paneLine1 = lines[i]
		}
	}
	if paneLine0 == "" || paneLine1 == "" {
		t.Fatalf("expected both pane rows to render, got rows: %+v", m.rows)
	}
	if !strings.Contains(paneLine0, "0 claude") {
		t.Errorf("expected pane row to show \"0 claude\", got %q", paneLine0)
	}
	if !strings.Contains(paneLine1, "1 zsh") {
		t.Errorf("expected pane row to show \"1 zsh\", got %q", paneLine1)
	}
}

// TestRenderLeftPaneRowDotReflectsOwnState verifies each pane row's dot
// reflects that PANE's own state, not the parent worktree/session's
// aggregated AgentState — the entire point of drilling down (ADR-0008 §3).
func TestRenderLeftPaneRowDotReflectsOwnState(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Name:       "feature-a",
			Repo:       "repo-a",
			TmuxWindow: "wt-feature-a",
			// Parent's aggregate is deliberately "blocked" (the highest
			// precedence state) while the individual panes are busy/blocked -
			// only the second pane should show "!".
			AgentState: worktree.AgentStateBlocked,
			Panes: []tmux.PaneState{
				{
					PaneID:         "%1",
					PaneIndex:      "0",
					CurrentCommand: "claude",
					State:          worktree.AgentStateBusy,
				},
				{
					PaneID:         "%2",
					PaneIndex:      "1",
					CurrentCommand: "claude",
					State:          worktree.AgentStateBlocked,
				},
			},
		},
	}
	m := makeTestModel(statuses)

	out := ansi.Strip(m.renderLeft(60))
	lines := strings.Split(out, "\n")

	var busyLine, blockedLine string
	for i, r := range m.rows {
		if r.kind == rowPane && r.pane.PaneID == "%1" {
			busyLine = lines[i]
		}
		if r.kind == rowPane && r.pane.PaneID == "%2" {
			blockedLine = lines[i]
		}
	}
	if busyLine == "" || blockedLine == "" {
		t.Fatalf("expected both pane rows to render, got rows: %+v", m.rows)
	}
	if !strings.Contains(busyLine, "●") {
		t.Errorf("expected the busy pane's own dot (●), got %q", busyLine)
	}
	if strings.Contains(busyLine, "!") {
		t.Errorf(
			"busy pane must not show the parent's aggregate blocked glyph, got %q",
			busyLine,
		)
	}
	if !strings.Contains(blockedLine, "!") {
		t.Errorf("expected the blocked pane's own dot (!), got %q", blockedLine)
	}
}

// TestRenderLeftPaneRowEmptyStateRendersRunning verifies that a pane with no
// agent state ever reported still renders as StateRunning (green ●), per the
// windowActive=true design: a pane row only exists because its pane is live.
func TestRenderLeftPaneRowEmptyStateRendersRunning(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Name:       "feature-a",
			Repo:       "repo-a",
			TmuxWindow: "wt-feature-a",
			Panes: []tmux.PaneState{
				{
					PaneID:         "%1",
					PaneIndex:      "0",
					CurrentCommand: "claude",
					State:          worktree.AgentStateBusy,
				},
				{
					PaneID:         "%2",
					PaneIndex:      "1",
					CurrentCommand: "vim",
					State:          worktree.AgentStateIdle,
				},
				{PaneID: "%3", PaneIndex: "2", CurrentCommand: "zsh", State: ""},
			},
		},
	}
	m := makeTestModel(statuses)

	rawLines := strings.Split(m.renderLeft(60), "\n")
	strippedLines := strings.Split(ansi.Strip(m.renderLeft(60)), "\n")

	var rawLine, strippedLine string
	for i, r := range m.rows {
		if r.kind == rowPane && r.pane.PaneID == "%3" {
			rawLine = rawLines[i]
			strippedLine = strippedLines[i]
		}
	}
	if rawLine == "" {
		t.Fatalf("expected the empty-state pane row to render, got rows: %+v", m.rows)
	}
	if !strings.Contains(strippedLine, "●") {
		t.Errorf("expected an empty-state pane to render the running glyph ●, got %q", strippedLine)
	}
	runningPrefix := strings.SplitN(m.palette.Running.Render("X"), "X", 2)[0]
	if !strings.Contains(rawLine, runningPrefix) {
		t.Errorf("expected an empty-state pane to use the Running (green) style, got %q", rawLine)
	}
}

// TestRenderLeftPaneRowDisambiguatesByWindow verifies pane rows for a session
// spanning multiple tmux windows render distinguishably when two panes share
// the same PaneIndex in different windows - tmux's #{pane_index} is only
// unique within a window (see tmux.PaneState.PaneIndex's doc comment), so a
// session hosting an agent in window A pane 0 and another in window B pane 0
// must not both render as the bare "0 claude" this used to collapse to.
func TestRenderLeftPaneRowDisambiguatesByWindow(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = []worktree.SessionStatus{
		{
			Name: "multi-window",
			Panes: []tmux.PaneState{
				{
					PaneID:         "%1",
					Window:         "windowA",
					PaneIndex:      "0",
					CurrentCommand: "claude",
					State:          worktree.AgentStateBusy,
				},
				{
					PaneID:         "%2",
					Window:         "windowB",
					PaneIndex:      "0",
					CurrentCommand: "vim",
					State:          worktree.AgentStateIdle,
				},
			},
		},
	}
	m.rebuildRows()

	out := ansi.Strip(m.renderLeft(60))
	lines := strings.Split(out, "\n")

	var lineA, lineB string
	for i, r := range m.rows {
		if r.kind == rowPane && r.pane.PaneID == "%1" {
			lineA = lines[i]
		}
		if r.kind == rowPane && r.pane.PaneID == "%2" {
			lineB = lines[i]
		}
	}
	if lineA == "" || lineB == "" {
		t.Fatalf("expected both pane rows to render, got rows: %+v", m.rows)
	}
	if lineA == lineB {
		t.Fatalf(
			"expected pane rows sharing PaneIndex 0 in different windows to render distinguishably, both got %q",
			lineA,
		)
	}
	if !strings.Contains(lineA, "windowA:0") {
		t.Errorf("expected pane row to show %q, got %q", "windowA:0", lineA)
	}
	if !strings.Contains(lineB, "windowB:0") {
		t.Errorf("expected pane row to show %q, got %q", "windowB:0", lineB)
	}
}

// TestPaneRowShowsGuidanceInsteadOfStaleDiff mirrors
// TestSessionRowShowsGuidanceInsteadOfStaleDiff for rowPane: pane rows have
// no diff either (selectedStatus/selectionChangedCmd never fires for them),
// so moving the cursor onto one must show guidance, not a stale worktree diff.
func TestPaneRowShowsGuidanceInsteadOfStaleDiff(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Name:       "feature-a",
			Repo:       "repo-a",
			TmuxWindow: "wt-feature-a",
			Panes: []tmux.PaneState{
				{
					PaneID:         "%1",
					PaneIndex:      "0",
					CurrentCommand: "claude",
					State:          worktree.AgentStateBusy,
				},
				{
					PaneID:         "%2",
					PaneIndex:      "1",
					CurrentCommand: "zsh",
					State:          worktree.AgentStateIdle,
				},
			},
		},
	}
	m := makeTestModel(statuses)
	// Seed a stale worktree diff, as if the cursor sat on a worktree row
	// before moving to the pane row below.
	m.diffContent = "stale diff content"
	m.diffBase = "main @3e90667"
	m.diffBranch = "feature-a"

	paneIdx := -1
	for i, r := range m.rows {
		if r.kind == rowPane {
			paneIdx = i
			break
		}
	}
	if paneIdx == -1 {
		t.Fatal("expected a rowPane row after rebuildRows")
	}
	m.cursor = paneIdx

	got := ansi.Strip(m.renderRight(100))
	if strings.Contains(got, "stale diff content") {
		t.Errorf("pane row must not render a stale worktree diff, got %q", got)
	}
	if !strings.Contains(got, "no diff") {
		t.Errorf("expected pane guidance text, got %q", got)
	}
}

func TestCursorSkipsRepoHeaders(t *testing.T) {
	m := makeTestModel(testStatuses())
	// Initial cursor should be on a worktree row
	if m.rows[m.cursor].kind != rowWorktree {
		t.Error("initial cursor should be on a worktree row")
	}
	// Move down
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	m3 := m2.(Model)
	if m3.rows[m3.cursor].kind != rowWorktree {
		t.Error("after j, cursor should still be on a worktree row")
	}
}

// The left panel accepts arrows as aliases for j/k. Asserting they match j/k
// rather than hardcoding indices keeps the two paths from drifting apart if
// row layout or the skip-headers rule changes.
func TestArrowKeysMoveCursorLikeJK(t *testing.T) {
	for _, tc := range []struct {
		name       string
		vim, arrow tea.KeyPressMsg
	}{
		{"down", tea.KeyPressMsg{Code: 'j'}, tea.KeyPressMsg{Code: tea.KeyDown}},
		{"up", tea.KeyPressMsg{Code: 'k'}, tea.KeyPressMsg{Code: tea.KeyUp}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Start mid-list so "up" has somewhere to go.
			start := makeTestModel(testStatuses())
			mi, _ := start.Update(tea.KeyPressMsg{Code: 'j'})
			start = mi.(Model)

			vi, _ := start.Update(tc.vim)
			ai, _ := start.Update(tc.arrow)
			wantCursor := vi.(Model).cursor
			gotCursor := ai.(Model).cursor

			if gotCursor == start.cursor {
				t.Fatalf("arrow key did not move the cursor at all (stayed at %d)", gotCursor)
			}
			if gotCursor != wantCursor {
				t.Errorf("arrow moved cursor to %d, j/k moved it to %d — they must match",
					gotCursor, wantCursor)
			}
		})
	}
}

// Arrows must keep belonging to the filter's text caret while it's active: the
// filter check in handleKey returns before the navigation switch, and this
// pins that ordering so a future reshuffle can't let the list steal them.
func TestArrowKeysDoNotMoveCursorWhileFiltering(t *testing.T) {
	m := makeTestModel(testStatuses())
	mi, _ := m.Update(tea.KeyPressMsg{Code: 'j'}) // move off row 0 so a stray move is visible
	m = mi.(Model)
	m.filter.Active = true
	before := m.cursor

	for _, k := range []tea.KeyPressMsg{
		{Code: tea.KeyDown},
		{Code: tea.KeyUp},
	} {
		mi, _ = m.Update(k)
		m = mi.(Model)
	}

	if m.cursor != before {
		t.Errorf("arrows moved the list cursor from %d to %d while the filter was active",
			before, m.cursor)
	}
}

func TestFoldHide(t *testing.T) {
	m := makeTestModel(testStatuses())
	// Collapse repo-a
	m.collapsed["repo-a"] = true
	m.rebuildRows()
	// Should have: repo-a header, repo-b header, feature-x
	if len(m.rows) != 3 {
		t.Fatalf("expected 3 rows after collapse, got %d", len(m.rows))
	}
}

func TestFoldUnfold(t *testing.T) {
	m := makeTestModel(testStatuses())
	// Start on a worktree in repo-a
	if m.rows[m.cursor].kind != rowWorktree || m.rows[m.cursor].status.Repo != "repo-a" {
		t.Fatal("expected initial cursor on repo-a worktree")
	}

	// h collapses the repo and lands cursor on the repo header
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m3 := m2.(Model)
	if m3.rows[m3.cursor].kind != rowRepo {
		t.Fatalf("after h, cursor should be on repo header, got kind=%d", m3.rows[m3.cursor].kind)
	}
	if m3.rows[m3.cursor].repo != "repo-a" {
		t.Errorf("cursor should be on repo-a header, got %q", m3.rows[m3.cursor].repo)
	}

	// l expands it and returns cursor to a worktree inside repo-a
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'l'})
	m5 := m4.(Model)
	if m5.rows[m5.cursor].kind != rowWorktree {
		t.Fatal("after l, cursor should be on a worktree row")
	}
	if m5.rows[m5.cursor].status.Repo != "repo-a" {
		t.Errorf("after l, cursor should be in repo-a, got %q", m5.rows[m5.cursor].status.Repo)
	}
}

func TestCollapsedHeaderReachableAfterNavAway(t *testing.T) {
	m := makeTestModel(testStatuses())

	// Collapse repo-a — cursor lands on repo-a header
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m3 := m2.(Model)
	if m3.rows[m3.cursor].kind != rowRepo || m3.rows[m3.cursor].repo != "repo-a" {
		t.Fatal("after h, cursor should be on repo-a header")
	}

	// Navigate away with j — should reach feature-x in repo-b
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'j'})
	m5 := m4.(Model)
	if m5.rows[m5.cursor].kind != rowWorktree || m5.rows[m5.cursor].status.Repo != "repo-b" {
		t.Fatalf("after j from collapsed header, expected repo-b worktree, got kind=%d repo=%q",
			m5.rows[m5.cursor].kind, m5.rows[m5.cursor].status.Repo)
	}

	// Navigate back with j — should wrap to repo-a header (collapsed, navigable)
	m6, _ := m5.Update(tea.KeyPressMsg{Code: 'j'})
	m7 := m6.(Model)
	if m7.rows[m7.cursor].kind != rowRepo || m7.rows[m7.cursor].repo != "repo-a" {
		t.Fatalf("after wrap, expected repo-a header, got kind=%d repo=%q",
			m7.rows[m7.cursor].kind, m7.rows[m7.cursor].repo)
	}

	// l from repo-a header should expand and land on worktree in repo-a
	m8, _ := m7.Update(tea.KeyPressMsg{Code: 'l'})
	m9 := m8.(Model)
	if m9.rows[m9.cursor].kind != rowWorktree || m9.rows[m9.cursor].status.Repo != "repo-a" {
		t.Error("after l on re-reached header, cursor should be in repo-a worktree")
	}
}

func TestFilterMode(t *testing.T) {
	m := makeTestModel(testStatuses())
	// Enter filter mode
	m2, _ := m.Update(tea.KeyPressMsg{Code: '/'})
	m3 := m2.(Model)
	if !m3.filter.Active {
		t.Error("should be in filtering mode after /")
	}
	// Type a char
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'b'})
	m5 := m4.(Model)
	if m5.filter.Value() != "b" {
		t.Errorf("expected filter 'b', got %q", m5.filter.Value())
	}
	// Esc clears and exits
	m6, _ := m5.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m7 := m6.(Model)
	if m7.filter.Active || m7.filter.Value() != "" {
		t.Error("esc should clear filter and exit filtering mode")
	}
}

func TestFilterModePaste(t *testing.T) {
	m := makeTestModel(testStatuses())
	m2, _ := m.Update(tea.KeyPressMsg{Code: '/'})
	m3 := m2.(Model)

	m4, _ := m3.Update(tea.PasteMsg{Content: "repo-a"})
	m5 := m4.(Model)
	if m5.filter.Value() != "repo-a" {
		t.Errorf("expected filter %q, got %q", "repo-a", m5.filter.Value())
	}
}

func TestAttachOutsideTmux(t *testing.T) {
	os.Unsetenv("TMUX") //nolint:errcheck
	m := makeTestModel(testStatuses())
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if m3.status == "" {
		t.Error("should set status when not in tmux, not quit")
	}
}

func TestDeleteDoubleConfirm(t *testing.T) {
	removeCalled := false
	var removedRepo, removedName string

	m := makeTestModel(testStatuses())
	m.removeFn = func(repo, name string, force bool) error {
		removeCalled = true
		removedRepo = repo
		removedName = name
		return nil
	}

	// First d: arm
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	m3 := m2.(Model)
	if removeCalled {
		t.Error("first d should not delete")
	}
	if m3.pendingDelete == "" {
		t.Error("first d should arm pendingDelete")
	}

	// Non-d key clears arm
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'j'})
	m5 := m4.(Model)
	if m5.pendingDelete != "" {
		t.Error("j should clear pendingDelete")
	}
	if removeCalled {
		t.Error("no delete should have happened")
	}

	// Second d on same row deletes
	m6, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	m7 := m6.(Model)
	m8, cmd := m7.Update(tea.KeyPressMsg{Code: 'd'})
	_ = m8
	if cmd != nil {
		// Execute the command
		msg := cmd()
		_ = msg
	}
	if !removeCalled {
		t.Error("second d should call removeFn")
	}
	if removedRepo != "repo-a" {
		t.Errorf("expected repo 'repo-a', got %q", removedRepo)
	}
	if removedName != "feature-a" {
		t.Errorf("expected name 'feature-a', got %q", removedName)
	}
}

func TestSessionDeleteDoubleConfirm(t *testing.T) {
	removeCalled := false
	var removedRepo, removedName string

	m := makeTestModel(testStatuses())
	m.removeSessionFn = func(repo, name string) error {
		removeCalled = true
		removedRepo = repo
		removedName = name
		return nil
	}

	// First D: arm
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'D'})
	m3 := m2.(Model)
	if removeCalled {
		t.Error("first D should not delete")
	}
	if m3.pendingSessionDelete == "" {
		t.Error("first D should arm pendingSessionDelete")
	}

	// Non-D key clears arm
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'j'})
	m5 := m4.(Model)
	if m5.pendingSessionDelete != "" {
		t.Error("j should clear pendingSessionDelete")
	}
	if removeCalled {
		t.Error("no delete should have happened")
	}

	// d does not confirm a D-armed delete
	m6, _ := m3.Update(tea.KeyPressMsg{Code: 'd'})
	m7 := m6.(Model)
	if m7.pendingSessionDelete != "" {
		t.Error("d should clear pendingSessionDelete instead of confirming it")
	}
	if removeCalled {
		t.Error("d after D must not trigger the session delete")
	}

	// Second D on same row deletes worktree + session
	m8, _ := m.Update(tea.KeyPressMsg{Code: 'D'})
	m9 := m8.(Model)
	m10, cmd := m9.Update(tea.KeyPressMsg{Code: 'D'})
	_ = m10
	if cmd != nil {
		cmd()
	}
	if !removeCalled {
		t.Error("second D should call removeSessionFn")
	}
	if removedRepo != "repo-a" {
		t.Errorf("expected repo 'repo-a', got %q", removedRepo)
	}
	if removedName != "feature-a" {
		t.Errorf("expected name 'feature-a', got %q", removedName)
	}
}

func TestSessionDeleteErrorPropagation(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.removeSessionFn = func(_, _ string) error {
		return fmt.Errorf("no such session")
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'D'})
	m3 := m2.(Model)
	m4, cmd := m3.Update(tea.KeyPressMsg{Code: 'D'})
	if cmd == nil {
		t.Fatal("expected a command after second D")
	}
	msg := cmd()
	m5, _ := m4.(Model).Update(msg)
	m6 := m5.(Model)
	if m6.status == "" {
		t.Error("expected inline status message on session delete error")
	}
	if m6.pendingSessionDelete != "" {
		t.Error("pendingSessionDelete should be cleared after error")
	}
}

func TestDeleteDuplicateNameAcrossRepos(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-x", Repo: "repo-a", Path: "/tmp/ax", WindowActive: false},
		{Name: "feature-x", Repo: "repo-b", Path: "/tmp/bx", WindowActive: false},
	}
	var calledRepo string
	m := makeTestModel(statuses)
	m.removeFn = func(repo, name string, force bool) error {
		calledRepo = repo
		return nil
	}
	// Navigate to the repo-b/feature-x row
	m.rebuildRows()
	for i, r := range m.rows {
		if r.kind == rowWorktree && r.status.Repo == "repo-b" {
			m.cursor = i
			break
		}
	}
	// Double d
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	m3 := m2.(Model)
	m4, cmd := m3.Update(tea.KeyPressMsg{Code: 'd'})
	_ = m4
	if cmd != nil {
		cmd()
	}
	if calledRepo != "repo-b" {
		t.Errorf("expected repo-b to be deleted, got %q", calledRepo)
	}
}

func TestRepairCallsRepairFn(t *testing.T) {
	repairCalled := false
	var repairedRepo, repairedName string
	m := makeTestModel(testStatuses())
	m.repairFn = func(repo, name string, layout worktree.Layout) error {
		repairCalled = true
		repairedRepo = repo
		repairedName = name
		return nil
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'r'})
	if cmd != nil {
		cmd()
	}
	if !repairCalled {
		t.Error("r should call repairFn")
	}
	_ = repairedRepo
	_ = repairedName
}

func TestFilterHidesNonMatchingRows(t *testing.T) {
	m := makeTestModel(testStatuses())
	totalBefore := len(m.rows)
	if totalBefore == 0 {
		t.Fatal("expected rows before filter")
	}

	// Enter filter mode and type "repo-b"
	m2, _ := m.Update(tea.KeyPressMsg{Code: '/'})
	for _, ch := range "repo-b" {
		m2, _ = m2.(Model).Update(tea.KeyPressMsg{Code: ch})
	}
	m3 := m2.(Model)

	// Only repo-b header + feature-x should remain
	if len(m3.rows) >= totalBefore {
		t.Errorf("filter should reduce rows: before=%d after=%d", totalBefore, len(m3.rows))
	}
	for _, r := range m3.rows {
		if r.kind == rowWorktree && r.status.Repo != "repo-b" {
			t.Errorf("filter left row from wrong repo: %s/%s", r.status.Repo, r.status.Name)
		}
	}

	// Esc clears filter and restores all rows
	m4, _ := m3.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m5 := m4.(Model)
	if m5.filter.Value() != "" {
		t.Error("esc should clear filter string")
	}
	if len(m5.rows) != totalBefore {
		t.Errorf("after esc rows should be restored: want %d got %d", totalBefore, len(m5.rows))
	}
}

func TestCursorWrapsAtBoundaries(t *testing.T) {
	m := makeTestModel(testStatuses())
	indices := leafIndices(m.rows)
	if len(indices) < 2 {
		t.Skip("need at least 2 worktree rows")
	}

	// Start at first worktree row
	m.cursor = indices[0]

	// k on first row should wrap to last
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'k'})
	m3 := m2.(Model)
	if m3.cursor != indices[len(indices)-1] {
		t.Errorf("k on first row should wrap to last, got cursor=%d want=%d",
			m3.cursor, indices[len(indices)-1])
	}

	// j on last row should wrap to first
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'j'})
	m5 := m4.(Model)
	if m5.cursor != indices[0] {
		t.Errorf("j on last row should wrap to first, got cursor=%d want=%d",
			m5.cursor, indices[0])
	}
}

func TestDeleteErrorPropagation(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.removeFn = func(_, _ string, _ bool) error {
		return fmt.Errorf("disk full")
	}

	// Arm
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	m3 := m2.(Model)
	// Confirm
	m4, cmd := m3.Update(tea.KeyPressMsg{Code: 'd'})
	_ = m4
	if cmd == nil {
		t.Fatal("expected a command after second d")
	}
	msg := cmd()
	m5, _ := m4.(Model).Update(msg)
	m6 := m5.(Model)
	if m6.status == "" {
		t.Error("expected inline status message on delete error")
	}
	if m6.pendingDelete != "" {
		t.Error("pendingDelete should be cleared after error")
	}
}

func TestDiffFocusMode(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.diffContent = "line0\nline1\nline2\nline3\nline4"
	m.diffFileLines = []int{0, 3}

	// space focuses the diff pane
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	m3 := m2.(Model)
	if !m3.diffFocused {
		t.Fatal("space should focus the diff pane")
	}

	// j scrolls the diff without moving the list cursor
	cursorBefore := m3.cursor
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'j'})
	m5 := m4.(Model)
	if m5.diffScroll != 1 {
		t.Errorf("j should scroll diff by 1, got %d", m5.diffScroll)
	}
	if m5.cursor != cursorBefore {
		t.Error("j while focused must not move the list cursor")
	}

	// ] jumps to the next file header
	m6, _ := m5.Update(tea.KeyPressMsg{Code: ']'})
	m7 := m6.(Model)
	if m7.diffScroll != 3 {
		t.Errorf("] should jump to next file header line 3, got %d", m7.diffScroll)
	}

	// [ jumps back to the previous file header
	m8, _ := m7.Update(tea.KeyPressMsg{Code: '['})
	m9 := m8.(Model)
	if m9.diffScroll != 0 {
		t.Errorf("[ should jump back to header line 0, got %d", m9.diffScroll)
	}

	// G/g hit bottom/top
	m10, _ := m9.Update(tea.KeyPressMsg{Code: 'G'})
	m11 := m10.(Model)
	if m11.diffScroll != 4 {
		t.Errorf("G should scroll to last line, got %d", m11.diffScroll)
	}

	// d while focused must not arm a delete
	m12, _ := m11.Update(tea.KeyPressMsg{Code: 'd'})
	m13 := m12.(Model)
	if m13.pendingDelete != "" {
		t.Error("d while focused must not arm pendingDelete")
	}

	// esc returns focus to the list
	m14, _ := m13.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m15 := m14.(Model)
	if m15.diffFocused {
		t.Error("esc should unfocus the diff pane")
	}
}

func TestDiffFocusRequiresContent(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.diffContent = ""
	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace})
	if m2.(Model).diffFocused {
		t.Error("space must not focus an empty (still loading) diff pane")
	}
}

func TestDiffHeaderShowsComparison(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.diffBase = "main @3e90667"
	m.diffBranch = "feature-a"
	m.diffFiles, m.diffAdded, m.diffRemoved = 2, 96, 14
	m.diffContent = "x"
	right := m.renderRight(100)
	header := strings.Split(ansi.Strip(right), "\n")[0]
	if !strings.Contains(header, "main @3e90667 ← feature-a") {
		t.Errorf("expected comparison label in header, got %q", header)
	}
	if !strings.Contains(header, "±2 +96 -14") {
		t.Errorf("expected stat line in header, got %q", header)
	}
}

func TestSessionRowShowsGuidanceInsteadOfStaleDiff(t *testing.T) {
	m := makeTestModel(testStatuses())
	// Seed a stale worktree diff, as if the cursor sat on a worktree row
	// before moving to the session row below.
	m.diffContent = "stale diff content"
	m.diffBase = "main @3e90667"
	m.diffBranch = "feature-a"
	m.sessions = []worktree.SessionStatus{{Name: "misc"}}
	m.rebuildRows()

	sessionIdx := -1
	for i, r := range m.rows {
		if r.kind == rowSession {
			sessionIdx = i
			break
		}
	}
	if sessionIdx == -1 {
		t.Fatal("expected a rowSession row after rebuildRows")
	}
	m.cursor = sessionIdx

	got := ansi.Strip(m.renderRight(100))
	if strings.Contains(got, "stale diff content") {
		t.Errorf("session row must not render a stale worktree diff, got %q", got)
	}
	if !strings.Contains(got, "no diff") {
		t.Errorf("expected session guidance text, got %q", got)
	}
}

func TestEmptyDashboardShowsGuidance(t *testing.T) {
	m := makeTestModel(nil)

	// Before the first List() result arrives, an empty pane is genuinely
	// still loading.
	if got := ansi.Strip(m.renderRight(100)); !strings.Contains(got, "(loading...)") {
		t.Errorf("before first load, expected loading state, got %q", got)
	}

	// Once List() returns zero worktrees, the pane must switch to create
	// guidance rather than showing "(loading...)" forever (nothing will ever
	// select a worktree to clear it on an empty dashboard).
	m2, _ := m.Update(statusesMsg{statuses: nil})
	got := ansi.Strip(m2.(Model).renderRight(100))
	if strings.Contains(got, "(loading...)") {
		t.Errorf("empty loaded dashboard must not show loading, got %q", got)
	}
	if !strings.Contains(got, "press n to create one") {
		t.Errorf("expected create guidance on empty dashboard, got %q", got)
	}
}

func TestNarrowTerminalNoPanic(t *testing.T) {
	m := makeTestModel(testStatuses())
	// Very narrow terminal
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 30, Height: 20})
	m3 := m2.(Model)
	// Should not panic when rendering
	v := m3.View()
	_ = v
	if m3.rightPaneWidth() < 0 {
		t.Error("rightPaneWidth should not be negative")
	}
}

// --- e toggles left-pane width (Step 3) ---

// TestToggleLeftPaneWidthDefaultAndWide verifies the plain default<->wide
// toggle on a comfortably wide terminal, where safeMaxLeft() never binds.
func TestToggleLeftPaneWidthDefaultAndWide(t *testing.T) {
	m := makeTestModel(testStatuses())
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m3 := m2.(Model)
	if m3.leftPaneWidth != defaultLeftPaneWidth {
		t.Fatalf(
			"expected default left pane width %d after resize, got %d",
			defaultLeftPaneWidth,
			m3.leftPaneWidth,
		)
	}

	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'e'})
	m5 := m4.(Model)
	if !m5.leftPaneWide {
		t.Error("expected leftPaneWide to be true after first e press")
	}
	if want := defaultLeftPaneWidth * 2; m5.leftPaneWidth != want {
		t.Errorf("expected wide left pane width %d, got %d", want, m5.leftPaneWidth)
	}

	m6, _ := m5.Update(tea.KeyPressMsg{Code: 'e'})
	m7 := m6.(Model)
	if m7.leftPaneWide {
		t.Error("expected leftPaneWide to be false after second e press")
	}
	if m7.leftPaneWidth != defaultLeftPaneWidth {
		t.Errorf(
			"expected default left pane width %d after second e press, got %d",
			defaultLeftPaneWidth,
			m7.leftPaneWidth,
		)
	}
}

// TestToggleLeftPaneWidthNarrowTerminalClampsBothTargets is the guard the
// brief names: at width 36, safeMaxLeft() is 21 - below defaultLeftPaneWidth
// (35) - so BOTH toggle targets must clamp to 21, not just the wide one. A
// bare defaultLeftPaneWidth on the narrow branch would hand back 35 (70% of
// the terminal, past the 60% cap) and, on presses past this width, could
// leave rightPaneWidth() at 0 with no way back.
func TestToggleLeftPaneWidthNarrowTerminalClampsBothTargets(t *testing.T) {
	m := makeTestModel(testStatuses())
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 36, Height: 20})
	m3 := m2.(Model)

	if got := m3.safeMaxLeft(); got != 21 {
		t.Fatalf("expected safeMaxLeft() 21 at width 36, got %d", got)
	}
	if m3.leftPaneWidth != 21 {
		t.Fatalf("expected WindowSizeMsg to clamp default target to 21, got %d", m3.leftPaneWidth)
	}

	// First e: default -> wide. Still clamped to 21, not defaultLeftPaneWidth
	// (35) and not defaultLeftPaneWidth*2 (70).
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'e'})
	m5 := m4.(Model)
	if m5.leftPaneWidth != 21 {
		t.Errorf("expected wide target clamped to 21 at width 36, got %d", m5.leftPaneWidth)
	}
	if m5.rightPaneWidth() <= 0 {
		t.Errorf(
			"expected the diff pane to survive (rightPaneWidth > 0), got %d",
			m5.rightPaneWidth(),
		)
	}

	// Second e: wide -> default. Still 21, not 35.
	m6, _ := m5.Update(tea.KeyPressMsg{Code: 'e'})
	m7 := m6.(Model)
	if m7.leftPaneWidth != 21 {
		t.Errorf("expected default target clamped to 21 at width 36, got %d", m7.leftPaneWidth)
	}
	if m7.rightPaneWidth() <= 0 {
		t.Errorf(
			"expected the diff pane to survive (rightPaneWidth > 0), got %d",
			m7.rightPaneWidth(),
		)
	}
}

// TestToggleLeftPaneWidthAfterMouseDrag exercises the reason leftPaneWide is
// a bool rather than a width comparison: a mouse drag can leave leftPaneWidth
// at an arbitrary value that matches neither toggle target, and e must still
// flip to the correct target from the bool's own state, not from comparing
// against the dragged width.
func TestToggleLeftPaneWidthAfterMouseDrag(t *testing.T) {
	m := makeTestModel(testStatuses())
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m3 := m2.(Model)

	// Simulate a mouse-drag resize to a width that is neither toggle target.
	m3.dragging = true
	m4, _ := m3.Update(tea.MouseMotionMsg{X: 50, Y: 0})
	m5 := m4.(Model)
	if m5.leftPaneWidth != 50 {
		t.Fatalf(
			"expected drag to set an arbitrary left pane width of 50, got %d",
			m5.leftPaneWidth,
		)
	}
	if m5.leftPaneWide {
		t.Fatal("drag alone must not flip leftPaneWide")
	}

	// leftPaneWide is still false, so e must move to the wide target, not
	// toggle back to default just because 50 happens to be closer to it.
	m6, _ := m5.Update(tea.KeyPressMsg{Code: 'e'})
	m7 := m6.(Model)
	if !m7.leftPaneWide {
		t.Error("expected leftPaneWide to become true after e post-drag")
	}
	if want := defaultLeftPaneWidth * 2; m7.leftPaneWidth != want {
		t.Errorf(
			"expected e to move the dragged width to the wide target %d, got %d",
			want,
			m7.leftPaneWidth,
		)
	}
}

func TestHelpOverlayShowsDashboardBackground(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.showHelp = true
	out := ansi.Strip(m.renderContent())

	if !strings.Contains(out, "press any key to close") {
		t.Error("expected the help popup to be rendered")
	}
	if !strings.Contains(out, "repo-a") {
		t.Error(
			"expected the dashboard background (repo-a) to remain visible behind the help popup",
		)
	}
	if !strings.Contains(out, "feature-a") {
		t.Error(
			"expected the dashboard background (feature-a) to remain visible behind the help popup",
		)
	}
}

// Create-flow (n → repo-pick → name-input → create) tests live in
// create_flow_test.go, mirroring the create_flow.go/model.go split.

// --- In-progress status feedback (shared with the create flow) ---

func TestDeleteShowsDeletingStatusOnConfirm(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.removeFn = func(_, _ string, _ bool) error { return nil }

	// First d arms the confirm — no "deleting" status yet.
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	m3 := m2.(Model)
	if strings.Contains(m3.status, "deleting") {
		t.Errorf("the arming press should not show a deleting status, got %q", m3.status)
	}

	// Second d confirms and runs the removal — status shows now.
	m4, cmd := m3.Update(tea.KeyPressMsg{Code: 'd'})
	m5 := m4.(Model)
	if !strings.Contains(m5.status, "deleting: ") {
		t.Errorf("the confirming press should show a deleting status, got %q", m5.status)
	}
	if cmd == nil {
		t.Fatal("the confirming press should return the async remove command")
	}
}

func TestDeleteStatusReplacedAfterCompletion(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.removeFn = func(_, _ string, _ bool) error { return nil }

	// Arm, then confirm — the transient "deleting…" status is up.
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	m4, cmd := m2.(Model).Update(tea.KeyPressMsg{Code: 'd'})
	m5 := m4.(Model)
	if !strings.Contains(m5.status, "deleting: ") {
		t.Fatalf("expected a deleting status while removing, got %q", m5.status)
	}
	if cmd == nil {
		t.Fatal("confirming press should return the async remove command")
	}

	// Run the async removal and feed its result back: the transient status must
	// be replaced (this is the bug — statusesMsg used to leave it lingering).
	m6, _ := m5.Update(cmd())
	m7 := m6.(Model)
	if strings.Contains(m7.status, "deleting") {
		t.Errorf("deleting status must not linger after the removal completes, got %q", m7.status)
	}
	if !strings.Contains(m7.status, "removed: ") {
		t.Errorf("expected a removed confirmation after delete, got %q", m7.status)
	}
}

func TestRepairShowsRepairingStatus(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repairFn = func(_, _ string, _ worktree.Layout) error { return nil }

	m2, cmd := m.Update(tea.KeyPressMsg{Code: 'r'})
	m3 := m2.(Model)
	if !strings.Contains(m3.status, "repairing: ") {
		t.Errorf("r should show a repairing status, got %q", m3.status)
	}
	if cmd == nil {
		t.Fatal("r should return the async repair command")
	}
}

func TestAttachShowsRepairingStatusWhenWindowMissing(t *testing.T) {
	t.Setenv("TMUX", "1") // handleAttach only acts inside tmux
	m := makeTestModel(testStatuses())
	// makeTestModel's windowSessionFn reports the window missing, so attach
	// falls into the (slow) auto-repair path.

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if !strings.Contains(m3.status, "repairing: ") {
		t.Errorf("attach with a missing window should show a repairing status, got %q", m3.status)
	}
	if cmd == nil {
		t.Fatal("attach should return a command")
	}
}

func TestAttachNoRepairingStatusWhenWindowPresent(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := makeTestModel(testStatuses())
	m.windowSessionFn = func(string) (string, bool) { return "misc", true } // window present

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if strings.Contains(m3.status, "repairing") {
		t.Errorf(
			"attach with a present window attaches instantly; no repairing status expected, got %q",
			m3.status,
		)
	}
}

func TestAttachClearsAgentStateForSelectedWindow(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := makeTestModel(testStatuses())
	m.windowSessionFn = func(string) (string, bool) { return "misc", true } // window present
	var gotCalls int
	var gotWindow string
	m.clearAgentStateFn = func(window string) error {
		gotCalls++
		gotWindow = window
		return nil
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if gotCalls != 1 {
		t.Fatalf("expected clearAgentStateFn to be called exactly once, got %d", gotCalls)
	}
	// cursor starts on the first row (testStatuses()[0]: repo-a/feature-a).
	want := worktree.GetWindowName("repo-a", "feature-a")
	if gotWindow != want {
		t.Errorf("clearAgentStateFn called with window %q, want %q", gotWindow, want)
	}
}

// mgrWithMockedTmux builds a real *worktree.WorktreeManager backed by a
// mocked Tmux, mirroring the pattern internal/tooling/worktree's own tests
// use (see TestListSessions in worktree_test.go). List() never touches Git
// or Base here: the worktree base dir doesn't exist under the test sandbox,
// so List() takes its os.IsNotExist short-circuit and returns an empty slice
// without error - exactly what these tests need, since they're only
// exercising the session-load side of loadCmd/sessionsLoadCmd.
func mgrWithMockedTmux(mockTmuxBase *commands.MockBaseCommand) *worktree.WorktreeManager {
	return &worktree.WorktreeManager{
		Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
	}
}

func TestInitBatchesWorktreeAndSessionLoads(t *testing.T) {
	// Init()'s tea.Batch only bundles the commands - it doesn't invoke them -
	// so this is safe even though makeTestModel leaves m.mgr nil (calling
	// loadCmd/sessionsLoadCmd's returned closures would nil-dereference, but
	// nothing here calls them).
	m := makeTestModel(nil)
	msg := m.Init()()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected Init() to return a tea.BatchMsg, got %T", msg)
	}
	if len(batch) != 4 {
		t.Fatalf(
			"expected Init() to batch 4 commands (slow load, session load, fast tick, slow tick), got %d",
			len(batch),
		)
	}
}

// hasRow reports whether rows contains a rowSession leaf with the given name
// (name is ignored for other kinds).
func hasRow(rows []row, kind rowKind, name string) bool {
	for _, r := range rows {
		if r.kind != kind {
			continue
		}
		if kind == rowSession && r.session.Name != name {
			continue
		}
		return true
	}
	return false
}

func TestSessionsLoadSuccessPopulatesSessionsAndRows(t *testing.T) {
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		// list-sessions
		commands.ExecCommandResult("misc\t0\n", "", nil),
		// list-windows -a: "misc" hosts no wt- window, so it's a standalone session.
		commands.ExecCommandResult("misc\tshell\n", "", nil),
	)
	m := makeTestModel(testStatuses())
	m.mgr = mgrWithMockedTmux(mockTmuxBase)

	msg := m.sessionsLoadCmd(m.sessionGen)()
	sm, ok := msg.(sessionsMsg)
	if !ok {
		t.Fatalf("expected sessionsMsg on a successful ListSessions, got %T: %+v", msg, msg)
	}

	m2, cmd := m.Update(sm)
	m3 := m2.(Model)
	if cmd != nil {
		t.Error("sessionsMsg handling should not return a command")
	}
	if len(m3.sessions) != 1 || m3.sessions[0].Name != "misc" {
		t.Fatalf("expected m.sessions populated with 'misc', got %+v", m3.sessions)
	}

	if !hasRow(m3.rows, rowSession, "misc") {
		t.Errorf("expected a rowSession leaf for 'misc', got rows: %+v", m3.rows)
	}
	if !hasRow(m3.rows, rowWorktree, "") {
		t.Errorf(
			"expected worktree rows to still render alongside sessions, got rows: %+v",
			m3.rows,
		)
	}
}

func TestSessionsLoadEmptyResultClearsSessionsWithoutWarning(t *testing.T) {
	// A no-server tmux (ListSessions' (nil, nil) case) is a legitimate empty
	// result, not an error - it must replace m.sessions (even a previously
	// populated one) with no status-line warning, distinguishing "no
	// sessions" from "query failed".
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResult(
		"",
		"error connecting to /tmp/tmux-1000/default (No such file or directory)",
		errors.New("exit status 1"),
	)
	m := makeTestModel(testStatuses())
	m.sessions = []worktree.SessionStatus{{Name: "stale"}}
	m.rebuildRows()
	m.mgr = mgrWithMockedTmux(mockTmuxBase)

	msg := m.sessionsLoadCmd(m.sessionGen)()
	sm, ok := msg.(sessionsMsg)
	if !ok {
		t.Fatalf("expected sessionsMsg for the no-server case, got %T: %+v", msg, msg)
	}

	m2, _ := m.Update(sm)
	m3 := m2.(Model)
	if m3.status != "" {
		t.Errorf(
			"expected no status-line warning for a legitimately empty session list, got %q",
			m3.status,
		)
	}
	if len(m3.sessions) != 0 {
		t.Errorf("expected m.sessions cleared to empty, got %+v", m3.sessions)
	}
}

func TestSessionsLoadErrorPreservesLastGoodSessionsAndWarnsStatus(t *testing.T) {
	// Seed a last-good sessions/rows state, as if a previous tick's load
	// succeeded.
	m := makeTestModel(testStatuses())
	m.sessions = []worktree.SessionStatus{{Name: "misc"}}
	m.rebuildRows()

	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResult(
		"",
		"some unexpected tmux failure",
		errors.New("exit status 1"),
	)
	m.mgr = mgrWithMockedTmux(mockTmuxBase)

	msg := m.sessionsLoadCmd(m.sessionGen)()
	sm, ok := msg.(statusMsg)
	if !ok {
		t.Fatalf("expected a statusMsg on a real ListSessions error, got %T: %+v", msg, msg)
	}
	if !strings.Contains(string(sm), "failed to list sessions: ") {
		t.Errorf("expected the 'failed to list sessions: ' prefix, got %q", sm)
	}

	m2, _ := m.Update(sm)
	m3 := m2.(Model)
	if !strings.Contains(m3.status, "failed to list sessions: ") {
		t.Errorf("expected the status line to show the session-load warning, got %q", m3.status)
	}
	if len(m3.sessions) != 1 || m3.sessions[0].Name != "misc" {
		t.Errorf(
			"expected the last-good sessions to be preserved (not blanked), got %+v",
			m3.sessions,
		)
	}

	if !hasRow(m3.rows, rowWorktree, "") {
		t.Errorf(
			"expected worktree rows to still render despite the session-load failure, got rows: %+v",
			m3.rows,
		)
	}
	if !hasRow(m3.rows, rowSession, "misc") {
		t.Errorf("expected the last-good session row to still render, got rows: %+v", m3.rows)
	}
}

// --- Step 7: status dot, chevron/badge, hint bar, help ---

func TestRenderLeftRepoHeaderShowsTreeCountBadge(t *testing.T) {
	// testStatuses(): repo-a has 2 worktrees, repo-b has 1 - exercises both
	// the plural and singular badge wording.
	m := makeTestModel(testStatuses())
	out := ansi.Strip(m.renderLeft(40))
	lines := strings.Split(out, "\n")
	if len(lines) != len(m.rows) {
		t.Fatalf("expected %d rendered lines, got %d", len(m.rows), len(lines))
	}

	var repoALine, repoBLine string
	for i, r := range m.rows {
		if r.kind == rowRepo && r.repo == "repo-a" {
			repoALine = lines[i]
		}
		if r.kind == rowRepo && r.repo == "repo-b" {
			repoBLine = lines[i]
		}
	}
	if !strings.Contains(repoALine, "2 trees") {
		t.Errorf("expected repo-a header to show '2 trees' badge, got %q", repoALine)
	}
	if !strings.Contains(repoBLine, "1 tree") {
		t.Errorf("expected repo-b header to show '1 tree' badge (singular), got %q", repoBLine)
	}
	if strings.Contains(repoBLine, "1 trees") {
		t.Errorf("expected repo-b header to use singular 'tree', not plural, got %q", repoBLine)
	}

	// Regression guard: testStatuses() sets no AgentState anywhere, so both
	// headers must aggregate to "" and render the dim "no session" glyph
	// (○) rather than a false "running" green dot - and the dot column must
	// not throw off the right-aligned badge (line width still == 40).
	if !strings.Contains(repoALine, "○") {
		t.Errorf("expected repo-a header with no reporting children to show ○, got %q", repoALine)
	}
	if !strings.Contains(repoBLine, "○") {
		t.Errorf("expected repo-b header with no reporting children to show ○, got %q", repoBLine)
	}
	if w := ansi.StringWidth(repoALine); w != 40 {
		t.Errorf("expected repo-a header line width 40, got %d (%q)", w, repoALine)
	}
	if w := ansi.StringWidth(repoBLine); w != 40 {
		t.Errorf("expected repo-b header line width 40, got %d (%q)", w, repoBLine)
	}
}

// TestRenderLeftRepoHeaderShowsAgentStateGlyph verifies ADR-0008 Step 6: a
// collapsed repo header aggregates its children's AgentState (via
// worktree.AggregateAgentState / buildRows) and renders the resulting glyph,
// so collapsing a repo with a blocked worktree still shows "!" instead of
// hiding it.
func TestRenderLeftRepoHeaderShowsAgentStateGlyph(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Name:         "feature-a",
			Repo:         "repo-a",
			WindowActive: true,
			AgentState:   worktree.AgentStateBlocked,
		},
		{
			Name:         "feature-b",
			Repo:         "repo-a",
			WindowActive: true,
			AgentState:   worktree.AgentStateIdle,
		},
	}
	m := makeTestModel(statuses)
	m.collapsed = map[string]bool{"repo-a": true}
	m.rebuildRows()

	out := ansi.Strip(m.renderLeft(40))
	lines := strings.Split(out, "\n")
	if len(lines) != len(m.rows) {
		t.Fatalf("expected %d rendered lines, got %d", len(m.rows), len(lines))
	}
	if len(m.rows) != 1 || m.rows[0].kind != rowRepo {
		t.Fatalf("expected a single collapsed repo header row, got rows: %+v", m.rows)
	}

	repoLine := lines[0]
	if !strings.Contains(repoLine, "!") {
		t.Errorf(
			"expected collapsed repo-a header with a blocked child to show '!', got %q",
			repoLine,
		)
	}
	if w := ansi.StringWidth(repoLine); w != 40 {
		t.Errorf("expected repo header line width 40, got %d (%q)", w, repoLine)
	}
	if !strings.Contains(repoLine, "2 trees") {
		t.Errorf("expected collapsed repo-a header to still show '2 trees' badge, got %q", repoLine)
	}
}

func TestRenderLeftSessionRowShowsGlyphAndLabel(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.sessions = testSessions() // notes: Attached=true, scratch: Attached=false
	m.rebuildRows()

	out := ansi.Strip(m.renderLeft(40))
	lines := strings.Split(out, "\n")
	if len(lines) != len(m.rows) {
		t.Fatalf("expected %d rendered lines, got %d", len(m.rows), len(lines))
	}

	var notesLine, scratchLine string
	for i, r := range m.rows {
		if r.kind == rowSession && r.session.Name == "notes" {
			notesLine = lines[i]
		}
		if r.kind == rowSession && r.session.Name == "scratch" {
			scratchLine = lines[i]
		}
	}
	if notesLine == "" || scratchLine == "" {
		t.Fatalf("expected both session rows to render, got rows: %+v", m.rows)
	}
	// Sessions use squares (■ attached / □ detached), a different shape from
	// the ●/○ circles worktree rows use.
	if !strings.Contains(notesLine, "■") {
		t.Errorf("expected attached session 'notes' to show the filled square ■, got %q", notesLine)
	}
	if strings.ContainsAny(notesLine, "●○") {
		t.Errorf("session row must not use a worktree circle glyph, got %q", notesLine)
	}
	if !strings.Contains(scratchLine, "□") {
		t.Errorf(
			"expected detached session 'scratch' to show the hollow square □, got %q",
			scratchLine,
		)
	}
	if !strings.Contains(notesLine, "session") {
		t.Errorf("expected session row to show the 'session' label, got %q", notesLine)
	}
	if !strings.Contains(scratchLine, "session") {
		t.Errorf("expected session row to show the 'session' label, got %q", scratchLine)
	}
}

// TestRenderLeftSessionRowCursorAndArmedStyling checks the raw (non-stripped)
// ANSI prefixes lipgloss emits for Selected (bg ANSI 4: "97;44m") vs Armed
// (bg ANSI 1: "97;41m") - captured once from the actual palette rather than
// hardcoded from reading the source, so this fails loudly if the palette's
// colors ever change instead of silently matching stale values.
func TestRenderLeftSessionRowCursorAndArmedStyling(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	m.rebuildRows()
	selectedPrefix := strings.SplitN(m.palette.Selected.Render("X"), "X", 2)[0]
	armedPrefix := strings.SplitN(m.palette.Armed.Render("X"), "X", 2)[0]

	idx := -1
	for i, r := range m.rows {
		if r.kind == rowSession && r.session.Name == "notes" {
			idx = i
		}
	}
	if idx == -1 {
		t.Fatal("expected a 'notes' session row")
	}
	m.cursor = idx

	rawLines := strings.Split(m.renderLeft(40), "\n")
	selectedLine := rawLines[idx]
	if !strings.Contains(selectedLine, selectedPrefix) {
		t.Errorf(
			"expected cursor-selected (non-armed) session row to use the Selected style, got %q",
			selectedLine,
		)
	}
	if strings.Contains(selectedLine, armedPrefix) {
		t.Errorf(
			"expected cursor-selected (non-armed) session row not to use the Armed style, got %q",
			selectedLine,
		)
	}

	m.pendingKillSession = "notes"
	rawLines2 := strings.Split(m.renderLeft(40), "\n")
	armedLine := rawLines2[idx]
	if !strings.Contains(armedLine, armedPrefix) {
		t.Errorf(
			"expected armed (pendingKillSession match) session row to use the Armed style, got %q",
			armedLine,
		)
	}
}

// TestRenderLeftSessionRowNoAgentStateShowsSquare is a regression guard: a
// session with AgentState == "" (no agent has ever reported on its panes)
// must keep rendering the original attached-only square glyph (■/□), not the
// agent-state vocabulary (●/◆/!/✕). This is the "keep today's behavior
// exactly" branch from ADR-0008 Step 5.
func TestRenderLeftSessionRowNoAgentStateShowsSquare(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.sessions = []worktree.SessionStatus{
		{Name: "scratch", Attached: false, AgentState: ""},
		{Name: "notes", Attached: true, AgentState: ""},
	}
	m.rebuildRows()

	out := ansi.Strip(m.renderLeft(40))
	lines := strings.Split(out, "\n")

	var notesLine, scratchLine string
	for i, r := range m.rows {
		if r.kind == rowSession && r.session.Name == "notes" {
			notesLine = lines[i]
		}
		if r.kind == rowSession && r.session.Name == "scratch" {
			scratchLine = lines[i]
		}
	}
	if notesLine == "" || scratchLine == "" {
		t.Fatalf("expected both session rows to render, got rows: %+v", m.rows)
	}
	if !strings.Contains(notesLine, "■") {
		t.Errorf("expected attached session with no agent state to show ■, got %q", notesLine)
	}
	if !strings.Contains(scratchLine, "□") {
		t.Errorf("expected detached session with no agent state to show □, got %q", scratchLine)
	}
	if strings.ContainsAny(notesLine, "●◆!✕") || strings.ContainsAny(scratchLine, "●◆!✕") {
		t.Errorf(
			"session rows with AgentState==\"\" must not use the agent-state glyphs, got notes=%q scratch=%q",
			notesLine,
			scratchLine,
		)
	}
}

// TestRenderLeftSessionRowShowsAgentStateGlyph verifies that once a session
// has AgentState set (a pane reported at least once), the row switches to
// StatusGlyph/StatusDot — the same agent-state vocabulary rowWorktree uses —
// instead of the SessionGlyph/SessionDot square.
func TestRenderLeftSessionRowShowsAgentStateGlyph(t *testing.T) {
	testCases := []struct {
		agentState string
		wantGlyph  string
	}{
		{agentState: worktree.AgentStateIdle, wantGlyph: "◆"},
		{agentState: worktree.AgentStateBlocked, wantGlyph: "!"},
		{agentState: worktree.AgentStateError, wantGlyph: "✕"},
		{agentState: worktree.AgentStateBusy, wantGlyph: "●"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("agentState_%s", tc.agentState), func(t *testing.T) {
			m := makeTestModel(testStatuses())
			m.sessions = []worktree.SessionStatus{
				{Name: "notes", Attached: true, AgentState: tc.agentState},
			}
			m.rebuildRows()

			out := ansi.Strip(m.renderLeft(40))
			lines := strings.Split(out, "\n")

			var notesLine string
			for i, r := range m.rows {
				if r.kind == rowSession && r.session.Name == "notes" {
					notesLine = lines[i]
				}
			}
			if notesLine == "" {
				t.Fatal("expected a 'notes' session row")
			}
			if !strings.Contains(notesLine, tc.wantGlyph) {
				t.Errorf(
					"agent state %q: expected glyph %q in session row, got %q",
					tc.agentState, tc.wantGlyph, notesLine,
				)
			}
			if strings.ContainsAny(notesLine, "■□") {
				t.Errorf(
					"agent state %q: session row must not fall back to the square glyph, got %q",
					tc.agentState, notesLine,
				)
			}
			// The "session" label must survive the glyph swap.
			if !strings.Contains(notesLine, "session") {
				t.Errorf(
					"expected session row to still show the 'session' label, got %q",
					notesLine,
				)
			}
		})
	}
}

// TestRenderLeftSessionRowAgentStateSelectedStyling mirrors
// TestRenderLeftSessionRowCursorAndArmedStyling but for a session that has
// reported an agent state: the cursor branch must swap in StatusGlyph while
// still nesting inside the Selected style, and the Armed (pending-kill)
// styling must behave the same regardless of agent state.
func TestRenderLeftSessionRowAgentStateSelectedStyling(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.sessions = []worktree.SessionStatus{
		{Name: "notes", Attached: true, AgentState: worktree.AgentStateIdle},
	}
	m.rebuildRows()

	idx := -1
	for i, r := range m.rows {
		if r.kind == rowSession && r.session.Name == "notes" {
			idx = i
		}
	}
	if idx == -1 {
		t.Fatal("expected a 'notes' session row")
	}
	m.cursor = idx

	selectedPrefix := strings.SplitN(m.palette.Selected.Render("X"), "X", 2)[0]
	armedPrefix := strings.SplitN(m.palette.Armed.Render("X"), "X", 2)[0]

	rawLines := strings.Split(m.renderLeft(40), "\n")
	selectedLine := rawLines[idx]
	strippedLine := ansi.Strip(selectedLine)
	if !strings.Contains(strippedLine, "◆") {
		t.Errorf("expected idle session row to show ◆ when selected, got %q", strippedLine)
	}
	if !strings.Contains(selectedLine, selectedPrefix) {
		t.Errorf("expected selected session row to use the Selected style, got %q", selectedLine)
	}

	m.pendingKillSession = "notes"
	rawLines2 := strings.Split(m.renderLeft(40), "\n")
	armedLine := rawLines2[idx]
	strippedArmedLine := ansi.Strip(armedLine)
	if !strings.Contains(strippedArmedLine, "◆") {
		t.Errorf(
			"expected armed session row to keep showing the agent-state glyph ◆, got %q",
			strippedArmedLine,
		)
	}
	if !strings.Contains(armedLine, armedPrefix) {
		t.Errorf("expected armed session row to use the Armed style, got %q", armedLine)
	}
}

// TestRenderLeftWorktreeRowShowsAgentStateGlyph verifies that unselected worktree
// rows render the correct glyph for each agent state by checking the stripped output.
// This mirrors TestRenderLeftSessionRowShowsGlyphAndLabel but for worktrees with
// agent states (blocked, idle, error, busy, or "").
func TestRenderLeftWorktreeRowShowsAgentStateGlyph(t *testing.T) {
	testCases := []struct {
		agentState string
		wantGlyph  string
	}{
		{agentState: worktree.AgentStateBlocked, wantGlyph: "!"},
		{agentState: worktree.AgentStateIdle, wantGlyph: "◆"},
		{agentState: worktree.AgentStateError, wantGlyph: "✕"},
		{agentState: worktree.AgentStateBusy, wantGlyph: "●"},
		{agentState: "", wantGlyph: "●"}, // No agent state renders as running
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("agentState_%s", tc.agentState), func(t *testing.T) {
			statuses := []worktree.WorktreeStatus{
				{
					Name:         "test-wt",
					Repo:         "test-repo",
					Path:         "/tmp/test",
					TmuxWindow:   "wt-test",
					WindowActive: true,
					AgentState:   tc.agentState,
				},
			}
			m := makeTestModel(statuses)
			m.rebuildRows()

			out := ansi.Strip(m.renderLeft(40))
			lines := strings.Split(out, "\n")

			// Find the worktree row (skipping the repo header, which is the first row)
			var wtLine string
			for i, r := range m.rows {
				if r.kind == rowWorktree && r.status.Name == "test-wt" {
					if i < len(lines) {
						wtLine = lines[i]
					}
				}
			}

			if wtLine == "" {
				t.Fatal("expected a worktree row for 'test-wt'")
			}

			if !strings.Contains(wtLine, tc.wantGlyph) {
				t.Errorf(
					"agent state %q: expected glyph %q in unselected worktree row, got %q",
					tc.agentState, tc.wantGlyph, wtLine,
				)
			}
		})
	}
}

// TestRenderLeftWorktreeRowAgentStateSelectedAndStyling verifies that selected
// worktree rows (with cursor on them) correctly render agent-state glyphs nested
// inside the Selected style, matching the unstyled glyph checked above and
// confirming the Selected prefix is present. This mirrors
// TestRenderLeftSessionRowCursorAndArmedStyling.
func TestRenderLeftWorktreeRowAgentStateSelectedAndStyling(t *testing.T) {
	testCases := []struct {
		agentState string
		wantGlyph  string
	}{
		{agentState: worktree.AgentStateBlocked, wantGlyph: "!"},
		{agentState: worktree.AgentStateIdle, wantGlyph: "◆"},
		{agentState: worktree.AgentStateError, wantGlyph: "✕"},
		{agentState: worktree.AgentStateBusy, wantGlyph: "●"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("agentState_%s_selected", tc.agentState), func(t *testing.T) {
			statuses := []worktree.WorktreeStatus{
				{
					Name:         "test-wt",
					Repo:         "test-repo",
					Path:         "/tmp/test",
					TmuxWindow:   "wt-test",
					WindowActive: true,
					AgentState:   tc.agentState,
				},
			}
			m := makeTestModel(statuses)
			m.rebuildRows()

			// Find the worktree row index
			wtIdx := -1
			for i, r := range m.rows {
				if r.kind == rowWorktree && r.status.Name == "test-wt" {
					wtIdx = i
					break
				}
			}
			if wtIdx == -1 {
				t.Fatal("expected a worktree row for 'test-wt'")
			}

			m.cursor = wtIdx
			selectedPrefix := strings.SplitN(m.palette.Selected.Render("X"), "X", 2)[0]

			// Render and check the stripped output contains the expected glyph
			rawLines := strings.Split(m.renderLeft(40), "\n")
			strippedLines := strings.Split(ansi.Strip(m.renderLeft(40)), "\n")

			if wtIdx >= len(strippedLines) {
				t.Fatal("worktree row index out of bounds")
			}

			strippedLine := strippedLines[wtIdx]
			rawLine := rawLines[wtIdx]

			// Check that the glyph appears in the stripped output
			if !strings.Contains(strippedLine, tc.wantGlyph) {
				t.Errorf(
					"agent state %q (selected): expected glyph %q in stripped output, got %q",
					tc.agentState, tc.wantGlyph, strippedLine,
				)
			}

			// Check that the raw output contains the Selected style prefix,
			// proving the glyph was successfully nested inside Selected.Render()
			if !strings.Contains(rawLine, selectedPrefix) {
				t.Errorf(
					"agent state %q (selected): expected Selected style prefix in raw output, got %q",
					tc.agentState,
					rawLine,
				)
			}
		})
	}
}

// TestRenderLeftWorktreeRowNoAgentStateRendersDot verifies that a worktree row
// with WindowActive but no agent state (AgentState == "") still renders the
// plain running dot "●", guarding against regression on the empty-string fallback.
func TestRenderLeftWorktreeRowNoAgentStateRendersDot(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Name:         "test-wt",
			Repo:         "test-repo",
			Path:         "/tmp/test",
			TmuxWindow:   "wt-test",
			WindowActive: true,
			AgentState:   "",
		},
	}
	m := makeTestModel(statuses)
	m.rebuildRows()

	out := ansi.Strip(m.renderLeft(40))
	lines := strings.Split(out, "\n")

	// Find the worktree row
	var wtLine string
	for i, r := range m.rows {
		if r.kind == rowWorktree && r.status.Name == "test-wt" {
			if i < len(lines) {
				wtLine = lines[i]
			}
		}
	}

	if wtLine == "" {
		t.Fatal("expected a worktree row for 'test-wt'")
	}

	// Must contain the plain running dot, not any of the agent-state glyphs
	if !strings.Contains(wtLine, "●") {
		t.Errorf(
			"expected unselected row with no agent state to show running dot ●, got %q",
			wtLine,
		)
	}
	if strings.ContainsAny(wtLine, "!◆✕") {
		t.Errorf(
			"expected unselected row with no agent state to not show agent-state glyphs, got %q",
			wtLine,
		)
	}
}

func TestRenderHintShowsArmedKillSessionHint(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.pendingKillSession = "notes"
	out := ansi.Strip(m.renderHint(200))
	if !strings.Contains(out, "press d again to kill notes") {
		t.Errorf("expected armed-kill-session hint mentioning 'notes', got %q", out)
	}
}

func TestRenderHintDefaultListIncludesNewSession(t *testing.T) {
	m := makeTestModel(testStatuses())
	out := ansi.Strip(m.renderHint(200))
	if !strings.Contains(out, "new session") {
		t.Errorf("expected default hint bar to include 's: new session', got %q", out)
	}
}

func TestRenderHelpPopupIncludesSessionKeys(t *testing.T) {
	m := makeTestModel(testStatuses())
	out := ansi.Strip(m.renderHelpPopup())
	if !strings.Contains(out, "create a new tmux session") {
		t.Errorf("expected help popup to document the 's' key for new session, got:\n%s", out)
	}
	if !strings.Contains(out, "switch to it") {
		t.Errorf(
			"expected help popup's enter entry to clarify session-row behavior, got:\n%s",
			out,
		)
	}
	if !strings.Contains(out, "kill it") {
		t.Errorf(
			"expected help popup's d-d entry to clarify session-row kill behavior, got:\n%s",
			out,
		)
	}
}

func TestRenderHintDefaultListIncludesReview(t *testing.T) {
	m := makeTestModel(testStatuses())
	out := ansi.Strip(m.renderHint(200))
	if !strings.Contains(out, "R") {
		t.Errorf("expected default hint bar to include 'R: review', got %q", out)
	}
	if !strings.Contains(out, "review") {
		t.Errorf("expected default hint bar to include 'R: review', got %q", out)
	}
}

// TestRenderHintWidthConstraintAt80 documents the actual, verified behavior of
// the default hint bar at 80 columns: HintBar joins every "key desc" pair with
// " · " and truncates the WHOLE joined string to the given width (see
// hintbar.go). Summing the pre-existing ~14 entries' character counts already
// exceeds 150 characters, well past 80 — so the hint bar was ALREADY
// truncating well before "q" (and several other entries) at 80 columns,
// before this task ever touched it. The new "R" entry lands even further into
// the already-invisible-at-80 tail, so it also does not appear at width 80.
// This is a pre-existing limitation of HintBar's truncate-the-whole-string
// design, not something this task's "R" addition caused or is responsible for
// fixing (fixing it is explicitly out of this task's scope per the brief).
func TestRenderHintWidthConstraintAt80(t *testing.T) {
	m := makeTestModel(testStatuses())

	out80 := ansi.Strip(m.renderHint(80))
	if out80 == "" {
		t.Fatal("expected non-empty hint bar at width 80")
	}
	// Verified actual output at width 80: the string is truncated mid-word
	// before reaching "d", "D", "r", "R", "/", "?", or "q" — so the new "R
	// review" entry does not appear. This is expected, pre-existing
	// truncation, not a regression from adding "R".
	if strings.Contains(out80, "R review") {
		t.Errorf(
			"expected 'R review' to be truncated away at width 80 (pre-existing hint bar overflow), but it was present: %q",
			out80,
		)
	}

	// At a comfortably wide width (matching the sibling tests), the "R"
	// entry IS present — this is the width where the feature is actually
	// usable/discoverable, and it's the meaningful regression-catching
	// assertion for this task.
	out200 := ansi.Strip(m.renderHint(200))
	if !strings.Contains(out200, "R review") {
		t.Errorf("expected default hint bar at width 200 to include 'R review', got %q", out200)
	}
}

func TestRenderHelpPopupIncludesReview(t *testing.T) {
	m := makeTestModel(testStatuses())
	out := ansi.Strip(m.renderHelpPopup())
	if !strings.Contains(out, "R") {
		t.Errorf("expected help popup to include 'R' key, got:\n%s", out)
	}
	if !strings.Contains(out, "kick a review") {
		t.Errorf("expected help popup to include the review entry, got:\n%s", out)
	}
}

func TestRenderHintDefaultListIncludesWidthToggle(t *testing.T) {
	m := makeTestModel(testStatuses())
	out := ansi.Strip(m.renderHint(200))
	if !strings.Contains(out, "e width") {
		t.Errorf("expected default hint bar to include 'e width', got %q", out)
	}
}

func TestRenderHelpPopupIncludesWidthToggle(t *testing.T) {
	m := makeTestModel(testStatuses())
	out := ansi.Strip(m.renderHelpPopup())
	if !strings.Contains(out, "toggle left pane width") {
		t.Errorf("expected help popup to include the width-toggle entry, got:\n%s", out)
	}
}

// --- h/l pane-row collapse (ADR-0008 Step 8) ---

// paneRowTestStatuses returns a single repo with one worktree whose window
// qualifies for pane rows (2 stateful panes) - used by the h/l pane-collapse
// tests below.
func paneRowTestStatuses() []worktree.WorktreeStatus {
	return []worktree.WorktreeStatus{
		{
			Name:       "feature-a",
			Repo:       "repo-a",
			TmuxWindow: "wt-feature-a",
			Panes: []tmux.PaneState{
				{
					PaneID:         "%1",
					PaneIndex:      "0",
					CurrentCommand: "claude",
					State:          worktree.AgentStateBusy,
				},
				{
					PaneID:         "%2",
					PaneIndex:      "1",
					CurrentCommand: "zsh",
					State:          worktree.AgentStateIdle,
				},
			},
		},
	}
}

// paneRowTestSessions mirrors paneRowTestStatuses for a standalone session
// parent - the plan's own manual test case ("a session with two agents
// expands to two pane rows").
func paneRowTestSessions() []worktree.SessionStatus {
	return []worktree.SessionStatus{
		{
			Name: "notes",
			Panes: []tmux.PaneState{
				{
					PaneID:         "%1",
					PaneIndex:      "0",
					CurrentCommand: "claude",
					State:          worktree.AgentStateBusy,
				},
				{
					PaneID:         "%2",
					PaneIndex:      "1",
					CurrentCommand: "claude",
					State:          worktree.AgentStateBlocked,
				},
			},
		},
	}
}

func TestLExpandsCollapsedQualifyingWorktreeRow(t *testing.T) {
	m := makeTestModel(paneRowTestStatuses())
	key := "worktree:wt-feature-a"
	m.collapsed[key] = true
	m.rebuildRows()
	if m.rows[m.cursor].kind != rowWorktree {
		t.Fatalf(
			"expected cursor on the worktree row while collapsed, got kind=%d",
			m.rows[m.cursor].kind,
		)
	}

	mi, _ := m.Update(tea.KeyPressMsg{Code: 'l'})
	m2 := mi.(Model)

	if m2.collapsed[key] {
		t.Error("expected the worktree pane key to be expanded (false) after l")
	}
	if m2.rows[m2.cursor].kind != rowPane || m2.rows[m2.cursor].pane.PaneID != "%1" {
		t.Fatalf("expected cursor to land on the first pane row (%%1), got kind=%d row=%+v",
			m2.rows[m2.cursor].kind, m2.rows[m2.cursor])
	}
}

func TestHCollapsesExpandedQualifyingWorktreeRowCursorStays(t *testing.T) {
	m := makeTestModel(paneRowTestStatuses())
	if m.rows[m.cursor].kind != rowWorktree {
		t.Fatalf("expected cursor on the worktree row, got kind=%d", m.rows[m.cursor].kind)
	}
	parentIdx := m.cursor

	mi, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m2 := mi.(Model)

	key := "worktree:wt-feature-a"
	if !m2.collapsed[key] {
		t.Error("expected the worktree pane key to be collapsed (true) after h")
	}
	if m2.cursor != parentIdx {
		t.Errorf("expected cursor to stay on the parent row index %d, got %d", parentIdx, m2.cursor)
	}
	if m2.rows[m2.cursor].kind != rowWorktree {
		t.Fatalf("expected cursor still on the worktree row, got kind=%d", m2.rows[m2.cursor].kind)
	}
	for _, r := range m2.rows {
		if r.kind == rowPane {
			t.Errorf("expected no pane rows visible after collapse, got %+v", m2.rows)
		}
	}
}

func TestHOnPaneRowCollapsesParentAndMovesCursorToParent(t *testing.T) {
	m := makeTestModel(paneRowTestStatuses())
	// Move cursor down onto the first pane row.
	mi, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	m = mi.(Model)
	if m.rows[m.cursor].kind != rowPane {
		t.Fatalf(
			"expected cursor to move onto a pane row via j, got kind=%d",
			m.rows[m.cursor].kind,
		)
	}

	mi, _ = m.Update(tea.KeyPressMsg{Code: 'h'})
	m2 := mi.(Model)

	key := "worktree:wt-feature-a"
	if !m2.collapsed[key] {
		t.Error("expected the worktree pane key to be collapsed after h on a pane row")
	}
	if m2.rows[m2.cursor].kind != rowWorktree ||
		m2.rows[m2.cursor].status.TmuxWindow != "wt-feature-a" {
		t.Fatalf(
			"expected cursor to move to the enclosing parent worktree row, got kind=%d row=%+v",
			m2.rows[m2.cursor].kind,
			m2.rows[m2.cursor],
		)
	}
}

func TestLExpandsCollapsedQualifyingSessionRow(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = paneRowTestSessions()
	key := "session:notes"
	m.collapsed[key] = true
	m.rebuildRows()
	if m.rows[m.cursor].kind != rowSession {
		t.Fatalf(
			"expected cursor on the session row while collapsed, got kind=%d",
			m.rows[m.cursor].kind,
		)
	}

	mi, _ := m.Update(tea.KeyPressMsg{Code: 'l'})
	m2 := mi.(Model)

	if m2.collapsed[key] {
		t.Error("expected the session pane key to be expanded (false) after l")
	}
	if m2.rows[m2.cursor].kind != rowPane || m2.rows[m2.cursor].pane.PaneID != "%1" {
		t.Fatalf("expected cursor to land on the first pane row (%%1), got kind=%d row=%+v",
			m2.rows[m2.cursor].kind, m2.rows[m2.cursor])
	}
}

func TestHCollapsesExpandedQualifyingSessionRowCursorStays(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = paneRowTestSessions()
	m.rebuildRows()
	if m.rows[m.cursor].kind != rowSession {
		t.Fatalf("expected cursor on the session row, got kind=%d", m.rows[m.cursor].kind)
	}
	parentIdx := m.cursor

	mi, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m2 := mi.(Model)

	key := "session:notes"
	if !m2.collapsed[key] {
		t.Error("expected the session pane key to be collapsed (true) after h")
	}
	if m2.cursor != parentIdx || m2.rows[m2.cursor].kind != rowSession {
		t.Fatal("expected cursor to stay on the session row after h")
	}
}

// TestHOnNonQualifyingWorktreeOnlyCollapsesRepo is a regression guard: a
// worktree row that doesn't qualify for pane rows (fewer than 2 stateful
// panes, or none at all here) must fall through to the existing repo-collapse
// behavior unchanged, and must never write a "worktree:"/"session:" key into
// the collapsed map.
func TestHOnNonQualifyingWorktreeOnlyCollapsesRepo(t *testing.T) {
	m := makeTestModel(testStatuses()) // no Panes set on any status
	mi, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m2 := mi.(Model)

	if !m2.collapsed["repo-a"] {
		t.Error("expected repo-a to be collapsed (existing behavior)")
	}
	for k := range m2.collapsed {
		if strings.HasPrefix(k, "worktree:") || strings.HasPrefix(k, "session:") {
			t.Errorf(
				"did not expect a pane-parent collapse key to be set for a non-qualifying row, got %q",
				k,
			)
		}
	}
}

// TestLOnNonQualifyingWorktreeOnlyAffectsRepo mirrors the h guard above for l.
func TestLOnNonQualifyingWorktreeOnlyAffectsRepo(t *testing.T) {
	m := makeTestModel(testStatuses())
	// Collapse via the real handler (not a direct map write) so the cursor
	// lands on the repo header the same way TestFoldUnfold verifies.
	mi, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m = mi.(Model)
	if m.rows[m.cursor].kind != rowRepo || m.rows[m.cursor].repo != "repo-a" {
		t.Fatalf(
			"expected cursor on the collapsed repo-a header, got kind=%d",
			m.rows[m.cursor].kind,
		)
	}

	mi, _ = m.Update(tea.KeyPressMsg{Code: 'l'})
	m2 := mi.(Model)

	if m2.collapsed["repo-a"] {
		t.Error("expected repo-a to be expanded (existing behavior)")
	}
	if m2.rows[m2.cursor].kind != rowWorktree || m2.rows[m2.cursor].status.Repo != "repo-a" {
		t.Fatalf(
			"expected cursor to land on a repo-a worktree row, got kind=%d",
			m2.rows[m2.cursor].kind,
		)
	}
	for k := range m2.collapsed {
		if strings.HasPrefix(k, "worktree:") || strings.HasPrefix(k, "session:") {
			t.Errorf(
				"did not expect a pane-parent collapse key to be set for a non-qualifying row, got %q",
				k,
			)
		}
	}
}

// TestHLNoOpOnNonQualifyingSessionRow is a regression guard: h/l on a session
// row that doesn't qualify for pane rows remain no-ops, matching the pre-Task-8
// baseline (selectedStatus returns ok=false for session rows, and a session
// row is never rowRepo).
func TestHLNoOpOnNonQualifyingSessionRow(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = []worktree.SessionStatus{{Name: "misc"}}
	m.rebuildRows()
	if m.rows[m.cursor].kind != rowSession {
		t.Fatalf("expected cursor on the session row, got kind=%d", m.rows[m.cursor].kind)
	}
	cursorBefore := m.cursor
	collapsedLenBefore := len(m.collapsed)

	mi, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m2 := mi.(Model)
	if m2.cursor != cursorBefore || m2.rows[m2.cursor].kind != rowSession {
		t.Error("expected h to be a no-op on a non-qualifying session row")
	}
	if len(m2.collapsed) != collapsedLenBefore {
		t.Error("expected h not to add any collapse-map entries for a non-qualifying session row")
	}

	mi, _ = m2.Update(tea.KeyPressMsg{Code: 'l'})
	m3 := mi.(Model)
	if m3.cursor != m2.cursor || m3.rows[m3.cursor].kind != rowSession {
		t.Error("expected l to be a no-op on a non-qualifying session row")
	}
	if len(m3.collapsed) != collapsedLenBefore {
		t.Error("expected l not to add any collapse-map entries for a non-qualifying session row")
	}
}

// --- Task 8.5: expand/collapse chevron on qualifying pane-parent rows ---

// qualifyingPanes returns 2 stateful panes - the minimum that makes
// qualifiesForPaneRows true (ADR-0008 §3).
func qualifyingPanes() []tmux.PaneState {
	return []tmux.PaneState{
		{PaneID: "%1", PaneIndex: "0", CurrentCommand: "claude", State: worktree.AgentStateBusy},
		{PaneID: "%2", PaneIndex: "1", CurrentCommand: "zsh", State: worktree.AgentStateIdle},
	}
}

// TestRenderLeftWorktreeRowShowsChevronWhenQualifying verifies ADR-0008 §3: a
// worktree row with 2+ stateful panes shows "▼" while expanded (the default,
// m.collapsed unset) and "▶" once its pane-parent key is collapsed. The line
// must stay exactly `width` display columns wide in both states.
func TestRenderLeftWorktreeRowShowsChevronWhenQualifying(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Name:       "feature-a",
			Repo:       "repo-a",
			TmuxWindow: "wt-feature-a",
			Panes:      qualifyingPanes(),
		},
	}
	m := makeTestModel(statuses)

	findWorktreeLine := func(lines []string) string {
		for i, r := range m.rows {
			if r.kind == rowWorktree && r.status.Name == "feature-a" {
				return lines[i]
			}
		}
		return ""
	}

	out := ansi.Strip(m.renderLeft(40))
	line := findWorktreeLine(strings.Split(out, "\n"))
	if line == "" {
		t.Fatal("expected a feature-a worktree row")
	}
	if !strings.Contains(line, "▼") {
		t.Errorf("expected expanded qualifying worktree row to show ▼, got %q", line)
	}
	if strings.Contains(line, "▶") {
		t.Errorf("expected expanded qualifying worktree row not to also show ▶, got %q", line)
	}
	if w := ansi.StringWidth(line); w != 40 {
		t.Errorf("expected worktree row line width 40, got %d (%q)", w, line)
	}

	m.collapsed["worktree:wt-feature-a"] = true
	m.rebuildRows()
	out2 := ansi.Strip(m.renderLeft(40))
	line2 := findWorktreeLine(strings.Split(out2, "\n"))
	if line2 == "" {
		t.Fatal("expected a feature-a worktree row after collapsing")
	}
	if !strings.Contains(line2, "▶") {
		t.Errorf("expected collapsed qualifying worktree row to show ▶, got %q", line2)
	}
	if strings.Contains(line2, "▼") {
		t.Errorf("expected collapsed qualifying worktree row not to also show ▼, got %q", line2)
	}
	if w := ansi.StringWidth(line2); w != 40 {
		t.Errorf("expected collapsed worktree row line width 40, got %d (%q)", w, line2)
	}
}

// TestRenderLeftSessionRowShowsChevronWhenQualifying mirrors
// TestRenderLeftWorktreeRowShowsChevronWhenQualifying for rowSession.
func TestRenderLeftSessionRowShowsChevronWhenQualifying(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = []worktree.SessionStatus{
		{Name: "notes", Attached: true, Panes: qualifyingPanes()},
	}
	m.rebuildRows()

	findSessionLine := func(lines []string) string {
		for i, r := range m.rows {
			if r.kind == rowSession && r.session.Name == "notes" {
				return lines[i]
			}
		}
		return ""
	}

	out := ansi.Strip(m.renderLeft(40))
	line := findSessionLine(strings.Split(out, "\n"))
	if line == "" {
		t.Fatal("expected a notes session row")
	}
	if !strings.Contains(line, "▼") {
		t.Errorf("expected expanded qualifying session row to show ▼, got %q", line)
	}
	if strings.Contains(line, "▶") {
		t.Errorf("expected expanded qualifying session row not to also show ▶, got %q", line)
	}
	if w := ansi.StringWidth(line); w != 40 {
		t.Errorf("expected session row line width 40, got %d (%q)", w, line)
	}

	m.collapsed["session:notes"] = true
	m.rebuildRows()
	out2 := ansi.Strip(m.renderLeft(40))
	line2 := findSessionLine(strings.Split(out2, "\n"))
	if line2 == "" {
		t.Fatal("expected a notes session row after collapsing")
	}
	if !strings.Contains(line2, "▶") {
		t.Errorf("expected collapsed qualifying session row to show ▶, got %q", line2)
	}
	if strings.Contains(line2, "▼") {
		t.Errorf("expected collapsed qualifying session row not to also show ▼, got %q", line2)
	}
	if w := ansi.StringWidth(line2); w != 40 {
		t.Errorf("expected collapsed session row line width 40, got %d (%q)", w, line2)
	}
}

// TestRenderLeftNonQualifyingRowsShowBlankChevronColumn verifies that a
// worktree/session row below the pane-row threshold reserves the chevron's
// 2-column slot as blank space instead of omitting it, so non-qualifying
// rows still line up with qualifying ones in the same column, and every
// row still pads out to exactly `width` display columns.
func TestRenderLeftNonQualifyingRowsShowBlankChevronColumn(t *testing.T) {
	m := makeTestModel(testStatuses()) // no Panes set on any status - none qualify
	m.sessions = []worktree.SessionStatus{{Name: "misc", Attached: true}}
	m.rebuildRows()

	out := ansi.Strip(m.renderLeft(40))
	lines := strings.Split(out, "\n")
	if len(lines) != len(m.rows) {
		t.Fatalf("expected %d rendered lines, got %d", len(m.rows), len(lines))
	}

	checked := 0
	for i, r := range m.rows {
		if r.kind != rowWorktree && r.kind != rowSession {
			continue
		}
		checked++
		line := lines[i]
		if strings.ContainsAny(line, "▼▶") {
			t.Errorf(
				"row %d (kind=%d) does not qualify for pane rows, expected a blank chevron column, got %q",
				i,
				r.kind,
				line,
			)
		}
		if w := ansi.StringWidth(line); w != 40 {
			t.Errorf("row %d: expected line width 40, got %d (%q)", i, w, line)
		}
	}
	if checked == 0 {
		t.Fatal("expected at least one worktree/session row to check")
	}
}
