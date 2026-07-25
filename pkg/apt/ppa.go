package apt

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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

// PPAManager handles PPA installation and management
type PPAManager struct {
	// Future: add state tracking if needed
}

// NewPPAManager creates a new PPA manager instance
func NewPPAManager() *PPAManager {
	return &PPAManager{}
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
	cmd := exec.Command("dpkg", "--print-architecture")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// Remove trailing newline
	arch := string(output)
	if len(arch) > 0 && arch[len(arch)-1] == '\n' {
		arch = arch[:len(arch)-1]
	}
	return arch, nil
}

// installPrerequisites installs required tools (gpg, wget, curl)
func (pm *PPAManager) installPrerequisites() error {
	logger.L().Infow("Installing PPA prerequisites")
	cmd := exec.Command("sudo", "apt", "install", "-y", "gpg", "wget", "curl")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apt install failed: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// createKeyringDir creates the keyring directory with proper permissions
func (pm *PPAManager) createKeyringDir() error {
	logger.L().Infow("Creating keyring directory")
	cmd := exec.Command("sudo", "install", "-dm", "755", "/etc/apt/keyrings")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create directory: %w\nOutput: %s", err, string(output))
	}
	return nil
}

// installGPGKey downloads and installs the GPG key
func (pm *PPAManager) installGPGKey(keyURL, keyringPath string) error {
	logger.L().Infow("Installing GPG key", "url", keyURL, "destination", keyringPath)

	// Download GPG key, dearmor, and save
	// wget -qO - {keyURL} | gpg --dearmor | sudo tee {keyringPath}
	wgetCmd := exec.Command("wget", "-qO", "-", keyURL)
	gpgCmd := exec.Command("gpg", "--dearmor")
	teeCmd := exec.Command("sudo", "tee", keyringPath)

	// Connect pipes — errors must be checked; a nil pipe silently produces an empty keyring
	wgetStdout, err := wgetCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create wget stdout pipe: %w", err)
	}
	gpgCmd.Stdin = wgetStdout

	gpgStdout, err := gpgCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create gpg stdout pipe: %w", err)
	}
	teeCmd.Stdin = gpgStdout

	// Start commands
	if err := teeCmd.Start(); err != nil {
		return fmt.Errorf("failed to start tee: %w", err)
	}
	if err := gpgCmd.Start(); err != nil {
		return fmt.Errorf("failed to start gpg: %w", err)
	}
	if err := wgetCmd.Run(); err != nil {
		return fmt.Errorf("failed to download GPG key: %w", err)
	}

	// Wait for commands to complete
	if err := gpgCmd.Wait(); err != nil {
		return fmt.Errorf("gpg command failed: %w", err)
	}
	if err := teeCmd.Wait(); err != nil {
		return fmt.Errorf("tee command failed: %w", err)
	}

	return nil
}

// createRepositoryEntry creates the repository sources.list.d entry
func (pm *PPAManager) createRepositoryEntry(repoEntry, sourcesFile string) error {
	logger.L().Infow("Creating repository entry", "file", sourcesFile)

	// echo {repoEntry} | sudo tee {sourcesFile}
	echoCmd := exec.Command("echo", repoEntry)
	teeCmd := exec.Command("sudo", "tee", sourcesFile)

	echoStdout, err := echoCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create echo stdout pipe: %w", err)
	}
	teeCmd.Stdin = echoStdout

	if err := teeCmd.Start(); err != nil {
		return fmt.Errorf("failed to start tee: %w", err)
	}
	if err := echoCmd.Run(); err != nil {
		return fmt.Errorf("failed to run echo: %w", err)
	}
	if err := teeCmd.Wait(); err != nil {
		return fmt.Errorf("tee command failed: %w", err)
	}

	return nil
}

// updateAptCache updates the apt package cache
func (pm *PPAManager) updateAptCache() error {
	logger.L().Infow("Updating apt cache")
	cmd := exec.Command("sudo", "apt", "update")
	output, err := cmd.CombinedOutput()
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

// dirExists checks if a directory exists
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	return err == nil && info.IsDir()
}

// ensureDir creates a directory if it doesn't exist
func ensureDir(path string) error {
	if !dirExists(path) {
		if err := os.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", path, err)
		}
	}
	return nil
}

// GetKeyringPath returns the keyring path for a PPA
func GetKeyringPath(ppaName string) string {
	return filepath.Join("/etc/apt/keyrings", fmt.Sprintf("%s-archive-keyring.gpg", ppaName))
}

// GetSourcesPath returns the sources.list.d path for a PPA
func GetSourcesPath(ppaName string) string {
	return filepath.Join("/etc/apt/sources.list.d", fmt.Sprintf("%s.list", ppaName))
}
