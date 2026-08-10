/*
* Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func init() { testutil.InitLogger() }

// resetWorktreeFlags saves and restores every flag var and the package-level
// globalConfig this file's tests touch, so one test's cobra flag parsing (or
// its config Load()) never bleeds into another's — the same state-bleed
// concern the cycle doc raised about sharing aiFlag across commands.
func resetWorktreeFlags(t *testing.T) {
	t.Helper()
	origCreateAI := createAIFlag
	origCreateLayout := createLayoutFlag
	origCreatePrompt := createPromptFlag
	origCreatePanes := createPaneFlags
	origRepairAI := repairAIFlag
	origRepairLayout := repairLayoutFlag
	origGlobalConfig := globalConfig
	t.Cleanup(func() {
		createAIFlag = origCreateAI
		createLayoutFlag = origCreateLayout
		createPromptFlag = origCreatePrompt
		createPaneFlags = origCreatePanes
		repairAIFlag = origRepairAI
		repairLayoutFlag = origRepairLayout
		globalConfig = origGlobalConfig
		// Cobra retains parsed flag values/"changed" state on the shared
		// command objects between Execute calls; reset both commands' flag
		// sets so a later test starts from a clean, unparsed state.
		_ = worktreeCreateCmd.Flags().Set("ai", "")
		_ = worktreeCreateCmd.Flags().Set("layout", "")
		_ = worktreeCreateCmd.Flags().Set("prompt", "")
		resetRepeatableFlag(t, worktreeCreateCmd, "pane")
		_ = worktreeRepairCmd.Flags().Set("ai", "")
		_ = worktreeRepairCmd.Flags().Set("layout", "")
	})
}

// resetRepeatableFlag clears a repeatable (StringArray) flag on cmd.
//
// Flags().Set(name, "") is WRONG for a repeatable flag and would introduce the
// very leak this reset exists to prevent: pflag's stringArrayValue.Set appends
// once the flag's `changed` is true (pflag/string_array.go), so setting it to ""
// leaves a stray empty string in the slice - which --pane rejects, so the next
// test would fail on a value it never passed.
//
// Clearing `changed` matters independently: pflag keys its append-vs-replace
// behavior off it, so a stale true makes the NEXT parse's first --pane append to
// the previous test's values instead of starting fresh.
func resetRepeatableFlag(t *testing.T, cmd *cobra.Command, name string) {
	t.Helper()
	flag := cmd.Flags().Lookup(name)
	if flag == nil {
		t.Fatalf("flag %q is not registered on %q", name, cmd.Name())
	}
	sv, ok := flag.Value.(pflag.SliceValue)
	if !ok {
		t.Fatalf("flag %q is not a slice value, cannot reset it as repeatable", name)
	}
	if err := sv.Replace(nil); err != nil {
		t.Fatalf("failed to clear flag %q: %v", name, err)
	}
	flag.Changed = false
}

// TestWorktreeCreateCmd_AIAndLayoutMutuallyExclusive verifies cobra rejects
// --ai + --layout together on `dg wt create`. This calls ParseFlags +
// ValidateFlagGroups directly rather than Command.Execute(): Execute() on a
// non-root command redirects to cobra's Root().ExecuteC() (running the full
// rootCmd tree), which is unnecessary here and would need every other
// subcommand's dependencies in play. ParseFlags+ValidateFlagGroups exercises
// exactly the MarkFlagsMutuallyExclusive registration this test targets,
// without ever reaching RunE / worktree.New() / real tmux or git calls.
func TestWorktreeCreateCmd_AIAndLayoutMutuallyExclusive(t *testing.T) {
	resetWorktreeFlags(t)

	if err := worktreeCreateCmd.ParseFlags(
		[]string{"--ai", "claude", "--layout", "nvim"},
	); err != nil {
		t.Fatalf("unexpected flag parse error: %v", err)
	}

	err := worktreeCreateCmd.ValidateFlagGroups()

	if err == nil {
		t.Fatal("expected an error for --ai + --layout together, got nil")
	}
	if !strings.Contains(err.Error(), "none of the others can be") {
		t.Errorf("expected a mutually-exclusive-flags error, got: %v", err)
	}
}

// TestWorktreeRepairCmd_AIAndLayoutMutuallyExclusive mirrors the create test
// for `dg wt repair`, whose own flag set carries its own
// MarkFlagsMutuallyExclusive registration.
func TestWorktreeRepairCmd_AIAndLayoutMutuallyExclusive(t *testing.T) {
	resetWorktreeFlags(t)

	if err := worktreeRepairCmd.ParseFlags(
		[]string{"--ai", "claude", "--layout", "nvim"},
	); err != nil {
		t.Fatalf("unexpected flag parse error: %v", err)
	}

	err := worktreeRepairCmd.ValidateFlagGroups()

	if err == nil {
		t.Fatal("expected an error for --ai + --layout together, got nil")
	}
	if !strings.Contains(err.Error(), "none of the others can be") {
		t.Errorf("expected a mutually-exclusive-flags error, got: %v", err)
	}
}

// TestLoadWorktreeGlobalConfig_MissingFileIsNonFatal covers the
// globalConfig.Load() gap fix: on a fresh install (no global_config.yaml
// yet), loadWorktreeGlobalConfig must not panic or leave globalConfig in a
// broken state - ResolveLayout still needs to see zero-valued
// DefaultAI/DefaultLayout so it falls through to the opencode default.
func TestLoadWorktreeGlobalConfig_MissingFileIsNonFatal(t *testing.T) {
	resetWorktreeFlags(t)
	origRoot := paths.Paths.Config.Root
	paths.Paths.Config.Root = t.TempDir()
	t.Cleanup(func() { paths.Paths.Config.Root = origRoot })

	globalConfig = config.GlobalConfig{}
	loadWorktreeGlobalConfig()

	if globalConfig.Worktree.DefaultAI != "" {
		t.Errorf(
			"expected DefaultAI to stay empty when no config file exists, got %q",
			globalConfig.Worktree.DefaultAI,
		)
	}
	if globalConfig.Worktree.DefaultLayout != "" {
		t.Errorf(
			"expected DefaultLayout to stay empty when no config file exists, got %q",
			globalConfig.Worktree.DefaultLayout,
		)
	}
}

// TestLoadWorktreeGlobalConfig_LoadsExistingConfig is the other half of the
// gap fix: when global_config.yaml does exist and sets worktree.default_ai /
// worktree.default_layout, loadWorktreeGlobalConfig must actually populate
// them - this is precisely the CLI-path behavior that was silently broken
// before this fix (dg ws loaded gc correctly elsewhere; dg wt
// create/repair never did).
func TestLoadWorktreeGlobalConfig_LoadsExistingConfig(t *testing.T) {
	resetWorktreeFlags(t)
	origRoot := paths.Paths.Config.Root
	paths.Paths.Config.Root = t.TempDir()
	t.Cleanup(func() { paths.Paths.Config.Root = origRoot })

	seed := config.GlobalConfig{}
	seed.Worktree.DefaultAI = "claude"
	seed.Worktree.DefaultLayout = "claude-nvim"
	if err := seed.Save(); err != nil {
		t.Fatalf("failed to seed global config: %v", err)
	}

	globalConfig = config.GlobalConfig{}
	loadWorktreeGlobalConfig()

	if globalConfig.Worktree.DefaultAI != "claude" {
		t.Errorf("expected DefaultAI %q, got %q", "claude", globalConfig.Worktree.DefaultAI)
	}
	if globalConfig.Worktree.DefaultLayout != "claude-nvim" {
		t.Errorf(
			"expected DefaultLayout %q, got %q",
			"claude-nvim",
			globalConfig.Worktree.DefaultLayout,
		)
	}
}

// TestLoadWorktreeGlobalConfig_CorruptFileIsNonFatal covers the other error
// branch of the Load() fix: an existing-but-corrupt global_config.yaml (a
// real problem, distinct from "file missing") must still not be fatal for
// create/repair - it only differs from the missing-file case in that it's
// surfaced via logger.Warnw instead of silently ignored. This test can't
// easily assert on the log line itself, but it pins the non-fatal/no-panic
// behavior; the os.IsNotExist branching itself is what routes a corrupt file
// to the Warnw call instead of the silent-ignore path.
func TestLoadWorktreeGlobalConfig_CorruptFileIsNonFatal(t *testing.T) {
	resetWorktreeFlags(t)
	origRoot := paths.Paths.Config.Root
	paths.Paths.Config.Root = t.TempDir()
	t.Cleanup(func() { paths.Paths.Config.Root = origRoot })

	configPath := filepath.Join(
		paths.Paths.Config.Root,
		constants.App.Name,
		constants.App.File.GlobalConfig,
	)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("not: [valid: yaml"), 0o644); err != nil {
		t.Fatalf("failed to write corrupt config: %v", err)
	}

	globalConfig = config.GlobalConfig{}
	loadWorktreeGlobalConfig() // must not panic on a corrupt (not just missing) file

	if globalConfig.Worktree.DefaultAI != "" || globalConfig.Worktree.DefaultLayout != "" {
		t.Errorf(
			"expected globalConfig to stay unpopulated after a corrupt-file load failure, got %+v",
			globalConfig.Worktree,
		)
	}
}

// TestWorktreeCreateCmd_LayoutFlagRegistered guards against the --layout
// flag silently disappearing from `dg wt create` in a future edit.
func TestWorktreeCreateCmd_LayoutFlagRegistered(t *testing.T) {
	flag := worktreeCreateCmd.Flags().Lookup("layout")
	if flag == nil {
		t.Fatal("expected --layout flag to be registered on dg wt create")
	}
	if flag.Shorthand != "l" {
		t.Errorf("expected --layout shorthand -l, got %q", flag.Shorthand)
	}
}

// TestWorktreeRepairCmd_LayoutFlagRegistered mirrors the create check for
// `dg wt repair`.
func TestWorktreeRepairCmd_LayoutFlagRegistered(t *testing.T) {
	flag := worktreeRepairCmd.Flags().Lookup("layout")
	if flag == nil {
		t.Fatal("expected --layout flag to be registered on dg wt repair")
	}
	if flag.Shorthand != "l" {
		t.Errorf("expected --layout shorthand -l, got %q", flag.Shorthand)
	}
}

// TestWorktreeMove covers `dg wt move`'s CLI surface: flag/alias
// registration and argument-count validation. This deliberately does NOT
// exercise RunE (that would reach worktree.New() and real git/tmux, which
// cmd package tests avoid throughout this file) - the full Move() behavior
// (resolution, no-op semantics, dirty check, gitignore warning, tmux
// retargeting including the busy-pane and no-window cases) is covered by
// TestWorktreeMove in internal/tooling/worktree/move_test.go, which can
// inject mocked Git/Tmux. This test's job is only to pin the command's
// shape so it doesn't silently drift (see CLAUDE.md section 10, "Altering
// command signatures... without a deprecation plan").
func TestWorktreeMove(t *testing.T) {
	t.Run("--to flag is registered with no shorthand", func(t *testing.T) {
		flag := worktreeMoveCmd.Flags().Lookup("to")
		if flag == nil {
			t.Fatal("expected --to flag to be registered on dg wt move")
		}
		if flag.Shorthand != "" {
			t.Errorf("expected --to to have no shorthand, got %q", flag.Shorthand)
		}
	})

	t.Run("--force flag is registered with -f shorthand", func(t *testing.T) {
		flag := worktreeMoveCmd.Flags().Lookup("force")
		if flag == nil {
			t.Fatal("expected --force flag to be registered on dg wt move")
		}
		if flag.Shorthand != "f" {
			t.Errorf("expected --force shorthand -f, got %q", flag.Shorthand)
		}
	})

	t.Run("mv alias is registered", func(t *testing.T) {
		found := false
		for _, alias := range worktreeMoveCmd.Aliases {
			if alias == "mv" {
				found = true
			}
		}
		if !found {
			t.Errorf(
				"expected alias %q on dg wt move, got aliases %v",
				"mv",
				worktreeMoveCmd.Aliases,
			)
		}
	})

	t.Run("requires exactly one argument", func(t *testing.T) {
		if err := worktreeMoveCmd.Args(worktreeMoveCmd, []string{}); err == nil {
			t.Error("expected an error with zero args")
		}
		if err := worktreeMoveCmd.Args(worktreeMoveCmd, []string{"a", "b"}); err == nil {
			t.Error("expected an error with two args")
		}
		if err := worktreeMoveCmd.Args(worktreeMoveCmd, []string{"a"}); err != nil {
			t.Errorf("expected no error with one arg, got: %v", err)
		}
	})

	t.Run("ValidArgsFunction rejects completion once a name is already given", func(t *testing.T) {
		_, directive := worktreeMoveCmd.ValidArgsFunction(
			worktreeMoveCmd,
			[]string{"already-given"},
			"",
		)
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("expected ShellCompDirectiveNoFileComp, got %v", directive)
		}
	})
}

// --- create's --prompt / --pane flags ---

// TestWorktreeCreateCmd_PromptAndPaneFlagShape pins the command's surface, per
// CLAUDE.md's change-discipline rule that command signatures don't drift
// silently. Neither flag takes a shorthand: -p would be ambiguous between them.
func TestWorktreeCreateCmd_PromptAndPaneFlagShape(t *testing.T) {
	resetWorktreeFlags(t)

	tests := []struct {
		name      string
		wantType  string
		wantUsage string
	}{
		{"prompt", "string", "prompt"},
		{"pane", "stringArray", "pane"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := worktreeCreateCmd.Flags().Lookup(tt.name)
			if flag == nil {
				t.Fatalf("expected --%s to be registered on create", tt.name)
			}
			if flag.Value.Type() != tt.wantType {
				t.Errorf("--%s: expected type %q, got %q", tt.name, tt.wantType, flag.Value.Type())
			}
			if flag.Shorthand != "" {
				t.Errorf(
					"--%s: expected no shorthand (-p is ambiguous between --prompt and --pane), got %q",
					tt.name,
					flag.Shorthand,
				)
			}
			if flag.Usage == "" {
				t.Errorf("--%s: expected a non-empty usage string", tt.name)
			}
		})
	}

	// Neither flag exists on repair: re-sending an opening prompt to a repaired
	// window would start a new conversation, not restore the old one.
	for _, name := range []string{"prompt", "pane"} {
		if worktreeRepairCmd.Flags().Lookup(name) != nil {
			t.Errorf("expected --%s NOT to be registered on repair", name)
		}
	}
}

// --pane must accumulate across repeats rather than overwrite, which is the
// whole point of StringArrayVar.
func TestWorktreeCreateCmd_PaneFlagAccumulates(t *testing.T) {
	resetWorktreeFlags(t)

	if err := worktreeCreateCmd.ParseFlags(
		[]string{"--pane", "make finit", "--pane", "npm run dev"},
	); err != nil {
		t.Fatalf("unexpected flag parse error: %v", err)
	}

	want := []string{"make finit", "npm run dev"}
	if len(createPaneFlags) != len(want) {
		t.Fatalf("expected %v, got %v", want, createPaneFlags)
	}
	for i := range want {
		if createPaneFlags[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, createPaneFlags[i], want[i])
		}
	}
}

// A pane command containing a comma must stay ONE pane. This is why the flag is
// StringArrayVar and not StringSliceVar, which would split on the comma.
func TestWorktreeCreateCmd_PaneFlagDoesNotSplitOnCommas(t *testing.T) {
	resetWorktreeFlags(t)

	if err := worktreeCreateCmd.ParseFlags(
		[]string{"--pane", "go test ./a,./b"},
	); err != nil {
		t.Fatalf("unexpected flag parse error: %v", err)
	}

	if len(createPaneFlags) != 1 || createPaneFlags[0] != "go test ./a,./b" {
		t.Errorf("expected one unsplit pane command, got %v", createPaneFlags)
	}
}

// TestWorktreeCreateCmd_PaneFlagDoesNotLeakBetweenParses is the regression test
// for the reset trap: pflag APPENDS to a stringArray once the flag's `changed`
// is true, so a naive reset (or none) makes a second parse inherit the first's
// values. Two parses in sequence, with the reset between them, must leave the
// second seeing only its own value.
func TestWorktreeCreateCmd_PaneFlagDoesNotLeakBetweenParses(t *testing.T) {
	resetWorktreeFlags(t)

	if err := worktreeCreateCmd.ParseFlags([]string{"--pane", "first"}); err != nil {
		t.Fatalf("unexpected flag parse error on the first parse: %v", err)
	}
	if len(createPaneFlags) != 1 {
		t.Fatalf("first parse: expected 1 pane, got %v", createPaneFlags)
	}

	resetRepeatableFlag(t, worktreeCreateCmd, "pane")

	if err := worktreeCreateCmd.ParseFlags([]string{"--pane", "second"}); err != nil {
		t.Fatalf("unexpected flag parse error on the second parse: %v", err)
	}
	if len(createPaneFlags) != 1 || createPaneFlags[0] != "second" {
		t.Errorf("second parse leaked the first's values: got %v, want [second]", createPaneFlags)
	}
}

// --- applyCreateLayoutOptions ---

// resolveTestLayout resolves a built-in layout by name for the helper tests
// below. This touches no git and no tmux: ResolveLayout only reads the
// package's own registry (a nil config skips config loading entirely).
func resolveTestLayout(t *testing.T, name string) worktree.Layout {
	t.Helper()
	layout, err := worktree.ResolveLayout(name, "", nil)
	if err != nil {
		t.Fatalf("failed to resolve layout %q: %v", name, err)
	}
	return layout
}

// typedClaudeCommand is the claude layout's Pane.Command - the TYPED form devgeta
// send-keys into a pane that already exists. It is the UN-ALIASED command (binary
// plus env prefix), not "cc": preflight probes the binary, so typing the alias sent
// a live pane something the check never verified (ADR-0021's 2026-08-07 amendment).
// Spelled out rather than read off the coder, so this expectation can fail.
const typedClaudeCommand = "CLAUDE_CODE_NO_FLICKER=1 claude"

func paneCommands(layout worktree.Layout) []string {
	commands := make([]string, 0, len(layout.Panes))
	for _, pane := range layout.Panes {
		commands = append(commands, pane.Command)
	}
	return commands
}

// TestApplyCreateLayoutOptions covers the helper create's RunE calls between
// resolving the layout and constructing a WorktreeManager. Every error case here
// is one where RunE returns BEFORE worktree.New(), so no worktree and no tmux
// window can exist - that is the fail-before-side-effects guarantee, tested
// where it actually lives rather than by driving RunE (which would build a real
// manager; see this file's other comments on the cmd-vs-package test split).
func TestApplyCreateLayoutOptions(t *testing.T) {
	t.Run("no flags leaves the layout untouched", func(t *testing.T) {
		got, err := applyCreateLayoutOptions(resolveTestLayout(t, "claude"), "", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if commands := paneCommands(got); len(commands) != 1 ||
			commands[0] != typedClaudeCommand {
			t.Errorf("expected the unmodified claude layout, got %v", commands)
		}
	})

	t.Run("both flags compose, prompt first then extra panes", func(t *testing.T) {
		got, err := applyCreateLayoutOptions(
			resolveTestLayout(t, "claude"),
			"fix the bug",
			[]string{"make finit"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := []string{typedClaudeCommand + " 'fix the bug'", "make finit"}
		commands := paneCommands(got)
		if len(commands) != len(want) {
			t.Fatalf("expected %v, got %v", want, commands)
		}
		for i := range want {
			if commands[i] != want[i] {
				t.Errorf("pane %d: got %q, want %q", i, commands[i], want[i])
			}
		}
	})

	t.Run("a prompt on a layout with no AI pane is an error", func(t *testing.T) {
		_, err := applyCreateLayoutOptions(resolveTestLayout(t, "nvim"), "fix the bug", nil)
		if err == nil {
			t.Fatal("expected an error prompting the nvim layout, got nil")
		}
		if got := err.Error(); !strings.Contains(got, "nvim") {
			t.Errorf("expected the error to name the layout, got %q", got)
		}
	})

	t.Run("an empty pane command is an error", func(t *testing.T) {
		_, err := applyCreateLayoutOptions(resolveTestLayout(t, "claude"), "", []string{""})
		if err == nil {
			t.Fatal("expected an error for an empty --pane, got nil")
		}
		if got := err.Error(); !strings.Contains(got, "--pane") {
			t.Errorf("expected the error to name --pane, got %q", got)
		}
	})

	// Apply order is observable: with both a bad prompt and a bad pane, the
	// prompt error must win, because WithPrompt runs first.
	t.Run("the prompt error wins over a later bad pane", func(t *testing.T) {
		_, err := applyCreateLayoutOptions(
			resolveTestLayout(t, "nvim"),
			"fix the bug",
			[]string{""},
		)
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if strings.Contains(err.Error(), "--pane") {
			t.Errorf("expected the --prompt error to win, got the --pane one: %q", err.Error())
		}
	})
}
