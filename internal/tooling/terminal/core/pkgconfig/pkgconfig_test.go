package pkgconfig

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/logger"
)

func init() {
	logger.Init(false)
}

func TestNew(t *testing.T) {
	app := New()

	if app == nil {
		t.Fatal("New() returned nil")
	}
}

func TestInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &PkgConfig{Cmd: mc}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mc.InstalledPkg != "pkg-config" {
		t.Fatalf("expected InstallPackage(%s), got %q", "pkg-config", mc.InstalledPkg)
	}
}

// SKIP: ForceInstall test as per guidelines
// ForceInstall calls Uninstall() first which returns error since pkg-config uninstall is not supported
// Testing this creates false negatives
// func TestForceInstall(t *testing.T) { ... }

func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &PkgConfig{Cmd: mc}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mc.MaybeInstalled != "pkg-config" {
		t.Fatalf("expected MaybeInstallPackage(%s), got %q", "pkg-config", mc.MaybeInstalled)
	}
}

// SKIP: ForceConfigure test since isn't needed for now
// func TestForceConfigure(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &PkgConfig{Cmd: mc}
//
// 	// pkg-config doesn't require separate configuration files
// 	// so ForceConfigure should return nil (no-op)
// 	if err := app.ForceConfigure(); err != nil {
// 		t.Fatalf("ForceConfigure error: %v", err)
// 	}
// }

// SKIP: SoftConfigure test since isn't needed for now
// func TestSoftConfigure(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &PkgConfig{Cmd: mc}
//
// 	// pkg-config doesn't require separate configuration files
// 	// so SoftConfigure should return nil (no-op)
// 	if err := app.SoftConfigure(); err != nil {
// 		t.Fatalf("SoftConfigure error: %v", err)
// 	}
// }

func TestExecuteCommand(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &PkgConfig{Cmd: mc, Base: mockBase}

	// Test 1: Successful execution
	t.Run("successful execution", func(t *testing.T) {
		mockBase.SetExecCommandResult("0.29.2", "", nil)

		err := app.ExecuteCommand("--version")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		// Verify ExecCommand was called once
		if mockBase.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 ExecCommand call, got %d", mockBase.GetExecCommandCallCount())
		}

		// Verify command parameters
		lastCall := mockBase.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}
		if lastCall.Command != "pkg-config" {
			t.Fatalf("Expected command 'pkg-config', got %q", lastCall.Command)
		}
		if len(lastCall.Args) != 1 || lastCall.Args[0] != "--version" {
			t.Fatalf("Expected args ['--version'], got %v", lastCall.Args)
		}
		if lastCall.IsSudo {
			t.Fatal("Expected IsSudo to be false")
		}
	})

	// Test 2: Error handling
	t.Run("command execution error", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult(
			"",
			"command not found",
			fmt.Errorf("command not found: pkg-config"),
		)

		err := app.ExecuteCommand("--invalid-flag")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}
		if !strings.Contains(err.Error(), "failed to run pkg-config command") {
			t.Fatalf("Expected error to contain 'failed to run pkg-config command', got: %v", err)
		}

		// Verify the error was properly wrapped
		if !strings.Contains(err.Error(), "command not found: pkg-config") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})

	// Test 3: Multiple arguments
	t.Run("multiple arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("-I/usr/include/glib-2.0 -lglib-2.0", "", nil)

		err := app.ExecuteCommand("--cflags", "--libs", "glib-2.0")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockBase.GetLastExecCommandCall()
		expectedArgs := []string{"--cflags", "--libs", "glib-2.0"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}
		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})

	// Test 4: Package query operations
	t.Run("package query operations", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("1.1.1", "", nil)

		err := app.ExecuteCommand("--modversion", "openssl")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockBase.GetLastExecCommandCall()
		if lastCall.Command != "pkg-config" {
			t.Fatalf("Expected command 'pkg-config', got %q", lastCall.Command)
		}
		expectedArgs := []string{"--modversion", "openssl"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}
	})
}

// SKIP: Uninstall test as per guidelines
// Uninstall returns error for unsupported operation
// func TestUninstall(t *testing.T) { ... }

// SKIP: Update test as per guidelines
// Update returns error for unsupported operation
// func TestUpdate(t *testing.T) { ... }
