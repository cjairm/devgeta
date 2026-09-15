package theme

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cjairm/devgeta/pkg/paths"
)

// pointAtRepoConfigs isolates paths.Paths.App.Configs.Themes/.Neovim and
// paths.Paths.Config.Nvim at the real repo's configs/, so Load exercises the
// actual shipped theme files and Neovim modules rather than fixtures. Used
// by the shipped-theme tests below - not by anything under normal test
// isolation, since it deliberately reads real repo content.
func pointAtRepoConfigs(t *testing.T) {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	origThemes := paths.Paths.App.Configs.Themes
	origNeovim := paths.Paths.App.Configs.Neovim
	origNvim := paths.Paths.Config.Nvim
	t.Cleanup(func() {
		paths.Paths.App.Configs.Themes = origThemes
		paths.Paths.App.Configs.Neovim = origNeovim
		paths.Paths.Config.Nvim = origNvim
	})
	paths.Paths.App.Configs.Themes = filepath.Join(repoRoot, "configs", "themes")
	paths.Paths.App.Configs.Neovim = filepath.Join(repoRoot, "configs", "neovim")
	// No deployed tree - the shipped tree alone must resolve every module
	// configs/themes/*.yaml names, on a machine that has never run
	// `dg configure neovim`.
	paths.Paths.Config.Nvim = filepath.Join(repoRoot, "does-not-exist-nvim")
}

// TestShippedThemes_AllLoadAndValidate proves every theme file actually
// shipped under configs/themes/ - not a fixture - loads and validates
// cleanly, including its neovim_module resolving in the shipped tree.
func TestShippedThemes_AllLoadAndValidate(t *testing.T) {
	pointAtRepoConfigs(t)

	names, err := List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(names) < 2 {
		t.Fatalf("expected at least default and tokyonight, got %v", names)
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			def, err := Load(name)
			if err != nil {
				t.Fatalf("Load(%s) error: %v", name, err)
			}
			if def.NeovimModule == "" {
				t.Errorf("Load(%s): empty NeovimModule", name)
			}
		})
	}
}

// TestTokyonight_ResolvesDistinctPaletteFromDefault proves tokyonight is a
// real, independent theme - not a copy of default's colors under a new
// name - across both surface groups PaletteFor can return.
func TestTokyonight_ResolvesDistinctPaletteFromDefault(t *testing.T) {
	pointAtRepoConfigs(t)

	def, err := Load(DefaultThemeName)
	if err != nil {
		t.Fatalf("Load(default) error: %v", err)
	}
	tokyo, err := Load("tokyonight")
	if err != nil {
		t.Fatalf("Load(tokyonight) error: %v", err)
	}

	for _, surface := range []string{
		"alacritty", "ghostty", "tmux", "neovim", "opencode", "claude", "i3",
	} {
		defPalette, err := def.PaletteFor(surface)
		if err != nil {
			t.Fatalf("default.PaletteFor(%s): %v", surface, err)
		}
		tokyoPalette, err := tokyo.PaletteFor(surface)
		if err != nil {
			t.Fatalf("tokyonight.PaletteFor(%s): %v", surface, err)
		}
		if defPalette.Foreground == tokyoPalette.Foreground {
			t.Errorf(
				"%s: default and tokyonight resolve the same foreground (%s) - tokyonight is not a real second theme",
				surface,
				defPalette.Foreground,
			)
		}
	}

	if tokyo.NeovimModule != "tokyonight" {
		t.Errorf("tokyonight.NeovimModule = %q, want tokyonight", tokyo.NeovimModule)
	}
}
