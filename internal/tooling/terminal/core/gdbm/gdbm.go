// GDBM is the GNU Database Manager library
//
// GNU dbm (GDBM) is a library of database functions that use extensible hashing
// and works similar to the standard UNIX dbm. These routines are provided to a
// programmer needing to create and manipulate a hashed database. GDBM is a set
// of database routines that use extensible hashing and work like the standard
// UNIX dbm routines.
//
// References:
// - GDBM Official Documentation: https://www.gnu.org.ua/software/gdbm/
// - GDBM Manual: https://www.gnu.org.ua/software/gdbm/manual.html
// - GDBM on GNU: https://www.gnu.org/software/gdbm/
//
// Common gdbm usage patterns:
//   - Used as a dependency for Perl, Python, and other language database modules
//   - Provides key-value database storage with extensible hashing
//   - Required by many system tools and applications for persistent data storage
//   - Used by package managers and system configuration tools
//
// Note: gdbm is primarily a library, not a CLI tool. ExecuteCommand() is
// provided for interface compliance but has limited practical use cases.

package gdbm

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Gdbm struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Gdbm {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Gdbm{Cmd: osCmd, Base: baseCmd}
}

func (g *Gdbm) Install() error {
	return g.Cmd.InstallPackage(constants.Gdbm)
}

func (g *Gdbm) SoftInstall() error {
	return g.Cmd.MaybeInstallPackage(constants.Gdbm)
}

func (g *Gdbm) ForceInstall() error {
	err := g.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall gdbm: %w", err)
	}
	return g.Install()
}

func (g *Gdbm) Uninstall() error {
	return fmt.Errorf("gdbm uninstall not supported through devgeta")
}

func (g *Gdbm) ForceConfigure() error {
	// gdbm is a library and typically doesn't require separate configuration files
	// Configuration is usually handled at build time or via language bindings
	return nil
}

func (g *Gdbm) SoftConfigure() error {
	// gdbm is a library and typically doesn't require separate configuration files
	// Configuration is usually handled at build time or via language bindings
	return nil
}

func (g *Gdbm) ExecuteCommand(args ...string) error {
	// Note: gdbm is primarily a library, not a CLI tool
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Gdbm,
		Args:    args,
	}
	if _, _, err := g.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run gdbm command: %w", err)
	}
	return nil
}

func (g *Gdbm) Update() error {
	return fmt.Errorf("gdbm update not implemented through devgeta")
}
