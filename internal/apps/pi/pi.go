// Pi terminal-based AI coding agent with devgeta integration
//
// Pi (@earendil-works/pi-coding-agent) is a terminal AI-coding harness. This
// module provides installation and state management for Pi with devgeta
// integration. Configuration is a curated extension set installed through Pi's
// own package manager (ADR-0060) and one devgeta-owned theme file that follows
// `dg theme` (ADR-0061); skills and two-agent parity stay out of scope
// (ADR-0059).
//
// References:
// - Official installer: https://pi.dev/install.sh
// - Package commands: `pi install <source>` / `pi remove <source>` / `pi list`

package pi

import (
	"bytes"
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

// declaredExtensions returns the curated extensions Pi's settings already list.
// Pi matches npm sources by package name, so a pinned `npm:<pkg>@<version>`
// counts. An entry is a bare source string or an object carrying one under
// "source"; both are read and anything else in the file is left to Pi. A
// missing settings file means nothing is installed yet.
func declaredExtensions() (map[string]bool, error) {
	declared := map[string]bool{}
	settingsPath := filepath.Join(piAgentDir(), "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return declared, nil
		}
		return nil, fmt.Errorf("failed to read %s: %w", settingsPath, err)
	}
	var settings struct {
		Packages []json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", settingsPath, err)
	}
	for _, raw := range settings.Packages {
		var source string
		if err := json.Unmarshal(raw, &source); err != nil {
			var entry struct {
				Source string `json:"source"`
			}
			if err := json.Unmarshal(raw, &entry); err != nil {
				continue
			}
			source = entry.Source
		}
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

// selectTheme sets "theme" to the devgeta theme in Pi's settings.json: the one
// key devgeta writes there (ADR-0061 §3). Without force an existing "theme" is
// the user's choice and stays. Every other key is carried as raw JSON so Pi's
// own state (packages above all) round-trips; HTML escaping is off because the
// default would rewrite "&" in a package URL as \u0026. A file that does not
// parse is an error, never a reset.
func selectTheme(force bool) error {
	settingsPath := filepath.Join(piAgentDir(), "settings.json")
	settings := map[string]json.RawMessage{}
	data, err := os.ReadFile(settingsPath)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("failed to parse %s: %w", settingsPath, err)
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("failed to read %s: %w", settingsPath, err)
	}
	if _, exists := settings["theme"]; exists && !force {
		return nil
	}
	settings["theme"] = json.RawMessage(strconv.Quote(piThemeName))

	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(settings); err != nil {
		return fmt.Errorf("failed to encode %s: %w", settingsPath, err)
	}
	return files.WriteFileAtomic(settingsPath, out.Bytes(), files.FilePermission)
}

// configure is the body of both configure modes: theme file, theme selection,
// extensions, then registration - last, so a failed step does not mark pi as
// configured. Soft keeps what already exists (an existing theme file, the
// user's chosen theme); force rewrites devgeta's own pieces. Extensions are
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
	if err := selectTheme(force); err != nil {
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

func (p *Pi) ExecuteCommand(args ...string) error {
	executable, err := piExecutable()
	if err != nil {
		return fmt.Errorf(
			"pi executable not found; install it with `dg install --only pi`: %w",
			err,
		)
	}
	params := cmd.CommandParams{
		Command: executable,
		Args:    args,
	}
	if _, _, err := p.Base.ExecCommand(params); err != nil {
		return fmt.Errorf("pi command execution failed: %w", err)
	}
	return nil
}

func (p *Pi) Update() error {
	return fmt.Errorf("%w for pi", apps.ErrUpdateNotSupported)
}
