// Readline is a library for line-editing and history capabilities
//
// GNU Readline is a software library that provides line-editing and history
// capabilities for interactive programs with a command-line interface, such as
// Bash. It allows users to edit command lines as they are typed in and provides
// a history mechanism for recalling previous commands. Readline is used by many
// command-line programs to provide a consistent and powerful editing interface.
//
// References:
// - GNU Readline Manual: https://tiswww.case.edu/php/chet/readline/rltop.html
// - Readline Documentation: https://www.gnu.org/software/bash/manual/html_node/Command-Line-Editing.html
// - Readline GitHub: https://git.savannah.gnu.org/cgit/readline.git
//
// Note: Readline is primarily a library (libreadline) that provides line-editing
// functionality to other programs. It doesn't have standalone command-line tools
// but is used by many interactive programs like bash, python, psql, mysql, etc.
//
// Common programs that use readline:
//   - bash - The GNU Bourne-Again SHell
//   - python - Interactive Python interpreter
//   - psql - PostgreSQL interactive terminal
//   - mysql - MySQL command-line client
//   - bc - Arbitrary precision calculator
//   - gdb - GNU Debugger

package readline

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Readline struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Readline {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Readline{Cmd: osCmd, Base: baseCmd}
}

func (r *Readline) Install() error {
	return r.Cmd.InstallPackage(constants.Readline)
}

func (r *Readline) SoftInstall() error {
	return r.Cmd.MaybeInstallPackage(constants.Readline)
}

func (r *Readline) ForceInstall() error {
	err := r.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall readline: %w", err)
	}
	return r.Install()
}

func (r *Readline) Uninstall() error {
	return fmt.Errorf("readline uninstall not supported through devgeta")
}

func (r *Readline) ForceConfigure() error {
	// readline is a library that doesn't require separate configuration files
	// Configuration is handled by applications that use readline via .inputrc
	return nil
}

func (r *Readline) SoftConfigure() error {
	// readline is a library that doesn't require separate configuration files
	// Configuration is handled by applications that use readline via .inputrc
	return nil
}

func (r *Readline) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Readline,
		Args:    args,
	}
	if _, _, err := r.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run readline command: %w", err)
	}
	return nil
}

func (r *Readline) Update() error {
	return fmt.Errorf("readline update not implemented through devgeta")
}
