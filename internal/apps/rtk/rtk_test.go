package rtk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/downloader"
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

func TestNameAndKind(t *testing.T) {
	a := &Rtk{}
	if a.Name() != constants.Rtk {
		t.Errorf("expected Name() %q, got %q", constants.Rtk, a.Name())
	}
	if a.Kind() != apps.KindTerminal {
		t.Errorf("expected Kind() KindTerminal, got %v", a.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = true // simulate macOS so test uses Homebrew path
	app := &Rtk{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mockApp.Cmd.InstalledPkg != "rtk" {
		t.Fatalf("expected InstallPackage(%s), got %q", "rtk", mockApp.Cmd.InstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestInstallDebian(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false // simulate Debian/Linux
	// Both ExecCommand calls (tar + install) succeed
	mockApp.Base.SetExecCommandResult("", "", nil)

	dl := testutil.ChecksumAwareDownloadFn(t)
	app := &Rtk{
		Cmd:  mockApp.Cmd,
		Base: mockApp.Base,
		fetchVersion: func(owner, repo string) (string, error) {
			if owner != "rtk-ai" || repo != "rtk" {
				t.Errorf("unexpected version fetch: owner=%s repo=%s", owner, repo)
			}
			return "0.43.0", nil
		},
		downloadFn: func(ctx context.Context, url, dest string, cfg downloader.RetryConfig) error {
			if !strings.Contains(url, "v0.43.0") {
				t.Errorf("download URL does not contain tag: %s", url)
			}
			if !strings.Contains(url, "linux") && !strings.Contains(url, "checksums.txt") {
				t.Errorf("download URL is not a Linux artifact: %s", url)
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
	app := &Rtk{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}
	if tc.MockApp.Cmd.InstalledPkg != constants.Rtk {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = true // macOS path uses MaybeInstallPackage
	app := &Rtk{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mockApp.Cmd.MaybeInstalled != "rtk" {
		t.Fatalf("expected MaybeInstallPackage(%s), got %q", "rtk", mockApp.Cmd.MaybeInstalled)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestSoftInstallDebian_AlreadyInstalled(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false

	orig := commands.LookPathFn
	commands.LookPathFn = func(string) (string, error) { return "/usr/local/bin/rtk", nil }
	defer func() { commands.LookPathFn = orig }()

	app := &Rtk{Cmd: mockApp.Cmd, Base: mockApp.Base}
	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall (already installed) error: %v", err)
	}
	// No install commands should have run
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

	app := &Rtk{
		Cmd:          mockApp.Cmd,
		Base:         mockApp.Base,
		fetchVersion: func(_, _ string) (string, error) { return "0.43.0", nil },
		downloadFn:   testutil.ChecksumAwareDownloadFn(t),
	}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall (not installed) error: %v", err)
	}
	// Should have run tar + sudo install
	if mockApp.Base.GetExecCommandCallCount() != 2 {
		t.Fatalf("expected 2 ExecCommand calls, got %d", mockApp.Base.GetExecCommandCallCount())
	}
}

func TestUninstall(t *testing.T) {
	t.Run("macOS", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Base.IsMacResult = true
		app := &Rtk{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall error: %v", err)
		}
		if tc.MockApp.Cmd.UninstalledPkg != constants.Rtk {
			t.Errorf(
				"expected UninstallPackage(%s), got %q",
				constants.Rtk,
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
		app := &Rtk{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall error: %v", err)
		}

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
		if len(lastCall.Args) < 2 || lastCall.Args[1] != "/usr/local/bin/rtk" {
			t.Errorf("expected /usr/local/bin/rtk in args, got %v", lastCall.Args)
		}
	})
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Rtk{Cmd: mockApp.Cmd, Base: mockApp.Base}

	err := app.Update()
	if err == nil {
		t.Fatal("expected Update to return error")
	}
	if !errors.Is(err, apps.ErrUpdateNotSupported) {
		t.Errorf("expected ErrUpdateNotSupported, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &Rtk{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	// First SoftConfigure records rtk in the global config
	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure error: %v", err)
	}

	// Second call is a no-op (already recorded) and must not error
	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure should not error on second call: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestExecuteCommand(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Rtk{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("successful execution", func(t *testing.T) {
		mockApp.Base.SetExecCommandResult("rtk 0.43.0", "", nil)

		err := app.ExecuteCommand("--version")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}
		if lastCall.Command != "rtk" {
			t.Fatalf("Expected command 'rtk', got %q", lastCall.Command)
		}
		if len(lastCall.Args) != 1 || lastCall.Args[0] != "--version" {
			t.Fatalf("Expected args ['--version'], got %v", lastCall.Args)
		}
		if lastCall.IsSudo {
			t.Fatal("Expected IsSudo to be false")
		}
	})

	t.Run("command execution error", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult(
			"",
			"command not found",
			fmt.Errorf("command not found: rtk"),
		)

		err := app.ExecuteCommand("gain")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}
		if !strings.Contains(err.Error(), "failed to run rtk command") {
			t.Fatalf("Expected error to contain 'failed to run rtk command', got: %v", err)
		}
	})
}

func TestUninstallClearsClaudeHookOptIn(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatal(err)
	}
	if err := gc.Load(); err != nil {
		t.Fatal(err)
	}
	gc.Integrations.RtkClaudeHook = true
	if err := gc.Save(); err != nil {
		t.Fatal(err)
	}

	tc.MockApp.Base.IsMacResult = true
	app := &Rtk{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}

	after := &config.GlobalConfig{}
	if err := after.Load(); err != nil {
		t.Fatal(err)
	}
	if after.Integrations.RtkClaudeHook {
		t.Error("expected Uninstall to clear the rtk Claude hook opt-in")
	}
}

func TestInitAgentMethods(t *testing.T) {
	for _, tt := range []struct {
		name     string
		call     func(r *Rtk) error
		wantArgs string
	}{
		{"InitClaude", func(r *Rtk) error { return r.InitClaude() }, "init -g --auto-patch"},
		{"InitOpenCode", func(r *Rtk) error { return r.InitOpenCode() }, "init -g --opencode"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mockApp := testutil.NewMockApp()
			mockApp.Base.SetExecCommandResult("", "", nil)
			app := &Rtk{Cmd: mockApp.Cmd, Base: mockApp.Base}

			if err := tt.call(app); err != nil {
				t.Fatalf("%s error: %v", tt.name, err)
			}
			lastCall := mockApp.Base.GetLastExecCommandCall()
			if lastCall == nil {
				t.Fatal("no ExecCommand call recorded")
			}
			if lastCall.Command != "rtk" || strings.Join(lastCall.Args, " ") != tt.wantArgs {
				t.Errorf("expected `rtk %s`, got %q %v", tt.wantArgs, lastCall.Command, lastCall.Args)
			}
			// Streaming is load-bearing: rtk init may show a one-time
			// interactive consent prompt; a captured (non-streamed) run
			// hides the prompt and hangs forever on a terminal.
			if !lastCall.Stream {
				t.Error("rtk init must run with Stream: true")
			}
		})
	}
}
