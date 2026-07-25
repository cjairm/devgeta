package aitools

import (
	"errors"
	"testing"

	"github.com/cjairm/devgeta/internal/testutil"
)

func init() { testutil.InitLogger() }

// fakeApp records SoftInstall/SoftConfigure calls and returns configured errors.
type fakeApp struct {
	installErr     error
	configureErr   error
	installCalls   int
	configureCalls int
}

func (f *fakeApp) SoftInstall() error {
	f.installCalls++
	return f.installErr
}

func (f *fakeApp) SoftConfigure() error {
	f.configureCalls++
	return f.configureErr
}

func TestInstallAndConfigure_InstallsAll(t *testing.T) {
	app := &fakeApp{}
	a := &AITools{appsOverride: []namedInstallable{{"rtk", app}}}

	a.InstallAndConfigure(nil, nil)

	if app.installCalls != 1 {
		t.Errorf("expected 1 SoftInstall call, got %d", app.installCalls)
	}
	if app.configureCalls != 1 {
		t.Errorf("expected 1 SoftConfigure call, got %d", app.configureCalls)
	}
}

func TestInstallAndConfigure_SkipFilter(t *testing.T) {
	app := &fakeApp{}
	a := &AITools{appsOverride: []namedInstallable{{"rtk", app}}}

	a.InstallAndConfigure(nil, map[string]bool{"rtk": true})

	if app.installCalls != 0 {
		t.Errorf("expected 0 SoftInstall calls when skipped, got %d", app.installCalls)
	}
}

func TestInstallAndConfigure_AppFilterExcludes(t *testing.T) {
	app := &fakeApp{}
	other := &fakeApp{}
	a := &AITools{appsOverride: []namedInstallable{{"rtk", app}, {"other", other}}}

	a.InstallAndConfigure(map[string]bool{"rtk": true}, nil)

	if app.installCalls != 1 {
		t.Errorf("expected rtk to install, got %d calls", app.installCalls)
	}
	if other.installCalls != 0 {
		t.Errorf("expected other NOT to install, got %d calls", other.installCalls)
	}
}

func TestInstallAndConfigure_InstallErrorSkipsConfigure(t *testing.T) {
	app := &fakeApp{installErr: errors.New("boom")}
	a := &AITools{appsOverride: []namedInstallable{{"rtk", app}}}

	a.InstallAndConfigure(nil, nil)

	if app.configureCalls != 0 {
		t.Errorf(
			"expected SoftConfigure not called after install error, got %d",
			app.configureCalls,
		)
	}
}
