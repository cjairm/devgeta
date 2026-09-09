package shottr

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
	s := &Shottr{}
	if s.Name() != constants.Shottr {
		t.Errorf("expected Name() %q, got %q", constants.Shottr, s.Name())
	}
	if s.Kind() != apps.KindDesktop {
		t.Errorf("expected Kind() KindDesktop, got %v", s.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Shottr{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("mac installs the cask", func(t *testing.T) {
		mockApp.Reset()
		mockApp.Base.IsMacResult = true

		if err := app.Install(); err != nil {
			t.Fatalf("Install error: %v", err)
		}
		if mockApp.Cmd.InstalledDesktopApp != constants.Shottr {
			t.Fatalf(
				"expected InstallDesktopApp(%s), got %q",
				constants.Shottr,
				mockApp.Cmd.InstalledDesktopApp,
			)
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})

	t.Run("linux reports unavailable instead of attempting an install", func(t *testing.T) {
		mockApp.Reset()
		mockApp.Base.IsMacResult = false

		err := app.Install()
		if !errors.Is(err, apps.ErrUnsupportedPlatform) {
			t.Fatalf("expected ErrUnsupportedPlatform, got: %v", err)
		}
		if mockApp.Cmd.InstalledDesktopApp != "" || mockApp.Cmd.InstalledPkg != "" {
			t.Error("expected no install attempt on Linux")
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Shottr{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("mac installs the cask", func(t *testing.T) {
		mockApp.Reset()
		mockApp.Base.IsMacResult = true

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("SoftInstall error: %v", err)
		}
		if mockApp.Cmd.MaybeInstalledDesktop != constants.Shottr {
			t.Fatalf(
				"expected MaybeInstallDesktopApp(%s), got %q",
				constants.Shottr,
				mockApp.Cmd.MaybeInstalledDesktop,
			)
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})

	t.Run("linux reports unavailable instead of attempting an install", func(t *testing.T) {
		mockApp.Reset()
		mockApp.Base.IsMacResult = false

		err := app.SoftInstall()
		if !errors.Is(err, apps.ErrUnsupportedPlatform) {
			t.Fatalf("expected ErrUnsupportedPlatform, got: %v", err)
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	tc.MockApp.Base.IsMacResult = true
	app := &Shottr{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}
	if tc.MockApp.Cmd.InstalledDesktopApp != constants.Shottr {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestForceConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &Shottr{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}
	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Shottr{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure() failed: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUninstall(t *testing.T) {
	t.Run("mac uninstalls the desktop app", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Base.IsMacResult = true
		app := &Shottr{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall() failed: %v", err)
		}
		if tc.MockApp.Cmd.UninstalledDesktopApp != constants.Shottr {
			t.Errorf(
				"expected UninstalledDesktopApp=%q, got %q",
				constants.Shottr,
				tc.MockApp.Cmd.UninstalledDesktopApp,
			)
		}
		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})

	t.Run("linux reports unavailable", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Base.IsMacResult = false
		app := &Shottr{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
		err := app.Uninstall()
		if !errors.Is(err, apps.ErrUnsupportedPlatform) {
			t.Fatalf("expected ErrUnsupportedPlatform, got: %v", err)
		}
	})

	t.Run("binary removal failure", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Base.IsMacResult = true
		tc.MockApp.Cmd.UninstallError = errors.New("brew error")
		app := &Shottr{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
		if err := app.Uninstall(); err == nil {
			t.Fatal("expected error when binary removal fails")
		}
	})
}

func TestExecuteCommand(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Shottr{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.ExecuteCommand("--version"); err != nil {
		t.Fatalf("ExecuteCommand() failed: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Shottr{Cmd: mockApp.Cmd, Base: mockApp.Base}

	err := app.Update()
	if err == nil {
		t.Fatal("Expected Update() to return error")
	}
	if !errors.Is(err, apps.ErrUpdateNotSupported) {
		t.Errorf("expected ErrUpdateNotSupported, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}
