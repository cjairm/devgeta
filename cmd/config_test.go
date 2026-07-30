/*
 * Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() { testutil.InitLogger() }

// isolateConfigPaths gives the test its own throwaway Config.Root (no
// global_config.yaml present yet), restoring the previous value on cleanup,
// and resets configPlainFlag so a --plain assignment from one test can never
// leak into another. paths.Paths.* mutations must always be paired with
// t.Cleanup restoration per CLAUDE.md's testing-patterns guide.
func isolateConfigPaths(t *testing.T) {
	t.Helper()
	origRoot := paths.Paths.Config.Root
	origPlain := configPlainFlag
	t.Cleanup(func() {
		paths.Paths.Config.Root = origRoot
		configPlainFlag = origPlain
	})
	paths.Paths.Config.Root = t.TempDir()
	configPlainFlag = false
}

// writeMalformedConfig writes corrupt, unparsable YAML directly to
// global_config.yaml (bypassing config.GlobalConfig entirely, since Save()
// can't produce invalid YAML) and returns the exact bytes written, so callers
// can assert the file is left byte-for-byte untouched afterward. Requires
// isolateConfigPaths to have already pointed paths.Paths.Config.Root at a
// throwaway directory.
func writeMalformedConfig(t *testing.T) []byte {
	t.Helper()
	configPath := filepath.Join(
		paths.Paths.Config.Root,
		constants.App.Name,
		constants.App.File.GlobalConfig,
	)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	malformed := []byte("worktree: [this is not valid yaml: :::\n")
	if err := os.WriteFile(configPath, malformed, 0o644); err != nil {
		t.Fatalf("failed to write malformed config file: %v", err)
	}
	return malformed
}

// seedConfig writes directly to global_config.yaml for test setup, going
// through config.Update the same way set/unset do. Update creates the file
// itself when it doesn't exist yet (initializing from a zero-value
// GlobalConfig in memory, inside the lock - see config.Update's doc comment
// in internal/config/lock.go), so no separate Create() call is needed or
// should be added back here.
func seedConfig(t *testing.T, fn func(gc *config.GlobalConfig) error) {
	t.Helper()
	require.NoError(t, config.Update(fn))
}

// runConfigCmd runs c's RunE directly (not c.Execute(), which would redirect
// to the shared rootCmd tree - see resetWorktreeFlags's comment in
// worktree_test.go for why direct RunE calls are this codebase's convention
// for exercising a single subcommand) and returns everything written to its
// output writer plus the RunE error.
func runConfigCmd(t *testing.T, c *cobra.Command, args []string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	c.SetOut(&buf)
	t.Cleanup(func() { c.SetOut(nil) })
	err := c.RunE(c, args)
	return buf.String(), err
}

// --- list / bare `dg config` ---

func TestConfigList_NoConfigFileYet_ShowsAllDefaults(t *testing.T) {
	isolateConfigPaths(t)

	out, err := runConfigCmd(t, configCmd, nil)

	require.NoError(t, err)
	for _, s := range Settings {
		assert.Containsf(t, out, s.Key, "expected %s in list output", s.Key)
	}
	// Nothing configured yet -> every row shows "(default)", not a real value.
	assert.Contains(t, out, "(default)")
	assert.NotContains(t, out, "true -> ") // sanity: not a set/unset line
}

func TestConfigList_BareConfigMatchesListSubcommand(t *testing.T) {
	isolateConfigPaths(t)

	bareOut, err := runConfigCmd(t, configCmd, nil)
	require.NoError(t, err)

	listOut, err := runConfigCmd(t, configListCmd, nil)
	require.NoError(t, err)

	assert.Equal(t, bareOut, listOut)
}

func TestConfigList_ShowsSetValueInsteadOfDefault(t *testing.T) {
	isolateConfigPaths(t)

	seedConfig(t, func(gc *config.GlobalConfig) error {
		gc.Worktree.ScanDepth = 9
		return nil
	})

	out, err := runConfigCmd(t, configListCmd, nil)

	require.NoError(t, err)
	assert.Contains(t, out, "worktree.scan_depth")
	assert.Contains(t, out, "9")
}

func TestConfigList_DefaultColumnAgreesWithGetForDefaultLayout(t *testing.T) {
	// Regression test: list's DEFAULT column for worktree.default_layout used
	// to always print "opencode" (Setting.Default()'s context-free fallback),
	// even when worktree.default_ai was set to something else - disagreeing
	// with `get`/`unset`, which both correctly resolved through
	// worktree.default_ai via effectiveValue. All three must agree.
	isolateConfigPaths(t)

	seedConfig(t, func(gc *config.GlobalConfig) error {
		gc.Worktree.DefaultAI = "claude"
		return nil
	})

	listOut, err := runConfigCmd(t, configListCmd, nil)
	require.NoError(t, err)

	getOut, err := runConfigCmd(t, configGetCmd, []string{"worktree.default_layout"})
	require.NoError(t, err)
	assert.Equal(t, "claude\n", getOut)

	found := false
	for _, line := range strings.Split(listOut, "\n") {
		if strings.HasPrefix(line, "worktree.default_layout") {
			found = true
			assert.Contains(
				t, line, "claude",
				"list's DEFAULT column for worktree.default_layout must show the same "+
					"effective value `get` reports (\"claude\"), not the static \"opencode\"",
			)
			assert.NotContains(
				t, line, "opencode",
				"list must not show the context-free fallback once default_ai overrides it",
			)
		}
	}
	require.True(t, found, "expected a worktree.default_layout row in `dg config list` output")
}

func TestConfigList_PlainSuppressesHint_NonInteractiveAnyway(t *testing.T) {
	isolateConfigPaths(t)
	// isInteractiveTerminal() is always false under `go test` (see
	// tty_test.go), so the hint never appears either way here - this test
	// only pins that --plain doesn't error and still prints the table.
	configPlainFlag = true

	out, err := runConfigCmd(t, configListCmd, nil)

	require.NoError(t, err)
	assert.Contains(t, out, "KEY")
	assert.NotContains(t, out, "Run `dg config get")
}

// --- get ---

func TestConfigGet_NoConfigFileYet_PrintsDefault(t *testing.T) {
	isolateConfigPaths(t)

	out, err := runConfigCmd(t, configGetCmd, []string{"worktree.scan_depth"})

	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(worktree.DefaultScanDepth())+"\n", out)
}

func TestConfigGet_PrintsBareValueNoDecoration(t *testing.T) {
	isolateConfigPaths(t)

	seedConfig(t, func(gc *config.GlobalConfig) error {
		gc.Worktree.ScanDepth = 6
		return nil
	})

	out, err := runConfigCmd(t, configGetCmd, []string{"worktree.scan_depth"})

	require.NoError(t, err)
	assert.Equal(t, "6\n", out, "get must print only the bare value, nothing else")
}

func TestConfigGet_DefaultLayoutFallsBackThroughDefaultAI(t *testing.T) {
	isolateConfigPaths(t)

	seedConfig(t, func(gc *config.GlobalConfig) error {
		gc.Worktree.DefaultAI = "claude"
		return nil
	})

	out, err := runConfigCmd(t, configGetCmd, []string{"worktree.default_layout"})

	require.NoError(t, err)
	assert.Equal(t, "claude\n", out)
}

func TestConfigGet_UnknownKey(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configGetCmd, []string{"worktree.nope"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown setting")
	assert.Contains(t, err.Error(), "worktree.scan_depth")
}

func TestConfigGet_StateKeyRejected(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configGetCmd, []string{"worktree.recent_repos"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown setting")
}

// TestConfigList_MalformedConfig_ClearErrorNoPanic covers the plan's
// "malformed config" requirement for the read path (loadConfigForRead, used
// by both list and get): a corrupt global_config.yaml must produce a plain
// error, not a panic, and loadConfigForRead's os.IsNotExist check must not
// swallow a real parse failure the way it deliberately swallows a missing
// file.
func TestConfigList_MalformedConfig_ClearErrorNoPanic(t *testing.T) {
	isolateConfigPaths(t)
	writeMalformedConfig(t)

	_, err := runConfigCmd(t, configListCmd, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load global config")
}

// --- set ---

func TestConfigSet_CreatesFileWhenMissing(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configSetCmd, []string{"worktree.scan_depth", "6"})
	require.NoError(t, err)

	gc := &config.GlobalConfig{}
	require.NoError(t, gc.Load())
	assert.Equal(t, 6, gc.Worktree.ScanDepth)
}

// TestConfigSet_MalformedConfigLeavesFileUnchanged covers the plan's
// "malformed config" requirement for the write path (configSetCmd, via
// config.Update): a corrupt global_config.yaml must produce a clear error
// and set must not write anything on top of it. Update's non-os.IsNotExist
// error branch (internal/config/lock.go) returns before fn ever runs and
// before Save is ever called, so the bad file on disk must be byte-for-byte
// unchanged afterward.
func TestConfigSet_MalformedConfigLeavesFileUnchanged(t *testing.T) {
	isolateConfigPaths(t)
	malformed := writeMalformedConfig(t)
	configPath := filepath.Join(
		paths.Paths.Config.Root,
		constants.App.Name,
		constants.App.File.GlobalConfig,
	)

	_, err := runConfigCmd(t, configSetCmd, []string{"worktree.scan_depth", "6"})
	require.Error(t, err)

	after, readErr := os.ReadFile(configPath)
	require.NoError(t, readErr)
	assert.Equal(
		t,
		malformed,
		after,
		"a failed set must leave the corrupt file byte-for-byte unchanged, nothing written on top of it",
	)
}

func TestConfigSet_PrintsPreviousDefaultAndNewValue(t *testing.T) {
	isolateConfigPaths(t)

	out, err := runConfigCmd(t, configSetCmd, []string{"worktree.scan_depth", "6"})

	require.NoError(t, err)
	assert.Contains(t, out, "worktree.scan_depth")
	assert.Contains(t, out, strconv.Itoa(worktree.DefaultScanDepth())+" (default)")
	assert.Contains(t, out, "-> 6")
}

func TestConfigSet_PrintsPreviousRealValueOnSecondSet(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configSetCmd, []string{"worktree.scan_depth", "6"})
	require.NoError(t, err)

	out, err := runConfigCmd(t, configSetCmd, []string{"worktree.scan_depth", "8"})

	require.NoError(t, err)
	assert.Contains(t, out, "6 -> 8")
}

func TestConfigSet_ZeroScanDepthReportsStillDefault(t *testing.T) {
	// Regression test: `dg config set worktree.scan_depth 0` used to print
	// "4 (default) -> 0" as if 0 took effect, when scan_depth's
	// `omitempty` tag actually drops 0 from the saved config entirely -
	// `dg config get worktree.scan_depth` kept reporting the default (4)
	// right after. The confirmation message must say so, not claim "0" is
	// now the effective value.
	isolateConfigPaths(t)

	out, err := runConfigCmd(t, configSetCmd, []string{"worktree.scan_depth", "0"})
	require.NoError(t, err)

	defaultStr := strconv.Itoa(worktree.DefaultScanDepth())
	assert.NotContains(
		t,
		out,
		"-> 0\n",
		"must never claim 0 took effect - it round-trips back to default",
	)
	assert.Contains(t, out, "equivalent to unset")
	assert.Contains(t, out, defaultStr+" (default)")

	getOut, err := runConfigCmd(t, configGetCmd, []string{"worktree.scan_depth"})
	require.NoError(t, err)
	assert.Equal(
		t, defaultStr+"\n", getOut,
		"get must still report the default after setting 0 - proving 0 never actually persisted",
	)

	gc := &config.GlobalConfig{}
	require.NoError(t, gc.Load())
	assert.Equal(t, 0, gc.Worktree.ScanDepth)
}

func TestConfigSet_InvalidValueWritesNothing(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configSetCmd, []string{"worktree.scan_depth", "not-a-number"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be an integer")

	gc := &config.GlobalConfig{}
	if loadErr := gc.Load(); loadErr == nil {
		// If Create() already made the file, it must still hold the zero
		// value - the invalid Set must never have been saved.
		assert.Equal(t, 0, gc.Worktree.ScanDepth)
	}
}

func TestConfigSet_InvalidBoolWritesNothing(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configSetCmd, []string{"worktree.attach_after_create", "sideways"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a boolean")
}

func TestConfigSet_UnknownKey(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configSetCmd, []string{"worktree.nope", "1"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown setting")
}

func TestConfigSet_StateKeyRejected(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configSetCmd, []string{"worktree.recent_repos", "x"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown setting")
}

func TestConfigSet_SearchPathsAcceptsMultipleValues(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configSetCmd, []string{"worktree.search_paths", "/a", "/b"})
	require.NoError(t, err)

	gc := &config.GlobalConfig{}
	require.NoError(t, gc.Load())
	assert.Equal(t, []string{"/a", "/b"}, gc.Worktree.SearchPaths)
}

func TestConfigSet_ScanDepthRejectsMultipleValues(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configSetCmd, []string{"worktree.scan_depth", "1", "2"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts exactly one value")
}

// --- unset ---

func TestConfigUnset_RoundTrip_SetThenUnsetFallsBackToDefault(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configSetCmd, []string{"worktree.scan_depth", "6"})
	require.NoError(t, err)

	getOut, err := runConfigCmd(t, configGetCmd, []string{"worktree.scan_depth"})
	require.NoError(t, err)
	assert.Equal(t, "6\n", getOut)

	unsetOut, err := runConfigCmd(t, configUnsetCmd, []string{"worktree.scan_depth"})
	require.NoError(t, err)
	assert.Contains(t, unsetOut, "now using default")
	assert.Contains(t, unsetOut, strconv.Itoa(worktree.DefaultScanDepth()))

	getOut, err = runConfigCmd(t, configGetCmd, []string{"worktree.scan_depth"})
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(worktree.DefaultScanDepth())+"\n", getOut)

	gc := &config.GlobalConfig{}
	require.NoError(t, gc.Load())
	assert.Equal(t, 0, gc.Worktree.ScanDepth, "unset must clear the field back to its zero value")
}

func TestConfigUnset_DefaultLayoutReportsEffectiveDefaultAI(t *testing.T) {
	// Regression check for the effectiveValue fix: unset's "now using
	// default (...)" must match what `get` reports right after - both must
	// resolve through worktree.default_ai, not the static, context-free
	// "opencode" Setting.Default() reports.
	isolateConfigPaths(t)

	seedConfig(t, func(gc *config.GlobalConfig) error {
		gc.Worktree.DefaultAI = "claude"
		gc.Worktree.DefaultLayout = "nvim"
		return nil
	})

	unsetOut, err := runConfigCmd(t, configUnsetCmd, []string{"worktree.default_layout"})
	require.NoError(t, err)
	assert.Contains(t, unsetOut, "now using default (claude)")

	getOut, err := runConfigCmd(t, configGetCmd, []string{"worktree.default_layout"})
	require.NoError(t, err)
	assert.Equal(t, "claude\n", getOut)
}

func TestConfigSet_DefaultLayoutShowsEffectivePreviousValue(t *testing.T) {
	isolateConfigPaths(t)

	seedConfig(t, func(gc *config.GlobalConfig) error {
		gc.Worktree.DefaultAI = "claude"
		return nil
	})

	out, err := runConfigCmd(t, configSetCmd, []string{"worktree.default_layout", "nvim"})

	require.NoError(t, err)
	assert.Contains(t, out, "claude (default) -> nvim")
}

func TestConfigUnset_CreatesFileWhenMissing(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configUnsetCmd, []string{"worktree.scan_depth"})

	require.NoError(t, err)
	gc := &config.GlobalConfig{}
	require.NoError(t, gc.Load())
}

func TestConfigUnset_UnknownKey(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configUnsetCmd, []string{"worktree.nope"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown setting")
}

func TestConfigUnset_StateKeyRejected(t *testing.T) {
	isolateConfigPaths(t)

	_, err := runConfigCmd(t, configUnsetCmd, []string{"integrations.rtk_claude_hook"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown setting")
}

// --- go-through-Update guarantee ---

func TestConfigSetUnset_GoThroughUpdateNotPlainSave(t *testing.T) {
	// This is the load-bearing guarantee Task 1 exists for: a concurrent
	// writer's change made between this test's Create() and its set/unset
	// must survive. Simulate that by writing a second field directly via
	// Update after Create() but logically "concurrently" is hard to test
	// without real concurrency; instead this asserts the observable
	// consequence - a value set by a prior Update survives a later
	// set/unset of a *different* key, which would NOT be true if
	// set/unset used a plain Load-mutate-Save that raced a stale in-memory
	// copy. Here it's sequential, but proves neither command ever nukes
	// unrelated fields (Load/Save's contract) while going through Update
	// (not calling gc.Save() itself, so if `config.Update` were bypassed
	// this test still could not directly observe the lock - the strongest
	// black-box check available in a single-process test).
	isolateConfigPaths(t)

	seedConfig(t, func(gc *config.GlobalConfig) error {
		gc.Worktree.DefaultAI = "claude"
		return nil
	})

	_, err := runConfigCmd(t, configSetCmd, []string{"worktree.scan_depth", "6"})
	require.NoError(t, err)

	gc := &config.GlobalConfig{}
	require.NoError(t, gc.Load())
	assert.Equal(t, "claude", gc.Worktree.DefaultAI, "set must not clobber unrelated fields")
	assert.Equal(t, 6, gc.Worktree.ScanDepth)

	_, err = runConfigCmd(t, configUnsetCmd, []string{"worktree.scan_depth"})
	require.NoError(t, err)

	// A fresh struct, not a reload onto gc: scan_depth's yaml tag has
	// omitempty, so a zero value is absent from the document entirely, and
	// yaml.Unmarshal only overwrites fields present in the document - reusing
	// gc here would leave its stale in-memory ScanDepth=6 untouched.
	gc2 := &config.GlobalConfig{}
	require.NoError(t, gc2.Load())
	assert.Equal(t, "claude", gc2.Worktree.DefaultAI, "unset must not clobber unrelated fields")
	assert.Equal(t, 0, gc2.Worktree.ScanDepth)
}

// --- cobra wiring ---

func TestConfigCmd_RegisteredOnRoot(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "config" {
			found = true
		}
	}
	assert.True(t, found, "expected `dg config` to be registered on rootCmd")
}

func TestConfigCmd_HasFourSubcommands(t *testing.T) {
	names := map[string]bool{}
	for _, c := range configCmd.Commands() {
		names[c.Name()] = true
	}
	for _, want := range []string{"list", "get", "set", "unset"} {
		assert.Truef(t, names[want], "expected `dg config %s` to be registered", want)
	}
}

func TestConfigCmd_ArgsRejectsUnknownSubcommandLikeArg(t *testing.T) {
	err := configCmd.Args(configCmd, []string{"bogus"})
	require.Error(t, err)
}

// --- real cobra Args validation, via ValidateArgs (the same path Execute()
// takes) ---
//
// The tests above call RunE directly, which never touches cobra's Args
// field - a regression that weakens e.g. configSetCmd's
// cobra.MinimumNArgs(2) to MinimumNArgs(1) would pass every test above
// while shipping broken arity validation. ValidateArgs(args) is the real
// cobra entry point (Execute calls it before RunE), so these exercise the
// actual registered Args func instead of re-implementing it in the test.

func TestConfigCmd_ValidateArgs_NoArgsOK_ExtraArgRejected(t *testing.T) {
	require.NoError(t, configCmd.ValidateArgs(nil))

	err := configCmd.ValidateArgs([]string{"bogus"})
	require.Error(t, err)
}

func TestConfigListCmd_ValidateArgs_NoArgsOK_ExtraArgRejected(t *testing.T) {
	require.NoError(t, configListCmd.ValidateArgs(nil))

	err := configListCmd.ValidateArgs([]string{"extra"})
	require.Error(t, err)
}

func TestConfigGetCmd_ValidateArgs_RequiresExactlyOne(t *testing.T) {
	require.Error(t, configGetCmd.ValidateArgs(nil), "expected zero args to be rejected")
	require.Error(
		t,
		configGetCmd.ValidateArgs([]string{"a", "b"}),
		"expected two args to be rejected",
	)
	require.NoError(t, configGetCmd.ValidateArgs([]string{"worktree.scan_depth"}))
}

func TestConfigSetCmd_ValidateArgs_RequiresAtLeastTwo(t *testing.T) {
	require.Error(t, configSetCmd.ValidateArgs(nil), "expected zero args to be rejected")
	require.Error(
		t,
		configSetCmd.ValidateArgs([]string{"worktree.scan_depth"}),
		"expected a single arg (key with no value) to be rejected - this is the exact"+
			" regression a MinimumNArgs(2)->MinimumNArgs(1) weakening would miss",
	)
	require.NoError(t, configSetCmd.ValidateArgs([]string{"worktree.scan_depth", "6"}))
	require.NoError(
		t,
		configSetCmd.ValidateArgs([]string{"worktree.search_paths", "/a", "/b"}),
		"set must still accept more than 2 args for multi-value settings",
	)
}

func TestConfigUnsetCmd_ValidateArgs_RequiresExactlyOne(t *testing.T) {
	require.Error(t, configUnsetCmd.ValidateArgs(nil), "expected zero args to be rejected")
	require.Error(
		t,
		configUnsetCmd.ValidateArgs([]string{"a", "b"}),
		"expected two args to be rejected",
	)
	require.NoError(t, configUnsetCmd.ValidateArgs([]string{"worktree.scan_depth"}))
}

// --- real --plain flag wiring, via ParseFlags (not direct variable
// mutation) ---
//
// isolateConfigPaths above sets configPlainFlag directly, which never
// verifies the BoolVar registration in cmd/config.go's init() actually
// binds "--plain" to that variable. ParseFlags is the established pattern
// for this in this codebase - see resetWorktreeFlags/
// TestWorktreeCreateCmd_AIAndLayoutMutuallyExclusive in worktree_test.go.

// resetConfigPlainFlag saves configPlainFlag and restores it on cleanup, so
// one test's ParseFlags call can never leak its value into another. It does
// NOT reset the flag's parsed "Changed" state: pflag's Set writes through the
// BoolVar binding (which would just overwrite the restored value right back)
// and always leaves Changed true regardless of what value is set, so calling
// it here could never actually clear Changed - restoring the variable
// directly is the only thing this cleanup can do.
func resetConfigPlainFlag(t *testing.T) {
	t.Helper()
	origPlain := configPlainFlag
	t.Cleanup(func() {
		configPlainFlag = origPlain
	})
}

func TestConfigCmd_PlainFlag_ParsesAndBindsVariable(t *testing.T) {
	resetConfigPlainFlag(t)

	err := configCmd.ParseFlags([]string{"--plain"})

	require.NoError(t, err)
	assert.True(
		t,
		configPlainFlag,
		"expected --plain, parsed by real cobra flag registration, to set configPlainFlag",
	)
	assert.True(t, configCmd.PersistentFlags().Changed("plain"))
}

func TestConfigListCmd_PlainFlag_InheritedFromParentAndParses(t *testing.T) {
	// --plain is registered as a PersistentFlag on configCmd specifically so
	// `dg config list --plain` (not just bare `dg config --plain`) works -
	// see its BoolVar's doc comment in cmd/config.go. Parsing it through
	// configListCmd (the child), not configCmd, is what actually proves
	// that inheritance.
	resetConfigPlainFlag(t)

	err := configListCmd.ParseFlags([]string{"--plain"})

	require.NoError(t, err)
	assert.True(t, configPlainFlag)
}

func TestUnknownSettingError_ListsAllRegisteredKeys(t *testing.T) {
	err := unknownSettingError("nope")

	require.Error(t, err)
	for _, s := range Settings {
		assert.Contains(t, err.Error(), s.Key)
	}
}

func TestSettingsTable_HeaderAndAllKeysPresent(t *testing.T) {
	out := settingsTable(&config.GlobalConfig{})

	assert.True(t, strings.HasPrefix(out, "KEY"))
	for _, s := range Settings {
		assert.Contains(t, out, s.Key)
		assert.Contains(t, out, s.Description)
	}
}
