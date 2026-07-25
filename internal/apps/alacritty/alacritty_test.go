package alacritty

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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
	a := &Alacritty{}
	if a.Name() != constants.Alacritty {
		t.Errorf("expected Name() %q, got %q", constants.Alacritty, a.Name())
	}
	if a.Kind() != apps.KindTerminal {
		t.Errorf("expected Kind() KindTerminal, got %v", a.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Alacritty{Cmd: mockApp.Cmd}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mockApp.Cmd.InstalledDesktopApp != constants.Alacritty {
		t.Fatalf("expected InstallDesktopApp(%s), got %q", constants.Alacritty, mockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	oldAlacrittyConfig := paths.Paths.Config.Alacritty
	t.Cleanup(func() { paths.Paths.Config.Alacritty = oldAlacrittyConfig })
	paths.Paths.Config.Alacritty = filepath.Join(tc.ConfigDir, "alacritty")

	app := &Alacritty{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}
	if tc.MockApp.Cmd.InstalledDesktopApp != constants.Alacritty {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Alacritty{Cmd: mockApp.Cmd}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mockApp.Cmd.MaybeInstalledDesktop != constants.Alacritty {
		t.Fatalf("expected MaybeInstallDesktopApp(%s), got %q", constants.Alacritty, mockApp.Cmd.MaybeInstalledDesktop)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUninstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	oldAlacrittyConfig := paths.Paths.Config.Alacritty
	t.Cleanup(func() { paths.Paths.Config.Alacritty = oldAlacrittyConfig })
	paths.Paths.Config.Alacritty = filepath.Join(tc.ConfigDir, "alacritty")

	app := &Alacritty{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}
	if tc.MockApp.Cmd.UninstalledDesktopApp != constants.Alacritty {
		t.Errorf("expected UninstallDesktopApp(%s), got %q", constants.Alacritty, tc.MockApp.Cmd.UninstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Alacritty{Cmd: mockApp.Cmd, Base: mockApp.Base}

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

	tmplDir := filepath.Join(tc.AppDir, "alacritty")
	if err := os.MkdirAll(tmplDir, 0755); err != nil {
		t.Fatal(err)
	}

	tmplContent := `[env]
TERM = "xterm-256color"

[window]
opacity = 0.8

{{if eq .Font "default"}}
[font]
size = 13
{{end}}

{{if eq .Theme "default"}}
[colors.primary]
background = "0x282828"
{{end}}
`
	tmplPath := filepath.Join(tmplDir, "alacritty.toml.tmpl")
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0644); err != nil {
		t.Fatal(err)
	}

	starterContent := "#!/bin/bash\nzsh"
	starterPath := filepath.Join(tmplDir, "starter.sh")
	if err := os.WriteFile(starterPath, []byte(starterContent), 0755); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(tc.ConfigDir, "alacritty")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}

	oldAppConfig := paths.Paths.App.Configs.Alacritty
	oldLocalConfig := paths.Paths.Config.Alacritty
	oldConfigRoot := paths.Paths.Config.Root

	paths.Paths.App.Configs.Alacritty = tmplDir
	paths.Paths.Config.Alacritty = destDir
	paths.Paths.Config.Root = tc.ConfigDir

	t.Cleanup(func() {
		paths.Paths.App.Configs.Alacritty = oldAppConfig
		paths.Paths.Config.Alacritty = oldLocalConfig
		paths.Paths.Config.Root = oldConfigRoot
	})

	app := &Alacritty{Cmd: tc.MockApp.Cmd}

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	configPath := filepath.Join(destDir, "alacritty.toml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected generated file at %s: %v", configPath, err)
	}

	starterDest := filepath.Join(destDir, "starter.sh")
	if _, err := os.Stat(starterDest); err != nil {
		t.Fatalf("expected copied file at %s: %v", starterDest, err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}
	configStr := string(content)
	if !strings.Contains(configStr, "TERM") {
		t.Error("expected generated config to contain TERM setting")
	}
	if !strings.Contains(configStr, "opacity = 0.8") {
		t.Error("expected generated config to contain opacity setting")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	tmplDir := filepath.Join(tc.AppDir, "alacritty")
	if err := os.MkdirAll(tmplDir, 0755); err != nil {
		t.Fatal(err)
	}

	tmplContent := `[env]
TERM = "xterm-256color"

[window]
opacity = 0.9

{{if eq .Font "default"}}
[font]
size = 14
{{end}}

{{if eq .Theme "default"}}
[colors.primary]
background = "0x1e1e1e"
{{end}}
`
	tmplPath := filepath.Join(tmplDir, "alacritty.toml.tmpl")
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0644); err != nil {
		t.Fatal(err)
	}

	starterContent := "#!/bin/bash\nzsh"
	starterPath := filepath.Join(tmplDir, "starter.sh")
	if err := os.WriteFile(starterPath, []byte(starterContent), 0755); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(tc.ConfigDir, "alacritty")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatal(err)
	}

	oldAppConfig := paths.Paths.App.Configs.Alacritty
	oldLocalConfig := paths.Paths.Config.Alacritty
	oldConfigRoot := paths.Paths.Config.Root

	paths.Paths.App.Configs.Alacritty = tmplDir
	paths.Paths.Config.Alacritty = destDir
	paths.Paths.Config.Root = tc.ConfigDir

	t.Cleanup(func() {
		paths.Paths.App.Configs.Alacritty = oldAppConfig
		paths.Paths.Config.Alacritty = oldLocalConfig
		paths.Paths.Config.Root = oldConfigRoot
	})

	app := &Alacritty{Cmd: tc.MockApp.Cmd}

	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure error: %v", err)
	}

	configPath := filepath.Join(destDir, "alacritty.toml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected generated file at %s: %v", configPath, err)
	}

	starterDest := filepath.Join(destDir, "starter.sh")
	if _, err := os.Stat(starterDest); err != nil {
		t.Fatalf("expected copied file at %s: %v", starterDest, err)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}
	configStr := string(content)
	if !strings.Contains(configStr, "opacity = 0.9") {
		t.Error("expected generated config to contain opacity setting")
	}

	modifiedContent := "[window]\nopacity = 0.5\noption_as_alt = \"none\"\n"
	if err := os.WriteFile(configPath, []byte(modifiedContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("second SoftConfigure error: %v", err)
	}

	finalContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	if string(finalContent) != modifiedContent {
		t.Fatalf("SoftConfigure should not overwrite existing config: expected %q, got %q", modifiedContent, string(finalContent))
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}
