# Cycle: Agent activity notifications in `dg ws`

**Date:** 2026-07-28
**Estimated Duration:** ~6 hours
**Status:** Done
**Order:** **Cycle 1 of 2. Start here.** The companion cycle
[2026-07-28-ws-kick-review.md](2026-07-28-ws-kick-review.md) is blocked on this one and must
not begin until this ships.
**Governed by:** [ADR-0005](../../decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md)
(ACCEPTED) — read it before Step 1; it fixes the state model, and re-litigating it mid-cycle
breaks the companion cycle.

> **Starting a fresh session on this doc?** Do **Step 0 first** — it settles the one Claude
> hook mapping that is genuinely unverified, and Step 3 is unwritable without the answer.
> Step 4 (the `List()` single-scan refactor) is the riskiest edit in the cycle: it changes
> how `WindowActive` is derived for every dashboard row. Land it on its own commit with the
> existing `List()` tests passing unmodified before building anything on top of it.

---

## 1. Domain Context

`dg ws` lists every worktree and standalone tmux session in one dashboard, one row each,
with a colored status dot. Today the dot answers only "does this worktree have a live tmux
window?" — it says nothing about what the AI coder inside that window is doing. A coder
that finished ten minutes ago and a coder mid-run look identical. So does one that has been
sitting on an unanswered permission prompt the whole time, which is the more expensive
case: that is dead time you only discover by switching to the window and looking.

This cycle wires the dot to the coder's real state. It implements the signal described in
[ADR-0005](../../decisions/ADR-0005-agent-activity-state-in-tmux-pane-options.md):
the coder writes a tmux pane option when it changes state, and the dashboard reads every
pane's option in one scan per refresh.

This is Phase 3 of
[2026-06-07-worktree-v2-tui-dashboard.md](2026-06-07-worktree-v2-tui-dashboard.md)
(line 37), deferred at the time. ADR-0005 revises that doc's "marker file" sketch — read
its "Rejected alternatives" section before proposing a different store.

It is a **general** mechanism, not a review feature: it reports every turn of every coder
in every worktree window, however that window was started. The `R`-to-kick-a-review flow
in [2026-07-28-ws-kick-review.md](2026-07-28-ws-kick-review.md) is a separate cycle that
consumes this one's output; nothing here is review-specific.

---

## 2. Engineer Context

**Relevant files:**

- `internal/tui/components/statusdot.go` — `SessionState` enum, `SessionStateFromWorktree`,
  `StatusDot` / `StatusGlyph`. `StateNeedsReview` (purple `◆`) already exists here, unused.
- `internal/tui/components/styles.go` — `Palette`. Has `NeedsReview` (ANSI 5); needs
  entries for the blocked and error states.
- `internal/tui/components/notification.go` — `Toast` + `ToastNeedsReview`. Built, no caller.
- `internal/tui/worktree/model.go` — the dashboard. Line 27 `refreshInterval` (3s), line 1286
  the `SessionStateFromWorktree(r.status, false, 0)` call with the hardcoded `false`,
  line 459 `applyStatuses`, line 948 `handleAttach`.
- `internal/apps/tmux/tmux.go:259` — `SessionWindows()`, the existing single-exec
  `list-windows -a -F` scan. The new pane scan is written by analogy to it, not on top of it.
- `internal/apps/tmux/tmux.go:210` — `WindowSession()`. **Runs a full `SessionWindows()` scan
  on every call.** `List()` calls it once per worktree, so the dashboard costs N tmux execs
  per 3-second refresh today. Step 4 fixes that; do not add another per-worktree lookup.
- `internal/tooling/worktree/worktree.go:415` — `List()`, the per-worktree loop that does it.
- `internal/tooling/worktree/worktree.go:62` — `WorktreeStatus`, the struct the dashboard
  renders per row.
- `configs/opencode/plugin/` — OpenCode plugins. Whole-directory copy at configure time
  (`internal/apps/opencode/opencode.go:144`), so a new file ships with no registration.
- `configs/claude/settings.json.tmpl` — Claude hooks. Scripts deploy from the list at
  `internal/apps/claude/claude.go:109`.

**Key APIs (verified against the installed toolchain, not assumed):**

- OpenCode plugin hook surface: `Hooks.event?: (input: { event: Event }) => Promise<void>`
  — `@opencode-ai/plugin/dist/index.d.ts:171`. Relevant event types: `session.idle`
  (payload: `{ sessionID }`), `session.error`. Plus the separate `chat.message` hook
  ("called when a new message is received") for the turn-start write.
- **Correction (2026-07-28, Step 2 prep).** `permission.asked` does **not** exist in the
  installed SDK (`@opencode-ai/sdk` 1.17.20, matching CLI `opencode --version` 1.18.9). The
  `Event` union (`@opencode-ai/sdk/dist/gen/types.gen.d.ts:602`) has no member of that name.
  The correct event is `permission.updated` (`EventPermissionUpdated`, line 384-387:
  `{ type: "permission.updated", properties: Permission }`), which fires when a `Permission`
  request record is created — this is "a permission dialog appeared," the same moment
  Claude's `Notification`/`permission_prompt` observes. There is also a distinct top-level
  `Hooks["permission.ask"]` (not an `event` type — a separate decision hook,
  `(input: Permission, output: { status: "ask"|"deny"|"allow" }) => Promise<void>`, meant to
  auto-decide the prompt) and `permission.replied` (fires once the prompt is answered) — do
  not use either for this cycle: `permission.ask` would change decision behavior, and
  `permission.replied` needs no explicit write because the agent's next real state (`busy`
  continuing, or `idle`/`error` when the turn ends) already gets written by the existing
  handlers. Step 2 subscribes to `session.idle` → `idle`, `permission.updated` → `blocked`,
  `session.error` → `error` via the `event` hook, plus `chat.message` → `busy`.
- Plugin export shape: `export const Name = async (ctx = {}) => ({ "hook.name": fn })`, with
  `ctx.directory` / `ctx.worktree` for the working directory. See `task-redirect.js` for the
  house pattern.
- tmux 3.5a: `tmux set-option -p -t "$TMUX_PANE" @dg_agent_state idle` writes it,
  `list-panes -a -F '...#{@dg_agent_state}'` reads every pane in one exec, and a pane with
  the option unset renders as an empty field rather than erroring.

**Verified (2026-07-28, Step 0).** Claude Code's hook for "blocked on a permission
prompt" is `Notification` with `matcher: "permission_prompt"`. Confirmed against the
live docs at `code.claude.com/docs/en/hooks` (not the settings schema, which does not
enumerate event names or matcher values at all — it only shapes `hookMatcher`/
`hookCommand` objects, so any event key validates against the pinned `$schema`):

- The `Notification` event's `matcher` filters on **notification type**, with example
  values `permission_prompt`, `idle_prompt`, `auth_success`, `elicitation_dialog`,
  `elicitation_complete`, `elicitation_response`, `agent_needs_input`, `agent_completed`.
  Registering `Notification` with `matcher: "permission_prompt"` fires only on the
  permission-prompt case — `idle_prompt` and the others are excluded, so the earlier
  "unfiltered Notification maps idle nags onto blocked" risk does not apply once the
  matcher is set.
- `PermissionRequest` (a separate event, "when a permission dialog appears") is **not**
  the right hook here: it is a blocking decision hook (`hookSpecificOutput.decision.behavior:
"allow"|"deny"`) meant to auto-decide the prompt, not to observe that one appeared.
  `Notification` is the observation-only signal this cycle needs.
- `UserPromptSubmit` fires "when you submit a prompt, before Claude processes it" —
  confirmed before the turn begins, as Step 3 requires for the `busy` write.

**Step 3 registers:** `Stop` → `idle` (no matcher), `UserPromptSubmit` → `busy` (no
matcher), `Notification` with `matcher: "permission_prompt"` → `blocked`.

**Testing patterns:** [testing-patterns.md](../../guides/testing-patterns.md). Everything
mocked — `testutil.MockApp`, `testutil.VerifyNoRealCommands`, never a real `tmux` call. The
OpenCode plugin is JavaScript and is tested the way `task-redirect.js` is, via
`task-redirect.test.mjs` at the repo root.

```bash
go test ./internal/tui/... ./internal/apps/tmux/ ./internal/tooling/worktree/
node --test notify.test.mjs
make lint
```

---

## 3. Objective

A worktree row in `dg ws` shows, within one refresh interval and without the user switching
to the window, whether its AI coder is working, finished and waiting, blocked on a
permission prompt, or errored — for both OpenCode and Claude Code.

---

## 4. Scope Boundary

### In Scope

- [x] OpenCode plugin `configs/opencode/plugin/notify.js` mapping `session.idle` /
      `permission.updated` / `session.error` / `chat.message` onto `@dg_agent_state`
- [x] Claude Code hooks writing the same option — turn end, turn start, and permission
      requested (see the note below — this is a deliberate scope call, flag it if you
      disagree)
- [x] A `Tmux.PaneStates()` primitive: one `list-panes -a` scan carrying `@dg_agent_state`
- [x] **Refactor `WorktreeManager.List()` to a single tmux scan**, populating both
      `WindowActive` and a new `WorktreeStatus.AgentState` from it
- [x] Per-pane state with an explicit `busy` value, aggregated per row (ADR-0005)
- [x] Two new `Palette` entries and two new `SessionState` values, with glyphs
- [x] `model.go:1286` fed the real state instead of the literal `false`
- [x] State cleared when the user attaches from `dg ws`
- [x] Status-bar signal for a worktree whose agent wants attention while you are elsewhere
- [x] Tests for every above item; docs updated

### Explicitly Out of Scope

- **Desktop notifications** (osascript / notify-send). Cross-platform surface of its own,
  and the status-bar signal covers the "tell me while I'm in another window" case.
- **Toast rendering in `dg ws`.** `notification.go` stays unused this cycle; the dot is the
  whole UI. Revisit once the dot has proven the signal is accurate.
- **The `R` review-kick flow.** Separate cycle, depends on this one.
- **Standalone session rows.** Only worktree rows get an agent state; a plain tmux session
  has no devgeta-managed coder. `SessionDot` is untouched.
- **Showing _which_ pane wants attention.** State is collected per pane, but a row renders
  one aggregated dot. A user with a reviewer and a coder in one window sees `◆` and must
  look to find out which finished. Per-pane display is a later cycle; the data will be
  there for it.
- **Any change to what counts as "dirty."** The existing `dirtyCount` parameter of
  `SessionStateFromWorktree` stays hardcoded `0`; wiring that up is its own cycle.

**Scope is locked.**

> **Scope note on Claude Code — please confirm.** The request that prompted this work said
> "default to opencode only." That was about which coder the `R` review flow launches, and
> it holds for that cycle. It reads differently here: `claude` and `claude-nvim` are shipped
> layouts (`layout.go:98`), so an OpenCode-only signal means those windows silently never
> change state, and the dashboard is quietly wrong for anyone using them. ADR-0005 fixed the
> storage to be agent-neutral precisely so the Claude side is one hook block plus a
> three-line script, not a second design. Included on that basis. Cut Step 3 if you'd rather
> ship OpenCode first — nothing else in the cycle depends on it.

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                                 | Description                                        |
| ------ | --------------------------------------------------------- | -------------------------------------------------- |
| Create | `configs/opencode/plugin/notify.js`                       | `event` hook → `@dg_agent_state`                   |
| Create | `notify.test.mjs`                                         | Plugin tests, mirroring `task-redirect.test.mjs`   |
| Create | `configs/claude/agent-state.sh`                           | Claude hook writing the same option                |
| Modify | `configs/claude/settings.json.tmpl`                       | Register the hooks confirmed in Step 0             |
| Modify | `internal/apps/claude/claude.go:109`                      | Add `agent-state.sh` to the deployed-scripts list  |
| Modify | `internal/apps/tmux/tmux.go`                              | New `PaneStates()`: one `list-panes -a` scan       |
| Modify | `internal/tooling/worktree/worktree.go:415`               | `List()` single-scan refactor (see Step 4)         |
| Modify | `internal/tooling/worktree/worktree.go:62`                | `WorktreeStatus.AgentState`                        |
| Modify | `internal/tui/components/styles.go`                       | `Blocked` + `Error` palette entries                |
| Modify | `internal/tui/components/statusdot.go`                    | `StateBlocked` / `StateError`; extend the mapper   |
| Modify | `internal/tui/worktree/model.go:1286`                     | Pass the real state                                |
| Modify | `internal/tui/worktree/model.go:948`                      | Clear the option on attach                         |
| Modify | `configs/tmux/tmux.conf:114`                              | `window-status-format` keyed off `@dg_agent_state` |
| Modify | `docs/apps/claude.md`, `internal/apps/opencode/README.md` | Document the hook/plugin                           |
| Modify | `docs/spec.md`                                            | Document the dashboard's agent states              |

### Step-by-Step

#### Step 0: Confirm the Claude hook surface before writing it

The OpenCode side was verified against the installed SDK types. The Claude side was **not**,
and one mapping is genuinely unknown. Settle these against the installed Claude Code version
and record the answers here before Step 3:

1. **What event fires when Claude blocks on a permission prompt?** The schema this repo pins
   (`json.schemastore.org/claude-code-settings.json`, referenced at the top of
   `settings.json.tmpl`) does not describe a `Notification` hook at all; it does list a
   `PermissionRequest` event, and describes `matcher` only as "Optional pattern to match
   event contexts... Behavior depends on event type."
2. **If it is `Notification`, what matcher narrows it to permission prompts?** An unfiltered
   `Notification` hook maps _every_ notification — including idle nags — onto `blocked`,
   which is wrong. Do not register one unfiltered.
3. **Does `UserPromptSubmit` fire before the turn begins?** Step 3 relies on it to write
   `busy`.

Whatever is registered must validate against the pinned `$schema`.

#### Step 1: tmux read path

Add `Tmux.PaneStates()` returning one entry per pane from a single exec:

```
list-panes -a -F '#{session_name}\t#{window_name}\t#{pane_id}\t#{@dg_agent_state}'
```

Match `SessionWindows()`'s existing tolerance: a malformed line is skipped, a failed exec
returns nil. Use `SplitN(..., "\t", 4)` and note an unset option yields a **trailing empty
field** — the parse must keep the line, not discard it as short.

- Verify: `go test ./internal/apps/tmux/` — include a fixture line with an empty last field.

#### Step 2: OpenCode plugin

Create `configs/opencode/plugin/notify.js` following `task-redirect.js`'s export shape:
`session.idle` → `idle`, `permission.updated` → `blocked` (see the Key APIs correction above —
not `permission.asked`, which does not exist in the installed SDK), `session.error` → `error`,
and the `chat.message` hook → `busy`.

`busy` is written explicitly, never by clearing (ADR-0005): an unset pane must keep meaning
"no agent here," so that an nvim pane in a `claude-nvim` window is not read as a working
agent. Write via `tmux set-option -p -t "$TMUX_PANE" @dg_agent_state <value>`, and no-op when
`TMUX_PANE` is unset — OpenCode run outside tmux must not error.

Guard against a plugin failure taking down the session: wrap the write, swallow its error.
A missing dot is a cosmetic bug; a crashed coder is not.

- Verify: `node --test notify.test.mjs`, plus manually — ask the coder something trivial and
  watch `tmux list-panes -a -F '#{pane_id} #{@dg_agent_state}'` go `busy` then `idle`.

#### Step 3: Claude Code hooks

Create `configs/claude/agent-state.sh` (same shebang/comment-header convention as
`format.sh`), register it per Step 0's answers, and add it to the deploy list at
`claude.go:109`.

| Event                            | Writes    |
| -------------------------------- | --------- |
| `Stop`                           | `idle`    |
| `UserPromptSubmit`               | `busy`    |
| permission event (Step 0 answer) | `blocked` |

`UserPromptSubmit` → `busy` is not optional. Without it a row stays `idle` or `blocked` for
the entire next turn — the dashboard would report "your move" while Claude is actively
working, which is worse than showing nothing. The OpenCode plugin's `chat.message` handler
is the counterpart; the two must stay behaviourally matched.

Note: `Stop` fires at the end of **every** assistant turn, including one merely waiting for
your next message. That is the intended meaning of `idle` — "your move" — not "the task is
complete."

Same `TMUX_PANE`-absent no-op as the plugin.

- Verify: `go test ./internal/apps/claude/`; deploy with `dg configure claude --force` and
  watch a pane cycle `busy` → `idle` across two turns.

#### Step 4: Single-scan `List()` refactor

`WorktreeManager.List()` (`worktree.go:415`) currently calls `w.Tmux.WindowSession(windowName)`
once per worktree, and `WindowSession` (`tmux.go:210`) runs a **fresh `list-windows -a` scan
every call**. That is N tmux execs per 3-second refresh today — a pre-existing cost, not one
this cycle introduces, but one that makes "just add another per-worktree lookup" the wrong
move.

Refactor: call `PaneStates()` **once** at the top of `List()`, index it by window name, and
populate both `WindowActive` (window present in the index) and `AgentState` (aggregated over
that window's panes) from the same scan. Add `AgentState string` to `WorktreeStatus`.

Aggregation, most-urgent-first per ADR-0005:

```
blocked > error > idle > busy > (no agent)
```

`idle` outranking `busy` is what lets a finished reviewer show `◆` while a coder in the
neighbouring pane keeps working — the split-pane case the review cycle depends on.

Leave `WindowSession` in place for its other callers; this is a change to `List()`, not a
removal.

- Verify: `go test ./internal/tooling/worktree/` — assert one scan for a multi-worktree
  fixture, plus a case with two panes at different states.

#### Step 5: Palette and state enum

Add `Blocked` (ANSI 1, red — it is the state that costs you the most) and `Error` (ANSI 1,
dim or bold to distinguish) to `Palette`. Add `StateBlocked` and `StateError` to
`SessionState` and to both `StatusDot` and `StatusGlyph`.

Glyph choice: `StateNeedsReview` keeps `◆`. `StateBlocked` needs a shape distinct from both
`●` and `◆` at a glance — propose `!`. Do not reuse `Dirty`'s yellow: it does not currently
collide (`SessionStateFromWorktree` only returns `StateDirty` when there is no live window)
but one color meaning two things is a readability trap.

Extend `SessionStateFromWorktree` to take the already-aggregated agent state from Step 4 —
aggregation belongs with the data, not in the renderer.

- Verify: `go test ./internal/tui/components/`

#### Step 6: Render it

Replace the `false, 0` at `model.go:1286` with the row's real agent state. Confirm the
selected-row path (`StatusGlyph` inside `Selected.Render`) and the unselected path
(`StatusDot`) both handle the new states — the split exists because nesting ANSI inside a
parent style corrupts it.

- Verify: `go test ./internal/tui/worktree/`; run `dg ws` against a worktree with a finished
  coder and see a purple `◆`.

#### Step 7: Clear on attach

In `handleAttach` (`model.go:948`), clear `@dg_agent_state` on the panes of the window being
attached to. Attaching is the user acknowledging the state; leaving it set would show `◆` on
the window you are actively sitting in.

Clear rather than write `busy`: the agent is not working, and the next real turn will write
`busy` itself.

- Verify: attach to a `◆` row, detach, confirm the dot is back to green.

#### Step 8: Status-bar signal for unattended worktrees

**Not a bell.** The earlier draft said "ring the bell" without saying how, and styling
`window-status-bell-style` (`configs/tmux/tmux.conf:143`) only controls how a window looks
_after_ a bell has occurred — it does not produce one. Emitting a real BEL from a hook means
writing to the pane's tty and depending on `monitor-bell`, which is fragile.

Drive the status bar off the option directly instead. `tmux.conf` currently sets
`window-status-style` (line 114) but never overrides `window-status-format`, so add one with
a conditional on `#{@dg_agent_state}` — declarative, reading the same single source of truth
the dashboard reads, with no BEL plumbing and no second mechanism to keep in sync.

Note `#{@dg_agent_state}` is a _pane_ option; check whether the window-status format can
reach it (`#{P:...}` / an active-pane fallback) and, if not, have the hook mirror the value
onto the window as a display-only convenience — the pane option stays authoritative.

**Resolved (2026-07-28, Step 8 prep).** Verified against `man tmux` (3.5a, matching the
installed version): `window-status-format` cannot reach a pane's own option value. Options
cascade **top-down only** — "Pane options inherit from window options" (`set-option(1)`) — so
a pane's own override is never visible looking the other way from its window's format
context, and `S:`/`W:`/`P:`/`L:` are **loop** constructs ("loop over each session, window,
pane or client and insert the format once for each"), not a way to address one specific
pane's value from outside a per-pane iteration. A mirror is required, as the plan anticipated.

**Critical: the window-level mirror must use a DIFFERENT option name than the pane-level
one — never `@dg_agent_state` itself.** Because pane options inherit from window options,
writing a window-level `@dg_agent_state` would make every pane in that window **without its
own override** (e.g. the nvim pane in a `claude-nvim` layout) start reading that mirrored
value via inheritance when `Tmux.PaneStates()` scans `list-panes -F '...#{@dg_agent_state}'`
— exactly the "an editor pane is not read as a working agent" guarantee ADR-0005 exists to
protect. Use a separate name, `@dg_window_agent_state`, for the mirror. This cannot collide
with or be inherited into the pane-level reads Step 1/Step 4 already built and reviewed.

**Mirror write semantics differ from the pane write:** `idle`/`blocked`/`error` (the
"wants-you" states) SET the window-level mirror to that value; `busy` CLEARS it (`-u`), not
writes `"busy"` — matching Step 3's reasoning for why `busy` must be written explicitly on
the pane side, applied here to stop the status-bar flag the moment a new turn begins, exactly
as it would be wrong to leave a row reading "your move" while Claude is actively working.
This means Steps 2 and 3's hooks (`notify.js`, `agent-state.sh`) each need one small,
additive change: write/clear the window-level mirror alongside the pane-level write they
already do. The pane-level write and its existing tests are unaffected.

On visibility gating: `#{window_active}` means "active within its session," **not** "visible
to an attached client." A window can be its session's active window while the client is
attached elsewhere. Use `#{window_active_clients}` (count of clients viewing the window,
confirmed present in `man tmux` 3.5a) for a real visibility test.

**Corrected (2026-07-28, final review).** The line above originally claimed the window-level
mirror does **not** need to be cleared by Step 7's `ClearAgentStateForWindow`, on the theory
that `window_active_clients == 0` going false on attach already makes the status-bar flag
disappear. That was wrong: `window_active_clients` only suppresses the flag _while the
window stays attached_ — if the user later detaches without a new `busy` write happening in
between, the stale mirror value resurfaces the flag on the next tick. This was caught in the
final whole-branch review and fixed in commit `363ac32` ("fix(tmux): clear window-level
agent state mirror on attach"), which extends `ClearAgentStateForWindow` to also clear
`@dg_window_agent_state` alongside the pane-level value. Do not revert that clear based on
the reasoning above — it was the bug, not the fix.

- Verify: leave a coder running, switch to another window, confirm the status bar flags it;
  confirm the window you are looking at is not flagged.

#### Step 9: Tests and docs

Fill any gaps from the checklist in CLAUDE.md §6, including the mock-safety traps. Document
the states in `docs/spec.md`, the Claude hook in `docs/apps/claude.md`, and the plugin in
`internal/apps/opencode/README.md`.

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/apps/tmux/ ./internal/apps/claude/ ./internal/apps/opencode/
go test ./internal/tooling/worktree/ ./internal/tui/...
node --test notify.test.mjs
go test ./... -cover
make lint
```

### Manual

1. `dg configure opencode --force && dg configure claude --force` after `make build` —
   configs are extracted from the running binary, so an old binary deploys an old config.
2. `dg ws` → create a worktree → ask the coder a trivial question → within 3s the row turns
   purple `◆`.
3. Trigger a permission prompt (e.g. `Bash(ssh …)`, which is on the `ask` list) → the row
   goes red.
4. Attach to that row → answer → detach → the row is green while it works, purple when done.
5. **Start a second turn while attached, then detach immediately** → the row must be green
   (`busy`), not purple. This is the check that catches a missing turn-start write.
6. Kill the window mid-run → the row drops to no-session. No stale `◆` (this is the failure
   mode a marker file would have had).
7. Run `opencode` outside tmux → no error, no output about tmux.
8. Repeat 2–5 with a `claude` layout worktree — the `claude-nvim` layout specifically, to
   confirm the nvim pane (which never writes the option) does not affect the row's dot.
9. **Two agents, one window:** split a second coder into a worktree window, let one finish
   while the other works → the row shows `◆`. Then start a new turn in the finished one →
   the row still shows `◆` only if the _other_ is idle. This is the aggregation rule under
   the exact condition the review cycle depends on.

### Regression Check

- `dg ws` renders unchanged for worktrees with no coder running.
- `dg wt` subcommands, `dg configure`, `dg install` unaffected.
- `internal/apps/opencode/permissions_test.go` still passes — this cycle adds no
  permission or formatter rules, so the two agents' symmetry is untouched.

---

## 7. Risks & Trade-offs

| Risk                                                             | Likelihood | Mitigation                                                                                           |
| ---------------------------------------------------------------- | ---------- | ---------------------------------------------------------------------------------------------------- |
| Plugin/hook error kills or hangs the coder session               | Med        | Wrap every tmux write, swallow errors, no-op without `TMUX_PANE`. A missing dot is cosmetic.         |
| Claude's permission event/matcher is not what we assume          | High       | Step 0 confirms it before any hook is written; nothing is registered unfiltered                      |
| A missing turn-start write leaves the row stale for a whole turn | Med        | Explicit `busy` writes on both sides (Step 2/3) plus manual check 5, which exists only to catch this |
| `Stop` semantics differ from expectation ("done" vs "your turn") | High       | Documented in Step 3 and in `docs/spec.md`; the visibility gate in Step 8 is what makes it tolerable |
| `List()` refactor changes `WindowActive` behavior                | Med        | Same source of truth, one scan instead of N; existing `List()` tests must pass unmodified            |
| Status-bar format cannot read a pane option                      | Med        | Step 8 checks this explicitly and falls back to a display-only window mirror                         |
| OpenCode event names change across versions                      | Med        | Names come from the installed SDK types, not docs; plugin no-ops on an unknown event                 |
| `list-panes` parsing breaks on an empty trailing field           | Low        | Explicit test case in Step 1                                                                         |
| Aggregated dot hides which pane wants attention                  | Certain    | Accepted and scoped out; per-pane display is a later cycle and the data will already be collected    |

### Trade-offs Made

- **State per pane, one dot per row.** Collecting per pane costs the same single exec as
  collecting per window and is what makes two coders in one window work. Rendering stays one
  dot: the aggregation rule surfaces the most urgent state, and _which_ pane it came from is
  a display problem deferred to a later cycle.
- **Explicit `busy` rather than clearing.** Slightly more hook code, in exchange for being
  able to tell "an agent is working here" from "there is no agent here" — which the
  `claude-nvim` layout requires and clearing cannot express.
- **Dot only, no toast this cycle.** `notification.go` stays unused. The dot is cheap to get
  right; a toast has placement and dismissal behavior to design, and is worth nothing until
  the underlying signal is trusted.
- **Status-bar format instead of a bell or a desktop notification.** Declarative, driven by
  the same option the dashboard reads, no BEL plumbing, works outside `dg ws`. A real
  desktop notification is a logged follow-on, not a gap.

---

## 8. Cross-Model Review Notes

- [x] Is the Claude-side scope call in §4 the right one, or should this ship OpenCode-first?
      **Resolved:** included, as planned — Claude hooks shipped alongside OpenCode's.
- [x] Is `!` the right blocked glyph, and red the right color, given `Armed` also uses ANSI 1?
      **Resolved:** shipped as designed (`!` blocked, `✕` error, both red-family) — no
      confusion found with `Armed` in review, since `Armed` is a row background style, not a
      status glyph color.
- [x] Should `error` be its own state, or fold into `blocked` as "needs you"?
      **Resolved:** kept separate (`StateError`, distinct glyph/color) — see Step 5.
- [x] Does clearing on attach lose information a user might want to keep (e.g. attaching to
      look at something else, then losing the "this finished" marker)?
      **Resolved:** yes, for `blocked` specifically — attaching clears it even if the
      permission prompt is still unanswered. Raised by the final whole-branch review; the
      user was asked and chose to keep this behavior (ship as-is), documented in ADR-0005.
- [x] Is `idle > busy` the right aggregation precedence? It means a row with one finished and
      one working agent reads as "wants you," which is intended — but it also means a single
      stale `idle` pane can mask an otherwise-busy window.
      **Resolved:** yes, shipped as designed — this is the split-pane review case the
      precedence exists for.
- [x] Should the `List()` single-scan refactor be split into its own commit ahead of this
      cycle, given it fixes a pre-existing N-execs-per-refresh cost independent of the feature?
      **Resolved:** landed as its own commit within this cycle (Step 4,
      `a6e54a6`), per this doc's own pre-flight instruction, rather than as a separate
      preceding cycle.

**Reviewer notes:**

**Round 1 (2026-07-28).** Seven findings, all verified against the codebase and all
accepted; see the git history of this file for the before state.

- The split-pane/last-writer-wins contradiction was real and is fixed at the source: ADR-0005
  now stores state per **pane** with an explicit `busy` value. Verified `set-option -p` and a
  one-exec `list-panes -a` read on tmux 3.5a first — the fix costs the same single exec the
  window-level design did, so there was no reason to fall back to refusing concurrent reviews.
- The missing turn-start clear, the unfiltered `Notification` matcher, the wrong `List()`
  claim, the `ConfigsFS` package boundary, the undefined bell, the wrong `#{window_active}`
  semantics, and the missing pane rollback were all confirmed and are addressed in Steps 0–8
  here and in the review cycle's Steps 1 and 3.
- On `List()`: it is worse than reported. `WindowSession` runs a **fresh** `list-windows -a`
  per call, so today's cost is N execs per 3-second refresh, not one scan being reused.
- On the Claude matcher: the concern is correct, but the specific names `permission_prompt` /
  `idle_prompt` could not be confirmed. The pinned settings schema does not describe a
  `Notification` hook at all and lists a `PermissionRequest` event instead. Step 0 now settles
  this against the installed version rather than baking in an unverified name.
