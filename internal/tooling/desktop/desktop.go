package desktop

import (
	"fmt"

	"github.com/cjairm/devgeta/internal/apps/aerospace"
	"github.com/cjairm/devgeta/internal/apps/alacritty"
	"github.com/cjairm/devgeta/internal/apps/brave"
	"github.com/cjairm/devgeta/internal/apps/docker"
	"github.com/cjairm/devgeta/internal/apps/flameshot"
	"github.com/cjairm/devgeta/internal/apps/fonts"
	"github.com/cjairm/devgeta/internal/apps/gimp"
	"github.com/cjairm/devgeta/internal/apps/handy"
	"github.com/cjairm/devgeta/internal/apps/i3"
	"github.com/cjairm/devgeta/internal/apps/raycast"
	"github.com/cjairm/devgeta/internal/apps/ulauncher"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/promptui"
	"github.com/cjairm/devgeta/pkg/utils"
)

// softInstaller is the subset of apps.App used by the desktop coordinator.
type softInstaller interface {
	SoftInstall() error
}

// namedInstaller pairs an app name with its installer. Used for injection in tests.
type namedInstaller struct {
	name string
	app  softInstaller
}

type Desktop struct {
	Cmd  cmd.Command
	Base cmd.BaseCommand
	// crossPlatformAppsOverride replaces the default cross-platform app list when non-nil (tests).
	crossPlatformAppsOverride []namedInstaller
	// launcherOverride replaces the platform-specific launcher (raycast/ulauncher) when non-nil (tests).
	launcherOverride *namedInstaller
}

func New() *Desktop {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Desktop{Cmd: osCmd, Base: *baseCmd}
}

func (d *Desktop) getCrossPlatformApps() []namedInstaller {
	if d.crossPlatformAppsOverride != nil {
		return d.crossPlatformAppsOverride
	}
	return []namedInstaller{
		{constants.Docker, docker.New()},
		{constants.Gimp, gimp.New()},
		{constants.Brave, brave.New()},
		{constants.Flameshot, flameshot.New()},
		{constants.Handy, handy.New()},
	}
}

// shouldInstallApp returns true when an app should run given the active filters.
// appFilter non-empty: app must be in the filter. skipFilter always excludes.
func shouldInstallApp(name string, appFilter, skipFilter map[string]bool) bool {
	if skipFilter[name] {
		return false
	}
	if len(appFilter) > 0 && !appFilter[name] {
		return false
	}
	return true
}

// InstallAndConfigure runs the full desktop setup.
// appFilter: when non-empty, only those apps are installed (fonts skipped).
// skipFilter: those apps are always skipped regardless of appFilter.
func (d *Desktop) InstallAndConfigure(appFilter, skipFilter map[string]bool) error {
	if shouldInstallApp(constants.Alacritty, appFilter, skipFilter) {
		err := d.InstallAlacritty()
		displayMessage(err, constants.Alacritty)
	}

	// Platform-specific window managers
	if d.Base.Platform.IsMac() {
		if shouldInstallApp(constants.Aerospace, appFilter, skipFilter) {
			err := d.InstallAerospace()
			displayMessage(err, constants.Aerospace)
		}
	} else {
		if shouldInstallApp(constants.I3, appFilter, skipFilter) {
			err := d.InstallI3()
			displayMessage(err, constants.I3)
		}
	}

	// Fonts only run when no specific app filter is active
	if len(appFilter) == 0 {
		utils.PrintInfo("Installing fonts (if no previously installed)...")
		f := fonts.New()
		f.SoftInstallAll()
	}

	d.InstallDesktopAppsWithoutConfiguration(appFilter, skipFilter)

	if d.Base.Platform.IsMac() {
		d.DisplayPrivacyInstructions()
	}

	return nil
}

// InstallDesktopAppsWithoutConfiguration installs cross-platform and launcher apps with filtering.
func (d *Desktop) InstallDesktopAppsWithoutConfiguration(appFilter, skipFilter map[string]bool) {
	for _, entry := range d.getCrossPlatformApps() {
		if cmd.Interrupted() {
			utils.PrintWarning("Installation interrupted; stopping desktop app installs.")
			break
		}
		if !shouldInstallApp(entry.name, appFilter, skipFilter) {
			continue
		}
		if err := entry.app.SoftInstall(); err != nil {
			displayMessage(err, entry.name)
		}
	}

	// Platform-specific launchers
	if d.launcherOverride != nil {
		entry := d.launcherOverride
		if shouldInstallApp(entry.name, appFilter, skipFilter) {
			if err := entry.app.SoftInstall(); err != nil {
				displayMessage(err, entry.name)
			}
		}
	} else if d.Base.Platform.IsMac() {
		if shouldInstallApp(constants.Raycast, appFilter, skipFilter) {
			r := raycast.New()
			if err := r.SoftInstall(); err != nil {
				displayMessage(err, constants.Raycast)
			}
		}
	} else {
		if shouldInstallApp(constants.Ulauncher, appFilter, skipFilter) {
			u := ulauncher.New()
			if err := u.SoftInstall(); err != nil {
				displayMessage(err, constants.Ulauncher)
			}
		}
	}
}

func (d *Desktop) InstallAlacritty() error {
	a := alacritty.New()
	err := a.SoftInstall()
	if err != nil {
		return err
	}
	err = a.SoftConfigure()
	if err != nil {
		return err
	}
	return nil
}

func (d *Desktop) InstallAerospace() error {
	a := aerospace.New()
	err := a.SoftInstall()
	if err != nil {
		return err
	}
	err = a.SoftConfigure()
	if err != nil {
		return err
	}
	return nil
}

func (d *Desktop) InstallI3() error {
	i := i3.New()
	err := i.SoftInstall()
	if err != nil {
		return err
	}
	err = i.SoftConfigure()
	if err != nil {
		return err
	}
	return nil
}

func (d *Desktop) DisplayPrivacyInstructions() error {
	instructions := `
1. Open System Preferences.
2. Go to Security & Privacy.
3. Click on the Privacy tab.
4. Select Full Disk Access from the left sidebar.
5. Click the lock icon in the bottom left corner to make changes and enter your password.
`
	return promptui.DisplayInstructions(
		"To enable full functionality of the applications, please do the following",
		instructions,
		false,
	)
}

func displayMessage(err error, desktopAppName string, displayOnlyErrors ...bool) {
	if err != nil {
		logger.L().Errorw("Error installing ", "desktop_app", desktopAppName, "error", err)
		utils.PrintWarning(
			fmt.Sprintf(
				"Install (%s) errored... To halt the installation, press ctrl+c or use --debug flag to see more details",
				desktopAppName,
			),
		)
	} else {
		if displayOnlyErrors != nil && displayOnlyErrors[0] == true {
			return
		}
		msg := fmt.Sprintf("Installing %s (if no previously installed)...", desktopAppName)
		utils.PrintInfo(msg)
	}
}
