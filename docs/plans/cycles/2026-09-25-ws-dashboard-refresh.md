# Cycle: ws dashboard — cleaner look, saved state, session rows, bug fixes

**Date:** 2026-09-25
**Estimated Duration:** ~2 days
**Status:** Done

Decisions: [ADR-0050](../../decisions/ADR-0050-dashboard-view-state-lives-in-a-tmux-server-option.md) ·
[ADR-0051](../../decisions/ADR-0051-worktree-rows-carry-a-diffstat-from-the-slow-refresh.md) ·
[ADR-0052](../../decisions/ADR-0052-a-repo-header-is-a-label-and-its-sessions-are-rows.md)

---

## 1. Domain Context

`dg ws` is the tmux dashboard that lists repos with their git worktrees, plus
standalone tmux sessions, and shows the selected worktree's diff on the right
([ADR-0003](../../decisions/ADR-0003-sessions-in-workspace-dashboard.md)).

Four problems:

1. **It looks cluttered.** A dim dot on every repo header, "N trees" badges, `└`
   connectors that only appear on the last child, a cramped `●∕` pair, names
   cut off with no `…`, the word "session" on every session row, a sentence of
   text in the right pane, and a 17-entry hint bar that runs off the screen.
2. **It forgets its layout.** Folds and the left-pane width reset every time the
   dashboard opens.
3. **The repo header has an action it shouldn't.** Since
   [ADR-0048](../../decisions/ADR-0048-a-repo-header-reaches-its-sessions-plain-window.md),
   the header becomes selectable when its session has a plain window, and enter
   on it switches there. A repo's windows can live in more than one session,
   and the header can only lead to one of them. The session's name is never
   shown, and there is no way to rename it.
4. **Bugs** found while reading the code (§4, "Bug fixes").

The look was chosen from six mockups: layout **B** (clean tree + per-row change
counts) with the **soft-bar** selection. The mockups are at
https://claude.ai/artifact/VAYvGFQQNM9kXDLbL9zuBX.

---

## 2. Engineer Context

- **Relevant files:**
  - `internal/tui/worktree/model.go` — the model: keys, refresh, rendering (`renderLeft`, `renderRight`, `renderHint`)
  - `internal/tui/worktree/tree.go` — `buildRows`, row kinds, collapse keys, `sameParentRow`
  - `internal/tui/worktree/session_flow.go` — session create/switch/kill, `handleSwitchToRepoSession`
  - `internal/tui/components/` — shared palette, `listnav` (`ClampCursor`, `MoveCursor`), `VisibleWindow`, `TextInput`
  - `internal/tooling/worktree/worktree.go` — `WorktreeStatus`, `StateLayer`, `PlainWindowBySession`, `createWindowWithLayout`
  - `internal/tooling/task/branchdiff.go` — `BranchDiffAt`, `collectWorktreeDiff`, `changedFiles`
  - `internal/apps/tmux/tmux.go` — the tmux wrapper. **It has unrelated uncommitted TPM work on `main`; do this cycle in its own worktree.**
- **Key rules:**
  - [ADR-0024](../../decisions/ADR-0024-the-dashboard-refreshes-fast-and-slow-state-separately.md) — fast (3s, tmux only) versus slow (30s, git) refresh. No git on the fast tick.
  - Every tmux call goes through the tmux wrapper (CLAUDE.md §6).
  - Tests use `testutil.MockApp` and the model's injected `...Fn` seams. Never run real tmux or git.
- **Tests** (targeted, per CLAUDE.md §6):
  ```bash
  # changed packages + their direct in-repo importers (go list .Imports, CLAUDE.md §6)
  go test ./internal/tui/worktree/ ./internal/tui/components/ ./internal/tui/inventory/ \
          ./internal/tooling/worktree/ ./internal/tooling/task/ ./internal/tooling/terminal/ \
          ./internal/apps/tmux/ ./internal/apps/registry/ ./internal/apps/opencode/ ./cmd/
  make lint
  ```
  The root package (`go test .`) imports `internal/tooling/worktree` but only
  tests `configs/` and the hook scripts, which this cycle does not touch, so it
  is left out.

---

## 3. Objective

`dg ws` draws layout B, remembers folds and width for as long as the
tmux server runs, shows each repo's tmux sessions as renameable rows under a
header that is only a label, and the bugs listed below are fixed, each with a
test.

---

## 4. Scope Boundary

### In Scope

**Look (layout B + soft bar)**

- [x] Repo header: bold name only. No status dot and no count badge. A collapsed header shows `▸ name  N`, where N is the number of worktrees hidden under it.
- [x] Worktree row: status glyph, name cut with `…`, and a dim `+A −R` pinned to the right edge ([ADR-0051](../../decisions/ADR-0051-worktree-rows-carry-a-diffstat-from-the-slow-refresh.md)). Nothing is shown when there are no changes. The `∕` glyph and `└` connectors are removed.
- [x] Standalone sessions go under a dim `sessions` header. The per-row "session" label is removed.
- [x] Selected row: a soft background with a yellow `▌` edge, and the glyph keeps its color. This replaces the solid blue block. The armed-delete red stays as it is.
- [x] Hint bar: `↵ open · n new · d delete · / filter · ? help`. Everything else stays reachable from `?`.
- [x] Right pane: blank on header, session and pane rows (no explanatory sentence).
- [x] Default left width goes from 35 to 40.

**Session rows under each repo** ([ADR-0052](../../decisions/ADR-0052-a-repo-header-is-a-label-and-its-sessions-are-rows.md), supersedes ADR-0048)

- [x] A repo header is a label. An expanded header is never a cursor stop. A collapsed header is a stop only so it can be expanded (`l` or enter expands it; neither switches sessions).
- [x] Under each repo, one row per live tmux session holding that repo's worktree windows, listed before the worktree rows: `■ hire2-tien`. There can be more than one. It is drawn like a standalone session row: `■`/`□` for attached/detached. Once any of its **plain-window** panes has reported agent state, it switches to the agent-state glyph aggregated over those panes. The repo's worktree panes are left out of that aggregate because their own rows already show them. It gets pane children under the same rule (2+ stateful plain-window panes, ADR-0008), with key `sess:<name>`.
- [x] Enter on a repo-session row switches to that session's first plain window if it has one, otherwise to that session's first worktree window of this repo. It never switches to the bare session, which would land on the dashboard's own window (ADR-0048, constraint 1).
- [x] `$` renames the selected session (repo or standalone), using tmux's own key for rename-session. The prompt is prefilled with the current name. The name is flattened the way `TmuxSessionName` does. A duplicate is checked against the live session list first, only for a clearer message; tmux itself rejects duplicates (`duplicate session: …`), and that error is what the status line shows if the check is raced. After a rename, both the fold key and the in-memory cursor key move from `sess:<old>` to `sess:<new>`, so the renamed row keeps its fold and the cursor.
- [x] New worktree windows for a repo go into the session that already holds that repo's windows. The derived `TmuxSessionName(repo)` is only a fallback. Without this, a rename makes the next `n` start a second session. The manager has no scan of its own, so a create runs **one `tmux list-panes -a` at create time** to find that session. A create is a mutation, so one tmux call is fine. Do not "optimize" it back into a derived name; the derived name is the bug.

**Saved state** ([ADR-0050](../../decisions/ADR-0050-dashboard-view-state-lives-in-a-tmux-server-option.md))

- [x] Saved: collapsed rows and left width. It lives in the tmux server option `@dg_ws_state` as versioned JSON.
- [x] **The cursor is not saved.** The dashboard keeps opening on the row for the session you are in (`placeCursorOnActive`, fixed by B8). A saved cursor would compete with that at startup and lose "open on where I am", which is the more useful default when `ctrl+t` is pressed from inside a session. Because nothing needs saving at quit, there is no quit-time write and no need to cover the dashboard's eight quit paths.
- [x] Loaded at startup. Written only at the moment a fold changes (`h`, `l`, `z`, a rename moving a fold key), when `e` is pressed, and when a drag ends (`MouseReleaseMsg`), never on each `MouseMotionMsg`. On write, keys for rows that no longer exist are dropped. A failed write (tmux rejects values of about 20 KB and up with `command too long`) is logged at debug level, and the fold or resize still happens. Saving is best-effort and never blocks the UI.
- [x] Width is one saved number. `e` toggles between the default and wide widths, and a drag sets any width. A terminal resize clamps the width without discarding it. The `leftPaneWide` bool goes away.
- [x] New tmux wrapper methods:
  - `GlobalOption(name)` runs `show-options -gqv <name>`. `-q` matters: without it an unset option exits 1 with `invalid option`, which is every first launch after a tmux restart. Empty output means unset, not an error.
  - `SetGlobalOption(name, value)` runs `set-option -g <name> <value>`.
  - `RenameSession(old, new)` runs `rename-session -t <old> <new>`.

**Bug fixes**

- [x] **B1. The cursor follows a row number, not a row.** Every rebuild (3-second tick, filter keystroke, fold, delete) keeps the old index, so rows appearing above the cursor move it onto another row. The diff switches, and enter then acts on the wrong row. Fix: one `rowKey(row)` function (see B5). Record the key before a rebuild, find it again after, and clamp only when that row is gone.
- [x] **B2. Expanding with `z` puts the cursor in another repo.** Same cause as B1, and the same fix covers it.
- [x] **B3. `z` sometimes needs two presses.** `allCollapsed` gets out of sync with the per-repo folds. Fix: remove the flag. `z` collapses every repo if any is expanded, and expands all otherwise.
- [x] **B4. The previous worktree's diff shows under the new row** until the new diff arrives, and the scroll offset carries over. Fix: when `diffPath` is not the selected row's path, draw `loading…`. Reset `diffScroll` on every selection change, not only on j/k.
- [x] **B5. Fold and identity keys use the tmux window name, which is not unique.** For example `taskqueue` + `groups-x` and `taskqueue-groups` + `x` both become `wt-taskqueue-groups-x`: folding one folds both, and `sameParentRow` can jump across repos. Fix: key rows by stable identity. `repo:<folder name>` (today's `Repo` value; identity by path is out of scope), `wt:<path>`, `sess:<name>` and `pane:<paneID>` are produced by one `rowKey`, which the fold map, cursor restore and saved state all use.
- [x] **B6. A diff over 64 KB is cut in the middle of a color code or character.** Fix: cut at the last full line before the limit.
- [x] **B7. The repo header maps to only one session** (the problem the maintainer reported). Resolved by the session rows above.
- [x] **B8. The dashboard opens on the wrong row when a repo's session has another name.** `placeCursorOnActive` (model.go:912) matches worktree rows with `TmuxSessionName(r.status.Repo) == current`, the derived name ADR-0048 already showed is wrong. From inside `hire2-tien`, the cursor lands nowhere. Fix: match the current session against session rows by name, repo-session rows included, which now carry the real name read from the scan.

### Explicitly Out of Scope

- **Window names colliding across repos** (`wt-<repo>-<name>` is ambiguous). Two such worktrees still share pane rows and agent state, and enter can open the wrong window. Fixing it renames windows on existing setups, which needs its own decision and a migration. Open an issue for it.
- **Two repos with the same folder name** (`~/a/api` and `~/b/api`) merge under one header. Same issue as the item above; it needs repo identity by path across the whole worktree tooling.
- Layouts C to F from the mockups, per-viewer themes, and state that survives a reboot (ADR-0050 records why).

**Scope is locked.** Anything else found goes into a follow-up.

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                                   | Description                                                                                                                        |
| ------ | ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| Modify | `internal/apps/tmux/tmux.go` (+ test)                       | `GlobalOption(name)`, `SetGlobalOption(name, value)`, `RenameSession(old, new)`                                                    |
| Modify | `internal/tooling/task/branchdiff.go` (+ test)              | `BranchStatsAt(g, dir)`: counts only, sharing base resolution and `changedFiles` with `collectWorktreeDiff`                        |
| Modify | `internal/tooling/worktree/worktree.go` (+ test)            | Repo-session lookup from the scan. `createWindowWithLayout` places the window in the session that already holds the repo's windows |
| Modify | `internal/tui/worktree/tree.go` (+ test)                    | `rowKey`, `rowRepoSession` kind, session rows under repos, a `sessions` section header, path-based fold keys                       |
| Create | `internal/tui/worktree/viewstate.go` (+ test)               | Encode, decode and prune `@dg_ws_state`                                                                                            |
| Modify | `internal/tui/worktree/model.go` (+ tests)                  | Cursor restore by key, `z`, width, diff staleness and scroll, stats map, new rendering, hint bar, `$`                              |
| Modify | `internal/tui/worktree/session_flow.go` (+ test)            | Enter on a repo-session row, the rename prompt. Remove `handleSwitchToRepoSession`                                                 |
| Modify | `internal/tui/components/styles.go`                         | Soft selection style and the `▌` edge                                                                                              |
| Modify | `docs/spec.md`, `docs/decisions/README.md`, ADR-0048 status | Describe the new behavior, index the ADRs, mark ADR-0048 superseded                                                                |

### Step-by-Step

Every step ends with its targeted tests green and a commit.

1. **Row identity (B5).** Add `rowKey`. Switch the fold map and `sameParentRow` to it. Tests cover the two colliding-window cases from B5.
2. **Cursor restore (B1, B2).** Record the key and restore it in `rebuildRows`. Tests cover: pane rows appearing above the cursor, a new session sorting before it, typing in the filter, and `z` expanding.
3. **`z` without the flag (B3).** Tests cover: every repo collapsed by hand then one `z`, and a new repo appearing while all are collapsed.
4. **Diff staleness and scroll (B4) and the 64 KB cut (B6).**
5. **tmux wrapper methods.** Mocked tests assert the exact argv, including `-q` on the read. A test also checks that empty stdout gives no error and "unset". The mock cannot show what real tmux does without `-q`; the comment on `GlobalOption` records that (verified on tmux 3.7c).
6. **Saved state.** `viewstate.go`, load on start, and write on fold, `e` and drag release. Prune on write, and log-and-continue on a failed write. Width becomes one number and resize clamps it. Tests cover: round trip, an empty, corrupt or old-version value (ignored, falls back to defaults), pruning, a drag writing once on release and never during motion, a failed write not blocking the fold, and resize keeping a dragged width.
7. **Session rows (ADR-0052) and B8.** Header becomes a label, `rowRepoSession`, enter targeting, remove the header switch, `placeCursorOnActive` matching by real session name. Tests cover: a repo with two sessions, a session without a plain window, an expanded header never being a stop, the repo-session glyph aggregating only plain-window panes, and opening from inside a non-derived session name.
8. **Rename (`$`).** Prompt, validation, the rename call, and moving both the fold key and the cursor key to the new name. New-worktree placement reads the session with one `list-panes` at create time. Tests cover: a duplicate name (pre-check message, and tmux's own rejection), a flattened name, the cursor staying on the renamed row, and `n` after a rename landing in the renamed session.
9. **Stats (ADR-0051).** `BranchStatsAt`, with the default branch resolved once per repo per slow load. It runs with bounded parallelism into a path→stats map, and is also filled from each `diffMsg`. Tests check the counts match `BranchDiffAt` on the same fixture.
10. **Look.** Rendering for B and the soft bar, the hint bar, the blank right pane, default width 40. Update the golden and render tests.
11. **Docs.** `docs/spec.md`, and the cycle doc checked off (ADR-0048 was marked superseded when this cycle was approved).

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/tui/worktree/ ./internal/tui/components/ ./internal/tui/inventory/ \
        ./internal/tooling/worktree/ ./internal/tooling/task/ ./internal/tooling/terminal/ \
        ./internal/apps/tmux/ ./internal/apps/registry/ ./internal/apps/opencode/ ./cmd/
make lint
```

That is the full direct-importer set from `go list` (CLAUDE.md §6). No full
suite is needed: the only slow package, the root one, tests `configs/` and the
hooks, and this cycle changes neither.

### Manual

1. Open `dg ws`. It matches mockup B: no header dots or badges, `+A −R` on rows with changes, a `sessions` section, the soft bar, and the five-key hint bar.
2. Collapse two repos, press `e`, drag the divider, quit, and reopen. Folds and width are as you left them, and the cursor is on the row for the session you opened it from.
   2b. Run `tmux kill-server`, start tmux again, and open `dg ws`. It opens with defaults and shows no error (the first-launch `-q` case).
3. Resize the tmux pane narrower and then wider. The width comes back instead of resetting.
4. Leave the cursor on a lower row while an agent starts in a worktree above it. The cursor does not move.
5. In a repo whose windows span two sessions, both session rows show. Enter on each goes to that session and never to the dashboard window.
6. `$` on a repo session: rename it, press `n` in that repo, and the new window appears in the renamed session.
7. Move from a big diff to a small one. `loading…` shows briefly and the scroll starts at the top.

### Regression

- `ctrl+t`, `n`, `N`, `s`, `d`, `D`, `r`, `R`, `/`, `ctrl+r` and pane-row enter all behave as before.
- The narrow-terminal fallback (no right pane) still renders.

---

## 7. Risks & Trade-offs

| Risk                                                                                      | Likelihood     | Mitigation                                                                                                                                 |
| ----------------------------------------------------------------------------------------- | -------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| Stats add git load (3 git calls per tree per 30s, plus 1 per repo for the default branch) | Certain, small | Bounded parallelism, slow tick only, and the selected row reuses its diff. Measured and recorded in ADR-0051                               |
| One more row per repo (its session)                                                       | Certain        | That row is the one that can be renamed and reached. The header row stops being a stop, so the number of cursor stops stays about the same |
| Saved state from an older version                                                         | Low            | Versioned JSON. An unknown or corrupt value is ignored                                                                                     |
| Rename races a create                                                                     | Low            | The create reads the session from the same scan, and a failed switch reports in the status line                                            |

### Trade-offs Made

- **tmux option vs. a state file:** the state lasts as long as the tmux server, not across reboots (ADR-0050).
- **Stats on every row vs. only the selected one:** more git work so the counts are visible while scanning the list (ADR-0051).
- **Session rows vs. a smart header:** one more row, but it scales to any number of sessions and makes the name visible and renameable (ADR-0052).

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope locked?
- [ ] Steps actionable?
- [ ] Verification executable?
- [ ] Risks realistic?

**Reviewer notes:**
