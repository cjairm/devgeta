package cmd

import (
	"fmt"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/testutil"
)

func init() { testutil.InitLogger() }

// mockConfigureApp is a minimal apps.App that records which configure method was called.
type mockConfigureApp struct {
	forceCalled bool
	softCalled  bool
	forceErr    error
	softErr     error
}

func (m *mockConfigureApp) Name() string                   { return "mock" }
func (m *mockConfigureApp) Kind() apps.AppKind             { return apps.KindTerminal }
func (m *mockConfigureApp) Install() error                 { return nil }
func (m *mockConfigureApp) ForceInstall() error            { return nil }
func (m *mockConfigureApp) SoftInstall() error             { return nil }
func (m *mockConfigureApp) Uninstall() error               { return nil }
func (m *mockConfigureApp) Update() error                  { return nil }
func (m *mockConfigureApp) ExecuteCommand(...string) error { return nil }

func (m *mockConfigureApp) ForceConfigure() error {
	m.forceCalled = true
	return m.forceErr
}

func (m *mockConfigureApp) SoftConfigure() error {
	m.softCalled = true
	return m.softErr
}

func setupConfigureCmd(t *testing.T, mock apps.App) func() {
	t.Helper()
	origApp := getAppFn
	origRefresh := refreshEmbeddedConfigs
	getAppFn = func(name string) (apps.App, error) { return mock, nil }
	refreshEmbeddedConfigs = func() error { return nil }
	return func() {
		getAppFn = origApp
		refreshEmbeddedConfigs = origRefresh
	}
}

func TestConfigure_SoftPath(t *testing.T) {
	mock := &mockConfigureApp{}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = false
	err := runConfigure(configureCmd, []string{"git"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !mock.softCalled {
		t.Error("expected SoftConfigure to be called")
	}
	if mock.forceCalled {
		t.Error("expected ForceConfigure NOT to be called")
	}
}

func TestConfigure_ForcePath(t *testing.T) {
	mock := &mockConfigureApp{}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = true
	defer func() { configureForce = false }()

	err := runConfigure(configureCmd, []string{"git"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !mock.forceCalled {
		t.Error("expected ForceConfigure to be called")
	}
	if mock.softCalled {
		t.Error("expected SoftConfigure NOT to be called")
	}
}

func TestConfigure_UnknownApp(t *testing.T) {
	origApp := getAppFn
	origRefresh := refreshEmbeddedConfigs
	getAppFn = func(name string) (apps.App, error) {
		return nil, fmt.Errorf("unknown app %q", name)
	}
	refreshEmbeddedConfigs = func() error { return nil }
	defer func() {
		getAppFn = origApp
		refreshEmbeddedConfigs = origRefresh
	}()

	configureForce = false
	err := runConfigure(configureCmd, []string{"notanapp"})
	if err == nil {
		t.Fatal("expected non-nil error for unknown app")
	}
}

// mockSelectiveApp also implements apps.SelectiveConfigurer.
type mockSelectiveApp struct {
	mockConfigureApp
	partsCalled bool
	parts       []string
	partsErr    error
}

func (m *mockSelectiveApp) ConfigurableParts() []string {
	return []string{"skills", "commands", "agents"}
}

func (m *mockSelectiveApp) ForceConfigureParts(parts []string) error {
	m.partsCalled = true
	m.parts = parts
	return m.partsErr
}

func TestConfigure_OnlyDispatchesToParts(t *testing.T) {
	mock := &mockSelectiveApp{}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = true
	configureOnly = []string{"skills"}
	defer func() { configureForce = false; configureOnly = nil }()

	if err := runConfigure(configureCmd, []string{"claude"}); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !mock.partsCalled {
		t.Fatal("expected ForceConfigureParts to be called")
	}
	if len(mock.parts) != 1 || mock.parts[0] != "skills" {
		t.Fatalf("expected parts [skills], got %v", mock.parts)
	}
	if mock.forceCalled || mock.softCalled {
		t.Error("expected neither ForceConfigure nor SoftConfigure with --only")
	}
}

func TestConfigure_OnlyRequiresForce(t *testing.T) {
	mock := &mockSelectiveApp{}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = false
	configureOnly = []string{"skills"}
	defer func() { configureOnly = nil }()

	err := runConfigure(configureCmd, []string{"claude"})
	if err == nil {
		t.Fatal("expected error: --only requires --force")
	}
	if mock.partsCalled {
		t.Error("expected no work when --only is used without --force")
	}
}

func TestConfigure_OnlyUnknownPart(t *testing.T) {
	mock := &mockSelectiveApp{}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = true
	configureOnly = []string{"bogus"}
	defer func() { configureForce = false; configureOnly = nil }()

	err := runConfigure(configureCmd, []string{"claude"})
	if err == nil {
		t.Fatal("expected error for unknown --only value")
	}
	if mock.partsCalled {
		t.Error("expected no work for an invalid part")
	}
}

func TestConfigure_OnlyUnsupportedApp(t *testing.T) {
	// mockConfigureApp does NOT implement SelectiveConfigurer.
	mock := &mockConfigureApp{}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = true
	configureOnly = []string{"skills"}
	defer func() { configureForce = false; configureOnly = nil }()

	err := runConfigure(configureCmd, []string{"git"})
	if err == nil {
		t.Fatal("expected error: --only not supported for this app")
	}
	if mock.forceCalled || mock.softCalled {
		t.Error("expected no configure call when --only is unsupported")
	}
}

func TestConfigure_NotSupported(t *testing.T) {
	mock := &mockConfigureApp{softErr: fmt.Errorf("%w for mock", apps.ErrConfigureNotSupported)}
	restore := setupConfigureCmd(t, mock)
	defer restore()

	configureForce = false
	err := runConfigure(configureCmd, []string{"brave"})
	if err != nil {
		t.Fatalf("ErrConfigureNotSupported should exit zero, got: %v", err)
	}
}
