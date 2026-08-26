// Store: shared per-branch file storage, extracted from
// reviewjournal.Manager's storage half (ADR-0012 §3, §5) so a second
// per-branch store (handoff) does not reimplement it.
//
// Location: <git common dir>/devgeta/<subdir>/<encoded-name>.md — the COMMON
// git directory, so the same branch seen from the main checkout and from a
// linked worktree shares one file (a per-worktree --git-dir would split it).
//
// The lock is per DIRECTORY, not per file: a store's mutations are cheap
// enough that serializing every branch's writes in a subdirectory costs
// nothing, and a per-file lock would have to be created and removed alongside
// each file, which reintroduces the very problem a long-lived lock file
// avoids (see files.WithLock's own doc). Because each subdir gets its own
// lock file, two Stores over different subdirs (review, handoff) never
// contend with each other. Do not "fix" this into one shared lock later.

package branchstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/pkg/files"
)

const (
	defaultLockFile    = "store.lock"
	defaultLockTimeout = 10 * time.Second
	// defaultPermission is tighter than files.FilePermission (0644): every
	// consumer of this store (review findings, handoff notes) quotes or
	// summarizes the repo's own content, so on a shared machine it carries
	// more than a settings file does.
	defaultPermission = 0o600
)

// Store is a per-branch file store rooted at
// <git common dir>/devgeta/<Subdir>/. The three fields beyond Git and Subdir
// have defaults from New and exist so a caller with pre-existing on-disk
// state (reviewjournal) can pin the exact name/timeout/permission it already
// used, rather than migrating files.
type Store struct {
	Git    *git.Git
	Subdir string

	// LockFile is the sidecar filename WithLock creates inside the store's
	// directory. Defaults to "store.lock".
	LockFile string
	// LockTimeout bounds how long WithLock waits for the lock before
	// returning an actionable error instead of hanging. Read fresh on every
	// WithLock call, so changing it takes effect on the next call, not the
	// next Store.
	LockTimeout time.Duration
	// FilePermission is the mode Write creates files with. Defaults to 0600.
	FilePermission os.FileMode
}

// New builds a Store over subdir (e.g. "review", "handoff") with this
// package's defaults. A caller may override LockFile, LockTimeout, or
// FilePermission on the returned Store before first use.
func New(g *git.Git, subdir string) *Store {
	return &Store{
		Git:            g,
		Subdir:         subdir,
		LockFile:       defaultLockFile,
		LockTimeout:    defaultLockTimeout,
		FilePermission: defaultPermission,
	}
}

// Dir resolves the store's directory for the repo containing repoDir, without
// creating it.
func (s *Store) Dir(repoDir string) (string, error) {
	common, err := s.Git.CommonDirIn(repoDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(common, "devgeta", s.Subdir), nil
}

// PathFor returns the file path for branch, verifying the encoded name
// resolves inside the store's directory. EncodeName cannot emit a path
// separator, so this containment check is defense in depth, not the primary
// guard.
func (s *Store) PathFor(repoDir, branch string) (string, error) {
	if strings.TrimSpace(branch) == "" {
		return "", fmt.Errorf("a branch name is required")
	}
	dir, err := s.Dir(repoDir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, EncodeName(branch)+".md")
	if filepath.Dir(path) != filepath.Clean(dir) {
		return "", fmt.Errorf(
			"branch %q does not resolve inside the %s directory",
			branch,
			s.Subdir,
		)
	}
	return path, nil
}

// Read returns branch's stored bytes. An absent file is (nil, nil), not an
// error — every branch starts with nothing stored.
func (s *Store) Read(repoDir, branch string) ([]byte, error) {
	path, err := s.PathFor(repoDir, branch)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return data, nil
}

// Write saves data for branch atomically: temp file in the store's
// directory, then rename, so a crash mid-write leaves any previous content
// intact and a reader never sees a half-written file.
func (s *Store) Write(repoDir, branch string, data []byte) error {
	path, err := s.PathFor(repoDir, branch)
	if err != nil {
		return err
	}
	if err := files.WriteFileAtomic(path, data, s.FilePermission); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

// Remove deletes branch's stored file. Removing a file that never existed is
// success — callers only care that nothing remains.
func (s *Store) Remove(repoDir, branch string) error {
	path, err := s.PathFor(repoDir, branch)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove %s: %w", path, err)
	}
	return nil
}

// WithLock runs fn holding the store's directory-wide exclusive lock, for the
// whole load-mutate-save cycle a caller needs to make atomic across separate
// devgeta processes. The caller owns that whole cycle inside fn — this never
// loads, mutates, or saves on its own, so a caller whose mutation does more
// than transform bytes (resolves a git blob, reserves a sibling file) keeps
// that work inside the lock without a byte-transform signature forcing it out.
func (s *Store) WithLock(repoDir string, fn func() error) error {
	dir, err := s.Dir(repoDir)
	if err != nil {
		return err
	}
	if err := files.WithLock(filepath.Join(dir, s.LockFile), s.LockTimeout, fn); err != nil {
		return fmt.Errorf("%s: %w", s.Subdir, err)
	}
	return nil
}
