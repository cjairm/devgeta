# ADR-0027 — A merge refuses on any dirty checkout and any non-stale open finding

**Date:** 2026-08-18
**Status:** ACCEPTED

## Context

`devgeta task worktree-finish --merge` (`internal/tooling/task/worktree.go`) lands a
branch by rebasing it (if diverged) and then fast-forward-merging it into the default
branch from the main checkout. A later `--check` flag is meant to answer "is this ready
to merge?" without acting — which only means something if `--check` and `--merge` agree
on exactly what blocks a merge. This record answers that one question — **what may block
a `--merge`, and how does `--check` mirror it** — for two forks that came out of the same
planning cycle (`docs/plans/cycles/2026-08-17-finish-work-command.md`, §8 Q3 and Q5).
They share one record rather than two because they answer the same question, share the
same rejected "advisory only" shape, and share the same no-`--force` consequence — split
across two records, a later change to either would have to remember the other exists to
keep `--check`'s notion of "ready" equal to `--merge`'s actual refusal set.

### Fork 1 (§8 Q5): the dirty main checkout

`worktreeFinishMerge` touches two checkouts: it rebases the branch at `wtPath`, and it
runs `git merge --ff-only` in the main checkout. Before this decision, only the worktree's
own dirtiness was ever checked — the main checkout's branch was examined, but nothing
looked at its working tree. That gap produces two bad outcomes:

- the dirty paths in the main checkout overlap what the fast-forward would write →
  `git merge --ff-only` refuses, so a worktree `--check` had already called "ready" fails
  at the very last step — the check/merge disagreement this whole cycle exists to close;
- they don't overlap → the merge lands, the default branch moves, and unrelated
  uncommitted work in the main checkout is left sitting on top of commits that just landed,
  with nothing ever having flagged it.

Three shapes were live: refuse on **any** dirty main checkout; refuse only when the dirty
paths overlap what the fast-forward would touch; report it advisory-only and never refuse.

### Fork 2 (§8 Q3): the journal gate

The branch's review journal (ADR-0012) can hold findings still open when a merge is
attempted. `worktree-finish` never asked it anything, and the journal is deleted with the
branch (`dropReviewJournal`) once the merge lands — so a finding still open at merge time
was never actually settled, it just disappears with the branch. `Manager.Verdict`
(`internal/tooling/reviewjournal/manager.go`) already computes each entry's freshness —
`FreshnessFresh`, `FreshnessStale`, or `FreshnessDateless` (a pathless, design-level
question) — by hashing the cited file's current content against the blob the entry was
stamped with. Whether staleness should excuse an otherwise-blocking finding was the open
question. Three shapes were live: block on **every** open finding regardless of
freshness; block only on **non-stale** open findings, reporting stale ones advisory-only;
warn only and never block.

## Decision

### The dirty main checkout: any dirtiness blocks, not only overlapping paths

`worktreeFinishMerge` refuses whenever `Git.IsWorktreeDirty(mainWorktree)` is true — any
uncommitted change in the main checkout, not only the paths a fast-forward would
overwrite. Any-dirty won over the other two for three reasons:

1. **Reuse, not a new mechanism.** `Git.IsWorktreeDirty` already exists and already backs
   the `wtPath` refusal (CLAUDE.md §6, "prefer existing over new"); this is the same probe
   pointed at a second directory, not a second implementation.
2. **The cost is asymmetric.** Over-refusing costs the user one commit or stash in the
   main checkout. The narrower rule — refuse only on overlapping paths — requires
   predicting which paths `git merge --ff-only` will actually write and intersecting them
   with the dirty set: fragile logic built to guard a rare case.
3. **Advisory-only was rejected outright.** It knowingly leaves `--check` reporting
   `ready: yes` for a merge that `git merge --ff-only` will actually refuse (or, in the
   non-overlapping case, silently bury unrelated work) — precisely the check/merge
   disagreement this cycle exists to close.

### The journal gate: non-stale open findings block; stale ones don't

`worktreeFinishMerge` refuses whenever the branch's journal holds an open finding whose
`Manager.Verdict` against the worktree's current content is **not** `FreshnessStale` — i.e.
`FreshnessFresh` or `FreshnessDateless` both block. A `FreshnessStale` open finding is
reported (in `--check`'s later output) as advisory only and does not block.

- **Blocking on every open finding** (including stale ones) was rejected: a stale entry's
  cited file has already changed since the finding was raised, so blocking on it would stop
  a merge over code that no longer exists — exactly the failure mode this cycle's own risk
  table names.
- **Warn-only** was rejected for the same reason as the main checkout's advisory-only
  alternative: it lets `ready: yes` and an actual unresolved finding landing silently
  disagree, which is the one disagreement this whole ADR exists to prevent.
- **`FreshnessDateless` blocks.** A pathless, design-level question has nothing mechanical
  that can invalidate it, and an unanswered design question is exactly what must not be
  papered over — the same principle ADR-0017 ("escalate, don't paper over") already
  applies to the review loop.
- The exit is `devgeta task review-note --settle <id>`. Settling as `rejected` with a
  reason is a legitimate way through the gate: the gate demands an answer, not agreement.

Both decisions read ADR-0012 (the journal's own acceptance gate) and ADR-0017 (escalate,
don't paper over) the same way: the journal exists so an open question is never silently
lost, and a merge that deletes the journal while an answerable question is still open
would be exactly that loss.

**Implementation note, worth recording because it is easy to get backwards:** the
journal's _location_ resolves through the main checkout (`reviewjournal.Manager.PathFor`'s
common-git-directory resolution — consistent with `dropReviewJournal`, which is forced to
use the main checkout because by the time it runs, the worktree is already gone). But each
entry's _freshness_ must be judged against the **worktree's** on-disk content, not the main
checkout's: `Manager.Verdict(repoDir, e)` hashes the cited file as it currently sits in
`repoDir`, and the branch's own fix (or breakage) lives in the worktree, not on the
unrelated default-branch copy the main checkout holds. Loading the journal from the main
checkout while judging freshness from the main checkout as well would make a finding
citing a file the branch just fixed read almost universally `stale`, silently letting a
real, unresolved finding through.

## Consequences

- **No `--force` escape on either refusal**, the same reasoning as the existing
  dirty-worktree refusal: there is no legitimate reason to force a merge through a
  stranded dirty checkout or an unsettled finding — only a real fix (commit or stash the
  main checkout; settle the finding) gets past either gate.
- **`--check` must report both as a blocking `ready: no`, never an advisory line**, so the
  two paths can never disagree about whether a merge would actually succeed. Every other
  mechanically-decidable fact `--check` reports and blocks on is added to this same set;
  anything reported only advisory (a stale finding, a predicted merge conflict) must stay
  excluded from `ready:` for the identical reason the advisory-only alternatives above were
  rejected.
- **One implementation serves both paths.** `openBlockingFindings(mainWorktree, wtPath,
branch)` is the single probe both `--merge` and the future `--check` call — there is no
  second copy of this logic that could drift out of sync with the first.
- **A cost accepted knowingly.** A main checkout with unrelated uncommitted work that would
  never have collided with the fast-forward still has to be cleaned up before a merge, and
  an occasional stale-looking finding needs a second look before its true freshness is
  obvious. Both are the cheaper failure mode against silently landing broken or
  half-finished state.
