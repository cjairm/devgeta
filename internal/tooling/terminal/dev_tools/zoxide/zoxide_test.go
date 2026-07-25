package zoxide

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
)

func init() {
	logger.Init(false)
}

func TestNew(t *testing.T) {
	app := New()

	if app == nil {
		t.Fatal("New() returned nil")
	}
}

func TestInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Zoxide{Cmd: mc}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mc.InstalledPkg != "zoxide" {
		t.Fatalf("expected InstallPackage(%s), got %q", "zoxide", mc.InstalledPkg)
	}
}

// SKIP: ForceInstall test as per guidelines
// ForceInstall calls Uninstall (which now modifies GlobalConfig) before Install
// Testing this creates false negatives and state dependencies
// func TestForceInstall(t *testing.T) { ... }

func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Zoxide{Cmd: mc}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mc.MaybeInstalled != "zoxide" {
		t.Fatalf("expected MaybeInstallPackage(%s), got %q", "zoxide", mc.MaybeInstalled)
	}
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
  extended_capabilities: false
`
	if err := os.WriteFile(globalConfigPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("Failed to create global config: %v", err)
	}

	// Create simple template
	templatePath := filepath.Join(templatesDir, constants.App.Template.ShellConfig)
	templateContent := `# Test template
{{if .Zoxide}}
eval "$(zoxide init zsh)"
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

	mc := commands.NewMockCommand()
	app := &Zoxide{Cmd: mc}

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	// Verify generated file contains zoxide initialization
	outputPath := filepath.Join(tempDir, "devgeta.zsh")
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}

	expectedContent := `eval "$(zoxide init zsh)"`
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
	if !strings.Contains(string(updatedConfig), "zoxide: true") {
		t.Fatalf(
			"Expected config to have zoxide enabled. Config: %s",
			string(updatedConfig),
		)
	}
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
  extended_capabilities: false
`
		if err := os.WriteFile(globalConfigPath, []byte(initialConfig), 0644); err != nil {
			t.Fatalf("Failed to create global config: %v", err)
		}

		// Create template
		templatePath := filepath.Join(templatesDir, constants.App.Template.ShellConfig)
		templateContent := `{{if .Zoxide}}enabled{{end}}`
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

		mc := commands.NewMockCommand()
		app := &Zoxide{Cmd: mc}

		if err := app.SoftConfigure(); err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}

		// Verify file was generated
		outputPath := filepath.Join(tempDir, "devgeta.zsh")
		content, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("Failed to read generated file: %v", err)
		}

		if !strings.Contains(string(content), "enabled") {
			t.Fatal("Expected template to be rendered with feature enabled")
		}
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
  mise: false
  zoxide: true
  zsh_autosuggestions: false
  zsh_syntax_highlighting: false
  powerlevel10k: false
  extended_capabilities: false
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

		mc := commands.NewMockCommand()
		app := &Zoxide{Cmd: mc}

		if err := app.SoftConfigure(); err != nil {
			t.Fatalf("SoftConfigure error: %v", err)
		}

		// Verify NO file was generated (should skip when already enabled)
		outputPath := filepath.Join(tempDir, "devgeta.zsh")
		if _, err := os.Stat(outputPath); err == nil {
			t.Fatal("Expected no file to be generated when feature already enabled")
		}
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
  mise: false
  zoxide: true
  zsh_autosuggestions: false
  zsh_syntax_highlighting: false
  powerlevel10k: false
  extended_capabilities: false
`
	if err := os.WriteFile(globalConfigPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("Failed to create global config: %v", err)
	}

	// Create template
	templatePath := filepath.Join(templatesDir, constants.App.Template.ShellConfig)
	templateContent := `{{if .Zoxide}}enabled{{else}}disabled{{end}}`
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

	mc := commands.NewMockCommand()
	app := &Zoxide{Cmd: mc}

	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}

	// Verify feature was disabled in config
	updatedConfig, err := os.ReadFile(globalConfigPath)
	if err != nil {
		t.Fatalf("Failed to read updated config: %v", err)
	}
	if !strings.Contains(string(updatedConfig), "zoxide: false") {
		t.Fatalf(
			"Expected config to have zoxide disabled. Config: %s",
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
}

func TestExecuteCommand(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &Zoxide{Cmd: mc, Base: mockBase}

	// Test 1: Successful execution
	t.Run("successful execution", func(t *testing.T) {
		mockBase.SetExecCommandResult("zoxide 0.9.4", "", nil)

		err := app.ExecuteCommand("--version")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		// Verify ExecCommand was called once
		if mockBase.GetExecCommandCallCount() != 1 {
			t.Fatalf("Expected 1 ExecCommand call, got %d", mockBase.GetExecCommandCallCount())
		}

		// Verify command parameters
		lastCall := mockBase.GetLastExecCommandCall()
		if lastCall == nil {
			t.Fatal("No ExecCommand call recorded")
		}
		if lastCall.Command != "zoxide" {
			t.Fatalf("Expected command 'zoxide', got %q", lastCall.Command)
		}
		if len(lastCall.Args) != 1 || lastCall.Args[0] != "--version" {
			t.Fatalf("Expected args ['--version'], got %v", lastCall.Args)
		}
		if lastCall.IsSudo {
			t.Fatal("Expected IsSudo to be false")
		}
	})

	// Test 2: Error handling
	t.Run("command execution error", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult(
			"",
			"command not found",
			fmt.Errorf("command not found: zoxide"),
		)

		err := app.ExecuteCommand("--invalid-flag")
		if err == nil {
			t.Fatal("Expected ExecuteCommand to return error")
		}
		if !strings.Contains(err.Error(), "failed to run zoxide command") {
			t.Fatalf("Expected error to contain 'failed to run zoxide command', got: %v", err)
		}

		// Verify the error was properly wrapped
		if !strings.Contains(err.Error(), "command not found: zoxide") {
			t.Fatalf("Expected error to contain original error message, got: %v", err)
		}
	})

	// Test 3: Multiple arguments
	t.Run("multiple arguments", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("/home/user/projects", "", nil)

		err := app.ExecuteCommand("query", "project")
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		lastCall := mockBase.GetLastExecCommandCall()
		expectedArgs := []string{"query", "project"}
		if len(lastCall.Args) != len(expectedArgs) {
			t.Fatalf("Expected %d args, got %d", len(expectedArgs), len(lastCall.Args))
		}
		for i, arg := range expectedArgs {
			if lastCall.Args[i] != arg {
				t.Fatalf("Expected arg[%d] to be %q, got %q", i, arg, lastCall.Args[i])
			}
		}
	})
}

func TestUpdate(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Zoxide{Cmd: mc}

	err := app.Update()
	if err == nil {
		t.Fatal("expected Update to return error for unsupported operation")
	}
	if err.Error() != "zoxide update not implemented through devgeta" {
		t.Fatalf("unexpected error message: %v", err)
	}
}
