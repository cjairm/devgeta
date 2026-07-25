package databases

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

	d := New()
	if d == nil {
		t.Fatal("Expected New() to return non-nil Databases")
	}
	if d.Cmd == nil {
		t.Error("Expected Cmd to be initialized")
	}
	if d.Base == nil {
		t.Error("Expected Base to be initialized")
	}
}

func TestGetDatabaseConfigs(t *testing.T) {
	configs := GetDatabaseConfigs()

	// 5 databases: MongoDB, MySQL, PostgreSQL, Redis, SQLite
	expectedCount := 5
	if len(configs) != expectedCount {
		t.Errorf("Expected %d database configs, got %d", expectedCount, len(configs))
	}

	// Verify each database has proper configuration
	for _, cfg := range configs {
		if cfg.DisplayName == "" {
			t.Error("Expected DisplayName to be set")
		}
		if cfg.Name == "" {
			t.Error("Expected Name to be set")
		}
	}
}

func TestGetSelectionOptions(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	d := New()
	databases := d.GetSelectionOptions()

	// Should have TUI controls + all configured databases
	configs := GetDatabaseConfigs()
	expectedCount := 3 + len(configs) // "All", "None", "Done" + databases
	if len(databases) != expectedCount {
		t.Errorf("Expected %d databases, got %d", expectedCount, len(databases))
	}

	// First three should always be TUI controls
	expectedControls := []string{"All", "None", "Done"}
	for i, control := range expectedControls {
		if databases[i] != control {
			t.Errorf("Expected databases[%d] to be %s, got %s", i, control, databases[i])
		}
	}

	// Remaining items should match configured database display names
	configNames := make(map[string]bool)
	for _, cfg := range configs {
		configNames[cfg.DisplayName] = true
	}

	for i := 3; i < len(databases); i++ {
		if !configNames[databases[i]] {
			t.Errorf("Database %s not found in configs", databases[i])
		}
	}
}

func TestGetSelectionOptions_DynamicGeneration(t *testing.T) {
	// This test verifies that GetSelectionOptions is dynamically generated
	// from GetDatabaseConfigs, ensuring consistency
	d := New()

	available := d.GetSelectionOptions()
	configs := GetDatabaseConfigs()

	// Every database in configs should be in available (after TUI controls)
	availableMap := make(map[string]bool)
	for _, db := range available[3:] { // Skip "All", "None", "Done"
		availableMap[db] = true
	}

	for _, cfg := range configs {
		if !availableMap[cfg.DisplayName] {
			t.Errorf("Database config %s not found in GetSelectionOptions", cfg.DisplayName)
		}
	}

	// Count should match
	if len(available)-3 != len(configs) {
		t.Errorf("Expected %d databases in available list, got %d",
			len(configs), len(available)-3)
	}
}

func TestToDisplayName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "mysql",
			input:    "mysql",
			expected: "MySQL",
		},
		{
			name:     "postgresql",
			input:    "postgresql",
			expected: "PostgreSQL",
		},
		{
			name:     "sqlite",
			input:    "sqlite",
			expected: "SQLite",
		},
		{
			name:     "mongodb",
			input:    "mongodb",
			expected: "MongoDB",
		},
		{
			name:     "redis",
			input:    "redis",
			expected: "Redis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toDisplayName(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestGetInstalledDatabases(t *testing.T) {
	d := New()

	// Create test global config
	gc := &config.GlobalConfig{}
	gc.Installed.Databases = []string{"redis", "postgresql"}
	gc.AlreadyInstalled.Databases = []string{"mysql"}

	installed := d.getInstalledDatabases(gc)

	expectedCount := 3 // redis, postgresql, mysql
	if len(installed) != expectedCount {
		t.Errorf("Expected %d installed databases, got %d", expectedCount, len(installed))
	}

	// Verify display names are returned
	expectedNames := []string{"Redis", "PostgreSQL", "MySQL"}
	for _, expected := range expectedNames {
		found := false
		for _, installed := range installed {
			if installed == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected to find %s in installed databases", expected)
		}
	}
}

func TestTrackInstallation(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	d := New()

	// Track installation
	err := d.trackInstallation("redis")
	if err != nil {
		t.Fatalf("trackInstallation failed: %v", err)
	}

	// Verify it was tracked
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !gc.IsInstalledByDevgeta("redis", "database") {
		t.Error("Expected redis to be tracked as installed")
	}
}

func TestInstallChosen_NoSelections(t *testing.T) {
	d := New()

	// Create context with no selections
	ctx := context.Background()
	initialConfig := config.ContextConfig{}
	initialConfig.SelectedDbs = []string{}
	ctx = config.WithConfig(ctx, initialConfig)

	// Should not panic or error
	d.InstallChosen(ctx)
}

func TestInstallChosen_WithSelections(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("", "", nil)

	d := &Databases{
		Cmd:  mockApp.Cmd,
		Base: mockApp.Base,
	}

	// Create context with selections
	ctx := context.Background()
	initialConfig := config.ContextConfig{}
	initialConfig.SelectedDbs = []string{"Redis"}
	ctx = config.WithConfig(ctx, initialConfig)

	// Install chosen (will attempt to install Redis)
	d.InstallChosen(ctx)

	// Verify package installation was attempted
	if mockApp.Cmd.MaybeInstalled != "redis" {
		t.Errorf("Expected to install redis, got %s", mockApp.Cmd.MaybeInstalled)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestGetVersionCommand(t *testing.T) {
	tests := []struct {
		name         string
		dbName       string
		expectedCmd  string
		expectedArgs []string
	}{
		{
			name:         "redis",
			dbName:       "redis",
			expectedCmd:  "redis-server",
			expectedArgs: []string{"--version"},
		},
		{
			name:         "postgresql",
			dbName:       "postgresql",
			expectedCmd:  "psql",
			expectedArgs: []string{"--version"},
		},
		{
			name:         "mysql",
			dbName:       "mysql",
			expectedCmd:  "mysql",
			expectedArgs: []string{"--version"},
		},
		{
			name:         "sqlite",
			dbName:       "sqlite",
			expectedCmd:  "sqlite3",
			expectedArgs: []string{"--version"},
		},
		{
			name:         "mongodb",
			dbName:       "mongodb",
			expectedCmd:  "mongod",
			expectedArgs: []string{"--version"},
		},
		{
			name:         "unknown database",
			dbName:       "cassandra",
			expectedCmd:  "cassandra",
			expectedArgs: []string{"--version"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, args := getVersionCommand(tt.dbName)
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

func TestIsDatabaseInstalledOnSystem(t *testing.T) {
	mockApp := testutil.NewMockApp()
	d := &Databases{
		Cmd:  mockApp.Cmd,
		Base: mockApp.Base,
	}

	t.Run("database installed", func(t *testing.T) {
		mockApp.Reset()
		// Simulate successful version check
		mockApp.Base.SetExecCommandResult("Redis server v=7.0.0", "", nil)

		dbCfg := DatabaseConfig{Name: "redis"}
		result := d.isDatabaseInstalledOnSystem(dbCfg)

		if !result {
			t.Error("Expected database to be detected as installed")
		}

		if mockApp.Base.GetExecCommandCallCount() != 1 {
			t.Errorf("Expected 1 ExecCommand call, got %d", mockApp.Base.GetExecCommandCallCount())
		}

		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall.Command != "redis-server" {
			t.Errorf("Expected command 'redis-server', got '%s'", lastCall.Command)
		}
	})

	t.Run("database not installed", func(t *testing.T) {
		mockApp.Reset()
		// Simulate command not found error
		mockApp.Base.SetExecCommandResult("", "command not found",
			fmt.Errorf("command not found"))

		dbCfg := DatabaseConfig{Name: "postgresql"}
		result := d.isDatabaseInstalledOnSystem(dbCfg)

		if result {
			t.Error("Expected database to be detected as not installed")
		}
	})
}

func TestDetectPreInstalledDatabases(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	mockApp := testutil.NewMockApp()

	t.Run("detects and tracks pre-installed database", func(t *testing.T) {
		mockApp.Reset()

		d := &Databases{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}

		// Simulate all databases are installed on system
		mockApp.Base.SetExecCommandResult("version output", "", nil)

		d.detectPreInstalledDatabases()

		// Verify config was updated - all databases should be detected
		gc := &config.GlobalConfig{}
		if err := gc.Load(); err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}

		// All should be detected since mock returns success for all
		if !gc.IsAlreadyInstalled("redis", "database") {
			t.Error("Expected redis to be tracked as already installed")
		}

		// Should have checked all 5 databases
		callCount := mockApp.Base.GetExecCommandCallCount()
		if callCount != 5 {
			t.Errorf("Expected 5 version checks, got %d", callCount)
		}
	})

	t.Run("skips already tracked databases", func(t *testing.T) {
		// Create fresh temp directory for this test
		tcLocal := testutil.SetupCompleteTest(t)
		defer tcLocal.Cleanup()

		mockApp.Reset()

		// Pre-populate config with redis
		gc := &config.GlobalConfig{}
		if err := gc.Load(); err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}
		gc.AddToInstalled("redis", "database")
		gc.AddToInstalled("postgresql", "database") // Track postgresql as well
		if err := gc.Save(); err != nil {
			t.Fatalf("Failed to save config: %v", err)
		}

		// Set all databases as "installed" on system
		mockApp.Base.SetExecCommandResult("version output", "", nil)

		d := &Databases{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}

		d.detectPreInstalledDatabases()

		// Should only check 3 databases (redis and postgresql were already tracked)
		callCount := mockApp.Base.GetExecCommandCallCount()
		if callCount != 3 {
			t.Errorf("Expected 3 version checks (skipping redis and postgresql), got %d", callCount)
		}
	})

	t.Run("does not track databases that are not installed", func(t *testing.T) {
		// Create fresh temp directory for this test
		tcLocal := testutil.SetupCompleteTest(t)
		defer tcLocal.Cleanup()

		mockApp.Reset()

		// Simulate all database version checks fail
		mockApp.Base.SetExecCommandResult("", "command not found", fmt.Errorf("command not found"))

		d := &Databases{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}

		d.detectPreInstalledDatabases()

		// Verify no databases were tracked
		gc := &config.GlobalConfig{}
		if err := gc.Load(); err != nil {
			t.Fatalf("Failed to load config: %v", err)
		}

		// None should be detected (check databases list is empty)
		if len(gc.AlreadyInstalled.Databases) > 0 {
			t.Errorf(
				"Expected no databases to be tracked, but found: %v",
				gc.AlreadyInstalled.Databases,
			)
		}
	})

	t.Run("handles config load error gracefully", func(t *testing.T) {
		mockApp.Reset()
		// Create Databases with invalid config path (will fail to load)
		// But detectPreInstalledDatabases should not panic
		d := &Databases{
			Cmd:  mockApp.Cmd,
			Base: mockApp.Base,
		}

		// Should not panic
		d.detectPreInstalledDatabases()
	})
}

func TestNew_CallsDetection(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	// New() should automatically call detectPreInstalledDatabases
	// We can verify this by checking if the function runs without errors
	d := New()

	if d == nil {
		t.Fatal("Expected New() to return non-nil Databases")
	}

	if d.Cmd == nil {
		t.Error("Expected Cmd to be initialized")
	}

	if d.Base == nil {
		t.Error("Expected Base to be initialized")
	}

	// GlobalConfig should have been loaded and potentially updated
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatalf("Expected global config to exist after New(): %v", err)
	}
}

func TestFilterSlice(t *testing.T) {
	tests := []struct {
		name     string
		source   []string
		exclude  []string
		expected []string
	}{
		{
			name:     "filter some items",
			source:   []string{"All", "None", "Done", "Redis", "MySQL", "PostgreSQL"},
			exclude:  []string{"Redis", "MySQL"},
			expected: []string{"All", "None", "Done", "PostgreSQL"},
		},
		{
			name:     "case insensitive filtering",
			source:   []string{"All", "None", "Done", "Redis", "MySQL"},
			exclude:  []string{"redis", "MYSQL"},
			expected: []string{"All", "None", "Done"},
		},
		{
			name:     "no exclusions",
			source:   []string{"All", "None", "Done"},
			exclude:  []string{},
			expected: []string{"All", "None", "Done"},
		},
		{
			name:     "exclude all",
			source:   []string{"Redis", "MySQL"},
			exclude:  []string{"Redis", "MySQL"},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterSlice(tt.source, tt.exclude)
			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d items, got %d", len(tt.expected), len(result))
			}
			for i, item := range tt.expected {
				if result[i] != item {
					t.Errorf("Expected result[%d] to be %s, got %s", i, item, result[i])
				}
			}
		})
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		items    []string
		expected bool
	}{
		{
			name:     "exact match",
			target:   "Redis",
			items:    []string{"Redis", "MySQL"},
			expected: true,
		},
		{
			name:     "case insensitive match",
			target:   "redis",
			items:    []string{"Redis", "MySQL"},
			expected: true,
		},
		{
			name:     "no match",
			target:   "PostgreSQL",
			items:    []string{"Redis", "MySQL"},
			expected: false,
		},
		{
			name:     "empty items",
			target:   "Redis",
			items:    []string{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsIgnoreCase(tt.target, tt.items)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestDatabaseConfig_AllNative(t *testing.T) {
	configs := GetDatabaseConfigs()

	// All databases use native package managers
	for _, cfg := range configs {
		if cfg.DisplayName == "" {
			t.Errorf("Database %s missing display name", cfg.Name)
		}
		if cfg.Name == "" {
			t.Errorf("Database missing name")
		}
	}

	// Verify expected databases are present
	expectedDatabases := map[string]bool{
		"MongoDB":    false,
		"MySQL":      false,
		"PostgreSQL": false,
		"Redis":      false,
		"SQLite":     false,
	}

	for _, cfg := range configs {
		if _, exists := expectedDatabases[cfg.DisplayName]; exists {
			expectedDatabases[cfg.DisplayName] = true
		}
	}

	for db, found := range expectedDatabases {
		if !found {
			t.Errorf("Expected database %s not found in configs", db)
		}
	}
}

func TestIsInstalledOnSystem_KnownDatabaseOK(t *testing.T) {
	mockApp := testutil.NewMockApp()
	d := &Databases{Cmd: mockApp.Cmd, Base: mockApp.Base}
	mockApp.Base.SetExecCommandResult("redis-server 7.2.0", "", nil)

	if !d.IsInstalledOnSystem("redis") {
		t.Error("expected redis to be detected as installed")
	}
}

func TestIsInstalledOnSystem_KnownDatabaseMissing(t *testing.T) {
	mockApp := testutil.NewMockApp()
	d := &Databases{Cmd: mockApp.Cmd, Base: mockApp.Base}
	mockApp.Base.SetExecCommandResult("", "command not found", fmt.Errorf("command not found"))

	if d.IsInstalledOnSystem("postgresql") {
		t.Error("expected postgresql to be detected as missing")
	}
}

func TestIsInstalledOnSystem_UnknownNameReturnsFalse(t *testing.T) {
	mockApp := testutil.NewMockApp()
	d := &Databases{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if d.IsInstalledOnSystem("cassandra") {
		t.Error("expected an unrecognized database name to return false")
	}
	testutil.VerifyNoRealCommands(t, mockApp.Base)
}
