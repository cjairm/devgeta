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

// paletteRoles lists every role internal/theme.Palette validates, so a test
// fixture theme file can be written without repeating this 24-line block at
// every call site.
var paletteRoles = []string{
	"background_hard", "background", "background_element", "background_subtle",
	"border", "foreground", "foreground_muted", "foreground_dim", "foreground_subtle",
	"red", "green", "yellow", "blue", "purple", "aqua", "orange",
	"red_dim", "green_dim", "yellow_dim", "blue_dim", "purple_dim", "aqua_dim",
	"diff_added_background", "diff_removed_background",
}

// writeThemeFixture writes a minimal, fully valid theme file under themesDir
// with every role set to hex, so internal/theme.Load accepts it.
func writeThemeFixture(t *testing.T, themesDir, name, hex, neovimModule string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("neovim_module: " + neovimModule + "\ncolors:\n")
	for _, role := range paletteRoles {
		b.WriteString("  " + role + ": \"" + hex + "\"\n")
	}
	path := filepath.Join(themesDir, name+".yaml")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("failed to write theme fixture %s: %v", path, err)
	}
}

// writeNeovimModuleFixture drops a fake Neovim colorscheme module where
// internal/theme's validateNeovimModule expects to find a shipped one.
func writeNeovimModuleFixture(t *testing.T, neovimConfigsDir, module string) {
	t.Helper()
	dir := filepath.Join(neovimConfigsDir, "lua", "devgeta", "themes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, module+".lua")
	if err := os.WriteFile(path, []byte("-- fixture\n"), 0o644); err != nil {
		t.Fatalf("failed to write neovim module fixture %s: %v", path, err)
	}
}

// setupThemeFixture isolates paths.Paths.App.Configs.Themes, .Neovim and
// paths.Paths.Config.Nvim under a fresh temp tree, writes a "default" theme
// fixture with every role set to hex, and restores the originals in
// t.Cleanup. ForceConfigure now resolves its theme through
// theme.CurrentDefinition, which loads and validates a whole theme file - so
// even a single-surface test has to give it one to load.
func setupThemeFixture(t *testing.T, hex string) {
	t.Helper()

	root := t.TempDir()
	themesDir := filepath.Join(root, "themes")
	neovimConfigsDir := filepath.Join(root, "neovim")
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeThemeFixture(t, themesDir, "default", hex, "gruvbox")
	writeNeovimModuleFixture(t, neovimConfigsDir, "gruvbox")

	oldThemes := paths.Paths.App.Configs.Themes
	oldNeovim := paths.Paths.App.Configs.Neovim
	oldNvim := paths.Paths.Config.Nvim
	t.Cleanup(func() {
		paths.Paths.App.Configs.Themes = oldThemes
		paths.Paths.App.Configs.Neovim = oldNeovim
		paths.Paths.Config.Nvim = oldNvim
	})
	paths.Paths.App.Configs.Themes = themesDir
	paths.Paths.App.Configs.Neovim = neovimConfigsDir
	paths.Paths.Config.Nvim = filepath.Join(root, "does-not-exist-nvim")
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
	// The script's first argument is the version to install: the pin.
	if len(execute.Args) != 2 || execute.Args[1] != constants.PinnedVersion.Claude {
		t.Errorf("expected the script to get the pinned version %q, got args %v",
			constants.PinnedVersion.Claude, execute.Args)
	}
}

// TestInstalledIsAtLeastPin covers the check SoftInstall uses to decide
// whether an installed Claude Code is current enough to keep: older or
// unreadable reinstalls, the pin or newer stays.
func TestInstalledIsAtLeastPin(t *testing.T) {
	pin := constants.PinnedVersion.Claude
	cases := []struct {
		name   string
		stdout string
		err    error
		want   bool
	}{
		{"the pin", pin + " (Claude Code)\n", nil, true},
		{"newer", "99.0.0 (Claude Code)\n", nil, true},
		{"older", "1.0.0 (Claude Code)\n", nil, false},
		{"unreadable", "Claude Code\n", nil, false},
		{"command fails", "", errors.New("not found"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mockApp := testutil.NewMockApp()
			mockApp.Base.SetExecCommandResults(commands.ExecCommandResult(tc.stdout, "", tc.err))
			app := &Claude{Cmd: mockApp.Cmd, Base: mockApp.Base}
			if got := app.installedIsAtLeastPin(); got != tc.want {
				t.Errorf("installedIsAtLeastPin() = %v, want %v", got, tc.want)
			}
			calls := mockApp.Base.ExecCommandCalls
			if len(calls) != 1 || calls[0].Command != constants.Claude ||
				len(calls[0].Args) != 1 || calls[0].Args[0] != "--version" {
				t.Errorf("expected one `claude --version` call, got %+v", calls)
			}
		})
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

// TestEmbeddedSettingsTemplate renders the real embedded settings.json.tmpl in
// and asserts the output is valid JSON — the constraint the
// external consumer (Claude Code) imposes on the rendered file (CLAUDE.md §12).
func TestEmbeddedSettingsTemplate(t *testing.T) {
	tmplPath := filepath.Join("..", "..", "..", "configs", "claude", "settings.json.tmpl")

	for _, tt := range []struct {
		name string
	}{
		{"default render"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "settings.json")
			renderData := settingsTemplateData{
				ScratchDir: `"/tmp/placeholder-scratch"`,
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
			// devgeta's own hooks must be present in every rendering.
			for _, hook := range []string{
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
				{"SessionStart", "agent-state.sh start"},
				{"SessionEnd", "agent-state.sh end"},
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
	for _, sub := range []string{"themes"} {
		if err := os.MkdirAll(filepath.Join(appConfigDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(appConfigDir, "settings.json.tmpl"),
		[]byte(`{"theme":"default"}`),
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
		filepath.Join(appConfigDir, "themes", "default.json.tmpl"),
		[]byte(`{"foreground": "{{.Palette.Foreground}}"}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	devgetaConfigDir := filepath.Join(tc.AppDir, "configs", "devgeta")
	if err := os.MkdirAll(devgetaConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(devgetaConfigDir, baseapp.FormatScriptName),
		[]byte(`#!/bin/bash`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	testutil.IsolateXDGDirs(t)

	oldAppConfigsClaude := paths.Paths.App.Configs.Claude
	t.Cleanup(func() { paths.Paths.App.Configs.Claude = oldAppConfigsClaude })
	paths.Paths.App.Configs.Claude = appConfigDir

	oldAppConfigsDevgeta := paths.Paths.App.Configs.Devgeta
	t.Cleanup(func() { paths.Paths.App.Configs.Devgeta = oldAppConfigsDevgeta })
	paths.Paths.App.Configs.Devgeta = devgetaConfigDir

	oldConfigClaude := paths.Paths.Config.Claude
	t.Cleanup(func() { paths.Paths.Config.Claude = oldConfigClaude })
	paths.Paths.Config.Claude = userConfigDir
	setupThemeFixture(t, "#ebdbb2")

	app := &Claude{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	// settings.json rendered from the template; rendered from the template
	rendered, err := os.ReadFile(filepath.Join(userConfigDir, "settings.json"))
	if err != nil {
		t.Errorf("Expected settings.json at %s: %v", userConfigDir, err)
	}
	if string(rendered) != `{"theme":"default"}` {
		t.Errorf("unexpected rendered settings.json: %s", rendered)
	}

	// statusline.sh, format.sh, agent-config-guard.sh and agent-state.sh
	// deployed and executable
	for _, script := range []string{
		"statusline.sh",
		"format.sh",
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

	// theme rendered from the palette, named after the current theme
	themeContent, err := os.ReadFile(filepath.Join(userConfigDir, "themes", "default.json"))
	if err != nil {
		t.Errorf("Expected rendered theme file: %v", err)
	}
	if !strings.Contains(string(themeContent), "#ebdbb2") {
		t.Errorf(
			"expected rendered theme to use the current theme's palette, got: %s",
			themeContent,
		)
	}

	// devgeta ships nothing into the agent's context: no skills, commands,
	// agents or instructions file, and it never touches the user's CLAUDE.md.
	for _, entry := range []string{"skills", "commands", "agents", "DEVGETA.md", "CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(userConfigDir, entry)); !os.IsNotExist(err) {
			t.Errorf("expected ForceConfigure not to create %s, stat err: %v", entry, err)
		}
	}

	testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
	testutil.VerifyNoRealConfigChanges(t)
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

func TestExecuteCommandNeverWaitsOnStdin(t *testing.T) {
	mockApp := testutil.NewMockApp()
	app := &Claude{Cmd: mockApp.Cmd, Base: mockApp.Base}

	if err := app.ExecuteCommand("plugin", "install", "x@y"); err != nil {
		t.Fatalf("ExecuteCommand error: %v", err)
	}
	if last := mockApp.Base.GetLastExecCommandCall(); !last.NoStdin {
		t.Error("a plugin command must run with stdin disconnected so it can't block on a prompt")
	}
}

func TestExecuteCommandOutput(t *testing.T) {
	t.Run("returns stdout", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult(`[{"id":"a@b"}]`, "", nil)
		app := &Claude{Cmd: mockApp.Cmd, Base: mockApp.Base}

		out, err := app.ExecuteCommandOutput("plugin", "list", "--json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != `[{"id":"a@b"}]` {
			t.Errorf("unexpected stdout %q", out)
		}
		last := mockApp.Base.GetLastExecCommandCall()
		if last.Command != constants.Claude || !last.NoStdin {
			t.Errorf("expected claude with NoStdin, got %+v", last)
		}
		testutil.VerifyNoRealConfigChanges(t)
	})

	t.Run("wraps a failure and keeps stderr", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "plugin not found", errors.New("exit 1"))
		app := &Claude{Cmd: mockApp.Cmd, Base: mockApp.Base}

		_, err := app.ExecuteCommandOutput("plugin", "list", "--json")
		if err == nil || !strings.Contains(err.Error(), "plugin not found") {
			t.Fatalf("expected stderr in the error, got %v", err)
		}
	})
}

// A coder's uninstall deletes its config directory, which takes every pack's
// Claude side with it, so the pack records must follow (ADR-0070 §6).
func TestUninstallForgetsAgentPacks(t *testing.T) {
	tc := testutil.SetupCompleteTest(t)
	defer tc.Cleanup()
	testutil.IsolateXDGDirs(t)

	oldClaudeDir, oldPacks := paths.Paths.Config.Claude, paths.Paths.App.AgentPacks
	t.Cleanup(func() {
		paths.Paths.Config.Claude, paths.Paths.App.AgentPacks = oldClaudeDir, oldPacks
	})
	paths.Paths.Config.Claude = filepath.Join(tc.ConfigDir, ".claude")
	paths.Paths.App.AgentPacks = filepath.Join(tc.AppDir, "agent-packs")
	for _, pack := range []string{"ponytail", "superpowers"} {
		if err := os.MkdirAll(filepath.Join(paths.Paths.App.AgentPacks, pack), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatal(err)
	}
	if err := gc.Load(); err != nil {
		t.Fatal(err)
	}
	gc.AgentPacks = map[string]config.AgentPackState{
		"ponytail":    {Coders: map[string]config.AgentPackCoderState{"claude": {Plugin: "ponytail@devgeta"}}},
		"superpowers": {Coders: map[string]config.AgentPackCoderState{"claude": {Plugin: "superpowers@devgeta"}, "pi": {PiPackage: "/p"}}},
	}
	if err := gc.Save(); err != nil {
		t.Fatal(err)
	}

	app := &Claude{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	if err := app.Uninstall(); err != nil {
		t.Fatalf("Uninstall error: %v", err)
	}

	after := &config.GlobalConfig{}
	if err := after.Load(); err != nil {
		t.Fatal(err)
	}
	if _, ok := after.AgentPacks["ponytail"]; ok {
		t.Error("a pack only Claude had should be gone from state")
	}
	if _, ok := after.AgentPacks["superpowers"].Coders["claude"]; ok {
		t.Error("Claude's record should be gone from the other packs")
	}
	if _, ok := after.AgentPacks["superpowers"].Coders["pi"]; !ok {
		t.Error("Pi's record must stay")
	}
	if _, err := os.Stat(filepath.Join(paths.Paths.App.AgentPacks, "ponytail")); !os.IsNotExist(err) {
		t.Errorf("the built tree of the orphaned pack should be deleted, stat err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(paths.Paths.App.AgentPacks, "superpowers")); err != nil {
		t.Errorf("a pack Pi still uses keeps its tree: %v", err)
	}
}

// forceConfigureRig sets up what ForceConfigure needs around a settings
// template the test supplies, and returns the app and the rendered file's path.
func forceConfigureRig(t *testing.T, settingsTmpl string) (*Claude, string, *config.GlobalConfig) {
	t.Helper()
	tc := testutil.SetupCompleteTest(t)
	t.Cleanup(tc.Cleanup)
	testutil.IsolateXDGDirs(t)

	appConfigDir := filepath.Join(tc.AppDir, "configs", "claude")
	devgetaConfigDir := filepath.Join(tc.AppDir, "configs", "devgeta")
	userConfigDir := filepath.Join(tc.ConfigDir, "..", ".claude")
	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(appConfigDir, "settings.json.tmpl"), settingsTmpl)
	for _, s := range []string{"statusline.sh", "format.sh", "agent-config-guard.sh", "agent-state.sh"} {
		write(filepath.Join(appConfigDir, s), "#!/bin/bash")
	}
	write(filepath.Join(appConfigDir, "themes", "default.json.tmpl"), `{"foreground": "{{.Palette.Foreground}}"}`)
	write(filepath.Join(devgetaConfigDir, baseapp.FormatScriptName), "#!/bin/bash")

	oldA, oldD, oldC, oldP := paths.Paths.App.Configs.Claude, paths.Paths.App.Configs.Devgeta,
		paths.Paths.Config.Claude, paths.Paths.App.AgentPacks
	t.Cleanup(func() {
		paths.Paths.App.Configs.Claude, paths.Paths.App.Configs.Devgeta = oldA, oldD
		paths.Paths.Config.Claude, paths.Paths.App.AgentPacks = oldC, oldP
	})
	paths.Paths.App.Configs.Claude = appConfigDir
	paths.Paths.App.Configs.Devgeta = devgetaConfigDir
	paths.Paths.Config.Claude = userConfigDir
	paths.Paths.App.AgentPacks = filepath.Join(tc.AppDir, "agent-packs")
	setupThemeFixture(t, "#ebdbb2")

	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		t.Fatal(err)
	}
	if err := gc.Load(); err != nil {
		t.Fatal(err)
	}
	app := &Claude{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
	return app, filepath.Join(userConfigDir, "settings.json"), gc
}

const packEchoTemplate = `{{.PackMarketplaceName}}|{{.PackMarketplacePath}}|{{range .PackPlugins}}{{.}},{{end}}`

func TestForceConfigure_RendersPackEntriesFromState(t *testing.T) {
	app, settings, gc := forceConfigureRig(t, packEchoTemplate)
	gc.AgentPacks = map[string]config.AgentPackState{
		"superpowers": {Coders: map[string]config.AgentPackCoderState{
			"claude": {Plugin: "superpowers@devgeta"}, "pi": {PiPackage: "/p"},
		}},
		"ponytail": {Coders: map[string]config.AgentPackCoderState{"claude": {Plugin: "ponytail@devgeta"}}},
		// A pack that is only in Pi must not put a Claude plugin in settings.
		"pi-only": {Coders: map[string]config.AgentPackCoderState{"pi": {PiPackage: "/q"}}},
	}
	if err := gc.Save(); err != nil {
		t.Fatal(err)
	}

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	want := `"devgeta"|` + mustJSON(t, paths.Paths.App.AgentPacks) + `|"ponytail@devgeta","superpowers@devgeta",`
	if got := string(mustRead(t, settings)); got != want {
		t.Errorf("settings rendered from state:\n got %s\nwant %s", got, want)
	}
}

func TestForceConfigure_WithoutPacksRendersNoPackData(t *testing.T) {
	app, settings, _ := forceConfigureRig(t, packEchoTemplate)

	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}

	if got := string(mustRead(t, settings)); got != "||" {
		t.Errorf("no packs means no marketplace and no plugins, got %q", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestIsInstalled(t *testing.T) {
	app := &Claude{}
	orig := commands.LookPathFn
	t.Cleanup(func() { commands.LookPathFn = orig })

	commands.LookPathFn = func(string) (string, error) { return "", errors.New("not found") }
	if app.IsInstalled() {
		t.Error("claude missing from PATH is not installed")
	}
	commands.LookPathFn = func(name string) (string, error) { return "/home/u/.local/bin/" + name, nil }
	if !app.IsInstalled() {
		t.Error("claude on PATH is installed")
	}
}
