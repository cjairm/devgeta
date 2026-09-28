package tuiworktree

// Tests for the second half of Step 6 of
// docs/plans/cycles/2026-09-28-ws-agents-section.md: a repo with no open
// window starts folded, applied once per repo per launch (ADR-0056).

import (
	"testing"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func windowlessRepoStatuses() []worktree.WorktreeStatus {
	return []worktree.WorktreeStatus{
		{Repo: "devgeta", Name: "a", Path: "/tmp/devgeta-a", WindowActive: false},
	}
}

// freshLaunchModel builds a model the way a real fresh launch would: no repo
// pre-marked as explicitly expanded - unlike plain makeTestModel(statuses),
// which pre-marks every initial repo expanded so the hundreds of pre-existing
// fixtures that predate this feature keep rendering open by default (see
// makeTestModel's own comment) - and sessionsLoaded already true, since
// applyDefaultFolds waits for it (the same guard placeCursorOnActive already
// waits on) before ever deciding a repo's fold, to avoid locking in a
// decision made against a session scan that hasn't been classified against
// the worktree list yet. These tests want to observe the REAL windowless-repo
// default-fold decision, so they must bypass both shortcuts.
func freshLaunchModel(statuses []worktree.WorktreeStatus) Model {
	m := makeTestModel(nil)
	m.sessionsLoaded = true
	m.statuses = statuses
	m.rebuildRows()
	return m
}

func TestDefaultFold_WindowlessRepoStartsFoldedOnFreshLaunch(t *testing.T) {
	m := freshLaunchModel(windowlessRepoStatuses())

	if !m.collapsed[repoKey("devgeta")] {
		t.Errorf("expected a windowless repo to start folded on a fresh launch")
	}
	for _, r := range m.rows {
		if r.kind == rowWorktree {
			t.Errorf(
				"expected no worktree row visible while the repo is default-folded, got %+v",
				r,
			)
		}
	}
}

func TestDefaultFold_UnfoldingSurvivesTheNextTick(t *testing.T) {
	m := freshLaunchModel(windowlessRepoStatuses())
	if !m.collapsed[repoKey("devgeta")] {
		t.Fatalf("test setup: expected the repo to start folded")
	}

	// Unfold it, the way enter/l does (expandCollapsedRepoHeader), by
	// landing the cursor on the repo header first.
	for i, r := range m.rows {
		if r.kind == rowRepo && r.repo == "devgeta" {
			m.cursor = i
		}
	}
	m.expandCollapsedRepoHeader()
	if m.collapsed[repoKey("devgeta")] {
		t.Fatalf("test setup: expected the repo to be unfolded after expandCollapsedRepoHeader")
	}

	// Simulate the 3-second tick's rebuild - it must never re-apply the
	// windowless default now that this repo has already been decided once
	// this launch.
	m.rebuildRows()

	if m.collapsed[repoKey("devgeta")] {
		t.Errorf("expected the repo to stay open across a rebuild, got re-folded")
	}
}

func TestDefaultFold_SavedExpandedEntryOpensOnNextLaunch(t *testing.T) {
	// Simulate a fresh launch (empty defaultFoldSeen) that already has a
	// saved "expanded" choice for this repo - Step 10 wires the load path
	// that populates m.expanded from disk; this test exercises the decision
	// logic directly, independent of that wiring.
	m := makeTestModel(nil)
	m.sessionsLoaded = true
	m.expanded[repoKey("devgeta")] = true
	m.statuses = windowlessRepoStatuses()
	m.rebuildRows()

	if m.collapsed[repoKey("devgeta")] {
		t.Errorf("expected a saved expanded entry to keep a windowless repo open on load")
	}
	foundWorktree := false
	for _, r := range m.rows {
		if r.kind == rowWorktree && r.repo == "devgeta" {
			foundWorktree = true
		}
	}
	if !foundWorktree {
		t.Errorf("expected the worktree row to be visible, got rows: %+v", m.rows)
	}
}

func TestDefaultFold_SavedCollapsedEntryIsRespected(t *testing.T) {
	// The mirror image of the expanded case: an explicit saved fold must
	// also count as "already has a choice" - the default must not overwrite
	// it (it wouldn't change anything here, but this pins that the check
	// looks at collapsed too, not just expanded).
	m := makeTestModel(nil)
	m.sessionsLoaded = true
	m.collapsed[repoKey("devgeta")] = true
	m.statuses = []worktree.WorktreeStatus{
		{Repo: "devgeta", Name: "a", Path: "/tmp/devgeta-a", WindowActive: true},
	}
	m.rebuildRows()

	if !m.collapsed[repoKey("devgeta")] {
		t.Errorf(
			"expected the explicit saved fold to be respected even though the repo has a window",
		)
	}
}

func TestDefaultFold_RepoLosingItsLastWindowMidLaunchIsNotReFolded(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Repo: "devgeta", Name: "a", Path: "/tmp/devgeta-a", WindowActive: true},
	}
	m := freshLaunchModel(statuses)
	if m.collapsed[repoKey("devgeta")] {
		t.Fatalf("test setup: expected the repo to start open (it has a window)")
	}

	// The worktree's window closes mid-launch.
	m.statuses[0].WindowActive = false
	m.rebuildRows()

	if m.collapsed[repoKey("devgeta")] {
		t.Errorf(
			"expected a repo that loses its last window mid-launch to stay open, not be re-folded",
		)
	}
}

func TestDefaultFold_AppliesToARepoThatAppearsLater(t *testing.T) {
	m := makeTestModel(nil)
	m.sessionsLoaded = true
	if len(m.statuses) != 0 {
		t.Fatalf("test setup: expected no repos yet")
	}

	// The repo appears later (e.g. a new worktree is created, or the slow
	// git load first sees it).
	m.statuses = windowlessRepoStatuses()
	m.rebuildRows()

	if !m.collapsed[repoKey("devgeta")] {
		t.Errorf("expected a windowless repo appearing later this launch to still start folded")
	}
}
