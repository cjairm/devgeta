package worktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
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
		{
			// The plain layout: one pane, no command to type, and nothing to
			// check for — a shell is what tmux starts a pane with anyway.
			name: "shell",
			wantPanes: []Pane{
				{Command: "", Split: ""},
			},
			wantChecks: 0,
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
			// Compared field-wise, not with ==: Pane carries its check and
			// prompt funcs, and funcs are not comparable in Go. Only the
			// exported tmux-facing fields are what this test is pinning
			// anyway; the funcs are asserted by behavior below.
			for i, wantPane := range tt.wantPanes {
				got := layout.Panes[i]
				if got.Command != wantPane.Command || got.Split != wantPane.Split {
					t.Errorf(
						"pane %d: expected command %q split %q, got command %q split %q",
						i, wantPane.Command, wantPane.Split, got.Command, got.Split,
					)
				}
			}
			if got := countPaneChecks(layout); got != tt.wantChecks {
				t.Errorf("expected %d pane install checks, got %d", tt.wantChecks, got)
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

	resolvedPath, err := ensureNvimInstalled()
	if err == nil {
		t.Fatal("expected error when nvim does not resolve in the shell, got nil")
	}
	if got := err.Error(); got == "" {
		t.Fatal("expected a non-empty, actionable error message")
	}
	if resolvedPath != "" {
		t.Errorf("expected no resolved path alongside the error, got %q", resolvedPath)
	}
}

func TestNvimEnsureInstalledOK(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name == "nvim" })

	if _, err := ensureNvimInstalled(); err != nil {
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

	_, err = layout.EnsureInstalled()
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

	if _, err := layout.EnsureInstalled(); err != nil {
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

	_, err = layout.EnsureInstalled()
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

	_, err = layout.EnsureInstalled()
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

	_, err = layout.EnsureInstalled()
	if err == nil {
		t.Fatal("expected EnsureInstalled to fail when claude is missing, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "claude") {
		t.Errorf("expected error to mention claude, got %q", got)
	}
}

// --- pane transformations: WithPrompt / WithExtraPanes ---

// countPaneChecks reports how many of layout's panes carry an install check.
// It replaces the old len(layout.paneCheckers) assertion: the checkers used to
// live in a slice parallel to Panes, and now live on the Pane they describe,
// so "how many checks does this layout have" is a property of the panes.
//
// There is deliberately no test that a checkers slice lines up with Panes
// anymore - that mismatch is what the deleted newLayout panicked on, and it is
// now unrepresentable rather than merely guarded.
func countPaneChecks(layout Layout) int {
	n := 0
	for _, pane := range layout.Panes {
		if pane.check != nil {
			n++
		}
	}
	return n
}

// commandsOf returns each pane's command, for asserting the shape of a
// transformed layout.
func commandsOf(layout Layout) []string {
	commands := make([]string, 0, len(layout.Panes))
	for _, pane := range layout.Panes {
		commands = append(commands, pane.Command)
	}
	return commands
}

// TestWithPromptRetargetsCoderPane covers every built-in layout that has an AI
// pane, including the multi-pane claude-nvim (where the prompt must land on
// pane 1 and leave the nvim pane alone - `nvim 'do the thing'` would open a
// file by that name).
func TestWithPromptRetargetsCoderPane(t *testing.T) {
	tests := []struct {
		layout       string
		wantCommands []string
	}{
		{"opencode", []string{"oc '--prompt' 'fix the bug'"}},
		{"claude", []string{"cc 'fix the bug'"}},
		{"claude-nvim", []string{"cc 'fix the bug'", "nvim"}},
	}

	for _, tt := range tests {
		t.Run(tt.layout, func(t *testing.T) {
			layout, err := ResolveLayout(tt.layout, "", nil)
			if err != nil {
				t.Fatalf("unexpected error resolving %q: %v", tt.layout, err)
			}

			got, err := layout.WithPrompt("fix the bug")
			if err != nil {
				t.Fatalf("WithPrompt returned error: %v", err)
			}

			gotCommands := commandsOf(got)
			if len(gotCommands) != len(tt.wantCommands) {
				t.Fatalf("expected %d panes, got %v", len(tt.wantCommands), gotCommands)
			}
			for i, want := range tt.wantCommands {
				if gotCommands[i] != want {
					t.Errorf("pane %d: got command %q, want %q", i, gotCommands[i], want)
				}
			}
		})
	}
}

// A layout with no AI pane must fail loudly rather than drop the prompt or
// hand it to a non-coder command. The error names the layout and lists the
// layouts that do accept a prompt. Both AI-free built-ins are covered: nvim
// (where the prompt would become a filename) and shell (where it would be
// typed at a bash prompt).
func TestWithPromptErrorsOnLayoutWithoutCoderPane(t *testing.T) {
	for _, name := range []string{"nvim", "shell"} {
		t.Run(name, func(t *testing.T) {
			layout, err := ResolveLayout(name, "", nil)
			if err != nil {
				t.Fatalf("unexpected error resolving %s layout: %v", name, err)
			}

			_, err = layout.WithPrompt("fix the bug")
			if err == nil {
				t.Fatal("expected an error prompting a layout with no AI pane, got nil")
			}
			if got := err.Error(); !strings.Contains(got, name) {
				t.Errorf("expected the error to name the %s layout, got %q", name, got)
			}
			// The suggestion list is derived from the registry, so it must
			// actually name promptable layouts.
			if got := err.Error(); !strings.Contains(got, "opencode") ||
				!strings.Contains(got, "claude") {
				t.Errorf("expected the error to list layouts that accept a prompt, got %q", got)
			}
		})
	}
}

// promptableLayoutNames feeds the error message above, so it must list exactly
// the built-ins with an AI pane, in registry order - hardcoding that list is
// what this guards against.
func TestPromptableLayoutNames(t *testing.T) {
	want := []string{"opencode", "claude", "claude-nvim"}

	got := promptableLayoutNames()

	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("index %d: got %q, want %q", i, got[i], name)
		}
	}
}

// A layout with two prompt-taking panes is ambiguous. None ships today, so
// this builds one directly to prove the guard fires for the first one added.
func TestWithPromptErrorsOnAmbiguousMultiCoderLayout(t *testing.T) {
	layout := Layout{
		Name: "two-coders",
		Panes: []Pane{
			coderPane(&ClaudeCoder{}, ""),
			coderPane(&OpenCodeCoder{}, splitVertical),
		},
	}

	_, err := layout.WithPrompt("fix the bug")
	if err == nil {
		t.Fatal("expected an error for a layout with two AI panes, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "more than one") {
		t.Errorf("expected an ambiguity error, got %q", got)
	}
}

// An empty prompt is a no-op so the CLI can apply WithPrompt unconditionally,
// without first testing whether --prompt was passed. Crucially it must NOT
// error on the nvim layout, which is the bare `dg wt create --layout nvim`
// case.
func TestWithPromptEmptyIsNoOp(t *testing.T) {
	for _, name := range []string{"claude", "nvim"} {
		t.Run(name, func(t *testing.T) {
			layout, err := ResolveLayout(name, "", nil)
			if err != nil {
				t.Fatalf("unexpected error resolving %q: %v", name, err)
			}

			got, err := layout.WithPrompt("")
			if err != nil {
				t.Fatalf("expected no error for an empty prompt, got %v", err)
			}

			before, after := commandsOf(layout), commandsOf(got)
			for i := range before {
				if before[i] != after[i] {
					t.Errorf("pane %d: command changed from %q to %q", i, before[i], after[i])
				}
			}
		})
	}
}

// A prompt containing a single quote must survive into the launch command
// intact - shellSingleQuote's escape path, exercised through the real
// transformation rather than only on the helper.
func TestWithPromptQuotesEmbeddedSingleQuote(t *testing.T) {
	layout, err := ResolveLayout("claude", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving claude layout: %v", err)
	}

	got, err := layout.WithPrompt("it's broken")
	if err != nil {
		t.Fatalf("WithPrompt returned error: %v", err)
	}

	want := `cc 'it'\''s broken'`
	if got.Panes[0].Command != want {
		t.Errorf("got command %q, want %q", got.Panes[0].Command, want)
	}
}

// TestWithExtraPanesAppendsInOrder pins that extra panes land after the
// layout's own panes, in flag order, each splitting "vertical" (side by side).
func TestWithExtraPanesAppendsInOrder(t *testing.T) {
	layout, err := ResolveLayout("claude", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving claude layout: %v", err)
	}

	got, err := layout.WithExtraPanes([]string{"make finit", "npm run dev"})
	if err != nil {
		t.Fatalf("WithExtraPanes returned error: %v", err)
	}

	wantCommands := []string{"cc", "make finit", "npm run dev"}
	gotCommands := commandsOf(got)
	if len(gotCommands) != len(wantCommands) {
		t.Fatalf("expected %d panes, got %v", len(wantCommands), gotCommands)
	}
	for i, want := range wantCommands {
		if gotCommands[i] != want {
			t.Errorf("pane %d: got command %q, want %q", i, gotCommands[i], want)
		}
	}
	for i, pane := range got.Panes[1:] {
		if pane.Split != splitVertical {
			t.Errorf("extra pane %d: got split %q, want %q", i, pane.Split, splitVertical)
		}
	}
}

// A --pane command is a shell command line, so it must reach the pane exactly
// as written - quoting it would break the compound commands that justify the
// flag. See ADR-0011 on the asymmetry with a prompt.
func TestWithExtraPanesDoesNotQuoteCommand(t *testing.T) {
	layout, err := ResolveLayout("claude", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving claude layout: %v", err)
	}

	command := "cd api && make dev"
	got, err := layout.WithExtraPanes([]string{command})
	if err != nil {
		t.Fatalf("WithExtraPanes returned error: %v", err)
	}

	if got.Panes[1].Command != command {
		t.Errorf("got command %q, want it unquoted as %q", got.Panes[1].Command, command)
	}
}

// An extra pane carries no install check: its command can be a shell builtin,
// a compound, or a Makefile target, so probing its first token would reject
// legitimate commands. EnsureInstalled must therefore still pass for a layout
// whose extra pane names a command that does not exist as a binary.
func TestWithExtraPanesAddsNoInstallCheck(t *testing.T) {
	setShellCommandExistsFn(t, func(name string) bool { return name == "cc" })

	layout, err := ResolveLayout("claude", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving claude layout: %v", err)
	}

	got, err := layout.WithExtraPanes([]string{"definitely-not-a-real-binary --x"})
	if err != nil {
		t.Fatalf("WithExtraPanes returned error: %v", err)
	}

	if n := countPaneChecks(got); n != 1 {
		t.Errorf("expected only the coder pane to carry a check, got %d checks", n)
	}
	if _, err := got.EnsureInstalled(); err != nil {
		t.Errorf("expected EnsureInstalled to ignore the extra pane, got %v", err)
	}
}

// An empty or whitespace-only --pane is rejected: `--pane "$VAR"` with an unset
// variable is a likelier cause than a deliberate request for an idle shell, and
// a silent empty pane looks like the feature half-worked.
func TestWithExtraPanesRejectsEmptyCommand(t *testing.T) {
	layout, err := ResolveLayout("claude", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving claude layout: %v", err)
	}

	for _, command := range []string{"", " ", "\t", "  \n "} {
		t.Run(fmt.Sprintf("%q", command), func(t *testing.T) {
			if _, err := layout.WithExtraPanes([]string{command}); err == nil {
				t.Fatalf("expected an error for --pane %q, got nil", command)
			}
		})
	}
}

// A bad command anywhere in the list fails the whole call, so a caller never
// gets a layout with only some of its requested panes.
func TestWithExtraPanesRejectsEmptyCommandAmongValidOnes(t *testing.T) {
	layout, err := ResolveLayout("claude", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving claude layout: %v", err)
	}

	if _, err := layout.WithExtraPanes([]string{"make finit", ""}); err == nil {
		t.Fatal("expected an error when a later --pane is empty, got nil")
	}
}

// No extra panes is a no-op, mirroring WithPrompt's empty-prompt case so the
// CLI can apply both unconditionally.
func TestWithExtraPanesEmptySliceIsNoOp(t *testing.T) {
	layout, err := ResolveLayout("claude", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving claude layout: %v", err)
	}

	got, err := layout.WithExtraPanes(nil)
	if err != nil {
		t.Fatalf("expected no error for no extra panes, got %v", err)
	}
	if len(got.Panes) != 1 {
		t.Errorf("expected the layout unchanged, got %v", commandsOf(got))
	}
}

// Both transformations must leave the RECEIVER untouched. Callers hold and
// reuse resolved layouts (the TUI resolves once and creates repeatedly), so an
// in-place edit would leak one worktree's prompt or extra panes into the next.
func TestTransformationsDoNotMutateSourceLayout(t *testing.T) {
	t.Run("WithPrompt", func(t *testing.T) {
		layout, err := ResolveLayout("claude", "", nil)
		if err != nil {
			t.Fatalf("unexpected error resolving claude layout: %v", err)
		}

		if _, err := layout.WithPrompt("fix the bug"); err != nil {
			t.Fatalf("WithPrompt returned error: %v", err)
		}

		if layout.Panes[0].Command != "cc" {
			t.Errorf("source layout was mutated: pane 0 command is now %q", layout.Panes[0].Command)
		}
	})

	t.Run("WithExtraPanes", func(t *testing.T) {
		layout, err := ResolveLayout("claude", "", nil)
		if err != nil {
			t.Fatalf("unexpected error resolving claude layout: %v", err)
		}

		if _, err := layout.WithExtraPanes([]string{"make finit"}); err != nil {
			t.Fatalf("WithExtraPanes returned error: %v", err)
		}

		if len(layout.Panes) != 1 {
			t.Errorf("source layout was mutated: now has %d panes", len(layout.Panes))
		}
	})

	// The claude-nvim case is the one where a shared backing array would
	// actually bite: WithPrompt writes to index 0 of a two-element slice, so a
	// shallow copy that aliased the array would corrupt the original.
	t.Run("WithPrompt on a multi-pane layout", func(t *testing.T) {
		layout, err := ResolveLayout("claude-nvim", "", nil)
		if err != nil {
			t.Fatalf("unexpected error resolving claude-nvim layout: %v", err)
		}

		got, err := layout.WithPrompt("fix the bug")
		if err != nil {
			t.Fatalf("WithPrompt returned error: %v", err)
		}

		if layout.Panes[0].Command != "cc" {
			t.Errorf("source layout was mutated: pane 0 command is now %q", layout.Panes[0].Command)
		}
		if got.Panes[0].Command == layout.Panes[0].Command {
			t.Error("expected the copy's coder pane command to differ from the source's")
		}
	})
}

// Chaining both transformations is what the CLI does; the prompt must land on
// the coder pane and the extra pane must still append after it.
func TestWithPromptThenWithExtraPanesCompose(t *testing.T) {
	layout, err := ResolveLayout("claude", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving claude layout: %v", err)
	}

	prompted, err := layout.WithPrompt("fix the bug")
	if err != nil {
		t.Fatalf("WithPrompt returned error: %v", err)
	}
	got, err := prompted.WithExtraPanes([]string{"make finit"})
	if err != nil {
		t.Fatalf("WithExtraPanes returned error: %v", err)
	}

	want := []string{"cc 'fix the bug'", "make finit"}
	gotCommands := commandsOf(got)
	if len(gotCommands) != len(want) {
		t.Fatalf("expected %v, got %v", want, gotCommands)
	}
	for i := range want {
		if gotCommands[i] != want[i] {
			t.Errorf("pane %d: got %q, want %q", i, gotCommands[i], want[i])
		}
	}
}

// --- reviewer registry ---

// reviewerAgentsDir is configs/shared/agents/ read directly off disk
// (relative to this package's directory, internal/tooling/worktree) rather
// than through ConfigsFS (embedded in package main, which this package
// cannot import without an import cycle). This is safe: main.go's
// `//go:embed all:configs` embeds these files byte-for-byte with no
// transformation, so reading them from disk here checks exactly what ships
// in the binary — see TestBuiltinReviewersAgentNamesMatchEmbeddedAgentFiles
// (package main, root of the repo, beside task_redirect_test.go) for the
// companion test that runs the same check against the embedded bytes, so a
// file that fails to embed is still caught.
var reviewerAgentsDir = filepath.Join("..", "..", "..", "configs", "shared", "agents")

// reviewerAgentFilesOnDisk lists the agent names (filenames minus ".md")
// present in dir.
func reviewerAgentFilesOnDisk(t *testing.T, dir string) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read %s: %v", dir, err)
	}

	names := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		names[strings.TrimSuffix(entry.Name(), ".md")] = true
	}
	return names
}

// TestBuiltinReviewersAgentNamesMatchAgentFiles is builtinReviewers' rule-5
// embedded-config constraint test (CLAUDE.md's "if a config must satisfy a
// constraint imposed by an external tool... enforce that constraint with a
// test"): each Reviewer.Agent is handed straight to `opencode --agent
// <name>` by a later step, so it must equal a real file in
// configs/shared/agents/ — a renamed or deleted agent file must fail the
// build rather than ship a flag naming an agent that no longer exists.
func TestBuiltinReviewersAgentNamesMatchAgentFiles(t *testing.T) {
	onDisk := reviewerAgentFilesOnDisk(t, reviewerAgentsDir)

	for key, reviewer := range builtinReviewers() {
		if !onDisk[reviewer.Agent] {
			t.Errorf(
				"reviewer %q: agent %q has no matching file in %s",
				key, reviewer.Agent, reviewerAgentsDir,
			)
		}
	}
}

// TestBuiltinReviewersKeysAreComplete guards the registry's shape: exactly
// the three documented keys, each with a non-empty agent name and label.
func TestBuiltinReviewersKeysAreComplete(t *testing.T) {
	wantKeys := []string{"code", "document", "skill"}
	reviewers := builtinReviewers()

	if len(reviewers) != len(wantKeys) {
		t.Fatalf("expected %d reviewers, got %d: %+v", len(wantKeys), len(reviewers), reviewers)
	}
	for _, key := range wantKeys {
		reviewer, ok := reviewers[key]
		if !ok {
			t.Errorf("expected reviewer key %q to be registered", key)
			continue
		}
		if reviewer.Agent == "" {
			t.Errorf("reviewer %q: expected non-empty agent name", key)
		}
		if reviewer.Label == "" {
			t.Errorf("reviewer %q: expected non-empty label", key)
		}
	}
}

// TestBuiltinReviewerChoicesOrderAndLabels guards the exported accessor both
// the TUI's R picker and `dg task review-run --reviewer` read the registry
// through: it must come back in reviewerKeys order ("code" first, the common
// case) with each choice's label AND agent matching the registry, so neither
// the picker's dropdown nor a headless run's `--agent` can drift from
// builtinReviewers() (the thing TestBuiltinReviewersKeysAreComplete guards).
func TestBuiltinReviewerChoicesOrderAndLabels(t *testing.T) {
	want := []ReviewerChoice{
		{Key: "code", Label: "code — bugs, security", Agent: "code-reviewer"},
		{Key: "document", Label: "document — plans, specs", Agent: "document-reviewer"},
		{Key: "skill", Label: "skill — agents/commands", Agent: "skill-reviewer"},
	}

	got := BuiltinReviewerChoices()

	if len(got) != len(want) {
		t.Fatalf("expected %d choices, got %d: %+v", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("choice %d: got %+v, want %+v", i, got[i], w)
		}
	}
}

// --- review pane command ---

// TestReviewCommandBuildsExpectedCommand asserts the exact command string
// for every registered reviewer key: the OpenCodeCoder launch token ("oc"),
// not a hardcoded "opencode", followed by --agent <name> and the fixed review
// prompt. Every argument is single-quoted, flags included, since this renders
// opencode's structured launch (see paneLaunch.render for why the rule is
// uniform rather than "quote only the values").
func TestReviewCommandBuildsExpectedCommand(t *testing.T) {
	wantOpenCodeToken := (&OpenCodeCoder{}).Command()

	tests := []struct {
		key       string
		wantAgent string
	}{
		{"code", "code-reviewer"},
		{"document", "document-reviewer"},
		{"skill", "skill-reviewer"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got, err := ReviewCommand(tt.key)
			if err != nil {
				t.Fatalf("ReviewCommand(%q) returned error: %v", tt.key, err)
			}

			want := wantOpenCodeToken + " '--agent' " + shellSingleQuote(tt.wantAgent) +
				" '--prompt' 'Review this branch against the default branch.'"
			if got != want {
				t.Errorf("ReviewCommand(%q) = %q, want %q", tt.key, got, want)
			}
		})
	}
}

// TestReviewCommandUnknownKeyErrors mirrors lookupBuiltinLayout's "unknown
// name" contract: an invalid reviewer key must error, not silently build a
// command for a zero-value Reviewer (which would send `oc --agent
// --prompt '...'` - a broken command - to a live tmux pane).
func TestReviewCommandUnknownKeyErrors(t *testing.T) {
	_, err := ReviewCommand("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown reviewer key, got nil")
	}
}

// TestShellSingleQuoteHandCheckedCases hand-verifies shellSingleQuote's
// output for exact string equality, including the embedded-single-quote
// escape case (the standard POSIX close-escape-reopen trick: quote,
// backslash, quote, quote), before
// trusting the round-trip test below to cross-check the same logic a second
// way.
func TestShellSingleQuoteHandCheckedCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", "hello world", "'hello world'"},
		{"empty string", "", "''"},
		{"single embedded quote", "it's", `'it'\''s'`},
		{"leading and trailing quotes", "'quoted'", `''\''quoted'\'''`},
		{
			"shell metacharacters stay inert inside quotes",
			`$(rm -rf /); echo "hi" | cat & ` + "`whoami`",
			`'$(rm -rf /); echo "hi" | cat & ` + "`whoami`'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellSingleQuote(tt.input)
			if got != tt.want {
				t.Errorf("shellSingleQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestShellSingleQuoteRoundTripsThroughRealShell proves shellSingleQuote is
// correct in general, not just for today's metacharacter-free review
// prompt: it shells out to `sh -c` and confirms the quoted argument is
// parsed back to exactly the original string, for inputs containing a
// single quote and other shell metacharacters ($, ;, |, &, backticks,
// double quotes). This is pure shell-syntax validation of a built string -
// it exercises no devgeta tmux/git behavior, so it does not need
// testutil.MockApp (see CLAUDE.md's testing rule and this task's brief).
func TestShellSingleQuoteRoundTripsThroughRealShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available in PATH, skipping shell round-trip test")
	}

	inputs := []string{
		"Review this branch against the default branch.",
		"it's a test",
		`$(rm -rf /); echo "hi" | cat & ` + "`whoami`",
		"''leading and trailing''",
		"multiple 'quotes' in 'one' string",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			quoted := shellSingleQuote(input)

			out, err := exec.Command("sh", "-c", "printf '%s' "+quoted).Output()
			if err != nil {
				t.Fatalf("sh -c failed for quoted=%q: %v", quoted, err)
			}
			if string(out) != input {
				t.Errorf("round trip mismatch: got %q, want %q (quoted was %q)", out, input, quoted)
			}
		})
	}
}

// --- resolveShell ---

// usableShellFixture creates an executable regular file in a temp dir, standing
// in for an installed shell. Nothing here touches a real shell or the user's
// environment: resolveShell takes its candidates as arguments precisely so it
// can be tested this way.
func usableShellFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("failed to create shell fixture: %v", err)
	}
	return path
}

// TestResolveShellFallsBackToPosixShell covers every way a candidate can fail
// the "usable" test. An absolute-path shape check alone would accept the first
// three of these and interpolate them into a pane command at two sites, which is
// why the check stats the file (ADR-0020).
func TestResolveShellFallsBackToPosixShell(t *testing.T) {
	dir := t.TempDir()

	nonExecutable := filepath.Join(dir, "notexec")
	if err := os.WriteFile(nonExecutable, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatalf("failed to create non-executable fixture: %v", err)
	}

	tests := []struct {
		name      string
		candidate string
	}{
		{"no candidate at all", ""},
		{"an absolute path that does not exist", filepath.Join(dir, "missing", "zsh")},
		{"a directory", dir},
		{"an existing file with no execute bit", nonExecutable},
		{"a relative path", "bin/zsh"},
		{"a bare command name", "zsh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveShell(tt.candidate); got != posixShell {
				t.Errorf("resolveShell(%q) = %q, want %q", tt.candidate, got, posixShell)
			}
		})
	}
}

// TestResolveShellWithNoCandidatesUsesThePosixFloor: a caller whose $SHELL is
// unset AND whose tmux query failed passes nothing, which must still resolve -
// no create is ever blocked on shell resolution.
func TestResolveShellWithNoCandidatesUsesThePosixFloor(t *testing.T) {
	if got := resolveShell(); got != posixShell {
		t.Errorf("resolveShell() = %q, want %q", got, posixShell)
	}
}

// TestResolveShellPrefersTheFirstUsableCandidate pins the ladder's order: the
// caller passes $SHELL first and tmux's default-shell second, so a usable $SHELL
// must win, and an unusable one must be skipped rather than returned.
func TestResolveShellPrefersTheFirstUsableCandidate(t *testing.T) {
	first := usableShellFixture(t, "zsh")
	second := usableShellFixture(t, "bash")

	if got := resolveShell(first, second); got != first {
		t.Errorf("resolveShell(usable, usable) = %q, want the first one %q", got, first)
	}

	broken := filepath.Join(t.TempDir(), "uninstalled-zsh")
	if got := resolveShell(broken, second); got != second {
		t.Errorf("resolveShell(broken, usable) = %q, want %q", got, second)
	}
}

// --- carrying the probe's resolution from the check to the launch ---

// creationCommandsOf returns what each pane of layout would be created with, for
// asserting the shape of a RESOLVED layout. It is the counterpart of commandsOf
// (which reads the typed form, Pane.Command); the two are deliberately different
// strings for the same pane - see ADR-0020 and launch.go's header.
func creationCommandsOf(t *testing.T, layout Layout, shell string) []string {
	t.Helper()
	out := make([]string, 0, len(layout.Panes))
	for _, pane := range layout.Panes {
		out = append(out, pane.creationCommand(shell))
	}
	return out
}

// countedProbe swaps the shell-lookup seam for one that COUNTS its invocations
// and answers with resolve(name). The count is what proves ADR-0020's "one probe
// per pane per create": a design that re-probes when the pane's command is built
// passes every string assertion in this file and only fails here.
func countedProbe(
	t *testing.T,
	resolve func(name string) (string, commands.ShellLookupResult),
) *int {
	t.Helper()
	calls := 0
	setShellCommandLookupPathFn(t, func(name string) (string, commands.ShellLookupResult) {
		calls++
		return resolve(name)
	})
	return &calls
}

// foundAt answers every lookup with paths[name], Found when the name is known and
// NotFound otherwise - the common stub for the tests below.
func foundAt(paths map[string]string) func(string) (string, commands.ShellLookupResult) {
	return func(name string) (string, commands.ShellLookupResult) {
		path, ok := paths[name]
		if !ok {
			return "", commands.ShellLookupNotFound
		}
		return path, commands.ShellLookupFound
	}
}

// TestEnsureInstalledCarriesTheProbesPathIntoThePaneCommand is the core of this
// step: whatever the ONE probe resolved has to be what the created pane runs.
// Both branches are covered, because the fallback is the one ADR-0020 warns is
// easy to get wrong (a bare name in tmux's non-interactive shell is NOT today's
// behavior - there is no alias and no repaired PATH there).
func TestEnsureInstalledCarriesTheProbesPathIntoThePaneCommand(t *testing.T) {
	const prompt = "fix issue 1082"
	const claudePath = "/Users/dev/.local/bin/claude"

	tests := []struct {
		name    string
		resolve func(name string) (string, commands.ShellLookupResult)
		want    string
	}{
		{
			name:    "a resolved path is exec'd, with the env prefix spelled out",
			resolve: foundAt(map[string]string{"cc": claudePath}),
			want: `CLAUDE_CODE_NO_FLICKER=1 '/Users/dev/.local/bin/claude' ` +
				`'fix issue 1082'; exec '/bin/zsh'`,
		},
		{
			// `command -v cc` prints "alias cc='...'", which is not path-shaped,
			// so the probe found the tool but resolved no path. The pane must get
			// the alias through an INTERACTIVE shell, which is the only shell
			// that has it.
			name: "a Found outcome with no path takes the interactive fallback",
			resolve: func(string) (string, commands.ShellLookupResult) {
				return "", commands.ShellLookupFound
			},
			want: `'/bin/zsh' -ic 'cc '\''fix issue 1082'\''; exec '\''/bin/zsh'\'' -i'`,
		},
		{
			// ADR-0016: an inconclusive probe proved nothing, so it must neither
			// block the create nor be treated as a path. Identical fallback.
			name: "an inconclusive probe takes the same fallback and does not block",
			resolve: func(string) (string, commands.ShellLookupResult) {
				return "", commands.ShellLookupInconclusive
			},
			want: `'/bin/zsh' -ic 'cc '\''fix issue 1082'\''; exec '\''/bin/zsh'\'' -i'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := countedProbe(t, tt.resolve)

			layout, err := ResolveLayout("claude", "", nil)
			if err != nil {
				t.Fatalf("unexpected error resolving claude layout: %v", err)
			}
			prompted, err := layout.WithPrompt(prompt)
			if err != nil {
				t.Fatalf("WithPrompt returned error: %v", err)
			}
			resolved, err := prompted.EnsureInstalled()
			if err != nil {
				t.Fatalf("EnsureInstalled must not block this create, got %v", err)
			}

			got := creationCommandsOf(t, resolved, testShell)
			if len(got) != 1 {
				t.Fatalf("expected one pane, got %v", got)
			}
			if got[0] != tt.want {
				t.Errorf("pane command = %q, want %q", got[0], tt.want)
			}
			// The typed form is unchanged by any of this - it is what the repair
			// path still sends into a pane that already exists (ADR-0020 part 4).
			if resolved.Panes[0].Command != "cc 'fix issue 1082'" {
				t.Errorf(
					"the typed command must be unaffected, got %q",
					resolved.Panes[0].Command,
				)
			}
			if *calls != 1 {
				t.Errorf("expected exactly 1 probe for a one-pane layout, got %d", *calls)
			}
		})
	}
}

// TestOneProbePerPanePerCreate pins the invariant ADR-0020 fixes as the
// requirement rather than the signatures: one probe per pane per create, and
// building the pane's command spends that answer rather than asking again.
// Building the commands repeatedly must add no probes at all.
func TestOneProbePerPanePerCreate(t *testing.T) {
	calls := countedProbe(t, foundAt(map[string]string{
		"cc":   "/Users/dev/.local/bin/claude",
		"nvim": "/opt/homebrew/bin/nvim",
	}))

	layout, err := ResolveLayout("claude-nvim", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving claude-nvim layout: %v", err)
	}

	resolved, err := layout.EnsureInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("expected 2 probes for a 2-pane layout, got %d", *calls)
	}

	for i := 0; i < 3; i++ {
		creationCommandsOf(t, resolved, testShell)
	}
	if *calls != 2 {
		t.Errorf(
			"building pane commands must not re-probe; probe count went from 2 to %d",
			*calls,
		)
	}
}

// TestNvimPaneExecsTheResolvedBinaryWithoutThePrompt covers the pane whose probe
// resolves a real path TODAY (nvim's launch token is the binary, not a cc/oc
// alias), and pins that the prompt on the coder pane never reaches it -
// `nvim 'fix issue 1082'` opens a file by that name.
func TestNvimPaneExecsTheResolvedBinaryWithoutThePrompt(t *testing.T) {
	countedProbe(t, foundAt(map[string]string{
		"cc":   "/Users/dev/.local/bin/claude",
		"nvim": "/opt/homebrew/bin/nvim",
	}))

	layout, err := ResolveLayout("claude-nvim", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving claude-nvim layout: %v", err)
	}
	prompted, err := layout.WithPrompt("fix issue 1082")
	if err != nil {
		t.Fatalf("WithPrompt returned error: %v", err)
	}
	resolved, err := prompted.EnsureInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := creationCommandsOf(t, resolved, testShell)
	want := []string{
		`CLAUDE_CODE_NO_FLICKER=1 '/Users/dev/.local/bin/claude' 'fix issue 1082'; ` +
			`exec '/bin/zsh'`,
		`'/opt/homebrew/bin/nvim'; exec '/bin/zsh'`,
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d panes, got %v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pane %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestCreationCommandForPanesWithNoLaunch covers the two pane kinds that carry no
// launch closure, so nothing about them is resolved:
//
//   - a --pane value goes through the interactive recipe UNPARSED, so `&&` and
//     `cd` keep working (ADR-0011);
//   - a shell pane gets no command at all, so tmux just starts its shell.
func TestCreationCommandForPanesWithNoLaunch(t *testing.T) {
	t.Run("a --pane value is wrapped unparsed", func(t *testing.T) {
		setShellCommandExistsFn(t, func(name string) bool { return name == "cc" })

		layout, err := ResolveLayout("claude", "", nil)
		if err != nil {
			t.Fatalf("unexpected error resolving claude layout: %v", err)
		}
		withExtra, err := layout.WithExtraPanes([]string{"cd api && make dev"})
		if err != nil {
			t.Fatalf("WithExtraPanes returned error: %v", err)
		}
		resolved, err := withExtra.EnsureInstalled()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got := resolved.Panes[1].creationCommand(testShell)
		want := `'/bin/zsh' -ic 'cd api && make dev; exec '\''/bin/zsh'\'' -i'`
		if got != want {
			t.Errorf("--pane creation command = %q, want %q", got, want)
		}
	})

	t.Run("a shell pane gets no command", func(t *testing.T) {
		layout, err := ResolveLayout("shell", "", nil)
		if err != nil {
			t.Fatalf("unexpected error resolving shell layout: %v", err)
		}
		resolved, err := layout.EnsureInstalled()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if got := resolved.Panes[0].creationCommand(testShell); got != "" {
			t.Errorf("a shell pane must get no command at all, got %q", got)
		}
	})
}

// TestResolvedLayoutDoesNotLeakBetweenCreates is the reason clone exists, applied
// to the two pieces of state this step adds. `dg ws` resolves a layout ONCE and
// creates from it repeatedly, so if either the prompt or the probe's resolution
// were written in place, the second worktree would inherit the first one's - a
// window that looks correctly created and is running the wrong task.
func TestResolvedLayoutDoesNotLeakBetweenCreates(t *testing.T) {
	// The shared, resolved layout a long-lived caller holds.
	source, err := ResolveLayout("claude", "", nil)
	if err != nil {
		t.Fatalf("unexpected error resolving claude layout: %v", err)
	}

	// Create 1: a path resolves, so this create takes the exec form.
	countedProbe(t, foundAt(map[string]string{"cc": "/Users/dev/.local/bin/claude"}))
	first, err := source.WithPrompt("first task")
	if err != nil {
		t.Fatalf("WithPrompt returned error: %v", err)
	}
	firstResolved, err := first.EnsureInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create 2, from the SAME source: a different prompt, and a probe that
	// resolves nothing, so this create takes the fallback. Both differences have
	// to survive independently.
	countedProbe(t, func(string) (string, commands.ShellLookupResult) {
		return "", commands.ShellLookupFound
	})
	second, err := source.WithPrompt("second task")
	if err != nil {
		t.Fatalf("WithPrompt returned error: %v", err)
	}
	secondResolved, err := second.EnsureInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	firstCmd := firstResolved.Panes[0].creationCommand(testShell)
	secondCmd := secondResolved.Panes[0].creationCommand(testShell)

	wantFirst := `CLAUDE_CODE_NO_FLICKER=1 '/Users/dev/.local/bin/claude' ` +
		`'first task'; exec '/bin/zsh'`
	if firstCmd != wantFirst {
		t.Errorf("first create's command = %q, want %q", firstCmd, wantFirst)
	}
	wantSecond := `'/bin/zsh' -ic 'cc '\''second task'\''; exec '\''/bin/zsh'\'' -i'`
	if secondCmd != wantSecond {
		t.Errorf("second create's command = %q, want %q", secondCmd, wantSecond)
	}
	if strings.Contains(secondCmd, "first task") {
		t.Errorf("the first create's prompt leaked into the second: %q", secondCmd)
	}

	// Create 3: no prompt at all - the bare `dg wt create` path. WithPrompt("")
	// returns the layout UNCHANGED and uncloned, so this is the only create where
	// EnsureInstalled receives the caller's own layout rather than a copy
	// WithPrompt already made. If EnsureInstalled wrote its resolution in place,
	// this is where it would land on the shared layout, and the source assertions
	// below are what catch it.
	countedProbe(t, foundAt(map[string]string{"cc": "/Users/dev/.local/bin/claude"}))
	thirdResolved, err := source.EnsureInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantThird := `CLAUDE_CODE_NO_FLICKER=1 '/Users/dev/.local/bin/claude'; exec '/bin/zsh'`
	if got := thirdResolved.Panes[0].creationCommand(testShell); got != wantThird {
		t.Errorf("third create's command = %q, want %q", got, wantThird)
	}

	// And the source is still pristine: no prompt, no resolution, so a fourth
	// create starts from the same clean state the first one did.
	if source.Panes[0].Command != "cc" {
		t.Errorf("the shared layout was mutated: command is now %q", source.Panes[0].Command)
	}
	if source.Panes[0].promptText != "" {
		t.Errorf("the shared layout kept a prompt: %q", source.Panes[0].promptText)
	}
	if source.Panes[0].resolvedPath != "" {
		t.Errorf(
			"the shared layout kept a probe resolution: %q",
			source.Panes[0].resolvedPath,
		)
	}
	if got := source.Panes[0].creationCommand(
		testShell,
	); got != `'/bin/zsh' -ic 'cc; exec '\''/bin/zsh'\'' -i'` {
		t.Errorf("the shared layout's own pane command changed: %q", got)
	}
}

// --- the reviewer pane's resolution-carrying constructor ---

// TestReviewerPaneProbesOnceAndCarriesTheResolution: the review path does its own
// preflight instead of going through validateLayout, and it used to throw the
// answer away. One constructor, one probe, and the resolution has to come back on
// the pane.
func TestReviewerPaneProbesOnceAndCarriesTheResolution(t *testing.T) {
	const opencodePath = "/opt/homebrew/bin/opencode"
	calls := countedProbe(t, foundAt(map[string]string{"oc": opencodePath}))

	pane, err := reviewerPane("code")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("expected exactly 1 probe for a reviewer launch, got %d", *calls)
	}

	// The typed form, for the idle-shell-reuse branch's send-keys, is exactly
	// what ReviewCommand builds - the two must not drift.
	wantTyped, err := ReviewCommand("code")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if pane.Command != wantTyped {
		t.Errorf("pane.Command = %q, want ReviewCommand's %q", pane.Command, wantTyped)
	}

	// The created form execs the resolved binary, with the reviewer agent still
	// pinned before the prompt.
	got := pane.creationCommand(testShell)
	want := `'/opt/homebrew/bin/opencode' '--agent' 'code-reviewer' ` +
		`'--prompt' 'Review this branch against the default branch.'; exec '/bin/zsh'`
	if got != want {
		t.Errorf("reviewer pane creation command = %q, want %q", got, want)
	}

	// Building the command must not add a probe.
	if *calls != 1 {
		t.Errorf("building the pane command re-probed; probe count is now %d", *calls)
	}

	// check is nil on purpose: the probe already ran, so putting this pane in a
	// Layout must not buy a second one - and EnsureInstalled must preserve the
	// resolution rather than clear it.
	if pane.check != nil {
		t.Error("a reviewer pane must carry no check - reviewerPane already probed")
	}
	layout := Layout{Name: "review-code", Panes: []Pane{pane}}
	resolved, err := layout.EnsureInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *calls != 1 {
		t.Errorf("EnsureInstalled re-probed the reviewer pane; probe count is now %d", *calls)
	}
	if got := resolved.Panes[0].creationCommand(testShell); got != want {
		t.Errorf("the resolution did not survive EnsureInstalled: got %q, want %q", got, want)
	}
}

// TestReviewerPaneFallsBackWhenNoPathResolves: the review launch gets the same
// interactive fallback every other devgeta-owned pane gets, so an inconclusive
// probe costs it nothing (ADR-0016) and the oc alias still resolves.
func TestReviewerPaneFallsBackWhenNoPathResolves(t *testing.T) {
	for _, tt := range []struct {
		name   string
		result commands.ShellLookupResult
	}{
		{"found, but the answer was not a path", commands.ShellLookupFound},
		{"inconclusive", commands.ShellLookupInconclusive},
	} {
		t.Run(tt.name, func(t *testing.T) {
			countedProbe(t, func(string) (string, commands.ShellLookupResult) {
				return "", tt.result
			})

			pane, err := reviewerPane("code")
			if err != nil {
				t.Fatalf("a probe with no path must not block a review launch, got %v", err)
			}

			got := pane.creationCommand(testShell)
			want := `'/bin/zsh' -ic 'oc '\''--agent'\'' '\''code-reviewer'\'' ` +
				`'\''--prompt'\'' '\''Review this branch against the default branch.'\''; ` +
				`exec '\''/bin/zsh'\'' -i'`
			if got != want {
				t.Errorf("reviewer fallback command = %q, want %q", got, want)
			}
		})
	}
}

// TestReviewerPaneRejectsAnUnknownKeyBeforeProbing: an unknown reviewer key is a
// programming/CLI error, and it must cost nothing - no shell probe, and therefore
// no 5-second worst case - before it is reported.
func TestReviewerPaneRejectsAnUnknownKeyBeforeProbing(t *testing.T) {
	calls := countedProbe(t, foundAt(map[string]string{"oc": "/opt/homebrew/bin/opencode"}))

	if _, err := reviewerPane("not-a-real-reviewer"); err == nil {
		t.Fatal("expected an error for an unknown reviewer key, got nil")
	}
	if *calls != 0 {
		t.Errorf("an unknown reviewer key must not probe at all, got %d probes", *calls)
	}
}

// TestReviewerPaneFailsWhenOpenCodeIsMissing: the preflight this constructor
// replaced still refuses the launch when the probe PROVED oc absent - the one
// outcome that may block (ADR-0016).
func TestReviewerPaneFailsWhenOpenCodeIsMissing(t *testing.T) {
	setShellCommandExistsFn(t, func(string) bool { return false })

	_, err := reviewerPane("code")
	if err == nil {
		t.Fatal("expected an error when opencode does not resolve, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "opencode") {
		t.Errorf("expected the error to name opencode, got %q", got)
	}
}
