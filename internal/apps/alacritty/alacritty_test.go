package alacritty

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
)

func init() {
	testutil.InitLogger()
}

// paletteRoles lists every role internal/theme.Palette validates, so a test
// fixture theme file can be written without repeating this 24-line block at
// every call site.
var paletteRoles = []string{
	"background_hard", "background", "background_element", "background_subtle",
	"border", "foreground", "foreground_muted", "foreground_dim", "foreground_subtle",
	"red", "green", "yellow", "blue", "purple", "aqua", "orange",
	"red_dim", "green_dim", "yellow_dim", "blue_dim", "purple_dim", "aqua_dim",
	"diff_added_background", "diff_removed_background",
}

// writeThemeFixture writes a minimal, fully valid theme file under themesDir
// with every role set to hex, so internal/theme.Load accepts it.
func writeThemeFixture(t *testing.T, themesDir, name, hex, neovimModule string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("neovim_module: " + neovimModule + "\ncolors:\n")
	for _, role := range paletteRoles {
		b.WriteString("  " + role + ": \"" + hex + "\"\n")
	}
	path := filepath.Join(themesDir, name+".yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("failed to write theme fixture %s: %v", path, err)
	}
}

// writeNeovimModuleFixture drops a fake Neovim colorscheme module where
// internal/theme's validateNeovimModule expects to find a shipped one.
func writeNeovimModuleFixture(t *testing.T, neovimConfigsDir, module string) {
	t.Helper()
	dir := filepath.Join(neovimConfigsDir, "lua", "devgeta", "themes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, module+".lua")
	if err := os.WriteFile(path, []byte("-- fixture\n"), 0o644); err != nil {
		t.Fatalf("failed to write neovim module fixture %s: %v", path, err)
	}
}

// setupThemeFixture isolates paths.Paths.App.Configs.Themes, .Neovim and
// paths.Paths.Config.Nvim under tc's temp tree, writes a "default" theme
// fixture with every role set to hex, and restores the originals in
// t.Cleanup. ForceConfigure now resolves its palette through
// theme.CurrentFor, which loads and validates a whole theme file - so even a
// single-surface test has to give it one to load.
func setupThemeFixture(t *testing.T, tc *testutil.TestConfig, hex string) {
	t.Helper()

	themesDir := filepath.Join(tc.AppDir, "themes")
	neovimConfigsDir := filepath.Join(tc.AppDir, "neovim")
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeThemeFixture(t, themesDir, "default", hex, "gruvbox")
	writeNeovimModuleFixture(t, neovimConfigsDir, "gruvbox")

	oldThemes := paths.Paths.App.Configs.Themes
	oldNeovim := paths.Paths.App.Configs.Neovim
	oldNvim := paths.Paths.Config.Nvim
	t.Cleanup(func() {
		paths.Paths.App.Configs.Themes = oldThemes
		paths.Paths.App.Configs.Neovim = oldNeovim
		paths.Paths.Config.Nvim = oldNvim
	})
	paths.Paths.App.Configs.Themes = themesDir
	paths.Paths.App.Configs.Neovim = neovimConfigsDir
	paths.Paths.Config.Nvim = filepath.Join(tc.ConfigDir, "does-not-exist-nvim")
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
		t.Fatalf(
			"expected InstallDesktopApp(%s), got %q",
			constants.Alacritty,
			mockApp.Cmd.InstalledDesktopApp,
		)
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
		t.Fatalf(
			"expected MaybeInstallDesktopApp(%s), got %q",
			constants.Alacritty,
			mockApp.Cmd.MaybeInstalledDesktop,
		)
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
		t.Errorf(
			"expected UninstallDesktopApp(%s), got %q",
			constants.Alacritty,
			tc.MockApp.Cmd.UninstalledDesktopApp,
		)
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
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
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

[colors.primary]
background = "{{.Colors.Background}}"
`
	tmplPath := filepath.Join(tmplDir, "alacritty.toml.tmpl")
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// starter.sh lives under the shared terminal configs dir, not alacritty's
	// own — see paths.Paths.App.Configs.Terminal.
	terminalDir := filepath.Join(tc.AppDir, "terminal")
	if err := os.MkdirAll(terminalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	starterContent := "#!/bin/bash\nzsh"
	starterPath := filepath.Join(terminalDir, "starter.sh")
	if err := os.WriteFile(starterPath, []byte(starterContent), 0o755); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(tc.ConfigDir, "alacritty")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldAppConfig := paths.Paths.App.Configs.Alacritty
	oldAppTerminal := paths.Paths.App.Configs.Terminal
	oldLocalConfig := paths.Paths.Config.Alacritty
	oldConfigRoot := paths.Paths.Config.Root

	paths.Paths.App.Configs.Alacritty = tmplDir
	paths.Paths.App.Configs.Terminal = terminalDir
	paths.Paths.Config.Alacritty = destDir
	paths.Paths.Config.Root = tc.ConfigDir

	t.Cleanup(func() {
		paths.Paths.App.Configs.Alacritty = oldAppConfig
		paths.Paths.App.Configs.Terminal = oldAppTerminal
		paths.Paths.Config.Alacritty = oldLocalConfig
		paths.Paths.Config.Root = oldConfigRoot
	})
	setupThemeFixture(t, tc, "#282828")

	app := &Alacritty{Cmd: tc.MockApp.Cmd}

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	configPath := filepath.Join(destDir, "alacritty.toml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected generated file at %s: %v", configPath, err)
	}

	starterDest := filepath.Join(destDir, "starter.sh")
	starterInfo, err := os.Stat(starterDest)
	if err != nil {
		t.Fatalf("expected copied file at %s: %v", starterDest, err)
	}
	// Alacritty's [terminal.shell] program execs this script directly. The repo
	// blob is 100644, the embedded-config extractor writes 0644, and
	// files.CopyFile writes 0644 — so without an explicit chmod the deployed
	// script is not executable and Alacritty comes up without tmux.
	if perm := starterInfo.Mode().Perm(); perm != 0o755 {
		t.Errorf("expected starter.sh to be deployed executable (0755), got %o", perm)
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

// TestForceConfigure_RendersCurrentThemePalette proves ForceConfigure reads
// the palette through theme.CurrentFor rather than a hardcoded name: with
// current_theme set to a fixture theme whose colors are distinguishable from
// "default", the rendered config must show that fixture's color.
func TestForceConfigure_RendersCurrentThemePalette(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	tmplDir := filepath.Join(tc.AppDir, "alacritty")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tmplContent := `[colors.primary]
background = "{{.Colors.Background}}"
foreground = "{{.Colors.Foreground}}"
`
	if err := os.WriteFile(
		filepath.Join(tmplDir, "alacritty.toml.tmpl"),
		[]byte(tmplContent),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	terminalDir := filepath.Join(tc.AppDir, "terminal")
	if err := os.MkdirAll(terminalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(terminalDir, "starter.sh"),
		[]byte("#!/bin/bash\nzsh"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(tc.ConfigDir, "alacritty")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldAppConfig := paths.Paths.App.Configs.Alacritty
	oldAppTerminal := paths.Paths.App.Configs.Terminal
	oldLocalConfig := paths.Paths.Config.Alacritty
	oldConfigRoot := paths.Paths.Config.Root
	paths.Paths.App.Configs.Alacritty = tmplDir
	paths.Paths.App.Configs.Terminal = terminalDir
	paths.Paths.Config.Alacritty = destDir
	paths.Paths.Config.Root = tc.ConfigDir
	t.Cleanup(func() {
		paths.Paths.App.Configs.Alacritty = oldAppConfig
		paths.Paths.App.Configs.Terminal = oldAppTerminal
		paths.Paths.Config.Alacritty = oldLocalConfig
		paths.Paths.Config.Root = oldConfigRoot
	})

	themesDir := filepath.Join(tc.AppDir, "themes")
	neovimConfigsDir := filepath.Join(tc.AppDir, "neovim")
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeThemeFixture(t, themesDir, "default", "#282828", "gruvbox")
	writeThemeFixture(t, themesDir, "custom", "#abcdef", "gruvbox")
	writeNeovimModuleFixture(t, neovimConfigsDir, "gruvbox")
	oldThemes := paths.Paths.App.Configs.Themes
	oldNeovim := paths.Paths.App.Configs.Neovim
	oldNvim := paths.Paths.Config.Nvim
	t.Cleanup(func() {
		paths.Paths.App.Configs.Themes = oldThemes
		paths.Paths.App.Configs.Neovim = oldNeovim
		paths.Paths.Config.Nvim = oldNvim
	})
	paths.Paths.App.Configs.Themes = themesDir
	paths.Paths.App.Configs.Neovim = neovimConfigsDir
	paths.Paths.Config.Nvim = filepath.Join(tc.ConfigDir, "does-not-exist-nvim")

	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatal(err)
	}
	if err := gc.Load(); err != nil {
		t.Fatal(err)
	}
	gc.CurrentTheme = "custom"
	if err := gc.Save(); err != nil {
		t.Fatal(err)
	}

	app := &Alacritty{Cmd: tc.MockApp.Cmd}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(destDir, "alacritty.toml"))
	if err != nil {
		t.Fatalf("failed to read generated config: %v", err)
	}
	if !strings.Contains(string(content), "0xabcdef") {
		t.Errorf(
			"expected rendered config to use current_theme %q's color (0xabcdef), got:\n%s",
			"custom",
			content,
		)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	tmplDir := filepath.Join(tc.AppDir, "alacritty")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
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

[colors.primary]
background = "{{.Colors.Background}}"
`
	tmplPath := filepath.Join(tmplDir, "alacritty.toml.tmpl")
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0o644); err != nil {
		t.Fatal(err)
	}

	terminalDir := filepath.Join(tc.AppDir, "terminal")
	if err := os.MkdirAll(terminalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	starterContent := "#!/bin/bash\nzsh"
	starterPath := filepath.Join(terminalDir, "starter.sh")
	if err := os.WriteFile(starterPath, []byte(starterContent), 0o755); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(tc.ConfigDir, "alacritty")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldAppConfig := paths.Paths.App.Configs.Alacritty
	oldAppTerminal := paths.Paths.App.Configs.Terminal
	oldLocalConfig := paths.Paths.Config.Alacritty
	oldConfigRoot := paths.Paths.Config.Root

	paths.Paths.App.Configs.Alacritty = tmplDir
	paths.Paths.App.Configs.Terminal = terminalDir
	paths.Paths.Config.Alacritty = destDir
	paths.Paths.Config.Root = tc.ConfigDir

	t.Cleanup(func() {
		paths.Paths.App.Configs.Alacritty = oldAppConfig
		paths.Paths.App.Configs.Terminal = oldAppTerminal
		paths.Paths.Config.Alacritty = oldLocalConfig
		paths.Paths.Config.Root = oldConfigRoot
	})
	setupThemeFixture(t, tc, "#1e1e1e")

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
	if err := os.WriteFile(configPath, []byte(modifiedContent), 0o644); err != nil {
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
		t.Fatalf(
			"SoftConfigure should not overwrite existing config: expected %q, got %q",
			modifiedContent,
			string(finalContent),
		)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

// TestSoftConfigure_WritesConfigOnTheRunThatInstalled reproduces the exact
// sequence the desktop coordinator runs: SoftInstall, then SoftConfigure.
// SoftInstall's real path (BaseCommand.MaybeInstall) records the app in
// global_config's installed list the moment the install succeeds, so a
// SoftConfigure that treats "devgeta installed this" as "already configured"
// writes nothing on the one run that actually needed it — a freshly installed
// Alacritty comes up with default colors, no transparency and no tmux.
//
// The app's own unit tests could not catch this: MockCommand answers
// MaybeInstallDesktopApp directly and never reaches BaseCommand.MaybeInstall,
// so the tracking entry this guard reads was never written under test. The
// tracking is therefore set up explicitly here, exactly as a real install
// leaves it.
func TestSoftConfigure_WritesConfigOnTheRunThatInstalled(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	tmplDir := filepath.Join(tc.AppDir, "alacritty")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}
	tmplPath := filepath.Join(tmplDir, "alacritty.toml.tmpl")
	if err := os.WriteFile(
		tmplPath,
		[]byte("[env]\nTERM = \"xterm-256color\"\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	terminalDir := filepath.Join(tc.AppDir, "terminal")
	if err := os.MkdirAll(terminalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	starterPath := filepath.Join(terminalDir, "starter.sh")
	if err := os.WriteFile(starterPath, []byte("#!/bin/bash\nzsh"), 0o755); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(tc.ConfigDir, "alacritty")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldAppConfig := paths.Paths.App.Configs.Alacritty
	oldAppTerminal := paths.Paths.App.Configs.Terminal
	oldLocalConfig := paths.Paths.Config.Alacritty
	oldConfigRoot := paths.Paths.Config.Root
	paths.Paths.App.Configs.Alacritty = tmplDir
	paths.Paths.App.Configs.Terminal = terminalDir
	paths.Paths.Config.Alacritty = destDir
	paths.Paths.Config.Root = tc.ConfigDir
	t.Cleanup(func() {
		paths.Paths.App.Configs.Alacritty = oldAppConfig
		paths.Paths.App.Configs.Terminal = oldAppTerminal
		paths.Paths.Config.Alacritty = oldLocalConfig
		paths.Paths.Config.Root = oldConfigRoot
	})
	setupThemeFixture(t, tc, "#282828")

	// What a successful SoftInstall leaves behind before SoftConfigure runs.
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatal(err)
	}
	if err := gc.Load(); err != nil {
		t.Fatal(err)
	}
	gc.AddToInstalled(constants.Alacritty, "desktop_app")
	if err := gc.Save(); err != nil {
		t.Fatal(err)
	}

	app := &Alacritty{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure error: %v", err)
	}

	configPath := filepath.Join(destDir, "alacritty.toml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf(
			"expected SoftConfigure to write %s on the run that installed alacritty: %v",
			configPath,
			err,
		)
	}
	if _, err := os.Stat(filepath.Join(destDir, "starter.sh")); err != nil {
		t.Fatalf("expected starter.sh to be deployed alongside the config: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}
