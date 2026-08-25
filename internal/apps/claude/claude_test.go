package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

func init() {
	testutil.InitLogger()
}

func TestNew(t *testing.T) {
	app := New()
	if app == nil {
		t.Fatal("New() returned nil")
	}
	if app.Cmd == nil {
		t.Error("Expected Cmd to be initialized")
	}
	if app.Base == nil {
		t.Error("Expected Base to be initialized")
	}
}

func TestNameAndKind(t *testing.T) {
	app := &Claude{}
	if app.Name() != constants.Claude {
		t.Errorf("expected Name() %q, got %q", constants.Claude, app.Name())
	}
	if app.Kind() != apps.KindTerminal {
		t.Errorf("expected Kind() KindTerminal, got %v", app.Kind())
	}
}

// TestInstall verifies Install() runs the staged download-then-execute shape
// (cmd.RunInstallScript) rather than the old raw `curl | bash` pipeline: a
// download step through curl with a non-zero Timeout, followed by an
// execute step through bash. Asserting the Timeout is non-zero matters
// because a test that only checks the staged shape would still pass with a
// stalled-connection hang left in place.
func TestInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Claude{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}

	calls := mockApp.Base.ExecCommandCalls
	if len(calls) != 2 {
		t.Fatalf("expected 2 ExecCommand calls (download, execute), got %d", len(calls))
	}
	download, execute := calls[0], calls[1]
	if download.Command != "curl" {
		t.Errorf("expected download step command 'curl', got %q", download.Command)
	}
	if download.Timeout == 0 {
		t.Error("expected download step to carry a non-zero Timeout")
	}
	if execute.Command != "bash" {
		t.Errorf("expected execute step command 'bash', got %q", execute.Command)
	}
	if execute.Timeout == 0 {
		t.Error("expected execute step to carry a non-zero Timeout")
	}
}

// TestInstall_FailedDownloadBlocksInstall asserts a failed (or truncated,
// simulated here as a curl error) download stops before the script ever
// runs, rather than being reported as a successful install — the exact
// defect the staged shape replaces (`bash -c 'false | bash'` exits 0).
func TestInstall_FailedDownloadBlocksInstall(t *testing.T) {
	mockApp := testutil.NewMockApp()
	mockApp.Base.SetExecCommandResults(
		commands.ExecCommandResult("", "", fmt.Errorf("404 not found")),
	)
	app := &Claude{Cmd: mockApp.Cmd, Base: mockApp.Base}

	err := app.Install()
	if err == nil {
		t.Fatal("expected Install to fail when the download step fails")
	}
	if !strings.Contains(err.Error(), "failed to download install script") {
		t.Errorf("expected download-failure error, got: %v", err)
	}
	if got := mockApp.Base.GetExecCommandCallCount(); got != 1 {
		t.Errorf(
			"expected the execute step to be skipped after a failed download, got %d calls",
			got,
		)
	}
}

func TestForceInstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	claudeConfigDir := filepath.Join(tc.ConfigDir, ".claude")
	oldClaudeDir := paths.Paths.Config.Claude
	t.Cleanup(func() { paths.Paths.Config.Claude = oldClaudeDir })
	paths.Paths.Config.Claude = claudeConfigDir

	app := &Claude{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceInstall(); err != nil {
		t.Fatalf("ForceInstall error: %v", err)
	}

	// ForceInstall runs Uninstall (sh rm) then Install (staged curl, then
	// bash) — 3 ExecCommand calls in total.
	calls := tc.MockApp.Base.ExecCommandCalls
	if len(calls) < 3 {
		t.Fatalf("expected at least 3 ExecCommand calls, got %d", len(calls))
	}
	if calls[0].Command != "sh" {
		t.Errorf("expected first command 'sh' (uninstall), got %q", calls[0].Command)
	}
	last := calls[len(calls)-1]
	if last.Command != "bash" {
		t.Errorf("Expected install script executed via 'bash', got %q", last.Command)
	}
}

func TestForceConfigureParts(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	// Stand up an embedded shared source with a skill and a command.
	src := t.TempDir()
	for _, f := range []string{"skills/demo/SKILL.md", "commands/x.md"} {
		p := filepath.Join(src, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldShared := paths.Paths.App.Configs.Shared
	t.Cleanup(func() { paths.Paths.App.Configs.Shared = oldShared })
	paths.Paths.App.Configs.Shared = src

	claudeDir := filepath.Join(tc.ConfigDir, ".claude")
	oldClaude := paths.Paths.Config.Claude
	t.Cleanup(func() { paths.Paths.Config.Claude = oldClaude })
	paths.Paths.Config.Claude = claudeDir

	app := &Claude{}
	if err := app.ForceConfigureParts([]string{"skills"}); err != nil {
		t.Fatalf("ForceConfigureParts error: %v", err)
	}

	// Only the requested part is synced.
	if _, err := os.Stat(filepath.Join(claudeDir, "skills", "demo", "SKILL.md")); err != nil {
		t.Fatalf("expected skills synced: %v", err)
	}
	if _, err := os.Stat(filepath.Join(claudeDir, "commands")); !os.IsNotExist(err) {
		t.Error("commands should not be synced when only skills was requested")
	}
	// The --only path must not write general config.
	if _, err := os.Stat(filepath.Join(claudeDir, "settings.json")); !os.IsNotExist(err) {
		t.Error("ForceConfigureParts should not write settings.json")
	}
}

func TestConfigurableParts_IncludesRtk(t *testing.T) {
	app := &Claude{}
	parts := app.ConfigurableParts()
	if parts[len(parts)-1] != "rtk" {
		t.Errorf("expected rtk as a configurable part, got %v", parts)
	}
	// The shared slice must not be mutated by the append.
	for _, p := range baseapp.SharedConfigParts {
		if p == "rtk" {
			t.Fatal("baseapp.SharedConfigParts was mutated to include rtk")
		}
	}
}

func TestForceConfigureParts_Rtk(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	claudeDir := filepath.Join(tc.ConfigDir, ".claude")
	oldClaude := paths.Paths.Config.Claude
	t.Cleanup(func() { paths.Paths.Config.Claude = oldClaude })
	paths.Paths.Config.Claude = claudeDir

	rtkInitCalled := 0
	app := &Claude{rtkInit: func() error {
		rtkInitCalled++
		return nil
	}}

	if err := app.ForceConfigureParts([]string{"rtk"}); err != nil {
		t.Fatalf("ForceConfigureParts(rtk) error: %v", err)
	}

	if rtkInitCalled != 1 {
		t.Errorf("expected rtk init to run once, got %d", rtkInitCalled)
	}
	// Opt-in recorded so future settings renders keep the hook entry.
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		t.Fatalf("failed to load global config: %v", err)
	}
	if !gc.Integrations.RtkClaudeHook {
		t.Error("expected RtkClaudeHook opt-in to be recorded in global config")
	}
	// A hand-written settings.json must survive --only=rtk untouched.
	if _, err := os.Stat(filepath.Join(claudeDir, "settings.json")); !os.IsNotExist(err) {
		t.Error("ForceConfigureParts(rtk) should not write settings.json")
	}
}

func TestForceConfigureParts_RtkInitFailure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	claudeDir := filepath.Join(tc.ConfigDir, ".claude")
	oldClaude := paths.Paths.Config.Claude
	t.Cleanup(func() { paths.Paths.Config.Claude = oldClaude })
	paths.Paths.Config.Claude = claudeDir

	app := &Claude{rtkInit: func() error { return fmt.Errorf("rtk not found") }}

	err := app.ForceConfigureParts([]string{"rtk"})
	if err == nil {
		t.Fatal("expected error when rtk init fails")
	}
	if !strings.Contains(err.Error(), "dg install --only rtk") {
		t.Errorf("expected install hint in error, got: %v", err)
	}

	// A failed init must not persist the opt-in — otherwise the next
	// settings render would emit a hook entry for an integration that
	// never succeeded. (Create first: the failure path returns before
	// enableRtkHook ever creates the global config file.)
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatal(err)
	}
	if err := gc.Load(); err != nil {
		t.Fatal(err)
	}
	if gc.Integrations.RtkClaudeHook {
		t.Fatal("rtk hook opt-in must remain false when rtk init fails")
	}
}

// TestEmbeddedSettingsTemplate renders the real embedded settings.json.tmpl in
// both opt-in states and asserts the output is valid JSON — the constraint the
// external consumer (Claude Code) imposes on the rendered file (CLAUDE.md §12).
func TestEmbeddedSettingsTemplate(t *testing.T) {
	tmplPath := filepath.Join("..", "..", "..", "configs", "claude", "settings.json.tmpl")

	for _, tt := range []struct {
		name    string
		flags   config.IntegrationsConfig
		wantRtk bool
	}{
		{"hook opted in", config.IntegrationsConfig{RtkClaudeHook: true}, true},
		{"hook not opted in", config.IntegrationsConfig{}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "settings.json")
			renderData := settingsTemplateData{
				IntegrationsConfig: tt.flags,
				ScratchDir:         `"/tmp/placeholder-scratch"`,
			}
			if err := files.GenerateFromTemplate(tmplPath, out, renderData); err != nil {
				t.Fatalf("failed to render template: %v", err)
			}
			data, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid(data) {
				t.Fatalf("rendered settings.json is not valid JSON:\n%s", data)
			}
			gotRtk := strings.Contains(string(data), "rtk hook claude")
			if gotRtk != tt.wantRtk {
				t.Errorf("rtk hook entry present=%v, want %v", gotRtk, tt.wantRtk)
			}
			// devgeta's own hooks must be present in every rendering.
			for _, hook := range []string{
				"task-redirect.sh",
				"secret-guard.sh",
				"suppression-guard.sh",
				"agent-config-guard.sh",
				"format.sh",
				"agent-state.sh",
			} {
				if !strings.Contains(string(data), hook) {
					t.Errorf("rendered settings.json is missing %s", hook)
				}
			}

			// Structural check (not just strings.Contains): Step 0 of the
			// agent-activity-notifications cycle confirmed the exact hook
			// registration shape for agent-state.sh (see ADR-0005 and
			// task-4-brief.md). A substring check alone would pass even if
			// the Notification matcher were missing or wrong, silently
			// reintroducing the bug Step 0 exists to prevent: an unfiltered
			// Notification hook mapping every notification (including idle
			// nags) onto "blocked".
			var rendered map[string]any
			if err := json.Unmarshal(data, &rendered); err != nil {
				t.Fatalf("failed to unmarshal rendered settings.json: %v", err)
			}
			hooks, ok := rendered["hooks"].(map[string]any)
			if !ok {
				t.Fatalf("rendered settings.json has no top-level \"hooks\" object")
			}

			firstHookEntry := func(t *testing.T, event string) map[string]any {
				t.Helper()
				entries, ok := hooks[event].([]any)
				if !ok || len(entries) == 0 {
					t.Fatalf(
						"expected %q to be a non-empty array in hooks, got %v",
						event,
						hooks[event],
					)
				}
				entry, ok := entries[0].(map[string]any)
				if !ok {
					t.Fatalf("expected %q[0] to be an object, got %v", event, entries[0])
				}
				return entry
			}
			commandOf := func(t *testing.T, entry map[string]any) string {
				t.Helper()
				innerHooks, ok := entry["hooks"].([]any)
				if !ok || len(innerHooks) == 0 {
					t.Fatalf("expected entry to have a non-empty \"hooks\" array, got %v", entry)
				}
				inner, ok := innerHooks[0].(map[string]any)
				if !ok {
					t.Fatalf("expected hooks[0] to be an object, got %v", innerHooks[0])
				}
				command, _ := inner["command"].(string)
				return command
			}

			for _, tt := range []struct {
				event       string
				wantCommand string
			}{
				{"Stop", "agent-state.sh idle"},
				{"UserPromptSubmit", "agent-state.sh busy"},
			} {
				entry := firstHookEntry(t, tt.event)
				if _, hasMatcher := entry["matcher"]; hasMatcher {
					t.Errorf(
						"%s hook must not have a matcher key, got %v",
						tt.event,
						entry["matcher"],
					)
				}
				command := commandOf(t, entry)
				if !strings.Contains(command, tt.wantCommand) {
					t.Errorf(
						"%s hook command %q does not contain %q",
						tt.event,
						command,
						tt.wantCommand,
					)
				}
			}

			notificationEntry := firstHookEntry(t, "Notification")
			matcher, _ := notificationEntry["matcher"].(string)
			if matcher != "permission_prompt" {
				t.Errorf(
					"Notification hook matcher = %q, want exactly %q",
					matcher,
					"permission_prompt",
				)
			}
			notificationCommand := commandOf(t, notificationEntry)
			if !strings.Contains(notificationCommand, "agent-state.sh blocked") {
				t.Errorf(
					"Notification hook command %q does not contain %q",
					notificationCommand,
					"agent-state.sh blocked",
				)
			}
		})
	}
}

func TestUninstall(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	claudeConfigDir := filepath.Join(tc.ConfigDir, ".claude")
	if err := os.MkdirAll(claudeConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldClaudeDir := paths.Paths.Config.Claude
	t.Cleanup(func() { paths.Paths.Config.Claude = oldClaudeDir })
	paths.Paths.Config.Claude = claudeConfigDir

	app := &Claude{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}

	// Verify sh rm uninstall was called
	last := tc.MockApp.Base.GetLastExecCommandCall()
	if last == nil || last.Command != "sh" {
		t.Fatalf("expected sh uninstall command, got %v", last)
	}
	if len(last.Args) < 2 ||
		last.Args[1] != "rm -f ~/.local/bin/claude && rm -rf ~/.local/share/claude" {
		t.Errorf("expected sh -c rm uninstall command, got args %v", last.Args)
	}

	// Config dir should be removed
	if _, err := os.Stat(claudeConfigDir); err == nil {
		t.Error("expected claude config dir to be removed")
	}
}

func TestUpdate(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Claude{Cmd: mockApp.Cmd, Base: mockApp.Base}

	err := app.Update()
	if err == nil {
		t.Fatal("Expected Update to return error")
	}
	if !errors.Is(err, apps.ErrUpdateNotSupported) {
		t.Errorf("expected ErrUpdateNotSupported, got: %v", err)
	}

	testutil.VerifyNoRealCommands(t, mockApp.Base)
}

func TestForceConfigure(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	appConfigDir := filepath.Join(tc.AppDir, "configs", "claude")
	userConfigDir := filepath.Join(tc.ConfigDir, "..", ".claude")

	// Create source structure
	for _, sub := range []string{"themes", "lib"} {
		if err := os.MkdirAll(filepath.Join(appConfigDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "settings.json.tmpl"),
		[]byte(`{"theme":"default"{{if .RtkClaudeHook}},"rtk":true{{end}}}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "statusline.sh"),
		[]byte(`#!/bin/bash`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "format.sh"),
		[]byte(`#!/bin/bash`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "task-redirect.sh"),
		[]byte(`#!/bin/bash`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "secret-guard.sh"),
		[]byte(`#!/bin/bash`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "suppression-guard.sh"),
		[]byte(`#!/bin/bash`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "agent-config-guard.sh"),
		[]byte(`#!/bin/bash`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "agent-state.sh"),
		[]byte(`#!/bin/bash`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "themes", "default.json"),
		[]byte(`{}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "lib", "segments.sh"),
		[]byte(`# test fixture`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	// Create shared dirs
	sharedDir := filepath.Join(tc.AppDir, "configs", "shared")
	for _, sub := range []string{"skills", "commands", "agents"} {
		if err := os.MkdirAll(filepath.Join(sharedDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	testutil.IsolateXDGDirs(t)

	oldAppConfigsClaude := paths.Paths.App.Configs.Claude
	t.Cleanup(func() { paths.Paths.App.Configs.Claude = oldAppConfigsClaude })
	paths.Paths.App.Configs.Claude = appConfigDir

	oldAppConfigsShared := paths.Paths.App.Configs.Shared
	t.Cleanup(func() { paths.Paths.App.Configs.Shared = oldAppConfigsShared })
	paths.Paths.App.Configs.Shared = sharedDir

	oldConfigClaude := paths.Paths.Config.Claude
	t.Cleanup(func() { paths.Paths.Config.Claude = oldConfigClaude })
	paths.Paths.Config.Claude = userConfigDir

	app := &Claude{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	// settings.json rendered from the template; opt-in flag unset → no rtk entry
	rendered, err := os.ReadFile(filepath.Join(userConfigDir, "settings.json"))
	if err != nil {
		t.Errorf("Expected settings.json at %s: %v", userConfigDir, err)
	}
	if string(rendered) != `{"theme":"default"}` {
		t.Errorf("unexpected rendered settings.json: %s", rendered)
	}

	// statusline.sh, format.sh, task-redirect.sh, secret-guard.sh,
	// suppression-guard.sh, agent-config-guard.sh, and agent-state.sh
	// deployed and executable
	for _, script := range []string{
		"statusline.sh",
		"format.sh",
		"task-redirect.sh",
		"secret-guard.sh",
		"suppression-guard.sh",
		"agent-config-guard.sh",
		"agent-state.sh",
	} {
		info, err := os.Stat(filepath.Join(userConfigDir, script))
		if err != nil {
			t.Fatalf("Expected %s: %v", script, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Errorf("Expected %s to be executable", script)
		}
	}

	// themes deployed
	if _, err := os.Stat(filepath.Join(userConfigDir, "themes")); err != nil {
		t.Errorf("Expected themes dir: %v", err)
	}

	// hook lib (sourced by task-redirect.sh/secret-guard.sh/suppression-guard.sh) deployed
	if _, err := os.Stat(filepath.Join(userConfigDir, "lib", "segments.sh")); err != nil {
		t.Errorf("Expected lib/segments.sh: %v", err)
	}

	// shared dirs deployed
	for _, dir := range []string{"skills", "commands", "agents"} {
		if _, err := os.Stat(filepath.Join(userConfigDir, dir)); err != nil {
			t.Errorf("Expected %s dir: %v", dir, err)
		}
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	testutil.VerifyNoRealConfigChanges(t)
}

func TestSoftConfigure_AlreadyConfigured(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()

	testutil.IsolateXDGDirs(t)

	userConfigDir := filepath.Join(tc.ConfigDir, "..", ".claude")
	oldConfigClaude := paths.Paths.Config.Claude
	t.Cleanup(func() { paths.Paths.Config.Claude = oldConfigClaude })
	paths.Paths.Config.Claude = userConfigDir

	// Pre-create marker file so SoftConfigure skips
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(userConfigDir, "settings.json"),
		[]byte(`{}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	app := &Claude{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure error: %v", err)
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
}

func TestExecuteCommand(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Claude{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.ExecuteCommand("--version"); err != nil {
		t.Fatalf("ExecuteCommand error: %v", err)
	}

	last := mockApp.Base.GetLastExecCommandCall()
	if last == nil || last.Command != constants.Claude {
		t.Errorf("Expected command %q, got %v", constants.Claude, last)
	}
	if len(last.Args) == 0 || last.Args[0] != "--version" {
		t.Errorf("Expected args [--version], got %v", last.Args)
	}
}

func TestForceConfigureParts_RtkRefusesRealExecInTests(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	claudeDir := filepath.Join(tc.ConfigDir, ".claude")
	oldClaude := paths.Paths.Config.Claude
	t.Cleanup(func() { paths.Paths.Config.Claude = oldClaude })
	paths.Paths.Config.Claude = claudeDir

	// No rtkInit injected: the guard must refuse instead of executing rtk.
	app := &Claude{}
	err := app.ForceConfigureParts([]string{"rtk"})
	if err == nil || !strings.Contains(err.Error(), "refusing to run real") {
		t.Fatalf("expected test-guard refusal, got: %v", err)
	}
}
