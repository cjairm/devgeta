# Cycle: `dg ws` — selectable panes, a wider list, and a refresh that stops burning CPU

**Date:** 2026-08-12
**Estimated Duration:** ~6 hours
**Status:** In Progress

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

**Test command** — the changed packages plus their direct importers, from the
`go list` query in CLAUDE.md §6 (`.Imports`/`.TestImports`, never `.Deps`):

```bash
go test ./internal/apps/tmux/ ./internal/apps/registry/ ./internal/tooling/terminal/ \
        ./internal/tui/worktree/ ./internal/tui/components/ ./internal/tui/inventory/ \
        ./internal/tooling/worktree/ ./internal/tooling/task/ ./internal/apps/opencode/ ./cmd/
make lint
```

Three of those are importers rather than packages this cycle edits, and the query
is what put them there: `internal/apps/registry` and `internal/tooling/terminal`
both import `internal/apps/tmux` (`registry.go:27`, `terminal.go:15`), which
Step 1 changes, and `internal/apps/opencode`'s `permissions_test.go` imports
`internal/tooling/worktree` for `BuiltinReviewerChoices()`, which Step 5
changes. Measured cold at 5.7s for the whole list, so there is no reason to
prune it by judging which importer "really" touches the changed symbol.

The root package (`go test .`) is the one direct importer deliberately left out.
The query returns it — `main.go` imports `cmd`, and the root's own test files
import `internal/tooling/worktree` — but those test files cover the embedded
configs and the hook shells, and Steps 1–7 and 9 change nothing under `configs/`,
so per CLAUDE.md §6 including it is 4.8 minutes of pure cost. Step 8 is the
exception, and it needs the full suite rather than the root package alone — see
there for why.

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
- [ ] `e` toggles the left pane between default (35) and double (70), both
      clamped to 60% of the terminal
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

| Action | File Path                                        | Description                                                                                                                                       |
| ------ | ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| Modify | `internal/apps/tmux/tmux.go`                     | Add `SwitchToPane`, `ClearAgentStateForPane`; home for `AggregateAgentState` + `AgentState*`                                                      |
| Modify | `internal/apps/tmux/tmux_test.go`                | Cover both new wrappers, the mirror recompute, the failed clear, a duplicate window name across sessions                                          |
| Modify | `internal/tui/worktree/model.go`                 | `selectedPane`, `handleSwitchToPane`, `e` key, `ctrl+r` refresh, `Update` wrapper + debounce, refresh split, `stateGen` ordering, `repairDoneMsg` |
| Modify | `internal/tui/worktree/model_test.go`            | Pane select, `e` toggle, `ctrl+r` refresh, debounce per cursor-moving path, stale-diff drop, refresh ordering, repair refresh, viewport           |
| Create | `internal/tui/components/viewport.go`            | `VisibleWindow(rowsLen, cursor, height) (start, end int)`                                                                                         |
| Create | `internal/tui/components/viewport_test.go`       | Boundary cases (cursor at 0, at end, rows < height)                                                                                               |
| Modify | `internal/tui/inventory/model.go`                | Delete local `visibleWindow`, call the shared one                                                                                                 |
| Modify | `internal/tooling/worktree/worktree.go`          | Split `List()` into `ListWorktreesOnly` + `ScanTmuxState` + `RefreshState`; cache config; alias the moved agent-state helpers                     |
| Modify | `internal/tooling/worktree/worktree_test.go`     | Cover the split and the single merged tmux scan                                                                                                   |
| Modify | `internal/tooling/task/branchdiff.go`            | Run the colored diff and numstat concurrently                                                                                                     |
| Modify | `internal/commands/mock.go`                      | Mutex on `MockBaseCommand`, locked in `ExecCommand` only, so a concurrent exec path is testable at all; doc comment states the field contract     |
| Modify | `internal/commands/mock_test.go`                 | Concurrent `ExecCommand` cases that fail if the locking boundary deadlocks                                                                        |
| Modify | `internal/tooling/reviewjournal/manager_test.go` | `newFakeRepoAt`'s comment reason — the recorder is no longer unsynchronized                                                                       |
| Modify | `internal/tooling/task/branchdiff_test.go`       | Positional mock queue → argument-aware `ExecCommandFn`; concurrent success and failure cases                                                      |
| Modify | `docs/apps/claude.md` / `docs/spec.md`           | Document the new keys, if `dg ws` keys are listed there                                                                                           |
| Modify | `cmd/workspace.go`                               | Mention `e` and `ctrl+r` in the command's long help                                                                                               |
| Modify | `docs/decisions/README.md`                       | Index ADR-0024                                                                                                                                    |

### Step-by-Step

#### Step 1: tmux wrappers for pane switching and per-pane state clear

- `SwitchToPane(session, window, paneID string) error` — `SwitchToSession`, then
  `select-window`, then `select-pane -t <paneID>`. One wrapper rather than the
  model composing `SwitchToWindow` + `SelectPane`, per CLAUDE.md §6.
- `ClearAgentStateForPane(paneID string) error` — unsets `@dg_agent_state` on
  that pane only (other panes' states must survive; that granularity is what
  ADR-0008 exists for). The window-level `@dg_window_agent_state` mirror is
  **recomputed, not cleared**: from the same `PaneStates()` scan, find the entry
  whose `PaneID` is `paneID`, aggregate the remaining panes that share **both**
  its `Session` and its `Window` by ADR-0005's precedence, and write that value
  when it is `idle`/`blocked`/`error`, unsetting the mirror only when nothing is
  left wanting attention (`busy` never goes in the mirror — ADR-0005). Unsetting it
  outright would drop the status-bar flag for a sibling pane that is still
  blocked, and "attaching hides the flag anyway" is not a defence: ADR-0005
  records that `window_active_clients` suppresses the flag only while the window
  is attached, so a wrong mirror value resurfaces on detach.
- **Filter that aggregate by `(Session, Window)`, not by window name alone.**
  tmux allows the same window name in two sessions, and this package already
  refuses to assume otherwise: `ClearAgentStateForWindow`'s comment says a window
  name "is unique in practice, but this doesn't assume it" (`tmux.go:413`). The
  pane path can be stricter than that window path, because the pane ID hands it
  the session for free. The ID is unique server-wide, so the write target itself
  is never in doubt — a window-scoped `set-option` aimed at a pane ID lands on
  the window containing that pane and no other — and the scan entry it matches
  carries `Session` next to `Window` (`PaneState`, `tmux.go:283`). Key the
  aggregate on the name alone, though, and a same-named window in another session
  contributes its panes, so the right window gets a value computed from someone
  else's agents: a foreign `blocked` pins a "wants you" flag on a window with
  nothing pending, and a foreign `idle` holds the flag up after the real window's
  last state was cleared. This is not ADR-0008's rejected "stop keying worktree
  rows by window name" — that is about which filesystem-sourced row a window
  belongs to, where name keying is correct. This is a per-pane write with a
  stricter key already in hand.
- "Remaining" means _every pane whose state is still on the pane_, not "every
  pane except the target", so **a failed clear keeps the target pane in the
  aggregate**. The unset is best-effort — the same treatment
  `ClearAgentStateForWindow` already gives it (records the first error, keeps
  going, `tmux.go:426`) — and when it fails the pane's
  `@dg_agent_state` is still sitting there, so dropping it from the recompute
  writes a mirror the panes contradict: the flag disappears, or a sibling's
  `idle` downgrades a `blocked` that is still live. Aggregate the pane's
  pre-clear state with the others in that case and return the error for logging.
  Do not rescan to resolve it: a second `PaneStates()` races the same way and
  costs exactly the scan ADR-0024 is trying to remove. If the pane had in fact
  closed between the scan and the clear, this over-reports for that window until
  the next agent write or the next attach (which unsets the mirror outright) —
  the safe direction, because a lost flag is a blocked agent nobody sees.
- The precedence rule stays in exactly one place, which means moving it:
  `AggregateAgentState` and the `AgentState*` constants go from
  `internal/tooling/worktree` to `internal/apps/tmux`. They are the vocabulary
  of the `@dg_agent_state` pane option, and `tmux` cannot import `worktree` —
  the dependency already runs the other way. `worktree` keeps thin aliases so no
  existing caller (`internal/tui/components/statusdot.go`) or test changes.
- Verify: `go test ./internal/apps/tmux/`, including a case where the per-pane
  unset fails and the mirror still reports that pane's state, not the sibling
  aggregate, and a case where two sessions hold a window of the same name and the
  other session's panes are left out of the aggregate (a `blocked` pane in the
  same-named foreign window must not set the target window's mirror).

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
  `min(defaultLeftPaneWidth, m.safeMaxLeft())` or
  `min(defaultLeftPaneWidth*2, m.safeMaxLeft())`. A bool rather than comparing
  widths, so it still behaves after a mouse drag has left an arbitrary width.
- **Clamp both targets, not just the wide one.** `safeMaxLeft()` is
  `max(int(width*0.60), minLeftPaneWidth)` (`model.go:596`), which sits _below_
  the default 35 for every terminal narrower than 59 columns — 34 at 58, 30 at
  50, 24 at 40, 21 at 36, and the 20 floor at 33 and under. A bare
  `defaultLeftPaneWidth` on the narrow branch would hand back exactly what
  `WindowSizeMsg` already clamps away today (`model.go:611`): at 50 columns the
  left pane takes 70% of the terminal instead of the capped 60%, and at 36 or
  fewer `rightPaneWidth()` (`:600`) floors to 0, so `renderDashboard` falls into
  its `rpw <= 0` list-only fallback (`:1282`). That fallback degrades cleanly —
  no crash, no negative width — so the user-visible cost is the diff pane
  disappearing after pressing `e` twice on a small terminal, and not coming
  back. With both targets clamped, a narrow terminal simply collapses the two
  widths onto one value (both 21 at width 36) — the cap binds, so there is
  nothing to toggle, which is the right outcome.
- `WindowSizeMsg` re-derives the width from the bool so a resize can't leave it
  past the 60% cap.
- Add to `renderHint` and `renderHelpPopup`.
- Verify: `go test -run 'Width|Hint|Help' ./internal/tui/worktree/`, including a
  narrow `WindowSizeMsg` (width 36) followed by both toggles, asserting
  `leftPaneWidth` is 21 on each branch rather than 35 on the default one, and
  that `rightPaneWidth()` stays above 0 so the diff pane survives.

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
- `ScanTmuxState() (StateLayer, error)` — exactly one `PaneStates()` scan plus
  one `tmux list-sessions`, returning the tmux-derived layer: that scan's panes
  grouped by window name, and the standalone-session list.
- `RefreshState(statuses []WorktreeStatus) ([]WorktreeStatus, []SessionStatus, error)`
  — `ScanTmuxState` then apply the layer. Applying treats `statuses` as
  read-only and returns a **new** slice; today's `List()` writes those fields in
  place, which is only safe because it owns the slice it just enumerated. Step 6
  depends on this.
- The session filter and aggregation move out of `ListSessions()` into the apply
  half so they work off an already-taken scan. Without that move there is no
  saving at all: `ListSessions()` takes its own `w.Tmux.PaneStates()` scan
  (`worktree.go:1014`), so a `RefreshState` that called it would still run two
  `list-panes -a` scans per refresh.
- `List()` and `ListSessions()` keep their signatures, recomposed from these
  halves so no caller changes and the paths cannot drift.
- Cache the `config.Load()` in `knownRepoAnchorGroups`, invalidated in
  `recordRepoUsed` — the single site that writes recent-repos — rather than at a
  public entry point. `Create` and `CreateAt` both reach it through the shared
  `create()` (via `launchWindowAndRecord`), so hooking invalidation to `CreateAt`
  alone would leave a plain `Create`'s write invisible for the process's whole
  life. Also drop the cache when the global config file's mtime or size changes,
  so a `dg wt new` run in another shell is picked up by a long-running `dg ws`.
- Guard the cache with a mutex. `WorktreeManager` has no mutable receiver state
  today, which is why nothing needed one; the cache is shared state read from
  Bubble Tea command goroutines, and the fast and slow refreshes can overlap.
- Verify: `go test ./internal/tooling/worktree/`

#### Step 6: Fast and slow ticks in the model

- Two tick commands and two messages: fast (3s, tmux only) and slow (30s, git).
- Ownership rule: **no model-owned memory crosses into a command goroutine.**
  The fast command calls `ScanTmuxState()` and nothing else, and its message
  carries the tmux layer; `Update` applies that layer to whatever `m.statuses`
  holds when the message lands, on the one goroutine Bubble Tea runs `Update`
  on. That keeps the invariant the model already relies on — a
  `[]WorktreeStatus` is written only by the producer that built it, and is
  read-only once the model holds it (`m.statuses` is never mutated element-wise
  today, only replaced) — and it removes an ordering bug: a fast command that
  captured `m.statuses` and returned a whole list would both race the renderer
  and overwrite a newer list from the slow path, so a worktree you just created
  could vanish for 30s (verification step 6 is exactly that case).
- The slow command calls `ListWorktreesOnly()` then `RefreshState()` on its own
  fresh slice, and its message replaces `m.statuses` wholesale.
- Devgeta's own mutations must not wait on the 30s timer. The rule this step
  implements is ADR-0024's decision §2, and that ADR is the source of truth for
  it: the git enumeration runs immediately after any create, remove, or repair
  the dashboard performs. Three of the paths already comply — `createdMsg`
  dispatches `loadCmd` (`create_flow.go:467`), `sessionKilledMsg` edits
  `m.sessions` in place, and `sessionCreatedMsg` dispatches `sessionsLoadCmd` —
  and each keeps working once `loadCmd` is the slow load. `attachToWindowCmd`'s
  auto-repair needs nothing on its success path: it attaches and quits the TUI.
  Two paths do not comply and are fixed in the next two bullets: remove, which
  rebuilds the list locally and dispatches nothing (`model.go:1167`), and repair,
  which refreshes nothing at all.
- **Remove dispatches the slow load as well as dropping the row locally.** The
  identity removal specified further down is what makes the row leave the list
  instantly and race-free, but it is not a substitute for the git re-read,
  because a removal changes more than its own row: `removeByRepo` finishes with
  `git worktree prune` at the repo root (`worktree.go:2423` → `PruneWorktreesAt`,
  `git.go:1092`), and prune is repo-wide — deleting one worktree also drops every
  _other_ stale registration in that repo. Those rows are on screen, and nothing
  but a `git worktree list` notices they are gone, so without the load they
  linger for up to 30s where today they clear in 3s. So `deletedMsg` bumps
  `stateGen`, drops its row by identity, and returns the stamped slow load —
  dispatched after the bump, so it captures the new number. That load cannot
  resurrect the row it just removed: the command produces `deletedMsg` only once
  `remove` has returned, so the enumeration starts after the worktree is already
  gone from git. The cost is one extra git enumeration per confirmed delete — the
  same trade the repair bullet makes, on an action that already runs
  `git worktree remove`, `branch -D`, a prune, and a `kill-window`.
- **Repair needs a new message on top of that.** `handleRepair` returns
  a `statusMsg` on every outcome (`model.go:1225`), and the `statusMsg` case only
  sets `m.status` — it triggers no refresh at all, so today the 3s tick is the
  only thing that picks a repair up. After the split the fast tick still covers a
  _successful_ repair, because everything repair changes about a row is tmux-side:
  the apply half writes only `TmuxWindow`, `WindowActive`, `AgentState`, `Panes`
  (`worktree.go:979-982`), and rebuilding the window is all a success does. The
  failure path is what breaks. Repairing a worktree whose directory is gone
  prunes the stale git entry and _then_ returns an error
  (`worktree.go:1965-1979`, reached via `RepairInRepo`), so the row it just
  removed sits on screen for up to 30s where today it clears in 3s. So: a
  `repairDoneMsg{status string}` whose handler sets the status and dispatches the
  stamped slow load **on both outcomes**, from the `r` keybinding
  (`model.go:1220`, `:1223`, `:1225`) and from `attachToWindowCmd`'s auto-repair
  on all four paths where it reports a result instead of quitting
  (`model.go:1114`–`:1124`, which includes two that repaired successfully and
  then failed to attach). The cost is one extra git enumeration per repair
  keypress — an action that already rebuilds a whole tmux window.
- Order those wholesale replacements with a generation counter, `stateGen int`
  on the model. `Update` bumps it whenever it dispatches a slow load; the
  command captures that number and returns it on its message; the handler drops
  any `statusesMsg` whose number is no longer the current one. The rule the fast
  path gets from the ownership bullet above only covers fast-versus-slow — two
  slow loads can still overlap, and without a counter the older one wins: the
  30s timer's load starts, you press `n`, the create finishes and its own slow
  load lands with the new worktree, then the timer's older git snapshot lands
  and replaces the list without it. The worktree disappears for another 30s,
  which is exactly what manual verification step 6 checks. The same race exists
  today (`handleCreateSuccess` dispatches `loadCmd`, `create_flow.go:467`, while
  the tick's `loadCmd` may be in flight) but self-heals within 3s, so it has
  never been visible; at 30s it is a bug. `Init`'s load needs no special case:
  it captures the zero value, which stays current until the first dispatch bumps
  the counter.
- Mutation results (`deletedMsg`, `createdMsg`) bump `stateGen` when applied
  rather than carrying a captured number, so they always win and invalidate
  every load in flight. A captured number is not enough for them: a slow load
  dispatched _after_ a delete began can still finish before the removal does,
  see the worktree, and hold a higher number than the `deletedMsg` that follows
  — which would resurrect the row. `deletedMsg` needs this even though its own
  row drop is local: the load it dispatches is stamped with the number the bump
  just produced, so it is never the load being invalidated.
- The counter orders mutations against loads. It does **not** order two mutations
  against each other, and two deletes can overlap: nothing marks a removal as in
  flight. `confirmThenRemove` clears the armed key on the confirming press and
  returns straight away (`model.go:1154`), and there is no `m.deleting` guard the
  way create has `m.creating` (`create_flow.go:372`) and review has
  `m.reviewLaunching` (`review_flow.go:98`) — so `d` `d` `j` `d` `d` dispatches a
  second `git worktree remove` while the first is still running. Both commands
  captured the same `m.statuses` (`model.go:1153`) and each returns a whole
  row-dropped list, so whichever lands second wins with a list that still holds
  the other's row: delete A, delete B, B's message lands first, then A's message
  puts B back. Two `stateGen` bumps don't help — both messages are mutations, so
  both are current when they apply. As with the create-versus-load race, the 3s
  tick hides this today and 30s makes it a bug.
- Fix it at the payload, not with a third counter: `deletedMsg` carries only the
  identity it removed (`repo`, `name`) instead of a list, and the handler drops
  that row from whatever `m.statuses` holds when the message lands. No
  model-owned memory crosses into the command goroutine — the same ownership rule
  the fast tick follows above — and it is already what `sessionKilledMsg` does
  for `m.sessions` (`model.go:701`–`:708`), so this makes one behavior serve both
  lists. `handleSessionDelete` gets it for free: it shares `confirmThenRemove`
  (`model.go:1190`). The `stateGen` bump still stays, and so does the slow load
  the bullet above adds. The three cover different jobs: identity removal handles
  mutation-versus-mutation, the bump stops a stale git load from re-adding the
  row, and the slow load is what surfaces the repo-wide prune.
- One counter per replaced list, not one shared counter: `stateGen` for
  `m.statuses`, `sessionGen` for `m.sessions`. The two lists have independent
  producers, so a shared counter would let a fast tmux scan's dispatch
  invalidate an in-flight slow git load.
- `sessionGen`'s rule, in full, because it is not symmetric with `stateGen`'s:
  `Update` bumps it whenever it dispatches something that returns a **whole
  session list**, the command captures that number, and the handler drops a
  session list whose number is no longer current. Two producers do that — the
  fast tick's message and `sessionsLoadCmd` — and the slow load is deliberately
  not a third: it discards `RefreshState`'s session half rather than applying it,
  since the fast tick already replaces `m.sessions` every 3s and a second
  producer buys nothing but another ordering case. The fast message is the
  asymmetric part: its pane half applies to whatever `m.statuses` holds
  unconditionally, because that half is a layer and not a replacement (the
  ownership bullet above is what makes it safe), while its session half _is_ a
  wholesale replacement (`model.go:642` is that assignment today) and so is
  gated on the stamp. A fast dispatch therefore bumps `sessionGen` and leaves
  `stateGen` untouched, which is the whole reason the two counters are separate.
- Session mutations bump `sessionGen` on apply instead of carrying a captured
  number, for the same reason `deletedMsg` does: a scan dispatched _after_
  `tmux kill-session` was dispatched can still run its `list-sessions` before the
  kill completes, see the session, and hold a higher number than the
  `sessionKilledMsg` that follows — a dispatch-time-only scheme would re-add the
  session you just killed. Both mutations need the bump: `sessionKilledMsg`
  (`model.go:701`) and `sessionCreatedMsg` (`:693`), whose own `sessionsLoadCmd`
  is dispatched after the bump and so captures the new number. `sessionKilledMsg`
  still removes by identity from whatever `m.sessions` holds (`:703`–`:708`) —
  that is what handles mutation-versus-mutation, the bump is what stops a stale
  scan from re-adding the row, the same division of labor as `deletedMsg` above.
  `Init`'s `sessionsLoadCmd` (`model.go:331`) needs no special case, for the same
  reason `Init`'s load doesn't: it captures the zero value.
- Be honest about what this one is worth: unlike the `m.statuses` races, the
  session race is bounded at 3s either way, because the fast tick still carries
  sessions and the next one clears a resurrected row — exactly today's behavior.
  The counter is here to stop a visible flicker, not a 30s stall. It is still
  worth writing, because without it `m.sessions` is the one wholesale-replaced
  list in the model with no ordering rule at all.
- Consequence to accept, not work around: if the newest load fails it produces a
  `statusMsg`, and the older successful one has already been dropped, so the
  previous list stays on screen until the next tick retries. That is the correct
  outcome — an older snapshot is never the better answer.
- Verify: `go test -run 'Tick|Refresh' ./internal/tui/worktree/`, including a
  case that applies an older slow load after a newer one and asserts the newer
  list survives, one that applies an older slow load after a `deletedMsg`
  and asserts the removed row stays gone, one that applies two `deletedMsg`s for
  different worktrees in either order and asserts both rows are gone, one that
  applies a fast tick's session list stamped before a `sessionKilledMsg` and
  asserts the killed session stays gone while the same message's pane half still
  applies, one asserting `deletedMsg` both drops its row by identity _and_
  dispatches the stamped slow load, and one per repair outcome (success and
  failure) asserting `repairDoneMsg` dispatches the slow load rather than only
  setting the status.

#### Step 7: Debounce the diff, and drop stale diffs

- `diffGen int` on the model. A change of selected worktree bumps it and
  dispatches a 180 ms `tea.Tick` carrying that generation; the handler dispatches
  `selectionChangedCmd` only if the generation still matches — a later change
  invalidates an earlier pending one.
- Detect that change structurally; do not enumerate the keys that cause it.
  Rename today's `Update` body to `update` and let `Update` wrap it: read the
  selected worktree's path before the call, read it again after, dispatch the
  debounce when the two differ. Enumerating is what turns this step into a bug,
  because `j`/`k` are far from the only paths that move the cursor:
  - `h` collapses and then relocates the cursor by identity, landing on the
    parent worktree row or on a repo header (`model.go:856`, `:886`).
  - `l` lands on the first revealed pane row (`:903`) or on the first worktree
    of the just-expanded repo (`:925`) — a different worktree than before.
  - `z` sets the cursor to row 0 when collapsing all (`:939`), and on expanding
    lets `rebuildRows`'s `ClampCursor` slide it onto another row.
  - The filter path (`m.filter.HandleKey` → `rebuildRows` → `ClampCursor`,
    `:796`) moves it on every typed rune and on `esc`, which clears the text and
    rebuilds every row.
  - `handlePaste` does the same for a filter string pasted in one shot
    (`:757`) — a key list would not have caught this one at all.
  - `placeCursorOnActive` moves it from the `sessionsMsg` handler when the
    session list is the second of the two initial loads to arrive.

  Every one of those returns `m, nil` today and is saved only by the 3s tick's
  unconditional recompute, which Step 6 removes — so each would leave the diff up
  to 30s stale. Arrows need no separate handling: `down` and `up` share the
  `j`/`k` switch cases (`:830`, `:838`). The wrapper covers all of them plus
  every key added later, which a list cannot (CLAUDE.md §4: make the mistake
  structurally impossible rather than documented).

- The `diffMsg` handler drops any message whose `path` is not the selected
  worktree's path.
- `applyStatuses` stops dispatching `selectionChangedCmd` unconditionally — the
  wrapper becomes the single dispatcher, so a slow load that also moves the
  cursor cannot produce two diffs for the same path. Because the wrapper fires
  only on a _changed_ selection, the two paths that must refresh an unchanged
  selection (the slow load and `ctrl+r`, both required by ADR-0024 §3) set a
  `forceDiff` flag the wrapper also honors and then clears. PR-title lookups
  ride the same debounce, so fast navigation stops firing a burst of `gh` calls.
- **Wire `ctrl+r` itself**, or the flag has only one producer and the key the
  Scope Boundary and ADR-0024 §3 both promise stays inert. `handleKey` gains a
  `ctrl+r` case that sets `forceDiff` and returns `m, nil` — no command of its
  own, because the wrapper is the single dispatcher and picks the flag up on the
  way out of `update`. That makes it a diff-only refresh of the _current_
  selection: it must **not** dispatch the slow git load, which stays the 30s
  tick's and the mutations' job (Step 6). `handleDiffKey` gets the same case,
  since every key routes there while the diff pane is focused (`model.go:803`)
  and that is where a reader most wants a refresh. The key is free: the only
  control keys bound today are `ctrl+c`, `ctrl+d`, and `ctrl+u` (`model.go:823`,
  `:983`, `:987`, `:1014`, `:1017`), and `ctrl+r` appears nowhere in the repo.
  It also can't leak into an active filter — the shared text input only inserts
  single-rune keys (`components/textinput.go:153`), so it reports the key
  unhandled and nothing types. Add it to `renderHint` and `renderHelpPopup`
  next to `e`, which is the Scope Boundary's "document the refresh key" line;
  Step 9 then carries it out to `cmd/workspace.go` and `docs/spec.md`.
- Verify: `go test -run 'Debounce|Diff' ./internal/tui/worktree/`, with a case
  per cursor-moving path above (`h`, `l`, `z`, filter typing, filter `esc`,
  filter paste) asserting the debounce fires, plus one asserting an unchanged
  selection on the slow tick still refreshes via `forceDiff`, and one per mode
  (list and diff-focused) asserting `ctrl+r` on an unchanged selection sets
  `forceDiff`, produces a debounced diff, and dispatches no slow load.

#### Step 8: Parallel diff execs

- In `collectWorktreeDiff`, run the `--color=always` diff and the `--numstat`
  diff concurrently and join. Both are reads of the same commit range; neither
  mutates. The `merge-base` before them and `untrackedFiles` after stay
  sequential — the first produces the `base` both diffs need.
- **The shared mock is not concurrency-safe, so this step starts there.**
  `MockBaseCommand.ExecCommand` appends to `ExecCommandCalls`
  (`internal/commands/mock.go:287`) and `execCommandResult` does `execCallIdx++`
  on the positional queue (`:304`–`:311`), both unguarded. Two concurrent
  `RunCapture` calls therefore hit two separate problems: a real data race on the
  slice append and the index, which `-race` fails; and a nondeterministic
  answer, because whichever exec reaches the mock first pops the next canned
  result — so the diff can be handed the numstat's stdout. Add a `sync.Mutex` to
  `MockBaseCommand` with exactly **one** locking boundary: `ExecCommand` locks,
  and `execCommandResult` becomes a lock-held helper that never locks itself —
  its doc comment has to say so, because an unexported helper has no other way
  to. `ExecCommand` calls it on the same goroutine (`:288`) and Go's `sync.Mutex`
  is not reentrant, so taking the lock in both would deadlock every mocked
  execution in the repo, not just a concurrent one. The public setters and
  accessors — `SetExecCommandResults`, `SetExecCommandResult`,
  `ResetExecCommand`, `GetLastExecCommandCall`, `GetExecCommandCallCount` — lock
  on their own, since nothing reaches them from inside the locked section. All of
  them, not just `ExecCommand`, or the lock only moves the race.
- **`ExecCommand` holds the lock for bookkeeping only.** Under it: the append to
  `ExecCommandCalls`, and resolving the answer through the helper (which is what
  makes `execCallIdx++` safe). Released before anything else runs, because both
  callbacks `ExecCommand` invokes belong to the caller and may reach back into
  the mock — `ExecCommandFn` is a test's own function, and `cmd.OnStdoutLine` is
  production code (`internal/tooling/task/reviewrun.go:289`) whose own comment
  reasons about what it may assume while inside `ExecCommand`. So the locked
  section snapshots `ExecCommandFn` and, only when it is nil, pops the positional
  entry; the call itself happens after the unlock. Holding the lock across the
  whole body is wrong for a second reason too: it would serialize every mocked
  exec, so the two `RunCapture` calls could never overlap and the concurrent
  cases below would assert nothing about concurrency. Fixing the mock rather
  than working around it in one test file is the point: it is the mock every
  package uses, so any future concurrent exec path gets a safe mock instead of
  rediscovering this (CLAUDE.md §4).
- **Write the mock's concurrency contract into its doc comment; do not privatize
  the exported fields.** The mutex makes `ExecCommand` safe to call from several
  goroutines at once. It does not make every field on the mock safe to touch at
  any moment, and only a stated contract tells the next caller which is which.
  The contract: `ExecCommandFn`, `ExecCommandStdout`/`Stderr`/`Error` and the
  positional queue are **configured before** the concurrent call under test
  starts, and `ExecCommandCalls` is **read after** that call has joined —
  neither during. Both halves already hold everywhere: all 28 `ExecCommandFn`
  assignments in the repo are test setup ahead of the call they script
  (`internal/tooling/task/reviewrun_test.go:61`,
  `internal/tooling/worktree/worktree_test.go:1419`, …), no production code
  assigns it at all, and every `ExecCommandCalls` read is a post-call assertion
  — including `testutil.VerifyNoRealCommands`, which runs at the end of a test
  (`internal/testutil/testutil.go:321`). So the locked read of `ExecCommandFn`
  inside `ExecCommand` never races a write. Privatizing those fields behind
  synchronized accessors would rewrite dozens of test files to buy safety for an
  access pattern nothing performs; the contract is the honest scope, and stating
  it is what makes a future mid-flight assignment reviewable as a defect rather
  than a surprise.
- One comment goes stale with this and is corrected in the same step:
  `newFakeRepoAt` (`internal/tooling/reviewjournal/manager_test.go:55`–`:60`)
  gives two goroutines two separate mocks because "MockBaseCommand records every
  call into a plain slice with no synchronization". The recorder is synchronized
  afterwards, so keep the two independent fixtures — that test wants two views of
  one repo, not one mock — but restate the reason.
- **Test the boundary where the mock lives**, in
  `internal/commands/mock_test.go`: fire N concurrent `ExecCommand` calls at one
  mock three ways — a positional queue, an `ExecCommandFn`, and a call carrying
  `OnStdoutLine` — and assert every call was recorded and each queued answer went
  to exactly one caller. Have the `ExecCommandFn` and the `OnStdoutLine` callback
  each touch an accessor (`GetExecCommandCallCount`), which is what makes this
  case prove the boundary rather than assume it: locking in both methods hangs
  it, and so does locking around the callbacks. The concurrent cases in
  `internal/tooling/task/` below only cover `collectWorktreeDiff`'s two execs.
- **Rewrite `branchdiff_test.go`'s mock setup to be argument-aware.** Its 14
  `SetExecCommandResults` calls are positional, and the assertions index by
  position too (`diffCall := gitBase.ExecCommandCalls[2]`,
  `branchdiff_test.go:125`), so both the answers and the assertions stop meaning
  what they say once the order of calls 3 and 4 is undefined. Move the affected
  cases to `ExecCommandFn`, keying the answer off the args (`merge-base`,
  `--numstat`, `ls-files`, otherwise the diff) — the hook the mock already
  documents as preferred for exactly this reason: positional sequences are
  "coupled to the exact number and order of the commands under test"
  (`mock.go:207`–`:217`). Replace the positional lookups with arg-matched ones;
  `untrackedLookupCall` (`branchdiff_test.go:65`) is already written that way and
  is the pattern to copy.
- Two new cases the sequential tests cannot cover: a concurrent success asserting
  both execs ran and the result is assembled the same way whichever finishes
  first, and a concurrent failure asserting the returned error is deterministic —
  either exec can be the one that fails, and both failing must not depend on
  which goroutine lost the race, so pick a winner (the rendered diff's error) and
  assert it rather than accepting whichever arrives.
- Verify: `go test -race -timeout 60s ./internal/commands/ ./internal/tooling/task/`,
  then `go test ./...` once. `-race` is not optional here — it is the only thing
  that catches the mock race before someone else's concurrent code trips over it.
  The short `-timeout` is what turns a botched locking boundary into a failure
  with a stack trace instead of a ten-minute hang. The full suite is required
  because `internal/commands` sits upstream of most of the tree, which CLAUDE.md §6
  names as a blast radius that overrides the targeted-test default.

#### Step 9: Docs

- ADR-0024 to `docs/decisions/README.md`.
- `e` and the refresh key into `cmd/workspace.go`'s long help and any `dg ws`
  key list in `docs/spec.md`.
- Mark this cycle Done.

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/apps/tmux/ ./internal/apps/registry/ ./internal/tooling/terminal/ \
        ./internal/tui/worktree/ ./internal/tui/components/ ./internal/tui/inventory/ \
        ./internal/tooling/worktree/ ./internal/tooling/task/ ./internal/apps/opencode/ ./cmd/
make lint

# Step 8 touches internal/commands/mock.go, which most of the tree imports:
go test -race -timeout 60s ./internal/commands/ ./internal/tooling/task/
go test ./...
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
6. Create a worktree with `n` → it appears immediately, not after 30s, and does
   not blink back out a moment later when a slow load started before the create
   finally lands.
7. Collapse and expand with `h`/`l`/`z`, and filter with `/` → whenever the
   cursor ends up on a different worktree, the diff pane follows it within a
   beat instead of keeping the previous row's diff.
8. Delete a worktree's directory by hand, then press `r` on its row → the
   "directory was missing; pruned stale entry" error shows _and_ the row leaves
   the list right away, not on the next 30s tick.
9. In a repo holding two worktrees, delete one directory by hand so its row goes
   stale, then `d` `d` the _other_ row → both rows leave the list right away.
   The second one only goes because the removal's repo-wide prune dropped it and
   the slow load re-read git; a purely local row drop would leave it for 30s.
10. `dg wt list` and shell completion still work (they use the recomposed `List()`).

### Regression Check

- `dg ws` with zero worktrees still shows the "press n to create one" guidance.
- `dg ws` outside tmux still reports `not inside tmux` for `enter` on all three
  row kinds.
- Agent-state dots still update within ~3s of an agent changing state — the
  whole point of keeping the fast tick fast.
- `dg wt` create/remove/repair unaffected.

---

## 7. Risks & Trade-offs

| Risk                                                           | Likelihood | Mitigation                                                                                                                                                                                                                                                   |
| -------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Splitting `List()` drops a field on one of the two paths       | Med        | `List()` is recomposed from both halves, so its existing tests cover the composition end-to-end                                                                                                                                                              |
| Viewport offset breaks cursor highlight or tree connectors     | Med        | `isLastChild` scans all rows, not the window; explicit tests at both window edges                                                                                                                                                                            |
| `e` pushes the left pane past the 60% cap on a narrow terminal | Med        | Step 3 clamps **both** toggle targets to `safeMaxLeft()`, not only the wide one; a narrow-resize-then-toggle-both test covers it, since below 59 columns the default 35 is itself over the cap                                                               |
| Debounce swallows a diff if a tick lands mid-debounce          | Low        | `diffGen` is the only gate, and the slow tick's `forceDiff` dispatches a fresh generation of its own                                                                                                                                                         |
| 30s slow tick feels stale for externally-created worktrees     | Accepted   | ADR-0024 trade-off; devgeta's own mutations bypass the timer                                                                                                                                                                                                 |
| Per-pane state clear leaves the window mirror wrong            | Low        | Step 1 recomputes the mirror from the surviving panes of that pane's own `(Session, Window)` instead of unsetting it, and counts a pane whose clear failed as surviving; ADR-0005 rules out leaning on `window_active_clients`                               |
| An older slow load overwrites a newer one or a mutation        | Med        | Step 6's `stateGen`: loads carry their dispatch generation and are dropped when stale, mutation results bump it and always win                                                                                                                               |
| A cursor-moving path is missed and leaves the diff stale       | Med        | Step 7 detects the change in the `Update` wrapper by comparing the selected path, so no key list can fall behind                                                                                                                                             |
| Locking the shared mock disturbs an unrelated test package     | Low        | The mutex is additive and invisible to a sequential caller, but `internal/commands` is upstream of most of the tree, so Step 8 verifies with a full `go test ./...` rather than the targeted list                                                            |
| A second lock inside the locked path deadlocks every test      | Med        | Step 8 fixes one boundary (`ExecCommand` locks; `execCommandResult` is lock-held; callbacks run unlocked) and guards it with concurrent cases in `internal/commands/mock_test.go` under `-timeout 60s`, so a mistake fails fast instead of hanging the suite |

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
