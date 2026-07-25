package tuiworktree

import (
	"sort"
	"strings"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

type rowKind int

const (
	rowRepo rowKind = iota
	rowWorktree
	rowSession
)

type row struct {
	kind   rowKind
	repo   string
	status worktree.WorktreeStatus

	// session holds the standalone tmux session this row describes, set only
	// when kind == rowSession.
	session worktree.SessionStatus

	// worktreeCount is the number of (post-filter) worktree children under
	// this repo, set only when kind == rowRepo. Feeds the left pane's "N
	// trees" badge.
	worktreeCount int
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
		rows = append(rows, row{kind: rowRepo, repo: repo, worktreeCount: len(visible)})
		if !collapsed[repo] {
			for _, s := range visible {
				rows = append(rows, row{kind: rowWorktree, repo: repo, status: s})
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
	}

	return rows
}

// leafIndices returns indices into rows that are "leaf" data rows —
// rowWorktree and rowSession — i.e. valid cursor landing spots that carry
// selectable data, as opposed to rowRepo header rows. Used to keep the
// cursor on a valid leaf after the row list is rebuilt. Deliberately not
// named "worktreeIndices" (its pre-session name): selectedStatus still keeps
// "worktree" narrow (rowWorktree only), and this now includes rowSession too
// — reusing that name here would contradict selectedStatus's semantics.
func leafIndices(rows []row) []int {
	var out []int
	for i, r := range rows {
		if r.kind == rowWorktree || r.kind == rowSession {
			out = append(out, i)
		}
	}
	return out
}
