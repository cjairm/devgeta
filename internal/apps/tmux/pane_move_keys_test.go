package tmux_test

// Tests for Step 9 of docs/plans/cycles/2026-09-28-ws-agents-section.md
// (ADR-0057): RootPaneMoveKeys parses `tmux list-keys -T root` for whichever
// key runs select-pane -L/-D/-U/-R, directly or as the fallback branch of an
// if-shell (the shipped is_vim-style bindings), falling back to
// ctrl+h/j/k/l per direction when nothing is found.

import (
	"errors"
	"testing"

	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/testutil"
)

// shippedIsVimBindings mirrors the exact shape configs/tmux/tmux.conf.tmpl
// ships and real tmux 3.7c echoes back for it (captured against a real
// server during this cycle's Step 0), one line per direction.
const shippedIsVimBindings = `bind-key  -T root C-h                       if-shell "ps -o state= -o comm= -t '#{pane_tty}' | grep -iqE '^[^TXZ ]+ +(\S+\/)?g?(view|n?vim?x?)(diff)?$'" "send-keys C-h" "select-pane -L"
bind-key  -T root C-j                       if-shell "ps -o state= -o comm= -t '#{pane_tty}' | grep -iqE '^[^TXZ ]+ +(\S+\/)?g?(view|n?vim?x?)(diff)?$'" "send-keys C-j" "select-pane -D"
bind-key  -T root C-k                       if-shell "ps -o state= -o comm= -t '#{pane_tty}' | grep -iqE '^[^TXZ ]+ +(\S+\/)?g?(view|n?vim?x?)(diff)?$'" "send-keys C-k" "select-pane -U"
bind-key  -T root C-l                       if-shell "ps -o state= -o comm= -t '#{pane_tty}' | grep -iqE '^[^TXZ ]+ +(\S+\/)?g?(view|n?vim?x?)(diff)?$'" "send-keys C-l" "select-pane -R"
`

func TestRootPaneMoveKeys_ParsesIfShellFallbackBindings(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult(shippedIsVimBindings, "", nil)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	keys := app.RootPaneMoveKeys()

	want := tmux.PaneMoveKeys{Left: "ctrl+h", Down: "ctrl+j", Up: "ctrl+k", Right: "ctrl+l"}
	if keys != want {
		t.Errorf("RootPaneMoveKeys() = %+v, want %+v", keys, want)
	}
}

func TestRootPaneMoveKeys_ParsesPlainDirectBindings(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult(
		"bind-key  -T root M-h                       select-pane -L\n"+
			"bind-key  -T root M-j                       select-pane -D\n"+
			"bind-key  -T root M-k                       select-pane -U\n"+
			"bind-key  -T root M-l                       select-pane -R\n",
		"", nil,
	)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	keys := app.RootPaneMoveKeys()

	want := tmux.PaneMoveKeys{Left: "alt+h", Down: "alt+j", Up: "alt+k", Right: "alt+l"}
	if keys != want {
		t.Errorf("RootPaneMoveKeys() = %+v, want %+v", keys, want)
	}
}

func TestRootPaneMoveKeys_FallsBackToDefaultsOnExecError(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("", "error", errors.New("no server"))
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	keys := app.RootPaneMoveKeys()

	want := tmux.PaneMoveKeys{Left: "ctrl+h", Down: "ctrl+j", Up: "ctrl+k", Right: "ctrl+l"}
	if keys != want {
		t.Errorf("RootPaneMoveKeys() = %+v, want the ctrl+h/j/k/l default %+v", keys, want)
	}
}

func TestRootPaneMoveKeys_FallsBackToDefaultsOnEmptyOutput(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult("", "", nil)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	keys := app.RootPaneMoveKeys()

	want := tmux.PaneMoveKeys{Left: "ctrl+h", Down: "ctrl+j", Up: "ctrl+k", Right: "ctrl+l"}
	if keys != want {
		t.Errorf("RootPaneMoveKeys() = %+v, want the ctrl+h/j/k/l default %+v", keys, want)
	}
}

func TestRootPaneMoveKeys_PartialMatchFallsBackPerDirection(t *testing.T) {
	mockApp := testutil.NewMockApp()
	// Only left and down are bound; up and right must fall back to defaults.
	mockApp.Base.SetExecCommandResult(
		"bind-key  -T root M-h                       select-pane -L\n"+
			"bind-key  -T root M-j                       select-pane -D\n",
		"", nil,
	)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	keys := app.RootPaneMoveKeys()

	want := tmux.PaneMoveKeys{Left: "alt+h", Down: "alt+j", Up: "ctrl+k", Right: "ctrl+l"}
	if keys != want {
		t.Errorf("RootPaneMoveKeys() = %+v, want %+v", keys, want)
	}
}

func TestRootPaneMoveKeys_IgnoresUnrelatedBindings(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResult(
		"bind-key  -T root C-t                       new-window -n [workspace] \"~/.local/bin/devgeta ws\"\n"+
			"bind-key  -T root MouseDown1Pane            select-pane -t = \\; send-keys -M\n"+
			shippedIsVimBindings,
		"",
		nil,
	)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	keys := app.RootPaneMoveKeys()

	want := tmux.PaneMoveKeys{Left: "ctrl+h", Down: "ctrl+j", Up: "ctrl+k", Right: "ctrl+l"}
	if keys != want {
		t.Errorf(
			"RootPaneMoveKeys() = %+v, want %+v (unrelated bindings must not confuse the parser)",
			keys,
			want,
		)
	}
}

func TestRootPaneMoveKeys_FirstMatchWinsWhenMultipleKeysBindTheSameDirection(t *testing.T) {
	mockApp := testutil.NewMockApp()
	// A user or the shipped config could plausibly bind two keys to the
	// same direction; the first one list-keys reports wins.
	mockApp.Base.SetExecCommandResult(
		"bind-key  -T root C-h                       select-pane -L\n"+
			"bind-key  -T root M-h                       select-pane -L\n",
		"", nil,
	)
	app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

	keys := app.RootPaneMoveKeys()

	if keys.Left != "ctrl+h" {
		t.Errorf("Left = %q, want the first-listed binding ctrl+h", keys.Left)
	}
}
