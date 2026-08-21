package apt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/pkg/logger"
)

func init() { logger.Init(false) }

// recordedCommand is one command the code under test tried to execute.
type recordedCommand struct {
	name string
	args []string
	// stagedContent is the content of the staged source file for `install`
	// commands, captured at call time because the staging directory is removed
	// as soon as the call returns.
	stagedContent string
}

func (r recordedCommand) String() string {
	return strings.TrimSpace(r.name + " " + strings.Join(r.args, " "))
}

// fakeExec stands in for every external process this package would otherwise
// spawn. Nothing here executes anything: this path shells out to sudo, apt and
// gpg, so a test that ran it for real would modify the machine running the suite.
type fakeExec struct {
	t *testing.T

	calls     []recordedCommand
	downloads []string

	// failOn makes the named command fail; failOutput is the output it reports.
	failOn      string
	failOutput  string
	downloadErr error

	// stdout is what capture-style commands report.
	stdout string

	// stagingDirs collects every staging directory the code created, so tests can
	// assert it was removed regardless of which error path ran.
	stagingDirs map[string]struct{}
}

func newFakeExec(t *testing.T) *fakeExec {
	t.Helper()
	return &fakeExec{t: t, stdout: "amd64\n", stagingDirs: map[string]struct{}{}}
}

// manager returns a PPAManager wired entirely to this fake.
func (f *fakeExec) manager() *PPAManager {
	return &PPAManager{run: f.run, capture: f.captureOutput, download: f.download}
}

func (f *fakeExec) run(name string, args ...string) ([]byte, error) {
	f.t.Helper()

	call := recordedCommand{name: name, args: args}
	// `gpg --batch --yes --dearmor --output <dest> <src>` — a real gpg leaves the
	// dearmored key behind for the install step, so the fake must too.
	if name == "gpg" && len(args) == 6 && args[2] == "--dearmor" && f.failOn != "gpg" {
		f.noteStagingDir(args[4])
		if err := os.WriteFile(args[4], []byte("binary keyring"), 0o600); err != nil {
			f.t.Fatalf("fake gpg could not write %s: %v", args[4], err)
		}
	}
	// `sudo install -m 644 <src> <dest>` — read the staged file before the caller
	// deletes its staging directory.
	if name == "sudo" && len(args) == 5 && args[0] == "install" && args[1] == "-m" {
		f.noteStagingDir(args[3])
		data, err := os.ReadFile(args[3])
		if err != nil {
			f.t.Errorf("staged file %s is not readable: %v", args[3], err)
		}
		call.stagedContent = string(data)
	}
	for _, arg := range args {
		f.noteStagingDir(arg)
	}
	f.calls = append(f.calls, call)

	if f.failOn != "" && name == f.failOn {
		return []byte(f.failOutput), errors.New("exit status 1")
	}
	return nil, nil
}

func (f *fakeExec) captureOutput(name string, args ...string) ([]byte, error) {
	f.t.Helper()
	f.calls = append(f.calls, recordedCommand{name: name, args: args})
	if f.failOn == name {
		return nil, errors.New("exit status 1")
	}
	return []byte(f.stdout), nil
}

func (f *fakeExec) download(_ context.Context, url, destPath string) error {
	f.t.Helper()
	f.downloads = append(f.downloads, url)
	f.noteStagingDir(destPath)
	if f.downloadErr != nil {
		return f.downloadErr
	}
	// A real download leaves the armored key on disk for gpg to read.
	if err := os.WriteFile(
		destPath,
		[]byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n"),
		0o600,
	); err != nil {
		f.t.Fatalf("fake download could not write %s: %v", destPath, err)
	}
	return nil
}

// noteStagingDir records any path that lives inside a devgeta staging directory.
func (f *fakeExec) noteStagingDir(path string) {
	dir := filepath.Dir(path)
	if strings.Contains(filepath.Base(dir), "devgeta-ppa-") {
		f.stagingDirs[dir] = struct{}{}
	}
}

func (f *fakeExec) commandLines() []string {
	lines := make([]string, 0, len(f.calls))
	for _, c := range f.calls {
		lines = append(lines, c.String())
	}
	return lines
}

func (f *fakeExec) callFor(t *testing.T, substr string) recordedCommand {
	t.Helper()
	for _, c := range f.calls {
		if strings.Contains(c.String(), substr) {
			return c
		}
	}
	t.Fatalf("no command matching %q; got %v", substr, f.commandLines())
	return recordedCommand{}
}

func (f *fakeExec) ran(substr string) bool {
	for _, c := range f.calls {
		if strings.Contains(c.String(), substr) {
			return true
		}
	}
	return false
}

// assertStagingCleaned fails when any staging directory the code created survived.
// It is the regression guard for the leak this file was rewritten to remove:
// every path, success or failure, must release what it acquired.
func (f *fakeExec) assertStagingCleaned(t *testing.T) {
	t.Helper()
	if len(f.stagingDirs) == 0 {
		t.Fatal("no staging directory was observed; the test is not exercising the staged path")
	}
	for dir := range f.stagingDirs {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("staging directory %s was not removed (stat err: %v)", dir, err)
		}
	}
}

func TestAddPPARejectsIncompleteConfig(t *testing.T) {
	testCases := []struct {
		name   string
		config PPAConfig
	}{
		{
			"missing name",
			PPAConfig{KeyURL: "https://example.test/key", RepoURL: "https://example.test/deb"},
		},
		{"missing key URL", PPAConfig{Name: "demo", RepoURL: "https://example.test/deb"}},
		{"missing repo URL", PPAConfig{Name: "demo", KeyURL: "https://example.test/key"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeExec(t)

			err := fake.manager().AddPPA(tc.config)
			if err == nil {
				t.Fatal("expected an error for an incomplete config")
			}
			if len(fake.calls) != 0 {
				t.Errorf("validation ran commands: %v", fake.commandLines())
			}
			if len(fake.downloads) != 0 {
				t.Errorf("validation downloaded: %v", fake.downloads)
			}
		})
	}
}

func TestAddPPARunsExpectedCommandsInOrder(t *testing.T) {
	fake := newFakeExec(t)
	fake.stdout = "arm64\n"

	config := PPAConfig{
		Name:    "devgeta-test-ppa",
		KeyURL:  "https://example.test/key.asc",
		RepoURL: "https://example.test/deb",
	}

	if err := fake.manager().AddPPA(config); err != nil {
		t.Fatalf("AddPPA: %v", err)
	}

	got := fake.commandLines()
	want := []string{
		"dpkg --print-architecture",
		"sudo apt install -y gpg wget curl",
		"sudo install -dm 755 /etc/apt/keyrings",
		"gpg --batch --yes --dearmor --output",
		"sudo install -m 644",
		"sudo install -m 644",
		"sudo apt update",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d commands, got %d: %v", len(want), len(got), got)
	}
	for i, prefix := range want {
		if !strings.HasPrefix(got[i], prefix) {
			t.Errorf("command %d = %q, want prefix %q", i, got[i], prefix)
		}
	}

	if len(fake.downloads) != 1 || fake.downloads[0] != config.KeyURL {
		t.Errorf("downloads = %v, want exactly [%s]", fake.downloads, config.KeyURL)
	}

	// No trace of the old pipeline should remain.
	for _, forbidden := range []string{"wget -qO", "tee ", "echo "} {
		if fake.ran(forbidden) {
			t.Errorf("command %q still spawns a piped process: %v", forbidden, fake.commandLines())
		}
	}

	sourcesEntry := fake.calls[len(fake.calls)-2]
	wantEntry := "deb [signed-by=/etc/apt/keyrings/devgeta-test-ppa-archive-keyring.gpg arch=arm64] " +
		"https://example.test/deb stable main\n"
	if sourcesEntry.stagedContent != wantEntry {
		t.Errorf("staged sources entry = %q, want %q", sourcesEntry.stagedContent, wantEntry)
	}
	if dest := sourcesEntry.args[4]; dest != "/etc/apt/sources.list.d/devgeta-test-ppa.list" {
		t.Errorf("sources destination = %q", dest)
	}

	keyringInstall := fake.calls[len(fake.calls)-3]
	if dest := keyringInstall.args[4]; dest != "/etc/apt/keyrings/devgeta-test-ppa-archive-keyring.gpg" {
		t.Errorf("keyring destination = %q", dest)
	}

	fake.assertStagingCleaned(t)
}

func TestAddPPAHonoursExplicitConfigValues(t *testing.T) {
	fake := newFakeExec(t)

	config := PPAConfig{
		Name:         "devgeta-test-ppa",
		KeyURL:       "https://example.test/key.asc",
		RepoURL:      "https://example.test/deb",
		Distribution: "bookworm",
		Component:    "contrib",
		Architecture: "amd64",
	}

	if err := fake.manager().AddPPA(config); err != nil {
		t.Fatalf("AddPPA: %v", err)
	}

	if fake.ran("dpkg") {
		t.Error("architecture was detected even though the config specified one")
	}
	entry := fake.calls[len(fake.calls)-2].stagedContent
	if !strings.Contains(entry, "arch=amd64") || !strings.Contains(entry, "bookworm contrib") {
		t.Errorf("staged entry did not use the configured values: %q", entry)
	}
}

func TestAddPPAFailsWhenArchitectureDetectionFails(t *testing.T) {
	fake := newFakeExec(t)
	fake.failOn = "dpkg"

	err := fake.manager().AddPPA(PPAConfig{
		Name:    "devgeta-test-ppa",
		KeyURL:  "https://example.test/key.asc",
		RepoURL: "https://example.test/deb",
	})
	if err == nil {
		t.Fatal("expected AddPPA to fail")
	}
	if !strings.Contains(err.Error(), "failed to detect architecture") {
		t.Errorf("unexpected error: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Errorf("AddPPA continued past the failure: %v", fake.commandLines())
	}
}

func TestInstallGPGKeyStopsAndCleansUpWhenDownloadFails(t *testing.T) {
	fake := newFakeExec(t)
	fake.downloadErr = errors.New("connection refused")

	err := fake.manager().
		installGPGKey("https://example.test/key.asc", "/etc/apt/keyrings/demo.gpg")
	if err == nil {
		t.Fatal("expected installGPGKey to fail")
	}
	if !strings.Contains(err.Error(), "failed to download GPG key") {
		t.Errorf("unexpected error: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("commands ran after a failed download: %v", fake.commandLines())
	}
	fake.assertStagingCleaned(t)
}

func TestInstallGPGKeyStopsAndCleansUpWhenDearmorFails(t *testing.T) {
	fake := newFakeExec(t)
	fake.failOn = "gpg"
	fake.failOutput = "gpg: no valid OpenPGP data found"

	err := fake.manager().
		installGPGKey("https://example.test/key.asc", "/etc/apt/keyrings/demo.gpg")
	if err == nil {
		t.Fatal("expected installGPGKey to fail")
	}
	if !strings.Contains(err.Error(), "failed to dearmor GPG key") ||
		!strings.Contains(err.Error(), "no valid OpenPGP data found") {
		t.Errorf("error dropped the tool output: %v", err)
	}
	if fake.ran("sudo install") {
		t.Errorf("keyring was installed despite a dearmor failure: %v", fake.commandLines())
	}
	fake.assertStagingCleaned(t)
}

func TestInstallGPGKeyCleansUpWhenInstallFails(t *testing.T) {
	fake := newFakeExec(t)
	fake.failOn = "sudo"
	fake.failOutput = "install: cannot create regular file"

	err := fake.manager().
		installGPGKey("https://example.test/key.asc", "/etc/apt/keyrings/demo.gpg")
	if err == nil {
		t.Fatal("expected installGPGKey to fail")
	}
	if !strings.Contains(err.Error(), "/etc/apt/keyrings/demo.gpg") {
		t.Errorf("error did not name the destination: %v", err)
	}
	fake.assertStagingCleaned(t)
}

func TestInstallGPGKeyDearmorsIntoTheStagingDirectory(t *testing.T) {
	fake := newFakeExec(t)

	if err := fake.manager().
		installGPGKey("https://example.test/key.asc", "/etc/apt/keyrings/demo.gpg"); err != nil {
		t.Fatalf("installGPGKey: %v", err)
	}

	gpgCall := fake.callFor(t, "gpg --batch")
	// gpg reads and writes files; neither end of the command is a pipe.
	output := gpgCall.args[4]
	input := gpgCall.args[5]
	if filepath.Dir(output) != filepath.Dir(input) {
		t.Errorf("gpg input %q and output %q are not both staged", input, output)
	}
	if installed := fake.callFor(t, "sudo install -m 644").args[3]; installed != output {
		t.Errorf("installed %q but dearmored to %q", installed, output)
	}
	fake.assertStagingCleaned(t)
}

func TestCreateRepositoryEntryStagesTheEntryAndInstallsIt(t *testing.T) {
	fake := newFakeExec(t)

	entry := "deb [signed-by=/etc/apt/keyrings/demo.gpg arch=amd64] https://example.test/deb stable main"
	if err := fake.manager().
		createRepositoryEntry(entry, "/etc/apt/sources.list.d/demo.list"); err != nil {
		t.Fatalf("createRepositoryEntry: %v", err)
	}

	call := fake.callFor(t, "sudo install -m 644")
	if call.stagedContent != entry+"\n" {
		t.Errorf("staged content = %q, want %q", call.stagedContent, entry+"\n")
	}
	if call.args[4] != "/etc/apt/sources.list.d/demo.list" {
		t.Errorf("destination = %q", call.args[4])
	}
	if len(fake.calls) != 1 {
		t.Errorf("expected exactly one command, got %v", fake.commandLines())
	}
	fake.assertStagingCleaned(t)
}

func TestCreateRepositoryEntryCleansUpWhenInstallFails(t *testing.T) {
	fake := newFakeExec(t)
	fake.failOn = "sudo"
	fake.failOutput = "install: permission denied"

	err := fake.manager().createRepositoryEntry("deb https://example.test/deb stable main",
		"/etc/apt/sources.list.d/demo.list")
	if err == nil {
		t.Fatal("expected createRepositoryEntry to fail")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error dropped the tool output: %v", err)
	}
	fake.assertStagingCleaned(t)
}

func TestWithStagingDirRemovesTheDirectoryOnBothPaths(t *testing.T) {
	pm := &PPAManager{}

	var successDir string
	if err := pm.withStagingDir("unit", func(dir string) error {
		successDir = dir
		return os.WriteFile(filepath.Join(dir, "file"), []byte("x"), 0o600)
	}); err != nil {
		t.Fatalf("withStagingDir: %v", err)
	}
	if _, err := os.Stat(successDir); !os.IsNotExist(err) {
		t.Errorf("staging directory %s survived the success path", successDir)
	}

	sentinel := errors.New("boom")
	var failureDir string
	err := pm.withStagingDir("unit", func(dir string) error {
		failureDir = dir
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("withStagingDir swallowed the callback error: %v", err)
	}
	if _, err := os.Stat(failureDir); !os.IsNotExist(err) {
		t.Errorf("staging directory %s survived the failure path", failureDir)
	}
}

func TestDetectArchitectureTrimsOutput(t *testing.T) {
	fake := newFakeExec(t)
	fake.stdout = "arm64\n"

	arch, err := fake.manager().detectArchitecture()
	if err != nil {
		t.Fatalf("detectArchitecture: %v", err)
	}
	if arch != "arm64" {
		t.Errorf("arch = %q, want %q", arch, "arm64")
	}
}

// A zero-value PPAManager must still resolve to working defaults; NewPPAManager
// leaves the seams nil, and internal/commands constructs it that way.
func TestDefaultSeamsAreResolved(t *testing.T) {
	for name, pm := range map[string]*PPAManager{
		"NewPPAManager": NewPPAManager(),
		"zero value":    {},
	} {
		if pm.runner() == nil || pm.capturer() == nil || pm.downloader() == nil {
			t.Errorf("%s: a default seam was nil", name)
		}
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "present")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !fileExists(file) {
		t.Error("fileExists returned false for an existing file")
	}
	if fileExists(filepath.Join(dir, "absent")) {
		t.Error("fileExists returned true for a missing file")
	}
	if fileExists(dir) {
		t.Error("fileExists returned true for a directory")
	}
}

// Guard: every seam is injected in these tests, so nothing in this package may
// reach a real binary or a real system path. This asserts the fake is the thing
// being called, and that the run left no trace outside its staging directories.
func TestFakeExecInterceptsEveryExternalCall(t *testing.T) {
	fake := newFakeExec(t)

	if err := fake.manager().AddPPA(PPAConfig{
		Name:    "devgeta-test-ppa",
		KeyURL:  "https://example.test/key.asc",
		RepoURL: "https://example.test/deb",
	}); err != nil {
		t.Fatalf("AddPPA: %v", err)
	}

	if len(fake.calls) == 0 {
		t.Fatal("no command reached the fake, so a real process may have been spawned")
	}
	for _, path := range []string{
		"/etc/apt/sources.list.d/devgeta-test-ppa.list",
		"/etc/apt/keyrings/devgeta-test-ppa-archive-keyring.gpg",
	} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s exists; the run touched a real system path", path)
		}
	}
	fake.assertStagingCleaned(t)
}
