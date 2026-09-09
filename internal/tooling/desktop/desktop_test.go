package desktop

import (
	"context"
	"testing"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
)

func init() { testutil.InitLogger() }

// mockSoftInstaller records SoftInstall calls.
type mockSoftInstaller struct {
	installCalled bool
	installErr    error
}

func (m *mockSoftInstaller) SoftInstall() error {
	m.installCalled = true
	return m.installErr
}

// buildCrossPlatformOverride creates a namedInstaller list backed by mocks and returns the mock map.
func buildCrossPlatformOverride(names ...string) ([]namedInstaller, map[string]*mockSoftInstaller) {
	mocks := make(map[string]*mockSoftInstaller, len(names))
	entries := make([]namedInstaller, len(names))
	for i, name := range names {
		m := &mockSoftInstaller{}
		mocks[name] = m
		entries[i] = namedInstaller{name: name, app: m}
	}
	return entries, mocks
}

// mockTerminalInstaller records SoftInstall and SoftConfigure calls.
type mockTerminalInstaller struct {
	installCalled   bool
	installErr      error
	configureCalled bool
	configureErr    error
}

func (m *mockTerminalInstaller) SoftInstall() error {
	m.installCalled = true
	return m.installErr
}

func (m *mockTerminalInstaller) SoftConfigure() error {
	m.configureCalled = true
	return m.configureErr
}

func newTestDesktop(
	crossPlatformEntries []namedInstaller,
	launcherName string,
) (*Desktop, *mockSoftInstaller) {
	launcherMock := &mockSoftInstaller{}
	return &Desktop{
		Base:                      *cmd.NewBaseCommand(),
		crossPlatformAppsOverride: crossPlatformEntries,
		launcherOverride:          &namedInstaller{name: launcherName, app: launcherMock},
		// Empty, non-nil: bypasses the real flameshot/shottr chooser default,
		// which would otherwise construct real apps and reach a real command
		// executor. Tests that care about the screenshot chooser itself set
		// their own screenshotCandidatesOverride.
		screenshotCandidatesOverride: []screenshotCandidateEntry{},
	}, launcherMock
}

func TestInstallDesktopAppsWithoutConfiguration_NoFilter(t *testing.T) {
	allApps := []string{constants.Docker, constants.Gimp, constants.Brave, constants.Flameshot}
	entries, mocks := buildCrossPlatformOverride(allApps...)
	d, _ := newTestDesktop(entries, constants.Raycast)

	d.InstallDesktopAppsWithoutConfiguration(nil, nil)

	for _, name := range allApps {
		if !mocks[name].installCalled {
			t.Errorf("expected %s to be installed with no filter", name)
		}
	}
}

func TestInstallDesktopAppsWithoutConfiguration_WithFilter(t *testing.T) {
	allApps := []string{constants.Docker, constants.Gimp, constants.Brave, constants.Flameshot}
	entries, mocks := buildCrossPlatformOverride(allApps...)
	d, _ := newTestDesktop(entries, constants.Raycast)

	d.InstallDesktopAppsWithoutConfiguration(map[string]bool{constants.Docker: true}, nil)

	if !mocks[constants.Docker].installCalled {
		t.Error("expected docker to be installed with filter")
	}
	for _, name := range []string{constants.Gimp, constants.Brave, constants.Flameshot} {
		if mocks[name].installCalled {
			t.Errorf("expected %s NOT to be installed when filter excludes it", name)
		}
	}
}

func TestInstallDesktopAppsWithoutConfiguration_SkipFilter(t *testing.T) {
	allApps := []string{constants.Docker, constants.Gimp, constants.Brave}
	entries, mocks := buildCrossPlatformOverride(allApps...)
	d, _ := newTestDesktop(entries, constants.Raycast)

	d.InstallDesktopAppsWithoutConfiguration(nil, map[string]bool{constants.Gimp: true})

	if mocks[constants.Gimp].installCalled {
		t.Error("expected gimp to be skipped by skipFilter")
	}
	for _, name := range []string{constants.Docker, constants.Brave} {
		if !mocks[name].installCalled {
			t.Errorf("expected %s to be installed (not in skipFilter)", name)
		}
	}
}

func TestInstallDesktopApps_LauncherSkippedByFilter(t *testing.T) {
	entries, _ := buildCrossPlatformOverride(constants.Docker)
	d, launcherMock := newTestDesktop(entries, constants.Raycast)

	// Filter only includes docker, not raycast
	d.InstallDesktopAppsWithoutConfiguration(map[string]bool{constants.Docker: true}, nil)

	if launcherMock.installCalled {
		t.Error("expected launcher (raycast) NOT to be installed when filter excludes it")
	}
}

// interruptingSoftInstaller wraps a mockSoftInstaller and additionally
// cancels the shared root context (as cmd.Execute's SIGINT/SIGTERM handler
// does in production) the moment its SoftInstall runs — simulating a Ctrl-C
// landing mid-loop, right after this entry was processed.
type interruptingSoftInstaller struct {
	*mockSoftInstaller
}

func (m *interruptingSoftInstaller) SoftInstall() error {
	err := m.mockSoftInstaller.SoftInstall()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd.SetRootContext(ctx)
	return err
}

// TestInstallDesktopAppsWithoutConfiguration_StopsOnInterrupt covers task
// 14's fix: once the root context is cancelled mid-loop, the cross-platform
// apps loop must stop before its remaining entries instead of calling
// SoftInstall on every one of them.
func TestInstallDesktopAppsWithoutConfiguration_StopsOnInterrupt(t *testing.T) {
	t.Cleanup(func() { cmd.SetRootContext(context.Background()) })

	first := &mockSoftInstaller{}
	interrupter := &interruptingSoftInstaller{mockSoftInstaller: &mockSoftInstaller{}}
	third := &mockSoftInstaller{}

	entries := []namedInstaller{
		{name: "first", app: first},
		{name: "second", app: interrupter},
		{name: "third", app: third},
	}
	d, _ := newTestDesktop(entries, constants.Raycast)

	d.InstallDesktopAppsWithoutConfiguration(nil, nil)

	if !first.installCalled {
		t.Error("expected the first app to install before the interrupt happened")
	}
	if !interrupter.installCalled {
		t.Error("expected the second app (which triggers the interrupt) to install")
	}
	if third.installCalled {
		t.Error("expected the third app to be skipped once Interrupted() became true")
	}
}

func TestShouldInstallApp(t *testing.T) {
	cases := []struct {
		name       string
		appName    string
		appFilter  map[string]bool
		skipFilter map[string]bool
		want       bool
	}{
		{"no filters", "docker", nil, nil, true},
		{"in appFilter", "docker", map[string]bool{"docker": true}, nil, true},
		{"not in appFilter", "gimp", map[string]bool{"docker": true}, nil, false},
		{"in skipFilter", "docker", nil, map[string]bool{"docker": true}, false},
		{
			"in appFilter but also skipped",
			"docker",
			map[string]bool{"docker": true},
			map[string]bool{"docker": true},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldInstallApp(tc.appName, tc.appFilter, tc.skipFilter)
			if got != tc.want {
				t.Errorf("shouldInstallApp(%q, appFilter=%v, skipFilter=%v) = %v, want %v",
					tc.appName, tc.appFilter, tc.skipFilter, got, tc.want)
			}
		})
	}
}

// buildTerminalCandidatesOverride builds a terminalCandidateEntry list backed
// by mocks, one per name, with the given availability. Returns the entries
// and the mock map so tests can assert on SoftInstall/SoftConfigure calls.
func buildTerminalCandidatesOverride(
	availability map[string]bool,
) ([]terminalCandidateEntry, map[string]*mockTerminalInstaller) {
	mocks := make(map[string]*mockTerminalInstaller, len(availability))
	entries := make([]terminalCandidateEntry, 0, len(availability))
	for name, available := range availability {
		m := &mockTerminalInstaller{}
		mocks[name] = m
		entries = append(entries, terminalCandidateEntry{
			name:      name,
			app:       m,
			available: func() bool { return available },
		})
	}
	return entries, mocks
}

func TestChooseTerminal_NoFilter_OneAvailable(t *testing.T) {
	entries, mocks := buildTerminalCandidatesOverride(map[string]bool{
		constants.Alacritty: false,
		constants.Ghostty:   true,
	})
	d := &Desktop{terminalCandidatesOverride: entries}

	app, name := d.chooseTerminal(nil, nil)

	if name != constants.Ghostty {
		t.Errorf("expected chosen name %q, got %q", constants.Ghostty, name)
	}
	if app != mocks[constants.Ghostty] {
		t.Error("expected the returned app to be ghostty's mock")
	}
}

func TestChooseTerminal_NoFilter_NonePrompts(t *testing.T) {
	entries, mocks := buildTerminalCandidatesOverride(map[string]bool{
		constants.Alacritty: true,
		constants.Ghostty:   true,
	})
	d := &Desktop{
		terminalCandidatesOverride: entries,
		selectOverride: func(label string, options []string) (string, error) {
			return constants.Alacritty, nil
		},
	}

	app, name := d.chooseTerminal(nil, nil)

	if name != constants.Alacritty {
		t.Errorf("expected chosen name %q, got %q", constants.Alacritty, name)
	}
	if app != mocks[constants.Alacritty] {
		t.Error("expected the returned app to be alacritty's mock")
	}
}

func TestChooseTerminal_NoFilter_NoneAvailable(t *testing.T) {
	entries, _ := buildTerminalCandidatesOverride(map[string]bool{
		constants.Alacritty: false,
		constants.Ghostty:   false,
	})
	d := &Desktop{terminalCandidatesOverride: entries}

	app, name := d.chooseTerminal(nil, nil)

	if app != nil || name != "" {
		t.Errorf("expected (nil, \"\") when nothing is available, got (%v, %q)", app, name)
	}
}

// TestChooseTerminal_OnlyFilterBypassesPromptEvenIfUnavailable covers manual
// verification #2 from the cycle plan: --only alacritty on macOS must narrow
// the candidate pool to just alacritty BEFORE the availability filter runs,
// so an unavailable app named explicitly is reported as "nothing available"
// (skipped, not an install failure) rather than falling through to ghostty.
func TestChooseTerminal_OnlyFilterBypassesPromptEvenIfUnavailable(t *testing.T) {
	entries, mocks := buildTerminalCandidatesOverride(map[string]bool{
		constants.Alacritty: false,
		constants.Ghostty:   true,
	})
	d := &Desktop{
		terminalCandidatesOverride: entries,
		selectOverride: func(label string, options []string) (string, error) {
			t.Fatal("selectFn must not be called: appFilter narrows the pool to one name")
			return "", nil
		},
	}

	app, name := d.chooseTerminal(map[string]bool{constants.Alacritty: true}, nil)

	if app != nil || name != "" {
		t.Errorf("expected (nil, \"\"), got (%v, %q)", app, name)
	}
	if mocks[constants.Ghostty].installCalled {
		t.Error("expected ghostty not to be touched when --only alacritty was requested")
	}
}

func TestChooseTerminal_OnlyFilterSelectsAvailableApp(t *testing.T) {
	entries, mocks := buildTerminalCandidatesOverride(map[string]bool{
		constants.Alacritty: true,
		constants.Ghostty:   true,
	})
	d := &Desktop{
		terminalCandidatesOverride: entries,
		selectOverride: func(label string, options []string) (string, error) {
			t.Fatal("selectFn must not be called: appFilter narrows the pool to one name")
			return "", nil
		},
	}

	app, name := d.chooseTerminal(map[string]bool{constants.Ghostty: true}, nil)

	if name != constants.Ghostty || app != mocks[constants.Ghostty] {
		t.Errorf("expected (ghostty's mock, %q), got (%v, %q)", constants.Ghostty, app, name)
	}
}

func TestChooseTerminal_SkipFilterExcludesApp(t *testing.T) {
	entries, mocks := buildTerminalCandidatesOverride(map[string]bool{
		constants.Alacritty: true,
		constants.Ghostty:   true,
	})
	d := &Desktop{
		terminalCandidatesOverride: entries,
		selectOverride: func(label string, options []string) (string, error) {
			t.Fatal("selectFn must not be called: skipFilter narrows the pool to one name")
			return "", nil
		},
	}

	app, name := d.chooseTerminal(nil, map[string]bool{constants.Alacritty: true})

	if name != constants.Ghostty || app != mocks[constants.Ghostty] {
		t.Errorf("expected (ghostty's mock, %q), got (%v, %q)", constants.Ghostty, app, name)
	}
}

// buildScreenshotCandidatesOverride builds a screenshotCandidateEntry list
// backed by mocks, one per name, with the given availability.
func buildScreenshotCandidatesOverride(
	availability map[string]bool,
) ([]screenshotCandidateEntry, map[string]*mockSoftInstaller) {
	mocks := make(map[string]*mockSoftInstaller, len(availability))
	entries := make([]screenshotCandidateEntry, 0, len(availability))
	for name, available := range availability {
		m := &mockSoftInstaller{}
		mocks[name] = m
		entries = append(entries, screenshotCandidateEntry{
			name:      name,
			app:       m,
			available: func() bool { return available },
		})
	}
	return entries, mocks
}

func TestChooseScreenshot_NoFilter_OneAvailable(t *testing.T) {
	entries, mocks := buildScreenshotCandidatesOverride(map[string]bool{
		constants.Flameshot: false,
		constants.Shottr:    true,
	})
	d := &Desktop{screenshotCandidatesOverride: entries}

	app, name := d.chooseScreenshot(nil, nil)

	if name != constants.Shottr {
		t.Errorf("expected chosen name %q, got %q", constants.Shottr, name)
	}
	if app != mocks[constants.Shottr] {
		t.Error("expected the returned app to be shottr's mock")
	}
}

func TestChooseScreenshot_NoFilter_NonePrompts(t *testing.T) {
	entries, mocks := buildScreenshotCandidatesOverride(map[string]bool{
		constants.Flameshot: true,
		constants.Shottr:    true,
	})
	d := &Desktop{
		screenshotCandidatesOverride: entries,
		selectOverride: func(label string, options []string) (string, error) {
			return constants.Flameshot, nil
		},
	}

	app, name := d.chooseScreenshot(nil, nil)

	if name != constants.Flameshot {
		t.Errorf("expected chosen name %q, got %q", constants.Flameshot, name)
	}
	if app != mocks[constants.Flameshot] {
		t.Error("expected the returned app to be flameshot's mock")
	}
}

func TestChooseScreenshot_NoFilter_NoneAvailable(t *testing.T) {
	entries, _ := buildScreenshotCandidatesOverride(map[string]bool{
		constants.Flameshot: false,
		constants.Shottr:    false,
	})
	d := &Desktop{screenshotCandidatesOverride: entries}

	app, name := d.chooseScreenshot(nil, nil)

	if app != nil || name != "" {
		t.Errorf("expected (nil, \"\") when nothing is available, got (%v, %q)", app, name)
	}
}

// TestChooseScreenshot_OnlyFilterBypassesPromptEvenIfUnavailable mirrors the
// terminal group's manual verification #2: --only flameshot on macOS must
// narrow the pool to just flameshot before availability is checked, so an
// unavailable app named explicitly is skipped rather than falling through to
// shottr.
func TestChooseScreenshot_OnlyFilterBypassesPromptEvenIfUnavailable(t *testing.T) {
	entries, mocks := buildScreenshotCandidatesOverride(map[string]bool{
		constants.Flameshot: false,
		constants.Shottr:    true,
	})
	d := &Desktop{
		screenshotCandidatesOverride: entries,
		selectOverride: func(label string, options []string) (string, error) {
			t.Fatal("selectFn must not be called: appFilter narrows the pool to one name")
			return "", nil
		},
	}

	app, name := d.chooseScreenshot(map[string]bool{constants.Flameshot: true}, nil)

	if app != nil || name != "" {
		t.Errorf("expected (nil, \"\"), got (%v, %q)", app, name)
	}
	if mocks[constants.Shottr].installCalled {
		t.Error("expected shottr not to be touched when --only flameshot was requested")
	}
}

// TestInstallDesktopAppsWithoutConfiguration_ScreenshotChooser confirms the
// screenshot chooser is wired into the main desktop-apps loop: with a single
// available candidate, it installs silently alongside the cross-platform
// apps and the launcher, without touching the unavailable one.
func TestInstallDesktopAppsWithoutConfiguration_ScreenshotChooser(t *testing.T) {
	crossEntries, crossMocks := buildCrossPlatformOverride(constants.Docker)
	screenshotEntries, screenshotMocks := buildScreenshotCandidatesOverride(map[string]bool{
		constants.Flameshot: false,
		constants.Shottr:    true,
	})
	launcherMock := &mockSoftInstaller{}
	d := &Desktop{
		Base:                         *cmd.NewBaseCommand(),
		crossPlatformAppsOverride:    crossEntries,
		screenshotCandidatesOverride: screenshotEntries,
		launcherOverride:             &namedInstaller{name: constants.Raycast, app: launcherMock},
	}

	d.InstallDesktopAppsWithoutConfiguration(nil, nil)

	if !crossMocks[constants.Docker].installCalled {
		t.Error("expected docker to be installed")
	}
	if !screenshotMocks[constants.Shottr].installCalled {
		t.Error("expected shottr (the only available candidate) to be installed")
	}
	if screenshotMocks[constants.Flameshot].installCalled {
		t.Error("expected flameshot (unavailable) not to be installed")
	}
	if !launcherMock.installCalled {
		t.Error("expected the launcher to be installed")
	}
}
