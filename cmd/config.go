/*
 * Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/spf13/cobra"
)

// configPlainFlag is `dg config`/`dg config list`'s --plain flag.
var configPlainFlag bool

// loadConfigForRead loads global_config.yaml for the read-only subcommands
// (list, get), which must work on a fresh machine with no config file yet.
// Load() returns the raw os.ReadFile error in that case (see
// internal/config/fromFile.go), so os.IsNotExist distinguishes "nothing
// written yet" (fine - gc stays at its zero value, so every Setting.Get
// reports isSet=false and callers fall back to Default()) from a real
// read/parse failure (surfaced as an error). This mirrors
// loadWorktreeGlobalConfig's same distinction in cmd/worktree.go.
func loadConfigForRead() (*config.GlobalConfig, error) {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load global config: %w", err)
	}
	return gc, nil
}

// settingKeys returns every registered dotted key, in registry order, for
// unknownSettingError's "valid keys are" listing.
func settingKeys() []string {
	keys := make([]string, len(Settings))
	for i, s := range Settings {
		keys[i] = s.Key
	}
	return keys
}

// unknownSettingError is returned by get/set/unset for any key FindSetting
// doesn't resolve. That covers two cases identically: a plain typo, and a
// real GlobalConfig field that is devgeta-written state rather than a user
// preference (see cmd/config_settings_test.go's knownStateFields) -
// deliberately absent from Settings, and therefore indistinguishable from a
// typo from this command's point of view.
func unknownSettingError(key string) error {
	return fmt.Errorf(
		"unknown setting %q; valid keys are: %s",
		key, strings.Join(settingKeys(), ", "),
	)
}

// effectiveValue returns the value list/get/set/unset should treat as a
// setting's current effective value: its real value when set, otherwise
// whatever Setting.EffectiveDefault resolves to when unset - the one place
// that knows about a cross-setting fallback like worktree.default_layout
// falling back through worktree.default_ai (see EffectiveDefault's and
// Effective's doc comments in cmd/config_settings.go). Every subcommand goes
// through this same function, so none of them can disagree about what an
// unset key's effective value is - the bug this replaced was list's DEFAULT
// column computing this independently via the bare, context-free Default().
func effectiveValue(setting *Setting, gc *config.GlobalConfig) string {
	if value, isSet := setting.Get(gc); isSet {
		return value
	}
	return setting.EffectiveDefault(gc)
}

// settingsTable renders every registered setting as an aligned table: key,
// effective value (or "(default)" when unset), the live default actually in
// effect (via Setting.EffectiveDefault - the same path get/set/unset use for
// "what does this key resolve to when unset", so this column can never
// disagree with them the way it once did for worktree.default_layout), and
// its description. This is all of `dg config list`'s output - modeled on
// list.go's formatInstalled (tabwriter, one row per item).
func settingsTable(gc *config.GlobalConfig) string {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tVALUE\tDEFAULT\tDESCRIPTION")
	for i := range Settings {
		s := &Settings[i]
		value, isSet := s.Get(gc)
		display := value
		if !isSet {
			display = "(default)"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.Key, display, s.EffectiveDefault(gc), s.Description)
	}
	_ = w.Flush()
	return buf.String()
}

// runConfigList is both `dg config`'s default action and `dg config list`'s
// RunE. There is no interactive dashboard for settings (unlike `dg list`'s
// tuiinventory - out of scope per the cycle doc), so isInteractiveTerminal()
// and --plain don't choose between two renderers; they gate whether a
// one-line usage hint follows the table. A script piping the output (or
// passing --plain) gets exactly the table and nothing else, matching `dg
// list --plain`'s convention that plain output is safe for non-interactive
// consumption.
func runConfigList(cmd *cobra.Command) error {
	gc, err := loadConfigForRead()
	if err != nil {
		return err
	}

	fmt.Fprint(cmd.OutOrStdout(), settingsTable(gc))

	if !configPlainFlag && isInteractiveTerminal() {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(
			cmd.OutOrStdout(),
			"Run `dg config get <key>` to print one value, or `dg config set <key> <value>` to change one.",
		)
	}
	return nil
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View and change devgeta settings (worktree.*)",
	Long: `View and change user-settable devgeta configuration keys
(~/.config/devgeta/global_config.yaml) without hand-editing YAML.

Bare "dg config" (same as "dg config list") prints every setting: its key,
current value (or "(default)" if unset), live default, and description.

  dg config get <key>          Print only the effective value (for scripts)
  dg config set <key> <value>  Set a value (validated; worktree.search_paths
                                takes multiple values)
  dg config unset <key>        Clear a value back to its default

set/unset both work on a fresh machine with no config file yet - the file is
created on first use.`,
	Example: `  dg config
  dg config get worktree.scan_depth
  dg config set worktree.scan_depth 6
  dg config set worktree.search_paths ~/code ~/work
  dg config unset worktree.attach_after_create`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigList(cmd)
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List every devgeta setting: value, default, and description",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runConfigList(cmd)
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Print the effective value of one setting (bare value, for scripts)",
	Long: `Print the effective value of one setting and nothing else - no label, no
trailing decoration - so scripts can consume it directly.

For a setting that resolves through a fallback chain (worktree.default_layout
falling back to worktree.default_ai, then to the built-in default), this
prints the resolved, effective value, never the literal word "unset".`,
	Example: `  dg config get worktree.scan_depth
  dg config get worktree.default_layout`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		setting, ok := FindSetting(args[0])
		if !ok {
			return unknownSettingError(args[0])
		}

		gc, err := loadConfigForRead()
		if err != nil {
			return err
		}

		fmt.Fprintln(cmd.OutOrStdout(), effectiveValue(setting, gc))
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value...>",
	Short: "Set a devgeta setting (validated; nothing is written on an invalid value)",
	Long: `Validate and set a devgeta setting. An invalid value writes nothing and
exits non-zero with the validator's error - the same error a user would hit
triggering the same validation elsewhere (e.g. "dg wt create --ai <bad>").

Settings of kind stringlist (e.g. worktree.search_paths, review.reviewers)
accept multiple values; every other key accepts exactly one.

On success, prints the previous value (or its default) and the new value.`,
	Example: `  dg config set worktree.scan_depth 6
  dg config set worktree.search_paths ~/code ~/work
  dg config set worktree.attach_after_create false`,
	Args: cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, values := args[0], args[1:]
		setting, ok := FindSetting(key)
		if !ok {
			return unknownSettingError(key)
		}

		var previousDisplay, newDisplay string
		err := config.Update(func(gc *config.GlobalConfig) error {
			_, isSet := setting.Get(gc)
			previousDisplay = effectiveValue(setting, gc)
			if !isSet {
				previousDisplay += " (default)"
			}
			if err := setting.Set(gc, values); err != nil {
				return err
			}
			// A value that round-trips back to isSet=false (e.g.
			// worktree.scan_depth's 0, dropped by its yaml `omitempty` tag -
			// see docs/spec.md's "0 means use the default") was NOT persisted
			// as a new value, whatever the raw input said - Get is the
			// source of truth here, not the input. Saying so plainly (rather
			// than echoing the raw input as if it took effect) is the fix for
			// the bug where `set worktree.scan_depth 0` claimed "-> 0" while
			// `get` kept reporting the old default.
			if newValue, newIsSet := setting.Get(gc); newIsSet {
				newDisplay = newValue
			} else {
				newDisplay = fmt.Sprintf(
					"%s (default) [%s is equivalent to unset; use `dg config unset %s` or a non-default value]",
					effectiveValue(setting, gc),
					strings.Join(values, " "),
					key,
				)
			}
			return nil
		})
		if err != nil {
			return err
		}

		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s -> %s\n", key, previousDisplay, newDisplay)
		return nil
	},
}

var configUnsetCmd = &cobra.Command{
	Use:   "unset <key>",
	Short: "Clear a setting back to its default",
	Long: `Clear a setting back to its default and confirm which default now
applies.`,
	Example: `  dg config unset worktree.scan_depth`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		setting, ok := FindSetting(key)
		if !ok {
			return unknownSettingError(key)
		}

		// The confirmation must name the value actually in effect after
		// unsetting, computed from the just-mutated gc via the same
		// effectiveValue used by get - not the static setting.Default(),
		// which for worktree.default_layout would misreport "opencode" even
		// when worktree.default_ai is set to something else (see
		// effectiveValue's comment).
		var confirmedDefault string
		if err := config.Update(func(gc *config.GlobalConfig) error {
			setting.Unset(gc)
			confirmedDefault = effectiveValue(setting, gc)
			return nil
		}); err != nil {
			return err
		}

		fmt.Fprintf(
			cmd.OutOrStdout(),
			"%s: now using default (%s)\n", key, confirmedDefault,
		)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	// Same generic override taskCmd uses (see standardHelpFunc's doc comment
	// in cmd/task.go) - without it, config's subcommands would be hidden
	// behind the root's branded Use+Long-only help.
	configCmd.SetHelpFunc(standardHelpFunc)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configUnsetCmd)

	// Persistent so both `dg config --plain` and `dg config list --plain`
	// work without registering the flag twice.
	configCmd.PersistentFlags().BoolVar(
		&configPlainFlag,
		"plain",
		false,
		"Suppress the interactive hint line after the settings table",
	)
}
