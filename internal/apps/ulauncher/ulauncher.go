package ulauncher

import (
	"fmt"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

var _ apps.App = (*Ulauncher)(nil)

type Ulauncher struct {
	Cmd  commands.Command
	Base commands.BaseCommandExecutor
}

func (u *Ulauncher) Name() string       { return constants.Ulauncher }
func (u *Ulauncher) Kind() apps.AppKind { return apps.KindDesktop }

func New() *Ulauncher {
	return &Ulauncher{Cmd: commands.NewCommand(), Base: commands.NewBaseCommand()}
}

func (u *Ulauncher) Install() error {
	return u.Cmd.InstallDesktopApp(constants.Ulauncher)
}

func (u *Ulauncher) SoftInstall() error {
	return u.Cmd.MaybeInstallDesktopApp(constants.Ulauncher)
}

func (u *Ulauncher) ForceInstall() error {
	return baseapp.Reinstall(u.Install, u.Uninstall)
}

func (u *Ulauncher) Uninstall() error {
	if u.Base.IsMac() {
		return nil
	}
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := u.Cmd.UninstallDesktopApp(constants.Ulauncher); err != nil {
		return fmt.Errorf("failed to uninstall ulauncher: %w", err)
	}
	gc.RemoveFromInstalled(constants.Ulauncher, "desktop_app")
	return gc.Save()
}

func (u *Ulauncher) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(constants.Ulauncher, "desktop_app")
	return gc.Save()
}

func (u *Ulauncher) SoftConfigure() error {
	// Ulauncher uses GUI-based configuration
	// No config files to manage
	return nil
}

func (u *Ulauncher) ExecuteCommand(args ...string) error {
	// Ulauncher is a desktop application without CLI commands typically managed by devgeta
	// Return success for interface compliance
	return nil
}

func (u *Ulauncher) Update() error {
	return fmt.Errorf("%w for ulauncher — use system package manager", apps.ErrUpdateNotSupported)
}
