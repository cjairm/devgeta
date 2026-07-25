package apps

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
