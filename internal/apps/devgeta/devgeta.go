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
	return filepath.Join(pointerPath(), "zsh", "zshenv.zsh")
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

// Install extracts the embedded configs and publishes them atomically.
//
// The tree is never rewritten in place: it is extracted to a stamped sibling
// directory and then committed by renaming a symlink over the `configs`
// pointer, so a reader always sees one complete tree or the other. See
// configs_pointer.go for the layout, the migration from the pre-pointer shape,
// and the validation that gates every removal.
func (dg *Devgeta) Install() error {
	root := paths.Paths.App.Root
	if err := os.MkdirAll(root, files.DirPermission); err != nil {
		return fmt.Errorf("failed to create app directory: %w", err)
	}

	// Resume an interrupted migration first, so everything below sees one of
	// the three settled shapes rather than a half-migrated tree.
	if err := reconcileLegacyMigration(); err != nil {
		return fmt.Errorf("failed to repair an interrupted configs migration: %w", err)
	}

	pointer := pointerPath()
	target := stampedDirPath()

	// The tree the pointer serves right now, so it can be collected after the
	// swap. Resolved before the extract, since the swap overwrites the link.
	var previous string
	if resolved, err := resolveManagedTarget(pointer); err == nil {
		previous = resolved
	}

	// Extracting over an existing directory of the same stamp is deliberate:
	// the stamp is version+commit, so the content is identical and a reader
	// racing the write sees the same bytes either way. It is also what
	// completes a tree an earlier run was interrupted midway through.
	if err := dg.ExtractEmbedded(target); err != nil {
		return fmt.Errorf("failed to extract embedded configs: %w", err)
	}

	if err := commitPointer(root, pointer, target); err != nil {
		return fmt.Errorf("failed to publish extracted configs: %w", err)
	}

	// Keep exactly one generation. Devgeta's readers re-resolve the pointer on
	// every read and an already-open descriptor outlives the unlink, so there
	// is nothing to retain the old tree for. A failure here is not fatal — the
	// sweep below collects it on the next run.
	//
	// Compare basenames, not full paths: `previous` came back from
	// EvalSymlinks and `target` did not, so on an app root reached through a
	// symlink (a /var/folders TMPDIR, a symlinked $HOME) the two spellings of
	// the same directory differ as strings — and a full-path comparison would
	// delete the tree this call just published. Both sit directly under the
	// app root, which resolveManagedTarget already established, so the
	// basename is the whole of the difference.
	if previous != "" && filepath.Base(previous) != filepath.Base(target) {
		if err := removeManagedTarget(previous); err != nil {
			logger.L().
				Warnw("Failed to remove the superseded config extract", "path", previous, "error", err)
		}
	}

	sweepStaleExtracts(root, filepath.Base(target))
	return nil
}

// InstallIfStale re-extracts only when the published tree does not belong to
// the running binary. It is what `dg configure` calls, so configuring one app
// no longer rewrites ~1.4 MB across 152 files first.
//
// It is deliberately not used by ForceConfigure: `--force` must stay a repair
// path for a hand-edited tree, which a stamp check would defeat.
func (dg *Devgeta) InstallIfStale() error {
	pointer := pointerPath()
	if pointerIsCurrent(pointer) {
		logger.L().
			Debugw("Embedded configs already match this build", "path", pointer, "stamp", buildStamp())
		return nil
	}
	return dg.Install()
}

func (dg *Devgeta) SoftInstall() error {
	configsDir := pointerPath()

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
	// Clean up the extracted configs. Removing the pointer alone would orphan
	// ~1.4 MB of extract under the app root (os.RemoveAll on a symlink drops
	// the link and leaves the directory), and the "remove the app root if
	// empty" branch below would then never fire.
	configsDir := pointerPath()
	if err := removeConfigsPointer(configsDir); err != nil {
		return err
	}
	// Any stamped extract still on disk — debris from an interrupted run, or a
	// generation a failed post-swap removal left behind — goes too.
	sweepStaleExtracts(paths.Paths.App.Root, "")
	if err := os.RemoveAll(legacyPath()); err != nil {
		return fmt.Errorf("failed to remove %s: %w", legacyPath(), err)
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
