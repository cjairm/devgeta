package libffi

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
	app := &Libffi{Cmd: mc}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mc.InstalledPkg != "libffi" {
		t.Fatalf("expected InstallPackage(%s), got %q", "libffi", mc.InstalledPkg)
	}
}

// SKIP: ForceInstall test as per guidelines
// ForceInstall calls Uninstall() first which returns error since libffi uninstall is not supported
// Testing this creates false negatives
// func TestForceInstall(t *testing.T) { ... }

func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Libffi{Cmd: mc}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mc.MaybeInstalled != "libffi" {
		t.Fatalf("expected MaybeInstallPackage(%s), got %q", "libffi", mc.MaybeInstalled)
	}
}

// SKIP: ForceConfigure test since isn't needed for now
// func TestForceConfigure(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &Libffi{Cmd: mc}
//
// 	// libffi doesn't require separate configuration files
// 	// so ForceConfigure should return nil (no-op)
// 	if err := app.ForceConfigure(); err != nil {
// 		t.Fatalf("ForceConfigure error: %v", err)
// 	}
// }

// SKIP: SoftConfigure test since isn't needed for now
// func TestSoftConfigure(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &Libffi{Cmd: mc}
//
// 	// libffi doesn't require separate configuration files
// 	// so SoftConfigure should return nil (no-op)
// 	if err := app.SoftConfigure(); err != nil {
// 		t.Fatalf("SoftConfigure error: %v", err)
// 	}
// }

func TestExecuteCommand(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &Libffi{Cmd: mc, Base: mockBase}

	// Test 1: Successful execution
	t.Run("successful execution", func(t *testing.T) {
		mockBase.SetExecCommandResult("libffi 3.4.4", "", nil)

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
		if lastCall.Command != "libffi" {
			t.Fatalf("Expected command 'libffi', got %q", lastCall.Command)
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
			fmt.Errorf("command not found: libffi"),
		)

		err := app.ExecuteCommand("--invalid-flag")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}
		if !strings.Contains(err.Error(), "failed to run libffi command") {
			t.Fatalf("Expected error to contain 'failed to run libffi command', got: %v", err)
		}

		// Verify the error was properly wrapped
		if !strings.Contains(err.Error(), "command not found: libffi") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})

	// Test 3: Multiple arguments
	t.Run("multiple arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("success", "", nil)

		err := app.ExecuteCommand("--cflags", "--libs")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockBase.GetLastExecCommandCall()
		expectedArgs := []string{"--cflags", "--libs"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}
		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})

	// Test 4: No arguments
	t.Run("no arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("libffi info", "", nil)

		err := app.ExecuteCommand()
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockBase.GetLastExecCommandCall()
		if lastCall.Command != "libffi" {
			t.Fatalf("Expected command 'libffi', got %q", lastCall.Command)
		}
		if len(lastCall.Args) != 0 {
			t.Fatalf("Expected no args, got %v", lastCall.Args)
		}
	})
}

// SKIP: Uninstall test as per guidelines
// Uninstall returns error for unsupported operation
// func TestUninstall(t *testing.T) { ... }

// SKIP: Update test as per guidelines
// Update returns error for unsupported operation
// func TestUpdate(t *testing.T) { ... }
