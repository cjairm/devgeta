package alacritty

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/testutil"
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
		"IsMac":      true,
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

// The Option+Enter binding exists so Pi's follow-up key reaches it as CSI-u
// (ADR-0061 §6). It changes what Option+Enter sends in every program, so it is
// macOS-only: it must be in the rendered config when the platform check says
// macOS and absent when it says Linux. The check goes through Base.IsMac, so
// this runs both platforms without depending on runtime.GOOS.
func TestForceConfigureTheme_OptionEnterBindingIsMacOnly(t *testing.T) {
	for _, tt := range []struct {
		name string
		mac  bool
	}{
		{"macOS", true},
		{"Linux", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tc := testutil.SetupCompleteTest(t)
			defer tc.Cleanup()
			testutil.IsolateXDGDirs(t)
			repoRoot, _ := repoDefaultAlacrittyPalette(t)
			def, err := theme.Load(theme.DefaultThemeName)
			if err != nil {
				t.Fatal(err)
			}

			destDir := filepath.Join(tc.ConfigDir, "alacritty")
			oldAlacritty := paths.Paths.App.Configs.Alacritty
			oldTerminal := paths.Paths.App.Configs.Terminal
			oldConfig := paths.Paths.Config.Alacritty
			oldRoot := paths.Paths.Config.Root
			t.Cleanup(func() {
				paths.Paths.App.Configs.Alacritty = oldAlacritty
				paths.Paths.App.Configs.Terminal = oldTerminal
				paths.Paths.Config.Alacritty = oldConfig
				paths.Paths.Config.Root = oldRoot
			})
			paths.Paths.App.Configs.Alacritty = filepath.Join(repoRoot, "configs", "alacritty")
			paths.Paths.App.Configs.Terminal = filepath.Join(repoRoot, "configs", "terminal")
			paths.Paths.Config.Alacritty = destDir
			paths.Paths.Config.Root = tc.ConfigDir

			tc.MockApp.Base.IsMacResult = tt.mac
			app := &Alacritty{Cmd: tc.MockApp.Cmd, Base: tc.MockApp.Base}
			if err := app.ForceConfigureTheme(def); err != nil {
				t.Fatalf("ForceConfigureTheme error: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(destDir, "alacritty.toml"))
			if err != nil {
				t.Fatal(err)
			}
			got := string(data)

			const binding = "[[keyboard.bindings]]\nkey = \"Enter\"\nmods = \"Alt\"\nchars = \"\\u001b[13;3u\"\n"
			if tt.mac && !strings.Contains(got, binding) {
				t.Errorf("macOS config is missing the Option+Enter binding:\n%s", got)
			}
			if !tt.mac && strings.Contains(got, "keyboard") {
				t.Errorf("Linux config must not carry keyboard bindings:\n%s", got)
			}
			testutil.VerifyNoRealCommands(t, tc.MockApp.Base)
		})
	}
}
