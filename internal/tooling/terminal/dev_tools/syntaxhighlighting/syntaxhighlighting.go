// Syntaxhighlighting module provides installation and configuration management for zsh-syntax-highlighting with devgeta integration.
// zsh-syntax-highlighting provides Fish shell-like syntax highlighting for commands as you type.

package syntaxhighlighting

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Syntaxhighlighting struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Syntaxhighlighting {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Syntaxhighlighting{Cmd: osCmd, Base: baseCmd}
}

func (a *Syntaxhighlighting) Install() error {
	return a.Cmd.InstallPackage(constants.Syntaxhighlighting)
}

func (a *Syntaxhighlighting) ForceInstall() error {
	err := a.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall syntaxhighlighting: %w", err)
	}
	return a.Install()
}

func (a *Syntaxhighlighting) SoftInstall() error {
	return a.Cmd.MaybeInstallPackage(constants.Syntaxhighlighting)
}

// ForceConfigure enables syntax highlighting feature and regenerates shell config
func (a *Syntaxhighlighting) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.EnableShellFeature(constants.Syntaxhighlighting)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

func (a *Syntaxhighlighting) SoftConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if gc.IsShellFeatureEnabled(constants.Syntaxhighlighting) {
		return nil
	}
	return a.ForceConfigure()
}

func (a *Syntaxhighlighting) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.DisableShellFeature(constants.Syntaxhighlighting)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	// TODO: We still uninstall the app or remove downloaded doc - see `Install`
	return gc.Save()
}

func (a *Syntaxhighlighting) ExecuteCommand(args ...string) error {
	return nil
}

func (a *Syntaxhighlighting) Update() error {
	return fmt.Errorf("update not implemented - use system package manager")
}
