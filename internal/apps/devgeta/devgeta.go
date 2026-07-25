package devgeta

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/embedded"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
)

var _ apps.App = (*Devgeta)(nil)

const DevgetaExtended = "extended_capabilities"

type Devgeta struct {
	Base            commands.BaseCommandExecutor
	ExtractEmbedded embedded.ExtractFunc
}

func (dg *Devgeta) Name() string       { return constants.DevgetaApp }
func (dg *Devgeta) Kind() apps.AppKind { return apps.KindMeta }

func getConfigDirPath() string {
	return filepath.Join(paths.Paths.Config.Root, constants.App.Name)
}

func getGlobalConfigPath() string {
	return filepath.Join(getConfigDirPath(), constants.App.File.GlobalConfig)
}

func getZshConfigPath() string {
	return filepath.Join(paths.Paths.App.Root, fmt.Sprintf("%s.zsh", constants.App.Name))
}

func getZshenvScriptPath() string {
	return filepath.Join(paths.Paths.App.Root, "configs", "zsh", "zshenv.zsh")
}

// setupZshenv wires devgeta's PATH self-repair script (configs/zsh/zshenv.zsh)
// into the user's ~/.zshenv. ~/.zshenv runs before /etc/zshrc for every zsh —
// login or not — which is what lets it fix a PATH a non-login tmux pane
// inherited broken, before oh-my-zsh/p10k get a chance to spew "command not
// found". The "[ -f ... ] &&" guard means uninstalling devgeta leaves a dead
// line instead of a broken shell startup. Only zsh reads ~/.zshenv, so bash
// users are skipped — wiring it in would just create a file nothing sources.
func (dg *Devgeta) setupZshenv() error {
	if filepath.Base(paths.Files.ShellConfig) != ".zshrc" {
		return nil
	}
	scriptPath := getZshenvScriptPath()
	line := fmt.Sprintf(`[ -f "%s" ] && source "%s"`, scriptPath, scriptPath)
	return dg.Base.MaybeSetupInFile(line, scriptPath, paths.Files.ZshEnv)
}

func New() *Devgeta {
	return &Devgeta{
		Base:            commands.NewBaseCommand(),
		ExtractEmbedded: embedded.DefaultExtractor,
	}
}

func (dg *Devgeta) Install() error {
	// Create configs directory inside app root
	configsDir := filepath.Join(paths.Paths.App.Root, "configs")

	// Clean up existing configs directory if it exists
	if files.DirAlreadyExist(configsDir) {
		if err := os.RemoveAll(configsDir); err != nil {
			return fmt.Errorf("failed to remove existing configs directory: %w", err)
		}
	}

	// Extract embedded configs to the app directory
	if err := dg.ExtractEmbedded(configsDir); err != nil {
		return fmt.Errorf("failed to extract embedded configs: %w", err)
	}

	return nil
}

func (dg *Devgeta) SoftInstall() error {
	configsDir := filepath.Join(paths.Paths.App.Root, "configs")

	// Check if configs/ subdirectory exists and is non-empty
	if files.DirAlreadyExist(configsDir) && !files.IsDirEmpty(configsDir) {
		logger.L().Infow("Devgeta configs already installed", "path", configsDir)
		return nil
	}

	return dg.Install()
}

func (dg *Devgeta) ForceInstall() error {
	return baseapp.Reinstall(dg.Install, dg.Uninstall)
}

func (dg *Devgeta) Uninstall() error {
	// Clean up extracted configs directory
	configsDir := filepath.Join(paths.Paths.App.Root, "configs")
	if files.DirAlreadyExist(configsDir) {
		logger.L().Debugw("Removing extracted configs directory", "path", configsDir)
		if err := os.RemoveAll(configsDir); err != nil {
			return fmt.Errorf("failed to remove configs directory: %w", err)
		}
	} else {
		logger.L().Debugw("Configs directory not found", "path", configsDir)
	}

	// Remove app root directory if empty
	if files.DirAlreadyExist(paths.Paths.App.Root) {
		if files.IsDirEmpty(paths.Paths.App.Root) {
			logger.L().Debugw("App directory is empty, removing", "path", paths.Paths.App.Root)
			if err := os.Remove(paths.Paths.App.Root); err != nil {
				return fmt.Errorf("failed to remove empty app directory: %w", err)
			}
		}
	}

	// Remove global config file
	if files.FileAlreadyExist(getGlobalConfigPath()) {
		logger.L().Debugw("Removing global config file", "path", getGlobalConfigPath())
		if err := os.Remove(getGlobalConfigPath()); err != nil {
			return fmt.Errorf("failed to remove global config file: %w", err)
		}
	} else {
		logger.L().Debugw("Global config file not found", "path", getGlobalConfigPath())
	}

	// Remove zsh config file
	if files.FileAlreadyExist(getZshConfigPath()) {
		logger.L().Debugw("Removing zsh config file", "path", getZshConfigPath())
		if err := os.Remove(getZshConfigPath()); err != nil {
			return fmt.Errorf("failed to remove zsh config file: %w", err)
		}
	} else {
		logger.L().Debugw("zsh file not found", "path", getZshConfigPath())
	}

	// Remove config directory if empty
	if files.DirAlreadyExist(getConfigDirPath()) && files.IsDirEmpty(getConfigDirPath()) {
		logger.L().Debugw("Config directory is empty, removing", "path", getConfigDirPath())
		if err := os.Remove(getConfigDirPath()); err != nil {
			return fmt.Errorf("failed to remove empty config directory: %w", err)
		}
	}

	return nil
}

func (dg *Devgeta) ForceConfigure() error {
	// Re-extract embedded configs so deployed configs/claude, configs/opencode, etc.
	// always match the current binary (not the original install).
	if err := dg.Install(); err != nil {
		return fmt.Errorf("failed to refresh embedded configs: %w", err)
	}

	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.AppPath = paths.Paths.App.Root
	gc.ConfigPath = getConfigDirPath()
	// Auto-detect installed tools so the shell config reflects reality,
	// even if global_config.yaml lost its shell feature flags.
	gc.ReconcileShellFeatures()
	gc.EnableShellFeature(DevgetaExtended)
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	// NOTE: This should be regenerated per app, but creating it just in case
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to create global config file: %w", err)
	}
	devgetaConfigLine := fmt.Sprintf(`source "%s"`, getZshConfigPath())
	if err := dg.Base.MaybeSetup(devgetaConfigLine, getZshConfigPath()); err != nil {
		return err
	}
	if err := dg.setupZshenv(); err != nil {
		return fmt.Errorf("failed to wire PATH self-repair into ~/.zshenv: %w", err)
	}
	logger.L().Infow("Shell config regenerated", "path", getZshConfigPath())
	fmt.Printf("Run `source %s` to apply shell changes.\n", getZshConfigPath())
	return nil
}

func (dg *Devgeta) SoftConfigure() error {
	// Cheap and idempotent, so existing installs pick up the PATH self-repair
	// wiring on a plain `dg configure`, not just a fresh `--force` one.
	if err := dg.setupZshenv(); err != nil {
		return fmt.Errorf("failed to wire PATH self-repair into ~/.zshenv: %w", err)
	}
	if !files.FileAlreadyExist(getGlobalConfigPath()) ||
		!files.FileAlreadyExist(getZshConfigPath()) {
		return dg.ForceConfigure()
	}
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if !gc.IsShellFeatureEnabled(DevgetaExtended) {
		if err := enableExtendedCapabilities(gc); err != nil {
			return fmt.Errorf("failed to enable extended capabilities: %w", err)
		}
	}
	return nil
}

func (dg *Devgeta) ExecuteCommand(_ ...string) error {
	return fmt.Errorf("%w for devgeta", apps.ErrExecuteNotSupported)
}

func (dg *Devgeta) Update() error {
	return fmt.Errorf("%w for devgeta", apps.ErrUpdateNotSupported)
}

func enableExtendedCapabilities(gc *config.GlobalConfig) error {
	gc.EnableShellFeature(DevgetaExtended)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}
