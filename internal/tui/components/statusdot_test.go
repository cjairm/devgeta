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
