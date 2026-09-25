package tuiworktree

// Tests for the saved worktree snapshot (ADR-0054): what it keeps, what it
// rejects, how the model seeds from it without claiming a real load, and when
// the model writes it.

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/tooling/task"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

func TestSnapshotRoundTripKeepsOnlyGitFields(t *testing.T) {
	statuses := []worktree.WorktreeStatus{{
		Name:         "feature-a",
		Repo:         "repo-a",
		Path:         "/tmp/a",
		Branch:       "feature-a",
		TmuxWindow:   "wt-feature-a",
		WindowActive: true,
		AgentState:   "busy",
		Panes:        []tmux.PaneState{{PaneID: "%1"}},
	}}
	stats := map[string]task.BranchStatsResult{
		"/tmp/a": {Files: 2, Added: 10, Removed: 3},
		"/gone":  {Files: 1}, // no longer a worktree - must not be saved
	}

	data, err := encodeSnapshot(statuses, stats)
	if err != nil {
		t.Fatalf("encodeSnapshot: %v", err)
	}
	gotStatuses, gotStats, ok := decodeSnapshot(data)
	if !ok {
		t.Fatalf("expected the encoded snapshot to decode, got ok=false for %s", data)
	}

	want := worktree.WorktreeStatus{
		Name:   "feature-a",
		Repo:   "repo-a",
		Path:   "/tmp/a",
		Branch: "feature-a",
	}
	if len(gotStatuses) != 1 || gotStatuses[0].Name != want.Name || gotStatuses[0].Repo != want.Repo ||
		gotStatuses[0].Path != want.Path ||
		gotStatuses[0].Branch != want.Branch {
		t.Fatalf("expected the git fields back, got %+v", gotStatuses)
	}
	got := gotStatuses[0]
	if got.TmuxWindow != "" || got.WindowActive || got.AgentState != "" || got.Panes != nil {
		t.Errorf("expected no tmux-derived fields in the snapshot, got %+v", got)
	}
	if len(gotStats) != 1 || gotStats["/tmp/a"] != stats["/tmp/a"] {
		t.Errorf("expected only the live worktree's stats, got %v", gotStats)
	}
}

func TestDecodeSnapshotRejectsEmptyCorruptAndUnknownVersion(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":           "",
		"corrupt":         "{not json",
		"unknown version": `{"v":99,"worktrees":[{"repo":"r","name":"n","path":"/p"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, ok := decodeSnapshot([]byte(raw)); ok {
				t.Errorf("expected %q to be rejected", raw)
			}
		})
	}
}

func TestSeedFromSnapshotShowsRowsWithoutMarkingLoaded(t *testing.T) {
	data, err := encodeSnapshot(
		testStatuses(),
		map[string]task.BranchStatsResult{"/tmp/a": {Added: 4}},
	)
	if err != nil {
		t.Fatalf("encodeSnapshot: %v", err)
	}
	m := makeTestModel(nil)

	m.seedFromSnapshot(data)

	// repo-a header, feature-a, feature-b, repo-b header, feature-x
	if len(m.rows) != 5 {
		t.Fatalf("expected the snapshot's 5 rows on screen, got %d", len(m.rows))
	}
	if m.loaded {
		t.Error("a snapshot is a guess, not a load: m.loaded must stay false until git answers")
	}
	if m.diffStats["/tmp/a"].Added != 4 {
		t.Errorf("expected the snapshot's diffstat to be shown, got %v", m.diffStats)
	}
}

func TestSeedFromSnapshotIgnoresBadData(t *testing.T) {
	m := makeTestModel(nil)

	m.seedFromSnapshot([]byte("{not json"))

	if len(m.rows) != 0 || len(m.statuses) != 0 {
		t.Errorf("expected a bad snapshot to leave the dashboard empty, got %d rows", len(m.rows))
	}
}

func TestRealLoadReplacesSnapshot(t *testing.T) {
	stale := append(
		testStatuses(),
		worktree.WorktreeStatus{Name: "gone", Repo: "repo-b", Path: "/tmp/gone"},
	)
	data, err := encodeSnapshot(stale, nil)
	if err != nil {
		t.Fatalf("encodeSnapshot: %v", err)
	}
	m := makeTestModel(nil)
	m.seedFromSnapshot(data)

	updated, _ := m.Update(statusesMsg{statuses: testStatuses(), gen: m.stateGen})
	m = updated.(Model)

	if !m.loaded {
		t.Error("expected the real load to mark the dashboard loaded")
	}
	for _, r := range m.rows {
		if r.kind == rowWorktree && r.status.Name == "gone" {
			t.Error("expected the real load to drop the snapshot's stale worktree")
		}
	}
}

// saveRecorder wires a saveSnapshotFn that records every write.
func saveRecorder(m *Model) *[][]byte {
	var saved [][]byte
	m.saveSnapshotFn = func(data []byte) error {
		saved = append(saved, data)
		return nil
	}
	return &saved
}

func TestCurrentStatusesMsgSavesSnapshot(t *testing.T) {
	m := makeTestModel(nil)
	saved := saveRecorder(&m)

	_, cmd := m.Update(statusesMsg{statuses: testStatuses(), gen: m.stateGen})
	flattenCmd(cmd)

	if len(*saved) != 1 {
		t.Fatalf("expected one snapshot write after a current load, got %d", len(*saved))
	}
	got, _, ok := decodeSnapshot((*saved)[0])
	if !ok || len(got) != 3 {
		t.Errorf(
			"expected the saved snapshot to hold the 3 loaded worktrees, got ok=%v %+v",
			ok,
			got,
		)
	}
}

func TestStaleStatusesMsgDoesNotSaveSnapshot(t *testing.T) {
	m := makeTestModel(nil)
	saved := saveRecorder(&m)

	_, cmd := m.Update(statusesMsg{statuses: testStatuses(), gen: m.stateGen - 1})
	flattenCmd(cmd)

	if len(*saved) != 0 {
		t.Errorf("expected a superseded load not to be saved, got %d writes", len(*saved))
	}
}

func TestCurrentDiffStatsMsgSavesSnapshotWithStats(t *testing.T) {
	m := makeTestModel(testStatuses())
	saved := saveRecorder(&m)

	_, cmd := m.Update(diffStatsMsg{
		stats: map[string]task.BranchStatsResult{"/tmp/b": {Files: 1, Added: 7}},
		gen:   m.stateGen,
	})
	flattenCmd(cmd)

	if len(*saved) != 1 {
		t.Fatalf("expected one snapshot write after a current diffstat sweep, got %d", len(*saved))
	}
	_, stats, ok := decodeSnapshot((*saved)[0])
	if !ok || stats["/tmp/b"].Added != 7 {
		t.Errorf("expected the saved snapshot to carry the new stats, got ok=%v %v", ok, stats)
	}
}

func TestFailedSnapshotSaveIsNotShown(t *testing.T) {
	m := makeTestModel(nil)
	m.saveSnapshotFn = func([]byte) error { return errors.New("disk full") }

	updated, cmd := m.Update(statusesMsg{statuses: testStatuses(), gen: m.stateGen})
	m = updated.(Model)
	for _, msg := range flattenCmd(cmd) {
		if next, _ := m.Update(msg); next != nil {
			m = next.(Model)
		}
	}

	if strings.Contains(m.status, "disk full") {
		t.Errorf("expected a failed cache write to stay out of the status line, got %q", m.status)
	}
}

func TestSnapshotFileRoundTrip(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)

	if got := snapshotPath(); got != filepath.Join(cache, "devgeta", "ws-snapshot.json") {
		t.Errorf("expected the snapshot under the devgeta cache dir, got %s", got)
	}
	if _, err := readSnapshotFile(); err == nil {
		t.Error("expected reading a missing snapshot to fail")
	}
	if err := writeSnapshotFile([]byte(`{"v":1}`)); err != nil {
		t.Fatalf("writeSnapshotFile: %v", err)
	}
	got, err := readSnapshotFile()
	if err != nil || string(got) != `{"v":1}` {
		t.Errorf("expected the written bytes back, got %q, %v", got, err)
	}
}

// repoSessionFixture is the real shape of the common case: the user sits in a
// repo session that holds a worktree window and a plain window of its own, so
// the row they belong on is a repo-session row only a classified scan builds.
func repoSessionFixture() ([]worktree.WorktreeStatus, worktree.StateLayer) {
	statuses := []worktree.WorktreeStatus{
		{Name: "ws-dashboard-refresh", Repo: "devgeta", Path: "/tmp/wt"},
	}
	window := worktree.GetWindowName("devgeta", "ws-dashboard-refresh")
	wtPane := tmux.PaneState{Session: "devgeta-yamcha", Window: window, PaneID: "%1"}
	layer := worktree.StateLayer{
		Sessions:      []tmux.SessionInfo{{Name: "devgeta-yamcha", Attached: true}},
		PanesByWindow: map[string][]tmux.PaneState{window: {wtPane}},
		PanesBySession: map[string][]tmux.PaneState{
			"devgeta-yamcha": {
				wtPane,
				{Session: "devgeta-yamcha", Window: "2.1.282", PaneID: "%2"},
			},
		},
	}
	return statuses, layer
}

func TestPrimeFirstFramePlacesCursorOnCurrentSessionBeforeRealLoad(t *testing.T) {
	statuses, layer := repoSessionFixture()
	data, err := encodeSnapshot(statuses, nil)
	if err != nil {
		t.Fatalf("encodeSnapshot: %v", err)
	}
	m := makeTestModel(nil)
	m.currentSessionFn = func() (string, bool) { return "devgeta-yamcha", true }

	m.primeFirstFrame(data, func() (worktree.StateLayer, error) { return layer, nil })

	if !m.cursorPlaced {
		t.Fatal("expected the first frame to place the cursor from the snapshot and one tmux scan")
	}
	got := m.rows[m.cursor]
	if got.kind != rowRepoSession || got.repoSession.Name != "devgeta-yamcha" {
		t.Errorf("expected the cursor on repo session 'devgeta-yamcha' on the first frame, got %+v", got)
	}
	if m.loaded {
		t.Error("the snapshot is still a guess: m.loaded must wait for git")
	}
}

// A snapshot that doesn't hold the user's row (a worktree made outside the
// dashboard, say) must not end placement for good: the real load gets its try.
func TestPrimeFirstFrameMissLeavesPlacementToRealLoad(t *testing.T) {
	data, err := encodeSnapshot(testStatuses(), nil)
	if err != nil {
		t.Fatalf("encodeSnapshot: %v", err)
	}
	m := makeTestModel(nil)
	m.currentSessionFn = func() (string, bool) { return "misc", true }

	m.primeFirstFrame(data, func() (worktree.StateLayer, error) {
		return sessionsLayer([]worktree.SessionStatus{{Name: "alpha"}}), nil
	})
	if m.cursorPlaced {
		t.Fatal("expected a miss on snapshot rows to leave placement to the real load")
	}

	sessions := sessionsLayer([]worktree.SessionStatus{{Name: "alpha"}, {Name: "misc"}})
	mi, _ := m.Update(statusesMsg{statuses: testStatuses(), gen: m.stateGen})
	m = mi.(Model)
	mi, _ = m.Update(sessionsMsg{layer: sessions, gen: m.sessionGen})
	m = mi.(Model)

	got := m.rows[m.cursor]
	if got.kind != rowSession || got.session.Name != "misc" {
		t.Errorf("expected the real load to place the cursor on 'misc', got %+v", got)
	}
}

func TestPrimeFirstFrameWithoutSnapshotSkipsScan(t *testing.T) {
	m := makeTestModel(nil)
	scanned := false

	m.primeFirstFrame([]byte("{not json"), func() (worktree.StateLayer, error) {
		scanned = true
		return worktree.StateLayer{}, nil
	})

	if scanned {
		t.Error("expected no tmux scan when there is no snapshot to place against")
	}
	if m.cursorPlaced {
		t.Error("expected placement to wait for the real loads, as it does today")
	}
}

func TestPrimeFirstFrameScanErrorKeepsSnapshotRows(t *testing.T) {
	data, err := encodeSnapshot(testStatuses(), nil)
	if err != nil {
		t.Fatalf("encodeSnapshot: %v", err)
	}
	m := makeTestModel(nil)
	m.currentSessionFn = func() (string, bool) { return "misc", true }

	m.primeFirstFrame(data, func() (worktree.StateLayer, error) {
		return worktree.StateLayer{}, errors.New("no server")
	})

	if len(m.rows) != 5 {
		t.Errorf("expected the snapshot's 5 rows despite the failed scan, got %d", len(m.rows))
	}
	if m.cursorPlaced {
		t.Error("expected a failed scan to leave placement to the real loads")
	}
}
