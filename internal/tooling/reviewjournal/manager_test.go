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
		if strings.HasPrefix(e.Name(), ".journal-") {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("expected exactly the journal file, got %d entries", len(entries))
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
