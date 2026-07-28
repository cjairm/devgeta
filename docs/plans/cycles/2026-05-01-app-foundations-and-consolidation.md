# Cycle: App Foundations & Pattern Consolidation

**Date:** 2026-05-01
**Estimated Duration:** ~12 hours
**Status:** Done

---

## 1. Domain Context

Before we expose user-facing commands like `dg configure`, `dg uninstall`, `dg update`, and `dg reinstall`, the underlying app layer needs to be solid and uniform. A multi-pass audit of `internal/apps/*` revealed:

1. **A real bug:** `ForceInstall` is broken in 13+ apps. The pattern `Uninstall() → Install()` always fails because `Uninstall()` returns a "not supported" error in every app that doesn't actually support uninstall. Tests confirm this — `TestForceInstall` is **commented out** in `aerospace_test.go`, `alacritty_test.go`, and `docker_test.go`. Only `claude.go` does it correctly (just calls `Install()` directly).
2. **No formal contract.** There is no Go `interface` that all apps implement. Every app exposes `Install`, `ForceInstall`, `SoftInstall`, `ForceConfigure`, `SoftConfigure`, `ExecuteCommand`, `Uninstall`, `Update` by convention only. This makes a future `dg uninstall` / `dg configure` dispatcher impossible to write generically — you cannot iterate over a `[]App` because there is no `App` type.
3. **Inconsistent unsupported-operation errors.** 11 apps return one of "uninstall not supported for X", "uninstall not implemented for X", "X uninstall not supported through devgeta", or longer free-form strings. A caller wanting to gracefully skip "this app doesn't support uninstall" cannot use `errors.Is` — they would have to string-match.
4. **Interface deviations.** `Fonts.Install(fontName string)` and `Fonts.ForceInstall(fontName string)` accept a parameter; every other app's `Install()` takes none. `Aerospace.ExecuteCommand()` takes no args; everyone else takes `...string`. These break the uniform shape future commands need.
5. **Two parallel constructor patterns.** Apps either embed only `Cmd` (Brave, Flameshot, Gimp, Raycast, I3, Ulauncher, Devgeta) or both `Cmd` + `Base` (everyone else). The split is intentional — desktop GUI apps don't need command execution — but it isn't documented anywhere, and the same `New()` boilerplate is hand-rolled in 19 places.
6. **Test patterns split.** 9 test files use `commands.NewMockCommand()` directly; 5 use the documented `testutil.NewMockApp()`. Two files (`git_test.go`, `alacritty_test.go`) reimplement path isolation manually instead of calling `testutil.SetupIsolatedPaths()`.

This cycle fixes the bug, defines the contract, standardizes the errors, and migrates every app and test to the new shape — so the next cycle can build `dg configure` / `dg uninstall` against a stable, iterable surface.

**References:**

- Audit findings (this conversation, 2026-05-01)
- [docs/spec.md](../../spec.md)
- [docs/guides/testing-patterns.md](../../guides/testing-patterns.md)
- [CLAUDE.md](../../../CLAUDE.md) — Section 6 (Implementation behavior), Section 11 (Architecture Patterns)
- Prior cycle: [2026-04-28-claude-code-installer.md](2026-04-28-claude-code-installer.md) — Claude is the reference implementation for `ForceInstall`

---

## 2. Engineer Context

**Relevant files:**

| File                                        | Why it matters                                                                     |
| ------------------------------------------- | ---------------------------------------------------------------------------------- |
| `internal/apps/contract.go` _(new)_         | Formal `App` interface every app must satisfy                                      |
| `internal/apps/errors.go` _(new)_           | Sentinel errors (`ErrUninstallNotSupported`, etc.)                                 |
| `internal/apps/baseapp/baseapp.go` _(new)_  | Shared helpers: `Reinstall()`, common no-op stubs                                  |
| `internal/apps/{appname}/{appname}.go`      | All 19 apps need refactoring (see App Inventory in §5)                             |
| `internal/apps/{appname}/{appname}_test.go` | All 19 test files need migration to `testutil.NewMockApp()`                        |
| `internal/testutil/testutil.go`             | Already has `NewMockApp`, `SetupIsolatedPaths`, `SetupCompleteTest` — use these    |
| `docs/guides/testing-patterns.md`           | Update to mandate `testutil.NewMockApp` (no more `commands.NewMockCommand` direct) |
| `docs/guides/app-interface.md` _(new)_      | Document the `App` interface, `AppKind`, sentinel errors, and constructor patterns |
| `CLAUDE.md`                                 | Update Section 6 ("App interface pattern") to point to the formal contract         |

**Patterns to follow:**

- `internal/apps/claude/claude.go` is the **reference implementation** for `ForceInstall` (`return c.Install()`), config deployment with `gc.Shell.X = true` + `RegenerateShellConfig()`, and sentinel-style error returns.
- `internal/apps/opencode/opencode.go` is the reference for `ConfigureOptions` variadic pattern.
- `internal/testutil/testutil.go` already provides everything needed — no new test infra required.

**Key types in the existing codebase:**

- `cmd.Command` (interface) — package install / OS-level helpers
- `cmd.BaseCommandExecutor` (interface) — generic `ExecCommand(CommandParams)`
- `commands.MockCommand`, `commands.MockBaseCommand` — test mocks
- `testutil.MockApp{Cmd, Base}`, `testutil.TestConfig` — wrappers tests should use

**Testing checklist (from CLAUDE.md §6):**

- [ ] All public functionality has tests
- [ ] Use `testutil.NewMockApp()` for command mocking
- [ ] Verify no real commands executed: `testutil.VerifyNoRealCommands(t, mockApp.Base)`
- [ ] `func init() { testutil.InitLogger() }` at top of test file

**Commands to run tests:**

```bash
go test ./internal/apps/...        # all apps
go test ./internal/tooling/...     # category coordinators (regression)
go test ./...                      # full suite
make lint
```

---

## 3. Objective

Define a formal `App` interface in `internal/apps/`, introduce sentinel errors for unsupported operations, fix the broken `ForceInstall` pattern across all apps, and migrate every test file to the documented `testutil` helpers — leaving the app layer with one uniform contract, one set of error sentinels, and one testing pattern, so `dg configure` / `dg uninstall` / `dg update` can be built against a consistent surface in the next cycle.

---

## 4. Scope Boundary

### In Scope

- [ ] Define `App` interface in new `internal/apps/contract.go`
- [ ] Define sentinel errors in new `internal/apps/errors.go`: `ErrUninstallNotSupported`, `ErrUpdateNotSupported`, `ErrConfigureNotSupported`, `ErrExecuteNotSupported`
- [ ] Define `AppKind` enum (`KindTerminal`, `KindDesktop`, `KindLanguage`, `KindDatabase`, `KindFont`, `KindMeta`) and a `Kind() AppKind` method on every app
- [ ] Add a `Name() string` method to every app returning the canonical constant name (used by future `dg uninstall <name>`)
- [ ] Create `internal/apps/baseapp/baseapp.go` with shared helpers:
  - `Reinstall(install, uninstall func() error) error` — handles "uninstall first if supported, otherwise just install" correctly using `errors.Is(err, ErrUninstallNotSupported)`
  - Optional: `NoOpConfigure()`, `NoOpExecuteCommand()` for desktop apps to delegate to
- [ ] Replace every free-form unsupported-operation `fmt.Errorf` with the matching sentinel (wrapped if extra context is useful: `fmt.Errorf("claude: %w — run: npm uninstall ...", apps.ErrUninstallNotSupported)`)
- [ ] **Fix the `ForceInstall` bug**: every app's `ForceInstall` now goes through `baseapp.Reinstall(...)` (or, for apps where uninstall genuinely is supported, keeps the old behavior — but verified by tests, not commented-out)
- [ ] Normalize `Fonts` interface — extract font-name-aware methods into a separate `FontInstaller` interface; the core `App` methods take no parameters. (Fonts is the only multi-instance app — handle it explicitly rather than warping the contract for one outlier.)
- [ ] Normalize `Aerospace.ExecuteCommand()` signature to `(...string)` (currently takes nothing)
- [ ] Add `var _ apps.App = (*X)(nil)` interface assertion to every app file (compile-time enforcement)
- [ ] Migrate all 9 test files using `commands.NewMockCommand()` directly to `testutil.NewMockApp()`:
  - `git_test.go`, `aerospace_test.go`, `fastfetch_test.go`, `alacritty_test.go`, `mise_test.go`, `neovim_test.go`, `tmux_test.go`, `lazygit_test.go`, `lazydocker_test.go`
- [ ] Replace manual path overrides with `testutil.SetupIsolatedPaths()` / `testutil.SetupCompleteTest()`:
  - `git_test.go` (lines 86-90, 143-148)
  - any other instances surfaced during the migration
- [ ] Re-enable commented-out `TestForceInstall` cases in `aerospace_test.go`, `alacritty_test.go`, `docker_test.go` (and any others surfaced) using the fixed `Reinstall` helper and proper mocking
- [ ] Update `docs/guides/testing-patterns.md`: mandate `testutil.NewMockApp` and `testutil.SetupCompleteTest`; document the `ConfigureOptions` variadic pattern; add a section on the new sentinel errors and how tests should assert with `errors.Is`
- [ ] Create `docs/guides/app-interface.md` documenting the `App` interface, `AppKind`, sentinel errors, the two constructor patterns (with vs. without `Base`), and when to use each
- [ ] Update `CLAUDE.md` §6 ("App interface pattern") to point to the new guide and the formal contract
- [ ] Full `go test ./...` and `make lint` pass

### Explicitly Out of Scope

- Implementing `dg configure`, `dg uninstall`, `dg update`, `dg reinstall` user-facing commands — that's the **next cycle**, which this one unblocks
- Cross-platform installation strategy refactoring (`internal/commands/debian_strategies.go`) — the strategy pattern is already clean; no churn there
- Restructuring `internal/tooling/{terminal,desktop,languages,databases}/` — registration sites only need to import the new sentinels if they want to handle them; otherwise unchanged
- Changing the `cmd.Command` / `cmd.BaseCommandExecutor` interfaces or their mocks
- Adding new apps or new install methods
- Renaming the `internal/apps/devgeta/` self-installer module
- Changing config file formats or migration logic in `internal/config/`
- Adding any new commands to `cmd/`

**Scope is locked.** If something here turns out to be undoable without a scope change, stop and document the issue rather than expand the cycle. Discoveries that warrant later work go in `ROADMAP.md` or a new cycle doc.

---

## 5. Implementation Plan

### App Inventory

To keep the migration tractable, here is the full set of apps (19 total), pre-classified:

| App        | Path                        | Kind         | Has `Base`?  | Currently broken `ForceInstall`? | Test file uses `testutil.NewMockApp`? |
| ---------- | --------------------------- | ------------ | ------------ | -------------------------------- | ------------------------------------- |
| aerospace  | `internal/apps/aerospace/`  | Desktop      | yes          | yes                              | no                                    |
| alacritty  | `internal/apps/alacritty/`  | Terminal     | yes          | yes                              | no (mixed)                            |
| brave      | `internal/apps/brave/`      | Desktop      | no           | yes                              | yes                                   |
| claude     | `internal/apps/claude/`     | Terminal     | yes          | **no** (reference)               | yes                                   |
| devgeta    | `internal/apps/devgeta/`    | Meta         | yes (custom) | yes                              | yes                                   |
| docker     | `internal/apps/docker/`     | Desktop      | yes          | yes                              | yes                                   |
| fastfetch  | `internal/apps/fastfetch/`  | Terminal     | yes          | yes                              | no                                    |
| flameshot  | `internal/apps/flameshot/`  | Desktop      | no           | yes                              | yes                                   |
| fonts      | `internal/apps/fonts/`      | Font (split) | yes          | n/a (param signature)            | n/a                                   |
| gimp       | `internal/apps/gimp/`       | Desktop      | no           | yes                              | yes                                   |
| git        | `internal/apps/git/`        | Terminal     | yes          | yes                              | no                                    |
| i3         | `internal/apps/i3/`         | Desktop      | no           | yes                              | (check)                               |
| lazydocker | `internal/apps/lazydocker/` | Terminal     | yes          | yes                              | no                                    |
| lazygit    | `internal/apps/lazygit/`    | Terminal     | yes          | yes                              | no                                    |
| mise       | `internal/apps/mise/`       | Terminal     | yes          | (uses real Uninstall — check)    | no                                    |
| neovim     | `internal/apps/neovim/`     | Terminal     | yes          | yes                              | no                                    |
| opencode   | `internal/apps/opencode/`   | Terminal     | yes          | yes                              | yes                                   |
| raycast    | `internal/apps/raycast/`    | Desktop      | no           | yes                              | yes                                   |
| tmux       | `internal/apps/tmux/`       | Terminal     | yes          | yes                              | no                                    |
| ulauncher  | `internal/apps/ulauncher/`  | Desktop      | no           | yes                              | (check)                               |

### File Changes

| Action | File Path                                | Description                                                                       |
| ------ | ---------------------------------------- | --------------------------------------------------------------------------------- |
| Create | `internal/apps/contract.go`              | `App` interface, `AppKind` type, kind constants                                   |
| Create | `internal/apps/errors.go`                | Sentinel errors                                                                   |
| Create | `internal/apps/baseapp/baseapp.go`       | `Reinstall` helper + no-op stubs                                                  |
| Modify | `internal/apps/{19 apps}/{name}.go`      | Adopt sentinels, fix `ForceInstall`, add `Name()` + `Kind()`, interface assertion |
| Modify | `internal/apps/{19 apps}/{name}_test.go` | Migrate to `testutil.NewMockApp` + `SetupIsolatedPaths`/`SetupCompleteTest`       |
| Modify | `internal/apps/fonts/fonts.go`           | Split: `App` core (no font name) + `FontInstaller` interface (font-aware)         |
| Create | `docs/guides/app-interface.md`           | Contract + AppKind + sentinels + constructors                                     |
| Modify | `docs/guides/testing-patterns.md`        | Mandate testutil patterns; add sentinel-assertion section                         |
| Modify | `docs/guides/README.md`                  | Link the new guide                                                                |
| Modify | `CLAUDE.md`                              | Update §6 to point at new contract + guide                                        |

### Step-by-Step

Each step is a commit boundary. Run the listed verify command before moving on; if it fails, stop and fix before proceeding.

#### Step 1 — Define the contract and sentinels

Create `internal/apps/contract.go`:

```go
package apps

type AppKind int

const (
    KindUnknown AppKind = iota
    KindTerminal
    KindDesktop
    KindLanguage
    KindDatabase
    KindFont
    KindMeta // devgeta itself
)

// App is the contract every app module satisfies (with the exception of Fonts,
// which adds parameterized methods via FontInstaller).
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

Create `internal/apps/errors.go`:

```go
package apps

import "errors"

var (
    ErrUninstallNotSupported  = errors.New("uninstall not supported")
    ErrUpdateNotSupported     = errors.New("update not supported")
    ErrConfigureNotSupported  = errors.New("configure not supported")
    ErrExecuteNotSupported    = errors.New("execute not supported")
)
```

Verify: `go build ./internal/apps/`

#### Step 2 — Build the `baseapp` helper

Create `internal/apps/baseapp/baseapp.go`:

```go
package baseapp

import (
    "errors"

    "github.com/cjairm/devgeta/internal/apps"
)

// Reinstall implements the "force reinstall" flow correctly:
// if uninstall is supported, run it then install; if not, just install.
// This replaces the previously-broken pattern that failed whenever
// Uninstall returned ErrUninstallNotSupported.
func Reinstall(install, uninstall func() error) error {
    if err := uninstall(); err != nil && !errors.Is(err, apps.ErrUninstallNotSupported) {
        return err
    }
    return install()
}
```

Add a test file `baseapp_test.go` covering: uninstall succeeds; uninstall returns sentinel (treated as success); uninstall returns other error (propagated); install error (propagated).

Verify: `go test ./internal/apps/baseapp/`

#### Step 3 — Refactor Claude (the reference) to the new contract

Claude already does `ForceInstall` correctly, but it doesn't yet implement `Name()` / `Kind()` / use sentinels. Update it:

- Add `Name() string { return constants.Claude }`
- Add `Kind() apps.AppKind { return apps.KindTerminal }`
- `Uninstall()`: `return fmt.Errorf("%w — run: npm uninstall -g @anthropic-ai/claude-code", apps.ErrUninstallNotSupported)`
- `Update()`: `return fmt.Errorf("%w — re-run: curl -fsSL https://claude.ai/install.sh | bash", apps.ErrUpdateNotSupported)`
- Add interface assertion: `var _ apps.App = (*Claude)(nil)`
- Migrate `ForceInstall` to use `baseapp.Reinstall(c.Install, c.Uninstall)` for consistency with the rest of the fleet (even though `Install()` directly was correct — uniform shape matters more here than saving one allocation)

Update `claude_test.go`:

- Add a test asserting `errors.Is(c.Uninstall(), apps.ErrUninstallNotSupported)`
- Confirm `TestForceInstall` still passes after switching to `baseapp.Reinstall`

Verify: `go test ./internal/apps/claude/ -v`

#### Step 4 — Migrate desktop apps (no `Base`)

Apps: brave, flameshot, gimp, raycast, i3, ulauncher.

For each:

1. Add `Name()` returning the constant.
2. Add `Kind() apps.AppKind { return apps.KindDesktop }`.
3. Replace every "not supported"/"not implemented" `fmt.Errorf` with the matching sentinel (wrapped with extra context where it's already useful).
4. Replace `ForceInstall` body with `return baseapp.Reinstall(b.Install, b.Uninstall)` (variable name per app).
5. Add `var _ apps.App = (*Brave)(nil)` (etc.) at the bottom of each file.
6. In the test file, **migrate to `testutil.NewMockApp()` everywhere** if not already (brave/flameshot/gimp/raycast already do; check i3 and ulauncher).
7. Re-enable any commented-out `TestForceInstall` and add an assertion: `assert errors.Is(uninstallErr, apps.ErrUninstallNotSupported)` in the dedicated `TestUninstall`.

Verify after each app: `go test ./internal/apps/{appname}/ -v`
Verify all desktop apps: `go test ./internal/apps/{brave,flameshot,gimp,raycast,i3,ulauncher}/`

#### Step 5 — Migrate terminal apps

Apps: aerospace, alacritty, fastfetch, git, lazygit, lazydocker, mise, neovim, opencode, tmux.

Same checklist as Step 4, plus:

- `Kind() apps.AppKind { return apps.KindTerminal }` (or `KindDesktop` for aerospace, which is a window manager — pick the correct kind per app)
- `Aerospace.ExecuteCommand()` → change signature to `ExecuteCommand(args ...string) error` and update the (currently empty) body. Update any callers (likely none).
- For test files using `commands.NewMockCommand()` directly: switch to `testutil.NewMockApp()` and update assertions.
- For tests with manual path overrides (`git_test.go`, `alacritty_test.go`): replace with `testutil.SetupIsolatedPaths()` or `testutil.SetupCompleteTest()` as appropriate (use `SetupCompleteTest` if templates are needed; `SetupIsolatedPaths` otherwise).
- Verify `mise.go`'s `ForceInstall` carefully — its uninstall flow may actually work; if so, leave the direct call but still route through `baseapp.Reinstall` for shape consistency.

Verify after each app. Then: `go test ./internal/apps/...`

#### Step 6 — Handle Fonts as a special case

Fonts is the only app where install/uninstall accept a `fontName string` parameter. Split it cleanly:

```go
// in internal/apps/contract.go
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

Update `fonts.go`:

- Rename `Install` → `InstallFont`, `ForceInstall` → `ForceInstallFont`, `SoftInstall` → `SoftInstallFont`, `Uninstall` → `UninstallFont`.
- Drop the parameterless `App` interface methods that don't apply (`Install()`, `ForceInstall()` etc.) — Fonts intentionally does **not** satisfy `App`; it satisfies `FontInstaller`.
- Add `var _ apps.FontInstaller = (*Fonts)(nil)` assertion.
- `Kind() apps.AppKind { return apps.KindFont }`
- Sentinel errors for unsupported ops.

Update callers (`internal/tooling/desktop/` likely) to use the new method names.

Verify: `go test ./internal/apps/fonts/` and `go test ./internal/tooling/...`

#### Step 7 — Refactor Devgeta self-installer

Devgeta is the meta-app. Apply the same treatment:

- `Name()`, `Kind() = KindMeta`, sentinel errors, interface assertion.
- It has a richer `New()` with embedded extractor — leave the custom fields alone, just add the new methods.
- If its `ForceInstall` actually does support uninstall (uninstalling devgeta itself), confirm Reinstall correctly dispatches.

Verify: `go test ./internal/apps/devgeta/ -v`

#### Step 8 — Re-enable commented-out tests

Files with `// func TestForceInstall(t *testing.T) {` — uncomment, update to use `testutil.NewMockApp()` and the now-correct `baseapp.Reinstall` semantics, run them.

Audit pass:

```bash
grep -rn "// func Test" internal/apps/
```

Verify: `go test ./internal/apps/... -v`

#### Step 9 — Update documentation

Create `docs/guides/app-interface.md`:

- The `App` interface with method docs
- `AppKind` enum
- Sentinel errors and the `errors.Is` pattern
- The two constructor patterns (with/without `Base`) — when each applies
- The `ConfigureOptions` variadic pattern (used by Alacritty, Neovim, OpenCode)
- The `FontInstaller` outlier
- Reference: `internal/apps/claude/claude.go` as the canonical example

Update `docs/guides/testing-patterns.md`:

- Replace any guidance that allows `commands.NewMockCommand()` directly with a hard requirement to use `testutil.NewMockApp()`.
- Add a "Asserting unsupported operations" section showing `errors.Is(err, apps.ErrUninstallNotSupported)`.
- Document `testutil.SetupCompleteTest` for template-bearing tests; `SetupIsolatedPaths` for simple isolation.

Update `docs/guides/README.md` to link the new app-interface guide.

Update `CLAUDE.md` §6 ("App interface pattern"):

- Replace the prose list of methods with a link to `docs/guides/app-interface.md` and `internal/apps/contract.go`.
- Mention the sentinel errors and `baseapp.Reinstall`.

Verify: read each modified doc end-to-end; confirm a fresh contributor could implement a new app from these docs alone.

#### Step 10 — Full suite + lint

```bash
go test ./...
make lint
```

Both must be green before marking the cycle complete.

---

## 6. Verification Plan

### Automated

```bash
# New foundations
go test ./internal/apps/                  # contract.go, errors.go
go test ./internal/apps/baseapp/          # Reinstall helper

# Every app, including the previously-broken ForceInstall
go test ./internal/apps/...

# Tooling regression — categories that import apps must still compile
go test ./internal/tooling/...

# Full suite
go test ./...

# Lint
make lint
```

### Manual

1. **Compile-time interface check.** Build the project — every `var _ apps.App = (*X)(nil)` line catches drift. If anyone removes a method from an app, the build fails.
2. **Sentinel grep.** `grep -rn "uninstall not supported\|update not supported\|uninstall not implemented\|update not implemented" internal/apps/` should return **zero** matches in `.go` source (test assertions may keep the strings if asserting wrapped messages — prefer `errors.Is` instead).
3. **Commented-test grep.** `grep -rn "// func Test" internal/apps/` should return **zero** matches.
4. **Direct-mock grep.** `grep -rn "commands.NewMockCommand()" internal/apps/` should return **zero** matches in `_test.go` files (the testutil layer is the one place allowed to call it).
5. **`./devgeta install --help`** still works and lists every category.

### Regression

- `dg install` end-to-end on a clean macOS VM (or local clean run): every app installs as before. No behavior change for users — this is purely an internal contract change.
- `dg worktree create test` still works (regression check on a non-app feature).
- `dg version` still prints.

---

## 7. Risks & Trade-offs

| Risk                                                                       | Likelihood | Mitigation                                                                                                                                             |
| -------------------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Breaking change to `Fonts` API ripples into tooling                        | High       | Step 6 explicitly updates callers; `go build ./...` catches missed call sites                                                                          |
| Splitting Fonts off the `App` interface breaks a future `dg install` loop  | Med        | Tooling already iterates by category, not by `[]App`. Document this in app-interface.md. Future `dg uninstall <name>` will need to dispatch on `Kind`. |
| Refactor introduces silent behavior change in some app's install flow      | Med        | Every step has a verify command. The only intentional behavior change is `ForceInstall` going from "always failed" to "actually works"                 |
| 19 apps × 2 files = 38 file edits is a big PR                              | Med        | Commit boundaries match steps (one commit per step). Reviewer can read commits in order; the diff per commit is bounded                                |
| `mise.ForceInstall` may rely on Uninstall actually succeeding              | Low        | Read `mise.go` carefully in Step 5; if its Uninstall is real, leave behavior unchanged — `baseapp.Reinstall` only short-circuits on the sentinel       |
| Tests that previously passed only because `ForceInstall` returned an error | Low        | Re-enabling commented tests will catch this; if a test was relying on the bug, fix the test                                                            |
| Doc drift between `CLAUDE.md` §6 and the new guide                         | Low        | §6 becomes a one-paragraph pointer to the guide; no duplicated prose                                                                                   |

### Trade-offs Made

- **Sentinel errors over typed errors.** A `type UnsupportedOpError struct{Op string}` would be more flexible, but `errors.New` sentinels match Go idiom (`io.EOF`, `sql.ErrNoRows`) and are simpler for `errors.Is` checks. Revisit if we need to attach more metadata.
- **Fonts as `FontInstaller` not `App`.** Bending `App` to support a `name` parameter would warp 18 apps to accommodate one. Cleaner to declare Fonts an outlier and document it.
- **Single big cycle vs. multiple small ones.** This is more work than a typical cycle (~12h vs. ~4h), but splitting it leaves the codebase in an awkward intermediate state where some apps have the new contract and some don't. Better to land it as one cohesive change and keep each step's commit small.
- **`baseapp.Reinstall` as a free function vs. embedded base struct.** A free function avoids forcing a struct hierarchy on apps that don't want one (the Brave/Flameshot/Gimp set has no `Base` for a reason). Composition over inheritance.
- **Keep both constructor patterns.** Desktop GUI apps genuinely don't need `Base`. Enforcing `Cmd + Base` everywhere would add unused state. Document the split, don't eliminate it.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear? (Is the `ForceInstall` bug + missing contract framing convincing?)
- [ ] Engineer context sufficient? (Is the App Inventory table enough to navigate the migration?)
- [ ] Objective unambiguous? (Is "uniform contract, sentinel errors, fixed `ForceInstall`, migrated tests" the explicit success state?)
- [ ] Scope is actually locked? (No `dg configure` implementation creep?)
- [ ] Steps are actionable? (Each step has a verify command; commit boundaries match steps)
- [ ] Verification is executable? (The grep checks at the bottom are concrete)
- [ ] Risks are realistic? (Fonts split is the only one with non-trivial blast radius)
- [ ] Order is right? (Foundations → Claude reference → desktop → terminal → fonts → devgeta → tests → docs)

**Reviewer notes:**
(Fill in during review.)

---

## Notes for Implementers

- **Step 1 and Step 2 are load-bearing** — every later step imports from them. Get them right and tested first.
- **Use Claude (Step 3) as the template.** It already has the correct `ForceInstall`. Pattern-match the rest of the fleet against it.
- **Commit per step**, not per file. Run `/smart-commit` after each step's verify check. Each commit should leave the system fully working — interface assertions catch drift mid-migration.
- **Don't expand scope.** If a refactor opportunity surfaces (e.g., `mise.go` could use a cleaner template generator), note it in `ROADMAP.md` for the next cycle and move on. The goal here is uniformity, not perfection.
- **The bug fix is the headline.** `ForceInstall` going from "always fails" to "actually reinstalls" is a real user-visible improvement. Mention it in the PR description.
- **Next cycle preview.** Once this lands, `dg uninstall <appname>` becomes ~50 lines: switch on `apps.AppKind`, look up the app by `Name()`, call `Uninstall()`, handle `errors.Is(err, ErrUninstallNotSupported)` with a friendly message. Same shape for `dg configure`, `dg update`, `dg reinstall`. That's the payoff.
