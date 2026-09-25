package tuiworktree

// Tests for ADR-0052's enter/navigation behavior: a repo header is a label
// (never a switch target, never a j/k stop while expanded), and a
// repo-session row is what enter now switches to instead - its first
// plain window (a row only exists for a session that has one).

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/cjairm/devgeta/internal/apps/tmux"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func repoHeaderRowIndex(t *testing.T, m Model, repo string) int {
	t.Helper()
	for i, r := range m.rows {
		if r.kind == rowRepo && r.repo == repo {
			return i
		}
	}
	t.Fatalf("test setup: no header row for repo %q", repo)
	return -1
}

func focusRepoSessionRow(t *testing.T, m Model, name string) Model {
	t.Helper()
	if _, ok := m.focusRow(func(r row) bool {
		return r.kind == rowRepoSession && r.repoSession.Name == name
	}); !ok {
		t.Fatalf("test setup: no repo-session row named %q", name)
	}
	return m
}

// --- the header is a label ---

func TestExpandedRepoHeaderNeverNavigable(t *testing.T) {
	// A plain window in the repo's session used to be exactly what made the
	// OLD header navigable (ADR-0048) - using that same setup here is what
	// makes this a real regression guard rather than a coincidence of an
	// empty fixture.
	m := makeTestModel(repoWithSession("repo-a", "repo-a"))
	m.plainWindowBySession = map[string]string{"repo-a": "zsh"}
	idx := repoHeaderRowIndex(t, m, "repo-a")

	for _, i := range m.navigableIndices() {
		if i == idx {
			t.Fatal(
				"an expanded repo header must never be a j/k stop (ADR-0052): it is a label, not a switch target",
			)
		}
	}
}

func TestCollapsedRepoHeaderStaysNavigable(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.collapsed[repoKey("repo-a")] = true
	m.rebuildRows()
	idx := repoHeaderRowIndex(t, m, "repo-a")

	found := false
	for _, i := range m.navigableIndices() {
		if i == idx {
			found = true
		}
	}
	if !found {
		t.Error("a collapsed header must stay reachable so l/enter can expand it")
	}
}

func TestEnterOnCollapsedRepoHeaderExpandsIt(t *testing.T) {
	m := makeTestModel(testStatuses()) // repo-a: feature-a, feature-b
	m.collapsed[repoKey("repo-a")] = true
	m.rebuildRows()
	m.cursor = repoHeaderRowIndex(t, m, "repo-a")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	if m.collapsed[repoKey("repo-a")] {
		t.Error("expected enter on a collapsed header to expand it")
	}
	if m.rows[m.cursor].kind != rowWorktree || m.rows[m.cursor].repo != "repo-a" {
		t.Errorf(
			"expected the cursor to land on repo-a's first worktree row, got %+v",
			m.rows[m.cursor],
		)
	}
}

// TestEnterOnExpandedRepoHeaderIsANoOp is defensive: an expanded header is
// unreachable via navigation (see above), but forcing the cursor there
// directly must still do nothing, per ADR-0052's "enter on a header never
// switches sessions."
func TestEnterOnExpandedRepoHeaderIsANoOp(t *testing.T) {
	m := makeTestModel(testStatuses())
	idx := repoHeaderRowIndex(t, m, "repo-a")
	m.cursor = idx

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := updated.(Model)

	if cmd != nil {
		t.Error("expected enter on an expanded header to return no command")
	}
	if m2.cursor != idx || m2.rows[m2.cursor].kind != rowRepo {
		t.Error("expected enter to leave the cursor on the header, untouched")
	}
}

// --- enter on a repo-session row ---

func TestEnterOnRepoSessionRowSwitchesToPlainWindow(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := makeTestModel(testStatuses())
	m.repoSessions = []worktree.RepoSessionStatus{{Repo: "repo-a", Name: "repo-a-tien"}}
	m.plainWindowBySession = map[string]string{"repo-a-tien": "zsh"}
	m.rebuildRows()
	m = focusRepoSessionRow(t, m, "repo-a-tien")
	var gotSession, gotWindow string
	m.attachFn = func(session, window string) error {
		gotSession, gotWindow = session, window
		return nil
	}
	m.switchToSessionFn = func(name string) error {
		t.Errorf(
			"switching to the bare session lands on its active window, not the plain one; got %q",
			name,
		)
		return nil
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = updated.(Model)
	if cmd == nil {
		t.Fatal("expected a command from enter on a repo-session row")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("expected tea.QuitMsg on a successful switch")
	}
	if gotSession != "repo-a-tien" || gotWindow != "zsh" {
		t.Errorf("expected a switch to repo-a-tien:zsh, got %q:%q", gotSession, gotWindow)
	}
}

func TestEnterOnRepoSessionRowOutsideTmuxShowsGuardMessage(t *testing.T) {
	t.Setenv("TMUX", "")
	m := makeTestModel(testStatuses())
	m.repoSessions = []worktree.RepoSessionStatus{{Repo: "repo-a", Name: "repo-a-tien"}}
	m.rebuildRows()
	m = focusRepoSessionRow(t, m, "repo-a-tien")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := updated.(Model)

	if cmd != nil {
		t.Error("expected no command outside tmux")
	}
	if !strings.Contains(m2.status, "not inside tmux") {
		t.Errorf("expected the not-inside-tmux guard, got %q", m2.status)
	}
}

// --- session-level keys on a repo's own session row ---

func repoSessionModel(t *testing.T) Model {
	t.Helper()
	m := makeTestModel(repoWithSession("repo-a", "repo-a-tien"))
	// %1 is the worktree window's pane (see repoWithSession); %2 is the
	// session's one plain window - the only thing the row stands for.
	m.repoSessions = []worktree.RepoSessionStatus{{
		Repo:  "repo-a",
		Name:  "repo-a-tien",
		Panes: []tmux.PaneState{{Session: "repo-a-tien", Window: "2.1.282", PaneID: "%2"}},
	}}
	m.rebuildRows()
	return focusRepoSessionRow(t, m, "repo-a-tien")
}

// d d on a repo's session row must never close the repo's worktree windows:
// they share the session but belong to their own rows. So it closes only the
// row's plain windows and never kills the session itself.
func TestKillOnRepoSessionRowKeepsWorktreeWindows(t *testing.T) {
	m := repoSessionModel(t)
	m.killSessionFn = func(name string) error {
		t.Errorf("killSessionFn(%q) called: that would close the worktree windows too", name)
		return nil
	}
	var killedPanes []string
	m.killPaneFn = func(id string) error {
		killedPanes = append(killedPanes, id)
		return nil
	}

	mi, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	m = mi.(Model)
	if m.pendingKillSession != "repo-a-tien" {
		t.Fatalf("first d should arm the repo's session, got %q", m.pendingKillSession)
	}
	if hint := m.renderHint(200); !strings.Contains(hint, "worktree windows stay") {
		t.Errorf("armed hint should say the worktree windows stay, got %q", hint)
	}

	mi, cmd := m.Update(tea.KeyPressMsg{Code: 'd'})
	m = mi.(Model)
	if cmd == nil {
		t.Fatal("second d should return the close command")
	}
	msg := cmd()
	if len(killedPanes) != 1 || killedPanes[0] != "%2" {
		t.Errorf("expected only the plain pane %%2 closed, got %v", killedPanes)
	}

	mi, _ = m.Update(msg)
	m = mi.(Model)
	for _, r := range m.rows {
		if r.kind == rowRepoSession {
			t.Errorf("expected the session row gone once its plain windows are, got %+v", r)
		}
	}
	found := false
	for _, r := range m.rows {
		if r.kind == rowWorktree && r.status.Name == "feature-a" {
			found = true
		}
	}
	if !found {
		t.Error("expected the worktree row to stay")
	}
}

// h on a repo's session row folds the repo group, like it does on a
// worktree row in the same group.
func TestHOnRepoSessionRowCollapsesRepo(t *testing.T) {
	m := repoSessionModel(t)

	mi, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m = mi.(Model)
	if !m.collapsed[repoKey("repo-a")] {
		t.Fatal("expected h on the repo's session row to collapse repo-a")
	}
	if got := m.rows[m.cursor]; got.kind != rowRepo || got.repo != "repo-a" {
		t.Errorf("expected cursor on repo-a's header, got %+v", got)
	}
}
