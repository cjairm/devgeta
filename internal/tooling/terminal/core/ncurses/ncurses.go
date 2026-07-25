// Ncurses terminal UI library with devgeta integration
//
// Ncurses (new curses) is a programming library providing an API that allows
// programmers to write text-based user interfaces in a terminal-independent manner.
// This module provides installation and configuration management for ncurses with
// devgeta integration.
//
// References:
// - Ncurses Homepage: https://invisible-island.net/ncurses/
// - Ncurses Documentation: https://invisible-island.net/ncurses/ncurses.html
// - GNU Ncurses: https://www.gnu.org/software/ncurses/
//
// Common ncurses-related usage:
//   - Development headers and libraries for building terminal UI applications
//   - System library dependency for terminal-based programs
//   - Required by many CLI tools, editors, and terminal applications (vim, tmux, etc.)

package ncurses

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Ncurses struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Ncurses {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Ncurses{Cmd: osCmd, Base: baseCmd}
}

func (n *Ncurses) Install() error {
	// TODO: Implement platform-specific ncurses installation logic
	// macOS: uses "ncurses" via Homebrew
	// Debian/Ubuntu: uses "libncurses-dev" or "libncurses5-dev" via apt
	return n.Cmd.InstallPackage(constants.Ncurses)
}

func (n *Ncurses) SoftInstall() error {
	return n.Cmd.MaybeInstallPackage(constants.Ncurses)
}

func (n *Ncurses) ForceInstall() error {
	err := n.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall ncurses: %w", err)
	}
	return n.Install()
}

func (n *Ncurses) Uninstall() error {
	return fmt.Errorf("ncurses uninstall not supported through devgeta")
}

func (n *Ncurses) ForceConfigure() error {
	// TODO: Implement configuration logic if needed
	// Ncurses typically doesn't require separate configuration files
	// Configuration is usually handled at compile-time or by applications using it
	return nil
}

func (n *Ncurses) SoftConfigure() error {
	// TODO: Implement conditional configuration logic if needed
	// Ncurses typically doesn't require separate configuration files
	// Configuration is usually handled at compile-time or by applications using it
	return nil
}

func (n *Ncurses) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Ncurses,
		Args:    args,
	}
	if _, _, err := n.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run ncurses command: %w", err)
	}
	return nil
}

func (n *Ncurses) Update() error {
	return fmt.Errorf("ncurses update not implemented through devgeta")
}
