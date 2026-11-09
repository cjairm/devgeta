package agentpack

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
)

type fakeOpenCode struct {
	installed   bool
	rendered    []map[string]config.AgentPackState
	renderErr   error
	debugConfig string
	debugErr    error
	calls       [][]string
}

func (f *fakeOpenCode) IsInstalled() bool { return f.installed }
func (f *fakeOpenCode) ConfigureWithPacks(p map[string]config.AgentPackState) error {
	f.rendered = append(f.rendered, p)
	return f.renderErr
}
func (f *fakeOpenCode) ExecuteCommandOutput(args ...string) (string, error) {
	f.calls = append(f.calls, args)
	return f.debugConfig, f.debugErr
}

const ownConfig = "/home/u/.config/opencode/opencode.json"

func newOpenCodeInstaller() (*OpenCodeInstaller, *fakeOpenCode) {
	f := &fakeOpenCode{installed: true}
	return &OpenCodeInstaller{App: f, OwnConfig: ownConfig}, f
}

func debugConfigJSON(t *testing.T, sources ...map[string]any) string {
	t.Helper()
	b, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func doc(path string, plugins ...string) map[string]any {
	info := map[string]any{"$schema": "https://opencode.ai/config.json"}
	if plugins != nil {
		info["plugins"] = plugins
	}
	return map[string]any{"type": "document", "path": path, "info": info}
}

func TestOpenCodeInstaller_NameAndAvailability(t *testing.T) {
	in, f := newOpenCodeInstaller()
	if in.Name() != "opencode" {
		t.Errorf("name %q", in.Name())
	}
	if !in.Available() {
		t.Error("an installed opencode is available")
	}
	f.installed = false
	if in.Available() {
		t.Error("a missing opencode is not available")
	}
}

func opencodeTree(t *testing.T, name string, files ...string) Tree {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	for _, f := range files {
		writeFile(t, filepath.Join(dir, f), "export default {}", 0o644)
	}
	return Tree{Pack: Manifest{Name: name}, Dir: dir}
}

func TestOpenCodeInstaller_InstallRecordsTheFolderThatHasAnIndexJS(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  func(dir string) []string
	}{
		{"index.js at the pack root (Superpowers)", []string{"index.js", ".opencode/plugins/superpowers.js"},
			func(d string) []string { return []string{d} }},
		{"index.js in .opencode/plugins (Ponytail)", []string{".opencode/plugins/index.js", ".opencode/plugins/ponytail.mjs"},
			func(d string) []string { return []string{filepath.Join(d, ".opencode", "plugins")} }},
		{"both: the plugin folder wins", []string{"index.js", ".opencode/plugins/index.js"},
			func(d string) []string { return []string{filepath.Join(d, ".opencode", "plugins")} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, f := newOpenCodeInstaller()
			tree := opencodeTree(t, "pack", tc.files...)

			rec, err := in.Install(tree)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(rec.OpenCodePlugins, tc.want(tree.Dir)) {
				t.Errorf("plugins %v, want %v", rec.OpenCodePlugins, tc.want(tree.Dir))
			}
			if len(rec.OpenCodePlugins) == 0 {
				t.Fatal("expected a plugin folder to be recorded")
			}
			if !filepath.IsAbs(rec.OpenCodePlugins[0]) {
				t.Errorf("OpenCode 2 needs an absolute path, got %q", rec.OpenCodePlugins[0])
			}
			if len(f.rendered) != 0 || len(f.calls) != 0 {
				t.Error("Install only reports the record; the engine renders opencode.json from state")
			}
			if got := in.Skipped(tree); len(got) != 0 {
				t.Errorf("nothing is skipped when a plugin folder exists, got %v", got)
			}
		})
	}
}

func TestOpenCodeInstaller_APackWithoutAPluginEntryIsNamedNotFaked(t *testing.T) {
	in, _ := newOpenCodeInstaller()
	tree := opencodeTree(t, "pack", "skills/a/SKILL.md")

	rec, err := in.Install(tree)
	if err != nil {
		t.Fatalf("not having an OpenCode plugin is not a failure: %v", err)
	}
	if len(rec.OpenCodePlugins) != 0 {
		t.Errorf("nothing to register, got %v", rec.OpenCodePlugins)
	}
	got := in.Skipped(tree)
	if len(got) != 1 || !strings.Contains(got[0], "index.js") {
		t.Errorf("expected the missing plugin entry to be named, got %v", got)
	}
}

func TestOpenCodeInstaller_UninstallHasNothingOfItsOwnToDo(t *testing.T) {
	in, f := newOpenCodeInstaller()

	// The engine drops the record from state and re-renders opencode.json
	// through Render; there is nothing else in OpenCode to remove.
	if err := in.Uninstall(Record{OpenCodePlugins: []string{"/p"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.rendered) != 0 || len(f.calls) != 0 {
		t.Error("Uninstall must not touch opencode itself")
	}
}

func TestOpenCodeInstaller_RenderRendersTheStateItIsGiven(t *testing.T) {
	in, f := newOpenCodeInstaller()
	state := map[string]config.AgentPackState{"superpowers": {Version: "v7.0.2"}}

	if err := in.Render(state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(f.rendered) != 1 || !reflect.DeepEqual(f.rendered[0], state) {
		t.Errorf("expected one render of the given state, got %v", f.rendered)
	}

	f.renderErr = errors.New("template broke")
	if err := in.Render(state); err == nil || !strings.Contains(err.Error(), "template broke") {
		t.Errorf("a render failure must come back, got %v", err)
	}
}

var superpowers = Manifest{Name: "superpowers", Repo: "https://github.com/obra/superpowers.git"}

func TestOpenCodeInstaller_ConflictsFindAnotherPluginSpecForTheSameUpstream(t *testing.T) {
	cases := []struct {
		name string
		spec string
	}{
		{"a git package spec", "superpowers@git+https://github.com/obra/superpowers.git"},
		{"a github URL", "git+https://github.com/Obra/Superpowers"},
		{"the bare npm name", "superpowers"},
		{"a checkout of it", "/home/u/code/superpowers"},
		{"a checkout's plugin folder", "/home/u/code/superpowers/.opencode/plugins/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, f := newOpenCodeInstaller()
			f.debugConfig = debugConfigJSON(t,
				doc(ownConfig, "/data/agent-packs/superpowers"),
				doc("/work/app/opencode.json", tc.spec),
			)

			got := in.Conflicts(superpowers)

			if len(got) != 1 || !strings.Contains(got[0], tc.spec) || !strings.Contains(got[0], "/work/app/opencode.json") {
				t.Errorf("expected one message naming the spec and the file, got %v", got)
			}
		})
	}
}

func TestOpenCodeInstaller_ConflictCheckIgnoresWhatIsNotAConflict(t *testing.T) {
	in, f := newOpenCodeInstaller()
	f.debugConfig = debugConfigJSON(t,
		// devgeta's own rendered file lists our own tree: never a conflict.
		doc(ownConfig, "/data/agent-packs/superpowers", "/data/agent-packs/ponytail/.opencode/plugins"),
		// Another project's unrelated plugins.
		doc("/work/app/opencode.json", "oh-my-opencode", "/home/u/code/my-plugin", "superpowers-extras"),
		// A config with no plugins at all.
		doc("/work/other/opencode.json"),
		map[string]any{"type": "directory", "path": "/home/u/.config/opencode"},
	)

	if got := in.Conflicts(superpowers); len(got) != 0 {
		t.Errorf("expected no conflicts, got %v", got)
	}
}

func TestOpenCodeInstaller_ConflictCheckAsksForAStandaloneConfigDump(t *testing.T) {
	in, f := newOpenCodeInstaller()
	f.debugConfig = "[]"

	in.Conflicts(superpowers)

	// --standalone keeps this query away from the user's shared background
	// service, which OpenCode 2 otherwise tries first.
	if len(f.calls) != 1 || !reflect.DeepEqual(f.calls[0], []string{"debug", "config", "--standalone"}) {
		t.Errorf("expected `opencode debug config --standalone`, got %v", f.calls)
	}
}

func TestOpenCodeInstaller_ConflictCheckThatCannotReadReportsNoConflict(t *testing.T) {
	t.Run("the command fails", func(t *testing.T) {
		in, f := newOpenCodeInstaller()
		f.debugErr = errors.New("boom")
		if got := in.Conflicts(superpowers); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})
	t.Run("the output is not JSON", func(t *testing.T) {
		in, f := newOpenCodeInstaller()
		f.debugConfig = "not json"
		if got := in.Conflicts(superpowers); len(got) != 0 {
			t.Errorf("got %v", got)
		}
	})
	t.Run("an embedded pack has no upstream to conflict with", func(t *testing.T) {
		in, f := newOpenCodeInstaller()
		f.debugConfig = debugConfigJSON(t, doc("/w/opencode.json", "devgeta"))
		if got := in.Conflicts(Manifest{Name: "devgeta", Source: sourceEmbedded}); len(got) != 0 {
			t.Errorf("got %v", got)
		}
		if len(f.calls) != 0 {
			t.Error("no need to run opencode for an embedded pack")
		}
	})
}

func TestOpenCodeInstaller_InstallingTwiceGivesTheSameRecord(t *testing.T) {
	in, _ := newOpenCodeInstaller()
	tree := opencodeTree(t, "pack", "index.js")

	first, err := in.Install(tree)
	if err != nil {
		t.Fatal(err)
	}
	second, err := in.Install(tree)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("a re-add must be idempotent: %+v vs %+v", first, second)
	}
}

func TestOpenCodeInstaller_TheDevgetaPackIsAnInstructionsEntryNotAPlugin(t *testing.T) {
	in, f := newOpenCodeInstaller()
	dir := filepath.Join(t.TempDir(), "devgeta")
	writeFile(t, filepath.Join(dir, "DEVGETA.md"), "# Style\n", 0o644)
	tree := Tree{Pack: embeddedManifest(), Dir: dir}

	rec, err := in.Install(tree)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{filepath.Join(dir, "DEVGETA.md")}
	if !reflect.DeepEqual(rec.OpenCodeInstructions, want) || len(rec.OpenCodePlugins) != 0 {
		t.Errorf("expected only instructions %v, got %+v", want, rec)
	}
	if got := in.Skipped(tree); len(got) != 0 {
		t.Errorf("nothing is skipped for the devgeta pack, got %v", got)
	}
	if len(f.rendered) != 0 {
		t.Error("the engine renders opencode.json from state; Install only reports the record")
	}
}

func TestOpenCodeInstaller_TheDevgetaPackNeedsItsInstructionsFile(t *testing.T) {
	in, _ := newOpenCodeInstaller()
	tree := Tree{Pack: embeddedManifest(), Dir: t.TempDir()}

	if _, err := in.Install(tree); err == nil || !strings.Contains(err.Error(), "DEVGETA.md") {
		t.Fatalf("expected an error naming DEVGETA.md, got %v", err)
	}
}
