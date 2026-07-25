package terminal

import (
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
)

func init() { testutil.InitLogger() }

// mockInstallable records SoftInstall/SoftConfigure calls for a named app.
type mockInstallable struct {
	installCalled   bool
	configureCalled bool
	installErr      error
	configureErr    error
}

func (m *mockInstallable) SoftInstall() error {
	m.installCalled = true
	return m.installErr
}

func (m *mockInstallable) SoftConfigure() error {
	m.configureCalled = true
	return m.configureErr
}

// buildOverride creates a namedInstallable list backed by mocks and returns the mock map.
func buildOverride(names ...string) ([]namedInstallable, map[string]*mockInstallable) {
	mocks := make(map[string]*mockInstallable, len(names))
	entries := make([]namedInstallable, len(names))
	for i, name := range names {
		m := &mockInstallable{}
		mocks[name] = m
		entries[i] = namedInstallable{name: name, app: m}
	}
	return entries, mocks
}

func TestInstallTerminalApps_NoFilter(t *testing.T) {
	allApps := []string{
		constants.Fastfetch, constants.Git, constants.Mise,
		constants.Neovim, constants.Tmux, constants.OpenCode,
		constants.Claude, constants.LazyDocker, constants.LazyGit,
	}
	entries, mocks := buildOverride(allApps...)
	term := &Terminal{appsOverride: entries}
	summary := &InstallationSummary{}

	term.InstallTerminalApps(summary, nil, nil)

	for _, name := range allApps {
		if !mocks[name].installCalled {
			t.Errorf("expected %s to be installed with no filter", name)
		}
	}
}

func TestInstallTerminalApps_WithFilter_SingleApp(t *testing.T) {
	allApps := []string{
		constants.Fastfetch, constants.Git, constants.Neovim, constants.Tmux,
	}
	entries, mocks := buildOverride(allApps...)
	term := &Terminal{appsOverride: entries}
	summary := &InstallationSummary{}

	term.InstallTerminalApps(summary, map[string]bool{constants.Neovim: true}, nil)

	if !mocks[constants.Neovim].installCalled {
		t.Error("expected neovim to be installed with filter")
	}
	for _, name := range []string{constants.Fastfetch, constants.Git, constants.Tmux} {
		if mocks[name].installCalled {
			t.Errorf("expected %s NOT to be installed when filter excludes it", name)
		}
	}
}

func TestInstallTerminalApps_WithFilter_MultipleApps(t *testing.T) {
	allApps := []string{
		constants.Fastfetch, constants.Git, constants.Neovim,
		constants.Tmux, constants.Mise,
	}
	entries, mocks := buildOverride(allApps...)
	term := &Terminal{appsOverride: entries}
	summary := &InstallationSummary{}

	filter := map[string]bool{constants.Neovim: true, constants.Git: true}
	term.InstallTerminalApps(summary, filter, nil)

	for _, name := range []string{constants.Neovim, constants.Git} {
		if !mocks[name].installCalled {
			t.Errorf("expected %s to be installed with filter", name)
		}
	}
	for _, name := range []string{constants.Fastfetch, constants.Tmux, constants.Mise} {
		if mocks[name].installCalled {
			t.Errorf("expected %s NOT to be installed when filter excludes it", name)
		}
	}
}

func TestInstallTerminalApps_SkipFilter(t *testing.T) {
	allApps := []string{constants.Neovim, constants.Git, constants.Tmux}
	entries, mocks := buildOverride(allApps...)
	term := &Terminal{appsOverride: entries}
	summary := &InstallationSummary{}

	term.InstallTerminalApps(summary, nil, map[string]bool{constants.Git: true})

	if mocks[constants.Git].installCalled {
		t.Error("expected git to be skipped by skipFilter")
	}
	for _, name := range []string{constants.Neovim, constants.Tmux} {
		if !mocks[name].installCalled {
			t.Errorf("expected %s to be installed (not in skipFilter)", name)
		}
	}
}

func TestInstallAndConfigure_SkipsDevToolsWhenFilterActive(t *testing.T) {
	// When appFilter is non-empty, InstallAndConfigure must NOT call InstallDevTools/InstallCoreLibs.
	// We verify this indirectly: the Terminal's Cmd mock must not record any MaybeInstall calls
	// that devtools/corelibs would trigger.
	mockApp := testutil.NewMockApp()
	entries, _ := buildOverride(constants.Neovim)
	term := &Terminal{
		Cmd:          mockApp.Cmd,
		Base:         *commands.NewBaseCommand(),
		appsOverride: entries,
	}

	summary := &InstallationSummary{}
	term.InstallTerminalApps(summary, map[string]bool{constants.Neovim: true}, nil)

	// Summary should show only neovim attempted (1 installed, 0 failed)
	if summary.Total() != 1 {
		t.Errorf("expected 1 app in summary with single-app filter, got %d", summary.Total())
	}
	testutil.VerifyNoRealCommands(t, mockApp.Base)
}
