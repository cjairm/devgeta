package tuiworktree

// Regression tests for B1/B2: rebuildRows used to clamp the OLD numeric
// cursor position against the new row list, so any rebuild that inserted
// rows above the cursor (a fold, a filter keystroke, a new pane row) slid the
// cursor onto a different row entirely - and enter, d, or the diff pane then
// acted on whatever landed there instead of what the user was actually
// looking at. Each case below rebuilds a row list that shifts the selected
// row's index without removing it, and asserts the cursor followed that
// row's identity rather than its old position.

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func TestRebuildRowsPreservesCursorWhenPaneRowsAppearAbove(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a", Path: "/tmp/a", TmuxWindow: "wt-feature-a"},
		{Name: "feature-b", Repo: "repo-a", Path: "/tmp/b", TmuxWindow: "wt-feature-b"},
	}
	m := makeTestModel(statuses)
	if _, ok := m.focusRow(func(r row) bool {
		return r.kind == rowWorktree && r.status.Path == "/tmp/b"
	}); !ok {
		t.Fatal("test setup: expected to find feature-b's worktree row")
	}
	before := m.selectedPath()
	if before != "/tmp/b" {
		t.Fatalf("test setup: expected cursor on feature-b, got %q", before)
	}

	// feature-a, which sorts before feature-b in the same repo, gains 2
	// stateful panes - two pane rows now appear ABOVE feature-b, shifting its
	// index down by 2.
	m.statuses[0].Panes = paneRowTestStatuses()[0].Panes
	m.rebuildRows()

	if got := m.selectedPath(); got != before {
		t.Errorf(
			"expected the cursor to stay on feature-b (%q) after pane rows appeared above it, got %q",
			before,
			got,
		)
	}
}

func TestRebuildRowsPreservesCursorWhenNewSessionSortsBefore(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = []worktree.SessionStatus{{Name: "zzz-session"}}
	m.rebuildRows()
	if _, ok := m.focusRow(func(r row) bool {
		return r.kind == rowSession && r.session.Name == "zzz-session"
	}); !ok {
		t.Fatal("test setup: expected to find zzz-session's row")
	}
	// A new session sorting before it alphabetically appears - zzz-session's
	// index shifts from 0 to 1, but its identity is unchanged.
	m.sessions = []worktree.SessionStatus{{Name: "aaa-session"}, {Name: "zzz-session"}}
	m.rebuildRows()

	sel, ok := m.selectedSession()
	if !ok || sel.Name != "zzz-session" {
		t.Errorf(
			"expected the cursor to stay on zzz-session's row after aaa-session sorted before it, got %+v (ok=%v)",
			sel,
			ok,
		)
	}
}

func TestRebuildRowsPreservesCursorWhileTypingFilterKeepsMatchingRow(t *testing.T) {
	// "aaa" is filtered out, "target" and "zzzt" both survive - so a naive
	// positional clamp lands on zzzt (the new last row) rather than target,
	// the row the cursor actually started on.
	statuses := []worktree.WorktreeStatus{
		{Name: "aaa", Repo: "repo-a", Path: "/tmp/aaa", TmuxWindow: "wt-aaa"},
		{Name: "target", Repo: "repo-a", Path: "/tmp/target", TmuxWindow: "wt-target"},
		{Name: "zzzt", Repo: "repo-a", Path: "/tmp/zzzt", TmuxWindow: "wt-zzzt"},
	}
	m := makeTestModel(statuses)
	if _, ok := m.focusRow(func(r row) bool {
		return r.kind == rowWorktree && r.status.Path == "/tmp/target"
	}); !ok {
		t.Fatal("test setup: expected to find target's worktree row")
	}

	// Typing "t" keeps target and zzzt but drops aaa, which sat above target.
	m = filterFor(t, m, "t")

	if got := m.selectedPath(); got != "/tmp/target" {
		t.Errorf(
			"expected the cursor to stay on target after filtering removed the row above it, got %q",
			got,
		)
	}
}

// TestZExpandingKeepsCursorInSameRepoNotAnotherOne is B2's own repro: collapse
// every repo with z, land on repo-b's header, then press z again to expand
// everything. Before the fix, the cursor's plain index (unchanged by
// collapsing two same-size groups) slid onto repo-a's first worktree once
// expansion revealed its children - a different repo than the one the user
// was looking at.
func TestZExpandingKeepsCursorInSameRepoNotAnotherOne(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a", Path: "/tmp/a", TmuxWindow: "wt-feature-a"},
		{Name: "feature-b", Repo: "repo-a", Path: "/tmp/b", TmuxWindow: "wt-feature-b"},
		{Name: "feature-x", Repo: "repo-b", Path: "/tmp/x", TmuxWindow: "wt-feature-x"},
	}
	m := makeTestModel(statuses)
	m.mgr = newTestWorktreeManager()

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'z'}) // collapse all
	m = updated.(Model)
	if _, ok := m.focusRow(
		func(r row) bool { return r.kind == rowRepo && r.repo == "repo-b" },
	); !ok {
		t.Fatal("test setup: expected repo-b's collapsed header row")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'z'}) // expand all
	m = updated.(Model)

	if got := m.selectedPath(); got != "/tmp/x" {
		t.Errorf(
			"expected z to expand and keep the cursor within repo-b (its own child /tmp/x), got %q",
			got,
		)
	}
}
