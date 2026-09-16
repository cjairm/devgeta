package appstate

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
)

// TestRegistryRoundTrips: the sidecar is the only thing carrying a profile's
// name to the new machine, so anything a user can type into a profile name has
// to survive it — quotes and newlines included, which is where a hand-rolled
// line format would lose.
func TestRegistryRoundTrips(t *testing.T) {
	want := map[string]apps.StateProfileInfo{
		"Default":   {Dir: "Default", Name: "Jair - Lever", Avatar: "chrome://theme/IDR_PROFILE_AVATAR_68"},
		"Profile 5": {Dir: "Profile 5", Name: "sadmin", Avatar: ""},
		"Profile 9": {Dir: "Profile 9", Name: "He said \"hi\"\nand left", Avatar: "a"},
	}

	encoded, err := EncodeRegistry(want)
	if err != nil {
		t.Fatalf("EncodeRegistry: %v", err)
	}

	got, err := DecodeRegistry(encoded)
	if err != nil {
		t.Fatalf("DecodeRegistry: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("decoded %d profiles, want %d", len(got), len(want))
	}
	for dir, w := range want {
		g, ok := got[dir]
		if !ok {
			t.Errorf("%q missing after round trip", dir)
			continue
		}
		if g != w {
			t.Errorf("%q = %+v, want %+v", dir, g, w)
		}
	}
}

// TestDecodeRegistryRejectsGarbage: a sidecar that does not parse must be an
// error the import reports, not an empty registry it silently proceeds with —
// proceeding would create every profile unnamed.
func TestDecodeRegistryRejectsGarbage(t *testing.T) {
	if _, err := DecodeRegistry([]byte("not json at all")); err == nil {
		t.Fatal("DecodeRegistry accepted garbage; it must refuse")
	}
}

// TestRegistrySidecarPathSitsBesideTheBundle pins the name both directions
// derive independently, so the writer and the reader cannot drift apart.
func TestRegistrySidecarPathSitsBesideTheBundle(t *testing.T) {
	got := RegistrySidecarPath("/Volumes/SSD/brave-state-2026-09-15.tar.zst")
	want := "/Volumes/SSD/brave-state-2026-09-15.profiles.json"
	if got != want {
		t.Errorf("RegistrySidecarPath = %q, want %q", got, want)
	}
	if strings.Contains(got, ".tar") {
		t.Errorf("sidecar %q still carries the archive extension", got)
	}
}

// fakeRegistrar is a fakePorter that also knows its profiles' display names,
// the way the Brave adapter does.
type fakeRegistrar struct {
	*fakePorter
	registry   map[string]apps.StateProfileInfo
	ensured    map[string]apps.StateProfileInfo
	createBase string
}

func (f *fakeRegistrar) ReadProfileRegistry() (map[string]apps.StateProfileInfo, error) {
	return f.registry, nil
}

func (f *fakeRegistrar) EnsureProfiles(
	want map[string]apps.StateProfileInfo,
) ([]string, error) {
	f.ensured = want
	dirs := make([]string, 0, len(want))
	for dir := range want {
		if f.createBase != "" {
			root := filepath.Join(f.createBase, dir)
			if err := os.MkdirAll(root, 0o755); err != nil {
				return nil, err
			}
			f.roots[dir] = root
		}
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs, nil
}

func (f *fakeRegistrar) RegistryPaths() []string { return nil }

// TestExportWritesTheProfileRegistrySidecar: without this file the new machine
// has no way to learn that "Profile 5" is called "sadmin", because the name
// lives in Local State and Local State never travels (ADR-0046).
func TestExportWritesTheProfileRegistrySidecar(t *testing.T) {
	base := newTestPorter(t, "Default", "Profile 5")
	porter := &fakeRegistrar{
		fakePorter: base,
		registry: map[string]apps.StateProfileInfo{
			"Default":   {Dir: "Default", Name: "Jair - Lever", Avatar: "a68"},
			"Profile 5": {Dir: "Profile 5", Name: "sadmin", Avatar: "a26"},
		},
	}

	plan, err := Prepare(porter, Selection{})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	destDir := t.TempDir()
	result, err := WriteBundle(plan, destDir, "brave-state-2026-09-15", ExportOptions{})
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	sidecar := RegistrySidecarPath(result.BundlePath)
	raw, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("no registry sidecar beside the bundle: %v", err)
	}
	got, err := DecodeRegistry(raw)
	if err != nil {
		t.Fatalf("decoding the sidecar: %v", err)
	}
	if got["Profile 5"].Name != "sadmin" {
		t.Errorf("Profile 5 name in sidecar = %q, want %q", got["Profile 5"].Name, "sadmin")
	}
	if got["Default"].Name != "Jair - Lever" {
		t.Errorf("Default name in sidecar = %q, want %q", got["Default"].Name, "Jair - Lever")
	}
}

// newRegistrarDest is a destination that behaves like Brave: EnsureProfiles
// really creates the directory and the profile then shows up in StateRoots,
// so a test exercises the wiring rather than a recording mock.
func newRegistrarDest(t *testing.T, existing ...string) *fakeRegistrar {
	t.Helper()
	base := filepath.Join(t.TempDir(), "Brave-Browser")
	roots := map[string]string{}
	for _, key := range existing {
		root := filepath.Join(base, key)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		roots[key] = root
	}
	dest := &fakeRegistrar{
		fakePorter: &fakePorter{roots: roots, groups: braveLikeGroups()},
		registry:   map[string]apps.StateProfileInfo{},
	}
	for _, key := range existing {
		dest.registry[key] = apps.StateProfileInfo{Dir: key, Name: "existing " + key}
	}
	dest.createBase = base
	return dest
}

// TestImportCreatesAProfileTheDestinationLacks is the bug this cycle exists to
// fix. The bundle carries "Profile 5"; the new machine has only "Default"; and
// Chromium's UI cannot be made to produce a directory called "Profile 5"
// because it allocates Profile N from a counter. Refusing here is a dead end,
// so the import creates and registers it (ADR-0046).
func TestImportCreatesAProfileTheDestinationLacks(t *testing.T) {
	bundle := exportRegistrarFixture(t, "Default", "Profile 5")
	dest := newRegistrarDest(t, "Default")

	result, err := Import(dest, bundle, Selection{}, ImportOptions{})
	if err != nil {
		t.Fatalf("Import refused a profile it could have created: %v", err)
	}

	root, ok := dest.roots["Profile 5"]
	if !ok {
		t.Fatalf("Profile 5 was not created; roots = %v", dest.roots)
	}
	if _, err := os.Stat(filepath.Join(root, "Bookmarks")); err != nil {
		t.Errorf("Profile 5's state was not restored into the new profile: %v", err)
	}
	if got := dest.ensured["Profile 5"].Name; got != "sadmin" {
		t.Errorf("Profile 5 was registered as %q, want %q from the sidecar", got, "sadmin")
	}
	if !contains(result.Profiles, "Profile 5") {
		t.Errorf("result profiles = %v, want Profile 5 among them", result.Profiles)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// exportRegistrarFixture writes a bundle whose sidecar carries real names.
func exportRegistrarFixture(t *testing.T, profileKeys ...string) string {
	t.Helper()
	names := map[string]string{"Default": "Jair - Lever", "Profile 5": "sadmin", "Profile 1": "Jair - Employ"}
	src := &fakeRegistrar{
		fakePorter: newTestPorter(t, profileKeys...),
		registry:   map[string]apps.StateProfileInfo{},
	}
	for _, key := range profileKeys {
		src.registry[key] = apps.StateProfileInfo{Dir: key, Name: names[key], Avatar: "a"}
	}
	plan, err := Prepare(src, Selection{Groups: []string{"bookmarks"}})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	result, err := WriteBundle(plan, t.TempDir(), "brave-state-2026-09-15", ExportOptions{})
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	return result.BundlePath
}

// TestRefusalDoesNotAdviseCreatingTheProfileByHand: when there is no sidecar
// to name the profiles, the import still has to refuse — but the old advice,
// "create the profile in brave first", cannot be followed. Chromium allocates
// Profile N from a counter, so a user cannot produce "Profile 5" on a fresh
// machine. The refusal has to say what is in the bundle, what is here, and
// give a line that actually runs.
func TestRefusalDoesNotAdviseCreatingTheProfileByHand(t *testing.T) {
	bundle := exportRegistrarFixture(t, "Default", "Profile 5")
	if err := os.Remove(RegistrySidecarPath(bundle)); err != nil {
		t.Fatalf("removing the sidecar: %v", err)
	}
	dest := newRegistrarDest(t, "Default")

	_, err := Import(dest, bundle, Selection{}, ImportOptions{})
	if err == nil {
		t.Fatal("Import succeeded without a sidecar naming Profile 5")
	}
	msg := err.Error()

	if strings.Contains(msg, "create the profile in") {
		t.Errorf("refusal still advises creating the profile by hand:\n%s", msg)
	}
	if !strings.Contains(msg, "--profile Default") {
		t.Errorf("refusal does not offer a runnable --profile line:\n%s", msg)
	}
	if !strings.Contains(msg, "Profile 5") {
		t.Errorf("refusal does not name the profile it cannot create:\n%s", msg)
	}
}

// TestImportReportsWhichProfilesItCreated: creating a profile is a change to
// the browser the user did not explicitly ask for, so the run has to say it
// happened rather than leave it to be discovered on next launch.
func TestImportReportsWhichProfilesItCreated(t *testing.T) {
	bundle := exportRegistrarFixture(t, "Default", "Profile 5")
	dest := newRegistrarDest(t, "Default")

	result, err := Import(dest, bundle, Selection{}, ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if len(result.CreatedProfiles) != 1 || result.CreatedProfiles[0] != "Profile 5" {
		t.Errorf("CreatedProfiles = %v, want [Profile 5]", result.CreatedProfiles)
	}
}
