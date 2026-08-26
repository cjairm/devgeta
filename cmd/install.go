/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/cjairm/devgeta/internal/apps/devgeta"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/tooling/aitools"
	"github.com/cjairm/devgeta/internal/tooling/databases"
	"github.com/cjairm/devgeta/internal/tooling/desktop"
	"github.com/cjairm/devgeta/internal/tooling/languages"
	"github.com/cjairm/devgeta/internal/tooling/terminal"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/utils"
	"github.com/spf13/cobra"
)

var (
	only []string
	skip []string
)

// knownCategories are the install categories accepted by --only/--skip.
var knownCategories = []string{"terminal", "languages", "databases", "desktop", "ai-tools"}

// appToCoordinator maps each registry app to the coordinator that installs it.
// alacritty has KindTerminal but is installed by the desktop coordinator.
// devgeta ("") is never included in dg install.
var appToCoordinator = map[string]string{
	"claude":     "terminal",
	"fastfetch":  "terminal",
	"git":        "terminal",
	"lazydocker": "terminal",
	"lazygit":    "terminal",
	"rtk":        "ai-tools",
	"mise":       "terminal",
	"neovim":     "terminal",
	"opencode":   "terminal",
	"tmux":       "terminal",
	"alacritty":  "desktop",
	"aerospace":  "desktop",
	"brave":      "desktop",
	"docker":     "desktop",
	"flameshot":  "desktop",
	"gimp":       "desktop",
	"i3":         "desktop",
	"raycast":    "desktop",
	"ulauncher":  "desktop",
	"devgeta":    "",
}

// installConfig holds the resolved plan for a single dg install run.
type installConfig struct {
	runTerminal  bool
	runLanguages bool
	runDatabases bool
	runDesktop   bool
	runAITools   bool
	// Per-coordinator filters (nil = no filter, non-nil = only these apps)
	terminalAppFilter  map[string]bool
	terminalSkipFilter map[string]bool
	desktopAppFilter   map[string]bool
	desktopSkipFilter  map[string]bool
	aiToolsAppFilter   map[string]bool
	aiToolsSkipFilter  map[string]bool
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install devgeta and all required tools",
	Long: `Installs the devgeta platform and sets up your development environment.

This command performs the following steps:
  - Validates your OS version
  - Installs the package manager (Homebrew on macOS, apt on Debian/Ubuntu)
  - Extracts embedded configuration templates
  - Installs terminal tools, programming languages, databases, and AI tools
  - Optionally installs desktop applications and shell configuration

Supported platforms:
  - macOS 13+ (Ventura) via Homebrew
  - Debian 12+ (Bookworm) / Ubuntu 24+ via apt

Flags:
  --only <...>     Only install specific categories or apps (e.g., terminal, neovim)
  --skip <...>     Skip specific categories or apps (e.g., databases, git)

Per-app targeting (registry apps only):
  dg install --only neovim            # install only neovim
  dg install --skip git               # install everything except git
  dg install --only terminal --skip lazygit  # full terminal minus lazygit
`,
	RunE: run,
}

func init() {
	rootCmd.AddCommand(installCmd)

	installCmd.Flags().
		StringSliceVar(&only, "only", []string{}, "Only install specific categories or apps (comma-separated or repeatable)")
	installCmd.Flags().
		StringSliceVar(&skip, "skip", []string{}, "Skip specific categories or apps (comma-separated or repeatable)")
}

func run(cmd *cobra.Command, args []string) error {
	cfg, err := parseInstallFlags(only, skip)
	if err != nil {
		return err
	}

	logger.L().Debugw("install config", "cfg", cfg, "verbose", verbose)

	utils.PrintBold(constants.Devgeta)
	utils.Print("=> Begin installation (or abort with ctrl+c)...", "")
	utils.Print("===============================================", "")

	ctx := context.Background()
	osCmd := commands.NewCommand()

	utils.PrintInfo("Validating version...")
	if err := osCmd.ValidateOSVersion(); err != nil {
		return err
	}

	utils.PrintInfo("Installing package manager...")
	if err := osCmd.MaybeInstallPackageManager(); err != nil {
		return err
	}

	installDevgeta()

	if stopIfInterrupted("terminal tools") {
		return nil
	}
	if cfg.runTerminal {
		installTerminalTools(cfg.terminalAppFilter, cfg.terminalSkipFilter)
	} else {
		utils.PrintInfo("Skipping terminal tools installation")
	}

	if stopIfInterrupted("languages") {
		return nil
	}
	if cfg.runLanguages {
		installLanguages(ctx)
	} else {
		utils.PrintInfo("Skipping development languages installation")
	}

	if stopIfInterrupted("databases") {
		return nil
	}
	if cfg.runDatabases {
		installDatabases(ctx)
	} else {
		utils.PrintInfo("Skipping databases installation")
	}

	if stopIfInterrupted("desktop applications") {
		return nil
	}
	if cfg.runDesktop {
		installDesktopTools(cfg.desktopAppFilter, cfg.desktopSkipFilter)
	} else {
		utils.PrintInfo("Skipping desktop applications installation")
	}

	if stopIfInterrupted("AI tools") {
		return nil
	}
	if cfg.runAITools {
		installAITools(cfg.aiToolsAppFilter, cfg.aiToolsSkipFilter)
	} else {
		utils.PrintInfo("Skipping AI tools installation")
	}

	return nil
}

// stopIfInterrupted reports whether the process-level root context is
// already cancelled (commands.Interrupted) — a Ctrl-C or SIGTERM that fired
// during an earlier category. When it has, it prints one calm message naming
// the category run() was about to start and returns true so run() can skip
// the rest of the install instead of starting a whole new category that
// would just grind through its own loop hitting the same cancelled context.
func stopIfInterrupted(nextCategory string) bool {
	if !commands.Interrupted() {
		return false
	}
	utils.PrintWarning(fmt.Sprintf("Installation interrupted; stopping before %s.", nextCategory))
	return true
}

// parseInstallFlags splits --only/--skip into category and app sets, validates them,
// and returns a resolved installConfig describing what should run and with what filters.
func parseInstallFlags(onlyFlags, skipFlags []string) (*installConfig, error) {
	onlyCategorySet := make(map[string]bool)
	onlyAppSet := make(map[string]bool)
	skipCategorySet := make(map[string]bool)
	skipAppSet := make(map[string]bool)

	for _, item := range onlyFlags {
		switch {
		case isKnownCategory(item):
			onlyCategorySet[item] = true
		case isKnownApp(item):
			onlyAppSet[item] = true
		default:
			return nil, fmt.Errorf(
				"unknown value %q for --only\n\nValid categories: %s\nValid apps: %s",
				item,
				strings.Join(knownCategories, ", "),
				formatAppNames(),
			)
		}
	}

	for _, item := range skipFlags {
		switch {
		case isKnownCategory(item):
			skipCategorySet[item] = true
		case isKnownApp(item):
			skipAppSet[item] = true
		default:
			return nil, fmt.Errorf(
				"unknown value %q for --skip\n\nValid categories: %s\nValid apps: %s",
				item,
				strings.Join(knownCategories, ", "),
				formatAppNames(),
			)
		}
	}

	hasAnyOnly := len(onlyCategorySet) > 0 || len(onlyAppSet) > 0

	terminalAppFilter := buildAppFilter("terminal", onlyCategorySet, onlyAppSet, skipAppSet)
	desktopAppFilter := buildAppFilter("desktop", onlyCategorySet, onlyAppSet, skipAppSet)
	aiToolsAppFilter := buildAppFilter("ai-tools", onlyCategorySet, onlyAppSet, skipAppSet)
	terminalSkipFilter := buildSkipFilter("terminal", skipAppSet)
	desktopSkipFilter := buildSkipFilter("desktop", skipAppSet)
	aiToolsSkipFilter := buildSkipFilter("ai-tools", skipAppSet)

	hasTerminalApps := hasAppsForCoordinator("terminal", onlyAppSet)
	hasDesktopApps := hasAppsForCoordinator("desktop", onlyAppSet)
	hasAIToolsApps := hasAppsForCoordinator("ai-tools", onlyAppSet)

	return &installConfig{
		runTerminal: shouldRunCategory(
			"terminal",
			onlyCategorySet,
			skipCategorySet,
			hasAnyOnly,
			hasTerminalApps,
		),
		runLanguages: shouldRunCategory(
			"languages",
			onlyCategorySet,
			skipCategorySet,
			hasAnyOnly,
			false,
		),
		runDatabases: shouldRunCategory(
			"databases",
			onlyCategorySet,
			skipCategorySet,
			hasAnyOnly,
			false,
		),
		runDesktop: shouldRunCategory(
			"desktop",
			onlyCategorySet,
			skipCategorySet,
			hasAnyOnly,
			hasDesktopApps,
		),
		runAITools: shouldRunCategory(
			"ai-tools",
			onlyCategorySet,
			skipCategorySet,
			hasAnyOnly,
			hasAIToolsApps,
		),
		terminalAppFilter:  terminalAppFilter,
		terminalSkipFilter: terminalSkipFilter,
		desktopAppFilter:   desktopAppFilter,
		desktopSkipFilter:  desktopSkipFilter,
		aiToolsAppFilter:   aiToolsAppFilter,
		aiToolsSkipFilter:  aiToolsSkipFilter,
	}, nil
}

// shouldRunCategory returns true if the given category should execute.
func shouldRunCategory(
	category string,
	onlyCategorySet, skipCategorySet map[string]bool,
	hasAnyOnly, hasAppsForCategory bool,
) bool {
	if skipCategorySet[category] {
		return false
	}
	if !hasAnyOnly {
		return true
	}
	if onlyCategorySet[category] {
		return true
	}
	return hasAppsForCategory
}

// buildAppFilter returns the app-level only-filter for a coordinator.
// Returns nil when the category is explicitly selected (full install) or no apps belong to it.
func buildAppFilter(
	coordinator string,
	onlyCategorySet, onlyAppSet, skipAppSet map[string]bool,
) map[string]bool {
	if onlyCategorySet[coordinator] {
		return nil
	}
	var filter map[string]bool
	for appName, coord := range appToCoordinator {
		if coord == coordinator && onlyAppSet[appName] && !skipAppSet[appName] {
			if filter == nil {
				filter = make(map[string]bool)
			}
			filter[appName] = true
		}
	}
	return filter
}

// buildSkipFilter returns the app-level skip-filter for a coordinator.
func buildSkipFilter(coordinator string, skipAppSet map[string]bool) map[string]bool {
	var filter map[string]bool
	for appName, coord := range appToCoordinator {
		if coord == coordinator && skipAppSet[appName] {
			if filter == nil {
				filter = make(map[string]bool)
			}
			filter[appName] = true
		}
	}
	return filter
}

// hasAppsForCoordinator reports whether onlyAppSet contains any app belonging to coordinator.
func hasAppsForCoordinator(coordinator string, onlyAppSet map[string]bool) bool {
	for appName := range onlyAppSet {
		if appToCoordinator[appName] == coordinator {
			return true
		}
	}
	return false
}

func isKnownCategory(s string) bool {
	for _, c := range knownCategories {
		if c == s {
			return true
		}
	}
	return false
}

func isKnownApp(s string) bool {
	coord, ok := appToCoordinator[s]
	return ok && coord != ""
}

// formatAppNames returns a sorted, comma-joined list of all targetable app names.
func formatAppNames() string {
	targetable := make([]string, 0, len(appToCoordinator))
	for name, coord := range appToCoordinator {
		if coord != "" {
			targetable = append(targetable, name)
		}
	}
	// sort for deterministic output
	for i := 0; i < len(targetable); i++ {
		for j := i + 1; j < len(targetable); j++ {
			if targetable[i] > targetable[j] {
				targetable[i], targetable[j] = targetable[j], targetable[i]
			}
		}
	}
	return strings.Join(targetable, ", ")
}

func installDevgeta() {
	dg := devgeta.New()
	utils.PrintInfo("Installing & configuring devgeta app")
	utils.MaybeExitWithError(dg.SoftInstall())
	utils.MaybeExitWithError(dg.SoftConfigure())
}

func installTerminalTools(appFilter, skipFilter map[string]bool) {
	t := terminal.New()
	t.InstallAndConfigure(appFilter, skipFilter)
}

func installLanguages(ctx context.Context) {
	l := languages.New()
	ctx, err := l.ChooseLanguages(ctx)
	utils.MaybeExitWithError(err)
	l.InstallChosen(ctx)
}

func installDatabases(ctx context.Context) {
	d := databases.New()
	ctx, err := d.ChooseDatabases(ctx)
	utils.MaybeExitWithError(err)
	d.InstallChosen(ctx)
}

func installDesktopTools(appFilter, skipFilter map[string]bool) {
	desktopTool := desktop.New()
	desktopTool.InstallAndConfigure(appFilter, skipFilter)
}

func installAITools(appFilter, skipFilter map[string]bool) {
	a := aitools.New()
	a.InstallAndConfigure(appFilter, skipFilter)
}
