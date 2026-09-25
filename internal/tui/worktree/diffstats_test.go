package tuiworktree

// Tests for ADR-0051: the ws dashboard's per-row diffstat, computed on the
// slow refresh only, with bounded parallelism and the default branch
// resolved once per repo rather than once per worktree.

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/cjairm/devgeta/internal/tooling/task"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func TestComputeDiffStatsResolvesDefaultBranchOncePerRepo(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Repo: "repo-a", Path: "/tmp/a1"},
		{Repo: "repo-a", Path: "/tmp/a2"},
		{Repo: "repo-b", Path: "/tmp/b1"},
	}

	var calls int32
	var mu sync.Mutex
	seenRepos := map[string]bool{}
	defaultBranchFn := func(path string) string {
		atomic.AddInt32(&calls, 1)
		mu.Lock()
		defer mu.Unlock()
		// Each call must be for a repo not yet resolved.
		for _, s := range statuses {
			if s.Path == path {
				if seenRepos[s.Repo] {
					t.Errorf("resolved default branch twice for repo %q", s.Repo)
				}
				seenRepos[s.Repo] = true
			}
		}
		return "main"
	}
	branchStatsFn := func(path, defaultBranch string) (task.BranchStatsResult, error) {
		if defaultBranch != "main" {
			t.Errorf("expected defaultBranch %q for %q, got %q", "main", path, defaultBranch)
		}
		return task.BranchStatsResult{Files: 1}, nil
	}

	computeDiffStats(statuses, defaultBranchFn, branchStatsFn)

	if calls != 2 {
		t.Errorf(
			"expected the default branch resolved once per repo (2 repos), got %d calls",
			calls,
		)
	}
}

func TestComputeDiffStatsSkipsFailedWorktreesKeepsOthers(t *testing.T) {
	statuses := []worktree.WorktreeStatus{
		{Repo: "repo-a", Path: "/tmp/good"},
		{Repo: "repo-a", Path: "/tmp/bad"},
	}
	defaultBranchFn := func(_ string) string { return "main" }
	branchStatsFn := func(path, _ string) (task.BranchStatsResult, error) {
		if path == "/tmp/bad" {
			return task.BranchStatsResult{}, errors.New("git error")
		}
		return task.BranchStatsResult{Files: 3, Added: 10, Removed: 2}, nil
	}

	got := computeDiffStats(statuses, defaultBranchFn, branchStatsFn)

	if _, ok := got["/tmp/bad"]; ok {
		t.Error("expected a failed worktree to be absent from the stats map, not a zero entry")
	}
	stats, ok := got["/tmp/good"]
	if !ok || stats.Files != 3 || stats.Added != 10 || stats.Removed != 2 {
		t.Errorf("expected good's stats present, got %+v (ok=%v)", stats, ok)
	}
}

func TestComputeDiffStatsBoundsParallelism(t *testing.T) {
	statuses := make([]worktree.WorktreeStatus, 20)
	for i := range statuses {
		statuses[i] = worktree.WorktreeStatus{Repo: "repo-a", Path: string(rune('a' + i))}
	}
	var inFlight, maxInFlight int32
	branchStatsFn := func(_, _ string) (task.BranchStatsResult, error) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxInFlight)
			if n <= old || atomic.CompareAndSwapInt32(&maxInFlight, old, n) {
				break
			}
		}
		atomic.AddInt32(&inFlight, -1)
		return task.BranchStatsResult{}, nil
	}

	computeDiffStats(statuses, func(_ string) string { return "main" }, branchStatsFn)

	if maxInFlight > statsParallelism {
		t.Errorf(
			"expected at most %d concurrent BranchStatsAt calls, saw %d",
			statsParallelism,
			maxInFlight,
		)
	}
}

func TestDiffMsgUpdatesDiffStatsForItsOwnPath(t *testing.T) {
	m := makeTestModel(testStatuses())

	updated, _ := m.Update(diffMsg{path: "/tmp/a", content: "diff", files: 2, added: 5, removed: 1})
	m = updated.(Model)

	stats, ok := m.diffStats["/tmp/a"]
	if !ok || stats.Files != 2 || stats.Added != 5 || stats.Removed != 1 {
		t.Errorf(
			"expected diffMsg to update diffStats for its own path, got %+v (ok=%v)",
			stats,
			ok,
		)
	}
}

func TestDiffStatsMsgAppliesStatsMapWhenCurrent(t *testing.T) {
	m := makeTestModel(testStatuses())

	updated, _ := m.Update(diffStatsMsg{
		stats: map[string]task.BranchStatsResult{"/tmp/b": {Files: 1, Added: 4}},
		gen:   m.stateGen,
	})
	m = updated.(Model)

	if stats := m.diffStats["/tmp/b"]; stats.Files != 1 || stats.Added != 4 {
		t.Errorf("expected the stats map applied, got %+v", stats)
	}
}

func TestDiffStatsMsgDroppedWhenSuperseded(t *testing.T) {
	m := makeTestModel(testStatuses())
	staleGen := m.stateGen
	m.stateGen++ // a newer load has already been dispatched

	updated, _ := m.Update(diffStatsMsg{
		stats: map[string]task.BranchStatsResult{"/tmp/b": {Files: 9}},
		gen:   staleGen,
	})
	m = updated.(Model)

	if _, ok := m.diffStats["/tmp/b"]; ok {
		t.Error("expected a superseded diffStatsMsg to be dropped")
	}
}

// The row's counts use the same green/red as the right pane's diff header.
func TestDiffstatSuffixIsColored(t *testing.T) {
	m := makeTestModel(nil)
	m.diffStats = map[string]task.BranchStatsResult{"/tmp/a": {Added: 3, Removed: 1}}

	got := m.diffstatSuffix("/tmp/a")
	want := m.palette.DiffAdded.Render("+3") + " " + m.palette.DiffRemoved.Render("−1")
	if got != want {
		t.Errorf("expected green/red diffstat %q, got %q", want, got)
	}
}
