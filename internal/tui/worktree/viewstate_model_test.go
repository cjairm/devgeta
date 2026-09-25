package tuiworktree

// Model-level wiring tests for the saved view state (ADR-0050): loading it
// once at startup, writing it on the triggers the ADR names (a fold change,
// e, a drag ending) and nowhere else, pruning stale keys before every write,
// and never letting a failed write block the fold or resize that triggered
// it.

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func TestViewStateMsgAppliesCollapsedAndWidth(t *testing.T) {
	m := makeTestModel(testStatuses()) // repo-a: feature-a, feature-b; repo-b: feature-x

	updated, _ := m.Update(viewStateMsg{
		state: viewStateV1{V: viewStateVersion, Collapsed: []string{"repo:repo-a"}, Left: 55},
		ok:    true,
	})
	m = updated.(Model)

	if m.leftPaneWidth != 55 {
		t.Errorf("expected the loaded width 55 to be applied, got %d", m.leftPaneWidth)
	}
	if !m.collapsed[repoKey("repo-a")] {
		t.Error("expected the loaded collapsed key repo:repo-a to be applied")
	}
	// repo-a's children must actually be hidden - not just the map entry set.
	for _, r := range m.rows {
		if r.kind == rowWorktree && r.repo == "repo-a" {
			t.Errorf(
				"expected repo-a's rows to be collapsed after loading saved state, found %+v",
				r,
			)
		}
	}
}

func TestViewStateMsgNotOkLeavesDefaults(t *testing.T) {
	m := makeTestModel(testStatuses())
	before := m.leftPaneWidth

	updated, _ := m.Update(viewStateMsg{ok: false})
	m = updated.(Model)

	if m.leftPaneWidth != before {
		t.Errorf(
			"expected a miss (ok=false) to leave leftPaneWidth untouched, got %d (was %d)",
			m.leftPaneWidth,
			before,
		)
	}
	if len(m.collapsed) != 0 {
		t.Errorf("expected a miss (ok=false) to leave collapsed empty, got %v", m.collapsed)
	}
}

// TestFoldChangeSavesPrunedViewState covers the plan's "pruning" and "a
// failed write not blocking the fold" cases together: h collapses repo-a,
// which must be the only surviving key in what gets saved even though
// m.collapsed also holds a stale entry for a repo that no longer exists.
func TestFoldChangeSavesPrunedViewState(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	m.collapsed[repoKey("repo-long-gone")] = true // stale, must not survive

	var savedName, savedValue string
	var calls int
	m.setGlobalOptionFn = func(name, value string) error {
		calls++
		savedName, savedValue = name, value
		return nil
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'h'}) // collapses repo-a (cursor starts there)
	m = updated.(Model)

	if calls != 1 {
		t.Fatalf("expected exactly one save on a repo fold, got %d", calls)
	}
	if savedName != viewStateOptionName {
		t.Errorf("expected the option name %q, got %q", viewStateOptionName, savedName)
	}
	vs, ok := decodeViewState(savedValue)
	if !ok {
		t.Fatalf("expected a decodable saved value, got %q", savedValue)
	}
	if len(vs.Collapsed) != 1 || vs.Collapsed[0] != repoKey("repo-a") {
		t.Errorf("expected only repo:repo-a to survive pruning, got %v", vs.Collapsed)
	}
	if m.collapsed[repoKey("repo-long-gone")] {
		t.Error("expected the stale repo-long-gone key to be pruned from the in-memory map too")
	}
}

// TestFailedViewStateWriteDoesNotBlockFold: a write failure (tmux's
// "command too long" at ~20 KB, or any other exec error) must not stop the
// fold itself, and must not panic.
func TestFailedViewStateWriteDoesNotBlockFold(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	m.setGlobalOptionFn = func(_, _ string) error {
		return errors.New("command too long")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m = updated.(Model)

	if !m.collapsed[repoKey("repo-a")] {
		t.Error("expected the fold to apply even though the save failed")
	}
}

// TestNilSetGlobalOptionFnDoesNotBlockFold covers a test model that (like
// makeTestModel) never wires the seam at all - saveViewState must be a no-op,
// not a nil-pointer panic.
func TestNilSetGlobalOptionFnDoesNotBlockFold(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	m.setGlobalOptionFn = nil

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m = updated.(Model)

	if !m.collapsed[repoKey("repo-a")] {
		t.Error("expected the fold to apply with no save seam wired")
	}
}

func TestDragSavesViewStateOnceOnReleaseNotDuringMotion(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a", Path: "/tmp/a", TmuxWindow: "wt-feature-a"},
	}
	m := makeTestModel(statuses)
	m.mgr = newTestWorktreeManager()
	m.width = 120
	var calls int
	m.setGlobalOptionFn = func(_, _ string) error {
		calls++
		return nil
	}

	m.dragging = true
	updated, _ := m.Update(tea.MouseMotionMsg{X: 50, Y: 0})
	m = updated.(Model)
	updated, _ = m.Update(tea.MouseMotionMsg{X: 55, Y: 0})
	m = updated.(Model)
	if calls != 0 {
		t.Fatalf("expected no save while the drag is still in motion, got %d calls", calls)
	}

	updated, _ = m.Update(tea.MouseReleaseMsg{})
	m = updated.(Model)
	if calls != 1 {
		t.Errorf("expected exactly one save when the drag ends, got %d calls", calls)
	}

	// A release that was never a drag (e.g. a plain click) must not save.
	updated, _ = m.Update(tea.MouseReleaseMsg{})
	m = updated.(Model)
	if calls != 1 {
		t.Errorf("expected a release with no active drag not to save again, got %d calls", calls)
	}
}

// TestPlainLPressOnExpandedRepoDoesNotSaveViewState guards against a real
// defect caught while writing this feature: l's priority-2 branch computes
// expandRepo on nearly every plain l press, not just one that actually
// re-expands a collapsed repo (a worktree row is only visible/reachable
// while its repo is expanded, so pressing l there sets collapsed[repo] to
// false again, a no-op). An unconditional save there would write
// @dg_ws_state on the single most common keypress.
func TestPlainLPressOnExpandedRepoDoesNotSaveViewState(t *testing.T) {
	m := makeTestModel(testStatuses()) // cursor starts on repo-a/feature-a, already expanded
	m.mgr = newTestWorktreeManager()
	var calls int
	m.setGlobalOptionFn = func(_, _ string) error {
		calls++
		return nil
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'l'})
	m = updated.(Model)

	if calls != 0 {
		t.Errorf(
			"expected a plain l press on an already-expanded repo not to save, got %d calls",
			calls,
		)
	}
}

func TestESavesViewState(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	m.width = 120
	var calls int
	m.setGlobalOptionFn = func(_, _ string) error {
		calls++
		return nil
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'e'})
	m = updated.(Model)

	if calls != 1 {
		t.Errorf("expected e to save the view state once, got %d calls", calls)
	}
}
