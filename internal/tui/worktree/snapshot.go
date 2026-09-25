package tuiworktree

// The saved worktree snapshot (ADR-0054): the last successful git load and its
// diffstats, stored as versioned JSON in the devgeta cache dir so the next
// dashboard opens with rows on screen instead of "(loading...)". The normal
// loads still run and replace it.

import (
	"encoding/json"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/tooling/task"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
)

// snapshotVersion is bumped whenever the JSON shape changes incompatibly;
// decodeSnapshot rejects anything else rather than guess at migrating it.
const snapshotVersion = 1

type snapshotV1 struct {
	V         int                               `json:"v"`
	Worktrees []snapshotWorktree                `json:"worktrees"`
	Stats     map[string]task.BranchStatsResult `json:"stats,omitempty"`
}

// snapshotWorktree holds only the git-derived fields. tmux-derived ones are
// left out on purpose: the tmux scan takes milliseconds and would be stale on
// arrival anyway.
type snapshotWorktree struct {
	Repo   string `json:"repo"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
}

// encodeSnapshot keeps stats only for paths that are still worktrees, so a
// removed worktree's numbers don't ride along forever.
func encodeSnapshot(
	statuses []worktree.WorktreeStatus,
	stats map[string]task.BranchStatsResult,
) ([]byte, error) {
	snap := snapshotV1{V: snapshotVersion, Worktrees: make([]snapshotWorktree, len(statuses))}
	for i, s := range statuses {
		snap.Worktrees[i] = snapshotWorktree{
			Repo:   s.Repo,
			Name:   s.Name,
			Path:   s.Path,
			Branch: s.Branch,
		}
		if st, ok := stats[s.Path]; ok {
			if snap.Stats == nil {
				snap.Stats = map[string]task.BranchStatsResult{}
			}
			snap.Stats[s.Path] = st
		}
	}
	return json.Marshal(snap)
}

// decodeSnapshot reports ok=false for an empty, corrupt, or unknown-version
// snapshot - all three mean "nothing to show yet", never an error.
func decodeSnapshot(
	data []byte,
) ([]worktree.WorktreeStatus, map[string]task.BranchStatsResult, bool) {
	var snap snapshotV1
	if err := json.Unmarshal(data, &snap); err != nil || snap.V != snapshotVersion {
		return nil, nil, false
	}
	statuses := make([]worktree.WorktreeStatus, len(snap.Worktrees))
	for i, w := range snap.Worktrees {
		statuses[i] = worktree.WorktreeStatus{
			Repo:   w.Repo,
			Name:   w.Name,
			Path:   w.Path,
			Branch: w.Branch,
		}
	}
	return statuses, snap.Stats, true
}

// seedFromSnapshot draws the snapshot's rows before any load has landed. It
// sets m.seeded, never m.loaded: loaded means "git has answered", and the
// empty-dashboard guidance still waits for it. Session classification and
// cursor placement may use a seeded list, but only a hit on it is final (see
// placeCursorOnActive).
func (m *Model) seedFromSnapshot(data []byte) {
	statuses, stats, ok := decodeSnapshot(data)
	if !ok {
		return
	}
	m.statuses = statuses
	m.seeded = true
	for path, st := range stats {
		m.diffStats[path] = st
	}
	m.refreshView()
}

// saveSnapshotCmd encodes the current list and stats here, on the Update
// goroutine, and writes them in the returned command so no model-owned memory
// crosses into it. The write is best-effort: a failure is logged at debug and
// never reaches the status line, since a missing cache only costs one slow
// first frame.
func (m Model) saveSnapshotCmd() tea.Cmd {
	save := m.saveSnapshotFn
	if save == nil {
		return nil
	}
	data, err := encodeSnapshot(m.statuses, m.diffStats)
	if err != nil {
		logger.L().Debugw("worktree: failed to encode ws dashboard snapshot", "err", err)
		return nil
	}
	return func() tea.Msg {
		if err := save(data); err != nil {
			logger.L().Debugw("worktree: failed to save ws dashboard snapshot", "err", err)
		}
		return nil
	}
}

func snapshotPath() string {
	return paths.GetCacheDir("devgeta", "ws-snapshot.json")
}

func readSnapshotFile() ([]byte, error) {
	return os.ReadFile(snapshotPath())
}

func writeSnapshotFile(data []byte) error {
	return files.WriteFileAtomic(snapshotPath(), data, 0o600)
}

// primeFirstFrame seeds the rows from the snapshot and, when there was one,
// applies one synchronous tmux scan so the first frame already has the session
// rows and the cursor on the user's own row - instead of opening at the top
// and jumping there once the loads land. The scan is two tmux calls; with no
// snapshot it is skipped, since there is no worktree list to classify it
// against and Init's own loads handle startup as they always have. A failed
// scan leaves the snapshot rows up and placement to those loads.
func (m *Model) primeFirstFrame(data []byte, scan func() (worktree.StateLayer, error)) {
	m.seedFromSnapshot(data)
	if !m.seeded {
		return
	}
	layer, err := scan()
	if err != nil {
		logger.L().Debugw("worktree: first-frame tmux scan failed", "err", err)
		return
	}
	m.applySessionScan(layer)
}
