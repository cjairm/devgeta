package baseapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cjairm/devgeta/pkg/paths"
)

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
