// Package fzf provides installation and command execution management for fzf
// fuzzy finder with devgeta integration. It follows the standardized devgeta
// app interface while providing fzf-specific operations for interactive file
// searching, command history filtering, and directory navigation.
package fzf

import (
	"fmt"
	"os/exec"
	"strings"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Fzf struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Fzf {
	return &Fzf{
		Cmd:  cmd.NewCommand(),
		Base: cmd.NewBaseCommand(),
	}
}

func (f *Fzf) Install() error {
	return f.Cmd.InstallPackage(constants.Fzf)
}

func (f *Fzf) ForceInstall() error {
	if err := f.Uninstall(); err != nil {
		return err
	}
	return f.Install()
}

func (f *Fzf) SoftInstall() error {
	return f.Cmd.MaybeInstallPackage(constants.Fzf)
}

func (f *Fzf) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.EnableShellFeature(constants.Fzf)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

func (f *Fzf) SoftConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if gc.IsShellFeatureEnabled(constants.Fzf) {
		return nil
	}
	return f.ForceConfigure()
}

func (f *Fzf) Uninstall() error {
	return fmt.Errorf("fzf uninstall not supported through devgeta")
}

func (f *Fzf) ExecuteCommand(args ...string) error {
	_, _, err := f.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Fzf,
		Args:    args,
		IsSudo:  false,
	})
	if err != nil {
		return fmt.Errorf("failed to run fzf command: %w", err)
	}
	return nil
}

func (f *Fzf) Update() error {
	return fmt.Errorf("fzf update not implemented - use system package manager")
}

// SelectFromList runs fzf with the given items piped to stdin and returns the selected item.
// The prompt parameter is displayed as the fzf header.
// Returns an error if the list is empty, fzf is not found, or user cancels (Esc).
func (f *Fzf) SelectFromList(items []string, prompt string) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("no items to select from")
	}

	// Build input string from items
	input := strings.Join(items, "\n")

	// Run fzf with prompt header and reverse layout (matches shell patterns)
	args := []string{"--header", prompt, "--reverse"}

	// Create command with stdin pipe
	fzfCmd := exec.Command(constants.Fzf, args...)
	fzfCmd.Stdin = strings.NewReader(input)

	output, err := fzfCmd.Output()
	if err != nil {
		return "", fmt.Errorf("fzf selection cancelled or failed: %w", err)
	}

	selected := strings.TrimSpace(string(output))
	if selected == "" {
		return "", fmt.Errorf("no selection made")
	}

	return selected, nil
}
