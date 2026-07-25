// Bat - A cat clone with syntax highlighting and Git integration
//
// Bat is a cat clone with syntax highlighting, Git integration, and automatic paging.
// This module provides installation and configuration management for bat with devgeta integration.
//
// References:
// - Bat Repository: https://github.com/sharkdp/bat
// - Bat Documentation: https://github.com/sharkdp/bat#readme
//
// Common bat commands available through ExecuteCommand():
//   - bat --version - Show bat version information
//   - bat <file> - Display file with syntax highlighting
//   - bat -l <lang> <file> - Force specific language syntax
//   - bat --list-languages - Show all supported languages
//   - bat --theme <theme> - Use specific color theme
//   - bat --list-themes - Show all available themes
//   - bat -n <file> - Show line numbers
//   - bat -A <file> - Show all characters including non-printable
//   - bat --style <components> - Configure which UI elements to display

package bat

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Bat struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Bat {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Bat{Cmd: osCmd, Base: baseCmd}
}

func (b *Bat) Install() error {
	return b.Cmd.InstallPackage(constants.Bat)
}

func (b *Bat) SoftInstall() error {
	return b.Cmd.MaybeInstallPackage(constants.Bat)
}

func (b *Bat) ForceInstall() error {
	err := b.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall bat: %w", err)
	}
	return b.Install()
}

func (b *Bat) Uninstall() error {
	return fmt.Errorf("bat uninstall not supported through devgeta")
}

func (b *Bat) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.EnableShellFeature(constants.Bat)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

func (b *Bat) SoftConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if gc.IsShellFeatureEnabled(constants.Bat) {
		return nil
	}
	return b.ForceConfigure()
}

func (b *Bat) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Bat,
		Args:    args,
	}
	if _, _, err := b.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run bat command: %w", err)
	}
	return nil
}

func (b *Bat) Update() error {
	return fmt.Errorf("bat update not implemented through devgeta")
}
