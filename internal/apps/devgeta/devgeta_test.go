package devgeta

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/embedded"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
)

// fakePlatform is a minimal commands.CustomizablePlatform so tests can build a
// real *commands.BaseCommand — MaybeSetupInFile is a file-only operation, so
// exercising it with the real BaseCommand (against sandboxed paths) doesn't
// violate the no-real-commands testing rule, and it's the only way to assert
// on the actual file content the wiring produces (MockBaseCommand no-ops it).
type fakePlatform struct{}

func (fakePlatform) IsLinux() bool { return false }
func (fakePlatform) IsMac() bool   { return true }

// setupZshenvPaths isolates paths.Files.ShellConfig and paths.Files.ZshEnv to
// sandboxed temp files and restores the originals on cleanup, per the testing
// checklist requirement to save/restore every paths.Files.* mutation.
func setupZshenvPaths(t *testing.T, shellConfigBasename string) (zshenvPath string) {
	t.Helper()

	dir := t.TempDir()
	origShellConfig := paths.Files.ShellConfig
	origZshEnv := paths.Files.ZshEnv
	t.Cleanup(func() {
		paths.Files.ShellConfig = origShellConfig
		paths.Files.ZshEnv = origZshEnv
	})

	paths.Files.ShellConfig = filepath.Join(dir, shellConfigBasename)
	zshenvPath = filepath.Join(dir, ".zshenv")
	paths.Files.ZshEnv = zshenvPath
	return zshenvPath
}

func init() {
	testutil.InitLogger()
}

// mockExtractor creates a test configs directory with sample files
func mockExtractor(destDir string) error {
	// Create sample config directories
	dirs := []string{
		filepath.Join(destDir, "git"),
		filepath.Join(destDir, "neovim"),
		filepath.Join(destDir, "tmux"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	// Create sample config files
	files := map[string]string{
		filepath.Join(destDir, "git", ".gitconfig"):  "[user]\n\tname = Test\n",
		filepath.Join(destDir, "neovim", "init.lua"): "-- Test config\n",
		filepath.Join(destDir, "tmux", "tmux.conf"):  "# Test config\n",
	}

	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}

	return nil
}

func TestGetZshConfigPath_MatchesRegenerateShellConfigOutput(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	// getZshConfigPath must return the same path that RegenerateShellConfig writes to.
	// A mismatch causes the source line in .zshrc to point to a non-existent file.
	expected := filepath.Join(paths.Paths.App.Root, fmt.Sprintf("%s.zsh", constants.App.Name))
	actual := getZshConfigPath()

	if actual != expected {
		t.Errorf(
			"getZshConfigPath() = %q, want %q (must match RegenerateShellConfig output path)",
			actual,
			expected,
		)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestNew(t *testing.T) {
	// Set temporary default extractor for test
	oldExtractor := embedded.DefaultExtractor
	embedded.DefaultExtractor = mockExtractor
	t.Cleanup(func() {
		embedded.DefaultExtractor = oldExtractor
	})

	dg := New()
	if dg == nil {
		t.Fatal("New() returned nil")
	}
	if dg.ExtractEmbedded == nil {
		t.Fatal("ExtractEmbedded function is nil")
	}
}

func TestNameAndKind(t *testing.T) {
	dg := &Devgeta{}
	if dg.Name() != constants.DevgetaApp {
		t.Errorf("expected Name() %q, got %q", constants.DevgetaApp, dg.Name())
	}
	if dg.Kind() != apps.KindMeta {
		t.Errorf("expected Kind() KindMeta, got %v", dg.Kind())
	}
}

func TestSoftInstall_DirectoryDoesNotExist(t *testing.T) {
	tempDir := t.TempDir()
	appDir := filepath.Join(tempDir, "devgeta")

	oldAppDir := paths.Paths.App.Root
	paths.Paths.App.Root = appDir
	t.Cleanup(func() {
		paths.Paths.App.Root = oldAppDir
	})

	mockApp := testutil.NewMockApp()
	dg := &Devgeta{
		Base:            mockApp.Base,
		ExtractEmbedded: mockExtractor,
	}

	err := dg.SoftInstall()
	if err != nil {
		t.Fatalf("SoftInstall() failed: %v", err)
	}

	// Verify configs directory was created
	configsDir := filepath.Join(appDir, "configs")
	if _, err := os.Stat(configsDir); os.IsNotExist(err) {
		t.Fatal("Configs directory was not created")
	}

	// Verify sample files exist
	gitConfig := filepath.Join(configsDir, "git", ".gitconfig")
	if _, err := os.Stat(gitConfig); os.IsNotExist(err) {
		t.Fatal("Git config was not extracted")
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestSoftInstall_DirectoryExistsWithFiles(t *testing.T) {
	tempDir := t.TempDir()
	appDir := filepath.Join(tempDir, "devgeta")
	configsDir := filepath.Join(appDir, "configs")

	// Create existing configs directory with a file
	if err := os.MkdirAll(configsDir, 0o755); err != nil {
		t.Fatalf("Failed to create configs dir: %v", err)
	}
	testFile := filepath.Join(configsDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	oldAppDir := paths.Paths.App.Root
	paths.Paths.App.Root = appDir
	t.Cleanup(func() {
		paths.Paths.App.Root = oldAppDir
	})

	mockApp := testutil.NewMockApp()
	dg := &Devgeta{
		Base:            mockApp.Base,
		ExtractEmbedded: mockExtractor,
	}

	err := dg.SoftInstall()
	if err != nil {
		t.Fatalf("SoftInstall() failed: %v", err)
	}

	// Verify existing file was preserved (extraction not called)
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Fatal("Expected existing file to be preserved")
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestUninstall_RemovesConfigsDirectory(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	// Create configs directory in app root
	configsDir := filepath.Join(paths.Paths.App.Root, "configs")
	if err := os.MkdirAll(configsDir, 0o755); err != nil {
		t.Fatalf("Failed to create configs dir: %v", err)
	}
	testFile := filepath.Join(configsDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0o644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	dg := &Devgeta{
		Base:            tc.MockApp.Base,
		ExtractEmbedded: mockExtractor,
	}

	err := dg.Uninstall()
	if err != nil {
		t.Fatalf("Uninstall() failed: %v", err)
	}

	// Verify configs directory was removed
	if _, err := os.Stat(configsDir); !os.IsNotExist(err) {
		t.Fatal("Expected configs directory to be removed")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestForceInstall(t *testing.T) {
	// ForceInstall runs Uninstall, which also touches Config.Root paths —
	// SetupCompleteTest isolates both App.Root and Config.Root.
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	configsDir := filepath.Join(paths.Paths.App.Root, "configs")

	// Create existing configs directory
	if err := os.MkdirAll(configsDir, 0o755); err != nil {
		t.Fatalf("Failed to create configs dir: %v", err)
	}
	oldFile := filepath.Join(configsDir, "old.txt")
	if err := os.WriteFile(oldFile, []byte("old"), 0o644); err != nil {
		t.Fatalf("Failed to create old file: %v", err)
	}

	dg := &Devgeta{
		Base:            tc.MockApp.Base,
		ExtractEmbedded: mockExtractor,
	}

	err := dg.ForceInstall()
	if err != nil {
		t.Fatalf("ForceInstall() failed: %v", err)
	}

	// Verify old file was removed and new configs extracted
	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("Expected old file to be removed")
	}

	gitConfig := filepath.Join(configsDir, "git", ".gitconfig")
	if _, err := os.Stat(gitConfig); os.IsNotExist(err) {
		t.Fatal("Expected new configs to be extracted")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestForceConfigure_CreatesConfig(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	dg := &Devgeta{
		Base:            tc.MockApp.Base,
		ExtractEmbedded: mockExtractor,
	}

	err := dg.ForceConfigure()
	if err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}

	// Verify global config was created
	if _, err := os.Stat(tc.ConfigPath); os.IsNotExist(err) {
		t.Fatal("Global config was not created")
	}

	// Verify IsMac field was set
	content, err := os.ReadFile(tc.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to read global config: %v", err)
	}

	// Check for is_mac field in YAML
	if !strings.Contains(string(content), "is_mac:") {
		t.Error("Expected is_mac field in global config")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestForceConfigure_OverwritesExisting(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	// Create existing config file
	oldContent := "old: config\n"
	if err := os.WriteFile(tc.ConfigPath, []byte(oldContent), 0o644); err != nil {
		t.Fatalf("Failed to create old config: %v", err)
	}

	dg := &Devgeta{
		Base:            tc.MockApp.Base,
		ExtractEmbedded: mockExtractor,
	}

	err := dg.ForceConfigure()
	if err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}

	// Verify config was overwritten
	content, err := os.ReadFile(tc.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	if strings.Contains(string(content), "old: config") {
		t.Error("Expected old config to be overwritten")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestSoftConfigure_PreservesExistingConfig(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	// Create existing config file with custom marker AND extended_capabilities enabled
	// This is key - if extended_capabilities is false, SoftConfigure will regenerate the config
	existingContent := `# Custom config marker
app_path: /custom/path
config_path: ""
shell:
  mise: false
  zoxide: false
  zsh_autosuggestions: false
  zsh_syntax_highlighting: false
  powerlevel10k: false
  extended_capabilities: true
  lazy_git: false
  lazy_docker: false
  fzf: false
  neovim: false
  tmux: false
  eza: false
  bat: false
`
	if err := os.WriteFile(tc.ConfigPath, []byte(existingContent), 0o644); err != nil {
		t.Fatalf("Failed to create existing config: %v", err)
	}

	// Create existing zsh config at the correct location (App.Root/devgeta.zsh)
	// This must match where RegenerateShellConfig writes the file
	actualZshPath := getZshConfigPath()
	existingZsh := "# Custom zsh marker\nsource /custom/path\n"
	if err := os.WriteFile(actualZshPath, []byte(existingZsh), 0o644); err != nil {
		t.Fatalf("Failed to create existing zsh config: %v", err)
	}

	// Record modification time
	configInfo, err := os.Stat(tc.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to stat config: %v", err)
	}
	originalModTime := configInfo.ModTime()

	dg := &Devgeta{
		Base:            tc.MockApp.Base,
		ExtractEmbedded: mockExtractor,
	}

	err = dg.SoftConfigure()
	if err != nil {
		t.Fatalf("SoftConfigure() failed: %v", err)
	}

	// Verify config was not regenerated (modification time should be same)
	newConfigInfo, err := os.Stat(tc.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to stat config after soft configure: %v", err)
	}

	if !newConfigInfo.ModTime().Equal(originalModTime) {
		t.Error("Expected config file to be preserved (not regenerated)")
	}

	// Verify custom marker still exists
	content, err := os.ReadFile(tc.ConfigPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	if !strings.Contains(string(content), "Custom config marker") {
		t.Error("Expected custom config marker to be preserved")
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

// assertZshenvWiredOnce reads zshenvPath and asserts it contains the guarded,
// quoted source line for scriptPath exactly once. The guarded line references
// scriptPath twice ("[ -f "X" ] && source "X""), so one write produces 2
// occurrences of the path; a duplicate write would produce 4. Callers use
// this both right after wiring and again after a second, idempotent run.
func assertZshenvWiredOnce(t *testing.T, zshenvPath, scriptPath string) {
	t.Helper()

	content, err := os.ReadFile(zshenvPath)
	if err != nil {
		t.Fatalf("Expected ~/.zshenv to exist, got error reading it: %v", err)
	}

	expectedLine := fmt.Sprintf(`[ -f "%s" ] && source "%s"`, scriptPath, scriptPath)
	if !strings.Contains(string(content), expectedLine) {
		t.Errorf("Expected ~/.zshenv to contain %q, got: %q", expectedLine, string(content))
	}
	if occurrences := strings.Count(string(content), scriptPath); occurrences != 2 {
		t.Errorf(
			"Expected the guarded line to appear exactly once (2 occurrences of the script path), got %d in: %q",
			occurrences,
			string(content),
		)
	}
}

func TestForceConfigure_WiresZshenv_WhenShellIsZsh(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	zshenvPath := setupZshenvPaths(t, ".zshrc")

	// Real BaseCommand: MaybeSetupInFile only touches files, so this is safe
	// against the sandboxed paths above, and it's the only way to see the
	// actual line written (MockBaseCommand no-ops MaybeSetupInFile).
	dg := &Devgeta{
		Base:            commands.NewBaseCommandCustom(fakePlatform{}),
		ExtractEmbedded: mockExtractor,
	}

	if err := dg.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}
	assertZshenvWiredOnce(t, zshenvPath, getZshenvScriptPath())

	// Running ForceConfigure again must not duplicate the line.
	if err := dg.ForceConfigure(); err != nil {
		t.Fatalf("Second ForceConfigure() failed: %v", err)
	}
	assertZshenvWiredOnce(t, zshenvPath, getZshenvScriptPath())
}

func TestForceConfigure_ReturnsWrappedError_WhenZshenvWiringFails(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	// ShellConfig must look like .zshrc so the gate in setupZshenv is passed
	// and dg.Base.MaybeSetupInFile actually gets called.
	setupZshenvPaths(t, ".zshrc")

	tc.MockApp.Base.MaybeSetupInFileError = fmt.Errorf("injected zshenv wiring failure")
	dg := &Devgeta{
		Base:            tc.MockApp.Base,
		ExtractEmbedded: mockExtractor,
	}

	err := dg.ForceConfigure()
	if err == nil {
		t.Fatal("Expected ForceConfigure() to return an error when zshenv wiring fails, got nil")
	}
	if !strings.Contains(err.Error(), "injected zshenv wiring failure") {
		t.Errorf("Expected error to wrap the underlying MaybeSetupInFile error, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestForceConfigure_NoZshenv_WhenShellIsNotZsh(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	zshenvPath := setupZshenvPaths(t, ".bashrc")

	dg := &Devgeta{
		Base:            commands.NewBaseCommandCustom(fakePlatform{}),
		ExtractEmbedded: mockExtractor,
	}

	if err := dg.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure() failed: %v", err)
	}

	if _, err := os.Stat(zshenvPath); !os.IsNotExist(err) {
		t.Errorf("Expected no ~/.zshenv to be created for a bash shell config, but it exists")
	}
}

func TestSoftConfigure_WiresZshenv_OnExistingInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	zshenvPath := setupZshenvPaths(t, ".zshrc")

	// Existing install with extended_capabilities already enabled, so
	// SoftConfigure takes its early-return path instead of ForceConfigure.
	existingContent := `app_path: /custom/path
config_path: ""
shell:
  mise: false
  zoxide: false
  zsh_autosuggestions: false
  zsh_syntax_highlighting: false
  powerlevel10k: false
  extended_capabilities: true
  lazy_git: false
  lazy_docker: false
  fzf: false
  neovim: false
  tmux: false
  eza: false
  bat: false
`
	if err := os.WriteFile(tc.ConfigPath, []byte(existingContent), 0o644); err != nil {
		t.Fatalf("Failed to create existing config: %v", err)
	}
	actualZshPath := getZshConfigPath()
	if err := os.WriteFile(actualZshPath, []byte("# existing\n"), 0o644); err != nil {
		t.Fatalf("Failed to create existing zsh config: %v", err)
	}

	dg := &Devgeta{
		Base:            commands.NewBaseCommandCustom(fakePlatform{}),
		ExtractEmbedded: mockExtractor,
	}

	if err := dg.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure() failed: %v", err)
	}
	assertZshenvWiredOnce(t, zshenvPath, getZshenvScriptPath())

	// Running SoftConfigure again must not duplicate the line.
	if err := dg.SoftConfigure(); err != nil {
		t.Fatalf("Second SoftConfigure() failed: %v", err)
	}
	assertZshenvWiredOnce(t, zshenvPath, getZshenvScriptPath())
}

func TestSoftConfigure_ReturnsWrappedError_WhenZshenvWiringFails(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	// ShellConfig must look like .zshrc so the gate in setupZshenv is passed
	// and dg.Base.MaybeSetupInFile actually gets called. setupZshenv runs
	// before SoftConfigure's early-return checks, so this fails immediately
	// regardless of whether the global config / zsh config already exist.
	setupZshenvPaths(t, ".zshrc")

	tc.MockApp.Base.MaybeSetupInFileError = fmt.Errorf("injected zshenv wiring failure")
	dg := &Devgeta{
		Base:            tc.MockApp.Base,
		ExtractEmbedded: mockExtractor,
	}

	err := dg.SoftConfigure()
	if err == nil {
		t.Fatal("Expected SoftConfigure() to return an error when zshenv wiring fails, got nil")
	}
	if !strings.Contains(err.Error(), "injected zshenv wiring failure") {
		t.Errorf("Expected error to wrap the underlying MaybeSetupInFile error, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}
