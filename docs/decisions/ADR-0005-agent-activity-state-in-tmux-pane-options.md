# ADR-0005 — Agent activity state lives in a tmux pane option

**Date:** 2026-07-28
**Status:** ACCEPTED

> **Revision (2026-07-28, pre-implementation).** The first draft of this ADR stored the
> state in a **window** option and treated "busy" as the absence of a value. Review found
> that combination cannot support two coders in one window — the exact arrangement
> [2026-07-28-ws-kick-review.md](../plans/cycles/2026-07-28-ws-kick-review.md) needs, where
> a review runs in a split pane beside a working coder. Either coder's write would clobber
> the other's, so the row could not reliably report that the review finished. This revision
> moves the state to a **pane** option and makes `busy` an explicit value. Nothing else
> about the decision changed; the rejected alternatives below are unaffected.

## Context

`dg ws` shows one row per worktree, colored by a status dot. Today that dot only
distinguishes "has a live tmux window" from "doesn't" — it cannot say whether the AI
coder running inside that window is still working, has finished and is waiting on you, or
is blocked on a permission prompt. Finding that out means switching to the window and
looking.

The dashboard already has the display half built and dead-wired:

- `internal/tui/components/statusdot.go:9` defines `StateNeedsReview` with a purple `◆`
  glyph, and `SessionStateFromWorktree(s, needsReview, dirtyCount)` takes a `needsReview`
  parameter.
- `internal/tui/worktree/model.go:1286` calls it with a literal `false`. Nothing has ever
  fed it.
- `internal/tui/components/notification.go:13` defines a `ToastNeedsReview` toast kind
  that no caller uses.

This was scoped as Phase 3 of `docs/plans/cycles/2026-06-07-worktree-v2-tui-dashboard.md`
(line 37) and deferred. That doc's one-line sketch proposed "a marker file the TUI
watches." This ADR revisits that choice before implementation, per CLAUDE.md §6
("investigate before designing new state or mechanisms").

Both coders can already report the transition. This is not new capability we have to
build — it is an existing signal we have to route:

| Coder    | Mechanism           | Events                                                            |
| -------- | ------------------- | ----------------------------------------------------------------- |
| OpenCode | plugin `event` hook | `session.idle`, `permission.updated`, `session.error`             |
| Claude   | hooks               | turn end, turn start, permission requested (see the caveat below) |

(OpenCode's hook surface is `Hooks.event` in
`@opencode-ai/plugin/dist/index.d.ts:171`; the event union is in the SDK's `types.gen.d.ts`.
Both were read off the installed package, not from documentation.)

**Correction (2026-07-28, implementing cycle Step 2 prep).** This table originally said
`permission.asked`. Re-verified against the installed `@opencode-ai/sdk` (1.17.20): no event
of that name exists. The event that fires when a permission request appears is
`permission.updated` (`EventPermissionUpdated`, `properties: Permission`). Nothing about the
storage decision above changes — this corrects only the event name feeding it.

**Caveat on the Claude column (resolved).** `Stop` and `UserPromptSubmit` were already
confirmed. The permission-prompt event was settled in the implementing cycle's Step 0: it is
`Notification` with `matcher: "permission_prompt"`. The settings schema this repo pins
(`json.schemastore.org/claude-code-settings.json`) does not enumerate event names or matcher
values at all, so it could not confirm or rule this out — the answer came from the live docs
at `code.claude.com/docs/en/hooks` instead. `Notification`'s `matcher` filters on
notification type (`permission_prompt`, `idle_prompt`, `auth_success`, and others), so
registering it with `matcher: "permission_prompt"` fires only on the permission-prompt case
and excludes `idle_prompt` and the rest — the earlier "unfiltered `Notification` maps idle
nags onto `blocked`" risk does not apply once the matcher is set. `PermissionRequest` (also
in the schema) was considered and rejected: it is a blocking decision hook meant to
auto-decide the prompt, not an observation-only signal.

Constraints the storage choice has to satisfy:

1. **The state is about one running agent, and dies with it.** A pane killed mid-run must
   not leave a stale "finished" marker behind.
2. **Two agents can share one window.** The review flow splits a reviewer pane into a
   worktree window that may already hold a working coder. Each must report independently.
3. **It is read on a 3-second timer.** `dg ws` refreshes every `refreshInterval`
   (`model.go:27`). Whatever holds the state gets polled on every tick, for every row.
4. **It is written from inside the pane, by a hook with no devgeta context.** The hook
   knows its own cwd and `$TMUX_PANE`. It does not know the repo slug or the flattened
   worktree name that devgeta uses to key a row.
5. **It must be agent-neutral.** `dg ws` must not care whether OpenCode or Claude wrote it.

## Decision

Record agent activity in a **tmux pane user option**, `@dg_agent_state`, on the pane the
agent is running in.

| Value             | Meaning                               | Written by                                     |
| ----------------- | ------------------------------------- | ---------------------------------------------- |
| _(unset / empty)_ | **no agent in this pane** — e.g. nvim | never written; this is the absence of a writer |
| `busy`            | an agent is working                   | `chat.message` / Claude `UserPromptSubmit`     |
| `idle`            | finished a turn; your move            | `session.idle` / Claude `Stop`                 |
| `blocked`         | waiting on a permission answer        | `permission.updated` / Claude `Notification`   |
| `error`           | the turn failed                       | `session.error`                                |

`busy` is an **explicit value, not the absence of one**. Distinguishing "an agent is
working" from "there is no agent here" is what lets a row aggregate correctly: a
`claude-nvim` window has an editor pane that never writes, and an unset pane must not be
mistaken for a working agent.

**Write** (from a hook running in the pane):

```sh
tmux set-option -p -t "$TMUX_PANE" @dg_agent_state idle
```

`$TMUX_PANE` is the pane's own id, so the hook needs no devgeta path knowledge and cannot
target the wrong pane.

**Read** (from the dashboard): one `list-panes -a` exec per refresh.

```
#{session_name}\t#{window_name}\t#{pane_id}\t#{@dg_agent_state}
```

**Aggregate** a worktree row from its window's panes, most-urgent-first:

```
blocked > error > idle > busy > (no agent)
```

`idle` outranking `busy` is deliberate and is what makes the split-pane review work: a
finished reviewer still shows `◆` while the coder beside it keeps working. The cost is
that one dot cannot say _which_ pane finished — see Consequences.

Verified against tmux 3.5a: `set-option -p` through `$TMUX_PANE`, reading back through
`list-panes -a -F`, and unset panes rendering as an empty field all behave as described.

### Window-level mirror for the status bar

`dg ws` reads the pane option, but tmux's own status bar cannot: `window-status-format`
only ever sees window-scoped context, and pane options inherit **downward** from window
options (tmux's own option-inheritance model), never the other way — there is no format
token that reaches one specific pane's value from a window-level format string. Showing an
unattended, wants-attention window in the status bar therefore needs a second, window-level
option that mirrors the pane state: `@dg_window_agent_state`.

This mirror **must** use a different name than the pane-level `@dg_agent_state` — never
reuse it. Because pane options inherit from window options, writing the mirror under the
pane-level name would make every pane in that window **without its own override** (e.g. the
nvim pane in a `claude-nvim` layout) start reading the mirrored value through inheritance,
exactly the "an editor pane must not be misread as a working agent" guarantee this ADR
exists to protect.

The mirror's write semantics differ from the pane-level write: `idle`/`blocked`/`error` set
the mirror to that value, but `busy` **clears** it (rather than writing `"busy"`) — the same
reasoning as why `busy` is written explicitly at the pane level, applied here to when the
status-bar flag should disappear: the moment a new turn starts, the "wants you" flag must go
away, not linger until something else clears it.

`ClearAgentStateForWindow` (the function that runs when the user attaches to a window from
`dg ws`) clears **both** the pane-level value and this window-level mirror. An earlier draft
of the implementing cycle's plan assumed the mirror would not need explicit clearing, on the
theory that `window_active_clients == 0` going false on attach already suppresses the status
bar's display of it. That assumption was wrong: `window_active_clients` only suppresses the
flag _while the window is attached_ — if the user later detaches without a new `busy` write
happening in between, the stale mirror value resurfaces the flag. The code clears the mirror
explicitly; this is not optional.

**Accepted: attach clears `blocked` too, even if the prompt is still unanswered.** Attaching
to a window clears its state (pane-level and mirror) immediately, including `blocked` — even
though the underlying permission prompt may genuinely still be sitting there unanswered.
This is a deliberate simplification, not an oversight: it keeps attach-clearing one uniform
rule (clear on attach, full stop) instead of a special case that has to distinguish "cleared
because the user saw it" from "cleared even though the prompt is still open," consistent with
how `idle` is already cleared the same way.

### Rejected alternatives

**A window option with implicit busy** (this ADR's own first draft). Two coders in one
window overwrite each other, and unset cannot distinguish a working agent from an editor
pane. Superseded before implementation — see the revision note above.

**A marker file per worktree** (the original Phase 3 sketch). Rejected on four counts:

- _Cleanup._ Nothing deletes the file when a pane is killed mid-run, so the dashboard
  would show `◆ finished` for a worktree whose agent was never given the chance to finish.
  Correctness would depend on a sweep that has to know which files are orphaned.
- _Read cost._ A stat per worktree per 3-second tick, versus one exec for every pane on
  the server.
- _Write cost._ The hook only knows its cwd; deriving `<repo-slug>/<flat-name>` from it
  means re-implementing devgeta's path scheme in shell and in JavaScript, and it breaks
  the moment the agent is invoked from a subdirectory of the worktree.
- _Drift._ It is a second source of truth for something tmux already owns.

**A field in `global_config.yaml`.** That file is durable installation state, written
atomically via temp-and-rename (CLAUDE.md §7). Rewriting it on every assistant turn abuses
a store built for a different lifetime and invites write contention between concurrent
worktrees.

**A watcher process** subscribed to OpenCode's server event stream. A daemon to service a
3-second poll is disproportionate, adds a lifecycle to manage, and has no Claude equivalent.

**tmux's native bell / `monitor-activity`.** Fires on any pane output, not on turn
completion, so it cannot distinguish "the agent is done" from "the agent printed a line."

## Consequences

**Easier.** No cleanup pass and no stale state — the option is destroyed with the pane. No
new store, no new file format, no migration. Two agents in one window report
independently, which is what the review flow needs. The dashboard stays agent-neutral;
adding a third coder later is a hook that writes the same option. It also works for panes
devgeta did not create, so a future non-worktree surface gets it for free.

**Harder.** The dashboard needs a real aggregation rule rather than reading a single value,
and one dot per row still cannot say _which_ pane wants attention — a user with a review
and a coder in one window sees `◆` and must look to find out which. Surfacing per-pane
detail is a display problem for a later cycle, not a storage one; the data is there.

Reading is now its own `list-panes -a` exec rather than riding an existing call. Note this
is still a net reduction: `WorktreeManager.List()` today calls `Tmux.WindowSession()` once
per worktree, and each of those runs a **fresh** `list-windows -a` scan — N execs per
3-second refresh. The implementing cycle consolidates that into one scan that populates
both `WindowActive` and the agent state.

The state does not survive a tmux server restart — acceptable, since neither do the panes
it describes. It is invisible to a coder run outside tmux, which is correct: `dg ws` is a
tmux dashboard and has no row for such a session. `#{@option}` in format strings requires
tmux ≥ 3.0; every platform in CLAUDE.md §8 ships newer, and devgeta installs tmux itself.

**Accepted trade-off.** `@dg_agent_state` is a public, unnamespaced-by-tmux user option:
another tool could in principle write it. The `@dg_` prefix is the only guard, which is
the same convention every tmux plugin in the ecosystem relies on.

**Follow-on.** This ADR governs
`docs/plans/cycles/2026-07-28-agent-activity-notifications.md`, which implements the
signal, and is a prerequisite for
`docs/plans/cycles/2026-07-28-ws-kick-review.md`, which relies on it to report a
kicked review's completion.
