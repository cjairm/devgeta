/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/registry"
	"github.com/cjairm/devgeta/internal/tooling/appstate"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/progress"
	"github.com/spf13/cobra"
)

var (
	exportDryRunFlag  bool
	exportGroupFlag   []string
	exportProfileFlag []string
)

// stateReportOut is where `dg export` and `dg import` draw their report.
// It is a seam for the same reason archiveProgressOut is: a command whose
// whole job is to say exactly what moves has that report as its behavior,
// and a test that cannot read it is not testing the command.
var stateReportOut io.Writer = os.Stdout

// statePorterFor resolves an app name to its state adapter. It is a
// variable so a test can supply a porter that does not shell out — the real
// adapters answer IsRunning by running pgrep, which a test must never do.
var statePorterFor = lookupStatePorter

var exportCmd = &cobra.Command{
	Use:   "export <app> [destination-dir]",
	Short: "Pack an app's own state into one portable bundle",
	Long: `Writes the state you accumulated inside an app — bookmarks, open tabs,
settings — into one verified .tar.zst on <destination-dir>, so it can be
carried to a new machine and restored with "dg import".

This moves ONLY what the app's adapter names. Passwords, cookies, autofill
and anything bound to this machine are never included, at any flag (see
ADR-0045). It is not a backup of the app's folder — for that, use
"dg archive".

  dg export brave --dry-run                 # show every group and its size
  dg export brave /Volumes/SSD              # the default-on groups
  dg export brave /Volumes/SSD --group extensions,history
  dg export brave /Volumes/SSD --profile "Profile 1"

Quit the app first: its state is SQLite and LevelDB, and a copy taken from
under a live process arrives corrupt.

The bundle is a plain tar with a checksum manifest beside it, so it can be
inspected and extracted without devgeta:
  tar --zstd -tf NAME.tar.zst                  # list without extracting
  shasum -a 256 -c NAME.tar.zst.sha256         # the bundle file is intact
`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runExport,
}

func init() {
	exportCmd.Flags().
		BoolVar(&exportDryRunFlag, "dry-run", false, "Report what would move without writing anything")
	exportCmd.Flags().StringSliceVar(
		&exportGroupFlag,
		"group",
		nil,
		"Export only these state groups (comma-separated); the default is every group that is on",
	)
	exportCmd.Flags().StringSliceVar(
		&exportProfileFlag,
		"profile",
		nil,
		"Export only these profiles (comma-separated); the default is all of them",
	)
	rootCmd.AddCommand(exportCmd)
}

func runExport(_ *cobra.Command, args []string) error {
	app := args[0]
	porter, err := statePorterFor(app)
	if err != nil {
		return err
	}

	destDir := ""
	if len(args) > 1 {
		destDir = args[1]
	}
	// A dry run writes nothing, so asking for a drive to not write to is
	// pure friction — and the report is exactly what a user wants before
	// they have gone looking for the drive.
	if !exportDryRunFlag {
		if destDir == "" {
			return fmt.Errorf(
				"dg export %s needs a destination directory, or --dry-run to only report",
				app,
			)
		}
		info, statErr := os.Stat(destDir)
		if statErr != nil {
			return fmt.Errorf("destination %s: %w", destDir, statErr)
		}
		if !info.IsDir() {
			return fmt.Errorf("destination %s is not a directory", destDir)
		}
	}

	// The name depends on nothing but the date, so the never-overwrite
	// refusal comes before the profile is walked: it costs nothing there,
	// and it puts the reason at the top of the output instead of under a
	// screen of report.
	name := appstate.BundleName(app, archiveNow())
	if !exportDryRunFlag {
		if err := refuseIfAnyOutputExists(destDir, name, appstate.BundleExt); err != nil {
			return err
		}
	}

	plan, err := appstate.Prepare(porter, appstate.Selection{
		Profiles: exportProfileFlag,
		Groups:   exportGroupFlag,
	})
	if err != nil {
		return err
	}
	printStatePlan(app, plan)

	if exportDryRunFlag {
		stateLine(constants.Blue, "Dry run: nothing written.")
		return nil
	}

	warnIfShortOnSpace(destDir, plan.SelectedBytes())

	writeMeter := progress.New(archiveProgressOut, writeMeterLabel, plan.SelectedBytes())
	writeMeter.Start()
	result, err := appstate.WriteBundle(plan, destDir, name, appstate.ExportOptions{
		OnWriteProgress: writeMeter.Add,
	})
	writeMeter.Stop()
	if err != nil {
		return err
	}

	stateLine(constants.Green, "wrote "+result.BundlePath)
	stateLine(constants.Green, "wrote "+result.ManifestPath)
	stateLine(constants.Green, "wrote "+result.SkipReportPath)
	// The registry is what carries each profile's name to the new machine,
	// so it has to travel with the bundle — naming it here is what tells the
	// user there is a fourth file to copy (ADR-0046).
	if result.RegistryPath != "" {
		stateLine(constants.Green, "wrote "+result.RegistryPath)
	}

	// Verified through the same helper `dg archive` uses, which sizes its
	// meter from the bundle on disk — this pass reads the compressed bytes
	// back off the drive, not the source.
	if err := verifyWithProgress(result.BundlePath); err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	stateLine(
		constants.Gray,
		"Import with:  dg import "+app+" "+filepath.Base(result.BundlePath),
	)
	stateLine(
		constants.Gray,
		"Or without devgeta:  "+restoreCommand(filepath.Base(result.BundlePath)),
	)
	return nil
}

// printStatePlan is the report ADR-0045 requires: every group with its
// paths, its size and whether it is on, so what moves is checkable before
// anything is written. Groups that are OFF are printed too — "what did not
// move" is the half of the answer a user cannot get any other way.
func printStatePlan(app string, plan *appstate.Plan) {
	stateLine(constants.Bold, fmt.Sprintf(
		"%s — %d profile(s), %d file(s), %s to move",
		app,
		len(plan.Profiles),
		plan.FileCount(),
		progress.FormatBytes(plan.SelectedBytes()),
	))
	for _, profile := range plan.Profiles {
		stateLine(constants.Blue, "  "+profile.Key)
		for _, group := range profile.Groups {
			mark := "off"
			color := constants.Gray
			if group.Selected {
				mark = "on "
				color = ""
			}
			stateLine(color, fmt.Sprintf(
				"    [%s] %-20s %10s  %s",
				mark,
				group.Name,
				progress.FormatBytes(group.Bytes),
				group.Why,
			))
			for _, p := range group.Paths {
				note := ""
				if !p.Exists {
					note = "  (not on this machine)"
				}
				stateLine(constants.Gray, "           "+p.Path+note)
			}
		}
	}
}

// lookupStatePorter resolves an app name through the same registry
// `dg configure <app>` uses, then asks whether it has a state adapter. An
// app without one is not an error the user can fix by spelling it
// differently, so the message says so and names the apps that do.
func lookupStatePorter(name string) (appstate.Porter, error) {
	app, err := registry.GetApp(name)
	if err != nil {
		return nil, err
	}
	porter, ok := app.(appstate.Porter)
	if !ok {
		return nil, fmt.Errorf(
			"%s has no portable state, so there is nothing to export or import\n\n"+
				"Apps that do:\n  %s",
			name,
			strings.Join(portableApps(), "\n  "),
		)
	}
	return porter, nil
}

// portableApps lists every registered app that implements the adapter.
func portableApps() []string {
	var names []string
	for _, name := range registry.Names() {
		app, err := registry.GetApp(name)
		if err != nil {
			continue
		}
		if _, ok := app.(apps.StatePorter); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return []string{"(none yet)"}
	}
	return names
}

// stateLine writes one report line. It goes through stateReportOut rather
// than pkg/utils so a test can read back what the run told the user; the
// colors are the same constants utils prints with, so the terminal output
// is indistinguishable.
func stateLine(color, msg string) {
	if color == "" {
		fmt.Fprintln(stateReportOut, msg)
		return
	}
	fmt.Fprintf(stateReportOut, "%s%s%s\n", color, msg, constants.Reset)
}
