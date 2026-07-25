// Autosuggestions module provides installation and configuration management for zsh-autosuggestions with devgeta integration.
// zsh-autosuggestions suggests commands as you type based on history and completions.

package autosuggestions

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Autosuggestions struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Autosuggestions {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Autosuggestions{Cmd: osCmd, Base: baseCmd}
}

func (a *Autosuggestions) Install() error {
	return a.Cmd.InstallPackage(constants.ZshAutosuggestions)
}

func (a *Autosuggestions) ForceInstall() error {
	err := a.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall autosuggestions: %w", err)
	}
	return a.Install()
}

func (a *Autosuggestions) SoftInstall() error {
	return a.Cmd.MaybeInstallPackage(constants.ZshAutosuggestions)
}

// ForceConfigure enables autosuggestions feature and regenerates shell config
func (a *Autosuggestions) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.EnableShellFeature(constants.ZshAutosuggestions)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

func (a *Autosuggestions) SoftConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if gc.IsShellFeatureEnabled(constants.ZshAutosuggestions) {
		return nil
	}
	return a.ForceConfigure()
}

func (a *Autosuggestions) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.DisableShellFeature(constants.ZshAutosuggestions)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	// TODO: We still uninstall the app or remove downloaded doc - see `Install`
	return gc.Save()
}

func (a *Autosuggestions) ExecuteCommand(args ...string) error {
	return nil
}

func (a *Autosuggestions) Update() error {
	return fmt.Errorf("update not implemented - use system package manager")
}
