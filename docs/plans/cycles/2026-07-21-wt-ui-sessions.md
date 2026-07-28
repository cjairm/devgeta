# Cycle: `dg ws` — unified workspace dashboard (sessions + worktrees)

**Date:** 2026-07-21
**Estimated Duration:** ~8-10 hours
**Status:** Done

---

## 1. Domain Context

Today a user reaches two tmux surfaces with two keys:

- **`ctrl+t`** (bare, no prefix) → `configs/tmux/tmux.conf:147-148`: a popup running tmux's
  native `choose-tree -Zs` — the built-in session switcher over every session on the tmux
  server, including ones devgeta never created (a manual `notes` session, an ssh session).
- **`ctrl+space u`** (prefix + `u`) → `configs/tmux/tmux.conf:150-154`: opens `devgeta wt ui`
  in a new window — the worktree dashboard, scoped to git worktrees only.

This cycle introduces a new top-level command **`dg ws`** (alias `workspace`) that unifies
both into one dashboard, **deprecates `dg wt ui`**, and retires the `choose-tree` popup so
both worktrees and sessions live behind a single dashboard command (reachable from either
`ctrl+t` or `prefix+u`, which both open `dg ws`). The design is recorded in
[ADR-0003](../../decisions/ADR-0003-sessions-in-workspace-dashboard.md).

Facts that shape the design (confirmed in code):

- tmux sessions **cannot be nested** (server → session → window → pane). "Everything under
  one parent" is a dashboard/view concept, not a tmux structure.
- A worktree is a tmux **window** (`wt-<repo>-<flat-name>`, `worktree.go:828-830`) inside a
  repo-slug **session** (`tmuxSessionName`, `worktree.go:50`). Per-repo-session model is
  unchanged by this cycle.
- Worktrees exist independently of tmux: `WorktreeManager.List()` walks the filesystem
  (`worktree.go:399-457`), so a worktree shows even with no live window
  (`WindowActive == false`, rendered as a hollow dot via `statusdot.go`).

### The model (`dg ws`)

One flat top-level list under the dashboard root. Every top-level row is a **workspace**,
exactly one of two kinds:

- **Repo workspace (worktree-backed):** a repo with worktrees. Expandable → its worktree
  rows. Sourced from `WorktreeManager.List()`. Shows even when its tmux session isn't live.
- **Session workspace (plain):** a standalone tmux session with no worktree. A leaf.
  Sourced from `tmux list-sessions`, excluding any session containing a `wt-` window.

Differentiated inline (one list, not two sections) by two orthogonal signals:

- **Kind** → expandability: repo workspace has a `▼/▶` chevron + an `N trees` badge; a
  plain session is a leaf labeled `session`.
- **Activity** → the existing status dot (`●` attached/live, `○` not), on both kinds.

```
▼ api          2 trees
  └ ● feat-login
  └ ○ fix-auth
▶ pillar       1 tree
● notes        session
○ scratch      session
```

Related: [ADR-0001](../../decisions/ADR-0001-in-tui-overlays-over-external-pickers.md),
prior cycles `2026-06-07-worktree-v2-tui-dashboard.md`, `2026-07-15-wt-ui-create-flow.md`,
`2026-07-17-wt-ui-repo-scan-and-layouts.md`.

---

## 2. Engineer Context

**Relevant files and their purposes:**

- `internal/apps/tmux/tmux.go` — tmux wrapper. Has `CreateSession`, `KillSession`,
  `SwitchToSession`, `HasSession`, `WindowSession`, `CurrentSession`. **No** "list all
  sessions" helper — the one new primitive this cycle adds.
- `internal/tooling/worktree/worktree.go` — `WorktreeManager`; `List()` builds
  `[]WorktreeStatus` (399-457); holds a `Tmux` ref.
- `internal/tui/worktree/` — the Bubble Tea dashboard (package `tuiworktree`):
  `tree.go` (row model), `model.go` (key dispatch, attach, two-press delete, render),
  `create_flow.go` (the `n`/`N` flow + floating name prompt — the pattern to reuse for
  "new session"), `run.go` (`Run()` entry point).
- `internal/tui/components/statusdot.go` — `SessionState` + glyphs.
- `cmd/worktree.go` — `dg wt` commands; TUI entry is `worktreeUICmd` (204-212), wired in
  `init()` (307-314). Top-level commands attach via `rootCmd.AddCommand` in each file's
  `init()`.
- `configs/tmux/tmux.conf` — the two keybindings (embedded config).

**Naming / package decision to make early:** the TUI package is `internal/tui/worktree`
(import alias `tuiworktree`). Since the dashboard is no longer worktree-only, either (a)
keep the package as-is and just add a new `cmd/workspace.go` that calls `tuiworktree.Run()`,
or (b) rename the package to `internal/tui/workspace`. Recommendation: **(a) keep the
package name this cycle** (a rename is churn with no user-visible benefit) and revisit in a
later cleanup cycle. Do not block on it.

**Key functions/types (new unless noted):**

- `Tmux.ListSessions()` — `tmux list-sessions -F "#{session_name}\t#{session_attached}"`.
- `WorktreeManager.ListSessions()` — standalone sessions (no `wt-` window).
- `SessionStatus{ Name string; Attached bool }`.
- `rowKind` gains `rowSession`; `row` gains a session field and an `N trees` count for repo
  headers.
- `Tmux.CreateSession` / `SwitchToSession` / `KillSession` — existing, reused.
- `Model.confirmThenRemove` (model.go:812-847) — reused for kill-session.

**Testing patterns:** [docs/guides/testing-patterns.md](../../guides/testing-patterns.md) —
`testutil.MockApp`; `testutil.VerifyNoRealCommands(t, mockApp.Base)` against the **same**
base; `func init() { testutil.InitLogger() }`; set `t.Setenv("TMUX", ...)` explicitly on
attach/switch paths; TUI tests drive `Model.Update` with `tea.KeyPressMsg`.

**Commands to run tests:**

```bash
go test ./internal/apps/tmux/
go test ./internal/tooling/worktree/
go test ./internal/tui/worktree/
go test ./cmd/
go test ./...
make lint
```

---

## 3. Objective

`dg ws` (alias `workspace`) opens one dashboard listing repo workspaces (expandable to
worktrees) and plain sessions under a single list, differentiated by chevron/badge and the
activity dot; the user can create a session (`s`), switch to a session (`enter`), and kill a
session (`d`, two-press) alongside the existing worktree actions. `dg wt ui` still works but
prints a deprecation notice pointing to `dg ws`, and the bare `ctrl+t` `choose-tree` popup
is replaced by opening `dg ws`.

---

## 4. Scope Boundary

### In Scope

- [x] `docs/decisions/ADR-0003` — the design record this cycle implements.
      **Gate: ADR-0003 must be `ACCEPTED` before Step 1 begins.** It stays `PROPOSED`
      while this cycle is Draft; on cycle approval, flip it to `ACCEPTED`, then start coding.
- [x] `Tmux.ListSessions()` + test.
- [x] `WorktreeManager.ListSessions()` returning standalone sessions (no `wt-` window) + test.
- [x] Row model: `rowSession` kind; repo-header rows carry a worktree count; `buildRows`
      emits one flat top-level list (repo workspaces + plain sessions) with the chevron +
      `N trees` badge / `session` leaf differentiation.
- [x] Session actions in the TUI: `s` (new session → name prompt → create in `$HOME`, then
      switch inside tmux / report detached outside tmux), `enter` (switch, `$TMUX`-guarded),
      `d` (kill session, two-press via `confirmThenRemove`). `D`/`r` are worktree-only no-ops
      on a session row.
- [x] Session status dot: attached vs detached.
- [x] New `cmd/workspace.go`: `dg ws` (alias `workspace`) calling `tuiworktree.Run()`,
      registered in `init()`.
- [x] Deprecate `dg wt ui`: cobra `Deprecated` notice pointing to `dg ws`; still runs.
- [x] `configs/tmux/tmux.conf`: retire the `choose-tree` popup; bind `ctrl+t` to open
      `dg ws`; keep `prefix+u` as a secondary alias (also pointing to `dg ws`).
- [ ] Rebuild + `dg configure tmux --force` + verify end-to-end (embedded-config change).
      **Deliberately left undone by the implementing agent** — running this against the real
      machine hosting the session would overwrite this machine's live tmux config and could
      disrupt an in-use tmux server. The human running this session must do this step manually.
- [x] Docs (all required): `docs/spec.md` (workspace model + `dg ws` in the features/commands
      section), `README.md` (command list — `dg ws` is a new top-level user-facing command),
      the TUI help overlay, and the `dg wt` long help (mention the `dg ws` rename). No file
      under `docs/apps/` — that directory is for app-installer docs, and `dg ws` is a command,
      not an installed app.

### Explicitly Out of Scope

- Session↔worktree **reconciliation** beyond the one-line "session contains a `wt-` window"
  exclusion filter (per ADR-0003).
- Renaming the `internal/tui/worktree` package (keep it; revisit later).
- A `dg ws` sub-verb family (create/list/rm as plain CLI) — `dg ws` opens the TUI only; the
  CLI verbs stay under `dg wt`.
- A diff/preview pane for session rows (right pane stays worktree-only; session row shows
  guidance there).
- tmux restructuring (the per-repo-session model is unchanged).
- Session persistence/restore (already handled by tmux-resurrect/continuum).

**Scope is locked.** Anything discovered out of scope gets documented here for a later cycle.

---

## 5. Implementation Plan

### File Changes

| Action        | File Path                                    | Description                                                                                                                    |
| ------------- | -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ |
| Modify        | `internal/apps/tmux/tmux.go`                 | Add `ListSessions()` (name + attached flag); optionally a scan helper returning `[]{session,window}` for classification        |
| Create/Modify | `internal/apps/tmux/tmux_test.go`            | Test `ListSessions` parsing (mocked)                                                                                           |
| Modify        | `internal/tooling/worktree/worktree.go`      | Add `SessionStatus`; `ListSessions()` excluding sessions with a `wt-` window                                                   |
| Modify        | `internal/tooling/worktree/worktree_test.go` | Test standalone-session filtering                                                                                              |
| Modify        | `internal/tui/worktree/tree.go`              | `rowSession` kind; worktree count on repo rows; `buildRows` = one flat list of repo workspaces + plain sessions                |
| Modify        | `internal/tui/worktree/model.go`             | Load sessions; row-kind-aware `enter`/`d`; add `s`; session dot; right-pane guidance for session rows; hint bar + help         |
| Create        | `internal/tui/worktree/session_flow.go`      | The `s` → name prompt → create-session flow (mirrors `create_flow.go`'s name-input)                                            |
| Modify        | `internal/tui/worktree/*_test.go`            | Model tests: session rows render/expand, `s` creates, `enter` switches, `d` kills (two-press)                                  |
| Modify        | `internal/tui/components/statusdot.go`       | Attached/detached rendering for sessions                                                                                       |
| Create        | `cmd/workspace.go`                           | `dg ws` (alias `workspace`) → `tuiworktree.Run()`; registered in `init()`                                                      |
| Create        | `cmd/workspace_test.go`                      | Command parses/aliases; TUI entry mocked                                                                                       |
| Modify        | `cmd/worktree.go`                            | `worktreeUICmd.Deprecated = "use \`dg ws\` instead"`; note rename in long help                                                 |
| Modify        | `configs/tmux/tmux.conf:147-154`             | `ctrl+t` → `dg ws`; keep `prefix+u` → `dg ws`; remove `choose-tree`                                                            |
| Modify        | `docs/spec.md`, `README.md`                  | Document `dg ws`, the workspace model, new keys, `dg wt ui` deprecation (no `docs/apps/` file — `ws` is a command, not an app) |

### Step-by-Step

#### Step 1: `Tmux.ListSessions()`

- Add to `tmux.go`, modeled on `WindowSession`'s scanner: run
  `tmux list-sessions -F "#{session_name}\t#{session_attached}"`, parse into
  `[]SessionInfo{ Name string; Attached bool }`. Signature is `([]SessionInfo, error)` —
  **not** the swallow-to-nil form `WindowSession` uses, because this populates a UI region
  and the model must distinguish "genuinely no sessions" from "the query failed" (see
  Concern-1 fix). Return `(nil, nil)` **only** for tmux's "no server running" case (no
  server means no sessions exist — legitimately empty); return the wrapped error for any
  other non-zero exit so the model can surface it. Detect "no server" by matching that
  substring in tmux's stderr.
- Verify: `go build ./internal/apps/tmux/`.

#### Step 2: Test `ListSessions`

- Mocked stdout (`main\t1\nnotes\t0`) → assert parsed slice. `VerifyNoRealCommands`.
- Verify: `go test ./internal/apps/tmux/`.

#### Step 3: `WorktreeManager.ListSessions()` + `SessionStatus`

- Add `SessionStatus{ Name string; Attached bool }` to `worktree.go`.
- `ListSessions() ([]SessionStatus, error)`: call `Tmux.ListSessions()`, then exclude any
  session containing a window whose name starts with `windowPrefix` (`"wt-"`), using a single
  `list-windows -a` scan (add a small `Tmux` helper returning `[]{session,window}` or reuse
  the existing scan shape). Excluding worktree-backed sessions prevents a repo session from
  appearing twice. Propagate the error from `Tmux.ListSessions()` unchanged (the `(nil, nil)`
  no-server case flows through as an empty list, not an error).
- Verify: `go test ./internal/tooling/worktree/`.

#### Step 4: Row model — one flat list

- `tree.go`: add `rowSession`; give the repo-header row a worktree count (for the `N trees`
  badge). `buildRows` emits, under one root: each repo workspace (header + children when
  expanded) then each plain session as a leaf `rowSession`. `leafIndices` (renamed from
  `worktreeIndices` during implementation, since it now covers both leaf kinds) /
  `navigableIndices` treat session rows as navigable but **not** worktree rows (so
  `selectedStatus`/diff/attach stay worktree-only).
- Verify: `go test ./internal/tui/worktree/`.
- Done: implemented in commits `603b548`, `c0f3dac`.

#### Step 5: Load sessions into the model

- `model.go`: add `sessions []worktree.SessionStatus`; fold into the existing load so it
  stays in sync with the 3s refresh (extend `loadCmd` to fetch both and emit a combined
  message, or batch a `sessionsMsg`). `rebuildRows` passes both to `buildRows`.
- **Session-query failure UX (Concern-1 fix):** on a `ListSessions()` error, surface it on
  the status line as `"failed to list sessions: " + err` — exactly how `loadCmd` already
  handles a `List()` error (`model.go:240-243`) — and keep the last-good session rows rather
  than blanking them. This distinguishes "query failed" (status-line warning) from "no
  sessions" (empty area, no warning). Worktrees are unaffected: they load independently from
  the filesystem, so a tmux failure never hides them.
- Verify: manual run shows the flat list; row tests; a test with a session-load error asserts
  the status line shows the warning and worktree rows still render.
- Done: implemented in commit `a7e7b4d`.

#### Step 6: Session actions — `enter`, `d`, `s`

- Add `selectedSession() (SessionStatus, bool)` mirroring `selectedStatus`.
- `handleKey` branches on the cursor row kind:
  - `enter` on a session → `$TMUX`-guarded `SwitchToSession` (same guard message as
    `handleAttach`).
  - `d` on a session → kill via a session variant of `confirmThenRemove` (two-press).
    `D`/`r` on a session row are no-ops.
  - `s` (any row) → floating name prompt (reuse `createNameInput` UI shape); on enter,
    `CreateSession(name, workdir)`. **Workdir is the user's home (`paths.Paths.Home.Root`)**:
    a plain session is deliberately not tied to a repo, so a selected-row-derived path would
    contradict the concept and the TUI's own cwd is just wherever `dg ws` launched. Then, if
    inside tmux, `SwitchToSession`; if outside tmux, create detached and report
    (`"session created: <name>"`) without switching — `tmux new-session -d` works without a
    client, so `s` is **not** blocked outside tmux; only the switch is skipped. This mirrors
    the worktree-create-outside-tmux path (`model.go:407-412`).

    > **Superseded (2026-07-22):** `s` now opens a folder-pick step first (`root` = home pinned,
    > plus the repo candidates and a free-typed path, validated as a directory via
    > `ValidateDirPath`), so the workdir is no longer always home. The name prompt now also
    > auto-generates a `devgeta-<character>` name on a blank Enter, collision-checked against the
    > live tmux sessions. See `session_flow.go` / `session_name.go` and `docs/spec.md`.
- **Duplicate session names (Q2):** rely on tmux's own error — `tmux new-session -s <name>`
  fails when the name exists — surfaced via the status line, the same way worktree
  create-failures surface (`createFailedMsg`/`statusMsg`). No prompt-layer `HasSession`
  pre-check: it duplicates the enforcement tmux already does and adds a TOCTOU gap (the name
  could be taken between check and create). `HasSession` exists (`tmux.go:250`) if faster
  inline feedback is ever wanted, but that's out of scope here.
- Verify (happy **and** failure paths, all mocked, `VerifyNoRealCommands`), tested inside and
  outside tmux via `t.Setenv("TMUX", ...)`:
  - `s`: success (create + switch inside tmux; create detached + report outside tmux) **and**
    `CreateSession` returning a duplicate-name error → status line shows the failure, prompt
    state cleared, no switch.
  - `enter`: success (switch + quit) **and** `SwitchToSession` error → status line shows it,
    TUI does not quit.
  - `d`: success (kill + row drops) **and** `KillSession` error → status line shows it, row
    stays.
- Done: implemented in commit `69963df`.

#### Step 7: Status dot, chevron/badge, hint bar, help

- `statusdot.go`: attached session → running glyph; detached → hollow/dim (consistent with
  worktree dots — CLAUDE.md §7).
- `renderLeft`: chevron + `N trees` badge on repo headers; `session` label on session rows.
- `renderHint`: add `s: new session`; note `d` kills a session on a session row.
- `renderHelpPopup`: add the session keys.
- Verify: manual run; row tests.
- Done: implemented in commit `e4955bf`.

#### Step 8: `dg ws` command + deprecate `dg wt ui`

- Create `cmd/workspace.go`: `workspaceCmd` (`Use: "ws"`, `Aliases: ["workspace"]`) whose
  `RunE` calls `tuiworktree.Run()`; register in `init()` via `rootCmd.AddCommand`.
- `cmd/worktree.go`: set ``worktreeUICmd.Deprecated = "use `dg ws` instead"`` (cobra prints
  the notice and still runs). Update `dg wt` long help.
- Verify: `go build ./cmd/`; `./devgeta ws --help`; `./devgeta wt ui` prints the notice.
- Done: implemented in commit `da14ff0`.

#### Step 9: Keybinding migration (embedded config)

- `configs/tmux/tmux.conf`: remove `choose-tree` (147-148); bind `ctrl+t` to open `dg ws`
  (mirror the existing `new-window ... ~/.local/bin/devgeta ...` form, now `ws`); repoint
  `prefix+u` to `dg ws` too.
- **Rebuild + redeploy**: `make build` → install → `dg configure tmux --force` → reload
  tmux. An old binary ships the old config (CLAUDE.md §"Changing an embedded config").
- **Rollback**: `git revert` the `tmux.conf` change, rebuild, `dg configure tmux --force`,
  reload. For an instant local revert without a rebuild, re-add the old
  `bind -n C-t display-popup ... choose-tree -Zs` line to `~/.tmux.conf` and
  `tmux source-file ~/.tmux.conf` — the embedded config is only re-applied on the next
  `dg configure`, so a hand edit holds until then.
- Verify: manual — see below.
- Done: `configs/tmux/tmux.conf` source change implemented in commits `4af9586`,
  `e32ca46`. **Rebuild + redeploy is NOT done** — deliberately left for the human running
  this session to do (a `dg configure tmux --force` here would overwrite this machine's
  real tmux config and could disrupt a live tmux server hosting this very session). Run
  the rebuild/redeploy/reload sequence above yourself when ready.

#### Step 10: Docs

- `docs/spec.md` (workspace model + `dg ws` in the features/commands section) and
  `README.md` (add `dg ws` to the command list, note `dg wt ui` deprecation): document the
  workspace model, the new keys, and the deprecation. No `docs/apps/` file — that directory
  is app-installer docs, and `dg ws` is a command, not an installed app.
- Verify: read for clarity.
- Done: implemented in commits `f7b7242`, `c585cd5`.

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/apps/tmux/ ./internal/tooling/worktree/ ./internal/tui/worktree/ ./cmd/
go test ./... -cover
make lint
```

### Manual

1. In tmux with a couple of manual sessions (`tmux new -d -s notes`) and a worktree window:
   `dg ws` → one flat list: repo workspaces with `▼ repo  N trees` + worktree children, and
   plain sessions (`notes`, …) as leaves; repo-slug sessions do **not** appear as separate
   session rows.
2. Chevron expands/collapses a repo workspace; the activity dot (`●`/`○`) is correct on both
   kinds.
3. `enter` on a session → client switches, TUI quits.
4. `s` → name prompt → `scratch` → new session created in `$HOME` + switched to (inside
   tmux); run `dg ws` from a plain shell → `s` creates it detached and reports, no switch.
5. `d` `d` on a session row → killed; single `d` arms (any other key cancels).
6. Worktree row `enter`/`d`/`D`/`r` behave exactly as before.
7. `dg wt ui` still opens the dashboard but prints the deprecation notice.
8. Bare `ctrl+t` opens `dg ws` (not `choose-tree`); `prefix+u` also opens `dg ws`.
9. Session-query failure: with no tmux server, `dg ws` shows worktrees and an empty session
   area with **no** warning (no server = no sessions). Simulate a real query failure in a
   test → status line shows "failed to list sessions: …" while worktree rows still render.

### Regression Check

- Worktree create/attach/delete/repair unchanged (`go test ./internal/tui/worktree/`).
- `dg wt --help`, `dg wt list` still work (`make build && ./devgeta wt --help`).
- Not inside tmux: `dg ws` lists; `enter` (switch) shows the "not inside tmux" guard; `s`
  still creates a detached session and reports it (not blocked, not an error).

---

## 7. Risks & Trade-offs

| Risk                                                                       | Likelihood | Mitigation                                                                                                              |
| -------------------------------------------------------------------------- | ---------- | ----------------------------------------------------------------------------------------------------------------------- |
| Repo-slug sessions double-listed (repo workspace + session row)            | Med        | Exclude sessions containing a `wt-` window in `ListSessions()`; test it                                                 |
| Losing `choose-tree`'s pane-level navigation for plain sessions            | Med        | Accepted (ADR-0003); can rebind `choose-tree` to a spare chord later                                                    |
| Muscle memory: `ctrl+t` now opens a full TUI, not the instant native popup | Low        | `dg ws` opens fast in a window; `prefix+u` still works; notice on `dg wt ui`                                            |
| Mocks hide real tmux calls                                                 | Low        | `VerifyNoRealCommands` on every new test; no real `list-sessions`/`kill-session`/`new-session`/`switch-client` in tests |
| Old binary ships old tmux.conf after config edit                           | Med        | Step 9 rebuild + `dg configure tmux --force` is mandatory                                                               |
| Package name `tui/worktree` now understates its scope                      | Low        | Keep this cycle; note a rename for a later cleanup                                                                      |

### Trade-offs Made

- **One flat list vs two sections:** one list with inline chevron/badge differentiation
  (user's call) — reads as a single parent, not two buckets.
- **`dg ws` (workspace) vs `dg session`:** "workspace" covers both a plain session and a
  repo-with-worktrees, and repos without a live session still appear.
- **Deprecate, don't delete, `dg wt ui`:** soft cobra `Deprecated` notice; still runs, so no
  breakage for existing muscle memory / scripts.

---

## 8. Cross-Model Review Notes

- [x] "Standalone session" defined unambiguously? (a session with **no** `wt-` window)
- [x] Does the row-kind split keep `selectedStatus`/diff/attach worktree-only?
- [x] All new tmux calls mocked and `VerifyNoRealCommands`-checked?
- [x] Embedded-config rebuild + reconfigure step explicit? (explicit in Step 9, but
      deliberately not executed — see that step's Done note)
- [x] `dg wt ui` deprecation prints and still runs?

**Reviewer notes:**

Implemented via subagent-driven development: one implementer + spec-compliance review +
code-quality review per step (Steps 5-10; Steps 1-4 were completed in a prior session), plus
a final whole-branch review after all steps landed. The final review caught one real
integration gap — the diff pane kept showing a stale worktree diff when the cursor moved onto
a session row, instead of the guidance the plan's Out-of-Scope section promised — and two
stale `dg wt ui` references left over from the Step 8 rename; both fixed
(`daa1b04`). ADR-0003's `Status` field was `APPROVED`, not a value the ADR template
permits (`PROPOSED | ACCEPTED | SUPERSEDED | DEPRECATED`) — corrected to `ACCEPTED`.
No other issues found. `go build ./...`, `go test ./...`, and `gofmt -l` are clean
end-to-end. Live deployment (`make build` → install → `dg configure tmux --force` → reload)
is intentionally left for a human to run — the agent implementing this never had authorization
to modify or reload this machine's real tmux config.

---

## Notes for Implementers

- **Commit after each step** with `/smart-commit` once its verify check passes.
- **Never execute real tmux in tests.** Mock every `list-sessions`/`kill-session`/
  `new-session`/`switch-client`.
- **Embedded-config change requires rebuild + `dg configure tmux --force`.**
- **This cycle implements [ADR-0003](../../decisions/ADR-0003-sessions-in-workspace-dashboard.md).**
