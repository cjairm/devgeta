package mise

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
	a := &Mise{}
	if a.Name() != constants.Mise {
		t.Errorf("expected Name() %q, got %q", constants.Mise, a.Name())
	}
	if a.Kind() != apps.KindTerminal {
		t.Errorf("expected Kind() KindTerminal, got %v", a.Kind())
	}
}

func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Mise{Cmd: mockApp.Cmd}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mockApp.Cmd.InstalledPkg != constants.Mise {
		t.Fatalf("expected InstallPackage(%s), got %q", constants.Mise, mockApp.Cmd.InstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	app := &Mise{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall() should succeed: %v", err)
	}
	if tc.MockApp.Cmd.InstalledPkg != constants.Mise {
		t.Errorf("expected Install to be called, got %q", tc.MockApp.Cmd.InstalledPkg)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Mise{Cmd: mockApp.Cmd}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mockApp.Cmd.MaybeInstalled != constants.Mise {
		t.Fatalf("expected MaybeInstallPackage(%s), got %q", constants.Mise, mockApp.Cmd.MaybeInstalled)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Mise{Cmd: mockApp.Cmd, Base: mockApp.Base}

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
	// Create temp directories
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "config")
	templatesDir := filepath.Join(tempDir, "templates")

	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		t.Fatalf("Failed to create templates dir: %v", err)
	}

	// Create global config file
	globalConfigPath := filepath.Join(configDir, constants.App.Name, constants.App.File.GlobalConfig)
	if err := os.MkdirAll(filepath.Dir(globalConfigPath), 0755); err != nil {
		t.Fatalf("Failed to create global config dir: %v", err)
	}

	initialConfig := `app_path: ""
config_path: ""
already_installed:
  packages: []
  desktop_apps: []
  fonts: []
  themes: []
  terminal_tools: []
  dev_languages: []
  databases: []
current_font: ""
current_theme: ""
installed:
  packages: []
  desktop_apps: []
  fonts: []
  themes: []
  terminal_tools: []
  dev_languages: []
  databases: []
shortcuts: {}
shell:
  mise: false
  zoxide: false
  zsh_autosuggestions: false
  zsh_syntax_highlighting: false
  powerlevel10k: false
`
	if err := os.WriteFile(globalConfigPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("Failed to create global config: %v", err)
	}

	// Create simple template
	templatePath := filepath.Join(templatesDir, constants.App.Template.ShellConfig)
	templateContent := `# Test template
{{if .Mise}}
if command -v mise &> /dev/null; then
  eval "$(mise activate zsh)"
fi
{{end}}
`
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create template: %v", err)
	}

	// Override paths
	oldConfigDir := paths.Paths.Config.Root
	oldAppDir := paths.Paths.App.Root
	oldTemplatesAppDir := paths.Paths.App.Configs.Templates
	t.Cleanup(func() {
		paths.Paths.Config.Root = oldConfigDir
		paths.Paths.App.Root = oldAppDir
		paths.Paths.App.Configs.Templates = oldTemplatesAppDir
	})

	paths.Paths.Config.Root = configDir
	paths.Paths.App.Root = tempDir
	paths.Paths.App.Configs.Templates = templatesDir

	mockApp := testutil.NewMockApp()
	app := &Mise{Cmd: mockApp.Cmd}

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	// Verify generated file contains mise activation
	outputPath := filepath.Join(tempDir, "devgeta.zsh")
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}

	expectedContent := `eval "$(mise activate zsh)"`
	if !strings.Contains(string(content), expectedContent) {
		t.Fatalf(
			"Expected file to contain %q, but it didn't. Content: %s",
			expectedContent,
			string(content),
		)
	}

	// Verify global config was updated
	updatedConfig, err := os.ReadFile(globalConfigPath)
	if err != nil {
		t.Fatalf("Failed to read updated config: %v", err)
	}
	if !strings.Contains(string(updatedConfig), "mise: true") {
		t.Fatalf(
			"Expected config to have mise enabled. Config: %s",
			string(updatedConfig),
		)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestSoftConfigure(t *testing.T) {
	t.Run("ConfigureWhenNotEnabled", func(t *testing.T) {
		// Create temp directories
		tempDir := t.TempDir()
		configDir := filepath.Join(tempDir, "config")
		templatesDir := filepath.Join(tempDir, "templates")

		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatalf("Failed to create config dir: %v", err)
		}
		if err := os.MkdirAll(templatesDir, 0755); err != nil {
			t.Fatalf("Failed to create templates dir: %v", err)
		}

		// Create global config with feature disabled
		globalConfigPath := filepath.Join(configDir, constants.App.Name, constants.App.File.GlobalConfig)
		if err := os.MkdirAll(filepath.Dir(globalConfigPath), 0755); err != nil {
			t.Fatalf("Failed to create global config dir: %v", err)
		}

		initialConfig := `shell:
  mise: false
  zoxide: false
  zsh_autosuggestions: false
  zsh_syntax_highlighting: false
  powerlevel10k: false
`
		if err := os.WriteFile(globalConfigPath, []byte(initialConfig), 0644); err != nil {
			t.Fatalf("Failed to create global config: %v", err)
		}

		// Create template
		templatePath := filepath.Join(templatesDir, constants.App.Template.ShellConfig)
		templateContent := `{{if .Mise}}mise-enabled{{end}}`
		if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
			t.Fatalf("Failed to create template: %v", err)
		}

		// Override paths
		oldConfigDir := paths.Paths.Config.Root
		oldAppDir := paths.Paths.App.Root
		oldTemplatesAppDir := paths.Paths.App.Configs.Templates
		t.Cleanup(func() {
			paths.Paths.Config.Root = oldConfigDir
			paths.Paths.App.Root = oldAppDir
			paths.Paths.App.Configs.Templates = oldTemplatesAppDir
		})

		paths.Paths.Config.Root = configDir
		paths.Paths.App.Root = tempDir
		paths.Paths.App.Configs.Templates = templatesDir

		mockApp := testutil.NewMockApp()
		app := &Mise{Cmd: mockApp.Cmd}

		if err := app.SoftConfigure(); err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}

		// Verify file was generated
		outputPath := filepath.Join(tempDir, "devgeta.zsh")
		content, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read generated file: %v", err)
		}

		if !strings.Contains(string(content), "mise-enabled") {
			t.Fatal("Expected template to be rendered with feature enabled")
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})

	t.Run("SkipWhenAlreadyEnabled", func(t *testing.T) {
		// Create temp directories
		tempDir := t.TempDir()
		configDir := filepath.Join(tempDir, "config")

		if err := os.MkdirAll(configDir, 0755); err != nil {
			t.Fatalf("Failed to create config dir: %v", err)
		}

		// Create global config with feature ALREADY enabled
		globalConfigPath := filepath.Join(configDir, constants.App.Name, constants.App.File.GlobalConfig)
		if err := os.MkdirAll(filepath.Dir(globalConfigPath), 0755); err != nil {
			t.Fatalf("Failed to create global config dir: %v", err)
		}

		configWithFeatureEnabled := `shell:
  mise: true
  zoxide: false
  zsh_autosuggestions: false
  zsh_syntax_highlighting: false
  powerlevel10k: false
`
		if err := os.WriteFile(globalConfigPath, []byte(configWithFeatureEnabled), 0644); err != nil {
			t.Fatalf("Failed to create global config: %v", err)
		}

		// Override paths
		oldConfigDir := paths.Paths.Config.Root
		oldAppDir := paths.Paths.App.Root
		t.Cleanup(func() {
			paths.Paths.Config.Root = oldConfigDir
			paths.Paths.App.Root = oldAppDir
		})

		paths.Paths.Config.Root = configDir
		paths.Paths.App.Root = tempDir

		mockApp := testutil.NewMockApp()
		app := &Mise{Cmd: mockApp.Cmd}

		if err := app.SoftConfigure(); err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}

		// Verify NO file was generated (should skip when already enabled)
		outputPath := filepath.Join(tempDir, "devgeta.zsh")
		if _, err := os.Stat(outputPath); err == nil {
			t.Fatal("Expected no file to be generated when feature already enabled")
		}

		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})
}

func TestUninstall(t *testing.T) {
	// Create temp directories
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "config")
	templatesDir := filepath.Join(tempDir, "templates")

	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}
	if err := os.MkdirAll(templatesDir, 0755); err != nil {
		t.Fatalf("Failed to create templates dir: %v", err)
	}

	// Create global config with feature enabled
	globalConfigPath := filepath.Join(configDir, constants.App.Name, constants.App.File.GlobalConfig)
	if err := os.MkdirAll(filepath.Dir(globalConfigPath), 0755); err != nil {
		t.Fatalf("Failed to create global config dir: %v", err)
	}

	initialConfig := `shell:
  mise: true
  zoxide: false
  zsh_autosuggestions: false
  zsh_syntax_highlighting: false
  powerlevel10k: false
`
	if err := os.WriteFile(globalConfigPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("Failed to create global config: %v", err)
	}

	// Create template
	templatePath := filepath.Join(templatesDir, constants.App.Template.ShellConfig)
	templateContent := `{{if .Mise}}enabled{{else}}disabled{{end}}`
	if err := os.WriteFile(templatePath, []byte(templateContent), 0644); err != nil {
		t.Fatalf("Failed to create template: %v", err)
	}

	// Override paths
	oldConfigDir := paths.Paths.Config.Root
	oldAppDir := paths.Paths.App.Root
	oldTemplatesAppDir := paths.Paths.App.Configs.Templates
	t.Cleanup(func() {
		paths.Paths.Config.Root = oldConfigDir
		paths.Paths.App.Root = oldAppDir
		paths.Paths.App.Configs.Templates = oldTemplatesAppDir
	})

	paths.Paths.Config.Root = configDir
	paths.Paths.App.Root = tempDir
	paths.Paths.App.Configs.Templates = templatesDir

	mockApp := testutil.NewMockApp()
	app := &Mise{Cmd: mockApp.Cmd}

	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}

	if mockApp.Cmd.UninstalledPkg != constants.Mise {
		t.Errorf("expected UninstallPackage(%s), got %q", constants.Mise, mockApp.Cmd.UninstalledPkg)
	}

	// Verify feature was disabled in config
	updatedConfig, err := os.ReadFile(globalConfigPath)
	if err != nil {
		t.Fatalf("Failed to read updated config: %v", err)
	}
	if !strings.Contains(string(updatedConfig), "mise: false") {
		t.Fatalf(
			"Expected config to have mise disabled. Config: %s",
			string(updatedConfig),
		)
	}

	// Verify generated file reflects disabled state
	outputPath := filepath.Join(tempDir, "devgeta.zsh")
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}
	if !strings.Contains(string(content), "disabled") {
		t.Fatalf("Expected template to show disabled state. Content: %s", string(content))
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestExecuteCommand(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Mise{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("successful execution", func(t *testing.T) {
		mockApp.Base.SetExecCommandResult("mise 2024.1.0", "", nil)

		err := app.ExecuteCommand("--version")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		// Verify command was called
		if mockApp.Base.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 call, got %d", mockApp.Base.GetExecCommandCallCount())
		}

		// Verify parameters
		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall.Command != constants.Mise {
			t.Fatalf("Expected command '%s', got %q", constants.Mise, lastCall.Command)
		}
		if len(lastCall.Args) != 1 || lastCall.Args[0] != "--version" {
			t.Fatalf("Expected args ['--version'], got %v", lastCall.Args)
		}
	})

	t.Run("multiple arguments", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		mockApp.Base.SetExecCommandResult("", "", nil)

		err := app.ExecuteCommand("install", "node@20")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if len(lastCall.Args) != 2 {
			t.Fatalf("Expected 2 args, got %d", len(lastCall.Args))
		}
		if lastCall.Args[0] != "install" || lastCall.Args[1] != "node@20" {
			t.Fatalf("Expected args ['install', 'node@20'], got %v", lastCall.Args)
		}
	})
}

func TestUseGlobal(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Mise{Cmd: mockApp.Cmd, Base: mockApp.Base}

	t.Run("successful use global", func(t *testing.T) {
		mockApp.Base.SetExecCommandResult("", "", nil)

		err := app.UseGlobal("node", "20")
		if err != nil {
			t.Fatalf("UseGlobal failed: %v", err)
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		expectedArgs := []string{"use", "--global", "node@20"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}
		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})

	t.Run("missing language parameter", func(t *testing.T) {
		mockApp.Base.ResetExecCommand()
		err := app.UseGlobal("", "20")
		if err == nil {
			t.Fatal("Expected error for missing language")
		}
		if !strings.Contains(err.Error(), "language") {
			t.Fatalf("Expected error about language, got: %v", err)
		}
	})

	t.Run("missing version parameter", func(t *testing.T) {
		err := app.UseGlobal("node", "")
		if err == nil {
			t.Fatal("Expected error for missing version")
		}
		if !strings.Contains(err.Error(), "version") {
			t.Fatalf("Expected error about version, got: %v", err)
		}
	})
}
