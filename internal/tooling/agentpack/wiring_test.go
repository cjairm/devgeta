package agentpack

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/paths"
)

func TestPackBuilder_FetchesBuildsWithBothLayersAndReturnsTheTree(t *testing.T) {
	root := t.TempDir()
	removeAllWritable(t, root)
	// The fetched clone is the fixture upstream; the fake git writes it.
	f := newGitFixture(t)
	f.cloneFrom = "upstream"
	shipped, user := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(shipped, "fixture", "skills", "alpha", "SKILL.md"),
		skillMD("alpha", "Use when you need the alpha behaviour", "shipped alpha"), 0o644)
	writeFile(t, filepath.Join(user, "fixture", "skills", "alpha", "SKILL.md"),
		skillMD("alpha", "Use when you need the alpha behaviour", "user alpha"), 0o644)
	pb := &PackBuilder{
		Fetcher:         &Fetcher{Git: f.git, Root: root},
		Builder:         &Builder{Root: root},
		ShippedLayerDir: shipped,
		UserLayerDir:    user,
	}

	tree, res, err := pb.Prepare(fixtureManifest(), []string{"gamma"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree.Dir != filepath.Join(root, "fixture") || res.Dir != tree.Dir || tree.Pack.Name != "fixture" {
		t.Errorf("unexpected tree %+v / %+v", tree, res)
	}
	if got := readFile(t, filepath.Join(tree.Dir, "skills", "alpha", "SKILL.md")); !strings.Contains(got, "user alpha") {
		t.Errorf("the user's layer must win over devgeta's, got %q", got)
	}
	if exists(filepath.Join(tree.Dir, "skills", "gamma")) {
		t.Error("the exclusion should have been applied")
	}
}

func TestPackBuilder_RemoveDeletesTheBuiltTreeOnly(t *testing.T) {
	root := t.TempDir()
	removeAllWritable(t, root)
	writeFile(t, filepath.Join(root, "superpowers", "skills", "a", "SKILL.md"), "x", 0o644)
	writeFile(t, filepath.Join(root, ".src", "superpowers@abc", "README"), "clone", 0o644)
	pb := &PackBuilder{Builder: &Builder{Root: root}}

	if err := pb.Remove("superpowers"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists(filepath.Join(root, "superpowers")) {
		t.Error("the built tree should be gone")
	}
	if !exists(filepath.Join(root, ".src", "superpowers@abc", "README")) {
		t.Error("the clone cache must stay so a reinstall needs no network (ADR-0070 §4)")
	}
	if err := pb.Remove("superpowers"); err != nil {
		t.Errorf("removing a tree that is already gone is not an error: %v", err)
	}
}

func TestPackBuilder_RemoveRefusesNamesThatEscapeTheRoot(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(filepath.Dir(root), "victim-"+filepath.Base(root))
	if err := os.MkdirAll(victim, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(victim) })
	pb := &PackBuilder{Builder: &Builder{Root: root}}

	for _, name := range []string{"../victim-" + filepath.Base(root), "", ".", "a/b"} {
		if err := pb.Remove(name); err == nil {
			t.Errorf("Remove(%q) must be refused", name)
		}
	}
	if !exists(victim) {
		t.Error("a name that climbs out of the root must never delete anything outside it")
	}
}

func isolateConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	old := paths.Paths.Config.Root
	t.Cleanup(func() {
		paths.Paths.Config.Root = old
		config.ResetGlobalConfigCacheForTest()
	})
	paths.Paths.Config.Root = dir
	config.ResetGlobalConfigCacheForTest()
}

func TestConfigStore_UpdateThenLoadRoundTrips(t *testing.T) {
	isolateConfig(t)
	store := ConfigStore{}

	err := store.Update(func(packs map[string]config.AgentPackState) error {
		packs["superpowers"] = config.AgentPackState{
			Version: "v7.0.2",
			Coders:  map[string]config.AgentPackCoderState{"pi": {PiPackage: "/p"}},
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	config.ResetGlobalConfigCacheForTest()
	got, err := store.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["superpowers"].Version != "v7.0.2" || got["superpowers"].Coders["pi"].PiPackage != "/p" {
		t.Errorf("unexpected state %+v", got)
	}
}

func TestConfigStore_LoadWithNoConfigFileIsEmpty(t *testing.T) {
	isolateConfig(t)

	got, err := ConfigStore{}.Load()
	if err != nil {
		t.Fatalf("a machine that never used packs has no config yet: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no packs, got %v", got)
	}
}

func TestConfigStore_DeletingTheLastPackDropsTheKey(t *testing.T) {
	isolateConfig(t)
	store := ConfigStore{}
	add := func(packs map[string]config.AgentPackState) error {
		packs["ponytail"] = config.AgentPackState{Version: "v5.3.3"}
		return nil
	}
	if err := store.Update(add); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(packs map[string]config.AgentPackState) error {
		delete(packs, "ponytail")
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(config.GlobalConfigFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "agent_packs") {
		t.Errorf("an emptied agent_packs must not linger as a key:\n%s", raw)
	}
}

func TestConfigStore_UpdateKeepsOtherConfigAndReportsFnErrors(t *testing.T) {
	isolateConfig(t)
	if err := config.Update(func(gc *config.GlobalConfig) error {
		gc.CurrentTheme = "gruvbox"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	err := ConfigStore{}.Update(func(map[string]config.AgentPackState) error {
		return os.ErrInvalid
	})
	if err == nil {
		t.Fatal("an error from fn must come back")
	}
	if err := (ConfigStore{}).Update(func(p map[string]config.AgentPackState) error {
		p["x"] = config.AgentPackState{Version: "1"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	gc := &config.GlobalConfig{}
	config.ResetGlobalConfigCacheForTest()
	if err := gc.Load(); err != nil {
		t.Fatal(err)
	}
	if gc.CurrentTheme != "gruvbox" {
		t.Errorf("the rest of the config must be untouched, theme is %q", gc.CurrentTheme)
	}
}

func TestPackBuilder_EmbeddedPackIsBuiltFromTheShippedFolderWithoutFetching(t *testing.T) {
	root := t.TempDir()
	removeAllWritable(t, root)
	shipped, user := t.TempDir(), t.TempDir()
	copyInto(t, filepath.Join("testdata", "devgeta"), filepath.Join(shipped, "devgeta"))
	writeFile(t, filepath.Join(user, "devgeta", "DEVGETA.md"), "# Mine\n", 0o644)
	pb := &PackBuilder{
		// A nil Fetcher proves nothing is fetched: it would panic.
		Builder:         &Builder{Root: root},
		ShippedLayerDir: shipped,
		UserLayerDir:    user,
	}

	tree, res, err := pb.Prepare(embeddedManifest(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree.Dir != filepath.Join(root, "devgeta") || res.Dir != tree.Dir || !tree.Pack.Embedded() {
		t.Errorf("unexpected tree %+v", tree)
	}
	if got := readFile(t, filepath.Join(tree.Dir, "DEVGETA.md")); got != "# Mine\n" {
		t.Errorf("the user's layer wins, got %q", got)
	}
	if !exists(filepath.Join(tree.Dir, "extensions", "devgeta-instructions.js")) {
		t.Error("the Pi package files must come through")
	}
}

func newCheckBuilder(t *testing.T) (*PackBuilder, *gitFixture, string) {
	t.Helper()
	root := t.TempDir()
	removeAllWritable(t, root)
	f := newGitFixture(t)
	f.cloneFrom = "upstream"
	return &PackBuilder{
		Fetcher:         &Fetcher{Git: f.git, Root: root},
		Builder:         &Builder{Root: root},
		ShippedLayerDir: t.TempDir(),
		UserLayerDir:    t.TempDir(),
	}, f, root
}

func clonedTags(f *gitFixture) []string {
	var tags []string
	for _, c := range f.base.ExecCommandCalls {
		for i, a := range c.Args {
			if a == "--branch" && i+1 < len(c.Args) {
				tags = append(tags, c.Args[i+1])
			}
		}
	}
	return tags
}

func TestPackBuilder_CheckBuildsThePinAndShowsTheTreeToTheInspector(t *testing.T) {
	pb, f, root := newCheckBuilder(t)
	var sawSkill bool

	res, sha, err := pb.Check(fixtureManifest(), "", []string{"gamma"}, func(dir string) error {
		sawSkill = exists(filepath.Join(dir, "skills", "alpha", "SKILL.md")) && !exists(filepath.Join(dir, "skills", "gamma"))
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha != goodSHA {
		t.Errorf("checking the pin reports the pinned commit, got %s", sha)
	}
	if !sawSkill {
		t.Error("the inspector must see the tree with the exclusion applied")
	}
	if got := clonedTags(f); len(got) != 1 || got[0] != "v1.0.0" {
		t.Errorf("expected a clone of the pinned tag, got %v", got)
	}
	if exists(filepath.Join(root, "fixture")) {
		t.Error("check must not create the pack's tree")
	}
	if res.ReplacedHashes == nil {
		t.Error("the replaced hashes are part of the result")
	}
}

func TestPackBuilder_CheckWithARefTriesThatTagAndReportsItsCommit(t *testing.T) {
	pb, f, root := newCheckBuilder(t)
	newer := strings.Repeat("f", 40)
	f.headSHA = newer

	_, sha, err := pb.Check(fixtureManifest(), "v1.1.0", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sha != newer {
		t.Errorf("expected the commit the ref resolved to, got %s", sha)
	}
	if got := clonedTags(f); len(got) != 1 || got[0] != "v1.1.0" {
		t.Errorf("expected a clone of the ref, got %v", got)
	}
	if !exists(filepath.Join(root, ".src", "fixture@"+newer)) {
		t.Error("the clone is cached by commit like any other")
	}
}

func TestPackBuilder_CheckOfTheEmbeddedPackBuildsItsFolder(t *testing.T) {
	root := t.TempDir()
	shipped := t.TempDir()
	copyInto(t, filepath.Join("testdata", "devgeta"), filepath.Join(shipped, "devgeta"))
	pb := &PackBuilder{Builder: &Builder{Root: root}, ShippedLayerDir: shipped, UserLayerDir: t.TempDir()}

	var sawFile bool
	_, sha, err := pb.Check(embeddedManifest(), "", nil, func(dir string) error {
		sawFile = exists(filepath.Join(dir, "DEVGETA.md"))
		return nil
	})
	if err != nil || !sawFile || sha != "" {
		t.Fatalf("expected the bundled pack to build with no commit; err=%v saw=%v sha=%q", err, sawFile, sha)
	}
}

func TestPackBuilder_CheckReportsABuildFailure(t *testing.T) {
	pb, _, _ := newCheckBuilder(t)

	_, _, err := pb.Check(fixtureManifest(), "", []string{"alpha"}, nil) // beta names alpha
	if err == nil || !strings.Contains(err.Error(), "beta") {
		t.Fatalf("expected the build check to fail naming beta, got %v", err)
	}
}

func TestPackBuilder_UpstreamPacksGetTheShippedLayerSeparately(t *testing.T) {
	root := t.TempDir()
	removeAllWritable(t, root)
	f := newGitFixture(t)
	f.cloneFrom = "upstream"
	pb := &PackBuilder{
		Fetcher:         &Fetcher{Git: f.git, Root: root},
		Builder:         &Builder{Root: root},
		ShippedLayerDir: "/shipped",
		UserLayerDir:    "/user",
	}

	opts, _, err := pb.options(fixtureManifest(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.ShippedLayer != "/shipped/fixture" || !reflect.DeepEqual(opts.Layers, []string{"/user/fixture"}) {
		t.Errorf("shipped layer %q, user layers %v", opts.ShippedLayer, opts.Layers)
	}
}
