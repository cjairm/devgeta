package worktree

import (
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
)

// --- built-in layout shapes ---

func TestBuiltinLayoutShapes(t *testing.T) {
	tests := []struct {
		name       string
		wantPanes  []Pane
		wantChecks int
	}{
		{
			name: "opencode",
			wantPanes: []Pane{
				{Command: "oc", Split: ""},
			},
			wantChecks: 1,
		},
		{
			name: "claude",
			wantPanes: []Pane{
				{Command: "cc", Split: ""},
			},
			wantChecks: 1,
		},
		{
			name: "claude-nvim",
			wantPanes: []Pane{
				{Command: "cc", Split: ""},
				{Command: "nvim", Split: "vertical"},
			},
			wantChecks: 2,
		},
		{
			name: "nvim",
			wantPanes: []Pane{
				{Command: "nvim", Split: ""},
			},
			wantChecks: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layout, err := ResolveLayout(tt.name, "", nil)
			if err != nil {
				t.Fatalf("unexpected error resolving %q: %v", tt.name, err)
			}
			if layout.Name != tt.name {
				t.Errorf("expected layout name %q, got %q", tt.name, layout.Name)
			}
			if len(layout.Panes) != len(tt.wantPanes) {
				t.Fatalf("expected %d panes, got %d", len(tt.wantPanes), len(layout.Panes))
			}
			for i, wantPane := range tt.wantPanes {
				if layout.Panes[i] != wantPane {
					t.Errorf("pane %d: expected %+v, got %+v", i, wantPane, layout.Panes[i])
				}
			}
			if len(layout.paneCheckers) != tt.wantChecks {
				t.Errorf(
					"expected %d pane checkers, got %d",
					tt.wantChecks,
					len(layout.paneCheckers),
				)
			}
		})
	}
}

func TestResolveLayoutUnknownName(t *testing.T) {
	_, err := ResolveLayout("cursor-split", "", nil)
	if err == nil {
		t.Fatal("expected error for unknown layout name, got nil")
	}
}

// --- precedence ladder ---

// Rung 1: explicit layoutName beats everything, including an explicit
// aiAlias and every config field.
func TestResolveLayoutNameBeatsEverything(t *testing.T) {
	gc := &config.GlobalConfig{}
	gc.Worktree.DefaultLayout = "claude-nvim"
	gc.Worktree.DefaultAI = "claude"

	layout, err := ResolveLayout("nvim", "opencode", gc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layout.Name != "nvim" {
		t.Errorf("expected explicit layout name 'nvim' to win, got %q", layout.Name)
	}
}

// Rung 2: an ai-alias-from-flag-or-env beats both default_layout and
// default_ai. This is the "dg wt repair --ai opencode with
// default_layout: claude-nvim honors --ai" example from the cycle doc.
func TestResolveLayoutAliasBeatsDefaultLayoutAndDefaultAI(t *testing.T) {
	gc := &config.GlobalConfig{}
	gc.Worktree.DefaultLayout = "claude-nvim"
	gc.Worktree.DefaultAI = "claude"

	layout, err := ResolveLayout("", "opencode", gc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layout.Name != "opencode" {
		t.Errorf(
			"expected alias 'opencode' to beat default_layout and default_ai, got %q",
			layout.Name,
		)
	}
	if len(layout.Panes) != 1 || layout.Panes[0].Command != "oc" {
		t.Errorf("expected single-pane opencode layout, got %+v", layout.Panes)
	}
}

// Rung 3: default_layout beats default_ai when no explicit layout name or
// alias is given (the bare `dg wt repair` / TUI auto-repair case).
func TestResolveLayoutDefaultLayoutBeatsDefaultAI(t *testing.T) {
	gc := &config.GlobalConfig{}
	gc.Worktree.DefaultLayout = "claude-nvim"
	gc.Worktree.DefaultAI = "opencode"

	layout, err := ResolveLayout("", "", gc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layout.Name != "claude-nvim" {
		t.Errorf(
			"expected default_layout 'claude-nvim' to win over default_ai, got %q",
			layout.Name,
		)
	}
}

// Rung 4: with no layout name, no alias, and no default_layout, default_ai
// derives a single-pane layout. This is the "config with only default_ai:
// claude" case from the task description.
func TestResolveLayoutDefaultAIOnlyDerivesSinglePaneLayout(t *testing.T) {
	gc := &config.GlobalConfig{}
	gc.Worktree.DefaultAI = "claude"

	layout, err := ResolveLayout("", "", gc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layout.Name != "claude" {
		t.Errorf("expected derived layout name 'claude', got %q", layout.Name)
	}
	if len(layout.Panes) != 1 || layout.Panes[0].Command != "cc" {
		t.Errorf("expected single-pane claude layout, got %+v", layout.Panes)
	}
	if layout.Panes[0].Split != "" {
		t.Errorf("expected first pane to have empty Split, got %q", layout.Panes[0].Split)
	}
}

// Rung 5: with nothing set at all (nil config, empty everything else),
// resolution falls all the way through to the opencode built-in fallback.
func TestResolveLayoutEmptyEverythingFallsBackToOpencode(t *testing.T) {
	layout, err := ResolveLayout("", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layout.Name != "opencode" {
		t.Errorf("expected fallback layout 'opencode', got %q", layout.Name)
	}
	if len(layout.Panes) != 1 || layout.Panes[0].Command != "oc" {
		t.Errorf("expected single-pane opencode layout, got %+v", layout.Panes)
	}
}

// Same as above but with a non-nil, fully empty config, to make sure an
// empty (not nil) *config.GlobalConfig behaves identically to nil.
func TestResolveLayoutEmptyConfigFallsBackToOpencode(t *testing.T) {
	gc := &config.GlobalConfig{}

	layout, err := ResolveLayout("", "", gc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if layout.Name != "opencode" {
		t.Errorf("expected fallback layout 'opencode', got %q", layout.Name)
	}
}

// An invalid default_layout value in config should error clearly, the same
// way an invalid explicit --layout name does.
func TestResolveLayoutInvalidDefaultLayoutErrors(t *testing.T) {
	gc := &config.GlobalConfig{}
	gc.Worktree.DefaultLayout = "not-a-real-layout"

	_, err := ResolveLayout("", "", gc)
	if err == nil {
		t.Fatal("expected error for invalid default_layout config value, got nil")
	}
}

// An invalid alias (from flag/env) should error the same way
// ResolveAICoder does for any other caller.
func TestResolveLayoutInvalidAliasErrors(t *testing.T) {
	_, err := ResolveLayout("", "not-a-real-ai", nil)
	if err == nil {
		t.Fatal("expected error for invalid ai alias, got nil")
	}
}

// --- install-check surface ---

// setShellCommandExistsFn (in repo_candidates_test.go, same package) swaps
// commands.ShellCommandExistsFn — the interactive-shell probe every coder/nvim
// install check now routes through (see aicoder.go's ensureToolInstalled).
// allToolsPresent/noToolsPresent are the two common stubs. Checks resolve the
// underlying binary name (opencode/claude/nvim), not the cc/oc launch alias.

func TestNvimEnsureInstalledSurfacesActionableError(t *testing.T) {
	setShellCommandExistsFn(t, func(string) bool { return false })

	err := ensureNvimInstalled()
	if err == nil {
		t.Fatal("expected error when nvim does not resolve in the shell, got nil")
	}
	if got := err.Error(); got == "" {
		t.Fatal("expected a non-empty, actionable error message")
	}
}

func TestNvimEnsureInstalledOK(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name == "nvim" })

	if err := ensureNvimInstalled(); err != nil {
		t.Fatalf("unexpected error when nvim resolves in the shell: %v", err)
	}
}

// Layout.EnsureInstalled aggregates all pane checks and names which pane
// failed. All built-ins (opencode, claude, nvim) route their install checks
// through the shared ensureToolInstalled helper in aicoder.go, which goes
// through the swappable commands.ShellCommandExistsFn - so every built-in
// layout's failure path is exercisable here, not just nvim's.
func TestLayoutEnsureInstalledReportsFailingPane(t *testing.T) {
	setShellCommandExistsFn(t, func(string) bool { return false })

	layout, err := ResolveLayout("nvim", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving layout: %v", err)
	}

	err = layout.EnsureInstalled()
	if err == nil {
		t.Fatal("expected EnsureInstalled to fail when nvim is missing, got nil")
	}
}

func TestLayoutEnsureInstalledOK(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name == "nvim" })

	layout, err := ResolveLayout("nvim", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving layout: %v", err)
	}

	if err := layout.EnsureInstalled(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The "claude-nvim" built-in has two panes with two different underlying
// tools (claude, nvim). Simulate only the second pane's tool being missing
// and confirm EnsureInstalled names pane 2, not pane 1, in its error.
func TestLayoutEnsureInstalledReportsCorrectPaneIndexInMultiPaneLayout(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name != "nvim" })

	layout, err := ResolveLayout("claude-nvim", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving layout: %v", err)
	}

	err = layout.EnsureInstalled()
	if err == nil {
		t.Fatal("expected EnsureInstalled to fail when nvim is missing, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "pane 2") {
		t.Errorf("expected error to name pane 2 (nvim), got %q", got)
	}
}

// opencode's and claude's install checks go through the same swappable
// commands.ShellCommandExistsFn as nvim's (via aicoder.go's shared
// ensureToolInstalled helper), so their failure paths are exercisable here too
// - this used to be a documented gap when each coder called exec.LookPath.
func TestLayoutEnsureInstalledFailsForOpencodeBuiltin(t *testing.T) {
	setShellCommandExistsFn(t, func(string) bool { return false })

	layout, err := ResolveLayout("opencode", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving layout: %v", err)
	}

	err = layout.EnsureInstalled()
	if err == nil {
		t.Fatal("expected EnsureInstalled to fail when opencode is missing, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "opencode") {
		t.Errorf("expected error to mention opencode, got %q", got)
	}
}

func TestLayoutEnsureInstalledFailsForClaudeBuiltin(t *testing.T) {
	setShellCommandExistsFn(t, func(string) bool { return false })

	layout, err := ResolveLayout("claude", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving layout: %v", err)
	}

	err = layout.EnsureInstalled()
	if err == nil {
		t.Fatal("expected EnsureInstalled to fail when claude is missing, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "claude") {
		t.Errorf("expected error to mention claude, got %q", got)
	}
}

// --- newLayout invariants ---

// newLayout requires panes and checkers to line up 1:1. A mismatch can only
// come from a bug in this file's own built-in registry construction, so it
// panics rather than silently producing a Layout whose EnsureInstalled
// would misreport which pane failed.
func TestNewLayoutPanicsOnPaneCheckerMismatch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected newLayout to panic on panes/checkers length mismatch, got no panic")
		}
	}()

	newLayout("broken", []Pane{{Command: "a"}, {Command: "b"}}, []func() error{
		func() error { return nil },
	})
}
