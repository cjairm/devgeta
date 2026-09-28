package tuiworktree

// Tests for agents.go (Step 4 of docs/plans/cycles/2026-09-28-ws-agents-section.md):
// building the dashboard's flat agents-section rows from the pane layer the
// fast tick already scans (ADR-0024), per ADR-0056's ordering and label rules.

import (
	"testing"

	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

func agentPane(id, index, kind, command, state string) tmux.PaneState {
	return tmux.PaneState{
		PaneID:         id,
		PaneIndex:      index,
		Kind:           kind,
		CurrentCommand: command,
		State:          state,
	}
}

// TestBuildAgentRows_Labels confirms ADR-0056's label rule: "repo/worktree"
// for a pane in a worktree window, otherwise the session name (whether the
// session is a repo-session or a standalone one).
func TestBuildAgentRows_Labels(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Repo: "devgeta", Name: "fix-stale-notify",
			Panes: []tmux.PaneState{agentPane("%1", "1", "claude", "claude", "idle")},
		},
	}
	repoSessions := []worktree.RepoSessionStatus{
		{
			Repo: "devgeta", Name: "devgeta-main",
			Panes: []tmux.PaneState{agentPane("%2", "1", "opencode", "opencode", "busy")},
		},
	}
	sessions := []worktree.SessionStatus{
		{
			Name:  "scratch",
			Panes: []tmux.PaneState{agentPane("%3", "1", "claude", "claude", "blocked")},
		},
	}

	rows := buildAgentRows(statuses, sessions, repoSessions, "")

	byPane := map[string]agentRow{}
	for _, r := range rows {
		byPane[r.pane.PaneID] = r
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 agent rows, got %d: %+v", len(rows), rows)
	}
	if got, want := byPane["%1"].label, "devgeta/fix-stale-notify"; got != want {
		t.Errorf("worktree pane label = %q, want %q", got, want)
	}
	if got, want := byPane["%2"].label, "devgeta-main"; got != want {
		t.Errorf("repo-session pane label = %q, want %q", got, want)
	}
	if got, want := byPane["%3"].label, "scratch"; got != want {
		t.Errorf("standalone session pane label = %q, want %q", got, want)
	}
}

// TestBuildAgentRows_SkipsNonAgentPanes confirms a pane that fails
// tmux.PaneState.IsAgent() (no kind, or kind set but the pane reverted to a
// plain shell) never produces a row.
func TestBuildAgentRows_SkipsNonAgentPanes(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Repo: "devgeta", Name: "editing",
			Panes: []tmux.PaneState{
				agentPane("%1", "1", "", "vim", ""),          // no kind at all
				agentPane("%2", "2", "claude", "zsh", ""),    // stale kind, coder has exited
				agentPane("%3", "3", "claude", "claude", ""), // the actual agent
			},
		},
	}

	rows := buildAgentRows(statuses, nil, nil, "")

	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 agent row, got %d: %+v", len(rows), rows)
	}
	if rows[0].pane.PaneID != "%3" {
		t.Errorf("expected the only row to be %%3, got %q", rows[0].pane.PaneID)
	}
}

// TestBuildAgentRows_TwoAgentsInOneWindow confirms a claude-nvim-style split
// window with two agent panes produces two rows, both sharing the same
// label, ordered by pane index.
func TestBuildAgentRows_TwoAgentsInOneWindow(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Repo: "devgeta", Name: "pairing",
			Panes: []tmux.PaneState{
				agentPane("%2", "2", "opencode", "opencode", "busy"),
				agentPane("%1", "1", "claude", "claude", "busy"),
			},
		},
	}

	rows := buildAgentRows(statuses, nil, nil, "")

	if len(rows) != 2 {
		t.Fatalf("expected 2 agent rows, got %d: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.label != "devgeta/pairing" {
			t.Errorf("row %+v has label %q, want %q", r, r.label, "devgeta/pairing")
		}
	}
	// Same state (working), same label -> tie-broken by pane index.
	if rows[0].pane.PaneID != "%1" || rows[1].pane.PaneID != "%2" {
		t.Errorf("expected pane index order [%%1, %%2], got [%s, %s]",
			rows[0].pane.PaneID, rows[1].pane.PaneID)
	}
}

// TestBuildAgentRows_Ordering confirms ADR-0056's sort: urgency first
// (blocked > error > done > working > idle), then label, then pane index —
// including ties at each level.
func TestBuildAgentRows_Ordering(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Repo: "zeta", Name: "wt",
			Panes: []tmux.PaneState{agentPane("%working", "1", "claude", "claude", "busy")},
		},
		{
			Repo: "alpha", Name: "wt",
			Panes: []tmux.PaneState{agentPane("%idle", "1", "claude", "claude", "")},
		},
		{
			Repo: "beta", Name: "wt-a",
			Panes: []tmux.PaneState{agentPane("%blocked-b", "1", "claude", "claude", "blocked")},
		},
		{
			Repo: "beta",
			Name: "wt-b",
			// Same label-sort-rank source repo, different name, same state
			// (blocked) as the row above - a tie broken by label.
			Panes: []tmux.PaneState{
				agentPane("%blocked-a-idx2", "2", "claude", "claude", "blocked"),
			},
		},
		{
			Repo: "gamma", Name: "wt",
			Panes: []tmux.PaneState{agentPane("%error", "1", "claude", "claude", "error")},
		},
		{
			Repo: "delta", Name: "wt",
			Panes: []tmux.PaneState{agentPane("%done", "1", "claude", "claude", "idle")},
		},
	}

	rows := buildAgentRows(statuses, nil, nil, "")

	var gotOrder []string
	for _, r := range rows {
		gotOrder = append(gotOrder, r.pane.PaneID)
	}
	// beta/wt-a ("%blocked-b") and beta/wt-b ("%blocked-a-idx2") both hold
	// "blocked" - the tie is broken by label ("beta/wt-a" < "beta/wt-b"),
	// so %blocked-b (wt-a) sorts first among the pair.
	wantOrder := []string{
		"%blocked-b",
		"%blocked-a-idx2",
		"%error",
		"%done",
		"%working",
		"%idle",
	}
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("expected %d rows, got %d: %+v", len(wantOrder), len(gotOrder), gotOrder)
	}
	for i, want := range wantOrder {
		if gotOrder[i] != want {
			t.Errorf("row[%d] = %q, want %q (full order: %v)", i, gotOrder[i], want, gotOrder)
			break
		}
	}
}

// TestBuildAgentRows_WorktreeBackReference confirms a worktree-sourced agent
// row carries the WorktreeStatus it came from (isWorktree=true), for the
// right pane's diff (ADR-0056: "the right pane shows the agent's worktree
// diff"), while a repo-session or standalone-session pane carries none
// (isWorktree=false) — those have no diff to show.
func TestBuildAgentRows_WorktreeBackReference(t *testing.T) {
	wtStatus := worktree.WorktreeStatus{
		Repo: "devgeta", Name: "fix-stale-notify", Path: "/repos/devgeta/fix-stale-notify",
		Panes: []tmux.PaneState{agentPane("%1", "1", "claude", "claude", "idle")},
	}
	statuses := []worktree.WorktreeStatus{wtStatus}
	repoSessions := []worktree.RepoSessionStatus{
		{
			Repo: "devgeta", Name: "devgeta-main",
			Panes: []tmux.PaneState{agentPane("%2", "1", "opencode", "opencode", "busy")},
		},
	}
	sessions := []worktree.SessionStatus{
		{
			Name:  "scratch",
			Panes: []tmux.PaneState{agentPane("%3", "1", "claude", "claude", "blocked")},
		},
	}

	rows := buildAgentRows(statuses, sessions, repoSessions, "")

	byPane := map[string]agentRow{}
	for _, r := range rows {
		byPane[r.pane.PaneID] = r
	}
	if got := byPane["%1"]; !got.isWorktree || got.worktree.Path != wtStatus.Path {
		t.Errorf("worktree pane: isWorktree=%v worktree=%+v, want isWorktree=true, worktree=%+v",
			got.isWorktree, got.worktree, wtStatus)
	}
	if got := byPane["%2"]; got.isWorktree {
		t.Errorf("repo-session pane: isWorktree=true, want false (got worktree=%+v)", got.worktree)
	}
	if got := byPane["%3"]; got.isWorktree {
		t.Errorf(
			"standalone session pane: isWorktree=true, want false (got worktree=%+v)",
			got.worktree,
		)
	}
}

// TestBuildAgentRows_FilterMatchesLocationCoderAndStateWord confirms Step 10
// of docs/plans/cycles/2026-09-28-ws-agents-section.md: the agents filter
// matches the location label, the coder's own name, and the display state
// word - so /blocked and /opencode both work, per ADR-0056.
func TestBuildAgentRows_FilterMatchesLocationCoderAndStateWord(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "fix-stale-notify"},
			agentPane("%1", "1", "claude", "claude", "blocked"),
		),
		withAgentPane(
			worktree.WorktreeStatus{Repo: "devgeta", Name: "feat-agents-section"},
			agentPane("%2", "1", "opencode", "opencode", "idle"),
		),
		withAgentPane(
			worktree.WorktreeStatus{Repo: "dotfiles", Name: "main"},
			agentPane("%3", "1", "claude", "claude", "busy"),
		),
	}

	for _, tc := range []struct {
		name       string
		filter     string
		wantPaneID string
	}{
		{"matches location", "stale-notify", "%1"},
		{"matches coder name", "opencode", "%2"},
		{"matches state word (blocked)", "blocked", "%1"},
		{"matches state word (done, idle raw state)", "done", "%2"},
		{"matches state word (working, busy raw state)", "working", "%3"},
		{"case-insensitive", "STALE", "%1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := buildAgentRows(statuses, nil, nil, tc.filter)
			if len(rows) != 1 || rows[0].pane.PaneID != tc.wantPaneID {
				t.Fatalf("filter %q: expected only %s, got %+v", tc.filter, tc.wantPaneID, rows)
			}
		})
	}
}

// TestBuildAgentRows_EmptyFilterMatchesEverything confirms "" (no active
// filter) is a no-op, same as buildRows' own empty-filter convention.
func TestBuildAgentRows_EmptyFilterMatchesEverything(t *testing.T) {
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
	rows := buildAgentRows(statuses, nil, nil, "")
	if len(rows) != 2 {
		t.Errorf("expected both rows with no filter, got %d", len(rows))
	}
}

// TestBuildAgentRows_StateAndKindPopulated confirms each row carries the
// display state (via tuicomponents.AgentRowStateFor) and the coder's own
// kind string, not just identity/label fields.
func TestBuildAgentRows_StateAndKindPopulated(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{
			Repo: "devgeta", Name: "wt",
			Panes: []tmux.PaneState{agentPane("%1", "1", "opencode", "opencode", "blocked")},
		},
	}

	rows := buildAgentRows(statuses, nil, nil, "")

	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].state != tuicomponents.AgentRowBlocked {
		t.Errorf("state = %v, want AgentRowBlocked", rows[0].state)
	}
	if rows[0].kind != "opencode" {
		t.Errorf("kind = %q, want %q", rows[0].kind, "opencode")
	}
}
