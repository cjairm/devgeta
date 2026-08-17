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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/reviewjournal"
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
	// in a worktree's window panes (per ADR-0005). These are thin aliases of
	// the tmux package's own constants, kept here so existing callers
	// (internal/tui/components/statusdot.go) and tests don't change: the
	// vocabulary of the @dg_agent_state pane option lives in
	// internal/apps/tmux now, since tmux cannot import worktree (the
	// dependency runs the other way) but worktree already imports tmux.
	AgentStateBusy    = tmux.AgentStateBusy
	AgentStateIdle    = tmux.AgentStateIdle
	AgentStateError   = tmux.AgentStateError
	AgentStateBlocked = tmux.AgentStateBlocked
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
	// exist), not "idle" - see AggregateAgentState.
	AgentState string
	Repo       string
	// Panes holds every pane state PaneStates() reported for this worktree's window,
	// for callers that need per-pane detail rather than just the aggregate.
	Panes []tmux.PaneState
}

// SessionStatus describes a standalone tmux session for the workspace
// dashboard - one with no worktree-backed window, so it doesn't already
// appear via List().
type SessionStatus struct {
	Name     string
	Attached bool
	// AgentState is the aggregated agent state across the session's own
	// panes, computed the same way as WorktreeStatus.AgentState (see
	// AggregateAgentState / ADR-0005). "" means no pane in the session ever
	// had an agent write to it.
	AgentState string
	// Panes holds every pane state PaneStates() reported for this session,
	// for callers that need per-pane detail rather than just the aggregate.
	Panes []tmux.PaneState
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

	// configMu guards configCache. It is the manager's only mutable receiver
	// state: the cache is read from Bubble Tea command goroutines, and the
	// dashboard's fast (tmux) and slow (git) refreshes can overlap, so a
	// plain field would be a data race.
	configMu sync.Mutex
	// configCache holds the last global config knownRepoAnchorGroups loaded,
	// keyed by the config file's identity at load time (see cachedConfig).
	// nil means "nothing cached yet, or the last load failed".
	configCache *cachedConfig
}

// cachedConfig is one memoized config.Load() result plus the identity of the
// file it came from. path is part of the key because paths.Paths.Config.Root
// can be repointed (tests do it per case), so a cached entry from one root
// must never be served under another.
type cachedConfig struct {
	path    string
	modTime time.Time
	size    int64
	gc      *config.GlobalConfig
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

// SetWarnFn installs fn as the warning sink for this manager AND for the git
// app it drives, so a TUI caller cannot silence one layer while the other
// keeps printing to stdout underneath its alt-screen. This exists because
// overriding only w.WarnFn used to leave git's own advisories (a diverged
// branch, an adopted source checkout) writing raw text into the running
// `dg ws` dashboard, scrolling the frame and leaving two frames' rows
// interleaved on screen. Callers rendering a TUI must use this rather than
// assigning w.WarnFn directly.
func (w *WorktreeManager) SetWarnFn(fn func(msg string)) {
	w.WarnFn = fn
	if w.Git != nil {
		w.Git.WarnFn = fn
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
	layout, err := validateLayout(layout)
	if err != nil {
		return err
	}
	repoRoot, err := w.Git.GetRepoRoot()
	if err != nil {
		return fmt.Errorf("not in a git repository: %w", err)
	}
	return w.create(repoRoot, name, layout, force, false)
}

// CreateAt is Create for a repository the caller is not inside: repoPath ("~"
// expanded) locates the repo and the window opens in the repo-slug tmux session
// (created when missing, reused otherwise).
//
// Like Create, it does not move the attached client — call FollowWindow if the
// user should end up in the new window.
func (w *WorktreeManager) CreateAt(repoPath, name string, layout Layout, force bool) error {
	layout, err := validateLayout(layout)
	if err != nil {
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
//
// It RETURNS the layout, and callers must use the returned value: each pane's
// check also resolves the absolute path of the tool it verified, and that
// resolution rides on the copy this returns. Discarding it would leave the
// eventual pane launch with nothing to build from, forcing it to either probe a
// second time or launch the bare name - the two outcomes ADR-0021 rules out. The
// returned layout is a copy, so calling this twice on one resolved layout (which
// `dg ws` does: resolve once, create repeatedly) keeps each create independent.
func validateLayout(layout Layout) (Layout, error) {
	if len(layout.Panes) == 0 {
		return Layout{}, fmt.Errorf("a layout with at least one pane is required")
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
			// Directory missing but git still tracks it - auto-prune and
			// continue. Anchored at repoRoot, not filepath.Dir(wtPath): see
			// pruneStaleWorktrees for why the parent directory was the wrong
			// anchor and made this prune silently do nothing.
			if pruneErr := w.pruneStaleWorktrees(repoRoot); pruneErr != nil {
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
		if isStaleRegistrationError(err) {
			if pruneErr := w.pruneStaleWorktrees(repoRoot); pruneErr == nil {
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

// isStaleRegistrationError reports whether a failed `git worktree add` looks
// like it was blocked by a registration whose directory is already gone -
// the case create() recovers from by pruning and retrying once.
//
// Git phrases this several ways depending on version and on whether the
// collision is with the worktree path or with the branch it holds, so all of
// them are matched. Matching too broadly is safe: prune only ever removes
// entries whose directory is missing, so when the conflicting worktree is
// genuinely still on disk the prune changes nothing and the retry fails with
// the same error the user would have seen anyway. Matching too NARROWLY is
// what hurts - it leaves a name permanently unusable, because the only thing
// standing between the user and a working `dg wt new <name>` is a dead entry
// nothing ever cleans up.
func isStaleRegistrationError(err error) bool {
	msg := err.Error()
	for _, marker := range []string{
		"is a missing but already registered",
		"already used by worktree at",
		"is already checked out at",
		"already exists and is not an empty directory",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
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
// missing, reused otherwise).
//
// It never moves the attached client. Creating a worktree and going to it are
// two decisions, and only the caller knows the second one: `dg wt create` in a
// shell wants to land you there, while the `dg ws` dashboard has a setting for
// it (worktree.attach_after_create) and its own attach path. This used to
// switch unconditionally, which made that setting impossible to honor — the
// client had already left before the dashboard read it. Callers that want to
// follow call FollowWindow.
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
	return nil
}

// FollowWindow moves the attached client to windowName, whichever session it
// lives in. It is the explicit counterpart to launchWindow's deliberate
// refusal to move anyone: a caller that wants the user to land in the window
// it just built says so by calling this.
//
// Outside tmux there is no client to move, and a window that isn't there
// cannot be followed; both are reported as errors rather than swallowed, so a
// caller can tell the user why they didn't end up where they expected. The
// caller decides how loud that is — `dg wt create` warns and still reports the
// create as the success it was.
func (w *WorktreeManager) FollowWindow(windowName string) error {
	if os.Getenv("TMUX") == "" {
		return fmt.Errorf("not inside tmux")
	}
	session, ok := w.Tmux.WindowSession(windowName)
	if !ok {
		return fmt.Errorf("tmux window %q not found", windowName)
	}
	return w.Tmux.SwitchToWindow(session, windowName)
}

// paneShell resolves the shell every pane command in one window build is run
// by and re-exec'd into - launch.go's two recipes interpolate it, twice each.
// It is resolved once per window build, here, because this is the layer that
// holds the tmux wrapper; layout.go's resolveShell takes its candidates as
// input precisely so it needs no tmux dependency of its own.
//
// The candidate order is ADR-0021's ladder: the user's $SHELL first, then
// tmux's own default-shell, with resolveShell's /bin/sh floor behind both. The
// tmux query is a CANDIDATE, never a requirement - a failed or empty answer
// simply drops it from the list. It is not an error, is not logged as one, and
// cannot block a create; the floor means resolution never comes back empty.
//
// tmux is queried unconditionally rather than only when $SHELL turns out to be
// unusable. Short-circuiting would make the tmux calls a create issues depend
// on whichever $SHELL the machine happens to have, which is both harder to
// reason about and exactly the kind of environment-dependent call sequence the
// ordered window-build tests exist to pin.
func (w *WorktreeManager) paneShell() string {
	candidates := []string{os.Getenv("SHELL")}
	if tmuxShell, ok := w.Tmux.DefaultShell(); ok {
		candidates = append(candidates, tmuxShell)
	}
	return resolveShell(candidates...)
}

// buildWindowFromLayout creates windowName - pane 0, running pane 0's own
// command as its process - and then builds the rest of layout's panes into it.
// If the window can't be created at all, or any later step fails partway (a
// split or the pane-0 reselect), the partially built window is killed
// (best-effort) and the worktree is rolled back - the same "all or nothing"
// guarantee the single-pane path gave, never a window with some panes up
// alongside a worktree that's still there.
func (w *WorktreeManager) buildWindowFromLayout(windowName, wtPath string, layout Layout) error {
	// Pane 0's command goes to the call that CREATES pane 0, because that is
	// the only place it can go: by the time buildWindowPanes runs, pane 0
	// already exists (ADR-0021 - the command travels as process arguments, so
	// the 1024-byte pty input queue that silently ate a long --prompt is out
	// of the path entirely).
	shell := w.paneShell()
	if err := w.Tmux.CreateWindow(
		windowName,
		wtPath,
		layout.pane0CreatedCommand(shell),
	); err != nil {
		_ = w.Git.RemoveWorktree(wtPath, true, "")
		return fmt.Errorf("failed to create tmux window: %w", err)
	}

	if err := w.buildWindowPanes(windowName, wtPath, shell, layout); err != nil {
		_ = w.Tmux.KillWindow(windowName)
		_ = w.Git.RemoveWorktree(wtPath, true, "")
		return err
	}
	return nil
}

// buildWindowPanes builds every pane of layout AFTER pane 0 into a window that
// already exists with exactly one (pane 0) pane - i.e. right after
// CreateWindow, CreateWindowInSession, or CreateSessionWithWindow. target is
// how the window is addressed for SplitWindow/ActivePaneID: a bare window name
// (current session) or "session:window" (qualified, for a window that may not
// be in the attached client's session). shell must come from paneShell.
//
// Pane 0 is deliberately not built here, and that is ADR-0021's shape rather
// than a division of labor: a pane's command is exec'd BY the tmux call that
// brings the pane into existence, and pane 0 was brought into existence by the
// caller's window-creating call. So pane 0's command is the caller's to pass -
// each of the three window-creating wrappers takes it - and this function
// starts at pane 1, where SplitWindow does the same job.
//
// Nothing is typed into any pane here. The send-keys seam this function used to
// take is gone, and that is the fix this whole change exists for: send-keys
// writes into the pane's pty, whose input queue is capped at 1024 bytes on
// macOS/BSD, and an overflowing write is discarded - the tail of the command
// AND the trailing Enter - with tmux still exiting 0. A window came up looking
// correct with a coder sitting at an empty session (ADR-0021).
func (w *WorktreeManager) buildWindowPanes(
	target, wtPath, shell string,
	layout Layout,
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

	// Panes are still built strictly in order, starting at pane 1, and the
	// order is load-bearing: split-window always splits the CURRENTLY ACTIVE
	// pane and makes the new pane active, so going in order is what puts each
	// pane where the layout says without an explicit pane index anywhere.
	//
	// Carrying the command ON the split (rather than in a call after it) also
	// removes the only gap in that reasoning: a pane's command is its process
	// from the pane's first instant, so "which pane is active" can no longer
	// change between a pane being created and its command arriving.
	//
	// A pane with no command (the "shell" layout's pane - see shellPane) gets
	// an empty command argument, which is exactly the argument list this split
	// produced before commands were passed at creation: tmux starts the pane's
	// shell and nothing else.
	for i := 1; i < len(layout.Panes); i++ {
		pane := layout.Panes[i]
		if err := w.Tmux.SplitWindow(
			target,
			wtPath,
			pane.Split,
			pane.creationCommand(shell),
		); err != nil {
			return fmt.Errorf(
				"layout %q, pane %d: failed to split window: %w",
				layout.Name, i+1, err,
			)
		}
	}

	if pane0ID != "" {
		// Land the user on pane 0 (e.g. the AI coder), not whichever pane was
		// split last (e.g. an editor pane), when they attach. Re-targeting by
		// tmux pane index (e.g. target+".0") is NOT reliable: devgeta's own
		// shipped tmux.conf sets pane-base-index to 1 (configs/tmux/tmux.conf.tmpl),
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

// AggregateAgentState reduces one window's pane states to the single value a
// worktree row should report, per ADR-0005's precedence
// (blocked > error > idle > busy > (no agent)). Returns "" when states is
// empty or nil, or every entry is "" / unrecognized - i.e. no pane in the
// window has a real agent state to report.
//
// Thin alias of tmux.AggregateAgentState, kept here so existing callers
// (worktree.go's own List()/session-status paths, internal/tui/worktree/tree.go)
// and tests don't change - see the AgentState* constants' doc comment for why
// the vocabulary itself now lives in internal/apps/tmux.
func AggregateAgentState(states []string) string {
	return tmux.AggregateAgentState(states)
}

// globalConfig returns the global config, re-reading and re-parsing the YAML
// only when the file it came from has changed. knownRepoAnchorGroups is on the
// dashboard's refresh path and used to pay a full read+unmarshal per refresh
// for a file that changes only when a worktree is created; ADR-0024's slow tick
// still calls it every 30 seconds, and every mutation calls it again.
//
// Staleness is bounded from both ends:
//
//   - out of process (a `dg wt new` in another shell) by the file's mtime and
//     size, stat'd on every call - three orders of magnitude cheaper than the
//     git exec this same enumeration is about to run per repo.
//   - in process by recordRepoUsed, the one place devgeta writes recent-repos,
//     which drops the cache outright. This matters because a same-second write
//     of the same size is invisible to a stat, and it is exactly what devgeta's
//     own create does.
//
// The stat happens before the load, never after, so cached content is always at
// least as new as the stamp it is filed under: a write racing the load leaves a
// newer file with a newer stamp, and the next call reloads. The returned config
// is shared with every other caller and must be treated as read-only.
func (w *WorktreeManager) globalConfig() (*config.GlobalConfig, error) {
	path := config.GlobalConfigFilePath()
	var modTime time.Time
	var size int64
	if fi, err := os.Stat(path); err == nil {
		modTime = fi.ModTime()
		size = fi.Size()
	}

	w.configMu.Lock()
	defer w.configMu.Unlock()

	if c := w.configCache; c != nil && c.path == path && c.size == size &&
		c.modTime.Equal(modTime) {
		return c.gc, nil
	}

	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		// Nothing worth caching: a missing or unreadable config is retried on
		// the next call, exactly as it was before this cache existed.
		w.configCache = nil
		return nil, err
	}
	w.configCache = &cachedConfig{path: path, modTime: modTime, size: size, gc: gc}
	return gc, nil
}

// invalidateGlobalConfig drops the memoized config so the next globalConfig()
// call re-reads the file. Called by recordRepoUsed - see globalConfig.
func (w *WorktreeManager) invalidateGlobalConfig() {
	w.configMu.Lock()
	defer w.configMu.Unlock()
	w.configCache = nil
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
// comment relies on the same guarantee). Reused by forEachKnownRepo, which
// both enumerateWorktrees (List()'s tmux-aware view and the tmux-agnostic
// findRepoForWorktree search) and cursorRepoRoot (the repo picker's cursor-
// repo resolution) build on, instead of each walking groups/anchors itself.
//
// Groups are collected, in this order (order only affects which group "wins"
// List()'s dedup, which itself doesn't matter since that dedup is by
// resolved main root, not by anchor):
//
//  1. the current repo (cwdRepoRoot, repo_candidates.go - reused rather than
//     reimplemented with GetRepoRoot) - a single-anchor group, since it's
//     already known to be one specific repo's root.
//  2. the recent-repos store (gc.Worktree.PrunedRecentRepos(), already
//     filters paths no longer on disk - reused rather than reimplemented),
//     read through w.globalConfig() so a dashboard refreshing on a timer
//     re-parses the YAML only when the file actually changed. A config load
//     failure is tolerated as "no config yet, skip this source", matching
//     RepoCandidates' own convention. Each recent repo is likewise a
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

	if gc, err := w.globalConfig(); err == nil {
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

// forEachKnownRepo calls fn once per known repo - once per
// knownRepoAnchorGroups group that successfully resolves to a repo not
// already seen via an earlier group in this same call - passing the
// resolved main worktree root and its full worktree list. fn returns false
// to stop iterating early (e.g. cursorRepoRoot, once it finds the slug it's
// looking for) or true to continue to the next group.
//
// This is the one place group/anchor resolution, dedup-by-main-root, and
// the early-exit-within-a-group logic (try anchors in a group in order,
// stop at the first one that resolves - a husk sibling never hides a good
// one) live: enumerateWorktrees and cursorRepoRoot both build on it instead
// of each maintaining its own copy of the same scaffolding that could
// silently drift. Both also get the dedup-by-main-root guarantee for free -
// previously only enumerateWorktrees had it; cursorRepoRoot could exec once
// per source even when two sources (e.g. the cwd group and a recent-repos
// entry) resolved to the same repo.
//
// So this costs one exec per known repo in the common case, not one per
// worktree - and, unlike a directory walk, a husk directory git does not
// report as a worktree can never appear (see ADR-0010,
// docs/decisions/ADR-0010-worktree-layout-is-a-setting-git-is-the-index.md).
func (w *WorktreeManager) forEachKnownRepo(
	fn func(mainRoot string, worktrees []git.WorktreeInfo) bool,
) {
	seenRoots := make(map[string]bool)
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

			if !fn(mainRoot, worktrees) {
				// fn asked to stop entirely (e.g. cursorRepoRoot found its match).
				return
			}

			// This group resolved successfully - the successful anchor's
			// result already has the repo's complete worktree list, so
			// there's no reason to query sibling anchors in the same group.
			// Breaking the inner (anchor) loop here falls straight through
			// to the outer loop's next group, exactly like the pre-extraction
			// code this replaces.
			break
		}
	}
}

// enumerateWorktrees resolves every worktree across every known repo, via
// forEachKnownRepo (see its doc comment for the group->anchor->dedup
// mechanics). Only Name/Path/Branch/Repo are populated on the returned
// WorktreeStatus values; TmuxWindow/WindowActive/AgentState are left at
// their zero values - no tmux exec happens in this function at all. This is
// the single enumeration implementation List() (the tmux-aware view) and
// findRepoForWorktree (a tmux-agnostic search) both build on, instead of
// each walking git separately.
func (w *WorktreeManager) enumerateWorktrees() []WorktreeStatus {
	statuses := []WorktreeStatus{}
	w.forEachKnownRepo(func(mainRoot string, worktrees []git.WorktreeInfo) bool {
		// Matches create()'s `repoSlug := filepath.Base(repoRoot)` -
		// must be the identical derivation used elsewhere, or
		// Repo/TmuxWindow won't match what create/repair/remove expect.
		repoSlug := filepath.Base(mainRoot)
		for _, wt := range worktrees {
			if wt.Path == mainRoot {
				// The repo's own checkout is not a devgeta-managed worktree row.
				continue
			}
			if wt.Prunable {
				// Git still holds an administrative entry, but the directory
				// is gone - this is debris, not a worktree. Emitting it as a
				// row is what put deleted worktrees back on the `dg ws`
				// dashboard after every refresh, each one failing its diff
				// with "cannot change to '<path>': No such file or
				// directory" because no git command can run there. Dropping
				// it here is a pure read - the entry is cleaned up by the
				// mutation paths that call pruneStaleWorktrees.
				logger.L().Debugw(
					"worktree: skipping stale registration",
					"path", wt.Path,
					"reason", wt.PrunableReason,
				)
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
		return true
	})

	return statuses
}

// StateLayer is one tmux scan's worth of state: everything the dashboard
// shows that comes from tmux rather than git. It is the unit ADR-0024's fast
// refresh moves around - taken by ScanTmuxState, applied by ApplyTo and
// SessionStatuses - so the fast tick can refresh tmux state without touching
// git, and both applications work off the same single scan instead of one
// each.
//
// A StateLayer is read-only once built (ScanTmuxState never hands out one it
// still writes to), which is what lets the TUI carry it across a command
// goroutine into Update.
type StateLayer struct {
	// PanesByWindow indexes the scan's panes by tmux window name. A window's
	// presence as a key is the direct equivalent of "does a window with this
	// name exist": PaneStates enumerates every pane on the server, and a
	// window with zero panes cannot exist in tmux.
	PanesByWindow map[string][]tmux.PaneState
	// PanesBySession indexes those same panes by the session that owns them.
	PanesBySession map[string][]tmux.PaneState
	// Sessions is the `tmux list-sessions` result, unfiltered - which of them
	// count as standalone is SessionStatuses' decision, not the scan's.
	Sessions []tmux.SessionInfo
}

// ScanTmuxState takes the whole tmux-derived layer in one `tmux list-sessions`
// plus one `tmux list-panes -a`. That single pane scan is the point: List()
// and ListSessions() used to take one each, so a refresh that wanted both -
// which the dashboard does, every tick - paid for two (ADR-0024).
//
// The pane scan is skipped entirely when there are no sessions, since a pane
// cannot exist outside one; that is also the no-tmux-server case, where
// ListSessions() returns (nil, nil) and this returns an empty layer, not an
// error.
//
// An error means `tmux list-sessions` genuinely failed (not "no server"), and
// is returned with an empty layer.
func (w *WorktreeManager) ScanTmuxState() (StateLayer, error) {
	sessions, err := w.Tmux.ListSessions()
	if err != nil {
		return StateLayer{}, err
	}
	layer := StateLayer{
		PanesByWindow:  make(map[string][]tmux.PaneState),
		PanesBySession: make(map[string][]tmux.PaneState),
		Sessions:       sessions,
	}
	if len(sessions) == 0 {
		return layer, nil
	}
	for _, ps := range w.Tmux.PaneStates() {
		layer.PanesByWindow[ps.Window] = append(layer.PanesByWindow[ps.Window], ps)
		layer.PanesBySession[ps.Session] = append(layer.PanesBySession[ps.Session], ps)
	}
	return layer, nil
}

// ApplyTo returns a copy of statuses with the tmux-derived fields
// (TmuxWindow, WindowActive, AgentState, Panes) filled in from this layer.
// Every other field is git-derived and passes through untouched.
//
// statuses is READ-ONLY here and the result is a new slice. The caller that
// enumerated a list stays its only writer, which is what lets the dashboard
// hand a layer to Update and apply it to a list the model already holds
// without racing whoever produced that list.
func (l StateLayer) ApplyTo(statuses []WorktreeStatus) []WorktreeStatus {
	applied := make([]WorktreeStatus, len(statuses))
	copy(applied, statuses)
	for i := range applied {
		windowName := GetWindowName(applied[i].Repo, applied[i].Name)
		// Comma-ok reports key presence, not slice non-emptiness: a window in
		// the index always has at least one pane, so ok alone is the "window
		// exists" signal.
		panes, windowActive := l.PanesByWindow[windowName]
		if panes == nil {
			panes = []tmux.PaneState{}
		}
		applied[i].TmuxWindow = windowName
		applied[i].WindowActive = windowActive
		applied[i].AgentState = aggregatePaneStates(panes)
		applied[i].Panes = panes
	}
	return applied
}

// SessionStatuses reduces this layer to the standalone sessions the workspace
// dashboard lists on their own - those with no worktree-backed window, since a
// worktree-backed session already appears as a worktree row. The exclusion and
// the per-session aggregation both live here, off an already-taken scan,
// rather than in ListSessions() taking a second one of its own.
func (l StateLayer) SessionStatuses() []SessionStatus {
	var statuses []SessionStatus
	for _, s := range l.Sessions {
		panes := l.PanesBySession[s.Name]
		if slices.ContainsFunc(panes, func(ps tmux.PaneState) bool {
			return isWorktreeWindow(ps.Window)
		}) {
			continue
		}
		statuses = append(statuses, SessionStatus{
			Name:       s.Name,
			Attached:   s.Attached,
			AgentState: aggregatePaneStates(panes),
			Panes:      panes,
		})
	}
	return statuses
}

// aggregatePaneStates reduces a window's or a session's panes to the single
// agent state its row reports, per ADR-0005's precedence. The one adapter
// between a []tmux.PaneState and tmux.AggregateAgentState's []string, shared
// by both halves of the apply step so neither grows its own copy.
func aggregatePaneStates(panes []tmux.PaneState) string {
	states := make([]string, 0, len(panes))
	for _, p := range panes {
		states = append(states, p.State)
	}
	return tmux.AggregateAgentState(states)
}

// RefreshState layers one fresh tmux scan onto statuses, returning the applied
// worktree rows and the standalone-session rows from that same scan. This is
// the whole refresh in one call for callers that want both halves - the
// dashboard's slow tick, and List() below.
//
// statuses is read-only (see ApplyTo). The applied rows are returned even when
// the scan failed: an empty layer applies cleanly - every window absent - which
// is exactly what a caller saw before this split when tmux was unreachable, so
// a broken tmux server costs the tmux columns and never the git-backed rows.
func (w *WorktreeManager) RefreshState(
	statuses []WorktreeStatus,
) ([]WorktreeStatus, []SessionStatus, error) {
	layer, err := w.ScanTmuxState()
	return layer.ApplyTo(statuses), layer.SessionStatuses(), err
}

// ListWorktreesOnly returns every worktree across every known repo with only
// its git-derived fields populated (Name/Path/Branch/Repo); the tmux-derived
// ones are left zero for RefreshState to fill in. This is ADR-0024's slow
// half on its own, for the dashboard's 30-second git tick. The error return
// exists for symmetry with List() and to leave room for enumeration to start
// reporting failures; it is always nil today.
func (w *WorktreeManager) ListWorktreesOnly() ([]WorktreeStatus, error) {
	return w.enumerateWorktrees(), nil
}

// List returns all worktrees with their window status across all repos - the
// git enumeration and the tmux layer in one call, which is what every non-TUI
// caller (dg wt list, ListNames, shell completion) wants. Composed from the
// two halves the dashboard refreshes separately, so the combined path and the
// split one cannot drift. The cost of composing rather than duplicating is one
// `tmux list-sessions` whose result List() discards - a single fixed exec on a
// call that already spawns a git process per known repo.
//
// A tmux failure is not a List failure: the git-backed rows are still correct
// and worth returning when no tmux server is reachable, which is how this
// behaved before the split (PaneStates() has always tolerated the same failure
// by returning nil). The dashboard, which does care, gets the error from
// RefreshState.
func (w *WorktreeManager) List() ([]WorktreeStatus, error) {
	statuses, err := w.ListWorktreesOnly()
	if err != nil {
		return nil, err
	}
	applied, _, err := w.RefreshState(statuses)
	if err != nil {
		logger.L().Debugw(
			"worktree: tmux scan failed, listing worktrees without tmux state",
			"error", err,
		)
	}
	return applied, nil
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
// worktree-backed session already appears via List(). Composed from the same
// scan and the same apply half RefreshState uses (see StateLayer), so the two
// paths cannot drift and a caller wanting both lists pays for one scan, not
// two.
//
// Errors from Tmux.ListSessions() propagate unchanged, including its (nil,
// nil) no-server result, which flows through as an empty list here rather
// than an error.
func (w *WorktreeManager) ListSessions() ([]SessionStatus, error) {
	layer, err := w.ScanTmuxState()
	if err != nil {
		return nil, err
	}
	return layer.SessionStatuses(), nil
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

	// The name matched no live worktree. Before giving up, check whether git
	// is still holding a registration for it whose directory is already gone,
	// and clean that up: "remove the worktree called X" is satisfied by
	// removing the last trace of X, and the user has no other way to reach it.
	//
	// This check has to live here, AFTER the live-worktree lookups, precisely
	// because the enumeration those lookups use hides stale entries (a dead
	// path can only produce failing commands). Hiding them without this made
	// them unreachable rather than fixed: invisible in `dg wt list` and the
	// dashboard, and unresolvable by name, so nothing devgeta shipped could
	// ever clean one up.
	if pruned, err := w.pruneStaleWorktreeNamed(name); err != nil {
		return err
	} else if pruned {
		return nil
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
//
// KNOWN GAP: if a worktree is somehow real at BOTH shapes simultaneously
// (e.g. `git worktree add` was used by hand at the other shape, bypassing
// devgeta entirely - a state findRepoForWorktree's own doc comment already
// anticipates for its analogous ambiguity), this silently prefers shared,
// since it is checked first and returned on match. `dg wt move X --to
// shared` on such a worktree would then report "already at target" and
// leave the in-repo twin completely untouched. Not handled here - at
// minimum this would need surfacing to the caller as a warning, or a check
// upstream, rather than silently picking one.
func (w *WorktreeManager) currentWorktreePath(repoSlug, repoRoot, name string) (string, bool) {
	// Ask git first, and take whatever path it reports verbatim (ADR-0010,
	// "git is the index"). The two shape probes below can only ever find a
	// worktree that sits at one of the exact locations devgeta knows how to
	// construct today - so a worktree living anywhere else was invisible to
	// every mutation, while List() (which reads git) kept showing it. That
	// split is what made `d` in the dashboard a silent no-op on any worktree
	// created under an older data-directory name, adopted by hand, or moved
	// with plain `git worktree move`: remove looked at the configured shape,
	// found nothing, and returned success without touching anything, so the
	// row came back on the next refresh. Resolving from git closes the split
	// at its source - there is no location a worktree can be at that git
	// reports and this cannot resolve.
	if wtPath, ok := w.gitWorktreePath(repoRoot, name); ok {
		return wtPath, true
	}

	// Shape probes remain as a fallback for the one case git cannot be asked
	// from repoRoot: an unresolved repoRoot (""). sharedWorktreePath needs no
	// root, so probing it can still find a worktree - and isRealWorktreeAt
	// asks git from the worktree itself, which answers even when the repo's
	// own root was never resolved.
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

// gitWorktreePath returns the path git itself reports for the worktree named
// name in the repo rooted at repoRoot, matching on the worktree directory's
// final path segment (the flattened name) exactly as enumerateWorktrees
// derives WorktreeStatus.Name - so a name that resolves to a row in
// `dg wt list` resolves to the same path here, whatever shape that path has.
//
// Prunable registrations are skipped: their directory is gone, so they are
// debris to prune (see pruneStaleWorktrees), never a worktree to act on.
// Returning one here would hand a mutation a path that cannot be operated on.
//
// Returns false when repoRoot is empty or git cannot answer there - both mean
// "no answer from git", never "no such worktree", so the caller falls through
// to its own fallbacks rather than concluding the worktree is absent.
func (w *WorktreeManager) gitWorktreePath(repoRoot, name string) (string, bool) {
	if repoRoot == "" {
		return "", false
	}
	worktrees, err := w.Git.ListWorktreesAt(repoRoot)
	if err != nil {
		return "", false
	}
	// Both sides go through CanonicalRepoPath for the main-checkout
	// comparison for the same reason isRealWorktreeAt does: git reports
	// symlink-resolved paths while repoRoot may not be resolved, and on macOS
	// (/tmp -> /private/tmp) a raw == would fail to recognize the main
	// checkout and let it match as if it were a linked worktree.
	canonicalRoot := config.CanonicalRepoPath(repoRoot)
	flat := FlattenName(name)
	for _, wt := range worktrees {
		if wt.Prunable {
			continue
		}
		if config.CanonicalRepoPath(wt.Path) == canonicalRoot {
			// The repo's own checkout is not a devgeta-managed worktree,
			// matching enumerateWorktrees' identical exclusion.
			continue
		}
		if filepath.Base(wt.Path) == flat {
			return wt.Path, true
		}
	}
	return "", false
}

// StaleWorktree describes one registration git reports as prunable: the
// directory is gone, so the entry is administrative debris that no command
// can act on. Carries RepoRoot because pruning must run from a repo root
// (see pruneStaleWorktrees) and the dead path itself is not one.
type StaleWorktree struct {
	Repo     string
	RepoRoot string
	Name     string
	Path     string
	Reason   string
}

// enumerateStaleWorktrees is enumerateWorktrees' mirror image: it returns
// exactly the registrations that one drops.
//
// Both are needed and neither can be folded into the other. Dashboard rows
// must never include debris (a diff against a missing directory can only
// fail), but the debris still has to be reachable by SOMETHING or it can
// never be cleaned up - which was the hole left by simply hiding it: the
// ghost stopped being visible and, in the same stroke, stopped being
// removable, because every lookup path went through the filtered
// enumeration.
func (w *WorktreeManager) enumerateStaleWorktrees() []StaleWorktree {
	var stale []StaleWorktree
	w.forEachKnownRepo(func(mainRoot string, worktrees []git.WorktreeInfo) bool {
		repoSlug := filepath.Base(mainRoot)
		for _, wt := range worktrees {
			if !wt.Prunable {
				continue
			}
			stale = append(stale, StaleWorktree{
				Repo:     repoSlug,
				RepoRoot: mainRoot,
				Name:     filepath.Base(wt.Path),
				Path:     wt.Path,
				Reason:   wt.PrunableReason,
			})
		}
		return true
	})
	return stale
}

// pruneStaleWorktreeNamed prunes the stale registrations whose flattened name
// matches name, across every repo devgeta knows about. Reports whether it
// found (and pruned) any.
//
// This is what makes `dg wt remove <name>` work on a worktree whose directory
// is already gone. Without it, hiding stale rows from the enumeration also
// hid them from every lookup, so the name resolved to nothing and the entry
// survived forever with no devgeta command able to touch it.
func (w *WorktreeManager) pruneStaleWorktreeNamed(name string) (bool, error) {
	flat := FlattenName(name)
	roots := make(map[string]bool)
	for _, s := range w.enumerateStaleWorktrees() {
		if s.Name == flat {
			roots[s.RepoRoot] = true
		}
	}
	if len(roots) == 0 {
		return false, nil
	}
	for root := range roots {
		if err := w.pruneStaleWorktrees(root); err != nil {
			return false, fmt.Errorf(
				"worktree '%s' is already gone but its stale git entry in %s could not be pruned: %w",
				name,
				root,
				err,
			)
		}
	}
	return true, nil
}

// PruneStale removes git's leftover bookkeeping for worktrees whose directory
// no longer exists, across every repo devgeta knows about, and returns what it
// cleaned. It never deletes a worktree, a directory, or a branch - git's own
// prune only drops entries whose directory is already missing - so unlike
// Prune (which removes every worktree it can find) this needs no
// confirmation.
func (w *WorktreeManager) PruneStale() ([]StaleWorktree, error) {
	stale := w.enumerateStaleWorktrees()
	if len(stale) == 0 {
		return nil, nil
	}

	roots := make(map[string]bool, len(stale))
	for _, s := range stale {
		roots[s.RepoRoot] = true
	}

	var failures []string
	for root := range roots {
		if err := w.pruneStaleWorktrees(root); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", root, err))
		}
	}
	if len(failures) > 0 {
		return stale, fmt.Errorf(
			"failed to prune stale entries in some repos:\n  %s",
			strings.Join(failures, "\n  "),
		)
	}
	return stale, nil
}

// staleWorktreePaths returns the paths of every registration git reports as
// prunable in the repo rooted at repoRoot whose flattened name matches name.
// These are the ghost rows: git still holds an administrative entry, but the
// directory is gone, so nothing can be run against the path.
//
// Used by removeByRepo to tell "this worktree is already gone, prune the
// leftover entry and report success" apart from "no such worktree at all",
// which must be an error rather than the silent no-op it used to be.
func (w *WorktreeManager) staleWorktreePaths(repoRoot, name string) []string {
	if repoRoot == "" {
		return nil
	}
	worktrees, err := w.Git.ListWorktreesAt(repoRoot)
	if err != nil {
		return nil
	}
	flat := FlattenName(name)
	var stale []string
	for _, wt := range worktrees {
		if wt.Prunable && filepath.Base(wt.Path) == flat {
			stale = append(stale, wt.Path)
		}
	}
	return stale
}

// repoRootForPrune resolves a repo root suitable for pruning, trying the
// slug-based lookup first and then asking git to resolve one directly from
// wtPath.
//
// The second step matters because cursorRepoRoot answers only for repos
// devgeta already knows as anchors (the current directory, the recent-repos
// store, a shared-root slug directory) and legitimately returns "" otherwise
// - at which point pruneStaleWorktrees reports "repo root is unknown" and a
// caller that escalates prune failures would fail a removal that actually
// worked. wtPath is a real worktree, so git can name its main worktree, and
// that answer is authoritative regardless of what devgeta has recorded.
//
// Must be called while wtPath still exists: git cannot resolve anything from
// a directory that has already been deleted.
func (w *WorktreeManager) repoRootForPrune(repoSlug, wtPath string) string {
	if root := w.cursorRepoRoot(repoSlug); root != "" {
		return root
	}
	if wtPath == "" {
		return ""
	}
	if root, err := w.Git.GetMainWorktree(wtPath); err == nil {
		return root
	}
	return ""
}

// pruneStaleWorktrees drops git's administrative entries for worktrees whose
// directory no longer exists, running from repoRoot.
//
// The anchor matters and was the bug: every caller here used to pass
// filepath.Dir(wtPath), the worktree's PARENT directory. For the in-repo
// shape that happens to sit inside the repo and worked by luck; for the
// shared shape it is ~/.local/share/devgeta/worktrees/<slug>, which is not a
// git repository at all, so `git -C <that> worktree prune` failed with "not a
// git repository" - and every call site discarded the error. Pruning was
// therefore a silent no-op for exactly the worktrees that most needed it, and
// the stale entries survived forever. A repo root is the only anchor guaranteed
// to be a git repository, so it is the only one this accepts.
func (w *WorktreeManager) pruneStaleWorktrees(repoRoot string) error {
	if repoRoot == "" {
		return fmt.Errorf("cannot prune stale worktree entries: repo root is unknown")
	}
	return w.Git.PruneWorktreesAt(repoRoot)
}

// isRealWorktreeAt reports whether path is a real, git-tracked worktree -
// git's own worktree list is authoritative (ADR-0010, "git is the index"),
// so a match there is trusted even if the directory has, say, been moved out
// from under git's expectation. When git itself can't run at path at all
// (e.g. the directory doesn't exist yet, so `-C path` fails outright), that
// is treated as "no answer from git" and a plain directory check is the
// fallback - mirroring worktreeStateFor's own tolerance for this same
// failure mode.
//
// The comparison against git's answer goes through
// config.CanonicalRepoPath on BOTH sides, not raw string equality: `git
// worktree list --porcelain` reports symlink-resolved absolute paths, while
// path is built via plain filepath.Join (sharedWorktreePath/
// inRepoWorktreePath do no symlink resolution). On a system where the data
// root (or $HOME) is itself a symlink - macOS's /tmp -> /private/tmp is the
// most common real-world trigger - the two representations differ and a raw
// == comparison would return a hard false even for a worktree that
// genuinely exists, with no os.Stat fallback to catch it (by design, since
// git DID answer successfully here). Canonicalizing the comparison closes
// that gap without weakening the trust-git branch - never add an os.Stat
// fallback here; that would re-admit the husk-directory bug this whole
// cycle exists to close.
func (w *WorktreeManager) isRealWorktreeAt(path string) bool {
	worktrees, err := w.Git.ListWorktreesAt(path)
	if err == nil {
		canonicalPath := config.CanonicalRepoPath(path)
		for _, wt := range worktrees {
			if config.CanonicalRepoPath(wt.Path) == canonicalPath {
				return true
			}
		}
		return false
	}
	info, statErr := os.Stat(path)
	return statErr == nil && info.IsDir()
}

// realWorktreePathOrConfigured resolves name's REAL on-disk path by checking
// both location shapes against git (currentWorktreePath, which uses
// isRealWorktreeAt), the same git-verified resolution Move already relies
// on - falling back to the config-derived worktreePath ONLY when no real
// worktree exists at either shape, so "genuinely doesn't exist" still
// resolves to a deterministic path for prune/error-reporting purposes,
// matching today's behavior for that case.
//
// This exists because worktreePath (and worktreeState, which calls it)
// resolves purely from the CONFIGURED worktree.location - which can
// silently disagree with where a worktree really lives, e.g. after `dg wt
// move --to <x>` without also changing the global default (a flow this
// cycle's own migration guide recommends). A mutation acting on the
// config-derived guess instead of reality can report success on a worktree
// it never touched. cursorRepoRoot resolves repoRoot (needed to check the
// in-repo candidate) the same way Move does; an unresolvable repoRoot just
// means the in-repo candidate can't be checked, not a failure.
func (w *WorktreeManager) realWorktreePathOrConfigured(repoSlug, name string) (string, error) {
	repoRoot := w.cursorRepoRoot(repoSlug)
	if wtPath, ok := w.currentWorktreePath(repoSlug, repoRoot, name); ok {
		return wtPath, nil
	}
	return w.worktreePath(repoSlug, name)
}

// worktreeStateReal is worktreeState's git-verified counterpart: it resolves
// a worktree's state AND the actual path it was computed against via
// realWorktreePathOrConfigured, instead of trusting the configured
// worktree.location the way worktreeState does. Callers that mutate a
// worktree (removeByRepo, Repair, RepairInRepo) must use this, not
// worktreeState, and must use the returned path for both the state check
// AND the actual git/filesystem operation - never one path for "does it
// exist" and a different, possibly-wrong one for "act on it here".
func (w *WorktreeManager) worktreeStateReal(repoSlug, name string) (WorktreeState, string, error) {
	wtPath, err := w.realWorktreePathOrConfigured(repoSlug, name)
	if err != nil {
		return WorktreeState{}, "", err
	}
	return w.worktreeStateFor(wtPath, GetWindowName(repoSlug, name)), wtPath, nil
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
		// Best-effort: remove the destination parent directory MkdirAll just
		// created above, if git's refusal (e.g. a locked worktree) left it
		// empty. os.Remove (never RemoveAll) only succeeds on an empty
		// directory, so this is a safe no-op - never a data-loss risk - when
		// the directory already held other worktrees, or already existed
		// before this call. Without this, a failed --to in-repo move leaves
		// behind the very .claude/worktrees/ directory the warning above
		// just said isn't gitignored.
		_ = os.Remove(filepath.Dir(toPath))
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

// idleShellPaneIn returns the id of the first pane of windowName that is
// sitting at a shell prompt, and whether there was one. "First" is tmux's own
// pane order, so it prefers the pane devgeta built the window with over any
// the user split off later.
//
// It shares isIdleShellPane with retargetWindowAfterMove rather than judging
// panes its own way, so "this pane is free" means one thing across the whole
// package: change the allowlist and every caller changes with it. The two
// callers do differ in what they need, and deliberately so - a move must
// retarget every pane or none (a half-updated window is worse than an
// untouched one), while a review only needs somewhere to run.
func (w *WorktreeManager) idleShellPaneIn(windowName string) (string, bool) {
	for _, p := range w.Tmux.PanesInWindow(windowName) {
		if isIdleShellPane(p.CurrentCommand) {
			return p.PaneID, true
		}
	}
	return "", false
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
//
// The current-repo check goes through currentWorktreePath (git-verified,
// checking both location shapes), not worktreePath+os.Stat: the
// config-derived worktreePath can disagree with where the worktree actually
// lives (e.g. after `dg wt move` without also changing the global default),
// which would make this candidate wrongly report "not here" for a worktree
// that is, falling through to findRepoForWorktree's slower search for no
// reason - or, worse, for an ambiguous name across repos, resolving to the
// wrong owner.
func (w *WorktreeManager) repoSlugForWorktree(name string) string {
	if repoRoot, err := w.Git.GetRepoRoot(); err == nil {
		candidate := filepath.Base(repoRoot)
		if _, ok := w.currentWorktreePath(candidate, repoRoot, name); ok {
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
	layout, err := validateLayout(layout)
	if err != nil {
		return err
	}

	repoSlug := w.repoSlugForWorktree(name)
	if repoSlug == "" {
		return fmt.Errorf("no worktree '%s' to repair", name)
	}

	// realWorktreePathOrConfigured, not worktreePath: the worktree may
	// actually live at the location shape git reports, not the one
	// currently configured (e.g. after `dg wt move` without also changing
	// the global default) - see its doc comment. Only when no real
	// worktree exists at either shape does this fall back to the
	// config-derived path, matching the pre-existing "directory missing,
	// prune and tell the user to recreate" behavior below.
	wtPath, err := w.realWorktreePathOrConfigured(repoSlug, name)
	if err != nil {
		return err
	}
	windowName := GetWindowName(repoSlug, name)

	// If directory doesn't exist on disk but git knows about it, prune and error
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		// Prune stale worktree entries from the repo root - see
		// pruneStaleWorktrees for why filepath.Dir(wtPath) was the wrong
		// anchor and left the entry in place.
		if pruneErr := w.pruneStaleWorktrees(w.cursorRepoRoot(repoSlug)); pruneErr != nil {
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

	// Re-apply the gitfile normalization creation established. This is what
	// keeps that fix from regressing silently: `git worktree repair` and `git
	// worktree move` rewrite .git and restore git's trailing newline, which
	// reintroduces the strict-parser breakage (commits failing inside the
	// worktree) with nothing to announce it. Repair is the command a user runs
	// when a worktree misbehaves, so it is the right place to heal it.
	// See ADR-0013.
	if err := w.Git.NormalizeWorktreeGitfile(wtPath); err != nil {
		return err
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
	layout, err := validateLayout(layout)
	if err != nil {
		return err
	}
	// realWorktreePathOrConfigured, not worktreePath - see Repair's identical
	// comment on why the git-verified real path must be used here.
	wtPath, err := w.realWorktreePathOrConfigured(repoSlug, name)
	if err != nil {
		return err
	}
	windowName := GetWindowName(repoSlug, name)
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		// Repo root, not filepath.Dir(wtPath) - see pruneStaleWorktrees.
		if pruneErr := w.pruneStaleWorktrees(w.cursorRepoRoot(repoSlug)); pruneErr != nil {
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
	// Heal the gitfile before handing the worktree back - see Repair's
	// identical call for why the repair paths own this.
	if err := w.Git.NormalizeWorktreeGitfile(wtPath); err != nil {
		return err
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
//   - Live window: a new pane is split off with the review command as its
//     process - never into the window's existing pane, which may already be
//     running a coder's interactive TUI (typing the review command there would
//     land it in the coder's composer instead of executing it). The one
//     exception is an existing pane that is sitting idle at a shell, which
//     launchReviewInLiveWindow reuses.
//
// A failed launch in the live-window case never touches the user's window, its
// existing pane, or the worktree - unlike buildWindowFromLayout's rollback,
// which kills the window and removes the worktree it just created. R never
// creates a worktree, and the window here is the user's, already holding their
// work. See launchReviewInLiveWindow on why there is now nothing left for it to
// roll back either.
func (w *WorktreeManager) LaunchReviewInRepo(repoSlug, name, reviewerKey string) error {
	// reviewerPane validates reviewerKey and runs the review path's ONE opencode
	// probe, keeping its resolution on the pane it returns (see reviewerPane).
	// Both launches below build from this single pane, so the check and whatever
	// runs cannot describe different things (ADR-0021).
	//
	// A review is always launched via OpenCode, regardless of the worktree's own
	// layout - a user whose default layout is claude/claude-nvim has never needed
	// opencode, so it may never have been installed. Probing here, before any tmux
	// call, turns that into one actionable error instead of a pane that prints
	// "command not found" while the dashboard reports "review started" with no
	// error anywhere.
	//
	// The probed token is the "opencode" BINARY (see
	// OpenCodeCoder.EnsureInstalled), because that is what both launches below
	// name - the created pane execs it, and the live-window branch types it. So an
	// opencode installed outside devgeta - one whose "oc" alias was never written
	// to devgeta.zsh - passes this check and launches correctly either way
	// (ADR-0021's 2026-08-07 amendment; the older alias probe refused it outright).
	pane, err := reviewerPane(reviewerKey)
	if err != nil {
		return err
	}

	// Resolved via realWorktreePathOrConfigured, not the config-derived
	// worktreePath: the worktree's real location can disagree with what the
	// CONFIGURED worktree.location computes (e.g. after `dg wt move` without
	// also changing the global default - the same split-brain
	// worktreeStateReal's doc comment describes for removeByRepo/Repair).
	// This function only needs the path, not a full WorktreeState, so it uses
	// realWorktreePathOrConfigured directly rather than worktreeStateReal.
	wtPath, err := w.realWorktreePathOrConfigured(repoSlug, name)
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
		// prevent. This one-pane layout carries no install checker, because
		// reviewerPane already probed - see its doc comment on why a nil check
		// is what keeps this from becoming a second probe.
		layout := Layout{
			Name:  "review-" + reviewerKey,
			Panes: []Pane{pane},
		}
		return w.createWindowWithLayout(repoSlug, windowName, wtPath, layout)
	}

	return w.launchReviewInLiveWindow(session, windowName, wtPath, pane)
}

// launchReviewInLiveWindow gets pane's review command running in an existing
// (live) window: in a pane that is sitting at a shell prompt if the window has
// one, otherwise in a new pane split off for it. Focus is restored to whichever
// pane was active beforehand on the success paths. There is no failure path
// that moves focus: a failed split never creates a pane, so it never touches
// focus in the first place, and a failed send-keys into the reused pane leaves
// that pane's own focus exactly as it was - both leave focus right where it
// found it, with nothing to restore.
//
// It takes the whole reviewer Pane, not just a command string, so this branch and
// LaunchReviewInRepo's create branch spend the SAME probe's resolution (see
// reviewerPane). The two sub-branches then use DIFFERENT forms of that one pane,
// and which form is decided by one question only - is this pane NEW? (ADR-0021
// part 4; asking "is the window live?" is what put this function's split branch
// on the wrong side of the line in an earlier draft, since the function is named
// for the window.)
//
//   - Reusing an idle shell pane sends pane.Command with send-keys. That pane
//     already exists and is running a live interactive shell, so there is
//     nothing to exec into; the 1023-byte guard in the tmux wrapper is what
//     keeps that path from ever truncating silently.
//   - Splitting a new pane CREATES a pane, so it passes pane.creationCommand at
//     creation and types nothing at all.
//
// The rule this function enforces is "never type into a pane that is running
// something" - not "always split". An idle shell is not running anything, so
// reusing it is both safe and what a user who picked the shell layout (an
// empty window, on purpose) expects: a review in the pane they already have,
// not a second pane beside it. Which panes are idle is decided by
// isIdleShellPane, the same allowlist `dg wt move` already uses to pick panes
// it may `cd` - see its doc comment for why an allowlist of shells, rather
// than any attempt to recognize an agent, is the only reliable direction (a
// Claude Code pane reports its versioned binary directory, e.g. "2.1.222",
// never "claude"). Anything unrecognized therefore counts as busy and gets
// the split, so the fallback is always the safe one.
//
// The residual risk is a shell pane where the user has a half-typed command
// waiting: the review command lands after it. That is the same trade
// retargetWindowAfterMove already accepts for `cd`, and pressing R from the
// dashboard means the user is not typing in that pane at that moment.
//
// The reuse branch sends with SendKeysToPane(paneID, ...), never a
// window/session-targeted send-keys: the latter resolves to whatever pane is
// active in the window at send time, which could have changed in the gap
// since it was chosen (a user keystroke, a tmux hook), landing the review
// command in the coder's pane - the exact outcome this function prevents. The
// split branch needs no such precaution at all: its command is an argument of
// the very call that creates the pane, so there is no gap and no target to
// resolve.
//
// Rollback: nothing to undo. It used to kill the pane it had just split when
// the follow-up send-keys failed, which was the only failure that could happen
// AFTER a pane existed. With the command carried by the split itself, that
// second step is gone - split-window either creates the pane (with its command
// already running) or fails having created nothing, so a failed split leaves
// nothing behind and a successful one leaves nothing that can still fail. The
// negative guarantees are unchanged and are what the tests pin: this function
// never kills the window, never touches the user's existing pane, and never
// removes a worktree (R does not create one).
func (w *WorktreeManager) launchReviewInLiveWindow(
	session, windowName, wtPath string,
	pane Pane,
) error {
	target := session + ":" + windowName

	// Capture the active pane before touching anything, so focus can be
	// restored to it afterward. If this fails, nothing has been created yet,
	// so there is nothing to roll back.
	coderPaneID, err := w.Tmux.ActivePaneID(target)
	if err != nil {
		return fmt.Errorf("failed to identify active pane in %s: %w", windowName, err)
	}

	// Reuse before creating. The TYPED form is correct here and only here: this
	// pane already exists and is running the user's interactive shell, so there
	// is no process to exec the command as (ADR-0021 part 4). No rollback branch
	// on purpose either - the pane already existed, so a failed send-keys leaves
	// it exactly as it was, and killing it would destroy a pane devgeta did not
	// create, and the user's shell with it.
	if idlePaneID, ok := w.idleShellPaneIn(windowName); ok {
		if err := w.Tmux.SendKeysToPane(idlePaneID, pane.Command); err != nil {
			return fmt.Errorf("failed to launch review in %s: %w", windowName, err)
		}
		if idlePaneID != coderPaneID {
			// Best-effort, same reasoning as the split path below.
			_ = w.Tmux.SelectPane(coderPaneID)
		}
		return nil
	}

	// This pane is NEW, so its command is exec'd as the pane's process at
	// creation, built from the resolution this pane already carries (ADR-0021
	// part 3). The shell is resolved here rather than at the top of the function
	// so the reuse branch above, which needs none, pays for none.
	shell := w.paneShell()
	if err := w.Tmux.SplitWindow(
		target,
		wtPath,
		splitVertical,
		pane.creationCommand(shell),
	); err != nil {
		return fmt.Errorf("failed to split window %s: %w", windowName, err)
	}

	// Best-effort: land the user back on their coder pane, not the reviewer,
	// the next time they attach to this window - split-window makes the new
	// pane active, and the review is already running correctly in it regardless
	// of which pane is "active" here. Swallow the error: not fatal, at most
	// log-worthy.
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
		// The "shell" layout's pane has no command (see shellPane), and a
		// window that already exists already has its shell. Repairing it is a
		// no-op rather than an Enter pressed into whatever pane is active -
		// which, in a window the user has since split, is not necessarily the
		// pane devgeta made.
		if layout.pane0TypedCommand() == "" {
			return nil
		}
		if err := w.Tmux.SendKeysToWindowInSession(
			session,
			windowName,
			layout.pane0TypedCommand(),
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
	// Both branches create pane 0, so both carry pane 0's command (ADR-0021:
	// any tmux call that brings a pane into existence carries that pane's
	// command). new-session is the one that reads like session setup rather
	// than pane setup, and it is the FIRST worktree for a repo - the common
	// case - so leaving it out would have kept the truncation bug alive
	// exactly where users hit it most.
	shell := w.paneShell()
	pane0Command := layout.pane0CreatedCommand(shell)
	if w.Tmux.HasSession(session) {
		if err := w.Tmux.CreateWindowInSession(
			session,
			windowName,
			wtPath,
			pane0Command,
		); err != nil {
			return fmt.Errorf("failed to create tmux window: %w", err)
		}
	} else {
		if err := w.Tmux.CreateSessionWithWindow(
			session,
			windowName,
			wtPath,
			pane0Command,
		); err != nil {
			return fmt.Errorf("failed to create tmux session: %w", err)
		}
	}

	if err := w.buildWindowPanes(session+":"+windowName, wtPath, shell, layout); err != nil {
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
//
// Resolves state and path via worktreeStateReal, NOT worktreeState +
// worktreePath: the worktree's real location can disagree with what the
// CONFIGURED worktree.location computes (e.g. after `dg wt move` without
// also changing the global default - a flow this cycle's own migration
// guide recommends). Before this fix, a worktree moved that way was
// invisible to this function: WtExists came back false for the real
// worktree, so a still-live tmux window got killed while the worktree
// itself was left completely untouched, and the function returned nil
// (success) either way. state and wtPath below are the SAME git-verified
// path throughout - used for the dirty check, the window kill, and the
// actual git/filesystem removal - never a separately-recomputed
// config-derived guess for any of them.
func (w *WorktreeManager) removeByRepo(repoSlug, name string, force bool) error {
	state, wtPath, err := w.worktreeStateReal(repoSlug, name)
	if err != nil {
		return err
	}
	// Resolved BEFORE anything is removed, while wtPath still exists: the
	// prune at the end needs a repo root, and asking git to resolve one from
	// a directory this function is about to delete only works beforehand.
	repoRoot := w.repoRootForPrune(repoSlug, wtPath)

	// Nothing real to remove: either git is holding a stale registration
	// whose directory is already gone (prune it and report success), or
	// there is genuinely no such worktree (an error the caller must see).
	//
	// Returning a bare nil for BOTH cases, as this used to, is what made a
	// ghost row unkillable: `d` in the dashboard reported success, pruned
	// nothing, and the next 3-second refresh read the same stale entry back
	// out of git and re-drew the row. A no-op that claims success is not a
	// no-op - it is a silent failure, and it must never be the answer when
	// the caller explicitly asked for a removal.
	if !state.WtExists && !state.WindowExists {
		if stale := w.staleWorktreePaths(repoRoot, name); len(stale) > 0 {
			if pruneErr := w.pruneStaleWorktrees(repoRoot); pruneErr != nil {
				return fmt.Errorf(
					"worktree '%s' is already gone but its stale git entry could not be pruned: %w",
					name,
					pruneErr,
				)
			}
			return nil
		}
		return fmt.Errorf("no worktree '%s' in repo '%s' to remove", name, repoSlug)
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
	_ = w.Tmux.KillWindow(state.WindowName)

	// removedByFallback records that git refused to remove the worktree and we
	// deleted the directory ourselves. That is the one path here that LEAVES
	// git holding a registration pointing at a directory that no longer
	// exists - in other words, it manufactures exactly the ghost this whole
	// change exists to eliminate - so the prune below stops being defensive
	// and becomes the step that must succeed.
	// The review journal is keyed by the real BRANCH, which is not `name`:
	// `name` is the flattened row/directory name (FlattenName strips "/"), so
	// branch "feat/login" arrives here as "feat-login". Resolved before the
	// removal, because afterwards there is no worktree left to ask. On failure
	// the journal is simply left for `review-notes --prune` — guessing from
	// `name` could delete a DIFFERENT branch's journal (a real branch named
	// "feat-login" alongside "feat/login").
	journalBranch, journalBranchErr := w.Git.BranchForWorktree(wtPath)

	removedByFallback := false
	if state.WtExists {
		if err := w.Git.RemoveWorktree(wtPath, true, name); err != nil {
			// The worktree came out cleanly and only `branch -D` failed: the
			// directory and its registration are already gone, so there is
			// nothing to fall back to and nothing to prune. This must be
			// reported, not swallowed - os.RemoveAll below would "succeed"
			// on the already-deleted path and turn a real failure into a
			// silent success, leaving the user with a branch they asked to
			// have deleted and no indication it survived.
			if errors.Is(err, git.ErrBranchDeleteFailed) {
				return fmt.Errorf("worktree '%s' removed, but: %w", name, err)
			}
			if rmErr := os.RemoveAll(wtPath); rmErr != nil {
				return fmt.Errorf("failed to remove worktree: %w", err)
			}
			removedByFallback = true
		}
	}

	// Prune from the repo root - the only anchor guaranteed to be a git
	// repository. This ran from filepath.Dir(wtPath) before, which for the
	// shared shape is ~/.local/share/devgeta/worktrees/<slug>, not a repo at
	// all, so the prune failed and the error was discarded: the entry this
	// call exists to clean up survived every single removal.
	//
	// The failure is only escalated on the fallback path. After a clean
	// `git worktree remove`, git has already dropped its own registration and
	// this prune has nothing left to do, so failing the whole removal over it
	// would report an error for an operation that fully succeeded - and it
	// would fire routinely, since pruneStaleWorktrees treats an unresolvable
	// repo root as an error and cursorRepoRoot legitimately returns "" for a
	// repo that is neither the current directory nor a known anchor.
	if pruneErr := w.pruneStaleWorktrees(repoRoot); pruneErr != nil {
		if removedByFallback {
			return fmt.Errorf(
				"worktree '%s' directory was removed, but git's stale entry for it could not be "+
					"pruned (it will keep reappearing until it is): %w",
				name,
				pruneErr,
			)
		}
		logger.L().Debugw(
			"worktree: defensive prune after a clean remove failed",
			"repo", repoSlug,
			"name", name,
			"error", pruneErr,
		)
	}

	w.dropReviewJournal(repoRoot, journalBranch, journalBranchErr)
	return nil
}

// dropReviewJournal deletes the removed branch's review journal (ADR-0012).
// This is the cleanup the journal's location was chosen for: the branch has
// just been deleted by RemoveWorktree above, so its remembered review
// exchanges describe work that no longer exists, and deleting them here is
// mechanical Go in the same operation rather than an instruction an agent has
// to remember.
//
// branchErr carries the outcome of resolving the branch before the removal.
// When it failed there is no safe key to delete by — the flattened row name is
// NOT the branch, and using it could destroy a different branch's journal — so
// the journal is left for `review-notes --prune`, which keys off branch
// existence and will collect it.
//
// Best-effort by design: the worktree and branch are already gone by now, so
// failing the removal over a leftover text file would report failure for an
// operation that succeeded.
func (w *WorktreeManager) dropReviewJournal(repoRoot, branch string, branchErr error) {
	if branchErr != nil || strings.TrimSpace(branch) == "" {
		logger.L().Debugw(
			"worktree: skipping review-journal cleanup, branch unresolved",
			"repo", repoRoot,
			"error", branchErr,
		)
		return
	}
	if err := reviewjournal.New(w.Git).Delete(repoRoot, branch); err != nil {
		w.warn(fmt.Sprintf(
			"worktree removed, but its review notes could not be deleted (%v); "+
				"run `devgeta task review-notes --prune` to clear them",
			err,
		))
	}
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
	err := config.Update(func(gc *config.GlobalConfig) error {
		gc.Worktree.UpsertRecentRepo(canonical, now)
		return nil
	})
	// Unconditionally, including on failure: a failed Update can still have
	// written, and this is the one place devgeta writes recent-repos, so
	// anything memoized from before it is now suspect. A same-second write of
	// the same size is invisible to globalConfig's stat, which is why the
	// write site drops the cache itself rather than relying on it.
	w.invalidateGlobalConfig()
	if err != nil {
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
