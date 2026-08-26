/*
 * Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cjairm/devgeta/internal/tooling/task"
	"github.com/spf13/cobra"
)

// standardHelpFunc restores standard Cobra help for a command subtree (used
// by both taskCmd and configCmd - see cmd/config.go). The root sets a
// branded help func (utils.PrompCustomHelp) that prints only Use+Long and is
// inherited by children — which hides subcommands and flags. Agents
// re-reading `dg task --help`/`dg config --help` (or a `<sub> --help`) need
// the full listing, so this renders the long/short description followed by
// the default usage block (Available Commands, Flags, Examples).
func standardHelpFunc(cmd *cobra.Command, args []string) {
	if cmd.Long != "" {
		cmd.Println(cmd.Long)
		cmd.Println()
	} else if cmd.Short != "" {
		cmd.Println(cmd.Short)
		cmd.Println()
	}
	cmd.Print(cmd.UsageString())
}

// taskRunner is the interface used by task subcommands, enabling injection in tests.
type taskRunner interface {
	RefreshBranch(target string, rebase bool) error
	ResetMainBranch() error
	ReinstallLibraries() error
	ReinstallLibrary(name string) error
	DeleteBranch(target string) error
	ReviewScope(bodies, sizes bool) (string, error)
	BranchDiff(file string) (string, error)
	ReviewPackage(base, head, file string) (string, error)
	ReviewNotes(branch, rev string, showPath, prune bool) (string, error)
	ReviewNoteOpen(branch, rev, cite, note string) (string, error)
	ReviewNoteSettle(branch, rev, id, resolution, cite, note string) (string, error)
	ReviewNoteRatify(branch, id string) (string, error)
	ReviewNoteReopen(branch, id string) (string, error)
	ReviewRun(reviewer, note string, rng task.ReviewRange) (string, error)
	WorktreeStart(name, base string) (string, error)
	WorktreeFinish(name string, merge, discard, check, force bool) (string, error)
	Release(version, messageFile string, push bool) (string, error)
	Scratch(key string) (string, error)
	ScratchClean(target string) error
	CommitTrailers(message string, require []string) (string, error)
	HandoffWrite(branch, note string) (string, error)
	HandoffRead(branch string) (string, error)
	HandoffClear(branch string) (string, error)
	ContextReport() (string, error)
}

// newTaskManager is the factory used by task subcommands; overridden in tests.
//
// The root --verbose flag is copied onto the manager here rather than read
// from the logger's level, because it steers progress output (review-run's
// per-tool-call stream) and not only logging. Reading it at call time — not at
// package init — is what makes it the flag's actual value: PersistentPreRunE
// has already parsed it by the time a subcommand's RunE calls this.
var newTaskManager = func() taskRunner {
	tm := task.New()
	tm.Verbose = verbose
	return tm
}

var taskCmd = &cobra.Command{
	Use:     "task",
	Aliases: []string{"t"},
	Short:   "Developer utilities (git, npm, GitHub PRs) callable by agents and humans",
	Long: `Developer utility commands callable by agents (Claude Code, CI, any
non-interactive process) and humans (via the dge() shell wrapper or directly).

Six families:
  - git branch:  refresh-branch, reset-main-branch, delete-branch
  - review scope: review-scope, branch-diff, review-package
  - worktree lifecycle: worktree-start, worktree-finish
  - release:     release
  - npm deps:    reinstall-libraries, reinstall-library
  - GitHub PRs:  review-threads, resolve/unresolve/reply-thread, submit-review,
                 create-pr, update-pr-description, approve-pr, request-changes-pr,
                 comment-pr, merge-pr, pr-view, pr-checks, pr-review-target,
                 pr-review-state, current-pr, current-repo

review-scope and PR data commands return compact, LLM-oriented output
(review-scope/branch-diff/review-package parse git plumbing; PR commands run
gh + jq). Run "dg task <subcommand> --help" for flags and examples.`,
	Example: `  dg task review-threads --state unresolved
  dg task pr-view
  dg task refresh-branch
  dg task reinstall-library lodash`,
}

// taskRefreshBranchRebaseFlag is refresh-branch's --rebase flag.
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

// taskBranchDiffFileFlag is branch-diff's --file flag.
var taskBranchDiffFileFlag string

// taskReviewPackageFileFlag is review-package's --file flag.
var taskReviewPackageFileFlag string

// taskReviewScopeBodiesFlag is review-scope's --bodies flag.
var (
	taskReviewScopeBodiesFlag bool
	taskReviewScopeSizesFlag  bool
)

var taskReviewScopeCmd = &cobra.Command{
	Use:   "review-scope",
	Short: "Fetch + orient in one call: branch, ahead/behind, commits, file stats (for agents)",
	Long: `Fetch origin (bounded, best-effort), resolve the default branch, and print a
compact orientation report: ahead/behind counts, commit lines (short SHA, ISO
date, subject), and a per-file stat table. Lockfile-style noise
(package-lock.json, go.sum, *.min.js, ...) is excluded from the table and
noted separately with its own stat counts, never silently dropped.

--bodies appends each commit's body as indented lines beneath its subject.

--sizes adds a "range size" line (unified-patch bytes for the whole
comparison, excluded paths left out) and a "commit sizes" line per commit
(that commit's own patch bytes, same exclusion). Use this to decide between
reviewing per-commit or as one range: costs one extra ` + "`git diff`" + ` per commit,
so it is opt-in — the default output is unchanged without it.

Run "dg task branch-diff" next to see the full (noise-filtered) diff, or
"dg task branch-diff --file <path>" to inspect one file, including an
otherwise-excluded one.`,
	Example: `  dg task review-scope
  dg task review-scope --bodies
  dg task review-scope --sizes
  dg task review-scope && dg task branch-diff`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newTaskManager().ReviewScope(taskReviewScopeBodiesFlag, taskReviewScopeSizesFlag)
		return emitPRResult(cmd, out, err)
	},
}

var taskBranchDiffCmd = &cobra.Command{
	Use:   "branch-diff",
	Short: "Show the merge-base diff against the default branch, noise excluded (for agents)",
	Long: `Diff the current branch against its merge-base with the default branch.
Lockfile-style noise (package-lock.json, go.sum, *.min.js, ...) is excluded by
default and noted separately with its own stat counts.

--file bypasses exclusions and returns just that file's diff, including an
otherwise-excluded file.

Does not fetch: run "dg task review-scope" first in the same review session,
since re-fetching per file pull could shift the comparison base mid-review.`,
	Example: `  dg task branch-diff
  dg task branch-diff --file internal/tooling/task/scope.go`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newTaskManager().BranchDiff(taskBranchDiffFileFlag)
		return emitPRResult(cmd, out, err)
	},
}

var taskReviewPackageCmd = &cobra.Command{
	Use:   "review-package <base> <head>",
	Short: "Verify a range, then print commits, noise-filtered stats, and the full diff (for agents)",
	Long: `Verify base and head both resolve to real commits, then print, in one call:
the base..head range, the commit list (short SHA, date, subject), a per-file
stat table, and the full -U10-context diff of the included files, fenced as
` + "```diff" + `.

Lockfile-style noise (package-lock.json, go.sum, *.min.js, ...) is excluded
from the stat table and diff, and noted separately with its own stat counts —
never silently dropped.

Unlike review-scope/branch-diff, base and head are not tied to the current
branch's default-branch merge-base: this is for reviewing an arbitrary
historical range or a PR that isn't checked out.

--file bypasses exclusions and returns just that file's -U10 diff, including
an otherwise-excluded file.`,
	Example: `  dg task review-package main feature-branch
  dg task review-package v1.2.0 v1.3.0
  dg task review-package main feature-branch --file internal/tooling/task/reviewpackage.go`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newTaskManager().ReviewPackage(args[0], args[1], taskReviewPackageFileFlag)
		return emitPRResult(cmd, out, err)
	},
}

// review-notes / review-note flags (ADR-0012's review journal).
var (
	taskReviewNotesBranchFlag string
	taskReviewNotesRevFlag    string
	taskReviewNotesPathFlag   bool
	taskReviewNotesPruneFlag  bool

	taskReviewNoteBranchFlag string
	taskReviewNoteRevFlag    string
	taskReviewNoteOpenFlag   bool
	taskReviewNoteSettleFlag bool
	taskReviewNoteRatifyFlag bool
	taskReviewNoteReopenFlag bool
	taskReviewNoteIDFlag     string
	taskReviewNoteAsFlag     string
	taskReviewNoteAtFlag     string
	taskReviewNoteNoteFlag   string
)

var taskReviewNotesCmd = &cobra.Command{
	Use:   "review-notes",
	Short: "Show this branch's remembered review exchanges, with staleness resolved (for agents)",
	Long: `Print the review journal for a branch: questions already answered, findings
already rejected with the author's reason, and findings already fixed — so a
re-review does not ask what has been asked and answered before.

Each entry is marked [fresh] or [STALE]. Staleness is decided by the content of
the cited file, not by a commit, so an uncommitted edit to that file correctly
invalidates the entry (a commit-based check cannot see one). A STALE entry is
still shown, never hidden: you learn both that it was settled and that the
answer may no longer hold.

Entries carry ids (n1, n2, ...). Pass an id to "review-note --settle" to record
the answer against the exact question it closes.

--branch targets another branch; omit it for the current one. A detached HEAD
has no branch and therefore no journal.
--rev decides staleness against that revision instead of the working tree. Pass
it when reviewing code that is not checked out (a pull request's head), giving
the revision under review RIGHT NOW: [STALE] then means "that revision changed
this file since the finding was written", not "your checkout differs from it".
--path prints the journal file's location, for hand-correcting a wrong entry.
--prune deletes journals whose branch no longer exists locally or on the remote.

The journal lives under the repo's common git directory, so it is never
committed and never appears in a diff, and it is deleted with the branch by
"dg wt remove".`,
	Example: `  dg task review-notes
  dg task review-notes --branch fix/retry-context
  dg task review-notes --path
  dg task review-notes --prune`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newTaskManager().ReviewNotes(
			taskReviewNotesBranchFlag,
			taskReviewNotesRevFlag,
			taskReviewNotesPathFlag,
			taskReviewNotesPruneFlag,
		)
		return emitPRResult(cmd, out, err)
	},
}

var taskReviewNoteCmd = &cobra.Command{
	Use:   "review-note (--open|--settle) --note <text> | (--ratify|--reopen) --id <id>",
	Short: "Record, settle, ratify, or reopen one review exchange (for agents and humans)",
	Long: `Write one entry to the branch's review journal. Exactly one of --open,
--settle, --ratify, or --reopen is required.

  --open --note "<text>" [--at <path[:line]>]
      Record a question or finding that is still awaiting an answer. Prints its
      new id ("Noted n4"); use that id to settle it later.

  --settle --id <id> --as answered|rejected|fixed --note "<answer>"
      Close an open entry. The cited path carries over from the open entry, so
      --at is refused here: a settle can never retarget the question it closes.

  --settle --as answered|rejected|fixed --note "<text>" [--at <path[:line]>]
      Record an exchange that was never open — asked and answered in one
      conversation — straight into the settled section.

  --ratify --id <id>
      A human accepts an agent's provisional rejection: when the review loop's
      coding agent decides a finding is wrong, it settles the entry "rejected"
      with a note prefixed to mark that the agent — not a human — made the
      call. --ratify strips that prefix, turning it into an ordinary rejection.
      Only a human should run this; it fails on anything that is not a
      rejected entry still carrying the agent's prefix, naming the actual
      state so you can see why.

  --reopen --id <id>
      A human refuses an agent's provisional rejection: the entry returns to
      open under the same id, with its original finding text intact and the
      settle note dropped, so the next review round asks it again rather than
      leaving it settled on the agent's say-so. Works on any settled entry,
      not only agent rejections.

--at is optional in both entry-creating forms: a design-level question ("should
this be an ADR?") cites no file and never goes stale mechanically. When --at is
given, the path must exist in the working tree — the command stamps the file's
content hash itself, and refuses rather than writing an entry that claims to
cite code but could never be checked against it.

--branch targets another branch; omit it for the current one.
--rev stamps the cited path at that revision instead of the working tree, and
the path must exist THERE. Pass it when reviewing code that is not checked out
(a pull request's head), where the cited file may be missing from your checkout
or hold unrelated content. It applies to --open and --settle only; --ratify and
--reopen do not restamp the entry at all.`,
	Example: `  dg task review-note --open --at store.go:12 --note "write is not atomic"
  dg task review-note --settle --id n4 --as fixed --note "atomic rename added"
  dg task review-note --settle --as answered --note "yes, ctx is threaded through"
  dg task review-note --open --branch pr/acme/api/213 --rev 9f2c1ab --at store.go:12 --note "write is not atomic"
  dg task review-note --ratify --id n7
  dg task review-note --reopen --id n7`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if taskReviewNoteIDFlag != "" &&
			!(taskReviewNoteSettleFlag || taskReviewNoteRatifyFlag || taskReviewNoteReopenFlag) {
			return fmt.Errorf("--id only applies to --settle, --ratify, or --reopen")
		}
		if (taskReviewNoteRatifyFlag || taskReviewNoteReopenFlag) && taskReviewNoteIDFlag == "" {
			return fmt.Errorf("--ratify and --reopen require --id")
		}
		// Ratify and reopen deliberately leave the blob/head stamp alone — one
		// confirms who an existing rejection belongs to, the other undoes a
		// settlement — so there is nothing for a revision to stamp against.
		// Accepting --rev there would silently do nothing.
		if taskReviewNoteRevFlag != "" &&
			(taskReviewNoteRatifyFlag || taskReviewNoteReopenFlag) {
			return fmt.Errorf(
				"--rev only applies to --open and --settle: --ratify and --reopen do not restamp the entry",
			)
		}
		tm := newTaskManager()
		switch {
		case taskReviewNoteOpenFlag:
			out, err := tm.ReviewNoteOpen(
				taskReviewNoteBranchFlag,
				taskReviewNoteRevFlag,
				taskReviewNoteAtFlag,
				taskReviewNoteNoteFlag,
			)
			return emitPRResult(cmd, out, err)
		case taskReviewNoteRatifyFlag:
			out, err := tm.ReviewNoteRatify(taskReviewNoteBranchFlag, taskReviewNoteIDFlag)
			return emitPRResult(cmd, out, err)
		case taskReviewNoteReopenFlag:
			out, err := tm.ReviewNoteReopen(taskReviewNoteBranchFlag, taskReviewNoteIDFlag)
			return emitPRResult(cmd, out, err)
		case taskReviewNoteSettleFlag:
			out, err := tm.ReviewNoteSettle(
				taskReviewNoteBranchFlag,
				taskReviewNoteRevFlag,
				taskReviewNoteIDFlag,
				taskReviewNoteAsFlag,
				taskReviewNoteAtFlag,
				taskReviewNoteNoteFlag,
			)
			return emitPRResult(cmd, out, err)
		default:
			// Cobra's MarkFlagsOneRequired already rejects this in production,
			// but RunE is also called directly by tests. Making --settle an
			// explicit case rather than the default means "no mode flag" can
			// never quietly settle an entry if that flag rule is ever relaxed.
			return fmt.Errorf("one of --open, --settle, --ratify, or --reopen is required")
		}
	},
}

// handoff flags (ADR-0032's per-branch handoff note).
var (
	taskHandoffBranchFlag   string
	taskHandoffWriteFlag    bool
	taskHandoffReadFlag     bool
	taskHandoffClearFlag    bool
	taskHandoffNoteFlag     string
	taskHandoffNoteFileFlag string
)

var taskHandoffCmd = &cobra.Command{
	Use:   "handoff --write (--note <text>|--note-file <path>) | --read | --clear",
	Short: "Write, read, or clear this branch's session handoff note (for agents and humans)",
	Long: `Write a durable checkpoint for the branch you are working on, so a fresh
session can read what came before instead of growing one session forever.
Exactly one of --write, --read, or --clear is required.

--write replaces the note (never appends) and stamps the current time and
HEAD. Give the text with --note, or --note-file for anything multi-line.
Nothing calls a model here — write whatever text you are given; summarizing
before you call this is a skill's job, not this command's.

--read prints the note. A branch with no note prints a clean message and
exits 0 — that is the normal state for a fresh branch, not an error. When
HEAD has moved since the note was written, the output says so.

--clear deletes the note. Clearing a branch with no note is also success.

--branch targets another branch; omit it for the current one. A detached
HEAD has no branch and therefore no note.

The note lives under the repo's common git directory, so it is never
committed and never appears in a diff.`,
	Example: `  dg task handoff --write --note "left off wiring the OAuth callback"
  dg task handoff --write --note-file handoff.md
  dg task handoff --read
  dg task handoff --clear`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		switch {
		case taskHandoffWriteFlag:
			note := taskHandoffNoteFlag
			if taskHandoffNoteFileFlag != "" {
				data, err := os.ReadFile(taskHandoffNoteFileFlag)
				if err != nil {
					return fmt.Errorf(
						"failed to read --note-file %q: %w", taskHandoffNoteFileFlag, err,
					)
				}
				note = string(data)
			}
			if strings.TrimSpace(note) == "" {
				return fmt.Errorf("--write requires --note or --note-file")
			}
			out, err := newTaskManager().HandoffWrite(taskHandoffBranchFlag, note)
			return emitPRResult(cmd, out, err)
		case taskHandoffReadFlag:
			out, err := newTaskManager().HandoffRead(taskHandoffBranchFlag)
			return emitPRResult(cmd, out, err)
		case taskHandoffClearFlag:
			out, err := newTaskManager().HandoffClear(taskHandoffBranchFlag)
			return emitPRResult(cmd, out, err)
		default:
			// Cobra's MarkFlagsOneRequired already rejects this in production,
			// but RunE is also called directly by tests (see review-note above).
			return fmt.Errorf("one of --write, --read, or --clear is required")
		}
	},
}

var taskContextReportCmd = &cobra.Command{
	Use:   "context-report",
	Short: "Report what loads into a Claude Code or OpenCode session before the first prompt (for agents and humans)",
	Long: `Measures the base-context cost of the current repo: CLAUDE.md/AGENTS.md
files (with @imports followed transitively for Claude), auto-memory,
settings, skills, commands, agents, plugins, MCP, and hooks — reported per
agent, since the two load different trees and a combined number would
describe neither.

Read-only; makes no network calls. The token figure is a character-based
estimate (bytes / 4), not a real tokenizer — treat it as a rough order of
magnitude, not an exact count.

Run this before deciding what to trim from CLAUDE.md or a skill: it turns
"this file feels big" into an actual number.`,
	Example: `  dg task context-report`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newTaskManager().ContextReport()
		return emitPRResult(cmd, out, err)
	},
}

// taskReviewRunReviewerFlag/NoteFlag are review-run's flags, and
// taskReviewRunBase/Head/Journal/ReportDirFlag are its explicit-range mode's
// group — all four together, or none of them (ADR-0023 §5).
var (
	taskReviewRunReviewerFlag  string
	taskReviewRunNoteFlag      string
	taskReviewRunBaseFlag      string
	taskReviewRunHeadFlag      string
	taskReviewRunJournalFlag   string
	taskReviewRunReportDirFlag string
)

var taskReviewRunCmd = &cobra.Command{
	Use: "review-run [--reviewer code|document|skill] [--note <text>] " +
		"[--base <sha> --head <sha> --journal <key> --report-dir <dir>]",
	Short: "Run one round of headless AI review and print each reviewer's verdict (for agents)",
	Long: `Run every reviewer model configured in "review.reviewers" against the current
branch, one after another, headless, and print what each concluded.

One invocation is one round. Rounds are not repeated here — the caller decides
whether another round is worth it.

--reviewer picks which reviewer agent runs (the same choices the "dg ws" R
keybinding offers): code (default), document, or skill. Every configured model
runs that same reviewer; with "review.reviewers" unset, one run uses OpenCode's
default model. A model listed more than once runs once, in the position of its
first entry.

--note passes your own words to every reviewer this round, on top of the fixed
review prompt — e.g. --note "focus on docs/spec.md, I only changed the wording
there". It adds emphasis, it does not narrow the review: reviewers are told
exactly that, and still review the whole branch. A blank --note is refused
rather than dropped.

The branch under review is everything the branch would merge: its commits AND
whatever is still uncommitted in the working tree, including untracked files.
So work in progress can be reviewed without committing it first.

Refuses to run on the default branch or a detached HEAD — both before any
reviewer starts — because a review compares a branch against the default one,
and a review journal is keyed by branch name. Also refuses a branch that
changes nothing at all: no commits ahead of the default branch AND a clean
working tree.

--base, --head, --journal, and --report-dir switch to explicit-range mode:
review those two commits instead of the checkout. All four are required
together, and passing some without the others is an error rather than a branch
review of unrelated code. Any commit-ish is accepted and resolved to a sha
before a reviewer starts; a commit this clone does not have is refused there
and then. --base must be the MERGE BASE when the range has to equal a pull
request's diff ("dg task pr-review-target" prints one) — a base branch tip
makes everything merged since look reverted.

In that mode nothing depends on the checkout: none of the three refusals above
apply, any branch (including the default one) or a detached HEAD is fine, and
the review covers the two commits ONLY — the working tree is never part of the
diff, unlike a branch review. --journal keys the review journal explicitly
(e.g. pr/owner/repo/213), so findings are not filed under whatever branch
happens to be checked out. Each reviewer's full report — every severity,
strengths, evidence — is written to <report-dir>/<agent>+<model>.md, where
<agent> is the resolved reviewer agent's name and <model> is percent-encoded so
a provider/model id cannot become a path. Each output line names the file:

  openai/gpt-5.2 → REQUEST CHANGES  report: /tmp/r/code-reviewer+openai%2Fgpt-5.2.md

Each reviewer reads the journal as it stood when the round began, so no
reviewer sees what another raised or settled in the same round. Their entries
still go to the live journal immediately and keep their real ids.

Output is exactly one line per reviewer:

  openai/gpt-5.2 → REQUEST CHANGES
  google/gemini-3-pro → APPROVE

The findings themselves are not printed here — they are in the journal; read
them with "dg task review-notes", which also lists what is still open.

An outcome is APPROVE, REQUEST CHANGES, NEEDS DISCUSSION, NO VERDICT (the run
finished but stated no verdict), NO VERDICT(<reason>) (the run wrote no report
at all, and the reason is why), or ERROR(<reason>) (the run itself failed).
NO VERDICT and ERROR are never approval. One reviewer failing does not stop
the others.

While a reviewer runs, progress goes to stderr as it happens — a line when it
starts, a heartbeat at most every 30 seconds naming the tool call it is on and
the running counters, and a closing line with the outcome, elapsed time, and
cost. Pass the root --verbose flag to see every tool call instead. stdout stays
exactly the contract above either way.`,
	Example: `  dg task review-run
  dg task review-run --reviewer document
  dg task review-run --note "focus on the retry path in internal/http"
  dg task review-run --base 4a1c2ef --head 9f2c1ab --journal pr/acme/web/213 --report-dir "$SCRATCH"`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newTaskManager().ReviewRun(
			taskReviewRunReviewerFlag,
			taskReviewRunNoteFlag,
			task.ReviewRange{
				Base:      taskReviewRunBaseFlag,
				Head:      taskReviewRunHeadFlag,
				Journal:   taskReviewRunJournalFlag,
				ReportDir: taskReviewRunReportDirFlag,
			},
		)
		return emitPRResult(cmd, out, err)
	},
}

// taskWorktreeStartBaseFlag is worktree-start's --base flag.
var taskWorktreeStartBaseFlag string

// taskWorktreeFinishMergeFlag/DiscardFlag/CheckFlag/ForceFlag are
// worktree-finish's flags.
var (
	taskWorktreeFinishMergeFlag   bool
	taskWorktreeFinishDiscardFlag bool
	taskWorktreeFinishCheckFlag   bool
	taskWorktreeFinishForceFlag   bool
)

var taskWorktreeStartCmd = &cobra.Command{
	Use:   "worktree-start <name> [--base <ref>]",
	Short: "Create a git worktree + branch in dg wt's shared location (for agents)",
	Long: `Refuse to run from a dirty tree, fetch origin, then create a new git worktree
with a new branch, in the same location "dg wt" uses
(~/.local/share/devgeta/worktrees/<repo-slug>/<flat-name>) — so "dg wt list" sees
it and vice versa.

--base sets the branch's starting point explicitly (any ref: a branch, tag, or
SHA). Without --base, the new branch is based on the repo's freshly-fetched
default branch (or an existing local/remote branch of the same name, if one
already exists).`,
	Example: `  dg task worktree-start add-retry-logic
  dg task worktree-start hotfix-123 --base origin/release-2.0`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := newTaskManager().WorktreeStart(args[0], taskWorktreeStartBaseFlag)
		return emitPRResult(cmd, out, err)
	},
}

var taskWorktreeFinishCmd = &cobra.Command{
	Use:   "worktree-finish [name] --merge|--discard|--check",
	Short: "Tear down a git worktree via merge or discard (for agents)",
	Long: `Tear down a worktree created by "worktree-start" (or "dg wt"). Exactly one of
--merge, --discard, or --check is required.

Target resolution is deterministic: an explicit name wins; otherwise the
current directory resolves to the linked worktree it's inside; otherwise the
command errors and lists the worktrees it found — it never guesses from a
main checkout.

--merge rebases the worktree's branch onto the default branch if it has
diverged, fast-forward-merges it into the default branch from the main
checkout, then removes the worktree and deletes the branch (safe only once
the fast-forward has landed the branch's commits on the default branch).

--discard refuses on a dirty worktree unless --force is passed, then removes
the worktree and deletes the branch unconditionally. This does not run a
build or test suite — verification is the caller's responsibility.

--check reports readiness without acting: it never mutates anything, never
moves a ref, and never fetches. It prints the same shape of information
--merge would use to decide (dirty state, rebase need, predicted merge
conflicts, open review-journal findings, changed docs' status markers) and
exits non-zero when the report's own "ready:" line says no.`,
	Example: `  dg task worktree-finish add-retry-logic --merge
  dg task worktree-finish --discard          # resolves from the current directory
  dg task worktree-finish stale-spike --discard --force
  dg task worktree-finish add-retry-logic --check`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		out, err := newTaskManager().WorktreeFinish(
			name, taskWorktreeFinishMergeFlag, taskWorktreeFinishDiscardFlag,
			taskWorktreeFinishCheckFlag, taskWorktreeFinishForceFlag,
		)
		var notReady *task.CheckNotReadyError
		if errors.As(err, &notReady) {
			fmt.Fprintln(cmd.OutOrStdout(), notReady.Report)
			return notReady
		}
		return emitPRResult(cmd, out, err)
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
		return emitPRResult(cmd, out, err)
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
		return emitPRResult(cmd, out, err)
	},
}

// taskCommitTrailersMessageFileFlag / taskCommitTrailersRequireFlags are
// commit-trailers' flags.
var (
	taskCommitTrailersMessageFileFlag string
	taskCommitTrailersRequireFlags    []string
)

var taskCommitTrailersCmd = &cobra.Command{
	Use:   "commit-trailers",
	Short: "Parse a commit message's trailers, and optionally require some (for agents)",
	Long: `Parse a candidate commit message's trailer block via
"git interpret-trailers --parse" and print the trailers it recognized.

The message comes from --message-file, or stdin when that flag is omitted —
never an implicit "current commit". Validate a message BEFORE it becomes a
commit.

--require <key> (repeatable) exits non-zero when key is not among the
trailers git itself parsed, case-insensitively. This command hardcodes no
trailer name — which trailers a commit needs is a per-repository decision,
so it is always an argument, never a default.

This does NOT scan for "a trailer-looking line that failed to parse": that
is undecidable from syntax alone (a line like "Reason: needed" followed by
more prose is not a recognized trailer block, and a scanner would reject an
ordinary commit message over it). --require only ever checks whether a key
you named is among what git actually parsed — which still catches a
required trailer glued to the body with no blank-line separator, since git
parses nothing for that shape.`,
	Example: `  dg task commit-trailers --message-file msg.txt
  dg task commit-trailers --message-file msg.txt --require Co-authored-by
  git log -1 --format=%B | dg task commit-trailers --require Signed-off-by`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		var data []byte
		var err error
		if taskCommitTrailersMessageFileFlag != "" {
			data, err = os.ReadFile(taskCommitTrailersMessageFileFlag)
			if err != nil {
				return fmt.Errorf(
					"failed to read --message-file %q: %w", taskCommitTrailersMessageFileFlag, err,
				)
			}
		} else {
			data, err = io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("failed to read stdin: %w", err)
			}
		}
		out, err := newTaskManager().CommitTrailers(string(data), taskCommitTrailersRequireFlags)
		return emitPRResult(cmd, out, err)
	},
}

func init() {
	rootCmd.AddCommand(taskCmd)
	// Standard Cobra help for the whole task subtree (overrides the branded
	// root help func, which children would otherwise inherit and which hides
	// subcommands/flags). Children inherit this from taskCmd.
	taskCmd.SetHelpFunc(standardHelpFunc)
	taskCmd.AddCommand(taskRefreshBranchCmd)
	taskCmd.AddCommand(taskResetMainBranchCmd)
	taskCmd.AddCommand(taskDeleteBranchCmd)
	taskCmd.AddCommand(taskReinstallLibrariesCmd)
	taskCmd.AddCommand(taskReinstallLibraryCmd)
	taskCmd.AddCommand(taskReviewScopeCmd)
	taskCmd.AddCommand(taskBranchDiffCmd)
	taskCmd.AddCommand(taskReviewPackageCmd)
	taskCmd.AddCommand(taskReviewNotesCmd)
	taskCmd.AddCommand(taskReviewNoteCmd)
	taskCmd.AddCommand(taskReviewRunCmd)
	taskCmd.AddCommand(taskWorktreeStartCmd)
	taskCmd.AddCommand(taskWorktreeFinishCmd)
	taskCmd.AddCommand(taskReleaseCmd)
	taskCmd.AddCommand(taskScratchCmd)
	taskCmd.AddCommand(taskCommitTrailersCmd)
	taskCmd.AddCommand(taskHandoffCmd)
	taskCmd.AddCommand(taskContextReportCmd)

	taskScratchCmd.Flags().
		StringVar(&taskScratchCleanFlag, "clean", "", "Remove a directory scratch previously allocated")
	taskScratchCmd.Flags().
		StringVar(&taskScratchKeyFlag, "key", "", "Allocate or re-derive a shared, keyed scratch directory instead of a unique one")

	taskCommitTrailersCmd.Flags().
		StringVar(&taskCommitTrailersMessageFileFlag, "message-file", "",
			"Read the candidate commit message from this file (default: stdin)")
	taskCommitTrailersCmd.Flags().
		StringArrayVar(&taskCommitTrailersRequireFlags, "require", nil,
			"Fail unless this trailer key is among what git parsed (repeatable, case-insensitive)")

	taskRefreshBranchCmd.Flags().
		BoolVar(&taskRefreshBranchRebaseFlag, "rebase", false, "Rebase onto target instead of merging it in (default stays merge)")

	taskBranchDiffCmd.Flags().
		StringVar(&taskBranchDiffFileFlag, "file", "", "Diff only this file, bypassing exclusions")
	taskReviewPackageCmd.Flags().
		StringVar(&taskReviewPackageFileFlag, "file", "", "Diff only this file, bypassing exclusions")
	taskReviewScopeCmd.Flags().
		BoolVar(&taskReviewScopeBodiesFlag, "bodies", false, "Append each commit's body beneath its subject")
	taskReviewScopeCmd.Flags().
		BoolVar(&taskReviewScopeSizesFlag, "sizes", false, "Add range and per-commit diff-byte sizes")

	taskReviewNotesCmd.Flags().
		StringVar(&taskReviewNotesBranchFlag, "branch", "", "Branch to read (default: current)")
	taskReviewNotesCmd.Flags().
		BoolVar(&taskReviewNotesPathFlag, "path", false, "Print the journal file's location instead of its contents")
	taskReviewNotesCmd.Flags().
		StringVar(&taskReviewNotesRevFlag, "rev", "", "Resolve staleness against this revision instead of the working tree (for reviewing code that is not checked out)")
	taskReviewNotesCmd.Flags().
		BoolVar(&taskReviewNotesPruneFlag, "prune", false, "Delete journals whose branch no longer exists locally or on the remote")
	taskReviewNotesCmd.MarkFlagsMutuallyExclusive("path", "prune")
	// --rev only steers freshness, which neither --path (prints a filename) nor
	// --prune (deletes journals) computes. Refusing the combination beats
	// accepting a flag and ignoring it.
	taskReviewNotesCmd.MarkFlagsMutuallyExclusive("rev", "path")
	taskReviewNotesCmd.MarkFlagsMutuallyExclusive("rev", "prune")

	taskReviewNoteCmd.Flags().
		StringVar(&taskReviewNoteBranchFlag, "branch", "", "Branch to write to (default: current)")
	taskReviewNoteCmd.Flags().
		StringVar(&taskReviewNoteRevFlag, "rev", "", "Stamp the cited path at this revision instead of the working tree (for reviewing code that is not checked out)")
	taskReviewNoteCmd.Flags().
		BoolVar(&taskReviewNoteOpenFlag, "open", false, "Record an entry that is still awaiting an answer")
	// --settle is a bool with a separate --id rather than "--settle [id]" with an
	// optional value: pflag's NoOptDefVal would make the natural `--settle n4`
	// parse as a bare flag plus a stray positional, which cobra.NoArgs then
	// rejects with an error naming the id instead of the mistake.
	taskReviewNoteCmd.Flags().
		BoolVar(&taskReviewNoteSettleFlag, "settle", false, "Settle an entry (with --id closes that open entry; without it records an exchange that was never open)")
	taskReviewNoteCmd.Flags().
		BoolVar(&taskReviewNoteRatifyFlag, "ratify", false, "Human-only: accept an agent's provisional rejection, stripping its provenance (requires --id)")
	taskReviewNoteCmd.Flags().
		BoolVar(&taskReviewNoteReopenFlag, "reopen", false, "Human-only: return a settled entry to open under the same id (requires --id)")
	taskReviewNoteCmd.Flags().
		StringVar(&taskReviewNoteIDFlag, "id", "", "Entry id to act on, as printed by --open (e.g. n4); required by --ratify and --reopen")
	taskReviewNoteCmd.Flags().
		StringVar(&taskReviewNoteAsFlag, "as", "", "Resolution for --settle: rejected, answered, or fixed")
	taskReviewNoteCmd.Flags().
		StringVar(&taskReviewNoteAtFlag, "at", "", "Cited location, path[:line] (optional; a design-level entry cites nothing)")
	taskReviewNoteCmd.Flags().
		StringVar(&taskReviewNoteNoteFlag, "note", "", "The question, finding, or answer text (required for --open and --settle)")
	taskReviewNoteCmd.MarkFlagsMutuallyExclusive("open", "settle", "ratify", "reopen")
	taskReviewNoteCmd.MarkFlagsOneRequired("open", "settle", "ratify", "reopen")

	taskHandoffCmd.Flags().
		StringVar(&taskHandoffBranchFlag, "branch", "", "Branch the note belongs to (default: current)")
	taskHandoffCmd.Flags().
		BoolVar(&taskHandoffWriteFlag, "write", false, "Replace the note for this branch")
	taskHandoffCmd.Flags().
		BoolVar(&taskHandoffReadFlag, "read", false, "Print the note for this branch")
	taskHandoffCmd.Flags().
		BoolVar(&taskHandoffClearFlag, "clear", false, "Delete the note for this branch")
	taskHandoffCmd.Flags().
		StringVar(&taskHandoffNoteFlag, "note", "", "Note text (with --write; use --note-file for multi-line)")
	taskHandoffCmd.Flags().
		StringVar(&taskHandoffNoteFileFlag, "note-file", "", "Read the note text from this file instead of --note")
	taskHandoffCmd.MarkFlagsMutuallyExclusive("write", "read", "clear")
	taskHandoffCmd.MarkFlagsOneRequired("write", "read", "clear")
	taskHandoffCmd.MarkFlagsMutuallyExclusive("note", "note-file")

	taskReviewRunCmd.Flags().
		StringVar(&taskReviewRunReviewerFlag, "reviewer", task.DefaultReviewerKey, "Reviewer agent to run: code, document, or skill")
	taskReviewRunCmd.Flags().
		StringVar(&taskReviewRunNoteFlag, "note", "", "Extra context for every reviewer this round (adds emphasis; does not narrow the review)")
	taskReviewRunCmd.Flags().
		StringVar(&taskReviewRunBaseFlag, "base", "", "Review this commit..--head instead of the checkout (the PR's merge base, not the base branch tip)")
	taskReviewRunCmd.Flags().
		StringVar(&taskReviewRunHeadFlag, "head", "", "Head commit of the reviewed range (with --base)")
	taskReviewRunCmd.Flags().
		StringVar(&taskReviewRunJournalFlag, "journal", "", "Review journal key to read and write, instead of the current branch name (e.g. pr/owner/repo/213)")
	taskReviewRunCmd.Flags().
		StringVar(&taskReviewRunReportDirFlag, "report-dir", "", "Directory each reviewer's full report is written to")
	// The four are one mode, so cobra refuses a partial group here as well as
	// ReviewRun refusing it in Go. Both are needed: this catches it at parse
	// time with cobra's own wording, and ReviewRun's check is what protects
	// every other caller — including RunE called directly by tests — from a
	// range review silently degrading into a branch review of unrelated code.
	taskReviewRunCmd.MarkFlagsRequiredTogether("base", "head", "journal", "report-dir")

	taskWorktreeStartCmd.Flags().
		StringVar(&taskWorktreeStartBaseFlag, "base", "", "Starting ref for the new branch (default: repo default branch)")

	taskWorktreeFinishCmd.Flags().
		BoolVar(&taskWorktreeFinishMergeFlag, "merge", false, "Fast-forward-merge the branch into default, then remove")
	taskWorktreeFinishCmd.Flags().
		BoolVar(&taskWorktreeFinishDiscardFlag, "discard", false, "Remove the worktree and branch without merging")
	taskWorktreeFinishCmd.Flags().
		BoolVar(&taskWorktreeFinishForceFlag, "force", false, "With --discard, remove even if the worktree has uncommitted changes")
	taskWorktreeFinishCmd.Flags().
		BoolVar(&taskWorktreeFinishCheckFlag, "check", false, "Read-only readiness report; non-zero exit when not ready")
	taskWorktreeFinishCmd.MarkFlagsMutuallyExclusive("merge", "discard", "check")
	taskWorktreeFinishCmd.MarkFlagsOneRequired("merge", "discard", "check")

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
