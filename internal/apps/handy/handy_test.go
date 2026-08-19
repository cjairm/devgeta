package handy

import (
	"errors"
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
	h := &Handy{}
	if h.Name() != constants.Handy {
		t.Errorf("expected Name() %q, got %q", constants.Handy, h.Name())
	}
	if h.Kind() != apps.KindDesktop {
		t.Errorf("expected Kind() KindDesktop, got %v", h.Kind())
	}
}

func TestInstall(t *testing.T) {
	t.Run("macOS", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.IsMacResult = true
		app := &Handy{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.Install(); err != nil {
			t.Fatalf("Install error: %v", err)
		}
		if mockApp.Cmd.InstalledDesktopApp != constants.Handy {
			t.Fatalf(
				"expected InstallDesktopApp(%s), got %q",
				constants.Handy,
				mockApp.Cmd.InstalledDesktopApp,
			)
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})

	t.Run("Linux", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.IsMacResult = false
		app := &Handy{Cmd: mockApp.Cmd, Base: mockApp.Base}

		err := app.Install()
		if err == nil {
			t.Fatal("expected error for Linux installation guide")
		}
		if err.Error() != "manual installation required for Linux" {
			t.Errorf("unexpected error message: %v", err)
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})
}

func TestSoftInstall(t *testing.T) {
	t.Run("macOS", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.IsMacResult = true
		app := &Handy{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("SoftInstall error: %v", err)
		}
		if mockApp.Cmd.MaybeInstalledDesktop != constants.Handy {
			t.Fatalf(
				"expected MaybeInstallDesktopApp(%s), got %q",
				constants.Handy,
				mockApp.Cmd.MaybeInstalledDesktop,
			)
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})

	t.Run("Linux - not installed", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.IsMacResult = false
		mockApp.Base.SetExecCommandResult("", "command not found", errors.New("exit 1"))
		app := &Handy{Cmd: mockApp.Cmd, Base: mockApp.Base}

		err := app.SoftInstall()
		if err == nil {
			t.Fatal("expected error for Linux installation guide")
		}
	})

	t.Run("Linux - already installed", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.IsMacResult = false
		mockApp.Base.SetExecCommandResult("/usr/bin/handy", "", nil)
		app := &Handy{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("unexpected error when handy already installed: %v", err)
		}
	})
}

func TestForceInstall(t *testing.T) {
	t.Run("macOS", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Base.IsMacResult = true
		app := &Handy{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.ForceInstall(); err != nil {
			t.Fatalf("ForceInstall() error: %v", err)
		}
		if tc.MockApp.Cmd.InstalledDesktopApp != constants.Handy {
			t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledDesktopApp)
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})
}

func TestUninstall(t *testing.T) {
	t.Run("macOS success", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Base.IsMacResult = true
		app := &Handy{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall() failed: %v", err)
		}
		if tc.MockApp.Cmd.UninstalledDesktopApp != constants.Handy {
			t.Errorf(
				"expected UninstalledDesktopApp=%q, got %q",
				constants.Handy,
				tc.MockApp.Cmd.UninstalledDesktopApp,
			)
		}
		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})

	t.Run("macOS binary removal failure", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Base.IsMacResult = true
		tc.MockApp.Cmd.UninstallError = errors.New("brew error")
		app := &Handy{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
		if err := app.Uninstall(); err == nil {
			t.Fatal("expected error when binary removal fails")
		}
	})

	t.Run("Linux returns manual error", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.IsMacResult = false
		app := &Handy{Cmd: mockApp.Cmd, Base: mockApp.Base}

		err := app.Uninstall()
		if err == nil {
			t.Fatal("expected error for Linux manual uninstallation")
		}
	})
}

func TestForceConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &Handy{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Handy{Cmd: mockApp.Cmd}

	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure error: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestExecuteCommand(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Handy{Cmd: mockApp.Cmd, Base: mockApp.Base}

	err := app.ExecuteCommand("test")
	if err == nil {
		t.Fatal("expected ExecuteCommand to return error")
	}
	if !errors.Is(err, apps.ErrExecuteNotSupported) {
		t.Errorf("expected ErrExecuteNotSupported, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Handy{Cmd: mockApp.Cmd}

	err := app.Update()
	if err == nil {
		t.Fatal("expected Update to return error")
	}
	if !errors.Is(err, apps.ErrUpdateNotSupported) {
		t.Errorf("expected ErrUpdateNotSupported, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}
