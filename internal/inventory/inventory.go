package inventory

import (
	cmdpkg "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/databases"
	"github.com/cjairm/devgeta/internal/tooling/languages"
)

// ItemState is the result of a live drift check for one tracked item.
type ItemState int

const (
	// StateOK: the presence check ran and definitively found the item.
	StateOK ItemState = iota
	// StateMissing: the check ran and definitively did not find the item.
	StateMissing
	// StateUnknown: the check itself failed to run (e.g. brew/dpkg unavailable).
	// Never conflated with StateMissing, so a flaky package manager never reads as drift.
	StateUnknown
)

func (s ItemState) String() string {
	switch s {
	case StateOK:
		return "OK"
	case StateMissing:
		return "MISSING"
	default:
		return "UNKNOWN"
	}
}

// Item is one tracked piece of devgeta state plus its live drift-check result.
type Item struct {
	Name     string
	Category string // "packages", "desktop_apps", "fonts", "themes", "terminal_tools", "dev_languages", "databases"
	Source   string // "installed" (devgeta installed it) or "pre-existing" (found already on the system)
	State    ItemState
	Detail   string // populated when State == StateUnknown (the check error's message)
}

// CategoryInfo pairs a category key with its display label, in the fixed display
// order used by the `dg list` dashboard.
type CategoryInfo struct {
	Key   string
	Label string
}

// Categories is the canonical 7-category vocabulary and display order.
var Categories = []CategoryInfo{
	{Key: "packages", Label: "Packages"},
	{Key: "desktop_apps", Label: "Desktop Apps"},
	{Key: "fonts", Label: "Fonts"},
	{Key: "themes", Label: "Themes"},
	{Key: "terminal_tools", Label: "Terminal Tools"},
	{Key: "dev_languages", Label: "Dev Languages"},
	{Key: "databases", Label: "Databases"},
}

// Collector runs presence checks for every item devgeta has tracked, for both
// the "installed" and "already_installed" buckets of global_config.yaml.
type Collector struct {
	Cmd  cmdpkg.Command
	Base cmdpkg.BaseCommandExecutor
}

// Collect is read-only by contract: it never calls gc.Save() or otherwise
// writes global_config.yaml, and never calls languages.New() / databases.New()
// (which would shell out for every configured — not just tracked — language and
// database, and can silently persist newly-detected pre-existing installs).
func (c *Collector) Collect(gc *config.GlobalConfig) []Item {
	dl := &languages.DevLanguages{Cmd: c.Cmd, Base: c.Base}
	db := &databases.Databases{Cmd: c.Cmd, Base: c.Base}

	var items []Item
	items = append(
		items,
		c.collectCategory(
			"packages",
			gc.Installed.Packages,
			gc.AlreadyInstalled.Packages,
			c.checkPackage,
		)...)
	items = append(
		items,
		c.collectCategory(
			"desktop_apps",
			gc.Installed.DesktopApps,
			gc.AlreadyInstalled.DesktopApps,
			c.checkDesktopApp,
		)...)
	items = append(
		items,
		c.collectCategory("fonts", gc.Installed.Fonts, gc.AlreadyInstalled.Fonts, c.checkFont)...)
	items = append(
		items,
		c.collectCategory(
			"themes",
			gc.Installed.Themes,
			gc.AlreadyInstalled.Themes,
			checkNotImplemented,
		)...)
	items = append(
		items,
		c.collectCategory(
			"terminal_tools",
			gc.Installed.TerminalTools,
			gc.AlreadyInstalled.TerminalTools,
			checkNotImplemented,
		)...)
	items = append(
		items,
		c.collectCategory(
			"dev_languages",
			gc.Installed.DevLanguages,
			gc.AlreadyInstalled.DevLanguages,
			checkLanguageFn(dl),
		)...)
	items = append(
		items,
		c.collectCategory(
			"databases",
			gc.Installed.Databases,
			gc.AlreadyInstalled.Databases,
			checkDatabaseFn(db),
		)...)
	return items
}

type checkFn func(name string) (ItemState, string)

func (c *Collector) collectCategory(
	category string,
	installed, alreadyInstalled []string,
	check checkFn,
) []Item {
	var items []Item
	for _, name := range installed {
		state, detail := check(name)
		items = append(
			items,
			Item{Name: name, Category: category, Source: "installed", State: state, Detail: detail},
		)
	}
	for _, name := range alreadyInstalled {
		state, detail := check(name)
		items = append(
			items,
			Item{
				Name:     name,
				Category: category,
				Source:   "pre-existing",
				State:    state,
				Detail:   detail,
			},
		)
	}
	return items
}

func (c *Collector) checkPackage(name string) (ItemState, string) {
	ok, err := c.Cmd.IsPackageInstalled(name)
	return stateFromCheck(ok, err)
}

func (c *Collector) checkDesktopApp(name string) (ItemState, string) {
	ok, err := c.Cmd.IsDesktopAppInstalled(name)
	return stateFromCheck(ok, err)
}

func (c *Collector) checkFont(name string) (ItemState, string) {
	ok, err := c.Base.IsFontPresent(name)
	return stateFromCheck(ok, err)
}

func checkLanguageFn(dl *languages.DevLanguages) checkFn {
	return func(name string) (ItemState, string) {
		if dl.IsInstalledOnSystem(name) {
			return StateOK, ""
		}
		return StateMissing, ""
	}
}

func checkDatabaseFn(db *databases.Databases) checkFn {
	return func(name string) (ItemState, string) {
		if db.IsInstalledOnSystem(name) {
			return StateOK, ""
		}
		return StateMissing, ""
	}
}

// checkNotImplemented backs the themes/terminal_tools categories, which no
// current code path ever populates. If a future feature (e.g. `dg change
// --theme`) starts populating them, tracked items surface as UNKNOWN here
// until a real presence check is added — never silently reported OK or MISSING.
func checkNotImplemented(name string) (ItemState, string) {
	return StateUnknown, "presence check not implemented for this category"
}

func stateFromCheck(ok bool, err error) (ItemState, string) {
	if err != nil {
		return StateUnknown, err.Error()
	}
	if ok {
		return StateOK, ""
	}
	return StateMissing, ""
}
