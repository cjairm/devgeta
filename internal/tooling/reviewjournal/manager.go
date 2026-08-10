// Manager: the single writer/reader of review journals (ADR-0012 §3, §5).
//
// Location: <git common dir>/devgeta/review/<encoded-branch>.md — the COMMON
// git directory, so the same branch reviewed from the main checkout and from a
// linked worktree shares one journal (the per-worktree --git-dir would split
// it). Nothing under the git directory can be committed or appear in a diff,
// which is why no .gitignore handling exists here at all (ADR-0010 forbids
// editing one anyway).

package reviewjournal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/pkg/files"
)

// journalPermission is the mode for both the journal and its round-start
// snapshot. It is deliberately tighter than files.FilePermission (0644, what
// devgeta uses for configs): a journal quotes findings verbatim out of the
// branch's own source, so on a shared machine it carries more of the repo's
// content than a settings file does. The journal was already owner-only
// before these two writes shared one helper, and staying 0600 keeps that
// rather than widening it as a side effect of the deduplication.
const journalPermission = 0o600

// Manager reads and writes review journals through the git app wrapper.
// NowFn is injectable for tests; it stamps last_review.
type Manager struct {
	Git   *git.Git
	NowFn func() time.Time
	// Rev pins the revision every stamp and freshness check resolves against
	// (ADR-0022 §4). Empty — the default — means the working tree, which is
	// what a branch review wants: the reviewer is looking at the checkout,
	// uncommitted edits included.
	//
	// It is a field on the Manager rather than an argument to Open, Settle,
	// and Verdict because it is a property of the whole review, not of one
	// call: every entry written and every verdict computed in a given review
	// must resolve against the same source, and a per-call argument is a
	// per-call opportunity to pass a different one.
	Rev string
}

func New(g *git.Git) *Manager {
	return &Manager{Git: g, NowFn: time.Now}
}

// NewAtRev builds a Manager that stamps and judges freshness at rev instead of
// the working tree — for reviewing a commit that is not checked out, such as a
// pull request's head, where the cited file may be absent from the checkout or
// hold unrelated content (ADR-0022 §4).
//
// A blank rev is the working tree, so this is the single place that decides
// what "no revision given" means and callers can pass an optional --rev through
// unconditionally.
func NewAtRev(g *git.Git, rev string) *Manager {
	m := New(g)
	m.Rev = strings.TrimSpace(rev)
	return m
}

// reviewDir resolves the journal directory for the repo containing repoDir,
// without creating it.
func (m *Manager) reviewDir(repoDir string) (string, error) {
	common, err := m.Git.CommonDirIn(repoDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, "devgeta", "review"), nil
}

// PathFor returns the journal file path for branch, verifying the encoded
// name resolves inside the review directory. EncodeBranch cannot emit a path
// separator, so this containment check is defense in depth, not the primary
// guard.
func (m *Manager) PathFor(repoDir, branch string) (string, error) {
	if strings.TrimSpace(branch) == "" {
		return "", fmt.Errorf("a branch name is required")
	}
	dir, err := m.reviewDir(repoDir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, EncodeBranch(branch)+".md")
	if filepath.Dir(path) != filepath.Clean(dir) {
		return "", fmt.Errorf("branch %q does not resolve inside the review directory", branch)
	}
	return path, nil
}

// Load reads branch's journal. A missing file is an empty journal, not an
// error — every branch starts with no memory.
func (m *Manager) Load(repoDir, branch string) (*Journal, error) {
	path, err := m.PathFor(repoDir, branch)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Journal{Branch: branch}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the review journal: %w", err)
	}
	return Parse(branch, data), nil
}

// snapshotSuffix names the round-start snapshot `dg task review-run` writes
// beside a branch's journal. It deliberately does NOT end in ".md": Prune
// owns every "*.md" file in the review directory and decides from the
// filename alone whether a branch still exists, so a snapshot called
// "<encoded>.snapshot.md" could be decoded as a branch nobody has and
// deleted in the middle of a round. Keeping the suffix outside Prune's
// filter makes that collision impossible instead of relying on DecodeBranch
// happening to fail.
const snapshotSuffix = ".snapshot"

// SnapshotPathFor returns the path of branch's round-start snapshot: one
// deterministic name per branch, in the same directory as the journal.
//
// There is no round number in the name. review-run writes the snapshot when
// a round starts and removes it when that round ends — including on its
// failure paths — relying on the invariant that only one `review-run`
// invocation runs against a given branch at a time. A per-round name would
// not help two overlapping invocations either, since both could be round 1;
// avoiding that requires not running review-run twice on the same branch at
// once, not a naming scheme here.
func (m *Manager) SnapshotPathFor(repoDir, branch string) (string, error) {
	path, err := m.PathFor(repoDir, branch)
	if err != nil {
		return "", err
	}
	return snapshotPathOf(path), nil
}

// snapshotPathOf derives a journal's snapshot path from the journal path
// itself, so a caller that already resolved one (Delete) does not pay a second
// git call to resolve the other, and the two paths cannot be derived by two
// different rules.
func snapshotPathOf(journalPath string) string {
	return strings.TrimSuffix(journalPath, ".md") + snapshotSuffix
}

// WriteSnapshot serializes branch's journal as it stands right now to the
// snapshot path, and returns that path.
//
// A branch with no journal file yet is not a special case here, deliberately:
// Load already reports a missing file as an empty journal, and that empty
// journal is written out like any other. "Empty at round start" is exactly
// what the second reviewer of a first-ever review must see — skipping the
// write there would leave it reading the live journal, i.e. the first
// reviewer's brand-new findings (ADR-0017 §4).
func (m *Manager) WriteSnapshot(repoDir, branch string) (string, error) {
	j, err := m.Load(repoDir, branch)
	if err != nil {
		return "", err
	}
	path, err := m.SnapshotPathFor(repoDir, branch)
	if err != nil {
		return "", err
	}
	if err := files.WriteFileAtomic(path, []byte(j.Render()), journalPermission); err != nil {
		return "", fmt.Errorf("failed to write the round-start review snapshot: %w", err)
	}
	return path, nil
}

// save writes the journal atomically: temp file in the same directory, then
// rename — the same write-to-temp-then-rename rule CLAUDE.md §7 mandates for
// the global config. A crash mid-write leaves the previous journal intact.
//
// It delegates to files.WriteFileAtomic, which is that rule's one
// implementation and is what WriteSnapshot above already uses; a second
// hand-rolled MkdirAll + CreateTemp + rename here would be the same logic
// twice, differing only in which of the two could grow a bug.
func (m *Manager) save(repoDir string, j *Journal) error {
	path, err := m.PathFor(repoDir, j.Branch)
	if err != nil {
		return err
	}
	j.LastReview = m.NowFn().Format("2006-01-02")
	if err := files.WriteFileAtomic(path, []byte(j.Render()), journalPermission); err != nil {
		return fmt.Errorf("failed to save the review journal: %w", err)
	}
	return nil
}

// citeBlob resolves the cited path's blob identity from whatever source this
// manager judges against — the working tree, or Rev when one is pinned. It is
// the single place the two modes differ, so stamp, restamp, and Verdict all
// get revision awareness from one function rather than three.
//
// The "not there at all" case returns ("", nil), not an error: both callers
// have to handle it and they disagree about it — a stamp refuses (ADR-0012
// §3's typo guard), a restamp tolerates it (a finding can be fixed by deleting
// the file it cited). An error means the lookup itself broke, which is fatal
// to every caller.
func (m *Manager) citeBlob(repoDir, file string) (string, error) {
	if m.Rev != "" {
		blob, err := m.Git.BlobAtRevIn(repoDir, m.Rev, file)
		// Only git's definitive "that revision has no such entry" is absence.
		// Every other failure — an unfetched revision, a broken repository —
		// is a failure to LOOK, and Prune's rule below applies here too: a
		// failed check means "unknown", never "absent". Collapsing the two
		// would answer a bad --rev with "cited path X does not exist", telling
		// an agent its citations are wrong when its revision is what is wrong,
		// so it would start rewriting correct paths.
		if errors.Is(err, git.ErrPathNotAtRev) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return blob, nil
	}
	if _, err := os.Stat(filepath.Join(repoDir, file)); err != nil {
		return "", nil
	}
	blob, err := m.Git.HashObjectIn(repoDir, file)
	if err != nil {
		return "", fmt.Errorf("failed to hash cited path %s: %w", file, err)
	}
	return blob, nil
}

// citeSource names, for an error message, where a cited path was looked for.
func (m *Manager) citeSource() string {
	if m.Rev != "" {
		return "at " + m.Rev
	}
	return "in the working tree"
}

// stampHead records which commit the entry was judged at. In revision mode
// that is Rev itself — the reviewed commit, which is the only honest answer
// when the checkout is on unrelated work. Rev reaches here already resolved to
// a SHA (the task layer's verifyRev does that), so what lands in the journal is
// the immutable commit ADR-0022 is named after and not a ref name that moves.
// Otherwise it is the checkout's HEAD, best-effort: an unborn branch has no
// HEAD, and the stamp is human context, not the staleness signal.
func (m *Manager) stampHead(repoDir string, e *Entry) {
	if m.Rev != "" {
		e.Head = m.Rev
		return
	}
	if head, err := m.Git.ShortHeadIn(repoDir); err == nil {
		e.Head = head
	}
}

// stamp fills an entry's blob and head. A cite that names a file which is not
// there fails without writing (ADR-0012 §3): a typo'd path silently recorded
// as "no blob" would create an entry that never goes stale while claiming to
// cite code.
func (m *Manager) stamp(repoDir string, e *Entry) error {
	if file := e.CitedFile(); file != "" {
		blob, err := m.citeBlob(repoDir, file)
		if err != nil {
			return err
		}
		if blob == "" {
			return fmt.Errorf("cited path %s does not exist %s", file, m.citeSource())
		}
		e.Blob = blob
	}
	m.stampHead(repoDir, e)
	return nil
}

// ensureBase fills the journal's base ref once, on first write.
func (m *Manager) ensureBase(repoDir string, j *Journal) {
	if j.Base == "" {
		j.Base = "origin/" + m.Git.DefaultBranchIn(repoDir)
	}
}

// Open records an open question or finding and returns its new id.
func (m *Manager) Open(repoDir, branch, cite, note string) (string, error) {
	j, err := m.Load(repoDir, branch)
	if err != nil {
		return "", err
	}
	e := Entry{ID: j.nextID(), Cite: cite, Note: note}
	if err := m.stamp(repoDir, &e); err != nil {
		return "", err
	}
	m.ensureBase(repoDir, j)
	j.Entries = append(j.Entries, e)
	if err := m.save(repoDir, j); err != nil {
		return "", err
	}
	return e.ID, nil
}

// SettleByID moves an open entry to settled with its resolution. The cite
// carries over from the opened entry — a settle can never retarget the
// question it closes (ADR-0012 §3) — but the blob stamp is refreshed, because
// the stamp records the content the CONCLUSION is true of, and the conclusion
// is formed now, not when the question was first asked.
//
// Keeping the open-time stamp instead looks defensible and is wrong in the
// most common case there is: a `fixed` entry is settled precisely because the
// cited file changed, so an open-time stamp would mark it stale the instant it
// was settled and every future review would re-check a fix that is fine. It
// also disagreed with SettleDirect, which stamps at settle time — the same
// exchange read fresh or stale depending only on whether it had been opened
// first.
func (m *Manager) SettleByID(repoDir, branch, id, resolution, answer string) error {
	if !ValidResolution(resolution) {
		return fmt.Errorf("invalid resolution %q (want rejected, answered, or fixed)", resolution)
	}
	j, err := m.Load(repoDir, branch)
	if err != nil {
		return err
	}
	e, err := j.findOrErr(id)
	if err != nil {
		return err
	}
	if !e.Open() {
		return fmt.Errorf("entry %s is already settled (%s)", id, e.Resolution)
	}
	e.Resolution = resolution
	e.Answer = answer
	if err := m.restamp(repoDir, e); err != nil {
		return err
	}
	return m.save(repoDir, j)
}

// restamp refreshes an entry's blob and head as it is settled, tolerating a
// cited file that no longer exists — a finding can be fixed by deleting the
// file it cited. In that case the whole stamp is left as it was and Verdict
// reads the missing file as stale, which is the honest answer; failing the
// settle would leave the exchange open forever with no way to close it.
func (m *Manager) restamp(repoDir string, e *Entry) error {
	if file := e.CitedFile(); file != "" {
		blob, err := m.citeBlob(repoDir, file)
		if err != nil {
			return err
		}
		if blob == "" {
			return nil
		}
		e.Blob = blob
	}
	m.stampHead(repoDir, e)
	return nil
}

// Ratify accepts an agent's provisional rejection as a human decision
// (ADR-0017 §6): it strips AgentNotePrefix from the settle note in place,
// leaving an ordinary human rejection under ADR-0012 semantics. Nothing else
// about the entry changes — the blob/head stamp is deliberately left alone,
// because ratifying forms no new judgment about the cited code; it only
// confirms who the existing rejection belongs to.
//
// Valid only on an entry settled as rejected whose note still carries the
// prefix. Every other state is refused with the actual state named, so the
// caller sees why: open (nothing settled to ratify yet), settled fixed or
// answered (ratify only concerns rejections), or a rejected entry with no
// prefix (already ratified once).
func (m *Manager) Ratify(repoDir, branch, id string) error {
	j, err := m.Load(repoDir, branch)
	if err != nil {
		return err
	}
	e, err := j.findOrErr(id)
	if err != nil {
		return err
	}
	if e.Open() {
		return fmt.Errorf("entry %s is open, not settled — nothing to ratify", id)
	}
	if e.Resolution != ResolutionRejected {
		return fmt.Errorf(
			"entry %s is settled as %s, not rejected — ratify only applies to a rejected entry",
			id, e.Resolution,
		)
	}
	if !HasAgentNote(e.Answer) {
		return fmt.Errorf(
			"entry %s is already an ordinary rejection (no %s prefix to strip)",
			id, AgentNotePrefix,
		)
	}
	e.Answer = StripAgentNote(e.Answer)
	return m.save(repoDir, j)
}

// Reopen returns a settled entry to open under the same id, keeping its
// original finding text and dropping the resolution note — ADR-0012 already
// specifies that an open entry is re-raised, never duplicated, so the next
// round asks it again exactly as it was first asked, not as a new entry.
//
// The blob/head stamp is left untouched, not refreshed: reopening undoes a
// settlement, it does not re-judge the cited code, so the stamp keeps
// answering the question it always has — has the cited code changed since it
// was last actually judged, which was the settle now being undone. Stamping
// here would falsely claim a fresh look was just taken.
//
// Valid on any settled entry, regardless of resolution. An already-open entry
// or an unknown id is refused with the actual state named.
func (m *Manager) Reopen(repoDir, branch, id string) error {
	j, err := m.Load(repoDir, branch)
	if err != nil {
		return err
	}
	e, err := j.findOrErr(id)
	if err != nil {
		return err
	}
	if e.Open() {
		return fmt.Errorf("entry %s is already open", id)
	}
	e.Resolution = ""
	e.Answer = ""
	return m.save(repoDir, j)
}

// SettleDirect records an exchange that was never open — asked and answered in
// one conversation — straight into settled, returning its id.
func (m *Manager) SettleDirect(repoDir, branch, resolution, cite, note string) (string, error) {
	if !ValidResolution(resolution) {
		return "", fmt.Errorf(
			"invalid resolution %q (want rejected, answered, or fixed)",
			resolution,
		)
	}
	j, err := m.Load(repoDir, branch)
	if err != nil {
		return "", err
	}
	e := Entry{ID: j.nextID(), Resolution: resolution, Cite: cite, Note: note}
	if err := m.stamp(repoDir, &e); err != nil {
		return "", err
	}
	m.ensureBase(repoDir, j)
	j.Entries = append(j.Entries, e)
	if err := m.save(repoDir, j); err != nil {
		return "", err
	}
	return e.ID, nil
}

// Freshness values Verdict can return.
const (
	FreshnessFresh    = "fresh"
	FreshnessStale    = "stale"
	FreshnessDateless = "" // pathless entry: no mechanical staleness
)

// Verdict computes an entry's freshness against the current working tree, or
// against Rev when one is pinned. A cited file that no longer exists is stale,
// not an error: there is nothing to compare against, and "the code this was
// judged on is gone" is exactly what stale means (ADR-0012 acceptance gate).
//
// In revision mode the caller passes the CURRENT head of the thing under
// review — the next tick of a PR review passes the PR's new head — so stale
// means "the pull request changed this file since the finding was written",
// never "your checkout differs from the pull request", which is true of almost
// every file and would mark the whole journal stale (ADR-0022 §4).
//
// A lookup that FAILS also reads stale here, unlike stamp, which returns the
// error. The asymmetry is deliberate, in the safe direction for each side. On
// this side a wrongly-stale finding costs one re-verification round — the
// reviewer agents are already told to re-check a stale entry and re-raise it
// only if the problem is back — whereas wrongly-fresh would hide a real
// change. Verdict also returns a bare string and structurally cannot report an
// error; a bad --rev is caught once, up front, by the task layer, so it never
// degrades into "every entry is stale" here.
func (m *Manager) Verdict(repoDir string, e Entry) string {
	file := e.CitedFile()
	if file == "" || e.Blob == "" {
		return FreshnessDateless
	}
	blob, err := m.citeBlob(repoDir, file)
	if err != nil || blob != e.Blob {
		return FreshnessStale
	}
	return FreshnessFresh
}

// Delete removes branch's journal AND its round-start snapshot. A file that
// never existed is success — callers (the worktree teardown) only care that
// nothing of the branch's review memory remains.
//
// The snapshot is deleted here rather than left to Prune because Prune only
// looks at "*.md" and would never see it: review-run removes its own snapshot
// on every exit path, but a hard-killed run leaves one behind, and without
// this that orphan would outlive the branch forever — making the promise that
// removing a worktree deletes the journal "so memory does not accumulate for
// work that no longer exists" (docs/spec.md) quietly false.
func (m *Manager) Delete(repoDir, branch string) error {
	path, err := m.PathFor(repoDir, branch)
	if err != nil {
		return err
	}
	for _, p := range []string{path, snapshotPathOf(path)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove the review journal: %w", err)
		}
	}
	return nil
}

// Prune removes journals whose branch no longer exists locally or on the
// remote, returning the decoded branch names removed. Undecodable filenames
// are skipped, never deleted — an unreadable name is a reason to look, not to
// destroy.
//
// PR-scoped keys are skipped for the same reason. "Does a branch by this name
// exist?" is not a question about a pull request: no branch is ever called
// "pr/<owner>/<repo>/<n>", so the check answers "gone" for every PR journal,
// including one whose review is mid-flight — the first prune after a PR
// review's first round would delete the settled findings the next tick reads,
// and the loop would re-raise everything the human already answered. Judging a
// PR key by the only signal that could settle it (whether the PR is still
// open) means asking GitHub, which this local, offline command does not do;
// until something does, keeping the file is the safe direction, and it is the
// same trade the branch checks already make above.
func (m *Manager) Prune(repoDir string) ([]string, error) {
	dir, err := m.reviewDir(repoDir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list review journals: %w", err)
	}
	var removed []string
	for _, de := range entries {
		name, ok := strings.CutSuffix(de.Name(), ".md")
		if de.IsDir() || !ok {
			continue
		}
		branch, err := DecodeBranch(name)
		if err != nil || IsPRKey(branch) {
			continue
		}
		// A failed check means "unknown", never "absent". Treating an error as
		// absent would delete a live branch's journal on a transient git
		// failure — irreversible data loss to save a few KB, which CLAUDE.md
		// §4's data-integrity rule forbids. Report and stop instead; the
		// journals already removed are returned so the caller can say what
		// happened before the failure.
		local, err := m.Git.BranchExistsIn(repoDir, branch)
		if err != nil {
			return removed, fmt.Errorf("failed to check local branch %s: %w", branch, err)
		}
		remote, err := m.Git.RemoteBranchExistsIn(repoDir, branch)
		if err != nil {
			return removed, fmt.Errorf("failed to check remote branch %s: %w", branch, err)
		}
		if local || remote {
			continue
		}
		if err := os.Remove(filepath.Join(dir, de.Name())); err != nil {
			return removed, fmt.Errorf("failed to prune journal for %s: %w", branch, err)
		}
		removed = append(removed, branch)
	}
	return removed, nil
}
