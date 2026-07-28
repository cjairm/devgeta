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

// SessionStateFromWorktree derives state from WorktreeStatus, incorporating
// the aggregated agent state from the worktree's window panes.
// agentState should be one of the AgentState* constants from the worktree package,
// or "" if no agent has reported a state (equivalent to StateRunning for active windows).
func SessionStateFromWorktree(
	s worktree.WorktreeStatus,
	agentState string,
	dirtyCount int,
) SessionState {
	if s.WindowActive {
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
