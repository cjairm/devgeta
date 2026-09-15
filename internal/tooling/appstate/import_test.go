package appstate

import (
	"archive/tar"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/tooling/archive"
	"github.com/klauspost/compress/zstd"
)

// newEmptyTestPorter is a destination machine: the profile directories
// exist (Brave was launched once) but hold none of the state the bundle
// carries.
func newEmptyTestPorter(t *testing.T, profileKeys ...string) *fakePorter {
	t.Helper()
	base := filepath.Join(t.TempDir(), "Brave-Browser")
	roots := map[string]string{}
	for _, key := range profileKeys {
		root := filepath.Join(base, key)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("MkdirAll %s: %v", root, err)
		}
		roots[key] = root
	}
	return &fakePorter{roots: roots, groups: braveLikeGroups()}
}

// exportFixture writes a real bundle from a real source tree — the same
// bytes a user would carry on a drive.
func exportFixture(t *testing.T, sel Selection, profileKeys ...string) (string, *fakePorter) {
	t.Helper()
	source := newTestPorter(t, profileKeys...)
	destDir := t.TempDir()
	result, err := Export(source, destDir, BundlePrefix("brave")+"2026-09-15", sel, ExportOptions{})
	if err != nil {
		t.Fatalf("Export fixture: %v", err)
	}
	return result.BundlePath, source
}

type craftedMember struct {
	Name     string
	Type     byte
	Content  string
	Linkname string
}

// writeCraftedBundle builds a bundle by hand, manifest included, so it
// passes archive.Verify and the import's own member gate is what has to
// reject it. This is the attacker's-eye view the allowlist cannot cover:
// the allowlist constrains what devgeta packed, not what is in front of us.
func writeCraftedBundle(t *testing.T, dir, name string, members []craftedMember) string {
	t.Helper()
	bundlePath := filepath.Join(dir, name+BundleExt)

	f, err := os.Create(bundlePath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	enc, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	tw := tar.NewWriter(enc)

	var manifest []archive.ManifestEntry
	for _, m := range members {
		hdr := &tar.Header{
			Name:     m.Name,
			Typeflag: m.Type,
			Mode:     0o644,
			Linkname: m.Linkname,
		}
		if m.Type == tar.TypeReg {
			hdr.Size = int64(len(m.Content))
		}
		if m.Type == tar.TypeDir {
			hdr.Mode = 0o755
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader %s: %v", m.Name, err)
		}
		if m.Type == tar.TypeReg {
			if _, err := tw.Write([]byte(m.Content)); err != nil {
				t.Fatalf("Write %s: %v", m.Name, err)
			}
			manifest = append(manifest, archive.ManifestEntry{
				Hash: fmt.Sprintf("%x", sha256.Sum256([]byte(m.Content))),
				Path: m.Name,
			})
		}
	}
	for _, closeErr := range []error{tw.Close(), enc.Close(), f.Close()} {
		if closeErr != nil {
			t.Fatalf("closing the crafted bundle: %v", closeErr)
		}
	}

	mf, err := os.Create(filepath.Join(dir, name+".sha256"))
	if err != nil {
		t.Fatalf("Create manifest: %v", err)
	}
	if err := archive.WriteManifest(mf, manifest); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if err := mf.Close(); err != nil {
		t.Fatalf("closing the manifest: %v", err)
	}
	return bundlePath
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	return string(content)
}

// assertNothingTouched is the assertion behind every pre-write refusal: not
// just "no state written" but "no backup taken either", since a backup is
// itself a change to the profile directory.
func assertNothingTouched(t *testing.T, porter *fakePorter) {
	t.Helper()
	for key, root := range porter.roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatalf("ReadDir %s: %v", root, err)
		}
		if len(entries) != 0 {
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			t.Errorf("profile %q was touched: %v", key, names)
		}
	}
}

func TestImportRestoresEveryGroupTheBundleCarries(t *testing.T) {
	// The export chose to carry history, which is default-OFF. A group's
	// export default has no say on the way in: the export already decided.
	bundle, source := exportFixture(
		t,
		Selection{Groups: []string{"bookmarks", "history"}},
		"Default",
	)
	dest := newEmptyTestPorter(t, "Default")

	result, err := Import(dest, bundle, Selection{}, ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	root := dest.roots["Default"]
	if got, want := readTestFile(t, filepath.Join(root, "Bookmarks")),
		readTestFile(t, filepath.Join(source.roots["Default"], "Bookmarks")); got != want {
		t.Errorf("Bookmarks = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "History")); err != nil {
		t.Errorf("the opt-in group the bundle carries was not restored: %v", err)
	}
	if got := result.Groups; len(got) != 2 {
		t.Errorf("restored groups = %v, want bookmarks and history", got)
	}
	if result.Profiles[0] != "Default" {
		t.Errorf("restored profiles = %v, want [Default]", result.Profiles)
	}
}

// TestImportIntoAFreshProfileLeavesNoMarkers is the common case — a brand
// new machine — and nothing there was replaced, so there is nothing to
// recover. The absent markers exist for the rollback, which by this point
// cannot happen; leaving them would scatter files a live app does not
// expect through its own profile directory, and would make the command
// report backups that back up nothing.
func TestImportIntoAFreshProfileLeavesNoMarkers(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks", "tabs"}}, "Default")
	dest := newEmptyTestPorter(t, "Default")

	result, err := Import(dest, bundle, Selection{}, ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	markers, err := filepath.Glob(filepath.Join(dest.roots["Default"], "*"+AbsentSuffix))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(markers) > 0 {
		t.Errorf("a fresh import left absent markers in the profile: %v", markers)
	}
	if len(result.Backups) != 0 {
		t.Errorf("Backups = %v, want none: nothing was replaced", result.Backups)
	}
}

// TestImportReportsOnlyRealBackups: a run that replaces one path and adds
// another reports the one it replaced, because that is the only one with
// anything to put back.
func TestImportReportsOnlyRealBackups(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks", "tabs"}}, "Default")
	dest := newEmptyTestPorter(t, "Default")
	writeTestFile(t, filepath.Join(dest.roots["Default"], "Bookmarks"), "the old bookmarks")

	result, err := Import(dest, bundle, Selection{}, ImportOptions{Force: true})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if len(result.Backups) != 1 ||
		filepath.Base(result.Backups[0]) != "Bookmarks" {
		t.Errorf("Backups = %v, want just the replaced Bookmarks", result.Backups)
	}
	if content := readTestFile(
		t,
		filepath.Join(dest.roots["Default"], "Bookmarks"+BackupSuffix),
	); content != "the old bookmarks" {
		t.Errorf("backup holds %q, want the replaced content", content)
	}
}

func TestImportRestoresADirectoryGroup(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"tabs"}}, "Default")
	dest := newEmptyTestPorter(t, "Default")

	if _, err := Import(dest, bundle, Selection{}, ImportOptions{}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	for _, name := range []string{"Session_1", "Tabs_1"} {
		if _, err := os.Stat(filepath.Join(dest.roots["Default"], "Sessions", name)); err != nil {
			t.Errorf("Sessions/%s was not restored: %v", name, err)
		}
	}
}

func TestImportRestoresEveryProfileInTheBundle(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks"}}, "Default", "Profile 1")
	dest := newEmptyTestPorter(t, "Default", "Profile 1")

	if _, err := Import(dest, bundle, Selection{}, ImportOptions{}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	for _, key := range []string{"Default", "Profile 1"} {
		content := readTestFile(t, filepath.Join(dest.roots[key], "Bookmarks"))
		if content != "bookmarks of "+key {
			t.Errorf("%s/Bookmarks = %q, want the bookmarks of that profile", key, content)
		}
	}
}

// TestImportNarrowsWithGroup: --group restores only what it names and
// refuses nothing — the groups the user chose not to restore are not an
// error, they are the point of the flag.
func TestImportNarrowsWithGroup(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks", "history"}}, "Default")
	dest := newEmptyTestPorter(t, "Default")

	if _, err := Import(
		dest,
		bundle,
		Selection{Groups: []string{"bookmarks"}},
		ImportOptions{},
	); err != nil {
		t.Fatalf("Import: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest.roots["Default"], "Bookmarks")); err != nil {
		t.Errorf("Bookmarks was not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest.roots["Default"], "History")); !os.IsNotExist(err) {
		t.Errorf("History was restored although --group did not name it")
	}
}

func TestImportNarrowsWithProfile(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks"}}, "Default", "Profile 1")
	dest := newEmptyTestPorter(t, "Default", "Profile 1")

	if _, err := Import(
		dest,
		bundle,
		Selection{Profiles: []string{"Profile 1"}},
		ImportOptions{},
	); err != nil {
		t.Fatalf("Import: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dest.roots["Profile 1"], "Bookmarks")); err != nil {
		t.Errorf("Profile 1's Bookmarks was not restored: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest.roots["Default"], "Bookmarks")); !os.IsNotExist(err) {
		t.Errorf("Default was restored although --profile did not name it")
	}
}

func TestImportRefusesWithoutForceWhenStateExists(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks"}}, "Default")
	dest := newTestPorter(t, "Default") // already has state
	before := readTestFile(t, filepath.Join(dest.roots["Default"], "Bookmarks"))

	_, err := Import(dest, bundle, Selection{Groups: []string{"bookmarks"}}, ImportOptions{})
	if err == nil {
		t.Fatal("expected a refusal without --force, got nil")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error %q does not name the flag that would allow it", err)
	}
	if after := readTestFile(
		t,
		filepath.Join(dest.roots["Default"], "Bookmarks"),
	); after != before {
		t.Error("the refused import changed the profile anyway")
	}
	if files, _ := filepath.Glob(
		filepath.Join(dest.roots["Default"], "*"+BackupSuffix),
	); len(
		files,
	) > 0 {
		t.Errorf("the refused import left backups behind: %v", files)
	}
}

func TestImportReplacesAndBacksUpWithForce(t *testing.T) {
	bundle, source := exportFixture(t, Selection{Groups: []string{"bookmarks"}}, "Default")
	dest := newTestPorter(t, "Default")
	before := readTestFile(t, filepath.Join(dest.roots["Default"], "Bookmarks"))

	result, err := Import(
		dest,
		bundle,
		Selection{Groups: []string{"bookmarks"}},
		ImportOptions{Force: true},
	)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	got := readTestFile(t, filepath.Join(dest.roots["Default"], "Bookmarks"))
	want := readTestFile(t, filepath.Join(source.roots["Default"], "Bookmarks"))
	if got != want {
		t.Errorf("Bookmarks = %q, want the bundle's %q", got, want)
	}
	// ADR-0045's recoverability promise: the backups stay, and the command
	// can tell the user where they are.
	backup := filepath.Join(dest.roots["Default"], "Bookmarks"+BackupSuffix)
	if content := readTestFile(t, backup); content != before {
		t.Errorf("backup holds %q, want the replaced %q", content, before)
	}
	if len(result.Backups) == 0 {
		t.Error("the result does not report the backups it left behind")
	}
}

func TestImportRefusesWhileTheAppIsRunning(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks"}}, "Default")
	dest := newEmptyTestPorter(t, "Default")
	dest.running = true

	_, err := Import(dest, bundle, Selection{}, ImportOptions{})
	if err == nil {
		t.Fatal("expected a refusal while the app is running, got nil")
	}
	assertNothingTouched(t, dest)
}

// TestImportRefusesABundleNamedForAnotherApp is the wrong-app guard. Every
// Chromium browser uses the same profile file names, so a Chrome bundle
// passes every path rule Brave's import applies — the name is the only thing
// between it and the live profile. The fixture here is deliberately not even
// a valid archive and has no manifest: if the refusal names the two apps
// rather than complaining about the manifest, the check ran before the hash.
func TestImportRefusesABundleNamedForAnotherApp(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "chrome-state-2026-09-15"+BundleExt)
	writeTestFile(t, bundle, "not even an archive")
	dest := newEmptyTestPorter(t, "Default")

	_, err := Import(dest, bundle, Selection{}, ImportOptions{Force: true})
	if err == nil {
		t.Fatal("expected a refusal for a bundle named for another app, got nil")
	}
	for _, want := range []string{"chrome-state-2026-09-15", "brave-state-"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	assertNothingTouched(t, dest)
}

func TestImportRefusesAnAlteredBundle(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks"}}, "Default")
	corrupt(t, bundle)
	dest := newEmptyTestPorter(t, "Default")

	_, err := Import(dest, bundle, Selection{}, ImportOptions{Force: true})
	if err == nil {
		t.Fatal("expected a refusal for an altered bundle, got nil")
	}
	assertNothingTouched(t, dest)
}

func TestImportRefusesABundleWithNoManifest(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks"}}, "Default")
	manifest := strings.TrimSuffix(bundle, BundleExt) + ".sha256"
	if err := os.Remove(manifest); err != nil {
		t.Fatalf("Remove manifest: %v", err)
	}
	dest := newEmptyTestPorter(t, "Default")

	_, err := Import(dest, bundle, Selection{}, ImportOptions{Force: true})
	if err == nil {
		t.Fatal("expected a refusal with no manifest beside the bundle, got nil")
	}
	if !strings.Contains(err.Error(), filepath.Base(manifest)) {
		t.Errorf("error %q does not name the manifest the user has to bring along", err)
	}
	assertNothingTouched(t, dest)
}

// TestImportRefusesAReadOnlyBundleDirectory: archive.Verify writes the
// bundle's own checksum beside it on success, so an import straight off a
// write-protected drive fails for a reason that has nothing to do with the
// bundle. Refuse with that reason rather than let a write error read as
// corruption.
func TestImportRefusesAReadOnlyBundleDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only directory is not read-only")
	}
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks"}}, "Default")
	dir := filepath.Dir(bundle)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	dest := newEmptyTestPorter(t, "Default")

	_, err := Import(dest, bundle, Selection{}, ImportOptions{Force: true})
	if err == nil {
		t.Fatal("expected a refusal for a read-only bundle directory, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "writ") {
		t.Errorf("error %q does not explain that the directory must be writable", err)
	}
	assertNothingTouched(t, dest)
}

// TestImportRefusesAProfileTheDestinationLacks: Brave's profile registry is
// in Local State, which is denied, so a profile directory Brave was never
// told about is one it never shows. Creating it would be a directory nobody
// ever sees.
func TestImportRefusesAProfileTheDestinationLacks(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks"}}, "Default", "Profile 1")
	dest := newEmptyTestPorter(t, "Default")

	_, err := Import(dest, bundle, Selection{}, ImportOptions{Force: true})
	if err == nil {
		t.Fatal("expected a refusal for a profile the destination lacks, got nil")
	}
	if !strings.Contains(err.Error(), "Profile 1") {
		t.Errorf("error %q does not name the missing profile", err)
	}
	assertNothingTouched(t, dest)
}

func TestImportRefusesAnUnknownGroupName(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks"}}, "Default")
	dest := newEmptyTestPorter(t, "Default")

	_, err := Import(dest, bundle, Selection{Groups: []string{"nope"}}, ImportOptions{Force: true})
	if err == nil {
		t.Fatal("expected a refusal for an unknown group, got nil")
	}
	for _, want := range []string{"nope", "bookmarks"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	assertNothingTouched(t, dest)
}

// TestImportRefusesAGroupAbsentFromTheBundle: a real group the bundle does
// not carry is a refusal naming it, rather than an import that quietly
// restores less than it was asked for.
func TestImportRefusesAGroupAbsentFromTheBundle(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks"}}, "Default")
	dest := newEmptyTestPorter(t, "Default")

	_, err := Import(
		dest,
		bundle,
		Selection{Groups: []string{"history"}},
		ImportOptions{Force: true},
	)
	if err == nil {
		t.Fatal("expected a refusal for a group the bundle does not carry, got nil")
	}
	if !strings.Contains(err.Error(), "history") {
		t.Errorf("error %q does not name the missing group", err)
	}
	assertNothingTouched(t, dest)
}

// TestImportRefusesACraftedBundle covers the member gate: the allowlist
// constrains what devgeta packed, not what the tar in front of us contains,
// so every member is checked and one bad member rejects the whole bundle.
func TestImportRefusesACraftedBundle(t *testing.T) {
	tests := []struct {
		name    string
		members []craftedMember
	}{
		{
			// A link written early redirects a later, perfectly relative
			// member outside the root — the escape "reject .. and absolute
			// paths" does not catch.
			"a symlink member",
			[]craftedMember{
				{Name: "Default/Bookmarks", Type: tar.TypeReg, Content: "ok"},
				{Name: "Default/Sessions", Type: tar.TypeSymlink, Linkname: "/etc"},
			},
		},
		{
			"a hard-link member",
			[]craftedMember{
				{Name: "Default/Bookmarks", Type: tar.TypeReg, Content: "ok"},
				{Name: "Default/Preferences", Type: tar.TypeLink, Linkname: "Default/Bookmarks"},
			},
		},
		{
			"a .. member",
			[]craftedMember{
				{Name: "Default/../../escape", Type: tar.TypeReg, Content: "ok"},
			},
		},
		{
			"an absolute member",
			[]craftedMember{
				{Name: "/etc/passwd", Type: tar.TypeReg, Content: "ok"},
			},
		},
		{
			// Clean, relative, under the right profile — and a credential
			// store. Without the denylist on this side, ADR-0045's promise
			// only ever held for export.
			"a denied member",
			[]craftedMember{
				{Name: "Default/Bookmarks", Type: tar.TypeReg, Content: "ok"},
				{Name: "Default/Login Data", Type: tar.TypeReg, Content: "secrets"},
			},
		},
		{
			"a member outside every group",
			[]craftedMember{
				{Name: "Default/Local Extension Settings/x", Type: tar.TypeReg, Content: "ok"},
			},
		},
		{
			"a member with no path under its profile",
			[]craftedMember{
				{Name: "Default", Type: tar.TypeDir},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			bundle := writeCraftedBundle(t, dir, BundlePrefix("brave")+"2026-09-15", tt.members)
			dest := newEmptyTestPorter(t, "Default")

			_, err := Import(dest, bundle, Selection{}, ImportOptions{Force: true})
			if err == nil {
				t.Fatal("expected the bundle to be refused, got nil")
			}
			assertNothingTouched(t, dest)
		})
	}
}

// TestImportRollsBackWhenALaterWriteFails is CLAUDE.md §4's complete-or-
// fully-rolled-back rule: a profile is many files, and a failure on the
// fourth of seven writes must not leave a half-imported profile.
func TestImportRollsBackWhenALaterWriteFails(t *testing.T) {
	bundle, _ := exportFixture(
		t,
		Selection{Groups: []string{"bookmarks", "tabs"}},
		"Default",
	)
	dest := newTestPorter(t, "Default")
	root := dest.roots["Default"]
	before := map[string]string{
		"Bookmarks":          readTestFile(t, filepath.Join(root, "Bookmarks")),
		"Sessions/Session_1": readTestFile(t, filepath.Join(root, "Sessions", "Session_1")),
	}

	original := writeMemberFile
	t.Cleanup(func() { writeMemberFile = original })
	writeMemberFile = func(dest string, mode os.FileMode, r io.Reader) error {
		if strings.HasSuffix(dest, "Tabs_1") {
			return errors.New("disk full")
		}
		return original(dest, mode, r)
	}

	_, err := Import(
		dest,
		bundle,
		Selection{Groups: []string{"bookmarks", "tabs"}},
		ImportOptions{Force: true},
	)
	if err == nil {
		t.Fatal("expected the failed write to fail the import, got nil")
	}

	for rel, want := range before {
		got := readTestFile(t, filepath.Join(root, filepath.FromSlash(rel)))
		if got != want {
			t.Errorf("%s = %q after the rollback, want the original %q", rel, got, want)
		}
	}
	leftover, _ := filepath.Glob(filepath.Join(root, "*"+BackupSuffix))
	if len(leftover) > 0 {
		t.Errorf("the rollback left its own backups behind: %v", leftover)
	}
}

// TestImportRollsBackAnAddedPath: a path the profile did not have has to be
// deleted by the rollback, which a rename-aside cannot express — hence the
// absent marker.
func TestImportRollsBackAnAddedPath(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"tabs"}}, "Default")
	dest := newEmptyTestPorter(t, "Default")

	original := writeMemberFile
	t.Cleanup(func() { writeMemberFile = original })
	writeMemberFile = func(dest string, mode os.FileMode, r io.Reader) error {
		if strings.HasSuffix(dest, "Tabs_1") {
			return errors.New("disk full")
		}
		return original(dest, mode, r)
	}

	if _, err := Import(dest, bundle, Selection{}, ImportOptions{Force: true}); err == nil {
		t.Fatal("expected the failed write to fail the import, got nil")
	}

	assertNothingTouched(t, dest)
}

func TestImportReportsVerifyProgress(t *testing.T) {
	bundle, _ := exportFixture(t, Selection{Groups: []string{"bookmarks"}}, "Default")
	dest := newEmptyTestPorter(t, "Default")

	var verified int64
	_, err := Import(dest, bundle, Selection{}, ImportOptions{
		OnVerifyProgress: func(n int64) { verified += n },
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if verified == 0 {
		t.Error("the verify meter was never advanced")
	}
}

// corrupt flips a byte in the middle of the bundle, the way a bad transfer
// would.
func corrupt(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(content) < 8 {
		t.Fatalf("bundle is too small to corrupt: %d bytes", len(content))
	}
	content[len(content)/2] ^= 0xff
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
