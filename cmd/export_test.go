package cmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/tooling/appstate"
)

// stubPorter stands in for a real app. The lookup is injectable because the
// real Brave adapter's running check shells out to pgrep, and a cmd test
// must never run a real command.
type stubPorter struct {
	name    string
	roots   map[string]string
	groups  []apps.StateGroup
	running bool
}

func (s *stubPorter) Name() string                           { return s.name }
func (s *stubPorter) StateRoots() (map[string]string, error) { return s.roots, nil }
func (s *stubPorter) StateGroups() []apps.StateGroup         { return s.groups }
func (s *stubPorter) IsRunning() (bool, error)               { return s.running, nil }

// newStubPorter builds a two-group profile tree: one single-file group and
// one directory group, plus a default-off one.
func newStubPorter(t *testing.T, profileKeys ...string) *stubPorter {
	t.Helper()
	base := filepath.Join(t.TempDir(), "Brave-Browser")
	roots := map[string]string{}
	for _, key := range profileKeys {
		root := filepath.Join(base, key)
		mustWrite(t, filepath.Join(root, "Bookmarks"), "bookmarks")
		mustWrite(t, filepath.Join(root, "Sessions", "Session_1"), "tabs")
		mustWrite(t, filepath.Join(root, "History"), "browsing")
		roots[key] = root
	}
	return &stubPorter{
		name:  "brave",
		roots: roots,
		groups: []apps.StateGroup{
			{Name: "bookmarks", Paths: []string{"Bookmarks"}, Default: true, Why: "plain JSON"},
			{Name: "tabs", Paths: []string{"Sessions"}, Default: true, Why: "the open tabs"},
			{Name: "history", Paths: []string{"History"}, Default: false, Why: "browsing record"},
		},
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// resetStateFlags restores every flag and seam `dg export` / `dg import`
// read, and points their meters and report at buffers the test owns.
func resetStateFlags(t *testing.T, porter appstate.Porter) *strings.Builder {
	t.Helper()
	orig := struct {
		dryRun          bool
		exportGroups    []string
		exportProfiles  []string
		importGroups    []string
		importProfiles  []string
		force           bool
		porterFor       func(string) (appstate.Porter, error)
		now             func() time.Time
		progressOut     io.Writer
		archiveProgress io.Writer
	}{
		exportDryRunFlag,
		exportGroupFlag,
		exportProfileFlag,
		importGroupFlag,
		importProfileFlag,
		importForceFlag,
		statePorterFor,
		archiveNow,
		stateReportOut,
		archiveProgressOut,
	}
	t.Cleanup(func() {
		exportDryRunFlag = orig.dryRun
		exportGroupFlag = orig.exportGroups
		exportProfileFlag = orig.exportProfiles
		importGroupFlag = orig.importGroups
		importProfileFlag = orig.importProfiles
		importForceFlag = orig.force
		statePorterFor = orig.porterFor
		archiveNow = orig.now
		stateReportOut = orig.progressOut
		archiveProgressOut = orig.archiveProgress
	})

	exportDryRunFlag = false
	exportGroupFlag = nil
	exportProfileFlag = nil
	importGroupFlag = nil
	importProfileFlag = nil
	importForceFlag = false
	archiveNow = func() time.Time { return time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC) }
	archiveProgressOut = io.Discard

	var report strings.Builder
	stateReportOut = &report
	if porter != nil {
		statePorterFor = func(string) (appstate.Porter, error) { return porter, nil }
	}
	return &report
}

func TestRunExportWritesTheDatedBundle(t *testing.T) {
	porter := newStubPorter(t, "Default")
	resetStateFlags(t, porter)
	destDir := t.TempDir()

	if err := runExport(exportCmd, []string{"brave", destDir}); err != nil {
		t.Fatalf("runExport: %v", err)
	}

	// The name is deterministic and fixed through the same archiveNow seam
	// `dg archive` uses, so this is an assertion and not a snapshot.
	bundle := filepath.Join(destDir, "brave-state-2026-09-15"+appstate.BundleExt)
	if _, err := os.Stat(bundle); err != nil {
		t.Errorf("expected %s: %v", bundle, err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "brave-state-2026-09-15.sha256")); err != nil {
		t.Errorf("expected the manifest beside the bundle: %v", err)
	}
}

// TestRunExportRefusesASecondRunIntoTheSameDirectory: archive.Write renames
// its .partial files into place unconditionally, so without this check a
// second export on the same day would replace the first without a word.
func TestRunExportRefusesASecondRunIntoTheSameDirectory(t *testing.T) {
	porter := newStubPorter(t, "Default")
	report := resetStateFlags(t, porter)
	destDir := t.TempDir()

	if err := runExport(exportCmd, []string{"brave", destDir}); err != nil {
		t.Fatalf("first runExport: %v", err)
	}
	bundle := filepath.Join(destDir, "brave-state-2026-09-15"+appstate.BundleExt)
	before, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	report.Reset()
	err = runExport(exportCmd, []string{"brave", destDir})
	if err == nil {
		t.Fatal("expected the second export to refuse, got nil")
	}
	if !strings.Contains(err.Error(), "brave-state-2026-09-15") {
		t.Errorf("error %q does not name the files already there", err)
	}
	// The refusal costs nothing, so it comes before the profile is walked
	// and before a screen of report buries the reason.
	if report.String() != "" {
		t.Errorf("the refusal printed a report first:\n%s", report.String())
	}

	after, err := os.ReadFile(bundle)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(before) != string(after) {
		t.Error("the refused export replaced the first bundle anyway")
	}
}

func TestRunExportDryRunWritesNothingAndReportsEveryGroup(t *testing.T) {
	porter := newStubPorter(t, "Default")
	report := resetStateFlags(t, porter)
	exportDryRunFlag = true
	destDir := t.TempDir()

	if err := runExport(exportCmd, []string{"brave", destDir}); err != nil {
		t.Fatalf("runExport --dry-run: %v", err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("--dry-run wrote %d file(s)", len(entries))
	}

	out := report.String()
	for _, want := range []string{"Default", "bookmarks", "tabs", "history", "Sessions"} {
		if !strings.Contains(out, want) {
			t.Errorf("the dry-run report does not mention %q:\n%s", want, out)
		}
	}
	// The reason is what makes a default a decision the user can review.
	if !strings.Contains(out, "browsing record") {
		t.Errorf("the dry-run report omits each group's reason:\n%s", out)
	}
}

// TestRunExportDryRunNeedsNoDestination: there is nothing to write, so
// asking for a drive to not write to is friction.
func TestRunExportDryRunNeedsNoDestination(t *testing.T) {
	porter := newStubPorter(t, "Default")
	resetStateFlags(t, porter)
	exportDryRunFlag = true

	if err := runExport(exportCmd, []string{"brave"}); err != nil {
		t.Fatalf("runExport --dry-run with no destination: %v", err)
	}
}

func TestRunExportNeedsADestinationForARealRun(t *testing.T) {
	porter := newStubPorter(t, "Default")
	resetStateFlags(t, porter)

	err := runExport(exportCmd, []string{"brave"})
	if err == nil {
		t.Fatal("expected an error with no destination directory, got nil")
	}
}

func TestRunExportRefusesANonDirectoryDestination(t *testing.T) {
	porter := newStubPorter(t, "Default")
	resetStateFlags(t, porter)
	dest := filepath.Join(t.TempDir(), "file")
	mustWrite(t, dest, "x")

	if err := runExport(exportCmd, []string{"brave", dest}); err == nil {
		t.Fatal("expected an error for a destination that is not a directory, got nil")
	}
}

func TestRunExportPassesTheGroupAndProfileFlagsThrough(t *testing.T) {
	porter := newStubPorter(t, "Default", "Profile 1")
	resetStateFlags(t, porter)
	exportGroupFlag = []string{"history"}
	exportProfileFlag = []string{"Profile 1"}
	destDir := t.TempDir()

	if err := runExport(exportCmd, []string{"brave", destDir}); err != nil {
		t.Fatalf("runExport: %v", err)
	}

	manifest, err := os.ReadFile(filepath.Join(destDir, "brave-state-2026-09-15.sha256"))
	if err != nil {
		t.Fatalf("ReadFile manifest: %v", err)
	}
	got := string(manifest)
	if !strings.Contains(got, "Profile 1/History") {
		t.Errorf("manifest does not list the selected group and profile:\n%s", got)
	}
	if strings.Contains(got, "Default/") || strings.Contains(got, "Bookmarks") {
		t.Errorf("manifest carries what the flags excluded:\n%s", got)
	}
}

func TestRunExportRefusesWhileTheAppIsRunning(t *testing.T) {
	porter := newStubPorter(t, "Default")
	porter.running = true
	resetStateFlags(t, porter)
	destDir := t.TempDir()

	err := runExport(exportCmd, []string{"brave", destDir})
	if err == nil {
		t.Fatal("expected a refusal while the app is running, got nil")
	}
	entries, _ := os.ReadDir(destDir)
	if len(entries) != 0 {
		t.Errorf("the refused export wrote %d file(s)", len(entries))
	}
}

// TestRunExportRefusesWhileRunningEvenOnADryRun: --dry-run reads the
// profile, and a report of a live profile is a report of something that is
// changing as it is read.
func TestRunExportRefusesWhileTheAppIsRunningOnADryRun(t *testing.T) {
	porter := newStubPorter(t, "Default")
	porter.running = true
	resetStateFlags(t, porter)
	exportDryRunFlag = true

	if err := runExport(exportCmd, []string{"brave"}); err == nil {
		t.Fatal("expected --dry-run to refuse while the app is running, got nil")
	}
}

// TestStatePorterForAnAppWithoutAnAdapter: an app with no portable state
// says so and lists the ones that have it, rather than "unknown app".
func TestStatePorterForAnAppWithoutAnAdapter(t *testing.T) {
	_, err := statePorterFor("git")
	if err == nil {
		t.Fatal("expected an error for an app with no state adapter, got nil")
	}
	for _, want := range []string{"git", "brave"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestStatePorterForAnUnknownApp(t *testing.T) {
	if _, err := statePorterFor("nosuchapp"); err == nil {
		t.Fatal("expected an error for an unknown app, got nil")
	}
}

// TestStatePorterForBrave: the real registry lookup resolves brave to its
// adapter. No method is called on it, so nothing shells out.
func TestStatePorterForBrave(t *testing.T) {
	porter, err := statePorterFor("brave")
	if err != nil {
		t.Fatalf("statePorterFor(brave): %v", err)
	}
	if porter.Name() != "brave" {
		t.Errorf("Name() = %q, want brave", porter.Name())
	}
}

func TestRunExportReportsALookupFailure(t *testing.T) {
	resetStateFlags(t, nil)
	statePorterFor = func(string) (appstate.Porter, error) {
		return nil, errors.New("no such app")
	}

	if err := runExport(exportCmd, []string{"nope", t.TempDir()}); err == nil {
		t.Fatal("expected the lookup failure to fail the command, got nil")
	}
}
