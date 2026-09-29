package tuiworktree

import (
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

// focusSection says which of the dashboard's two left-column sections the
// cursor is logically in (ADR-0056). Spaces is the zero value, so a Model's
// zero value (and every test model that never touches it) starts there,
// matching today's single-list behavior exactly.
type focusSection int

const (
	sectionSpaces focusSection = iota
	sectionAgents
)

// agentRow is one row in the dashboard's agents section (ADR-0056): one
// agent pane (ADR-0055), its display state, its location label, and the
// coder's own name. Unlike a space `row`, an agent row is never a parent
// (it hosts no children) and is never affected by fold/collapse state on the
// spaces side — the two lists are entirely independent (ADR-0056). Row
// identity is pane.PaneID (ADR-0056): stable across rebuilds and independent
// of label, which changes on a worktree rename even though the pane doesn't.
type agentRow struct {
	pane  tmux.PaneState
	state tuicomponents.AgentRowState
	// label is "repo/worktree" for a pane in a worktree window, otherwise
	// the session name (repo-session or standalone) that owns the pane.
	label string
	// kind is the coder's own word for what it is ("claude", "opencode") —
	// tmux.PaneState.Kind, copied here so a caller never has to reach back
	// into pane for it.
	kind string
	// worktree is the WorktreeStatus this pane's window belongs to, for the
	// right pane's diff (ADR-0056: "the right pane shows the agent's
	// worktree diff"). isWorktree is false (worktree left at its zero
	// value) for a repo-session or standalone-session pane, which has no
	// diff to show.
	worktree   worktree.WorktreeStatus
	isWorktree bool
}

// agentRowStatesByUrgency is ADR-0056's urgency order, most urgent first:
// blocked > error > done > working > idle - ADR-0005's aggregation order
// with "idle" split into done (unseen) and idle (seen/never prompted). The
// list sort and the folded bar's per-state counts both read it.
var agentRowStatesByUrgency = []tuicomponents.AgentRowState{
	tuicomponents.AgentRowBlocked,
	tuicomponents.AgentRowError,
	tuicomponents.AgentRowDone,
	tuicomponents.AgentRowWorking,
	tuicomponents.AgentRowIdle,
}

// agentRowStateRank is agentRowStatesByUrgency as a lookup: lower ranks sort
// first.
var agentRowStateRank = func() map[tuicomponents.AgentRowState]int {
	rank := make(map[tuicomponents.AgentRowState]int, len(agentRowStatesByUrgency))
	for i, st := range agentRowStatesByUrgency {
		rank[st] = i
	}
	return rank
}()

// paneIndexNum parses a PaneIndex string ("0", "1", "12", ...) for numeric
// tie-breaking - a plain string compare would sort "10" before "2". Any
// unparseable value (should not happen; tmux always reports a numeric
// pane_index) sorts as 0 rather than panicking or dropping the row.
func paneIndexNum(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// buildAgentRows builds the dashboard's flat agents-section rows from the
// same pane layer the fast tick already scans (ADR-0024: no new exec here),
// across every repo's worktrees, every repo's live sessions (ADR-0052), and
// every standalone session. A pane that fails tmux.PaneState.IsAgent() (no
// kind, or a stale kind left behind by a coder that has since exited into a
// plain shell — ADR-0055) is skipped entirely; it has no agent row.
//
// filter (Step 10, ADR-0056), when non-empty, keeps only rows whose
// location label, coder kind, or display state word contains it
// case-insensitively - "" (no active filter, or the section is folded and
// so isn't searched at all - the caller's job to decide that) matches
// everything, mirroring buildRows' own empty-filter convention.
//
// Sorted by ADR-0056's rule: urgency first (agentRowStateRank), then label,
// then pane index numerically — so two agents in the same window (e.g. a
// claude-nvim-style split) order by which pane they're actually in.
func buildAgentRows(
	statuses []worktree.WorktreeStatus,
	sessions []worktree.SessionStatus,
	repoSessions []worktree.RepoSessionStatus,
	filter string,
) []agentRow {
	filter = strings.ToLower(filter)
	var rows []agentRow

	for _, s := range statuses {
		label := s.Repo + "/" + s.Name
		for _, p := range s.Panes {
			if !p.IsAgent() {
				continue
			}
			rows = append(rows, agentRow{
				pane:       p,
				state:      tuicomponents.AgentRowStateFor(p.State),
				label:      label,
				kind:       p.Kind,
				worktree:   s,
				isWorktree: true,
			})
		}
	}

	for _, rs := range repoSessions {
		for _, p := range rs.Panes {
			if !p.IsAgent() {
				continue
			}
			rows = append(rows, agentRow{
				pane:  p,
				state: tuicomponents.AgentRowStateFor(p.State),
				label: rs.Name,
				kind:  p.Kind,
			})
		}
	}

	for _, s := range sessions {
		for _, p := range s.Panes {
			if !p.IsAgent() {
				continue
			}
			rows = append(rows, agentRow{
				pane:  p,
				state: tuicomponents.AgentRowStateFor(p.State),
				label: s.Name,
				kind:  p.Kind,
			})
		}
	}

	if filter != "" {
		filtered := rows[:0]
		for _, r := range rows {
			if strings.Contains(strings.ToLower(r.label), filter) ||
				strings.Contains(strings.ToLower(r.kind), filter) ||
				strings.Contains(tuicomponents.AgentRowWord(r.state), filter) {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := agentRowStateRank[rows[i].state], agentRowStateRank[rows[j].state]
		if ri != rj {
			return ri < rj
		}
		if rows[i].label != rows[j].label {
			return rows[i].label < rows[j].label
		}
		return paneIndexNum(rows[i].pane.PaneIndex) < paneIndexNum(rows[j].pane.PaneIndex)
	})

	return rows
}

// agentPaneClosedMsg reports that an agent row's pane was closed.
type agentPaneClosedMsg struct{ label string }

// handleCloseAgentPane is d on an agent row: a two-press confirm, armed and
// cleared the same way handleKillSession is, that closes that agent's pane
// and nothing else. An agent row is one pane (ADR-0056), so a split's other
// panes (an editor next to the coder) stay; tmux drops the window with its
// last pane, and the session with its last window.
func (m Model) handleCloseAgentPane() (tea.Model, tea.Cmd) {
	if m.agentCursor < 0 || m.agentCursor >= len(m.agentRows) {
		return m, nil
	}
	r := m.agentRows[m.agentCursor]
	if m.pendingCloseAgent != r.pane.PaneID {
		m.pendingCloseAgent = r.pane.PaneID
		return m, nil
	}
	m.pendingCloseAgent = ""

	killPaneFn := m.killPaneFn
	paneID, label := r.pane.PaneID, r.label
	m.status = actionStatus("closing agent", label)
	return m, func() tea.Msg {
		if err := killPaneFn(paneID); err != nil {
			return statusMsg("close agent failed: " + err.Error())
		}
		return agentPaneClosedMsg{label: label}
	}
}
