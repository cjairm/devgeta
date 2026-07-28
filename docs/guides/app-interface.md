# App Interface Guide

Every app in `internal/apps/` satisfies the `App` interface defined in `internal/apps/contract.go`. This document explains the contract, the `AppKind` taxonomy, the sentinel errors, and the constructor patterns.

---

## The `App` Interface

```go
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
```

All apps must add a compile-time assertion immediately after the struct declaration:

```go
var _ apps.App = (*MyApp)(nil)
```

If any method is missing or has the wrong signature, the build fails. This is the primary mechanism for catching interface drift.

### Reference implementation

`internal/apps/claude/claude.go` is the canonical example. Read it before implementing a new app.

---

## Method Semantics

| Method           | Semantics                                                                           |
| ---------------- | ----------------------------------------------------------------------------------- |
| `Name()`         | Returns the canonical constant name (e.g., `constants.Alacritty`). Used by future `dg uninstall <name>`. |
| `Kind()`         | Returns the `AppKind` for dispatch (see below).                                     |
| `Install()`      | Unconditional install. May re-install if already present.                           |
| `ForceInstall()` | Uninstall then re-install. Use `baseapp.Reinstall` — see below.                     |
| `SoftInstall()`  | Install only if not already present. Idempotent.                                    |
| `ForceConfigure()` | Apply configuration, overwriting any existing config.                             |
| `SoftConfigure()` | Apply configuration only if not already present. Idempotent.                      |
| `Uninstall()`    | Remove the app. Return `ErrUninstallNotSupported` if not implemented.               |
| `Update()`       | Upgrade to latest version. Return `ErrUpdateNotSupported` if not implemented.       |
| `ExecuteCommand(args ...string)` | Run an app-specific command. Return `ErrExecuteNotSupported` if unused. |

---

## `AppKind` Enum

```go
type AppKind int

const (
    KindUnknown  AppKind = iota
    KindTerminal           // CLI tools: git, neovim, tmux, lazygit, ...
    KindDesktop            // GUI apps: docker, brave, aerospace, ...
    KindLanguage           // Language runtimes (managed by tooling/languages)
    KindDatabase           // Database servers (managed by tooling/databases)
    KindFont               // Font packages (Fonts uses FontInstaller, not App)
    KindMeta               // devgeta itself
)
```

`Kind()` is used by future commands (`dg uninstall`, `dg configure`) to dispatch on app type.

---

## Sentinel Errors

```go
var (
    ErrUninstallNotSupported = errors.New("uninstall not supported")
    ErrUpdateNotSupported    = errors.New("update not supported")
    ErrConfigureNotSupported = errors.New("configure not supported")
    ErrExecuteNotSupported   = errors.New("execute not supported")
)
```

**Always wrap with `%w`** so callers can use `errors.Is`:

```go
func (a *MyApp) Uninstall() error {
    return fmt.Errorf("%w for myapp", apps.ErrUninstallNotSupported)
}
```

**Never** use free-form strings like `"uninstall not supported for X"` — they break `errors.Is`.

In tests, assert unsupported operations with:

```go
if !errors.Is(err, apps.ErrUninstallNotSupported) {
    t.Errorf("expected ErrUninstallNotSupported, got: %v", err)
}
```

---

## `ForceInstall` and `baseapp.Reinstall`

The broken pre-cycle pattern:

```go
// WRONG — always fails when Uninstall returns ErrUninstallNotSupported
func (a *MyApp) ForceInstall() error {
    if err := a.Uninstall(); err != nil {
        return fmt.Errorf("failed to uninstall: %w", err)
    }
    return a.Install()
}
```

The correct pattern using `baseapp.Reinstall`:

```go
func (a *MyApp) ForceInstall() error {
    return baseapp.Reinstall(a.Install, a.Uninstall)
}
```

`baseapp.Reinstall` skips uninstall when it returns `ErrUninstallNotSupported` and proceeds directly to install. If uninstall returns any other error, it propagates.

---

## Constructor Patterns

There are two constructor patterns in the fleet, both intentional.

### Pattern A — With `Base` (terminal tools, database tools)

Apps that execute shell commands at runtime need `Base`:

```go
type Neovim struct {
    Cmd  cmd.Command
    Base cmd.BaseCommandExecutor
}

func New() *Neovim {
    return &Neovim{Cmd: cmd.NewCommand(), Base: cmd.NewBaseCommand()}
}
```

Use this when `ExecuteCommand` runs a real binary or when install logic calls `Base.ExecCommand`.

### Pattern B — Without `Base` (desktop GUI apps)

Apps that only install packages and copy config files don't need `Base`:

```go
type Brave struct {
    Cmd cmd.Command
}

func New() *Brave {
    return &Brave{Cmd: cmd.NewCommand()}
}
```

`ExecuteCommand` for these apps is a no-op or returns `ErrExecuteNotSupported`.

**Rule:** if `ExecuteCommand` is a no-op and no method calls `Base.ExecCommand`, use Pattern B.

---

## The `FontInstaller` Outlier

Fonts is the only app where install/uninstall methods accept a `fontName string` parameter. It satisfies `FontInstaller`, not `App`:

```go
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
```

`var _ apps.FontInstaller = (*Fonts)(nil)` enforces this at compile time.

Future commands that dispatch over `[]App` will need to handle `KindFont` separately, since `Fonts` does not implement `App`.

---

## Adding a New App

1. Create `internal/apps/{name}/{name}.go`
2. Add `var _ apps.App = (*Name)(nil)` after the struct
3. Implement all interface methods (use `ErrXxxNotSupported` for stubs)
4. Use `baseapp.Reinstall` in `ForceInstall`
5. Create `internal/apps/{name}/{name}_test.go` — see [testing-patterns.md](testing-patterns.md)
6. Register in the appropriate category coordinator in `internal/tooling/`
7. Document in `docs/apps/{name}.md`
