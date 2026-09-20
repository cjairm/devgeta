package i3

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
// paths.Paths.Config.Nvim under a fresh temp tree, writes a "default" theme
// fixture with every role set to hex, and restores the originals in
// t.Cleanup. ForceConfigure now resolves its theme through
// theme.CurrentDefinition, which loads and validates a whole theme file - so
// even a single-surface test has to give it one to load.
func setupThemeFixture(t *testing.T, hex string) {
	t.Helper()

	root := t.TempDir()
	themesDir := filepath.Join(root, "themes")
	neovimConfigsDir := filepath.Join(root, "neovim")
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
	paths.Paths.Config.Nvim = filepath.Join(root, "does-not-exist-nvim")
}

func TestNew(t *testing.T) {
	i := New()
	if i == nil {
		t.Fatal("New() returned nil")
	}
	if i.Cmd == nil {
		t.Fatal("Cmd is nil")
	}
}

func TestNameAndKind(t *testing.T) {
	i := &I3{}
	if i.Name() != constants.I3 {
		t.Errorf("expected Name() %q, got %q", constants.I3, i.Name())
	}
	if i.Kind() != apps.KindDesktop {
		t.Errorf("expected Kind() KindDesktop, got %v", i.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	i := &I3{Cmd: mockApp.Cmd}

	err := i.Install()
	if err != nil {
		t.Fatalf("Install() failed: %v", err)
	}

	if mockApp.Cmd.InstalledPkg != constants.I3 {
		t.Errorf("Expected package %s, got %s", constants.I3, mockApp.Cmd.InstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	i := &I3{Cmd: mockApp.Cmd}

	err := i.SoftInstall()
	if err != nil {
		t.Fatalf("SoftInstall() failed: %v", err)
	}

	if mockApp.Cmd.MaybeInstalled != constants.I3 {
		t.Errorf("Expected package %s, got %s", constants.I3, mockApp.Cmd.MaybeInstalled)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	i := &I3{Cmd: tc.MockApp.Cmd}

	if err := i.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}
	if tc.MockApp.Cmd.InstalledPkg != constants.I3 {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestUninstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	i := &I3{Cmd: tc.MockApp.Cmd}

	if err := i.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}
	if tc.MockApp.Cmd.UninstalledPkg != constants.I3 {
		t.Errorf(
			"expected UninstallPackage(%s), got %q",
			constants.I3,
			tc.MockApp.Cmd.UninstalledPkg,
		)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestForceConfigure(t *testing.T) {
	cleanup := testutil.SetupIsolatedPaths(t)
	defer cleanup()
	testutil.IsolateXDGDirs(t)

	appDir, configDir, _, _ := testutil.SetupTestDirs(t)

	// Create source i3 config
	i3ConfigAppDir := filepath.Join(appDir, "i3")
	if err := os.MkdirAll(i3ConfigAppDir, 0o755); err != nil {
		t.Fatalf("Failed to create app i3 dir: %v", err)
	}

	sourceConfig := filepath.Join(i3ConfigAppDir, "config.tmpl")
	sourceContent := "# i3 config\nset $mod Mod4\nset $bg {{.Palette.Background}}\n"
	if err := os.WriteFile(sourceConfig, []byte(sourceContent), 0o644); err != nil {
		t.Fatalf("Failed to create source config: %v", err)
	}

	// Override paths
	oldAppConfigsI3 := paths.Paths.App.Configs.I3
	oldConfigI3 := paths.Paths.Config.I3
	t.Cleanup(func() {
		paths.Paths.App.Configs.I3 = oldAppConfigsI3
		paths.Paths.Config.I3 = oldConfigI3
	})
	paths.Paths.App.Configs.I3 = i3ConfigAppDir
	paths.Paths.Config.I3 = filepath.Join(configDir, "i3")
	setupThemeFixture(t, "#282828")

	mockApp := testutil.NewMockApp()
	i := &I3{Cmd: mockApp.Cmd}

	err := i.ForceConfigure()
	if err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}

	wantContent := "# i3 config\nset $mod Mod4\nset $bg #282828\n"

	// Verify config was rendered
	dstConfig := filepath.Join(paths.Paths.Config.I3, "config")
	content, err := os.ReadFile(dstConfig)
	if err != nil {
		t.Fatalf("Failed to read destination config: %v", err)
	}

	if string(content) != wantContent {
		t.Errorf("Config content mismatch.\nExpected: %s\nGot: %s", wantContent, string(content))
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
	testutil.VerifyNoRealConfigChanges(t)
}

func TestSoftConfigure_PreservesExisting(t *testing.T) {
	cleanup := testutil.SetupIsolatedPaths(t)
	defer cleanup()
	testutil.IsolateXDGDirs(t)

	appDir, configDir, _, _ := testutil.SetupTestDirs(t)

	// Create source config (won't be used)
	i3ConfigAppDir := filepath.Join(appDir, "i3")
	if err := os.MkdirAll(i3ConfigAppDir, 0o755); err != nil {
		t.Fatalf("Failed to create app i3 dir: %v", err)
	}

	// Create existing config in target location
	i3ConfigLocalDir := filepath.Join(configDir, "i3")
	if err := os.MkdirAll(i3ConfigLocalDir, 0o755); err != nil {
		t.Fatalf("Failed to create local i3 dir: %v", err)
	}

	existingConfig := filepath.Join(i3ConfigLocalDir, "config")
	existingContent := "# Existing custom config\n"
	if err := os.WriteFile(existingConfig, []byte(existingContent), 0o644); err != nil {
		t.Fatalf("Failed to create existing config: %v", err)
	}

	// Override paths
	oldAppConfigsI3 := paths.Paths.App.Configs.I3
	oldConfigI3 := paths.Paths.Config.I3
	t.Cleanup(func() {
		paths.Paths.App.Configs.I3 = oldAppConfigsI3
		paths.Paths.Config.I3 = oldConfigI3
	})
	paths.Paths.App.Configs.I3 = i3ConfigAppDir
	paths.Paths.Config.I3 = i3ConfigLocalDir

	mockApp := testutil.NewMockApp()
	i := &I3{Cmd: mockApp.Cmd}

	err := i.SoftConfigure()
	if err != nil {
		t.Fatalf("SoftConfigure() failed: %v", err)
	}

	// Verify existing config was preserved
	content, err := os.ReadFile(existingConfig)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	if string(content) != existingContent {
		t.Error("Expected existing config to be preserved")
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
	testutil.VerifyNoRealConfigChanges(t)
}

func TestSoftConfigure_AppliesWhenMissing(t *testing.T) {
	cleanup := testutil.SetupIsolatedPaths(t)
	defer cleanup()
	testutil.IsolateXDGDirs(t)

	appDir, configDir, _, _ := testutil.SetupTestDirs(t)

	// Create source config
	i3ConfigAppDir := filepath.Join(appDir, "i3")
	if err := os.MkdirAll(i3ConfigAppDir, 0o755); err != nil {
		t.Fatalf("Failed to create app i3 dir: %v", err)
	}

	sourceConfig := filepath.Join(i3ConfigAppDir, "config.tmpl")
	sourceContent := "# i3 config from template\n"
	if err := os.WriteFile(sourceConfig, []byte(sourceContent), 0o644); err != nil {
		t.Fatalf("Failed to create source config: %v", err)
	}

	// Override paths
	oldAppConfigsI3 := paths.Paths.App.Configs.I3
	oldConfigI3 := paths.Paths.Config.I3
	t.Cleanup(func() {
		paths.Paths.App.Configs.I3 = oldAppConfigsI3
		paths.Paths.Config.I3 = oldConfigI3
	})
	paths.Paths.App.Configs.I3 = i3ConfigAppDir
	paths.Paths.Config.I3 = filepath.Join(configDir, "i3")
	setupThemeFixture(t, "#282828")

	mockApp := testutil.NewMockApp()
	i := &I3{Cmd: mockApp.Cmd}

	err := i.SoftConfigure()
	if err != nil {
		t.Fatalf("SoftConfigure() failed: %v", err)
	}

	// Verify config was applied
	dstConfig := filepath.Join(paths.Paths.Config.I3, "config")
	content, err := os.ReadFile(dstConfig)
	if err != nil {
		t.Fatalf("Failed to read destination config: %v", err)
	}

	if string(content) != sourceContent {
		t.Errorf("Config content mismatch.\nExpected: %s\nGot: %s", sourceContent, string(content))
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
	testutil.VerifyNoRealConfigChanges(t)
}

func TestExecuteCommand(t *testing.T) {
	mockApp := testutil.NewMockApp()
	i := &I3{Cmd: mockApp.Cmd}

	err := i.ExecuteCommand("reload")
	if err != nil {
		t.Fatalf("ExecuteCommand() failed: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	i := &I3{Cmd: mockApp.Cmd}

	err := i.Update()
	if err == nil {
		t.Fatal("Expected Update to return error")
	}
	if !errors.Is(err, apps.ErrUpdateNotSupported) {
		t.Errorf("expected ErrUpdateNotSupported, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}
