package task

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
)

// scriptPRStateGh scripts the five gh calls PRState makes with an already
// resolved --pr: CurrentRepo, PRChecks, FetchReviewThreads, the PR state
// read, then the authenticated login.
func scriptPRStateGh(
	ghBase *commands.MockBaseCommand,
	checksJSON, threadsJSON, stateJSON, login string,
) {
	ghBase.SetExecCommandResults(
		commands.ExecCommandResult("octocat/hello", "", nil),
		commands.ExecCommandResult(checksJSON, "", nil),
		commands.ExecCommandResult(threadsJSON, "", nil),
		commands.ExecCommandResult(stateJSON, "", nil),
		commands.ExecCommandResult(login+"\n", "", nil),
	)
}

func threadsDoc(unresolvedCount, resolvedCount int) string {
	var nodes []string
	for i := 0; i < unresolvedCount; i++ {
		nodes = append(nodes, `{"isResolved":false}`)
	}
	for i := 0; i < resolvedCount; i++ {
		nodes = append(nodes, `{"isResolved":true}`)
	}
	return fmt.Sprintf(
		`{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[%s],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`,
		strings.Join(nodes, ","),
	)
}

func TestGatherPRChecksBuckets(t *testing.T) {
	checks := []prCheck{
		{Bucket: "pass"},
		{Bucket: "pass"},
		{Bucket: "fail"},
		{Bucket: "pending"},
		{Bucket: "skipping"},
		{Bucket: "cancel"},
	}
	got := tallyChecksBuckets(checks)
	want := checksBucketCounts{Pass: 2, Fail: 1, Pending: 1, Skipping: 1, Cancel: 1}
	if got != want {
		t.Errorf("tallyChecksBuckets: got %+v, want %+v", got, want)
	}
}

func TestCountUnresolvedReviewThreads(t *testing.T) {
	t.Run("counts across pages", func(t *testing.T) {
		raw := threadsDoc(1, 1) + threadsDoc(2, 0)
		got, err := countUnresolvedReviewThreads(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 3 {
			t.Errorf("got %d, want 3", got)
		}
	})

	t.Run("malformed json errors", func(t *testing.T) {
		if _, err := countUnresolvedReviewThreads("not json"); err == nil {
			t.Error("expected an error")
		}
	})
}

func TestFormatPRState(t *testing.T) {
	t.Run("every gatherer succeeded", func(t *testing.T) {
		got := formatPRState(prStateData{
			ReviewStateOK: true,
			ReviewState: prReviewStateData{
				Lifecycle: prLifecycleOpen,
				Requested: true,
				MyReview:  myReviewNone,
			},
			ChecksOK:          true,
			Checks:            checksBucketCounts{Pass: 3, Fail: 1},
			ThreadsOK:         true,
			UnresolvedThreads: 2,
		})
		want := "pr: open\n" +
			"requested: yes\n" +
			"my-review: none\n" +
			"checks: pass=3 fail=1 pending=0 skipping=0 cancel=0\n" +
			"threads-unresolved: 2"
		if got != want {
			t.Fatalf("unexpected output:\n%q\nwant:\n%q", got, want)
		}
	})

	t.Run("checks gatherer failed, the rest still render", func(t *testing.T) {
		got := formatPRState(prStateData{
			ReviewStateOK: true,
			ReviewState: prReviewStateData{
				Lifecycle: prLifecycleDraft,
				MyReview:  myReviewApproved,
			},
			ChecksErr:         fmt.Errorf("gh: rate limited"),
			ThreadsOK:         true,
			UnresolvedThreads: 0,
		})
		want := "pr: draft\n" +
			"requested: no\n" +
			"my-review: approved\n" +
			"checks: unavailable: gh: rate limited\n" +
			"threads-unresolved: 0"
		if got != want {
			t.Fatalf("unexpected output:\n%q\nwant:\n%q", got, want)
		}
	})

	t.Run("review-state gatherer failed, checks and threads still render", func(t *testing.T) {
		got := formatPRState(prStateData{
			ReviewStateErr:    fmt.Errorf("gh: not found"),
			ChecksOK:          true,
			Checks:            checksBucketCounts{Pass: 1},
			ThreadsOK:         true,
			UnresolvedThreads: 4,
		})
		want := "pr: unavailable: gh: not found\n" +
			"requested: unavailable\n" +
			"my-review: unavailable\n" +
			"checks: pass=1 fail=0 pending=0 skipping=0 cancel=0\n" +
			"threads-unresolved: 4"
		if got != want {
			t.Fatalf("unexpected output:\n%q\nwant:\n%q", got, want)
		}
	})
}

func TestPRState(t *testing.T) {
	t.Run(
		"composes all three gatherers within the ADR-0034 call budget (--pr given)",
		func(t *testing.T) {
			pm, ghBase, _ := newPRSetup()
			checksJSON := `[{"name":"build","state":"SUCCESS","bucket":"pass","link":""}]`
			scriptPRStateGh(
				ghBase,
				checksJSON,
				threadsDoc(1, 0),
				ghStateJSON("OPEN", false, nil, nil),
				stateTestLogin,
			)

			out, err := pm.PRState("213")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := "pr: open\n" +
				"requested: no\n" +
				"my-review: none\n" +
				"checks: pass=1 fail=0 pending=0 skipping=0 cancel=0\n" +
				"threads-unresolved: 1"
			if out != want {
				t.Fatalf("unexpected output:\n%q\nwant:\n%q", out, want)
			}

			if got := len(ghBase.ExecCommandCalls); got != 5 {
				t.Fatalf(
					"expected exactly 5 gh calls with --pr given, got %d: %v",
					got,
					ghBase.ExecCommandCalls,
				)
			}
			// No RunFailedJobLog call: pr-state must never trigger PRChecks' own
			// per-failure log-digest fetch (ADR-0034 §2).
			for _, c := range ghBase.ExecCommandCalls {
				if slices.Contains(c.Args, "run") {
					t.Fatalf("unexpected `gh run` call (log digest fetch): %v", c.Args)
				}
			}
			// Never fetches PR discussion — the unresolved count comes from the
			// review-threads query alone (ADR-0034 §2).
			joinedAll := ""
			for _, c := range ghBase.ExecCommandCalls {
				joinedAll += strings.Join(c.Args, " ") + "\n"
			}
			if strings.Contains(joinedAll, "reviews(first") {
				t.Fatalf("unexpected PR discussion fetch in calls:\n%s", joinedAll)
			}
		},
	)

	t.Run(
		"infers the PR from the current branch when --pr is omitted (6 calls)",
		func(t *testing.T) {
			pm, ghBase, _ := newPRSetup()
			checksJSON := `[]`
			ghBase.SetExecCommandResults(
				commands.ExecCommandResult("octocat/hello", "", nil),
				commands.ExecCommandResult("77\n", "", nil),
				commands.ExecCommandResult(checksJSON, "", nil),
				commands.ExecCommandResult(threadsDoc(0, 0), "", nil),
				commands.ExecCommandResult(ghStateJSON("OPEN", false, nil, nil), "", nil),
				commands.ExecCommandResult(stateTestLogin+"\n", "", nil),
			)

			if _, err := pm.PRState(""); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := len(ghBase.ExecCommandCalls); got != 6 {
				t.Fatalf(
					"expected exactly 6 gh calls when --pr is inferred, got %d: %v",
					got,
					ghBase.ExecCommandCalls,
				)
			}
		},
	)

	t.Run("checks gatherer failing does not fail the whole command", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult("", "rate limited", fmt.Errorf("exit 1")),
			commands.ExecCommandResult(threadsDoc(0, 0), "", nil),
			commands.ExecCommandResult(ghStateJSON("OPEN", false, nil, nil), "", nil),
			commands.ExecCommandResult(stateTestLogin+"\n", "", nil),
		)

		out, err := pm.PRState("213")
		if err != nil {
			t.Fatalf("expected the command to still succeed, got error: %v", err)
		}
		if !strings.Contains(out, "checks: unavailable:") {
			t.Fatalf("expected an unavailable checks line, got:\n%s", out)
		}
		if !strings.Contains(out, "pr: open") {
			t.Fatalf("expected the review-state line to still render, got:\n%s", out)
		}
	})

	t.Run("rejects a non-numeric PR number before any gatherer runs", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		ghBase.SetExecCommandResults(commands.ExecCommandResult("octocat/hello", "", nil))

		if _, err := pm.PRState("--repo=attacker/evil"); err == nil {
			t.Fatal("expected an error")
		}
		if got := len(ghBase.ExecCommandCalls); got != 1 {
			t.Fatalf(
				"expected only the repo resolution call, got %d: %v",
				got,
				ghBase.ExecCommandCalls,
			)
		}
		testutil.VerifyNoRealCommands(t, jqBase)
	})
}
