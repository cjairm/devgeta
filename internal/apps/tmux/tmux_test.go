package tmux_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
)

func init() {
	// Initialize logger for tests
	testutil.InitLogger()
}

func TestNew(t *testing.T) {
	t.Helper()

	app := tmux.New()
	if app == nil {
		t.Error("Expected New() to return a non-nil Tmux instance")
	}
}

func TestNameAndKind(t *testing.T) {
	app := &tmux.Tmux{}
	if app.Name() != constants.Tmux {
		t.Errorf("expected Name() %q, got %q", constants.Tmux, app.Name())
	}
	if app.Kind() != apps.KindTerminal {
		t.Errorf("expected Kind() KindTerminal, got %v", app.Kind())
	}
}

func TestInstall(t *testing.T) {
	t.Helper()

	tests := []struct {
		name        string
		shouldError bool
		installErr  error
	}{
		{
			name:        "successful installation",
			shouldError: false,
			installErr:  nil,
		},
		{
			name:        "installation failure",
			shouldError: true,
			installErr:  errors.New("installation failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			mockApp := testutil.NewMockApp()
			mockApp.Cmd.InstallError = tt.installErr

			app := &tmux.Tmux{
				Cmd: mockApp.Cmd,
			}

			err := app.Install()

			if tt.shouldError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// Verify the correct package was passed
			if mockApp.Cmd.InstalledPkg != "tmux" {
				t.Errorf(
					"Expected package 'tmux', got '%s'",
					mockApp.Cmd.InstalledPkg,
				)
			}

			testutil.VerifyNoRealCommands(t, mockApp.Base)
		})
	}
}

func TestForceInstall(t *testing.T) {
	t.Helper()

	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	testutil.IsolateXDGDirs(t)

	oldHome := paths.Paths.Home.Root
	t.Cleanup(func() { paths.Paths.Home.Root = oldHome })
	paths.Paths.Home.Root = tc.ConfigDir

	app := &tmux.Tmux{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() error: %v", err)
	}
	if tc.MockApp.Cmd.InstalledPkg != constants.Tmux {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	t.Helper()

	tests := []struct {
		name        string
		shouldError bool
		installErr  error
	}{
		{
			name:        "successful soft installation",
			shouldError: false,
			installErr:  nil,
		},
		{
			name:        "soft installation failure",
			shouldError: true,
			installErr:  errors.New("soft installation failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			mockApp := testutil.NewMockApp()
			mockApp.Cmd.MaybeInstallError = tt.installErr

			app := &tmux.Tmux{
				Cmd: mockApp.Cmd,
			}

			err := app.SoftInstall()

			if tt.shouldError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// Verify the correct package was passed
			if mockApp.Cmd.MaybeInstalled != "tmux" {
				t.Errorf(
					"Expected package 'tmux', got '%s'",
					mockApp.Cmd.MaybeInstalled,
				)
			}

			testutil.VerifyNoRealCommands(t, mockApp.Base)
		})
	}
}

func TestUninstall(t *testing.T) {
	t.Helper()

	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	testutil.IsolateXDGDirs(t)

	oldHome := paths.Paths.Home.Root
	t.Cleanup(func() { paths.Paths.Home.Root = oldHome })
	paths.Paths.Home.Root = tc.ConfigDir

	app := &tmux.Tmux{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}
	if tc.MockApp.Cmd.UninstalledPkg != constants.Tmux {
		t.Errorf(
			"expected UninstallPackage(%s), got %q",
			constants.Tmux,
			tc.MockApp.Cmd.UninstalledPkg,
		)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestUpdate(t *testing.T) {
	t.Helper()

	mockApp := testutil.NewMockApp()
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

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
	t.Helper()

	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	// Create source directory with tmux config
	sourceDir := filepath.Join(tc.AppDir, "tmux")
	err := os.MkdirAll(sourceDir, 0o755)
	if err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	// Create destination directory
	destDir := tc.ConfigDir
	err = os.MkdirAll(destDir, 0o755)
	if err != nil {
		t.Fatalf("Failed to create destination directory: %v", err)
	}

	testutil.IsolateXDGDirs(t)
	t.Setenv("TMUX", "") // ensure source-file reload is not triggered in this test

	oldTmux := paths.Paths.App.Configs.Tmux
	t.Cleanup(func() { paths.Paths.App.Configs.Tmux = oldTmux })
	paths.Paths.App.Configs.Tmux = sourceDir

	oldHome := paths.Paths.Home.Root
	t.Cleanup(func() { paths.Paths.Home.Root = oldHome })
	paths.Paths.Home.Root = destDir

	// Create source tmux.conf file (without leading dot in source)
	sourceConfig := filepath.Join(sourceDir, "tmux.conf")
	configContent := "# Test tmux configuration\nset -g default-terminal \"screen-256color\""
	err = os.WriteFile(sourceConfig, []byte(configContent), 0o644)
	if err != nil {
		t.Fatalf("Failed to create source config: %v", err)
	}

	app := &tmux.Tmux{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	err = app.ForceConfigure()
	if err != nil {
		t.Errorf("ForceConfigure returned error: %v", err)
	}

	// Verify config was copied to destination
	destConfig := filepath.Join(destDir, ".tmux.conf")
	content, err := os.ReadFile(destConfig)
	if err != nil {
		t.Fatalf("Failed to read destination config: %v", err)
	}

	if string(content) != configContent {
		t.Errorf(
			"Expected config content %q, got %q",
			configContent,
			string(content),
		)
	}

	// Verify shell config was generated
	shellContent, err := os.ReadFile(tc.ZshConfigPath)
	if err != nil {
		t.Fatalf("Failed to read shell config: %v", err)
	}

	if !strings.Contains(string(shellContent), "# Tmux enabled") {
		t.Error("Expected shell config to contain Tmux feature")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestForceConfigureReloadsInsideTmux(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	sourceDir := filepath.Join(tc.AppDir, "tmux")
	if err := os.MkdirAll(sourceDir, 0o750); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	testutil.IsolateXDGDirs(t)
	t.Setenv("TMUX", "test-session") // simulate being inside a tmux session

	oldTmux := paths.Paths.App.Configs.Tmux
	t.Cleanup(func() { paths.Paths.App.Configs.Tmux = oldTmux })
	paths.Paths.App.Configs.Tmux = sourceDir

	oldHome := paths.Paths.Home.Root
	t.Cleanup(func() { paths.Paths.Home.Root = oldHome })
	paths.Paths.Home.Root = tc.ConfigDir

	sourceConfig := filepath.Join(sourceDir, "tmux.conf")
	if err := os.WriteFile(sourceConfig, []byte("# test"), 0o600); err != nil {
		t.Fatalf("Failed to create source config: %v", err)
	}

	app := &tmux.Tmux{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure returned error: %v", err)
	}

	// Verify source-file was called via the mock (not real tmux)
	last := tc.MockApp.Base.GetLastExecCommandCall()
	if last == nil {
		t.Fatal("expected a mock ExecCommand call for source-file reload")
	}
	if last.Command != constants.Tmux {
		t.Errorf("expected tmux command, got %q", last.Command)
	}
	if len(last.Args) < 1 || last.Args[0] != "source-file" {
		t.Errorf("expected source-file arg, got %v", last.Args)
	}
}

func TestSoftConfigure(t *testing.T) {
	t.Helper()

	// Test case 1: Configuration doesn't exist - should configure
	t.Run("ConfigureWhenNotExists", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		// Create source directory with tmux config
		sourceDir := filepath.Join(tc.AppDir, "tmux")
		err := os.MkdirAll(sourceDir, 0o755)
		if err != nil {
			t.Fatalf("Failed to create source directory: %v", err)
		}

		destDir := tc.ConfigDir

		testutil.IsolateXDGDirs(t)
		t.Setenv("TMUX", "") // ensure source-file reload is not triggered in this test

		oldTmux := paths.Paths.App.Configs.Tmux
		t.Cleanup(func() { paths.Paths.App.Configs.Tmux = oldTmux })
		paths.Paths.App.Configs.Tmux = sourceDir

		oldHome := paths.Paths.Home.Root
		t.Cleanup(func() { paths.Paths.Home.Root = oldHome })
		paths.Paths.Home.Root = destDir

		// Create source tmux.conf file (without leading dot in source)
		sourceConfig := filepath.Join(sourceDir, "tmux.conf")
		configContent := "# Test tmux configuration\nset -g default-terminal \"screen-256color\""
		err = os.WriteFile(sourceConfig, []byte(configContent), 0o644)
		if err != nil {
			t.Fatalf("Failed to create source config: %v", err)
		}

		// Mock the HOME environment variable since SoftConfigure uses os.UserHomeDir()
		t.Setenv("HOME", destDir)

		app := &tmux.Tmux{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		err = app.SoftConfigure()
		if err != nil {
			t.Errorf("SoftConfigure returned error: %v", err)
		}

		// Verify config was copied to destination
		destConfig := filepath.Join(destDir, ".tmux.conf")
		content, err := os.ReadFile(destConfig)
		if err != nil {
			t.Fatalf("Failed to read destination config: %v", err)
		}

		if string(content) != configContent {
			t.Errorf(
				"Expected config content %q, got %q",
				configContent,
				string(content),
			)
		}

		// Verify shell config was generated
		shellContent, err := os.ReadFile(tc.ZshConfigPath)
		if err != nil {
			t.Fatalf("Failed to read shell config: %v", err)
		}

		if !strings.Contains(string(shellContent), "# Tmux enabled") {
			t.Error("Expected shell config to contain Tmux feature on first call")
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})

	// Test case 2: Configuration already exists - should skip file copy but enable shell feature
	t.Run("SkipWhenExists", func(t *testing.T) {
		tc := testutil.SetupCompleteTest(t)
		defer tc.Cleanup()

		// Create home directory with existing .tmux.conf
		homeDir := tc.ConfigDir

		// Set Home path before creating the config file
		testutil.IsolateXDGDirs(t)

		oldHome := paths.Paths.Home.Root
		t.Cleanup(func() { paths.Paths.Home.Root = oldHome })
		paths.Paths.Home.Root = homeDir

		existingConfig := filepath.Join(homeDir, ".tmux.conf")
		existingContent := "# Existing tmux configuration\nset -g mouse on"
		err := os.WriteFile(existingConfig, []byte(existingContent), 0o644)
		if err != nil {
			t.Fatalf("Failed to create existing config: %v", err)
		}

		// Create source directory (though it shouldn't be used for file copy)
		sourceDir := filepath.Join(tc.AppDir, "tmux")
		err = os.MkdirAll(sourceDir, 0o755)
		if err != nil {
			t.Fatalf("Failed to create source directory: %v", err)
		}
		oldTmux2 := paths.Paths.App.Configs.Tmux
		t.Cleanup(func() { paths.Paths.App.Configs.Tmux = oldTmux2 })
		paths.Paths.App.Configs.Tmux = sourceDir

		// Create a different source config to prove it's not copied (without leading dot in source)
		sourceConfig := filepath.Join(sourceDir, "tmux.conf")
		sourceContent := "# Different config that should not be copied"
		err = os.WriteFile(sourceConfig, []byte(sourceContent), 0o644)
		if err != nil {
			t.Fatalf("Failed to create source config: %v", err)
		}

		// Mock the UserHomeDir for this test by temporarily setting HOME env var
		t.Setenv("HOME", homeDir)

		app := &tmux.Tmux{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

		err = app.SoftConfigure()
		if err != nil {
			t.Errorf("SoftConfigure returned error: %v", err)
		}

		// Verify content wasn't changed
		contentAfter, err := os.ReadFile(existingConfig)
		if err != nil {
			t.Fatalf("Failed to read config after test: %v", err)
		}

		if string(contentAfter) != existingContent {
			t.Errorf("Expected config to remain unchanged, but it was modified")
		}

		if string(contentAfter) == sourceContent {
			t.Errorf(
				"Config was overwritten with source content when it should have been preserved",
			)
		}

		// Verify shell config was still generated (feature should be enabled even when file exists)
		shellContent, err := os.ReadFile(tc.ZshConfigPath)
		if err != nil {
			t.Fatalf("Failed to read shell config: %v", err)
		}

		if !strings.Contains(string(shellContent), "# Tmux enabled") {
			t.Error("Expected shell config to contain Tmux feature even when config file exists")
		}

		testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	})
}

func TestExecuteCommand(t *testing.T) {
	t.Helper()

	tests := []struct {
		name        string
		args        []string
		shouldError bool
		execErr     error
	}{
		{
			name:        "successful execution",
			args:        []string{"--version"},
			shouldError: false,
			execErr:     nil,
		},
		{
			name:        "execution failure",
			args:        []string{"invalid-command"},
			shouldError: true,
			execErr:     errors.New("command failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			mockApp := testutil.NewMockApp()

			if tt.execErr != nil {
				mockApp.Base.SetExecCommandResult("", "error", tt.execErr)
			} else {
				mockApp.Base.SetExecCommandResult("tmux 3.3a", "", nil)
			}

			app := &tmux.Tmux{
				Cmd:  mockApp.Cmd,
				Base: mockApp.Base,
			}

			err := app.ExecuteCommand(tt.args...)

			if tt.shouldError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// Verify the command was called
			if mockApp.Base.GetExecCommandCallCount() != 1 {
				t.Errorf(
					"Expected 1 ExecCommand call, got %d",
					mockApp.Base.GetExecCommandCallCount(),
				)
			}

			lastCall := mockApp.Base.GetLastExecCommandCall()
			if lastCall == nil {
				t.Fatal("No ExecCommand call recorded")
			}
			if lastCall.Command != "tmux" {
				t.Errorf("Expected command 'tmux', got %q", lastCall.Command)
			}
		})
	}
}

func TestCreateSession(t *testing.T) {
	t.Helper()

	tests := []struct {
		name        string
		sessionName string
		workdir     string
		shouldError bool
		execErr     error
	}{
		{
			name:        "successful session creation",
			sessionName: "my-session",
			workdir:     "/path/to/project",
			shouldError: false,
			execErr:     nil,
		},
		{
			name:        "session creation failure",
			sessionName: "duplicate-session",
			workdir:     "/path/to/project",
			shouldError: true,
			execErr:     errors.New("duplicate session: duplicate-session"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			mockApp := testutil.NewMockApp()

			if tt.execErr != nil {
				mockApp.Base.SetExecCommandResult("", "error", tt.execErr)
			} else {
				mockApp.Base.SetExecCommandResult("", "", nil)
			}

			app := &tmux.Tmux{
				Cmd:  mockApp.Cmd,
				Base: mockApp.Base,
			}

			err := app.CreateSession(tt.sessionName, tt.workdir)

			if tt.shouldError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// Verify correct arguments were passed
			lastCall := mockApp.Base.GetLastExecCommandCall()
			if lastCall == nil {
				t.Fatal("No ExecCommand call recorded")
			}

			expectedArgs := []string{"new-session", "-d", "-s", tt.sessionName, "-c", tt.workdir}
			if len(lastCall.Args) != len(expectedArgs) {
				t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
			}
			for i, arg := range expectedArgs {
				if lastCall.Args[i] != arg {
					t.Errorf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
				}
			}
		})
	}
}

func TestKillSession(t *testing.T) {
	t.Helper()

	tests := []struct {
		name        string
		sessionName string
		shouldError bool
		execErr     error
	}{
		{
			name:        "successful session kill",
			sessionName: "my-session",
			shouldError: false,
			execErr:     nil,
		},
		{
			name:        "session not found",
			sessionName: "nonexistent-session",
			shouldError: true,
			execErr:     errors.New("session not found: nonexistent-session"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			mockApp := testutil.NewMockApp()

			if tt.execErr != nil {
				mockApp.Base.SetExecCommandResult("", "error", tt.execErr)
			} else {
				mockApp.Base.SetExecCommandResult("", "", nil)
			}

			app := &tmux.Tmux{
				Cmd:  mockApp.Cmd,
				Base: mockApp.Base,
			}

			err := app.KillSession(tt.sessionName)

			if tt.shouldError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// Verify correct arguments were passed
			lastCall := mockApp.Base.GetLastExecCommandCall()
			if lastCall == nil {
				t.Fatal("No ExecCommand call recorded")
			}

			expectedArgs := []string{"kill-session", "-t", tt.sessionName}
			if len(lastCall.Args) != len(expectedArgs) {
				t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
			}
			for i, arg := range expectedArgs {
				if lastCall.Args[i] != arg {
					t.Errorf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
				}
			}
		})
	}
}

func TestHasSession(t *testing.T) {
	t.Helper()

	tests := []struct {
		name        string
		sessionName string
		exists      bool
		execErr     error
	}{
		{
			name:        "session exists",
			sessionName: "my-session",
			exists:      true,
			execErr:     nil,
		},
		{
			name:        "session does not exist",
			sessionName: "nonexistent-session",
			exists:      false,
			execErr:     errors.New("session not found"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			mockApp := testutil.NewMockApp()

			if tt.execErr != nil {
				mockApp.Base.SetExecCommandResult("", "error", tt.execErr)
			} else {
				mockApp.Base.SetExecCommandResult("", "", nil)
			}

			app := &tmux.Tmux{
				Cmd:  mockApp.Cmd,
				Base: mockApp.Base,
			}

			result := app.HasSession(tt.sessionName)

			if result != tt.exists {
				t.Errorf("Expected HasSession to return %v, got %v", tt.exists, result)
			}

			// Verify correct arguments were passed
			lastCall := mockApp.Base.GetLastExecCommandCall()
			if lastCall == nil {
				t.Fatal("No ExecCommand call recorded")
			}

			expectedArgs := []string{"has-session", "-t", tt.sessionName}
			if len(lastCall.Args) != len(expectedArgs) {
				t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
			}
			for i, arg := range expectedArgs {
				if lastCall.Args[i] != arg {
					t.Errorf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
				}
			}
		})
	}
}

func TestSendKeys(t *testing.T) {
	t.Helper()

	tests := []struct {
		name        string
		sessionName string
		keys        string
		shouldError bool
		execErr     error
	}{
		{
			name:        "successful send keys",
			sessionName: "my-session",
			keys:        "opencode",
			shouldError: false,
			execErr:     nil,
		},
		{
			name:        "send keys failure",
			sessionName: "nonexistent-session",
			keys:        "opencode",
			shouldError: true,
			execErr:     errors.New("session not found"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			mockApp := testutil.NewMockApp()

			if tt.execErr != nil {
				mockApp.Base.SetExecCommandResult("", "error", tt.execErr)
			} else {
				mockApp.Base.SetExecCommandResult("", "", nil)
			}

			app := &tmux.Tmux{
				Cmd:  mockApp.Cmd,
				Base: mockApp.Base,
			}

			err := app.SendKeys(tt.sessionName, tt.keys)

			if tt.shouldError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// Verify correct arguments were passed
			lastCall := mockApp.Base.GetLastExecCommandCall()
			if lastCall == nil {
				t.Fatal("No ExecCommand call recorded")
			}

			expectedArgs := []string{"send-keys", "-t", tt.sessionName, tt.keys, "Enter"}
			if len(lastCall.Args) != len(expectedArgs) {
				t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
			}
			for i, arg := range expectedArgs {
				if lastCall.Args[i] != arg {
					t.Errorf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
				}
			}
		})
	}
}

func TestSelectWindow(t *testing.T) {
	t.Helper()

	tests := []struct {
		name        string
		windowName  string
		shouldError bool
		execErr     error
	}{
		{
			name:        "successful window selection",
			windowName:  "wt-feature",
			shouldError: false,
			execErr:     nil,
		},
		{
			name:        "window not found",
			windowName:  "wt-nonexistent",
			shouldError: true,
			execErr:     errors.New("can't find window: wt-nonexistent"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Helper()

			mockApp := testutil.NewMockApp()

			if tt.execErr != nil {
				mockApp.Base.SetExecCommandResult("", "error", tt.execErr)
			} else {
				mockApp.Base.SetExecCommandResult("", "", nil)
			}

			app := &tmux.Tmux{
				Cmd:  mockApp.Cmd,
				Base: mockApp.Base,
			}

			err := app.SelectWindow(tt.windowName)

			if tt.shouldError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// Verify correct arguments were passed
			lastCall := mockApp.Base.GetLastExecCommandCall()
			if lastCall == nil {
				t.Fatal("No ExecCommand call recorded")
			}

			expectedArgs := []string{"select-window", "-t", tt.windowName}
			if len(lastCall.Args) != len(expectedArgs) {
				t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
			}
			for i, arg := range expectedArgs {
				if lastCall.Args[i] != arg {
					t.Errorf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
				}
			}
		})
	}
}

func TestWindowSession(t *testing.T) {
	t.Run("window found returns session", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult(
			"my-session\twt-feature-a\nmy-session\twt-feature-b\n",
			"",
			nil,
		)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		session, ok := app.WindowSession("wt-feature-a")
		if !ok {
			t.Fatal("expected window to be found")
		}
		if session != "my-session" {
			t.Errorf("expected session 'my-session', got %q", session)
		}
	})

	t.Run("window not found returns empty and false", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("my-session\twt-other\n", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		_, ok := app.WindowSession("wt-missing")
		if ok {
			t.Error("expected window not to be found")
		}
	})

	t.Run("exec error returns false", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "error", errors.New("no server"))
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		_, ok := app.WindowSession("wt-any")
		if ok {
			t.Error("expected false on exec error")
		}
	})
}

func TestListSessions(t *testing.T) {
	t.Run(
		"parses tab-separated session lines, attached is a client count not a bool",
		func(t *testing.T) {
			mockApp := testutil.NewMockApp()
			mockApp.Base.SetExecCommandResult("main\t1\nnotes\t0\nshared\t2\n", "", nil)
			app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

			sessions, err := app.ListSessions()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			expected := []tmux.SessionInfo{
				{Name: "main", Attached: true},
				{Name: "notes", Attached: false},
				{Name: "shared", Attached: true},
			}
			if len(sessions) != len(expected) {
				t.Fatalf("expected %d sessions, got %d: %v", len(expected), len(sessions), sessions)
			}
			for i, exp := range expected {
				if sessions[i] != exp {
					t.Errorf("session[%d] = %+v, want %+v", i, sessions[i], exp)
				}
			}
		},
	)

	t.Run("stale socket: stderr has no server running returns nil, nil", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult(
			"",
			"no server running on /tmp/tmux-1000/default",
			errors.New("exit status 1"),
		)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		sessions, err := app.ListSessions()
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		if sessions != nil {
			t.Errorf("expected nil sessions, got %v", sessions)
		}
	})

	t.Run(
		"fresh machine: error connecting to plus no such file or directory returns nil, nil",
		func(t *testing.T) {
			mockApp := testutil.NewMockApp()
			mockApp.Base.SetExecCommandResult(
				"",
				"error connecting to /tmp/tmux-1000/default (No such file or directory)",
				errors.New("exit status 1"),
			)
			app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

			sessions, err := app.ListSessions()
			if err != nil {
				t.Fatalf("expected nil error, got: %v", err)
			}
			if sessions != nil {
				t.Errorf("expected nil sessions, got %v", sessions)
			}
		},
	)

	t.Run(
		"regression: error connecting to without ENOENT is a real error, not swallowed",
		func(t *testing.T) {
			mockApp := testutil.NewMockApp()
			mockApp.Base.SetExecCommandResult(
				"",
				"error connecting to /tmp/tmux-1000/default (Permission denied)",
				errors.New("exit status 1"),
			)
			app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

			sessions, err := app.ListSessions()
			if err == nil {
				t.Fatal("expected error to be returned, not swallowed")
			}
			if sessions != nil {
				t.Errorf("expected nil sessions on error, got %v", sessions)
			}
		},
	)

	t.Run("generic unrelated error is wrapped and returned", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult(
			"",
			"some unexpected tmux failure",
			errors.New("exit status 1"),
		)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		sessions, err := app.ListSessions()
		if err == nil {
			t.Fatal("expected error to be returned")
		}
		if !strings.Contains(err.Error(), "failed to list tmux sessions") {
			t.Errorf("expected wrapped error message, got: %v", err)
		}
		if sessions != nil {
			t.Errorf("expected nil sessions on error, got %v", sessions)
		}
	})
}

func TestFindWindowsBySuffix(t *testing.T) {
	t.Run("returns all windows matching suffix across repos", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult(
			"wt-repo-a-CXE-35\nwt-repo-b-CXE-35\nwt-repo-a-other\n",
			"",
			nil,
		)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		matches := app.FindWindowsBySuffix("-CXE-35")
		if len(matches) != 2 {
			t.Fatalf("expected 2 matches, got %d: %v", len(matches), matches)
		}
		if matches[0] != "wt-repo-a-CXE-35" || matches[1] != "wt-repo-b-CXE-35" {
			t.Errorf("unexpected matches: %v", matches)
		}
	})

	t.Run("no match returns nil", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("wt-repo-a-other\n", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if matches := app.FindWindowsBySuffix("-CXE-35"); matches != nil {
			t.Errorf("expected nil, got %v", matches)
		}
	})

	t.Run("exec error returns nil", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "error", errors.New("no server"))
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if matches := app.FindWindowsBySuffix("-CXE-35"); matches != nil {
			t.Errorf("expected nil on exec error, got %v", matches)
		}
	})
}

func TestSessionWindows(t *testing.T) {
	t.Run("parses tab-separated session/window pairs across the server", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult(
			"my-session\twt-feature-a\nmy-session\teditor\nother\tnotes\n",
			"",
			nil,
		)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		pairs := app.SessionWindows()

		expected := []tmux.SessionWindow{
			{Session: "my-session", Window: "wt-feature-a"},
			{Session: "my-session", Window: "editor"},
			{Session: "other", Window: "notes"},
		}
		if len(pairs) != len(expected) {
			t.Fatalf("expected %d pairs, got %d: %+v", len(expected), len(pairs), pairs)
		}
		for i, exp := range expected {
			if pairs[i] != exp {
				t.Errorf("pair[%d] = %+v, want %+v", i, pairs[i], exp)
			}
		}
	})

	t.Run("exec error returns nil", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "error", errors.New("no server"))
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if pairs := app.SessionWindows(); pairs != nil {
			t.Errorf("expected nil on exec error, got %+v", pairs)
		}
	})

	t.Run("no windows returns nil", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if pairs := app.SessionWindows(); pairs != nil {
			t.Errorf("expected nil when no windows exist, got %+v", pairs)
		}
	})
}

func TestSwitchToWindow(t *testing.T) {
	t.Run("calls switch-client then select-window", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		err := app.SwitchToWindow("my-session", "wt-feature")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		calls := mockApp.Base.GetExecCommandCallCount()
		if calls != 2 {
			t.Fatalf("expected 2 calls (switch-client + select-window), got %d", calls)
		}
	})

	t.Run("returns error when switch-client fails", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "error", errors.New("no client"))
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		err := app.SwitchToWindow("bad-session", "wt-feature")
		if err == nil {
			t.Error("expected error when switch-client fails")
		}
	})
}

func TestKillWindow(t *testing.T) {
	t.Run("resolves session then kills window", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		// First call: list-windows for WindowSession lookup
		// Second call: kill-window
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("my-session\twt-feature\n", "", nil),
			commands.ExecCommandResult("", "", nil),
		)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		err := app.KillWindow("wt-feature")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		last := mockApp.Base.GetLastExecCommandCall()
		if last == nil || last.Args[0] != "kill-window" {
			t.Errorf("expected kill-window call, got %v", last)
		}
		// Target should be session:name
		found := false
		for _, arg := range last.Args {
			if arg == "my-session:wt-feature" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected target 'my-session:wt-feature' in args %v", last.Args)
		}
	})

	t.Run("falls back to bare name when session lookup fails", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		// WindowSession fails, then kill-window with bare name
		mockApp.Base.SetExecCommandResults(
			commands.ExecCommandResult("", "error", errors.New("no server")),
			commands.ExecCommandResult("", "", nil),
		)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		err := app.KillWindow("wt-feature")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		last := mockApp.Base.GetLastExecCommandCall()
		if last == nil || last.Args[0] != "kill-window" {
			t.Errorf("expected kill-window call, got %v", last)
		}
	})
}

func TestKillPane(t *testing.T) {
	t.Run("kills the pane by id, no window/session qualification", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.KillPane("%12"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		last := mockApp.Base.GetLastExecCommandCall()
		if last == nil || last.Args[0] != "kill-pane" {
			t.Fatalf("expected 'kill-pane', got %v", last)
		}
		found := false
		for _, arg := range last.Args {
			if arg == "%12" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected target '%%12' in args %v", last.Args)
		}
	})

	t.Run("returns error when kill-pane fails", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "error", errors.New("no such pane"))
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.KillPane("%12"); err == nil {
			t.Error("expected error when kill-pane fails")
		}
	})
}

func TestCreateWindow(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("", "", nil)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.CreateWindow("wt-feature", "/tmp/repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := mockApp.Base.GetLastExecCommandCall()
	if last == nil {
		t.Fatal("no ExecCommand call recorded")
	}
	if last.Args[0] != "new-window" {
		t.Errorf("expected 'new-window', got %q", last.Args[0])
	}
}

func TestSplitWindow(t *testing.T) {
	t.Run("vertical direction splits with -h", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.SplitWindow("wt-feature", "/tmp/repo", "vertical"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		last := mockApp.Base.GetLastExecCommandCall()
		if last == nil {
			t.Fatal("no ExecCommand call recorded")
		}
		expectedArgs := []string{"split-window", "-h", "-t", "wt-feature", "-c", "/tmp/repo"}
		if len(last.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d: %v", len(expectedArgs), len(last.Args), last.Args)
		}
		for i, arg := range expectedArgs {
			if last.Args[i] != arg {
				t.Errorf("Expected arg[%d] to be %q, got %q", i, arg, last.Args[i])
			}
		}
	})

	t.Run("horizontal direction splits with -v", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.SplitWindow("wt-feature", "/tmp/repo", "horizontal"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		last := mockApp.Base.GetLastExecCommandCall()
		if last == nil {
			t.Fatal("no ExecCommand call recorded")
		}
		expectedArgs := []string{"split-window", "-v", "-t", "wt-feature", "-c", "/tmp/repo"}
		if len(last.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d: %v", len(expectedArgs), len(last.Args), last.Args)
		}
		for i, arg := range expectedArgs {
			if last.Args[i] != arg {
				t.Errorf("Expected arg[%d] to be %q, got %q", i, arg, last.Args[i])
			}
		}
	})

	t.Run("unknown direction returns error without executing a command", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		err := app.SplitWindow("wt-feature", "/tmp/repo", "diagonal")
		if err == nil {
			t.Fatal("expected error for unknown direction")
		}
		if mockApp.Base.GetExecCommandCallCount() != 0 {
			t.Errorf(
				"expected no ExecCommand calls, got %d",
				mockApp.Base.GetExecCommandCallCount(),
			)
		}
	})
}

func TestCreateWindowInSession(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("", "", nil)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.CreateWindowInSession("my-session", "wt-feature", "/tmp/repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := mockApp.Base.GetLastExecCommandCall()
	if last == nil || last.Args[0] != "new-window" {
		t.Errorf("expected 'new-window', got %v", last)
	}
	// Target should include session
	found := false
	for _, arg := range last.Args {
		if arg == "my-session:" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected session target 'my-session:' in args %v", last.Args)
	}
}

func TestCreateSessionWithWindow(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("", "", nil)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.CreateSessionWithWindow("my-session", "wt-feature", "/tmp/repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := mockApp.Base.GetLastExecCommandCall()
	if last == nil || last.Args[0] != "new-session" {
		t.Errorf("expected 'new-session', got %v", last)
	}
}

func TestHasWindow(t *testing.T) {
	t.Run("returns false outside tmux without calling exec", func(t *testing.T) {
		t.Setenv("TMUX", "")
		mockApp := testutil.NewMockApp()
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if app.HasWindow("wt-feature") {
			t.Error("expected false outside tmux")
		}
		if mockApp.Base.GetExecCommandCallCount() != 0 {
			t.Error("expected no exec calls outside tmux")
		}
	})

	t.Run("returns true when window found inside tmux", func(t *testing.T) {
		t.Setenv("TMUX", "test-session")
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("wt-feature\nwt-other\n", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if !app.HasWindow("wt-feature") {
			t.Error("expected window to be found")
		}
	})

	t.Run("returns false when window not in list", func(t *testing.T) {
		t.Setenv("TMUX", "test-session")
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("wt-other\n", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if app.HasWindow("wt-missing") {
			t.Error("expected window not to be found")
		}
	})
}

func TestSendKeysToWindow(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("", "", nil)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.SendKeysToWindow("wt-feature", "opencode"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := mockApp.Base.GetLastExecCommandCall()
	if last == nil || last.Args[0] != "send-keys" {
		t.Errorf("expected 'send-keys', got %v", last)
	}
}

func TestSendKeysToWindowInSession(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("", "", nil)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.SendKeysToWindowInSession("my-session", "wt-feature", "claude"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := mockApp.Base.GetLastExecCommandCall()
	if last == nil || last.Args[0] != "send-keys" {
		t.Errorf("expected 'send-keys', got %v", last)
	}
	// Verify target includes session:window
	found := false
	for _, arg := range last.Args {
		if arg == "my-session:wt-feature" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected target 'my-session:wt-feature' in args %v", last.Args)
	}
}

func TestSendKeysToPane(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("", "", nil)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.SendKeysToPane("%12", "claude"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := mockApp.Base.GetLastExecCommandCall()
	if last == nil || last.Args[0] != "send-keys" {
		t.Fatalf("expected 'send-keys', got %v", last)
	}
	// Verify target is the bare pane id, not a window/session-qualified form.
	found := false
	for _, arg := range last.Args {
		if arg == "%12" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected target '%%12' in args %v", last.Args)
	}
}

func TestCurrentSession(t *testing.T) {
	t.Run("returns session name inside tmux", func(t *testing.T) {
		t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("misc\n", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		name, ok := app.CurrentSession()
		if !ok || name != "misc" {
			t.Errorf("expected (misc, true), got (%q, %v)", name, ok)
		}
		last := mockApp.Base.GetLastExecCommandCall()
		if last == nil || last.Args[0] != "display-message" {
			t.Errorf("expected 'display-message', got %v", last)
		}
	})

	t.Run("returns false outside tmux without calling tmux", func(t *testing.T) {
		t.Setenv("TMUX", "")
		mockApp := testutil.NewMockApp()
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if _, ok := app.CurrentSession(); ok {
			t.Error("expected false when not inside tmux")
		}
		if mockApp.Base.GetExecCommandCallCount() != 0 {
			t.Error("should not execute tmux commands outside tmux")
		}
	})

	t.Run("returns false on exec error", func(t *testing.T) {
		t.Setenv("TMUX", "/tmp/tmux-1000/default,123,0")
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "no server", errors.New("no server"))
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if _, ok := app.CurrentSession(); ok {
			t.Error("expected false on exec error")
		}
	})
}

func TestSwitchToSession(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("", "", nil)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.SwitchToSession("misc"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := mockApp.Base.GetLastExecCommandCall()
	if last == nil || last.Args[0] != "switch-client" {
		t.Fatalf("expected 'switch-client', got %v", last)
	}
	found := false
	for _, arg := range last.Args {
		if arg == "misc" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected target 'misc' in args %v", last.Args)
	}
}

func TestActivePaneID(t *testing.T) {
	t.Run("returns trimmed pane id", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("%67\n", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		id, err := app.ActivePaneID("wt-feature")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "%67" {
			t.Errorf("expected %%67, got %q", id)
		}
		last := mockApp.Base.GetLastExecCommandCall()
		if last == nil || last.Args[0] != "display-message" {
			t.Fatalf("expected 'display-message', got %v", last)
		}
		found := false
		for _, arg := range last.Args {
			if arg == "wt-feature" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected target 'wt-feature' in args %v", last.Args)
		}
	})

	t.Run("accepts a qualified session:window target", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("%12\n", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		id, err := app.ActivePaneID("my-session:wt-feature")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id != "%12" {
			t.Errorf("expected %%12, got %q", id)
		}
	})

	t.Run("exec error is returned", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "no server", errors.New("no server"))
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if _, err := app.ActivePaneID("wt-feature"); err == nil {
			t.Error("expected error on exec failure")
		}
	})

	t.Run("empty stdout is an error", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if _, err := app.ActivePaneID("wt-feature"); err == nil {
			t.Error("expected error when tmux returns no pane id")
		}
	})
}

func TestSelectPane(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("", "", nil)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.SelectPane("%67"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	last := mockApp.Base.GetLastExecCommandCall()
	if last == nil || last.Args[0] != "select-pane" {
		t.Fatalf("expected 'select-pane', got %v", last)
	}
	found := false
	for _, arg := range last.Args {
		if arg == "%67" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected target '%%67' in args %v", last.Args)
	}
}

func TestPaneStates(t *testing.T) {
	t.Run(
		"parses tab-separated 4-field pane lines across multiple sessions/windows/panes, including empty last field",
		func(t *testing.T) {
			mockApp := testutil.NewMockApp()
			mockApp.Base.SetExecCommandResult(
				"my-session\twt-feature-a\t%1\tworking\nmy-session\teditor\t%2\t\nother\tnotes\t%3\tblocked\n",
				"",
				nil,
			)
			app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

			states := app.PaneStates()

			expected := []tmux.PaneState{
				{Session: "my-session", Window: "wt-feature-a", PaneID: "%1", State: "working"},
				{Session: "my-session", Window: "editor", PaneID: "%2", State: ""},
				{Session: "other", Window: "notes", PaneID: "%3", State: "blocked"},
			}
			if len(states) != len(expected) {
				t.Fatalf("expected %d states, got %d: %+v", len(expected), len(states), states)
			}
			for i, exp := range expected {
				if states[i] != exp {
					t.Errorf("state[%d] = %+v, want %+v", i, states[i], exp)
				}
			}
		},
	)

	t.Run("exec error returns nil", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "error", errors.New("no server"))
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if states := app.PaneStates(); states != nil {
			t.Errorf("expected nil on exec error, got %+v", states)
		}
	})

	t.Run("empty stdout returns nil", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if states := app.PaneStates(); states != nil {
			t.Errorf("expected nil when no panes exist, got %+v", states)
		}
	})
}

func TestPanesInWindow(t *testing.T) {
	t.Run(
		"returns only panes belonging to the matching window, with pane_current_command",
		func(t *testing.T) {
			mockApp := testutil.NewMockApp()
			mockApp.Base.SetExecCommandResult(
				"wt-feature-a\t%1\tzsh\nwt-feature-a\t%2\tnvim\nother-window\t%3\tbash\n",
				"",
				nil,
			)
			app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

			panes := app.PanesInWindow("wt-feature-a")

			expected := []tmux.WindowPane{
				{PaneID: "%1", CurrentCommand: "zsh"},
				{PaneID: "%2", CurrentCommand: "nvim"},
			}
			if len(panes) != len(expected) {
				t.Fatalf("expected %d panes, got %d: %+v", len(expected), len(panes), panes)
			}
			for i, exp := range expected {
				if panes[i] != exp {
					t.Errorf("pane[%d] = %+v, want %+v", i, panes[i], exp)
				}
			}
		},
	)

	t.Run("no matching window returns nil", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("other-window\t%3\tbash\n", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if panes := app.PanesInWindow("wt-feature-a"); panes != nil {
			t.Errorf("expected nil when no pane matches, got %+v", panes)
		}
	})

	t.Run("exec error returns nil", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "error", errors.New("no server"))
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if panes := app.PanesInWindow("wt-feature-a"); panes != nil {
			t.Errorf("expected nil on exec error, got %+v", panes)
		}
	})
}

func TestClearAgentStateForWindow(t *testing.T) {
	t.Run("clears every pane belonging to the matching window only", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult(
			"my-session\twt-feature-a\t%1\tworking\nmy-session\twt-feature-a\t%2\tidle\nother\tnotes\t%3\tblocked\n",
			"",
			nil,
		)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.ClearAgentStateForWindow("wt-feature-a"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// call 0 is the list-panes scan; the two matching panes (%1, %2) should
		// each get exactly one set-option -p -u call, and %3 (a different
		// window) should never appear in a set-option call at all. Call 3 is the
		// window-level clear of @dg_window_agent_state.
		calls := mockApp.Base.ExecCommandCalls
		if len(calls) != 4 {
			t.Fatalf(
				"expected 4 calls (1 scan + 2 pane clears + 1 window clear), got %d: %+v",
				len(calls),
				calls,
			)
		}
		expectedTargets := []string{"%1", "%2"}
		for i, target := range expectedTargets {
			call := calls[i+1]
			expectedArgs := []string{"set-option", "-p", "-u", "-t", target, "@dg_agent_state"}
			if len(call.Args) != len(expectedArgs) {
				t.Fatalf("call[%d] args = %v, want %v", i+1, call.Args, expectedArgs)
			}
			for j, arg := range expectedArgs {
				if call.Args[j] != arg {
					t.Errorf("call[%d] arg[%d] = %q, want %q", i+1, j, call.Args[j], arg)
				}
			}
		}
		for _, call := range calls {
			if call.Args[0] == "set-option" {
				for _, arg := range call.Args {
					if arg == "%3" {
						t.Errorf(
							"set-option call must not target %%3 (different window): %v",
							call.Args,
						)
					}
				}
			}
		}
		// Verify the window-level clear call (call 3) targets %2 (last matched pane)
		// with -w -u flags for the window-level option @dg_window_agent_state.
		windowClearCall := calls[3]
		expectedWindowClearArgs := []string{
			"set-option",
			"-w",
			"-u",
			"-t",
			"%2",
			"@dg_window_agent_state",
		}
		if len(windowClearCall.Args) != len(expectedWindowClearArgs) {
			t.Fatalf(
				"window clear call args = %v, want %v",
				windowClearCall.Args,
				expectedWindowClearArgs,
			)
		}
		for j, arg := range expectedWindowClearArgs {
			if windowClearCall.Args[j] != arg {
				t.Errorf("window clear call arg[%d] = %q, want %q", j, windowClearCall.Args[j], arg)
			}
		}
	})

	t.Run("window with no matching panes issues zero set-option calls", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult(
			"my-session\teditor\t%1\t\nother\tnotes\t%2\tblocked\n",
			"",
			nil,
		)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.ClearAgentStateForWindow("wt-does-not-exist"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if calls := mockApp.Base.GetExecCommandCallCount(); calls != 1 {
			t.Fatalf("expected 1 call (only the list-panes scan), got %d", calls)
		}
	})

	t.Run("a failure on one matching pane does not stop the rest", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResults(
			// scan
			commands.ExecCommandResult(
				"s\twt-feature-a\t%1\tworking\ns\twt-feature-a\t%2\tworking\ns\twt-feature-a\t%3\tworking\n",
				"",
				nil,
			),
			// clear %1: fails
			commands.ExecCommandResult("", "error", errors.New("pane closed")),
			// clear %2: succeeds
			commands.ExecCommandResult("", "", nil),
			// clear %3: succeeds
			commands.ExecCommandResult("", "", nil),
			// clear window-level: succeeds (best-effort, independent of pane failures)
			commands.ExecCommandResult("", "", nil),
		)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		err := app.ClearAgentStateForWindow("wt-feature-a")
		if err == nil {
			t.Fatal("expected the first pane's error to be returned")
		}

		calls := mockApp.Base.ExecCommandCalls
		if len(calls) != 5 {
			t.Fatalf(
				"expected 5 calls (1 scan + 3 pane clears + 1 window clear), got %d: %+v",
				len(calls),
				calls,
			)
		}
		expectedTargets := []string{"%1", "%2", "%3"}
		for i, target := range expectedTargets {
			call := calls[i+1]
			found := false
			for _, arg := range call.Args {
				if arg == target {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("call[%d] args %v does not target %q", i+1, call.Args, target)
			}
		}
		// Verify window-level clear is still attempted (call 4), even though
		// a pane-level clear failed. It should target %3 (last matched pane).
		windowClearCall := calls[4]
		expectedWindowClearArgs := []string{
			"set-option",
			"-w",
			"-u",
			"-t",
			"%3",
			"@dg_window_agent_state",
		}
		if len(windowClearCall.Args) != len(expectedWindowClearArgs) {
			t.Fatalf(
				"window clear call args = %v, want %v",
				windowClearCall.Args,
				expectedWindowClearArgs,
			)
		}
		for j, arg := range expectedWindowClearArgs {
			if windowClearCall.Args[j] != arg {
				t.Errorf("window clear call arg[%d] = %q, want %q", j, windowClearCall.Args[j], arg)
			}
		}
	})
}
