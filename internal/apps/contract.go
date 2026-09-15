package apps

import "github.com/cjairm/devgeta/internal/theme"

// AppKind classifies what kind of application an app is.
type AppKind int

const (
	KindUnknown  AppKind = iota
	KindTerminal         // CLI tools, terminal emulators, shell utilities
	KindDesktop          // GUI desktop applications
	KindLanguage         // Programming language runtimes and managers
	KindDatabase         // Database systems
	KindFont             // Font packages (satisfies FontInstaller, not App)
	KindMeta             // Devgeta itself
)

// App is the contract every app module must satisfy.
// Fonts is the only exception — it satisfies FontInstaller instead.
type App interface {
	Name() string
	Kind() AppKind

	Install() error
	ForceInstall() error
	SoftInstall() error

	ForceConfigure() error
	SoftConfigure() error

	Uninstall() error
	Update() error

	ExecuteCommand(args ...string) error
}

// SelectiveConfigurer is an optional interface for apps whose configuration
// includes discrete, separately-refreshable parts. It backs
// `dg configure <app> --force --only=...`. What a "part" is belongs to the
// app: the AI coders (claude, opencode) expose their shared
// skills/commands/agents subtrees so those can be overwritten without
// disturbing edited config, plus an "rtk" part that wires rtk's
// command-rewriting hook into that coder — the explicit opt-in required by
// ADR-0004. Apps that don't implement it reject --only.
type SelectiveConfigurer interface {
	// ConfigurableParts lists the part names accepted by --only.
	ConfigurableParts() []string
	// ForceConfigureParts overwrites only the named parts, leaving all other
	// configuration in place.
	ForceConfigureParts(parts []string) error
}

// ThemedConfigurer is implemented by the seven apps with a theme surface
// (Alacritty, Ghostty, tmux, Neovim, OpenCode, Claude, i3). It exists because
// ForceConfigure() takes no arguments and resolves its theme from
// current_theme, which `dg theme set` deliberately has not written yet when
// it needs to render the *target* theme — see
// docs/plans/cycles/2026-09-14-dg-theme.md's Step 5. `dg theme set` resolves
// the requested theme itself and calls ForceConfigureTheme directly with the
// already-loaded Definition; an app's own ForceConfigure calls it too, via
// theme.CurrentDefinition(), so there is exactly one configure body per app.
//
// The argument is the full Definition, not just a Palette: every app calls
// def.PaletteFor(a.Name()) to get its own color group (PaletteFor stays the
// only place a group is chosen — no app or template picks colors: vs
// terminal: itself), but OpenCode and Claude also need def.Name (their
// rendered theme file's name and their config's "theme" field) and Neovim
// needs def.NeovimModule (its generated shim) — neither of which a flat
// Palette carries.
type ThemedConfigurer interface {
	ForceConfigureTheme(def theme.Definition) error
}

// LiveThemeApplier is implemented by a themed app that can push a theme into
// an already-running process — tmux's `source-file` into a live session is
// the only case today. `dg theme set` calls it only after its transaction has
// committed, because a live push cannot be rolled back (cycle doc Step 5).
type LiveThemeApplier interface {
	ApplyLiveTheme() error
}

// FontInstaller is the contract for the Fonts module, which installs named fonts
// rather than a single application.
type FontInstaller interface {
	Name() string
	Kind() AppKind
	Available() []string
	SoftInstallAll()
	InstallFont(name string) error
	ForceInstallFont(name string) error
	SoftInstallFont(name string) error
	UninstallFont(name string) error
}
