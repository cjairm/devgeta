// The $ → rename-prompt flow (ADR-0052), for a standalone session row. Unlike
// kill-session and rename-session, RenameSession runs against the server
// directly and needs no attached client, so this flow has no $TMUX guard.

package tuiworktree

import (
	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// sessionRenamedMsg reports a successful renameSessionFn call so Update can
// update the standalone session that held the old name, move its fold key,
// and land the cursor back on it - mirroring
// sessionKilledMsg/deletedMsg's shape for the same reason: identity, not a
// position, survives a rebuild.
type sessionRenamedMsg struct {
	oldName string
	newName string
}

// handleRename opens the $ prompt for the selected session row, prefilled with its current name so a partial edit is the
// common case rather than retyping the whole thing.
func (m Model) handleRename() (tea.Model, tea.Cmd) {
	oldName, ok := m.selectedSessionName()
	if !ok {
		return m, nil
	}
	m.renaming = true
	m.renameOldName = oldName
	m.renameInput.SetValue(oldName)
	return m, nil
}

// handleRenameInputKey drives the rename prompt: esc cancels, enter
// validates and dispatches, everything else edits the text.
func (m Model) handleRenameInputKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.clearRenameState()
		return m, nil
	case "enter":
		return m.dispatchRename()
	default:
		m.renameInput.HandleKey(key)
	}
	return m, nil
}

// handleRenameInputPaste inserts pasted text into the rename field in one
// shot, the paste counterpart to handleRenameInputKey.
func (m Model) handleRenameInputPaste(text string) (tea.Model, tea.Cmd) {
	m.renameInput.InsertText(text)
	return m, nil
}

// clearRenameState resets the $ → rename-prompt flow back to normal mode,
// used on cancellation and once a rename has been dispatched.
func (m *Model) clearRenameState() {
	m.renaming = false
	m.renameOldName = ""
	m.renameInput.Reset()
}

// dispatchRename validates the typed name and kicks off the async
// renameSessionFn call. Unlike the session-create prompt, a blank name is
// rejected outright rather than auto-generated - there is no sensible
// auto-name for a rename, and leaving the prompt open is safer than a
// silent no-op.
//
// The duplicate check against the live session list is only for a clearer
// message: tmux's own rename-session already rejects a duplicate name
// ("duplicate session: <name>"), and that is what the status line shows if
// this check is raced by another client creating or renaming a session
// between the check and the call.
func (m Model) dispatchRename() (tea.Model, tea.Cmd) {
	old := m.renameOldName
	newName := worktree.TmuxSessionName(m.renameInput.Value)
	m.clearRenameState()

	if newName == "" {
		m.status = "session name cannot be blank"
		return m, nil
	}
	if newName == old {
		return m, nil
	}
	if names, err := m.listSessionNamesFn(); err == nil {
		for _, n := range names {
			if n == newName {
				m.status = "a session named " + newName + " already exists"
				return m, nil
			}
		}
	}

	renameFn := m.renameSessionFn
	m.status = actionStatus("renaming session", old)
	return m, func() tea.Msg {
		if err := renameFn(old, newName); err != nil {
			return statusMsg("rename failed: " + err.Error())
		}
		return sessionRenamedMsg{oldName: old, newName: newName}
	}
}

// applySessionRenamed is sessionRenamedMsg's handler: update the standalone
// session that held the old name, move its fold key from "sess:<old>" to "sess:<new>", rebuild, and land the cursor back on the
// renamed row by its NEW identity - rebuildRows' own cursor-restore looks up
// the OLD key, which no longer exists in the fresh rows the instant the name
// changes.
func (m *Model) applySessionRenamed(msg sessionRenamedMsg) {
	for i := range m.sessions {
		if m.sessions[i].Name == msg.oldName {
			m.sessions[i].Name = msg.newName
		}
	}
	oldKey, newKey := "sess:"+msg.oldName, "sess:"+msg.newName
	if v, ok := m.collapsed[oldKey]; ok {
		delete(m.collapsed, oldKey)
		m.collapsed[newKey] = v
	}
	m.rebuildRows()
	m.focusRow(func(r row) bool {
		return r.kind == rowSession && r.session.Name == msg.newName
	})
	m.status = "renamed: " + msg.oldName + " -> " + msg.newName
	m.saveViewState()
}
