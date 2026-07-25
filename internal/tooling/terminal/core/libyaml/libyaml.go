// Libyaml YAML parser library with devgeta integration
//
// Libyaml is a YAML 1.1 parser and emitter library written in C. It provides
// a low-level API for parsing and emitting YAML documents. This module provides
// installation and configuration management for libyaml with devgeta integration.
//
// References:
// - Libyaml Homepage: https://pyyaml.org/wiki/LibYAML
// - Libyaml Repository: https://github.com/yaml/libyaml
// - YAML Specification: https://yaml.org/spec/
//
// Common libyaml-related usage:
//   - Development headers and libraries for building software with YAML support
//   - System library dependency for YAML parsing in various programming languages
//   - Required by Ruby, Python, and other language runtimes for YAML functionality

package libyaml

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Libyaml struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Libyaml {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Libyaml{Cmd: osCmd, Base: baseCmd}
}

func (l *Libyaml) Install() error {
	// TODO: Implement platform-specific libyaml installation logic
	// macOS: uses "libyaml" via Homebrew
	// Debian/Ubuntu: uses "libyaml-dev" via apt
	return l.Cmd.InstallPackage(constants.Libyaml)
}

func (l *Libyaml) SoftInstall() error {
	return l.Cmd.MaybeInstallPackage(constants.Libyaml)
}

func (l *Libyaml) ForceInstall() error {
	err := l.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall libyaml: %w", err)
	}
	return l.Install()
}

func (l *Libyaml) Uninstall() error {
	return fmt.Errorf("libyaml uninstall not supported through devgeta")
}

func (l *Libyaml) ForceConfigure() error {
	// TODO: Implement configuration logic if needed
	// Libyaml typically doesn't require separate configuration files
	// Configuration is usually handled at compile-time or by applications using it
	return nil
}

func (l *Libyaml) SoftConfigure() error {
	// TODO: Implement conditional configuration logic if needed
	// Libyaml typically doesn't require separate configuration files
	// Configuration is usually handled at compile-time or by applications using it
	return nil
}

func (l *Libyaml) ExecuteCommand(args ...string) error {
	// NOTE: libyaml is primarily a library, not a CLI tool
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Libyaml,
		Args:    args,
	}
	if _, _, err := l.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run libyaml command: %w", err)
	}
	return nil
}

func (l *Libyaml) Update() error {
	return fmt.Errorf("libyaml update not implemented through devgeta")
}
