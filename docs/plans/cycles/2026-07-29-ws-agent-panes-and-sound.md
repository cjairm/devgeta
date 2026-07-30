# Cycle: Agent state on every row, and a sound when one wants you

**Date:** 2026-07-29
**Estimated Duration:** ~7 hours (Part A ~5, Part B ~2)
**Status:** Draft — awaiting approval

---

## 1. Domain Context

[2026-07-28-agent-activity-notifications.md](2026-07-28-agent-activity-notifications.md)
shipped agent activity reporting: both AI coders write `@dg_agent_state` on their tmux pane
(`busy`/`idle`/`blocked`/`error`), `dg ws` reads it on a 3-second refresh, and a worktree row
shows `●`/`◆`/`!`/`✕`. [ADR-0005](../../decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md)
governs the storage.

It works — and it is invisible for the way the tool is actually used. A live scan taken while
two Claude Code sessions were running:

```
devgeta-goku           | 2.1.220                        | %15  | @dg_agent_state=blocked
devgeta-goku           | 2.1.220                        | %126 | @dg_agent_state=blocked
flux-goku              | 2.1.220                        | %1   | @dg_agent_state=idle
flux-goku              | 2.1.220                        | %2   | (empty — a zsh pane)
misc                   | zsh                            | %5   | (empty)
pillar-cloud-functions | wt-pillar-cloud-functions-fix… | %131 | (empty — opencode, no turn yet)
```

One agent blocked on a permission prompt, another finished and waiting. `dg ws` reported
neither, and nothing made a sound. This cycle fixes both halves:

- **Part A** — the dashboard drops every pane state that is not in a `wt-…` window, session
  rows cannot carry state at all, and a collapsed repo header hides its children's.
  Governed by [ADR-0008](../../decisions/ADR-0008-agent-state-on-every-pane-row.md).
- **Part B** — nothing in the repo makes a sound at any layer, so the signal only works if
  you are looking at it. Governed by
  [ADR-0009](../../decisions/ADR-0009-audible-agent-notifications.md).

Read both ADRs before starting. They carry the evidence, the rejected alternatives, and the
reason process-name detection is not an option.

---

## 2. Engineer Context

### Part A — the read and render path

| File                                        | Role                                                                                            |
| ------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `internal/apps/tmux/tmux.go:293`            | `PaneStates()` — the one `list-panes -a` scan; returns `(Session, Window, PaneID, State)`       |
| `internal/apps/tmux/tmux.go:267`            | `SessionWindows()` — `(Session, Window)`; a strict subset of the above, dropped from one caller |
| `internal/tooling/worktree/worktree.go:445` | `aggregateAgentState` — ADR-0005's precedence, currently unexported and single-caller           |
| `internal/tooling/worktree/worktree.go:477` | `List()` indexes the scan by window name…                                                       |
| `internal/tooling/worktree/worktree.go:519` | …and only ever looks up `wt-<repo>-<flat>`. **This is where the state is lost.**                |
| `internal/tooling/worktree/worktree.go:88`  | `SessionStatus{Name, Attached}` — no state field to fill                                        |
| `internal/tooling/worktree/worktree.go:557` | `ListSessions()` — runs `SessionWindows()` for the `wt-` exclusion filter                       |
| `internal/tui/components/statusdot.go:20`   | `SessionStateFromWorktree` — the worktree-shaped mapper                                         |
| `internal/tui/components/statusdot.go:95`   | `SessionGlyph`/`SessionDot` — ■/□, attached-only                                                |
| `internal/tui/worktree/tree.go:13`          | `rowKind` (`rowRepo`/`rowWorktree`/`rowSession`), `row`, `buildRows`, `leafIndices`             |
| `internal/tui/worktree/model.go:1273`       | `rowRepo` render — chevron + name + badge, **no dot**                                           |
| `internal/tui/worktree/model.go:1300`       | `rowSession` render — `SessionDot(attached)`                                                    |
| `internal/tui/worktree/model.go:1328`       | `rowWorktree` render — the one path that works today                                            |
| `internal/tui/worktree/model.go:817,839`    | `h`/`l` collapse/expand, keyed by `m.collapsed[repo]`                                           |

Layout note: every render branch computes explicit display widths (`prefix = connector(2) +
dot(1) + branchChar(1) + space(1) = 5`). Adding a glyph to a row means updating that arithmetic
in the same branch, or columns stop aligning.

### Part B — the two hooks

| File                                | Role                                                                         |
| ----------------------------------- | ---------------------------------------------------------------------------- |
| `configs/claude/agent-state.sh`     | Claude side; `$1` is the value; already `                                    |     | true`-tolerant on its tmux call |
| `configs/opencode/plugin/notify.js` | OpenCode side; one injected `execFn(args)` seam for **all** tmux calls       |
| `configs/tmux/tmux.conf:126`        | `window-status-format`, source of the `window_active_clients == 0` predicate |
| `internal/config/fromFile.go:89`    | `WorktreeConfig` — where `notify_sound` goes                                 |
| `agent_state_test.go` (repo root)   | Go test driving the real script with a **fake `tmux` on PATH**               |
| `notify.test.mjs` (repo root)       | Node test driving `Notify()` with a stubbed `execFn`                         |

CLAUDE.md's two-agent sync rule applies to every behaviour in Part B: gate, switch, state set,
and player must match in both files.

### Commands

```bash
go test ./internal/tooling/worktree/ ./internal/tui/... ./internal/apps/tmux/
go test .                 # root package: agent_state_test.go
node --test notify.test.mjs
go test ./... && make lint
```

`node --test` is not wired into `make` or CI — run it by hand.

---

## 3. Objective

An agent that wants your attention is impossible to miss: it shows on whichever `dg ws` row
owns it — worktree, plain session, or collapsed repo header — it can be drilled to the exact
pane, and it makes a distinct sound when you are not already looking at its window.

---

## 4. Scope Boundary

### In Scope — Part A (ADR-0008)

- [ ] Export `AggregateAgentState`; one precedence rule for all row kinds
- [ ] `SessionStatus.AgentState`, aggregated over that session's panes
- [ ] `ListSessions` derives its `wt-` exclusion from `PaneStates()`; drops `SessionWindows()`
- [ ] `rowSession` renders the shared state vocabulary; keeps ■/□ when no agent reported
- [ ] `rowRepo` renders an aggregated dot so collapsing hides nothing
- [ ] `rowPane` child rows, revealed by expanding a parent with 2+ stateful panes
- [ ] `m.collapsed` generalized from repo-keyed to parent-keyed
- [ ] Tests for each; `docs/spec.md` updated

### In Scope — Part B (ADR-0009)

- [ ] Sound on `idle`/`blocked`/`error`, never `busy`, in **both** hooks
- [ ] Gated on `window_active_clients == 0`
- [ ] Opt-in via `@dg_notify_sound`, **off by default**
- [ ] Per-state sounds; `afplay` → `paplay` → `printf '\a'` probe; backgrounded; never fails
- [ ] `worktree.notify_sound` in `WorktreeConfig`, rendered into `tmux.conf`
- [ ] Tests: fake `tmux` + fake player on PATH (Go), stubbed seam (Node)

### Explicitly Out of Scope

- **A cross-session `status-right` aggregate.** ADR-0008 drops it: Part B covers the case
  better. Do not add it back as a "small extra."
- **Detecting agents by process name.** ADR-0008 rejects it with evidence (`2.1.220`).
- **Writing state at agent launch.** A pane whose agent has not taken a turn stays stateless;
  ADR-0008 accepts this. Fixing it belongs to whatever launches the agent.
- **Desktop notifications.** ADR-0009 rejects them; both agents currently deny `osascript`.
- **Debounce/coalescing of sounds.** ADR-0009 accepts the annoyance until it is proven real.
- **Configurable sound files or a custom player command.** `@dg_notify_sound` is `on`/`off`;
  widening it later is additive.
- **Toast rendering.** `notification.go`'s `ToastNeedsReview` stays unused, as before.
- **`dirtyCount`.** Still hardcoded `0` at every call site. Its own cycle.
- **`dg config` plumbing for the new setting** beyond one registry entry — that cycle
  ([2026-07-29-dg-config-command.md](2026-07-29-dg-config-command.md)) owns the surface. If it
  has not landed, ship the YAML field and the tmux option and stop there.

**Scope is locked.**

---

## 5. Implementation Plan

### File Changes

| Action | File                                     | Description                                                                                 |
| ------ | ---------------------------------------- | ------------------------------------------------------------------------------------------- |
| Modify | `internal/tooling/worktree/worktree.go`  | Export aggregator; `SessionStatus.AgentState` + `Panes`; `ListSessions` uses `PaneStates()` |
| Modify | `internal/tui/components/statusdot.go`   | `SessionStateFromAgent`; session/pane dot behaviour                                         |
| Modify | `internal/tui/worktree/tree.go`          | `rowPane`; parent-keyed collapse; `buildRows`/`leafIndices`                                 |
| Modify | `internal/tui/worktree/model.go`         | Render branches for repo dot, session state, pane rows; expand/collapse                     |
| Modify | `configs/claude/agent-state.sh`          | Gate + switch + player                                                                      |
| Modify | `configs/opencode/plugin/notify.js`      | Same, through a generalized single seam                                                     |
| Modify | `configs/tmux/tmux.conf`                 | `set -g @dg_notify_sound "{{ ... }}"`                                                       |
| Modify | `internal/config/fromFile.go`            | `WorktreeConfig.NotifySound`                                                                |
| Modify | `agent_state_test.go`, `notify.test.mjs` | Sound coverage                                                                              |
| Modify | `docs/spec.md`                           | Both features                                                                               |

### Part A

#### Step 1: One aggregator, exported

Rename `aggregateAgentState` → `AggregateAgentState` (worktree.go:445) and keep `agentStateRank`
unexported. No behaviour change; it stops being single-caller.

- Verify: `go test ./internal/tooling/worktree/`

#### Step 2: Sessions carry state

Add to `SessionStatus`: `AgentState string` and `Panes []tmux.PaneState`. In `ListSessions`,
call `PaneStates()` once, build `panesBySession`, derive the `wt-` exclusion set from the same
slice (`isWorktreeWindow(ps.Window)`), drop the `SessionWindows()` call, and fill both new
fields with `AggregateAgentState`.

- Expected: a plain session hosting a blocked agent reports `AgentState == "blocked"`
- Verify: `go test ./internal/tooling/worktree/`

#### Step 3: Worktree rows keep their panes too

Add `WorktreeStatus.Panes []tmux.PaneState`, filled from the same index `List()` already builds
(worktree.go:477). Pure addition — `AgentState` stays as it is.

- Verify: `go test ./internal/tooling/worktree/`

#### Step 4: A state mapper that is not worktree-shaped

Add `SessionStateFromAgent(windowActive bool, agentState string, dirtyCount int) SessionState`
holding the body of `SessionStateFromWorktree`, and make `SessionStateFromWorktree` a one-line
delegate. Callers of the old name are untouched.

- Verify: `go test ./internal/tui/components/`

#### Step 5: Session rows show it

`rowSession` render (model.go:1300): when `r.session.AgentState == ""`, keep today's
`SessionDot(attached)`. Otherwise render `StatusDot(SessionStateFromAgent(true, state, 0))`.
Width arithmetic is unchanged — one glyph either way.

- Manual: with an agent idle in a plain session, `dg ws` shows `◆` on that row
- Verify: `go test ./internal/tui/worktree/`

#### Step 6: Repo headers show it

`rowRepo` render (model.go:1273): aggregate the repo's children's `AgentState` in `buildRows`
into `row.agentState`, and render its dot before the chevron. Update that branch's width
arithmetic by the two columns the dot and its space add.

- Manual: collapse a repo whose worktree is blocked → header shows `!`
- Verify: `go test ./internal/tui/worktree/`

#### Step 7: Pane rows

`rowPane` in `tree.go`, carrying one `tmux.PaneState`. In `buildRows`, emit pane children for
any expanded `rowSession`/`rowWorktree` parent with **2+** panes whose state is non-empty.
Include them in `leafIndices` so the cursor can land on them. Render as
`indent + dot + pane_index + " " + pane_current_command` — which needs `PaneStates()` to also
select `#{pane_index}` and `#{pane_current_command}` (extend the format string and `PaneState`;
`SplitN` count moves 4 → 6).

- Verify: `go test ./internal/apps/tmux/ ./internal/tui/worktree/`

#### Step 8: Parent-keyed collapse

`m.collapsed[repo]` becomes `m.collapsed[parentKey]` where a repo's key is its slug and a
session's is `"session:" + name` (prefixed so a repo named like a session cannot collide).
Update `h`/`l` (model.go:817, 839) to compute the key from the cursor row.

- Manual: `l` on a session with two agents expands to two pane rows; `h` collapses
- Verify: `go test ./internal/tui/worktree/`

#### Step 9: Part A tests and docs

Table tests for: session aggregation precedence, `wt-` exclusion derived from the new scan,
repo-header aggregation, pane-row emission at the 2+ threshold, `leafIndices` with pane rows,
collapse-key collision between a repo and a session of the same name. Update `docs/spec.md`.

- Verify: `go test ./... && make lint`

### Part B

#### Step 10: Sound in `agent-state.sh`

After the existing writes, and only for `idle|blocked|error`:

```sh
[ "$(tmux show-option -gqv @dg_notify_sound)" = "on" ] || exit 0
[ "$(tmux display-message -p -t "$TMUX_PANE" '#{window_active_clients}')" = "0" ] || exit 0
```

then a `play_notify_sound <state>` helper: pick the file per state, probe `afplay` then
`paplay`, fall back to `printf '\a'`, and fire it detached —
`( cmd >/dev/null 2>&1 & ) >/dev/null 2>&1 || true`. Nothing here may exit non-zero.

- Manual: `tmux set-option -g @dg_notify_sound on`, run `agent-state.sh idle` from a pane in an
  unattended window → sound; from the attended one → silence
- Verify: `go test .`

#### Step 11: Sound in `notify.js`

Generalize the seam to `execFn(cmd, args) -> stdout` — the header comment's one-seam rule is
the reason: reading `show-option`/`display-message` needs stdout, and the player is a second
binary, so a separate seam would be exactly the "test mocks one, forgets the other" bug that
comment exists to prevent. Update `execTmux` and every existing call, then add the gate,
switch, and player alongside `mirrorWindowState` for the same three states.

- Verify: `node --test notify.test.mjs`

#### Step 12: The setting

`WorktreeConfig.NotifySound bool` (`fromFile.go:89`) — a plain `bool` is right here, unlike
`AttachAfterCreate`: the default is `false`, so the zero value already means "off". Render it
into `tmux.conf` as `set -g @dg_notify_sound "on"|"off"`, and add one `dg config` registry
entry if that cycle has landed.

- Manual: `dg configure tmux --force`, reload, `tmux show-option -gqv @dg_notify_sound`
- Verify: `go test ./internal/config/ ./internal/apps/tmux/`

#### Step 13: Part B tests

Extend `agent_state_test.go`'s fake-`tmux` harness to answer `show-option` and
`display-message`, and add a fake player on PATH that records invocations. Assert: silence when
off, silence when `window_active_clients != 0`, silence on `busy`, distinct file per state,
still exits 0 when the player is missing entirely. Mirror all of it in `notify.test.mjs`.

- Verify: `go test . && node --test notify.test.mjs`

#### Step 14: Deploy, verify, flip the ADRs

`dg configure claude --force`, `dg configure opencode --force`, `dg configure tmux --force`,
then run a real agent to idle in an unattended window. Set both ADRs to **ACCEPTED** and mark
this doc **Complete**.

---

## 6. Verification Plan

### Automated

```bash
go test ./... -cover
node --test notify.test.mjs
make lint
```

### Manual

1. Agent idle in a plain (non-worktree) session → that session's row shows `◆`
2. Agent blocked in a collapsed repo's worktree → repo header shows `!`
3. Two agents in one window, one finished → expand → the finished pane's row shows `◆`
4. `@dg_notify_sound off` → no sound on any transition
5. `on`, agent finishes in an unattended window → sound; in the window you are viewing → silence
6. `blocked`, `idle`, and `error` are audibly different
7. `PATH` stripped of `afplay`/`paplay` → silence, hook still exits 0
8. Agent outside tmux → no error, no output

### Regression

- `dg ws` cursor movement, filter, `h`/`l`, `d d`, `n`/`N`, `R` all behave as before
- A worktree row's dot is identical to today for every state
- Both agents still write `@dg_agent_state` and the window mirror (existing tests unchanged)
- `internal/apps/opencode/permissions_test.go` still passes — no permission change here

---

## 7. Risks & Trade-offs

| Risk                                                          | Likelihood | Mitigation                                                                             |
| ------------------------------------------------------------- | ---------- | -------------------------------------------------------------------------------------- |
| Row-model change breaks cursor/filter invariants              | Med        | Pane rows go through `leafIndices`; the existing cursor tests are the guard            |
| Width arithmetic drift misaligns columns                      | Med        | Update the constant comment in the same branch; check at 80 and 200 cols               |
| `execFn` signature change silently drops a `notify.js` caller | Med        | One seam by design; the Node test asserts every call's argv                            |
| Two hook calls added per turn end                             | Low        | Both are tmux queries the hook is already paying to talk to; gate exits early when off |
| Sound fires for an agent you are watching on another monitor  | Low        | Accepted in ADR-0009 — same imprecision the status bar already has                     |
| Collapse-key collision (repo named like a session)            | Low        | `"session:"` prefix, with a test                                                       |

### Trade-offs Made

- **Session rows lose the square shape once an agent reports.** One state vocabulary beats two;
  the `session` label still marks the kind. Relaxes ADR-0003 — recorded in ADR-0008.
- **Pane rows appear only at 2+ stateful panes.** Below that the parent's dot already says it.
- **No bundled audio.** Stock OS sounds and a bell fallback, versus licensing and `embed`
  weight for an off-by-default feature.

---

## 8. Cross-Model Review Notes

- [ ] Is the 2+ threshold for pane rows right, or should expansion always be available?
- [ ] Is `"session:"` the right collapse-key prefix, or should the map become a typed struct key?
- [ ] Does `execFn(cmd, args) -> stdout` still honour the one-seam guarantee in spirit?
- [ ] Should Part B ship first? It is independent of Part A and closes the reported gap alone.

**Reviewer notes:**
(unfilled)
