// Package docker provides installation and configuration management for Docker Desktop.
// Docker Desktop is a containerization platform that enables developers to build, ship, and run
// distributed applications using containers. This module follows the standardized devgeta app
// interface for consistent lifecycle management.

package docker

import (
	"fmt"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

var _ apps.App = (*Docker)(nil)

type Docker struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func (d *Docker) Name() string       { return constants.Docker }
func (d *Docker) Kind() apps.AppKind { return apps.KindDesktop }

func New() *Docker {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Docker{Cmd: osCmd, Base: baseCmd}
}

func (d *Docker) Install() error {
	return d.Cmd.InstallDesktopApp(constants.Docker)
}

func (d *Docker) SoftInstall() error {
	return d.Cmd.MaybeInstallDesktopApp(constants.Docker)
}

func (d *Docker) ForceInstall() error {
	return baseapp.Reinstall(d.Install, d.Uninstall)
}

func (d *Docker) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(constants.Docker, "desktop_app")
	return gc.Save()
}

func (d *Docker) SoftConfigure() error {
	// Docker Desktop doesn't require separate configuration files
	// Configuration is managed through Docker Desktop GUI or daemon.json
	return nil
}

func (d *Docker) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := d.Cmd.UninstallDesktopApp(constants.Docker); err != nil {
		return fmt.Errorf("failed to uninstall docker: %w", err)
	}
	gc.RemoveFromInstalled(constants.Docker, "desktop_app")
	return gc.Save()
}

func (d *Docker) ExecuteCommand(args ...string) error {
	params := cmd.CommandParams{
		Command: constants.Docker,
		Args:    args,
	}
	_, _, err := d.Base.ExecCommand(params)
	if err != nil {
		return fmt.Errorf("failed to execute docker command: %w", err)
	}
	return nil
}

func (d *Docker) Update() error {
	return fmt.Errorf("%w for docker", apps.ErrUpdateNotSupported)
}
