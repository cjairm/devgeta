package agentpack

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps/claude"
	"github.com/cjairm/devgeta/internal/commands"
)

// fakeClaude models just enough of `claude plugin ...` to drive the installer:
// registered marketplaces, installed plugins, and a log of every call. It is
// the mocked executor under the real *claude.Claude wrapper, so the arguments
// the installer sends are the ones the CLI would get.
type fakeClaude struct {
	t            *testing.T
	calls        []string
	marketplaces map[string]string // name -> path
	plugins      map[string]string // id -> scope
	failOn       string            // a call prefix that fails
	listErr      bool
}

func newFakeClaude(t *testing.T) (*fakeClaude, *claude.Claude) {
	t.Helper()
	f := &fakeClaude{t: t, marketplaces: map[string]string{}, plugins: map[string]string{}}
	base := commands.NewMockBaseCommand()
	base.ExecCommandFn = f.handle
	return f, &claude.Claude{Cmd: commands.NewMockCommand(), Base: base}
}

func (f *fakeClaude) handle(c commands.CommandParams) (string, string, error) {
	if c.Command != "claude" {
		f.t.Errorf("unexpected command %q", c.Command)
	}
	if !c.NoStdin {
		f.t.Errorf("a claude call must not read stdin: %v", c.Args)
	}
	call := strings.Join(c.Args, " ")
	f.calls = append(f.calls, call)
	if f.failOn != "" && strings.HasPrefix(call, f.failOn) {
		return "", "boom", errors.New("exit 1")
	}
	args := c.Args
	switch {
	case call == "plugin list --json":
		if f.listErr {
			return "", "cannot list", errors.New("exit 1")
		}
		var out []map[string]any
		for id, scope := range f.plugins {
			out = append(out, map[string]any{"id": id, "scope": scope, "enabled": true})
		}
		b, _ := json.Marshal(out)
		return string(b), "", nil
	case call == "plugin marketplace list --json":
		var out []map[string]any
		for name, path := range f.marketplaces {
			out = append(out, map[string]any{"name": name, "source": "directory", "path": path})
		}
		b, _ := json.Marshal(out)
		return string(b), "", nil
	case len(args) == 3 && args[1] == "validate":
		return "Validation passed", "", nil
	case len(args) == 4 && args[1] == "marketplace" && args[2] == "add":
		f.marketplaces["devgeta"] = args[3]
	case len(args) == 4 && args[1] == "marketplace" && args[2] == "remove":
		delete(f.marketplaces, args[3])
	case len(args) == 5 && args[1] == "install" && args[3] == "-s":
		f.plugins[args[2]] = args[4]
	case len(args) == 5 && args[1] == "uninstall" && args[3] == "-s":
		delete(f.plugins, args[2])
	default:
		f.t.Errorf("unexpected claude call: %s", call)
	}
	return "", "", nil
}

func (f *fakeClaude) called(prefix string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

type claudeRig struct {
	f         *fakeClaude
	in        *ClaudeInstaller
	root      string
	configDir string
}

func newClaudeRig(t *testing.T) *claudeRig {
	t.Helper()
	f, cli := newFakeClaude(t)
	root, configDir := t.TempDir(), t.TempDir()
	return &claudeRig{
		f:         f,
		root:      root,
		configDir: configDir,
		in: &ClaudeInstaller{
			CLI:       cli,
			PacksRoot: root,
			ConfigDir: configDir,
			LookPath:  func(string) (string, error) { return "/bin/claude", nil },
		},
	}
}

// builtPack lays down a built tree the way the builder would.
func (r *claudeRig) builtPack(t *testing.T, name string) Tree {
	t.Helper()
	dir := filepath.Join(r.root, name)
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"), `{"name":"`+name+`"}`, 0o644)
	return Tree{Pack: Manifest{Name: name}, Dir: dir}
}

func (r *claudeRig) marketplace(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(mustRead(t, filepath.Join(r.root, ".claude-plugin", "marketplace.json")), &m); err != nil {
		t.Fatalf("marketplace.json is not valid JSON: %v", err)
	}
	return m
}

func marketplacePlugins(m map[string]any) map[string]string {
	out := map[string]string{}
	for _, p := range m["plugins"].([]any) {
		pm := p.(map[string]any)
		out[pm["name"].(string)] = pm["source"].(string)
	}
	return out
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// --- tests -----------------------------------------------------------------

func TestClaudeInstaller_NameAvailabilityAndNothingSkipped(t *testing.T) {
	r := newClaudeRig(t)
	if r.in.Name() != "claude" {
		t.Errorf("name %q", r.in.Name())
	}
	if !r.in.Available() {
		t.Error("claude on PATH is available")
	}
	r.in.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if r.in.Available() {
		t.Error("claude missing from PATH is not available")
	}
	if got := r.in.Skipped(Tree{}); len(got) != 0 {
		t.Errorf("Claude Code loads everything a pack has, got %v", got)
	}
}

func TestClaudeInstaller_InstallRegistersTheMarketplaceThenTheSharedPlugin(t *testing.T) {
	r := newClaudeRig(t)
	tree := r.builtPack(t, "superpowers")

	rec, err := r.in.Install(tree)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Marketplace != "devgeta" || rec.Plugin != "superpowers@devgeta" {
		t.Errorf("unexpected record %+v", rec)
	}
	if got := r.f.marketplaces["devgeta"]; got != r.root {
		t.Errorf("the marketplace must point at the pack root %s, got %q", r.root, got)
	}
	if scope, ok := r.f.plugins["superpowers@devgeta"]; !ok || scope != "user" {
		t.Errorf("expected a user-scope install, got %v", r.f.plugins)
	}
	want := []string{
		"plugin marketplace list --json",
		"plugin marketplace add " + r.root,
		"plugin list --json",
		"plugin install superpowers@devgeta -s user",
	}
	if strings.Join(r.f.calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n got %v\nwant %v", r.f.calls, want)
	}
}

func TestClaudeInstaller_MarketplaceFileListsEveryBuiltPackAndNothingElse(t *testing.T) {
	r := newClaudeRig(t)
	r.builtPack(t, "ponytail")
	tree := r.builtPack(t, "superpowers")
	writeFile(t, filepath.Join(r.root, "devgeta", "DEVGETA.md"), "x", 0o644)                                     // no plugin.json: not a Claude plugin
	writeFile(t, filepath.Join(r.root, ".src", "superpowers@abc", ".claude-plugin", "plugin.json"), "{}", 0o644) // the clone cache
	writeFile(t, filepath.Join(r.root, ".build-x-1", ".claude-plugin", "plugin.json"), "{}", 0o644)              // a build in progress

	if _, err := r.in.Install(tree); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m := r.marketplace(t)
	if m["name"] != "devgeta" {
		t.Errorf("marketplace name %v", m["name"])
	}
	got := marketplacePlugins(m)
	if len(got) != 2 || got["ponytail"] != "./ponytail" || got["superpowers"] != "./superpowers" {
		t.Errorf("expected the two built packs by relative path, got %v", got)
	}
}

func TestClaudeInstaller_AnAlreadyRegisteredMarketplaceIsNotAddedAgain(t *testing.T) {
	r := newClaudeRig(t)
	r.f.marketplaces["devgeta"] = r.root

	if _, err := r.in.Install(r.builtPack(t, "superpowers")); err != nil {
		t.Fatal(err)
	}
	if r.f.called("plugin marketplace add") != 0 || r.f.called("plugin marketplace remove") != 0 {
		t.Errorf("the marketplace is already right: %v", r.f.calls)
	}
}

func TestClaudeInstaller_AMarketplaceAtAnotherPathIsReplaced(t *testing.T) {
	r := newClaudeRig(t)
	r.f.marketplaces["devgeta"] = "/old/data/dir"

	if _, err := r.in.Install(r.builtPack(t, "superpowers")); err != nil {
		t.Fatal(err)
	}
	if r.f.marketplaces["devgeta"] != r.root {
		t.Errorf("expected the marketplace to be repointed, got %q", r.f.marketplaces["devgeta"])
	}
	if r.f.called("plugin marketplace remove devgeta") != 1 {
		t.Errorf("the stale registration must be removed first: %v", r.f.calls)
	}
}

func TestClaudeInstaller_ReinstallRefreshesTheCacheCopy(t *testing.T) {
	r := newClaudeRig(t)
	r.f.marketplaces["devgeta"] = r.root
	r.f.plugins["superpowers@devgeta"] = "user"

	if _, err := r.in.Install(r.builtPack(t, "superpowers")); err != nil {
		t.Fatal(err)
	}
	// `plugin update` leaves Claude's cache copy stale; uninstall + install
	// refreshes it.
	uninstall := indexOf(r.f.calls, "plugin uninstall superpowers@devgeta -s user")
	install := indexOf(r.f.calls, "plugin install superpowers@devgeta -s user")
	if uninstall < 0 || install < 0 || uninstall > install {
		t.Errorf("expected an uninstall before the install: %v", r.f.calls)
	}
}

func indexOf(calls []string, call string) int {
	for i, c := range calls {
		if c == call {
			return i
		}
	}
	return -1
}

func TestClaudeInstaller_AFailedInstallLeavesNothingBehind(t *testing.T) {
	t.Run("removes the marketplace it added", func(t *testing.T) {
		r := newClaudeRig(t)
		r.f.failOn = "plugin install"

		_, err := r.in.Install(r.builtPack(t, "superpowers"))
		if err == nil || !strings.Contains(err.Error(), "superpowers") {
			t.Fatalf("expected an error naming the pack, got %v", err)
		}
		if len(r.f.marketplaces) != 0 || len(r.f.plugins) != 0 {
			t.Errorf("nothing may be left registered: %v %v", r.f.marketplaces, r.f.plugins)
		}
	})
	t.Run("keeps a marketplace that was already there", func(t *testing.T) {
		r := newClaudeRig(t)
		r.f.marketplaces["devgeta"] = r.root
		r.f.failOn = "plugin install"

		if _, err := r.in.Install(r.builtPack(t, "superpowers")); err == nil {
			t.Fatal("expected an error")
		}
		if _, ok := r.f.marketplaces["devgeta"]; !ok {
			t.Error("another pack may be using the marketplace; it must stay")
		}
	})
	t.Run("a failed marketplace add installs no plugin", func(t *testing.T) {
		r := newClaudeRig(t)
		r.f.failOn = "plugin marketplace add"

		if _, err := r.in.Install(r.builtPack(t, "superpowers")); err == nil {
			t.Fatal("expected an error")
		}
		if r.f.called("plugin install") != 0 {
			t.Errorf("no plugin may be installed without its marketplace: %v", r.f.calls)
		}
	})
}

func TestClaudeInstaller_InstallRefusesATreeWithoutAPluginManifest(t *testing.T) {
	r := newClaudeRig(t)
	dir := filepath.Join(r.root, "superpowers")
	writeFile(t, filepath.Join(dir, "README"), "x", 0o644)

	if _, err := r.in.Install(Tree{Pack: Manifest{Name: "superpowers"}, Dir: dir}); err == nil {
		t.Fatal("expected an error: nothing for Claude Code to load")
	}
	if r.f.called("plugin") != 0 {
		t.Errorf("no claude call: %v", r.f.calls)
	}
}

func TestClaudeInstaller_UninstallRemovesOnlyTheRecordedPluginAndDropsItFromTheMarketplace(t *testing.T) {
	r := newClaudeRig(t)
	r.builtPack(t, "ponytail")
	tree := r.builtPack(t, "superpowers")
	if _, err := r.in.Install(tree); err != nil {
		t.Fatal(err)
	}
	r.f.calls = nil
	// The engine deletes the tree after Uninstall; the marketplace must already
	// stop listing it or it would name a folder that no longer exists.
	if err := r.in.Uninstall(Record{Marketplace: "devgeta", Plugin: "superpowers@devgeta"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.f.plugins["superpowers@devgeta"]; ok {
		t.Error("the plugin should be gone")
	}
	if r.f.called("plugin marketplace") != 0 {
		t.Errorf("the marketplace stays until Finish: %v", r.f.calls)
	}
	got := marketplacePlugins(r.marketplace(t))
	if _, ok := got["superpowers"]; ok || got["ponytail"] != "./ponytail" {
		t.Errorf("expected only ponytail to be listed now, got %v", got)
	}
}

func TestClaudeInstaller_UninstallOfAPluginThatIsAlreadyGoneIsFine(t *testing.T) {
	r := newClaudeRig(t)
	r.builtPack(t, "ponytail")

	if err := r.in.Uninstall(Record{Plugin: "superpowers@devgeta"}); err != nil {
		t.Fatalf("a plugin the user already removed is not an error: %v", err)
	}
	if r.f.called("plugin uninstall") != 0 {
		t.Errorf("nothing to uninstall: %v", r.f.calls)
	}
}

func TestClaudeInstaller_UninstallReportsAFailure(t *testing.T) {
	r := newClaudeRig(t)
	r.f.plugins["superpowers@devgeta"] = "user"
	r.f.failOn = "plugin uninstall"

	err := r.in.Uninstall(Record{Plugin: "superpowers@devgeta"})
	if err == nil || !strings.Contains(err.Error(), "superpowers@devgeta") {
		t.Fatalf("expected the failure to name the plugin, got %v", err)
	}
}

func TestClaudeInstaller_FinishRemovesTheMarketplaceAndItsFile(t *testing.T) {
	r := newClaudeRig(t)
	tree := r.builtPack(t, "superpowers")
	if _, err := r.in.Install(tree); err != nil {
		t.Fatal(err)
	}
	r.f.calls = nil

	if err := r.in.Finish(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := r.f.marketplaces["devgeta"]; ok {
		t.Error("the marketplace should be removed")
	}
	if exists(filepath.Join(r.root, ".claude-plugin")) {
		t.Error("devgeta's marketplace file should be removed with it")
	}
}

func TestClaudeInstaller_FinishWithoutARegisteredMarketplaceCallsNothing(t *testing.T) {
	r := newClaudeRig(t)

	if err := r.in.Finish(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.f.called("plugin marketplace remove") != 0 {
		t.Errorf("nothing registered, nothing to remove: %v", r.f.calls)
	}
}

func TestClaudeInstaller_ConflictsNameTheCommandThatRemovesAnotherCopy(t *testing.T) {
	r := newClaudeRig(t)
	r.f.plugins["superpowers@claude-plugins-official"] = "user"
	r.f.plugins["superpowers@devgeta"] = "user"     // ours: not a conflict
	r.f.plugins["ponytail@somewhere-else"] = "user" // another pack: not a conflict

	got := r.in.Conflicts(Manifest{Name: "superpowers"})

	want := "claude plugin uninstall superpowers@claude-plugins-official -s user"
	if len(got) != 1 || !strings.Contains(got[0], want) {
		t.Errorf("expected exactly the removal command %q, got %v", want, got)
	}
}

func TestClaudeInstaller_NoConflictWhenNothingElseIsInstalled(t *testing.T) {
	r := newClaudeRig(t)
	r.f.plugins["superpowers@devgeta"] = "user"

	if got := r.in.Conflicts(Manifest{Name: "superpowers"}); len(got) != 0 {
		t.Errorf("our own install is not a conflict, got %v", got)
	}
}

func TestClaudeInstaller_ConflictCheckThatCannotListReportsNoConflict(t *testing.T) {
	r := newClaudeRig(t)
	r.f.listErr = true

	// Not being able to look must not invent a conflict (and a made-up
	// removal command); the install's own CLI calls will surface a real
	// problem.
	if got := r.in.Conflicts(Manifest{Name: "superpowers"}); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestClaudeInstaller_InspectValidatesTheBuiltTree(t *testing.T) {
	r := newClaudeRig(t)
	tree := r.builtPack(t, "superpowers")

	got := r.in.Inspect(tree)

	if got.Err != nil {
		t.Fatalf("unexpected error: %v", got.Err)
	}
	if len(got.Lines) != 1 || !strings.Contains(got.Lines[0], "claude plugin validate") || !strings.Contains(got.Lines[0], "passed") {
		t.Errorf("expected a 'validate passed' line, got %v", got.Lines)
	}
	if want := "plugin validate " + tree.Dir; indexOf(r.f.calls, want) < 0 || len(r.f.calls) != 1 {
		t.Errorf("expected exactly `claude %s`, got %v", want, r.f.calls)
	}
}

func TestClaudeInstaller_InspectReportsAValidationFailureAsAFinding(t *testing.T) {
	r := newClaudeRig(t)
	r.f.failOn = "plugin validate"

	got := r.in.Inspect(r.builtPack(t, "superpowers"))

	if got.Err == nil || !strings.Contains(got.Err.Error(), "superpowers") {
		t.Fatalf("expected a failing finding that names the pack, got %+v", got)
	}
}

func TestClaudeInstaller_InspectSkipsWhatItCannotValidate(t *testing.T) {
	r := newClaudeRig(t)
	r.in.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if got := r.in.Inspect(r.builtPack(t, "superpowers")); got.Err != nil || len(got.Lines) != 0 || len(r.f.calls) != 0 {
		t.Errorf("without Claude Code there is nothing to validate with, got %+v / %v", got, r.f.calls)
	}
}
