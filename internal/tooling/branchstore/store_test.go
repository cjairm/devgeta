package branchstore

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/files"
)

// newTestStore wires a Store whose git calls are answered from a real temp
// directory, mirroring reviewjournal's fakeRepo fixture: --git-common-dir
// resolves to a real .git under root, so PathFor/Dir resolve real paths
// without shelling out to git.
func newTestStore(t *testing.T, subdir string) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	repoDir := filepath.Join(root, "work")
	gitDir := filepath.Join(repoDir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	mockBase := commands.NewMockBaseCommand()
	mockBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if slices.Contains(c.Args, "--git-common-dir") {
			return gitDir + "\n", "", nil
		}
		return "", "", nil
	}
	g := &git.Git{Base: mockBase}
	return New(g, subdir), repoDir
}

func TestPathForResolvesUnderCommonDirAndSubdir(t *testing.T) {
	s, repoDir := newTestStore(t, "handoff")
	path, err := s.PathFor(repoDir, "feat/login")
	if err != nil {
		t.Fatalf("PathFor: %v", err)
	}
	want := filepath.Join(repoDir, ".git", "devgeta", "handoff", EncodeName("feat/login")+".md")
	if path != want {
		t.Errorf("PathFor = %q, want %q", path, want)
	}
}

func TestPathForRejectsEmptyBranch(t *testing.T) {
	s, repoDir := newTestStore(t, "handoff")
	if _, err := s.PathFor(repoDir, ""); err == nil {
		t.Fatal("expected an error for an empty branch name")
	}
}

func TestReadAbsentFileReturnsNilNil(t *testing.T) {
	s, repoDir := newTestStore(t, "handoff")
	data, err := s.Read(repoDir, "feat")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if data != nil {
		t.Errorf("Read on an absent file = %q, want nil", data)
	}
}

func TestWriteThenReadRoundTrips(t *testing.T) {
	s, repoDir := newTestStore(t, "handoff")
	want := []byte("---\nbranch: feat\n---\nsome note\n")
	if err := s.Write(repoDir, "feat", want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := s.Read(repoDir, "feat")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("Read = %q, want %q", got, want)
	}
}

func TestWriteIsAtomicNoLeftoverTempFile(t *testing.T) {
	s, repoDir := newTestStore(t, "handoff")
	if err := s.Write(repoDir, "feat", []byte("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	dir, err := s.Dir(repoDir)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
}

func TestWriteUsesTheConfiguredPermission(t *testing.T) {
	s, repoDir := newTestStore(t, "handoff")
	s.FilePermission = 0o600
	if err := s.Write(repoDir, "feat", []byte("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	path, err := s.PathFor(repoDir, "feat")
	if err != nil {
		t.Fatalf("PathFor: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestRemoveDeletesAnExistingFile(t *testing.T) {
	s, repoDir := newTestStore(t, "handoff")
	if err := s.Write(repoDir, "feat", []byte("x")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := s.Remove(repoDir, "feat"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	data, err := s.Read(repoDir, "feat")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if data != nil {
		t.Errorf("file still present after Remove")
	}
}

func TestRemoveOnAbsentFileIsNotAnError(t *testing.T) {
	s, repoDir := newTestStore(t, "handoff")
	if err := s.Remove(repoDir, "never-existed"); err != nil {
		t.Errorf("Remove on an absent file: %v", err)
	}
}

func TestWithLockSerializesConcurrentCallers(t *testing.T) {
	s, repoDir := newTestStore(t, "handoff")
	s.LockTimeout = 2 * time.Second

	var mu sync.Mutex
	order := []string{}
	record := func(tag string) {
		mu.Lock()
		order = append(order, tag)
		mu.Unlock()
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		_ = s.WithLock(repoDir, func() error {
			record("first-enter")
			close(entered)
			<-release
			record("first-exit")
			return nil
		})
		close(done)
	}()

	<-entered
	// release must be closed from another goroutine, concurrently with the
	// second WithLock call below: the first holder only lets go of the lock
	// once release is closed, and the second call blocks on that lock, so
	// closing release AFTER the second call returns would deadlock.
	go close(release)
	if err := s.WithLock(repoDir, func() error {
		record("second-enter")
		return nil
	}); err != nil {
		t.Fatalf("second WithLock: %v", err)
	}
	<-done

	want := []string{"first-enter", "first-exit", "second-enter"}
	if !slices.Equal(order, want) {
		t.Errorf("order = %v, want %v", order, want)
	}
}

func TestWithLockTimesOutOnAWedgedLock(t *testing.T) {
	s, repoDir := newTestStore(t, "handoff")
	s.LockTimeout = 100 * time.Millisecond

	dir, err := s.Dir(repoDir)
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	lockPath := filepath.Join(dir, s.LockFile)

	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		if err := files.WithLock(lockPath, time.Minute, func() error {
			close(held)
			<-release
			return nil
		}); err != nil {
			t.Errorf("stand-in holder failed to take the lock: %v", err)
		}
	}()
	<-held
	t.Cleanup(func() { close(release) })

	err = s.WithLock(repoDir, func() error {
		t.Fatal("fn ran without the lock")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("WithLock error = %v, want a timeout", err)
	}
}

func TestTwoSubdirsDoNotShareALock(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "work")
	gitDir := filepath.Join(repoDir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	mockBase := commands.NewMockBaseCommand()
	mockBase.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if slices.Contains(c.Args, "--git-common-dir") {
			return gitDir + "\n", "", nil
		}
		return "", "", nil
	}
	g := &git.Git{Base: mockBase}

	review := New(g, "review")
	handoff := New(g, "handoff")
	review.LockTimeout = 2 * time.Second
	handoff.LockTimeout = 2 * time.Second

	held := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = review.WithLock(repoDir, func() error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	defer close(release)

	if err := handoff.WithLock(repoDir, func() error { return nil }); err != nil {
		t.Errorf("a lock on one subdir blocked another: %v", err)
	}
}
