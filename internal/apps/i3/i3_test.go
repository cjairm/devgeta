package i3

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
		t.Errorf("expected UninstallPackage(%s), got %q", constants.I3, tc.MockApp.Cmd.UninstalledPkg)
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
	if err := os.MkdirAll(i3ConfigAppDir, 0755); err != nil {
		t.Fatalf("Failed to create app i3 dir: %v", err)
	}

	sourceConfig := filepath.Join(i3ConfigAppDir, "config")
	sourceContent := "# i3 config\nset $mod Mod4\n"
	if err := os.WriteFile(sourceConfig, []byte(sourceContent), 0644); err != nil {
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

	mockApp := testutil.NewMockApp()
	i := &I3{Cmd: mockApp.Cmd}

	err := i.ForceConfigure()
	if err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}

	// Verify config was copied
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

func TestSoftConfigure_PreservesExisting(t *testing.T) {
	cleanup := testutil.SetupIsolatedPaths(t)
	defer cleanup()
	testutil.IsolateXDGDirs(t)

	appDir, configDir, _, _ := testutil.SetupTestDirs(t)

	// Create source config (won't be used)
	i3ConfigAppDir := filepath.Join(appDir, "i3")
	if err := os.MkdirAll(i3ConfigAppDir, 0755); err != nil {
		t.Fatalf("Failed to create app i3 dir: %v", err)
	}

	// Create existing config in target location
	i3ConfigLocalDir := filepath.Join(configDir, "i3")
	if err := os.MkdirAll(i3ConfigLocalDir, 0755); err != nil {
		t.Fatalf("Failed to create local i3 dir: %v", err)
	}

	existingConfig := filepath.Join(i3ConfigLocalDir, "config")
	existingContent := "# Existing custom config\n"
	if err := os.WriteFile(existingConfig, []byte(existingContent), 0644); err != nil {
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
	if err := os.MkdirAll(i3ConfigAppDir, 0755); err != nil {
		t.Fatalf("Failed to create app i3 dir: %v", err)
	}

	sourceConfig := filepath.Join(i3ConfigAppDir, "config")
	sourceContent := "# i3 config from template\n"
	if err := os.WriteFile(sourceConfig, []byte(sourceContent), 0644); err != nil {
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
