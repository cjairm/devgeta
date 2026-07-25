package fontconfig

import (
	"fmt"
	"testing"

	"github.com/cjairm/devgeta/internal/testutil"
)

func init() {
	testutil.InitLogger()
}

// TestNew verifies FontConfig instance creation
func TestNew(t *testing.T) {
	fc := New()

	if fc == nil {
		t.Fatal("New() returned nil")
	}
}

// TestInstall verifies fontconfig package installation
func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	fc := &FontConfig{
		Cmd:  mockApp.Cmd,
		Base: mockApp.Base,
	}

	err := fc.Install()

	if err != nil {
		t.Fatalf("Install() failed: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

// TestSoftInstall verifies conditional fontconfig installation
func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	fc := &FontConfig{
		Cmd:  mockApp.Cmd,
		Base: mockApp.Base,
	}

	err := fc.SoftInstall()

	if err != nil {
		t.Fatalf("SoftInstall() failed: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

// TestForceInstall is skipped - uninstall not supported
func TestForceInstall(t *testing.T) {
	t.Skip("ForceInstall calls Uninstall which is not supported for fontconfig")

	// This test is skipped because:
	// - ForceInstall calls Uninstall first
	// - Uninstall is not supported for system libraries
	// - The method will always return an error from Uninstall
}

// TestForceConfigure verifies fontconfig configuration override
func TestForceConfigure(t *testing.T) {
	mockApp := testutil.NewMockApp()
	fc := &FontConfig{
		Cmd:  mockApp.Cmd,
		Base: mockApp.Base,
	}

	// Fontconfig uses system defaults, no devgeta-managed config needed
	err := fc.ForceConfigure()

	// Should return nil since fontconfig doesn't need configuration
	if err != nil {
		t.Errorf("Expected nil error, got: %v", err)
	}
}

// TestSoftConfigure verifies conditional fontconfig configuration
func TestSoftConfigure(t *testing.T) {
	mockApp := testutil.NewMockApp()
	fc := &FontConfig{
		Cmd:  mockApp.Cmd,
		Base: mockApp.Base,
	}

	t.Run("not configured", func(t *testing.T) {
		// Fontconfig uses system defaults, no devgeta-managed config needed
		err := fc.SoftConfigure()

		// Should return nil since fontconfig doesn't need configuration
		if err != nil {
			t.Errorf("Expected nil error, got: %v", err)
		}
	})

	t.Run("already configured", func(t *testing.T) {
		// Fontconfig uses system defaults, no devgeta-managed config needed
		err := fc.SoftConfigure()

		// Should return nil since fontconfig doesn't need configuration
		if err != nil {
			t.Errorf("Expected nil error, got: %v", err)
		}
	})

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

// TestUninstall is skipped - not supported for system libraries
func TestUninstall(t *testing.T) {
	t.Skip("Uninstall is not supported for fontconfig - system library")

	// This test is skipped because:
	// - Fontconfig is a system library
	// - Uninstalling system libraries can break system functionality
	// - Managed at OS level, not by devgeta
}

// TestExecuteCommand verifies fontconfig command execution
func TestExecuteCommand(t *testing.T) {
	t.Run("fc-cache command", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		fc := &FontConfig{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}
		mockApp.Base.SetExecCommandResult("", "", nil)

		err := fc.ExecuteCommand("fc-cache", "-fv")

		if err != nil {
			t.Fatalf("ExecuteCommand() failed: %v", err)
		}

		// Verify command was executed
		if mockApp.Base.GetExecCommandCallCount() != 1 {
			t.Errorf("Expected 1 ExecCommand call, got %d", mockApp.Base.GetExecCommandCallCount())
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall.Command != "fc-cache" {
			t.Errorf("Expected command fc-cache, got %s", lastCall.Command)
		}
	})

	t.Run("fc-list command", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		fc := &FontConfig{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}
		mockApp.Base.SetExecCommandResult("Font list output", "", nil)

		err := fc.ExecuteCommand("fc-list")

		if err != nil {
			t.Fatalf("ExecuteCommand() failed: %v", err)
		}

		if mockApp.Base.GetExecCommandCallCount() != 1 {
			t.Errorf("Expected 1 ExecCommand call, got %d", mockApp.Base.GetExecCommandCallCount())
		}
	})

	t.Run("fc-match command", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		fc := &FontConfig{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}
		mockApp.Base.SetExecCommandResult("", "", nil)

		err := fc.ExecuteCommand("fc-match", "monospace")

		if err != nil {
			t.Fatalf("ExecuteCommand() failed: %v", err)
		}
	})

	t.Run("fc-pattern command", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		fc := &FontConfig{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}
		mockApp.Base.SetExecCommandResult("", "", nil)

		err := fc.ExecuteCommand("fc-pattern", "sans-serif")

		if err != nil {
			t.Fatalf("ExecuteCommand() failed: %v", err)
		}
	})

	t.Run("unsupported command", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		fc := &FontConfig{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}

		err := fc.ExecuteCommand("invalid-command")

		if err == nil {
			t.Fatal("Expected error for unsupported command")
		}

		// Should not call ExecCommand for unsupported commands
		if mockApp.Base.GetExecCommandCallCount() != 0 {
			t.Errorf("Expected 0 ExecCommand calls, got %d", mockApp.Base.GetExecCommandCallCount())
		}
	})

	t.Run("empty command", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		fc := &FontConfig{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}

		err := fc.ExecuteCommand("")

		if err == nil {
			t.Fatal("Expected error for empty command")
		}

		// Should validate before calling ExecCommand
		if mockApp.Base.GetExecCommandCallCount() != 0 {
			t.Errorf("Expected 0 ExecCommand calls, got %d", mockApp.Base.GetExecCommandCallCount())
		}
	})

	t.Run("error handling", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		fc := &FontConfig{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}
		mockApp.Base.SetExecCommandResult("", "error output", fmt.Errorf("command failed"))

		err := fc.ExecuteCommand("fc-cache")

		if err == nil {
			t.Fatal("Expected error from failed command execution")
		}
	})
}

// TestUpdate is skipped - not implemented
func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	fc := &FontConfig{
		Cmd:  mockApp.Cmd,
		Base: mockApp.Base,
	}

	err := fc.Update()

	if err == nil {
		t.Fatal("Expected error from Update()")
	}
}
