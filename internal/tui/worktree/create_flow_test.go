package tuiworktree

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// newTestWorktreeManager builds a WorktreeManager with harmless, fully mocked
// Git/Tmux/Base fields for create-flow tests whose flow (via
// handleCreateSuccess's loadCmd) reaches mgr.List(): List() now unconditionally
// calls Tmux.PaneStates() and, via knownRepoAnchorGroups, Git.GetMainWorktree - a
// bare zero-value WorktreeManager panics on those nil fields now that List()
// asks git instead of just scanning directories (ADR-0010, the
// worktree-layout-support cycle's Step 4). These tests don't care about
// List()'s actual output, only that the create-flow's status/attach/quit
// logic runs safely afterward.
func newTestWorktreeManager() *worktree.WorktreeManager {
	mockGitBase := commands.NewMockBaseCommand()
	mockGitBase.SetExecCommandResult("", "fatal: not a git repository", os.ErrNotExist)
	mockTmuxBase := commands.NewMockBaseCommand()
	return &worktree.WorktreeManager{
		Git:  &git.Git{Cmd: commands.NewMockCommand(), Base: mockGitBase},
		Tmux: &tmux.Tmux{Cmd: commands.NewMockCommand(), Base: mockTmuxBase},
		Base: commands.NewMockBaseCommand(),
	}
}

// --- Create flow (n) ---

func TestNewWorktreePicksCursorRepoFirst(t *testing.T) {
	m := makeTestModel(testStatuses()) // cursor starts on repo-a/feature-a
	var gotSlug string
	m.repoCandidatesFn = func(cursorRepoSlug string) ([]string, error) {
		gotSlug = cursorRepoSlug
		return []string{"/repos/repo-a", "/repos/other"}, nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'n'})
	m3 := m2.(Model)

	if gotSlug != "repo-a" {
		t.Errorf("expected repoCandidatesFn called with cursor repo 'repo-a', got %q", gotSlug)
	}
	if m3.createMode != createRepoPick {
		t.Fatalf("n should enter repo-pick mode, got mode=%d", m3.createMode)
	}
	if m3.repoPicker == nil {
		t.Fatal("expected a repo picker to be built")
	}
	sel, ok := m3.repoPicker.Selected()
	if !ok || sel.Command != "/repos/repo-a" {
		t.Errorf("expected first candidate pre-selected, got %+v ok=%v", sel, ok)
	}
}

// TestNewWorktreeHintsTheCursorRowsRepo covers every row kind the cursor can
// be on when n is pressed. Session and pane rows are the ones that used to
// hint nothing: hovering a session offered no repo at all, so the picker's top
// candidate came from whatever else RepoCandidates found (in practice the
// directory `dg ws` was launched from) and moving the cursor appeared to have
// no effect at all.
func TestNewWorktreeHintsTheCursorRowsRepo(t *testing.T) {
	// Rows, in the order buildRows emits them: repo-a header, its two
	// worktrees, repo-b header, its worktree, then the standalone sessions.
	m := makeTestModel(testStatuses())
	m.sessions = testSessions()
	m.rebuildRows()

	for i, r := range m.rows {
		var want string
		switch r.kind {
		case rowRepo:
			want = r.repo
		case rowWorktree:
			want = r.status.Repo
		case rowSession:
			want = r.session.Name
		case rowPane:
			t.Fatalf("row %d: this fixture has no multi-pane parents, so no pane rows", i)
		}

		mm := m
		mm.cursor = i
		var got string
		mm.repoCandidatesFn = func(cursorRepoSlug string) ([]string, error) {
			got = cursorRepoSlug
			return []string{"/repos/whatever"}, nil
		}

		if _, _ = mm.Update(tea.KeyPressMsg{Code: 'n'}); got != want {
			t.Errorf("row %d (kind=%d): expected hint %q, got %q", i, r.kind, want, got)
		}
	}
}

// A pane row has no repo of its own and must answer as its parent row would,
// so a cursor parked on one of a worktree's panes still hints that worktree's
// repo rather than nothing.
func TestNewWorktreePaneRowHintsItsParent(t *testing.T) {
	m := makeTestModel(testStatuses())

	// Hand-build the row list rather than driving buildRows through a
	// multi-pane fixture: the assertion is about which row a pane defers to,
	// not about how pane rows come to exist.
	m.rows = []row{
		{kind: rowRepo, repo: "repo-a"},
		{kind: rowWorktree, repo: "repo-a", status: worktree.WorktreeStatus{
			Repo: "repo-a", Name: "feature-a", TmuxWindow: "wt-repo-a-feature-a",
		}},
		{kind: rowPane, pane: tmux.PaneState{PaneID: "%1", Window: "wt-repo-a-feature-a"}},
		{kind: rowSession, session: worktree.SessionStatus{Name: "collections"}},
		{kind: rowPane, pane: tmux.PaneState{PaneID: "%2", Session: "collections"}},
	}

	cases := map[int]string{
		2: "repo-a",      // pane under the worktree row
		4: "collections", // pane under the session row
	}
	for cursor, want := range cases {
		mm := m
		mm.cursor = cursor
		var got string
		mm.repoCandidatesFn = func(cursorRepoSlug string) ([]string, error) {
			got = cursorRepoSlug
			return []string{"/repos/whatever"}, nil
		}
		if _, _ = mm.Update(tea.KeyPressMsg{Code: 'n'}); got != want {
			t.Errorf("cursor on pane row %d: expected hint %q, got %q", cursor, want, got)
		}
	}
}

func TestNewWorktreeIgnoredWhileCreating(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.creating = true
	candidatesCalled := false
	m.repoCandidatesFn = func(_ string) ([]string, error) {
		candidatesCalled = true
		return []string{"/repos/alpha"}, nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: 'n'})
	m3 := m2.(Model)

	if cmd != nil {
		t.Error("n while a create is in flight should return no command")
	}
	if candidatesCalled {
		t.Error("n while a create is in flight must not rebuild the repo picker")
	}
	if m3.createMode != createNone {
		t.Errorf("n while a create is in flight must not change createMode, got %d", m3.createMode)
	}
	if m3.repoPicker != nil {
		t.Error("n while a create is in flight must not build a repo picker")
	}
}

func TestRepoPickFilterSelectsMatch(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repoCandidatesFn = func(_ string) ([]string, error) {
		return []string{"/repos/alpha", "/repos/beta"}, nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'n'})
	m3 := m2.(Model)

	for _, ch := range "beta" {
		m2, _ = m3.Update(tea.KeyPressMsg{Code: ch})
		m3 = m2.(Model)
	}
	m4, _ := m3.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m5 := m4.(Model)

	if m5.createMode != createNameInput {
		t.Fatalf("expected name-input mode after selecting a match, got mode=%d", m5.createMode)
	}
	if m5.createRepo != "/repos/beta" {
		t.Errorf("expected createRepo '/repos/beta', got %q", m5.createRepo)
	}
}

func TestRepoPickSelectedValidatesAndResolves(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repoCandidatesFn = func(_ string) ([]string, error) {
		return []string{"/repos/alpha"}, nil
	}
	var validated string
	m.validateRepoPathFn = func(path string) (string, error) {
		validated = path
		// Return a different resolved root than the raw candidate to prove
		// the resolution is actually applied, not just a pass-through (e.g.
		// a zoxide candidate that is a non-root subdirectory of a repo).
		return "/resolved/alpha-root", nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'n'})
	m3 := m2.(Model)
	m4, _ := m3.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m5 := m4.(Model)

	if validated != "/repos/alpha" {
		t.Errorf(
			"expected validateRepoPathFn called with the selected candidate, got %q",
			validated,
		)
	}
	if m5.createMode != createNameInput {
		t.Fatalf(
			"expected name-input mode after selecting a valid candidate, got mode=%d",
			m5.createMode,
		)
	}
	if m5.createRepo != "/resolved/alpha-root" {
		t.Errorf(
			"expected createRepo to be validateRepoPathFn's resolved root, got %q",
			m5.createRepo,
		)
	}
}

func TestRepoPickSelectedInvalidStaysInPicker(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repoCandidatesFn = func(_ string) ([]string, error) {
		return []string{"/repos/alpha"}, nil
	}
	m.validateRepoPathFn = func(path string) (string, error) {
		return "", fmt.Errorf("not a git repository: %s", path)
	}
	createCalled := false
	m.createFn = func(_, _, _ string) (string, error) {
		createCalled = true
		return "", nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'n'})
	m3 := m2.(Model)
	m4, _ := m3.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m5 := m4.(Model)

	if m5.createMode != createRepoPick {
		t.Fatalf(
			"selecting a candidate that fails validation should stay in repo-pick mode, got mode=%d",
			m5.createMode,
		)
	}
	if m5.status == "" {
		t.Error("expected a status message describing the invalid candidate")
	}
	if createCalled {
		t.Error("createFn must not be called when the selected candidate fails validation")
	}
}

func TestRepoPickFreeTypedPathValidated(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repoCandidatesFn = func(_ string) ([]string, error) {
		return []string{"/repos/alpha"}, nil
	}
	var validated string
	m.validateRepoPathFn = func(path string) (string, error) {
		validated = path
		return "/resolved/root", nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'n'})
	m3 := m2.(Model)
	for _, ch := range "/nonexistent/typed/path" {
		m2, _ = m3.Update(tea.KeyPressMsg{Code: ch})
		m3 = m2.(Model)
	}
	m4, _ := m3.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m5 := m4.(Model)

	if validated != "/nonexistent/typed/path" {
		t.Errorf("expected validateRepoPathFn called with the typed query, got %q", validated)
	}
	if m5.createMode != createNameInput {
		t.Fatalf(
			"expected name-input mode after a valid free-typed path, got mode=%d",
			m5.createMode,
		)
	}
	if m5.createRepo != "/resolved/root" {
		t.Errorf(
			"expected createRepo to be validateRepoPathFn's resolved root, got %q",
			m5.createRepo,
		)
	}
}

func TestRepoPickFreeTypedPathInvalidStaysInPicker(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repoCandidatesFn = func(_ string) ([]string, error) {
		return []string{"/repos/alpha"}, nil
	}
	m.validateRepoPathFn = func(path string) (string, error) {
		return "", fmt.Errorf("not a git repository: %s", path)
	}
	createCalled := false
	m.createFn = func(_, _, _ string) (string, error) {
		createCalled = true
		return "", nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'n'})
	m3 := m2.(Model)
	for _, ch := range "zzz-bad-path" {
		m2, _ = m3.Update(tea.KeyPressMsg{Code: ch})
		m3 = m2.(Model)
	}
	m4, _ := m3.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m5 := m4.(Model)

	if m5.createMode != createRepoPick {
		t.Fatalf(
			"invalid free-typed path should stay in repo-pick mode, got mode=%d",
			m5.createMode,
		)
	}
	if m5.status == "" {
		t.Error("expected a status message describing the invalid path")
	}
	if createCalled {
		t.Error("createFn must not be called when the typed path fails validation")
	}
}

func TestRepoPickEscReturnsToNormal(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repoCandidatesFn = func(_ string) ([]string, error) {
		return []string{"/repos/alpha"}, nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'n'})
	m3 := m2.(Model)
	if m3.createMode != createRepoPick {
		t.Fatal("expected repo-pick mode after n")
	}

	m4, _ := m3.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m5 := m4.(Model)
	if m5.createMode != createNone {
		t.Error("esc should return to normal mode")
	}
	if m5.repoPicker != nil {
		t.Error("esc should clear the repo picker")
	}
}

func TestNameInputTypingAccumulates(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"

	for _, ch := range "feat" {
		m2, _ := m.Update(tea.KeyPressMsg{Code: ch})
		m = m2.(Model)
	}
	if m.createInput.Value != "feat" {
		t.Errorf("expected createName 'feat', got %q", m.createInput.Value)
	}
}

func TestNameInputPasteInsertsInOneShot(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"

	m2, _ := m.Update(tea.PasteMsg{Content: "feat/pasted-branch"})
	m3 := m2.(Model)
	if m3.createInput.Value != "feat/pasted-branch" {
		t.Errorf("expected createName %q, got %q", "feat/pasted-branch", m3.createInput.Value)
	}
}

func TestNameInputPasteStripsControlChars(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")

	m2, _ := m.Update(tea.PasteMsg{Content: "/branch\n"})
	m3 := m2.(Model)
	if m3.createInput.Value != "feat/branch" {
		t.Errorf("expected createName %q, got %q", "feat/branch", m3.createInput.Value)
	}
}

func TestNameInputBackspaceRemovesLastRuneNotLastByte(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("café")

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m3 := m2.(Model)
	if m3.createInput.Value != "caf" {
		t.Errorf("expected createName %q, got %q", "caf", m3.createInput.Value)
	}
}

func TestRepoPickPasteInsertsIntoQuery(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repoCandidatesFn = func(string) ([]string, error) {
		return []string{"/repos/repo-a", "/repos/other"}, nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'n'})
	m3 := m2.(Model)
	if m3.createMode != createRepoPick {
		t.Fatalf("expected repo-pick mode, got mode=%d", m3.createMode)
	}

	m4, _ := m3.Update(tea.PasteMsg{Content: "other"})
	m5 := m4.(Model)
	if m5.repoPicker.Query() != "other" {
		t.Errorf("expected query %q, got %q", "other", m5.repoPicker.Query())
	}
}

func TestNameInputEnterEmptyIsNoop(t *testing.T) {
	createCalled := false
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createFn = func(_, _, _ string) (string, error) {
		createCalled = true
		return "", nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if cmd != nil {
		cmd()
	}
	if createCalled {
		t.Error("enter with an empty name must not call createFn")
	}
	if m3.createMode != createNameInput {
		t.Error("enter with an empty name should stay in name-input mode")
	}
}

func TestNameInputEscCancels(t *testing.T) {
	createCalled := false
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")
	m.createFn = func(_, _, _ string) (string, error) {
		createCalled = true
		return "", nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m3 := m2.(Model)
	if m3.createMode != createNone {
		t.Error("esc should return to normal mode")
	}
	if m3.createRepo != "" || m3.createInput.Value != "" {
		t.Error("esc should clear create state")
	}
	if createCalled {
		t.Error("esc must not call createFn")
	}
}

func TestCreateSuccessAttachesAndQuits(t *testing.T) {
	t.Setenv("TMUX", "test-session")

	var gotRepo, gotName string
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")
	m.createFn = func(repoPath, name, _ string) (string, error) {
		gotRepo = repoPath
		gotName = name
		return "", nil
	}
	attachCalled := false
	m.windowSessionFn = func(_ string) (string, bool) { return "sess", true }
	m.attachFn = func(_, _ string) error {
		attachCalled = true
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if m3.createMode != createNone {
		t.Error("enter should leave name-input mode immediately, before the async create resolves")
	}
	if !m3.creating {
		t.Error("enter should set creating=true before the async create resolves")
	}
	if cmd == nil {
		t.Fatal("expected a command after enter")
	}

	msg := cmd()
	created, ok := msg.(createdMsg)
	if !ok {
		t.Fatalf("expected createdMsg after a successful createFn, got %T", msg)
	}
	if gotRepo != "/repos/alpha" || gotName != "feat" {
		t.Errorf("createFn called with wrong args: repo=%q name=%q", gotRepo, gotName)
	}

	m3b, cmd2 := m3.Update(created)
	msgs := flattenCmd(cmd2)

	if m3b.(Model).creating {
		t.Error("creating should be cleared once createdMsg is processed")
	}
	if !attachCalled {
		t.Error("expected attach to be attempted after a successful create")
	}
	foundQuit := false
	for _, mm := range msgs {
		if _, ok := mm.(tea.QuitMsg); ok {
			foundQuit = true
		}
	}
	if !foundQuit {
		t.Error("expected tea.QuitMsg among the resulting messages after a successful attach")
	}
}

// TestCreateSuccessStaysInDashboardWhenAttachAfterCreateFalse proves
// worktree.attach_after_create: false keeps the user in the dashboard after a
// successful create — no attach, no quit — while still confirming the create
// via the status line. It runs with TMUX set so the only thing suppressing the
// attach is the config, not the "no tmux client to move" path.
func TestCreateSuccessStaysInDashboardWhenAttachAfterCreateFalse(t *testing.T) {
	t.Setenv("TMUX", "test-session")

	attachFalse := false
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	m.gc = &config.GlobalConfig{
		Worktree: config.WorktreeConfig{AttachAfterCreate: &attachFalse},
	}

	attachCalled := false
	m.windowSessionFn = func(_ string) (string, bool) { return "sess", true }
	m.attachFn = func(_, _ string) error {
		attachCalled = true
		return nil
	}

	m2, cmd := m.Update(createdMsg{repoPath: "/repos/alpha", name: "feat"})
	m3 := m2.(Model)

	// flattenCmd runs the returned commands: attachFn fires from inside the
	// async attach cmd, not from Update itself, so asserting before this point
	// would pass even if the attach were still wired up.
	msgs := flattenCmd(cmd)

	if attachCalled {
		t.Error("attach_after_create: false must not attach into the new window")
	}
	for _, mm := range msgs {
		if _, ok := mm.(tea.QuitMsg); ok {
			t.Error("attach_after_create: false must not quit the dashboard")
		}
	}
	if m3.creating {
		t.Error("creating should be cleared once createdMsg is processed")
	}
	if !strings.Contains(m3.status, "worktree created: feat") {
		t.Errorf("expected a create confirmation in the status line, got %q", m3.status)
	}
}

// TestCreateSuccessAttachesWhenAttachAfterCreateUnset is the backward-compat
// guard: a real loaded config that simply never sets the key must still
// attach-and-quit. TestCreateSuccessAttachesAndQuits covers the same default
// with a nil m.gc; this one proves a present-but-unset config behaves
// identically, which is the shape every pre-existing config on disk has.
func TestCreateSuccessAttachesWhenAttachAfterCreateUnset(t *testing.T) {
	t.Setenv("TMUX", "test-session")

	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	m.gc = &config.GlobalConfig{Worktree: config.WorktreeConfig{}}

	attachCalled := false
	m.windowSessionFn = func(_ string) (string, bool) { return "sess", true }
	m.attachFn = func(_, _ string) error {
		attachCalled = true
		return nil
	}

	_, cmd := m.Update(createdMsg{repoPath: "/repos/alpha", name: "feat"})

	// Runs the returned commands — the attach happens inside one of them, so
	// this must come before asserting attachCalled.
	msgs := flattenCmd(cmd)

	if !attachCalled {
		t.Error("an unset attach_after_create must keep attaching (the documented default)")
	}
	foundQuit := false
	for _, mm := range msgs {
		if _, ok := mm.(tea.QuitMsg); ok {
			foundQuit = true
		}
	}
	if !foundQuit {
		t.Error("an unset attach_after_create must still quit after a successful attach")
	}
}

func TestCreateFnFailureSetsStatusNoAttachNoQuit(t *testing.T) {
	t.Setenv("TMUX", "test-session")

	attachCalled := false
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")
	m.createFn = func(_, _, _ string) (string, error) {
		return "", fmt.Errorf("worktree already exists")
	}
	m.attachFn = func(_, _ string) error {
		attachCalled = true
		return nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if m3.createMode != createNone {
		t.Error(
			"enter should leave name-input mode immediately regardless of the eventual create result",
		)
	}
	if !m3.creating {
		t.Error("enter should set creating=true before the async create resolves")
	}
	if cmd == nil {
		t.Fatal("expected a command after enter")
	}

	msg := cmd()
	fm, ok := msg.(createFailedMsg)
	if !ok {
		t.Fatalf("expected createFailedMsg on create failure, got %T", msg)
	}
	if !strings.Contains(fm.err.Error(), "worktree already exists") {
		t.Errorf("expected the createFn error to be preserved, got %q", fm.err)
	}

	m4, _ := m3.Update(fm)
	m5 := m4.(Model)
	if m5.status == "" {
		t.Error("expected status set on the model after create failure")
	}
	if !strings.Contains(m5.status, "create failed") {
		t.Errorf("expected 'create failed' in status, got %q", m5.status)
	}
	if m5.creating {
		t.Error("creating should be cleared once createFailedMsg is processed")
	}
	if attachCalled {
		t.Error("attach must not be attempted when create fails")
	}
}

func TestCreateSuccessAttachFailureTriggersRefresh(t *testing.T) {
	t.Setenv("TMUX", "test-session")

	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	m.createFn = func(_, _, _ string) (string, error) { return "", nil }
	m.windowSessionFn = func(_ string) (string, bool) { return "", false }
	m.repairFn = func(_, _ string, _ worktree.Layout) error {
		return fmt.Errorf("repair unavailable")
	}

	created := createdMsg{repoPath: "/repos/alpha", name: "feat"}
	_, cmd := m.Update(created)
	msgs := flattenCmd(cmd)

	var gotStatus, gotStatuses bool
	for _, mm := range msgs {
		switch mm.(type) {
		case statusMsg:
			gotStatus = true
		case statusesMsg:
			gotStatuses = true
		}
	}
	if !gotStatus {
		t.Error("expected a failure statusMsg when attach and repair both fail")
	}
	if !gotStatuses {
		t.Error("expected a statusesMsg refresh even when attach fails, per the cycle plan")
	}
}

func TestNameInputEnterWithHookWarningRequiresSecondEnter(t *testing.T) {
	createCalled := false
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")
	m.checkHookCompatibilityFn = func(repoPath string) []string {
		if repoPath != "/repos/alpha" {
			t.Errorf("expected checkHookCompatibilityFn called with createRepo, got %q", repoPath)
		}
		return []string{"pre-commit (contains \"[ -d .git\")"}
	}
	m.createFn = func(_, _, _ string) (string, error) {
		createCalled = true
		return "", nil
	}

	// First enter: arms the warning, must not call createFn or leave name-input mode.
	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if cmd != nil {
		cmd()
	}
	if createCalled {
		t.Error("createFn must not be called on the first enter when hook warnings exist")
	}
	if m3.createMode != createNameInput {
		t.Error("first enter with hook warnings should stay in name-input mode")
	}
	if !m3.pendingHookWarning {
		t.Error("first enter with hook warnings should arm pendingHookWarning")
	}
	if !strings.Contains(m3.status, "hook warning") {
		t.Errorf("expected status to describe the hook warning, got %q", m3.status)
	}

	// Second enter: confirms, calls createFn, and leaves name-input mode.
	m4, cmd2 := m3.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m5 := m4.(Model)
	if cmd2 == nil {
		t.Fatal("expected a command after the confirming second enter")
	}
	cmd2()
	if !createCalled {
		t.Error("createFn must be called on the second enter once the hook warning is confirmed")
	}
	if m5.createMode != createNone {
		t.Error("second enter should leave name-input mode")
	}
	if m5.pendingHookWarning {
		t.Error("pendingHookWarning should be cleared once the create is kicked off")
	}
}

func TestNameInputEnterWithoutHookWarningCreatesImmediately(t *testing.T) {
	createCalled := false
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")
	m.checkHookCompatibilityFn = func(_ string) []string { return nil }
	m.createFn = func(_, _, _ string) (string, error) {
		createCalled = true
		return "", nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if cmd == nil {
		t.Fatal("expected a command on the first enter when there are no hook warnings")
	}
	cmd()
	if !createCalled {
		t.Error("createFn must be called on the first enter when there are no hook warnings")
	}
	if m3.createMode != createNone {
		t.Error("enter with no hook warnings should leave name-input mode immediately")
	}
}

func TestNameInputEditingNameDearmsHookWarning(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")
	m.checkHookCompatibilityFn = func(_ string) []string {
		return []string{"pre-commit (contains \"[ -d .git\")"}
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if !m3.pendingHookWarning {
		t.Fatal("expected pendingHookWarning armed after first enter")
	}

	m4, _ := m3.Update(tea.KeyPressMsg{Code: 'x'})
	m5 := m4.(Model)
	if m5.pendingHookWarning {
		t.Error("typing after the warning was armed should de-arm it, like confirmThenRemove")
	}
}

func TestCreateSuccessWarningSurfacesAsStatus(t *testing.T) {
	t.Setenv("TMUX", "test-session")

	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	m.windowSessionFn = func(_ string) (string, bool) { return "sess", true }
	m.attachFn = func(_, _ string) error { return nil }

	created := createdMsg{
		repoPath: "/repos/alpha",
		name:     "feat",
		warning:  "recent-repos store: disk full",
	}
	m2, cmd := m.Update(created)
	m3 := m2.(Model)
	if !strings.Contains(m3.status, "created, but:") || !strings.Contains(m3.status, "disk full") {
		t.Errorf("expected the warning to surface via status, got %q", m3.status)
	}

	msgs := flattenCmd(cmd)
	foundQuit := false
	for _, mm := range msgs {
		if _, ok := mm.(tea.QuitMsg); ok {
			foundQuit = true
		}
	}
	if !foundQuit {
		t.Error("a create warning must not prevent the attach/quit flow from proceeding")
	}
}

// --- Create flow (N — layout picker) ---

func TestNewWorktreeWithLayoutPickArmsWantsLayoutPick(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.repoCandidatesFn = func(_ string) ([]string, error) {
		return []string{"/repos/alpha"}, nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: 'N'})
	m3 := m2.(Model)

	if m3.createMode != createRepoPick {
		t.Fatalf("N should enter repo-pick mode like n, got mode=%d", m3.createMode)
	}
	if !m3.wantsLayoutPick {
		t.Error("N should arm wantsLayoutPick so name-input's enter continues into layout-pick")
	}
}

func TestNameInputEnterWithoutWantsLayoutPickDispatchesImmediately(t *testing.T) {
	// The n path (wantsLayoutPick == false): enter dispatches createFn
	// directly with layoutName == "" and never touches the layout picker —
	// today's default-layout behavior must stay unchanged.
	var gotLayout string
	layoutArgSeen := false
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")
	m.createFn = func(_, _, layoutName string) (string, error) {
		gotLayout = layoutName
		layoutArgSeen = true
		return "", nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if m3.createMode != createNone {
		t.Error("n path enter should dispatch immediately, not enter layout-pick")
	}
	if m3.layoutPicker != nil {
		t.Error("n path must never build a layout picker")
	}
	if cmd == nil {
		t.Fatal("expected a command after enter")
	}
	cmd()
	if !layoutArgSeen || gotLayout != "" {
		t.Errorf("expected createFn called with empty layoutName for the n path, got %q", gotLayout)
	}
}

func TestNameInputEnterWithWantsLayoutPickEntersLayoutPick(t *testing.T) {
	createCalled := false
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")
	m.wantsLayoutPick = true
	m.createFn = func(_, _, _ string) (string, error) {
		createCalled = true
		return "", nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)

	if cmd != nil {
		t.Error("entering layout-pick must not dispatch createFn yet")
	}
	if createCalled {
		t.Error("createFn must not be called until a layout is chosen")
	}
	if m3.createMode != createLayoutPick {
		t.Fatalf("expected createLayoutPick mode, got mode=%d", m3.createMode)
	}
	if m3.layoutPicker == nil {
		t.Fatal("expected a layout picker to be built")
	}
	if m3.createRepo != "/repos/alpha" {
		t.Error("createRepo must survive into layout-pick mode for the later dispatch")
	}
	if m3.createInput.Value != "feat" {
		t.Error("createInput must survive into layout-pick mode for the later dispatch")
	}
	// No gc is set on the test model, so ResolveLayout("", "", nil) falls all
	// the way to the opencode built-in fallback — the same default n's own
	// createFn closure would resolve to.
	sel, ok := m3.layoutPicker.Selected()
	if !ok || sel.Command != "opencode" {
		t.Errorf(
			"expected the resolved default layout (opencode) pre-selected, got %+v ok=%v",
			sel,
			ok,
		)
	}
}

func TestLayoutPickSelectedDispatchesCreateWithChosenLayout(t *testing.T) {
	var gotRepo, gotName, gotLayout string
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")
	m.wantsLayoutPick = true
	m.createFn = func(repoPath, name, layoutName string) (string, error) {
		gotRepo = repoPath
		gotName = name
		gotLayout = layoutName
		return "", nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if m3.createMode != createLayoutPick {
		t.Fatalf("expected createLayoutPick mode, got mode=%d", m3.createMode)
	}

	// Move off the pre-selected default (opencode) onto the next built-in and
	// select that one, so the test actually exercises a non-default choice
	// flowing through to createFn.
	m4, _ := m3.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m5 := m4.(Model)
	m6, cmd := m5.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m7 := m6.(Model)

	if m7.createMode != createNone {
		t.Error("selecting a layout should leave layout-pick mode immediately")
	}
	if !m7.creating {
		t.Error("selecting a layout should set creating=true before the async create resolves")
	}
	if m7.layoutPicker != nil {
		t.Error("selecting a layout should clear the layout picker")
	}
	if cmd == nil {
		t.Fatal("expected a command after selecting a layout")
	}
	cmd()
	if gotRepo != "/repos/alpha" || gotName != "feat" {
		t.Errorf("createFn called with wrong repo/name: repo=%q name=%q", gotRepo, gotName)
	}
	if gotLayout == "" || gotLayout == "opencode" {
		t.Errorf("expected a non-default layout name to reach createFn, got %q", gotLayout)
	}
}

func TestLayoutPickEscCancelsWholeCreate(t *testing.T) {
	createCalled := false
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")
	m.wantsLayoutPick = true
	m.createFn = func(_, _, _ string) (string, error) {
		createCalled = true
		return "", nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if m3.createMode != createLayoutPick {
		t.Fatal("expected createLayoutPick mode before esc")
	}

	m4, cmd := m3.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m5 := m4.(Model)

	if m5.createMode != createNone {
		t.Error("esc at layout-pick should return to normal mode")
	}
	if m5.createRepo != "" || m5.createInput.Value != "" {
		t.Error("esc at layout-pick should clear create state, same as the other two steps")
	}
	if m5.layoutPicker != nil {
		t.Error("esc at layout-pick should clear the layout picker")
	}
	if m5.wantsLayoutPick {
		t.Error("esc at layout-pick should clear wantsLayoutPick")
	}
	if createCalled {
		t.Error("esc at layout-pick must not call createFn")
	}
	if cmd != nil {
		t.Error("esc at layout-pick should return no command")
	}
}

func TestLayoutPickFreeTypedQueryDispatchesUnlistedName(t *testing.T) {
	// Mirrors the repo picker's free-typed-path precedent: a query that
	// matches no built-in name is passed through to createFn as-is, and
	// ResolveLayout (inside createFn) is what ultimately validates it.
	var gotLayout string
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")
	m.wantsLayoutPick = true
	m.createFn = func(_, _, layoutName string) (string, error) {
		gotLayout = layoutName
		return "", nil
	}

	m2, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)
	if m3.createMode != createLayoutPick {
		t.Fatal("expected createLayoutPick mode before typing")
	}

	for _, ch := range "totally-custom" {
		m4, _ := m3.Update(tea.KeyPressMsg{Code: ch})
		m3 = m4.(Model)
	}
	m5, cmd := m3.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m6 := m5.(Model)

	if m6.createMode != createNone {
		t.Fatalf("expected dispatch after a free-typed layout enter, got mode=%d", m6.createMode)
	}
	if cmd == nil {
		t.Fatal("expected a command after a free-typed layout enter")
	}
	cmd()
	if gotLayout != "totally-custom" {
		t.Errorf("expected the free-typed layout name to reach createFn, got %q", gotLayout)
	}
}

func TestEnterLayoutPickResolveLayoutErrorStaysInNameInput(t *testing.T) {
	// Only reachable with an invalid gc.Worktree.DefaultLayout: enterLayoutPick
	// resolves the default via ResolveLayout("", "", gc) before it can build the
	// picker, and an unknown configured default_layout makes that resolution
	// fail. The picker must never open in that case.
	createCalled := false
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")
	m.wantsLayoutPick = true
	m.gc = &config.GlobalConfig{Worktree: config.WorktreeConfig{DefaultLayout: "not-a-real-layout"}}
	m.createFn = func(_, _, _ string) (string, error) {
		createCalled = true
		return "", nil
	}

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)

	if cmd != nil {
		t.Error("a ResolveLayout failure must not dispatch createFn")
	}
	if createCalled {
		t.Error("createFn must not be called when the default layout fails to resolve")
	}
	if m3.createMode != createNameInput {
		t.Errorf(
			"expected to stay in name-input mode on resolve failure, got mode=%d",
			m3.createMode,
		)
	}
	if m3.layoutPicker != nil {
		t.Error("a ResolveLayout failure must not build a layout picker")
	}
	if m3.status == "" {
		t.Error("expected a status message describing the resolve failure")
	}
}

// --- Create flow (loading status) ---

func TestFriendlyLayoutName(t *testing.T) {
	cases := map[string]string{
		"nvim":               "neovim",             // config token reads as the real name
		"claude-nvim":        "claude + neovim",    // two-pane layout
		"opencode":           "opencode",           // already friendly, unchanged
		"claude":             "claude",             // already friendly, unchanged
		"some-future-custom": "some-future-custom", // unknown falls back to the raw name
	}
	for in, want := range cases {
		if got := friendlyLayoutName(in); got != want {
			t.Errorf("friendlyLayoutName(%q) = %q, want %q", in, want, got)
		}
	}
}

func TestLayoutActionStatusNamesResolvedLayout(t *testing.T) {
	// N-path create: an explicit layout name, friendly-mapped.
	if got := layoutActionStatus(
		"creating worktree",
		"feat",
		"nvim",
		nil,
	); got != "creating worktree: feat (neovim)…" {
		t.Errorf("N-path create status = %q", got)
	}
	// n-path: "" resolves the default the same way the action will (nil gc falls
	// all the way through to the opencode built-in), so the status still names it.
	if got := layoutActionStatus(
		"creating worktree",
		"feat",
		"",
		nil,
	); got != "creating worktree: feat (opencode)…" {
		t.Errorf("n-path create status = %q", got)
	}
	// Reused for repair with a different verb, same layout naming.
	if got := layoutActionStatus(
		"repairing",
		"feat",
		"nvim",
		nil,
	); got != "repairing: feat (neovim)…" {
		t.Errorf("repair status = %q", got)
	}
	// An unresolvable layout name drops the tool label rather than erroring here;
	// the action's own ResolveLayout call surfaces the real error afterward.
	if got := layoutActionStatus(
		"creating worktree",
		"feat",
		"bogus-xyz",
		nil,
	); got != "creating worktree: feat…" {
		t.Errorf("unresolvable-layout status = %q", got)
	}
}

func TestActionStatus(t *testing.T) {
	if got := actionStatus("deleting", "feat"); got != "deleting: feat…" {
		t.Errorf("actionStatus = %q", got)
	}
}

// The dashboard must show immediate feedback the moment a create is dispatched,
// not sit blank while the worktree + tmux window are built.
func TestDispatchCreateSetsCreatingStatus(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.createMode = createNameInput
	m.createRepo = "/repos/alpha"
	m.createInput.SetValue("feat")
	// wantsLayoutPick is false → the n-path dispatches immediately on enter.

	m2, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m3 := m2.(Model)

	if !strings.Contains(m3.status, "creating worktree: feat") {
		t.Errorf("expected a creating-status while the create runs, got %q", m3.status)
	}
	if cmd == nil {
		t.Fatal("expected the async create command to be returned")
	}
}
