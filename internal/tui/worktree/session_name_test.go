package tuiworktree

import (
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
		{"filesystem root falls back", "/", defaultSessionLabel},
		{"empty falls back", "", defaultSessionLabel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionLabelForDir(tc.workdir); got != tc.want {
				t.Errorf("sessionLabelForDir(%q) = %q, want %q", tc.workdir, got, tc.want)
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
