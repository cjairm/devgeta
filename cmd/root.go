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
	Use: "dg",
	// Both are silenced because devgeta reports failures itself, through
	// utils.MaybeExitWithError below. Leaving SilenceErrors off printed every
	// RunE failure twice — once by Cobra on stderr, once by us on stdout.
	SilenceUsage:  true,
	SilenceErrors: true,
	Short:         "Devgeta - Your cross-platform CLI to install, configure, and manage development environments",
	// The command list is deliberately NOT written here. It used to be, and it
	// drifted: the help advertised six commands that did not exist (reinstall,
	// re-configure, update, check-updates, backup, restore - the last four are
	// planned, and live in ROADMAP.md) while hiding five that did (completion,
	// archive, task, workspace, worktree), plus every flag. Cobra renders the
	// commands and flags from what is registered, so only prose belongs in
	// Long, and a planned command belongs in ROADMAP.md until it runs.
	Long: `Devgeta (dg) helps you set up and manage your development environment.

  • One command syntax on both macOS and Debian/Ubuntu
  • Installs, configures, and uninstalls apps, fonts, themes, and languages
  • Tracks what it installed, so it only ever removes its own work
  • Re-applies configuration on demand, without touching your edits
  • Switches the theme of every themed app together`,
	Example: `  dg install
  dg install --only terminal
  dg configure neovim --force
  dg theme set tokyonight
  dg completion zsh`,
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

	// The same help function taskCmd and configCmd already opted into. The
	// root used to print only Use+Long, which is why they had to opt out at
	// all: it hid every subcommand and flag from `dg <sub> --help` too. One
	// help function for the whole tree means no command can be left out of the
	// listing by hand.
	rootCmd.SetHelpFunc(standardHelpFunc)

	rootCmd.Version = buildinfo.Version
	rootCmd.SetVersionTemplate(fmt.Sprintf(
		"devgeta {{.Version}} (commit: %s, built: %s)\n", buildinfo.Commit, buildinfo.BuildDate,
	))
}
