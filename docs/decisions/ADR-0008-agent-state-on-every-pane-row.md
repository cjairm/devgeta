# ADR-0008 — Agent state belongs to every row in `dg ws`, not just worktree rows

**Date:** 2026-07-29
**Status:** PROPOSED

## Context

[ADR-0005](ADR-0005-agent-activity-state-in-tmux-pane-options.md) put agent activity in a
tmux **pane** option, `@dg_agent_state`, and its implementing cycle
([2026-07-28-agent-activity-notifications.md](../plans/cycles/2026-07-28-agent-activity-notifications.md))
wired it to the dashboard. That cycle locked two things out of scope:

> **Standalone session rows.** Only worktree rows get an agent state; a plain tmux session
> has no devgeta-managed coder. `SessionDot` is untouched.

> **Showing _which_ pane wants attention.** State is collected per pane, but a row renders
> one aggregated dot. Per-pane display is a later cycle; the data will be there for it.

Real usage disproves the first one. A live `tmux list-panes -a` on the author's machine,
taken while two Claude Code sessions were running:

```
devgeta-goku           | 2.1.220                        | %15  | @dg_agent_state=blocked
devgeta-goku           | 2.1.220                        | %126 | @dg_agent_state=blocked
flux-goku              | 2.1.220                        | %1   | @dg_agent_state=idle
flux-goku              | 2.1.220                        | %2   | (empty — a zsh pane)
misc                   | zsh                            | %5   | (empty)
pillar-cloud-functions | wt-pillar-cloud-functions-fix… | %131 | (empty — opencode, no turn yet)
```

Both hooks worked perfectly. One agent was **blocked on a permission prompt** and another
was **finished and waiting**, and `dg ws` showed neither. Three independent reasons:

1. **`WorktreeManager.List()` reads pane states, then discards everything that isn't a
   worktree window.** `worktree.go:477` indexes the scan by window name; `worktree.go:519`
   only ever looks up `wt-<repo>-<flat-name>`. Panes in any other window are read off the
   server and dropped.
2. **A session row cannot carry state.** `SessionStatus` is `{Name, Attached}`
   (`worktree.go:88`) and `rowSession` renders `SessionDot(attached)` — ■/□
   (`model.go:1321`). There is no path from `@dg_agent_state` to a session row at all, so
   the dot is structurally incapable of reporting one.
3. **A collapsed repo header shows no dot at all.** `rowRepo` renders chevron + name +
   "N trees" badge (`model.go:1273`). Collapsing a repo hides every child's state.

The out-of-scope note's premise — "a plain tmux session has no devgeta-managed coder" — is
simply not how the tool is used. An agent launched by hand in a session the user made
themselves is the common case, not the exception. ADR-0005 already anticipated this in its
Consequences ("it also works for panes devgeta did not create, so a future non-worktree
surface gets it for free"); the dashboard is the surface that never collected on it.

**Do not detect agents by process name.** The evidence above kills that option outright:
Claude Code's `pane_current_command` is `2.1.220` — tmux's automatic-rename picked up the
versioned binary directory, which is also why the window is named `2.1.220`. Any allowlist
of binary names (`claude`, `opencode`, …) is already broken on the shipped installer and
would break again on the next release. Worktree windows are unaffected because devgeta
creates them with `new-window -n <name>`, which turns automatic-rename off for that window.

## Decision

**Presence of `@dg_agent_state` on a pane is the only agent signal, and every row kind
aggregates the panes beneath it.** Three changes to the row model, no change to storage.

### 1. Every row kind reports agent state

| Row kind      | Aggregates over                          | Today                  |
| ------------- | ---------------------------------------- | ---------------------- |
| `rowRepo`     | every pane of every worktree in the repo | nothing                |
| `rowWorktree` | every pane of its `wt-…` window          | already correct        |
| `rowSession`  | every pane in that tmux session          | attached/detached only |
| `rowPane`     | itself (new — see 3)                     | does not exist         |

All four use the **same** precedence function ADR-0005 defined —
`blocked > error > idle > busy > (no agent)` — exported from `aggregateAgentState` rather
than reimplemented per row kind. One rule, one place.

### 2. Session rows adopt the shared state vocabulary

A session row keeps ■/□ while no agent has reported, and switches to the state glyph
(`◆` idle, `!` blocked, `✕` error) when one has. Color alone cannot carry this: `Blocked`
and `Error` are both palette color `1`, distinguished only by bold (`styles.go:72-73`), so
a color-only session dot would make a red-hot permission prompt look like an error.

This knowingly relaxes [ADR-0003](ADR-0003-sessions-in-workspace-dashboard.md)'s rule that
shape distinguishes row kind (square = session, circle = worktree). Accepted because the
row already carries a literal `session` label in its right column (`model.go:1301`), so
kind is never ambiguous, and because one state vocabulary learned once beats two vocabularies
that mean the same thing. Shape still distinguishes the quiet case, which is the common one.

### 3. A pane row, revealed by expansion

`rowPane` becomes a child row under a `rowSession` or `rowWorktree` parent, listing
`pane_index`, `pane_current_command`, and that pane's own dot. Parents holding **two or
more** panes with a non-empty state get a chevron and expand like a repo header does;
parents below that threshold stay leaves, because a single pane's state is already exactly
what the parent's dot says and a chevron there would be noise.

This is the "which pane wants attention" case ADR-0005 deferred, and it is the same
hierarchy [herdr](https://herdr.dev/) puts in its sidebar (workspace → tab → pane, each
with its own agent state). Ours is session/repo → window → pane. The data has been in
`PaneStates()` since ADR-0005; only the renderer is new.

### 4. `ListSessions` drops `SessionWindows()` in favour of the scan it needs anyway

`PaneStates()` already returns `(session, window, pane, state)` — a strict superset of
`SessionWindows()`'s `(session, window)`. `ListSessions` needs pane states now for its
aggregate, so it derives the "exclude sessions that host a `wt-` window" filter from the
same scan instead of running a second one. Server exec count per 3-second refresh is
unchanged; one primitive stops being called by this path.

### Rejected alternatives

**Detect agent panes by `pane_current_command`.** Broken on arrival — see the `2.1.220`
evidence above. Kept out even as a supplementary signal, because a pane whose agent has
never taken a turn (the `opencode` row above) is genuinely stateless, and inventing a
"probably an agent" tier would make the dot mean two different things.

**Stop keying worktree rows by window name.** Tempting after diagnosing a name-related bug,
but the names devgeta creates are stable (explicit `-n` disables automatic-rename) and the
worktree row's job is to exist even when no window does — it is sourced from the filesystem
(ADR-0003, constraint 3). Name keying is correct here; the bug was discarding the
_non-matching_ panes, not matching the wrong ones.

**A cross-session `status-right` aggregate.** tmux's `window-status-format` only paints
windows in the attached session, so a blocked agent in another session is invisible on the
status bar too. Aggregating across the server from the status bar means either
`#(dg ws --status)` polled every `status-interval` or a global counter maintained by the
hooks — new machinery whose whole job is to tell you something the audible signal in
[ADR-0009](ADR-0009-audible-agent-notifications.md) tells you better. Dropped, not deferred.

**One toast per transition** (`notification.go`'s unused `ToastNeedsReview`). Still unused.
Toasts announce edges; the dashboard's job here is to show current truth at a glance.

## Consequences

**Easier.** The signal stops being silently lost for the way people actually run agents.
One precedence rule and one state vocabulary serve every row kind. Collapsing a repo no
longer hides its children's urgency. A user with a reviewer and a coder in one window can
expand and see which one finished — the question ADR-0005 explicitly could not answer.

**Harder.** The row model gains a fourth kind and a second level of expansion, which
touches cursor movement, `leafIndices`, filtering, and the collapsed-state map (keyed by
repo today; it needs a parent key that can also be a session name). Session rows gain a
render branch that was previously a two-glyph switch.

**Accepted trade-off.** A pane whose agent has not yet taken a turn reports nothing, so a
freshly launched agent looks like an empty shell pane until its first prompt. Fixing that
needs a launch-time write from the layout code, which is a separate change and arguably
belongs to whatever launches the agent, not to the dashboard.

**Accepted trade-off.** Expanding to pane rows exposes tmux structure (pane indexes,
process names) in a dashboard that has so far spoken only in repos and worktrees. That is
the price of answering "which one," and it is opt-in behind a chevron.

**Follow-on.** This ADR governs
[2026-07-29-ws-agent-panes-and-sound.md](../plans/cycles/2026-07-29-ws-agent-panes-and-sound.md),
Part A. It relaxes ADR-0003's shape rule (see 2) and collects on ADR-0005's unused
"works for panes devgeta did not create" property. Storage is untouched: no ADR-0005
revision is needed.
