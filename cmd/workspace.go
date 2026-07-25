/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	tuiworktree "github.com/cjairm/devgeta/internal/tui/worktree"
	"github.com/spf13/cobra"
)

var workspaceCmd = &cobra.Command{
	Use:     "ws",
	Aliases: []string{"workspace"},
	Short:   "Open the workspace dashboard (TUI)",
	Long: `Open the workspace dashboard, a single view of your repo worktrees and tmux sessions (alias: workspace).

The dashboard lists every repo workspace with its git worktrees alongside
standalone tmux sessions that aren't tied to a worktree, so you can manage
both from one place.

Key session actions:
  s       New session
  enter   Switch/attach to the selected session
  d       Delete/kill the selected session`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return tuiworktree.Run()
	},
}

func init() {
	rootCmd.AddCommand(workspaceCmd)
}
