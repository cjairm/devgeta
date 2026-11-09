package cmd

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/tooling/agentpack"
)

// fakeAgentPackEngine records what the command asked for and answers with
// whatever the test set up. The engine's own behaviour is tested in
// internal/tooling/agentpack; these tests cover the command: what it parses,
// what it passes on, and what it prints.
type fakeAgentPackEngine struct {
	packs []agentpack.Manifest

	installReq   []agentpack.InstallRequest
	installRep   agentpack.Report
	installErr   error
	uninstallReq []agentpack.UninstallRequest
	uninstallRep agentpack.Report
	uninstallErr error
	updateCalls  []string
	updateReps   []agentpack.Report
	updateErr    error
	checkReq     []agentpack.CheckRequest
	checkRep     agentpack.CheckReport
	checkErr     error
	status       []agentpack.PackStatus
	statusErr    error
}

func (f *fakeAgentPackEngine) Install(r agentpack.InstallRequest) (agentpack.Report, error) {
	f.installReq = append(f.installReq, r)
	return f.installRep, f.installErr
}
func (f *fakeAgentPackEngine) Uninstall(r agentpack.UninstallRequest) (agentpack.Report, error) {
	f.uninstallReq = append(f.uninstallReq, r)
	return f.uninstallRep, f.uninstallErr
}
func (f *fakeAgentPackEngine) Update(p string) ([]agentpack.Report, error) {
	f.updateCalls = append(f.updateCalls, p)
	return f.updateReps, f.updateErr
}
func (f *fakeAgentPackEngine) Check(r agentpack.CheckRequest) (agentpack.CheckReport, error) {
	f.checkReq = append(f.checkReq, r)
	return f.checkRep, f.checkErr
}
func (f *fakeAgentPackEngine) Status() ([]agentpack.PackStatus, error) { return f.status, f.statusErr }
func (f *fakeAgentPackEngine) Packs() []agentpack.Manifest             { return f.packs }

type agentPackRig struct {
	engine    *fakeAgentPackEngine
	refreshed int
	order     []string
}

// useFakeAgentPackEngine swaps the engine factory and the config refresh, and
// reports which happened in which order.
func useFakeAgentPackEngine(t *testing.T) *agentPackRig {
	t.Helper()
	rig := &agentPackRig{engine: &fakeAgentPackEngine{}}
	origNew, origRefresh, origLook := newAgentPackEngine, refreshEmbeddedConfigs, commands.LookPathFn
	t.Cleanup(func() {
		newAgentPackEngine, refreshEmbeddedConfigs, commands.LookPathFn = origNew, origRefresh, origLook
	})
	refreshEmbeddedConfigs = func() error {
		rig.refreshed++
		rig.order = append(rig.order, "refresh")
		return nil
	}
	newAgentPackEngine = func() (agentPackEngine, error) {
		rig.order = append(rig.order, "engine")
		return rig.engine, nil
	}
	commands.LookPathFn = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	return rig
}

// runAgentPack runs `agent-pack <args>` on a fresh command tree and returns
// what it printed.
func runAgentPack(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newAgentPackCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func coderOutcome(coder string, done bool, reason string, parts, notes []string) agentpack.CoderOutcome {
	return agentpack.CoderOutcome{Coder: coder, Done: done, Reason: reason, Parts: parts, Notes: notes}
}

// --- registration and help --------------------------------------------------

func TestAgentPack_IsRegisteredWithItsFiveSubcommands(t *testing.T) {
	var found *string
	for _, c := range rootCmd.Commands() {
		if c.Name() == "agent-pack" {
			name := c.Name()
			found = &name
			var subs []string
			for _, s := range c.Commands() {
				subs = append(subs, s.Name())
			}
			want := []string{"check", "install", "list", "uninstall", "update"}
			got := append([]string(nil), subs...)
			sortStrings(got)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("subcommands %v, want %v", got, want)
			}
		}
	}
	if found == nil {
		t.Fatal("agent-pack must be registered on the root command")
	}
}

func sortStrings(s []string) {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func TestAgentPack_HelpDocumentsEveryCommandAndFlag(t *testing.T) {
	root := newAgentPackCmd()
	for _, want := range []string{"list", "install", "uninstall", "update", "check"} {
		found := false
		for _, c := range root.Commands() {
			found = found || c.Name() == want
		}
		if !found {
			t.Errorf("missing subcommand %s", want)
		}
	}
	flags := map[string][]string{
		"install":   {"coder", "exclude"},
		"uninstall": {"coder"},
		"check":     {"ref"},
	}
	for name, want := range flags {
		sub, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range want {
			if sub.Flags().Lookup(f) == nil {
				t.Errorf("%s needs a --%s flag", name, f)
			}
		}
		if strings.TrimSpace(sub.Long) == "" || !strings.Contains(sub.Long, "dg agent-pack "+name) {
			t.Errorf("%s needs a Long description with an example", name)
		}
	}
}

// --- install ----------------------------------------------------------------

func TestAgentPackInstall_PassesThePackCodersAndExcludeOn(t *testing.T) {
	list := func(s ...string) *[]string { return &s }
	cases := []struct {
		name        string
		args        []string
		wantCoders  []string
		wantExclude *[]string
	}{
		{"nothing else", []string{"superpowers"}, nil, nil},
		{"coders, comma separated", []string{"superpowers", "--coder", "claude,pi"}, []string{"claude", "pi"}, nil},
		{"coders, repeated", []string{"superpowers", "--coder", "claude", "--coder", "pi"}, []string{"claude", "pi"}, nil},
		{"a new exclusion list", []string{"superpowers", "--exclude", "a,b"}, nil, list("a", "b")},
		{"an empty exclusion list clears the saved one", []string{"superpowers", "--exclude="}, nil, list()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := useFakeAgentPackEngine(t)

			if _, err := runAgentPack(t, append([]string{"install"}, tc.args...)...); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(rig.engine.installReq) != 1 {
				t.Fatalf("expected one install request, got %d", len(rig.engine.installReq))
			}
			got := rig.engine.installReq[0]
			if got.Pack != "superpowers" || !reflect.DeepEqual(got.Coders, tc.wantCoders) {
				t.Errorf("request %+v, want coders %v", got, tc.wantCoders)
			}
			switch {
			case tc.wantExclude == nil && got.Exclude != nil:
				t.Errorf("no --exclude must leave the saved list alone (nil), got %v", *got.Exclude)
			case tc.wantExclude != nil && got.Exclude == nil:
				t.Errorf("expected exclude %v, got nil", *tc.wantExclude)
			case tc.wantExclude != nil && len(*tc.wantExclude) == 0 && len(*got.Exclude) != 0:
				t.Errorf("--exclude= must clear the list, got %v", *got.Exclude)
			case tc.wantExclude != nil && len(*tc.wantExclude) > 0 && !reflect.DeepEqual(*got.Exclude, *tc.wantExclude):
				t.Errorf("exclude %v, want %v", *got.Exclude, *tc.wantExclude)
			}
		})
	}
}

func TestAgentPackInstall_NeedsExactlyOnePack(t *testing.T) {
	useFakeAgentPackEngine(t)

	for _, args := range [][]string{{"install"}, {"install", "a", "b"}} {
		if _, err := runAgentPack(t, args...); err == nil {
			t.Errorf("%v must be refused", args)
		}
	}
}

func TestAgentPackInstall_RefreshesConfigsBeforeBuildingTheEngine(t *testing.T) {
	rig := useFakeAgentPackEngine(t)

	if _, err := runAgentPack(t, "install", "superpowers"); err != nil {
		t.Fatal(err)
	}
	// The pack definitions are extracted from the binary; an old extraction
	// would install an old pin.
	if !reflect.DeepEqual(rig.order, []string{"refresh", "engine"}) {
		t.Errorf("order %v, want the refresh first", rig.order)
	}
}

func TestAgentPackInstall_ErrorsComeBackUnwrapped(t *testing.T) {
	t.Run("a failed refresh", func(t *testing.T) {
		rig := useFakeAgentPackEngine(t)
		refreshEmbeddedConfigs = func() error { return errors.New("disk full") }
		if _, err := runAgentPack(t, "install", "superpowers"); err == nil || !strings.Contains(err.Error(), "disk full") {
			t.Errorf("got %v", err)
		}
		if len(rig.engine.installReq) != 0 {
			t.Error("nothing is installed when the definitions can't be refreshed")
		}
	})
	t.Run("a failed engine", func(t *testing.T) {
		useFakeAgentPackEngine(t)
		newAgentPackEngine = func() (agentPackEngine, error) { return nil, errors.New("no definitions") }
		if _, err := runAgentPack(t, "install", "superpowers"); err == nil || !strings.Contains(err.Error(), "no definitions") {
			t.Errorf("got %v", err)
		}
	})
	t.Run("a conflict keeps its removal command", func(t *testing.T) {
		rig := useFakeAgentPackEngine(t)
		rig.engine.installErr = &agentpack.ConflictError{
			Pack: "superpowers", Messages: []string{"claude plugin uninstall superpowers@claude-plugins-official -s user"},
		}
		_, err := runAgentPack(t, "install", "superpowers")
		if err == nil || !strings.Contains(err.Error(), "claude plugin uninstall superpowers@claude-plugins-official -s user") {
			t.Errorf("the exact command must reach the user, got %v", err)
		}
	})
}

func TestAgentPackInstall_PrintsWhatHappenedInEachCoder(t *testing.T) {
	rig := useFakeAgentPackEngine(t)
	rig.engine.installRep = agentpack.Report{
		Pack: "superpowers", Version: "v7.0.2", SHA: "22f0b463e7a5e579904dd6211a520321035526f7",
		Outcomes: []agentpack.CoderOutcome{
			coderOutcome("claude", true, "", nil, []string{"Added @DEVGETA.md to CLAUDE.md"}),
			coderOutcome("opencode", true, "", []string{"the OpenCode plugin (no index.js)"}, nil),
			coderOutcome("pi", false, "pi is not installed", nil, nil),
		},
	}

	out, err := runAgentPack(t, "install", "superpowers")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"superpowers v7.0.2 (22f0b46)",
		"claude", "installed",
		"Added @DEVGETA.md to CLAUDE.md",
		"not loaded: the OpenCode plugin (no index.js)",
		"pi", "skipped", "pi is not installed",
		// A skipped coder gets the command that finishes the job.
		"dg agent-pack install superpowers --coder pi",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q, got:\n%s", want, out)
		}
	}
}

func TestAgentPackInstall_NamesMissingRuntimesAndShowsTheRightNotices(t *testing.T) {
	rig := useFakeAgentPackEngine(t)
	rig.engine.packs = []agentpack.Manifest{{
		Name: "ponytail",
		Requires: []agentpack.Requirement{
			{Command: "node", Reason: "the hooks it runs in Claude Code"},
			{Command: "git", Reason: "cloning"},
		},
		Notices: []agentpack.Notice{
			{Coder: "claude", Text: "LEDGER NOTICE"},
			{Coder: "opencode", Text: "SERVICE NOTICE"},
			{Text: "FOR EVERYONE"},
		},
	}}
	rig.engine.installRep = agentpack.Report{
		Pack: "ponytail", Version: "v5.3.3", SHA: strings.Repeat("a", 40),
		Outcomes: []agentpack.CoderOutcome{
			coderOutcome("claude", true, "", nil, nil),
			coderOutcome("opencode", false, "opencode is not installed", nil, nil),
		},
	}
	commands.LookPathFn = func(name string) (string, error) {
		if name == "git" {
			return "/usr/bin/git", nil
		}
		return "", errors.New("not found")
	}

	out, err := runAgentPack(t, "install", "ponytail")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, "node") || !strings.Contains(out, "the hooks it runs in Claude Code") {
		t.Errorf("a missing runtime must be named with its reason, got:\n%s", out)
	}
	if strings.Contains(out, "git is not") || strings.Contains(out, "cloning") {
		t.Errorf("a runtime that is present must not be mentioned, got:\n%s", out)
	}
	if !strings.Contains(out, "LEDGER NOTICE") || !strings.Contains(out, "FOR EVERYONE") {
		t.Errorf("notices for an installed coder and for everyone must show, got:\n%s", out)
	}
	if strings.Contains(out, "SERVICE NOTICE") {
		t.Errorf("a notice for a coder the pack did not go into must not show, got:\n%s", out)
	}
}

func TestAgentPackInstall_StaysQuietAboutRuntimesWhenNothingWasInstalled(t *testing.T) {
	rig := useFakeAgentPackEngine(t)
	rig.engine.packs = []agentpack.Manifest{{
		Name: "ponytail", Requires: []agentpack.Requirement{{Command: "node", Reason: "hooks"}},
		Notices: []agentpack.Notice{{Text: "SHOULD NOT SHOW"}},
	}}
	rig.engine.installRep = agentpack.Report{
		Pack: "ponytail", Outcomes: []agentpack.CoderOutcome{coderOutcome("claude", false, "claude is not installed", nil, nil)},
	}
	commands.LookPathFn = func(string) (string, error) { return "", errors.New("not found") }

	out, err := runAgentPack(t, "install", "ponytail")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "SHOULD NOT SHOW") || strings.Contains(out, "hooks") {
		t.Errorf("nothing was installed, so there is nothing to warn about, got:\n%s", out)
	}
}

// --- uninstall --------------------------------------------------------------

func TestAgentPackUninstall_PassesCodersOnAndPrintsEachOutcome(t *testing.T) {
	rig := useFakeAgentPackEngine(t)
	rig.engine.uninstallRep = agentpack.Report{
		Pack: "devgeta",
		Outcomes: []agentpack.CoderOutcome{
			coderOutcome("claude", true, "", nil, []string{"Removed the @DEVGETA.md line from CLAUDE.md"}),
			coderOutcome("pi", false, "pi is not installed, so its record was kept", nil, nil),
		},
	}

	out, err := runAgentPack(t, "uninstall", "devgeta", "--coder", "claude,pi")
	if err != nil {
		t.Fatal(err)
	}
	if got := rig.engine.uninstallReq; len(got) != 1 || got[0].Pack != "devgeta" || !reflect.DeepEqual(got[0].Coders, []string{"claude", "pi"}) {
		t.Errorf("request %+v", got)
	}
	for _, want := range []string{"claude", "removed", "Removed the @DEVGETA.md line from CLAUDE.md", "pi", "kept"} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q, got:\n%s", want, out)
		}
	}
}

func TestAgentPackUninstall_FailureStillShowsWhatWasDone(t *testing.T) {
	rig := useFakeAgentPackEngine(t)
	rig.engine.uninstallRep = agentpack.Report{
		Pack: "superpowers", Outcomes: []agentpack.CoderOutcome{coderOutcome("claude", true, "", nil, nil)},
	}
	rig.engine.uninstallErr = errors.New("removing superpowers from pi: locked")

	out, err := runAgentPack(t, "uninstall", "superpowers")

	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("expected the failure, got %v", err)
	}
	if !strings.Contains(out, "claude") {
		t.Errorf("what succeeded before the failure is still worth showing, got:\n%s", out)
	}
}

// --- update -----------------------------------------------------------------

func TestAgentPackUpdate_WithAndWithoutAPack(t *testing.T) {
	t.Run("a named pack", func(t *testing.T) {
		rig := useFakeAgentPackEngine(t)
		rig.engine.updateReps = []agentpack.Report{{
			Pack: "superpowers", Version: "v7.0.2", SHA: strings.Repeat("b", 40),
			Outcomes: []agentpack.CoderOutcome{coderOutcome("claude", true, "", nil, nil)},
		}}

		out, err := runAgentPack(t, "update", "superpowers")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(rig.engine.updateCalls, []string{"superpowers"}) {
			t.Errorf("calls %v", rig.engine.updateCalls)
		}
		if !strings.Contains(out, "superpowers v7.0.2") {
			t.Errorf("got:\n%s", out)
		}
	})
	t.Run("every installed pack", func(t *testing.T) {
		rig := useFakeAgentPackEngine(t)
		done := []agentpack.CoderOutcome{coderOutcome("claude", true, "", nil, nil)}
		rig.engine.updateReps = []agentpack.Report{
			{Pack: "ponytail", Version: "v5.3.3", SHA: strings.Repeat("c", 40), Outcomes: done},
			{Pack: "superpowers", Version: "v7.0.2", SHA: strings.Repeat("d", 40), Outcomes: done},
		}

		out, err := runAgentPack(t, "update")
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(rig.engine.updateCalls, []string{""}) {
			t.Errorf("no pack means all: %v", rig.engine.updateCalls)
		}
		if !strings.Contains(out, "ponytail v5.3.3") || !strings.Contains(out, "superpowers v7.0.2") {
			t.Errorf("got:\n%s", out)
		}
	})
	t.Run("nothing installed says how to install", func(t *testing.T) {
		useFakeAgentPackEngine(t)

		out, err := runAgentPack(t, "update")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "No agent packs are installed") || !strings.Contains(out, "dg agent-pack install") {
			t.Errorf("got:\n%s", out)
		}
	})
	t.Run("more than one pack name is refused", func(t *testing.T) {
		useFakeAgentPackEngine(t)
		if _, err := runAgentPack(t, "update", "a", "b"); err == nil {
			t.Error("expected an error")
		}
	})
}

// --- check ------------------------------------------------------------------

func TestAgentPackCheck_PassesTheRefAndPrintsWhatItFound(t *testing.T) {
	rig := useFakeAgentPackEngine(t)
	rig.engine.checkRep = agentpack.CheckReport{
		Pack: "superpowers", Tag: "v7.0.1", SHA: "2ce8aa15d05bd57213e6f746f953aafb7ded7a8b",
		Findings: []string{"claude plugin validate: passed", "pi 1.5.0 (devgeta's Pi support was last checked against 1.5.0)"},
		Changed:  []string{"skills/brainstorming/SKILL.md"},
	}

	out, err := runAgentPack(t, "check", "superpowers", "--ref", "v7.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if got := rig.engine.checkReq; len(got) != 1 || got[0].Pack != "superpowers" || got[0].Ref != "v7.0.1" {
		t.Errorf("request %+v", got)
	}
	for _, want := range []string{
		"superpowers v7.0.1 (2ce8aa1)", "every check",
		"claude plugin validate: passed", "pi 1.5.0",
		"skills/brainstorming/SKILL.md", "re-read",
		"Nothing was installed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q, got:\n%s", want, out)
		}
	}
	if rig.refreshed != 1 {
		t.Error("check needs the current pack definitions too")
	}
}

func TestAgentPackCheck_WithoutARefChecksThePinAndSaysNoReplacementsChanged(t *testing.T) {
	rig := useFakeAgentPackEngine(t)
	rig.engine.checkRep = agentpack.CheckReport{Pack: "ponytail", Tag: "v5.3.3", SHA: strings.Repeat("7", 40), Pinned: true}

	out, err := runAgentPack(t, "check", "ponytail")
	if err != nil {
		t.Fatal(err)
	}
	if got := rig.engine.checkReq[0]; got.Ref != "" {
		t.Errorf("no --ref means the pin, got %q", got.Ref)
	}
	if strings.Contains(out, "re-read") {
		t.Errorf("no changed replacements, nothing to re-read, got:\n%s", out)
	}
}

func TestAgentPackCheck_AFailingCheckShowsFindingsThenFails(t *testing.T) {
	rig := useFakeAgentPackEngine(t)
	rig.engine.checkRep = agentpack.CheckReport{Pack: "superpowers", Tag: "v9", Findings: []string{"claude plugin validate: rejected"}}
	rig.engine.checkErr = errors.New("claude plugin validate rejected superpowers")

	out, err := runAgentPack(t, "check", "superpowers", "--ref", "v9")

	if err == nil || !strings.Contains(err.Error(), "rejected superpowers") {
		t.Fatalf("expected the failure, got %v", err)
	}
	if !strings.Contains(out, "claude plugin validate: rejected") {
		t.Errorf("findings found before the failure must still be shown, got:\n%s", out)
	}
	if strings.Contains(out, "every check") {
		t.Errorf("must not claim success, got:\n%s", out)
	}
}

// --- list -------------------------------------------------------------------

func TestAgentPackList_ShowsPinsVersionsAndEachCoder(t *testing.T) {
	rig := useFakeAgentPackEngine(t)
	rig.engine.status = []agentpack.PackStatus{
		{Pack: "devgeta", Pinned: "bundled", Installed: "bundled", Coders: []agentpack.CoderStatus{
			{Coder: "claude", State: "installed"},
			{Coder: "pi", State: "skipped", Detail: "pi is not installed"},
		}},
		{Pack: "ponytail", Pinned: "v5.3.3", Coders: []agentpack.CoderStatus{
			{Coder: "claude", State: "not installed"},
		}},
		{Pack: "superpowers", Pinned: "v7.0.2", Installed: "v7.0.1", SHA: "2ce8aa15d05bd57213e6f746f953aafb7ded7a8b",
			Coders: []agentpack.CoderStatus{
				{Coder: "claude", State: "installed"},
				{Coder: "pi", State: "installed", Detail: "Pi's settings no longer list /p; run: dg agent-pack install superpowers --coder pi"},
			}},
	}

	out, err := runAgentPack(t, "list")
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"devgeta", "bundled",
		"ponytail", "v5.3.3", "not installed",
		"superpowers", "pinned v7.0.2", "installed v7.0.1", "2ce8aa1",
		"pi is not installed",
		"Pi's settings no longer list /p",
		// An installed version behind the pin says how to catch up.
		"dg agent-pack update superpowers",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output should contain %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "dg agent-pack update devgeta") || strings.Contains(out, "dg agent-pack update ponytail") {
		t.Errorf("only a pack behind its pin gets an update hint, got:\n%s", out)
	}
}

func TestAgentPackList_AStatusErrorIsReturned(t *testing.T) {
	rig := useFakeAgentPackEngine(t)
	rig.engine.statusErr = errors.New("state unreadable")

	if _, err := runAgentPack(t, "list"); err == nil || !strings.Contains(err.Error(), "state unreadable") {
		t.Errorf("got %v", err)
	}
}
