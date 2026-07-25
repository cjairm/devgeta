package languages

import (
	"context"
	"fmt"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
)

func init() {
	testutil.InitLogger()
}

func TestNew(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	dl := New()
	if dl == nil {
		t.Fatal("Expected New() to return non-nil DevLanguages")
	}
	if dl.Cmd == nil {
		t.Error("Expected Cmd to be initialized")
	}
	if dl.Base == nil {
		t.Error("Expected Base to be initialized")
	}
}

func TestGetLanguageConfigs(t *testing.T) {
	configs := GetLanguageConfigs()

	// Updated for 11 languages: Bun, Deno, Elixir, Erlang, Go, Java, Node, PHP, Python, Ruby, Rust
	expectedCount := 11
	if len(configs) != expectedCount {
		t.Errorf("Expected %d language configs, got %d", expectedCount, len(configs))
	}

	// Verify each language has proper configuration
	for _, cfg := range configs {
		if cfg.DisplayName == "" {
			t.Error("Expected DisplayName to be set")
		}
		if cfg.Name == "" {
			t.Error("Expected Name to be set")
		}
		// PHP uses native, others use mise
		if cfg.DisplayName == "PHP" {
			if cfg.UseMise {
				t.Error("Expected PHP to use native package manager")
			}
			if cfg.Version != "" {
				t.Error("Expected PHP version to be empty for native install")
			}
		} else {
			if !cfg.UseMise {
				t.Errorf("Expected %s to use mise", cfg.DisplayName)
			}
			if cfg.Version == "" {
				t.Errorf("Expected %s to have version", cfg.DisplayName)
			}
		}
	}
}

func TestGetSelectionOptions(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	dl := New()
	languages := dl.GetSelectionOptions()

	// Should have TUI controls + all configured languages
	configs := GetLanguageConfigs()
	expectedCount := 3 + len(configs) // "All", "None", "Done" + languages
	if len(languages) != expectedCount {
		t.Errorf("Expected %d languages, got %d", expectedCount, len(languages))
	}

	// First three should always be TUI controls
	expectedControls := []string{"All", "None", "Done"}
	for i, control := range expectedControls {
		if languages[i] != control {
			t.Errorf("Expected languages[%d] to be %s, got %s", i, control, languages[i])
		}
	}

	// Remaining items should match configured language display names
	configNames := make(map[string]bool)
	for _, cfg := range configs {
		configNames[cfg.DisplayName] = true
	}

	for i := 3; i < len(languages); i++ {
		if !configNames[languages[i]] {
			t.Errorf("Language %s not found in configs", languages[i])
		}
	}
}

func TestGetSelectionOptions_DynamicGeneration(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	// This test verifies that GetSelectionOptions is dynamically generated
	// from GetLanguageConfigs, ensuring consistency
	dl := New()

	available := dl.GetSelectionOptions()
	configs := GetLanguageConfigs()

	// Every language in configs should be in available (after TUI controls)
	availableMap := make(map[string]bool)
	for _, lang := range available[3:] { // Skip "All", "None", "Done"
		availableMap[lang] = true
	}

	for _, cfg := range configs {
		if !availableMap[cfg.DisplayName] {
			t.Errorf("Language config %s not found in GetSelectionOptions", cfg.DisplayName)
		}
	}

	// Count should match
	if len(available)-3 != len(configs) {
		t.Errorf("Expected %d languages in available list, got %d",
			len(configs), len(available)-3)
	}
}

func TestFormatSpec(t *testing.T) {
	tests := []struct {
		name     string
		langName string
		version  string
		useMise  bool
		expected string
	}{
		{
			name:     "mise language",
			langName: "node",
			version:  "lts",
			useMise:  true,
			expected: "node@lts",
		},
		{
			name:     "native language",
			langName: "php",
			version:  "",
			useMise:  false,
			expected: "php",
		},
		{
			name:     "mise with latest",
			langName: "go",
			version:  "latest",
			useMise:  true,
			expected: "go@latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatSpec(tt.langName, tt.version, tt.useMise)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestGetInstalledLanguages(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	dl := New()

	// Create test global config
	gc := &config.GlobalConfig{}
	gc.Installed.DevLanguages = []string{"node@lts", "python@latest"}
	gc.AlreadyInstalled.DevLanguages = []string{"php"}

	installed := dl.getInstalledLanguages(gc)

	expectedCount := 3 // node, python, php
	if len(installed) != expectedCount {
		t.Errorf("Expected %d installed languages, got %d", expectedCount, len(installed))
	}

	// Verify display names are returned
	expectedNames := []string{"Node", "Python", "PHP"}
	for _, expected := range expectedNames {
		found := false
		for _, installed := range installed {
			if installed == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find %s in installed languages", expected)
		}
	}
}

func TestTrackInstallation(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	dl := New()

	// Track installation
	err := dl.trackInstallation("node@lts")
	if err != nil {
		t.Fatalf("trackInstallation failed: %v", err)
	}

	// Verify it was tracked
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !gc.IsInstalledByDevgeta("node@lts", "dev_language") {
		t.Error("Expected node@lts to be tracked as installed")
	}
}

func TestInstallChosen_NoSelections(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	dl := New()

	// Create context with no selections
	ctx := context.Background()
	initialConfig := config.ContextConfig{}
	initialConfig.SelectedLanguages = []string{}
	ctx = config.WithConfig(ctx, initialConfig)

	// Should not panic or error
	dl.InstallChosen(ctx)
}

func TestInstallChosen_WithSelections(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("", "", nil)

	dl := &DevLanguages{
		Cmd:  mockApp.Cmd,
		Base: mockApp.Base,
	}

	// Create context with selections
	ctx := context.Background()
	initialConfig := config.ContextConfig{}
	initialConfig.SelectedLanguages = []string{"PHP"} // Native install for simpler test
	ctx = config.WithConfig(ctx, initialConfig)

	// Install chosen (will attempt to install PHP)
	dl.InstallChosen(ctx)

	// Verify package installation was attempted
	if mockApp.Cmd.MaybeInstalled != "php" {
		t.Errorf("Expected to install php, got %s", mockApp.Cmd.MaybeInstalled)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestLanguageConfig_MiseVsNative(t *testing.T) {
	configs := GetLanguageConfigs()

	miseLanguages := 0
	nativeLanguages := 0

	for _, cfg := range configs {
		if cfg.UseMise {
			miseLanguages++
			// Mise languages should have version
			if cfg.Version == "" {
				t.Errorf("Mise language %s missing version", cfg.DisplayName)
			}
		} else {
			nativeLanguages++
		}
	}

	// Updated for 11 languages: 10 via Mise, 1 native (PHP)
	if nativeLanguages != 1 {
		t.Errorf("Expected 1 native language, got %d", nativeLanguages)
	}
	if miseLanguages != 10 {
		t.Errorf("Expected 10 mise languages, got %d", miseLanguages)
	}
}

func TestGetVersionCommand(t *testing.T) {
	tests := []struct {
		name         string
		langCfg      LanguageConfig
		expectedCmd  string
		expectedArgs []string
	}{
		{
			name:         "node",
			langCfg:      LanguageConfig{Name: "node"},
			expectedCmd:  "node",
			expectedArgs: []string{"--version"},
		},
		{
			name:         "python",
			langCfg:      LanguageConfig{Name: "python"},
			expectedCmd:  "python3",
			expectedArgs: []string{"--version"},
		},
		{
			name:         "go",
			langCfg:      LanguageConfig{Name: "go"},
			expectedCmd:  "go",
			expectedArgs: []string{"version"},
		},
		{
			name:         "php",
			langCfg:      LanguageConfig{Name: "php"},
			expectedCmd:  "php",
			expectedArgs: []string{"--version"},
		},
		{
			name:         "rust",
			langCfg:      LanguageConfig{Name: "rust"},
			expectedCmd:  "rustc",
			expectedArgs: []string{"--version"},
		},
		{
			name:         "unknown language",
			langCfg:      LanguageConfig{Name: "elixir"},
			expectedCmd:  "elixir",
			expectedArgs: []string{"--version"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args := getVersionCommand(tt.langCfg.Name)
			if cmd != tt.expectedCmd {
				t.Errorf("Expected command %s, got %s", tt.expectedCmd, cmd)
			}
			if len(args) != len(tt.expectedArgs) {
				t.Errorf("Expected %d args, got %d", len(tt.expectedArgs), len(args))
			}
			for i, arg := range tt.expectedArgs {
				if args[i] != arg {
					t.Errorf("Expected arg[%d] to be %s, got %s", i, arg, args[i])
				}
			}
		})
	}
}

func TestIsLanguageInstalledOnSystem(t *testing.T) {
	mockApp := testutil.NewMockApp()
	dl := &DevLanguages{
		Cmd:  mockApp.Cmd,
		Base: mockApp.Base,
	}

	t.Run("language installed", func(t *testing.T) {
		mockApp.Reset()
		// Simulate successful version check
		mockApp.Base.SetExecCommandResult("v20.0.0", "", nil)

		langCfg := LanguageConfig{Name: "node"}
		result := dl.isLanguageInstalledOnSystem(langCfg)

		if !result {
			t.Error("Expected language to be detected as installed")
		}

		if mockApp.Base.GetExecCommandCallCount() != 1 {
			t.Errorf("Expected 1 ExecCommand call, got %d", mockApp.Base.GetExecCommandCallCount())
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall.Command != "node" {
			t.Errorf("Expected command 'node', got '%s'", lastCall.Command)
		}
	})

	t.Run("language not installed", func(t *testing.T) {
		mockApp.Reset()
		// Simulate command not found error
		mockApp.Base.SetExecCommandResult("", "command not found",
			fmt.Errorf("command not found"))

		langCfg := LanguageConfig{Name: "go"}
		result := dl.isLanguageInstalledOnSystem(langCfg)

		if result {
			t.Error("Expected language to be detected as not installed")
		}
	})
}

func TestDetectPreInstalledLanguages(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	mockApp := testutil.NewMockApp()

	t.Run("detects and tracks pre-installed language", func(t *testing.T) {
		mockApp.Reset()

		dl := &DevLanguages{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}

		// Simulate first call (node) succeeds, others fail
		// Since MockBaseCommand returns the same result for all calls,
		// we'll test the behavior with all commands succeeding or all failing
		mockApp.Base.SetExecCommandResult("v20.0.0", "", nil)

		dl.detectPreInstalledLanguages()

		// Verify config was updated - all languages should be detected
		gc := &config.GlobalConfig{}
		if err := gc.Load(); err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}

		// All should be detected since mock returns success for all
		if !gc.IsAlreadyInstalled("node@lts", "dev_language") {
			t.Error("Expected node@lts to be tracked as already installed")
		}

		// Should have checked all 11 languages
		callCount := mockApp.Base.GetExecCommandCallCount()
		if callCount != 11 {
			t.Errorf("Expected 11 version checks, got %d", callCount)
		}
	})

	t.Run("skips already tracked languages", func(t *testing.T) {
		// Create fresh temp directory for this test
		tcLocal := testutil.SetupCompleteTest(t)
		defer tcLocal.Cleanup()

		mockApp.Reset()

		// Pre-populate config with node
		gc := &config.GlobalConfig{}
		if err := gc.Load(); err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}
		gc.AddToInstalled("node@lts", "dev_language")
		gc.AddToInstalled("go@latest", "dev_language") // Track go as well
		if err := gc.Save(); err != nil {
			t.Fatalf("Failed to save config: %v", err)
		}

		// Set all languages as "installed" on system
		mockApp.Base.SetExecCommandResult("version output", "", nil)

		dl := &DevLanguages{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}

		dl.detectPreInstalledLanguages()

		// Should only check 9 languages (node and go were already tracked)
		callCount := mockApp.Base.GetExecCommandCallCount()
		if callCount != 9 {
			t.Errorf("Expected 9 version checks (skipping node and go), got %d", callCount)
		}
	})

	t.Run("does not track languages that are not installed", func(t *testing.T) {
		// Create fresh temp directory for this test
		tcLocal := testutil.SetupCompleteTest(t)
		defer tcLocal.Cleanup()

		mockApp.Reset()

		// Simulate all language version checks fail
		mockApp.Base.SetExecCommandResult("", "command not found", fmt.Errorf("command not found"))

		dl := &DevLanguages{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}

		dl.detectPreInstalledLanguages()

		// Verify no languages were tracked
		gc := &config.GlobalConfig{}
		if err := gc.Load(); err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}

		// None should be detected (check dev_languages list is empty)
		if len(gc.AlreadyInstalled.DevLanguages) > 0 {
			t.Errorf(
				"Expected no languages to be tracked, but found: %v",
				gc.AlreadyInstalled.DevLanguages,
			)
		}
	})

	t.Run("handles config load error gracefully", func(t *testing.T) {
		mockApp.Reset()
		// Create DevLanguages with invalid config path (will fail to load)
		// But detectPreInstalledLanguages should not panic
		dl := &DevLanguages{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}

		// Should not panic
		dl.detectPreInstalledLanguages()
	})
}

func TestNew_CallsDetection(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	// New() should automatically call detectPreInstalledLanguages
	// We can verify this by checking if the function runs without errors
	dl := New()

	if dl == nil {
		t.Fatal("Expected New() to return non-nil DevLanguages")
	}

	if dl.Cmd == nil {
		t.Error("Expected Cmd to be initialized")
	}

	if dl.Base == nil {
		t.Error("Expected Base to be initialized")
	}

	// GlobalConfig should have been loaded and potentially updated
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatalf("Expected global config to exist after New(): %v", err)
	}
}

func TestIsInstalledOnSystem_MiseLanguageOK(t *testing.T) {
	mockApp := testutil.NewMockApp()
	dl := &DevLanguages{Cmd: mockApp.Cmd, Base: mockApp.Base}
	mockApp.Base.SetExecCommandResult("v20.0.0", "", nil)

	// "node@lts" is exactly what formatSpec produces for the mise-managed Node config.
	if !dl.IsInstalledOnSystem("node@lts") {
		t.Error("expected node@lts to be detected as installed")
	}
}

func TestIsInstalledOnSystem_NativeLanguageMissing(t *testing.T) {
	mockApp := testutil.NewMockApp()
	dl := &DevLanguages{Cmd: mockApp.Cmd, Base: mockApp.Base}
	mockApp.Base.SetExecCommandResult("", "command not found", fmt.Errorf("command not found"))

	// "php" (no version suffix — PHP is UseMise: false) is what formatSpec produces for PHP.
	if dl.IsInstalledOnSystem("php") {
		t.Error("expected php to be detected as missing")
	}
}

func TestIsInstalledOnSystem_UnknownSpecReturnsFalse(t *testing.T) {
	mockApp := testutil.NewMockApp()
	dl := &DevLanguages{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if dl.IsInstalledOnSystem("cobol@1985") {
		t.Error("expected an unrecognized language spec to return false")
	}
	testutil.VerifyNoRealCommands(t, mockApp.Base)
}
