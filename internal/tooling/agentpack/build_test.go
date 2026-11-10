package agentpack

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// --- helpers ---------------------------------------------------------------

// copyFixture copies testdata/<name> into a fresh temp dir, keeping the
// executable bit, so a test can change it freely. It deliberately does not use
// the code under test.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	dst := t.TempDir()
	copyInto(t, filepath.Join("testdata", name), dst)
	return dst
}

// copyInto copies the tree at src into dst, keeping the executable bit.
func copyInto(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func skillMD(name, description, body string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n" + body + "\n"
}

func fixtureManifest() Manifest {
	return Manifest{
		Name: "fixture",
		Repo: "https://example.test/fixture.git",
		Tag:  "v1.0.0",
		SHA:  goodSHA,
	}
}

type buildEnv struct {
	t      *testing.T
	root   string
	source string
	b      *Builder
}

func newBuildEnv(t *testing.T) *buildEnv {
	t.Helper()
	root := t.TempDir()
	return &buildEnv{t: t, root: root, source: copyFixture(t, "upstream"), b: &Builder{Root: root}}
}

func (e *buildEnv) opts(exclude []string, layers ...string) BuildOptions {
	return BuildOptions{Manifest: fixtureManifest(), Source: e.source, Exclude: exclude, Layers: layers}
}

func (e *buildEnv) tree() string { return filepath.Join(e.root, "fixture") }

// layer writes an override layer: a folder laid out like the pack.
func (e *buildEnv) layer(files map[string]string) string {
	e.t.Helper()
	dir := e.t.TempDir()
	for rel, content := range files {
		writeFile(e.t, filepath.Join(dir, rel), content, 0o644)
	}
	return dir
}

// leftovers lists anything in Root other than the finished tree, such as a
// temp build dir a failed run forgot to delete.
func (e *buildEnv) leftovers() []string {
	e.t.Helper()
	entries, err := os.ReadDir(e.root)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, ent := range entries {
		if ent.Name() != "fixture" {
			out = append(out, ent.Name())
		}
	}
	return out
}

func sha256Of(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// --- copy ------------------------------------------------------------------

func TestBuild_CopiesUpstreamIntoTheTree(t *testing.T) {
	e := newBuildEnv(t)
	writeFile(t, filepath.Join(e.source, ".git", "HEAD"), "ref", 0o644)

	res, err := e.b.Build(e.opts(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Dir != e.tree() {
		t.Errorf("expected the tree at %s, got %s", e.tree(), res.Dir)
	}
	if got := readFile(t, filepath.Join(res.Dir, "skills", "alpha", "SKILL.md")); !strings.Contains(got, "Upstream alpha body") {
		t.Errorf("alpha was not copied: %q", got)
	}
	if exists(filepath.Join(res.Dir, ".git")) {
		t.Error("the clone's .git must not be copied into the built tree")
	}
	if left := e.leftovers(); len(left) != 0 {
		t.Errorf("unexpected leftovers in the pack root: %v", left)
	}
}

func TestBuild_KeepsTheExecutableBitAndMakesFilesWritable(t *testing.T) {
	e := newBuildEnv(t)
	// The cached clone is read-only; the built tree is a normal local copy.
	if err := makeReadOnly(e.source); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = removeTree(e.source) })

	res, err := e.b.Build(e.opts(nil))
	if err != nil {
		t.Fatalf("a read-only source must still build: %v", err)
	}
	for _, rel := range []string{"hooks/run-hook.cmd", "skills/gamma/helper.sh"} {
		info, err := os.Stat(filepath.Join(res.Dir, rel))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s lost its executable bit: %v", rel, info.Mode().Perm())
		}
	}
	info, err := os.Stat(filepath.Join(res.Dir, "skills", "alpha", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 == 0 {
		t.Errorf("a built file should be writable by its owner, got %v", info.Mode().Perm())
	}
	// And the source is exactly as it was.
	if !exists(filepath.Join(e.source, "skills", "alpha", "SKILL.md")) {
		t.Error("the source must be left alone")
	}
}

func TestBuild_Symlinks(t *testing.T) {
	t.Run("a link that stays inside the tree is kept as a link", func(t *testing.T) {
		e := newBuildEnv(t)
		if err := os.Symlink("alpha", filepath.Join(e.source, "skills", "alias")); err != nil {
			t.Fatal(err)
		}
		res, err := e.b.Build(e.opts(nil))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		target, err := os.Readlink(filepath.Join(res.Dir, "skills", "alias"))
		if err != nil || target != "alpha" {
			t.Errorf("expected a link to alpha, got %q (%v)", target, err)
		}
	})
	t.Run("a link that leaves the tree fails the build", func(t *testing.T) {
		e := newBuildEnv(t)
		if err := os.Symlink("../../../../etc/passwd", filepath.Join(e.source, "skills", "evil")); err != nil {
			t.Fatal(err)
		}
		_, err := e.b.Build(e.opts(nil))
		if err == nil || !strings.Contains(err.Error(), "evil") {
			t.Fatalf("expected an error naming the link, got %v", err)
		}
		if left := e.leftovers(); len(left) != 0 {
			t.Errorf("a failed build left %v", left)
		}
	})
	t.Run("an absolute link fails the build", func(t *testing.T) {
		e := newBuildEnv(t)
		if err := os.Symlink("/etc/passwd", filepath.Join(e.source, "abs")); err != nil {
			t.Fatal(err)
		}
		if _, err := e.b.Build(e.opts(nil)); err == nil {
			t.Fatal("expected an absolute symlink to fail the build")
		}
	})
}

// --- exclusion -------------------------------------------------------------

func TestBuild_ExcludeRemovesTheSkill(t *testing.T) {
	e := newBuildEnv(t)

	res, err := e.b.Build(e.opts([]string{"gamma"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists(filepath.Join(res.Dir, "skills", "gamma")) {
		t.Error("gamma should have been removed")
	}
	if !exists(filepath.Join(res.Dir, "skills", "alpha", "SKILL.md")) {
		t.Error("alpha should have been kept")
	}
	// The clone is never edited.
	if !exists(filepath.Join(e.source, "skills", "gamma", "SKILL.md")) {
		t.Error("the source clone must not be edited")
	}
}

func TestBuild_ExcludeOfAnUnknownSkillFails(t *testing.T) {
	e := newBuildEnv(t)

	_, err := e.b.Build(e.opts([]string{"nope"}))
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected an error naming the skill, got %v", err)
	}
	if exists(e.tree()) || len(e.leftovers()) != 0 {
		t.Errorf("a failed build must leave nothing: tree=%v leftovers=%v", exists(e.tree()), e.leftovers())
	}
}

func TestBuild_ExcludeNamesThatAreNotSkillFoldersFail(t *testing.T) {
	for _, name := range []string{"README.txt", "../hooks", "skills/alpha", ""} {
		e := newBuildEnv(t)
		if _, err := e.b.Build(e.opts([]string{name})); err == nil {
			t.Errorf("exclude %q should fail: it is not a skill folder", name)
		}
	}
}

func TestBuild_ExcludingASkillAKeptSkillNamesFails(t *testing.T) {
	e := newBuildEnv(t)

	// beta says "fixture:alpha"; excluding alpha would leave a dangling call.
	_, err := e.b.Build(e.opts([]string{"alpha"}))
	if err == nil {
		t.Fatal("expected the build to fail")
	}
	for _, want := range []string{"alpha", "beta"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to name %q, got: %v", want, err)
		}
	}
	if exists(e.tree()) || len(e.leftovers()) != 0 {
		t.Error("nothing should be left behind")
	}
}

func TestBuild_ExcludingASkillTogetherWithEveryOneThatNamesItWorks(t *testing.T) {
	e := newBuildEnv(t)

	if _, err := e.b.Build(e.opts([]string{"alpha", "beta"})); err != nil {
		t.Fatalf("excluding both ends of the reference should work: %v", err)
	}
}

func TestBuild_ExcludedNameInsideALongerNameIsNotAReference(t *testing.T) {
	e := newBuildEnv(t)
	// beta also contains "alpha-two", and gamma is excluded: only whole names
	// count, so a skill called "alpha" excluded elsewhere is the only real hit.
	writeFile(t, filepath.Join(e.source, "skills", "alpha-two", "SKILL.md"),
		skillMD("alpha-two", "Use for alpha-two", "x"), 0o644)
	writeFile(t, filepath.Join(e.source, "skills", "beta", "SKILL.md"),
		skillMD("beta", "Use when you need beta", "Only mentions alpha-two here."), 0o644)

	if _, err := e.b.Build(e.opts([]string{"alpha"})); err != nil {
		t.Fatalf("alpha-two is not a reference to alpha: %v", err)
	}
}

func TestMentionsSkill(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"use fixture:alpha first", true},
		{"use `alpha`.", true},
		{"alpha", true},
		{"(alpha)", true},
		{"alpha-two is different", false},
		{"two-alpha is different", false},
		{"alphabet", false},
		{"xalpha", false},
		{"nothing here", false},
	}
	for _, tc := range cases {
		if got := mentionsSkill(tc.text, "alpha"); got != tc.want {
			t.Errorf("mentionsSkill(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

// --- overrides -------------------------------------------------------------

func TestBuild_OverrideLayersApplyInOrderAndTheUserWins(t *testing.T) {
	e := newBuildEnv(t)
	devgetaLayer := e.layer(map[string]string{
		"skills/alpha/SKILL.md": skillMD("alpha", "Use when you need the alpha behaviour", "devgeta alpha"),
		"skills/beta/SKILL.md":  skillMD("beta", "Use when you need beta", "devgeta beta. Uses alpha."),
	})
	userLayer := e.layer(map[string]string{
		"skills/alpha/SKILL.md": skillMD("alpha", "Use when you need the alpha behaviour", "user alpha"),
	})

	res, err := e.b.Build(e.opts(nil, devgetaLayer, userLayer))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, filepath.Join(res.Dir, "skills", "alpha", "SKILL.md")); !strings.Contains(got, "user alpha") {
		t.Errorf("the user's layer should win, got %q", got)
	}
	if got := readFile(t, filepath.Join(res.Dir, "skills", "beta", "SKILL.md")); !strings.Contains(got, "devgeta beta") {
		t.Errorf("devgeta's layer should apply where the user has none, got %q", got)
	}
}

func TestBuild_MissingLayerDirectoriesAreSkipped(t *testing.T) {
	e := newBuildEnv(t)

	if _, err := e.b.Build(e.opts(nil, filepath.Join(t.TempDir(), "no-such-layer"))); err != nil {
		t.Fatalf("a layer that does not exist is just empty: %v", err)
	}
}

func TestBuild_PackManifestInALayerIsNotCopiedIntoTheTree(t *testing.T) {
	e := newBuildEnv(t)
	layer := e.layer(map[string]string{"pack.yaml": "name: fixture\n"})

	res, err := e.b.Build(e.opts(nil, layer))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists(filepath.Join(res.Dir, "pack.yaml")) {
		t.Error("pack.yaml is devgeta's pin, not part of the pack")
	}
}

func TestBuild_OverrideMayReplaceANonSkillFileAndAddAFileInASkillFolder(t *testing.T) {
	e := newBuildEnv(t)
	layer := e.layer(map[string]string{
		"hooks/hooks.json":       `{ "hooks": {"x": []} }`,
		"skills/gamma/extra.md":  "extra",
		"skills/gamma/helper.sh": "#!/bin/sh\necho override\n",
	})

	res, err := e.b.Build(e.opts(nil, layer))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, filepath.Join(res.Dir, "hooks", "hooks.json")); !strings.Contains(got, `"x"`) {
		t.Errorf("hooks.json was not replaced: %q", got)
	}
	if readFile(t, filepath.Join(res.Dir, "skills", "gamma", "extra.md")) != "extra" {
		t.Error("an extra file inside an existing skill folder should be added")
	}
}

func TestBuild_OverrideCannotAddANewSkill(t *testing.T) {
	e := newBuildEnv(t)
	layer := e.layer(map[string]string{
		"skills/brand-new/SKILL.md": skillMD("brand-new", "Use for new", "x"),
	})

	_, err := e.b.Build(e.opts(nil, layer))
	if err == nil || !strings.Contains(err.Error(), "brand-new") {
		t.Fatalf("expected an error naming the skill, got %v", err)
	}
	if exists(e.tree()) || len(e.leftovers()) != 0 {
		t.Error("nothing should be left behind")
	}
}

func TestBuild_OverrideOfAnExcludedSkillFails(t *testing.T) {
	e := newBuildEnv(t)
	layer := e.layer(map[string]string{
		"skills/gamma/SKILL.md": skillMD("gamma", "Use when you need gamma", "x"),
	})

	_, err := e.b.Build(e.opts([]string{"gamma"}, layer))
	if err == nil || !strings.Contains(err.Error(), "gamma") {
		t.Fatalf("expected an error naming the skill, got %v", err)
	}
}

func TestBuild_OverrideKeepsUpstreamsSkillName(t *testing.T) {
	e := newBuildEnv(t)
	layer := e.layer(map[string]string{
		"skills/gamma/SKILL.md": skillMD("gamma-renamed", "Use when you need gamma", "x"),
	})

	_, err := e.b.Build(e.opts(nil, layer))
	if err == nil || !strings.Contains(err.Error(), "gamma-renamed") || !strings.Contains(err.Error(), "gamma") {
		t.Fatalf("expected the error to name both names, got %v", err)
	}
}

func TestBuild_FullReplacementMayChangeTheDescription(t *testing.T) {
	e := newBuildEnv(t)
	layer := e.layer(map[string]string{
		"skills/gamma/SKILL.md": skillMD("gamma", "A different description", "replaced"),
	})

	if _, err := e.b.Build(e.opts(nil, layer)); err != nil {
		t.Fatalf("a full replacement may reword its description: %v", err)
	}
}

func TestBuild_OverrideWithoutFrontmatterFails(t *testing.T) {
	e := newBuildEnv(t)
	layer := e.layer(map[string]string{"skills/gamma/SKILL.md": "just text, no frontmatter\n"})

	_, err := e.b.Build(e.opts(nil, layer))
	if err == nil || !strings.Contains(err.Error(), "gamma") {
		t.Fatalf("expected an error naming the skill, got %v", err)
	}
}

// --- wrappers --------------------------------------------------------------

func TestBuild_WrapperKeepsUpstreamsSkillBesideIt(t *testing.T) {
	e := newBuildEnv(t)
	upstream := readFile(t, filepath.Join(e.source, "skills", "gamma", "SKILL.md"))
	wrapper := skillMD("gamma", "Use when you need gamma", "Read UPSTREAM.md first, then also do this.")
	layer := e.layer(map[string]string{"skills/gamma/SKILL.md": wrapper})

	res, err := e.b.Build(e.opts(nil, layer))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, filepath.Join(res.Dir, "skills", "gamma", "SKILL.md")); got != wrapper {
		t.Errorf("SKILL.md should be the wrapper, got %q", got)
	}
	if got := readFile(t, filepath.Join(res.Dir, "skills", "gamma", "UPSTREAM.md")); got != upstream {
		t.Errorf("UPSTREAM.md should be upstream's SKILL.md, got %q", got)
	}
}

func TestBuild_WrapperOverADevgetaOverrideWrapsUpstreamNotDevgeta(t *testing.T) {
	e := newBuildEnv(t)
	upstream := readFile(t, filepath.Join(e.source, "skills", "gamma", "SKILL.md"))
	devgetaLayer := e.layer(map[string]string{
		"skills/gamma/SKILL.md": skillMD("gamma", "Use when you need gamma", "devgeta's full replacement"),
	})
	userLayer := e.layer(map[string]string{
		"skills/gamma/SKILL.md": skillMD("gamma", "Use when you need gamma", "See UPSTREAM.md."),
	})

	res, err := e.b.Build(e.opts(nil, devgetaLayer, userLayer))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, filepath.Join(res.Dir, "skills", "gamma", "UPSTREAM.md")); got != upstream {
		t.Errorf("UPSTREAM.md must be upstream's file, never devgeta's override; got %q", got)
	}
}

func TestBuild_WrapperMustKeepUpstreamsDescription(t *testing.T) {
	e := newBuildEnv(t)
	layer := e.layer(map[string]string{
		"skills/gamma/SKILL.md": skillMD("gamma", "Changed description", "See UPSTREAM.md."),
	})

	_, err := e.b.Build(e.opts(nil, layer))
	if err == nil || !strings.Contains(err.Error(), "description") || !strings.Contains(err.Error(), "gamma") {
		t.Fatalf("expected a description error naming the skill, got %v", err)
	}
	if exists(e.tree()) || len(e.leftovers()) != 0 {
		t.Error("nothing should be left behind")
	}
}

func TestBuild_AnOverrideMayNotShipItsOwnUpstreamMD(t *testing.T) {
	e := newBuildEnv(t)
	layer := e.layer(map[string]string{
		"skills/gamma/SKILL.md":    skillMD("gamma", "Use when you need gamma", "See UPSTREAM.md."),
		"skills/gamma/UPSTREAM.md": "fake",
	})

	_, err := e.b.Build(e.opts(nil, layer))
	if err == nil || !strings.Contains(err.Error(), "UPSTREAM.md") {
		t.Fatalf("expected an error about UPSTREAM.md, got %v", err)
	}
}

// --- required files --------------------------------------------------------

func TestBuild_RequiredFiles(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, src string)
		want   string
	}{
		{"no plugin.json", func(t *testing.T, src string) {
			_ = os.Remove(filepath.Join(src, ".claude-plugin", "plugin.json"))
		}, "plugin.json"},
		{"plugin.json is not json", func(t *testing.T, src string) {
			writeFile(t, filepath.Join(src, ".claude-plugin", "plugin.json"), "{", 0o644)
		}, "plugin.json"},
		{"no package.json", func(t *testing.T, src string) {
			_ = os.Remove(filepath.Join(src, "package.json"))
		}, "package.json"},
		{"package.json without pi", func(t *testing.T, src string) {
			writeFile(t, filepath.Join(src, "package.json"), `{"main":"index.js"}`, 0o644)
		}, "pi"},
		{"package.json without main", func(t *testing.T, src string) {
			writeFile(t, filepath.Join(src, "package.json"), `{"pi":{}}`, 0o644)
		}, "main"},
		{"default hooks file is missing", func(t *testing.T, src string) {
			_ = os.Remove(filepath.Join(src, "hooks", "hooks.json"))
		}, "hooks/hooks.json"},
		{"a declared hooks file is missing", func(t *testing.T, src string) {
			writeFile(t, filepath.Join(src, ".claude-plugin", "plugin.json"),
				`{"name":"fixture","hooks":["./hooks/hooks.json","./hooks/gone.json"]}`, 0o644)
		}, "hooks/gone.json"},
		{"a declared hooks file climbs out of the tree", func(t *testing.T, src string) {
			writeFile(t, filepath.Join(src, ".claude-plugin", "plugin.json"),
				`{"name":"fixture","hooks":"../outside.json"}`, 0o644)
		}, "outside.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newBuildEnv(t)
			tc.mutate(t, e.source)

			_, err := e.b.Build(e.opts(nil))
			if err == nil {
				t.Fatal("expected the build to fail")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected the error to mention %q, got: %v", tc.want, err)
			}
			if exists(e.tree()) || len(e.leftovers()) != 0 {
				t.Error("a failed build must leave nothing behind")
			}
		})
	}
}

// A plugin that declares its hooks in plugin.json (Ponytail does) has no
// hooks/hooks.json, and that is fine as long as the declared files exist.
func TestBuild_HooksDeclaredInPluginJSONReplaceTheDefaultFile(t *testing.T) {
	cases := map[string]string{
		"a list":        `{"name":"fixture","hooks":["./hooks/a.json","./hooks/b.json"]}`,
		"a single path": `{"name":"fixture","hooks":"./hooks/a.json"}`,
		"inline hooks":  `{"name":"fixture","hooks":{"hooks":{}}}`,
	}
	for name, plugin := range cases {
		t.Run(name, func(t *testing.T) {
			e := newBuildEnv(t)
			_ = os.Remove(filepath.Join(e.source, "hooks", "hooks.json"))
			writeFile(t, filepath.Join(e.source, "hooks", "a.json"), "{}", 0o644)
			writeFile(t, filepath.Join(e.source, "hooks", "b.json"), "{}", 0o644)
			writeFile(t, filepath.Join(e.source, ".claude-plugin", "plugin.json"), plugin, 0o644)

			if _, err := e.b.Build(e.opts(nil)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestBuild_SkillsFolderIgnoresPlainFiles(t *testing.T) {
	e := newBuildEnv(t) // the fixture has skills/README.txt next to the skill folders

	if _, err := e.b.Build(e.opts(nil)); err != nil {
		t.Fatalf("a plain file in skills/ is not a skill: %v", err)
	}
	if _, err := e.b.Build(e.opts([]string{"README.txt"})); err == nil {
		t.Error("a plain file is not an excludable skill")
	}
}

// --- hashes ----------------------------------------------------------------

func TestBuild_RecordsUpstreamHashesOfFullReplacementsOnly(t *testing.T) {
	e := newBuildEnv(t)
	upstreamAlpha := readFile(t, filepath.Join(e.source, "skills", "alpha", "SKILL.md"))
	upstreamHooks := readFile(t, filepath.Join(e.source, "hooks", "hooks.json"))
	layer := e.layer(map[string]string{
		// full replacement
		"skills/alpha/SKILL.md": skillMD("alpha", "Use when you need the alpha behaviour", "replaced"),
		// wrapper: follows upstream, so no hash
		"skills/gamma/SKILL.md": skillMD("gamma", "Use when you need gamma", "See UPSTREAM.md."),
		// a file upstream does not have: nothing to hash
		"skills/gamma/extra.md": "extra",
		// a non-skill file replaced in full
		"hooks/hooks.json": `{ "hooks": {} }`,
	})

	res, err := e.b.Build(e.opts(nil, layer))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		"skills/alpha/SKILL.md": sha256Of(upstreamAlpha),
		"hooks/hooks.json":      sha256Of(upstreamHooks),
	}
	if !reflect.DeepEqual(res.ReplacedHashes, want) {
		t.Errorf("replaced hashes:\n got %v\nwant %v", res.ReplacedHashes, want)
	}
}

func TestChangedReplacements(t *testing.T) {
	recorded := map[string]string{"a": "sha256:1", "b": "sha256:2", "gone": "sha256:3"}
	current := map[string]string{"a": "sha256:1", "b": "sha256:CHANGED"}

	got := ChangedReplacements(recorded, current)
	if !reflect.DeepEqual(got, []string{"b", "gone"}) {
		t.Errorf("expected [b gone] (changed, then no longer replaced upstream), got %v", got)
	}
	if got := ChangedReplacements(nil, current); len(got) != 0 {
		t.Errorf("nothing recorded means nothing changed, got %v", got)
	}
}

// --- swap ------------------------------------------------------------------

func TestBuild_ReplacesAnExistingTreeAndDeletesTheOldOne(t *testing.T) {
	e := newBuildEnv(t)
	writeFile(t, filepath.Join(e.tree(), "old-only.txt"), "old", 0o644)

	if _, err := e.b.Build(e.opts(nil)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists(filepath.Join(e.tree(), "old-only.txt")) {
		t.Error("the old tree should have been replaced")
	}
	if !exists(filepath.Join(e.tree(), "skills", "alpha", "SKILL.md")) {
		t.Error("the new tree should be in place")
	}
	if left := e.leftovers(); len(left) != 0 {
		t.Errorf("old and temp trees must be deleted, found %v", left)
	}
}

func TestBuild_FailedBuildLeavesThePreviousTreeUntouched(t *testing.T) {
	e := newBuildEnv(t)
	writeFile(t, filepath.Join(e.tree(), "marker.txt"), "previous", 0o644)

	_, err := e.b.Build(e.opts([]string{"alpha"})) // fails: beta names alpha
	if err == nil {
		t.Fatal("expected the build to fail")
	}
	if got := readFile(t, filepath.Join(e.tree(), "marker.txt")); got != "previous" {
		t.Errorf("the previous tree must be untouched, marker is %q", got)
	}
	if left := e.leftovers(); len(left) != 0 {
		t.Errorf("a failed build must delete its temp tree, found %v", left)
	}
}

func TestBuild_FailedSwapPutsThePreviousTreeBack(t *testing.T) {
	e := newBuildEnv(t)
	writeFile(t, filepath.Join(e.tree(), "marker.txt"), "previous", 0o644)
	var calls atomic.Int32
	e.b.rename = func(oldPath, newPath string) error {
		// 1: old tree aside (works). 2: new tree into place (fails).
		// 3: old tree back (works).
		if calls.Add(1) == 2 {
			return errors.New("simulated rename failure")
		}
		return os.Rename(oldPath, newPath)
	}

	_, err := e.b.Build(e.opts(nil))
	if err == nil || !strings.Contains(err.Error(), "simulated rename failure") {
		t.Fatalf("expected the rename failure, got %v", err)
	}
	if got := readFile(t, filepath.Join(e.tree(), "marker.txt")); got != "previous" {
		t.Errorf("the previous tree must be put back, marker is %q", got)
	}
	if left := e.leftovers(); len(left) != 0 {
		t.Errorf("a failed swap must delete its temp tree, found %v", left)
	}
}

func TestBuild_FailedSwapThatCannotRestoreSaysWhereTheOldTreeIs(t *testing.T) {
	e := newBuildEnv(t)
	writeFile(t, filepath.Join(e.tree(), "marker.txt"), "previous", 0o644)
	var calls atomic.Int32
	e.b.rename = func(oldPath, newPath string) error {
		if n := calls.Add(1); n >= 2 {
			return errors.New("rename refused")
		}
		return os.Rename(oldPath, newPath)
	}

	_, err := e.b.Build(e.opts(nil))
	if err == nil || !strings.Contains(err.Error(), "previous tree") {
		t.Fatalf("expected the error to say where the previous tree went, got %v", err)
	}
}

// --- check -----------------------------------------------------------------

func TestCheck_BuildsAndRunsTheChecksButInstallsNothing(t *testing.T) {
	e := newBuildEnv(t)
	layer := e.layer(map[string]string{
		"skills/alpha/SKILL.md": skillMD("alpha", "Use when you need the alpha behaviour", "replaced"),
	})

	res, err := e.b.Check(e.opts([]string{"gamma"}, layer), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ReplacedHashes["skills/alpha/SKILL.md"] == "" {
		t.Error("check should report the replaced hashes")
	}
	if exists(e.tree()) {
		t.Error("check must not create the pack's tree")
	}
	if left := e.leftovers(); len(left) != 0 {
		t.Errorf("check must delete its temp tree, found %v", left)
	}
}

func TestCheck_ReportsAFailingCheck(t *testing.T) {
	e := newBuildEnv(t)

	if _, err := e.b.Check(e.opts([]string{"alpha"}), nil); err == nil {
		t.Fatal("expected the same checks as a build")
	}
	if left := e.leftovers(); len(left) != 0 {
		t.Errorf("check must delete its temp tree, found %v", left)
	}
}

// --- skill metadata --------------------------------------------------------

func TestParseSkillMeta(t *testing.T) {
	t.Run("yaml frontmatter", func(t *testing.T) {
		m, err := parseSkillMeta([]byte(skillMD("alpha", `"Use when: quoted"`, "body")))
		if err != nil {
			t.Fatal(err)
		}
		if m.Name != "alpha" || m.Description != "Use when: quoted" {
			t.Errorf("unexpected meta %+v", m)
		}
	})
	t.Run("a description that is not valid yaml still compares by its text", func(t *testing.T) {
		// An unquoted colon is invalid YAML but common in real skills.
		a, err := parseSkillMeta([]byte("---\nname: x\ndescription: Use when: a thing\n---\nbody"))
		if err != nil {
			t.Fatalf("a lenient fallback should read this: %v", err)
		}
		b, err := parseSkillMeta([]byte("---\nname: x\ndescription: Use when: a thing\n---\nother body"))
		if err != nil {
			t.Fatal(err)
		}
		if a != b || a.Name != "x" || !strings.Contains(a.Description, "a thing") {
			t.Errorf("unexpected meta %+v / %+v", a, b)
		}
	})
	t.Run("a folded multi-line description", func(t *testing.T) {
		m, err := parseSkillMeta([]byte("---\nname: x\ndescription: >\n  line one\n  line two\n---\nbody"))
		if err != nil {
			t.Fatal(err)
		}
		if m.Description != "line one line two" {
			t.Errorf("unexpected description %q", m.Description)
		}
	})
	t.Run("no frontmatter", func(t *testing.T) {
		if _, err := parseSkillMeta([]byte("# just markdown")); err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("no name", func(t *testing.T) {
		if _, err := parseSkillMeta([]byte("---\ndescription: d\n---\n")); err == nil {
			t.Error("expected an error for a missing name")
		}
	})
}

// --- embedded packs (the devgeta pack) --------------------------------------

func embeddedManifest() Manifest { return Manifest{Name: "devgeta", Source: sourceEmbedded} }

func newEmbeddedEnv(t *testing.T) *buildEnv {
	t.Helper()
	root := t.TempDir()
	return &buildEnv{t: t, root: root, source: copyFixture(t, "devgeta"), b: &Builder{Root: root}}
}

func TestBuild_EmbeddedPackBuildsFromItsFolderWithoutItsManifest(t *testing.T) {
	e := newEmbeddedEnv(t)

	res, err := e.b.Build(BuildOptions{Manifest: embeddedManifest(), Source: e.source})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Dir != filepath.Join(e.root, "devgeta") {
		t.Errorf("tree at %s", res.Dir)
	}
	for _, f := range []string{"DEVGETA.md", "package.json", "extensions/devgeta-instructions.js", ".claude-plugin/plugin.json", "skills/sample/SKILL.md"} {
		if !exists(filepath.Join(res.Dir, f)) {
			t.Errorf("%s should be in the tree", f)
		}
	}
	if exists(filepath.Join(res.Dir, "pack.yaml")) {
		t.Error("pack.yaml is devgeta's pin, not part of the pack")
	}
}

func TestBuild_EmbeddedPackAppliesTheUsersLayer(t *testing.T) {
	e := newEmbeddedEnv(t)
	layer := e.layer(map[string]string{"DEVGETA.md": "# Mine\n"})

	res, err := e.b.Build(BuildOptions{Manifest: embeddedManifest(), Source: e.source, Layers: []string{layer}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, filepath.Join(res.Dir, "DEVGETA.md")); got != "# Mine\n" {
		t.Errorf("the user's DEVGETA.md should win, got %q", got)
	}
}

func TestBuild_EmbeddedPackChecks(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, src string)
		want   string
	}{
		{"no DEVGETA.md", func(t *testing.T, src string) { _ = os.Remove(filepath.Join(src, "DEVGETA.md")) }, "DEVGETA.md"},
		{"an empty DEVGETA.md", func(t *testing.T, src string) {
			writeFile(t, filepath.Join(src, "DEVGETA.md"), " \n\n", 0o644)
		}, "DEVGETA.md"},
		{"no package.json", func(t *testing.T, src string) { _ = os.Remove(filepath.Join(src, "package.json")) }, "package.json"},
		{"package.json without pi", func(t *testing.T, src string) {
			writeFile(t, filepath.Join(src, "package.json"), `{"name":"devgeta"}`, 0o644)
		}, "pi"},
		{"no plugin.json", func(t *testing.T, src string) {
			_ = os.RemoveAll(filepath.Join(src, ".claude-plugin"))
		}, ".claude-plugin/plugin.json"},
		{"skills but no pi.skills entry", func(t *testing.T, src string) {
			writeFile(t, filepath.Join(src, "package.json"), `{"name":"devgeta","pi":{"extensions":[]}}`, 0o644)
		}, "pi.skills"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEmbeddedEnv(t)
			tc.mutate(t, e.source)

			_, err := e.b.Build(BuildOptions{Manifest: embeddedManifest(), Source: e.source})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.want, err)
			}
			if exists(filepath.Join(e.root, "devgeta")) || len(e.leftovers()) != 0 {
				t.Error("a failed build leaves nothing behind")
			}
		})
	}
}

func TestCheck_ShowsTheBuiltTreeToAnInspectorBeforeItIsDeleted(t *testing.T) {
	e := newBuildEnv(t)
	var seen string
	var hadAlpha bool

	_, err := e.b.Check(e.opts([]string{"gamma"}), func(dir string) error {
		seen = dir
		hadAlpha = exists(filepath.Join(dir, "skills", "alpha", "SKILL.md"))
		if exists(filepath.Join(dir, "skills", "gamma")) {
			t.Error("the inspector must see the tree as it would be installed, exclusion applied")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seen == "" || !hadAlpha {
		t.Fatalf("the inspector should have been shown the built tree, saw %q", seen)
	}
	if exists(seen) {
		t.Error("the tree is deleted once the inspector is done")
	}
}

func TestCheck_AnInspectorErrorFailsTheCheckAndStillDeletesTheTree(t *testing.T) {
	e := newBuildEnv(t)

	_, err := e.b.Check(e.opts(nil), func(string) error { return errors.New("validator said no") })

	if err == nil || !strings.Contains(err.Error(), "validator said no") {
		t.Fatalf("expected the inspector's error, got %v", err)
	}
	if left := e.leftovers(); len(left) != 0 {
		t.Errorf("check must delete its temp tree, found %v", left)
	}
}

func TestCheck_AnInspectorIsNotRunWhenTheBuildFails(t *testing.T) {
	e := newBuildEnv(t)
	called := false

	_, err := e.b.Check(e.opts([]string{"alpha"}), func(string) error { called = true; return nil })

	if err == nil || called {
		t.Fatalf("a tree that failed its checks is not worth inspecting; err=%v called=%v", err, called)
	}
}

func TestBuild_ShippedOverrideOfAnExcludedSkillIsDropped(t *testing.T) {
	e := newBuildEnv(t)
	shipped := e.layer(map[string]string{
		"skills/gamma/SKILL.md": skillMD("gamma", "Use when you need gamma", "x"),
	})
	o := e.opts([]string{"gamma"})
	o.ShippedLayer = shipped

	if _, err := e.b.Build(o); err != nil {
		t.Fatalf("an excluded skill must win over devgeta's own override, got %v", err)
	}
	if exists(filepath.Join(e.tree(), "skills", "gamma")) {
		t.Error("the excluded skill must be absent, not resurrected by the shipped override")
	}
}

func TestBuild_UserOverrideOfAnExcludedSkillStillFails(t *testing.T) {
	e := newBuildEnv(t)
	user := e.layer(map[string]string{
		"skills/gamma/SKILL.md": skillMD("gamma", "Use when you need gamma", "x"),
	})
	o := e.opts([]string{"gamma"}, user)
	o.ShippedLayer = e.layer(nil)

	_, err := e.b.Build(o)
	if err == nil || !strings.Contains(err.Error(), "both excluded and overridden") {
		t.Fatalf("expected the excluded-and-overridden error, got %v", err)
	}
}

func TestBuild_ShippedLayerAppliesBeforeTheUsersLayers(t *testing.T) {
	e := newBuildEnv(t)
	o := e.opts(nil, e.layer(map[string]string{"skills/alpha/SKILL.md": skillMD("alpha", "Use when you need the alpha behaviour", "user")}))
	o.ShippedLayer = e.layer(map[string]string{"skills/alpha/SKILL.md": skillMD("alpha", "Use when you need the alpha behaviour", "shipped")})

	if _, err := e.b.Build(o); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(e.tree(), "skills", "alpha", "SKILL.md")); !strings.Contains(got, "user") {
		t.Errorf("the user's layer must win over the shipped one, got %q", got)
	}
}
