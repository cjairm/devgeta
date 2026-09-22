package tuiworktree

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	"github.com/cjairm/devgeta/pkg/paths"
)

// --- selectedSession ---

func TestSelectedSessionOnSessionRow(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	m.rebuildRows()
	for i, r := range m.rows {
		if r.kind == rowSession && r.session.Name == "notes" {
			m.cursor = i
			break
		}
	}

	sel, ok := m.selectedSession()
	if !ok {
		t.Fatal("expected selectedSession to return ok=true on a rowSession row")
	}
	if sel.Name != "notes" {
		t.Errorf("expected session 'notes', got %q", sel.Name)
	}
}

func TestSelectedSessionOnWorktreeRowIsFalse(t *testing.T) {
	m := makeTestModel(testStatuses()) // cursor starts on repo-a/feature-a, a rowWorktree
	if _, ok := m.selectedSession(); ok {
		t.Error("expected selectedSession to return ok=false on a rowWorktree row")
	}
}

// --- enter on a session row (switch) ---

func TestEnterOnSessionRowSwitchesAndQuits(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	m.rebuildRows()
	for i, r := range m.rows {
		if r.kind == rowSession && r.session.Name == "notes" {
			m.cursor = i
			break
		}
	}
	var switchedTo string
	m.switchToSessionFn = func(name string) error {
		switchedTo = name
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = m2.(Model)
	if cmd == nil {
		t.Fatal("expected a command from enter on a session row")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg on successful switch, got %T: %+v", msg, msg)
	}
	if switchedTo != "notes" {
		t.Errorf("expected switchToSessionFn called with 'notes', got %q", switchedTo)
	}
}

func TestEnterOnSessionRowSwitchFailureShowsStatusAndDoesNotQuit(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	m.rebuildRows()
	for i, r := range m.rows {
		if r.kind == rowSession && r.session.Name == "notes" {
			m.cursor = i
			break
		}
	}
	m.switchToSessionFn = func(_ string) error {
		return fmt.Errorf("no such session")
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = m2.(Model)
	if cmd == nil {
		t.Fatal("expected a command from enter on a session row")
	}
	msg := cmd()
	m3, _ := m2.(Model).Update(msg)
	m4 := m3.(Model)
	if _, ok := msg.(tea.QuitMsg); ok {
		t.Error("a failed switch must not quit the TUI")
	}
	if !strings.Contains(m4.status, "switch failed") {
		t.Errorf("expected a 'switch failed' status, got %q", m4.status)
	}
}

// --- enter on a repo header row (switch to the repo's own session) ---
//
// The header is the only row standing for the session that holds a repo's
// worktree WINDOWS: ADR-0003 excludes any session containing a wt- window from
// the session rows, so the plain windows alongside them are otherwise
// unreachable. Before this, enter here fell through to handleAttach, which has
// no worktree selected on a header row and returned in silence.

// focusRepoHeader puts the cursor on repo's header row.
func focusRepoHeader(t *testing.T, m Model, repo string) Model {
	t.Helper()
	if _, ok := m.focusRow(func(r row) bool { return r.kind == rowRepo && r.repo == repo }); !ok {
		t.Fatalf("test setup: no header row for repo %q", repo)
	}
	return m
}

// repoWithSession returns one worktree status for repo whose live pane reports
// belonging to sessionName — the shape the dashboard sees for any repo with a
// live worktree window.
func repoWithSession(repo, sessionName string) []worktree.WorktreeStatus {
	return []worktree.WorktreeStatus{{
		Name:       "feature-a",
		Repo:       repo,
		Path:       "/tmp/a",
		TmuxWindow: worktree.GetWindowName(repo, "feature-a"),
		Panes: []tmux.PaneState{
			{Session: sessionName, Window: worktree.GetWindowName(repo, "feature-a"), PaneID: "%1"},
		},
	}}
}

// headerIndex returns the row index of repo's header row.
func headerIndex(t *testing.T, m Model, repo string) int {
	t.Helper()
	for i, r := range m.rows {
		if r.kind == rowRepo && r.repo == repo {
			return i
		}
	}
	t.Fatalf("test setup: no header row for repo %q", repo)
	return -1
}

// --- which repo headers j/k stops on ---
//
// A header is worth stopping on only when switching to its session would land
// somewhere the repo's own child rows do not already reach. When the session
// holds nothing but worktree windows, stopping there is a keypress that buys
// nothing and lengthens every trip down the list.

func TestExpandedRepoHeaderIsSkippedWhenItsSessionIsOnlyWorktreeWindows(t *testing.T) {
	m := makeTestModel(repoWithSession("repo-a", "repo-a"))
	m.plainWindowBySession = map[string]string{} // no plain window anywhere

	if slices.Contains(m.navigableIndices(), headerIndex(t, m, "repo-a")) {
		t.Error(
			"a header whose session holds only the worktree windows its child rows " +
				"already reach must not be a j/k stop",
		)
	}
}

func TestExpandedRepoHeaderIsNavigableWhenItsSessionHasAPlainWindow(t *testing.T) {
	m := makeTestModel(repoWithSession("repo-a", "repo-a"))
	m.plainWindowBySession = map[string]string{"repo-a": "zsh"}

	if !slices.Contains(m.navigableIndices(), headerIndex(t, m, "repo-a")) {
		t.Error(
			"the session's plain window is reachable from no other row, so its header " +
				"must be a j/k stop",
		)
	}
}

// The header's session is read off the panes, so it is the session's REAL name
// that decides, not TmuxSessionName(repo) — a `wt-hire2-…` window living in a
// session called `hire2-tien` is a real case.
func TestExpandedRepoHeaderUsesItsPanesSessionNameNotTheDerivedOne(t *testing.T) {
	m := makeTestModel(repoWithSession("hire2", "hire2-tien"))
	m.plainWindowBySession = map[string]string{"hire2-tien": "node"}

	if !slices.Contains(m.navigableIndices(), headerIndex(t, m, "hire2")) {
		t.Error("the plain window belongs to hire2-tien, the session the panes actually name")
	}
}

// Unchanged from before the header gained an action: a collapsed header has to
// stay reachable or l can never re-expand it.
func TestCollapsedRepoHeaderStaysNavigableWithNoPlainWindow(t *testing.T) {
	m := makeTestModel(repoWithSession("repo-a", "repo-a"))
	m.plainWindowBySession = map[string]string{}
	m.collapsed["repo-a"] = true
	m.rebuildRows()

	if !slices.Contains(m.navigableIndices(), headerIndex(t, m, "repo-a")) {
		t.Error("a collapsed header must stay reachable so l can re-expand it")
	}
}

// A repo with no live window has no session to read, so there is nothing to
// switch to and nothing to stop on.
func TestExpandedRepoHeaderIsSkippedWhenNoWorktreeWindowIsLive(t *testing.T) {
	m := makeTestModel(testStatuses()) // no Panes on any status
	m.plainWindowBySession = map[string]string{"repo-a": "zsh"}

	if slices.Contains(m.navigableIndices(), headerIndex(t, m, "repo-a")) {
		t.Error("with no live pane there is no session to resolve, so the header is not a stop")
	}
}

// The startup load has to answer this too, not just the 3-second tick. It
// already takes a full tmux scan of its own and was throwing the plain-window
// half away, so for the first tick's worth of time no header was a stop — the
// same header became selectable a few seconds after the dashboard opened,
// which is indistinguishable from it being broken.
func TestInitialSessionLoadCarriesWhichSessionsHavePlainWindows(t *testing.T) {
	window := worktree.GetWindowName("repo-a", "feature-a")
	mockTmuxBase := commands.NewMockBaseCommand()
	mockTmuxBase.SetExecCommandResults(
		commands.ExecCommandResult("repo-a\t0", "", nil), // list-sessions
		// list-panes: the worktree window plus a plain zsh. No trailing tab on
		// the last line — that is what the real executor's TrimSpace leaves.
		commands.ExecCommandResult(
			"repo-a\t"+window+"\t%1\t0\tzsh\t\nrepo-a\tzsh\t%2\t0\tzsh",
			"",
			nil,
		),
	)
	m := makeTestModel(repoWithSession("repo-a", "repo-a"))
	m.mgr = &worktree.WorktreeManager{
		Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: commands.NewMockBaseCommand()},
		Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
		Base: commands.NewMockBaseCommand(),
	}
	m.plainWindowBySession = nil // as it is on the very first frame

	updated, _ := m.Update(m.sessionsLoadCmd(m.sessionGen)())
	m = updated.(Model)

	if m.plainWindowBySession["repo-a"] != "zsh" {
		t.Error(
			"the startup session load must report repo-a's plain zsh window; otherwise its " +
				"header only becomes selectable once the first 3-second tick lands",
		)
	}
	// No VerifyNoRealCommands here: it asserts the base was never touched at
	// all, and this test's whole point is to drive the scan through it. Nothing
	// real can run — the manager's Tmux is a MockBaseCommand by construction.
	if got := mockTmuxBase.GetExecCommandCallCount(); got != 2 {
		t.Errorf("expected exactly the two mocked scan commands, got %d", got)
	}
}

// End to end for the dashboard's own window: `ctrl+t` opens dg ws as a
// "[workspace]" window in the session you pressed it from, so a repo session
// holding nothing but worktree windows must not become selectable just because
// you opened the dashboard from inside it.
func TestTheDashboardsOwnWindowDoesNotMakeItsSessionSelectable(t *testing.T) {
	t.Setenv("TMUX_PANE", "%9")
	window := worktree.GetWindowName("repo-a", "feature-a")
	wtPane := tmux.PaneState{Session: "repo-a", Window: window, PaneID: "%1"}
	layer := worktree.StateLayer{
		PanesByWindow: map[string][]tmux.PaneState{window: {wtPane}},
		PanesBySession: map[string][]tmux.PaneState{
			"repo-a": {wtPane, {Session: "repo-a", Window: "[workspace]", PaneID: "%9"}},
		},
	}
	m := makeTestModel(repoWithSession("repo-a", "repo-a"))

	updated, _ := m.Update(tmuxStateMsg{layer: layer, gen: m.sessionGen})
	m = updated.(Model)

	if slices.Contains(m.navigableIndices(), headerIndex(t, m, "repo-a")) {
		t.Error(
			"the only non-worktree window in repo-a's session is the dashboard itself, " +
				"so its header leads nowhere and must not be a stop",
		)
	}
}

// --- a selected header has to STAY selected ---
//
// Every rebuild re-clamps the cursor, and a rebuild happens on every 3-second
// tmux tick, every filter keystroke and every collapse. Clamping against a
// different set than j/k moves over is what made a selected header feel
// haunted: you land on it, a tick fires, and the cursor is on the worktree
// below it — sometimes before you even look.

// fastTickLayer is a scan in which repo's worktree window is live in
// sessionName and that session also holds a plain zsh window, i.e. one where
// repo's header stays navigable.
func fastTickLayer(repo, sessionName string) worktree.StateLayer {
	window := worktree.GetWindowName(repo, "feature-a")
	wtPane := tmux.PaneState{Session: sessionName, Window: window, PaneID: "%1"}
	return worktree.StateLayer{
		PanesByWindow: map[string][]tmux.PaneState{window: {wtPane}},
		PanesBySession: map[string][]tmux.PaneState{
			sessionName: {wtPane, {Session: sessionName, Window: "zsh", PaneID: "%2"}},
		},
	}
}

func TestTmuxTickDoesNotKnockTheCursorOffASelectedRepoHeader(t *testing.T) {
	m := makeTestModel(repoWithSession("repo-a", "repo-a"))
	m.plainWindowBySession = map[string]string{"repo-a": "zsh"}
	m = focusRepoHeader(t, m, "repo-a")

	updated, _ := m.Update(
		tmuxStateMsg{layer: fastTickLayer("repo-a", "repo-a"), gen: m.sessionGen},
	)
	m = updated.(Model)

	if got := m.rows[m.cursor]; got.kind != rowRepo || got.repo != "repo-a" {
		t.Errorf(
			"a tick that changed nothing must leave the cursor on the header; it moved to kind=%d repo=%q path=%q",
			got.kind,
			got.repo,
			got.status.Path,
		)
	}
}

// The same defect by its shortest path: a plain rebuild, which is what a filter
// keystroke and a collapse both do.
func TestRebuildKeepsTheCursorOnASelectedRepoHeader(t *testing.T) {
	m := makeTestModel(repoWithSession("repo-a", "repo-a"))
	m.plainWindowBySession = map[string]string{"repo-a": "zsh"}
	m = focusRepoHeader(t, m, "repo-a")
	before := m.cursor

	m.rebuildRows()

	if m.cursor != before {
		t.Errorf(
			"rebuildRows clamped a navigable header away: cursor %d -> %d (kind=%d)",
			before, m.cursor, m.rows[m.cursor].kind,
		)
	}
}

// The mirror of the two above: a header that is NOT navigable must still be
// clamped away, or the cursor sits on a row j/k cannot return to.
func TestRebuildClampsTheCursorOffANonNavigableRepoHeader(t *testing.T) {
	m := makeTestModel(repoWithSession("repo-a", "repo-a"))
	m.plainWindowBySession = map[string]string{} // header not navigable
	m.cursor = headerIndex(t, m, "repo-a")

	m.rebuildRows()

	if m.rows[m.cursor].kind == rowRepo {
		t.Error("a header j/k cannot reach must not keep the cursor either")
	}
}

// The set has to come off the same scan everything else does, or it goes stale
// the moment a plain window is opened or closed.
func TestTmuxScanRefreshesWhichSessionsHavePlainWindows(t *testing.T) {
	m := makeTestModel(repoWithSession("repo-a", "repo-a"))
	layer := worktree.StateLayer{
		PanesBySession: map[string][]tmux.PaneState{
			"repo-a": {
				{Session: "repo-a", Window: worktree.GetWindowName("repo-a", "feature-a")},
				{Session: "repo-a", Window: "zsh"},
			},
		},
	}

	updated, _ := m.Update(tmuxStateMsg{layer: layer, gen: m.sessionGen})
	m = updated.(Model)

	if m.plainWindowBySession["repo-a"] != "zsh" {
		t.Error("a scan showing a plain zsh window in repo-a's session must be picked up")
	}
}

// enter on a header has to land on the session's PLAIN window by name, not on
// the session and whatever window happens to be active in it.
//
// Switching to the session alone put the user right back on a worktree window:
// the dashboard runs as a [workspace] window INSIDE that session, so it is the
// active window at the moment of the switch, and when the dashboard then quits
// its window dies and tmux drops the client onto whatever is left — typically
// the wt- window the header was supposed to be an alternative to.
func TestEnterOnRepoHeaderLandsOnThePlainWindowNotJustTheSession(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := makeTestModel(repoWithSession("repo-a", "repo-a"))
	m.plainWindowBySession = map[string]string{"repo-a": "zsh"}
	m = focusRepoHeader(t, m, "repo-a")
	m.switchToSessionFn = func(name string) error {
		t.Errorf("switching to the session alone lands on its active window; got %q", name)
		return nil
	}
	var gotSession, gotWindow string
	m.attachFn = func(session, window string) error {
		gotSession, gotWindow = session, window
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = m2.(Model)
	if cmd == nil {
		t.Fatal("expected a command from enter on a repo header row")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("expected tea.QuitMsg on a successful switch")
	}
	if gotSession != "repo-a" || gotWindow != "zsh" {
		t.Errorf(
			"expected a switch to repo-a:zsh, the window that made the header a stop; got %q:%q",
			gotSession, gotWindow,
		)
	}
}

// The session a repo's worktree windows actually live in is READ off those
// windows' panes, never assumed to be TmuxSessionName(repo). A `wt-hire2-…`
// window sitting in a session called `hire2-tien` is a real case, and deriving
// the name would switch to a session that does not exist.
func TestEnterOnRepoHeaderSwitchesToTheSessionItsPanesReport(t *testing.T) {
	t.Setenv("TMUX", "1")
	statuses := []worktree.WorktreeStatus{{
		Name:       "feature-a",
		Repo:       "repo-a",
		Path:       "/tmp/a",
		TmuxWindow: worktree.GetWindowName("repo-a", "feature-a"),
		Panes: []tmux.PaneState{
			{Session: "repo-a-nicknamed", PaneID: "%1", PaneIndex: "0"},
		},
	}}
	m := focusRepoHeader(t, makeTestModel(statuses), "repo-a")
	m.hasSessionFn = func(_ string) bool {
		t.Error("a live pane already names the session; has-session must not be consulted")
		return false
	}
	var switchedTo string
	m.switchToSessionFn = func(name string) error {
		switchedTo = name
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = m2.(Model)
	if cmd == nil {
		t.Fatal("expected a command from enter on a repo header row")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg on successful switch, got %T: %+v", msg, msg)
	}
	if switchedTo != "repo-a-nicknamed" {
		t.Errorf("expected the pane's own session %q, got %q", "repo-a-nicknamed", switchedTo)
	}
}

// With no live window to read a session off, the name ensureWindow would have
// used is worth one has-session check.
func TestEnterOnRepoHeaderFallsBackToTheDerivedNameWhenNoWindowIsLive(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := focusRepoHeader(t, makeTestModel(testStatuses()), "repo-a") // no Panes on any status
	var asked string
	m.hasSessionFn = func(name string) bool {
		asked = name
		return true
	}
	var switchedTo string
	m.switchToSessionFn = func(name string) error {
		switchedTo = name
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = m2.(Model)
	if cmd == nil {
		t.Fatal("expected a command from enter on a repo header row")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("expected tea.QuitMsg on a successful switch")
	}
	want := worktree.TmuxSessionName("repo-a")
	if asked != want {
		t.Errorf("expected has-session asked about %q, got %q", want, asked)
	}
	if switchedTo != want {
		t.Errorf("expected switchToSessionFn called with %q, got %q", want, switchedTo)
	}
}

// No live window and no session by the derived name either: say so rather than
// letting switch-client fail with tmux's own wording.
func TestEnterOnRepoHeaderWithNoSessionExplainsInsteadOfSwitching(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := focusRepoHeader(t, makeTestModel(testStatuses()), "repo-a")
	m.hasSessionFn = func(_ string) bool { return false }
	switchCalled := false
	m.switchToSessionFn = func(_ string) error {
		switchCalled = true
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if cmd != nil {
		t.Error("enter on a repo header with no session should return no command")
	}
	if switchCalled {
		t.Error("switchToSessionFn must not be called when the session does not exist")
	}
	if !strings.Contains(m3.status, "repo-a") {
		t.Errorf("expected a status naming the repo, got %q", m3.status)
	}
}

func TestEnterOnRepoHeaderOutsideTmuxShowsGuardMessage(t *testing.T) {
	t.Setenv("TMUX", "")
	m := focusRepoHeader(t, makeTestModel(testStatuses()), "repo-a")
	switchCalled := false
	m.switchToSessionFn = func(_ string) error {
		switchCalled = true
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if cmd != nil {
		t.Error("enter outside tmux on a repo header row should return no command")
	}
	if switchCalled {
		t.Error("switchToSessionFn must not be called outside tmux")
	}
	if !strings.Contains(m3.status, "not inside tmux") {
		t.Errorf(
			"expected the same not-inside-tmux guard message as handleAttach, got %q",
			m3.status,
		)
	}
}

func TestEnterOnSessionRowOutsideTmuxShowsGuardMessage(t *testing.T) {
	t.Setenv("TMUX", "")
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	m.rebuildRows()
	for i, r := range m.rows {
		if r.kind == rowSession && r.session.Name == "notes" {
			m.cursor = i
			break
		}
	}
	switchCalled := false
	m.switchToSessionFn = func(_ string) error {
		switchCalled = true
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if cmd != nil {
		t.Error("enter outside tmux on a session row should return no command")
	}
	if switchCalled {
		t.Error("switchToSessionFn must not be called outside tmux")
	}
	if !strings.Contains(m3.status, "not inside tmux") {
		t.Errorf(
			"expected the same not-inside-tmux guard message as handleAttach, got %q",
			m3.status,
		)
	}
}

// --- d on a session row (kill, two-press) ---

func cursorToSession(m *Model, name string) {
	m.rebuildRows()
	for i, r := range m.rows {
		if r.kind == rowSession && r.session.Name == name {
			m.cursor = i
			return
		}
	}
}

func TestKillSessionDoubleConfirm(t *testing.T) {
	killCalled := false
	var killedName string
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	cursorToSession(&m, "notes")
	m.killSessionFn = func(name string) error {
		killCalled = true
		killedName = name
		return nil
	}

	// First d: arm
	m2, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	m3 := m2.(Model)
	if killCalled {
		t.Error("first d should not kill")
	}
	if m3.pendingKillSession != "notes" {
		t.Errorf("first d should arm pendingKillSession, got %q", m3.pendingKillSession)
	}

	// Non-d key clears arm
	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'j'})
	m5 := m4.(Model)
	if m5.pendingKillSession != "" {
		t.Error("j should clear pendingKillSession")
	}

	// Second d on same row kills
	cursorToSession(&m, "notes")
	m6, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	m7 := m6.(Model)
	m8, cmd := m7.Update(tea.KeyPressMsg{Code: 'd'})
	_ = m8
	if cmd == nil {
		t.Fatal("second d should return the kill command")
	}
	msg := cmd()
	if !killCalled {
		t.Error("second d should call killSessionFn")
	}
	if killedName != "notes" {
		t.Errorf("expected killSessionFn called with 'notes', got %q", killedName)
	}
	skm, ok := msg.(sessionKilledMsg)
	if !ok {
		t.Fatalf("expected sessionKilledMsg on successful kill, got %T: %+v", msg, msg)
	}
	if skm.name != "notes" {
		t.Errorf("expected sessionKilledMsg.name 'notes', got %q", skm.name)
	}
}

func TestKillSessionSuccessDropsRow(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	cursorToSession(&m, "notes")

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	m3 := m2.(Model)
	m4, cmd := m3.Update(tea.KeyPressMsg{Code: 'd'})
	if cmd == nil {
		t.Fatal("expected a command after second d")
	}
	msg := cmd()
	m5, _ := m4.(Model).Update(msg)
	m6 := m5.(Model)

	if hasRow(m6.rows, rowSession, "notes") {
		t.Error("expected the killed session's row to be dropped")
	}
	if !strings.Contains(m6.status, "removed: notes") {
		t.Errorf("expected a 'removed: notes' status, got %q", m6.status)
	}
	for _, s := range m6.sessions {
		if s.Name == "notes" {
			t.Error("expected m.sessions to no longer contain 'notes'")
		}
	}
}

func TestKillSessionErrorPropagationRowStays(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	cursorToSession(&m, "notes")
	m.killSessionFn = func(_ string) error {
		return fmt.Errorf("no such session")
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	m3 := m2.(Model)
	m4, cmd := m3.Update(tea.KeyPressMsg{Code: 'd'})
	if cmd == nil {
		t.Fatal("expected a command after second d")
	}
	msg := cmd()
	m5, _ := m4.(Model).Update(msg)
	m6 := m5.(Model)

	if !strings.Contains(m6.status, "kill session failed") {
		t.Errorf("expected a 'kill session failed' status, got %q", m6.status)
	}
	if !hasRow(m6.rows, rowSession, "notes") {
		t.Error("expected the session's row to stay after a failed kill")
	}
	if m6.pendingKillSession != "" {
		t.Error("pendingKillSession should be cleared after the second press regardless of outcome")
	}
}

// --- D / r are no-ops on a session row ---

func TestSessionDeleteAndRepairAreNoopsOnSessionRow(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	cursorToSession(&m, "notes")
	removeSessionCalled := false
	repairCalled := false
	m.removeSessionFn = func(_, _ string) error {
		removeSessionCalled = true
		return nil
	}
	m.repairFn = func(_, _ string, _ worktree.Layout) error {
		repairCalled = true
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: 'D'})
	m3 := m2.(Model)
	if cmd != nil {
		t.Error("D on a session row should return no command")
	}
	if removeSessionCalled {
		t.Error("D on a session row must not call removeSessionFn")
	}
	if m3.pendingSessionDelete != "" {
		t.Error("D on a session row must not arm pendingSessionDelete")
	}

	m4, cmd2 := m3.Update(tea.KeyPressMsg{Code: 'r'})
	m5 := m4.(Model)
	if cmd2 != nil {
		t.Error("r on a session row should return no command")
	}
	if repairCalled {
		t.Error("r on a session row must not call repairFn")
	}
	_ = m5
}

// --- d on a worktree row still uses handleDelete (no crosstalk with sessions) ---

func TestDeleteOnWorktreeRowDoesNotArmKillSession(t *testing.T) {
	m := makeTestModel(testStatuses()) // cursor on repo-a/feature-a, a rowWorktree
	m.sessions = testSessions()
	m.rebuildRows()

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	m3 := m2.(Model)
	if m3.pendingKillSession != "" {
		t.Error("d on a worktree row must not arm pendingKillSession")
	}
	if m3.pendingDelete == "" {
		t.Error(
			"d on a worktree row should still arm pendingDelete (existing worktree-delete flow)",
		)
	}
}

// --- s: new session (folder pick → name → create) ---

// atSessionNameStep drives a fresh model through s → pick "root" so tests that
// only care about the name step start from a resolved home workdir, the same
// shortcut the old tests took by setting creatingSession directly (before the
// folder-pick step existed).
func atSessionNameStep(t *testing.T, m Model) Model {
	t.Helper()
	m2, _ := m.Update(tea.KeyPressMsg{Code: 's'})
	m3, _ := m2.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // select pinned "root"
	m4 := m3.(Model)
	if m4.sessionMode != sessionNameInput {
		t.Fatalf(
			"expected to reach sessionNameInput after picking root, got mode %d",
			m4.sessionMode,
		)
	}
	if m4.sessionWorkdir != paths.Paths.Home.Root {
		t.Fatalf("expected root pick to resolve workdir to home %q, got %q",
			paths.Paths.Home.Root, m4.sessionWorkdir)
	}
	return m4
}

func TestNewSessionOpensFolderPicker(t *testing.T) {
	m := makeTestModel(testStatuses())
	m2, _ := m.Update(tea.KeyPressMsg{Code: 's'})
	m3 := m2.(Model)
	if m3.sessionMode != sessionFolderPick {
		t.Fatal("s should enter the folder-pick mode")
	}
	if m3.sessionFolderPicker == nil {
		t.Fatal("s should build the folder picker")
	}
}

func TestNewSessionRootPickResolvesToHome(t *testing.T) {
	m := makeTestModel(testStatuses())
	m2, _ := m.Update(tea.KeyPressMsg{Code: 's'})
	// The pinned first item is "root"; enter selects it.
	m3, _ := m2.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m4 := m3.(Model)
	if m4.sessionMode != sessionNameInput {
		t.Fatalf("expected sessionNameInput after selecting root, got mode %d", m4.sessionMode)
	}
	if m4.sessionWorkdir != paths.Paths.Home.Root {
		t.Errorf("expected workdir home %q, got %q", paths.Paths.Home.Root, m4.sessionWorkdir)
	}
}

func TestNewSessionFolderPickSelectsCandidate(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repoCandidatesFn = func(_ string) ([]string, error) {
		return []string{"/tmp/project"}, nil
	}
	var validated string
	m.validateSessionDirFn = func(path string) (string, error) {
		validated = path
		return path, nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 's'})
	// Move cursor off the pinned "root" onto the candidate, then select.
	m3, _ := m2.(Model).Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m4, _ := m3.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m5 := m4.(Model)

	if validated != "/tmp/project" {
		t.Errorf("expected validateSessionDirFn called with the candidate, got %q", validated)
	}
	if m5.sessionWorkdir != "/tmp/project" {
		t.Errorf("expected workdir to be the validated candidate, got %q", m5.sessionWorkdir)
	}
	if m5.sessionMode != sessionNameInput {
		t.Errorf("expected to advance to sessionNameInput, got mode %d", m5.sessionMode)
	}
}

func TestNewSessionFolderPickFreeTypedPath(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repoCandidatesFn = func(_ string) ([]string, error) { return nil, nil }
	var validated string
	m.validateSessionDirFn = func(path string) (string, error) {
		validated = path
		return path + "/resolved", nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 's'})
	m = m2.(Model)
	for _, ch := range "/tmp/typed" {
		mm, _ := m.Update(tea.KeyPressMsg{Code: ch})
		m = mm.(Model)
	}
	m3, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m4 := m3.(Model)

	if validated != "/tmp/typed" {
		t.Errorf("expected validateSessionDirFn called with the typed query, got %q", validated)
	}
	if m4.sessionWorkdir != "/tmp/typed/resolved" {
		t.Errorf("expected workdir to be the resolved path, got %q", m4.sessionWorkdir)
	}
}

func TestNewSessionFolderPickInvalidPathStaysInPicker(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repoCandidatesFn = func(_ string) ([]string, error) { return nil, nil }
	m.validateSessionDirFn = func(path string) (string, error) {
		return "", fmt.Errorf("path does not exist: %s", path)
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 's'})
	m = m2.(Model)
	for _, ch := range "/nope" {
		mm, _ := m.Update(tea.KeyPressMsg{Code: ch})
		m = mm.(Model)
	}
	m3, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m4 := m3.(Model)

	if m4.sessionMode != sessionFolderPick {
		t.Errorf("an invalid path should keep the folder picker open, got mode %d", m4.sessionMode)
	}
	if !strings.Contains(m4.status, "does not exist") {
		t.Errorf("expected a validation error status, got %q", m4.status)
	}
}

func TestNewSessionFolderPickEscCancels(t *testing.T) {
	m := makeTestModel(testStatuses())
	m2, _ := m.Update(tea.KeyPressMsg{Code: 's'})
	m3, _ := m2.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m4 := m3.(Model)
	if m4.sessionMode != sessionNone {
		t.Error("esc in the folder picker should exit the session flow")
	}
	if m4.sessionFolderPicker != nil {
		t.Error("esc should drop the folder picker")
	}
}

func TestNewSessionTypingAccumulates(t *testing.T) {
	m := atSessionNameStep(t, makeTestModel(testStatuses()))
	for _, ch := range "scratch" {
		m2, _ := m.Update(tea.KeyPressMsg{Code: ch})
		m = m2.(Model)
	}
	if m.sessionNameInput.Value != "scratch" {
		t.Errorf("expected sessionNameInput 'scratch', got %q", m.sessionNameInput.Value)
	}
}

func TestNewSessionPasteInsertsInOneShot(t *testing.T) {
	m := atSessionNameStep(t, makeTestModel(testStatuses()))
	m2, _ := m.Update(tea.PasteMsg{Content: "pasted-session"})
	m3 := m2.(Model)
	if m3.sessionNameInput.Value != "pasted-session" {
		t.Errorf(
			"expected sessionNameInput %q, got %q",
			"pasted-session",
			m3.sessionNameInput.Value,
		)
	}
}

func TestNewSessionEscCancels(t *testing.T) {
	createCalled := false
	m := atSessionNameStep(t, makeTestModel(testStatuses()))
	m.sessionNameInput.SetValue("scratch")
	m.createSessionFn = func(_, _ string) error {
		createCalled = true
		return nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m3 := m2.(Model)
	if m3.sessionMode != sessionNone {
		t.Error("esc should exit the session-name-prompt mode")
	}
	if m3.sessionNameInput.Value != "" {
		t.Error("esc should clear the session name input")
	}
	if createCalled {
		t.Error("esc must not call createSessionFn")
	}
}

func TestNewSessionEnterEmptyAutoGeneratesName(t *testing.T) {
	t.Setenv("TMUX", "")
	m := atSessionNameStep(t, makeTestModel(testStatuses()))
	// atSessionNameStep picks the pinned "root" folder (home), so the label is
	// "home". home-goku is taken, so the collision check must pick a different
	// character.
	m.listSessionNamesFn = func() ([]string, error) {
		return []string{"home-goku"}, nil
	}
	var createdName string
	m.createSessionFn = func(name, _ string) error {
		createdName = name
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if m3.sessionMode != sessionNone {
		t.Error("enter with a blank name should still dispatch and close the prompt")
	}
	if cmd == nil {
		t.Fatal("expected a command to run the async create")
	}
	cmd()
	if !strings.HasPrefix(createdName, "home-") {
		t.Errorf(
			"expected an auto-generated 'home-*' name for the home folder, got %q",
			createdName,
		)
	}
	if createdName == "home-goku" {
		t.Error("auto-name must not collide with the existing 'home-goku' session")
	}
}

func TestNewSessionCreateAndSwitchInsideTmux(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := atSessionNameStep(t, makeTestModel(testStatuses()))
	m.sessionNameInput.SetValue("scratch")

	var createdName, createdWorkdir, switchedTo string
	m.createSessionFn = func(name, workdir string) error {
		createdName = name
		createdWorkdir = workdir
		return nil
	}
	m.switchToSessionFn = func(name string) error {
		switchedTo = name
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if m3.sessionMode != sessionNone {
		t.Error("enter with a name should close the session-name prompt")
	}
	if cmd == nil {
		t.Fatal("expected a command to run the async create")
	}
	msg := cmd()
	if createdName != "scratch" {
		t.Errorf("expected createSessionFn called with 'scratch', got %q", createdName)
	}
	if createdWorkdir != paths.Paths.Home.Root {
		t.Errorf(
			"expected workdir %q (home from root pick), got %q",
			paths.Paths.Home.Root,
			createdWorkdir,
		)
	}
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected tea.QuitMsg after create+switch inside tmux, got %T: %+v", msg, msg)
	}
	if switchedTo != "scratch" {
		t.Errorf("expected switchToSessionFn called with 'scratch', got %q", switchedTo)
	}
}

func TestNewSessionCreateDetachedOutsideTmuxReportsWithoutSwitching(t *testing.T) {
	t.Setenv("TMUX", "")
	m := atSessionNameStep(t, makeTestModel(testStatuses()))
	m.sessionNameInput.SetValue("scratch")

	createdName := ""
	switchCalled := false
	m.createSessionFn = func(name, _ string) error {
		createdName = name
		return nil
	}
	m.switchToSessionFn = func(_ string) error {
		switchCalled = true
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_ = m2.(Model)
	if cmd == nil {
		t.Fatal("expected a command to run the async create")
	}
	msg := cmd()
	if createdName != "scratch" {
		t.Errorf("expected createSessionFn called with 'scratch', got %q", createdName)
	}
	if switchCalled {
		t.Error("switchToSessionFn must not be called outside tmux")
	}
	scm, ok := msg.(sessionCreatedMsg)
	if !ok {
		t.Fatalf("expected sessionCreatedMsg outside tmux, got %T: %+v", msg, msg)
	}
	if scm.name != "scratch" {
		t.Errorf("expected sessionCreatedMsg.name 'scratch', got %q", scm.name)
	}

	m3, _ := m2.(Model).Update(msg)
	m4 := m3.(Model)
	if !strings.Contains(m4.status, "session created: scratch") {
		t.Errorf("expected a 'session created: scratch' status, got %q", m4.status)
	}
}

func TestNewSessionCreateDuplicateNameErrorShowsStatusAndClearsPrompt(t *testing.T) {
	t.Setenv("TMUX", "1")
	m := atSessionNameStep(t, makeTestModel(testStatuses()))
	m.sessionNameInput.SetValue("dup")

	switchCalled := false
	m.createSessionFn = func(_, _ string) error {
		return fmt.Errorf("duplicate session: dup")
	}
	m.switchToSessionFn = func(_ string) error {
		switchCalled = true
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if m3.sessionMode != sessionNone {
		t.Error(
			"prompt state should be cleared once the create is dispatched, even though it later fails",
		)
	}
	if cmd == nil {
		t.Fatal("expected a command to run the async create")
	}
	msg := cmd()
	if switchCalled {
		t.Error("switchToSessionFn must not be called when create fails")
	}
	if _, ok := msg.(tea.QuitMsg); ok {
		t.Error("a failed create must not quit the TUI")
	}

	m4, _ := m2.(Model).Update(msg)
	m5 := m4.(Model)
	if !strings.Contains(m5.status, "create session failed") {
		t.Errorf("expected a 'create session failed' status, got %q", m5.status)
	}
}
