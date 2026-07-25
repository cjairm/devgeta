// Package btop provides installation and configuration management for btop resource monitor
// with devgeta integration. It follows the standardized devgeta app interface while providing
// btop-specific operations for system resource monitoring and process management.
//
// References:
// - docs/apps/btop.md: Complete module documentation
// - docs/project-overview.md: Architecture and patterns
// - docs/guides/testing-patterns.md: Testing guidelines
package btop

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Btop struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Btop {
	return &Btop{
		Cmd:  cmd.NewCommand(),
		Base: cmd.NewBaseCommand(),
	}
}

func (b *Btop) Install() error {
	return b.Cmd.InstallPackage(constants.Btop)
}

func (b *Btop) ForceInstall() error {
	if err := b.Uninstall(); err != nil {
		return err
	}
	return b.Install()
}

func (b *Btop) SoftInstall() error {
	return b.Cmd.MaybeInstallPackage(constants.Btop)
}

func (b *Btop) ForceConfigure() error {
	// TODO: Determine if btop requires configuration management
	// Expected: Return nil (no configuration required)
	return nil
}

func (b *Btop) SoftConfigure() error {
	// TODO: Determine if btop requires configuration management
	// Expected: Return nil (no configuration required)
	return nil
}

func (b *Btop) Uninstall() error {
	return fmt.Errorf("btop uninstall not supported through devgeta")
}

func (b *Btop) ExecuteCommand(args ...string) error {
	_, _, err := b.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Btop,
		Args:    args,
		IsSudo:  false,
	})
	if err != nil {
		return fmt.Errorf("failed to run btop command: %w", err)
	}
	return nil
}

func (b *Btop) Update() error {
	return fmt.Errorf(
		"btop update not implemented - use system package manager (brew upgrade btop or apt upgrade btop)",
	)
}
