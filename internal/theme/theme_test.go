package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
)

func init() {
	testutil.InitLogger()
}

// isolateThemePaths points every path Load/validation touches at a fresh
// temp tree and restores the originals on cleanup - the trap CLAUDE.md's
// testing-patterns guide calls out for tests that override some but not all
// of the roots a function reads.
func isolateThemePaths(t *testing.T) (themesDir, shippedNeovimDir, deployedNvimDir string) {
	t.Helper()

	root := t.TempDir()
	themesDir = filepath.Join(root, "app-configs", "themes")
	shippedNeovimDir = filepath.Join(root, "app-configs", "neovim")
	deployedNvimDir = filepath.Join(root, "config", "nvim")

	for _, dir := range []string{themesDir, shippedNeovimDir, deployedNvimDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}

	origThemes := paths.Paths.App.Configs.Themes
	origNeovim := paths.Paths.App.Configs.Neovim
	origNvim := paths.Paths.Config.Nvim
	t.Cleanup(func() {
		paths.Paths.App.Configs.Themes = origThemes
		paths.Paths.App.Configs.Neovim = origNeovim
		paths.Paths.Config.Nvim = origNvim
	})
	paths.Paths.App.Configs.Themes = themesDir
	paths.Paths.App.Configs.Neovim = shippedNeovimDir
	paths.Paths.Config.Nvim = deployedNvimDir

	return themesDir, shippedNeovimDir, deployedNvimDir
}

// writeShippedModule drops a fake Neovim theme module under the shipped
// tree's lua/devgeta/themes/<module>.lua, matching validateNeovimModule's
// expected layout.
func writeShippedModule(t *testing.T, shippedNeovimDir, module string) {
	t.Helper()
	writeModule(t, shippedNeovimDir, module)
}

func writeDeployedModule(t *testing.T, deployedNvimDir, module string) {
	t.Helper()
	writeModule(t, deployedNvimDir, module)
}

func writeModule(t *testing.T, base, module string) {
	t.Helper()
	dir := filepath.Join(base, "lua", "devgeta", "themes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create %s: %v", dir, err)
	}
	path := filepath.Join(dir, module+".lua")
	if err := os.WriteFile(path, []byte("-- fixture\n"), 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

// fullPaletteYAML renders every role in the ADR-0043 table with the given
// hex value, so a fixture theme can be built without repeating all 24 roles
// at every call site.
func fullPaletteYAML(indent, hex string) string {
	roles := []string{
		"background_hard", "background", "background_element", "background_subtle",
		"border", "foreground", "foreground_muted", "foreground_dim", "foreground_subtle",
		"red", "green", "yellow", "blue", "purple", "aqua", "orange",
		"red_dim", "green_dim", "yellow_dim", "blue_dim", "purple_dim", "aqua_dim",
		"diff_added_background", "diff_removed_background",
	}
	var b strings.Builder
	for _, r := range roles {
		b.WriteString(indent + r + `: "` + hex + `"` + "\n")
	}
	return b.String()
}

func writeThemeFile(t *testing.T, themesDir, name, content string) {
	t.Helper()
	path := filepath.Join(themesDir, name+".yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write theme file %s: %v", path, err)
	}
}

func TestLoad_SingleGroupTheme_TerminalFallsBackToColors(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "gruvbox")

	content := "neovim_module: gruvbox\ncolors:\n" + fullPaletteYAML("  ", "#111111")
	writeThemeFile(t, themesDir, "onegroup", content)

	def, err := Load("onegroup")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	p, err := def.PaletteFor(constants.Alacritty)
	if err != nil {
		t.Fatalf("PaletteFor(alacritty) error: %v", err)
	}
	if p.Foreground != "#111111" {
		t.Errorf("terminal surface should fall back to colors: got Foreground=%q", p.Foreground)
	}
}

func TestPaletteFor_TerminalSurfacesUseTerminalGroup(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "gruvbox")

	content := "neovim_module: gruvbox\n" +
		"colors:\n" + fullPaletteYAML("  ", "#111111") +
		"terminal:\n" + fullPaletteYAML("  ", "#222222")
	writeThemeFile(t, themesDir, "twogroup", content)

	def, err := Load("twogroup")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	for _, surface := range []string{constants.Alacritty, constants.Ghostty} {
		p, err := def.PaletteFor(surface)
		if err != nil {
			t.Fatalf("PaletteFor(%s) error: %v", surface, err)
		}
		if p.Foreground != "#222222" {
			t.Errorf("%s: want terminal group value #222222, got %q", surface, p.Foreground)
		}
	}
}

func TestPaletteFor_OtherSurfacesUseColorsGroup(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "gruvbox")

	content := "neovim_module: gruvbox\n" +
		"colors:\n" + fullPaletteYAML("  ", "#111111") +
		"terminal:\n" + fullPaletteYAML("  ", "#222222")
	writeThemeFile(t, themesDir, "twogroup", content)

	def, err := Load("twogroup")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	for _, surface := range []string{
		constants.Tmux, constants.Neovim, constants.OpenCode, constants.Claude, constants.I3,
	} {
		p, err := def.PaletteFor(surface)
		if err != nil {
			t.Fatalf("PaletteFor(%s) error: %v", surface, err)
		}
		if p.Foreground != "#111111" {
			t.Errorf("%s: want colors group value #111111, got %q", surface, p.Foreground)
		}
	}
}

func TestPaletteFor_UnknownSurfaceIsAnError(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "gruvbox")

	content := "neovim_module: gruvbox\ncolors:\n" + fullPaletteYAML("  ", "#111111")
	writeThemeFile(t, themesDir, "onegroup", content)

	def, err := Load("onegroup")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if _, err := def.PaletteFor("not-a-real-surface"); err == nil {
		t.Fatal("expected an error for an unmapped surface, got nil")
	}
}

func TestPaletteFor_CoversEveryThemedSurface(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "gruvbox")

	content := "neovim_module: gruvbox\ncolors:\n" + fullPaletteYAML("  ", "#111111")
	writeThemeFile(t, themesDir, "onegroup", content)

	def, err := Load("onegroup")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	for _, surface := range []string{
		constants.Alacritty, constants.Ghostty, constants.Tmux,
		constants.Neovim, constants.OpenCode, constants.Claude, constants.I3,
	} {
		if _, err := def.PaletteFor(surface); err != nil {
			t.Errorf("PaletteFor(%s): %v", surface, err)
		}
	}
}

func TestLoad_PartialTerminalGroup_IsRejected(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "gruvbox")

	content := "neovim_module: gruvbox\n" +
		"colors:\n" + fullPaletteYAML("  ", "#111111") +
		"terminal:\n  foreground: \"#222222\"\n"
	writeThemeFile(t, themesDir, "partial", content)

	_, err := Load("partial")
	if err == nil {
		t.Fatal("expected an error for a partial terminal group, got nil")
	}
	if !strings.Contains(err.Error(), "terminal") {
		t.Errorf("error should name the group (terminal): %v", err)
	}
	if !strings.Contains(err.Error(), "red") {
		t.Errorf("error should name a missing role: %v", err)
	}
}

func TestLoad_MissingColorsGroup_IsRejected(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "gruvbox")

	writeThemeFile(t, themesDir, "nocolors", "neovim_module: gruvbox\n")

	if _, err := Load("nocolors"); err == nil {
		t.Fatal("expected an error when colors: is missing, got nil")
	}
}

func TestLoad_UnparseableColor_IsRejected(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "gruvbox")

	content := "neovim_module: gruvbox\ncolors:\n" + fullPaletteYAML("  ", "#111111")
	content = strings.Replace(content, `red: "#111111"`, `red: "not-a-color"`, 1)
	writeThemeFile(t, themesDir, "badcolor", content)

	_, err := Load("badcolor")
	if err == nil {
		t.Fatal("expected an error for an unparseable color, got nil")
	}
	if !strings.Contains(err.Error(), "red") {
		t.Errorf("error should name the offending role: %v", err)
	}
}

func TestLoad_NeovimModule_ShippedTreeOnly(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "onlyshipped")

	content := "neovim_module: onlyshipped\ncolors:\n" + fullPaletteYAML("  ", "#111111")
	writeThemeFile(t, themesDir, "shippedonly", content)

	if _, err := Load("shippedonly"); err != nil {
		t.Fatalf("Load() should accept a module present only in the shipped tree: %v", err)
	}
}

func TestLoad_NeovimModule_DeployedTreeOnly(t *testing.T) {
	themesDir, _, deployedNvimDir := isolateThemePaths(t)
	writeDeployedModule(t, deployedNvimDir, "onlydeployed")

	content := "neovim_module: onlydeployed\ncolors:\n" + fullPaletteYAML("  ", "#111111")
	writeThemeFile(t, themesDir, "deployedonly", content)

	if _, err := Load("deployedonly"); err != nil {
		t.Fatalf("Load() should accept a module present only in the deployed tree: %v", err)
	}
}

func TestLoad_NeovimModule_Neither_IsRejected(t *testing.T) {
	themesDir, _, _ := isolateThemePaths(t)

	content := "neovim_module: nowhere\ncolors:\n" + fullPaletteYAML("  ", "#111111")
	writeThemeFile(t, themesDir, "orphanmodule", content)

	_, err := Load("orphanmodule")
	if err == nil {
		t.Fatal("expected an error when the neovim module exists in neither tree, got nil")
	}
	if !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("error should name the missing module: %v", err)
	}
}

// TestLoad_RelativeWallpaperResolvesAgainstTheThemesDir covers a wallpaper
// that ships alongside its theme: the YAML names it relative
// (`wallpaper: wallpapers/default.jpg`), and Load resolves it against the
// extracted configs tree, so the recorded path is correct on whatever
// machine ExtractEmbeddedConfigs just wrote it to. A relative path left
// unresolved would be interpreted against the process's working directory,
// which for `dg theme set` is wherever the user happened to be standing.
func TestLoad_RelativeWallpaperResolvesAgainstTheThemesDir(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "gruvbox")

	content := "neovim_module: gruvbox\nwallpaper: wallpapers/shipped.jpg\ncolors:\n" +
		fullPaletteYAML("  ", "#111111")
	writeThemeFile(t, themesDir, "shippedwall", content)

	def, err := Load("shippedwall")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := filepath.Join(themesDir, "wallpapers", "shipped.jpg")
	if def.Wallpaper != want {
		t.Errorf("Wallpaper = %q, want %q", def.Wallpaper, want)
	}
}

// TestLoad_AbsoluteWallpaperIsLeftAlone covers the other half: a user
// pointing a theme at an image somewhere else on their own machine keeps
// the exact path they wrote.
func TestLoad_AbsoluteWallpaperIsLeftAlone(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "gruvbox")

	content := "neovim_module: gruvbox\nwallpaper: /home/tester/mine.jpg\ncolors:\n" +
		fullPaletteYAML("  ", "#111111")
	writeThemeFile(t, themesDir, "abswall", content)

	def, err := Load("abswall")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if def.Wallpaper != "/home/tester/mine.jpg" {
		t.Errorf("Wallpaper = %q, want the absolute path unchanged", def.Wallpaper)
	}
}

// TestLoad_NoWallpaperStaysEmpty proves the resolution never invents a path
// for a theme that declares no wallpaper - an empty string must not become
// the themes directory itself, which would then be handed to a
// WallpaperSetter as if it were an image.
func TestLoad_NoWallpaperStaysEmpty(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "gruvbox")

	content := "neovim_module: gruvbox\ncolors:\n" + fullPaletteYAML("  ", "#111111")
	writeThemeFile(t, themesDir, "nowall", content)

	def, err := Load("nowall")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if def.Wallpaper != "" {
		t.Errorf("Wallpaper = %q, want empty", def.Wallpaper)
	}
}

// TestList_MissingThemesDir_ExplainsHowToPublishConfigs covers the failure a
// binary upgrade actually produces. paths.Paths.App.Configs is a
// build-stamped pointer refreshed only by devgeta.InstallIfStale (which
// `dg install` and `dg configure` call), so a newly installed binary that
// introduced themes at all reads the PREVIOUS build's tree - which has no
// themes/ directory. The bare os.ReadDir error for that is
// "no such file or directory" naming a path the user never chose, with no
// hint that the fix is to publish the configs.
func TestList_MissingThemesDir_ExplainsHowToPublishConfigs(t *testing.T) {
	isolateThemePaths(t)
	paths.Paths.App.Configs.Themes = filepath.Join(t.TempDir(), "never-extracted")

	_, err := List()
	if err == nil {
		t.Fatal("expected an error when the themes directory does not exist")
	}
	for _, want := range []string{"dg install", "dg configure"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should tell the user to run %q, got: %v", want, err)
		}
	}
}

// TestLoad_MissingThemesDir_ExplainsHowToPublishConfigs is the same failure
// reached through `dg theme set <name>` rather than `dg theme list`.
func TestLoad_MissingThemesDir_ExplainsHowToPublishConfigs(t *testing.T) {
	isolateThemePaths(t)
	paths.Paths.App.Configs.Themes = filepath.Join(t.TempDir(), "never-extracted")

	_, err := Load("default")
	if err == nil {
		t.Fatal("expected an error when the themes directory does not exist")
	}
	for _, want := range []string{"dg install", "dg configure"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should tell the user to run %q, got: %v", want, err)
		}
	}
}

// TestLoad_UnknownThemeNamesTheAvailableOnes keeps the missing-directory
// message from swallowing the ordinary typo case: when the tree IS
// published and the user just named a theme that isn't there, the error
// should say so and list what is (cycle doc §6's manual check 6).
func TestLoad_UnknownThemeNamesTheAvailableOnes(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "gruvbox")
	writeThemeFile(
		t, themesDir, "default",
		"neovim_module: gruvbox\ncolors:\n"+fullPaletteYAML("  ", "#111111"),
	)

	_, err := Load("nonexistent")
	if err == nil {
		t.Fatal("expected an error for an unknown theme")
	}
	if strings.Contains(err.Error(), "dg install") {
		t.Errorf("a published tree must not produce the publish-your-configs message: %v", err)
	}
	if !strings.Contains(err.Error(), "default") {
		t.Errorf("error should list the available themes, got: %v", err)
	}
}

func TestList_EnumeratesThemeFiles(t *testing.T) {
	themesDir, shippedNeovimDir, _ := isolateThemePaths(t)
	writeShippedModule(t, shippedNeovimDir, "gruvbox")

	writeThemeFile(
		t,
		themesDir,
		"default",
		"neovim_module: gruvbox\ncolors:\n"+fullPaletteYAML("  ", "#111111"),
	)
	writeThemeFile(
		t,
		themesDir,
		"tokyonight",
		"neovim_module: gruvbox\ncolors:\n"+fullPaletteYAML("  ", "#222222"),
	)

	names, err := List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(names) != 2 || names[0] != "default" || names[1] != "tokyonight" {
		t.Errorf("List() = %v, want [default tokyonight]", names)
	}
}
