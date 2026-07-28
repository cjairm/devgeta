package claude

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

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
	_ apps.App                 = (*Claude)(nil)
	_ apps.SelectiveConfigurer = (*Claude)(nil)
)

type Claude struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
	// rtkInit overrides the `rtk init` invocation used by the rtk part
	// (used in tests).
	rtkInit func() error
}

func (c *Claude) Name() string       { return constants.Claude }
func (c *Claude) Kind() apps.AppKind { return apps.KindTerminal }

func New() *Claude {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Claude{Cmd: osCmd, Base: baseCmd}
}

func (c *Claude) Install() error {
	params := cmd.CommandParams{
		Command: "sh",
		Args:    []string{"-c", "curl -fsSL https://claude.ai/install.sh | bash"},
	}
	_, _, err := c.Base.ExecCommand(params)
	if err != nil {
		return fmt.Errorf("failed to install claude: %w", err)
	}
	return nil
}

func (c *Claude) ForceInstall() error {
	return baseapp.Reinstall(c.Install, c.Uninstall)
}

func (c *Claude) SoftInstall() error {
	if _, err := exec.LookPath(constants.Claude); err == nil {
		return nil
	}
	return c.Install()
}

func (c *Claude) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if _, _, err := c.Base.ExecCommand(cmd.CommandParams{
		Command: "sh",
		Args:    []string{"-c", "rm -f ~/.local/bin/claude && rm -rf ~/.local/share/claude"},
	}); err != nil {
		return fmt.Errorf("failed to uninstall claude: %w", err)
	}
	_ = os.RemoveAll(paths.Paths.Config.Claude)
	gc.DisableShellFeature(constants.Claude)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to regenerate shell config: %w", err)
	}
	gc.RemoveFromInstalled(constants.Claude, "package")
	return gc.Save()
}

func (c *Claude) ForceConfigure() error {
	if err := os.MkdirAll(paths.Paths.Config.Claude, 0o755); err != nil {
		return err
	}

	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}

	// settings.json is rendered from a template so tracked opt-ins (the rtk
	// hook — ADR-0004) survive a --force re-render instead of being wiped.
	if err := files.GenerateFromTemplate(
		filepath.Join(paths.Paths.App.Configs.Claude, "settings.json.tmpl"),
		filepath.Join(paths.Paths.Config.Claude, "settings.json"),
		gc.Integrations,
	); err != nil {
		return fmt.Errorf("failed to render claude settings: %w", err)
	}

	for _, script := range []string{"statusline.sh", "format.sh", "task-redirect.sh", "agent-state.sh"} {
		dst := filepath.Join(paths.Paths.Config.Claude, script)
		if err := files.CopyFile(
			filepath.Join(paths.Paths.App.Configs.Claude, script),
			dst,
		); err != nil {
			return fmt.Errorf("failed to copy claude %s: %w", script, err)
		}
		if err := os.Chmod(dst, 0o755); err != nil {
			return fmt.Errorf("failed to chmod %s: %w", script, err)
		}
	}

	if err := files.CopyDir(
		filepath.Join(paths.Paths.App.Configs.Claude, "themes"),
		filepath.Join(paths.Paths.Config.Claude, "themes"),
	); err != nil {
		return fmt.Errorf("failed to copy claude themes: %w", err)
	}

	if err := baseapp.SyncSharedParts(
		paths.Paths.Config.Claude,
		baseapp.SharedConfigParts,
	); err != nil {
		return fmt.Errorf("failed to copy claude shared config: %w", err)
	}

	gc.ReconcileShellFeatures()
	gc.AddToInstalled(constants.Claude, "package")
	gc.Shell.Claude = true
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to regenerate shell config: %w", err)
	}
	return nil
}

func (c *Claude) SoftConfigure() error {
	markerFile := filepath.Join(paths.Paths.Config.Claude, "settings.json")
	if files.FileAlreadyExist(markerFile) {
		// Config already exists, but ensure shell feature is enabled
		gc := &config.GlobalConfig{}
		if err := gc.Create(); err != nil {
			return fmt.Errorf("failed to create global config: %w", err)
		}
		if err := gc.Load(); err != nil {
			return fmt.Errorf("failed to load global config: %w", err)
		}
		if !gc.Shell.Claude {
			gc.Shell.Claude = true
			if err := gc.Save(); err != nil {
				return fmt.Errorf("failed to save global config: %w", err)
			}
			if err := gc.RegenerateShellConfig(); err != nil {
				return fmt.Errorf("failed to regenerate shell config: %w", err)
			}
		}
		return nil
	}
	return c.ForceConfigure()
}

// ConfigurableParts lists the parts --only can refresh: the shared config
// subtrees plus the rtk integration (wires rtk's command-rewriting hook —
// the explicit opt-in required by ADR-0004).
func (c *Claude) ConfigurableParts() []string {
	return append(slices.Clone(baseapp.SharedConfigParts), constants.Rtk)
}

// ForceConfigureParts refreshes only the named parts, leaving settings.json,
// the scripts, and themes untouched. Shared subtrees (skills, commands,
// agents) are overwritten from the embedded configs; the rtk part opts into
// rtk's hook via enableRtkHook. This is the `--force --only=...` path.
func (c *Claude) ForceConfigureParts(parts []string) error {
	if err := os.MkdirAll(paths.Paths.Config.Claude, 0o755); err != nil {
		return err
	}
	shared := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == constants.Rtk {
			if err := c.enableRtkHook(); err != nil {
				return err
			}
			continue
		}
		shared = append(shared, part)
	}
	if len(shared) == 0 {
		return nil
	}
	return baseapp.SyncSharedParts(paths.Paths.Config.Claude, shared)
}

// enableRtkHook wires rtk's command-rewriting hook via `rtk init` — which
// owns the integration formats (settings.json hook entry, RTK.md, the global
// CLAUDE.md reference) and patches in place rather than overwriting — and
// then records the opt-in in the global config so every future settings.json
// render keeps the hook entry. Wiring runs BEFORE persisting: a failed init
// must not leave a false "enabled" flag behind, or the next render would emit
// a hook entry for an integration that never succeeded. (The inverse failure
// — init succeeded, save failed — is benign: re-running the command
// converges.) It deliberately does NOT render settings.json here: a
// hand-written settings.json must survive `--only=rtk` untouched.
func (c *Claude) enableRtkHook() error {
	if err := c.runRtkInit(); err != nil {
		return fmt.Errorf(
			"failed to wire rtk into claude (is rtk installed? try `dg install --only rtk`): %w",
			err,
		)
	}
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.Integrations.RtkClaudeHook = true
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

// runRtkInit executes rtk's Claude Code integration through the rtk app
// wrapper; injectable for tests. InitClaude streams output so rtk's one-time
// interactive consent prompt is visible instead of hanging in a captured
// buffer.
func (c *Claude) runRtkInit() error {
	if c.rtkInit != nil {
		return c.rtkInit()
	}
	// Same philosophy as pkg/paths' test sandbox: a test that forgets to
	// inject rtkInit must fail loudly, never execute the real rtk binary.
	if testing.Testing() {
		return fmt.Errorf(
			"refusing to run real `rtk init` under go test — inject rtkInit in the test",
		)
	}
	return rtk.New().InitClaude()
}

func (c *Claude) ExecuteCommand(args ...string) error {
	params := cmd.CommandParams{
		Command: constants.Claude,
		Args:    args,
	}
	_, _, err := c.Base.ExecCommand(params)
	if err != nil {
		return fmt.Errorf("claude command execution failed: %w", err)
	}
	return nil
}

func (c *Claude) Update() error {
	return fmt.Errorf(
		"%w — re-run: curl -fsSL https://claude.ai/install.sh | bash",
		apps.ErrUpdateNotSupported,
	)
}
