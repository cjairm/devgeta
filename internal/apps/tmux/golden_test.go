package tmux_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/theme"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

// repoDefaultTmuxPalette isolates paths.Paths.App.Configs.Themes/.Neovim and
// paths.Paths.Config.Nvim at the real repo's configs/, loads the real
// shipped "default" theme, and returns the repo root plus tmux's resolved
// palette. Used only by TestForceConfigureTheme_MatchesGoldenRender, which
// renders the real shipped template - not a fixture - to guard against
// silent drift in either the palette or the template (cycle doc Step 6 /
// docs/plans/cycles/2026-09-14-dg-theme.md §3's byte-identical bar).
func repoDefaultTmuxPalette(t *testing.T) (repoRoot string, p theme.Palette) {
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
	p, err = def.PaletteFor("tmux")
	if err != nil {
		t.Fatalf("failed to resolve tmux's palette: %v", err)
	}
	return repoRoot, p
}

// renderShippedTmuxConf renders the real shipped tmux.conf.tmpl with the real
// "default" theme's palette and returns the result, so tests assert against
// what `dg configure tmux` actually writes rather than a fixture.
func renderShippedTmuxConf(t *testing.T) []byte {
	t.Helper()
	repoRoot, p := repoDefaultTmuxPalette(t)

	out := filepath.Join(t.TempDir(), ".tmux.conf")
	tmplPath := filepath.Join(repoRoot, "configs", "tmux", "tmux.conf.tmpl")
	if err := files.GenerateFromTemplate(tmplPath, out, struct {
		NotifySound bool
		Palette     theme.Palette
	}{
		NotifySound: false,
		Palette:     p,
	}); err != nil {
		t.Fatalf("failed to render tmux.conf.tmpl: %v", err)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestForceConfigureTheme_MatchesGoldenRender renders the real shipped
// tmux.conf.tmpl with the real "default" theme's real palette and compares
// it byte-for-byte against a committed golden. A change to either the
// template or configs/themes/default.yaml that moves the rendered output
// fails this test rather than only "looking right" by eye; update
// testdata/golden_tmux.conf deliberately when the change is intended.
func TestForceConfigureTheme_MatchesGoldenRender(t *testing.T) {
	got := renderShippedTmuxConf(t)
	goldenPath := filepath.Join("testdata", "golden_tmux.conf")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("failed to read golden %s: %v", goldenPath, err)
	}
	if string(got) != string(want) {
		t.Errorf(
			"rendered tmux config differs from %s\n--- got ---\n%s\n--- want ---\n%s",
			goldenPath, got, want,
		)
	}
}

// TestForceConfigureTheme_PaneMoveKeysPassThroughForDgWs is a structural
// check (not just the byte-for-byte golden above) that each of the four
// C-h/j/k/l bindings passes the key through when the pane runs `devgeta ws`,
// per ADR-0057 (Step 9 of docs/plans/cycles/2026-09-28-ws-agents-section.md):
// a substring check alone would pass even if is_dgws's condition were wrong
// or a binding's nested if-shell were malformed, so this asserts the exact
// per-key relationship between the binding and the shared is_dgws check.
func TestForceConfigureTheme_PaneMoveKeysPassThroughForDgWs(t *testing.T) {
	got := string(renderShippedTmuxConf(t))

	if !strings.Contains(got, `is_dgws="ps -o state= -o args= -t '#{pane_tty}'`) {
		t.Fatalf("expected an is_dgws check reading pane_tty's args, got:\n%s", got)
	}
	if !strings.Contains(got, `devgeta ws`) {
		t.Fatalf("expected is_dgws's pattern to match \"devgeta ws\", got:\n%s", got)
	}

	for _, key := range []string{"C-h", "C-j", "C-k", "C-l"} {
		wantBinding := `bind -n ` + key + ` if-shell "$is_vim" "send-keys ` + key +
			`"  "if-shell \"$is_dgws\" \"send-keys ` + key + `\"  \"select-pane -`
		if !strings.Contains(got, wantBinding) {
			t.Errorf(
				"expected %s to fall through is_vim into an is_dgws-gated pass-through, "+
					"got no match for %q in:\n%s",
				key, wantBinding, got,
			)
		}
	}
}
