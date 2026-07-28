# Cycle: Worktree v2 — TUI Dashboard (Phase 1: core two-pane dashboard + fzf replacement)

**Date:** 2026-06-07
**Estimated Duration:** ~10-12 hours (must-haves, incl. Step 0 API prep, mouse-resize,
attach with auto-repair, delete, repair, ANSI-safe rendering, untracked-file diff,
offline/narrow-terminal handling, removal of the fzf jump flow, helpers + tests); +2-3h
for nice-to-haves. This is realistically a **~2-cycle chunk** because Phase 1 now fully
replaces the fzf popup (jump + delete + repair parity) before deleting it, and adds the
rendering-robustness work surfaced in review. Consider splitting the nice-to-haves (or
the whole removal/Step 8b) into a Phase 1b if time is tight.
**Status:** Done

---

## 1. Domain Context

`dg worktree` (`wt`) pairs a git worktree with a tmux window running an AI coder
(Claude Code focus). Today, navigation is an **fzf popup** (`dg wt j`, bound to
`prefix + u` in `configs/tmux/tmux.conf:113`) that lists worktrees and supports
jump (Enter), delete (ctrl-d), repair (ctrl-r).

This is the feature the maintainer uses most. **v2** turns the fzf popup into a
persistent full-screen **TUI dashboard** inspired by claude-squad / vibe-kanban /
bernstein, but deliberately minimal: it should feel like **NERDTree** (familiar
keys, no new mental model) and stay **tmux-backed** (each worktree's Claude keeps
running in its own tmux window; the TUI is a dashboard over those windows).

The full v2 is larger than one cycle, so it is **phased**:

- **Phase 1 (this cycle):** Core two-pane dashboard — left NERDTree-style worktree
  tree grouped by repo; right underline tabs Agent (`tmux capture-pane`) + Diff
  (`git diff`); navigate, attach, filter, quit. **The TUI fully replaces the fzf
  popup**: it reaches parity with `dg wt j` by migrating attach (with auto-repair on
  a missing window), **delete** (with double-confirm), and **repair** into the
  dashboard, then **removes the fzf jump flow and the `dg wt j`/`jump` subcommand**.
- **Phase 2 (future):** Inline **new worktree** creation, **open in nvim**.
- **Phase 3 (future):** Completion notifications via Claude Code Stop hook writing a
  marker file the TUI watches, plus optional desktop notification.
  > **Superseded (2026-07-28).** Phase 3 is now planned in
  > [2026-07-28-agent-activity-notifications.md](2026-07-28-agent-activity-notifications.md).
  > The marker-file sketch above was reconsidered and rejected — see
  > [ADR-0005](../../decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md),
  > which stores the state in a tmux pane option instead, and covers both AI coders
  > rather than Claude Code alone.

Design reference: the wireframe set the maintainer approved selects **Layout A
(classic two-pane)**, **tree grouping T1 (by repo)**, **right pane R1 (underline
tabs)**, and **keybinding model K1 (persistent hint bar)**.

---

## 2. Engineer Context

**Relevant existing files:**

| File                                    | Role                                                                  |
| --------------------------------------- | --------------------------------------------------------------------- |
| `cmd/worktree.go`                       | Cobra subcommands; add `dg wt ui` here, register in `init()`          |
| `internal/tooling/worktree/worktree.go` | `WorktreeManager`; `List()` returns `[]WorktreeStatus`                |
| `internal/apps/tmux/tmux.go`            | tmux primitives; `WindowSession`, `SwitchToWindow` exist; add capture |
| `internal/apps/git/git.go`              | git helpers; `IsWorktreeDirty` exists; add `Diff`/`DiffStat`          |
| `internal/commands/base.go:191`         | `BaseCommand.ExecCommand(CommandParams) (stdout, stderr, error)`      |
| `internal/commands/mock.go:204`         | `MockBaseCommand.ExecCommand` for tests                               |
| `configs/tmux/tmux.conf:113`            | `prefix + u` popup binding (currently `devgeta wt j`)                 |
| `go.mod`                                | Add Bubble Tea + Lipgloss deps                                        |

**Key existing types/functions to reuse (do NOT reimplement — see the §5 guardrails):**

- `worktree.WorktreeStatus{ Name, Path, Branch, TmuxWindow, WindowActive, Repo }`
- `(*WorktreeManager).List() ([]WorktreeStatus, error)` — walks the centralized
  base path, two levels deep, across all repos.
- `worktree.GetWindowName(name) string` — `wt-<flat-name>`.
- `(*Tmux).WindowSession(window) (session string, ok bool)` — finds the session
  hosting a window across all sessions.
- `(*Tmux).SwitchToWindow(session, window) error` — switch-client + select-window.
- `(*Git).IsWorktreeDirty(path) (bool, error)` — `git -C path status --porcelain`.
- `(*WorktreeManager).Remove(name, force) error` — **business** delete logic (kill
  window, remove worktree, delete branch); shared with `dg wt rm`/`prune`.
  ⚠️ **Cross-package gap:** the repo-scoped core, `removeByRepo(repoSlug, name, force)`,
  is **unexported**, and `Remove(name, force)` disambiguates by _searching_ — for a
  worktree name that exists in two repos, `findRepoForWorktree` returns `""` and the
  delete fails. The TUI knows the repo, so **Step 0 must add an exported
  `RemoveInRepo(repo, name, force)`** (thin wrapper over `removeByRepo`) and the TUI
  must call that, not `Remove`.
- `(*WorktreeManager).Repair(name, coder) error` — recreate window + launch AI; shared
  with `dg wt repair`. ⚠️ **Same gap:** it resolves the repo via `repoSlugForWorktree(name)`
  (also ambiguous for duplicate names). **Step 0 must add `RepairInRepo(repo, name, coder)`**;
  the TUI repair + auto-repair call that.
- `worktree.ResolveAICoder(alias)` — exported, maps alias → coder. ⚠️ But the
  **precedence** logic (`--ai` flag → `DEVGETA_WORKTREE_AI` → `global_config` → `opencode`)
  lives in `cmd.resolveAIAlias`, which is **unexported in package `cmd`** and unreachable
  from the TUI (and `cmd` already imports the TUI → importing back would cycle).
  **Step 0 must hoist that precedence into a shared exported resolver** (in
  `internal/tooling/worktree`) that both `cmd` and the TUI call — reuse, not duplicate.

**fzf jump code to REMOVE (becomes dead once `dg wt j` is gone — Step 8b):**

- `internal/tooling/worktree/worktree.go`: `Jump`, `confirmAndRemove`, `buildConfirmRows`,
  `formatJumpRow`, `parseJumpRow`, `parseRepoAndName`, `parseJumpOutput`,
  `runFzfWithExpect`, `execFzf`; the `fzfRun` field + its `New()` assignment; and the
  `pendingDelete` field + `pendingDeleteInfo` type.
- `cmd/worktree.go`: `worktreeJumpCmd`, its `AddCommand` registration, and the
  `dg wt j` example lines in `worktreeCmd.Long`.
- **Keep (shared, NOT dead):** `SelectWorktreeInteractively` + the `Fzf` field +
  `Fzf.SelectFromList` (used by `dg wt rm`), and all of `Remove`/`removeByRepo`/`Repair`.
- The TUI must preserve the existing **double-confirm + force** delete semantics that
  `confirmAndRemove` documented (a running AI coder makes a worktree almost always
  dirty, so the two-key confirm forces through the dirty guard).

**Stale user-facing string to UPDATE (not delete — it's in a kept function):**

- `internal/tooling/worktree/worktree.go:135` — `Create`'s "already exists" error tells
  users to run `` `dg wt jump %s` ``. After the command is removed this is stale; change
  it to point at `` `dg wt ui` ``. (Step 8b grep gate must catch strings, not just symbols.)

**Devgeta repo patterns to follow (verified from the codebase — match these, don't invent):**

- **Construction:** every app/coordinator is a struct built by a package-level `New()`.
  `tmux.New()` and `git.New()` each return a struct holding `Cmd cmd.Command` +
  `Base cmd.BaseCommandExecutor` (from `cmd.NewCommand()` / `cmd.NewBaseCommand()`).
  `worktree.New()` **already wires its own `.Git`, `.Tmux`, `.Base`** — so the TUI
  should build **one** `worktree.New()` and reuse `mgr.Git` / `mgr.Tmux`, **not**
  call `tmux.New()`/`git.New()` again (see the Step 8 fix). One manager, shared deps.
- **Low-level command exec (mirror exactly for `CapturePane`/`Diff`/`DiffStat`):**
  ```go
  execCommand := cmd.CommandParams{Command: constants.Tmux, Args: []string{...}}
  stdout, stderr, err := t.Base.ExecCommand(execCommand)
  if err != nil { return ..., fmt.Errorf("…: %w", err) } // surface stderr when set
  ```
  This is exactly how `WindowSession` and `IsWorktreeDirty` are written. Higher-level
  tmux verbs use the `t.ExecuteCommand(args...)` wrapper (see `SwitchToWindow`).
- **Constants, not string literals:** command names come from `pkg/constants`
  (`constants.Tmux`, `constants.Git`). Never hardcode `"tmux"`/`"git"`.
- **Errors:** wrap with `fmt.Errorf("context: %w", err)` and return up the stack.
  Cobra `RunE` returns the error (don't `os.Exit` in command logic);
  `utils.MaybeExitWithError` is the top-level boundary. User-facing text goes through
  `utils.PrintSuccess/PrintInfo/PrintError` — the TUI's inline `status` line should
  read in that same actionable tone.
- **Paths:** never hardcode `~/.local/share`; the worktree path comes from
  `WorktreeStatus.Path` (already built via `paths.Paths.*` + `filepath.Join`). The TUI
  consumes `Path`/`Name`/`Repo` from `List()`; it does not compute paths itself.
- **Cobra command shape:** package-level `var worktreeUICmd = &cobra.Command{Use,
Aliases, Short, Long, Args, RunE}`, registered in `init()` via
  `worktreeCmd.AddCommand(...)` — identical to the other `wt` subcommands.
- **Injected-seam precedent:** the existing `worktree_test.go` already injects a
  `fzfRun` func to avoid real fzf. The model's `captureFn`/`diffFn`/`attachFn`/
  `removeFn`/`repairFn` seams follow that **established** precedent — not a new idea.

**Testing patterns (CLAUDE.md §6, docs/guides/testing-patterns.md):**

- `func init() { testutil.InitLogger() }` at top of each test file.
- Use `testutil.NewMockApp()` → `.Cmd` / `.Base`; assert via
  `mockApp.Base.GetLastExecCommandCall()` and `GetExecCommandCallCount()`;
  **never execute real commands** in tests.
- `testutil.VerifyNoRealCommands(t, mockApp.Base)` where applicable.
- For the TUI model, unit-test the pure `Update`/state transitions with synthetic
  `tea.KeyPressMsg`/mouse msgs and injected I/O seams — **do not** spawn a real
  `tea.Program` or touch tmux/git.

**Commands:**

- Build: `go build -o devgeta main.go`
- Test: `go test ./...`
- Lint: `make lint` (`go fmt ./...` + `go vet ./...`)

---

## 3. Objective

Ship `dg wt ui`: a full-screen, tmux-backed Bubble Tea dashboard with a
NERDTree-style worktree tree (grouped by repo) on the left and Agent/Diff tabs
on the right, supporting `j/k` move, `h/l`+`z` fold, `Enter` attach (auto-repairing
a missing window), `d` delete (double-confirm), `r` repair, `Tab` switch tab,
`/` filter, and `q` quit. It **replaces and removes** the `prefix + u` fzf popup and
the `dg wt j`/`jump` subcommand, migrating their jump/delete/repair behavior into the
dashboard. All other `dg wt` subcommands (`create`, `list`, `remove`, `repair`,
`prune`) are untouched.

---

## 4. Scope Boundary

### In Scope

- [x] **API prep (Step 0):** export repo-scoped `RemoveInRepo`/`RepairInRepo` and a
      shared `ResolveAIAlias` in `internal/tooling/worktree` so the TUI can reuse the
      business logic across the package boundary (the unexported `removeByRepo`/
      `resolveAIAlias` are unreachable, and name-only `Remove`/`Repair` are ambiguous
      across repos).
- [x] Add `charm.land/bubbletea/v2` + `charm.land/lipgloss/v2` + `charm.land/x/ansi` to
      `go.mod`/`go.sum`.
- [x] New `internal/tui/worktree/` package: Bubble Tea model, tree rendering,
      styles, and a `Run()` entrypoint.
- [x] Left pane: worktree tree grouped by repo (T1), status glyphs
      (`●` running/has-window, `○` no session) and dirty indicator `+A/-R` from
      `DiffStat` (e.g., `+12/-5`). Falls back to glyph-only if `DiffStat` fails.
- [x] Left pane is **mouse-resizable**: drag the vertical divider left/right to
      change `leftPaneWidth`; clamp between `minLeftPaneWidth` (e.g. 20 cols) and
      `maxLeftPaneWidth` (60% of terminal width). **Always starts at
      `minLeftPaneWidth`** so the right (Agent/Diff) pane is maximized by default.
- [x] Tree nav: `j/k` move cursor over **worktree rows only** (skip repo headers),
      `h/l` collapse/expand current repo node, `z` toggle collapse-all, wrap-around
      within visible worktree rows.
- [x] Right pane: underline tabs `Agent` and `Diff`; `Tab` switches; Agent shows
      `tmux capture-pane` of the selected worktree's window **pane 0** (the agent's pane),
      ANSI-truncated to the pane width; Diff shows colored `git diff HEAD` **plus
      untracked files**, in a scrollable viewport. (See Steps 2/3/6 for the rendering and
      untracked-file handling that make these correct.)
- [x] `Enter` = attach (parity with fzf Enter): when inside tmux, `SwitchToWindow` to
      the worktree's window and quit; **if the window is missing, auto-repair it**
      (`Repair` → recreate window + launch the resolved AI coder) then switch — matching
      the current fzf jump behavior. When **not** inside tmux, show an inline status
      message (no crash; read-only browsing still works).
- [x] `d` = delete (parity with fzf `ctrl-d`): **double-confirm** — first `d` arms the
      selected row (highlight + inline "press d again to delete"), second `d` calls the
      shared `Remove`/`removeByRepo` (force) and drops the row from the tree. Any other
      key cancels the pending delete. Reuses existing business logic, not a new path.
- [x] `r` = repair (parity with fzf `ctrl-r`): call `Repair(name, coder)` for the
      selected worktree, then refresh its row/glyph. Resolve the coder the same way
      `dg wt create`/`repair` do.
- [x] **Remove the fzf jump flow** (Step 8b): delete `Jump`, `confirmAndRemove`,
      `buildConfirmRows`, the jump row encode/decode helpers, `runFzfWithExpect`,
      `execFzf`, the `fzfRun` field, and `pendingDelete`/`pendingDeleteInfo` from
      `worktree.go`; delete `worktreeJumpCmd` + its registration + the `dg wt j` doc
      lines from `cmd/worktree.go`; and delete the now-orphaned fzf-jump unit tests.
      Keep `SelectWorktreeInteractively`/`Fzf` (still used by `dg wt rm`).
- [x] `/` filter: type to filter worktrees by `repo/name` substring; `Esc` **clears
      filter and exits** filter mode; `Enter` **keeps filter and exits** filter mode.
- [x] Persistent hint bar (K1) at the bottom showing the active keys.
- [x] Selected-row refresh (**must-have**): re-capture Agent on a timer tick (~1.5s)
      and recompute Diff on selection change. Pause refresh while the user is actively
      navigating. The **hash-based blink reduction** refinement — diffing captured
      content and skipping the viewport update when it is unchanged — is a
      **nice-to-have** (see Priority Split); without it the basic refresh still works,
      just with possible flicker.
- [x] `tmux.CapturePane(session, window) (string, error)` helper + test.
- [x] `git.Diff(path) (string, error)` and `git.DiffStat(path) (files, added, removed int, error)` helpers + tests.
- [x] `dg wt ui` Cobra subcommand (aliases `dash`, `dashboard`) registered in `cmd/worktree.go`.
- [x] Update `configs/tmux/tmux.conf:113` to launch `devgeta wt ui` in the popup.
- [x] Unit tests for the model's `Update` transitions (nav, fold, tab switch, filter).

### Explicitly Out of Scope

- Inline **new worktree** creation (Phase 2). _(Delete and repair are now in scope —
  migrated from fzf — but creating a worktree from the TUI is still Phase 2.)_
- **Open in nvim** (Phase 2).
- **IDE tab** showing file tree or file preview (Phase 2+).
- **Multi-pane awareness** — Phase 1 captures only pane 0 (the agent's pane) of a
  worktree's window; showing all panes or a pane selector is Phase 2+.
- **Multiple agents per worktree** — e.g., Claude + Aider in different panes (Phase 2+).
- **Stop-hook completion notifications** + desktop notify + `◆ needs review` glyph
  (Phase 3).
- Alternate layouts/groupings (B–E, T2/T3, R2/R3), command palette, which-key popup.
- Per-hunk navigation / extra syntax highlighting beyond git's own `--color` output
  (Phase 1 renders colored `git diff HEAD` + an untracked-files list in a scrollable
  viewport; rendering untracked file _contents_ is also Phase 2).
- **Outside-tmux path output** — old `dg wt j` printed the worktree path on Enter when
  run outside tmux (script-friendly); the TUI does not. A dedicated `dg wt path <name>`
  is logged for ROADMAP. See the migration note (Step 11) — this is a documented break.
- PR/merge status via `gh` (stretch, later).
- Removing or changing **other** `dg wt` subcommands (`create`/`list`/`remove`/`repair`/
  `prune`). _(Note: `dg wt j`/`jump` **is** being removed this cycle — its behavior is
  migrated into the TUI; this is an intentional, documented removal per CLAUDE.md §10.)_
- **Live worktree list refresh** — list is loaded once at startup; requires quit +
  relaunch to see new worktrees. _(Exception: a row deleted via `d` is dropped from the
  in-memory tree immediately so it disappears without relaunch.)_

**Scope is locked.** Anything discovered beyond this is logged for a later phase.

### Priority Split (Phase 1)

| Must-have (ship-blocking)              | Nice-to-have (defer if time runs out)   |
| -------------------------------------- | --------------------------------------- |
| Two-pane layout renders                | Hash-based blink reduction              |
| Tree grouped by repo                   | `z` toggle-all-collapsed                |
| `j/k` navigation (skip repo headers)   | `+A/-R` counts (fallback to glyph only) |
| `Tab` switches Agent/Diff              | Diff tab content (can show placeholder) |
| `Enter` attach (+ auto-repair if gone) | `/` filter mode                         |
| `d` delete (double-confirm)            | Hint bar styling                        |
| `r` repair                             |                                         |
| Remove fzf jump flow + `dg wt j`       |                                         |
| `q` quit                               |                                         |
| `CapturePane` + basic Agent view       |                                         |
| Outside-tmux graceful degradation      |                                         |
| Mouse-drag divider resizes left pane   |                                         |
| Left pane starts at `minLeftPaneWidth` |                                         |

---

## 5. Implementation Plan

> ### ⚠️ READ FIRST — Implementer guardrails (reuse existing logic; do not break patterns)
>
> **This cycle is a thin TUI layer over logic that already exists. Treat the existing
> code as the source of truth and call into it — do NOT reimplement, fork, or
> copy-paste worktree/tmux/git behavior into the TUI.** Breaking the established
> patterns is a review-blocking failure, not a style nit.
>
> 1. **Reuse the business logic verbatim.** Attach → `Tmux.WindowSession` +
>    `SwitchToWindow`. Delete → `WorktreeManager.RemoveInRepo` (new exported wrapper —
>    see Step 0; do **not** reach for unexported `removeByRepo`). Repair (and
>    auto-repair on attach) → `WorktreeManager.RepairInRepo` (Step 0). Listing →
>    `WorktreeManager.List`. Coder resolution → the **shared** exported resolver added
>    in Step 0 (same flag → `DEVGETA_WORKTREE_AI` → `global_config` → `opencode`
>    precedence), reused by both `cmd` and the TUI — never duplicated. The TUI holds
>    **UI state only**; every side-effect routes through an existing manager method.
> 2. **New helpers mirror existing siblings.** `Tmux.CapturePane` must mirror
>    `WindowSession`'s `ExecCommand` + stderr-surfacing shape; `Git.Diff`/`DiffStat`
>    must mirror `IsWorktreeDirty`. Same receiver style, same error wrapping, same
>    `constants.*` usage — do not invent a new command-exec pattern.
> 3. **Preserve behavior on migration.** The migrated delete keeps the documented
>    double-confirm-plus-`force=true` semantics from `confirmAndRemove`; the migrated
>    attach keeps fzf's auto-repair-then-jump behavior. **Parity first, deletion second**
>    (Step 8b removes the fzf code only after the TUI matches it).
> 4. **Follow CLAUDE.md conventions** (this is non-negotiable): error handling
>    (`docs/guides/error-handling.md`, sentinel errors not free-form strings), logger
>    over `fmt.Print`, and the testing rules — `testutil` mocks, **never execute real
>    commands**, `func init() { testutil.InitLogger() }`, injected I/O seams for the
>    model (no real `tea.Program`/tmux/git in tests).
> 5. **When in doubt, grep for an existing helper before writing a new one.** If you
>    find yourself about to re-derive window names, parse worktree paths, or shell out
>    to tmux/git directly from the TUI package, stop — there is almost certainly an
>    existing method (`GetWindowName`, `WorktreeStatus`, the manager/tmux/git receivers).

### File Changes

| Action | File Path                                      | Description                                                                                                                                                                                                                                                                                                                                |
| ------ | ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Modify | `go.mod` / `go.sum`                            | Add `bubbletea/v2@v2.0.7` + `lipgloss/v2@v2.0.3` (pinned, no `@latest`); promote `charmbracelet/x/ansi` to a direct dep (already transitive via lipgloss v2) for ANSI-safe truncation                                                                                                                                                      |
| Create | `internal/tui/worktree/model.go`               | Bubble Tea `Model`, `Init`/`Update`/`View` (v2 declarative style)                                                                                                                                                                                                                                                                          |
| Create | `internal/tui/worktree/tree.go`                | Tree node type, build-from-statuses, flatten-to-visible, render rows                                                                                                                                                                                                                                                                       |
| Create | `internal/tui/worktree/styles.go`              | Lipgloss styles (panes, tabs, selection, status glyphs, hint bar)                                                                                                                                                                                                                                                                          |
| Create | `internal/tui/worktree/run.go`                 | `Run()` — wires `WorktreeManager`/`Tmux`/`Git`, runs `tea.Program`                                                                                                                                                                                                                                                                         |
| Create | `internal/tui/worktree/model_test.go`          | Unit tests for `Update` transitions with synthetic msgs                                                                                                                                                                                                                                                                                    |
| Modify | `internal/apps/tmux/tmux.go` (after ~line 244) | Add `CapturePane(session, window) (string, error)`                                                                                                                                                                                                                                                                                         |
| Modify | `internal/apps/tmux/tmux_test.go`              | Test `CapturePane` with mock                                                                                                                                                                                                                                                                                                               |
| Modify | `internal/apps/git/git.go` (after ~line 356)   | Add `Diff(path)` (= `diff --color=always HEAD` + untracked `??` from `status --porcelain`) and `DiffStat(path)` (numstat + untracked file count)                                                                                                                                                                                           |
| Modify | `internal/apps/git/git_test.go`                | Test `Diff`/`DiffStat` with mock (incl. untracked-file case)                                                                                                                                                                                                                                                                               |
| Modify | `cmd/worktree.go`                              | Add `worktreeUICmd` + register; rewire `resolveAIAlias` to call the shared Step-0 resolver; **remove** `worktreeJumpCmd`, its registration, and the `dg wt j` doc lines                                                                                                                                                                    |
| Modify | `internal/tooling/worktree/worktree.go`        | **Step 0:** add exported `RemoveInRepo`/`RepairInRepo` wrappers + shared AI-alias resolver; fix the stale `dg wt jump` string at :135 → `dg wt ui`. **Step 8b:** remove `Jump`, `confirmAndRemove`, `buildConfirmRows`, jump row encode/decode helpers, `runFzfWithExpect`, `execFzf`, `fzfRun` field, `pendingDelete`/`pendingDeleteInfo` |
| Modify | `internal/tooling/worktree/worktree_test.go`   | **Remove** orphaned fzf-jump tests (`TestFormatJumpRow`, `TestParseJumpRow`, `TestParseJumpOutput*`, jump/`confirmAndRemove` fzfCall harness tests)                                                                                                                                                                                        |
| Modify | `README.md`                                    | Document `dg wt ui` (and `dash`/`dashboard` aliases); **remove `dg wt j` references** — user-facing per §12                                                                                                                                                                                                                                |
| Modify | `docs/spec.md`                                 | Add `dg wt ui` dashboard to the worktree feature spec; **remove the `dg wt j` fzf-jump description**                                                                                                                                                                                                                                       |
| Modify | `configs/tmux/tmux.conf:113`                   | `devgeta wt j` → `devgeta wt ui`                                                                                                                                                                                                                                                                                                           |

### Data flow

```
Run() → WorktreeManager.List() ──► []WorktreeStatus
                                     │
        build tree (group by repo) ─┘
                                     │ on selection / tick
        selected worktree ──► Tmux.CapturePane(session, window)  → Agent text (pane 0, ANSI-truncated)
                          └─► Git.Diff(path) / Git.DiffStat(path) → Diff text (HEAD + untracked) + ±N
        Enter ──► Tmux.WindowSession(window) ─ ok ─► SwitchToWindow → quit
                                            └ gone ► RepairInRepo(repo, name, coder) → SwitchToWindow → quit
        d (2×) ──► WorktreeManager.RemoveInRepo(repo, name, force) → drop row from tree
        r ──────► WorktreeManager.RepairInRepo(repo, name, coder) → refresh row glyph
```

**Abstraction note:** The TUI presents each worktree as a single item, but the
underlying tmux window may contain multiple panes (e.g., Claude in pane 0, a shell
in pane 1). **Phase 1 captures the agent's pane explicitly by index: target
`session:window.0`** — the AI coder is launched into the window's first pane at
creation, so `.0` reliably shows the agent even if the user later splits the window
and focuses a shell. (Capturing the _active_ pane would show whatever the user last
clicked.) Multi-pane awareness / a pane selector is deferred to Phase 2+.

### Step-by-Step

#### Step 0: API prep — export repo-scoped actions + a shared AI-alias resolver

> **Why first:** the TUI lives in `internal/tui/worktree` and cannot reach the
> unexported `removeByRepo`/`resolveAIAlias`, and the exported `Remove`/`Repair`
> resolve the repo by _searching_ (ambiguous when a worktree name exists in two repos).
> These seams must exist before Steps 7/7b/7c, or implementation stalls.

- In `internal/tooling/worktree`, add exported, **repo-scoped** wrappers that delegate
  to the existing unexported logic (no behavior change):
  ```go
  // RemoveInRepo deletes a worktree disambiguated by repo slug (the TUI always knows it).
  func (w *WorktreeManager) RemoveInRepo(repoSlug, name string, force bool) error {
      return w.removeByRepo(repoSlug, name, force)
  }
  // RepairInRepo repairs a worktree in a specific repo.
  func (w *WorktreeManager) RepairInRepo(repoSlug, name string, coder AICoder) error { ... }
  ```
  `RepairInRepo` should factor the repo-resolution out of the existing `Repair` so both
  share one window-ensuring core (reuse, don't fork). `WorktreeStatus.Repo` feeds the
  `repoSlug` argument.
- Hoist the AI-alias precedence out of `cmd.resolveAIAlias` into a shared exported
  resolver so there is **one** source of truth:
  ```go
  // ResolveAIAlias applies precedence: flag → DEVGETA_WORKTREE_AI → global_config → opencode.
  func ResolveAIAlias(flag string, gc *config.GlobalConfig) string { ... }
  ```
  Then `cmd.resolveAIAlias` becomes a thin call to `worktree.ResolveAIAlias` (keeps the
  existing call sites working; the TUI calls the same function).
- Verify: `go build ./...` clean; existing `cmd`/`worktree` tests still pass; add unit
  tests for `RemoveInRepo`/`RepairInRepo` with **two repos sharing a worktree name** to
  prove disambiguation (mock base, no real commands).

#### Step 1: Add dependencies

- Run `go get charm.land/bubbletea/v2@v2.0.7 charm.land/lipgloss/v2@v2.0.3`.
  Both versions are pinned exactly (no `@latest`) for reproducible builds — bump
  deliberately in a future cycle. (Bubble Tea v2 uses a declarative View struct +
  Cursed Renderer; v2.0.7 also includes a mouse-release-correctness fix relevant to
  the drag-resize divider. Lipgloss v2 is pure, no I/O conflicts.)
- Then `go get charm.land/x/ansi@latest` to promote the already-transitive ANSI helper
  to a direct dep; it provides `ansi.Truncate`/`ansi.Hardwrap` for ANSI-safe right-pane
  rendering (Step 6). Pin the resolved version in `go.mod`.
- Run `go mod tidy`.
- Verify: `go build ./...` succeeds; `go.mod` lists all three modules at pinned versions.

#### Step 1b: Verified Bubble Tea v2 API reference (confirm against installed version)

The v2 API differs from v1 in ways that bite if you assume v1. The following are
**verified against the official v2 upgrade guide**; still run `go doc charm.land/bubbletea/v2`
once to confirm the installed v2.0.7 matches, but Steps 6–8 are written to these:

- **View is declarative.** `View() tea.View`; build with `v := tea.NewView(content)`,
  then set fields and `return v`. Terminal features are View fields, **not** program
  options — `tea.WithAltScreen()` is **removed**. Set `v.AltScreen = true`.
- **Mouse is a View field, not a command/option.** Set `v.MouseMode = tea.MouseModeCellMotion`
  (values: `MouseModeNone` / `MouseModeCellMotion` / `MouseModeAllMotion`). There is
  **no** `tea.EnableMouseCellMotion`.
- **Key messages:** match `tea.KeyPressMsg` (not `tea.KeyMsg`). `msg.String()` still
  works (`"q"`, `"ctrl+c"`, `"enter"`, `"tab"`, `"/"`, `"d"`, `"r"`); note the space
  key is now `"space"`, not `" "`.
- **Mouse messages are split:** `tea.MouseClickMsg`, `tea.MouseReleaseMsg`,
  `tea.MouseMotionMsg`, `tea.MouseWheelMsg`. Get coords/button via `m := msg.Mouse()`
  → `m.X`, `m.Y`, `m.Button`; left button is `tea.MouseLeft`.
- **Program:** `tea.NewProgram(model)` + `.Run()` are unchanged.
- Verify: `go doc` confirms these names; fix any delta inline before coding the model.

#### Step 2: Add `tmux.CapturePane`

- In `internal/apps/tmux/tmux.go`, add:
  ```go
  // CapturePane returns the visible content of a window's pane 0 (where the AI coder
  // runs), including ANSI color escapes (-e). target is "session:window.0".
  func (t *Tmux) CapturePane(session, window string) (string, error) {
      target := session + ":" + window + ".0" // pane 0 = the agent's pane (see abstraction note)
      execCommand := cmd.CommandParams{
          Command: constants.Tmux,
          Args:    []string{"capture-pane", "-p", "-e", "-t", target},
      }
      stdout, stderr, err := t.Base.ExecCommand(execCommand)
      if err != nil {
          if stderr != "" {
              return "", fmt.Errorf("capture-pane: %s", stderr)
          }
          return "", fmt.Errorf("failed to capture pane %s: %w", target, err)
      }
      return stdout, nil
  }
  ```
- Verify: compiles; mirrors `WindowSession`'s stdout/stderr handling.
- **Note for the caller (Step 6):** when the window has closed (agent finished), tmux
  returns a `can't find pane`/`no such pane` error on stderr. `CapturePane` surfaces it
  as-is; the model converts it into an `agentOfflineMsg` (placeholder, no spam) rather
  than logging on every 1.5s tick.

#### Step 3: Add `git.Diff` and `git.DiffStat`

> **Untracked files matter:** AI coders frequently create brand-new files. Plain
> `git diff` (working tree vs index) and even `git diff HEAD` **ignore untracked files**,
> so the agent's new files would be invisible in the Diff tab. Both helpers must account
> for untracked entries. The TUI is read-only, so do **not** mutate state (`git add -N`);
> read untracked paths from `git status --porcelain` instead.

- Add `Diff(path) (string, error)` (reuse the `ExecCommand` + stderr-surfacing pattern
  from `IsWorktreeDirty`):
  - run `git -C path diff --color=always HEAD` for tracked changes (this catches both
    **staged and unstaged** edits; `--color=always` gives a consistent highlighted diff
    regardless of the user's `color.ui` config — the TUI renders ANSI anyway).
  - fall back to `git -C path diff --color=always` if `HEAD` errors (repo with no commits).
  - append an `Untracked files:` section listing the `??` paths from
    `git -C path status --porcelain` so agent-created files are visible (Phase 1 lists
    them; rendering their full contents is a Phase 2 nicety).
- Add `DiffStat(path) (files, added, removed int, err error)`:
  - parse `git -C path diff --numstat HEAD` and sum columns (numstat lines:
    `<added>\t<removed>\t<file>`; binary files report `-`/`-` → 0 lines but still
    increment `files`).
  - add the count of `??` entries from `git -C path status --porcelain` to `files` so
    the `+A/-R`/file count reflects new files too (their line counts are best-effort 0
    in Phase 1 — document the limitation).
- Verify: compiles; tested in Step 10 incl. an untracked-file case.

#### Step 4: Tree model (`tree.go`)

- Define a flat, render-friendly representation:

  ```go
  type rowKind int
  const ( rowRepo rowKind = iota; rowWorktree )

  type row struct {
      kind     rowKind
      repo     string
      status   worktree.WorktreeStatus // zero for repo rows
      depth    int
  }
  ```

- `buildRows(statuses []worktree.WorktreeStatus, collapsed map[string]bool, filter string) []row`:
  group by `Repo` (stable alpha sort), emit a repo header row then its worktree
  rows unless `collapsed[repo]`; apply case-insensitive `repo/name` substring filter
  (a repo row is kept if any child matches).
- `worktreeIndices(rows []row) []int`: return indices of worktree rows only (for
  cursor navigation that skips repo headers).
- `glyph(s worktree.WorktreeStatus) string`: `●` when `WindowActive`, else `○`.
- Verify: pure functions, covered by Step 9 tests.

#### Step 5: Styles (`styles.go`)

- Lipgloss styles: left/right pane borders, active/inactive tab, selected row
  (reverse/﻿highlight), repo header (bold), dirty `+A/-R` (green/red), dim hint bar.
  Keep a single `Styles` struct constructed once.
- Verify: compiles.

#### Step 6: Model core (`model.go`)

- `Model` fields: `mgr *worktree.WorktreeManager`, `tmux *tmux.Tmux`, `git *git.Git`,
  `statuses []WorktreeStatus`, `rows []row`, `cursor int`, `collapsed map[string]bool`,
  `allCollapsed bool`, `activeTab` (agent|diff), `agentContent string`, `agentHash string`
  (for diff-based refresh), `diff string`, `diffAdded/Removed/Files int`, `filtering bool`,
  `filter string`, `status string` (inline message), `width/height int`, `styles Styles`,
  `lastNavTime time.Time` (to pause refresh during active navigation),
  `leftPaneWidth int` (current divider column; starts at `minLeftPaneWidth`),
  `minLeftPaneWidth int` (constant, e.g. 20), `maxLeftPaneWidthPct float64` (e.g. 0.60),
  `dragging bool`, `dragStartX int` (mouse X at button-down on the divider column),
  `pendingDelete string` (`repo/name` of the row armed for delete, empty if none),
  `agentOffline bool` (selected window closed — show placeholder, suppress capture),
  `diffScroll int` (vertical scroll offset for the Diff viewport),
  and **injected I/O seams** (see Step 9): `captureFn`, `diffFn`, `attachFn`,
  `removeFn`, `repairFn` — defaulting to the real `tmux`/`git`/`mgr` calls but
  overridable in tests.
- `Init()` returns a batch: initial `loadCmd` + `tickCmd` (1.5s). `leftPaneWidth` is
  initialized to `minLeftPaneWidth`. (Mouse is **not** enabled here — in v2 it is a
  declarative View field; set `v.MouseMode = tea.MouseModeCellMotion` in `View()`.)
- Messages: `statusesMsg`, `agentContentMsg`, `agentOfflineMsg`, `diffMsg`, `tickMsg`,
  plus `tea.KeyPressMsg`, the mouse messages (`tea.MouseClickMsg`, `tea.MouseMotionMsg`,
  `tea.MouseReleaseMsg`), and `tea.WindowSizeMsg`.
- **All I/O runs as async `tea.Cmd`s** (`captureAgentCmd`, `computeDiffCmd`,
  `attachCmd`, …) returned from `Update` — never call tmux/git inline in `Update`, or a
  large `git diff` would block the event loop and freeze the UI. `computeDiffCmd` should
  also cap very large diffs (e.g. truncate to N KB / first M lines) before returning,
  to keep rendering bounded.
- `Update` handling:
  - `WindowSizeMsg`: store dims, recompute pane widths. **Clamp safely** — the upper
    bound can fall below `minLeftPaneWidth` on a narrow terminal (e.g. width 30 →
    `0.60*30 = 18 < 20`), and a naive `clamp(v, 20, 18)` yields an inverted range /
    negative right-pane width. Use
    `maxLeft := max(minLeftPaneWidth, int(float64(width)*maxLeftPaneWidthPct))`,
    then `leftPaneWidth = min(leftPaneWidth, maxLeft)`, and derive
    `rightPaneWidth = max(0, width - leftPaneWidth - dividerWidth)`; if the terminal is
    too small for both panes, render a single-pane fallback rather than panicking.
  - mouse (v2 split messages; get coords via `m := msg.Mouse()` → `m.X`/`m.Y`/`m.Button`):
    - `tea.MouseClickMsg` with `m.Button == tea.MouseLeft` at column `m.X == leftPaneWidth`
      (the divider) → set `dragging = true`, `dragStartX = m.X`.
    - `tea.MouseMotionMsg` while `dragging` → `leftPaneWidth = clamp(m.X, minLeftPaneWidth, maxLeft)`
      using the same safe `maxLeft` as `WindowSizeMsg`; recompute `rightPaneWidth`.
    - `tea.MouseReleaseMsg` → `dragging = false`.
    - All other mouse events are ignored.
  - `tickMsg`: if `agentOffline` for the selection, skip capture (just reschedule); else
    if `time.Since(lastNavTime) > 500ms`, re-issue `captureAgentCmd(selected)` and
    reschedule; otherwise just reschedule (skip capture during active nav).
  - `agentContentMsg`: hash the content; if the hash differs from `agentHash`, update
    `agentContent`/`agentHash` and clear `agentOffline`; if identical, skip re-render
    (prevents blinking).
  - `agentOfflineMsg` (capture failed because the window/pane is gone): set
    `agentOffline = true`, render a dim `⟂ window offline — press r to repair` placeholder
    in the Agent pane, and **stop re-issuing capture** until the selection changes or a
    repair succeeds. No error log spam on the tick.
  - key handling matches `tea.KeyPressMsg` via `msg.String()` (e.g. `"q"`, `"ctrl+c"`,
    `"enter"`, `"tab"`, `"esc"`, `"/"`, `"d"`, `"r"`; space is `"space"`, not `" "`).
  - filtering mode on: printable runes append to `filter`, `"backspace"` edits,
    `Esc` **clears filter and exits**, `Enter` **keeps filter and exits**.
  - normal mode keys: `j/k` move cursor, **strictly skipping repo header rows**
    (cursor index always points to a worktree row), `h`/`l` collapse/expand the repo
    of the current row, `z` toggle `allCollapsed`, `Tab` toggle `activeTab`,
    `/` enter filtering, `Enter` attach (see Step 7), `d` delete (see Step 7b),
    `r` repair (see Step 7c), `q`/`ctrl+c` quit.
  - **right-pane scroll (Diff tab):** the Diff renders into a scrollable viewport.
    `ctrl+d`/`ctrl+u` (or `PgDn`/`PgUp`) move `diffScroll` by a page, clamped to the
    content height; `diffScroll` resets to 0 on selection change or tab switch. `j/k`
    stay bound to the **tree**, not the viewport (NERDTree feel). The Agent tab is a
    single visible-pane snapshot (no scrollback in Phase 1), so it does not scroll.
  - **delete arming:** any key other than a second `d` on the same row clears
    `pendingDelete` (so navigating away or pressing another action cancels the
    pending delete) — mirrors the fzf double-confirm cancel behavior.
  - on cursor move or filter change → recompute `rows`, update `lastNavTime`, clear
    `pendingDelete`, reset `diffScroll`, clear `agentOffline`, issue `captureAgentCmd`
    and `computeDiffCmd` for the newly selected worktree.
- `View() tea.View`: build the left tree and the right tabbed pane.
  - **ANSI-safe right pane (critical):** the Agent capture (raw `capture-pane -e` ANSI)
    and the colored Diff are external strings wider/taller than the pane. Before placing
    them in the Lipgloss layout, run every line through `ansi.Truncate(line, rightPaneWidth, "")`
    (and select the visible slice for the viewport) so escape codes don't bleed past the
    border or break wrapping. Never hand raw capture output straight to Lipgloss.
  - render the hint bar
    `↵ attach · j/k move · h/l fold · ⇥ tab · d del · r repair · / filter · q quit`
    (plus a footer for the inline `status` message; when a row is armed for delete, show
    `press d again to delete <name>`).
  - Then `v := tea.NewView(content); v.AltScreen = true;
v.MouseMode = tea.MouseModeCellMotion; return v` — alt-screen and mouse are
    **declarative View fields** in v2.
- Verify: compiles; `go vet` clean.

#### Step 7: Attach behavior (with auto-repair — parity with fzf Enter)

- `attachCmd(selected)` (selected carries `Repo` + `Name`):
  - resolve `window := worktree.GetWindowName(selected.Name)`.
  - if `os.Getenv("TMUX") == ""` → return a `statusMsg` "not inside tmux; run `dg wt ui` from a tmux session" (do **not** quit).
  - `session, ok := tmux.WindowSession(window)`:
    - if `ok` → `tmux.SwitchToWindow(session, window)` then `tea.Quit`.
    - if `!ok` (window gone) → **auto-repair**: resolve the coder via the shared
      `worktree.ResolveAIAlias` + `ResolveAICoder`, call
      `mgr.RepairInRepo(selected.Repo, selected.Name, coder)` (repo-scoped — Step 0),
      re-resolve `WindowSession`, `SwitchToWindow`, then `tea.Quit`. Mirrors the current
      fzf jump (Enter on a missing window repairs + jumps). On repair failure → `statusMsg`,
      do **not** quit.
- Verify: covered by model test using the injected `attachFn`/`repairFn` seams (no real tmux).

#### Step 7b: Delete behavior (double-confirm — parity with fzf ctrl-d)

- On `d` over a worktree row:
  - if `pendingDelete != "<repo>/<name>"` (not yet armed) → set
    `pendingDelete = "<repo>/<name>"`, show the inline "press d again to delete" hint,
    and highlight the row (reuse the styles' "armed" style). No deletion yet.
  - if `pendingDelete == "<repo>/<name>"` (second `d` on the same row) → call the
    injected `removeFn` (real impl: `mgr.RemoveInRepo(selected.Repo, selected.Name, true)`
    — repo-scoped, Step 0; `force=true` preserves the documented "running coder ⇒ dirty
    ⇒ force through guard" semantics), then **drop the row from `statuses`/`rows`** and
    move the cursor to the nearest remaining worktree row, clear `pendingDelete`. On
    error → `statusMsg`, clear arm.
  - **Disambiguation:** passing `selected.Repo` is what makes delete correct when the
    same worktree name exists in two repos (`Remove(name,…)` would be ambiguous).
- Verify: model test arms on first `d`, deletes on second `d` (via mock `removeFn`),
  a non-`d` key clears the arm without deleting, and a **duplicate-name-across-repos**
  case deletes the correct one (asserts the `repo` arg passed to `removeFn`).

#### Step 7c: Repair behavior (parity with fzf ctrl-r)

- On `r` over a worktree row: resolve the coder via the shared `worktree.ResolveAIAlias`
  - `ResolveAICoder` (same precedence as `dg wt create`/`repair`), call the injected
    `repairFn` (real impl: `mgr.RepairInRepo(selected.Repo, selected.Name, coder)` —
    repo-scoped, Step 0), then refresh the row's glyph/status (re-query `WindowSession`)
    and clear `agentOffline`. On error → `statusMsg`.
- Verify: model test calls `repairFn` once with the selected row's `repo`+`name` (mock;
  no real tmux).

#### Step 8: `Run()` entrypoint + command wiring

- `internal/tui/worktree/run.go`: `func Run() error` builds **one** `mgr := worktree.New()`
  and reuses its already-wired deps — `mgr`, `mgr.Tmux`, `mgr.Git` — to construct the
  `Model` (do **not** call `tmux.New()`/`git.New()` again; the manager owns them). Then
  `tea.NewProgram(m).Run()`. (v2 sets `AltScreen`/`MouseMode` declaratively in `View`,
  not via program options.)
- `cmd/worktree.go`: add
  ```go
  var worktreeUICmd = &cobra.Command{
      Use:     "ui",
      Aliases: []string{"dash", "dashboard"},
      Short:   "Open the worktree dashboard (TUI)",
      Args:    cobra.NoArgs,
      RunE:    func(cmd *cobra.Command, args []string) error { return tuiworktree.Run() },
  }
  ```
  and `worktreeCmd.AddCommand(worktreeUICmd)` in `init()`.
- Verify: `./devgeta wt ui` launches (manual, Step 11).

#### Step 8b: Remove the fzf jump flow

- **Only after** Steps 7/7b/7c give the TUI jump + delete + repair parity, delete the
  now-dead fzf jump code so no unused code ships:
  - `cmd/worktree.go`: remove `worktreeJumpCmd`, its `worktreeCmd.AddCommand(...)`
    line in `init()`, and the `dg wt j` example lines in `worktreeCmd.Long`.
  - `internal/tooling/worktree/worktree.go`: remove `Jump`, `confirmAndRemove`,
    `buildConfirmRows`, `formatJumpRow`, `parseJumpRow`, `parseRepoAndName`,
    `parseJumpOutput`, `runFzfWithExpect`, `execFzf`, the `fzfRun` struct field, its
    `wm.fzfRun = wm.execFzf` assignment in `New()`, and the `pendingDelete` field +
    `pendingDeleteInfo` type.
  - `internal/tooling/worktree/worktree_test.go`: remove the orphaned fzf-jump tests
    (`TestFormatJumpRow`, `TestParseJumpRow`, `TestParseJumpOutput*`, and the
    `fzfCall`-harness tests exercising `Jump`/`confirmAndRemove`).
  - `internal/tooling/worktree/worktree.go:135`: update the stale `Create` error string
    `` use `dg wt jump %s` `` → `` use `dg wt ui` `` (this function stays; only the
    message is fixed).
  - **Do NOT touch** `SelectWorktreeInteractively`, the `Fzf` field, `Fzf.SelectFromList`,
    `Remove`/`removeByRepo`/`RemoveInRepo`, or `Repair`/`RepairInRepo` — still used by
    `dg wt rm`/`repair`/`prune` and the new TUI.
- Verify: `go build ./...` and `go vet ./...` are clean (no unused symbols); the grep
  gate finds no stale **symbols or user-facing strings** anywhere in `cmd/`/`internal/`/
  `docs/`/`README.md` —
  `grep -rniE "worktreeJumpCmd|execFzf|parseJumpOutput|dg wt j(ump)?\b|worktree jump" cmd/ internal/ docs/ README.md`
  returns nothing (outside this cycle doc); `go test ./internal/tooling/worktree/...` passes.

#### Step 9: Model unit tests (`model_test.go`)

- `func init() { testutil.InitLogger() }`.
- Construct a `Model` with injected fake data and **injected I/O functions** so no
  real commands run: give the model function fields (`captureFn`, `diffFn`,
  `attachFn`, `removeFn`, `repairFn`) defaulting to the real tmux/git/mgr calls in
  `Run()` but overridable in tests. (These seams are declared in Step 6.)
- Tests:
  - `build-rows` grouping/order: repos sorted alpha, worktrees nested under each.
  - `j/k` cursor movement: cursor only lands on worktree rows, never repo headers;
    verify cursor index always maps to `rowWorktree` kind.
  - `h/l`/`z` fold behavior: collapse hides children, expand reveals.
  - `Tab` toggles `activeTab` between agent and diff.
  - `/` filter mode: runes filter `rows`; `Esc` clears filter and exits; `Enter`
    keeps filter and exits.
  - Attach with no `TMUX` env var sets inline status message and does **not** quit.
  - Attach when window is gone invokes `repairFn` then `attachFn` (auto-repair path).
  - Delete double-confirm: first `d` arms `pendingDelete` (no `removeFn` call); second
    `d` on the same row calls `removeFn` once and drops the row; a non-`d` key between
    the two clears the arm and `removeFn` is never called.
  - **Duplicate names across repos:** with `repo-a/feature-x` and `repo-b/feature-x`,
    deleting/repairing the `repo-b` row passes `repo == "repo-b"` to `removeFn`/`repairFn`
    (asserts disambiguation, the whole reason for Step 0's repo-scoped APIs).
  - Repair: `r` invokes `repairFn` once for the selected worktree.
  - **Offline agent:** an `agentOfflineMsg` sets `agentOffline`, renders the placeholder,
    and the next `tickMsg` does **not** re-issue `captureAgentCmd`.
  - **Narrow terminal:** a `WindowSizeMsg` with width 30 does not panic and yields
    `rightPaneWidth >= 0` (safe-clamp regression test).
- Verify: `go test ./internal/tui/worktree/...` passes.

#### Step 10: Helper tests

- `tmux_test.go`: `CapturePane` issues `capture-pane -p -e -t s:w.0` (pane 0) and returns
  stdout (assert via `MockBaseCommand` + `GetLastExecCommandCall`); error path surfaces
  stderr (incl. a `can't find pane` case the model maps to offline).
- `git_test.go`: `Diff` runs `diff --color=always HEAD` and appends untracked `??`
  entries from `status --porcelain` (assert an untracked-only worktree still shows its
  new file); `DiffStat` parses `--numstat` (incl. a binary `-\t-\tfile` line) **and**
  counts untracked files. `testutil.VerifyNoRealCommands` where applicable.
- Verify: `go test ./internal/apps/tmux/... ./internal/apps/git/...` passes.

#### Step 11: tmux binding + manual smoke

- `configs/tmux/tmux.conf:113`: change the popup command from `devgeta wt j` to
  `devgeta wt ui`. Keep `display-popup -E -w 80% -h 60%`.
- **Removal / migration note (this is a real removal, not additive):** `dg wt j`/`jump`
  is **deleted** this cycle (its jump/delete/repair behavior is migrated into `dg wt ui`).
  Two consequences for existing users:
  - **Binary:** after upgrading, `dg wt j` no longer exists — it errors as an unknown
    command. This is an intentional CLI change (CLAUDE.md §10) and must be called out
    in the release notes / PR description with the one-line migration: "use `dg wt ui`".
  - **tmux config:** per Product Principle 4 (user config edits are never overwritten),
    users who already applied `tmux.conf` keep their old `prefix + u → devgeta wt j`
    binding until they re-run the relevant `dg` configure step and reload tmux
    (`tmux source-file` / restart). Because `devgeta wt j` is now gone, that stale
    binding would fail — so the release notes must tell upgraders to refresh the tmux
    config (or manually repoint `prefix + u` to `devgeta wt ui`).
  - **Second behavioral break (automation):** the old `dg wt j` **outside tmux** printed
    the selected worktree's **path** to stdout (script-friendly). The TUI has no such
    output. Release notes must call this out separately from the command removal. If
    anyone relied on it for scripting, the discovery path is `dg wt l`/`dg wt ls`; a
    dedicated `dg wt path <name>` affordance is logged for ROADMAP (not Phase 1).
  - **Semver:** this removes a subcommand and changes outside-tmux behavior. By CLAUDE.md
    §9 a CLI-breaking change is MAJOR, but the project is pre-1.0 (`v0.x`), where breaking
    changes ship in a MINOR bump with prominent notes. **Recommendation: MINOR bump +
    bold "Breaking changes" section in the release.** Confirm with the maintainer before
    tagging (CLAUDE.md §9 "when in doubt, ask before tagging").
- Verify: see Manual Verification below.

#### Step 12: User-facing docs

- Per CLAUDE.md §12 ("document in README.md and `docs/spec.md` if user-facing"),
  add `dg wt ui` (and the `dash`/`dashboard` aliases) to `README.md`'s command list
  and to the worktree section of `docs/spec.md`, including the keymap
  (`j/k`, `h/l`, `z`, `Tab`, `Enter`, `d`, `r`, `/`, `q`), mouse-drag pane resize, and
  the outside-tmux read-only behavior.
- **Remove** all references to `dg wt j`/`jump` and the old fzf popup from `README.md`
  and `docs/spec.md`, and add the migration line ("`dg wt j` → `dg wt ui`") + the tmux
  refresh caveat from Step 11.
- Verify: README and spec document the new command and keymap; `grep -rn "wt j\|wt jump"`
  across `README.md`/`docs/` returns nothing (no stale fzf-jump references remain).

---

## 6. Verification Plan

### Automated Verification

```bash
# Fast inner loop while iterating on the touched packages:
go test ./internal/tui/worktree/... ./internal/apps/tmux/... ./internal/apps/git/...

# Full gate before marking the cycle done (this superset covers the line above):
go build -o devgeta main.go
go vet ./...
go test ./...
```

### Manual Verification

1. In a tmux session, create two worktrees with Claude running:
   `./devgeta wt new demo-a --ai claude` and `./devgeta wt new demo-b --ai claude`.
2. Run `./devgeta wt ui`.
3. Confirm: tree shows the repo with `demo-a`/`demo-b` children, status glyph + any
   `+A/-R`; `j/k` moves (cursor never lands on repo headers); `h/l` and `z`
   fold/unfold; right pane Agent shows the selected worktree's Claude output;
   `Tab` switches to Diff and shows `git diff`.
4. Press `/`, type part of a name → list filters; `Esc` clears filter and exits;
   `Enter` keeps filter and exits.
5. Select `demo-a`, press `Enter` → TUI exits and tmux is now on the `wt-demo-a`
   window.
6. **Auto-repair on attach:** kill `demo-b`'s window (`tmux kill-window -t wt-demo-b`),
   relaunch `dg wt ui`, select `demo-b`, press `Enter` → the window is recreated, the
   AI coder relaunches, and tmux switches to it (parity with the old fzf Enter).
7. **Delete (double-confirm):** select a throwaway worktree, press `d` → row arms with
   "press d again to delete"; press `d` again → worktree + window + branch are removed
   and the row disappears. Press `d` then `j` on another row → arm cleared, nothing deleted.
8. **Repair:** kill a worktree's window, select it, press `r` → window recreated + coder
   relaunched, glyph flips to `●`.
9. **Untracked file visibility:** in `demo-a`, have the agent (or you) create a new
   file `touch demo-a/NEW.txt`; the Diff tab must show it (under "Untracked files")
   and the `+A/-R`/file count must reflect it — not appear empty.
10. **ANSI integrity:** make the agent print wide colored output; confirm the right
    pane truncates cleanly at the border with no color bleed or broken layout.
11. **Offline agent:** let an agent finish so its window closes; confirm the Agent pane
    shows the "window offline — press r to repair" placeholder and the UI does not
    flicker/spam errors on the 1.5s tick; press `r` to bring it back.
12. **Narrow terminal:** shrink the terminal to ~30 cols; confirm no panic and a sane
    single-pane/fallback layout.
13. Re-bind check: `prefix + u` opens the same dashboard in a popup.
14. **Outside-tmux goal**: Run `./devgeta wt ui` **outside** tmux → it still renders
    (read-only browsing works); `Enter` shows the inline "not inside tmux" status
    instead of crashing. This is an **explicit goal**, not just graceful degradation.
    (Note: unlike old `dg wt j`, it does **not** print a path — see the migration note.)

### Regression Check

- `dg wt l`, `dg wt new`, `dg wt rm`, `dg wt repair`, `dg wt prune` still behave
  exactly as before (only added the `ui` command + removed `j`).
- **`dg wt j` / `dg wt jump` is removed**: running it errors as an unknown command
  (intentional — verify the removal is complete, not a regression).
- `dg wt rm`'s interactive fzf picker (`SelectWorktreeInteractively`) still works —
  it shares the `Fzf` field that was deliberately kept.
- `go test ./internal/tooling/worktree/...` still passes (jump tests removed; the
  remaining `Remove`/`Repair`/`List` logic is unchanged).

---

## 7. Risks & Trade-offs

| Risk                                                                                                                               | Likelihood | Mitigation                                                                                                                                                                                                         |
| ---------------------------------------------------------------------------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| New deps (bubbletea v2/lipgloss v2) increase binary size                                                                           | High       | Both are pure Go, no cgo → cross-compilation (darwin/linux, amd64/arm64) unaffected. Acceptable.                                                                                                                   |
| `capture-pane` only returns the **visible** pane, not scrollback                                                                   | Medium     | Acceptable for a live agent view in Phase 1; document. Scrollback/viewport scroll deferred.                                                                                                                        |
| `switch-client` from inside a `display-popup` behaves oddly                                                                        | Medium     | Same mechanism the current fzf popup already uses successfully; verified in manual Step 5/6.                                                                                                                       |
| Bubble Tea models are awkward to unit-test                                                                                         | Medium     | Test the pure `Update`/state transitions with synthetic msgs + injected I/O fns; no real program.                                                                                                                  |
| Agent refresh tick causes flicker/CPU                                                                                              | Low        | **Mitigated:** v2's Cursed Renderer has optimized diffing + our hash compare skips unchanged content + pause during active nav.                                                                                    |
| Removing `dg wt j` breaks users' muscle memory / stale tmux bindings                                                               | Medium     | Migrate full jump/delete/repair parity into `dg wt ui` first (Steps 7/7b/7c) so nothing is lost; call out the removal + `prefix + u` refresh in release notes (Step 11). Intentional CLI change per CLAUDE.md §10. |
| Deleting fzf code accidentally removes still-shared helpers                                                                        | Medium     | Step 8b explicitly scopes removal to jump-only symbols and lists the keep-set (`SelectWorktreeInteractively`/`Fzf`/`Remove`/`Repair`); `go build`+`go vet`+grep gate catches over-deletion.                        |
| TUI delete loses the fzf double-confirm/force safety semantics                                                                     | Low        | Step 7b preserves the documented double-confirm + `force=true` behavior and reuses the same `Remove`/`removeByRepo` business logic.                                                                                |
| Untracked agent-created files invisible in Diff (plain `git diff` skips them)                                                      | High       | Step 3: `Diff` = `diff --color=always HEAD` **+** untracked `??` from `status --porcelain`; `DiffStat` counts untracked files. Manual Step 9 verifies.                                                             |
| Raw `capture-pane -e` / colored diff ANSI bleeds past the pane, breaks borders                                                     | High       | Step 6: every right-pane line is run through `ansi.Truncate` to `rightPaneWidth` before layout; never hand raw ANSI to Lipgloss. Manual Step 10.                                                                   |
| Narrow terminal makes `maxLeft < minLeftPaneWidth` → inverted clamp / negative width panic                                         | Medium     | Step 6 safe clamp `maxLeft = max(min, pct*width)`, derive `rightPaneWidth = max(0, …)`, single-pane fallback. Unit + manual (Step 9/12) regression.                                                                |
| Dead/closed agent window → 1.5s tick spams failing `capture-pane`                                                                  | Medium     | Step 6 `agentOfflineMsg` sets `agentOffline`, shows a placeholder, and **stops** re-issuing capture until selection change or repair. Manual Step 11.                                                              |
| Cross-package API gap: TUI can't reach unexported `removeByRepo`/`resolveAIAlias`; `Remove`/`Repair` ambiguous for duplicate names | High       | **Step 0** adds exported `RemoveInRepo`/`RepairInRepo` + a shared `ResolveAIAlias`; the TUI passes `WorktreeStatus.Repo`. Duplicate-name test in Step 9.                                                           |
| Large `git diff` blocks the event loop / freezes UI                                                                                | Medium     | `computeDiffCmd`/`captureAgentCmd` run as async `tea.Cmd`s (never inline in `Update`), with a size cap on very large diffs (Step 6).                                                                               |

### Trade-offs Made

- **Bubble Tea v2 over v1** — v2's declarative View struct, Cursed Renderer (optimized
  diffing), and cleaner keyboard/mouse APIs outweigh the newer release. v2.0.7 is
  stable with 7 patch releases. Import path is `charm.land/bubbletea/v2`.
- **Bubble Tea over hand-rolled raw-terminal rendering** — matches Go ecosystem
  norms, gives us alt-screen/diffed rendering for free, and Phases 2–3 (filtering,
  popups, notifications) build naturally on it. Cost: two new dependencies.
- **Replace and remove `dg wt j` (fzf) outright** — rather than keeping it as a
  fallback, the TUI reaches full parity (jump + auto-repair + delete + repair) and the
  fzf jump flow is deleted this cycle. Chosen by the maintainer: one entry point, no
  dead code, no two-ways-to-do-the-same-thing. Cost: a one-time CLI removal + tmux
  rebinding the release notes must flag. The TUI reuses the existing `Remove`/`Repair`
  business logic, so only the fzf _presentation_ layer is discarded.
- **NERDTree-familiar keys, no palette/which-key** — honors the maintainer's
  "least thinking / fewest new keybindings" constraint; richer models from the
  wireframes are explicitly deferred.

### Decisions (resolved from cross-model review — maintainer may override)

- **Duplicate worktree names across repos:** handled by Step 0's repo-scoped
  `RemoveInRepo`/`RepairInRepo`; the TUI always passes `WorktreeStatus.Repo`. No
  guessing, no ambiguity.
- **AI coder selection in the TUI:** Phase 1 resolves from env/`global_config` default
  via the shared `ResolveAIAlias` (no per-action `--ai` prompt). A `--ai` flag / picker
  is deferred to Phase 2.
- **Pane targeting:** capture `session:window.0` (the agent's pane), not the active
  pane — reliable even if the user splits the window.
- **Scrolling:** Diff renders in a scrollable viewport (`ctrl+d`/`ctrl+u`/`PgDn`/`PgUp`);
  Agent is a single visible-pane snapshot (no scrollback in Phase 1). `j/k` stay on the
  tree.
- **Async I/O:** all tmux/git work runs as `tea.Cmd`s off the `Update` loop, with a
  large-diff size cap — the UI never blocks on a big `git diff`.
- **Outside-tmux path output:** not preserved in Phase 1 (documented break); discovery
  via `dg wt l`, with `dg wt path <name>` logged for ROADMAP.
- **Release impact:** recommend a **MINOR** bump (pre-1.0) with a bold "Breaking
  changes" section covering both the `dg wt j` removal and the outside-tmux behavior
  change — confirm before tagging.

---

## 8. Cross-Model Review Notes

- [ ] Root cause / goal confirmed? (This is a feature, not a bug — objective in §3.)
- [ ] All affected files identified? (§5 table)
- [ ] Verification steps executable? (§6)
- [ ] Scope realistic? Phase 1 now includes fzf parity (delete/repair/auto-repair) +
      removal, making it ~2 cycles — is that acceptable, or should it be split? (§4, Duration)
- [ ] Dependency addition acceptable (bubbletea/lipgloss, pure Go)?
- [ ] TUI testability seam (injected I/O fns) acceptable vs. teatest dependency?
- [ ] fzf removal scoped correctly — only jump-only symbols deleted, shared
      `Fzf`/`Remove`/`Repair` kept? (§2 remove-list, Step 8b)
- [ ] `dg wt j` removal acceptable per CLAUDE.md §10 (intentional CLI change, release-noted)?
- [ ] Step 0 API shape OK — `RemoveInRepo`/`RepairInRepo` + shared `ResolveAIAlias`
      names/placement (`internal/tooling/worktree`) acceptable? (Step 0)
- [ ] Diff completeness (untracked files via `status --porcelain`, `--color=always`)
      and ANSI-safe rendering (`ansi.Truncate`) sufficient? (Steps 3, 6)
- [ ] Edge cases covered — narrow-terminal clamp, offline-agent placeholder, async
      diff, duplicate names across repos? (Step 6, 9)
- [ ] Release impact: MINOR bump + breaking-changes notes (two breaks) acceptable? (§7 Decisions)

**Reviewer notes:**
(Fill in during review)

```

```
