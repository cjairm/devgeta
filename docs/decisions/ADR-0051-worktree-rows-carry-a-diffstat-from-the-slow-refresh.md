# ADR-0051 — Worktree rows carry a diffstat from the slow refresh

**Date:** 2026-09-25
**Status:** ACCEPTED

Extends [ADR-0024](ADR-0024-the-dashboard-refreshes-fast-and-slow-state-separately.md).

## Context

The chosen `dg ws` layout shows a dim `+A −R` at the end of every worktree row,
so you can see which trees have changes without opening each one. Today the
numbers exist only for the selected row, as a by-product of its full branch
diff (`BranchDiffAt`, 5 git calls).

ADR-0024 moved all git work to a 30-second slow refresh, taking idle cost from a
~17% duty cycle down to ~1.7%. Any per-row number has to respect that split.

## Decision

- Add `task.BranchStatsAt(g, dir)`, which returns counts only (files, added,
  removed). It shares base resolution and `changedFiles` (`git diff --numstat`)
  with `collectWorktreeDiff`, so the row numbers and the right pane's header are
  computed the same way and cannot disagree. It skips the diff body and the
  color pass.
- Cost per worktree: `merge-base`, `diff --numstat`, and `ls-files --others`
  for the untracked count, which makes 3 git calls. The default branch
  (`DefaultBranchIn`, 1 call, or more when `origin/HEAD` is unset) belongs to
  the repo, not the worktree. It is resolved once per repo per slow refresh and
  passed in. It is not re-resolved per worktree, which would make 4 calls.
- Run it for every worktree on the **slow** refresh only, with bounded
  parallelism, into a path → stats map on the model. Never on the fast tick.
- Every `diffMsg` also updates its own row's entry, so the selected row stays
  current between slow refreshes for free.
- No changes → nothing drawn, not `+0 −0`.

**Rejected: stats for the selected row only.** That is what exists today, and it
defeats the point of scanning the list.

**Rejected: `git diff --shortstat` alone.** It would not count untracked files,
so the row would disagree with the right pane, which does count them.

## Consequences

- Easier: the list shows where the work is at a glance.
- Harder: slow-refresh cost grows with the number of worktrees: 3 git calls per
  tree plus 1 per repo, every 30 seconds. With 7 trees that is roughly another 0.3s per 30s:
  still a low single-digit duty cycle. Measure this during implementation and
  record the result here.
- Accepted: a row's numbers can be up to 30 seconds stale unless it is the
  selected row. This is the same trade ADR-0024 made for the diff.
