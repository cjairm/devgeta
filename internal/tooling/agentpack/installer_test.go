package agentpack

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
)

// --- fakes -----------------------------------------------------------------

// journal records, in order, everything the engine asked of its collaborators,
// so a test can assert the sequence (installs, then a rollback in reverse).
type journal struct{ events []string }

func (j *journal) add(format string, args ...any) {
	j.events = append(j.events, fmt.Sprintf(format, args...))
}

type fakeInstaller struct {
	name        string
	unavailable bool
	conflicts   []string
	installErr  error
	unErr       error
	skipped     []string
	record      Record
	j           *journal

	rendered   []map[string]config.AgentPackState
	finished   int
	lastRecord Record
}

func (f *fakeInstaller) Name() string                { return f.name }
func (f *fakeInstaller) Available() bool             { return !f.unavailable }
func (f *fakeInstaller) Conflicts(Manifest) []string { return f.conflicts }
func (f *fakeInstaller) Skipped(Tree) []string       { return f.skipped }
func (f *fakeInstaller) Finish() error               { f.finished++; f.j.add("finish %s", f.name); return nil }
func (f *fakeInstaller) Install(tree Tree) (Record, error) {
	f.j.add("install %s %s", f.name, tree.Dir)
	if f.installErr != nil {
		return Record{}, f.installErr
	}
	r := f.record
	if r.Plugin == "" && r.PiPackage == "" && len(r.OpenCodePlugins) == 0 {
		r.Plugin = f.name + ":" + tree.Pack.Name
	}
	return r, nil
}
func (f *fakeInstaller) Uninstall(r Record) error {
	f.lastRecord = r
	f.j.add("uninstall %s %s", f.name, r.Plugin)
	return f.unErr
}
func (f *fakeInstaller) Render(packs map[string]config.AgentPackState) error {
	// A snapshot: the engine keeps mutating its working map afterwards.
	snap := map[string]config.AgentPackState{}
	for k, v := range packs {
		coders := map[string]config.AgentPackCoderState{}
		for c, r := range v.Coders {
			coders[c] = r
		}
		v.Coders = coders
		snap[k] = v
	}
	f.rendered = append(f.rendered, snap)
	f.j.add("render %s", f.name)
	return nil
}

// plainInstaller has no Render or Finish, like a coder whose own CLI writes
// its config.
type plainInstaller struct{ *fakeInstaller }

func (plainInstaller) Render() {} // wrong signature on purpose: not a Renderer
func (plainInstaller) Finish() {} // wrong signature on purpose: not a Finisher

type fakeTrees struct {
	j           *journal
	dir         string
	hashes      map[string]string
	prepareErr  error
	removed     []string
	gotExclude  [][]string
	checks      []checkCall
	checkErr    error
	refSHA      string
	checkHashes map[string]string
}

func (f *fakeTrees) Prepare(m Manifest, exclude []string) (Tree, BuildResult, error) {
	f.j.add("prepare %s", m.Name)
	f.gotExclude = append(f.gotExclude, exclude)
	if f.prepareErr != nil {
		return Tree{}, BuildResult{}, f.prepareErr
	}
	return Tree{Pack: m, Dir: f.dir + "/" + m.Name}, BuildResult{Dir: f.dir + "/" + m.Name, ReplacedHashes: f.hashes}, nil
}

type checkCall struct {
	pack, ref string
	exclude   []string
}

func (f *fakeTrees) Check(m Manifest, ref string, exclude []string, inspect func(dir string) error) (BuildResult, string, error) {
	f.j.add("check %s %s", m.Name, ref)
	f.checks = append(f.checks, checkCall{m.Name, ref, exclude})
	if f.checkErr != nil {
		return BuildResult{}, "", f.checkErr
	}
	if inspect != nil {
		if err := inspect(f.dir + "/check-" + m.Name); err != nil {
			return BuildResult{}, "", err
		}
	}
	sha := m.SHA
	if ref != "" {
		sha = f.refSHA
	}
	return BuildResult{ReplacedHashes: f.checkHashes}, sha, nil
}

func (f *fakeTrees) Remove(pack string) error {
	f.j.add("remove-tree %s", pack)
	f.removed = append(f.removed, pack)
	return nil
}

type fakeState struct {
	j       *journal
	packs   map[string]config.AgentPackState
	updates int
}

func (f *fakeState) Load() (map[string]config.AgentPackState, error) {
	out := map[string]config.AgentPackState{}
	for k, v := range f.packs {
		out[k] = v
	}
	return out, nil
}
func (f *fakeState) Update(fn func(map[string]config.AgentPackState) error) error {
	f.updates++
	f.j.add("save-state")
	if f.packs == nil {
		f.packs = map[string]config.AgentPackState{}
	}
	return fn(f.packs)
}

type rig struct {
	e      *Engine
	j      *journal
	trees  *fakeTrees
	state  *fakeState
	claude *fakeInstaller
	oc     *fakeInstaller
	pi     *fakeInstaller
}

func newRig() *rig {
	j := &journal{}
	r := &rig{j: j}
	r.claude = &fakeInstaller{name: "claude", j: j}
	r.oc = &fakeInstaller{name: "opencode", j: j}
	r.pi = &fakeInstaller{name: "pi", j: j}
	r.trees = &fakeTrees{j: j, dir: "/trees", hashes: map[string]string{"skills/a/SKILL.md": "sha256:1"}}
	r.state = &fakeState{j: j}
	r.e = &Engine{
		Manifests: []Manifest{
			{Name: "superpowers", Repo: "https://x/s.git", Tag: "v7.0.2", SHA: goodSHA},
			{Name: "ponytail", Repo: "https://x/p.git", Tag: "v5.3.3", SHA: strings.Repeat("b", 40)},
		},
		Installers: []Installer{r.claude, r.oc, r.pi},
		Trees:      r.trees,
		State:      r.state,
	}
	return r
}

func (r *rig) count(prefix string) int {
	n := 0
	for _, e := range r.j.events {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

func outcome(t *testing.T, rep Report, coder string) CoderOutcome {
	t.Helper()
	for _, o := range rep.Outcomes {
		if o.Coder == coder {
			return o
		}
	}
	t.Fatalf("no outcome for %s in %+v", coder, rep.Outcomes)
	return CoderOutcome{}
}

// --- install ---------------------------------------------------------------

func TestInstall_BuildsOnceInstallsEverywhereAndWritesStateOnce(t *testing.T) {
	r := newRig()

	rep, err := r.e.Install(InstallRequest{Pack: "superpowers"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.count("prepare") != 1 {
		t.Errorf("the tree is built once for all coders, got %d builds", r.count("prepare"))
	}
	for _, c := range []string{"claude", "opencode", "pi"} {
		if !outcome(t, rep, c).Done {
			t.Errorf("%s should be installed: %+v", c, outcome(t, rep, c))
		}
	}
	if r.state.updates != 1 {
		t.Errorf("state is written exactly once, at the end; got %d writes", r.state.updates)
	}
	got := r.state.packs["superpowers"]
	if got.Version != "v7.0.2" || got.SHA != goodSHA {
		t.Errorf("state should hold the pin, got %+v", got)
	}
	if got.ReplacedHashes["skills/a/SKILL.md"] != "sha256:1" {
		t.Errorf("state should hold the replaced hashes, got %v", got.ReplacedHashes)
	}
	if got.Coders["claude"].Plugin != "claude:superpowers" || got.Coders["pi"].Plugin != "pi:superpowers" {
		t.Errorf("state should hold each coder's record, got %+v", got.Coders)
	}
	if rep.Version != "v7.0.2" || rep.SHA != goodSHA {
		t.Errorf("report should carry the pin, got %+v", rep)
	}
	// The state write is the very last thing that happens.
	if last := r.j.events[len(r.j.events)-1]; last != "save-state" {
		t.Errorf("state must be saved last, journal ends with %q (%v)", last, r.j.events)
	}
}

func TestInstall_ASecondCoderFailingUndoesTheFirstOnesAndLeavesStateAlone(t *testing.T) {
	r := newRig()
	r.pi.installErr = errors.New("pi install exploded")

	_, err := r.e.Install(InstallRequest{Pack: "superpowers"})
	if err == nil || !strings.Contains(err.Error(), "pi install exploded") || !strings.Contains(err.Error(), "pi") {
		t.Fatalf("expected the error to name pi and its cause, got %v", err)
	}
	want := []string{
		"prepare superpowers",
		"install claude /trees/superpowers",
		"install opencode /trees/superpowers",
		"install pi /trees/superpowers",
		// undone in reverse order
		"uninstall opencode opencode:superpowers",
		"uninstall claude claude:superpowers",
	}
	var got []string
	for _, e := range r.j.events {
		if !strings.HasPrefix(e, "render") {
			got = append(got, e)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("journal:\n got %v\nwant %v", got, want)
	}
	if r.state.updates != 0 || len(r.state.packs) != 0 {
		t.Errorf("a failed run must leave state unchanged, got %d writes / %v", r.state.updates, r.state.packs)
	}
}

func TestInstall_RollbackFailuresAreReportedNotHidden(t *testing.T) {
	r := newRig()
	r.pi.installErr = errors.New("pi install exploded")
	r.claude.unErr = errors.New("claude refused to uninstall")

	_, err := r.e.Install(InstallRequest{Pack: "superpowers"})
	if err == nil || !strings.Contains(err.Error(), "claude refused to uninstall") {
		t.Fatalf("a failed rollback must be in the error, got %v", err)
	}
	if r.count("uninstall opencode") != 1 {
		t.Error("a failing rollback step must not stop the remaining ones")
	}
}

func TestInstall_ACoderThatWasAlreadyInstalledIsNotUndoneWhenALaterOneFails(t *testing.T) {
	r := newRig()
	// Claude already has the pack from an earlier run; all coders share the
	// built tree, so reinstalling refreshes it in place and a rollback must
	// not rip the previous install out.
	r.state.packs = map[string]config.AgentPackState{
		"superpowers": {Version: "v7.0.1", Coders: map[string]config.AgentPackCoderState{"claude": {Plugin: "old"}}},
	}
	r.pi.installErr = errors.New("pi install exploded")

	if _, err := r.e.Install(InstallRequest{Pack: "superpowers"}); err == nil {
		t.Fatal("expected an error")
	}
	if r.count("uninstall claude") != 0 {
		t.Error("claude had the pack before this run; rolling back must leave it")
	}
	if r.count("uninstall opencode") != 1 {
		t.Error("opencode was added by this run and must be undone")
	}
	if r.state.packs["superpowers"].Version != "v7.0.1" {
		t.Errorf("state must be unchanged, got %+v", r.state.packs["superpowers"])
	}
}

func TestInstall_ACoderThatIsNotInstalledIsSkippedWithAReasonNotFailed(t *testing.T) {
	r := newRig()
	r.pi.unavailable = true

	rep, err := r.e.Install(InstallRequest{Pack: "superpowers"})
	if err != nil {
		t.Fatalf("a missing coder is not a failure: %v", err)
	}
	pi := outcome(t, rep, "pi")
	if pi.Done || !strings.Contains(pi.Reason, "pi") || !strings.Contains(pi.Reason, "not installed") {
		t.Errorf("expected a 'pi is not installed' reason, got %+v", pi)
	}
	if r.count("install pi") != 0 {
		t.Error("an unavailable coder must not be installed into")
	}
	if _, ok := r.state.packs["superpowers"].Coders["pi"]; ok {
		t.Error("a skipped coder must not appear in state")
	}
	if !outcome(t, rep, "claude").Done || !outcome(t, rep, "opencode").Done {
		t.Error("the others should still be installed")
	}
}

func TestInstall_EveryCoderSkippedBuildsAndSavesNothing(t *testing.T) {
	r := newRig()
	r.claude.unavailable, r.oc.unavailable, r.pi.unavailable = true, true, true

	rep, err := r.e.Install(InstallRequest{Pack: "superpowers"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.count("prepare") != 0 || r.state.updates != 0 {
		t.Errorf("nothing to install means no fetch, build or state write: %v", r.j.events)
	}
	if len(rep.Outcomes) != 3 {
		t.Errorf("every coder should be accounted for, got %+v", rep.Outcomes)
	}
}

func TestInstall_ConflictsStopEverythingBeforeAnythingIsBuilt(t *testing.T) {
	r := newRig()
	r.oc.conflicts = []string{"remove it with: claude plugin uninstall superpowers@claude-plugins-official"}

	rep, err := r.e.Install(InstallRequest{Pack: "superpowers"})
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("expected a ConflictError, got %v", err)
	}
	if ce.Pack != "superpowers" || len(ce.Messages) != 1 ||
		!strings.Contains(err.Error(), "claude plugin uninstall superpowers@claude-plugins-official") {
		t.Errorf("the error must carry the exact removal command, got %v", err)
	}
	if r.count("prepare") != 0 || r.count("install") != 0 || r.state.updates != 0 {
		t.Errorf("a conflict installs nothing: %v", r.j.events)
	}
	_ = rep
}

func TestInstall_ConflictsOfAnUnavailableCoderAreNotAskedAbout(t *testing.T) {
	r := newRig()
	r.pi.unavailable = true
	r.pi.conflicts = []string{"should never be shown"}

	if _, err := r.e.Install(InstallRequest{Pack: "superpowers"}); err != nil {
		t.Fatalf("a coder that is skipped cannot conflict: %v", err)
	}
}

func TestInstall_CoderFlagSelectsSubsetAndRejectsUnknownNames(t *testing.T) {
	t.Run("a subset", func(t *testing.T) {
		r := newRig()
		rep, err := r.e.Install(InstallRequest{Pack: "superpowers", Coders: []string{"opencode"}})
		if err != nil {
			t.Fatal(err)
		}
		if r.count("install claude") != 0 || r.count("install pi") != 0 || r.count("install opencode") != 1 {
			t.Errorf("only opencode should be installed: %v", r.j.events)
		}
		if len(rep.Outcomes) != 1 {
			t.Errorf("only the asked-for coder is reported, got %+v", rep.Outcomes)
		}
		coders := r.state.packs["superpowers"].Coders
		if len(coders) != 1 || coders["opencode"].Plugin == "" {
			t.Errorf("state should list only opencode, got %v", coders)
		}
	})
	t.Run("an unknown coder fails before anything happens", func(t *testing.T) {
		r := newRig()
		_, err := r.e.Install(InstallRequest{Pack: "superpowers", Coders: []string{"vim"}})
		if err == nil || !strings.Contains(err.Error(), "vim") || !strings.Contains(err.Error(), "claude, opencode, pi") {
			t.Fatalf("expected an error naming the coder and the known ones, got %v", err)
		}
		if len(r.j.events) != 0 {
			t.Errorf("nothing should have run: %v", r.j.events)
		}
	})
}

func TestInstall_UnknownPackListsTheKnownOnes(t *testing.T) {
	r := newRig()

	_, err := r.e.Install(InstallRequest{Pack: "nope"})
	if err == nil || !strings.Contains(err.Error(), "nope") ||
		!strings.Contains(err.Error(), "ponytail") || !strings.Contains(err.Error(), "superpowers") {
		t.Fatalf("expected an error naming the pack and the known ones, got %v", err)
	}
}

func TestInstall_BuildFailureInstallsNothing(t *testing.T) {
	r := newRig()
	r.trees.prepareErr = errors.New("sha mismatch")

	_, err := r.e.Install(InstallRequest{Pack: "superpowers"})
	if err == nil || !strings.Contains(err.Error(), "sha mismatch") {
		t.Fatalf("expected the build error, got %v", err)
	}
	if r.count("install") != 0 || r.state.updates != 0 {
		t.Errorf("a failed build installs nothing: %v", r.j.events)
	}
}

func TestInstall_ExcludeIsSavedKeptAndCleared(t *testing.T) {
	list := func(s ...string) *[]string { return &s }
	cases := []struct {
		name      string
		saved     []string
		flag      *[]string
		wantBuilt []string
		wantSaved []string
	}{
		{"no flag keeps the saved list", []string{"a"}, nil, []string{"a"}, []string{"a"}},
		{"a new list replaces the saved one", []string{"a"}, list("b", "c"), []string{"b", "c"}, []string{"b", "c"}},
		{"an empty list clears it", []string{"a"}, list(), nil, nil},
		{"first install with no flag excludes nothing", nil, nil, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig()
			if tc.saved != nil {
				r.state.packs = map[string]config.AgentPackState{"superpowers": {Exclude: tc.saved}}
			}
			if _, err := r.e.Install(InstallRequest{Pack: "superpowers", Exclude: tc.flag}); err != nil {
				t.Fatal(err)
			}
			if len(r.trees.gotExclude) == 0 {
				t.Fatal("the tree was never built")
			}
			if !reflect.DeepEqual(r.trees.gotExclude[0], tc.wantBuilt) {
				t.Errorf("built with %v, want %v", r.trees.gotExclude[0], tc.wantBuilt)
			}
			if got := r.state.packs["superpowers"].Exclude; !reflect.DeepEqual(got, tc.wantSaved) {
				t.Errorf("saved %v, want %v", got, tc.wantSaved)
			}
		})
	}
}

func TestInstall_ReportsWhatACoderCannotLoad(t *testing.T) {
	r := newRig()
	r.pi.skipped = []string{"hooks (Pi has no hook system)"}

	rep, err := r.e.Install(InstallRequest{Pack: "superpowers"})
	if err != nil {
		t.Fatal(err)
	}
	if got := outcome(t, rep, "pi").Parts; !reflect.DeepEqual(got, []string{"hooks (Pi has no hook system)"}) {
		t.Errorf("the unsupported parts must be named, got %v", got)
	}
}

func TestInstall_RenderersSeeTheStateIncludingThisPackBeforeItIsSaved(t *testing.T) {
	r := newRig()

	if _, err := r.e.Install(InstallRequest{Pack: "superpowers", Coders: []string{"opencode"}}); err != nil {
		t.Fatal(err)
	}
	if len(r.oc.rendered) != 1 {
		t.Fatalf("expected one render, got %d", len(r.oc.rendered))
	}
	if got := r.oc.rendered[0]["superpowers"].Coders["opencode"].Plugin; got != "opencode:superpowers" {
		t.Errorf("the render must see this pack's record, got %q", got)
	}
	// Rendered before the single state write, so a failure can still leave
	// state untouched.
	if r.j.events[len(r.j.events)-1] != "save-state" {
		t.Errorf("state must still be saved last: %v", r.j.events)
	}
}

func TestInstall_RollbackRendersAgainWithoutThePack(t *testing.T) {
	r := newRig()
	r.pi.installErr = errors.New("boom")

	_, _ = r.e.Install(InstallRequest{Pack: "superpowers"})

	if len(r.oc.rendered) != 2 {
		t.Fatalf("expected a render on install and one on rollback, got %d", len(r.oc.rendered))
	}
	if _, ok := r.oc.rendered[1]["superpowers"]; ok {
		t.Errorf("after the rollback the state must no longer list the pack: %v", r.oc.rendered[1])
	}
}

func TestInstall_InstallersWithoutRenderOrFinishWork(t *testing.T) {
	r := newRig()
	r.e.Installers = []Installer{plainInstaller{r.claude}}

	if _, err := r.e.Install(InstallRequest{Pack: "superpowers"}); err != nil {
		t.Fatalf("an installer that is not a Renderer must be fine: %v", err)
	}
}

// --- uninstall -------------------------------------------------------------

func installedState() map[string]config.AgentPackState {
	return map[string]config.AgentPackState{
		"superpowers": {Version: "v7.0.2", SHA: goodSHA, Coders: map[string]config.AgentPackCoderState{
			"claude":   {Plugin: "superpowers@devgeta"},
			"opencode": {OpenCodePlugins: []string{"/t/superpowers"}},
			"pi":       {PiPackage: "/t/superpowers"},
		}},
		"ponytail": {Version: "v5.3.3", Coders: map[string]config.AgentPackCoderState{
			"claude": {Plugin: "ponytail@devgeta"},
		}},
	}
}

func TestUninstall_RemovesOnlyTheRecordedCodersAndKeepsTheRest(t *testing.T) {
	r := newRig()
	r.state.packs = installedState()

	rep, err := r.e.Uninstall(UninstallRequest{Pack: "superpowers", Coders: []string{"pi"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !outcome(t, rep, "pi").Done {
		t.Errorf("pi should be removed: %+v", rep)
	}
	if r.pi.lastRecord.PiPackage != "/t/superpowers" {
		t.Errorf("uninstall must be handed the recorded item, got %+v", r.pi.lastRecord)
	}
	if r.count("uninstall claude") != 0 || r.count("uninstall opencode") != 0 {
		t.Error("coders that were not asked for must be left alone")
	}
	coders := r.state.packs["superpowers"].Coders
	if _, ok := coders["pi"]; ok || len(coders) != 2 {
		t.Errorf("only pi's record should be gone, got %v", coders)
	}
	if len(r.trees.removed) != 0 {
		t.Errorf("the tree stays while a coder still uses it, removed %v", r.trees.removed)
	}
	if r.state.updates != 1 {
		t.Errorf("state is written once, got %d", r.state.updates)
	}
}

func TestUninstall_WithoutCoderRemovesFromEveryCoderThatHasIt(t *testing.T) {
	r := newRig()
	r.state.packs = installedState()

	if _, err := r.e.Uninstall(UninstallRequest{Pack: "superpowers"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range []string{"claude", "opencode", "pi"} {
		if r.count("uninstall "+c) != 1 {
			t.Errorf("%s should be uninstalled once: %v", c, r.j.events)
		}
	}
	if _, ok := r.state.packs["superpowers"]; ok {
		t.Error("with no coder left the state entry is deleted")
	}
	if !reflect.DeepEqual(r.trees.removed, []string{"superpowers"}) {
		t.Errorf("with no coder left the built tree is deleted, removed %v", r.trees.removed)
	}
	if _, ok := r.state.packs["ponytail"]; !ok {
		t.Error("other packs are untouched")
	}
}

func TestUninstall_FinishesACoderOnceNoPackIsLeftForIt(t *testing.T) {
	r := newRig()
	r.state.packs = installedState()

	// ponytail is the last pack besides superpowers on claude: removing
	// superpowers alone leaves claude in use.
	if _, err := r.e.Uninstall(UninstallRequest{Pack: "superpowers", Coders: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	if r.claude.finished != 0 {
		t.Error("claude still has ponytail, so it is not finished")
	}

	if _, err := r.e.Uninstall(UninstallRequest{Pack: "ponytail"}); err != nil {
		t.Fatal(err)
	}
	if r.claude.finished != 1 {
		t.Errorf("claude has no pack left, so it should be finished once; finished %d times", r.claude.finished)
	}
}

func TestUninstall_AnUnavailableCoderIsSkippedAndItsRecordKept(t *testing.T) {
	r := newRig()
	r.state.packs = installedState()
	r.pi.unavailable = true

	rep, err := r.e.Uninstall(UninstallRequest{Pack: "superpowers"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pi := outcome(t, rep, "pi")
	if pi.Done || !strings.Contains(pi.Reason, "not installed") {
		t.Errorf("expected pi to be skipped with a reason, got %+v", pi)
	}
	if _, ok := r.state.packs["superpowers"].Coders["pi"]; !ok {
		t.Error("the record of a coder we could not clean must be kept")
	}
	if len(r.trees.removed) != 0 {
		t.Error("the tree stays while a record remains")
	}
}

func TestUninstall_ACoderFailingDoesNotStopTheOthersAndKeepsItsRecord(t *testing.T) {
	r := newRig()
	r.state.packs = installedState()
	r.opencodeFails()

	_, err := r.e.Uninstall(UninstallRequest{Pack: "superpowers"})
	if err == nil || !strings.Contains(err.Error(), "opencode") {
		t.Fatalf("expected the failure to be reported, got %v", err)
	}
	if r.count("uninstall pi") != 1 || r.count("uninstall claude") != 1 {
		t.Errorf("the other coders must still be cleaned: %v", r.j.events)
	}
	coders := r.state.packs["superpowers"].Coders
	if _, ok := coders["opencode"]; !ok || len(coders) != 1 {
		t.Errorf("only the failed coder's record should remain, got %v", coders)
	}
	if len(r.trees.removed) != 0 {
		t.Error("the tree stays while a record remains")
	}
}

func (r *rig) opencodeFails() { r.oc.unErr = errors.New("opencode.json is locked") }

func TestUninstall_ACoderWithoutARecordIsReportedNotTouched(t *testing.T) {
	r := newRig()
	r.state.packs = installedState()

	rep, err := r.e.Uninstall(UninstallRequest{Pack: "ponytail", Coders: []string{"pi"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if o := outcome(t, rep, "pi"); o.Done || o.Reason == "" {
		t.Errorf("expected a reason, got %+v", o)
	}
	if r.count("uninstall") != 0 || r.state.updates != 0 {
		t.Errorf("nothing to do means nothing done: %v", r.j.events)
	}
}

func TestUninstall_APackThatIsNotInstalledIsAnError(t *testing.T) {
	r := newRig()

	_, err := r.e.Uninstall(UninstallRequest{Pack: "superpowers"})
	if err == nil || !strings.Contains(err.Error(), "superpowers") || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected a 'not installed' error naming the pack, got %v", err)
	}
	if _, err := r.e.Uninstall(UninstallRequest{Pack: "nope"}); err == nil {
		t.Error("an unknown pack is an error")
	}
}

func TestUninstall_RenderersSeeTheStateWithoutThePack(t *testing.T) {
	r := newRig()
	r.state.packs = installedState()

	if _, err := r.e.Uninstall(UninstallRequest{Pack: "superpowers", Coders: []string{"opencode"}}); err != nil {
		t.Fatal(err)
	}
	if len(r.oc.rendered) != 1 {
		t.Fatalf("expected one render, got %d", len(r.oc.rendered))
	}
	if _, ok := r.oc.rendered[0]["superpowers"].Coders["opencode"]; ok {
		t.Errorf("the render must no longer list the pack for opencode: %v", r.oc.rendered[0])
	}
}

// --- optional interfaces: RecordMerger and Noter ----------------------------

// mergingInstaller carries one fact of a previous record into the new one, the
// way Claude's installer keeps "devgeta added the @DEVGETA.md line" across a
// reinstall that finds the line already present.
type mergingInstaller struct {
	*fakeInstaller
	merged int
}

func (m *mergingInstaller) MergeRecord(prior, next Record) Record {
	m.merged++
	next.ClaudeMDLineAdded = next.ClaudeMDLineAdded || prior.ClaudeMDLineAdded
	return next
}

func TestInstall_AReinstallLetsTheInstallerCarryFactsFromThePreviousRecord(t *testing.T) {
	r := newRig()
	m := &mergingInstaller{fakeInstaller: r.claude}
	r.e.Installers = []Installer{m}
	r.state.packs = map[string]config.AgentPackState{
		"superpowers": {Coders: map[string]config.AgentPackCoderState{
			"claude": {Plugin: "old", ClaudeMDLineAdded: true},
		}},
	}

	if _, err := r.e.Install(InstallRequest{Pack: "superpowers"}); err != nil {
		t.Fatal(err)
	}
	got := r.state.packs["superpowers"].Coders["claude"]
	if !got.ClaudeMDLineAdded || got.Plugin != "claude:superpowers" {
		t.Errorf("expected the new record with the carried flag, got %+v", got)
	}
	if m.merged != 1 {
		t.Errorf("merged %d times, want once", m.merged)
	}
}

func TestInstall_AFirstInstallHasNothingToMerge(t *testing.T) {
	r := newRig()
	m := &mergingInstaller{fakeInstaller: r.claude}
	r.e.Installers = []Installer{m}

	if _, err := r.e.Install(InstallRequest{Pack: "superpowers"}); err != nil {
		t.Fatal(err)
	}
	if m.merged != 0 {
		t.Errorf("there is no previous record to merge, merged %d times", m.merged)
	}
}

// notingInstaller has something to tell the user after each call.
type notingInstaller struct {
	*fakeInstaller
	pending []string
}

func (n *notingInstaller) Install(tree Tree) (Record, error) {
	n.pending = append(n.pending, "installed note")
	return n.fakeInstaller.Install(tree)
}
func (n *notingInstaller) Uninstall(r Record) error {
	n.pending = append(n.pending, "removed note")
	return n.fakeInstaller.Uninstall(r)
}
func (n *notingInstaller) TakeNotes() []string {
	out := n.pending
	n.pending = nil
	return out
}

func TestEngine_NotesFromAnInstallerReachTheReport(t *testing.T) {
	r := newRig()
	n := &notingInstaller{fakeInstaller: r.claude}
	r.e.Installers = []Installer{n}

	rep, err := r.e.Install(InstallRequest{Pack: "superpowers"})
	if err != nil {
		t.Fatal(err)
	}
	if got := outcome(t, rep, "claude").Notes; !reflect.DeepEqual(got, []string{"installed note"}) {
		t.Errorf("install notes %v", got)
	}

	rep, err = r.e.Uninstall(UninstallRequest{Pack: "superpowers"})
	if err != nil {
		t.Fatal(err)
	}
	if got := outcome(t, rep, "claude").Notes; !reflect.DeepEqual(got, []string{"removed note"}) {
		t.Errorf("uninstall notes %v (the install note must not repeat)", got)
	}
}

func TestEngine_NotesFromAFailedUninstallAreKeptToo(t *testing.T) {
	r := newRig()
	n := &notingInstaller{fakeInstaller: r.claude}
	r.claude.unErr = errors.New("nope")
	r.e.Installers = []Installer{n}
	r.state.packs = installedState()

	rep, err := r.e.Uninstall(UninstallRequest{Pack: "superpowers"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := outcome(t, rep, "claude").Notes; len(got) != 1 {
		t.Errorf("a note written before the failure is still worth showing, got %v", got)
	}
}

// --- status, update and check ------------------------------------------------

type verifyingInstaller struct {
	*fakeInstaller
	problem string
}

func (v *verifyingInstaller) Verify(pack string, r Record) string { return v.problem }

type inspectingInstaller struct {
	*fakeInstaller
	inspection Inspection
	treeDirs   []string
}

func (i *inspectingInstaller) Inspect(tree Tree) Inspection {
	i.treeDirs = append(i.treeDirs, tree.Dir)
	return i.inspection
}

func statusOf(t *testing.T, all []PackStatus, pack string) PackStatus {
	t.Helper()
	for _, p := range all {
		if p.Pack == pack {
			return p
		}
	}
	t.Fatalf("no status for %s in %+v", pack, all)
	return PackStatus{}
}

func coderStatus(t *testing.T, p PackStatus, coder string) CoderStatus {
	t.Helper()
	for _, c := range p.Coders {
		if c.Coder == coder {
			return c
		}
	}
	t.Fatalf("no status for coder %s in %+v", coder, p)
	return CoderStatus{}
}

func TestStatus_ShowsPinnedAndInstalledVersionsAndEachCoder(t *testing.T) {
	r := newRig()
	r.e.Manifests = append(r.e.Manifests, Manifest{Name: "devgeta", Source: sourceEmbedded})
	r.pi.unavailable = true
	r.state.packs = map[string]config.AgentPackState{
		"superpowers": {Version: "v7.0.1", SHA: strings.Repeat("e", 40), Coders: map[string]config.AgentPackCoderState{
			"claude": {Plugin: "superpowers@devgeta"},
		}},
	}

	all, err := r.e.Status()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sp := statusOf(t, all, "superpowers")
	if sp.Pinned != "v7.0.2" || sp.Installed != "v7.0.1" || sp.SHA != strings.Repeat("e", 40) {
		t.Errorf("versions: %+v", sp)
	}
	if c := coderStatus(t, sp, "claude"); c.State != "installed" || c.Detail != "" {
		t.Errorf("claude: %+v", c)
	}
	if c := coderStatus(t, sp, "opencode"); c.State != "not installed" {
		t.Errorf("opencode: %+v", c)
	}
	if c := coderStatus(t, sp, "pi"); c.State != "skipped" || !strings.Contains(c.Detail, "pi is not installed") {
		t.Errorf("a coder that is not installed is skipped with its reason: %+v", c)
	}

	pt := statusOf(t, all, "ponytail")
	if pt.Installed != "" {
		t.Errorf("ponytail is not installed, got %q", pt.Installed)
	}
	if dg := statusOf(t, all, "devgeta"); dg.Pinned != "bundled" {
		t.Errorf("an embedded pack has no tag; it ships with the binary: %+v", dg)
	}
	if names := []string{all[0].Pack, all[1].Pack, all[2].Pack}; !reflect.DeepEqual(names, []string{"devgeta", "ponytail", "superpowers"}) {
		t.Errorf("packs are listed by name, got %v", names)
	}
}

func TestStatus_ARecordForACoderThatIsNoLongerInstalledSaysSo(t *testing.T) {
	r := newRig()
	r.pi.unavailable = true
	r.state.packs = installedState() // records pi, which is gone

	all, err := r.e.Status()
	if err != nil {
		t.Fatal(err)
	}
	c := coderStatus(t, statusOf(t, all, "superpowers"), "pi")
	if c.State != "installed" || !strings.Contains(c.Detail, "pi is not installed") || !strings.Contains(c.Detail, "record") {
		t.Errorf("expected the record to be shown with the reason, got %+v", c)
	}
}

func TestStatus_ReportsDriftFromACoderThatCanBeVerified(t *testing.T) {
	r := newRig()
	v := &verifyingInstaller{fakeInstaller: r.pi, problem: "Pi's settings no longer list /p"}
	r.e.Installers = []Installer{r.claude, r.oc, v}
	r.state.packs = installedState()

	all, err := r.e.Status()
	if err != nil {
		t.Fatal(err)
	}
	if c := coderStatus(t, statusOf(t, all, "superpowers"), "pi"); c.State != "installed" || c.Detail != v.problem {
		t.Errorf("expected the drift in the detail, got %+v", c)
	}
	if c := coderStatus(t, statusOf(t, all, "superpowers"), "claude"); c.Detail != "" {
		t.Errorf("a coder with nothing to verify shows no detail, got %+v", c)
	}
}

func TestInstall_RecordsABundledVersionForAnEmbeddedPack(t *testing.T) {
	r := newRig()
	r.e.Manifests = append(r.e.Manifests, Manifest{Name: "devgeta", Source: sourceEmbedded})

	rep, err := r.e.Install(InstallRequest{Pack: "devgeta"})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.state.packs["devgeta"]; got.Version != "bundled" || got.SHA != "" {
		t.Errorf("an embedded pack has no tag or commit; its state should say it is bundled, got %+v", got)
	}
	if rep.Version != "bundled" {
		t.Errorf("report version %q", rep.Version)
	}
}

func TestUpdate_ReinstallsIntoTheCodersThatHaveThePackAndKeepsTheSavedExclusions(t *testing.T) {
	r := newRig()
	r.state.packs = map[string]config.AgentPackState{
		"superpowers": {Version: "v7.0.1", Exclude: []string{"a"}, Coders: map[string]config.AgentPackCoderState{
			"claude": {Plugin: "old"}, "pi": {PiPackage: "/p"},
		}},
	}

	reports, err := r.e.Update("superpowers")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reports) != 1 || reports[0].Version != "v7.0.2" {
		t.Fatalf("expected one report at the pin, got %+v", reports)
	}
	if r.count("install claude") != 1 || r.count("install pi") != 1 || r.count("install opencode") != 0 {
		t.Errorf("only the coders that had the pack are updated: %v", r.j.events)
	}
	if len(r.trees.gotExclude) == 0 {
		t.Fatal("the tree was never rebuilt")
	}
	if !reflect.DeepEqual(r.trees.gotExclude[0], []string{"a"}) {
		t.Errorf("the saved exclusions must be kept and rebuilt with, got %v", r.trees.gotExclude[0])
	}
	if got := r.state.packs["superpowers"].Version; got != "v7.0.2" {
		t.Errorf("state should move to the pin, got %s", got)
	}
}

func TestUpdate_WithoutAPackUpdatesEveryInstalledOneInNameOrder(t *testing.T) {
	r := newRig()
	r.state.packs = installedState()

	reports, err := r.e.Update("")
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 || reports[0].Pack != "ponytail" || reports[1].Pack != "superpowers" {
		t.Errorf("expected ponytail then superpowers, got %+v", reports)
	}
}

func TestUpdate_NothingInstalledIsNotAnError(t *testing.T) {
	r := newRig()

	reports, err := r.e.Update("")
	if err != nil || len(reports) != 0 {
		t.Errorf("expected nothing and no error, got %+v / %v", reports, err)
	}
	if r.count("prepare") != 0 {
		t.Error("nothing to rebuild")
	}
}

func TestUpdate_ANamedPackThatIsNotInstalledIsAnErrorWithTheFix(t *testing.T) {
	r := newRig()

	_, err := r.e.Update("superpowers")
	if err == nil || !strings.Contains(err.Error(), "not installed") || !strings.Contains(err.Error(), "dg agent-pack install superpowers") {
		t.Fatalf("expected a 'not installed' error with the install command, got %v", err)
	}
	if _, err := r.e.Update("nope"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("an unknown pack is an error, got %v", err)
	}
}

func TestUpdate_StopsAtTheFirstFailureAndSaysWhichPack(t *testing.T) {
	r := newRig()
	r.state.packs = installedState()
	r.trees.prepareErr = errors.New("network down")

	reports, err := r.e.Update("")
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("expected the failure, got %v", err)
	}
	if len(reports) > 1 {
		t.Errorf("must stop at the first failure, got %d reports", len(reports))
	}
}

func TestCheck_BuildsWithTheSavedExclusionsAndReportsWhatChanged(t *testing.T) {
	r := newRig()
	r.state.packs = map[string]config.AgentPackState{
		"superpowers": {
			Version: "v7.0.2", Exclude: []string{"a"},
			ReplacedHashes: map[string]string{"skills/x/SKILL.md": "sha256:old", "skills/y/SKILL.md": "sha256:same"},
			Coders:         map[string]config.AgentPackCoderState{"claude": {Plugin: "p"}},
		},
	}
	r.trees.refSHA = strings.Repeat("d", 40)
	r.trees.checkHashes = map[string]string{"skills/x/SKILL.md": "sha256:new", "skills/y/SKILL.md": "sha256:same"}

	rep, err := r.e.Check(CheckRequest{Pack: "superpowers", Ref: "v7.0.3"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep.Tag != "v7.0.3" || rep.SHA != strings.Repeat("d", 40) || rep.Pinned {
		t.Errorf("expected the ref and the commit it resolved to, got %+v", rep)
	}
	if !reflect.DeepEqual(rep.Changed, []string{"skills/x/SKILL.md"}) {
		t.Errorf("only the replacement whose upstream hash changed is listed, got %v", rep.Changed)
	}
	if len(r.trees.checks) == 0 {
		t.Fatal("the tree was never built for the check")
	}
	if got := r.trees.checks[0]; got.ref != "v7.0.3" || !reflect.DeepEqual(got.exclude, []string{"a"}) {
		t.Errorf("the check must build as an install would, got %+v", got)
	}
	if r.count("install") != 0 || r.state.updates != 0 || r.count("render") != 0 {
		t.Errorf("check installs nothing and changes no state: %v", r.j.events)
	}
}

func TestCheck_WithoutARefChecksThePin(t *testing.T) {
	r := newRig()

	rep, err := r.e.Check(CheckRequest{Pack: "superpowers"})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Pinned || rep.Tag != "v7.0.2" || rep.SHA != goodSHA {
		t.Errorf("expected the pin, got %+v", rep)
	}
	if len(r.trees.checks) == 0 {
		t.Fatal("the tree was never built for the check")
	}
	if r.trees.checks[0].ref != "" {
		t.Errorf("no ref means the pin, got %q", r.trees.checks[0].ref)
	}
}

func TestCheck_CollectsWhatEachInspectorFoundAndFailsOnAFailingOne(t *testing.T) {
	t.Run("lines are collected in installer order", func(t *testing.T) {
		r := newRig()
		a := &inspectingInstaller{fakeInstaller: r.claude, inspection: Inspection{Lines: []string{"claude plugin validate: passed"}}}
		b := &inspectingInstaller{fakeInstaller: r.pi, inspection: Inspection{Lines: []string{"pi 1.5.0"}}}
		r.e.Installers = []Installer{a, r.oc, b}

		rep, err := r.e.Check(CheckRequest{Pack: "superpowers"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(rep.Findings, []string{"claude plugin validate: passed", "pi 1.5.0"}) {
			t.Errorf("findings %v", rep.Findings)
		}
		if len(a.treeDirs) != 1 || !strings.HasSuffix(a.treeDirs[0], "check-superpowers") {
			t.Errorf("inspectors must be shown the built tree, got %v", a.treeDirs)
		}
	})
	t.Run("a failing inspection fails the check but keeps the lines", func(t *testing.T) {
		r := newRig()
		a := &inspectingInstaller{fakeInstaller: r.claude, inspection: Inspection{
			Lines: []string{"first"}, Err: errors.New("claude plugin validate rejected superpowers"),
		}}
		r.e.Installers = []Installer{a}

		rep, err := r.e.Check(CheckRequest{Pack: "superpowers"})
		if err == nil || !strings.Contains(err.Error(), "rejected superpowers") {
			t.Fatalf("expected the failing finding, got %v", err)
		}
		if !reflect.DeepEqual(rep.Findings, []string{"first"}) {
			t.Errorf("lines found before the failure are still shown, got %v", rep.Findings)
		}
	})
}

func TestCheck_ABuildFailureIsReported(t *testing.T) {
	r := newRig()
	r.trees.checkErr = errors.New("kept skill refers to an excluded one")

	if _, err := r.e.Check(CheckRequest{Pack: "superpowers"}); err == nil || !strings.Contains(err.Error(), "excluded") {
		t.Fatalf("expected the build error, got %v", err)
	}
}

func TestCheck_AnEmbeddedPackHasNoRefToTry(t *testing.T) {
	r := newRig()
	r.e.Manifests = append(r.e.Manifests, Manifest{Name: "devgeta", Source: sourceEmbedded})

	if _, err := r.e.Check(CheckRequest{Pack: "devgeta", Ref: "v1"}); err == nil || !strings.Contains(err.Error(), "devgeta") {
		t.Fatalf("expected an error: an embedded pack ships with the binary and has no tags, got %v", err)
	}
	if _, err := r.e.Check(CheckRequest{Pack: "devgeta"}); err != nil {
		t.Errorf("checking the bundled pack itself is fine: %v", err)
	}
	if _, err := r.e.Check(CheckRequest{Pack: "nope"}); err == nil {
		t.Error("an unknown pack is an error")
	}
}
