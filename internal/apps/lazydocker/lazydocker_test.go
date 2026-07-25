package lazydocker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/downloader"
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

func TestNameAndKind(t *testing.T) {
	a := &LazyDocker{}
	if a.Name() != constants.LazyDocker {
		t.Errorf("expected Name() %q, got %q", constants.LazyDocker, a.Name())
	}
	if a.Kind() != apps.KindTerminal {
		t.Errorf("expected Kind() KindTerminal, got %v", a.Kind())
	}
}

var expectedPackageName = fmt.Sprintf(
	"jesseduffield/%s/%s",
	constants.LazyDocker,
	constants.LazyDocker,
)

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = true // simulate macOS so test uses Homebrew path
	app := &LazyDocker{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mockApp.Cmd.InstalledPkg != expectedPackageName {
		t.Fatalf(
			"expected InstallPackage(%s), got %q",
			expectedPackageName,
			mockApp.Cmd.InstalledPkg,
		)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestInstallDebian(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false // simulate Debian/Linux
	// Both ExecCommand calls (tar + install) succeed
	mockApp.Base.SetExecCommandResult("", "", nil)

	dl := testutil.ChecksumAwareDownloadFn(t)
	app := &LazyDocker{
		Cmd:  mockApp.Cmd,
		Base: mockApp.Base,
		fetchVersion: func(owner, repo string) (string, error) {
			if owner != "jesseduffield" || repo != "lazydocker" {
				t.Errorf("unexpected version fetch: owner=%s repo=%s", owner, repo)
			}
			return "0.23.1", nil
		},
		downloadFn: func(ctx context.Context, url, dest string, cfg downloader.RetryConfig) error {
			if !strings.Contains(url, "0.23.1") {
				t.Errorf("download URL does not contain version: %s", url)
			}
			return dl(ctx, url, dest, cfg)
		},
	}

	if err := app.Install(); err != nil {
		t.Fatalf("Install (Debian) error: %v", err)
	}

	// Expect 2 ExecCommand calls: tar (extract) + sudo install
	if mockApp.Base.GetExecCommandCallCount() != 2 {
		t.Fatalf("expected 2 ExecCommand calls, got %d", mockApp.Base.GetExecCommandCallCount())
	}
	calls := mockApp.Base.ExecCommandCalls
	if calls[0].Command != "tar" {
		t.Errorf("expected first command 'tar', got %q", calls[0].Command)
	}
	if calls[1].Command != "install" || !calls[1].IsSudo {
		t.Errorf("expected second command 'install' with IsSudo=true, got command=%q IsSudo=%v",
			calls[1].Command, calls[1].IsSudo)
	}
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	tc.MockApp.Base.IsMacResult = true // macOS path: brew uninstall
	app := &LazyDocker{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}
	if tc.MockApp.Cmd.InstalledPkg != expectedPackageName {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = true // macOS path uses MaybeInstallPackage
	app := &LazyDocker{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mockApp.Cmd.MaybeInstalled != expectedPackageName {
		t.Fatalf(
			"expected MaybeInstallPackage(%s), got %q",
			expectedPackageName,
			mockApp.Cmd.MaybeInstalled,
		)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestSoftInstallDebian_AlreadyInstalled(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false

	orig := commands.LookPathFn
	commands.LookPathFn = func(string) (string, error) { return "/usr/local/bin/lazydocker", nil }
	defer func() { commands.LookPathFn = orig }()

	app := &LazyDocker{Cmd: mockApp.Cmd, Base: mockApp.Base}
	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall (already installed) error: %v", err)
	}
	if mockApp.Base.GetExecCommandCallCount() != 0 {
		t.Fatalf("expected 0 ExecCommand calls, got %d", mockApp.Base.GetExecCommandCallCount())
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestSoftInstallDebian_NotInstalled(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false
	mockApp.Base.SetExecCommandResult("", "", nil)

	orig := commands.LookPathFn
	commands.LookPathFn = func(string) (string, error) { return "", fmt.Errorf("not found") }
	defer func() { commands.LookPathFn = orig }()

	app := &LazyDocker{
		Cmd:          mockApp.Cmd,
		Base:         mockApp.Base,
		fetchVersion: func(_, _ string) (string, error) { return "0.23.1", nil },
		downloadFn:   testutil.ChecksumAwareDownloadFn(t),
	}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall (not installed) error: %v", err)
	}
	if mockApp.Base.GetExecCommandCallCount() != 2 {
		t.Fatalf("expected 2 ExecCommand calls, got %d", mockApp.Base.GetExecCommandCallCount())
	}
}

func TestUninstall(t *testing.T) {
	t.Run("macOS", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Base.IsMacResult = true
		app := &LazyDocker{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall error: %v", err)
		}
		if tc.MockApp.Cmd.UninstalledPkg != expectedPackageName {
			t.Errorf(
				"expected UninstallPackage(%s), got %q",
				expectedPackageName,
				tc.MockApp.Cmd.UninstalledPkg,
			)
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})

	t.Run("linux", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Base.IsMacResult = false
		tc.MockApp.Base.SetExecCommandResult("", "", nil)
		app := &LazyDocker{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall error: %v", err)
		}

		// Linux path uses Base.ExecCommand directly for rm
		lastCall := tc.MockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("expected ExecCommand call for rm")
		}
		if lastCall.Command != "rm" || !lastCall.IsSudo {
			t.Errorf(
				"expected sudo rm, got command=%q IsSudo=%v",
				lastCall.Command,
				lastCall.IsSudo,
			)
		}
		if len(lastCall.Args) < 2 || lastCall.Args[1] != "/usr/local/bin/lazydocker" {
			t.Errorf("expected /usr/local/bin/lazydocker in args, got %v", lastCall.Args)
		}
	})
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &LazyDocker{Cmd: mockApp.Cmd, Base: mockApp.Base}

	err := app.Update()
	if err == nil {
		t.Fatal("expected Update to return error")
	}
	if !errors.Is(err, apps.ErrUpdateNotSupported) {
		t.Errorf("expected ErrUpdateNotSupported, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &LazyDocker{Cmd: tc.MockApp.Cmd}

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

	if !strings.Contains(string(content), "# LazyDocker enabled") {
		t.Error("Expected shell config to contain LazyDocker feature")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &LazyDocker{Cmd: tc.MockApp.Cmd}

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

	if !strings.Contains(string(content), "# LazyDocker enabled") {
		t.Error("Expected shell config to contain LazyDocker feature on first call")
	}

	// Second call should skip (feature already enabled)
	err = app.SoftConfigure()
	if err != nil {
		t.Fatalf("SoftConfigure should not error on second call: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestExecuteCommand(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &LazyDocker{Cmd: mockApp.Cmd, Base: mockApp.Base}

	// Test 1: Successful execution
	t.Run("successful execution", func(t *testing.T) {
		mockApp.Base.SetExecCommandResult("lazydocker version 0.20.0", "", nil)

		err := app.ExecuteCommand("--version")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		// Verify ExecCommand was called once
		if mockApp.Base.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 ExecCommand call, got %d", mockApp.Base.GetExecCommandCallCount())
		}

		// Verify command parameters
		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}
		if lastCall.Command != "lazydocker" {
			t.Fatalf("Expected command 'lazydocker', got %q", lastCall.Command)
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
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult(
			"",
			"command not found",
			fmt.Errorf("command not found: lazydocker"),
		)

		err := app.ExecuteCommand("--invalid-flag")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}
		if !strings.Contains(err.Error(), "failed to run lazydocker command") {
			t.Fatalf("Expected error to contain 'failed to run lazydocker command', got: %v", err)
		}

		// Verify the error was properly wrapped
		if !strings.Contains(err.Error(), "command not found: lazydocker") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})

	// Test 3: No arguments (launch TUI)
	t.Run("launch without arguments", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("TUI launched", "", nil)

		err := app.ExecuteCommand()
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if len(lastCall.Args) != 0 {
			t.Fatalf("Expected no args, got %v", lastCall.Args)
		}
	})
}
