# Cycle: Dependency Upgrades, Dead Code Removal, and Cobra API Improvements

**Date:** 2026-05-10  
**Estimated Duration:** ~4-5 hours  
**Status:** Done

---

## 1. Domain Context

This cycle addresses accumulated dependency drift, dead code, and non-idiomatic Cobra patterns:

- **Go version** in `go.mod` is 1.23.0, but the environment (mise) provides 1.26.3 — three minor versions behind
- **Cobra** is on v1.8.1; v1.10.2 is the current stable release (December 2025) and introduces shell completion improvements (`CompletionFunc` type, PowerShell support, better multi-value flag detection) and context-aware help
- **Zap** is on v1.27.0; v1.28.0 is the current stable release (April 2026)
- **Bubbletea + Lipgloss** (charmbracelet) are pinned in `go.mod` but **completely unused** — the only consuming file (`internal/tui/installer.go`) is dead code with no callers anywhere in the project
- **Cobra patterns** in the codebase predate idiomatic Go error handling: all commands use `Run` + manual `utils.MaybeExitWithError()` instead of `RunE`; a non-standard `ParseFlags` in `init()` intercepts `--version` before Cobra runs; no shell completion is wired up

The charmbracelet removal is the highest-value change: eliminates dead code and 7+ transitive dependencies. The Cobra API improvements are additive — no behavior regressions, but meaningfully cleaner code and new UX (tab completion).

Relevant docs: [CLAUDE.md](../../CLAUDE.md), [ROADMAP.md](../../ROADMAP.md) (TUI open question section)

---

## 2. Engineer Context

**Files to modify or delete:**

| File | Purpose | Change |
|------|---------|--------|
| `go.mod` | Module definition | Version bumps |
| `go.sum` | Checksums | Auto-updated by `go mod tidy` |
| `internal/tui/installer.go` | **Dead code** — bubbletea/lipgloss, no callers | **DELETE** |
| `cmd/root.go` | Root Cobra command, persistent flags | `SilenceUsage`, deprecate `--debug`, remove `PersistentPreRunE` logger call if moving to `RunE`, set `rootCmd.Version` + `SetVersionTemplate` |
| `cmd/version.go` | `dg version` subcommand + `--version` flag | Remove `init()` side-effect pattern; replace `--version` flag + `ParseFlags` + `os.Exit` with Cobra's built-in Version mechanism |
| `cmd/install.go` | `dg install` command | `Run` → `RunE` |
| `cmd/worktree.go` | `dg worktree` + 7 subcommands | `Run` → `RunE`; add `NoArgs()` on list/jump/prune; add `ValidArgsFunction` on remove and jump |
| `cmd/completion.go` | **New file** | `dg completion [bash\|zsh\|fish\|powershell]` command |
| `pkg/logger/logger.go` | Zap initialization | No code change needed; `go get` updates version |
| `CLAUDE.md` | Tech Stack table | Update Go version note |

**Key functions and types involved:**

- `cobra.Command.RunE` — error-returning handler (replaces `Run` + manual exit)
- `cobra.Command.SilenceUsage` — suppresses usage dump on errors
- `cobra.Command.Version` + `cobra.Command.SetVersionTemplate()` — built-in version output
- `cobra.ValidArgsFunction` — dynamic shell completion callback
- `cobra.NoArgs()` — arg validator for commands that take no arguments
- `cmd.Flags().MarkDeprecated("debug", "use --verbose instead")` — built-in flag deprecation
- `rootCmd.GenZshCompletion()` / `GenBashCompletion()` etc. — built-in completion generators (available since Cobra v1.2)

**Current version output format (must be preserved):**

```go
// cmd/version.go:27 — current format:
fmt.Printf("devgeta %s (commit: %s, built: %s)\n", Version, Commit, BuildDate)
```

This format must survive migration to `rootCmd.Version`. Preserve it using:

```go
rootCmd.Version = Version
rootCmd.SetVersionTemplate(fmt.Sprintf(
    "devgeta {{.Version}} (commit: %s, built: %s)\n", Commit, BuildDate,
))
```

**Non-standard pattern being removed (`cmd/version.go:31-43`):**

```go
func init() {
    rootCmd.Flags().BoolP("version", "v", false, "Print version information")
    rootCmd.ParseFlags(os.Args[1:])   // side-effect: runs before Cobra's own parsing
    if versionFlag, _ := rootCmd.Flags().GetBool("version"); versionFlag {
        fmt.Printf(...)
        os.Exit(0)                    // hard exit bypasses cleanup
    }
}
```

This bypasses Cobra's normal lifecycle and causes `os.Exit(0)` in `init()` — a surprising behavior that will disappear after migration.

**Binary artifact locations (from Makefile):**

```
devgeta               → make build (current platform)
devgeta-darwin-arm64  → make all
devgeta-darwin-amd64  → make all
devgeta-linux-amd64   → make all
```

All artifacts land in the **repo root**, not a `build/` directory.

**Testing patterns:**

- See [docs/guides/testing-patterns.md](../guides/testing-patterns.md) — always use `testutil.MockApp`, never execute real commands
- `ValidArgsFunction` completion callbacks should be tested with a mocked worktree lister
- New `cmd/completion_test.go` should verify the command exists and generates non-empty output

---

## 3. Objective

Upgrade Go, Cobra, and Zap to current stable versions; remove the unused charmbracelet dead code; adopt Cobra v1.9–v1.10 API improvements (RunE, shell completion, built-in Version, NoArgs validators); ship a cleaner, smaller binary with tab-completion support for worktree commands.

---

## 4. Scope Boundary

### In Scope

**Track A — Dependency cleanup:**
- [x] Delete `internal/tui/installer.go` (dead code, no callers)
- [x] Upgrade Go directive in `go.mod`: 1.23.0 → 1.26.3; toolchain: go1.23.5 → go1.26.3
- [x] Upgrade Cobra: v1.8.1 → v1.10.2
- [x] Upgrade Zap: v1.27.0 → v1.28.0
- [x] Run `go mod tidy`; verify charmbracelet entries removed from `go.mod`/`go.sum`

**Track B — Cobra API improvements:**
- [x] Easy wins: `SilenceUsage: true` on rootCmd; `NoArgs()` on list/jump/prune; `MarkDeprecated("debug", "use --verbose instead")`
- [x] Migrate all `Run` handlers → `RunE` across `cmd/install.go`, `cmd/version.go`, `cmd/worktree.go` (7 subcommands); remove `utils.MaybeExitWithError()` at each call site
- [x] Replace `cmd/version.go` `init()` side-effect (ParseFlags + os.Exit) with `rootCmd.Version` + `rootCmd.SetVersionTemplate()`; preserve exact output format
- [x] Add `cmd/completion.go`: `dg completion [bash|zsh|fish|powershell]`
- [x] Wire `ValidArgsFunction` on `worktreeRemoveCmd` and `worktreeJumpCmd` for dynamic worktree name completion
- [x] Update CLAUDE.md Tech Stack Go version note

### Explicitly Out of Scope

- Upgrading or replacing promptui — no newer versions exist; keep at v0.9.0
- Migrating selection UI from promptui to `fzf` — separate future cycle
- Rebuilding TUI with charmbracelet — project direction is `fzf`-based
- Changing `dg version` subcommand behavior — `dg version` and `dg --version` must produce identical output

**Scope is locked.** If you discover anything out of scope is needed, document it for a future cycle.

---

## 5. Implementation Plan

### Track A: Dependency Cleanup

#### Step A1: Delete dead code

```bash
rm internal/tui/installer.go
# If directory is now empty, remove it too:
# rmdir internal/tui/
```

- Verify: `go build ./...` succeeds; `rg "charmbracelet|bubbletea|lipgloss" --glob '*.go'` → no results

#### Step A2: Upgrade Go version

Edit `go.mod` manually — change the `go` and `toolchain` directives:

```
go 1.26.3
toolchain go1.26.3
```

- Verify: `go build ./...` succeeds

> **Rollback note:** If CI fails on the `go` directive (older toolchains may reject 1.26.3 in the module line), set `go 1.23.0` with `toolchain go1.26.3` — the module minimum and the toolchain version are independent. Don't lower the toolchain directive.

#### Step A3: Upgrade Cobra

```bash
go get github.com/spf13/cobra@v1.10.2
```

> **pflag note:** Cobra v1.10.0 renamed `ParseErrorsWhitelist` → `ParseErrorsAllowlist` in pflag. Devgeta does not use pflag directly, so no code changes needed. After `go get`, verify `go build ./...` compiles without pflag errors.

- Verify: `go build ./cmd/...` succeeds; `go test ./cmd/...` passes

#### Step A4: Upgrade Zap

```bash
go get go.uber.org/zap@v1.28.0
```

- Verify: `go build ./pkg/logger/...` succeeds; `go test ./...` passes

#### Step A5: Tidy module graph

```bash
go mod tidy
```

- Verify:

```bash
rg "charmbracelet|bubbletea|lipgloss" go.mod go.sum  # must return nothing
```

- Commit: `refactor: remove unused charmbracelet TUI dead code`
- Commit: `deps: upgrade Go 1.23→1.26.3, cobra 1.8.1→1.10.2, zap 1.27→1.28`
- Commit: `chore: go mod tidy after upgrades`

---

### Track B: Cobra API Improvements

#### Step B1: Easy wins in `cmd/root.go`

Add to `rootCmd` initialization:

```go
rootCmd.SilenceUsage = true   // don't dump usage on every error
```

Add after flag definitions:

```go
rootCmd.PersistentFlags().MarkDeprecated("debug", "use --verbose instead")
```

Add `cobra.NoArgs()` to `worktreeListCmd`, `worktreeJumpCmd`, `worktreePruneCmd` in `cmd/worktree.go`:

```go
Args: cobra.NoArgs(),
```

- Verify: `go build ./...`; `dg worktree list extra-arg` should now error cleanly without usage dump
- Commit: `refactor: cobra easy wins — SilenceUsage, NoArgs, deprecate --debug`

#### Step B2: `RunE` migration

Replace every `Run: func(...)` with `RunE: func(...) error` across:

- `cmd/install.go` — 1 handler
- `cmd/version.go` — 1 handler  
- `cmd/worktree.go` — 7 handlers (create, list, remove, jump, repair, prune, parent)

Pattern:

```go
// Before:
Run: func(cmd *cobra.Command, args []string) {
    err := doSomething(args)
    utils.MaybeExitWithError(err)
},

// After:
RunE: func(cmd *cobra.Command, args []string) error {
    return doSomething(args)
},
```

After migrating all handlers, check if `utils.MaybeExitWithError()` is still needed anywhere outside of Cobra handlers. If not, remove it or keep for non-Cobra paths.

- Verify: `go build ./...`; `go test ./...`; error paths still exit non-zero
- Commit: `refactor: migrate all cobra commands from Run to RunE`

#### Step B3: Replace `--version` init() hack with Cobra's built-in Version

In `cmd/version.go`, remove the entire `init()` block that does:

```go
rootCmd.Flags().BoolP("version", "v", ...)
rootCmd.ParseFlags(os.Args[1:])
if versionFlag ... { os.Exit(0) }
```

In `cmd/root.go`, set the built-in Version field **and** preserve the exact output format via `SetVersionTemplate`:

```go
rootCmd.Version = Version
rootCmd.SetVersionTemplate(fmt.Sprintf(
    "devgeta {{.Version}} (commit: %s, built: %s)\n", Commit, BuildDate,
))
```

Keep the `dg version` subcommand (`versionCmd`) in place — it should continue to work as before. The built-in `--version` flag is additive.

**Acceptance criteria:**
- `dg version` → `devgeta <ver> (commit: <sha>, built: <date>)`
- `dg --version` → same format
- Neither command calls `os.Exit()` inside `init()`

- Verify: `go build . && ./devgeta version && ./devgeta --version` — both print matching output
- Commit: `refactor: replace init() version flag hack with cobra built-in Version field`

#### Step B4: Add `dg completion` command

Create `cmd/completion.go`:

```go
package cmd

import (
    "os"
    "github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
    Use:   "completion [bash|zsh|fish|powershell]",
    Short: "Generate shell completion script",
    Long: `To load completions:

Bash:   source <(dg completion bash)
Zsh:    echo 'source <(dg completion zsh)' >> ~/.zshrc
Fish:   dg completion fish | source
PowerShell: dg completion powershell | Out-String | Invoke-Expression`,
    DisableFlagsInUseLine: true,
    ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
    Args:                  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        switch args[0] {
        case "bash":
            return rootCmd.GenBashCompletion(os.Stdout)
        case "zsh":
            return rootCmd.GenZshCompletion(os.Stdout)
        case "fish":
            return rootCmd.GenFishCompletion(os.Stdout, true)
        case "powershell":
            return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
        default:
            return fmt.Errorf("unsupported shell: %s", args[0])
        }
    },
}

func init() {
    rootCmd.AddCommand(completionCmd)
}
```

**Acceptance criteria:**
- `dg completion zsh` → prints non-empty zsh completion script
- `dg completion bash` → prints non-empty bash completion script
- `dg completion unknown` → returns an error, exits non-zero
- Command does not panic when run outside a git repo

- Verify: `go build . && ./devgeta completion zsh | head -5` shows a valid script header
- Commit: `feat: add dg completion command for shell tab completion`

#### Step B5: Wire `ValidArgsFunction` on worktree remove and jump

In `cmd/worktree.go`, add `ValidArgsFunction` to `worktreeRemoveCmd` and `worktreeJumpCmd` so tab-completing their first argument lists current worktree names:

```go
ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
    if len(args) != 0 {
        return nil, cobra.ShellCompDirectiveNoFileComp
    }
    names, err := worktree.ListNames() // existing or new thin helper
    if err != nil {
        return nil, cobra.ShellCompDirectiveError
    }
    return names, cobra.ShellCompDirectiveNoFileComp
},
```

If `worktree.ListNames()` does not exist, add a thin wrapper in `internal/tooling/worktree/` that returns just the names (not the full struct) — do not implement new worktree logic.

**Acceptance criteria:**
- `dg worktree remove <TAB>` → lists current worktree names
- `dg worktree jump <TAB>` → lists current worktree names
- Returns empty list (not error) when no worktrees exist
- Returns `ShellCompDirectiveError` on read failure (not panic)
- Works when run outside a git repo (returns empty list gracefully)

- Verify: `go build . && ./devgeta __complete worktree remove ""` → prints worktree names (Cobra's internal completion test command)
- Commit: `feat: add dynamic tab completion for worktree remove and jump`

#### Step B6: Update CLAUDE.md

Update the Tech Stack table Go version note from `Go 1.21+` → `Go 1.23+` (or the actual minimum the project now enforces).

- Commit: `docs: update CLAUDE.md Go version note`

---

## 6. Verification Plan

### Automated Verification

```bash
# No charmbracelet in Go source files
rg "charmbracelet|bubbletea|lipgloss" --glob '*.go'   # must return nothing

# No charmbracelet in go.mod
rg "charmbracelet|bubbletea|lipgloss" go.mod go.sum   # must return nothing

# All tests pass
go test ./...

# Format and vet
make lint

# All three platform builds land in repo root
make all
ls devgeta-darwin-arm64 devgeta-darwin-amd64 devgeta-linux-amd64
```

### Manual Verification

1. `./devgeta --help` → custom help renders correctly; no usage dump on `--help`
2. `./devgeta version` → `devgeta <ver> (commit: <sha>, built: <date>)`
3. `./devgeta --version` → same format as above
4. `./devgeta install --help` → shows `--only` and `--skip` flags
5. `./devgeta worktree --help` → lists all subcommands including `completion`
6. `./devgeta worktree list extra-arg` → clean error (no usage dump, thanks to `SilenceUsage`)
7. `./devgeta --debug` → shows deprecation warning; still activates verbose logging
8. `./devgeta completion zsh` → prints non-empty zsh script
9. `./devgeta completion bash` → prints non-empty bash script
10. `./devgeta __complete worktree remove ""` → lists worktree names (or empty list if none)
11. Binary size: `ls -lh devgeta` — should be slightly smaller than before charmbracelet removal

### Regression Check

- `dg install` interactive mode works (promptui category selection functions)
- `dg worktree create/list/remove` work end-to-end
- All three platform binaries produced by `make all`

---

## 7. Risks & Trade-offs

| Risk | Likelihood | Mitigation |
|------|------------|-----------|
| Go 1.26 stdlib change breaks something | Very Low | `go test ./...` catches it; Go maintains strong backward compat |
| Cobra v1.10 pflag rename causes compile error | Very Low | Devgeta doesn't use pflag directly; `go build` will surface it immediately |
| `init()` removal in version.go changes startup order | Low | No other `init()` depends on the version flag; test with `dg --version` |
| `ValidArgsFunction` panics outside git repo | Low | Guard with `err != nil` → return `ShellCompDirectiveError` |
| `internal/tui/` directory left empty after delete | Low | Check `ls internal/tui/`; remove directory if empty |
| Go toolchain directive rejected by older CI | Low | If CI fails, set `go 1.23.0` + `toolchain go1.26.3` independently |
| `go mod tidy` silently changes unrelated indirect deps | Low | Review `go.sum` diff before committing; focus on charmbracelet removals |

### Trade-offs Made

- **Scope kept as one cycle:** The two tracks are related (Cobra upgrade enables Cobra API improvements) and the Cobra refactor is additive — no behavior regression risk. Splitting would create unnecessary back-and-forth between the dependency version and the API surface.
- **`dg version` subcommand kept:** The built-in `--version` flag is additive. `dg version` continues to exist for discoverability. Both produce identical output.
- **Version output format preserved exactly:** Using `SetVersionTemplate` to keep `devgeta <ver> (commit: <sha>, built: <date>)` — this is user-visible and must not change.
- **Promptui kept at v0.9.0:** Upstream is unmaintained but stable; replacing it is a UX decision for a separate cycle.
- **Charmbracelet removed entirely:** Speculative code that was never wired up. Removing now is better than maintaining a dependency that doesn't run.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear? (Do I understand the problem being solved?)
- [ ] Engineer context sufficient? (Do I know which files to touch and patterns to follow?)
- [ ] Objective unambiguous? (Is "done" crystal clear?)
- [ ] Scope is actually locked? (No ambiguity, no scope creep temptation?)
- [ ] Steps are actionable? (Each 5-15 min, with clear success criteria?)
- [ ] Verification is executable? (Can someone actually run these steps?)
- [ ] Risks are realistic? (Have similar risks appeared before?)

**Reviewer notes:**
(Fill in during review)

---

## Notes for Implementers

- **Work Track A before Track B.** Get the dependencies upgraded and green first; then layer on Cobra API improvements on a clean compile.
- **Commit after each step.** Run `/smart-commit` once a step's verify check passes.
- **Verification must pass before "done."** Full `go test ./...` + `make all` + manual version/completion checks.
- **Do not touch promptui.** It is out of scope.
- **Watch the `go.sum` diff after tidy.** Removed entries should be charmbracelet-family only. If unrelated packages change, investigate before committing.
- **`ValidArgsFunction` must not panic.** Test with `./devgeta __complete worktree remove ""` in a repo with no worktrees and outside a git repo.
