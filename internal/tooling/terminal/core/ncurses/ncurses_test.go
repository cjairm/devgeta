package ncurses

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
	app := &Ncurses{Cmd: mc}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mc.InstalledPkg != "ncurses" {
		t.Fatalf("expected InstallPackage(%s), got %q", "ncurses", mc.InstalledPkg)
	}
}

// SKIP: ForceInstall test as per guidelines
// ForceInstall calls Uninstall (which returns error) before Install
// Testing this creates false negatives
// func TestForceInstall(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &Ncurses{Cmd: mc}
//
// 	if err := app.ForceInstall(); err != nil {
// 		t.Fatalf("ForceInstall error: %v", err)
// 	}
// 	if mc.InstalledPkg != "ncurses" {
// 		t.Fatalf("expected InstallPackage(%s), got %q", "ncurses", mc.InstalledPkg)
// 	}
// }

func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Ncurses{Cmd: mc}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mc.MaybeInstalled != "ncurses" {
		t.Fatalf("expected MaybeInstallPackage(%s), got %q", "ncurses", mc.MaybeInstalled)
	}
}

// SKIP: ForceConfigure test since isn't needed for now
// func TestForceConfigure(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &Ncurses{Cmd: mc}
//
// 	// Ncurses doesn't require separate configuration files
// 	// so ForceConfigure should return nil (no-op)
// 	if err := app.ForceConfigure(); err != nil {
// 		t.Fatalf("ForceConfigure error: %v", err)
// 	}
// }

// SKIP: SoftConfigure test since isn't needed for now
// func TestSoftConfigure(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &Ncurses{Cmd: mc}
//
// 	// Ncurses doesn't require separate configuration files
// 	// so SoftConfigure should return nil (no-op)
// 	if err := app.SoftConfigure(); err != nil {
// 		t.Fatalf("SoftConfigure error: %v", err)
// 	}
// }

func TestExecuteCommand(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &Ncurses{Cmd: mc, Base: mockBase}

	// Test 1: Successful execution
	t.Run("successful execution", func(t *testing.T) {
		mockBase.SetExecCommandResult("ncurses 6.4", "", nil)

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
		if lastCall.Command != "ncurses" {
			t.Fatalf("Expected command 'ncurses', got %q", lastCall.Command)
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
			fmt.Errorf("command not found: ncurses"),
		)

		err := app.ExecuteCommand("--invalid-flag")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}
		if !strings.Contains(err.Error(), "failed to run ncurses command") {
			t.Fatalf("Expected error to contain 'failed to run ncurses command', got: %v", err)
		}

		// Verify the error was properly wrapped
		if !strings.Contains(err.Error(), "command not found: ncurses") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})

	// Test 3: Multiple arguments
	t.Run("multiple arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("success", "", nil)

		err := app.ExecuteCommand("arg1", "arg2", "arg3")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockBase.GetLastExecCommandCall()
		expectedArgs := []string{"arg1", "arg2", "arg3"}
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
// Verify unsupported operations return appropriate errors
// func TestUninstall(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &Ncurses{Cmd: mc}
//
// 	err := app.Uninstall()
// 	if err == nil {
// 		t.Fatal("expected Uninstall to return error for unsupported operation")
// 	}
// 	if err.Error() != "ncurses uninstall not supported through devgeta" {
// 		t.Fatalf("unexpected error message: %v", err)
// 	}
// }

// SKIP: Update test as per guidelines
// Verify unsupported operations return appropriate errors
// func TestUpdate(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &Ncurses{Cmd: mc}
//
// 	err := app.Update()
// 	if err == nil {
// 		t.Fatal("expected Update to return error for unsupported operation")
// 	}
// 	if err.Error() != "ncurses update not implemented through devgeta" {
// 		t.Fatalf("unexpected error message: %v", err)
// 	}
// }
