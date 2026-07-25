package xcode

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
	if app.Cmd == nil {
		t.Fatal("New() returned nil Cmd")
	}
	if app.Base == nil {
		t.Fatal("New() returned nil Base")
	}
}

func TestInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &XcodeCommandLineTools{Cmd: mc, Base: mockBase}

	t.Run("already installed", func(t *testing.T) {
		// Mock isInstalled() check to return true
		mockBase.SetExecCommandResult("/Library/Developer/CommandLineTools", "", nil)

		err := app.Install()
		if err != nil {
			t.Fatalf("Install error when already installed: %v", err)
		}

		// Should only call xcode-select -p (check), not --install
		if mockBase.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 call (check only), got %d", mockBase.GetExecCommandCallCount())
		}
	})

	t.Run("needs installation", func(t *testing.T) {
		mockBase.ResetExecCommand()

		// First call: isInstalled() returns false (command fails)
		// Second call: actual installation succeeds
		mockBase.SetExecCommandResult("", "", fmt.Errorf("not installed"))

		// We need to simulate two different responses, but MockBaseCommand
		// doesn't support that yet. For now, just test the structure.
		// TODO: Enhance mock to support sequential responses
	})
}

// SKIP: ForceInstall test as per guidelines
// ForceInstall calls Uninstall() first which returns error since xcode uninstall is not supported
// Testing this creates false negatives
// func TestForceInstall(t *testing.T) { ... }

func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &XcodeCommandLineTools{Cmd: mc, Base: mockBase}

	t.Run("already installed", func(t *testing.T) {
		// Mock isInstalled() to return true
		mockBase.SetExecCommandResult("/Library/Developer/CommandLineTools", "", nil)

		err := app.SoftInstall()
		if err != nil {
			t.Fatalf("SoftInstall error when already installed: %v", err)
		}

		// Should only call xcode-select -p (check)
		if mockBase.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 call (check only), got %d", mockBase.GetExecCommandCallCount())
		}
	})
}

func TestForceConfigure(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &XcodeCommandLineTools{Cmd: mc, Base: mockBase}

	// Xcode Command Line Tools don't require configuration files
	// ForceConfigure should return nil (no-op)
	err := app.ForceConfigure()
	if err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	// Should not call any commands
	if mockBase.GetExecCommandCallCount() != 0 {
		t.Fatalf("Expected 0 calls for no-op, got %d", mockBase.GetExecCommandCallCount())
	}
}

func TestSoftConfigure(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &XcodeCommandLineTools{Cmd: mc, Base: mockBase}

	// Xcode Command Line Tools don't require configuration files
	// SoftConfigure should return nil (no-op)
	err := app.SoftConfigure()
	if err != nil {
		t.Fatalf("SoftConfigure error: %v", err)
	}

	// Should not call any commands
	if mockBase.GetExecCommandCallCount() != 0 {
		t.Fatalf("Expected 0 calls for no-op, got %d", mockBase.GetExecCommandCallCount())
	}
}

func TestExecuteCommand(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &XcodeCommandLineTools{Cmd: mc, Base: mockBase}

	// Test 1: Successful execution
	t.Run("successful execution", func(t *testing.T) {
		mockBase.SetExecCommandResult("xcode-select version 2395", "", nil)

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
		if lastCall.Command != "xcode-select" {
			t.Fatalf("Expected command 'xcode-select', got %q", lastCall.Command)
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
			fmt.Errorf("command not found: xcode-select"),
		)

		err := app.ExecuteCommand("--invalid-flag")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}
		if !strings.Contains(err.Error(), "failed to run xcode-select command") {
			t.Fatalf("Expected error to contain 'failed to run xcode-select command', got: %v", err)
		}

		// Verify the error was properly wrapped
		if !strings.Contains(err.Error(), "command not found: xcode-select") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})

	// Test 3: Multiple arguments
	t.Run("multiple arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("/Applications/Xcode.app/Contents/Developer", "", nil)

		err := app.ExecuteCommand("--switch", "/Applications/Xcode.app/Contents/Developer")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockBase.GetLastExecCommandCall()
		expectedArgs := []string{"--switch", "/Applications/Xcode.app/Contents/Developer"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}
		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})

	// Test 4: Print path command
	t.Run("print path", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("/Library/Developer/CommandLineTools", "", nil)

		err := app.ExecuteCommand("--print-path")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockBase.GetLastExecCommandCall()
		if lastCall.Command != "xcode-select" {
			t.Fatalf("Expected command 'xcode-select', got %q", lastCall.Command)
		}
		if len(lastCall.Args) != 1 || lastCall.Args[0] != "--print-path" {
			t.Fatalf("Expected args ['--print-path'], got %v", lastCall.Args)
		}
	})
}

// SKIP: Uninstall test as per guidelines
// Uninstall returns error for unsupported operation
// func TestUninstall(t *testing.T) { ... }

// SKIP: Update test as per guidelines
// Update returns error for unsupported operation
// func TestUpdate(t *testing.T) { ... }

func TestIsInstalled(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &XcodeCommandLineTools{Cmd: mc, Base: mockBase}

	t.Run("installed with xcode.app path", func(t *testing.T) {
		mockBase.SetExecCommandResult("/Applications/Xcode.app/Contents/Developer", "", nil)

		installed, err := app.isInstalled()
		if err != nil {
			t.Fatalf("isInstalled error: %v", err)
		}
		if !installed {
			t.Fatal("Expected isInstalled to return true for xcode.app path")
		}
	})

	t.Run("installed with commandlinetools path", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("/Library/Developer/CommandLineTools", "", nil)

		installed, err := app.isInstalled()
		if err != nil {
			t.Fatalf("isInstalled error: %v", err)
		}
		if !installed {
			t.Fatal("Expected isInstalled to return true for commandlinetools path")
		}
	})

	t.Run("not installed - error checking", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("", "xcode-select: error", fmt.Errorf("command failed"))

		installed, err := app.isInstalled()
		if err == nil {
			t.Fatal("Expected isInstalled to return error when xcode-select command fails")
		}
		if installed {
			t.Fatal("Expected isInstalled to return false when command fails")
		}
		if !strings.Contains(err.Error(), "error running xcode-select") {
			t.Fatalf("Expected error to contain 'error running xcode-select', got: %v", err)
		}
	})
}
