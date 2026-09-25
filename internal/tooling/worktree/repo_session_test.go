package worktree

// Tests for RepoSessionStatuses (ADR-0052): the repo-session rows the ws
// dashboard draws under each repo header, one per live session holding that
// repo's worktree windows and at least one plain window.

import (
	"testing"

	"github.com/cjairm/devgeta/internal/apps/tmux"
)

func TestRepoSessionStatuses(t *testing.T) {
	wtWindow := GetWindowName("hire2", "feat")

	t.Run("one session holding the repo's worktree windows", func(t *testing.T) {
		statuses := []WorktreeStatus{{Repo: "hire2", Name: "feat"}}
		layer := StateLayer{
			Sessions: []tmux.SessionInfo{{Name: "hire2-tien", Attached: true}},
			PanesByWindow: map[string][]tmux.PaneState{
				wtWindow: {{Session: "hire2-tien", Window: wtWindow, PaneID: "%1"}},
			},
			PanesBySession: map[string][]tmux.PaneState{
				"hire2-tien": {
					{Session: "hire2-tien", Window: wtWindow, PaneID: "%1"},
					{Session: "hire2-tien", Window: "zsh", PaneID: "%2"},
				},
			},
		}

		got := RepoSessionStatuses(statuses, layer, liveFor([2]string{"hire2", "feat"}), "")
		if len(got) != 1 {
			t.Fatalf("expected 1 repo-session row, got %d: %+v", len(got), got)
		}
		rs := got[0]
		if rs.Repo != "hire2" || rs.Name != "hire2-tien" || !rs.Attached {
			t.Errorf("expected {hire2 hire2-tien attached=true}, got %+v", rs)
		}
		if len(rs.Panes) != 1 || rs.Panes[0].PaneID != "%2" {
			t.Errorf("expected only the plain-window pane %%2, got %+v", rs.Panes)
		}
	})

	t.Run("a repo spanning two sessions gets two rows", func(t *testing.T) {
		wtWindowB := GetWindowName("hire2", "other")
		statuses := []WorktreeStatus{
			{Repo: "hire2", Name: "feat"},
			{Repo: "hire2", Name: "other"},
		}
		layer := StateLayer{
			Sessions: []tmux.SessionInfo{
				{Name: "hire2"},
				{Name: "hire2-tien"},
			},
			PanesByWindow: map[string][]tmux.PaneState{
				wtWindow:  {{Session: "hire2", Window: wtWindow, PaneID: "%1"}},
				wtWindowB: {{Session: "hire2-tien", Window: wtWindowB, PaneID: "%2"}},
			},
			PanesBySession: map[string][]tmux.PaneState{
				"hire2":      {{Session: "hire2", Window: "zsh", PaneID: "%3"}},
				"hire2-tien": {{Session: "hire2-tien", Window: "vim", PaneID: "%4"}},
			},
		}

		got := RepoSessionStatuses(
			statuses, layer,
			liveFor([2]string{"hire2", "feat"}, [2]string{"hire2", "other"}),
			"",
		)
		if len(got) != 2 {
			t.Fatalf("expected 2 repo-session rows, got %d: %+v", len(got), got)
		}
		names := map[string]bool{got[0].Name: true, got[1].Name: true}
		if !names["hire2"] || !names["hire2-tien"] {
			t.Errorf("expected rows for both hire2 and hire2-tien, got %+v", got)
		}
	})

	t.Run("agent state aggregates only plain-window panes, not worktree ones", func(t *testing.T) {
		statuses := []WorktreeStatus{{Repo: "hire2", Name: "feat"}}
		layer := StateLayer{
			Sessions: []tmux.SessionInfo{{Name: "hire2-tien"}},
			PanesByWindow: map[string][]tmux.PaneState{
				wtWindow: {
					{Session: "hire2-tien", Window: wtWindow, PaneID: "%1", State: AgentStateBusy},
				},
			},
			PanesBySession: map[string][]tmux.PaneState{
				"hire2-tien": {
					{Session: "hire2-tien", Window: wtWindow, PaneID: "%1", State: AgentStateBusy},
					{Session: "hire2-tien", Window: "zsh", PaneID: "%2", State: AgentStateIdle},
				},
			},
		}

		got := RepoSessionStatuses(statuses, layer, liveFor([2]string{"hire2", "feat"}), "")
		if len(got) != 1 {
			t.Fatalf("expected 1 row, got %d", len(got))
		}
		if got[0].AgentState != AgentStateIdle {
			t.Errorf(
				"expected AgentState %q (from the plain pane only, busy worktree pane excluded), got %q",
				AgentStateIdle,
				got[0].AgentState,
			)
		}
	})

	// The session is just a container for the worktree window, which already
	// has its own row - a session row would be a second way to the same place.
	// Panes split inside the worktree window don't change that.
	t.Run("a session holding only worktree windows gets no row", func(t *testing.T) {
		statuses := []WorktreeStatus{{Repo: "hire2", Name: "feat"}}
		split := []tmux.PaneState{
			{Session: "hire2", Window: wtWindow, PaneID: "%1"},
			{Session: "hire2", Window: wtWindow, PaneID: "%2"},
		}
		layer := StateLayer{
			Sessions:       []tmux.SessionInfo{{Name: "hire2"}},
			PanesByWindow:  map[string][]tmux.PaneState{wtWindow: split},
			PanesBySession: map[string][]tmux.PaneState{"hire2": split},
		}

		if got := RepoSessionStatuses(
			statuses, layer, liveFor([2]string{"hire2", "feat"}), "",
		); len(got) != 0 {
			t.Errorf("expected no repo-session row, got %+v", got)
		}
	})

	// ctrl+t opens the dashboard as a plain "[workspace]" window in the current
	// session; looking at a worktree-only session must not give it a row.
	t.Run("the dashboard's own window does not count as a plain window", func(t *testing.T) {
		statuses := []WorktreeStatus{{Repo: "hire2", Name: "feat"}}
		layer := StateLayer{
			Sessions: []tmux.SessionInfo{{Name: "hire2"}},
			PanesByWindow: map[string][]tmux.PaneState{
				wtWindow: {{Session: "hire2", Window: wtWindow, PaneID: "%1"}},
			},
			PanesBySession: map[string][]tmux.PaneState{
				"hire2": {
					{Session: "hire2", Window: wtWindow, PaneID: "%1"},
					{Session: "hire2", Window: "[workspace]", PaneID: "%9"},
				},
			},
		}
		live := liveFor([2]string{"hire2", "feat"})

		if got := RepoSessionStatuses(statuses, layer, live, "%9"); len(got) != 0 {
			t.Errorf("expected no row with only the dashboard's window, got %+v", got)
		}
		// Same scan, dashboard not ignored: that window is a real plain window.
		if got := RepoSessionStatuses(statuses, layer, live, ""); len(got) != 1 {
			t.Errorf("expected a row when the window is not the dashboard's, got %+v", got)
		}
	})

	t.Run("a plain window next to the dashboard's still gets a row", func(t *testing.T) {
		statuses := []WorktreeStatus{{Repo: "hire2", Name: "feat"}}
		layer := StateLayer{
			Sessions: []tmux.SessionInfo{{Name: "hire2"}},
			PanesByWindow: map[string][]tmux.PaneState{
				wtWindow: {{Session: "hire2", Window: wtWindow, PaneID: "%1"}},
			},
			PanesBySession: map[string][]tmux.PaneState{
				"hire2": {
					{Session: "hire2", Window: wtWindow, PaneID: "%1"},
					{Session: "hire2", Window: "zsh", PaneID: "%2"},
					{Session: "hire2", Window: "[workspace]", PaneID: "%9"},
				},
			},
		}

		got := RepoSessionStatuses(statuses, layer, liveFor([2]string{"hire2", "feat"}), "%9")
		if len(got) != 1 {
			t.Fatalf("expected 1 row, got %+v", got)
		}
		if len(got[0].Panes) != 1 || got[0].Panes[0].PaneID != "%2" {
			t.Errorf("expected only the zsh pane %%2, got %+v", got[0].Panes)
		}
	})

	t.Run("no worktree panes report a session at all", func(t *testing.T) {
		statuses := []WorktreeStatus{{Repo: "hire2", Name: "feat"}}
		if got := RepoSessionStatuses(
			statuses,
			StateLayer{},
			liveFor([2]string{"hire2", "feat"}),
			"",
		); len(got) != 0 {
			t.Errorf("expected no repo-session rows, got %+v", got)
		}
	})

	// The defect this design specifically avoids: RepoSessionStatuses must
	// answer correctly from a layer alone, without needing statuses[].Panes
	// to have been populated by a prior ApplyTo call - the sessionsMsg path
	// (model.go) never calls ApplyTo at all.
	t.Run("works with statuses that carry no Panes field at all", func(t *testing.T) {
		statuses := []WorktreeStatus{{Repo: "hire2", Name: "feat"}} // .Panes is nil
		layer := StateLayer{
			Sessions: []tmux.SessionInfo{{Name: "hire2-tien"}},
			PanesByWindow: map[string][]tmux.PaneState{
				wtWindow: {{Session: "hire2-tien", Window: wtWindow, PaneID: "%1"}},
			},
			PanesBySession: map[string][]tmux.PaneState{
				"hire2-tien": {{Session: "hire2-tien", Window: "zsh", PaneID: "%2"}},
			},
		}

		got := RepoSessionStatuses(statuses, layer, liveFor([2]string{"hire2", "feat"}), "")
		if len(got) != 1 || got[0].Name != "hire2-tien" {
			t.Errorf("expected a row for hire2-tien derived from the layer alone, got %+v", got)
		}
	})
}
