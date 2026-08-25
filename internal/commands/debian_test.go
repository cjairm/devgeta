package commands_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
)

// newDebianCommand builds a DebianCommand for tests. See newMacOSCommand's
// doc in macos_test.go for why this can't hold a commands.MockBaseCommand.
func newDebianCommand() *commands.DebianCommand {
	return &commands.DebianCommand{BaseCommand: *commands.NewBaseCommand()}
}

func resetDebianTestState(t *testing.T) {
	t.Helper()
	original := commands.CommandFn
	t.Cleanup(func() {
		commands.CommandFn = original
		commands.InvalidateInstalledPackageCache()
	})
	commands.InvalidateInstalledPackageCache()
}

// writeFakeAptTools drops harmless "sudo", "apt", and "apt-get" scripts in a
// fresh directory: DebianCommand.InstallPackage's default apt strategy shells
// out to "apt", UninstallPackage to "apt-get", and both go through IsSudo
// (Command becomes "sudo", with the original command prepended to its
// args) — so all three names need a stand-in for a test to reach these
// methods without touching the real package manager or a real sudo prompt.
// The fake sudo simply re-execs its own arguments, unprivileged, which lands
// on the fake apt/apt-get in the same directory.
func writeFakeAptTools(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	writeExecutable(t, dir, "sudo", `exec "$@"`)
	writeExecutable(t, dir, "apt", fmt.Sprintf("exit %d", exitCode))
	writeExecutable(t, dir, "apt-get", fmt.Sprintf("exit %d", exitCode))
	return dir
}

func writeExecutable(t *testing.T, dir, name, body string) {
	t.Helper()
	script := filepath.Join(dir, name)
	content := fmt.Sprintf("#!/bin/sh\n%s\n", body)
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("failed to write fake %s: %v", name, err)
	}
}

func TestDebianCommand_IsPackageInstalled_CachesListing(t *testing.T) {
	resetDebianTestState(t)

	calls := 0
	commands.CommandFn = func(name string, args ...string) *exec.Cmd {
		calls++
		// dpkg -l format: status, name, version (name is column 2).
		return fakeCmdWithOutput("ii  existingpkg  1.0\nii  anotherpkg  1.0")
	}

	d := newDebianCommand()

	found, err := d.IsPackageInstalled("existingpkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Errorf("expected existingpkg to be found")
	}

	found, err = d.IsPackageInstalled("missingpkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Errorf("expected missingpkg not to be found")
	}

	if calls != 1 {
		t.Errorf("expected the listing command to run exactly once, ran %d times", calls)
	}
}

// TestDebianCommand_InstallPackageInvalidatesCache is the install-then-probe
// case against the default (apt) strategy DebianCommand.InstallPackage
// dispatches to for any package name with no special-cased strategy.
func TestDebianCommand_InstallPackageInvalidatesCache(t *testing.T) {
	resetDebianTestState(t)

	listing := "ii  existingpkg  1.0"
	commands.CommandFn = func(name string, args ...string) *exec.Cmd {
		return fakeCmdWithOutput(listing)
	}

	d := newDebianCommand()

	found, err := d.IsPackageInstalled("newpkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatalf("newpkg should not be found before install")
	}

	setFakePATH(t, writeFakeAptTools(t, 0))
	listing = "ii  existingpkg  1.0\nii  newpkg  1.0"

	if err := d.InstallPackage("newpkg"); err != nil {
		t.Fatalf("unexpected error from InstallPackage: %v", err)
	}

	found, err = d.IsPackageInstalled("newpkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Errorf("expected newpkg to be found after install invalidated the cache")
	}
}

// TestDebianCommand_InstallPackageInvalidatesCacheEvenOnFailure mirrors
// ADR-0029's "attempted, not only successful" rule on the Debian side.
func TestDebianCommand_InstallPackageInvalidatesCacheEvenOnFailure(t *testing.T) {
	resetDebianTestState(t)

	listing := "ii  existingpkg  1.0"
	commands.CommandFn = func(name string, args ...string) *exec.Cmd {
		return fakeCmdWithOutput(listing)
	}

	d := newDebianCommand()

	if _, err := d.IsPackageInstalled("newpkg"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	setFakePATH(t, writeFakeAptTools(t, 1)) // fails, like a real failed install
	listing = "ii  existingpkg  1.0\nii  newpkg  1.0"

	if err := d.InstallPackage("newpkg"); err == nil {
		t.Fatalf("expected InstallPackage to report the fake apt's failure")
	}

	found, err := d.IsPackageInstalled("newpkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Errorf("expected the cache to have been dropped even though the install failed")
	}
}

// TestDebianCommand_UninstallPackageInvalidatesCache is the uninstall-then-
// probe case: a package uninstalled after the cache was populated must not
// keep reading as present.
func TestDebianCommand_UninstallPackageInvalidatesCache(t *testing.T) {
	resetDebianTestState(t)

	listing := "ii  oldpkg  1.0"
	commands.CommandFn = func(name string, args ...string) *exec.Cmd {
		return fakeCmdWithOutput(listing)
	}

	d := newDebianCommand()

	found, err := d.IsPackageInstalled("oldpkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatalf("oldpkg should be found before uninstall")
	}

	setFakePATH(t, writeFakeAptTools(t, 0))
	listing = ""

	if err := d.UninstallPackage("oldpkg"); err != nil {
		t.Fatalf("unexpected error from UninstallPackage: %v", err)
	}

	found, err = d.IsPackageInstalled("oldpkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Errorf("expected oldpkg not to be found after uninstall invalidated the cache")
	}
}

// TestDebianCommand_UninstallDesktopAppInvalidatesCache exercises
// UninstallDesktopApp, which delegates to UninstallPackage (debian.go) —
// confirming that delegation carries the invalidation through rather than
// needing (or duplicating) a drop of its own.
func TestDebianCommand_UninstallDesktopAppInvalidatesCache(t *testing.T) {
	resetDebianTestState(t)

	listing := "ii  oldapp  1.0"
	commands.CommandFn = func(name string, args ...string) *exec.Cmd {
		return fakeCmdWithOutput(listing)
	}

	d := newDebianCommand()

	if found, err := d.IsPackageInstalled("oldapp"); err != nil || !found {
		t.Fatalf("oldapp should be found before uninstall (found=%v err=%v)", found, err)
	}

	setFakePATH(t, writeFakeAptTools(t, 0))
	listing = ""

	if err := d.UninstallDesktopApp("oldapp"); err != nil {
		t.Fatalf("unexpected error from UninstallDesktopApp: %v", err)
	}

	if found, err := d.IsPackageInstalled("oldapp"); err != nil || found {
		t.Fatalf("expected oldapp not to be found after uninstall (found=%v err=%v)", found, err)
	}
}
