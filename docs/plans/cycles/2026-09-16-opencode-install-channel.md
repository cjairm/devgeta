# Cycle: One opencode install channel, on both platforms

**Date:** 2026-09-16
**Estimated Duration:** ~3 hours
**Status:** Done

---

## 1. Domain Context

Devgeta installs opencode two different ways depending on platform: `brew install
opencode` on macOS, and the upstream install script on Debian. Neither pins a
version.

opencode 1.18.30 shipped a startup crash (`TypeError: undefined is not an object
(evaluating 'a.name')` in `SystemPrompt.environment`) that killed every
`dg task review-run` reviewer with an opaque
`ERROR(Unexpected server error...)`, before any request reached a model. On
macOS the Homebrew formula offers only `stable`, so there was no way to hold
back or roll back without leaving devgeta's channel entirely.

Investigating the fix found three further defects in the Debian script path that
a naive macOS switch would have inherited: it runs a `#!/usr/bin/env bash`
script with `sh`, its idempotency check asks `dpkg -l` about a binary that is
never in dpkg, and its uninstall runs `apt-get remove` against a file in
`~/.opencode`.

Decision and its costs: [ADR-0047](../../decisions/ADR-0047-opencode-installs-from-its-official-script-on-every-platform.md).
Prior art for install-script apps: [ADR-0004](../../decisions/ADR-0004-ai-tools-install-category.md) §3.

---

## 2. Engineer Context

**The pattern to copy already exists.** `internal/apps/claude/claude.go:84-100`
installs Claude Code from its official script through `cmd.RunInstallScript(...,
"bash")`, detects with `exec.LookPath`, and uninstalls by removing the install
prefix. opencode should end up as the same shape. Do not write a new mechanism.

- **Relevant files:**
  - `internal/apps/opencode/opencode.go:57-82` — `Install` / `SoftInstall` / `Uninstall`, today all package-manager calls
  - `internal/apps/claude/claude.go:77-119` — the shape to follow
  - `internal/commands/debian_strategies.go:174-230` — `RunInstallScript`, the shared staged-download helper (keep)
  - `internal/commands/debian_strategies.go:365-397` — `InstallScriptStrategy`, whose only construction site is the opencode case (becomes dead)
  - `internal/commands/debian.go:112-117` — the opencode strategy case to remove
  - `pkg/paths/paths.go` — `Home` struct; needs the `~/.opencode` install prefix
  - `configs/templates/devgeta.zsh.tmpl:201` + `internal/config/fromFile.go:694,705` — where the `oc` alias renders; PATH entry goes alongside

- **Key facts about the upstream script** (verified 2026-09-16 against
  `https://opencode.ai/install`, which 307-redirects to
  `raw.githubusercontent.com/anomalyco/opencode/refs/heads/dev/install`):
  - shebang `#!/usr/bin/env bash` — it is not POSIX sh
  - installs to `$HOME/.opencode/bin`, hardcoded, no override env var
  - pins with `VERSION=` or `--version` (not used by this cycle — see §4)
  - downloads a GitHub release archive; **no checksum verification**

- **Testing patterns:** [testing-patterns.md](../../guides/testing-patterns.md).
  Never `opencode.New()` in a test that installs; use
  `&opencode.OpenCode{Cmd: mockApp.Cmd, Base: mockApp.Base}` and assert with
  `testutil.VerifyNoRealCommands(t, mockApp.Base)` against that same base.
  `Uninstall` removes a directory, so isolate `paths.Paths.Home.*` and
  `paths.Paths.Config.OpenCode` and restore via `t.Cleanup`.

- **Commands to run tests** (from the §6 `go list` query; opencode's importers
  are registry, task and terminal):

  ```bash
  go test ./internal/apps/opencode/ ./internal/apps/registry/ \
          ./internal/tooling/task/ ./internal/tooling/terminal/ \
          ./internal/commands/ ./pkg/paths/
  make lint
  ```

  Step 4 edits `configs/`, which the root package's embedded-config tests read —
  so the final verification run is the full `go test ./...` (CLAUDE.md §6).

---

## 3. Objective

opencode installs, detects and uninstalls identically on macOS and Debian, by
running its official install script with bash, keying every check on the binary
at `~/.opencode/bin/opencode`, with that directory on the PATH devgeta owns.

---

## 4. Scope Boundary

### In Scope

- [x] `Install` runs the official script with `bash` on both platforms
- [x] `SoftInstall` is idempotent against the installed binary
- [x] `Uninstall` removes `~/.opencode` and the existing config/global-config bookkeeping
- [x] Remove the now-unreachable opencode case from the Debian strategy switch, and the `InstallScriptStrategy` it was the only caller of
- [x] `~/.opencode/bin` added to the generated shell config
- [x] Tests for all of the above
- [x] Docs: ADR-0047, `internal/apps/opencode/README.md`, a migration note for macOS users with a brew-installed opencode, CLAUDE.md §11 strategy list

### Explicitly Out of Scope

- **Version pinning.** The maintainer chose to track latest (ADR-0047 decision 4) after the trade was put in writing. The script's `VERSION`/`--version`
  support is recorded in ADR-0047 for whoever revisits it.
- **A pin-mismatch warning on `dg install`.** Depends on a pin; nothing to
  compare against.
- **Checksum verification of the upstream script.** Not offered upstream; the
  accepted §4 cost is recorded in ADR-0047 decision 5.
- **Changing rtk/lazygit/lazydocker's verified `InstallGitHubBinary` channel.**
- **Auto-removing an existing brew-installed opencode.** Documented as a
  migration step, not done silently — §10 forbids changing installation paths
  silently.

**Scope is locked.**

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                        | Description                                                                            |
| ------ | ------------------------------------------------ | -------------------------------------------------------------------------------------- |
| Modify | `pkg/paths/paths.go`                             | Add `Home.OpenCode` = `~/.opencode`, the script's hardcoded prefix                     |
| Modify | `internal/apps/opencode/opencode.go:57-82`       | Install/SoftInstall/Uninstall onto the script + binary, claude's shape                 |
| Modify | `internal/commands/debian.go:112-117`            | Delete the opencode strategy case                                                      |
| Modify | `internal/commands/debian_strategies.go:365-397` | Delete `InstallScriptStrategy` (dead once the case is gone)                            |
| Modify | `internal/commands/debian_strategies_test.go`    | Drop `InstallScriptStrategy` tests; keep `RunInstallScript` ones, retargeted to `bash` |
| Modify | `internal/config/fromFile.go:694,705`            | Add the opencode bin-dir field to the shell-config template data                       |
| Modify | `configs/templates/devgeta.zsh.tmpl:201`         | Prepend `~/.opencode/bin` to PATH next to the `oc` alias                               |
| Modify | `internal/apps/opencode/opencode_test.go`        | Install/SoftInstall/Uninstall tests, all mocked                                        |
| Create | `docs/migrations/opencode-install-channel.md`    | macOS users: remove the leftover brew copy                                             |
| Modify | `internal/apps/opencode/README.md`               | Install channel, install location, how to pin by hand after a bad release              |
| Modify | `CLAUDE.md` §11                                  | Drop `InstallScriptStrategy` from the strategy list                                    |
| Modify | `docs/guides/cross-platform-installation.md`     | Same                                                                                   |

### Step-by-Step

#### Step 1: Add the install prefix to `pkg/paths`

- `Home.OpenCode = GetHomeDir(".opencode")`, with a comment that the script
  hardcodes it and offers no override.
- Verify: `go test ./pkg/paths/`

#### Step 2: Move opencode onto the script

- `const openCodeInstallScriptURL = "https://opencode.ai/install"`, commented
  like `claudeInstallScriptURL` with why it is staged rather than piped.
- `Install` → `cmd.RunInstallScript(o.Base, "opencode", openCodeInstallScriptURL, "bash")`.
- `SoftInstall` → return nil when the binary is already present; else `Install`.
  Check `~/.opencode/bin/opencode` as well as `exec.LookPath`, because a fresh
  install's directory is not yet on the running process's PATH.
- `Uninstall` → `os.RemoveAll(paths.Paths.Home.OpenCode)` (stdlib, not a shelled
  `rm`, so the `pkg/paths` test sandbox contains it), keeping the existing
  config removal, `DisableShellFeature`, `RegenerateShellConfig` and
  `RemoveFromInstalled` calls.
- `ForceInstall` already uses `baseapp.Reinstall`; unchanged.
- Verify: `go build ./... && go test ./internal/apps/opencode/`

#### Step 3: Delete the dead Debian strategy

- Remove the `case constants.OpenCode` from `getInstallationStrategy`, then
  `InstallScriptStrategy` and its tests. Keep `RunInstallScript` — claude and
  now opencode both call it.
- Retarget the surviving `RunInstallScript` tests to assert `bash`.
- Verify: `go build ./... && go test ./internal/commands/`

#### Step 4: Put `~/.opencode/bin` on PATH

- Add the field in `fromFile.go` next to `OpenCodeAlias`, render it in
  `devgeta.zsh.tmpl` beside the `oc` alias.
- The alias resolves `opencode` through PATH
  (`pkg/constants/coder_launch.go:76-79`), so it is dead without this.
- Verify: `go test ./internal/config/ .` — the root package pins the rendered
  template against constants (`TestShellConfigTemplateRendersCoderAliasesFromConstants`).

#### Step 5: Tests

- Install runs curl-then-bash, never brew/apt.
- SoftInstall with the binary present executes nothing.
- Uninstall removes the prefix and clears global-config state.
- Every one asserts `testutil.VerifyNoRealCommands` on the app's own base.
- Verify: the targeted list in §2.

#### Step 6: Docs and migration

- Migration note: macOS users who installed opencode before this change keep a
  brew copy that `dg uninstall opencode` no longer removes; `brew uninstall
opencode` once, then `dg install --only opencode`. Note that whichever comes
  first on PATH wins until they do.
- README: install location, and the `VERSION=`/`--version` escape hatch for a
  bad upstream release, since devgeta does not pin.
- Verify: read them cold.

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/apps/opencode/ ./internal/apps/registry/ \
        ./internal/tooling/task/ ./internal/tooling/terminal/ \
        ./internal/commands/ ./internal/config/ ./pkg/paths/
make lint
go test ./...        # Step 4 touches configs/ — CLAUDE.md §6
```

### Manual

1. `make build`
2. `./devgeta install --only opencode` on macOS → script runs, binary lands in `~/.opencode/bin`
3. Run it again → skips, does not re-run the installer (this is the Debian idempotency bug)
4. `opencode --version` in a new shell → resolves without a manual PATH edit
5. `./devgeta task review-run` → reviewers return verdicts, not `ERROR(...)`
6. `./devgeta uninstall opencode` → `~/.opencode` gone, `~/.config/opencode` gone
7. `./devgeta install --only opencode` again → reinstalls cleanly

### Regression

- `dg install` full run still completes; claude still installs (shared `RunInstallScript`)
- `dg configure opencode --force` and `--only=rtk` still work
- `dg theme set` still renders opencode's theme

---

## 7. Risks & Trade-offs

| Risk                                                      | Likelihood | Mitigation                                                                                              |
| --------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------- |
| Unverified script from a moving `dev` branch head         | Certain    | Accepted and recorded in ADR-0047 §5; staged via `RunInstallScript` so a bad download fails the install |
| An upstream release crashes on startup, as 1.18.30 did    | Med        | **Unmitigated by choice** (no pin). Recovery is a manual `--version` rerun, documented in the README    |
| macOS users left with a stale brew copy shadowing on PATH | High       | Migration note; `dg install` does not silently remove it (§10)                                          |
| Deleting `InstallScriptStrategy` breaks a caller          | Low        | It has exactly one construction site, which this cycle removes; `go build ./...` proves it              |
| Shell-config change lands before the binary moves         | Low        | PATH entry is harmless when the directory is absent                                                     |

### Trade-offs Made

- **Script over verified release binary.** ADR-0004 §3 preferred
  `InstallGitHubBinary` with mandatory SHA-256 precisely to avoid this. Taken
  anyway: it is opencode's only supported channel, devgeta already ran it on
  Debian, and the maintainer confirmed it end to end. Scoped to opencode.
- **Latest over pinned.** Maintainer's call after the rollback cost was stated.
  Fixes the channel, not the exposure.
- **Delete the strategy rather than leave it unused.** §6 forbids speculative
  code; `RunInstallScript`, the part with two real callers, survives.

---

## 8. Cross-Model Review Notes

- [x] Does `SoftInstall` checking both `LookPath` and the hardcoded prefix double-count, or is the prefix check genuinely needed on a first install?
- [x] Is `os.RemoveAll` on `~/.opencode` safe against a user who symlinked it?
- [x] Should the PATH entry be gated on the opencode shell feature flag, the way `DisableShellFeature(constants.OpenCode)` in `Uninstall` implies?

**Reviewer notes:**

1. **Both checks are needed, and the prefix one carries the idempotency.** A
   freshly installed `~/.opencode/bin` is not on the running process's PATH, nor
   on the user's until they open a new shell, so a PATH-only check re-runs the
   installer on every `dg install` — the exact Debian defect this cycle fixes.
   The `LookPathFn` check is not redundant either: it is what leaves a user's
   existing opencode (npm, brew, their own build) alone instead of laying a
   second copy over it. Both directions are pinned by `TestSoftInstall`, whose
   first subtest stubs `LookPathFn` to fail so the prefix check alone must carry
   the skip.
2. **Yes.** `os.RemoveAll` does not follow a symlink — it unlinks it. A user who
   symlinked `~/.opencode` elsewhere loses the link, not the target.
3. **Gated on the flag**, inside the same `{{if .Opencode}}` block as the `oc`
   alias, so `dg uninstall opencode` → `DisableShellFeature` → regenerate takes
   the PATH entry with it. Checked in both directions by
   `TestShellConfigPutsOpenCodeBinDirOnPath`. The line is also guarded against
   prepending a second copy when a shell config is re-sourced.
