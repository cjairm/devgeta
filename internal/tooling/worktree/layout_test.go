package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
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

// TestBuiltinReviewerChoicesOrderAndLabels guards the exported accessor the
// TUI's R picker builds its item list from: it must come back in
// reviewerKeys order ("code" first, the common case) with each choice's
// label matching the registry, so the picker's dropdown can never drift from
// builtinReviewers() (the thing TestBuiltinReviewersKeysAreComplete guards).
func TestBuiltinReviewerChoicesOrderAndLabels(t *testing.T) {
	want := []ReviewerChoice{
		{Key: "code", Label: "code — bugs, security"},
		{Key: "document", Label: "document — plans, specs"},
		{Key: "skill", Label: "skill — agents/commands"},
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
// not a hardcoded "opencode", followed by --agent <name> and the fixed,
// single-quoted review prompt.
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

			want := wantOpenCodeToken + " --agent " + tt.wantAgent +
				" --prompt 'Review this branch against the default branch.'"
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
