package alacritty

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

// repoDefaultAlacrittyPalette isolates paths.Paths.App.Configs.Themes/.Neovim
// and paths.Paths.Config.Nvim at the real repo's configs/, loads the real
// shipped "default" theme, and returns the repo root plus Alacritty's
// resolved palette. Used only by TestForceConfigureTheme_MatchesGoldenRender,
// which renders the real shipped template - not a fixture - to guard
// against silent drift in either the palette or the template (cycle doc
// Step 6 / docs/plans/cycles/2026-09-14-dg-theme.md §3's byte-identical
// bar).
func repoDefaultAlacrittyPalette(t *testing.T) (repoRoot string, p theme.Palette) {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot = filepath.Join(filepath.Dir(thisFile), "..", "..", "..")

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
	paths.Paths.Config.Nvim = filepath.Join(repoRoot, "does-not-exist-nvim")

	def, err := theme.Load(theme.DefaultThemeName)
	if err != nil {
		t.Fatalf("failed to load the real shipped default theme: %v", err)
	}
	p, err = def.PaletteFor("alacritty")
	if err != nil {
		t.Fatalf("failed to resolve alacritty's palette: %v", err)
	}
	return repoRoot, p
}

// TestForceConfigureTheme_MatchesGoldenRender renders the real shipped
// alacritty.toml.tmpl with the real "default" theme's real palette and
// compares it byte-for-byte against a committed golden. A change to either
// the template or configs/themes/default.yaml that moves the rendered
// output fails this test rather than only "looking right" by eye; update
// testdata/golden_alacritty.toml deliberately when the change is intended.
func TestForceConfigureTheme_MatchesGoldenRender(t *testing.T) {
	repoRoot, p := repoDefaultAlacrittyPalette(t)

	out := filepath.Join(t.TempDir(), "alacritty.toml")
	tmplPath := filepath.Join(repoRoot, "configs", "alacritty", "alacritty.toml.tmpl")
	if err := files.GenerateFromTemplate(tmplPath, out, map[string]any{
		"Font":       "default",
		"ConfigPath": "/home/tester/.config",
		"Colors":     newAlacrittyColors(p),
	}); err != nil {
		t.Fatalf("failed to render alacritty.toml.tmpl: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	goldenPath := filepath.Join("testdata", "golden_alacritty.toml")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("failed to read golden %s: %v", goldenPath, err)
	}
	if string(got) != string(want) {
		t.Errorf(
			"rendered alacritty.toml differs from %s\n--- got ---\n%s\n--- want ---\n%s",
			goldenPath, got, want,
		)
	}
}
