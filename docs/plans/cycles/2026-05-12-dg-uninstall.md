# Cycle: dg uninstall [app/category]

**Date:** 2026-05-12  
**Estimated Duration:** ~14-18 hours (40+ file touches, 18 app uninstall paths + tests; Steps 5-8 are parallelizable)  
**Status:** Done

---

## 1. Domain Context

Devgeta tracks everything it installs in `~/.config/devgeta/global_config.yaml` under two sections:

- `installed` — packages devgeta installed itself
- `already_installed` — packages that were pre-existing when devgeta ran

`dg uninstall` reverses the install process: removes the binary/package, removes config files devgeta wrote, disables shell features, and updates `global_config.yaml`. Pre-existing packages are never touched.

Each app's `Uninstall()` is self-contained — it handles binary removal, config cleanup, shell feature disabling, and global config updates, exactly mirroring how `Install` + `ForceConfigure` work.

Related: [ROADMAP.md](../../../ROADMAP.md) · [docs/guides/app-interface.md](../../guides/app-interface.md) · [docs/guides/testing-patterns.md](../../guides/testing-patterns.md)

---

## 2. Engineer Context

### Key files

| File | Role |
|------|------|
| `internal/commands/factory.go` | `Command` interface — needs `UninstallPackage`, `UninstallDesktopApp` |
| `internal/commands/macos.go` | macOS impl — needs `UninstallPackage` (`brew uninstall`), `UninstallDesktopApp` (`brew uninstall --cask`) |
| `internal/commands/debian.go` | Debian impl — needs `UninstallPackage` (`apt-get remove -y`), `UninstallDesktopApp` (same as package on Debian) |
| `internal/commands/mock.go` | Already has `UninstallPackage` — needs `UninstallDesktopApp` added |
| `internal/config/fromFile.go` | Needs `RemoveFromInstalled(itemName, itemType string)` |
| `internal/apps/{app}/{app}.go` | 18 app files — all need real `Uninstall()` |
| `internal/apps/registry/registry.go` | Add `AppMeta` map and helpers (keep in current package — see import cycle note) |
| `cmd/install.go` | No import change needed (already uses `internal/apps/registry`) |
| `cmd/uninstall.go` | **New** — `dg uninstall <app|category>` command |

### Current state of `UninstallPackage`

`MockCommand` already has `UninstallPackage` (with `UninstalledPkg` and `UninstallError` fields) but the `Command` interface in `factory.go` does NOT declare it, and neither `MacOSCommand` nor `DebianCommand` implement it. This must be fixed before any app `Uninstall()` can call it.

### Per-app uninstall specification

Each app's `Uninstall()` must do all of: binary/package removal → config dir removal (best-effort) → shell feature disable → shell config regeneration (if applicable) → `gc.RemoveFromInstalled()` → `gc.Save()`. Config removal failures are logged but do not block state updates — the binary is the critical artifact; leftover config dirs are harmless and can be cleaned up manually.

| App | Coordinator | Binary removal | Config path to remove | Shell feature |
|-----|-------------|---------------|----------------------|---------------|
| `aerospace` | desktop | `Cmd.UninstallDesktopApp("nikitabobko/tap/aerospace")` (macOS) | `paths.Config.Aerospace` | none |
| `alacritty` | desktop | `Cmd.UninstallDesktopApp(alacritty)` | `paths.Config.Alacritty` | none |
| `brave` | desktop | `Cmd.UninstallDesktopApp("brave-browser")` | none | none |
| `claude` | terminal | `Base.ExecCommand("npm uninstall -g @anthropic-ai/claude-code")` | `paths.Config.Claude` | `constants.Claude` |
| `docker` | desktop | `Cmd.UninstallDesktopApp(docker)` | none | none |
| `fastfetch` | terminal | `Cmd.UninstallPackage(fastfetch)` | `paths.Config.Fastfetch` | none |
| `flameshot` | desktop | `Cmd.UninstallDesktopApp(flameshot)` | none | none |
| `gimp` | desktop | `Cmd.UninstallDesktopApp(gimp)` | none | none |
| `git` | terminal | `Cmd.UninstallPackage(git)` | `paths.Config.Git` | none |
| `i3` | desktop | `Cmd.UninstallPackage(i3)` | `paths.Config.I3` | none |
| `lazydocker` | terminal | macOS: `Cmd.UninstallPackage("jesseduffield/lazydocker/lazydocker")` · Linux: `sudo rm /usr/local/bin/lazydocker` | none | `constants.LazyDocker` |
| `lazygit` | terminal | macOS: `Cmd.UninstallPackage(lazygit)` · Linux: `sudo rm /usr/local/bin/lazygit` | none | `constants.LazyGit` |
| `mise` | terminal | `Cmd.UninstallPackage(mise)` | none | `constants.Mise` |
| `neovim` | terminal | macOS: `Cmd.UninstallPackage(neovim)` · Linux: `sudo rm /usr/local/bin/nvim` + `sudo rm -rf /usr/local/lib/nvim*` + `sudo rm -rf /usr/local/share/nvim*` | `paths.Config.Nvim` | `constants.Neovim` |
| `opencode` | terminal | `Cmd.UninstallPackage(opencode)` | `paths.Config.OpenCode` | `constants.OpenCode` |
| `raycast` | desktop | `Cmd.UninstallDesktopApp(raycast)` (macOS only; no-op on Linux) | none | none |
| `tmux` | terminal | `Cmd.UninstallPackage(tmux)` | `~/.tmux.conf` (single file, not dir) | `constants.Tmux` |
| `ulauncher` | desktop | `Cmd.UninstallDesktopApp(ulauncher)` (Linux only; no-op on macOS) | none | none |

### ⚠️ REVISED: Item type model — actual tracking vs aspirational

**Critical finding from codebase audit:** Apps are tracked during `Install()` by `MaybeInstallPackage` or `MaybeInstallDesktopApp` in `internal/commands/base.go` (line 324). These methods call `AddToInstalled` internally with these item types:

- `MaybeInstallPackage` → tracks as `"package"`
- `MaybeInstallDesktopApp` → tracks as `"desktop_app"`

This means the **actual item types in `global_config.yaml`** for existing installations are:

| App | Tracked via | Actual item type in global_config |
|-----|------------|----------------------------------|
| `aerospace` | `MaybeInstallDesktopApp` | `"desktop_app"` |
| `alacritty` | `MaybeInstallDesktopApp` | `"desktop_app"` |
| `brave` | `MaybeInstallDesktopApp` | `"desktop_app"` |
| `claude` | `MaybeInstallPackage` + explicit `AddToInstalled` | `"package"` |
| `docker` | `MaybeInstallDesktopApp` | `"desktop_app"` |
| `fastfetch` | `MaybeInstallPackage` | `"package"` |
| `flameshot` | `MaybeInstallDesktopApp` | `"desktop_app"` |
| `gimp` | `MaybeInstallDesktopApp` | `"desktop_app"` |
| `git` | `MaybeInstallPackage` | `"package"` |
| `i3` | `MaybeInstallPackage` | `"package"` |
| `lazydocker` | `MaybeInstallPackage` | `"package"` |
| `lazygit` | `MaybeInstallPackage` | `"package"` |
| `mise` | `MaybeInstallPackage` | `"package"` |
| `neovim` | `MaybeInstallPackage` | `"package"` |
| `opencode` | `MaybeInstallPackage` + explicit `AddToInstalled` | `"package"` |
| `raycast` | `MaybeInstallDesktopApp` | `"desktop_app"` |
| `tmux` | `MaybeInstallPackage` | `"package"` |
| `ulauncher` | `MaybeInstallDesktopApp` | `"desktop_app"` |

**The previous draft's `AppEntry.ItemType` table was wrong for 9 apps** (fastfetch, git, i3, lazydocker, lazygit, mise, neovim, tmux were listed as `"terminal_tool"` but are actually tracked as `"package"`). Using the wrong item type in `RemoveFromInstalled` would silently fail to remove the entry.

**Decision:** `RemoveFromInstalled` and `IsInstalledByDevgeta` calls in `Uninstall()` must use the **actual tracked item type** (`"package"` or `"desktop_app"`), matching what `MaybeInstall*` stored. The `AppMeta` map below reflects reality.

### ⚠️ REVISED: Registry stays in `internal/apps/registry/` — no move

**Import cycle risk:** The original plan proposed moving the registry to `internal/registry/` for "broader reuse." However, the registry imports all app packages (`internal/apps/aerospace`, `internal/apps/alacritty`, etc.) for its factory map. If `cmd/uninstall.go` or any non-app package imports `internal/registry`, that's fine — but if any app package ever needed to import the registry (e.g., to look up its own metadata), it would create an import cycle.

**The current location `internal/apps/registry/` is correct.** The registry's factory map must live alongside app imports. Instead of moving it, we add metadata to the same package:

```go
// AppMeta holds metadata for uninstall orchestration.
// ItemType must match what MaybeInstall* stored in global_config.yaml.
type AppMeta struct {
    Coordinator     string // "terminal" | "desktop" | ""
    ItemType        string // "package" | "desktop_app" — must match actual tracking
    HasShellFeature bool
}

var Meta = map[string]AppMeta{
    "aerospace":  {Coordinator: "desktop",   ItemType: "desktop_app",  HasShellFeature: false},
    "alacritty":  {Coordinator: "desktop",   ItemType: "desktop_app",  HasShellFeature: false},
    "brave":      {Coordinator: "desktop",   ItemType: "desktop_app",  HasShellFeature: false},
    "claude":     {Coordinator: "terminal",  ItemType: "package",      HasShellFeature: true},
    "docker":     {Coordinator: "desktop",   ItemType: "desktop_app",  HasShellFeature: false},
    "fastfetch":  {Coordinator: "terminal",  ItemType: "package",      HasShellFeature: false},
    "flameshot":  {Coordinator: "desktop",   ItemType: "desktop_app",  HasShellFeature: false},
    "gimp":       {Coordinator: "desktop",   ItemType: "desktop_app",  HasShellFeature: false},
    "git":        {Coordinator: "terminal",  ItemType: "package",      HasShellFeature: false},
    "i3":         {Coordinator: "desktop",   ItemType: "package",      HasShellFeature: false},
    "lazydocker": {Coordinator: "terminal",  ItemType: "package",      HasShellFeature: true},
    "lazygit":    {Coordinator: "terminal",  ItemType: "package",      HasShellFeature: true},
    "mise":       {Coordinator: "terminal",  ItemType: "package",      HasShellFeature: true},
    "neovim":     {Coordinator: "terminal",  ItemType: "package",      HasShellFeature: true},
    "opencode":   {Coordinator: "terminal",  ItemType: "package",      HasShellFeature: true},
    "raycast":    {Coordinator: "desktop",   ItemType: "desktop_app",  HasShellFeature: false},
    "tmux":       {Coordinator: "terminal",  ItemType: "package",      HasShellFeature: true},
    "ulauncher":  {Coordinator: "desktop",   ItemType: "desktop_app",  HasShellFeature: false},
    "devgeta":    {Coordinator: "",           ItemType: "",             HasShellFeature: false},
}
```

Add helpers: `IsKnownApp`, `IsKnownCategory`, `KnownCategories`, `AppsByCoordinator`.

### ⚠️ REVISED: "14 apps missing tracking" is overstated

**Previous claim:** 14 apps don't call `AddToInstalled` in `ForceConfigure`, so `IsInstalledByDevgeta` returns false.

**Actual situation:** All 18 apps ARE tracked during `Install()` by `MaybeInstallPackage`/`MaybeInstallDesktopApp` (which call `AddToInstalled` internally in `base.go:324`). The "gap" only affects `dg configure --force <app>` when run standalone without a prior install — a rare edge case.

**Revised fix:** Still add `AddToInstalled` to the 14 apps' `ForceConfigure()` for correctness, but **this is NOT a prerequisite for uninstall**. Apps installed via `dg install` are already tracked. Demote this from "critical prerequisite" (Step 4) to "cleanup" (final step). The uninstall orchestrator will work correctly for normally-installed apps without this fix.

**Item types for `ForceConfigure` AddToInstalled calls must match the actual tracking:** use `"package"` for terminal apps, `"desktop_app"` for desktop apps — matching what `MaybeInstall*` uses. The 4 apps that already have explicit `AddToInstalled` calls (aerospace: `"desktop_app"`, alacritty: `"desktop_app"`, claude: `"package"`, opencode: `"package"`) are already correct.

### Uninstall outcome decision table

One definitive rule for every exit path from `app.Uninstall()`:

| Outcome | Binary removed? | gc.RemoveFromInstalled | gc.DisableShellFeature | gc.RegenerateShellConfig | gc.Save | Command exit |
|---------|----------------|----------------------|----------------------|--------------------------|---------|-------------|
| Success | ✅ | ✅ | ✅ (if applicable) | ✅ (if applicable) | ✅ | 0 |
| Binary removal fails | ❌ | ❌ | ❌ | ❌ | ❌ | error logged, continue batch |
| Config removal fails | ✅ | ✅ | ✅ (if applicable) | ✅ (if applicable) | ✅ | 0 (warning logged) |
| gc.Save fails (after all else succeeds) | ✅ | done in-memory | done in-memory | done | ❌ persisted | error, stop for this app |

**Rule:** state is only persisted after binary removal succeeds. Config removal is best-effort (`_ = os.RemoveAll`) — failures are logged as warnings but do not block gc updates. This matches the implementation pattern used for tmux, neovim, and claude. Partial success (binary gone, gc.Save failed) is logged as an error — the user knows to clean up manually. The orchestrator collects all per-app errors and returns non-zero if any app failed.

`ErrUninstallNotSupported` will not occur for any of the 18 apps after this cycle. If it somehow appears (e.g. future app added to registry but Uninstall not updated), treat it as a hard error — do NOT update global config.

**⚠️ Item type in `RemoveFromInstalled` calls:** Every `Uninstall()` must use the item type that matches what `MaybeInstall*` stored. Terminal apps installed via `MaybeInstallPackage` use `"package"`, not `"terminal_tool"`. Desktop apps installed via `MaybeInstallDesktopApp` use `"desktop_app"`. Getting this wrong means the entry stays in global_config after uninstall.

### Global config `RemoveFromInstalled` — new method needed

```go
// RemoveFromInstalled removes itemName from the installed tracking list for itemType.
func (gc *GlobalConfig) RemoveFromInstalled(itemName, itemType string) {
    slice := gc.getInstalledSlice(itemType)
    if slice == nil {
        return
    }
    result := (*slice)[:0]
    for _, v := range *slice {
        if v != itemName {
            result = append(result, v)
        }
    }
    *slice = result
}
```

### Testing patterns

- Every `Uninstall()` test uses `testutil.NewMockApp()` — inject `MockCommand` and `MockBaseCommand`
- Always verify: `testutil.VerifyNoRealCommands(t, mockApp.Base)`
- Test success path, error-from-binary-removal path, and (for shell-feature apps) gc-save-failure path
- `func init() { testutil.InitLogger() }` in every test file
- See [docs/guides/testing-patterns.md](../../guides/testing-patterns.md)

### Commands to verify

```bash
go test ./internal/commands/
go test ./internal/config/
go test ./internal/registry/
go test ./internal/apps/...
go test ./cmd/
go test ./...
make lint
```

---

## 3. Objective

Implement `dg uninstall <app|category>` that fully reverses the install process (binary removal + config cleanup + shell feature disable + global config update) for all 18 registry apps, with mocked tests for each, and add the supporting `UninstallPackage`/`UninstallDesktopApp` methods to the `Command` layer.

---

## 4. Scope Boundary

### In Scope

- [ ] Add `UninstallPackage` and `UninstallDesktopApp` to `Command` interface + implement on `MacOSCommand`, `DebianCommand` + add `UninstallDesktopApp` to `MockCommand`
- [ ] Add `RemoveFromInstalled` to `GlobalConfig`
- [ ] Add `AppMeta` struct and `Meta` map to `internal/apps/registry/` (NO package move — avoids import cycle risk); add filter helpers (`IsKnownApp`, `IsKnownCategory`, `AppsByCoordinator`)
- [ ] Implement real `Uninstall()` for all 18 apps per the spec table above, using correct item types (`"package"` for terminal apps, `"desktop_app"` for desktop apps)
- [ ] Mocked tests for every app's `Uninstall()` (success + failure paths)
- [ ] Implement `cmd/uninstall.go` with `dg uninstall <app|category>`
- [ ] (Cleanup) Fix `AddToInstalled` tracking in `ForceConfigure` for the 14 apps that are missing it — not a prerequisite for uninstall (apps are tracked during `Install()` by `MaybeInstall*`)
- [ ] Orchestrator checks `IsInstalledByDevgeta` before calling `Uninstall()`; skips pre-existing apps with a warning
- [ ] `dg uninstall languages` / `dg uninstall databases` fail fast with explicit "not supported yet" error
- [ ] `dg uninstall devgeta` fails fast with "cannot uninstall devgeta from itself"
- [ ] Print "run `source ~/.zshrc` to apply shell changes" after any shell-feature-touching uninstall
- [ ] Command registered via `init()` in `cmd/uninstall.go` (no `cmd/root.go` change needed)

### Explicitly Out of Scope

- Languages and databases uninstall (mise manages language runtimes; complex, separate cycle)
- `--force` flag to override `IsInstalledByDevgeta` check
- Interactive multi-select TUI
- `dg uninstall` with no args
- Compound `--only/--skip` flags on uninstall (single positional arg only in v1)
- Uninstalling core terminal deps (autoconf, libffi, readline, etc.) — never in registry

**Scope is locked.** New discoveries go in ROADMAP.md.

---

## 5. Implementation Plan

### File Changes

| Action | File | Description |
|--------|------|-------------|
| Modify | `internal/commands/factory.go` | Add `UninstallPackage`, `UninstallDesktopApp` to `Command` interface |
| Modify | `internal/commands/macos.go` | Implement `UninstallPackage`, `UninstallDesktopApp` |
| Modify | `internal/commands/debian.go` | Implement `UninstallPackage`, `UninstallDesktopApp` |
| Modify | `internal/commands/mock.go` | Add `UninstallDesktopApp` + `UninstalledDesktopApp` field |
| Modify | `internal/config/fromFile.go` | Add `RemoveFromInstalled` |
| Modify | `internal/config/fromFile_test.go` | Test `RemoveFromInstalled` |
| Modify | `internal/apps/registry/registry.go` | Add `AppMeta` struct, `Meta` map, filter helpers (NO package move) |
| Modify | `internal/apps/registry/registry_test.go` | Add `AppMeta` consistency tests |
| Modify | `internal/apps/aerospace/aerospace.go` | Real `Uninstall()` |
| Create/Modify | `internal/apps/aerospace/aerospace_test.go` | Uninstall tests |
| Modify | `internal/apps/alacritty/alacritty.go` | Real `Uninstall()` |
| Modify | `internal/apps/alacritty/alacritty_test.go` | Uninstall tests |
| Modify | `internal/apps/brave/brave.go` | Real `Uninstall()` |
| Create/Modify | `internal/apps/brave/brave_test.go` | Uninstall tests |
| Modify | `internal/apps/claude/claude.go` | Real `Uninstall()` |
| Modify | `internal/apps/claude/claude_test.go` | Uninstall tests |
| Modify | `internal/apps/docker/docker.go` | Real `Uninstall()` |
| Modify | `internal/apps/docker/docker_test.go` | Uninstall tests |
| Modify | `internal/apps/fastfetch/fastfetch.go` | Real `Uninstall()` |
| Create/Modify | `internal/apps/fastfetch/fastfetch_test.go` | Uninstall tests |
| Modify | `internal/apps/flameshot/flameshot.go` | Real `Uninstall()` |
| Modify | `internal/apps/flameshot/flameshot_test.go` | Uninstall tests |
| Modify | `internal/apps/gimp/gimp.go` | Real `Uninstall()` |
| Create/Modify | `internal/apps/gimp/gimp_test.go` | Uninstall tests |
| Modify | `internal/apps/git/git.go` | Real `Uninstall()` |
| Modify | `internal/apps/git/git_test.go` | Uninstall tests (already has tests) |
| Modify | `internal/apps/i3/i3.go` | Real `Uninstall()` |
| Modify | `internal/apps/i3/i3_test.go` | Uninstall tests |
| Modify | `internal/apps/lazydocker/lazydocker.go` | Real `Uninstall()` |
| Modify | `internal/apps/lazydocker/lazydocker_test.go` | Uninstall tests |
| Modify | `internal/apps/lazygit/lazygit.go` | Real `Uninstall()` |
| Modify | `internal/apps/lazygit/lazygit_test.go` | Uninstall tests |
| Modify | `internal/apps/mise/mise.go` | Complete existing partial `Uninstall()` (add binary removal) |
| Modify | `internal/apps/mise/mise_test.go` | Uninstall tests |
| Modify | `internal/apps/neovim/neovim.go` | Real `Uninstall()` |
| Modify | `internal/apps/neovim/neovim_test.go` | Uninstall tests |
| Modify | `internal/apps/opencode/opencode.go` | Real `Uninstall()` |
| Modify | `internal/apps/opencode/opencode_test.go` | Uninstall tests |
| Modify | `internal/apps/raycast/raycast.go` | Real `Uninstall()` |
| Modify | `internal/apps/raycast/raycast_test.go` | Uninstall tests |
| Modify | `internal/apps/tmux/tmux.go` | Real `Uninstall()` |
| Modify | `internal/apps/tmux/tmux_test.go` | Uninstall tests |
| Modify | `internal/apps/ulauncher/ulauncher.go` | Real `Uninstall()` |
| Create/Modify | `internal/apps/ulauncher/ulauncher_test.go` | Uninstall tests |
| Create | `cmd/uninstall.go` | `dg uninstall <app|category>` command; `init()` self-registers |
| Create | `cmd/uninstall_test.go` | Command-level tests |

---

### Step-by-Step

#### Step 1: Extend the `Command` layer with uninstall methods

**`internal/commands/factory.go`** — add to interface:
```go
UninstallPackage(packageName string) error
UninstallDesktopApp(packageName string) error
```

**`internal/commands/macos.go`** — implement:
```go
func (m *MacOSCommand) UninstallPackage(pkg string) error {
    _, _, err := m.Base.ExecCommand(CommandParams{Command: "brew", Args: []string{"uninstall", pkg}})
    return err
}
func (m *MacOSCommand) UninstallDesktopApp(pkg string) error {
    _, _, err := m.Base.ExecCommand(CommandParams{Command: "brew", Args: []string{"uninstall", "--cask", pkg}})
    return err
}
```

**`internal/commands/debian.go`** — implement:
```go
func (d *DebianCommand) UninstallPackage(pkg string) error {
    _, _, err := d.Base.ExecCommand(CommandParams{Command: "apt-get", Args: []string{"remove", "-y", pkg}, IsSudo: true})
    return err
}
func (d *DebianCommand) UninstallDesktopApp(pkg string) error {
    return d.UninstallPackage(pkg) // Debian has no cask concept
}
```

**`internal/commands/mock.go`** — add `UninstallDesktopApp`:
```go
UninstalledDesktopApp string

func (m *MockCommand) UninstallDesktopApp(pkg string) error {
    m.UninstalledDesktopApp = pkg
    return m.UninstallError
}
```
Also add `UninstalledDesktopApp` to `Reset()`.

- Verify: `go build ./internal/commands/`

#### Step 2: Add `RemoveFromInstalled` to `GlobalConfig`

**`internal/config/fromFile.go`**:
```go
func (gc *GlobalConfig) RemoveFromInstalled(itemName, itemType string) {
    slice := gc.getInstalledSlice(itemType)
    if slice == nil {
        return
    }
    result := (*slice)[:0]
    for _, v := range *slice {
        if v != itemName {
            result = append(result, v)
        }
    }
    *slice = result
}
```

**`internal/config/fromFile_test.go`** — table-driven tests:
- Removes item that exists
- No-op when item absent
- Does not affect `already_installed`
- Works for each item type (`terminal_tool`, `desktop_app`, `package`, `database`, etc.)

- Verify: `go test ./internal/config/ -v`

#### Step 3: Add `AppMeta` map to `internal/apps/registry/`

**No package move.** The registry stays at `internal/apps/registry/` to avoid import cycle risk (it imports all app packages). Add the `AppMeta` struct and `Meta` map (see spec in Engineer Context). Add new helpers:
- `IsKnownApp`, `IsKnownCategory`, `KnownCategories`, `AppsByCoordinator`

Add a compile-time test asserting every `Meta` entry with `Coordinator != ""` has a matching factory — catches registry-factory drift.

No import changes needed for `cmd/install.go` or `cmd/configure.go` — they already import `internal/apps/registry`.

- Verify: `go test ./internal/apps/registry/` and `go test ./cmd/ -run TestInstall` and `go test ./cmd/ -run TestConfigure`

#### Step 4: (Cleanup, not prerequisite) Fix `AddToInstalled` tracking in 14 apps' `ForceConfigure`

**Why this is NOT a prerequisite for uninstall:** All apps are already tracked during `Install()` by `MaybeInstallPackage`/`MaybeInstallDesktopApp` in `base.go:324`. This fix only matters for the `dg configure --force <app>` edge case (standalone reconfigure without prior install).

For each of the 14 apps missing the call, add to their `ForceConfigure()` alongside existing gc operations:
```go
gc.AddToInstalled(constants.AppName, "package")   // for terminal apps using MaybeInstallPackage
gc.AddToInstalled(constants.AppName, "desktop_app") // for desktop apps using MaybeInstallDesktopApp
```

Apps needing `"package"`: `fastfetch`, `git`, `i3`, `lazydocker`, `lazygit`, `mise`, `neovim`, `tmux`.
Apps needing `"desktop_app"`: `brave`, `docker`, `flameshot`, `gimp`, `raycast`, `ulauncher`.

The 4 that already have it (alacritty: `"desktop_app"`, aerospace: `"desktop_app"`, claude: `"package"`, opencode: `"package"`) are correct.

- Verify: `go test ./internal/apps/... -v`

#### Step 5: Implement `Uninstall()` for no-config desktop apps (batch)

Apps: `brave`, `docker`, `flameshot`, `gimp`, `raycast`, `ulauncher`

These are the simplest — no config files, no shell features. Pattern:

```go
// brave example
func (b *Brave) Uninstall() error {
    gc := &config.GlobalConfig{}
    if err := gc.Load(); err != nil {
        return fmt.Errorf("failed to load global config: %w", err)
    }
    if err := b.Cmd.UninstallDesktopApp(constants.BraveBrowser); err != nil {
        // Note: add `BraveBrowser = "brave-browser"` to pkg/constants/constants.go
        // to avoid constructing the cask name via fmt.Sprintf (drift risk if constants.Brave changes)
        return fmt.Errorf("failed to uninstall brave: %w", err)
    }
    gc.RemoveFromInstalled(constants.Brave, "desktop_app")
    return gc.Save()
}
```

Notes:
- `raycast`: macOS only — guard with `if !b.Base.IsMac() { return nil }` (requires adding `Base` field to `Raycast` and `Brave` structs)
- `ulauncher`: Linux only — guard with `if b.Base.IsMac() { return nil }`
- `docker`: Just `UninstallDesktopApp(docker)` — Docker manages its own data dirs

Test pattern for each:
```go
func TestBraveUninstall(t *testing.T) {
    mockApp := testutil.NewMockApp()
    app := &Brave{Cmd: mockApp.Cmd}
    err := app.Uninstall()
    assert.NoError(t, err)
    assert.Equal(t, "brave-browser", mockApp.Cmd.(*commands.MockCommand).UninstalledDesktopApp)
    testutil.VerifyNoRealCommands(t, mockApp.Base)
}
```

- Verify: `go test ./internal/apps/brave/ ./internal/apps/docker/ ./internal/apps/flameshot/ ./internal/apps/gimp/ ./internal/apps/raycast/ ./internal/apps/ulauncher/ -v`

#### Step 6: Implement `Uninstall()` for simple package apps with config dirs

Apps: `aerospace`, `alacritty`, `fastfetch`, `git`, `i3`

Pattern (aerospace example):
```go
func (a *Aerospace) Uninstall() error {
    gc := &config.GlobalConfig{}
    if err := gc.Load(); err != nil {
        return fmt.Errorf("failed to load global config: %w", err)
    }
    if err := a.Cmd.UninstallDesktopApp("nikitabobko/tap/aerospace"); err != nil {
        return fmt.Errorf("failed to uninstall aerospace: %w", err)
    }
    if err := os.RemoveAll(paths.Paths.Config.Aerospace); err != nil {
        return fmt.Errorf("failed to remove aerospace config: %w", err)
    }
    gc.RemoveFromInstalled(constants.Aerospace, "desktop_app")
    return gc.Save()
}
```

tmux is special — it removes a file in `$HOME`, not a dir:
```go
func (t *Tmux) Uninstall() error {
    gc := &config.GlobalConfig{}
    if err := gc.Load(); err != nil { ... }
    if err := t.Cmd.UninstallPackage(constants.Tmux); err != nil { ... }
    _ = os.Remove(filepath.Join(paths.Paths.Home.Root, ".tmux.conf")) // best-effort
    gc.DisableShellFeature(constants.Tmux)
    if err := gc.RegenerateShellConfig(); err != nil { ... }
    gc.RemoveFromInstalled(constants.Tmux, "package")
    return gc.Save()
}
```

Test pattern — inject `os.RemoveAll` via a field or just verify the command was called and mock the file operations via `testutil`. The key invariant to test: `UninstallPackage` or `UninstallDesktopApp` was called with the right arg.

- Verify: `go test ./internal/apps/aerospace/ ./internal/apps/alacritty/ ./internal/apps/fastfetch/ ./internal/apps/git/ ./internal/apps/i3/ ./internal/apps/tmux/ -v`

#### Step 7: Implement `Uninstall()` for shell-feature package apps

Apps: `lazydocker`, `lazygit`, `mise`, `opencode`

Pattern (lazydocker example — platform-aware binary removal):
```go
func (ld *LazyDocker) Uninstall() error {
    gc := &config.GlobalConfig{}
    if err := gc.Load(); err != nil { ... }
    if ld.Base.IsMac() {
        if err := ld.Cmd.UninstallPackage("jesseduffield/lazydocker/lazydocker"); err != nil {
            return fmt.Errorf("failed to uninstall lazydocker: %w", err)
        }
    } else {
        if _, _, err := ld.Base.ExecCommand(commands.CommandParams{
            Command: "rm", Args: []string{"-f", "/usr/local/bin/lazydocker"}, IsSudo: true,
        }); err != nil {
            return fmt.Errorf("failed to remove lazydocker binary: %w", err)
        }
    }
    gc.DisableShellFeature(constants.LazyDocker)
    if err := gc.RegenerateShellConfig(); err != nil { ... }
    gc.RemoveFromInstalled(constants.LazyDocker, "package")
    return gc.Save()
}
```

`mise.Uninstall()` already has the gc/shell logic — add the binary removal before it and remove the TODO comment.

`opencode` also needs `os.RemoveAll(paths.Config.OpenCode)` before the gc update.

Test each with both `IsMacResult: true` and `IsMacResult: false` to verify both branches.

- Verify: `go test ./internal/apps/lazydocker/ ./internal/apps/lazygit/ ./internal/apps/mise/ ./internal/apps/opencode/ -v`

#### Step 8: Implement `Uninstall()` for complex apps — `neovim` and `claude`

**neovim** — platform-aware, installs to multiple system paths on Linux:
```go
func (n *Neovim) Uninstall() error {
    gc := &config.GlobalConfig{}
    if err := gc.Load(); err != nil { ... }
    if n.Base.IsMac() {
        if err := n.Cmd.UninstallPackage(constants.Neovim); err != nil { ... }
    } else {
        for _, path := range []struct{ cmd string; args []string }{
            {"rm", []string{"-f", "/usr/local/bin/nvim"}},
            {"rm", []string{"-rf", "/usr/local/lib/nvim"}},
            {"rm", []string{"-rf", "/usr/local/share/nvim"}},
        } {
            if _, _, err := n.Base.ExecCommand(commands.CommandParams{
                Command: path.cmd, Args: path.args, IsSudo: true,
            }); err != nil {
                return fmt.Errorf("failed to remove neovim files: %w", err)
            }
        }
    }
    _ = os.RemoveAll(paths.Paths.Config.Nvim) // best-effort
    gc.DisableShellFeature(constants.Neovim)
    if err := gc.RegenerateShellConfig(); err != nil { ... }
    gc.RemoveFromInstalled(constants.Neovim, "package")
    return gc.Save()
}
```

**claude** — npm uninstall, then remove config dir and disable shell feature:
```go
func (c *Claude) Uninstall() error {
    gc := &config.GlobalConfig{}
    if err := gc.Load(); err != nil { ... }
    if _, _, err := c.Base.ExecCommand(commands.CommandParams{
        Command: "npm", Args: []string{"uninstall", "-g", "@anthropic-ai/claude-code"},
    }); err != nil {
        return fmt.Errorf("failed to uninstall claude: %w", err)
    }
    _ = os.RemoveAll(paths.Paths.Config.Claude)
    gc.DisableShellFeature(constants.Claude)
    if err := gc.RegenerateShellConfig(); err != nil { ... }
    gc.RemoveFromInstalled(constants.Claude, "package")
    return gc.Save()
}
```

Test neovim with both `IsMacResult: true` and `false`. Test claude verifies npm uninstall command was called.

- Verify: `go test ./internal/apps/neovim/ ./internal/apps/claude/ -v`

#### Step 9: Implement `cmd/uninstall.go`

```go
var uninstallCmd = &cobra.Command{
    Use:   "uninstall <app|category>",
    Short: "Uninstall an app or category installed by devgeta",
    Long: `Reverses the install process for an app or category.
Only removes apps that devgeta originally installed. Pre-existing apps are skipped.

Examples:
  dg uninstall git           # uninstall a single app
  dg uninstall terminal      # uninstall all terminal apps devgeta installed
`,
    Args: cobra.ExactArgs(1),
    RunE: runUninstall,
}

func init() { rootCmd.AddCommand(uninstallCmd) }
```

`runUninstall` logic:

```go
// test seam — overridden in tests to inject mocks
// NOTE: named `uninstallGetAppFn` to avoid collision with `getAppFn` in cmd/configure.go
var uninstallGetAppFn = registry.GetApp
```

1. **Block reserved targets first** (before any other validation):
   - `languages` or `databases` → return error: `"dg uninstall languages/databases is not yet supported — manage runtimes via mise"`
   - `devgeta` → return error: `"cannot uninstall devgeta from itself"`
2. Validate arg — `registry.IsKnownCategory` or `registry.IsKnownApp`; if neither, return error listing valid categories + apps
3. Build target list: if category arg, all `registry.Meta` entries where `Coordinator == arg`; if app arg, just that entry
4. Load `gc` once
5. Track `anyFailed bool` and `shellFeatureChanged bool`
6. For each target app:
   a. `itemType := registry.Meta[name].ItemType`
   b. If `!gc.IsInstalledByDevgeta(name, itemType)`: log warning "skipping %s: not installed by devgeta", continue
   c. `app, err := uninstallGetAppFn(name)` — uses the seam, mockable in tests
   d. If `err := app.Uninstall(); err != nil`: log error, set `anyFailed = true`, continue
    e. On success: set `shellFeatureChanged = true` if `registry.Meta[name].HasShellFeature` is true
7. If `shellFeatureChanged`: print `"Run \`source ~/.zshrc\` to apply shell changes."`
   - Note: `RegenerateShellConfig` is called inside each `app.Uninstall()`, not here. The orchestrator only prints the user-facing reminder.
8. If `anyFailed`: return a summary error listing which apps failed

#### Step 10: Tests for `cmd/uninstall.go`

`cmd/uninstall_test.go`:
- `dg uninstall languages` → explicit "not supported" error (checked before validation)
- `dg uninstall devgeta` → "cannot uninstall devgeta from itself" (checked before validation)
- Unknown arg → error listing valid targets
- App not tracked by devgeta → warning logged, exit zero
- Single app uninstall succeeds → no error, `getAppFn` called with correct name
- `app.Uninstall()` returns error → `anyFailed` captured, non-zero exit, other apps continue
- Category uninstall → all apps in category processed, non-tracked ones skipped
- Shell-feature app uninstalled → "source ~/.zshrc" message printed

**Use the `uninstallGetAppFn` seam** — override it in test setup:
```go
func TestUninstallSingleApp(t *testing.T) {
    mockCmd := commands.NewMockCommand()
    uninstallGetAppFn = func(name string) (apps.App, error) {
        return &someapp.SomeApp{Cmd: mockCmd, Base: commands.NewMockBaseCommand()}, nil
    }
    t.Cleanup(func() { uninstallGetAppFn = registry.GetApp }) // restore
    // ... run command
}
```

- Verify: `go test ./cmd/ -run TestUninstall -v`

#### Step 11: Full test suite + lint

```bash
go test ./...
make lint
```

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/commands/     # Command interface + impls
go test ./internal/config/       # RemoveFromInstalled
go test ./internal/apps/registry/ # App registry helpers
go test ./internal/apps/...      # All 18 app Uninstall() implementations
go test ./cmd/                   # CLI command
go test ./...                    # Everything
make lint                        # Format + vet
```

### Manual

1. `dg uninstall --help` → shows `<app|category>` with examples
2. `dg uninstall fakeapp` → error listing valid targets
3. `dg uninstall languages` → "not supported" error
4. `dg uninstall git` (not tracked) → "was not installed by devgeta, skipping"
5. After `dg install --only fastfetch`: run `dg uninstall fastfetch` → binary gone, config dir gone, global_config updated
6. After `dg install --only tmux`: run `dg uninstall tmux` → binary gone, `.tmux.conf` gone, shell feature disabled in global_config, "source ~/.zshrc" message printed
7. `dg uninstall terminal` → processes all terminal apps, skips pre-existing ones, shows summary

### Regression

```bash
go test ./cmd/ -run TestInstall   # install --only/--skip still works
go test ./cmd/ -run TestConfigure # dg configure unaffected
go test ./...                     # nothing else broken
```

---

## 7. Risks & Trade-offs

| Risk | Likelihood | Mitigation |
|------|------------|-----------|
| `Raycast`/`Brave`/`Ulauncher` structs lack `Base` field needed for `IsMac()` | High | Add `Base cmd.BaseCommandExecutor` field in this cycle (same as other apps) |
| `os.RemoveAll` in tests would hit real filesystem | Medium | Inject `removeAllFn func(string) error` on structs that remove dirs, stub in tests |
| macOS package name for `lazydocker`/`lazygit` doesn't match binary name | Low | Verify against `AppToCoordinator` and brew formula names |
| `mise` uninstall removes all managed runtimes | High (user surprise) | Print clear warning: "this will remove the mise binary; your managed runtimes in ~/.local/share/mise remain" |

### Trade-offs Made

- **`os.RemoveAll` injection vs direct call:** For testability, apps that remove config dirs should accept a `removeAllFn` field. Apps that don't need it (brave, gimp, etc.) can call `os.RemoveAll` directly since those code paths aren't in the test surface.
- **Orchestrator vs per-app gc management:** Per-app `Uninstall()` owns its own gc operations (consistent with `ForceConfigure` pattern). The orchestrator only does the `IsInstalledByDevgeta` pre-check.
- **Continue on error:** Category uninstall continues past individual failures and reports them at the end, rather than aborting. Consistent with how `dg install` handles partial failures.

---

## 8. Cross-Model Review Notes

- [x] Domain context clear?
- [x] Step ordering correct? (Command layer → GlobalConfig → Registry metadata → Apps → CLI → Cleanup ForceConfigure)
- [ ] `removeAllFn` injection needed for all config-dir apps, or just the complex ones?
- [x] ~~`AppEntry.HasShellFeature bool` — should this field be added to the registry struct?~~ **Decision: Yes, added as explicit `HasShellFeature bool` field on `AppMeta`.**
- [x] ~~`paths.Config.Nvim` vs `paths.Config.Neovim`~~ **Confirmed: `paths.Config.Nvim` is the actual field name in `paths.go`.**
- [x] ~~Confirm item types in `AppEntry` table match what each app's `ForceConfigure` was using~~ **REVISED: Item types now match actual `MaybeInstall*` tracking (`"package"` for terminal apps, `"desktop_app"` for desktop apps). Previous draft had 9 wrong entries using `"terminal_tool"`.**
- [x] ~~Registry package move~~ **CANCELLED: Stays at `internal/apps/registry/` to avoid import cycle risk.**
- [x] ~~`getAppFn` naming collision~~ **Fixed: renamed to `uninstallGetAppFn` in `cmd/uninstall.go`.**
- [x] ~~14 apps "missing tracking" as prerequisite~~ **REVISED: Demoted to cleanup. Apps are tracked during `Install()` by `MaybeInstall*`.**

**Key invariant:** After a successful `dg uninstall <app>`, running `dg install --only <app>` must reinstall cleanly — global config no longer tracks it as installed, so `MaybeInstallPackage` proceeds normally. Test this end-to-end manually for at least one app (fastfetch is good — simple, no shell feature).

---

## Notes for Implementers

- **Steps 5–8 are parallelizable** once Steps 1–4 are done. Each app is independent.
- **Commit after each step.** Run `/smart-commit` once the step's verify check passes.
- **Check item types carefully** — `claude` and `opencode` use `"package"` not `"terminal_tool"` (see their `ForceConfigure`). Wrong type means `IsInstalledByDevgeta` returns false and uninstall silently skips.
- **`mise` warning is important** — users may not realize mise is the runtime manager for all their language versions.
- **`removeAllFn` injection pattern:** Add `removeAllFn func(string) error` to any struct whose `Uninstall` calls `os.RemoveAll`; default to `os.RemoveAll` in `New()`. Test sets it to a stub. See neovim's `downloadFn` for the established precedent.
