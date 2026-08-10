package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/internal/tooling/reviewjournal"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	"github.com/cjairm/devgeta/pkg/paths"
)

// --- fixtures -------------------------------------------------------------

// withReviewers points the global config at a throwaway root and writes one
// there with the given review.reviewers list. Pass no models for "no config
// file at all", which is the same state as a config with no review key.
//
// The root is always overridden (and restored) even in the no-config case:
// the paths sandbox is shared by every test in this binary, so a config
// another test wrote must never decide this one's reviewer list.
func withReviewers(t *testing.T, models ...string) {
	t.Helper()
	orig := paths.Paths.Config.Root
	paths.Paths.Config.Root = t.TempDir()
	t.Cleanup(func() { paths.Paths.Config.Root = orig })

	if len(models) == 0 {
		return
	}
	var sb strings.Builder
	sb.WriteString("review:\n  reviewers:\n")
	for _, m := range models {
		sb.WriteString("    - " + m + "\n")
	}
	testutil.CreateGlobalConfigFile(t, paths.Paths.Config.Root, sb.String())
}

// withAheadCount overrides newRepoSetup's default ahead-of-default-branch
// count (3) for the one test that needs a different value — chiefly 0, to
// reach the working-tree half of checkBranchHasReviewableChanges. Every other
// git call keeps answering exactly as newRepoSetup's fixture already does.
func withAheadCount(t *testing.T, tm *TaskManager, ahead int) {
	t.Helper()
	gitBase, ok := tm.Git.Base.(*commands.MockBaseCommand)
	if !ok {
		t.Fatalf("expected a mock git base, got %T", tm.Git.Base)
	}
	orig := gitBase.ExecCommandFn
	gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if slices.Contains(c.Args, "rev-list") {
			return fmt.Sprintf("0\t%d\n", ahead), "", nil
		}
		return orig(c)
	}
}

// withRevListFailure makes the `rev-list` call aheadBehind runs fail exactly
// as it does in a repo with no local refs/remotes/origin/<default> ref: no
// remote, a shallow or --single-branch clone, or a default branch never
// fetched. This is the case checkBranchHasReviewableChanges must fail OPEN on
// rather than surface — see the guard's own comment.
func withRevListFailure(t *testing.T, tm *TaskManager) {
	t.Helper()
	gitBase, ok := tm.Git.Base.(*commands.MockBaseCommand)
	if !ok {
		t.Fatalf("expected a mock git base, got %T", tm.Git.Base)
	}
	orig := gitBase.ExecCommandFn
	gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if slices.Contains(c.Args, "rev-list") {
			return "", "fatal: ambiguous argument 'origin/main...HEAD': unknown revision or " +
				"path not in the working tree.", errors.New("exit status 128")
		}
		return orig(c)
	}
}

// withBranchSwitchAfter makes `git branch --show-current` answer `to` from the
// nth answer onward, reproducing something moving HEAD mid-round — what a
// reviewer's own shell command did in a real round. n counts every
// --show-current call, and review-run makes one before the first reviewer
// (checkOnReviewableBranch) and one after each, so n=2 means "HEAD moved while
// reviewer 1 was running".
func withBranchSwitchAfter(t *testing.T, tm *TaskManager, n int, to string) {
	t.Helper()
	gitBase, ok := tm.Git.Base.(*commands.MockBaseCommand)
	if !ok {
		t.Fatalf("expected a mock git base, got %T", tm.Git.Base)
	}
	orig := gitBase.ExecCommandFn
	calls := 0
	gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if slices.Contains(c.Args, "--show-current") {
			calls++
			if calls >= n {
				return to + "\n", "", nil
			}
		}
		return orig(c)
	}
}

// withDirtyWorktree makes `git status --porcelain` report the given porcelain
// output, so the working-tree half of checkBranchHasReviewableChanges sees a
// dirty tree. newRepoSetup's fixture answers "" (clean) by default.
func withDirtyWorktree(t *testing.T, tm *TaskManager, porcelain string) {
	t.Helper()
	gitBase, ok := tm.Git.Base.(*commands.MockBaseCommand)
	if !ok {
		t.Fatalf("expected a mock git base, got %T", tm.Git.Base)
	}
	orig := gitBase.ExecCommandFn
	gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if slices.Contains(c.Args, "status") {
			return porcelain, "", nil
		}
		return orig(c)
	}
}

// withStatusFailure makes `git status --porcelain` fail, the case the guard's
// second half must fail OPEN on.
func withStatusFailure(t *testing.T, tm *TaskManager) {
	t.Helper()
	gitBase, ok := tm.Git.Base.(*commands.MockBaseCommand)
	if !ok {
		t.Fatalf("expected a mock git base, got %T", tm.Git.Base)
	}
	orig := gitBase.ExecCommandFn
	gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if slices.Contains(c.Args, "status") {
			return "", "fatal: not a git repository", errors.New("exit status 128")
		}
		return orig(c)
	}
}

// scriptedRun is one `opencode run` the fixture will answer, in order.
//
// One scriptedRun is one ATTEMPT, not one reviewer: a reviewer whose attempt
// produces no report is retried once (reviewerAttempts), so such a reviewer
// consumes two entries here.
type scriptedRun struct {
	stdout string
	// stderr is what OpenCode wrote outside the event stream. It is where an
	// auto-rejected permission request lands, and the only explanation a run
	// that produced no report ever has.
	stderr string
	err    error
	// onCall, when set, runs at the moment this reviewer is launched — the
	// only place a test can observe what a reviewer would see mid-round.
	onCall func(t *testing.T, c commands.CommandParams)
}

// scriptOpenCode answers the OpenCode wrapper's calls from runs, in order,
// and fails the test if the command under test runs more or fewer reviewers
// than the script expects (a silently dropped reviewer is the failure this
// command must never have).
func scriptOpenCode(
	t *testing.T,
	base *commands.MockBaseCommand,
	runs ...scriptedRun,
) {
	t.Helper()
	calls := 0
	base.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if calls >= len(runs) {
			t.Errorf("unexpected extra opencode run #%d: %v", calls+1, c.Args)
			calls++
			return "", "", nil
		}
		run := runs[calls]
		calls++
		if run.onCall != nil {
			run.onCall(t, c)
		}
		return run.stdout, run.stderr, run.err
	}
	t.Cleanup(func() {
		if calls != len(runs) {
			t.Errorf("expected %d opencode run(s), got %d", len(runs), calls)
		}
	})
}

// retried returns the two scripted attempts ONE reviewer consumes when its run
// produces no report: the same failure twice, which is what a permanent cause
// (a rejected permission, an unusable model, a dead provider) actually does.
//
// Spelling it out at every call site is what keeps the retry visible in the
// tests: a reviewer scripted with a single failing attempt would fail the
// fixture's call-count check rather than quietly passing, which is how a
// dropped retry gets caught.
func retried(run scriptedRun) []scriptedRun {
	return []scriptedRun{run, run}
}

// verifyNoStrayCommands asserts review-run never reached around the app
// wrappers to the generic executor: it checks tm.Base — the TaskManager's own
// executor, which review-run is expected to leave completely unused — and not
// the Git app's or the OpenCode wrapper's bases, each of which has its own mock
// that the scripted expectations cover. So a real command recorded on either
// wrapper's base is out of this helper's reach.
func verifyNoStrayCommands(t *testing.T, tm *TaskManager) {
	t.Helper()
	base, ok := tm.Base.(*commands.MockBaseCommand)
	if !ok {
		t.Fatalf("expected a mock base executor, got %T", tm.Base)
	}
	testutil.VerifyNoRealCommands(t, base)
}

// textEvent renders one assistant-text NDJSON event, the shape the Step 0
// probe captured from the real binary.
func textEvent(text string) string {
	encoded, err := json.Marshal(text)
	if err != nil {
		panic(err)
	}
	return `{"type":"text","timestamp":1,"part":{"type":"text","text":` +
		string(encoded) + `}}`
}

// errorEvent renders the error event shape the Step 0 probe captured for an
// unusable model: a generic name and message, with nothing in the text
// naming the real cause.
func errorEvent(name, message string) string {
	encoded, err := json.Marshal(message)
	if err != nil {
		panic(err)
	}
	return `{"type":"error","error":{"name":"` + name + `","data":{"message":` +
		string(encoded) + `}}}`
}

// statusReport is a reviewer's report ending in the contract's verdict line.
func statusReport(verdict string) string {
	return strings.Join([]string{
		`{"type":"step_start","part":{"type":"step-start"}}`,
		textEvent("### Recommendation\n\n**Status:** " + verdict + "\n"),
		`{"type":"step_finish","part":{"type":"step-finish"}}`,
	}, "\n") + "\n"
}

// statusLineReport is statusReport's more general sibling: the caller
// supplies the WHOLE status line verbatim, including wherever it wants to
// place (or omit) markdown emphasis — statusReport always emphasizes only
// "Status:", which cannot express the shape a real run produced (emphasis
// around the whole line: "**Status: REQUEST CHANGES**").
func statusLineReport(line string) string {
	return strings.Join([]string{
		`{"type":"step_start","part":{"type":"step-start"}}`,
		textEvent("### Recommendation\n\n" + line + "\n"),
		`{"type":"step_finish","part":{"type":"step-finish"}}`,
	}, "\n") + "\n"
}

// --- outcomes -------------------------------------------------------------

func TestReviewRunVerdictOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		err    error
		want   string
	}{
		{"approve", statusReport("APPROVE"), nil, "APPROVE"},
		{"request changes", statusReport("REQUEST CHANGES"), nil, "REQUEST CHANGES"},
		{"needs discussion", statusReport("NEEDS DISCUSSION"), nil, "NEEDS DISCUSSION"},
		{
			"no verdict when the run says nothing about status",
			textEvent("I read the diff and have no comment.") + "\n",
			nil,
			"NO VERDICT",
		},
		// A run that exits 0 having said nothing at all is NOT in this table:
		// it produces no report, which is retried and reported with a reason.
		// See the no-report tests below.
		{
			// The verdict is the LAST status line: a report that revises its
			// conclusion is taken at its conclusion.
			"last status line wins",
			textEvent("**Status:** APPROVE\nOn reflection:\n**Status:** REQUEST CHANGES\n"),
			nil,
			"REQUEST CHANGES",
		},
		{
			// The reviewer agents' instructions carry the format line
			// verbatim; a report that quotes it must not read as approval.
			"a quoted template line is not a verdict",
			textEvent(
				"**Status:** REQUEST CHANGES\n\nFormat: **Status:** APPROVE | REQUEST CHANGES | NEEDS DISCUSSION\n",
			),
			nil,
			"REQUEST CHANGES",
		},
		{
			// A report can quote a concrete example inside a fenced code
			// block, below its own real verdict. The unsafe direction is
			// toward APPROVE, so a fenced "**Status:** APPROVE" must never
			// overwrite the real "REQUEST CHANGES" verdict above it.
			"a status line inside a fenced code block is not a verdict",
			textEvent(
				"**Status:** REQUEST CHANGES\n\nExample of the format:\n```\n**Status:** APPROVE\n```\n",
			),
			nil,
			"REQUEST CHANGES",
		},
		// A verdict followed by prose (an em dash, or the ASCII-hyphen
		// variant) still counts as that verdict — this is the common,
		// legitimate shape "**Status:** APPROVE — looks good" and must not
		// regress to NO VERDICT. A verdict followed by a letter (APPROVED) or
		// another word (APPROVE NOT) must NOT count, because the unsafe
		// direction is toward approval: a malformed line costs a round rather
		// than fabricating one.
		{"approve with em-dash prose", statusReport("APPROVE — looks good"), nil, "APPROVE"},
		{"approve with ascii-hyphen prose", statusReport("APPROVE - looks good"), nil, "APPROVE"},
		{"approved is not approve", statusReport("APPROVED"), nil, "NO VERDICT"},
		{"approve not is not approve", statusReport("APPROVE NOT"), nil, "NO VERDICT"},
		{
			"request changes with em-dash prose",
			statusReport("REQUEST CHANGES — see below"),
			nil,
			"REQUEST CHANGES",
		},
		{
			"request changes with ascii-hyphen prose",
			statusReport("REQUEST CHANGES - see below"),
			nil,
			"REQUEST CHANGES",
		},
		{
			"request changesx is not request changes",
			statusReport("REQUEST CHANGESX"),
			nil,
			"NO VERDICT",
		},
		{
			"request changes not is not request changes",
			statusReport("REQUEST CHANGES NOT"),
			nil,
			"NO VERDICT",
		},
		{
			"needs discussion with em-dash prose",
			statusReport("NEEDS DISCUSSION — open question"),
			nil,
			"NEEDS DISCUSSION",
		},
		{
			"needs discussion with ascii-hyphen prose",
			statusReport("NEEDS DISCUSSION - open question"),
			nil,
			"NEEDS DISCUSSION",
		},
		{
			"needs discussionx is not needs discussion",
			statusReport("NEEDS DISCUSSIONX"),
			nil,
			"NO VERDICT",
		},
		{
			"needs discussion not is not needs discussion",
			statusReport("NEEDS DISCUSSION NOT"),
			nil,
			"NO VERDICT",
		},
		// Markdown emphasis PLACEMENT must not matter. The reviewer
		// template wraps only "Status:" in "**...**" (statusReport,
		// above); a real run (github-copilot/gpt-5.3-codex, under the
		// document-reviewer agent) instead wrapped the WHOLE line --
		// "**Status: REQUEST CHANGES**" -- which the pre-fix parser never
		// matched at all, silently reporting a genuine REQUEST CHANGES as
		// NO VERDICT. Every placement below must resolve identically to
		// the template form, for all three verdicts.
		{
			"whole-line double-asterisk emphasis (the real bug shape)",
			statusLineReport("**Status: APPROVE**"),
			nil,
			"APPROVE",
		},
		{
			"single-asterisk emphasis around Status only",
			statusLineReport("*Status:* APPROVE"),
			nil,
			"APPROVE",
		},
		{
			"single-asterisk emphasis around the whole line",
			statusLineReport("*Status: APPROVE*"),
			nil,
			"APPROVE",
		},
		{
			"underscore emphasis around Status only",
			statusLineReport("_Status:_ APPROVE"),
			nil,
			"APPROVE",
		},
		{
			"underscore emphasis around the whole line",
			statusLineReport("_Status: APPROVE_"),
			nil,
			"APPROVE",
		},
		{"no emphasis at all", statusLineReport("Status: APPROVE"), nil, "APPROVE"},
		{
			"whole-line double-asterisk emphasis, request changes (the real bug shape)",
			statusLineReport("**Status: REQUEST CHANGES**"),
			nil,
			"REQUEST CHANGES",
		},
		{
			"single-asterisk emphasis around Status only, request changes",
			statusLineReport("*Status:* REQUEST CHANGES"),
			nil,
			"REQUEST CHANGES",
		},
		{
			"single-asterisk emphasis around the whole line, request changes",
			statusLineReport("*Status: REQUEST CHANGES*"),
			nil,
			"REQUEST CHANGES",
		},
		{
			"underscore emphasis around Status only, request changes",
			statusLineReport("_Status:_ REQUEST CHANGES"),
			nil,
			"REQUEST CHANGES",
		},
		{
			"underscore emphasis around the whole line, request changes",
			statusLineReport("_Status: REQUEST CHANGES_"),
			nil,
			"REQUEST CHANGES",
		},
		{
			"no emphasis at all, request changes",
			statusLineReport("Status: REQUEST CHANGES"),
			nil,
			"REQUEST CHANGES",
		},
		{
			"whole-line double-asterisk emphasis, needs discussion",
			statusLineReport("**Status: NEEDS DISCUSSION**"),
			nil,
			"NEEDS DISCUSSION",
		},
		{
			"single-asterisk emphasis around Status only, needs discussion",
			statusLineReport("*Status:* NEEDS DISCUSSION"),
			nil,
			"NEEDS DISCUSSION",
		},
		{
			"single-asterisk emphasis around the whole line, needs discussion",
			statusLineReport("*Status: NEEDS DISCUSSION*"),
			nil,
			"NEEDS DISCUSSION",
		},
		{
			"underscore emphasis around Status only, needs discussion",
			statusLineReport("_Status:_ NEEDS DISCUSSION"),
			nil,
			"NEEDS DISCUSSION",
		},
		{
			"underscore emphasis around the whole line, needs discussion",
			statusLineReport("_Status: NEEDS DISCUSSION_"),
			nil,
			"NEEDS DISCUSSION",
		},
		{
			"no emphasis at all, needs discussion",
			statusLineReport("Status: NEEDS DISCUSSION"),
			nil,
			"NEEDS DISCUSSION",
		},
		// Adversarial: emphasis stripping must not turn a fail-closed case
		// into a false approval. The unsafe direction is toward APPROVE,
		// so these must still fall through to NO VERDICT with whole-line
		// emphasis exactly as they do with the template's placement.
		{
			"whole-line emphasis: approved is not approve",
			statusLineReport("**Status: APPROVED**"),
			nil,
			"NO VERDICT",
		},
		{
			"whole-line emphasis: approve not is not approve",
			statusLineReport("**Status: APPROVE NOT**"),
			nil,
			"NO VERDICT",
		},
		{
			// The reviewer template's own format line, but wrapped in
			// whole-line emphasis instead of the template's "Status:"-only
			// emphasis -- must be refused as a quoted format, not read as
			// any of the three verdicts it lists.
			"the pipe template line wrapped in whole-line emphasis is not a verdict",
			statusLineReport("**Status: APPROVE | REQUEST CHANGES | NEEDS DISCUSSION**"),
			nil,
			"NO VERDICT",
		},
		{
			// A fenced example wrapped in whole-line emphasis, below a
			// real verdict that uses the template's own placement -- the
			// fence skip must still apply regardless of which placement
			// the quoted example inside it uses.
			"a whole-line-emphasis status line inside a fenced code block is not a verdict",
			textEvent(
				"**Status:** REQUEST CHANGES\n\nExample of the format:\n```\n" +
					"**Status: APPROVE**\n```\n",
			),
			nil,
			"REQUEST CHANGES",
		},
		{
			// Dropping "\*\*" from the anchor (so emphasis placement stops
			// mattering, see stripLineEmphasis) also widened the pattern to
			// match a markdown list bullet: stripLineEmphasis removes ALL
			// "*"/"_" on the line, including a leading "* " bullet marker, not
			// just emphasis around "Status:". A bulleted "* **Status:**
			// APPROVE" therefore strips to "  Status: APPROVE" and matches
			// exactly like the template's own unbulleted line.
			//
			// PINNED BEHAVIOR: this counts as a real APPROVE, deliberately.
			// stripLineEmphasis's own doc comment already accepts that it
			// "cannot tell '*' used as emphasis from '*' used as [something
			// else]" -- a list bullet is exactly that other use, and telling
			// it apart from emphasis would need real markdown-structure
			// parsing, which is explicitly out of scope for a line-level
			// normalizer. The reviewer contract's status line is a single
			// declarative line, never a list item, so a bulleted status line
			// showing up at all is already an out-of-contract report; no
			// live reviewer output has been observed to produce one. If that
			// ever changes -- e.g. a report bullets a quoted verdict outside
			// a fence and "last status line wins" lets it override a real
			// verdict above it -- re-narrowing the pattern is a deliberate,
			// separate decision, not a side effect of an unrelated change;
			// this test is what makes that a deliberate edit instead of an
			// accidental regression.
			"a bulleted status line matches like the unbulleted template form",
			statusLineReport("* **Status:** APPROVE"),
			nil,
			"APPROVE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm, _, ocBase := newRepoSetup(t, "feat")
			withReviewers(t)
			scriptOpenCode(t, ocBase, scriptedRun{stdout: tt.stdout, err: tt.err})

			out, err := tm.ReviewRun("", "", ReviewRange{})
			if err != nil {
				t.Fatalf("ReviewRun: %v", err)
			}
			want := "OpenCode default model → " + tt.want
			if out != want {
				t.Errorf("got:\n%s\nwant:\n%s", out, want)
			}
			verifyNoStrayCommands(t, tm)
		})
	}
}

// TestReviewRunClassifiesRealCodexDocumentReviewerCapture is the regression
// test for the bug this fix closes: a real headless run of
// github-copilot/gpt-5.3-codex under the document-reviewer agent wrote
// "**Status: REQUEST CHANGES**" — emphasis wrapping the WHOLE line, not just
// "Status:" the way the agent template does — and the pre-fix parser never
// matched it, silently reporting the round as NO VERDICT instead of the
// blocking REQUEST CHANGES the reviewer actually delivered.
//
// The fixture below is hand-trimmed from the real capture, on disk (not
// committed — it is a 46KB, 43-event NDJSON stream) at
// ~/.cache/devgeta/scratch/codex-doc.json: that capture has 13
// step_start/step_finish pairs, 15 tool_use events, and 2 text events. This
// keeps one step_start/step_finish pair, one tool_use event (to prove event
// types classifyReviewerRun ignores are still tolerated alongside the real
// one), and the exact text event carrying the bug's status line, with the
// surrounding prose shortened.
func TestReviewRunClassifiesRealCodexDocumentReviewerCapture(t *testing.T) {
	capture := strings.Join([]string{
		`{"type":"step_start","part":{"type":"step-start"}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"read","state":{"status":"completed"}}}`,
		textEvent(
			"## Summary\n\nThis is a **code branch review** request, not a document " +
				"review. The blocking concerns already logged in the branch journal " +
				"(`n1`, `n4`) are still valid and unresolved. I recommend " +
				"**REQUEST CHANGES**.\n\n## Recommendation\n\n**Status: REQUEST CHANGES**\n\n" +
				"Settle when answered: `devgeta task review-note --settle --id n4 " +
				`--as fixed|rejected|answered --note "<why>"`,
		),
		`{"type":"step_finish","part":{"type":"step-finish"}}`,
	}, "\n") + "\n"

	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{stdout: capture})

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "OpenCode default model → REQUEST CHANGES"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	verifyNoStrayCommands(t, tm)
}

// An error EVENT is what classifies ERROR, not the text of the message: the
// real binary's message for an unusable model says only "Unexpected server
// error", so any text match would be a guess.
func TestReviewRunErrorOutcomeFromErrorEvent(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2")
	scriptOpenCode(t, ocBase, retried(scriptedRun{
		stdout: errorEvent("UnknownError", "Unexpected server error. Check server logs.") + "\n",
		err:    errors.New("exit status 1"),
	})...)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "openai/gpt-5.2 → ERROR(Unexpected server error. Check server logs.)"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	verifyNoStrayCommands(t, tm)
}

// A nonzero exit with no error event to explain it is still ERROR — the
// wrapper's own message is all there is to report.
func TestReviewRunErrorOutcomeFromNonzeroExitAlone(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, retried(scriptedRun{err: errors.New("exit status 127")})...)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "OpenCode default model → ERROR(opencode run failed: exit status 127)"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

// A long provider message is cut, never reworded, and the cut is marked so a
// reader cannot mistake it for the whole message.
func TestReviewRunErrorReasonIsTruncatedNotReworded(t *testing.T) {
	long := strings.Repeat("provider said something very long. ", 10)
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(
		t,
		ocBase,
		retried(scriptedRun{stdout: errorEvent("UnknownError", long) + "\n"})...,
	)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	reason := strings.TrimSuffix(strings.TrimPrefix(
		strings.Split(out, "\n")[0], "OpenCode default model → ERROR(",
	), ")")
	if !strings.HasSuffix(reason, "…") {
		t.Errorf("expected a truncation marker, got %q", reason)
	}
	if !strings.HasPrefix(long, strings.TrimSuffix(reason, "…")) {
		t.Errorf("the reason must be OpenCode's own text, cut — got %q", reason)
	}
	if len(
		out,
	) != len(
		strings.ReplaceAll(strings.Split(out, "\n")[0], "\n", ""),
	)+len(
		"",
	) {
		t.Errorf("a reviewer's outcome must stay on one line:\n%s", out)
	}
}

// A multi-byte provider message must be cut on a rune boundary, never a byte
// offset: maxReasonLen runes of a 2-byte character puts the naive byte cut
// squarely inside the last rune, which would emit invalid UTF-8 into output
// an agent parses.
func TestReviewRunErrorReasonTruncationIsRuneSafe(t *testing.T) {
	long := strings.Repeat("é", maxReasonLen+10)
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(
		t,
		ocBase,
		retried(scriptedRun{stdout: errorEvent("UnknownError", long) + "\n"})...,
	)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if !utf8.ValidString(out) {
		t.Fatalf("ReviewRun produced invalid UTF-8:\n%s", out)
	}
	reason := strings.TrimSuffix(strings.TrimPrefix(
		strings.Split(out, "\n")[0], "OpenCode default model → ERROR(",
	), ")")
	trimmed := strings.TrimSuffix(reason, "…")
	if got := utf8.RuneCountInString(trimmed); got != maxReasonLen {
		t.Errorf("expected %d runes before the ellipsis, got %d in %q", maxReasonLen, got, trimmed)
	}
}

// Output that is not the event stream at all (no line parses) is a failed
// run, not a silent NO VERDICT.
func TestReviewRunErrorOutcomeWhenNothingParses(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, retried(scriptedRun{stdout: "opencode: command not found\n"})...)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "OpenCode default model → ERROR(opencode: command not found)"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

// Shell noise sharing stdout with the event stream (the Step 0 probe capture
// starts with zoxide's warning) must not turn a good run into an ERROR.
func TestReviewRunIgnoresNonEventNoiseOnStdout(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{
		stdout: "zoxide: detected a possible configuration issue.\n" + statusReport("APPROVE"),
	})

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "OpenCode default model → APPROVE"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

// A reviewer that fails must not cost the reviewers after it: each opinion
// is independent, and the round already paid for them.
func TestReviewRunMidListFailureDoesNotStopRemainingReviewers(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2", "google/gemini-3-pro", "anthropic/claude-opus-4-6")
	runs := []scriptedRun{{stdout: statusReport("REQUEST CHANGES")}}
	// The middle reviewer produces no report, so it costs two attempts before
	// the round moves on to the third.
	runs = append(runs, retried(scriptedRun{
		stdout: errorEvent("UnknownError", "Unexpected server error.") + "\n",
		err:    errors.New("exit status 1"),
	})...)
	runs = append(runs, scriptedRun{stdout: statusReport("APPROVE")})
	scriptOpenCode(t, ocBase, runs...)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := strings.Join([]string{
		"openai/gpt-5.2 → REQUEST CHANGES",
		"google/gemini-3-pro → ERROR(Unexpected server error.)",
		"anthropic/claude-opus-4-6 → APPROVE",
	}, "\n")
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	verifyNoStrayCommands(t, tm)
}

// --- runs that produce no report -------------------------------------------

// stepFinishWithReason renders a step_finish carrying OpenCode's own reason
// for ending that step. "tool-calls" means the model still had work queued;
// "stop" means it was done. (reviewprogress_test.go's stepFinishEvent carries
// the step's cost instead — the two pin different fields of the same event.)
func stepFinishWithReason(reason string) string {
	return `{"type":"step_finish","part":{"type":"step-finish","reason":"` +
		reason + `","cost":0}}`
}

// noReportCapture is the event stream of a run whose agent loop died mid-work:
// steps that start, a tool that runs, and every step_finish — the LAST one
// included — ending on "tool-calls" rather than "stop". Not one text event
// anywhere, and the process still exits 0.
//
// This is the shape a real github-copilot/gpt-5.6-terra round produced, and
// the reason this whole path exists: read only as an event stream it is
// indistinguishable from a reviewer that chose to say nothing.
func noReportCapture() string {
	return strings.Join([]string{
		`{"type":"step_start","part":{"type":"step-start"}}`,
		`{"type":"tool_use","part":{"type":"tool","tool":"read","callID":"c1",` +
			`"state":{"input":{"filePath":"/Users/x/.claude/RTK.md"}}}}`,
		stepFinishWithReason("tool-calls"),
	}, "\n") + "\n"
}

// autoRejectStderr is the real line OpenCode writes — ANSI colour codes and
// all — when a headless run rejects a permission it cannot ask a human about.
// Copied from a captured run, not paraphrased.
const autoRejectStderr = "\x1b[93m\x1b[1m! \x1b[0mpermission requested: " +
	"external_directory (/Users/jair.mendez/.claude/*); auto-rejecting"

// The regression test for the whole bug: a run that exits 0, writes no report,
// and explains itself ONLY on stderr must report that explanation. A bare
// "NO VERDICT" here is what cost a real multi-minute round with no way to tell
// why it failed.
func TestReviewRunNoReportReportsTheStderrReason(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t, "github-copilot/gpt-5.6-terra")
	scriptOpenCode(t, ocBase, retried(scriptedRun{
		stdout: noReportCapture(),
		stderr: autoRejectStderr,
	})...)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	// The "!" is OpenCode's own severity marker, not decoration we added: only
	// the ANSI escapes around it are stripped, and its words are kept verbatim.
	want := "github-copilot/gpt-5.6-terra → NO VERDICT(! permission requested: " +
		"external_directory (/Users/jair.mendez/.claude/*); auto-rejecting)"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	verifyNoStrayCommands(t, tm)
}

// The reason is OpenCode's words, but the ANSI escapes it wraps them in must
// never reach the outcome line: that line is a contract an agent parses, and
// raw escape bytes in it are noise at best.
func TestReviewRunNoReportReasonStripsANSIEscapes(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, retried(scriptedRun{
		stdout: noReportCapture(),
		stderr: autoRejectStderr,
	})...)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("outcome line still carries ANSI escapes: %q", out)
	}
	if !strings.Contains(out, "permission requested: external_directory") {
		t.Errorf("expected OpenCode's own words in the reason, got:\n%s", out)
	}
}

// With nothing on stderr, the last step's reason is the fallback: it cannot
// name the cause, but it does distinguish "the loop ended mid-work" from "the
// reviewer ran and chose to say nothing".
func TestReviewRunNoReportFallsBackToTheStepReason(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, retried(scriptedRun{stdout: noReportCapture()})...)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if !strings.Contains(out, "tool-calls") {
		t.Errorf("expected the last step reason in the outcome, got:\n%s", out)
	}
	if !strings.HasPrefix(out, "OpenCode default model → NO VERDICT(") {
		t.Errorf("expected a NO VERDICT outcome carrying a reason, got:\n%s", out)
	}
}

// A run that ended "stop" having said nothing explains nothing about the
// missing report, so the step reason is not worth printing — but the outcome
// must still carry SOME reason rather than going back to a bare NO VERDICT.
func TestReviewRunNoReportWithNothingToExplainItStillGivesAReason(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, retried(scriptedRun{
		stdout: `{"type":"step_start","part":{"type":"step-start"}}` + "\n" +
			stepFinishWithReason(finishReasonStop) + "\n",
	})...)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "OpenCode default model → NO VERDICT(" + noReportFallbackReason + ")"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

// The retry's whole point: a first attempt that dies with no report is not the
// reviewer's verdict, and a second attempt that works is what the round
// reports.
func TestReviewRunRetriesAReviewerThatProducedNoReport(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(
		t, ocBase,
		scriptedRun{stdout: noReportCapture(), stderr: autoRejectStderr},
		scriptedRun{stdout: statusReport("APPROVE")},
	)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "OpenCode default model → APPROVE"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	verifyNoStrayCommands(t, tm)
}

// A reviewer that WROTE something has said its piece, even when what it wrote
// names no verdict. Re-running it would pay twice to overwrite an opinion that
// was already earned — the fixture's call count is what pins this.
func TestReviewRunDoesNotRetryAReviewerThatReported(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{
		stdout: textEvent("I read the diff and have no comment.") + "\n",
	})

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	// A bare NO VERDICT, with no reason: the reviewer reported, so there is no
	// failure to explain.
	want := "OpenCode default model → NO VERDICT"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

// A reviewer is retried once, never twice: two attempts bound what a
// permanently broken provider can cost a round.
func TestReviewRunRetriesAtMostOnce(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	// Exactly reviewerAttempts entries. A third attempt would trip the
	// fixture's "unexpected extra opencode run" check.
	scriptOpenCode(t, ocBase, retried(scriptedRun{
		stdout: noReportCapture(),
		stderr: autoRejectStderr,
	})...)

	if _, err := tm.ReviewRun("", "", ReviewRange{}); err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
}

// When the retry fails too, the FIRST attempt's outcome is reported. A retry
// that dies with nothing to say must not erase a first attempt that named a
// real cause — that would lose the diagnosis exactly when it is needed.
func TestReviewRunKeepsTheFirstOutcomeWhenTheRetryAlsoFails(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(
		t, ocBase,
		scriptedRun{
			stdout: errorEvent("UnknownError", "Unexpected server error.") + "\n",
			err:    errors.New("exit status 1"),
		},
		// The retry dies with nothing anywhere to explain it.
		scriptedRun{},
	)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "OpenCode default model → ERROR(Unexpected server error.)"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

// A stderr reason is folded onto one line and cut like any other reason: one
// reviewer must stay one line in output an agent parses.
func TestReviewRunNoReportReasonStaysOneLine(t *testing.T) {
	long := strings.Repeat("opencode complained at length. ", 20)
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, retried(scriptedRun{
		stdout: noReportCapture(),
		stderr: "first line\n" + long,
	})...)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if strings.Count(out, "\n") != 0 {
		t.Errorf("one reviewer must stay one line, got:\n%s", out)
	}
	if !strings.HasSuffix(out, "…)") {
		t.Errorf("expected a truncation marker on a long reason, got:\n%s", out)
	}
}

// --- progress lines --------------------------------------------------------

// The nil-ProgressOut default must resolve to os.Stderr — never os.Stdout,
// which would silently merge progress into the parseable payload
// docs/guides/task-design.md governs. A TaskManager built as a bare literal
// (bypassing New()) is exactly the shape every test in this file uses, so
// this is what actually pins the fallback; nothing else in the suite pins
// which stream the default resolves to.
//
// The assertion installs its own sentinel over os.Stderr instead of comparing
// against the real one, because under `go test -json` the harness itself sets
// `os.Stderr = os.Stdout` (testing.go, (*M).Run — it makes the two share one
// pfd so their writes cannot interleave and confuse test2json). Once the two
// variables name the same *os.File, every naive form of this check is broken:
// `got == os.Stdout` is true by construction and fails a correct
// implementation, and both `got == os.Stderr` and an fd comparison against
// fd 2 stop distinguishing the two streams at all — os.Stderr's own Fd() is 1
// under that harness — so they would pass a buggy `return os.Stdout`.
// Swapping in a file that is neither stream makes the check mean the one
// thing that matters under both harnesses: progressWriter reads os.Stderr.
func TestReviewRunProgressWriterDefaultsToStderr(t *testing.T) {
	sentinel, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	realStderr := os.Stderr
	os.Stderr = sentinel
	t.Cleanup(func() {
		os.Stderr = realStderr
		if err := sentinel.Close(); err != nil {
			t.Logf("closing the stderr sentinel: %v", err)
		}
	})

	tm := &TaskManager{}
	if got := tm.progressWriter(); got != io.Writer(sentinel) {
		t.Fatalf("expected the nil ProgressOut default to be os.Stderr, got %v", got)
	}
}

// fixedClock returns a func() time.Time for TaskManager.NowFn that starts at
// start and advances by step on every call after the first. ReviewRun calls
// it twice per reviewer (start, then finish), so consecutive reviewers each
// see exactly step elapsed — a deterministic duration instead of a race
// against the wall clock.
func fixedClock(start time.Time, step time.Duration) func() time.Time {
	next := start
	first := true
	return func() time.Time {
		if first {
			first = false
			return next
		}
		next = next.Add(step)
		return next
	}
}

// Progress lines go to ProgressOut, one per reviewer as it starts and one as
// it resolves, each carrying its position, label, outcome, and elapsed time
// — and the returned string (the stdout contract docs/guides/task-design.md
// governs) is completely unaffected. This is the regression that matters:
// every other test in this file asserts that exact returned string byte for
// byte, with newRepoSetup's default ProgressOut (io.Discard, so the suite
// stays quiet — see TestReviewRunProgressWriterDefaultsToStderr for the
// os.Stderr default that default stands in for), so their continuing to pass
// already proves the same thing from the other side.
func TestReviewRunWritesProgressLinesPerReviewer(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2", "google/gemini-3-pro")
	scriptOpenCode(
		t, ocBase,
		scriptedRun{stdout: statusReport("REQUEST CHANGES")},
		scriptedRun{stdout: statusReport("APPROVE")},
	)

	var progress bytes.Buffer
	tm.ProgressOut = &progress
	tm.NowFn = fixedClock(time.Unix(0, 0), 1500*time.Millisecond)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	wantOut := "openai/gpt-5.2 → REQUEST CHANGES\ngoogle/gemini-3-pro → APPROVE"
	if out != wantOut {
		t.Errorf("stdout contract changed:\ngot:\n%s\nwant:\n%s", out, wantOut)
	}
	wantProgress := strings.Join([]string{
		"[1/2] openai/gpt-5.2: running",
		"[1/2] openai/gpt-5.2: REQUEST CHANGES (1.5s)",
		"[2/2] google/gemini-3-pro: running",
		"[2/2] google/gemini-3-pro: APPROVE (1.5s)",
		"",
	}, "\n")
	if progress.String() != wantProgress {
		t.Errorf("progress got:\n%s\nwant:\n%s", progress.String(), wantProgress)
	}
}

// The unset-reviewers case: the progress label must match the final output's
// label exactly ("OpenCode default model"), not the empty model string.
func TestReviewRunProgressUsesDefaultModelLabel(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("APPROVE")})

	var progress bytes.Buffer
	tm.ProgressOut = &progress
	tm.NowFn = fixedClock(time.Unix(0, 0), time.Second)

	if _, err := tm.ReviewRun("", "", ReviewRange{}); err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "[1/1] OpenCode default model: running\n[1/1] OpenCode default model: APPROVE (1s)\n"
	if progress.String() != want {
		t.Errorf("progress got:\n%s\nwant:\n%s", progress.String(), want)
	}
}

// A reviewer that fails still gets both its progress lines, and the
// reviewer after it still gets its own — a failure must never go quiet or
// stop the rest.
func TestReviewRunProgressContinuesAfterAReviewerFails(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2", "google/gemini-3-pro")
	runs := retried(scriptedRun{err: errors.New("exit status 1")})
	runs = append(runs, scriptedRun{stdout: statusReport("APPROVE")})
	scriptOpenCode(t, ocBase, runs...)

	var progress bytes.Buffer
	tm.ProgressOut = &progress
	tm.NowFn = fixedClock(time.Unix(0, 0), time.Second)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("a failed reviewer is an outcome, not a command error: %v", err)
	}
	if !strings.Contains(out, "ERROR(") {
		t.Errorf("expected an ERROR outcome in the stdout contract, got:\n%s", out)
	}
	// The failed reviewer reads "(2s)" rather than "(1s)" purely because
	// fixedClock advances on every call and the retry announcement stamps
	// lastLine, costing one extra tick. Its elapsed time is still measured
	// from construction to finished() — the retry does not double it.
	wantProgress := strings.Join([]string{
		"[1/2] openai/gpt-5.2: running",
		"[1/2] openai/gpt-5.2: ERROR(opencode run failed: exit status 1) — no report, retrying once",
		"[1/2] openai/gpt-5.2: ERROR(opencode run failed: exit status 1) (2s)",
		"[2/2] google/gemini-3-pro: running",
		"[2/2] google/gemini-3-pro: APPROVE (1s)",
		"",
	}, "\n")
	if progress.String() != wantProgress {
		t.Errorf("progress got:\n%s\nwant:\n%s", progress.String(), wantProgress)
	}
}

// --- reviewer list resolution --------------------------------------------

func TestReviewRunConfiguredReviewersPinTheirModels(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2", "google/gemini-3-pro")
	scriptOpenCode(
		t,
		ocBase,
		scriptedRun{
			stdout: statusReport("APPROVE"),
			onCall: func(t *testing.T, c commands.CommandParams) {
				assertModelFlag(t, c, "openai/gpt-5.2")
			},
		},
		scriptedRun{
			stdout: statusReport("APPROVE"),
			onCall: func(t *testing.T, c commands.CommandParams) {
				assertModelFlag(t, c, "google/gemini-3-pro")
			},
		},
	)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "openai/gpt-5.2 → APPROVE\ngoogle/gemini-3-pro → APPROVE"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

// With review.reviewers unset there is exactly one run, with no -m flag at
// all — the line names the condition because there is no model name to name.
func TestReviewRunUnsetReviewersRunsOpenCodeDefaultModel(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{
		stdout: statusReport("APPROVE"),
		onCall: func(t *testing.T, c commands.CommandParams) {
			if slices.Contains(c.Args, "-m") {
				t.Errorf("expected no -m flag with reviewers unset, got %v", c.Args)
			}
		},
	})

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if out != "OpenCode default model → APPROVE" {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// A model id repeated in review.reviewers is ONE run, keeping the first
// occurrence's position. In branch mode running it twice only wastes a paid
// round on a second copy of one model's opinion; in range mode it was silent
// data loss, because both runs derive the same report filename from the same
// label — the second overwrote the first while both output lines still named
// that one path, so the first line pointed at a report describing a different
// verdict. Both modes are covered here because the dedup is one rule, and the
// range half is the one that was actually broken.
func TestReviewRunRunsADuplicatedModelOnce(t *testing.T) {
	const (
		dupModel   = "openai/gpt-5.2"
		otherModel = "google/gemini-3-pro"
	)

	// The duplicate sits LAST, so this also pins the position rule: the kept run
	// is the first occurrence, ahead of the model configured between the two.
	configured := []string{dupModel, otherModel, dupModel}

	t.Run("branch mode", func(t *testing.T) {
		tm, _, ocBase := newRepoSetup(t, "feat")
		withReviewers(t, configured...)
		// Two scripted attempts for three configured entries: a third launch
		// fails the fixture's call-count check, which is how a lost dedup gets
		// caught even if the printed lines somehow still looked right.
		scriptOpenCode(
			t, ocBase,
			scriptedRun{
				stdout: statusReport("REQUEST CHANGES"),
				onCall: func(t *testing.T, c commands.CommandParams) {
					assertModelFlag(t, c, dupModel)
				},
			},
			scriptedRun{
				stdout: statusReport("APPROVE"),
				onCall: func(t *testing.T, c commands.CommandParams) {
					assertModelFlag(t, c, otherModel)
				},
			},
		)

		out, err := tm.ReviewRun("", "", ReviewRange{})
		if err != nil {
			t.Fatalf("ReviewRun: %v", err)
		}
		want := dupModel + " → REQUEST CHANGES\n" + otherModel + " → APPROVE"
		if out != want {
			t.Errorf("got:\n%s\nwant:\n%s", out, want)
		}
	})

	t.Run("range mode", func(t *testing.T) {
		tm, ocBase, rng := newRangeSetup(t, "feat")
		withReviewers(t, configured...)
		scriptOpenCode(
			t, ocBase,
			scriptedRun{stdout: fullReport("gpt found a race", "REQUEST CHANGES")},
			scriptedRun{stdout: fullReport("gemini found nothing blocking", "APPROVE")},
		)

		out, err := tm.ReviewRun("", "", rng)
		if err != nil {
			t.Fatalf("ReviewRun: %v", err)
		}
		wants := []struct{ prefix, body, verdict string }{
			{dupModel + " → REQUEST CHANGES", "gpt found a race", "REQUEST CHANGES"},
			{otherModel + " → APPROVE", "gemini found nothing blocking", "APPROVE"},
		}
		lines := strings.Split(out, "\n")
		if len(lines) != len(wants) {
			// Not fatal: the report-directory check below is what names the
			// actual damage (more lines than files), and it is worth reporting
			// alongside the line count rather than instead of it.
			t.Errorf("expected one line per distinct model, got:\n%s", out)
		}

		// One file per line. A duplicate run overwrites its twin's report, so a
		// line count above the file count IS the data loss.
		entries, err := os.ReadDir(rng.ReportDir)
		if err != nil {
			t.Fatalf("reading the report directory: %v", err)
		}
		if len(entries) != len(lines) {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			t.Errorf(
				"%d verdict line(s) but %d report file(s) %v — a report was overwritten",
				len(lines), len(entries), names,
			)
		}
		if len(lines) < len(wants) {
			return
		}

		// And each line's report says what that line says — the pairing the
		// overwrite destroyed.
		seen := make(map[string]bool, len(wants))
		for i, want := range wants {
			if !strings.HasPrefix(lines[i], want.prefix+reportField) {
				t.Errorf("line %d: got %q, want %q then a report field", i+1, lines[i], want.prefix)
				continue
			}
			path := reportPathFrom(t, lines[i])
			if seen[path] {
				t.Errorf("line %d: report path %q already belongs to an earlier line", i+1, path)
			}
			seen[path] = true
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading the persisted report: %v", err)
			}
			wantText := fullReportText(want.body, want.verdict)
			if string(data) != wantText {
				t.Errorf("line %d: persisted report is\n%s\nwant\n%s", i+1, data, wantText)
			}
		}
	})
}

func assertModelFlag(t *testing.T, c commands.CommandParams, want string) {
	t.Helper()
	i := slices.Index(c.Args, "-m")
	if i < 0 || i+1 >= len(c.Args) {
		t.Fatalf("expected -m %s in %v", want, c.Args)
	}
	if c.Args[i+1] != want {
		t.Errorf("expected -m %s, got -m %s", want, c.Args[i+1])
	}
}

// --- the reviewer agent and its prompt ------------------------------------

func TestReviewRunLaunchesTheSelectedReviewerAgent(t *testing.T) {
	tests := []struct{ flag, wantAgent string }{
		{"", "code-reviewer"}, // unset means the default reviewer
		{"code", "code-reviewer"},
		{"document", "document-reviewer"},
		{"skill", "skill-reviewer"},
	}
	for _, tt := range tests {
		t.Run("reviewer="+tt.flag, func(t *testing.T) {
			tm, _, ocBase := newRepoSetup(t, "feat")
			withReviewers(t)
			scriptOpenCode(t, ocBase, scriptedRun{
				stdout: statusReport("APPROVE"),
				onCall: func(t *testing.T, c commands.CommandParams) {
					i := slices.Index(c.Args, "--agent")
					if i < 0 || i+1 >= len(c.Args) || c.Args[i+1] != tt.wantAgent {
						t.Errorf("expected --agent %s, got %v", tt.wantAgent, c.Args)
					}
					// The prompt has exactly one definition, shared with the
					// tmux-pane launch path.
					if last := c.Args[len(c.Args)-1]; last != worktree.ReviewPrompt {
						t.Errorf("expected the shared review prompt, got %q", last)
					}
					if c.Timeout <= 0 {
						t.Error("a headless reviewer run must be bounded by a timeout")
					}
				},
			})

			if _, err := tm.ReviewRun(tt.flag, "", ReviewRange{}); err != nil {
				t.Fatalf("ReviewRun: %v", err)
			}
		})
	}
}

// --- --note ---------------------------------------------------------------

// --note reaches every reviewer, appended to the shared prompt rather than
// replacing it, and the prompt still opens with the same fixed sentence — a
// note must add to what the reviewer is asked, never substitute for it.
func TestReviewRunNoteReachesEveryReviewer(t *testing.T) {
	const note = "focus on docs/spec.md, I only changed wording there"
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2", "google/gemini-3-pro")

	check := func(t *testing.T, c commands.CommandParams) {
		prompt := c.Args[len(c.Args)-1]
		if !strings.HasPrefix(prompt, worktree.ReviewPrompt) {
			t.Errorf("the note must be appended to the shared prompt, got %q", prompt)
		}
		if !strings.Contains(prompt, note) {
			t.Errorf("expected the note in the prompt, got %q", prompt)
		}
		// The framing is what stops "focus on X" from being read as "review
		// only X" — a narrowed review that still reports a whole-branch
		// verdict is the failure this guards against.
		if !strings.Contains(prompt, "not a narrower scope") {
			t.Errorf("expected the note to be framed as emphasis, got %q", prompt)
		}
	}
	scriptOpenCode(
		t, ocBase,
		scriptedRun{stdout: statusReport("APPROVE"), onCall: check},
		scriptedRun{stdout: statusReport("APPROVE"), onCall: check},
	)

	if _, err := tm.ReviewRun("", note, ReviewRange{}); err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
}

// A whitespace-only --note is refused, not dropped: the caller meant to say
// something to the reviewers, and a silently ignored note produces a round
// that looks exactly like one that carried it.
func TestReviewRunBlankNoteRefused(t *testing.T) {
	tm, root, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase) // no run may happen

	_, err := tm.ReviewRun("", "   \n\t", ReviewRange{})
	if err == nil {
		t.Fatal("expected a refusal for a blank --note")
	}
	if !strings.Contains(err.Error(), "--note") {
		t.Errorf("expected the refusal to name the flag, got: %v", err)
	}
	assertNoReviewFilesWritten(t, root)
}

// An unknown reviewer is refused against the shared registry, before
// anything is launched.
func TestReviewRunUnknownReviewerRefused(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase)

	_, err := tm.ReviewRun("architecture", "", ReviewRange{})
	if err == nil {
		t.Fatal("expected an error for an unregistered reviewer")
	}
	for _, want := range []string{"architecture", "code", "document", "skill"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in the error, got: %v", want, err)
		}
	}
}

// --- the branch guard -----------------------------------------------------

func TestReviewRunProceedsOnANamedNonDefaultBranch(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "fix/retry-context")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("APPROVE")})

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun on a feature branch must proceed, got: %v", err)
	}
	if out != "OpenCode default model → APPROVE" {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestReviewRunRefusesOnTheDefaultBranch(t *testing.T) {
	tm, root, ocBase := newRepoSetup(t, "main")
	withReviewers(t)
	scriptOpenCode(t, ocBase) // no run may happen

	_, err := tm.ReviewRun("", "", ReviewRange{})
	if err == nil {
		t.Fatal("expected a refusal on the default branch")
	}
	if !strings.Contains(err.Error(), "git switch -c") {
		t.Errorf("the refusal must carry the fix, got: %v", err)
	}
	assertNoReviewFilesWritten(t, root)
}

// The case a `current != defaultBranch` check silently lets through: a
// detached HEAD has no branch name at all, and `git branch --show-current`
// reports that by printing nothing and exiting 0.
func TestReviewRunRefusesOnDetachedHead(t *testing.T) {
	tm, root, ocBase := newRepoSetup(t, "")
	withReviewers(t)
	scriptOpenCode(t, ocBase) // no run may happen

	_, err := tm.ReviewRun("", "", ReviewRange{})
	if err == nil {
		t.Fatal("expected a refusal on a detached HEAD")
	}
	if !strings.Contains(err.Error(), "detached") {
		t.Errorf("the refusal must name the detached HEAD, got: %v", err)
	}
	if !strings.Contains(err.Error(), "git switch -c") {
		t.Errorf("the refusal must carry the fix, got: %v", err)
	}
	assertNoReviewFilesWritten(t, root)
}

// A named, non-default branch that changes NOTHING — no commits ahead and a
// clean working tree — has nothing to review: the third refusal, distinct
// from the other two in that HEAD is perfectly fine.
func TestReviewRunRefusesWhenBranchChangesNothing(t *testing.T) {
	tm, root, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	withAheadCount(t, tm, 0)
	scriptOpenCode(t, ocBase) // no run may happen

	_, err := tm.ReviewRun("", "", ReviewRange{})
	if err == nil {
		t.Fatal("expected a refusal when the branch has no commits and a clean tree")
	}
	if !strings.Contains(err.Error(), "feat") {
		t.Errorf("expected the branch name in the refusal, got: %v", err)
	}
	// The refusal must say BOTH halves were checked. "No commits ahead" alone
	// reads as "commit your work" — which is exactly the dead end this change
	// removed, since uncommitted work is now reviewable.
	for _, want := range []string{"no commits ahead", "no uncommitted changes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the refusal to state %q, got: %v", want, err)
		}
	}
	assertNoReviewFilesWritten(t, root)
}

// Uncommitted work alone is enough to review: no commits ahead, but a dirty
// working tree. This is the case that used to be refused, and refusing it
// forced a commit just to get a review (ADR-0019).
func TestReviewRunProceedsOnUncommittedWorkAlone(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	withAheadCount(t, tm, 0)
	withDirtyWorktree(t, tm, " M internal/x.go\n")
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("REQUEST CHANGES")})

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("a dirty tree with no commits must still be reviewable, got: %v", err)
	}
	if out != "OpenCode default model → REQUEST CHANGES" {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// An untracked file is uncommitted work too: `git status --porcelain` reports
// it as "??", so a branch whose only change is a brand-new file is reviewable.
func TestReviewRunProceedsOnUntrackedWorkAlone(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	withAheadCount(t, tm, 0)
	withDirtyWorktree(t, tm, "?? docs/draft.md\n")
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("APPROVE")})

	if _, err := tm.ReviewRun("", "", ReviewRange{}); err != nil {
		t.Fatalf("an untracked-only branch must still be reviewable, got: %v", err)
	}
}

// The dirty check fails open for the same reason the ahead count does: the
// guard saves cost, it is not a safety property, so "cannot tell whether the
// tree is dirty" must never be treated as "confirmed empty".
func TestReviewRunProceedsWhenDirtinessCannotBeDetermined(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	withAheadCount(t, tm, 0)
	withStatusFailure(t, tm)
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("APPROVE")})

	if _, err := tm.ReviewRun("", "", ReviewRange{}); err != nil {
		t.Fatalf("an unresolvable status check must fail open, not block the round: %v", err)
	}
}

// --- the branch must stay put for the whole round ------------------------

// The branch decides which tree is reviewed AND which journal the findings go
// to, so it is a precondition of the whole round, not just its first moment.
// This is a real incident, not a hypothetical: HEAD was switched to the default
// branch one minute into a live round, and that round's findings landed in a
// `main` journal — the file ADR-0018 exists to prevent.
func TestReviewRunAbortsWhenHeadMovesMidRound(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2", "google/gemini-3-pro")
	// Only ONE reviewer may run: the round must not launch the second one
	// against a tree nobody asked it to review. scriptOpenCode fails the test
	// if a second run happens.
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("APPROVE")})
	withBranchSwitchAfter(t, tm, 2, "main")

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err == nil {
		t.Fatal("expected the round to abort when HEAD moved mid-round")
	}
	if out != "" {
		t.Errorf("a round that cannot be trusted must report no verdicts, got:\n%s", out)
	}
	// The error has to carry everything needed to recover: both branches, which
	// reviewer was running, and where the findings actually went.
	for _, want := range []string{"feat", "main", "openai/gpt-5.2", "git switch feat", "review-notes"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in the error, got: %v", want, err)
		}
	}
}

// A detached HEAD mid-round is the same failure and must read as one — "moved
// to \"\"" would tell the reader nothing.
func TestReviewRunAbortsWhenHeadDetachesMidRound(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("APPROVE")})
	withBranchSwitchAfter(t, tm, 2, "")

	_, err := tm.ReviewRun("", "", ReviewRange{})
	if err == nil {
		t.Fatal("expected the round to abort when HEAD detached mid-round")
	}
	if !strings.Contains(err.Error(), "detached HEAD") {
		t.Errorf("expected the detached case named plainly, got: %v", err)
	}
}

// The check fails OPEN when git cannot answer: an unreadable HEAD is not
// evidence that anything moved, and throwing away a paid-for round on "cannot
// tell" would cost real money to prevent a problem that may not exist.
func TestReviewRunContinuesWhenTheBranchRecheckFails(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("APPROVE")})

	gitBase, ok := tm.Git.Base.(*commands.MockBaseCommand)
	if !ok {
		t.Fatalf("expected a mock git base, got %T", tm.Git.Base)
	}
	orig := gitBase.ExecCommandFn
	calls := 0
	gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if slices.Contains(c.Args, "--show-current") {
			calls++
			if calls >= 2 { // the re-check, not the round's opening resolution
				return "", "fatal: not a git repository", errors.New("exit status 128")
			}
		}
		return orig(c)
	}

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("an unreadable HEAD must not abort the round: %v", err)
	}
	if out != "OpenCode default model → APPROVE" {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// A round where nothing moves must not pay any attention to this: every
// configured reviewer still runs and every verdict is still reported.
func TestReviewRunUnaffectedWhenTheBranchStaysPut(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2", "google/gemini-3-pro")
	scriptOpenCode(
		t, ocBase,
		scriptedRun{stdout: statusReport("REQUEST CHANGES")},
		scriptedRun{stdout: statusReport("APPROVE")},
	)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "openai/gpt-5.2 → REQUEST CHANGES\ngoogle/gemini-3-pro → APPROVE"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

// With commits ahead, the guard is already satisfied and must not spend a
// second git call asking about the working tree — the answer cannot change
// the outcome.
func TestReviewRunSkipsTheDirtyCheckWhenCommitsExist(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	withAheadCount(t, tm, 2)
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("APPROVE")})

	if _, err := tm.ReviewRun("", "", ReviewRange{}); err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	gitBase, ok := tm.Git.Base.(*commands.MockBaseCommand)
	if !ok {
		t.Fatalf("expected a mock git base, got %T", tm.Git.Base)
	}
	for _, call := range gitBase.ExecCommandCalls {
		if slices.Contains(call.Args, "status") {
			t.Errorf("the dirty check must be skipped when commits exist, got: %v", call.Args)
		}
	}
}

// A branch that does have commits ahead of the default branch proceeds,
// regardless of how many.
func TestReviewRunProceedsWhenBranchHasCommittedDiff(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	withAheadCount(t, tm, 1)
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("APPROVE")})

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun on a branch with a committed diff must proceed, got: %v", err)
	}
	if out != "OpenCode default model → APPROVE" {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// When the ahead/behind comparison itself cannot be made — no local
// refs/remotes/origin/<default> ref, as in a repo with no remote, a shallow
// or --single-branch clone, or a default branch never fetched —
// checkBranchHasCommittedDiff must fail OPEN and let the round proceed,
// never surface git's raw "unknown revision" error or block the round on
// "cannot tell". Before the fix, this failed: ReviewRun returned the raw
// rev-list error and no reviewer ran at all.
func TestReviewRunProceedsWhenAheadCountCannotBeDetermined(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	withRevListFailure(t, tm)
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("APPROVE")})

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf(
			"an unresolvable ahead/behind comparison must fail open, not block the round: %v",
			err,
		)
	}
	if out != "OpenCode default model → APPROVE" {
		t.Errorf("unexpected output:\n%s", out)
	}
}

// assertNoReviewFilesWritten proves a refusal cost nothing: no journal, no
// snapshot, not even the review directory.
func assertNoReviewFilesWritten(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, ".git", "devgeta", "review")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	if len(entries) > 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("a refused run must write nothing, found: %v", names)
	}
}

// --- the round-start snapshot ---------------------------------------------

// The first-review path, and the one most easily skipped: with no journal on
// disk at round start, the snapshot is still written — "empty at round
// start" is a real state a later reviewer must be able to read — AND a
// reviewer reading that snapshot does not see an entry a peer opened this
// same round. That second half is the exact bug this test guards against: an
// implementation that skips the snapshot write when Load returns an empty
// journal would still pass if only existence/removal were checked, because
// the live journal and a (never-written) snapshot would look identical to
// reviewer 2 in that case.
func TestReviewRunWritesSnapshotEvenWithNoJournal(t *testing.T) {
	t.Setenv(ReviewJournalSnapshotEnvVar, "")

	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2", "google/gemini-3-pro")

	var seenPath string
	scriptOpenCode(
		t, ocBase,
		scriptedRun{
			stdout: statusReport("REQUEST CHANGES"),
			onCall: func(t *testing.T, c commands.CommandParams) {
				seenPath = snapshotPointer(t, c)
				if _, err := os.Stat(seenPath); err != nil {
					t.Errorf("the snapshot must exist while a reviewer runs: %v", err)
				}
				// Stand in for reviewer 1's own `review-note --open` process,
				// which writes to the LIVE journal.
				if _, err := tm.ReviewNoteOpen("", "", "", "reviewer-1 finding"); err != nil {
					t.Fatalf("reviewer 1's write: %v", err)
				}
			},
		},
		scriptedRun{
			stdout: statusReport("APPROVE"),
			onCall: func(t *testing.T, c commands.CommandParams) {
				data, err := os.ReadFile(snapshotPointer(t, c))
				if err != nil {
					t.Fatalf("reading the snapshot reviewer 2 was pointed at: %v", err)
				}
				if strings.Contains(string(data), "reviewer-1 finding") {
					t.Errorf(
						"a peer's same-round entry must not be in the snapshot, "+
							"even when the round started with no journal on disk:\n%s",
						data,
					)
				}
			},
		},
	)

	if _, err := tm.ReviewRun("", "", ReviewRange{}); err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if seenPath == "" {
		t.Fatal("the reviewer was never pointed at a snapshot")
	}
	if _, err := os.Stat(seenPath); !os.IsNotExist(err) {
		t.Errorf("the snapshot must be removed at round end, stat gave: %v", err)
	}
	// The pointer is child-only: devgeta's own process must be untouched, or
	// it would leak into unrelated review-notes calls.
	if got := os.Getenv(ReviewJournalSnapshotEnvVar); got != "" {
		t.Errorf("review-run must not set %s on its own process, got %q",
			ReviewJournalSnapshotEnvVar, got)
	}
}

// The isolation the snapshot exists for: what reviewer 1 opens mid-round is
// invisible to reviewer 2, which is still reading round-start state.
func TestReviewRunSnapshotHidesWhatAPeerWroteThisRound(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2", "google/gemini-3-pro")
	if _, err := tm.ReviewNoteOpen("", "", "", "round-start finding"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	scriptOpenCode(
		t, ocBase,
		scriptedRun{
			stdout: statusReport("REQUEST CHANGES"),
			onCall: func(t *testing.T, c commands.CommandParams) {
				// Stand in for the reviewer's own `review-note --open`
				// process, which writes to the LIVE journal.
				if _, err := tm.ReviewNoteOpen("", "", "", "reviewer-1 finding"); err != nil {
					t.Fatalf("reviewer 1's write: %v", err)
				}
			},
		},
		scriptedRun{
			stdout: statusReport("APPROVE"),
			onCall: func(t *testing.T, c commands.CommandParams) {
				data, err := os.ReadFile(snapshotPointer(t, c))
				if err != nil {
					t.Fatalf("reading the snapshot reviewer 2 was pointed at: %v", err)
				}
				if !strings.Contains(string(data), "round-start finding") {
					t.Errorf("the snapshot must carry round-start state:\n%s", data)
				}
				if strings.Contains(string(data), "reviewer-1 finding") {
					t.Errorf("a peer's same-round entry must not be in the snapshot:\n%s", data)
				}
			},
		},
	)

	if _, err := tm.ReviewRun("", "", ReviewRange{}); err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	// Ids keep advancing in the LIVE journal while the reads stay frozen: the
	// round-start entry and the one reviewer 1 wrote mid-round are both there,
	// each with its own real id.
	notes, err := tm.ReviewNotes("", "", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	for _, want := range []string{"n1", "n2", "round-start finding", "reviewer-1 finding"} {
		if !strings.Contains(notes, want) {
			t.Errorf("expected %q in the live journal, got:\n%s", want, notes)
		}
	}
}

// Every reviewer in a round reads the same snapshot: one round, one frozen
// view.
func TestReviewRunPointsEveryReviewerAtTheSameSnapshot(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2", "google/gemini-3-pro")

	var pointers []string
	record := func(t *testing.T, c commands.CommandParams) {
		pointers = append(pointers, snapshotPointer(t, c))
	}
	scriptOpenCode(
		t, ocBase,
		scriptedRun{stdout: statusReport("APPROVE"), onCall: record},
		scriptedRun{stdout: statusReport("APPROVE"), onCall: record},
	)

	if _, err := tm.ReviewRun("", "", ReviewRange{}); err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if len(pointers) != 2 || pointers[0] != pointers[1] {
		t.Fatalf("expected one shared snapshot pointer, got %v", pointers)
	}
	// It is the path the journal package owns, not one review-run invented.
	want, err := reviewjournal.New(tm.Git).SnapshotPathFor("", "feat")
	if err != nil {
		t.Fatalf("SnapshotPathFor: %v", err)
	}
	if pointers[0] != want {
		t.Errorf("expected the snapshot at %s, got %s", want, pointers[0])
	}
}

// A crashed reviewer must not leave a snapshot behind: the next round would
// still overwrite it, but a stale file is exactly the state this command is
// responsible for never producing.
func TestReviewRunRemovesSnapshotAfterAReviewerFails(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)

	var seenPath string
	scriptOpenCode(t, ocBase, retried(scriptedRun{
		err: errors.New("exit status 1"),
		onCall: func(t *testing.T, c commands.CommandParams) {
			seenPath = snapshotPointer(t, c)
		},
	})...)

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("a failed reviewer is an outcome, not a command error: %v", err)
	}
	if !strings.Contains(out, "ERROR(") {
		t.Errorf("expected an ERROR outcome, got:\n%s", out)
	}
	if _, err := os.Stat(seenPath); !os.IsNotExist(err) {
		t.Errorf("the snapshot must be removed even after a failure, stat gave: %v", err)
	}
}

// snapshotPointer pulls the child-only snapshot pointer out of a recorded
// command's environment overlay.
func snapshotPointer(t *testing.T, c commands.CommandParams) string {
	t.Helper()
	prefix := ReviewJournalSnapshotEnvVar + "="
	for _, kv := range c.Env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix)
		}
	}
	t.Fatalf(
		"expected %s in the reviewer's environment, got %v",
		ReviewJournalSnapshotEnvVar,
		c.Env,
	)
	return ""
}

// --- stdout carries verdicts and nothing else ------------------------------

// review-run's stdout is one line per reviewer, full stop: no open ids, no
// findings, no reviewer prose. The journal is where a finding lives, and
// `review-notes` is what reads it — printing any of it here duplicated that
// output into every round for a caller that has to re-read the journal anyway.
//
// The journal below is deliberately non-empty (two open entries and one
// settled), so a regression that reintroduces a journal tail has something to
// print and this test fails instead of passing vacuously.
func TestReviewRunPrintsOnlyVerdictLines(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	for _, note := range []string{"first", "second", "third"} {
		if _, err := tm.ReviewNoteOpen("", "", "", note); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	if _, err := tm.ReviewNoteSettle("", "", "n2", "fixed", "", "done"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("REQUEST CHANGES")})

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if out != "OpenCode default model → REQUEST CHANGES" {
		t.Errorf("got:\n%s\nwant only the verdict line", out)
	}
	// Named explicitly rather than left to the equality check above, so a
	// failure says WHICH journal content leaked back into stdout.
	for _, unwanted := range []string{"open:", "n1", "n3", "first", "third"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("stdout must not carry journal content, found %q in:\n%s", unwanted, out)
		}
	}
}

// --- explicit-range mode (ADR-0022 §5) --------------------------------------

const (
	// The two commit-ish values a range-mode test passes, and the SHAs the
	// fixture resolves them to. They are deliberately DIFFERENT strings: what
	// reaches the prompt has to be the resolved commit, never the caller's
	// spelling of it, or two ticks of the same review describe two different
	// states under one name.
	testRangeBaseRef = "merge-base-of-213"
	testRangeHeadRef = "refs/devgeta/pr/213/head"
	testRangeBaseSHA = "4a1c2ef9b0d1e2f3a4b5c6d7e8f90123456789ab"
	testRangeHeadSHA = "9f2c1abcdef0123456789abcdef0123456789abc"

	// The PR-scoped journal key. It carries slashes on purpose: a key is not a
	// branch name, and the journal's encoder is what makes one a filename.
	testRangeJournalKey = "pr/acme/web/213"
)

// withResolvableCommits answers `rev-parse --verify <ref>^{commit}` from refs
// and fails for anything else — the exact shape a commit this clone never
// fetched has, which is range mode's likeliest bad input. newRepoSetup's
// fixture answers that call with nothing, so a range-mode test has to say which
// commits the clone actually has; every other git call keeps answering exactly
// as that fixture does.
func withResolvableCommits(t *testing.T, tm *TaskManager, refs map[string]string) {
	t.Helper()
	gitBase, ok := tm.Git.Base.(*commands.MockBaseCommand)
	if !ok {
		t.Fatalf("expected a mock git base, got %T", tm.Git.Base)
	}
	orig := gitBase.ExecCommandFn
	gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if slices.Contains(c.Args, "rev-parse") && slices.Contains(c.Args, "--verify") {
			ref := strings.TrimSuffix(c.Args[len(c.Args)-1], "^{commit}")
			sha, found := refs[ref]
			if !found {
				return "", "fatal: Needed a single revision", errors.New("exit status 128")
			}
			return sha + "\n", "", nil
		}
		return orig(c)
	}
}

// newRangeSetup is newRepoSetup plus the two commits every range-mode test
// reviews and the flag group that selects them.
//
// branch is whatever the checkout happens to be on — which range mode must not
// care about at all, which is why the tests below pass a feature branch, the
// default branch, and a detached HEAD to the same fixture. The report directory
// is deliberately NOT created here: creating it is the command's job.
func newRangeSetup(
	t *testing.T,
	branch string,
) (*TaskManager, *commands.MockBaseCommand, ReviewRange) {
	t.Helper()
	tm, _, ocBase := newRepoSetup(t, branch)
	withResolvableCommits(t, tm, map[string]string{
		testRangeBaseRef: testRangeBaseSHA,
		testRangeHeadRef: testRangeHeadSHA,
	})
	return tm, ocBase, ReviewRange{
		Base:      testRangeBaseRef,
		Head:      testRangeHeadRef,
		Journal:   testRangeJournalKey,
		ReportDir: filepath.Join(t.TempDir(), "reports"),
	}
}

// fullReport is a reviewer's whole report — prose, then the contract's verdict
// line — rather than statusReport's verdict alone. Range mode must persist all
// of it: the verdict is already on the output line, and the findings, evidence
// and strengths around it are the part nothing else keeps.
func fullReport(body, verdict string) string {
	return strings.Join([]string{
		`{"type":"step_start","part":{"type":"step-start"}}`,
		textEvent(fullReportText(body, verdict)),
		`{"type":"step_finish","part":{"type":"step-finish","reason":"stop"}}`,
	}, "\n") + "\n"
}

// fullReportText is the exact text those events concatenate to, so a test can
// assert the persisted file byte for byte instead of searching it for a phrase.
func fullReportText(body, verdict string) string {
	return "### Findings\n\n" + body + "\n\n**Status:** " + verdict + "\n"
}

// reportPathFrom pulls the report path out of one verdict line, reading the
// field from the right — an ERROR(...) outcome carries OpenCode's own words, so
// the report label is only unambiguous as the line's last field.
func reportPathFrom(t *testing.T, line string) string {
	t.Helper()
	i := strings.LastIndex(line, reportField)
	if i < 0 {
		t.Fatalf(
			"expected a %q field on the verdict line, got %q",
			strings.TrimSpace(reportField),
			line,
		)
	}
	return line[i+len(reportField):]
}

// promptOf returns the prompt argument the wrapper put on the command line.
func promptOf(c commands.CommandParams) string {
	return c.Args[len(c.Args)-1]
}

// The report field's bytes are a published contract, not internal formatting:
// the agent-side loop splits the report path off a verdict line by this exact
// prefix and reads this exact sentinel as "no report at all".
//
// Every other assertion in this file spells the field via the constants
// themselves, which is right for testing behavior but means a reformat (two
// spaces to one) or a reword of the sentinel would keep the whole suite green
// while silently breaking that consumer. These literals exist so such an edit
// fails HERE, loudly, instead of downstream — CLAUDE.md's "make the mistake
// structurally impossible" applied to a string a parser depends on. Changing
// either value is a contract change, so updating this test is meant to be the
// deliberate act that records it.
func TestReviewRunReportFieldBytesAreTheContract(t *testing.T) {
	if reportField != "  report: " {
		t.Errorf(
			"reportField is %q, want %q — this is what a caller parses on",
			reportField,
			"  report: ",
		)
	}
	if want := "none (the reviewer wrote no report)"; reportNone != want {
		t.Errorf(
			"reportNone is %q, want %q — this is what a caller reads as 'no report'",
			reportNone,
			want,
		)
	}
}

// The report filename joins two encoded segments, and the join is only
// unambiguous because reportNameSeparator is a byte EncodeBranch can never
// emit: the encoder keeps [A-Za-z0-9._-] and percent-encodes everything else.
// That property, not the reviewer registry's current names, is what stops one
// (agent, label) pair spelling the same filename as a different pair — and what
// stops one run's report overwriting another's. Pinned here because it is a
// property of a helper in another package that this one silently relies on.
func TestReviewerReportNameSeparatorCannotComeFromASegment(t *testing.T) {
	for _, segment := range []string{
		reportNameSeparator,
		"code" + reportNameSeparator + "reviewer",
		"openai/gpt" + reportNameSeparator + "5.2",
	} {
		if encoded := reviewjournal.EncodeBranch(
			segment,
		); strings.Contains(
			encoded,
			reportNameSeparator,
		) {
			t.Errorf(
				"EncodeBranch(%q) = %q, which contains the separator %q — the join is no longer injective",
				segment,
				encoded,
				reportNameSeparator,
			)
		}
	}
	// So the separator lands in a filename exactly once, wherever the two
	// segments came from.
	name := reviewerReportName(
		"code"+reportNameSeparator+"reviewer",
		"openai/gpt"+reportNameSeparator+"5.2",
	)
	if got := strings.Count(name, reportNameSeparator); got != 1 {
		t.Errorf("report filename %q contains the separator %d time(s), want exactly 1", name, got)
	}
}

// Range mode runs the configured models exactly as branch mode does, and adds
// one thing to each line: where that run's full report was written.
func TestReviewRunRangeModeRunsEveryConfiguredModelAndPersistsReports(t *testing.T) {
	tm, ocBase, rng := newRangeSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2", "google/gemini-3-pro")
	scriptOpenCode(
		t, ocBase,
		scriptedRun{stdout: fullReport("gpt found a race in the retry path", "REQUEST CHANGES")},
		scriptedRun{stdout: fullReport("gemini found nothing blocking", "APPROVE")},
	)

	out, err := tm.ReviewRun("", "", rng)
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected one line per reviewer, got:\n%s", out)
	}

	// The model id is a path separator away from escaping the report directory,
	// so the encoded filename is asserted literally, not derived in the test.
	wants := []struct{ prefix, file, body, verdict string }{
		{
			"openai/gpt-5.2 → REQUEST CHANGES",
			"code-reviewer+openai%2Fgpt-5.2.md",
			"gpt found a race in the retry path",
			"REQUEST CHANGES",
		},
		{
			"google/gemini-3-pro → APPROVE",
			"code-reviewer+google%2Fgemini-3-pro.md",
			"gemini found nothing blocking",
			"APPROVE",
		},
	}
	for i, want := range wants {
		if !strings.HasPrefix(lines[i], want.prefix+reportField) {
			t.Errorf("line %d: got %q, want %q then a report field", i+1, lines[i], want.prefix)
		}
		got := reportPathFrom(t, lines[i])
		if wantPath := filepath.Join(rng.ReportDir, want.file); got != wantPath {
			t.Errorf("line %d: report at %q, want %q", i+1, got, wantPath)
		}
		data, err := os.ReadFile(got)
		if err != nil {
			t.Fatalf("reading the persisted report: %v", err)
		}
		if string(data) != fullReportText(want.body, want.verdict) {
			t.Errorf(
				"line %d: persisted report is\n%s\nwant\n%s",
				i+1, data, fullReportText(want.body, want.verdict),
			)
		}
	}
	verifyNoStrayCommands(t, tm)
}

// With review.reviewers unset there is one run on OpenCode's own default model,
// in range mode as in branch mode. The report filename then encodes the label
// that names that condition, because there is no model id to name.
func TestReviewRunRangeModeWithoutConfiguredModels(t *testing.T) {
	tm, ocBase, rng := newRangeSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, scriptedRun{
		stdout: fullReport("nothing blocking", "APPROVE"),
		onCall: func(t *testing.T, c commands.CommandParams) {
			if slices.Contains(c.Args, "-m") {
				t.Errorf("expected no -m flag with reviewers unset, got %v", c.Args)
			}
		},
	})

	out, err := tm.ReviewRun("", "", rng)
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	wantPath := filepath.Join(rng.ReportDir, "code-reviewer+OpenCode%20default%20model.md")
	want := "OpenCode default model → APPROVE" + reportField + wantPath
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected the report at %s: %v", wantPath, err)
	}
}

// The four flags are one mode. Any subset of them means a caller intended a
// range review, so the round is refused with the missing flags named — never
// silently downgraded to a branch review of unrelated code.
func TestReviewRunRangeModeRequiresTheWholeFlagGroup(t *testing.T) {
	full := ReviewRange{
		Base:      testRangeBaseRef,
		Head:      testRangeHeadRef,
		Journal:   testRangeJournalKey,
		ReportDir: "reports",
	}
	tests := []struct {
		name        string
		rng         ReviewRange
		wantMissing []string
	}{
		{
			"base alone",
			ReviewRange{Base: full.Base},
			[]string{"--head", "--journal", "--report-dir"},
		},
		{
			"head alone",
			ReviewRange{Head: full.Head},
			[]string{"--base", "--journal", "--report-dir"},
		},
		{
			"journal alone",
			ReviewRange{Journal: full.Journal},
			[]string{"--base", "--head", "--report-dir"},
		},
		{
			"report-dir alone",
			ReviewRange{ReportDir: full.ReportDir},
			[]string{"--base", "--head", "--journal"},
		},
		{
			"three of four",
			ReviewRange{Base: full.Base, Head: full.Head, Journal: full.Journal},
			[]string{"--report-dir"},
		},
		{
			// Whitespace is not a value: a shell variable that expanded to
			// nothing must be refused, not treated as a key.
			"a whitespace-only journal key counts as absent",
			ReviewRange{
				Base:      full.Base,
				Head:      full.Head,
				Journal:   "   ",
				ReportDir: full.ReportDir,
			},
			[]string{"--journal"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm, _, ocBase := newRepoSetup(t, "feat")
			withReviewers(t)
			scriptOpenCode(t, ocBase) // no reviewer may be launched

			_, err := tm.ReviewRun("", "", tt.rng)
			if err == nil {
				t.Fatal("expected a partial range flag group to be refused")
			}
			for _, flag := range tt.wantMissing {
				if !strings.Contains(err.Error(), flag) {
					t.Errorf("expected %s named as missing, got: %v", flag, err)
				}
			}
		})
	}
}

// A base or head this clone does not have is refused before a reviewer starts.
// Discovering it inside a headless run costs minutes and comes back as a
// verdict-shaped report about a diff the reviewer never read.
func TestReviewRunRangeModeRefusesACommitThisCloneLacks(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*ReviewRange)
		wantFlag string
	}{
		{"unknown base", func(r *ReviewRange) { r.Base = "deadbee" }, "--base"},
		{"unknown head", func(r *ReviewRange) { r.Head = "deadbee" }, "--head"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm, ocBase, rng := newRangeSetup(t, "feat")
			withReviewers(t)
			scriptOpenCode(t, ocBase) // no reviewer may be launched
			tt.mutate(&rng)

			_, err := tm.ReviewRun("", "", rng)
			if err == nil {
				t.Fatal("expected an unresolvable commit to be refused")
			}
			for _, want := range []string{tt.wantFlag, "deadbee", "fetch"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected %q in the error, got: %v", want, err)
				}
			}
			// Refused before anything was written, too: the round costs nothing.
			if _, statErr := os.Stat(rng.ReportDir); !os.IsNotExist(statErr) {
				t.Errorf("no report directory should exist yet, stat gave: %v", statErr)
			}
		})
	}
}

// A report directory that cannot be created is refused up front for the same
// reason: it would otherwise be discovered only after a round's worth of
// reports had been produced with nowhere to go.
func TestReviewRunRangeModeRefusesAnUnusableReportDir(t *testing.T) {
	tm, ocBase, rng := newRangeSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase) // no reviewer may be launched

	// A path whose parent is a regular file: MkdirAll cannot create it.
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	rng.ReportDir = filepath.Join(blocker, "reports")

	_, err := tm.ReviewRun("", "", rng)
	if err == nil {
		t.Fatal("expected an unusable --report-dir to be refused")
	}
	for _, want := range []string{"--report-dir", rng.ReportDir} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected %q in the error, got: %v", want, err)
		}
	}
}

// The round's journal is the one --journal names, not the checked-out branch's.
// Both directions matter: the reviewers must read what this PR already settled,
// and nothing may be filed under the branch that happens to be checked out.
func TestReviewRunRangeModeJournalsUnderTheKeyNotTheBranch(t *testing.T) {
	tm, ocBase, rng := newRangeSetup(t, "feat")
	withReviewers(t)
	if _, err := tm.ReviewNoteOpen(
		testRangeJournalKey,
		"",
		"",
		"settled on an earlier tick",
	); err != nil {
		t.Fatalf("setup: %v", err)
	}

	var pointer string
	scriptOpenCode(t, ocBase, scriptedRun{
		stdout: fullReport("nothing blocking", "APPROVE"),
		onCall: func(t *testing.T, c commands.CommandParams) {
			pointer = snapshotPointer(t, c)
		},
	})

	if _, err := tm.ReviewRun("", "", rng); err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}

	jm := reviewjournal.New(tm.Git)
	wantSnapshot, err := jm.SnapshotPathFor("", testRangeJournalKey)
	if err != nil {
		t.Fatalf("SnapshotPathFor: %v", err)
	}
	if pointer != wantSnapshot {
		t.Errorf("reviewer was pointed at %s, want the key's snapshot %s", pointer, wantSnapshot)
	}
	// The branch's own journal must not exist at all: range mode never touches it.
	branchJournal, err := jm.PathFor("", "feat")
	if err != nil {
		t.Fatalf("PathFor: %v", err)
	}
	if _, err := os.Stat(branchJournal); !os.IsNotExist(err) {
		t.Errorf("a branch journal must not be written in range mode, stat gave: %v", err)
	}
	// And what the reviewer read was the key's own history.
	notes, err := tm.ReviewNotes(testRangeJournalKey, "", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	if !strings.Contains(notes, "settled on an earlier tick") {
		t.Errorf("expected the key's journal to carry the earlier entry, got:\n%s", notes)
	}
}

// The prompt is where range mode's scope actually lives: the diff is fetched by
// the reviewer's own review-package call, so nothing in Go can enforce the
// target. Every clause it must carry is pinned here.
func TestReviewRunRangeModePromptTargetsTheImmutableRange(t *testing.T) {
	tm, ocBase, rng := newRangeSetup(t, "feat")
	withReviewers(t)

	var prompt string
	scriptOpenCode(t, ocBase, scriptedRun{
		stdout: fullReport("nothing blocking", "APPROVE"),
		onCall: func(t *testing.T, c commands.CommandParams) { prompt = promptOf(c) },
	})
	if _, err := tm.ReviewRun("", "", rng); err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}

	wanted := []string{
		// The range, as resolved SHAs.
		testRangeBaseSHA + ".." + testRangeHeadSHA,
		// The diff comes from review-package, and the checkout-shaped commands
		// are named as not applying.
		"devgeta task review-package " + testRangeBaseSHA + " " + testRangeHeadSHA,
		"devgeta task review-scope",
		"devgeta task branch-diff",
		"do not run them",
		"git show " + testRangeHeadSHA + ":<path>",
		// Immutable SHAs only — the difference from ADR-0019's branch scope,
		// stated rather than left to be inferred.
		"the working tree is not part of it",
		"uncommitted and untracked files here belong to other work and are out of scope",
		// The journal key and the revision, which is the pair the three
		// reviewer agents' scoped-journal clause triggers on.
		"Your journal key is " + testRangeJournalKey,
		"the revision under review is " + testRangeHeadSHA,
		"--branch " + testRangeJournalKey + " --rev " + testRangeHeadSHA,
	}
	for _, want := range wanted {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected %q in the range prompt, got:\n%s", want, prompt)
		}
	}

	unwanted := []string{
		// Branch mode's prompt would point the reviewer at the checkout, whose
		// working state is explicitly NOT what range mode reviews.
		worktree.ReviewPrompt,
		// The caller's spelling of the range must not survive into the prompt:
		// a ref name read twice during a multi-model round can name two commits.
		testRangeBaseRef,
		testRangeHeadRef,
	}
	for _, bad := range unwanted {
		if strings.Contains(prompt, bad) {
			t.Errorf("the range prompt must not contain %q, got:\n%s", bad, prompt)
		}
	}
}

// --note composes with range mode exactly as it does with a branch: appended to
// whichever prompt the round uses, with the same emphasis-not-scope framing, and
// a blank note still refused before any git call.
func TestReviewRunRangeModeNoteRidesThePrompt(t *testing.T) {
	const note = "the author says the retry cap is deliberate"

	t.Run("the note is appended to the range prompt", func(t *testing.T) {
		tm, ocBase, rng := newRangeSetup(t, "feat")
		withReviewers(t)
		scriptOpenCode(t, ocBase, scriptedRun{
			stdout: fullReport("nothing blocking", "APPROVE"),
			onCall: func(t *testing.T, c commands.CommandParams) {
				prompt := promptOf(c)
				if !strings.HasPrefix(prompt, "Review the commit range "+testRangeBaseSHA) {
					t.Errorf("the note must be appended to the range prompt, got:\n%s", prompt)
				}
				for _, want := range []string{note, "not a narrower scope", testRangeJournalKey} {
					if !strings.Contains(prompt, want) {
						t.Errorf("expected %q in the prompt, got:\n%s", want, prompt)
					}
				}
			},
		})

		if _, err := tm.ReviewRun("", note, rng); err != nil {
			t.Fatalf("ReviewRun: %v", err)
		}
	})

	t.Run("a blank note is refused before any commit is resolved", func(t *testing.T) {
		tm, ocBase, rng := newRangeSetup(t, "feat")
		withReviewers(t)
		scriptOpenCode(t, ocBase) // no reviewer may be launched

		_, err := tm.ReviewRun("", "   ", rng)
		if err == nil {
			t.Fatal("expected a blank --note to be refused in range mode too")
		}
		if !strings.Contains(err.Error(), "--note is blank") {
			t.Errorf("expected the blank-note refusal, got: %v", err)
		}
		if _, statErr := os.Stat(rng.ReportDir); !os.IsNotExist(statErr) {
			t.Errorf("nothing should have been created yet, stat gave: %v", statErr)
		}
	})
}

// None of the three HEAD-dependent refusals applies in range mode: the target is
// stated, so there is nothing left for them to infer. A tick of an unattended
// loop runs from whatever the human left checked out — including the default
// branch, and including no branch at all.
func TestReviewRunRangeModeIgnoresWhatIsCheckedOut(t *testing.T) {
	tests := []struct {
		name   string
		branch string
	}{
		{"on the default branch", "main"},
		{"on a detached HEAD", ""},
		{"on an unrelated feature branch", "some-other-work"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm, ocBase, rng := newRangeSetup(t, tt.branch)
			withReviewers(t)
			// A clean tree with nothing ahead of the default branch, which is
			// what ADR-0019's refusal blocks in branch mode. Range mode must
			// not consult either fact.
			withAheadCount(t, tm, 0)
			scriptOpenCode(
				t,
				ocBase,
				scriptedRun{stdout: fullReport("nothing blocking", "APPROVE")},
			)

			out, err := tm.ReviewRun("", "", rng)
			if err != nil {
				t.Fatalf("range mode must not refuse a checkout it does not use: %v", err)
			}
			if !strings.Contains(out, "→ APPROVE") {
				t.Errorf("expected a verdict line, got:\n%s", out)
			}
		})
	}
}

// HEAD moving mid-round abandons a branch review, because a branch review's
// journal key and reviewed tree both come from HEAD. In range mode both were
// stated by the caller, so a checkout that moves changes nothing this command
// asserts — and the round must not be thrown away over it.
func TestReviewRunRangeModeSurvivesHeadMovingMidRound(t *testing.T) {
	tm, ocBase, rng := newRangeSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2", "google/gemini-3-pro")

	gitBase, ok := tm.Git.Base.(*commands.MockBaseCommand)
	if !ok {
		t.Fatalf("expected a mock git base, got %T", tm.Git.Base)
	}
	orig := gitBase.ExecCommandFn
	headReads := 0
	gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if slices.Contains(c.Args, "--show-current") {
			headReads++
			return "main\n", "", nil
		}
		return orig(c)
	}

	scriptOpenCode(
		t, ocBase,
		scriptedRun{stdout: fullReport("first opinion", "REQUEST CHANGES")},
		scriptedRun{stdout: fullReport("second opinion", "APPROVE")},
	)

	out, err := tm.ReviewRun("", "", rng)
	if err != nil {
		t.Fatalf("a moving HEAD must not abandon a range round: %v", err)
	}
	if len(strings.Split(out, "\n")) != 2 {
		t.Errorf("expected both reviewers to be reported, got:\n%s", out)
	}
	// Stronger than "it did not fail": range mode never asks what HEAD is, so
	// there is no window in which the answer could matter.
	if headReads != 0 {
		t.Errorf(
			"range mode must not read the current branch at all, it read it %d time(s)",
			headReads,
		)
	}
}

// The persisted report and the reported outcome always come from the SAME
// attempt. Filing one attempt's report under another attempt's verdict would
// put findings behind a conclusion they never reached.
func TestReviewRunRangeModePersistsTheReportedAttemptsText(t *testing.T) {
	t.Run("first attempt reported", func(t *testing.T) {
		tm, ocBase, rng := newRangeSetup(t, "feat")
		withReviewers(t)
		scriptOpenCode(t, ocBase, scriptedRun{
			stdout: fullReport("the first attempt's findings", "REQUEST CHANGES"),
		})

		out, err := tm.ReviewRun("", "", rng)
		if err != nil {
			t.Fatalf("ReviewRun: %v", err)
		}
		if !strings.Contains(out, "→ REQUEST CHANGES") {
			t.Fatalf("expected the first attempt's verdict, got:\n%s", out)
		}
		data, err := os.ReadFile(reportPathFrom(t, out))
		if err != nil {
			t.Fatalf("reading the persisted report: %v", err)
		}
		want := fullReportText("the first attempt's findings", "REQUEST CHANGES")
		if string(data) != want {
			t.Errorf("persisted:\n%s\nwant:\n%s", data, want)
		}
	})

	// The retry replaces the outcome only by producing a report — so when it
	// does, its own text is what must be persisted, not the failed attempt's
	// nothing.
	t.Run("first attempt produced no report, the retry did", func(t *testing.T) {
		tm, ocBase, rng := newRangeSetup(t, "feat")
		withReviewers(t)
		scriptOpenCode(
			t, ocBase,
			scriptedRun{stdout: noReportCapture(), stderr: autoRejectStderr},
			scriptedRun{stdout: fullReport("the retry's findings", "APPROVE")},
		)

		out, err := tm.ReviewRun("", "", rng)
		if err != nil {
			t.Fatalf("ReviewRun: %v", err)
		}
		if !strings.Contains(out, "→ APPROVE") {
			t.Fatalf("expected the retry's verdict, got:\n%s", out)
		}
		data, err := os.ReadFile(reportPathFrom(t, out))
		if err != nil {
			t.Fatalf("reading the persisted report: %v", err)
		}
		want := fullReportText("the retry's findings", "APPROVE")
		if string(data) != want {
			t.Errorf("persisted:\n%s\nwant:\n%s", data, want)
		}
		if strings.Contains(string(data), "permission requested") {
			t.Error("the failed attempt's stderr must not end up in the report")
		}
	})
}

// A run that produced no report at all writes no file: an empty report file
// would read as a reviewer who found nothing. The field is still printed, so a
// caller parses one shape either way.
func TestReviewRunRangeModeWritesNoFileWhenThereIsNoReport(t *testing.T) {
	tm, ocBase, rng := newRangeSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase, retried(scriptedRun{
		stdout: noReportCapture(),
		stderr: autoRejectStderr,
	})...)

	out, err := tm.ReviewRun("", "", rng)
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if !strings.HasSuffix(out, reportField+reportNone) {
		t.Errorf("expected the report field to name the absence, got:\n%s", out)
	}
	entries, err := os.ReadDir(rng.ReportDir)
	if err != nil {
		t.Fatalf("reading the report directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no report files, got %d", len(entries))
	}
}

// Branch mode's output contract is untouched: verdict lines with nothing
// appended. The reports are range mode's addition, and a caller parsing branch
// mode's output must see exactly what it saw before this mode existed.
func TestReviewRunBranchModeOutputCarriesNoReportField(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t, "openai/gpt-5.2")
	scriptOpenCode(t, ocBase, scriptedRun{
		stdout: fullReport("plenty of findings that are not persisted anywhere", "REQUEST CHANGES"),
	})

	out, err := tm.ReviewRun("", "", ReviewRange{})
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if out != "openai/gpt-5.2 → REQUEST CHANGES" {
		t.Errorf("got:\n%s\nwant the verdict line alone", out)
	}
	if strings.Contains(out, "report:") {
		t.Errorf("branch mode must not print a report field, got:\n%s", out)
	}
}
