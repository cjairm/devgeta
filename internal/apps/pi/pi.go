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
			piMinNodeMajor,
			piMinNodeMinor,
			err,
		)
	}
	major, minor, ok := parseNodeVersion(strings.TrimSpace(stdout))
	if !ok || major < piMinNodeMajor || (major == piMinNodeMajor && minor < piMinNodeMinor) {
		return fmt.Errorf(
			"pi needs Node.js %d.%d or newer; found %s — upgrade Node (dg install --only languages), then re-run",
			piMinNodeMajor,
			piMinNodeMinor,
			strings.TrimSpace(stdout),
		)
	}
	if _, err := cmd.LookPathFn("npm"); err != nil {
		return fmt.Errorf(
			"pi needs Node.js %d.%d or newer and npm; npm was not found — install npm first (dg install --only languages), then re-run: %w",
			piMinNodeMajor,
			piMinNodeMinor,
			err,
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

// managedInstallMarker is the JSON marker Pi's script writes inside a managed
// install root. Its presence is the script's own statement that the install
// under that root is managed.
const managedInstallMarker = "managed-install.json"

// managedInstallRoot is the managed install root Pi's script actually used, or
// "" when Pi is not managed-installed anywhere devgeta can see.
//
// Candidates are checked in the script's own precedence order
// (select_managed_install_dir): an explicit $PI_MANAGED_INSTALL_ROOT, then
// <agent dir>/install — the agent dir being $PI_CODING_AGENT_DIR or
// ~/.pi/agent, which the last candidate already spells out — then devgeta's
// recorded default root. Both env vars move the root, so keying detection and
// uninstall on the default alone would re-run the installer on every soft
// install for a user who set either one, and would leave the real root in place
// on uninstall (ADR-0059 decisions 2-3).
//
// Only the marker's presence is checked, never its contents: a marker written by
// a newer Pi schema is still an install, and refusing to see it would strand the
// root at uninstall. The script's third candidate — the root an already-resolved
// `pi` command points at — is deliberately not mirrored: devgeta consults these
// roots only when `pi` does not resolve, so there is no command to derive one
// from.
func managedInstallRoot() string {
	for _, root := range []string{
		strings.TrimRight(os.Getenv("PI_MANAGED_INSTALL_ROOT"), "/"),
		filepath.Join(piAgentDir(), "install"),
		paths.Paths.Home.PiInstall,
	} {
		if root == "" {
			continue
		}
		if files.FileAlreadyExist(filepath.Join(root, managedInstallMarker)) {
			return root
		}
	}
	return ""
}

// SoftInstall installs pi only when it is not already there. The managed marker
// — wherever the env vars put the root — covers a devgeta- or script-installed
// copy even before the new bin dir is on the running process's PATH; the PATH
// check covers a `pi` the user installed by other means.
func (p *Pi) SoftInstall() error {
	return baseapp.SoftInstall(
		func() bool { return managedInstallRoot() != "" },
		constants.Pi,
		p.Install,
	)
}

// piAgentDir is Pi's own config home. Pi relocates it when
// PI_CODING_AGENT_DIR is set, and both the settings file and the npm tree move
// with it, so devgeta has to read the same directory Pi writes — reading
// ~/.pi/agent while Pi writes elsewhere would make every soft configure
// reinstall the whole set. Without the override it is the managed install
// root's parent, which is ~/.pi/agent by construction (ADR-0059).
//
// This stays keyed on PI_CODING_AGENT_DIR alone, never on the resolved install
// root: Pi itself resolves its config home the same way, so a
// PI_MANAGED_INSTALL_ROOT that moves the install leaves settings.json where it
// was. Deriving the agent dir from the install root would read the wrong
// settings file for exactly that user.
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

// readInstalledExtensions reports, per curated extension, the source string Pi
// currently declares for it (matched the way Pi matches — by package name, so
// a pinned `npm:<pkg>@<version>` counts). A missing settings file means
// nothing is installed yet. An entry is either a bare source string or an
// object carrying one under "source" — Pi's own settings accepts both — so
// both shapes are read; anything else in the file is left to Pi.
func readInstalledExtensions() (map[string]string, error) {
	installed := map[string]string{}
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
		if err := json.Unmarshal(raw, &source); err != nil {
			var entry struct {
				Source string `json:"source"`
			}
			if err := json.Unmarshal(raw, &entry); err != nil {
				continue
			}
			source = entry.Source
		}
		// Pi compares npm sources by package name, ignoring a version or tag.
		// Preserve the declared source so a forced refresh does not unpin it.
		for _, name := range piExtensions {
			if source == name || strings.HasPrefix(source, name+"@") {
				if _, exists := installed[name]; !exists {
					installed[name] = source
				}
				break
			}
		}
	}
	return installed, nil
}

// extension is one curated source during a configure run.
type extension struct {
	source string
	added  bool
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
	var pending []extension
	for _, name := range piExtensions {
		source, exists := installed[name]
		if exists && !force {
			utils.PrintInfo("Pi extension already configured: " + source)
			continue
		}
		if !exists {
			source = name
		}
		pending = append(pending, extension{source: source, added: !exists})
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
	// A failed configure removes what it added; extensions it merely refreshed
	// over are left alone — their prior installed version is not recoverable
	// (ADR-0060 decision 4).
	var done []extension
	for _, ext := range pending {
		utils.PrintInfo("Installing Pi extension " + ext.source + "...")
		if err := p.runPi(executable, "install", ext.source); err != nil {
			return p.rollbackExtensions(executable, ext.source, err, done)
		}
		utils.PrintSuccess("Installed Pi extension " + ext.source)
		done = append(done, ext)
	}
	return nil
}

// rollbackExtensions undoes a failed configure by removing, newest first, the
// sources this run added. Extensions the run refreshed over an existing
// declaration are left alone: Pi offers no query for the version that was
// installed before the run, so re-installing the same spec would fetch the
// latest again and restore nothing — and would risk a spurious "rollback
// failed" on a package that was never disturbed. Each removal's failure is
// collected rather than stopping the rest, and every failure is named so the
// user can finish by hand with `pi remove <package>`. The failing source
// itself needs no removal: pi install records a package only after its own
// install succeeds.
func (p *Pi) rollbackExtensions(executable, failed string, cause error, done []extension) error {
	var failures []string
	for i := len(done) - 1; i >= 0; i-- {
		ext := done[i]
		if !ext.added {
			continue
		}
		if err := p.runPi(executable, "remove", ext.source); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", ext.source, err))
		} else {
			utils.PrintInfo("Rolled back Pi extension " + ext.source)
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf(
			"failed to install pi extension %s: %w; rollback could not remove %s — finish by hand with `pi remove <package>`",
			failed,
			cause,
			strings.Join(failures, "; "),
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
	root := managedInstallRoot()
	if root == "" {
		// No marker anywhere: an interrupted install leaves the tree without
		// one, because the script writes the marker last. Clear where the script
		// would have put it — under the agent dir, which is
		// PI_CODING_AGENT_DIR/install when that is set and the default root
		// otherwise. On a machine that never had Pi this removes nothing.
		root = filepath.Join(piAgentDir(), "install")
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("failed to remove %s: %w", root, err)
	}
	// No shell feature was ever registered for pi (ADR-0059 decision 4), so
	// there is nothing to disable or regenerate — only the installed list.
	gc.RemoveFromInstalled(constants.Pi, "package")
	return gc.Save()
}

// managedLauncherPath is the launcher the official installer writes beside a
// managed install root: dirname(root)/bin/pi. That is the root's parent, NOT
// $PI_CODING_AGENT_DIR — the script computes PI_MANAGED_AGENT_DIR as
// "${PI_MANAGED_INSTALL_DIR%/*}" and hands that to
// write_managed_install_launcher, so with PI_MANAGED_INSTALL_ROOT=/opt/pi-root
// the launcher is /opt/bin/pi. The two coincide only for the default root. The
// script creates it on every managed install, so a machine with the install
// marker also has this file even when nothing on PATH points at it yet (see
// ADR-0059 decision 4).
func managedLauncherPath(root string) string {
	return filepath.Join(filepath.Dir(root), "bin", constants.Pi)
}

// piExecutable prefers the user's PATH, then the managed launcher beside
// whichever root holds the install marker. The launcher is available before a
// shell reload, so a configure run right after install works without one.
func piExecutable() (string, error) {
	path, err := cmd.LookPathFn(constants.Pi)
	if err == nil {
		return path, nil
	}
	if root := managedInstallRoot(); root != "" {
		launcher := managedLauncherPath(root)
		if info, statErr := os.Stat(
			launcher,
		); statErr == nil && info.Mode().IsRegular() &&
			info.Mode().Perm()&0o111 != 0 {
			return launcher, nil
		}
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
