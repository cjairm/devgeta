package baseapp

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

// SharedConfigParts are the embedded configs/shared subtrees applied to the AI
// coder apps (claude, opencode): the skills, commands, and agents trees. They
// are the parts a user can refresh in isolation via
// `dg configure <app> --force --only=...`.
var SharedConfigParts = []string{"skills", "commands", "agents"}

// SyncSharedParts overwrites each named shared part under destRoot with a fresh
// copy from the embedded configs/shared tree. Each part is fully synced — its
// destination directory is removed first, so anything deleted upstream
// disappears locally too (a true mirror, not a merge). Config that lives
// outside these parts — settings, themes, generated files — is left untouched,
// which is what makes --only safe to run against a hand-edited config dir.
//
// Callers are expected to validate the requested parts (against
// SharedConfigParts) before calling; an unknown part here simply fails when its
// missing source directory can't be copied.
func SyncSharedParts(destRoot string, parts []string) error {
	for _, part := range parts {
		src := filepath.Join(paths.Paths.App.Configs.Shared, part)
		dst := filepath.Join(destRoot, part)
		if err := os.RemoveAll(dst); err != nil {
			return fmt.Errorf("failed to clear %s: %w", part, err)
		}
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return fmt.Errorf("failed to create %s dir: %w", part, err)
		}
		if err := files.CopyDir(src, dst); err != nil {
			return fmt.Errorf("failed to copy %s: %w", part, err)
		}
	}
	return nil
}
