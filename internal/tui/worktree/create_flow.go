// The n → repo-pick → name-input → create flow: picking a repo (from a
// FuzzyPicker list or a free-typed path), naming the new worktree, running
// the hook-compatibility confirm, and following a successful create with the
// same attach-and-quit path as pressing enter on an existing worktree row.

package tuiworktree

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

// createMode tracks progress through the n/N → repo-pick → name-input →
// (layout-pick) → create flow, layered on top of the dashboard's existing
// mode flags (showHelp, filter.Active, diffFocused) the same way those are: a
// small explicit field checked early in handleKey, not a deep state machine.
//
// createLayoutPick only appears in this progression when the flow was
// started via N (see Model.wantsLayoutPick): n's flow goes
// createRepoPick -> createNameInput -> dispatch, while N's goes
// createRepoPick -> createNameInput -> createLayoutPick -> dispatch.
type createMode int

const (
	createNone createMode = iota
	createRepoPick
	createNameInput
	createLayoutPick
)

// createdMsg reports a successful createFn call so Update can trigger the
// attach-and-quit (or refresh-on-cancel) follow-up outside the create cmd
// itself, matching how every other async result in this model flows back
// through a typed message rather than a callback. warning carries a non-fatal
// WarnFn message captured during the create (e.g. recent-repos record
// failure), if any, so it can be surfaced without blocking attach/quit.
type createdMsg struct {
	repoPath string
	name     string
	warning  string
}

// createFailedMsg reports a failed createFn call as its own typed message
// (rather than the generic statusMsg every other failure in this model
// uses) so Update can clear m.creating specifically when a create's result
// is what's being processed, instead of clearing it on every unrelated
// statusMsg (delete/session-delete/attach failures use statusMsg too, and
// none of those should touch the create-in-flight guard).
type createFailedMsg struct {
	err error
}

// handleNewWorktree opens the repo picker for the n keybinding: today's
// default-layout create, with no layout picker step.
func (m Model) handleNewWorktree() (tea.Model, tea.Cmd) {
	return m.startNewWorktree(false)
}

// handleNewWorktreeWithLayoutPick opens the repo picker for the N keybinding:
// the same repo-pick -> name-input flow as n, but once the name is entered it
// continues into createLayoutPick instead of dispatching immediately (see
// handleNameInputKey and enterLayoutPick).
func (m Model) handleNewWorktreeWithLayoutPick() (tea.Model, tea.Cmd) {
	return m.startNewWorktree(true)
}

// startNewWorktree is the shared n/N entry point: it offers the cursor row's
// repo first. RepoCandidates already ranks candidates (cursor repo, then cwd
// repo, then recents, then zoxide) with the top-ranked one first, matching
// FuzzyPicker's initial cursor at index 0, so no extra pre-selection logic is
// needed here. wantsLayoutPick records which keybinding started the flow so
// handleNameInputKey knows, once the name step succeeds, whether to dispatch
// createFn immediately (n) or transition into createLayoutPick first (N).
//
// Guarded by m.creating: clearCreateState runs synchronously in
// handleNameInputKey's enter branch, before the async createFn tea.Cmd it
// returns has run, so without this guard a user could press n/N again while a
// create is still in flight and start a second concurrent createFn call —
// racing the mgr.WarnFn swap/restore in createFn (see newModel). m.creating
// stays true for exactly that window, so a second n/N is a no-op instead.
func (m Model) startNewWorktree(wantsLayoutPick bool) (tea.Model, tea.Cmd) {
	if m.creating {
		return m, nil
	}

	candidates, err := m.repoCandidatesFn(m.cursorRepoHint())
	if err != nil {
		m.status = "failed to list repos: " + err.Error()
		return m, nil
	}

	items := make([]tuicomponents.PaletteItem, len(candidates))
	for i, c := range candidates {
		items[i] = tuicomponents.PaletteItem{Command: c}
	}

	m.repoPicker = tuicomponents.NewFuzzyPicker("New worktree — pick a repo", items)
	m.createMode = createRepoPick
	m.wantsLayoutPick = wantsLayoutPick
	return m, nil
}

// cursorRepoHint returns what the cursor row says about which repo the user
// means, for RepoCandidates to rank first. It is a hint, not an identifier:
// RepoCandidates resolves it against the repos it knows and ignores it when
// nothing matches, so a row that points at no repo costs nothing.
//
// Every row kind answers, because every row kind is something the user can be
// sitting on when they press n/N:
//
//   - a worktree row and a repo header already carry their repo slug.
//   - a session row carries a tmux session name. For a session devgeta made,
//     that is TmuxSessionName(repoSlug) - not always the slug itself, since it
//     rewrites ".", ":" and whitespace to "_" - so resolution matches through
//     that function rather than comparing strings. A session the user made by
//     hand still resolves whenever its name matches a known repo, which is
//     the common case for one opened in a project directory.
//   - a pane row has no repo of its own, so it defers to the worktree or
//     session row that owns it (enclosingPaneParent, the same backward scan
//     the chevron and fold logic use) and answers as that row would.
//
// Only session rows and pane rows are new here. Before them, hovering a
// session left the hint empty, so the picker offered the same top candidate
// regardless of where the cursor was - the reason this looked broken.
func (m Model) cursorRepoHint() string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	return repoHintForRow(m.rows, m.cursor)
}

// repoHintForRow is cursorRepoHint's per-row half, split out so the pane case
// can recurse into its parent row by index without re-entering the model's
// cursor bounds check.
func repoHintForRow(rows []row, i int) string {
	r := rows[i]
	switch r.kind {
	case rowWorktree:
		return r.status.Repo
	case rowRepo:
		return r.repo
	case rowSession:
		return r.session.Name
	case rowPane:
		// A pane row sits directly under its parent, so this resolves in one
		// step; ok=false only if emission order ever changes, and then the
		// hint is simply absent rather than wrong.
		if parent, ok := enclosingPaneParent(rows, i); ok {
			if parent.kind == rowWorktree {
				return parent.status.Repo
			}
			return parent.session.Name
		}
	}
	return ""
}

// resolveAndValidateRepoPath runs candidate through validateRepoPathFn and,
// on success, transitions into name-input mode with the resolved root
// recorded as createRepo. On failure it reports the error via m.status and
// leaves createMode untouched, so the caller stays in the picker.
//
// Both handleRepoPickKey branches (a list selection and a free-typed query)
// call this: a FuzzyPicker candidate isn't guaranteed to be a git repo
// root — RepoCandidates' zoxide source only canonicalizes a path
// (config.CanonicalRepoPath), it never confirms the path is actually a repo
// root, so a zoxide-tracked subdirectory could otherwise slip through
// unvalidated the way only free-typed paths used to be checked. Validating
// both the same way also means a resolved root that differs from the raw
// candidate (e.g. a subdirectory resolving to its repo's root) is what
// downstream code (checkHookCompatibilityFn, createFn, and
// handleCreateSuccess's repo-slug derivation) actually sees.
func (m *Model) resolveAndValidateRepoPath(candidate string) {
	root, err := m.validateRepoPathFn(candidate)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.createRepo = root
	m.createMode = createNameInput
	m.createInput.Reset()
}

// handleRepoPickKey delegates to the FuzzyPicker for everything except the
// free-typed-path case: FuzzyPicker only knows about the candidate items it
// was given, so a query that matches none of them makes enter report
// FuzzyPickerNone (by design) instead of selecting anything. The model
// validates that query as a path itself in that case.
func (m Model) handleRepoPickKey(key string) (tea.Model, tea.Cmd) {
	result := m.repoPicker.HandleKey(key)
	switch result.Action {
	case tuicomponents.FuzzyPickerCancelled:
		m.clearCreateState()

	case tuicomponents.FuzzyPickerSelected:
		m.resolveAndValidateRepoPath(result.Item.Command)

	case tuicomponents.FuzzyPickerNone:
		if key != "enter" {
			break
		}
		query := m.repoPicker.Query()
		if query == "" {
			break
		}
		m.resolveAndValidateRepoPath(query)
	}
	return m, nil
}

// handleNameInputKey drives the floating single-line name prompt: esc cancels
// the whole create flow, enter with a non-empty name kicks off createFn, and
// every other key is a name edit delegated to the shared TextInput (typing,
// backspace/delete, and left/right/home/end caret movement). Auto-naming on a
// blank name is out of scope for this cycle, so enter with no text is a
// no-op rather than falling back to a generated name.
//
// Enter also runs the hook-compatibility check (checkHookCompatibilityFn)
// before create: if it finds warnings, the same two-press arm/confirm
// pattern as delete (confirmThenRemove/pendingDelete) is used instead of
// worktree.go's own force=false prompt, which raw-prints and blocks on
// os.Stdin and would corrupt or hang the running bubbletea alt-screen
// program. The first enter arms pendingHookWarning and shows the warning as
// a status; a second enter proceeds. Editing the name de-arms it, same as
// confirmThenRemove clearing pendingDelete.
func (m Model) handleNameInputKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.clearCreateState()
		return m, nil

	case "enter":
		if m.createInput.Value == "" {
			return m, nil
		}
		if !m.pendingHookWarning {
			if warnings := m.checkHookCompatibilityFn(m.createRepo); len(warnings) > 0 {
				m.pendingHookWarning = true
				m.status = "hook warning: " + strings.Join(warnings, "; ") +
					" — press enter again to create anyway"
				return m, nil
			}
		}
		if m.wantsLayoutPick {
			// N started this flow: don't clearCreateState or dispatch yet —
			// createRepo/createInput are still needed one step later, once
			// the layout picker resolves a selection (see enterLayoutPick and
			// handleLayoutPickKey's dispatchCreate call). pendingHookWarning
			// was already satisfied above, so clear it now rather than
			// leaving it armed for a mode that never rechecks it.
			m.pendingHookWarning = false
			return m.enterLayoutPick()
		}
		return m.dispatchCreate("")

	default:
		// Everything else is a name edit delegated to the shared TextInput:
		// backspace/delete, caret movement (left/right/home/end), and printable
		// insertion. A change to the text de-arms a pending hook-warning confirm
		// (same as confirmThenRemove clearing pendingDelete on an edit); a bare
		// caret move leaves the warning armed since the name it applies to
		// hasn't changed.
		if _, changed := m.createInput.HandleKey(key); changed {
			m.pendingHookWarning = false
		}
	}
	return m, nil
}

// handleNameInputPaste inserts pasted text into the name field in one shot,
// the paste counterpart to handleNameInputKey: a tea.PasteMsg carries the
// whole clipboard content as one string, which handleNameInputKey's
// per-rune default case would otherwise drop except for its first rune.
func (m Model) handleNameInputPaste(text string) (tea.Model, tea.Cmd) {
	if m.createInput.InsertText(text) {
		m.pendingHookWarning = false
	}
	return m, nil
}

// enterLayoutPick transitions from name-input into the N-triggered layout
// picker, once a name has been entered and any hook warning confirmed.
// createRepo/createInput are deliberately left untouched here (no
// clearCreateState) — dispatchCreate needs them at the final dispatch point,
// one step later than the n path's immediate dispatch.
//
// The item list is ordered with the resolved default layout first (skipping
// it from the rest to avoid a duplicate), then every other built-in name in
// their existing stable order: FuzzyPicker's cursor always starts at index 0
// of an unfiltered item list (see NewFuzzyPicker/SetItems/refilter), so this
// ordering is what puts the cursor on the default without any extra
// pre-selection logic — the same trick startNewWorktree's repo picker already
// relies on for RepoCandidates' ranked output.
func (m Model) enterLayoutPick() (tea.Model, tea.Cmd) {
	def, err := worktree.ResolveLayout("", "", m.gc)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}

	names := worktree.BuiltinLayoutNames()
	items := make([]tuicomponents.PaletteItem, 0, len(names))
	items = append(items, tuicomponents.PaletteItem{Command: def.Name})
	for _, name := range names {
		if name == def.Name {
			continue
		}
		items = append(items, tuicomponents.PaletteItem{Command: name})
	}

	m.layoutPicker = tuicomponents.NewFuzzyPicker("New worktree — pick a layout", items)
	m.createMode = createLayoutPick
	return m, nil
}

// handleLayoutPickKey delegates to the FuzzyPicker, mirroring
// handleRepoPickKey's shape: a list selection or a free-typed query that
// matches nothing (FuzzyPickerNone + enter) both reach dispatchCreate.
// Free-typed layout names are allowed through unvalidated, same as the repo
// picker allows a free-typed path through to validateRepoPathFn — here,
// ResolveLayout itself is what validates the name (inside createFn), and its
// "unknown layout" error already lists the valid names, so a second
// unlisted-name check in this picker would just duplicate that message.
func (m Model) handleLayoutPickKey(key string) (tea.Model, tea.Cmd) {
	result := m.layoutPicker.HandleKey(key)
	switch result.Action {
	case tuicomponents.FuzzyPickerCancelled:
		m.clearCreateState()

	case tuicomponents.FuzzyPickerSelected:
		return m.dispatchCreate(result.Item.Command)

	case tuicomponents.FuzzyPickerNone:
		if key != "enter" {
			break
		}
		query := m.layoutPicker.Query()
		if query == "" {
			break
		}
		return m.dispatchCreate(query)
	}
	return m, nil
}

// dispatchCreate captures the flow's accumulated state (repo, name, and the
// resolved layout name — "" for the n path, a picked/typed name for N) and
// kicks off the async createFn call. It's the single dispatch point both the
// n path (handleNameInputKey) and the N path (handleLayoutPickKey) funnel
// through, so the m.creating-arming safety reasoning only has to live in one
// place.
func (m Model) dispatchCreate(layoutName string) (tea.Model, tea.Cmd) {
	repoPath := m.createRepo
	name := m.createInput.Value
	createFn := m.createFn
	m.clearCreateState()
	// Armed here, before the tea.Cmd below ever runs: Bubble Tea runs every
	// returned tea.Cmd in its own goroutine, so without setting this now
	// (synchronously, in the same Update call that clears createMode back to
	// createNone) a user could press n/N again and start a second concurrent
	// create before this one resolves. startNewWorktree checks m.creating and
	// refuses to open the picker while it's true; cleared again once
	// createdMsg or createFailedMsg is processed (handleCreateSuccess /
	// Update).
	m.creating = true
	// Show immediate feedback while the async create runs: building a worktree
	// (git) + a multi-pane tmux window is not instant, and clearCreateState
	// above just closed the popup, so without this the dashboard would sit with
	// no indication anything is happening. Replaced by handleCreateSuccess /
	// createFailedMsg the moment the create resolves (and moot inside tmux,
	// where success attaches-and-quits) — so it never lingers.
	m.status = layoutActionStatus("creating worktree", name, layoutName, m.gc)
	return m, func() tea.Msg {
		warning, err := createFn(repoPath, name, layoutName)
		if err != nil {
			return createFailedMsg{err: err}
		}
		return createdMsg{repoPath: repoPath, name: name, warning: warning}
	}
}

// actionStatus is the base in-progress status line shared by every worktree
// action (create/repair/delete): "<verb>: <name>…". Actions that build a tmux
// window layer the layout name on via layoutActionStatus; delete uses this
// bare form. Centralizing the shape keeps the feedback consistent across
// actions and in one place to change.
func actionStatus(verb, name string) string {
	return verb + ": " + name + "…"
}

// layoutActionStatus is actionStatus plus the friendly name of the layout the
// action will build — used by create and repair, which both stand up a window.
// It names the tool(s) (claude, opencode, neovim, "claude + neovim") rather
// than the internal config token, resolving layoutName the same way the action
// itself will ("" = resolve the configured default). Resolution is best-effort:
// if it fails (e.g. a free-typed unknown layout name), the action's own error
// surfaces later, so here we just drop the tool label rather than duplicate it.
func layoutActionStatus(verb, name, layoutName string, gc *config.GlobalConfig) string {
	if layout, err := worktree.ResolveLayout(layoutName, "", gc); err == nil {
		return verb + ": " + name + " (" + friendlyLayoutName(layout.Name) + ")…"
	}
	return actionStatus(verb, name)
}

// friendlyLayoutName maps a resolved layout's Name to a human label for the
// status line: the "nvim" config token reads as "neovim", the two-pane layout
// as "claude + neovim". opencode/claude already read fine and fall through, as
// does any future custom layout name.
func friendlyLayoutName(name string) string {
	switch name {
	case "nvim":
		return "neovim"
	case "claude-nvim":
		return "claude + neovim"
	default:
		return name
	}
}

// clearCreateState resets the n/N → repo-pick → name-input → (layout-pick)
// flow back to normal mode, used on cancellation and once a create has been
// kicked off. It does not touch m.creating: that field's lifecycle is
// independent, spanning from just before the create tea.Cmd is dispatched
// until its result is processed, which outlives this reset (clearCreateState
// runs synchronously before the async createFn call even starts).
func (m *Model) clearCreateState() {
	m.createMode = createNone
	m.repoPicker = nil
	m.layoutPicker = nil
	m.createRepo = ""
	m.createInput.Reset()
	m.wantsLayoutPick = false
	m.pendingHookWarning = false
}

// handleCreateSuccess follows a successful createFn with the same
// attach-and-quit path as pressing enter on an existing worktree row
// (attachToWindowCmd), batched with a statuses refresh so the just-created
// worktree shows up in the dashboard list even when attach doesn't end in
// quitting (e.g. the window couldn't be found and repair also failed). A
// non-empty warning (captured from mgr.WarnFn by createFn) is surfaced as
// status prefixed with "created, but:" so the user still knows the create
// itself succeeded, without blocking the attach/quit flow that follows.
//
// Two independent things can suppress the attach, and both leave the user in
// the dashboard with a "worktree created:" status instead:
//   - Outside tmux there is no client to move, so attaching is impossible.
//   - worktree.attach_after_create: false is the user asking to stay put and
//     keep working in the dashboard (see WorktreeConfig.AttachAfterCreate);
//     unset means attach, so existing configs are unaffected.
func (m Model) handleCreateSuccess(repoPath, name, warning string) (tea.Model, tea.Cmd) {
	m.creating = false
	attach := os.Getenv("TMUX") != "" && m.gc.ShouldAttachAfterCreate()
	if warning != "" {
		m.status = "created, but: " + warning
	} else if !attach {
		m.status = "worktree created: " + name
	}
	if !attach {
		return m, m.loadCmd()
	}
	repoSlug := filepath.Base(repoPath)
	return m, tea.Batch(m.attachToWindowCmd(repoSlug, name), m.loadCmd())
}

// renderRepoPickPopup builds the raw (uncentered) repo-picker popup content;
// the caller composites it over the dashboard background via Overlay, same
// as the help popup.
func (m Model) renderRepoPickPopup() string {
	maxW := min(m.width-2, 64)
	return m.repoPicker.View(maxW)
}

// renderNameInputPopup builds the raw (uncentered) name-prompt popup
// content; composited over the dashboard background via Overlay.
func (m Model) renderNameInputPopup() string {
	maxW := min(m.width-2, 64)
	lines := []string{"> " + m.createInput.RenderPlain()}
	return m.palette.BorderedPane("New worktree — name", maxW, lines)
}

// renderLayoutPickPopup builds the raw (uncentered) layout-picker popup
// content; the caller composites it over the dashboard background via
// Overlay, same as renderRepoPickPopup.
func (m Model) renderLayoutPickPopup() string {
	maxW := min(m.width-2, 64)
	return m.layoutPicker.View(maxW)
}
