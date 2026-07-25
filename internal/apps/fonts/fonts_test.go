package fonts

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
	f := New()
	if f == nil {
		t.Fatal("New() returned nil")
	}
	if f.Cmd == nil {
		t.Fatal("New() did not initialize command")
	}
}

func TestNameAndKind(t *testing.T) {
	f := &Fonts{}
	if f.Name() != constants.Fonts {
		t.Errorf("expected Name() %q, got %q", constants.Fonts, f.Name())
	}
	if f.Kind() != apps.KindFont {
		t.Errorf("expected Kind() KindFont, got %v", f.Kind())
	}
}

func TestInstallFont(t *testing.T) {
	mockApp := testutil.NewMockApp()
	f := &Fonts{Cmd: mockApp.Cmd}

	fontName := "font-hack-nerd-font"
	if err := f.InstallFont(fontName); err != nil {
		t.Fatalf("InstallFont() failed: %v", err)
	}
	if mockApp.Cmd.InstalledDesktopApp != fontName {
		t.Errorf("Expected font %s, got %s", fontName, mockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceInstallFont(t *testing.T) {
	mockApp := testutil.NewMockApp()
	f := &Fonts{Cmd: mockApp.Cmd, Base: mockApp.Base}

	fontName := "font-hack-nerd-font"
	if err := f.ForceInstallFont(fontName); err != nil {
		t.Fatalf("ForceInstallFont() should succeed even when uninstall is not supported: %v", err)
	}
	if mockApp.Cmd.InstalledDesktopApp != fontName {
		t.Errorf("Expected Install to be called for %s, got %s", fontName, mockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestSoftInstallFont(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = true
	f := &Fonts{Cmd: mockApp.Cmd, Base: mockApp.Base}

	fontName := "font-meslo-lg-nerd-font"
	if err := f.SoftInstallFont(fontName); err != nil {
		t.Fatalf("SoftInstallFont() failed: %v", err)
	}
	if mockApp.Cmd.FontName != fontName {
		t.Errorf("Expected font %s, got %s", fontName, mockApp.Cmd.FontName)
	}
	if mockApp.Cmd.FontURL != "" {
		t.Errorf("Expected empty URL, got %s", mockApp.Cmd.FontURL)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUninstallFont(t *testing.T) {
	mockApp := testutil.NewMockApp()
	f := &Fonts{Cmd: mockApp.Cmd, Base: mockApp.Base}

	err := f.UninstallFont("font-hack-nerd-font")
	if err == nil {
		t.Fatal("expected UninstallFont to return error for unsupported operation")
	}
	if !errors.Is(err, apps.ErrUninstallNotSupported) {
		t.Errorf("expected ErrUninstallNotSupported, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestAvailable(t *testing.T) {
	mockApp := testutil.NewMockApp()
	f := &Fonts{Cmd: mockApp.Cmd}

	available := f.Available()
	expectedFonts := []string{
		"font-hack-nerd-font",
		"font-meslo-lg-nerd-font",
		"font-caskaydia-mono-nerd-font",
		"font-fira-mono-nerd-font",
		"font-jetbrains-mono-nerd-font",
	}

	if len(available) != len(expectedFonts) {
		t.Fatalf("Expected %d fonts, got %d", len(expectedFonts), len(available))
	}
	for i, expected := range expectedFonts {
		if available[i] != expected {
			t.Errorf("Expected font %s at index %d, got %s", expected, i, available[i])
		}
	}
}
