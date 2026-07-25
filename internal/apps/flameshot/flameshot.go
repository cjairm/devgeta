// Package flameshot provides installation and management for Flameshot screenshot tool.
//
// Flameshot is a powerful yet simple to use screenshot software. This module follows
// the standardized devgeta app interface for desktop applications.

package flameshot

import (
	"fmt"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

var _ apps.App = (*Flameshot)(nil)

type Flameshot struct {
	Cmd cmd.Command
}

func (f *Flameshot) Name() string       { return constants.Flameshot }
func (f *Flameshot) Kind() apps.AppKind { return apps.KindDesktop }

func New() *Flameshot {
	return &Flameshot{Cmd: cmd.NewCommand()}
}

func (f *Flameshot) Install() error {
	return f.Cmd.InstallDesktopApp(constants.Flameshot)
}

func (f *Flameshot) ForceInstall() error {
	return baseapp.Reinstall(f.Install, f.Uninstall)
}

func (f *Flameshot) SoftInstall() error {
	return f.Cmd.MaybeInstallDesktopApp(constants.Flameshot)
}

func (f *Flameshot) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(constants.Flameshot, "desktop_app")
	return gc.Save()
}

func (f *Flameshot) SoftConfigure() error {
	// No configuration needed for GUI-based screenshot tool
	return nil
}

func (f *Flameshot) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := f.Cmd.UninstallDesktopApp(constants.Flameshot); err != nil {
		return fmt.Errorf("failed to uninstall flameshot: %w", err)
	}
	gc.RemoveFromInstalled(constants.Flameshot, "desktop_app")
	return gc.Save()
}

func (f *Flameshot) ExecuteCommand(args ...string) error {
	// No CLI commands for desktop GUI application
	return nil
}

func (f *Flameshot) Update() error {
	return fmt.Errorf("%w for flameshot", apps.ErrUpdateNotSupported)
}
