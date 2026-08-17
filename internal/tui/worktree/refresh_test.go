package tuiworktree

// Ordering tests for ADR-0024's fast/slow refresh split. Every case here exists
// because the 3-second tick used to hide the race it describes: a wrong list
// self-healed within 3 seconds, so nobody saw it. With the git enumeration on a
// 30-second timer the same race leaves a wrong list on screen for half a
// minute, which is a bug. Each test therefore pins the ORDER messages are
// applied in, not just that the path runs.

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// errRepairUnavailable stands in for whatever a real repair failure returns;
// the handler's behavior is identical for any error.
var errRepairUnavailable = errors.New("repair unavailable")

// statusNames lists m.statuses by "repo/name" so a test can assert on the whole
// list in one comparison rather than hunting for individual rows.
func statusNames(statuses []worktree.WorktreeStatus) []string {
	out := make([]string, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, s.Repo+"/"+s.Name)
	}
	return out
}

func hasStatus(statuses []worktree.WorktreeStatus, repo, name string) bool {
	for _, s := range statuses {
		if s.Repo == repo && s.Name == name {
			return true
		}
	}
	return false
}

// findStatusesMsg returns the first statusesMsg a command produced, flattening
// any batch. Used to assert both THAT a handler dispatched the slow load and
// that the load carries the generation the handler had just bumped to.
func findStatusesMsg(t *testing.T, cmd tea.Cmd) (statusesMsg, bool) {
	t.Helper()
	for _, msg := range flattenCmd(cmd) {
		if sm, ok := msg.(statusesMsg); ok {
			return sm, true
		}
	}
	return statusesMsg{}, false
}

// tickWork splits a tick handler's batch into the refresh command it dispatched
// and its own timer re-arm, and returns the message the refresh produced. The
// re-arm is deliberately never run: it is a tea.Tick, so running it would block
// the test for the full 3 or 30 seconds.
func tickWork(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("a tick handler must return a command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected a tick to batch its refresh with its re-arm, got %T", batch)
	}
	if len(batch) != 2 {
		t.Fatalf("expected a tick to batch exactly the refresh and the re-arm, got %d", len(batch))
	}
	return batch[0]()
}

// --- Tick dispatch: which counter each tick touches ---

// The two counters exist only because the fast tick must not invalidate a slow
// load in flight. That is what this pins: a fast dispatch bumps sessionGen and
// leaves stateGen alone, and a slow dispatch does the reverse.
func TestFastTickBumpsSessionGenOnlyAndSlowTickBumpsStateGenOnly(t *testing.T) {
	m := makeTestModel(nil)
	m.mgr = newTestWorktreeManager()

	mi, cmd := m.Update(fastTickMsg(time.Now()))
	afterFast := mi.(Model)
	if afterFast.sessionGen != 1 {
		t.Errorf("fast tick should bump sessionGen to 1, got %d", afterFast.sessionGen)
	}
	if afterFast.stateGen != 0 {
		t.Errorf(
			"fast tick must leave stateGen alone (a 3s scan must not invalidate an in-flight git load), got %d",
			afterFast.stateGen,
		)
	}
	scan, ok := tickWork(t, cmd).(tmuxStateMsg)
	if !ok {
		t.Fatal("expected the fast tick to dispatch a tmux scan")
	}
	if scan.gen != afterFast.sessionGen {
		t.Errorf("tmux scan stamped %d, want the just-bumped %d", scan.gen, afterFast.sessionGen)
	}

	mi, cmd = afterFast.Update(slowTickMsg(time.Now()))
	afterSlow := mi.(Model)
	if afterSlow.stateGen != 1 {
		t.Errorf("slow tick should bump stateGen to 1, got %d", afterSlow.stateGen)
	}
	if afterSlow.sessionGen != afterFast.sessionGen {
		t.Errorf(
			"slow tick must leave sessionGen alone, got %d (was %d)",
			afterSlow.sessionGen,
			afterFast.sessionGen,
		)
	}
	load, ok := tickWork(t, cmd).(statusesMsg)
	if !ok {
		t.Fatal("expected the slow tick to dispatch the git load")
	}
	if load.gen != afterSlow.stateGen {
		t.Errorf("slow load stamped %d, want the just-bumped %d", load.gen, afterSlow.stateGen)
	}
}

// --- Slow load versus slow load ---

// Two slow loads can overlap: the 30-second timer's load starts, something else
// dispatches its own, and without the stamp whichever finishes last wins. Here
// the newer one lands first and the older git snapshot lands after it — the
// older must be dropped.
func TestRefreshOlderSlowLoadDroppedAfterNewerOne(t *testing.T) {
	m := makeTestModel(nil)
	m.mgr = newTestWorktreeManager()

	older := []worktree.WorktreeStatus{{Name: "before", Repo: "repo-a", Path: "/tmp/before"}}
	newer := []worktree.WorktreeStatus{{Name: "after", Repo: "repo-a", Path: "/tmp/after"}}

	// Two dispatches, so the two generations come from the model rather than
	// being hand-picked by the test.
	mi, _ := m.Update(slowTickMsg(time.Now()))
	m = mi.(Model)
	olderGen := m.stateGen
	mi, _ = m.Update(slowTickMsg(time.Now()))
	m = mi.(Model)
	newerGen := m.stateGen

	mi, _ = m.Update(statusesMsg{statuses: newer, gen: newerGen})
	m = mi.(Model)
	mi, _ = m.Update(statusesMsg{statuses: older, gen: olderGen})
	m = mi.(Model)

	if got := statusNames(m.statuses); len(got) != 1 || got[0] != "repo-a/after" {
		t.Errorf("expected the newer list to survive the older one landing after it, got %v", got)
	}
}

// --- Slow load versus a mutation ---

// A slow load dispatched BEFORE a delete began can still finish after it, with
// a git snapshot that predates the removal. deletedMsg bumps stateGen when it
// applies, so that snapshot is stale by the time it lands and the removed row
// stays gone.
func TestRefreshStaleSlowLoadAfterDeleteKeepsRowGone(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()

	// The 30s timer fires; its load is now in flight.
	mi, _ := m.Update(slowTickMsg(time.Now()))
	m = mi.(Model)
	inFlightGen := m.stateGen

	// The user deletes repo-a/feature-a and the result lands first.
	mi, _ = m.Update(deletedMsg{repo: "repo-a", name: "feature-a"})
	m = mi.(Model)
	if hasStatus(m.statuses, "repo-a", "feature-a") {
		t.Fatalf(
			"the delete should have dropped its row immediately, got %v",
			statusNames(m.statuses),
		)
	}

	// Now the older enumeration lands, still listing the deleted worktree.
	mi, _ = m.Update(statusesMsg{statuses: testStatuses(), gen: inFlightGen})
	m = mi.(Model)

	if hasStatus(m.statuses, "repo-a", "feature-a") {
		t.Errorf(
			"a git snapshot taken before the removal must not resurrect the row, got %v",
			statusNames(m.statuses),
		)
	}
}

// --- Mutation versus mutation ---

// Nothing marks a removal as in flight, so `d` `d` `j` `d` `d` runs two
// `git worktree remove` calls at once and their results can land in either
// order. Because deletedMsg carries only an identity, neither can restore the
// other's row — which is exactly what a payload carrying a whole list did.
func TestRefreshTwoDeletesInEitherOrderDropBothRows(t *testing.T) {
	orders := [][2]string{
		{"feature-a", "feature-b"},
		{"feature-b", "feature-a"},
	}
	for _, order := range orders {
		t.Run(order[0]+"-then-"+order[1], func(t *testing.T) {
			m := makeTestModel(testStatuses())
			m.mgr = newTestWorktreeManager()

			for _, name := range order {
				mi, _ := m.Update(deletedMsg{repo: "repo-a", name: name})
				m = mi.(Model)
			}

			for _, name := range order {
				if hasStatus(m.statuses, "repo-a", name) {
					t.Errorf(
						"%q should be gone regardless of the order the two deletes landed in, got %v",
						name,
						statusNames(m.statuses),
					)
				}
			}
			if !hasStatus(m.statuses, "repo-b", "feature-x") {
				t.Errorf(
					"untouched rows must survive both deletes, got %v",
					statusNames(m.statuses),
				)
			}
		})
	}
}

// deletedMsg has three jobs and this pins two of them: the row leaves the list
// by identity (so it also drops from a list built after the removal was
// dispatched), and a slow load stamped with the just-bumped generation goes out
// so the repo-wide `git worktree prune` the removal ran is picked up.
func TestRefreshDeletedMsgDropsRowByIdentityAndDispatchesStampedLoad(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()

	// A worktree that appeared after the removal was dispatched. It must
	// survive: the message carries no list, so it can only remove its own
	// identity from whatever m.statuses holds when it lands.
	m.statuses = append(m.statuses, worktree.WorktreeStatus{
		Name: "appeared",
		Repo: "repo-a",
		Path: "/tmp/appeared",
	})
	m.rebuildRows()
	before := m.stateGen

	mi, cmd := m.Update(deletedMsg{repo: "repo-a", name: "feature-a"})
	m = mi.(Model)

	if hasStatus(m.statuses, "repo-a", "feature-a") {
		t.Errorf("expected the removed row to be dropped, got %v", statusNames(m.statuses))
	}
	if !hasStatus(m.statuses, "repo-a", "appeared") {
		t.Errorf(
			"a row added after the delete was dispatched must survive, got %v",
			statusNames(m.statuses),
		)
	}
	if m.stateGen != before+1 {
		t.Errorf("deletedMsg should bump stateGen to %d, got %d", before+1, m.stateGen)
	}
	if !strings.Contains(m.status, "removed: feature-a") {
		t.Errorf("expected a removed confirmation, got %q", m.status)
	}

	sm, found := findStatusesMsg(t, cmd)
	if !found {
		t.Fatal(
			"deletedMsg must dispatch the slow load too: the removal's repo-wide prune also clears other stale rows, and only a fresh git enumeration notices",
		)
	}
	if sm.gen != m.stateGen {
		t.Errorf(
			"the load deletedMsg dispatches must carry the number the bump produced (%d), got %d",
			m.stateGen,
			sm.gen,
		)
	}
}

// --- Fast tick: asymmetric halves ---

// The fast tick's two halves are gated differently. Its session list is a
// wholesale replacement, so a scan whose `list-sessions` ran before
// `tmux kill-session` completed must not put the killed session back. Its pane
// half is a layer, not a replacement, so it applies regardless.
func TestFastTickStaleSessionHalfDroppedButPaneHalfStillApplies(t *testing.T) {
	window := worktree.GetWindowName("repo-a", "feature-a")
	layer := worktree.StateLayer{
		PanesByWindow: map[string][]tmux.PaneState{
			window: {{PaneID: "%1", Window: window, State: "busy"}},
		},
		PanesBySession: map[string][]tmux.PaneState{
			"scratch": {{PaneID: "%9", Session: "scratch"}},
		},
		Sessions: []tmux.SessionInfo{{Name: "scratch"}},
	}

	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	m.sessions = []worktree.SessionStatus{{Name: "scratch"}}
	m.rebuildRows()

	// The fast tick dispatches a scan stamped with the bumped sessionGen.
	mi, _ := m.Update(fastTickMsg(time.Now()))
	m = mi.(Model)
	scanGen := m.sessionGen

	// The user kills "scratch" and the kill result lands first.
	mi, _ = m.Update(sessionKilledMsg{name: "scratch"})
	m = mi.(Model)

	// The scan lands afterwards, still listing "scratch".
	mi, _ = m.Update(tmuxStateMsg{layer: layer, gen: scanGen})
	m = mi.(Model)

	if len(m.sessions) != 0 {
		t.Errorf(
			"a scan taken before the kill completed must not re-add the session, got %+v",
			m.sessions,
		)
	}
	var feature worktree.WorktreeStatus
	for _, s := range m.statuses {
		if s.Repo == "repo-a" && s.Name == "feature-a" {
			feature = s
		}
	}
	if feature.AgentState != "busy" || len(feature.Panes) != 1 || !feature.WindowActive {
		t.Errorf(
			"the same message's pane half is a layer, not a replacement, and must apply even when the session half is stale; got %+v",
			feature,
		)
	}
}

// --- Repair ---

// Both repair outcomes need the git re-read, for different reasons: a success
// rebuilt a tmux window, and a failure may have pruned the stale git entry for
// a worktree whose directory is gone — leaving a row on screen that only a
// fresh enumeration clears.
func TestRefreshRepairDispatchesSlowLoadOnBothOutcomes(t *testing.T) {
	cases := []struct {
		name       string
		repairErr  error
		wantStatus string
	}{
		{name: "success", repairErr: nil, wantStatus: "repaired: feature-a"},
		{name: "failure", repairErr: errRepairUnavailable, wantStatus: "repair failed:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := makeTestModel(testStatuses())
			m.mgr = newTestWorktreeManager()
			m.repairFn = func(_, _ string, _ worktree.Layout) error { return tc.repairErr }

			mi, cmd := m.Update(tea.KeyPressMsg{Code: 'r'})
			m = mi.(Model)
			if cmd == nil {
				t.Fatal("r should return the async repair command")
			}
			done, ok := cmd().(repairDoneMsg)
			if !ok {
				t.Fatalf(
					"repair must report a repairDoneMsg, not a plain statusMsg that refreshes nothing",
				)
			}
			if !strings.Contains(done.status, tc.wantStatus) {
				t.Errorf("expected a %q status, got %q", tc.wantStatus, done.status)
			}

			before := m.stateGen
			mi, cmd = m.Update(done)
			m = mi.(Model)

			if !strings.Contains(m.status, tc.wantStatus) {
				t.Errorf("repairDoneMsg should set the status, got %q", m.status)
			}
			if m.stateGen != before+1 {
				t.Errorf("repairDoneMsg should bump stateGen to %d, got %d", before+1, m.stateGen)
			}
			sm, found := findStatusesMsg(t, cmd)
			if !found {
				t.Fatal("repairDoneMsg must dispatch the slow load, not only set the status")
			}
			if sm.gen != m.stateGen {
				t.Errorf(
					"the load repairDoneMsg dispatches must carry the number the bump produced (%d), got %d",
					m.stateGen,
					sm.gen,
				)
			}
		})
	}
}
