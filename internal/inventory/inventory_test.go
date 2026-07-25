package inventory

import (
	"errors"
	"os"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
)

func init() { testutil.InitLogger() }

func TestCollect_PackageStates_OK_Missing_Unknown(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Cmd.PackageInstalledMap = map[string]bool{"git": true, "tmux": false}
	mockApp.Cmd.PackageInstalledErrors = map[string]error{
		"broken-pkg": errors.New("brew: not found"),
	}

	gc := &config.GlobalConfig{}
	gc.Installed.Packages = []string{"git", "tmux", "broken-pkg"}

	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	items := c.Collect(gc)

	byName := map[string]Item{}
	for _, it := range items {
		byName[it.Name] = it
	}

	if byName["git"].State != StateOK {
		t.Errorf("git: got state %v, want StateOK", byName["git"].State)
	}
	if byName["tmux"].State != StateMissing {
		t.Errorf("tmux: got state %v, want StateMissing", byName["tmux"].State)
	}
	if byName["broken-pkg"].State != StateUnknown {
		t.Errorf("broken-pkg: got state %v, want StateUnknown", byName["broken-pkg"].State)
	}
	if byName["broken-pkg"].Detail == "" {
		t.Error("broken-pkg: expected Detail to carry the check error")
	}
	for _, name := range []string{"git", "tmux", "broken-pkg"} {
		if byName[name].Category != "packages" {
			t.Errorf("%s: got category %q, want %q", name, byName[name].Category, "packages")
		}
		if byName[name].Source != "installed" {
			t.Errorf("%s: got source %q, want %q", name, byName[name].Source, "installed")
		}
	}
}

func TestCollect_AlreadyInstalledSource(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Cmd.PackageInstalledMap = map[string]bool{"curl": true}

	gc := &config.GlobalConfig{}
	gc.AlreadyInstalled.Packages = []string{"curl"}

	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	items := c.Collect(gc)

	if len(items) != 1 || items[0].Source != "pre-existing" {
		t.Fatalf("got %+v, want a single pre-existing curl item", items)
	}
}

func TestCollect_DesktopAppAndFontStates(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Cmd.DesktopAppInstalledMap = map[string]bool{"docker": true}
	mockApp.Base.IsFontPresentResult = false
	mockApp.Base.IsFontPresentError = errors.New("fc-list: not found")

	gc := &config.GlobalConfig{}
	gc.Installed.DesktopApps = []string{"docker"}
	gc.Installed.Fonts = []string{"JetBrainsMono"}

	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	items := c.Collect(gc)

	var dockerState, fontState ItemState
	for _, it := range items {
		if it.Name == "docker" {
			dockerState = it.State
		}
		if it.Name == "JetBrainsMono" {
			fontState = it.State
		}
	}
	if dockerState != StateOK {
		t.Errorf("docker: got %v, want StateOK", dockerState)
	}
	if fontState != StateUnknown {
		t.Errorf("JetBrainsMono: got %v, want StateUnknown", fontState)
	}
}

func TestCollect_DevLanguageAndDatabaseStates(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("v20.0.0", "", nil) // every version check succeeds

	gc := &config.GlobalConfig{}
	gc.Installed.DevLanguages = []string{"node@lts"} // matches mise-managed Node config
	gc.Installed.Databases = []string{"redis"}

	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	items := c.Collect(gc)

	for _, it := range items {
		if it.Name == "node@lts" && it.State != StateOK {
			t.Errorf("node@lts: got %v, want StateOK", it.State)
		}
		if it.Name == "redis" && it.State != StateOK {
			t.Errorf("redis: got %v, want StateOK", it.State)
		}
	}
}

func TestCollect_ThemesAndTerminalToolsAlwaysEmptyIsHarmless(t *testing.T) {
	mockApp := testutil.NewMockApp()
	gc := &config.GlobalConfig{} // Themes and TerminalTools are never populated by real code paths

	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	items := c.Collect(gc)

	if len(items) != 0 {
		t.Errorf("expected zero items for an empty config, got %d", len(items))
	}
}

func TestCollect_EmptyCategoriesProduceNoItems(t *testing.T) {
	mockApp := testutil.NewMockApp()
	gc := &config.GlobalConfig{}

	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	items := c.Collect(gc)

	if items != nil {
		t.Errorf("expected nil/empty items for a fully empty config, got %+v", items)
	}
}

func TestCollect_DoesNotWriteGlobalConfig(t *testing.T) {
	// config.GlobalConfig.Load/Save resolve their file path from the package-level
	// paths.Paths.Config.Root var, not from XDG_CONFIG_HOME directly — that var is
	// set once when the paths package loads, so t.Setenv (testutil.IsolateXDGDirs)
	// does not affect it. testutil.SetupCompleteTest is the pattern that actually
	// redirects paths.Paths.Config.Root, and is required for any test that reads or
	// writes global_config.yaml through GlobalConfig.Load/Save/Create.
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatalf("gc.Load() failed: %v", err)
	}
	gc.Installed.Packages = []string{"git"}
	if err := gc.Save(); err != nil {
		t.Fatalf("gc.Save() failed: %v", err)
	}

	before, err := os.ReadFile(tc.ConfigPath)
	if err != nil {
		t.Fatalf("reading config before Collect: %v", err)
	}

	mockApp := testutil.NewMockApp()
	mockApp.Cmd.PackageInstalledMap = map[string]bool{"git": true}
	c := &Collector{Cmd: mockApp.Cmd, Base: mockApp.Base}
	c.Collect(gc)

	after, err := os.ReadFile(tc.ConfigPath)
	if err != nil {
		t.Fatalf("reading config after Collect: %v", err)
	}
	if string(before) != string(after) {
		t.Error("Collect must not modify global_config.yaml on disk")
	}
}
