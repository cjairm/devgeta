// Package unzip provides installation and command execution management for unzip
// archive extraction utility with devgeta integration.
//
// Unzip is a command-line tool for extracting files from ZIP archives. This module
// ensures unzip is properly installed across macOS (Homebrew) and Debian/Ubuntu (apt)
// systems and provides high-level operations for archive extraction.
//
// References:
//   - Project overview: docs/project-overview.md
//   - Testing patterns: docs/guides/testing-patterns.md
//   - Error handling: docs/guides/error-handling.md
//   - App documentation: docs/apps/unzip.md

package unzip

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Unzip struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Unzip {
	return &Unzip{
		Cmd:  cmd.NewCommand(),
		Base: cmd.NewBaseCommand(),
	}
}

func (u *Unzip) Install() error {
	if err := u.Cmd.InstallPackage(constants.Unzip); err != nil {
		return fmt.Errorf("failed to install unzip: %w", err)
	}
	return nil
}

func (u *Unzip) ForceInstall() error {
	if err := u.Uninstall(); err != nil {
		return fmt.Errorf("failed to uninstall before force install: %w", err)
	}
	return u.Install()
}

func (u *Unzip) SoftInstall() error {
	if err := u.Cmd.MaybeInstallPackage(constants.Unzip); err != nil {
		return fmt.Errorf("failed to conditionally install unzip: %w", err)
	}
	return nil
}

func (u *Unzip) ForceConfigure() error {
	// Unzip doesn't require configuration files
	// No-op for interface compliance
	return nil
}

func (u *Unzip) SoftConfigure() error {
	// Unzip doesn't require configuration files
	// No-op for interface compliance
	return nil
}

func (u *Unzip) Uninstall() error {
	return fmt.Errorf("unzip uninstall not supported through devgeta")
}

func (u *Unzip) ExecuteCommand(args ...string) error {
	_, _, err := u.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Unzip,
		Args:    args,
		IsSudo:  false,
	})
	if err != nil {
		return fmt.Errorf("failed to run unzip command: %w", err)
	}
	return nil
}

func (u *Unzip) Update() error {
	return fmt.Errorf("unzip update not implemented - use system package manager")
}
