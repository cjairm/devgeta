// Zlib compression library with devgeta integration
//
// Zlib is a software library used for data compression, providing in-memory
// compression and decompression functions including integrity checks of the
// uncompressed data. This module provides installation and configuration
// management for zlib with devgeta integration.
//
// References:
// - Zlib Documentation: https://zlib.net/
// - Zlib Manual: https://zlib.net/manual.html
//
// Common zlib-related commands and usage:
//   - Development headers and libraries for building software with zlib
//   - System library dependency for various compression utilities
//   - Required by many package managers and build tools

package zlib

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Zlib struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Zlib {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Zlib{Cmd: osCmd, Base: baseCmd}
}

func (z *Zlib) Install() error {
	// TODO: Implement platform-specific zlib installation logic
	// macOS: uses "zlib" via Homebrew
	// Debian/Ubuntu: uses "zlib1g-dev" via apt
	return z.Cmd.InstallPackage(constants.Zlib)
}

func (z *Zlib) SoftInstall() error {
	// TODO: Implement conditional installation check
	// Should verify if zlib is already installed before attempting installation
	return z.Cmd.MaybeInstallPackage(constants.Zlib)
}

func (z *Zlib) ForceInstall() error {
	err := z.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall zlib: %w", err)
	}
	return z.Install()
}

func (z *Zlib) Uninstall() error {
	return fmt.Errorf("zlib uninstall not supported through devgeta")
}

func (z *Zlib) ForceConfigure() error {
	// TODO: Implement configuration logic if needed
	// Zlib typically doesn't require separate configuration files
	// Configuration is usually handled at compile-time or by applications using it
	return nil
}

func (z *Zlib) SoftConfigure() error {
	// TODO: Implement conditional configuration logic if needed
	// Zlib typically doesn't require separate configuration files
	// Configuration is usually handled at compile-time or by applications using it
	return nil
}

func (z *Zlib) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Zlib,
		Args:    args,
	}
	if _, _, err := z.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run zlib command: %w", err)
	}
	return nil
}

func (z *Zlib) Update() error {
	return fmt.Errorf("zlib update not implemented through devgeta")
}
