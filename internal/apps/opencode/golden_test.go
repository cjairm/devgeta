package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

// repoDefaultOpenCodePalette isolates paths.Paths.App.Configs.Themes/.Neovim
// and paths.Paths.Config.Nvim at the real repo's configs/, loads the real
// shipped "default" theme, and returns the repo root plus OpenCode's
// resolved palette. Used only by the two
// TestForceConfigureTheme_*MatchesGoldenRender tests below, which render
// the real shipped templates - not fixtures - to guard against silent
// drift in either the palette or a template (cycle doc Step 6 /
// docs/plans/cycles/2026-09-14-dg-theme.md §3's byte-identical bar).
func repoDefaultOpenCodePalette(t *testing.T) (repoRoot string, p theme.Palette) {
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
	p, err = def.PaletteFor("opencode")
	if err != nil {
		t.Fatalf("failed to resolve opencode's palette: %v", err)
	}
	return repoRoot, p
}

// TestForceConfigureTheme_ConfigMatchesGoldenRender renders the real shipped
// opencode.json.tmpl (via the same helper permissions_test.go already uses)
// and compares it byte-for-byte against a committed golden. Update
// testdata/golden_opencode.json deliberately when the change is intended.
func TestForceConfigureTheme_ConfigMatchesGoldenRender(t *testing.T) {
	got := renderedOpenCodeConfig(t)

	want, err := os.ReadFile(filepath.Join("testdata", "golden_opencode.json"))
	if err != nil {
		t.Fatalf("failed to read golden: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf(
			"rendered opencode.json differs from golden\n--- got ---\n%s\n--- want ---\n%s",
			got, want,
		)
	}
}

// TestForceConfigureTheme_ThemeJSONMatchesGoldenRender renders the real
// shipped themes/default.json.tmpl with the real "default" theme's real
// palette and compares it byte-for-byte against a committed golden. Update
// testdata/golden_theme.json deliberately when the change is intended.
func TestForceConfigureTheme_ThemeJSONMatchesGoldenRender(t *testing.T) {
	repoRoot, p := repoDefaultOpenCodePalette(t)

	out := filepath.Join(t.TempDir(), "default.json")
	tmplPath := filepath.Join(repoRoot, "configs", "opencode", "themes", "default.json.tmpl")
	if err := files.GenerateFromTemplate(
		tmplPath, out, map[string]any{"Palette": p},
	); err != nil {
		t.Fatalf("failed to render opencode theme json: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "golden_theme.json"))
	if err != nil {
		t.Fatalf("failed to read golden: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf(
			"rendered opencode theme json differs from golden\n--- got ---\n%s\n--- want ---\n%s",
			got, want,
		)
	}
}

// TestForceConfigureTheme_ConfigWithPacksMatchesGoldenRender covers the other
// half of the template: with agent packs installed in OpenCode, their plugin
// folders and instruction files are rendered from state, so a `--force`
// re-render keeps them (ADR-0068 §6). With no packs the output is the
// existing golden, byte for byte.
func TestForceConfigureTheme_ConfigWithPacksMatchesGoldenRender(t *testing.T) {
	repoRoot, _ := repoDefaultOpenCodePalette(t)

	out := filepath.Join(t.TempDir(), "opencode.json")
	if err := files.GenerateFromTemplate(
		filepath.Join(repoRoot, "configs", "opencode", "opencode.json.tmpl"),
		out,
		configTemplateData{
			ScratchDirGlob: `"/tmp/placeholder-scratch/**"`,
			Plugins: []string{
				`"/home/tester/.local/share/devgeta/agent-packs/ponytail/.opencode/plugins"`,
				`"/home/tester/.local/share/devgeta/agent-packs/superpowers"`,
			},
			Instructions: []string{`"/home/tester/.local/share/devgeta/agent-packs/devgeta/DEVGETA.md"`},
		},
	); err != nil {
		t.Fatalf("failed to render opencode.json.tmpl: %v", err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(got) {
		t.Fatalf("the rendered config is not valid JSON:\n%s", got)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "golden_opencode_packs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("rendered output differs from golden_opencode_packs.json\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
