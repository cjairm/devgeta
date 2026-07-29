// Package tuiworktree provides the Bubble Tea TUI dashboard for dg ws.
package tuiworktree

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/task"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/githubcli"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

const (
	minLeftPaneWidth     = 20
	defaultLeftPaneWidth = 35
	maxLeftPaneWidthPct  = 0.60
	dividerWidth         = 1
	refreshInterval      = 3 * time.Second
	maxDiffBytes         = 64 * 1024
	// prTitleTimeout bounds the best-effort gh PR-title lookup so a hung or slow
	// gh can't outlive one refresh interval and stall the diff pane.
	prTitleTimeout = 2 * time.Second
	// notInsideTmuxStatus is shown by both handleAttach and
	// handleSwitchToSession (session_flow.go): switching a tmux client only
	// makes sense from inside a tmux session.
	notInsideTmuxStatus = "not inside tmux; run `dg ws` from a tmux session"
)

// --- Messages ---

type (
	statusesMsg []worktree.WorktreeStatus
	// sessionsMsg carries a successful WorktreeManager.ListSessions() result;
	// see sessionsLoadCmd for the success/failure split.
	sessionsMsg []worktree.SessionStatus
	diffMsg     struct {
		content               string
		files, added, removed int
		fileLines             []int  // line indexes of per-file headers, for [ / ] jumps
		base                  string // comparison base label, e.g. "main @3e90667"
		branch                string // worktree branch the diff belongs to (display only)
		path                  string // worktree path the diff belongs to (PR-title cache key)
	}
	prTitleMsg struct {
		path  string // worktree path — the cache key (unique; branch names collide across repos)
		title string
	}
)

type (
	tickMsg   time.Time
	statusMsg string
)

// deletedMsg reports a successful removal so Update can both refresh the list
// (drop the removed row) and replace the transient "deleting…" status with a
// "removed:" confirmation. A plain statusesMsg would refresh the list but leave
// the "deleting…" status lingering, since its handler never touches m.status
// (it's also the periodic-refresh message, which must not clobber statuses).
type deletedMsg struct {
	name     string
	statuses []worktree.WorktreeStatus
}

// --- Model ---

// Model is the Bubble Tea model for the worktree TUI dashboard.
type Model struct {
	mgr     *worktree.WorktreeManager
	tmuxApp *tmux.Tmux
	gitApp  *git.Git
	gc      *config.GlobalConfig

	statuses []worktree.WorktreeStatus
	// sessions holds standalone tmux sessions with no worktree-backed window;
	// see sessionsLoadCmd for refresh cadence and failure handling.
	sessions       []worktree.SessionStatus
	loaded         bool // true once the first List() result is in, so an empty dashboard shows guidance instead of a permanent "(loading...)"
	sessionsLoaded bool // true once the first ListSessions() result is in; mirrors loaded, for placeCursorOnActive's give-up condition
	cursorPlaced   bool // true once placeCursorOnActive has landed the cursor on the attached row (or given up) — guards against a later periodic refresh re-running it and fighting the user's own navigation
	rows           []row
	cursor         int // index into rows (a leaf row — rowWorktree or rowSession — or a collapsed rowRepo header)
	collapsed      map[string]bool
	allCollapsed   bool

	diffContent   string
	diffFiles     int
	diffAdded     int
	diffRemoved   int
	diffScroll    int
	diffFocused   bool
	diffFileLines []int
	diffBase      string
	diffBranch    string // display branch for the "base ← branch" label
	diffPath      string // worktree path the current diff belongs to; PR-title cache lookup key

	prTitles       map[string]string // path -> PR title; "" cached means "looked up, no PR"
	prTitlePending map[string]bool   // path -> lookup in flight, so we don't double-dispatch

	filter tuicomponents.FilterField

	status string

	width  int
	height int

	palette *tuicomponents.Palette

	leftPaneWidth int

	dragging   bool
	dragStartX int

	pendingDelete        string // "repo/name" or ""
	pendingSessionDelete string // "repo/name" or ""
	pendingKillSession   string // armed session name (sessions have no repo) or ""
	showHelp             bool

	// sessionMode and its companion fields back the s → folder-pick →
	// name-prompt → CreateSession flow, kept deliberately separate from
	// createMode/createInput: that machinery belongs to the n/N worktree flow
	// (repo-pick, hook-check, layout-pick) and none of those steps apply to a
	// plain session. See session_flow.go. sessionWorkdir holds the folder
	// resolved in sessionFolderPick, carried into the create dispatched from
	// sessionNameInput.
	sessionMode         sessionMode
	sessionFolderPicker *tuicomponents.FuzzyPicker
	sessionWorkdir      string
	sessionNameInput    tuicomponents.TextInput

	createMode         createMode
	repoPicker         *tuicomponents.FuzzyPicker
	createRepo         string                  // resolved repo path chosen in repo-pick mode
	createInput        tuicomponents.TextInput // in-progress name text + caret in name-input mode
	wantsLayoutPick    bool                    // set when the flow was started via N (handleNewWorktreeWithLayoutPick) rather than n; read once, after a successful name-input enter, to decide whether to dispatch createFn immediately (n) or transition into createLayoutPick (N) first
	layoutPicker       *tuicomponents.FuzzyPicker
	pendingHookWarning bool // armed by a first enter when CheckHookCompatibility found warnings; a second enter confirms, any other key (or edited name) de-arms it
	creating           bool // true from the moment the create tea.Cmd is dispatched until its result (createdMsg/createFailedMsg) is processed; the ONLY thing that actually enforces "one create at a time" (see createFn's WarnFn-swap comment below) — handleNewWorktree checks this and ignores n while it's true

	// reviewMode and its companion fields back the R -> picker -> launch flow
	// (see review_flow.go): a single-step flow triggered directly from the
	// cursor row, with no repo-pick or name-input, so it's its own small
	// sibling enum next to createMode/sessionMode rather than a new
	// createXxx value. reviewRepo/reviewWorktreeName are captured at
	// pick-open time (the cursor row's Repo/Name), not re-derived later,
	// matching how createRepo/createInput are captured before their own
	// later steps run.
	reviewMode         reviewMode
	reviewPicker       *tuicomponents.FuzzyPicker
	reviewRepo         string // repo slug captured when the picker opened
	reviewWorktreeName string // worktree name captured when the picker opened
	reviewLaunching    bool   // true from the moment the launch tea.Cmd is dispatched until reviewLaunchedMsg is processed; the re-entry guard handleKickReview checks

	// Injected I/O seams (overridable in tests)
	diffFn                   func(path string) (task.BranchDiffResult, error)
	attachFn                 func(session, window string) error
	removeFn                 func(repo, name string, force bool) error
	removeSessionFn          func(repo, name string) error
	repairFn                 func(repo, name string, layout worktree.Layout) error
	windowSessionFn          func(window string) (string, bool)
	clearAgentStateFn        func(window string) error
	currentSessionFn         func() (string, bool)
	createSessionFn          func(name, workdir string) error
	switchToSessionFn        func(name string) error
	killSessionFn            func(name string) error
	listSessionNamesFn       func() ([]string, error)
	repoCandidatesFn         func(cursorRepoSlug string) ([]string, error)
	validateRepoPathFn       func(path string) (string, error)
	validateSessionDirFn     func(path string) (string, error)
	checkHookCompatibilityFn func(repoPath string) []string
	createFn                 func(repoPath, name, layoutName string) (warning string, err error)
	prTitleFn                func(branch, path string) string
	launchReviewFn           func(repo, name, reviewerKey string) error
}

func newModel(
	mgr *worktree.WorktreeManager,
	tmuxApp *tmux.Tmux,
	gitApp *git.Git,
	gc *config.GlobalConfig,
) Model {
	m := Model{
		mgr:            mgr,
		tmuxApp:        tmuxApp,
		gitApp:         gitApp,
		gc:             gc,
		collapsed:      map[string]bool{},
		palette:        tuicomponents.NewPalette(),
		leftPaneWidth:  defaultLeftPaneWidth,
		prTitles:       map[string]string{},
		prTitlePending: map[string]bool{},
	}
	m.diffFn = func(path string) (task.BranchDiffResult, error) {
		return task.BranchDiffAt(gitApp, path)
	}
	m.attachFn = func(session, window string) error {
		return tmuxApp.SwitchToWindow(session, window)
	}
	m.removeFn = func(repo, name string, force bool) error {
		return mgr.RemoveInRepo(repo, name, force)
	}
	m.removeSessionFn = func(repo, name string) error {
		return mgr.RemoveWithSessionInRepo(repo, name)
	}
	m.repairFn = func(repo, name string, layout worktree.Layout) error {
		return mgr.RepairInRepo(repo, name, layout)
	}
	m.launchReviewFn = func(repo, name, reviewerKey string) error {
		return mgr.LaunchReviewInRepo(repo, name, reviewerKey)
	}
	m.windowSessionFn = tmuxApp.WindowSession
	m.clearAgentStateFn = tmuxApp.ClearAgentStateForWindow
	// CurrentSession (tmux display-message) rather than scanning ListSessions
	// for the first session_attached: it resolves the session of *this*
	// client specifically, so it stays correct when several clients are
	// attached to different sessions at once. Verified to report the right
	// session both for a plain `dg ws` and for a dashboard launched into a
	// window spawned by the tmux binding's `new-window`.
	m.currentSessionFn = tmuxApp.CurrentSession
	m.createSessionFn = tmuxApp.CreateSession
	m.switchToSessionFn = tmuxApp.SwitchToSession
	m.killSessionFn = tmuxApp.KillSession
	// listSessionNamesFn feeds the blank-name auto-namer's collision check: it
	// needs every session on the tmux server (not just the standalone ones the
	// dashboard shows), so it goes through tmuxApp.ListSessions directly rather
	// than mgr.ListSessions, which filters to standalone sessions.
	m.listSessionNamesFn = func() ([]string, error) {
		sessions, err := tmuxApp.ListSessions()
		if err != nil {
			return nil, err
		}
		names := make([]string, len(sessions))
		for i, s := range sessions {
			names[i] = s.Name
		}
		return names, nil
	}
	m.repoCandidatesFn = mgr.RepoCandidates
	m.validateRepoPathFn = mgr.ValidateRepoPath
	m.validateSessionDirFn = mgr.ValidateDirPath
	// CheckHookCompatibility only stats/reads hook files (and reads
	// core.hooksPath via a read-only git config --get) — no prints, no
	// prompts, no writes — so it's safe to call directly from the TUI, unlike
	// worktree.go's own force=false path which raw-prints and blocks on
	// os.Stdin. The model calls this itself before create so the user still
	// gets the warning, just through a TUI-safe confirm (see
	// handleNameInputKey), instead of losing it to a hardcoded force=true.
	m.checkHookCompatibilityFn = gitApp.CheckHookCompatibility
	m.createFn = func(repoPath, name, layoutName string) (string, error) {
		// layoutName is "" for the n path (today's default behavior) and the
		// N-picked name for the N path. aiAlias is always "" here — no flag or
		// env var reaches this closure — which is exactly what lets
		// ResolveLayout consult gc.Worktree.DefaultLayout before falling back
		// to gc.Worktree.DefaultAI and then opencode when layoutName is also
		// empty. See ResolveLayout's doc comment for why the folded
		// ResolveAIAlias output must NOT be passed here instead.
		layout, err := worktree.ResolveLayout(layoutName, "", gc)
		if err != nil {
			return "", err
		}
		// mgr.WarnFn fires synchronously from inside CreateAt below (e.g. the
		// recent-repos store failed to record this create). Swapping it to a
		// local closure and restoring it via defer right after CreateAt
		// returns is safe only because this TUI never runs two creates
		// concurrently — and that's actually true, not just assumed: this
		// tea.Cmd closure only ever runs while m.creating is true, and
		// handleNewWorktree (the only way to start another create) refuses
		// to open the picker while m.creating is true, so a second createFn
		// call can never be in flight to race this swap/restore.
		var warning string
		original := mgr.WarnFn
		mgr.WarnFn = func(msg string) { warning = msg }
		defer func() { mgr.WarnFn = original }()
		// force=true is safe here specifically because the model already ran
		// its own equivalent hook-compatibility check (checkHookCompatibilityFn)
		// and, when it found warnings, its own equivalent TUI-safe confirm
		// (handleNameInputKey's pendingHookWarning arm/second-enter) before
		// ever reaching this closure — so this isn't skipping the check,
		// worktree.go's raw fmt.Println/stdin-read version of it is just
		// redundant by this point and would otherwise corrupt or hang the
		// running bubbletea alt-screen display.
		if err := mgr.CreateAt(repoPath, name, layout, true); err != nil {
			return "", err
		}
		return warning, nil
	}
	// Reuse the shared gh wrapper rather than shelling out to gh here, so the
	// diff pane's PR-title lookup goes through the same executor as every other
	// gh call. PRTitleAt is best-effort and bounded: it returns "" (never an
	// error) when gh is absent, unauthenticated, times out, or the branch has
	// no PR — all of which the header treats as "no title".
	gh := githubcli.New()
	m.prTitleFn = func(_, path string) string {
		return gh.PRTitleAt(path, prTitleTimeout)
	}
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadCmd(),
		m.sessionsLoadCmd(),
		m.tickCmd(),
	)
}

func (m Model) loadCmd() tea.Cmd {
	return func() tea.Msg {
		statuses, err := m.mgr.List()
		if err != nil {
			return statusMsg("failed to list worktrees: " + err.Error())
		}
		return statusesMsg(statuses)
	}
}

// sessionsLoadCmd fetches standalone tmux sessions, dispatched alongside
// loadCmd (Init and every tickMsg) so the two stay in sync on the same 3s
// refresh. It's a separate tea.Cmd/message pair rather than folded into
// loadCmd's result: worktrees load from the filesystem/git and sessions from
// tmux, so a failure in either source must never affect the other's rows.
//
// On a real error, this returns a statusMsg (not a sessionsMsg) exactly like
// loadCmd does above - the statusMsg case only sets m.status, so m.sessions
// is never touched and the last-good session rows keep rendering. A genuinely
// empty result (WorktreeManager.ListSessions' no-server case is (nil, nil))
// is not an error: it produces a sessionsMsg, which the Update case below
// applies with no status warning.
func (m Model) sessionsLoadCmd() tea.Cmd {
	return func() tea.Msg {
		sessions, err := m.mgr.ListSessions()
		if err != nil {
			return statusMsg("failed to list sessions: " + err.Error())
		}
		return sessionsMsg(sessions)
	}
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// branchLabel returns the display branch for a worktree status: s.Branch,
// falling back to s.Name when the worktree has no tracked branch. Used both
// for the diff header's "base ← branch" label and as the branch argument
// passed into prTitleFn, so the two can never drift apart. It is NOT a cache
// key — branch names can collide across repos (see maybePRTitleCmd, which
// keys its cache by path instead).
func branchLabel(s worktree.WorktreeStatus) string {
	if s.Branch != "" {
		return s.Branch
	}
	return s.Name
}

func (m Model) computeDiffCmd(s worktree.WorktreeStatus) tea.Cmd {
	df := m.diffFn
	p := m.palette
	path := s.Path
	branch := branchLabel(s)
	return func() tea.Msg {
		res, err := df(path)
		content := res.Content
		if err != nil {
			content = "(diff unavailable: " + err.Error() + ")"
		} else {
			content = rewriteFileHeaders(content, res.FileStats, p)
		}
		if len(content) > maxDiffBytes {
			content = content[:maxDiffBytes] + "\n... (truncated)"
		}
		base := res.BaseBranch
		if base != "" && res.BaseSHA != "" {
			base += " @" + res.BaseSHA
		}
		return diffMsg{
			content:   content,
			files:     res.Files,
			added:     res.Added,
			removed:   res.Removed,
			fileLines: diffFileHeaderLines(content),
			base:      base,
			branch:    branch,
			path:      path,
		}
	}
}

// prTitleCmd looks up branch's PR title via m.prTitleFn, run in path. The
// returned message is keyed by path (see maybePRTitleCmd for why). It is
// dispatched separately from computeDiffCmd so a slow or hung `gh` can never
// stall the 3s diff-refresh tick.
func (m Model) prTitleCmd(branch, path string) tea.Cmd {
	fn := m.prTitleFn
	return func() tea.Msg { return prTitleMsg{path: path, title: fn(branch, path)} }
}

// maybePRTitleCmd returns a command to look up s's PR title, or nil if it is
// already cached or a lookup is already in flight. Marks the path pending so
// a later selection/tick won't dispatch a duplicate.
//
// The cache (and pending set) is keyed by s.Path, not branch name: the
// dashboard aggregates worktrees across multiple repos, and two different
// repos can have worktrees on identically-named branches (e.g. "main", or a
// coincidental "feature/x") — keying by branch would let the second one's
// lookup get skipped as "cached" and render the first repo's title. Path is
// unique per worktree, so it can't collide. branchLabel(s) is still what
// gets passed to prTitleFn as the branch argument gh needs.
func (m *Model) maybePRTitleCmd(s worktree.WorktreeStatus) tea.Cmd {
	if _, cached := m.prTitles[s.Path]; cached {
		return nil
	}
	if m.prTitlePending[s.Path] {
		return nil
	}
	m.prTitlePending[s.Path] = true
	return m.prTitleCmd(branchLabel(s), s.Path)
}

// selectionChangedCmd builds the batch of commands to run whenever the
// selected worktree changes (statusesMsg reload, or j/k moving the cursor):
// always recompute the diff, and additionally kick off a PR-title lookup
// when s's path has no cache entry and none is pending. Pointer receiver is
// required so maybePRTitleCmd's pending-flag mutation lands on the
// addressable local m in the three call sites (all of which operate on a
// value-receiver Update/handleKey's local m before returning it).
func (m *Model) selectionChangedCmd(sel worktree.WorktreeStatus) tea.Cmd {
	cmds := []tea.Cmd{m.computeDiffCmd(sel)}
	if c := m.maybePRTitleCmd(sel); c != nil {
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

func (m Model) selectedStatus() (worktree.WorktreeStatus, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return worktree.WorktreeStatus{}, false
	}
	r := m.rows[m.cursor]
	if r.kind != rowWorktree {
		return worktree.WorktreeStatus{}, false
	}
	return r.status, true
}

// selectedSession mirrors selectedStatus for rowSession rows: it reports the
// cursor's session (ok=true) only when the cursor sits on a rowSession leaf,
// so handleKey can branch enter/d on row kind the same way selectedStatus lets
// it branch on rowWorktree today.
func (m Model) selectedSession() (worktree.SessionStatus, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return worktree.SessionStatus{}, false
	}
	r := m.rows[m.cursor]
	if r.kind != rowSession {
		return worktree.SessionStatus{}, false
	}
	return r.session, true
}

func (m *Model) rebuildRows() {
	m.rows = buildRows(m.statuses, m.sessions, m.collapsed, m.filter.Value())
	// Keep cursor on a valid leaf row (worktree or session)
	m.cursor = tuicomponents.ClampCursor(leafIndices(m.rows), m.cursor)
}

// applyStatuses installs a refreshed worktree list, rebuilds the rows, and
// re-derives the selected worktree's diff. Shared by the statusesMsg (periodic
// refresh, delete-result) and deletedMsg handlers so they can't drift; it does
// not touch m.status, leaving that to the caller (statusesMsg keeps whatever
// status is up; deletedMsg sets a "removed:" confirmation first).
func (m Model) applyStatuses(statuses []worktree.WorktreeStatus) (tea.Model, tea.Cmd) {
	m.statuses = statuses
	m.loaded = true
	m.rebuildRows()
	m.placeCursorOnActive()
	if sel, ok := m.selectedStatus(); ok {
		return m, m.selectionChangedCmd(sel)
	}
	return m, nil
}

// placeCursorOnActive points the cursor at the row for the tmux session
// dg ws is actually running in — the same session it reopens into whether
// launched by typing `dg ws` or via the tmux binding that runs it in a new
// *window* of the current session (new-window never changes session, only
// which window is focused within it). No persistence needed: tmux's own
// session_name is the source of truth for "where the user currently is."
//
// This deliberately does NOT use WorktreeStatus.WindowActive: that field
// means "this worktree's tmux window exists somewhere on the server," true
// for every worktree that's ever been created and not closed — not "this is
// the one I'm in right now." Since worktree rows are listed before session
// rows (see buildRows), the first such row would almost always win, even
// when the user is actually sitting in an unrelated standalone session.
// Comparing against currentSessionFn's actual session name avoids that.
//
// It's a no-op once cursorPlaced is set, so periodic reloads after startup
// never fight the user's own j/k navigation.
//
// It also waits for BOTH initial loads before placing anything. m.cursor is a
// positional index into m.rows, and rebuildRows only clamps it to the nearest
// leaf (ClampCursor) — it cannot re-find the row the index used to mean. Init
// dispatches loadCmd and sessionsLoadCmd concurrently and ListSessions (one
// tmux call) beats List (a filesystem + git scan per repo), so placing on the
// sessions-only row list would land on the right index, then statusesMsg would
// prepend every repo header and worktree row (see buildRows) and slide that
// same index onto a worktree. Waiting until both lists are in means the row
// composition is final when the index is computed. It also subsumes the old
// give-up condition: one attempt happens, against complete rows, and stands.
func (m *Model) placeCursorOnActive() {
	if m.cursorPlaced || !m.loaded || !m.sessionsLoaded {
		return
	}
	// From here this runs exactly once, so it either lands on the current
	// session's row or gives up for good — a missing match means the session
	// genuinely isn't in the dashboard, not that rows are still filling in.
	m.cursorPlaced = true
	current, ok := m.currentSessionFn()
	if !ok {
		return
	}
	for i, r := range m.rows {
		switch {
		case r.kind == rowSession && r.session.Name == current,
			r.kind == rowWorktree && worktree.TmuxSessionName(r.status.Repo) == current:
			m.cursor = i
			return
		}
	}
}

// navigableIndices returns row indices that j/k visit: all worktree rows,
// all session rows, plus collapsed repo header rows (so the user can reach a
// collapsed header and press l).
func (m *Model) navigableIndices() []int {
	var out []int
	for i, r := range m.rows {
		if r.kind == rowWorktree || r.kind == rowSession ||
			(r.kind == rowRepo && m.collapsed[r.repo]) {
			out = append(out, i)
		}
	}
	return out
}

func (m *Model) moveCursor(delta int) {
	m.cursor = tuicomponents.MoveCursor(m.navigableIndices(), m.cursor, delta)
}

func (m Model) safeMaxLeft() int {
	return max(int(float64(m.width)*maxLeftPaneWidthPct), minLeftPaneWidth)
}

func (m Model) rightPaneWidth() int {
	return max(m.width-m.leftPaneWidth-dividerWidth, 0)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.leftPaneWidth = min(m.leftPaneWidth, m.safeMaxLeft())
		return m, nil

	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		if mouse.Button == tea.MouseLeft && mouse.X == m.leftPaneWidth {
			m.dragging = true
			m.dragStartX = mouse.X
		}
		return m, nil

	case tea.MouseMotionMsg:
		if m.dragging {
			mouse := msg.Mouse()
			maxLeft := m.safeMaxLeft()
			newWidth := min(max(mouse.X, minLeftPaneWidth), maxLeft)
			m.leftPaneWidth = newWidth
		}
		return m, nil

	case tea.MouseReleaseMsg:
		m.dragging = false
		return m, nil

	case statusesMsg:
		return m.applyStatuses([]worktree.WorktreeStatus(msg))

	case sessionsMsg:
		// Only reached on a successful ListSessions() (see sessionsLoadCmd);
		// a failure produces a statusMsg instead, which never reaches this
		// case and so never touches m.sessions - see that comment for why.
		m.sessions = []worktree.SessionStatus(msg)
		m.sessionsLoaded = true
		m.rebuildRows()
		m.placeCursorOnActive()
		return m, nil

	case deletedMsg:
		// Replace the transient "deleting…" status with a confirmation, then
		// apply the refreshed (row-dropped) list the same way statusesMsg does.
		m.status = "removed: " + msg.name
		return m.applyStatuses(msg.statuses)

	case diffMsg:
		m.diffContent = msg.content
		m.diffFiles = msg.files
		m.diffAdded = msg.added
		m.diffRemoved = msg.removed
		m.diffFileLines = msg.fileLines
		m.diffBase = msg.base
		m.diffBranch = msg.branch
		m.diffPath = msg.path
		return m, nil

	case prTitleMsg:
		m.prTitles[msg.path] = msg.title
		delete(m.prTitlePending, msg.path)
		return m, nil

	case tickMsg:
		// Periodic refresh: reload statuses (re-deriving the selected
		// worktree's diff via the statusesMsg handler) and sessions, kept in
		// sync on the same 3s cadence.
		return m, tea.Batch(m.loadCmd(), m.sessionsLoadCmd(), m.tickCmd())

	case statusMsg:
		m.status = string(msg)
		return m, nil

	case createdMsg:
		return m.handleCreateSuccess(msg.repoPath, msg.name, msg.warning)

	case createFailedMsg:
		m.creating = false
		m.status = "create failed: " + msg.err.Error()
		return m, nil

	case reviewLaunchedMsg:
		m.reviewLaunching = false
		m.status = string(msg)
		return m, nil

	case sessionCreatedMsg:
		// Only reached for a successful createSessionFn call made outside
		// tmux (see dispatchSessionCreate) - inside tmux, success
		// switches-and-quits directly from that tea.Cmd without ever
		// producing this message.
		m.status = "session created: " + msg.name
		return m, m.sessionsLoadCmd()

	case sessionKilledMsg:
		var updated []worktree.SessionStatus
		for _, s := range m.sessions {
			if s.Name != msg.name {
				updated = append(updated, s)
			}
		}
		m.sessions = updated
		m.rebuildRows()
		m.status = "removed: " + msg.name
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.PasteMsg:
		return m.handlePaste(msg.Content)
	}

	return m, nil
}

// handlePaste routes a bracketed-paste event to whichever text field is
// currently active, mirroring handleKey's mode dispatch (help overlay,
// repo-pick, name-input, filter) but inserting the whole clipboard content
// in one shot rather than one rune at a time. Bubble Tea delivers a paste as
// a single tea.PasteMsg regardless of length, and handleKey's per-key
// handlers only accept single-rune keys — routing it through handleKey
// instead would silently drop all but the paste's first rune. Falls through
// to a no-op everywhere else (diff-focused, plain dashboard), where there is
// no text field for pasted content to go.
func (m Model) handlePaste(text string) (tea.Model, tea.Cmd) {
	if m.showHelp {
		return m, nil
	}

	if m.createMode == createRepoPick {
		m.repoPicker.InsertText(text)
		return m, nil
	}
	if m.createMode == createNameInput {
		return m.handleNameInputPaste(text)
	}
	if m.createMode == createLayoutPick {
		m.layoutPicker.InsertText(text)
		return m, nil
	}
	if m.sessionMode == sessionFolderPick {
		m.sessionFolderPicker.InsertText(text)
		return m, nil
	}
	if m.sessionMode == sessionNameInput {
		return m.handleSessionNameInputPaste(text)
	}

	if m.filter.Active {
		if m.filter.InsertText(text) {
			m.rebuildRows()
		}
		return m, nil
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Help overlay: any key dismisses it
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}

	// Repo-pick and name-input float over the dashboard and intercept every
	// key while active, the same way showHelp does above.
	if m.createMode == createRepoPick {
		return m.handleRepoPickKey(key)
	}
	if m.createMode == createNameInput {
		return m.handleNameInputKey(key)
	}
	if m.createMode == createLayoutPick {
		return m.handleLayoutPickKey(key)
	}
	if m.sessionMode == sessionFolderPick {
		return m.handleSessionFolderPickKey(key)
	}
	if m.sessionMode == sessionNameInput {
		return m.handleSessionNameInputKey(key)
	}
	if m.reviewMode == reviewPick {
		return m.handleReviewPickKey(key)
	}

	if m.filter.Active {
		if m.filter.HandleKey(key) {
			m.rebuildRows()
		}
		return m, nil
	}

	if m.diffFocused {
		return m.handleDiffKey(key)
	}

	// Clear pending deletes on any key that doesn't continue the confirmation
	if key != "d" && m.pendingDelete != "" {
		m.pendingDelete = ""
	}
	if key != "D" && m.pendingSessionDelete != "" {
		m.pendingSessionDelete = ""
	}
	if key != "d" && m.pendingKillSession != "" {
		m.pendingKillSession = ""
	}

	switch key {
	case "?":
		m.showHelp = true
		return m, nil

	case "q", "ctrl+c":
		return m, tea.Quit

	// Arrows are accepted alongside j/k, matching handleDiffKey and the shared
	// fuzzy picker. Safe here in a way it isn't inside the picker or the filter
	// field: this handler only runs when no text input has focus, so there's no
	// query for a bare keystroke to compete with.
	case "j", "down":
		m.diffScroll = 0
		m.moveCursor(1)
		if sel, ok := m.selectedStatus(); ok {
			return m, m.selectionChangedCmd(sel)
		}
		return m, nil

	case "k", "up":
		m.diffScroll = 0
		m.moveCursor(-1)
		if sel, ok := m.selectedStatus(); ok {
			return m, m.selectionChangedCmd(sel)
		}
		return m, nil

	case "h":
		var collapseRepo string
		if sel, ok := m.selectedStatus(); ok {
			collapseRepo = sel.Repo
		} else if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowRepo {
			collapseRepo = m.rows[m.cursor].repo
		}
		if collapseRepo != "" {
			m.collapsed[collapseRepo] = true
			m.rebuildRows()
			// Land cursor on the just-collapsed repo header so l can re-expand it.
			for i, r := range m.rows {
				if r.kind == rowRepo && r.repo == collapseRepo {
					m.cursor = i
					break
				}
			}
		}
		return m, nil

	case "l":
		var expandRepo string
		wasCollapsed := false
		if sel, ok := m.selectedStatus(); ok {
			expandRepo = sel.Repo
			wasCollapsed = m.collapsed[expandRepo]
		} else if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowRepo {
			expandRepo = m.rows[m.cursor].repo
			wasCollapsed = m.collapsed[expandRepo]
		}
		if expandRepo != "" {
			m.collapsed[expandRepo] = false
			m.rebuildRows()
			if wasCollapsed {
				// Move cursor to first worktree of the just-expanded repo.
				for i, r := range m.rows {
					if r.kind == rowWorktree && r.repo == expandRepo {
						m.cursor = i
						break
					}
				}
			}
		}
		return m, nil

	case "z":
		m.allCollapsed = !m.allCollapsed
		for _, s := range m.statuses {
			m.collapsed[s.Repo] = m.allCollapsed
		}
		m.rebuildRows()
		if m.allCollapsed && len(m.rows) > 0 {
			m.cursor = 0 // first visible row is a repo header when all collapsed
		}
		return m, nil

	case "/":
		m.filter.Active = true
		return m, nil

	case "space":
		if m.diffContent != "" {
			m.diffFocused = true
		}
		return m, nil

	case "enter":
		if _, ok := m.selectedSession(); ok {
			return m.handleSwitchToSession()
		}
		return m.handleAttach()

	case "d":
		if _, ok := m.selectedSession(); ok {
			return m.handleKillSession()
		}
		return m.handleDelete()

	case "D":
		return m.handleSessionDelete()

	case "r":
		return m.handleRepair()

	case "R":
		return m.handleKickReview()

	case "s":
		return m.handleNewSession()

	case "n":
		return m.handleNewWorktree()

	case "N":
		return m.handleNewWorktreeWithLayoutPick()

	case "ctrl+d":
		m.diffScroll = min(m.diffScroll+m.diffPageSize(), m.maxDiffScroll())
		return m, nil

	case "ctrl+u":
		m.diffScroll = max(m.diffScroll-m.diffPageSize(), 0)
		return m, nil
	}

	return m, nil
}

// handleDiffKey processes keys while the diff pane is focused: vim-style
// scrolling, [ / ] file jumps, and esc/space to return to the list.
func (m Model) handleDiffKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "esc", "space":
		m.diffFocused = false

	case "?":
		m.showHelp = true

	case "j", "down":
		m.diffScroll = min(m.diffScroll+1, m.maxDiffScroll())

	case "k", "up":
		m.diffScroll = max(m.diffScroll-1, 0)

	case "ctrl+d":
		m.diffScroll = min(m.diffScroll+m.diffPageSize(), m.maxDiffScroll())

	case "ctrl+u":
		m.diffScroll = max(m.diffScroll-m.diffPageSize(), 0)

	case "g":
		m.diffScroll = 0

	case "G":
		m.diffScroll = m.maxDiffScroll()

	case "]":
		for _, ln := range m.diffFileLines {
			if ln > m.diffScroll {
				m.diffScroll = min(ln, m.maxDiffScroll())
				break
			}
		}

	case "[":
		for i := len(m.diffFileLines) - 1; i >= 0; i-- {
			if m.diffFileLines[i] < m.diffScroll {
				m.diffScroll = m.diffFileLines[i]
				break
			}
		}
	}
	return m, nil
}

func (m Model) diffPageSize() int {
	return max(m.height-4, 1)
}

func (m Model) maxDiffScroll() int {
	return max(strings.Count(m.diffContent, "\n"), 0)
}

func (m Model) handleAttach() (tea.Model, tea.Cmd) {
	sel, ok := m.selectedStatus()
	if !ok {
		return m, nil
	}

	if os.Getenv("TMUX") == "" {
		m.status = notInsideTmuxStatus
		return m, nil
	}

	windowName := worktree.GetWindowName(sel.Repo, sel.Name)

	// A missing window makes attachToWindowCmd auto-repair — rebuilding the tmux
	// window, as slow as a create — before it attaches. Surface the same
	// "repairing…" feedback the r keybinding shows so the enter keypress isn't
	// silent while that happens. A present window attaches instantly and quits,
	// so it needs no status. This re-checks the window (the cmd checks again in
	// its goroutine): a cheap read-only tmux lookup, kept here rather than
	// restructuring the shared cmd the create-success path also uses.
	if _, ok := m.windowSessionFn(windowName); !ok {
		m.status = layoutActionStatus("repairing", sel.Name, "", m.gc)
	}
	// Attaching is the user acknowledging this row's state - clear it now so
	// the window you're about to sit in doesn't keep showing a stale ◆/!/✕.
	// No-op when the window doesn't exist yet. Best-effort: a failed clear
	// is cosmetic (the dot stays stale until the next real state write), it
	// must never block the attach itself.
	_ = m.clearAgentStateFn(windowName)
	return m, m.attachToWindowCmd(sel.Repo, sel.Name)
}

// attachToWindowCmd looks up repo/name's tmux window (auto-repairing it
// first if the window is missing) and attaches, quitting the TUI on
// success. Extracted from handleAttach so the create flow can reuse the
// identical retry/auto-repair logic for a worktree that was just created and
// isn't in m.statuses yet (selectedStatus can't provide it there), instead
// of duplicating this logic at a second call site.
func (m Model) attachToWindowCmd(repo, name string) tea.Cmd {
	window := worktree.GetWindowName(repo, name)
	attachFn := m.attachFn
	repairFn := m.repairFn
	windowSessionFn := m.windowSessionFn
	gc := m.gc

	return func() tea.Msg {
		session, ok := windowSessionFn(window)
		if ok {
			if err := attachFn(session, window); err != nil {
				return statusMsg("attach failed: " + err.Error())
			}
			return tea.QuitMsg{}
		}
		// Auto-repair: window missing. No --layout flag/picker reaches this
		// path (that's a later step), so layoutName and aiAlias are both ""
		// here, letting ResolveLayout rebuild gc.Worktree.DefaultLayout when
		// set, else derive from gc.Worktree.DefaultAI, else opencode - see
		// ResolveLayout's doc comment for why the folded ResolveAIAlias
		// output must NOT be passed here instead.
		layout, err := worktree.ResolveLayout("", "", gc)
		if err != nil {
			return statusMsg("repair failed: " + err.Error())
		}
		if err := repairFn(repo, name, layout); err != nil {
			return statusMsg("repair failed: " + err.Error())
		}
		session, ok = windowSessionFn(window)
		if !ok {
			return statusMsg("repair succeeded but window not found")
		}
		if err := attachFn(session, window); err != nil {
			return statusMsg("attach after repair failed: " + err.Error())
		}
		return tea.QuitMsg{}
	}
}

// confirmThenRemove implements the shared two-press delete confirmation.
// pending is the currently armed "repo/name" key (or ""); remove performs the
// removal on the second press. It returns the new pending value and, on
// confirmation, a command that runs the removal and refreshes the list.
func (m Model) confirmThenRemove(
	pending string,
	remove func(repo, name string) error,
) (string, tea.Cmd) {
	sel, ok := m.selectedStatus()
	if !ok {
		return pending, nil
	}

	key := sel.Repo + "/" + sel.Name

	// First press (or cursor moved to another row): arm
	if pending != key {
		return key, nil
	}

	// Second press: execute
	repo := sel.Repo
	name := sel.Name
	statuses := m.statuses
	return "", func() tea.Msg {
		if err := remove(repo, name); err != nil {
			return statusMsg("delete failed: " + err.Error())
		}
		// Drop from statuses
		var updated []worktree.WorktreeStatus
		for _, s := range statuses {
			if s.Repo != repo || s.Name != name {
				updated = append(updated, s)
			}
		}
		// deletedMsg (not statusesMsg) so the "deleting…" status is replaced
		// with a "removed:" confirmation, not left lingering on the refreshed list.
		return deletedMsg{name: name, statuses: updated}
	}
}

func (m Model) handleDelete() (tea.Model, tea.Cmd) {
	removeFn := m.removeFn
	pending, cmd := m.confirmThenRemove(m.pendingDelete, func(repo, name string) error {
		return removeFn(repo, name, true)
	})
	m.pendingDelete = pending
	// cmd is non-nil only on the confirming (second) press, i.e. when the
	// removal actually runs - so the "deleting…" status appears then, never on
	// the first (arming) press. Superseded by the refreshed list / delete-failed
	// status the moment it resolves, same as the create flow's status.
	if cmd != nil {
		if sel, ok := m.selectedStatus(); ok {
			m.status = actionStatus("deleting", sel.Name)
		}
	}
	return m, cmd
}

func (m Model) handleSessionDelete() (tea.Model, tea.Cmd) {
	pending, cmd := m.confirmThenRemove(m.pendingSessionDelete, m.removeSessionFn)
	m.pendingSessionDelete = pending
	if cmd != nil {
		if sel, ok := m.selectedStatus(); ok {
			m.status = actionStatus("deleting + session", sel.Name)
		}
	}
	return m, cmd
}

func (m Model) handleRepair() (tea.Model, tea.Cmd) {
	sel, ok := m.selectedStatus()
	if !ok {
		return m, nil
	}
	repairFn := m.repairFn
	gc := m.gc
	repo := sel.Repo
	name := sel.Name
	// Repair rebuilds a tmux window (same cost as create), so show the same
	// in-progress feedback naming the layout it will rebuild ("" resolves the
	// configured default, matching the ResolveLayout call below). Superseded by
	// "repaired:"/"repair failed:" when the async work resolves.
	m.status = layoutActionStatus("repairing", name, "", gc)
	return m, func() tea.Msg {
		// Same no-flags-given reasoning as attachToWindowCmd's auto-repair:
		// "" for both layoutName and aiAlias lets ResolveLayout honor
		// gc.Worktree.DefaultLayout, then gc.Worktree.DefaultAI, then opencode.
		layout, err := worktree.ResolveLayout("", "", gc)
		if err != nil {
			return statusMsg("repair failed: " + err.Error())
		}
		if err := repairFn(repo, name, layout); err != nil {
			return statusMsg("repair failed: " + err.Error())
		}
		return statusMsg("repaired: " + name)
	}
}

// View implements tea.Model.
func (m Model) View() tea.View {
	content := m.renderContent()
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) renderContent() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	background := m.renderDashboard()
	if m.showHelp {
		return tuicomponents.Overlay(background, m.renderHelpPopup(), m.width, m.height)
	}
	if m.createMode == createRepoPick {
		return tuicomponents.Overlay(background, m.renderRepoPickPopup(), m.width, m.height)
	}
	if m.createMode == createNameInput {
		return tuicomponents.Overlay(background, m.renderNameInputPopup(), m.width, m.height)
	}
	if m.createMode == createLayoutPick {
		return tuicomponents.Overlay(background, m.renderLayoutPickPopup(), m.width, m.height)
	}
	if m.sessionMode == sessionFolderPick {
		return tuicomponents.Overlay(
			background,
			m.renderSessionFolderPickPopup(),
			m.width,
			m.height,
		)
	}
	if m.sessionMode == sessionNameInput {
		return tuicomponents.Overlay(background, m.renderSessionNameInputPopup(), m.width, m.height)
	}
	if m.reviewMode == reviewPick {
		return tuicomponents.Overlay(background, m.renderReviewPickPopup(), m.width, m.height)
	}
	return background
}

// renderDashboard renders the normal (non-help) dashboard: narrow-terminal
// fallback or the left+divider+right layout, plus hint and status lines. It
// always runs, even while the help popup is shown, so renderContent has a
// live background to composite the popup over instead of blanking the screen.
func (m Model) renderDashboard() string {
	rpw := m.rightPaneWidth()
	lpw := m.leftPaneWidth

	// Narrow terminal fallback
	if rpw <= 0 {
		left := m.renderLeft(m.width - 1)
		hint := m.renderHint(m.width)
		status := m.renderStatus(m.width)
		lines := strings.Split(left, "\n")
		// Trim to height
		maxLines := max(m.height-2, 0)
		if len(lines) > maxLines {
			lines = lines[:maxLines]
		}
		return strings.Join(lines, "\n") + "\n" + hint + "\n" + status
	}

	left := m.renderLeft(lpw)
	divider := m.renderDivider(m.height - 2)
	right := m.renderRight(rpw)
	hint := m.renderHint(m.width)
	status := m.renderStatus(m.width)

	// Join left + divider + right horizontally
	leftLines := padLines(strings.Split(left, "\n"), m.height-2, lpw)
	rightLines := padLines(strings.Split(right, "\n"), m.height-2, rpw)
	divLines := strings.Split(divider, "\n")

	var combined []string
	for i := range m.height - 2 {
		l := ""
		if i < len(leftLines) {
			l = leftLines[i]
		}
		d := ""
		if i < len(divLines) {
			d = divLines[i]
		}
		r := ""
		if i < len(rightLines) {
			r = rightLines[i]
		}
		combined = append(combined, l+d+r)
	}

	body := strings.Join(combined, "\n")
	return body + "\n" + hint + "\n" + status
}

func (m Model) renderLeft(width int) string {
	// isLastChild reports whether row i is the last worktree under its repo header.
	isLastChild := func(i int) bool {
		repo := m.rows[i].status.Repo
		for j := i + 1; j < len(m.rows); j++ {
			if m.rows[j].kind == rowRepo {
				return true
			}
			if m.rows[j].kind == rowWorktree && m.rows[j].status.Repo == repo {
				return false
			}
		}
		return true
	}

	const branchChar = "∕" // U+2215 DIVISION SLASH — branch glyph (1 display col)

	var sb strings.Builder
	for i, r := range m.rows {
		var line string
		if r.kind == rowRepo {
			collapse := "▼"
			if m.collapsed[r.repo] {
				collapse = "▶"
			}
			text := collapse + " " + r.repo
			// Right-aligned "N trees"/"1 tree" badge. Truncate the header text
			// first (leaving room for at least one separating space) so the
			// badge is never pushed past width, then pad the remainder — the
			// same fixed-width layout the rowWorktree branch below uses.
			badge := fmt.Sprintf("%d trees", r.worktreeCount)
			if r.worktreeCount == 1 {
				badge = "1 tree"
			}
			badgeW := ansi.StringWidth(badge)
			text = ansi.Truncate(text, max(0, width-badgeW-1), "")
			pad := strings.Repeat(" ", max(0, width-ansi.StringWidth(text)-badgeW))
			if i == m.cursor {
				// Cursor landed here after h — show repo header with selection highlight.
				line = m.palette.Selected.Render(text + pad + badge)
			} else {
				line = m.palette.RepoHeader.Render(
					text,
				) + pad + m.palette.HintDesc.Render(
					badge,
				)
			}
		} else if r.kind == rowSession {
			const label = "session"
			labelW := ansi.StringWidth(label)
			// prefix = square(1) + space(1) = 2 display cols — same width as the
			// "▼ "/"▶ " chevron above, so session rows (flat top-level leaves,
			// no tree connector) line up with repo headers in the left column.
			// The square (■/□) is a different shape from the worktree ●/○ circle
			// so the two row kinds are distinguishable at a glance, not just by
			// the trailing "session" label.
			name := ansi.Truncate(r.session.Name, max(0, width-2-labelW-1), "")
			pad := strings.Repeat(" ", max(0, width-2-ansi.StringWidth(name)-labelW))

			if i == m.cursor {
				g := m.palette.SessionGlyph(r.session.Attached)
				plainText := g + " " + name
				if m.pendingKillSession == r.session.Name {
					line = m.palette.Armed.Render(plainText + pad + label)
				} else {
					line = m.palette.Selected.Render(plainText + pad + label)
				}
			} else {
				line = m.palette.SessionDot(
					r.session.Attached,
				) + " " + name + pad + m.palette.HintDesc.Render(
					label,
				)
			}
		} else {
			state := tuicomponents.SessionStateFromWorktree(r.status, r.status.AgentState, 0)
			// Tree connector: "└ " for last child, "  " otherwise (both 2 display cols).
			connectorRaw := "  "
			connectorStyled := "  "
			if isLastChild(i) {
				connectorRaw = "└ "
				connectorStyled = m.palette.Divider.Render("└") + " "
			}
			// prefix = connector(2) + dot(1) + branchChar(1) + space(1) = 5 display cols
			name := ansi.Truncate(r.status.Name, max(0, width-5), "")
			pendingKey := r.status.Repo + "/" + r.status.Name
			padding := strings.Repeat(" ", max(0, width-5-ansi.StringWidth(name)))

			if i == m.cursor {
				g := m.palette.StatusGlyph(state)
				plainText := connectorRaw + g + branchChar + " " + name
				if m.pendingDelete == pendingKey || m.pendingSessionDelete == pendingKey {
					line = m.palette.Armed.Render(plainText + padding)
				} else {
					line = m.palette.Selected.Render(plainText + padding)
				}
			} else {
				line = connectorStyled + m.palette.StatusDot(
					state,
				) + m.palette.BranchLabel() + " " + name + padding
			}
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (m Model) renderDivider(height int) string {
	style := m.palette.Divider
	if m.diffFocused {
		// Brighter divider signals the diff pane holds keyboard focus.
		style = m.palette.HintKey
	}
	divChar := style.Render("│")
	lines := make([]string, height)
	for i := range lines {
		lines[i] = divChar
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderRight(width int) string {
	// Empty dashboard: once the first List() is in and there are no worktrees,
	// there is nothing to diff, so show guidance instead of the "(loading...)"
	// that renderDiffContent would otherwise display forever (it only ever
	// clears when a worktree row is selected, which can't happen here).
	if m.loaded && len(m.statuses) == 0 {
		return m.palette.Inactive.Render(
			ansi.Truncate("No worktrees yet — press n to create one.", width, ""),
		)
	}

	// Session rows have no diff: selectedStatus (and so selectionChangedCmd)
	// never fires for them, so without this check the pane would keep
	// showing whichever worktree's diff was selected last instead of
	// something that reflects the current row.
	if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowSession {
		return m.palette.Inactive.Render(
			ansi.Truncate("Sessions have no diff — enter switches, d d kills.", width, ""),
		)
	}

	header := m.palette.DiffStatLine(m.diffFiles, m.diffAdded, m.diffRemoved)
	// GitHub-style "base ← compare" label, shown once for the whole diff.
	if m.diffBase != "" && m.diffBranch != "" {
		header = m.palette.RepoHeader.Render(m.diffBase) +
			m.palette.Divider.Render(" ← ") +
			m.palette.DiffFileHeader.Render(m.diffBranch) +
			"  " + header
	}

	// PR title, when known, is its own line above the base/branch header.
	// Keyed by diffPath, not diffBranch: branch names can collide across
	// repos, but the worktree path is unique (see maybePRTitleCmd).
	title := m.prTitles[m.diffPath]
	extraLines := 0
	if title != "" {
		extraLines = 1
	}

	contentHeight := max(
		m.height-4-extraLines,
		0,
	) // height minus hint, status, header, blank line (and title line, if shown)
	content := m.renderDiffContent(width, contentHeight)

	out := ansi.Truncate(header, width, "") + "\n" + content
	if title != "" {
		out = ansi.Truncate(m.palette.DiffFileHeader.Render(title), width, "") + "\n" + out
	}
	return out
}

func (m Model) renderDiffContent(width, height int) string {
	if m.diffContent == "" {
		return m.palette.Inactive.Render("(loading...)")
	}
	lines := strings.Split(m.diffContent, "\n")
	// Apply scroll
	start := min(max(m.diffScroll, 0), len(lines)-1)
	end := min(start+height, len(lines))
	visible := lines[start:end]
	var truncated []string
	for _, line := range visible {
		truncated = append(truncated, ansi.Truncate(line, width, ""))
	}
	return strings.Join(truncated, "\n")
}

// armedDeleteHint renders the confirmation hint for a two-press delete/kill.
// pending is the armed key — a "repo/name" worktree key, or a bare session
// name (strings.SplitN degrades gracefully to len(parts)==1, falling back to
// the full pending string) — key is the keypress to repeat, verb is "delete"
// or "kill", and suffix an optional description of extra effects.
func (m Model) armedDeleteHint(pending, key, verb, suffix string, width int) string {
	parts := strings.SplitN(pending, "/", 2)
	name := pending
	if len(parts) == 2 {
		name = parts[1]
	}
	hint := "press " + key + " again to " + verb + " " + name + suffix + " · any other key cancels"
	return m.palette.HintDesc.Render(ansi.Truncate(hint, width, ""))
}

func (m Model) renderHint(width int) string {
	if m.pendingKillSession != "" {
		return m.armedDeleteHint(m.pendingKillSession, "d", "kill", "", width)
	}
	if m.pendingSessionDelete != "" {
		return m.armedDeleteHint(
			m.pendingSessionDelete,
			"D",
			"delete",
			" and kill its session",
			width,
		)
	}
	if m.pendingDelete != "" {
		return m.armedDeleteHint(m.pendingDelete, "d", "delete", "", width)
	}
	// createRepoPick, createLayoutPick, and reviewPick are all plain
	// FuzzyPicker interactions (list nav + select + cancel), so they share
	// one hint set instead of copy-pasted literals.
	if m.createMode == createRepoPick || m.createMode == createLayoutPick ||
		m.reviewMode == reviewPick {
		hints := []tuicomponents.KeyHint{
			{Key: "esc", Desc: "cancel"},
			{Key: "enter", Desc: "select"},
			{Key: "↑/↓", Desc: "move"},
		}
		return m.palette.HintBar(hints, width)
	}
	if m.createMode == createNameInput {
		hints := []tuicomponents.KeyHint{
			{Key: "esc", Desc: "cancel"},
			{Key: "enter", Desc: "create"},
		}
		return m.palette.HintBar(hints, width)
	}
	if m.filter.Active {
		return m.palette.FilterHint(m.filter, width)
	}
	if m.diffFocused {
		hints := []tuicomponents.KeyHint{
			{Key: "esc", Desc: "back"},
			{Key: "j/k", Desc: "scroll"},
			{Key: "^d/^u", Desc: "page"},
			{Key: "[/]", Desc: "file"},
			{Key: "g/G", Desc: "top/end"},
			{Key: "?", Desc: "help"},
			{Key: "q", Desc: "quit"},
		}
		return m.palette.HintBar(hints, width)
	}
	// "d" stays one generic "del" entry rather than a row-kind-aware pair
	// ("del worktree" vs "del session"): the hint bar already documents d/D/r
	// once each regardless of row-kind nuance (e.g. "D" doesn't clarify it
	// only applies to worktree rows either), and the armed-kill/armed-delete
	// hints above already disambiguate the moment a press actually arms —
	// splitting "d" into two entries here would add width for a distinction
	// the help popup (which has room) already covers.
	hints := []tuicomponents.KeyHint{
		{Key: "↵", Desc: "attach"},
		{Key: "n", Desc: "new"},
		{Key: "N", Desc: "new w/ layout"},
		{Key: "s", Desc: "new session"},
		{Key: "spc", Desc: "diff"},
		{Key: "j/k", Desc: "move"},
		{Key: "h/l", Desc: "fold"},
		{Key: "z", Desc: "all"},
		{Key: "d", Desc: "del"},
		{Key: "D", Desc: "del+sess"},
		{Key: "r", Desc: "repair"},
		{Key: "R", Desc: "review"},
		{Key: "/", Desc: "filter"},
		{Key: "?", Desc: "help"},
		{Key: "q", Desc: "quit"},
	}
	return m.palette.HintBar(hints, width)
}

func (m Model) renderStatus(width int) string {
	if m.status == "" {
		return ""
	}
	return m.palette.StatusMsg.Render(ansi.Truncate(m.status, width, ""))
}

// renderHelpPopup builds the raw (uncentered) help popup content; the
// caller composites it over the dashboard background via Overlay.
func (m Model) renderHelpPopup() string {
	entries := []tuicomponents.WhichKeyEntry{
		{
			Key:  "enter",
			Desc: "attach (auto-repairs missing window); on a session row: switch to it",
		},
		{Key: "n", Desc: "create a new worktree (repo picker → name prompt)"},
		{Key: "N", Desc: "create a new worktree (repo picker → name prompt → layout picker)"},
		{Key: "s", Desc: "create a new tmux session (folder picker → name prompt)"},
		{Key: "j / k  ↓ / ↑", Desc: "move cursor down / up"},
		{Key: "h / l", Desc: "collapse / expand repo"},
		{Key: "z", Desc: "toggle collapse all repos"},
		{
			Key:  "d d",
			Desc: "delete worktree (confirm twice); on a session row: kill it (confirm twice)",
		},
		{Key: "D D", Desc: "delete worktree + kill its session"},
		{Key: "r", Desc: "repair (recreate window + relaunch AI)"},
		{Key: "R", Desc: "kick a review (picker: code / document / skill)"},
		{Key: "/", Desc: "filter  esc:clear  enter:keep"},
		{Key: "space", Desc: "focus diff pane (esc returns to the list)"},
		{Key: "ctrl+d / ctrl+u", Desc: "scroll diff down / up"},
		{Key: "[ / ]", Desc: "previous / next file (diff focused)"},
		{Key: "g / G", Desc: "diff top / bottom (diff focused)"},
		{Key: "?", Desc: "toggle this help"},
		{Key: "q / ctrl+c", Desc: "quit"},
	}
	return m.palette.HelpPopup("Keybindings", entries, m.width)
}

// padLines ensures a slice of lines has exactly n entries, each padded/truncated to w visible chars.
// Uses ansi.StringWidth so ANSI escape codes are not counted as visible characters.
func padLines(lines []string, n, w int) []string {
	blank := strings.Repeat(" ", w)
	result := make([]string, n)
	for i := range n {
		if i < len(lines) {
			t := ansi.Truncate(lines[i], w, "")
			vis := ansi.StringWidth(t)
			if vis < w {
				t += strings.Repeat(" ", w-vis)
			}
			result[i] = t
		} else {
			result[i] = blank
		}
	}
	return result
}
