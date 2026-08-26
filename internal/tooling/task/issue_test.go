package task

import (
	"fmt"
	"testing"

	gitapp "github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	gitcli "github.com/cjairm/devgeta/internal/tooling/terminal/dev_tools/githubcli"
)

func init() { testutil.InitLogger() }

// newIssueSetup builds an IssueManager whose gh and git calls go through
// separate mock bases so each side can be scripted independently.
func newIssueSetup() (im *IssueManager, ghBase, gitBase *commands.MockBaseCommand) {
	ghBase = commands.NewMockBaseCommand()
	gitBase = commands.NewMockBaseCommand()
	im = &IssueManager{
		Gh:  &gitcli.GithubCli{Cmd: commands.NewMockCommand(), Base: ghBase},
		Git: &gitapp.Git{Cmd: commands.NewMockCommand(), Base: gitBase},
	}
	return
}

func TestValidateIssueNumber(t *testing.T) {
	for _, c := range []struct {
		in      string
		wantErr bool
	}{
		{"1", false},
		{"3", false},
		{"99999", false},
		{"", true},
		{"-1", true},
		{"1a", true},
		{"../../etc", true},
		{"1 2", true},
		{"0", true},
		{"00", true},
		{"007", true},
	} {
		t.Run(c.in, func(t *testing.T) {
			err := validateIssueNumber(c.in)
			if c.wantErr && err == nil {
				t.Fatalf("expected an error for %q", c.in)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", c.in, err)
			}
		})
	}
}

// TestBranchReferencesIssue is the adversarial table for ADR-0035's grammar:
// the exact digit run of the issue number, with neighbors (when they exist)
// outside [0-9A-Za-z.], and an optional single '#' immediately before. Every
// case here is derived directly from that rule (see the ADR), not picked ad
// hoc — this table IS the manual verification for this boundary.
func TestBranchReferencesIssue(t *testing.T) {
	for _, c := range []struct {
		branch string
		number string
		want   bool
	}{
		{"#12", "12", true},
		{"12", "12", true},
		{"x#12", "12", true},
		{"issue-12-fix", "12", true},
		{"12-fix", "12", true},
		{"fix-12", "12", true},
		{"#1234", "12", false},
		{"#12x", "12", false},
		{"v1.12", "12", false},
		{"2012", "12", false},
		{"12.3", "12", false},
		{"x12", "12", false},
		{"12x", "12", false},
		{"no-number-here", "12", false},
		{"", "12", false},
	} {
		t.Run(c.branch+"_vs_"+c.number, func(t *testing.T) {
			if got := branchReferencesIssue(c.branch, c.number); got != c.want {
				t.Errorf(
					"branchReferencesIssue(%q, %q) = %v, want %v",
					c.branch,
					c.number,
					got,
					c.want,
				)
			}
		})
	}
}

func TestMatchingBranches(t *testing.T) {
	branches := []string{"main", "issue-12-fix", "v1.12-release", "#12", "unrelated"}
	got := matchingBranches(branches, "12")
	want := []string{"issue-12-fix", "#12"}
	if !equalStrSlices(got, want) {
		t.Errorf("matchingBranches: got %v, want %v", got, want)
	}
}

func equalStrSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParseIssueCrossReferences(t *testing.T) {
	t.Run("filters to same-repo PRs and dedups across pages", func(t *testing.T) {
		// --paginate emits one JSON document per page, concatenated.
		raw := `{"data":{"repository":{"issue":{"timelineItems":{"nodes":[` +
			`{"isCrossRepository":false,"source":{"number":5,"title":"Fix it","state":"OPEN","repository":{"nameWithOwner":"octocat/hello"}}},` +
			`{"isCrossRepository":true,"source":{"number":9,"title":"External","state":"OPEN","repository":{"nameWithOwner":"someoneelse/fork"}}}` +
			`],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}` +
			`{"data":{"repository":{"issue":{"timelineItems":{"nodes":[` +
			`{"isCrossRepository":false,"source":{"number":5,"title":"Fix it","state":"OPEN","repository":{"nameWithOwner":"octocat/hello"}}}` +
			`],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`

		got, err := parseIssueCrossReferences(raw, "octocat", "hello")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []issuePRRef{{Number: "5", State: "OPEN", Title: "Fix it"}}
		if len(got) != len(want) || got[0] != want[0] {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("empty timeline returns nothing", func(t *testing.T) {
		raw := `{"data":{"repository":{"issue":{"timelineItems":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`
		got, err := parseIssueCrossReferences(raw, "octocat", "hello")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected no PRs, got %+v", got)
		}
	})

	t.Run("malformed json errors", func(t *testing.T) {
		if _, err := parseIssueCrossReferences("not json", "octocat", "hello"); err == nil {
			t.Error("expected an error for malformed json")
		}
	})
}

func TestFormatIssueScope(t *testing.T) {
	t.Run("nothing references the issue", func(t *testing.T) {
		got := formatIssueScope(issueScopeData{
			Number: "3",
			State:  "OPEN",
			Title:  "agent-config-guard",
		})
		want := "issue: #3 (open)\n" +
			"title: agent-config-guard\n" +
			"prs (confirmed): none\n" +
			"branches (candidate): none\n" +
			"worktrees: none"
		if got != want {
			t.Fatalf("unexpected output:\n%q\nwant:\n%q", got, want)
		}
	})

	t.Run("every field populated", func(t *testing.T) {
		got := formatIssueScope(issueScopeData{
			Number:            "3",
			State:             "OPEN",
			Title:             "agent-config-guard",
			ConfirmedPRs:      []issuePRRef{{Number: "5", State: "OPEN", Title: "Fix it"}},
			CandidateBranches: []string{"issue-3-fix"},
			CandidateWorktrees: []issueWorktreeRef{
				{Branch: "issue-3-fix", Path: "/repo/.worktrees/issue-3-fix"},
			},
		})
		want := "issue: #3 (open)\n" +
			"title: agent-config-guard\n" +
			"prs (confirmed):\n" +
			"  #5 open \"Fix it\"\n" +
			"branches (candidate):\n" +
			"  issue-3-fix\n" +
			"worktrees:\n" +
			"  issue-3-fix -> /repo/.worktrees/issue-3-fix"
		if got != want {
			t.Fatalf("unexpected output:\n%q\nwant:\n%q", got, want)
		}
	})
}

func TestIssueScope(t *testing.T) {
	t.Run("orchestrates gh and git, formats the result", func(t *testing.T) {
		im, ghBase, gitBase := newIssueSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult(
				`{"number":3,"state":"OPEN","title":"agent-config-guard"}`,
				"",
				nil,
			),
			commands.ExecCommandResult(
				`{"data":{"repository":{"issue":{"timelineItems":{"nodes":[`+
					`{"isCrossRepository":false,"source":{"number":5,"title":"Fix it","state":"OPEN","repository":{"nameWithOwner":"octocat/hello"}}}`+
					`],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`,
				"", nil,
			),
		)
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("main\nissue-3-fix\n", "", nil),
			commands.ExecCommandResult(
				"worktree /repo\nHEAD abc\nbranch refs/heads/main\n\n"+
					"worktree /repo/.worktrees/issue-3-fix\nHEAD def\nbranch refs/heads/issue-3-fix\n",
				"", nil,
			),
		)

		out, err := im.IssueScope("3")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "issue: #3 (open)\n" +
			"title: agent-config-guard\n" +
			"prs (confirmed):\n" +
			"  #5 open \"Fix it\"\n" +
			"branches (candidate):\n" +
			"  issue-3-fix\n" +
			"worktrees:\n" +
			"  issue-3-fix -> /repo/.worktrees/issue-3-fix"
		if out != want {
			t.Fatalf("unexpected output:\n%q\nwant:\n%q", out, want)
		}
	})

	t.Run("stable sentinel when nothing references the issue", func(t *testing.T) {
		im, ghBase, gitBase := newIssueSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult(
				`{"number":3,"state":"OPEN","title":"agent-config-guard"}`,
				"",
				nil,
			),
			commands.ExecCommandResult(
				`{"data":{"repository":{"issue":{"timelineItems":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}}`,
				"",
				nil,
			),
		)
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("main\n", "", nil),
			commands.ExecCommandResult(
				"worktree /repo\nHEAD abc\nbranch refs/heads/main\n",
				"",
				nil,
			),
		)

		out, err := im.IssueScope("3")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := "issue: #3 (open)\n" +
			"title: agent-config-guard\n" +
			"prs (confirmed): none\n" +
			"branches (candidate): none\n" +
			"worktrees: none"
		if out != want {
			t.Fatalf("unexpected output:\n%q\nwant:\n%q", out, want)
		}
	})

	t.Run("rejects an invalid issue number before any call", func(t *testing.T) {
		im, ghBase, gitBase := newIssueSetup()

		if _, err := im.IssueScope("abc"); err == nil {
			t.Fatal("expected an error for a non-numeric issue number")
		}
		testutil.VerifyNoRealCommands(t, ghBase)
		testutil.VerifyNoRealCommands(t, gitBase)
	})

	t.Run("propagates a gh error", func(t *testing.T) {
		im, ghBase, _ := newIssueSetup()
		ghBase.SetExecCommandResult("", "not found", fmt.Errorf("exit 1"))

		if _, err := im.IssueScope("3"); err == nil {
			t.Fatal("expected error")
		}
	})
}
