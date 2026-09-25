# ADR-0050 — Dashboard view state lives in a tmux server option

**Date:** 2026-09-25
**Status:** ACCEPTED

## Context

`dg ws` forgets its folds and left-pane width every time it opens. The
dashboard is opened often (`ctrl+t`) and closed as soon as you switch, so it
starts from scratch every time.

The state describes tmux things: repo sessions, worktree windows, and panes.
devgeta already stores per-pane and per-window state in tmux user options
(`@dg_agent_state`, `@dg_window_agent_state`) rather than in files.

## Decision

Store the view state as versioned JSON in one tmux **server-global** user option,
`@dg_ws_state`:

```json
{
  "v": 1,
  "collapsed": ["repo:hire2", "wt:/path/to/tree", "sess:misc"],
  "left": 40
}
```

- **Keys come from the dashboard's single `rowKey` function,** so the saved
  state and the in-memory fold map cannot disagree about identity.
- **Read once at startup**, with `show-options -gqv`. Without `-q`, an unset
  option exits 1 with `invalid option`, and that happens on every first launch
  after a tmux restart. Empty output means unset, not an error.
- **Written only when the saved state changes:**
  - when a fold changes (`h`, `l`, `z`, or a rename moving a fold key),
  - when `e` is pressed,
  - when a drag of the divider ends (`MouseReleaseMsg`), not on every
    `MouseMotionMsg` during it.
- **Keys for rows that no longer exist are dropped on write.**
- **A missing, corrupt, or unknown-version value is ignored,** and the dashboard
  starts from defaults.
- **Writes are best-effort.** tmux rejects an option value of about 20 KB or
  more (`command too long`) and keeps the old one. A failed write is logged at
  debug level and never blocks or undoes the fold or resize that triggered it.
  Pruning keeps the value far below that limit in practice.
- All reads and writes go through the tmux wrapper.

**The cursor is not saved.** The dashboard opens on the row for the session you
are in (`placeCursorOnActive`). That is the more useful default for a dashboard
opened with `ctrl+t` from inside a session. A saved cursor would compete with it
at startup and one of the two would silently lose. Leaving the cursor out also
means nothing needs writing at quit, so the dashboard's many quit paths
(key handlers, and command results that return `tea.QuitMsg`) need no save hook.

**Rejected: a file under `$XDG_STATE_HOME/devgeta/`.** It would survive
reboots, but it adds an atomic write and a second store next to the tmux one,
and stale entries for repos that are long gone would need pruning. The state
describes sessions that die with the tmux server anyway, so outliving them
buys little.

**Rejected: writing on every mouse-motion event or keypress.** That would be
one tmux call per event. The saved state only has to be right once the change
is finished.

## Consequences

- Easier: no file, no migration, and cleanup comes free with tmux's lifetime.
  Every dashboard opened on the same tmux server shares one view.
- Harder: the view resets after a reboot or `tmux kill-server`. Outside tmux
  (where `dg ws` already refuses to switch) nothing is saved.
- Accepted: two dashboards open at once share the option, and the last write
  wins.
