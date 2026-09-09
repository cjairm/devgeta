package desktop

import (
	"fmt"

	"github.com/cjairm/devgeta/internal/apps/aerospace"
	"github.com/cjairm/devgeta/internal/apps/alacritty"
	"github.com/cjairm/devgeta/internal/apps/brave"
	"github.com/cjairm/devgeta/internal/apps/docker"
	"github.com/cjairm/devgeta/internal/apps/flameshot"
	"github.com/cjairm/devgeta/internal/apps/fonts"
	"github.com/cjairm/devgeta/internal/apps/ghostty"
	"github.com/cjairm/devgeta/internal/apps/gimp"
	"github.com/cjairm/devgeta/internal/apps/handy"
	"github.com/cjairm/devgeta/internal/apps/i3"
	"github.com/cjairm/devgeta/internal/apps/raycast"
	"github.com/cjairm/devgeta/internal/apps/shottr"
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

// terminalInstaller is the subset of apps.App the terminal chooser needs:
// install, then configure — exactly what InstallAlacritty did for the single
// hardcoded terminal before this group existed.
type terminalInstaller interface {
	SoftInstall() error
	SoftConfigure() error
}

// terminalCandidateEntry pairs a name with its terminalInstaller and an
// availability predicate — the terminal group's namedInstaller, extended
// with what chooseOne needs to decide whether to prompt at all.
type terminalCandidateEntry struct {
	name      string
	app       terminalInstaller
	available func() bool
}

// screenshotCandidateEntry is the screenshot group's namedInstaller,
// extended the same way terminalCandidateEntry extends the terminal group's:
// with the availability predicate chooseOne needs. Screenshot tools only
// need SoftInstall (the cross-platform apps loop never calls SoftConfigure
// on them), so it reuses softInstaller rather than terminalInstaller.
type screenshotCandidateEntry struct {
	name      string
	app       softInstaller
	available func() bool
}

type Desktop struct {
	Cmd  cmd.Command
	Base cmd.BaseCommand
	// crossPlatformAppsOverride replaces the default cross-platform app list when non-nil (tests).
	crossPlatformAppsOverride []namedInstaller
	// launcherOverride replaces the platform-specific launcher (raycast/ulauncher) when non-nil (tests).
	launcherOverride *namedInstaller
	// terminalCandidatesOverride replaces the default terminal candidate group when non-nil (tests).
	terminalCandidatesOverride []terminalCandidateEntry
	// screenshotCandidatesOverride replaces the default screenshot candidate group when non-nil (tests).
	screenshotCandidatesOverride []screenshotCandidateEntry
	// selectOverride replaces promptui.Select when non-nil (tests) — see chooseTerminal/chooseScreenshot.
	selectOverride func(label string, options []string) (string, error)
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
		{constants.Handy, handy.New()},
	}
}

// getScreenshotCandidates returns the screenshot group's candidate list:
// flameshot and shottr, each paired with a predicate answering whether it
// can actually be installed on this platform (see candidates.go).
func (d *Desktop) getScreenshotCandidates() []screenshotCandidateEntry {
	if d.screenshotCandidatesOverride != nil {
		return d.screenshotCandidatesOverride
	}
	base := &d.Base
	isMac := d.Base.IsMac()
	return []screenshotCandidateEntry{
		{
			name: constants.Flameshot,
			app:  flameshot.New(),
			available: func() bool {
				return platformAvailable(base, isMac, constants.Flameshot)
			},
		},
		{
			name: constants.Shottr,
			app:  shottr.New(),
			available: func() bool {
				return platformAvailable(base, isMac, constants.Shottr)
			},
		},
	}
}

// chooseScreenshot mirrors chooseTerminal: it narrows the screenshot group by
// appFilter/skipFilter first, so naming one with --only bypasses the prompt
// and picks that one even if it turns out unavailable, then resolves the
// remaining candidates via chooseOne.
func (d *Desktop) chooseScreenshot(appFilter, skipFilter map[string]bool) (softInstaller, string) {
	all := d.getScreenshotCandidates()
	filtered := make([]screenshotCandidateEntry, 0, len(all))
	for _, c := range all {
		if shouldInstallApp(c.name, appFilter, skipFilter) {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		return nil, ""
	}

	candidates := make([]candidate, len(filtered))
	for i, c := range filtered {
		candidates[i] = candidate{name: c.name, available: c.available}
	}

	selectFn := d.selectOverride
	if selectFn == nil {
		selectFn = promptui.Select
	}

	chosenName, ok := chooseOne("Choose a screenshot tool", candidates, selectFn)
	if !ok {
		return nil, ""
	}
	for _, c := range filtered {
		if c.name == chosenName {
			return c.app, c.name
		}
	}
	return nil, ""
}

// getTerminalCandidates returns the terminal group's candidate list:
// alacritty and ghostty, each paired with a predicate answering whether it
// can actually be installed on this platform (see candidates.go).
func (d *Desktop) getTerminalCandidates() []terminalCandidateEntry {
	if d.terminalCandidatesOverride != nil {
		return d.terminalCandidatesOverride
	}
	base := &d.Base
	isMac := d.Base.IsMac()
	return []terminalCandidateEntry{
		{
			name: constants.Alacritty,
			app:  alacritty.New(),
			available: func() bool {
				return platformAvailable(base, isMac, constants.Alacritty)
			},
		},
		{
			name: constants.Ghostty,
			app:  ghostty.New(),
			available: func() bool {
				return platformAvailable(base, isMac, constants.Ghostty)
			},
		},
	}
}

// chooseTerminal narrows the terminal group by appFilter/skipFilter first —
// so naming one with --only bypasses the prompt and picks that one even if
// it turns out unavailable, rather than falling through to the other — then
// resolves the remaining candidates via chooseOne. Returns (nil, "") when
// nothing in the (possibly filtered) group is available.
func (d *Desktop) chooseTerminal(
	appFilter, skipFilter map[string]bool,
) (terminalInstaller, string) {
	all := d.getTerminalCandidates()
	filtered := make([]terminalCandidateEntry, 0, len(all))
	for _, c := range all {
		if shouldInstallApp(c.name, appFilter, skipFilter) {
			filtered = append(filtered, c)
		}
	}
	if len(filtered) == 0 {
		return nil, ""
	}

	candidates := make([]candidate, len(filtered))
	for i, c := range filtered {
		candidates[i] = candidate{name: c.name, available: c.available}
	}

	selectFn := d.selectOverride
	if selectFn == nil {
		selectFn = promptui.Select
	}

	chosenName, ok := chooseOne("Choose a terminal emulator", candidates, selectFn)
	if !ok {
		return nil, ""
	}
	for _, c := range filtered {
		if c.name == chosenName {
			return c.app, c.name
		}
	}
	return nil, ""
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
	if app, name := d.chooseTerminal(appFilter, skipFilter); app != nil {
		err := installTerminal(app)
		displayMessage(err, name)
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

	// Screenshot tool: chosen from {flameshot, shottr} by platform availability.
	if app, name := d.chooseScreenshot(appFilter, skipFilter); app != nil {
		if err := app.SoftInstall(); err != nil {
			displayMessage(err, name)
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

// installTerminal runs the two-step lifecycle every terminal candidate
// needs: install, then configure — what InstallAlacritty did before the
// terminal group existed.
func installTerminal(app terminalInstaller) error {
	if err := app.SoftInstall(); err != nil {
		return err
	}
	return app.SoftConfigure()
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
		logger.L().
			Errorw("Error installing desktop app", "desktop_app", desktopAppName, "error", err)
		utils.PrintWarning(
			fmt.Sprintf(
				"Install (%s) errored... To halt the installation, press ctrl+c or use --debug flag to see more details",
				desktopAppName,
			),
		)
	} else {
		// len, not a nil check: a caller passing an empty slice explicitly
		// would index out of range on the nil check alone.
		if len(displayOnlyErrors) > 0 && displayOnlyErrors[0] {
			return
		}
		msg := fmt.Sprintf("Installing %s (if no previously installed)...", desktopAppName)
		utils.PrintInfo(msg)
	}
}
