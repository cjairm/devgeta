package commands_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
)

// These tests cover MacOSCommand.InstallPackageManager, which is where a
// fresh macOS install stopped dead before this file existed:
//
//	Installing Homebrew
//	/bin/bash: #!/bin/bash: No such file or directory
//	Error: failed to install Homebrew: exit status 127
//
// devgeta passed Homebrew's documented one-liner to exec.Command as
// `/bin/bash -c '$(curl …)'`. With no outer shell to perform the command
// substitution, bash did it itself, split the downloaded script on
// whitespace, and ran its shebang line as a command.
//
// Like the rest of macos_test.go, these control what actually runs through
// PATH rather than through a mock — MacOSCommand embeds a concrete
// BaseCommand, so there is no seam to inject. Nothing here reaches the
// network or the real Homebrew: `curl` is a stand-in script and so is `brew`.

// writeFakeCurl drops an executable "curl" on disk that ignores its arguments
// and writes body to stdout, exiting with exitCode. It stands in for the
// download of Homebrew's installer.
func writeFakeCurl(t *testing.T, dir, body string, exitCode int) {
	t.Helper()
	script := fmt.Sprintf(
		"#!/bin/sh\ncat <<'DEVGETA_FAKE_CURL_EOF'\n%s\nDEVGETA_FAKE_CURL_EOF\nexit %d\n",
		body,
		exitCode,
	)
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake curl: %v", err)
	}
}

// isolatePATH narrows PATH to dir plus the system directories, so a `brew`
// installed on the machine running the tests cannot answer for the one under
// test. Homebrew never lives in /usr/bin or /bin; keeping those means the
// stand-in scripts still have a working `cat` and friends.
func isolatePATH(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", strings.Join([]string{dir, "/usr/bin", "/bin"}, string(os.PathListSeparator)))
	// PATH is only half the story: the code under test resolves binaries
	// through the package's LookPathFn seam, which other tests in this package
	// swap. Pin it to the real thing so these tests answer to PATH alone,
	// whatever ran before them.
	original := commands.LookPathFn
	t.Cleanup(func() { commands.LookPathFn = original })
	commands.LookPathFn = exec.LookPath
}

// stubHomebrewPrefixes points the "is brew installed but off PATH?" search at
// a directory the test controls, instead of the real /opt/homebrew that most
// macOS dev machines have.
func stubHomebrewPrefixes(t *testing.T, prefixes ...string) {
	t.Helper()
	original := commands.HomebrewPrefixes
	t.Cleanup(func() { commands.HomebrewPrefixes = original })
	commands.HomebrewPrefixes = prefixes
}

func TestInstallPackageManagerRunsTheDownloadedInstaller(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "installer-ran")
	// A shebang first line is the whole point: that line is what the old
	// unquoted `$(curl …)` argument tried to execute.
	writeFakeCurl(t, dir, fmt.Sprintf("#!/bin/bash\necho ok > %q\n", marker), 0)
	writeFakeBrewAt(t, dir, 0)
	isolatePATH(t, dir)
	stubHomebrewPrefixes(t, t.TempDir())

	m := newMacOSCommand()
	if err := m.InstallPackageManager(); err != nil {
		t.Fatalf("unexpected error from InstallPackageManager: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the downloaded installer never ran: %v", err)
	}
}

func TestInstallPackageManagerFailsWhenTheDownloadFails(t *testing.T) {
	dir := t.TempDir()
	writeFakeCurl(t, dir, "", 1)
	writeFakeBrewAt(t, dir, 0)
	isolatePATH(t, dir)
	stubHomebrewPrefixes(t, t.TempDir())

	m := newMacOSCommand()
	err := m.InstallPackageManager()
	if err == nil {
		t.Fatal("a failed download must not be reported as a successful install")
	}
	if !strings.Contains(err.Error(), "failed to install Homebrew") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestInstallPackageManagerFailsWhenTheDownloadIsEmpty(t *testing.T) {
	dir := t.TempDir()
	// curl succeeding with no body is the silent case: an empty script runs
	// and exits 0, so without the guard devgeta announced "Homebrew installed"
	// and failed on the next brew call instead.
	writeFakeCurl(t, dir, "", 0)
	writeFakeBrewAt(t, dir, 0)
	isolatePATH(t, dir)
	stubHomebrewPrefixes(t, t.TempDir())

	m := newMacOSCommand()
	if err := m.InstallPackageManager(); err == nil {
		t.Fatal("an empty installer download must not be reported as a successful install")
	}
}

func TestInstallPackageManagerPutsANewHomebrewOnPATH(t *testing.T) {
	dir := t.TempDir()
	writeFakeCurl(t, dir, "#!/bin/bash\nexit 0\n", 0)
	isolatePATH(t, dir)

	// Homebrew's installer leaves PATH to the user's shell profile, which this
	// process will never read. brew therefore exists only under the prefix.
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("failed to create %s: %v", binDir, err)
	}
	writeFakeBrewAt(t, binDir, 0)
	stubHomebrewPrefixes(t, prefix)

	m := newMacOSCommand()
	if err := m.InstallPackageManager(); err != nil {
		t.Fatalf("unexpected error from InstallPackageManager: %v", err)
	}
	if !strings.Contains(os.Getenv("PATH"), binDir) {
		t.Errorf("expected %s on PATH after the install, got %q", binDir, os.Getenv("PATH"))
	}
	if !m.IsPackageManagerInstalled() {
		t.Error("brew should be usable for the rest of the run once it is on PATH")
	}
}

func TestInstallPackageManagerReportsAMissingBrewAfterInstalling(t *testing.T) {
	dir := t.TempDir()
	writeFakeCurl(t, dir, "#!/bin/bash\nexit 0\n", 0)
	isolatePATH(t, dir)
	stubHomebrewPrefixes(t, t.TempDir())

	m := newMacOSCommand()
	err := m.InstallPackageManager()
	if err == nil {
		t.Fatal("an installer that leaves no brew behind must be an error")
	}
	if !strings.Contains(err.Error(), "no `brew` command was found") {
		t.Errorf("error should say what is missing and what to do, got: %v", err)
	}
}

func TestIsPackageManagerInstalledFindsBrewOffPATH(t *testing.T) {
	// The same PATH repair on the probe side: a Homebrew this process cannot
	// see is otherwise indistinguishable from a missing one, and `dg install`
	// would re-run the installer over a working Homebrew.
	isolatePATH(t, t.TempDir())
	prefix := t.TempDir()
	binDir := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("failed to create %s: %v", binDir, err)
	}
	writeFakeBrewAt(t, binDir, 0)
	stubHomebrewPrefixes(t, prefix)

	m := newMacOSCommand()
	if !m.IsPackageManagerInstalled() {
		t.Error("expected an installed-but-off-PATH Homebrew to be reported as installed")
	}
}

func TestIsPackageManagerInstalledReportsNoBrewAnywhere(t *testing.T) {
	isolatePATH(t, t.TempDir())
	stubHomebrewPrefixes(t, t.TempDir())

	m := newMacOSCommand()
	if m.IsPackageManagerInstalled() {
		t.Error("expected no Homebrew to be reported as not installed")
	}
}
