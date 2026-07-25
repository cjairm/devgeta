// Package gimp provides GIMP (GNU Image Manipulation Program) installation and management
// with devgeta integration.
//
// GIMP is a free and open-source raster graphics editor used for image manipulation,
// image editing, free-form drawing, transcoding between different image file formats,
// and more specialized tasks. This module ensures GIMP is properly installed across
// macOS (Homebrew cask) and Debian/Ubuntu (apt) systems.

package gimp

import (
	"fmt"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

var _ apps.App = (*Gimp)(nil)

type Gimp struct {
	Cmd cmd.Command
}

func (g *Gimp) Name() string       { return constants.Gimp }
func (g *Gimp) Kind() apps.AppKind { return apps.KindDesktop }

func New() *Gimp {
	return &Gimp{Cmd: cmd.NewCommand()}
}

func (g *Gimp) Install() error {
	return g.Cmd.InstallDesktopApp(constants.Gimp)
}

func (g *Gimp) SoftInstall() error {
	return g.Cmd.MaybeInstallDesktopApp(constants.Gimp)
}

func (g *Gimp) ForceInstall() error {
	return baseapp.Reinstall(g.Install, g.Uninstall)
}

func (g *Gimp) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(constants.Gimp, "desktop_app")
	return gc.Save()
}

func (g *Gimp) SoftConfigure() error {
	// Desktop applications don't require configuration file management
	return nil
}

func (g *Gimp) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := g.Cmd.UninstallDesktopApp(constants.Gimp); err != nil {
		return fmt.Errorf("failed to uninstall gimp: %w", err)
	}
	gc.RemoveFromInstalled(constants.Gimp, "desktop_app")
	return gc.Save()
}

func (g *Gimp) ExecuteCommand(args ...string) error {
	// Desktop applications don't have CLI commands to execute
	return nil
}

func (g *Gimp) Update() error {
	return fmt.Errorf("%w for gimp", apps.ErrUpdateNotSupported)
}
