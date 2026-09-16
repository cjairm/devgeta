package opencode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
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

func TestForceConfigureParts(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	src := t.TempDir()
	for _, f := range []string{"skills/demo/SKILL.md", "commands/x.md"} {
		p := filepath.Join(src, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldShared := paths.Paths.App.Configs.Shared
	t.Cleanup(func() { paths.Paths.App.Configs.Shared = oldShared })
	paths.Paths.App.Configs.Shared = src

	ocDir := filepath.Join(tc.ConfigDir, "opencode")
	oldOC := paths.Paths.Config.OpenCode
	t.Cleanup(func() { paths.Paths.Config.OpenCode = oldOC })
	paths.Paths.Config.OpenCode = ocDir

	app := &OpenCode{}
	if err := app.ForceConfigureParts([]string{"skills"}); err != nil {
		t.Fatalf("ForceConfigureParts error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(ocDir, "skills", "demo", "SKILL.md")); err != nil {
		t.Fatalf("expected skills synced: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ocDir, "commands")); !os.IsNotExist(err) {
		t.Error("commands should not be synced when only skills was requested")
	}
	// The --only path must not regenerate opencode.json.
	if _, err := os.Stat(filepath.Join(ocDir, "opencode.json")); !os.IsNotExist(err) {
		t.Error("ForceConfigureParts should not write opencode.json")
	}
}

func setupSharedDir(t *testing.T, baseDir string) {
	t.Helper()
	sharedDir := filepath.Join(baseDir, "configs", "shared")
	for _, sub := range []string{"skills", "commands", "agents"} {
		if err := os.MkdirAll(filepath.Join(sharedDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldShared := paths.Paths.App.Configs.Shared
	t.Cleanup(func() { paths.Paths.App.Configs.Shared = oldShared })
	paths.Paths.App.Configs.Shared = sharedDir
}

// setupOutputBudgetRunnerSource stubs the runner source
// baseapp.EnsureAgentRuntime deploys — configs/devgeta/, its own agent-
// neutral source dir (not configs/claude/: output-budget.sh, the HOOK,
// lives there, but the shared runner does not) — so OpenCode's own
// ForceConfigure, which also calls EnsureAgentRuntime (cycle doc Step 5),
// has something to copy even though these tests otherwise only stub
// configs/opencode/.
func setupOutputBudgetRunnerSource(t *testing.T, baseDir string) {
	t.Helper()
	devgetaConfigDir := filepath.Join(baseDir, "configs", "devgeta")
	if err := os.MkdirAll(devgetaConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(devgetaConfigDir, "output-budget-run.sh"),
		[]byte("#!/usr/bin/env bash\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	oldDevgeta := paths.Paths.App.Configs.Devgeta
	t.Cleanup(func() { paths.Paths.App.Configs.Devgeta = oldDevgeta })
	paths.Paths.App.Configs.Devgeta = devgetaConfigDir
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
	t.Run("skips when the script's own binary is present, even off PATH", func(t *testing.T) {
		prefix := isolateOpenCodeInstallPrefix(t)
		writeOpenCodeBinaryFixture(t, prefix)
		stubLookPath(t, func(string) (string, error) {
			return "", fmt.Errorf("not found")
		})

		mockApp := testutil.NewMockApp()
		app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("SoftInstall error: %v", err)
		}
		if got := mockApp.Base.GetExecCommandCallCount(); got != 0 {
			t.Errorf("expected no commands when opencode is already installed, got %d", got)
		}
		if mockApp.Cmd.MaybeInstalled != "" || mockApp.Cmd.InstalledPkg != "" {
			t.Errorf(
				"expected no package-manager calls, got maybe=%q install=%q",
				mockApp.Cmd.MaybeInstalled, mockApp.Cmd.InstalledPkg,
			)
		}
	})

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
  "theme": "{{ .Theme }}",
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

		pluginDir := filepath.Join(appConfigDir, "plugin")
		if err := os.MkdirAll(pluginDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(pluginDir, "task-redirect.js"),
			[]byte(`export const TaskRedirect = async () => ({});`),
			0o644,
		); err != nil {
			t.Fatal(err)
		}

		setupSharedDir(t, tc.AppDir)
		setupOutputBudgetRunnerSource(t, tc.AppDir)

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
		if !strings.Contains(configStr, `"theme": "default"`) {
			t.Errorf("Expected theme to be 'default', got: %s", configStr)
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

		// task-redirect.js plugin deployed
		pluginPath := filepath.Join(userConfigDir, "plugin", "task-redirect.js")
		if _, err := os.Stat(pluginPath); err != nil {
			t.Fatalf("Expected plugin file at %s: %v", pluginPath, err)
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
		if err := os.MkdirAll(filepath.Join(appConfigDir, "plugin"), 0o755); err != nil {
			t.Fatal(err)
		}

		templatePath := filepath.Join(appConfigDir, "opencode.json.tmpl")
		if err := os.WriteFile(
			templatePath,
			[]byte(`{"theme": "{{ .Theme }}"}`),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
		themeSourcePath := filepath.Join(appConfigDir, "themes", "default.json.tmpl")
		if err := os.WriteFile(themeSourcePath, []byte(`{"name": "test"}`), 0o644); err != nil {
			t.Fatal(err)
		}

		if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
			t.Fatal(err)
		}
		oldFilePath := filepath.Join(userConfigDir, "old-file.json")
		if err := os.WriteFile(oldFilePath, []byte("old content"), 0o644); err != nil {
			t.Fatal(err)
		}

		setupSharedDir(t, tc.AppDir)
		setupOutputBudgetRunnerSource(t, tc.AppDir)
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
// from "default", both opencode.json's "theme" field and the rendered
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
		[]byte(`{"theme": "{{ .Theme }}"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	writeOpenCodeThemeTemplateFixture(t, appConfigDir, `{"foreground": "{{.Palette.Foreground}}"}`)

	pluginDir := filepath.Join(appConfigDir, "plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}

	setupSharedDir(t, tc.AppDir)
	setupOutputBudgetRunnerSource(t, tc.AppDir)

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

	configContent, err := os.ReadFile(filepath.Join(userConfigDir, "opencode.json"))
	if err != nil {
		t.Fatalf("failed to read opencode.json: %v", err)
	}
	if !strings.Contains(string(configContent), `"theme": "custom"`) {
		t.Errorf("expected opencode.json to name the current theme, got: %s", configContent)
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
		if err := os.MkdirAll(filepath.Join(appConfigDir, "plugin"), 0o755); err != nil {
			t.Fatal(err)
		}

		templatePath := filepath.Join(appConfigDir, "opencode.json.tmpl")
		if err := os.WriteFile(
			templatePath,
			[]byte(`{"theme": "{{ .Theme }}"}`),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
		themeSourcePath := filepath.Join(appConfigDir, "themes", "default.json.tmpl")
		if err := os.WriteFile(themeSourcePath, []byte(`{"name": "test"}`), 0o644); err != nil {
			t.Fatal(err)
		}

		setupSharedDir(t, tc.AppDir)
		setupOutputBudgetRunnerSource(t, tc.AppDir)
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
		if err := os.MkdirAll(filepath.Join(appConfigDir, "plugin"), 0o755); err != nil {
			t.Fatal(err)
		}
		templatePath := filepath.Join(appConfigDir, "opencode.json.tmpl")
		if err := os.WriteFile(
			templatePath,
			[]byte(`{"theme": "{{ .Theme }}"}`),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
		themeSourcePath := filepath.Join(appConfigDir, "themes", "default.json.tmpl")
		if err := os.WriteFile(themeSourcePath, []byte(`{"name": "test"}`), 0o644); err != nil {
			t.Fatal(err)
		}

		setupSharedDir(t, tc.AppDir)
		setupOutputBudgetRunnerSource(t, tc.AppDir)
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

func TestConfigurablePartsIncludesRtk(t *testing.T) {
	app := &OpenCode{}
	parts := app.ConfigurableParts()
	if parts[len(parts)-1] != "rtk" {
		t.Errorf("expected rtk as a configurable part, got %v", parts)
	}
	for _, p := range baseapp.SharedConfigParts {
		if p == "rtk" {
			t.Fatal("baseapp.SharedConfigParts was mutated to include rtk")
		}
	}
}

func TestForceConfigurePartsRtk(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	opencodeDir := filepath.Join(tc.ConfigDir, "opencode")
	oldOpenCode := paths.Paths.Config.OpenCode
	t.Cleanup(func() { paths.Paths.Config.OpenCode = oldOpenCode })
	paths.Paths.Config.OpenCode = opencodeDir

	rtkInitCalled := 0
	app := &OpenCode{rtkInit: func() error {
		rtkInitCalled++
		return nil
	}}

	if err := app.ForceConfigureParts([]string{"rtk"}); err != nil {
		t.Fatalf("ForceConfigureParts(rtk) error: %v", err)
	}
	if rtkInitCalled != 1 {
		t.Errorf("expected rtk init to run once, got %d", rtkInitCalled)
	}
	// The --only=rtk path must not touch opencode.json.
	if _, err := os.Stat(filepath.Join(opencodeDir, "opencode.json")); !os.IsNotExist(err) {
		t.Error("ForceConfigureParts(rtk) should not write opencode.json")
	}
}

func TestForceConfigurePartsRtkInitFailure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	opencodeDir := filepath.Join(tc.ConfigDir, "opencode")
	oldOpenCode := paths.Paths.Config.OpenCode
	t.Cleanup(func() { paths.Paths.Config.OpenCode = oldOpenCode })
	paths.Paths.Config.OpenCode = opencodeDir

	app := &OpenCode{rtkInit: func() error { return fmt.Errorf("rtk not found") }}

	err := app.ForceConfigureParts([]string{"rtk"})
	if err == nil {
		t.Fatal("expected error when rtk init fails")
	}
	if !strings.Contains(err.Error(), "dg install --only rtk") {
		t.Errorf("expected install hint in error, got: %v", err)
	}
}

func TestForceConfigurePartsRtkRefusesRealExecInTests(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	opencodeDir := filepath.Join(tc.ConfigDir, "opencode")
	oldOpenCode := paths.Paths.Config.OpenCode
	t.Cleanup(func() { paths.Paths.Config.OpenCode = oldOpenCode })
	paths.Paths.Config.OpenCode = opencodeDir

	// No rtkInit injected: the guard must refuse instead of executing rtk.
	app := &OpenCode{}
	err := app.ForceConfigureParts([]string{"rtk"})
	if err == nil || !strings.Contains(err.Error(), "refusing to run real") {
		t.Fatalf("expected test-guard refusal, got: %v", err)
	}
}

func TestRun(t *testing.T) {
	t.Run("WithModelAndEnv", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}
		mockApp.Base.SetExecCommandResult(`{"type":"text"}`, "", nil)

		out, err := app.Run(RunOptions{
			Agent:   "code-reviewer",
			Model:   "openai/gpt-5.2",
			Prompt:  "review this branch",
			Dir:     "/tmp/some-worktree",
			Timeout: 30 * time.Minute,
			Env:     []string{"DEVGETA_REVIEW_JOURNAL_SNAPSHOT=/tmp/snap.md"},
		})
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		if out.Stdout != `{"type":"text"}` {
			t.Fatalf("expected stdout to be returned, got %q", out.Stdout)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("expected ExecCommand to be called")
		}
		if lastCall.Command != constants.OpenCode {
			t.Fatalf("expected command %q, got %q", constants.OpenCode, lastCall.Command)
		}
		wantArgs := []string{
			"run", "--agent", "code-reviewer", "--format", "json",
			"-m", "openai/gpt-5.2", "review this branch",
		}
		if !reflect.DeepEqual(lastCall.Args, wantArgs) {
			t.Fatalf("expected args %v, got %v", wantArgs, lastCall.Args)
		}
		if lastCall.Dir != "/tmp/some-worktree" {
			t.Fatalf("expected Dir to land on params, got %q", lastCall.Dir)
		}
		if lastCall.Timeout != 30*time.Minute {
			t.Fatalf("expected Timeout to land on params, got %v", lastCall.Timeout)
		}
		wantEnv := []string{"DEVGETA_REVIEW_JOURNAL_SNAPSHOT=/tmp/snap.md"}
		if !reflect.DeepEqual(lastCall.Env, wantEnv) {
			t.Fatalf("expected Env %v, got %v", wantEnv, lastCall.Env)
		}
		// A headless run must never be handed a terminal: a prompt it decided
		// to raise could only wedge the caller until the timeout. It is also
		// what lets the executor put the run in its own process group, so the
		// timeout kills the whole tree the agent spawns instead of just the
		// `opencode` process.
		if !lastCall.NoStdin {
			t.Fatal("expected a headless run to disconnect stdin")
		}

		// Exactly one call, and it went through the mock — never a real
		// binary invocation.
		if mockApp.Base.GetExecCommandCallCount() != 1 {
			t.Fatalf(
				"expected exactly 1 ExecCommand call, got %d",
				mockApp.Base.GetExecCommandCallCount(),
			)
		}
	})

	t.Run("WithoutModelWithoutEnv", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}
		mockApp.Base.SetExecCommandResult(`{"type":"text"}`, "", nil)

		out, err := app.Run(RunOptions{
			Agent:   "code-reviewer",
			Prompt:  "review this branch",
			Dir:     "/tmp/some-worktree",
			Timeout: 30 * time.Minute,
		})
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		if out.Stdout != `{"type":"text"}` {
			t.Fatalf("expected stdout to be returned, got %q", out.Stdout)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("expected ExecCommand to be called")
		}
		// No -m flag when Model is empty.
		wantArgs := []string{
			"run", "--agent", "code-reviewer", "--format", "json", "review this branch",
		}
		if !reflect.DeepEqual(lastCall.Args, wantArgs) {
			t.Fatalf("expected args %v, got %v", wantArgs, lastCall.Args)
		}
		// Env left nil (not empty-non-nil) so ExecCommand's overlay stays
		// off and the child keeps today's full inheritance — see
		// CommandParams.Env's doc comment.
		if len(lastCall.Env) != 0 {
			t.Fatalf("expected no Env overlay, got %v", lastCall.Env)
		}

		if mockApp.Base.GetExecCommandCallCount() != 1 {
			t.Fatalf(
				"expected exactly 1 ExecCommand call, got %d",
				mockApp.Base.GetExecCommandCallCount(),
			)
		}
	})

	t.Run("PropagatesErrorButStillReturnsStdout", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}
		mockApp.Base.SetExecCommandResult(
			`{"type":"error"}`,
			"stderr noise",
			fmt.Errorf("exit status 1"),
		)

		out, err := app.Run(RunOptions{Agent: "code-reviewer", Prompt: "review"})
		if err == nil {
			t.Fatal("expected Run to return an error")
		}
		if !strings.Contains(err.Error(), "opencode run failed") {
			t.Fatalf("expected wrapped error, got: %v", err)
		}
		// Partial/diagnostic output must still be handed back to the caller
		// even on failure — a nonzero exit can still carry an error event.
		if out.Stdout != `{"type":"error"}` {
			t.Fatalf("expected stdout on error path, got %q", out.Stdout)
		}
	})
}

// TestForceConfigure_OpenCodeOnlyProducesAWorkingOutputBudgetRuntime is the
// cycle-level case Step 5's design exists to protect against: on a machine
// with no Claude config at all, `dg configure opencode --force` alone must
// still produce a working runner and a valid sidecar (cycle doc Step 6,
// "the runner exists at paths.Paths.Config.Devgeta ... with no Claude
// config present"). An earlier draft had only claude.go deploy the runner,
// which broke exactly this.
func TestForceConfigure_OpenCodeOnlyProducesAWorkingOutputBudgetRuntime(t *testing.T) {
	testutil.IsolateXDGDirs(t)
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	appConfigDir := filepath.Join(tc.AppDir, "configs", "opencode")
	userConfigDir := filepath.Join(tc.ConfigDir, "opencode")

	if err := os.MkdirAll(filepath.Join(appConfigDir, "themes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "opencode.json.tmpl"),
		[]byte(`{"theme": "{{ .Theme }}"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "themes", "default.json.tmpl"),
		[]byte(`{}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(appConfigDir, "plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}

	setupSharedDir(t, tc.AppDir)
	setupOutputBudgetRunnerSource(t, tc.AppDir)
	setupThemeFixture(t, "#282828")

	oldAppConfigsOpenCode := paths.Paths.App.Configs.OpenCode
	t.Cleanup(func() { paths.Paths.App.Configs.OpenCode = oldAppConfigsOpenCode })
	paths.Paths.App.Configs.OpenCode = appConfigDir

	oldConfigOpenCode := paths.Paths.Config.OpenCode
	t.Cleanup(func() { paths.Paths.Config.OpenCode = oldConfigOpenCode })
	paths.Paths.Config.OpenCode = userConfigDir

	// EnsureAgentRuntime writes here, independent of paths.Paths.Config.Root
	// (Devgeta is its own already-resolved field, not derived from Root at
	// read time) — override it explicitly so this test can assert on it.
	devgetaDeployDir := filepath.Join(t.TempDir(), "devgeta")
	oldConfigDevgeta := paths.Paths.Config.Devgeta
	t.Cleanup(func() { paths.Paths.Config.Devgeta = oldConfigDevgeta })
	paths.Paths.Config.Devgeta = devgetaDeployDir

	// No paths.Paths.Config.Claude or App.Configs.Claude override at all —
	// this is the point: no Claude config exists anywhere in this test.

	app := &OpenCode{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	runnerPath := filepath.Join(devgetaDeployDir, "output-budget-run.sh")
	info, err := os.Stat(runnerPath)
	if err != nil {
		t.Fatalf("expected the runner to be deployed to %s: %v", runnerPath, err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("deployed runner is not executable: mode %o", info.Mode().Perm())
	}

	sidecarData, err := os.ReadFile(filepath.Join(devgetaDeployDir, "agent-runtime.json"))
	if err != nil {
		t.Fatalf("expected a sidecar at %s: %v", devgetaDeployDir, err)
	}
	var sidecar map[string]any
	if err := json.Unmarshal(sidecarData, &sidecar); err != nil {
		t.Fatalf("sidecar is not valid JSON: %v\n%s", err, sidecarData)
	}
	if sidecar["runner"] != runnerPath {
		t.Errorf("sidecar runner = %v, want %q", sidecar["runner"], runnerPath)
	}
}
