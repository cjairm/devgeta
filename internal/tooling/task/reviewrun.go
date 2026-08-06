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

// statusLinePattern matches the reviewer's verdict line, e.g.
// "**Status:** REQUEST CHANGES". Case-insensitive because the marker is
// prose the model reproduces, and per-line ([^\S\n]) so the capture stops at
// the end of that line rather than swallowing the rest of the report.
var statusLinePattern = regexp.MustCompile(`(?mi)^[^\S\n]*\*\*status:\*\*[^\S\n]*(.*)$`)

// reviewerRun is one reviewer this round runs: how its line is labeled, and
// which model to pin it to ("" = no -m flag, i.e. OpenCode's own default).
type reviewerRun struct {
	label string
	model string
}

// ReviewRun runs one review round: every configured reviewer model, in
// order, through the OpenCode wrapper, against the current branch — then
// prints one line per reviewer and the ids still open in the journal.
//
// Reviewers are isolated on the read side only (ADR-0017 §4): a snapshot of
// the journal as it stood at round start is written first, and each reviewer
// is pointed at it, so no reviewer sees what a peer opened or settled during
// this same round. Their writes go straight to the live journal and get
// real, final ids — which is why the open list at the end is read from the
// live journal, not the snapshot.
func (tm *TaskManager) ReviewRun(reviewer string) (string, error) {
	// Cheapest guard first: a bad --reviewer needs no git and no config.
	agent, err := reviewerAgentFor(reviewer)
	if err != nil {
		return "", err
	}

	// Both branch refusals happen here, before any reviewer is launched and
	// before any file is written — a wrong branch must cost nothing.
	branch, err := tm.checkOnReviewableBranch()
	if err != nil {
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

	var out strings.Builder
	for _, run := range runs {
		// A reviewer that fails never aborts the ones after it: each is an
		// independent opinion, and losing the rest to one bad provider would
		// throw away work already paid for.
		stdout, runErr := tm.OpenCode.Run(opencode.RunOptions{
			Agent:   agent,
			Model:   run.model,
			Prompt:  worktree.ReviewPrompt,
			Timeout: reviewRunTimeout,
			Env:     []string{ReviewJournalSnapshotEnvVar + "=" + snapshot},
		})
		fmt.Fprintf(&out, "%s → %s\n", run.label, classifyReviewerRun(stdout, runErr))
	}

	open, err := openEntryIDs(jm, branch)
	if err != nil {
		return "", fmt.Errorf("review-run: %w", err)
	}
	out.WriteString(open)
	return out.String(), nil
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
// two can never disagree about what git reported.
//
// It refuses two of the three things HEAD can be. On the default branch
// there is nothing to review against, and a detached HEAD has no branch name
// to key a journal by — reviewjournal would eventually refuse it too, but
// only after a full multi-model review had already been spent, so it is
// caught here instead. Both refusals carry the same fix.
func (tm *TaskManager) checkOnReviewableBranch() (string, error) {
	current, defaultBranch, err := tm.resolveHead()
	if err != nil {
		return "", fmt.Errorf("review-run: %w", err)
	}
	if current == "" {
		return "", fmt.Errorf(
			"review-run: HEAD is detached, so there is no branch to review or to key a " +
				"review journal by — run 'git switch -c <branch>' to put this work on a branch first",
		)
	}
	if current == defaultBranch {
		return "", fmt.Errorf(
			"review-run: on the default branch %q, which is what a review compares against — "+
				"run 'git switch -c <branch>' to move this work onto a branch first",
			defaultBranch,
		)
	}
	return current, nil
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

// openEntryIDs renders the round's closing line from the LIVE journal, so it
// includes what this round's reviewers just opened (the snapshot they read
// from deliberately does not).
func openEntryIDs(jm *reviewjournal.Manager, branch string) (string, error) {
	j, err := jm.Load("", branch)
	if err != nil {
		return "", err
	}
	ids := make([]string, 0, len(j.Entries))
	for _, e := range j.Entries {
		if e.Open() {
			ids = append(ids, e.ID)
		}
	}
	if len(ids) == 0 {
		// A sentinel, never an empty tail: an agent cannot tell "no open
		// findings" from truncated output (task-design.md output rule 4).
		return "open: none", nil
	}
	return "open: " + strings.Join(ids, " "), nil
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

// lastStatusVerdict returns the verdict from the LAST `**Status:**` line
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
		match := statusLinePattern.FindStringSubmatch(line)
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
func isFenceDelimiter(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
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
//
// The cut is by rune, not by byte: a provider message is free-form text and
// can carry multi-byte runes, and slicing at a byte offset that lands inside
// one would emit an invalid UTF-8 fragment into output an agent parses.
func truncateReason(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= maxReasonLen {
		return s
	}
	return strings.TrimSpace(string(runes[:maxReasonLen])) + "…"
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
