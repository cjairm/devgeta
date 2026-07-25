// Zoxide smart directory navigation tool with devgeta integration
//
// Zoxide is a smarter cd command that learns your habits and allows you to navigate
// to frequently and recently used directories with just a few keystrokes. It tracks
// your most used directories and provides fuzzy matching for quick navigation.
//
// References:
// - Zoxide Documentation: https://github.com/ajeetdsouza/zoxide
// - Zoxide Wiki: https://github.com/ajeetdsouza/zoxide/wiki
//
// Common zoxide commands available through ExecuteCommand():
//   - zoxide --version - Show zoxide version information
//   - zoxide query <keywords> - Search for directories matching keywords
//   - zoxide add <path> - Add a directory to the database
//   - zoxide remove <path> - Remove a directory from the database
//   - zoxide query --list - List all tracked directories
//   - zoxide query --interactive - Interactive directory selection
//   - zoxide import <file> - Import directories from file
//   - zoxide init zsh - Generate shell initialization script
//
// Shell integration:
//   After installation and configuration, zoxide is automatically enabled via
//   devgeta's template-based shell configuration system. The 'z' command becomes
//   available for smart navigation:
//   - z foo - Jump to directory matching 'foo'
//   - zi foo - Interactive selection when multiple matches

package zoxide

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Zoxide struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Zoxide {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Zoxide{Cmd: osCmd, Base: baseCmd}
}

func (z *Zoxide) Install() error {
	return z.Cmd.InstallPackage(constants.Zoxide)
}

func (z *Zoxide) SoftInstall() error {
	return z.Cmd.MaybeInstallPackage(constants.Zoxide)
}

func (z *Zoxide) ForceInstall() error {
	err := z.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall zoxide: %w", err)
	}
	return z.Install()
}

// ForceConfigure enables zoxide shell integration and regenerates shell config
func (z *Zoxide) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.EnableShellFeature(constants.Zoxide)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

func (z *Zoxide) SoftConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if gc.IsShellFeatureEnabled(constants.Zoxide) {
		return nil
	}
	return z.ForceConfigure()
}

func (z *Zoxide) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.DisableShellFeature(constants.Zoxide)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	// TODO: We still uninstall the app or remove downloaded doc - see `Install`
	return gc.Save()
}

func (z *Zoxide) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		Command: constants.Zoxide,
		Args:    args,
	}
	if _, _, err := z.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run zoxide command: %w", err)
	}
	return nil
}

func (z *Zoxide) Update() error {
	return fmt.Errorf("zoxide update not implemented through devgeta")
}
