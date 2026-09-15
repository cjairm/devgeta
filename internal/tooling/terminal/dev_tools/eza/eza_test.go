package eza

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
)

func init() {
	// Initialize logger for tests
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
	app := &Eza{Cmd: mc}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mc.InstalledPkg != "eza" {
		t.Fatalf("expected InstallPackage(%s), got %q", "eza", mc.InstalledPkg)
	}
}

func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Eza{Cmd: mc}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mc.MaybeInstalled != "eza" {
		t.Fatalf("expected MaybeInstallPackage(%s), got %q", "eza", mc.MaybeInstalled)
	}
}

func TestForceConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &Eza{Cmd: tc.MockApp.Cmd}

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

	if !strings.Contains(string(content), "# Eza enabled") {
		t.Error("Expected shell config to contain Eza feature")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &Eza{Cmd: tc.MockApp.Cmd}

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

	if !strings.Contains(string(content), "# Eza enabled") {
		t.Error("Expected shell config to contain Eza feature on first call")
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
	app := &Eza{Cmd: mc, Base: mockBase}

	// Test 1: Successful execution
	t.Run("successful execution", func(t *testing.T) {
		mockBase.SetExecCommandResult("eza 0.17.0", "", nil)

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
		if lastCall.Command != "eza" {
			t.Fatalf("Expected command 'eza', got %q", lastCall.Command)
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
			fmt.Errorf("command not found: eza"),
		)

		err := app.ExecuteCommand("--invalid-flag")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}
		if !strings.Contains(err.Error(), "failed to run eza command") {
			t.Fatalf("Expected error to contain 'failed to run eza command', got: %v", err)
		}

		// Verify the error was properly wrapped
		if !strings.Contains(err.Error(), "command not found: eza") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})

	// Test 3: Multiple arguments
	t.Run("multiple arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("file listing output", "", nil)

		err := app.ExecuteCommand("-l", "-a", "-h")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockBase.GetLastExecCommandCall()
		expectedArgs := []string{"-l", "-a", "-h"}
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

// TestNameAndKind covers what registration needs: GetApp() hands back an
// apps.App, and both `dg configure` and the uninstall bookkeeping identify it
// by Name(). Without these two methods eza cannot be registered at all, which
// is why `dg configure eza --force` answered "unknown app".
func TestNameAndKind(t *testing.T) {
	app := &Eza{}

	if got := app.Name(); got != "eza" {
		t.Errorf("Name() is %q, want %q", got, "eza")
	}
	if got := app.Kind(); got != apps.KindTerminal {
		t.Errorf("Kind() is %v, want KindTerminal", got)
	}
}

// TestUnsupportedOperationsReturnSentinels: the App contract requires the
// sentinel errors, never free-form strings, because callers branch on them -
// baseapp.Reinstall lets ForceInstall through only when Uninstall's error IS
// apps.ErrUninstallNotSupported, and cmd/configure.go reports a clean message
// only for apps.ErrConfigureNotSupported. A hand-written string is an ordinary
// failure to every one of those callers.
func TestUnsupportedOperationsReturnSentinels(t *testing.T) {
	app := &Eza{}

	if err := app.Uninstall(); !errors.Is(err, apps.ErrUninstallNotSupported) {
		t.Errorf("Uninstall() returned %v, want apps.ErrUninstallNotSupported", err)
	}
	if err := app.Update(); !errors.Is(err, apps.ErrUpdateNotSupported) {
		t.Errorf("Update() returned %v, want apps.ErrUpdateNotSupported", err)
	}
}

// TestForceInstall_ReinstallsThroughAnUnsupportedUninstall is why the sentinels
// matter here in practice. ForceInstall called Uninstall directly and returned
// its error, so `--force` could never install eza at all: Uninstall always
// fails for a tool devgeta cannot remove. baseapp.Reinstall is the contract's
// answer - tolerate that one sentinel, then install.
func TestForceInstall_ReinstallsThroughAnUnsupportedUninstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Eza{Cmd: mc}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() failed: %v", err)
	}
	if mc.InstalledPkg != "eza" {
		t.Errorf("expected InstallPackage(%q), got %q", "eza", mc.InstalledPkg)
	}
}
