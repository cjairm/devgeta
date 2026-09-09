package ghostty

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
	g := &Ghostty{}
	if g.Name() != constants.Ghostty {
		t.Errorf("expected Name() %q, got %q", constants.Ghostty, g.Name())
	}
	if g.Kind() != apps.KindTerminal {
		t.Errorf("expected Kind() KindTerminal, got %v", g.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Ghostty{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("mac uses desktop app cask", func(t *testing.T) {
		mockApp.Reset()
		mockApp.Base.IsMacResult = true

		if err := app.Install(); err != nil {
			t.Fatalf("Install error: %v", err)
		}
		if mockApp.Cmd.InstalledDesktopApp != constants.Ghostty {
			t.Fatalf(
				"expected InstallDesktopApp(%s), got %q",
				constants.Ghostty,
				mockApp.Cmd.InstalledDesktopApp,
			)
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})

	t.Run("linux uses apt package", func(t *testing.T) {
		mockApp.Reset()
		mockApp.Base.IsMacResult = false

		if err := app.Install(); err != nil {
			t.Fatalf("Install error: %v", err)
		}
		if mockApp.Cmd.InstalledPkg != constants.Ghostty {
			t.Fatalf(
				"expected InstallPackage(%s), got %q",
				constants.Ghostty,
				mockApp.Cmd.InstalledPkg,
			)
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Ghostty{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("mac uses desktop app cask", func(t *testing.T) {
		mockApp.Reset()
		mockApp.Base.IsMacResult = true

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("SoftInstall error: %v", err)
		}
		if mockApp.Cmd.MaybeInstalledDesktop != constants.Ghostty {
			t.Fatalf(
				"expected MaybeInstallDesktopApp(%s), got %q",
				constants.Ghostty,
				mockApp.Cmd.MaybeInstalledDesktop,
			)
		}
		if mockApp.Cmd.MaybeInstalledDesktopAlias != "" {
			t.Errorf("expected no alias, got %q", mockApp.Cmd.MaybeInstalledDesktopAlias)
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})

	t.Run("linux uses apt package", func(t *testing.T) {
		mockApp.Reset()
		mockApp.Base.IsMacResult = false

		if err := app.SoftInstall(); err != nil {
			t.Fatalf("SoftInstall error: %v", err)
		}
		if mockApp.Cmd.MaybeInstalled != constants.Ghostty {
			t.Fatalf(
				"expected MaybeInstallPackage(%s), got %q",
				constants.Ghostty,
				mockApp.Cmd.MaybeInstalled,
			)
		}
		if mockApp.Cmd.MaybeInstalledAlias != "" {
			t.Errorf("expected no alias, got %q", mockApp.Cmd.MaybeInstalledAlias)
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	oldGhosttyConfig := paths.Paths.Config.Ghostty
	t.Cleanup(func() { paths.Paths.Config.Ghostty = oldGhosttyConfig })
	paths.Paths.Config.Ghostty = filepath.Join(tc.ConfigDir, "ghostty")

	tc.MockApp.Base.IsMacResult = true
	app := &Ghostty{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}
	if tc.MockApp.Cmd.InstalledDesktopApp != constants.Ghostty {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledDesktopApp)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestUninstall(t *testing.T) {
	setupUninstallPaths := func(t *testing.T, tc *testutil.TestConfig) {
		t.Helper()
		oldGhosttyConfig := paths.Paths.Config.Ghostty
		t.Cleanup(func() { paths.Paths.Config.Ghostty = oldGhosttyConfig })
		paths.Paths.Config.Ghostty = filepath.Join(tc.ConfigDir, "ghostty")
	}

	t.Run("mac uninstalls the desktop app", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()
		testutil.IsolateXDGDirs(t)
		setupUninstallPaths(t, tc)

		tc.MockApp.Base.IsMacResult = true
		app := &Ghostty{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall error: %v", err)
		}
		if tc.MockApp.Cmd.UninstalledDesktopApp != constants.Ghostty {
			t.Errorf(
				"expected UninstallDesktopApp(%s), got %q",
				constants.Ghostty,
				tc.MockApp.Cmd.UninstalledDesktopApp,
			)
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})

	t.Run("linux uninstalls the package", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()
		testutil.IsolateXDGDirs(t)
		setupUninstallPaths(t, tc)

		tc.MockApp.Base.IsMacResult = false
		app := &Ghostty{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall error: %v", err)
		}
		if tc.MockApp.Cmd.UninstalledPkg != constants.Ghostty {
			t.Errorf(
				"expected UninstallPackage(%s), got %q",
				constants.Ghostty,
				tc.MockApp.Cmd.UninstalledPkg,
			)
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Ghostty{Cmd: mockApp.Cmd, Base: mockApp.Base}

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

	tmplDir := filepath.Join(tc.AppDir, "ghostty")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tmplContent := `term = xterm-256color

command = {{.ConfigPath}}/ghostty/starter.sh

window-padding-x = 10

{{if eq .Font "default"}}
font-size = 13
{{end}}

{{if eq .Theme "default"}}
background = #282828
{{end}}
`
	tmplPath := filepath.Join(tmplDir, "ghostty.conf.tmpl")
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// starter.sh lives under the shared terminal configs dir, not ghostty's
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

	destDir := filepath.Join(tc.ConfigDir, "ghostty")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldAppConfig := paths.Paths.App.Configs.Ghostty
	oldAppTerminal := paths.Paths.App.Configs.Terminal
	oldLocalConfig := paths.Paths.Config.Ghostty
	oldConfigRoot := paths.Paths.Config.Root

	paths.Paths.App.Configs.Ghostty = tmplDir
	paths.Paths.App.Configs.Terminal = terminalDir
	paths.Paths.Config.Ghostty = destDir
	paths.Paths.Config.Root = tc.ConfigDir

	t.Cleanup(func() {
		paths.Paths.App.Configs.Ghostty = oldAppConfig
		paths.Paths.App.Configs.Terminal = oldAppTerminal
		paths.Paths.Config.Ghostty = oldLocalConfig
		paths.Paths.Config.Root = oldConfigRoot
	})

	app := &Ghostty{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	configPath := filepath.Join(destDir, "config")
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
	if !strings.Contains(configStr, "term = xterm-256color") {
		t.Error("expected generated config to contain the term setting")
	}
	if !strings.Contains(configStr, "window-padding-x = 10") {
		t.Error("expected generated config to contain window-padding-x")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure_Ghostty(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	tmplDir := filepath.Join(tc.AppDir, "ghostty")
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		t.Fatal(err)
	}

	tmplContent := `term = xterm-256color

window-padding-x = 10
`
	tmplPath := filepath.Join(tmplDir, "ghostty.conf.tmpl")
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

	destDir := filepath.Join(tc.ConfigDir, "ghostty")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}

	oldAppConfig := paths.Paths.App.Configs.Ghostty
	oldAppTerminal := paths.Paths.App.Configs.Terminal
	oldLocalConfig := paths.Paths.Config.Ghostty
	oldConfigRoot := paths.Paths.Config.Root

	paths.Paths.App.Configs.Ghostty = tmplDir
	paths.Paths.App.Configs.Terminal = terminalDir
	paths.Paths.Config.Ghostty = destDir
	paths.Paths.Config.Root = tc.ConfigDir

	t.Cleanup(func() {
		paths.Paths.App.Configs.Ghostty = oldAppConfig
		paths.Paths.App.Configs.Terminal = oldAppTerminal
		paths.Paths.Config.Ghostty = oldLocalConfig
		paths.Paths.Config.Root = oldConfigRoot
	})

	app := &Ghostty{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure error: %v", err)
	}

	configPath := filepath.Join(destDir, "config")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected generated file at %s: %v", configPath, err)
	}

	modifiedContent := "term = xterm-256color\nwindow-padding-x = 99\n"
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
