package tuiworktree

// Tests for ADR-0052's enter/navigation behavior: a repo header is a label
// (never a switch target, never a j/k stop while expanded), and a window row
// is what enter switches to instead - that exact window, by its id.

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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

func focusWindowRow(t *testing.T, m Model, windowID string) Model {
	t.Helper()
	if _, ok := m.focusRow(func(r row) bool {
		return r.kind == rowWindow && r.window.WindowID == windowID
	}); !ok {
		t.Fatalf("test setup: no window row with id %q", windowID)
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
	m.repoWindows = []worktree.RepoWindowStatus{
		{Repo: "repo-a", Session: "repo-a", Window: "zsh", WindowID: "@2"},
	}
	m.rebuildRows()
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

// --- enter on a window row ---

func windowRowModel(t *testing.T) Model {
	t.Helper()
	m := makeTestModel(testStatuses())
	m.repoWindows = []worktree.RepoWindowStatus{
		{Repo: "repo-a", Session: "repo-a-tien", Window: "zsh", WindowID: "@4"},
		{Repo: "repo-a", Session: "repo-a-tien", Window: "zsh", WindowID: "@7"},
	}
	m.rebuildRows()
	return m
}

func TestEnterOnWindowRowSwitchesToThatWindowByID(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := focusWindowRow(t, windowRowModel(t), "@7")
	var gotSession, gotID string
	m.attachWindowIDFn = func(session, id string) error {
		gotSession, gotID = session, id
		return nil
	}
	m.attachFn = func(session, window string) error {
		t.Errorf("a name lookup cannot tell two zsh windows apart; got %q:%q", session, window)
		return nil
	}
	m.switchToSessionFn = func(name string) error {
		t.Errorf("switching to the bare session lands on its active window; got %q", name)
		return nil
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command from enter on a window row")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("expected tea.QuitMsg on a successful switch")
	}
	if gotSession != "repo-a-tien" || gotID != "@7" {
		t.Errorf("expected a switch to repo-a-tien @7 (the second zsh), got %q %q", gotSession, gotID)
	}
}

func TestEnterOnWindowRowReportsAFailedSwitch(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := focusWindowRow(t, windowRowModel(t), "@4")
	m.attachWindowIDFn = func(string, string) error { return errors.New("boom") }

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command")
	}
	msg, ok := cmd().(statusMsg)
	if !ok || !strings.Contains(string(msg), "switch failed: boom") {
		t.Errorf("expected a switch-failed status, got %#v", msg)
	}
}

func TestEnterOnWindowRowOutsideTmuxShowsGuardMessage(t *testing.T) {
	t.Setenv("TMUX", "")
	m := focusWindowRow(t, windowRowModel(t), "@4")
	m.attachWindowIDFn = func(string, string) error {
		t.Error("must not switch outside tmux")
		return nil
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := updated.(Model)

	if cmd != nil {
		t.Error("expected no command outside tmux")
	}
	if !strings.Contains(m2.status, "not inside tmux") {
		t.Errorf("expected the not-inside-tmux guard, got %q", m2.status)
	}
}

// --- d d closes the window ---

func TestDOnWindowRowArmsThenClosesThatWindowByID(t *testing.T) {
	m := focusWindowRow(t, windowRowModel(t), "@7")
	var closed []string
	m.killWindowIDFn = func(id string) error {
		closed = append(closed, id)
		return nil
	}
	m.killSessionFn = func(name string) error {
		t.Errorf("killSessionFn(%q): a window row must never kill the session", name)
		return nil
	}

	mi, cmd := m.Update(tea.KeyPressMsg{Code: 'd'})
	m = mi.(Model)
	if cmd != nil || len(closed) != 0 {
		t.Fatal("first d must only arm, not close anything")
	}
	if m.pendingKillWindow != "win:@7" {
		t.Fatalf("expected the second zsh (@7) armed, got %q", m.pendingKillWindow)
	}
	if hint := ansi.Strip(m.renderHint(200)); !strings.Contains(hint, "press d again to close zsh") {
		t.Errorf("armed hint should name the window, got %q", hint)
	}

	mi, cmd = m.Update(tea.KeyPressMsg{Code: 'd'})
	m = mi.(Model)
	if cmd == nil {
		t.Fatal("second d should return the close command")
	}
	msg := cmd()
	if len(closed) != 1 || closed[0] != "@7" {
		t.Errorf("expected only @7 closed, got %v", closed)
	}

	mi, _ = m.Update(msg)
	m = mi.(Model)
	var left []string
	for _, r := range m.rows {
		if r.kind == rowWindow {
			left = append(left, r.window.WindowID)
		}
	}
	if len(left) != 1 || left[0] != "@4" {
		t.Errorf("expected only the first zsh (@4) left, got %v", left)
	}
	if m.pendingKillWindow != "" {
		t.Errorf("expected the arming cleared, got %q", m.pendingKillWindow)
	}
}

func TestAnyOtherKeyCancelsAnArmedWindowClose(t *testing.T) {
	m := focusWindowRow(t, windowRowModel(t), "@4")
	m.killWindowIDFn = func(id string) error {
		t.Errorf("killWindowIDFn(%q) after a cancelled arm", id)
		return nil
	}

	mi, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	m = mi.(Model)
	mi, _ = m.Update(tea.KeyPressMsg{Code: 'j'})
	m = mi.(Model)
	if m.pendingKillWindow != "" {
		t.Fatalf("expected j to cancel, still armed on %q", m.pendingKillWindow)
	}
	// Back on a window row, the next d arms afresh instead of closing.
	m = focusWindowRow(t, m, "@4")
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'd'})
	if cmd != nil {
		t.Error("d after a cancel must arm again, not close")
	}
}

func TestClosingAWindowReportsAFailure(t *testing.T) {
	m := focusWindowRow(t, windowRowModel(t), "@4")
	m.killWindowIDFn = func(string) error { return errors.New("boom") }

	mi, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	_, cmd := mi.(Model).Update(tea.KeyPressMsg{Code: 'd'})
	msg, ok := cmd().(statusMsg)
	if !ok || !strings.Contains(string(msg), "close window failed: boom") {
		t.Errorf("expected a close-window-failed status, got %#v", msg)
	}
}

// $ renames a whole session; a window row is one window of it.
func TestDollarDoesNothingOnAWindowRow(t *testing.T) {
	m := focusWindowRow(t, windowRowModel(t), "@4")

	mi, cmd := m.Update(tea.KeyPressMsg{Code: '$'})
	if m2 := mi.(Model); cmd != nil || m2.renaming {
		t.Errorf("$ on a window row must do nothing, got cmd=%v renaming=%v", cmd != nil, m2.renaming)
	}
}

// h on a window row folds the repo group, like it does on a worktree row in
// the same group.
func TestHOnWindowRowCollapsesRepo(t *testing.T) {
	m := focusWindowRow(t, windowRowModel(t), "@4")

	mi, _ := m.Update(tea.KeyPressMsg{Code: 'h'})
	m = mi.(Model)
	if !m.collapsed[repoKey("repo-a")] {
		t.Fatal("expected h on a window row to collapse repo-a")
	}
	if got := m.rows[m.cursor]; got.kind != rowRepo || got.repo != "repo-a" {
		t.Errorf("expected cursor on repo-a's header, got %+v", got)
	}
}
