# ADR-0048 — A repo header reaches its session's plain window

**Date:** 2026-09-22
**Status:** ACCEPTED

Supersedes the "no further session↔worktree reconciliation" clause of
[ADR-0003](ADR-0003-sessions-in-workspace-dashboard.md). The rest of ADR-0003 —
one flat list, two workspace kinds, the `wt-`-window exclusion from the session
rows — stands unchanged.

## Context

ADR-0003 built `dg ws` as one flat list of two workspace kinds, and sourced the
plain-session rows from `tmux list-sessions` **excluding any session that
contains a `wt-` window**. It said that exclusion was "the _only_ correlation
done between sessions and worktrees", and listed "session↔worktree
reconciliation" under rejected alternatives as more machinery than needed.

That exclusion is right about double-listing and wrong about coverage. A repo
session holds a `wt-` window per worktree **and** whatever plain windows the
user has there — typically the `zsh` the session was started from. Excluding the
session wholesale removes the only row those plain windows could have appeared
under, so the dashboard could neither show nor reach them. Live example:

```
lever-meta :: wt-lever-meta-docs-cycle08-outcome
lever-meta :: zsh                                  ← no row, unreachable
```

The repo header row was also inert: `enter` on it fell through to
`handleAttach`, which has no worktree selected on a header row and returned
silently. So the dashboard had a visible row that did nothing, next to a window
it could not reach.

Four constraints shaped the answer, each of which was a shipped bug first:

1. **A session is not a window.** `switch-client -t <session>` lands on that
   session's _active_ window. `ctrl+t` opens the dashboard as a `[workspace]`
   window **inside** the current session, so at the moment of the switch the
   active window is the dashboard itself; when the dashboard then exits, its
   window dies and tmux drops the client onto whatever remains — usually the
   `wt-` window the plain window was supposed to be an alternative to. Reaching
   a window means naming that window.
2. **The dashboard's own window is not a destination.** That same `[workspace]`
   window makes any session look like it has a plain window, including a repo
   session that holds nothing but `wt-` windows.
3. **A session's name is not derivable.** `worktree.TmuxSessionName(repoSlug)`
   is only where `ensureWindow` _puts_ a new window. Windows end up elsewhere: a
   `wt-hire2-…` window living in a session named `hire2-tien` is a real case, so
   deriving the name switches to a session that does not exist while the right
   one is on screen.
4. **A stop the user cannot use costs them every trip down the list.** Making
   every header navigable added a keypress per repo, and most of those headers
   led nowhere.

## Decision

Do the narrow reconciliation ADR-0003 rejected, entirely within one tmux scan,
and give the header row an action instead of giving the plain windows rows of
their own.

**Resolution, in two reads of the scan the dashboard already takes:**

- `StateLayer.PlainWindowBySession(ignorePaneID)` maps each session to its first
  window that is **not** worktree-backed, skipping the window holding
  `ignorePaneID` (`$TMUX_PANE` — the dashboard's own, pinned to that pane's
  session because window names are not unique across sessions). This is exactly
  the fact `SessionStatuses` discards when it drops a `wt-`-containing session.
- A repo's session is **read** off its worktree rows' panes, which carry it from
  the same scan. The derived name is a fallback only, and only where there is no
  live window to read.

**Behavior:**

- A repo header is a `j`/`k` stop when it is collapsed (so `l` can re-expand it,
  unchanged) **or** when the resolution above yields a window. One predicate,
  `repoPlainWindow`, both decides that and supplies `enter`'s target, so
  "selectable" and "has somewhere to go" cannot disagree.
- `enter` switches to `session:window` and quits. The session-only switch
  survives for one case: a collapsed header whose repo has no live window.
- Neither reduction costs a tmux call at keypress time. `navigableIndices` runs
  for every row on every keypress and render, so it reads only the last scan's
  results.

**Rejected alternatives:**

- **Rows for the plain windows** (nested under the repo, or as window rows).
  Rejected by the maintainer on sight: the dashboard does not need more rows, it
  needs the row it already has to work. It would also re-open the
  ADR-0003 question of what a "workspace" is.
- **Dropping the `wt-` exclusion** so repo sessions appear among the session
  rows. Restores exactly the double-listing ADR-0003 introduced the exclusion to
  prevent, and puts one session on screen twice under two different names.
- **Every header navigable.** Shipped, then withdrawn: a keypress per repo on
  every trip down the list, most of them landing on headers that led nowhere.
- **Deriving the session name** from the repo slug. Constraint 3.
- **Switching to the session, not the window.** Shipped, then withdrawn:
  constraint 1. This is the one that reads as "the header sends me to the
  worktree", because tmux, not devgeta, picks the window.
- **A second tmux call to resolve the session per keypress.** `navigableIndices`
  runs per row per render; the scan already has the answer.

## Consequences

- Easier: the plain windows in a repo session are reachable, from the row that
  already stood for that session. A header that is a stop always leads
  somewhere. No new row kind, no new tmux calls, no second source of truth —
  both reductions come off ADR-0024's existing scan.
- Harder: `dg ws` now correlates sessions to repos, which ADR-0003 deliberately
  avoided. That correlation is only as good as the scan (a repo with no live
  window resolves to nothing, and its expanded header is not a stop), and it
  needs the dashboard to know its own pane — a dependency on `$TMUX_PANE`, the
  signal `configs/claude/agent-state.sh` already relies on.
- Accepted trade-off: only the **first** plain window is reachable. A session
  with several gets one target; the rest are a `prefix + n` away once you are
  there. Ranking them would need a policy nothing yet asks for.
- Structural: the cursor's valid positions are now defined once
  (`navigableIndices`, used by both movement and post-rebuild clamping). The
  second definition that existed alongside it, `leafIndices`, disagreed about
  headers — and since a rebuild runs on every 3-second tick, it pulled the
  cursor off a just-selected header on its own. It is deleted rather than
  corrected; two answers to "where may the cursor sit" is the defect.
