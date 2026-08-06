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
	"slices"
	"testing"
	"time"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	"github.com/cjairm/devgeta/internal/apps/rtk"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

var (
	_ apps.App                 = (*OpenCode)(nil)
	_ apps.SelectiveConfigurer = (*OpenCode)(nil)
)

const DEFAULT_THEME_NAME = "default"

type OpenCode struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
	// rtkInit overrides the `rtk init` invocation used by the rtk part
	// (used in tests).
	rtkInit func() error
}

func (o *OpenCode) Name() string       { return constants.OpenCode }
func (o *OpenCode) Kind() apps.AppKind { return apps.KindTerminal }

func New() *OpenCode {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &OpenCode{Cmd: osCmd, Base: baseCmd}
}

func (o *OpenCode) Install() error {
	return o.Cmd.InstallPackage(constants.OpenCode)
}

func (o *OpenCode) ForceInstall() error {
	return baseapp.Reinstall(o.Install, o.Uninstall)
}

func (o *OpenCode) SoftInstall() error {
	return o.Cmd.MaybeInstallPackage(constants.OpenCode)
}

func (o *OpenCode) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := o.Cmd.UninstallPackage(constants.OpenCode); err != nil {
		return fmt.Errorf("failed to uninstall opencode: %w", err)
	}
	_ = os.RemoveAll(paths.Paths.Config.OpenCode)
	gc.DisableShellFeature(constants.OpenCode)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to regenerate shell config: %w", err)
	}
	gc.RemoveFromInstalled(constants.OpenCode, "package")
	return gc.Save()
}

func (o *OpenCode) ForceConfigure() error {
	if err := os.RemoveAll(paths.Paths.Config.OpenCode); err != nil {
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
	theme := DEFAULT_THEME_NAME
	configFilePath := filepath.Join(
		paths.Paths.Config.OpenCode,
		fmt.Sprintf("%s.json", constants.OpenCode),
	)
	tmplPath := filepath.Join(
		paths.Paths.App.Configs.OpenCode,
		fmt.Sprintf("%s.json.tmpl", constants.OpenCode),
	)
	if theme == DEFAULT_THEME_NAME {
		themesDir := filepath.Join(paths.Paths.Config.OpenCode, "themes")
		if err := os.MkdirAll(themesDir, 0o755); err != nil {
			return fmt.Errorf("failed to create themes directory: %w", err)
		}
		if err := files.CopyFile(
			filepath.Join(
				paths.Paths.App.Configs.OpenCode,
				"themes",
				fmt.Sprintf("%s.json", DEFAULT_THEME_NAME),
			),
			filepath.Join(themesDir, fmt.Sprintf("%s.json", DEFAULT_THEME_NAME)),
		); err != nil {
			return fmt.Errorf("failed to copy opencode config theme: %w", err)
		}
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
		"Theme":          theme,
		"ScratchDirGlob": string(scratchDirGlobJSON),
	}); err != nil {
		return fmt.Errorf("failed to generate opencode configuration: %w", err)
	}
	if err := baseapp.SyncSharedParts(
		paths.Paths.Config.OpenCode,
		baseapp.SharedConfigParts,
	); err != nil {
		return fmt.Errorf("failed to copy opencode shared config: %w", err)
	}

	// The task-redirect plugin (and any future local OpenCode plugins) ships
	// from configs/opencode/plugin/, not configs/shared/ — plugins are an
	// OpenCode-specific mechanism, outside SharedConfigParts' skills/commands/
	// agents sync surface. OpenCode loads plugin files from
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

// ConfigurableParts lists the parts --only can refresh: the shared config
// subtrees plus the rtk integration (installs rtk's OpenCode plugin — the
// explicit opt-in required by ADR-0004).
func (o *OpenCode) ConfigurableParts() []string {
	return append(slices.Clone(baseapp.SharedConfigParts), constants.Rtk)
}

// ForceConfigureParts refreshes only the named parts. Shared subtrees
// (skills, commands, agents) are overwritten from the embedded configs;
// the rtk part runs `rtk init -g --opencode`, which installs rtk's plugin
// as its own file (plugins/rtk.ts) — devgeta never touches that file, so no
// opt-in state needs tracking here, unlike claude's settings.json hook.
// Unlike full ForceConfigure this does not remove or regenerate
// opencode.json or themes, so a hand-edited config survives. This is the
// `--force --only=...` path.
func (o *OpenCode) ForceConfigureParts(parts []string) error {
	if err := os.MkdirAll(paths.Paths.Config.OpenCode, 0o755); err != nil {
		return err
	}
	shared := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == constants.Rtk {
			if err := o.runRtkInit(); err != nil {
				return fmt.Errorf(
					"failed to wire rtk into opencode (is rtk installed? try `dg install --only rtk`): %w",
					err,
				)
			}
			continue
		}
		shared = append(shared, part)
	}
	if len(shared) == 0 {
		return nil
	}
	return baseapp.SyncSharedParts(paths.Paths.Config.OpenCode, shared)
}

// runRtkInit executes rtk's OpenCode integration through the rtk app
// wrapper; injectable for tests. InitOpenCode streams output so rtk's
// one-time interactive consent prompt is visible instead of hanging in a
// captured buffer.
func (o *OpenCode) runRtkInit() error {
	if o.rtkInit != nil {
		return o.rtkInit()
	}
	// Same philosophy as pkg/paths' test sandbox: a test that forgets to
	// inject rtkInit must fail loudly, never execute the real rtk binary.
	if testing.Testing() {
		return fmt.Errorf(
			"refusing to run real `rtk init` under go test — inject rtkInit in the test",
		)
	}
	return rtk.New().InitOpenCode()
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

// RunOptions configures a headless `opencode run` invocation (see (*OpenCode).Run).
type RunOptions struct {
	// Agent selects the OpenCode agent to run as (`--agent <name>`), e.g. a
	// reviewer agent's name.
	Agent string
	// Model, when non-empty, pins the run to a specific `provider/model`
	// (`-m provider/model`). Empty lets OpenCode use its configured default.
	Model string
	// Prompt is the run's message argument.
	Prompt string
	// Dir sets the command's working directory (e.g. a worktree path), so the
	// agent operates against a specific checkout rather than devgeta's own cwd.
	Dir string
	// Timeout bounds the run. Headless review runs can take a long time, so
	// callers are expected to pass a generous value rather than leave this
	// zero (unbounded).
	Timeout time.Duration
	// Env adds child-only environment variables on top of devgeta's own
	// environment (e.g. a review-round snapshot pointer) — see
	// CommandParams.Env for the overlay semantics.
	Env []string
}

// Run executes `opencode run` headlessly and returns its captured stdout.
//
// This is the only place in devgeta that assembles the `opencode run` command
// line (CLAUDE.md §6): every caller that needs a headless agent run — e.g.
// `dg task review-run` spawning reviewer agents — goes through here rather
// than building its own CommandParams. `--format json` is always requested:
// a headless run's whole point is to be machine-read, and every caller of
// this method needs that, so it is not a caller-chosen option. Interpreting
// the resulting NDJSON events and the `**Status:**` verdict line stays out of
// this method and belongs to the caller. Unlike ExecuteCommand, which
// discards output, Run returns stdout because the caller's whole job is to
// read what the agent produced — stdout is returned even when err is
// non-nil, since a non-zero exit can still carry a partial or diagnostic
// event stream worth inspecting.
func (o *OpenCode) Run(opts RunOptions) (string, error) {
	args := []string{"run", "--agent", opts.Agent, "--format", "json"}
	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}
	args = append(args, opts.Prompt)

	params := cmd.CommandParams{
		Command: constants.OpenCode,
		Args:    args,
		Dir:     opts.Dir,
		Timeout: opts.Timeout,
		Env:     opts.Env,
	}
	stdout, _, err := o.Base.ExecCommand(params)
	if err != nil {
		return stdout, fmt.Errorf("opencode run failed: %w", err)
	}
	return stdout, nil
}

func (o *OpenCode) Update() error {
	return fmt.Errorf("%w for opencode", apps.ErrUpdateNotSupported)
}
