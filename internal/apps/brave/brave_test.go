package brave

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
	brave := New()

	if brave == nil {
		t.Fatal("New() returned nil")
	}

	if brave.Cmd == nil {
		t.Error("Expected Cmd to be initialized")
	}
}

func TestNameAndKind(t *testing.T) {
	b := &Brave{}
	if b.Name() != constants.Brave {
		t.Errorf("expected Name() %q, got %q", constants.Brave, b.Name())
	}
	if b.Kind() != apps.KindDesktop {
		t.Errorf("expected Kind() KindDesktop, got %v", b.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	brave := &Brave{Cmd: mockApp.Cmd}

	err := brave.Install()
	if err != nil {
		t.Fatalf("Install() failed: %v", err)
	}

	if mockApp.Cmd.InstalledDesktopApp != "brave-browser" {
		t.Errorf("Expected InstalledDesktopApp to be 'brave-browser', got '%s'", mockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	testutil.IsolateXDGDirs(t)

	brave := &Brave{Cmd: tc.MockApp.Cmd}

	if err := brave.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() should succeed even when uninstall is not supported: %v", err)
	}
	if tc.MockApp.Cmd.InstalledDesktopApp != "brave-browser" {
		t.Errorf("expected Install to be called, got InstalledDesktopApp=%q", tc.MockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	brave := &Brave{Cmd: mockApp.Cmd}

	t.Run("installs when not present", func(t *testing.T) {
		mockApp.Reset()
		mockApp.Cmd.DesktopAppInstalled = false

		err := brave.SoftInstall()
		if err != nil {
			t.Fatalf("SoftInstall() failed: %v", err)
		}

		if mockApp.Cmd.MaybeInstalledDesktop != "brave-browser" {
			t.Errorf("Expected MaybeInstalledDesktop to be 'brave-browser', got '%s'", mockApp.Cmd.MaybeInstalledDesktop)
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})

	t.Run("skips when already present", func(t *testing.T) {
		mockApp.Reset()
		mockApp.Cmd.DesktopAppInstalled = true

		err := brave.SoftInstall()
		if err != nil {
			t.Fatalf("SoftInstall() failed: %v", err)
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})
}

func TestForceConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	brave := &Brave{Cmd: tc.MockApp.Cmd}

	err := brave.ForceConfigure()
	if err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	mockApp := testutil.NewMockApp()
	brave := &Brave{Cmd: mockApp.Cmd}

	err := brave.SoftConfigure()
	if err != nil {
		t.Fatalf("SoftConfigure() failed: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUninstall(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		app := &Brave{Cmd: tc.MockApp.Cmd}
		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall() failed: %v", err)
		}
		if tc.MockApp.Cmd.UninstalledDesktopApp != constants.BraveBrowser {
			t.Errorf("expected UninstalledDesktopApp=%q, got %q", constants.BraveBrowser, tc.MockApp.Cmd.UninstalledDesktopApp)
		}
		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})

	t.Run("binary removal failure", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Cmd.UninstallError = errors.New("brew error")
		app := &Brave{Cmd: tc.MockApp.Cmd}
		if err := app.Uninstall(); err == nil {
			t.Fatal("expected error when binary removal fails")
		}
	})
}

func TestExecuteCommand(t *testing.T) {
	mockApp := testutil.NewMockApp()
	brave := &Brave{Cmd: mockApp.Cmd}

	err := brave.ExecuteCommand("--version")
	if err != nil {
		t.Fatalf("ExecuteCommand() failed: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	brave := &Brave{Cmd: mockApp.Cmd}

	err := brave.Update()
	if err == nil {
		t.Fatal("Expected Update() to return error")
	}
	if !errors.Is(err, apps.ErrUpdateNotSupported) {
		t.Errorf("expected ErrUpdateNotSupported, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}
