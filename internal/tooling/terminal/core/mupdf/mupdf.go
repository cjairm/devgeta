// Mupdf is a lightweight PDF, XPS, and E-book viewer and toolkit
//
// MuPDF is a lightweight PDF and XPS viewer with a small footprint. It consists
// of a software library, command line tools, and viewers for various platforms.
// MuPDF is designed for portability and includes support for many document formats
// including PDF, XPS, OpenXPS, CBZ, EPUB, and FictionBook.
//
// References:
// - MuPDF Official Site: https://mupdf.com/
// - MuPDF Documentation: https://mupdf.com/docs/
// - MuPDF Repository: https://git.ghostscript.com/?p=mupdf.git
// - Command-line Tools: https://mupdf.com/docs/manual-mutool-run.html
//
// Common mupdf usage patterns:
//   - PDF viewing and rendering from command line
//   - Converting PDF to other formats (PNG, SVG, text)
//   - Extracting text and metadata from PDF files
//   - PDF manipulation: merge, split, clean
//   - Document information extraction
//
// Key features:
//   - Lightweight: Small binary size and minimal dependencies
//   - Fast: Efficient rendering engine
//   - Cross-platform: Works on Unix, Windows, macOS, iOS, Android
//   - Format support: PDF, XPS, OpenXPS, CBZ, EPUB, FB2
//   - Command-line tools: mutool for document manipulation
//
// Note: mupdf is a document viewer and toolkit installed for PDF operations.
// Configuration is handled via command-line arguments. The main tool is 'mutool'
// for PDF manipulation tasks.

package mupdf

import (
	"fmt"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type Mupdf struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *Mupdf {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Mupdf{Cmd: osCmd, Base: baseCmd}
}

func (m *Mupdf) Install() error {
	return m.Cmd.InstallPackage(constants.Mupdf)
}

func (m *Mupdf) SoftInstall() error {
	return m.Cmd.MaybeInstallPackage(constants.Mupdf)
}

func (m *Mupdf) ForceInstall() error {
	err := m.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall mupdf: %w", err)
	}
	return m.Install()
}

func (m *Mupdf) Uninstall() error {
	return fmt.Errorf("mupdf uninstall not supported through devgeta")
}

func (m *Mupdf) ForceConfigure() error {
	// mupdf is a document viewer/toolkit and doesn't require separate configuration files
	// Configuration is handled via command-line arguments when invoking mutool
	return nil
}

func (m *Mupdf) SoftConfigure() error {
	// mupdf is a document viewer/toolkit and doesn't require separate configuration files
	// Configuration is handled via command-line arguments when invoking mutool
	return nil
}

func (m *Mupdf) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.Mupdf,
		Args:    args,
	}
	if _, _, err := m.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to run mupdf command: %w", err)
	}
	return nil
}

func (m *Mupdf) Update() error {
	return fmt.Errorf("mupdf update not implemented through devgeta")
}
