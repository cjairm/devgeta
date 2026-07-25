/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/cjairm/devgeta/internal/config"
	tuiinventory "github.com/cjairm/devgeta/internal/tui/inventory"
	"github.com/spf13/cobra"
)

// categoryDef binds a yaml key / display label to accessors for both the
// Installed and AlreadyInstalled buckets, which share the same field shape.
type categoryDef struct {
	Key              string
	Label            string
	Installed        func(*config.InstalledConfig) []string
	AlreadyInstalled func(*config.AlreadyInstalledConfig) []string
}

// categoryDefs is iterated in this fixed order for stable, testable output.
var categoryDefs = []categoryDef{
	{
		Key:              "packages",
		Label:            "Packages",
		Installed:        func(c *config.InstalledConfig) []string { return c.Packages },
		AlreadyInstalled: func(c *config.AlreadyInstalledConfig) []string { return c.Packages },
	},
	{
		Key:              "desktop_apps",
		Label:            "Desktop Apps",
		Installed:        func(c *config.InstalledConfig) []string { return c.DesktopApps },
		AlreadyInstalled: func(c *config.AlreadyInstalledConfig) []string { return c.DesktopApps },
	},
	{
		Key:              "fonts",
		Label:            "Fonts",
		Installed:        func(c *config.InstalledConfig) []string { return c.Fonts },
		AlreadyInstalled: func(c *config.AlreadyInstalledConfig) []string { return c.Fonts },
	},
	{
		Key:              "themes",
		Label:            "Themes",
		Installed:        func(c *config.InstalledConfig) []string { return c.Themes },
		AlreadyInstalled: func(c *config.AlreadyInstalledConfig) []string { return c.Themes },
	},
	{
		Key:              "terminal_tools",
		Label:            "Terminal Tools",
		Installed:        func(c *config.InstalledConfig) []string { return c.TerminalTools },
		AlreadyInstalled: func(c *config.AlreadyInstalledConfig) []string { return c.TerminalTools },
	},
	{
		Key:              "dev_languages",
		Label:            "Dev Languages",
		Installed:        func(c *config.InstalledConfig) []string { return c.DevLanguages },
		AlreadyInstalled: func(c *config.AlreadyInstalledConfig) []string { return c.DevLanguages },
	},
	{
		Key:              "databases",
		Label:            "Databases",
		Installed:        func(c *config.InstalledConfig) []string { return c.Databases },
		AlreadyInstalled: func(c *config.AlreadyInstalledConfig) []string { return c.Databases },
	},
}

func validCategoryKeys() []string {
	keys := make([]string, len(categoryDefs))
	for i, def := range categoryDefs {
		keys[i] = def.Key
	}
	return keys
}

func isValidCategory(category string) bool {
	for _, def := range categoryDefs {
		if def.Key == category {
			return true
		}
	}
	return false
}

// writeCategoryTables writes one tabwriter table per non-empty category (or
// just the matching one, if category is set). It returns true if anything
// was written.
func writeCategoryTables(
	buf *bytes.Buffer,
	heading string,
	items func(categoryDef) []string,
	category string,
) bool {
	wrote := false
	for _, def := range categoryDefs {
		if category != "" && def.Key != category {
			continue
		}
		names := items(def)
		if len(names) == 0 {
			continue
		}
		if !wrote && heading != "" {
			fmt.Fprintln(buf, heading)
			fmt.Fprintln(buf)
		}
		fmt.Fprintf(buf, "%s:\n", def.Label)
		w := tabwriter.NewWriter(buf, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME")
		for _, name := range names {
			fmt.Fprintf(w, "%s\n", name)
		}
		_ = w.Flush()
		fmt.Fprintln(buf)
		wrote = true
	}
	return wrote
}

// formatInstalled renders gc.Installed and gc.AlreadyInstalled as tables
// grouped by category. category == "" means show all categories; otherwise
// it must be one of the valid yaml key names, or an error is returned.
func formatInstalled(gc *config.GlobalConfig, category string) (string, error) {
	if category != "" && !isValidCategory(category) {
		return "", fmt.Errorf(
			"invalid category %q: valid categories are %s",
			category, strings.Join(validCategoryKeys(), ", "),
		)
	}

	var buf bytes.Buffer
	installedWrote := writeCategoryTables(&buf, "", func(d categoryDef) []string {
		return d.Installed(&gc.Installed)
	}, category)
	alreadyWrote := writeCategoryTables(
		&buf,
		"Already on this machine (not installed by Devgeta):",
		func(d categoryDef) []string { return d.AlreadyInstalled(&gc.AlreadyInstalled) },
		category,
	)

	if !installedWrote && !alreadyWrote {
		return "Nothing installed yet. Run `dg install` to get started.\n", nil
	}

	return buf.String(), nil
}

var (
	listCategoryFlag string
	listPlainFlag    bool
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"installed"},
	Short:   "View all items installed via Devgeta",
	Long: `View all items installed via Devgeta (alias: installed).

In a terminal, opens the interactive inventory dashboard grouped by category
with a live OK/MISSING/UNKNOWN status per item. Piped output, CI, or --plain
fall back to the plain-text table (reads ~/.config/devgeta/global_config.yaml
directly, with no live status check).

Examples:
  dg list                          # Interactive dashboard in a terminal
  dg list --plain                  # Force the plain-text table
  dg list --category=terminal_tools  # Show only one category
  dg installed                     # Same as 'dg list'`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if listCategoryFlag != "" && !isValidCategory(listCategoryFlag) {
			return fmt.Errorf(
				"invalid category %q: valid categories are %s",
				listCategoryFlag, strings.Join(validCategoryKeys(), ", "),
			)
		}

		gc := &config.GlobalConfig{}
		if err := gc.Load(); err != nil {
			return fmt.Errorf("failed to load global config: %w", err)
		}

		if !listPlainFlag && isInteractiveTerminal() {
			return tuiinventory.Run(gc, tuiinventory.Options{Category: listCategoryFlag})
		}

		out, err := formatInstalled(gc, listCategoryFlag)
		if err != nil {
			return err
		}

		fmt.Print(out)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)

	listCmd.Flags().StringVar(
		&listCategoryFlag,
		"category",
		"",
		fmt.Sprintf("Filter to a single category (%s)", strings.Join(validCategoryKeys(), ", ")),
	)
	listCmd.Flags().BoolVar(
		&listPlainFlag,
		"plain",
		false,
		"Force plain-text output even in a terminal",
	)
}
