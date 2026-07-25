package registry

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/aerospace"
	"github.com/cjairm/devgeta/internal/apps/alacritty"
	"github.com/cjairm/devgeta/internal/apps/brave"
	"github.com/cjairm/devgeta/internal/apps/claude"
	"github.com/cjairm/devgeta/internal/apps/devgeta"
	"github.com/cjairm/devgeta/internal/apps/docker"
	"github.com/cjairm/devgeta/internal/apps/fastfetch"
	"github.com/cjairm/devgeta/internal/apps/flameshot"
	"github.com/cjairm/devgeta/internal/apps/gimp"
	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/apps/i3"
	"github.com/cjairm/devgeta/internal/apps/lazydocker"
	"github.com/cjairm/devgeta/internal/apps/lazygit"
	"github.com/cjairm/devgeta/internal/apps/mise"
	"github.com/cjairm/devgeta/internal/apps/neovim"
	"github.com/cjairm/devgeta/internal/apps/opencode"
	"github.com/cjairm/devgeta/internal/apps/raycast"
	"github.com/cjairm/devgeta/internal/apps/rtk"
	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/apps/ulauncher"
	"github.com/cjairm/devgeta/pkg/constants"
)

// AppMeta holds metadata for uninstall orchestration.
// ItemType must match what MaybeInstall* stored in global_config.yaml.
type AppMeta struct {
	Coordinator     string // "terminal" | "desktop" | "ai-tools" | ""
	ItemType        string // "package" | "desktop_app" — must match actual tracking
	HasShellFeature bool
}

// Meta maps every registered app name to its uninstall metadata.
var Meta = map[string]AppMeta{
	constants.Aerospace:  {Coordinator: "desktop", ItemType: "desktop_app", HasShellFeature: false},
	constants.Alacritty:  {Coordinator: "desktop", ItemType: "desktop_app", HasShellFeature: false},
	constants.Brave:      {Coordinator: "desktop", ItemType: "desktop_app", HasShellFeature: false},
	constants.Claude:     {Coordinator: "terminal", ItemType: "package", HasShellFeature: true},
	constants.Docker:     {Coordinator: "desktop", ItemType: "desktop_app", HasShellFeature: false},
	constants.Fastfetch:  {Coordinator: "terminal", ItemType: "package", HasShellFeature: false},
	constants.Flameshot:  {Coordinator: "desktop", ItemType: "desktop_app", HasShellFeature: false},
	constants.Gimp:       {Coordinator: "desktop", ItemType: "desktop_app", HasShellFeature: false},
	constants.Git:        {Coordinator: "terminal", ItemType: "package", HasShellFeature: false},
	constants.I3:         {Coordinator: "desktop", ItemType: "package", HasShellFeature: false},
	constants.LazyDocker: {Coordinator: "terminal", ItemType: "package", HasShellFeature: true},
	constants.LazyGit:    {Coordinator: "terminal", ItemType: "package", HasShellFeature: true},
	constants.Mise:       {Coordinator: "terminal", ItemType: "package", HasShellFeature: true},
	constants.Neovim:     {Coordinator: "terminal", ItemType: "package", HasShellFeature: true},
	constants.OpenCode:   {Coordinator: "terminal", ItemType: "package", HasShellFeature: true},
	constants.Raycast:    {Coordinator: "desktop", ItemType: "desktop_app", HasShellFeature: false},
	constants.Rtk:        {Coordinator: "ai-tools", ItemType: "package", HasShellFeature: false},
	constants.Tmux:       {Coordinator: "terminal", ItemType: "package", HasShellFeature: true},
	constants.Ulauncher:  {Coordinator: "desktop", ItemType: "desktop_app", HasShellFeature: false},
	constants.DevgetaApp: {Coordinator: "", ItemType: "", HasShellFeature: false},
}

// knownCategories is the set of valid uninstall coordinator categories.
var knownCategories = map[string]bool{
	"terminal": true,
	"desktop":  true,
	"ai-tools": true,
}

// IsKnownApp reports whether name is a registered app (excluding devgeta itself).
func IsKnownApp(name string) bool {
	_, ok := Meta[name]
	return ok && name != constants.DevgetaApp
}

// IsKnownCategory reports whether name is a valid coordinator category.
func IsKnownCategory(name string) bool {
	return knownCategories[name]
}

// KnownCategories returns a sorted slice of valid coordinator categories.
func KnownCategories() []string {
	cats := make([]string, 0, len(knownCategories))
	for c := range knownCategories {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	return cats
}

// AppsByCoordinator returns sorted app names whose Coordinator matches the given value.
func AppsByCoordinator(coordinator string) []string {
	var result []string
	for name, meta := range Meta {
		if meta.Coordinator == coordinator {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}

// factories maps app names to lazy constructors so apps are only instantiated when needed.
var factories = map[string]func() apps.App{
	constants.Aerospace:  func() apps.App { return aerospace.New() },
	constants.Alacritty:  func() apps.App { return alacritty.New() },
	constants.Brave:      func() apps.App { return brave.New() },
	constants.Claude:     func() apps.App { return claude.New() },
	constants.DevgetaApp: func() apps.App { return devgeta.New() },
	constants.Docker:     func() apps.App { return docker.New() },
	constants.Fastfetch:  func() apps.App { return fastfetch.New() },
	constants.Flameshot:  func() apps.App { return flameshot.New() },
	constants.Gimp:       func() apps.App { return gimp.New() },
	constants.Git:        func() apps.App { return git.New() },
	constants.I3:         func() apps.App { return i3.New() },
	constants.LazyDocker: func() apps.App { return lazydocker.New() },
	constants.LazyGit:    func() apps.App { return lazygit.New() },
	constants.Mise:       func() apps.App { return mise.New() },
	constants.Neovim:     func() apps.App { return neovim.New() },
	constants.OpenCode:   func() apps.App { return opencode.New() },
	constants.Raycast:    func() apps.App { return raycast.New() },
	constants.Rtk:        func() apps.App { return rtk.New() },
	constants.Tmux:       func() apps.App { return tmux.New() },
	constants.Ulauncher:  func() apps.App { return ulauncher.New() },
}

// GetApp returns the App for the given name, or an error listing all supported names.
func GetApp(name string) (apps.App, error) {
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown app %q\n\nSupported apps:\n  %s", name, formatNames())
	}
	return factory(), nil
}

// Names returns a sorted slice of all registered app names.
func Names() []string {
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// GetAppsByKind returns sorted names of all registered apps matching the given kind.
func GetAppsByKind(kind apps.AppKind) []string {
	var names []string
	for name, factory := range factories {
		if factory().Kind() == kind {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// formatNames formats registered names into aligned columns for display.
func formatNames() string {
	names := Names()
	const cols = 7
	var rows []string
	for i := 0; i < len(names); i += cols {
		end := i + cols
		if end > len(names) {
			end = len(names)
		}
		rows = append(rows, strings.Join(names[i:end], "  "))
	}
	return strings.Join(rows, "\n  ")
}
