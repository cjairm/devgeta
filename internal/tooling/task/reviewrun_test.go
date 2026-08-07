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
type scriptedRun struct {
	stdout string
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
		return run.stdout, "", run.err
	}
	t.Cleanup(func() {
		if calls != len(runs) {
			t.Errorf("expected %d opencode run(s), got %d", len(runs), calls)
		}
	})
}

// verifyNoStrayCommands asserts review-run never reached around the app
// wrappers to the generic executor: everything it shells out for goes
// through the Git app or the OpenCode wrapper, each with its own mock base.
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
		{
			// A run that exits 0 having said nothing at all still completed;
			// it just stated no verdict.
			"no verdict on empty output",
			"",
			nil,
			"NO VERDICT",
		},
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

			out, err := tm.ReviewRun("", "")
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

	out, err := tm.ReviewRun("", "")
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
	scriptOpenCode(t, ocBase, scriptedRun{
		stdout: errorEvent("UnknownError", "Unexpected server error. Check server logs.") + "\n",
		err:    errors.New("exit status 1"),
	})

	out, err := tm.ReviewRun("", "")
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
	scriptOpenCode(t, ocBase, scriptedRun{err: errors.New("exit status 127")})

	out, err := tm.ReviewRun("", "")
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
	scriptOpenCode(t, ocBase, scriptedRun{stdout: errorEvent("UnknownError", long) + "\n"})

	out, err := tm.ReviewRun("", "")
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
	scriptOpenCode(t, ocBase, scriptedRun{stdout: errorEvent("UnknownError", long) + "\n"})

	out, err := tm.ReviewRun("", "")
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
	scriptOpenCode(t, ocBase, scriptedRun{stdout: "opencode: command not found\n"})

	out, err := tm.ReviewRun("", "")
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

	out, err := tm.ReviewRun("", "")
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
	scriptOpenCode(
		t, ocBase,
		scriptedRun{stdout: statusReport("REQUEST CHANGES")},
		scriptedRun{
			stdout: errorEvent("UnknownError", "Unexpected server error.") + "\n",
			err:    errors.New("exit status 1"),
		},
		scriptedRun{stdout: statusReport("APPROVE")},
	)

	out, err := tm.ReviewRun("", "")
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

	out, err := tm.ReviewRun("", "")
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

	if _, err := tm.ReviewRun("", ""); err != nil {
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
	scriptOpenCode(
		t, ocBase,
		scriptedRun{err: errors.New("exit status 1")},
		scriptedRun{stdout: statusReport("APPROVE")},
	)

	var progress bytes.Buffer
	tm.ProgressOut = &progress
	tm.NowFn = fixedClock(time.Unix(0, 0), time.Second)

	out, err := tm.ReviewRun("", "")
	if err != nil {
		t.Fatalf("a failed reviewer is an outcome, not a command error: %v", err)
	}
	if !strings.Contains(out, "ERROR(") {
		t.Errorf("expected an ERROR outcome in the stdout contract, got:\n%s", out)
	}
	wantProgress := strings.Join([]string{
		"[1/2] openai/gpt-5.2: running",
		"[1/2] openai/gpt-5.2: ERROR(opencode run failed: exit status 1) (1s)",
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

	out, err := tm.ReviewRun("", "")
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

	out, err := tm.ReviewRun("", "")
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if out != "OpenCode default model → APPROVE" {
		t.Errorf("unexpected output:\n%s", out)
	}
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

			if _, err := tm.ReviewRun(tt.flag, ""); err != nil {
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

	if _, err := tm.ReviewRun("", note); err != nil {
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

	_, err := tm.ReviewRun("", "   \n\t")
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

	_, err := tm.ReviewRun("architecture", "")
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

	out, err := tm.ReviewRun("", "")
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

	_, err := tm.ReviewRun("", "")
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

	_, err := tm.ReviewRun("", "")
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

	_, err := tm.ReviewRun("", "")
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

	out, err := tm.ReviewRun("", "")
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

	if _, err := tm.ReviewRun("", ""); err != nil {
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

	if _, err := tm.ReviewRun("", ""); err != nil {
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

	out, err := tm.ReviewRun("", "")
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

	_, err := tm.ReviewRun("", "")
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

	out, err := tm.ReviewRun("", "")
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

	out, err := tm.ReviewRun("", "")
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

	if _, err := tm.ReviewRun("", ""); err != nil {
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

	out, err := tm.ReviewRun("", "")
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

	out, err := tm.ReviewRun("", "")
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
				if _, err := tm.ReviewNoteOpen("", "", "reviewer-1 finding"); err != nil {
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

	if _, err := tm.ReviewRun("", ""); err != nil {
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
	if _, err := tm.ReviewNoteOpen("", "", "round-start finding"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	scriptOpenCode(
		t, ocBase,
		scriptedRun{
			stdout: statusReport("REQUEST CHANGES"),
			onCall: func(t *testing.T, c commands.CommandParams) {
				// Stand in for the reviewer's own `review-note --open`
				// process, which writes to the LIVE journal.
				if _, err := tm.ReviewNoteOpen("", "", "reviewer-1 finding"); err != nil {
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

	if _, err := tm.ReviewRun("", ""); err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	// Ids keep advancing in the LIVE journal while the reads stay frozen: the
	// round-start entry and the one reviewer 1 wrote mid-round are both there,
	// each with its own real id.
	notes, err := tm.ReviewNotes("", false, false)
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

	if _, err := tm.ReviewRun("", ""); err != nil {
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
	scriptOpenCode(t, ocBase, scriptedRun{
		err: errors.New("exit status 1"),
		onCall: func(t *testing.T, c commands.CommandParams) {
			seenPath = snapshotPointer(t, c)
		},
	})

	out, err := tm.ReviewRun("", "")
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
		if _, err := tm.ReviewNoteOpen("", "", note); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	if _, err := tm.ReviewNoteSettle("", "n2", "fixed", "", "done"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("REQUEST CHANGES")})

	out, err := tm.ReviewRun("", "")
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
