// Package unzip provides tests for the unzip archive extraction utility module.
//
// These tests use mock interfaces to verify app behavior without executing real
// system commands. Tests follow devgeta testing patterns with dependency injection
// via BaseCommandExecutor interface.
//
// References:
//   - Testing patterns: docs/guides/testing-patterns.md
//   - Error handling: docs/guides/error-handling.md

package unzip

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/logger"
)

func init() {
	// Initialize logger for tests (prevents nil pointer errors)
	logger.Init(false)
}

// TestNew verifies that New() creates a properly initialized Unzip instance.
func TestNew(t *testing.T) {
	app := New()

	if app == nil {
		t.Fatal("New() returned nil")
	}

	if app.Cmd == nil {
		t.Fatal("New() created instance with nil Cmd")
	}

	if app.Base == nil {
		t.Fatal("New() created instance with nil Base")
	}
}

// TestInstall verifies that Install() calls InstallPackage with correct package name.
func TestInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Unzip{Cmd: mc}

	err := app.Install()
	if err != nil {
		t.Fatalf("Install() returned error: %v", err)
	}

	// Verify InstallPackage was called with correct package name
	if mc.InstalledPkg != "unzip" {
		t.Fatalf("expected InstallPackage(unzip), got InstallPackage(%s)", mc.InstalledPkg)
	}
}

// SKIP: ForceInstall test as per guidelines
// ForceInstall calls Uninstall (which returns error) before Install
// Testing this creates false negatives since Uninstall is not supported
// func TestForceInstall(t *testing.T) { ... }

// TestSoftInstall verifies that SoftInstall() calls MaybeInstallPackage.
func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Unzip{Cmd: mc}

	err := app.SoftInstall()
	if err != nil {
		t.Fatalf("SoftInstall() returned error: %v", err)
	}

	// Verify MaybeInstallPackage was called with correct package name
	if mc.MaybeInstalled != "unzip" {
		t.Fatalf(
			"expected MaybeInstallPackage(unzip), got MaybeInstallPackage(%s)",
			mc.MaybeInstalled,
		)
	}
}

// TestForceConfigure verifies that ForceConfigure() returns nil (no-op).
func TestForceConfigure(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &Unzip{Cmd: mc, Base: mockBase}

	err := app.ForceConfigure()
	if err != nil {
		t.Fatalf("ForceConfigure() returned error: %v (expected nil for no-op)", err)
	}
}

// TestSoftConfigure verifies that SoftConfigure() returns nil (no-op).
func TestSoftConfigure(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &Unzip{Cmd: mc, Base: mockBase}

	err := app.SoftConfigure()
	if err != nil {
		t.Fatalf("SoftConfigure() returned error: %v (expected nil for no-op)", err)
	}
}

// SKIP: Uninstall test - verifies error return for unsupported operation
// Uninstall is not supported for unzip as per app design
// func TestUninstall(t *testing.T) {
//     err := app.Uninstall()
//     if err == nil {
//         t.Fatal("expected error for unsupported operation")
//     }
// }

// TestExecuteCommand tests unzip command execution scenarios.
func TestExecuteCommand(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &Unzip{Cmd: mc, Base: mockBase}

	t.Run("successful execution with single argument", func(t *testing.T) {
		mockBase.SetExecCommandResult("Archive:  test.zip\n  inflating: file.txt", "", nil)

		err := app.ExecuteCommand("test.zip")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		// Verify command was called
		if mockBase.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 ExecCommand call, got %d", mockBase.GetExecCommandCallCount())
		}

		// Verify command parameters
		lastCall := mockBase.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}
		if lastCall.Command != "unzip" {
			t.Fatalf("Expected command 'unzip', got %q", lastCall.Command)
		}
		if len(lastCall.Args) != 1 || lastCall.Args[0] != "test.zip" {
			t.Fatalf("Expected args ['test.zip'], got %v", lastCall.Args)
		}
		if lastCall.IsSudo {
			t.Fatal("Expected IsSudo to be false")
		}
	})

	t.Run("successful execution with multiple arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("Archive:  test.zip\n  inflating: /tmp/file.txt", "", nil)

		err := app.ExecuteCommand("-d", "/tmp", "test.zip")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		// Verify command parameters
		lastCall := mockBase.GetLastExecCommandCall()
		expectedArgs := []string{"-d", "/tmp", "test.zip"}

		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}

		if !reflect.DeepEqual(lastCall.Args, expectedArgs) {
			t.Fatalf("Expected args %v, got %v", expectedArgs, lastCall.Args)
		}
	})

	t.Run("command execution error", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("", "unzip: cannot find test.zip",
			fmt.Errorf("command failed with exit code 9"))

		err := app.ExecuteCommand("test.zip")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}

		// Verify error message contains context
		if !strings.Contains(err.Error(), "failed to run unzip command") {
			t.Fatalf("Expected error to contain 'failed to run unzip command', got: %v", err)
		}

		// Verify original error is wrapped
		if !strings.Contains(err.Error(), "command failed with exit code 9") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})
}

// SKIP: Update test - verifies error return for unimplemented operation
// Update is not implemented as package manager handles updates
// func TestUpdate(t *testing.T) {
//     err := app.Update()
//     if err == nil {
//         t.Fatal("expected error for unimplemented operation")
//     }
// }
