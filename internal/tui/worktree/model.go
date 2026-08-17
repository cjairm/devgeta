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
	"github.com/cjairm/devgeta/pkg/logger"
)

const (
	minLeftPaneWidth     = 20
	defaultLeftPaneWidth = 35
	maxLeftPaneWidthPct  = 0.60
	dividerWidth         = 1
	// fastRefreshInterval and slowRefreshInterval are ADR-0024's cadence split:
	// tmux-derived state (agent dots, window presence, pane rows, the
	// standalone-session list) is cheap and fast-moving, so it refreshes every
	// 3 seconds; the git enumeration behind the worktree rows costs one process
	// per known repo and changes rarely, so it refreshes every 30 seconds —
	// plus immediately after any create, remove, or repair the dashboard itself
	// performs, so your own actions never wait on the timer.
	fastRefreshInterval = 3 * time.Second
	slowRefreshInterval = 30 * time.Second
	// diffDebounceInterval is ADR-0024 §3's navigation debounce: a selection
	// change arms a timer instead of computing the diff straight away, and only
	// the last change to survive the interval actually spends the five git
	// processes a branch diff costs. Holding j through fifteen rows therefore
	// costs one diff, not fifteen. 180 ms sits below the threshold at which the
	// pane reads as lagging a deliberate single keypress.
	diffDebounceInterval = 180 * time.Millisecond
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
	// statusesMsg carries one slow (git-backed) load's whole worktree list,
	// stamped with the stateGen the load was dispatched with. The stamp is what
	// lets Update drop a snapshot that a newer load or a mutation has already
	// superseded; see the stateGen field for the races it closes.
	statusesMsg struct {
		statuses []worktree.WorktreeStatus
		gen      int
	}
	// sessionsMsg carries a successful whole-session-list result, stamped with
	// the sessionGen it was dispatched with. Its only producer is
	// sessionsLoadCmd; see sessionsLoadCmd for the success/failure split.
	sessionsMsg struct {
		sessions []worktree.SessionStatus
		gen      int
	}
	// tmuxStateMsg is the fast tick's result: one tmux scan's worth of state,
	// carried as a layer rather than as a finished list.
	//
	// The two halves are applied differently on purpose. The pane half is a
	// layer, so Update applies it to whatever m.statuses holds when the message
	// lands and needs no stamp — no model-owned memory ever crossed into the
	// command goroutine, so there is nothing to race. The session half IS a
	// wholesale replacement of m.sessions, so it is gated on gen the same way
	// statusesMsg is gated on its own.
	tmuxStateMsg struct {
		layer worktree.StateLayer
		gen   int
	}
	diffMsg struct {
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
	// diffDebounceMsg is the navigation debounce firing. It carries the diffGen
	// its timer was armed with, so a later selection change (which bumps
	// diffGen) invalidates every timer already ticking: only the last one still
	// matching gets to spend a diff.
	diffDebounceMsg struct{ gen int }
)

type (
	// fastTickMsg drives the 3-second tmux refresh, slowTickMsg the 30-second
	// git one. Two timers rather than one with a counter: each re-arms itself
	// from its own handler, so neither can drift into the other's cadence.
	fastTickMsg time.Time
	slowTickMsg time.Time
	statusMsg   string
)

// deletedMsg reports a successful removal so Update can both drop the removed
// row and replace the transient "deleting…" status with a "removed:"
// confirmation. A plain statusesMsg would refresh the list but leave the
// "deleting…" status lingering, since its handler never touches m.status.
//
// It carries only the identity it removed, never a list. Two removals can be in
// flight at once — nothing marks one as in progress, so `d` `d` `j` `d` `d`
// dispatches a second `git worktree remove` while the first still runs — and if
// each command returned its own row-dropped copy of m.statuses, the one that
// landed second would win with a list that still held the other's row. Removing
// by identity from whatever m.statuses holds when the message lands makes the
// order between them irrelevant, and keeps model-owned memory out of the
// command goroutine. It is the same shape sessionKilledMsg already uses for
// m.sessions.
type deletedMsg struct {
	repo string
	name string
}

// repairDoneMsg reports a finished repair — succeeded or failed — so its
// handler can set the status AND dispatch the slow git load. A plain statusMsg
// only sets the status, which is why repair used to depend entirely on the
// periodic tick to show its effect. That is fine for a success (everything a
// successful repair changes is tmux-side, so the fast tick covers it) but not
// for a failure: repairing a worktree whose directory is gone prunes the stale
// git entry and then returns an error, so the row it just removed would sit on
// screen until the next slow tick. Both outcomes therefore carry this message.
type repairDoneMsg struct {
	status string
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

	// stateGen orders the wholesale replacements of m.statuses against each
	// other. Update bumps it whenever it dispatches a slow load, the load
	// carries that number on its statusesMsg, and the handler drops any message
	// whose number is no longer current. Without it the older of two overlapping
	// loads wins: the 30s timer's load starts, you press n, the create's own
	// load lands with the new worktree, then the timer's older git snapshot
	// lands and replaces the list without it — and the worktree you just made
	// disappears for another 30 seconds.
	//
	// Mutation results (deletedMsg, createdMsg, repairDoneMsg) bump it when they
	// apply rather than carrying a captured number, so they always win and
	// invalidate every load in flight. A captured number would not be enough:
	// a load dispatched after a delete began can still finish before the removal
	// does, see the worktree, and hold a higher number than the deletedMsg that
	// follows — which would put the deleted row back.
	//
	// It orders mutations against loads only. Two mutations against each other
	// are handled at the payload instead — see deletedMsg.
	stateGen int
	// sessionGen is stateGen's counterpart for m.sessions, and deliberately not
	// symmetric with it. Update bumps it whenever it dispatches something that
	// returns a WHOLE session list; two producers do (the fast tick's
	// tmuxStateMsg and sessionsLoadCmd), and the slow load is deliberately not a
	// third — it discards RefreshState's session half, since the fast tick
	// already replaces m.sessions every 3 seconds.
	//
	// A separate counter, not a shared one: the two lists have independent
	// producers, so one counter would let a fast tmux scan's dispatch invalidate
	// an in-flight slow git load. A fast dispatch bumps sessionGen and leaves
	// stateGen untouched, which is the whole reason there are two.
	//
	// Worth being honest about the size of what it buys: unlike the m.statuses
	// races, a resurrected session row is cleared by the next fast tick either
	// way, so this stops a flicker rather than a 30-second stall. It is still
	// worth having, because m.sessions is otherwise the one wholesale-replaced
	// list in the model with no ordering rule at all.
	sessionGen int
	// diffGen orders the navigation debounce's timers against each other, and
	// is a third counter rather than a reuse of either of the two above: those
	// order wholesale replacements of a list, this one orders pending diffs, and
	// folding them together would let a 3-second tmux scan cancel a diff the
	// user's last keypress asked for. Update bumps it every time it arms a
	// debounce; the timer carries the number it was armed with, and its handler
	// only spends a diff while that number is still current.
	diffGen int
	// forceDiff asks the Update wrapper to recompute the diff even though the
	// selection did not change. The wrapper is the single dispatcher of diff
	// work (see Update), and it fires on a CHANGED selection — so the two paths
	// ADR-0024 §3 requires to refresh an unchanged one, the slow git load and
	// the explicit ctrl+r, raise this flag instead of dispatching for
	// themselves. The wrapper clears it as it consumes it.
	forceDiff bool

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
	// leftPaneWide tracks the e-toggle's own state, independent of
	// leftPaneWidth's actual value — a mouse drag can leave leftPaneWidth at
	// an arbitrary width, so the toggle can't just compare it against
	// defaultLeftPaneWidth to know which way to flip. See leftPaneTarget.
	leftPaneWide bool

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
	switchToPaneFn           func(session, window, paneID string) error
	clearAgentStateForPaneFn func(paneID string) error
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
	m.switchToPaneFn = tmuxApp.SwitchToPane
	m.clearAgentStateForPaneFn = tmuxApp.ClearAgentStateForPane
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
		// The warn sink fires synchronously from inside CreateAt below — from
		// the manager (e.g. the recent-repos store failed to record this
		// create) and from the git app underneath it (e.g. the branch
		// diverged from origin and was not fast-forwarded). SetWarnFn swaps
		// both in one call, so neither can fall through to a raw stdout print
		// that would scribble over this alt-screen. Swapping to a local
		// closure and restoring it via defer right after CreateAt returns is
		// safe only because this TUI never runs two creates concurrently —
		// and that's actually true, not just assumed: this tea.Cmd closure
		// only ever runs while m.creating is true, and handleNewWorktree (the
		// only way to start another create) refuses to open the picker while
		// m.creating is true, so a second createFn call can never be in
		// flight to race this swap/restore.
		// Accumulate, never overwrite: one CreateAt can raise several
		// independent advisories in sequence — git frees a branch held by
		// the source checkout AND then finds that branch diverged from
		// origin, and the manager can separately fail to record the repo as
		// recently used. Keeping only the last one silently dropped the
		// others, including the "your source checkout was moved to <default
		// branch>" notice, which changes state the user needs to know about.
		//
		// Joined with " · " rather than "\n" on purpose: this string becomes
		// m.status, and the dashboard budgets exactly m.height lines. Every
		// embedded newline is an extra terminal row, which scrolls the frame
		// and leaves the previous frame's rows interleaved with the new ones
		// — the same corruption this cycle set out to fix. renderStatus
		// enforces that invariant for every status source; this just avoids
		// creating work for it.
		var warnings []string
		original := mgr.WarnFn
		mgr.SetWarnFn(func(msg string) { warnings = append(warnings, msg) })
		defer func() { mgr.SetWarnFn(original) }()
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
		return strings.Join(warnings, " · "), nil
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
//
// Both loads run once up front — the slow one for the worktree rows, the
// session one so the dashboard has sessions before the first fast tick fires
// three seconds later — and both timers start. Neither load needs a special
// generation: they capture the zero value, which stays current until the first
// dispatch bumps a counter.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadCmd(m.stateGen),
		m.sessionsLoadCmd(m.sessionGen),
		m.fastTickCmd(),
		m.slowTickCmd(),
	)
}

// loadCmd is the slow, git-backed half of ADR-0024's split: it enumerates
// worktrees (one git process per known repo) and layers one tmux scan onto that
// fresh slice so the rows arrive complete rather than briefly tmux-blank.
//
// gen is the stateGen the caller bumped to before dispatching; the resulting
// statusesMsg carries it back so Update can drop a snapshot that a newer load
// or a mutation has already superseded.
//
// It enumerates into its OWN slice and never reads m.statuses, so no
// model-owned memory crosses into this goroutine. RefreshState's session half
// is discarded on purpose: the fast tick is m.sessions' producer, and a second
// one would only add an ordering case (see the sessionGen field).
func (m Model) loadCmd(gen int) tea.Cmd {
	mgr := m.mgr
	return func() tea.Msg {
		applied, err := mgr.List()
		if err != nil {
			return statusMsg("failed to list worktrees: " + err.Error())
		}
		return statusesMsg{statuses: applied, gen: gen}
	}
}

// scanTmuxCmd is the fast, tmux-only half of ADR-0024's split: one
// `tmux list-sessions` plus one `tmux list-panes -a`, no git at all.
//
// It returns the raw layer rather than a finished list, which is the ownership
// rule this whole step turns on: a fast command that captured m.statuses and
// returned a whole list would both race the renderer and overwrite a newer list
// from the slow path, so a worktree you just created could vanish for 30
// seconds. Update applies the layer to whatever m.statuses holds when the
// message lands, on the single goroutine Bubble Tea runs Update on.
//
// gen is the sessionGen the caller bumped to; only the message's session half
// is gated on it (see tmuxStateMsg).
func (m Model) scanTmuxCmd(gen int) tea.Cmd {
	mgr := m.mgr
	return func() tea.Msg {
		layer, err := mgr.ScanTmuxState()
		if err != nil {
			return statusMsg("failed to list sessions: " + err.Error())
		}
		return tmuxStateMsg{layer: layer, gen: gen}
	}
}

// sessionsLoadCmd fetches standalone tmux sessions on their own. The fast tick
// is the routine producer of m.sessions; this is the immediate refresh a
// session mutation needs so it doesn't wait up to 3 seconds for that tick, and
// the one that fills the list at startup.
//
// On a real error, this returns a statusMsg (not a sessionsMsg) exactly like
// loadCmd does above - the statusMsg case only sets m.status, so m.sessions
// is never touched and the last-good session rows keep rendering. A genuinely
// empty result (WorktreeManager.ListSessions' no-server case is (nil, nil))
// is not an error: it produces a sessionsMsg, which the Update case below
// applies with no status warning.
func (m Model) sessionsLoadCmd(gen int) tea.Cmd {
	mgr := m.mgr
	return func() tea.Msg {
		sessions, err := mgr.ListSessions()
		if err != nil {
			return statusMsg("failed to list sessions: " + err.Error())
		}
		return sessionsMsg{sessions: sessions, gen: gen}
	}
}

func (m Model) fastTickCmd() tea.Cmd {
	return tea.Tick(fastRefreshInterval, func(t time.Time) tea.Msg {
		return fastTickMsg(t)
	})
}

func (m Model) slowTickCmd() tea.Cmd {
	return tea.Tick(slowRefreshInterval, func(t time.Time) tea.Msg {
		return slowTickMsg(t)
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

// selectionChangedCmd builds the batch of commands that give the diff pane its
// contents: recompute the diff, and additionally kick off a PR-title lookup
// when s's path has no cache entry and none is pending. Pointer receiver is
// required so maybePRTitleCmd's pending-flag mutation lands on the addressable
// local m in its call site.
//
// Its one caller is the diffDebounceMsg handler. Everything that wants a diff
// goes through the debounce (see Update), so this is never dispatched straight
// off a keypress or a load.
func (m *Model) selectionChangedCmd(sel worktree.WorktreeStatus) tea.Cmd {
	cmds := []tea.Cmd{m.computeDiffCmd(sel)}
	if c := m.maybePRTitleCmd(sel); c != nil {
		cmds = append(cmds, c)
	}
	return tea.Batch(cmds...)
}

// selectedPath is the identity of what the diff pane is currently about: the
// selected worktree's path, or "" when the cursor sits on anything that has no
// diff (a repo header, a session, a pane). Update compares it either side of a
// handler to notice a selection change without enumerating the keys that cause
// one, and the diffMsg handler compares an arriving diff against it to drop one
// that belongs to a row the user has already left.
func (m Model) selectedPath() string {
	if sel, ok := m.selectedStatus(); ok {
		return sel.Path
	}
	return ""
}

// armDiffDebounce starts (or restarts) the navigation debounce: bump diffGen so
// every timer already ticking is invalidated, then stamp the new timer with the
// number that bump produced, so it is never the one being invalidated. Same
// shape as dispatchSlowLoad's bump-then-stamp, for the same reason.
func (m *Model) armDiffDebounce() tea.Cmd {
	m.diffGen++
	gen := m.diffGen
	return tea.Tick(diffDebounceInterval, func(time.Time) tea.Msg {
		return diffDebounceMsg{gen: gen}
	})
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

// selectedPane mirrors selectedStatus/selectedSession for rowPane rows: it
// reports the cursor's pane (ok=true) only when the cursor sits on a rowPane
// leaf, so handleKey can branch enter to handleSwitchToPane the same way it
// branches to handleSwitchToSession/handleAttach today.
func (m Model) selectedPane() (tmux.PaneState, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return tmux.PaneState{}, false
	}
	r := m.rows[m.cursor]
	if r.kind != rowPane {
		return tmux.PaneState{}, false
	}
	return r.pane, true
}

func (m *Model) rebuildRows() {
	m.rows = buildRows(m.statuses, m.sessions, m.collapsed, m.filter.Value())
	// Keep cursor on a valid leaf row (worktree or session)
	m.cursor = tuicomponents.ClampCursor(leafIndices(m.rows), m.cursor)
}

// refreshView rebuilds the row list from the current m.statuses/m.sessions and
// re-runs the once-per-launch cursor placement. Every handler that replaces or
// layers onto either list ends with it, so the two refresh paths share this
// tail instead of each repeating it.
func (m *Model) refreshView() {
	m.rebuildRows()
	m.placeCursorOnActive()
}

// applyStatuses installs a refreshed worktree list, rebuilds the rows, and asks
// for the selected worktree's diff to be re-derived. Shared by the statusesMsg
// (slow refresh) and deletedMsg handlers so they can't drift; it does not touch
// m.status, leaving that to the caller (statusesMsg keeps whatever status is
// up; deletedMsg sets a "removed:" confirmation first).
//
// It raises forceDiff rather than dispatching the diff itself: the slow load is
// one of ADR-0024 §3's refresh triggers even when the selection is unchanged,
// and Update is the single dispatcher of diff work. Dispatching here as well
// would produce two diffs for the same path whenever a load also moved the
// cursor.
func (m *Model) applyStatuses(statuses []worktree.WorktreeStatus) {
	m.statuses = statuses
	m.loaded = true
	m.refreshView()
	m.forceDiff = true
}

// applySessions installs a refreshed session list, the m.sessions counterpart
// to applyStatuses. Shared by the two producers of a whole session list — the
// fast tick's tmuxStateMsg and sessionsMsg — so neither grows its own copy of
// the install-and-rebuild step. Both call it only after their gen stamp has
// been checked.
func (m *Model) applySessions(sessions []worktree.SessionStatus) {
	m.sessions = sessions
	m.sessionsLoaded = true
	m.refreshView()
}

// dropWorktree returns statuses without the repo/name row, leaving the input
// untouched. Removal by identity is what makes two overlapping deletes safe
// (see deletedMsg).
func dropWorktree(
	statuses []worktree.WorktreeStatus,
	repo, name string,
) []worktree.WorktreeStatus {
	var remaining []worktree.WorktreeStatus
	for _, s := range statuses {
		if s.Repo != repo || s.Name != name {
			remaining = append(remaining, s)
		}
	}
	return remaining
}

// dispatchSlowLoad is the only way a slow load leaves Update after startup:
// bump stateGen so every load already in flight is invalidated, then stamp the
// new load with the number that bump just produced — so this load is never the
// one being invalidated. The 30-second tick uses it, and so does every
// dashboard mutation that changes what git would enumerate (create, delete,
// repair), which is what "your own actions never wait on the timer"
// (ADR-0024 §2) means in code.
func (m *Model) dispatchSlowLoad() tea.Cmd {
	m.stateGen++
	return m.loadCmd(m.stateGen)
}

// dispatchSessionsLoad is dispatchSlowLoad's m.sessions counterpart, for the
// session mutations that need an immediate whole-list refresh rather than
// waiting up to 3 seconds for the fast tick.
func (m *Model) dispatchSessionsLoad() tea.Cmd {
	m.sessionGen++
	return m.sessionsLoadCmd(m.sessionGen)
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
		if r.kind == rowWorktree || r.kind == rowSession || r.kind == rowPane ||
			(r.kind == rowRepo && m.collapsed[r.repo]) {
			out = append(out, i)
		}
	}
	return out
}

func (m *Model) moveCursor(delta int) {
	m.cursor = tuicomponents.MoveCursor(m.navigableIndices(), m.cursor, delta)
}

// focusRow moves m.cursor to the first row matching pred, if any. Used after
// a rebuildRows() call that may have shifted every row's index, so the
// cursor is relocated by identity rather than assumed to still point at the
// right thing.
func (m *Model) focusRow(pred func(row) bool) (int, bool) {
	for i, r := range m.rows {
		if pred(r) {
			m.cursor = i
			return i, true
		}
	}
	return 0, false
}

func (m Model) safeMaxLeft() int {
	return max(int(float64(m.width)*maxLeftPaneWidthPct), minLeftPaneWidth)
}

func (m Model) rightPaneWidth() int {
	return max(m.width-m.leftPaneWidth-dividerWidth, 0)
}

// leftPaneTarget derives the left pane width from the e-toggle's bool state
// rather than from the pane's current (possibly mouse-dragged) width. Both
// targets are clamped to safeMaxLeft(), not just the wide one: below 59
// columns safeMaxLeft() already sits under defaultLeftPaneWidth, so an
// unclamped default-width target would hand back more than the 60% cap
// WindowSizeMsg otherwise enforces, and on a narrow-enough terminal it would
// leave rightPaneWidth() at 0 with no way back once toggled.
func (m Model) leftPaneTarget() int {
	if m.leftPaneWide {
		return min(defaultLeftPaneWidth*2, m.safeMaxLeft())
	}
	return min(defaultLeftPaneWidth, m.safeMaxLeft())
}

// Update implements tea.Model. It is a thin wrapper around update, which holds
// the actual message handling, and its only job is to notice that the selected
// worktree changed and arm the diff debounce for it.
//
// The check is structural — the selected path read before the handler ran,
// compared against the same read after — rather than a list of the keys that
// move the cursor, because that list is unmaintainable and was already wrong:
// h and l relocate the cursor by identity after a rebuild, z sends it to row 0,
// every typed filter rune re-clamps it, esc clears the filter and rebuilds every
// row, a bracketed paste does the same in one shot with no key involved at all,
// and placeCursorOnActive moves it from a message handler. Any key added later
// is covered for free. CLAUDE.md §4: make the class of mistake structurally
// impossible rather than documenting a convention someone has to remember.
//
// Being the single dispatcher is the other half of the job: no handler arms its
// own diff, so a slow load that also moves the cursor cannot produce two diffs
// for one path. The paths that must refresh an UNCHANGED selection say so with
// forceDiff, which this consumes and clears.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := m.selectedPath()

	updated, cmd := m.update(msg)
	next, ok := updated.(Model)
	if !ok {
		return updated, cmd
	}

	if next.selectedPath() == before && !next.forceDiff {
		return next, cmd
	}
	next.forceDiff = false
	return next, tea.Batch(cmd, next.armDiffDebounce())
}

func (m Model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Re-derived from the e-toggle's bool, not clamped from whatever
		// value leftPaneWidth already held: a prior mouse drag can have left
		// it at an arbitrary width, and clamping that would still let it sit
		// above the toggle's own targets.
		m.leftPaneWidth = m.leftPaneTarget()
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
		if msg.gen != m.stateGen {
			// A newer load or a mutation has already replaced this list.
			// Dropping the older snapshot is the correct outcome even when the
			// newer load then fails and only produces a statusMsg: the previous
			// list stays on screen until the next tick retries, and an older
			// snapshot is never the better answer.
			return m, nil
		}
		m.applyStatuses(msg.statuses)
		return m, nil

	case tmuxStateMsg:
		// Pane half: a layer, not a replacement, so it applies unconditionally
		// to whatever m.statuses holds right now. ApplyTo returns a new slice
		// and treats its input as read-only, so this cannot disturb a list a
		// slow load produced.
		m.statuses = msg.layer.ApplyTo(m.statuses)
		if msg.gen != m.sessionGen {
			// Session half is stale — a newer scan, a session load, or a
			// session mutation has superseded it. The pane half above still
			// stands, so the rows still need rebuilding.
			m.refreshView()
			return m, nil
		}
		m.applySessions(msg.layer.SessionStatuses())
		return m, nil

	case sessionsMsg:
		// Only reached on a successful ListSessions() (see sessionsLoadCmd);
		// a failure produces a statusMsg instead, which never reaches this
		// case and so never touches m.sessions - see that comment for why.
		if msg.gen != m.sessionGen {
			return m, nil
		}
		m.applySessions(msg.sessions)
		return m, nil

	case deletedMsg:
		// Replace the transient "deleting…" status with a confirmation, drop
		// the row by identity, and re-read git.
		//
		// The local drop and the load are not redundant. The drop is what makes
		// the row leave instantly and survive a second delete landing around it.
		// The load is what catches everything else the removal changed:
		// RemoveInRepo finishes with a repo-wide `git worktree prune`, which
		// also clears every OTHER stale registration in that repo, and nothing
		// but a fresh `git worktree list` notices those rows are gone. The load
		// cannot resurrect the row it just removed — this message is only
		// produced once the removal returned, so the enumeration starts after
		// the worktree is already gone from git.
		m.status = "removed: " + msg.name
		load := m.dispatchSlowLoad()
		m.applyStatuses(dropWorktree(m.statuses, msg.repo, msg.name))
		return m, load

	case repairDoneMsg:
		m.status = msg.status
		load := m.dispatchSlowLoad()
		return m, load

	case diffDebounceMsg:
		// The debounce elapsed. Spend the diff only if no newer selection change
		// has armed a timer since this one (ADR-0024 §3): holding j through
		// fifteen rows arms fifteen timers, and fourteen of them land here with a
		// superseded generation and cost nothing. The PR-title lookup rides along
		// for the same reason — otherwise the same navigation fires a burst of
		// `gh` calls.
		if msg.gen != m.diffGen {
			return m, nil
		}
		if sel, ok := m.selectedStatus(); ok {
			return m, m.selectionChangedCmd(sel)
		}
		return m, nil

	case diffMsg:
		// Drop a diff for a row the user has already left. A diff takes ~0.16s of
		// git, so a fast enough j/k outruns one in flight; without this check the
		// pane would render another worktree's changes under the selected row's
		// name until something recomputed it.
		if msg.path != m.selectedPath() {
			return m, nil
		}
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

	case fastTickMsg:
		// Fast refresh: one tmux scan, no git. It bumps sessionGen (its session
		// half replaces m.sessions wholesale) and deliberately leaves stateGen
		// alone — the pane half is a layer, and touching stateGen here would let
		// a 3-second scan invalidate a slow git load already in flight.
		m.sessionGen++
		return m, tea.Batch(m.scanTmuxCmd(m.sessionGen), m.fastTickCmd())

	case slowTickMsg:
		// Slow refresh: the git enumeration. This is the safety net for
		// worktrees created outside devgeta; anything the dashboard does itself
		// dispatches its own load immediately (see dispatchSlowLoad).
		load := m.dispatchSlowLoad()
		return m, tea.Batch(load, m.slowTickCmd())

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
		load := m.dispatchSessionsLoad()
		return m, load

	case sessionKilledMsg:
		// Removal by identity plus a sessionGen bump, the same division of
		// labor deletedMsg uses: the identity removal handles a second kill
		// landing around this one, and the bump stops a scan that started
		// before `tmux kill-session` finished from re-adding the row it saw.
		m.sessionGen++
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
	// Neither of these asks for a diff: they just move the cursor, and Update
	// notices the selection changed on the way out and arms the debounce. Same
	// for h, l, z, and the filter below.
	case "j", "down":
		m.diffScroll = 0
		m.moveCursor(1)
		return m, nil

	case "k", "up":
		m.diffScroll = 0
		m.moveCursor(-1)
		return m, nil

	case "h":
		// Priority 1: cursor on a pane row - collapse its enclosing parent.
		if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowPane {
			if parent, ok := enclosingPaneParent(m.rows, m.cursor); ok {
				if parentKey, qualifies := paneParentKey(parent); qualifies {
					m.collapsed[parentKey] = true
					m.rebuildRows()
					// The pane row the cursor was on just disappeared;
					// relocate the parent by identity (rebuild can shift
					// row positions) and land the cursor there.
					m.focusRow(func(r row) bool { return sameParentRow(r, parent) })
					return m, nil
				}
			}
		}

		// Priority 2: cursor on an expanded, qualifying worktree/session row -
		// collapse its own pane children in place (cursor stays put).
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			if parentKey, qualifies := paneParentKey(
				m.rows[m.cursor],
			); qualifies &&
				!m.collapsed[parentKey] {
				m.collapsed[parentKey] = true
				m.rebuildRows()
				return m, nil
			}
		}

		// Priority 3: existing repo-collapse behavior (unchanged).
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
			m.focusRow(func(r row) bool { return r.kind == rowRepo && r.repo == collapseRepo })
		}
		return m, nil

	case "l":
		// Priority 1: cursor on a collapsed, qualifying worktree/session row -
		// expand its pane children and land on the first one revealed.
		if m.cursor >= 0 && m.cursor < len(m.rows) {
			if parentKey, qualifies := paneParentKey(
				m.rows[m.cursor],
			); qualifies &&
				m.collapsed[parentKey] {
				parent := m.rows[m.cursor]
				m.collapsed[parentKey] = false
				m.rebuildRows()
				if i, ok := m.focusRow(func(r row) bool { return sameParentRow(r, parent) }); ok {
					if i+1 < len(m.rows) && m.rows[i+1].kind == rowPane {
						m.cursor = i + 1
					}
				}
				return m, nil
			}
		}

		// Priority 2: existing repo-expand behavior (unchanged).
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
				m.focusRow(
					func(r row) bool { return r.kind == rowWorktree && r.repo == expandRepo },
				)
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

	case "e":
		m.leftPaneWide = !m.leftPaneWide
		m.leftPaneWidth = m.leftPaneTarget()
		return m, nil

	case "enter":
		if _, ok := m.selectedPane(); ok {
			return m.handleSwitchToPane()
		}
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

	case "ctrl+r":
		return m.refreshDiff()

	case "ctrl+d":
		m.diffScroll = min(m.diffScroll+m.diffPageSize(), m.maxDiffScroll())
		return m, nil

	case "ctrl+u":
		m.diffScroll = max(m.diffScroll-m.diffPageSize(), 0)
		return m, nil
	}

	return m, nil
}

// refreshDiff is ctrl+r: buy back the staleness ADR-0024 §3 accepts when it
// takes the diff off the fast tick, by recomputing it for the CURRENT selection
// on demand. It returns no command of its own — the Update wrapper is the single
// dispatcher and picks forceDiff up on the way out, so the recompute rides the
// same 180 ms debounce as navigation does.
//
// It deliberately does not dispatch the slow git load. That enumerates every
// known repo (one git process each) and stays the 30-second tick's and the
// mutations' job; what a reader watching an agent write files wants back from
// this key is the diff, not the worktree list.
func (m Model) refreshDiff() (tea.Model, tea.Cmd) {
	m.forceDiff = true
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

	// Every key routes here while the diff pane is focused, and this is where a
	// reader most wants the refresh — so ctrl+r has to be bound in both handlers,
	// not just the list's.
	case "ctrl+r":
		return m.refreshDiff()

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

// handleSwitchToPane is enter's rowPane counterpart to handleAttach and
// handleSwitchToSession: switches the attached client straight to the
// selected pane's session, window, and pane, then quits. Guarded by the same
// $TMUX check and message as those two, since moving the client requires one
// to already be attached.
//
// The agent-state clear is best-effort, same treatment as handleAttach's own
// clearAgentStateFn call: a failed clear must never stop the switch, since
// the switch is the point of pressing enter. Unlike handleAttach, the error
// is not silently dropped - ClearAgentStateForPane returns it specifically so
// a caller can log it (CLAUDE.md forbids discarding an error outright), so it
// goes to the logger at debug level, matching this dashboard's existing
// best-effort-failure logging (see run.go's WarnFn sink).
func (m Model) handleSwitchToPane() (tea.Model, tea.Cmd) {
	sel, ok := m.selectedPane()
	if !ok {
		return m, nil
	}

	if os.Getenv("TMUX") == "" {
		m.status = notInsideTmuxStatus
		return m, nil
	}

	switchFn := m.switchToPaneFn
	clearFn := m.clearAgentStateForPaneFn
	session, window, paneID := sel.Session, sel.Window, sel.PaneID
	return m, func() tea.Msg {
		if err := clearFn(paneID); err != nil {
			logger.L().Debugw(
				"worktree: failed to clear agent state for pane",
				"pane", paneID,
				"err", err,
			)
		}
		if err := switchFn(session, window, paneID); err != nil {
			return statusMsg("switch failed: " + err.Error())
		}
		return tea.QuitMsg{}
	}
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
		//
		// Every outcome from here on reports a repairDoneMsg rather than a
		// plain statusMsg: each one has either rebuilt a window or, on the
		// failure path, pruned a stale git entry, and both need the git
		// re-read the repairDoneMsg handler dispatches. The plain-attach
		// failure above is not one of them — nothing was repaired there, so
		// nothing changed for git to notice.
		layout, err := worktree.ResolveLayout("", "", gc)
		if err != nil {
			return repairDoneMsg{status: "repair failed: " + err.Error()}
		}
		if err := repairFn(repo, name, layout); err != nil {
			return repairDoneMsg{status: "repair failed: " + err.Error()}
		}
		session, ok = windowSessionFn(window)
		if !ok {
			return repairDoneMsg{status: "repair succeeded but window not found"}
		}
		if err := attachFn(session, window); err != nil {
			return repairDoneMsg{status: "attach after repair failed: " + err.Error()}
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

	// Second press: execute. Only the identity crosses into the goroutine —
	// m.statuses deliberately does not, so two overlapping removals can't each
	// return a whole list and undo one another (see deletedMsg).
	repo := sel.Repo
	name := sel.Name
	return "", func() tea.Msg {
		if err := remove(repo, name); err != nil {
			return statusMsg("delete failed: " + err.Error())
		}
		// deletedMsg (not statusesMsg) so the "deleting…" status is replaced
		// with a "removed:" confirmation, not left lingering on the refreshed list.
		return deletedMsg{repo: repo, name: name}
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
			return repairDoneMsg{status: "repair failed: " + err.Error()}
		}
		if err := repairFn(repo, name, layout); err != nil {
			return repairDoneMsg{status: "repair failed: " + err.Error()}
		}
		return repairDoneMsg{status: "repaired: " + name}
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

	// Scroll viewport: only rows[start:end] are rendered, so a list longer
	// than the pane's height no longer hides its tail (nor lets the cursor
	// move into it) — isLastChild above deliberately still scans the full
	// m.rows slice, not this window, or the tree connectors would break at
	// the window edge.
	viewportHeight := max(m.height-2, 0)
	start, end := tuicomponents.VisibleWindow(len(m.rows), m.cursor, viewportHeight)

	var sb strings.Builder
	for i, r := range m.rows[start:end] {
		idx := start + i
		var line string
		if r.kind == rowRepo {
			// A repo has no single tmux window, so there's no natural
			// "is the window active" bool the way a worktree/session has.
			// Use r.agentState != "" as the proxy: if any child worktree
			// ever had an agent report a state, treat the header as active
			// (blocked/error/idle/busy map to their real colors); if no
			// child ever reported anything, SessionStateFromAgent(false, "",
			// 0) falls through to StateNoSession (dim "○") — a more honest
			// default than a false "everything's running" green dot.
			state := tuicomponents.SessionStateFromAgent(r.agentState != "", r.agentState, 0)
			collapse := "▼"
			if m.collapsed[r.repo] {
				collapse = "▶"
			}
			text := collapse + " " + r.repo
			// Right-aligned "N trees"/"1 tree" badge. Truncate the header text
			// first (leaving room for at least one separating space) so the
			// badge is never pushed past width, then pad the remainder — the
			// same fixed-width layout the rowWorktree branch below uses.
			// prefix = dot(1) + space(1) = 2 display cols, on top of the
			// collapse+" "+repo text already accounted for by `text` itself.
			badge := fmt.Sprintf("%d trees", r.worktreeCount)
			if r.worktreeCount == 1 {
				badge = "1 tree"
			}
			badgeW := ansi.StringWidth(badge)
			text = ansi.Truncate(text, max(0, width-2-badgeW-1), "")
			pad := strings.Repeat(" ", max(0, width-2-ansi.StringWidth(text)-badgeW))
			if idx == m.cursor {
				// Cursor landed here after h — show repo header with selection highlight.
				g := m.palette.StatusGlyph(state)
				line = m.palette.Selected.Render(g + " " + text + pad + badge)
			} else {
				line = m.palette.StatusDot(state) + " " + m.palette.RepoHeader.Render(
					text,
				) + pad + m.palette.HintDesc.Render(
					badge,
				)
			}
		} else if r.kind == rowSession {
			const label = "session"
			labelW := ansi.StringWidth(label)

			// Expand/collapse chevron for pane-row children (ADR-0008 §3):
			// "▼" expanded, "▶" collapsed, blank when the session doesn't
			// qualify (fewer than 2 stateful panes) — but the 2-column slot
			// (chevron + space) is reserved unconditionally so every session
			// row's square/name/label line up regardless of qualification.
			chevronGlyph := chevronGlyphFor(r, m.collapsed)

			// prefix = chevron(1) + space(1) + square(1) + space(1) = 4 display
			// cols — the chevron slot above is the same width as the repo
			// header's, and the square(1)+space(1) after it is the same width
			// as the "▼ "/"▶ " chevron pair used there, so session rows (flat
			// top-level leaves, no tree connector) line up with repo headers
			// in the left column. The square (■/□) is a different shape from
			// the worktree ●/○ circle so the two row kinds are distinguishable
			// at a glance, not just by the trailing "session" label.
			name := ansi.Truncate(r.session.Name, max(0, width-4-labelW-1), "")
			pad := strings.Repeat(" ", max(0, width-4-ansi.StringWidth(name)-labelW))

			// No agent has ever reported on this session's panes: keep the
			// original attached-only square glyph. Otherwise, a pane reported
			// state at least once, so switch to the agent-state vocabulary
			// (●/◆/!/✕) shared with rowWorktree, via StatusGlyph/StatusDot.
			hasAgentState := r.session.AgentState != ""
			var state tuicomponents.SessionState
			if hasAgentState {
				state = tuicomponents.SessionStateFromAgent(true, r.session.AgentState, 0)
			}

			if idx == m.cursor {
				var g string
				if hasAgentState {
					g = m.palette.StatusGlyph(state)
				} else {
					g = m.palette.SessionGlyph(r.session.Attached)
				}
				plainText := chevronGlyph + " " + g + " " + name
				if m.pendingKillSession == r.session.Name {
					line = m.palette.Armed.Render(plainText + pad + label)
				} else {
					line = m.palette.Selected.Render(plainText + pad + label)
				}
			} else {
				var dot string
				if hasAgentState {
					dot = m.palette.StatusDot(state)
				} else {
					dot = m.palette.SessionDot(r.session.Attached)
				}
				line = chevronGlyph + " " + dot + " " + name + pad + m.palette.HintDesc.Render(
					label,
				)
			}
		} else if r.kind == rowPane {
			// 4-space indent: deeper than the repo/session 2-column prefix and
			// the worktree 5-column prefix, so pane rows read as nested one
			// level further under either parent kind.
			const indent = "    "
			// windowActive is unconditionally true: a pane row only ever
			// exists because its pane is live in an existing window/session,
			// so an empty r.pane.State falls through to StateRunning -
			// consistent with how worktree/session rows with no agent state
			// are treated.
			state := tuicomponents.SessionStateFromAgent(true, r.pane.State, 0)

			if idx == m.cursor {
				g := m.palette.StatusGlyph(state)
				plainText := indent + g + " " + r.pane.Window + ":" + r.pane.PaneIndex + " " + r.pane.CurrentCommand
				plainText = ansi.Truncate(plainText, width, "")
				plainText += strings.Repeat(" ", max(0, width-ansi.StringWidth(plainText)))
				line = m.palette.Selected.Render(plainText)
			} else {
				dot := m.palette.StatusDot(state)
				text := indent + dot + " " + r.pane.Window + ":" + r.pane.PaneIndex + " " + r.pane.CurrentCommand
				text = ansi.Truncate(text, width, "")
				text += strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
				line = text
			}
		} else {
			state := tuicomponents.SessionStateFromWorktree(r.status, r.status.AgentState, 0)

			// Expand/collapse chevron for pane-row children (ADR-0008 §3):
			// "▼" expanded, "▶" collapsed, blank when the worktree doesn't
			// qualify (fewer than 2 stateful panes) — the 2-column slot
			// (chevron + space) is reserved unconditionally so every
			// worktree row's connector/dot/name lines up regardless of
			// qualification.
			chevronGlyph := chevronGlyphFor(r, m.collapsed)
			chevronPrefix := chevronGlyph + " "

			// Tree connector: "└ " for last child, "  " otherwise (both 2 display cols).
			connectorRaw := "  "
			connectorStyled := "  "
			if isLastChild(idx) {
				connectorRaw = "└ "
				connectorStyled = m.palette.Divider.Render("└") + " "
			}
			// prefix = chevron(1) + space(1) + connector(2) + dot(1) + branchChar(1) + space(1)
			// = 7 display cols
			name := ansi.Truncate(r.status.Name, max(0, width-7), "")
			pendingKey := r.status.Repo + "/" + r.status.Name
			padding := strings.Repeat(" ", max(0, width-7-ansi.StringWidth(name)))

			if idx == m.cursor {
				g := m.palette.StatusGlyph(state)
				plainText := chevronPrefix + connectorRaw + g + branchChar + " " + name
				if m.pendingDelete == pendingKey || m.pendingSessionDelete == pendingKey {
					line = m.palette.Armed.Render(plainText + padding)
				} else {
					line = m.palette.Selected.Render(plainText + padding)
				}
			} else {
				line = chevronPrefix + connectorStyled + m.palette.StatusDot(
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

	// Pane rows have the same problem as session rows above: selectedStatus
	// (and so selectionChangedCmd) never fires for them either, so without
	// this check the pane would keep showing whichever worktree's diff was
	// selected last instead of something that reflects the current row.
	if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowPane {
		return m.palette.Inactive.Render(
			ansi.Truncate(
				"Panes have no diff — select the worktree or session row to see its diff.",
				width,
				"",
			),
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
			{Key: "^r", Desc: "refresh"},
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
		{Key: "e", Desc: "width"},
		{Key: "^r", Desc: "refresh"},
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

// renderStatus renders the one-line status message at the bottom of the
// dashboard.
//
// flattenToOneLine is applied before truncation, and is load-bearing rather
// than cosmetic: renderDashboard budgets exactly m.height lines
// (body + hint + status), so a status containing a newline emits an extra
// terminal row, the terminal scrolls to make room, and the previous frame's
// rows stay on screen interleaved with the new ones — the "duplicated and
// nested worktrees" corruption this cycle exists to fix. Status text arrives
// from many sources (git advisories, wrapped errors, tool output), several of
// which are legitimately multi-line, so the invariant is enforced here at the
// single point every one of them passes through rather than trusted to each
// caller.
func (m Model) renderStatus(width int) string {
	if m.status == "" {
		return ""
	}
	return m.palette.StatusMsg.Render(ansi.Truncate(flattenToOneLine(m.status), width, ""))
}

// flattenToOneLine collapses every newline, carriage return, and tab in s
// into single spaces, so the result occupies exactly one terminal row.
// Runs of resulting whitespace are squeezed so an indented multi-line message
// (git's advisories are written that way) reads as prose rather than as a
// line with a gap in the middle.
func flattenToOneLine(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	lastWasSpace := false
	for _, r := range s {
		isSpace := r == '\n' || r == '\r' || r == '\t' || r == ' '
		if isSpace {
			if !lastWasSpace && b.Len() > 0 {
				b.WriteByte(' ')
			}
			lastWasSpace = true
			continue
		}
		b.WriteRune(r)
		lastWasSpace = false
	}
	return strings.TrimRight(b.String(), " ")
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
		{Key: "h / l", Desc: "collapse / expand repo, or a worktree/session's panes"},
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
		{Key: "e", Desc: "toggle left pane width (default / double)"},
		{Key: "ctrl+r", Desc: "recompute the selected worktree's diff now"},
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
