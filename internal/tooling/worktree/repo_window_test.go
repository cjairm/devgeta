package worktree

// Tests for RepoWindowStatuses (ADR-0052, amended): the rows the ws dashboard
// draws under each repo header, one per plain window in a session that also
// holds one of the repo's worktree windows.

import (
	"testing"

	"github.com/cjairm/devgeta/internal/apps/tmux"
)

func TestRepoWindowStatuses(t *testing.T) {
	wtWindow := GetWindowName("hire2", "feat")
	statuses := []WorktreeStatus{{Repo: "hire2", Name: "feat"}}
	live := liveFor([2]string{"hire2", "feat"})

	// pane builds one scanned pane; the window id is derived from the window
	// name unless the caller needs two windows to share a name.
	pane := func(session, window, windowID, paneID string, state string) tmux.PaneState {
		return tmux.PaneState{
			Session: session, Window: window, WindowID: windowID, PaneID: paneID, State: state,
		}
	}
	layerOf := func(session string, panes ...tmux.PaneState) StateLayer {
		l := StateLayer{
			Sessions:       []tmux.SessionInfo{{Name: session, Attached: true}},
			PanesByWindow:  map[string][]tmux.PaneState{},
			PanesBySession: map[string][]tmux.PaneState{session: panes},
		}
		for _, p := range panes {
			l.PanesByWindow[p.Window] = append(l.PanesByWindow[p.Window], p)
		}
		return l
	}

	t.Run("one row per plain window, in tmux's window order", func(t *testing.T) {
		layer := layerOf("hire2",
			pane("hire2", "node", "@1", "%1", ""),
			pane("hire2", "node", "@1", "%2", ""),
			pane("hire2", "node", "@1", "%3", ""),
			pane("hire2", "zsh", "@2", "%4", ""),
			pane("hire2", wtWindow, "@3", "%5", ""),
		)

		got := RepoWindowStatuses(statuses, layer, live, "")
		if len(got) != 2 {
			t.Fatalf("expected 2 window rows (node, zsh), got %d: %+v", len(got), got)
		}
		if got[0].Window != "node" || got[1].Window != "zsh" {
			t.Errorf("expected node then zsh, got %q then %q", got[0].Window, got[1].Window)
		}
		if got[0].Repo != "hire2" || got[0].Session != "hire2" || got[0].WindowID != "@1" {
			t.Errorf("expected {hire2 hire2 @1}, got %+v", got[0])
		}
		if len(got[0].Panes) != 3 {
			t.Errorf("expected node's 3 panes on its own row, got %+v", got[0].Panes)
		}
	})

	t.Run("two windows that share a name stay two rows", func(t *testing.T) {
		layer := layerOf("hire2",
			pane("hire2", "zsh", "@1", "%1", ""),
			pane("hire2", "zsh", "@2", "%2", ""),
			pane("hire2", wtWindow, "@3", "%3", ""),
		)

		got := RepoWindowStatuses(statuses, layer, live, "")
		if len(got) != 2 || got[0].WindowID != "@1" || got[1].WindowID != "@2" {
			t.Fatalf("expected windows @1 and @2 as separate rows, got %+v", got)
		}
	})

	t.Run("agent state aggregates the window's own panes only", func(t *testing.T) {
		layer := layerOf("hire2",
			pane("hire2", wtWindow, "@3", "%1", AgentStateBusy),
			pane("hire2", "zsh", "@2", "%2", AgentStateIdle),
		)

		got := RepoWindowStatuses(statuses, layer, live, "")
		if len(got) != 1 || got[0].AgentState != AgentStateIdle {
			t.Errorf("expected one row with state %q, got %+v", AgentStateIdle, got)
		}
	})

	t.Run("a repo spanning two sessions lists each session's windows", func(t *testing.T) {
		two := []WorktreeStatus{{Repo: "hire2", Name: "feat"}, {Repo: "hire2", Name: "other"}}
		wtOther := GetWindowName("hire2", "other")
		layer := StateLayer{
			Sessions: []tmux.SessionInfo{{Name: "hire2"}, {Name: "hire2-tien"}},
			PanesByWindow: map[string][]tmux.PaneState{
				wtWindow: {pane("hire2", wtWindow, "@1", "%1", "")},
				wtOther:  {pane("hire2-tien", wtOther, "@4", "%4", "")},
			},
			PanesBySession: map[string][]tmux.PaneState{
				"hire2":      {pane("hire2", wtWindow, "@1", "%1", ""), pane("hire2", "zsh", "@2", "%2", "")},
				"hire2-tien": {pane("hire2-tien", wtOther, "@4", "%4", ""), pane("hire2-tien", "vim", "@5", "%5", "")},
			},
		}

		got := RepoWindowStatuses(two, layer, liveFor([2]string{"hire2", "feat"}, [2]string{"hire2", "other"}), "")
		if len(got) != 2 || got[0].Window != "zsh" || got[1].Window != "vim" {
			t.Fatalf("expected zsh (hire2) then vim (hire2-tien), got %+v", got)
		}
		if got[0].Session != "hire2" || got[1].Session != "hire2-tien" {
			t.Errorf("windows must carry their own session, got %+v", got)
		}
	})

	t.Run("a session holding only worktree windows gets no rows", func(t *testing.T) {
		layer := layerOf("hire2",
			pane("hire2", wtWindow, "@1", "%1", ""),
			pane("hire2", wtWindow, "@1", "%2", ""),
		)
		if got := RepoWindowStatuses(statuses, layer, live, ""); len(got) != 0 {
			t.Errorf("expected no rows, got %+v", got)
		}
	})

	// ctrl+t opens the dashboard as a plain "[workspace]" window in the current
	// session; it must never list itself.
	t.Run("the dashboard's own window is left out", func(t *testing.T) {
		layer := layerOf("hire2",
			pane("hire2", wtWindow, "@1", "%1", ""),
			pane("hire2", "zsh", "@2", "%2", ""),
			pane("hire2", "[workspace]", "@9", "%9", ""),
		)

		got := RepoWindowStatuses(statuses, layer, live, "%9")
		if len(got) != 1 || got[0].Window != "zsh" {
			t.Errorf("expected only zsh, got %+v", got)
		}
		if got := RepoWindowStatuses(statuses, layer, live, ""); len(got) != 2 {
			t.Errorf("with nothing ignored both plain windows count, got %+v", got)
		}
	})

	t.Run("only the dashboard's window is left out when another shares its name", func(t *testing.T) {
		layer := layerOf("hire2",
			pane("hire2", wtWindow, "@1", "%1", ""),
			pane("hire2", "[workspace]", "@8", "%8", ""),
			pane("hire2", "[workspace]", "@9", "%9", ""),
		)

		got := RepoWindowStatuses(statuses, layer, live, "%9")
		if len(got) != 1 || got[0].WindowID != "@8" {
			t.Errorf("expected only @8 to remain, got %+v", got)
		}
	})

	t.Run("no worktree panes report a session at all", func(t *testing.T) {
		if got := RepoWindowStatuses(statuses, StateLayer{}, live, ""); len(got) != 0 {
			t.Errorf("expected no rows, got %+v", got)
		}
	})

	// The sessionsMsg path never calls ApplyTo, so statuses carry no Panes.
	t.Run("works with statuses that carry no Panes field at all", func(t *testing.T) {
		layer := layerOf("hire2-tien",
			pane("hire2-tien", "zsh", "@2", "%2", ""),
		)
		layer.PanesByWindow[wtWindow] = []tmux.PaneState{pane("hire2-tien", wtWindow, "@1", "%1", "")}

		got := RepoWindowStatuses(statuses, layer, live, "")
		if len(got) != 1 || got[0].Session != "hire2-tien" {
			t.Errorf("expected a zsh row in hire2-tien from the layer alone, got %+v", got)
		}
	})
}
