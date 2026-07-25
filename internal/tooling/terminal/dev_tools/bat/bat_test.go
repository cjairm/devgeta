package bat

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
)

func init() {
	testutil.InitLogger()
}

func TestNew(t *testing.T) {
	app := New()

	if app == nil {
		t.Fatal("New() returned nil")
	}
}

func TestInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Bat{Cmd: mc}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mc.InstalledPkg != "bat" {
		t.Fatalf("expected InstallPackage(%s), got %q", "bat", mc.InstalledPkg)
	}
}

// SKIP: ForceInstall test as per guidelines
// func TestForceInstall(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &Bat{Cmd: mc}
//
// 	if err := app.ForceInstall(); err != nil {
// 		t.Fatalf("ForceInstall error: %v", err)
// 	}
// 	// ForceInstall should call Install() which uses InstallPackage
// 	if mc.InstalledPkg != "bat" {
// 		t.Fatalf("expected InstallPackage(%s), got %q", "bat", mc.InstalledPkg)
// 	}
// }

func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Bat{Cmd: mc}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mc.MaybeInstalled != "bat" {
		t.Fatalf("expected MaybeInstallPackage(%s), got %q", "bat", mc.MaybeInstalled)
	}
}

func TestForceConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &Bat{Cmd: tc.MockApp.Cmd}

	// Test ForceConfigure - should enable shell feature
	err := app.ForceConfigure()
	if err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	// Verify shell config was generated
	content, err := os.ReadFile(tc.ZshConfigPath)
	if err != nil {
		t.Fatalf("Failed to read shell config: %v", err)
	}

	if !strings.Contains(string(content), "# Bat enabled") {
		t.Error("Expected shell config to contain Bat feature")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &Bat{Cmd: tc.MockApp.Cmd}

	// First call should configure
	err := app.SoftConfigure()
	if err != nil {
		t.Fatalf("SoftConfigure error: %v", err)
	}

	// Verify shell config was generated
	content, err := os.ReadFile(tc.ZshConfigPath)
	if err != nil {
		t.Fatalf("Failed to read shell config: %v", err)
	}

	if !strings.Contains(string(content), "# Bat enabled") {
		t.Error("Expected shell config to contain Bat feature on first call")
	}

	// Second call should skip (feature already enabled)
	err = app.SoftConfigure()
	if err != nil {
		t.Fatalf("SoftConfigure should not error on second call: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestExecuteCommand(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &Bat{Cmd: mc, Base: mockBase}

	// Test 1: Successful execution
	t.Run("successful execution", func(t *testing.T) {
		mockBase.SetExecCommandResult("bat 0.24.0", "", nil)

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
		if lastCall.Command != "bat" {
			t.Fatalf("Expected command 'bat', got %q", lastCall.Command)
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
			fmt.Errorf("command not found: bat"),
		)

		err := app.ExecuteCommand("--invalid-flag")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}
		if !strings.Contains(err.Error(), "failed to run bat command") {
			t.Fatalf("Expected error to contain 'failed to run bat command', got: %v", err)
		}

		// Verify the error was properly wrapped
		if !strings.Contains(err.Error(), "command not found: bat") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})

	// Test 3: Multiple arguments
	t.Run("multiple arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("file content with syntax highlighting", "", nil)

		err := app.ExecuteCommand("-n", "--theme", "Monokai Extended", "file.go")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockBase.GetLastExecCommandCall()
		expectedArgs := []string{"-n", "--theme", "Monokai Extended", "file.go"}
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
// 	app := &Bat{Cmd: mc}
//
// 	err := app.Uninstall()
// 	if err == nil {
// 		t.Fatal("expected Uninstall to return error for unsupported operation")
// 	}
// 	if err.Error() != "bat uninstall not supported through devgeta" {
// 		t.Fatalf("unexpected error message: %v", err)
// 	}
// }

// SKIP: Update test as per guidelines
// func TestUpdate(t *testing.T) {
// 	mc := commands.NewMockCommand()
// 	app := &Bat{Cmd: mc}
//
// 	err := app.Update()
// 	if err == nil {
// 		t.Fatal("expected Update to return error for unsupported operation")
// 	}
// 	if err.Error() != "bat update not implemented through devgeta" {
// 		t.Fatalf("unexpected error message: %v", err)
// 	}
// }
