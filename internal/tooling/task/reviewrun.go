// The review-run task: one round of headless AI review (ADR-0017).
//
// One invocation is exactly one round. The round cap (review.rounds) belongs
// to the agent-side loop command that calls this, not here — this command
// runs every configured reviewer once, sequentially, and reports what each
// one concluded.
//
// Two things are deliberately not decided here: whether a verdict is good
// enough to stop, and whether two findings are the same finding. Both are
// judgment, which ADR-0017 §5 keeps out of Go.

package task

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cjairm/devgeta/internal/apps/opencode"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/reviewjournal"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/logger"
)

// DefaultReviewerKey is the reviewer --reviewer selects when it is not
// passed: the code reviewer, the common case. The key is validated against
// worktree.BuiltinReviewerChoices() like any other, so it cannot drift from
// the registry. Exported so cmd/task.go can use it as the flag's own
// default instead of restating the literal "code" — a change to this
// constant would otherwise leave the flag silently offering the old value.
const DefaultReviewerKey = "code"

// defaultModelLabel labels the reviewer line when review.reviewers is unset.
// That case runs one reviewer with no -m flag at all, so there is no model
// name to print — naming the condition is more useful to the reader than an
// empty column.
const defaultModelLabel = "OpenCode default model"

// The five outcomes a reviewer run can end in. The first three are parsed
// from the reviewer's own `**Status:**` line (the reviewer agents' reporting
// contract, configs/shared/agents/*-reviewer.md); NO VERDICT and ERROR are
// this command's own, and neither ever counts as approval.
const (
	verdictApprove         = "APPROVE"
	verdictRequestChanges  = "REQUEST CHANGES"
	verdictNeedsDiscussion = "NEEDS DISCUSSION"
	outcomeNoVerdict       = "NO VERDICT"
)

// knownVerdicts is checked longest-first so "NEEDS DISCUSSION" is never
// matched as some shorter prefix of itself.
var knownVerdicts = []string{verdictNeedsDiscussion, verdictRequestChanges, verdictApprove}

// reviewRunTimeout bounds a single reviewer run. A headless review reads the
// whole branch diff and writes journal entries, so this is generous by
// design — the point of the bound is that a wedged provider connection
// eventually reports ERROR instead of hanging the round forever.
const reviewRunTimeout = 30 * time.Minute

// maxReasonLen caps an ERROR(...) reason so one reviewer stays one line.
// The reason is OpenCode's own text, cut — never reworded or guessed at.
const maxReasonLen = 120

// statusLinePattern matches ONE reviewer verdict line, e.g. "Status: REQUEST
// CHANGES". It is applied to a line AFTER stripLineEmphasis has removed the
// line's markdown emphasis markers, which is why it names no "**" at all — a
// real run (github-copilot/gpt-5.3-codex, under document-reviewer) wrote
// "**Status: REQUEST CHANGES**", wrapping the whole line in emphasis rather
// than just "Status:" the way the agent template does, and the old pattern
// (which hard-coded "\*\*status:\*\*") never matched it, silently reporting a
// genuine REQUEST CHANGES as NO VERDICT. Case-insensitive because the marker
// is prose the model reproduces. It is applied to a single line at a time by
// lastStatusVerdict — which is why it carries no (?m) flag: there is no
// multi-line text for ^ and $ to anchor within, and the horizontal-only
// whitespace classes ([^\S\n]) keep it that way even if a caller ever passed
// a fragment containing a newline.
var statusLinePattern = regexp.MustCompile(`(?i)^[^\S\n]*status:[^\S\n]*(.*)$`)

// reviewerRun is one reviewer this round runs: how its line is labeled, and
// which model to pin it to ("" = no -m flag, i.e. OpenCode's own default).
type reviewerRun struct {
	label string
	model string
}

// ReviewRange is review-run's explicit-range mode (ADR-0023 §5): review an
// arbitrary pair of commits under an explicit journal key, and persist each
// reviewer's full report, instead of reviewing whatever is checked out.
//
// The four fields are one group — all set, or none. The zero value is branch
// mode, which is byte-identical to what review-run did before this mode
// existed. That is deliberate: a partially filled group means the caller
// intended a range review and got a branch review of unrelated code, which is
// exactly the silent wrong-target failure ADR-0023 exists to prevent, so
// mode() refuses it instead.
type ReviewRange struct {
	// Base and Head are the ends of the reviewed range. Any commit-ish is
	// accepted and resolved to an immutable SHA before a reviewer launches;
	// the caller is responsible for Base being a MERGE BASE when the range
	// must equal a pull request's diff (ADR-0023 §2 — a base branch tip makes
	// everything merged since look reverted).
	Base string
	Head string
	// Journal is the key the round's findings are read from and written under
	// (e.g. `pr/owner/repo/213`). It is a plain string key, not a git branch:
	// the journal has been addressable by an explicit key since `review-note
	// --branch` landed, and ADR-0023 §3 is what a PR's key looks like.
	Journal string
	// ReportDir is where each run's full report is persisted. Without it the
	// reports die with the headless runs — stdout carries verdict lines only,
	// and the journal carries one-line blocking findings, so nothing
	// downstream could compose a cohesive review out of them.
	ReportDir string
}

// normalized trims every field once, up front. A key or path that arrives with
// stray whitespace — a shell variable that expanded with a trailing newline is
// the realistic case — must key the same journal and name the same file as the
// value the caller meant, not a near-duplicate of it.
func (r ReviewRange) normalized() ReviewRange {
	return ReviewRange{
		Base:      strings.TrimSpace(r.Base),
		Head:      strings.TrimSpace(r.Head),
		Journal:   strings.TrimSpace(r.Journal),
		ReportDir: strings.TrimSpace(r.ReportDir),
	}
}

// mode reports whether this round is an explicit range, and refuses a
// partially filled flag group by naming exactly what is missing.
//
// It is called on a normalized value, so a flag whose value is only whitespace
// counts as absent: a shell variable that expanded to nothing is a caller who
// meant to supply a key and did not, which must not silently become a branch
// review.
func (r ReviewRange) mode() (bool, error) {
	fields := []struct{ flag, value string }{
		{"--base", r.Base},
		{"--head", r.Head},
		{"--journal", r.Journal},
		{"--report-dir", r.ReportDir},
	}
	given := make([]string, 0, len(fields))
	missing := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.value == "" {
			missing = append(missing, f.flag)
			continue
		}
		given = append(given, f.flag)
	}
	switch {
	case len(given) == 0:
		return false, nil
	case len(missing) == 0:
		return true, nil
	}
	return false, fmt.Errorf(
		"review-run: %s given without %s — reviewing an explicit range needs all four of "+
			"--base, --head, --journal, and --report-dir. Pass the missing flag(s), or drop "+
			"%s to review the current branch instead",
		strings.Join(given, ", "),
		strings.Join(missing, ", "),
		strings.Join(given, ", "),
	)
}

// ReviewRun runs one review round: every configured reviewer model, in
// order, through the OpenCode wrapper, against the current branch — then
// returns one line per reviewer, and nothing else.
//
// note, when non-empty, is the human's own steering for this round (`--note`).
// It is appended to the fixed review prompt as extra context, and deliberately
// does not narrow the review — see reviewNoteHeader. It rides the prompt in
// both modes, unchanged by which one is running.
//
// rng, when set, switches the round to explicit-range mode (ADR-0023 §5): an
// arbitrary base..head pair is reviewed instead of the checkout, the journal is
// keyed explicitly instead of by branch name, and each run's full report is
// persisted. Its zero value is branch mode, unchanged. The two modes differ in
// exactly three places, each pinned by its own test:
//
//   - Range mode skips all three HEAD-dependent refusals, and the
//     did-HEAD-move re-check between reviewers. Every one of them guards an
//     inference about the checkout that range mode supplies outright, and a
//     range review does not depend on the checkout at all — see
//     reviewRoundTarget and the loop below.
//   - Range mode reviews the immutable SHAs only; branch mode covers the
//     branch's whole working state, uncommitted work included (ADR-0019). The
//     prompt states which, because the scoping happens inside the reviewer's
//     own `review-package` call, not here.
//   - Range mode persists each report and names the file on the run's output
//     line. Branch mode's line stays exactly "<label> → <outcome>".
//
// Reviewers are isolated on the read side only (ADR-0017 §4): a snapshot of
// the journal as it stood at round start is written first, and each reviewer
// is pointed at it, so no reviewer sees what a peer opened or settled during
// this same round. Their writes go straight to the live journal and get
// real, final ids.
//
// While a reviewer runs, progress goes to progressWriter() (stderr by
// default) as it happens — a start line, a sampled heartbeat while it works
// (every tool call instead, under TaskManager.Verbose), and a closing line
// with the outcome — never to the returned string, which stays the exact
// parseable contract docs/guides/task-design.md governs. A multi-minute
// headless run against a real branch diff would otherwise leave the caller
// watching silence with no way to tell working from stuck.
func (tm *TaskManager) ReviewRun(reviewer, note string, rng ReviewRange) (string, error) {
	// Cheapest guards first: a bad --reviewer, an incomplete range flag group,
	// or a blank --note needs no git and no config.
	agent, err := reviewerAgentFor(reviewer)
	if err != nil {
		return "", err
	}
	rng = rng.normalized()
	rangeMode, err := rng.mode()
	if err != nil {
		return "", err
	}
	noteSuffix, err := reviewNoteSuffix(note)
	if err != nil {
		return "", err
	}

	// Everything the round needs to know about its target — including all
	// three refusals in branch mode — is settled here, before any reviewer is
	// launched and before any file is written. A wrong branch, a branch with
	// nothing to review, a sha this clone does not have, or a report directory
	// that cannot be created must all cost nothing.
	basePrompt, journalKey, branch, err := tm.reviewRoundTarget(rng, rangeMode)
	if err != nil {
		return "", err
	}
	prompt := basePrompt + noteSuffix

	runs, err := resolveReviewerRuns()
	if err != nil {
		return "", err
	}

	jm := reviewjournal.New(tm.Git)
	snapshot, err := jm.WriteSnapshot("", journalKey)
	if err != nil {
		return "", fmt.Errorf("review-run: %w", err)
	}
	// The snapshot must not outlive the round on ANY path — including the
	// ones where a reviewer or the journal read below fails.
	defer func() {
		if rmErr := os.Remove(snapshot); rmErr != nil && !os.IsNotExist(rmErr) {
			// Reported, not returned: the round's verdicts are already
			// earned, and the next round rewrites this file before anything
			// reads it, so a leftover cannot silently freeze a later round.
			logger.L().Errorw(
				"failed to remove the round-start review snapshot",
				"path", snapshot, "error", rmErr,
			)
		}
	}()

	progressOut := tm.progressWriter()
	lines := make([]string, 0, len(runs))
	for i, run := range runs {
		progress := newReviewerProgress(
			progressOut,
			fmt.Sprintf("[%d/%d]", i+1, len(runs)),
			run.label,
			tm.Verbose,
			tm.now,
		)
		progress.started()

		// A reviewer that fails never aborts the ones after it: each is an
		// independent opinion, and losing the rest to one bad provider would
		// throw away work already paid for.
		outcome, report := tm.runReviewerWithRetry(opencode.RunOptions{
			Agent:        agent,
			Model:        run.model,
			Prompt:       prompt,
			Timeout:      reviewRunTimeout,
			Env:          []string{ReviewJournalSnapshotEnvVar + "=" + snapshot},
			OnStdoutLine: progress.line,
		}, progress)
		progress.finished(outcome)

		// The branch is a precondition of the whole round, not just of its
		// first moment, so it is re-checked after every reviewer — in branch
		// mode. In range mode nothing about the round is derived from HEAD:
		// the reviewed commits and the journal key were both stated by the
		// caller, so a checkout that moves mid-round changes nothing this
		// command asserts, and abandoning a paid round over it would be a
		// defect in a mode built to run unattended while a human works.
		if !rangeMode {
			if err := tm.checkBranchStillCheckedOut(branch, run.label); err != nil {
				return "", err
			}
		}

		line := fmt.Sprintf("%s → %s", run.label, outcome)
		if rangeMode {
			where, err := writeReviewerReport(rng.ReportDir, agent, run.label, report)
			if err != nil {
				return "", err
			}
			line += reportField + where
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

// reviewerAttempts is how many times one reviewer is launched before its
// outcome is reported: the run, plus one retry.
//
// Only ONE retry, and only when the attempt produced no report at all. A
// reviewer that wrote something — even something with no verdict in it — has
// said its piece, and re-running it would pay a second time to overwrite an
// opinion that was already earned. Two attempts also bound the worst case: a
// reviewer that never reports costs at most twice its timeout, not an
// unbounded chain of retries against a provider that is simply broken.
const reviewerAttempts = 2

// runReviewerWithRetry launches one reviewer and returns the outcome to
// report for it — plus that same attempt's report text — retrying once when an
// attempt produced no report at all.
//
// The two returns always describe ONE attempt. That pairing is the whole point
// of returning them together rather than having the caller re-derive the text:
// persisting one attempt's report beside another attempt's verdict would file a
// report under a conclusion it never reached.
//
// The retry exists because "no report" is usually not the reviewer's verdict —
// it is the run failing in a way that leaves nothing to read. The case this was
// built for is an auto-rejected permission request, which kills the agent loop
// mid-work while the process still exits 0 (see noReportReason). A second
// attempt is cheap next to the round it saves, and a permanent cause simply
// fails the same way twice and gets reported with its reason instead of
// silently.
//
// Progress reporting is deliberately shared across both attempts rather than
// restarted: the tool count and cost on the closing line then cover everything
// the reviewer actually spent, which is what a human wants to see after paying
// for two attempts.
//
// When the retry fails too, the FIRST attempt's outcome is what gets reported,
// not the second's. The retry is a rescue, not a second opinion on the failure:
// it can replace the outcome only by producing a report. Letting a degenerate
// second attempt overwrite the first would lose the diagnosis exactly when it
// matters — a first attempt that failed with "ERROR(Unexpected server error…)"
// followed by a retry that died with nothing on stderr would report the vaguer
// of the two, throwing away the only words anyone had to go on.
func (tm *TaskManager) runReviewerWithRetry(
	opts opencode.RunOptions,
	progress *reviewerProgress,
) (outcome, report string) {
	var firstOutcome, firstReport string
	for attempt := 1; attempt <= reviewerAttempts; attempt++ {
		res, runErr := tm.OpenCode.Run(opts)
		outcome, report, reported := classifyReviewerRun(res, runErr)
		if reported {
			return outcome, report
		}
		if attempt == 1 {
			// Kept together for the same reason the reported case returns both:
			// whichever attempt's outcome is reported, its own text is what
			// gets persisted. An unreported attempt has no report text by
			// definition (that is what "unreported" means), so this carries
			// nothing today — carrying it anyway is what keeps the pairing
			// true of every return rather than of most of them.
			firstOutcome, firstReport = outcome, report
			progress.retrying(outcome)
		}
	}
	return firstOutcome, firstReport
}

// checkBranchStillCheckedOut verifies HEAD is still on the branch the round
// started against, and aborts the round when it is not.
//
// The branch decides two things for every reviewer: which tree gets reviewed,
// and — because the journal is keyed by branch name (ADR-0012 §5) — which
// journal their findings are written to. checkOnReviewableBranch establishes
// both before the first reviewer launches, but a reviewer runs shell commands
// for minutes, and one of them moving HEAD silently invalidates the round from
// that point on. This is not hypothetical: HEAD was switched to the default
// branch one minute into a real round, and that round's findings were written
// to a `main` journal — the file ADR-0018 exists to prevent, since journal
// cleanup rides on branch deletion and nobody deletes `main`.
//
// The round is aborted rather than continued with a warning: every verdict it
// could still produce would be about an unknown tree, and "which branch was
// reviewed" is exactly what a verdict means. The already-earned verdicts are
// dropped with it, deliberately — there is no way to tell whether the switch
// happened before, during, or after the reviewer that just finished, so
// reporting its verdict would be asserting something this command cannot know.
//
// HEAD is NOT put back. devgeta never silently moves a user's HEAD (ADR-0018),
// and the error names the branch to return to instead.
func (tm *TaskManager) checkBranchStillCheckedOut(branch, lastReviewer string) error {
	current, err := tm.Git.CurrentBranch()
	if err != nil {
		// Fail OPEN, unlike the mismatch case below: an unreadable HEAD is not
		// evidence that anything moved, and aborting a paid-for round on
		// "cannot tell" would throw away work to prevent a problem that may
		// not exist. The mismatch itself is what must never be waved through.
		logger.L().Debugw(
			"review-run: could not re-check the current branch after a reviewer; "+
				"continuing the round",
			"branch", branch, "reviewer", lastReviewer, "error", err,
		)
		return nil
	}
	if current == branch {
		return nil
	}
	where := fmt.Sprintf("%q", current)
	if current == "" {
		where = "a detached HEAD"
	}
	return fmt.Errorf(
		"review-run: HEAD moved from %q to %s while %s was running, so this round is "+
			"abandoned — anything that reviewer recorded went to the journal for %s, not "+
			"%q, and no verdict from this round can be trusted. Run 'git switch %s', "+
			"check 'devgeta task review-notes' on both branches, then run review-run again",
		branch, where, lastReviewer, where, branch, branch,
	)
}

// progressWriter returns where ReviewRun writes its per-reviewer progress
// lines: TaskManager.ProgressOut when the caller set one, os.Stderr
// otherwise — so a TaskManager built as a literal in a test, bypassing New(),
// still gets a safe default instead of writing through a nil interface.
func (tm *TaskManager) progressWriter() io.Writer {
	if tm.ProgressOut != nil {
		return tm.ProgressOut
	}
	return os.Stderr
}

// now returns TaskManager.NowFn() when set, time.Now() otherwise — the same
// nil-means-default fallback as progressWriter, for the same reason.
func (tm *TaskManager) now() time.Time {
	if tm.NowFn != nil {
		return tm.NowFn()
	}
	return time.Now()
}

// formatElapsed renders a progress line's elapsed duration for a human
// glancing at it, in place of time.Duration.String()'s raw nanosecond
// precision — which reads as "(6.54025ms)" for a fast run and
// "(4m12.183746291s)" for a multi-minute one, neither of which anyone reads
// at a glance. A reviewer run is seconds-to-minutes long, so once a second
// has elapsed, precision below a tenth of a second is noise; below a second,
// though, millisecond precision is the only thing that distinguishes two
// fast runs, so it is kept there rather than rounded away to "0s" or "1s".
func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(100 * time.Millisecond).String()
}

// reviewerAgentFor resolves --reviewer to the OpenCode agent name to run,
// validating it against worktree.BuiltinReviewerChoices() — the same
// registry the `dg ws` R keybinding picks from, never a restated list here.
func reviewerAgentFor(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		key = DefaultReviewerKey
	}
	choices := worktree.BuiltinReviewerChoices()
	valid := make([]string, 0, len(choices))
	for _, c := range choices {
		if c.Key == key {
			return c.Agent, nil
		}
		valid = append(valid, c.Key)
	}
	return "", fmt.Errorf(
		"review-run: unknown reviewer %q. Valid reviewers: %s",
		key, strings.Join(valid, ", "),
	)
}

// checkOnReviewableBranch is review-run's branch guard — the mirror image of
// release's checkOnDefaultBranch, sharing the same HEAD resolution so the
// two can never disagree about what git reported. It returns the resolved
// default branch alongside current so checkBranchHasCommittedDiff (the third
// refusal) does not have to resolve HEAD a second time.
//
// It refuses two of the three things HEAD can be. On the default branch
// there is nothing to review against, and a detached HEAD has no branch name
// to key a journal by — reviewjournal would eventually refuse it too, but
// only after a full multi-model review had already been spent, so it is
// caught here instead. Both refusals carry the same fix.
func (tm *TaskManager) checkOnReviewableBranch() (current, defaultBranch string, err error) {
	current, defaultBranch, err = tm.resolveHead()
	if err != nil {
		return "", "", fmt.Errorf("review-run: %w", err)
	}
	if current == "" {
		return "", "", fmt.Errorf(
			"review-run: HEAD is detached, so there is no branch to review or to key a " +
				"review journal by — run 'git switch -c <branch>' to put this work on a branch first",
		)
	}
	if current == defaultBranch {
		return "", "", fmt.Errorf(
			"review-run: on the default branch %q, which is what a review compares against — "+
				"run 'git switch -c <branch>' to move this work onto a branch first",
			defaultBranch,
		)
	}
	return current, defaultBranch, nil
}

// checkBranchHasReviewableChanges is review-run's third refusal: a branch
// that changes nothing at all has nothing to review, even though it is a
// real, named, non-default branch that the first two refusals let through.
//
// "Changes nothing" means BOTH no commits ahead of the default branch AND a
// clean working tree. Either one alone is reviewable, because a review covers
// the branch's whole working state, not just its committed history — the same
// thing `dg ws`'s diff pane shows (see ADR-0019, and BranchDiff in
// branchdiff.go, which produces the diff the reviewers actually read). This
// is why uncommitted work no longer has to be committed just to get a review.
//
// It reuses aheadBehind (scope.go) — the same `git rev-list --left-right
// --count` call review-scope already runs to answer "what does this branch
// change against the default branch" — and Git.IsWorktreeDirty, the same
// `git status --porcelain` check release and worktree-start already use,
// rather than new invocations of its own. `git status --porcelain` reports
// untracked files too, so a branch whose only work is a brand-new file is
// reviewable rather than refused.
func (tm *TaskManager) checkBranchHasReviewableChanges(branch, defaultBranch string) error {
	_, ahead, err := tm.aheadBehind(defaultBranch)
	if err != nil {
		// Fail OPEN, not closed. This guard exists only to save the cost of a
		// round that has nothing to review — it is not a safety property, so
		// "cannot tell" must not be treated the same as "confirmed empty".
		// `aheadBehind` runs `git rev-list … origin/<default>...HEAD`, which
		// needs a local refs/remotes/origin/<default> ref; that ref is
		// legitimately absent in a repo with no remote, a shallow or
		// --single-branch clone, or a default branch never fetched (unlike
		// review-scope, review-run does not fetch first — see
		// Git.DefaultBranch's fallback in internal/apps/git/git.go). In that
		// case git exits 128 with a raw "unknown revision" error, and
		// surfacing that to the user is exactly what CLAUDE.md's error rule
		// forbids. Blocking the round on an unresolvable comparison is worse
		// than just spending one round finding out the branch turned out
		// empty, so let it proceed instead.
		logger.L().Debugw(
			"review-run: could not determine commits ahead of default branch; "+
				"proceeding without the empty-diff guard",
			"branch", branch, "defaultBranch", defaultBranch, "error", err,
		)
		return nil
	}
	if ahead > 0 {
		return nil
	}

	// No commits ahead, so the working tree is the only place a change could
	// still be. It is checked ONLY in this branch: with commits ahead there is
	// already something to review, and asking git a second question there
	// would cost a call whose answer changes nothing.
	dirty, err := tm.Git.IsWorktreeDirty("")
	if err != nil {
		// Fails open for the same reason the ahead count does: this guard saves
		// cost, it does not protect correctness. "Cannot tell whether the tree
		// is dirty" must not become "confirmed empty".
		logger.L().Debugw(
			"review-run: could not determine whether the working tree is dirty; "+
				"proceeding without the nothing-to-review guard",
			"branch", branch, "defaultBranch", defaultBranch, "error", err,
		)
		return nil
	}
	if dirty {
		return nil
	}
	return fmt.Errorf(
		"review-run: branch %q has no commits ahead of %q and no uncommitted changes, so "+
			"there is nothing to review — make a change, then run review-run again",
		branch, defaultBranch,
	)
}

// resolveReviewerRuns turns review.reviewers into the runs for this round.
// An unset (or all-blank) list is not an error: it means one reviewer on
// OpenCode's own default model, launched with no -m flag at all.
//
// A model id repeated in the list becomes ONE run, keeping the first
// occurrence's position. Running it twice would pay twice for one model's
// opinion, and in range mode it also destroys output: the report filename is
// derived from the run's label (reviewerReportName), so a second run with the
// same label overwrites the first's report while both output lines still name
// that one path — leaving a verdict line pointing at a report that describes a
// different verdict. One line, one report, matching text is the invariant this
// mode is built on, so the duplicate is dropped rather than reconciled later.
func resolveReviewerRuns() ([]reviewerRun, error) {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("review-run: failed to read the global config: %w", err)
	}
	// A missing config file is "nothing configured yet", which the empty
	// list below already handles — the same state as a config with no
	// review.reviewers key.
	runs := make([]reviewerRun, 0, len(gc.Review.Reviewers))
	seen := make(map[string]struct{}, len(gc.Review.Reviewers))
	for _, model := range gc.Review.Reviewers {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, dup := seen[model]; dup {
			continue
		}
		seen[model] = struct{}{}
		runs = append(runs, reviewerRun{label: model, model: model})
	}
	if len(runs) == 0 {
		return []reviewerRun{{label: defaultModelLabel}}, nil
	}
	return runs, nil
}

// reviewNoteHeader introduces --note's text inside the reviewer's prompt. It
// states plainly that the note adds emphasis rather than replacing the
// reviewer's scope, so "focus on document A" cannot be read as "review only
// document A" — the reviewer agent's own instructions still decide what is in
// scope, and a note must never be able to shrink a review into a spot check
// while still reporting a whole-branch verdict.
const reviewNoteHeader = "Note from the person who asked for this review — extra context " +
	"and emphasis, not a narrower scope; still review everything you normally would:\n"

// reviewNoteSuffix validates --note and returns what it appends to whichever
// prompt the round is using: nothing when the flag was not passed.
//
// It is split from prompt composition so that both modes get one validation,
// one header, and one spelling of where the note sits — and so a blank --note
// is still refused before either mode pays a single git call, which is what
// range mode's own resolve-first guards would otherwise push it behind.
//
// A note that is present but blank is refused rather than dropped: the caller
// meant to say something to the reviewers, and silently running a round
// without it would look identical to a round that carried it.
func reviewNoteSuffix(note string) (string, error) {
	if note == "" {
		return "", nil
	}
	trimmed := strings.TrimSpace(note)
	if trimmed == "" {
		return "", fmt.Errorf(
			"review-run: --note is blank — pass the text to send the reviewers, " +
				"or omit the flag entirely",
		)
	}
	return "\n\n" + reviewNoteHeader + trimmed, nil
}

// rangeReviewPromptTemplate is the opening prompt every reviewer gets in
// explicit-range mode, in place of worktree.ReviewPrompt.
//
// Longer than the branch prompt on purpose. That one can be one sentence
// because the reviewer agents' own instructions already scope themselves
// correctly for a checked-out branch (`review-scope`, `branch-diff`). Here
// every one of those defaults is wrong — wrong diff, wrong files, wrong
// journal — so the prompt has to say what replaces them. Three things it must
// state, each pinned by a test:
//
//  1. The target is two commits, and the working tree is NOT part of it. This
//     is the one place range mode contradicts ADR-0019, which deliberately
//     widened a branch review to include uncommitted work; on someone else's
//     pull request that work is unrelated, and including it would put the
//     reader's own edits in front of a reviewer judging another author's
//     change (ADR-0023 §5).
//  2. The diff comes from `review-package <base> <head>`. `review-scope` and
//     `branch-diff` read the checkout, so here they describe different code
//     entirely — the same reasoning /review-pr's --target mode applies.
//  3. A journal key AND a revision, named plainly. That pair is the trigger
//     the three reviewer agents already carry ("when your launch prompt gives
//     you a journal key and a revision, append --branch <key> --rev <sha>"),
//     so the prompt states both and the appended flags, and the agent files
//     stay the single place that rule is spelled out.
//
// The shas interpolated here are resolved commit SHAs, never the caller's
// spelling of them: a review takes minutes across several models, and a ref
// name read twice inside that window can name two different commits.
const rangeReviewPromptTemplate = `Review the commit range %[1]s..%[2]s.

This is not a review of the checked-out branch. Review those two commits and nothing else: the range is immutable, and the working tree is not part of it — uncommitted and untracked files here belong to other work and are out of scope no matter how related they look, and whatever branch happens to be checked out is irrelevant to this review.

Get the diff with ` + "`devgeta task review-package %[1]s %[2]s`" + ` — one call for the commit list, the noise-filtered stat table, and the full diff of the range. ` + "`devgeta task review-scope`" + ` and ` + "`devgeta task branch-diff`" + ` describe the checked-out branch, so they do not apply here: do not run them. Read any file you need at the reviewed revision with ` + "`git show %[2]s:<path>`" + `, never from disk.

Your journal key is %[3]s and the revision under review is %[2]s, so every ` + "`devgeta task review-notes`" + ` and ` + "`devgeta task review-note`" + ` call carries ` + "`--branch %[3]s --rev %[2]s`" + `.`

// rangeReviewPrompt fills that template with the resolved range and the
// journal key the round writes under.
func rangeReviewPrompt(base, head, journalKey string) string {
	return fmt.Sprintf(rangeReviewPromptTemplate, base, head, journalKey)
}

// reviewRoundTarget settles what this round reviews and where its findings go:
// the base prompt every reviewer gets, the journal key entries are written
// under, and — branch mode only — the branch HEAD must stay on for the round's
// verdicts to mean anything.
//
// branch comes back empty in range mode, and that is the whole difference
// between the modes' preconditions. Branch mode infers its target from the
// checkout, so it must refuse the three HEADs that cannot be reviewed
// (ADR-0018's default branch and detached HEAD, ADR-0019's branch that changes
// nothing) and must keep re-checking that HEAD has not moved. Range mode is
// handed its target, so none of those three refusals has anything left to
// protect: the diff is an explicit non-empty range and the journal key is
// stated, which is exactly what each refusal exists to infer (ADR-0023 §5).
//
// What range mode refuses instead is a target it cannot resolve — a base or
// head this clone does not have, or a report directory it cannot create.
// Cheapest guards first, for the same reason the branch refusals run here: a
// bad sha must cost nothing, not surface minutes later as a confusing failure
// inside a reviewer's own tool call, and a report directory that cannot be
// written must not be discovered after a round's worth of reports have been
// produced with nowhere to go.
func (tm *TaskManager) reviewRoundTarget(
	rng ReviewRange,
	rangeMode bool,
) (basePrompt, journalKey, branch string, err error) {
	if !rangeMode {
		current, defaultBranch, err := tm.checkOnReviewableBranch()
		if err != nil {
			return "", "", "", err
		}
		if err := tm.checkBranchHasReviewableChanges(current, defaultBranch); err != nil {
			return "", "", "", err
		}
		return worktree.ReviewPrompt, current, current, nil
	}

	base, err := tm.resolveRangeEnd("--base", rng.Base)
	if err != nil {
		return "", "", "", err
	}
	head, err := tm.resolveRangeEnd("--head", rng.Head)
	if err != nil {
		return "", "", "", err
	}
	if err := ensureWritableReportDir(rng.ReportDir); err != nil {
		return "", "", "", fmt.Errorf(
			"review-run: cannot use %s as --report-dir: %w — pass a directory this user can "+
				"create and write to, since every reviewer's report is written there",
			rng.ReportDir, err,
		)
	}
	return rangeReviewPrompt(base, head, rng.Journal), rng.Journal, "", nil
}

// ensureWritableReportDir creates the report directory and proves a report can
// actually be written into it.
//
// os.MkdirAll alone proves only half of that. It returns nil for a path that
// already exists as a directory whatever its permissions, so a --report-dir that
// exists but rejects writes — another user's directory, a read-only mount, a
// leftover from a run under different ownership — would pass the up-front check
// and fail later inside writeReviewerReport, after the first reviewer had already
// spent its minutes. That is exactly the failure this guard exists to prevent, so
// the guard has to cover it.
//
// The proof is an attempted write, not a reading of the mode bits: ACLs, a
// read-only filesystem, and running as root each make the permission bits a bad
// predictor of whether the write lands. So the probe is a real report write —
// WriteFileAtomic with report content and report permissions, the same call
// writeReviewerReport makes per report later — paid once here where it costs
// nothing.
//
// It has to be that same call, not an imitation of it. Creating an empty file
// and storing bytes in it are separate operations with separate failure modes:
// a full disk or an exhausted block quota can still hand out a zero-byte inode
// while the write of actual content fails with ENOSPC/EDQUOT, and the rename
// that commits a report can fail on a path an empty temp file never touches.
// A probe that only created and closed a file would pass in exactly those cases
// and leave writeReviewerReport — whose own error message tells the user to
// free space — to discover them after a reviewer had already run.
func ensureWritableReportDir(dir string) error {
	if err := os.MkdirAll(dir, files.DirPermission); err != nil {
		return err
	}
	path := reportProbePath(dir)
	if err := files.WriteFileAtomic(
		path,
		[]byte("devgeta --report-dir write probe\n"),
		reviewReportPermission,
	); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		// Reported, not returned, like the round-start snapshot's cleanup: the
		// write this guard was checking for already succeeded, so refusing the
		// round here would reject a directory that works.
		logger.L().Errorw(
			"failed to remove the --report-dir write probe",
			"path", path, "error", err,
		)
	}
	return nil
}

// reportProbePath is where ensureWritableReportDir writes its probe report.
//
// Dot-prefixed so a probe that somehow outlives the call cannot be mistaken for
// a report by whoever reads the directory, and pid-suffixed so two rounds
// probing one shared --report-dir at the same time cannot delete each other's
// probe out from under the write they are each testing.
func reportProbePath(dir string) string {
	return filepath.Join(dir, fmt.Sprintf(".devgeta-report-probe-%d", os.Getpid()))
}

// resolveRangeEnd resolves one end of an explicit range to an immutable commit
// SHA, or refuses the round.
//
// Refusing here rather than letting the reviewer find out is the point. The
// likeliest bad value in this flow is a head that was never fetched (the pull
// request flow fetches refs/pull/<n>/head before resolving anything), and a
// reviewer handed one would spend minutes discovering it through failing tool
// calls of its own — then report something shaped like a verdict about a diff
// it never read.
func (tm *TaskManager) resolveRangeEnd(flag, ref string) (string, error) {
	sha, err := tm.Git.ResolveCommit(ref)
	if err != nil {
		return "", fmt.Errorf(
			"review-run: %s %s does not name a commit this clone has — check the sha, or "+
				"fetch the commit first (a pull request head needs "+
				"'git fetch origin refs/pull/<n>/head'), then run review-run again: %w",
			flag, ref, err,
		)
	}
	return sha, nil
}

// reportField separates a run's verdict from where its full report was written.
// Two spaces, then a `key: value` label, per docs/guides/task-design.md's
// labeled-plain-text rule. It is the LAST field on the line: an ERROR(...)
// outcome carries OpenCode's own words, so a parser reads the report path from
// the right rather than assuming the outcome contains no colon.
const reportField = "  report: "

// reportNone is the report path's value for a run that produced no report at
// all. The field is always present in range mode so a caller can parse one
// shape rather than two, and naming the absence is what keeps "no report" from
// reading as a missing field or an empty file.
const reportNone = "none (the reviewer wrote no report)"

// reviewReportPermission is the mode a persisted report is written with. Same
// 0600 as the review journal (reviewjournal's journalPermission) and for the
// same reason, more strongly: a report quotes findings, evidence, and code
// verbatim out of the reviewed range, so on a shared machine it carries more of
// the repo's content than a settings file does.
const reviewReportPermission = 0o600

// writeReviewerReport persists one run's full report and returns what the run's
// output line says about it: the file's path, or reportNone when the run
// produced nothing to persist.
//
// A whitespace-only report is treated as none rather than written, because that
// is the same state classifyReviewerRun reads as "no report at all" — writing a
// file for it would put an empty report in front of whoever composes the review
// and let it read as a reviewer who found nothing.
func writeReviewerReport(dir, agent, label, report string) (string, error) {
	if strings.TrimSpace(report) == "" {
		return reportNone, nil
	}
	path := filepath.Join(dir, reviewerReportName(agent, label))
	if err := files.WriteFileAtomic(path, []byte(report), reviewReportPermission); err != nil {
		// The round stops here, unlike a reviewer that fails: in range mode the
		// report IS the deliverable the caller composes a review from, so
		// continuing would keep paying for reviewer runs whose output lands
		// nowhere. Reports already written stay on disk, and the message names
		// the one that did not.
		return "", fmt.Errorf(
			"review-run: %s reviewed on %s but its report could not be written to %s: %w — "+
				"the round is stopped rather than left with findings nowhere; free space or "+
				"fix the directory's permissions, then run review-run again",
			agent, label, path, err,
		)
	}
	return path, nil
}

// reviewerReportName is one run's report filename: the reviewer agent that
// produced it, then the model it ran on.
//
// Both segments go through reviewjournal.EncodeBranch — the encoder ADR-0012 §5
// already relies on, not a second safe-filename scheme. It is what makes a
// model id usable here at all: those are `provider/model`, so an unencoded
// segment would carry a path separator and write outside the report directory.
// The agent segment needs no encoding today (BuiltinReviewerChoices' names are
// plain), and is encoded anyway so the filename has exactly one construction
// rule rather than one rule per segment.
//
// The segments are joined with reportNameSeparator, which makes the join
// injective as a property of the encoder rather than of today's registry: no
// (agent, label) pair can spell the same filename as a different pair, whatever
// names the reviewer registry grows.
func reviewerReportName(agent, label string) string {
	return reviewjournal.EncodeBranch(agent) +
		reportNameSeparator +
		reviewjournal.EncodeBranch(label) + ".md"
}

// reportNameSeparator joins the two segments of a report filename.
//
// It is "+" because EncodeBranch can never emit that byte: the encoder keeps
// only [A-Za-z0-9._-] and percent-encodes every other byte, so "+" survives in
// the filename exactly once — at the join — and the two segments are always
// recoverable from it. A separator the encoder passes through, "-" among them,
// would make the join ambiguous in the abstract: an agent named
// "code-reviewer-strict" with label "x" and an agent "code-reviewer" with label
// "strict-x" would spell one filename, and one run's report would overwrite the
// other's. That is impossible in today's registry, which is exactly why it is
// worth closing structurally instead of trusting a comment about the registry's
// current shape to survive the next name added to it.
const reportNameSeparator = "+"

// ocEvent is the slice of an `opencode run --format json` NDJSON event this
// command reads. Part and Error stay raw and are decoded per event type, so
// one event whose payload has an unexpected shape cannot cost us the events
// around it.
type ocEvent struct {
	Type  string          `json:"type"`
	Part  json.RawMessage `json:"part"`
	Error json.RawMessage `json:"error"`
}

// classifyReviewerRun maps one reviewer's run to exactly one outcome, hands
// back the report that outcome was read out of, and reports whether the
// reviewer wrote a report at all.
//
// report is the assistant text scanRunEvents already extracted from this run's
// event stream — returned rather than dropped so range mode can persist it
// (ADR-0023 §5) without parsing the same NDJSON a second way. Two derivations
// of "the reviewer's report" could disagree, and the one that decided the
// verdict is the one worth keeping.
//
// ERROR is decided by the error EVENT and the process exit status, never by
// matching message text: a probe against the real binary showed an unusable
// model produces a generic "Unexpected server error", with nothing in it
// naming the actual cause, so any text match would be guessing.
//
// The second return value, reported, is false when the run produced no
// assistant text whatsoever. That is a distinct failure from "wrote a report
// that named no verdict": it means the agent loop ended before the model ever
// said anything, so there is nothing to read a verdict out of and nothing to
// tell the human why. ReviewRun uses it to decide whether the attempt is worth
// repeating — see runReviewerWithRetry.
func classifyReviewerRun(
	res opencode.RunResult,
	runErr error,
) (outcome, report string, reported bool) {
	text, reason, finishReason, parsedAny := scanRunEvents(res.Stdout)
	reported = strings.TrimSpace(text) != ""
	switch {
	case reason != "":
		return fmt.Sprintf("ERROR(%s)", reason), text, reported
	case runErr != nil:
		// Spawn failure, nonzero exit, or timeout with no error event to
		// explain it — the wrapper's own message is all there is.
		return fmt.Sprintf("ERROR(%s)", truncateReason(runErr.Error())), text, reported
	case !parsedAny && strings.TrimSpace(res.Stdout) != "":
		// Output that is not the event stream at all. Note this needs
		// EVERY line to be unreadable: a shell that prints its own warning
		// before opencode's first event (zoxide's, in the Step 0 probe
		// captures) must not be mistaken for a failed run.
		return fmt.Sprintf(
			"ERROR(%s)",
			truncateReason(firstNonBlankLine(res.Stdout)),
		), text, reported
	}
	if verdict := lastStatusVerdict(text); verdict != "" {
		return verdict, text, reported
	}
	if !reported {
		// The failure this whole path exists for: exit 0, no error event, and
		// not one word from the model. Reporting a bare NO VERDICT here is what
		// made a real round unexplainable — see noReportReason.
		return fmt.Sprintf(
			"%s(%s)",
			outcomeNoVerdict,
			noReportReason(res.Stderr, finishReason),
		), text, false
	}
	return outcomeNoVerdict, text, true
}

// noReportFallbackReason is used when a run wrote no report and left nothing
// anywhere to explain it — no stderr, and no step reason. It states that
// plainly rather than inventing a cause.
const noReportFallbackReason = "the reviewer wrote no report and opencode gave no reason"

// noReportReason explains a run that produced no assistant text at all.
//
// stderr comes first because it is where the real cause is written and where
// nothing else looks. A headless run auto-rejects any permission it cannot ask
// a human about, and says so ONLY there:
//
//	! permission requested: external_directory (/Users/x/.claude/*); auto-rejecting
//
// That line never appears as an event, the process still exits 0, and the
// agent loop dies mid-work — which is how a paid multi-minute round came back
// as a bare "NO VERDICT" with no reason anywhere.
//
// The step reason is the fallback: OpenCode ends every step with why it
// stopped, and a final step that ends on "tool-calls" rather than "stop" means
// the loop quit while the model still had work queued. That does not name the
// cause the way stderr does, but it does distinguish "ended mid-work" from
// "ran and chose to say nothing", which is the next most useful thing to tell
// a human.
//
// Both are OpenCode's own words, cut to length — never reworded or guessed at,
// the same rule truncateReason follows for a provider error message.
func noReportReason(stderr, finishReason string) string {
	if cleaned := strings.TrimSpace(stripANSI(stderr)); cleaned != "" {
		return truncateReason(cleaned)
	}
	// "stop" is a normal ending, so it explains nothing about a missing report
	// and is not worth printing; anything else is.
	if finishReason != "" && finishReason != finishReasonStop {
		return truncateReason(fmt.Sprintf(
			"the agent loop ended while it still had work queued (last step reason: %s)",
			finishReason,
		))
	}
	return noReportFallbackReason
}

// finishReasonStop is the step_finish reason for a step that ended normally,
// because the model was done rather than because it was cut off. Any other
// final reason on a run that produced no report means the loop stopped while
// the model still had work queued — see noReportReason.
const finishReasonStop = "stop"

// scanRunEvents walks the NDJSON stream once and returns the assistant's
// concatenated text, the first error event's reason (empty if none), the LAST
// step_finish reason seen (empty if none), and whether any line parsed as an
// event at all.
//
// Text is concatenated rather than examined per event because the assistant's
// message arrives in chunks that can split a line — including the
// `**Status:**` line — anywhere.
//
// The last step reason is kept rather than the first because it is the one
// that describes how the run ENDED. A healthy run ends "stop"; every step
// before it legitimately ends "tool-calls" while the model works.
func scanRunEvents(stdout string) (text, reason, finishReason string, parsedAny bool) {
	var sb strings.Builder
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev ocEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue // not an event: shell noise sharing the stream
		}
		parsedAny = true
		switch ev.Type {
		case "text":
			var part struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(ev.Part, &part) == nil {
				sb.WriteString(part.Text)
			}
		case "error":
			if reason == "" {
				reason = errorEventReason(ev.Error)
			}
		case "step_finish":
			var part struct {
				Reason string `json:"reason"`
			}
			if json.Unmarshal(ev.Part, &part) == nil && part.Reason != "" {
				finishReason = part.Reason
			}
		}
	}
	return sb.String(), reason, finishReason, parsedAny
}

// ansiEscapePattern matches the ANSI escape sequences OpenCode wraps its
// stderr warnings in — the real auto-reject line arrives as
// "\x1b[93m\x1b[1m! \x1b[0mpermission requested: ...".
//
// They are stripped before a reason reaches the outcome line because that line
// is a parseable contract an agent reads (docs/guides/task-design.md), and raw
// escape bytes in it are noise at best and a broken parse at worst.
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI removes those sequences, leaving OpenCode's own words untouched.
func stripANSI(s string) string {
	return ansiEscapePattern.ReplaceAllString(s, "")
}

// errorEventReason extracts OpenCode's own words from an error event. It
// never returns "", because "" is what classifyReviewerRun reads as "no
// error event happened" — an error with nothing readable in it is still an
// error.
func errorEventReason(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "opencode reported an error with no message"
	}
	var structured struct {
		Name string `json:"name"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &structured) == nil {
		if msg := strings.TrimSpace(structured.Data.Message); msg != "" {
			return truncateReason(msg)
		}
		if name := strings.TrimSpace(structured.Name); name != "" {
			return truncateReason(name)
		}
	}
	var plain string
	if json.Unmarshal(raw, &plain) == nil && strings.TrimSpace(plain) != "" {
		return truncateReason(plain)
	}
	return truncateReason(string(raw))
}

// lastStatusVerdict returns the verdict from the LAST status line ("Status:"
// with any placement of "*"/"_" emphasis around it, see stripLineEmphasis)
// that names a real verdict, or "" when none does.
//
// "Last" is what the reviewer contract means: a report can quote the format
// before filling it in, and the final line is the conclusion. Skipping a
// status line that names no verdict is what keeps a trailing quote of the
// template from erasing a real verdict above it — and a line still offering
// the choices ("APPROVE | REQUEST CHANGES | ...") is exactly that quote,
// not a decision, so the "|" check refuses it.
//
// A status line inside a fenced code block is skipped outright, even when it
// names a real verdict: a report that quotes a concrete example — e.g. a
// fence showing what "**Status:** APPROVE" looks like — sits below its own
// real verdict, and the unsafe direction is toward APPROVE, so a quoted
// example must never be able to overwrite a decision already made.
//
// Fence tracking is a plain open/close toggle, so an UNBALANCED fence in a
// report (an opening ``` the model never closes) leaves the scanner "inside a
// fence" for the rest of the text and every status line after it is skipped —
// yielding NO VERDICT. That is deliberate rather than a gap worth repairing:
// NO VERDICT is blocking, so a malformed report costs a round and is reported
// to the human, which is the safe direction. Repairing it would mean guessing
// where the author meant the fence to end, and a wrong guess points the other
// way — toward reading a quoted example as a real approval.
func lastStatusVerdict(text string) string {
	verdict := ""
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		if isFenceDelimiter(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		match := statusLinePattern.FindStringSubmatch(stripLineEmphasis(line))
		if match == nil {
			continue
		}
		value := normalizeStatusValue(match[1])
		if strings.Contains(value, "|") {
			continue
		}
		for _, known := range knownVerdicts {
			if matchesKnownVerdict(value, known) {
				verdict = known
				break
			}
		}
	}
	return verdict
}

// verdictProsePunctuation is the set of characters allowed to immediately
// follow a known verdict (after at most one intervening space) for the value
// to still count as that verdict. Each one unambiguously opens a trailing
// prose clause rather than continuing the verdict word itself: an em dash or
// an ASCII hyphen introduces a clause ("APPROVE — looks good" /
// "APPROVE - looks good"), a colon or semicolon introduces an explanation, a
// comma or period closes the clause, and an opening parenthesis opens an
// aside. A letter or digit right after the verdict (as in "APPROVED") is not
// prose punctuation, and neither is a plain space before another word (as in
// "APPROVE NOT") — both must fall through to NO VERDICT rather than match,
// because the unsafe direction here is toward APPROVE: a malformed line
// should cost a round, never fabricate an approval.
const verdictProsePunctuation = "—-:,.;("

// matchesKnownVerdict reports whether value IS known, or known followed
// immediately (or after a single space) by verdictProsePunctuation. This
// replaces a plain strings.HasPrefix check, which let "APPROVED" match
// "APPROVE" — and it deliberately stops short of strict equality, which
// would break the common "**Status:** APPROVE — looks good" line by turning
// a real approval into NO VERDICT.
func matchesKnownVerdict(value, known string) bool {
	if value == known {
		return true
	}
	if !strings.HasPrefix(value, known) {
		return false
	}
	rest := strings.TrimPrefix(value[len(known):], " ")
	if rest == "" {
		// Nothing but a trailing space after the verdict: normalizeStatusValue
		// already trims via strings.Fields, so this should not occur — but
		// treat it as "not clearly prose" rather than guess.
		return false
	}
	r, _ := utf8.DecodeRuneInString(rest)
	return strings.ContainsRune(verdictProsePunctuation, r)
}

// isFenceDelimiter reports whether line opens or closes a fenced code block
// (``` or ~~~, optionally followed by a language tag) — Markdown's two fence
// styles, either of which can wrap a quoted example in a reviewer's report.
//
// It is checked against the RAW line, never the emphasis-stripped one:
// stripLineEmphasis only removes "*" and "_", neither of which a fence
// delimiter is built from ("```" / "~~~"), so this ordering is not
// load-bearing today — but it stays deliberate so a future emphasis marker
// never has a chance to hide a fence boundary.
func isFenceDelimiter(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// stripLineEmphasis removes markdown emphasis markers ("*" and "_") from a
// single line before statusLinePattern is applied, so a reviewer's verdict
// line reads the same regardless of where those markers landed: the agent
// template's "**Status:** APPROVE" (emphasis around "Status:" only), a real
// run's "**Status: APPROVE**" (emphasis around the whole line), "Status:
// APPROVE" (no emphasis at all), and the single-asterisk and underscore
// variants of both. This is a line-level normalization only — it does not
// parse markdown structure, so it cannot tell "*" used as emphasis from "*"
// used as, say, a literal multiplication sign; that tradeoff is acceptable
// here because the only thing read out of the result is whether the line
// starts with "status:" and what follows it.
//
// Backticks are deliberately left alone: they can legitimately wrap the
// verdict VALUE (e.g. "**Status:** `APPROVE`"), and normalizeStatusValue
// already strips them from the captured value — stripping them here too
// would be redundant, not more correct.
func stripLineEmphasis(line string) string {
	return strings.NewReplacer("*", "", "_", "").Replace(line)
}

// normalizeStatusValue strips the markdown a status line can carry and
// collapses its spacing, so "`Approve`" and "**APPROVE**" read the same as
// "APPROVE".
func normalizeStatusValue(s string) string {
	s = strings.NewReplacer("*", " ", "`", " ", "_", " ").Replace(s)
	return strings.ToUpper(strings.Join(strings.Fields(s), " "))
}

// truncateReason folds a reason onto one line and cuts it to maxReasonLen,
// marking the cut with an ellipsis so a truncated message is never mistaken
// for the whole of what OpenCode said.
func truncateReason(s string) string {
	return truncateOneLine(s, maxReasonLen)
}

// truncateOneLine folds s onto a single line (collapsing every run of
// whitespace, including newlines, to one space) and cuts it to max runes,
// marking the cut with an ellipsis. Shared by truncateReason and the
// progress reporter's tool labels (reviewprogress.go): both take free-form
// text from OpenCode — a provider message, a tool's own arguments — and both
// must keep one reviewer to one line.
//
// The cut is by rune, not by byte: that text can carry multi-byte runes, and
// slicing at a byte offset that lands inside one would emit an invalid UTF-8
// fragment into output an agent parses.
func truncateOneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return strings.TrimSpace(string(runes[:max])) + "…"
}

// firstNonBlankLine returns the first line with content, for the case where
// stdout carried no events to quote from.
func firstNonBlankLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}
