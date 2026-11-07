/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"errors"
	"fmt"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/devgeta"
	"github.com/cjairm/devgeta/internal/apps/registry"
	"github.com/cjairm/devgeta/pkg/utils"
	"github.com/spf13/cobra"
)

var configureForce bool

// getAppFn is the registry lookup; overridden in tests.
var getAppFn = func(name string) (apps.App, error) {
	return registry.GetApp(name)
}

// refreshEmbeddedConfigs re-extracts embedded configs so templates match the
// running binary. It is a no-op when the published tree already belongs to this
// build - same version, commit and config content (see buildStamp) - so
// configuring one app no longer rewrites the whole tree first. An app's own
// --force does not re-extract; this check is what keeps its templates current.
// Overridden in tests to avoid nil ExtractEmbedded.
var refreshEmbeddedConfigs = func() error {
	return devgeta.New().InstallIfStale()
}

var configureCmd = &cobra.Command{
	Use:   "configure [app]",
	Short: "Apply configuration files for a named app",
	Long: `Apply configuration files for a named app without reinstalling.

By default (soft mode), configuration is only applied if files do not already exist.
Use --force to overwrite existing configuration files.

Examples:
  dg configure git             # Apply git config if not already present
  dg configure neovim --force  # Overwrite existing neovim config
`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigure,
}

func init() {
	rootCmd.AddCommand(configureCmd)

	configureCmd.Flags().
		BoolVar(&configureForce, "force", false, "Overwrite existing configuration files")
}

func runConfigure(cmd *cobra.Command, args []string) error {
	// Sweep for a crashed `dg theme set`'s leftover backups before writing
	// anything: otherwise a --force here can write a fresh config while its
	// .dg-theme-backup sibling still sits beside it, and a later `dg theme
	// set` would restore that stale backup right back over this run's work
	// (cycle doc Step 5).
	if msg, err := recoverInterruptedFn(); err != nil {
		return fmt.Errorf("failed to recover an interrupted theme switch: %w", err)
	} else if msg != "" {
		utils.PrintInfo(msg)
	}

	appName := args[0]

	// Re-extract embedded configs so templates always match the running binary.
	// Without this, a newer binary may configure apps using stale templates
	// left on disk by an older version.
	if err := refreshEmbeddedConfigs(); err != nil {
		return err
	}

	app, err := getAppFn(appName)
	if err != nil {
		return err
	}

	if configureForce {
		err = app.ForceConfigure()
	} else {
		err = app.SoftConfigure()
	}

	if errors.Is(err, apps.ErrConfigureNotSupported) {
		utils.PrintInfo("configure not supported for " + appName)
		return nil
	}
	if err != nil {
		return err
	}

	utils.PrintSuccess("configured " + appName)
	return nil
}
