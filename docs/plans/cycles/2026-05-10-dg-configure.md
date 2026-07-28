# Cycle: dg configure command

**Date:** 2026-05-10  
**Estimated Duration:** ~2.5 hours  
**Status:** Done

---

## 1. Domain Context

Devgeta manages the installation and configuration of development tools. Each app implements `ForceConfigure()` and `SoftConfigure()` on the `App` interface in `internal/apps/contract.go`. Until now, configuration was only triggered automatically during `dg install`. This cycle adds a standalone `dg configure [app]` command so users can re-apply configs for a specific app at any time — without reinstalling.

This was explicitly planned in ROADMAP.md and unblocked once the app foundations cycle (2026-05-01) landed a stable `App` interface with `ErrConfigureNotSupported`.

**Relevant docs:** ROADMAP.md, `internal/apps/contract.go`, `internal/apps/errors.go`, `docs/guides/cli-patterns.md`, `docs/guides/app-interface.md`

---

## 2. Engineer Context

**Relevant files and their purposes:**
- `internal/apps/contract.go` — `App` interface; has `ForceConfigure()` and `SoftConfigure()`
- `internal/apps/errors.go` — Sentinel errors including `ErrConfigureNotSupported`
- `internal/apps/registry/registry.go` — **New file**: maps app name → `apps.App` instance; also exposes `Names()` for discovery UX
- `cmd/configure.go` — **New file**: `dg configure [app] [--force]` command handler + `init()` registration
- `pkg/constants/constants.go` — App name constants (e.g., `constants.Neovim = "neovim"`)

**Key functions/types involved:**
- `apps.App.ForceConfigure()` — Overwrites existing config files
- `apps.App.SoftConfigure()` — Only applies config if files don't already exist
- `apps.ErrConfigureNotSupported` — Returned by apps with no config to deploy
- `registry.GetApp(name string) (apps.App, error)` — New lookup function
- `registry.Names() []string` — Returns all registered names; used in error messages for discovery

**Pattern used by all apps (see `internal/apps/git/git.go:77-86`):**
```go
func (g *Git) ForceConfigure() error {
    return files.CopyDir(paths.Paths.App.Configs.Git, paths.Paths.Config.Git)
}

func (g *Git) SoftConfigure() error {
    if files.FileAlreadyExist(configFile) {
        return nil
    }
    return files.CopyDir(paths.Paths.App.Configs.Git, paths.Paths.Config.Git)
}
```

**Command registration pattern (canonical):**
Commands self-register in their own `init()` function via `rootCmd.AddCommand(...)`. See `cmd/install.go:49-57` and `cmd/worktree.go`. Do NOT add registration to `cmd/root.go` — that file is for global setup only.

**Testing patterns used in this area:**
- See `docs/guides/testing-patterns.md` — always use `testutil.MockApp`, never execute real commands
- Initialize logger in test files: `func init() { testutil.InitLogger() }`
- Verify no real commands: `testutil.VerifyNoRealCommands(t, mockApp.Base)`

**Commands to run tests:**
```bash
go test ./internal/apps/registry/
go test ./cmd/
go test ./...
make lint
```

---

## 3. Objective

Implement `dg configure [app]` command that re-applies configuration files for a named app, with `--force` flag to overwrite existing configs, so users can update their tool configs without reinstalling.

---

## 4. Scope Boundary

### In Scope

- [x] `internal/apps/registry/registry.go` — app lookup by name string, covering all 19 named apps; `Names()` for sorted app list
- [x] `internal/apps/registry/registry_test.go` — tests for lookup success, unknown app error, all apps registered, `Names()` count
- [x] `cmd/configure.go` — command handler + self-registration in `init()`; `--force` flag; ErrConfigureNotSupported handled as info; unknown app prints sorted names list
- [x] `cmd/configure_test.go` — tests for: success path, `--force` dispatch, default soft dispatch, unknown app error with non-zero exit, ErrConfigureNotSupported exits zero
- [x] `docs/spec.md` — add configure command to Features section
- [x] `cmd/root.go` — update Long help text to list `configure` in Available Commands

### Explicitly Out of Scope

- `dg configure` with no app (configure all apps) — deferred until `dg list` exists
- Config backup/rollback before overwrite — deferred to a future backup cycle
- Config validation (`dg validate`) — separate planned command
- Per-app configuration options (e.g., `--with-plugin=foo`) — needs design
- Fonts (uses `FontInstaller`, not `App`) — different interface, handle separately
- Case-insensitive app name matching — strict exact matching only for now
- Interactive `--force` confirmation — silent overwrite; confirmation deferred

**Scope is locked.** If you discover something out of scope is needed, document it for a future cycle and reference here.

---

## 5. Design Decisions

**Unknown-app error UX:** Since `dg list` is out of scope, the unknown-app error must be self-contained. On unrecognized app name, print a sorted list of all supported names inline:
```
unknown app "foo"

Supported apps:
  aerospace  alacritty  brave  claude  devgeta  docker  fastfetch
  flameshot  gimp       git    i3      lazydocker  lazygit  mise
  neovim     opencode   raycast  tmux  ulauncher
```

**Command registration:** Self-register in `cmd/configure.go:init()`. No changes to `cmd/root.go` `init()` — only update its `Long` help string.

**`--force` behavior:** Always overwrites silently. No confirmation prompt in this cycle.

**App name matching:** Strict exact match (case-sensitive). Normalization (lowercasing input) can be added later if users request it.

---

## 6. Implementation Plan

### File Changes

| Action | File Path | Description |
|--------|-----------|-------------|
| Create | `internal/apps/registry/registry.go` | App name → apps.App lookup + Names() |
| Create | `internal/apps/registry/registry_test.go` | Registry tests |
| Create | `cmd/configure.go` | configure command handler + init() self-registration |
| Create | `cmd/configure_test.go` | Command behavior tests |
| Modify | `cmd/root.go` | Update Long help text only |
| Modify | `docs/spec.md` | Document configure command |

### Step-by-Step

#### Step 1: Create app registry

Create `internal/apps/registry/registry.go`:
- `map[string]func() apps.App` with all 19 apps; constructors called lazily on `GetApp`
- `GetApp(name string) (apps.App, error)` — returns app or error with supported names list
- `Names() []string` — returns sorted list of all registered names
- Expected outcome: Registry compiles, returns correct app instances
- Verify: `go build ./internal/apps/registry/`

#### Step 2: Write registry tests

Create `internal/apps/registry/registry_test.go`:
- `TestGetApp_KnownApp` — returns non-nil app with correct Name()
- `TestGetApp_UnknownApp` — returns non-nil error
- `TestGetApp_AllRegisteredApps` — table test over all 19 expected names
- `TestNames_ContainsAllApps` — verifies count == 19 and output is sorted
- Use `func init() { testutil.InitLogger() }`
- Verify: `go test ./internal/apps/registry/ -v`

#### Step 3: Create configure command

Create `cmd/configure.go`:
- `configureCmd` with `Use: "configure [app]"`, `Args: cobra.ExactArgs(1)`
- `configureForce bool` flag (name distinct from worktree's `forceFlag`)
- `init()` calls `rootCmd.AddCommand(configureCmd)` — no changes to root.go `init()`
- `runConfigure`: lookup via registry → call Force/SoftConfigure → handle sentinel → print result
- On unknown app: return the registry error (which includes supported names)
- On `ErrConfigureNotSupported`: `utils.PrintInfo(...)`, return nil
- On success: `utils.PrintSuccess(...)`
- Expected outcome: Command compiles, help text is correct
- Verify: `go build ./cmd/` and `./devgeta configure --help`

#### Step 4: Write command tests

Create `cmd/configure_test.go`:
- Use a mock `apps.App` that records which configure method was called
- `TestConfigure_SoftPath` — default (no `--force`) calls SoftConfigure
- `TestConfigure_ForcePath` — `--force` calls ForceConfigure
- `TestConfigure_UnknownApp` — RunE returns non-nil error
- `TestConfigure_NotSupported` — ErrConfigureNotSupported → RunE returns nil
- Use `func init() { testutil.InitLogger() }`
- Verify: `go test ./cmd/ -run TestConfigure -v`

#### Step 5: Update root help text

Modify `cmd/root.go` Long string only:
- Add `configure` to the Available Commands list with a one-line description
- No changes to `init()` or any other logic
- Verify: `./devgeta --help` shows configure in the list

#### Step 6: Update spec

Modify `docs/spec.md`:
- Add configure command to Features section with usage examples and flag descriptions
- Verify: Read docs/spec.md and confirm clarity

---

## 7. Verification Plan

### Automated Verification

```bash
go test ./internal/apps/registry/ -v
go test ./cmd/ -run TestConfigure -v
make lint
go test ./... -cover
```

### Manual Verification (sandboxed)

Run all manual checks with an isolated HOME to avoid touching real user configs:

```bash
export HOME=$(mktemp -d)
./devgeta configure --help
./devgeta configure git            # SoftConfigure: applies only if missing
./devgeta configure git --force    # ForceConfigure: always overwrites
./devgeta configure brave          # ErrConfigureNotSupported: info message, exit 0
./devgeta configure unknown-app    # Error with supported names list, exit non-zero
echo $?                            # Verify non-zero
unset HOME
```

### Regression Check

- `dg install --help` still works
- `dg worktree --help` still works
- `dg version` still works
- `go test ./...` all pass

---

## 8. Risks & Trade-offs

| Risk | Likelihood | Mitigation |
|------|------------|-----------|
| App constructor side-effects on registry init | Low | Lazy factories — constructed only when `GetApp` is called |
| ErrConfigureNotSupported treated as failure | Low | Explicit `errors.Is()` check; returns nil |
| Registry misses a new app added later | Low | Table test in registry_test.go enumerates all 19 expected names; CI will catch gaps |
| `--force` with real HOME overwrites user configs | Med | Manual verification prescribed with sandboxed HOME |

### Trade-offs Made

- **Switch dispatch vs. registry map**: Registry map is cleaner and testable. Registry wins.
- **Require app arg vs. configure-all default**: Requiring `[app]` avoids mass-reconfiguration accidents.
- **`ErrConfigureNotSupported` is info, not error**: Exiting non-zero for "no config to apply" would be confusing in scripts. Print and exit 0.
- **Discovery via Names() vs. dg list**: Since `dg list` is out of scope, print `registry.Names()` inline in the error message for immediate discoverability without extra infrastructure.

---

## 9. Cross-Model Review Notes

- [x] Domain context clear?
- [x] Engineer context sufficient?
- [x] Objective unambiguous?
- [x] Scope is actually locked?
- [x] Steps are actionable?
- [x] Verification is executable?
- [x] Risks are realistic?

**Review feedback addressed (2026-05-10):**
- Added `cmd/configure_test.go` to scope and step-by-step
- Unknown-app error now prints sorted `registry.Names()` list inline — no dependency on `dg list`
- Removed `cmd/root.go init()` change; command self-registers in `cmd/configure.go:init()` per project convention
- Added `cmd/root.go` Long help text update to scope (documentation surface)
- Manual verification now prescribes sandboxed `HOME=$(mktemp -d)` to prevent real config overwrites

---

## Notes for Implementers

- **Cycle document is your spec.** Update it if requirements change.
- **Commit after each step.** Run `/smart-commit` once a step's verify check passes.
- **Verification must pass before "done."** Automated tests + sandboxed manual checks + regression check.
- **If you hit a risk, escalate immediately.** Don't handle it silently.
