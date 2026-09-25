# ADR-0052 — A repo header is a label, and its sessions are rows

**Date:** 2026-09-25
**Status:** ACCEPTED

Supersedes [ADR-0048](ADR-0048-a-repo-header-reaches-its-sessions-plain-window.md).
The rest of [ADR-0003](ADR-0003-sessions-in-workspace-dashboard.md) stands: one
list, repo workspaces and standalone sessions, and no repo session listed among
the standalone sessions.

## Context

ADR-0048 gave the repo header an action. It becomes a cursor stop when its
session has a plain window, and enter on it switches there. In use, that was the
wrong shape:

- **A repo's windows can live in more than one session** (`hire2` and
  `hire2-tien` is a real case). The header can lead to only one of them, and
  which one depends on the order the scan found the panes in.
- **The session's name is never shown.** You select a repo name and land in a
  session you cannot see or name from the dashboard.
- **There is no way to rename it** from the dashboard.
- **The header's behavior changes with hidden state.** Whether a header is a
  stop depends on whether a plain window happens to exist, so j/k behaves
  differently from one repo to the next.

ADR-0048 rejected "rows for the plain windows" as more rows than needed. That
rejection was about one row per plain _window_. This decision adds one row per
_session_, a smaller set, and one the header could never represent.

## Decision

- **The repo header is a label.** When expanded, it is never a cursor stop. When
  collapsed, it is a stop only so `l` (or enter) can expand it. Enter on a
  header never switches sessions.
- **Each live session holding this repo's worktree windows gets a row under the
  repo**, before the worktree rows, showing the session's name — **but only when
  the session also holds a plain window** (amended 2026-09-25, below).
  - Sessions are read from the repo's worktree panes, from the same scan
    (ADR-0024). No new tmux calls, and names are never derived.
  - Enter switches to that session's first plain window. It never switches to
    the bare session,
    which would land on the dashboard's own window (ADR-0048, constraint 1,
    still holds).
  - **Glyph:** the row is drawn like a standalone session row, `■`/`□` for
    attached/detached. Once any of its plain-window panes has reported agent
    state, it shows the agent-state glyph aggregated over those panes only.
    The repo's worktree panes are left out because their own rows already
    show them.
  - **Children:** pane rows under the same ADR-0008 rule, counting 2+ stateful
    plain-window panes. Its key is `sess:<name>`, the same as a standalone
    session, since a session name is unique on the server.
  - **Startup cursor:** the dashboard opens on this row when it is the session
    you are in. That replaces `placeCursorOnActive`'s derived-name match, which
    missed any session not named after its repo. When the session you are in
    has no row, it opens on the first worktree row whose pane reports that
    session.
- **`$` renames a session**, on repo-session and standalone-session rows alike.
  `$` is tmux's own key for rename-session. The new name is flattened the way
  `TmuxSessionName` does it. It is checked against the live sessions first, but
  only for a clearer message: tmux rejects a duplicate itself. The row's fold
  key and cursor key move from `sess:<old>` to `sess:<new>`.
- **New worktree windows follow the repo's existing session.**
  `createWindowWithLayout` places a new window in the session that already holds
  the repo's windows. `TmuxSessionName(repo)` is used only when there is none.
  Without this, renaming a repo's session would make the next `n` start a
  second session with the old name. The manager keeps no scan of its own, so a
  create runs one `tmux list-panes -a` to find the session. That is acceptable
  on a mutation. Going back to deriving the name to save that call would bring
  the bug back.

## Consequences

- Easier: every session a repo lives in is visible, reachable and renameable.
  j/k stops are predictable: headers only when collapsed.
- Harder: one more row per repo that has a live session. This is offset by the
  header no longer being a stop.
- Structural: `repoPlainWindow`, `repoHeaderLeadsSomewhere` and
  `handleSwitchToRepoSession` are removed. The session lookup moves into the
  worktree manager, where the dashboard and window creation both use it.
- Not solved here: `wt-<repo>-<name>` window names can collide across repos,
  and two repos with the same folder name share a header. Both need repo
  identity by path across the worktree tooling, which is a separate decision.

## Amendment (2026-09-25): worktree-only sessions get no row

In use, a session holding nothing but one worktree window (the common case for
a repo you opened once and never added a window to) drew a session row and a
worktree row for the same window. Enter on either landed in the same place, so
the list read as a duplicate.

- **A repo-session row now requires at least one plain window** in the session.
  Windows count, not panes: a worktree window split into several panes is still
  one worktree window. The dashboard's own `[workspace]` window is excluded,
  the same way `PlainWindowBySession` already excludes it, so opening the
  dashboard inside such a session does not make its row appear.
- **Enter's fallback to the repo's worktree window is removed**: a row now
  implies a plain window to switch to.
- **Cost:** a worktree-only session's name is not shown, and `$` rename is not
  reachable from the dashboard for it until it gains a plain window. Accepted:
  the name is visible in tmux itself, and the moment a second real window
  exists the row comes back.
