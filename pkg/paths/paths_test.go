package paths_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
)

var cmd = commands.NewBaseCommand()

func TestConfigDir(t *testing.T) {
	t.Run("no subdirs", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_CONFIG_HOME", "")
		got := paths.GetConfigDir()
		want := filepath.Join(tmpDir, ".config")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("one subdir", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_CONFIG_HOME", "")
		got := paths.GetConfigDir(constants.App.Name)
		want := filepath.Join(tmpDir, ".config", constants.App.Name)
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("multiple subdirs", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_CONFIG_HOME", "")
		got := paths.GetConfigDir(constants.App.Name, "nvim")
		want := filepath.Join(tmpDir, ".config", constants.App.Name, "nvim")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("XDG_CONFIG_HOME override", func(t *testing.T) {
		xdgDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", xdgDir)
		got := paths.GetConfigDir(constants.App.Name)
		want := filepath.Join(xdgDir, constants.App.Name)
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})
}

func TestDataDir(t *testing.T) {
	t.Run("default location", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_DATA_HOME", "")
		got := paths.GetDataDir(constants.App.Name)
		want := filepath.Join(tmpDir, ".local", "share", constants.App.Name)
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("no subdir", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_DATA_HOME", "")
		got := paths.GetDataDir()
		want := filepath.Join(tmpDir, ".local", "share")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("override", func(t *testing.T) {
		xdgDir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", xdgDir)
		got := paths.GetDataDir("app")
		want := filepath.Join(xdgDir, "app")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})
}

func TestCacheDir(t *testing.T) {
	t.Run("default location", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_CACHE_HOME", "")
		got := paths.GetCacheDir(constants.App.Name)
		want := filepath.Join(tmpDir, ".cache", constants.App.Name)
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("multiple subdirs", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_CACHE_HOME", "")
		got := paths.GetCacheDir(constants.App.Name, "nvim")
		want := filepath.Join(tmpDir, ".cache", constants.App.Name, "nvim")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("override", func(t *testing.T) {
		xdgDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", xdgDir)
		got := paths.GetCacheDir("logs")
		want := filepath.Join(xdgDir, "logs")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})
}

func TestAppDir(t *testing.T) {
	t.Run("returns app dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_DATA_HOME", "")
		got := paths.GetAppDir("logs")
		want := filepath.Join(tmpDir, ".local", "share", constants.App.Name, "logs")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})
}

func TestApplicationsDirs(t *testing.T) {
	t.Run("user applications dir - linux", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_DATA_HOME", "")
		got := paths.GetUserApplicationsDir(false, "myapp")
		want := filepath.Join(tmpDir, ".local", "share", "applications", "myapp")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("user applications dir - mac", func(t *testing.T) {
		got := paths.GetUserApplicationsDir(true, "MyApp.app")
		want := filepath.Join("/Applications", "MyApp.app")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("system applications dir - linux", func(t *testing.T) {
		got := paths.GetSystemApplicationsDir(false, "myapp.desktop")
		want := filepath.Join("/usr/share/applications", "myapp.desktop")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("system applications dir - mac", func(t *testing.T) {
		got := paths.GetSystemApplicationsDir(true, "MyApp.app")
		want := filepath.Join("/Applications", "MyApp.app")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})
}

func createFile(t *testing.T, path string) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	err = os.WriteFile(path, []byte("test content"), 0644)
	if err != nil {
		t.Fatalf("failed to create file %q: %v", path, err)
	}
}

func TestGetShellConfigFile(t *testing.T) {
	originalChecker := paths.FileAlreadyExist
	defer func() { paths.FileAlreadyExist = originalChecker }()

	t.Run("returns first matching config file", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))

		target := filepath.Join(tmpDir, ".bash_profile")
		createFile(t, target)

		paths.FileAlreadyExist = func(path string) bool {
			return path == target
		}

		got := paths.GetShellConfigFile()
		want := target
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("returns fish config if only it exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		fishDir := filepath.Join(tmpDir, ".config", "fish")
		if err := os.MkdirAll(fishDir, 0755); err != nil {
			t.Fatalf("failed to create fish config dir: %v", err)
		}

		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))

		target := filepath.Join(fishDir, "config.fish")
		createFile(t, target)

		paths.FileAlreadyExist = func(path string) bool {
			return path == target
		}

		got := paths.GetShellConfigFile()
		want := target
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("returns default if none exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("HOME", tmpDir)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))

		paths.FileAlreadyExist = func(path string) bool {
			return false
		}

		got := paths.GetShellConfigFile()
		want := filepath.Join(tmpDir, ".zshrc")
		if got != want {
			t.Errorf("expected default %q, got %q", want, got)
		}
	})
}

func TestFontsDirs(t *testing.T) {
	t.Run("user fonts dir - mac", func(t *testing.T) {
		tempHome := t.TempDir()
		t.Setenv("HOME", tempHome)

		got := paths.GetUserFontsDir(true, "MyFont.ttf")
		want := filepath.Join(tempHome, "Library", "Fonts", "MyFont.ttf")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("user fonts dir - linux", func(t *testing.T) {
		tempHome := t.TempDir()
		t.Setenv("HOME", tempHome)
		t.Setenv("XDG_DATA_HOME", filepath.Join(tempHome, ".local", "share"))

		got := paths.GetUserFontsDir(false, "font.ttf")
		want := filepath.Join(tempHome, ".local", "share", "fonts", "font.ttf")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("system fonts dir - mac", func(t *testing.T) {
		got := paths.GetSystemFontsDir(true, "MyFont.ttf")
		want := filepath.Join("/Library", "Fonts", "MyFont.ttf")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("system fonts dir - linux", func(t *testing.T) {
		got := paths.GetSystemFontsDir(false, "font.ttf")
		want := filepath.Join("/usr/share/fonts", "font.ttf")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("could not resolve home: %v", err)
	}

	t.Run("expands bare tilde", func(t *testing.T) {
		if got := paths.ExpandHome("~"); got != home {
			t.Errorf("expected %q, got %q", home, got)
		}
	})

	t.Run("expands tilde-slash prefix", func(t *testing.T) {
		want := filepath.Join(home, "code", "repo")
		if got := paths.ExpandHome("~/code/repo"); got != want {
			t.Errorf("expected %q, got %q", want, got)
		}
	})

	t.Run("leaves other paths untouched", func(t *testing.T) {
		for _, p := range []string{"/abs/path", "relative/path", "no~expansion/~here"} {
			if got := paths.ExpandHome(p); got != p {
				t.Errorf("expected %q unchanged, got %q", p, got)
			}
		}
	})
}
