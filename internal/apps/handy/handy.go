// Package handy provides installation and configuration management for Handy.
// Handy is a fully-offline speech-to-text desktop application built with Tauri,
// using Whisper and Parakeet models. It provides system-wide text injection
// capabilities for dictation across any application.
//
// macOS: installed via Homebrew cask (handy)
// Linux: no package manager available; manual GitHub release download required
//        (.deb, .rpm, or AppImage). Also requires xdotool (X11) or wtype (Wayland)
//        for text injection functionality.

package handy

import (
	"fmt"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/utils"
)

var _ apps.App = (*Handy)(nil)

type Handy struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func (h *Handy) Name() string       { return constants.Handy }
func (h *Handy) Kind() apps.AppKind { return apps.KindDesktop }

func New() *Handy {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Handy{Cmd: osCmd, Base: baseCmd}
}

func (h *Handy) Install() error {
	if h.Base.IsMac() {
		return h.Cmd.InstallDesktopApp(constants.Handy)
	}

	// Linux: no package manager support, guide user to manual installation
	return h.guideLinuxInstallation()
}

func (h *Handy) SoftInstall() error {
	if h.Base.IsMac() {
		return h.Cmd.MaybeInstallDesktopApp(constants.Handy)
	}

	// Linux: check if already installed, otherwise guide to manual installation
	if installed, _ := h.isLinuxInstalled(); installed {
		return nil
	}
	return h.guideLinuxInstallation()
}

func (h *Handy) ForceInstall() error {
	return baseapp.Reinstall(h.Install, h.Uninstall)
}

func (h *Handy) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(constants.Handy, "desktop_app")
	return gc.Save()
}

func (h *Handy) SoftConfigure() error {
	// Handy does not require separate configuration files
	// Configuration is managed through the app's own settings
	return nil
}

func (h *Handy) Uninstall() error {
	if h.Base.IsMac() {
		gc := &config.GlobalConfig{}
		if err := gc.Load(); err != nil {
			return fmt.Errorf("failed to load global config: %w", err)
		}
		if err := h.Cmd.UninstallDesktopApp(constants.Handy); err != nil {
			return fmt.Errorf("failed to uninstall handy: %w", err)
		}
		gc.RemoveFromInstalled(constants.Handy, "desktop_app")
		return gc.Save()
	}

	// Linux: manual uninstallation required
	return fmt.Errorf("uninstall on Linux must be done manually (remove AppImage/package)")
}

func (h *Handy) ExecuteCommand(args ...string) error {
	return fmt.Errorf("%w for handy", apps.ErrExecuteNotSupported)
}

func (h *Handy) Update() error {
	return fmt.Errorf("%w for handy", apps.ErrUpdateNotSupported)
}

// isLinuxInstalled checks if Handy is installed on Linux by looking for the binary
func (h *Handy) isLinuxInstalled() (bool, error) {
	// Check for handy in common installation locations
	_, _, err := h.Base.ExecCommand(cmd.CommandParams{
		Command: "which",
		Args:    []string{"handy"},
	})
	return err == nil, nil
}

// guideLinuxInstallation provides user instructions for manual Linux installation
func (h *Handy) guideLinuxInstallation() error {
	logger.L().Warn("Handy installation on Linux requires manual download")
	utils.PrintWarning("\nHandy is not available via package managers on Linux.")
	utils.PrintInfo("\nTo install Handy on Linux:")
	utils.PrintInfo("1. Visit: https://github.com/cjpais/Handy/releases")
	utils.PrintInfo("2. Download the appropriate package for your system:")
	utils.PrintInfo("   - .deb file for Debian/Ubuntu")
	utils.PrintInfo("   - .rpm file for Fedora/RHEL")
	utils.PrintInfo("   - AppImage for universal Linux")
	utils.PrintInfo("3. Install text injection dependency:")
	utils.PrintInfo("   - X11: sudo apt install xdotool")
	utils.PrintInfo("   - Wayland: sudo apt install wtype")
	utils.PrintInfo("\nAfter manual installation, run: dg configure handy --force")
	utils.PrintInfo("to register Handy with devgeta.\n")

	return fmt.Errorf("manual installation required for Linux")
}
