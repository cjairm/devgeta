package tuiworktree

// Tests for ADR-0052's window row (amended): buildRows emitting one per plain
// window, and the rowKey/paneParentKey plumbing that lets it behave like a
// session row wherever that matters (fold key, pane-row expansion, cursor
// restore).

import (
	"testing"

	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func TestRowKeyWindowUsesTheWindowID(t *testing.T) {
	a := row{kind: rowWindow, window: worktree.RepoWindowStatus{Window: "zsh", WindowID: "@4"}}
	b := row{kind: rowWindow, window: worktree.RepoWindowStatus{Window: "zsh", WindowID: "@7"}}

	if got := rowKey(a); got != "win:@4" {
		t.Errorf("expected win:@4, got %q", got)
	}
	if rowKey(a) == rowKey(b) {
		t.Errorf("two windows named zsh must not share a key, both got %q", rowKey(a))
	}
}

func TestRowKeyWindowWithoutAnIDFallsBackToSessionAndName(t *testing.T) {
	r := row{
		kind:   rowWindow,
		window: worktree.RepoWindowStatus{Session: "hire2", Window: "node"},
	}
	if got := rowKey(r); got != "win:hire2:node" {
		t.Errorf("expected win:hire2:node, got %q", got)
	}
}

func TestBuildRowsEmitsWindowRowsBeforeWorktreeRows(t *testing.T) {
	statuses := testStatuses() // repo-a: feature-a, feature-b; repo-b: feature-x
	windows := []worktree.RepoWindowStatus{
		{Repo: "repo-a", Session: "repo-a", Window: "node", WindowID: "@1"},
		{Repo: "repo-a", Session: "repo-a", Window: "zsh", WindowID: "@2"},
	}

	rows := buildRows(statuses, nil, windows, map[string]bool{}, "")

	if rows[0].kind != rowRepo || rows[0].repo != "repo-a" {
		t.Fatalf("expected row 0 to be repo-a's header, got %+v", rows[0])
	}
	if rows[1].kind != rowWindow || rows[1].window.Window != "node" {
		t.Fatalf("expected row 1 to be the node window, got %+v", rows[1])
	}
	if rows[2].kind != rowWindow || rows[2].window.Window != "zsh" {
		t.Fatalf("expected row 2 to be the zsh window, got %+v", rows[2])
	}
	if rows[3].kind != rowWorktree || rows[3].status.Name != "feature-a" {
		t.Fatalf("expected row 3 to be feature-a's worktree row, got %+v", rows[3])
	}
}

func TestBuildRowsWindowRowsHiddenWhenRepoCollapsed(t *testing.T) {
	windows := []worktree.RepoWindowStatus{{Repo: "repo-a", Window: "zsh", WindowID: "@2"}}
	collapsed := map[string]bool{repoKey("repo-a"): true}

	for _, r := range buildRows(testStatuses(), nil, windows, collapsed, "") {
		if r.kind == rowWindow {
			t.Errorf("expected repo-a's window rows hidden while collapsed, found %+v", r)
		}
	}
}

func TestBuildRowsNoWindowRowsWhenThereAreNoWindows(t *testing.T) {
	for _, r := range buildRows(testStatuses(), nil, nil, map[string]bool{}, "") {
		if r.kind == rowWindow {
			t.Errorf("expected no window rows from an empty list, found %+v", r)
		}
	}
}

func TestBuildRowsWindowRowsCanShareANameInOneRepo(t *testing.T) {
	windows := []worktree.RepoWindowStatus{
		{Repo: "repo-a", Window: "zsh", WindowID: "@1"},
		{Repo: "repo-a", Window: "zsh", WindowID: "@2"},
	}

	keys := map[string]bool{}
	for _, r := range buildRows(testStatuses(), nil, windows, map[string]bool{}, "") {
		if r.kind == rowWindow {
			keys[rowKey(r)] = true
		}
	}
	if len(keys) != 2 {
		t.Errorf("expected two distinct window keys, got %v", keys)
	}
}

func windowRowTestData() []worktree.RepoWindowStatus {
	return []worktree.RepoWindowStatus{
		{
			Repo:     "repo-a",
			Session:  "repo-a",
			Window:   "node",
			WindowID: "@1",
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
					State:          worktree.AgentStateIdle,
				},
			},
		},
	}
}

func TestBuildRowsWindowRowGetsPaneChildrenWhenQualifying(t *testing.T) {
	rows := buildRows(testStatuses(), nil, windowRowTestData(), map[string]bool{}, "")

	if rows[1].kind != rowWindow {
		t.Fatalf("expected row 1 to be the window row, got %+v", rows[1])
	}
	if rows[2].kind != rowPane || rows[2].pane.PaneID != "%1" {
		t.Fatalf("expected row 2 to be the first pane row, got %+v", rows[2])
	}
	if rows[3].kind != rowPane || rows[3].pane.PaneID != "%2" {
		t.Fatalf("expected row 3 to be the second pane row, got %+v", rows[3])
	}
	if rows[4].kind != rowWorktree {
		t.Fatalf("expected row 4 to be feature-a's worktree row, got %+v", rows[4])
	}
}

func TestBuildRowsWindowPaneRowsCollapsedByKey(t *testing.T) {
	collapsed := map[string]bool{"win:@1": true}

	for _, r := range buildRows(testStatuses(), nil, windowRowTestData(), collapsed, "") {
		if r.kind == rowPane {
			t.Errorf("expected pane rows collapsed by their win: key, found %+v", r)
		}
	}
}

func TestPaneParentKeyWindowQualifying(t *testing.T) {
	r := row{kind: rowWindow, window: windowRowTestData()[0]}
	key, qualifies := paneParentKey(r)
	if !qualifies {
		t.Error("expected a window row with 2 stateful panes to qualify")
	}
	if key != "win:@1" {
		t.Errorf("expected key win:@1, got %q", key)
	}
}

func TestPaneParentKeyWindowNotQualifying(t *testing.T) {
	r := row{kind: rowWindow, window: worktree.RepoWindowStatus{WindowID: "@1"}}
	if _, qualifies := paneParentKey(r); qualifies {
		t.Error("expected a window row with no stateful panes not to qualify")
	}
}

// n from a window row should offer that window's repo first, the way it does
// from a worktree row - and from a pane row under it.
func TestRepoHintForWindowAndItsPaneRows(t *testing.T) {
	rows := buildRows(testStatuses(), nil, windowRowTestData(), map[string]bool{}, "")

	if rows[1].kind != rowWindow || rows[2].kind != rowPane {
		t.Fatalf("test setup: expected a window row then its pane, got %+v / %+v", rows[1], rows[2])
	}
	if got := repoHintForRow(rows, 1); got != "repo-a" {
		t.Errorf("window row hint = %q, want repo-a", got)
	}
	if got := repoHintForRow(rows, 2); got != "repo-a" {
		t.Errorf("pane-under-window hint = %q, want repo-a", got)
	}
}
