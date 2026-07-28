# ADR-0003 — A single `dg ws` dashboard for sessions and worktrees

**Date:** 2026-07-21
**Status:** ACCEPTED

## Context

Today a user reaches two tmux surfaces with two keys: bare `ctrl+t` runs tmux's native
`choose-tree -Zs` (a session switcher over every session on the server), and `prefix + u`
opens `dg wt ui` (the worktree dashboard). The worktree dashboard is scoped to git
worktrees under `~/.local/share/devgeta/worktrees/`; it has no concept of a standalone
tmux session — one not backed by any devgeta worktree (a manual `notes` session, an ssh
session). So `choose-tree` and `dg wt ui` show overlapping-but-different things, and the
user must remember which key shows which.

We want one launcher. Three facts constrain the design:

1. **tmux sessions cannot be nested.** The hierarchy is server → session → window → pane.
   Any notion of "everything under one parent" is a dashboard/view concept, not a tmux
   structural one.
2. **devgeta models a worktree as a tmux _window_, not a session.** Worktree windows are
   named `wt-<repo>-<flat-name>` and live in a session named after the repo slug
   (`tmuxSessionName`). Several worktrees of one repo are windows in one repo session.
3. **Worktrees exist independently of tmux.** `WorktreeManager.List()` walks the
   filesystem, so a worktree appears even when it has no live tmux window
   (`WindowActive == false`). A pure "list tmux sessions" model would hide those.

## Decision

Introduce a new top-level command **`dg ws`** (alias `workspace`) that opens a single
dashboard listing **workspaces** under one parent view. A workspace is exactly one of two
kinds:

- **Repo workspace (worktree-backed):** a repo that has worktrees. Sourced from the
  filesystem (`WorktreeManager.List()`), so it shows regardless of whether its repo tmux
  session is live. Expandable to its worktree rows.
- **Session workspace (plain):** a standalone tmux session with no worktree. Sourced from
  `tmux list-sessions`, excluding any session that contains a `wt-` window (that exclusion
  is the _only_ correlation done between sessions and worktrees — there is no further
  session↔worktree reconciliation). A leaf, no children.

The two kinds are differentiated inline in one flat top-level list — **not** in two labeled
sections — by two orthogonal signals:

- **Kind:** expandability. A repo workspace has a `▼/▶` chevron and an `N trees` badge; a
  plain session is a leaf.
- **Activity:** the existing status dot (`●` attached/live, `○` not), applied to both kinds.

`dg wt ui` is **deprecated** (cobra `Deprecated` notice) and forwards to the same TUI; the
rest of `dg wt` (create/list/rm/repair/prune) stays. The bare `ctrl+t` `choose-tree` popup
is retired and rebound to open `dg ws`.

Rejected alternatives:

- **Two labeled sections ("Worktrees" / "Sessions").** Reads as two buckets, not one
  parent; the user asked for a single unified list.
- **Session↔worktree reconciliation** (mapping each session back to a worktree). More
  machinery than needed; the `wt-`-window exclusion filter is sufficient to avoid
  double-listing a repo session.
- **A pure "list tmux sessions" model.** Would hide worktrees whose window isn't live —
  the exact "worktree without a session" case the dashboard must surface.
- **Keeping `dg wt ui` as the name.** "Worktree UI" no longer describes a surface that
  also manages plain sessions; "workspace" covers both a plain session and a
  repo-with-worktrees.

## Consequences

- Easier: one key, one command for both worktrees and sessions; the worktree-vs-session
  distinction is visible at a glance without two screens; no tmux restructuring (the
  per-repo-session model is unchanged); reuses the existing status-dot and tree renderer.
- Harder: the row model gains a workspace/kind dimension (repo vs plain session); we add a
  `tmux list-sessions` primitive and a standalone-session filter; `dg ws` and the
  deprecation shim are new surface area.
- Accepted trade-off: we lose `choose-tree`'s pane-level navigation for non-worktree
  sessions. Standalone sessions rarely need pane-jumping; `choose-tree` can be rebound to a
  spare chord later if it's missed.
- Follow-on: this ADR governs the cycle
  `docs/plans/cycles/2026-07-21-wt-ui-sessions.md`.
