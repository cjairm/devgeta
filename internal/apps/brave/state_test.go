package brave

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/paths"
)

var _ apps.StatePorter = (*Brave)(nil)

// TestStateGroupsMatchTheADR asserts the table in ADR-0045 §"Brave's groups"
// verbatim — names, paths and defaults. The table is not restated in the
// cycle doc or in docs/apps/brave.md for exactly this reason: one
// authoritative copy, and a silent edit to the adapter fails here rather
// than shipping. Changing a row is an ADR amendment, not an implementation
// detail.
func TestStateGroupsMatchTheADR(t *testing.T) {
	want := []apps.StateGroup{
		{Name: "bookmarks", Paths: []string{"Bookmarks"}, Default: true},
		{Name: "preferences", Paths: []string{"Preferences"}, Default: true},
		{Name: "tabs", Paths: []string{"Sessions"}, Default: true},
		{
			Name:    "extension-settings",
			Paths:   []string{"Extension State", "Local Extension Settings"},
			Default: false,
		},
		{Name: "extensions", Paths: []string{"Extensions"}, Default: false},
		{Name: "history", Paths: []string{"History"}, Default: false},
	}

	got := (&Brave{}).StateGroups()
	if len(got) != len(want) {
		t.Fatalf("StateGroups returned %d groups, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.Name != w.Name {
			t.Errorf("group %d: name = %q, want %q", i, g.Name, w.Name)
		}
		if strings.Join(g.Paths, ",") != strings.Join(w.Paths, ",") {
			t.Errorf("%s: paths = %v, want %v", w.Name, g.Paths, w.Paths)
		}
		if g.Default != w.Default {
			t.Errorf("%s: default = %v, want %v", w.Name, g.Default, w.Default)
		}
		if g.Why == "" {
			t.Errorf("%s: has no reason, which --dry-run prints beside it", w.Name)
		}
	}
}

// TestStateGroupsOmitTheRegenerablePaths: Favicons and Top Sites are not
// opt-in and not skipped-with-a-note — they have no flag at all, because
// both rebuild themselves on the new machine (ADR-0045).
func TestStateGroupsOmitTheRegenerablePaths(t *testing.T) {
	for _, group := range (&Brave{}).StateGroups() {
		for _, p := range group.Paths {
			if p == "Favicons" || p == "Top Sites" {
				t.Errorf("%q names %q, which ADR-0045 omits entirely", group.Name, p)
			}
		}
	}
}

func TestStateRootsPerPlatform(t *testing.T) {
	tests := []struct {
		name  string
		isMac bool
		base  func() string
	}{
		{
			"macOS",
			true,
			func() string {
				return paths.GetHomeDir(
					"Library",
					"Application Support",
					"BraveSoftware",
					"Brave-Browser",
				)
			},
		},
		{
			// Chromium's own rule on Linux is $XDG_CONFIG_HOME, so going
			// through paths.GetConfigDir gets it right for free.
			"Debian/Ubuntu",
			false,
			func() string { return paths.GetConfigDir("BraveSoftware", "Brave-Browser") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testutil.IsolateXDGDirs(t)
			base := tt.base()
			for _, dir := range []string{"Default", "Profile 1", "Profile 5"} {
				if err := os.MkdirAll(filepath.Join(base, dir), 0o755); err != nil {
					t.Fatalf("MkdirAll: %v", err)
				}
			}

			mockBase := cmd.NewMockBaseCommand()
			mockBase.IsMacResult = tt.isMac
			roots, err := (&Brave{Base: mockBase}).StateRoots()
			if err != nil {
				t.Fatalf("StateRoots: %v", err)
			}

			gotKeys := make([]string, 0, len(roots))
			for key := range roots {
				gotKeys = append(gotKeys, key)
			}
			sort.Strings(gotKeys)
			want := []string{"Default", "Profile 1", "Profile 5"}
			if strings.Join(gotKeys, ",") != strings.Join(want, ",") {
				t.Errorf("profiles = %v, want %v", gotKeys, want)
			}
			// Every root must sit at base/<key>: that is what makes the
			// profile key usable as the bundle's top-level directory.
			for key, root := range roots {
				if root != filepath.Join(base, key) {
					t.Errorf("root for %q = %q, want %q", key, root, filepath.Join(base, key))
				}
			}
		})
	}
}

// TestStateRootsIgnoresNonProfileDirectories: a Brave data directory holds
// plenty that is not a profile, and a "System Profile" is not one a user
// has state in.
func TestStateRootsIgnoresNonProfileDirectories(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	base := paths.GetConfigDir("BraveSoftware", "Brave-Browser")
	for _, dir := range []string{
		"Default",
		"Profile 2",
		"System Profile",
		"Guest Profile",
		"GrShaderCache",
		"Crashpad",
	} {
		if err := os.MkdirAll(filepath.Join(base, dir), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "Local State"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	mockBase := cmd.NewMockBaseCommand()
	roots, err := (&Brave{Base: mockBase}).StateRoots()
	if err != nil {
		t.Fatalf("StateRoots: %v", err)
	}

	if len(roots) != 2 || roots["Default"] == "" || roots["Profile 2"] == "" {
		t.Errorf("profiles = %v, want exactly Default and Profile 2", roots)
	}
}

func TestStateRootsWhenBraveWasNeverLaunched(t *testing.T) {
	testutil.IsolateXDGDirs(t)

	mockBase := cmd.NewMockBaseCommand()
	roots, err := (&Brave{Base: mockBase}).StateRoots()
	if err != nil {
		t.Fatalf("StateRoots on a machine with no Brave data directory: %v", err)
	}
	if len(roots) != 0 {
		t.Errorf("profiles = %v, want none", roots)
	}
}

func TestIsRunning(t *testing.T) {
	tests := []struct {
		name    string
		stdout  string
		execErr error
		want    bool
		wantErr bool
	}{
		{"a live process", "4213\n", nil, true, false},
		// pgrep exits 1 when nothing matched, which is an answer and not a
		// failure.
		{"nothing matched", "", errExitOne, false, false},
		{"pgrep itself failed", "", errors.New("permission denied"), false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockBase := cmd.NewMockBaseCommand()
			if tt.execErr == errExitOne {
				mockBase.ExecCommandError = testutil.ExitError(t, 1)
			} else {
				mockBase.ExecCommandError = tt.execErr
			}
			mockBase.ExecCommandStdout = tt.stdout

			got, err := (&Brave{Base: mockBase}).IsRunning()
			if (err != nil) != tt.wantErr {
				t.Fatalf("IsRunning error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("IsRunning = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIsRunningMatchesTheProcessNameExactly: `pgrep -f brave` would match
// the devgeta process running `dg import brave` and refuse every import on
// every machine. The name has to be matched exactly.
func TestIsRunningMatchesTheProcessNameExactly(t *testing.T) {
	mockBase := cmd.NewMockBaseCommand()
	mockBase.ExecCommandError = testutil.ExitError(t, 1)

	if _, err := (&Brave{Base: mockBase}).IsRunning(); err != nil {
		t.Fatalf("IsRunning: %v", err)
	}

	if len(mockBase.ExecCommandCalls) == 0 {
		t.Fatal("IsRunning never ran a process lookup")
	}
	for _, call := range mockBase.ExecCommandCalls {
		if call.Command != "pgrep" {
			t.Errorf("looked processes up with %q, want pgrep", call.Command)
		}
		if len(call.Args) == 0 || call.Args[0] != "-x" {
			t.Errorf("pgrep args = %v, want an exact-name match (-x)", call.Args)
		}
		if call.IsSudo {
			t.Error("a process lookup must not ask for sudo")
		}
	}
}

// errExitOne is a marker the table above swaps for a real *exec.ExitError,
// which needs a *testing.T to build.
var errExitOne = errors.New("exit status 1")
