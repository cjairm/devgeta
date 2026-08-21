package apt

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cjairm/devgeta/pkg/downloader"
	"github.com/cjairm/devgeta/pkg/logger"
)

// PPAConfig defines the configuration for a Debian/Ubuntu PPA (Personal Package Archive)
type PPAConfig struct {
	Name         string // PPA identifier (e.g., "mise")
	KeyURL       string // GPG public key URL
	RepoURL      string // Repository base URL
	Distribution string // Distribution codename or version (e.g., "stable")
	Component    string // Repository component (e.g., "main")
	Architecture string // Target architecture (auto-detected if empty)
}

// commandRunner executes an external command and returns its output.
type commandRunner func(name string, args ...string) ([]byte, error)

// fileDownloader fetches url and stores the response body at destPath.
type fileDownloader func(ctx context.Context, url, destPath string) error

// PPAManager handles PPA installation and management
type PPAManager struct {
	// run executes a command and returns its combined output. Nil means "use the
	// real implementation"; tests substitute a recorder so that no PPA operation —
	// all of which shell out to sudo — ever runs for real.
	run commandRunner
	// capture executes a command and returns only its stdout, for commands whose
	// output is parsed and would be corrupted by interleaved stderr.
	capture commandRunner
	// download fetches a remote file. Nil means "use the real implementation".
	download fileDownloader
}

// NewPPAManager creates a new PPA manager instance
func NewPPAManager() *PPAManager {
	return &PPAManager{}
}

// runner returns the command runner to use for side-effecting commands.
func (pm *PPAManager) runner() commandRunner {
	if pm.run != nil {
		return pm.run
	}
	return func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}
}

// capturer returns the command runner to use when stdout is parsed.
func (pm *PPAManager) capturer() commandRunner {
	if pm.capture != nil {
		return pm.capture
	}
	return func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).Output()
	}
}

// downloader returns the file downloader to use.
func (pm *PPAManager) downloader() fileDownloader {
	if pm.download != nil {
		return pm.download
	}
	return func(ctx context.Context, url, destPath string) error {
		return downloader.DownloadFileWithRetry(ctx, url, destPath, downloader.DefaultRetryConfig())
	}
}

// AddPPA adds a PPA repository to the system
// This function is idempotent - it checks if the PPA is already configured before making changes
func (pm *PPAManager) AddPPA(config PPAConfig) error {
	// Validate configuration
	if config.Name == "" || config.KeyURL == "" || config.RepoURL == "" {
		return fmt.Errorf("invalid PPA config: Name, KeyURL, and RepoURL are required")
	}

	// Set default values
	if config.Distribution == "" {
		config.Distribution = "stable"
	}
	if config.Component == "" {
		config.Component = "main"
	}

	// Auto-detect architecture if not specified
	if config.Architecture == "" {
		arch, err := pm.detectArchitecture()
		if err != nil {
			return fmt.Errorf("failed to detect architecture: %w", err)
		}
		config.Architecture = arch
	}

	// Derive paths
	keyringPath := fmt.Sprintf("/etc/apt/keyrings/%s-archive-keyring.gpg", config.Name)
	sourcesFile := fmt.Sprintf("/etc/apt/sources.list.d/%s.list", config.Name)

	// Check if already configured
	if fileExists(sourcesFile) {
		logger.L().Infow("PPA already configured", "name", config.Name, "sources_file", sourcesFile)
		return nil
	}

	logger.L().Infow("Adding PPA", "name", config.Name)

	// Step 1: Install prerequisites (gpg, wget, curl)
	if err := pm.installPrerequisites(); err != nil {
		return fmt.Errorf("failed to install prerequisites: %w", err)
	}

	// Step 2: Create keyring directory
	if err := pm.createKeyringDir(); err != nil {
		return fmt.Errorf("failed to create keyring directory: %w", err)
	}

	// Step 3: Download and install GPG key
	if err := pm.installGPGKey(config.KeyURL, keyringPath); err != nil {
		return fmt.Errorf("failed to install GPG key: %w", err)
	}

	// Step 4: Create repository entry
	repoEntry := fmt.Sprintf("deb [signed-by=%s arch=%s] %s %s %s",
		keyringPath, config.Architecture, config.RepoURL, config.Distribution, config.Component)

	if err := pm.createRepositoryEntry(repoEntry, sourcesFile); err != nil {
		return fmt.Errorf("failed to create repository entry: %w", err)
	}

	// Step 5: Update apt cache
	if err := pm.updateAptCache(); err != nil {
		return fmt.Errorf("failed to update apt cache: %w", err)
	}

	logger.L().Infow("PPA added successfully", "name", config.Name)
	return nil
}

// detectArchitecture detects the system architecture using dpkg
func (pm *PPAManager) detectArchitecture() (string, error) {
	output, err := pm.capturer()("dpkg", "--print-architecture")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// installPrerequisites installs required tools (gpg, wget, curl)
func (pm *PPAManager) installPrerequisites() error {
	logger.L().Infow("Installing PPA prerequisites")
	output, err := pm.runner()("sudo", "apt", "install", "-y", "gpg", "wget", "curl")
	if err != nil {
		return fmt.Errorf("apt install failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// createKeyringDir creates the keyring directory with proper permissions
func (pm *PPAManager) createKeyringDir() error {
	logger.L().Infow("Creating keyring directory")
	output, err := pm.runner()("sudo", "install", "-dm", "755", "/etc/apt/keyrings")
	if err != nil {
		return fmt.Errorf("failed to create directory: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// installGPGKey downloads the PPA's public key and installs it as a binary keyring.
//
// The download happens in-process (pkg/downloader already carries the retry and
// backoff logic wget was standing in for) and gpg dearmors file-to-file, so every
// external tool runs as one self-contained process. That is deliberate: the
// earlier `wget | gpg --dearmor | sudo tee` pipeline hand-managed two os.Pipe
// pairs across five early returns, each of which leaked file descriptors and/or
// left a started child unreaped. Nothing here holds a pipe open, so no error path
// has anything to leak.
func (pm *PPAManager) installGPGKey(keyURL, keyringPath string) error {
	logger.L().Infow("Installing GPG key", "url", keyURL, "destination", keyringPath)

	return pm.withStagingDir("keyring", func(stagingDir string) error {
		armoredPath := filepath.Join(stagingDir, "key.asc")
		if err := pm.downloader()(context.Background(), keyURL, armoredPath); err != nil {
			return fmt.Errorf("failed to download GPG key from %s: %w", keyURL, err)
		}

		dearmoredPath := filepath.Join(stagingDir, "key.gpg")
		output, err := pm.runner()(
			"gpg", "--batch", "--yes", "--dearmor", "--output", dearmoredPath, armoredPath,
		)
		if err != nil {
			return fmt.Errorf("failed to dearmor GPG key: %w\nOutput: %s", err, string(output))
		}

		return pm.installAsRoot(dearmoredPath, keyringPath)
	})
}

// createRepositoryEntry creates the repository sources.list.d entry
//
// Same shape as installGPGKey, for the same reason: the entry is staged as a
// plain file and handed to install(1), replacing an `echo | sudo tee` pipeline
// whose two error paths leaked a pipe pair and orphaned a started tee.
func (pm *PPAManager) createRepositoryEntry(repoEntry, sourcesFile string) error {
	logger.L().Infow("Creating repository entry", "file", sourcesFile)

	return pm.withStagingDir("sources", func(stagingDir string) error {
		entryPath := filepath.Join(stagingDir, "entry.list")
		if err := os.WriteFile(entryPath, []byte(repoEntry+"\n"), 0o644); err != nil {
			return fmt.Errorf("failed to stage repository entry: %w", err)
		}
		return pm.installAsRoot(entryPath, sourcesFile)
	})
}

// installAsRoot copies a staged file to a root-owned destination.
// install(1) sets the mode explicitly; apt requires both keyrings and sources
// entries to be world-readable.
func (pm *PPAManager) installAsRoot(srcPath, destPath string) error {
	output, err := pm.runner()("sudo", "install", "-m", "644", srcPath, destPath)
	if err != nil {
		return fmt.Errorf("failed to install %s: %w\nOutput: %s", destPath, err, string(output))
	}
	return nil
}

// withStagingDir runs fn against a private temporary directory and removes that
// directory afterwards, on the failure path as well as the success path.
func (pm *PPAManager) withStagingDir(purpose string, fn func(stagingDir string) error) error {
	stagingDir, err := os.MkdirTemp("", "devgeta-ppa-"+purpose+"-")
	if err != nil {
		return fmt.Errorf("failed to create staging directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(stagingDir); err != nil {
			logger.L().Warnw("Failed to remove staging directory", "path", stagingDir, "error", err)
		}
	}()

	return fn(stagingDir)
}

// updateAptCache updates the apt package cache
func (pm *PPAManager) updateAptCache() error {
	logger.L().Infow("Updating apt cache")
	output, err := pm.runner()("sudo", "apt", "update")
	if err != nil {
		return fmt.Errorf("apt update failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return err == nil && !info.IsDir()
}

// GetKeyringPath returns the keyring path for a PPA
func GetKeyringPath(ppaName string) string {
	return filepath.Join("/etc/apt/keyrings", fmt.Sprintf("%s-archive-keyring.gpg", ppaName))
}

// GetSourcesPath returns the sources.list.d path for a PPA
func GetSourcesPath(ppaName string) string {
	return filepath.Join("/etc/apt/sources.list.d", fmt.Sprintf("%s.list", ppaName))
}
