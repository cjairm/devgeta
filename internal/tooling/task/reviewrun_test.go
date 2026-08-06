package task

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm, _, ocBase := newRepoSetup(t, "feat")
			withReviewers(t)
			scriptOpenCode(t, ocBase, scriptedRun{stdout: tt.stdout, err: tt.err})

			out, err := tm.ReviewRun("")
			if err != nil {
				t.Fatalf("ReviewRun: %v", err)
			}
			want := "OpenCode default model → " + tt.want + "\nopen: none"
			if out != want {
				t.Errorf("got:\n%s\nwant:\n%s", out, want)
			}
			verifyNoStrayCommands(t, tm)
		})
	}
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

	out, err := tm.ReviewRun("")
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "openai/gpt-5.2 → ERROR(Unexpected server error. Check server logs.)\nopen: none"
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

	out, err := tm.ReviewRun("")
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "OpenCode default model → ERROR(opencode run failed: exit status 127)\nopen: none"
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

	out, err := tm.ReviewRun("")
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
		"\nopen: none",
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

	out, err := tm.ReviewRun("")
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

	out, err := tm.ReviewRun("")
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "OpenCode default model → ERROR(opencode: command not found)\nopen: none"
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

	out, err := tm.ReviewRun("")
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "OpenCode default model → APPROVE\nopen: none"
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

	out, err := tm.ReviewRun("")
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := strings.Join([]string{
		"openai/gpt-5.2 → REQUEST CHANGES",
		"google/gemini-3-pro → ERROR(Unexpected server error.)",
		"anthropic/claude-opus-4-6 → APPROVE",
		"open: none",
	}, "\n")
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
	verifyNoStrayCommands(t, tm)
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

	out, err := tm.ReviewRun("")
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "openai/gpt-5.2 → APPROVE\ngoogle/gemini-3-pro → APPROVE\nopen: none"
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

	out, err := tm.ReviewRun("")
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	if out != "OpenCode default model → APPROVE\nopen: none" {
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

			if _, err := tm.ReviewRun(tt.flag); err != nil {
				t.Fatalf("ReviewRun: %v", err)
			}
		})
	}
}

// An unknown reviewer is refused against the shared registry, before
// anything is launched.
func TestReviewRunUnknownReviewerRefused(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	scriptOpenCode(t, ocBase)

	_, err := tm.ReviewRun("architecture")
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

	out, err := tm.ReviewRun("")
	if err != nil {
		t.Fatalf("ReviewRun on a feature branch must proceed, got: %v", err)
	}
	if out != "OpenCode default model → APPROVE\nopen: none" {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestReviewRunRefusesOnTheDefaultBranch(t *testing.T) {
	tm, root, ocBase := newRepoSetup(t, "main")
	withReviewers(t)
	scriptOpenCode(t, ocBase) // no run may happen

	_, err := tm.ReviewRun("")
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

	_, err := tm.ReviewRun("")
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

	if _, err := tm.ReviewRun(""); err != nil {
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

	out, err := tm.ReviewRun("")
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	// Ids keep advancing in the live journal while the reads stay frozen.
	if !strings.HasSuffix(out, "\nopen: n1 n2") {
		t.Errorf("expected both live entries listed as open, got:\n%s", out)
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

	if _, err := tm.ReviewRun(""); err != nil {
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

	out, err := tm.ReviewRun("")
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

// --- the open-ids line ----------------------------------------------------

func TestReviewRunListsOpenJournalIDs(t *testing.T) {
	tm, _, ocBase := newRepoSetup(t, "feat")
	withReviewers(t)
	for _, note := range []string{"first", "second", "third"} {
		if _, err := tm.ReviewNoteOpen("", "", note); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	// A settled entry is not open, so it must not be listed.
	if _, err := tm.ReviewNoteSettle("", "n2", "fixed", "", "done"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	scriptOpenCode(t, ocBase, scriptedRun{stdout: statusReport("REQUEST CHANGES")})

	out, err := tm.ReviewRun("")
	if err != nil {
		t.Fatalf("ReviewRun: %v", err)
	}
	want := "OpenCode default model → REQUEST CHANGES\nopen: n1 n3"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}
