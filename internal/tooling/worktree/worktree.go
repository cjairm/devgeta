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

// sharedWorktreePath returns ~/.local/share/devgeta/worktrees/<repo-slug>/<flat-name> -
// the WorktreeLocationShared path shape (today's only shape, and still the
// default). Slashes in name are replaced with dashes to keep the worktree
// directory directly under the repo slug. This ensures the parent directory
// is always the repo slug (important for tools that display the parent dir,
// e.g. Claude Code). The single implementation of this shape - every caller
// that needs it (worktreePath, worktreePathIn) goes through here.
func sharedWorktreePath(repoSlug, name string) string {
	return filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, FlattenName(name))
}

// inRepoWorktreePath returns <repo-root>/.claude/worktrees/<flat-name> - the
// WorktreeLocationInRepo path shape. Unlike the shared shape, there is no
// repo-slug segment: repoRoot already identifies the repo. The single
// implementation of this shape - every caller that needs it (worktreePath,
// worktreePathIn) goes through here.
func inRepoWorktreePath(repoRoot, name string) string {
	return filepath.Join(repoRoot, ".claude", "worktrees", FlattenName(name))
}

// worktreePathIn returns the on-disk worktree path for name inside the repo
// rooted at repoRoot, honoring gc.Worktree.Location. Unlike worktreePath
// (below), it already holds the repo root - no slug->root resolution is
// needed, so it cannot fail. Callers that already have repoRoot in hand
// (e.g. create, which derives repoSlug from repoRoot on the very next line
// anyway) should use this directly instead of round-tripping
// root->slug->root through worktreePath: that round trip is not just
// wasteful but lossy, since two repos sharing a basename in different
// parents would collapse to the same slug.
//
// Loads the global config itself (see worktreePath's doc comment for why),
// tolerating a load failure as "no config yet, use the shared default".
func worktreePathIn(repoRoot, name string) string {
	gc := &config.GlobalConfig{}
	// A load failure (e.g. no config file yet) leaves gc at its zero value,
	// which already means "shared" (Location's zero value) - the correct
	// default, not an actionable error.
	_ = gc.Load()
	if gc.Worktree.Location == config.WorktreeLocationInRepo {
		return inRepoWorktreePath(repoRoot, name)
	}
	return sharedWorktreePath(filepath.Base(repoRoot), name)
}

// worktreePath resolves the on-disk worktree path for repoSlug's name,
// honoring gc.Worktree.Location: WorktreeLocationShared (the default, and
// today's only behavior) needs nothing beyond repoSlug and cannot fail. For
// WorktreeLocationInRepo, repoSlug (a bare directory basename) is not enough
// - it must first be resolved back to an absolute repo root (resolveRepoRoot)
// so the path can be nested under that repo's own .claude/worktrees. If the
// root cannot be resolved, this returns an actionable error rather than
// falling back to the shared path: silently doing so would recreate exactly
// the split-brain state (a worktree devgeta creates in one place and looks
// for in another) that this cycle exists to eliminate.
//
// Loads the global config itself via gc := &config.GlobalConfig{}; gc.Load()
// (repo_candidates.go's established pattern), tolerating a load failure as
// "no config yet, use defaults" the same way RepoCandidates does - rather
// than accepting a gc parameter the way ResolveLayout does. None of this
// function's 7 call sites already holds a *config.GlobalConfig in scope, so
// threading one through would only push a load onto every caller for no
// benefit; ResolveLayout's callers, by contrast, already have one on hand
// when they call it.
func (w *WorktreeManager) worktreePath(repoSlug, name string) (string, error) {
	gc := &config.GlobalConfig{}
	_ = gc.Load() // no config yet, or a transient read error: default to shared
	if gc.Worktree.Location != config.WorktreeLocationInRepo {
		return sharedWorktreePath(repoSlug, name), nil
	}

	repoRoot, err := w.resolveRepoRoot(gc, repoSlug)
	if err != nil {
		return "", err
	}
	return worktreePathIn(repoRoot, name), nil
}

// resolveRepoRoot resolves repoSlug (a bare directory basename, e.g.
// "devgeta") back to the absolute repo root worktreePath's in-repo location
// needs. cursorRepoRoot (repo_candidates.go) is not reusable for this: it
// resolves a root by reading a worktree directory already living under the
// shared root, which is circular here - computing where a worktree should
// live must not require one already living somewhere else first.
//
// Resolution order, cheapest and most certain first:
//  1. the current repo (w.Git.GetRepoRoot()), if its basename matches repoSlug
//  2. gc.Worktree.RecentRepos, first entry whose basename matches - already
//     in memory from the caller's config load, so this is neither a new
//     state nor a filesystem walk
//
// Returns an actionable error (naming the slug) when neither source
// resolves it - never a fallback to the shared-root trick.
func (w *WorktreeManager) resolveRepoRoot(
	gc *config.GlobalConfig,
	repoSlug string,
) (string, error) {
	if root, err := w.Git.GetRepoRoot(); err == nil && filepath.Base(root) == repoSlug {
		return root, nil
	}
	for _, r := range gc.Worktree.RecentRepos {
		if filepath.Base(r.Path) == repoSlug {
			return r.Path, nil
		}
	}
	return "", fmt.Errorf(
		"worktree location is 'in-repo' but repo %q is not known to devgeta yet "+
			"(it isn't the current repo and isn't in the recent-repos store); "+
			"cd into the repo, or use a command/flag that takes the repo path "+
			"directly (e.g. --repo <path>) instead of a bare name",
		repoSlug,
	)
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
	// repoRoot is already known here, so worktreePathIn is used directly
	// instead of round-tripping root->slug->root through the slug-based
	// worktreePath - see worktreePathIn's doc comment for why that round
	// trip would be both wasteful and lossy.
	wtPath := worktreePathIn(repoRoot, name)
	windowName := GetWindowName(repoSlug, name)

	// repoRoot is already known here, so worktreeStateIn is used directly
	// instead of round-tripping root->slug->root through the slug-based
	// worktreeState - see worktreeStateIn's doc comment. Before this, the slug
	// round trip could fail create() outright on its very first invocation for
	// a repo passed via --repo (CreateAt): resolveRepoRoot can only resolve a
	// bare slug back to a root via the current directory or the recent-repos
	// store, and recordRepoUsed only populates that store *after* a successful
	// create - so a brand-new --repo target had no way to satisfy either path.
	state := w.worktreeStateIn(repoRoot, name)

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

// worktreeState checks the current state of a worktree. Propagates
// worktreePath's error unchanged (e.g. an unresolvable in-repo root) rather
// than reporting a false "doesn't exist" - a caller must not treat "we
// couldn't determine where this worktree would live" as "it doesn't exist".
//
// For a caller that already holds repoRoot in scope (e.g. create, Remove),
// prefer worktreeStateIn below: it skips this function's slug->root
// resolution entirely (via worktreePathIn instead of worktreePath) and so
// cannot fail on an unresolvable slug in the first place.
func (w *WorktreeManager) worktreeState(repoSlug, name string) (WorktreeState, error) {
	wtPath, err := w.worktreePath(repoSlug, name)
	if err != nil {
		return WorktreeState{}, err
	}
	return w.worktreeStateFor(wtPath, GetWindowName(repoSlug, name)), nil
}

// worktreeStateIn is worktreeState's root-taking counterpart, mirroring how
// worktreePathIn relates to worktreePath: a caller that already resolved
// repoRoot (create, Remove) uses worktreePathIn directly instead of
// round-tripping root->slug->root through the fallible worktreePath, so this
// cannot fail - hence no error return, unlike worktreeState.
func (w *WorktreeManager) worktreeStateIn(repoRoot, name string) WorktreeState {
	repoSlug := filepath.Base(repoRoot)
	wtPath := worktreePathIn(repoRoot, name)
	return w.worktreeStateFor(wtPath, GetWindowName(repoSlug, name))
}

// worktreeStateFor computes a WorktreeState for a worktree already known to
// live at wtPath under window windowName - the state-inspection logic shared
// by worktreeState (slug-based, fallible root resolution) and worktreeStateIn
// (root-taking, infallible), so the two path-resolution shapes don't each
// carry their own copy of it.
func (w *WorktreeManager) worktreeStateFor(wtPath, windowName string) WorktreeState {
	state := WorktreeState{
		WtPath:     wtPath,
		WindowName: windowName,
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

	return state
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

// anchorGroup is a set of candidate anchor paths that are all believed to
// belong to the same repo. List() tries the anchors in a group in order and
// stops at the first one that resolves (a husk sibling never hides a good
// one), so a group's cost is one exec in the common case where the first
// anchor tried is real, and only grows if earlier anchors in the same group
// fail.
type anchorGroup []string

// knownRepoAnchorGroups collects one anchor group per repo devgeta knows
// about - each anchor an actual worktree (main or linked) of that repo,
// since `git worktree list --porcelain` resolves the whole repo's worktree
// set from any single one of its worktrees (GetMainWorktree's own doc
// comment relies on the same guarantee). Reused by List() (below) and, per
// the cycle plan, by findRepoForWorktree/the repo picker in a later step
// (not wired up here).
//
// Groups are collected, in this order (order only affects which group "wins"
// List()'s dedup, which itself doesn't matter since that dedup is by
// resolved main root, not by anchor):
//
//  1. the current repo (cwdRepoRoot, repo_candidates.go - reused rather than
//     reimplemented with GetRepoRoot) - a single-anchor group, since it's
//     already known to be one specific repo's root.
//  2. the recent-repos store (gc.Worktree.PrunedRecentRepos(), already
//     filters paths no longer on disk - reused rather than reimplemented). A
//     config load failure is tolerated as "no config yet, skip this source",
//     matching RepoCandidates' own convention. Each recent repo is likewise a
//     single-anchor group.
//  3. one group per shared-root repo-slug directory, containing every
//     subdirectory found under that slug - not just the first. A repo-slug
//     directory can contain a husk left behind by a botched move; taking
//     only the first subdirectory found and having it turn out to be the
//     husk would make List() give up on the entire slug, hiding every real
//     worktree that repo has. Grouping every subdirectory under the slug
//     into one group, and having List() try them in order with an early
//     exit on the first success, means the common case (first subdirectory
//     is a real worktree) costs exactly one exec, while a husk sibling still
//     can't hide a good one under the same slug.
//
// Anchors are NOT deduplicated here: two anchors can't be known to resolve to
// the same repo until git is asked, so dedup happens on the query result (in
// List()), never on this list.
func (w *WorktreeManager) knownRepoAnchorGroups() []anchorGroup {
	var groups []anchorGroup

	if cwdRoot := w.cwdRepoRoot(); cwdRoot != "" {
		groups = append(groups, anchorGroup{cwdRoot})
	}

	gc := &config.GlobalConfig{}
	if err := gc.Load(); err == nil {
		for _, r := range gc.Worktree.PrunedRecentRepos() {
			groups = append(groups, anchorGroup{r.Path})
		}
	}

	basePath := GetWorktreeBasePath()
	slugEntries, err := os.ReadDir(basePath)
	if err == nil {
		for _, slugEntry := range slugEntries {
			if !slugEntry.IsDir() {
				continue
			}
			repoDir := filepath.Join(basePath, slugEntry.Name())
			wtEntries, err := os.ReadDir(repoDir)
			if err != nil {
				continue
			}
			var group anchorGroup
			for _, wtEntry := range wtEntries {
				if !wtEntry.IsDir() {
					continue
				}
				group = append(group, filepath.Join(repoDir, wtEntry.Name()))
			}
			if len(group) > 0 {
				groups = append(groups, group)
			}
		}
	}

	return groups
}

// enumerateWorktrees resolves every worktree across every known repo -
// group->anchor->ListWorktreesAt->dedup-by-main-root chain, exactly as
// documented on knownRepoAnchorGroups: for each known repo anchor group,
// anchors in the group are tried in order and the first successful
// ListWorktreesAt call returns that repo's whole worktree set (path and
// branch) - sibling anchors in the same group are only tried as a fallback
// when an earlier one fails (e.g. a husk). So this costs one exec per known
// repo in the common case, not one per worktree - and, unlike a directory
// walk, a husk directory git does not report as a worktree can never appear
// (see ADR-0010,
// docs/decisions/ADR-0010-worktree-layout-is-a-setting-git-is-the-index.md).
//
// Only Name/Path/Branch/Repo are populated on the returned WorktreeStatus
// values; TmuxWindow/WindowActive/AgentState are left at their zero values -
// no tmux exec happens in this function at all. This is the single
// enumeration implementation List() (the tmux-aware view) and
// findRepoForWorktree (a tmux-agnostic search) both build on, instead of each
// walking git separately.
func (w *WorktreeManager) enumerateWorktrees() []WorktreeStatus {
	seenRoots := make(map[string]bool)
	statuses := []WorktreeStatus{}
	for _, group := range w.knownRepoAnchorGroups() {
		for _, anchor := range group {
			worktrees, err := w.Git.ListWorktreesAt(anchor)
			if err != nil || len(worktrees) == 0 {
				// Not a real worktree (husk, deleted, never existed) - try
				// the next anchor in this same group, if any, before giving
				// up on the group entirely.
				continue
			}
			// git worktree list --porcelain always lists the main worktree
			// first - a documented, stable git guarantee (GetMainWorktree
			// relies on the same one), not an assumption to hedge.
			mainRoot := worktrees[0].Path
			if seenRoots[mainRoot] {
				// This repo was already processed via an earlier group -
				// move on to the next group without querying its remaining
				// anchors.
				break
			}
			seenRoots[mainRoot] = true

			// Matches create()'s `repoSlug := filepath.Base(repoRoot)` -
			// must be the identical derivation used elsewhere, or
			// Repo/TmuxWindow won't match what create/repair/remove expect.
			repoSlug := filepath.Base(mainRoot)
			for _, wt := range worktrees {
				if wt.Path == mainRoot {
					// The repo's own checkout is not a devgeta-managed worktree row.
					continue
				}
				// filepath.Base(wt.Path) gives the right Name regardless of
				// location: for shared it's <basePath>/<slug>/<flat-name> ->
				// <flat-name>; for in-repo it's
				// <repo-root>/.claude/worktrees/<flat-name> -> <flat-name>. Both
				// path shapes put the flattened name as the final path segment,
				// so no location branching is needed here.
				name := filepath.Base(wt.Path)
				statuses = append(statuses, WorktreeStatus{
					Name:   name,
					Path:   wt.Path,
					Branch: wt.Branch,
					Repo:   repoSlug,
				})
			}

			// This group resolved successfully - the successful anchor's
			// result already has the repo's complete worktree list, so
			// there's no reason to query sibling anchors in the same group.
			break
		}
	}

	return statuses
}

// List returns all worktrees with their window status across all repos.
// enumerateWorktrees does all the git-backed resolution (see its doc
// comment); List's own job is layering tmux state on top via a single
// PaneStates() scan, instead of a WindowSession() call per worktree - each of
// which ran its own fresh list-windows -a scan (N tmux execs per 3-second
// refresh). paneStatesByWindow is indexed by window name: a window's
// presence as a key is the direct equivalent of "does a window with this
// name exist" (PaneStates enumerates every pane on the server, and a window
// with zero panes cannot exist in tmux), and its slice of pane states is
// what aggregateAgentState reduces to AgentState. See ADR-0005.
func (w *WorktreeManager) List() ([]WorktreeStatus, error) {
	paneStatesByWindow := make(map[string][]string)
	for _, ps := range w.Tmux.PaneStates() {
		paneStatesByWindow[ps.Window] = append(paneStatesByWindow[ps.Window], ps.State)
	}

	statuses := w.enumerateWorktrees()
	for i := range statuses {
		windowName := GetWindowName(statuses[i].Repo, statuses[i].Name)
		// Comma-ok reports key presence, not slice non-emptiness: a window
		// in the index always has at least one pane state appended above, so
		// ok alone is the "window exists" signal.
		states, windowActive := paneStatesByWindow[windowName]
		statuses[i].TmuxWindow = windowName
		statuses[i].WindowActive = windowActive
		statuses[i].AgentState = aggregateAgentState(states)
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

// findRepoForWorktree searches every known repo (via enumerateWorktrees, the
// same git-backed enumeration List() uses - not a shared-root directory
// scan, which could never see an in-repo-located worktree) for one named
// name, and returns the repo slug that owns it. Returns "" if not found or
// ambiguous (2+ distinct repos have a worktree by this name): Remove and
// repoSlugForWorktree both treat "" as "cannot safely act", so this must
// never loosen to "first match wins".
func (w *WorktreeManager) findRepoForWorktree(name string) string {
	flat := FlattenName(name)
	var matches []string
	for _, wt := range w.enumerateWorktrees() {
		if wt.Name == flat {
			matches = append(matches, wt.Repo)
		}
	}

	// Dedupe to distinct repo slugs before applying the exactly-one-match
	// rule below: the same repo can appear twice here if it has two
	// worktrees that happen to share this flattened name (e.g. one
	// shared-root, one in-repo, left over from a location change) - that's
	// one repo owning the name, not an ambiguity between two repos.
	seen := make(map[string]bool, len(matches))
	var distinct []string
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			distinct = append(distinct, m)
		}
	}

	if len(distinct) == 1 {
		return distinct[0]
	}
	return ""
}

// Remove removes a worktree and its tmux window.
// Works from any directory — first tries current repo, then searches the
// centralized base path so cross-repo removal works from anywhere.
func (w *WorktreeManager) Remove(name string, force bool) error {
	var repoSlug string

	// Try current repo first. repoRoot is already known here, so
	// worktreeStateIn is used directly instead of the slug-based
	// worktreeState - see worktreeStateIn's doc comment on create() for why
	// that round trip is both wasteful and (for an in-repo location) can fail
	// outright.
	repoRoot, err := w.Git.GetRepoRoot()
	if err == nil {
		repoSlug = filepath.Base(repoRoot)
		state := w.worktreeStateIn(repoRoot, name)
		if state.WtExists || state.WindowExists {
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

// resolveWorktreeRepoForMove finds the repo that owns a worktree named name,
// mirroring Remove()'s resolution order exactly: the current repo first
// (cheapest and most certain via w.Git.GetRepoRoot() + worktreeStateIn), then
// a fallback search across every known repo via findRepoForWorktree. Unlike
// Remove, there is no last-resort "orphan window, unknown repo" path: move
// operates on a real git worktree, so failing both steps is a plain "no such
// worktree" error, not an orphan-window cleanup. repoRoot is returned
// non-empty only when resolution succeeded via the current-repo branch (it
// is already known there for free); the fallback branch returns it empty,
// since findRepoForWorktree only yields a repo slug - Move resolves a root
// lazily afterward, only if it turns out to be needed.
func (w *WorktreeManager) resolveWorktreeRepoForMove(
	name string,
) (repoSlug, repoRoot string, err error) {
	if root, gerr := w.Git.GetRepoRoot(); gerr == nil {
		state := w.worktreeStateIn(root, name)
		if state.WtExists || state.WindowExists {
			return filepath.Base(root), root, nil
		}
	}

	if slug := w.findRepoForWorktree(name); slug != "" {
		return slug, "", nil
	}

	return "", "", fmt.Errorf("no such worktree '%s'", name)
}

// currentWorktreePath finds which of the two possible location shapes
// (sharedWorktreePath, inRepoWorktreePath) a worktree named name actually
// occupies on disk right now - checked directly against reality, never
// inferred from the CURRENTLY CONFIGURED worktree.location the way
// worktreePath/worktreeStateIn do. That distinction matters specifically for
// Move: its whole purpose is relocating a worktree that is NOT YET at the
// location the config now names (the maintainer's hand-migration incident
// this cycle fixes - config was changed, but the worktree hadn't moved yet).
// Computing "where should it be, per config" in that situation would find
// the not-yet-existing target, not the worktree to move FROM. repoRoot, when
// known, lets the in-repo candidate be checked too; when empty (name
// resolved via the slug-only fallback path, no root resolved yet), only the
// shared candidate is checked. Returns ok=false if neither candidate is a
// real worktree.
func (w *WorktreeManager) currentWorktreePath(repoSlug, repoRoot, name string) (string, bool) {
	if shared := sharedWorktreePath(repoSlug, name); w.isRealWorktreeAt(shared) {
		return shared, true
	}
	if repoRoot != "" {
		if inRepo := inRepoWorktreePath(repoRoot, name); w.isRealWorktreeAt(inRepo) {
			return inRepo, true
		}
	}
	return "", false
}

// isRealWorktreeAt reports whether path is a real, git-tracked worktree -
// git's own worktree list is authoritative (ADR-0010, "git is the index"),
// so a match there is trusted even if the directory has, say, been moved out
// from under git's expectation. When git itself can't run at path at all
// (e.g. the directory doesn't exist yet, so `-C path` fails outright), that
// is treated as "no answer from git" and a plain directory check is the
// fallback - mirroring worktreeStateFor's own tolerance for this same
// failure mode.
func (w *WorktreeManager) isRealWorktreeAt(path string) bool {
	worktrees, err := w.Git.ListWorktreesAt(path)
	if err == nil {
		for _, wt := range worktrees {
			if wt.Path == path {
				return true
			}
		}
		return false
	}
	info, statErr := os.Stat(path)
	return statErr == nil && info.IsDir()
}

// effectiveMoveLocation resolves --to into a concrete target location: the
// flag value when given, otherwise the configured worktree.location
// (defaulting to shared when empty, per config.WorktreeConfig.Location's own
// semantics, unchanged here) - never "the other" location. A bare
// `dg wt move <name>` means "bring this worktree in line with my configured
// layout," which is the actual migration case.
func effectiveMoveLocation(to string, gc *config.GlobalConfig) (string, error) {
	switch to {
	case "":
		if gc.Worktree.Location == config.WorktreeLocationInRepo {
			return config.WorktreeLocationInRepo, nil
		}
		return config.WorktreeLocationShared, nil
	case config.WorktreeLocationShared, config.WorktreeLocationInRepo:
		return to, nil
	default:
		return "", fmt.Errorf(
			"invalid --to value %q (want %q or %q)",
			to, config.WorktreeLocationShared, config.WorktreeLocationInRepo,
		)
	}
}

// Move relocates a worktree between the shared and in-repo location shapes
// and retargets its tmux window (if one exists) to the new path. to selects
// the destination explicitly (config.WorktreeLocationShared or
// config.WorktreeLocationInRepo); "" means "the configured
// worktree.location" (see effectiveMoveLocation). Refuses on a dirty
// worktree unless force is set. Returns moved=false (with err==nil) when the
// worktree is already at the destination - the no-op case is reported here
// as a plain message via utils.PrintInfo, not as an error, so a caller
// doesn't print a second "moved" message on top of it. The git-level move
// (Git.MoveWorktree) always happens before any tmux retargeting is
// attempted, and a retargeting problem (a busy pane, or no window at all)
// never turns a successful move into a reported failure - see
// retargetWindowAfterMove's doc comment.
func (w *WorktreeManager) Move(name, to string, force bool) (bool, error) {
	repoSlug, repoRoot, err := w.resolveWorktreeRepoForMove(name)
	if err != nil {
		return false, err
	}

	gc := &config.GlobalConfig{}
	_ = gc.Load() // no config yet, or a transient read error: default to shared

	if repoRoot == "" {
		// The fallback resolution above only yielded a slug. Best-effort
		// resolve the root too, so the in-repo candidate can be checked
		// below and an in-repo destination can be computed later without a
		// second resolution attempt. A failure here isn't fatal yet: the
		// shared candidate/destination may be all that's needed.
		if root, rErr := w.resolveRepoRoot(gc, repoSlug); rErr == nil {
			repoRoot = root
		}
	}

	fromPath, ok := w.currentWorktreePath(repoSlug, repoRoot, name)
	if !ok {
		return false, fmt.Errorf("no such worktree '%s'", name)
	}

	location, err := effectiveMoveLocation(to, gc)
	if err != nil {
		return false, err
	}

	if location == config.WorktreeLocationInRepo && repoRoot == "" {
		root, rErr := w.resolveRepoRoot(gc, repoSlug)
		if rErr != nil {
			return false, rErr
		}
		repoRoot = root
	}

	var toPath string
	if location == config.WorktreeLocationInRepo {
		toPath = inRepoWorktreePath(repoRoot, name)
	} else {
		toPath = sharedWorktreePath(repoSlug, name)
	}

	if fromPath == toPath {
		utils.PrintInfo(fmt.Sprintf("worktree '%s' is already at %s", name, toPath))
		return false, nil
	}

	if !force {
		dirty, dirtyErr := w.Git.IsWorktreeDirty(fromPath)
		if dirtyErr != nil {
			return false, fmt.Errorf(
				"failed to check worktree '%s' for uncommitted changes: %w",
				name, dirtyErr,
			)
		}
		if dirty {
			return false, fmt.Errorf(
				"worktree '%s' has uncommitted changes; use --force to move anyway",
				name,
			)
		}
	}

	if location == config.WorktreeLocationInRepo {
		w.warnIfWorktreesNotIgnored(repoRoot)
	}

	if err := os.MkdirAll(filepath.Dir(toPath), 0o755); err != nil {
		return false, fmt.Errorf("failed to create destination directory for '%s': %w", name, err)
	}

	if err := w.Git.MoveWorktree(fromPath, toPath); err != nil {
		return false, fmt.Errorf("failed to move worktree '%s': %w", name, err)
	}

	w.retargetWindowAfterMove(repoSlug, name, toPath)

	return true, nil
}

// warnIfWorktreesNotIgnored warns (never refuses) when repoRoot's effective
// .gitignore does not already ignore .claude/worktrees/ - the directory
// --to in-repo moves a worktree into. devgeta must never edit another repo's
// .gitignore itself (ADR-0010), so this only prints a suggestion and lets
// the move proceed regardless. A check-ignore failure (e.g. repoRoot isn't a
// git repo, which shouldn't happen here but isn't this function's job to
// diagnose) is tolerated as "can't tell, skip the warning" - best-effort,
// like the rest of Move's non-fatal warnings.
func (w *WorktreeManager) warnIfWorktreesNotIgnored(repoRoot string) {
	ignored, err := w.Git.IsPathIgnored(repoRoot, filepath.Join(".claude", "worktrees"))
	if err != nil || ignored {
		return
	}
	w.warn(fmt.Sprintf(
		"%s is not gitignored in %s; add it to .gitignore to keep worktree state out of commits",
		filepath.Join(".claude", "worktrees"),
		repoRoot,
	))
}

// warn reports msg via WarnFn, falling back to utils.PrintWarning when unset
// - the same fallback warnRepoRecordFailure already relies on, factored out
// here since Move needs it for two distinct warnings (gitignore, busy pane).
func (w *WorktreeManager) warn(msg string) {
	warnFn := w.WarnFn
	if warnFn == nil {
		warnFn = utils.PrintWarning
	}
	warnFn(msg)
}

// idleShellCommands is the allowlist of pane_current_command values treated
// as "safe to retarget" - a plain interactive shell prompt with nothing
// running in it. This is deliberately an allowlist of known-idle shells, not
// a denylist/heuristic for "is an agent running": ADR-0008
// (docs/decisions/ADR-0008-agent-state-on-every-pane-row.md) documents, from
// a real live capture, that Claude Code's own pane_current_command reports a
// bare version string ("2.1.220" - tmux's automatic-rename following the
// versioned binary directory), not a recognizable process name - so any
// attempt to detect "an agent is running" by matching a process name is
// already broken on the shipped installer and would break again on the next
// release. An allowlist of known-idle shells sidesteps this entirely:
// anything that isn't a plain shell prompt - an editor, a build, or an
// agent's odd self-report - is correctly treated as "something is running
// here," without needing to special-case any one program.
var idleShellCommands = map[string]bool{
	"zsh":  true,
	"bash": true,
	"fish": true,
	"sh":   true,
}

// isIdleShellPane reports whether currentCommand (a pane's
// #{pane_current_command}) looks like a bare interactive shell prompt - the
// only case a pane returned by PanesInWindow is safe to `cd` into. Beyond
// the fixed allowlist above, the basename of $SHELL (the user's own default
// shell) is also accepted, covering a shell tmux reports under a name this
// list didn't anticipate.
func isIdleShellPane(currentCommand string) bool {
	if idleShellCommands[currentCommand] {
		return true
	}
	if shell := os.Getenv("SHELL"); shell != "" {
		return filepath.Base(shell) == currentCommand
	}
	return false
}

// retargetWindowAfterMove sends `cd <newPath>` to every pane of the
// worktree's tmux window - but only if a window exists AND every pane in it
// is an idle shell. tmux respawn-pane is deliberately never used here: it
// would kill whatever is running in the pane, including a live agent
// session, to satisfy a path change - exactly the harm this command exists
// to prevent (see the cycle's motivating incident: a hand-migrated worktree
// left a tmux shell pointing at a deleted directory, where an agent could
// have run git commands against the wrong repo). If even one pane is not an
// idle shell, NOTHING is sent to ANY pane in the window - a partial
// retarget would leave a more confusing half-updated window than doing
// nothing - and a warning names the busy pane(s). A window that doesn't
// exist at all is silently skipped (nothing to update). Either outcome is
// reported as a warning (or silence), never an error: the git-level move has
// already succeeded by the time this runs, and a stale tmux shell is a
// follow-up inconvenience, never a reason to fail the whole command or roll
// back the move.
func (w *WorktreeManager) retargetWindowAfterMove(repoSlug, name, newPath string) {
	windowName := GetWindowName(repoSlug, name)
	panes := w.Tmux.PanesInWindow(windowName)
	if len(panes) == 0 {
		// No window for this worktree - nothing to retarget.
		return
	}

	var busy []string
	for _, p := range panes {
		if !isIdleShellPane(p.CurrentCommand) {
			busy = append(busy, fmt.Sprintf("%s (%s)", p.PaneID, p.CurrentCommand))
		}
	}
	if len(busy) > 0 {
		w.warn(fmt.Sprintf(
			"worktree moved, but window %s has a busy pane and was not retargeted (%s); "+
				"cd to %s manually once idle",
			windowName, strings.Join(busy, ", "), newPath,
		))
		return
	}

	for _, p := range panes {
		if err := w.Tmux.SendKeysToPane(p.PaneID, cdCommand(newPath)); err != nil {
			w.warn(fmt.Sprintf(
				"worktree moved, but failed to retarget pane %s in window %s: %v",
				p.PaneID, windowName, err,
			))
		}
	}
}

// cdCommand builds a `cd` command for tmux send-keys with newPath safely
// single-quoted via shellSingleQuote (layout.go's existing quoting helper -
// reused, not duplicated), so a destination containing spaces or shell
// metacharacters (e.g. a repo root under a directory a user happened to name
// with a space) still lands as one argument instead of being split by the
// pane's shell.
func cdCommand(newPath string) string {
	return "cd " + shellSingleQuote(newPath)
}

// repoSlugForWorktree resolves the repo slug that owns a worktree, first trying
// the current repo (if cwd is inside one) and falling back to a search of the
// centralized base path so it works from any directory or session.
func (w *WorktreeManager) repoSlugForWorktree(name string) string {
	if repoRoot, err := w.Git.GetRepoRoot(); err == nil {
		candidate := filepath.Base(repoRoot)
		// An error here just means "not this candidate" (e.g. an
		// unresolvable in-repo root, which can't happen for candidate since
		// it was derived from repoRoot a line above - this guards the
		// theoretical case anyway rather than assuming it away), not a
		// failure to propagate: falling through to findRepoForWorktree is
		// exactly the existing fallback for "current repo doesn't have it".
		if path, pathErr := w.worktreePath(candidate, name); pathErr == nil {
			if _, statErr := os.Stat(path); statErr == nil {
				return candidate
			}
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

	wtPath, err := w.worktreePath(repoSlug, name)
	if err != nil {
		return err
	}
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
	wtPath, err := w.worktreePath(repoSlug, name)
	if err != nil {
		return err
	}
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

// LaunchReviewInRepo gets the reviewer agent registered under reviewerKey
// running against name's branch, in the right place in tmux, disambiguated
// by repo slug (mirrors RepairInRepo). Two cases, per the cycle plan's Step
// 3:
//
//   - No live window: the review command becomes the window's only pane,
//     built via the same create-if-missing path ensureWindow already gives
//     Create/Repair/RepairInRepo.
//   - Live window: a new pane is split off and the review command is sent
//     there - never into the window's existing pane, which may already be
//     running a coder's interactive TUI (send-keys would type the review
//     command into its composer instead of executing it).
//
// A failed launch in the live-window case rolls back only what this call
// added - the new pane it just split - never the user's window, its
// existing pane, or the worktree: unlike buildWindowFromLayout's rollback
// (which also kills the window and removes the worktree it just created),
// R never creates a worktree, and the window here is the user's, already
// holding their work.
func (w *WorktreeManager) LaunchReviewInRepo(repoSlug, name, reviewerKey string) error {
	reviewCmd, err := ReviewCommand(reviewerKey)
	if err != nil {
		return err
	}

	// A review is always launched via OpenCode (the "oc" alias), regardless
	// of the worktree's own layout - a user whose default layout is
	// claude/claude-nvim has never needed oc, so it may never have been
	// installed. Checking here, before any tmux call, turns that into one
	// actionable error instead of a pane that prints "oc: command not found"
	// while the dashboard reports "review started" with no error anywhere.
	// EnsureInstalled checks the "oc" alias itself (see its doc comment), not
	// the raw "opencode" binary, so a coder installed outside devgeta (whose
	// alias was never written to devgeta.zsh) correctly fails this check.
	if err := (&OpenCodeCoder{}).EnsureInstalled(); err != nil {
		return err
	}

	wtPath, err := w.worktreePath(repoSlug, name)
	if err != nil {
		return err
	}
	windowName := GetWindowName(repoSlug, name)

	session, exists := w.Tmux.WindowSession(windowName)
	if !exists {
		// Goes straight to createWindowWithLayout, not ensureWindow: this
		// call's own WindowSession check just above already established the
		// window doesn't exist, so routing through ensureWindow (which would
		// query WindowSession again internally) would open a race - if
		// something else creates this exact window in the gap between the
		// two queries, ensureWindow's existing-window branch would send the
		// review command via a window-targeted send-keys into whatever pane
		// is active, exactly the unsafe behavior this cycle exists to
		// prevent. This ad-hoc one-pane layout has no install checker to run
		// (a review launch never introduces a new tool to check for beyond
		// the oc check above).
		layout := Layout{
			Name:  "review-" + reviewerKey,
			Panes: []Pane{{Command: reviewCmd}},
		}
		return w.createWindowWithLayout(repoSlug, windowName, wtPath, layout)
	}

	return w.launchReviewInLiveWindow(session, windowName, wtPath, reviewCmd)
}

// launchReviewInLiveWindow splits a new pane off an existing (live) window
// and launches reviewCmd there, restoring focus to the window's original
// (coder) pane afterward on both success and failure - see
// LaunchReviewInRepo's doc comment for why the review command is never sent
// to the window's existing pane directly. The command is sent with
// SendKeysToPane(newPaneID, ...), not a window/session-targeted send-keys:
// the latter resolves to whatever pane is active in the window at send time,
// which could have changed in the gap between the split and the send (e.g. a
// user keystroke, a tmux hook), landing the review command in the coder's
// pane instead - the exact outcome this function exists to prevent.
func (w *WorktreeManager) launchReviewInLiveWindow(
	session, windowName, wtPath, reviewCmd string,
) error {
	target := session + ":" + windowName

	// Capture the coder's pane before touching anything, so focus can be
	// restored to it afterward. If this fails, nothing has been created yet,
	// so there is nothing to roll back.
	coderPaneID, err := w.Tmux.ActivePaneID(target)
	if err != nil {
		return fmt.Errorf("failed to identify active pane in %s: %w", windowName, err)
	}

	if err := w.Tmux.SplitWindow(target, wtPath, "vertical"); err != nil {
		return fmt.Errorf("failed to split window %s: %w", windowName, err)
	}

	// split-window always makes the new pane active (see buildWindowPanes's
	// comment on the same property), so this second ActivePaneID call -
	// immediately after the split - captures the new pane's id.
	newPaneID, err := w.Tmux.ActivePaneID(target)
	if err != nil {
		return fmt.Errorf("failed to identify new pane in %s: %w", windowName, err)
	}

	if err := w.Tmux.SendKeysToPane(newPaneID, reviewCmd); err != nil {
		// Roll back only the pane this call added - never the window or the
		// worktree (there is none to roll back; R doesn't create one).
		_ = w.Tmux.KillPane(newPaneID)
		// Best-effort: this is cleanup after an already-failed operation, not
		// something to fail loudly over.
		_ = w.Tmux.SelectPane(coderPaneID)
		return fmt.Errorf("failed to launch review in %s: %w", windowName, err)
	}

	// Best-effort: land the user back on their coder pane, not the reviewer,
	// the next time they attach to this window - the review is already
	// running correctly in its own pane regardless of which pane is "active"
	// here. Swallow the error: not fatal, at most log-worthy.
	_ = w.Tmux.SelectPane(coderPaneID)
	return nil
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

	return w.createWindowWithLayout(repoSlug, windowName, wtPath, layout)
}

// createWindowWithLayout creates windowName in repoSlug's repo-slug tmux
// session (created when absent, reused otherwise) and builds layout's panes
// into it. This is ensureWindow's create-if-missing branch, extracted so a
// caller that has already established (via its own WindowSession call) that
// the window does not exist can go straight here instead of routing through
// ensureWindow, which would otherwise re-query WindowSession a second time
// internally. That redundant second query left a narrow race: if something
// else created the window in the gap between the caller's check and
// ensureWindow's internal one, ensureWindow's existing-window branch would
// send the caller's pane-0 command via a window-targeted send-keys into
// whatever pane is active - unsafe for LaunchReviewInRepo, whose whole point
// is never to type into a pane that might already be running a coder's
// interactive TUI. Callers must only invoke this right after their own
// WindowSession lookup has returned false, never speculatively.
//
// Preserves ensureWindow's existing create-path behavior exactly: same calls,
// same rollback (kill only the window, never the session - other worktrees'
// windows may already live here).
func (w *WorktreeManager) createWindowWithLayout(
	repoSlug, windowName, wtPath string,
	layout Layout,
) error {
	session := TmuxSessionName(repoSlug)
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
	wtPath, err := w.worktreePath(repoSlug, name)
	if err != nil {
		return err
	}
	windowName := GetWindowName(repoSlug, name)

	state, stateErr := w.worktreeState(repoSlug, name)
	if stateErr != nil {
		return stateErr
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

	now := time.Now()
	if err := config.Update(func(gc *config.GlobalConfig) error {
		gc.Worktree.UpsertRecentRepo(canonical, now)
		return nil
	}); err != nil {
		w.warnRepoRecordFailure(canonical, err)
	}
}

// warnRepoRecordFailure reports a recordRepoUsed failure through WarnFn
// (falling back to utils.PrintWarning when unset, so CLI callers get a
// sensible default even if a WorktreeManager was constructed as a literal
// instead of via New) and always logs it at debug level.
func (w *WorktreeManager) warnRepoRecordFailure(repoRoot string, err error) {
	logger.L().Debugw("failed to record recent repo", "repo", repoRoot, "error", err)
	w.warn(fmt.Sprintf(
		"worktree created, but failed to remember repo %s for later reuse: %v",
		repoRoot,
		err,
	))
}
