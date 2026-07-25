// Curl HTTP client tool with devgeta integration
//
// Curl is a command-line tool for transferring data with URLs, supporting various
// protocols including HTTP, HTTPS, FTP, and more. This module provides installation
// and configuration management for curl with devgeta integration.
//
// References:
// - Curl Documentation: https://curl.se/docs/
// - Curl Manual: https://curl.se/docs/manual.html
//
// Common curl commands available through ExecuteCommand():
//   - curl --version - Show curl version information
//   - curl <url> - Fetch content from URL
//   - curl -o <file> <url> - Download file from URL
//   - curl -X POST <url> - Send POST request
//   - curl -H "Header: value" <url> - Add custom headers
//   - curl -d "data" <url> - Send data with request
//   - curl -i <url> - Include response headers
//   - curl -s <url> - Silent mode (no progress bar)

package curl

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Curl struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Curl {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Curl{Cmd: osCmd, Base: baseCmd}
}

func (c *Curl) Install() error {
	return c.Cmd.InstallPackage("curl")
}

func (c *Curl) SoftInstall() error {
	return c.Cmd.MaybeInstallPackage("curl")
}

func (c *Curl) ForceInstall() error {
	err := c.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall curl: %w", err)
	}
	return c.Install()
}

func (c *Curl) Uninstall() error {
	return fmt.Errorf("curl uninstall not supported through devgeta")
}

func (c *Curl) ForceConfigure() error {
	// Curl typically doesn't require separate configuration files
	// Configuration is usually handled via command-line arguments
	return nil
}

func (c *Curl) SoftConfigure() error {
	// Curl typically doesn't require separate configuration files
	// Configuration is usually handled via command-line arguments
	return nil
}

func (c *Curl) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Curl,
		Args:    args,
	}
	if _, _, err := c.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run curl command: %w", err)
	}
	return nil
}

func (c *Curl) Update() error {
	return fmt.Errorf("curl update not implemented through devgeta")
}
