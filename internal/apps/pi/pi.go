// Pi terminal-based AI coding agent with devgeta integration
//
// Pi (@earendil-works/pi-coding-agent) is a terminal AI-coding harness. This
// module provides installation and state management for Pi with devgeta
// integration. Configuration is limited to a curated extension set installed
// through Pi's own package manager; themes, skills and two-agent parity stay
// out of scope (ADR-0059, ADR-0060).
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
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
	"github.com/cjairm/devgeta/pkg/utils"
)

var _ apps.App = (*Pi)(nil)

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

// piAgentDir is Pi's own config home, ~/.pi/agent: the managed install root's
// parent. Its settings.json records the packages `pi install` has added.
func piAgentDir() string {
	return filepath.Dir(paths.Paths.Home.PiInstall)
}

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
	if len(pending) == 0 {
		return nil
	}
	executable, err := piExecutable()
	if err != nil {
		return fmt.Errorf(
			"configuring pi extensions needs the pi binary; install pi first (dg install --only pi), then re-run: %w",
			err,
		)
	}
	for _, source := range pending {
		utils.PrintInfo("Installing Pi extension " + source + "...")
		if err := p.runPi(executable, "install", source); err != nil {
			return fmt.Errorf("failed to install pi extension %s: %w", source, err)
		}
		utils.PrintSuccess("Installed Pi extension " + source)
	}
	return nil
}

// registerInstalled records pi in the installed list, after the extensions
// have landed so a failed install does not mark pi as configured.
func (p *Pi) registerInstalled() error {
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

// SoftConfigure installs the curated extensions Pi does not already declare,
// then registers pi. No config files are written, so Pi's own settings are the
// only state to check. Themes, shared skills and agent-config parity remain out
// of scope (ADR-0059).
func (p *Pi) SoftConfigure() error {
	if err := p.configureExtensions(); err != nil {
		return err
	}
	return p.registerInstalled()
}

// ForceConfigure is SoftConfigure: there are no devgeta-owned files to
// overwrite, and refreshing installed extensions is `pi update`'s job.
func (p *Pi) ForceConfigure() error { return p.SoftConfigure() }

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

// runPi executes the resolved pi binary once for this caller, so a configure
// run pays for one lookup instead of one per extension.
func (p *Pi) runPi(executable string, args ...string) error {
	params := cmd.CommandParams{
		Command: executable,
		Args:    args,
	}
	_, _, err := p.Base.ExecCommand(params)
	if err != nil {
		return fmt.Errorf("pi command execution failed: %w", err)
	}
	return nil
}

func (p *Pi) ExecuteCommand(args ...string) error {
	path, err := piExecutable()
	if err != nil {
		return fmt.Errorf("pi executable not found: %w", err)
	}
	return p.runPi(path, args...)
}

func (p *Pi) Update() error {
	return fmt.Errorf("%w for pi", apps.ErrUpdateNotSupported)
}
