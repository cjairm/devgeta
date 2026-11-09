package claude

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

// settingsTemplateData is settings.json.tmpl's render data. ScratchDir is the
// scratch directory granted through permissions.additionalDirectories
// (ADR-0015 §2).
//
// ScratchDir must already be JSON-encoded (quotes included, via
// json.Marshal) by the time it reaches the template: text/template does no
// escaping of its own, and this is the first user-influenced value
// (XDG_CACHE_HOME-derived) this template has ever interpolated — a quote or
// backslash in an unescaped path would emit a settings.json Claude Code
// cannot parse at all.
type settingsTemplateData struct {
	ScratchDir string
	// Theme names the current theme (theme.Definition.Name), rendered into
	// "custom:{{ .Theme }}" - the name Claude's own theme picker resolves
	// against the file settings.json.tmpl's sibling ForceConfigureTheme call
	// writes to themes/<name>.json.
	Theme string
	// PackMarketplaceName, PackMarketplacePath and PackPlugins render the
	// agent packs installed in Claude Code from agent_packs state, so a
	// --force re-render keeps them (ADR-0068 §6). Like ScratchDir they are
	// already JSON-encoded. PackPlugins is empty, and nothing is rendered,
	// when no pack is installed in Claude Code.
	PackMarketplaceName string
	PackMarketplacePath string
	PackPlugins         []string
}

// addPacks fills the pack fields from agent_packs state: one plugin id per
// pack that has a Claude Code record, in pack-name order so the output is
// stable. Packs installed only in other coders add nothing.
func (d *settingsTemplateData) addPacks(gc *config.GlobalConfig) error {
	var names []string
	for name, pack := range gc.AgentPacks {
		if pack.Coders[constants.Claude].Plugin != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	for _, name := range names {
		id, err := json.Marshal(gc.AgentPacks[name].Coders[constants.Claude].Plugin)
		if err != nil {
			return fmt.Errorf("failed to encode the plugin id of %s: %w", name, err)
		}
		d.PackPlugins = append(d.PackPlugins, string(id))
	}
	marketplace, err := json.Marshal(constants.AgentPackMarketplace)
	if err != nil {
		return fmt.Errorf("failed to encode the marketplace name: %w", err)
	}
	path, err := json.Marshal(paths.Paths.App.AgentPacks)
	if err != nil {
		return fmt.Errorf("failed to encode the agent pack path: %w", err)
	}
	d.PackMarketplaceName, d.PackMarketplacePath = string(marketplace), string(path)
	return nil
}

var (
	_ apps.App              = (*Claude)(nil)
	_ apps.ThemedConfigurer = (*Claude)(nil)
)

type Claude struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func (c *Claude) Name() string       { return constants.Claude }
func (c *Claude) Kind() apps.AppKind { return apps.KindTerminal }

func New() *Claude {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Claude{Cmd: osCmd, Base: baseCmd}
}

// claudeInstallScriptURL is the official Claude Code installer. Install()
// stages it through cmd.RunInstallScript rather than a raw `curl | bash`
// pipeline: the pipeline's exit status was bash's, not curl's, so a 404 or a
// truncated download piped an empty script into bash — which exits 0 — and a
// broken download was reported as a successful install.
const claudeInstallScriptURL = "https://claude.ai/install.sh"

// Install installs exactly the pinned version: the script takes
// `[stable|latest|VERSION]` as its first argument. settings.json turns the
// auto-updater off (DISABLE_AUTOUPDATER) so the pin holds afterwards.
func (c *Claude) Install() error {
	if err := cmd.RunInstallScript(
		c.Base, "claude", claudeInstallScriptURL, "bash", constants.PinnedVersion.Claude,
	); err != nil {
		return fmt.Errorf("failed to install claude: %w", err)
	}
	return nil
}

// IsInstalled reports whether claude is on PATH. The official installer puts it
// in ~/.local/bin, which the user's PATH normally has; SoftInstall makes the
// same check.
func (c *Claude) IsInstalled() bool {
	_, err := cmd.LookPathFn(constants.Claude)
	return err == nil
}

func (c *Claude) ForceInstall() error {
	return baseapp.Reinstall(c.Install, c.Uninstall)
}

// SoftInstall installs Claude Code when it is missing or older than the pin.
// A newer version the user installed is left alone.
func (c *Claude) SoftInstall() error {
	if c.IsInstalled() && c.installedIsAtLeastPin() {
		return nil
	}
	return c.Install()
}

// installedIsAtLeastPin compares `claude --version` ("2.1.319 (Claude Code)")
// with the pin. Anything it cannot read counts as older, so SoftInstall
// reinstalls rather than trusting it.
func (c *Claude) installedIsAtLeastPin() bool {
	stdout, _, err := c.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Claude,
		Args:    []string{"--version"},
		NoStdin: true,
	})
	if err != nil {
		return false
	}
	installed, ok := version.Find(stdout)
	return ok && version.AtLeast(installed, constants.PinnedVersion.Claude)
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
	// Uninstall deleted the config dir, so every pack's record of this coder is
	// stale (ADR-0070 §6).
	baseapp.ForgetAgentPackCoder(gc, constants.Claude)
	return gc.Save()
}

// ForceConfigure resolves the theme for current_theme (falling back to
// theme.DefaultThemeName when it is empty) and renders with it. `dg theme
// set` does not go through here: it resolves the target theme itself and
// calls ForceConfigureTheme directly, because current_theme is deliberately
// not written yet at that point (docs/plans/cycles/2026-09-14-dg-theme.md
// Step 5) - reading it here would render the theme being replaced.
func (c *Claude) ForceConfigure() error {
	def, err := theme.CurrentDefinition()
	if err != nil {
		return fmt.Errorf("failed to resolve current theme: %w", err)
	}
	return c.ForceConfigureTheme(def)
}

func (c *Claude) ForceConfigureTheme(def theme.Definition) error {
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

	palette, err := def.PaletteFor(c.Name())
	if err != nil {
		return fmt.Errorf("failed to resolve theme palette: %w", err)
	}

	scratchDir, err := paths.EnsureScratchDir()
	if err != nil {
		return fmt.Errorf("failed to ensure scratch dir: %w", err)
	}
	scratchDirJSON, err := json.Marshal(scratchDir)
	if err != nil {
		return fmt.Errorf("failed to encode scratch dir: %w", err)
	}

	settingsData := settingsTemplateData{
		ScratchDir: string(scratchDirJSON),
		Theme:      def.Name,
	}
	if err := settingsData.addPacks(gc); err != nil {
		return err
	}

	// settings.json is rendered from a template so tracked opt-ins survive a
	// --force re-render instead of being wiped.
	if err := files.GenerateFromTemplate(
		filepath.Join(paths.Paths.App.Configs.Claude, "settings.json.tmpl"),
		filepath.Join(paths.Paths.Config.Claude, "settings.json"),
		settingsData,
	); err != nil {
		return fmt.Errorf("failed to render claude settings: %w", err)
	}

	for _, script := range []string{
		"statusline.sh",
		"format.sh",
		"agent-config-guard.sh",
		"agent-state.sh",
	} {
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

	// Rendered into a clean directory, not CopyDir'd: a blind directory copy
	// would ship default.json.tmpl itself (a file no app can parse) and, on a
	// second call with a different current_theme, leave a stale <old>.json
	// behind alongside the new one (cycle doc Step 4). Unlike OpenCode's
	// themes/ dir, nothing else in ~/.claude ever gets wiped, so this
	// directory needs its own clean-slate treatment here.
	themesDest := filepath.Join(paths.Paths.Config.Claude, "themes")
	if err := os.RemoveAll(themesDest); err != nil {
		return fmt.Errorf("failed to clear claude themes: %w", err)
	}
	if err := os.MkdirAll(themesDest, 0o755); err != nil {
		return fmt.Errorf("failed to create claude themes directory: %w", err)
	}
	if err := files.GenerateFromTemplate(
		filepath.Join(paths.Paths.App.Configs.Claude, "themes", "default.json.tmpl"),
		filepath.Join(themesDest, def.Name+".json"),
		map[string]any{"Palette": palette},
	); err != nil {
		return fmt.Errorf("failed to render claude theme: %w", err)
	}

	if err := baseapp.MaintainScratchDir(); err != nil {
		return fmt.Errorf("failed to maintain scratch dir: %w", err)
	}

	// The hook wrapper (format.sh) delegates to this shared script.
	if err := baseapp.EnsureFormatScript(); err != nil {
		return fmt.Errorf("failed to ensure the format script: %w", err)
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

// ExecuteCommand runs the claude CLI. Stdin is disconnected: a plugin or
// marketplace command run by an installer must fail rather than sit waiting
// on a prompt nobody can see.
func (c *Claude) ExecuteCommand(args ...string) error {
	_, err := c.ExecuteCommandOutput(args...)
	return err
}

// ExecuteCommandOutput is ExecuteCommand returning stdout, for commands that
// are read for their answer (`claude plugin list --json`). The error carries
// the CLI's stderr, which is where it says what went wrong.
func (c *Claude) ExecuteCommandOutput(args ...string) (string, error) {
	stdout, stderr, err := c.Base.ExecCommand(cmd.CommandParams{
		Command: constants.Claude,
		Args:    args,
		NoStdin: true,
	})
	if err != nil {
		if stderr != "" {
			return "", fmt.Errorf("claude command execution failed: %s: %w", strings.TrimSpace(stderr), err)
		}
		return "", fmt.Errorf("claude command execution failed: %w", err)
	}
	return stdout, nil
}

func (c *Claude) Update() error {
	return fmt.Errorf(
		"%w — re-run: curl -fsSL https://claude.ai/install.sh | bash",
		apps.ErrUpdateNotSupported,
	)
}
