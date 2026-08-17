package tuiworktree

// Ordering tests for ADR-0024's fast/slow refresh split. Every case here exists
// because the 3-second tick used to hide the race it describes: a wrong list
// self-healed within 3 seconds, so nobody saw it. With the git enumeration on a
// 30-second timer the same race leaves a wrong list on screen for half a
// minute, which is a bug. Each test therefore pins the ORDER messages are
// applied in, not just that the path runs.

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/config"
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

// The fast tick is tmux-only by design (ADR-0024): a fast tick that does not
// move the selection must recompute nothing at all, not even a debounce timer
// — because after Task 7 every diff arrives one message later behind a
// diffDebounceMsg, so a test that only checked for a synchronous diffMsg would
// pass for any message type and could not catch a future step that routes the
// fast path through applyStatuses (the slow load's diff-refresh helper),
// which sets forceDiff and would make the wrapper arm a debounce on every
// 3-second tick regardless of whether anything moved.
func TestFastTickWithUnchangedSelectionRecomputesNothing(t *testing.T) {
	m := makeTestModel(testStatuses()) // cursor on repo-a/feature-a, /tmp/a
	m.mgr = newTestWorktreeManager()
	before := m.selectedPath()

	mi, cmd := m.Update(tmuxStateMsg{layer: worktree.StateLayer{}, gen: m.sessionGen})
	m = mi.(Model)

	if m.selectedPath() != before {
		t.Fatalf(
			"this test needs the selection to stay put, got %q (was %q)",
			m.selectedPath(),
			before,
		)
	}
	if m.forceDiff {
		t.Error("a fast tick that doesn't move the selection must not set forceDiff")
	}
	for _, msg := range flattenCmd(cmd) {
		switch msg.(type) {
		case diffMsg:
			t.Fatalf(
				"the fast tick's tmuxStateMsg handler must never produce a diffMsg, got %+v",
				msg,
			)
		case diffDebounceMsg:
			t.Fatalf(
				"a fast tick that doesn't move the selection must not arm the diff debounce, got %+v",
				msg,
			)
		}
	}
}

// A fast tick CAN move the selection: the pane half of a tmuxStateMsg is a
// layer applied unconditionally, and when it changes which rows qualify for
// pane-row expansion, rebuildRows' ClampCursor can land the cursor on a
// different worktree than before. That is a genuine selection change, so it
// debounces a diff exactly like a keypress would (ADR-0024 §3) — the idle-cost
// win the test above pins only holds when the dashboard is actually idle.
func TestFastTickThatMovesSelectionArmsDebounce(t *testing.T) {
	// One repo, two worktrees: feature-a starts with 2 stateful panes (so it
	// has pane rows), feature-b has none. Rows: repo header, feature-a,
	// feature-a's two pane rows, feature-b.
	statuses := append(paneRowTestStatuses(), worktree.WorktreeStatus{
		Name:       "feature-b",
		Repo:       "repo-a",
		Path:       "/tmp/b",
		TmuxWindow: worktree.GetWindowName("repo-a", "feature-b"),
	})
	m := makeTestModel(statuses)
	m.mgr = newTestWorktreeManager()
	m.cursor = 2 // the first pane row under feature-a
	before := m.selectedPath()
	if before != "" {
		t.Fatalf("expected the cursor to start on a pane row (no selected path), got %q", before)
	}

	// The scan reports no panes at all for feature-a's window, so it no longer
	// qualifies for pane rows and both pane rows disappear. ClampCursor then
	// slides the numeric cursor position onto feature-b, a different worktree.
	mi, cmd := m.Update(tmuxStateMsg{layer: worktree.StateLayer{}, gen: m.sessionGen})
	m = mi.(Model)

	if m.selectedPath() != "/tmp/b" {
		t.Fatalf(
			"this test needs the fast tick to relocate the cursor onto feature-b, got %q",
			m.selectedPath(),
		)
	}
	assertDebouncedDiff(t, m, flattenCmd(cmd), "/tmp/b")
}

// --- The diff: debounce, staleness, and the explicit refresh key ---

// assertDebouncedDiff asserts that the messages a handler's command produced
// include the navigation debounce, that the cursor landed on wantPath, and that
// letting the debounce elapse computes the diff for that path (or, when
// wantPath is "", computes nothing — the selected row has no diff).
func assertDebouncedDiff(t *testing.T, m Model, msgs []tea.Msg, wantPath string) {
	t.Helper()

	after, next := resolveDiffDebounceIn(t, m, msgs)
	if got := after.selectedPath(); got != wantPath {
		t.Fatalf(
			"cursor landed on %q, want %q — the test's setup no longer moves it",
			got,
			wantPath,
		)
	}

	var got string
	var found bool
	for _, msg := range flattenCmd(next) {
		if d, ok := msg.(diffMsg); ok {
			got, found = d.path, true
		}
	}
	switch {
	case wantPath == "" && found:
		t.Errorf("nothing diffable is selected, yet the debounce computed a diff for %q", got)
	case wantPath != "" && !found:
		t.Errorf("expected the debounce to compute a diff for %q, got none", wantPath)
	case found && got != wantPath:
		t.Errorf("debounce computed a diff for %q, want the newly selected %q", got, wantPath)
	}
}

// Every one of these paths moves the cursor and returns (m, nil) — none of them
// asks for a diff. They are covered only because Update compares the selected
// path either side of the handler, which is the whole point of doing the check
// structurally: a list of "keys that move the cursor" would have missed the
// pasted filter entirely, and would go stale the next time a key is added.
//
// Without the wrapper each of these would leave the diff pane showing the
// previous row's changes for up to 30 seconds, since ADR-0024 took the diff off
// the 3-second tick.
func TestEveryCursorMovingPathArmsTheDiffDebounce(t *testing.T) {
	// filterFor activates the filter and types text into it, so a case can start
	// from a filtered list. FilterField's text is unexported, so it can only be
	// set by driving the same keys a user would.
	filterFor := func(t *testing.T, m Model, text string) Model {
		t.Helper()
		updated, _ := m.Update(tea.KeyPressMsg{Code: '/'})
		m = updated.(Model)
		for _, r := range text {
			updated, _ = m.Update(tea.KeyPressMsg{Code: r})
			m = updated.(Model)
		}
		return m
	}

	cases := []struct {
		name     string
		setup    func(t *testing.T) Model
		msg      tea.Msg
		wantPath string
	}{
		{
			// model.go's h, priority 1: the pane row the cursor sat on disappears,
			// so the cursor is relocated onto the parent worktree by identity.
			name: "h folds a worktree's panes and lands on the worktree",
			setup: func(t *testing.T) Model {
				// paneRowTestStatuses is the package's existing two-stateful-pane
				// fixture (testStatuses has no panes, so it can only exercise the
				// repo-level fold).
				m := makeTestModel(paneRowTestStatuses())
				m.cursor = 2 // the first pane row
				return m
			},
			msg:      tea.KeyPressMsg{Code: 'h'},
			wantPath: "/tmp/a",
		},
		{
			// model.go's h, priority 3: collapsing the repo leaves only its header.
			name: "h collapses a repo and lands on its header",
			setup: func(t *testing.T) Model {
				return makeTestModel(testStatuses()) // cursor on repo-a/feature-a
			},
			msg:      tea.KeyPressMsg{Code: 'h'},
			wantPath: "",
		},
		{
			// model.go's l, priority 2: the cursor moves to a different repo's
			// first worktree, so the diff pane must follow it.
			name: "l expands a repo and lands on its first worktree",
			setup: func(t *testing.T) Model {
				m := makeTestModel(testStatuses())
				m.collapsed["repo-b"] = true
				m.rebuildRows()
				m.cursor = 3 // the collapsed repo-b header
				return m
			},
			msg:      tea.KeyPressMsg{Code: 'l'},
			wantPath: "/tmp/x",
		},
		{
			name: "z collapses everything and sends the cursor to row 0",
			setup: func(t *testing.T) Model {
				return makeTestModel(testStatuses())
			},
			msg:      tea.KeyPressMsg{Code: 'z'},
			wantPath: "",
		},
		{
			// One typed rune re-runs buildRows over a smaller list, and
			// ClampCursor slides the cursor onto whatever survived.
			name: "typing into the filter re-clamps the cursor",
			setup: func(t *testing.T) Model {
				m := makeTestModel(testStatuses())
				m.filter.Active = true
				return m
			},
			msg:      tea.KeyPressMsg{Code: 'x'},
			wantPath: "/tmp/x",
		},
		{
			// esc clears the text and rebuilds every row, so the cursor's index
			// now means a completely different row.
			name: "esc clears the filter and rebuilds every row",
			setup: func(t *testing.T) Model {
				return filterFor(t, makeTestModel(testStatuses()), "x")
			},
			msg:      tea.KeyPressMsg{Code: tea.KeyEscape},
			wantPath: "/tmp/a",
		},
		{
			// The path no key list could ever have caught: a bracketed paste
			// never reaches handleKey at all.
			name: "pasting into the filter moves the cursor with no key involved",
			setup: func(t *testing.T) Model {
				m := makeTestModel(testStatuses())
				m.filter.Active = true
				return m
			},
			msg:      tea.PasteMsg{Content: "x"},
			wantPath: "/tmp/x",
		},
		{
			// model.go's placeCursorOnActive, reached from the sessionsMsg
			// handler when the session list is the second of the two initial
			// loads to arrive: it jumps the cursor onto the row for the tmux
			// session dg ws is actually running in.
			name: "sessionsMsg arriving second places the cursor on the active session's row",
			setup: func(t *testing.T) Model {
				m := makeTestModel(testStatuses())
				m.loaded = true // the worktree load already arrived
				m.currentSessionFn = func() (string, bool) { return "repo-b", true }
				return m
			},
			msg:      sessionsMsg{sessions: testSessions()},
			wantPath: "/tmp/x",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.setup(t)
			m.mgr = newTestWorktreeManager()
			before := m.selectedPath()

			updated, cmd := m.Update(tc.msg)
			m = updated.(Model)

			if m.selectedPath() == before {
				t.Fatalf(
					"this case is meant to move the cursor off %q but it did not; the setup needs fixing, not the debounce",
					before,
				)
			}
			assertDebouncedDiff(t, m, flattenCmd(cmd), tc.wantPath)
		})
	}
}

// The debounce only earns its keep if the moves it coalesces cost nothing:
// holding j through fifteen rows arms fifteen timers, and fourteen of them must
// land on a superseded generation and compute no diff at all.
func TestOnlyTheLastOfSeveralRapidMovesComputesADiff(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()

	updated, firstCmd := m.Update(tea.KeyPressMsg{Code: 'j'}) // → repo-a/feature-b
	m = updated.(Model)
	updated, secondCmd := m.Update(tea.KeyPressMsg{Code: 'j'}) // → repo-b/feature-x
	m = updated.(Model)

	// The first move's timer fires after the second move already re-armed.
	var first diffDebounceMsg
	var armed bool
	for _, msg := range flattenCmd(firstCmd) {
		if db, ok := msg.(diffDebounceMsg); ok {
			first, armed = db, true
		}
	}
	if !armed {
		t.Fatal("the first j must arm a debounce")
	}
	updated, stale := m.Update(first)
	m = updated.(Model)
	for _, msg := range flattenCmd(stale) {
		if d, ok := msg.(diffMsg); ok {
			t.Errorf("a superseded debounce must compute nothing, got a diff for %q", d.path)
		}
	}

	assertDebouncedDiff(t, m, flattenCmd(secondCmd), "/tmp/x")
}

// A diff costs ~0.16s of git, so navigation outruns one in flight. Rendering it
// anyway would put another worktree's changes under the selected row's name.
func TestDiffForAnUnselectedRowIsDropped(t *testing.T) {
	m := makeTestModel(testStatuses()) // cursor on repo-a/feature-a, /tmp/a
	m.mgr = newTestWorktreeManager()

	updated, _ := m.Update(diffMsg{path: "/tmp/b", content: "feature-b's changes", files: 3})
	m = updated.(Model)
	if m.diffContent != "" || m.diffPath != "" {
		t.Errorf(
			"a diff for a row the cursor already left must not be rendered, got path=%q content=%q",
			m.diffPath,
			m.diffContent,
		)
	}

	updated, _ = m.Update(diffMsg{path: "/tmp/a", content: "feature-a's changes", files: 1})
	m = updated.(Model)
	if m.diffContent != "feature-a's changes" || m.diffPath != "/tmp/a" {
		t.Errorf(
			"the selected row's own diff must still be applied, got path=%q content=%q",
			m.diffPath,
			m.diffContent,
		)
	}
}

// The slow load is one of ADR-0024 §3's three refresh triggers, and it has to
// work when nothing moved — that is the ordinary case, since a 30-second
// enumeration usually finds the same list. Update only fires on a CHANGED
// selection, so applyStatuses raises forceDiff to ask for one anyway.
func TestSlowLoadRefreshesTheDiffForAnUnchangedSelection(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	before := m.selectedPath()

	mi, _ := m.Update(slowTickMsg(time.Now()))
	m = mi.(Model)

	mi, cmd := m.Update(statusesMsg{statuses: testStatuses(), gen: m.stateGen})
	m = mi.(Model)

	if m.selectedPath() != before {
		t.Fatalf(
			"this test needs the selection to stay put, got %q (was %q)",
			m.selectedPath(),
			before,
		)
	}
	if m.forceDiff {
		t.Error(
			"Update must clear forceDiff as it consumes it, or every later message would re-diff",
		)
	}
	assertDebouncedDiff(t, m, flattenCmd(cmd), before)
}

// ctrl+r is what buys back the staleness ADR-0024 accepts when it takes the
// diff off the 3-second tick, so it has to work on an unchanged selection —
// and it has to be bound in both key handlers, because while the diff pane is
// focused every key routes through handleDiffKey instead.
//
// What it must NOT do is dispatch the slow git load: that enumerates every
// known repo, which is exactly the per-tick cost the ADR removed.
func TestCtrlRRefreshesTheDiffWithoutASlowGitLoad(t *testing.T) {
	cases := []struct {
		name        string
		diffFocused bool
	}{
		{name: "list", diffFocused: false},
		{name: "diff focused", diffFocused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := makeTestModel(testStatuses())
			m.mgr = newTestWorktreeManager()
			m.diffFocused = tc.diffFocused
			before := m.selectedPath()
			stateGen := m.stateGen

			mi, cmd := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
			m = mi.(Model)

			if m.selectedPath() != before {
				t.Fatalf(
					"ctrl+r must not move the cursor, got %q (was %q)",
					m.selectedPath(),
					before,
				)
			}
			if m.forceDiff {
				t.Error("Update must clear forceDiff as it consumes it")
			}
			if m.stateGen != stateGen {
				t.Errorf(
					"ctrl+r must not dispatch the slow git load (it bumped stateGen to %d); that stays the 30s tick's job",
					m.stateGen,
				)
			}
			// Flattened once and shared: a tea.Tick command cannot be run twice
			// (see resolveDiffDebounceIn).
			msgs := flattenCmd(cmd)
			for _, msg := range msgs {
				if _, ok := msg.(statusesMsg); ok {
					t.Error(
						"ctrl+r is a diff-only refresh and must not re-enumerate every known repo",
					)
				}
			}

			assertDebouncedDiff(t, m, msgs, before)
		})
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
			assertRepairDoneDispatchesSlowLoad(t, m, done, tc.wantStatus)
		})
	}
}

// The auto-repair inside attachToWindowCmd has four outcomes once the window
// is missing: ResolveLayout fails, repairFn fails (covered by
// TestCreateSuccessAttachFailureTriggersRefresh in create_flow_test.go), the
// repaired window still can't be found, or attaching after a successful
// repair fails. All four must report a repairDoneMsg and dispatch the slow
// load stamped with the freshly bumped stateGen - the last two are exactly
// the "repaired successfully, then failed to attach" cases where a forgotten
// refresh would be silent.
func assertRepairDoneDispatchesSlowLoad(
	t *testing.T,
	m Model,
	done repairDoneMsg,
	wantStatus string,
) {
	t.Helper()
	if !strings.Contains(done.status, wantStatus) {
		t.Errorf("expected a %q status, got %q", wantStatus, done.status)
	}

	before := m.stateGen
	mi, loadCmd := m.Update(done)
	m = mi.(Model)

	if m.stateGen != before+1 {
		t.Errorf("repairDoneMsg should bump stateGen to %d, got %d", before+1, m.stateGen)
	}
	sm, found := findStatusesMsg(t, loadCmd)
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
}

func TestAttachToWindowCmdResolveLayoutErrorDispatchesSlowLoad(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	m.windowSessionFn = func(_ string) (string, bool) { return "", false }
	m.gc = &config.GlobalConfig{Worktree: config.WorktreeConfig{DefaultLayout: "not-a-real-layout"}}

	cmd := m.attachToWindowCmd("repo-a", "feature-a")
	done, ok := cmd().(repairDoneMsg)
	if !ok {
		t.Fatalf("a ResolveLayout failure must report a repairDoneMsg, not a bare statusMsg")
	}
	assertRepairDoneDispatchesSlowLoad(t, m, done, "repair failed")
}

func TestAttachToWindowCmdWindowNotFoundAfterRepairDispatchesSlowLoad(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	m.windowSessionFn = func(_ string) (string, bool) { return "", false }
	m.repairFn = func(_, _ string, _ worktree.Layout) error { return nil }

	cmd := m.attachToWindowCmd("repo-a", "feature-a")
	done, ok := cmd().(repairDoneMsg)
	if !ok {
		t.Fatalf("a repaired-but-missing window must report a repairDoneMsg, not a bare statusMsg")
	}
	assertRepairDoneDispatchesSlowLoad(t, m, done, "window not found")
}

func TestAttachToWindowCmdAttachAfterRepairFailureDispatchesSlowLoad(t *testing.T) {
	m := makeTestModel(testStatuses())
	m.mgr = newTestWorktreeManager()
	calls := 0
	m.windowSessionFn = func(_ string) (string, bool) {
		calls++
		if calls == 1 {
			return "", false // missing before repair, triggers auto-repair
		}
		return "test-session", true // present after repair
	}
	m.repairFn = func(_, _ string, _ worktree.Layout) error { return nil }
	m.attachFn = func(_, _ string) error { return fmt.Errorf("attach unavailable") }

	cmd := m.attachToWindowCmd("repo-a", "feature-a")
	done, ok := cmd().(repairDoneMsg)
	if !ok {
		t.Fatalf(
			"a failed attach after a successful repair must report a repairDoneMsg, not a bare statusMsg",
		)
	}
	assertRepairDoneDispatchesSlowLoad(t, m, done, "attach after repair failed")
}
