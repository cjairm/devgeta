package worktree

// RepoSessionStatuses and its RepoSessionStatus (ADR-0052): the sessions a
// repo's worktree windows actually live in, read straight off the same tmux
// scan the fast tick already took - never derived from a name, which
// ADR-0048 already showed can be wrong (a repo's windows can live in more
// than one session).

import (
	"sort"

	"github.com/cjairm/devgeta/internal/apps/tmux"
)

// RepoSessionStatus is one live tmux session holding repo's worktree
// windows, as the ws dashboard's repo-session row needs it (ADR-0052): drawn
// like a standalone session row (Name, Attached), with an aggregate
// AgentState and pane-row children (ADR-0008) computed from Panes alone.
//
// Panes deliberately holds only this session's PLAIN-window panes, never its
// repo-worktree ones: the repo's own worktree rows already show those, so
// counting them again here would double-report the same pane on two rows
// and let a busy worktree window's dot bleed onto a row that isn't it.
type RepoSessionStatus struct {
	Repo       string
	Name       string
	Attached   bool
	AgentState string
	Panes      []tmux.PaneState
}

// RepoSessionStatuses reduces statuses and one tmux scan (l) to one
// RepoSessionStatus per (repo, session) pair where a live session holds
// worktree windows for that repo AND at least one plain window of its own.
//
// A session holding nothing but worktree windows gets no row: each of those
// windows already has its own worktree row, so a session row would only be a
// second way to reach the same window. Windows are what count, not panes - a
// worktree window split into several panes is still just that worktree.
//
// ignorePaneID excludes the window that pane lives in, for the same reason
// PlainWindowBySession takes it: the dashboard opened with ctrl+t is itself a
// plain "[workspace]" window in the current session, and must not make a
// worktree-only session grow a row the moment you look at it. Pass "" to
// count every window.
//
// The session is read off l.PanesByWindow, keyed by GetWindowName(s.Repo,
// s.Name) - the same derivation ApplyTo itself uses - rather than off each
// status's own Panes field. Panes is populated by ApplyTo from a DIFFERENT
// point in the fast-tick cycle than this function's own callers run at (see
// model.go's sessionsMsg handler, which computes this from its own scan
// without ever calling ApplyTo), so depending on it would make the answer
// one scan late, or entirely absent on that path. Deriving the window name
// directly needs nothing but statuses' own git-derived fields and this
// scan's own layer - both already in hand at every call site.
//
// Rows come back sorted by (Repo, Name) for a deterministic, testable order;
// buildRows relies on this rather than re-sorting itself.
func RepoSessionStatuses(
	statuses []WorktreeStatus,
	l StateLayer,
	backed WorktreeWindows,
	ignorePaneID string,
) []RepoSessionStatus {
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

	ignoreSession, ignoreWindow := l.paneWindow(ignorePaneID)
	sessionInfo := make(map[string]tmux.SessionInfo, len(l.Sessions))
	for _, si := range l.Sessions {
		sessionInfo[si.Name] = si
	}

	out := make([]RepoSessionStatus, 0, len(seen))
	for rs := range seen {
		si, ok := sessionInfo[rs.session]
		if !ok {
			// A pane reported a session the session list doesn't have - both
			// come from the same scan, so this should not happen, but a
			// session that died between the two tmux calls is not worth a
			// row for.
			continue
		}
		var plainPanes []tmux.PaneState
		for _, p := range l.PanesBySession[rs.session] {
			if backed.backs(p.Window) {
				continue
			}
			if rs.session == ignoreSession && p.Window == ignoreWindow {
				continue
			}
			plainPanes = append(plainPanes, p)
		}
		if len(plainPanes) == 0 {
			continue
		}
		out = append(out, RepoSessionStatus{
			Repo:       rs.repo,
			Name:       rs.session,
			Attached:   si.Attached,
			AgentState: aggregatePaneStates(plainPanes),
			Panes:      plainPanes,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].Name < out[j].Name
	})
	return out
}
