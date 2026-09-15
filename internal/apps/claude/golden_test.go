package claude

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

// repoDefaultClaudePalette isolates paths.Paths.App.Configs.Themes/.Neovim
// and paths.Paths.Config.Nvim at the real repo's configs/, loads the real
// shipped "default" theme, and returns the repo root plus Claude's resolved
// palette. Used only by the two TestForceConfigureTheme_*MatchesGoldenRender
// tests below, which render the real shipped templates - not fixtures - to
// guard against silent drift in either the palette or a template (cycle doc
// Step 6 / docs/plans/cycles/2026-09-14-dg-theme.md §3's byte-identical
// bar).
func repoDefaultClaudePalette(t *testing.T) (repoRoot string, p theme.Palette) {
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
	p, err = def.PaletteFor("claude")
	if err != nil {
		t.Fatalf("failed to resolve claude's palette: %v", err)
	}
	return repoRoot, p
}

func compareToGolden(t *testing.T, gotPath, goldenPath string) {
	t.Helper()
	got, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("failed to read golden %s: %v", goldenPath, err)
	}
	if string(got) != string(want) {
		t.Errorf(
			"rendered output differs from %s\n--- got ---\n%s\n--- want ---\n%s",
			goldenPath, got, want,
		)
	}
}

// TestForceConfigureTheme_SettingsMatchesGoldenRender renders the real
// shipped settings.json.tmpl with a deterministic, zero-value
// IntegrationsConfig (no rtk/output-budget opt-ins) and compares it
// byte-for-byte against a committed golden. Update
// testdata/golden_settings.json deliberately when the change is intended.
func TestForceConfigureTheme_SettingsMatchesGoldenRender(t *testing.T) {
	repoRoot, _ := repoDefaultClaudePalette(t)

	out := filepath.Join(t.TempDir(), "settings.json")
	tmplPath := filepath.Join(repoRoot, "configs", "claude", "settings.json.tmpl")
	if err := files.GenerateFromTemplate(tmplPath, out, settingsTemplateData{
		ScratchDir:          `"/home/tester/.cache/devgeta/scratch"`,
		OutputBudgetEnabled: false,
		Theme:               theme.DefaultThemeName,
	}); err != nil {
		t.Fatalf("failed to render settings.json.tmpl: %v", err)
	}

	compareToGolden(t, out, filepath.Join("testdata", "golden_settings.json"))
}

// TestForceConfigureTheme_ThemeJSONMatchesGoldenRender renders the real
// shipped themes/default.json.tmpl with the real "default" theme's real
// palette and compares it byte-for-byte against a committed golden. Update
// testdata/golden_theme.json deliberately when the change is intended.
func TestForceConfigureTheme_ThemeJSONMatchesGoldenRender(t *testing.T) {
	repoRoot, p := repoDefaultClaudePalette(t)

	out := filepath.Join(t.TempDir(), "default.json")
	tmplPath := filepath.Join(repoRoot, "configs", "claude", "themes", "default.json.tmpl")
	if err := files.GenerateFromTemplate(
		tmplPath, out, map[string]any{"Palette": p},
	); err != nil {
		t.Fatalf("failed to render claude theme json: %v", err)
	}

	compareToGolden(t, out, filepath.Join("testdata", "golden_theme.json"))
}
