// Bison is a general-purpose parser generator
//
// GNU Bison is a general-purpose parser generator that converts an annotated
// context-free grammar into a deterministic LR or generalized LR (GLR) parser.
// Once you are proficient with Bison, you can use it to develop a wide range
// of language parsers, from those used in simple desk calculators to complex
// programming languages.
//
// References:
// - GNU Bison Manual: https://www.gnu.org/software/bison/manual/
// - Bison Documentation: https://www.gnu.org/software/bison/
//
// Common bison commands available through ExecuteCommand():
//   - bison --version - Show bison version information
//   - bison file.y - Generate parser from grammar file
//   - bison -d file.y - Generate parser with header file
//   - bison -o output.c file.y - Specify output file name
//   - bison -v file.y - Generate verbose report
//   - bison --debug file.y - Enable debug mode
//   - bison --graph file.y - Generate graphical representation

package bison

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Bison struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Bison {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Bison{Cmd: osCmd, Base: baseCmd}
}

func (b *Bison) Install() error {
	return b.Cmd.InstallPackage(constants.Bison)
}

func (b *Bison) SoftInstall() error {
	return b.Cmd.MaybeInstallPackage(constants.Bison)
}

func (b *Bison) ForceInstall() error {
	err := b.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall bison: %w", err)
	}
	return b.Install()
}

func (b *Bison) Uninstall() error {
	return fmt.Errorf("bison uninstall not supported through devgeta")
}

func (b *Bison) ForceConfigure() error {
	// bison typically doesn't require separate configuration files
	// Configuration is usually handled via grammar files (.y) in project directories
	return nil
}

func (b *Bison) SoftConfigure() error {
	// bison typically doesn't require separate configuration files
	// Configuration is usually handled via grammar files (.y) in project directories
	return nil
}

func (b *Bison) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Bison,
		Args:    args,
	}
	if _, _, err := b.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run bison command: %w", err)
	}
	return nil
}

func (b *Bison) Update() error {
	return fmt.Errorf("bison update not implemented through devgeta")
}
