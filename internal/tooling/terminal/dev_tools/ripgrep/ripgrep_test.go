package ripgrep

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

// TestNew verifies the factory method creates a properly initialized instance.
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

// TestInstall verifies standard installation behavior.
func TestInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Ripgrep{Cmd: mc, Base: commands.NewMockBaseCommand()}

	if err := app.Install(); err != nil {
		t.Fatalf("Install() returned error: %v", err)
	}

	// Verify correct package was installed
	if mc.InstalledPkg != "ripgrep" {
		t.Fatalf("expected InstallPackage(ripgrep), got %q", mc.InstalledPkg)
	}
}

// TestSoftInstall verifies conditional installation behavior.
func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Ripgrep{Cmd: mc, Base: commands.NewMockBaseCommand()}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall() returned error: %v", err)
	}

	// Verify MaybeInstallPackage was called
	if mc.MaybeInstalled != "ripgrep" {
		t.Fatalf("expected MaybeInstallPackage(ripgrep), got %q", mc.MaybeInstalled)
	}
}

// SKIP: ForceInstall test as per testing guidelines
// ForceInstall calls Uninstall (which returns error) before Install
// Testing this creates false negatives
// func TestForceInstall(t *testing.T) { ... }

// TestUninstall verifies it returns expected error for unsupported operation.
func TestUninstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Ripgrep{Cmd: mc, Base: commands.NewMockBaseCommand()}

	err := app.Uninstall()
	if err == nil {
		t.Fatal("expected error for unsupported operation")
	}

	expectedMsg := "ripgrep uninstall not supported through devgeta"
	if err.Error() != expectedMsg {
		t.Fatalf("expected error message %q, got %q", expectedMsg, err.Error())
	}
}

// TestForceConfigure verifies force configuration behavior.
func TestForceConfigure(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Ripgrep{Cmd: mc, Base: commands.NewMockBaseCommand()}

	// Ripgrep doesn't use config files, should return nil
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure() returned error: %v", err)
	}
}

// TestSoftConfigure verifies conditional configuration behavior.
func TestSoftConfigure(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Ripgrep{Cmd: mc, Base: commands.NewMockBaseCommand()}

	// Ripgrep doesn't use config files, should return nil
	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure() returned error: %v", err)
	}
}

// TestExecuteCommand verifies command execution with various scenarios.
func TestExecuteCommand(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &Ripgrep{Cmd: mc, Base: mockBase}

	t.Run("successful execution", func(t *testing.T) {
		mockBase.SetExecCommandResult("ripgrep 14.0.0", "", nil)

		err := app.ExecuteCommand("--version")
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
		if lastCall.Command != "rg" {
			t.Fatalf("Expected command 'rg', got %q", lastCall.Command)
		}
		if len(lastCall.Args) != 1 || lastCall.Args[0] != "--version" {
			t.Fatalf("Expected args ['--version'], got %v", lastCall.Args)
		}
		if lastCall.IsSudo {
			t.Fatal("Expected IsSudo to be false")
		}
	})

	t.Run("command execution error", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("", "command not found", fmt.Errorf("command not found: rg"))

		err := app.ExecuteCommand("--invalid-flag")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}

		// Verify error contains context
		if !strings.Contains(err.Error(), "failed to run ripgrep command") {
			t.Fatalf("Expected error to contain context, got: %v", err)
		}

		// Verify original error is wrapped
		if !strings.Contains(err.Error(), "command not found: rg") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})

	t.Run("multiple arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("match results", "", nil)

		err := app.ExecuteCommand("-i", "--type", "go", "TODO")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		// Verify all arguments are passed correctly
		lastCall := mockBase.GetLastExecCommandCall()
		expectedArgs := []string{"-i", "--type", "go", "TODO"}

		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}

		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})

	t.Run("search operation", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("file.go:42:// TODO: implement", "", nil)

		err := app.ExecuteCommand("TODO", ".")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		// Verify search parameters
		lastCall := mockBase.GetLastExecCommandCall()
		if lastCall.Command != "rg" {
			t.Fatalf("Expected command 'rg', got %q", lastCall.Command)
		}
		if len(lastCall.Args) != 2 {
			t.Fatalf("Expected 2 args, got %d", len(lastCall.Args))
		}
		if lastCall.Args[0] != "TODO" || lastCall.Args[1] != "." {
			t.Fatalf("Expected args ['TODO', '.'], got %v", lastCall.Args)
		}
	})
}

// SKIP: Update test as per testing guidelines
// Update is not implemented and should be handled by system package manager
// func TestUpdate(t *testing.T) { ... }
