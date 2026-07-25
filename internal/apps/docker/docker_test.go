package docker

import (
	"errors"
	"fmt"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
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
	d := &Docker{}
	if d.Name() != constants.Docker {
		t.Errorf("expected Name() %q, got %q", constants.Docker, d.Name())
	}
	if d.Kind() != apps.KindDesktop {
		t.Errorf("expected Kind() KindDesktop, got %v", d.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Docker{Cmd: mockApp.Cmd}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mockApp.Cmd.InstalledDesktopApp != constants.Docker {
		t.Fatalf("expected InstallDesktopApp(%s), got %q", constants.Docker, mockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &Docker{Cmd: tc.MockApp.Cmd}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}
	if tc.MockApp.Cmd.InstalledDesktopApp != constants.Docker {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Docker{Cmd: mockApp.Cmd}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mockApp.Cmd.MaybeInstalledDesktop != constants.Docker {
		t.Fatalf("expected MaybeInstallDesktopApp(%s), got %q", constants.Docker, mockApp.Cmd.MaybeInstalledDesktop)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUninstall(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		app := &Docker{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall() failed: %v", err)
		}
		if tc.MockApp.Cmd.UninstalledDesktopApp != constants.Docker {
			t.Errorf("expected UninstalledDesktopApp=%q, got %q", constants.Docker, tc.MockApp.Cmd.UninstalledDesktopApp)
		}
		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})

	t.Run("binary removal failure", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Cmd.UninstallError = errors.New("brew error")
		app := &Docker{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
		if err := app.Uninstall(); err == nil {
			t.Fatal("expected error when binary removal fails")
		}
	})
}

func TestForceConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &Docker{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Docker{Cmd: mockApp.Cmd}

	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure error: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestExecuteCommand(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Docker{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("successful execution", func(t *testing.T) {
		mockApp.Base.SetExecCommandResult("Docker version 24.0.0", "", nil)

		if err := app.ExecuteCommand("--version"); err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		if mockApp.Base.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 ExecCommand call, got %d", mockApp.Base.GetExecCommandCallCount())
		}
		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}
		if lastCall.Command != constants.Docker {
			t.Fatalf("Expected command %s, got %q", constants.Docker, lastCall.Command)
		}
	})

	t.Run("error handling", func(t *testing.T) {
		mockApp.Reset()
		mockApp.Base.SetExecCommandResult("", "command failed", fmt.Errorf("exit status 1"))

		if err := app.ExecuteCommand("--invalid"); err == nil {
			t.Fatal("Expected error from ExecuteCommand")
		}
	})
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Docker{Cmd: mockApp.Cmd}

	err := app.Update()
	if err == nil {
		t.Fatal("expected Update to return error")
	}
	if !errors.Is(err, apps.ErrUpdateNotSupported) {
		t.Errorf("expected ErrUpdateNotSupported, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}
