// Package shottr provides installation and management for Shottr, a macOS
// screenshot and annotation tool. It exists as devgeta's platform-filtered
// alternative to Flameshot (ADR-0037): Shottr has no Linux build, so every
// mutating method reports apps.ErrUnsupportedPlatform there instead of
// attempting an install.

package shottr

import (
	"fmt"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

var _ apps.App = (*Shottr)(nil)

type Shottr struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func (s *Shottr) Name() string       { return constants.Shottr }
func (s *Shottr) Kind() apps.AppKind { return apps.KindDesktop }

func New() *Shottr {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Shottr{Cmd: osCmd, Base: baseCmd}
}

func (s *Shottr) Install() error {
	if !s.Base.IsMac() {
		return apps.ErrUnsupportedPlatform
	}
	return s.Cmd.InstallDesktopApp(constants.Shottr)
}

func (s *Shottr) ForceInstall() error {
	return baseapp.Reinstall(s.Install, s.Uninstall)
}

func (s *Shottr) SoftInstall() error {
	if !s.Base.IsMac() {
		return apps.ErrUnsupportedPlatform
	}
	return s.Cmd.MaybeInstallDesktopApp(constants.Shottr)
}

func (s *Shottr) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(constants.Shottr, "desktop_app")
	return gc.Save()
}

func (s *Shottr) SoftConfigure() error {
	// No configuration needed for GUI-based screenshot tool
	return nil
}

func (s *Shottr) Uninstall() error {
	if !s.Base.IsMac() {
		return apps.ErrUnsupportedPlatform
	}
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := s.Cmd.UninstallDesktopApp(constants.Shottr); err != nil {
		return fmt.Errorf("failed to uninstall shottr: %w", err)
	}
	gc.RemoveFromInstalled(constants.Shottr, "desktop_app")
	return gc.Save()
}

func (s *Shottr) ExecuteCommand(args ...string) error {
	// No CLI commands for desktop GUI application
	return nil
}

func (s *Shottr) Update() error {
	return fmt.Errorf("%w for shottr", apps.ErrUpdateNotSupported)
}
