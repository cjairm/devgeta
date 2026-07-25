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
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
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
	origRepairAI := repairAIFlag
	origRepairLayout := repairLayoutFlag
	origGlobalConfig := globalConfig
	t.Cleanup(func() {
		createAIFlag = origCreateAI
		createLayoutFlag = origCreateLayout
		repairAIFlag = origRepairAI
		repairLayoutFlag = origRepairLayout
		globalConfig = origGlobalConfig
		// Cobra retains parsed flag values/"changed" state on the shared
		// command objects between Execute calls; reset both commands' flag
		// sets so a later test starts from a clean, unparsed state.
		_ = worktreeCreateCmd.Flags().Set("ai", "")
		_ = worktreeCreateCmd.Flags().Set("layout", "")
		_ = worktreeRepairCmd.Flags().Set("ai", "")
		_ = worktreeRepairCmd.Flags().Set("layout", "")
	})
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
