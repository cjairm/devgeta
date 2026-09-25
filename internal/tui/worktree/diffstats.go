// Per-row diffstat (ADR-0051): a dim "+A -R" the dashboard draws on every
// worktree row, computed on the slow refresh only, with the default branch
// resolved once per repo and bounded parallelism across worktrees.

package tuiworktree

import (
	"sync"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/sync/errgroup"

	"github.com/cjairm/devgeta/internal/tooling/task"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// statsParallelism bounds how many task.BranchStatsAt calls (3 git processes
// each) run at once - a repo with many worktrees must not fork one git
// process per tree simultaneously.
const statsParallelism = 4

// diffStatsMsg carries a fresh path -> stats map computed from whatever
// m.statuses held when the slow refresh that triggered it started. gen is
// the stateGen stamped at dispatch, so a snapshot a newer load has already
// superseded is dropped exactly like statusesMsg's own generation check.
type diffStatsMsg struct {
	stats map[string]task.BranchStatsResult
	gen   int
}

// loadStatsCmd computes ADR-0051's diffstat for every worktree in statuses
// (a snapshot taken here, not read from within the returned tea.Cmd, so no
// model-owned memory crosses into this goroutine - matching loadCmd's own
// rule). A nil seam (a test model that doesn't wire one) produces no command
// at all rather than a nil-pointer call.
func (m Model) loadStatsCmd(gen int) tea.Cmd {
	statuses := m.statuses
	defaultBranchFn := m.defaultBranchFn
	branchStatsFn := m.branchStatsFn
	if defaultBranchFn == nil || branchStatsFn == nil {
		return nil
	}
	return func() tea.Msg {
		return diffStatsMsg{
			stats: computeDiffStats(statuses, defaultBranchFn, branchStatsFn),
			gen:   gen,
		}
	}
}

// computeDiffStats resolves each repo's default branch once - DefaultBranchIn
// reads the repo's remote config, not anything worktree-specific, so any one
// of a repo's worktree paths answers for all of them - then runs
// branchStatsFn for every worktree with bounded parallelism. A worktree whose
// stats call fails is left out of the result entirely rather than given a
// zero entry, so a transient git failure reads as "unknown," not "no
// changes."
func computeDiffStats(
	statuses []worktree.WorktreeStatus,
	defaultBranchFn func(path string) string,
	branchStatsFn func(path, defaultBranch string) (task.BranchStatsResult, error),
) map[string]task.BranchStatsResult {
	defaultBranches := make(map[string]string, len(statuses))
	for _, s := range statuses {
		if _, ok := defaultBranches[s.Repo]; !ok {
			defaultBranches[s.Repo] = defaultBranchFn(s.Path)
		}
	}

	var mu sync.Mutex
	out := make(map[string]task.BranchStatsResult, len(statuses))
	var eg errgroup.Group
	eg.SetLimit(statsParallelism)
	for _, s := range statuses {
		eg.Go(func() error {
			stats, err := branchStatsFn(s.Path, defaultBranches[s.Repo])
			if err != nil {
				return nil
			}
			mu.Lock()
			out[s.Path] = stats
			mu.Unlock()
			return nil
		})
	}
	_ = eg.Wait() // branchStatsFn never returns a non-nil error (see above)
	return out
}
