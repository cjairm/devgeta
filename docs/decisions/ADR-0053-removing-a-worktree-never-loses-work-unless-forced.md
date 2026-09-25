# ADR-0053 — Removing a worktree never loses work unless forced

**Date:** 2026-09-25
**Status:** ACCEPTED

## Context

A worktree was deleted from the `dg ws` dashboard with `d d`, and its
uncommitted changes went with it. The commits survived only because they had
been pushed. Reading the removal path showed why nothing stopped it:

- **The dashboard always forces.** `d d` and `D D` call the removal with
  `force = true`, which skips the uncommitted-changes check. When git itself
  then refuses to remove a dirty worktree, the code deletes the directory by
  hand.
- **Every removal runs `git branch -D`.** Merged or not, pushed or not. Commits
  that exist only on that branch are left dangling, found again only by
  `git fsck` until the next gc.
- **The one check fails open.** Without force, the uncommitted-changes check
  runs, but an error from `git status` lets the removal go ahead.

Two presses of the same key are not enough protection on their own. The row
under the cursor can be a different kind than the user thinks: since
ADR-0052's amendment, a session holding only a worktree window has no row, so
the cursor lands on the worktree row.

## Decision

- **A removal without force refuses when it would lose work**, and says what
  would be lost:
  - **Uncommitted changes**, including untracked files (`git status
--porcelain`, as today).
  - **Unpushed commits**: commits on the worktree's branch that no
    remote-tracking branch and no local default branch contain (`git rev-list
--count HEAD --not --remotes refs/heads/<default>`). A repo with no remote
    counts every commit not merged into its default branch, which is right:
    those commits exist nowhere else.
  - **A check that cannot be answered refuses too.** Fail closed, never open.
- **Force keeps today's behavior**: remove regardless, and delete the branch.
- **The dashboard stops forcing.** `d d` and `D D` remove safely. When a
  removal is refused, the status line says why and offers `F F`, a separate
  two-press force delete. Its armed hint names what will be lost.
- **`dg wt remove` gets the same check**: without `--force` it now also refuses
  unpushed commits, not only uncommitted changes. `dg wt remove --all` already
  runs without force and so becomes safe too.
- **Out of scope:** `dg task worktree-finish`. Its discard mode is an explicit
  request to throw the work away, and its merge mode deletes the branch only
  after merging it.

## Consequences

- Easier: an accidental `d d`, on the wrong row or not, can no longer destroy
  work. The worst case is a message.
- Harder: deleting a branch that was never pushed now takes a force, in the
  dashboard (`F F`) and on the command line (`--force`). This is a behavior
  change for `dg wt remove` without `--force`, which used to delete unpushed
  branches silently.
- One more git call per removal (`rev-list --count`), on a user action only.
