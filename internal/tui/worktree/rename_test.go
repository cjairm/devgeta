package tuiworktree

// Tests for the `$` rename flow (ADR-0052): prompt prefilled with the
// current name, flattening, the duplicate pre-check and tmux's own
// rejection, and moving the fold/cursor keys to the new name.

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func TestPressDollarOnSessionRowOpensRenamePromptPrefilled(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = testSessions() // "scratch", "notes"
	m.rebuildRows()
	if _, ok := m.focusRow(func(r row) bool {
		return r.kind == rowSession && r.session.Name == "notes"
	}); !ok {
		t.Fatal("test setup: expected to find the notes session row")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: '$'})
	m = updated.(Model)

	if !m.renaming {
		t.Fatal("expected $ to open the rename prompt")
	}
	if m.renameOldName != "notes" {
		t.Errorf("expected renameOldName %q, got %q", "notes", m.renameOldName)
	}
	if m.renameInput.Value != "notes" {
		t.Errorf("expected the prompt prefilled with %q, got %q", "notes", m.renameInput.Value)
	}
}

func TestPressDollarOnRepoSessionRowOpensRenamePromptPrefilled(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repoSessions = []worktree.RepoSessionStatus{{Repo: "repo-a", Name: "repo-a-tien"}}
	m.rebuildRows()
	m = focusRepoSessionRow(t, m, "repo-a-tien")

	updated, _ := m.Update(tea.KeyPressMsg{Code: '$'})
	m = updated.(Model)

	if !m.renaming || m.renameOldName != "repo-a-tien" || m.renameInput.Value != "repo-a-tien" {
		t.Errorf(
			"expected the rename prompt open and prefilled with repo-a-tien, got renaming=%v old=%q value=%q",
			m.renaming,
			m.renameOldName,
			m.renameInput.Value,
		)
	}
}

func TestPressDollarOnWorktreeRowIsNoOp(t *testing.T) {
	m := makeTestModel(testStatuses()) // cursor on a worktree row

	updated, _ := m.Update(tea.KeyPressMsg{Code: '$'})
	m = updated.(Model)

	if m.renaming {
		t.Error("expected $ on a worktree row to do nothing")
	}
}

func TestRenameEscCancels(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = testSessions()
	m.rebuildRows()
	m = focusRepoSessionRowOrSessionRow(t, m, "notes")
	updated, _ := m.Update(tea.KeyPressMsg{Code: '$'})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = updated.(Model)

	if m.renaming || m.renameInput.Value != "" {
		t.Errorf(
			"expected esc to cancel and clear the prompt, got renaming=%v value=%q",
			m.renaming,
			m.renameInput.Value,
		)
	}
}

// focusRepoSessionRowOrSessionRow is a small helper for tests that don't care
// which kind of session row they land on.
func focusRepoSessionRowOrSessionRow(t *testing.T, m Model, name string) Model {
	t.Helper()
	if _, ok := m.focusRow(func(r row) bool {
		return (r.kind == rowSession && r.session.Name == name) ||
			(r.kind == rowRepoSession && r.repoSession.Name == name)
	}); !ok {
		t.Fatalf("test setup: no session row named %q", name)
	}
	return m
}

func typeIntoRename(m Model, text string) Model {
	for _, r := range text {
		updated, _ := m.Update(tea.KeyPressMsg{Code: r})
		m = updated.(Model)
	}
	return m
}

func TestRenameEnterFlattensNameAndCallsRenameSessionFn(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = testSessions()
	m.rebuildRows()
	m = focusRepoSessionRowOrSessionRow(t, m, "notes")
	updated, _ := m.Update(tea.KeyPressMsg{Code: '$'})
	m = updated.(Model)

	// Clear the prefilled value and type a name containing a "."
	// (TmuxSessionName's flattening target - a literal space is a separate,
	// pre-existing gap in TextInput/handleKey's key routing, not something
	// this test is about).
	for range m.renameInput.Value {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = updated.(Model)
	}
	m = typeIntoRename(m, "new.name")

	var gotOld, gotNew string
	m.renameSessionFn = func(old, newName string) error {
		gotOld, gotNew = old, newName
		return nil
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if m.renaming {
		t.Error("expected enter to close the prompt")
	}
	if cmd == nil {
		t.Fatal("expected a command dispatching the rename")
	}
	msg := cmd()
	if _, ok := msg.(sessionRenamedMsg); !ok {
		if sm, ok := msg.(statusMsg); ok {
			t.Fatalf("expected a sessionRenamedMsg, got a statusMsg: %q", sm)
		}
		t.Fatalf("expected a sessionRenamedMsg, got %T", msg)
	}
	if gotOld != "notes" {
		t.Errorf("expected renameSessionFn's old name %q, got %q", "notes", gotOld)
	}
	if gotNew != worktree.TmuxSessionName("new.name") {
		t.Errorf(
			"expected the flattened name %q, got %q",
			worktree.TmuxSessionName("new.name"),
			gotNew,
		)
	}
}

func TestRenameBlankNameIsRejected(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = testSessions()
	m.rebuildRows()
	m = focusRepoSessionRowOrSessionRow(t, m, "notes")
	updated, _ := m.Update(tea.KeyPressMsg{Code: '$'})
	m = updated.(Model)
	for range m.renameInput.Value {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = updated.(Model)
	}
	renameCalled := false
	m.renameSessionFn = func(_, _ string) error {
		renameCalled = true
		return nil
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := updated.(Model)

	if cmd != nil {
		t.Error("expected no command for a blank name")
	}
	if renameCalled {
		t.Error("expected renameSessionFn not to be called for a blank name")
	}
	if !strings.Contains(m2.status, "blank") {
		t.Errorf("expected a status explaining the blank name, got %q", m2.status)
	}
}

func TestRenamePreChecksDuplicateAgainstLiveSessions(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = testSessions() // "scratch", "notes"
	m.rebuildRows()
	m = focusRepoSessionRowOrSessionRow(t, m, "notes")
	m.listSessionNamesFn = func() ([]string, error) { return []string{"scratch", "notes"}, nil }
	renameCalled := false
	m.renameSessionFn = func(_, _ string) error {
		renameCalled = true
		return nil
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: '$'})
	m = updated.(Model)
	for range m.renameInput.Value {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = updated.(Model)
	}
	m = typeIntoRename(m, "scratch")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := updated.(Model)

	if cmd != nil {
		t.Error("expected no command when the pre-check finds a duplicate")
	}
	if renameCalled {
		t.Error("expected renameSessionFn not to be called when the pre-check finds a duplicate")
	}
	if !strings.Contains(m2.status, "scratch") {
		t.Errorf("expected a status naming the duplicate, got %q", m2.status)
	}
}

func TestRenameSurfacesTmuxsOwnDuplicateRejection(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = testSessions()
	m.rebuildRows()
	m = focusRepoSessionRowOrSessionRow(t, m, "notes")
	// The pre-check is raced: the live list doesn't (yet) show the duplicate.
	m.listSessionNamesFn = func() ([]string, error) { return []string{"notes"}, nil }
	m.renameSessionFn = func(_, _ string) error {
		return errors.New("duplicate session: scratch")
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: '$'})
	m = updated.(Model)
	for range m.renameInput.Value {
		updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = updated.(Model)
	}
	m = typeIntoRename(m, "scratch")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected a command dispatching the rename")
	}
	msg := cmd()
	updated, _ = m.Update(msg)
	m2 := updated.(Model)
	if !strings.Contains(m2.status, "duplicate session: scratch") {
		t.Errorf("expected tmux's own rejection surfaced, got %q", m2.status)
	}
}

func TestSessionRenamedMsgMovesFoldAndCursorToNewName(t *testing.T) {
	m := makeTestModel(nil)
	m.sessions = testSessions() // "scratch", "notes"
	m.rebuildRows()
	// Fold "notes"'s pane rows (its collapse-map key is its rowKey, sess:notes).
	m.collapsed["sess:notes"] = true
	m = focusRepoSessionRowOrSessionRow(t, m, "notes")

	updated, _ := m.Update(sessionRenamedMsg{oldName: "notes", newName: "journal"})
	m = updated.(Model)

	if m.collapsed["sess:notes"] {
		t.Error("expected the old fold key to be gone")
	}
	if !m.collapsed["sess:journal"] {
		t.Error("expected the fold to move to the new name's key")
	}
	sel, ok := m.selectedSession()
	if !ok || sel.Name != "journal" {
		t.Errorf("expected the cursor to stay on the renamed row, got %+v (ok=%v)", sel, ok)
	}
	if !strings.Contains(m.status, "notes") || !strings.Contains(m.status, "journal") {
		t.Errorf("expected a status naming both the old and new name, got %q", m.status)
	}
}

func TestSessionRenamedMsgUpdatesRepoSessionRow(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repoSessions = []worktree.RepoSessionStatus{{Repo: "repo-a", Name: "repo-a-tien"}}
	m.rebuildRows()
	m = focusRepoSessionRow(t, m, "repo-a-tien")

	updated, _ := m.Update(sessionRenamedMsg{oldName: "repo-a-tien", newName: "repo-a-v2"})
	m = updated.(Model)

	found := false
	for _, r := range m.rows {
		if r.kind == rowRepoSession && r.repoSession.Name == "repo-a-v2" {
			found = true
		}
	}
	if !found {
		t.Error("expected the repo-session row to reflect the new name")
	}
	if m.rows[m.cursor].kind != rowRepoSession || m.rows[m.cursor].repoSession.Name != "repo-a-v2" {
		t.Errorf(
			"expected the cursor to land on the renamed repo-session row, got %+v",
			m.rows[m.cursor],
		)
	}
}
