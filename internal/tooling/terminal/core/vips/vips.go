// Vips is a fast image processing library with low memory needs
//
// libvips is a demand-driven, horizontally threaded image processing library.
// Besides sequential access to any image format, it provides operations for
// arithmetic, histograms, convolution, morphological operations, filtering,
// DFT, profile operations, and file format support for JPEG, TIFF, PNG, WebP,
// HEIF, GIF, SVG, PDF, and many other formats.
//
// References:
// - Vips Official Site: https://www.libvips.org/
// - Vips GitHub: https://github.com/libvips/libvips
// - API Documentation: https://www.libvips.org/API/current/
// - CLI Reference: https://www.libvips.org/API/current/using-cli.html
//
// Common vips usage patterns:
//   - Fast image resizing and thumbnail generation
//   - Batch image format conversion (JPEG, PNG, WebP, HEIF)
//   - Image optimization for web delivery
//   - Smart cropping and content-aware operations
//   - Memory-efficient processing of large images
//
// Key features:
//   - Demand-driven: Only processes necessary pixels
//   - Multi-threaded: Automatic parallel processing
//   - Memory efficient: Streaming architecture with tiling
//   - Format support: Wide range of input/output formats
//
// Note: vips is a system library installed for image processing operations.
// Configuration is handled via command-line arguments and environment variables
// (VIPS_CONCURRENCY, VIPS_DISC_THRESHOLD, VIPS_WARNING).

package vips

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Vips struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Vips {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Vips{Cmd: osCmd, Base: baseCmd}
}

func (v *Vips) Install() error {
	return v.Cmd.InstallPackage(constants.Vips)
}

func (v *Vips) SoftInstall() error {
	return v.Cmd.MaybeInstallPackage(constants.Vips)
}

func (v *Vips) ForceInstall() error {
	err := v.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall vips: %w", err)
	}
	return v.Install()
}

func (v *Vips) Uninstall() error {
	return fmt.Errorf("vips uninstall not supported through devgeta")
}

func (v *Vips) ForceConfigure() error {
	// vips is a system library and doesn't require separate configuration files
	// Configuration is handled via command-line arguments and environment variables
	return nil
}

func (v *Vips) SoftConfigure() error {
	// vips is a system library and doesn't require separate configuration files
	// Configuration is handled via command-line arguments and environment variables
	return nil
}

func (v *Vips) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Vips,
		Args:    args,
	}
	if _, _, err := v.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run vips command: %w", err)
	}
	return nil
}

func (v *Vips) Update() error {
	return fmt.Errorf("vips update not implemented through devgeta")
}
