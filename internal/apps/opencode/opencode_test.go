package opencode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"text/template"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/commands"
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

// writeOpenCodeThemeTemplateFixture writes the minimal default.json.tmpl the
// real configs/opencode/themes/default.json.tmpl provides, into appConfigDir
// (a themes/ subdirectory is created).
func writeOpenCodeThemeTemplateFixture(t *testing.T, appConfigDir, content string) {
	t.Helper()
	dir := filepath.Join(appConfigDir, "themes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "default.json.tmpl"),
		[]byte(content),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func TestNew(t *testing.T) {
	app := New()
	if app == nil {
		t.Fatal("New() returned nil")
	}
	if app.Cmd == nil {
		t.Error("Expected Cmd to be initialized")
	}
	if app.Base == nil {
		t.Error("Expected Base to be initialized")
	}
}

func TestNameAndKind(t *testing.T) {
	o := &OpenCode{}
	if o.Name() != constants.OpenCode {
		t.Errorf("expected Name() %q, got %q", constants.OpenCode, o.Name())
	}
	if o.Kind() != apps.KindTerminal {
		t.Errorf("expected Kind() KindTerminal, got %v", o.Kind())
	}
}

// isolateOpenCodeInstallPrefix points paths.Paths.Home.OpenCode - the
// directory the install script owns - at a fresh temp tree and restores the
// original afterwards, so a test that creates or removes the installed binary
// cannot touch the real one.
func isolateOpenCodeInstallPrefix(t *testing.T) string {
	t.Helper()
	prefix := filepath.Join(t.TempDir(), ".opencode")
	old := paths.Paths.Home.OpenCode
	t.Cleanup(func() { paths.Paths.Home.OpenCode = old })
	paths.Paths.Home.OpenCode = prefix
	return prefix
}

// stubLookPath swaps the package-level PATH-lookup seam every app uses for
// binary detection (commands.LookPathFn), so a test decides whether opencode
// is on PATH instead of inheriting whatever the machine running the test has
// installed.
func stubLookPath(t *testing.T, fn func(string) (string, error)) {
	t.Helper()
	orig := commands.LookPathFn
	t.Cleanup(func() { commands.LookPathFn = orig })
	commands.LookPathFn = fn
}

// writeOpenCodeBinaryFixture creates the file the install script would leave
// at <prefix>/bin/opencode.
func writeOpenCodeBinaryFixture(t *testing.T, prefix string) {
	t.Helper()
	binDir := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(binDir, constants.OpenCode),
		[]byte("#!/bin/sh\n"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}

	calls := mockApp.Base.ExecCommandCalls
	if len(calls) != 2 {
		t.Fatalf("expected 2 ExecCommand calls (download, execute), got %d", len(calls))
	}
	download, execute := calls[0], calls[1]
	if download.Command != "curl" {
		t.Errorf("expected download step command 'curl', got %q", download.Command)
	}
	if !slices.Contains(download.Args, openCodeInstallScriptURL) {
		t.Errorf(
			"expected download step to fetch %q, got args %v",
			openCodeInstallScriptURL, download.Args,
		)
	}
	if download.Timeout == 0 {
		t.Error("expected download step to carry a non-zero Timeout")
	}
	// The script's shebang is `#!/usr/bin/env bash` and it uses bash-only
	// syntax. Running it with `sh` - dash on Debian - is the defect ADR-0047
	// fixes, so the interpreter is part of the contract, not an incidental
	// argument.
	if execute.Command != "bash" {
		t.Errorf("expected execute step command 'bash', got %q", execute.Command)
	}
	if execute.Timeout == 0 {
		t.Error("expected execute step to carry a non-zero Timeout")
	}
	// The v2 script installs the latest release unless told a version; devgeta
	// always installs the pin.
	if want := []string{"--version", constants.PinnedVersion.OpenCode}; !slices.Equal(execute.Args[1:], want) {
		t.Errorf("expected script args %v, got %v", want, execute.Args[1:])
	}

	// No package manager is involved on either platform any more: brew on
	// macOS offered no pin and no downgrade, and apt never had opencode at
	// all (ADR-0047 decision 1).
	if mockApp.Cmd.InstalledPkg != "" {
		t.Errorf(
			"expected no package-manager install, got InstallPackage(%q)",
			mockApp.Cmd.InstalledPkg,
		)
	}
}

// TestInstall_FailedDownloadBlocksInstall asserts a failed or truncated
// download stops before the script ever runs, rather than being reported as a
// successful install - the defect the staged shape replaces (`false | bash`
// exits 0).
func TestInstall_FailedDownloadBlocksInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResults(
		commands.ExecCommandResult("", "", fmt.Errorf("404 not found")),
	)
	app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

	err := app.Install()
	if err == nil {
		t.Fatal("expected Install to fail when the download step fails")
	}
	if !strings.Contains(err.Error(), "failed to download install script") {
		t.Errorf("expected download-failure error, got: %v", err)
	}
	if got := mockApp.Base.GetExecCommandCallCount(); got != 1 {
		t.Errorf(
			"expected the execute step to be skipped after a failed download, got %d calls",
			got,
		)
	}
}

func TestForceInstall(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	prefix := isolateOpenCodeInstallPrefix(t)
	writeOpenCodeBinaryFixture(t, prefix)

	userConfigDir := filepath.Join(tc.ConfigDir, "opencode")
	oldOpenCodeDir := paths.Paths.Config.OpenCode
	t.Cleanup(func() { paths.Paths.Config.OpenCode = oldOpenCodeDir })
	paths.Paths.Config.OpenCode = userConfigDir

	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}

	// Uninstall removes the prefix with os.RemoveAll (no command at all),
	// then Install stages the script: curl, then bash.
	calls := tc.MockApp.Base.ExecCommandCalls
	if len(calls) != 2 {
		t.Fatalf("expected 2 ExecCommand calls (download, execute), got %d", len(calls))
	}
	if calls[0].Command != "curl" {
		t.Errorf("expected download step 'curl', got %q", calls[0].Command)
	}
	if calls[1].Command != "bash" {
		t.Errorf("expected install script executed via 'bash', got %q", calls[1].Command)
	}
	if tc.MockApp.Cmd.InstalledPkg != "" || tc.MockApp.Cmd.UninstalledPkg != "" {
		t.Errorf(
			"expected no package-manager calls, got install=%q uninstall=%q",
			tc.MockApp.Cmd.InstalledPkg, tc.MockApp.Cmd.UninstalledPkg,
		)
	}
}

func TestSoftInstall(t *testing.T) {
	// The prefix check is not a duplicate of the PATH lookup: a fresh
	// install's ~/.opencode/bin is not on the running process's PATH (nor on
	// the user's, until they open a new shell), so a PATH-only check would
	// re-run the installer on every `dg install` - the Debian idempotency
	// defect in ADR-0047.
	t.Run("skips when the script's own binary is present at the pin, even off PATH", func(t *testing.T) {
		prefix := isolateOpenCodeInstallPrefix(t)
		writeOpenCodeBinaryFixture(t, prefix)
		stubLookPath(t, func(string) (string, error) {
			return "", fmt.Errorf("not found")
		})

		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("opencode v"+constants.PinnedVersion.OpenCode+"\n", "", nil),
		)
		app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("SoftInstall error: %v", err)
		}
		calls := mockApp.Base.ExecCommandCalls
		if len(calls) != 1 || calls[0].Command != installedBinary() || calls[0].Args[0] != "--version" {
			t.Errorf("expected only the managed binary's --version, got %+v", calls)
		}
		if mockApp.Cmd.MaybeInstalled != "" || mockApp.Cmd.InstalledPkg != "" {
			t.Errorf(
				"expected no package-manager calls, got maybe=%q install=%q",
				mockApp.Cmd.MaybeInstalled, mockApp.Cmd.InstalledPkg,
			)
		}
	})

	// An OpenCode 1 install (or anything older than the pin, or a version it
	// cannot read) is replaced: devgeta's config only means what it says on
	// the pinned release. A newer one the user installed stays.
	for _, tt := range []struct {
		name, versionOutput string
		wantInstall         bool
	}{
		{"upgrades an OpenCode 1 install", "1.19.0\n", true},
		{"reinstalls an unreadable version", "opencode\n", true},
		{"keeps a newer version", "opencode v99.0.0\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prefix := isolateOpenCodeInstallPrefix(t)
			writeOpenCodeBinaryFixture(t, prefix)
			stubLookPath(t, func(string) (string, error) {
				return "/usr/local/bin/opencode", nil
			})

			mockApp := testutil.NewMockApp()
			mockApp.Base.SetExecCommandResults(commands.ExecCommandResult(tt.versionOutput, "", nil))
			app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

			if err := app.SoftInstall(); err != nil {
				t.Fatalf("SoftInstall error: %v", err)
			}
			calls := mockApp.Base.ExecCommandCalls
			installed := len(calls) == 3 && calls[1].Command == "curl" && calls[2].Command == "bash"
			if installed != tt.wantInstall {
				t.Errorf("install ran = %v, want %v (calls %+v)", installed, tt.wantInstall, calls)
			}
		})
	}

	// A user who already has opencode from somewhere else (npm, brew, their
	// own build) keeps it: devgeta does not install a second copy over it.
	t.Run("skips when opencode resolves on PATH from elsewhere", func(t *testing.T) {
		isolateOpenCodeInstallPrefix(t)
		stubLookPath(t, func(string) (string, error) {
			return "/usr/local/bin/opencode", nil
		})

		mockApp := testutil.NewMockApp()
		app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("SoftInstall error: %v", err)
		}
		if got := mockApp.Base.GetExecCommandCallCount(); got != 0 {
			t.Errorf("expected no commands when opencode is on PATH, got %d", got)
		}
	})

	t.Run("installs when neither the prefix nor PATH has it", func(t *testing.T) {
		isolateOpenCodeInstallPrefix(t)
		stubLookPath(t, func(string) (string, error) {
			return "", fmt.Errorf("not found")
		})

		mockApp := testutil.NewMockApp()
		app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("SoftInstall error: %v", err)
		}
		calls := mockApp.Base.ExecCommandCalls
		if len(calls) != 2 {
			t.Fatalf("expected 2 ExecCommand calls (download, execute), got %d", len(calls))
		}
		if calls[0].Command != "curl" || calls[1].Command != "bash" {
			t.Errorf(
				"expected staged curl-then-bash install, got %q then %q",
				calls[0].Command, calls[1].Command,
			)
		}
		if mockApp.Cmd.MaybeInstalled != "" {
			t.Errorf(
				"expected no package-manager install, got MaybeInstallPackage(%q)",
				mockApp.Cmd.MaybeInstalled,
			)
		}
	})
}

func TestUninstall(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	prefix := isolateOpenCodeInstallPrefix(t)
	writeOpenCodeBinaryFixture(t, prefix)

	userConfigDir := filepath.Join(tc.ConfigDir, "opencode")
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldOpenCodeDir := paths.Paths.Config.OpenCode
	t.Cleanup(func() { paths.Paths.Config.OpenCode = oldOpenCodeDir })
	paths.Paths.Config.OpenCode = userConfigDir

	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatal(err)
	}
	if err := gc.Load(); err != nil {
		t.Fatal(err)
	}
	gc.AddToInstalled(constants.OpenCode, "package")
	gc.Shell.Opencode = true
	if err := gc.Save(); err != nil {
		t.Fatal(err)
	}

	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}

	// Removing the prefix IS the uninstall: there is no package-manager
	// entry to remove, and `apt-get remove opencode` against a file in
	// ~/.opencode was the Debian no-op ADR-0047 fixes.
	if _, err := os.Stat(prefix); !os.IsNotExist(err) {
		t.Errorf("expected %q to be removed, stat err = %v", prefix, err)
	}
	if _, err := os.Stat(userConfigDir); !os.IsNotExist(err) {
		t.Errorf("expected %q to be removed, stat err = %v", userConfigDir, err)
	}
	if tc.MockApp.Cmd.UninstalledPkg != "" {
		t.Errorf(
			"expected no package-manager uninstall, got UninstallPackage(%q)",
			tc.MockApp.Cmd.UninstalledPkg,
		)
	}

	after := &config.GlobalConfig{}
	if err := after.Load(); err != nil {
		t.Fatal(err)
	}
	if after.IsTracked(constants.OpenCode, "package", "installed") {
		t.Error("expected opencode to be dropped from the installed packages list")
	}
	if after.Shell.Opencode {
		t.Error("expected the opencode shell feature to be disabled")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

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
	t.Run("ConfigureWithDefaultTheme", func(t *testing.T) {
		testutil.IsolateXDGDirs(t)
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		appConfigDir := filepath.Join(tc.AppDir, "configs", "opencode")
		userConfigDir := filepath.Join(tc.ConfigDir, "opencode")

		if err := os.MkdirAll(appConfigDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(appConfigDir, "themes"), 0o755); err != nil {
			t.Fatal(err)
		}

		templateContent := `{
  "version": "1.0.0",
  "settings": {
    "fontSize": 14,
    "fontFamily": "JetBrains Mono"
  }
}`
		templatePath := filepath.Join(appConfigDir, "opencode.json.tmpl")
		if err := os.WriteFile(templatePath, []byte(templateContent), 0o644); err != nil {
			t.Fatal(err)
		}

		writeOpenCodeThemeTemplateFixture(
			t,
			appConfigDir,
			`{"name": "Devgeta Gruvbox", "foreground": "{{.Palette.Foreground}}"}`,
		)

		writePluginFixtures(t, appConfigDir)

		oldAppConfigs := paths.Paths.App.Configs.OpenCode
		t.Cleanup(func() { paths.Paths.App.Configs.OpenCode = oldAppConfigs })
		paths.Paths.App.Configs.OpenCode = appConfigDir

		oldConfigOpenCode := paths.Paths.Config.OpenCode
		t.Cleanup(func() { paths.Paths.Config.OpenCode = oldConfigOpenCode })
		paths.Paths.Config.OpenCode = userConfigDir
		setupThemeFixture(t, "#ebdbb2")

		app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.ForceConfigure(); err != nil {
			t.Fatalf("ForceConfigure error: %v", err)
		}

		configPath := filepath.Join(userConfigDir, "opencode.json")
		if _, err := os.Stat(configPath); err != nil {
			t.Fatalf("Expected config file at %s: %v", configPath, err)
		}

		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("Failed to read config: %v", err)
		}
		configStr := string(content)
		if got := cliThemeName(t, userConfigDir); got != "default" {
			t.Errorf("expected cli.json theme.name 'default', got %q", got)
		}
		if !strings.Contains(configStr, "JetBrains Mono") {
			t.Error("Expected config to contain font family")
		}

		themePath := filepath.Join(userConfigDir, "themes", "default.json")
		if _, err := os.Stat(themePath); err != nil {
			t.Fatalf("Expected theme file at %s: %v", themePath, err)
		}

		themeContentRead, err := os.ReadFile(themePath)
		if err != nil {
			t.Fatalf("Failed to read theme: %v", err)
		}
		if !strings.Contains(string(themeContentRead), "Devgeta Gruvbox") {
			t.Error("Expected theme file to contain Gruvbox theme")
		}
		if !strings.Contains(string(themeContentRead), "#ebdbb2") {
			t.Error("Expected theme file to be rendered with the current theme's palette")
		}

		// every devgeta plugin folder deployed
		for _, name := range devgetaPlugins {
			pluginPath := filepath.Join(userConfigDir, "plugins", name, "server.js")
			if _, err := os.Stat(pluginPath); err != nil {
				t.Errorf("Expected plugin file at %s: %v", pluginPath, err)
			}
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})

	t.Run("RemovesExistingConfigDirectory", func(t *testing.T) {
		testutil.IsolateXDGDirs(t)
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		appConfigDir := filepath.Join(tc.AppDir, "configs", "opencode")
		userConfigDir := filepath.Join(tc.ConfigDir, "opencode")

		if err := os.MkdirAll(appConfigDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(appConfigDir, "themes"), 0o755); err != nil {
			t.Fatal(err)
		}
		writePluginFixtures(t, appConfigDir)

		templatePath := filepath.Join(appConfigDir, "opencode.json.tmpl")
		if err := os.WriteFile(
			templatePath,
			[]byte(`{"permissions": []}`),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
		themeSourcePath := filepath.Join(appConfigDir, "themes", "default.json.tmpl")
		if err := os.WriteFile(themeSourcePath, []byte(`{"name": "test"}`), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := os.MkdirAll(filepath.Join(userConfigDir, "themes"), 0o755); err != nil {
			t.Fatal(err)
		}
		// A stale theme devgeta itself rendered on an earlier run. The probe
		// has to be devgeta-managed: ForceConfigure now clears only what
		// devgeta generates, so an arbitrary foreign file would (correctly)
		// survive and would not test this at all.
		oldFilePath := filepath.Join(userConfigDir, "themes", "old-theme.json")
		if err := os.WriteFile(oldFilePath, []byte("old content"), 0o644); err != nil {
			t.Fatal(err)
		}

		setupThemeFixture(t, "#282828")

		oldAppConfigs := paths.Paths.App.Configs.OpenCode
		t.Cleanup(func() { paths.Paths.App.Configs.OpenCode = oldAppConfigs })
		paths.Paths.App.Configs.OpenCode = appConfigDir

		oldConfigOpenCode := paths.Paths.Config.OpenCode
		t.Cleanup(func() { paths.Paths.Config.OpenCode = oldConfigOpenCode })
		paths.Paths.Config.OpenCode = userConfigDir

		app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.ForceConfigure(); err != nil {
			t.Fatalf("ForceConfigure error: %v", err)
		}

		if _, err := os.Stat(oldFilePath); err == nil {
			t.Error("Expected old file to be removed")
		}

		configPath := filepath.Join(userConfigDir, "opencode.json")
		if _, err := os.Stat(configPath); err != nil {
			t.Fatalf("Expected new config file: %v", err)
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})
}

// TestForceConfigure_RendersCurrentThemePalette proves ForceConfigure reads
// the theme through theme.CurrentDefinition rather than a hardcoded name:
// with current_theme set to a fixture theme whose colors are distinguishable
// from "default", both cli.json's theme.name and the rendered
// themes/<name>.json must reflect it.
func TestForceConfigure_RendersCurrentThemePalette(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	appConfigDir := filepath.Join(tc.AppDir, "configs", "opencode")
	userConfigDir := filepath.Join(tc.ConfigDir, "opencode")

	if err := os.MkdirAll(appConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "opencode.json.tmpl"),
		[]byte(`{"permissions": []}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeOpenCodeThemeTemplateFixture(t, appConfigDir, `{"foreground": "{{.Palette.Foreground}}"}`)

	writePluginFixtures(t, appConfigDir)

	oldAppConfigs := paths.Paths.App.Configs.OpenCode
	t.Cleanup(func() { paths.Paths.App.Configs.OpenCode = oldAppConfigs })
	paths.Paths.App.Configs.OpenCode = appConfigDir

	oldConfigOpenCode := paths.Paths.Config.OpenCode
	t.Cleanup(func() { paths.Paths.Config.OpenCode = oldConfigOpenCode })
	paths.Paths.Config.OpenCode = userConfigDir

	root := t.TempDir()
	themesDir := filepath.Join(root, "themes")
	neovimConfigsDir := filepath.Join(root, "neovim")
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
	paths.Paths.Config.Nvim = filepath.Join(root, "does-not-exist-nvim")

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

	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	if got := cliThemeName(t, userConfigDir); got != "custom" {
		t.Errorf("expected cli.json to select the current theme 'custom', got %q", got)
	}

	themePath := filepath.Join(userConfigDir, "themes", "custom.json")
	themeContent, err := os.ReadFile(themePath)
	if err != nil {
		t.Fatalf("expected theme file at %s: %v", themePath, err)
	}
	if !strings.Contains(string(themeContent), "#abcdef") {
		t.Errorf(
			"expected rendered theme file to use current_theme %q's color (#abcdef), got:\n%s",
			"custom",
			themeContent,
		)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	t.Run("SkipWhenAlreadyConfigured", func(t *testing.T) {
		testutil.IsolateXDGDirs(t)
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		userConfigDir := filepath.Join(tc.ConfigDir, "opencode")
		if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
			t.Fatal(err)
		}
		markerPath := filepath.Join(userConfigDir, "opencode.json")
		if err := os.WriteFile(markerPath, []byte(`{"theme": "existing"}`), 0o644); err != nil {
			t.Fatal(err)
		}

		oldConfigOpenCode := paths.Paths.Config.OpenCode
		t.Cleanup(func() { paths.Paths.Config.OpenCode = oldConfigOpenCode })
		paths.Paths.Config.OpenCode = userConfigDir

		app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.SoftConfigure(); err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}

		content, err := os.ReadFile(markerPath)
		if err != nil {
			t.Fatalf("Failed to read config: %v", err)
		}
		if !strings.Contains(string(content), "existing") {
			t.Error("Expected existing config to be preserved")
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})

	t.Run("ConfigureWhenNotConfigured", func(t *testing.T) {
		testutil.IsolateXDGDirs(t)
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		appConfigDir := filepath.Join(tc.AppDir, "configs", "opencode")
		userConfigDir := filepath.Join(tc.ConfigDir, "opencode")

		if err := os.MkdirAll(appConfigDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(appConfigDir, "themes"), 0o755); err != nil {
			t.Fatal(err)
		}
		writePluginFixtures(t, appConfigDir)

		templatePath := filepath.Join(appConfigDir, "opencode.json.tmpl")
		if err := os.WriteFile(
			templatePath,
			[]byte(`{"permissions": []}`),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
		themeSourcePath := filepath.Join(appConfigDir, "themes", "default.json.tmpl")
		if err := os.WriteFile(themeSourcePath, []byte(`{"name": "test"}`), 0o644); err != nil {
			t.Fatal(err)
		}

		setupThemeFixture(t, "#282828")

		oldAppConfigs := paths.Paths.App.Configs.OpenCode
		t.Cleanup(func() { paths.Paths.App.Configs.OpenCode = oldAppConfigs })
		paths.Paths.App.Configs.OpenCode = appConfigDir

		oldConfigOpenCode := paths.Paths.Config.OpenCode
		t.Cleanup(func() { paths.Paths.Config.OpenCode = oldConfigOpenCode })
		paths.Paths.Config.OpenCode = userConfigDir

		app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.SoftConfigure(); err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}

		configPath := filepath.Join(userConfigDir, "opencode.json")
		if _, err := os.Stat(configPath); err != nil {
			t.Fatalf("Expected config file to be created: %v", err)
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})

	t.Run("ConfigureWhenAlreadyInstalledButNotConfigured", func(t *testing.T) {
		testutil.IsolateXDGDirs(t)
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		globalConfigContent := `app_path: ""
config_path: ""
installed:
  packages:
    - opencode
shell:
  mise: false
`
		globalConfigPath := filepath.Join(
			tc.ConfigDir,
			constants.App.Name,
			constants.App.File.GlobalConfig,
		)
		if err := os.WriteFile(globalConfigPath, []byte(globalConfigContent), 0o644); err != nil {
			t.Fatal(err)
		}

		appConfigDir := filepath.Join(tc.AppDir, "configs", "opencode")
		userConfigDir := filepath.Join(tc.ConfigDir, "opencode")

		if err := os.MkdirAll(appConfigDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(appConfigDir, "themes"), 0o755); err != nil {
			t.Fatal(err)
		}
		writePluginFixtures(t, appConfigDir)
		templatePath := filepath.Join(appConfigDir, "opencode.json.tmpl")
		if err := os.WriteFile(
			templatePath,
			[]byte(`{"permissions": []}`),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
		themeSourcePath := filepath.Join(appConfigDir, "themes", "default.json.tmpl")
		if err := os.WriteFile(themeSourcePath, []byte(`{"name": "test"}`), 0o644); err != nil {
			t.Fatal(err)
		}

		setupThemeFixture(t, "#282828")

		oldAppConfigs := paths.Paths.App.Configs.OpenCode
		t.Cleanup(func() { paths.Paths.App.Configs.OpenCode = oldAppConfigs })
		paths.Paths.App.Configs.OpenCode = appConfigDir

		oldConfigOpenCode := paths.Paths.Config.OpenCode
		t.Cleanup(func() { paths.Paths.Config.OpenCode = oldConfigOpenCode })
		paths.Paths.Config.OpenCode = userConfigDir

		app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.SoftConfigure(); err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}

		configPath := filepath.Join(userConfigDir, "opencode.json")
		if _, err := os.Stat(configPath); err != nil {
			t.Fatalf("Expected config file to be created even when already installed: %v", err)
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})
}

func TestExecuteCommand(t *testing.T) {
	t.Run("SuccessfulExecution", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

		mockApp.Base.SetExecCommandResult("OpenCode 1.0.0", "", nil)

		if err := app.ExecuteCommand("--version"); err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		if mockApp.Base.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 call, got %d", mockApp.Base.GetExecCommandCallCount())
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall.Command != constants.OpenCode {
			t.Fatalf("Expected command '%s', got %q", constants.OpenCode, lastCall.Command)
		}
		if len(lastCall.Args) != 1 || lastCall.Args[0] != "--version" {
			t.Fatalf("Expected args ['--version'], got %v", lastCall.Args)
		}
	})

	t.Run("CommandError", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

		mockApp.Base.SetExecCommandResult("", "error output", fmt.Errorf("command failed"))

		err := app.ExecuteCommand("--invalid")
		if err == nil {
			t.Fatal("Expected error from ExecuteCommand")
		}
		if !strings.Contains(err.Error(), "opencode command execution failed") {
			t.Fatalf("Expected wrapped error, got: %v", err)
		}
	})
}

func TestDefaultThemeName(t *testing.T) {
	if DEFAULT_THEME_NAME != "default" {
		t.Errorf("Expected DEFAULT_THEME_NAME to be 'default', got %q", DEFAULT_THEME_NAME)
	}
}

// OpenCode 1 left resolved plugin dependencies (node_modules, package.json,
// a lockfile) in this directory. They are not devgeta's: ForceConfigure
// clears only what devgeta generates, so they survive.
func TestForceConfigure_PreservesOpenCodeDependencyState(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	appConfigDir := filepath.Join(tc.AppDir, "configs", "opencode")
	userConfigDir := filepath.Join(tc.ConfigDir, "opencode")
	if err := os.MkdirAll(appConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "opencode.json.tmpl"),
		[]byte(`{"permissions": []}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeOpenCodeThemeTemplateFixture(t, appConfigDir, `{"name": "t"}`)
	writePluginFixtures(t, appConfigDir)

	oldAppConfigs := paths.Paths.App.Configs.OpenCode
	t.Cleanup(func() { paths.Paths.App.Configs.OpenCode = oldAppConfigs })
	paths.Paths.App.Configs.OpenCode = appConfigDir

	oldConfigOpenCode := paths.Paths.Config.OpenCode
	t.Cleanup(func() { paths.Paths.Config.OpenCode = oldConfigOpenCode })
	paths.Paths.Config.OpenCode = userConfigDir
	isolateOpenCodeInstallPrefix(t)

	// Stand in for what a real OpenCode install leaves behind.
	vendored := filepath.Join(userConfigDir, "node_modules", "@opencode-ai", "plugin")
	if err := os.MkdirAll(vendored, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(vendored, "package.json"),
		[]byte(`{"version":"1.18.32"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"package.json":      `{"dependencies":{"@opencode-ai/plugin":"1.18.32"}}`,
		"package-lock.json": `{"lockfileVersion":3}`,
	} {
		if err := os.WriteFile(
			filepath.Join(userConfigDir, name),
			[]byte(body),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}
	// A stale artifact devgeta SHOULD still clear, proving preservation did
	// not turn into "never delete anything". themes/ is devgeta-generated.
	if err := os.MkdirAll(filepath.Join(userConfigDir, "themes"), 0o755); err != nil {
		t.Fatal(err)
	}
	staleTheme := filepath.Join(userConfigDir, "themes", "stale.json")
	if err := os.WriteFile(staleTheme, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	setupThemeFixture(t, "#ebdbb2")

	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	for _, keep := range []string{
		filepath.Join("node_modules", "@opencode-ai", "plugin", "package.json"),
		"package.json",
		"package-lock.json",
	} {
		if _, err := os.Stat(filepath.Join(userConfigDir, keep)); err != nil {
			t.Errorf("ForceConfigure destroyed dependency state %q: %v", keep, err)
		}
	}
	if _, err := os.Stat(staleTheme); !os.IsNotExist(err) {
		t.Error("ForceConfigure should still clear stale devgeta-generated files")
	}
}

// plugins/ is shared: other tools install their plugins there too. devgeta
// owns only its own folders inside it, so a --force reconfigure must leave
// everything else in plugins/ alone.
func TestForceConfigure_PreservesThirdPartyPluginsDir(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	appConfigDir := filepath.Join(tc.AppDir, "configs", "opencode")
	userConfigDir := filepath.Join(tc.ConfigDir, "opencode")
	writePluginFixtures(t, appConfigDir)
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "opencode.json.tmpl"),
		[]byte(`{"permissions": []}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeOpenCodeThemeTemplateFixture(t, appConfigDir, `{"name": "t"}`)

	oldAppConfigs := paths.Paths.App.Configs.OpenCode
	t.Cleanup(func() { paths.Paths.App.Configs.OpenCode = oldAppConfigs })
	paths.Paths.App.Configs.OpenCode = appConfigDir

	oldConfigOpenCode := paths.Paths.Config.OpenCode
	t.Cleanup(func() { paths.Paths.Config.OpenCode = oldConfigOpenCode })
	paths.Paths.Config.OpenCode = userConfigDir
	isolateOpenCodeInstallPrefix(t)

	thirdPartyPlugin := filepath.Join(userConfigDir, "plugins", "other-tool.ts")
	if err := os.MkdirAll(filepath.Dir(thirdPartyPlugin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		thirdPartyPlugin,
		[]byte("export const OtherToolPlugin = 1"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	setupThemeFixture(t, "#ebdbb2")

	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	if _, err := os.Stat(thirdPartyPlugin); err != nil {
		t.Errorf("ForceConfigure deleted the third-party plugins/ dir: %v", err)
	}
}

// A --force reconfigure must clear what devgeta generates and nothing else.
// OpenCode and its users keep their own files in this directory (tui.json,
// auth state, hand-kept backups); devgeta neither ships nor tracks them, so
// deleting them is data loss with no upside.
func TestForceConfigure_LeavesFilesDevgetaDoesNotManage(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	appConfigDir := filepath.Join(tc.AppDir, "configs", "opencode")
	userConfigDir := filepath.Join(tc.ConfigDir, "opencode")
	writePluginFixtures(t, appConfigDir)
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "opencode.json.tmpl"),
		[]byte(`{"permissions": []}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeOpenCodeThemeTemplateFixture(t, appConfigDir, `{"name": "t"}`)

	oldAppConfigs := paths.Paths.App.Configs.OpenCode
	t.Cleanup(func() { paths.Paths.App.Configs.OpenCode = oldAppConfigs })
	paths.Paths.App.Configs.OpenCode = appConfigDir

	oldConfigOpenCode := paths.Paths.Config.OpenCode
	t.Cleanup(func() { paths.Paths.Config.OpenCode = oldConfigOpenCode })
	paths.Paths.Config.OpenCode = userConfigDir
	isolateOpenCodeInstallPrefix(t)

	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Real examples seen on a live machine, none of them devgeta's.
	foreign := map[string]string{
		"tui.json":                        `{"theme":"gruvbox"}`,
		"opencode.json.tui-migration.bak": `{"old":"config"}`,
	}
	for name, body := range foreign {
		if err := os.WriteFile(
			filepath.Join(userConfigDir, name),
			[]byte(body),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}
	// A devgeta-generated file that MUST still be replaced, so the test can
	// tell "preserves everything" apart from "preserves the right things".
	if err := os.WriteFile(
		filepath.Join(userConfigDir, "opencode.json"),
		[]byte(`{"stale":true}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	setupThemeFixture(t, "#ebdbb2")

	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	for name, want := range foreign {
		got, err := os.ReadFile(filepath.Join(userConfigDir, name))
		if err != nil {
			t.Errorf("ForceConfigure deleted %q, which devgeta does not manage: %v", name, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want it untouched (%q)", name, got, want)
		}
	}
	regenerated, err := os.ReadFile(filepath.Join(userConfigDir, "opencode.json"))
	if err != nil {
		t.Fatalf("opencode.json missing: %v", err)
	}
	if strings.Contains(string(regenerated), "stale") {
		t.Error("opencode.json was preserved but devgeta generates it — it must be regenerated")
	}
}

// writePluginFixtures gives the fixture app config dir one folder per devgeta
// plugin, the way configs/opencode/plugins/ ships them.
func writePluginFixtures(t *testing.T, appConfigDir string) {
	t.Helper()
	for _, name := range devgetaPlugins {
		dir := filepath.Join(appConfigDir, "plugins", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(dir, "server.js"),
			[]byte(`export default { id: "fixture", setup() {} };`),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}
}

// cliThemeName reads theme.name out of the deployed cli.json.
func cliThemeName(t *testing.T, userConfigDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(userConfigDir, "cli.json"))
	if err != nil {
		t.Fatalf("failed to read cli.json: %v", err)
	}
	var cli struct {
		Theme struct {
			Name string `json:"name"`
		} `json:"theme"`
	}
	if err := json.Unmarshal(data, &cli); err != nil {
		t.Fatalf("cli.json is not valid JSON: %v", err)
	}
	return cli.Theme.Name
}

// setupConfigureFixture isolates every path ForceConfigure touches and
// returns the user config dir it writes into.
func setupConfigureFixture(t *testing.T) (*testutil.TestConfig, string) {
	t.Helper()
	testutil.IsolateXDGDirs(t)
	tc := testutil.SetupCompleteTest(t)
	t.Cleanup(tc.Cleanup)

	appConfigDir := filepath.Join(tc.AppDir, "configs", "opencode")
	userConfigDir := filepath.Join(tc.ConfigDir, "opencode")
	if err := os.MkdirAll(appConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "opencode.json.tmpl"),
		[]byte(`{"permissions": []}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeOpenCodeThemeTemplateFixture(t, appConfigDir, `{"name": "t"}`)
	writePluginFixtures(t, appConfigDir)

	oldAppConfigs := paths.Paths.App.Configs.OpenCode
	t.Cleanup(func() { paths.Paths.App.Configs.OpenCode = oldAppConfigs })
	paths.Paths.App.Configs.OpenCode = appConfigDir

	oldConfigOpenCode := paths.Paths.Config.OpenCode
	t.Cleanup(func() { paths.Paths.Config.OpenCode = oldConfigOpenCode })
	paths.Paths.Config.OpenCode = userConfigDir
	isolateOpenCodeInstallPrefix(t)
	setupThemeFixture(t, "#ebdbb2")
	return tc, userConfigDir
}

// cli.json holds the user's own TUI settings; devgeta sets theme.name in it
// and nothing else.
func TestForceConfigure_SelectsThemeInCliJSONAndKeepsUserSettings(t *testing.T) {
	tc, userConfigDir := setupConfigureFixture(t)
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(userConfigDir, "cli.json"),
		[]byte(`{"theme":{"name":"opencode","mode":"dark"},"animations":false,"diffs":{"wrap":"word"}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(userConfigDir, "cli.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cli map[string]any
	if err := json.Unmarshal(data, &cli); err != nil {
		t.Fatal(err)
	}
	theme := cli["theme"].(map[string]any)
	if theme["name"] != "default" || theme["mode"] != "dark" {
		t.Errorf("theme = %v, want name 'default' with mode kept", theme)
	}
	if cli["animations"] != false || cli["diffs"] == nil {
		t.Errorf("user settings not kept: %s", data)
	}
	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

// OpenCode 1 plugins lived in plugin/ (singular), which only devgeta ever
// wrote. OpenCode 2 cannot run them, so a reconfigure removes the folder.
func TestForceConfigure_RemovesOpenCode1PluginDir(t *testing.T) {
	tc, userConfigDir := setupConfigureFixture(t)
	legacy := filepath.Join(userConfigDir, "plugin", "notify.js")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("export const Notify = 1"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(userConfigDir, "plugin")); !os.IsNotExist(err) {
		t.Errorf("expected the OpenCode 1 plugin/ folder to be removed, stat err = %v", err)
	}
}

// A stale file inside a devgeta plugin folder is cleared on --force: the
// folder is devgeta's alone.
func TestForceConfigure_ReplacesDevgetaPluginFolders(t *testing.T) {
	tc, userConfigDir := setupConfigureFixture(t)
	stale := filepath.Join(userConfigDir, "plugins", devgetaPlugins[0], "stale.js")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("expected stale file in a devgeta plugin folder to be removed, stat err = %v", err)
	}
}

// devgetaPlugins drives both deploy and cleanup, so it must name exactly the
// folders configs/opencode/plugins/ ships: a folder missing from the list
// would never deploy, and a name with no folder would fail every configure.
func TestDevgetaPluginsMatchShippedFolders(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "..", "configs", "opencode", "plugins"))
	if err != nil {
		t.Fatal(err)
	}
	var shipped []string
	for _, e := range entries {
		if e.IsDir() {
			shipped = append(shipped, e.Name())
		}
	}
	want := slices.Clone(devgetaPlugins)
	slices.Sort(want)
	slices.Sort(shipped)
	if !slices.Equal(shipped, want) {
		t.Errorf("configs/opencode/plugins ships %v, devgetaPlugins lists %v", shipped, want)
	}
}

// A coder's uninstall deletes its config directory, which takes every pack's
// OpenCode side with it, so the pack records must follow (ADR-0070 §6).
func TestUninstallForgetsAgentPacks(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	prefix := isolateOpenCodeInstallPrefix(t)
	writeOpenCodeBinaryFixture(t, prefix)

	oldCfg, oldPacks := paths.Paths.Config.OpenCode, paths.Paths.App.AgentPacks
	t.Cleanup(func() {
		paths.Paths.Config.OpenCode, paths.Paths.App.AgentPacks = oldCfg, oldPacks
	})
	paths.Paths.Config.OpenCode = filepath.Join(tc.ConfigDir, "opencode")
	paths.Paths.App.AgentPacks = filepath.Join(tc.AppDir, "agent-packs")
	if err := os.MkdirAll(filepath.Join(paths.Paths.App.AgentPacks, "devgeta"), 0o755); err != nil {
		t.Fatal(err)
	}

	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatal(err)
	}
	if err := gc.Load(); err != nil {
		t.Fatal(err)
	}
	gc.AgentPacks = map[string]config.AgentPackState{
		"devgeta": {Coders: map[string]config.AgentPackCoderState{
			"opencode": {OpenCodeInstructions: []string{"/d/DEVGETA.md"}},
		}},
		"superpowers": {Coders: map[string]config.AgentPackCoderState{
			"opencode": {OpenCodePlugins: []string{"/p"}}, "claude": {Plugin: "superpowers@devgeta"},
		}},
	}
	if err := gc.Save(); err != nil {
		t.Fatal(err)
	}

	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}

	after := &config.GlobalConfig{}
	if err := after.Load(); err != nil {
		t.Fatal(err)
	}
	if _, ok := after.AgentPacks["devgeta"]; ok {
		t.Error("a pack only OpenCode had should be gone from state")
	}
	if _, ok := after.AgentPacks["superpowers"].Coders["opencode"]; ok {
		t.Error("OpenCode's record should be gone from the other packs")
	}
	if _, ok := after.AgentPacks["superpowers"].Coders["claude"]; !ok {
		t.Error("Claude's record must stay")
	}
	if _, err := os.Stat(filepath.Join(paths.Paths.App.AgentPacks, "devgeta")); !os.IsNotExist(err) {
		t.Errorf("the built tree of the orphaned pack should be deleted, stat err: %v", err)
	}
}

const packEchoTemplate = `{{.ScratchDirGlob}}|{{range .Plugins}}{{.}},{{end}}|{{range .Instructions}}{{.}},{{end}}`

// withPackEchoTemplate swaps the fixture's opencode.json.tmpl for one that
// prints the pack fields, so a test sees exactly what the app passed in.
func withPackEchoTemplate(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(paths.Paths.App.Configs.OpenCode, "opencode.json.tmpl"),
		[]byte(packEchoTemplate), 0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func readOpenCodeJSON(t *testing.T, userConfigDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(userConfigDir, "opencode.json"))
	if err != nil {
		t.Fatalf("failed to read opencode.json: %v", err)
	}
	return string(data)
}

func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestForceConfigure_RendersPackEntriesFromState(t *testing.T) {
	tc, userConfigDir := setupConfigureFixture(t)
	withPackEchoTemplate(t)
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatal(err)
	}
	if err := gc.Load(); err != nil {
		t.Fatal(err)
	}
	gc.AgentPacks = map[string]config.AgentPackState{
		"superpowers": {Coders: map[string]config.AgentPackCoderState{
			"opencode": {OpenCodePlugins: []string{"/p/superpowers"}},
			"claude":   {Plugin: "superpowers@devgeta"},
		}},
		"ponytail": {Coders: map[string]config.AgentPackCoderState{
			"opencode": {OpenCodePlugins: []string{"/p/ponytail/.opencode/plugins"}},
		}},
		"devgeta": {Coders: map[string]config.AgentPackCoderState{
			"opencode": {OpenCodeInstructions: []string{"/p/devgeta/DEVGETA.md"}},
		}},
		// Only in Pi: nothing for OpenCode to load.
		"pi-only": {Coders: map[string]config.AgentPackCoderState{"pi": {PiPackage: "/q"}}},
	}
	if err := gc.Save(); err != nil {
		t.Fatal(err)
	}

	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	scratch, err := paths.EnsureScratchDir()
	if err != nil {
		t.Fatal(err)
	}
	// Packs in name order: devgeta, ponytail, superpowers.
	want := jsonString(t, scratch+"/**") + "|" +
		jsonString(t, "/p/ponytail/.opencode/plugins") + "," + jsonString(t, "/p/superpowers") + "," + "|" +
		jsonString(t, "/p/devgeta/DEVGETA.md") + ","
	if got := readOpenCodeJSON(t, userConfigDir); got != want {
		t.Errorf("opencode.json rendered from state:\n got %s\nwant %s", got, want)
	}
}

func TestForceConfigure_WithoutPacksRendersNoPackData(t *testing.T) {
	tc, userConfigDir := setupConfigureFixture(t)
	withPackEchoTemplate(t)

	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	got := readOpenCodeJSON(t, userConfigDir)
	if !strings.HasSuffix(got, "||") {
		t.Errorf("no packs means no plugins and no instructions, got %q", got)
	}
}

func TestConfigureWithPacks_RendersTheGivenStateNotTheSavedOne(t *testing.T) {
	tc, userConfigDir := setupConfigureFixture(t)
	withPackEchoTemplate(t)
	// Saved state has nothing; the engine passes the state as it will be.
	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	err := app.ConfigureWithPacks(map[string]config.AgentPackState{
		"superpowers": {Coders: map[string]config.AgentPackCoderState{
			"opencode": {OpenCodePlugins: []string{"/p/superpowers"}},
		}},
	})
	if err != nil {
		t.Fatalf("ConfigureWithPacks error: %v", err)
	}

	got := readOpenCodeJSON(t, userConfigDir)
	if !strings.Contains(got, jsonString(t, "/p/superpowers")) {
		t.Errorf("the given state must be rendered, got %q", got)
	}
}

// Installing a pack must re-render opencode.json and nothing else: the
// theme, cli.json and devgeta's plugin folders are `dg configure`'s business.
func TestConfigureWithPacks_TouchesOnlyOpenCodeJSON(t *testing.T) {
	tc, userConfigDir := setupConfigureFixture(t)
	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ConfigureWithPacks(nil); err != nil {
		t.Fatalf("ConfigureWithPacks error: %v", err)
	}

	entries, err := os.ReadDir(userConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "opencode.json" {
		t.Errorf("expected only opencode.json to be written, got %v", names)
	}
	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestIsInstalled(t *testing.T) {
	prefix := isolateOpenCodeInstallPrefix(t)
	notOnPath := func(string) (string, error) { return "", errors.New("not found") }
	app := &OpenCode{}

	t.Run("neither the managed binary nor PATH", func(t *testing.T) {
		stubLookPath(t, notOnPath)
		if app.IsInstalled() {
			t.Error("expected not installed")
		}
	})
	t.Run("the managed binary, which is not on PATH yet", func(t *testing.T) {
		stubLookPath(t, notOnPath)
		writeOpenCodeBinaryFixture(t, prefix)
		if !app.IsInstalled() {
			t.Error("a fresh managed install counts even before PATH has it")
		}
	})
	t.Run("an opencode from another channel", func(t *testing.T) {
		if err := os.RemoveAll(prefix); err != nil {
			t.Fatal(err)
		}
		stubLookPath(t, func(string) (string, error) { return "/usr/bin/opencode", nil })
		if !app.IsInstalled() {
			t.Error("an opencode on PATH counts")
		}
	})
}

func TestExecuteCommandOutput(t *testing.T) {
	t.Run("returns stdout and never reads stdin", func(t *testing.T) {
		isolateOpenCodeInstallPrefix(t)
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult(`[{"type":"document"}]`, "", nil)
		app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

		out, err := app.ExecuteCommandOutput("debug", "config", "--standalone")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != `[{"type":"document"}]` {
			t.Errorf("unexpected stdout %q", out)
		}
		last := mockApp.Base.GetLastExecCommandCall()
		if !last.NoStdin {
			t.Error("stdin must be disconnected so a query can never wait on a prompt")
		}
		if len(last.Args) != 3 || last.Args[0] != "debug" {
			t.Errorf("unexpected args %v", last.Args)
		}
	})
	t.Run("prefers the managed binary, which is not on PATH until a new shell", func(t *testing.T) {
		prefix := isolateOpenCodeInstallPrefix(t)
		writeOpenCodeBinaryFixture(t, prefix)
		mockApp := testutil.NewMockApp()
		app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if _, err := app.ExecuteCommandOutput("--version"); err != nil {
			t.Fatal(err)
		}
		if got := mockApp.Base.GetLastExecCommandCall().Command; got != filepath.Join(prefix, "bin", "opencode") {
			t.Errorf("expected the managed binary, got %q", got)
		}
	})
	t.Run("falls back to PATH", func(t *testing.T) {
		isolateOpenCodeInstallPrefix(t)
		mockApp := testutil.NewMockApp()
		app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if _, err := app.ExecuteCommandOutput("--version"); err != nil {
			t.Fatal(err)
		}
		if got := mockApp.Base.GetLastExecCommandCall().Command; got != constants.OpenCode {
			t.Errorf("expected %q, got %q", constants.OpenCode, got)
		}
	})
	t.Run("wraps a failure and keeps stderr", func(t *testing.T) {
		isolateOpenCodeInstallPrefix(t)
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "bad flag", fmt.Errorf("exit 1"))
		app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

		_, err := app.ExecuteCommandOutput("--nope")
		if err == nil || !strings.Contains(err.Error(), "bad flag") ||
			!strings.Contains(err.Error(), "opencode command execution failed") {
			t.Fatalf("expected a wrapped error with stderr, got %v", err)
		}
	})
}

func TestConfigureWithPacks_IsIdempotentAndRemovalLeavesOtherPacksAlone(t *testing.T) {
	tc, userConfigDir := setupConfigureFixture(t)
	withPackEchoTemplate(t)
	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	both := map[string]config.AgentPackState{
		"superpowers": {Coders: map[string]config.AgentPackCoderState{"opencode": {OpenCodePlugins: []string{"/p/superpowers"}}}},
		"ponytail":    {Coders: map[string]config.AgentPackCoderState{"opencode": {OpenCodePlugins: []string{"/p/ponytail/.opencode/plugins"}}}},
	}

	if err := app.ConfigureWithPacks(both); err != nil {
		t.Fatal(err)
	}
	first := readOpenCodeJSON(t, userConfigDir)
	if err := app.ConfigureWithPacks(both); err != nil {
		t.Fatal(err)
	}
	if again := readOpenCodeJSON(t, userConfigDir); again != first {
		t.Errorf("adding the same state again must change nothing:\n%s\n%s", first, again)
	}

	delete(both, "superpowers")
	if err := app.ConfigureWithPacks(both); err != nil {
		t.Fatal(err)
	}
	got := readOpenCodeJSON(t, userConfigDir)
	if strings.Contains(got, "/p/superpowers") || !strings.Contains(got, "/p/ponytail/.opencode/plugins") {
		t.Errorf("removing superpowers must leave ponytail alone, got %q", got)
	}
}

func TestRenderConfig_ListsPackSkills(t *testing.T) {
	packs := map[string]config.AgentPackState{
		"superpowers": {Coders: map[string]config.AgentPackCoderState{
			"opencode": {OpenCodeSkills: []string{"/b/skills"}},
		}},
		"devgeta": {Coders: map[string]config.AgentPackCoderState{
			"opencode": {OpenCodeSkills: []string{"/a/skills", `/q"uote/skills`}},
		}},
	}
	var d configTemplateData
	if err := d.addPacks(packs); err != nil {
		t.Fatal(err)
	}
	tmpl, err := os.ReadFile(filepath.Join("..", "..", "..", "configs", "opencode", "opencode.json.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	render := func(d configTemplateData) string {
		t.Helper()
		var out strings.Builder
		if err := template.Must(template.New("x").Parse(string(tmpl))).Execute(&out, d); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}

	want := `"skills": ["/a/skills", "/q\"uote/skills", "/b/skills"],`
	if got := render(d); !strings.Contains(got, want) {
		t.Errorf("rendered config lacks %s (pack-name order, quotes escaped):\n%s", want, got)
	}
	if got := render(configTemplateData{ScratchDirGlob: `"/s/**"`}); strings.Contains(got, `"skills"`) {
		t.Error("no packs must render no skills key")
	}
}
