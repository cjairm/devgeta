package commands_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
	"gopkg.in/yaml.v3"
)

func init() {
	// Some tests in this file (e.g. TestMaybeSetupInFile_*) exercise
	// files.ContentExistsInFile, which logs through the package logger. Init
	// it here instead of relying on test execution order across this file.
	testutil.InitLogger()
}

type FakePlatform struct {
	Linux bool
	Mac   bool
}

func (f FakePlatform) IsLinux() bool { return f.Linux }
func (f FakePlatform) IsMac() bool   { return f.Mac }

func createFile(t *testing.T, dir, name string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o644)
	if err != nil {
		t.Fatalf("Failed to create test file %q: %v", name, err)
	}
}

func fakeCmdWithOutput(output string) *exec.Cmd {
	return exec.Command("bash", "-c", "echo -e \""+output+"\"")
}

func TestIsDesktopAppPresent(t *testing.T) {
	t.Run("Linux with matching .desktop file", func(t *testing.T) {
		tmpDir := t.TempDir()
		createFile(t, tmpDir, "myapp.desktop")

		b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})
		found, err := b.IsDesktopAppPresent(tmpDir, "myapp")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !found {
			t.Errorf("Expected to find the desktop app, but did not")
		}
	})

	t.Run("Mac with matching file", func(t *testing.T) {
		tmpDir := t.TempDir()
		createFile(t, tmpDir, "myapp")

		b := commands.NewBaseCommandCustom(FakePlatform{Mac: true})
		found, err := b.IsDesktopAppPresent(tmpDir, "myapp")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !found {
			t.Errorf("Expected to find the app file, but did not")
		}
	})

	t.Run("Linux: no match with wrong extension", func(t *testing.T) {
		tmpDir := t.TempDir()
		createFile(t, tmpDir, "myapp.txt")

		b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})
		found, err := b.IsDesktopAppPresent(tmpDir, "myapp")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if found {
			t.Errorf("Expected not to find a desktop app, but did")
		}
	})

	t.Run("Mac: partial match not found", func(t *testing.T) {
		tmpDir := t.TempDir()
		createFile(t, tmpDir, "unrelated")

		b := commands.NewBaseCommandCustom(FakePlatform{Mac: true})
		found, err := b.IsDesktopAppPresent(tmpDir, "myapp")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if found {
			t.Errorf("Expected not to find the app file, but did")
		}
	})

	t.Run("Linux: case-insensitive match with .desktop", func(t *testing.T) {
		tmpDir := t.TempDir()
		createFile(t, tmpDir, "MyApp.Desktop")

		b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})
		found, err := b.IsDesktopAppPresent(tmpDir, "myapp")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !found {
			t.Errorf("Expected to find desktop app with case-insensitive match")
		}
	})

	t.Run("Linux: directory read error", func(t *testing.T) {
		b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})
		found, err := b.IsDesktopAppPresent("/nonexistent/path", "app")
		if err == nil {
			t.Fatalf("Expected error reading nonexistent directory, got nil")
		}
		if found {
			t.Errorf("Expected not to find the app file, but did")
		}
	})
}

func TestIsPackagePresent_Mac(t *testing.T) {
	b := commands.NewBaseCommandCustom(FakePlatform{Mac: true})

	t.Run("Exact match in brew output", func(t *testing.T) {
		cmd := fakeCmdWithOutput("pkg1\nmytool\npkg2")
		found, err := b.IsPackagePresent(cmd, "mytool")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !found {
			t.Errorf("Expected to find package 'mytool', but did not")
		}
	})

	t.Run("No match in brew output", func(t *testing.T) {
		cmd := fakeCmdWithOutput("pkg1\npkg2")
		found, err := b.IsPackagePresent(cmd, "missing")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if found {
			t.Errorf("Expected not to find package 'missing', but did")
		}
	})
}

func TestIsPackagePresent_Linux(t *testing.T) {
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	t.Run("Match in dpkg output", func(t *testing.T) {
		// Simulate `dpkg -l` format: status name version
		cmd := fakeCmdWithOutput("ii  mytool  1.0.0\nrc  oldtool  0.9.0")
		found, err := b.IsPackagePresent(cmd, "mytool")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !found {
			t.Errorf("Expected to find package 'mytool', but did not")
		}
	})

	t.Run("No match in dpkg output", func(t *testing.T) {
		cmd := fakeCmdWithOutput("ii  someother  1.0.0")
		found, err := b.IsPackagePresent(cmd, "missing")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if found {
			t.Errorf("Expected not to find package 'missing', but did")
		}
	})
}

func TestIsPackagePresent_CommandError(t *testing.T) {
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	// Using an invalid command to trigger an error
	cmd := exec.Command("false") // This always exits with non-zero status
	_, err := b.IsPackagePresent(cmd, "anything")
	if err == nil {
		t.Fatalf("Expected error from failed command, got nil")
	}
}

func TestIsFontPresent(t *testing.T) {
	t.Run("Linux: fc-list detects font", func(t *testing.T) {
		b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})
		// Only works if `fc-list` and the font actually exist in system
		found, err := b.IsFontPresent("DejaVu Sans")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		// Can't assert found == true unless we're sure the font is installed,
		// so just log the result
		t.Logf("DejaVu Sans found: %v", found)
	})

	t.Run("Fallback to directory scan - font present", func(t *testing.T) {
		tmpUser := t.TempDir()
		tmpSystem := t.TempDir()

		paths.Paths.User.Fonts = tmpUser
		paths.Paths.System.Fonts = tmpSystem

		createFile(t, tmpSystem, "myfont.ttf")

		b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})
		commands.LookPathFn = func(string) (string, error) {
			return "", exec.ErrNotFound
		}
		commands.CommandFn = func(name string, args ...string) *exec.Cmd {
			t.Fatalf("fc-list should not be called during fallback")
			return nil
		}

		found, err := b.IsFontPresent("myfont")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !found {
			t.Errorf("Expected to find the font via fallback directory scan, but did not")
		}
	})

	t.Run("Fallback to directory scan - font not found", func(t *testing.T) {
		tmpUserDir := t.TempDir()
		tmpSystemDir := t.TempDir()

		// override paths
		originalUser := paths.Paths.User.Fonts
		originalSystem := paths.Paths.System.Fonts
		paths.Paths.User.Fonts = tmpUserDir
		paths.Paths.System.Fonts = tmpSystemDir
		defer func() {
			paths.Paths.User.Fonts = originalUser
			paths.Paths.System.Fonts = originalSystem
		}()

		b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})
		found, err := b.IsFontPresent("UnknownFont")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if found {
			t.Errorf("Expected not to find nonexistent font")
		}
	})

	t.Run("Fallback to directory scan - font present with valid extension", func(t *testing.T) {
		tmpUser := t.TempDir()
		tmpSystem := t.TempDir()

		paths.Paths.User.Fonts = tmpUser
		paths.Paths.System.Fonts = tmpSystem

		createFile(t, tmpUser, "FancyFont.OTF")

		b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})
		commands.LookPathFn = func(string) (string, error) {
			return "", exec.ErrNotFound
		}
		commands.CommandFn = func(name string, args ...string) *exec.Cmd {
			t.Fatalf("fc-list should not be called during fallback")
			return nil
		}

		found, err := b.IsFontPresent("fancyfont")
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		if !found {
			t.Errorf("Expected to find font via fallback directory scan, but did not")
		}
	})
}

// Mock install function that tracks calls for MaybeInstall tests
type MockInstaller struct {
	InstallCalled bool
	InstalledItem string
	ShouldFail    bool
}

func (m *MockInstaller) Install(item string) error {
	m.InstallCalled = true
	m.InstalledItem = item
	if m.ShouldFail {
		return fmt.Errorf("mock install error")
	}
	return nil
}

func (m *MockInstaller) Check(item string) (bool, error) {
	// Default: item not installed
	return false, nil
}

type MockInstallerPresent struct {
	MockInstaller
}

func (m *MockInstallerPresent) Check(item string) (bool, error) {
	// Item is already installed
	return true, nil
}

// Helper function to setup test environment for MaybeInstall tests
func setupMaybeInstallTest(t *testing.T, testConfig *config.GlobalConfig) (cleanup func()) {
	// Initialize logger for tests
	logger.Init(false)

	// Set up temporary config - each test gets its own unique directory
	tempDir := t.TempDir() // This creates a unique temp dir per test
	configDir := filepath.Join(tempDir, constants.App.Name)
	os.MkdirAll(configDir, 0o755)
	configPath := filepath.Join(configDir, constants.App.File.GlobalConfig)

	// Marshal and write the test config
	data, err := yaml.Marshal(testConfig)
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	err = os.WriteFile(configPath, data, 0o644)
	if err != nil {
		t.Fatalf("Failed to write test config file: %v", err)
	}

	// Override global config path
	originalConfigDir := paths.Paths.Config.Root
	paths.Paths.Config.Root = tempDir

	return func() {
		paths.Paths.Config.Root = originalConfigDir
	}
}

func TestMaybeInstall_ItemNotInstalled_InstallsSuccessfully(t *testing.T) {
	// Create test config
	testConfig := &config.GlobalConfig{
		Installed: config.InstalledConfig{
			Packages: []string{},
		},
		AlreadyInstalled: config.AlreadyInstalledConfig{
			Packages: []string{},
		},
	}

	cleanup := setupMaybeInstallTest(t, testConfig)
	defer cleanup()

	b := commands.NewBaseCommandCustom(FakePlatform{Mac: true})
	mockInstaller := &MockInstaller{}

	err := b.MaybeInstall(
		"test-package",
		[]string{},
		mockInstaller.Check,
		mockInstaller.Install,
		nil,
		"package",
	)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if !mockInstaller.InstallCalled {
		t.Errorf("Expected install to be called")
	}

	if mockInstaller.InstalledItem != "test-package" {
		t.Errorf("Expected to install 'test-package', got '%s'", mockInstaller.InstalledItem)
	}

	// Verify item was added to installed config
	var updatedConfig config.GlobalConfig
	updatedConfig.Load()
	if len(updatedConfig.Installed.Packages) != 1 ||
		updatedConfig.Installed.Packages[0] != "test-package" {
		t.Errorf("Expected 'test-package' to be added to installed config")
	}
}

func TestMaybeInstall_ItemAlreadyInstalledByDevgeta_SkipsInstall(t *testing.T) {
	// Create test config with item already installed by devgeta
	testConfig := &config.GlobalConfig{
		Installed: config.InstalledConfig{
			Packages: []string{"test-package"},
		},
		AlreadyInstalled: config.AlreadyInstalledConfig{
			Packages: []string{},
		},
	}

	cleanup := setupMaybeInstallTest(t, testConfig)
	defer cleanup()

	b := commands.NewBaseCommandCustom(FakePlatform{Mac: true})
	mockInstaller := &MockInstaller{}

	err := b.MaybeInstall(
		"test-package",
		[]string{},
		mockInstaller.Check,
		mockInstaller.Install,
		nil,
		"package",
	)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if mockInstaller.InstallCalled {
		t.Errorf("Expected install NOT to be called for already installed item")
	}
}

func TestMaybeInstall_ItemPreExisting_TracksAsAlreadyInstalled(t *testing.T) {
	// Create test config with empty lists
	testConfig := &config.GlobalConfig{
		Installed: config.InstalledConfig{
			Packages: []string{},
		},
		AlreadyInstalled: config.AlreadyInstalledConfig{
			Packages: []string{},
		},
	}

	// Set up temporary config
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, constants.App.Name)
	os.MkdirAll(configDir, 0o755)
	configPath := filepath.Join(configDir, constants.App.File.GlobalConfig)

	data, _ := yaml.Marshal(testConfig)
	os.WriteFile(configPath, data, 0o644)

	// Override global config path
	originalConfigDir := paths.Paths.Config.Root
	paths.Paths.Config.Root = tempDir
	defer func() {
		paths.Paths.Config.Root = originalConfigDir
	}()

	b := commands.NewBaseCommandCustom(FakePlatform{Mac: true})
	mockInstaller := &MockInstallerPresent{} // This will return true for Check()

	err := b.MaybeInstall(
		"pre-existing-package",
		[]string{},
		mockInstaller.Check,
		mockInstaller.Install,
		nil,
		"package",
	)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if mockInstaller.InstallCalled {
		t.Errorf("Expected install NOT to be called for pre-existing item")
	}

	// Verify item was added to already installed config
	var updatedConfig config.GlobalConfig
	updatedConfig.Load()
	if len(updatedConfig.AlreadyInstalled.Packages) != 1 ||
		updatedConfig.AlreadyInstalled.Packages[0] != "pre-existing-package" {
		t.Errorf("Expected 'pre-existing-package' to be added to already installed config")
	}
}

func TestMaybeSetupInFile_CreatesFileWhenMissing(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "does-not-exist-yet")

	b := commands.NewBaseCommandCustom(FakePlatform{Mac: true})
	line := "[ -f /some/script ] && source /some/script"

	if err := b.MaybeSetupInFile(line, "/some/script", targetFile); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("Expected file to be created, got error reading it: %v", err)
	}
	if !strings.Contains(string(content), line) {
		t.Errorf("Expected file to contain line %q, got: %q", line, string(content))
	}
}

func TestMaybeSetupInFile_SecondCallDoesNotDuplicate(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "zshenv")

	b := commands.NewBaseCommandCustom(FakePlatform{Mac: true})
	line := "[ -f /some/script ] && source /some/script"

	if err := b.MaybeSetupInFile(line, "/some/script", targetFile); err != nil {
		t.Fatalf("First call: expected no error, got: %v", err)
	}
	if err := b.MaybeSetupInFile(line, "/some/script", targetFile); err != nil {
		t.Fatalf("Second call: expected no error, got: %v", err)
	}

	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	occurrences := strings.Count(string(content), "/some/script")
	// The guarded line references the path twice ("[ -f X ] && source X"), so
	// one write of the line produces two occurrences; a duplicate write would
	// produce four.
	if occurrences != 2 {
		t.Errorf(
			"Expected line to be written exactly once (2 occurrences of the path), got %d occurrences in: %q",
			occurrences,
			string(content),
		)
	}
}

func TestMaybeSetupInFile_SkipsWhenAlreadyPresent(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "zshenv")

	existing := "# some pre-existing content\n[ -f /some/script ] && source /some/script\n"
	if err := os.WriteFile(targetFile, []byte(existing), 0o644); err != nil {
		t.Fatalf("Failed to seed file: %v", err)
	}

	b := commands.NewBaseCommandCustom(FakePlatform{Mac: true})
	line := "[ -f /some/script ] && source /some/script"

	if err := b.MaybeSetupInFile(line, "/some/script", targetFile); err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if content := string(content); content != existing {
		t.Errorf("Expected file to be untouched, got: %q", content)
	}
}

func TestMaybeSetupInFile_NonNotExistErrorIsPropagated(t *testing.T) {
	// A directory at filePath: os.Open succeeds (files.ContentExistsInFile
	// doesn't fail at open), but reading it via the scanner fails with "is a
	// directory" — not fs.ErrNotExist. MaybeSetupInFile must propagate that
	// scan error rather than treating it as "not set up yet" and appending to
	// it. Asserting the error text pins it to the scan failure specifically:
	// if MaybeSetupInFile swallowed the ContentExistsInFile error instead, it
	// would fall through to AddLineToFile, which also errors on a directory
	// (a different message, "failed to open file ... for appending") — so a
	// bare "err == nil" check alone would stay green under that mutation.
	tmpDir := t.TempDir()
	dirAsFilePath := filepath.Join(tmpDir, "a-directory")
	if err := os.Mkdir(dirAsFilePath, 0o755); err != nil {
		t.Fatalf("Failed to create directory: %v", err)
	}

	b := commands.NewBaseCommandCustom(FakePlatform{Mac: true})
	line := "[ -f /some/script ] && source /some/script"

	err := b.MaybeSetupInFile(line, "/some/script", dirAsFilePath)
	if err == nil {
		t.Fatal("Expected an error when filePath is a directory, got nil")
	}
	if !strings.Contains(err.Error(), "error scanning file") {
		t.Errorf("Expected the ContentExistsInFile scan error to be propagated, got: %v", err)
	}

	// Nothing should have been created inside the directory: the error must
	// short-circuit before AddLineToFile is ever reached.
	entries, err := os.ReadDir(dirAsFilePath)
	if err != nil {
		t.Fatalf("Failed to read directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected directory to remain empty, got entries: %v", entries)
	}
}

func TestMaybeInstall_DifferentItemTypes(t *testing.T) {
	testCases := []struct {
		name     string
		itemType string
		itemName string
	}{
		{"Font", "font", "test-font"},
		{"DesktopApp", "desktop_app", "test-app"},
		{"TerminalTool", "terminal_tool", "test-tool"},
		{"Theme", "theme", "test-theme"},
		{"DevLanguage", "dev_language", "test-language"},
		{"Database", "database", "test-db"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create test config
			testConfig := &config.GlobalConfig{
				Installed:        config.InstalledConfig{},
				AlreadyInstalled: config.AlreadyInstalledConfig{},
			}

			// Set up temporary config
			tempDir := t.TempDir()
			configDir := filepath.Join(tempDir, constants.App.Name)
			os.MkdirAll(configDir, 0o755)
			configPath := filepath.Join(configDir, constants.App.File.GlobalConfig)

			data, _ := yaml.Marshal(testConfig)
			os.WriteFile(configPath, data, 0o644)

			// Override global config path
			originalConfigDir := paths.Paths.Config.Root
			paths.Paths.Config.Root = tempDir
			defer func() {
				paths.Paths.Config.Root = originalConfigDir
			}()

			b := commands.NewBaseCommandCustom(FakePlatform{Mac: true})
			mockInstaller := &MockInstaller{}

			err := b.MaybeInstall(
				tc.itemName,
				[]string{},
				mockInstaller.Check,
				mockInstaller.Install,
				nil,
				tc.itemType,
			)
			if err != nil {
				t.Fatalf("Expected no error, got: %v", err)
			}

			if !mockInstaller.InstallCalled {
				t.Errorf("Expected install to be called for %s", tc.itemType)
			}

			// Verify item was added to correct section of installed config
			var updatedConfig config.GlobalConfig
			updatedConfig.Load()
			found := false
			switch tc.itemType {
			case "font":
				found = len(updatedConfig.Installed.Fonts) == 1 &&
					updatedConfig.Installed.Fonts[0] == tc.itemName
			case "desktop_app":
				found = len(updatedConfig.Installed.DesktopApps) == 1 &&
					updatedConfig.Installed.DesktopApps[0] == tc.itemName
			case "terminal_tool":
				found = len(updatedConfig.Installed.TerminalTools) == 1 &&
					updatedConfig.Installed.TerminalTools[0] == tc.itemName
			case "theme":
				found = len(updatedConfig.Installed.Themes) == 1 &&
					updatedConfig.Installed.Themes[0] == tc.itemName
			case "dev_language":
				found = len(updatedConfig.Installed.DevLanguages) == 1 &&
					updatedConfig.Installed.DevLanguages[0] == tc.itemName
			case "database":
				found = len(updatedConfig.Installed.Databases) == 1 &&
					updatedConfig.Installed.Databases[0] == tc.itemName
			}

			if !found {
				t.Errorf(
					"Expected '%s' to be added to installed %s config",
					tc.itemName,
					tc.itemType,
				)
			}
		})
	}
}
