package baseapp

import (
	"fmt"
	"path/filepath"

	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

// InstructionsFileName is devgeta's own instructions file. Each agent gets an
// identical copy in its config dir, and devgeta overwrites it on every
// configure. The user's own instructions file is never overwritten (ADR-0058).
const InstructionsFileName = "DEVGETA.md"

// DeployInstructions copies the shared instructions file into configDir and
// returns the path it was written to.
func DeployInstructions(configDir string) (string, error) {
	destination := filepath.Join(configDir, InstructionsFileName)
	if err := files.CopyFile(
		filepath.Join(paths.Paths.App.Configs.Shared, InstructionsFileName),
		destination,
	); err != nil {
		return "", fmt.Errorf("failed to copy %s: %w", InstructionsFileName, err)
	}
	return destination, nil
}
