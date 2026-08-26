package baseapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// scratchStalePruneAge is how old a scratch subdirectory must be before
// MaintainScratchDir removes it. A safety net only — the normal path is a
// command removing its own directory via `devgeta task scratch --clean`
// when it finishes; this only ever catches one that never got there
// because its command was interrupted first (ADR-0015 §3-4).
const scratchStalePruneAge = 24 * time.Hour

// MaintainScratchDir ensures devgeta's scratch directory (ADR-0015) exists
// at the right mode and prunes stale allocations inside it. Callers: both
// agents' ForceConfigure, which covers `--force` and first install — the
// only times this runs, since SoftConfigure returns early at its marker
// file otherwise.
//
// Pruning is bounded by the SAME ownership rule `--clean` enforces: only a
// directory carrying paths.ScratchAllocPrefix is a candidate. The scratch
// root is granted to the agent (additionalDirectories / external_directory),
// so a user may reasonably keep something of their own in there; deleting
// any old directory just because it sits under the root would exceed what
// devgeta allocated and is entitled to remove. A symlink is skipped too —
// os.MkdirTemp never creates one, so it is by definition not ours.
//
// Deliberately NOT folded into SyncSharedParts, despite that being the one
// function both agents already call: SyncSharedParts's contract promises
// that config outside the requested parts is left untouched, which is what
// makes `--only` safe against a hand-edited config dir. Pruning the scratch
// root on every `--only=skills` call would break that promise.
func MaintainScratchDir() error {
	dir, err := paths.EnsureScratchDir()
	if err != nil {
		return fmt.Errorf("failed to ensure scratch dir: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read scratch dir: %w", err)
	}

	cutoff := time.Now().Add(-scratchStalePruneAge)
	for _, entry := range entries {
		// Only paths.ScratchAllocPrefix ("task-") entries are candidates —
		// NOT paths.ScratchKeyPrefix ("key-"). A keyed scratch directory
		// (ADR-0033) exists precisely so a later, independent session can
		// re-derive its path; reaping it on a 24h timer would silently empty
		// a hand-off the moment `dg configure --force` next runs. It is
		// skipped here entirely, not pruned on a longer timer.
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), paths.ScratchAllocPrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			// Gone between ReadDir and Info (e.g. a concurrent --clean) —
			// nothing left to prune, and not a failure of this pass.
			continue
		}
		// entry.IsDir() follows from ReadDir's own Lstat, so a symlink is
		// already excluded above; this keeps that explicit against a future
		// change to the loop's shape.
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
				return fmt.Errorf("failed to prune stale scratch dir %s: %w", entry.Name(), err)
			}
		}
	}
	return nil
}
