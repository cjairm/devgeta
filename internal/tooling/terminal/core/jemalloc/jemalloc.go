// Jemalloc is a general-purpose memory allocator library
//
// jemalloc is a general purpose malloc(3) implementation that emphasizes
// fragmentation avoidance and scalable concurrency support. It provides many
// introspection, memory management, and tuning features beyond the standard
// allocator. jemalloc is widely used in production systems, particularly for
// high-performance applications requiring efficient memory management.
//
// References:
// - Jemalloc Official Site: http://jemalloc.net/
// - Jemalloc GitHub: https://github.com/jemalloc/jemalloc
// - Jemalloc Documentation: https://jemalloc.net/jemalloc.3.html
//
// Common jemalloc usage patterns:
//   - Used as a replacement for system malloc for better performance
//   - Required by Redis, MariaDB, and other high-performance databases
//   - Linked into applications via LD_PRELOAD or direct linking
//   - Provides memory profiling and debugging capabilities
//   - Reduces memory fragmentation in long-running applications
//
// Note: jemalloc is primarily a library, not a CLI tool. ExecuteCommand() is
// provided for interface compliance but has limited practical use cases.
// jemalloc utilities like jemalloc-config may be available on some systems.

package jemalloc

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Jemalloc struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Jemalloc {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Jemalloc{Cmd: osCmd, Base: baseCmd}
}

func (j *Jemalloc) Install() error {
	return j.Cmd.InstallPackage(constants.Jemalloc)
}

func (j *Jemalloc) SoftInstall() error {
	return j.Cmd.MaybeInstallPackage(constants.Jemalloc)
}

func (j *Jemalloc) ForceInstall() error {
	err := j.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall jemalloc: %w", err)
	}
	return j.Install()
}

func (j *Jemalloc) Uninstall() error {
	return fmt.Errorf("jemalloc uninstall not supported through devgeta")
}

func (j *Jemalloc) ForceConfigure() error {
	// jemalloc is a library and typically doesn't require separate configuration files
	// Configuration is usually handled via environment variables or compile-time options
	return nil
}

func (j *Jemalloc) SoftConfigure() error {
	// jemalloc is a library and typically doesn't require separate configuration files
	// Configuration is usually handled via environment variables or compile-time options
	return nil
}

func (j *Jemalloc) ExecuteCommand(args ...string) error {
	// Note: jemalloc is primarily a library, not a CLI tool
	// This method is provided for interface compliance
	// In practice, jemalloc utilities like jemalloc-config may be available
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Jemalloc,
		Args:    args,
	}
	if _, _, err := j.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run jemalloc command: %w", err)
	}
	return nil
}

func (j *Jemalloc) Update() error {
	return fmt.Errorf("jemalloc update not implemented through devgeta")
}
