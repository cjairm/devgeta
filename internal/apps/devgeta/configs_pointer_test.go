package devgeta

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/buildinfo"
	"github.com/cjairm/devgeta/pkg/paths"
)

// generationFiles are the relative paths every generated test tree contains.
// Reading all of them and finding one generation in each is what "the reader
// saw a complete tree" means below.
var generationFiles = []string{
	filepath.Join("git", ".gitconfig"),
	filepath.Join("neovim", "init.lua"),
	filepath.Join("tmux", "tmux.conf.tmpl"),
	filepath.Join("zsh", "zshenv.zsh"),
}

// generationExtractor returns an extractor writing a tree whose every file
// holds the same marker, so a reader can tell one generation from another.
func generationExtractor(marker string) func(string) error {
	return func(destDir string) error {
		for _, rel := range generationFiles {
			path := filepath.Join(destDir, rel)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(marker), 0o644); err != nil {
				return err
			}
		}
		return nil
	}
}

// countingExtractor wraps mockExtractor and records how many times it ran, so
// tests can assert an extract was skipped rather than merely idempotent.
func countingExtractor(runs *int32) func(string) error {
	return func(destDir string) error {
		atomic.AddInt32(runs, 1)
		return mockExtractor(destDir)
	}
}

// setBuildStamp pins buildinfo for one test and restores it afterwards, so a
// test can simulate an upgrade without rebuilding anything.
func setBuildStamp(t *testing.T, version, commit string) {
	t.Helper()
	origVersion, origCommit := buildinfo.Version, buildinfo.Commit
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit = origVersion, origCommit
	})
	buildinfo.Version, buildinfo.Commit = version, commit
}

// assertPointsAt asserts configs is a symlink naming want and that the tree it
// resolves to is really there.
func assertPointsAt(t *testing.T, want string) {
	t.Helper()
	pointer := pointerPath()
	info, err := os.Lstat(pointer)
	if err != nil {
		t.Fatalf("expected a configs pointer at %s: %v", pointer, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to be a symlink, got mode %v", pointer, info.Mode())
	}
	dest, err := os.Readlink(pointer)
	if err != nil {
		t.Fatalf("failed to read the configs pointer: %v", err)
	}
	if dest != want {
		t.Errorf("configs pointer = %q, want %q", dest, want)
	}
	if st, err := os.Stat(pointer); err != nil || !st.IsDir() {
		t.Errorf("expected the pointer to resolve to a directory, got err=%v", err)
	}
}

// assertNotExists asserts nothing (not even a dangling link) sits at path.
func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be gone, got err=%v", path, err)
	}
}

func TestBuildStamp_SanitizesAndMatchesTheRemovalGuard(t *testing.T) {
	cases := []struct {
		version, commit, want string
	}{
		{"v1.2.3", "abc1234", "configs-v1.2.3-abc1234"},
		{"dev", "unknown", "configs-dev-unknown"},
		// A separator or any other unexpected byte must not escape the app
		// root, and the result must still satisfy stampedDirPattern or the
		// tree could never be cleaned up.
		{"../../etc", "a/b", "configs-.._.._etc-a_b"},
		{"", "", "configs-unknown-unknown"},
	}
	for _, tc := range cases {
		setBuildStamp(t, tc.version, tc.commit)
		got := stampedDirName()
		if got != tc.want {
			t.Errorf(
				"stampedDirName() for %q/%q = %q, want %q",
				tc.version,
				tc.commit,
				got,
				tc.want,
			)
		}
		if !stampedDirPattern.MatchString(got) {
			t.Errorf("stampedDirName() produced %q, which stampedDirPattern rejects", got)
		}
		if filepath.Base(got) != got {
			t.Errorf("stampedDirName() produced a multi-component name: %q", got)
		}
	}
	// The transient names must stay outside the sweep's reach.
	for _, name := range []string{configsPointerName, legacyName, tempPointerPrefix + "1234-0"} {
		if stampedDirPattern.MatchString(name) {
			t.Errorf("stampedDirPattern must not match %q; the sweep would delete it", name)
		}
	}
}

// TestSwapPointer_ReaderNeverSeesAnAbsentOrPartialTree flips the pointer under
// a reader that is continuously reading through it, and pins down exactly what
// the commit protocol guarantees:
//
//   - every successful read returns one generation's complete content — never a
//     truncated, empty, or invented value;
//   - no read ever fails with "no such file". That is the regression this whole
//     step exists to prevent: the RemoveAll-then-re-extract it replaces served
//     ENOENT for the entire duration of a 152-file rewrite.
//
// Two things it deliberately does NOT assert, because the design does not
// provide them and the plan says so:
//
//   - A multi-file read straddling a swap can mix generations (read file X from
//     the old tree, get descheduled, read file Y from the new one). The plan
//     names this the residual risk and notes no retention policy fixes it,
//     since the swap causes it, not the delete.
//   - On macOS/APFS a read racing the rename itself can fail transiently with
//     EINVAL. Measured at roughly one failed read per twenty swaps under a
//     reader spinning as hard as it can; see the task report. It is a clean
//     error, never wrong data, and unreachable unless two devgeta processes run
//     at once — which the plan already accepts as pre-existing exposure.
func TestSwapPointer_ReaderNeverSeesAnAbsentOrPartialTree(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	root := paths.Paths.App.Root
	oldTarget := filepath.Join(root, "configs-v1-aaaaaaa")
	newTarget := filepath.Join(root, "configs-v2-bbbbbbb")
	if err := generationExtractor("A")(oldTarget); err != nil {
		t.Fatalf("failed to build generation A: %v", err)
	}
	if err := generationExtractor("B")(newTarget); err != nil {
		t.Fatalf("failed to build generation B: %v", err)
	}

	pointer := pointerPath()
	if err := os.Symlink(filepath.Base(oldTarget), pointer); err != nil {
		t.Fatalf("failed to seed the pointer: %v", err)
	}

	var (
		stop      = make(chan struct{})
		wg        sync.WaitGroup
		reads     int
		sawA      bool
		sawB      bool
		transient int
		failures  []string
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Read through the pointer path, exactly as devgeta's own readers
			// do: a fresh resolution per read, no cached target.
			for _, rel := range generationFiles {
				data, err := os.ReadFile(filepath.Join(pointer, rel))
				reads++
				if err != nil {
					if errors.Is(err, fs.ErrNotExist) {
						failures = append(
							failures,
							fmt.Sprintf("the tree was absent while reading %s: %v", rel, err),
						)
						continue
					}
					transient++
					continue
				}
				switch string(data) {
				case "A":
					sawA = true
				case "B":
					sawB = true
				default:
					failures = append(
						failures,
						fmt.Sprintf("incomplete content for %s: %q", rel, string(data)),
					)
				}
			}
		}
	}()

	// Flip back and forth so the reader is guaranteed to straddle a swap.
	for i := 0; i < 50; i++ {
		target := newTarget
		if i%2 == 1 {
			target = oldTarget
		}
		if err := swapPointer(root, pointer, target); err != nil {
			close(stop)
			wg.Wait()
			t.Fatalf("swapPointer failed on iteration %d: %v", i, err)
		}
	}
	close(stop)
	wg.Wait()

	if len(failures) > 0 {
		t.Fatalf("reader saw an absent or incomplete tree: %v", failures)
	}
	if reads == 0 {
		t.Fatal("reader completed no reads; the test proved nothing")
	}
	if !sawA || !sawB {
		t.Fatalf("expected the reader to observe both generations, sawA=%v sawB=%v", sawA, sawB)
	}
	t.Logf(
		"%d reads across 50 swaps: both generations seen, 0 absent trees, %d transient rename-race errors",
		reads,
		transient,
	)
	// No temp symlink may survive a completed swap.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("failed to read the app root: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tempPointerPrefix) {
			t.Errorf("temporary pointer %s was left behind", e.Name())
		}
	}
}

func TestInstall_PublishesThroughAPointerAndCollectsThePrevious(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	setBuildStamp(t, "v1.0.0", "aaaaaaa")

	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: generationExtractor("A")}
	if err := dg.Install(); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}
	assertPointsAt(t, "configs-v1.0.0-aaaaaaa")

	// An upgrade must swap the pointer and take the superseded tree with it.
	setBuildStamp(t, "v2.0.0", "bbbbbbb")
	dg.ExtractEmbedded = generationExtractor("B")
	if err := dg.Install(); err != nil {
		t.Fatalf("second Install() failed: %v", err)
	}
	assertPointsAt(t, "configs-v2.0.0-bbbbbbb")
	assertNotExists(t, filepath.Join(paths.Paths.App.Root, "configs-v1.0.0-aaaaaaa"))

	data, err := os.ReadFile(filepath.Join(pointerPath(), "git", ".gitconfig"))
	if err != nil {
		t.Fatalf("failed to read through the pointer: %v", err)
	}
	if string(data) != "B" {
		t.Errorf("read %q through the pointer, want the new generation %q", string(data), "B")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

// TestInstall_ReExtractingAtTheSameStampKeepsTheTreeThroughASymlinkedAppRoot
// pins the one comparison in Install that is easy to get wrong. The previous
// target is discovered through EvalSymlinks and the new one is not, so when the
// app root is reached through a symlink — a /var/folders TMPDIR on macOS, a
// symlinked $HOME — the same directory has two spellings. Comparing full paths
// makes a same-stamp re-extract (`dg configure --force`) look like a generation
// change and delete the tree it just published.
func TestInstall_ReExtractingAtTheSameStampKeepsTheTreeThroughASymlinkedAppRoot(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	setBuildStamp(t, "v1.0.0", "aaaaaaa")

	// Point App.Root at a symlink to the real root, so EvalSymlinks and the
	// raw path disagree the way they do on a real macOS temp dir.
	realRoot := filepath.Join(t.TempDir(), "real-app")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatalf("failed to create the real app root: %v", err)
	}
	linkedRoot := filepath.Join(t.TempDir(), "linked-app")
	if err := os.Symlink(realRoot, linkedRoot); err != nil {
		t.Fatalf("failed to create the symlinked app root: %v", err)
	}
	origRoot := paths.Paths.App.Root
	t.Cleanup(func() { paths.Paths.App.Root = origRoot })
	paths.Paths.App.Root = linkedRoot

	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: mockExtractor}
	if err := dg.Install(); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}
	// Same stamp, same binary: this is what --force does.
	if err := dg.Install(); err != nil {
		t.Fatalf("second Install() failed: %v", err)
	}

	assertPointsAt(t, "configs-v1.0.0-aaaaaaa")
	if _, err := os.Stat(filepath.Join(pointerPath(), "git", ".gitconfig")); err != nil {
		t.Fatalf("Install deleted the tree it had just published: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestInstall_SweepsDebrisFromAnInterruptedExtract(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	setBuildStamp(t, "v1.0.0", "aaaaaaa")

	root := paths.Paths.App.Root
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("failed to create the app root: %v", err)
	}
	// A half-written extract from a run that was killed, plus a stranded old
	// generation whose immediate removal failed.
	debris := []string{"configs-v0.9.0-ccccccc", "configs-v1.1.0-ddddddd"}
	for _, name := range debris {
		if err := os.MkdirAll(filepath.Join(root, name, "partial"), 0o755); err != nil {
			t.Fatalf("failed to create debris: %v", err)
		}
	}
	// Something that is not devgeta's must survive the sweep untouched.
	bystander := filepath.Join(root, "configs.backup")
	if err := os.MkdirAll(bystander, 0o755); err != nil {
		t.Fatalf("failed to create the bystander directory: %v", err)
	}

	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: mockExtractor}
	if err := dg.Install(); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}

	for _, name := range debris {
		assertNotExists(t, filepath.Join(root, name))
	}
	if _, err := os.Stat(bystander); err != nil {
		t.Errorf("the sweep removed a directory that is not a devgeta extract: %v", err)
	}
	assertPointsAt(t, "configs-v1.0.0-aaaaaaa")

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestUninstall_RemovesThePointerTargetNotJustTheLink(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	setBuildStamp(t, "v1.0.0", "aaaaaaa")

	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: mockExtractor}
	if err := dg.Install(); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}
	root := paths.Paths.App.Root
	target := filepath.Join(root, "configs-v1.0.0-aaaaaaa")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected the extract at %s: %v", target, err)
	}

	if err := dg.Uninstall(); err != nil {
		t.Fatalf("Uninstall() failed: %v", err)
	}

	assertNotExists(t, pointerPath())
	// os.RemoveAll on a symlink drops only the link; the tree must go too, or
	// uninstall orphans the whole extract under the app root.
	assertNotExists(t, target)

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestUninstall_PointerOutsideTheAppRootLeavesItsTargetIntact(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	root := paths.Paths.App.Root
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("failed to create the app root: %v", err)
	}
	// A directory anything on the machine could point `configs` at. Resolving
	// the pointer and removing the result without validating it would delete
	// this (CLAUDE.md §4).
	victim := filepath.Join(t.TempDir(), "documents")
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatalf("failed to create the victim directory: %v", err)
	}
	precious := filepath.Join(victim, "precious.txt")
	if err := os.WriteFile(precious, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("failed to create the victim file: %v", err)
	}

	pointer := pointerPath()
	if err := os.Symlink(victim, pointer); err != nil {
		t.Fatalf("failed to create the hostile pointer: %v", err)
	}

	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: mockExtractor}
	if err := dg.Uninstall(); err != nil {
		t.Fatalf("Uninstall() failed: %v", err)
	}

	assertNotExists(t, pointer)
	if _, err := os.Stat(precious); err != nil {
		t.Fatalf("Uninstall deleted a directory outside the app root: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestUninstall_PointerInsideTheAppRootButNotAnExtractLeavesItsTargetIntact(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	root := paths.Paths.App.Root
	// Directly under the app root, but not a name Install ever produces, so
	// removing it would still be removing something devgeta did not create.
	sibling := filepath.Join(root, "user-data")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatalf("failed to create the sibling directory: %v", err)
	}
	keep := filepath.Join(sibling, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("failed to create the sibling file: %v", err)
	}
	pointer := pointerPath()
	if err := os.Symlink("user-data", pointer); err != nil {
		t.Fatalf("failed to create the pointer: %v", err)
	}

	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: mockExtractor}
	if err := dg.Uninstall(); err != nil {
		t.Fatalf("Uninstall() failed: %v", err)
	}

	assertNotExists(t, pointer)
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("Uninstall deleted a directory it did not create: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

// --- The one-time migration and its three interrupted states ----------------
//
// Each case starts from the exact on-disk shape by hand rather than trying to
// land a kill at the right instant.

// seedLegacyTree writes a pre-pointer config tree at path, marked so the test
// can tell whether the old tree survived.
func seedLegacyTree(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, "git"), 0o755); err != nil {
		t.Fatalf("failed to create %s: %v", path, err)
	}
	if err := os.WriteFile(
		filepath.Join(path, "git", "old-marker.txt"),
		[]byte("old"),
		0o644,
	); err != nil {
		t.Fatalf("failed to write the legacy marker: %v", err)
	}
}

// assertMigrated asserts the migration finished: a live pointer at this build's
// extract, a complete tree behind it, and no `configs.legacy` left.
func assertMigrated(t *testing.T, stampedName string) {
	t.Helper()
	assertPointsAt(t, stampedName)
	assertNotExists(t, legacyPath())
	if _, err := os.Stat(filepath.Join(pointerPath(), "git", ".gitconfig")); err != nil {
		t.Errorf("expected a complete extracted tree behind the pointer: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pointerPath(), "git", "old-marker.txt")); err == nil {
		t.Error("the superseded tree is still being served through the pointer")
	}
}

func TestMigration_RealDirectoryBecomesAPointer(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	setBuildStamp(t, "v1.0.0", "aaaaaaa")

	seedLegacyTree(t, pointerPath())

	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: mockExtractor}
	if err := dg.Install(); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}
	assertMigrated(t, "configs-v1.0.0-aaaaaaa")

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

// Repair state 1: interrupted between renaming `configs` aside and swapping the
// pointer in. `configs.legacy` holds the only copy of the tree.
func TestMigration_RepairsConfigsMissingWithLegacyPresent(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	setBuildStamp(t, "v1.0.0", "aaaaaaa")

	if err := os.MkdirAll(paths.Paths.App.Root, 0o755); err != nil {
		t.Fatalf("failed to create the app root: %v", err)
	}
	seedLegacyTree(t, legacyPath())
	assertNotExists(t, pointerPath())

	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: mockExtractor}
	if err := dg.Install(); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}
	assertMigrated(t, "configs-v1.0.0-aaaaaaa")

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

// Repair state 2: interrupted between swapping the pointer in and removing the
// old tree. The pointer is already live; `configs.legacy` is a leftover.
func TestMigration_RepairsSymlinkWithLegacyPresent(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	setBuildStamp(t, "v1.0.0", "aaaaaaa")

	root := paths.Paths.App.Root
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("failed to create the app root: %v", err)
	}
	seedLegacyTree(t, legacyPath())
	target := filepath.Join(root, "configs-v1.0.0-aaaaaaa")
	if err := mockExtractor(target); err != nil {
		t.Fatalf("failed to seed the new extract: %v", err)
	}
	if err := os.Symlink("configs-v1.0.0-aaaaaaa", pointerPath()); err != nil {
		t.Fatalf("failed to seed the pointer: %v", err)
	}

	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: mockExtractor}
	if err := dg.Install(); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}
	assertMigrated(t, "configs-v1.0.0-aaaaaaa")

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

// Repair state 3: a real `configs` directory alongside a leftover
// `configs.legacy`. `configs` is authoritative; the leftover is redundant.
func TestMigration_RepairsRealDirectoryWithLegacyPresent(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	setBuildStamp(t, "v1.0.0", "aaaaaaa")

	if err := os.MkdirAll(paths.Paths.App.Root, 0o755); err != nil {
		t.Fatalf("failed to create the app root: %v", err)
	}
	seedLegacyTree(t, pointerPath())
	seedLegacyTree(t, legacyPath())

	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: mockExtractor}
	if err := dg.Install(); err != nil {
		t.Fatalf("Install() failed: %v", err)
	}
	assertMigrated(t, "configs-v1.0.0-aaaaaaa")

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

// --- Stamp-and-skip ---------------------------------------------------------

func TestInstallIfStale_SkipsWhenTheStampMatches(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	setBuildStamp(t, "v1.0.0", "aaaaaaa")

	var runs int32
	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: countingExtractor(&runs)}
	if err := dg.InstallIfStale(); err != nil {
		t.Fatalf("first InstallIfStale() failed: %v", err)
	}
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("expected the first call to extract once, got %d extracts", got)
	}

	for i := 0; i < 3; i++ {
		if err := dg.InstallIfStale(); err != nil {
			t.Fatalf("InstallIfStale() failed: %v", err)
		}
	}
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Errorf("expected later calls to skip the extract, got %d extracts total", got)
	}
	assertPointsAt(t, "configs-v1.0.0-aaaaaaa")

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestInstallIfStale_ReExtractsWhenTheStampChanges(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	setBuildStamp(t, "v1.0.0", "aaaaaaa")

	var runs int32
	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: countingExtractor(&runs)}
	if err := dg.InstallIfStale(); err != nil {
		t.Fatalf("InstallIfStale() failed: %v", err)
	}

	setBuildStamp(t, "v2.0.0", "bbbbbbb")
	if err := dg.InstallIfStale(); err != nil {
		t.Fatalf("InstallIfStale() after the upgrade failed: %v", err)
	}
	if got := atomic.LoadInt32(&runs); got != 2 {
		t.Errorf("expected an upgrade to re-extract, got %d extracts total", got)
	}
	assertPointsAt(t, "configs-v2.0.0-bbbbbbb")

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestInstallIfStale_ReExtractsWhenTheTargetIsMissing(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	setBuildStamp(t, "v1.0.0", "aaaaaaa")

	var runs int32
	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: countingExtractor(&runs)}
	if err := dg.InstallIfStale(); err != nil {
		t.Fatalf("InstallIfStale() failed: %v", err)
	}
	// A pointer naming the right stamp is not enough — the tree has to be
	// there, or a wiped extract would read as current forever.
	if err := os.RemoveAll(
		filepath.Join(paths.Paths.App.Root, "configs-v1.0.0-aaaaaaa"),
	); err != nil {
		t.Fatalf("failed to remove the extract: %v", err)
	}
	if err := dg.InstallIfStale(); err != nil {
		t.Fatalf("second InstallIfStale() failed: %v", err)
	}
	if got := atomic.LoadInt32(&runs); got != 2 {
		t.Errorf("expected a dangling pointer to force a re-extract, got %d extracts", got)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestInstallIfStale_ReExtractsWhenAMigrationIsUnfinished(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	setBuildStamp(t, "v1.0.0", "aaaaaaa")

	var runs int32
	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: countingExtractor(&runs)}
	if err := dg.InstallIfStale(); err != nil {
		t.Fatalf("InstallIfStale() failed: %v", err)
	}
	// The pointer looks current, but a leftover configs.legacy means the
	// migration never finished; skipping would strand it forever.
	seedLegacyTree(t, legacyPath())
	if err := dg.InstallIfStale(); err != nil {
		t.Fatalf("second InstallIfStale() failed: %v", err)
	}
	if got := atomic.LoadInt32(&runs); got != 2 {
		t.Errorf("expected an unfinished migration to force a re-extract, got %d extracts", got)
	}
	assertNotExists(t, legacyPath())

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

// TestForceConfigure_ReExtractsEvenWhenTheStampMatches is the other half of the
// stamp-and-skip contract: `dg configure --force` must stay a repair path for a
// hand-edited tree, which routing it through the stamp check would defeat.
func TestForceConfigure_ReExtractsEvenWhenTheStampMatches(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	setBuildStamp(t, "v1.0.0", "aaaaaaa")

	var runs int32
	dg := &Devgeta{Base: tc.MockApp.Base, ExtractEmbedded: countingExtractor(&runs)}
	if err := dg.InstallIfStale(); err != nil {
		t.Fatalf("InstallIfStale() failed: %v", err)
	}
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("expected one extract from the initial install, got %d", got)
	}

	// A user hand-edited a deployed template; --force must put it back.
	edited := filepath.Join(pointerPath(), "git", ".gitconfig")
	if err := os.WriteFile(edited, []byte("hand edited\n"), 0o644); err != nil {
		t.Fatalf("failed to hand-edit a config: %v", err)
	}

	if err := dg.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}
	if got := atomic.LoadInt32(&runs); got != 2 {
		t.Errorf("expected --force to re-extract despite the matching stamp, got %d extracts", got)
	}
	data, err := os.ReadFile(edited)
	if err != nil {
		t.Fatalf("failed to read the repaired config: %v", err)
	}
	if string(data) == "hand edited\n" {
		t.Error("--force did not restore the hand-edited config")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}
