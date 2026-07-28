# Cycle: Per-App Install Filtering (`dg install --only`/`--skip` by App Name)

**Date:** 2026-05-12
**Estimated Duration:** ~3 hours
**Status:** Done

---

## 1. Domain Context

`dg install` currently supports `--only <category>` and `--skip <category>` (categories: `terminal`, `languages`, `databases`, `desktop`). There is no way to target a single app — e.g., install only `neovim`, or skip `git` while installing everything else.

`dg configure` already uses the registry (`internal/apps/registry/`) which has 18 fully-compliant `apps.App` implementations. Every one of these has a `Name()` and `Kind()` method. The install path bypasses the registry entirely and uses hardcoded lists in the tooling coordinators.

This cycle extends `--only` and `--skip` to also accept individual app names from the registry, so they work at two granularities:

- `dg install --only terminal` — category (existing behavior unchanged)
- `dg install --only neovim` — single app by name (new)
- `dg install --skip git --skip lazydocker` — skip specific apps from their category (new)
- `dg install --only terminal --skip neovim` — mixed: category minus one app (new)

This also lays the groundwork for `dg uninstall` — once apps are individually addressable by name in install, the same pattern applies to uninstall.

Related files: [docs/spec.md](../../spec.md), [ROADMAP.md](../../../ROADMAP.md), [docs/guides/app-interface.md](../../guides/app-interface.md)

---

## 2. Engineer Context

### Two tiers of "apps" — only one tier is targetable

| Location | Examples | `Name()` / `Kind()` | In registry |
|---|---|---|---|
| `internal/apps/{appname}/` | neovim, tmux, lazygit, alacritty… | ✅ Full 10-method `App` interface | ✅ Yes (18 apps) |
| `internal/tooling/terminal/core/` | autoconf, bison, ncurses… | ❌ 8 methods, no Name/Kind | ❌ No |
| `internal/tooling/terminal/dev_tools/` | bat, fzf, zoxide… | ❌ 8 methods, no Name/Kind | ❌ No |

**Only the 18 registry apps are individually targetable.** Core libs (16) and dev tools (13) are always installed as a block whenever the terminal category runs without an app filter.

### Registry apps by category

**KindTerminal (10):** alacritty, claude, fastfetch, git, lazydocker, lazygit, mise, neovim, opencode, tmux

**KindDesktop (8):** aerospace, brave, docker, flameshot, gimp, i3, raycast, ulauncher

**KindMeta (1):** devgeta ← never included in install

Note: `alacritty` is `KindTerminal` but is currently installed by the desktop coordinator. It stays there.

### Relevant files

- `internal/apps/contract.go` — `App` interface and `AppKind` enum
- `internal/apps/registry/registry.go` — `GetApp()`, `Names()` functions
- `cmd/install.go` — Flag definitions, `onlySet`/`skipSet` parsing, `shouldInstall()`
- `internal/tooling/terminal/terminal.go` — `InstallTerminalApps()`, `InstallDevTools()`, `InstallCoreLibs()`
- `internal/tooling/desktop/desktop.go` — `InstallAndConfigure()`, `InstallDesktopAppsWithoutConfiguration()`

### Testing patterns

Always use `testutil.MockApp`; never execute real commands. See [docs/guides/testing-patterns.md](../../guides/testing-patterns.md).

```bash
go test ./cmd/
go test ./internal/tooling/terminal/
go test ./internal/tooling/desktop/
go test ./internal/apps/registry/
go test ./...
make lint
```

---

## 3. Objective

Extend `--only` and `--skip` flags on `dg install` to accept both category names and individual registry app names, with per-app filtering applied inside the terminal and desktop coordinators — leaving languages, databases, core libs, and dev_tools behavior unchanged.

---

## 4. Scope Boundary

### In Scope

- [x] `registry.go`: Add `GetAppsByKind(kind apps.AppKind) []string` helper
- [x] `cmd/install.go`: Split `--only`/`--skip` values into category set and app set; validate both; derive which categories should run based on app sets; pass app filter to coordinators
- [x] `internal/tooling/terminal/terminal.go`: `InstallTerminalApps()` accepts `appFilter map[string]bool`; skips apps not in the filter when filter is non-empty; `InstallDevTools()` and `InstallCoreLibs()` are skipped entirely when an app filter is active
- [x] `internal/tooling/desktop/desktop.go`: `InstallAndConfigure()` and `InstallDesktopAppsWithoutConfiguration()` accept `appFilter map[string]bool` and skip apps not in filter
- [x] Tests for all changes (cmd, registry, terminal coordinator, desktop coordinator)
- [x] `docs/spec.md`: Document per-app targeting under `dg install`

### Explicitly Out of Scope

- Adding languages or databases to the registry (separate cycle)
- Per-app targeting for core libs (`autoconf`, `bat`, etc.) — they have no Name/Kind
- `dg uninstall` command — this cycle only handles install
- Changing the languages or databases TUI selection flow
- Interactive fzf-style app selection

---

## 5. Implementation Plan

### Design: Flag parsing logic

**Known values:**
```
knownCategories = {"terminal", "languages", "databases", "desktop"}
knownAppNames   = registry.Names()  // 18 sorted app names
```

**Parsing (in `cmd/install.go` `run()`):**
```
for each value in --only / --skip:
    if value in knownCategories → onlyCategorySet / skipCategorySet
    else if value in knownAppNames → onlyAppSet / skipAppSet
    else → return error "unknown: %q\n\nValid categories: ...\nValid apps: ..."
```

**Category execution logic:**

A category should run if:
1. No `--only` flags at all → run everything
2. `--only terminal` is set → run terminal with no app filter (all 38+ packages)
3. `--only neovim` is set → neovim's Kind() == KindTerminal → run terminal with `appFilter = {"neovim"}`
4. `--only terminal --skip neovim` → run terminal, pass `skipAppSet = {"neovim"}` to coordinator
5. `--skip neovim` → run all categories, terminal skips neovim in its app list

Helper: `shouldRunCategory(category string, onlyCategorySet, skipCategorySet map[string]bool, appsBelongingToCategory []string, onlyAppSet map[string]bool) bool`

**App filter for coordinators:**
- `appFilter` is non-nil and non-empty only when `onlyAppSet` contains apps for that category
- When `appFilter` is non-nil: only install apps in the filter; skip `InstallDevTools()` and `InstallCoreLibs()` entirely (user asked for specific apps, not full setup)
- When `appFilter` is nil: existing behavior (install everything in the category)
- `skipAppSet` applies in both cases: filtered out before calling coordinator

### File Changes

| Action | File | Description |
|--------|------|-------------|
| Modify | `internal/apps/registry/registry.go` | Add `GetAppsByKind(kind apps.AppKind) []string` |
| Modify | `internal/apps/registry/registry_test.go` | Test `GetAppsByKind` (or create if missing) |
| Modify | `cmd/install.go` | Split flags, validate, compute per-category app filters, pass to coordinators |
| Modify | `cmd/install_test.go` | Tests for new flag parsing, validation, app-level filtering |
| Modify | `internal/tooling/terminal/terminal.go` | `InstallTerminalApps(summary, appFilter)`, skip devtools/corelibs when filter active |
| Modify | `internal/tooling/terminal/terminal_test.go` | Tests for app filter behavior |
| Modify | `internal/tooling/desktop/desktop.go` | `InstallAndConfigure(appFilter)`, `InstallDesktopAppsWithoutConfiguration(appFilter)` |
| Modify | `internal/tooling/desktop/desktop_test.go` | Tests for app filter behavior |
| Modify | `docs/spec.md` | Document per-app targeting under `dg install` |

### Step-by-Step

#### Step 1: Add `GetAppsByKind()` to registry

- In `internal/apps/registry/registry.go`, add:
  ```go
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
  ```
- Add test in `internal/apps/registry/registry_test.go` (or create it):
  - `TestGetAppsByKind_Terminal` — returns expected terminal apps
  - `TestGetAppsByKind_Desktop` — returns expected desktop apps
  - `TestGetAppsByKind_NoMeta` — KindMeta not in terminal or desktop results
- Expected outcome: function compiles and tests pass
- Verify: `go test ./internal/apps/registry/`

#### Step 2: Update terminal coordinator to accept app filter

- Change signature: `InstallTerminalApps(summary *InstallationSummary, appFilter map[string]bool)`
- Logic: when `appFilter` is non-nil and non-empty:
  - Wrap each app install in: `if len(appFilter) == 0 || appFilter[constants.X]`
  - After the terminal apps loop, skip `InstallDevTools()` and `InstallCoreLibs()` entirely
- When `appFilter` is nil or empty: existing behavior unchanged
- Also handle `skipAppSet` if needed — caller removes skipped apps from filter before calling
- Update `InstallAndConfigure()` call site to pass `nil` (existing behavior)
- Expected outcome: compiles, existing tests still pass
- Verify: `go test ./internal/tooling/terminal/`

#### Step 3: Tests for terminal coordinator filter

- In `terminal_test.go` (create if missing):
  - `TestInstallTerminalApps_NoFilter` — all apps attempted
  - `TestInstallTerminalApps_WithFilter_SingleApp` — only filtered app attempted, dev_tools/core_libs skipped
  - `TestInstallTerminalApps_WithFilter_MultipleApps` — multiple apps, rest skipped
  - Use `testutil.NewMockApp()`, verify `testutil.VerifyNoRealCommands`
- Verify: `go test ./internal/tooling/terminal/ -v`

#### Step 4: Update desktop coordinator to accept app filter

- Change `InstallAndConfigure(appFilter map[string]bool)` signature
- `InstallDesktopAppsWithoutConfiguration(appFilter map[string]bool)` — wrap each app in filter check
- Platform-specific apps (aerospace/i3, raycast/ulauncher) also respect filter
- Fonts installation: only runs when `appFilter` is nil/empty (not targeted individually)
- Update any existing call sites to pass `nil`
- Expected outcome: compiles, existing tests pass
- Verify: `go test ./internal/tooling/desktop/`

#### Step 5: Tests for desktop coordinator filter

- `TestInstallDesktopAppsWithoutConfiguration_WithFilter` — only filtered apps installed
- `TestInstallAerospace_Skipped_WhenNotInFilter` — platform-specific app skipped when filter excludes it
- Verify: `go test ./internal/tooling/desktop/ -v`

#### Step 6: Update `cmd/install.go` flag parsing

This is the core integration step.

- Add `var knownCategories = []string{"terminal", "languages", "databases", "desktop"}`
- Add helper `isKnownCategory(s string) bool` and `isKnownApp(s string) bool` (uses `registry.Names()`)
- Modify `run()`:
  1. Build `onlyCategorySet`, `onlyAppSet`, `skipCategorySet`, `skipAppSet` from `--only`/`--skip`
  2. Validate: unknown value → return descriptive error listing valid categories and valid app names
  3. For `onlyAppSet`: group by Kind, compute `terminalAppFilter` and `desktopAppFilter`
  4. Compute `shouldRunTerminal`:
     - False if `skipCategorySet["terminal"]`
     - True if `onlyCategorySet["terminal"]`
     - True if any app in `onlyAppSet` has `KindTerminal`
     - True if both sets are empty
  5. Same logic for desktop
  6. Pass computed filters: `installTerminalTools(summary, onlyCategorySet, skipCategorySet, terminalAppFilter)` etc.
- Update `shouldInstall()` signature or replace with per-category logic
- Expected outcome: compiles, existing category-only tests pass
- Verify: `go build ./cmd/` then `./devgeta install --help`

#### Step 7: Tests for `cmd/install.go` flag parsing

- `TestInstallFlags_CategoryOnly` — `--only terminal` works as before
- `TestInstallFlags_AppOnly` — `--only neovim` derives terminal category
- `TestInstallFlags_AppSkip` — `--skip git` runs terminal minus git
- `TestInstallFlags_UnknownValue` — `--only bogus` returns error with hint
- `TestInstallFlags_MixedCategoryAndApp` — `--only terminal --skip neovim` works
- Verify: `go test ./cmd/ -v`

#### Step 8: Update `docs/spec.md`

- Under `dg install`, add a section documenting per-app targeting
- Include examples: `--only neovim`, `--skip git`, `--only terminal --skip lazygit`
- Note which apps are individually targetable (registry names) vs. not (core libs, dev tools, languages, databases)
- Verify: read the section for clarity

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/apps/registry/
go test ./internal/tooling/terminal/
go test ./internal/tooling/desktop/
go test ./cmd/
go test ./...
make lint
```

### Manual (smoke test — build only, don't install)

```bash
make build
./devgeta install --help           # Check flag descriptions
./devgeta install --only bogus     # Should error with valid names listed
./devgeta install --only neovim --help   # Verify accepted
./devgeta install --only terminal --skip lazygit --help  # Verify accepted
```

### Regression Check

- `./devgeta install --only terminal` — existing behavior unchanged (no app filter)
- `./devgeta install --skip databases` — existing behavior unchanged
- `./devgeta install --help` — all flags still present and described
- `./devgeta configure neovim` — unrelated command unaffected

---

## 7. Risks & Trade-offs

| Risk | Likelihood | Mitigation |
|---|---|---|
| `alacritty` is `KindTerminal` but installed by desktop coordinator | Med | Derive category membership from which coordinator installs the app, not from `Kind()`. Or document that `--only alacritty` maps to desktop (since desktop installs it). Clarify in step 6. |
| App filter skips dev_tools / core_libs when user does `--only terminal --only neovim` | Low | Rule: category flag with no app filter = full install; any app filter = registry apps only. Document clearly. |
| `registry.GetAppsByKind` instantiates all apps just to read `Kind()` | Low | Acceptable for a CLI that runs once. If it becomes a concern, store kind alongside the factory. |
| Missing test coverage for mixed flags | Med | Step 7 explicitly covers mixed cases. |

### Trade-offs Made

- **App filter skips core libs/dev_tools:** When `--only neovim` is specified, devtools and core libs are not installed. This is the right behavior — the user asked for a single app, not a full setup.
- **`--only` for both categories and apps on one flag:** Avoids two flags (`--only-app` vs `--only`), but requires validation to tell them apart. Validation error messages will list valid categories and valid app names to guide users.
- **alacritty lives in desktop coordinator:** `alacritty.Kind() == KindTerminal` but it's desktop-installed. We'll document that for per-app targeting purposes, app names map to the coordinator that installs them, not strictly to their `Kind()`. A `kindToCoordinator` mapping in `cmd/install.go` resolves this cleanly.

---

## 8. Cross-Model Review Notes

- [x] Domain context clear?
- [x] Engineer context sufficient?
- [x] Objective unambiguous?
- [x] Scope is actually locked?
- [x] Steps are actionable?
- [x] Verification is executable?
- [x] Risks are realistic?

**Reviewer notes:** Watch the `alacritty` edge case (KindTerminal but desktop-installed). Step 6 must handle this explicitly via a `kindToCoordinator` mapping rather than relying purely on `Kind()`.
