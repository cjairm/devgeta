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

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
)

var (
	_ apps.App              = (*OpenCode)(nil)
	_ apps.ThemedConfigurer = (*OpenCode)(nil)
)

const DEFAULT_THEME_NAME = "default"

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

// openCodeInstallScriptURL is opencode's official installer, and its only
// supported install channel on either platform (ADR-0047).
//
// Install() stages it through cmd.RunInstallScript rather than a raw
// `curl | bash` pipeline: the pipeline's exit status is bash's, not curl's, so
// a 404 or a truncated download pipes an empty script into bash - which exits
// 0 - and a broken download is reported as a successful install. It runs with
// `bash`, never `sh`: the script's shebang is `#!/usr/bin/env bash`, and on
// Debian `sh` is dash.
const openCodeInstallScriptURL = "https://opencode.ai/install"

func (o *OpenCode) Install() error {
	if err := cmd.RunInstallScript(
		o.Base,
		constants.OpenCode,
		openCodeInstallScriptURL,
		"bash",
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

// SoftInstall installs opencode only when it is not already there.
//
// The prefix check is not redundant with the PATH lookup: a freshly installed
// ~/.opencode/bin is not on the running process's PATH - nor on the user's,
// until they open a new shell - so a PATH-only check re-runs the installer on
// every `dg install`, which is the Debian idempotency defect ADR-0047 fixes.
func (o *OpenCode) SoftInstall() error {
	return baseapp.SoftInstall(
		func() bool { return files.FileAlreadyExist(installedBinary()) },
		constants.OpenCode,
		o.Install,
	)
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
	configFilePath := filepath.Join(
		paths.Paths.Config.OpenCode,
		fmt.Sprintf("%s.json", constants.OpenCode),
	)
	tmplPath := filepath.Join(
		paths.Paths.App.Configs.OpenCode,
		fmt.Sprintf("%s.json.tmpl", constants.OpenCode),
	)
	// The theme's raw-color block is templated (ADR-0043); the rest of the
	// role-mapping JSON stays static and hand-maintained. The destination
	// file is named after the theme itself, because that is the name
	// opencode.json's "theme" field references below - one template renders
	// whichever theme is current.
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
	scratchDir, err := paths.EnsureScratchDir()
	if err != nil {
		return fmt.Errorf("failed to ensure scratch dir: %w", err)
	}
	// external_directory keys use gitignore-style patterns (like the
	// read/edit blocks above), so the grant needs a "/**" suffix to cover
	// nested paths — unlike Claude's additionalDirectories, which grants a
	// bare directory. Marshal the FULL key (suffix included) in one call:
	// concatenating a literal suffix onto an already-escaped string would
	// corrupt the JSON if the path itself needed any escaping.
	scratchDirGlobJSON, err := json.Marshal(scratchDir + "/**")
	if err != nil {
		return fmt.Errorf("failed to encode scratch dir: %w", err)
	}

	if err := files.GenerateFromTemplate(tmplPath, configFilePath, map[string]string{
		"Theme":          def.Name,
		"ScratchDirGlob": string(scratchDirGlobJSON),
	}); err != nil {
		return fmt.Errorf("failed to generate opencode configuration: %w", err)
	}

	// The task-redirect plugin (and any future local OpenCode plugins) ships
	// from configs/opencode/plugin/. OpenCode loads plugin files from
	// ~/.config/opencode/plugin/ (or the singular/plural "plugins" variant;
	// see task-redirect.js's header comment).
	// CopyDir creates its destination directory itself (see pkg/files.CopyDir),
	// same as every other CopyDir call site in this codebase — no explicit
	// MkdirAll needed here.
	pluginDst := filepath.Join(paths.Paths.Config.OpenCode, "plugin")
	if err := files.CopyDir(
		filepath.Join(paths.Paths.App.Configs.OpenCode, "plugin"),
		pluginDst,
	); err != nil {
		return fmt.Errorf("failed to copy opencode plugins: %w", err)
	}

	// Having just given OpenCode plugins to load, pay off the startup cost
	// that loading them triggers. Both directories matter: OpenCode resolves
	// dependencies in each, and locking only the config dir leaves most of
	// the delay in place (ADR-0049).
	for _, dir := range []string{paths.Paths.Config.OpenCode, paths.Paths.Home.OpenCode} {
		ensurePluginLockfile(o.Base, dir)
	}

	if err := baseapp.MaintainScratchDir(); err != nil {
		return fmt.Errorf("failed to maintain scratch dir: %w", err)
	}

	// Deploys the output-budget runner and writes agent-runtime.json.
	// Called from both opencode's and claude's configure paths (cycle doc
	// Step 5) so running either one converges both agents.
	if err := baseapp.EnsureAgentRuntime(gc); err != nil {
		return fmt.Errorf("failed to ensure the output-budget runtime: %w", err)
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

func (o *OpenCode) Update() error {
	return fmt.Errorf("%w for opencode", apps.ErrUpdateNotSupported)
}

// pluginLockfiles are the lockfile names OpenCode's embedded bun recognizes.
// Any one of them is enough to keep bun off the network.
var pluginLockfiles = []string{"package-lock.json", "bun.lock", "bun.lockb"}

// devgetaManagedEntries is everything devgeta generates inside
// ~/.config/opencode, and therefore everything a --force reconfigure is
// entitled to delete. "plugin" is singular on purpose: OpenCode also loads a
// sibling "plugins" directory, which devgeta never creates and where other
// tools install their own plugins.
//
// Keep this in step with what ForceConfigureTheme writes.
var devgetaManagedEntries = []string{"opencode.json", "themes", "plugin"}

// clearManagedConfig removes devgeta's own generated files from dir, leaving
// everything else untouched.
//
// This deletes by allowlist rather than wiping the directory, because devgeta
// is one of several writers here and by far the least entitled. OpenCode
// keeps ~60MB of resolved node_modules, the package.json and lockfile
// describing them, its tui.json, whatever backups a user made, and other
// tools' plugins. A blanket wipe took all of it, which cost users those
// plugins and scheduled a full cold dependency install on the next launch
// — the worst case of the stall ADR-0049 removes.
//
// An allowlist also fails safe: a file devgeta has not heard of survives,
// where the inverse rule (enumerate what to keep) silently destroys anything
// new that OpenCode or an integration starts writing.
func clearManagedConfig(dir string) error {
	for _, entry := range devgetaManagedEntries {
		if err := os.RemoveAll(filepath.Join(dir, entry)); err != nil {
			return err
		}
	}
	return nil
}

// ensurePluginLockfile gives dir a dependency lockfile if it declares
// dependencies and has none.
//
// OpenCode resolves its plugin dependency with its embedded bun on EVERY
// startup, whenever at least one plugin file exists — and devgeta installs
// six. With no lockfile that resolution is a registry round-trip: measured at
// 45-60s of idle wait per launch, growing as a machine's npm cache ages, even
// though the dependency is already vendored at the right version. A lockfile
// takes it under a second. See ADR-0049 for the measurements.
//
// --offline is load-bearing: it resolves purely from the vendored
// node_modules, so generating the lockfile cannot itself make the network
// call this exists to eliminate, nor change which version is installed.
//
// Best-effort by design. A missing npm or a failed generation logs and
// continues — a slow OpenCode beats a failed `dg configure`. An existing
// lockfile is never rewritten: regenerating one could change the resolved
// version out from under the user.
func ensurePluginLockfile(base cmd.BaseCommandExecutor, dir string) {
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		return // nothing declares a dependency here, so there is nothing to lock
	}
	for _, lockfile := range pluginLockfiles {
		if _, err := os.Stat(filepath.Join(dir, lockfile)); err == nil {
			return
		}
	}
	if _, _, err := base.ExecCommand(cmd.CommandParams{
		Command: "npm",
		Args:    []string{"install", "--package-lock-only", "--offline"},
		Dir:     dir,
	}); err != nil {
		logger.L().Warnw("could not generate opencode plugin lockfile", "dir", dir, "error", err)
	}
}
