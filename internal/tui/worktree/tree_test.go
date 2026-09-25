package tuiworktree

import (
	"testing"

	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// --- rowKey (B5: identity, not the collidable tmux window name) ---

func TestRowKeyWorktreeDistinguishesCollidingWindowNames(t *testing.T) {
	// "taskqueue" + "groups-x" and "taskqueue-groups" + "x" both derive the
	// same TmuxWindow ("wt-taskqueue-groups-x") but are different worktrees
	// with different paths.
	a := row{
		kind: rowWorktree,
		status: worktree.WorktreeStatus{
			TmuxWindow: "wt-taskqueue-groups-x",
			Path:       "/repos/taskqueue/groups-x",
		},
	}
	b := row{
		kind: rowWorktree,
		status: worktree.WorktreeStatus{
			TmuxWindow: "wt-taskqueue-groups-x",
			Path:       "/repos/taskqueue-groups/x",
		},
	}
	if rowKey(a) == rowKey(b) {
		t.Errorf(
			"expected different worktrees with colliding TmuxWindow names to get different keys, both got %q",
			rowKey(a),
		)
	}
}

func TestRowKeyWorktreeUsesPath(t *testing.T) {
	r := row{kind: rowWorktree, status: worktree.WorktreeStatus{Path: "/repos/api/feature-a"}}
	if got, want := rowKey(r), "wt:/repos/api/feature-a"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestRowKeyRepoUsesRepoName(t *testing.T) {
	r := row{kind: rowRepo, repo: "api"}
	if got, want := rowKey(r), "repo:api"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestRowKeySessionUsesName(t *testing.T) {
	r := row{kind: rowSession, session: worktree.SessionStatus{Name: "notes"}}
	if got, want := rowKey(r), "sess:notes"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestRowKeyPaneUsesPaneID(t *testing.T) {
	r := row{kind: rowPane, pane: tmux.PaneState{PaneID: "%3"}}
	if got, want := rowKey(r), "pane:%3"; got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

// --- paneParentKey ---

func TestPaneParentKeyDistinguishesCollidingWindowNames(t *testing.T) {
	a := row{
		kind: rowWorktree,
		status: worktree.WorktreeStatus{
			TmuxWindow: "wt-taskqueue-groups-x",
			Path:       "/repos/taskqueue/groups-x",
			Panes: []tmux.PaneState{
				{PaneID: "%1", State: worktree.AgentStateBusy},
				{PaneID: "%2", State: worktree.AgentStateIdle},
			},
		},
	}
	b := row{
		kind: rowWorktree,
		status: worktree.WorktreeStatus{
			TmuxWindow: "wt-taskqueue-groups-x",
			Path:       "/repos/taskqueue-groups/x",
			Panes: []tmux.PaneState{
				{PaneID: "%3", State: worktree.AgentStateBusy},
				{PaneID: "%4", State: worktree.AgentStateIdle},
			},
		},
	}
	keyA, _ := paneParentKey(a)
	keyB, _ := paneParentKey(b)
	if keyA == keyB {
		t.Errorf(
			"expected colliding-window worktrees to get different pane-parent keys, both got %q",
			keyA,
		)
	}
}

func TestPaneParentKeyWorktreeQualifying(t *testing.T) {
	r := row{
		kind: rowWorktree,
		status: worktree.WorktreeStatus{
			TmuxWindow: "wt-feature-a",
			Path:       "/repos/feature-a",
			Panes: []tmux.PaneState{
				{PaneID: "%1", State: worktree.AgentStateBusy},
				{PaneID: "%2", State: worktree.AgentStateIdle},
			},
		},
	}
	key, qualifies := paneParentKey(r)
	if key != "wt:/repos/feature-a" {
		t.Errorf("expected key %q, got %q", "wt:/repos/feature-a", key)
	}
	if !qualifies {
		t.Error("expected a worktree row with 2 stateful panes to qualify")
	}
}

func TestPaneParentKeyWorktreeNotQualifying(t *testing.T) {
	r := row{
		kind: rowWorktree,
		status: worktree.WorktreeStatus{
			TmuxWindow: "wt-feature-a",
			Path:       "/repos/feature-a",
			Panes:      []tmux.PaneState{{PaneID: "%1", State: worktree.AgentStateBusy}},
		},
	}
	key, qualifies := paneParentKey(r)
	if key != "wt:/repos/feature-a" {
		t.Errorf("expected key to still be computed even when not qualifying, got %q", key)
	}
	if qualifies {
		t.Error("expected a worktree row with only 1 stateful pane not to qualify")
	}
}

func TestPaneParentKeySessionQualifying(t *testing.T) {
	r := row{
		kind: rowSession,
		session: worktree.SessionStatus{
			Name: "notes",
			Panes: []tmux.PaneState{
				{PaneID: "%1", State: worktree.AgentStateBlocked},
				{PaneID: "%2", State: worktree.AgentStateError},
			},
		},
	}
	key, qualifies := paneParentKey(r)
	if key != "sess:notes" {
		t.Errorf("expected key %q, got %q", "sess:notes", key)
	}
	if !qualifies {
		t.Error("expected a session row with 2 stateful panes to qualify")
	}
}

func TestPaneParentKeySessionNotQualifying(t *testing.T) {
	r := row{kind: rowSession, session: worktree.SessionStatus{Name: "notes"}}
	key, qualifies := paneParentKey(r)
	if key != "sess:notes" {
		t.Errorf("expected key %q, got %q", "sess:notes", key)
	}
	if qualifies {
		t.Error("expected a session row with no stateful panes not to qualify")
	}
}

func TestPaneParentKeyOtherKinds(t *testing.T) {
	for _, r := range []row{
		{kind: rowRepo, repo: "repo-a"},
		{kind: rowPane, pane: tmux.PaneState{PaneID: "%1"}},
	} {
		key, qualifies := paneParentKey(r)
		if key != "" || qualifies {
			t.Errorf(
				"expected kind=%d to return (\"\", false), got (%q, %v)",
				r.kind,
				key,
				qualifies,
			)
		}
	}
}

// --- enclosingPaneParent ---

func TestEnclosingPaneParentAtIndexZero(t *testing.T) {
	rows := []row{{kind: rowPane, pane: tmux.PaneState{PaneID: "%1"}}}
	_, ok := enclosingPaneParent(rows, 0)
	if ok {
		t.Error("expected no enclosing parent for a pane row at index 0")
	}
}

func TestEnclosingPaneParentImmediatelyPrecedingWorktree(t *testing.T) {
	rows := []row{
		{kind: rowWorktree, status: worktree.WorktreeStatus{TmuxWindow: "wt-a"}},
		{kind: rowPane, pane: tmux.PaneState{PaneID: "%1"}},
	}
	parent, ok := enclosingPaneParent(rows, 1)
	if !ok {
		t.Fatal("expected an enclosing parent")
	}
	if parent.kind != rowWorktree || parent.status.TmuxWindow != "wt-a" {
		t.Errorf("expected the worktree row, got %+v", parent)
	}
}

func TestEnclosingPaneParentSkipsOverPrecedingPanes(t *testing.T) {
	rows := []row{
		{kind: rowSession, session: worktree.SessionStatus{Name: "notes"}},
		{kind: rowPane, pane: tmux.PaneState{PaneID: "%1"}},
		{kind: rowPane, pane: tmux.PaneState{PaneID: "%2"}},
	}
	parent, ok := enclosingPaneParent(rows, 2)
	if !ok {
		t.Fatal("expected an enclosing parent")
	}
	if parent.kind != rowSession || parent.session.Name != "notes" {
		t.Errorf("expected the session row, got %+v", parent)
	}
}

func TestEnclosingPaneParentStopsAtUnrelatedRowKind(t *testing.T) {
	rows := []row{
		{kind: rowRepo, repo: "repo-a"},
		{kind: rowPane, pane: tmux.PaneState{PaneID: "%1"}},
	}
	_, ok := enclosingPaneParent(rows, 1)
	if ok {
		t.Error("expected no enclosing parent when preceded by an unrelated row kind (rowRepo)")
	}
}

// --- sameParentRow ---

func TestSameParentRowWorktreeMatch(t *testing.T) {
	a := row{kind: rowWorktree, status: worktree.WorktreeStatus{Path: "/repos/a"}}
	b := row{kind: rowWorktree, status: worktree.WorktreeStatus{Path: "/repos/a"}}
	if !sameParentRow(a, b) {
		t.Error("expected two worktree rows with the same path to match")
	}
}

func TestSameParentRowWorktreeMismatch(t *testing.T) {
	a := row{kind: rowWorktree, status: worktree.WorktreeStatus{Path: "/repos/a"}}
	b := row{kind: rowWorktree, status: worktree.WorktreeStatus{Path: "/repos/b"}}
	if sameParentRow(a, b) {
		t.Error("expected worktree rows with different paths not to match")
	}
}

// TestSameParentRowWorktreeCollidingWindowName is B5's exact collision: two
// worktrees whose derived TmuxWindow strings are identical but whose paths
// (their real identity) are not.
func TestSameParentRowWorktreeCollidingWindowName(t *testing.T) {
	a := row{
		kind: rowWorktree,
		status: worktree.WorktreeStatus{
			TmuxWindow: "wt-taskqueue-groups-x",
			Path:       "/repos/taskqueue/groups-x",
		},
	}
	b := row{
		kind: rowWorktree,
		status: worktree.WorktreeStatus{
			TmuxWindow: "wt-taskqueue-groups-x",
			Path:       "/repos/taskqueue-groups/x",
		},
	}
	if sameParentRow(a, b) {
		t.Error("expected worktrees with colliding TmuxWindow but different paths not to match")
	}
}

func TestSameParentRowSessionMatch(t *testing.T) {
	a := row{kind: rowSession, session: worktree.SessionStatus{Name: "notes"}}
	b := row{kind: rowSession, session: worktree.SessionStatus{Name: "notes"}}
	if !sameParentRow(a, b) {
		t.Error("expected two session rows with the same Name to match")
	}
}

func TestSameParentRowDifferentKinds(t *testing.T) {
	a := row{kind: rowWorktree, status: worktree.WorktreeStatus{TmuxWindow: "wt-a"}}
	b := row{kind: rowSession, session: worktree.SessionStatus{Name: "wt-a"}}
	if sameParentRow(a, b) {
		t.Error(
			"expected rows of different kinds never to match, even with overlapping identity strings",
		)
	}
}

func TestSameParentRowUnsupportedKind(t *testing.T) {
	a := row{kind: rowPane, pane: tmux.PaneState{PaneID: "%1"}}
	b := row{kind: rowPane, pane: tmux.PaneState{PaneID: "%1"}}
	if sameParentRow(a, b) {
		t.Error("expected rowPane (not a pane-parent kind) never to match via sameParentRow")
	}
}
