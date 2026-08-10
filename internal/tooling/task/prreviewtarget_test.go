package task

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	git_app "github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
)

// Fixed SHAs for the review target. mergeBaseSHA is what merge-base returns —
// deliberately unequal to the head, standing for a base branch that advanced
// past the PR, which is the case a merge-base-less implementation gets wrong.
const (
	mergeBaseSHA = "9f2c1ab8bc0d1e2f3a4b5c6d7e8f90a1b2c3d4e5"
	prHeadSHA    = "2f38a274cd0e1f2a3b4c5d6e7f8091a2b3c4d5e6"
)

// newPRTargetSetup extends newPRSetup with the git leg PRReviewTarget needs,
// so the gh and git sides can be scripted (and asserted) independently.
func newPRTargetSetup() (pm *PRManager, ghBase, gitBase *commands.MockBaseCommand) {
	pm, ghBase, _ = newPRSetup()
	gitBase = commands.NewMockBaseCommand()
	pm.Git = &git_app.Git{Cmd: commands.NewMockCommand(), Base: gitBase}
	return
}

// scriptTargetGh scripts the two gh calls a resolved --pr makes: CurrentRepo,
// then PRView for the base branch.
func scriptTargetGh(ghBase *commands.MockBaseCommand, repo, baseBranch string) {
	ghBase.SetExecCommandResults(
		commands.ExecCommandResult(repo, "", nil),
		commands.ExecCommandResult(fmt.Sprintf(`{"baseRefName":%q}`, baseBranch), "", nil),
	)
}

// scriptTargetGit scripts the five git calls of a successful resolution:
// fetch, rev-parse (head), merge-base, then fileChanges' numstat/name-status.
func scriptTargetGit(gitBase *commands.MockBaseCommand, numstat, nameStatus string) {
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult(prHeadSHA+"\n", "", nil),
		commands.ExecCommandResult(mergeBaseSHA+"\n", "", nil),
		commands.ExecCommandResult(numstat, "", nil),
		commands.ExecCommandResult(nameStatus, "", nil),
	)
}

// gitArgs returns the argv of the nth git call, failing the test when that
// call never happened.
func gitArgs(t *testing.T, gitBase *commands.MockBaseCommand, n int) []string {
	t.Helper()
	if len(gitBase.ExecCommandCalls) <= n {
		t.Fatalf("expected at least %d git calls, got %d", n+1, len(gitBase.ExecCommandCalls))
	}
	return gitBase.ExecCommandCalls[n].Args
}

func TestPRReviewTarget(t *testing.T) {
	t.Run("prints the target for a same-repo PR", func(t *testing.T) {
		pm, ghBase, gitBase := newPRTargetSetup()
		scriptTargetGh(ghBase, "octocat/hello", "main")
		scriptTargetGit(
			gitBase,
			"12\t3\tinternal/tooling/task/pr.go\n4\t0\tdocs/spec.md\n",
			"M\tinternal/tooling/task/pr.go\nA\tdocs/spec.md\n",
		)

		out, err := pm.PRReviewTarget("213")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := "base: " + mergeBaseSHA + "\n" +
			"head: " + prHeadSHA + "\n" +
			"journal: pr/octocat/hello/213\n" +
			"files:\n" +
			"- internal/tooling/task/pr.go\n" +
			"- docs/spec.md"
		if out != want {
			t.Fatalf("unexpected output:\n%s\nwant:\n%s", out, want)
		}
	})

	t.Run("fetches refs/pull/<n>/head read-only, bounded, and forced", func(t *testing.T) {
		pm, ghBase, gitBase := newPRTargetSetup()
		scriptTargetGh(ghBase, "octocat/hello", "main")
		scriptTargetGit(gitBase, "1\t0\ta.go\n", "M\ta.go\n")

		if _, err := pm.PRReviewTarget("213"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		fetch := gitBase.ExecCommandCalls[0]
		want := []string{
			"fetch", "--no-tags", "origin",
			"+refs/pull/213/head:refs/devgeta/pr/213/head",
			"+refs/heads/main:refs/devgeta/pr/213/base",
		}
		if !slices.Equal(fetch.Args, want) {
			t.Fatalf("unexpected fetch args:\n%v\nwant:\n%v", fetch.Args, want)
		}
		if fetch.Timeout != reviewFetchTimeout {
			t.Fatalf("expected the fetch bounded at %v, got %v", reviewFetchTimeout, fetch.Timeout)
		}
		// Nothing in the whole run may touch the checkout: no branch is
		// created or moved, nothing is checked out, no stash. A review loop
		// runs unattended, so the user's working tree must be exactly as they
		// left it (ADR-0022 §1).
		for _, call := range gitBase.ExecCommandCalls {
			for _, arg := range call.Args {
				switch arg {
				case "checkout", "switch", "stash", "branch", "reset", "merge", "pull":
					t.Fatalf("git command touches the checkout: %v", call.Args)
				}
			}
		}
	})

	t.Run("takes the same single path for a fork PR", func(t *testing.T) {
		// A fork PR differs only in fields this command deliberately does not
		// read (headRefName, the head repository's owner): refs/pull/<n>/head
		// is served by the UPSTREAM repo for forks too, which is the whole
		// reason ADR-0022 chose it over the fork's own branch ref. So the
		// contract to pin is that the resolution is byte-identical to a
		// same-repo PR — no fork remote, no second URL, no fork-only branch.
		pm, ghBase, gitBase := newPRTargetSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult(
				`{"baseRefName":"main","headRefName":"patch-1","headRepositoryOwner":{"login":"contributor"}}`,
				"",
				nil,
			),
		)
		scriptTargetGit(
			gitBase,
			"7\t2\tinternal/apps/git/git.go\n",
			"M\tinternal/apps/git/git.go\n",
		)

		out, err := pm.PRReviewTarget("213")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := []string{
			"fetch", "--no-tags", "origin",
			"+refs/pull/213/head:refs/devgeta/pr/213/head",
			"+refs/heads/main:refs/devgeta/pr/213/base",
		}
		if !slices.Equal(gitArgs(t, gitBase, 0), want) {
			t.Fatalf("fork PR fetched differently:\n%v\nwant:\n%v", gitArgs(t, gitBase, 0), want)
		}
		// No remote was added, and nothing named the fork's owner.
		for _, call := range gitBase.ExecCommandCalls {
			for _, arg := range call.Args {
				if arg == "remote" || strings.Contains(arg, "contributor") {
					t.Fatalf("fork PR took a fork-specific path: %v", call.Args)
				}
			}
		}
		if !strings.Contains(out, "head: "+prHeadSHA) {
			t.Fatalf("expected the fork's head resolved, got:\n%s", out)
		}
	})

	t.Run("ranges from the merge base, not the base branch tip", func(t *testing.T) {
		// The base branch advanced after the PR opened: its tip (baseTipSHA)
		// is ahead of the merge base (mergeBaseSHA). A `base-tip..head` range
		// would show every commit merged into the base meanwhile as a
		// reversal, and reviewers would report other people's work as this
		// PR's deletions.
		pm, ghBase, gitBase := newPRTargetSetup()
		scriptTargetGh(ghBase, "octocat/hello", "main")
		scriptTargetGit(gitBase, "3\t1\tapi/server.go\n", "M\tapi/server.go\n")

		out, err := pm.PRReviewTarget("213")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// mergeBaseSHA reaches the output from the merge-base call and from
		// nowhere else: the run resolves exactly one ref by name, the head,
		// and reuses that SHA. That is the structural reason a base tip cannot
		// be printed here — the code never asks for one — so the guard is
		// these three positive assertions (base value, merge-base operands,
		// diff range), not a negative check against a tip SHA no scripted call
		// can return.
		if !strings.Contains(out, "base: "+mergeBaseSHA) {
			t.Fatalf("expected the merge base as base, got:\n%s", out)
		}
		mb := gitArgs(t, gitBase, 2)
		wantMB := []string{"merge-base", "refs/devgeta/pr/213/base", prHeadSHA}
		if !slices.Equal(mb, wantMB) {
			t.Fatalf("unexpected merge-base args %v, want %v", mb, wantMB)
		}
		// The diff must run over the merge-base range, not the base tip's.
		wantRange := mergeBaseSHA + ".." + prHeadSHA
		for _, n := range []int{3, 4} {
			args := gitArgs(t, gitBase, n)
			if !slices.Contains(args, wantRange) {
				t.Fatalf("expected diff over %s, got %v", wantRange, args)
			}
		}
	})

	t.Run(
		"pairs the base with the head it resolved, not a ref that moved after",
		func(t *testing.T) {
			// The refs under refs/devgeta/pr/<n>/ are LOCAL and mutable: every fetch
			// of this PR force-updates them. A review loop runs unattended on an
			// interval, so a second tick — or a human running the command by hand —
			// can fetch the same PR while this run sits between resolving the head
			// and asking for the merge base.
			//
			// The mock models exactly that, the way git would: once the head has
			// been resolved, the ref NAME points at a force-pushed head that was
			// rebased onto a newer base, so a merge base asked for by name comes
			// back as rebasedBaseSHA — a commit that is not an ancestor of the head
			// this run captured and prints. Pairing the two would emit a
			// `base..head` range describing no single diff, which is the guarantee
			// ADR-0022 exists to make.
			const rebasedBaseSHA = "c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f7"
			headRef := "refs/devgeta/pr/213/head"

			pm, ghBase, gitBase := newPRTargetSetup()
			scriptTargetGh(ghBase, "octocat/hello", "main")
			headResolved := false
			gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
				switch {
				case c.Args[0] == "fetch":
					return "", "", nil
				case c.Args[0] == "rev-parse":
					// The concurrent fetch lands right after this resolution.
					headResolved = true
					return prHeadSHA + "\n", "", nil
				case c.Args[0] == "merge-base":
					if headResolved && slices.Contains(c.Args, headRef) {
						return rebasedBaseSHA + "\n", "", nil
					}
					return mergeBaseSHA + "\n", "", nil
				case slices.Contains(c.Args, "--numstat"):
					return "3\t1\tapi/server.go\n", "", nil
				default:
					return "M\tapi/server.go\n", "", nil
				}
			}

			out, err := pm.PRReviewTarget("213")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// The printed pair first, since that is the whole product of this
			// command: base must be the merge base of the head it returns.
			if strings.Contains(out, rebasedBaseSHA) {
				t.Fatalf("base came from the moved ref, not the reviewed head:\n%s", out)
			}
			if !strings.Contains(out, "base: "+mergeBaseSHA) ||
				!strings.Contains(out, "head: "+prHeadSHA) {
				t.Fatalf("expected the base paired with the resolved head, got:\n%s", out)
			}
			// Then the mechanism that guarantees it: the merge base is asked
			// for against the resolved SHA, so no later move of the ref can be
			// read back into this run.
			mb := gitArgs(t, gitBase, 2)
			wantMB := []string{"merge-base", "refs/devgeta/pr/213/base", prHeadSHA}
			if !slices.Equal(mb, wantMB) {
				t.Fatalf("unexpected merge-base args %v, want %v", mb, wantMB)
			}
		},
	)

	t.Run("filters noise out of the file list and says it did", func(t *testing.T) {
		pm, ghBase, gitBase := newPRTargetSetup()
		scriptTargetGh(ghBase, "octocat/hello", "main")
		scriptTargetGit(
			gitBase,
			"12\t3\tinternal/tooling/task/pr.go\n40\t12\tgo.sum\n0\t0\tweb/app.min.js\n",
			"M\tinternal/tooling/task/pr.go\nM\tgo.sum\nM\tweb/app.min.js\n",
		)

		out, err := pm.PRReviewTarget("213")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if strings.Contains(out, "- go.sum") || strings.Contains(out, "- web/app.min.js") {
			t.Fatalf("noise survived into the file list:\n%s", out)
		}
		if !strings.Contains(out, "- internal/tooling/task/pr.go") {
			t.Fatalf("reviewable file missing:\n%s", out)
		}
		// Lossy output announces itself and offers a way in (task-design.md).
		wantNote := "excluded (see `dg task review-package " + mergeBaseSHA + " " + prHeadSHA +
			" --file <path>` to inspect): go.sum (+40/-12), web/app.min.js (+0/-0)"
		if !strings.Contains(out, wantNote) {
			t.Fatalf("expected exclusion receipt %q in:\n%s", wantNote, out)
		}
	})

	t.Run("a range with no reviewable file gets a sentinel, not empty output", func(t *testing.T) {
		pm, ghBase, gitBase := newPRTargetSetup()
		scriptTargetGh(ghBase, "octocat/hello", "main")
		scriptTargetGit(gitBase, "", "")

		out, err := pm.PRReviewTarget("213")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "files:\n(none)") {
			t.Fatalf("expected the (none) sentinel, got:\n%s", out)
		}
	})

	t.Run("a failed fetch ends the command instead of reviewing local refs", func(t *testing.T) {
		pm, ghBase, gitBase := newPRTargetSetup()
		scriptTargetGh(ghBase, "octocat/hello", "main")
		gitBase.SetExecCommandResults(
			commands.ExecCommandResult("", "fatal: unable to access origin: network unreachable",
				fmt.Errorf("exit status 128")),
		)

		out, err := pm.PRReviewTarget("213")
		if err == nil {
			t.Fatalf("expected an error, got output:\n%s", out)
		}
		if out != "" {
			t.Fatalf("expected no output on a failed fetch, got:\n%s", out)
		}
		if !strings.Contains(err.Error(), "network unreachable") {
			t.Fatalf("expected git's reason in the error, got: %v", err)
		}
		if !strings.Contains(err.Error(), "stale") {
			t.Fatalf("expected the error to say why it refuses, got: %v", err)
		}
		// The same failure fires when the PR's base branch was deleted or
		// renamed upstream and the `+refs/heads/<base>` half finds nothing.
		// Advice naming only the network would send the reader hunting the
		// wrong thing, so the message must reach both causes.
		if !strings.Contains(err.Error(), "base branch") {
			t.Fatalf("expected the error to offer the base branch as a cause, got: %v", err)
		}
		// The fetch is the ONLY git call: no rev-parse, no merge-base, no
		// diff. Falling through would review whatever is on disk, which is a
		// confident review of code the PR may no longer contain.
		if len(gitBase.ExecCommandCalls) != 1 {
			t.Fatalf(
				"expected the run to stop at the fetch, got %d git calls: %v",
				len(gitBase.ExecCommandCalls), gitBase.ExecCommandCalls,
			)
		}
	})

	t.Run("infers the PR from the current branch when --pr is omitted", func(t *testing.T) {
		pm, ghBase, gitBase := newPRTargetSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult("77\n", "", nil),
			commands.ExecCommandResult(`{"baseRefName":"develop"}`, "", nil),
		)
		scriptTargetGit(gitBase, "1\t0\ta.go\n", "M\ta.go\n")

		out, err := pm.PRReviewTarget("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, "journal: pr/octocat/hello/77") {
			t.Fatalf("expected the inferred PR in the journal key, got:\n%s", out)
		}
		want := []string{
			"fetch", "--no-tags", "origin",
			"+refs/pull/77/head:refs/devgeta/pr/77/head",
			"+refs/heads/develop:refs/devgeta/pr/77/base",
		}
		if !slices.Equal(gitArgs(t, gitBase, 0), want) {
			t.Fatalf("unexpected fetch args %v, want %v", gitArgs(t, gitBase, 0), want)
		}
	})

	t.Run("no PR and no --pr reports the family's error without touching git", func(t *testing.T) {
		pm, ghBase, gitBase := newPRTargetSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult("", "", nil),
		)

		_, err := pm.PRReviewTarget("")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(
			err.Error(),
			"no pull request found for the current branch; pass --pr",
		) {
			t.Fatalf("expected the family's existing error, got: %v", err)
		}
		testutil.VerifyNoRealCommands(t, gitBase)
	})

	t.Run("rejects a non-numeric PR number before building a refspec", func(t *testing.T) {
		pm, ghBase, gitBase := newPRTargetSetup()
		ghBase.SetExecCommandResults(commands.ExecCommandResult("octocat/hello", "", nil))

		_, err := pm.PRReviewTarget("../../evil")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "invalid pull request number") {
			t.Fatalf("unexpected error: %v", err)
		}
		testutil.VerifyNoRealCommands(t, gitBase)
	})

	t.Run("a PR with no base branch fails before fetching", func(t *testing.T) {
		pm, ghBase, gitBase := newPRTargetSetup()
		scriptTargetGh(ghBase, "octocat/hello", "")

		_, err := pm.PRReviewTarget("213")
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "no base branch") {
			t.Fatalf("unexpected error: %v", err)
		}
		testutil.VerifyNoRealCommands(t, gitBase)
	})

	t.Run("unreadable gh output fails rather than guessing a base", func(t *testing.T) {
		pm, ghBase, gitBase := newPRTargetSetup()
		ghBase.SetExecCommandResults(
			commands.ExecCommandResult("octocat/hello", "", nil),
			commands.ExecCommandResult("not json", "", nil),
		)

		_, err := pm.PRReviewTarget("213")
		if err == nil {
			t.Fatal("expected an error")
		}
		testutil.VerifyNoRealCommands(t, gitBase)
	})

	t.Run("asks gh only for the base branch", func(t *testing.T) {
		// headRefName and the head repository's owner are the fork-remote
		// path ADR-0022 rejected; requesting them would be data with nowhere
		// to go.
		pm, ghBase, gitBase := newPRTargetSetup()
		scriptTargetGh(ghBase, "octocat/hello", "main")
		scriptTargetGit(gitBase, "1\t0\ta.go\n", "M\ta.go\n")

		if _, err := pm.PRReviewTarget("213"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// The count is asserted, not just call [1]'s argv: MockBaseCommand
		// clamps to its last scripted result once the script runs out, so an
		// unexpected extra gh call would succeed silently and leave every
		// indexed assertion here still passing.
		if got := len(ghBase.ExecCommandCalls); got != 2 {
			t.Fatalf(
				"expected exactly 2 gh calls (repo, then the base branch), got %d: %v",
				got, ghBase.ExecCommandCalls,
			)
		}
		want := []string{"pr", "view", "213", "--json", "baseRefName"}
		if got := ghBase.ExecCommandCalls[1].Args; !slices.Equal(got, want) {
			t.Fatalf("unexpected gh args %v, want %v", got, want)
		}
	})
}

func TestValidatePRNumber(t *testing.T) {
	for _, c := range []struct {
		in      string
		wantErr bool
	}{
		{"1", false},
		{"213", false},
		{"99999", false},
		{"", true},
		{"-1", true},
		{"1a", true},
		{"../../etc", true},
		{"1 2", true},
		// Digits, but no pull request GitHub can serve: it numbers PRs from 1
		// and echoes them back without padding. Caught here rather than left to
		// surface as an opaque "couldn't fetch" further down.
		{"0", true},
		{"00", true},
		{"007", true},
	} {
		t.Run(c.in, func(t *testing.T) {
			err := validatePRNumber(c.in)
			if c.wantErr && err == nil {
				t.Fatalf("expected an error for %q", c.in)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", c.in, err)
			}
		})
	}
}

func TestFormatPRReviewTarget(t *testing.T) {
	got := formatPRReviewTarget(prReviewTargetData{
		Base:    mergeBaseSHA,
		Head:    prHeadSHA,
		Journal: "pr/Employ-Inc/employ-agent/213",
		Files: []fileChange{
			{Path: "internal/tooling/task/pr.go", Status: "M", Added: 12, Removed: 3},
			{Path: "docs/spec.md", Status: "A", Added: 4},
		},
	})

	want := "base: " + mergeBaseSHA + "\n" +
		"head: " + prHeadSHA + "\n" +
		"journal: pr/Employ-Inc/employ-agent/213\n" +
		"files:\n" +
		"- internal/tooling/task/pr.go\n" +
		"- docs/spec.md"
	if got != want {
		t.Fatalf("unexpected output:\n%q\nwant:\n%q", got, want)
	}
	// Payload only: the first byte is data, and no line is markdown scaffolding.
	if strings.HasPrefix(got, "#") || strings.Contains(got, "**") {
		t.Fatalf("output carries markdown decoration:\n%s", got)
	}
}
