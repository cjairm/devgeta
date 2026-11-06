// EnsureFormatScript deploys format-file.sh, the one place the per-language
// formatter and linter table lives (ADR-0063). Claude's hook wrapper and Pi's
// devgeta-format extension both call it from paths.Paths.Config.Devgeta, so it
// is agent-neutral and both configure paths write it.

package baseapp

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

// FormatScriptName is the deployed script's file name.
const FormatScriptName = "format-file.sh"

func EnsureFormatScript() error {
	if err := os.MkdirAll(paths.Paths.Config.Devgeta, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", paths.Paths.Config.Devgeta, err)
	}
	dst := filepath.Join(paths.Paths.Config.Devgeta, FormatScriptName)
	if err := files.CopyFile(
		filepath.Join(paths.Paths.App.Configs.Devgeta, FormatScriptName),
		dst,
	); err != nil {
		return fmt.Errorf("failed to deploy the format script: %w", err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return fmt.Errorf("failed to chmod the format script: %w", err)
	}
	return nil
}
