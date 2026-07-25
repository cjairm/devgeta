// PowerLevel10k module provides installation and configuration management for Powerlevel10k Zsh theme with devgeta integration.
// Powerlevel10k is a fast, customizable Zsh theme that provides a visually rich command-line prompt.

package powerlevel10k

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

type PowerLevel10k struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *PowerLevel10k {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &PowerLevel10k{Cmd: osCmd, Base: baseCmd}
}

func (p *PowerLevel10k) Install() error {
	return p.Cmd.InstallPackage(constants.Powerlevel10k)
}

func (p *PowerLevel10k) ForceInstall() error {
	if err := p.Uninstall(); err != nil {
		return fmt.Errorf("failed to uninstall powerlevel10k before force install: %w", err)
	}
	return p.Install()
}

func (p *PowerLevel10k) SoftInstall() error {
	return p.Cmd.MaybeInstallPackage(constants.Powerlevel10k)
}

// ForceConfigure enables powerlevel10k feature and regenerates shell config
func (p *PowerLevel10k) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.EnableShellFeature(constants.Powerlevel10k)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

func (p *PowerLevel10k) SoftConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if gc.IsShellFeatureEnabled(constants.Powerlevel10k) {
		return nil
	}
	return p.ForceConfigure()
}

func (p *PowerLevel10k) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.DisableShellFeature(constants.Powerlevel10k)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	// TODO: We still uninstall the app or remove downloaded doc - see `Install`
	return gc.Save()
}

func (p *PowerLevel10k) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: "p10k",
		Args:    args,
	}
	if _, _, err := p.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run powerlevel10k command: %w", err)
	}
	return nil
}

func (p *PowerLevel10k) Update() error {
	return fmt.Errorf("update not implemented for %s", constants.Powerlevel10k)
}

func (p *PowerLevel10k) Reconfigure() error {
	return p.ExecuteCommand("configure")
}
