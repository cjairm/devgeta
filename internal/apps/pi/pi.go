// Pi terminal-based AI coding agent with devgeta integration
//
// Pi (@earendil-works/pi-coding-agent) is a terminal AI-coding harness. This
// module provides installation and state management for Pi with devgeta
// integration. Configuration (themes, skills, shared config) is deliberately
// out of scope for now — see ADR-0059 decision 6.
//
// References:
// - Official installer: https://pi.dev/install.sh

package pi

import (
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

// Uninstall removes only the managed install root. ~/.pi/agent is Pi's own
// config/session home — sessions, agent state, skills — and must survive
// (ADR-0059 decision 3). os.RemoveAll rather than a shelled `rm -rf` so
// pkg/paths' test sandbox contains it, and because RemoveAll deletes a
// symlinked root itself rather than following it into whatever it points at.
// ForceConfigure registers pi in the installed list and nothing else.
// There are no themes, no shared skills and no generated config files: how
// devgeta should configure Pi is undecided until a later cycle (ADR-0059
// decision 6).
func (p *Pi) ForceConfigure() error {
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

// SoftConfigure is the same registration: with no config files, there is no
// marker to check before writing the installed list.
func (p *Pi) SoftConfigure() error {
	return p.ForceConfigure()
}

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
