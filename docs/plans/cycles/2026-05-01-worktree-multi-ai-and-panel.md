# Cycle: Worktree — Multi-AI Launcher, Centralized Storage, Gap Hardening, and Jump Dialog

**Date:** 2026-05-01
**Estimated Duration:** ~10–12 hours (3 phases, independently shippable)
**Status:** DONE

---

## 1. Domain Context

The `dg worktree` (`wt`) command pairs a git worktree with a tmux window and auto-launches an AI
assistant in that window. Introduced in [`2026-04-22-worktree-ux-improvements.md`](./2026-04-22-worktree-ux-improvements.md).

Three things need fixing:

1. **Hardcoded OpenCode** — `internal/tooling/worktree/worktree.go:94` always launches OpenCode even when Claude Code is installed. Users need a `--ai` flag (and config default) to pick the launcher.
2. **In-repo `.worktrees/` is the wrong home** — worktrees inside the repo show up as untracked noise, can't be listed across projects, and make a shared fzf view of "all my worktrees" impossible. Moving to `~/.local/share/devgeta/worktrees/<repo-slug>/` (`paths.Paths.Data.Root`) fixes all of this in one move and lets `dg wt ls` and `dg wt j` be cross-repo by default.
3. **Navigation is tedious** — `dg wt j` only shows worktrees from the current repo and uses a flat fzf call with no key-action bindings. A richer jump that surfaces all worktrees and allows actions (delete, repair) from the same dialog replaces the need for a separate `dg wt panel` command.

**Key principle: devgeta owns the dialog. No hard dependency on tmux or nvim.**

- `dg wt j` is a standalone command. It uses `fzf` (already a devgeta dependency) and works whether or not the user is inside a tmux session.
- Inside tmux, jump = `tmux select-window`. Outside tmux, jump = print the worktree path to stdout (user can wrap in a shell function: `wtj() { cd "$(dg wt j)"; }`).
- The shipped `tmux.conf` adds a _convenience_ keybinding that calls `dg wt j` inside `display-popup`. It is not required for the feature to work; users who don't run tmux still get the dialog by typing `dg wt j`.
- We do **not** integrate with nvim-tree, tmux `choose-tree`, or any other external picker. The list rendering and key handling live in devgeta.

Reference: `paths.Paths.Data.Root` already resolves to `$XDG_DATA_HOME` or `~/.local/share`
(see `pkg/paths/paths.go:225`).

---

## 2. Engineer Context

**Relevant files:**

| File                                             | Role                                           |
| ------------------------------------------------ | ---------------------------------------------- |
| `cmd/worktree.go`                                | Cobra subcommands (create, list, remove, jump) |
| `internal/tooling/worktree/worktree.go`          | `WorktreeManager` coordinator                  |
| `internal/tooling/worktree/worktree_test.go`     | Existing mocked tests                          |
| `internal/apps/git/git.go`                       | Worktree + branch helpers                      |
| `internal/apps/tmux/tmux.go`                     | Window primitives                              |
| `internal/tooling/terminal/dev_tools/fzf/fzf.go` | `SelectFromList` (plain fzf)                   |
| `pkg/paths/paths.go`                             | `paths.Paths.Data.Root` → `~/.local/share`     |
| `internal/config/fromFile.go:68`                 | `GlobalConfig` (add `Worktree` settings here)  |
| `configs/tmux/tmux.conf`                         | Add `<prefix> + K` binding here                |

**Key decisions embedded in this doc:**

- Centralized path = `filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, name)`
  where `repoSlug = filepath.Base(git rev-parse --show-toplevel)`.
- Repo-slug collision (two repos named the same) is documented but not solved here — out of scope.
- `dg wt j` enhanced jump uses a raw `fzf` invocation with `--expect ctrl-x,ctrl-r` rather than
  `Fzf.SelectFromList`, because `SelectFromList` doesn't expose `--expect`. Keep it internal to worktree package.
- **Breaking change**: `.worktrees/` inside the repo is gone. The user is the only consumer right now (no deprecation needed) — bare migration note in the PR is enough.

**Cobra patterns to follow:**

All subcommands keep the existing hierarchical structure (`dg wt <verb> [args] [flags]`) and standard conventions:

- `Use:`, `Short:`, `Long:` set on every command (existing).
- `Aliases:` for short verbs (existing: `c`/`new`, `l`/`ls`, `rm`/`r`, `j`).
- `Args:` validators (`cobra.ExactArgs(1)`, `cobra.MaximumNArgs(1)`).
- Flag conventions: `--ai`/`-a`, `--force`/`-f`. Long+short pair using `Flags().StringVarP`/`BoolVarP`.
- **Configuration hierarchy** (precedence high → low) for the AI coder selection:
  1. `--ai` flag
  2. `DEVGETA_WORKTREE_AI` env var
  3. `worktree.default_ai` in `~/.config/devgeta/global_config.yaml`
  4. Built-in default `"opencode"`

  Resolved in `cmd/worktree.go` before calling `wm.Create`/`wm.Repair`. Same chain reused for both subcommands.

**Run tests:**

```bash
go test ./internal/tooling/worktree/ ./internal/apps/git/ ./internal/apps/tmux/ ./internal/config/ ./cmd/
make lint
```

---

## 3. Objective

`dg wt create <name> --ai <coder>` launches the chosen AI in a centralized worktree; `dg wt j`
shows all worktrees across repos plus regular tmux windows in a single fzf dialog with
jump/delete/repair actions; `<prefix> + K` opens that dialog as a popup; `dg wt prune` bulk-removes all worktrees.

---

## 4. Scope Boundary

### In Scope

**Phase 1 — AI launcher abstraction**

- [ ] `AICoder` interface in `internal/tooling/worktree/aicoder.go` with `OpenCodeCoder` + `ClaudeCoder` impls
- [ ] `ResolveAICoder(alias string) (AICoder, error)` — accepts `opencode|oc|claude|cc|claudecode` (case-insensitive); empty string is not resolved here (callers walk the precedence chain first)
- [ ] `EnsureInstalled() error` on each impl — uses `exec.LookPath`; returns actionable hint (`dg install --only terminal`)
- [ ] `Worktree WorktreeConfig` added to `GlobalConfig` (`worktree.default_ai: opencode`)
- [ ] `--ai` / `-a` flag on `dg wt create` and `dg wt repair`
- [ ] Resolution chain (Cobra-standard): `--ai` flag → `DEVGETA_WORKTREE_AI` env → `worktree.default_ai` config → `"opencode"` default
- [ ] `WorktreeManager.Create(name, coder AICoder)` — replaces hardcoded `constants.OpenCode` at `:94`

**Phase 2 — Centralized storage + gap hardening**

- [ ] Move worktree root from `.worktrees/` in the repo to `~/.local/share/devgeta/worktrees/<repo-slug>/`
  - Add `GetWorktreeBasePath() string` to `paths` (or compute inline in the manager — ok either way)
  - `repoSlug` = `filepath.Base(git rev-parse --show-toplevel)` from `Git.GetRepoRoot()`
  - `WorktreeManager.Create` builds the full path as: `worktreeBasePath/<name>`
- [ ] `dg wt ls` reads the centralized dir — shows worktrees from **all repos**, with a `REPO` column
- [ ] Gap hardening on `Create` — `worktreeState(name)` helper → `{wtExists, windowExists, branchExists}`:
  - wt+window exist → error "already exists; use `dg wt jump <name>`"
  - wt only → error "window missing; use `dg wt repair <name>`"
  - window only → error "orphan window; run `tmux kill-window -t wt-<name>` manually"
  - neither → normal create
- [ ] New `dg wt repair <name> [--ai <coder>]` — creates missing window for an existing worktree and re-sends the AI command
- [ ] Tolerant `Remove` — handles missing wt dir or missing window independently:
  - wt gone but window exists → kill window, `git worktree prune`
  - window gone but wt exists → skip tmux, proceed with git removal
  - Add `Git.PruneWorktrees() error` and `Git.IsWorktreeDirty(path) (bool, error)` helpers
  - `--force` / `-f` flag to bypass dirty-tree guard (default: error if dirty)
  - **Keep** branch deletion as `-D` (current behavior, force-delete) — no merge check for now (simpler)
- [ ] New `dg wt prune` — removes **all** worktrees in `~/.local/share/devgeta/worktrees/` with a "are you sure?" prompt; calls `Remove` per worktree

**Phase 3 — Enhanced jump (devgeta-owned, tmux-optional)**

- [ ] `dg wt j` works standalone — no hard dependency on tmux:
  - **Inside tmux** (`TMUX` env set):
    - List = all devgeta worktrees (all repos, from centralized dir) + current tmux windows that are not worktree-owned (shown below the worktrees, prefixed with `[win]`)
    - `enter` → `tmux select-window -t wt-<name>` if window exists; else offer repair. For `[win]` rows, `tmux select-window -t <name>`
  - **Outside tmux**:
    - List = all devgeta worktrees (no `[win]` rows since there is no session)
    - `enter` → print the absolute worktree path to stdout. Exit 0. (Users wrap in shell function: `wtj() { cd "$(dg wt j)"; }`)
  - Format per row (tab-delimited, fzf `--with-nth 1,2,3`): `<repo-slug>/<name>  <branch>  <status>`
  - Use raw `fzf` with `--expect ctrl-x,ctrl-r` — first stdout line = pressed key, second = selected row
  - `ctrl-x` → `Remove(name, false)` (confirmation prompt inline). For `[win]` rows: no-op.
  - `ctrl-r` → `Repair(name, resolvedCoder)`. For `[win]` rows: no-op.
- [ ] Add convenience binding to `configs/tmux/tmux.conf` (optional for the user; opt-in via the shipped config):
  ```
  # devgeta worktree jump dialog (convenience binding; dg wt j works standalone too)
  bind K display-popup -E -w 80% -h 60% "dg wt j"
  ```
  Verify `K` is not bound — currently unbound.
- [ ] Add `--all` flag stub on `dg wt j` (currently: always all repos since centralized; flag reserved for future cross-machine / mounted-path support)

### Explicitly Out of Scope

- Repo-slug collision resolution (two repos with same `basename`)
- Per-worktree AI persistence across `repair` invocations (repair uses flag/config default)
- `fzf --preview` showing git status in the jump dialog
- Branch-merge check before deletion (`--keep-branch` flag deferred)
- Cross-repo `--all` with a stored repos registry
- Migration tooling for old `.worktrees/` directories (document only)
- Auto-cleanup on shell exit

**Scope is locked.** New requirements → new cycle.

---

## 5. Implementation Plan

### File Changes

| Action | File                                         | Description                                                                                        |
| ------ | -------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Create | `internal/tooling/worktree/aicoder.go`       | `AICoder` interface, two impls, `ResolveAICoder`                                                   |
| Create | `internal/tooling/worktree/aicoder_test.go`  | Alias coverage, missing-binary error                                                               |
| Modify | `internal/tooling/worktree/worktree.go`      | Centralized path, `Create(name, coder)`, `Repair`, tolerant `Remove`, enhanced `Jump`, `Prune`     |
| Modify | `internal/tooling/worktree/worktree_test.go` | Update path assumptions, new methods                                                               |
| Modify | `internal/apps/git/git.go`                   | `IsWorktreeDirty`, `PruneWorktrees`                                                                |
| Modify | `internal/apps/git/git_test.go`              | Tests for the two new helpers                                                                      |
| Modify | `internal/config/fromFile.go`                | Add `WorktreeConfig{DefaultAI string}` to `GlobalConfig`                                           |
| Modify | `cmd/worktree.go`                            | `--ai`/`-a` on create+repair; `--force` on remove; `repair` + `prune` subcommands; enhanced `jump` |
| Modify | `configs/tmux/tmux.conf`                     | `bind K display-popup -E -w 80% -h 60% "dg wt j"`                                                  |
| Modify | `.gitignore`                                 | Add `.claude/worktrees/`                                                                           |
| Modify | `docs/spec.md`                               | Update worktree section                                                                            |

### Step-by-Step

#### Phase 1 — AI launcher

**Step 1.1 — `AICoder` interface and registry**

Create `internal/tooling/worktree/aicoder.go`:

```go
type AICoder interface {
    Name() string     // "opencode" | "claude"
    Command() string  // sent via tmux send-keys, e.g. "opencode" or "claude"
    EnsureInstalled() error
}

func ResolveAICoder(alias string) (AICoder, error)
```

Alias table: `"" → nil (caller handles)`, `opencode|oc → OpenCodeCoder`, `claude|cc|claudecode → ClaudeCoder`.
`EnsureInstalled` uses `exec.LookPath(coder.Command())`.

Verify: `go build ./internal/tooling/worktree/`

**Step 1.2 — Tests**

- All aliases resolve correctly.
- Unknown alias returns error listing valid values.
- Missing binary returns error with `dg install --only terminal` hint.

Verify: `go test ./internal/tooling/worktree/ -run TestResolve`

**Step 1.3 — Config field**

In `internal/config/fromFile.go`, add before `GlobalConfig`:

```go
type WorktreeConfig struct {
    DefaultAI string `yaml:"default_ai"` // "opencode" | "claude"; empty = fallback to "opencode"
}
```

Add `Worktree WorktreeConfig \`yaml:"worktree"\``to`GlobalConfig`.
Zero value unmarshals cleanly; no migration needed.

Verify: `go test ./internal/config/`

**Step 1.4 — Wire flag + env + update Create**

- Add `--ai`/`-a` to `worktreeCreateCmd` and `worktreeRepairCmd` in `cmd/worktree.go` using `Flags().StringVarP(&aiAlias, "ai", "a", "", "...")`.
- Add a small helper `resolveAIAlias(flagValue string, gc *config.GlobalConfig) string` in `cmd/worktree.go`:
  ```go
  // Precedence: flag > env > config > default
  if flagValue != "" { return flagValue }
  if env := os.Getenv("DEVGETA_WORKTREE_AI"); env != "" { return env }
  if gc.Worktree.DefaultAI != "" { return gc.Worktree.DefaultAI }
  return "opencode"
  ```
- Pass result to `ResolveAICoder` → `EnsureInstalled` → `wm.Create(name, coder)`.
- Update `WorktreeManager.Create(name string, coder AICoder)`: replace `:94` hardcoded send with `coder.Command()`.

Verify: `go build ./...` + `dg wt create --help` shows the flag + table test for the resolver.

**Step 1.5 — Tests for coder wire-up + resolution chain**

- Mock `Tmux.SendKeysToWindow`; assert sent keys equal `coder.Command()` for each coder.
- Table test for `resolveAIAlias` covering: flag wins over env wins over config wins over default. Use `t.Setenv("DEVGETA_WORKTREE_AI", ...)` for the env layer.

---

#### Phase 2 — Centralized storage + gap hardening

**Step 2.1 — Centralized path helper**

In `worktree.go`, replace `worktreeDir = ".worktrees"` constant with a function:

```go
// worktreePath returns ~/.local/share/devgeta/worktrees/<repo-slug>/<name>
func (w *WorktreeManager) worktreePath(repoSlug, name string) string {
    return filepath.Join(paths.Paths.Data.Root, "devgeta", "worktrees", repoSlug, name)
}
```

Get `repoSlug` from `filepath.Base(w.Git.GetRepoRoot())`.
`GetWorktreeDir()` exported func becomes a computed path (update callers in `cmd/worktree.go`).

Verify: `go build ./...`

**Step 2.2 — Cross-repo `List`**

`List()` now walks `~/.local/share/devgeta/worktrees/` two levels deep instead of calling `git worktree list`. Add a `Repo` field to `WorktreeStatus`. Keep `git worktree list --porcelain` for current repo only when we need branch info.

The table in `dg wt ls` gains a `REPO` column as the first column.

Verify: `go test ./internal/tooling/worktree/ -run TestList`

**Step 2.3 — `worktreeState` helper**

```go
type WorktreeState struct {
    WtPath      string
    WindowName  string
    WtExists    bool
    WindowExists bool
    BranchExists bool
}
func (w *WorktreeManager) worktreeState(repoSlug, name string) (WorktreeState, error)
```

Verify: test all meaningful combinations (mocked).

**Step 2.4 — Refactor `Create` to use state**

| wt  | win | branch | Action                                                       |
| --- | --- | ------ | ------------------------------------------------------------ |
| ✗   | ✗   | \*     | normal create                                                |
| ✓   | ✗   | \*     | error → "use `dg wt repair <name>`"                          |
| ✓   | ✓   | \*     | error → "use `dg wt jump <name>`"                            |
| ✗   | ✓   | \*     | error → "orphan window; run `tmux kill-window -t wt-<name>`" |

**Step 2.5 — `Git.IsWorktreeDirty` + `Git.PruneWorktrees`**

```go
// IsWorktreeDirty runs git -C <path> status --porcelain
func (g *Git) IsWorktreeDirty(path string) (bool, error)
// PruneWorktrees runs git worktree prune
func (g *Git) PruneWorktrees() error
```

Tests: mock stdout `"M file.go\n"` → dirty; `""` → clean.

**Step 2.6 — Tolerant `Remove(name string, force bool)`**

```
state := worktreeState(...)
if !state.WtExists && !state.WindowExists → error "nothing to remove"
if state.WtExists && !force → IsWorktreeDirty → abort if dirty
kill window if present (log on error, don't fail)
if state.WtExists → git worktree remove → git worktree prune
else → git worktree prune only
delete branch with -D (same as today)
```

Add `--force` / `-f` flag (Cobra: `Flags().BoolVarP(&force, "force", "f", false, "...")`). Wire into `worktreeRemoveCmd`.

**Step 2.7 — `Repair(name, coder)` + `dg wt repair`**

```go
func (w *WorktreeManager) Repair(name string, coder AICoder) error
```

- Requires `WtExists`; returns "no worktree to repair" if not.
- Creates window if missing, then `SendKeysToWindow(coder.Command())`.
- If window exists: re-sends keys (re-launches the AI in that window).

Add `worktreeRepairCmd` with `--ai` flag.

**Step 2.8 — `dg wt prune`**

```go
func (w *WorktreeManager) Prune() error
```

- Calls `List()` to get all worktrees across all repos.
- If empty → "nothing to prune".
- Print list, prompt `Remove all? [y/N]`.
- Call `Remove(name, force=false)` for each; collect errors; report summary.

Add `worktreePruneCmd` (no flags needed for v1).

---

#### Phase 3 — Enhanced jump + tmux keybinding

**Step 3.1 — Enhanced `Jump` (`dg wt j`)**

Replace `SelectWorktreeInteractively` call with a custom fzf invocation. The implementation must work both inside and outside tmux — gate behavior on `os.Getenv("TMUX") != ""`.

Build rows:

```
// Inside tmux: devgeta worktrees first, then current tmux windows that aren't worktree-owned
// Outside tmux: devgeta worktrees only
// Format: "<repo>/<name>\t<branch>\t<status>" for worktrees
//         "[win]\t<window-name>\t" for regular tmux windows (only added when in tmux)
```

Fzf invocation:

```
--header "enter: jump | ctrl-x: delete | ctrl-r: repair"
--expect ctrl-x,ctrl-r
--with-nth 1,2,3
--delimiter \t
--reverse
```

Parse output: line1 = key pressed (`""`, `ctrl-x`, `ctrl-r`), line2 = selected row.

Action dispatch:

| Inside tmux                                                                             | Outside tmux                                                     |
| --------------------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| `enter` on worktree → `Tmux.SelectWindow(wt-name)` if window exists; else prompt repair | `enter` on worktree → print absolute path to stdout, exit 0      |
| `enter` on `[win]` → `Tmux.SelectWindow(name)`                                          | (`[win]` rows not shown)                                         |
| `ctrl-x` on worktree → `Remove(name, false)` with inline confirm                        | same                                                             |
| `ctrl-r` on worktree → `Repair(name, resolvedCoder)`                                    | same (best-effort: warns "tmux not running, window not created") |
| `ctrl-x`/`ctrl-r` on `[win]` → no-op                                                    | n/a                                                              |

Verify: unit-test `formatJumpRow` and `parseJumpOutput` directly; skip actual fzf invocation.

**Step 3.2 — Tmux keybinding (convenience, not required)**

Append to `configs/tmux/tmux.conf`:

```tmux
# devgeta worktree jump (convenience binding; dg wt j works standalone too)
# <prefix> + K opens the dialog inside a tmux popup
bind K display-popup -E -w 80% -h 60% "dg wt j"
```

`K` is currently unbound in the config (verified by reading tmux.conf). This binding is opt-in by virtue of being in devgeta's shipped tmux.conf — users who don't run tmux still get the same dialog by typing `dg wt j`.

Verify: `tmux source-file ~/.tmux.conf` + `<C-Space> K` opens fzf dialog. Then verify outside tmux: open a non-tmux terminal, run `dg wt j`, confirm the dialog renders and `enter` prints the path.

**Step 3.3 — `.gitignore` + docs**

- `.gitignore`: add `.claude/worktrees/`
- `docs/spec.md`: worktree section — centralized path, `--ai` flag, `repair`, `prune`, `<prefix> + K`
- `cmd/worktree.go` help text: update examples for all subcommands

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/tooling/worktree/ ./internal/apps/git/ ./internal/apps/tmux/ ./internal/config/ ./cmd/
make lint
go test ./... -cover
```

### Manual — happy paths

1. `dg wt create feat-a` → window opens with OpenCode in `~/.local/share/devgeta/worktrees/<repo>/feat-a/`
2. `dg wt create feat-b --ai claude` → window opens with `claude`
3. `DEVGETA_WORKTREE_AI=claude dg wt create feat-c` → uses claude (env var precedence)
4. Set `worktree.default_ai: claude` in global config; unset env; `dg wt create feat-d` → uses claude (config precedence)
5. `dg wt ls` → shows worktrees from current and other repos with REPO column
6. `<C-Space> K` (in tmux) → fzf popup with all worktrees + tmux windows; enter jumps; ctrl-x deletes; ctrl-r repairs
7. **Outside tmux**: `dg wt j` → fzf renders, enter prints worktree path to stdout (no tmux required)

### Manual — gap paths

6. Kill `wt-feat-a` window manually → `dg wt create feat-a` says "use repair" → `dg wt repair feat-a` restores window
7. Make a dirty change in feat-b → `dg wt remove feat-b` blocked → `dg wt remove feat-b --force` succeeds
8. Delete the worktree dir outside devgeta → `dg wt remove feat-b` still kills window and prunes
9. `dg wt prune` → prompts, removes all worktrees

### Regression

- `dg install` still works
- Existing `dg wt list/jump/create` (no flags) still work
- `tmux source-file ~/.tmux.conf` succeeds with no errors
- `go test ./...` clean

---

## 7. Risks & Trade-offs

| Risk                                                       | Likelihood | Mitigation                                                                                                                                                                           |
| ---------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Breaking change**: old `.worktrees/` dirs become orphans | High       | Document in PR; note in release notes; users must `dg wt remove` before upgrading                                                                                                    |
| `fzf --expect` parsing fragile if names contain `\t`       | Low        | Reject names with `\t` or `/` at `Create` time                                                                                                                                       |
| Same `basename` for two different repos → path collision   | Medium     | Document limitation; out of scope to solve here                                                                                                                                      |
| `display-popup` requires tmux ≥ 3.2                        | Medium     | The keybinding is opt-in convenience. `dg wt j` itself never invokes `display-popup` — it just runs fzf. So tmux <3.2 (or no tmux at all) only loses the keybinding, not the dialog. |
| `git worktree remove` fails on non-empty dir when dirty    | Low        | `--force` flag is passed to git worktree remove when `force=true` on our side                                                                                                        |

### Trade-offs made

- **`dg wt panel` removed** — keybinding calls `dg wt j` directly. Simpler, no new command surface. The fzf dialog handles both the "list" and "jump" use cases, so `dg wt ls` keeps its role as scripting/piping output only.
- **`dg wt ls` kept** — different from fzf: it's a plain table useful in scripts or when `fzf` isn't available. Cross-repo now by default since it reads the centralized dir.
- **Branch deletion stays force (`-D`)** — same as current behavior; `--keep-branch` deferred. Fewer flags, consistent with what users already know.
- **Centralized storage over in-repo** — trades discoverability (no `.worktrees/` visible in `git status`) for cross-repo list and clean repo trees.
- **Devgeta owns the dialog (no tmux/nvim coupling)** — `dg wt j` is a self-contained command driven by fzf (already installed by devgeta). The shipped `tmux.conf` adds a keybinding for convenience, but the feature degrades gracefully without tmux. No nvim-tree, no `choose-tree` integration.
- **Env var added to resolution chain** — Cobra-standard `flag > env > config > default`. Env var name `DEVGETA_WORKTREE_AI` follows `<APP>_<SCOPE>_<KEY>` convention.

---

## 8. Cross-Model Review Notes

- [ ] Centralized path (`paths.Paths.Data.Root`) correct — check `pkg/paths/paths.go:225`?
- [ ] `repoSlug = filepath.Base(GetRepoRoot())` acceptable collision risk for this cycle?
- [ ] `dg wt j` standalone (no tmux required) behavior makes sense — print path to stdout outside tmux?
- [ ] Env var name `DEVGETA_WORKTREE_AI` follows project convention?
- [ ] `dg wt j` fzf `--expect` output parsing is the only non-obvious implementation detail — is the inline custom fzf call the right approach vs. extending `Fzf.SelectFromList`?
- [ ] Keybinding `K` — verify no conflict with existing tmux.conf bindings before shipping.

---

## Notes for Implementers

- **Breaking change must be called out in the PR description** — old `.worktrees/` inside repos becomes orphaned.
- Phases are independently shippable. Recommend: Phase 1 → Phase 2 → Phase 3, each as its own PR.
- Run `/smart-commit` after each step's verify passes.
- The custom fzf invocation in Phase 3 is the highest-risk code path — write format/parse helpers with unit tests before wiring them to the fzf subprocess.
