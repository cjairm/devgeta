package agentpack

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/pkg/constants"
)

// fakePi models `pi install/remove` and the settings.json package list the
// shared reader returns: sources are absolute, as that reader resolves them.
type fakePi struct {
	version    string
	versionErr error
	installed  bool
	sources    []string
	calls      [][]string
	installErr error
	// failButRecord makes `pi install` fail after it already wrote the source,
	// as a package manager can.
	failButRecord bool
	removeErr     error
	readErr       error
}

func (f *fakePi) IsInstalled() bool { return f.installed }
func (f *fakePi) PackageSources() ([]string, error) {
	return append([]string(nil), f.sources...), f.readErr
}
func (f *fakePi) ExecuteCommand(args ...string) error {
	f.calls = append(f.calls, args)
	switch args[0] {
	case "install":
		if f.installErr != nil {
			if f.failButRecord {
				f.sources = append(f.sources, args[1])
			}
			return f.installErr
		}
		f.sources = append(f.sources, args[1])
	case "remove":
		if f.removeErr != nil {
			return f.removeErr
		}
		var kept []string
		for _, s := range f.sources {
			if s != args[1] {
				kept = append(kept, s)
			}
		}
		f.sources = kept
	}
	return nil
}

func (f *fakePi) ExecuteCommandOutput(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	return f.version, f.versionErr
}

func (f *fakePi) called(verb string) int {
	n := 0
	for _, c := range f.calls {
		if c[0] == verb {
			n++
		}
	}
	return n
}

func newPiInstaller(t *testing.T) (*PiInstaller, *fakePi, string) {
	t.Helper()
	f := &fakePi{installed: true}
	root := t.TempDir()
	return &PiInstaller{App: f, PacksRoot: root}, f, root
}

func piTree(t *testing.T, root, name string, withPackageJSON bool) Tree {
	t.Helper()
	dir := filepath.Join(root, name)
	if withPackageJSON {
		writeFile(t, filepath.Join(dir, "package.json"), `{"name":"`+name+`","main":"index.js","pi":{}}`, 0o644)
	} else {
		writeFile(t, filepath.Join(dir, "README"), "x", 0o644)
	}
	return Tree{Pack: Manifest{Name: name}, Dir: dir}
}

func TestPiInstaller_NameAvailabilityAndNothingSkipped(t *testing.T) {
	in, f, root := newPiInstaller(t)
	if in.Name() != "pi" {
		t.Errorf("name %q", in.Name())
	}
	if !in.Available() {
		t.Error("an installed pi is available")
	}
	f.installed = false
	if in.Available() {
		t.Error("a missing pi is not available")
	}
	if got := in.Skipped(piTree(t, root, "superpowers", true)); len(got) != 0 {
		t.Errorf("a Pi package loads the pack's extension and skills, got %v", got)
	}
}

func TestPiInstaller_InstallRunsPiInstallOnTheTreeAndRecordsIt(t *testing.T) {
	in, f, root := newPiInstaller(t)
	tree := piTree(t, root, "superpowers", true)

	rec, err := in.Install(tree)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.PiPackage != tree.Dir {
		t.Errorf("record %+v, want package %s", rec, tree.Dir)
	}
	if !reflect.DeepEqual(f.calls, [][]string{{"install", tree.Dir}}) {
		t.Errorf("expected a single `pi install <tree>` with the absolute path, got %v", f.calls)
	}
}

func TestPiInstaller_AnAlreadyInstalledTreeIsNotInstalledAgain(t *testing.T) {
	in, f, root := newPiInstaller(t)
	tree := piTree(t, root, "superpowers", true)
	f.sources = []string{tree.Dir}

	rec, err := in.Install(tree)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.PiPackage != tree.Dir || f.called("install") != 0 {
		t.Errorf("a package that is already declared is referenced in place; got %+v, calls %v", rec, f.calls)
	}
}

func TestPiInstaller_InstallRefusesATreeWithoutAPackageJSON(t *testing.T) {
	in, f, root := newPiInstaller(t)

	if _, err := in.Install(piTree(t, root, "superpowers", false)); err == nil {
		t.Fatal("expected an error: a Pi package needs a package.json")
	}
	if len(f.calls) != 0 {
		t.Errorf("no pi call: %v", f.calls)
	}
}

func TestPiInstaller_AFailedInstallLeavesNothingBehind(t *testing.T) {
	t.Run("pi install fails cleanly", func(t *testing.T) {
		in, f, root := newPiInstaller(t)
		f.installErr = errors.New("npm exploded")

		_, err := in.Install(piTree(t, root, "superpowers", true))
		if err == nil || !strings.Contains(err.Error(), "npm exploded") || !strings.Contains(err.Error(), "superpowers") {
			t.Fatalf("expected the cause and the pack in the error, got %v", err)
		}
		if f.called("remove") != 0 {
			t.Errorf("nothing was recorded, so nothing to remove: %v", f.calls)
		}
	})
	t.Run("pi install fails after recording the package", func(t *testing.T) {
		in, f, root := newPiInstaller(t)
		f.installErr, f.failButRecord = errors.New("late failure"), true
		tree := piTree(t, root, "superpowers", true)

		if _, err := in.Install(tree); err == nil {
			t.Fatal("expected an error")
		}
		if len(f.sources) != 0 {
			t.Errorf("the half-recorded package must be removed, sources %v", f.sources)
		}
	})
	t.Run("pi succeeds but never recorded the package", func(t *testing.T) {
		in, f, root := newPiInstaller(t)
		silent := &silentPi{fakePi: f}
		in.App = silent

		_, err := in.Install(piTree(t, root, "superpowers", true))
		if err == nil || !strings.Contains(err.Error(), "pi list") {
			t.Fatalf("expected an error pointing at `pi list`, got %v", err)
		}
	})
}

// silentPi accepts `pi install` and records nothing.
type silentPi struct{ *fakePi }

func (s *silentPi) ExecuteCommand(args ...string) error {
	s.fakePi.calls = append(s.fakePi.calls, args)
	return nil
}

func TestPiInstaller_UninstallRemovesOnlyTheRecordedPackage(t *testing.T) {
	in, f, root := newPiInstaller(t)
	ours := filepath.Join(root, "superpowers")
	f.sources = []string{"npm:pi-lens@1.2.3", ours, filepath.Join(root, "ponytail")}

	if err := in.Uninstall(Record{PiPackage: ours}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(f.calls, [][]string{{"remove", ours}}) {
		t.Errorf("expected `pi remove <absolute path>`, got %v", f.calls)
	}
	want := []string{"npm:pi-lens@1.2.3", filepath.Join(root, "ponytail")}
	if !reflect.DeepEqual(f.sources, want) {
		t.Errorf("only the recorded package may go, got %v", f.sources)
	}
}

func TestPiInstaller_UninstallOfAPackageThatIsAlreadyGoneIsFine(t *testing.T) {
	in, f, root := newPiInstaller(t)

	if err := in.Uninstall(Record{PiPackage: filepath.Join(root, "superpowers")}); err != nil {
		t.Fatalf("a package the user already removed is not an error: %v", err)
	}
	if f.called("remove") != 0 {
		t.Errorf("nothing to remove: %v", f.calls)
	}
	if err := in.Uninstall(Record{}); err != nil || len(f.calls) != 0 {
		t.Errorf("a record with no Pi package has nothing to do: %v / %v", err, f.calls)
	}
}

func TestPiInstaller_UninstallReportsFailures(t *testing.T) {
	in, f, root := newPiInstaller(t)
	ours := filepath.Join(root, "superpowers")
	f.sources = []string{ours}
	f.removeErr = errors.New("locked")

	err := in.Uninstall(Record{PiPackage: ours})
	if err == nil || !strings.Contains(err.Error(), "locked") || !strings.Contains(err.Error(), ours) {
		t.Fatalf("expected the failure to name the package, got %v", err)
	}
	f.removeErr, f.readErr = nil, errors.New("settings.json is unreadable")
	if err := in.Uninstall(Record{PiPackage: ours}); err == nil {
		t.Fatal("when Pi's settings can't be read, an uninstall must not claim success")
	}
}

func TestPiInstaller_ConflictsNameTheCommandThatRemovesAnotherCopy(t *testing.T) {
	in, f, root := newPiInstaller(t)
	f.sources = []string{
		"npm:pi-lens@1.2.3",                      // another package entirely
		"git:github.com/obra/superpowers@v7.0.2", // the same upstream, from git
		"/home/u/code/superpowers",               // the user's own checkout
		filepath.Join(root, "superpowers"),       // ours: never a conflict
		filepath.Join(root, "ponytail"),          // another pack of ours
	}

	got := in.Conflicts(superpowers)

	want := []string{
		"pi remove git:github.com/obra/superpowers@v7.0.2",
		"pi remove /home/u/code/superpowers",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("conflicts:\n got %v\nwant %v", got, want)
	}
}

func TestPiInstaller_NoConflictWhenOnlyOurOwnCopyIsThere(t *testing.T) {
	in, f, root := newPiInstaller(t)
	f.sources = []string{filepath.Join(root, "superpowers")}

	if got := in.Conflicts(superpowers); len(got) != 0 {
		t.Errorf("our own install is not a conflict, got %v", got)
	}
}

func TestPiInstaller_ConflictCheckThatCannotReadReportsNoConflict(t *testing.T) {
	in, f, _ := newPiInstaller(t)
	f.readErr = errors.New("unreadable")

	if got := in.Conflicts(superpowers); len(got) != 0 {
		t.Errorf("not being able to look must not invent a conflict, got %v", got)
	}
}

func TestPiInstaller_TheDevgetaPackIsAnOrdinaryPiPackage(t *testing.T) {
	in, f, root := newPiInstaller(t)
	dir := filepath.Join(root, "devgeta")
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"devgeta","pi":{"extensions":["./extensions/devgeta-instructions.js"]}}`, 0o644)
	tree := Tree{Pack: embeddedManifest(), Dir: dir}

	rec, err := in.Install(tree)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.PiPackage != dir || !reflect.DeepEqual(f.calls, [][]string{{"install", dir}}) {
		t.Errorf("expected `pi install <tree>`, got record %+v, calls %v", rec, f.calls)
	}
}

func TestPiInstaller_InspectReportsThePiVersionItRanAgainst(t *testing.T) {
	in, f, root := newPiInstaller(t)
	f.version = "1.5.0\n"

	got := in.Inspect(piTree(t, root, "superpowers", true))

	if got.Err != nil {
		t.Fatalf("unexpected error: %v", got.Err)
	}
	if len(got.Lines) != 1 || !strings.Contains(got.Lines[0], "pi 1.5.0") || !strings.Contains(got.Lines[0], constants.PinnedVersion.Pi) {
		t.Errorf("expected the version and the one devgeta last checked, got %v", got.Lines)
	}
	if !reflect.DeepEqual(f.calls, [][]string{{"--version"}}) {
		t.Errorf("expected `pi --version`, got %v", f.calls)
	}
}

func TestPiInstaller_InspectNeverFailsTheCheckOverPiItself(t *testing.T) {
	in, f, root := newPiInstaller(t)
	f.versionErr = errors.New("boom")
	got := in.Inspect(piTree(t, root, "superpowers", true))
	if got.Err != nil || len(got.Lines) != 1 || !strings.Contains(got.Lines[0], "could not read") {
		t.Errorf("an unreadable version is a note, not a failed check; got %+v", got)
	}

	f.installed = false
	f.calls = nil
	got = in.Inspect(piTree(t, root, "superpowers", true))
	if got.Err != nil || len(got.Lines) != 0 || len(f.calls) != 0 {
		t.Errorf("no Pi, nothing to report; got %+v / %v", got, f.calls)
	}
}

func TestPiInstaller_VerifyReportsADifferenceFromState(t *testing.T) {
	in, f, root := newPiInstaller(t)
	ours := filepath.Join(root, "superpowers")

	f.sources = []string{ours}
	if got := in.Verify("superpowers", Record{PiPackage: ours}); got != "" {
		t.Errorf("recorded and listed agree, got %q", got)
	}

	f.sources = nil
	got := in.Verify("superpowers", Record{PiPackage: ours})
	if !strings.Contains(got, ours) || !strings.Contains(got, "dg agent-pack install superpowers --coder pi") {
		t.Errorf("expected the difference and the command that fixes it, got %q", got)
	}

	f.readErr = errors.New("unreadable")
	if got := in.Verify("superpowers", Record{PiPackage: ours}); !strings.Contains(got, "could not read") {
		t.Errorf("an unreadable settings file must be said, not read as 'fine': %q", got)
	}
	if got := in.Verify("devgeta", Record{}); got != "" {
		t.Errorf("a record with no Pi package has nothing to verify, got %q", got)
	}
}
