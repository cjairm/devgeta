// Pi terminal-based AI coding agent with devgeta integration
//
// Pi (@earendil-works/pi-coding-agent) is a terminal AI-coding harness. This
// module provides installation and state management for Pi with devgeta
// integration. Configuration is a curated extension set installed through Pi's
// own package manager (ADR-0060) and one devgeta-owned theme file that follows
// `dg theme` (ADR-0061). Skills arrive as agent packs, and Pi joins the coder
// parity wherever it has an equivalent (ADR-0059 §7, ADR-0060 §7-8).
//
// References:
// - Official installer: https://pi.dev/install.sh
// - Package commands: `pi install <source>` / `pi remove <source>` / `pi list`

package pi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
	"github.com/cjairm/devgeta/pkg/utils"
)

var (
	_ apps.App              = (*Pi)(nil)
	_ apps.ThemedConfigurer = (*Pi)(nil)
)

type Pi struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func (p *Pi) Name() string       { return constants.Pi }
func (p *Pi) Kind() apps.AppKind { return apps.KindTerminal }

func New() *Pi {
	return &Pi{Cmd: cmd.NewCommand(), Base: cmd.NewBaseCommand()}
}

// piInstallScriptURL is Pi's official installer, and its only supported
// install channel (ADR-0059).
//
// Like opencode's, it is staged through cmd.RunInstallScript rather than a
// raw `curl | sh` pipeline: a broken download must not be reported as a
// successful install. Unlike opencode's, its shebang is `#!/bin/sh` — POSIX —
// so it runs under `sh` (dash on Debian), not bash.
const piInstallScriptURL = "https://pi.dev/install.sh"

// piExtensions is the curated set Pi is configured with, installed through Pi's
// own package manager (`pi install <npm source>`). Pi owns the versions, the
// npm tree and the settings entries; devgeta only states which sources it
// wants present (ADR-0060). Add an extension here and to the list in
// docs/apps/pi.md.
var piExtensions = []string{
	"npm:pi-subagents",
	"npm:pi-web-access",
	"npm:pi-mcp-adapter",
	"npm:@juicesharp/rpiv-ask-user-question",
	"npm:@juicesharp/rpiv-todo",
	"npm:pi-powerline-footer",
	"npm:pi-stalled-agent-recovery",
	"npm:pi-openrouter-advanced-provider",
	"npm:pi-openrouter-request-details",
	"npm:pi-thinking-reformatter",
	// Last: its native build is the likeliest to fail, and everything before
	// it should already be installed by then (ADR-0063).
	"npm:pi-lens",
}

func (p *Pi) Install() error {
	if err := cmd.RunInstallScript(
		p.Base,
		constants.Pi,
		piInstallScriptURL,
		"sh",
	); err != nil {
		return fmt.Errorf("failed to install pi: %w", err)
	}
	return nil
}

func (p *Pi) ForceInstall() error {
	return baseapp.Reinstall(p.Install, p.Uninstall)
}

// managedInstallMarker is the JSON marker Pi's script writes inside its managed
// install root, last. Its presence is the script's own statement that the
// install is complete.
const managedInstallMarker = "managed-install.json"

// SoftInstall installs pi only when it is not already there. The marker covers
// a copy installed by devgeta or the script even before its bin dir is on the
// running process's PATH; the PATH check covers a `pi` installed by other means.
func (p *Pi) SoftInstall() error {
	return baseapp.SoftInstall(
		func() bool {
			return files.FileAlreadyExist(
				filepath.Join(paths.Paths.Home.PiInstall, managedInstallMarker),
			)
		},
		constants.Pi,
		p.Install,
	)
}

// piAgentDir is Pi's own config home, ~/.pi/agent. Its settings.json records the
// packages `pi install` has added and the active theme.
func piAgentDir() string { return paths.PiAgentDir() }

// readPackageSources returns every source Pi's settings list under "packages",
// not just the curated ones. An entry is a bare source string or an object
// carrying one under "source"; both are read, and anything else in the file is
// left to Pi. A missing settings file means nothing is installed yet.
//
// A local package is stored relative to the agent dir (`pi install
// /abs/path` writes "../data/..."), so a path source is resolved against that
// dir and returned absolute: callers compare it with the path they installed.
// npm:, git: and URL sources are returned as written.
func readPackageSources() ([]string, error) {
	settingsPath := filepath.Join(piAgentDir(), "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read %s: %w", settingsPath, err)
	}
	var settings struct {
		Packages []json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", settingsPath, err)
	}
	var sources []string
	for _, raw := range settings.Packages {
		var source string
		if err := json.Unmarshal(raw, &source); err != nil {
			var entry struct {
				Source string `json:"source"`
			}
			if err := json.Unmarshal(raw, &entry); err != nil || entry.Source == "" {
				continue
			}
			source = entry.Source
		}
		sources = append(sources, resolvePackageSource(source))
	}
	return sources, nil
}

// resolvePackageSource makes a path source absolute and clean. A relative path
// is anchored at the agent dir, where Pi stores it from. Anything that is not a
// path is returned unchanged.
func resolvePackageSource(source string) string {
	switch {
	case source == "." || source == ".." ||
		strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../"):
		return filepath.Clean(filepath.Join(piAgentDir(), source))
	case strings.HasPrefix(source, "~"):
		return filepath.Clean(paths.ExpandHome(source))
	case filepath.IsAbs(source):
		return filepath.Clean(source)
	}
	return source
}

// PackageSources is readPackageSources for callers outside this package, such
// as the agent pack installer.
func (p *Pi) PackageSources() ([]string, error) { return readPackageSources() }

// declaredExtensions returns the curated extensions Pi's settings already list.
// Pi matches npm sources by package name, so a pinned `npm:<pkg>@<version>`
// counts.
func declaredExtensions() (map[string]bool, error) {
	declared := map[string]bool{}
	sources, err := readPackageSources()
	if err != nil {
		return nil, err
	}
	for _, source := range sources {
		for _, name := range piExtensions {
			if source == name || strings.HasPrefix(source, name+"@") {
				declared[name] = true
			}
		}
	}
	return declared, nil
}

// configureExtensions installs the curated extensions Pi's settings do not
// already declare, stopping at the first failure. There is no rollback: the
// extensions are Pi's own state, and re-running installs whatever is missing.
// Updating installed ones is `pi update`'s job (ADR-0060).
func (p *Pi) configureExtensions() error {
	declared, err := declaredExtensions()
	if err != nil {
		return err
	}
	var pending []string
	for _, name := range piExtensions {
		if declared[name] {
			utils.PrintInfo("Pi extension already configured: " + name)
			continue
		}
		pending = append(pending, name)
	}
	for _, source := range pending {
		utils.PrintInfo("Installing Pi extension " + source + "...")
		if err := p.ExecuteCommand("install", source); err != nil {
			return fmt.Errorf("failed to install pi extension %s: %w", source, err)
		}
		utils.PrintSuccess("Installed Pi extension " + source)
	}
	return nil
}

// piThemeName is the one theme devgeta owns in Pi: the file's content follows
// current_theme, its name never changes (ADR-0061 §2). Pi's themes directory is
// the user's too, so devgeta may own exactly this path and never the directory.
const piThemeName = "devgeta"

func themeFile() string {
	return filepath.Join(piAgentDir(), "themes", piThemeName+".json")
}

// ForceConfigureTheme renders the theme file from def and writes nothing else:
// `dg theme set` calls this for every themed app, and Pi's own settings are
// deliberately outside a theme switch (ADR-0061 §3). Pi hot-reloads the active
// theme file, and the write is atomic, so a running Pi restyles without ever
// reading a partial file.
func (p *Pi) ForceConfigureTheme(def theme.Definition) error {
	palette, err := def.PaletteFor(p.Name())
	if err != nil {
		return fmt.Errorf("failed to resolve theme palette: %w", err)
	}
	if err := files.GenerateFromTemplate(
		filepath.Join(paths.Paths.App.Configs.Pi, "themes", piThemeName+".json.tmpl"),
		themeFile(),
		map[string]any{"Palette": palette},
	); err != nil {
		return fmt.Errorf("failed to render pi theme: %w", err)
	}
	return nil
}

// footerThemeFile is where pi-powerline-footer reads its color overrides. Pi
// does not load a directory holding only this file as an extension, so it is
// safe beside the extension's own files (ADR-0062 §2).
func footerThemeFile() string {
	return filepath.Join(piAgentDir(), "extensions", "powerline-footer", "theme.json")
}

// writeFooterOverride installs the static override that maps the footer's
// hard-coded colors to Pi theme role names (ADR-0062 §2). It names roles, never
// colors, so nothing in it follows the palette directly and `dg theme set`
// never touches it. Without force an existing file is the user's and stays.
func writeFooterOverride(force bool) error {
	if !force && files.FileAlreadyExist(footerThemeFile()) {
		return nil
	}
	source := filepath.Join(paths.Paths.App.Configs.Pi, "powerline-footer", "theme.json")
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", source, err)
	}
	if err := files.WriteFileAtomic(footerThemeFile(), data, files.FilePermission); err != nil {
		return fmt.Errorf("failed to write %s: %w", footerThemeFile(), err)
	}
	return nil
}

// piSettingDefaults are the only keys devgeta writes in Pi's settings.json
// (ADR-0061 §3, ADR-0064). Collapsed thinking still shows a "Thinking..." line
// that a click or Ctrl+T expands, so hiding it by default loses nothing.
var piSettingDefaults = []files.JSONSetting{
	{Key: "theme", Value: json.RawMessage(strconv.Quote(piThemeName))},
	{Key: "hideThinkingBlock", Value: json.RawMessage("true")},
}

// applySettingDefaults writes piSettingDefaults into Pi's settings.json,
// keeping Pi's own state (packages above all). Without force a key already
// present is the user's choice and stays.
func applySettingDefaults(force bool) error {
	return files.MergeJSONSettings(
		filepath.Join(piAgentDir(), "settings.json"),
		piSettingDefaults,
		force,
	)
}

// formatExtensionName is the devgeta-owned extension that runs the shared
// format script after each edit (ADR-0063).
const formatExtensionName = "devgeta-format.js"

// devgetaExtensions are the extensions devgeta ships in configs/pi/extensions.
// Unlike the footer override they are devgeta code, not user config, so they
// are rewritten on every configure. devgeta-quiet-bash shows only the command
// of a bash call until ctrl+o expands it. devgeta-notify reports Pi's activity
// to tmux through the shared agent-state.sh.
var devgetaExtensions = []string{formatExtensionName, "devgeta-quiet-bash.js", "devgeta-notify.js"}

func devgetaExtensionFile(name string) string {
	return filepath.Join(piAgentDir(), "extensions", name)
}

func formatExtensionFile() string { return devgetaExtensionFile(formatExtensionName) }

func writeDevgetaExtensions() error {
	for _, name := range devgetaExtensions {
		source := filepath.Join(paths.Paths.App.Configs.Pi, "extensions", name)
		data, err := os.ReadFile(source)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", source, err)
		}
		target := devgetaExtensionFile(name)
		if err := files.WriteFileAtomic(target, data, files.FilePermission); err != nil {
			return fmt.Errorf("failed to write %s: %w", target, err)
		}
	}
	return nil
}

// piLensConfigPath is where pi-lens reads its settings: PI_LENS_CONFIG_PATH if
// set, else ~/.pi-lens/config.json. PI_CODING_AGENT_DIR is deliberately not
// followed (ADR-0059 §5); a relocated Pi would not load our extension either.
func piLensConfigPath() string {
	if override := os.Getenv("PI_LENS_CONFIG_PATH"); override != "" {
		return override
	}
	return filepath.Join(paths.Paths.Home.Root, ".pi-lens", "config.json")
}

// piLensDefaults are the pi-lens settings devgeta sets, as section → key → raw
// JSON value. Formatting and autofix are off because they would fight devgeta's
// Mason formatters (ADR-0063). The diagnostics widget is hidden because the
// findings already reach the model in the tool results; /lens-widget-toggle
// still shows it for one session.
var piLensDefaults = []files.JSONSetting{
	{Section: "format", Key: "enabled", Value: json.RawMessage("false")},
	{Section: "autofix", Key: "enabled", Value: json.RawMessage("false")},
	{Section: "widget", Key: "visible", Value: json.RawMessage("false")},
}

// applyPiLensDefaults merges piLensDefaults into the user's pi-lens config.
// Without force a value the user already set stays.
func applyPiLensDefaults(force bool) error {
	return files.MergeJSONSettings(piLensConfigPath(), piLensDefaults, force)
}

// configure is the body of both configure modes: theme file, Pi setting defaults,
// footer override, format script and extension, pi-lens config, extensions, then registration - last, so a failed step does not mark pi as
// configured. Soft keeps what already exists (an existing theme file, the
// user's own setting values); force rewrites devgeta's own pieces. Extensions are
// Pi's own state: only the missing ones install, and refreshing the rest is
// `pi update`'s job (ADR-0060).
func (p *Pi) configure(force bool) error {
	if force || !files.FileAlreadyExist(themeFile()) {
		def, err := theme.CurrentDefinition()
		if err != nil {
			return fmt.Errorf("failed to resolve current theme: %w", err)
		}
		if err := p.ForceConfigureTheme(def); err != nil {
			return err
		}
	}
	if err := applySettingDefaults(force); err != nil {
		return err
	}
	if err := writeFooterOverride(force); err != nil {
		return err
	}
	if err := baseapp.EnsureFormatScript(); err != nil {
		return err
	}
	if err := baseapp.DeploySharedScript(
		baseapp.AgentStateScriptName,
		paths.Paths.Config.Devgeta,
	); err != nil {
		return err
	}
	if err := writeDevgetaExtensions(); err != nil {
		return err
	}
	if err := applyPiLensDefaults(force); err != nil {
		return err
	}
	if err := p.configureExtensions(); err != nil {
		return err
	}
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AddToInstalled(constants.Pi, "package")
	return gc.Save()
}

func (p *Pi) SoftConfigure() error { return p.configure(false) }

func (p *Pi) ForceConfigure() error { return p.configure(true) }

// Uninstall removes only the managed install root. ~/.pi/agent is Pi's own
// config/session home — sessions, agent state, skills — and must survive
// (ADR-0059 decision 3). os.RemoveAll rather than a shelled `rm -rf` so
// pkg/paths' test sandbox contains it, and because RemoveAll deletes a
// symlinked root itself rather than following it into whatever it points at.
// Installed extensions are Pi's own state and stay put (ADR-0060).
func (p *Pi) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := os.RemoveAll(paths.Paths.Home.PiInstall); err != nil {
		return fmt.Errorf("failed to remove %s: %w", paths.Paths.Home.PiInstall, err)
	}
	// No shell feature was ever registered for pi (ADR-0059 decision 4), so
	// there is nothing to disable or regenerate — only the installed list.
	gc.RemoveFromInstalled(constants.Pi, "package")
	return gc.Save()
}

// managedLauncher is the launcher the official installer writes beside the
// managed root (~/.pi/agent/bin/pi). It exists before the user's PATH picks up
// the new bin dir, so a configure run right after install works without a
// shell reload.
func managedLauncher() string {
	return filepath.Join(piAgentDir(), "bin", constants.Pi)
}

// piExecutable prefers the user's PATH, then the managed launcher.
func piExecutable() (string, error) {
	path, err := cmd.LookPathFn(constants.Pi)
	if err == nil {
		return path, nil
	}
	if launcher := managedLauncher(); files.FileAlreadyExist(launcher) {
		return launcher, nil
	}
	return "", err
}

// ExecuteCommand runs pi, for commands whose effect is the point
// (`pi install <source>`).
func (p *Pi) ExecuteCommand(args ...string) error {
	_, err := p.ExecuteCommandOutput(args...)
	return err
}

// ExecuteCommandOutput runs pi and returns its stdout, for commands read for
// their answer (`pi --version`). Stdin is disconnected: a package command run
// by an installer must fail rather than sit waiting on a prompt nobody can see.
func (p *Pi) ExecuteCommandOutput(args ...string) (string, error) {
	executable, err := piExecutable()
	if err != nil {
		return "", fmt.Errorf(
			"pi executable not found; install it with `dg install --only pi`: %w",
			err,
		)
	}
	stdout, stderr, err := p.Base.ExecCommand(cmd.CommandParams{
		Command: executable,
		Args:    args,
		NoStdin: true,
	})
	if err != nil {
		if stderr != "" {
			return "", fmt.Errorf(
				"pi command execution failed: %s: %w",
				strings.TrimSpace(stderr),
				err,
			)
		}
		return "", fmt.Errorf("pi command execution failed: %w", err)
	}
	return stdout, nil
}

// IsInstalled reports whether pi is present: on PATH, or the managed launcher
// the official installer writes, which exists before PATH picks up its bin dir.
func (p *Pi) IsInstalled() bool {
	_, err := piExecutable()
	return err == nil
}

func (p *Pi) Update() error {
	return fmt.Errorf("%w for pi", apps.ErrUpdateNotSupported)
}
