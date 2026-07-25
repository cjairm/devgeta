package aerospace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
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
	a := &Aerospace{}
	if a.Name() != constants.Aerospace {
		t.Errorf("expected Name() %q, got %q", constants.Aerospace, a.Name())
	}
	if a.Kind() != apps.KindDesktop {
		t.Errorf("expected Kind() KindDesktop, got %v", a.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Aerospace{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mockApp.Cmd.InstalledDesktopApp != "nikitabobko/tap/aerospace" {
		t.Fatalf("expected InstallDesktopApp(nikitabobko/tap/aerospace), got %q", mockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	oldCfg := paths.Paths.Config.Aerospace
	t.Cleanup(func() { paths.Paths.Config.Aerospace = oldCfg })
	paths.Paths.Config.Aerospace = filepath.Join(tc.ConfigDir, "aerospace")

	app := &Aerospace{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}
	if tc.MockApp.Cmd.InstalledDesktopApp != "nikitabobko/tap/aerospace" {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Aerospace{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mockApp.Cmd.MaybeInstalledDesktop != "nikitabobko/tap/aerospace" {
		t.Fatalf("expected MaybeInstallDesktopApp(nikitabobko/tap/aerospace), got %q", mockApp.Cmd.MaybeInstalledDesktop)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUninstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	oldCfg := paths.Paths.Config.Aerospace
	t.Cleanup(func() { paths.Paths.Config.Aerospace = oldCfg })
	paths.Paths.Config.Aerospace = filepath.Join(tc.ConfigDir, "aerospace")

	app := &Aerospace{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}
	if tc.MockApp.Cmd.UninstalledDesktopApp != "nikitabobko/tap/aerospace" {
		t.Errorf("expected UninstallDesktopApp(nikitabobko/tap/aerospace), got %q", tc.MockApp.Cmd.UninstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Aerospace{Cmd: mockApp.Cmd, Base: mockApp.Base}

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
	testutil.IsolateXDGDirs(t)

	src := filepath.Join(tc.AppDir, "aerospace")
	dst := filepath.Join(tc.ConfigDir, "aerospace")

	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}

	oldAppDir, oldLocalDir := paths.Paths.App.Configs.Aerospace, paths.Paths.Config.Aerospace
	paths.Paths.App.Configs.Aerospace, paths.Paths.Config.Aerospace = src, dst
	t.Cleanup(func() {
		paths.Paths.App.Configs.Aerospace, paths.Paths.Config.Aerospace = oldAppDir, oldLocalDir
	})

	originalContent := "[workspace]\nkey = \"value\""
	if err := os.WriteFile(filepath.Join(src, "aerospace.toml"), []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	app := &Aerospace{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	check := filepath.Join(dst, "aerospace.toml")
	if _, err := os.Stat(check); err != nil {
		t.Fatalf("expected copied file at %s: %v", check, err)
	}

	copiedContent, err := os.ReadFile(check)
	if err != nil {
		t.Fatalf("failed to read copied file: %v", err)
	}
	if string(copiedContent) != originalContent {
		t.Fatalf("content mismatch: expected %q, got %q", originalContent, string(copiedContent))
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	src := filepath.Join(tc.AppDir, "aerospace")
	dst := filepath.Join(tc.ConfigDir, "aerospace")

	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}

	oldAppDir, oldLocalDir := paths.Paths.App.Configs.Aerospace, paths.Paths.Config.Aerospace
	paths.Paths.App.Configs.Aerospace, paths.Paths.Config.Aerospace = src, dst
	t.Cleanup(func() {
		paths.Paths.App.Configs.Aerospace, paths.Paths.Config.Aerospace = oldAppDir, oldLocalDir
	})

	originalContent := "[workspace]\nkey = \"value\""
	if err := os.WriteFile(filepath.Join(src, "aerospace.toml"), []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	app := &Aerospace{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure error: %v", err)
	}

	check := filepath.Join(dst, "aerospace.toml")
	if _, err := os.Stat(check); err != nil {
		t.Fatalf("expected copied file at %s: %v", check, err)
	}

	modifiedContent := "[workspace]\nkey = \"modified\""
	if err := os.WriteFile(check, []byte(modifiedContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("second SoftConfigure error: %v", err)
	}

	finalContent, err := os.ReadFile(check)
	if err != nil {
		t.Fatalf("failed to read file after second configure: %v", err)
	}
	if string(finalContent) != modifiedContent {
		t.Fatalf("SoftConfigure should not overwrite existing file")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}
