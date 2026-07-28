// Worktree coordinator manages git worktrees with tmux window integration
//
// Each worktree gets its own tmux window with an AI assistant running, enabling
// parallel AI-assisted development across multiple branches within the same session.
// This follows the "one session per folder" workflow where worktrees are managed
// as separate windows rather than separate sessions.
//
// References:
// - Git Worktree Documentation: https://git-scm.com/docs/git-worktree
// - Tmux Manual: https://man7.org/linux/man-pages/man1/tmux.1.html

package worktree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/fzf"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
	"github.com/cjairm/devgeta/pkg/utils"
)

const (
	// windowPrefix is prepended to worktree names for tmux windows
	windowPrefix = "wt-"

	// fallbackSession is the always-available session the attached client is
	// moved to before its current session is killed. It matches the session
	// name created by configs/alacritty/starter.sh on terminal startup and is
	// created on demand when missing. It is never killed itself.
	fallbackSession = "misc"

	// Agent state constants representing the aggregated state of AI agents
	// in a worktree's window panes (per ADR-0005).
	AgentStateBusy    = "busy"
	AgentStateIdle    = "idle"
	AgentStateError   = "error"
	AgentStateBlocked = "blocked"
)

// isWorktreeWindow reports whether a tmux window name belongs to a worktree
// (i.e. was produced by GetWindowName), rather than a window a user created
// themselves in a plain session.
func isWorktreeWindow(name string) bool {
	return strings.HasPrefix(name, windowPrefix)
}

// FlattenName converts a branch-style name (with slashes) to a flat directory name.
// e.g. "feat/search-specs" → "feat-search-specs"
func FlattenName(name string) string {
	return strings.ReplaceAll(name, "/", "-")
}

// TmuxSessionName derives a valid tmux session name from a repo slug or folder
// name. tmux treats ".", ":", and whitespace specially in target names, so each
// is replaced with "_".
func TmuxSessionName(repoSlug string) string {
	return strings.NewReplacer(".", "_", ":", "_", " ", "_", "\t", "_").Replace(repoSlug)
}

// WorktreeStatus contains information about a worktree and its associated window
type WorktreeStatus struct {
	Name         string
	Path         string
	Branch       string
	TmuxWindow   string
	WindowActive bool
	// AgentState is the aggregated agent state of the worktree's window,
	// computed by aggregating over its panes' @dg_agent_state values per
	// ADR-0005 (blocked > error > idle > busy > no agent). "" means no pane
	// in the window ever had an agent write to it (or the window doesn't
	// exist), not "idle" - see aggregateAgentState.
	AgentState string
	Repo       string
}

// SessionStatus describes a standalone tmux session for the workspace
// dashboard - one with no worktree-backed window, so it doesn't already
// appear via List().
type SessionStatus struct {
	Name     string
	Attached bool
}

// WorktreeState holds the current state of a worktree
type WorktreeState struct {
	WtPath       string
	WindowName   string
	WtExists     bool
	WindowExists bool
}

// WorktreeManager coordinates git worktrees with tmux windows
type WorktreeManager struct {
	Git  *git.Git
	Tmux *tmux.Tmux
	Fzf  *fzf.Fzf
	Base cmd.BaseCommandExecutor
	// WarnFn reports a non-fatal warning to the user (e.g. the recent-repos
	// store failed to record a successful create). It defaults to a CLI-safe
	// print in New(); a caller rendering a TUI must override it before
	// constructing its model with something like a toast, since printing
	// directly to stdout underneath a running Bubble Tea alt-screen program
	// would corrupt its rendering.
	WarnFn func(msg string)
}

// New creates a new WorktreeManager instance
func New() *WorktreeManager {
	return &WorktreeManager{
		Git:    git.New(),
		Tmux:   tmux.New(),
		Fzf:    fzf.New(),
		Base:   cmd.NewBaseCommand(),
		WarnFn: utils.PrintWarning,
	}
}

// worktreePath returns ~/.local/share/devgeta/worktrees/<repo-slug>/<flat-name>
// Slashes in the name are replaced with dashes to keep the worktree directory
// directly under the repo slug. This ensures the parent directory is always
// the repo slug (important for tools that display the parent dir, e.g. Claude Code).
func (w *WorktreeManager) worktreePath(repoSlug, name string) string {
	return filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, FlattenName(name))
}

// GetWorktreeBasePath returns the base path for all devgeta worktrees
func GetWorktreeBasePath() string {
	return filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees")
}

// Create creates a new worktree with tmux window and builds the given window
// layout in it (one pane per layout.Panes entry). The repo is the one
// containing the current directory and the window opens in the current tmux
// session. If force is false and the repo has hooks incompatible with git
// worktrees, the user is prompted to confirm before proceeding.
func (w *WorktreeManager) Create(name string, layout Layout, force bool) error {
	if err := validateLayout(layout); err != nil {
		return err
	}
	repoRoot, err := w.Git.GetRepoRoot()
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}
	return w.create(repoRoot, name, layout, force, false)
}

// CreateAt is Create for a repository the caller is not inside: repoPath ("~"
// expanded) locates the repo, the window opens in the repo-slug tmux session
// (created when missing, reused otherwise), and the attached client follows
// it when running inside tmux.
func (w *WorktreeManager) CreateAt(repoPath, name string, layout Layout, force bool) error {
	if err := validateLayout(layout); err != nil {
		return err
	}
	repoRoot, err := w.Git.GetRepoRootIn(paths.ExpandHome(repoPath))
	if err != nil {
		return fmt.Errorf("no git repository at %s: %w", repoPath, err)
	}
	return w.create(repoRoot, name, layout, force, true)
}

// validateLayout guards the shared create/repair flow: a layout with at
// least one pane is required, and every pane's underlying tool must be
// installed before any git or tmux state is touched. layout.EnsureInstalled
// already runs every pane's checker and fails on the first bad one, so this
// gives the same "one actionable message before anything is touched"
// guarantee validateCoder gave for a single coder - a layout is just N of
// those checks instead of one.
func validateLayout(layout Layout) error {
	if len(layout.Panes) == 0 {
		return fmt.Errorf("a layout with at least one pane is required")
	}
	return layout.EnsureInstalled()
}

// create is the shared worktree-creation flow. useRepoSession selects where
// the window goes: the current tmux session (plain Create) or the repo-slug
// session (CreateAt).
func (w *WorktreeManager) create(
	repoRoot, name string,
	layout Layout,
	force, useRepoSession bool,
) error {
	repoSlug := filepath.Base(repoRoot)
	wtPath := w.worktreePath(repoSlug, name)
	windowName := GetWindowName(repoSlug, name)

	state, err := w.worktreeState(repoSlug, name)
	if err != nil {
		return err
	}

	if state.WtExists && state.WindowExists {
		return fmt.Errorf(
			"worktree '%s' already exists and has an active window; use `dg ws`",
			name,
		)
	}
	if state.WtExists && !state.WindowExists {
		// Check if directory actually exists on disk
		if _, err := os.Stat(wtPath); os.IsNotExist(err) {
			// Directory missing but git still tracks it - auto-prune and continue
			if pruneErr := w.Git.PruneWorktreesAt(filepath.Dir(wtPath)); pruneErr != nil {
				return fmt.Errorf("stale worktree entry detected but failed to prune: %w", pruneErr)
			}
			// After pruning, continue with creation
		} else {
			// Directory exists, suggest repair
			return fmt.Errorf(
				"worktree '%s' exists but has no active window; use `dg wt repair %s`",
				name,
				name,
			)
		}
	}
	if !state.WtExists && state.WindowExists {
		return fmt.Errorf(
			"orphan window '%s' exists; run `tmux kill-window -t %s` manually",
			windowName,
			windowName,
		)
	}

	if !force {
		if warnings := w.Git.CheckHookCompatibility(repoRoot); len(warnings) > 0 {
			fmt.Println("Warning: this repo has hooks incompatible with git worktrees:")
			for _, warning := range warnings {
				fmt.Printf("  - %s\n", warning)
			}
			fmt.Println("In a worktree, .git is a file not a directory, so these hooks will fail.")
			fmt.Print("Continue anyway? [y/N] (or re-run with --force to skip this check): ")
			if !confirmFromTTY() {
				return fmt.Errorf("cancelled")
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return fmt.Errorf("failed to create worktree directory: %w", err)
	}

	if err := w.Git.CreateWorktreeIn(repoRoot, wtPath, name); err != nil {
		if strings.Contains(err.Error(), "is a missing but already registered") {
			if pruneErr := w.Git.PruneWorktreesAt(filepath.Dir(wtPath)); pruneErr == nil {
				if retryErr := w.Git.CreateWorktreeIn(repoRoot, wtPath, name); retryErr == nil {
					return w.launchWindowAndRecord(
						repoRoot,
						repoSlug,
						windowName,
						wtPath,
						layout,
						useRepoSession,
					)
				}
			}
		}
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	return w.launchWindowAndRecord(repoRoot, repoSlug, windowName, wtPath, layout, useRepoSession)
}

// launchWindowAndRecord wraps launchWindow so both create() call sites (the
// happy path and the stale-entry retry path) record the repo as used on
// success without duplicating that logic at each call site.
func (w *WorktreeManager) launchWindowAndRecord(
	repoRoot, repoSlug, windowName, wtPath string,
	layout Layout,
	useRepoSession bool,
) error {
	if err := w.launchWindow(repoSlug, windowName, wtPath, layout, useRepoSession); err != nil {
		return err
	}
	w.recordRepoUsed(repoRoot)
	return nil
}

// launchWindow creates the worktree's tmux window and builds layout's panes in
// it, rolling the worktree back if the window cannot be created or built. The
// window goes to the current session or the repo-slug session (created when
// missing, reused otherwise); in the latter case the attached client follows
// it.
func (w *WorktreeManager) launchWindow(
	repoSlug, windowName, wtPath string,
	layout Layout,
	useRepoSession bool,
) error {
	if !useRepoSession {
		return w.buildWindowFromLayout(windowName, wtPath, layout)
	}

	if err := w.ensureWindow(repoSlug, windowName, wtPath, layout); err != nil {
		_ = w.Git.RemoveWorktree(wtPath, true, "")
		return err
	}
	// Follow the new window when running inside tmux (best-effort).
	if os.Getenv("TMUX") != "" {
		if session, ok := w.Tmux.WindowSession(windowName); ok {
			_ = w.Tmux.SwitchToWindow(session, windowName)
		}
	}
	return nil
}

// buildWindowFromLayout creates windowName (pane 0 only) and then builds the
// rest of layout's panes into it. If the window can't be created at all, or
// any later step fails partway (pane 0's launch, a split, or a later pane's
// launch/reselect), the partially built window is killed (best-effort) and
// the worktree is rolled back - the same "all or nothing" guarantee the
// single-pane path gave, never a window with some panes up alongside a
// worktree that's still there.
func (w *WorktreeManager) buildWindowFromLayout(windowName, wtPath string, layout Layout) error {
	if err := w.Tmux.CreateWindow(windowName, wtPath); err != nil {
		_ = w.Git.RemoveWorktree(wtPath, true, "")
		return fmt.Errorf("failed to create tmux window: %w", err)
	}

	sendKeys := func(command string) error {
		return w.Tmux.SendKeysToWindow(windowName, command)
	}
	if err := w.buildWindowPanes(windowName, wtPath, layout, sendKeys); err != nil {
		_ = w.Tmux.KillWindow(windowName)
		_ = w.Git.RemoveWorktree(wtPath, true, "")
		return err
	}
	return nil
}

// buildWindowPanes builds every pane of layout into a window that already
// exists with exactly one (pane 0) pane - i.e. right after CreateWindow,
// CreateWindowInSession, or CreateSessionWithWindow. target is how the window
// is addressed for SplitWindow/ActivePaneID: a bare window name (current
// session) or "session:window" (qualified, for a window that may not be in
// the attached client's session). sendKeys sends a command to target's
// currently active pane, mirroring whichever of SendKeysToWindow /
// SendKeysToWindowInSession target's form matches.
func (w *WorktreeManager) buildWindowPanes(
	target, wtPath string,
	layout Layout,
	sendKeys func(command string) error,
) error {
	// Pane 0's tmux pane_id must be captured now, before any split, while it
	// is still the window's only (and therefore unambiguously "active") pane.
	// It's needed only when there's more than one pane to reselect it later.
	var pane0ID string
	if len(layout.Panes) > 1 {
		id, err := w.Tmux.ActivePaneID(target)
		if err != nil {
			return fmt.Errorf("layout %q: failed to identify pane 0: %w", layout.Name, err)
		}
		pane0ID = id
	}

	for i, pane := range layout.Panes {
		if i > 0 {
			// split-window always splits the CURRENTLY ACTIVE pane and makes
			// the new pane active, so building panes strictly in order (pane
			// 0 first, no explicit pane index anywhere) is enough for each
			// pane's command to land in the right place: right after this
			// split, the new pane is active, and sendKeys below (send-keys
			// with no pane index) always targets whichever pane in the
			// window is currently active.
			if err := w.Tmux.SplitWindow(target, wtPath, pane.Split); err != nil {
				return fmt.Errorf(
					"layout %q, pane %d: failed to split window: %w",
					layout.Name, i+1, err,
				)
			}
		}
		if err := sendKeys(pane.Command); err != nil {
			return fmt.Errorf("layout %q, pane %d: failed to launch: %w", layout.Name, i+1, err)
		}
	}

	if pane0ID != "" {
		// Land the user on pane 0 (e.g. the AI coder), not whichever pane was
		// split last (e.g. an editor pane), when they attach. Re-targeting by
		// tmux pane index (e.g. target+".0") is NOT reliable: devgeta's own
		// shipped tmux.conf sets pane-base-index to 1 (configs/tmux/tmux.conf),
		// so a window's first pane is index 1, not 0 - pane_id is tmux's own
		// stable, globally-unique identifier and is unaffected by that option.
		if err := w.Tmux.SelectPane(pane0ID); err != nil {
			return fmt.Errorf("layout %q: failed to select pane 0: %w", layout.Name, err)
		}
	}

	return nil
}

// worktreeState checks the current state of a worktree
func (w *WorktreeManager) worktreeState(repoSlug, name string) (WorktreeState, error) {
	state := WorktreeState{
		WtPath:     w.worktreePath(repoSlug, name),
		WindowName: GetWindowName(repoSlug, name),
	}

	if _, err := os.Stat(state.WtPath); err == nil {
		state.WtExists = true
	}

	worktrees, err := w.Git.ListWorktreesAt(state.WtPath)
	if err == nil {
		for _, wt := range worktrees {
			if wt.Path == state.WtPath {
				state.WtExists = true
				break
			}
		}
	}

	if _, ok := w.Tmux.WindowSession(state.WindowName); ok {
		state.WindowExists = true
	}

	return state, nil
}

// agentStateRank ranks a pane's @dg_agent_state value by aggregation urgency
// per ADR-0005: blocked > error > idle > busy > (no agent) - higher wins.
// The empty string (no agent has ever written to this pane) and any
// unrecognized value both resolve to the zero value here (Go's map lookup
// default), so a value this cycle didn't anticipate can never silently
// outrank a real one; it just falls back to "no agent" for ranking purposes.
var agentStateRank = map[string]int{
	AgentStateBusy:    1,
	AgentStateIdle:    2,
	AgentStateError:   3,
	AgentStateBlocked: 4,
}

// aggregateAgentState reduces one window's pane states to the single value a
// worktree row should report, per ADR-0005's precedence. Pure function of
// the pane states so it's testable without a WorktreeManager. Returns "" when
// states is empty or nil, or every entry is "" / unrecognized - i.e. no pane
// in the window has a real agent state to report.
func aggregateAgentState(states []string) string {
	best := ""
	bestRank := 0
	for _, s := range states {
		if rank := agentStateRank[s]; rank > bestRank {
			bestRank = rank
			best = s
		}
	}
	return best
}

// List returns all worktrees with their window status across all repos
func (w *WorktreeManager) List() ([]WorktreeStatus, error) {
	basePath := GetWorktreeBasePath()

	entries, err := os.ReadDir(basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []WorktreeStatus{}, nil
		}
		return nil, err
	}

	// One list-panes -a scan for the whole dashboard refresh, instead of a
	// WindowSession() call per worktree - each of which ran its own fresh
	// list-windows -a scan (N tmux execs per 3-second refresh). Indexed by
	// window name: a window's presence as a key is the direct equivalent of
	// "does a window with this name exist" (PaneStates enumerates every pane
	// on the server, and a window with zero panes cannot exist in tmux), and
	// its slice of pane states is what aggregateAgentState reduces to
	// AgentState. See ADR-0005.
	paneStatesByWindow := make(map[string][]string)
	for _, ps := range w.Tmux.PaneStates() {
		paneStatesByWindow[ps.Window] = append(paneStatesByWindow[ps.Window], ps.State)
	}

	var statuses []WorktreeStatus
	for _, repoEntry := range entries {
		if !repoEntry.IsDir() {
			continue
		}

		repoSlug := repoEntry.Name()
		repoDir := filepath.Join(basePath, repoSlug)

		wtEntries, err := os.ReadDir(repoDir)
		if err != nil {
			continue
		}

		for _, wtEntry := range wtEntries {
			if !wtEntry.IsDir() {
				continue
			}

			name := wtEntry.Name()
			wtPath := filepath.Join(repoDir, name)
			windowName := GetWindowName(repoSlug, name)

			branch := ""
			worktrees, err := w.Git.ListWorktreesAt(wtPath)
			if err == nil {
				for _, wt := range worktrees {
					if wt.Path == wtPath {
						branch = wt.Branch
						break
					}
				}
			}

			// Comma-ok reports key presence, not slice non-emptiness: a
			// window in the index always has at least one pane state
			// appended above, so ok alone is the "window exists" signal.
			states, windowActive := paneStatesByWindow[windowName]
			statuses = append(statuses, WorktreeStatus{
				Name:         name,
				Path:         wtPath,
				Branch:       branch,
				TmuxWindow:   windowName,
				WindowActive: windowActive,
				AgentState:   aggregateAgentState(states),
				Repo:         repoSlug,
			})
		}
	}

	return statuses, nil
}

// ListNames returns just the worktree names across all repos for shell completion.
func (w *WorktreeManager) ListNames() ([]string, error) {
	statuses, err := w.List()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(statuses))
	for _, s := range statuses {
		names = append(names, s.Name)
	}
	return names, nil
}

// ListSessions returns tmux sessions with no worktree-backed window - plain
// sessions the workspace dashboard should list on their own, since a
// worktree-backed session already appears via List(). A single
// Tmux.SessionWindows() scan finds every wt-prefixed window across the
// server; any session hosting at least one is excluded here.
//
// Errors from Tmux.ListSessions() propagate unchanged, including its (nil,
// nil) no-server result, which flows through as an empty list here rather
// than an error.
func (w *WorktreeManager) ListSessions() ([]SessionStatus, error) {
	sessions, err := w.Tmux.ListSessions()
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}

	worktreeSessions := make(map[string]bool, len(sessions))
	for _, sw := range w.Tmux.SessionWindows() {
		if isWorktreeWindow(sw.Window) {
			worktreeSessions[sw.Session] = true
		}
	}

	var statuses []SessionStatus
	for _, s := range sessions {
		if worktreeSessions[s.Name] {
			continue
		}
		statuses = append(statuses, SessionStatus{Name: s.Name, Attached: s.Attached})
	}
	return statuses, nil
}

// findRepoForWorktree searches the centralized base path for a worktree by name
// and returns the repo slug that owns it. Returns "" if not found or ambiguous.
func (w *WorktreeManager) findRepoForWorktree(name string) string {
	entries, err := os.ReadDir(GetWorktreeBasePath())
	if err != nil {
		return ""
	}
	var matches []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(
			filepath.Join(GetWorktreeBasePath(), e.Name(), FlattenName(name)),
		); err == nil {
			matches = append(matches, e.Name())
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}

// Remove removes a worktree and its tmux window.
// Works from any directory — first tries current repo, then searches the
// centralized base path so cross-repo removal works from anywhere.
func (w *WorktreeManager) Remove(name string, force bool) error {
	var repoSlug string

	// Try current repo first
	repoRoot, err := w.Git.GetRepoRoot()
	if err == nil {
		repoSlug = filepath.Base(repoRoot)
		state, stateErr := w.worktreeState(repoSlug, name)
		if stateErr == nil && (state.WtExists || state.WindowExists) {
			return w.removeByRepo(repoSlug, name, force)
		}
	}

	// Not in a git repo or worktree not in current repo — search centralized base path.
	if slug := w.findRepoForWorktree(name); slug != "" {
		return w.removeByRepo(slug, name, force)
	}

	// Last resort: the repo could not be determined, so we don't know the full
	// window name (wt-<repo>-<flat-name>). Match orphan windows by their trailing
	// "-<flat-name>" segment, keeping only those with the wt- prefix.
	var orphans []string
	for _, window := range w.Tmux.FindWindowsBySuffix("-" + FlattenName(name)) {
		if isWorktreeWindow(window) {
			orphans = append(orphans, window)
		}
	}
	switch len(orphans) {
	case 0:
		return fmt.Errorf("nothing to remove for worktree '%s'", name)
	case 1:
		_ = w.Tmux.KillWindow(orphans[0])
		return nil
	default:
		// Same worktree name across repos — killing an arbitrary match could
		// destroy the wrong active window. Require the caller to disambiguate.
		return fmt.Errorf(
			"multiple windows match '%s' (%s); run `dg wt remove` from the repo that owns it",
			name,
			strings.Join(orphans, ", "),
		)
	}
}

// repoSlugForWorktree resolves the repo slug that owns a worktree, first trying
// the current repo (if cwd is inside one) and falling back to a search of the
// centralized base path so it works from any directory or session.
func (w *WorktreeManager) repoSlugForWorktree(name string) string {
	if repoRoot, err := w.Git.GetRepoRoot(); err == nil {
		candidate := filepath.Base(repoRoot)
		if _, statErr := os.Stat(w.worktreePath(candidate, name)); statErr == nil {
			return candidate
		}
	}
	return w.findRepoForWorktree(name)
}

// Repair recreates the missing window for an existing worktree and rebuilds
// layout in it. The window is created in a tmux session named after the
// worktree's parent folder (the repo slug), creating that session if it does
// not exist. Works from any directory or session.
func (w *WorktreeManager) Repair(name string, layout Layout) error {
	if err := validateLayout(layout); err != nil {
		return err
	}

	repoSlug := w.repoSlugForWorktree(name)
	if repoSlug == "" {
		return fmt.Errorf("no worktree '%s' to repair", name)
	}

	wtPath := w.worktreePath(repoSlug, name)
	windowName := GetWindowName(repoSlug, name)

	// If directory doesn't exist on disk but git knows about it, prune and error
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		// Prune stale worktree entries
		if pruneErr := w.Git.PruneWorktreesAt(filepath.Dir(wtPath)); pruneErr != nil {
			return fmt.Errorf(
				"worktree '%s' directory missing and failed to prune: %w",
				name,
				pruneErr,
			)
		}
		return fmt.Errorf(
			"worktree '%s' directory was missing; pruned stale entry. Run `dg wt new %s` to recreate",
			name,
			name,
		)
	}

	return w.ensureWindow(repoSlug, windowName, wtPath, layout)
}

// RemoveInRepo deletes a worktree disambiguated by repo slug.
func (w *WorktreeManager) RemoveInRepo(repoSlug, name string, force bool) error {
	return w.removeByRepo(repoSlug, name, force)
}

// RemoveWithSessionInRepo force-deletes a worktree and also kills the tmux
// session that hosted its window. If the attached client is on that session,
// it is first moved to the fallback session (created on demand) so the
// terminal survives the kill. The fallback session itself is never killed.
func (w *WorktreeManager) RemoveWithSessionInRepo(repoSlug, name string) error {
	windowName := GetWindowName(repoSlug, name)
	session, hadWindow := w.Tmux.WindowSession(windowName)

	if err := w.removeByRepo(repoSlug, name, true); err != nil {
		return err
	}

	if !hadWindow || session == fallbackSession {
		return nil
	}
	// Killing the worktree window may have already destroyed the session
	// (tmux removes a session when its last window closes).
	if !w.Tmux.HasSession(session) {
		return nil
	}

	if current, ok := w.Tmux.CurrentSession(); ok && current == session {
		if !w.Tmux.HasSession(fallbackSession) {
			workdir, err := os.UserHomeDir()
			if err != nil {
				workdir = "/"
			}
			if err := w.Tmux.CreateSession(fallbackSession, workdir); err != nil {
				return fmt.Errorf(
					"worktree removed but session '%s' kept: failed to create fallback session '%s': %w",
					session,
					fallbackSession,
					err,
				)
			}
		}
		if err := w.Tmux.SwitchToSession(fallbackSession); err != nil {
			return fmt.Errorf(
				"worktree removed but session '%s' kept: failed to switch to '%s': %w",
				session, fallbackSession, err,
			)
		}
	}

	if err := w.Tmux.KillSession(session); err != nil {
		return fmt.Errorf("worktree removed but failed to kill session '%s': %w", session, err)
	}
	return nil
}

// RepairInRepo repairs a worktree in a specific repo, bypassing the slug-search ambiguity.
func (w *WorktreeManager) RepairInRepo(repoSlug, name string, layout Layout) error {
	if err := validateLayout(layout); err != nil {
		return err
	}
	wtPath := w.worktreePath(repoSlug, name)
	windowName := GetWindowName(repoSlug, name)
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		if pruneErr := w.Git.PruneWorktreesAt(filepath.Dir(wtPath)); pruneErr != nil {
			return fmt.Errorf(
				"worktree '%s' directory missing and failed to prune: %w",
				name,
				pruneErr,
			)
		}
		return fmt.Errorf(
			"worktree '%s' directory was missing; pruned stale entry. Run `dg wt new %s` to recreate",
			name,
			name,
		)
	}
	return w.ensureWindow(repoSlug, windowName, wtPath, layout)
}

// ensureWindow guarantees a tmux window for the worktree exists and reflects
// layout. If the window already lives in some session, it is reused - but
// only pane 0's command is (re)launched into it, never a full rebuild: an
// existing window may already have panes from a prior create/repair, and
// there is no way to tell from here whether those panes match layout's shape,
// so re-splitting would risk duplicating panes on every repair. Building the
// full layout (split, launch every pane, reselect pane 0) only happens when
// the window doesn't exist yet, in the worktree's repo-slug session (created
// when absent).
func (w *WorktreeManager) ensureWindow(repoSlug, windowName, wtPath string, layout Layout) error {
	session, exists := w.Tmux.WindowSession(windowName)
	if exists {
		if err := w.Tmux.SendKeysToWindowInSession(
			session,
			windowName,
			layout.Panes[0].Command,
		); err != nil {
			return fmt.Errorf("failed to launch %s: %w", layout.Name, err)
		}
		return nil
	}

	session = TmuxSessionName(repoSlug)
	if w.Tmux.HasSession(session) {
		if err := w.Tmux.CreateWindowInSession(session, windowName, wtPath); err != nil {
			return fmt.Errorf("failed to create tmux window: %w", err)
		}
	} else {
		if err := w.Tmux.CreateSessionWithWindow(session, windowName, wtPath); err != nil {
			return fmt.Errorf("failed to create tmux session: %w", err)
		}
	}

	sendKeys := func(command string) error {
		return w.Tmux.SendKeysToWindowInSession(session, windowName, command)
	}
	if err := w.buildWindowPanes(session+":"+windowName, wtPath, layout, sendKeys); err != nil {
		// Kill only the window, never the session: other worktrees' windows
		// may already live in this same repo-slug session.
		_ = w.Tmux.KillWindow(windowName)
		return err
	}
	return nil
}

// Prune removes all worktrees in the centralized directory
func (w *WorktreeManager) Prune() error {
	statuses, err := w.List()
	if err != nil {
		return err
	}

	if len(statuses) == 0 {
		fmt.Println("Nothing to prune")
		return nil
	}

	fmt.Println("The following worktrees will be removed:")
	for _, s := range statuses {
		fmt.Printf("  - %s/%s\n", s.Repo, s.Name)
	}

	fmt.Print("Remove all? [y/N]: ")
	if !confirmFromTTY() {
		return fmt.Errorf("cancelled")
	}

	var errors []string
	for _, s := range statuses {
		if err := w.removeByRepo(s.Repo, s.Name, false); err != nil {
			errors = append(errors, fmt.Sprintf("%s/%s: %v", s.Repo, s.Name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to remove some worktrees:\n  %s", strings.Join(errors, "\n  "))
	}

	return nil
}

// removeByRepo removes a worktree by repo slug and name.
// Mirrors the same tolerant logic as Remove.
func (w *WorktreeManager) removeByRepo(repoSlug, name string, force bool) error {
	wtPath := w.worktreePath(repoSlug, name)
	windowName := GetWindowName(repoSlug, name)

	state, err := w.worktreeState(repoSlug, name)
	if err != nil {
		return err
	}

	if !state.WtExists && !state.WindowExists {
		return nil
	}

	if state.WtExists && !force {
		dirty, err := w.Git.IsWorktreeDirty(wtPath)
		if err == nil && dirty {
			return fmt.Errorf(
				"worktree '%s' has uncommitted changes; use --force to remove anyway",
				name,
			)
		}
	}

	// Always try to kill the window, even if state check didn't find it
	// (state check may fail if not in tmux or window detection is unreliable)
	_ = w.Tmux.KillWindow(windowName)

	if state.WtExists {
		if err := w.Git.RemoveWorktree(wtPath, true, name); err != nil {
			if rmErr := os.RemoveAll(wtPath); rmErr != nil {
				return fmt.Errorf("failed to remove worktree: %w", err)
			}
		}
		// Prune from repo base dir (parent of worktree dirs)
		_ = w.Git.PruneWorktreesAt(filepath.Dir(wtPath))
	} else {
		_ = w.Git.PruneWorktreesAt(filepath.Dir(wtPath))
	}

	return nil
}

// confirmFromTTY reads a y/n answer directly from /dev/tty so it works even
// after fzf has consumed the process stdin (e.g. inside a tmux display-popup).
// In tmux popup mode, we skip confirmation since the user already pressed ctrl-d.
func confirmFromTTY() bool {
	if os.Getenv("TMUX") != "" {
		return true
	}
	tty, err := os.Open("/dev/tty")
	if err != nil {
		var response string
		if _, scanErr := fmt.Scanln(&response); scanErr != nil {
			return false
		}
		return strings.ToLower(strings.TrimSpace(response)) == "y"
	}
	var response string
	_, _ = fmt.Fscan(tty, &response)
	_ = tty.Close()
	return strings.ToLower(strings.TrimSpace(response)) == "y"
}

// GetWindowName returns the tmux window name for a worktree, scoped by repo slug.
//
// Worktree directories are already namespaced per repo
// (…/worktrees/<repo-slug>/<flat-name>), but tmux window names live in a single
// server-wide namespace. Without the repo scope, a worktree named after a shared
// ticket ID (e.g. "CXE-35") collides across repos: a leftover window from one repo
// makes `dg wt new CXE-35` in another repo fail with a false "orphan window" error.
//
// The repo prefix is sanitized with TmuxSessionName so it matches the session name
// used in ensureWindow, keeping window and session naming consistent.
func GetWindowName(repoSlug, name string) string {
	return windowPrefix + TmuxSessionName(repoSlug) + "-" + FlattenName(name)
}

// WindowNameFor resolves the repo that owns the given worktree and returns its
// repo-scoped tmux window name. It is for callers (e.g. the `dg wt repair`
// command) that have only the worktree name and need the window name for display.
func (w *WorktreeManager) WindowNameFor(name string) string {
	return GetWindowName(w.repoSlugForWorktree(name), name)
}

// GetWorktreeDir returns the worktree directory name (deprecated, use worktreePath instead)
func GetWorktreeDir() string {
	return ".worktrees"
}

// SelectWorktreeInteractively presents an fzf picker with available worktrees
// and returns the selected worktree name. Returns error if no worktrees exist
// or user cancels selection.
func (w *WorktreeManager) SelectWorktreeInteractively(prompt string) (string, error) {
	statuses, err := w.List()
	if err != nil {
		return "", fmt.Errorf("failed to list worktrees: %w", err)
	}

	if len(statuses) == 0 {
		return "", fmt.Errorf("no worktrees available")
	}

	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = s.Name
	}

	selected, err := w.Fzf.SelectFromList(names, prompt)
	if err != nil {
		return "", fmt.Errorf("selection failed: %w", err)
	}

	return selected, nil
}

// recordRepoUsed best-effort upserts repoRoot into the recent-repos store so
// the worktree picker can offer this repo again later, even after every
// worktree under it has been removed. This never fails create: repoRoot's
// worktree and tmux window already exist by the time this runs, so a store
// write failure here is a degraded-but-working outcome, not a create
// failure. The failure is still surfaced (never silently swallowed): via
// WarnFn (CLI prints it, a TUI caller can route it to a toast) and always via
// a debug log entry.
func (w *WorktreeManager) recordRepoUsed(repoRoot string) {
	canonical := config.CanonicalRepoPath(repoRoot)

	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		w.warnRepoRecordFailure(canonical, err)
		return
	}
	if err := gc.Load(); err != nil {
		w.warnRepoRecordFailure(canonical, err)
		return
	}
	gc.Worktree.UpsertRecentRepo(canonical, time.Now())
	if err := gc.Save(); err != nil {
		w.warnRepoRecordFailure(canonical, err)
		return
	}
}

// warnRepoRecordFailure reports a recordRepoUsed failure through WarnFn
// (falling back to utils.PrintWarning when unset, so CLI callers get a
// sensible default even if a WorktreeManager was constructed as a literal
// instead of via New) and always logs it at debug level.
func (w *WorktreeManager) warnRepoRecordFailure(repoRoot string, err error) {
	logger.L().Debugw("failed to record recent repo", "repo", repoRoot, "error", err)
	warn := w.WarnFn
	if warn == nil {
		warn = utils.PrintWarning
	}
	warn(
		fmt.Sprintf(
			"worktree created, but failed to remember repo %s for later reuse: %v",
			repoRoot,
			err,
		),
	)
}
