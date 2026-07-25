// -------------------------
// TODO: Write documentation how to use this
// - Kickstart documentation: https://github.com/nvim-lua/kickstart.nvim?tab=readme-ov-file
// - Personal configuration: https://github.com/cjairm/devenv/blob/main/nvim/init.lua
// - Releases: https://github.com/neovim/neovim/releases
// - Download app directly instead of using `brew`?
//
// NOTE: install different themes...?
// -------------------------

package neovim

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/downloader"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
)

var _ apps.App = (*Neovim)(nil)

type Neovim struct {
	Cmd        cmd.Command
	Base       cmd.BaseCommandExecutor
	downloadFn func(ctx context.Context, url, dest string, cfg downloader.RetryConfig) error // injectable for tests
}

func (n *Neovim) Name() string       { return constants.Neovim }
func (n *Neovim) Kind() apps.AppKind { return apps.KindTerminal }

func (n *Neovim) doDownload(ctx context.Context, url, dest string, cfg downloader.RetryConfig) error {
	if n.downloadFn != nil {
		return n.downloadFn(ctx, url, dest, cfg)
	}
	return downloader.DownloadFileWithRetry(ctx, url, dest, cfg)
}

func New() *Neovim {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Neovim{Cmd: osCmd, Base: baseCmd}
}

func (n *Neovim) Install() error {
	if err := InstallDeps(n.Base, n.Cmd); err != nil {
		return fmt.Errorf("failed to install neovim dependencies: %w", err)
	}
	if n.Base.IsMac() {
		return n.Cmd.InstallPackage(constants.Neovim)
	}
	return n.installDebianNeovim()
}

func (n *Neovim) ForceInstall() error {
	return baseapp.Reinstall(n.Install, n.Uninstall)
}

func (n *Neovim) SoftInstall() error {
	if err := InstallDeps(n.Base, n.Cmd); err != nil {
		return fmt.Errorf("failed to install neovim dependencies: %w", err)
	}
	if n.Base.IsMac() {
		return n.Cmd.MaybeInstallPackage(constants.Neovim)
	}
	// On Debian: check if nvim binary exists with sufficient version before installing
	if n.isDebianNeovimInstalled() {
		logger.L().Infow("Neovim already installed with sufficient version, skipping")
		return nil
	}
	return n.installDebianNeovim()
}

// isDebianNeovimInstalled checks if nvim binary exists and meets the minimum version requirement
func (n *Neovim) isDebianNeovimInstalled() bool {
	if _, err := cmd.LookPathFn("nvim"); err != nil {
		return false
	}
	return n.checkVersion() == nil
}

// nvimLinuxArch returns the architecture string used in neovim release artifact names.
func nvimLinuxArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "x86_64"
}

// installDebianNeovim downloads the neovim Linux tar.gz for the current architecture
// from GitHub releases, extracts it, installs the binary to /usr/local/bin/nvim,
// and copies lib/share to /usr/local/
func (n *Neovim) installDebianNeovim() error {
	version := constants.SupportedVersion.Neovim.Number
	arch := nvimLinuxArch()
	artifactName := fmt.Sprintf("nvim-linux-%s", arch)
	url := fmt.Sprintf(
		"https://github.com/neovim/neovim/releases/download/v%s/%s.tar.gz",
		version, artifactName,
	)
	tarPath := fmt.Sprintf("/tmp/%s.tar.gz", artifactName)
	extractDir := fmt.Sprintf("/tmp/%s-extract", artifactName)

	defer os.Remove(tarPath)
	defer os.RemoveAll(extractDir)

	logger.L().Infow("Downloading Neovim for Debian", "version", version, "arch", arch, "url", url)

	ctx := context.Background()
	if err := n.doDownload(ctx, url, tarPath, downloader.DefaultRetryConfig()); err != nil {
		return fmt.Errorf("failed to download neovim: %w", err)
	}

	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return fmt.Errorf("failed to create extract directory: %w", err)
	}

	if _, stderr, err := n.Base.ExecCommand(cmd.CommandParams{
		Command: "tar",
		Args:    []string{"-xf", tarPath, "-C", extractDir},
	}); err != nil {
		return fmt.Errorf("failed to extract neovim: %w\nOutput: %s", err, stderr)
	}

	// Install binary to /usr/local/bin/nvim with 755 permissions
	binaryPath := filepath.Join(extractDir, artifactName, "bin", "nvim")
	if _, stderr, err := n.Base.ExecCommand(cmd.CommandParams{
		Command: "install",
		Args:    []string{"-m", "755", binaryPath, "/usr/local/bin/nvim"},
		IsSudo:  true,
	}); err != nil {
		return fmt.Errorf("failed to install neovim binary: %w\nOutput: %s", err, stderr)
	}

	// Copy lib/ and share/ to /usr/local/
	for _, dir := range []string{"lib", "share"} {
		srcDir := filepath.Join(extractDir, artifactName, dir)
		if _, stderr, err := n.Base.ExecCommand(cmd.CommandParams{
			Command: "cp",
			Args:    []string{"-r", srcDir, "/usr/local/"},
			IsSudo:  true,
		}); err != nil {
			return fmt.Errorf("failed to copy %s to /usr/local/: %w\nOutput: %s", dir, err, stderr)
		}
	}

	logger.L().Infow("Neovim installed successfully for Debian", "version", version, "arch", arch)
	return nil
}

func (n *Neovim) ForceConfigure() error {
	if err := n.checkVersion(); err != nil {
		return fmt.Errorf("failed to check Neovim version: %w", err)
	}
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.ReconcileShellFeatures()
	gc.AddToInstalled(constants.Neovim, "package")
	if err := enableFeature(gc); err != nil {
		return fmt.Errorf("failed to enable neovim feature: %w", err)
	}
	return files.CopyDir(paths.Paths.App.Configs.Neovim, paths.Paths.Config.Nvim)
}

func (n *Neovim) SoftConfigure() error {
	isDirPresent := files.DirAlreadyExist(paths.Paths.Config.Nvim)
	isDirEmpty := files.IsDirEmpty(paths.Paths.Config.Nvim)
	if isDirPresent && !isDirEmpty {
		gc := &config.GlobalConfig{}
		if err := gc.Create(); err != nil {
			return fmt.Errorf("failed to create global config: %w", err)
		}
		if err := gc.Load(); err != nil {
			return fmt.Errorf("failed to load global config: %w", err)
		}
		if !gc.IsShellFeatureEnabled(constants.Neovim) {
			if err := enableFeature(gc); err != nil {
				return fmt.Errorf("failed to enable neovim feature: %w", err)
			}
		}
		return nil
	}
	return n.ForceConfigure()
}

func (n *Neovim) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if n.Base.IsMac() {
		if err := n.Cmd.UninstallPackage(constants.Neovim); err != nil {
			return fmt.Errorf("failed to uninstall neovim: %w", err)
		}
	} else {
		for _, rmArgs := range [][]string{
			{"-f", "/usr/local/bin/nvim"},
			{"-rf", "/usr/local/lib/nvim"},
			{"-rf", "/usr/local/share/nvim"},
		} {
			if _, _, err := n.Base.ExecCommand(cmd.CommandParams{
				Command: "rm",
				Args:    rmArgs,
				IsSudo:  true,
			}); err != nil {
				return fmt.Errorf("failed to remove neovim files (%v): %w", rmArgs, err)
			}
		}
	}
	_ = os.RemoveAll(paths.Paths.Config.Nvim)
	for _, dir := range []string{
		paths.GetDataDir(constants.Nvim),
		paths.GetStateDir(constants.Nvim),
		paths.GetCacheDir(constants.Nvim),
	} {
		_ = os.RemoveAll(dir)
	}
	gc.DisableShellFeature(constants.Neovim)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to regenerate shell config: %w", err)
	}
	gc.RemoveFromInstalled(constants.Neovim, "package")
	return gc.Save()
}

func (n *Neovim) ExecuteCommand(args ...string) error {
	baseCmd := getBaseCmd(args...)
	_, stderr, err := n.Base.ExecCommand(baseCmd)
	if err != nil {
		return fmt.Errorf("failed to check neovim version: %w, stderr: %s", err, stderr)
	}
	return nil
}

func (n *Neovim) Update() error {
	return fmt.Errorf("%w for neovim", apps.ErrUpdateNotSupported)
}

func (n *Neovim) checkVersion() error {
	baseCmd := getBaseCmd("--version")
	stdout, stderr, err := n.Base.ExecCommand(baseCmd)
	if err != nil {
		return fmt.Errorf("failed to check neovim version: %w, stderr: %s", err, stderr)
	}
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		line := scanner.Text()
		if versionStr, found := strings.CutPrefix(line, "NVIM v"); found {
			versionStr = strings.Fields(versionStr)[0]
			if isVersionEqualOrHigher(versionStr, constants.SupportedVersion.Neovim.Number) {
				return nil
			}
			return fmt.Errorf(
				"neovim version %s is too old, requires %s",
				versionStr,
				constants.SupportedVersion.Neovim.Number,
			)
		}
	}
	return fmt.Errorf("could not parse Neovim version from output")
}

func enableFeature(gc *config.GlobalConfig) error {
	gc.EnableShellFeature(constants.Neovim)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}

func isVersionEqualOrHigher(currentVersion, requiredVersion string) bool {
	currentParts := strings.Split(currentVersion, ".")
	requiredParts := strings.Split(requiredVersion, ".")
	for i, requiredPartStr := range requiredParts {
		if i >= len(currentParts) {
			return false // Current version has fewer parts
		}
		currentPart, err := strconv.Atoi(currentParts[i])
		if err != nil {
			return false
		}
		requiredPart, err := strconv.Atoi(requiredPartStr)
		if err != nil {
			return false
		}
		if currentPart < requiredPart {
			return false
		}
	}
	return true
}

func getBaseCmd(args ...string) cmd.CommandParams {
	return cmd.CommandParams{
		Command: constants.Nvim,
		Args:    args,
	}
}
