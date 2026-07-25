package flameshot

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
	flameshot := New()

	if flameshot == nil {
		t.Fatal("New() returned nil")
	}

	if flameshot.Cmd == nil {
		t.Error("Expected Cmd to be initialized")
	}
}

func TestNameAndKind(t *testing.T) {
	f := &Flameshot{}
	if f.Name() != constants.Flameshot {
		t.Errorf("expected Name() %q, got %q", constants.Flameshot, f.Name())
	}
	if f.Kind() != apps.KindDesktop {
		t.Errorf("expected Kind() KindDesktop, got %v", f.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	flameshot := &Flameshot{Cmd: mockApp.Cmd}

	err := flameshot.Install()
	if err != nil {
		t.Fatalf("Install() failed: %v", err)
	}

	if mockApp.Cmd.InstalledDesktopApp == "" {
		t.Error("Expected Flameshot to be installed")
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &Flameshot{Cmd: tc.MockApp.Cmd}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}
	if tc.MockApp.Cmd.InstalledDesktopApp != constants.Flameshot {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	flameshot := &Flameshot{Cmd: mockApp.Cmd}

	t.Run("installs when not present", func(t *testing.T) {
		mockApp.Reset()
		mockApp.Cmd.DesktopAppInstalled = false

		err := flameshot.SoftInstall()
		if err != nil {
			t.Fatalf("SoftInstall() failed: %v", err)
		}

		if mockApp.Cmd.MaybeInstalledDesktop == "" {
			t.Error("Expected MaybeInstallDesktopApp to be called")
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})

	t.Run("skips when already present", func(t *testing.T) {
		mockApp.Reset()
		mockApp.Cmd.DesktopAppInstalled = true

		err := flameshot.SoftInstall()
		if err != nil {
			t.Fatalf("SoftInstall() failed: %v", err)
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})
}

func TestForceConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	flameshot := &Flameshot{Cmd: tc.MockApp.Cmd}
	if err := flameshot.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}
	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	mockApp := testutil.NewMockApp()
	flameshot := &Flameshot{Cmd: mockApp.Cmd}

	err := flameshot.SoftConfigure()
	if err != nil {
		t.Fatalf("SoftConfigure() failed: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUninstall(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		app := &Flameshot{Cmd: tc.MockApp.Cmd}
		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall() failed: %v", err)
		}
		if tc.MockApp.Cmd.UninstalledDesktopApp != constants.Flameshot {
			t.Errorf("expected UninstalledDesktopApp=%q, got %q", constants.Flameshot, tc.MockApp.Cmd.UninstalledDesktopApp)
		}
		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})

	t.Run("binary removal failure", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Cmd.UninstallError = errors.New("brew error")
		app := &Flameshot{Cmd: tc.MockApp.Cmd}
		if err := app.Uninstall(); err == nil {
			t.Fatal("expected error when binary removal fails")
		}
	})
}

func TestExecuteCommand(t *testing.T) {
	mockApp := testutil.NewMockApp()
	flameshot := &Flameshot{Cmd: mockApp.Cmd}

	err := flameshot.ExecuteCommand("--version")
	if err != nil {
		t.Fatalf("ExecuteCommand() failed: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	flameshot := &Flameshot{Cmd: mockApp.Cmd}

	err := flameshot.Update()
	if err == nil {
		t.Fatal("Expected Update() to return error")
	}
	if !errors.Is(err, apps.ErrUpdateNotSupported) {
		t.Errorf("expected ErrUpdateNotSupported, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}
