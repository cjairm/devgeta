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
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cjairm/devgeta/internal/apps/opencode"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/reviewjournal"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
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

// ReviewRun runs one review round: every configured reviewer model, in
// order, through the OpenCode wrapper, against the current branch — then
// returns one line per reviewer, and nothing else.
//
// note, when non-empty, is the human's own steering for this round (`--note`).
// It is appended to the fixed review prompt as extra context, and deliberately
// does not narrow the review — see reviewNoteHeader.
//
// Reviewers are isolated on the read side only (ADR-0017 §4): a snapshot of
// the journal as it stood at round start is written first, and each reviewer
// is pointed at it, so no reviewer sees what a peer opened or settled during
// this same round. Their writes go straight to the live journal and get
// real, final ids.
//
// While a reviewer runs, progress goes to progressWriter() (stderr by
// default) as it happens — a start line, one line per tool call the reviewer
// makes, and a closing line with the outcome — never to the returned string,
// which stays the exact parseable contract docs/guides/task-design.md
// governs. A multi-minute headless run against a real branch diff would
// otherwise leave the caller watching silence with no way to tell working
// from stuck.
func (tm *TaskManager) ReviewRun(reviewer, note string) (string, error) {
	// Cheapest guards first: a bad --reviewer or a blank --note needs no git
	// and no config.
	agent, err := reviewerAgentFor(reviewer)
	if err != nil {
		return "", err
	}
	prompt, err := reviewPromptWithNote(note)
	if err != nil {
		return "", err
	}

	// All three refusals happen here, before any reviewer is launched and
	// before any file is written — a wrong branch, or a branch with nothing
	// to review at all, must cost nothing.
	branch, defaultBranch, err := tm.checkOnReviewableBranch()
	if err != nil {
		return "", err
	}
	if err := tm.checkBranchHasReviewableChanges(branch, defaultBranch); err != nil {
		return "", err
	}

	runs, err := resolveReviewerRuns()
	if err != nil {
		return "", err
	}

	jm := reviewjournal.New(tm.Git)
	snapshot, err := jm.WriteSnapshot("", branch)
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
		)
		progress.started()
		start := tm.now()

		// A reviewer that fails never aborts the ones after it: each is an
		// independent opinion, and losing the rest to one bad provider would
		// throw away work already paid for.
		stdout, runErr := tm.OpenCode.Run(opencode.RunOptions{
			Agent:        agent,
			Model:        run.model,
			Prompt:       prompt,
			Timeout:      reviewRunTimeout,
			Env:          []string{ReviewJournalSnapshotEnvVar + "=" + snapshot},
			OnStdoutLine: progress.line,
		})
		outcome := classifyReviewerRun(stdout, runErr)
		progress.finished(outcome, tm.now().Sub(start))

		// The branch is a precondition of the whole round, not just of its
		// first moment, so it is re-checked after every reviewer.
		if err := tm.checkBranchStillCheckedOut(branch, run.label); err != nil {
			return "", err
		}

		lines = append(lines, fmt.Sprintf("%s → %s", run.label, outcome))
	}
	return strings.Join(lines, "\n"), nil
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
func resolveReviewerRuns() ([]reviewerRun, error) {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("review-run: failed to read the global config: %w", err)
	}
	// A missing config file is "nothing configured yet", which the empty
	// list below already handles — the same state as a config with no
	// review.reviewers key.
	runs := make([]reviewerRun, 0, len(gc.Review.Reviewers))
	for _, model := range gc.Review.Reviewers {
		if model = strings.TrimSpace(model); model != "" {
			runs = append(runs, reviewerRun{label: model, model: model})
		}
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

// reviewPromptWithNote builds the round's prompt: the fixed review prompt on
// its own, or that prompt followed by the human's --note.
//
// A note that is present but blank is refused rather than dropped: the caller
// meant to say something to the reviewers, and silently running a round
// without it would look identical to a round that carried it.
func reviewPromptWithNote(note string) (string, error) {
	if note == "" {
		return worktree.ReviewPrompt, nil
	}
	trimmed := strings.TrimSpace(note)
	if trimmed == "" {
		return "", fmt.Errorf(
			"review-run: --note is blank — pass the text to send the reviewers, " +
				"or omit the flag entirely",
		)
	}
	return worktree.ReviewPrompt + "\n\n" + reviewNoteHeader + trimmed, nil
}

// ocEvent is the slice of an `opencode run --format json` NDJSON event this
// command reads. Part and Error stay raw and are decoded per event type, so
// one event whose payload has an unexpected shape cannot cost us the events
// around it.
type ocEvent struct {
	Type  string          `json:"type"`
	Part  json.RawMessage `json:"part"`
	Error json.RawMessage `json:"error"`
}

// classifyReviewerRun maps one reviewer's run to exactly one outcome.
//
// ERROR is decided by the error EVENT and the process exit status, never by
// matching message text: a probe against the real binary showed an unusable
// model produces a generic "Unexpected server error", with nothing in it
// naming the actual cause, so any text match would be guessing.
func classifyReviewerRun(stdout string, runErr error) string {
	text, reason, parsedAny := scanRunEvents(stdout)
	switch {
	case reason != "":
		return fmt.Sprintf("ERROR(%s)", reason)
	case runErr != nil:
		// Spawn failure, nonzero exit, or timeout with no error event to
		// explain it — the wrapper's own message is all there is.
		return fmt.Sprintf("ERROR(%s)", truncateReason(runErr.Error()))
	case !parsedAny && strings.TrimSpace(stdout) != "":
		// Output that is not the event stream at all. Note this needs
		// EVERY line to be unreadable: a shell that prints its own warning
		// before opencode's first event (zoxide's, in the Step 0 probe
		// captures) must not be mistaken for a failed run.
		return fmt.Sprintf("ERROR(%s)", truncateReason(firstNonBlankLine(stdout)))
	}
	if verdict := lastStatusVerdict(text); verdict != "" {
		return verdict
	}
	return outcomeNoVerdict
}

// scanRunEvents walks the NDJSON stream once and returns the assistant's
// concatenated text, the first error event's reason (empty if none), and
// whether any line parsed as an event at all.
//
// Text is concatenated rather than examined per event because the assistant's
// message arrives in chunks that can split a line — including the
// `**Status:**` line — anywhere.
func scanRunEvents(stdout string) (text, reason string, parsedAny bool) {
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
		}
	}
	return sb.String(), reason, parsedAny
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
