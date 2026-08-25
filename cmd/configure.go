/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/devgeta"
	"github.com/cjairm/devgeta/internal/apps/registry"
	"github.com/cjairm/devgeta/pkg/utils"
	"github.com/spf13/cobra"
)

var (
	configureForce bool
	configureOnly  []string
)

// getAppFn is the registry lookup; overridden in tests.
var getAppFn = func(name string) (apps.App, error) {
	return registry.GetApp(name)
}

// refreshEmbeddedConfigs re-extracts embedded configs so templates match the
// running binary. It is a no-op when the published tree already belongs to this
// build, so configuring one app no longer rewrites the whole tree first;
// --force still re-extracts unconditionally via the app's ForceConfigure.
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

The AI coders (claude, opencode) expose discrete, separately-refreshable parts
via --only (must be combined with --force): the shared config subtrees
(skills, commands, agents) overwrite only those folders, leaving settings,
themes, and other config you may have edited untouched; the rtk part runs
"rtk init" to wire rtk's command-rewriting hook into that AI coder — the
explicit opt-in required by ADR-0004, recorded so the hook survives future
--force re-renders of claude's settings.json.

Examples:
  dg configure git                              # Apply git config if not already present
  dg configure neovim --force                   # Overwrite existing neovim config
  dg configure claude --force --only=skills     # Refresh only the skills folder
  dg configure opencode --force --only=skills,commands
  dg configure claude --force --only=rtk        # Opt into rtk's hook for Claude Code
  dg configure opencode --force --only=rtk      # Install rtk's OpenCode plugin
`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigure,
}

func init() {
	rootCmd.AddCommand(configureCmd)

	configureCmd.Flags().
		BoolVar(&configureForce, "force", false, "Overwrite existing configuration files")
	configureCmd.Flags().
		StringSliceVar(&configureOnly, "only", nil, "Refresh only these app-defined config parts (claude/opencode: skills,commands,agents,rtk); requires --force")
}

func runConfigure(cmd *cobra.Command, args []string) error {
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

	// --only refreshes just the named shared subtrees. It's overwrite-only (so
	// it never silently no-ops in soft mode) and limited to apps that expose
	// separately-refreshable parts (the AI coders).
	if len(configureOnly) > 0 {
		if !configureForce {
			return fmt.Errorf("--only requires --force (it overwrites the named config folders)")
		}
		sc, ok := app.(apps.SelectiveConfigurer)
		if !ok {
			return fmt.Errorf("--only is not supported for %s", appName)
		}
		allowed := sc.ConfigurableParts()
		for _, part := range configureOnly {
			if !slices.Contains(allowed, part) {
				return fmt.Errorf(
					"unknown --only value %q for %s (valid: %s)",
					part, appName, strings.Join(allowed, ", "),
				)
			}
		}
		if err := sc.ForceConfigureParts(configureOnly); err != nil {
			return err
		}
		utils.PrintSuccess(
			fmt.Sprintf("configured %s (%s)", appName, strings.Join(configureOnly, ", ")),
		)
		return nil
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
