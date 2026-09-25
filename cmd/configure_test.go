package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/registry"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
)

func init() { testutil.InitLogger() }

// mockConfigureApp is a minimal apps.App that records which configure method was called.
type mockConfigureApp struct {
	forceCalled bool
	softCalled  bool
	forceErr    error
	softErr     error
}

func (m *mockConfigureApp) Name() string                   { return "mock" }
func (m *mockConfigureApp) Kind() apps.AppKind             { return apps.KindTerminal }
func (m *mockConfigureApp) Install() error                 { return nil }
func (m *mockConfigureApp) ForceInstall() error            { return nil }
func (m *mockConfigureApp) SoftInstall() error             { return nil }
func (m *mockConfigureApp) Uninstall() error               { return nil }
func (m *mockConfigureApp) Update() error                  { return nil }
func (m *mockConfigureApp) ExecuteCommand(...string) error { return nil }

func (m *mockConfigureApp) ForceConfigure() error {
	m.forceCalled = true
	return m.forceErr
}

func (m *mockConfigureApp) SoftConfigure() error {
	m.softCalled = true
	return m.softErr
}

func setupConfigureCmd(t *testing.T, mock apps.App) func() {
	t.Helper()
	origApp := getAppFn
	origRefresh := refreshEmbeddedConfigs
	getAppFn = func(name string) (apps.App, error) { return mock, nil }
	refreshEmbeddedConfigs = func() error { return nil }
	return func() {
		getAppFn = origApp
		refreshEmbeddedConfigs = origRefresh
	}
}

func TestConfigure_SoftPath(t *testing.T) {
	mock := &mockConfigureApp{}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = false
	err := runConfigure(configureCmd, []string{"git"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !mock.softCalled {
		t.Error("expected SoftConfigure to be called")
	}
	if mock.forceCalled {
		t.Error("expected ForceConfigure NOT to be called")
	}
}

func TestConfigure_ForcePath(t *testing.T) {
	mock := &mockConfigureApp{}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = true
	defer func() { configureForce = false }()

	err := runConfigure(configureCmd, []string{"git"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !mock.forceCalled {
		t.Error("expected ForceConfigure to be called")
	}
	if mock.softCalled {
		t.Error("expected SoftConfigure NOT to be called")
	}
}

func TestConfigure_UnknownApp(t *testing.T) {
	origApp := getAppFn
	origRefresh := refreshEmbeddedConfigs
	getAppFn = func(name string) (apps.App, error) {
		return nil, fmt.Errorf("unknown app %q", name)
	}
	refreshEmbeddedConfigs = func() error { return nil }
	defer func() {
		getAppFn = origApp
		refreshEmbeddedConfigs = origRefresh
	}()

	configureForce = false
	err := runConfigure(configureCmd, []string{"notanapp"})
	if err == nil {
		t.Fatal("expected non-nil error for unknown app")
	}
}

// mockSelectiveApp also implements apps.SelectiveConfigurer.
type mockSelectiveApp struct {
	mockConfigureApp
	partsCalled bool
	parts       []string
	partsErr    error
}

func (m *mockSelectiveApp) ConfigurableParts() []string {
	return []string{"skills", "commands", "agents"}
}

func (m *mockSelectiveApp) ForceConfigureParts(parts []string) error {
	m.partsCalled = true
	m.parts = parts
	return m.partsErr
}

func TestConfigure_OnlyDispatchesToParts(t *testing.T) {
	mock := &mockSelectiveApp{}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = true
	configureOnly = []string{"skills"}
	defer func() { configureForce = false; configureOnly = nil }()

	if err := runConfigure(configureCmd, []string{"claude"}); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !mock.partsCalled {
		t.Fatal("expected ForceConfigureParts to be called")
	}
	if len(mock.parts) != 1 || mock.parts[0] != "skills" {
		t.Fatalf("expected parts [skills], got %v", mock.parts)
	}
	if mock.forceCalled || mock.softCalled {
		t.Error("expected neither ForceConfigure nor SoftConfigure with --only")
	}
}

func TestConfigure_OnlyRequiresForce(t *testing.T) {
	mock := &mockSelectiveApp{}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = false
	configureOnly = []string{"skills"}
	defer func() { configureOnly = nil }()

	err := runConfigure(configureCmd, []string{"claude"})
	if err == nil {
		t.Fatal("expected error: --only requires --force")
	}
	if mock.partsCalled {
		t.Error("expected no work when --only is used without --force")
	}
}

func TestConfigure_OnlyUnknownPart(t *testing.T) {
	mock := &mockSelectiveApp{}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = true
	configureOnly = []string{"bogus"}
	defer func() { configureForce = false; configureOnly = nil }()

	err := runConfigure(configureCmd, []string{"claude"})
	if err == nil {
		t.Fatal("expected error for unknown --only value")
	}
	if mock.partsCalled {
		t.Error("expected no work for an invalid part")
	}
}

func TestConfigure_OnlyUnsupportedApp(t *testing.T) {
	// mockConfigureApp does NOT implement SelectiveConfigurer.
	mock := &mockConfigureApp{}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = true
	configureOnly = []string{"skills"}
	defer func() { configureForce = false; configureOnly = nil }()

	err := runConfigure(configureCmd, []string{"git"})
	if err == nil {
		t.Fatal("expected error: --only not supported for this app")
	}
	if mock.forceCalled || mock.softCalled {
		t.Error("expected no configure call when --only is unsupported")
	}
}

// TestConfigure_CallsRecoverInterruptedFn asserts runConfigure sweeps for a
// crashed `dg theme set`'s leftover backups before doing any of its own
// filesystem work (cycle doc Step 5) - a command that deploys configs
// without this call would fail this test rather than a user's files.
func TestConfigure_CallsRecoverInterruptedFn(t *testing.T) {
	mock := &mockConfigureApp{}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	calls := 0
	origRecover := recoverInterruptedFn
	defer func() { recoverInterruptedFn = origRecover }()
	recoverInterruptedFn = func() (string, error) {
		calls++
		return "", nil
	}

	configureForce = false
	if err := runConfigure(configureCmd, []string{"git"}); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected recoverInterruptedFn called once, got %d", calls)
	}
}

func TestConfigure_NotSupported(t *testing.T) {
	mock := &mockConfigureApp{softErr: fmt.Errorf("%w for mock", apps.ErrConfigureNotSupported)}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = false
	err := runConfigure(configureCmd, []string{"brave"})
	if err != nil {
		t.Fatalf("ErrConfigureNotSupported should exit zero, got: %v", err)
	}
}

// TestConfigureAfterInterruptedSwitch_SurvivesNextSet proves the sweep in
// runConfigure (cycle doc Step 5) closes the interleaving where an ordinary
// `dg configure --force` writes onto a path a crashed `dg theme set` left
// mid-switch: without that sweep, this test would end with the pre-crash
// file restored over the freshly configured one, because a LATER
// `dg theme set`'s own sweep would find the dead backup/absent-marker
// sibling still sitting beside the new file and restore it. It uses the
// real Tmux app (not a fake) against `.tmux.conf`, because the bug is in
// what's left on disk, not in any mock's bookkeeping.
func TestConfigureAfterInterruptedSwitch_SurvivesNextSet(t *testing.T) {
	for _, sub := range []struct {
		name    string
		crashed bool // whether ~/.tmux.conf existed before the simulated crash
	}{
		{name: "BackupLeftBehind", crashed: true},
		{name: "AbsentMarkerLeftBehind", crashed: false},
	} {
		t.Run(sub.name, func(t *testing.T) {
			root := t.TempDir()

			origPaths := paths.Paths
			t.Cleanup(func() { paths.Paths = origPaths })
			paths.Paths.Home.Root = filepath.Join(root, "home")
			paths.Paths.App.Root = filepath.Join(root, "app")
			paths.Paths.App.Configs.Tmux = filepath.Join(root, "tmux-src")
			paths.Paths.App.Configs.Themes = filepath.Join(root, "themes")
			paths.Paths.App.Configs.Neovim = filepath.Join(root, "shipped-neovim")
			paths.Paths.App.Configs.Templates = filepath.Join(root, "templates")
			paths.Paths.Config.Nvim = filepath.Join(root, "does-not-exist-nvim")

			for _, dir := range []string{
				paths.Paths.Home.Root,
				paths.Paths.App.Root,
				paths.Paths.App.Configs.Tmux,
				paths.Paths.App.Configs.Themes,
				paths.Paths.App.Configs.Templates,
				filepath.Join(paths.Paths.App.Configs.Neovim, "lua", "devgeta", "themes"),
			} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			testutil.CreateShellConfigTemplate(t, paths.Paths.App.Configs.Templates, "")
			if err := os.WriteFile(
				filepath.Join(
					paths.Paths.App.Configs.Neovim, "lua", "devgeta", "themes", "gruvbox.lua",
				),
				[]byte("-- fixture\n"), 0o644,
			); err != nil {
				t.Fatal(err)
			}
			writeThemeSetFixture(t, paths.Paths.App.Configs.Themes, "default", "#111111", "gruvbox")

			tmplPath := filepath.Join(paths.Paths.App.Configs.Tmux, "tmux.conf.tmpl")
			if err := os.WriteFile(
				tmplPath, []byte("# post-crash configure output\n"), 0o644,
			); err != nil {
				t.Fatal(err)
			}

			config.ResetGlobalConfigCacheForTest()
			t.Cleanup(config.ResetGlobalConfigCacheForTest)

			gc := &config.GlobalConfig{}
			if err := gc.Create(); err != nil {
				t.Fatal(err)
			}
			if err := gc.Load(); err != nil {
				t.Fatal(err)
			}
			gc.AddToInstalled(constants.Tmux, registry.Meta[constants.Tmux].ItemType)
			if err := gc.Save(); err != nil {
				t.Fatal(err)
			}

			tmuxConfigPath := filepath.Join(paths.Paths.Home.Root, ".tmux.conf")
			if sub.crashed {
				if err := os.WriteFile(
					tmuxConfigPath, []byte("pre-crash-content"), 0o644,
				); err != nil {
					t.Fatal(err)
				}
			}
			// Simulate a `dg theme set` that died mid-transaction: the
			// switch was recorded in progress, and tmux's path had already
			// been backed up (or marked absent) - but current_theme was
			// never written, matching order points 2-3 of Step 5.
			if err := config.Update(func(g *config.GlobalConfig) error {
				g.PendingTheme = "tokyonight"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if err := theme.BackupPath(tmuxConfigPath); err != nil {
				t.Fatal(err)
			}

			t.Setenv("TMUX", "") // no live push in this test

			origGetApp := getAppFn
			t.Cleanup(func() { getAppFn = origGetApp })
			// Mocked Cmd/Base, not a zero value: ForceConfigure shells out
			// now (it installs the tmux plugins the rendered config
			// declares), and a nil Base would take that call to a real one.
			mockApp := testutil.NewMockApp()
			tmuxApp := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}
			getAppFn = func(name string) (apps.App, error) {
				if name != constants.Tmux {
					return nil, fmt.Errorf("unexpected app lookup: %s", name)
				}
				return tmuxApp, nil
			}

			origRefresh := refreshEmbeddedConfigs
			t.Cleanup(func() { refreshEmbeddedConfigs = origRefresh })
			refreshEmbeddedConfigs = func() error { return nil }

			origRecover := recoverInterruptedFn
			t.Cleanup(func() { recoverInterruptedFn = origRecover })
			recoverInterruptedFn = theme.RecoverInterrupted

			configureForce = true
			configureOnly = nil
			t.Cleanup(func() { configureForce = false })

			if err := runConfigure(configureCmd, []string{constants.Tmux}); err != nil {
				t.Fatalf("dg configure tmux --force failed: %v", err)
			}

			configuredContent, err := os.ReadFile(tmuxConfigPath)
			if err != nil {
				t.Fatalf("expected configure to have written %s: %v", tmuxConfigPath, err)
			}
			if theme.HasBackup(tmuxConfigPath) {
				t.Fatalf(
					"expected configure's own sweep to have cleared the crashed backup/marker",
				)
			}

			// themeGetAppFn also needs the real tmux app - dg theme set
			// resolves apps through its own package var, not getAppFn.
			origThemeGetApp := themeGetAppFn
			t.Cleanup(func() { themeGetAppFn = origThemeGetApp })
			themeGetAppFn = func(name string) (apps.App, error) {
				if name != constants.Tmux {
					return nil, fmt.Errorf("unexpected app lookup: %s", name)
				}
				return tmuxApp, nil
			}

			if err := runThemeSet(themeSetCmd, []string{"default"}); err != nil {
				t.Fatalf("dg theme set default failed: %v", err)
			}

			finalContent, err := os.ReadFile(tmuxConfigPath)
			if err != nil {
				t.Fatalf("expected %s to still exist: %v", tmuxConfigPath, err)
			}
			if string(finalContent) == "pre-crash-content" {
				t.Fatalf(
					"dg theme set restored the pre-crash content over configure's work - " +
						"the sweep in runConfigure did not run before configure's own write",
				)
			}
			_ = configuredContent

			gcAfter := &config.GlobalConfig{}
			if err := gcAfter.Load(); err != nil {
				t.Fatal(err)
			}
			if gcAfter.PendingTheme != "" {
				t.Errorf("expected pending_theme empty, got %q", gcAfter.PendingTheme)
			}
			if theme.HasBackup(tmuxConfigPath) {
				t.Errorf("expected no leftover backup/marker after dg theme set")
			}
		})
	}
}
