# Cycle: Neovim Dependencies

**Date:** 2026-05-15
**Estimated Duration:** ~3 hours
**Status:** Done

---

## 1. Domain Context

Neovim requires several system tools to function correctly: `make`, `gcc`, `ripgrep`, `fd-find`,
`unzip`, a clipboard utility (`xclip` on Linux; macOS uses built-in `pbcopy`/`pbpaste`), and
`tree-sitter-cli` for parser generation and syntax highlighting. These are not installed today
when a user runs `dg install --only neovim`, meaning a fresh targeted install can produce a
working Neovim binary that silently fails at runtime.

The ROADMAP marks this as 🔴 Priority. The design decision (Option C) is to add a thin
`InstallDeps` helper inside the existing `neovim` package, called by `Install()` and
`SoftInstall()` before the Neovim binary itself is installed. No full `App` interface modules
are created for the deps — they are installed via `MaybeInstallPackage` (idempotent).

Note: `ripgrep`, `fd-find`, and `unzip` are already installed by `InstallDevTools` and
`InstallCoreLibs` during a full `dg install`. They are included in `InstallDeps` specifically
to cover the `dg install --only neovim` path where those flows are skipped.

---

## 2. Engineer Context

**Relevant files:**

| File | Purpose |
|------|---------|
| `internal/apps/neovim/neovim.go` | Neovim app — `Install()` and `SoftInstall()` need to call `InstallDeps` |
| `internal/apps/neovim/neovim_test.go` | Existing tests — must be updated to account for dep installs |
| `pkg/constants/constants.go` | Add `Make`, `Gcc`, `Xclip`, `TreeSitterCli` constants |
| `pkg/constants/package_mappings.go` | Add `TreeSitterCli` mapping (brew: `tree-sitter`, apt: `tree-sitter-cli`) |
| `internal/commands/mock.go` | `MockCommand` needs per-package error injection and call history |
| `internal/testutil/testutil.go` | `MockApp` infrastructure used in all tests |

**Key patterns:**
- `MaybeInstallPackage(constant)` — idempotent install, used for all deps
- `base.IsMac()` — platform guard for `xclip`/`gcc` (Linux-only) and tree-sitter fallback
- `Base.ExecCommand(cmd.CommandParams{Command: "npm", Args: [...]})` — npm fallback path
- `MockBaseCommand.ExecCommandCalls []CommandParams` — already tracks all `ExecCommand` calls
- `testutil.VerifyNoRealCommands` checks `ExecCommandCallCount > 0` — do NOT call in tests
  that intentionally exercise the npm fallback; assert `ExecCommandCalls` contents directly
- `MockCommand.MaybeInstalled` tracks only the **last** call — must add call history and
  per-package error map before reliable dep testing is possible
- Package name translation: `pkg/constants/package_mappings.go` maps brew→apt names

**Test commands:**
```bash
go test ./internal/apps/neovim/...
go test ./pkg/constants/...
go build -o /tmp/devgeta-test main.go && rm /tmp/devgeta-test
go vet ./...
```

---

## 3. Objective

Install all ROADMAP-listed Neovim dependencies (`make`, `gcc`, `ripgrep`, `fd-find`, `unzip`,
`xclip`, `tree-sitter-cli`) as prerequisites whenever Neovim is installed — including via
`dg install --only neovim` — with graceful fallback for `tree-sitter-cli` and transparent
state tracking in `global_config.yaml`.

---

## 4. Dependency Tiers

| Tier | Packages | Behavior |
|------|----------|----------|
| **Hard** (must succeed) | `make`, `gcc` (Linux), `ripgrep`, `fd-find`, `unzip`, `xclip` (Linux) | Error returned if install fails; Neovim install aborts |
| **Soft** (best-effort) | `tree-sitter-cli` | Primary package manager → npm fallback → warn-and-continue; never fails Neovim install |

---

## 5. Scope Boundary

### In Scope
- [ ] Enhance `MockCommand` in `internal/commands/mock.go`: add `MaybeInstalledPkgs []string` call history and `MaybeInstallErrors map[string]error` per-package error injection
- [ ] Add constants: `Make`, `Gcc`, `Xclip`, `TreeSitterCli` to `pkg/constants/constants.go`
- [ ] No `package_mappings.go` change needed (`tree-sitter-cli` is the same on brew and apt)
- [ ] Create `internal/apps/neovim/deps.go` with `InstallDeps(base, cmd, gc)` function
- [ ] Wire `InstallDeps` into `Neovim.Install()` and `Neovim.SoftInstall()`
- [ ] Track `tree-sitter-cli` npm fallback success in `global_config.yaml`
- [ ] Create `internal/apps/neovim/deps_test.go` with full test coverage
- [ ] Update `neovim_test.go` existing tests to use call history assertions

### Explicitly Out of Scope
- Full `App` interface modules for any of the new deps
- Uninstalling deps when Neovim is uninstalled
- Installing `node` as a prerequisite for the npm fallback
- Any changes to `tree-sitter` grammars or Neovim plugin configuration
- `gcc` on macOS (Xcode CLT provides `clang`; brew `gcc` is redundant and large)

---

## 6. Implementation Plan

### File Changes

| Action | File Path | Description |
|--------|-----------|-------------|
| Modify | `internal/commands/mock.go` | Add `MaybeInstalledPkgs []string` + `MaybeInstallErrors map[string]error` to `MockCommand` |
| Modify | `pkg/constants/constants.go` | Add `Make`, `Gcc`, `Xclip`, `TreeSitterCli` constants |
| ~~Modify~~ | `pkg/constants/package_mappings.go` | No change needed — `tree-sitter-cli` name is identical on brew and apt |
| Create | `internal/apps/neovim/deps.go` | `InstallDeps` helper + `installTreeSitter` fallback logic |
| Create | `internal/apps/neovim/deps_test.go` | Tests for `InstallDeps` |
| Modify | `internal/apps/neovim/neovim.go:58` | `Install()` calls `InstallDeps` first |
| Modify | `internal/apps/neovim/neovim.go:69` | `SoftInstall()` calls `InstallDeps` first |
| Modify | `internal/apps/neovim/neovim_test.go` | Update assertions to use `MaybeInstalledPkgs` call history |

---

### Step-by-Step

#### Step 1: Enhance `MockCommand` in `internal/commands/mock.go`

Add to `MockCommand` struct:
```go
MaybeInstalledPkgs  []string         // ordered history of all MaybeInstallPackage calls
MaybeInstallErrors  map[string]error // per-package error injection: pkg -> error
```

Update `MaybeInstallPackage`:
```go
func (m *MockCommand) MaybeInstallPackage(pkg string, alias ...string) error {
    m.MaybeInstalled = pkg                          // preserve existing single-value field
    m.MaybeInstalledPkgs = append(m.MaybeInstalledPkgs, pkg)
    if m.MaybeInstallErrors != nil {
        if err, ok := m.MaybeInstallErrors[pkg]; ok {
            return err
        }
    }
    return m.MaybeInstallError
}
```

Update `NewMockCommand` to initialize the new fields:
```go
MaybeInstalledPkgs: []string{},
MaybeInstallErrors: map[string]error{},
```

Update `Reset()` to clear them:
```go
m.MaybeInstalledPkgs = []string{}
m.MaybeInstallErrors = map[string]error{}
```

**Verify:** `go build ./internal/commands/...` passes. Run existing mock-dependent tests to confirm no regressions: `go test ./...`

---

#### Step 2: Add constants to `pkg/constants/constants.go`

Add these four constants near `Ripgrep` and `Unzip`:
```go
Make          = "make"
Gcc           = "gcc"
Xclip         = "xclip"
TreeSitterCli = "tree-sitter-cli"  // Same name on both Homebrew and apt (trixie/sid)
```

**Verify:** `go build ./pkg/constants/...` passes.

---

#### Step 3: Add `TreeSitterCli` to `pkg/constants/package_mappings.go`

`tree-sitter-cli` is the same package name on both Homebrew and apt — no mapping entry
needed. `make`, `gcc`, `xclip`, `ripgrep`, and `unzip` also have identical names on both
platforms. `fd-find` already has an existing entry (`fd` → `fd-find`).

No changes to `package_mappings.go` are required for this cycle.

**Verify:** `go test ./pkg/constants/...` passes. `package_mappings.go` requires no changes.

---

#### Step 4: Create `internal/apps/neovim/deps.go`

```go
package neovim

import (
    "fmt"

    cmd "github.com/cjairm/devgeta/internal/commands"
    "github.com/cjairm/devgeta/internal/config"
    "github.com/cjairm/devgeta/pkg/constants"
    "github.com/cjairm/devgeta/pkg/logger"
)

// InstallDeps installs all system packages required for Neovim to function correctly.
// Hard deps (make, gcc, ripgrep, fd-find, unzip, xclip) return an error if they fail.
// tree-sitter-cli is soft: primary package manager → npm fallback → warn-and-continue.
// Called by Install() and SoftInstall() before the Neovim binary is installed.
func InstallDeps(base cmd.BaseCommandExecutor, c cmd.Command) error {
    // make — required by Neovim plugin ecosystem (e.g. telescope-fzf-native)
    if err := c.MaybeInstallPackage(constants.Make); err != nil {
        return fmt.Errorf("failed to install make: %w", err)
    }

    // gcc — required for compiling Neovim plugins on Linux
    // On macOS, Xcode CLT provides clang; brew gcc is unnecessary
    if !base.IsMac() {
        if err := c.MaybeInstallPackage(constants.Gcc); err != nil {
            return fmt.Errorf("failed to install gcc: %w", err)
        }
    }

    // ripgrep — live grep search in Neovim (e.g. telescope live_grep)
    if err := c.MaybeInstallPackage(constants.Ripgrep); err != nil {
        return fmt.Errorf("failed to install ripgrep: %w", err)
    }

    // fd-find — fast file finder used by Neovim plugins (e.g. telescope find_files)
    if err := c.MaybeInstallPackage(constants.FdFind); err != nil {
        return fmt.Errorf("failed to install fd-find: %w", err)
    }

    // unzip — required for extracting Neovim plugin archives
    if err := c.MaybeInstallPackage(constants.Unzip); err != nil {
        return fmt.Errorf("failed to install unzip: %w", err)
    }

    // xclip — clipboard integration for Neovim on Linux
    // macOS uses pbcopy/pbpaste which are built-in
    if !base.IsMac() {
        if err := c.MaybeInstallPackage(constants.Xclip); err != nil {
            return fmt.Errorf("failed to install xclip: %w", err)
        }
    }

    // tree-sitter-cli — best-effort; fallback to npm if primary package manager fails
    installTreeSitter(base, c)

    return nil
}

// installTreeSitter attempts to install tree-sitter-cli via the primary package
// manager (brew on macOS, apt on Linux), falling back to npm install -g.
// On Debian Bookworm (stable), tree-sitter-cli is only in trixie/sid, so the
// apt path is expected to fail and npm is the real install path.
// If both fail, a warning is logged and nil is returned — Neovim still installs.
func installTreeSitter(base cmd.BaseCommandExecutor, c cmd.Command) {
    primaryErr := c.MaybeInstallPackage(constants.TreeSitterCli)
    if primaryErr == nil {
        return
    }
    logger.L().Warnw("Primary tree-sitter-cli install failed, trying npm fallback",
        "error", primaryErr)

    _, stderr, err := base.ExecCommand(cmd.CommandParams{
        Command: "npm",
        Args:    []string{"install", "-g", "tree-sitter-cli"},
    })
    if err != nil {
        logger.L().Warnw("npm fallback for tree-sitter-cli also failed — skipping",
            "error", err, "stderr", stderr)
        return
    }

    // Track npm-installed tree-sitter-cli in global_config for transparent state
    gc := &config.GlobalConfig{}
    if loadErr := gc.Load(); loadErr != nil {
        logger.L().Warnw("Could not load global config to track tree-sitter-cli",
            "error", loadErr)
        return
    }
    gc.AddToInstalled(constants.TreeSitterCli, "package")
    if saveErr := gc.Save(); saveErr != nil {
        logger.L().Warnw("Could not save global config after tracking tree-sitter-cli",
            "error", saveErr)
    }
}
```

**Verify:** `go build ./internal/apps/neovim/...` passes.

---

#### Step 5: Wire `InstallDeps` into `neovim.go`

In `Install()` (~line 58), add as first call:
```go
func (n *Neovim) Install() error {
    if err := InstallDeps(n.Base, n.Cmd); err != nil {
        return fmt.Errorf("failed to install neovim dependencies: %w", err)
    }
    if n.Base.IsMac() {
        return n.Cmd.InstallPackage(constants.Neovim)
    }
    return n.installDebianNeovim()
}
```

In `SoftInstall()` (~line 69), add as first call:
```go
func (n *Neovim) SoftInstall() error {
    if err := InstallDeps(n.Base, n.Cmd); err != nil {
        return fmt.Errorf("failed to install neovim dependencies: %w", err)
    }
    if n.Base.IsMac() {
        return n.Cmd.MaybeInstallPackage(constants.Neovim)
    }
    // ... rest unchanged
}
```

**Verify:** `go build ./internal/apps/neovim/...` passes.

---

#### Step 6: Create `internal/apps/neovim/deps_test.go`

| Test | Platform | tree-sitter primary | npm fallback | Expected |
|------|----------|---------------------|-------------|---------|
| `TestInstallDeps_Mac` | macOS | success | — | make, ripgrep, fd-find, unzip, tree-sitter installed; gcc/xclip NOT in call list |
| `TestInstallDeps_Linux` | Linux | success | — | make, gcc, ripgrep, fd-find, unzip, xclip, tree-sitter all in `MaybeInstalledPkgs` |
| `TestInstallDeps_TreeSitterFallback_Linux` | Linux | fails | success | npm ExecCommand called; nil returned; `VerifyNoRealCommands` NOT called |
| `TestInstallDeps_TreeSitterBothFail` | Linux | fails | fails | npm ExecCommand called; nil returned (warn-and-continue) |
| `TestInstallDeps_MakeFails` | Linux | — | — | error returned immediately; gcc not attempted |
| `TestInstallDeps_RipgrepFails` | Linux | — | — | error returned |

For fallback tests: assert `mockApp.Base.ExecCommandCalls[0].Command == "npm"` instead of
calling `VerifyNoRealCommands`.

For happy-path tests: assert `mockApp.Cmd.MaybeInstalledPkgs` contains expected packages
in order; call `VerifyNoRealCommands` since no `ExecCommand` calls are expected.

**Verify:** `go test ./internal/apps/neovim/...` passes.

---

#### Step 7: Update `neovim_test.go` existing tests

`TestInstall` currently asserts `mockApp.Cmd.InstalledPkg == constants.Neovim`. This is
unaffected (dep installs use `MaybeInstallPackage`, Neovim binary uses `InstallPackage`).

`TestSoftInstall` currently asserts `mockApp.Cmd.MaybeInstalled == constants.Neovim`. Since
`MaybeInstalled` now reflects the last call (which is still Neovim), this may pass as-is.
However, add an assertion on `MaybeInstalledPkgs` to confirm deps were installed first:
```go
// Confirm deps were installed before neovim itself
if len(mockApp.Cmd.MaybeInstalledPkgs) < 2 {
    t.Fatalf("expected dep installs before neovim, got %v", mockApp.Cmd.MaybeInstalledPkgs)
}
last := mockApp.Cmd.MaybeInstalledPkgs[len(mockApp.Cmd.MaybeInstalledPkgs)-1]
if last != constants.Neovim {
    t.Errorf("expected last MaybeInstall to be neovim, got %q", last)
}
```

**Verify:** `go test ./internal/apps/neovim/...` passes with no failures.

---

## 7. Verification Plan

### Automated Verification

```bash
go test ./internal/apps/neovim/...
go test ./internal/commands/...
go test ./pkg/constants/...
go build -o /tmp/devgeta-test main.go && rm /tmp/devgeta-test
go vet ./...
```

All commands must exit 0 with no failures.

### Manual Verification (on a clean VM)

1. Run `dg install --only neovim`
2. Verify all deps present:
   ```bash
   make --version
   gcc --version          # Linux only
   rg --version
   fd --version
   unzip -v
   xclip -version         # Linux only
   tree-sitter --version
   nvim --version
   ```
3. Check `~/.config/devgeta/global_config.yaml` — `tree-sitter` should appear under `installed.packages` (especially on Bookworm where npm fallback runs)
4. Open Neovim — no "missing binary" warnings on startup.

### Regression Check

- `TestInstall`, `TestSoftInstall`, `TestInstallDebian` in `neovim_test.go` must all pass
- `go test ./internal/tooling/terminal/...` must pass (terminal coordinator unchanged)
- `go test ./pkg/constants/...` must pass
- All existing mock-dependent tests must pass after `MockCommand` enhancement in Step 1

---

## 8. Risks & Trade-offs

| Risk | Likelihood | Mitigation |
|------|------------|------------|
| `MockCommand.MaybeInstallError` is global; existing tests that set it may now fail on first dep, not Neovim | Medium | Step 1 adds per-package errors; existing tests that don't set per-package errors are unaffected (global fallback preserved) |
| `tree-sitter-cli` apt absent on Debian Bookworm stable | High (expected) | npm fallback handles this; `global_config.yaml` updated on npm success |
| `ripgrep`/`fd-find`/`unzip` double-installed on full `dg install` | Low | `MaybeInstallPackage` is idempotent — safe to call twice |
| `gcc` on macOS: brew gcc vs Xcode CLT conflict | Low | Skipped on macOS by design |

### Trade-offs Made

- **`gcc` skipped on macOS**: Xcode CLT (`clang`) is sufficient for all Neovim plugin compilation. Installing brew `gcc` would pull in a large toolchain unnecessarily.
- **`tree-sitter-cli` is soft dep**: Needed for parser development, not basic Neovim use. A missing binary does not prevent Neovim from starting.
- **`ripgrep`/`fd-find`/`unzip` duplicated across terminal flows**: Acceptable — `MaybeInstallPackage` is idempotent, and correctness for `--only neovim` users outweighs the minor redundancy.
- **No uninstall of deps**: `make`, `gcc`, `ripgrep` etc. are general-purpose system tools shared across workflows. Removing them on Neovim uninstall would be destructive.

---

## 9. Cross-Model Review Notes

- [ ] Root cause confirmed? (N/A — feature addition)
- [ ] All affected files identified?
- [ ] Verification steps are executable?
- [ ] Scope is appropriately bounded?
- [ ] Mock enhancement is backward-compatible with existing tests?

**Reviewer notes:**
(Fill in during review)
