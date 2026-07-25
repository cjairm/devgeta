package config

import (
	"testing"

	"github.com/cjairm/devgeta/pkg/logger"
)

func init() { logger.Init(false) }

// testLookPathError satisfies the error interface for exec.LookPath mock.
type testLookPathError struct {
	name string
}

func (e *testLookPathError) Error() string {
	return e.name + ": executable file not found in $PATH"
}

func TestReconcileShellFeatures_DetectsBinaries(t *testing.T) {
	originalLookPath := lookPath
	defer func() { lookPath = originalLookPath }()

	installed := map[string]bool{
		"mise":       true,
		"nvim":       true,
		"tmux":       true,
		"fzf":        true,
		"lazygit":    true,
		"lazydocker": false,
		"eza":        true,
		"bat":        true,
		"zoxide":     true,
		"opencode":   true,
		"claude":     false,
	}
	lookPath = func(file string) (string, error) {
		if installed[file] {
			return "/usr/local/bin/" + file, nil
		}
		return "", &testLookPathError{file}
	}

	originalFileExists := fileExists
	defer func() { fileExists = originalFileExists }()
	fileExists = func(_ string) bool { return false }

	gc := &GlobalConfig{}
	gc.ReconcileShellFeatures()

	if !gc.Shell.Mise {
		t.Error("expected Mise to be true")
	}
	if !gc.Shell.Neovim {
		t.Error("expected Neovim to be true")
	}
	if !gc.Shell.Tmux {
		t.Error("expected Tmux to be true")
	}
	if !gc.Shell.Fzf {
		t.Error("expected Fzf to be true")
	}
	if !gc.Shell.LazyGit {
		t.Error("expected LazyGit to be true")
	}
	if gc.Shell.LazyDocker {
		t.Error("expected LazyDocker to be false")
	}
	if !gc.Shell.Eza {
		t.Error("expected Eza to be true")
	}
	if !gc.Shell.Bat {
		t.Error("expected Bat to be true")
	}
	if !gc.Shell.Zoxide {
		t.Error("expected Zoxide to be true")
	}
	if !gc.Shell.Opencode {
		t.Error("expected Opencode to be true")
	}
	if gc.Shell.Claude {
		t.Error("expected Claude to be false")
	}
	if !gc.Shell.ExtendedCapabilities {
		t.Error("expected ExtendedCapabilities to always be true")
	}
}

func TestReconcileShellFeatures_DetectsZshPlugins(t *testing.T) {
	originalLookPath := lookPath
	defer func() { lookPath = originalLookPath }()
	lookPath = func(file string) (string, error) {
		return "", &testLookPathError{file}
	}

	originalFileExists := fileExists
	defer func() { fileExists = originalFileExists }()
	fileExists = func(path string) bool {
		return path == "/opt/homebrew/share/zsh-autosuggestions/zsh-autosuggestions.zsh" ||
			path == "/usr/share/powerlevel10k/powerlevel10k.zsh-theme"
	}

	gc := &GlobalConfig{}
	gc.ReconcileShellFeatures()

	if !gc.Shell.ZshAutosuggestions {
		t.Error("expected ZshAutosuggestions to be true")
	}
	if gc.Shell.ZshSyntaxHighlighting {
		t.Error("expected ZshSyntaxHighlighting to be false")
	}
	if !gc.Shell.Powerlevel10k {
		t.Error("expected Powerlevel10k to be true")
	}
}

func TestReconcileShellFeatures_SetsIsMacAndExtended(t *testing.T) {
	originalLookPath := lookPath
	defer func() { lookPath = originalLookPath }()
	lookPath = func(file string) (string, error) {
		return "", &testLookPathError{file}
	}
	originalFileExists := fileExists
	defer func() { fileExists = originalFileExists }()
	fileExists = func(_ string) bool { return false }

	gc := &GlobalConfig{}
	gc.ReconcileShellFeatures()

	// IsMac depends on runtime.GOOS — just verify ExtendedCapabilities is always set
	if !gc.Shell.ExtendedCapabilities {
		t.Error("expected ExtendedCapabilities to always be true")
	}
}
