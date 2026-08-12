# Cycle: `dg ws` — selectable panes, a wider list, and a refresh that stops burning CPU

**Date:** 2026-08-12
**Estimated Duration:** ~6 hours
**Status:** Draft

---

## 1. Domain Context

`dg ws` (`internal/tui/worktree`) is the workspace dashboard: a two-pane Bubble
Tea TUI listing every repo's git worktrees plus standalone tmux sessions on the
left, and the selected worktree's branch diff on the right. Rows nest
repo → worktree → pane, or session → pane, with each row carrying an agent-state
dot ([ADR-0003](../../decisions/ADR-0003-sessions-in-workspace-dashboard.md),
[ADR-0005](../../decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md),
[ADR-0008](../../decisions/ADR-0008-agent-state-on-every-pane-row.md)).

Three problems surfaced in real use, all reported together:

1. **Pane rows can't be selected.** `enter` on a pane row does nothing, so
   reaching a specific agent pane means backing out to the parent row.
2. **Long names are unreadable.** The left pane is 35 columns by default and
   only resizable by mouse drag, so worktree and session names truncate.
3. **The dashboard is a constant CPU load.** Measured at ~0.5s of git/tmux
   process work every 3 seconds while idle, plus an un-debounced full branch
   diff on every `j`/`k`.

Problem 3's design is governed by a new
[ADR-0024](../../decisions/ADR-0024-the-dashboard-refreshes-fast-and-slow-state-separately.md)
— read it first; it carries the measurements, the cadence decision, and three
optimizations that were measured and rejected (including the two most obvious
ones).

Two further defects were found while measuring and are folded in, because both
live in the same left-pane render path this cycle already touches:

4. **Stale diffs render.** The `diffMsg` handler applies any arriving diff
   without checking it belongs to the selected row, so fast navigation can leave
   the pane showing another worktree's diff until the next refresh.
5. **The left list has no scroll viewport.** `renderLeft` emits every row and
   `padLines` hard-truncates to `height-2`, so with more rows than terminal
   height the tail is invisible — and `navigableIndices` still moves the cursor
   into that invisible region. `internal/tui/inventory/model.go` already solved
   this with a `visibleWindow` helper that per
   [CLAUDE.md §12](../../../CLAUDE.md) belongs in `internal/tui/components`.

---

## 2. Engineer Context

**Relevant files:**

- `internal/tui/worktree/model.go` — the dashboard model (~1800 lines). Row
  selection (`selectedStatus:466`, `selectedSession:481`), key dispatch
  (`handleKey:766`), refresh (`tickCmd:368`, `loadCmd:336`, `sessionsLoadCmd:358`,
  `applyStatuses:503`), diff dispatch (`computeDiffCmd:387`,
  `selectionChangedCmd:458`), attach (`handleAttach:1053`), left-pane render
  (`renderLeft:1327`), hint bar (`renderHint:1623`), help (`renderHelpPopup:1746`),
  layout (`renderDashboard:1277`, `padLines:1778`).
- `internal/tui/worktree/tree.go` — row model and `buildRows`/`leafIndices`.
- `internal/tooling/worktree/worktree.go` — `List():960`, `ListSessions():1014`,
  `enumerateWorktrees():902`, `forEachKnownRepo():853`,
  `knownRepoAnchorGroups():790`.
- `internal/apps/tmux/tmux.go` — `PaneState:283`, `SwitchToWindow:459`,
  `SelectPane:649`, `ClearAgentStateForWindow:426`.
- `internal/tooling/task/branchdiff.go` — `collectWorktreeDiff:70` (the two
  full-tree diffs), `BranchDiffAt:115`.
- `internal/tui/components/` — shared TUI toolkit; gains `visibleWindow`.
- `internal/tui/inventory/model.go:240` — the `visibleWindow` to move out.

**Key patterns:**

- Every external binary goes through its app wrapper, never a raw
  `exec.Command` (CLAUDE.md §6). Pane switching therefore gets a tmux wrapper
  method rather than two composed calls from the model.
- Every I/O the model performs is an injected seam (`m.attachFn`,
  `m.diffFn`, …) so tests never touch a real tmux or git.
- Tests use `testutil.MockApp` and always end with
  `testutil.VerifyNoRealCommands(t, mockApp.Base)`. See
  [testing-patterns.md](../../guides/testing-patterns.md).

**Test command** (changed packages + their direct importers, from the `go list`
query in CLAUDE.md §6):

```bash
go test ./internal/tui/worktree/ ./internal/tui/components/ ./internal/tui/inventory/ \
        ./internal/tooling/worktree/ ./internal/tooling/task/ ./internal/apps/tmux/ ./cmd/
make lint
```

---

## 3. Objective

`dg ws` lets you select a pane row directly, toggle the left pane to double
width with `e`, scrolls the list instead of hiding rows past the bottom, never
renders a diff belonging to a row you left, and costs roughly a tenth of its
current idle CPU.

---

## 4. Scope Boundary

### In Scope

- [ ] `enter` on a pane row switches the tmux client to that exact pane
- [ ] `e` toggles the left pane between default (35) and double (70, still
      clamped to 60% of the terminal)
- [ ] A scroll viewport for the left list, via a `visibleWindow` moved into
      `internal/tui/components` and shared with `internal/tui/inventory`
- [ ] The fast/slow refresh split from ADR-0024, including merging the two
      `tmux list-panes -a` scans into one
- [ ] Diff recomputes on debounced selection change, on the slow tick, and on an
      explicit `ctrl+r` refresh; never on the 3s tick
- [ ] Stale `diffMsg` (path ≠ selected row) is dropped
- [ ] The two full-tree diffs inside `collectWorktreeDiff` run concurrently
- [ ] `config.Load()` inside `knownRepoAnchorGroups` is cached, invalidated on create
- [ ] Hint bar and help popup document `e` and the refresh key

### Explicitly Out of Scope

- `d`/`D`/`r`/`R` on a pane row — they stay inert (decided: killing a pane could
  kill an agent mid-turn, and a pane is not a workspace object devgeta manages)
- Persisting the left-pane width to `global_config.yaml` — per-session only, no
  config-format change
- Anchor dedup before exec, a filesystem pre-filter for repos with no worktrees,
  and caching `DefaultBranchIn` — all three measured and rejected in ADR-0024
- Any change to how agent state is stored or aggregated (ADR-0005/0008 stand)

**Scope is locked.**

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                    | Description                                                            |
| ------ | -------------------------------------------- | ---------------------------------------------------------------------- |
| Modify | `internal/apps/tmux/tmux.go`                 | Add `SwitchToPane`, `ClearAgentStateForPane`                           |
| Modify | `internal/apps/tmux/tmux_test.go`            | Cover both new wrappers                                                |
| Modify | `internal/tui/worktree/model.go`             | `selectedPane`, `handleSwitchToPane`, `e` key, debounce, refresh split |
| Modify | `internal/tui/worktree/model_test.go`        | Pane select, `e` toggle, debounce, stale-diff drop, viewport           |
| Create | `internal/tui/components/viewport.go`        | `VisibleWindow(rowsLen, cursor, height) (start, end int)`              |
| Create | `internal/tui/components/viewport_test.go`   | Boundary cases (cursor at 0, at end, rows < height)                    |
| Modify | `internal/tui/inventory/model.go`            | Delete local `visibleWindow`, call the shared one                      |
| Modify | `internal/tooling/worktree/worktree.go`      | Split `List()` into `ListWorktreesOnly` + `RefreshState`; cache config |
| Modify | `internal/tooling/worktree/worktree_test.go` | Cover the split and the single merged tmux scan                        |
| Modify | `internal/tooling/task/branchdiff.go`        | Run the colored diff and numstat concurrently                          |
| Modify | `docs/apps/claude.md` / `docs/spec.md`       | Document the new keys, if `dg ws` keys are listed there                |
| Modify | `cmd/workspace.go`                           | Mention `e` in the command's long help                                 |
| Modify | `docs/decisions/README.md`                   | Index ADR-0024                                                         |

### Step-by-Step

#### Step 1: tmux wrappers for pane switching and per-pane state clear

- `SwitchToPane(session, window, paneID string) error` — `SwitchToSession`, then
  `select-window`, then `select-pane -t <paneID>`. One wrapper rather than the
  model composing `SwitchToWindow` + `SelectPane`, per CLAUDE.md §6.
- `ClearAgentStateForPane(paneID string) error` — unsets `@dg_agent_state` on
  that pane only (other panes' states must survive; that granularity is what
  ADR-0008 exists for) and the window-level `@dg_window_agent_state` mirror,
  mirroring `ClearAgentStateForWindow`'s two writes at pane scope.
- Verify: `go test ./internal/apps/tmux/`

#### Step 2: `enter` selects a pane row

- `selectedPane() (tmux.PaneState, bool)` in `model.go`, mirroring
  `selectedStatus`/`selectedSession`.
- New seams `switchToPaneFn`, `clearAgentStateForPaneFn` wired in `newModel`.
- `handleSwitchToPane()` next to `handleAttach`: the same
  `os.Getenv("TMUX") == ""` → `notInsideTmuxStatus` guard the other two use,
  best-effort state clear, then switch and quit on success.
- `handleKey`'s `enter` case gains a `selectedPane` branch before `handleAttach`.
- Verify: `go test -run 'Pane' ./internal/tui/worktree/`

#### Step 3: `e` toggles left-pane width

- `leftPaneWide bool` on the model; `e` flips it and sets `leftPaneWidth` to
  `defaultLeftPaneWidth` or `min(defaultLeftPaneWidth*2, m.safeMaxLeft())`.
  A bool rather than comparing widths, so it still behaves after a mouse drag
  has left an arbitrary width.
- `WindowSizeMsg` re-derives the width from the bool so a resize can't leave it
  past the 60% cap.
- Add to `renderHint` and `renderHelpPopup`.
- Verify: `go test -run 'Width|Hint|Help' ./internal/tui/worktree/`

#### Step 4: Shared scroll viewport

- Move `visibleWindow` from `internal/tui/inventory/model.go:240` into
  `internal/tui/components/viewport.go` as exported `VisibleWindow`; inventory
  calls the shared one (no behavior change there).
- `renderLeft` renders only `rows[start:end]`, and the cursor-highlight
  comparison shifts by `start`. `isLastChild` must keep scanning the full row
  slice, not the window, or the tree connector breaks at the window edge.
- Verify: `go test ./internal/tui/components/ ./internal/tui/inventory/ ./internal/tui/worktree/`

#### Step 5: Split `List()` in the manager

- `ListWorktreesOnly() ([]WorktreeStatus, error)` — the git enumeration alone.
- `RefreshState(statuses []WorktreeStatus) ([]WorktreeStatus, []SessionStatus, error)`
  — one `PaneStates()` scan plus one `ListSessions()`, layering tmux state onto
  the given statuses and returning the standalone-session list from the same
  scan. This is where the two duplicate `list-panes -a` scans become one.
- `List()` and `ListSessions()` keep their signatures, recomposed from the two
  halves so no caller changes and the paths cannot drift.
- Cache the `config.Load()` in `knownRepoAnchorGroups`, invalidated by `CreateAt`
  (the only path that writes recent-repos).
- Verify: `go test ./internal/tooling/worktree/`

#### Step 6: Fast and slow ticks in the model

- Two tick commands and two messages: fast (3s → `RefreshState`) and slow
  (30s → `ListWorktreesOnly`, then a `RefreshState` on the result).
- Create/remove/repair success paths trigger the slow path directly, so your own
  actions never wait on the 30s timer.
- Verify: `go test -run 'Tick|Refresh' ./internal/tui/worktree/`

#### Step 7: Debounce the diff, and drop stale diffs

- `diffGen int` on the model. `j`/`k` bump it and dispatch a 180 ms
  `tea.Tick` carrying that generation; the handler dispatches
  `selectionChangedCmd` only if the generation still matches — a later keypress
  invalidates an earlier pending one.
- The `diffMsg` handler drops any message whose `path` is not the selected
  worktree's path.
- Remove the unconditional `selectionChangedCmd` from `applyStatuses`; the slow
  tick and the explicit refresh key drive it instead. PR-title lookups ride the
  same debounce, so fast navigation stops firing a burst of `gh` calls.
- Verify: `go test -run 'Debounce|Diff' ./internal/tui/worktree/`

#### Step 8: Parallel diff execs

- In `collectWorktreeDiff`, run the `--color=always` diff and the `--numstat`
  diff concurrently and join. Both are reads of the same commit range; neither
  mutates.
- Verify: `go test ./internal/tooling/task/`

#### Step 9: Docs

- ADR-0024 to `docs/decisions/README.md`.
- `e` and the refresh key into `cmd/workspace.go`'s long help and any `dg ws`
  key list in `docs/spec.md`.
- Mark this cycle Done.

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/tui/worktree/ ./internal/tui/components/ ./internal/tui/inventory/ \
        ./internal/tooling/worktree/ ./internal/tooling/task/ ./internal/apps/tmux/ ./cmd/
make lint
```

### Manual

1. `dg ws` from inside tmux, on a worktree whose window has 2+ agent panes.
   Expand with `l`, move to a pane row, press `enter` → lands in that exact
   pane, not the window's first one.
2. Press `e` → left pane doubles, long names fully readable. Press `e` again →
   back to 35. Drag the divider, then press `e` → still toggles cleanly.
3. Shrink the terminal so rows exceed its height → the list scrolls with the
   cursor instead of the tail vanishing; `j` past the bottom stays visible.
4. Hold `j` through a dozen rows → the diff pane settles once, on the row you
   land on, and never shows a different worktree's diff.
5. Leave the dashboard open and idle; watch `top`/Activity Monitor → git process
   churn is visibly intermittent rather than continuous.
6. Create a worktree with `n` → it appears immediately, not after 30s.
7. `dg wt list` and shell completion still work (they use the recomposed `List()`).

### Regression Check

- `dg ws` with zero worktrees still shows the "press n to create one" guidance.
- `dg ws` outside tmux still reports `not inside tmux` for `enter` on all three
  row kinds.
- Agent-state dots still update within ~3s of an agent changing state — the
  whole point of keeping the fast tick fast.
- `dg wt` create/remove/repair unaffected.

---

## 7. Risks & Trade-offs

| Risk                                                       | Likelihood | Mitigation                                                                                      |
| ---------------------------------------------------------- | ---------- | ----------------------------------------------------------------------------------------------- |
| Splitting `List()` drops a field on one of the two paths   | Med        | `List()` is recomposed from both halves, so its existing tests cover the composition end-to-end |
| Viewport offset breaks cursor highlight or tree connectors | Med        | `isLastChild` scans all rows, not the window; explicit tests at both window edges               |
| Debounce swallows a diff if a tick lands mid-debounce      | Low        | Generation counter is the only gate; the slow tick dispatches independently                     |
| 30s slow tick feels stale for externally-created worktrees | Accepted   | ADR-0024 trade-off; devgeta's own mutations bypass the timer                                    |
| Per-pane state clear leaves the window mirror wrong        | Low        | Mirror is display-only and gated on `window_active_clients == 0`, so it is hidden once attached |

### Trade-offs Made

- **30s vs 3s for git state** — accepted staleness for a ~10× idle-cost cut.
- **A bool for wide/narrow instead of stepped widths** — `e` was chosen over a
  `>`/`<` pair, so there are exactly two widths and no in-between.
- **Two refresh paths instead of one** — more model state, but the single path
  was a compromise between a cheap tmux query and sixteen git processes.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope actually locked?
- [ ] Steps actionable?
- [ ] Verification executable?
- [ ] Risks realistic?

**Reviewer notes:**
(Fill in during review.)
