package tuiworktree

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/apps/tmux"
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

// TestPlaceCursorOnActiveLandsOnRepoSessionRow is B8's fix: placement matches
// the current session against session rows by their REAL name - repo-session
// rows included - rather than a worktree row's derived TmuxSessionName(repo),
// which the dashboard's own repo-session feature already proved can be wrong
// (a repo's windows can live in a session named anything at all).
func TestPlaceCursorOnActiveLandsOnRepoSessionRow(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a", WindowActive: true},
		{Name: "feature-b", Repo: "repo-b", WindowActive: true},
	}
	m := makeTestModel(statuses)
	m.repoSessions = []worktree.RepoSessionStatus{{Repo: "repo-b", Name: "repo-b-nicknamed"}}
	m.rebuildRows()
	m.currentSessionFn = func() (string, bool) { return "repo-b-nicknamed", true }
	m.loaded = true
	m.sessionsLoaded = true
	m.cursor = 0
	m.placeCursorOnActive()

	got := m.rows[m.cursor]
	if got.kind != rowRepoSession || got.repoSession.Name != "repo-b-nicknamed" {
		t.Errorf("expected cursor on repo-b's session row 'repo-b-nicknamed', got %+v", got)
	}
	if !m.cursorPlaced {
		t.Error("expected cursorPlaced to be true after landing on the current session's row")
	}
}

// TestPlaceCursorOnActiveDoesNotMatchAWorktreeRowsDerivedName pins B8's
// negative case: a worktree row's TmuxSessionName(repo) coincidentally
// matching the current session must NOT be enough on its own - only an
// actual session row (repo-session or standalone) with that real name is.
func TestPlaceCursorOnActiveDoesNotMatchAWorktreeRowsDerivedName(t *testing.T) {
	statuses := []worktree.WorktreeStatus{{Name: "feature-a", Repo: "repo-a", WindowActive: true}}
	m := makeTestModel(statuses)
	// No repoSessions entry: nothing backs TmuxSessionName("repo-a") as a real
	// session row.
	m.currentSessionFn = func() (string, bool) { return worktree.TmuxSessionName("repo-a"), true }
	m.loaded = true
	m.sessionsLoaded = true
	before := m.cursor
	m.placeCursorOnActive()

	if m.cursor != before {
		t.Errorf(
			"expected placeCursorOnActive to give up (no session row matches), cursor moved from %d to %d",
			before,
			m.cursor,
		)
	}
	if !m.cursorPlaced {
		t.Error("expected cursorPlaced to be true even when it gave up")
	}
}

// A session holding only worktree windows has no row (see
// worktree.RepoSessionStatuses), so opening the dashboard from inside it lands
// on the worktree row whose window lives there - matched by the pane's REAL
// session, never the derived name the test above rejects.
func TestPlaceCursorOnActiveFallsBackToWorktreeRowInCurrentSession(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a"},
		{
			Name:  "feature-b",
			Repo:  "repo-b",
			Panes: []tmux.PaneState{{Session: "taskqueue2", PaneID: "%1"}},
		},
	}
	m := makeTestModel(statuses)
	m.currentSessionFn = func() (string, bool) { return "taskqueue2", true }
	m.loaded = true
	m.sessionsLoaded = true
	m.cursor = 0
	m.placeCursorOnActive()

	got := m.rows[m.cursor]
	if got.kind != rowWorktree || got.status.Name != "feature-b" {
		t.Errorf("expected cursor on worktree row 'feature-b', got %+v", got)
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
	// Distinct Path values matter here: rebuildRows now restores the cursor by
	// rowKey (repo+path), and two rows sharing an (empty) path would collide
	// and defeat the very thing this test checks.
	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a", Path: "/tmp/a"},
		{Name: "feature-b", Repo: "repo-a", Path: "/tmp/b"},
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

	mi, _ := m.Update(sessionsMsg{layer: sessionsLayer(sessions)})
	m = mi.(Model)
	if m.cursorPlaced {
		t.Error("expected placement to wait for the worktree load before committing")
	}

	mi, _ = m.Update(statusesMsg{statuses: statuses})
	m = mi.(Model)
	// The first worktree load re-dispatches a session scan (see statusesMsg);
	// placement waits for it, since the first scan was classified blind.
	mi, _ = m.Update(sessionsMsg{layer: sessionsLayer(sessions)})
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

	mi, _ := m.Update(statusesMsg{statuses: statuses})
	m = mi.(Model)
	if m.cursorPlaced {
		t.Error("expected placement to wait for the session load before giving up")
	}

	mi, _ = m.Update(sessionsMsg{layer: sessionsLayer(sessions)})
	m = mi.(Model)

	got := m.rows[m.cursor]
	if got.kind != rowSession || got.session.Name != "misc" {
		t.Errorf("expected cursor on session 'misc' once both loads landed, got %+v", got)
	}
}

// The real startup order for a repo session: the session scan lands first,
// before any worktree window is known, so it builds no repo-session row. The
// worktree load must not place the cursor on those rows - it would miss the
// session and land on its worktree row instead. Placement waits for the
// re-dispatched scan, which does see the row.
func TestPlacementWaitsForRepoSessionRowWhenSessionsArriveFirst(t *testing.T) {
	m := makeTestModel(nil)
	m.currentSessionFn = func() (string, bool) { return "devgeta-yamcha", true }

	statuses := []worktree.WorktreeStatus{{Name: "ws-dashboard-refresh", Repo: "devgeta"}}
	window := worktree.GetWindowName("devgeta", "ws-dashboard-refresh")
	wtPane := tmux.PaneState{Session: "devgeta-yamcha", Window: window, PaneID: "%1"}
	layer := worktree.StateLayer{
		Sessions:      []tmux.SessionInfo{{Name: "devgeta-yamcha", Attached: true}},
		PanesByWindow: map[string][]tmux.PaneState{window: {wtPane}},
		PanesBySession: map[string][]tmux.PaneState{
			"devgeta-yamcha": {
				wtPane,
				{Session: "devgeta-yamcha", Window: "2.1.282", PaneID: "%2"},
			},
		},
	}

	mi, _ := m.Update(sessionsMsg{layer: layer})
	m = mi.(Model)
	mi, cmd := m.Update(statusesMsg{statuses: statuses})
	m = mi.(Model)
	if m.cursorPlaced {
		t.Fatal(
			"expected placement to wait for a session scan classified against the worktree list",
		)
	}
	if cmd == nil {
		t.Fatal("expected the first worktree load to re-dispatch a session scan")
	}
	mi, _ = m.Update(sessionsMsg{layer: layer})
	m = mi.(Model)

	got := m.rows[m.cursor]
	if got.kind != rowRepoSession || got.repoSession.Name != "devgeta-yamcha" {
		t.Errorf("expected cursor on session row 'devgeta-yamcha', got %+v", got)
	}
}

// A worktree-backed repo-session row, mixed with standalone sessions: the
// repo-session row must win over any standalone session row (ADR-0052/B8),
// and the trailing standalone rows must not pull the cursor off it.
//
// This drives statusesMsg BEFORE sessionsMsg, unlike the "arriving first"
// pair above: RepoSessionStatuses (computed inside the sessionsMsg handler)
// reads m.statuses' Repo/Name to derive each window name, so it only finds
// repo-b's session once the worktree list has actually landed.
func TestPlacementLandsOnRepoSessionRowWhenMixedWithStandaloneSessions(t *testing.T) {
	m := makeTestModel(nil)
	m.currentSessionFn = func() (string, bool) { return "repo-b-nicknamed", true }

	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a"},
		{Name: "feature-c", Repo: "repo-b"},
	}
	window := worktree.GetWindowName("repo-b", "feature-c")
	layer := worktree.StateLayer{
		Sessions: []tmux.SessionInfo{{Name: "repo-b-nicknamed"}, {Name: "alpha"}, {Name: "zeta"}},
		PanesByWindow: map[string][]tmux.PaneState{
			window: {{Session: "repo-b-nicknamed", Window: window, PaneID: "%1"}},
		},
		PanesBySession: map[string][]tmux.PaneState{
			"repo-b-nicknamed": {
				{Session: "repo-b-nicknamed", Window: window, PaneID: "%1"},
				// A plain window: without one the session gets no row at all.
				{Session: "repo-b-nicknamed", Window: "zsh", PaneID: "%2"},
			},
		},
	}

	mi, _ := m.Update(statusesMsg{statuses: statuses})
	m = mi.(Model)
	mi, _ = m.Update(sessionsMsg{layer: layer})
	m = mi.(Model)

	got := m.rows[m.cursor]
	if got.kind != rowRepoSession || got.repoSession.Name != "repo-b-nicknamed" {
		t.Errorf("expected cursor on repo-b's session row, got %+v", got)
	}
}

// The same startup sequence for a session holding only its worktree window:
// no session row exists, so placement must land on that worktree's row. The
// sessionsMsg carries the only scan the model has seen at this point, so it
// has to be what tells the fallback which session each worktree lives in.
func TestPlacementLandsOnWorktreeRowForWorktreeOnlySession(t *testing.T) {
	m := makeTestModel(nil)
	m.currentSessionFn = func() (string, bool) { return "taskqueue2", true }

	statuses := []worktree.WorktreeStatus{
		{Name: "feature-a", Repo: "repo-a"},
		{Name: "feature-c", Repo: "repo-b"},
	}
	window := worktree.GetWindowName("repo-b", "feature-c")
	pane := tmux.PaneState{Session: "taskqueue2", Window: window, PaneID: "%1"}
	layer := worktree.StateLayer{
		Sessions:       []tmux.SessionInfo{{Name: "taskqueue2"}},
		PanesByWindow:  map[string][]tmux.PaneState{window: {pane}},
		PanesBySession: map[string][]tmux.PaneState{"taskqueue2": {pane}},
	}

	mi, _ := m.Update(statusesMsg{statuses: statuses})
	m = mi.(Model)
	mi, _ = m.Update(sessionsMsg{layer: layer})
	m = mi.(Model)

	for _, r := range m.rows {
		if r.kind == rowRepoSession {
			t.Fatalf("expected no session row for a worktree-only session, got %+v", r)
		}
	}
	got := m.rows[m.cursor]
	if got.kind != rowWorktree || got.status.Name != "feature-c" {
		t.Errorf("expected cursor on worktree row 'feature-c', got %+v", got)
	}
}

// The periodic refresh re-sends both messages every 3s. Placement is a
// once-per-launch action, so those later rounds must leave the cursor alone.
func TestPlacementIgnoresPeriodicRefreshAfterPlacing(t *testing.T) {
	m := makeTestModel(nil)
	m.currentSessionFn = func() (string, bool) { return "misc", true }

	sessions := []worktree.SessionStatus{{Name: "alpha"}, {Name: "misc"}}
	statuses := []worktree.WorktreeStatus{{Name: "feature-a", Repo: "repo-a"}}

	mi, _ := m.Update(sessionsMsg{layer: sessionsLayer(sessions)})
	m = mi.(Model)
	mi, _ = m.Update(statusesMsg{statuses: statuses})
	m = mi.(Model)
	// The re-dispatched scan the first worktree load triggers.
	mi, _ = m.Update(sessionsMsg{layer: sessionsLayer(sessions)})
	m = mi.(Model)
	if !m.cursorPlaced {
		t.Fatal("test setup: expected placement after the startup loads")
	}

	m.moveCursor(-1) // user navigates away from the placed row
	movedTo := m.cursor

	mi, _ = m.Update(sessionsMsg{layer: sessionsLayer(sessions)})
	m = mi.(Model)
	mi, _ = m.Update(statusesMsg{statuses: statuses})
	m = mi.(Model)

	if m.cursor != movedTo {
		t.Errorf("expected refresh to leave cursor at %d, got %d", movedTo, m.cursor)
	}
}

// originModel is devgeta-yamcha as it really is: a worktree window and a
// plain window in one session, so the session has its own row too.
func originModel(t *testing.T, origin string) Model {
	t.Helper()
	window := worktree.GetWindowName("devgeta", "ws-dashboard-refresh")
	statuses := []worktree.WorktreeStatus{{
		Name:       "ws-dashboard-refresh",
		Repo:       "devgeta",
		TmuxWindow: window,
		Panes:      []tmux.PaneState{{Session: "devgeta-yamcha", Window: window, PaneID: "%1"}},
	}}
	m := makeTestModel(statuses)
	m.repoSessions = []worktree.RepoSessionStatus{{Repo: "devgeta", Name: "devgeta-yamcha"}}
	m.rebuildRows()
	m.currentSessionFn = func() (string, bool) { return "devgeta-yamcha", true }
	m.originWindowFn = func() (string, bool) { return origin, true }
	m.loaded = true
	m.sessionsLoaded = true
	m.cursor = 0
	return m
}

// Coming from the worktree window, the worktree row wins over its session's.
func TestPlaceCursorOnActivePrefersOriginWorktreeWindow(t *testing.T) {
	m := originModel(t, worktree.GetWindowName("devgeta", "ws-dashboard-refresh"))
	m.placeCursorOnActive()

	got := m.rows[m.cursor]
	if got.kind != rowWorktree || got.status.Name != "ws-dashboard-refresh" {
		t.Errorf("expected cursor on worktree row 'ws-dashboard-refresh', got %+v", got)
	}
}

// Coming from a plain window in the same session, the session row wins.
func TestPlaceCursorOnActiveUsesSessionRowFromPlainWindow(t *testing.T) {
	m := originModel(t, "2.1.282")
	m.placeCursorOnActive()

	got := m.rows[m.cursor]
	if got.kind != rowRepoSession || got.repoSession.Name != "devgeta-yamcha" {
		t.Errorf("expected cursor on session row 'devgeta-yamcha', got %+v", got)
	}
}

// j/k pressed before the startup loads finish is the user choosing where to
// be; the placement that runs when the loads land must not undo it.
func TestPlacementYieldsToNavigationBeforeLoad(t *testing.T) {
	m := makeTestModel(nil)
	m.currentSessionFn = func() (string, bool) { return "misc", true }
	sessions := []worktree.SessionStatus{{Name: "alpha"}, {Name: "misc"}}
	statuses := []worktree.WorktreeStatus{{Name: "feature-a", Repo: "repo-a"}}

	mi, _ := m.Update(sessionsMsg{layer: sessionsLayer(sessions)})
	m = mi.(Model)
	mi, _ = m.Update(statusesMsg{statuses: statuses})
	m = mi.(Model)
	mi, _ = m.Update(tea.KeyPressMsg{Code: 'k'}) // before the re-dispatched scan lands
	m = mi.(Model)
	movedTo := m.cursor

	mi, _ = m.Update(sessionsMsg{layer: sessionsLayer(sessions)})
	m = mi.(Model)

	if m.cursor != movedTo {
		t.Errorf("expected the user's cursor at %d to stand, got %d (%+v)",
			movedTo, m.cursor, m.rows[m.cursor])
	}
}
