package worktree

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
)

func init() {
	testutil.InitLogger()
}

// mkRepo creates dir (and any parents) plus a ".git" child inside it, so
// dir itself is a git repo root as scanRepos defines it.
func mkRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir repo %s: %v", dir, err)
	}
}

// mkDir creates dir (and any parents) without a .git marker — a plain
// directory the walk should descend into.
func mkDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func TestScanRepos_RepoAtSearchPathRoot(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)

	got := scanRepos([]string{root}, 4)

	want := config.CanonicalRepoPath(root)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("scanRepos() = %v, want [%s]", got, want)
	}
}

func TestScanRepos_NestedRepoNotListed(t *testing.T) {
	root := t.TempDir()
	outer := filepath.Join(root, "outer")
	mkRepo(t, outer)
	inner := filepath.Join(outer, "nested")
	mkRepo(t, inner)

	got := scanRepos([]string{root}, 4)

	wantOuter := config.CanonicalRepoPath(outer)
	if len(got) != 1 || got[0] != wantOuter {
		t.Fatalf(
			"scanRepos() = %v, want only the outer repo [%s] (nested repo must be pruned)",
			got,
			wantOuter,
		)
	}
}

func TestScanRepos_ExcludedComponentNotListed(t *testing.T) {
	root := t.TempDir()
	// A project dir that is not itself a repo, but has a .git buried
	// inside an excluded component (node_modules) — must not appear.
	proj := filepath.Join(root, "proj")
	mkDir(t, proj)
	mkRepo(t, filepath.Join(proj, "node_modules"))

	got := scanRepos([]string{root}, 4)

	if len(got) != 0 {
		t.Fatalf("scanRepos() = %v, want none (node_modules/.git must be excluded)", got)
	}
}

func TestScanRepos_ExcludedComponentVariants(t *testing.T) {
	for _, name := range []string{".cache", "vendor", "target", "dist"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			mkRepo(t, filepath.Join(root, name))

			got := scanRepos([]string{root}, 4)

			if len(got) != 0 {
				t.Fatalf("scanRepos() = %v, want none (%s is an excluded component)", got, name)
			}
		})
	}
}

func TestScanRepos_DepthBoundary(t *testing.T) {
	root := t.TempDir()

	// At depth 2 (root/a/b), maxDepth=2: found.
	atBoundary := filepath.Join(root, "a", "b")
	mkRepo(t, atBoundary)

	// One level past the boundary (root/c/d/e), maxDepth=2: not found. "d"
	// (depth 2) has no .git of its own, so the only repo is "e" at depth 3.
	pastBoundary := filepath.Join(root, "c", "d", "e")
	mkRepo(t, pastBoundary)

	got := scanRepos([]string{root}, 2)

	wantAtBoundary := config.CanonicalRepoPath(atBoundary)
	if len(got) != 1 || got[0] != wantAtBoundary {
		t.Fatalf(
			"scanRepos() = %v, want exactly the repo at the depth boundary [%s]",
			got,
			wantAtBoundary,
		)
	}
}

func TestScanRepos_SymlinkLoopDoesNotHang(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	mkDir(t, a)
	// a/link_to_a -> a: a symlink cycle rooted at "a" itself.
	if err := os.Symlink(a, filepath.Join(a, "link_to_a")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	// A real repo elsewhere in the tree so we can confirm the scan still
	// completes and produces the expected (non-looping) result.
	repo := filepath.Join(root, "repo")
	mkRepo(t, repo)

	done := make(chan []string, 1)
	go func() {
		done <- scanRepos([]string{root}, 4)
	}()

	select {
	case got := <-done:
		want := config.CanonicalRepoPath(repo)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("scanRepos() = %v, want [%s]", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("scanRepos() did not return within 5s — symlink loop was followed")
	}
}

func TestScanRepos_MissingAndNonDirEntriesSkipped(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "valid")
	mkRepo(t, valid)

	missing := filepath.Join(root, "does-not-exist")

	notADir := filepath.Join(root, "a-file")
	if err := os.WriteFile(notADir, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got := scanRepos([]string{missing, notADir, valid}, 4)

	want := config.CanonicalRepoPath(valid)
	if len(got) != 1 || got[0] != want {
		t.Fatalf(
			"scanRepos() = %v, want only the valid repo [%s]; missing/non-dir entries must be skipped without error",
			got,
			want,
		)
	}
}

func TestScanRepos_ScanDepthZeroAndNegativeDefaultToFour(t *testing.T) {
	for _, depth := range []int{0, -1, -100} {
		t.Run(depthLabel(depth), func(t *testing.T) {
			root := t.TempDir()

			// At the default depth (4): root/a/b/c/d.
			atDefault := filepath.Join(root, "a", "b", "c", "d")
			mkRepo(t, atDefault)

			// One level past the default: root/e/f/g/h/i.
			pastDefault := filepath.Join(root, "e", "f", "g", "h", "i")
			mkRepo(t, pastDefault)

			got := scanRepos([]string{root}, depth)

			want := config.CanonicalRepoPath(atDefault)
			if len(got) != 1 || got[0] != want {
				t.Fatalf(
					"scanRepos(depth=%d) = %v, want only the repo at the default depth [%s]",
					depth,
					got,
					want,
				)
			}
		})
	}
}

func depthLabel(depth int) string {
	if depth < 0 {
		return "negative"
	}
	return "zero"
}

func TestScanRepos_NestedSearchPathsDedupedNotDoubleWalked(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	mkDir(t, sub)
	repo := filepath.Join(sub, "repo")
	mkRepo(t, repo)

	// "sub" is nested inside "root" — configuring both must not walk "sub"
	// twice (and so must not report the repo under it twice).
	got := scanRepos([]string{root, sub}, 4)

	want := config.CanonicalRepoPath(repo)
	if len(got) != 1 || got[0] != want {
		t.Fatalf(
			"scanRepos() = %v, want the repo listed exactly once [%s] (nested root must be deduped)",
			got,
			want,
		)
	}
}

func TestScanRepos_SymlinkedConfiguredRootIsStillScanned(t *testing.T) {
	parent := t.TempDir()
	realRoot := filepath.Join(parent, "real")
	mkRepo(t, realRoot)

	linkRoot := filepath.Join(parent, "link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got := scanRepos([]string{linkRoot}, 4)

	want := config.CanonicalRepoPath(realRoot)
	if len(got) != 1 || got[0] != want {
		t.Fatalf(
			"scanRepos() = %v, want the repo at the resolved root [%s] (a configured root that is itself a symlink must still be scanned)",
			got,
			want,
		)
	}
}

func TestScanRepos_MultipleDistinctRootsAllScanned(t *testing.T) {
	rootA := t.TempDir()
	rootB := t.TempDir()
	repoA := filepath.Join(rootA, "repoA")
	repoB := filepath.Join(rootB, "repoB")
	mkRepo(t, repoA)
	mkRepo(t, repoB)

	got := scanRepos([]string{rootA, rootB}, 4)

	sort.Strings(got)
	want := []string{config.CanonicalRepoPath(repoA), config.CanonicalRepoPath(repoB)}
	sort.Strings(want)

	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("scanRepos() = %v, want %v", got, want)
	}
}
