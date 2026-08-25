package config

import (
	"reflect"
	"testing"
	"time"
)

// newFullyPopulatedGlobalConfig returns a fresh *GlobalConfig with every
// field set to a non-zero value, including at least one element in every
// slice, one entry in the map, and a non-nil pointer - so
// TestCloneGlobalConfig_DeepCopiesEveryReachableField below has something to
// perturb in each reachable position. Called twice independently (never
// reused between src and want) so the two literals never share any backing
// storage to begin with.
func newFullyPopulatedGlobalConfig() *GlobalConfig {
	attach := true
	fixedTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return &GlobalConfig{
		AppPath:      "/app",
		ConfigPath:   "/config",
		CurrentFont:  "font",
		CurrentTheme: "theme",
		AlreadyInstalled: AlreadyInstalledConfig{
			Packages:      []string{"a"},
			DesktopApps:   []string{"b"},
			Fonts:         []string{"c"},
			Themes:        []string{"d"},
			TerminalTools: []string{"e"},
			DevLanguages:  []string{"f"},
			Databases:     []string{"g"},
		},
		Installed: InstalledConfig{
			Packages:      []string{"a1"},
			DesktopApps:   []string{"b1"},
			Fonts:         []string{"c1"},
			Themes:        []string{"d1"},
			TerminalTools: []string{"e1"},
			DevLanguages:  []string{"f1"},
			Databases:     []string{"g1"},
		},
		Shortcuts: map[string]string{"k": "v"},
		Shell: ShellFeatures{
			IsMac: true, Mise: true, Zoxide: true, ZshAutosuggestions: true,
			ZshSyntaxHighlighting: true, Powerlevel10k: true, ExtendedCapabilities: true,
			LazyGit: true, LazyDocker: true, Fzf: true, Neovim: true, Tmux: true,
			Eza: true, Bat: true, Opencode: true, Claude: true,
		},
		FailedInstallations: []FailedInstallation{
			{
				PackageName:  "p",
				Category:     "c",
				ErrorMessage: "e",
				FailedAt:     fixedTime,
				AttemptCount: 1,
			},
		},
		Worktree: WorktreeConfig{
			DefaultAI:         "claude",
			RecentRepos:       []RecentRepo{{Path: "/r", LastUsed: fixedTime}},
			SearchPaths:       []string{"/s"},
			ScanDepth:         4,
			DefaultLayout:     "layout",
			Location:          WorktreeLocationShared,
			AttachAfterCreate: &attach,
			NotifySound:       true,
		},
		Integrations: IntegrationsConfig{RtkClaudeHook: true},
		Review:       ReviewConfig{Reviewers: []string{"anthropic/x"}, Rounds: 3},
	}
}

// perturb walks v (which must be addressable) and mutates every reachable
// leaf: it flips bools, appends to strings, bumps ints, and adds an hour to
// any time.Time - recursing through pointers (non-nil only), structs,
// slices, and maps along the way. Applied to the SOURCE after cloning, this
// is what proves the clone is truly independent: if cloneGlobalConfig ever
// leaves a field aliased (a slice header copied instead of its backing
// array, a pointer copied instead of its pointee, a future field the clone
// forgets entirely), perturbing the source changes the clone too.
func perturb(v reflect.Value) {
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			perturb(v.Elem())
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			if v.CanSet() {
				v.Set(reflect.ValueOf(v.Interface().(time.Time).Add(time.Hour)))
			}
			return
		}
		for i := 0; i < v.NumField(); i++ {
			perturb(v.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			perturb(v.Index(i))
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			val := v.MapIndex(k)
			nv := reflect.New(val.Type()).Elem()
			nv.Set(val)
			perturb(nv)
			v.SetMapIndex(k, nv)
		}
	case reflect.String:
		if v.CanSet() {
			v.SetString(v.String() + "-mutated")
		}
	case reflect.Bool:
		if v.CanSet() {
			v.SetBool(!v.Bool())
		}
	case reflect.Int:
		if v.CanSet() {
			v.SetInt(v.Int() + 1)
		}
	}
}

// TestCloneGlobalConfig_DeepCopiesEveryReachableField is the structural
// guard ADR-0030 requires in place of a hand-maintained convention: it
// populates every field, clones, then mutates every reachable slice
// element, map entry, and pointee in the SOURCE by reflection, and asserts
// the clone is unaffected. A field added to GlobalConfig later that
// cloneGlobalConfig forgets to deep-copy would alias its source through the
// `dst := *src` shallow copy, and this test would catch it: perturbing the
// source would change the "clone" too, and the DeepEqual below would fail.
func TestCloneGlobalConfig_DeepCopiesEveryReachableField(t *testing.T) {
	src := newFullyPopulatedGlobalConfig()
	want := newFullyPopulatedGlobalConfig()

	clone := cloneGlobalConfig(src)

	perturb(reflect.ValueOf(src).Elem())

	// Sanity: prove perturb actually mutated src, so a no-op perturb (e.g.
	// from a Kind this function doesn't handle) can't make this test pass
	// trivially.
	if reflect.DeepEqual(src, want) {
		t.Fatal("perturb did not mutate src - this test is not exercising anything")
	}

	if !reflect.DeepEqual(clone, want) {
		t.Fatalf(
			"clone was affected by mutating the source after cloning "+
				"(or a field was added to GlobalConfig without extending cloneGlobalConfig):\nclone = %#v\nwant  = %#v",
			clone,
			want,
		)
	}
}

// TestCloneGlobalConfig_NilInputReturnsNil is a narrow guard for the one
// input every caller of cloneGlobalConfig must handle safely: the cache
// starts with no cached document at all.
func TestCloneGlobalConfig_NilInputReturnsNil(t *testing.T) {
	if got := cloneGlobalConfig(nil); got != nil {
		t.Fatalf("cloneGlobalConfig(nil) = %#v, want nil", got)
	}
}
