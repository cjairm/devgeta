package worktree

// RepoWindowStatuses and its RepoWindowStatus (ADR-0052, amended): the plain
// windows a repo's sessions hold, read straight off the same tmux scan the
// fast tick already took - never derived from a name, which ADR-0048 already
// showed can be wrong (a repo's windows can live in more than one session).

import (
	"sort"

	"github.com/cjairm/devgeta/internal/apps/tmux"
)

// RepoWindowStatus is one plain (non-worktree) tmux window in a session that
// holds repo's worktree windows, as the ws dashboard's window row needs it.
//
// Panes holds only this window's own panes. A worktree window's panes are
// never here: the worktree row already shows them, and counting them again
// would let a busy worktree's state bleed onto a row that isn't it.
type RepoWindowStatus struct {
	Repo    string
	Session string
	Window  string
	// WindowID is tmux's #{window_id}. It, not Window, identifies the row and
	// is what enter switches to: two windows can share a name.
	WindowID   string
	AgentState string
	Panes      []tmux.PaneState
}

// RepoWindowStatuses reduces statuses and one tmux scan (l) to one
// RepoWindowStatus per plain window in every session that holds a worktree
// window of a listed repo. A window in a session serving two repos is listed
// under each.
//
// A session holding nothing but worktree windows yields no rows: each of
// those windows already has its own worktree row. Windows are what count, not
// panes - a worktree window split into several panes is still just that
// worktree.
//
// ignorePaneID excludes the window that pane lives in: the dashboard opened
// with ctrl+t is itself a plain "[workspace]" window in the current session
// and must not list itself. Pass "" to count every window.
//
// The session is read off l.PanesByWindow, keyed by GetWindowName(s.Repo,
// s.Name) - the same derivation ApplyTo itself uses - rather than off each
// status's own Panes field. Panes is populated by ApplyTo from a DIFFERENT
// point in the fast-tick cycle than this function's callers run at (see
// model.go's sessionsMsg handler, which computes this from its own scan
// without ever calling ApplyTo), so depending on it would make the answer one
// scan late, or entirely absent on that path.
//
// Rows come back sorted by (Repo, Session), each session's windows in tmux's
// own order; buildRows relies on this rather than re-sorting itself.
func RepoWindowStatuses(
	statuses []WorktreeStatus,
	l StateLayer,
	backed WorktreeWindows,
	ignorePaneID string,
) []RepoWindowStatus {
	type repoSession struct{ repo, session string }
	seen := map[repoSession]bool{}
	for _, s := range statuses {
		window := GetWindowName(s.Repo, s.Name)
		for _, p := range l.PanesByWindow[window] {
			if p.Session == "" {
				continue
			}
			seen[repoSession{s.Repo, p.Session}] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}

	ignored, hasIgnored := l.paneNamed(ignorePaneID)

	keys := make([]repoSession, 0, len(seen))
	for rs := range seen {
		keys = append(keys, rs)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].repo != keys[j].repo {
			return keys[i].repo < keys[j].repo
		}
		return keys[i].session < keys[j].session
	})

	var out []RepoWindowStatus
	for _, rs := range keys {
		var order []int // indexes into out, in first-seen window order
		byWindow := map[[2]string]int{}
		for _, p := range l.PanesBySession[rs.session] {
			if backed.backs(p.Window) {
				continue
			}
			if hasIgnored && sameWindow(p, ignored) {
				continue
			}
			// The id is what tells two windows apart; a pane with none (a
			// scan that predates it) falls back to its window's name.
			key := [2]string{p.WindowID, p.Window}
			i, ok := byWindow[key]
			if !ok {
				i = len(out)
				byWindow[key] = i
				order = append(order, i)
				out = append(out, RepoWindowStatus{
					Repo:     rs.repo,
					Session:  rs.session,
					Window:   p.Window,
					WindowID: p.WindowID,
				})
			}
			out[i].Panes = append(out[i].Panes, p)
		}
		for _, i := range order {
			out[i].AgentState = aggregatePaneStates(out[i].Panes)
		}
	}
	return out
}

// sameWindow reports whether a and b are panes of the same tmux window: by id
// when both carry one, otherwise by session and name.
func sameWindow(a, b tmux.PaneState) bool {
	if a.WindowID != "" && b.WindowID != "" {
		return a.WindowID == b.WindowID
	}
	return a.Session == b.Session && a.Window == b.Window
}
