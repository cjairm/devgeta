// Package ripgrep provides installation and command execution management for ripgrep
// with devgeta integration. It follows the standardized devgeta app interface while
// providing ripgrep-specific operations for fast code searching and pattern matching.
//
// Ripgrep is a line-oriented search tool that recursively searches the current directory
// for a regex pattern. This module ensures ripgrep is properly installed across macOS
// (Homebrew) and Debian/Ubuntu (apt) systems.
package ripgrep

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Ripgrep struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Ripgrep {
	return &Ripgrep{
		Cmd:  cmd.NewCommand(),
		Base: cmd.NewBaseCommand(),
	}
}

var packageName = "ripgrep"

func (rg *Ripgrep) Install() error {
	if err := rg.Cmd.InstallPackage(packageName); err != nil {
		return fmt.Errorf("failed to install ripgrep: %w", err)
	}
	return nil
}

func (rg *Ripgrep) ForceInstall() error {
	if err := rg.Uninstall(); err != nil {
		return err
	}
	return rg.Install()
}

func (rg *Ripgrep) SoftInstall() error {
	if err := rg.Cmd.MaybeInstallPackage(packageName); err != nil {
		return fmt.Errorf("failed to soft install ripgrep: %w", err)
	}
	return nil
}

func (rg *Ripgrep) ForceConfigure() error {
	// Ripgrep doesn't require separate configuration files
	// Configuration is handled via CLI flags and environment variables
	return nil
}

func (rg *Ripgrep) SoftConfigure() error {
	// Ripgrep doesn't require separate configuration files
	// Configuration is handled via CLI flags and environment variables
	return nil
}

func (rg *Ripgrep) Uninstall() error {
	return fmt.Errorf("ripgrep uninstall not supported through devgeta")
}

func (rg *Ripgrep) ExecuteCommand(args ...string) error {
	_, _, err := rg.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Ripgrep,
		Args:    args,
		IsSudo:  false,
	})
	if err != nil {
		return fmt.Errorf("failed to run ripgrep command: %w", err)
	}
	return nil
}

func (rg *Ripgrep) Update() error {
	return fmt.Errorf("ripgrep update not implemented - use system package manager")
}
