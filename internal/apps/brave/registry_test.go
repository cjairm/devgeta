package brave

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/paths"
)

// writeLocalStateFixture puts a Local State with the given raw JSON into the
// sandboxed data directory and returns its path.
func writeLocalStateFixture(t *testing.T, body string) string {
	t.Helper()
	base := paths.GetConfigDir("BraveSoftware", "Brave-Browser")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(base, "Local State")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// TestReadProfileRegistryReturnsDisplayNames is the reason ADR-0046 exists: a
// profile's human-readable name is not in the profile directory, it is in
// Local State, so without reading it every imported profile arrives as
// "Person 1".
func TestReadProfileRegistryReturnsDisplayNames(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	writeLocalStateFixture(t, `{
	  "profile": {
	    "info_cache": {
	      "Default":   {"name": "Jair - Lever",  "avatar_icon": "chrome://theme/IDR_PROFILE_AVATAR_68"},
	      "Profile 5": {"name": "sadmin",        "avatar_icon": "chrome://theme/IDR_PROFILE_AVATAR_26"}
	    }
	  }
	}`)

	got, err := (&Brave{Base: cmd.NewMockBaseCommand()}).ReadProfileRegistry()
	if err != nil {
		t.Fatalf("ReadProfileRegistry: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("read %d profiles, want 2: %v", len(got), got)
	}
	if got["Default"].Name != "Jair - Lever" {
		t.Errorf("Default name = %q, want %q", got["Default"].Name, "Jair - Lever")
	}
	if got["Profile 5"].Name != "sadmin" {
		t.Errorf("Profile 5 name = %q, want %q", got["Profile 5"].Name, "sadmin")
	}
	if got["Profile 5"].Avatar != "chrome://theme/IDR_PROFILE_AVATAR_26" {
		t.Errorf("Profile 5 avatar = %q, want the one in the fixture", got["Profile 5"].Avatar)
	}
	if got["Profile 5"].Dir != "Profile 5" {
		t.Errorf("Profile 5 Dir = %q, want it keyed by its own directory", got["Profile 5"].Dir)
	}
}

// TestReadProfileRegistryOnAMachineWhereBraveNeverRan: the destination of a
// machine move may have no Local State at all. That is a machine with nothing
// registered yet, not a failure — refusing here would refuse the exact case
// ADR-0046 exists to serve.
func TestReadProfileRegistryOnAMachineWhereBraveNeverRan(t *testing.T) {
	testutil.IsolateXDGDirs(t)

	got, err := (&Brave{Base: cmd.NewMockBaseCommand()}).ReadProfileRegistry()
	if err != nil {
		t.Fatalf("ReadProfileRegistry with no data directory: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("read %v, want an empty registry", got)
	}
}

// readInfoCache reads back what EnsureProfiles wrote, as plain JSON, so the
// assertions are about the file Chromium will read rather than about the
// struct devgeta happens to keep in memory.
func readInfoCache(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back %s: %v", path, err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parsing back %s: %v\n%s", path, err, raw)
	}
	profile, _ := root["profile"].(map[string]any)
	cache, _ := profile["info_cache"].(map[string]any)
	out := map[string]map[string]any{}
	for k, v := range cache {
		entry, _ := v.(map[string]any)
		out[k] = entry
	}
	return out
}

// TestEnsureProfilesCreatesAndNamesAMissingProfile is the failure that opened
// ADR-0046: a bundle names "Profile 5", the destination has no such directory,
// and Chromium's UI cannot be made to produce that name because it allocates
// Profile N from a counter. The import has to create and register it.
func TestEnsureProfilesCreatesAndNamesAMissingProfile(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	statePath := writeLocalStateFixture(t, `{"profile": {"info_cache": {}}}`)
	b := &Brave{Base: cmd.NewMockBaseCommand()}

	created, err := b.EnsureProfiles(map[string]apps.StateProfileInfo{
		"Profile 5": {Dir: "Profile 5", Name: "sadmin", Avatar: "chrome://theme/IDR_PROFILE_AVATAR_26"},
	})
	if err != nil {
		t.Fatalf("EnsureProfiles: %v", err)
	}

	if len(created) != 1 || created[0] != "Profile 5" {
		t.Errorf("created = %v, want [Profile 5]", created)
	}

	dir := filepath.Join(paths.GetConfigDir("BraveSoftware", "Brave-Browser"), "Profile 5")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("profile directory %s was not created (err=%v)", dir, err)
	}

	entry := readInfoCache(t, statePath)["Profile 5"]
	if entry == nil {
		t.Fatalf("Profile 5 was not registered in Local State")
	}
	if entry["name"] != "sadmin" {
		t.Errorf("registered name = %v, want %q", entry["name"], "sadmin")
	}
	if entry["avatar_icon"] != "chrome://theme/IDR_PROFILE_AVATAR_26" {
		t.Errorf("registered avatar = %v, want the one supplied", entry["avatar_icon"])
	}
}

// TestEnsureProfilesNeverRenamesAProfileTheUserAlreadyHas is the rule that
// keeps an import from damaging the destination. Restoring state into a
// profile is one thing; relabelling the profile a user is already using with
// a name from another machine is another, and ADR-0046 forbids it.
//
// It also pins that unknown keys survive. Local State holds far more than
// devgeta models, and a write that dropped the rest would take the user's
// browser settings with it.
func TestEnsureProfilesNeverRenamesAProfileTheUserAlreadyHas(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	statePath := writeLocalStateFixture(t, `{
	  "browser": {"enabled_labs_experiments": ["a-flag"]},
	  "profile": {
	    "info_cache": {
	      "Default": {"name": "The Name On This Machine", "avatar_icon": "keep-me"}
	    }
	  }
	}`)
	b := &Brave{Base: cmd.NewMockBaseCommand()}

	created, err := b.EnsureProfiles(map[string]apps.StateProfileInfo{
		"Default":   {Dir: "Default", Name: "A Name From The Other Machine"},
		"Profile 1": {Dir: "Profile 1", Name: "Jair - Employ"},
	})
	if err != nil {
		t.Fatalf("EnsureProfiles: %v", err)
	}

	if len(created) != 1 || created[0] != "Profile 1" {
		t.Errorf("created = %v, want only [Profile 1] — Default already existed", created)
	}

	cache := readInfoCache(t, statePath)
	if got := cache["Default"]["name"]; got != "The Name On This Machine" {
		t.Errorf("Default was renamed to %v; an import must not rename an existing profile", got)
	}
	if got := cache["Default"]["avatar_icon"]; got != "keep-me" {
		t.Errorf("Default avatar = %v, want it untouched", got)
	}
	if got := cache["Profile 1"]["name"]; got != "Jair - Employ" {
		t.Errorf("Profile 1 name = %v, want it registered", got)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parsing: %v", err)
	}
	if _, ok := root["browser"]; !ok {
		t.Error("the browser key devgeta does not model was dropped by the write")
	}
}

// profilesCreated reads the counter Chromium uses to name the next profile.
func profilesCreated(t *testing.T, path string) float64 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	profile, _ := root["profile"].(map[string]any)
	n, _ := profile["profiles_created"].(float64)
	return n
}

// TestEnsureProfilesRaisesTheDirectoryCounter: Chromium names the next profile
// from profiles_created, not from what is on disk. Registering "Profile 5" on
// a machine whose counter is 1 and leaving it there means the next four
// profiles the user creates are named Profile 1..4 and the fifth collides with
// the directory this import just wrote.
func TestEnsureProfilesRaisesTheDirectoryCounter(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	statePath := writeLocalStateFixture(t,
		`{"profile": {"info_cache": {}, "profiles_created": 1}}`)

	if _, err := (&Brave{Base: cmd.NewMockBaseCommand()}).EnsureProfiles(
		map[string]apps.StateProfileInfo{"Profile 5": {Dir: "Profile 5", Name: "sadmin"}},
	); err != nil {
		t.Fatalf("EnsureProfiles: %v", err)
	}

	if got := profilesCreated(t, statePath); got < 6 {
		t.Errorf("profiles_created = %v, want at least 6 so Profile 5 cannot be handed out again", got)
	}
}

// TestEnsureProfilesNeverLowersTheDirectoryCounter: the counter records how
// many profiles this machine has ever made. Importing a low-numbered profile
// onto a machine that has made many must not walk it backwards, or Chromium
// starts reissuing names that are already on disk here.
func TestEnsureProfilesNeverLowersTheDirectoryCounter(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	statePath := writeLocalStateFixture(t,
		`{"profile": {"info_cache": {}, "profiles_created": 9}}`)

	if _, err := (&Brave{Base: cmd.NewMockBaseCommand()}).EnsureProfiles(
		map[string]apps.StateProfileInfo{"Profile 1": {Dir: "Profile 1", Name: "Jair - Employ"}},
	); err != nil {
		t.Fatalf("EnsureProfiles: %v", err)
	}

	if got := profilesCreated(t, statePath); got != 9 {
		t.Errorf("profiles_created = %v, want it left at 9", got)
	}
}

// TestRegistryPathsNamesLocalState: the import backs up every path it is about
// to write before the first write, so a failure rolls all of it back. The
// registry write has to be in that set or a half-done import leaves Chromium's
// profile list edited with no way back.
func TestRegistryPathsNamesLocalState(t *testing.T) {
	testutil.IsolateXDGDirs(t)

	got := (&Brave{Base: cmd.NewMockBaseCommand()}).RegistryPaths()

	want := filepath.Join(paths.GetConfigDir("BraveSoftware", "Brave-Browser"), "Local State")
	if len(got) != 1 || got[0] != want {
		t.Errorf("RegistryPaths() = %v, want [%s]", got, want)
	}
}

// TestEnsureProfilesRefusesAnUnreadableLocalState: overwriting a Local State
// devgeta could not parse would throw away every profile the user has, which
// is a far worse outcome than a refused import. The file must come back
// byte-for-byte unchanged.
func TestEnsureProfilesRefusesAnUnreadableLocalState(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	const corrupt = `{"profile": {"info_cache": {` // truncated mid-object
	statePath := writeLocalStateFixture(t, corrupt)

	_, err := (&Brave{Base: cmd.NewMockBaseCommand()}).EnsureProfiles(
		map[string]apps.StateProfileInfo{"Profile 1": {Dir: "Profile 1", Name: "Jair - Employ"}},
	)
	if err == nil {
		t.Fatal("EnsureProfiles succeeded on an unparseable Local State; it must refuse")
	}

	raw, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatalf("reading back: %v", readErr)
	}
	if string(raw) != corrupt {
		t.Errorf("Local State was rewritten:\n got %s\nwant %s", raw, corrupt)
	}
}

// profilesOrder reads the explicit ordering Chromium keeps when a user has
// arranged their profiles.
func profilesOrder(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	profile, _ := root["profile"].(map[string]any)
	raws, _ := profile["profiles_order"].([]any)
	var out []string
	for _, v := range raws {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

// TestEnsureProfilesAppendsToAnExistingOrder: when the user has arranged their
// profiles, Chromium reads that order and shows only what it lists. A new
// profile left out of it is registered but invisible.
func TestEnsureProfilesAppendsToAnExistingOrder(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	statePath := writeLocalStateFixture(t, `{"profile": {
	  "info_cache": {"Default": {"name": "mine"}},
	  "profiles_order": ["Default"]
	}}`)

	if _, err := (&Brave{Base: cmd.NewMockBaseCommand()}).EnsureProfiles(
		map[string]apps.StateProfileInfo{"Profile 5": {Dir: "Profile 5", Name: "sadmin"}},
	); err != nil {
		t.Fatalf("EnsureProfiles: %v", err)
	}

	got := profilesOrder(t, statePath)
	if len(got) != 2 || got[0] != "Default" || got[1] != "Profile 5" {
		t.Errorf("profiles_order = %v, want [Default Profile 5]", got)
	}
}

// TestEnsureProfilesDoesNotInventAnOrder: Chromium writes profiles_order only
// once a user has arranged their profiles. An absent key means "default
// order", so creating one would invent a preference the user never set.
func TestEnsureProfilesDoesNotInventAnOrder(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	statePath := writeLocalStateFixture(t, `{"profile": {"info_cache": {}}}`)

	if _, err := (&Brave{Base: cmd.NewMockBaseCommand()}).EnsureProfiles(
		map[string]apps.StateProfileInfo{"Profile 5": {Dir: "Profile 5", Name: "sadmin"}},
	); err != nil {
		t.Fatalf("EnsureProfiles: %v", err)
	}

	if got := profilesOrder(t, statePath); got != nil {
		t.Errorf("profiles_order = %v, want it left absent", got)
	}
}
