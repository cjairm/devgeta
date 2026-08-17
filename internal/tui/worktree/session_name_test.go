package tuiworktree

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/paths"
)

// testLabel is a stand-in folder label for the name-generation tests, standing
// where sessionLabelForDir's output would in production. Session names built
// from it are "<testLabel>-<character>".
const testLabel = "myrepo"

func TestNextFreeSessionNamePicksFirstInOrder(t *testing.T) {
	// Order [0,1,2,...] with an empty taken-set must return the first
	// character in dragonBallNames, prefixed with the label.
	order := seqOrder(len(dragonBallNames))
	got := nextFreeSessionName(testLabel, map[string]bool{}, order)
	want := testLabel + "-" + dragonBallNames[0]
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestNextFreeSessionNameSkipsTaken(t *testing.T) {
	order := seqOrder(len(dragonBallNames))
	taken := map[string]bool{
		testLabel + "-" + dragonBallNames[0]: true,
		testLabel + "-" + dragonBallNames[1]: true,
	}
	got := nextFreeSessionName(testLabel, taken, order)
	want := testLabel + "-" + dragonBallNames[2]
	if got != want {
		t.Errorf("expected the first free character %q, got %q", want, got)
	}
}

func TestNextFreeSessionNameNeverReturnsTaken(t *testing.T) {
	order := seqOrder(len(dragonBallNames))
	// Every bare character is taken — the numeric-suffix fallback must kick in
	// and still return something absent from taken.
	taken := map[string]bool{}
	for _, n := range dragonBallNames {
		taken[testLabel+"-"+n] = true
	}
	got := nextFreeSessionName(testLabel, taken, order)
	if taken[got] {
		t.Errorf("fallback returned a taken name %q", got)
	}
	if !strings.HasPrefix(got, testLabel+"-"+dragonBallNames[0]+"-") {
		t.Errorf("expected a numeric-suffixed fallback on the first character, got %q", got)
	}
}

func TestNextFreeSessionNameFallbackIncrements(t *testing.T) {
	order := seqOrder(len(dragonBallNames))
	base := testLabel + "-" + dragonBallNames[0]
	taken := map[string]bool{}
	for _, n := range dragonBallNames {
		taken[testLabel+"-"+n] = true
	}
	// -2 also taken → must land on -3.
	taken[base+"-2"] = true
	got := nextFreeSessionName(testLabel, taken, order)
	if got != base+"-3" {
		t.Errorf("expected %q, got %q", base+"-3", got)
	}
}

func TestRandomSessionNameOrderIsAPermutation(t *testing.T) {
	order := randomSessionNameOrder()
	if len(order) != len(dragonBallNames) {
		t.Fatalf(
			"expected an order covering all %d names, got %d",
			len(dragonBallNames),
			len(order),
		)
	}
	seen := make(map[int]bool, len(order))
	for _, i := range order {
		if i < 0 || i >= len(dragonBallNames) {
			t.Fatalf("index %d out of range", i)
		}
		if seen[i] {
			t.Fatalf("index %d repeated — not a permutation", i)
		}
		seen[i] = true
	}
}

func TestSessionLabelForDir(t *testing.T) {
	home := config.CanonicalRepoPath(paths.Paths.Home.Root)
	cases := []struct {
		name    string
		workdir string
		want    string
	}{
		{"plain folder", "/Users/x/dev/myrepo", "myrepo"},
		{"dots and colons flattened", "/Users/x/dev/my.app:v2", "my_app_v2"},
		{"home maps to home", home, "home"},
		// The raw, unresolved home - which is what production actually passes,
		// since sessionWorkdir is whatever validateSessionDirFn returned and is
		// never symlink-resolved. When home is reached through a symlink this
		// differs from the canonical form above, and comparing only the canonical
		// home made it fall through to the account-name basename.
		{"unresolved home still maps to home", paths.Paths.Home.Root, "home"},
		{"filesystem root falls back", "/", defaultSessionLabel},
		{"empty falls back", "", defaultSessionLabel},
		{"dot falls back", ".", defaultSessionLabel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionLabelForDir(tc.workdir); got != tc.want {
				t.Errorf("sessionLabelForDir(%q) = %q, want %q", tc.workdir, got, tc.want)
			}
		})
	}
}

// TestSessionLabelForDirRecognizesHomeHoweverSpelled pins the home branch to the
// folder rather than to one string form of it. The regression it guards is a
// real-world home reached through a symlink (/home/x -> /mnt/data/x on Linux, a
// relocated home on macOS): when only one side of the comparison was
// symlink-resolved, the branch silently missed and the session label fell back to
// the basename — the opaque account name.
func TestSessionLabelForDirRecognizesHomeHoweverSpelled(t *testing.T) {
	tempDir := t.TempDir()
	realHome := filepath.Join(tempDir, "real-home")
	if err := os.MkdirAll(realHome, 0o755); err != nil {
		t.Fatalf("failed to create the real home dir: %v", err)
	}
	linkedHome := filepath.Join(tempDir, "linked-home")
	if err := os.Symlink(realHome, linkedHome); err != nil {
		t.Fatalf("failed to symlink home: %v", err)
	}

	oldHome := paths.Paths.Home.Root
	t.Cleanup(func() { paths.Paths.Home.Root = oldHome })
	paths.Paths.Home.Root = linkedHome
	// ExpandHome resolves "~" from HOME, not from paths.Paths, so the tilde case
	// needs both pointed at the same symlinked home.
	t.Setenv("HOME", linkedHome)

	cases := []struct {
		name    string
		workdir string
	}{
		{"unresolved symlinked home", linkedHome},
		{"resolved home", config.CanonicalRepoPath(linkedHome)},
		{"trailing separator", linkedHome + string(filepath.Separator)},
		{"tilde", "~"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionLabelForDir(tc.workdir); got != "home" {
				t.Errorf("sessionLabelForDir(%q) = %q, want %q", tc.workdir, got, "home")
			}
		})
	}
}

// TestSessionLabelForDirDotFallsBackEvenFromHome is the counterpart to the test
// above, and the reason the basename guard runs before the home check rather
// than after it. Canonicalizing makes a path absolute, so "." becomes the
// working directory — and when that directory IS home, a home check placed
// first matches and labels a blank workdir "home" instead of taking the
// fallback. The bug is invisible from anywhere else, which is why this test
// moves into home rather than relying on the table above: `go test` runs in the
// package directory, where "." and home differ and the wrong order still passes.
func TestSessionLabelForDirDotFallsBackEvenFromHome(t *testing.T) {
	home := t.TempDir()

	oldHome := paths.Paths.Home.Root
	t.Cleanup(func() { paths.Paths.Home.Root = oldHome })
	paths.Paths.Home.Root = home
	t.Setenv("HOME", home)
	t.Chdir(home)

	for _, workdir := range []string{".", ""} {
		t.Run("workdir "+strconv.Quote(workdir), func(t *testing.T) {
			if got := sessionLabelForDir(workdir); got != defaultSessionLabel {
				t.Errorf(
					"sessionLabelForDir(%q) from home = %q, want %q — a blank workdir must take the fallback, not resolve to the current directory and report home",
					workdir,
					got,
					defaultSessionLabel,
				)
			}
		})
	}
}

// seqOrder returns [0,1,2,...,n-1], a deterministic order for tests.
func seqOrder(n int) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	return order
}
