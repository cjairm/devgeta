package tuiworktree

import (
	"sort"
	"strings"

	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

type rowKind int

const (
	rowRepo rowKind = iota
	rowWorktree
	rowSession
	rowPane
)

type row struct {
	kind   rowKind
	repo   string
	status worktree.WorktreeStatus

	// session holds the standalone tmux session this row describes, set only
	// when kind == rowSession.
	session worktree.SessionStatus

	// pane holds the individual tmux pane this row describes, set only when
	// kind == rowPane.
	pane tmux.PaneState

	// worktreeCount is the number of (post-filter) worktree children under
	// this repo, set only when kind == rowRepo. Feeds the left pane's "N
	// trees" badge.
	worktreeCount int

	// agentState is the repo's children's AgentState values aggregated via
	// worktree.AggregateAgentState (blocked > error > idle > busy > ""), set
	// only when kind == rowRepo. Lets a collapsed repo header still surface
	// an urgent child state instead of hiding it.
	agentState string
}

// qualifiesForPaneRows reports whether a worktree/session parent has 2+ panes
// with a non-empty agent state - the threshold at which drilling down into
// individual panes is useful (ADR-0008 §3). A single stateful pane already
// shows fully on the parent's own dot; expansion only earns its keep once
// there's more than one pane's worth of information the parent's aggregate
// would otherwise hide.
func qualifiesForPaneRows(panes []tmux.PaneState) bool {
	count := 0
	for _, p := range panes {
		if p.State != "" {
			count++
		}
	}
	return count >= 2
}

// paneParentKey returns the collapse-map key for a row that can host pane
// children (rowWorktree/rowSession) and whether it qualifies for expansion at
// all (see qualifiesForPaneRows). ok=false for every other row kind, or a
// worktree/session with fewer than 2 stateful panes.
func paneParentKey(r row) (key string, qualifies bool) {
	switch r.kind {
	case rowWorktree:
		return "worktree:" + r.status.TmuxWindow, qualifiesForPaneRows(r.status.Panes)
	case rowSession:
		return "session:" + r.session.Name, qualifiesForPaneRows(r.session.Panes)
	}
	return "", false
}

// chevronGlyphFor returns the expand/collapse indicator for a row that can
// host pane children: "▼" if expanded, "▶" if collapsed, " " (reserving the
// column) if the row doesn't qualify for pane-row expansion at all.
func chevronGlyphFor(r row, collapsed map[string]bool) string {
	key, qualifies := paneParentKey(r)
	if !qualifies {
		return " "
	}
	if collapsed[key] {
		return "▶"
	}
	return "▼"
}

// enclosingPaneParent scans backward from row index i for the nearest
// preceding rowWorktree/rowSession — the parent that owns row i's pane
// children, mirroring the order buildRows emits them in (parent immediately
// followed by its panes). Stops and returns ok=false if it hits a row that
// isn't a pane before finding one, which should not happen given emission
// order but guards against it anyway.
func enclosingPaneParent(rows []row, i int) (row, bool) {
	for j := i - 1; j >= 0; j-- {
		if rows[j].kind == rowWorktree || rows[j].kind == rowSession {
			return rows[j], true
		}
		if rows[j].kind != rowPane {
			break
		}
	}
	return row{}, false
}

// sameParentRow reports whether a and b are the same rowWorktree/rowSession
// parent, identified the same way as paneParentKey's key (TmuxWindow /
// session Name) - used to relocate a parent row by identity after a rebuild,
// since a rebuild can shift row positions.
func sameParentRow(a, b row) bool {
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case rowWorktree:
		return a.status.TmuxWindow == b.status.TmuxWindow
	case rowSession:
		return a.session.Name == b.session.Name
	}
	return false
}

// buildRows groups statuses by repo (alpha-sorted), applies filter, respects
// collapsed map, then appends sessions (standalone tmux sessions with no
// worktree-backed window) as leaf rows after every repo group — one flat
// list: repo workspaces first, then plain sessions.
func buildRows(
	statuses []worktree.WorktreeStatus,
	sessions []worktree.SessionStatus,
	collapsed map[string]bool,
	filter string,
) []row {
	// Group by repo
	groups := map[string][]worktree.WorktreeStatus{}
	for _, s := range statuses {
		groups[s.Repo] = append(groups[s.Repo], s)
	}

	// Sort repos
	repos := make([]string, 0, len(groups))
	for r := range groups {
		repos = append(repos, r)
	}
	sort.Strings(repos)

	filter = strings.ToLower(filter)
	var rows []row
	for _, repo := range repos {
		children := groups[repo]
		// Filter: keep only children that match
		var visible []worktree.WorktreeStatus
		for _, s := range children {
			if filter == "" || strings.Contains(strings.ToLower(repo+"/"+s.Name), filter) {
				visible = append(visible, s)
			}
		}
		if len(visible) == 0 {
			continue
		}
		childStates := make([]string, 0, len(visible))
		for _, s := range visible {
			childStates = append(childStates, s.AgentState)
		}
		rows = append(rows, row{
			kind:          rowRepo,
			repo:          repo,
			worktreeCount: len(visible),
			agentState:    worktree.AggregateAgentState(childStates),
		})
		if !collapsed[repo] {
			for _, s := range visible {
				rows = append(rows, row{kind: rowWorktree, repo: repo, status: s})
				if qualifiesForPaneRows(s.Panes) {
					key := "worktree:" + s.TmuxWindow
					if !collapsed[key] {
						for _, p := range s.Panes {
							rows = append(rows, row{kind: rowPane, pane: p})
						}
					}
				}
			}
		}
	}

	// Plain sessions: alpha-sorted (mirrors the repo ordering above) and
	// appended after every repo group so they read as trailing leaves of one
	// unified list. They have no children and are unaffected by any repo's
	// collapsed state.
	sorted := make([]worktree.SessionStatus, len(sessions))
	copy(sorted, sessions)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, s := range sorted {
		if filter != "" && !strings.Contains(strings.ToLower(s.Name), filter) {
			continue
		}
		rows = append(rows, row{kind: rowSession, session: s})
		if qualifiesForPaneRows(s.Panes) {
			key := "session:" + s.Name
			if !collapsed[key] {
				for _, p := range s.Panes {
					rows = append(rows, row{kind: rowPane, pane: p})
				}
			}
		}
	}

	return rows
}

// leafIndices returns indices into rows that are "leaf" data rows —
// rowWorktree, rowSession, and rowPane — i.e. valid cursor landing spots that
// carry selectable data, as opposed to rowRepo header rows. Used to keep the
// cursor on a valid leaf after the row list is rebuilt. Deliberately not
// named "worktreeIndices" (its pre-session name): selectedStatus still keeps
// "worktree" narrow (rowWorktree only), and this now includes rowSession and
// rowPane too — reusing that name here would contradict selectedStatus's
// semantics.
func leafIndices(rows []row) []int {
	var out []int
	for i, r := range rows {
		if r.kind == rowWorktree || r.kind == rowSession || r.kind == rowPane {
			out = append(out, i)
		}
	}
	return out
}
