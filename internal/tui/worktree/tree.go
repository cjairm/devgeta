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
	rowRepoSession
	rowSession
	// rowSessionsHeader is the dim "sessions" section label buildRows emits
	// once, right before the standalone-session rows, when there is at least
	// one to show (layout B). It hosts no children and is never a cursor
	// stop, so navigableIndices and rowKey both leave it out entirely.
	rowSessionsHeader
	rowPane
)

type row struct {
	kind   rowKind
	repo   string
	status worktree.WorktreeStatus

	// session holds the standalone tmux session this row describes, set only
	// when kind == rowSession.
	session worktree.SessionStatus

	// repoSession holds the live session holding repo's worktree windows this
	// row describes (ADR-0052), set only when kind == rowRepoSession.
	repoSession worktree.RepoSessionStatus

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

// repoKey is rowKey's rowRepo case, exposed separately for the call sites
// that only have the repo name in hand rather than a full row — the h/l/z
// fold handlers and buildRows.
func repoKey(repo string) string {
	return "repo:" + repo
}

// rowKey identifies a row by stable identity rather than by the tmux window
// or session name it happens to display (B5): "repo:<folder name>" for a
// repo header, "wt:<path>" for a worktree, "sess:<name>" for a session (a
// repo-session row uses the exact same prefix as a standalone one, since a
// session name is unique on the server - ADR-0052), "pane:<paneID>" for a
// pane. The fold map, sameParentRow and paneParentKey all key off this one
// function so they can't drift apart.
//
// A worktree's TmuxWindow ("wt-<repo>-<name>") is not unique: "taskqueue" +
// "groups-x" and "taskqueue-groups" + "x" both derive
// "wt-taskqueue-groups-x". Keying by Path instead can't collide — no two
// worktrees ever share a filesystem path.
func rowKey(r row) string {
	switch r.kind {
	case rowRepo:
		return repoKey(r.repo)
	case rowWorktree:
		return "wt:" + r.status.Path
	case rowRepoSession:
		return "sess:" + r.repoSession.Name
	case rowSession:
		return "sess:" + r.session.Name
	case rowPane:
		return "pane:" + r.pane.PaneID
	}
	return ""
}

// paneParentKey returns the collapse-map key for a row that can host pane
// children (rowWorktree/rowRepoSession/rowSession) and whether it qualifies
// for expansion at all (see qualifiesForPaneRows). ok=false for every other
// row kind, or a parent with fewer than 2 stateful panes.
func paneParentKey(r row) (key string, qualifies bool) {
	switch r.kind {
	case rowWorktree:
		return rowKey(r), qualifiesForPaneRows(r.status.Panes)
	case rowRepoSession:
		return rowKey(r), qualifiesForPaneRows(r.repoSession.Panes)
	case rowSession:
		return rowKey(r), qualifiesForPaneRows(r.session.Panes)
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
// preceding rowWorktree/rowRepoSession/rowSession — the parent that owns row
// i's pane children, mirroring the order buildRows emits them in (parent
// immediately followed by its panes). Stops and returns ok=false if it hits a
// row that isn't a pane before finding one, which should not happen given
// emission order but guards against it anyway.
func enclosingPaneParent(rows []row, i int) (row, bool) {
	for j := i - 1; j >= 0; j-- {
		if rows[j].kind == rowWorktree || rows[j].kind == rowRepoSession ||
			rows[j].kind == rowSession {
			return rows[j], true
		}
		if rows[j].kind != rowPane {
			break
		}
	}
	return row{}, false
}

// sameParentRow reports whether a and b are the same
// rowWorktree/rowRepoSession/rowSession parent, identified by rowKey (the
// same identity paneParentKey uses) - used to relocate a parent row after a
// rebuild, since a rebuild can shift row positions.
func sameParentRow(a, b row) bool {
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case rowWorktree, rowRepoSession, rowSession:
		return rowKey(a) == rowKey(b)
	}
	return false
}

// buildRows groups statuses by repo (alpha-sorted), applies filter, respects
// collapsed map, then appends sessions (standalone tmux sessions with no
// worktree-backed window) as leaf rows after every repo group — one flat
// list: repo workspaces first, then plain sessions.
//
// repoSessions supplies each repo's live sessions (ADR-0052): sorted by
// (Repo, Name) already (RepoSessionStatuses' own contract), so grouping them
// here is a single linear pass, not a re-sort.
func buildRows(
	statuses []worktree.WorktreeStatus,
	sessions []worktree.SessionStatus,
	repoSessions []worktree.RepoSessionStatus,
	collapsed map[string]bool,
	filter string,
) []row {
	// Group by repo
	groups := map[string][]worktree.WorktreeStatus{}
	for _, s := range statuses {
		groups[s.Repo] = append(groups[s.Repo], s)
	}
	repoSessionsByRepo := map[string][]worktree.RepoSessionStatus{}
	for _, rs := range repoSessions {
		repoSessionsByRepo[rs.Repo] = append(repoSessionsByRepo[rs.Repo], rs)
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
		if !collapsed[repoKey(repo)] {
			// Session rows first (ADR-0052): the repo's live sessions, before
			// its worktree rows, so a repo reads as "where it lives, then
			// what's in it."
			for _, rs := range repoSessionsByRepo[repo] {
				sessRow := row{kind: rowRepoSession, repo: repo, repoSession: rs}
				rows = append(rows, sessRow)
				if qualifiesForPaneRows(rs.Panes) {
					if !collapsed[rowKey(sessRow)] {
						for _, p := range rs.Panes {
							rows = append(rows, row{kind: rowPane, pane: p})
						}
					}
				}
			}
			for _, s := range visible {
				wtRow := row{kind: rowWorktree, repo: repo, status: s}
				rows = append(rows, wtRow)
				if qualifiesForPaneRows(s.Panes) {
					if !collapsed[rowKey(wtRow)] {
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
	sorted := make([]worktree.SessionStatus, 0, len(sessions))
	for _, s := range sessions {
		if filter == "" || strings.Contains(strings.ToLower(s.Name), filter) {
			sorted = append(sorted, s)
		}
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	if len(sorted) > 0 {
		rows = append(rows, row{kind: rowSessionsHeader})
	}
	for _, s := range sorted {
		sessRow := row{kind: rowSession, session: s}
		rows = append(rows, sessRow)
		if qualifiesForPaneRows(s.Panes) {
			if !collapsed[rowKey(sessRow)] {
				for _, p := range s.Panes {
					rows = append(rows, row{kind: rowPane, pane: p})
				}
			}
		}
	}

	return rows
}

// Removed: leafIndices, which returned every row EXCEPT a repo header and was
// the set a rebuild clamped the cursor to. Having it alongside
// navigableIndices meant two answers to "where may the cursor sit", and they
// disagreed about headers — so a rebuild (every 3-second tick, every filter
// keystroke, every collapse) pulled the cursor off a header the user had just
// navigated to. Model.navigableIndices is now the only definition; keeping a
// second one is what let them drift in the first place.
