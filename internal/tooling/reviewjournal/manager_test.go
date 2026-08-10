package reviewjournal

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/commands"
)

// fakeRepo wires a Manager whose git calls are answered from the real files in
// a temp directory, so staleness is exercised against actual content rather
// than a canned hash. hash-object hashes the file's CURRENT bytes — the whole
// point of ADR-0012 §2 — so a dirty edit genuinely moves the hash here.
type fakeRepo struct {
	t        *testing.T
	mgr      *Manager
	repoDir  string
	gitDir   string
	branches []string // "local" branches, for prune
	remotes  []string
	// revs is the committed history the checkout cannot show: rev -> path ->
	// content, answered by `ls-tree <rev> -- <path>`. It is what makes
	// a revision-mode test able to disagree with the working tree, which is
	// the whole point of that mode.
	revs map[string]map[string]string
	// revTrees records paths that are DIRECTORIES at a revision. git answers
	// those with a tree object and exit 0, so a fixture that only knew about
	// blobs could not show whether the code rejects a cite naming a directory
	// (ADR-0012 §3's typo guard) or stamps an entry with a tree hash.
	revTrees map[string]map[string]bool
	// revErrs makes a revision fail the way a broken lookup does — nonzero
	// exit with git's message — as opposed to the exit-0-with-no-output that
	// means "this revision genuinely has no such path". The two are different
	// answers to different questions, and only a fixture that can produce both
	// shapes can prove the code keeps them apart.
	revErrs map[string]string
}

func newFakeRepo(t *testing.T) *fakeRepo {
	t.Helper()
	root := t.TempDir()
	repoDir := filepath.Join(root, "work")
	gitDir := filepath.Join(root, "work", ".git")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	fr := &fakeRepo{t: t, repoDir: repoDir, gitDir: gitDir}

	mockBase := commands.NewMockBaseCommand()
	mockBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		args := c.Args
		switch {
		case slices.Contains(args, "--git-common-dir"):
			return fr.gitDir + "\n", "", nil
		// `ls-tree --full-tree <rev> -- <path>`, answered from fr.revs — the
		// history a checkout cannot show. It reproduces all THREE shapes real
		// git answers with, because the code under test now branches on which
		// one it gets: a revision git cannot resolve fails with a nonzero
		// exit, a resolvable revision with no such entry succeeds with EMPTY
		// output, and a hit prints one "<mode> <type> <sha>\t<path>" line.
		// Blobs are hashed with the same function as the hash-object case
		// below so the two modes produce comparable blob ids, exactly as real
		// git does.
		case slices.Contains(args, "ls-tree"):
			// The wrapper always sends the path as a `:(literal)` pathspec, so
			// git treats it as an exact name rather than a glob. This fixture
			// looks paths up exactly, which is that same semantics — stripping
			// the prefix is how it reads the pathspec git would have honored.
			rev, path := args[len(args)-3], strings.TrimPrefix(args[len(args)-1], ":(literal)")
			if stderr, failing := fr.revErrs[rev]; failing {
				return "", stderr, errors.New("exit 128")
			}
			tree, resolvable := fr.revs[rev]
			if !resolvable {
				return "", "fatal: Not a valid object name " + rev, errors.New("exit 128")
			}
			if fr.revTrees[rev][path] {
				return "040000 tree 1111111\t" + path + "\n", "", nil
			}
			content, ok := tree[path]
			if !ok {
				return "", "", nil
			}
			sum := sha1.Sum([]byte(content))
			return "100644 blob " + hex.EncodeToString(sum[:])[:7] + "\t" + path + "\n", "", nil
		case slices.Contains(args, "hash-object"):
			path := args[len(args)-1]
			data, err := os.ReadFile(filepath.Join(fr.repoDir, path))
			if err != nil {
				return "", "fatal: Cannot open", errors.New("exit 128")
			}
			sum := sha1.Sum(data)
			return hex.EncodeToString(sum[:])[:7] + "\n", "", nil
		case slices.Contains(args, "--short"):
			return "abc1234\n", "", nil
		case slices.Contains(args, "-r") && slices.Contains(args, "--list"):
			want := strings.TrimPrefix(args[len(args)-1], "origin/")
			if slices.Contains(fr.remotes, want) {
				return "  origin/" + want + "\n", "", nil
			}
			return "", "", nil
		case slices.Contains(args, "--list"):
			want := args[len(args)-1]
			if slices.Contains(fr.branches, want) {
				return "  " + want + "\n", "", nil
			}
			return "", "", nil
		case slices.Contains(args, "symbolic-ref"), slices.Contains(args, "rev-parse"):
			return "main\n", "", nil
		}
		return "", "", nil
	}

	fr.mgr = New(&git.Git{Cmd: commands.NewMockCommand(), Base: mockBase})
	fr.mgr.NowFn = func() time.Time { return time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC) }
	return fr
}

func (fr *fakeRepo) write(path, content string) {
	fr.t.Helper()
	full := filepath.Join(fr.repoDir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		fr.t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		fr.t.Fatalf("setup: %v", err)
	}
}

// writeRev records path's content as of rev — the fake's equivalent of a
// commit, reachable only through revision mode.
func (fr *fakeRepo) writeRev(rev, path, content string) {
	fr.t.Helper()
	if fr.revs == nil {
		fr.revs = map[string]map[string]string{}
	}
	if fr.revs[rev] == nil {
		fr.revs[rev] = map[string]string{}
	}
	fr.revs[rev][path] = content
}

// writeRevTree records path as a DIRECTORY at rev. git answers a directory
// with a tree object and exit 0, which is the shape that could sneak a
// non-file cite past a guard built on "did the lookup succeed".
func (fr *fakeRepo) writeRevTree(rev, path string) {
	fr.t.Helper()
	if fr.revTrees == nil {
		fr.revTrees = map[string]map[string]bool{}
	}
	if fr.revTrees[rev] == nil {
		fr.revTrees[rev] = map[string]bool{}
	}
	fr.revTrees[rev][path] = true
	// A revision has to be resolvable before any of its entries mean
	// anything; an unregistered one is answered as "not a valid object name".
	if fr.revs == nil {
		fr.revs = map[string]map[string]string{}
	}
	if fr.revs[rev] == nil {
		fr.revs[rev] = map[string]string{}
	}
}

// failRev makes every lookup at rev fail with stderr, the way git does when
// the revision itself is the problem — never fetched, mistyped — or when the
// repository is broken. An empty stderr models a git that failed without
// saying anything, which is a different branch of the wrapper's error path.
func (fr *fakeRepo) failRev(rev, stderr string) {
	fr.t.Helper()
	if fr.revErrs == nil {
		fr.revErrs = map[string]string{}
	}
	fr.revErrs[rev] = stderr
}

// atRev returns a second Manager over the same fake repo, pinned to rev. It
// shares the git wrapper and the clock, so a test can hold both modes at once
// and show them disagreeing about the same journal.
func (fr *fakeRepo) atRev(rev string) *Manager {
	fr.t.Helper()
	m := NewAtRev(fr.mgr.Git, rev)
	m.NowFn = fr.mgr.NowFn
	return m
}

func (fr *fakeRepo) journalPath(branch string) string {
	fr.t.Helper()
	p, err := fr.mgr.PathFor(fr.repoDir, branch)
	if err != nil {
		fr.t.Fatalf("PathFor: %v", err)
	}
	return p
}

// --- staleness: the ADR's central claim ---

// TestVerdictCatchesDirtyEditThatHeadWouldMiss is the acceptance-gate case.
// The file is edited WITHOUT committing, so HEAD is unchanged and any
// SHA-based comparison would report "fresh". Blob identity reports stale.
func TestVerdictCatchesDirtyEditThatHeadWouldMiss(t *testing.T) {
	fr := newFakeRepo(t)
	fr.write("client.go", "v1\n")

	id, err := fr.mgr.Open(fr.repoDir, "feat", "client.go:42", "N+1 in the retry loop")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	j, err := fr.mgr.Load(fr.repoDir, "feat")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e := *j.find(id)
	if got := fr.mgr.Verdict(fr.repoDir, e); got != FreshnessFresh {
		t.Fatalf("unchanged file should be fresh, got %q", got)
	}

	// Dirty edit only — no commit, so HEAD (mocked constant "abc1234") is the
	// same as when the entry was stamped.
	fr.write("client.go", "v2-dirty\n")
	if got := fr.mgr.Verdict(fr.repoDir, e); got != FreshnessStale {
		t.Fatalf("dirty edit must be stale, got %q", got)
	}
	if e.Head != "abc1234" {
		t.Fatalf("precondition: head should be unchanged across the edit, got %q", e.Head)
	}
}

// A cited file that was deleted renders stale, not an error: there is nothing
// to hash against, which is exactly what stale means.
func TestVerdictTreatsDeletedCitedFileAsStale(t *testing.T) {
	fr := newFakeRepo(t)
	fr.write("gone.go", "x\n")
	id, err := fr.mgr.Open(fr.repoDir, "feat", "gone.go:3", "unchecked error")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.Remove(filepath.Join(fr.repoDir, "gone.go")); err != nil {
		t.Fatalf("setup: %v", err)
	}

	j, _ := fr.mgr.Load(fr.repoDir, "feat")
	if got := fr.mgr.Verdict(fr.repoDir, *j.find(id)); got != FreshnessStale {
		t.Fatalf("deleted cited file must be stale, got %q", got)
	}
}

// A design-level entry cites no file and has no mechanical staleness.
func TestVerdictPathlessEntryNeverStale(t *testing.T) {
	fr := newFakeRepo(t)
	id, err := fr.mgr.Open(fr.repoDir, "feat", "", "Should this be an ADR?")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	j, _ := fr.mgr.Load(fr.repoDir, "feat")
	if got := fr.mgr.Verdict(fr.repoDir, *j.find(id)); got != FreshnessDateless {
		t.Fatalf("pathless entry should have no verdict, got %q", got)
	}
}

// --- revision mode (ADR-0023 §4) ---
//
// A pull request is reviewed from whatever branch the human happens to have
// checked out, which usually does not contain the PR's files at all. Every
// test below therefore makes the working tree disagree with the revision, and
// asserts that the revision is what decides.

// The checkout is dirty on the cited file, and the stamp ignores it entirely:
// the blob is the revision's, not the edit's. The same entry read in
// working-tree mode is stale, which is precisely why revision mode exists —
// that verdict is about the checkout, not about the pull request.
func TestStampAtRevIgnoresDirtyWorkingTree(t *testing.T) {
	fr := newFakeRepo(t)
	fr.writeRev("9f2c1ab", "client.go", "v1\n")
	fr.write("client.go", "unrelated local edit\n")

	rev := fr.atRev("9f2c1ab")
	id, err := rev.Open(fr.repoDir, "pr/acme/api/213", "client.go:42", "N+1 in the retry loop")
	if err != nil {
		t.Fatalf("Open at rev: %v", err)
	}

	j, err := rev.Load(fr.repoDir, "pr/acme/api/213")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	e := *j.find(id)
	if got := rev.Verdict(fr.repoDir, e); got != FreshnessFresh {
		t.Fatalf("entry stamped at the revision must be fresh there, got %q", got)
	}
	if got := fr.mgr.Verdict(fr.repoDir, e); got != FreshnessStale {
		t.Fatalf("the dirty checkout must NOT be what the revision-mode entry was judged against;"+
			" working-tree verdict was %q", got)
	}
	// The head stamp is the reviewed commit, not the checkout's HEAD (which
	// the fixture reports as "abc1234").
	if e.Head != "9f2c1ab" {
		t.Errorf("head stamp = %q, want the reviewed revision 9f2c1ab", e.Head)
	}
}

// The cited file is not in the checkout at all — the ordinary case when the
// PR's branch was never fetched into the working tree. Working-tree mode
// refuses this write; revision mode must accept it.
func TestStampAtRevSucceedsWhenCitedFileIsAbsentFromTheCheckout(t *testing.T) {
	fr := newFakeRepo(t)
	fr.writeRev("9f2c1ab", "internal/store/store.go", "func Write() {}\n")

	if _, err := fr.mgr.Open(
		fr.repoDir, "pr/acme/api/213", "internal/store/store.go:1", "write is not atomic",
	); err == nil {
		t.Fatal("precondition: working-tree mode should refuse a path the checkout does not have")
	}

	rev := fr.atRev("9f2c1ab")
	id, err := rev.Open(
		fr.repoDir, "pr/acme/api/213", "internal/store/store.go:1", "write is not atomic",
	)
	if err != nil {
		t.Fatalf("Open at rev: %v", err)
	}
	j, _ := rev.Load(fr.repoDir, "pr/acme/api/213")
	if got := rev.Verdict(fr.repoDir, *j.find(id)); got != FreshnessFresh {
		t.Fatalf("entry should be fresh at the revision it was stamped at, got %q", got)
	}
}

// The checkout is on unrelated work and keeps moving underneath the review —
// the file is edited, then deleted. Neither touches the verdict, because the
// verdict never looks at the checkout in this mode.
func TestVerdictAtRevIsUnaffectedByAnUnrelatedCheckout(t *testing.T) {
	fr := newFakeRepo(t)
	fr.writeRev("9f2c1ab", "client.go", "v1\n")
	fr.write("client.go", "someone else's branch\n")

	rev := fr.atRev("9f2c1ab")
	id, err := rev.Open(fr.repoDir, "pr/acme/api/213", "client.go:42", "N+1 in the retry loop")
	if err != nil {
		t.Fatalf("Open at rev: %v", err)
	}
	j, _ := rev.Load(fr.repoDir, "pr/acme/api/213")
	e := *j.find(id)

	fr.write("client.go", "still someone else's branch, edited\n")
	if got := rev.Verdict(fr.repoDir, e); got != FreshnessFresh {
		t.Fatalf("an unrelated checkout edit must not move the verdict, got %q", got)
	}
	if err := os.Remove(filepath.Join(fr.repoDir, "client.go")); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if got := rev.Verdict(fr.repoDir, e); got != FreshnessFresh {
		t.Fatalf("deleting the checkout's copy must not move the verdict, got %q", got)
	}
}

// ADR-0012 §3's typo guard, relocated: the path has to exist AT THE REVISION.
// Existing in the working tree is not a substitute — that is exactly the
// mistake a foreign-PR review would make.
func TestOpenAtRevRejectsPathMissingAtThatRevisionWithoutWriting(t *testing.T) {
	fr := newFakeRepo(t)
	fr.writeRev("9f2c1ab", "client.go", "v1\n")
	fr.write("typo.go", "this file exists locally, but not in the PR\n")

	rev := fr.atRev("9f2c1ab")
	_, err := rev.Open(fr.repoDir, "pr/acme/api/213", "typo.go:1", "oops")
	if err == nil {
		t.Fatal("expected an error for a path that does not exist at the revision")
	}
	if !strings.Contains(err.Error(), "typo.go") || !strings.Contains(err.Error(), "9f2c1ab") {
		t.Errorf("error should name the path and the revision, got %v", err)
	}
	if _, statErr := os.Stat(fr.journalPath("pr/acme/api/213")); !os.IsNotExist(statErr) {
		t.Error("no journal should have been written")
	}

	if _, err := rev.SettleDirect(
		fr.repoDir, "pr/acme/api/213", ResolutionAnswered, "typo.go:1", "note",
	); err == nil {
		t.Fatal("SettleDirect must apply the same guard at the revision")
	}
}

// Freshness is judged at the revision the CALLER passes now: the next tick of
// a review passes the PR's new head, so stale means "the pull request changed
// this file since the finding was written".
func TestVerdictAtRevFlipsOnlyWhenTheRevisionChangesTheFile(t *testing.T) {
	fr := newFakeRepo(t)
	fr.writeRev("aaa1111", "client.go", "v1\n")
	// The author pushes twice: once touching another file, once rewriting the
	// cited one.
	fr.writeRev("bbb2222", "client.go", "v1\n")
	fr.writeRev("ccc3333", "client.go", "v2 — retry loop rewritten\n")

	first := fr.atRev("aaa1111")
	id, err := first.Open(fr.repoDir, "pr/acme/api/213", "client.go:42", "N+1 in the retry loop")
	if err != nil {
		t.Fatalf("Open at rev: %v", err)
	}
	j, _ := first.Load(fr.repoDir, "pr/acme/api/213")
	e := *j.find(id)

	if got := fr.atRev("bbb2222").Verdict(fr.repoDir, e); got != FreshnessFresh {
		t.Fatalf("a new head that did not touch the cited file must stay fresh, got %q", got)
	}
	if got := fr.atRev("ccc3333").Verdict(fr.repoDir, e); got != FreshnessStale {
		t.Fatalf("a new head that rewrote the cited file must be stale, got %q", got)
	}
}

// Settling re-stamps at the revision the conclusion was formed against, and
// tolerates a cited file the revision no longer has — the revision-mode twin
// of "a finding can be fixed by deleting the file it cited".
func TestSettleAtRevRestampsAtTheSettlingRevision(t *testing.T) {
	fr := newFakeRepo(t)
	fr.writeRev("aaa1111", "client.go", "v1\n")
	fr.writeRev("bbb2222", "client.go", "v2 — fixed\n")

	id, err := fr.atRev("aaa1111").
		Open(fr.repoDir, "pr/acme/api/213", "client.go:42", "N+1 in the retry loop")
	if err != nil {
		t.Fatalf("Open at rev: %v", err)
	}

	settling := fr.atRev("bbb2222")
	if err := settling.SettleByID(
		fr.repoDir, "pr/acme/api/213", id, ResolutionFixed, "batched in one query",
	); err != nil {
		t.Fatalf("SettleByID at rev: %v", err)
	}
	j, _ := settling.Load(fr.repoDir, "pr/acme/api/213")
	e := *j.find(id)
	if e.Head != "bbb2222" {
		t.Errorf("head stamp = %q, want the settling revision bbb2222", e.Head)
	}
	if got := settling.Verdict(fr.repoDir, e); got != FreshnessFresh {
		t.Fatalf("a fix settled at the new head must read fresh there, got %q", got)
	}

	// A later head deletes the file the finding cited: settling must still
	// succeed, keeping the previous stamp rather than dead-ending the entry.
	fr.writeRev("ccc3333", "other.go", "x\n")
	deleted := fr.atRev("ccc3333")
	if err := deleted.Reopen(fr.repoDir, "pr/acme/api/213", id); err != nil {
		t.Fatalf("Reopen: %v", err)
	}
	if err := deleted.SettleByID(
		fr.repoDir, "pr/acme/api/213", id, ResolutionFixed, "file removed entirely",
	); err != nil {
		t.Fatalf("settling a finding whose file the revision deleted must succeed: %v", err)
	}
	j, _ = deleted.Load(fr.repoDir, "pr/acme/api/213")
	after := *j.find(id)
	if after.Blob != e.Blob || after.Head != e.Head {
		t.Errorf("stamp should be untouched when the file is gone: got blob %q head %q, want %q %q",
			after.Blob, after.Head, e.Blob, e.Head)
	}
	if got := deleted.Verdict(fr.repoDir, after); got != FreshnessStale {
		t.Fatalf("a cited file the revision no longer has is stale, got %q", got)
	}
}

// The three outcomes a revision lookup can have must stay three different
// answers. Collapsing them — which is what `rev-parse --verify <rev>:<path>`
// forced, since it reports a missing path and an unresolvable revision with
// the same "Needed a single revision" / exit 128 — told an agent its CITATIONS
// were wrong whenever its REVISION was wrong, so it would start rewriting
// correct paths.
func TestStampAtRevTellsAMissingPathApartFromABrokenLookup(t *testing.T) {
	fr := newFakeRepo(t)
	fr.writeRev("9f2c1ab", "client.go", "v1\n")
	fr.failRev("deadbee", "fatal: Not a valid object name deadbee")
	fr.failRev("brokenrepo", "") // git failed and said nothing

	// 1. The revision resolves and has no such path: the path is the culprit.
	_, err := fr.atRev("9f2c1ab").Open(fr.repoDir, "pr/acme/api/213", "typo.go:1", "oops")
	if err == nil {
		t.Fatal("a path absent at the revision must be refused")
	}
	if !strings.Contains(err.Error(), "does not exist") ||
		!strings.Contains(err.Error(), "typo.go") {
		t.Errorf("a missing path must be reported as a missing path, got %v", err)
	}

	// 2. The revision itself cannot be resolved: the REV is the culprit, and
	// saying "cited path client.go does not exist" here would be a lie.
	_, err = fr.atRev("deadbee").Open(fr.repoDir, "pr/acme/api/213", "client.go:42", "n+1")
	if err == nil {
		t.Fatal("an unresolvable revision must be an error, never read as an absent path")
	}
	if strings.Contains(err.Error(), "does not exist") {
		t.Errorf("a bad revision must not be blamed on the cited path, got %v", err)
	}
	if !strings.Contains(err.Error(), "Not a valid object name") {
		t.Errorf("git's own diagnosis must reach the user, got %v", err)
	}

	// 3. A generic git failure is still a failure, even with nothing on
	// stderr to quote.
	_, err = fr.atRev("brokenrepo").Open(fr.repoDir, "pr/acme/api/213", "client.go:42", "n+1")
	if err == nil {
		t.Fatal("a git failure with no stderr must still be an error")
	}
	if strings.Contains(err.Error(), "does not exist") {
		t.Errorf("a broken lookup must not be reported as an absent path, got %v", err)
	}

	// None of the three wrote anything.
	if _, statErr := os.Stat(fr.journalPath("pr/acme/api/213")); !os.IsNotExist(statErr) {
		t.Error("no journal should have been written")
	}
}

// A cite naming a DIRECTORY must be refused at a revision exactly as it is in
// the working tree, where `git hash-object` fails on one. git resolves a
// directory to a tree object and exits 0, so a guard built on "did the lookup
// succeed" would accept it and stamp a permanent entry with a tree hash —
// precisely the write ADR-0012 §3 exists to prevent.
func TestStampAtRevRejectsACiteNamingADirectory(t *testing.T) {
	fr := newFakeRepo(t)
	fr.writeRevTree("9f2c1ab", "internal/store")

	// Working-tree mode refuses it: hash-object cannot hash a directory.
	if err := os.MkdirAll(filepath.Join(fr.repoDir, "internal/store"), 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := fr.mgr.Open(
		fr.repoDir, "pr/acme/api/213", "internal/store:12", "the whole package is wrong",
	); err == nil {
		t.Fatal("precondition: working-tree mode must refuse a cite naming a directory")
	}

	_, err := fr.atRev("9f2c1ab").Open(
		fr.repoDir, "pr/acme/api/213", "internal/store:12", "the whole package is wrong",
	)
	if err == nil {
		t.Fatal("revision mode must refuse a cite naming a directory too")
	}
	if !strings.Contains(err.Error(), "internal/store") || !strings.Contains(err.Error(), "tree") {
		t.Errorf("error should name the path and what it actually is, got %v", err)
	}
	if _, statErr := os.Stat(fr.journalPath("pr/acme/api/213")); !os.IsNotExist(statErr) {
		t.Error("no journal should have been written")
	}
}

// Verdict deliberately does NOT inherit stamp's strictness: a lookup it cannot
// complete reads stale. A wrongly-stale finding costs one re-verification
// round; a wrongly-fresh one would hide a real change. Verdict also returns a
// bare string and cannot report an error at all — a bad --rev is caught once,
// up front, by the task layer.
func TestVerdictAtRevReadsABrokenLookupAsStaleNotAnError(t *testing.T) {
	fr := newFakeRepo(t)
	fr.writeRev("9f2c1ab", "client.go", "v1\n")

	rev := fr.atRev("9f2c1ab")
	id, err := rev.Open(fr.repoDir, "pr/acme/api/213", "client.go:42", "N+1 in the retry loop")
	if err != nil {
		t.Fatalf("Open at rev: %v", err)
	}
	j, _ := rev.Load(fr.repoDir, "pr/acme/api/213")
	e := *j.find(id)

	fr.failRev("deadbee", "fatal: Not a valid object name deadbee")
	if got := fr.atRev("deadbee").Verdict(fr.repoDir, e); got != FreshnessStale {
		t.Fatalf("an unresolvable revision must read stale on the freshness side, got %q", got)
	}
}

// A blank revision is the working tree — the single place that decision is
// made, so a caller can pass an optional --rev through unconditionally.
func TestNewAtRevWithBlankRevIsWorkingTreeMode(t *testing.T) {
	fr := newFakeRepo(t)
	fr.write("client.go", "v1\n")

	blank := fr.atRev("   ")
	id, err := blank.Open(fr.repoDir, "feat", "client.go:42", "N+1 in the retry loop")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	j, _ := blank.Load(fr.repoDir, "feat")
	e := *j.find(id)
	if e.Head != "abc1234" {
		t.Errorf("head stamp = %q, want the checkout's HEAD abc1234", e.Head)
	}
	fr.write("client.go", "v2-dirty\n")
	if got := blank.Verdict(fr.repoDir, e); got != FreshnessStale {
		t.Fatalf("a dirty edit must still be stale without a revision, got %q", got)
	}
}

// --- write guards ---

// A cited path that does not exist fails WITHOUT writing: an entry stamped
// with no blob would claim to cite code and never go stale.
func TestOpenRejectsNonexistentCitedPathWithoutWriting(t *testing.T) {
	fr := newFakeRepo(t)

	if _, err := fr.mgr.Open(fr.repoDir, "feat", "typo.go:1", "oops"); err == nil {
		t.Fatal("expected an error for a nonexistent cited path")
	} else if !strings.Contains(err.Error(), "typo.go") {
		t.Errorf("error should echo the path, got %v", err)
	}
	if _, err := os.Stat(fr.journalPath("feat")); !os.IsNotExist(err) {
		t.Error("no journal should have been written")
	}
}

func TestSettleDirectRejectsNonexistentCitedPath(t *testing.T) {
	fr := newFakeRepo(t)
	if _, err := fr.mgr.SettleDirect(
		fr.repoDir, "feat", ResolutionAnswered, "typo.go:1", "note",
	); err == nil {
		t.Fatal("expected an error for a nonexistent cited path")
	}
}

func TestInvalidResolutionRejected(t *testing.T) {
	fr := newFakeRepo(t)
	if _, err := fr.mgr.SettleDirect(fr.repoDir, "feat", "maybe", "", "note"); err == nil {
		t.Fatal("expected an error for an invalid resolution")
	}
	if err := fr.mgr.SettleByID(fr.repoDir, "feat", "n1", "maybe", "a"); err == nil {
		t.Fatal("expected an error for an invalid resolution")
	}
}

// --- settle semantics ---

// The cite carries over from the opened entry (a settle must not retarget the
// question it closes), but the blob is re-stamped at settle time: the stamp
// records what the CONCLUSION is true of, and the conclusion is formed now.
//
// This is the `fixed` case, and it is why the stamp cannot be the open-time
// one: a fix changes the cited file by definition, so an open-time stamp would
// mark the entry stale the moment it was settled, and every later review would
// re-check a fix that is perfectly fine.
func TestSettleByIDReStampsSoAFixReadsFresh(t *testing.T) {
	fr := newFakeRepo(t)
	fr.write("client.go", "v1-buggy\n")
	id, err := fr.mgr.Open(fr.repoDir, "feat", "client.go:99", "missing %w on the wrap")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	before, _ := fr.mgr.Load(fr.repoDir, "feat")
	openBlob := before.find(id).Blob

	fr.write("client.go", "v2-fixed\n") // the fix lands, then it is settled
	if err := fr.mgr.SettleByID(
		fr.repoDir,
		"feat",
		id,
		ResolutionFixed,
		"wrapped with %w",
	); err != nil {
		t.Fatalf("SettleByID: %v", err)
	}

	after, _ := fr.mgr.Load(fr.repoDir, "feat")
	e := after.find(id)
	if e.Open() {
		t.Fatal("entry should be settled")
	}
	if e.Cite != "client.go:99" {
		t.Errorf("cite should carry over, got %q", e.Cite)
	}
	if e.Blob == openBlob {
		t.Errorf("blob should have been re-stamped at settle time, still %s", openBlob)
	}
	if got := fr.mgr.Verdict(fr.repoDir, *e); got != FreshnessFresh {
		t.Fatalf("a just-settled fix must read fresh, got %q", got)
	}

	// It goes stale only when the file changes AFTER the settle — e.g. the fix
	// was reverted. That is the signal worth re-checking.
	fr.write("client.go", "v3-reverted\n")
	if got := fr.mgr.Verdict(fr.repoDir, *e); got != FreshnessStale {
		t.Errorf("a change after the settle must read stale, got %q", got)
	}
}

// A finding can be fixed by deleting the file it cited. Settling must still
// work: the original stamp is kept and the entry reads stale (the code it
// judged is gone), rather than the settle failing and leaving the exchange
// open with no way to close it.
func TestSettleByIDSucceedsWhenTheCitedFileWasDeleted(t *testing.T) {
	fr := newFakeRepo(t)
	fr.write("dead.go", "unused\n")
	id, err := fr.mgr.Open(fr.repoDir, "feat", "dead.go:1", "this file is unreferenced")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := os.Remove(filepath.Join(fr.repoDir, "dead.go")); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := fr.mgr.SettleByID(
		fr.repoDir,
		"feat",
		id,
		ResolutionFixed,
		"deleted the file",
	); err != nil {
		t.Fatalf("settling a finding fixed by deletion must succeed: %v", err)
	}
	after, _ := fr.mgr.Load(fr.repoDir, "feat")
	e := after.find(id)
	if e.Open() {
		t.Error("entry should be settled")
	}
	if got := fr.mgr.Verdict(fr.repoDir, *e); got != FreshnessStale {
		t.Errorf("a deleted cited file reads stale, got %q", got)
	}
}

// A rejected finding is settled without the code changing, so it reads fresh —
// and the reason travels with it, which is what a later reviewer re-reads
// before overriding the decision.
func TestSettleByIDRejectedKeepsReasonAndReadsFresh(t *testing.T) {
	fr := newFakeRepo(t)
	fr.write("client.go", "batched\n")
	id, err := fr.mgr.Open(fr.repoDir, "feat", "client.go:42", "N+1 query")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := fr.mgr.SettleByID(
		fr.repoDir, "feat", id, ResolutionRejected, "intentional, capped by config.MaxBatch",
	); err != nil {
		t.Fatalf("SettleByID: %v", err)
	}

	after, _ := fr.mgr.Load(fr.repoDir, "feat")
	e := after.find(id)
	if e.Answer != "intentional, capped by config.MaxBatch" {
		t.Errorf("the reason must survive: %q", e.Answer)
	}
	if got := fr.mgr.Verdict(fr.repoDir, *e); got != FreshnessFresh {
		t.Errorf("unchanged code should read fresh, got %q", got)
	}
}

func TestSettleByIDRejectsUnknownAndAlreadySettled(t *testing.T) {
	fr := newFakeRepo(t)
	if err := fr.mgr.SettleByID(fr.repoDir, "feat", "n9", ResolutionAnswered, "a"); err == nil {
		t.Fatal("expected an error for an unknown id")
	}
	id, err := fr.mgr.SettleDirect(fr.repoDir, "feat", ResolutionAnswered, "", "asked and answered")
	if err != nil {
		t.Fatalf("SettleDirect: %v", err)
	}
	if err := fr.mgr.SettleByID(fr.repoDir, "feat", id, ResolutionFixed, "again"); err == nil {
		t.Fatal("expected an error settling an already-settled entry")
	}
}

// --- ratify / reopen (ADR-0017 §6) ---

// Ratify on an agent-rejected entry strips the prefix, leaving an ordinary
// human rejection: same resolution, same reason minus the provenance marker.
//
// The prefix lives in the settle note (Answer), which only SettleByID
// populates — SettleDirect has no separate open-time question, so its text
// lands in Note instead. The realistic path this transition guards is always
// "reviewer opens a finding, agent settles it by id as rejected", so the
// fixture is built the same way.
func TestRatifyStripsAgentPrefixFromRejectedEntry(t *testing.T) {
	fr := newFakeRepo(t)
	id, err := fr.mgr.Open(fr.repoDir, "feat", "", "N+1 query")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := fr.mgr.SettleByID(
		fr.repoDir, "feat", id, ResolutionRejected, AgentNotePrefix+"disagree, capped by config",
	); err != nil {
		t.Fatalf("SettleByID: %v", err)
	}

	if err := fr.mgr.Ratify(fr.repoDir, "feat", id); err != nil {
		t.Fatalf("Ratify: %v", err)
	}

	j, _ := fr.mgr.Load(fr.repoDir, "feat")
	e := j.find(id)
	if e.Resolution != ResolutionRejected {
		t.Errorf("resolution should stay rejected, got %q", e.Resolution)
	}
	if e.Answer != "disagree, capped by config" {
		t.Errorf("expected the prefix stripped, got %q", e.Answer)
	}
}

// Ratify refuses every state that is not an agent-prefixed rejection, naming
// the actual state in each case so the caller can see why.
func TestRatifyRejectsEveryStateThatIsNotAnAgentRejection(t *testing.T) {
	fr := newFakeRepo(t)

	if err := fr.mgr.Ratify(fr.repoDir, "feat", "n9"); err == nil {
		t.Error("expected an error for an unknown id")
	}

	openID, err := fr.mgr.Open(fr.repoDir, "feat", "", "still open")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := fr.mgr.Ratify(fr.repoDir, "feat", openID); err == nil {
		t.Error("expected an error ratifying an open entry")
	}

	fixedID, err := fr.mgr.Open(fr.repoDir, "feat", "", "q-fixed")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := fr.mgr.SettleByID(fr.repoDir, "feat", fixedID, ResolutionFixed, "done"); err != nil {
		t.Fatalf("SettleByID: %v", err)
	}
	if err := fr.mgr.Ratify(fr.repoDir, "feat", fixedID); err == nil {
		t.Error("expected an error ratifying a fixed entry")
	}

	answeredID, err := fr.mgr.Open(fr.repoDir, "feat", "", "q-answered")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := fr.mgr.SettleByID(
		fr.repoDir,
		"feat",
		answeredID,
		ResolutionAnswered,
		"yes",
	); err != nil {
		t.Fatalf("SettleByID: %v", err)
	}
	if err := fr.mgr.Ratify(fr.repoDir, "feat", answeredID); err == nil {
		t.Error("expected an error ratifying an answered entry")
	}

	humanRejectedID, err := fr.mgr.Open(fr.repoDir, "feat", "", "q-rejected")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := fr.mgr.SettleByID(
		fr.repoDir, "feat", humanRejectedID, ResolutionRejected, "intentional, no agent involved",
	); err != nil {
		t.Fatalf("SettleByID: %v", err)
	}
	if err := fr.mgr.Ratify(fr.repoDir, "feat", humanRejectedID); err == nil {
		t.Error("expected an error ratifying an already-ordinary (unprefixed) rejection")
	}
}

// Reopen returns a settled entry to open under the SAME id — no new entry is
// created, so the total entry count is unchanged — with its original finding
// text intact and the resolution note dropped.
// Ratify must accept an agent note whether or not a space follows the colon.
//
// The writer of that note is prose — configs/shared/commands/review-loop.md
// tells the loop to settle with `--note "agent: <evidence>"` — so nothing
// mechanically guarantees the space arrives. When the marker constant carried
// the space, a note written as "agent:<evidence>" was still shown as an agent
// rejection by renderNotes and still carried into the terminal report with a
// --ratify command, which Ratify then refused: the human's only exit was
// --reopen, re-raising a finding that was already disproved.
func TestRatifyAcceptsAnAgentNoteWithOrWithoutASpaceAfterTheColon(t *testing.T) {
	// Built from the marker with its own spacing removed, deliberately: this
	// test is ABOUT the spacing, so it must not inherit whatever spacing the
	// constant happens to carry — otherwise "without a space" silently becomes
	// "with a space" the moment the constant grows one again.
	marker := strings.TrimSpace(AgentNotePrefix)
	for name, note := range map[string]string{
		"with a space":    marker + " capped by config",
		"without a space": marker + "capped by config",
	} {
		t.Run(name, func(t *testing.T) {
			fr := newFakeRepo(t)
			id, err := fr.mgr.Open(fr.repoDir, "feat", "", "N+1 query")
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if err := fr.mgr.SettleByID(
				fr.repoDir, "feat", id, ResolutionRejected, note,
			); err != nil {
				t.Fatalf("SettleByID: %v", err)
			}

			if err := fr.mgr.Ratify(fr.repoDir, "feat", id); err != nil {
				t.Fatalf("Ratify must accept %q: %v", note, err)
			}

			j, _ := fr.mgr.Load(fr.repoDir, "feat")
			if got := j.find(id).Answer; got != "capped by config" {
				t.Errorf("expected the marker and its spacing stripped, got %q", got)
			}
		})
	}
}

func TestReopenReturnsSameIDToOpenWithEntryCountUnchanged(t *testing.T) {
	fr := newFakeRepo(t)
	id, err := fr.mgr.Open(fr.repoDir, "feat", "", "N+1 query")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := fr.mgr.SettleByID(
		fr.repoDir, "feat", id, ResolutionRejected, AgentNotePrefix+"looks intentional",
	); err != nil {
		t.Fatalf("SettleByID: %v", err)
	}
	before, _ := fr.mgr.Load(fr.repoDir, "feat")
	countBefore := len(before.Entries)

	if err := fr.mgr.Reopen(fr.repoDir, "feat", id); err != nil {
		t.Fatalf("Reopen: %v", err)
	}

	after, _ := fr.mgr.Load(fr.repoDir, "feat")
	if len(after.Entries) != countBefore {
		t.Fatalf("entry count changed: before %d, after %d", countBefore, len(after.Entries))
	}
	e := after.find(id)
	if e == nil {
		t.Fatalf("entry %s must still exist under the same id", id)
	}
	if !e.Open() {
		t.Errorf("entry should be open again, got resolution %q", e.Resolution)
	}
	if e.Note != "N+1 query" {
		t.Errorf("original finding text must survive, got %q", e.Note)
	}
	if e.Answer != "" {
		t.Errorf("resolution note must be dropped, got %q", e.Answer)
	}
}

func TestReopenRejectsNonexistentOrAlreadyOpenID(t *testing.T) {
	fr := newFakeRepo(t)

	if err := fr.mgr.Reopen(fr.repoDir, "feat", "n9"); err == nil {
		t.Error("expected an error for an unknown id")
	}

	openID, err := fr.mgr.Open(fr.repoDir, "feat", "", "q")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := fr.mgr.Reopen(fr.repoDir, "feat", openID); err == nil {
		t.Error("expected an error reopening an already-open entry")
	}
}

func TestIDsAreStableAcrossWrites(t *testing.T) {
	fr := newFakeRepo(t)
	first, _ := fr.mgr.Open(fr.repoDir, "feat", "", "q1")
	second, _ := fr.mgr.Open(fr.repoDir, "feat", "", "q2")
	if first == second {
		t.Fatal("ids must be distinct")
	}
	if err := fr.mgr.SettleByID(fr.repoDir, "feat", first, ResolutionAnswered, "yes"); err != nil {
		t.Fatalf("SettleByID: %v", err)
	}
	third, _ := fr.mgr.Open(fr.repoDir, "feat", "", "q3")
	if third == first || third == second {
		t.Fatalf("id %s was reused", third)
	}
	j, _ := fr.mgr.Load(fr.repoDir, "feat")
	if j.find(second) == nil || j.find(second).Note != "q2" {
		t.Error("unsettled entry lost its identity across writes")
	}
}

// --- location, atomicity, lifecycle ---

// The journal lives under the COMMON git dir, so nothing in the work tree can
// pick it up.
func TestJournalPathIsUnderTheCommonGitDir(t *testing.T) {
	fr := newFakeRepo(t)
	path := fr.journalPath("fix/retry")
	wantDir := filepath.Join(fr.gitDir, "devgeta", "review")
	if filepath.Dir(path) != wantDir {
		t.Fatalf("expected journal under %s, got %s", wantDir, path)
	}
	if filepath.Base(path) != "fix%2Fretry.md" {
		t.Fatalf("expected the encoded filename, got %s", filepath.Base(path))
	}
}

func TestPathForRejectsEmptyBranch(t *testing.T) {
	fr := newFakeRepo(t)
	if _, err := fr.mgr.PathFor(fr.repoDir, "  "); err == nil {
		t.Fatal("expected an error for an empty branch")
	}
}

// A hostile branch name cannot escape the review directory — it is encoded, so
// the separators never survive into the path.
func TestHostileBranchNameStaysInsideTheReviewDir(t *testing.T) {
	fr := newFakeRepo(t)
	path := fr.journalPath("../../etc/passwd")
	wantDir := filepath.Join(fr.gitDir, "devgeta", "review")
	if filepath.Dir(path) != wantDir {
		t.Fatalf("escaped the review dir: %s", path)
	}
}

// Writes are atomic: a crash mid-write must leave the previous journal intact,
// and no temp files may survive a successful write.
func TestWritesAreAtomicAndLeaveNoTempFiles(t *testing.T) {
	fr := newFakeRepo(t)
	if _, err := fr.mgr.Open(fr.repoDir, "feat", "", "q1"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := fr.mgr.Open(fr.repoDir, "feat", "", "q2"); err != nil {
		t.Fatalf("Open: %v", err)
	}

	dir := filepath.Dir(fr.journalPath("feat"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		// Any dotfile sibling is a leaked staging file, whatever the
		// atomic writer happens to name its temp file today.
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("expected exactly the journal file, got %d entries", len(entries))
	}
}

// TestJournalAndSnapshotAreOwnerOnly pins the mode of both files the review
// directory holds. A journal quotes findings verbatim out of the branch's
// source, so it carries more of the repo's content than a settings file does
// and stays owner-only. This is a regression guard rather than a style
// preference: the journal and the snapshot share one atomic writer, and that
// writer takes the mode as an argument, so passing files.FilePermission
// (0644) at either call site would silently widen both.
func TestJournalAndSnapshotAreOwnerOnly(t *testing.T) {
	fr := newFakeRepo(t)
	if _, err := fr.mgr.Open(fr.repoDir, "feat", "", "q1"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	snapshotPath, err := fr.mgr.WriteSnapshot(fr.repoDir, "feat")
	if err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}

	for _, path := range []string{fr.journalPath("feat"), snapshotPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != journalPermission {
			t.Errorf("%s has mode %#o, want %#o", filepath.Base(path), got, journalPermission)
		}
	}
}

func TestLoadMissingJournalIsEmptyNotAnError(t *testing.T) {
	fr := newFakeRepo(t)
	j, err := fr.mgr.Load(fr.repoDir, "never-reviewed")
	if err != nil {
		t.Fatalf("a branch with no journal must not error: %v", err)
	}
	if len(j.Entries) != 0 {
		t.Fatalf("expected no entries, got %d", len(j.Entries))
	}
}

func TestDeleteRemovesJournalAndIsIdempotent(t *testing.T) {
	fr := newFakeRepo(t)
	if _, err := fr.mgr.Open(fr.repoDir, "feat", "", "q"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := fr.mgr.Delete(fr.repoDir, "feat"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(fr.journalPath("feat")); !os.IsNotExist(err) {
		t.Error("journal should be gone")
	}
	if err := fr.mgr.Delete(fr.repoDir, "feat"); err != nil {
		t.Errorf("deleting a missing journal should succeed, got %v", err)
	}
}

// Delete must also remove the branch's round-start snapshot. review-run
// deletes its own snapshot on every exit path, but a hard-killed run leaves
// one behind, and Prune only ever looks at "*.md" — so without this the
// orphan would outlive the branch forever, contradicting docs/spec.md's
// promise that removing a worktree leaves no review memory behind.
func TestDeleteRemovesTheRoundStartSnapshotToo(t *testing.T) {
	fr := newFakeRepo(t)
	if _, err := fr.mgr.Open(fr.repoDir, "feat", "", "q"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	snapshot, err := fr.mgr.WriteSnapshot(fr.repoDir, "feat")
	if err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatalf("setup: the snapshot must exist before Delete: %v", err)
	}

	if err := fr.mgr.Delete(fr.repoDir, "feat"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := os.Stat(snapshot); !os.IsNotExist(err) {
		t.Errorf("the round-start snapshot must be gone with the journal, stat err: %v", err)
	}
	if _, err := os.Stat(fr.journalPath("feat")); !os.IsNotExist(err) {
		t.Error("journal should be gone")
	}
}

func TestDeleteLeavesOtherBranchesAlone(t *testing.T) {
	fr := newFakeRepo(t)
	if _, err := fr.mgr.Open(fr.repoDir, "feat-a", "", "q"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := fr.mgr.Open(fr.repoDir, "feat-b", "", "q"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := fr.mgr.Delete(fr.repoDir, "feat-a"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(fr.journalPath("feat-b")); err != nil {
		t.Errorf("sibling journal must survive: %v", err)
	}
}

// Prune drops journals whose branch exists neither locally nor on the remote,
// and keeps the ones that do.
func TestPruneDropsOnlyJournalsWithNoBranch(t *testing.T) {
	fr := newFakeRepo(t)
	for _, b := range []string{"live-local", "live-remote", "fix/gone"} {
		if _, err := fr.mgr.Open(fr.repoDir, b, "", "q"); err != nil {
			t.Fatalf("Open(%s): %v", b, err)
		}
	}
	fr.branches = []string{"live-local"}
	fr.remotes = []string{"live-remote"}

	removed, err := fr.mgr.Prune(fr.repoDir)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !slices.Equal(removed, []string{"fix/gone"}) {
		t.Fatalf("expected only fix/gone pruned (decoded), got %v", removed)
	}
	if _, err := os.Stat(fr.journalPath("live-local")); err != nil {
		t.Error("local branch's journal must survive")
	}
	if _, err := os.Stat(fr.journalPath("live-remote")); err != nil {
		t.Error("remote branch's journal must survive")
	}
}

// A failed branch check means "unknown", never "absent". Treating an error as
// absent would delete a live branch's journal whenever git hiccups — losing a
// human's recorded decisions to reclaim a few KB.
func TestPruneNeverDeletesWhenTheBranchCheckFails(t *testing.T) {
	fr := newFakeRepo(t)
	if _, err := fr.mgr.Open(fr.repoDir, "feat", "", "q"); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Every branch-existence query now fails.
	base := fr.mgr.Git.Base.(*commands.MockBaseCommand)
	inner := base.ExecCommandFn
	base.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if slices.Contains(c.Args, "--list") {
			return "", "fatal: not a git repository", errors.New("exit 128")
		}
		return inner(c)
	}

	if _, err := fr.mgr.Prune(fr.repoDir); err == nil {
		t.Error("a failed branch check must be reported, not treated as absent")
	}
	if _, err := os.Stat(fr.journalPath("feat")); err != nil {
		t.Errorf("the journal must survive an inconclusive check: %v", err)
	}
}

// A PR-scoped key is not a branch name, so the branch-existence test says
// nothing about it: no branch is ever called "pr/acme/api/213", so an open
// PR's journal would be deleted by the first prune after its first review
// round, losing every settled finding while the PR is still being reviewed.
func TestPruneKeepsPRScopedJournals(t *testing.T) {
	fr := newFakeRepo(t)
	for _, key := range []string{PRKey("acme", "api", "213"), "fix/gone"} {
		if _, err := fr.mgr.Open(fr.repoDir, key, "", "q"); err != nil {
			t.Fatalf("Open(%s): %v", key, err)
		}
	}

	removed, err := fr.mgr.Prune(fr.repoDir)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !slices.Equal(removed, []string{"fix/gone"}) {
		t.Fatalf("expected only the dead branch pruned, got %v", removed)
	}
	if _, err := os.Stat(fr.journalPath(PRKey("acme", "api", "213"))); err != nil {
		t.Errorf("an open PR's journal must survive prune: %v", err)
	}
}

func TestPruneOnMissingDirIsNoOp(t *testing.T) {
	fr := newFakeRepo(t)
	removed, err := fr.mgr.Prune(fr.repoDir)
	if err != nil {
		t.Fatalf("Prune on a repo with no journals: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("expected nothing pruned, got %v", removed)
	}
}

// An undecodable filename is skipped, never deleted: an unreadable name is a
// reason to look, not to destroy.
func TestPruneSkipsUndecodableFilenames(t *testing.T) {
	fr := newFakeRepo(t)
	dir := filepath.Join(fr.gitDir, "devgeta", "review")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	junk := filepath.Join(dir, "not%ZZvalid.md")
	if err := os.WriteFile(junk, []byte("---\n---\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := fr.mgr.Prune(fr.repoDir); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if _, err := os.Stat(junk); err != nil {
		t.Error("an undecodable journal filename must be left alone")
	}
}
