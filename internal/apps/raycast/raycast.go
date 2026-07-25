// Package raycast provides installation and management for Raycast productivity launcher.
//
// Raycast is a blazingly fast, extendable launcher for macOS. This module follows the
// standardized devgeta app interface for desktop applications.

package raycast

import (
	"fmt"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

var _ apps.App = (*Raycast)(nil)

type Raycast struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func (r *Raycast) Name() string       { return constants.Raycast }
func (r *Raycast) Kind() apps.AppKind { return apps.KindDesktop }

func New() *Raycast {
	return &Raycast{Cmd: cmd.NewCommand(), Base: cmd.NewBaseCommand()}
}

func (r *Raycast) Install() error {
	return r.Cmd.InstallDesktopApp(constants.Raycast)
}

func (r *Raycast) ForceInstall() error {
	return baseapp.Reinstall(r.Install, r.Uninstall)
}

func (r *Raycast) SoftInstall() error {
	return r.Cmd.MaybeInstallDesktopApp(constants.Raycast)
}

func (r *Raycast) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(constants.Raycast, "desktop_app")
	return gc.Save()
}

func (r *Raycast) SoftConfigure() error {
	// No configuration needed for GUI-based launcher
	return nil
}

func (r *Raycast) Uninstall() error {
	if !r.Base.IsMac() {
		return nil
	}
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := r.Cmd.UninstallDesktopApp(constants.Raycast); err != nil {
		return fmt.Errorf("failed to uninstall raycast: %w", err)
	}
	gc.RemoveFromInstalled(constants.Raycast, "desktop_app")
	return gc.Save()
}

func (r *Raycast) ExecuteCommand(args ...string) error {
	// No CLI commands for desktop GUI application
	return nil
}

func (r *Raycast) Update() error {
	return fmt.Errorf("%w for raycast", apps.ErrUpdateNotSupported)
}
