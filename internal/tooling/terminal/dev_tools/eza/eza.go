// Eza modern ls replacement tool with devgeta integration
//
// Eza is a modern, maintained replacement for ls with improved features including
// colors, icons, git integration, and tree views. This module provides installation
// and command execution management for eza with devgeta integration.
//
// References:
// - Eza Repository: https://github.com/eza-community/eza
// - Eza Documentation: https://github.com/eza-community/eza/blob/main/README.md
//
// Common eza commands available through ExecuteCommand():
//   - eza --version - Show eza version information
//   - eza - List directory contents with colors
//   - eza -l - Long format listing
//   - eza -T - Tree view
//   - eza -a - Show hidden files
//   - eza --git - Show git status
//   - eza --icons=auto - Show file icons (the value must be spelled with `=`;
//     bare --icons consumes the next argument as its value)
//   - eza -lah - Long format with hidden files and human-readable sizes

package eza

import (
	"fmt"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Eza struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Eza {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Eza{Cmd: osCmd, Base: baseCmd}
}

var _ apps.App = (*Eza)(nil)

func (e *Eza) Name() string { return constants.Eza }

func (e *Eza) Kind() apps.AppKind { return apps.KindTerminal }

func (e *Eza) Install() error {
	return e.Cmd.InstallPackage("eza")
}

func (e *Eza) SoftInstall() error {
	return e.Cmd.MaybeInstallPackage("eza")
}

func (e *Eza) ForceInstall() error {
	return baseapp.Reinstall(e.Install, e.Uninstall)
}

func (e *Eza) Uninstall() error {
	return fmt.Errorf("%w for eza", apps.ErrUninstallNotSupported)
}

func (e *Eza) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.EnableShellFeature(constants.Eza)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

func (e *Eza) SoftConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if gc.IsShellFeatureEnabled(constants.Eza) {
		return nil
	}
	return e.ForceConfigure()
}

func (e *Eza) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Eza,
		Args:    args,
	}
	if _, _, err := e.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run eza command: %w", err)
	}
	return nil
}

func (e *Eza) Update() error {
	return fmt.Errorf("%w for eza", apps.ErrUpdateNotSupported)
}
