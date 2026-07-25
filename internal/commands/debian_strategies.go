package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/cjairm/devgeta/pkg/apt"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/downloader"
	"github.com/cjairm/devgeta/pkg/logger"
)

// InstallGitHubBinary downloads binaryName from a GitHub release tar.gz, verifies
// its SHA-256 against the release's checksums file, extracts the root-level binary,
// and installs it to /usr/local/bin/<binaryName> with 755 permissions.
//
// checksumsURL must point to the release's sha256sum-format checksums file (the
// conventional checksums.txt asset); the archive's expected hash is looked up by
// its file name (path.Base of archiveURL). Verification is mandatory — the binary
// is installed with sudo, so an unverifiable download is refused outright
// (CLAUDE.md §4: never execute arbitrary downloaded code without verification).
//
// downloadFn is injectable for tests; pass nil to use the default retry downloader.
func InstallGitHubBinary(
	base BaseCommandExecutor,
	binaryName string,
	archiveURL string,
	checksumsURL string,
	downloadFn func(ctx context.Context, url, dest string, cfg downloader.RetryConfig) error,
) error {
	if downloadFn == nil {
		downloadFn = downloader.DownloadFileWithRetry
	}

	tarPath := filepath.Join("/tmp", binaryName+".tar.gz")
	checksumsPath := filepath.Join("/tmp", binaryName+"-checksums.txt")
	extractDir := filepath.Join("/tmp", binaryName+"-extract")
	defer os.Remove(tarPath)
	defer os.Remove(checksumsPath)
	defer os.RemoveAll(extractDir)

	ctx := context.Background()
	if err := downloadFn(ctx, archiveURL, tarPath, downloader.DefaultRetryConfig()); err != nil {
		return fmt.Errorf("failed to download %s: %w", binaryName, err)
	}

	if err := downloadFn(
		ctx,
		checksumsURL,
		checksumsPath,
		downloader.DefaultRetryConfig(),
	); err != nil {
		return fmt.Errorf(
			"failed to download checksums for %s — refusing to install unverified binary: %w",
			binaryName, err,
		)
	}

	if err := verifySHA256(tarPath, checksumsPath, path.Base(archiveURL)); err != nil {
		return fmt.Errorf(
			"%s failed checksum verification — refusing to install: %w",
			binaryName,
			err,
		)
	}

	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return fmt.Errorf("failed to create extract directory: %w", err)
	}

	if _, stderr, err := base.ExecCommand(CommandParams{
		Command: "tar",
		Args:    []string{"-xf", tarPath, "-C", extractDir, binaryName},
	}); err != nil {
		return fmt.Errorf("failed to extract %s: %w\nOutput: %s", binaryName, err, stderr)
	}

	binaryPath := filepath.Join(extractDir, binaryName)
	if _, stderr, err := base.ExecCommand(CommandParams{
		Command: "install",
		Args:    []string{"-m", "755", binaryPath, "/usr/local/bin/" + binaryName},
		IsSudo:  true,
	}); err != nil {
		return fmt.Errorf("failed to install %s binary: %w\nOutput: %s", binaryName, err, stderr)
	}

	return nil
}

// verifySHA256 checks that the SHA-256 of filePath matches the entry for
// assetName in a sha256sum-format checksums file (lines of
// "<hex-hash>  <file name>"; a leading "*" on the name marks sha256sum's
// binary mode and is ignored).
func verifySHA256(filePath, checksumsPath, assetName string) error {
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return fmt.Errorf("failed to read checksums file: %w", err)
	}

	var expected string
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if strings.TrimPrefix(fields[1], "*") == assetName {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("no checksum entry for %s", assetName)
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open %s for hashing: %w", filePath, err)
	}
	// Close error is non-actionable for a read-only file.
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("failed to hash %s: %w", filePath, err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf(
			"SHA-256 mismatch for %s: expected %s, got %s",
			assetName, expected, actual,
		)
	}
	return nil
}

// InstallationStrategy defines the contract for different package installation methods
type InstallationStrategy interface {
	// Install installs the package using the strategy's specific method
	Install(packageName string) error

	// IsInstalled checks if the package is already installed
	IsInstalled(packageName string) (bool, error)
}

// AptStrategy implements installation via apt package manager with package name translation
type AptStrategy struct {
	cmd *DebianCommand
}

// Install installs a package using apt after translating the package name
func (s *AptStrategy) Install(packageName string) error {
	// Translate package name using mapping (e.g., gdbm -> libgdbm-dev)
	debianName := constants.GetDebianPackageName(packageName)

	logger.L().Infow(
		"Installing package via apt",
		"original_name", packageName,
		"debian_name", debianName,
	)

	return s.cmd.installWithApt(debianName)
}

// IsInstalled checks if a package is installed using dpkg
func (s *AptStrategy) IsInstalled(packageName string) (bool, error) {
	debianName := constants.GetDebianPackageName(packageName)
	return s.cmd.IsPackageInstalled(debianName)
}

// PPAStrategy implements installation via PPA (Personal Package Archive)
type PPAStrategy struct {
	cmd       *DebianCommand
	ppaConfig apt.PPAConfig
}

// Install adds the PPA and then installs the package
func (s *PPAStrategy) Install(packageName string) error {
	logger.L().Infow(
		"Installing package via PPA",
		"package", packageName,
		"ppa", s.ppaConfig.Name,
	)

	// Add PPA repository
	ppaManager := apt.NewPPAManager()
	if err := ppaManager.AddPPA(s.ppaConfig); err != nil {
		return fmt.Errorf("failed to add PPA: %w", err)
	}

	// Install package via apt
	return s.cmd.installWithApt(packageName)
}

// IsInstalled checks if a package is installed
func (s *PPAStrategy) IsInstalled(packageName string) (bool, error) {
	return s.cmd.IsPackageInstalled(packageName)
}

// LaunchpadPPAStrategy installs a package from a Launchpad PPA using add-apt-repository.
// Use this instead of PPAStrategy for ppa:owner/name style repositories, which require
// add-apt-repository to handle key import and repo setup correctly.
type LaunchpadPPAStrategy struct {
	cmd    *DebianCommand
	ppaRef string // e.g., "ppa:zhangsongcui3371/fastfetch"
}

// Install adds the Launchpad PPA and installs the package
func (s *LaunchpadPPAStrategy) Install(packageName string) error {
	logger.L().Infow(
		"Installing package via Launchpad PPA",
		"package", packageName,
		"ppa", s.ppaRef,
	)

	// Ensure software-properties-common is present (provides add-apt-repository)
	if _, stderr, err := s.cmd.ExecCommand(CommandParams{
		Command: "apt",
		Args:    []string{"install", "-y", "software-properties-common"},
		IsSudo:  true,
	}); err != nil {
		return fmt.Errorf(
			"failed to install software-properties-common: %w\nOutput: %s",
			err,
			stderr,
		)
	}

	if _, stderr, err := s.cmd.ExecCommand(CommandParams{
		Command: "add-apt-repository",
		Args:    []string{"-y", s.ppaRef},
		IsSudo:  true,
	}); err != nil {
		return fmt.Errorf("failed to add Launchpad PPA %s: %w\nOutput: %s", s.ppaRef, err, stderr)
	}

	if _, stderr, err := s.cmd.ExecCommand(CommandParams{
		Command: "apt",
		Args:    []string{"update"},
		IsSudo:  true,
	}); err != nil {
		return fmt.Errorf("apt update failed after adding PPA: %w\nOutput: %s", err, stderr)
	}

	return s.cmd.installWithApt(packageName)
}

// IsInstalled checks if the package is installed
func (s *LaunchpadPPAStrategy) IsInstalled(packageName string) (bool, error) {
	return s.cmd.IsPackageInstalled(packageName)
}

// InstallScriptStrategy implements installation by downloading and executing an install script
type InstallScriptStrategy struct {
	cmd       *DebianCommand
	scriptURL string
}

// Install downloads and executes an install script via curl | sh
func (s *InstallScriptStrategy) Install(packageName string) error {
	logger.L().Infow(
		"Installing package via install script",
		"package", packageName,
		"script_url", s.scriptURL,
	)

	curlCmd := exec.Command("sh", "-c", fmt.Sprintf("curl -fsSL %s | sh", s.scriptURL))
	output, err := curlCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"install script failed for %s: %w\nOutput: %s",
			packageName,
			err,
			string(output),
		)
	}

	return nil
}

// IsInstalled checks if the package binary exists in common PATH locations
func (s *InstallScriptStrategy) IsInstalled(packageName string) (bool, error) {
	_, err := exec.LookPath(packageName)
	if err != nil {
		return false, nil
	}
	return true, nil
}

// NerdFontStrategy implements installation by downloading Nerd Font archives from GitHub releases
type NerdFontStrategy struct {
	cmd        *DebianCommand
	archiveURL string // Full GitHub release URL for the tar.xz archive
}

// Install downloads a Nerd Font tar.xz archive, extracts fonts to ~/.local/share/fonts/,
// and runs fc-cache to register them
func (s *NerdFontStrategy) Install(packageName string) error {
	logger.L().Infow(
		"Installing Nerd Font from GitHub releases",
		"package", packageName,
		"url", s.archiveURL,
	)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	fontsDir := filepath.Join(homeDir, ".local", "share", "fonts")
	if err := os.MkdirAll(fontsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create fonts directory: %w", err)
	}

	// Download the tar.xz archive with retry
	tmpArchive := filepath.Join("/tmp", fmt.Sprintf("%s-nerd-font.tar.xz", packageName))
	defer os.Remove(tmpArchive)

	ctx := context.Background()
	config := downloader.DefaultRetryConfig()
	if err := downloader.DownloadFileWithRetry(ctx, s.archiveURL, tmpArchive, config); err != nil {
		return fmt.Errorf("failed to download font archive: %w", err)
	}

	// Extract .tar.xz to fonts directory
	extractCmd := exec.Command("tar", "-xf", tmpArchive, "-C", fontsDir)
	if output, err := extractCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to extract font archive: %w\nOutput: %s", err, string(output))
	}

	// Update font cache
	cacheCmd := exec.Command("fc-cache", "-fv")
	if output, err := cacheCmd.CombinedOutput(); err != nil {
		logger.L().Warnw("fc-cache failed (non-fatal)", "error", err, "output", string(output))
	}

	logger.L().Infow("Nerd Font installed successfully", "package", packageName)
	return nil
}

// IsInstalled checks if the font is present using fc-list
func (s *NerdFontStrategy) IsInstalled(packageName string) (bool, error) {
	return s.cmd.IsFontPresent(packageName)
}

// GitCloneStrategy implements installation by cloning a Git repository
type GitCloneStrategy struct {
	cmd         *DebianCommand
	repoURL     string
	installPath string
}

// Install clones a Git repository to the specified path
func (s *GitCloneStrategy) Install(packageName string) error {
	logger.L().Infow(
		"Installing package via Git clone",
		"package", packageName,
		"repo", s.repoURL,
		"path", s.installPath,
	)

	// Check if already cloned
	if _, err := os.Stat(s.installPath); err == nil {
		logger.L().Infow("Repository already cloned", "path", s.installPath)
		return nil
	}

	// Clone repository
	cmd := exec.Command("git", "clone", "--depth", "1", s.repoURL, s.installPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}

// IsInstalled checks if the repository is already cloned
func (s *GitCloneStrategy) IsInstalled(packageName string) (bool, error) {
	_, err := os.Stat(s.installPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
