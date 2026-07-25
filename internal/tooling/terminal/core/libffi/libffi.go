// Libffi is a portable foreign-function interface library
//
// The libffi library provides a portable, high level programming interface to
// various calling conventions. This allows a programmer to call any function
// specified by a call interface description at run time. FFI stands for Foreign
// Function Interface. A foreign function interface is the popular name for the
// interface that allows code written in one language to call code written in
// another language.
//
// References:
// - Libffi Official Site: https://sourceware.org/libffi/
// - Libffi GitHub: https://github.com/libffi/libffi
// - Libffi Documentation: https://www.sourceware.org/libffi/
//
// Common libffi usage patterns:
//   - Used as a dependency for dynamic language interpreters (Python, Ruby, etc.)
//   - Enables runtime code generation and just-in-time compilation
//   - Provides foreign function call interface for cross-language integration
//   - Required by many programming language runtimes and frameworks
//
// Note: libffi is primarily a library, not a CLI tool. ExecuteCommand() is
// provided for interface compliance but has limited practical use cases.

package libffi

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Libffi struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Libffi {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Libffi{Cmd: osCmd, Base: baseCmd}
}

func (l *Libffi) Install() error {
	return l.Cmd.InstallPackage(constants.Libffi)
}

func (l *Libffi) SoftInstall() error {
	return l.Cmd.MaybeInstallPackage(constants.Libffi)
}

func (l *Libffi) ForceInstall() error {
	err := l.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall libffi: %w", err)
	}
	return l.Install()
}

func (l *Libffi) Uninstall() error {
	return fmt.Errorf("libffi uninstall not supported through devgeta")
}

func (l *Libffi) ForceConfigure() error {
	// libffi is a library and typically doesn't require separate configuration files
	// Configuration is usually handled at build time or via pkg-config
	return nil
}

func (l *Libffi) SoftConfigure() error {
	// libffi is a library and typically doesn't require separate configuration files
	// Configuration is usually handled at build time or via pkg-config
	return nil
}

func (l *Libffi) ExecuteCommand(args ...string) error {
	// NOTE: libffi is primarily a library, not a CLI tool
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Libffi,
		Args:    args,
	}
	if _, _, err := l.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run libffi command: %w", err)
	}
	return nil
}

func (l *Libffi) Update() error {
	return fmt.Errorf("libffi update not implemented through devgeta")
}
