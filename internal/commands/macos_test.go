package commands_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
)

// newMacOSCommand builds a MacOSCommand for tests. Its embedded BaseCommand
// field is the real, concrete implementation — MacOSCommand embeds
// BaseCommand by value, not the BaseCommandExecutor interface, so there is
// no seam to substitute commands.MockBaseCommand through for these tests.
// Every test in this file that reaches ExecCommand (InstallPackage,
// UninstallPackage, InstallDesktopApp, UninstallDesktopApp) therefore
// controls what actually runs via PATH — a harmless stand-in "brew" script —
// rather than via a mock. The probe side (IsPackageInstalled,
// IsDesktopAppInstalled) is mocked the same way base_test.go already does
// for IsFontPresent: swapping commands.CommandFn.
func newMacOSCommand() *commands.MacOSCommand {
	return &commands.MacOSCommand{BaseCommand: *commands.NewBaseCommand()}
}

// resetMacOSTestState restores the package-level test seams this file
// mutates: the installed-package cache (ADR-0029's process-wide state, which
// outlives any one test) and commands.CommandFn (already used the same way
// by TestIsFontPresent's fallback subtests in base_test.go).
func resetMacOSTestState(t *testing.T) {
	t.Helper()
	original := commands.CommandFn
	t.Cleanup(func() {
		commands.CommandFn = original
		commands.InvalidateInstalledPackageCache()
	})
	commands.InvalidateInstalledPackageCache()
}

// setFakePATH prepends dir to the real PATH (rather than replacing it), so a
// fake binary placed there shadows the real one while everything else this
// process needs — bash for fakeCmdWithOutput, coreutils, etc. — still
// resolves normally.
func setFakePATH(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// writeFakeBrew drops an executable "brew" script on disk that always exits
// with exitCode, ignoring its arguments — a stand-in for the real `brew
// install`/`brew uninstall` a mutation-seam method shells out to, so the
// test never touches the real package manager. Returns the directory it was
// written into, to be passed to setFakePATH.
func writeFakeBrew(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "brew")
	body := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("failed to write fake brew: %v", err)
	}
	return dir
}

func TestMacOSCommand_IsPackageInstalled_CachesListing(t *testing.T) {
	resetMacOSTestState(t)

	calls := 0
	commands.CommandFn = func(name string, args ...string) *exec.Cmd {
		calls++
		return fakeCmdWithOutput("existingpkg\nanotherpkg")
	}

	m := newMacOSCommand()

	found, err := m.IsPackageInstalled("existingpkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Errorf("expected existingpkg to be found")
	}

	// A second, different probe must be answered from the cache: the
	// listing command must not run again. This is ADR-0029's entire point —
	// one `brew list` per process, not one per package.
	found, err = m.IsPackageInstalled("missingpkg")
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

func TestMacOSCommand_IsDesktopAppInstalled_CachesListing(t *testing.T) {
	resetMacOSTestState(t)

	calls := 0
	commands.CommandFn = func(name string, args ...string) *exec.Cmd {
		calls++
		return fakeCmdWithOutput("mycask")
	}

	m := newMacOSCommand()

	found, err := m.IsDesktopAppInstalled("mycask")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Errorf("expected mycask to be found")
	}

	if _, err := m.IsDesktopAppInstalled("othercask"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if calls != 1 {
		t.Errorf("expected the cask listing command to run exactly once, ran %d times", calls)
	}
}

// TestMacOSCommand_InstallPackageInvalidatesCache is the install-then-probe
// case: a package installed after the cache was already populated must not
// keep reading as missing.
func TestMacOSCommand_InstallPackageInvalidatesCache(t *testing.T) {
	resetMacOSTestState(t)

	listing := "existingpkg"
	commands.CommandFn = func(name string, args ...string) *exec.Cmd {
		return fakeCmdWithOutput(listing)
	}

	m := newMacOSCommand()

	found, err := m.IsPackageInstalled("newpkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatalf("newpkg should not be found before install")
	}

	// The real `brew install` this method shells out to must never run in a
	// test: point PATH at a harmless stand-in instead.
	setFakePATH(t, writeFakeBrew(t, 0))

	// Simulate the package manager's own state changing as a result of the
	// install: the next listing includes it.
	listing = "existingpkg\nnewpkg"

	if err := m.InstallPackage("newpkg"); err != nil {
		t.Fatalf("unexpected error from InstallPackage: %v", err)
	}

	found, err = m.IsPackageInstalled("newpkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Errorf("expected newpkg to be found after install invalidated the cache")
	}
}

// TestMacOSCommand_InstallPackageInvalidatesCacheEvenOnFailure covers
// ADR-0029's "attempted, not only successful" rule: a failed brew install
// can still have changed package state (e.g. it left a partial install), so
// the cache must drop regardless of InstallPackage's returned error.
func TestMacOSCommand_InstallPackageInvalidatesCacheEvenOnFailure(t *testing.T) {
	resetMacOSTestState(t)

	listing := "existingpkg"
	commands.CommandFn = func(name string, args ...string) *exec.Cmd {
		return fakeCmdWithOutput(listing)
	}

	m := newMacOSCommand()

	if _, err := m.IsPackageInstalled("newpkg"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	setFakePATH(t, writeFakeBrew(t, 1)) // fails, like a real failed install
	listing = "existingpkg\nnewpkg"

	if err := m.InstallPackage("newpkg"); err == nil {
		t.Fatalf("expected InstallPackage to report the fake brew's failure")
	}

	found, err := m.IsPackageInstalled("newpkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Errorf("expected the cache to have been dropped even though the install failed")
	}
}

// TestMacOSCommand_UninstallPackageInvalidatesCache is the uninstall-then-
// probe case the plan text called out as the one previously left out: a
// package uninstalled after the cache was populated must not keep reading
// as present.
func TestMacOSCommand_UninstallPackageInvalidatesCache(t *testing.T) {
	resetMacOSTestState(t)

	listing := "oldpkg"
	commands.CommandFn = func(name string, args ...string) *exec.Cmd {
		return fakeCmdWithOutput(listing)
	}

	m := newMacOSCommand()

	found, err := m.IsPackageInstalled("oldpkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatalf("oldpkg should be found before uninstall")
	}

	setFakePATH(t, writeFakeBrew(t, 0))
	listing = ""

	if err := m.UninstallPackage("oldpkg"); err != nil {
		t.Fatalf("unexpected error from UninstallPackage: %v", err)
	}

	found, err = m.IsPackageInstalled("oldpkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Errorf("expected oldpkg not to be found after uninstall invalidated the cache")
	}
}

// TestMacOSCommand_DesktopAppMutationsInvalidateCache covers the cask side
// (InstallDesktopApp / UninstallDesktopApp) with the same install-then-probe
// and uninstall-then-probe shape as the formula tests above.
func TestMacOSCommand_DesktopAppMutationsInvalidateCache(t *testing.T) {
	resetMacOSTestState(t)

	listing := "existingcask"
	commands.CommandFn = func(name string, args ...string) *exec.Cmd {
		return fakeCmdWithOutput(listing)
	}

	m := newMacOSCommand()

	if found, err := m.IsDesktopAppInstalled("newcask"); err != nil || found {
		t.Fatalf("newcask should not be found yet (found=%v err=%v)", found, err)
	}

	setFakePATH(t, writeFakeBrew(t, 0))
	listing = "existingcask\nnewcask"

	if err := m.InstallDesktopApp("newcask"); err != nil {
		t.Fatalf("unexpected error from InstallDesktopApp: %v", err)
	}
	if found, err := m.IsDesktopAppInstalled("newcask"); err != nil || !found {
		t.Fatalf("expected newcask to be found after install (found=%v err=%v)", found, err)
	}

	listing = "existingcask"

	if err := m.UninstallDesktopApp("newcask"); err != nil {
		t.Fatalf("unexpected error from UninstallDesktopApp: %v", err)
	}
	if found, err := m.IsDesktopAppInstalled("newcask"); err != nil || found {
		t.Fatalf("expected newcask not to be found after uninstall (found=%v err=%v)", found, err)
	}
}
