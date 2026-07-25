package task

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	gitcli "github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/githubcli"
	"github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/jq"

	"github.com/cjairm/devgeta/internal/commands"
)

// newPRSetup builds a PRManager whose gh and jq calls go through separate mock
// bases so each side can be scripted independently.
func newPRSetup() (pm *PRManager, ghBase, jqBase *commands.MockBaseCommand) {
	ghBase = commands.NewMockBaseCommand()
	jqBase = commands.NewMockBaseCommand()
	pm = &PRManager{
		Gh: &gitcli.GithubCli{Cmd: commands.NewMockCommand(), Base: ghBase},
		Jq: &jq.Jq{Cmd: commands.NewMockCommand(), Base: jqBase},
	}
	return
}

func TestResolvedPtrForState(t *testing.T) {
	cases := []struct {
		state   string
		wantNil bool
		wantVal bool
		wantErr bool
	}{
		{"", false, false, false},
		{"unresolved", false, false, false},
		{"resolved", false, true, false},
		{"all", true, false, false},
		{"bogus", false, false, true},
	}
	for _, c := range cases {
		t.Run(c.state, func(t *testing.T) {
			got, err := resolvedPtrForState(c.state)
			if c.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %v", *got)
				}
				return
			}
			if got == nil || *got != c.wantVal {
				t.Fatalf("expected %v, got %v", c.wantVal, got)
			}
		})
	}
}

func TestReviewThreads(t *testing.T) {
	t.Run("explicit pr renders threads and discussion combined", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		// gh: CurrentRepo, FetchReviewThreads, FetchPRDiscussion (pr given, so no CurrentPRNumber)
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult(`{"data":{}}`, "", nil),
			commands.ExecCommandResult(`{"data":{}}`, "", nil),
		)
		// jq: FormatReviewThreads, then FormatPRDiscussion
		jqBase.SetExecCommandResults(
			commands.ExecCommandResult("## a.go:1 (thread T1)", "", nil),
			commands.ExecCommandResult("## Conversation\n\n**dave**: hi", "", nil),
		)

		out, err := pm.ReviewThreads("42", "all")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "## a.go:1 (thread T1)\n\n## Conversation\n\n**dave**: hi"
		if out != want {
			t.Fatalf("unexpected output: %q", out)
		}

		// Both gh fetches must carry the resolved owner/name/pr
		if ghBase.GetExecCommandCallCount() != 3 {
			t.Fatalf("expected 3 gh calls (CurrentRepo, threads, discussion), got %d",
				ghBase.GetExecCommandCallCount())
		}
		for _, call := range ghBase.ExecCommandCalls[1:] {
			joined := strings.Join(call.Args, " ")
			if !strings.Contains(joined, "owner=octocat") ||
				!strings.Contains(joined, "name=hello") ||
				!strings.Contains(joined, "pr=42") {
				t.Fatalf("fetch missing resolved vars: %v", call.Args)
			}
		}
		// The discussion fetch must NOT be paginated (distinct from the threads fetch).
		discussionCall := ghBase.ExecCommandCalls[2]
		if strings.Contains(strings.Join(discussionCall.Args, " "), "--paginate") {
			t.Fatalf("discussion fetch must not paginate, got: %v", discussionCall.Args)
		}
	})

	t.Run("only threads non-empty", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult(`{"data":{}}`, "", nil),
			commands.ExecCommandResult(`{"data":{}}`, "", nil),
		)
		jqBase.SetExecCommandResults(
			commands.ExecCommandResult("## a.go:1 (thread T1)", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		out, err := pm.ReviewThreads("42", "all")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "## a.go:1 (thread T1)" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("only discussion non-empty", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult(`{"data":{}}`, "", nil),
			commands.ExecCommandResult(`{"data":{}}`, "", nil),
		)
		jqBase.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("## Conversation\n\n**dave**: hi", "", nil),
		)

		out, err := pm.ReviewThreads("42", "all")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "## Conversation\n\n**dave**: hi" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("both empty yields flat friendly message", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult(`{"data":{}}`, "", nil),
			commands.ExecCommandResult(`{"data":{}}`, "", nil),
		)
		jqBase.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		out, err := pm.ReviewThreads("42", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "No review threads or comments." {
			t.Fatalf("unexpected message: %q", out)
		}
	})

	t.Run("discussion shown regardless of --state", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult(`{"data":{}}`, "", nil),
			commands.ExecCommandResult(`{"data":{}}`, "", nil),
		)
		// FormatReviewThreads returns empty (no threads match "resolved"), but
		// discussion is still rendered.
		jqBase.SetExecCommandResults(
			commands.ExecCommandResult("", "", nil),
			commands.ExecCommandResult("## Conversation\n\n**dave**: hi", "", nil),
		)

		out, err := pm.ReviewThreads("42", "resolved")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "## Conversation\n\n**dave**: hi" {
			t.Fatalf("unexpected output: %q", out)
		}
		// FormatReviewThreads must have been called with resolved=true, proving
		// --state still filters only the threads path.
		threadsCall := jqBase.ExecCommandCalls[0]
		if !strings.Contains(strings.Join(threadsCall.Args, " "), "resolved true") {
			t.Fatalf(
				"expected resolved=true passed to threads formatter, got: %v",
				threadsCall.Args,
			)
		}
	})

	t.Run("no pr for branch errors", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		// CurrentRepo ok, CurrentPRNumber returns empty (no PR)
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		_, err := pm.ReviewThreads("", "unresolved")
		if err == nil {
			t.Fatal("expected error when no PR is found")
		}
		if !strings.Contains(err.Error(), "no pull request found") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("bad state errors before any gh call", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()

		if _, err := pm.ReviewThreads("42", "bogus"); err == nil {
			t.Fatal("expected error for bad state")
		}
		if ghBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call when state is invalid")
		}
	})

	t.Run("error from threads fetch propagates before discussion fetch", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult("", "boom", fmt.Errorf("exit 1")),
		)

		if _, err := pm.ReviewThreads("42", "all"); err == nil {
			t.Fatal("expected error when threads fetch fails")
		}
		if ghBase.GetExecCommandCallCount() != 2 {
			t.Fatalf("expected no discussion fetch after threads fetch failure, got %d calls",
				ghBase.GetExecCommandCallCount())
		}
	})

	t.Run("error from discussion fetch propagates", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult(`{"data":{}}`, "", nil),
			commands.ExecCommandResult("", "boom", fmt.Errorf("exit 1")),
		)
		jqBase.SetExecCommandResult("## a.go:1 (thread T1)", "", nil)

		if _, err := pm.ReviewThreads("42", "all"); err == nil {
			t.Fatal("expected error when discussion fetch fails")
		}
	})
}

func TestPRViewAndChecks(t *testing.T) {
	t.Run("pr-view formats via jq", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		ghBase.SetExecCommandResult(`{"number":42}`, "", nil)
		jqBase.SetExecCommandResult("PR #42: Title", "", nil)

		out, err := pm.PRView("42")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "PR #42: Title" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("pr-checks formats via jq", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		ghBase.SetExecCommandResult(`[{"name":"build","state":"SUCCESS"}]`, "", nil)
		jqBase.SetExecCommandResult("SUCCESS\tbuild", "", nil)

		out, err := pm.PRChecks("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "SUCCESS\tbuild" {
			t.Fatalf("unexpected output: %q", out)
		}
	})
}

// TestPRChecksDigest covers the failing-check log digest enrichment: the
// passing/pending one-line format must stay byte-identical to today's (the
// sentinel-adjacent contract task-design.md calls out), and failing checks
// get extra indented lines appended — a fetched log digest, or an honest
// "log unavailable" note when no digest could be produced.
func TestPRChecksDigest(t *testing.T) {
	t.Run("all passing: output unchanged, no extra gh calls", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		raw := `[{"name":"build","state":"SUCCESS","bucket":"pass","link":""}]`
		ghBase.SetExecCommandResult(raw, "", nil)
		jqBase.SetExecCommandResult("SUCCESS\tbuild", "", nil)

		out, err := pm.PRChecks("42")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "SUCCESS\tbuild" {
			t.Fatalf("unexpected output: %q", out)
		}
		if ghBase.GetExecCommandCallCount() != 1 {
			t.Fatalf("expected exactly 1 gh call (PRChecks only), got %d",
				ghBase.GetExecCommandCallCount())
		}
	})

	t.Run("failing check with a parseable Actions link gets a digest appended", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		raw := `[
			{"name":"build","state":"SUCCESS","bucket":"pass","link":""},
			{"name":"test","state":"FAILURE","bucket":"fail","link":"https://github.com/octocat/hello/actions/runs/111/job/222"}
		]`
		formatted := "SUCCESS\tbuild\n" +
			"FAILURE\ttest  https://github.com/octocat/hello/actions/runs/111/job/222"
		logContent := "job\tstep\t2026-07-22T18:32:18.0000000Z compile error: undefined foo\n" +
			"job\tstep\t2026-07-22T18:32:19.0000000Z exit status 1\n"

		ghBase.SetExecCommandResults(
			commands.ExecCommandResult(raw, "", nil),
			commands.ExecCommandResult(logContent, "", nil),
		)
		jqBase.SetExecCommandResult(formatted, "", nil)

		out, err := pm.PRChecks("42")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		wantLines := []string{
			"SUCCESS\tbuild",
			"FAILURE\ttest  https://github.com/octocat/hello/actions/runs/111/job/222",
			"    compile error: undefined foo",
			"    exit status 1",
		}
		if out != strings.Join(wantLines, "\n") {
			t.Fatalf("unexpected output:\n%s", out)
		}
		if ghBase.GetExecCommandCallCount() != 2 {
			t.Fatalf("expected 2 gh calls (PRChecks + RunFailedJobLog), got %d",
				ghBase.GetExecCommandCallCount())
		}
		// The second gh call must target the parsed job id.
		logCall := ghBase.ExecCommandCalls[1]
		if !strings.Contains(strings.Join(logCall.Args, " "), "222") {
			t.Fatalf("expected job id 222 in log-fetch args, got: %v", logCall.Args)
		}
	})

	t.Run("failing check with a non-Actions link falls back cleanly, no crash", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		raw := `[{"name":"external","state":"FAILURE","bucket":"fail","link":"https://ci.example.com/build/1"}]`
		formatted := "FAILURE\texternal  https://ci.example.com/build/1"

		ghBase.SetExecCommandResult(raw, "", nil)
		jqBase.SetExecCommandResult(formatted, "", nil)

		out, err := pm.PRChecks("42")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "FAILURE\texternal  https://ci.example.com/build/1\n" +
			"    log unavailable: external check"
		if out != want {
			t.Fatalf("unexpected output:\n%s", out)
		}
		// No RunFailedJobLog call should be attempted for an unparseable link.
		if ghBase.GetExecCommandCallCount() != 1 {
			t.Fatalf("expected exactly 1 gh call, got %d", ghBase.GetExecCommandCallCount())
		}
	})

	t.Run(
		"mixed pass, pending, and fail: only the failing check gets a digest",
		func(t *testing.T) {
			pm, ghBase, jqBase := newPRSetup()
			raw := `[
			{"name":"build","state":"SUCCESS","bucket":"pass","link":""},
			{"name":"deploy","state":"PENDING","bucket":"pending","link":""},
			{"name":"test","state":"FAILURE","bucket":"fail","link":"https://github.com/octocat/hello/actions/runs/1/job/2"}
		]`
			formatted := "SUCCESS\tbuild\n" +
				"PENDING\tdeploy\n" +
				"FAILURE\ttest  https://github.com/octocat/hello/actions/runs/1/job/2"
			logContent := "job\tstep\t2026-07-22T18:32:18.0000000Z it broke\n"

			ghBase.SetExecCommandResults(
				commands.ExecCommandResult(raw, "", nil),
				commands.ExecCommandResult(logContent, "", nil),
			)
			jqBase.SetExecCommandResult(formatted, "", nil)

			out, err := pm.PRChecks("42")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			wantLines := []string{
				"SUCCESS\tbuild",
				"PENDING\tdeploy",
				"FAILURE\ttest  https://github.com/octocat/hello/actions/runs/1/job/2",
				"    it broke",
			}
			if out != strings.Join(wantLines, "\n") {
				t.Fatalf("unexpected output:\n%s", out)
			}
		},
	)

	t.Run(
		"log fetch error falls back to a note instead of failing the whole call",
		func(t *testing.T) {
			pm, ghBase, jqBase := newPRSetup()
			raw := `[{"name":"test","state":"FAILURE","bucket":"fail","link":"https://github.com/o/r/actions/runs/1/job/2"}]`
			formatted := "FAILURE\ttest  https://github.com/o/r/actions/runs/1/job/2"

			ghBase.SetExecCommandResults(
				commands.ExecCommandResult(raw, "", nil),
				commands.ExecCommandResult("", "boom", fmt.Errorf("exit 1")),
			)
			jqBase.SetExecCommandResult(formatted, "", nil)

			out, err := pm.PRChecks("42")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(out, "log unavailable:") {
				t.Fatalf("expected a log-unavailable note, got: %q", out)
			}
		},
	)

	t.Run(
		"empty but error-free log fetch (permission-gated) falls back to a note",
		func(t *testing.T) {
			pm, ghBase, jqBase := newPRSetup()
			raw := `[{"name":"test","state":"FAILURE","bucket":"fail","link":"https://github.com/o/r/actions/runs/1/job/2"}]`
			formatted := "FAILURE\ttest  https://github.com/o/r/actions/runs/1/job/2"

			ghBase.SetExecCommandResults(
				commands.ExecCommandResult(raw, "", nil),
				commands.ExecCommandResult("", "", nil),
			)
			jqBase.SetExecCommandResult(formatted, "", nil)

			out, err := pm.PRChecks("42")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := "FAILURE\ttest  https://github.com/o/r/actions/runs/1/job/2\n" +
				"    log unavailable: no failed-step log returned"
			if out != want {
				t.Fatalf("unexpected output:\n%s", out)
			}
		},
	)

	t.Run("no checks sentinel passes through untouched", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		ghBase.SetExecCommandResult("[]", "", nil)
		jqBase.SetExecCommandResult("No checks.", "", nil)

		out, err := pm.PRChecks("42")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "No checks." {
			t.Fatalf("unexpected output: %q", out)
		}
		if ghBase.GetExecCommandCallCount() != 1 {
			t.Fatalf("expected exactly 1 gh call, got %d", ghBase.GetExecCommandCallCount())
		}
	})
}

// TestFailingCheckDigestBudget exercises the per-check/total digest size
// bounding directly against the unexported orchestration helper.
func TestFailingCheckDigestBudget(t *testing.T) {
	t.Run("budget already spent: no gh call, one-line note", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		check := prCheck{
			Name: "test", State: "FAILURE", Bucket: "fail",
			Link: "https://github.com/o/r/actions/runs/1/job/2",
		}

		lines, spent := pm.failingCheckDigest(check, 0)
		if spent != 0 {
			t.Fatalf("expected 0 lines spent, got %d", spent)
		}
		if len(lines) != 1 || !strings.Contains(lines[0], "total digest size bound reached") {
			t.Fatalf("unexpected lines: %v", lines)
		}
		if ghBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call once budget is exhausted")
		}
	})

	t.Run("budget smaller than per-check max caps the digest", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		check := prCheck{
			Name: "test", State: "FAILURE", Bucket: "fail",
			Link: "https://github.com/o/r/actions/runs/1/job/2",
		}
		var b strings.Builder
		for i := 0; i < 50; i++ {
			b.WriteString("job\tstep\t2026-07-22T18:32:18.0000000Z log line\n")
		}
		ghBase.SetExecCommandResult(b.String(), "", nil)

		lines, spent := pm.failingCheckDigest(check, 3)
		if spent > 3 {
			t.Fatalf("expected spent <= budget (3), got %d", spent)
		}
		if len(lines) != spent {
			t.Fatalf("expected returned line count to equal spent, got %d lines vs spent=%d",
				len(lines), spent)
		}
	})
}

func TestPRConfirmations(t *testing.T) {
	t.Run("resolve thread", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResult(`{"data":{}}`, "", nil)

		out, err := pm.ResolveThread("PRRT_x")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "Resolved thread PRRT_x" {
			t.Fatalf("unexpected confirmation: %q", out)
		}
	})

	t.Run("approve with explicit pr", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResult("", "", nil)

		out, err := pm.ApprovePR("7", "LGTM")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "Approved PR #7" {
			t.Fatalf("unexpected confirmation: %q", out)
		}
	})

	t.Run("request review with explicit pr", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResult("", "", nil)

		out, err := pm.RequestReviewPR("7", []string{"octocat", "hubot"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "Requested review from octocat, hubot on PR #7" {
			t.Fatalf("unexpected confirmation: %q", out)
		}
	})

	t.Run("merge current branch pr", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResult("", "", nil)

		out, err := pm.MergePR("", "squash")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "Merged the current branch's PR" {
			t.Fatalf("unexpected confirmation: %q", out)
		}
	})
}

func TestCurrentPR(t *testing.T) {
	t.Run("returns number", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResult("42", "", nil)

		out, err := pm.CurrentPR()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "42" {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("no pr friendly message", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResult("", `no pull requests found for branch "feat"`, commandsErr())

		out, err := pm.CurrentPR()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "No pull request found") {
			t.Fatalf("unexpected message: %q", out)
		}
	})
}

func TestReviewEventForVerdict(t *testing.T) {
	cases := map[string]string{
		"approve":         "APPROVE",
		"request-changes": "REQUEST_CHANGES",
		"request_changes": "REQUEST_CHANGES",
		"COMMENT":         "COMMENT",
		"  Approve  ":     "APPROVE",
	}
	for in, want := range cases {
		got, err := reviewEventForVerdict(in)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", in, err)
		}
		if got != want {
			t.Fatalf("%q: want %q, got %q", in, want, got)
		}
	}
	if _, err := reviewEventForVerdict("bogus"); err == nil {
		t.Fatal("expected error for unknown verdict")
	}
}

func TestBuildReviewPayload(t *testing.T) {
	t.Run("embeds comments array and body", func(t *testing.T) {
		out, err := buildReviewPayload(
			"REQUEST_CHANGES",
			"Please fix",
			`[{"path":"a.go","line":1,"body":"x"}]`,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var got struct {
			Body     string           `json:"body"`
			Event    string           `json:"event"`
			Comments []map[string]any `json:"comments"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("payload is not valid JSON: %v\n%s", err, out)
		}
		if got.Event != "REQUEST_CHANGES" || got.Body != "Please fix" || len(got.Comments) != 1 {
			t.Fatalf("unexpected payload: %s", out)
		}
	})

	t.Run("approve allows empty body and no comments", func(t *testing.T) {
		if _, err := buildReviewPayload("APPROVE", "", ""); err != nil {
			t.Fatalf("approve with empty body should be allowed: %v", err)
		}
	})

	t.Run("comment without body or comments errors", func(t *testing.T) {
		if _, err := buildReviewPayload("COMMENT", "", ""); err == nil {
			t.Fatal("expected error: a comment review needs a body or comments")
		}
	})

	t.Run("invalid comments json errors", func(t *testing.T) {
		if _, err := buildReviewPayload("COMMENT", "ok", "{not json"); err == nil {
			t.Fatal("expected error for malformed comments json")
		}
	})
}

func TestSubmitReview(t *testing.T) {
	t.Run("posts review to the resolved endpoint with inline comments", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil), // CurrentRepo
			commands.ExecCommandResult(`{"id":1}`, "", nil),      // CreateReview
		)

		out, err := pm.SubmitReview(
			"42",
			"request-changes",
			"Please fix",
			`[{"path":"a.go","line":10,"body":"fix"}]`,
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "request-changes") || !strings.Contains(out, "PR #42") {
			t.Fatalf("unexpected confirmation: %q", out)
		}

		call := ghBase.ExecCommandCalls[len(ghBase.ExecCommandCalls)-1]
		joined := strings.Join(call.Args, " ")
		if !strings.Contains(joined, "/repos/octocat/hello/pulls/42/reviews") {
			t.Fatalf("missing reviews endpoint: %v", call.Args)
		}
		if !strings.Contains(joined, "--input") {
			t.Fatalf("expected payload passed via --input: %v", call.Args)
		}
	})

	t.Run("invalid verdict errors before any gh call", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		if _, err := pm.SubmitReview("42", "bogus", "x", ""); err == nil {
			t.Fatal("expected error for invalid verdict")
		}
		if ghBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call when the verdict is invalid")
		}
	})

	t.Run("malformed comments error before any gh call", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		if _, err := pm.SubmitReview("42", "comment", "body", "{bad"); err == nil {
			t.Fatal("expected error for malformed comments")
		}
		if ghBase.GetExecCommandCallCount() != 0 {
			t.Fatal("expected no gh call when comments are malformed")
		}
	})
}

// commandsErr returns a non-nil error for simulating gh's non-zero exit.
func commandsErr() error { return &execErr{} }

type execErr struct{}

func (e *execErr) Error() string { return "exit 1" }
