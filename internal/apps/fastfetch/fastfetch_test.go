package fastfetch

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
	// Initialize logger for tests
	testutil.InitLogger()
}

func TestNew(t *testing.T) {
	app := New()

	if app == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNameAndKind(t *testing.T) {
	a := &Fastfetch{}
	if a.Name() != constants.Fastfetch {
		t.Errorf("expected Name() %q, got %q", constants.Fastfetch, a.Name())
	}
	if a.Kind() != apps.KindTerminal {
		t.Errorf("expected Kind() KindTerminal, got %v", a.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Fastfetch{Cmd: mockApp.Cmd}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mockApp.Cmd.InstalledPkg != "fastfetch" {
		t.Fatalf(
			"expected InstallPackage(%s), got %q",
			"fastfetch",
			mockApp.Cmd.InstalledPkg,
		)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	oldConfigDir := paths.Paths.Config.Fastfetch
	t.Cleanup(func() { paths.Paths.Config.Fastfetch = oldConfigDir })
	paths.Paths.Config.Fastfetch = filepath.Join(tc.ConfigDir, "fastfetch")

	app := &Fastfetch{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}
	if tc.MockApp.Cmd.InstalledPkg != constants.Fastfetch {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Fastfetch{Cmd: mockApp.Cmd}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mockApp.Cmd.MaybeInstalled != "fastfetch" {
		t.Fatalf(
			"expected MaybeInstallPackage(%s), got %q",
			"fastfetch",
			mockApp.Cmd.MaybeInstalled,
		)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUninstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	oldConfigDir := paths.Paths.Config.Fastfetch
	t.Cleanup(func() { paths.Paths.Config.Fastfetch = oldConfigDir })
	paths.Paths.Config.Fastfetch = filepath.Join(tc.ConfigDir, "fastfetch")

	app := &Fastfetch{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}
	if tc.MockApp.Cmd.UninstalledPkg != constants.Fastfetch {
		t.Errorf("expected UninstallPackage(%s), got %q", constants.Fastfetch, tc.MockApp.Cmd.UninstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Fastfetch{Cmd: mockApp.Cmd, Base: mockApp.Base}

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

	tempSourceDir := filepath.Join(tc.AppDir, "fastfetch-src")
	tempTargetDir := filepath.Join(tc.ConfigDir, "fastfetch")
	if err := os.MkdirAll(tempSourceDir, 0755); err != nil {
		t.Fatal(err)
	}

	oldAppDir := paths.Paths.App.Configs.Fastfetch
	oldConfigDir := paths.Paths.Config.Fastfetch
	t.Cleanup(func() {
		paths.Paths.App.Configs.Fastfetch = oldAppDir
		paths.Paths.Config.Fastfetch = oldConfigDir
	})
	paths.Paths.App.Configs.Fastfetch = tempSourceDir
	paths.Paths.Config.Fastfetch = tempTargetDir

	testConfigContent := `{"display": {"separator": " ~ "}}`
	if err := os.WriteFile(filepath.Join(tempSourceDir, "config.jsonc"), []byte(testConfigContent), 0644); err != nil {
		t.Fatal(err)
	}

	app := &Fastfetch{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	targetConfigFile := filepath.Join(tempTargetDir, "config.jsonc")
	if _, err := os.Stat(targetConfigFile); os.IsNotExist(err) {
		t.Fatalf("expected config file to be copied to target directory")
	}

	copiedContent, err := os.ReadFile(targetConfigFile)
	if err != nil {
		t.Fatalf("failed to read copied config file: %v", err)
	}
	if string(copiedContent) != testConfigContent {
		t.Fatalf("content mismatch: expected %q, got %q", testConfigContent, string(copiedContent))
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	t.Run("ConfigureWhenNotExists", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()
		testutil.IsolateXDGDirs(t)

		tempSourceDir := filepath.Join(tc.AppDir, "fastfetch-src")
		tempTargetDir := filepath.Join(tc.ConfigDir, "fastfetch")
		if err := os.MkdirAll(tempSourceDir, 0755); err != nil {
			t.Fatal(err)
		}

		oldAppDir := paths.Paths.App.Configs.Fastfetch
		oldConfigDir := paths.Paths.Config.Fastfetch
		t.Cleanup(func() {
			paths.Paths.App.Configs.Fastfetch = oldAppDir
			paths.Paths.Config.Fastfetch = oldConfigDir
		})
		paths.Paths.App.Configs.Fastfetch = tempSourceDir
		paths.Paths.Config.Fastfetch = tempTargetDir

		testConfigContent := `{"display": {"separator": " ~ "}}`
		if err := os.WriteFile(filepath.Join(tempSourceDir, "config.jsonc"), []byte(testConfigContent), 0644); err != nil {
			t.Fatal(err)
		}

		app := &Fastfetch{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.SoftConfigure(); err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}

		targetConfigFile := filepath.Join(tempTargetDir, "config.jsonc")
		if _, err := os.Stat(targetConfigFile); os.IsNotExist(err) {
			t.Fatalf("expected config file to be copied to target directory")
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})

	t.Run("SkipWhenExists", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()
		testutil.IsolateXDGDirs(t)

		tempSourceDir := filepath.Join(tc.AppDir, "fastfetch-src")
		tempTargetDir := filepath.Join(tc.ConfigDir, "fastfetch")
		if err := os.MkdirAll(tempSourceDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(tempTargetDir, 0755); err != nil {
			t.Fatal(err)
		}

		oldAppDir := paths.Paths.App.Configs.Fastfetch
		oldConfigDir := paths.Paths.Config.Fastfetch
		t.Cleanup(func() {
			paths.Paths.App.Configs.Fastfetch = oldAppDir
			paths.Paths.Config.Fastfetch = oldConfigDir
		})
		paths.Paths.App.Configs.Fastfetch = tempSourceDir
		paths.Paths.Config.Fastfetch = tempTargetDir

		existingContent := `{"existing": "config"}`
		targetConfigFile := filepath.Join(tempTargetDir, "config.jsonc")
		if err := os.WriteFile(targetConfigFile, []byte(existingContent), 0644); err != nil {
			t.Fatal(err)
		}

		sourceContent := `{"new": "config"}`
		if err := os.WriteFile(filepath.Join(tempSourceDir, "config.jsonc"), []byte(sourceContent), 0644); err != nil {
			t.Fatal(err)
		}

		app := &Fastfetch{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.SoftConfigure(); err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}

		content, err := os.ReadFile(targetConfigFile)
		if err != nil {
			t.Fatalf("failed to read target config file: %v", err)
		}
		if string(content) != existingContent {
			t.Fatalf("expected file content unchanged, got %q", string(content))
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
