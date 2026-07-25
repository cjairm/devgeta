package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/logger"
)

// shellBinaryDetector pairs a shell feature setter with the binary name to detect.
type shellBinaryDetector struct {
	binary string
	enable func(sf *ShellFeatures)
}

// binaryDetectors lists all shell features detectable via exec.LookPath.
var binaryDetectors = []shellBinaryDetector{
	{constants.Mise, func(sf *ShellFeatures) { sf.Mise = true }},
	{constants.Zoxide, func(sf *ShellFeatures) { sf.Zoxide = true }},
	{constants.LazyGit, func(sf *ShellFeatures) { sf.LazyGit = true }},
	{constants.LazyDocker, func(sf *ShellFeatures) { sf.LazyDocker = true }},
	{constants.Fzf, func(sf *ShellFeatures) { sf.Fzf = true }},
	{constants.Nvim, func(sf *ShellFeatures) { sf.Neovim = true }},
	{constants.Tmux, func(sf *ShellFeatures) { sf.Tmux = true }},
	{constants.Eza, func(sf *ShellFeatures) { sf.Eza = true }},
	{constants.Bat, func(sf *ShellFeatures) { sf.Bat = true }},
	{constants.OpenCode, func(sf *ShellFeatures) { sf.Opencode = true }},
	{constants.Claude, func(sf *ShellFeatures) { sf.Claude = true }},
}

// shellPluginDetector pairs a shell feature setter with file paths to check.
type shellPluginDetector struct {
	paths  []string
	enable func(sf *ShellFeatures)
}

// pluginDetectors returns zsh plugin detectors with platform-appropriate file paths.
func pluginDetectors() []shellPluginDetector {
	homeDir, _ := os.UserHomeDir()

	// Homebrew prefixes (Apple Silicon + Intel)
	brewPrefixes := []string{"/opt/homebrew", "/usr/local"}

	autosuggestionsPaths := []string{
		"/usr/share/zsh-autosuggestions/zsh-autosuggestions.zsh",
	}
	syntaxHighlightingPaths := []string{
		"/usr/share/zsh-syntax-highlighting/zsh-syntax-highlighting.zsh",
	}
	powerlevel10kPaths := []string{
		"/usr/share/powerlevel10k/powerlevel10k.zsh-theme",
	}

	for _, prefix := range brewPrefixes {
		autosuggestionsPaths = append(autosuggestionsPaths,
			filepath.Join(prefix, "share", constants.ZshAutosuggestions, "zsh-autosuggestions.zsh"))
		syntaxHighlightingPaths = append(
			syntaxHighlightingPaths,
			filepath.Join(
				prefix,
				"share",
				constants.Syntaxhighlighting,
				"zsh-syntax-highlighting.zsh",
			),
		)
		powerlevel10kPaths = append(powerlevel10kPaths,
			filepath.Join(prefix, "share", constants.Powerlevel10k, "powerlevel10k.zsh-theme"))
	}

	if homeDir != "" {
		powerlevel10kPaths = append(powerlevel10kPaths,
			filepath.Join(homeDir, constants.Powerlevel10k, "powerlevel10k.zsh-theme"))
	}

	return []shellPluginDetector{
		{autosuggestionsPaths, func(sf *ShellFeatures) { sf.ZshAutosuggestions = true }},
		{syntaxHighlightingPaths, func(sf *ShellFeatures) { sf.ZshSyntaxHighlighting = true }},
		{powerlevel10kPaths, func(sf *ShellFeatures) { sf.Powerlevel10k = true }},
	}
}

// lookPath is the function used to detect binaries. Replaceable for testing.
var lookPath = exec.LookPath

// fileExists is the function used to check file existence. Replaceable for testing.
var fileExists = func(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ReconcileShellFeatures detects which tools are installed on the system
// and enables their corresponding shell features. This makes the config
// self-healing: even if shell feature booleans were lost, they are restored
// based on what's actually installed.
func (gc *GlobalConfig) ReconcileShellFeatures() {
	logger.L().Debug("Reconciling shell features with installed tools")

	// Platform detection
	gc.Shell.IsMac = runtime.GOOS == "darwin"

	// Extended capabilities is always on (devgeta meta-feature)
	gc.Shell.ExtendedCapabilities = true

	// Detect binary tools
	for _, d := range binaryDetectors {
		if _, err := lookPath(d.binary); err == nil {
			d.enable(&gc.Shell)
		}
	}

	// Detect zsh plugins via file paths
	for _, d := range pluginDetectors() {
		for _, p := range d.paths {
			if fileExists(p) {
				d.enable(&gc.Shell)
				break
			}
		}
	}
}
