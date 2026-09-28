package tuicomponents_test

import (
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
	tuicomponents "github.com/cjairm/devgeta/internal/tui/components"
)

func TestSessionStateFromAgent(t *testing.T) {
	cases := []struct {
		name         string
		windowActive bool
		agentState   string
		dirtyCount   int
		want         tuicomponents.SessionState
	}{
		{
			name:         "active window agent busy",
			windowActive: true,
			agentState:   worktree.AgentStateBusy,
			want:         tuicomponents.StateRunning,
		},
		{
			name:         "active window agent idle (needs review)",
			windowActive: true,
			agentState:   worktree.AgentStateIdle,
			want:         tuicomponents.StateNeedsReview,
		},
		{
			name:         "active window agent blocked",
			windowActive: true,
			agentState:   worktree.AgentStateBlocked,
			want:         tuicomponents.StateBlocked,
		},
		{
			name:         "active window agent error",
			windowActive: true,
			agentState:   worktree.AgentStateError,
			want:         tuicomponents.StateError,
		},
		{
			name:         "active window no agent state",
			windowActive: true,
			agentState:   "",
			want:         tuicomponents.StateRunning,
		},
		{
			name:         "inactive window dirty",
			windowActive: false,
			agentState:   worktree.AgentStateBusy,
			dirtyCount:   3,
			want:         tuicomponents.StateDirty,
		},
		{
			name:         "inactive window no dirty",
			windowActive: false,
			agentState:   worktree.AgentStateIdle,
			want:         tuicomponents.StateNoSession,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tuicomponents.SessionStateFromAgent(
				tc.windowActive,
				tc.agentState,
				tc.dirtyCount,
			)
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSessionStateFromWorktree(t *testing.T) {
	cases := []struct {
		name       string
		status     worktree.WorktreeStatus
		agentState string
		dirtyCount int
		want       tuicomponents.SessionState
	}{
		{
			name:       "active window agent busy",
			status:     worktree.WorktreeStatus{WindowActive: true},
			agentState: worktree.AgentStateBusy,
			want:       tuicomponents.StateRunning,
		},
		{
			name:       "active window agent idle (needs review)",
			status:     worktree.WorktreeStatus{WindowActive: true},
			agentState: worktree.AgentStateIdle,
			want:       tuicomponents.StateNeedsReview,
		},
		{
			name:       "active window agent blocked",
			status:     worktree.WorktreeStatus{WindowActive: true},
			agentState: worktree.AgentStateBlocked,
			want:       tuicomponents.StateBlocked,
		},
		{
			name:       "active window agent error",
			status:     worktree.WorktreeStatus{WindowActive: true},
			agentState: worktree.AgentStateError,
			want:       tuicomponents.StateError,
		},
		{
			name:       "active window no agent state",
			status:     worktree.WorktreeStatus{WindowActive: true},
			agentState: "",
			want:       tuicomponents.StateRunning,
		},
		{
			name:       "inactive window dirty",
			status:     worktree.WorktreeStatus{WindowActive: false},
			agentState: worktree.AgentStateBusy,
			dirtyCount: 3,
			want:       tuicomponents.StateDirty,
		},
		{
			name:       "inactive window no dirty",
			status:     worktree.WorktreeStatus{WindowActive: false},
			agentState: worktree.AgentStateIdle,
			want:       tuicomponents.StateNoSession,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tuicomponents.SessionStateFromWorktree(tc.status, tc.agentState, tc.dirtyCount)
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSessionGlyph(t *testing.T) {
	p := tuicomponents.NewPalette()
	// Sessions use squares, deliberately a different shape from the ●/○ circles
	// StatusGlyph returns for worktrees, and never overlapping them.
	if got := p.SessionGlyph(true); got != "■" {
		t.Errorf("attached: got %q want ■", got)
	}
	if got := p.SessionGlyph(false); got != "□" {
		t.Errorf("detached: got %q want □", got)
	}
	if strings.ContainsRune(p.SessionGlyph(true), '\x1b') {
		t.Error("SessionGlyph must not contain ANSI escape bytes")
	}
}

func TestSessionDotContainsGlyph(t *testing.T) {
	p := tuicomponents.NewPalette()
	if got := p.SessionDot(true); !strings.Contains(got, "■") {
		t.Errorf("attached: SessionDot %q does not contain ■", got)
	}
	if got := p.SessionDot(false); !strings.Contains(got, "□") {
		t.Errorf("detached: SessionDot %q does not contain □", got)
	}
}

func TestStatusGlyphNoANSI(t *testing.T) {
	p := tuicomponents.NewPalette()
	cases := []struct {
		state tuicomponents.SessionState
		glyph string
	}{
		{tuicomponents.StateRunning, "●"},
		{tuicomponents.StateNeedsReview, "◆"},
		{tuicomponents.StateDirty, "●"},
		{tuicomponents.StateNoSession, "○"},
		{tuicomponents.StateBlocked, "!"},
		{tuicomponents.StateError, "✕"},
	}
	for _, tc := range cases {
		got := p.StatusGlyph(tc.state)
		if got != tc.glyph {
			t.Errorf("state %d: got %q want %q", tc.state, got, tc.glyph)
		}
		if strings.ContainsRune(got, '\x1b') {
			t.Errorf("state %d: StatusGlyph must not contain ANSI escape bytes", tc.state)
		}
	}
}

func TestStatusDotContainsGlyph(t *testing.T) {
	p := tuicomponents.NewPalette()
	cases := []struct {
		state tuicomponents.SessionState
		glyph string
	}{
		{tuicomponents.StateRunning, "●"},
		{tuicomponents.StateNeedsReview, "◆"},
		{tuicomponents.StateDirty, "●"},
		{tuicomponents.StateNoSession, "○"},
		{tuicomponents.StateBlocked, "!"},
		{tuicomponents.StateError, "✕"},
	}
	for _, tc := range cases {
		got := p.StatusDot(tc.state)
		if !strings.Contains(got, tc.glyph) {
			t.Errorf("state %d: StatusDot %q does not contain glyph %q", tc.state, got, tc.glyph)
		}
	}
}

// TestGlyphUniqueness verifies that non-running/dirty states each have distinct glyphs.
// Running and Dirty intentionally share "●" — color is the differentiator.
// The other four states must each be unique.
func TestGlyphUniqueness(t *testing.T) {
	p := tuicomponents.NewPalette()
	glyphs := map[string]tuicomponents.SessionState{
		p.StatusGlyph(tuicomponents.StateNeedsReview): tuicomponents.StateNeedsReview,
		p.StatusGlyph(tuicomponents.StateNoSession):   tuicomponents.StateNoSession,
		p.StatusGlyph(tuicomponents.StateBlocked):     tuicomponents.StateBlocked,
		p.StatusGlyph(tuicomponents.StateError):       tuicomponents.StateError,
	}
	if len(glyphs) != 4 {
		t.Errorf("glyph collision detected: got %d unique glyphs, want 4", len(glyphs))
		for glyph, state := range glyphs {
			t.Logf("  %q -> state %d", glyph, state)
		}
	}
}

// TestAgentRowStateFor covers ADR-0055's state table for the dashboard's
// agents section: blocked/error/idle/busy/(unset) -> AgentRowBlocked/
// AgentRowError/AgentRowDone/AgentRowWorking/AgentRowIdle. Any unrecognized
// value (not in ADR-0005's vocabulary) falls back to AgentRowIdle, the same
// "unknown reads as unset" tolerance tmux.AggregateAgentState already uses.
func TestAgentRowStateFor(t *testing.T) {
	cases := []struct {
		name  string
		state string
		want  tuicomponents.AgentRowState
	}{
		{"blocked", worktree.AgentStateBlocked, tuicomponents.AgentRowBlocked},
		{"error", worktree.AgentStateError, tuicomponents.AgentRowError},
		{"idle is shown as done", worktree.AgentStateIdle, tuicomponents.AgentRowDone},
		{"busy is shown as working", worktree.AgentStateBusy, tuicomponents.AgentRowWorking},
		{"unset is shown as idle", "", tuicomponents.AgentRowIdle},
		{"unrecognized value falls back to idle", "some-future-value", tuicomponents.AgentRowIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tuicomponents.AgentRowStateFor(tc.state); got != tc.want {
				t.Errorf("AgentRowStateFor(%q) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

// TestAgentRowWord covers the exact display words ADR-0055 specifies — "done"
// (idle, unseen) is deliberately distinct from "idle" (unset, seen or never
// prompted), even though both derive from an otherwise-quiet pane.
func TestAgentRowWord(t *testing.T) {
	cases := []struct {
		state tuicomponents.AgentRowState
		want  string
	}{
		{tuicomponents.AgentRowBlocked, "blocked"},
		{tuicomponents.AgentRowError, "error"},
		{tuicomponents.AgentRowDone, "done"},
		{tuicomponents.AgentRowWorking, "working"},
		{tuicomponents.AgentRowIdle, "idle"},
	}
	for _, tc := range cases {
		if got := tuicomponents.AgentRowWord(tc.state); got != tc.want {
			t.Errorf("AgentRowWord(%v) = %q, want %q", tc.state, got, tc.want)
		}
	}
}

// TestAgentStateWord covers the (isAgent, state) -> word entry point a
// caller that hasn't already filtered to agent-only panes can use directly:
// a non-agent pane has no agent row to show a word on, so this returns ""
// regardless of what garbage might be sitting in its @dg_agent_state.
func TestAgentStateWord(t *testing.T) {
	cases := []struct {
		name    string
		isAgent bool
		state   string
		want    string
	}{
		{"agent, blocked", true, worktree.AgentStateBlocked, "blocked"},
		{"agent, idle shown as done", true, worktree.AgentStateIdle, "done"},
		{"agent, unset shown as idle", true, "", "idle"},
		{"not an agent, state ignored entirely", false, worktree.AgentStateBlocked, ""},
		{"not an agent, unset", false, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tuicomponents.AgentStateWord(tc.isAgent, tc.state); got != tc.want {
				t.Errorf("AgentStateWord(%v, %q) = %q, want %q", tc.isAgent, tc.state, got, tc.want)
			}
		})
	}
}

// TestAgentRowWordText confirms the styled word text contains its plain word
// and reuses the SAME color AgentRowDot's glyph uses for that state, so a
// row's glyph and word can never visually disagree.
func TestAgentRowWordText(t *testing.T) {
	p := tuicomponents.NewPalette()
	cases := []struct {
		state tuicomponents.AgentRowState
		word  string
	}{
		{tuicomponents.AgentRowBlocked, "blocked"},
		{tuicomponents.AgentRowError, "error"},
		{tuicomponents.AgentRowDone, "done"},
		{tuicomponents.AgentRowWorking, "working"},
		{tuicomponents.AgentRowIdle, "idle"},
	}
	for _, tc := range cases {
		got := p.AgentRowWordText(tc.state)
		if !strings.Contains(got, tc.word) {
			t.Errorf("AgentRowWordText(%v) = %q, does not contain word %q", tc.state, got, tc.word)
		}
		glyphColor, _, _ := strings.Cut(p.AgentRowDot(tc.state), p.AgentRowGlyph(tc.state))
		wordColor, _, _ := strings.Cut(got, tc.word)
		if glyphColor != wordColor {
			t.Errorf("AgentRowWordText(%v) color %q != AgentRowDot's glyph color %q",
				tc.state, wordColor, glyphColor)
		}
	}
}

// TestAgentRowGlyphNoANSI mirrors TestStatusGlyphNoANSI for the agents
// section's own glyph set.
func TestAgentRowGlyphNoANSI(t *testing.T) {
	p := tuicomponents.NewPalette()
	cases := []struct {
		state tuicomponents.AgentRowState
		want  string
	}{
		{tuicomponents.AgentRowBlocked, "!"},
		{tuicomponents.AgentRowError, "✕"},
		{tuicomponents.AgentRowDone, "◆"},
		{tuicomponents.AgentRowWorking, "●"},
		{tuicomponents.AgentRowIdle, "○"},
	}
	for _, tc := range cases {
		got := p.AgentRowGlyph(tc.state)
		if got != tc.want {
			t.Errorf("AgentRowGlyph(%v) = %q, want %q", tc.state, got, tc.want)
		}
		if strings.Contains(got, "\x1b") {
			t.Errorf("AgentRowGlyph(%v) contains ANSI codes, want raw glyph only", tc.state)
		}
	}
}

// TestAgentRowDotContainsGlyph mirrors TestStatusDotContainsGlyph: the styled
// form must still contain its raw glyph somewhere in the ANSI-wrapped output.
func TestAgentRowDotContainsGlyph(t *testing.T) {
	p := tuicomponents.NewPalette()
	cases := []struct {
		state tuicomponents.AgentRowState
		glyph string
	}{
		{tuicomponents.AgentRowBlocked, "!"},
		{tuicomponents.AgentRowError, "✕"},
		{tuicomponents.AgentRowDone, "◆"},
		{tuicomponents.AgentRowWorking, "●"},
		{tuicomponents.AgentRowIdle, "○"},
	}
	for _, tc := range cases {
		got := p.AgentRowDot(tc.state)
		if !strings.Contains(got, tc.glyph) {
			t.Errorf("AgentRowDot(%v) = %q, does not contain glyph %q", tc.state, got, tc.glyph)
		}
	}
}

// TestAgentRowGlyphUniqueness mirrors TestGlyphUniqueness: every agent row
// state (unlike the space-row set, which deliberately shares "●" between
// Running and Dirty) must have its own distinct glyph, since the agents
// section has no second dimension (like Dirty's "no active session") to
// disambiguate a shared shape by color alone.
func TestAgentRowGlyphUniqueness(t *testing.T) {
	p := tuicomponents.NewPalette()
	states := []tuicomponents.AgentRowState{
		tuicomponents.AgentRowBlocked,
		tuicomponents.AgentRowError,
		tuicomponents.AgentRowDone,
		tuicomponents.AgentRowWorking,
		tuicomponents.AgentRowIdle,
	}
	glyphs := map[string]tuicomponents.AgentRowState{}
	for _, s := range states {
		glyphs[p.AgentRowGlyph(s)] = s
	}
	if len(glyphs) != len(states) {
		t.Errorf(
			"glyph collision detected: got %d unique glyphs, want %d",
			len(glyphs),
			len(states),
		)
	}
}
