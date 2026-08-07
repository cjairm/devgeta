package task

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	gitapp "github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/opencode"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/tooling/reviewjournal"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// newJournalSetup is newRepoSetup on the branch these journal tests use.
func newJournalSetup(t *testing.T) (*TaskManager, string) {
	t.Helper()
	tm, root, _ := newRepoSetup(t, "feat")
	return tm, root
}

// newRepoSetup builds a TaskManager whose git calls are answered against a
// real temp directory, so these tests exercise the actual journal file the
// commands write — not a stubbed manager. The process cwd is moved into the
// fake repo because the task layer deliberately passes "" as the repo dir
// (every task runs in the caller's repo).
//
// branch is what `git branch --show-current` reports; pass "" for a detached
// HEAD, which prints nothing and exits 0. The default branch always resolves
// to "main".
//
// The third return is the mock base the OpenCode wrapper runs through, so a
// review-run test can script `opencode run`'s output and inspect the exact
// command line and environment the wrapper assembled. tm.Base is a separate
// mock that nothing under test should ever touch.
func newRepoSetup(
	t *testing.T,
	branch string,
) (*TaskManager, string, *commands.MockBaseCommand) {
	t.Helper()
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Chdir(root)

	gitBase := commands.NewMockBaseCommand()
	gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		args := c.Args
		switch {
		case slices.Contains(args, "--git-common-dir"):
			return gitDir + "\n", "", nil
		case slices.Contains(args, "--show-current"):
			return branch + "\n", "", nil
		case slices.Contains(args, "hash-object"):
			data, err := os.ReadFile(filepath.Join(root, args[len(args)-1]))
			if err != nil {
				return "", "fatal: Cannot open", errors.New("exit 128")
			}
			sum := sha1.Sum(data)
			return hex.EncodeToString(sum[:])[:7] + "\n", "", nil
		case slices.Contains(args, "rev-list"):
			// review-run's empty-diff guard (checkBranchHasCommittedDiff)
			// needs a nonzero ahead count by default so every existing test
			// that expects the round to proceed keeps proceeding;
			// withAheadCount (reviewrun_test.go) overrides this per test for
			// the guard's own refusal/allow tests.
			return "0\t3\n", "", nil // behind 0, ahead 3
		// Ahead of the "--short" case below on purpose: the default-branch
		// query is `symbolic-ref --short refs/remotes/origin/HEAD`, so
		// matching on "--short" first would answer it with HEAD's SHA and
		// make the repo's default branch look like a commit.
		case slices.Contains(args, "symbolic-ref"):
			return "origin/main\n", "", nil
		case slices.Contains(args, "--short"):
			return "abc1234\n", "", nil
		}
		return "", "", nil
	}

	openCodeBase := commands.NewMockBaseCommand()
	tm := &TaskManager{
		Git:      &gitapp.Git{Cmd: commands.NewMockCommand(), Base: gitBase},
		Base:     commands.NewMockBaseCommand(),
		OpenCode: &opencode.OpenCode{Cmd: commands.NewMockCommand(), Base: openCodeBase},
	}
	return tm, root, openCodeBase
}

func TestReviewNotesSentinelWhenEmpty(t *testing.T) {
	tm, _ := newJournalSetup(t)

	out, err := tm.ReviewNotes("", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// An empty result must be a fixed sentence, never empty stdout: an agent
	// cannot tell empty output from a crash (task-design.md output rule 4).
	if out != "No review notes for branch feat." {
		t.Fatalf("expected the empty sentinel, got %q", out)
	}
}

func TestReviewNoteOpenThenNotesShowsEntry(t *testing.T) {
	tm, root := newJournalSetup(t)
	if err := os.WriteFile(filepath.Join(root, "store.go"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	out, err := tm.ReviewNoteOpen("", "store.go:12", "write is not atomic")
	if err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	// Mutations echo verb + target so the caller can reuse the id without
	// re-reading the journal.
	if out != "Noted n1" {
		t.Fatalf("expected 'Noted n1', got %q", out)
	}

	notes, err := tm.ReviewNotes("", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	for _, want := range []string{"branch: feat", "open:", "n1", "store.go:12", "[fresh]", "write is not atomic"} {
		if !strings.Contains(notes, want) {
			t.Errorf("expected %q in output:\n%s", want, notes)
		}
	}
}

// The central behavior: after an uncommitted edit to the cited file, the entry
// reads STALE — and the reader is told to re-check rather than left to work it
// out.
func TestReviewNotesMarksStaleAfterDirtyEdit(t *testing.T) {
	tm, root := newJournalSetup(t)
	path := filepath.Join(root, "store.go")
	if err := os.WriteFile(path, []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := tm.ReviewNoteOpen("", "store.go:12", "not atomic"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	if err := os.WriteFile(path, []byte("v2-dirty\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	notes, err := tm.ReviewNotes("", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	if !strings.Contains(notes, "STALE") {
		t.Fatalf("expected a STALE marker after a dirty edit:\n%s", notes)
	}
	if !strings.Contains(notes, "re-check") {
		t.Errorf("the stale marker should tell the reader what to do:\n%s", notes)
	}
}

func TestReviewNoteSettleByIDMovesEntryAndEchoesID(t *testing.T) {
	tm, _ := newJournalSetup(t)
	if _, err := tm.ReviewNoteOpen("", "", "Does retry reuse the outer context?"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}

	out, err := tm.ReviewNoteSettle("", "n1", "answered", "", "yes, ctx is threaded through")
	if err != nil {
		t.Fatalf("ReviewNoteSettle: %v", err)
	}
	if out != "Settled n1 (answered)" {
		t.Fatalf("expected 'Settled n1 (answered)', got %q", out)
	}

	notes, _ := tm.ReviewNotes("", false, false)
	if !strings.Contains(notes, "settled:") || !strings.Contains(notes, "answered") {
		t.Errorf("entry should appear as settled:\n%s", notes)
	}
	if strings.Contains(notes, "open:") {
		t.Errorf("nothing should remain open:\n%s", notes)
	}
}

// --at alongside --settle <id> is refused: the cite carries over from the open
// entry, so accepting a second path would let an answer silently retarget the
// question it closes.
func TestReviewNoteSettleByIDRejectsAt(t *testing.T) {
	tm, _ := newJournalSetup(t)
	if _, err := tm.ReviewNoteOpen("", "", "q"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}

	_, err := tm.ReviewNoteSettle("", "n1", "answered", "other.go:1", "a")
	if err == nil {
		t.Fatal("expected an error when --at accompanies --settle <id>")
	}
	if !strings.Contains(err.Error(), "carries over") {
		t.Errorf("error should explain why, got %v", err)
	}
}

func TestReviewNoteSettleDirectWithoutID(t *testing.T) {
	tm, _ := newJournalSetup(t)

	out, err := tm.ReviewNoteSettle("", "", "rejected", "", "intentional, capped by config")
	if err != nil {
		t.Fatalf("ReviewNoteSettle: %v", err)
	}
	if out != "Settled n1 (rejected)" {
		t.Fatalf("expected 'Settled n1 (rejected)', got %q", out)
	}
}

func TestReviewNoteRejectsBadInput(t *testing.T) {
	tm, _ := newJournalSetup(t)

	if _, err := tm.ReviewNoteOpen("", "", "   "); err == nil {
		t.Error("expected an error for an empty --note")
	}
	if _, err := tm.ReviewNoteSettle("", "", "maybe", "", "x"); err == nil {
		t.Error("expected an error for an invalid --as")
	}
	if _, err := tm.ReviewNoteOpen("", "missing.go:1", "x"); err == nil {
		t.Error("expected an error for a cited path that does not exist")
	}
}

func TestReviewNotesPathPrintsJournalLocation(t *testing.T) {
	tm, root := newJournalSetup(t)

	out, err := tm.ReviewNotes("", true, false)
	if err != nil {
		t.Fatalf("ReviewNotes --path: %v", err)
	}
	want := filepath.Join(root, ".git", "devgeta", "review", "feat.md")
	if out != want {
		t.Fatalf("expected %q, got %q", want, out)
	}
}

// A detached HEAD has no branch, so there is no journal — reported as a fixed
// sentinel rather than inventing a key from the SHA.
func TestReviewNotesDetachedHeadSentinel(t *testing.T) {
	tm, _ := newJournalSetup(t)
	tm.Git.Base.(*commands.MockBaseCommand).ExecCommandFn = func(
		c commands.CommandParams,
	) (string, string, error) {
		if slices.Contains(c.Args, "--show-current") {
			return "\n", "", nil // detached: empty branch name
		}
		return "", "", nil
	}

	out, err := tm.ReviewNotes("", false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "No branch — review notes are keyed by branch." {
		t.Fatalf("expected the no-branch sentinel, got %q", out)
	}
}

func TestReviewNoteRefusesToWriteOnDetachedHead(t *testing.T) {
	tm, _ := newJournalSetup(t)
	tm.Git.Base.(*commands.MockBaseCommand).ExecCommandFn = func(
		c commands.CommandParams,
	) (string, string, error) {
		if slices.Contains(c.Args, "--show-current") {
			return "\n", "", nil
		}
		return "", "", nil
	}

	if _, err := tm.ReviewNoteOpen("", "", "q"); err == nil {
		t.Fatal("expected an error writing with no branch")
	}
}

// worktree-finish deletes the branch through Git.RemoveWorktree directly, not
// through WorktreeManager.removeByRepo, so it does not inherit that path's
// cleanup — it needs its own, or a finished branch's journal outlives the work
// it describes. This asserts the whole path, not just the helper.
func TestWorktreeFinishDeletesTheBranchsJournal(t *testing.T) {
	root := t.TempDir()
	mainWorktree := filepath.Join(root, "main")
	gitDir := filepath.Join(mainWorktree, ".git")
	reviewDir := filepath.Join(gitDir, "devgeta", "review")
	if err := os.MkdirAll(reviewDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	journal := filepath.Join(reviewDir, "spike.md")
	sibling := filepath.Join(reviewDir, "keep-me.md")
	for _, p := range []string{journal, sibling} {
		if err := os.WriteFile(p, []byte("---\n---\n"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	wtPath := filepath.Join(worktree.GetWorktreeBasePath(), uniqueRepoSlug(t), "spike")
	if err := os.MkdirAll(wtPath, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(filepath.Dir(wtPath))
	})

	porcelainWt := "worktree " + wtPath + "\nHEAD abc123\nbranch refs/heads/spike\n\n"
	porcelainMain := "worktree " + mainWorktree + "\nHEAD def456\nbranch refs/heads/main\n\n"

	gitBase := commands.NewMockBaseCommand()
	calls := 0
	gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		calls++
		switch {
		case slices.Contains(c.Args, "--git-common-dir"):
			return gitDir + "\n", "", nil
		case slices.Contains(c.Args, "status"):
			// Empty status = clean, so the discard path proceeds without --force.
			// Matched before the porcelain case below, since `status --porcelain`
			// carries that flag too.
			return "", "", nil
		case slices.Contains(c.Args, "worktree"):
			// The first `worktree list --porcelain` resolves the target worktree;
			// later ones resolve the main checkout.
			if calls == 1 {
				return porcelainWt, "", nil
			}
			return porcelainMain, "", nil
		}
		return "", "", nil
	}
	tm := &TaskManager{
		Git:  &gitapp.Git{Cmd: commands.NewMockCommand(), Base: gitBase},
		Base: commands.NewMockBaseCommand(),
	}

	if _, err := tm.WorktreeFinish("spike", false, true, false); err != nil {
		t.Fatalf("WorktreeFinish: %v", err)
	}

	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Error("the finished branch's review journal should be gone")
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Errorf("another branch's journal must survive: %v", err)
	}
}

func TestReviewNotesPruneSentinelWhenNothingToDo(t *testing.T) {
	tm, _ := newJournalSetup(t)

	out, err := tm.ReviewNotes("", false, true)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if out != "No review journals to prune." {
		t.Fatalf("expected the prune sentinel, got %q", out)
	}
}

// --- ratify / reopen (ADR-0017 §6) ---

func TestReviewNoteRatifyStripsAgentPrefixAndEchoesID(t *testing.T) {
	tm, _ := newJournalSetup(t)
	if _, err := tm.ReviewNoteOpen("", "", "N+1 query"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	if _, err := tm.ReviewNoteSettle(
		"", "n1", "rejected", "", reviewjournal.AgentNotePrefix+"looks intentional",
	); err != nil {
		t.Fatalf("ReviewNoteSettle: %v", err)
	}

	out, err := tm.ReviewNoteRatify("", "n1")
	if err != nil {
		t.Fatalf("ReviewNoteRatify: %v", err)
	}
	if out != "Ratified n1" {
		t.Fatalf("expected 'Ratified n1', got %q", out)
	}

	notes, _ := tm.ReviewNotes("", false, false)
	if !strings.Contains(notes, "looks intentional") {
		t.Errorf("the reason must survive: %s", notes)
	}
	if strings.Contains(notes, reviewjournal.AgentNotePrefix) {
		t.Errorf("the agent prefix must be gone after ratifying:\n%s", notes)
	}
}

func TestReviewNoteRatifyOnAnythingElseErrors(t *testing.T) {
	tm, _ := newJournalSetup(t)
	if _, err := tm.ReviewNoteOpen("", "", "still open"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}

	if _, err := tm.ReviewNoteRatify("", "n1"); err == nil {
		t.Error("expected an error ratifying an open entry")
	}
	if _, err := tm.ReviewNoteRatify("", "n9"); err == nil {
		t.Error("expected an error ratifying an unknown id")
	}

	if _, err := tm.ReviewNoteSettle("", "n1", "fixed", "", "done"); err != nil {
		t.Fatalf("ReviewNoteSettle: %v", err)
	}
	if _, err := tm.ReviewNoteRatify("", "n1"); err == nil {
		t.Error("expected an error ratifying a fixed (non-rejected) entry")
	}
}

func TestReviewNoteRatifyRequiresID(t *testing.T) {
	tm, _ := newJournalSetup(t)
	if _, err := tm.ReviewNoteRatify("", ""); err == nil {
		t.Error("expected an error for --ratify without an id")
	}
}

func TestReviewNoteReopenReturnsSameIDToOpenWithCountUnchanged(t *testing.T) {
	tm, _ := newJournalSetup(t)
	if _, err := tm.ReviewNoteOpen("", "", "N+1 query"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	if _, err := tm.ReviewNoteSettle(
		"", "n1", "rejected", "", reviewjournal.AgentNotePrefix+"looks intentional",
	); err != nil {
		t.Fatalf("ReviewNoteSettle: %v", err)
	}

	out, err := tm.ReviewNoteReopen("", "n1")
	if err != nil {
		t.Fatalf("ReviewNoteReopen: %v", err)
	}
	if out != "Reopened n1" {
		t.Fatalf("expected 'Reopened n1', got %q", out)
	}

	notes, _ := tm.ReviewNotes("", false, false)
	if !strings.Contains(notes, "open:") || strings.Contains(notes, "settled:") {
		t.Errorf("entry should be open again, nothing settled:\n%s", notes)
	}
	if !strings.Contains(notes, "N+1 query") {
		t.Errorf("original finding text must survive:\n%s", notes)
	}
	if strings.Contains(notes, "looks intentional") {
		t.Errorf("the resolution note must be dropped:\n%s", notes)
	}
}

func TestReviewNoteReopenOfNonexistentOrOpenIDErrors(t *testing.T) {
	tm, _ := newJournalSetup(t)
	if _, err := tm.ReviewNoteReopen("", "n9"); err == nil {
		t.Error("expected an error reopening an unknown id")
	}

	if _, err := tm.ReviewNoteOpen("", "", "q"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	if _, err := tm.ReviewNoteReopen("", "n1"); err == nil {
		t.Error("expected an error reopening an already-open entry")
	}
}

func TestReviewNoteReopenRequiresID(t *testing.T) {
	tm, _ := newJournalSetup(t)
	if _, err := tm.ReviewNoteReopen("", ""); err == nil {
		t.Error("expected an error for --reopen without an id")
	}
}

// --- round-start snapshot read (ADR-0017 §4) ---
//
// review-run's snapshot writer and the executor's env overlay belong to other
// tasks. These tests only cover the read side: review-notes consults
// ReviewJournalSnapshotEnvVar and falls back to the live journal on any
// failure to resolve it.

// The regression that matters most: with the pointer unset (today's world),
// review-notes must produce byte-identical output to reading the live
// journal directly, since review-notes is used by hand and by every agent
// outside the review loop, not just inside a round.
func TestReviewNotesSnapshotPointerUnsetMatchesLiveJournal(t *testing.T) {
	tm, _ := newJournalSetup(t)
	if _, err := tm.ReviewNoteOpen("", "", "first finding"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	if _, err := tm.ReviewNoteSettle("", "n1", "answered", "", "resolved"); err != nil {
		t.Fatalf("ReviewNoteSettle: %v", err)
	}

	viaTask, err := tm.ReviewNotes("", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}

	jm := reviewjournal.New(tm.Git)
	j, err := jm.Load("", "feat")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	viaLive := renderNotes(jm, j)

	if viaTask != viaLive {
		t.Fatalf(
			"pointer-unset output diverged from the live journal:\ngot:  %q\nwant: %q",
			viaTask,
			viaLive,
		)
	}
}

// Pointer set to the empty string is explicitly the same as unset, not "an
// empty snapshot" — the empty string never names a file.
func TestReviewNotesSnapshotPointerEmptyStringUsesLiveJournal(t *testing.T) {
	tm, _ := newJournalSetup(t)
	if _, err := tm.ReviewNoteOpen("", "", "live finding"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	t.Setenv(ReviewJournalSnapshotEnvVar, "")

	out, err := tm.ReviewNotes("", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	if !strings.Contains(out, "live finding") {
		t.Fatalf("expected the live journal's entry, got:\n%s", out)
	}
}

// The isolation case: a reviewer pointed at a snapshot taken before a
// second, later entry was opened must not see that later entry, even though
// it is sitting in the live journal file right next to it.
func TestReviewNotesSnapshotPointerReadsSnapshotNotLive(t *testing.T) {
	tm, root := newJournalSetup(t)
	if _, err := tm.ReviewNoteOpen("", "", "round-start finding"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}

	jm := reviewjournal.New(tm.Git)
	journalPath, err := jm.PathFor("", "feat")
	if err != nil {
		t.Fatalf("PathFor: %v", err)
	}
	snapshotData, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatalf("reading the live journal to build the snapshot: %v", err)
	}
	snapshotPath := filepath.Join(root, "feat.round-1.snapshot.md")
	if err := os.WriteFile(snapshotPath, snapshotData, 0o644); err != nil {
		t.Fatalf("writing snapshot: %v", err)
	}

	// Diverge the live journal after the snapshot was taken.
	if _, err := tm.ReviewNoteOpen("", "", "same-round finding"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}

	t.Setenv(ReviewJournalSnapshotEnvVar, snapshotPath)
	out, err := tm.ReviewNotes("", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	if !strings.Contains(out, "round-start finding") {
		t.Errorf("expected the snapshot's entry in output:\n%s", out)
	}
	if strings.Contains(out, "same-round finding") {
		t.Errorf("a same-round live entry must be invisible when reading the snapshot:\n%s", out)
	}
}

// A missing snapshot is an anomaly (step 1 always writes one before a round
// starts), not the normal path — but review-notes must never fail because of
// it; it falls back to the live journal.
func TestReviewNotesSnapshotPointerMissingFileFallsBackToLive(t *testing.T) {
	tm, root := newJournalSetup(t)
	if _, err := tm.ReviewNoteOpen("", "", "live finding"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	t.Setenv(ReviewJournalSnapshotEnvVar, filepath.Join(root, "no-such-snapshot.md"))

	out, err := tm.ReviewNotes("", false, false)
	if err != nil {
		t.Fatalf("expected no error falling back to the live journal, got: %v", err)
	}
	if !strings.Contains(out, "live finding") {
		t.Fatalf("expected the live journal's entry, got:\n%s", out)
	}
}

// An unreadable snapshot (here: the pointer names a directory, so
// os.ReadFile fails) falls back the same way a missing one does.
func TestReviewNotesSnapshotPointerUnreadableFileFallsBackToLive(t *testing.T) {
	tm, root := newJournalSetup(t)
	if _, err := tm.ReviewNoteOpen("", "", "live finding"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	dirAsPointer := filepath.Join(root, "not-a-file")
	if err := os.MkdirAll(dirAsPointer, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Setenv(ReviewJournalSnapshotEnvVar, dirAsPointer)

	out, err := tm.ReviewNotes("", false, false)
	if err != nil {
		t.Fatalf("expected no error falling back to the live journal, got: %v", err)
	}
	if !strings.Contains(out, "live finding") {
		t.Fatalf("expected the live journal's entry, got:\n%s", out)
	}
}

// Writes must keep hitting the live journal regardless of the pointer: the
// snapshot is a read-side illusion, not a second store.
func TestReviewNoteWritesStillHitLiveJournalWhileSnapshotPointerSet(t *testing.T) {
	tm, root := newJournalSetup(t)
	t.Setenv(ReviewJournalSnapshotEnvVar, filepath.Join(root, "no-such-snapshot.md"))

	out, err := tm.ReviewNoteOpen("", "", "written while pointer is set")
	if err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	if out != "Noted n1" {
		t.Fatalf("expected 'Noted n1', got %q", out)
	}

	// Read the live file directly (bypassing loadJournalForDisplay) to prove
	// the write landed in the live journal, not somewhere the pointer
	// redirected it.
	jm := reviewjournal.New(tm.Git)
	j, err := jm.Load("", "feat")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(j.Entries) != 1 || j.Entries[0].Note != "written while pointer is set" {
		t.Fatalf("expected the write in the live journal, got entries: %+v", j.Entries)
	}
}
