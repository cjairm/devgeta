package agentpack

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/commands"
)

func testManifest() Manifest {
	return Manifest{
		Name: "superpowers",
		Repo: "https://github.com/obra/superpowers.git",
		Tag:  "v7.0.2",
		SHA:  goodSHA,
	}
}

// removeAllWritable registers a cleanup that runs BEFORE t.TempDir's own: a
// fetched clone is read-only, and the standard cleanup can't delete a
// read-only directory.
func removeAllWritable(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() {
		if err := removeTree(dir); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})
}

// gitFixture answers the git calls Fetch makes. A "clone" call writes a tiny
// upstream tree into the destination it was given, and "rev-parse" reports
// headSHA, so nothing leaves the test.
type gitFixture struct {
	base     *commands.MockBaseCommand
	git      *git.Git
	headSHA  string
	cloneErr error
	// cloneFrom, when set, is a directory under testdata a "clone" copies into
	// its destination instead of the tiny default tree.
	cloneFrom string
}

func newGitFixture(t *testing.T) *gitFixture {
	t.Helper()
	f := &gitFixture{base: commands.NewMockBaseCommand(), headSHA: goodSHA}
	f.git = &git.Git{Cmd: commands.NewMockCommand(), Base: f.base}
	f.base.ExecCommandFn = func(c commands.CommandParams) (string, string, error) {
		if slices.Contains(c.Args, "clone") {
			if f.cloneErr != nil {
				return "", "fatal: unable to access", f.cloneErr
			}
			dst := c.Args[len(c.Args)-1]
			if f.cloneFrom != "" {
				copyInto(t, filepath.Join("testdata", f.cloneFrom), dst)
				return "", "", nil
			}
			writeFile(t, filepath.Join(dst, "skills", "a", "SKILL.md"), "a", 0o644)
			writeFile(t, filepath.Join(dst, "hooks", "run.sh"), "#!/bin/sh\n", 0o755)
			writeFile(t, filepath.Join(dst, ".git", "HEAD"), "ref", 0o644)
			return "", "", nil
		}
		if slices.Contains(c.Args, "rev-parse") {
			return f.headSHA + "\n", "", nil
		}
		t.Errorf("unexpected git call: %v", c.Args)
		return "", "", nil
	}
	return f
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func cloneCalls(f *gitFixture) int {
	n := 0
	for _, c := range f.base.ExecCommandCalls {
		if slices.Contains(c.Args, "clone") {
			n++
		}
	}
	return n
}

func TestFetch_ClonesChecksShaAndCaches(t *testing.T) {
	root := t.TempDir()
	removeAllWritable(t, root)
	f := newGitFixture(t)
	fetcher := &Fetcher{Git: f.git, Root: root}

	dir, err := fetcher.Fetch(testManifest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(root, ".src", "superpowers@"+goodSHA)
	if dir != want {
		t.Errorf("expected the clone at %s, got %s", want, dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "a", "SKILL.md")); err != nil {
		t.Errorf("the cloned files should be in place: %v", err)
	}

	var clone *commands.CommandParams
	for i, c := range f.base.ExecCommandCalls {
		if slices.Contains(c.Args, "clone") {
			clone = &f.base.ExecCommandCalls[i]
		}
	}
	if clone == nil || !slices.Contains(clone.Args, "v7.0.2") ||
		!slices.Contains(clone.Args, testManifest().Repo) {
		t.Errorf("expected a clone of the pinned tag from the pinned repo, got %+v", clone)
	}
}

func TestFetch_CachedCloneIsReusedWithoutTheNetwork(t *testing.T) {
	root := t.TempDir()
	removeAllWritable(t, root)
	f := newGitFixture(t)
	fetcher := &Fetcher{Git: f.git, Root: root}

	first, err := fetcher.Fetch(testManifest())
	if err != nil {
		t.Fatal(err)
	}
	callsAfterFirst := len(f.base.ExecCommandCalls)

	second, err := fetcher.Fetch(testManifest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if second != first {
		t.Errorf("expected the same cached clone, got %s and %s", first, second)
	}
	if got := len(f.base.ExecCommandCalls); got != callsAfterFirst {
		t.Errorf("a cached clone must not run git again; %d extra calls", got-callsAfterFirst)
	}
}

func TestFetch_ShaMismatchFailsAndCachesNothing(t *testing.T) {
	root := t.TempDir()
	removeAllWritable(t, root)
	f := newGitFixture(t)
	f.headSHA = strings.Repeat("a", 40) // the tag was moved upstream
	fetcher := &Fetcher{Git: f.git, Root: root}

	_, err := fetcher.Fetch(testManifest())
	if err == nil {
		t.Fatal("expected a SHA mismatch to fail")
	}
	for _, want := range []string{"superpowers", "v7.0.2", goodSHA, f.headSHA} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to mention %q, got: %v", want, err)
		}
	}
	assertNothingCached(t, root)
}

func TestFetch_NetworkFailureFailsAndCachesNothing(t *testing.T) {
	root := t.TempDir()
	removeAllWritable(t, root)
	f := newGitFixture(t)
	f.cloneErr = errors.New("exit status 128")
	fetcher := &Fetcher{Git: f.git, Root: root}

	_, err := fetcher.Fetch(testManifest())
	if err == nil {
		t.Fatal("expected a clone failure to fail")
	}
	if !strings.Contains(err.Error(), "agent-pack install superpowers") {
		t.Errorf("the error should say how to retry, got: %v", err)
	}
	assertNothingCached(t, root)
}

// A failed fetch must leave neither a cached clone nor a half-written temp
// directory behind: the next attempt would otherwise reuse a bad one.
func assertNothingCached(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".src"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected nothing left in .src, found %v", names)
	}
}

func TestFetch_ClonePermissionsAreStripped(t *testing.T) {
	root := t.TempDir()
	removeAllWritable(t, root)
	f := newGitFixture(t)
	fetcher := &Fetcher{Git: f.git, Root: root}

	dir, err := fetcher.Fetch(testManifest())
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0o222 != 0 {
			t.Errorf("%s is still writable (%v)", p, info.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Read-only must not cost the executable bit the hooks rely on.
	info, err := os.Stat(filepath.Join(dir, "hooks", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("run.sh lost its executable bit: %v", info.Mode().Perm())
	}
}

func TestRemoveTree_RemovesAReadOnlyTree(t *testing.T) {
	root := t.TempDir()
	tree := filepath.Join(root, "tree")
	writeFile(t, filepath.Join(tree, "sub", "f"), "x", 0o444)
	if err := makeReadOnly(tree); err != nil {
		t.Fatal(err)
	}

	if err := removeTree(tree); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Errorf("expected the tree to be gone, stat err: %v", err)
	}
}

func TestRemoveTree_MissingIsFine(t *testing.T) {
	if err := removeTree(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Errorf("removing a missing tree is not an error: %v", err)
	}
}

func TestFetchRef_ResolvesATagWithoutAPinAndCachesItByCommit(t *testing.T) {
	root := t.TempDir()
	removeAllWritable(t, root)
	f := newGitFixture(t)
	other := strings.Repeat("c", 40) // v7.0.1's commit, not the one the manifest pins
	f.headSHA = other
	fetcher := &Fetcher{Git: f.git, Root: root}
	m := testManifest()
	m.Tag = "v7.0.1"

	dir, sha, err := fetcher.FetchRef(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha != other {
		t.Errorf("expected the commit the tag resolved to, got %s", sha)
	}
	if want := filepath.Join(root, ".src", "superpowers@"+other); dir != want {
		t.Errorf("expected the clone at %s, got %s", want, dir)
	}
	if info, err := os.Stat(filepath.Join(dir, "skills")); err != nil || info.Mode().Perm()&0o222 != 0 {
		t.Errorf("the cached clone must be read-only: %v %v", err, info)
	}
	entries, _ := os.ReadDir(filepath.Join(root, ".src"))
	if len(entries) != 1 {
		t.Errorf("no temp directory may be left behind, found %d entries", len(entries))
	}
}

func TestFetchRef_ReusesAnAlreadyCachedCommit(t *testing.T) {
	root := t.TempDir()
	removeAllWritable(t, root)
	f := newGitFixture(t)
	fetcher := &Fetcher{Git: f.git, Root: root}

	first, _, err := fetcher.FetchRef(testManifest())
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := fetcher.FetchRef(testManifest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first != second {
		t.Errorf("expected the same cached clone, got %s and %s", first, second)
	}
	entries, _ := os.ReadDir(filepath.Join(root, ".src"))
	if len(entries) != 1 {
		t.Errorf("the second clone must be discarded, found %d entries", len(entries))
	}
}

func TestFetchRef_NetworkFailureSaysHowToRetryCheck(t *testing.T) {
	root := t.TempDir()
	removeAllWritable(t, root)
	f := newGitFixture(t)
	f.cloneErr = errors.New("exit status 128")
	fetcher := &Fetcher{Git: f.git, Root: root}

	_, _, err := fetcher.FetchRef(testManifest())
	if err == nil || !strings.Contains(err.Error(), "agent-pack check superpowers") {
		t.Fatalf("expected a retry hint for check, got %v", err)
	}
	assertNothingCached(t, root)
}
