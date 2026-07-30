/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"fmt"

	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/utils"
	"github.com/spf13/cobra"
)

var verbose bool

var rootCmd = &cobra.Command{
	Use:          "dg",
	SilenceUsage: true,
	Short:        "Devgeta - Your cross-platform CLI to install, configure, and manage development environments",
	Long: `Devgeta (dg) helps you set up and manage your development environment with ease.

Key Features:
  • Debian/Ubuntu and macOS support
  • Install, configure, and uninstall development apps, fonts, themes, and languages
  • Maintain a global manifest of installed components to prevent conflicts
  • Choose and apply themes and fonts for your environment
  • Reconfigure or force reconfigure apps and dotfiles
  • Safely uninstall only what Devgeta managed
  • Detect and revert failed installs to keep your system clean
  • Create and restore configuration backups
  • Validate your setup to catch issues early
  • Verbose output mode for better insight into what’s happening

Available Commands:
  install        Install apps, languages, fonts, themes (with optional --soft mode)
  reinstall      Force reinstallation and configuration
  configure      Apply configuration files for a named app (e.g., dg configure git)
  re-configure   Re-apply configuration even if already present
  uninstall      Remove previously installed apps or assets (fonts/themes) safely
  update         Update selected apps (e.g., --neovim, --aerospace)
  list           View all items installed via Devgeta
  config         View and change devgeta settings (worktree.*)
  check-updates  See if any managed apps have updates
  backup         Create a backup of your current Devgeta-managed environment
  restore        Restore a previous backup configuration
  change         Change font or theme (--theme=..., --font=...)
  version        Print the version number of devgeta

Examples:
  dg install
  dg uninstall --font=my-font --app=aerospace
  dg re-configure --app=neovim
  dg change --theme=tokyonight --font=JetBrainsMono
  dg backup --output=~/dg_backup.json
`,
}

func Execute() {
	err := rootCmd.Execute()
	utils.MaybeExitWithError(err)
}

func init() {
	rootCmd.PersistentFlags().
		BoolVar(&verbose, "verbose", false, "Enable verbose logging")

	// Ensure this runs before any subcommand
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Init logger here using the global verbose flag
		logger.Init(verbose)
		return nil
	}

	rootCmd.SetHelpFunc(utils.PrompCustomHelp)

	rootCmd.Version = Version
	rootCmd.SetVersionTemplate(fmt.Sprintf(
		"devgeta {{.Version}} (commit: %s, built: %s)\n", Commit, BuildDate,
	))
}
