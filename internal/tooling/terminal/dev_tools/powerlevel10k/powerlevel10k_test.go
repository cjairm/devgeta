package powerlevel10k

import (
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
	// Initialize logger for tests
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
	app := &PowerLevel10k{Cmd: mc}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mc.InstalledPkg != constants.Powerlevel10k {
		t.Fatalf(
			"expected InstallPackage(%s), got %q",
			constants.Powerlevel10k,
			mc.InstalledPkg,
		)
	}
}

// SKIP: ForceInstall test as per guidelines
// ForceInstall calls Uninstall (which now modifies config) before Install
// Testing this creates complex state management in tests

func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &PowerLevel10k{Cmd: mc}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mc.MaybeInstalled != constants.Powerlevel10k {
		t.Fatalf(
			"expected MaybeInstallPackage(%s), got %q",
			constants.Powerlevel10k,
			mc.MaybeInstalled,
		)
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
{{if .Powerlevel10k}}
# Powerlevel10k - Fast, customizable Zsh theme
if [[ -f $(brew --prefix)/share/powerlevel10k/powerlevel10k.zsh-theme ]]; then
    source $(brew --prefix)/share/powerlevel10k/powerlevel10k.zsh-theme
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

	mc := commands.NewMockCommand()
	app := &PowerLevel10k{Cmd: mc}

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	// Verify generated file contains powerlevel10k
	outputPath := filepath.Join(tempDir, "devgeta.zsh")
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read generated file: %v", err)
	}

	expectedContent := "source $(brew --prefix)/share/powerlevel10k/powerlevel10k.zsh-theme"
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
	if !strings.Contains(string(updatedConfig), "powerlevel10k: true") {
		t.Fatalf(
			"Expected config to have powerlevel10k enabled. Config: %s",
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
		templateContent := `{{if .Powerlevel10k}}enabled{{end}}`
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
		app := &PowerLevel10k{Cmd: mc}

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
  zoxide: false
  zsh_autosuggestions: false
  zsh_syntax_highlighting: false
  powerlevel10k: true
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
		app := &PowerLevel10k{Cmd: mc}

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
  zoxide: false
  zsh_autosuggestions: false
  zsh_syntax_highlighting: false
  powerlevel10k: true
  extended_capabilities: false
`
	if err := os.WriteFile(globalConfigPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("Failed to create global config: %v", err)
	}

	// Create template
	templatePath := filepath.Join(templatesDir, constants.App.Template.ShellConfig)
	templateContent := `{{if .Powerlevel10k}}enabled{{else}}disabled{{end}}`
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
	app := &PowerLevel10k{Cmd: mc}

	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}

	// Verify feature was disabled in config
	updatedConfig, err := os.ReadFile(globalConfigPath)
	if err != nil {
		t.Fatalf("Failed to read updated config: %v", err)
	}
	if !strings.Contains(string(updatedConfig), "powerlevel10k: false") {
		t.Fatalf(
			"Expected config to have powerlevel10k disabled. Config: %s",
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
	mockBase := commands.NewMockBaseCommand()
	mc := commands.NewMockCommand()
	app := &PowerLevel10k{Cmd: mc, Base: mockBase}

	// Test successful execution
	mockBase.SetExecCommandResult("p10k configure output", "", nil)

	if err := app.ExecuteCommand("configure"); err != nil {
		t.Fatalf("ExecuteCommand error: %v", err)
	}

	// Verify command was called
	if mockBase.GetExecCommandCallCount() != 1 {
		t.Fatalf("Expected 1 ExecCommand call, got %d", mockBase.GetExecCommandCallCount())
	}

	lastCall := mockBase.GetLastExecCommandCall()
	if lastCall == nil {
		t.Fatal("No ExecCommand call recorded")
	}
	if lastCall.Command != "p10k" {
		t.Fatalf("Expected command 'p10k', got %q", lastCall.Command)
	}
	if len(lastCall.Args) != 1 || lastCall.Args[0] != "configure" {
		t.Fatalf("Expected args ['configure'], got %v", lastCall.Args)
	}
}

func TestReconfigure(t *testing.T) {
	mockBase := commands.NewMockBaseCommand()
	mc := commands.NewMockCommand()
	app := &PowerLevel10k{Cmd: mc, Base: mockBase}

	mockBase.SetExecCommandResult("p10k configure output", "", nil)

	if err := app.Reconfigure(); err != nil {
		t.Fatalf("Reconfigure error: %v", err)
	}

	// Verify ExecuteCommand was called with "configure"
	if mockBase.GetExecCommandCallCount() != 1 {
		t.Fatalf("Expected 1 ExecCommand call, got %d", mockBase.GetExecCommandCallCount())
	}

	lastCall := mockBase.GetLastExecCommandCall()
	if lastCall == nil {
		t.Fatal("No ExecCommand call recorded")
	}
	if lastCall.Command != "p10k" {
		t.Fatalf("Expected command 'p10k', got %q", lastCall.Command)
	}
	if len(lastCall.Args) != 1 || lastCall.Args[0] != "configure" {
		t.Fatalf("Expected args ['configure'], got %v", lastCall.Args)
	}
}
