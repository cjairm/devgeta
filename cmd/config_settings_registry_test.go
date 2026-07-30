package cmd

import (
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() { testutil.InitLogger() }

// findSettingForTest is a small local helper (not FindSetting - this tests
// FindSetting itself too, further down) that fails the test immediately if
// the key isn't registered, so every settings test below can assume it has a
// valid *Setting without repeating the same "ok" check everywhere.
func findSettingForTest(t *testing.T, key string) *Setting {
	t.Helper()
	s, ok := FindSetting(key)
	require.True(t, ok, "expected %q to be registered", key)
	return s
}

func TestFindSetting(t *testing.T) {
	s, ok := FindSetting("worktree.default_ai")
	require.True(t, ok)
	assert.Equal(t, "worktree.default_ai", s.Key)

	_, ok = FindSetting("worktree.nonexistent")
	assert.False(t, ok)
}

// --- worktree.default_ai ---

func TestSettingsDefaultAI_Default(t *testing.T) {
	s := findSettingForTest(t, "worktree.default_ai")
	assert.Equal(t, "opencode", s.Default())
}

func TestSettingsDefaultAI_GetUnset(t *testing.T) {
	s := findSettingForTest(t, "worktree.default_ai")
	gc := &config.GlobalConfig{}

	value, isSet := s.Get(gc)

	assert.False(t, isSet)
	assert.Equal(t, "", value)
}

func TestSettingsDefaultAI_SetValid(t *testing.T) {
	s := findSettingForTest(t, "worktree.default_ai")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"claude"})

	require.NoError(t, err)
	assert.Equal(t, "claude", gc.Worktree.DefaultAI)

	value, isSet := s.Get(gc)
	assert.True(t, isSet)
	assert.Equal(t, "claude", value)
}

func TestSettingsDefaultAI_SetAlias(t *testing.T) {
	s := findSettingForTest(t, "worktree.default_ai")
	gc := &config.GlobalConfig{}

	// Aliases (cc, oc, claudecode) are accepted by ResolveAICoder and stored
	// as-given - canonicalization happens at resolve time, not set time.
	err := s.Set(gc, []string{"cc"})

	require.NoError(t, err)
	assert.Equal(t, "cc", gc.Worktree.DefaultAI)
}

func TestSettingsDefaultAI_SetInvalid(t *testing.T) {
	s := findSettingForTest(t, "worktree.default_ai")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"gpt5"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown AI coder alias")
	assert.Equal(t, "", gc.Worktree.DefaultAI)
}

func TestSettingsDefaultAI_SetTooManyValues(t *testing.T) {
	s := findSettingForTest(t, "worktree.default_ai")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"claude", "opencode"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts exactly one value")
}

func TestSettingsDefaultAI_Unset(t *testing.T) {
	s := findSettingForTest(t, "worktree.default_ai")
	gc := &config.GlobalConfig{Worktree: config.WorktreeConfig{DefaultAI: "claude"}}

	s.Unset(gc)

	assert.Equal(t, "", gc.Worktree.DefaultAI)
	_, isSet := s.Get(gc)
	assert.False(t, isSet)
}

// --- worktree.search_paths ---

func TestSettingsSearchPaths_Default(t *testing.T) {
	s := findSettingForTest(t, "worktree.search_paths")
	assert.Equal(t, "", s.Default())
}

func TestSettingsSearchPaths_GetUnset(t *testing.T) {
	s := findSettingForTest(t, "worktree.search_paths")
	gc := &config.GlobalConfig{}

	value, isSet := s.Get(gc)

	assert.False(t, isSet)
	assert.Equal(t, "", value)
}

func TestSettingsSearchPaths_SetSingle(t *testing.T) {
	s := findSettingForTest(t, "worktree.search_paths")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"~/code"})

	require.NoError(t, err)
	assert.Equal(t, []string{"~/code"}, gc.Worktree.SearchPaths)

	value, isSet := s.Get(gc)
	assert.True(t, isSet)
	assert.Equal(t, "~/code", value)
}

func TestSettingsSearchPaths_SetVariadic(t *testing.T) {
	s := findSettingForTest(t, "worktree.search_paths")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"~/code", "~/work"})

	require.NoError(t, err)
	assert.Equal(t, []string{"~/code", "~/work"}, gc.Worktree.SearchPaths)

	value, isSet := s.Get(gc)
	assert.True(t, isSet)
	assert.Equal(t, "~/code, ~/work", value)
}

func TestSettingsSearchPaths_SetEmptyRejected(t *testing.T) {
	s := findSettingForTest(t, "worktree.search_paths")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires at least one path")
}

func TestSettingsSearchPaths_Unset(t *testing.T) {
	s := findSettingForTest(t, "worktree.search_paths")
	gc := &config.GlobalConfig{Worktree: config.WorktreeConfig{SearchPaths: []string{"~/code"}}}

	s.Unset(gc)

	assert.Nil(t, gc.Worktree.SearchPaths)
	_, isSet := s.Get(gc)
	assert.False(t, isSet)
}

// --- worktree.scan_depth ---

func TestSettingsScanDepth_Default(t *testing.T) {
	s := findSettingForTest(t, "worktree.scan_depth")
	assert.Equal(t, "4", s.Default())
	assert.Equal(t, 4, worktree.DefaultScanDepth())
}

func TestSettingsScanDepth_GetUnset(t *testing.T) {
	s := findSettingForTest(t, "worktree.scan_depth")
	gc := &config.GlobalConfig{}

	value, isSet := s.Get(gc)

	assert.False(t, isSet)
	assert.Equal(t, "0", value)
}

func TestSettingsScanDepth_SetValid(t *testing.T) {
	s := findSettingForTest(t, "worktree.scan_depth")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"6"})

	require.NoError(t, err)
	assert.Equal(t, 6, gc.Worktree.ScanDepth)

	value, isSet := s.Get(gc)
	assert.True(t, isSet)
	assert.Equal(t, "6", value)
}

func TestSettingsScanDepth_SetNegativeRejected(t *testing.T) {
	s := findSettingForTest(t, "worktree.scan_depth")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"-1"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-negative")
	assert.Equal(t, 0, gc.Worktree.ScanDepth)
}

func TestSettingsScanDepth_SetNonIntegerRejected(t *testing.T) {
	s := findSettingForTest(t, "worktree.scan_depth")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"deep"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be an integer")
}

func TestSettingsScanDepth_SetTooManyValues(t *testing.T) {
	s := findSettingForTest(t, "worktree.scan_depth")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"1", "2"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts exactly one value")
}

// TestSettingsScanDepth_SetZeroRoundTripsToUnset is the regression test for
// the bug where `dg config set worktree.scan_depth 0` reported "-> 0" as if
// 0 were now the effective value, when scan_depth's yaml `omitempty` tag
// actually drops a zero value from the saved document entirely - so Get
// reports isSet=false right back, the same as if scan_depth had never been
// set at all. Setting.Set/Get is the layer this round-trip is provable at;
// cmd/config_test.go covers the corresponding CLI-visible symptom (the `set`
// command's confirmation message).
func TestSettingsScanDepth_SetZeroRoundTripsToUnset(t *testing.T) {
	s := findSettingForTest(t, "worktree.scan_depth")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"0"})
	require.NoError(t, err, "0 is a valid (non-negative) input, so Set itself must not error")

	value, isSet := s.Get(gc)
	assert.False(
		t,
		isSet,
		"0 is scan_depth's zero value - Get must report isSet=false, not a persisted 0",
	)
	assert.Equal(t, "0", value)
}

func TestSettingsScanDepth_Unset(t *testing.T) {
	s := findSettingForTest(t, "worktree.scan_depth")
	gc := &config.GlobalConfig{Worktree: config.WorktreeConfig{ScanDepth: 8}}

	s.Unset(gc)

	assert.Equal(t, 0, gc.Worktree.ScanDepth)
	_, isSet := s.Get(gc)
	assert.False(t, isSet)
}

// --- worktree.default_layout ---

func TestSettingsDefaultLayout_Default(t *testing.T) {
	s := findSettingForTest(t, "worktree.default_layout")
	assert.Equal(t, "opencode", s.Default())
}

func TestSettingsDefaultLayout_GetUnset(t *testing.T) {
	s := findSettingForTest(t, "worktree.default_layout")
	gc := &config.GlobalConfig{}

	value, isSet := s.Get(gc)

	assert.False(t, isSet)
	assert.Equal(t, "", value)
}

func TestSettingsDefaultLayout_SetValid(t *testing.T) {
	s := findSettingForTest(t, "worktree.default_layout")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"claude-nvim"})

	require.NoError(t, err)
	assert.Equal(t, "claude-nvim", gc.Worktree.DefaultLayout)

	value, isSet := s.Get(gc)
	assert.True(t, isSet)
	assert.Equal(t, "claude-nvim", value)
}

func TestSettingsDefaultLayout_SetInvalid(t *testing.T) {
	s := findSettingForTest(t, "worktree.default_layout")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"quad"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown layout")
	// Same error listing valid names that --layout/the N-picker would hit.
	for _, name := range worktree.BuiltinLayoutNames() {
		assert.Contains(t, err.Error(), name)
	}
	assert.Equal(t, "", gc.Worktree.DefaultLayout)
}

func TestSettingsDefaultLayout_SetTooManyValues(t *testing.T) {
	s := findSettingForTest(t, "worktree.default_layout")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"nvim", "claude"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts exactly one value")
}

func TestSettingsDefaultLayout_Unset(t *testing.T) {
	s := findSettingForTest(t, "worktree.default_layout")
	gc := &config.GlobalConfig{Worktree: config.WorktreeConfig{DefaultLayout: "nvim"}}

	s.Unset(gc)

	assert.Equal(t, "", gc.Worktree.DefaultLayout)
	_, isSet := s.Get(gc)
	assert.False(t, isSet)
}

// --- worktree.attach_after_create ---

func TestSettingsAttachAfterCreate_Default(t *testing.T) {
	s := findSettingForTest(t, "worktree.attach_after_create")
	assert.Equal(t, "true", s.Default())
}

func TestSettingsAttachAfterCreate_GetUnset(t *testing.T) {
	s := findSettingForTest(t, "worktree.attach_after_create")
	gc := &config.GlobalConfig{}

	value, isSet := s.Get(gc)

	assert.False(t, isSet)
	assert.Equal(t, "", value)
}

func TestSettingsAttachAfterCreate_SetFalse(t *testing.T) {
	s := findSettingForTest(t, "worktree.attach_after_create")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"false"})

	require.NoError(t, err)
	require.NotNil(t, gc.Worktree.AttachAfterCreate)
	assert.False(t, *gc.Worktree.AttachAfterCreate)
	assert.False(t, gc.ShouldAttachAfterCreate())

	value, isSet := s.Get(gc)
	assert.True(t, isSet)
	assert.Equal(t, "false", value)
}

func TestSettingsAttachAfterCreate_SetTrue(t *testing.T) {
	s := findSettingForTest(t, "worktree.attach_after_create")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"true"})

	require.NoError(t, err)
	require.NotNil(t, gc.Worktree.AttachAfterCreate)
	assert.True(t, *gc.Worktree.AttachAfterCreate)
}

func TestSettingsAttachAfterCreate_SetInvalid(t *testing.T) {
	s := findSettingForTest(t, "worktree.attach_after_create")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"maybe"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a boolean")
	assert.Nil(t, gc.Worktree.AttachAfterCreate)
}

func TestSettingsAttachAfterCreate_SetTooManyValues(t *testing.T) {
	s := findSettingForTest(t, "worktree.attach_after_create")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"true", "false"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts exactly one value")
}

func TestSettingsAttachAfterCreate_Unset(t *testing.T) {
	s := findSettingForTest(t, "worktree.attach_after_create")
	falseVal := false
	gc := &config.GlobalConfig{
		Worktree: config.WorktreeConfig{AttachAfterCreate: &falseVal},
	}

	s.Unset(gc)

	assert.Nil(t, gc.Worktree.AttachAfterCreate)
	_, isSet := s.Get(gc)
	assert.False(t, isSet)
	// Unset restores the documented default: attach.
	assert.True(t, gc.ShouldAttachAfterCreate())
}

// --- worktree.notify_sound ---

func TestSettingsNotifySound_Default(t *testing.T) {
	s := findSettingForTest(t, "worktree.notify_sound")
	assert.Equal(t, "false", s.Default())
}

func TestSettingsNotifySound_GetUnset(t *testing.T) {
	s := findSettingForTest(t, "worktree.notify_sound")
	gc := &config.GlobalConfig{}

	value, isSet := s.Get(gc)

	assert.False(t, isSet)
	assert.Equal(t, "false", value)
}

func TestSettingsNotifySound_SetTrue(t *testing.T) {
	s := findSettingForTest(t, "worktree.notify_sound")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"true"})

	require.NoError(t, err)
	assert.True(t, gc.Worktree.NotifySound)

	value, isSet := s.Get(gc)
	assert.True(t, isSet)
	assert.Equal(t, "true", value)
}

func TestSettingsNotifySound_SetFalse(t *testing.T) {
	s := findSettingForTest(t, "worktree.notify_sound")
	gc := &config.GlobalConfig{Worktree: config.WorktreeConfig{NotifySound: true}}

	err := s.Set(gc, []string{"false"})

	require.NoError(t, err)
	assert.False(t, gc.Worktree.NotifySound)

	// false is notify_sound's zero value, so Get reports isSet=false right
	// back - the same documented trade-off worktree.scan_depth's Set(0)
	// round-trip has (TestSettingsScanDepth_SetZeroRoundTripsToUnset).
	value, isSet := s.Get(gc)
	assert.False(t, isSet)
	assert.Equal(t, "false", value)
}

func TestSettingsNotifySound_SetInvalid(t *testing.T) {
	s := findSettingForTest(t, "worktree.notify_sound")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"maybe"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a boolean")
	assert.False(t, gc.Worktree.NotifySound)
}

func TestSettingsNotifySound_SetTooManyValues(t *testing.T) {
	s := findSettingForTest(t, "worktree.notify_sound")
	gc := &config.GlobalConfig{}

	err := s.Set(gc, []string{"true", "false"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts exactly one value")
}

func TestSettingsNotifySound_Unset(t *testing.T) {
	s := findSettingForTest(t, "worktree.notify_sound")
	gc := &config.GlobalConfig{Worktree: config.WorktreeConfig{NotifySound: true}}

	s.Unset(gc)

	assert.False(t, gc.Worktree.NotifySound)
	_, isSet := s.Get(gc)
	assert.False(t, isSet)
}
