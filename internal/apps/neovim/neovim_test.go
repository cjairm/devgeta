package neovim

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/downloader"
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
	a := &Neovim{}
	if a.Name() != constants.Neovim {
		t.Errorf("expected Name() %q, got %q", constants.Neovim, a.Name())
	}
	if a.Kind() != apps.KindTerminal {
		t.Errorf("expected Kind() KindTerminal, got %v", a.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = true // simulate macOS so test uses Homebrew path
	app := &Neovim{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mockApp.Cmd.InstalledPkg != constants.Neovim {
		t.Fatalf("expected InstallPackage(%s), got %q", constants.Neovim, mockApp.Cmd.InstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = true // simulate macOS so test uses Homebrew path
	app := &Neovim{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mockApp.Cmd.MaybeInstalled != constants.Neovim {
		t.Fatalf("expected MaybeInstallPackage(%s), got %q", constants.Neovim, mockApp.Cmd.MaybeInstalled)
	}

	// Confirm deps were installed before neovim itself
	if len(mockApp.Cmd.MaybeInstalledPkgs) < 2 {
		t.Fatalf("expected dep installs before neovim, got %v", mockApp.Cmd.MaybeInstalledPkgs)
	}
	last := mockApp.Cmd.MaybeInstalledPkgs[len(mockApp.Cmd.MaybeInstalledPkgs)-1]
	if last != constants.Neovim {
		t.Errorf("expected last MaybeInstall to be neovim, got %q", last)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestInstallDebian(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.IsMacResult = false // simulate Debian/Linux
	// All ExecCommand calls (tar + install + 2×cp) succeed
	mockApp.Base.SetExecCommandResult("", "", nil)

	app := &Neovim{
		Cmd:  mockApp.Cmd,
		Base: mockApp.Base,
		downloadFn: func(_ context.Context, url, _ string, _ downloader.RetryConfig) error {
			if !strings.Contains(url, constants.SupportedVersion.Neovim.Number) {
				t.Errorf("download URL does not contain version %s: %s",
					constants.SupportedVersion.Neovim.Number, url)
			}
			return nil
		},
	}

	if err := app.Install(); err != nil {
		t.Fatalf("Install (Debian) error: %v", err)
	}

	// Expect 4 ExecCommand calls: tar + sudo install + sudo cp lib + sudo cp share
	if mockApp.Base.GetExecCommandCallCount() != 4 {
		t.Fatalf("expected 4 ExecCommand calls, got %d", mockApp.Base.GetExecCommandCallCount())
	}
	calls := mockApp.Base.ExecCommandCalls
	if calls[0].Command != "tar" {
		t.Errorf("expected first command 'tar', got %q", calls[0].Command)
	}
	if calls[1].Command != "install" || !calls[1].IsSudo {
		t.Errorf("expected second command 'install' with IsSudo=true, got command=%q IsSudo=%v",
			calls[1].Command, calls[1].IsSudo)
	}
	if calls[2].Command != "cp" || !calls[2].IsSudo {
		t.Errorf("expected third command 'cp' with IsSudo=true, got command=%q IsSudo=%v",
			calls[2].Command, calls[2].IsSudo)
	}
	if calls[3].Command != "cp" || !calls[3].IsSudo {
		t.Errorf("expected fourth command 'cp' with IsSudo=true, got command=%q IsSudo=%v",
			calls[3].Command, calls[3].IsSudo)
	}
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	testutil.IsolateXDGDirs(t)

	nvimConfigDir := filepath.Join(tc.ConfigDir, "nvim")
	oldNvimDir := paths.Paths.Config.Nvim
	t.Cleanup(func() { paths.Paths.Config.Nvim = oldNvimDir })
	paths.Paths.Config.Nvim = nvimConfigDir

	tc.MockApp.Base.IsMacResult = true // macOS path: brew uninstall
	app := &Neovim{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}
	if tc.MockApp.Cmd.InstalledPkg != constants.Neovim {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestUninstall(t *testing.T) {
	t.Run("macOS", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		testutil.IsolateXDGDirs(t)

		nvimConfigDir := filepath.Join(tc.ConfigDir, "nvim")
		oldNvimDir := paths.Paths.Config.Nvim
		t.Cleanup(func() { paths.Paths.Config.Nvim = oldNvimDir })
		paths.Paths.Config.Nvim = nvimConfigDir

		tc.MockApp.Base.IsMacResult = true
		app := &Neovim{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall error: %v", err)
		}
		if tc.MockApp.Cmd.UninstalledPkg != constants.Neovim {
			t.Errorf("expected UninstallPackage(%s), got %q", constants.Neovim, tc.MockApp.Cmd.UninstalledPkg)
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})

	t.Run("linux", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		testutil.IsolateXDGDirs(t)

		nvimConfigDir := filepath.Join(tc.ConfigDir, "nvim")
		oldNvimDir := paths.Paths.Config.Nvim
		t.Cleanup(func() { paths.Paths.Config.Nvim = oldNvimDir })
		paths.Paths.Config.Nvim = nvimConfigDir

		tc.MockApp.Base.IsMacResult = false
		tc.MockApp.Base.SetExecCommandResult("", "", nil)
		app := &Neovim{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		if err := app.Uninstall(); err != nil {
			t.Fatalf("Uninstall error: %v", err)
		}

		// Linux path issues 3 rm commands via Base.ExecCommand
		if tc.MockApp.Base.GetExecCommandCallCount() != 3 {
			t.Fatalf("expected 3 ExecCommand calls, got %d", tc.MockApp.Base.GetExecCommandCallCount())
		}
		calls := tc.MockApp.Base.ExecCommandCalls
		for _, call := range calls {
			if call.Command != "rm" || !call.IsSudo {
				t.Errorf("expected sudo rm, got command=%q IsSudo=%v", call.Command, call.IsSudo)
			}
		}
		// Verify the three paths
		if calls[0].Args[1] != "/usr/local/bin/nvim" {
			t.Errorf("expected /usr/local/bin/nvim, got %v", calls[0].Args)
		}
		if calls[1].Args[1] != "/usr/local/lib/nvim" {
			t.Errorf("expected /usr/local/lib/nvim, got %v", calls[1].Args)
		}
		if calls[2].Args[1] != "/usr/local/share/nvim" {
			t.Errorf("expected /usr/local/share/nvim, got %v", calls[2].Args)
		}
	})
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Neovim{Cmd: mockApp.Cmd, Base: mockApp.Base}

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

	// Create neovim config source
	src := filepath.Join(tc.AppDir, "neovim")
	dst := filepath.Join(tc.ConfigDir, "nvim")

	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}

	// Override global paths
	oldNeovimSrc := paths.Paths.App.Configs.Neovim
	oldNeovimDst := paths.Paths.Config.Nvim
	t.Cleanup(func() {
		paths.Paths.App.Configs.Neovim = oldNeovimSrc
		paths.Paths.Config.Nvim = oldNeovimDst
	})
	paths.Paths.App.Configs.Neovim = src
	paths.Paths.Config.Nvim = dst

	originalContent := "-- Neovim init.lua\nvim.g.mapleader = ' '"
	if err := os.WriteFile(filepath.Join(src, "init.lua"), []byte(originalContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Mock successful version check with version output
	tc.MockApp.Base.SetExecCommandResult("NVIM v0.11.1\nBuild type: Release", "", nil)
	app := &Neovim{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	// Test ForceConfigure - should succeed with mocked version check
	err := app.ForceConfigure()

	if err != nil {
		t.Fatalf("ForceConfigure failed: %v", err)
	}

	// Verify that the configuration was copied
	check := filepath.Join(dst, "init.lua")
	if _, err := os.Stat(check); err != nil {
		t.Fatalf("expected copied file at %s: %v", check, err)
	}

	copiedContent, err := os.ReadFile(check)
	if err != nil {
		t.Fatalf("failed to read copied file: %v", err)
	}
	if string(copiedContent) != originalContent {
		t.Fatalf("content mismatch: expected %q, got %q", originalContent, string(copiedContent))
	}

	// Verify shell config was generated
	shellContent, err := os.ReadFile(tc.ZshConfigPath)
	if err != nil {
		t.Fatalf("Failed to read shell config: %v", err)
	}

	if !strings.Contains(string(shellContent), "# Neovim enabled") {
		t.Error("Expected shell config to contain Neovim feature")
	}

	// Verify version check was called once (this is expected)
	if tc.MockApp.Base.GetExecCommandCallCount() != 1 {
		t.Errorf("Expected 1 command call (version check), got %d", tc.MockApp.Base.GetExecCommandCallCount())
	}
}

func TestSoftConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	testutil.IsolateXDGDirs(t)

	// Create neovim config source
	src := filepath.Join(tc.AppDir, "neovim")
	dst := filepath.Join(tc.ConfigDir, "nvim")

	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}

	// Override global paths
	oldNeovimSrc := paths.Paths.App.Configs.Neovim
	oldNeovimDst := paths.Paths.Config.Nvim
	t.Cleanup(func() {
		paths.Paths.App.Configs.Neovim = oldNeovimSrc
		paths.Paths.Config.Nvim = oldNeovimDst
	})
	paths.Paths.App.Configs.Neovim = src
	paths.Paths.Config.Nvim = dst

	originalContent := "-- Neovim init.lua\nvim.g.mapleader = ' '"
	if err := os.WriteFile(filepath.Join(src, "init.lua"), []byte(originalContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Mock successful version check with version output
	tc.MockApp.Base.SetExecCommandResult("NVIM v0.11.1\nBuild type: Release", "", nil)
	app := &Neovim{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	// First call should attempt to configure
	err := app.SoftConfigure()

	if err != nil {
		t.Fatalf("SoftConfigure failed: %v", err)
	}

	// Verify that the configuration was copied
	check := filepath.Join(dst, "init.lua")
	if _, err := os.Stat(check); err != nil {
		t.Fatalf("expected copied file at %s: %v", check, err)
	}

	// Verify shell config was generated
	shellContent, err := os.ReadFile(tc.ZshConfigPath)
	if err != nil {
		t.Fatalf("Failed to read shell config: %v", err)
	}

	if !strings.Contains(string(shellContent), "# Neovim enabled") {
		t.Error("Expected shell config to contain Neovim feature on first call")
	}

	// Create the marker file to simulate existing config (or ensure it exists)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	markerContent := "-- Existing config"
	if err := os.WriteFile(filepath.Join(dst, "init.lua"), []byte(markerContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second call should skip configuration since init.lua exists
	// But it should still enable the shell feature if not already enabled
	err = app.SoftConfigure()
	if err != nil {
		t.Fatalf("SoftConfigure should not error on second call: %v", err)
	}

	// Verify the existing file wasn't changed
	finalContent, err := os.ReadFile(filepath.Join(dst, "init.lua"))
	if err != nil {
		t.Fatalf("failed to read init.lua: %v", err)
	}
	if string(finalContent) != markerContent {
		t.Fatalf(
			"SoftConfigure overwrote existing file: expected %q, got %q",
			markerContent,
			string(finalContent),
		)
	}

	// Verify version check was called once in first SoftConfigure (this is expected)
	if tc.MockApp.Base.GetExecCommandCallCount() != 1 {
		t.Errorf("Expected 1 command call (version check), got %d", tc.MockApp.Base.GetExecCommandCallCount())
	}
}
