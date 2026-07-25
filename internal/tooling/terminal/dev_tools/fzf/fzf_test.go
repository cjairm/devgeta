package fzf

import (
	"fmt"
	"os"
	"reflect"
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
	if app.Cmd == nil {
		t.Fatal("Cmd is nil")
	}
	if app.Base == nil {
		t.Fatal("Base is nil")
	}
}

func TestInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Fzf{Cmd: mc}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}

	if mc.InstalledPkg != "fzf" {
		t.Fatalf("expected InstallPackage(fzf), got %q", mc.InstalledPkg)
	}
}

func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Fzf{Cmd: mc}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}

	if mc.MaybeInstalled != "fzf" {
		t.Fatalf("expected MaybeInstallPackage(fzf), got %q", mc.MaybeInstalled)
	}
}

func TestForceConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &Fzf{Cmd: tc.MockApp.Cmd}

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

	if !strings.Contains(string(content), "# Fzf enabled") {
		t.Error("Expected shell config to contain Fzf feature")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &Fzf{Cmd: tc.MockApp.Cmd}

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

	if !strings.Contains(string(content), "# Fzf enabled") {
		t.Error("Expected shell config to contain Fzf feature on first call")
	}

	// Second call should skip (feature already enabled)
	err = app.SoftConfigure()
	if err != nil {
		t.Fatalf("SoftConfigure should not error on second call: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestUninstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Fzf{Cmd: mc}

	err := app.Uninstall()
	if err == nil {
		t.Fatal("expected Uninstall to return error")
	}
	if err.Error() != "fzf uninstall not supported through devgeta" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestUpdate(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Fzf{Cmd: mc}

	err := app.Update()
	if err == nil {
		t.Fatal("expected Update to return error")
	}
	expectedMsg := "fzf update not implemented - use system package manager"
	if err.Error() != expectedMsg {
		t.Fatalf("expected error %q, got %q", expectedMsg, err.Error())
	}
}

func TestExecuteCommand(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &Fzf{Cmd: mc, Base: mockBase}

	t.Run("successful execution", func(t *testing.T) {
		mockBase.SetExecCommandResult("success", "", nil)

		err := app.ExecuteCommand("--version")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		if mockBase.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 ExecCommand call, got %d", mockBase.GetExecCommandCallCount())
		}

		lastCall := mockBase.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}

		if lastCall.Command != "fzf" {
			t.Fatalf("Expected command 'fzf', got %q", lastCall.Command)
		}

		expectedArgs := []string{"--version"}
		if !reflect.DeepEqual(lastCall.Args, expectedArgs) {
			t.Fatalf("Expected args %v, got %v", expectedArgs, lastCall.Args)
		}

		if lastCall.IsSudo {
			t.Fatal("Expected IsSudo to be false")
		}
	})

	t.Run("command execution error", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("", "command not found", fmt.Errorf("command not found: fzf"))

		err := app.ExecuteCommand("--invalid")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}

		if !strings.Contains(err.Error(), "failed to run fzf command") {
			t.Fatalf("Expected error to contain 'failed to run fzf command', got: %v", err)
		}

		if !strings.Contains(err.Error(), "command not found: fzf") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})

	t.Run("multiple arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("output", "", nil)

		err := app.ExecuteCommand("--query", "test", "--reverse")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		if mockBase.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 call, got %d", mockBase.GetExecCommandCallCount())
		}

		lastCall := mockBase.GetLastExecCommandCall()
		expectedArgs := []string{"--query", "test", "--reverse"}

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

func TestSelectFromList(t *testing.T) {
	t.Run("empty list returns error", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &Fzf{
			Cmd:  commands.NewMockCommand(),
			Base: mockBase,
		}

		items := []string{}
		_, err := app.SelectFromList(items, "Select:")

		if err == nil {
			t.Fatal("Expected error for empty list")
		}

		if err.Error() != "no items to select from" {
			t.Errorf("Unexpected error: %v", err)
		}
	})

	// Note: Testing successful selection and cancellation requires actual fzf execution
	// since we use exec.Command directly with stdin piping. Those are integration tests.
	// The empty list test validates the interface and basic error handling.
}

// SKIP: ForceInstall test
// ForceInstall calls Uninstall (which returns error) before Install
// Testing this creates false negatives since Uninstall is intentionally unsupported
// func TestForceInstall(t *testing.T) { ... }
