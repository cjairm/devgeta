package ulauncher

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
	u := New()
	if u == nil {
		t.Fatal("New() returned nil")
	}
	if u.Cmd == nil {
		t.Fatal("Cmd is nil")
	}
}

func TestNameAndKind(t *testing.T) {
	u := &Ulauncher{}
	if u.Name() != constants.Ulauncher {
		t.Errorf("expected Name() %q, got %q", constants.Ulauncher, u.Name())
	}
	if u.Kind() != apps.KindDesktop {
		t.Errorf("expected Kind() KindDesktop, got %v", u.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	u := &Ulauncher{Cmd: mockApp.Cmd}

	err := u.Install()
	if err != nil {
		t.Fatalf("Install() failed: %v", err)
	}

	if mockApp.Cmd.InstalledDesktopApp != constants.Ulauncher {
		t.Errorf("Expected desktop app %s, got %s", constants.Ulauncher, mockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	u := &Ulauncher{Cmd: mockApp.Cmd}

	err := u.SoftInstall()
	if err != nil {
		t.Fatalf("SoftInstall() failed: %v", err)
	}

	if mockApp.Cmd.MaybeInstalledDesktop != constants.Ulauncher {
		t.Errorf("Expected desktop app %s, got %s", constants.Ulauncher, mockApp.Cmd.MaybeInstalledDesktop)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	tc.MockApp.Base.IsMacResult = false // Linux: Uninstall is a real operation
	u := &Ulauncher{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := u.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() should succeed: %v", err)
	}
	if tc.MockApp.Cmd.InstalledDesktopApp != constants.Ulauncher {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledDesktopApp)
	}
}

func TestUninstall(t *testing.T) {
	t.Run("linux success", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Base.IsMacResult = false
		app := &Ulauncher{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall() failed: %v", err)
		}
		if tc.MockApp.Cmd.UninstalledDesktopApp != constants.Ulauncher {
			t.Errorf("expected UninstalledDesktopApp=%q, got %q", constants.Ulauncher, tc.MockApp.Cmd.UninstalledDesktopApp)
		}
	})

	t.Run("macOS no-op", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Base.IsMacResult = true
		app := &Ulauncher{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall() on macOS should return nil: %v", err)
		}
		if tc.MockApp.Cmd.UninstalledDesktopApp != "" {
			t.Error("expected no uninstall on macOS")
		}
	})

	t.Run("binary removal failure on linux", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		tc.MockApp.Base.IsMacResult = false
		tc.MockApp.Cmd.UninstallError = errors.New("apt error")
		app := &Ulauncher{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
		if err := app.Uninstall(); err == nil {
			t.Fatal("expected error when binary removal fails")
		}
	})
}

func TestForceConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	u := &Ulauncher{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := u.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}
	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	mockApp := testutil.NewMockApp()
	u := &Ulauncher{Cmd: mockApp.Cmd}

	err := u.SoftConfigure()
	if err != nil {
		t.Fatalf("SoftConfigure() failed: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestExecuteCommand(t *testing.T) {
	mockApp := testutil.NewMockApp()
	u := &Ulauncher{Cmd: mockApp.Cmd}

	err := u.ExecuteCommand("--version")
	if err != nil {
		t.Fatalf("ExecuteCommand() failed: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	u := &Ulauncher{Cmd: mockApp.Cmd}

	err := u.Update()
	if err == nil {
		t.Fatal("Expected Update to return error")
	}
	if !errors.Is(err, apps.ErrUpdateNotSupported) {
		t.Errorf("expected ErrUpdateNotSupported, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}
