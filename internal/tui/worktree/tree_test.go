package tuiworktree

import (
	"testing"

	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// --- paneParentKey ---

func TestPaneParentKeyWorktreeQualifying(t *testing.T) {
	r := row{
		kind: rowWorktree,
		status: worktree.WorktreeStatus{
			TmuxWindow: "wt-feature-a",
			Panes: []tmux.PaneState{
				{PaneID: "%1", State: worktree.AgentStateBusy},
				{PaneID: "%2", State: worktree.AgentStateIdle},
			},
		},
	}
	key, qualifies := paneParentKey(r)
	if key != "worktree:wt-feature-a" {
		t.Errorf("expected key %q, got %q", "worktree:wt-feature-a", key)
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
			Panes:      []tmux.PaneState{{PaneID: "%1", State: worktree.AgentStateBusy}},
		},
	}
	key, qualifies := paneParentKey(r)
	if key != "worktree:wt-feature-a" {
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
	if key != "session:notes" {
		t.Errorf("expected key %q, got %q", "session:notes", key)
	}
	if !qualifies {
		t.Error("expected a session row with 2 stateful panes to qualify")
	}
}

func TestPaneParentKeySessionNotQualifying(t *testing.T) {
	r := row{kind: rowSession, session: worktree.SessionStatus{Name: "notes"}}
	key, qualifies := paneParentKey(r)
	if key != "session:notes" {
		t.Errorf("expected key %q, got %q", "session:notes", key)
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
	a := row{kind: rowWorktree, status: worktree.WorktreeStatus{TmuxWindow: "wt-a"}}
	b := row{kind: rowWorktree, status: worktree.WorktreeStatus{TmuxWindow: "wt-a"}}
	if !sameParentRow(a, b) {
		t.Error("expected two worktree rows with the same TmuxWindow to match")
	}
}

func TestSameParentRowWorktreeMismatch(t *testing.T) {
	a := row{kind: rowWorktree, status: worktree.WorktreeStatus{TmuxWindow: "wt-a"}}
	b := row{kind: rowWorktree, status: worktree.WorktreeStatus{TmuxWindow: "wt-b"}}
	if sameParentRow(a, b) {
		t.Error("expected worktree rows with different TmuxWindow not to match")
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
