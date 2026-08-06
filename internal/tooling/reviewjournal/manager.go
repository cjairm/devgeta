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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/pkg/files"
)

// Manager reads and writes review journals through the git app wrapper.
// NowFn is injectable for tests; it stamps last_review.
type Manager struct {
	Git   *git.Git
	NowFn func() time.Time
}

func New(g *git.Git) *Manager {
	return &Manager{Git: g, NowFn: time.Now}
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
	return strings.TrimSuffix(path, ".md") + snapshotSuffix, nil
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
	if err := files.WriteFileAtomic(path, []byte(j.Render()), files.FilePermission); err != nil {
		return "", fmt.Errorf("failed to write the round-start review snapshot: %w", err)
	}
	return path, nil
}

// save writes the journal atomically: temp file in the same directory, then
// rename — the same write-to-temp-then-rename rule CLAUDE.md §7 mandates for
// the global config. A crash mid-write leaves the previous journal intact.
func (m *Manager) save(repoDir string, j *Journal) error {
	path, err := m.PathFor(repoDir, j.Branch)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create the review directory: %w", err)
	}
	j.LastReview = m.NowFn().Format("2006-01-02")
	tmp, err := os.CreateTemp(filepath.Dir(path), ".journal-*")
	if err != nil {
		return fmt.Errorf("failed to stage the review journal: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(j.Render()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to write the review journal: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to write the review journal: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to save the review journal: %w", err)
	}
	return nil
}

// stamp fills an entry's blob and head. A cite that names a file which does
// not exist in the working tree fails without writing (ADR-0012 §3): a typo'd
// path silently recorded as "no blob" would create an entry that never goes
// stale while claiming to cite code. The head stamp is best-effort — an
// unborn branch has no HEAD, and the stamp is human context, not the
// staleness signal.
func (m *Manager) stamp(repoDir string, e *Entry) error {
	if file := e.CitedFile(); file != "" {
		if _, err := os.Stat(filepath.Join(repoDir, file)); err != nil {
			return fmt.Errorf("cited path %s does not exist in the working tree", file)
		}
		blob, err := m.Git.HashObjectIn(repoDir, file)
		if err != nil {
			return fmt.Errorf("failed to hash cited path %s: %w", file, err)
		}
		e.Blob = blob
	}
	if head, err := m.Git.ShortHeadIn(repoDir); err == nil {
		e.Head = head
	}
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
// file it cited. In that case the original stamp is kept and Verdict reads the
// missing file as stale, which is the honest answer; failing the settle would
// leave the exchange open forever with no way to close it.
func (m *Manager) restamp(repoDir string, e *Entry) error {
	if file := e.CitedFile(); file != "" {
		if _, err := os.Stat(filepath.Join(repoDir, file)); err != nil {
			return nil
		}
	}
	return m.stamp(repoDir, e)
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
	if !strings.HasPrefix(e.Answer, AgentNotePrefix) {
		return fmt.Errorf(
			"entry %s is already an ordinary rejection (no %s prefix to strip)",
			id, strings.TrimSpace(AgentNotePrefix),
		)
	}
	e.Answer = strings.TrimPrefix(e.Answer, AgentNotePrefix)
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

// Verdict computes an entry's freshness against the current working tree.
// A cited file that no longer exists is stale, not an error: there is nothing
// to hash against, and "the code this was judged on is gone" is exactly what
// stale means (ADR-0012 acceptance gate).
func (m *Manager) Verdict(repoDir string, e Entry) string {
	file := e.CitedFile()
	if file == "" || e.Blob == "" {
		return FreshnessDateless
	}
	blob, err := m.Git.HashObjectIn(repoDir, file)
	if err != nil || blob != e.Blob {
		return FreshnessStale
	}
	return FreshnessFresh
}

// Delete removes branch's journal. A journal that never existed is success —
// callers (the worktree teardown) only care that no journal remains.
func (m *Manager) Delete(repoDir, branch string) error {
	path, err := m.PathFor(repoDir, branch)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove the review journal: %w", err)
	}
	return nil
}

// Prune removes journals whose branch no longer exists locally or on the
// remote, returning the decoded branch names removed. Undecodable filenames
// are skipped, never deleted — an unreadable name is a reason to look, not to
// destroy.
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
		if err != nil {
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
