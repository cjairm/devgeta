// Package xcode provides installation and command execution management for
// Xcode Command Line Tools with devgeta integration.
//
// Xcode Command Line Tools is a macOS-specific package that provides essential
// development tools including compilers (clang, gcc), build tools (make), SDKs,
// and headers needed for software development on macOS. This module ensures
// Xcode Command Line Tools are properly installed on macOS systems.
//
// References:
//   - Project overview: docs/project-overview.md
//   - Testing patterns: docs/guides/testing-patterns.md
//   - Error handling: docs/guides/error-handling.md
//   - Module documentation: docs/tooling/terminal/core/xcode.md
//
// Common xcode-select commands available through ExecuteCommand():
//   - xcode-select --version         - Show xcode-select version
//   - xcode-select --print-path      - Print active developer directory path
//   - xcode-select --install         - Install Xcode Command Line Tools
//   - xcode-select --switch <path>   - Switch active developer directory
//   - xcode-select --reset           - Reset to default developer directory

package xcode

import (
	"fmt"
	"strings"

	cmd "github.com/cjairm/devgeta/internal/commands"
)

type XcodeCommandLineTools struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *XcodeCommandLineTools {
	return &XcodeCommandLineTools{
		Cmd:  cmd.NewCommand(),
		Base: cmd.NewBaseCommand(),
	}
}

func (x *XcodeCommandLineTools) Install() error {
	// Install using xcode-select --install
	_, _, err := x.Base.ExecCommand(cmd.CommandParams{
		PreExecMsg: "Installing Xcode Command Line Tools",
		Command:    "xcode-select",
		Args:       []string{"--install"},
	})
	if err != nil {
		return fmt.Errorf("failed to install xcode command line tools: %w", err)
	}
	return nil
}

func (x *XcodeCommandLineTools) ForceInstall() error {
	if err := x.Uninstall(); err != nil {
		return fmt.Errorf("failed to uninstall before force install: %w", err)
	}
	return x.Install()
}

func (x *XcodeCommandLineTools) SoftInstall() error {
	installed, err := x.isInstalled()
	if err != nil {
		return fmt.Errorf("failed to check xcode command line tools installation: %w", err)
	}
	if installed {
		return nil
	}
	return x.Install()
}

func (x *XcodeCommandLineTools) ForceConfigure() error {
	// Xcode Command Line Tools don't require configuration files
	// No-op for interface compliance
	return nil
}

func (x *XcodeCommandLineTools) SoftConfigure() error {
	// Xcode Command Line Tools don't require configuration files
	// No-op for interface compliance
	return nil
}

func (x *XcodeCommandLineTools) Uninstall() error {
	return fmt.Errorf("xcode command line tools uninstall not supported through devgeta")
}

func (x *XcodeCommandLineTools) ExecuteCommand(args ...string) error {
	_, _, err := x.Base.ExecCommand(cmd.CommandParams{
		Command: "xcode-select",
		Args:    args,
	})
	if err != nil {
		return fmt.Errorf("failed to run xcode-select command: %w", err)
	}
	return nil
}

func (x *XcodeCommandLineTools) Update() error {
	return fmt.Errorf("xcode command line tools update not implemented - use macOS software update")
}

func (x *XcodeCommandLineTools) isInstalled() (bool, error) {
	stdout, _, err := x.Base.ExecCommand(cmd.CommandParams{
		Command: "xcode-select",
		Args:    []string{"-p"},
		IsSudo:  false,
	})
	if err != nil {
		return false, fmt.Errorf("error running xcode-select: %w", err)
	}
	xcodePath := strings.ToLower(strings.TrimSpace(stdout))
	return strings.Contains(xcodePath, "xcode.app") ||
		strings.Contains(xcodePath, "commandlinetools"), nil
}
