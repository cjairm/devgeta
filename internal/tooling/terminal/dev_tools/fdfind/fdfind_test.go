package fdfind

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
	app := &FdFind{Cmd: mc}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mc.InstalledPkg != "fd" {
		t.Fatalf("expected InstallPackage(%s), got %q", "fd", mc.InstalledPkg)
	}
}

// SKIP: ForceInstall test as per guidelines
// func TestForceInstall(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &FdFind{Cmd: mc}
//
// 	if err := app.ForceInstall(); err != nil {
// 		t.Fatalf("ForceInstall error: %v", err)
// 	}
// 	// ForceInstall should call Install() which uses InstallPackage
// 	if mc.InstalledPkg != "fd-find" {
// 		t.Fatalf("expected InstallPackage(%s), got %q", "fd-find", mc.InstalledPkg)
// 	}
// }

func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &FdFind{Cmd: mc}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mc.MaybeInstalled != "fd" {
		t.Fatalf("expected MaybeInstallPackage(%s), got %q", "fd", mc.MaybeInstalled)
	}
}

// SKIP: ForceConfigure test since isn't needed for now
// func TestForceConfigure(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &FdFind{Cmd: mc}
//
// 	// fd-find doesn't require separate configuration files
// 	// so ForceConfigure should return nil (no-op)
// 	if err := app.ForceConfigure(); err != nil {
// 		t.Fatalf("ForceConfigure error: %v", err)
// 	}
// }

// SKIP: SoftConfigure test since isn't needed for now
// func TestSoftConfigure(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &FdFind{Cmd: mc}
//
// 	// fd-find doesn't require separate configuration files
// 	// so SoftConfigure should return nil (no-op)
// 	if err := app.SoftConfigure(); err != nil {
// 		t.Fatalf("SoftConfigure error: %v", err)
// 	}
// }

func TestExecuteCommand(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &FdFind{Cmd: mc, Base: mockBase}

	// Test 1: Successful execution
	t.Run("successful execution", func(t *testing.T) {
		mockBase.SetExecCommandResult("fd 8.3.2", "", nil)

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
		if lastCall.Command != "fd" {
			t.Fatalf("Expected command 'fd', got %q", lastCall.Command)
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
			fmt.Errorf("command not found: fd-find"),
		)

		err := app.ExecuteCommand("--invalid-flag")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}
		if !strings.Contains(err.Error(), "failed to run fd-find command") {
			t.Fatalf("Expected error to contain 'failed to run fd-find command', got: %v", err)
		}

		// Verify the error was properly wrapped
		if !strings.Contains(err.Error(), "command not found: fd-find") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})

	// Test 3: Multiple arguments
	t.Run("multiple arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("found files", "", nil)

		err := app.ExecuteCommand("-e", "go", "main")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockBase.GetLastExecCommandCall()
		expectedArgs := []string{"-e", "go", "main"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}
		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})
}

// SKIP: Uninstall test as per guidelines
// func TestUninstall(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &FdFind{Cmd: mc}
//
// 	err := app.Uninstall()
// 	if err == nil {
// 		t.Fatal("expected Uninstall to return error for unsupported operation")
// 	}
// 	if err.Error() != "fd-find uninstall not supported through devgeta" {
// 		t.Fatalf("unexpected error message: %v", err)
// 	}
// }

// SKIP: Update test as per guidelines
// func TestUpdate(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &FdFind{Cmd: mc}
//
// 	err := app.Update()
// 	if err == nil {
// 		t.Fatal("expected Update to return error for unsupported operation")
// 	}
// 	if err.Error() != "fd-find update not implemented through devgeta" {
// 		t.Fatalf("unexpected error message: %v", err)
// 	}
// }
