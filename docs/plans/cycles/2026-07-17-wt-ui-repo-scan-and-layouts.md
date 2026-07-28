# Cycle: Repo Discovery Scan + Window Layouts for `dg wt ui` Create

**Date:** 2026-07-17
**Estimated Duration:** ~10 hours
**Status:** Done

---

## 1. Domain Context

The `2026-07-15-wt-ui-create-flow` cycle added `n` to `dg wt ui`: pick a repo from a
floating fuzzy picker → type a name → worktree + tmux window created → attach and quit.
Two gaps surfaced in real use:

1. **Repos you've never touched are invisible.** The picker's candidate sources are: the
   cwd repo, the cursor row's repo, devgeta's recent-repos store, and `zoxide query -l`.
   None of these scans the filesystem, so a repo you've never `cd`'d into (no zoxide entry)
   and never created a worktree in (not in the store) simply does not appear — you must
   type its full path from memory. Example that prompted this cycle: `~/pillar/pillar-infrastructure`
   did not show up. This is not a matching bug; the repo was in no source.

2. **The created window is always one pane running one AI coder.** `launchWindow`
   (`worktree.go:256`) makes a single-pane window and sends the resolved coder's command
   (`opencode` by default, `claude` via `default_ai`/`DEVGETA_WORKTREE_AI`). There is no
   way to express "claude + nvim side by side" or "just nvim for reading code" — the three
   real workflows this cycle's requester uses: (a) review in opencode, (b) work in
   claude + nvim vertical split, (c) read/review code in nvim only.

A July 2026 survey of competing tools (tmux-sessionizer, sesh, twm, gwq, ghq,
vscode-project-manager, lazygit) informed both fixes:

- **Discovery:** scanners that stop descending at the repo boundary (ghq's
  `filepath.SkipDir` on the `.git` marker) produce clean repo-root lists; scanners that
  keep descending (twm to depth 3, tmux-sessionizer fixed-depth) surface nested-directory
  noise. Since the picker's only valid answer is a repo root, prune-at-`.git` is the right
  model for us.
- **Open mode:** the dominant pattern is a silently-applied configured default with
  per-project overrides. A genuine invocation-time layout picker is rare — **twm is the
  only surveyed tool with one** (`twm -l` prompts among named layouts; bare `twm` uses the
  default silently). That is exactly the `n` (default) / `N` (pick) split this cycle adds.
  Per-project startup scripts (`.tmux-sessionizer`, `.twm.yaml`, `.gwq.toml`) are
  explicitly acknowledged by those projects as code-execution vectors — devgeta keeps
  layouts in its own config instead (see § 7).

Related: [ROADMAP.md](../../ROADMAP.md) Worktree Enhancements; prior cycles
`2026-07-15-wt-ui-create-flow.md`, `2026-06-07-worktree-v2-tui-dashboard.md`.

---

## 2. Engineer Context

- **Relevant files:**
  - `internal/tooling/worktree/repo_candidates.go` — `RepoCandidates(cursorRepoSlug)`
    (`:33`) merges cwd → cursor → recents → zoxide, canonicalizes and dedupes. New scan
    source plugs in here. `ValidateRepoPath` (`:151`) resolves a path to its repo root.
  - `internal/tooling/worktree/aicoder.go` — `AICoder` interface (`:13`), `OpenCodeCoder`
    / `ClaudeCoder`, `ResolveAICoder(alias)` (`:47`), `ResolveAIAlias(flag, gc)` (`:63`,
    precedence flag → env → config → opencode). Layouts generalize this "what runs"
    concept; the coder resolution stays reachable for backward compatibility.
  - `internal/tooling/worktree/worktree.go` — `launchWindow` (`:256`),
    `createWindowAndLaunch` (`:278`, single pane + `SendKeysToWindow`), `ensureWindow`
    (`:604`), `create` (`:153`), `CreateAt` (`:130`). This is where a layout is applied
    after the window exists.
  - `internal/apps/tmux/tmux.go` — tmux wrapper. Has `CreateWindow` (`:261`),
    `SendKeysToWindow` (`:298`), `SelectWindow` (`:303`). **No split-pane method exists** —
    this cycle adds one to the wrapper (never a raw `exec.Command`; see CLAUDE.md § 6
    "Route external tools through their app wrappers").
  - `internal/config/fromFile.go` — `WorktreeConfig{DefaultAI, RecentRepos}` (`:80-83`),
    `CanonicalRepoPath` (`:125`), atomic `Save()`/`Reset()`. New config fields land here.
  - `internal/tui/worktree/create_flow.go` — `handleNewWorktree` (`:64`, opens picker),
    `createMode` states (`:24`), `handleRepoPickKey` (`:123`), `handleNameInputKey`
    (`:160`), `clearCreateState` (`:230`), `createFn` seam wired in `model.go:168`. The `N`
    layout-pick step and the layout-aware createFn extend this.
  - `internal/tui/worktree/model.go` — key dispatch, `attachToWindowCmd` (`:716`), hint bar
    (`:1099`), help entries (`:1126`). `repoCandidatesFn`/`createFn` seams at `:158,168`.
  - `internal/tui/components/fuzzypicker.go` — reused as-is for the layout picker (`N`).

- **Key facts:**
  - The recent-repos store already closed part of the discovery gap after the fact
    (`pillar-infrastructure` appears now that a worktree was created in it). The scan
    source closes it _before_ first use — the actual ask.
  - Layouts must degrade to today's exact behavior when unconfigured: a single-pane window
    running the resolved AI coder. `default_ai` must keep working unchanged.
  - `createFn` currently resolves the coder internally (`model.go:168-197`) and swaps
    `mgr.WarnFn`. A layout selection has to reach `create`, so `createFn`'s signature or
    the manager's create entrypoint gains a layout parameter — keep the WarnFn swap intact.

- **Testing:** [docs/guides/testing-patterns.md](../guides/testing-patterns.md) — always
  `testutil.MockApp`, `testutil.VerifyNoRealCommands`, isolate every `paths.Paths.*`
  mutation with `t.Cleanup`, `func init() { testutil.InitLogger() }`. The scan walks a real
  temp directory tree the test creates under an isolated root (walking the filesystem is
  not "running a real command" — but it must stay inside the test sandbox). TUI tests drive
  `Update` with `tea.KeyPressMsg` and assert on `View()` and injected seam calls.

- **Test commands:**

  ```bash
  go test ./internal/tui/... ./internal/tooling/worktree/ ./internal/config/ ./internal/apps/tmux/
  go test ./...
  make lint
  ```

---

## 3. Objective

Pressing `n` in `dg wt ui` finds any git repo under the user's configured search paths
(not only repos already known to zoxide or the recent store), listing repo roots only and
never their nested subdirectories; and the created tmux window is built from a named
**layout** — `n` applies the default layout silently, `N` opens a picker to choose one for
this create — so a worktree can open as opencode, claude + nvim, or nvim-only without
leaving the TUI. Unconfigured, behavior is identical to today.

---

## 4. Scope Boundary

### In Scope

**Part A — repo discovery scan**

- [x] `search_paths` config under `worktree` (list of directories) + optional `scan_depth`
      (default 4); a sane built-in default when unset (the user's top-level code dirs are
      _not_ hardcoded — default is empty = scan disabled, with docs showing how to set it)
- [x] A filesystem scan candidate source: walk each search path with `filepath.WalkDir`,
      **prune at the `.git` boundary** (emit the repo root, `SkipDir` its subtree), skip an
      exclude-list of components (`node_modules`, `.cache`, `vendor`, `target`, `dist`,
      `.git`), respect `scan_depth`, canonicalize every hit through `CanonicalRepoPath`
- [x] Wire the scan into `RepoCandidates` after recents, deduped against all other sources
      (a repo already offered by cwd/cursor/recents/zoxide never doubles)

**Part B — window layouts**

- [x] A `Layout` model: named, ordered list of panes; each pane has a command and (for
      panes after the first) a split direction (`vertical`/`horizontal`) — data only, no
      per-repo scripts
- [x] Built-in layouts requiring zero config: `opencode` (single pane, = today's default),
      `claude` (single pane), `claude-nvim` (claude left + nvim right, vertical split),
      `nvim` (single pane). Each pane command reuses `AICoder.Command()` / a known editor
      command, and validates its tool is installed the way `AICoder.EnsureInstalled` does
- [x] `default_layout` config field with an explicit resolution contract (see § 5
      "Layout resolution contract") that keeps every existing AI-selection behavior —
      `--ai`, `DEVGETA_WORKTREE_AI`, `default_ai` — working unchanged
- [x] A `SplitWindow`/pane method on the tmux wrapper (`internal/apps/tmux`); `launchWindow`
      builds the window from the chosen layout via the wrapper, replacing the hardcoded
      single-pane `createWindowAndLaunch`. **Deviation from this bullet's original "only new
      tmux primitive" wording:** Step 6 also added `ActivePaneID`/`SelectPane`. Reason:
      `configs/tmux/tmux.conf` sets `pane-base-index 1`, so a naive index-0 pane target after
      a split would silently hit the wrong pane rather than error — confirmed with a live
      isolated tmux session during Step 6's review. `ActivePaneID` captures pane 0's
      globally-unique tmux pane_id right after window creation; `SelectPane` reselects it once
      all panes are built, so the primary pane (not the last-split one) has focus on attach.
- [x] `N` keybinding: same repo → name flow as `n`, plus a layout picker step (reuses
      `FuzzyPicker`, cursor on the default) before create; `n` stays exactly as-is
      (default layout, no extra keystroke)
- [x] `--layout` flag on `dg wt new` **and** `dg wt repair` (mutually exclusive with
      `--ai`), so CLI and TUI share one contract; `repair --ai` keeps today's semantics
- [x] Hint bar + `?` help entries for `N`; docs (`docs/spec.md`, `README.md`, ROADMAP —
      no worktree guide exists in `docs/guides/`, so no guide is touched or invented)

### Explicitly Out of Scope

- Per-project layout files checked into repos (`.tmux-sessionizer`-style) — security
  vector, see § 7; layouts live in devgeta config only
- User-defined custom layouts in config **beyond the built-ins** — deferred to a follow-up
  once the built-in set and the pane model prove out (note it in ROADMAP)
- Prompt-first create (passing a task prompt to the coder) — already a separate roadmap item
- Auto-naming a blank name — separate roadmap item
- Base-branch selection
- Changing the name-prompt step (the requester confirmed it "is fine")
- Per-worktree layout memory (repair rebuilding the layout a worktree was _created_ with)
  — requires storing a layout per worktree in `global_config.yaml`; deferred with the
  custom-layouts follow-up. Repair uses the same resolution as create (see contract)
- Making the scan recursive across symlink cycles or network mounts — scan is local,
  depth-limited, and skips symlinked dirs to avoid loops

**Scope is locked.** If something out of scope is needed, document it for a future cycle
and reference here.

---

## 5. Implementation Plan

### Layout resolution contract

One rule for create, repair, and TUI auto-repair. Highest present source wins; every
AI-alias source resolves to a **derived single-pane layout** of that coder, so today's
`--ai` → `DEVGETA_WORKTREE_AI` → `default_ai` → `opencode` chain is preserved verbatim,
with `default_layout` slotted between env and `default_ai`:

1. `--layout` flag (CLI) / `N` picker selection (TUI)
2. `--ai` flag → derived single-pane layout (`--layout` and `--ai` together = error;
   `cobra.MarkFlagsMutuallyExclusive`)
3. `DEVGETA_WORKTREE_AI` env → derived single-pane layout
4. `default_layout` config
5. `default_ai` config → derived single-pane layout
6. Built-in fallback: `opencode` single-pane

Consequences to pin down explicitly:

- `dg wt repair --ai opencode` with `default_layout: claude-nvim` honors `--ai`
  (single-pane opencode) — an explicit flag always beats config. Bare `dg wt repair`
  and TUI auto-repair (missing-window attach) resolve with no flags, so they rebuild
  `default_layout` when set, else derive from AI as today. Repair does **not** remember
  the layout a worktree was created with (out of scope, § 4); this is documented in
  `repair --help`.
- Existing configs and invocations hit rules 2/3/5/6 only — behavior is bit-identical
  to today. Rule 4 activates only for users who set `default_layout`.
- The `repair` command's `Long` help text (`cmd/worktree.go:213-224`) documents the old
  precedence and must be updated to this contract in the same PR.

### Scanner validation rules

- Search roots are canonicalized (`CanonicalRepoPath`, which resolves symlinks) and
  deduped before walking; a root nested inside another configured root is dropped. A
  configured root that is itself a symlink is honored (resolved and scanned once) — the
  "skip symlinked dirs" rule below applies only to symlinks _encountered during
  traversal_, to avoid loops; it never suppresses a path the user explicitly configured.
- The exclude-component list (`node_modules`, `.cache`, `vendor`, `target`, `dist`,
  `.git`) applies to directories _discovered during the walk_, not to the configured
  roots themselves: a user who points `search_paths` directly at `~/work/vendor` means
  it, so the root is scanned even though `vendor` is an excluded component name.
- A missing, non-directory, or unreadable entry in `search_paths` is skipped with a
  debug-level log — never a TUI status or per-`n` warning (a stale entry would otherwise
  nag on every picker open), and never an error that blanks other candidate sources.
- `scan_depth` unset, `0`, or negative → default `4`. Disabling the scan is done only by
  leaving `search_paths` empty, so there is exactly one off-switch.
- Walk errors on individual subtrees (permissions) skip that subtree and continue.

### File Changes

| Action | File Path                                      | Description                                                             |
| ------ | ---------------------------------------------- | ----------------------------------------------------------------------- |
| Modify | `internal/config/fromFile.go`                  | Add `SearchPaths`, `ScanDepth`, `DefaultLayout` to `WorktreeConfig`     |
| Create | `internal/tooling/worktree/scan.go`            | Filesystem repo scan (WalkDir + prune-at-`.git` + excludes + depth)     |
| Create | `internal/tooling/worktree/scan_test.go`       | Scan tests over a temp tree (nested repos, excludes, depth, symlinks)   |
| Modify | `internal/tooling/worktree/repo_candidates.go` | Add scan as a candidate source in `RepoCandidates`                      |
| Create | `internal/tooling/worktree/layout.go`          | `Layout`/`Pane` model, built-ins, `ResolveLayout`, derive-from-AI       |
| Create | `internal/tooling/worktree/layout_test.go`     | Resolution, backward-compat derivation, install checks                  |
| Modify | `internal/apps/tmux/tmux.go`                   | Add `SplitWindow` (pane split) wrapper method                           |
| Modify | `internal/apps/tmux/tmux_test.go`              | Test the new wrapper method (mocked)                                    |
| Modify | `internal/tooling/worktree/worktree.go`        | `launchWindow` builds the window from a `Layout`; thread layout through |
| Modify | `internal/tui/worktree/create_flow.go`         | `N` layout-pick step; layout-aware `createFn`                           |
| Modify | `internal/tui/worktree/model.go`               | Wire `N`, layout seam, hint bar + help                                  |
| Modify | `cmd/worktree.go`                              | `--layout` on `new` + `repair` (excl. with `--ai`); update help texts   |
| Modify | `docs/spec.md`, `README.md`, `ROADMAP.md`      | Document scan config, layouts, resolution contract; move roadmap items  |

### Step-by-Step

#### Step 1: Config fields

- Add to `WorktreeConfig`: `SearchPaths []string yaml:"search_paths,omitempty"`,
  `ScanDepth int yaml:"scan_depth,omitempty"`, `DefaultLayout string yaml:"default_layout,omitempty"`.
  All additive/optional; old configs load unchanged. Call the schema change out in the PR
  per change discipline (§ 10).
- Verify: `go test ./internal/config/` incl. a legacy-config round-trip fixture.

#### Step 2: Repo scan

- `scan.go`: `scanRepos(searchPaths []string, maxDepth int) []string`. For each path,
  `filepath.WalkDir`; on encountering a `.git` entry, record `filepath.Dir` as a repo root
  and return `filepath.SkipDir` (prune the subtree — nested repos/submodules below a repo
  root are intentionally not listed). Skip excluded components and symlinked dirs; enforce
  depth relative to each search-path root. Canonicalize each root. Apply the "Scanner
  validation rules" above (root dedupe, skip-with-debug-log for bad entries, depth
  clamping, per-subtree error tolerance).
- `scan_test.go`: temp tree with a repo, a nested repo inside it (must NOT appear), an
  excluded `node_modules/.git` (must NOT appear), a repo just past `scan_depth` (must NOT
  appear), a symlink loop (must not hang), a missing/non-dir `search_paths` entry (skipped,
  others still scanned), `scan_depth: 0` and negative (behave as default 4). No real commands.
- Verify: `go test ./internal/tooling/worktree/ -run Scan`

#### Step 3: Scan into candidates

- In `RepoCandidates`, after the recent-repos loop and before/around zoxide, append
  `scanRepos(gc.Worktree.SearchPaths, depth)` results to `raw`. Existing canonical dedupe
  handles overlap. Empty `SearchPaths` = no scan (zero behavior change for users who don't
  opt in). A scan error never blanks other sources (same contract as § header of that file).
- Verify: `go test ./internal/tooling/worktree/` with `VerifyNoRealCommands`.

#### Step 4: Layout model + built-ins

- `layout.go`: `type Pane struct { Command string; Split string }` (Split empty for the
  first pane; `"vertical"`/`"horizontal"` after), `type Layout struct { Name string; Panes []Pane }`.
  Built-in registry: `opencode`, `claude`, `claude-nvim`, `nvim`. Pane commands reuse
  `AICoder.Command()` and a neovim command constant. An `EnsureInstalled`-style check per
  pane so a layout referencing a missing tool fails with an actionable message before the
  window is touched.
- `ResolveLayout(layoutName, aiAlias string, gc)`: implements the "Layout resolution
  contract" above — explicit layout name, else explicit AI alias (flag/env via
  `ResolveAIAlias`) as a derived single-pane layout, else `DefaultLayout`, else
  `default_ai` derivation, else opencode.
- `layout_test.go`: each built-in resolves; the full precedence ladder (each rule beats
  the ones below it); empty everything resolves to opencode single-pane; a config with
  only `default_ai: claude` derives a single-pane claude layout; `--ai`-style alias beats
  `default_layout`; unknown name errors.
- Verify: `go test ./internal/tooling/worktree/ -run Layout`

#### Step 5: tmux split wrapper

- Add `SplitWindow(window, workdir, direction string) error` (or minimal equivalent) to
  `internal/apps/tmux/tmux.go`, going through `Base.ExecCommand` like the other methods —
  never a raw `exec.Command`. Map `vertical` → `split-window -h` (panes side by side) /
  `horizontal` → `split-window -v`, targeting the window, with `-c workdir`.
- Test mocked in `tmux_test.go`.
- Verify: `go test ./internal/apps/tmux/`

#### Step 6: Build the window from a layout

- Replace `createWindowAndLaunch`'s hardcoded single pane: create the window for pane 0,
  send pane 0's command, then for each subsequent pane `SplitWindow` + send its command,
  finally select pane 0. Thread the chosen `Layout` from `create`/`CreateAt` down through
  `launchWindow`.
- **Failure semantics (make rollback deterministic, don't leave a half-built window):**
  the per-pane install check from Step 4 runs for _every_ pane before any tmux call, so
  the common "tool missing" case fails before the window exists. If a tmux call itself
  fails partway (window created, or one of N panes split/launched, then a later pane
  errors): kill the partially built window (`KillWindow`, best-effort) and roll back the
  worktree via `Git.RemoveWorktree` — the same end state as today's single-pane rollback,
  never a window with some panes up and the worktree half-created. In the `useRepoSession`
  path, kill only the window, never the shared repo session (other worktrees live there).
- Repair (`Repair`/`RepairInRepo`/`ensureWindow`, and the TUI's auto-repair in
  `attachToWindowCmd`) rebuilds the layout per the resolution contract: honors an explicit
  `--ai`/`--layout` when given, else `default_layout`, else AI derivation — never a
  remembered per-worktree layout (out of scope).
- Verify: `go test ./internal/tooling/worktree/` — layout drives the mocked tmux calls in
  order; `VerifyNoRealCommands`.

#### Step 7: `N` layout picker in the TUI

- `create_flow.go`: add a `createLayoutPick` mode. `n` = today's path with the default
  layout. `N` = after name (or before create), open a `FuzzyPicker` of layout names with
  the cursor on the default; Enter selects, Esc cancels the whole create. `createFn` gains
  a layout argument (seam in `model.go`), passed to `CreateAt`.
- Add `N` to the hint bar (`model.go:1099`) and `?` help (`model.go:1126`).
- TUI tests: `n` uses default silently; `N` reaches the picker and the selected layout
  flows to the `createFn` seam; Esc at the layout step cancels cleanly.
- Verify: `go test ./internal/tui/worktree/`

#### Step 8: CLI parity + docs

- `dg wt new` and `dg wt repair` gain an optional `--layout` flag, mutually exclusive
  with `--ai` (`cobra.MarkFlagsMutuallyExclusive` on each command's own flag set).
  Omitted flags resolve per the contract, so behavior without flags is unchanged. Update
  both commands' `Long` help (the repair help currently documents the old AI-only
  precedence) to the new contract.
- **Use command-local flag variables for the new pair, not the existing package-level
  globals.** Today `cmd/worktree.go:277-280` declares `aiFlag`/`forceFlag`/`repoFlag` as
  shared package vars, and `aiFlag` is already bound to _both_ `worktreeCreateCmd` (`:293`)
  and `worktreeRepairCmd` (`:306`). `MarkFlagsMutuallyExclusive` is per-command and works
  on flag names regardless of the backing var, so exclusivity is not broken by sharing —
  but a shared `aiFlag`/`layoutFlag` is a state-bleed smell that bites tests running more
  than one command in-process without resetting globals. Declare
  `createAIFlag`/`createLayoutFlag` and `repairAIFlag`/`repairLayoutFlag` (or a small
  per-command flag struct) so each command owns its state. Migrating the existing shared
  `aiFlag` to command-local is a small, in-scope cleanup here; leave `forceFlag`/`repoFlag`
  alone (not touched by this cycle).
- Update `docs/spec.md` (scan config + layouts + resolution contract), `README.md` if it
  lists worktree flags, and ROADMAP (flip these items; add user-defined custom layouts
  and per-worktree layout memory as the deferred follow-ups). No worktree guide exists in
  `docs/guides/` — do not invent one in this cycle.
- Verify: full `go test ./...` + `make lint`.

---

## 6. Verification Plan

### Automated Verification

```bash
go test ./internal/tui/... ./internal/tooling/worktree/ ./internal/config/ ./internal/apps/tmux/
go test ./... -cover
make lint
```

### Manual Verification

Run in an isolated sandbox (throwaway `$HOME`/XDG, throwaway git repos, a dedicated tmux
session) — same method as the create-flow cycle.

1. [x] Set `worktree.search_paths` to a dir containing a repo you've never used → `dg wt ui`
       → `n` → that repo appears in the picker without typing its full path
2. [x] A repo nested inside another repo under a search path does NOT appear as its own entry;
       `node_modules`/excluded dirs never appear; a repo deeper than `scan_depth` does not appear
3. [x] Free-typing a path still works and still resolves to the repo root
4. [x] `n` with no `default_layout` set and `default_ai: opencode` → single-pane opencode window
       (identical to pre-cycle behavior)
5. [x] `n` with `default_layout: claude-nvim` → window opens with claude + nvim in two vertical panes
6. [x] `N` → layout picker appears with the default highlighted → pick `nvim` → window opens nvim only
7. [x] `N` → Esc at the layout step returns to the dashboard unchanged; no worktree created
8. [x] A layout referencing an uninstalled tool → actionable error before any tmux window
       is created (per-pane precheck), worktree not created
       8b. [x] Simulate a mid-build tmux failure (e.g. second pane's `SplitWindow` errors) →
       the partially built window is killed and the worktree rolled back; in a shared
       repo session, sibling worktrees' windows survive
9. [x] `dg wt new <name>` still works; `--layout` selects a layout; omitting it uses the default;
       `--layout` + `--ai` together → error
10. [x] With `default_layout: claude-nvim`: `dg wt repair <name>` rebuilds claude-nvim;
        `dg wt repair <name> --ai opencode` rebuilds single-pane opencode (flag beats config)
11. [x] TUI auto-repair (attach to a worktree whose window was killed) rebuilds per the
        same resolution as create

### Regression Check

- [x] `dg wt ui` attach/destroy/repair/filter/diff and the existing `n` common case unchanged
- [x] `N` was previously unbound in normal mode (verified 2026-07-17 — no `"N"` case in
      `model.go`); confirm it doesn't shadow filter-mode or popup-mode key handling
- [x] Old `global_config.yaml` (no `search_paths`/`default_layout`) loads and behaves as before
- [x] `default_ai` alone still selects the coder (via derived layout); `--ai` and
      `DEVGETA_WORKTREE_AI` still behave exactly per the pre-cycle precedence
- [x] `dg wt new/rm/ls/repair` unchanged aside from the additive `--layout` flag and help text

---

## 7. Risks & Trade-offs

| Risk                                                          | Likelihood | Mitigation                                                                                        |
| ------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------- |
| Scan slow on large/deep trees or network mounts               | Med        | Depth cap (default 4), exclude-list prune, `SkipDir` at repo boundary, skip symlinked dirs        |
| Symlink loops hang the walk                                   | Med        | Do not follow symlinked directories; test a loop fixture                                          |
| Nested repos (submodules) hidden when genuinely wanted        | Low        | Free-typed path still resolves any repo root; documented; prune is the intended behavior          |
| Layout config change breaks old configs                       | Low        | Additive optional fields; `default_ai` derivation keeps old behavior; legacy round-trip test      |
| Multi-pane window build partially fails, leaves broken window | Med        | Roll back worktree on any pane failure (as today); validate tool install before touching tmux     |
| Reaching around the tmux wrapper for split (CLAUDE.md § 6)    | Low        | Add `SplitWindow` to the wrapper; no raw `exec.Command` in worktree code                          |
| Two new config knobs confuse the config surface               | Low        | Docs with concrete examples; sensible empty-defaults (scan off, layout derived from `default_ai`) |

### Trade-offs Made

- **Prune at `.git` vs. list everything:** the picker's only valid answer is a repo root,
  so pruning is higher-signal and matches ghq. Nested repos are recoverable via free-typed
  path. (twm/tmux-sessionizer keep descending and surface exactly the noise the requester
  wants gone.)
- **`N` picker vs. mandatory third step vs. no override:** a mandatory layout step taxes
  the common case; no override can't express three workflows. `n` default / `N` pick
  mirrors twm's `-l` — the one surveyed tool with an invocation-time layout picker — and
  costs the extra keystroke only when deviating.
- **Built-in layouts in config vs. per-repo scripts:** per-repo startup files
  (`.tmux-sessionizer`, `.twm.yaml`, `.gwq.toml`) are code-execution vectors those projects
  themselves flag. devgeta keeps layouts as data in its own config, consistent with the
  security rules in CLAUDE.md § 4.
- **Empty search-paths default:** no repos are hardcoded; the scan is opt-in. Users who
  don't configure it keep today's exact candidate set — no surprise scanning of `$HOME`.

---

## 8. Cross-Model Review Notes

- [x] Domain context clear?
- [x] Engineer context sufficient?
- [x] Objective unambiguous?
- [x] Scope is actually locked?
- [x] Steps are actionable?
- [x] Verification is executable?
- [x] Risks are realistic?

**Reviewer notes:**
2026-07-17 external review (risk: Medium), resolutions:

- IMPORTANT — `default_layout` "supersedes `default_ai`" was underspecified vs. the
  existing `--ai`/env/config precedence used by create, repair, and TUI auto-repair.
  Resolved: explicit "Layout resolution contract" added to § 5; existing AI chain
  preserved verbatim, `default_layout` slotted between env and `default_ai`;
  `--layout`+`--ai` mutually exclusive.
- IMPORTANT — repair semantics vs. layouts undefined (`repair --ai`, auto-repair).
  Resolved: repair follows the same contract as create; flags beat config; per-worktree
  layout memory explicitly out of scope (§ 4) and noted as a ROADMAP follow-up; repair
  `Long` help text update added to Step 8.
- IMPORTANT — `search_paths`/`scan_depth` lacked validation and failure policy.
  Resolved: "Scanner validation rules" added to § 5 (skip-with-debug-log, root dedupe,
  depth clamp to default 4, per-subtree error tolerance) with matching test cases in
  Step 2. `scan_depth: 0` = default, not disabled — the only off-switch is empty
  `search_paths`.
- MINOR — Step 8 referenced a "worktree guide" that doesn't exist in `docs/guides/`.
  Resolved: doc targets pinned to `docs/spec.md`, `README.md`, `ROADMAP.md`.
- MINOR — no keymap-conflict check for `N`. Verified `N` is currently unbound in
  `model.go` normal mode; regression check added.

2026-07-17 second external review (risk: Medium), resolutions:

- IMPORTANT — `--layout`/`--ai` exclusivity risky given shared package-level flag vars.
  Verified: `cmd/worktree.go:277-280` declares `aiFlag`/`forceFlag`/`repoFlag` as package
  globals and `aiFlag` is bound to both create (`:293`) and repair (`:306`). Resolved:
  Step 8 now requires command-local flag vars for the new `--ai`/`--layout` pair (migrating
  the shared `aiFlag` to command-local as an in-scope cleanup), with a note that
  `MarkFlagsMutuallyExclusive` itself is per-command and unaffected by sharing — the real
  risk is test-time state bleed, not broken exclusivity.
- MINOR (suggestion 2) — multi-pane failure rollback was "as today" (single-pane only).
  Resolved: Step 6 "Failure semantics" spells out per-pane precheck before any tmux call,
  kill-partial-window + worktree rollback on mid-build failure, and never killing a shared
  repo session; verification case 8b added.
- MINOR (suggestion 3) — unclear whether excludes apply to configured roots. Resolved:
  Scanner validation rules now state excludes apply to walk-discovered dirs only, not to
  explicitly configured roots (a root named `vendor` is still scanned).
- Q3 (symlinked roots) — resolved in Scanner validation rules: a configured root that is a
  symlink is resolved and scanned once; only symlinks encountered _during traversal_ are
  skipped (loop avoidance).

---

## Notes for Implementers

- **Cycle document is your spec.** Update it if requirements change, but don't change scope
  without calling it out.
- **Commit after each step.** Run `/smart-commit` once a step's verify check passes.
- **Verification must pass before "done."** Automated tests + manual checks + regression check.
- **Route tmux through the wrapper.** The pane split is a new wrapper method, never a raw
  `exec.Command` (CLAUDE.md § 6).
- **Unconfigured must equal today.** Empty `search_paths` = no scan; empty `default_layout`
  = single-pane layout derived from `default_ai`. Guard this with the regression checks.
- **If you hit a risk, escalate immediately.**
