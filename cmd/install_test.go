package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/internal/tooling/agentpack"
)

func init() { testutil.InitLogger() }

// --- stopIfInterrupted (task 14: install loops must stop on Ctrl-C) ---

// TestStopIfInterrupted_NotInterrupted covers run()'s between-category guard
// when nothing has cancelled the root context: it must not report a stop, so
// the next category actually starts.
func TestStopIfInterrupted_NotInterrupted(t *testing.T) {
	t.Cleanup(func() { commands.SetRootContext(context.Background()) })

	if stopIfInterrupted("languages") {
		t.Error(
			"expected stopIfInterrupted to report false against the default, uncancelled root context",
		)
	}
}

// TestStopIfInterrupted_Interrupted covers run()'s between-category guard
// once a prior SIGINT/SIGTERM already cancelled the root context: it must
// report a stop so run() skips starting the next category instead of
// grinding through its loop against an already-dead context.
func TestStopIfInterrupted_Interrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	commands.SetRootContext(ctx)
	t.Cleanup(func() { commands.SetRootContext(context.Background()) })

	if !stopIfInterrupted("databases") {
		t.Error("expected stopIfInterrupted to report true once the root context is cancelled")
	}
}

// --- helpers ---

func assertRunsOnly(t *testing.T, cfg *installConfig, categories ...string) {
	t.Helper()
	want := make(map[string]bool, len(categories))
	for _, c := range categories {
		want[c] = true
	}
	got := map[string]bool{
		"terminal":  cfg.runTerminal,
		"languages": cfg.runLanguages,
		"databases": cfg.runDatabases,
		"desktop":   cfg.runDesktop,
		// Agent packs are a category too (ADR-0068 §8): they run by default,
		// and only when named by --only otherwise.
		"agent-packs": cfg.runAgentPacks,
	}
	for cat, shouldRun := range got {
		if shouldRun != want[cat] {
			if shouldRun {
				t.Errorf("expected category %q NOT to run", cat)
			} else {
				t.Errorf("expected category %q to run", cat)
			}
		}
	}
}

// --- category-level tests (existing behavior) ---

func TestParseFlags_NoFlags_RunsAll(t *testing.T) {
	cfg, err := parseInstallFlags(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal", "languages", "databases", "desktop", "agent-packs")
	if cfg.terminalAppFilter != nil {
		t.Error("expected terminalAppFilter nil with no flags")
	}
	if cfg.desktopAppFilter != nil {
		t.Error("expected desktopAppFilter nil with no flags")
	}
}

func TestParseFlags_OnlyTerminal(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"terminal"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal")
	if cfg.terminalAppFilter != nil {
		t.Error("expected terminalAppFilter nil for category-only flag")
	}
}

func TestParseFlags_SkipDatabases(t *testing.T) {
	cfg, err := parseInstallFlags(nil, []string{"databases"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal", "languages", "desktop", "agent-packs")
}

func TestParseFlags_OnlyDesktop(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"desktop"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "desktop")
	if cfg.desktopAppFilter != nil {
		t.Error("expected desktopAppFilter nil for category-only flag")
	}
}

// --- app-level targeting ---

func TestParseFlags_OnlyNeovim_RunsTerminal(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"neovim"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal")
	if !cfg.terminalAppFilter["neovim"] {
		t.Error("expected terminalAppFilter to include neovim")
	}
	if len(cfg.terminalAppFilter) != 1 {
		t.Errorf("expected terminalAppFilter length 1, got %d", len(cfg.terminalAppFilter))
	}
}

func TestParseFlags_OnlyDocker_RunsDesktop(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"docker"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "desktop")
	if !cfg.desktopAppFilter["docker"] {
		t.Error("expected desktopAppFilter to include docker")
	}
}

func TestParseFlags_OnlyAlacritty_RunsDesktop(t *testing.T) {
	// alacritty is KindTerminal but installed by desktop coordinator
	cfg, err := parseInstallFlags([]string{"alacritty"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "desktop")
	if !cfg.desktopAppFilter["alacritty"] {
		t.Error("expected desktopAppFilter to include alacritty")
	}
}

func TestParseFlags_SkipGit_TerminalRunsWithSkipFilter(t *testing.T) {
	cfg, err := parseInstallFlags(nil, []string{"git"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal", "languages", "databases", "desktop", "agent-packs")
	if cfg.terminalAppFilter != nil {
		t.Error("expected terminalAppFilter nil when only skip flags given")
	}
	if !cfg.terminalSkipFilter["git"] {
		t.Error("expected terminalSkipFilter to include git")
	}
}

// --- mixed category + app ---

func TestParseFlags_OnlyTerminalSkipNeovim(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"terminal"}, []string{"neovim"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal")
	// Category selected → full install (no appFilter), but neovim in skipFilter
	if cfg.terminalAppFilter != nil {
		t.Error("expected terminalAppFilter nil when category is in only-set")
	}
	if !cfg.terminalSkipFilter["neovim"] {
		t.Error("expected terminalSkipFilter to include neovim")
	}
}

func TestParseFlags_OnlyNeovimAndDocker(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"neovim", "docker"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "terminal", "desktop")
	if !cfg.terminalAppFilter["neovim"] {
		t.Error("expected terminalAppFilter to include neovim")
	}
	if !cfg.desktopAppFilter["docker"] {
		t.Error("expected desktopAppFilter to include docker")
	}
}

func TestParseFlags_OnlyNeovimSkipNeovim(t *testing.T) {
	// Skip wins over only: neovim in both sets → filtered out of appFilter
	cfg, err := parseInstallFlags([]string{"neovim"}, []string{"neovim"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// terminal would "run" but appFilter is empty because neovim was excluded from it
	// hasAppsForCoordinator checks onlyAppSet (before skip), so terminal still runs
	if cfg.terminalAppFilter == nil && cfg.runTerminal {
		// appFilter is nil or empty - neovim was filtered out
		if cfg.terminalAppFilter != nil && cfg.terminalAppFilter["neovim"] {
			t.Error("expected neovim NOT in terminalAppFilter when also in skip")
		}
	}
}

// --- validation ---

func TestParseFlags_UnknownOnly_ReturnsError(t *testing.T) {
	_, err := parseInstallFlags([]string{"bogus"}, nil)
	if err == nil {
		t.Fatal("expected error for unknown --only value")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("expected error to mention %q, got: %v", "bogus", err)
	}
	if !strings.Contains(err.Error(), "Valid categories") {
		t.Errorf("expected error to list valid categories, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Valid apps") {
		t.Errorf("expected error to list valid apps, got: %v", err)
	}
}

func TestParseFlags_UnknownSkip_ReturnsError(t *testing.T) {
	_, err := parseInstallFlags(nil, []string{"notanapp"})
	if err == nil {
		t.Fatal("expected error for unknown --skip value")
	}
	if !strings.Contains(err.Error(), "notanapp") {
		t.Errorf("expected error to mention %q, got: %v", "notanapp", err)
	}
}

func TestParseFlags_Devgeta_IsNotTargetable(t *testing.T) {
	// devgeta maps to "" coordinator; it's not a valid --only or --skip target
	_, err := parseInstallFlags([]string{"devgeta"}, nil)
	if err == nil {
		t.Fatal("expected error: devgeta should not be targetable via --only")
	}
}

func TestParseFlags_AllKnownApps_Valid(t *testing.T) {
	for appName, coord := range appToCoordinator {
		if coord == "" {
			continue // devgeta not targetable
		}
		t.Run(appName, func(t *testing.T) {
			cfg, err := parseInstallFlags([]string{appName}, nil)
			if err != nil {
				t.Fatalf("--only %q returned unexpected error: %v", appName, err)
			}
			if cfg == nil {
				t.Fatalf("--only %q returned nil config", appName)
			}
		})
	}
}

// TestInstall_CallsRecoverInterruptedFn asserts run() sweeps for a crashed
// `dg theme set`'s leftover backups before any category's SoftConfigure
// writes onto the same paths (cycle doc Step 5), mirroring
// TestConfigure_CallsRecoverInterruptedFn. run() has no mocking harness for
// the install flow itself (real OS/package-manager calls), so this proves
// call order the same way that gap is closed elsewhere: rig the sweep to
// fail and assert run() returns that error immediately, before it reaches
// any of its own real work (which would otherwise fail differently in a
// test environment).
func TestInstall_CallsRecoverInterruptedFn(t *testing.T) {
	origRecover := recoverInterruptedFn
	t.Cleanup(func() { recoverInterruptedFn = origRecover })

	calls := 0
	sentinel := fmt.Errorf("sentinel sweep failure")
	recoverInterruptedFn = func() (string, error) {
		calls++
		return "", sentinel
	}

	err := run(installCmd, nil)
	if err == nil {
		t.Fatal("expected run() to return the sweep's error")
	}
	if !strings.Contains(err.Error(), sentinel.Error()) {
		t.Errorf("expected run()'s error to wrap the sweep failure, got: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected recoverInterruptedFn called once, got %d", calls)
	}
}

// --- agent packs (ADR-0068 §8) ---

func TestParseFlags_AgentPacks(t *testing.T) {
	cases := []struct {
		name         string
		only, skip   []string
		wantRun      bool
		wantExplicit bool
	}{
		{"default: runs, into the coders this run installed", nil, nil, true, false},
		{"--skip agent-packs installs none", nil, []string{"agent-packs"}, false, false},
		{"--only agent-packs is an explicit request for every configured coder", []string{"agent-packs"}, nil, true, true},
		{"--only terminal did not ask for packs", []string{"terminal"}, nil, false, false},
		{"--only claude did not ask for packs", []string{"claude"}, nil, false, false},
		{"--only terminal,agent-packs runs them after the fresh install", []string{"terminal", "agent-packs"}, nil, true, true},
		{"--skip wins over --only", []string{"agent-packs"}, []string{"agent-packs"}, false, false},
		{"skipping something else leaves packs on", nil, []string{"desktop"}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parseInstallFlags(tc.only, tc.skip)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.runAgentPacks != tc.wantRun {
				t.Errorf("runAgentPacks = %v, want %v", cfg.runAgentPacks, tc.wantRun)
			}
			if cfg.agentPacksExplicit != tc.wantExplicit {
				t.Errorf("agentPacksExplicit = %v, want %v", cfg.agentPacksExplicit, tc.wantExplicit)
			}
		})
	}
}

func TestParseFlags_OnlyAgentPacksRunsNothingElse(t *testing.T) {
	cfg, err := parseInstallFlags([]string{"agent-packs"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRunsOnly(t, cfg, "agent-packs")
}

func TestParseFlags_ErrorsListAgentPacksAsACategory(t *testing.T) {
	_, err := parseInstallFlags([]string{"bogus"}, nil)
	if err == nil || !strings.Contains(err.Error(), "agent-packs") {
		t.Errorf("the valid categories must include agent-packs, got: %v", err)
	}
}

func TestInstallCommandDocumentsAgentPacks(t *testing.T) {
	for _, want := range []string{"agent-packs", "dg agent-pack"} {
		if !strings.Contains(installCmd.Long, want) {
			t.Errorf("dg install --help must mention %q:\n%s", want, installCmd.Long)
		}
	}
}

func installedPacks(f *fakeAgentPackEngine) []string {
	var names []string
	for _, r := range f.installReq {
		names = append(names, r.Pack)
	}
	return names
}

func TestInstallDefaultAgentPacks_GoesOnlyIntoTheCodersThisRunInstalled(t *testing.T) {
	engine := &fakeAgentPackEngine{}
	var out bytes.Buffer

	installDefaultAgentPacks(&out, engine, agentPackPlan{fresh: []string{"opencode"}, already: []string{"claude"}})

	if got := installedPacks(engine); !reflect.DeepEqual(got, []string{"devgeta", "superpowers", "ponytail"}) {
		t.Errorf("packs installed %v, want the three defaults in order", got)
	}
	for _, r := range engine.installReq {
		if !reflect.DeepEqual(r.Coders, []string{"opencode"}) {
			t.Errorf("%s went to %v; only the coder this run installed may get it", r.Pack, r.Coders)
		}
		if r.Exclude != nil {
			t.Errorf("a default install leaves the saved exclusions alone, got %v", *r.Exclude)
		}
	}
	text := out.String()
	for _, pack := range []string{"devgeta", "superpowers", "ponytail"} {
		want := "dg agent-pack install " + pack + " --coder claude"
		if !strings.Contains(text, want) {
			t.Errorf("the coder that was already there needs its command, missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "--coder opencode") {
		t.Errorf("opencode got its packs; no command needed, got:\n%s", text)
	}
}

func TestInstallDefaultAgentPacks_ExplicitRequestTargetsEveryConfiguredCoder(t *testing.T) {
	engine := &fakeAgentPackEngine{}
	var out bytes.Buffer

	installDefaultAgentPacks(&out, engine, agentPackPlan{explicit: true, already: []string{"claude"}})

	if got := installedPacks(engine); len(got) != 3 {
		t.Fatalf("expected the three default packs, got %v", got)
	}
	for _, r := range engine.installReq {
		if r.Coders != nil {
			t.Errorf("--only agent-packs means every configured coder (nil), %s got %v", r.Pack, r.Coders)
		}
	}
	if strings.Contains(out.String(), "--coder claude") {
		t.Errorf("an explicit request already covered claude; no hint needed, got:\n%s", out.String())
	}
}

func TestInstallDefaultAgentPacks_NoFreshCoderMeansNoPacksAndSaysHow(t *testing.T) {
	engine := &fakeAgentPackEngine{}
	var out bytes.Buffer

	installDefaultAgentPacks(&out, engine, agentPackPlan{already: []string{"claude", "git"}})

	if len(engine.installReq) != 0 {
		t.Errorf("nothing was freshly installed, so nothing may be installed, got %v", installedPacks(engine))
	}
	text := out.String()
	if !strings.Contains(text, "No coder was installed in this run") {
		t.Errorf("expected the reason, got:\n%s", text)
	}
	if !strings.Contains(text, "dg agent-pack install superpowers --coder claude") {
		t.Errorf("the coder that was already there needs its command, got:\n%s", text)
	}
	if strings.Contains(text, "--coder git") {
		t.Errorf("git is not a coder, got:\n%s", text)
	}
}

func TestInstallDefaultAgentPacks_NothingAtAllStillSaysHowToInstallLater(t *testing.T) {
	engine := &fakeAgentPackEngine{}
	var out bytes.Buffer

	installDefaultAgentPacks(&out, engine, agentPackPlan{})

	if len(engine.installReq) != 0 || !strings.Contains(out.String(), "dg agent-pack install") {
		t.Errorf("expected a pointer to dg agent-pack install, got:\n%s", out.String())
	}
}

func TestInstallDefaultAgentPacks_OnePackFailingDoesNotStopTheOthers(t *testing.T) {
	engine := &fakeFailingPack{fakeAgentPackEngine: &fakeAgentPackEngine{}, failPack: "superpowers"}
	var out bytes.Buffer

	installDefaultAgentPacks(&out, engine, agentPackPlan{fresh: []string{"claude"}})

	if got := installedPacks(engine.fakeAgentPackEngine); !reflect.DeepEqual(got, []string{"devgeta", "superpowers", "ponytail"}) {
		t.Errorf("every pack must be tried, got %v", got)
	}
	text := out.String()
	if !strings.Contains(text, "superpowers") || !strings.Contains(text, "network down") {
		t.Errorf("the failure must be reported with its cause, got:\n%s", text)
	}
	if !strings.Contains(text, "dg agent-pack install superpowers") {
		t.Errorf("a pack that failed is not retried; the command to run must be printed, got:\n%s", text)
	}
}

// fakeFailingPack fails one pack's install and records every request.
type fakeFailingPack struct {
	*fakeAgentPackEngine
	failPack string
}

func (f *fakeFailingPack) Install(r agentpack.InstallRequest) (agentpack.Report, error) {
	rep, _ := f.fakeAgentPackEngine.Install(r)
	if r.Pack == f.failPack {
		return rep, errors.New("network down")
	}
	return rep, nil
}

func TestInstallDefaultAgentPacks_AConflictShowsTheRemovalCommand(t *testing.T) {
	engine := &fakeFailingPack{fakeAgentPackEngine: &fakeAgentPackEngine{}}
	conflicting := &fakeConflictPack{fakeAgentPackEngine: engine.fakeAgentPackEngine}
	var out bytes.Buffer

	installDefaultAgentPacks(&out, conflicting, agentPackPlan{fresh: []string{"claude"}})

	if !strings.Contains(out.String(), "claude plugin uninstall superpowers@claude-plugins-official -s user") {
		t.Errorf("the exact removal command must reach the user, got:\n%s", out.String())
	}
}

type fakeConflictPack struct{ *fakeAgentPackEngine }

func (f *fakeConflictPack) Install(r agentpack.InstallRequest) (agentpack.Report, error) {
	f.fakeAgentPackEngine.Install(r)
	if r.Pack == "superpowers" {
		return agentpack.Report{}, &agentpack.ConflictError{
			Pack: "superpowers", Messages: []string{"claude plugin uninstall superpowers@claude-plugins-official -s user"},
		}
	}
	return agentpack.Report{}, nil
}

func TestInstallDefaultAgentPacks_ShowsWhatEachPackDid(t *testing.T) {
	engine := &fakeAgentPackEngine{}
	engine.installRep = agentpack.Report{
		Pack: "devgeta", Version: "bundled",
		Outcomes: []agentpack.CoderOutcome{{Coder: "claude", Done: true}},
	}
	var out bytes.Buffer

	installDefaultAgentPacks(&out, engine, agentPackPlan{fresh: []string{"claude"}})

	if !strings.Contains(out.String(), "devgeta bundled") {
		t.Errorf("each pack's report should be shown, got:\n%s", out.String())
	}
}
