package task

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
)

// The login the tests authenticate as, and one that is not it.
const (
	stateTestLogin      = "cjairm"
	stateTestOtherLogin = "octocat"
)

// ghUserRequest and ghTeamRequest render the two shapes gh emits inside
// reviewRequests. A team entry carries name/slug and NO login — that is the
// wire fact ADR-0021 §3's personal-request-only rule rests on, so the tests
// pin it as literal JSON rather than building it from the parsing structs.
func ghUserRequest(login string) string {
	return fmt.Sprintf(`{"__typename":"User","login":%q}`, login)
}

func ghTeamRequest(slug string) string {
	return fmt.Sprintf(`{"__typename":"Team","name":%q,"slug":%q}`, slug, slug)
}

// ghReview renders one entry of gh's reviews list.
func ghReview(login, state, submittedAt string) string {
	return fmt.Sprintf(
		`{"author":{"login":%q},"state":%q,"submittedAt":%q}`,
		login, state, submittedAt,
	)
}

// ghStateJSON assembles the `gh pr view --json state,isDraft,reviewRequests,reviews`
// payload.
func ghStateJSON(state string, isDraft bool, requests, reviews []string) string {
	return fmt.Sprintf(
		`{"state":%q,"isDraft":%t,"reviewRequests":[%s],"reviews":[%s]}`,
		state, isDraft, strings.Join(requests, ","), strings.Join(reviews, ","),
	)
}

// scriptStateGh scripts the three gh calls a resolved --pr makes: CurrentRepo,
// the PR state read, then the authenticated login.
func scriptStateGh(ghBase *commands.MockBaseCommand, view, login string) {
	ghBase.SetExecCommandResults(
		commands.ExecCommandResult("octocat/hello", "", nil),
		commands.ExecCommandResult(view, "", nil),
		commands.ExecCommandResult(login+"\n", "", nil),
	)
}

// wantState renders the three-line output for the expected values.
func wantState(pr, requested, myReview string) string {
	return fmt.Sprintf("pr: %s\nrequested: %s\nmy-review: %s", pr, requested, myReview)
}

// TestPRReviewStateDecisionTable walks every row of /pr-review-loop's decision
// table (cycle 2026-08-06-pr-review-loop §5) plus each my-review value, and
// asserts the three lines a tick would read. The `action` field is the row the
// caller must land on — it is what each case exists to make reachable.
func TestPRReviewStateDecisionTable(t *testing.T) {
	cases := []struct {
		name     string
		state    string
		isDraft  bool
		requests []string
		reviews  []string
		want     string
		action   string
	}{
		{
			name:     "merged outranks everything below it",
			state:    "MERGED",
			requests: []string{ghUserRequest(stateTestLogin)},
			reviews:  []string{ghReview(stateTestLogin, "APPROVED", "2026-08-06T10:00:00Z")},
			want:     wantState("merged", "yes", "approved"),
			action:   "terminal: closed",
		},
		{
			name:     "closed outranks everything below it",
			state:    "CLOSED",
			requests: []string{ghUserRequest(stateTestLogin)},
			reviews: []string{
				ghReview(stateTestLogin, "CHANGES_REQUESTED", "2026-08-06T10:00:00Z"),
			},
			want:   wantState("closed", "yes", "changes-requested"),
			action: "terminal: closed",
		},
		{
			// The row the loop must not fall through: a requested draft is
			// still unfinished work, so `pr:` has to be distinguishable from
			// open even though GitHub reports the PR as OPEN.
			name:     "a requested draft is reported as draft, not open",
			state:    "OPEN",
			isDraft:  true,
			requests: []string{ghUserRequest(stateTestLogin)},
			want:     wantState("draft", "yes", "none"),
			action:   "wait",
		},
		{
			name:    "an unrequested draft is a draft too",
			state:   "OPEN",
			isDraft: true,
			want:    wantState("draft", "no", "none"),
			action:  "wait",
		},
		{
			// GitHub lets a draft be closed. `closed` must win, or the loop
			// would wait forever on a pull request that is already over.
			name:    "a closed draft reads closed, not draft",
			state:   "CLOSED",
			isDraft: true,
			want:    wantState("closed", "no", "none"),
			action:  "terminal: closed",
		},
		{
			name:     "open and requested with no prior review",
			state:    "OPEN",
			requests: []string{ghUserRequest(stateTestLogin)},
			want:     wantState("open", "yes", "none"),
			action:   "review",
		},
		{
			// A re-request after an approval: the standing approval does not
			// suppress the new request, because the request row is evaluated
			// before the approved row.
			name:     "open and requested again after an approval",
			state:    "OPEN",
			requests: []string{ghUserRequest(stateTestLogin)},
			reviews:  []string{ghReview(stateTestLogin, "APPROVED", "2026-08-06T10:00:00Z")},
			want:     wantState("open", "yes", "approved"),
			action:   "review",
		},
		{
			name:    "open, not requested, standing approval",
			state:   "OPEN",
			reviews: []string{ghReview(stateTestLogin, "APPROVED", "2026-08-06T10:00:00Z")},
			want:    wantState("open", "no", "approved"),
			action:  "terminal: approved",
		},
		{
			name:  "open, not requested, changes requested",
			state: "OPEN",
			reviews: []string{
				ghReview(stateTestLogin, "CHANGES_REQUESTED", "2026-08-06T10:00:00Z"),
			},
			want:   wantState("open", "no", "changes-requested"),
			action: "wait",
		},
		{
			// Verified live: a comment-only review clears the request too, so
			// this is the state right after the loop posts a non-approving
			// review.
			name:    "open, not requested, comment-only review",
			state:   "OPEN",
			reviews: []string{ghReview(stateTestLogin, "COMMENTED", "2026-08-06T10:00:00Z")},
			want:    wantState("open", "no", "commented"),
			action:  "wait",
		},
		{
			name:   "open, not requested, never reviewed",
			state:  "OPEN",
			want:   wantState("open", "no", "none"),
			action: "wait",
		},
		{
			// ADR-0021 §3: a request addressed to a team the user may well be
			// on is a staffing judgment the loop does not make.
			name:     "a team request that does not name the user is not a request",
			state:    "OPEN",
			requests: []string{ghTeamRequest("reviewers"), ghUserRequest(stateTestOtherLogin)},
			want:     wantState("open", "no", "none"),
			action:   "wait",
		},
		{
			name:  "a request naming the user among others does trigger",
			state: "OPEN",
			requests: []string{
				ghTeamRequest("reviewers"),
				ghUserRequest(stateTestOtherLogin),
				ghUserRequest(stateTestLogin),
			},
			want:   wantState("open", "yes", "none"),
			action: "review",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pm, ghBase, jqBase := newPRSetup()
			scriptStateGh(
				ghBase,
				ghStateJSON(c.state, c.isDraft, c.requests, c.reviews),
				stateTestLogin,
			)

			out, err := pm.PRReviewState("213")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out != c.want {
				t.Fatalf("wrong state for %q:\ngot:\n%s\nwant:\n%s", c.action, out, c.want)
			}
			// The state read is gh plus pure Go formatting — no jq hop, and
			// (pm.Git being nil in this setup) no git either.
			testutil.VerifyNoRealCommands(t, jqBase)
		})
	}
}

func TestPRReviewStateMyReview(t *testing.T) {
	run := func(t *testing.T, reviews []string) string {
		t.Helper()
		pm, ghBase, _ := newPRSetup()
		scriptStateGh(ghBase, ghStateJSON("OPEN", false, nil, reviews), stateTestLogin)
		out, err := pm.PRReviewState("213")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return out
	}

	t.Run("the newest submitted review wins, not the last listed", func(t *testing.T) {
		out := run(t, []string{
			ghReview(stateTestLogin, "APPROVED", "2026-08-06T12:00:00Z"),
			ghReview(stateTestLogin, "COMMENTED", "2026-08-06T09:00:00Z"),
		})
		if !strings.Contains(out, "my-review: approved") {
			t.Fatalf("expected the newer approval to win, got:\n%s", out)
		}
	})

	t.Run("a newer review supersedes an older one", func(t *testing.T) {
		out := run(t, []string{
			ghReview(stateTestLogin, "APPROVED", "2026-08-06T09:00:00Z"),
			ghReview(stateTestLogin, "CHANGES_REQUESTED", "2026-08-06T12:00:00Z"),
		})
		if !strings.Contains(out, "my-review: changes-requested") {
			t.Fatalf("expected the newer review to win, got:\n%s", out)
		}
	})

	t.Run("another user's reviews are not mine", func(t *testing.T) {
		out := run(t, []string{ghReview(stateTestOtherLogin, "APPROVED", "2026-08-06T12:00:00Z")})
		if !strings.Contains(out, "my-review: none") {
			t.Fatalf("read someone else's review as mine:\n%s", out)
		}
	})

	t.Run("logins match case-insensitively", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		scriptStateGh(
			ghBase,
			ghStateJSON(
				"OPEN", false,
				[]string{ghUserRequest("CJAIRM")},
				[]string{ghReview("CJairm", "APPROVED", "2026-08-06T12:00:00Z")},
			),
			stateTestLogin,
		)
		out, err := pm.PRReviewState("213")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != wantState("open", "yes", "approved") {
			t.Fatalf("login case defeated the match:\n%s", out)
		}
	})

	t.Run("a review still being drafted is not a submitted review", func(t *testing.T) {
		// A PENDING review carries no submittedAt, so it can never be the
		// user's latest submitted position.
		out := run(t, []string{ghReview(stateTestLogin, "PENDING", "")})
		if !strings.Contains(out, "my-review: none") {
			t.Fatalf("an unsubmitted review was reported as submitted:\n%s", out)
		}
	})

	t.Run("a dismissed approval no longer reads as approved", func(t *testing.T) {
		// Reporting `approved` here would stop the loop on an approval GitHub
		// has already thrown away. `none` lands on the wait row instead.
		out := run(t, []string{ghReview(stateTestLogin, "DISMISSED", "2026-08-06T12:00:00Z")})
		if !strings.Contains(out, "my-review: none") {
			t.Fatalf("a dismissed approval still counted:\n%s", out)
		}
	})
}

func TestPRReviewStateOrchestration(t *testing.T) {
	t.Run("asks gh for exactly the four fields it reads, plus the login", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		scriptStateGh(ghBase, ghStateJSON("OPEN", false, nil, nil), stateTestLogin)

		if _, err := pm.PRReviewState("213"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The count is asserted, not just the argv at each index:
		// MockBaseCommand repeats its last scripted result once the script runs
		// out, so an extra gh call would succeed silently.
		if got := len(ghBase.ExecCommandCalls); got != 3 {
			t.Fatalf(
				"expected exactly 3 gh calls (repo, state, login), got %d: %v",
				got, ghBase.ExecCommandCalls,
			)
		}
		// baseRefName, headRefName and the head repository's owner are
		// deliberately absent: pr-review-target owns the review target, and
		// asking for them here would be data with no consumer.
		wantView := []string{
			"pr", "view", "213", "--json", "state,isDraft,reviewRequests,reviews",
		}
		if got := ghBase.ExecCommandCalls[1].Args; !slices.Equal(got, wantView) {
			t.Fatalf("unexpected state-read args %v, want %v", got, wantView)
		}
		wantLogin := []string{"api", "user", "--jq", ".login"}
		if got := ghBase.ExecCommandCalls[2].Args; !slices.Equal(got, wantLogin) {
			t.Fatalf("unexpected login args %v, want %v", got, wantLogin)
		}
	})

	t.Run("infers the PR from the current branch when --pr is omitted", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult("77\n", "", nil),
			commands.ExecCommandResult(
				ghStateJSON("OPEN", false, []string{ghUserRequest(stateTestLogin)}, nil), "", nil,
			),
			commands.ExecCommandResult(stateTestLogin+"\n", "", nil),
		)

		out, err := pm.PRReviewState("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != wantState("open", "yes", "none") {
			t.Fatalf("unexpected output:\n%s", out)
		}
		want := []string{"pr", "view", "77", "--json", "state,isDraft,reviewRequests,reviews"}
		if got := ghBase.ExecCommandCalls[2].Args; !slices.Equal(got, want) {
			t.Fatalf("unexpected state-read args %v, want %v", got, want)
		}
	})

	t.Run("no PR and no --pr reports the family's error", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		_, err := pm.PRReviewState("")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(
			err.Error(),
			"no pull request found for the current branch; pass --pr",
		) {
			t.Fatalf("expected the family's existing error, got: %v", err)
		}
		if got := len(ghBase.ExecCommandCalls); got != 2 {
			t.Fatalf("expected the run to stop at resolution, got %d gh calls", got)
		}
		testutil.VerifyNoRealCommands(t, jqBase)
	})

	t.Run("rejects a non-numeric PR number before asking gh for state", func(t *testing.T) {
		pm, ghBase, jqBase := newPRSetup()
		ghBase.SetExecCommandResults(commands.ExecCommandResult("octocat/hello", "", nil))

		_, err := pm.PRReviewState("--repo=attacker/evil")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "invalid pull request number") {
			t.Fatalf("unexpected error: %v", err)
		}
		// Only the repo resolution ran: a value that gh would have parsed as a
		// flag never reached a gh argv.
		if got := len(ghBase.ExecCommandCalls); got != 1 {
			t.Fatalf("expected 1 gh call, got %d: %v", got, ghBase.ExecCommandCalls)
		}
		testutil.VerifyNoRealCommands(t, jqBase)
	})

	t.Run("an unknown state ends the command instead of picking a row", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		scriptStateGh(ghBase, ghStateJSON("LOCKED", false, nil, nil), stateTestLogin)

		out, err := pm.PRReviewState("213")
		if err == nil {
			t.Fatalf("expected an error, got output:\n%s", out)
		}
		if out != "" {
			t.Fatalf("expected no output on an unknown state, got:\n%s", out)
		}
		if !strings.Contains(err.Error(), "LOCKED") {
			t.Fatalf("expected the unknown state named in the error, got: %v", err)
		}
		// It stops before spending a call on the login: nothing downstream can
		// be answered once the row is unknowable.
		if got := len(ghBase.ExecCommandCalls); got != 2 {
			t.Fatalf("expected the run to stop at the state read, got %d gh calls", got)
		}
	})

	t.Run("unreadable gh output fails rather than guessing a state", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		scriptStateGh(ghBase, "not json", stateTestLogin)

		out, err := pm.PRReviewState("213")
		if err == nil {
			t.Fatalf("expected an error, got output:\n%s", out)
		}
		if out != "" {
			t.Fatalf("expected no output, got:\n%s", out)
		}
		if !strings.Contains(err.Error(), "PR #213") {
			t.Fatalf("expected the PR named in the error, got: %v", err)
		}
	})

	t.Run("an unauthenticated gh ends the command", func(t *testing.T) {
		// Without a login there is nobody to compare reviewRequests against,
		// and "not requested" would be a wrong answer rather than a missing one.
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult(
				ghStateJSON("OPEN", false, []string{ghUserRequest(stateTestLogin)}, nil), "", nil,
			),
			commands.ExecCommandResult("", "", nil),
		)

		out, err := pm.PRReviewState("213")
		if err == nil {
			t.Fatalf("expected an error, got output:\n%s", out)
		}
		if !strings.Contains(err.Error(), "gh auth login") {
			t.Fatalf("expected an actionable auth error, got: %v", err)
		}
	})

	t.Run("a failed state read is reported, not silently emptied", func(t *testing.T) {
		pm, ghBase, _ := newPRSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult("", "HTTP 404: Not Found", fmt.Errorf("exit status 1")),
		)

		out, err := pm.PRReviewState("213")
		if err == nil {
			t.Fatalf("expected an error, got output:\n%s", out)
		}
		if out != "" {
			t.Fatalf("expected no output, got:\n%s", out)
		}
		// gh's stderr is the only place the reason exists — its exit error is
		// just "exit status 1". A tick that stops reporting only that tells the
		// human nothing about why the watch broke.
		if !strings.Contains(err.Error(), "HTTP 404: Not Found") {
			t.Fatalf("expected gh's own reason in the error, got: %v", err)
		}
		if !strings.Contains(err.Error(), "pr-review-state") {
			t.Fatalf("expected the command named in the error, got: %v", err)
		}
	})
}

func TestMyReviewLabel(t *testing.T) {
	for _, c := range []struct{ state, want string }{
		{"APPROVED", myReviewApproved},
		{"CHANGES_REQUESTED", myReviewChangesRequested},
		{"COMMENTED", myReviewCommented},
		// Everything outside the three standing states reports `none`, which
		// is the value that lands the caller on the wait row.
		{"DISMISSED", myReviewNone},
		{"PENDING", myReviewNone},
		{"SOMETHING_GITHUB_ADDS_LATER", myReviewNone},
		{"", myReviewNone},
	} {
		t.Run(c.state, func(t *testing.T) {
			if got := myReviewLabel(c.state); got != c.want {
				t.Fatalf("myReviewLabel(%q) = %q, want %q", c.state, got, c.want)
			}
		})
	}
}

func TestFormatPRReviewState(t *testing.T) {
	got := formatPRReviewState(prReviewStateData{
		Lifecycle: prLifecycleOpen,
		Requested: true,
		MyReview:  myReviewChangesRequested,
	})

	want := "pr: open\nrequested: yes\nmy-review: changes-requested"
	if got != want {
		t.Fatalf("unexpected output:\n%q\nwant:\n%q", got, want)
	}
	// Three lines, payload only: the first byte is data and no line is
	// markdown scaffolding (task-design.md principles 1-2).
	if lines := strings.Split(got, "\n"); len(lines) != 3 {
		t.Fatalf("expected exactly 3 lines, got %d:\n%s", len(lines), got)
	}
	if strings.HasPrefix(got, "#") || strings.Contains(got, "**") {
		t.Fatalf("output carries markdown decoration:\n%s", got)
	}

	if got := formatPRReviewState(prReviewStateData{
		Lifecycle: prLifecycleMerged,
		MyReview:  myReviewNone,
	}); got != "pr: merged\nrequested: no\nmy-review: none" {
		t.Fatalf("unexpected output for the not-requested case:\n%q", got)
	}
}
