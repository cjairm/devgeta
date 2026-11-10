// OpenCode terminal-based AI code editor with devgeta integration
//
// OpenCode is an AI-powered code editor that runs in the terminal, providing
// intelligent code completion, refactoring, and assistance. This module provides
// installation and configuration management for OpenCode with devgeta integration.
//
// References:
// - OpenCode Documentation: https://opencode.ai/docs

package opencode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
	"github.com/cjairm/devgeta/pkg/version"
)

var (
	_ apps.App              = (*OpenCode)(nil)
	_ apps.ThemedConfigurer = (*OpenCode)(nil)
)

const DEFAULT_THEME_NAME = "default"

// configTemplateData is opencode.json.tmpl's render data. Like ScratchDirGlob,
// every value is already JSON-encoded: text/template does no escaping.
type configTemplateData struct {
	ScratchDirGlob string
	// Plugins, Instructions and Skills list what agent packs added to
	// OpenCode, in pack-name order. All are empty when no pack is installed in
	// OpenCode.
	Plugins      []string
	Instructions []string
	Skills       []string
}

// addPacks fills Plugins, Instructions and Skills from agent_packs state: what each
// pack recorded for OpenCode, in pack-name order so the output is stable.
// Packs with no OpenCode record add nothing.
func (d *configTemplateData) addPacks(packs map[string]config.AgentPackState) error {
	names := make([]string, 0, len(packs))
	for name := range packs {
		names = append(names, name)
	}
	sort.Strings(names)
	encode := func(values []string, into *[]string, what, pack string) error {
		for _, v := range values {
			encoded, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("failed to encode the %s path of %s: %w", what, pack, err)
			}
			*into = append(*into, string(encoded))
		}
		return nil
	}
	for _, name := range names {
		rec, ok := packs[name].Coders[constants.OpenCode]
		if !ok {
			continue
		}
		if err := encode(rec.OpenCodePlugins, &d.Plugins, "plugin", name); err != nil {
			return err
		}
		if err := encode(rec.OpenCodeInstructions, &d.Instructions, "instructions", name); err != nil {
			return err
		}
		if err := encode(rec.OpenCodeSkills, &d.Skills, "skills", name); err != nil {
			return err
		}
	}
	return nil
}

// renderConfig renders opencode.json from the template, with the agent packs
// in packs. ForceConfigureTheme passes the saved state; the pack installer
// passes the state as it will be once the install is saved.
func (o *OpenCode) renderConfig(packs map[string]config.AgentPackState) error {
	scratchDir, err := paths.EnsureScratchDir()
	if err != nil {
		return fmt.Errorf("failed to ensure scratch dir: %w", err)
	}
	// Permission resources are globs, so the grant needs a "/**" suffix to
	// cover nested paths — unlike Claude's additionalDirectories, which grants
	// a bare directory. Marshal the FULL value (suffix included) in one call:
	// concatenating a literal suffix onto an already-escaped string would
	// corrupt the JSON if the path itself needed any escaping.
	scratchDirGlobJSON, err := json.Marshal(scratchDir + "/**")
	if err != nil {
		return fmt.Errorf("failed to encode scratch dir: %w", err)
	}
	data := configTemplateData{ScratchDirGlob: string(scratchDirGlobJSON)}
	if err := data.addPacks(packs); err != nil {
		return err
	}
	if err := files.GenerateFromTemplate(
		filepath.Join(paths.Paths.App.Configs.OpenCode, fmt.Sprintf("%s.json.tmpl", constants.OpenCode)),
		filepath.Join(paths.Paths.Config.OpenCode, fmt.Sprintf("%s.json", constants.OpenCode)),
		data,
	); err != nil {
		return fmt.Errorf("failed to generate opencode configuration: %w", err)
	}
	return nil
}

// ConfigureWithPacks re-renders opencode.json alone, with packs as the agent
// pack state. It is how `dg agent-pack` adds or removes a pack's plugins and
// instructions: opencode.json is devgeta-rendered, so the packs go in through
// the template and never by editing the JSON. The theme, cli.json and
// devgeta's own plugin folders are left for `dg configure`.
func (o *OpenCode) ConfigureWithPacks(packs map[string]config.AgentPackState) error {
	if err := os.MkdirAll(paths.Paths.Config.OpenCode, 0o755); err != nil {
		return err
	}
	return o.renderConfig(packs)
}

// IsInstalled reports whether opencode is present: devgeta's managed install,
// which is not on PATH until a new shell, or one from another channel.
func (o *OpenCode) IsInstalled() bool {
	if files.FileAlreadyExist(installedBinary()) {
		return true
	}
	_, err := cmd.LookPathFn(constants.OpenCode)
	return err == nil
}

type OpenCode struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func (o *OpenCode) Name() string       { return constants.OpenCode }
func (o *OpenCode) Kind() apps.AppKind { return apps.KindTerminal }

func New() *OpenCode {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &OpenCode{Cmd: osCmd, Base: baseCmd}
}

// openCodeInstallScriptURL is OpenCode 2's official installer, its only
// supported install channel on either platform (ADR-0047; docs/apps/opencode.md). The
// unversioned https://opencode.ai/install now installs only OpenCode 1 and
// refuses a 2.x version.
//
// Install() stages it through cmd.RunInstallScript rather than a raw
// `curl | bash` pipeline: the pipeline's exit status is bash's, not curl's, so
// a 404 or a truncated download pipes an empty script into bash - which exits
// 0 - and a broken download is reported as a successful install. It runs with
// `bash`, never `sh`: the script's shebang is `#!/usr/bin/env bash`, and on
// Debian `sh` is dash.
const openCodeInstallScriptURL = "https://opencode.ai/v2/install"

// Install installs exactly the pinned version (constants.PinnedVersion), so
// what devgeta configures is what was checked against that release.
func (o *OpenCode) Install() error {
	if err := cmd.RunInstallScript(
		o.Base,
		constants.OpenCode,
		openCodeInstallScriptURL,
		"bash",
		"--version", constants.PinnedVersion.OpenCode,
	); err != nil {
		return fmt.Errorf("failed to install opencode: %w", err)
	}
	return nil
}

func (o *OpenCode) ForceInstall() error {
	return baseapp.Reinstall(o.Install, o.Uninstall)
}

// installedBinary is the path the install script writes opencode to. Nothing
// resolves it through PATH: that is the whole point of checking it (see
// SoftInstall).
func installedBinary() string {
	return filepath.Join(paths.Paths.Home.OpenCode, "bin", constants.OpenCode)
}

// SoftInstall installs opencode when it is missing or older than the pin.
//
// The prefix check is not redundant with a PATH lookup: a freshly installed
// ~/.opencode/bin is not on the running process's PATH - nor on the user's,
// until they open a new shell - so a PATH-only check re-runs the installer on
// every `dg install`, which is the Debian idempotency defect ADR-0047 fixes.
//
// An older version (an OpenCode 1 machine above all) counts as missing, since
// devgeta writes a config only the pinned release reads correctly. A newer
// version the user installed is left alone (docs/apps/opencode.md).
//
// The version check applies only to devgeta's managed install. When it is
// absent, baseapp.SoftInstall's PATH fallback still leaves an opencode from
// another channel alone, as before.
func (o *OpenCode) SoftInstall() error {
	if files.FileAlreadyExist(installedBinary()) {
		if o.installedIsAtLeastPin() {
			return nil
		}
		return o.Install()
	}
	return baseapp.SoftInstall(nil, constants.OpenCode, o.Install)
}

// installedIsAtLeastPin runs the installed binary's --version (it prints
// "opencode v2.2.6") and compares it with the pin. Anything it cannot read
// counts as older, so SoftInstall reinstalls rather than trusting it.
func (o *OpenCode) installedIsAtLeastPin() bool {
	stdout, _, err := o.Base.ExecCommand(cmd.CommandParams{
		Command: installedBinary(),
		Args:    []string{"--version"},
		NoStdin: true,
	})
	if err != nil {
		return false
	}
	installed, ok := version.Find(stdout)
	return ok && version.AtLeast(installed, constants.PinnedVersion.OpenCode)
}

func (o *OpenCode) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	// Removing the prefix IS the uninstall - the script puts the binary and
	// everything with it under ~/.opencode, and there is no package-manager
	// entry to remove on either platform. os.RemoveAll rather than a shelled
	// `rm -rf` so pkg/paths' test sandbox contains it, and because RemoveAll
	// deletes a symlinked ~/.opencode itself rather than following it into
	// whatever it points at.
	if err := os.RemoveAll(paths.Paths.Home.OpenCode); err != nil {
		return fmt.Errorf(
			"failed to remove %s: %w",
			paths.Paths.Home.OpenCode,
			err,
		)
	}
	_ = os.RemoveAll(paths.Paths.Config.OpenCode)
	gc.DisableShellFeature(constants.OpenCode)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to regenerate shell config: %w", err)
	}
	gc.RemoveFromInstalled(constants.OpenCode, "package")
	// Uninstall deleted the config dir, so every pack's record of this coder is
	// stale (ADR-0070 §6).
	baseapp.ForgetAgentPackCoder(gc, constants.OpenCode)
	return gc.Save()
}

// ForceConfigure resolves the theme for current_theme (falling back to
// theme.DefaultThemeName when it is empty) and renders with it. `dg theme
// set` does not go through here: it resolves the target theme itself and
// calls ForceConfigureTheme directly, because current_theme is deliberately
// not written yet at that point (docs/plans/cycles/2026-09-14-dg-theme.md
// Step 5) - reading it here would render the theme being replaced.
func (o *OpenCode) ForceConfigure() error {
	def, err := theme.CurrentDefinition()
	if err != nil {
		return fmt.Errorf("failed to resolve current theme: %w", err)
	}
	return o.ForceConfigureTheme(def)
}

func (o *OpenCode) ForceConfigureTheme(def theme.Definition) error {
	if err := clearManagedConfig(paths.Paths.Config.OpenCode); err != nil {
		return err
	}
	// Directory permissions should be 0755 not 0644. Directories need execute
	// permission to be entered.
	if err := os.MkdirAll(paths.Paths.Config.OpenCode, 0o755); err != nil {
		return err
	}
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	palette, err := def.PaletteFor(o.Name())
	if err != nil {
		return fmt.Errorf("failed to resolve theme palette: %w", err)
	}
	// The theme's raw-color block is templated (ADR-0043); the rest of the
	// role-mapping JSON stays static and hand-maintained. The destination
	// file is named after the theme itself, because that is the name cli.json
	// selects below - one template renders whichever theme is current.
	themesDir := filepath.Join(paths.Paths.Config.OpenCode, "themes")
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		return fmt.Errorf("failed to create themes directory: %w", err)
	}
	if err := files.GenerateFromTemplate(
		filepath.Join(paths.Paths.App.Configs.OpenCode, "themes", "default.json.tmpl"),
		filepath.Join(themesDir, def.Name+".json"),
		map[string]any{"Palette": palette},
	); err != nil {
		return fmt.Errorf("failed to render opencode theme: %w", err)
	}
	if err := selectTheme(def.Name); err != nil {
		return err
	}
	// Saved agent pack state is rendered in, so a --force re-render keeps the
	// packs instead of silently dropping them (ADR-0068 §6).
	if err := o.renderConfig(gc.AgentPacks); err != nil {
		return err
	}

	// Each devgeta plugin is a folder under configs/opencode/plugins/
	// (server.js runs in OpenCode's server, tui.js in each terminal client;
	// docs/apps/opencode.md). OpenCode discovers every folder in ~/.config/opencode/plugins/
	// by itself, so nothing registers them. The folders are copied one by one
	// because plugins/ is shared: other tools install their plugins there too.
	for _, name := range devgetaPlugins {
		if err := files.CopyDir(
			filepath.Join(paths.Paths.App.Configs.OpenCode, "plugins", name),
			filepath.Join(paths.Paths.Config.OpenCode, "plugins", name),
		); err != nil {
			return fmt.Errorf("failed to copy opencode plugin %s: %w", name, err)
		}
	}

	if err := baseapp.MaintainScratchDir(); err != nil {
		return fmt.Errorf("failed to maintain scratch dir: %w", err)
	}

	gc.ReconcileShellFeatures()
	gc.AddToInstalled(constants.OpenCode, "package")
	gc.Shell.Opencode = true
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to regenerate shell config: %w", err)
	}
	return nil
}

func (o *OpenCode) SoftConfigure() error {
	markerFile := filepath.Join(
		paths.Paths.Config.OpenCode,
		fmt.Sprintf("%s.json", constants.OpenCode),
	)
	if files.FileAlreadyExist(markerFile) {
		// Config already exists, but ensure shell feature is enabled
		gc := &config.GlobalConfig{}
		if err := gc.Create(); err != nil {
			return fmt.Errorf("failed to create global config: %w", err)
		}
		if err := gc.Load(); err != nil {
			return fmt.Errorf("failed to load global config: %w", err)
		}
		if !gc.Shell.Opencode {
			gc.Shell.Opencode = true
			if err := gc.Save(); err != nil {
				return fmt.Errorf("failed to save global config: %w", err)
			}
			if err := gc.RegenerateShellConfig(); err != nil {
				return fmt.Errorf("failed to regenerate shell config: %w", err)
			}
		}
		return nil
	}
	return o.ForceConfigure()
}

func (o *OpenCode) ExecuteCommand(args ...string) error {
	params := cmd.CommandParams{
		Command: constants.OpenCode,
		Args:    args,
	}
	_, _, err := o.Base.ExecCommand(params)
	if err != nil {
		return fmt.Errorf("opencode command execution failed: %w", err)
	}
	return nil
}

// ExecuteCommandOutput runs opencode and returns its stdout, for commands read
// for their answer (`opencode debug config --standalone`). The managed binary
// is preferred over PATH, because a fresh managed install is not on PATH until
// a new shell, and stdin is disconnected so a query can never wait on a prompt.
//
// Callers that must not talk to the user's shared background service pass
// --standalone themselves: OpenCode 2 otherwise tries that service first.
func (o *OpenCode) ExecuteCommandOutput(args ...string) (string, error) {
	binary := constants.OpenCode
	if files.FileAlreadyExist(installedBinary()) {
		binary = installedBinary()
	}
	stdout, stderr, err := o.Base.ExecCommand(cmd.CommandParams{
		Command: binary,
		Args:    args,
		NoStdin: true,
	})
	if err != nil {
		if stderr != "" {
			return "", fmt.Errorf("opencode command execution failed: %s: %w", strings.TrimSpace(stderr), err)
		}
		return "", fmt.Errorf("opencode command execution failed: %w", err)
	}
	return stdout, nil
}

func (o *OpenCode) Update() error {
	return fmt.Errorf("%w for opencode", apps.ErrUpdateNotSupported)
}

// devgetaPlugins are the plugin folders devgeta ships under
// configs/opencode/plugins/. TestDevgetaPluginsMatchShippedFolders keeps this
// list and that directory identical, so a new plugin cannot ship without
// being deployed and cleaned up.
var devgetaPlugins = []string{"devgeta-agent-config-guard", "devgeta-notify"}

// devgetaManagedEntries is everything devgeta generates inside
// ~/.config/opencode, and therefore everything a --force reconfigure is
// entitled to delete. plugins/ itself is not on it: other tools install their
// plugins there, so only devgeta's own folders inside it are. "plugin"
// (singular) is where devgeta put its OpenCode 1 plugins; nothing else ever
// wrote there, and OpenCode 2 cannot run what is in it (docs/apps/opencode.md).
//
// cli.json is not on it either: it holds the user's TUI preferences, and
// devgeta only sets theme.name inside it (selectTheme).
//
// Keep this in step with what ForceConfigureTheme writes.
func devgetaManagedEntries() []string {
	entries := []string{"opencode.json", "themes", "plugin"}
	for _, name := range devgetaPlugins {
		entries = append(entries, filepath.Join("plugins", name))
	}
	return entries
}

// clearManagedConfig removes devgeta's own generated files from dir, leaving
// everything else untouched.
//
// This deletes by allowlist rather than wiping the directory, because devgeta
// is one of several writers here and by far the least entitled. OpenCode
// keeps its own state here (cli.json, service.json, and for OpenCode 1
// installs node_modules with its package.json and lockfile), alongside
// whatever backups a user made and other tools' plugins. A blanket wipe once
// took all of it, which cost users those plugins.
//
// An allowlist also fails safe: a file devgeta has not heard of survives,
// where the inverse rule (enumerate what to keep) silently destroys anything
// new that OpenCode or an integration starts writing.
func clearManagedConfig(dir string) error {
	for _, entry := range devgetaManagedEntries() {
		if err := os.RemoveAll(filepath.Join(dir, entry)); err != nil {
			return err
		}
	}
	return nil
}

// selectTheme points OpenCode at the theme devgeta just rendered. OpenCode 2
// reads the theme from cli.json's theme.name and ignores a "theme" key in
// opencode.json (docs/apps/opencode.md). cli.json also holds the user's own TUI settings,
// so only that one key is written and everything else in the file is kept.
func selectTheme(name string) error {
	themeName, err := json.Marshal(name)
	if err != nil {
		return fmt.Errorf("failed to encode theme name: %w", err)
	}
	if err := files.MergeJSONSettings(
		filepath.Join(paths.Paths.Config.OpenCode, "cli.json"),
		[]files.JSONSetting{{Section: "theme", Key: "name", Value: themeName}},
		true,
	); err != nil {
		return fmt.Errorf("failed to select opencode theme: %w", err)
	}
	return nil
}
