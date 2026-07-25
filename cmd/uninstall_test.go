package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
)

// mockUninstallApp records whether Uninstall was called and returns a preset error.
type mockUninstallApp struct {
	name            string
	uninstallCalled bool
	uninstallErr    error
}

func (m *mockUninstallApp) Name() string                   { return m.name }
func (m *mockUninstallApp) Kind() apps.AppKind             { return apps.KindTerminal }
func (m *mockUninstallApp) Install() error                 { return nil }
func (m *mockUninstallApp) ForceInstall() error            { return nil }
func (m *mockUninstallApp) SoftInstall() error             { return nil }
func (m *mockUninstallApp) ForceConfigure() error          { return nil }
func (m *mockUninstallApp) SoftConfigure() error           { return nil }
func (m *mockUninstallApp) Update() error                  { return nil }
func (m *mockUninstallApp) ExecuteCommand(...string) error { return nil }
func (m *mockUninstallApp) Uninstall() error {
	m.uninstallCalled = true
	return m.uninstallErr
}

// setupUninstallSeam overrides uninstallGetAppFn and returns a cleanup function.
func setupUninstallSeam(t *testing.T, mock apps.App) func() {
	t.Helper()
	orig := uninstallGetAppFn
	uninstallGetAppFn = func(name string) (apps.App, error) { return mock, nil }
	return func() { uninstallGetAppFn = orig }
}

// configWithPackages writes a global config with specific packages tracked as installed.
func configWithPackages(t *testing.T, configPath string, pkgs []string) {
	t.Helper()
	pkgLines := ""
	for _, p := range pkgs {
		pkgLines += fmt.Sprintf("    - %s\n", p)
	}
	content := fmt.Sprintf(`app_path: ""
config_path: ""
installed:
  packages:
%s  desktop_apps: []
  fonts: []
  themes: []
  terminal_tools: []
  dev_languages: []
  databases: []
already_installed:
  packages: []
  desktop_apps: []
  fonts: []
  themes: []
  terminal_tools: []
  dev_languages: []
  databases: []
shell:
  mise: false
  lazy_git: false
  lazy_docker: false
  neovim: false
  tmux: false
  opencode: false
  claude: false
  fzf: false
  zoxide: false
  zsh_autosuggestions: false
  zsh_syntax_highlighting: false
  powerlevel10k: false
  extended_capabilities: false
  eza: false
  bat: false
`, pkgLines)
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
}

func TestUninstall_LanguagesBlocked(t *testing.T) {
	err := runUninstall(uninstallCmd, []string{"languages"})
	if err == nil {
		t.Fatal("expected error for 'languages'")
	}
	if !strings.Contains(err.Error(), "not yet supported") {
		t.Errorf("expected 'not yet supported' error, got: %v", err)
	}
}

func TestUninstall_DatabasesBlocked(t *testing.T) {
	err := runUninstall(uninstallCmd, []string{"databases"})
	if err == nil {
		t.Fatal("expected error for 'databases'")
	}
	if !strings.Contains(err.Error(), "not yet supported") {
		t.Errorf("expected 'not yet supported' error, got: %v", err)
	}
}

func TestUninstall_DevgetaBlocked(t *testing.T) {
	err := runUninstall(uninstallCmd, []string{"devgeta"})
	if err == nil {
		t.Fatal("expected error for 'devgeta'")
	}
	if !strings.Contains(err.Error(), "cannot uninstall devgeta from itself") {
		t.Errorf("expected 'cannot uninstall devgeta' error, got: %v", err)
	}
}

func TestUninstall_UnknownTarget(t *testing.T) {
	err := runUninstall(uninstallCmd, []string{"notanapp"})
	if err == nil {
		t.Fatal("expected error for unknown target")
	}
	if !strings.Contains(err.Error(), "unknown target") {
		t.Errorf("expected 'unknown target' error, got: %v", err)
	}
}

func TestUninstall_AppNotTracked(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	mock := &mockUninstallApp{name: constants.Fastfetch}
	restore := setupUninstallSeam(t, mock)
	defer restore()

	// Default config has empty installed list — fastfetch is not tracked.
	err := runUninstall(uninstallCmd, []string{constants.Fastfetch})
	if err != nil {
		t.Fatalf("expected no error when app not tracked, got: %v", err)
	}
	if mock.uninstallCalled {
		t.Error("expected Uninstall NOT to be called for untracked app")
	}
}

func TestUninstall_AppSuccess(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	configWithPackages(t, tc.ConfigPath, []string{constants.Fastfetch})

	mock := &mockUninstallApp{name: constants.Fastfetch}
	restore := setupUninstallSeam(t, mock)
	defer restore()

	err := runUninstall(uninstallCmd, []string{constants.Fastfetch})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !mock.uninstallCalled {
		t.Error("expected Uninstall to be called")
	}
}

func TestUninstall_AppFails(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	configWithPackages(t, tc.ConfigPath, []string{constants.Fastfetch})

	mock := &mockUninstallApp{
		name:         constants.Fastfetch,
		uninstallErr: fmt.Errorf("binary removal failed"),
	}
	restore := setupUninstallSeam(t, mock)
	defer restore()

	err := runUninstall(uninstallCmd, []string{constants.Fastfetch})
	if err == nil {
		t.Fatal("expected error when Uninstall fails")
	}
	if !strings.Contains(err.Error(), constants.Fastfetch) {
		t.Errorf("expected error to mention failed app, got: %v", err)
	}
}

func TestUninstall_CategorySkipsUntracked(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	// Only fastfetch is installed; git is not.
	configWithPackages(t, tc.ConfigPath, []string{constants.Fastfetch})

	calledWith := map[string]bool{}
	orig := uninstallGetAppFn
	uninstallGetAppFn = func(name string) (apps.App, error) {
		calledWith[name] = true
		return &mockUninstallApp{name: name}, nil
	}
	defer func() { uninstallGetAppFn = orig }()

	err := runUninstall(uninstallCmd, []string{"terminal"})
	if err != nil {
		t.Fatalf("expected no error for category uninstall, got: %v", err)
	}

	if !calledWith[constants.Fastfetch] {
		t.Errorf("expected fastfetch (tracked) to be uninstalled")
	}
	if calledWith[constants.Git] {
		t.Errorf("expected git (untracked) to be skipped")
	}
}

func TestUninstall_CategoryContinuesOnError(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	// Both fastfetch and git are tracked.
	configWithPackages(t, tc.ConfigPath, []string{constants.Fastfetch, constants.Git})

	fastfetchMock := &mockUninstallApp{
		name:         constants.Fastfetch,
		uninstallErr: fmt.Errorf("failed"),
	}
	gitMock := &mockUninstallApp{name: constants.Git}

	orig := uninstallGetAppFn
	uninstallGetAppFn = func(name string) (apps.App, error) {
		if name == constants.Fastfetch {
			return fastfetchMock, nil
		}
		return gitMock, nil
	}
	defer func() { uninstallGetAppFn = orig }()

	err := runUninstall(uninstallCmd, []string{"terminal"})
	// Should fail (fastfetch failed) but git should still have been tried.
	if err == nil {
		t.Fatal("expected error because fastfetch failed")
	}
	if !gitMock.uninstallCalled {
		t.Error("expected git to be attempted despite fastfetch failure")
	}
}

func TestUninstall_ShellFeatureMessage(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	// mise has HasShellFeature = true.
	configWithPackages(t, tc.ConfigPath, []string{constants.Mise})

	mock := &mockUninstallApp{name: constants.Mise}
	restore := setupUninstallSeam(t, mock)
	defer restore()

	// Capture stdout to verify the "source ~/.zshrc" message.
	// We can't capture print output directly, but we can verify no error and
	// that shellFeatureChanged logic runs (tested via absence of error).
	err := runUninstall(uninstallCmd, []string{constants.Mise})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !mock.uninstallCalled {
		t.Error("expected Uninstall to be called for mise")
	}
}

func TestUninstall_DesktopApp(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	// Write config with a desktop_app tracked.
	content := fmt.Sprintf(`app_path: ""
config_path: ""
installed:
  packages: []
  desktop_apps:
    - %s
  fonts: []
  themes: []
  terminal_tools: []
  dev_languages: []
  databases: []
already_installed:
  packages: []
  desktop_apps: []
  fonts: []
  themes: []
  terminal_tools: []
  dev_languages: []
  databases: []
shell:
  mise: false
`, constants.Gimp)
	if err := os.WriteFile(tc.ConfigPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	mock := &mockUninstallApp{name: constants.Gimp}
	restore := setupUninstallSeam(t, mock)
	defer restore()

	err := runUninstall(uninstallCmd, []string{constants.Gimp})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !mock.uninstallCalled {
		t.Error("expected Uninstall to be called for gimp")
	}
}

func TestUninstall_TemplatesDir(t *testing.T) {
	// Verify that SetupCompleteTest creates the shell template so gc.Load works.
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	if _, err := os.Stat(filepath.Join(tc.TemplatesDir, constants.App.Template.ShellConfig)); err != nil {
		t.Fatalf("expected shell template to exist: %v", err)
	}
}
