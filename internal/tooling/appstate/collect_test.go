package appstate

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/tooling/archive"
)

// fakePorter is a StatePorter under the test's control: real directories in
// a t.TempDir, and canned answers for the running check. Every test in this
// package builds its roots this way rather than touching a real profile.
type fakePorter struct {
	name     string
	roots    map[string]string
	rootsErr error
	groups   []apps.StateGroup
	running  bool
	runErr   error
}

func (f *fakePorter) Name() string {
	if f.name == "" {
		return "brave"
	}
	return f.name
}

func (f *fakePorter) StateRoots() (map[string]string, error) { return f.roots, f.rootsErr }
func (f *fakePorter) StateGroups() []apps.StateGroup         { return f.groups }
func (f *fakePorter) IsRunning() (bool, error)               { return f.running, f.runErr }

// braveLikeGroups mirrors the shape of ADR-0045's table without restating it:
// single-file groups and directory groups, default-on and default-off. The
// real table lives in the Brave adapter and is asserted there.
func braveLikeGroups() []apps.StateGroup {
	return []apps.StateGroup{
		{Name: "bookmarks", Paths: []string{"Bookmarks"}, Default: true, Why: "plain JSON"},
		{Name: "preferences", Paths: []string{"Preferences"}, Default: true, Why: "settings"},
		{Name: "tabs", Paths: []string{"Sessions"}, Default: true, Why: "open tabs"},
		{Name: "history", Paths: []string{"History"}, Default: false, Why: "browsing record"},
	}
}

// newTestPorter builds a two-profile tree under t.TempDir and returns a
// porter over it. Every profile gets the same files, so a test asserting the
// profile prefix is asserting something that would actually collide without
// it.
func newTestPorter(t *testing.T, profileKeys ...string) *fakePorter {
	t.Helper()
	base := filepath.Join(t.TempDir(), "Brave-Browser")
	roots := map[string]string{}
	for _, key := range profileKeys {
		root := filepath.Join(base, key)
		writeTestFile(t, filepath.Join(root, "Bookmarks"), "bookmarks of "+key)
		writeTestFile(t, filepath.Join(root, "Preferences"), "prefs")
		writeTestFile(t, filepath.Join(root, "Sessions", "Session_1"), "tab state")
		writeTestFile(t, filepath.Join(root, "Sessions", "Tabs_1"), "more tab state")
		writeTestFile(t, filepath.Join(root, "History"), "browsing")
		// Not in any group: proof that collection is an allowlist and not a
		// copy of the profile with a few things removed.
		writeTestFile(t, filepath.Join(root, "Favicons"), "regenerable")
		writeTestFile(t, filepath.Join(root, "Login Data"), "secret")
		roots[key] = root
	}
	return &fakePorter{roots: roots, groups: braveLikeGroups()}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// filePaths returns the plan's regular-file entries, sorted, for comparison.
func filePaths(plan *Plan) []string {
	var got []string
	for _, e := range plan.Scan.Entries {
		if e.Kind == archive.KindFile {
			got = append(got, e.Path)
		}
	}
	sort.Strings(got)
	return got
}

func assertPaths(t *testing.T, plan *Plan, want []string) {
	t.Helper()
	got := filePaths(plan)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("collected files:\n  got:  %v\n  want: %v", got, want)
	}
}

func TestCollectDefaultGroupsAcrossEveryProfile(t *testing.T) {
	porter := newTestPorter(t, "Default", "Profile 1")

	plan, err := Collect(porter, Selection{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// Every default-on group of both profiles, each under its profile key —
	// and nothing else. History is default-off; Favicons and Login Data are
	// not groups at all.
	assertPaths(t, plan, []string{
		"Default/Bookmarks",
		"Default/Preferences",
		"Default/Sessions/Session_1",
		"Default/Sessions/Tabs_1",
		"Profile 1/Bookmarks",
		"Profile 1/Preferences",
		"Profile 1/Sessions/Session_1",
		"Profile 1/Sessions/Tabs_1",
	})
}

// TestCollectSourceRootIsTheSharedBase pins the half of the layout that is
// not visible in the entry paths: archive.Write opens sourceRoot + entry
// path, so the base has to be the profiles' shared parent for the profile
// key to resolve.
func TestCollectSourceRootIsTheSharedBase(t *testing.T) {
	porter := newTestPorter(t, "Default", "Profile 1")

	plan, err := Collect(porter, Selection{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	wantBase := filepath.Dir(porter.roots["Default"])
	if plan.Base != wantBase {
		t.Errorf("Base = %q, want %q", plan.Base, wantBase)
	}
	for _, e := range plan.Scan.Entries {
		if e.Kind != archive.KindFile {
			continue
		}
		full := filepath.Join(plan.Base, filepath.FromSlash(e.Path))
		if _, err := os.Stat(full); err != nil {
			t.Errorf("entry %q does not resolve under Base: %v", e.Path, err)
		}
	}
}

// TestCollectKeepsTwoProfilesFilesApart is the reason the profile key is a
// prefix and not cosmetic: both profiles have a "Bookmarks", and without the
// prefix they are the same tar member and the second silently wins.
func TestCollectKeepsTwoProfilesFilesApart(t *testing.T) {
	porter := newTestPorter(t, "Default", "Profile 1")

	plan, err := Collect(porter, Selection{Groups: []string{"bookmarks"}})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	assertPaths(t, plan, []string{"Default/Bookmarks", "Profile 1/Bookmarks"})
}

func TestCollectNarrowsToNamedGroups(t *testing.T) {
	porter := newTestPorter(t, "Default")

	plan, err := Collect(porter, Selection{Groups: []string{"bookmarks", "history"}})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	assertPaths(t, plan, []string{"Default/Bookmarks", "Default/History"})
}

func TestCollectNarrowsToNamedProfiles(t *testing.T) {
	porter := newTestPorter(t, "Default", "Profile 1")

	plan, err := Collect(porter, Selection{
		Profiles: []string{"Profile 1"},
		Groups:   []string{"bookmarks"},
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	assertPaths(t, plan, []string{"Profile 1/Bookmarks"})
}

func TestCollectRefusesAnUnknownGroup(t *testing.T) {
	porter := newTestPorter(t, "Default")

	_, err := Collect(porter, Selection{Groups: []string{"nope"}})
	if err == nil {
		t.Fatal("expected an error for an unknown group, got nil")
	}
	// An error that does not say what the real names are leaves the user
	// guessing at exactly the moment they already guessed wrong.
	for _, want := range []string{"nope", "bookmarks", "history"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestCollectRefusesAnUnknownProfile(t *testing.T) {
	porter := newTestPorter(t, "Default", "Profile 1")

	_, err := Collect(porter, Selection{Profiles: []string{"Profile 7"}})
	if err == nil {
		t.Fatal("expected an error for an unknown profile, got nil")
	}
	for _, want := range []string{"Profile 7", "Default", "Profile 1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// TestCollectToleratesAMissingPath: a fresh profile has no Sessions/ yet,
// which is normal and not a failure.
func TestCollectToleratesAMissingPath(t *testing.T) {
	porter := newTestPorter(t, "Default")
	if err := os.RemoveAll(filepath.Join(porter.roots["Default"], "Sessions")); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}

	plan, err := Collect(porter, Selection{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	assertPaths(t, plan, []string{"Default/Bookmarks", "Default/Preferences"})
}

func TestCollectSumsBytes(t *testing.T) {
	porter := newTestPorter(t, "Default")

	plan, err := Collect(porter, Selection{Groups: []string{"bookmarks"}})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	want := int64(len("bookmarks of Default"))
	if plan.Scan.TotalBytes != want {
		t.Errorf("TotalBytes = %d, want %d", plan.Scan.TotalBytes, want)
	}
}

// TestCollectDropsLinks: import rejects any bundle carrying a link member,
// so a link found under an allowlisted directory is dropped here rather than
// packed into a bundle that cannot be imported.
func TestCollectDropsLinks(t *testing.T) {
	porter := newTestPorter(t, "Default")
	link := filepath.Join(porter.roots["Default"], "Sessions", "elsewhere")
	if err := os.Symlink("/etc/passwd", link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	plan, err := Collect(porter, Selection{Groups: []string{"tabs"}})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	for _, e := range plan.Scan.Entries {
		if e.Kind != archive.KindFile && e.Kind != archive.KindDir {
			t.Errorf(
				"entry %q has kind %v; only files and directories may be collected",
				e.Path,
				e.Kind,
			)
		}
		if strings.HasSuffix(e.Path, "elsewhere") {
			t.Errorf("symlink %q was collected", e.Path)
		}
	}
}

// TestCollectReportsEveryGroup backs --dry-run: the report lists groups that
// are off as well as on, because "what did NOT move" is the half of the
// answer a user cannot get any other way.
func TestCollectReportsEveryGroup(t *testing.T) {
	porter := newTestPorter(t, "Default")

	plan, err := Collect(porter, Selection{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	if len(plan.Profiles) != 1 || plan.Profiles[0].Key != "Default" {
		t.Fatalf("Profiles = %+v, want one entry keyed Default", plan.Profiles)
	}
	groups := plan.Profiles[0].Groups
	if len(groups) != len(braveLikeGroups()) {
		t.Fatalf("reported %d groups, want %d", len(groups), len(braveLikeGroups()))
	}

	byName := map[string]GroupReport{}
	for _, g := range groups {
		byName[g.Name] = g
	}
	if g := byName["bookmarks"]; !g.Selected || !g.Default || g.Bytes == 0 || g.Why == "" {
		t.Errorf("bookmarks report = %+v, want selected, default, sized and explained", g)
	}
	if g := byName["history"]; g.Selected || g.Default {
		t.Errorf("history report = %+v, want neither selected nor default", g)
	}
	if g := byName["tabs"]; g.Bytes != int64(len("tab state")+len("more tab state")) {
		t.Errorf("tabs Bytes = %d, want the sum of the directory's files", g.Bytes)
	}
}

// TestCollectRefusesRootsWithoutOneSharedBase guards the contract
// StatePorter states: the bundle is written relative to one base directory,
// so roots under different parents cannot be expressed in one tar.
func TestCollectRefusesRootsWithoutOneSharedBase(t *testing.T) {
	porter := newTestPorter(t, "Default")
	porter.roots["Elsewhere"] = filepath.Join(t.TempDir(), "Other", "Elsewhere")

	_, err := Collect(porter, Selection{})
	if err == nil {
		t.Fatal("expected an error for roots under different parents, got nil")
	}
}

// TestCollectRefusesAnAdapterNamingADeniedPath makes the denylist a runtime
// guarantee and not only a test-time one: the check that fails the build in
// denylist_test.go also refuses the run.
func TestCollectRefusesAnAdapterNamingADeniedPath(t *testing.T) {
	porter := newTestPorter(t, "Default")
	porter.groups = append(
		porter.groups,
		apps.StateGroup{Name: "passwords", Paths: []string{"Login Data"}, Default: true},
	)

	_, err := Collect(porter, Selection{})
	if err == nil {
		t.Fatal("expected an error for an adapter naming a denied path, got nil")
	}
	if !strings.Contains(err.Error(), "Login Data") {
		t.Errorf("error %q does not name the denied path", err)
	}
}
