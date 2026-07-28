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
		{Name: "devgeta-cell", Attached: false},
		{Name: "misc", Attached: false},
	}
	m.rebuildRows()
	m.currentSessionFn = func() (string, bool) { return "devgeta-cell", true }
	m.loaded = true
	m.sessionsLoaded = true
	m.cursor = 0 // simulate the fresh-open default before placement runs
	m.placeCursorOnActive()

	got := m.rows[m.cursor]
	if got.kind != rowSession || got.session.Name != "devgeta-cell" {
		t.Errorf("expected cursor on session 'devgeta-cell', got %+v", got)
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

// --- placement vs. the real message arrival order ---
//
// The cases above call placeCursorOnActive directly with both lists already
// populated. These drive Update instead, because the ordering between the two
// initial loads is what previously broke placement: m.cursor is a positional
// index and rebuildRows can only clamp it, so placing before both lists are in
// let a later rebuild slide the cursor onto a different row.

// sessionsMsg wins the race against the slower worktree scan — the common case,
// since ListSessions is one tmux call and List walks the filesystem per repo.
func TestPlacementSurvivesSessionsMsgArrivingFirst(t *testing.T) {
	m := makeTestModel(nil) // no worktrees yet: a fresh launch, mid-load
	m.currentSessionFn = func() (string, bool) { return "misc", true }

	sessions := []worktree.SessionStatus{{Name: "alpha"}, {Name: "beta"}, {Name: "misc"}}
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a"},
		{Name: "feature-b", Repo: "repo-a"},
		{Name: "feature-c", Repo: "repo-b"},
	}

	mi, _ := m.Update(sessionsMsg(sessions))
	m = mi.(Model)
	if m.cursorPlaced {
		t.Error("expected placement to wait for the worktree load before committing")
	}

	mi, _ = m.Update(statusesMsg(statuses))
	m = mi.(Model)

	got := m.rows[m.cursor]
	if got.kind != rowSession || got.session.Name != "misc" {
		t.Errorf("expected cursor on session 'misc' once both loads landed, got %+v", got)
	}
}

// statusesMsg wins instead: placement must come out the same, and must not give
// up early just because the current session is absent from a half-built list.
func TestPlacementSurvivesStatusesMsgArrivingFirst(t *testing.T) {
	m := makeTestModel(nil)
	m.currentSessionFn = func() (string, bool) { return "misc", true }

	statuses := []worktree.WorktreeStatus{{Name: "feature-a", Repo: "repo-a"}}
	sessions := []worktree.SessionStatus{{Name: "alpha"}, {Name: "misc"}}

	mi, _ := m.Update(statusesMsg(statuses))
	m = mi.(Model)
	if m.cursorPlaced {
		t.Error("expected placement to wait for the session load before giving up")
	}

	mi, _ = m.Update(sessionsMsg(sessions))
	m = mi.(Model)

	got := m.rows[m.cursor]
	if got.kind != rowSession || got.session.Name != "misc" {
		t.Errorf("expected cursor on session 'misc' once both loads landed, got %+v", got)
	}
}

// A worktree-backed session, mixed with standalone sessions: the repo's row
// must win over any session row, and the trailing session rows must not pull
// the cursor off it.
func TestPlacementLandsOnWorktreeWhenMixedWithSessions(t *testing.T) {
	m := makeTestModel(nil)
	m.currentSessionFn = func() (string, bool) {
		return worktree.TmuxSessionName("repo-b"), true
	}

	sessions := []worktree.SessionStatus{{Name: "alpha"}, {Name: "zeta"}}
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a"},
		{Name: "feature-c", Repo: "repo-b"},
	}

	mi, _ := m.Update(sessionsMsg(sessions))
	m = mi.(Model)
	mi, _ = m.Update(statusesMsg(statuses))
	m = mi.(Model)

	got := m.rows[m.cursor]
	if got.kind != rowWorktree || got.status.Repo != "repo-b" {
		t.Errorf("expected cursor on a repo-b worktree row, got %+v", got)
	}
}

// The periodic refresh re-sends both messages every 3s. Placement is a
// once-per-launch action, so those later rounds must leave the cursor alone.
func TestPlacementIgnoresPeriodicRefreshAfterPlacing(t *testing.T) {
	m := makeTestModel(nil)
	m.currentSessionFn = func() (string, bool) { return "misc", true }

	sessions := []worktree.SessionStatus{{Name: "alpha"}, {Name: "misc"}}
	statuses := []worktree.WorktreeStatus{{Name: "feature-a", Repo: "repo-a"}}

	mi, _ := m.Update(sessionsMsg(sessions))
	m = mi.(Model)
	mi, _ = m.Update(statusesMsg(statuses))
	m = mi.(Model)

	m.moveCursor(-1) // user navigates away from the placed row
	movedTo := m.cursor

	mi, _ = m.Update(sessionsMsg(sessions))
	m = mi.(Model)
	mi, _ = m.Update(statusesMsg(statuses))
	m = mi.(Model)

	if m.cursor != movedTo {
		t.Errorf("expected refresh to leave cursor at %d, got %d", movedTo, m.cursor)
	}
}
