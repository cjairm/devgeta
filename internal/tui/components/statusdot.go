package tuicomponents

import "github.com/cjairm/devgeta/internal/tooling/worktree"

type SessionState int

const (
	StateRunning     SessionState = iota
	StateNeedsReview              // session finished, fired by Stop hook
	StateDirty                    // uncommitted changes, no active session
	StateNoSession                // worktree exists, no tmux window
	StateBlocked                  // agent is blocked, needs user intervention
	StateError                    // agent encountered an error
)

// SessionStateFromAgent derives state from a boolean window-active flag and
// aggregated agent state, incorporating the agent state from window panes.
// agentState should be one of the AgentState* constants from the worktree package,
// or "" if no agent has reported a state (equivalent to StateRunning for active windows).
func SessionStateFromAgent(
	windowActive bool,
	agentState string,
	dirtyCount int,
) SessionState {
	if windowActive {
		switch agentState {
		case worktree.AgentStateBlocked:
			return StateBlocked
		case worktree.AgentStateError:
			return StateError
		case worktree.AgentStateIdle:
			return StateNeedsReview
		default: // worktree.AgentStateBusy, or "" (no agent ever reported here)
			return StateRunning
		}
	}
	if dirtyCount > 0 {
		return StateDirty
	}
	return StateNoSession
}

// SessionStateFromWorktree is a convenience wrapper over SessionStateFromAgent
// that extracts the window-active flag from WorktreeStatus.
func SessionStateFromWorktree(
	s worktree.WorktreeStatus,
	agentState string,
	dirtyCount int,
) SessionState {
	return SessionStateFromAgent(s.WindowActive, agentState, dirtyCount)
}

// StatusDot returns a styled glyph string with ANSI color codes.
// Use in standalone contexts (non-selected rows, status bars).
// Do NOT nest inside a parent style.Render() — use StatusGlyph instead.
func (p *Palette) StatusDot(state SessionState) string {
	g := p.StatusGlyph(state)
	switch state {
	case StateRunning:
		return p.Running.Render(g)
	case StateNeedsReview:
		return p.NeedsReview.Render(g)
	case StateDirty:
		return p.Dirty.Render(g)
	case StateBlocked:
		return p.Blocked.Render(g)
	case StateError:
		return p.Error.Render(g)
	default:
		return p.NoSession.Render(g)
	}
}

// StatusGlyph returns the raw glyph character with no ANSI styling.
// Use when the caller wraps the result in a parent style (e.g. Selected.Render(...)).
// StateRunning and StateDirty intentionally share "●" — color is the differentiator.
func (p *Palette) StatusGlyph(state SessionState) string {
	switch state {
	case StateRunning:
		return "●"
	case StateNeedsReview:
		return "◆"
	case StateDirty:
		return "●"
	case StateBlocked:
		return "!"
	case StateError:
		return "✕"
	default:
		return "○"
	}
}

// BranchLabel returns the styled ∕ branch glyph.
func (p *Palette) BranchLabel() string {
	return p.BranchGlyph.Render("∕")
}

// SessionGlyph returns the raw glyph for a standalone tmux session row: a
// square (■ attached, □ detached), deliberately a different shape from the ●/○
// circle StatusGlyph uses for worktrees so the two row kinds read as distinct
// at a glance, not just by their trailing label. Use when the caller wraps the
// result in a parent style (e.g. Selected.Render(...)); use SessionDot for a
// standalone styled glyph.
func (p *Palette) SessionGlyph(attached bool) string {
	if attached {
		return "■"
	}
	return "□"
}

// SessionDot returns the styled session glyph: attached in the same green as a
// running worktree, detached in the same dim gray as a worktree with no tmux
// window — so color still reads as activity while the square shape signals
// "session". Do NOT nest inside a parent style.Render() — use SessionGlyph.
func (p *Palette) SessionDot(attached bool) string {
	g := p.SessionGlyph(attached)
	if attached {
		return p.Running.Render(g)
	}
	return p.NoSession.Render(g)
}

// AgentRowState is the display state for one row in the dashboard's agents
// section (ADR-0055) — distinct from SessionState, which governs space rows
// (worktree/session/pane): an agent row's five states come from a raw
// @dg_agent_state value (via AgentRowStateFor) rather than the window/dirty
// inputs SessionStateFromAgent uses, and "done" (finished, unseen) is
// deliberately a different state from "idle" (finished and seen, or never
// prompted) even though both derive from an otherwise-quiet pane — collapsing
// them the way SessionStateFromAgent's default branch does would lose
// exactly the distinction the agents section exists to show.
type AgentRowState int

const (
	AgentRowBlocked AgentRowState = iota
	AgentRowError
	AgentRowDone
	AgentRowWorking
	AgentRowIdle
)

// AgentRowStateFor maps an agent pane's raw @dg_agent_state value to its
// AgentRowState, per ADR-0055's table. Callers first confirm the pane IS an
// agent (tmux.PaneState.IsAgent) — this only maps the state string. Any
// value outside ADR-0005's vocabulary (including "") falls back to
// AgentRowIdle, the same "unrecognized reads as unset" tolerance
// tmux.AggregateAgentState already uses for the same raw values.
func AgentRowStateFor(state string) AgentRowState {
	switch state {
	case worktree.AgentStateBlocked:
		return AgentRowBlocked
	case worktree.AgentStateError:
		return AgentRowError
	case worktree.AgentStateIdle:
		return AgentRowDone
	case worktree.AgentStateBusy:
		return AgentRowWorking
	default:
		return AgentRowIdle
	}
}

// AgentRowWord returns state's display word, exactly ADR-0055's table.
func AgentRowWord(state AgentRowState) string {
	switch state {
	case AgentRowBlocked:
		return "blocked"
	case AgentRowError:
		return "error"
	case AgentRowDone:
		return "done"
	case AgentRowWorking:
		return "working"
	default:
		return "idle"
	}
}

// AgentStateWord maps an agent pane's (isAgent, state) directly to its
// display word, for a caller that has not already filtered to agent-only
// panes. A non-agent pane has no agent row to show a word on, so this
// returns "" for isAgent == false regardless of state — rather than leaving
// that decision to every caller — instead of AgentRowStateFor/AgentRowWord's
// state-only mapping.
func AgentStateWord(isAgent bool, state string) string {
	if !isAgent {
		return ""
	}
	return AgentRowWord(AgentRowStateFor(state))
}

// AgentRowWordText returns state's display word, styled in the SAME color
// AgentRowDot uses for its glyph, so a row's glyph and word can never
// visually disagree.
func (p *Palette) AgentRowWordText(state AgentRowState) string {
	word := AgentRowWord(state)
	switch state {
	case AgentRowBlocked:
		return p.Blocked.Render(word)
	case AgentRowError:
		return p.Error.Render(word)
	case AgentRowDone:
		return p.NeedsReview.Render(word)
	case AgentRowWorking:
		return p.Running.Render(word)
	default:
		return p.NoSession.Render(word)
	}
}

// AgentRowGlyph returns the raw glyph character with no ANSI styling for an
// agent row's state. Use when the caller wraps the result in a parent style
// (e.g. the selection stripe); use AgentRowDot for a standalone styled
// glyph. Reuses StatusGlyph's shapes (!/✕/◆/●/○) so the two sections read as
// the same visual language, distinguished by their words, not by a second
// glyph set to learn.
func (p *Palette) AgentRowGlyph(state AgentRowState) string {
	switch state {
	case AgentRowBlocked:
		return "!"
	case AgentRowError:
		return "✕"
	case AgentRowDone:
		return "◆"
	case AgentRowWorking:
		return "●"
	default:
		return "○"
	}
}

// AgentRowDot returns a styled glyph string with ANSI color codes for an
// agent row's state, reusing the same palette colors StatusDot maps its
// equivalent space-row states to. Do NOT nest inside a parent
// style.Render() — use AgentRowGlyph instead.
func (p *Palette) AgentRowDot(state AgentRowState) string {
	g := p.AgentRowGlyph(state)
	switch state {
	case AgentRowBlocked:
		return p.Blocked.Render(g)
	case AgentRowError:
		return p.Error.Render(g)
	case AgentRowDone:
		return p.NeedsReview.Render(g)
	case AgentRowWorking:
		return p.Running.Render(g)
	default:
		return p.NoSession.Render(g)
	}
}
