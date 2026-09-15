/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/cjairm/devgeta/internal/tooling/appstate"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/progress"
	"github.com/spf13/cobra"
)

var (
	importForceFlag   bool
	importGroupFlag   []string
	importProfileFlag []string
)

var importCmd = &cobra.Command{
	Use:   "import <app> <bundle-file>",
	Short: "Restore an app's state from a bundle written by dg export",
	Long: `Reads a bundle written by "dg export" back into the app's own directories
on this machine.

The bundle is checked against its manifest before anything is touched, and
every path it would replace is renamed aside first — so a failure partway
leaves the profile exactly as it was, and a restore you did not want can be
undone by hand.

  dg import brave /Volumes/SSD/brave-state-2026-09-15.tar.zst
  dg import brave BUNDLE --force             # the profile already has state
  dg import brave BUNDLE --group bookmarks   # restore only part of it

By default every group the bundle carries is restored, including ones that
are off by default on the way out: the export already decided what was
worth carrying.

Quit the app first, and copy the bundle's .sha256 along with it — without
the manifest the bundle cannot be checked, and the check is not skipped.
`,
	Args: cobra.ExactArgs(2),
	RunE: runImport,
}

func init() {
	importCmd.Flags().BoolVar(
		&importForceFlag,
		"force",
		false,
		"Replace state that is already there (each path is backed up beside itself first)",
	)
	importCmd.Flags().StringSliceVar(
		&importGroupFlag,
		"group",
		nil,
		"Restore only these state groups (comma-separated); the default is every group in the bundle",
	)
	importCmd.Flags().StringSliceVar(
		&importProfileFlag,
		"profile",
		nil,
		"Restore only these profiles (comma-separated); the default is every profile in the bundle",
	)
	rootCmd.AddCommand(importCmd)
}

func runImport(_ *cobra.Command, args []string) error {
	app := args[0]
	bundlePath := args[1]

	porter, err := statePorterFor(app)
	if err != nil {
		return err
	}

	var total int64
	if info, statErr := os.Stat(bundlePath); statErr == nil {
		total = info.Size()
	}
	meter := &lazyMeter{reporter: progress.New(archiveProgressOut, verifyMeterLabel, total)}
	result, err := appstate.Import(
		porter,
		bundlePath,
		appstate.Selection{Profiles: importProfileFlag, Groups: importGroupFlag},
		appstate.ImportOptions{Force: importForceFlag, OnVerifyProgress: meter.Add},
	)
	meter.Stop()
	if err != nil {
		return err
	}

	stateLine(constants.Green, fmt.Sprintf(
		"restored %d file(s) into %s: %s",
		result.Files,
		pluralProfiles(result.Profiles),
		strings.Join(result.Groups, ", "),
	))
	// The backups are the only undo, and an undo nobody was told about is
	// not one — so the suffix and a count are printed every run, not only
	// when something went wrong (ADR-0045's recoverability promise).
	if len(result.Backups) > 0 {
		stateLine(constants.Gray, fmt.Sprintf(
			"%d path(s) were renamed aside to a %q sibling first; delete them once %s looks right",
			len(result.Backups),
			appstate.BackupSuffix,
			app,
		))
		for _, p := range result.Backups {
			stateLine(constants.Gray, "  "+p)
		}
	}
	stateLine(constants.Gray, "Start "+app+" to check the result.")
	return nil
}

// lazyMeter starts a progress reporter on its first byte, and stops it only
// if it ever started. `dg import` runs five refusals before it reads a byte
// of the bundle — the name check, the manifest check, the writable check —
// and a meter started up front prints "Verifying 0 B" directly above an
// error that has nothing to do with verification.
//
// No lock: archive.Verify calls its progress func synchronously, on the
// goroutine that called it.
type lazyMeter struct {
	reporter *progress.Reporter
	started  bool
}

func (m *lazyMeter) Add(n int64) {
	if !m.started {
		m.started = true
		m.reporter.Start()
	}
	m.reporter.Add(n)
}

func (m *lazyMeter) Stop() {
	if m.started {
		m.reporter.Stop()
	}
}

func pluralProfiles(profiles []string) string {
	if len(profiles) == 1 {
		return "profile " + profiles[0]
	}
	return fmt.Sprintf("profiles %s", strings.Join(profiles, ", "))
}
