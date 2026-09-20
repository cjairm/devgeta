/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"fmt"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/registry"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/utils"
	"github.com/spf13/cobra"
)

// themedApps is the fixed, ordered list of the seven surfaces with a theme
// (ADR-0043). It is not derivable from apps.AppKind: Alacritty and i3 are
// KindDesktop, tmux and Neovim are KindTerminal, and plenty of apps in both
// kinds have no theme at all. Ordered, not a set, so a test can place one
// surface before another it rigs to fail. This is also theme.Manifest()'s
// app order — the two must stay in step, since `set` looks up each
// manifest entry by the app name it is currently configuring.
var themedApps = []string{
	constants.Alacritty,
	constants.Ghostty,
	constants.OpenCode,
	constants.Tmux,
	constants.Neovim,
	constants.Claude,
	constants.I3,
}

// themeGetAppFn is the registry lookup for theme commands; overridden in
// tests, same pattern as getAppFn in configure.go.
var themeGetAppFn = func(name string) (apps.App, error) {
	return registry.GetApp(name)
}

// recoverInterruptedFn sweeps for a crashed `dg theme set`'s leftover
// backups before this command does its own filesystem work — see
// docs/plans/cycles/2026-09-14-dg-theme.md's Step 5 for why `dg theme set`,
// `dg configure` and `dg install` all call it, and why refusing to run
// instead was rejected. A package-level var, like getAppFn and
// refreshEmbeddedConfigs, so a test can spy on it or stub it.
var recoverInterruptedFn = theme.RecoverInterrupted

// themeWallpaperSetterFn constructs the platform wallpaper setter; a
// package-level var, like getAppFn and recoverInterruptedFn, so a test can
// stub it instead of exercising a real osascript/feh call.
var themeWallpaperSetterFn = theme.NewWallpaperSetter

var themeCmd = &cobra.Command{
	Use:   "theme",
	Short: "Show, list, or switch the active theme",
	Long: `Show the current theme and what else is available.

Examples:
  dg theme                  # show the current theme and what's available
  dg theme list             # list available themes
  dg theme set tokyonight   # switch every installed themed app to tokyonight
`,
	RunE: runThemeShow,
}

var themeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available themes",
	RunE:  runThemeList,
}

var themeSetCmd = &cobra.Command{
	Use:   "set <name>",
	Short: "Switch every installed themed app to <name>",
	Long: `Switches Alacritty, Ghostty, tmux, Neovim, OpenCode, Claude and i3 to the
named theme together, skipping any that are not installed.

The switch is transactional: <name> is validated and every app configured
before current_theme is persisted, and any failure rolls the whole machine
back to the theme it had before. current_theme is not readable or writable
through "dg config" — it names deploy state, not a preference, so this
command is the only way to change it.`,
	Args: cobra.ExactArgs(1),
	RunE: runThemeSet,
}

var themeSetWallpaperCmd = &cobra.Command{
	Use:   "set-wallpaper <path>",
	Short: "Set the current theme's wallpaper image",
	Long: `Copies <path> into ~/.config/devgeta/wallpapers/ under a content-addressed
name and records it against the current theme. Applying it to the desktop
is best-effort (macOS via AppleScript, Linux/i3 via feh) and never fails
this command; "dg theme set" re-applies it whenever you switch back to this
theme, and leaves the desktop untouched for a theme with no wallpaper
recorded.`,
	Args: cobra.ExactArgs(1),
	RunE: runThemeSetWallpaper,
}

func init() {
	rootCmd.AddCommand(themeCmd)
	themeCmd.AddCommand(themeListCmd)
	themeCmd.AddCommand(themeSetCmd)
	themeCmd.AddCommand(themeSetWallpaperCmd)
}

func currentThemeName(gc *config.GlobalConfig) string {
	if gc.CurrentTheme == "" {
		return theme.DefaultThemeName
	}
	return gc.CurrentTheme
}

func runThemeShow(_ *cobra.Command, _ []string) error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	names, err := theme.List()
	if err != nil {
		return err
	}
	utils.PrintInfo(fmt.Sprintf("current theme: %s", currentThemeName(gc)))
	utils.PrintInfo(fmt.Sprintf("available: %s", strings.Join(names, ", ")))
	return nil
}

func runThemeList(_ *cobra.Command, _ []string) error {
	names, err := theme.List()
	if err != nil {
		return err
	}
	for _, n := range names {
		utils.Print(n, "")
	}
	return nil
}

// isThemedAppInstalled mirrors cmd/uninstall.go's installed check
// (registry.Meta's ItemType, falling back to AltItemType) and additionally
// accepts gc.IsAlreadyInstalled: an app the user installed themselves is
// still one devgeta configures (cycle doc Step 5).
func isThemedAppInstalled(gc *config.GlobalConfig, name string) bool {
	meta := registry.Meta[name]
	if gc.IsInstalledByDevgeta(name, meta.ItemType) || gc.IsAlreadyInstalled(name, meta.ItemType) {
		return true
	}
	if meta.AltItemType != "" {
		if gc.IsInstalledByDevgeta(name, meta.AltItemType) ||
			gc.IsAlreadyInstalled(name, meta.AltItemType) {
			return true
		}
	}
	return false
}

// rollbackThemeSwitch restores every manifest path in backedUp (in the
// state backupThemeSwitch left them: a rename-aside or an absent marker)
// and clears pending_theme. Used both when an app's ForceConfigureTheme
// fails and when the final commit itself fails — in both cases nothing may
// be left claiming the new theme.
func rollbackThemeSwitch(backedUp []string) {
	for _, p := range backedUp {
		if err := theme.RestorePath(p); err != nil {
			logger.L().Errorw("failed to restore theme-switch backup", "path", p, "error", err)
		}
	}
	if err := config.Update(func(gc *config.GlobalConfig) error {
		gc.PendingTheme = ""
		return nil
	}); err != nil {
		logger.L().Errorw("failed to clear pending_theme after rollback", "error", err)
	}
}

func runThemeSet(_ *cobra.Command, args []string) error {
	name := args[0]

	if msg, err := recoverInterruptedFn(); err != nil {
		return fmt.Errorf("failed to recover an interrupted theme switch: %w", err)
	} else if msg != "" {
		utils.PrintInfo(msg)
	}

	// Republish the embedded configs when they don't belong to this build,
	// exactly as cmd/configure.go does and for the same reason:
	// paths.Paths.App.Configs is a pointer to a build-stamped directory, so
	// an upgraded binary otherwise reads the previous build's tree - which
	// for the build that introduced themes has no themes/ directory at all,
	// and for any later one can hold stale templates. This is a no-op once
	// the tree already matches.
	if err := refreshEmbeddedConfigs(); err != nil {
		return err
	}

	// Validate before touching anything (cycle doc Step 5, order point 1).
	def, err := theme.Load(name)
	if err != nil {
		return fmt.Errorf("theme %q is not valid: %w", name, err)
	}

	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}

	var targeted []string
	for _, appName := range themedApps {
		if isThemedAppInstalled(gc, appName) {
			targeted = append(targeted, appName)
			continue
		}
		utils.PrintInfo(fmt.Sprintf("skipping %s: not installed", appName))
	}

	// Record that a switch is in progress, before the first backup exists
	// (order point 2) — RecoverInterrupted's only signal that a dead attempt
	// never committed.
	if err := config.Update(func(g *config.GlobalConfig) error {
		g.PendingTheme = name
		return nil
	}); err != nil {
		return fmt.Errorf("failed to record pending theme switch: %w", err)
	}

	manifestByApp := make(map[string][]string, len(themedApps))
	for _, e := range theme.Manifest() {
		manifestByApp[e.App] = e.Paths
	}

	var backedUp []string
	var liveAppliers []apps.LiveThemeApplier

	for _, appName := range targeted {
		app, err := themeGetAppFn(appName)
		if err != nil {
			rollbackThemeSwitch(backedUp)
			return fmt.Errorf("failed to get app %s: %w", appName, err)
		}
		tc, ok := app.(apps.ThemedConfigurer)
		if !ok {
			rollbackThemeSwitch(backedUp)
			return fmt.Errorf("%s does not support theming", appName)
		}

		for _, p := range manifestByApp[appName] {
			if err := theme.BackupPath(p); err != nil {
				rollbackThemeSwitch(backedUp)
				return fmt.Errorf("failed to back up %s: %w", p, err)
			}
			backedUp = append(backedUp, p)
		}

		if err := tc.ForceConfigureTheme(def); err != nil {
			rollbackThemeSwitch(backedUp)
			return fmt.Errorf("failed to configure %s: %w", appName, err)
		}

		if la, ok := app.(apps.LiveThemeApplier); ok {
			liveAppliers = append(liveAppliers, la)
		}
	}

	// current_theme and pending_theme change together in the one write that
	// commits the switch (order point 5) — nothing on disk names the new
	// theme until every app above has already succeeded.
	if err := config.Update(func(g *config.GlobalConfig) error {
		g.CurrentTheme = name
		g.PendingTheme = ""
		return nil
	}); err != nil {
		rollbackThemeSwitch(backedUp)
		return fmt.Errorf("failed to persist current theme: %w", err)
	}

	for _, p := range backedUp {
		if err := theme.DiscardBackup(p); err != nil {
			logger.L().Warnw("failed to remove theme-switch backup", "path", p, "error", err)
		}
	}

	// Live push only after commit — it cannot be rolled back, so it must
	// never run while a failure could still undo the switch (order point 7).
	for _, la := range liveAppliers {
		if err := la.ApplyLiveTheme(); err != nil {
			logger.L().Warnw("failed to apply live theme", "error", err)
		}
	}

	// Wallpaper is outside the transaction and best-effort (ADR-0044 point
	// 4): applied only after everything above has committed, and a failure
	// here warns rather than failing an otherwise-successful switch. The
	// durable wallpapers map (populated by `dg theme set-wallpaper`) wins
	// over the theme file's own declared default (`def.Wallpaper`, which
	// every shipped theme leaves empty); a theme with neither leaves the
	// desktop untouched.
	wp := gc.Wallpapers[name]
	if wp == "" {
		wp = def.Wallpaper
	}
	if wp != "" {
		setter := themeWallpaperSetterFn(commands.NewBaseCommand())
		if err := setter.SetWallpaper(wp); err != nil {
			logger.L().Warnw("failed to apply wallpaper", "theme", name, "error", err)
		}
	}

	utils.PrintSuccess(fmt.Sprintf("switched to theme %s", name))
	return nil
}

// runThemeSetWallpaper copies the given image into WallpapersDir() under a
// content-addressed name and records it against the current theme
// (cycle doc Step 9). It does not apply the image to the desktop itself -
// that happens the next time `dg theme set` runs (including immediately
// after, if the caller wants it live right away).
func runThemeSetWallpaper(_ *cobra.Command, args []string) error {
	srcPath := args[0]

	if msg, err := recoverInterruptedFn(); err != nil {
		return fmt.Errorf("failed to recover an interrupted theme switch: %w", err)
	} else if msg != "" {
		utils.PrintInfo(msg)
	}

	dest, err := theme.CopyWallpaper(srcPath)
	if err != nil {
		return fmt.Errorf("failed to set wallpaper: %w", err)
	}

	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	name := currentThemeName(gc)

	if err := config.Update(func(g *config.GlobalConfig) error {
		if g.Wallpapers == nil {
			g.Wallpapers = map[string]string{}
		}
		g.Wallpapers[name] = dest
		return nil
	}); err != nil {
		return fmt.Errorf("failed to persist wallpaper: %w", err)
	}

	// Best-effort: the recorded path is already correct even if the sweep
	// fails, so a failure here warns rather than failing the command.
	gcAfter := &config.GlobalConfig{}
	if err := gcAfter.Load(); err != nil {
		logger.L().Warnw("failed to reload config for wallpaper sweep", "error", err)
	} else if err := theme.SweepOrphanedWallpapers(gcAfter); err != nil {
		logger.L().Warnw("failed to sweep orphaned wallpapers", "error", err)
	}

	utils.PrintSuccess(fmt.Sprintf("set wallpaper for theme %s", name))
	return nil
}
