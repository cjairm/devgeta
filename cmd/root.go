/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/buildinfo"
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

// Execute runs the root command after wiring a process-level context that
// SIGINT and SIGTERM cancel. That context is what every command the shared
// executor runs (internal/commands.BaseCommand.ExecCommand) roots its own
// context in — see commands.SetRootContext — so a Ctrl-C or a SIGTERM takes
// the same path a per-call Timeout already does: it cancels the context,
// which fires exec.CommandContext's Cancel hook and, for a NoStdin command,
// group-kills the whole child tree instead of leaving forks (a brew
// invoking curl, an opencode invoking a model call) running unattended.
//
// The handling is two-stage, and both stages matter:
//  1. The first SIGINT/SIGTERM cancels the context. Whatever is running gets
//     the same treatment a timeout gives it.
//  2. context.AfterFunc runs stop (signal.NotifyContext's own unregister
//     function) once the context is done, restoring the default signal
//     disposition. That is what lets a *second* Ctrl-C or SIGTERM terminate
//     devgeta itself the normal way — without it, the handler would stay
//     installed and swallow every later signal, leaving a user facing a
//     wedged command with no way out.
func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	context.AfterFunc(ctx, stop)
	commands.SetRootContext(ctx)

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

	rootCmd.Version = buildinfo.Version
	rootCmd.SetVersionTemplate(fmt.Sprintf(
		"devgeta {{.Version}} (commit: %s, built: %s)\n", buildinfo.Commit, buildinfo.BuildDate,
	))
}
