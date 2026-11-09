/*
 * Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"fmt"
	"strings"

	"github.com/cjairm/devgeta/internal/tooling/task"
	"github.com/spf13/cobra"
)

// standardHelpFunc renders the long (or short) description followed by Cobra's
// default usage block - Available Commands, Flags, Examples. The root sets it
// for the whole tree (cmd/root.go), so every command's help lists its real
// subcommands and flags.
//
// It exists as a named function because the root previously printed only
// Use+Long, which hid subcommands and flags everywhere and forced taskCmd and
// configCmd to opt out of it one subtree at a time. Keeping one help function
// for the whole tree is what stops a command from being left out of a listing
// by hand.
//
// It writes to OutOrStdout, not through cmd.Print, because cmd.Print resolves
// to OutOrStderr and so falls back to *stderr* when nothing has called SetOut.
// Cobra's own help function uses OutOrStdout; matching it is what keeps
// `dg --help | grep export` and `dg task --help > notes.txt` working. Asking
// for help is not an error, so it does not belong on the error stream.
func standardHelpFunc(cmd *cobra.Command, args []string) {
	out := cmd.OutOrStdout()
	if cmd.Long != "" {
		fmt.Fprintln(out, cmd.Long)
		fmt.Fprintln(out)
	} else if cmd.Short != "" {
		fmt.Fprintln(out, cmd.Short)
		fmt.Fprintln(out)
	}
	fmt.Fprint(out, cmd.UsageString())
}

// taskRunner is the interface used by task subcommands, enabling injection in tests.
// taskRunner is the interface used by task subcommands, enabling injection in tests.
type taskRunner interface {
	RefreshBranch(target string, rebase bool) error
	ResetMainBranch() error
	ReinstallLibraries() error
	ReinstallLibrary(name string) error
	DeleteBranch(target string) error
	Release(version, messageFile string, push bool) (string, error)
	Scratch(key string) (string, error)
	ScratchClean(target string) error
}

// newTaskManager is the factory used by task subcommands; overridden in tests.
var newTaskManager = func() taskRunner {
	return task.New()
}

var taskCmd = &cobra.Command{
	Use:     "task",
	Aliases: []string{"t"},
	Short:   "Developer utilities (git, npm, release, scratch) for people and scripts",
	Long: `Developer utility commands, run directly or through the dge() shell wrapper.

Four families:
  - git branch:  refresh-branch, reset-main-branch, delete-branch
  - npm deps:    reinstall-libraries, reinstall-library
  - release:     release
  - scratch:     scratch

Run "dg task <subcommand> --help" for flags and examples.`,
	Example: `  dg task refresh-branch
  dg task reinstall-library lodash
  dg task release v0.12.0 --message-file release-notes.txt`,
	// Without a Run, cobra prints help and exits 0 for an unknown subcommand,
	// so a script still calling a removed one would report success.
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// emitTaskResult prints a task's output (when non-empty) and returns its error.
func emitTaskResult(cmd *cobra.Command, out string, err error) error {
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		fmt.Fprintln(cmd.OutOrStdout(), out)
	}
	return nil
}

var taskRefreshBranchRebaseFlag bool

var taskRefreshBranchCmd = &cobra.Command{
	Use:   "refresh-branch [target] [--rebase]",
	Short: "Checkout target branch, pull, return to previous branch, and merge (or rebase)",
	Long: `Checkout the target branch (default: main), pull latest changes from origin,
return to the previous branch (git checkout -), and integrate target into it.

By default that integration is a merge — this is equivalent to the dge
refresh-branch shell utility, and merge stays the default here too.

--rebase rebases the previous branch onto target instead of merging it in,
for a repository whose merge gate rejects merge commits. It does not change
the default; pass it explicitly when you want linear history instead.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := ""
		if len(args) > 0 {
			target = args[0]
		}
		return newTaskManager().RefreshBranch(target, taskRefreshBranchRebaseFlag)
	},
}

var taskResetMainBranchCmd = &cobra.Command{
	Use:   "reset-main-branch",
	Short: "Checkout main and hard-reset to origin/main",
	Long: `Checkout the main branch and reset it hard to origin/main, discarding
any local commits or changes on main.

This is equivalent to the dge reset-main-branch shell utility.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return newTaskManager().ResetMainBranch()
	},
}

var taskDeleteBranchCmd = &cobra.Command{
	Use:   "delete-branch [target]",
	Short: "Checkout target, pull, then pick a branch to force-delete via fzf",
	Long: `Checkout the target branch (default: main), fetch, and pull, then open an
interactive fzf picker to select a local branch to force-delete (git branch -D).

This is equivalent to the dge delete-branch shell utility.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := ""
		if len(args) > 0 {
			target = args[0]
		}
		return newTaskManager().DeleteBranch(target)
	},
}

var taskReinstallLibrariesCmd = &cobra.Command{
	Use:   "reinstall-libraries",
	Short: "Clean git-ignored files, remove node_modules, and run npm install",
	Long: `Run git clean -Xdf, remove node_modules/, run npm install, and remove
tsconfig.tsbuildinfo. Useful for fixing dependency issues in Node.js projects.

This is equivalent to the dge reinstall-libraries shell utility.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return newTaskManager().ReinstallLibraries()
	},
}

var taskReinstallLibraryCmd = &cobra.Command{
	Use:   "reinstall-library <name>",
	Short: "Remove a specific node_modules package and run npm install",
	Long: `Remove node_modules/<name> and re-run npm install. Useful for fixing
a single corrupted or mis-linked package without reinstalling everything.

This is equivalent to the dge reinstall-library shell utility.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return newTaskManager().ReinstallLibrary(args[0])
	},
}

// taskReleaseMessageFileFlag / taskReleasePushFlag are release's flags.
var (
	taskReleaseMessageFileFlag string
	taskReleasePushFlag        bool
)

var taskReleaseCmd = &cobra.Command{
	Use:   "release <version> --message-file <file> [--push]",
	Short: "Automate the CLAUDE.md §9 squash-and-tag release flow",
	Long: `Automate the CLAUDE.md §9 push-and-tag workflow: verify a clean working tree
on the default branch, count commits ahead of origin/<default>, squash 2+ of
them into one commit using --message-file, create an annotated tag with the
same message, and push commit+tag together only when --push is passed.

version must match vMAJOR.MINOR.PATCH (e.g. v0.12.0) — strict semver only, no
prerelease suffixes. Every guard (version format, clean tree, default branch,
message file, tag-not-exists) runs before any mutation.

Without --push, nothing is pushed: the final line states exactly what remains
to run, e.g. "Tagged v0.12.0 (squashed 3 commits). Not pushed — run: git push
origin main --tags".`,
	Example: `  dg task release v0.12.0 --message-file release-notes.txt
  dg task release v0.12.0 --message-file release-notes.txt --push`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newTaskManager().
			Release(args[0], taskReleaseMessageFileFlag, taskReleasePushFlag)
		return emitTaskResult(cmd, out, err)
	},
}

// taskScratchCleanFlag is scratch's --clean flag.
var taskScratchCleanFlag string

// taskScratchKeyFlag is scratch's --key flag (ADR-0033).
var taskScratchKeyFlag string

var taskScratchCmd = &cobra.Command{
	Use:   "scratch [--key <name>] [--clean <path>]",
	Short: "Allocate or remove a devgeta-owned scratch directory (for agents)",
	Long: `Bare "dg task scratch" allocates a fresh, uniquely-named directory under
devgeta's scratch root (~/.cache/devgeta/scratch, ADR-0015) and prints its
absolute path — the destination for a command's working files instead of
/tmp, which both agents prompt on when written to directly.

--key <name> allocates (or re-derives) a directory at a fixed, predictable
path instead of a unique one, so a LATER, independent session that knows the
same key can find a file an earlier one left there — a hand-off, not a
private working directory (ADR-0033). This directory is SHARED, not
private: any process that passes the same key gets the same path, so two
sessions that pick the same key by accident share one directory, and
whichever writes last wins. It also does not age out on its own — an
unkeyed allocation is pruned after 24h by "dg configure --force" as a
safety net, but a keyed one persists until explicitly "--clean"ed, because
that is what makes a hand-off durable across sessions that may not run for
days. The key must be a single path element: no "/", no ".", no "..", not
empty or whitespace-only.

--clean <path> removes a directory this command allocated (keyed or not).
Pass it the exact path scratch printed: cleanup only accepts a direct child
of the scratch root whose name carries an allocation prefix, so a
reconstructed or parent path is refused rather than silently widened.`,
	Example: `  SCRATCH=$(dg task scratch)
  ... write files under "$SCRATCH" ...
  dg task scratch --clean "$SCRATCH"

  HANDOFF=$(dg task scratch --key issue-42)
  ... a later, independent session re-derives the same path ...
  dg task scratch --key issue-42
  dg task scratch --clean "$HANDOFF"`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		tm := newTaskManager()
		if taskScratchCleanFlag != "" {
			return tm.ScratchClean(taskScratchCleanFlag)
		}
		out, err := tm.Scratch(taskScratchKeyFlag)
		return emitTaskResult(cmd, out, err)
	},
}

func init() {
	rootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskRefreshBranchCmd)
	taskCmd.AddCommand(taskResetMainBranchCmd)
	taskCmd.AddCommand(taskDeleteBranchCmd)
	taskCmd.AddCommand(taskReinstallLibrariesCmd)
	taskCmd.AddCommand(taskReinstallLibraryCmd)
	taskCmd.AddCommand(taskReleaseCmd)
	taskCmd.AddCommand(taskScratchCmd)

	taskScratchCmd.Flags().
		StringVar(&taskScratchCleanFlag, "clean", "", "Remove a directory scratch previously allocated")
	taskScratchCmd.Flags().
		StringVar(&taskScratchKeyFlag, "key", "", "Allocate or re-derive a shared, keyed scratch directory instead of a unique one")

	taskRefreshBranchCmd.Flags().
		BoolVar(&taskRefreshBranchRebaseFlag, "rebase", false, "Rebase onto target instead of merging it in (default stays merge)")

	taskReleaseCmd.Flags().StringVar(
		&taskReleaseMessageFileFlag,
		"message-file",
		"",
		"File containing the squash-commit/tag message (required)",
	)
	taskReleaseCmd.Flags().BoolVar(
		&taskReleasePushFlag,
		"push",
		false,
		"Push the commit and tag to origin after tagging (default: false, tag-only)",
	)
	_ = taskReleaseCmd.MarkFlagRequired("message-file")
}
