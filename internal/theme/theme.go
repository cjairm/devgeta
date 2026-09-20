// Package theme loads and validates devgeta's theme files
// (configs/themes/<name>.yaml) and resolves them into the flat Palette each
// themed surface's template consumes. See
// docs/decisions/ADR-0043-a-theme-is-a-palette-file-not-a-branch-in-every-template.md
// for why a theme is one file with two color groups and one surface->group
// table, and docs/plans/cycles/2026-09-14-dg-theme.md's Step 2 for how this
// package is meant to be used.
package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/paths"
	"gopkg.in/yaml.v3"
)

// DefaultThemeName is what current_theme resolves to when it is empty -
// the fallback every themed app's ForceConfigure has always rendered.
const DefaultThemeName = "default"

// Palette is the one flat set of role->hex values a template consumes.
// Every field here is one entry in ADR-0043 decision point 4's role table;
// the yaml tag is that table's snake_case key, the field name its PascalCase
// template name. This is the only shape that crosses into a template's data
// or into apps.ThemedConfigurer - nothing downstream of Definition.PaletteFor
// ever sees that a theme file can hold more than one group.
type Palette struct {
	BackgroundHard        string `yaml:"background_hard"`
	Background            string `yaml:"background"`
	BackgroundElement     string `yaml:"background_element"`
	BackgroundSubtle      string `yaml:"background_subtle"`
	Border                string `yaml:"border"`
	Foreground            string `yaml:"foreground"`
	ForegroundMuted       string `yaml:"foreground_muted"`
	ForegroundDim         string `yaml:"foreground_dim"`
	ForegroundSubtle      string `yaml:"foreground_subtle"`
	Red                   string `yaml:"red"`
	Green                 string `yaml:"green"`
	Yellow                string `yaml:"yellow"`
	Blue                  string `yaml:"blue"`
	Purple                string `yaml:"purple"`
	Aqua                  string `yaml:"aqua"`
	Orange                string `yaml:"orange"`
	RedDim                string `yaml:"red_dim"`
	GreenDim              string `yaml:"green_dim"`
	YellowDim             string `yaml:"yellow_dim"`
	BlueDim               string `yaml:"blue_dim"`
	PurpleDim             string `yaml:"purple_dim"`
	AquaDim               string `yaml:"aqua_dim"`
	DiffAddedBackground   string `yaml:"diff_added_background"`
	DiffRemovedBackground string `yaml:"diff_removed_background"`
}

// rawDefinition is configs/themes/<name>.yaml's on-disk shape: two optional
// color groups (colors: is required in practice, checked after unmarshal so
// the error names the theme) plus the required neovim_module. Pointers, not
// plain Palette values, so a missing group in the file is distinguishable
// from one whose keys all failed to parse.
type rawDefinition struct {
	NeovimModule string   `yaml:"neovim_module"`
	Colors       *Palette `yaml:"colors"`
	Terminal     *Palette `yaml:"terminal"`
	// Wallpaper optionally names a wallpaper image path on the machine
	// loading this file - never a path devgeta ships, since configs/ is
	// embedded into the binary (ADR-0044). A theme declares a wallpaper; it
	// never ships one. It is a declared default: `dg theme set` consults it
	// only when GlobalConfig.Wallpapers has no entry for this theme
	// (cycle doc Step 9) - `dg theme set-wallpaper` is what actually makes a
	// wallpaper durable, by copying it into ~/.config/devgeta/wallpapers/.
	Wallpaper string `yaml:"wallpaper,omitempty"`
}

// Definition is one loaded, validated theme file: its name, the Neovim
// module it names, and its two resolved color groups. It is deliberately not
// a Palette itself - PaletteFor is the only place a group is chosen for a
// given surface.
type Definition struct {
	Name         string
	NeovimModule string
	// Wallpaper is this theme file's declared default wallpaper path, if
	// any - see rawDefinition.Wallpaper. Empty for every theme devgeta
	// ships.
	Wallpaper string
	colors    Palette
	terminal  Palette
}

// group identifies which of a Definition's two color groups a surface reads.
// Unexported: nothing outside this package ever names a group directly (an
// app names itself and gets its group via PaletteFor).
type group int

const (
	groupColors group = iota
	groupTerminal
)

// surfaceGroup is ADR-0043's "which group each surface gets" table, in code.
// A surface with no entry here is not silently defaulted to colors: PaletteFor
// treats that as an error, so an eighth themed surface added without an entry
// fails loudly instead of rendering the wrong group.
var surfaceGroup = map[string]group{
	constants.Alacritty: groupTerminal,
	constants.Ghostty:   groupTerminal,
	constants.Tmux:      groupColors,
	constants.Neovim:    groupColors,
	constants.OpenCode:  groupColors,
	constants.Claude:    groupColors,
	constants.I3:        groupColors,
}

var hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Load reads and validates configs/themes/<name>.yaml from the extracted
// embedded configs (paths.Paths.App.Configs.Themes). Every required role is
// checked as present and parseable, and neovim_module is resolved against
// both the shipped and deployed Neovim trees, before any error-free
// Definition is returned - so a caller never has to check a partially valid
// theme.
func Load(name string) (Definition, error) {
	path := filepath.Join(paths.Paths.App.Configs.Themes, name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Two very different failures land here and want opposite
			// advice: the whole configs tree is unpublished (run an
			// install/configure), or the tree is fine and the user named a
			// theme that is not in it (here is what is). Telling them apart
			// by whether the directory itself exists is what keeps the
			// publish-your-configs message off an ordinary typo.
			if _, dirErr := os.Stat(paths.Paths.App.Configs.Themes); os.IsNotExist(dirErr) {
				return Definition{}, errUnpublishedConfigs(err)
			}
			available, listErr := List()
			if listErr != nil || len(available) == 0 {
				return Definition{}, fmt.Errorf("theme %q not found", name)
			}
			return Definition{}, fmt.Errorf(
				"theme %q not found (available: %s)",
				name, strings.Join(available, ", "),
			)
		}
		return Definition{}, fmt.Errorf("theme %q: %w", name, err)
	}

	var raw rawDefinition
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Definition{}, fmt.Errorf("theme %q: invalid yaml: %w", name, err)
	}

	if raw.Colors == nil {
		return Definition{}, fmt.Errorf("theme %q: colors: group is required", name)
	}
	if err := validatePalette(*raw.Colors, "colors"); err != nil {
		return Definition{}, fmt.Errorf("theme %q: %w", name, err)
	}

	terminal := *raw.Colors
	if raw.Terminal != nil {
		if err := validatePalette(*raw.Terminal, "terminal"); err != nil {
			return Definition{}, fmt.Errorf("theme %q: %w", name, err)
		}
		terminal = *raw.Terminal
	}

	if raw.NeovimModule == "" {
		return Definition{}, fmt.Errorf("theme %q: neovim_module is required", name)
	}
	if err := validateNeovimModule(raw.NeovimModule); err != nil {
		return Definition{}, fmt.Errorf("theme %q: %w", name, err)
	}

	return Definition{
		Name:         name,
		NeovimModule: raw.NeovimModule,
		Wallpaper:    resolveWallpaperPath(raw.Wallpaper),
		colors:       *raw.Colors,
		terminal:     terminal,
	}, nil
}

// resolveWallpaperPath turns a theme file's wallpaper: value into an
// absolute path. A relative value names an image shipped alongside the
// theme (configs/themes/wallpapers/<file>), which lands under
// paths.Paths.App.Configs.Themes once ExtractEmbeddedConfigs has run - so
// resolving it there is what makes a shipped wallpaper correct on whatever
// machine the binary was installed on. An absolute value is a path to the
// user's own image and is left exactly as written. Empty stays empty:
// filepath.Join would otherwise turn "" into the themes directory itself,
// handing a directory to a WallpaperSetter as though it were an image.
func resolveWallpaperPath(wallpaper string) string {
	if wallpaper == "" || filepath.IsAbs(wallpaper) {
		return wallpaper
	}
	return filepath.Join(paths.Paths.App.Configs.Themes, wallpaper)
}

// validatePalette checks every role in the Palette struct is present and
// parses as a #RRGGBB color, naming the group in every error so "which group
// is this role missing from" is never ambiguous (ADR-0043).
func validatePalette(p Palette, group string) error {
	v := reflect.ValueOf(p)
	t := v.Type()

	var missing []string
	var invalid []string
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("yaml")
		val := v.Field(i).String()
		switch {
		case val == "":
			missing = append(missing, tag)
		case !hexColorPattern.MatchString(val):
			invalid = append(invalid, fmt.Sprintf("%s=%q", tag, val))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("%s: missing role(s): %s", group, strings.Join(missing, ", "))
	}
	if len(invalid) > 0 {
		return fmt.Errorf("%s: invalid color value(s): %s", group, strings.Join(invalid, ", "))
	}
	return nil
}

// validateNeovimModule resolves module against the shipped tree
// (paths.Paths.App.Configs.Neovim, the extracted embedded configs) and the
// deployed tree (paths.Paths.Config.Nvim, the user's ~/.config/nvim),
// accepting either - see the cycle doc's Step 2 for why both trees have to be
// checked rather than the deployed one alone.
func validateNeovimModule(module string) error {
	shippedDir := filepath.Join(paths.Paths.App.Configs.Neovim, "lua", "devgeta", "themes")
	deployedDir := filepath.Join(paths.Paths.Config.Nvim, "lua", "devgeta", "themes")

	shippedPath := filepath.Join(shippedDir, module+".lua")
	deployedPath := filepath.Join(deployedDir, module+".lua")

	if fileExists(shippedPath) || fileExists(deployedPath) {
		return nil
	}

	return fmt.Errorf(
		"neovim module %q not found; looked in shipped tree %s (has: %v) and deployed tree %s (has: %v)",
		module,
		shippedDir,
		listLuaModules(shippedDir),
		deployedDir,
		listLuaModules(deployedDir),
	)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// listLuaModules returns the module names (basenames without .lua) found in
// dir, for a validateNeovimModule error message. Never fails: a missing or
// unreadable directory just reports no modules found there.
func listLuaModules(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".lua") {
			names = append(names, strings.TrimSuffix(e.Name(), ".lua"))
		}
	}
	sort.Strings(names)
	return names
}

// PaletteFor returns the one flat palette surface's template consumes,
// resolved from the single group table this package owns. A surface with no
// entry in that table is an error, not a silent fall back to colors:.
func (d Definition) PaletteFor(surface string) (Palette, error) {
	g, ok := surfaceGroup[surface]
	if !ok {
		return Palette{}, fmt.Errorf("theme: no palette group mapping for surface %q", surface)
	}
	if g == groupTerminal {
		return d.terminal, nil
	}
	return d.colors, nil
}

// List enumerates the themes available under paths.Paths.App.Configs.Themes,
// by filename (without the .yaml extension), sorted.
// errUnpublishedConfigs explains the one failure that is not about any
// particular theme: the extracted-configs tree does not exist, or belongs to
// an older build that had no themes/ directory at all.
//
// paths.Paths.App.Configs is a build-stamped pointer, republished only by
// devgeta.InstallIfStale - which `dg install` and `dg configure` call and
// the read-only theme commands deliberately do not (the cycle doc's Step 5
// rejected giving read-only commands filesystem side effects). So the first
// `dg theme` after upgrading to a binary that introduced themes reads the
// PREVIOUS build's tree and finds nothing, and the bare os error for that is
// "no such file or directory" naming a path the user never chose. Say what
// to run instead.
func errUnpublishedConfigs(cause error) error {
	return fmt.Errorf(
		"no themes found: %s does not exist.\n"+
			"devgeta's embedded configs have not been published for this build yet - "+
			"run `dg install`, or `dg configure <app> --force` for any installed app, "+
			"to extract them, then retry.\n(%w)",
		paths.Paths.App.Configs.Themes, cause,
	)
}

func List() ([]string, error) {
	entries, err := os.ReadDir(paths.Paths.App.Configs.Themes)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errUnpublishedConfigs(err)
		}
		return nil, fmt.Errorf("failed to list themes: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".yaml") {
			names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
		}
	}
	sort.Strings(names)
	return names, nil
}

// CurrentDefinition resolves current_theme from the global config (falling
// back to DefaultThemeName when it is empty) and loads it. This is the
// convenience a themed app's own ForceConfigure uses when it has not been
// handed an explicit theme (unlike `dg theme set`, which resolves the target
// theme itself and passes it in via apps.ThemedConfigurer - see the cycle
// doc's Step 5). Definition, not just Palette, because some apps need more
// than colors from it: OpenCode and Claude need Name for their rendered
// theme file's name and their config's "theme" field, and Neovim needs
// NeovimModule for its generated shim - PaletteFor(surface) is still the
// only place a color group is chosen, apps just call it on what this
// returns.
func CurrentDefinition() (Definition, error) {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return Definition{}, fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return Definition{}, fmt.Errorf("failed to load global config: %w", err)
	}
	name := gc.CurrentTheme
	if name == "" {
		name = DefaultThemeName
	}
	return Load(name)
}
