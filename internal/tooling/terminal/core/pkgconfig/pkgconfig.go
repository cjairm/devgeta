// PkgConfig is a helper tool for compiling applications and libraries
//
// pkg-config is a helper tool used when compiling applications and libraries.
// It helps you insert the correct compiler options on the command line so an
// application can use gcc -o test test.c `pkg-config --libs --cflags glib-2.0`
// for instance, rather than hard-coding values on where to find glib (or other libraries).
//
// References:
// - pkg-config Guide: https://www.freedesktop.org/wiki/Software/pkg-config/
// - pkg-config Manual: https://linux.die.net/man/1/pkg-config
//
// Common pkg-config commands available through ExecuteCommand():
//   - pkg-config --version - Show pkg-config version information
//   - pkg-config --modversion <package> - Show package version
//   - pkg-config --cflags <package> - Output compiler flags
//   - pkg-config --libs <package> - Output linker flags
//   - pkg-config --exists <package> - Check if package exists
//   - pkg-config --list-all - List all packages
//   - pkg-config --print-errors <package> - Print errors if package not found

package pkgconfig

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type PkgConfig struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *PkgConfig {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &PkgConfig{Cmd: osCmd, Base: baseCmd}
}

func (pc *PkgConfig) Install() error {
	return pc.Cmd.InstallPackage(constants.PkgConfig)
}

func (pc *PkgConfig) SoftInstall() error {
	return pc.Cmd.MaybeInstallPackage(constants.PkgConfig)
}

func (pc *PkgConfig) ForceInstall() error {
	err := pc.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall pkg-config: %w", err)
	}
	return pc.Install()
}

func (pc *PkgConfig) Uninstall() error {
	return fmt.Errorf("pkg-config uninstall not supported through devgeta")
}

func (pc *PkgConfig) ForceConfigure() error {
	// pkg-config typically doesn't require separate configuration files
	// Configuration is usually handled via PKG_CONFIG_PATH environment variable
	return nil
}

func (pc *PkgConfig) SoftConfigure() error {
	// pkg-config typically doesn't require separate configuration files
	// Configuration is usually handled via PKG_CONFIG_PATH environment variable
	return nil
}

func (pc *PkgConfig) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.PkgConfig,
		Args:    args,
	}
	if _, _, err := pc.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run pkg-config command: %w", err)
	}
	return nil
}

func (pc *PkgConfig) Update() error {
	return fmt.Errorf("pkg-config update not implemented through devgeta")
}
