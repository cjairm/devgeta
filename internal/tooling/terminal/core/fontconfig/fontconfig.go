// Package fontconfig provides installation and configuration management for fontconfig library
// with devgeta integration.
//
// Fontconfig is a library for configuring and customizing font access, used by many applications
// on Linux/Unix systems for font rendering and management.

package fontconfig

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type FontConfig struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *FontConfig {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &FontConfig{Cmd: osCmd, Base: baseCmd}
}

func (fc *FontConfig) Install() error {
	return fc.Cmd.InstallPackage(constants.FontConfig)
}

func (fc *FontConfig) SoftInstall() error {
	return fc.Cmd.MaybeInstallPackage(constants.FontConfig)
}

func (fc *FontConfig) ForceInstall() error {
	err := fc.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall fontconfig: %w", err)
	}
	return fc.Install()
}

func (fc *FontConfig) ForceConfigure() error {
	// fontconfig doesn't require devgeta-managed configuration
	// It uses system defaults which are appropriate for most use cases
	// Configuration would involve:
	// 1. Copy configuration from paths.FontConfigConfigAppDir to paths.FontConfigConfigLocalDir
	// 2. Update font cache if needed
	// Example:
	// err := files.CopyDir(paths.FontConfigConfigAppDir, paths.FontConfigConfigLocalDir)
	// if err != nil {
	//     return fmt.Errorf("failed to copy fontconfig configuration: %w", err)
	// }
	return nil
}

func (fc *FontConfig) SoftConfigure() error {
	// fontconfig doesn't require devgeta-managed configuration
	// It uses system defaults which are appropriate for most use cases
	return nil
}

func (fc *FontConfig) Uninstall() error {
	return fmt.Errorf("fontconfig uninstall not supported through devgeta")
}

func (fc *FontConfig) ExecuteCommand(fontConfigCmd string, args ...string) error {
	if fontConfigCmd == "" {
		return fmt.Errorf("fontConfigCmd cannot be empty")
	}
	// Common fontconfig commands:
	// - fc-cache: Build font information cache
	// - fc-list: List available fonts
	// - fc-match: Match available fonts
	// - fc-pattern: Parse and validate patterns
	switch fontConfigCmd {
	case "fc-cache", "fc-list", "fc-match", "fc-pattern":
		// Supported commands - will be executed with provided arguments
	default:
		return fmt.Errorf("unsupported fontconfig command: %s", fontConfigCmd)
	}
	execCommand := cmd.CommandParams{
		Command: fontConfigCmd,
		Args:    args,
	}
	if _, _, err := fc.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run fontconfig command: %w", err)
	}
	return nil
}

func (fc *FontConfig) Update() error {
	return fmt.Errorf("fontconfig update not implemented - use system package manager")
}
