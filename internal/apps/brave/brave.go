// Package brave provides installation and management for Brave browser desktop application.
//
// Brave is a privacy-focused web browser built on Chromium. This module follows the
// standardized devgeta app interface for desktop applications.

package brave

import (
	"fmt"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

var _ apps.App = (*Brave)(nil)

type Brave struct {
	Cmd cmd.Command
}

func (b *Brave) Name() string       { return constants.Brave }
func (b *Brave) Kind() apps.AppKind { return apps.KindDesktop }

func New() *Brave {
	return &Brave{Cmd: cmd.NewCommand()}
}

func (b *Brave) Install() error {
	return b.Cmd.InstallDesktopApp(fmt.Sprintf("%s-browser", constants.Brave))
}

func (b *Brave) ForceInstall() error {
	return baseapp.Reinstall(b.Install, b.Uninstall)
}

func (b *Brave) SoftInstall() error {
	return b.Cmd.MaybeInstallDesktopApp(
		fmt.Sprintf("%s-browser", constants.Brave),
		constants.Brave,
	)
}

func (b *Brave) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(constants.Brave, "desktop_app")
	return gc.Save()
}

func (b *Brave) SoftConfigure() error {
	// No configuration needed for GUI-based browser
	return nil
}

func (b *Brave) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := b.Cmd.UninstallDesktopApp(constants.BraveBrowser); err != nil {
		return fmt.Errorf("failed to uninstall brave: %w", err)
	}
	gc.RemoveFromInstalled(constants.Brave, "desktop_app")
	return gc.Save()
}

func (b *Brave) ExecuteCommand(args ...string) error {
	// No CLI commands for desktop GUI application
	return nil
}

func (b *Brave) Update() error {
	return fmt.Errorf("%w for brave", apps.ErrUpdateNotSupported)
}
