package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
)

// This allows swapping it during tests
var FileAlreadyExist = files.FileAlreadyExist

// testSandbox is the throwaway root directory every derived path resolves
// under while running inside `go test`. It exists so a test that forgets (or
// incompletely applies) path isolation can never read or delete real user
// data. Outside `go test` it is empty and has no effect.
//
// HOME and the XDG base variables are redirected into the sandbox too, so
// env-reading fallbacks resolve inside it as well; tests that set their own
// XDG values (e.g. via t.Setenv) still take precedence because those reads
// stay dynamic. Every path helper resolves the home directory through
// userHome, which references this variable — that reference guarantees Go
// initializes the sandbox before any package-level path (e.g. Paths) is
// derived.
var testSandbox = func() string {
	if !testing.Testing() {
		return ""
	}
	dir, err := os.MkdirTemp("", "devgeta-test-sandbox-")
	if err != nil {
		panic("could not create test sandbox directory: " + err.Error())
	}
	for key, value := range map[string]string{
		"HOME":            dir,
		"XDG_CONFIG_HOME": filepath.Join(dir, ".config"),
		"XDG_DATA_HOME":   filepath.Join(dir, ".local", "share"),
		"XDG_STATE_HOME":  filepath.Join(dir, ".local", "state"),
		"XDG_CACHE_HOME":  filepath.Join(dir, ".cache"),
	} {
		if err := os.Setenv(key, value); err != nil {
			panic("could not redirect " + key + " to the test sandbox: " + err.Error())
		}
	}
	return dir
}()

// userHome resolves the user's home directory, panicking when it cannot be
// determined. Under `go test` the read stays dynamic (tests may point HOME at
// their own temp dir via t.Setenv), but it can never resolve to the real home
// directory: the sandbox init already redirected HOME, and an empty HOME
// falls back to the sandbox root.
func userHome() string {
	if testSandbox != "" {
		if home := os.Getenv("HOME"); home != "" {
			return home
		}
		return testSandbox
	}
	home, err := os.UserHomeDir()
	if err != nil {
		panic("could not determine home directory")
	}
	return home
}

// Paths contains all directory path structures
var Paths = struct {
	App struct {
		Root    string
		Configs struct {
			Aerospace string
			Alacritty string
			Claude    string
			Fastfetch string
			Fonts     string
			Git       string
			I3        string
			Neovim    string
			OpenCode  string
			Shared    string
			Templates string
			Themes    string
			Tmux      string
		}
		Fonts struct {
			Alacritty string
		}
		Themes struct {
			Alacritty string
		}
	}
	Cache struct {
		Root string
	}
	Config struct {
		Root      string
		Aerospace string
		Alacritty string
		Claude    string
		Devgeta   string
		Fastfetch string
		Fonts     string
		Git       string
		I3        string
		Nvim      string
		OpenCode  string
		Themes    string
		Tmux      string
	}
	Data struct {
		Root string
	}
	Home struct {
		Root string
	}
	System struct {
		Applications string
		Fonts        string
	}
	User struct {
		Applications string
		Fonts        string
	}
}{
	App: struct {
		Root    string
		Configs struct {
			Aerospace string
			Alacritty string
			Claude    string
			Fastfetch string
			Fonts     string
			Git       string
			I3        string
			Neovim    string
			OpenCode  string
			Shared    string
			Templates string
			Themes    string
			Tmux      string
		}
		Fonts struct {
			Alacritty string
		}
		Themes struct {
			Alacritty string
		}
	}{
		Root: GetDataDir(constants.App.Name),
		Configs: struct {
			Aerospace string
			Alacritty string
			Claude    string
			Fastfetch string
			Fonts     string
			Git       string
			I3        string
			Neovim    string
			OpenCode  string
			Shared    string
			Templates string
			Themes    string
			Tmux      string
		}{
			Aerospace: GetAppDir(constants.App.Dir.Configs, constants.Aerospace),
			Alacritty: GetAppDir(constants.App.Dir.Configs, constants.Alacritty),
			Claude:    GetAppDir(constants.App.Dir.Configs, constants.Claude),
			Fastfetch: GetAppDir(constants.App.Dir.Configs, constants.Fastfetch),
			Fonts:     GetAppDir(constants.App.Dir.Configs, constants.Fonts),
			Git:       GetAppDir(constants.App.Dir.Configs, constants.Git),
			I3:        GetAppDir(constants.App.Dir.Configs, constants.I3),
			Neovim:    GetAppDir(constants.App.Dir.Configs, constants.Neovim),
			OpenCode:  GetAppDir(constants.App.Dir.Configs, constants.OpenCode),
			Shared:    GetAppDir(constants.App.Dir.Configs, constants.Shared),
			Templates: GetAppDir(constants.App.Dir.Configs, constants.Templates),
			Themes:    GetAppDir(constants.App.Dir.Configs, constants.Themes),
			Tmux:      GetAppDir(constants.App.Dir.Configs, constants.Tmux),
		},
		Fonts: struct {
			Alacritty string
		}{
			Alacritty: GetAppDir(constants.App.Dir.Configs, constants.Fonts, constants.Alacritty),
		},
		Themes: struct {
			Alacritty string
		}{
			Alacritty: GetAppDir(constants.App.Dir.Configs, constants.Themes, constants.Alacritty),
		},
	},
	Cache: struct {
		Root string
	}{
		Root: GetCacheDir(),
	},
	Config: struct {
		Root      string
		Aerospace string
		Alacritty string
		Claude    string
		Devgeta   string
		Fastfetch string
		Fonts     string
		Git       string
		I3        string
		Nvim      string
		OpenCode  string
		Themes    string
		Tmux      string
	}{
		Root:      GetConfigDir(),
		Aerospace: GetConfigDir(constants.Aerospace),
		Alacritty: GetConfigDir(constants.Alacritty),
		Claude:    GetHomeDir(".claude"),
		Devgeta:   GetConfigDir(constants.DevgetaApp),
		Fastfetch: GetConfigDir(constants.Fastfetch),
		Fonts:     GetConfigDir(constants.Fonts),
		Git:       GetConfigDir(constants.Git),
		I3:        GetConfigDir(constants.I3),
		Nvim:      GetConfigDir(constants.Nvim),
		OpenCode:  GetConfigDir(constants.OpenCode),
		Themes:    GetConfigDir(constants.Themes),
		Tmux:      GetConfigDir(constants.Tmux),
	},
	Data: struct {
		Root string
	}{
		Root: GetDataDir(),
	},
	Home: struct {
		Root string
	}{
		Root: GetHomeDir(),
	},
	System: struct {
		Applications string
		Fonts        string
	}{
		Applications: GetSystemApplicationsDir(runtime.GOOS == "darwin"),
		Fonts:        GetSystemFontsDir(runtime.GOOS == "darwin"),
	},
	User: struct {
		Applications string
		Fonts        string
	}{
		Applications: GetUserApplicationsDir(runtime.GOOS == "darwin"),
		Fonts:        GetUserFontsDir(runtime.GOOS == "darwin"),
	},
}

// Files contains all file path structures
var Files = struct {
	ShellConfig string
	ZshEnv      string
}{
	ShellConfig: GetShellConfigFile(),
	ZshEnv:      GetZshEnvFile(),
}

// Public API functions

// ExpandHome replaces a leading "~" with the user's home directory, so CLI
// flags can accept paths like ~/code/repo. Other paths pass through unchanged.
func ExpandHome(path string) string {
	if path == "~" {
		return userHome()
	}
	if after, ok := strings.CutPrefix(path, "~/"); ok {
		return filepath.Join(userHome(), after)
	}
	return path
}

// Returns XDG_CONFIG_HOME or fallback to ~/.config
// Reads environment variables dynamically to support testing
func GetConfigDir(subPath ...string) string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(userHome(), ".config")
	}
	return filepath.Join(append([]string{base}, subPath...)...)
}

// Returns XDG_DATA_HOME or fallback to ~/.local/share
// Reads environment variables dynamically to support testing
func GetDataDir(subPath ...string) string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		base = filepath.Join(userHome(), ".local", "share")
	}
	return filepath.Join(append([]string{base}, subPath...)...)
}

func GetAppDir(subPath ...string) string {
	appRoot := GetDataDir(constants.App.Name)
	return filepath.Join(append([]string{appRoot}, subPath...)...)
}

func GetHomeDir(subPath ...string) string {
	return filepath.Join(append([]string{userHome()}, subPath...)...)
}

// Returns XDG_STATE_HOME or fallback to ~/.local/state
// Reads environment variables dynamically to support testing
func GetStateDir(subPath ...string) string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(userHome(), ".local", "state")
	}
	return filepath.Join(append([]string{base}, subPath...)...)
}

// Returns XDG_CACHE_HOME or fallback to ~/.cache
// Reads environment variables dynamically to support testing
func GetCacheDir(subPath ...string) string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		base = filepath.Join(userHome(), ".cache")
	}
	return filepath.Join(append([]string{base}, subPath...)...)
}

// Returns user-level applications dir
func GetUserApplicationsDir(isMac bool, subPath ...string) string {
	if isMac {
		base := "/Applications"
		return filepath.Join(append([]string{base}, subPath...)...)
	}
	// Linux (XDG-compliant user apps)
	return GetDataDir(append([]string{"applications"}, subPath...)...)
}

// Returns system-level applications dir
func GetSystemApplicationsDir(isMac bool, subPath ...string) string {
	if isMac {
		base := "/Applications"
		return filepath.Join(append([]string{base}, subPath...)...)
	}
	// Linux system-wide applications dirs
	// NOTE: /usr/share/applications is more common, but /usr/local/share/applications is also valid
	// You could return both or let the caller choose
	base := "/usr/share/applications"
	return filepath.Join(append([]string{base}, subPath...)...)
}

func GetShellConfigFile() string {
	home := userHome()
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		configDir = filepath.Join(home, ".config")
	}
	shellConfigFiles := []string{
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".profile"),
		filepath.Join(configDir, "fish", "config.fish"),
	}
	for _, filepath := range shellConfigFiles {
		if FileAlreadyExist(filepath) {
			return filepath
		}
	}
	// If none exist, default to .zshrc
	return filepath.Join(home, ".zshrc")
}

// GetZshEnvFile returns the user's ~/.zshenv path. Unlike GetShellConfigFile,
// this never falls back to searching alternate candidates: ~/.zshenv is the
// one place zsh sources before anything else (login or not), whether or not
// the file exists yet.
func GetZshEnvFile() string {
	return filepath.Join(userHome(), ".zshenv")
}

// Returns user-level fonts dir
func GetUserFontsDir(isMac bool, subPath ...string) string {
	if isMac {
		home := userHome()
		base := filepath.Join(home, "Library", "Fonts")
		return filepath.Join(append([]string{base}, subPath...)...)
	}
	// Linux user fonts (XDG-compliant)
	return GetDataDir(append([]string{"fonts"}, subPath...)...)
}

// Returns system-level fonts dir
func GetSystemFontsDir(isMac bool, subPath ...string) string {
	if isMac {
		base := filepath.Join("/Library", "Fonts")
		return filepath.Join(append([]string{base}, subPath...)...)
	}
	// Linux system fonts (common default)
	base := "/usr/share/fonts"
	return filepath.Join(append([]string{base}, subPath...)...)
}
