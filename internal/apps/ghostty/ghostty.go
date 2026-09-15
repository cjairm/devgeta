// Package ghostty provides installation and configuration management for the
// Ghostty terminal emulator. Ghostty is a fast, GPU-accelerated terminal that
// devgeta offers as a platform-filtered alternative to Alacritty (ADR-0037).
// This module follows the standardized devgeta app interface for consistent
// lifecycle management.

package ghostty

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

// ghosttyConfigFileName is what Ghostty actually reads: the file is named
// `config`, with no extension, inside its config directory.
const ghosttyConfigFileName = "config"

var (
	_ apps.App              = (*Ghostty)(nil)
	_ apps.ThemedConfigurer = (*Ghostty)(nil)
)

type Ghostty struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func (g *Ghostty) Name() string       { return constants.Ghostty }
func (g *Ghostty) Kind() apps.AppKind { return apps.KindTerminal }

func New() *Ghostty {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Ghostty{Cmd: osCmd, Base: baseCmd}
}

func (g *Ghostty) Install() error {
	if g.Base.IsMac() {
		return g.Cmd.InstallDesktopApp(constants.Ghostty)
	}
	return g.Cmd.InstallPackage(constants.Ghostty)
}

func (g *Ghostty) ForceInstall() error {
	return baseapp.Reinstall(g.Install, g.Uninstall)
}

func (g *Ghostty) SoftInstall() error {
	if g.Base.IsMac() {
		return g.Cmd.MaybeInstallDesktopApp(constants.Ghostty)
	}
	return g.Cmd.MaybeInstallPackage(constants.Ghostty)
}

// itemType returns what Install/SoftInstall tracked Ghostty under in the
// global config on this platform: "desktop_app" for the macOS cask,
// "package" for the apt package on Linux (see registry.Meta's AltItemType
// comment on ghostty). ForceConfigure, SoftConfigure and Uninstall all need
// to agree with Install/SoftInstall on this, or dg uninstall / a second
// configure silently checks the wrong tracking list.
func (g *Ghostty) itemType() string {
	if g.Base.IsMac() {
		return "desktop_app"
	}
	return "package"
}

// ForceConfigure resolves the theme for current_theme (falling back to
// theme.DefaultThemeName when it is empty) and renders with it. `dg theme
// set` does not go through here: it resolves the target theme itself and
// calls ForceConfigureTheme directly, because current_theme is deliberately
// not written yet at that point (docs/plans/cycles/2026-09-14-dg-theme.md
// Step 5) - reading it here would render the theme being replaced.
func (g *Ghostty) ForceConfigure() error {
	def, err := theme.CurrentDefinition()
	if err != nil {
		return fmt.Errorf("failed to resolve current theme: %w", err)
	}
	return g.ForceConfigureTheme(def)
}

func (g *Ghostty) ForceConfigureTheme(def theme.Definition) error {
	p, err := def.PaletteFor(g.Name())
	if err != nil {
		return fmt.Errorf("failed to resolve theme palette: %w", err)
	}
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	font := "default"
	configFilePath := filepath.Join(paths.Paths.Config.Ghostty, ghosttyConfigFileName)
	tmplPath := filepath.Join(paths.Paths.App.Configs.Ghostty, "ghostty.conf.tmpl")
	if err := files.GenerateFromTemplate(tmplPath, configFilePath, map[string]any{
		"Font":       font,
		"ConfigPath": paths.Paths.Config.Root,
		"Palette":    p,
	}); err != nil {
		return fmt.Errorf("failed to generate ghostty configuration: %w", err)
	}
	starterDst := filepath.Join(paths.Paths.Config.Ghostty, "starter.sh")
	if err := files.CopyFile(
		filepath.Join(paths.Paths.App.Configs.Terminal, "starter.sh"),
		starterDst,
	); err != nil {
		return fmt.Errorf("failed to copy ghostty starter script: %w", err)
	}
	// The `command` setting in the template execs this script. Both the
	// embedded-config extractor and files.CopyFile write 0644, so it has to be
	// made executable here or Ghostty cannot launch it and comes up without
	// tmux. See files.CopyFile's doc for the established pattern.
	if err := os.Chmod(starterDst, 0o755); err != nil {
		return fmt.Errorf("failed to make ghostty starter script executable: %w", err)
	}
	gc.AddToInstalled(constants.Ghostty, g.itemType())
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

// SoftConfigure deploys the config only when there isn't one already, so a
// hand-edited config is never overwritten.
//
// The guard is the config file itself, not the global config's installed
// tracking. Tracking is set by SoftInstall moments earlier — the desktop
// coordinator runs SoftInstall then SoftConfigure back to back — so reading it
// here would mean the config is never written on the one run that installed
// Ghostty, leaving a fresh install with default colors, no blur, a native
// titlebar and no tmux. This matches aerospace, tmux and neovim, which have
// always guarded on their own config file.
func (g *Ghostty) SoftConfigure() error {
	if files.FileAlreadyExist(filepath.Join(paths.Paths.Config.Ghostty, ghosttyConfigFileName)) {
		return nil
	}
	if err := g.ForceConfigure(); err != nil {
		return fmt.Errorf("failed to configure ghostty: %w", err)
	}
	return nil
}

// Uninstall removes Ghostty.
func (g *Ghostty) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if g.Base.IsMac() {
		if err := g.Cmd.UninstallDesktopApp(constants.Ghostty); err != nil {
			return fmt.Errorf("failed to uninstall ghostty: %w", err)
		}
	} else {
		if err := g.Cmd.UninstallPackage(constants.Ghostty); err != nil {
			return fmt.Errorf("failed to uninstall ghostty: %w", err)
		}
	}
	_ = os.RemoveAll(paths.Paths.Config.Ghostty)
	gc.RemoveFromInstalled(constants.Ghostty, g.itemType())
	return gc.Save()
}

func (g *Ghostty) ExecuteCommand(args ...string) error {
	return apps.ErrExecuteNotSupported
}

func (g *Ghostty) Update() error {
	return apps.ErrUpdateNotSupported
}
