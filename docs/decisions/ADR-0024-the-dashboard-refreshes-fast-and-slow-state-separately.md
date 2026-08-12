# ADR-0024 — The dashboard refreshes fast and slow state separately

**Date:** 2026-08-12
**Status:** PROPOSED

## Context

`dg ws` refreshes everything on one 3-second tick. `Model.tickCmd` dispatches
`loadCmd` and `sessionsLoadCmd`, and the resulting `statusesMsg` runs
`applyStatuses`, which unconditionally recomputes the selected worktree's diff.
So one tick costs, measured on this maintainer's machine (16 known repos,
warm cache, macOS):

| Work per 3-second tick                                                 | Cost   |
| ---------------------------------------------------------------------- | ------ |
| `git worktree list --porcelain` × 17 anchors (`knownRepoAnchorGroups`) | 0.35s  |
| Selected worktree's branch diff — 5 git execs, two full-tree diffs     | ~0.16s |
| `tmux list-panes -a` × 2, `tmux list-sessions` × 1, one YAML read      | small  |

That is roughly **0.5s of process-spawning work every 3 seconds — a ~17% duty
cycle — with the dashboard merely open and nobody touching it.** On top of it,
every `j`/`k` dispatches another full diff immediately with no debounce, so
holding `j` through fifteen rows queues fifteen concurrent diffs that nothing
cancels.

The two costs have completely different natures, and the single tick hides that:

- **Agent state is fast-moving.** A pane goes busy → idle → blocked within
  seconds, and showing that promptly is the dashboard's whole point (ADR-0005,
  ADR-0008). It comes from one `tmux list-panes -a` — cheap.
- **Worktree existence, branch, and diff are slow-moving.** A worktree appears
  when someone creates one; a branch changes on a checkout; a diff changes when
  an agent writes a file. None of that happens twenty times a minute, and all of
  it costs one git process per repo — expensive.

Polling the slow-moving state at the fast-moving state's cadence is where the
load comes from. Of the 16 repos scanned every tick, **13 have no linked
worktrees at all**, so they cost a git process each and contribute zero rows.

## Decision

**Split the dashboard's refresh in two, by how fast the underlying state
actually changes.**

1. **Fast tick — 3 seconds, tmux only.** One `tmux list-panes -a` plus one
   `tmux list-sessions` serve both jobs at once: layering agent state, window
   presence, and pane rows onto the worktree list devgeta already holds, and
   producing the standalone-session list. `WorktreeManager.List()` and
   `ListSessions()` each run their own `list-panes -a` today; they collapse into
   one shared scan, finishing what ADR-0008 §4 started within a single call.

2. **Slow tick — 30 seconds, plus every mutation, git-backed.** The
   `git worktree list --porcelain` enumeration runs on startup, immediately
   after any create/remove/repair the dashboard itself performs (so your own
   actions are never waiting on a timer), and on a 30-second safety net that
   catches worktrees created outside devgeta.

3. **The diff leaves the tick entirely.** It recomputes on selection change —
   debounced 180 ms, so holding `j` costs one diff instead of fifteen — on the
   slow tick, and on an explicit refresh key (`ctrl+r`, the only free
   conventional choice). A `diffMsg` whose `path` is no
   longer the selected row is dropped rather than rendered.

Idle cost falls from ~0.5s per 3s to ~0.5s per 30s: **a ~17% duty cycle becomes
~1.7%**, with no loss of responsiveness in the signal the dashboard exists to
show.

### Rejected alternatives

**Dedup repo anchors before exec'ing instead of after.** `forEachKnownRepo` runs
`git worktree list` on an anchor and only then dedups by resolved main root, so
two anchors pointing into the same repo cost two processes. This looked like the
obvious fix until it was measured: 17 anchors resolve to 16 distinct repos.
It saves one exec out of seventeen. Rejected as complexity that buys nothing —
recorded here because it is the change a reader will reach for first.

**Skip the git exec for repos whose worktree directory is absent or empty.**
Thirteen of sixteen repos have no linked worktree, and an `os.Stat` costs
microseconds against a git process's ~20 ms, so this is by far the largest
theoretical win. Rejected because it makes the filesystem the index, which
[ADR-0010](ADR-0010-worktree-layout-is-a-setting-git-is-the-index.md) decided
against: a worktree at a path devgeta did not choose — created by hand, or
before the `location` setting last changed — has no directory where the filter
looks, so it would silently disappear from the dashboard. The cadence split
gets a comparable win losslessly.

**Cache `DefaultBranchIn` per repo across the session.** Measured at 0.01s of a
~0.16s diff. Noise, and it would add a mutex-guarded cache to the git app for
it. Not worth it.

**Make the fast tick faster, since it is now cheap.** Tempting, but 3 seconds is
already below the latency at which an agent-state change matters, and a shorter
interval would re-spend the savings on the one path that has to stay hot.

## Consequences

**Easier.** The dashboard stops being a background CPU load. Navigation stops
queueing work that is thrown away. Each refresh path can be tuned against what
it actually reads, instead of one interval being a compromise between a tmux
query and sixteen git processes. Fast-tick tmux exec count per refresh halves.

**Harder.** There are now two refresh paths and two messages instead of one, and
`List()` splits into a git enumeration and a tmux layering step. A future
contributor adding a field to `WorktreeStatus` has to know which of the two
populates it. `List()` keeps its current signature and behavior for every
non-TUI caller (`ListNames`, completions), composed from the two halves, so the
two cannot drift.

**Accepted trade-off.** A worktree created outside devgeta — a bare
`git worktree add` in another terminal — can take up to 30 seconds to appear,
where it previously took 3. Worktrees created through devgeta appear
immediately, because a mutation triggers the slow path directly rather than
waiting for its timer.

**Accepted trade-off.** A diff can be up to 30 seconds stale while you watch an
agent write files, where it was previously up to 3. This is the cost the
explicit refresh key exists to buy back, and it is the single change that most
of the savings come from: two full-tree diffs every three seconds is the second
biggest line in the table above.

**Accepted trade-off.** The 180 ms navigation debounce means the diff pane lags a
deliberate single `j` by that much. Below the threshold where it reads as
lag, and it is what removes the burst that reads as lag today.
