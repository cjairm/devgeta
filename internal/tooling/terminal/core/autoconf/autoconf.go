// Autoconf is an extensible package of M4 macros for producing shell scripts
//
// GNU Autoconf is a tool for producing shell scripts that automatically configure
// software source code packages to adapt to many kinds of POSIX-like systems.
// The configuration scripts produced by Autoconf are independent of Autoconf when
// they are run, so their users do not need to have Autoconf.
//
// References:
// - GNU Autoconf Manual: https://www.gnu.org/software/autoconf/manual/
// - Autoconf Documentation: https://www.gnu.org/savannah-checkouts/gnu/autoconf/manual/autoconf.html
//
// Common autoconf commands available through ExecuteCommand():
//   - autoconf --version - Show autoconf version information
//   - autoconf - Generate configure script from configure.ac
//   - autoconf configure.ac - Generate configure from specified file
//   - autoreconf - Update generated configuration files
//   - autoreconf -i - Install missing auxiliary files
//   - autoreconf -fvi - Force verbose install mode
//   - autoheader - Create template header for configure

package autoconf

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Autoconf struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Autoconf {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Autoconf{Cmd: osCmd, Base: baseCmd}
}

func (a *Autoconf) Install() error {
	return a.Cmd.InstallPackage(constants.Autoconf)
}

func (a *Autoconf) SoftInstall() error {
	return a.Cmd.MaybeInstallPackage(constants.Autoconf)
}

func (a *Autoconf) ForceInstall() error {
	err := a.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall autoconf: %w", err)
	}
	return a.Install()
}

func (a *Autoconf) Uninstall() error {
	return fmt.Errorf("autoconf uninstall not supported through devgeta")
}

func (a *Autoconf) ForceConfigure() error {
	// autoconf typically doesn't require separate configuration files
	// Configuration is usually handled via configure.ac in project directories
	return nil
}

func (a *Autoconf) SoftConfigure() error {
	// autoconf typically doesn't require separate configuration files
	// Configuration is usually handled via configure.ac in project directories
	return nil
}

func (a *Autoconf) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Autoconf,
		Args:    args,
	}
	if _, _, err := a.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run autoconf command: %w", err)
	}
	return nil
}

func (a *Autoconf) Update() error {
	return fmt.Errorf("autoconf update not implemented through devgeta")
}
