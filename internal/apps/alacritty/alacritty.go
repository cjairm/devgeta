// Package alacritty provides installation and configuration management for Alacritty terminal emulator.
// Alacritty is a fast, cross-platform terminal emulator written in Rust that uses GPU acceleration.
// This module follows the standardized devgeta app interface for consistent lifecycle management.

package alacritty

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

var _ apps.App = (*Alacritty)(nil)

type Alacritty struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func (a *Alacritty) Name() string       { return constants.Alacritty }
func (a *Alacritty) Kind() apps.AppKind { return apps.KindTerminal }

func New() *Alacritty {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Alacritty{Cmd: osCmd, Base: baseCmd}
}

func (a *Alacritty) Install() error {
	return a.Cmd.InstallDesktopApp(constants.Alacritty)
}

func (a *Alacritty) SoftInstall() error {
	return a.Cmd.MaybeInstallDesktopApp(constants.Alacritty)
}

func (a *Alacritty) ForceInstall() error {
	return baseapp.Reinstall(a.Install, a.Uninstall)
}

func (a *Alacritty) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	font := "default"
	theme := "default"
	configFilePath := filepath.Join(
		paths.Paths.Config.Alacritty,
		fmt.Sprintf("%s.toml", constants.Alacritty),
	)
	tmplPath := filepath.Join(
		paths.Paths.App.Configs.Alacritty,
		fmt.Sprintf("%s.toml.tmpl", constants.Alacritty),
	)
	if err := files.GenerateFromTemplate(tmplPath, configFilePath, map[string]string{
		"Font":       font,
		"Theme":      theme,
		"ConfigPath": paths.Paths.Config.Root,
	}); err != nil {
		return fmt.Errorf("failed to generate alacritty configuration: %w", err)
	}
	if err := files.CopyFile(
		filepath.Join(paths.Paths.App.Configs.Alacritty, "starter.sh"),
		filepath.Join(paths.Paths.Config.Alacritty, "starter.sh"),
	); err != nil {
		return fmt.Errorf("failed to copy alacritty starter script: %w", err)
	}
	gc.AddToInstalled(constants.Alacritty, "desktop_app")
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

func (a *Alacritty) SoftConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if gc.IsAlreadyInstalled(constants.Alacritty, "desktop_app") ||
		gc.IsInstalledByDevgeta(constants.Alacritty, "desktop_app") {
		return nil
	}
	if err := a.ForceConfigure(); err != nil {
		return fmt.Errorf("failed to configure alacritty: %w", err)
	}
	return nil
}

func (a *Alacritty) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := a.Cmd.UninstallDesktopApp(constants.Alacritty); err != nil {
		return fmt.Errorf("failed to uninstall alacritty: %w", err)
	}
	_ = os.RemoveAll(paths.Paths.Config.Alacritty)
	gc.RemoveFromInstalled(constants.Alacritty, "desktop_app")
	return gc.Save()
}

func (a *Alacritty) ExecuteCommand(args ...string) error {
	// No alacritty commands in terminal
	return nil
}

func (a *Alacritty) Update() error {
	return fmt.Errorf("%w for alacritty", apps.ErrUpdateNotSupported)
}
