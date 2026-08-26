/*
 * Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/cjairm/devgeta/internal/tooling/task"
	"github.com/spf13/cobra"
)

// prRunner is the interface used by the PR task subcommands, enabling injection
// in tests. It mirrors task.PRManager.
type prRunner interface {
	ReviewThreads(prNumber, state string) (string, error)
	ResolveThread(threadID string) (string, error)
	UnresolveThread(threadID string) (string, error)
	ReplyThread(threadID, body string) (string, error)
	SubmitReview(prNumber, verdict, body, commentsJSON, commitID string) (string, error)
	CreatePR(title, body, base string) (string, error)
	UpdatePRDescription(prNumber, body string) (string, error)
	ApprovePR(prNumber, body, commitID string) (string, error)
	RequestChangesPR(prNumber, body string) (string, error)
	RequestReviewPR(prNumber string, reviewers []string) (string, error)
	CommentPR(prNumber, body string) (string, error)
	MergePR(prNumber, method string) (string, error)
	PRView(prNumber string) (string, error)
	PRChecks(prNumber string) (string, error)
	PRReviewTarget(prNumber string) (string, error)
	PRReviewState(prNumber string) (string, error)
	PRState(prNumber string) (string, error)
	CurrentPR() (string, error)
	CurrentRepo() (string, error)
}

// newPRTasks is the factory used by the PR subcommands; overridden in tests.
var newPRTasks = func() prRunner { return task.NewPR() }

// Shared flags for the PR subcommands. Only one subcommand runs per invocation,
// so sharing these package-level vars is safe.
var (
	prFlag         string
	prStateFlag    string
	prBodyFlag     string
	prBodyFileFlag string
	prTitleFlag    string
	prBaseFlag     string
	prMethodFlag   string
	prEventFlag    string
	prCommentsFile string
	prCommitFlag   string
)

// commitFlagUsage documents --commit for the two commands that post a review.
// It is deliberately explicit that the anchor is attribution: GitHub accepts a
// review whose commit is behind the head, so a caller must not read this flag
// as a guard against the PR moving mid-review.
const commitFlagUsage = "Anchor the review to this commit sha (attribution: " +
	"inline comments hang off that diff and the review names it; it does not " +
	"reject a newer head)"

// resolveBody returns the body text to use, preferring --body-file over --body.
// The file contents are passed through verbatim, so GitHub-Flavored Markdown
// (headings, lists, fenced code, multi-line) renders as written. Passing both
// flags is an error.
func resolveBody(body, bodyFile string) (string, error) {
	if bodyFile != "" {
		if body != "" {
			return "", fmt.Errorf("pass either --body or --body-file, not both")
		}
		data, err := os.ReadFile(bodyFile)
		if err != nil {
			return "", fmt.Errorf("failed to read --body-file %q: %w", bodyFile, err)
		}
		return string(data), nil
	}
	return body, nil
}

// emitPRResult prints a task's output (when non-empty) and returns its error.
func emitPRResult(cmd *cobra.Command, out string, err error) error {
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		fmt.Fprintln(cmd.OutOrStdout(), out)
	}
	return nil
}

var taskReviewThreadsCmd = &cobra.Command{
	Use:   "review-threads",
	Short: "Show PR review threads as compact markdown (for agents)",
	Long: `Fetch a pull request's review feedback and render it as compact markdown:
inline review threads, review summary bodies (Approve/Request-changes/Comment
text with no line anchor), and top-level conversation comments.

--state filters only the inline review threads: unresolved (default), resolved,
or all. Review summaries and conversation comments have no resolved state, so
they are always included.
--pr targets a PR number; omit it to use the current branch's PR.

Each inline thread renders as "## file:line (thread <id>)", an optional diff
hunk, and one line per comment "**author** (<id>): body" — thread and comment
ids feed the resolve-thread / reply-thread commands. Review summaries render as
"**author** [STATE]: body" under "## Review summaries"; conversation comments
render as "**author**: body" under "## Conversation".`,
	Example: `  dg task review-threads                 # unresolved on the current branch's PR
  dg task review-threads --state all
  dg task review-threads --pr 42 --state resolved`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newPRTasks().ReviewThreads(prFlag, prStateFlag)
		return emitPRResult(cmd, out, err)
	},
}

var taskResolveThreadCmd = &cobra.Command{
	Use:   "resolve-thread <thread-id>",
	Short: "Mark a PR review thread as resolved",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newPRTasks().ResolveThread(args[0])
		return emitPRResult(cmd, out, err)
	},
}

var taskUnresolveThreadCmd = &cobra.Command{
	Use:   "unresolve-thread <thread-id>",
	Short: "Reopen a resolved PR review thread",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newPRTasks().UnresolveThread(args[0])
		return emitPRResult(cmd, out, err)
	},
}

var taskReplyThreadCmd = &cobra.Command{
	Use:   "reply-thread <thread-id> [body]",
	Short: "Reply to a PR review thread (body inline or via --body-file)",
	Long: `Reply to a PR review thread. Provide the body inline as the second argument
or via --body-file (Markdown is rendered by GitHub). Pass exactly one.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		inline := ""
		if len(args) > 1 {
			inline = args[1]
		}
		body, err := resolveBody(inline, prBodyFileFlag)
		if err != nil {
			return err
		}
		if strings.TrimSpace(body) == "" {
			return fmt.Errorf("reply-thread requires a body (inline arg or --body-file)")
		}
		out, err := newPRTasks().ReplyThread(args[0], body)
		return emitPRResult(cmd, out, err)
	},
}

var taskSubmitReviewCmd = &cobra.Command{
	Use:   "submit-review",
	Short: "Post one PR review with optional inline comments (--event approve|request-changes|comment)",
	Long: `Submit a pull request review in a single call: a verdict, an optional Markdown
summary body, and optional inline comments anchored to diff lines.

--event selects the verdict: approve, request-changes, or comment (required).
--body / --body-file supply the review summary (Markdown).
--comments-file points to a JSON file holding an array of inline comments, each
{"path","line","body"} (optionally "start_line" and "side"); they are posted as
line-anchored review comments in the same submission.
--pr targets a PR number; omit it to use the current branch's PR.
--commit anchors the review to the commit sha that was actually reviewed. This
is attribution, not enforcement: GitHub does not reject a review whose commit is
behind the head. What it buys is that the inline comments hang off the diff that
was read (GitHub shows them as outdated once the head moves), the review record
names that commit, and dismiss-stale-approvals has a sha to key off.

A request-changes or comment review needs a body or inline comments; approve may
have neither.`,
	Example: `  devgeta task submit-review --event approve --body "LGTM"
  devgeta task submit-review --event request-changes --body-file review.md --comments-file comments.json
  devgeta task submit-review --pr 42 --event comment --comments-file comments.json
  devgeta task submit-review --pr 42 --event approve --body "LGTM" --commit 9f2c1ab`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := resolveBody(prBodyFlag, prBodyFileFlag)
		if err != nil {
			return err
		}
		comments := ""
		if prCommentsFile != "" {
			data, err := os.ReadFile(prCommentsFile)
			if err != nil {
				return fmt.Errorf("failed to read --comments-file %q: %w", prCommentsFile, err)
			}
			comments = string(data)
		}
		out, err := newPRTasks().SubmitReview(prFlag, prEventFlag, body, comments, prCommitFlag)
		return emitPRResult(cmd, out, err)
	},
}

var taskCreatePRCmd = &cobra.Command{
	Use:   "create-pr",
	Short: "Open a pull request from the current branch",
	Example: `  dg task create-pr --title "Add task command" --body "Implements **dg task**"
  dg task create-pr --title "Add task command" --body-file pr.md --base main`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := resolveBody(prBodyFlag, prBodyFileFlag)
		if err != nil {
			return err
		}
		out, err := newPRTasks().CreatePR(prTitleFlag, body, prBaseFlag)
		return emitPRResult(cmd, out, err)
	},
}

var taskUpdatePRDescriptionCmd = &cobra.Command{
	Use:   "update-pr-description",
	Short: "Replace a pull request's description/body",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := resolveBody(prBodyFlag, prBodyFileFlag)
		if err != nil {
			return err
		}
		out, err := newPRTasks().UpdatePRDescription(prFlag, body)
		return emitPRResult(cmd, out, err)
	},
}

var taskApprovePRCmd = &cobra.Command{
	Use:   "approve-pr",
	Short: "Approve a pull request",
	Long: `Approve a pull request, optionally with a one-line body.

--pr targets a PR number; omit it to use the current branch's PR.
--commit anchors the approval to the commit sha that was actually reviewed —
the same attribution (not enforcement) described under submit-review. Passing it
switches the call to the REST reviews endpoint, because ` + "`gh pr review`" + `
cannot carry a commit id; without it the plain gh route is used.`,
	Example: `  devgeta task approve-pr --body "LGTM"
  devgeta task approve-pr --pr 42 --body "LGTM" --commit 9f2c1ab`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := resolveBody(prBodyFlag, prBodyFileFlag)
		if err != nil {
			return err
		}
		out, err := newPRTasks().ApprovePR(prFlag, body, prCommitFlag)
		return emitPRResult(cmd, out, err)
	},
}

var taskRequestChangesPRCmd = &cobra.Command{
	Use:   "request-changes-pr",
	Short: "Request changes on a pull request (body required)",
	Example: `  dg task request-changes-pr --pr 42 --body "Please add tests"
  dg task request-changes-pr --pr 42 --body-file review.md`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := resolveBody(prBodyFlag, prBodyFileFlag)
		if err != nil {
			return err
		}
		out, err := newPRTasks().RequestChangesPR(prFlag, body)
		return emitPRResult(cmd, out, err)
	},
}

var taskRequestReviewCmd = &cobra.Command{
	Use:   "request-review <reviewer> [reviewer...]",
	Short: "Re-request review from one or more reviewers (adds them to the PR's reviewers list)",
	Long: `Add reviewers back to a pull request's requested-reviewers list. GitHub
re-requests review from a reviewer who already reviewed, so this hands the PR
back after feedback is addressed. --pr targets a PR number; omit it to use the
current branch's PR.`,
	Example: `  dg task request-review octocat
  dg task request-review --pr 42 octocat hubot`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newPRTasks().RequestReviewPR(prFlag, args)
		return emitPRResult(cmd, out, err)
	},
}

var taskCommentPRCmd = &cobra.Command{
	Use:   "comment-pr",
	Short: "Post a top-level comment on a pull request (body required)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := resolveBody(prBodyFlag, prBodyFileFlag)
		if err != nil {
			return err
		}
		out, err := newPRTasks().CommentPR(prFlag, body)
		return emitPRResult(cmd, out, err)
	},
}

var taskMergePRCmd = &cobra.Command{
	Use:     "merge-pr",
	Short:   "Merge a pull request (--method squash|merge|rebase)",
	Example: `  dg task merge-pr --method squash      # current branch's PR`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newPRTasks().MergePR(prFlag, prMethodFlag)
		return emitPRResult(cmd, out, err)
	},
}

var taskPRViewCmd = &cobra.Command{
	Use:   "pr-view",
	Short: "Show a compact summary of a pull request",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newPRTasks().PRView(prFlag)
		return emitPRResult(cmd, out, err)
	},
}

var taskPRChecksCmd = &cobra.Command{
	Use:   "pr-checks",
	Short: "Show CI check status for a pull request",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newPRTasks().PRChecks(prFlag)
		return emitPRResult(cmd, out, err)
	},
}

var taskPRReviewTargetCmd = &cobra.Command{
	Use:   "pr-review-target",
	Short: "Print the immutable review target for a pull request (base/head SHAs, journal key, files)",
	Long: `Resolve what a pull request review should look at, as fixed commit SHAs.

It fetches the PR's head (refs/pull/<n>/head, which the upstream repo serves for
fork PRs too) and its base branch read-only into non-branch refs — no local
branch moves, nothing is checked out, the working tree is untouched — then
prints four things:

  base:    the MERGE BASE of the base branch and the PR head, so a two-dot
           base..head diff is exactly the diff GitHub shows. (A base branch tip
           would inject every commit merged into it since the PR opened as if
           this PR were undoing them.)
  head:    the PR's head commit
  journal: pr/<owner>/<repo>/<number> — the PR-scoped review journal key, for
           review-notes/review-note --branch, so a PR's review memory is never
           mixed with a checkout branch's
  files:   the changed files in the range, lockfile-style noise filtered out

Both SHAs are resolved once here so every later step reads one fixed snapshot.
A failed fetch ends the command with an error rather than falling back to local
refs: reviewing a stale ref produces a confident review of code the PR no
longer contains.

The two fetched refs (refs/devgeta/pr/<n>/head and .../base) are left in place
on purpose: later steps of the review read them, and holding them keeps git gc
from collecting the commits mid-review. They are reused per PR number. To drop
them: git update-ref -d refs/devgeta/pr/<n>/head (and .../base).

--pr targets a PR number; omit it to use the current branch's PR.`,
	Example: `  dg task pr-review-target --pr 42
  dg task pr-review-target             # the current branch's PR`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newPRTasks().PRReviewTarget(prFlag)
		return emitPRResult(cmd, out, err)
	},
}

var taskPRReviewStateCmd = &cobra.Command{
	Use:   "pr-review-state",
	Short: "Print whether a pull request currently wants a review from you",
	Long: `Read one pull request's current review-request state and print it as three
lines:

  pr: open
  requested: yes
  my-review: none

  pr:        open | draft | merged | closed
  requested: yes | no — is the authenticated GitHub user (gh api user) named in
             the PR's reviewRequests RIGHT NOW. A request addressed to a team
             that does not name the user is not a request from them.
  my-review: approved | changes-requested | commented | none — the state of that
             user's latest submitted review

This is a state read, not an event log. GitHub maintains all three facts:
submitting any review (approve, request-changes, or comment alike) removes the
user from reviewRequests, and the re-request button puts them back. So the same
field answers "is a review wanted", "was it already answered", and "did the
author ask again" — devgeta keeps no record of its own that could go stale.

draft is reported separately from open because a requested draft is still a
draft; a caller that treats drafts as unfinished work needs to see it.

Nothing is guessed: a state gh reports that devgeta does not recognize ends the
command with an error rather than resolving to a wrong answer.

--pr targets a PR number; omit it to use the current branch's PR.`,
	Example: `  dg task pr-review-state --pr 42
  dg task pr-review-state             # the current branch's PR`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newPRTasks().PRReviewState(prFlag)
		return emitPRResult(cmd, out, err)
	},
}

var taskPRStateCmd = &cobra.Command{
	Use:   "pr-state",
	Short: "One-call answer to \"where does this PR stand?\" (for agents)",
	Long: `Compose pr-view, pr-checks, review-threads, and pr-review-state's own
underlying reads into one compact payload — five labeled lines:

  pr: open
  requested: yes
  my-review: none
  checks: pass=3 fail=1 pending=0 skipping=0 cancel=0
  threads-unresolved: 2

This never re-queries GitHub for a fact one of those four commands already
answers (ADR-0034): it composes the same underlying reads and reduces them
to counts and states only, never thread bodies or a per-check log digest —
pass --checks-digest to pr-checks directly if you need that. If one part
fails to fetch (e.g. checks temporarily unavailable), that line reports
"unavailable: <reason>" and the rest still render; the command itself only
fails if the PR cannot be resolved at all.

The four original commands are unchanged and still exist for when you want
just one of these answers, or pr-checks' failing-log digest, or
review-threads' thread bodies.

--pr targets a PR number; omit it to use the current branch's PR.`,
	Example: `  dg task pr-state --pr 42
  dg task pr-state             # the current branch's PR`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newPRTasks().PRState(prFlag)
		return emitPRResult(cmd, out, err)
	},
}

var taskCurrentPRCmd = &cobra.Command{
	Use:   "current-pr",
	Short: "Print the PR number for the current branch",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newPRTasks().CurrentPR()
		return emitPRResult(cmd, out, err)
	},
}

var taskCurrentRepoCmd = &cobra.Command{
	Use:   "current-repo",
	Short: "Print the current repository as owner/name",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newPRTasks().CurrentRepo()
		return emitPRResult(cmd, out, err)
	},
}

func init() {
	taskCmd.AddCommand(taskReviewThreadsCmd)
	taskCmd.AddCommand(taskResolveThreadCmd)
	taskCmd.AddCommand(taskUnresolveThreadCmd)
	taskCmd.AddCommand(taskReplyThreadCmd)
	taskCmd.AddCommand(taskSubmitReviewCmd)
	taskCmd.AddCommand(taskCreatePRCmd)
	taskCmd.AddCommand(taskUpdatePRDescriptionCmd)
	taskCmd.AddCommand(taskApprovePRCmd)
	taskCmd.AddCommand(taskRequestChangesPRCmd)
	taskCmd.AddCommand(taskRequestReviewCmd)
	taskCmd.AddCommand(taskCommentPRCmd)
	taskCmd.AddCommand(taskMergePRCmd)
	taskCmd.AddCommand(taskPRViewCmd)
	taskCmd.AddCommand(taskPRChecksCmd)
	taskCmd.AddCommand(taskPRReviewTargetCmd)
	taskCmd.AddCommand(taskPRReviewStateCmd)
	taskCmd.AddCommand(taskPRStateCmd)
	taskCmd.AddCommand(taskCurrentPRCmd)
	taskCmd.AddCommand(taskCurrentRepoCmd)

	taskReviewThreadsCmd.Flags().StringVar(&prFlag, "pr", "", "PR number (default: current branch)")
	taskReviewThreadsCmd.Flags().
		StringVar(&prStateFlag, "state", "unresolved", "Filter: unresolved, resolved, or all")

	// bodyFileUsage documents the markdown-from-file alternative shared by the
	// body-bearing commands.
	const bodyFileUsage = "Read the body from a file (Markdown; alternative to --body)"

	taskReplyThreadCmd.Flags().StringVar(&prBodyFileFlag, "body-file", "", bodyFileUsage)

	taskSubmitReviewCmd.Flags().StringVar(&prFlag, "pr", "", "PR number (default: current branch)")
	taskSubmitReviewCmd.Flags().
		StringVar(&prEventFlag, "event", "", "Verdict: approve, request-changes, or comment (required)")
	taskSubmitReviewCmd.Flags().
		StringVar(&prBodyFlag, "body", "", "Review summary body (Markdown)")
	taskSubmitReviewCmd.Flags().StringVar(&prBodyFileFlag, "body-file", "", bodyFileUsage)
	taskSubmitReviewCmd.Flags().
		StringVar(&prCommentsFile, "comments-file", "", "JSON file: array of inline comments ({path,line,body})")
	taskSubmitReviewCmd.Flags().StringVar(&prCommitFlag, "commit", "", commitFlagUsage)
	_ = taskSubmitReviewCmd.MarkFlagRequired("event")

	taskUpdatePRDescriptionCmd.Flags().
		StringVar(&prFlag, "pr", "", "PR number (default: current branch)")
	taskUpdatePRDescriptionCmd.Flags().
		StringVar(&prBodyFlag, "body", "", "New PR description (Markdown)")
	taskUpdatePRDescriptionCmd.Flags().StringVar(&prBodyFileFlag, "body-file", "", bodyFileUsage)

	taskCreatePRCmd.Flags().StringVar(&prTitleFlag, "title", "", "PR title (required)")
	taskCreatePRCmd.Flags().StringVar(&prBodyFlag, "body", "", "PR body (Markdown)")
	taskCreatePRCmd.Flags().StringVar(&prBodyFileFlag, "body-file", "", bodyFileUsage)
	taskCreatePRCmd.Flags().
		StringVar(&prBaseFlag, "base", "", "Base branch (default: repo default)")
	_ = taskCreatePRCmd.MarkFlagRequired("title")

	taskApprovePRCmd.Flags().StringVar(&prFlag, "pr", "", "PR number (default: current branch)")
	taskApprovePRCmd.Flags().
		StringVar(&prBodyFlag, "body", "", "Optional approval comment (Markdown)")
	taskApprovePRCmd.Flags().StringVar(&prBodyFileFlag, "body-file", "", bodyFileUsage)
	taskApprovePRCmd.Flags().StringVar(&prCommitFlag, "commit", "", commitFlagUsage)

	taskRequestChangesPRCmd.Flags().
		StringVar(&prFlag, "pr", "", "PR number (default: current branch)")
	taskRequestChangesPRCmd.Flags().
		StringVar(&prBodyFlag, "body", "", "Review comment, Markdown (required)")
	taskRequestChangesPRCmd.Flags().StringVar(&prBodyFileFlag, "body-file", "", bodyFileUsage)

	taskRequestReviewCmd.Flags().
		StringVar(&prFlag, "pr", "", "PR number (default: current branch)")

	taskCommentPRCmd.Flags().StringVar(&prFlag, "pr", "", "PR number (default: current branch)")
	taskCommentPRCmd.Flags().
		StringVar(&prBodyFlag, "body", "", "Comment body, Markdown (required)")
	taskCommentPRCmd.Flags().StringVar(&prBodyFileFlag, "body-file", "", bodyFileUsage)

	taskMergePRCmd.Flags().StringVar(&prFlag, "pr", "", "PR number (default: current branch)")
	taskMergePRCmd.Flags().
		StringVar(&prMethodFlag, "method", "squash", "Merge method: squash, merge, or rebase")

	taskPRViewCmd.Flags().StringVar(&prFlag, "pr", "", "PR number (default: current branch)")
	taskPRChecksCmd.Flags().StringVar(&prFlag, "pr", "", "PR number (default: current branch)")
	taskPRReviewTargetCmd.Flags().
		StringVar(&prFlag, "pr", "", "PR number (default: current branch)")
	taskPRReviewStateCmd.Flags().
		StringVar(&prFlag, "pr", "", "PR number (default: current branch)")
	taskPRStateCmd.Flags().
		StringVar(&prFlag, "pr", "", "PR number (default: current branch)")
}
