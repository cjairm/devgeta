/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
	"github.com/cjairm/devgeta/pkg/utils"
	"github.com/spf13/cobra"
)

var worktreeCmd = &cobra.Command{
	Use:     "worktree",
	Aliases: []string{"wt"},
	Short:   "Manage git worktrees with tmux windows",
	Long: `Manage git worktrees with tmux windows (alias: wt).

Each worktree gets its own tmux window in the current session with an AI assistant running,
enabling parallel AI-assisted development across multiple branches.

Worktrees are stored in ~/.local/share/devgeta/worktrees/<repo-slug>/,
and tmux windows are prefixed with "wt-" for easy identification.

Examples:
  dg worktree create feature-login                # Create worktree + window with default AI/layout
  dg worktree create feature-login --ai claude    # Create with Claude Code
  dg worktree create feature-login --layout nvim  # Create with the nvim-only layout
  dg wt c feature-login                           # Same, using short form
  dg wt new fix-auth --repo ~/code/api            # Create for another repo (window opens in its session)
  dg wt l                                         # List all worktrees
  dg wt rm                                        # Remove worktree (fzf selection)
  dg wt repair feature-login                      # Repair missing window
  dg wt prune                                     # Remove all worktrees`,
}

var worktreeCreateCmd = &cobra.Command{
	Use:     "create <name>",
	Aliases: []string{"c", "new"},
	Short:   "Create a new worktree with tmux window",
	Long: `Create a new git worktree with an associated tmux window (aliases: c, new).

This command:
  1. Creates a new git worktree in ~/.local/share/devgeta/worktrees/<repo>/<name>
  2. Creates a new branch with the same name
  3. Creates a new tmux window named wt-<repo>-<name> in the current session
  4. Launches the selected AI coder in the window

If a branch named <name> already exists locally, create adopts it into the
worktree instead of failing. If that branch is currently checked out in the
main clone, the source checkout is moved to the repo's default branch first
(git can't have the same branch checked out in two places at once) — a note
is printed so the switch isn't a surprise.

With --repo you don't need to be inside the repository: the worktree is
created for the repo at the given path, and the window opens in a tmux
session named after the repo (created if missing, reused otherwise). When run
inside tmux, the client switches to the new window.

Window layout selection precedence (--layout and --ai are mutually exclusive):
  1. --layout flag (explicit layout name)
  2. --ai flag, derived into a single-pane layout
  3. DEVGETA_WORKTREE_AI environment variable, derived into a single-pane layout
  4. worktree.default_layout in global_config.yaml
  5. worktree.default_ai in global_config.yaml, derived into a single-pane layout
  6. Default: opencode, single-pane

Valid AI coders: opencode (oc), claude (cc, claudecode)
Valid layouts: opencode, claude, claude-nvim, nvim

After creation, switch to the window with:
  <prefix> + [window number] or <prefix> + w to see all windows`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		layout, err := resolveWorktreeLayout(createLayoutFlag, createAIFlag)
		if err != nil {
			return err
		}

		wm := worktree.New()
		var repoRoot string
		if repoFlag != "" {
			if err := wm.CreateAt(repoFlag, name, layout, forceFlag); err != nil {
				return err
			}
			repoRoot, _ = wm.Git.GetRepoRootIn(paths.ExpandHome(repoFlag))
		} else {
			if err := wm.Create(name, layout, forceFlag); err != nil {
				return err
			}
			repoRoot, _ = wm.Git.GetRepoRoot()
		}
		repoSlug := repoRoot
		if repoRoot != "" {
			repoSlug = repoRoot[findLastSlash(repoRoot)+1:]
		}
		utils.PrintSuccess(fmt.Sprintf("Created worktree: %s/%s", repoSlug, name))
		utils.PrintSuccess(
			fmt.Sprintf("Created tmux window: %s", worktree.GetWindowName(repoSlug, name)),
		)
		utils.PrintInfo("Switch to window with: <prefix> + w")
		return nil
	},
}

var worktreeListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"l", "ls"},
	Short:   "List all worktrees with window status",
	Long: `List all git worktrees managed by devgeta with their tmux window status (aliases: l, ls).

Shows worktrees from all repos in ~/.local/share/devgeta/worktrees/ along with:
  - Repo name
  - Branch name
  - Associated tmux window name
  - Whether the window is currently active`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		wm := worktree.New()

		statuses, err := wm.List()
		if err != nil {
			return err
		}

		if len(statuses) == 0 {
			utils.PrintInfo("No worktrees found")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "REPO\tWORKTREE\tBRANCH\tWINDOW\tSTATUS")
		for _, s := range statuses {
			status := "No window"
			if s.WindowActive {
				status = "Active"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				s.Repo, s.Name, s.Branch, s.TmuxWindow, status)
		}
		return w.Flush()
	},
}

var worktreeRemoveCmd = &cobra.Command{
	Use:     "remove [name]",
	Aliases: []string{"rm", "r"},
	Short:   "Remove a worktree and its tmux window",
	Long: `Remove a git worktree and kill its associated tmux window (aliases: rm, r).

This command:
  1. Kills the tmux window wt-<repo>-<name> if it exists
  2. Removes the git worktree
  3. Deletes the branch (force delete with -D)

If no name is provided, opens an interactive fzf picker to select a worktree.

Use --force to remove even if the worktree has uncommitted changes.

Warning: Any uncommitted changes in the worktree will be lost.`,
	Args: cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names, err := worktree.New().ListNames()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		wm := worktree.New()
		var name string

		if len(args) == 0 {
			selected, err := wm.SelectWorktreeInteractively("Select worktree to remove:")
			if err != nil {
				return err
			}
			name = selected
		} else {
			name = args[0]
		}

		if err := wm.Remove(name, forceFlag); err != nil {
			return err
		}

		utils.PrintSuccess(fmt.Sprintf("Removed worktree: %s", name))
		return nil
	},
}

var worktreeRepairCmd = &cobra.Command{
	Use:   "repair <name>",
	Short: "Repair a worktree by recreating its tmux window",
	Long: `Repair a worktree by recreating its tmux window and launching the AI coder.

This command:
  1. Checks that the worktree directory exists
  2. Creates a new tmux window if missing
  3. Launches the selected AI coder in the window

Window layout selection follows the same precedence as create (--layout and
--ai are mutually exclusive):
  1. --layout flag (explicit layout name)
  2. --ai flag, derived into a single-pane layout
  3. DEVGETA_WORKTREE_AI environment variable, derived into a single-pane layout
  4. worktree.default_layout in global_config.yaml
  5. worktree.default_ai in global_config.yaml, derived into a single-pane layout
  6. Default: opencode, single-pane

Valid AI coders: opencode (oc), claude (cc, claudecode)
Valid layouts: opencode, claude, claude-nvim, nvim

Note: repair does not remember the layout a worktree was created with. If the
window is missing, it is rebuilt from scratch using the precedence above,
same as create. If the window already exists (e.g. only a pane inside it was
closed), repair only relaunches the AI coder in the existing window — it does
not add or recreate missing panes, since there is no way to tell whether the
surviving panes already match the requested layout.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		layout, err := resolveWorktreeLayout(repairLayoutFlag, repairAIFlag)
		if err != nil {
			return err
		}

		wm := worktree.New()
		if err := wm.Repair(name, layout); err != nil {
			return err
		}

		utils.PrintSuccess(fmt.Sprintf("Repaired worktree: %s", name))
		utils.PrintSuccess(
			fmt.Sprintf("Launched AI coder in window: %s", wm.WindowNameFor(name)),
		)
		return nil
	},
}

var worktreePruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove all worktrees",
	Long: `Remove all worktrees managed by devgeta.

This command prompts for confirmation before removing all worktrees
across all repos in ~/.local/share/devgeta/worktrees/.

Each worktree is removed using the same logic as 'dg wt remove':
  - Kills the tmux window if present
  - Removes the git worktree
  - Deletes the branch

Example:
  dg wt prune    # Prompts for confirmation, then removes all worktrees`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		wm := worktree.New()

		if err := wm.Prune(); err != nil {
			return err
		}

		utils.PrintSuccess("All worktrees removed")
		return nil
	},
}

// createAIFlag/createLayoutFlag and repairAIFlag/repairLayoutFlag are
// deliberately command-local rather than shared: a single shared var bound
// to both commands' flags (the old aiFlag) works today only because cobra
// re-populates it fresh on every invocation, but it's a state-bleed smell
// that bites any test running more than one of these commands in-process.
// forceFlag/repoFlag stay shared package vars — untouched by this change,
// out of scope.
var (
	createAIFlag     string
	createLayoutFlag string
	repairAIFlag     string
	repairLayoutFlag string
	forceFlag        bool
	repoFlag         string
)

func init() {
	rootCmd.AddCommand(worktreeCmd)
	worktreeCmd.AddCommand(worktreeCreateCmd)
	worktreeCmd.AddCommand(worktreeListCmd)
	worktreeCmd.AddCommand(worktreeRemoveCmd)
	worktreeCmd.AddCommand(worktreeRepairCmd)
	worktreeCmd.AddCommand(worktreePruneCmd)

	worktreeCreateCmd.Flags().
		StringVarP(&createAIFlag, "ai", "a", "", "AI coder to launch (opencode, oc, claude, cc, claudecode)")
	worktreeCreateCmd.Flags().
		StringVarP(&createLayoutFlag, "layout", "l", "", "Window layout to build (opencode, claude, claude-nvim, nvim)")
	worktreeCreateCmd.MarkFlagsMutuallyExclusive("ai", "layout")
	worktreeCreateCmd.Flags().
		BoolVarP(&forceFlag, "force", "f", false, "Skip hook compatibility check")
	worktreeCreateCmd.Flags().
		StringVarP(&repoFlag, "repo", "r", "",
			"Path to the repository (defaults to the repo containing the current directory); the window opens in the repo's tmux session")
	_ = worktreeCreateCmd.RegisterFlagCompletionFunc(
		"repo",
		func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveFilterDirs
		},
	)
	worktreeRepairCmd.Flags().
		StringVarP(&repairAIFlag, "ai", "a", "", "AI coder to launch (opencode, oc, claude, cc, claudecode)")
	worktreeRepairCmd.Flags().
		StringVarP(&repairLayoutFlag, "layout", "l", "", "Window layout to build (opencode, claude, claude-nvim, nvim)")
	worktreeRepairCmd.MarkFlagsMutuallyExclusive("ai", "layout")
	worktreeRemoveCmd.Flags().
		BoolVarP(&forceFlag, "force", "f", false, "Force removal even if worktree has uncommitted changes")
}

var globalConfig config.GlobalConfig

// resolveWorktreeLayout is the single load+resolve sequence create's and
// repair's RunE both need - they differ only in which flag vars they pass,
// so this is the one place that sequence is written.
func resolveWorktreeLayout(layoutFlag, aiFlag string) (worktree.Layout, error) {
	loadWorktreeGlobalConfig()
	return worktree.ResolveLayout(layoutFlag, resolveWorktreeAIFlag(aiFlag), &globalConfig)
}

// loadWorktreeGlobalConfig loads global_config.yaml into the package-level
// globalConfig so ResolveLayout can see worktree.default_ai/default_layout
// from the CLI path (the dg ws dashboard loads its own gc elsewhere and never
// hit this gap). A missing file is expected on a fresh install - not fatal, mirroring
// RepoCandidates' "if err := gc.Load(); err == nil" fallback in
// repo_candidates.go, so create/repair keep working with globalConfig at its
// zero value (empty DefaultAI/DefaultLayout). But Load() returns the same
// error type for "file missing" and "file exists but is corrupt/unreadable
// YAML" - silently ignoring both would hide a real config problem, so only
// the missing-file case is swallowed; anything else is surfaced as a
// warning (still non-fatal: a corrupt config shouldn't block create/repair,
// but it also shouldn't be invisible).
func loadWorktreeGlobalConfig() {
	if err := globalConfig.Load(); err != nil && !os.IsNotExist(err) {
		logger.L().Warnw("worktree: failed to load global config, using defaults", "error", err)
	}
}

// resolveWorktreeAIFlag resolves an aiAlias from ONLY --ai/DEVGETA_WORKTREE_AI
// (not through ResolveAIAlias's folded flag->env->default_ai->opencode
// chain), leaving it "" when neither is given - that "" is what lets
// ResolveLayout consult worktree.default_layout before falling back to
// worktree.default_ai and then opencode. See ResolveLayout's doc comment for
// why a folded alias (one that always resolves to at least "opencode") must
// never be passed here instead.
func resolveWorktreeAIFlag(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return os.Getenv("DEVGETA_WORKTREE_AI")
}

func findLastSlash(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return i
		}
	}
	return 0
}
