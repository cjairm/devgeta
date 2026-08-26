package task

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
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

	// The snapshot pointer is inherited from whatever process runs the suite,
	// and loadJournalForDisplay reads it on every review-notes call. A
	// reviewer runs its own `go test ./...` from inside a round, where
	// review-run has set this to that round's snapshot — so without clearing
	// it here, these tests would display an unrelated external journal
	// instead of the one they just wrote, and fail. Clearing it in the shared
	// setup is what keeps a future test from having to remember. The handful
	// of tests that need a pointer set it with their own t.Setenv after this
	// call, which wins; both values are restored at cleanup.
	t.Setenv(ReviewJournalSnapshotEnvVar, "")

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
		// ResolveCommit's `rev-parse --verify <ref>^{commit}` — handoff's HEAD
		// stamp. A fixed default so any test that does not care which sha it
		// gets still resolves one; withHeadResolvesTo (handoff_test.go)
		// overrides this per test.
		case slices.Contains(args, "--verify"):
			return "0000000000000000000000000000000000000000\n", "", nil
		}
		return "", "", nil
	}

	openCodeBase := commands.NewMockBaseCommand()
	tm := &TaskManager{
		Git:      &gitapp.Git{Cmd: commands.NewMockCommand(), Base: gitBase},
		Base:     commands.NewMockBaseCommand(),
		OpenCode: &opencode.OpenCode{Cmd: commands.NewMockCommand(), Base: openCodeBase},
		// ReviewRun's progress lines default to os.Stderr (see
		// TaskManager.progressWriter), which would otherwise spray every
		// test built from this setup across the real test-run stderr. Only
		// the handful of tests that actually assert on progress content
		// override this after the call.
		ProgressOut: io.Discard,
	}
	return tm, root, openCodeBase
}

// withRevContents answers the git calls revision mode makes from an in-test
// table keyed "<rev>:<path>", so a test can give a revision content the
// checkout does not have — the situation revision mode exists for. Every other
// git call keeps answering exactly as newRepoSetup's fixture does.
//
// A revision at least one key names EXISTS: `rev-parse --verify <rev>^{commit}`
// resolves it, and `ls-tree <rev> -- <path>` prints the entry, or — for a path
// the table does not carry — succeeds with EMPTY OUTPUT, which is git's way of
// saying "this revision genuinely has no such path". A revision no key names
// does not exist at all, and both calls fail with a nonzero exit, which is what
// a --rev that was never fetched really does.
//
// Keeping those two failure shapes apart is the point: it is what lets a test
// show the code blaming the path when the path is wrong and the revision when
// the revision is wrong, instead of answering both with "cited path X does not
// exist".
func withRevContents(t *testing.T, tm *TaskManager, contents map[string]string) {
	t.Helper()
	gitBase, ok := tm.Git.Base.(*commands.MockBaseCommand)
	if !ok {
		t.Fatalf("expected a mock git base, got %T", tm.Git.Base)
	}
	revExists := func(rev string) bool {
		for spec := range contents {
			if r, _, found := strings.Cut(spec, ":"); found && r == rev {
				return true
			}
		}
		return false
	}
	orig := gitBase.ExecCommandFn
	gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		args := c.Args
		switch {
		case slices.Contains(args, "ls-tree"):
			// The wrapper always sends the path as a `:(literal)` pathspec, so
			// git looks it up as an exact name rather than a glob; stripping
			// the prefix is how this fixture honors that same semantics.
			rev, path := args[len(args)-3], strings.TrimPrefix(args[len(args)-1], ":(literal)")
			if !revExists(rev) {
				return "", "fatal: Not a valid object name " + rev, errors.New("exit 128")
			}
			content, found := contents[rev+":"+path]
			if !found {
				return "", "", nil
			}
			// Hashed with the same function as the fixture's hash-object case,
			// so a blob id means the same thing in both modes — as it does in
			// real git.
			sum := sha1.Sum([]byte(content))
			return "100644 blob " + hex.EncodeToString(sum[:])[:7] + "\t" + path + "\n", "", nil
		case slices.Contains(args, "rev-parse") && slices.Contains(args, "--verify"):
			rev := strings.TrimSuffix(args[len(args)-1], "^{commit}")
			if !revExists(rev) {
				return "", "fatal: Needed a single revision", errors.New("exit 128")
			}
			return rev + "\n", "", nil
		}
		return orig(c)
	}
}

func TestReviewNotesSentinelWhenEmpty(t *testing.T) {
	tm, _ := newJournalSetup(t)

	out, err := tm.ReviewNotes("", "", false, false)
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

	out, err := tm.ReviewNoteOpen("", "", "store.go:12", "write is not atomic")
	if err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	// Mutations echo verb + target so the caller can reuse the id without
	// re-reading the journal.
	if out != "Noted n1" {
		t.Fatalf("expected 'Noted n1', got %q", out)
	}

	notes, err := tm.ReviewNotes("", "", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	for _, want := range []string{"branch: feat", "open:", "n1", "store.go:12", "[fresh]", "write is not atomic"} {
		if !strings.Contains(notes, want) {
			t.Errorf("expected %q in output:\n%s", want, notes)
		}
	}
}

// --- --rev: reviewing code that is not checked out (ADR-0023 §4) ---

// The reviewed file is not in the checkout at all — the ordinary case for a
// pull request opened from someone else's branch. Without --rev the write is
// refused and the freshness signal is meaningless; with it, both work.
func TestReviewNoteOpenAtRevStampsTheRevisionNotTheCheckout(t *testing.T) {
	tm, _ := newJournalSetup(t)
	withRevContents(t, tm, map[string]string{"9f2c1ab:store.go": "func Write() {}\n"})

	if _, err := tm.ReviewNoteOpen("", "", "store.go:12", "write is not atomic"); err == nil {
		t.Fatal("precondition: without --rev the missing path must be refused")
	}

	out, err := tm.ReviewNoteOpen("", "9f2c1ab", "store.go:12", "write is not atomic")
	if err != nil {
		t.Fatalf("ReviewNoteOpen at rev: %v", err)
	}
	if out != "Noted n1" {
		t.Fatalf("expected 'Noted n1', got %q", out)
	}

	notes, err := tm.ReviewNotes("", "9f2c1ab", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes at rev: %v", err)
	}
	if !strings.Contains(notes, "[fresh]") {
		t.Errorf("entry read at the revision it was stamped at should be fresh:\n%s", notes)
	}
	// The same journal read against the checkout says STALE — which is why the
	// flag exists: that verdict is about the checkout, not the pull request.
	local, err := tm.ReviewNotes("", "", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	if !strings.Contains(local, "STALE") {
		t.Errorf("without --rev the checkout should read stale:\n%s", local)
	}
}

// withRevAlias makes ref resolve to sha, which is what a mutable ref name like
// refs/pull/213/head really is: a name for whatever commit it points at today.
//
// ONLY the resolve is aliased. ls-tree still knows nothing about ref, so any
// lookup that skipped the resolve and passed the ref name straight through
// fails outright instead of quietly working — which is what makes a test able
// to tell the two apart. Layer it over withRevContents, whose contents are
// keyed by the sha.
func withRevAlias(t *testing.T, tm *TaskManager, ref, sha string) {
	t.Helper()
	gitBase, ok := tm.Git.Base.(*commands.MockBaseCommand)
	if !ok {
		t.Fatalf("expected a mock git base, got %T", tm.Git.Base)
	}
	orig := gitBase.ExecCommandFn
	gitBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		args := c.Args
		if slices.Contains(args, "rev-parse") && slices.Contains(args, "--verify") &&
			args[len(args)-1] == ref+"^{commit}" {
			return sha + "\n", "", nil
		}
		return orig(c)
	}
}

// ADR-0023's whole premise is that a review targets an IMMUTABLE commit, so
// what the journal records has to be one. A ref name written down verbatim
// describes a different commit after the next fetch, and two ticks of the same
// review would stamp the same text for two different states, leaving their
// entries incomparable.
func TestReviewNoteAtARefNameStampsTheResolvedSha(t *testing.T) {
	tm, _ := newJournalSetup(t)
	const (
		sha = "9f2c1ab"
		ref = "refs/pull/213/head"
	)
	withRevContents(t, tm, map[string]string{sha + ":store.go": "func Write() {}\n"})
	withRevAlias(t, tm, ref, sha)

	if _, err := tm.ReviewNoteOpen("", ref, "store.go:12", "write is not atomic"); err != nil {
		t.Fatalf("ReviewNoteOpen at a ref name: %v", err)
	}

	j, err := reviewjournal.New(tm.Git).Load("", "feat")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(j.Entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(j.Entries))
	}
	if got := j.Entries[0].Head; got != sha {
		t.Fatalf("head stamp is %q, want the resolved sha %q (never the ref name)", got, sha)
	}

	// The read path resolves too, so freshness is judged at the commit the ref
	// named — not against a ref name git would refuse to look up.
	notes, err := tm.ReviewNotes("", ref, false, false)
	if err != nil {
		t.Fatalf("ReviewNotes at a ref name: %v", err)
	}
	if !strings.Contains(notes, "[fresh]") {
		t.Errorf("entry read at the revision it was stamped at should be fresh:\n%s", notes)
	}
}

// ADR-0012 §3's typo guard, relocated to the revision: existing locally is not
// a substitute for existing in the pull request.
func TestReviewNoteOpenAtRevRejectsPathMissingAtThatRevision(t *testing.T) {
	tm, root := newJournalSetup(t)
	withRevContents(t, tm, map[string]string{"9f2c1ab:store.go": "func Write() {}\n"})
	if err := os.WriteFile(
		filepath.Join(root, "typo.go"),
		[]byte("local only\n"),
		0o644,
	); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := tm.ReviewNoteOpen("", "9f2c1ab", "typo.go:1", "oops")
	if err == nil {
		t.Fatal("expected an error for a path that does not exist at the revision")
	}
	if !strings.Contains(err.Error(), "typo.go") {
		t.Errorf("error should echo the path, got %v", err)
	}
	notes, err := tm.ReviewNotes("", "9f2c1ab", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	if !strings.Contains(notes, "No review notes") {
		t.Errorf("nothing should have been written:\n%s", notes)
	}
}

// A --rev this repository does not have is the likeliest real failure of the
// whole feature: ADR-0023 fetches refs/pull/<n>/head, and a fetch that was
// skipped, failed, or mistyped leaves nothing to resolve against. It must fail
// ONCE, naming the revision — never degrade into "your cited paths are wrong",
// which would send an agent off rewriting correct paths, and never into "every
// entry is stale" on the read path, where Verdict returns a bare string and
// structurally cannot report anything.
func TestReviewNotesRejectsARevisionTheRepositoryDoesNotHave(t *testing.T) {
	tm, _ := newJournalSetup(t)
	withRevContents(t, tm, map[string]string{"9f2c1ab:store.go": "func Write() {}\n"})

	if _, err := tm.ReviewNoteOpen(
		"", "9f2c1ab", "store.go:12", "write is not atomic",
	); err != nil {
		t.Fatalf("setup: ReviewNoteOpen at a good rev: %v", err)
	}

	for _, tc := range []struct {
		name string
		call func() (string, error)
	}{
		{"open", func() (string, error) {
			return tm.ReviewNoteOpen("", "nosucrev", "store.go:12", "n+1")
		}},
		{"settle", func() (string, error) {
			return tm.ReviewNoteSettle("", "nosucrev", "n1", "fixed", "", "done")
		}},
		{"notes", func() (string, error) {
			return tm.ReviewNotes("", "nosucrev", false, false)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := tc.call()
			if err == nil {
				t.Fatalf("a revision the repository does not have must be refused, got %q", out)
			}
			if !strings.Contains(err.Error(), "nosucrev") {
				t.Errorf("the error must name the revision, got %v", err)
			}
			if strings.Contains(err.Error(), "store.go") {
				t.Errorf("a bad revision must not be blamed on the cited path, got %v", err)
			}
			if strings.Contains(out, "STALE") {
				t.Errorf("a bad revision must not read as stale findings, got %q", out)
			}
		})
	}
}

// The next tick of a review passes the pull request's NEW head, so staleness
// means "the pull request changed this file since the finding was written".
func TestReviewNotesAtRevFlipsWhenTheRevisionChangesTheFile(t *testing.T) {
	tm, _ := newJournalSetup(t)
	withRevContents(t, tm, map[string]string{
		"aaa1111:store.go": "v1\n",
		"bbb2222:store.go": "v1\n",             // author pushed, cited file untouched
		"ccc3333:store.go": "v2 — rewritten\n", // author pushed a rewrite
	})

	if _, err := tm.ReviewNoteOpen(
		"",
		"aaa1111",
		"store.go:12",
		"write is not atomic",
	); err != nil {
		t.Fatalf("ReviewNoteOpen at rev: %v", err)
	}

	unchanged, err := tm.ReviewNotes("", "bbb2222", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	if !strings.Contains(unchanged, "[fresh]") {
		t.Errorf("a head that did not touch the cited file must stay fresh:\n%s", unchanged)
	}

	changed, err := tm.ReviewNotes("", "ccc3333", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	if !strings.Contains(changed, "STALE") {
		t.Errorf("a head that rewrote the cited file must be stale:\n%s", changed)
	}
}

// A settle stamps the conclusion at the revision it was formed against, so the
// entry reads fresh at that head. Dropping --rev here would stamp the settle
// against a checkout that does not have the file, leaving the open-time stamp
// and reporting the just-verified fix as stale.
func TestReviewNoteSettleAtRevStampsAtThatRevision(t *testing.T) {
	tm, _ := newJournalSetup(t)
	withRevContents(t, tm, map[string]string{
		"aaa1111:store.go": "v1\n",
		"bbb2222:store.go": "v2 — atomic rename added\n",
	})

	if _, err := tm.ReviewNoteOpen(
		"",
		"aaa1111",
		"store.go:12",
		"write is not atomic",
	); err != nil {
		t.Fatalf("ReviewNoteOpen at rev: %v", err)
	}
	if _, err := tm.ReviewNoteSettle(
		"",
		"bbb2222",
		"n1",
		"fixed",
		"",
		"atomic rename added",
	); err != nil {
		t.Fatalf("ReviewNoteSettle at rev: %v", err)
	}

	notes, err := tm.ReviewNotes("", "bbb2222", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	if !strings.Contains(notes, "settled:") || !strings.Contains(notes, "[fresh]") {
		t.Errorf("a fix settled at the new head must read fresh there:\n%s", notes)
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
	if _, err := tm.ReviewNoteOpen("", "", "store.go:12", "not atomic"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	if err := os.WriteFile(path, []byte("v2-dirty\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	notes, err := tm.ReviewNotes("", "", false, false)
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

// A rejection renders its own marker, and keeps it after the cited file changes.
// This is what a reviewer actually reads, so it is asserted at this layer too:
// the manager can exempt a rejection all it likes, but if the rendered line still
// said "re-check before trusting it" the next round would re-litigate a settled
// decision anyway. The instruction the marker carries has to match the reviewer
// prompts' own rule about a rejected entry.
func TestReviewNotesMarksRejectionStandingThroughAnEdit(t *testing.T) {
	tm, root := newJournalSetup(t)
	path := filepath.Join(root, "client.go")
	if err := os.WriteFile(path, []byte("batched\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := tm.ReviewNoteOpen("", "", "client.go:42", "N+1 query"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	if _, err := tm.ReviewNoteSettle(
		"", "", "n1", "rejected", "", "intentional, capped by config.MaxBatch",
	); err != nil {
		t.Fatalf("ReviewNoteSettle: %v", err)
	}
	if err := os.WriteFile(path, []byte("batched\nunrelated fix\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	notes, err := tm.ReviewNotes("", "", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	if !strings.Contains(notes, "STANDING") {
		t.Fatalf("a rejection should render its own marker:\n%s", notes)
	}
	if strings.Contains(notes, "STALE") {
		t.Errorf("an edit to the cited file must not stale a rejection:\n%s", notes)
	}
	if !strings.Contains(notes, "re-read the reason") {
		t.Errorf("the marker should say what to do instead of re-checking:\n%s", notes)
	}
}

// A multi-paragraph settle note reaches the reader whole, and the entry keeps the
// freshness marker that tells the next round it is settled. Both were lost to the
// same parse bug: the blank line between paragraphs ended the entry, dropping the
// rest of the note and the stamp behind it.
func TestReviewNotesKeepsMultiParagraphNoteAndFreshness(t *testing.T) {
	tm, root := newJournalSetup(t)
	if err := os.WriteFile(filepath.Join(root, "store.go"), []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := tm.ReviewNoteOpen("", "", "store.go:12", "write is not atomic"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	if _, err := tm.ReviewNoteSettle(
		"", "", "n1", "fixed", "",
		"switched to a temp file plus rename.\n\nran: go test ./pkg/files — all green.",
	); err != nil {
		t.Fatalf("ReviewNoteSettle: %v", err)
	}

	notes, err := tm.ReviewNotes("", "", false, false)
	if err != nil {
		t.Fatalf("ReviewNotes: %v", err)
	}
	if !strings.Contains(notes, "all green") {
		t.Errorf("the second paragraph of the settle note was dropped:\n%s", notes)
	}
	if !strings.Contains(notes, "[fresh]") {
		t.Errorf("the entry lost the stamp that makes it judgeable:\n%s", notes)
	}
}

func TestReviewNoteSettleByIDMovesEntryAndEchoesID(t *testing.T) {
	tm, _ := newJournalSetup(t)
	if _, err := tm.ReviewNoteOpen("", "", "", "Does retry reuse the outer context?"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}

	out, err := tm.ReviewNoteSettle("", "", "n1", "answered", "", "yes, ctx is threaded through")
	if err != nil {
		t.Fatalf("ReviewNoteSettle: %v", err)
	}
	if out != "Settled n1 (answered)" {
		t.Fatalf("expected 'Settled n1 (answered)', got %q", out)
	}

	notes, _ := tm.ReviewNotes("", "", false, false)
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
	if _, err := tm.ReviewNoteOpen("", "", "", "q"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}

	_, err := tm.ReviewNoteSettle("", "", "n1", "answered", "other.go:1", "a")
	if err == nil {
		t.Fatal("expected an error when --at accompanies --settle <id>")
	}
	if !strings.Contains(err.Error(), "carries over") {
		t.Errorf("error should explain why, got %v", err)
	}
}

func TestReviewNoteSettleDirectWithoutID(t *testing.T) {
	tm, _ := newJournalSetup(t)

	out, err := tm.ReviewNoteSettle("", "", "", "rejected", "", "intentional, capped by config")
	if err != nil {
		t.Fatalf("ReviewNoteSettle: %v", err)
	}
	if out != "Settled n1 (rejected)" {
		t.Fatalf("expected 'Settled n1 (rejected)', got %q", out)
	}
}

func TestReviewNoteRejectsBadInput(t *testing.T) {
	tm, _ := newJournalSetup(t)

	if _, err := tm.ReviewNoteOpen("", "", "", "   "); err == nil {
		t.Error("expected an error for an empty --note")
	}
	if _, err := tm.ReviewNoteSettle("", "", "", "maybe", "", "x"); err == nil {
		t.Error("expected an error for an invalid --as")
	}
	if _, err := tm.ReviewNoteOpen("", "", "missing.go:1", "x"); err == nil {
		t.Error("expected an error for a cited path that does not exist")
	}
}

func TestReviewNotesPathPrintsJournalLocation(t *testing.T) {
	tm, root := newJournalSetup(t)

	out, err := tm.ReviewNotes("", "", true, false)
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

	out, err := tm.ReviewNotes("", "", false, false)
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

	if _, err := tm.ReviewNoteOpen("", "", "", "q"); err == nil {
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

	if _, err := tm.WorktreeFinish("spike", false, true, false, false); err != nil {
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

	out, err := tm.ReviewNotes("", "", false, true)
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
	if _, err := tm.ReviewNoteOpen("", "", "", "N+1 query"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	if _, err := tm.ReviewNoteSettle(
		"", "", "n1", "rejected", "", reviewjournal.AgentNotePrefix+"looks intentional",
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

	notes, _ := tm.ReviewNotes("", "", false, false)
	if !strings.Contains(notes, "looks intentional") {
		t.Errorf("the reason must survive: %s", notes)
	}
	if strings.Contains(notes, reviewjournal.AgentNotePrefix) {
		t.Errorf("the agent prefix must be gone after ratifying:\n%s", notes)
	}
}

func TestReviewNoteRatifyOnAnythingElseErrors(t *testing.T) {
	tm, _ := newJournalSetup(t)
	if _, err := tm.ReviewNoteOpen("", "", "", "still open"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}

	if _, err := tm.ReviewNoteRatify("", "n1"); err == nil {
		t.Error("expected an error ratifying an open entry")
	}
	if _, err := tm.ReviewNoteRatify("", "n9"); err == nil {
		t.Error("expected an error ratifying an unknown id")
	}

	if _, err := tm.ReviewNoteSettle("", "", "n1", "fixed", "", "done"); err != nil {
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
	if _, err := tm.ReviewNoteOpen("", "", "", "N+1 query"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	if _, err := tm.ReviewNoteSettle(
		"", "", "n1", "rejected", "", reviewjournal.AgentNotePrefix+"looks intentional",
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

	notes, _ := tm.ReviewNotes("", "", false, false)
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

	if _, err := tm.ReviewNoteOpen("", "", "", "q"); err != nil {
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
	if _, err := tm.ReviewNoteOpen("", "", "", "first finding"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	if _, err := tm.ReviewNoteSettle("", "", "n1", "answered", "", "resolved"); err != nil {
		t.Fatalf("ReviewNoteSettle: %v", err)
	}

	viaTask, err := tm.ReviewNotes("", "", false, false)
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
	if _, err := tm.ReviewNoteOpen("", "", "", "live finding"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	t.Setenv(ReviewJournalSnapshotEnvVar, "")

	out, err := tm.ReviewNotes("", "", false, false)
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
	if _, err := tm.ReviewNoteOpen("", "", "", "round-start finding"); err != nil {
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
	if _, err := tm.ReviewNoteOpen("", "", "", "same-round finding"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}

	t.Setenv(ReviewJournalSnapshotEnvVar, snapshotPath)
	out, err := tm.ReviewNotes("", "", false, false)
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
	if _, err := tm.ReviewNoteOpen("", "", "", "live finding"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	t.Setenv(ReviewJournalSnapshotEnvVar, filepath.Join(root, "no-such-snapshot.md"))

	out, err := tm.ReviewNotes("", "", false, false)
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
	if _, err := tm.ReviewNoteOpen("", "", "", "live finding"); err != nil {
		t.Fatalf("ReviewNoteOpen: %v", err)
	}
	dirAsPointer := filepath.Join(root, "not-a-file")
	if err := os.MkdirAll(dirAsPointer, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Setenv(ReviewJournalSnapshotEnvVar, dirAsPointer)

	out, err := tm.ReviewNotes("", "", false, false)
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

	out, err := tm.ReviewNoteOpen("", "", "", "written while pointer is set")
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
