package tuiworktree

// Tests for ADR-0052's repo-session row: buildRows emitting it, and the
// rowKey/paneParentKey plumbing that lets it behave exactly like a
// standalone session row wherever that matters (fold key, pane-row
// expansion, cursor restore).

import (
	"testing"

	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func TestRowKeyRepoSessionUsesSameSessPrefixAsStandalone(t *testing.T) {
	repoSess := row{
		kind:        rowRepoSession,
		repo:        "repo-a",
		repoSession: worktree.RepoSessionStatus{Name: "hire2-tien"},
	}
	standalone := row{kind: rowSession, session: worktree.SessionStatus{Name: "hire2-tien"}}

	if got := rowKey(repoSess); got != "sess:hire2-tien" {
		t.Errorf("expected sess:hire2-tien, got %q", got)
	}
	if rowKey(repoSess) != rowKey(standalone) {
		t.Errorf(
			"expected a repo-session row and a standalone one with the same name to share a key (ADR-0052: session names are unique on the server), got %q vs %q",
			rowKey(repoSess),
			rowKey(standalone),
		)
	}
}

func TestBuildRowsEmitsRepoSessionRowBeforeWorktreeRows(t *testing.T) {
	statuses := testStatuses() // repo-a: feature-a, feature-b; repo-b: feature-x
	repoSessions := []worktree.RepoSessionStatus{
		{Repo: "repo-a", Name: "repo-a-tien", Attached: true},
	}

	rows := buildRows(statuses, nil, repoSessions, map[string]bool{}, "")

	if rows[0].kind != rowRepo || rows[0].repo != "repo-a" {
		t.Fatalf("expected row 0 to be repo-a's header, got %+v", rows[0])
	}
	if rows[1].kind != rowRepoSession || rows[1].repoSession.Name != "repo-a-tien" {
		t.Fatalf("expected row 1 to be repo-a's session row, got %+v", rows[1])
	}
	if rows[2].kind != rowWorktree || rows[2].status.Name != "feature-a" {
		t.Fatalf("expected row 2 to be feature-a's worktree row, got %+v", rows[2])
	}
}

func TestBuildRowsRepoSessionRowHiddenWhenRepoCollapsed(t *testing.T) {
	statuses := testStatuses()
	repoSessions := []worktree.RepoSessionStatus{{Repo: "repo-a", Name: "repo-a-tien"}}
	collapsed := map[string]bool{repoKey("repo-a"): true}

	rows := buildRows(statuses, nil, repoSessions, collapsed, "")

	for _, r := range rows {
		if r.kind == rowRepoSession {
			t.Errorf("expected repo-a's session row to be hidden while collapsed, found %+v", r)
		}
	}
}

func TestBuildRowsRepoSessionRowAbsentWhenNoSessionHoldsTheRepo(t *testing.T) {
	rows := buildRows(testStatuses(), nil, nil, map[string]bool{}, "")

	for _, r := range rows {
		if r.kind == rowRepoSession {
			t.Errorf("expected no repo-session rows with an empty repoSessions list, found %+v", r)
		}
	}
}

func repoSessionRowTestData() []worktree.RepoSessionStatus {
	return []worktree.RepoSessionStatus{
		{
			Repo: "repo-a",
			Name: "repo-a-tien",
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

func TestBuildRowsRepoSessionRowGetsPaneChildrenWhenQualifying(t *testing.T) {
	rows := buildRows(testStatuses(), nil, repoSessionRowTestData(), map[string]bool{}, "")

	if rows[1].kind != rowRepoSession {
		t.Fatalf("expected row 1 to be the repo-session row, got %+v", rows[1])
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

func TestBuildRowsRepoSessionPaneRowsCollapsedByKey(t *testing.T) {
	repoSessions := repoSessionRowTestData()
	collapsed := map[string]bool{"sess:repo-a-tien": true}

	rows := buildRows(testStatuses(), nil, repoSessions, collapsed, "")

	for _, r := range rows {
		if r.kind == rowPane {
			t.Errorf("expected pane rows collapsed by their sess: key, found %+v", r)
		}
	}
}

func TestPaneParentKeyRepoSessionQualifying(t *testing.T) {
	r := row{kind: rowRepoSession, repoSession: worktree.RepoSessionStatus{
		Name:  "repo-a-tien",
		Panes: repoSessionRowTestData()[0].Panes,
	}}
	key, qualifies := paneParentKey(r)
	if !qualifies {
		t.Error("expected a repo-session row with 2 stateful panes to qualify")
	}
	if key != "sess:repo-a-tien" {
		t.Errorf("expected key sess:repo-a-tien, got %q", key)
	}
}

func TestPaneParentKeyRepoSessionNotQualifying(t *testing.T) {
	r := row{kind: rowRepoSession, repoSession: worktree.RepoSessionStatus{Name: "repo-a-tien"}}
	if _, qualifies := paneParentKey(r); qualifies {
		t.Error("expected a repo-session row with no stateful panes not to qualify")
	}
}
