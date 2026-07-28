package tuiworktree

import (
	"testing"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// --- placeCursorOnActive ---

func TestPlaceCursorOnActiveLandsOnCurrentStandaloneSession(t *testing.T) {
	// Regression test: a worktree window existing elsewhere on the server
	// must not win just because its window exists — only the session
	// dg ws is actually running in should. See placeCursorOnActive's doc
	// comment for why WindowActive alone is the wrong signal.
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a", WindowActive: true},
	}
	m := makeTestModel(statuses)
	m.sessions = []worktree.SessionStatus{
		{Name: "devgita-cell", Attached: false},
		{Name: "misc", Attached: false},
	}
	m.rebuildRows()
	m.currentSessionFn = func() (string, bool) { return "devgita-cell", true }
	m.loaded = true
	m.sessionsLoaded = true
	m.cursor = 0 // simulate the fresh-open default before placement runs
	m.placeCursorOnActive()

	got := m.rows[m.cursor]
	if got.kind != rowSession || got.session.Name != "devgita-cell" {
		t.Errorf("expected cursor on session 'devgita-cell', got %+v", got)
	}
	if !m.cursorPlaced {
		t.Error("expected cursorPlaced to be true after landing on the current session")
	}
}

func TestPlaceCursorOnActiveLandsOnWorktreeInCurrentSession(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a", WindowActive: true},
		{Name: "feature-b", Repo: "repo-b", WindowActive: true},
	}
	m := makeTestModel(statuses)
	m.currentSessionFn = func() (string, bool) {
		return worktree.TmuxSessionName("repo-b"), true
	}
	m.loaded = true
	m.sessionsLoaded = true
	m.cursor = 0
	m.placeCursorOnActive()

	got := m.rows[m.cursor]
	if got.kind != rowWorktree || got.status.Name != "feature-b" {
		t.Errorf("expected cursor on worktree 'feature-b' (repo-b's session), got %+v", got)
	}
	if !m.cursorPlaced {
		t.Error("expected cursorPlaced to be true after landing on the current session's worktree")
	}
}

func TestPlaceCursorOnActiveGivesUpWhenCurrentSessionUnknown(t *testing.T) {
	statuses := []worktree.WorktreeStatus{{Name: "feature-a", Repo: "repo-a"}}
	m := makeTestModel(statuses)
	m.sessions = []worktree.SessionStatus{{Name: "alpha"}}
	m.currentSessionFn = func() (string, bool) { return "", false } // not in tmux
	m.cursor = 0

	// Both loads not yet complete: must not give up prematurely.
	m.loaded = true
	m.sessionsLoaded = false
	m.rebuildRows()
	m.placeCursorOnActive()
	if m.cursorPlaced {
		t.Fatal("expected placeCursorOnActive not to give up before both initial loads complete")
	}

	// Second load completes with still nothing to match: must give up now,
	// rather than retrying forever.
	m.sessionsLoaded = true
	m.rebuildRows()
	m.placeCursorOnActive()
	if !m.cursorPlaced {
		t.Error(
			"expected placeCursorOnActive to give up once both loads completed with no current session",
		)
	}
}

func TestPlaceCursorOnActiveGivesUpWhenCurrentSessionNotInRows(t *testing.T) {
	statuses := []worktree.WorktreeStatus{{Name: "feature-a", Repo: "repo-a"}}
	m := makeTestModel(statuses)
	m.currentSessionFn = func() (string, bool) { return "unrelated-session", true }
	m.loaded = true
	m.sessionsLoaded = true
	m.cursor = 0
	m.placeCursorOnActive()

	if !m.cursorPlaced {
		t.Error("expected placeCursorOnActive to give up when the current session matches no row")
	}
	if m.cursor != 0 {
		t.Errorf("expected cursor to stay untouched at 0, got %d", m.cursor)
	}
}

func TestPlaceCursorOnActiveDoesNotClobberLaterNavigation(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a"},
		{Name: "feature-b", Repo: "repo-a"},
	}
	m := makeTestModel(statuses)
	m.currentSessionFn = func() (string, bool) {
		return worktree.TmuxSessionName("repo-a"), true
	}
	m.loaded = true
	m.sessionsLoaded = true
	m.placeCursorOnActive()
	if !m.cursorPlaced {
		t.Fatal("test setup: expected placeCursorOnActive to succeed")
	}

	// User navigates away, then a periodic refresh rebuilds rows — this must
	// not fight the user's own navigation by re-landing on the matched row.
	m.moveCursor(1)
	movedTo := m.cursor
	m.rebuildRows()
	m.placeCursorOnActive()
	if m.cursor != movedTo {
		t.Errorf(
			"expected cursor to stay at %d after a post-placement refresh, got %d",
			movedTo,
			m.cursor,
		)
	}
}
