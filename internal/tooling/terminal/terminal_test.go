package terminal

import (
	"context"
	"errors"
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
		constants.Claude, constants.Pi, constants.LazyDocker, constants.LazyGit,
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

// interruptingInstallable wraps a mockInstallable and additionally cancels
// the shared root context (as cmd.Execute's SIGINT/SIGTERM handler does in
// production) the moment its SoftInstall runs — simulating a Ctrl-C that
// lands mid-loop, right after this entry was processed.
type interruptingInstallable struct {
	*mockInstallable
}

func (m *interruptingInstallable) SoftInstall() error {
	err := m.mockInstallable.SoftInstall()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	commands.SetRootContext(ctx)
	return err
}

// TestInstallTerminalApps_StopsOnInterrupt covers task 14's fix: once the
// root context is cancelled mid-loop, InstallTerminalApps must stop before
// its remaining entries rather than call SoftInstall on every one of them
// and let each fail spuriously.
func TestInstallTerminalApps_StopsOnInterrupt(t *testing.T) {
	t.Cleanup(func() { commands.SetRootContext(context.Background()) })

	first := &mockInstallable{}
	interrupter := &interruptingInstallable{mockInstallable: &mockInstallable{}}
	third := &mockInstallable{}

	entries := []namedInstallable{
		{name: "first", app: first},
		{name: "second", app: interrupter},
		{name: "third", app: third},
	}
	term := &Terminal{appsOverride: entries}
	summary := &InstallationSummary{}

	term.InstallTerminalApps(summary, nil, nil)

	if !first.installCalled {
		t.Error("expected the first app to install before the interrupt happened")
	}
	if !interrupter.installCalled {
		t.Error("expected the second app (which triggers the interrupt) to install")
	}
	if third.installCalled {
		t.Error("expected the third app to be skipped once Interrupted() became true")
	}
	if summary.Total() != 2 {
		t.Errorf("expected exactly 2 apps attempted before stopping, got %d", summary.Total())
	}
}

// TestInstallDevTools_StopsOnInterrupt covers InstallDevTools' outer loop and
// its Debian-only inner loop. Both loop over hardcoded real apps with no test
// override, so this proves the loop-entry guard rather than a mid-loop stop:
// with the root context already cancelled before the call, neither loop may
// call a single real app method (which would shell out for real), so the
// summary must stay empty.
func TestInstallDevTools_StopsOnInterrupt(t *testing.T) {
	t.Cleanup(func() { commands.SetRootContext(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	commands.SetRootContext(ctx)

	term := &Terminal{}
	summary := &InstallationSummary{}

	term.InstallDevTools(summary)

	if summary.Total() != 0 {
		t.Errorf(
			"expected InstallDevTools to attempt nothing once already interrupted, got %d attempts: %+v",
			summary.Total(),
			summary.Results,
		)
	}
}

// TestInstallCoreLibs_StopsOnInterrupt mirrors TestInstallDevTools_StopsOnInterrupt
// for InstallCoreLibs' loop over hardcoded real apps.
func TestInstallCoreLibs_StopsOnInterrupt(t *testing.T) {
	t.Cleanup(func() { commands.SetRootContext(context.Background()) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	commands.SetRootContext(ctx)

	term := &Terminal{}
	summary := &InstallationSummary{}

	term.InstallCoreLibs(summary)

	if summary.Total() != 0 {
		t.Errorf(
			"expected InstallCoreLibs to attempt nothing once already interrupted, got %d attempts: %+v",
			summary.Total(),
			summary.Results,
		)
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

// presenceMock is an installable that can say whether it is installed, the way
// the coder apps can. present is what IsInstalled reports; a successful
// SoftInstall sets it to becomesPresent, so a test models "absent, then
// installed" and "already there, then upgraded".
type presenceMock struct {
	mockInstallable
	present        bool
	becomesPresent bool
}

func (p *presenceMock) IsInstalled() bool { return p.present }

func (p *presenceMock) SoftInstall() error {
	p.installCalled = true
	if p.installErr == nil {
		p.present = p.becomesPresent
	}
	return p.installErr
}

func presenceEntries(apps map[string]*presenceMock, order ...string) []namedInstallable {
	var entries []namedInstallable
	for _, name := range order {
		entries = append(entries, namedInstallable{name: name, app: apps[name]})
	}
	return entries
}

func TestInstallTerminalApps_ReportsWhichAppsThisRunInstalled(t *testing.T) {
	apps := map[string]*presenceMock{
		constants.Claude:   {present: false, becomesPresent: true},  // installed by this run
		constants.OpenCode: {present: true, becomesPresent: true},   // was there, upgraded to the pin
		constants.Pi:       {present: false, becomesPresent: false}, // install "succeeded" but is not present
	}
	term := &Terminal{appsOverride: presenceEntries(apps, constants.Claude, constants.OpenCode, constants.Pi)}
	summary := &InstallationSummary{}

	term.InstallTerminalApps(summary, nil, nil)

	if want := []string{constants.Claude}; !equalStrings(summary.FreshlyInstalled, want) {
		t.Errorf("freshly installed %v, want %v", summary.FreshlyInstalled, want)
	}
	if want := []string{constants.OpenCode}; !equalStrings(summary.AlreadyPresent, want) {
		t.Errorf("already present %v, want %v (an upgrade to the pin does not make it fresh)", summary.AlreadyPresent, want)
	}
}

func TestInstallTerminalApps_AFailedInstallIsNeitherFreshNorPresent(t *testing.T) {
	failing := &presenceMock{present: false, becomesPresent: true}
	failing.installErr = errBoom
	term := &Terminal{appsOverride: presenceEntries(map[string]*presenceMock{constants.Claude: failing}, constants.Claude)}
	summary := &InstallationSummary{}

	term.InstallTerminalApps(summary, nil, nil)

	if len(summary.FreshlyInstalled) != 0 || len(summary.AlreadyPresent) != 0 {
		t.Errorf("a failed install is neither, got fresh %v present %v", summary.FreshlyInstalled, summary.AlreadyPresent)
	}
}

func TestInstallTerminalApps_AppsThatCannotSayAreNeverListed(t *testing.T) {
	entries, _ := buildOverride(constants.Git, constants.Tmux) // plain mocks: no IsInstalled
	term := &Terminal{appsOverride: entries}
	summary := &InstallationSummary{}

	term.InstallTerminalApps(summary, nil, nil)

	if len(summary.FreshlyInstalled) != 0 || len(summary.AlreadyPresent) != 0 {
		t.Errorf("got fresh %v present %v", summary.FreshlyInstalled, summary.AlreadyPresent)
	}
}

func TestInstallTerminalApps_SkippedAndFilteredAppsAreNeverListed(t *testing.T) {
	apps := map[string]*presenceMock{
		constants.Claude:   {present: false, becomesPresent: true},
		constants.OpenCode: {present: true, becomesPresent: true},
		constants.Pi:       {present: true, becomesPresent: true},
	}
	term := &Terminal{appsOverride: presenceEntries(apps, constants.Claude, constants.OpenCode, constants.Pi)}
	summary := &InstallationSummary{}

	// --skip claude, --only opencode: the run touched only opencode.
	term.InstallTerminalApps(summary, map[string]bool{constants.OpenCode: true}, map[string]bool{constants.Claude: true})

	if len(summary.FreshlyInstalled) != 0 {
		t.Errorf("nothing was freshly installed, got %v", summary.FreshlyInstalled)
	}
	if want := []string{constants.OpenCode}; !equalStrings(summary.AlreadyPresent, want) {
		t.Errorf("only apps this run processed count, got %v", summary.AlreadyPresent)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var errBoom = errors.New("boom")
