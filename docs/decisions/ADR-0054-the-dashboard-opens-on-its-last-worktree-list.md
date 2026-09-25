# ADR-0054 — The dashboard opens on its last worktree list

**Date:** 2026-09-25
**Status:** ACCEPTED

## Context

`dg ws` is opened often (`ctrl+t`) and closed as soon as you switch. Every
open starts empty and shows `(loading...)` until the slow load finishes: one
`git worktree list` per known repo, then a diffstat sweep (ADR-0051). The rows
it ends up showing are almost always the same rows as last time, so the wait
buys nothing most of the time.

The dashboard already keeps itself fresh after it opens: tmux every 3 seconds,
git every 30 seconds (ADR-0024). What is missing is something to draw before
the first git load lands.

## Decision

Save the last successful worktree list to a file and draw it on open, while
the normal loads run as usual and replace it.

- **Where:** `$XDG_CACHE_HOME/devgeta/ws-snapshot.json`
  (`paths.GetCacheDir`). It is a cache in the XDG sense: deleting it only
  costs one slow first frame.
- **What:** only the git-derived fields of each worktree (repo, name, path,
  branch) plus the last diffstat per path. Nothing tmux-derived is saved: the
  tmux scan takes milliseconds and would be stale on arrival anyway.
- **Shape:** versioned JSON (`{"v":1,...}`). A missing, corrupt, or
  unknown-version file is ignored, the same rule as `@dg_ws_state`.
- **Written** after every slow load that is still current (its generation
  matches) and after every current diffstat sweep, with
  `files.WriteFileAtomic`, off the Update goroutine. A failed write is logged
  at debug level and never shown.
- **Read** once, synchronously, when the model is built, so the first frame
  already has the rows. Reading it in a command instead would race the first
  tmux scan: a snapshot landing after the scan would wipe the pane layer the
  scan just applied.
- **The cursor opens on your row.** When a snapshot was read, one tmux scan
  runs synchronously right after it (two tmux calls), and the session rows and
  cursor placement are worked out against the snapshot's worktrees. The
  cursor starts on the row for the session you are in, the same rule as
  before, instead of starting at the top and jumping there once the loads
  land. Nothing about the cursor is saved (ADR-0050 still holds). With no
  snapshot, the scan is skipped and startup works as it did before.
- **A snapshot is a guess, not a load.** It sets its own `seeded` flag, never
  `m.loaded`, which still means "git has answered":
  - the "no worktrees yet" guidance waits for git;
  - placing the cursor on snapshot rows is final only when it finds your row.
    A miss may just mean the snapshot is behind git, so the real load tries
    again, the same way it does today;
  - the first real load re-classifies sessions against the real list and
    replaces the snapshot through the normal `statusesMsg` path.

**Rejected: a tmux server option,** like ADR-0050's view state. tmux rejects a
value of about 20 KB, and a worktree list with paths and branches for many
repos can reach that. The view state is small and describes tmux things; this
is a git listing that is just as valid after a tmux restart.

**Rejected: skipping the git load when a snapshot exists.** Worktrees are
created and removed outside the dashboard (`dg worktree`, plain `git`), so the
snapshot can be wrong, and only git can say so.

## Consequences

- Easier: the dashboard opens with rows on screen instead of `(loading...)`,
  and with the cursor already on your row.
- Harder: a worktree removed outside the dashboard shows for up to one slow
  load after opening, then disappears. One created outside it appears at the
  same point, as it does today.
- Harder: startup does two extra tmux calls before the first frame when a
  snapshot exists.
- Accepted: two dashboards open at once share the file, and the last write
  wins. Both write only fresh git results, so either is correct.
