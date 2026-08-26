/*
 * Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"github.com/cjairm/devgeta/internal/tooling/task"
	"github.com/spf13/cobra"
)

// issueRunner is the interface used by the issue task subcommands, enabling
// injection in tests. It mirrors task.IssueManager.
type issueRunner interface {
	IssueScope(issueNumber string) (string, error)
}

// newIssueTasks is the factory used by the issue subcommands; overridden in
// tests.
var newIssueTasks = func() issueRunner { return task.NewIssue() }

var taskIssueScopeCmd = &cobra.Command{
	Use:   "issue-scope <n>",
	Short: "Orient on a tracked issue in one call (for agents)",
	Long: `Report what already exists for issue n: its state and title; the pull
requests GitHub's own cross-reference confirms reference it; local branches
whose name looks like a reference to it; and any worktree holding one of
those branches.

Pull request references come from GitHub's own cross-referenced timeline
events (the same signal its UI uses for "N linked pull requests"), same
repository only — never a text search of PR bodies or titles, which is
boundary-wrong (a search for "12" also matches inside "1234"). They are
reported as confirmed.

Branch references are reported as CANDIDATES only, never confirmed: a local
branch is a candidate when its name contains the exact digit run of n, with
the characters immediately before and after (if any) not being a letter,
digit, or '.' (an optional single '#' may precede). No branch-naming
convention is assumed (ADR-0035) — a bare number in a branch name is a
coincidence as often as a link, so it is never promoted past "candidate".

When nothing references the issue, every section prints its own "none"
line rather than a single collapsed sentinel — the labels are always present
so a caller can rely on their position.`,
	Example: `  dg task issue-scope 42`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newIssueTasks().IssueScope(args[0])
		return emitPRResult(cmd, out, err)
	},
}

func init() {
	taskCmd.AddCommand(taskIssueScopeCmd)
}
