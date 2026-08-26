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

func newTestDesktop(
	crossPlatformEntries []namedInstaller,
	launcherName string,
) (*Desktop, *mockSoftInstaller) {
	launcherMock := &mockSoftInstaller{}
	return &Desktop{
		Base:                      *cmd.NewBaseCommand(),
		crossPlatformAppsOverride: crossPlatformEntries,
		launcherOverride:          &namedInstaller{name: launcherName, app: launcherMock},
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
