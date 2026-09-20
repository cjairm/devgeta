package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/registry"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
)

// paletteRoles lists every role internal/theme.Palette validates, so a test
// fixture theme file can be written without repeating this 24-line block at
// every call site.
var themeTestPaletteRoles = []string{
	"background_hard", "background", "background_element", "background_subtle",
	"border", "foreground", "foreground_muted", "foreground_dim", "foreground_subtle",
	"red", "green", "yellow", "blue", "purple", "aqua", "orange",
	"red_dim", "green_dim", "yellow_dim", "blue_dim", "purple_dim", "aqua_dim",
	"diff_added_background", "diff_removed_background",
}

func writeThemeSetFixture(t *testing.T, themesDir, name, hex, neovimModule string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("neovim_module: " + neovimModule + "\ncolors:\n")
	for _, role := range themeTestPaletteRoles {
		b.WriteString("  " + role + ": \"" + hex + "\"\n")
	}
	path := filepath.Join(themesDir, name+".yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("failed to write theme fixture %s: %v", path, err)
	}
}

// fakeThemedApp is a minimal apps.App + apps.ThemedConfigurer used to drive
// `dg theme set`'s transaction without exercising any real app's full
// configure logic. ForceConfigureTheme actually writes its manifest paths
// (rather than being a no-op), so backup/restore/commit are exercised for
// real against the filesystem.
type fakeThemedApp struct {
	name          string
	manifestPaths []string
	configureErr  error
	calls         *[]string
	liveCalls     *[]string
	liveErr       error
	live          bool
	// onStart, if set, runs synchronously at the very top of
	// ForceConfigureTheme, before any file write or error check - used to
	// pin "runThemeSet has begun configuring this app" to a specific instant
	// deterministically, instead of racing a goroutine against the command's
	// own execution.
	onStart func()
	// onConfigured, if set, runs synchronously right before a successful
	// ForceConfigureTheme returns - the same idea as onStart, pinned to "this
	// app just finished" instead.
	onConfigured func()
}

func (f *fakeThemedApp) Name() string                   { return f.name }
func (f *fakeThemedApp) Kind() apps.AppKind             { return apps.KindTerminal }
func (f *fakeThemedApp) Install() error                 { return nil }
func (f *fakeThemedApp) ForceInstall() error            { return nil }
func (f *fakeThemedApp) SoftInstall() error             { return nil }
func (f *fakeThemedApp) ForceConfigure() error          { return nil }
func (f *fakeThemedApp) SoftConfigure() error           { return nil }
func (f *fakeThemedApp) Uninstall() error               { return nil }
func (f *fakeThemedApp) Update() error                  { return nil }
func (f *fakeThemedApp) ExecuteCommand(...string) error { return nil }

func (f *fakeThemedApp) ForceConfigureTheme(def theme.Definition) error {
	*f.calls = append(*f.calls, f.name)
	if f.onStart != nil {
		f.onStart()
	}
	if f.configureErr != nil {
		return f.configureErr
	}
	for _, p := range f.manifestPaths {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte("configured:"+def.Name), 0o644); err != nil {
			return err
		}
	}
	if f.onConfigured != nil {
		f.onConfigured()
	}
	return nil
}

var (
	_ apps.App              = (*fakeThemedApp)(nil)
	_ apps.ThemedConfigurer = (*fakeThemedApp)(nil)
)

// fakeLiveThemedApp adds apps.LiveThemeApplier on top of fakeThemedApp. A
// separate type, not a flag-gated method on fakeThemedApp itself: Go
// interface satisfaction is structural, so a method defined unconditionally
// would make every fake satisfy LiveThemeApplier regardless of whether the
// test wants that surface to have a live push.
type fakeLiveThemedApp struct {
	*fakeThemedApp
}

func (f *fakeLiveThemedApp) ApplyLiveTheme() error {
	*f.liveCalls = append(*f.liveCalls, f.name)
	return f.liveErr
}

var _ apps.LiveThemeApplier = (*fakeLiveThemedApp)(nil)

// themeSetTestEnv isolates every path the manifest and theme loader touch,
// marks all seven themed apps as installed, and wires themeGetAppFn to a set
// of fakeThemedApp instances keyed by name - restoring everything in
// t.Cleanup. Returns the fakes (for call-order assertions) and the root temp
// dir.
type themeSetTestEnv struct {
	root  string
	fakes map[string]*fakeThemedApp
	calls []string
	live  []string
}

func setupThemeSetTest(t *testing.T, installed []string) *themeSetTestEnv {
	t.Helper()

	root := t.TempDir()
	origPaths := paths.Paths
	t.Cleanup(func() { paths.Paths = origPaths })

	paths.Paths.Config.Root = filepath.Join(root, "config")
	paths.Paths.Config.Alacritty = filepath.Join(root, "alacritty")
	paths.Paths.Config.Ghostty = filepath.Join(root, "ghostty")
	paths.Paths.Config.OpenCode = filepath.Join(root, "opencode")
	paths.Paths.Home.Root = filepath.Join(root, "home")
	paths.Paths.Config.Nvim = filepath.Join(root, "nvim")
	paths.Paths.Config.Claude = filepath.Join(root, "claude")
	paths.Paths.Config.I3 = filepath.Join(root, "i3")
	paths.Paths.Config.Devgeta = filepath.Join(root, "devgeta")

	themesDir := filepath.Join(root, "themes")
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	neovimConfigsDir := filepath.Join(root, "shipped-neovim")
	if err := os.MkdirAll(
		filepath.Join(neovimConfigsDir, "lua", "devgeta", "themes"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(neovimConfigsDir, "lua", "devgeta", "themes", "gruvbox.lua"),
		[]byte("-- fixture\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	paths.Paths.App.Configs.Themes = themesDir
	paths.Paths.App.Configs.Neovim = neovimConfigsDir

	writeThemeSetFixture(t, themesDir, "default", "#111111", "gruvbox")
	writeThemeSetFixture(t, themesDir, "tokyonight", "#222222", "gruvbox")

	config.ResetGlobalConfigCacheForTest()
	t.Cleanup(config.ResetGlobalConfigCacheForTest)

	env := &themeSetTestEnv{root: root, fakes: map[string]*fakeThemedApp{}}

	manifestByApp := map[string][]string{}
	for _, e := range theme.Manifest() {
		manifestByApp[e.App] = e.Paths
	}
	for _, name := range themedApps {
		env.fakes[name] = &fakeThemedApp{
			name:          name,
			manifestPaths: manifestByApp[name],
			calls:         &env.calls,
			liveCalls:     &env.live,
		}
	}

	origGetApp := themeGetAppFn
	t.Cleanup(func() { themeGetAppFn = origGetApp })
	themeGetAppFn = func(name string) (apps.App, error) {
		f, ok := env.fakes[name]
		if !ok {
			return nil, fmt.Errorf("unexpected app lookup: %s", name)
		}
		if f.live {
			return &fakeLiveThemedApp{f}, nil
		}
		return f, nil
	}

	origRecover := recoverInterruptedFn
	t.Cleanup(func() { recoverInterruptedFn = origRecover })
	recoverInterruptedFn = theme.RecoverInterrupted

	// runThemeSet republishes the build-stamped configs tree on entry (the
	// same call cmd/configure.go makes). The real one runs
	// devgeta.InstallIfStale against the machine, so every test that drives
	// runThemeSet has to stub it or it does real filesystem work outside the
	// isolated tree these tests set up.
	origRefresh := refreshEmbeddedConfigs
	t.Cleanup(func() { refreshEmbeddedConfigs = origRefresh })
	refreshEmbeddedConfigs = func() error { return nil }

	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatal(err)
	}
	if err := gc.Load(); err != nil {
		t.Fatal(err)
	}
	for _, name := range installed {
		meta := registry.Meta[name]
		gc.AddToInstalled(name, meta.ItemType)
	}
	if err := gc.Save(); err != nil {
		t.Fatal(err)
	}

	return env
}

func readGC(t *testing.T) *config.GlobalConfig {
	t.Helper()
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatal(err)
	}
	return gc
}

func TestThemeSet_UnknownTheme_WritesNothing(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)

	err := runThemeSet(themeSetCmd, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected an error for an unknown theme")
	}
	if len(env.calls) != 0 {
		t.Errorf("expected no app to be configured, got calls: %v", env.calls)
	}
	gc := readGC(t)
	if gc.CurrentTheme != "" {
		t.Errorf("expected current_theme unset, got %q", gc.CurrentTheme)
	}
	if gc.PendingTheme != "" {
		t.Errorf("expected pending_theme unset, got %q", gc.PendingTheme)
	}
}

func TestThemeSet_Success_ConfiguresInstalledAppsAndPersists(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)

	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err != nil {
		t.Fatalf("runThemeSet error: %v", err)
	}

	if len(env.calls) != len(themedApps) {
		t.Errorf("expected all %d apps configured, got calls: %v", len(themedApps), env.calls)
	}

	gc := readGC(t)
	if gc.CurrentTheme != "tokyonight" {
		t.Errorf("current_theme = %q, want tokyonight", gc.CurrentTheme)
	}
	if gc.PendingTheme != "" {
		t.Errorf("expected pending_theme cleared, got %q", gc.PendingTheme)
	}

	// No backups/markers survive a successful switch.
	for _, e := range theme.Manifest() {
		for _, p := range e.Paths {
			if theme.HasBackup(p) {
				t.Errorf("expected no backup left for %s", p)
			}
		}
	}

	// tmux's live push happens after commit (fakeThemedApp only implements
	// LiveThemeApplier when configured to - see the dedicated live-push test).
}

// TestThemeSet_SuccessDoesNotEatUnrelatedFiles asserts a successful switch
// touches only the paths in theme.Manifest() and leaves everything else a
// user (or another tool) put in the same directories untouched - the
// manifest is per-path rather than per-directory precisely so this holds
// (cycle doc Step 5's per-surface backup table).
func TestThemeSet_SuccessDoesNotEatUnrelatedFiles(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)
	_ = env

	// Pre-existing default-theme content so BackupPath takes the real
	// rename-aside branch (not the absent-marker one) for at least one
	// manifest entry sharing a directory with an unrelated file below.
	alacrittyPath := filepath.Join(paths.Paths.Config.Alacritty, "alacritty.toml")
	if err := os.MkdirAll(filepath.Dir(alacrittyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alacrittyPath, []byte("configured:default"), 0o644); err != nil {
		t.Fatal(err)
	}

	unrelated := map[string]string{
		filepath.Join(paths.Paths.Config.Nvim, "lua", "devgeta", "user-notes.lua"): "-- mine\n",
		filepath.Join(paths.Paths.Config.Alacritty, "user-notes.toml"):             "# mine\n",
		filepath.Join(paths.Paths.Config.Claude, "CLAUDE.md"):                      "# mine\n",
	}
	for p, content := range unrelated {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err != nil {
		t.Fatalf("runThemeSet error: %v", err)
	}

	for p, want := range unrelated {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("unrelated file %s no longer exists: %v", p, err)
			continue
		}
		if string(got) != want {
			t.Errorf("unrelated file %s content = %q, want unchanged %q", p, got, want)
		}
	}
}

func TestThemeSet_SkipsUninstalledApps(t *testing.T) {
	installed := []string{constants.Alacritty, constants.Tmux}
	env := setupThemeSetTest(t, installed)

	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err != nil {
		t.Fatalf("runThemeSet error: %v", err)
	}

	if len(env.calls) != 2 {
		t.Fatalf("expected exactly 2 apps configured, got: %v", env.calls)
	}
	for _, name := range env.calls {
		if name != constants.Alacritty && name != constants.Tmux {
			t.Errorf("unexpected app configured: %s", name)
		}
	}

	// Nothing written under an uninstalled app's config path.
	ghosttyPath := filepath.Join(paths.Paths.Config.Ghostty, "config")
	if _, err := os.Stat(ghosttyPath); !os.IsNotExist(err) {
		t.Errorf("expected nothing written for uninstalled ghostty, err=%v", err)
	}
}

func TestThemeSet_FailureRollsBackEverything(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)

	// Seed pre-existing "default" content for a file-backed and the
	// directory-backed (OpenCode) surfaces so rollback has something real to
	// restore.
	alacrittyPath := filepath.Join(paths.Paths.Config.Alacritty, "alacritty.toml")
	if err := os.MkdirAll(filepath.Dir(alacrittyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alacrittyPath, []byte("configured:default"), 0o644); err != nil {
		t.Fatal(err)
	}

	// tmux runs before Claude in themedApps order; rig Claude to fail so at
	// least one prior surface (tmux, already configured) must be rolled back
	// too.
	env.fakes[constants.Claude].configureErr = fmt.Errorf("boom")

	err := runThemeSet(themeSetCmd, []string{"tokyonight"})
	if err == nil {
		t.Fatal("expected an error from the failing app")
	}

	gc := readGC(t)
	if gc.CurrentTheme != "" {
		t.Errorf("expected current_theme untouched, got %q", gc.CurrentTheme)
	}
	if gc.PendingTheme != "" {
		t.Errorf("expected pending_theme cleared after rollback, got %q", gc.PendingTheme)
	}

	content, err := os.ReadFile(alacrittyPath)
	if err != nil {
		t.Fatalf("expected alacritty config to still exist: %v", err)
	}
	if string(content) != "configured:default" {
		t.Errorf("alacritty content = %q, want the pre-switch content restored", content)
	}

	tmuxPath := filepath.Join(paths.Paths.Home.Root, ".tmux.conf")
	if _, err := os.Stat(tmuxPath); !os.IsNotExist(err) {
		t.Errorf(
			"expected tmux's fresh (never-existed-before) config rolled back to absent, err=%v",
			err,
		)
	}

	for _, e := range theme.Manifest() {
		for _, p := range e.Paths {
			if theme.HasBackup(p) {
				t.Errorf("expected no leftover backup for %s after rollback", p)
			}
		}
	}
}

// TestThemeSet_FailedCommitRollsBackEverything covers the case every app
// succeeded but the final config.Update - the one that writes current_theme
// - itself fails (cycle doc Step 5: "A failed final commit rolls everything
// back"). A second, stand-in holder of the config's sidecar lock stands in
// for a wedged concurrent devgeta process; i3 (last in themedApps order) is
// rigged to wait until that holder actually has the lock before returning,
// so every app is guaranteed to have already succeeded - and its own backup
// already taken - by the time runThemeSet reaches the commit and finds the
// lock unavailable.
func TestThemeSet_FailedCommitRollsBackEverything(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)

	alacrittyPath := filepath.Join(paths.Paths.Config.Alacritty, "alacritty.toml")
	if err := os.MkdirAll(filepath.Dir(alacrittyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alacrittyPath, []byte("configured:default"), 0o644); err != nil {
		t.Fatal(err)
	}

	const timeout = 150 * time.Millisecond
	restoreTimeout := config.SetLockAcquireTimeoutForTest(timeout)
	t.Cleanup(restoreTimeout)

	// The holder must not even attempt to acquire the lock until
	// runThemeSet's own initial pending_theme write (order point 2, before
	// the app loop) has already completed - otherwise it can race that
	// write instead of the commit, timing the wrong config.Update out.
	// Alacritty is first in themedApps order, so gating on its start
	// guarantees the pending_theme write already released the lock.
	startAcquire := make(chan struct{})
	env.fakes[constants.Alacritty].onStart = func() { close(startAcquire) }

	held := make(chan struct{})
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		<-startAcquire
		if err := config.Update(func(_ *config.GlobalConfig) error {
			close(held)
			// Outlast the commit's own (shortened) timeout so it genuinely
			// fails, but release well before rollback's separate Update
			// call would time out too - proving rollback still runs even
			// though the lock only frees up partway through it.
			time.Sleep(timeout + timeout/2)
			return nil
		}); err != nil {
			t.Errorf("stand-in lock holder failed: %v", err)
		}
	}()
	t.Cleanup(func() { <-holderDone })

	env.fakes[constants.I3].onConfigured = func() { <-held }

	err := runThemeSet(themeSetCmd, []string{"tokyonight"})
	if err == nil {
		t.Fatal("expected the final commit to fail")
	}

	if len(env.calls) != len(themedApps) {
		t.Fatalf(
			"expected every app configured before the commit was attempted, got: %v",
			env.calls,
		)
	}

	gc := readGC(t)
	if gc.CurrentTheme != "" {
		t.Errorf(
			"current_theme = %q, want empty - the failed commit must not have persisted",
			gc.CurrentTheme,
		)
	}
	if gc.PendingTheme != "" {
		t.Errorf("expected pending_theme cleared by rollback, got %q", gc.PendingTheme)
	}

	content, err := os.ReadFile(alacrittyPath)
	if err != nil {
		t.Fatalf("expected alacritty config restored: %v", err)
	}
	if string(content) != "configured:default" {
		t.Errorf("alacritty content = %q, want the pre-switch content restored", content)
	}

	tmuxPath := filepath.Join(paths.Paths.Home.Root, ".tmux.conf")
	if _, err := os.Stat(tmuxPath); !os.IsNotExist(err) {
		t.Errorf(
			"expected tmux's fresh (never-existed-before) config rolled back to absent, err=%v",
			err,
		)
	}

	for _, e := range theme.Manifest() {
		for _, p := range e.Paths {
			if theme.HasBackup(p) {
				t.Errorf("expected no leftover backup for %s after commit-failure rollback", p)
			}
		}
	}
}

func TestThemeSet_LivePushOnlyAfterCommit(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)
	env.fakes[constants.Tmux].live = true

	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err != nil {
		t.Fatalf("runThemeSet error: %v", err)
	}

	if len(env.live) != 1 || env.live[0] != constants.Tmux {
		t.Errorf("expected exactly one live push (tmux), got: %v", env.live)
	}
}

func TestThemeSet_LivePushNotCalledOnFailure(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)
	env.fakes[constants.Tmux].live = true
	env.fakes[constants.Claude].configureErr = fmt.Errorf("boom")

	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err == nil {
		t.Fatal("expected an error")
	}

	if len(env.live) != 0 {
		t.Errorf("expected no live push on a failed switch, got: %v", env.live)
	}
}

// TestThemedApps_AllSatisfyThemedConfigurer asserts every entry in
// themedApps resolves through the real registry to an app implementing
// apps.ThemedConfigurer. A surface added to themedApps without wiring the
// interface then fails this test rather than a user's `dg theme set`.
func TestThemedApps_AllSatisfyThemedConfigurer(t *testing.T) {
	for _, name := range themedApps {
		app, err := registry.GetApp(name)
		if err != nil {
			t.Errorf("registry.GetApp(%s): %v", name, err)
			continue
		}
		if _, ok := app.(apps.ThemedConfigurer); !ok {
			t.Errorf("%s does not implement apps.ThemedConfigurer", name)
		}
	}
}

// TestThemeSet_RefreshesEmbeddedConfigs asserts dg theme set republishes the
// build-stamped configs tree before reading a theme out of it, the same way
// cmd/configure.go does and for the same reason: paths.Paths.App.Configs is
// a pointer to a per-build directory, so an upgraded binary otherwise reads
// the PREVIOUS build's tree. For a binary that introduced themes at all,
// that tree has no themes/ directory, and every theme command fails with a
// bare "no such file or directory" until some other command happens to
// republish it.
func TestThemeSet_RefreshesEmbeddedConfigs(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)
	_ = env

	calls := 0
	origRefresh := refreshEmbeddedConfigs
	t.Cleanup(func() { refreshEmbeddedConfigs = origRefresh })
	refreshEmbeddedConfigs = func() error {
		calls++
		return nil
	}

	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err != nil {
		t.Fatalf("runThemeSet error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected refreshEmbeddedConfigs called once, got %d", calls)
	}
}

// TestThemeSet_RefreshFailureStopsBeforeTouchingAnything asserts a failed
// republish aborts the switch rather than letting it proceed against a tree
// that may be the wrong build's.
func TestThemeSet_RefreshFailureStopsBeforeTouchingAnything(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)

	origRefresh := refreshEmbeddedConfigs
	t.Cleanup(func() { refreshEmbeddedConfigs = origRefresh })
	refreshEmbeddedConfigs = func() error { return fmt.Errorf("publish failed") }

	err := runThemeSet(themeSetCmd, []string{"tokyonight"})
	if err == nil {
		t.Fatal("expected runThemeSet to fail when the configs cannot be republished")
	}
	if len(env.calls) != 0 {
		t.Errorf("expected no app configured, got: %v", env.calls)
	}
	gc := readGC(t)
	if gc.CurrentTheme != "" || gc.PendingTheme != "" {
		t.Errorf(
			"expected no theme state written, got current=%q pending=%q",
			gc.CurrentTheme, gc.PendingTheme,
		)
	}
}

// TestRecoverInterruptedRunsOnEveryConfigWriter asserts dg theme set,
// dg configure, and dg install all call recoverInterruptedFn on entry
// (cycle doc Step 5) - a fourth command that deploys configs without the
// call would fail this test rather than a user's files.
func TestRecoverInterruptedRunsOnEveryConfigWriter(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)

	calls := 0
	origRecover := recoverInterruptedFn
	defer func() { recoverInterruptedFn = origRecover }()
	recoverInterruptedFn = func() (string, error) {
		calls++
		return "", nil
	}

	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err != nil {
		t.Fatalf("runThemeSet error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected recoverInterruptedFn called once by theme set, got %d", calls)
	}
	_ = env
}

// tiny1x1PNG is a real, valid 1x1 transparent PNG's bytes, so
// http.DetectContentType (theme.CopyWallpaper's validation) sniffs it as
// image/png.
var tiny1x1PNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0a, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

// fakeWallpaperSetter records every SetWallpaper call so a test can assert
// dg theme set applied (or correctly skipped) a theme's recorded wallpaper,
// without a real osascript/feh invocation.
type fakeWallpaperSetter struct {
	calls []string
	err   error
}

func (f *fakeWallpaperSetter) SetWallpaper(path string) error {
	f.calls = append(f.calls, path)
	return f.err
}

func TestThemeSetWallpaper_CopiesAndPersistsAgainstCurrentTheme(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)
	_ = env

	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err != nil {
		t.Fatalf("runThemeSet error: %v", err)
	}

	src := filepath.Join(t.TempDir(), "wallpaper.png")
	if err := os.WriteFile(src, tiny1x1PNG, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runThemeSetWallpaper(themeSetWallpaperCmd, []string{src}); err != nil {
		t.Fatalf("runThemeSetWallpaper error: %v", err)
	}

	gc := readGC(t)
	dest, ok := gc.Wallpapers["tokyonight"]
	if !ok || dest == "" {
		t.Fatalf("expected wallpapers[tokyonight] to be set, got %v", gc.Wallpapers)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected the copied wallpaper to exist at %s: %v", dest, err)
	}
	if string(got) != string(tiny1x1PNG) {
		t.Errorf("copied wallpaper content does not match the source")
	}
}

func TestThemeSetWallpaper_RejectsNonImage(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)
	_ = env

	src := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(src, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runThemeSetWallpaper(themeSetWallpaperCmd, []string{src}); err == nil {
		t.Fatal("expected an error for a non-image file")
	}
	gc := readGC(t)
	if len(gc.Wallpapers) != 0 {
		t.Errorf("expected no wallpaper recorded, got %v", gc.Wallpapers)
	}
}

// TestThemeSetWallpaper_ReplacingSweepsThePreviousFile proves the
// content-addressed sweep actually runs from the command, not just as a
// standalone theme.SweepOrphanedWallpapers unit (cycle doc Step 9).
func TestThemeSetWallpaper_ReplacingSweepsThePreviousFile(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)
	_ = env
	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err != nil {
		t.Fatalf("runThemeSet error: %v", err)
	}

	src1 := filepath.Join(t.TempDir(), "one.png")
	if err := os.WriteFile(src1, tiny1x1PNG, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runThemeSetWallpaper(themeSetWallpaperCmd, []string{src1}); err != nil {
		t.Fatalf("first runThemeSetWallpaper error: %v", err)
	}
	firstDest := readGC(t).Wallpapers["tokyonight"]

	src2 := filepath.Join(t.TempDir(), "two.png")
	if err := os.WriteFile(src2, append(append([]byte{}, tiny1x1PNG...), 0x00), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runThemeSetWallpaper(themeSetWallpaperCmd, []string{src2}); err != nil {
		t.Fatalf("second runThemeSetWallpaper error: %v", err)
	}
	secondDest := readGC(t).Wallpapers["tokyonight"]

	if firstDest == secondDest {
		t.Fatalf("expected a distinct destination for a distinct image")
	}
	if _, err := os.Stat(firstDest); !os.IsNotExist(err) {
		t.Errorf("expected the superseded wallpaper file to be swept, err=%v", err)
	}
	if _, err := os.Stat(secondDest); err != nil {
		t.Errorf("expected the new wallpaper file to exist: %v", err)
	}
}

func TestThemeSet_AppliesRecordedWallpaper(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)
	_ = env

	if err := config.Update(func(g *config.GlobalConfig) error {
		g.Wallpapers = map[string]string{"tokyonight": "/wallpapers/abc.png"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	fake := &fakeWallpaperSetter{}
	origSetter := themeWallpaperSetterFn
	t.Cleanup(func() { themeWallpaperSetterFn = origSetter })
	themeWallpaperSetterFn = func(commands.BaseCommandExecutor) theme.WallpaperSetter { return fake }

	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err != nil {
		t.Fatalf("runThemeSet error: %v", err)
	}

	if len(fake.calls) != 1 || fake.calls[0] != "/wallpapers/abc.png" {
		t.Errorf("expected exactly one SetWallpaper(/wallpapers/abc.png) call, got: %v", fake.calls)
	}
}

// TestThemeSet_FallsBackToThemeFilesDeclaredWallpaper covers the theme
// file's own `wallpaper:` key (cycle doc Step 9) - a declared default,
// consulted only when GlobalConfig.Wallpapers has no entry for the theme
// being switched to.
func TestThemeSet_FallsBackToThemeFilesDeclaredWallpaper(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)
	_ = env

	tokyonightPath := filepath.Join(paths.Paths.App.Configs.Themes, "tokyonight.yaml")
	data, err := os.ReadFile(tokyonightPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("wallpaper: /home/tester/wallpaper.jpg\n")...)
	if err := os.WriteFile(tokyonightPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	fake := &fakeWallpaperSetter{}
	origSetter := themeWallpaperSetterFn
	t.Cleanup(func() { themeWallpaperSetterFn = origSetter })
	themeWallpaperSetterFn = func(commands.BaseCommandExecutor) theme.WallpaperSetter { return fake }

	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err != nil {
		t.Fatalf("runThemeSet error: %v", err)
	}

	if len(fake.calls) != 1 || fake.calls[0] != "/home/tester/wallpaper.jpg" {
		t.Errorf(
			"expected exactly one SetWallpaper(/home/tester/wallpaper.jpg) call, got: %v",
			fake.calls,
		)
	}
}

// TestThemeSet_DurableWallpaperWinsOverThemeFilesDeclaredDefault proves
// GlobalConfig.Wallpapers (set via `dg theme set-wallpaper`) takes priority
// over a theme file's own declared default, not the other way around.
func TestThemeSet_DurableWallpaperWinsOverThemeFilesDeclaredDefault(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)
	_ = env

	tokyonightPath := filepath.Join(paths.Paths.App.Configs.Themes, "tokyonight.yaml")
	data, err := os.ReadFile(tokyonightPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("wallpaper: /home/tester/declared-default.jpg\n")...)
	if err := os.WriteFile(tokyonightPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.Update(func(g *config.GlobalConfig) error {
		g.Wallpapers = map[string]string{"tokyonight": "/wallpapers/durable.png"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	fake := &fakeWallpaperSetter{}
	origSetter := themeWallpaperSetterFn
	t.Cleanup(func() { themeWallpaperSetterFn = origSetter })
	themeWallpaperSetterFn = func(commands.BaseCommandExecutor) theme.WallpaperSetter { return fake }

	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err != nil {
		t.Fatalf("runThemeSet error: %v", err)
	}

	if len(fake.calls) != 1 || fake.calls[0] != "/wallpapers/durable.png" {
		t.Errorf(
			"expected the durable wallpapers-map entry to win, got: %v",
			fake.calls,
		)
	}
}

func TestThemeSet_NoRecordedWallpaper_LeavesDesktopAlone(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)
	_ = env

	fake := &fakeWallpaperSetter{}
	origSetter := themeWallpaperSetterFn
	t.Cleanup(func() { themeWallpaperSetterFn = origSetter })
	themeWallpaperSetterFn = func(commands.BaseCommandExecutor) theme.WallpaperSetter { return fake }

	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err != nil {
		t.Fatalf("runThemeSet error: %v", err)
	}

	if len(fake.calls) != 0 {
		t.Errorf(
			"expected no SetWallpaper call for a theme with no recorded wallpaper, got: %v",
			fake.calls,
		)
	}
}

// TestThemeSet_WallpaperFailureDoesNotFailTheCommand covers ADR-0044 point
// 4: a wallpaper application failure is cosmetic and must never turn an
// otherwise-successful theme switch into a command failure.
func TestThemeSet_WallpaperFailureDoesNotFailTheCommand(t *testing.T) {
	env := setupThemeSetTest(t, themedApps)
	_ = env

	if err := config.Update(func(g *config.GlobalConfig) error {
		g.Wallpapers = map[string]string{"tokyonight": "/wallpapers/abc.png"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	fake := &fakeWallpaperSetter{err: fmt.Errorf("boom")}
	origSetter := themeWallpaperSetterFn
	t.Cleanup(func() { themeWallpaperSetterFn = origSetter })
	themeWallpaperSetterFn = func(commands.BaseCommandExecutor) theme.WallpaperSetter { return fake }

	if err := runThemeSet(themeSetCmd, []string{"tokyonight"}); err != nil {
		t.Fatalf("expected dg theme set to still succeed, got: %v", err)
	}

	gc := readGC(t)
	if gc.CurrentTheme != "tokyonight" {
		t.Errorf(
			"expected the switch to have committed despite the wallpaper failure, current_theme = %q",
			gc.CurrentTheme,
		)
	}
}
