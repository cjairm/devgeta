// The s → folder-pick → name-prompt → CreateSession flow for standalone tmux
// sessions. Deliberately separate from create_flow.go's n/N worktree flow: a
// plain session has no hook-compatibility check and no layout pick, and its
// folder is any directory (not necessarily a git repo), so reusing
// createMode/createInput here would conflate two flows whose invariants
// (createRepo, dispatchCreate's layout handling) don't apply to a session.

package tuiworktree

import (
	"os"

	tea "charm.land/bubbletea/v2"

	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
	"github.com/cjairm/devgeta/pkg/paths"
)

// sessionMode tracks progress through the s → folder-pick → name-input →
// create flow, mirroring create_flow.go's createMode: a small explicit field
// checked early in handleKey, not a deep state machine. sessionNone means the
// flow is inactive (the dashboard is in its normal state).
type sessionMode int

const (
	sessionNone sessionMode = iota
	sessionFolderPick
	sessionNameInput
)

// sessionRootLabel is the pinned first entry of the folder picker: selecting
// it opens the session in the user's home directory. It's a display label, not
// a path — resolveSessionFolder maps it to paths.Paths.Home.Root. A bare
// "root" can never collide with a real candidate, since every candidate path
// from repoCandidatesFn is absolute (e.g. "/Users/x/dev"), so this sentinel is
// unambiguous.
const sessionRootLabel = "root"

// sessionCreatedMsg reports a successful createSessionFn call made outside
// tmux, where there is no attached client to switch - so Update sets the
// "session created:" confirmation and refreshes the session rows instead.
// Inside tmux, a successful create instead switches-and-quits directly from
// dispatchSessionCreate's tea.Cmd, and this message is never produced.
type sessionCreatedMsg struct {
	name string
}

// sessionKilledMsg reports a successful killSessionFn call so Update can drop
// the killed session from m.sessions, rebuild rows, and set a "removed:"
// confirmation - mirroring deletedMsg's shape for worktree deletes.
type sessionKilledMsg struct {
	name string
}

// handleNewSession opens the folder picker for the s keybinding, from any row.
// It offers "root" (the user's home) pinned first, then the same ranked repo
// candidates the worktree flow uses (repoCandidatesFn), and — like that flow —
// also accepts a free-typed path that matches no candidate. A plain session
// isn't scoped to a repo, so the picked folder is validated as a directory
// only (validateSessionDirFn), not a git repo. The cursor row's repo is passed
// as the candidate hint so, if you're sitting on a repo, its folder ranks high.
func (m Model) handleNewSession() (tea.Model, tea.Cmd) {
	var cursorRepoSlug string
	if sel, ok := m.selectedStatus(); ok {
		cursorRepoSlug = sel.Repo
	} else if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowRepo {
		cursorRepoSlug = m.rows[m.cursor].repo
	}

	candidates, err := m.repoCandidatesFn(cursorRepoSlug)
	if err != nil {
		m.status = "failed to list folders: " + err.Error()
		return m, nil
	}

	// "root" is pinned at index 0 so FuzzyPicker's cursor (always starting at
	// 0 of an unfiltered list) lands on home by default — the common case for a
	// scratch session. Its Hint shows "~" so it's clear where root points.
	items := make([]tuicomponents.PaletteItem, 0, len(candidates)+1)
	items = append(items, tuicomponents.PaletteItem{Command: sessionRootLabel, Hint: "~"})
	for _, c := range candidates {
		items = append(items, tuicomponents.PaletteItem{Command: c})
	}

	m.sessionFolderPicker = tuicomponents.NewFuzzyPicker("New session — pick a folder", items)
	m.sessionMode = sessionFolderPick
	return m, nil
}

// handleSessionFolderPickKey mirrors create_flow.go's handleRepoPickKey: it
// delegates to the FuzzyPicker for list selection and cancellation, and
// handles the free-typed-path case itself (a query matching no candidate makes
// enter report FuzzyPickerNone). resolveSessionFolder validates whichever path
// was chosen and, on success, advances to the name prompt.
func (m Model) handleSessionFolderPickKey(key string) (tea.Model, tea.Cmd) {
	result := m.sessionFolderPicker.HandleKey(key)
	switch result.Action {
	case tuicomponents.FuzzyPickerCancelled:
		m.clearSessionState()

	case tuicomponents.FuzzyPickerSelected:
		m.resolveSessionFolder(result.Item.Command)

	case tuicomponents.FuzzyPickerNone:
		if key != "enter" {
			break
		}
		query := m.sessionFolderPicker.Query()
		if query == "" {
			break
		}
		m.resolveSessionFolder(query)
	}
	return m, nil
}

// resolveSessionFolder maps the picked entry to a directory and, on success,
// records it as sessionWorkdir and advances to the name prompt. The
// sessionRootLabel sentinel resolves to the user's home; anything else is
// treated as a path (a picked candidate or a free-typed query). Both are run
// through validateSessionDirFn so a non-existent or non-directory path is
// rejected immediately with a status message, leaving the picker open — the
// same shape as create_flow.go's resolveAndValidateRepoPath, minus the git
// requirement.
func (m *Model) resolveSessionFolder(candidate string) {
	path := candidate
	if candidate == sessionRootLabel {
		path = paths.Paths.Home.Root
	}
	dir, err := m.validateSessionDirFn(path)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.sessionWorkdir = dir
	m.sessionMode = sessionNameInput
	m.sessionNameInput.Reset()
}

// handleSessionNameInputKey drives the floating single-line session-name
// prompt: esc cancels the whole flow, and every other key is a name edit
// delegated to the shared TextInput. Unlike create_flow.go's name step, enter
// with a blank name is NOT a no-op: it auto-generates a Dragon Ball name (see
// autoSessionName) so a user can create a scratch session by just pressing
// enter twice.
func (m Model) handleSessionNameInputKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.clearSessionState()
		return m, nil

	case "enter":
		name := m.sessionNameInput.Value
		if name == "" {
			name = m.autoSessionName()
		}
		return m.dispatchSessionCreate(name)

	default:
		m.sessionNameInput.HandleKey(key)
	}
	return m, nil
}

// handleSessionNameInputPaste inserts pasted text into the session-name field
// in one shot, the paste counterpart to handleSessionNameInputKey.
func (m Model) handleSessionNameInputPaste(text string) (tea.Model, tea.Cmd) {
	m.sessionNameInput.InsertText(text)
	return m, nil
}

// autoSessionName returns a "<folder>-<character>" name for a blank prompt,
// prefixed with the label of the folder the session opens in (see
// sessionLabelForDir) and checked against the live tmux sessions so it never
// collides with an existing one (see nextFreeSessionName). Listing is
// best-effort: if it fails (tmux unreachable for a real reason), we fall back
// to an empty taken-set and let tmux's own "new-session -s" duplicate check be
// the final guard — the same philosophy as the rest of this file, where create
// failures surface via statusMsg rather than being pre-checked. The listing
// error is folded into that best-effort fallback rather than blocking the user
// from creating.
func (m Model) autoSessionName() string {
	taken := map[string]bool{}
	if names, err := m.listSessionNamesFn(); err == nil {
		for _, n := range names {
			taken[n] = true
		}
	}
	return nextFreeSessionName(
		sessionLabelForDir(m.sessionWorkdir),
		taken,
		randomSessionNameOrder(),
	)
}

// clearSessionState resets the s → folder-pick → name-prompt flow back to
// normal mode, used on cancellation and once a create has been dispatched.
func (m *Model) clearSessionState() {
	m.sessionMode = sessionNone
	m.sessionFolderPicker = nil
	m.sessionWorkdir = ""
	m.sessionNameInput.Reset()
}

// dispatchSessionCreate captures the entered (or auto-generated) name and the
// folder chosen in the folder-pick step, closes the prompt, and kicks off the
// async createSessionFn call in that directory.
//
// Duplicate session names are not pre-checked here (Q2 in the original cycle
// plan): tmux's own "new-session -s <name>" already fails when the name
// exists, and a prompt-layer HasSession check would both duplicate that
// enforcement and add a TOCTOU gap between the check and the create. The
// failure just surfaces via statusMsg, same as every other action failure in
// this file. (autoSessionName's collision check is a convenience for the blank
// case, not the enforcement — a typed name still relies on tmux here.)
func (m Model) dispatchSessionCreate(name string) (tea.Model, tea.Cmd) {
	workdir := m.sessionWorkdir
	m.clearSessionState()
	m.status = actionStatus("creating session", name)

	createFn := m.createSessionFn
	switchFn := m.switchToSessionFn
	insideTmux := os.Getenv("TMUX") != ""

	return m, func() tea.Msg {
		if err := createFn(name, workdir); err != nil {
			return statusMsg("create session failed: " + err.Error())
		}
		if !insideTmux {
			// tmux new-session -d works without an attached client, so
			// create-outside-tmux is allowed through - only the switch is
			// skipped, mirroring the worktree create-outside-tmux path
			// (handleCreateSuccess).
			return sessionCreatedMsg{name: name}
		}
		if err := switchFn(name); err != nil {
			return statusMsg("switch failed: " + err.Error())
		}
		return tea.QuitMsg{}
	}
}

// handleSwitchToSession is enter's rowSession counterpart to handleAttach:
// switches the attached client to the selected session and quits. Guarded by
// the same $TMUX check and message as handleAttach, since moving the client
// requires one to already be attached.
func (m Model) handleSwitchToSession() (tea.Model, tea.Cmd) {
	sel, ok := m.selectedSession()
	if !ok {
		return m, nil
	}

	if os.Getenv("TMUX") == "" {
		m.status = notInsideTmuxStatus
		return m, nil
	}

	switchFn := m.switchToSessionFn
	name := sel.Name
	return m, func() tea.Msg {
		if err := switchFn(name); err != nil {
			return statusMsg("switch failed: " + err.Error())
		}
		return tea.QuitMsg{}
	}
}

// handleKillSession is d's rowSession counterpart to handleDelete: a
// two-press kill confirmation, armed/confirmed the same way
// confirmThenRemove is (arm on first press, clear on any other key, confirm
// on second press - see handleKey's pendingKillSession clearing block), but
// keyed by session name alone since sessions have no repo.
func (m Model) handleKillSession() (tea.Model, tea.Cmd) {
	sel, ok := m.selectedSession()
	if !ok {
		return m, nil
	}
	name := sel.Name

	// First press (or cursor moved to another session): arm
	if m.pendingKillSession != name {
		m.pendingKillSession = name
		return m, nil
	}

	// Second press: execute
	m.pendingKillSession = ""
	killFn := m.killSessionFn
	m.status = actionStatus("killing session", name)
	return m, func() tea.Msg {
		if err := killFn(name); err != nil {
			return statusMsg("kill session failed: " + err.Error())
		}
		return sessionKilledMsg{name: name}
	}
}

// renderSessionFolderPickPopup builds the raw (uncentered) folder-picker popup
// content; composited over the dashboard background via Overlay, mirroring
// renderRepoPickPopup's shape.
func (m Model) renderSessionFolderPickPopup() string {
	maxW := min(m.width-2, 64)
	return m.sessionFolderPicker.View(maxW)
}

// renderSessionNameInputPopup builds the raw (uncentered) session-name-prompt
// popup content; composited over the dashboard background via Overlay,
// mirroring renderNameInputPopup's shape. The hint spells out that a blank
// name auto-generates one, so the empty-enter path is discoverable.
func (m Model) renderSessionNameInputPopup() string {
	maxW := min(m.width-2, 64)
	lines := []string{
		"> " + m.sessionNameInput.RenderPlain(),
		m.palette.Inactive.Render("(blank = random <folder>-* name)"),
	}
	return m.palette.BorderedPane("New session — name", maxW, lines)
}
