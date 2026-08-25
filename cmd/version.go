/*
 * Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"fmt"
	"io"
	"runtime/debug"

	"github.com/cjairm/devgeta/pkg/buildinfo"
	"github.com/spf13/cobra"
)

// readBuildInfo is overridable for tests.
var readBuildInfo = debug.ReadBuildInfo

// resolveVersionInfo returns the version, commit, and build date, falling
// back to runtime/debug.BuildInfo when ldflags weren't applied (e.g. plain
// `go build` or `go install github.com/cjairm/devgeta@latest`).
func resolveVersionInfo() (version, commit, buildDate string) {
	version, commit, buildDate = buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate

	info, ok := readBuildInfo()
	if !ok {
		return
	}

	if version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}

	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if commit == "unknown" && s.Value != "" {
				if len(s.Value) > 7 {
					commit = s.Value[:7]
				} else {
					commit = s.Value
				}
			}
		case "vcs.time":
			if buildDate == "unknown" && s.Value != "" {
				buildDate = s.Value
			}
		}
	}
	return
}

func printVersion(w io.Writer) {
	version, commit, buildDate := resolveVersionInfo()
	fmt.Fprintf(w, "devgeta %s (commit: %s, built: %s)\n", version, commit, buildDate)
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of devgeta",
	Long:  `All software has versions. This is devgeta's.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		printVersion(cmd.OutOrStdout())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
