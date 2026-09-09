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
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

var _ apps.App = (*Ghostty)(nil)

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

func (g *Ghostty) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	font := "default"
	theme := "default"
	configFilePath := filepath.Join(paths.Paths.Config.Ghostty, "config")
	tmplPath := filepath.Join(paths.Paths.App.Configs.Ghostty, "ghostty.conf.tmpl")
	if err := files.GenerateFromTemplate(tmplPath, configFilePath, map[string]string{
		"Font":       font,
		"Theme":      theme,
		"ConfigPath": paths.Paths.Config.Root,
	}); err != nil {
		return fmt.Errorf("failed to generate ghostty configuration: %w", err)
	}
	if err := files.CopyFile(
		filepath.Join(paths.Paths.App.Configs.Terminal, "starter.sh"),
		filepath.Join(paths.Paths.Config.Ghostty, "starter.sh"),
	); err != nil {
		return fmt.Errorf("failed to copy ghostty starter script: %w", err)
	}
	gc.AddToInstalled(constants.Ghostty, g.itemType())
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

func (g *Ghostty) SoftConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	itemType := g.itemType()
	if gc.IsAlreadyInstalled(constants.Ghostty, itemType) ||
		gc.IsInstalledByDevgeta(constants.Ghostty, itemType) {
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
