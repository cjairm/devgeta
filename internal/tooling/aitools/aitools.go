// Package aitools coordinates installation of the ai-tools category:
// tools that make AI coding agents cheaper or more capable (rtk today;
// Ollama, Gemini CLI planned — see ROADMAP.md and ADR-0004).
package aitools

import (
	"fmt"

	"github.com/cjairm/devgeta/internal/apps/rtk"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/utils"
)

// softInstallable is the subset of apps.App used by the ai-tools coordinator.
type softInstallable interface {
	SoftInstall() error
	SoftConfigure() error
}

// namedInstallable pairs an app name with its installer. Used for injection in tests.
type namedInstallable struct {
	name string
	app  softInstallable
}

type AITools struct {
	Cmd  cmd.Command
	Base cmd.BaseCommand
	// appsOverride replaces the default ai-tools app list when non-nil (used in tests).
	appsOverride []namedInstallable
}

func New() *AITools {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &AITools{Cmd: osCmd, Base: *baseCmd}
}

// defaultApps returns the production list of registry apps managed by this coordinator.
func defaultApps() []namedInstallable {
	return []namedInstallable{
		{constants.Rtk, rtk.New()},
	}
}

func (a *AITools) getApps() []namedInstallable {
	if a.appsOverride != nil {
		return a.appsOverride
	}
	return defaultApps()
}

// InstallAndConfigure installs the ai-tools registry apps.
// appFilter: when non-empty, only those apps are installed.
// skipFilter: those apps are always skipped regardless of appFilter.
func (a *AITools) InstallAndConfigure(appFilter, skipFilter map[string]bool) {
	for _, entry := range a.getApps() {
		if skipFilter[entry.name] {
			continue
		}
		if len(appFilter) > 0 && !appFilter[entry.name] {
			continue
		}
		utils.PrintInfo(fmt.Sprintf("Installing %s (if no previously installed)...", entry.name))
		if err := entry.app.SoftInstall(); err != nil {
			logger.L().Errorw("Error installing", "package_name", entry.name, "error", err)
			utils.PrintWarning(fmt.Sprintf(
				"Install (%s) errored... To halt the installation, press ctrl+c or use --debug flag to see more details",
				entry.name,
			))
			continue
		}
		if err := entry.app.SoftConfigure(); err != nil {
			logger.L().Errorw("Error configuring", "package_name", entry.name, "error", err)
			utils.PrintWarning(fmt.Sprintf(
				"Configure (%s) errored... To halt the installation, press ctrl+c or use --debug flag to see more details",
				entry.name,
			))
		}
	}
}
