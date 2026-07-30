package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cjairm/devgeta/pkg/paths"
	"github.com/stretchr/testify/assert"
)

// setupIsolatedConfigPaths points paths.Paths at a throwaway temp directory
// for the duration of the test and restores the originals on cleanup.
//
// internal/testutil can't be used here: it imports internal/commands, which
// imports internal/config, which would create an import cycle for this
// package's tests. This mirrors testutil.SetupIsolatedPaths's behavior using
// only pkg/paths, which internal/config already depends on.
func setupIsolatedConfigPaths(t *testing.T) {
	t.Helper()
	tempDir := t.TempDir()
	origAppRoot := paths.Paths.App.Root
	origConfigRoot := paths.Paths.Config.Root
	origTemplatesDir := paths.Paths.App.Configs.Templates
	paths.Paths.App.Root = filepath.Join(tempDir, "app")
	paths.Paths.Config.Root = filepath.Join(tempDir, "config")
	paths.Paths.App.Configs.Templates = filepath.Join(tempDir, "templates")
	t.Cleanup(func() {
		paths.Paths.App.Root = origAppRoot
		paths.Paths.Config.Root = origConfigRoot
		paths.Paths.App.Configs.Templates = origTemplatesDir
	})
}

func TestRemoveFromInstalled(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*GlobalConfig)
		removeItem  string
		removeType  string
		checkField  func(*GlobalConfig) []string
		wantRemains []string
	}{
		{
			name: "removes existing package",
			setup: func(gc *GlobalConfig) {
				gc.Installed.Packages = []string{"git", "tmux", "neovim"}
			},
			removeItem:  "tmux",
			removeType:  "package",
			checkField:  func(gc *GlobalConfig) []string { return gc.Installed.Packages },
			wantRemains: []string{"git", "neovim"},
		},
		{
			name: "no-op when package absent",
			setup: func(gc *GlobalConfig) {
				gc.Installed.Packages = []string{"git"}
			},
			removeItem:  "tmux",
			removeType:  "package",
			checkField:  func(gc *GlobalConfig) []string { return gc.Installed.Packages },
			wantRemains: []string{"git"},
		},
		{
			name: "removes desktop_app",
			setup: func(gc *GlobalConfig) {
				gc.Installed.DesktopApps = []string{"brave", "alacritty", "raycast"}
			},
			removeItem:  "brave",
			removeType:  "desktop_app",
			checkField:  func(gc *GlobalConfig) []string { return gc.Installed.DesktopApps },
			wantRemains: []string{"alacritty", "raycast"},
		},
		{
			name: "does not affect already_installed",
			setup: func(gc *GlobalConfig) {
				gc.Installed.Packages = []string{"git"}
				gc.AlreadyInstalled.Packages = []string{"git"}
			},
			removeItem:  "git",
			removeType:  "package",
			checkField:  func(gc *GlobalConfig) []string { return gc.AlreadyInstalled.Packages },
			wantRemains: []string{"git"},
		},
		{
			name: "unknown item type is no-op",
			setup: func(gc *GlobalConfig) {
				gc.Installed.Packages = []string{"git"}
			},
			removeItem:  "git",
			removeType:  "unknown_type",
			checkField:  func(gc *GlobalConfig) []string { return gc.Installed.Packages },
			wantRemains: []string{"git"},
		},
		{
			name: "removes terminal_tool",
			setup: func(gc *GlobalConfig) {
				gc.Installed.TerminalTools = []string{"tool1", "tool2"}
			},
			removeItem:  "tool1",
			removeType:  "terminal_tool",
			checkField:  func(gc *GlobalConfig) []string { return gc.Installed.TerminalTools },
			wantRemains: []string{"tool2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gc := &GlobalConfig{}
			tt.setup(gc)
			gc.RemoveFromInstalled(tt.removeItem, tt.removeType)
			assert.Equal(t, tt.wantRemains, tt.checkField(gc))
		})
	}
}

// tempFileGlobs lists any leftover ".<name>.tmp.*" files next to the global
// config file, so tests can prove WriteFileAtomic never leaks its scratch file.
func tempFileGlobs(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(
		filepath.Join(filepath.Dir(getGlobalConfigFilePath()), ".*.tmp.*"),
	)
	if err != nil {
		t.Fatalf("failed to glob for leftover temp files: %v", err)
	}
	return matches
}

func TestSave_AtomicRoundTripAndNoLeakedTempFile(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	gc.CurrentFont = "JetBrainsMono"
	gc.Installed.Packages = []string{"git", "tmux"}
	gc.Worktree.DefaultAI = "claude"

	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	assert.Empty(t, tempFileGlobs(t), "Save must not leave its temp file behind")

	loaded := &GlobalConfig{}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	assert.Equal(t, gc.CurrentFont, loaded.CurrentFont)
	assert.Equal(t, gc.Installed.Packages, loaded.Installed.Packages)
	assert.Equal(t, gc.Worktree.DefaultAI, loaded.Worktree.DefaultAI)
}

func TestSave_NeverLeavesTruncatedFileOnDirFailure(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	gc.CurrentFont = "original"
	if err := gc.Save(); err != nil {
		t.Fatalf("initial Save failed: %v", err)
	}

	before, err := os.ReadFile(getGlobalConfigFilePath())
	if err != nil {
		t.Fatalf("failed to read config before failing save: %v", err)
	}

	// Make the config directory read-only so the temp file used by the next
	// Save() can't be created; the write must fail before touching the real
	// file, proving there is no window where the config is left truncated.
	configDir := filepath.Dir(getGlobalConfigFilePath())
	if err := os.Chmod(configDir, 0o555); err != nil {
		t.Fatalf("failed to make config dir read-only: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(configDir, 0o755)
	})

	gc.CurrentFont = "should-not-persist"
	err = gc.Save()
	assert.Error(t, err, "Save must fail when the temp file cannot be created")

	_ = os.Chmod(configDir, 0o755)

	after, err := os.ReadFile(getGlobalConfigFilePath())
	if err != nil {
		t.Fatalf("failed to read config after failed save: %v", err)
	}
	assert.Equal(t, before, after, "a failed Save must never modify the on-disk config")
	assert.Empty(t, tempFileGlobs(t), "a failed Save must not leave a temp file behind")
}

func TestReset_AtomicRoundTripAndNoLeakedTempFile(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	gc.CurrentFont = "will-be-wiped"
	gc.Installed.Packages = []string{"git"}
	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if err := gc.Reset(); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	assert.Empty(t, tempFileGlobs(t), "Reset must not leave its temp file behind")
	assert.Equal(t, "", gc.CurrentFont)
	assert.Nil(t, gc.Installed.Packages)

	loaded := &GlobalConfig{}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	// yaml.v3 round-trips a marshaled nil slice back as an empty (non-nil)
	// slice for fields without `omitempty`, so compare on emptiness/zero
	// value rather than exact struct equality with a bare GlobalConfig{}.
	assert.Equal(t, "", loaded.CurrentFont)
	assert.Empty(t, loaded.Installed.Packages)
	assert.Equal(t, "", loaded.Worktree.DefaultAI)
	assert.Empty(t, loaded.Worktree.RecentRepos)
}

func TestReset_NeverLeavesTruncatedFileOnDirFailure(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	gc.CurrentFont = "keep-me"
	if err := gc.Save(); err != nil {
		t.Fatalf("initial Save failed: %v", err)
	}

	before, err := os.ReadFile(getGlobalConfigFilePath())
	if err != nil {
		t.Fatalf("failed to read config before failing reset: %v", err)
	}

	configDir := filepath.Dir(getGlobalConfigFilePath())
	if err := os.Chmod(configDir, 0o555); err != nil {
		t.Fatalf("failed to make config dir read-only: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(configDir, 0o755)
	})

	err = gc.Reset()
	assert.Error(t, err, "Reset must fail when the temp file cannot be created")

	_ = os.Chmod(configDir, 0o755)

	after, err := os.ReadFile(getGlobalConfigFilePath())
	if err != nil {
		t.Fatalf("failed to read config after failed reset: %v", err)
	}
	assert.Equal(t, before, after, "a failed Reset must never modify the on-disk config")
	assert.Empty(t, tempFileGlobs(t), "a failed Reset must not leave a temp file behind")
}

func TestRecentRepos_YAMLRoundTripIsRFC3339(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	lastUsed := time.Date(2026, 7, 15, 10, 30, 0, 0, time.UTC)
	gc.Worktree.RecentRepos = []RecentRepo{
		{Path: "/repo/one", LastUsed: lastUsed},
	}
	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	raw, err := os.ReadFile(getGlobalConfigFilePath())
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	assert.Contains(t, string(raw), "2026-07-15T10:30:00Z", "LastUsed must serialize as RFC3339")

	loaded := &GlobalConfig{}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if assert.Len(t, loaded.Worktree.RecentRepos, 1) {
		assert.Equal(t, "/repo/one", loaded.Worktree.RecentRepos[0].Path)
		assert.True(t, lastUsed.Equal(loaded.Worktree.RecentRepos[0].LastUsed))
	}
}

func TestLoad_LegacyConfigWithoutRecentReposLoadsUnchanged(t *testing.T) {
	setupIsolatedConfigPaths(t)

	legacyContent := `app_path: ""
config_path: ""
current_font: ""
current_theme: ""
shell:
  mise: false
worktree:
  default_ai: opencode
`
	configPath := getGlobalConfigFilePath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(legacyContent), 0o644); err != nil {
		t.Fatalf("failed to write legacy config file: %v", err)
	}

	gc := &GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatalf("Load failed on legacy config without recent_repos: %v", err)
	}
	assert.Equal(t, "opencode", gc.Worktree.DefaultAI)
	assert.Nil(t, gc.Worktree.RecentRepos)
	assert.Nil(t, gc.Worktree.SearchPaths)
	assert.Equal(t, 0, gc.Worktree.ScanDepth)
	assert.Equal(t, "", gc.Worktree.DefaultLayout)
}

// TestLoad_LegacyConfigWithoutScanFieldsLoadsUnchanged proves a
// pre-repo-scan global_config.yaml (worktree section has only default_ai
// and recent_repos, no search_paths/scan_depth/default_layout keys) still
// loads: the three new fields must come back at their zero values instead
// of failing to unmarshal or defaulting to something non-zero.
func TestLoad_LegacyConfigWithoutScanFieldsLoadsUnchanged(t *testing.T) {
	setupIsolatedConfigPaths(t)

	legacyContent := `app_path: ""
config_path: ""
current_font: ""
current_theme: ""
shell:
  mise: false
worktree:
  default_ai: claude
  recent_repos:
    - path: /repo/one
      last_used: 2026-07-15T10:30:00Z
`
	configPath := getGlobalConfigFilePath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(legacyContent), 0o644); err != nil {
		t.Fatalf("failed to write legacy config file: %v", err)
	}

	gc := &GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatalf("Load failed on legacy config without scan fields: %v", err)
	}
	assert.Equal(t, "claude", gc.Worktree.DefaultAI)
	if assert.Len(t, gc.Worktree.RecentRepos, 1) {
		assert.Equal(t, "/repo/one", gc.Worktree.RecentRepos[0].Path)
	}
	assert.Nil(
		t,
		gc.Worktree.SearchPaths,
		"search_paths must be nil (scanning disabled) when absent from legacy config",
	)
	assert.Equal(
		t,
		0,
		gc.Worktree.ScanDepth,
		"scan_depth must be zero-value when absent from legacy config",
	)
	assert.Equal(
		t,
		"",
		gc.Worktree.DefaultLayout,
		"default_layout must be empty when absent from legacy config",
	)
}

// TestWorktreeConfig_ScanAndLayoutFieldsRoundTrip proves Save/Load round-trips
// SearchPaths, ScanDepth, and DefaultLayout once a user sets them, not just
// that they default correctly when absent (covered above).
func TestWorktreeConfig_ScanAndLayoutFieldsRoundTrip(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	gc.Worktree.SearchPaths = []string{"/code", "/work/repos"}
	gc.Worktree.ScanDepth = 6
	gc.Worktree.DefaultLayout = "main-vertical"

	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded := &GlobalConfig{}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	assert.Equal(t, gc.Worktree.SearchPaths, loaded.Worktree.SearchPaths)
	assert.Equal(t, gc.Worktree.ScanDepth, loaded.Worktree.ScanDepth)
	assert.Equal(t, gc.Worktree.DefaultLayout, loaded.Worktree.DefaultLayout)
}

// TestSave_DefaultAIOmittedWhenEmpty proves default_ai gets the same
// omitempty treatment as every other user-settable worktree field: an unset
// DefaultAI must not persist as a visible `default_ai: ""` key.
func TestSave_DefaultAIOmittedWhenEmpty(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// gc.Worktree.DefaultAI left at its zero value ("") on purpose.

	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	raw, err := os.ReadFile(getGlobalConfigFilePath())
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	assert.NotContains(
		t,
		string(raw),
		"default_ai",
		"an empty default_ai must be omitted from the saved config, like every other unset worktree field",
	)
}

// TestSave_LocationOmittedWhenEmpty proves worktree.location gets the same
// omitempty treatment as every other user-settable worktree field: an unset
// Location must not persist as a visible `location: ""` key, so existing
// installs' saved configs are untouched by this field's addition.
func TestSave_LocationOmittedWhenEmpty(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	// gc.Worktree.Location left at its zero value ("") on purpose.

	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	raw, err := os.ReadFile(getGlobalConfigFilePath())
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	assert.NotContains(
		t,
		string(raw),
		"location",
		"an empty worktree.location must be omitted from the saved config, like every other unset worktree field",
	)
}

// TestWorktreeConfig_LocationRoundTrips proves an explicit non-default
// Location ("in-repo") survives Save/Load unchanged - the shape a `dg config
// set worktree.location in-repo` round trip depends on.
func TestWorktreeConfig_LocationRoundTrips(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	gc.Worktree.Location = WorktreeLocationInRepo

	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded := &GlobalConfig{}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	assert.Equal(t, WorktreeLocationInRepo, loaded.Worktree.Location)
}

// TestLoad_LegacyConfigWithEmptyDefaultAIResolvesToOpencodeDefault is the
// backward-compatibility check for the omitempty change: a config file
// written before this change with an explicit `default_ai: ""` key must
// still load as "" (the same value ResolveLayout/ResolveAICoder already
// treat as "fall back to opencode" - see internal/tooling/worktree/layout.go
// ResolveLayout rung 4). omitempty only changes what Save writes; it must
// not change what Load accepts.
func TestLoad_LegacyConfigWithEmptyDefaultAIResolvesToOpencodeDefault(t *testing.T) {
	setupIsolatedConfigPaths(t)

	legacyContent := `app_path: ""
config_path: ""
current_font: ""
current_theme: ""
shell:
  mise: false
worktree:
  default_ai: ""
`
	configPath := getGlobalConfigFilePath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(legacyContent), 0o644); err != nil {
		t.Fatalf("failed to write legacy config file: %v", err)
	}

	gc := &GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatalf("Load failed on legacy config with explicit empty default_ai: %v", err)
	}
	assert.Equal(
		t,
		"",
		gc.Worktree.DefaultAI,
		"explicit default_ai: \"\" must still load as empty, the same as an absent key",
	)
}

// TestShouldAttachAfterCreate covers the one bool in this config whose default
// is true, so "absent" and "explicitly false" must not collapse to the same
// answer. The nil-receiver case is not hypothetical: the TUI reads this off
// m.gc, which worktree.ResolveLayout already treats as possibly-nil.
func TestShouldAttachAfterCreate(t *testing.T) {
	attachTrue := true
	attachFalse := false

	tests := []struct {
		name string
		gc   *GlobalConfig
		want bool
	}{
		{name: "nil config attaches", gc: nil, want: true},
		{name: "unset key attaches", gc: &GlobalConfig{}, want: true},
		{
			name: "explicit true attaches",
			gc:   &GlobalConfig{Worktree: WorktreeConfig{AttachAfterCreate: &attachTrue}},
			want: true,
		},
		{
			name: "explicit false does not attach",
			gc:   &GlobalConfig{Worktree: WorktreeConfig{AttachAfterCreate: &attachFalse}},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.gc.ShouldAttachAfterCreate())
		})
	}
}

// TestWorktreeConfig_AttachAfterCreateFalseRoundTrips proves an explicit false
// survives Save/Load. This is the case `omitempty` could plausibly have eaten:
// on a *bool it omits only a nil pointer, so a pointer to false still writes
// `attach_after_create: false` and loads back as an explicit false rather than
// silently reverting to the attach default.
func TestWorktreeConfig_AttachAfterCreateFalseRoundTrips(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	attachFalse := false
	gc.Worktree.AttachAfterCreate = &attachFalse

	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded := &GlobalConfig{}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.Worktree.AttachAfterCreate == nil {
		t.Fatal("an explicit false must not load back as nil (unset)")
	}
	assert.False(t, *loaded.Worktree.AttachAfterCreate)
	assert.False(t, loaded.ShouldAttachAfterCreate())
}

// TestWorktreeConfig_AttachAfterCreateAbsentFromOldConfigs proves a config
// written before this key existed still attaches on create — the backward
// compatibility this field's *bool shape exists to protect.
func TestWorktreeConfig_AttachAfterCreateAbsentFromOldConfigs(t *testing.T) {
	setupIsolatedConfigPaths(t)

	gc := &GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := gc.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded := &GlobalConfig{}
	if err := loaded.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	assert.Nil(
		t,
		loaded.Worktree.AttachAfterCreate,
		"key should stay absent, not be written as false",
	)
	assert.True(t, loaded.ShouldAttachAfterCreate())
}

func TestUpsertRecentRepo(t *testing.T) {
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)

	t.Run("prepends a new path", func(t *testing.T) {
		wc := &WorktreeConfig{}
		wc.UpsertRecentRepo("/repo/a", now)
		if assert.Len(t, wc.RecentRepos, 1) {
			assert.Equal(t, "/repo/a", wc.RecentRepos[0].Path)
		}
		wc.UpsertRecentRepo("/repo/b", now.Add(time.Minute))
		if assert.Len(t, wc.RecentRepos, 2) {
			assert.Equal(t, "/repo/b", wc.RecentRepos[0].Path, "most recently used goes first")
			assert.Equal(t, "/repo/a", wc.RecentRepos[1].Path)
		}
	})

	t.Run(
		"re-adding an existing path bumps timestamp and moves to front without duplicating",
		func(t *testing.T) {
			wc := &WorktreeConfig{
				RecentRepos: []RecentRepo{
					{Path: "/repo/b", LastUsed: now.Add(time.Minute)},
					{Path: "/repo/a", LastUsed: now},
				},
			}
			bumped := now.Add(time.Hour)
			wc.UpsertRecentRepo("/repo/a", bumped)

			assert.Len(t, wc.RecentRepos, 2, "re-adding must not create a duplicate entry")
			assert.Equal(t, "/repo/a", wc.RecentRepos[0].Path, "bumped entry moves to the front")
			assert.True(t, bumped.Equal(wc.RecentRepos[0].LastUsed))
			assert.Equal(t, "/repo/b", wc.RecentRepos[1].Path)
		},
	)

	t.Run("caps the list at maxRecentRepos, dropping the oldest", func(t *testing.T) {
		wc := &WorktreeConfig{}
		for i := 0; i < maxRecentRepos+5; i++ {
			wc.UpsertRecentRepo(
				filepath.Join("/repo", string(rune('a'+i))),
				now.Add(time.Duration(i)*time.Minute),
			)
		}
		assert.Len(t, wc.RecentRepos, maxRecentRepos)
		// Most recently inserted (last loop iteration) must be first.
		lastInserted := filepath.Join("/repo", string(rune('a'+maxRecentRepos+4)))
		assert.Equal(t, lastInserted, wc.RecentRepos[0].Path)
		// The oldest entries must have been dropped.
		firstInserted := filepath.Join("/repo", string(rune('a')))
		for _, r := range wc.RecentRepos {
			assert.NotEqual(t, firstInserted, r.Path)
		}
	})
}

func TestPrunedRecentRepos(t *testing.T) {
	tempDir := t.TempDir()
	existingPath := filepath.Join(tempDir, "exists")
	if err := os.MkdirAll(existingPath, 0o755); err != nil {
		t.Fatalf("failed to create existing dir: %v", err)
	}
	missingPath := filepath.Join(tempDir, "missing")

	wc := &WorktreeConfig{
		RecentRepos: []RecentRepo{
			{Path: existingPath, LastUsed: time.Now()},
			{Path: missingPath, LastUsed: time.Now()},
		},
	}

	pruned := wc.PrunedRecentRepos()

	if assert.Len(t, pruned, 1) {
		assert.Equal(t, existingPath, pruned[0].Path)
	}
	assert.Len(t, wc.RecentRepos, 2, "PrunedRecentRepos must not mutate the receiver")
}

func TestCanonicalRepoPath(t *testing.T) {
	t.Run("expands a leading tilde", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		repoDir := filepath.Join(home, "code", "repo")
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatalf("failed to create repo dir: %v", err)
		}

		got := CanonicalRepoPath("~/code/repo")
		want, err := filepath.EvalSymlinks(repoDir)
		if err != nil {
			t.Fatalf("failed to resolve expected path: %v", err)
		}
		assert.Equal(t, want, got)
	})

	t.Run("makes a relative path absolute and cleans it", func(t *testing.T) {
		tempDir := t.TempDir()
		repoDir := filepath.Join(tempDir, "a", "b")
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			t.Fatalf("failed to create repo dir: %v", err)
		}
		originalWd, err := os.Getwd()
		if err != nil {
			t.Fatalf("failed to get working directory: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chdir(originalWd)
		})
		if err := os.Chdir(tempDir); err != nil {
			t.Fatalf("failed to chdir: %v", err)
		}

		got := CanonicalRepoPath(filepath.Join("a", "..", "a", ".", "b"))
		want, err := filepath.EvalSymlinks(repoDir)
		if err != nil {
			t.Fatalf("failed to resolve expected path: %v", err)
		}
		assert.Equal(t, want, got)
		assert.True(t, filepath.IsAbs(got))
	})

	t.Run(
		"falls back to the cleaned absolute path when the path does not exist",
		func(t *testing.T) {
			tempDir := t.TempDir()
			nonExistent := filepath.Join(tempDir, "does", "not", "exist")

			got := CanonicalRepoPath(nonExistent)
			assert.Equal(t, filepath.Clean(nonExistent), got)
		},
	)

	t.Run("resolves symlinks", func(t *testing.T) {
		tempDir := t.TempDir()
		realDir := filepath.Join(tempDir, "real")
		if err := os.MkdirAll(realDir, 0o755); err != nil {
			t.Fatalf("failed to create real dir: %v", err)
		}
		linkPath := filepath.Join(tempDir, "link")
		if err := os.Symlink(realDir, linkPath); err != nil {
			t.Fatalf("failed to create symlink: %v", err)
		}

		got := CanonicalRepoPath(linkPath)
		want, err := filepath.EvalSymlinks(realDir)
		if err != nil {
			t.Fatalf("failed to resolve expected path: %v", err)
		}
		assert.Equal(t, want, got)
		assert.NotEqual(t, linkPath, got)
	})
}
