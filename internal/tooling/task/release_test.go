package task

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
)

// writeReleaseMessage writes content to a temp file and returns its path.
func writeReleaseMessage(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "release-notes.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write release message file: %v", err)
	}
	return path
}

// --- Guards: each must fire before any mutating command runs ---

func TestRelease_InvalidVersion(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")

	_, err := tm.Release("0.12.0", msgFile, false)
	if err == nil {
		t.Fatal("expected error for malformed version")
	}
	if !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("unexpected error: %v", err)
	}
	// No git call at all: the regex guard is cheapest and runs first.
	testutil.VerifyNoRealCommands(t, gitBase)
}

func TestRelease_InvalidVersion_RejectsPrerelease(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")

	_, err := tm.Release("v0.12.0-rc1", msgFile, false)
	if err == nil {
		t.Fatal("expected error for prerelease-suffixed version")
	}
	if !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("unexpected error: %v", err)
	}
	testutil.VerifyNoRealCommands(t, gitBase)
}

func TestRelease_DirtyTree(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult(" M some/file.go\n", "", nil), // status --porcelain
	)

	_, err := tm.Release("v0.12.0", msgFile, false)
	if err == nil {
		t.Fatal("expected error for dirty tree")
	}
	if !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := gitBase.GetExecCommandCallCount(); got != 1 {
		t.Fatalf("expected exactly 1 git call (status check only), got %d", got)
	}
	assertCmd(t, gitBase.ExecCommandCalls[0], "git", "-C", "", "status", "--porcelain")
}

func TestRelease_WrongBranch(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),              // status --porcelain (clean)
		commands.ExecCommandResult("feat/x\n", "", nil),      // branch --show-current
		commands.ExecCommandResult("origin/main\n", "", nil), // symbolic-ref (default branch)
	)

	_, err := tm.Release("v0.12.0", msgFile, false)
	if err == nil {
		t.Fatal("expected error for non-default branch")
	}
	if !strings.Contains(err.Error(), `on branch "feat/x"`) ||
		!strings.Contains(err.Error(), `default branch "main"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := gitBase.GetExecCommandCallCount(); got != 3 {
		t.Fatalf("expected exactly 3 git calls (status, branch check), got %d", got)
	}
}

func TestRelease_MissingMessageFile(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("main\n", "", nil),
		commands.ExecCommandResult("origin/main\n", "", nil),
	)

	_, err := tm.Release("v0.12.0", "/no/such/release-notes.txt", false)
	if err == nil {
		t.Fatal("expected error for missing message file")
	}
	if !strings.Contains(err.Error(), "failed to read --message-file") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := gitBase.GetExecCommandCallCount(); got != 3 {
		t.Fatalf("expected exactly 3 git calls (no tag-exists check yet), got %d", got)
	}
}

func TestRelease_EmptyMessageFile(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("main\n", "", nil),
		commands.ExecCommandResult("origin/main\n", "", nil),
	)
	msgFile := writeReleaseMessage(t, "   \n  ")

	_, err := tm.Release("v0.12.0", msgFile, false)
	if err == nil {
		t.Fatal("expected error for blank message file")
	}
	if !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := gitBase.GetExecCommandCallCount(); got != 3 {
		t.Fatalf("expected exactly 3 git calls, got %d", got)
	}
}

func TestRelease_TagAlreadyExists(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("main\n", "", nil),
		commands.ExecCommandResult("origin/main\n", "", nil),
		commands.ExecCommandResult("v0.12.0\n", "", nil), // tag -l -> already exists
	)

	_, err := tm.Release("v0.12.0", msgFile, false)
	if err == nil {
		t.Fatal("expected error for existing tag")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := gitBase.GetExecCommandCallCount(); got != 4 {
		t.Fatalf("expected exactly 4 git calls (no rev-list/reset/commit/tag), got %d", got)
	}
	assertCmd(t, gitBase.ExecCommandCalls[3], "git", "tag", "-l", "v0.12.0")
}

// --- Happy paths ---

func TestRelease_HappyPath_SquashNoPush(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),              // status --porcelain
		commands.ExecCommandResult("main\n", "", nil),        // branch --show-current
		commands.ExecCommandResult("origin/main\n", "", nil), // symbolic-ref
		commands.ExecCommandResult("", "", nil),              // tag -l (not found)
		commands.ExecCommandResult("3\n", "", nil),           // rev-list --count
		commands.ExecCommandResult("", "", nil),              // reset --soft
		commands.ExecCommandResult("", "", nil),              // commit -F
		commands.ExecCommandResult("", "", nil),              // tag -a
	)

	out, err := tm.Release("v0.12.0", msgFile, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Tagged v0.12.0 (squashed 3 commits). Not pushed — run: git push origin main --tags"
	if out != want {
		t.Fatalf("unexpected output:\ngot:  %q\nwant: %q", out, want)
	}

	calls := gitBase.ExecCommandCalls
	if len(calls) != 8 {
		t.Fatalf("expected exactly 8 git calls, got %d: %+v", len(calls), calls)
	}
	assertCmd(t, calls[4], "git", "rev-list", "--count", "origin/main..HEAD")
	assertCmd(t, calls[5], "git", "reset", "--soft", "HEAD~3")
	assertCmd(t, calls[6], "git", "commit", "--cleanup=verbatim", "-F", msgFile)
	assertCmd(t, calls[7], "git", "tag", "-a", "v0.12.0", "--cleanup=verbatim", "-F", msgFile)
}

// TestRelease_HappyPath_SquashAtBoundary covers ahead=2, the exact threshold
// where `ahead >= 2` flips from false to true — the single conditional that
// decides whether the destructive reset --soft runs at all.
func TestRelease_HappyPath_SquashAtBoundary(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),              // status --porcelain
		commands.ExecCommandResult("main\n", "", nil),        // branch --show-current
		commands.ExecCommandResult("origin/main\n", "", nil), // symbolic-ref
		commands.ExecCommandResult("", "", nil),              // tag -l (not found)
		commands.ExecCommandResult("2\n", "", nil),           // rev-list --count
		commands.ExecCommandResult("", "", nil),              // reset --soft
		commands.ExecCommandResult("", "", nil),              // commit -F
		commands.ExecCommandResult("", "", nil),              // tag -a
	)

	out, err := tm.Release("v0.12.0", msgFile, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Tagged v0.12.0 (squashed 2 commits). Not pushed — run: git push origin main --tags"
	if out != want {
		t.Fatalf("unexpected output:\ngot:  %q\nwant: %q", out, want)
	}

	calls := gitBase.ExecCommandCalls
	if len(calls) != 8 {
		t.Fatalf("expected exactly 8 git calls, got %d: %+v", len(calls), calls)
	}
	assertCmd(t, calls[4], "git", "rev-list", "--count", "origin/main..HEAD")
	assertCmd(t, calls[5], "git", "reset", "--soft", "HEAD~2")
	assertCmd(t, calls[6], "git", "commit", "--cleanup=verbatim", "-F", msgFile)
	assertCmd(t, calls[7], "git", "tag", "-a", "v0.12.0", "--cleanup=verbatim", "-F", msgFile)
}

func TestRelease_HappyPath_SquashWithPush(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("main\n", "", nil),
		commands.ExecCommandResult("origin/main\n", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("3\n", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("", "", nil), // push
	)

	out, err := tm.Release("v0.12.0", msgFile, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Tagged v0.12.0 (squashed 3 commits). Pushed to origin/main."
	if out != want {
		t.Fatalf("unexpected output:\ngot:  %q\nwant: %q", out, want)
	}

	calls := gitBase.ExecCommandCalls
	if len(calls) != 9 {
		t.Fatalf("expected exactly 9 git calls, got %d: %+v", len(calls), calls)
	}
	assertCmd(t, calls[8], "git", "push", "origin", "main", "--tags")
}

func TestRelease_NoSquashNeeded_ZeroAhead(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("main\n", "", nil),
		commands.ExecCommandResult("origin/main\n", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("0\n", "", nil), // rev-list --count
		commands.ExecCommandResult("", "", nil),    // tag -a (no reset/commit)
	)

	out, err := tm.Release("v0.12.0", msgFile, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Tagged v0.12.0 (no unpushed commits). Not pushed — run: git push origin main --tags"
	if out != want {
		t.Fatalf("unexpected output:\ngot:  %q\nwant: %q", out, want)
	}
	if got := gitBase.GetExecCommandCallCount(); got != 6 {
		t.Fatalf("expected exactly 6 git calls (no reset/commit), got %d", got)
	}
	assertCmd(
		t,
		gitBase.ExecCommandCalls[5],
		"git",
		"tag",
		"-a",
		"v0.12.0",
		"--cleanup=verbatim",
		"-F",
		msgFile,
	)
}

func TestRelease_NoSquashNeeded_OneAhead(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("main\n", "", nil),
		commands.ExecCommandResult("origin/main\n", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("1\n", "", nil),
		commands.ExecCommandResult("", "", nil),
	)

	out, err := tm.Release("v0.12.0", msgFile, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Tagged v0.12.0 (1 commit, no squash needed). Not pushed — run: git push origin main --tags"
	if out != want {
		t.Fatalf("unexpected output:\ngot:  %q\nwant: %q", out, want)
	}
	if got := gitBase.GetExecCommandCallCount(); got != 6 {
		t.Fatalf("expected exactly 6 git calls (no reset/commit), got %d", got)
	}
}

// --- Mutation-failure states: each must name what happened and what to run next ---

func TestRelease_ResetSoftFails(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("main\n", "", nil),
		commands.ExecCommandResult("origin/main\n", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("3\n", "", nil),
		commands.ExecCommandResult("", "fatal: ambiguous argument", fmt.Errorf("reset failed")),
	)

	_, err := tm.Release("v0.12.0", msgFile, false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no commits were changed") {
		t.Fatalf("expected error to state no commits changed, got: %v", err)
	}
	if got := gitBase.GetExecCommandCallCount(); got != 6 {
		t.Fatalf("expected exactly 6 git calls (stopped after failed reset), got %d", got)
	}
}

func TestRelease_CommitFailsAfterReset(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("main\n", "", nil),
		commands.ExecCommandResult("origin/main\n", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("3\n", "", nil),
		commands.ExecCommandResult("", "", nil), // reset --soft succeeds
		commands.ExecCommandResult("", "fatal: empty message", fmt.Errorf("commit failed")),
	)

	_, err := tm.Release("v0.12.0", msgFile, false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "staged but uncommitted") {
		t.Fatalf("expected error to describe staged-but-uncommitted state, got: %v", err)
	}
	if !strings.Contains(err.Error(), "git reset --hard ORIG_HEAD") {
		t.Fatalf("expected error to name the undo command, got: %v", err)
	}
}

func TestRelease_TagFailsAfterCommit(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("main\n", "", nil),
		commands.ExecCommandResult("origin/main\n", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("3\n", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("", "fatal: tag exists", fmt.Errorf("tag failed")),
	)

	_, err := tm.Release("v0.12.0", msgFile, false)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tag was not created") {
		t.Fatalf("expected error to describe missing tag, got: %v", err)
	}
	if !strings.Contains(err.Error(), "git tag -a v0.12.0") {
		t.Fatalf("expected error to name the tag command to finish with, got: %v", err)
	}
}

// TestRelease_TagFailsNoSquash covers the ahead=0/1 path where the ahead >= 2
// squash block never runs: the tag-failure message must not claim a commit
// happened, since none did in this invocation.
func TestRelease_TagFailsNoSquash(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("main\n", "", nil),
		commands.ExecCommandResult("origin/main\n", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("0\n", "", nil), // rev-list --count (no squash)
		commands.ExecCommandResult("", "fatal: tag exists", fmt.Errorf("tag failed")),
	)

	_, err := tm.Release("v0.12.0", msgFile, false)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "commit succeeded") {
		t.Fatalf("error must not claim a commit happened when no squash ran, got: %v", err)
	}
	if !strings.Contains(err.Error(), "the tag was not created") {
		t.Fatalf("expected error to describe missing tag, got: %v", err)
	}
	if !strings.Contains(err.Error(), "git tag -a v0.12.0") {
		t.Fatalf("expected error to name the tag command to finish with, got: %v", err)
	}
}

func TestRelease_PushFailsAfterTag(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "release notes")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("main\n", "", nil),
		commands.ExecCommandResult("origin/main\n", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("3\n", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("", "", nil),
		commands.ExecCommandResult("", "fatal: unreachable", fmt.Errorf("push failed")),
	)

	_, err := tm.Release("v0.12.0", msgFile, true)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "push failed") {
		t.Fatalf("expected error to describe the push failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "git push origin main --tags") {
		t.Fatalf("expected error to name the push command to finish with, got: %v", err)
	}
}

// --- Pure helper tests ---

func TestValidateReleaseVersion(t *testing.T) {
	valid := []string{"v0.0.1", "v1.0.0", "v10.20.30"}
	for _, v := range valid {
		if err := validateReleaseVersion(v); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", v, err)
		}
	}

	invalid := []string{"0.12.0", "v0.12", "v0.12.0-rc1", "v0.12.0+build", "v1.2.3.4", ""}
	for _, v := range invalid {
		if err := validateReleaseVersion(v); err == nil {
			t.Errorf("expected %q to be invalid", v)
		}
	}
}

func TestReleaseSummary(t *testing.T) {
	t.Run("squashed", func(t *testing.T) {
		got := releaseSummary("v0.12.0", 3, 3)
		want := "Tagged v0.12.0 (squashed 3 commits)."
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("single commit, no squash", func(t *testing.T) {
		got := releaseSummary("v0.12.0", 1, 0)
		want := "Tagged v0.12.0 (1 commit, no squash needed)."
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("no unpushed commits", func(t *testing.T) {
		got := releaseSummary("v0.12.0", 0, 0)
		want := "Tagged v0.12.0 (no unpushed commits)."
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

// TestRelease_MessageIsPassedVerbatim pins the flag that keeps the release
// notes intact. `git tag -a -F` defaults to --cleanup=strip, which deletes
// every line beginning with '#'. The notes template in
// docs/guides/RELEASE-NOTES-TEMPLATE.md is markdown whose section headings are
// exactly those lines, and .github/workflows/release.yml publishes the GitHub
// release body straight out of this annotation — so without --cleanup=verbatim
// the release page ships with all of its headings silently removed, and a tag
// cannot be rewritten once pushed. The commit path carries the same flag so the
// squashed commit message and the tag message cannot diverge.
func TestRelease_MessageIsPassedVerbatim(t *testing.T) {
	tm, gitBase, _ := newTaskSetup()
	msgFile := writeReleaseMessage(t, "feat: thing\n\n## Section\n\n- bullet\n")
	gitBase.SetExecCommandResults(
		commands.ExecCommandResult("", "", nil),              // status --porcelain
		commands.ExecCommandResult("main\n", "", nil),        // branch --show-current
		commands.ExecCommandResult("origin/main\n", "", nil), // symbolic-ref
		commands.ExecCommandResult("", "", nil),              // tag -l (not found)
		commands.ExecCommandResult("3\n", "", nil),           // rev-list --count
		commands.ExecCommandResult("", "", nil),              // reset --soft
		commands.ExecCommandResult("", "", nil),              // commit -F
		commands.ExecCommandResult("", "", nil),              // tag -a
	)

	if _, err := tm.Release("v0.12.0", msgFile, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// `tag -l` is the existence probe, not the annotate call — match on the
	// full prefix so this asserts against the call that writes the message.
	for _, want := range [][]string{{"commit"}, {"tag", "-a"}} {
		found := false
		for _, call := range gitBase.ExecCommandCalls {
			if len(call.Args) < len(want) || !slices.Equal(call.Args[:len(want)], want) {
				continue
			}
			found = true
			if !slices.Contains(call.Args, "--cleanup=verbatim") {
				t.Fatalf("git %v must pass --cleanup=verbatim, got: %v", want, call.Args)
			}
		}
		if !found {
			t.Fatalf("expected a git %v call, got: %+v", want, gitBase.ExecCommandCalls)
		}
	}
}

// --- The publish half: .github/workflows/release.yml ---

// readReleaseWorkflow returns the release workflow's text. The two tests below
// are the only structural check on the half of a release this package cannot
// reach: `devgeta task release` writes the annotation, and the workflow turns
// that annotation into the GitHub release body. Everything they assert has
// already shipped wrong once.
func readReleaseWorkflow(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", ".github", "workflows", "release.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return string(data)
}

// TestReleaseWorkflow_RefusesALightweightTag pins the fix from commit dd6503a.
// actions/checkout re-fetches the tag being built as a raw commit SHA, leaving a
// lightweight tag whose %(contents) is the COMMIT's message — so reading it does
// not fail, it silently returns the wrong text. Three releases published that
// way before anyone noticed, because a squashed release commit carries the same
// message as its tag. The re-fetch alone is not enough: without the object-type
// check, any future path that leaves a lightweight tag publishes the wrong body
// again with a green job.
func TestReleaseWorkflow_RefusesALightweightTag(t *testing.T) {
	wf := readReleaseWorkflow(t)

	if !strings.Contains(wf, `git fetch --force origin "refs/tags/${VERSION}:refs/tags/${VERSION}"`) {
		t.Error(
			"the workflow no longer re-fetches the annotated tag before reading it; " +
				"actions/checkout overwrites it with a lightweight tag, whose contents " +
				"are the commit message rather than the release notes",
		)
	}
	if !strings.Contains(wf, `git cat-file -t "refs/tags/${VERSION}"`) {
		t.Error(
			"the workflow no longer verifies that the ref is an annotated tag object. " +
				"Reading a lightweight tag returns the wrong text instead of erroring, " +
				"so this check is what turns a silent wrong-body release into a failed job",
		)
	}
}

// TestReleaseWorkflow_ReassertsTheBodyFromTheTag guards the repair path.
// softprops/action-gh-release sets `body` only on a release it CREATES; when a
// release object for the tag already exists it uploads the assets and leaves the
// old body alone. docs/guides/releasing.md's own retry advice produces exactly
// that state, since deleting a tag does not delete its release — which is how
// v1.22.0's page kept a wrong body even after a corrected run read the right
// annotation. The unconditional `gh release edit` is what makes a retry
// self-healing instead of needing a human to spot it.
//
// What this cannot check: that the edit actually succeeds on GitHub, or that the
// preserved tail is the right tail. Only a real release exercises those.
func TestReleaseWorkflow_ReassertsTheBodyFromTheTag(t *testing.T) {
	wf := readReleaseWorkflow(t)

	const edit = `gh release edit "$VERSION" --notes-file release-body.txt`
	editAt := strings.Index(wf, edit)
	if editAt < 0 {
		t.Fatalf(
			"the workflow no longer re-asserts the release body from the tag (%q). "+
				"Without it, a release object that already exists keeps whatever body "+
				"it was first created with, however wrong",
			edit,
		)
	}

	// Order is load bearing: re-asserting before the release exists would edit
	// nothing, so this must follow the step that creates it.
	createAt := strings.Index(wf, "softprops/action-gh-release")
	if createAt < 0 {
		t.Fatal("expected the release-creating action in the workflow")
	}
	if editAt < createAt {
		t.Error(
			"the body re-assertion must run AFTER the release is created — editing a " +
				"release that does not exist yet cannot repair anything",
		)
	}
}
