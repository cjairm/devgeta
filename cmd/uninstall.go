/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"fmt"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/registry"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/utils"
	"github.com/spf13/cobra"
)

// uninstallGetAppFn is the registry lookup for uninstall; overridden in tests.
var uninstallGetAppFn = func(name string) (apps.App, error) {
	return registry.GetApp(name)
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <app|category>",
	Short: "Uninstall an app or category installed by devgeta",
	Long: `Reverses the install process for an app or category.
Only removes apps that devgeta originally installed. Pre-existing apps are skipped.

Examples:
  dg uninstall git           # uninstall a single app
  dg uninstall terminal      # uninstall all terminal apps devgeta installed
`,
	Args: cobra.ExactArgs(1),
	RunE: runUninstall,
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}

func runUninstall(_ *cobra.Command, args []string) error {
	target := args[0]

	// Block reserved targets before any other validation.
	if target == "languages" || target == "databases" {
		return fmt.Errorf("dg uninstall %s is not yet supported — manage runtimes via mise", target)
	}
	if target == "devgeta" {
		return fmt.Errorf("cannot uninstall devgeta from itself")
	}

	isApp := registry.IsKnownApp(target)
	isCategory := registry.IsKnownCategory(target)

	if !isApp && !isCategory {
		categories := strings.Join(registry.KnownCategories(), ", ")
		return fmt.Errorf(
			"unknown target %q\n\nValid categories: %s\nValid apps: see `dg install --help`",
			target,
			categories,
		)
	}

	// Build the list of app names to process.
	var targets []string
	if isCategory {
		targets = registry.AppsByCoordinator(target)
	} else {
		targets = []string{target}
	}

	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}

	var failedApps []string
	shellFeatureChanged := false

	for _, name := range targets {
		meta := registry.Meta[name]

		if !gc.IsInstalledByDevgeta(name, meta.ItemType) {
			logger.L().Infow("skipping: not installed by devgeta", "app", name)
			utils.PrintInfo(fmt.Sprintf("skipping %s: not installed by devgeta", name))
			continue
		}

		app, err := uninstallGetAppFn(name)
		if err != nil {
			logger.L().Errorw("failed to get app", "app", name, "error", err)
			failedApps = append(failedApps, name)
			continue
		}

		if err := app.Uninstall(); err != nil {
			logger.L().Errorw("uninstall failed", "app", name, "error", err)
			utils.PrintError(fmt.Sprintf("failed to uninstall %s: %v", name, err))
			failedApps = append(failedApps, name)
			continue
		}

		utils.PrintSuccess(fmt.Sprintf("uninstalled %s", name))
		if meta.HasShellFeature {
			shellFeatureChanged = true
		}
	}

	if shellFeatureChanged {
		utils.PrintInfo("Run `source ~/.zshrc` to apply shell changes.")
	}

	if len(failedApps) > 0 {
		return fmt.Errorf("uninstall failed for: %s", strings.Join(failedApps, ", "))
	}

	return nil
}
