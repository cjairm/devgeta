// Pi terminal-based AI coding agent with devgeta integration
//
// Pi (@earendil-works/pi-coding-agent) is a terminal AI-coding harness. This
// module provides installation and state management for Pi with devgeta
// integration. Configuration is limited to a curated extension set installed
// through Pi's own package manager; themes, skills and two-agent parity stay
// out of scope (ADR-0059 decision 6, ADR-0060).
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
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
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
	"npm:pi-web-access",
	"npm:pi-schedule-prompt",
	"npm:pi-powerline-footer",
	"npm:pi-openrouter-advanced-provider",
	"npm:pi-openrouter-request-details",
	"npm:pi-stalled-agent-recovery",
	"npm:pi-thinking-reformatter",
}

// piMinNodeVersion is what the official script demands in its own preflight:
// Node.js 22.19.0 or newer plus npm. Validating it here — before the download
// — turns the script's failure message into devgeta's own actionable error,
// and keeps a clean-machine install from downloading anything it cannot use.
const piMinNodeMajor, piMinNodeMinor = 22, 19

// requireCompatibleNode verifies the runtime Pi's installer needs, matching
// the script's own preflight. `node --version` goes through the Base
// executor, same as the languages coordinator's isLanguageInstalledOnSystem.
func (p *Pi) requireCompatibleNode() error {
	stdout, _, err := p.Base.ExecCommand(cmd.CommandParams{
		Command: "node",
		Args:    []string{"--version"},
	})
	if err != nil {
		return fmt.Errorf(
			"pi needs Node.js %d.%d or newer and npm; node was not found — install Node first (dg install --only languages), then re-run: %w",
			piMinNodeMajor, piMinNodeMinor, err,
		)
	}
	major, minor, ok := parseNodeVersion(strings.TrimSpace(stdout))
	if !ok || major < piMinNodeMajor || (major == piMinNodeMajor && minor < piMinNodeMinor) {
		return fmt.Errorf(
			"pi needs Node.js %d.%d or newer; found %s — upgrade Node (dg install --only languages), then re-run",
			piMinNodeMajor, piMinNodeMinor, strings.TrimSpace(stdout),
		)
	}
	if _, err := cmd.LookPathFn("npm"); err != nil {
		return fmt.Errorf(
			"pi needs Node.js %d.%d or newer and npm; npm was not found — install npm first (dg install --only languages), then re-run: %w",
			piMinNodeMajor, piMinNodeMinor, err,
		)
	}
	return nil
}

// parseNodeVersion reads `node --version` output ("v22.19.0") into major and
// minor ints. Reports ok=false on anything else rather than guessing — the
// caller's error names the actual output.
func parseNodeVersion(version string) (major, minor int, ok bool) {
	trimmed := strings.TrimPrefix(version, "v")
	firstDot := strings.IndexByte(trimmed, '.')
	if firstDot < 0 {
		return 0, 0, false
	}
	rest := trimmed[firstDot+1:]
	secondDot := strings.IndexByte(rest, '.')
	if secondDot < 0 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(trimmed[:firstDot])
	if err != nil {
		return 0, 0, false
	}
	minor, err = strconv.Atoi(rest[:secondDot])
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func (p *Pi) Install() error {
	if err := p.requireCompatibleNode(); err != nil {
		return err
	}
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

// managedMarkerPath is the JSON marker Pi's script writes inside the managed
// install root. Its presence is the script's own statement that the install
// under that root is managed.
func managedMarkerPath() string {
	return filepath.Join(paths.Paths.Home.PiInstall, "managed-install.json")
}

// SoftInstall installs pi only when it is not already there.
//
// Two checks, like opencode's: the managed marker covers a devgeta- or
// script-installed copy even before the new bin dir is on the running
// process's PATH, and LookPath covers a `pi` the user installed by other
// means — devgeta does not lay a second copy over it.
func (p *Pi) SoftInstall() error {
	if files.FileAlreadyExist(managedMarkerPath()) {
		return nil
	}
	if _, err := cmd.LookPathFn(constants.Pi); err == nil {
		return nil
	}
	return p.Install()
}

// piAgentDir is Pi's own config home. Pi relocates it when
// PI_CODING_AGENT_DIR is set, and both the settings file and the npm tree move
// with it, so devgeta has to read the same directory Pi writes — reading
// ~/.pi/agent while Pi writes elsewhere would make every soft configure
// reinstall the whole set. Without the override it is the managed install
// root's parent, which is ~/.pi/agent by construction (ADR-0059).
func piAgentDir() string {
	dir := os.Getenv("PI_CODING_AGENT_DIR")
	if dir == "" {
		return filepath.Dir(paths.Paths.Home.PiInstall)
	}
	// Pi expands a leading tilde itself (expandTildePath), so match it rather
	// than treating "~/x" as a path relative to the working directory.
	if dir == "~" {
		return paths.Paths.Home.Root
	}
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		return filepath.Join(paths.Paths.Home.Root, rest)
	}
	return dir
}

// piSettingsPath is where `pi install` records the package sources it has
// installed: <agent dir>/settings.json.
func piSettingsPath() string {
	return filepath.Join(piAgentDir(), "settings.json")
}

// readInstalledExtensions returns the package sources Pi's settings already
// declare. A missing settings file means nothing is installed yet. An entry is
// either a bare source string or an object carrying one under "source" — Pi's
// own settings accepts both — so both shapes are read; anything else in the
// file is left to Pi.
func readInstalledExtensions() (map[string]bool, error) {
	installed := map[string]bool{}
	settingsPath := piSettingsPath()
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return installed, nil
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
		if err := json.Unmarshal(raw, &source); err == nil {
			installed[source] = true
			continue
		}
		var entry struct {
			Source string `json:"source"`
		}
		if err := json.Unmarshal(raw, &entry); err == nil && entry.Source != "" {
			installed[entry.Source] = true
		}
	}
	return installed, nil
}

// configureExtensions installs the curated extensions Pi does not already
// declare. The soft path (install) skips what settings already list, so a
// re-run costs no network; force re-runs every install, which is how Pi picks
// up newer versions of an already-declared source.
func (p *Pi) configureExtensions(force bool) error {
	installed, err := readInstalledExtensions()
	if err != nil {
		return err
	}
	var pending []string
	for _, source := range piExtensions {
		if !force && installed[source] {
			continue
		}
		pending = append(pending, source)
	}
	if len(pending) == 0 {
		return nil
	}
	if _, err := cmd.LookPathFn(constants.Pi); err != nil {
		return fmt.Errorf(
			"configuring pi extensions needs the pi binary; install pi first (dg install --only pi), then re-run: %w",
			err,
		)
	}
	// A partial set is not an acceptable end state (§4, atomic installation),
	// so track what this run added and undo it if a later install fails. Only
	// sources that were not already declared count: a force re-install of an
	// extension Pi already listed is not ours to remove.
	var added []string
	for _, source := range pending {
		if err := p.ExecuteCommand("install", source); err != nil {
			return p.rollbackExtensions(source, err, added)
		}
		if !installed[source] {
			added = append(added, source)
		}
	}
	return nil
}

// rollbackExtensions uninstalls what this run installed, newest first. A
// removal that fails does not stop the rest from being undone — the goal is as
// close to the original state as possible, and every failure is named so the
// user can finish the job with `pi remove`. pi install records a package only
// after it installs successfully, so the failing source itself needs no removal
// (ADR-0060).
func (p *Pi) rollbackExtensions(failed string, cause error, added []string) error {
	var failures []string
	for i := len(added) - 1; i >= 0; i-- {
		if err := p.ExecuteCommand("remove", added[i]); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", added[i], err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf(
			"failed to install pi extension %s: %w; rollback could not remove %s — remove these with `pi remove <package>`",
			failed, cause, strings.Join(failures, "; "),
		)
	}
	return fmt.Errorf("failed to install pi extension %s: %w", failed, cause)
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

// ForceConfigure installs every curated extension (re-installing the ones
// already declared) and registers pi in the installed list. Themes, shared
// skills and agent-config parity remain out of scope (ADR-0059 decision 6).
func (p *Pi) ForceConfigure() error {
	if err := p.configureExtensions(true); err != nil {
		return err
	}
	return p.registerInstalled()
}

// SoftConfigure installs only the extensions Pi's settings do not already
// declare, then registers pi. No config files are written, so there is no
// marker to check beyond Pi's own settings.
func (p *Pi) SoftConfigure() error {
	if err := p.configureExtensions(false); err != nil {
		return err
	}
	return p.registerInstalled()
}

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
		return fmt.Errorf(
			"failed to remove %s: %w",
			paths.Paths.Home.PiInstall,
			err,
		)
	}
	// No shell feature was ever registered for pi (ADR-0059 decision 4), so
	// there is nothing to disable or regenerate — only the installed list.
	gc.RemoveFromInstalled(constants.Pi, "package")
	return gc.Save()
}

func (p *Pi) ExecuteCommand(args ...string) error {
	params := cmd.CommandParams{
		Command: constants.Pi,
		Args:    args,
	}
	_, _, err := p.Base.ExecCommand(params)
	if err != nil {
		return fmt.Errorf("pi command execution failed: %w", err)
	}
	return nil
}

func (p *Pi) Update() error {
	return fmt.Errorf("%w for pi", apps.ErrUpdateNotSupported)
}
