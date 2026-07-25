package btop

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

// TestNew verifies that the New factory method creates a properly initialized Btop instance.
func TestNew(t *testing.T) {
	app := New()

	if app == nil {
		t.Fatal("New() returned nil")
	}

	if app.Cmd == nil {
		t.Error("New() returned instance with nil Cmd")
	}

	if app.Base == nil {
		t.Error("New() returned instance with nil Base")
	}
}

// TestInstall verifies that Install calls the platform package manager correctly.
func TestInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Btop{Cmd: mc}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}

	expectedPkg := "btop"
	if mc.InstalledPkg != expectedPkg {
		t.Fatalf("expected InstallPackage(%s), got %q", expectedPkg, mc.InstalledPkg)
	}
}

// SKIP: ForceInstall test as per testing guidelines
// ForceInstall calls Uninstall (which returns error) before Install
// Testing this creates false negatives since Uninstall is not supported
// Test Install and Uninstall independently instead

// TestSoftInstall verifies that SoftInstall checks before installing.
func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Btop{Cmd: mc}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}

	expectedPkg := "btop"
	if mc.MaybeInstalled != expectedPkg {
		t.Fatalf("expected MaybeInstallPackage(%s), got %q", expectedPkg, mc.MaybeInstalled)
	}
}

// SKIP: TestForceConfigure since it's not needed for now

// SKIP: TestSoftConfigure since it's not needed for now

// SKIP: TestUninstall since it's not needed for now

// TestExecuteCommand verifies that ExecuteCommand properly executes btop with arguments.
func TestExecuteCommand(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &Btop{Cmd: mc, Base: mockBase}

	t.Run("successful execution", func(t *testing.T) {
		mockBase.SetExecCommandResult("btop++ 1.2.13", "", nil)

		err := app.ExecuteCommand("--version")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		// Verify command was called
		if mockBase.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 ExecCommand call, got %d", mockBase.GetExecCommandCallCount())
		}

		// Verify parameters
		lastCall := mockBase.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}
		if lastCall.Command != "btop" {
			t.Fatalf("Expected command 'btop', got %q", lastCall.Command)
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
		mockBase.SetExecCommandResult(
			"",
			"command not found",
			fmt.Errorf("command not found: btop"),
		)

		err := app.ExecuteCommand("--invalid-flag")

		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}

		// Verify error message contains context
		if !strings.Contains(err.Error(), "failed to run btop command") {
			t.Fatalf("Expected error to contain 'failed to run btop command', got: %v", err)
		}

		// Verify original error is wrapped
		if !strings.Contains(err.Error(), "command not found: btop") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})

	t.Run("multiple arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("btop running", "", nil)

		err := app.ExecuteCommand("--update", "2000")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		// Verify all arguments are passed correctly
		lastCall := mockBase.GetLastExecCommandCall()
		expectedArgs := []string{"--update", "2000"}

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

// SKIP: Update test as per testing guidelines
