package tuiworktree

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
