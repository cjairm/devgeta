# ADR-0018: The review loop refuses the default branch, and the fix is a branch

**Status:** PROPOSED
**Date:** 2026-08-06
**Deciders:** cjairm
**Related:** [ADR-0012](ADR-0012-review-knowledge-in-a-local-journal.md), [ADR-0017](ADR-0017-review-loop-escalates-instead-of-seeking-consensus.md), [ADR-0010](ADR-0010-worktree-layout-is-a-setting-git-is-the-index.md), [cycle 2026-08-05-review-loop](../plans/cycles/2026-08-05-review-loop.md)

---

## Context

devgeta's review machinery is branch-shaped in two independent places:

- **The journal is keyed by branch** and lives at
  `$(git rev-parse --git-common-dir)/devgeta/review/<encoded-branch>.md`
  ([ADR-0012](ADR-0012-review-knowledge-in-a-local-journal.md) §5).
- **The review prompt diffs the branch against the default branch**
  (`reviewPrompt` in `internal/tooling/worktree/layout.go`).

Both assumptions break when the current branch _is_ the default branch, and they break in
different ways:

1. **The diff is meaningless.** `main` against `main` is empty, so there is nothing to
   review. The loop would run reviewers over nothing and could plausibly return
   `APPROVE` — the single worst outcome available, since it looks like a passing review.
2. **The journal would never be cleaned up.** Journal cleanup rides on branch deletion.
   Nobody deletes `main`, so a `main` journal accumulates forever and is read by every
   future review of the default branch.

So the question: when `/review-loop` is invoked on the default branch, what should happen?
Three candidate answers — refuse, silently create a branch, or silently create a worktree —
and the choice is not obvious, because the loop is a long unattended operation and a
refusal costs the user a round trip.

A second, related question, since isolation is the underlying need: **if** devgeta ever
provides the isolation itself, is the right mechanism a branch or a worktree?

## Decision

**`dg task review-run` refuses to run unless HEAD is a named, non-default branch, with an
actionable error naming the fix. It creates nothing on the user's behalf.**

Two states are refused, not one:

| HEAD state               | Why it is refused                                                        | Error names            |
| ------------------------ | ------------------------------------------------------------------------ | ---------------------- |
| **The default branch**   | The diff is empty, and a `main` journal can never be cleaned up          | `git switch -c <name>` |
| **Detached (no branch)** | There is no branch to key a journal by and no branch to diff — see below | `git switch -c <name>` |

The refusal reuses the existing default-branch resolution that the `review-scope` family
already uses — not a restated branch name or a hardcoded `main`.

### Detached HEAD must be refused explicitly, not by implication

This case is easy to miss and fails late and expensively, so it is called out here rather
than left to the implementer. `Git.CurrentBranchIn` runs `git branch --show-current`, which
**prints nothing when HEAD is detached** and is therefore returned as `("", nil)` — an empty
string with **no error** (`internal/apps/git/git.go:356`). So a check written only as the
inverse of the default-branch test passes straight through: `"" != "main"` is true.

The journal does refuse an empty branch — `PathFor` returns "a branch name is required"
(`internal/tooling/reviewjournal/manager.go:48`) — but that is a **late backstop, not a
gate**. By the time it fires, the loop has already spent a full multi-model review; the
findings then cannot be journaled, so the next round has no memory and the loop cannot
function as designed. A guard that fires after the expensive work is not a guard.

Detached HEAD is a reachable state, not a theoretical one: `git checkout <sha>`, `git
bisect`, and CI checkouts all produce it, and devgeta's own
`finishing-a-development-branch` flow treats "detached HEAD (externally managed workspace)"
as an ordinary case with its own menu.

So the branch resolution must distinguish three outcomes — named non-default branch
(proceed), default branch (refuse), no branch at all (refuse) — and both refusals must
happen before any reviewer is launched.

**The same package already contains the mirror image of this check, and it must be shared
rather than copied.** `internal/tooling/task/release.go:143` has
`checkOnDefaultBranch()`, which refuses when HEAD is _not_ the default branch and names
the fix command in its error. The review loop needs the inverse — refuse when HEAD _is_
the default branch — over the identical `tm.Git.CurrentBranch()` / `tm.Git.DefaultBranch()`
pair, in the same `TaskManager`, with the same error shape. Per CLAUDE.md §6, the second
use is the point at which the comparison is extracted into one helper serving both
directions, in the same change that introduces it. Two hand-rolled copies of "compare HEAD
to the default branch and format an actionable error" is the duplication that rule exists
to prevent.

And **the isolation mechanism, if it is ever automated, is a branch — not a worktree.**
`git switch -c`:

- carries uncommitted work across for free, with no copy step;
- needs no merge-back machinery, because the work never left the checkout;
- is one command the user can run, understand, and undo.

A worktree needs a copy and a merge-back path, which is a materially larger feature. A
`--worktree` opt-in — for "let the loop run while I keep coding" — is future work built on
`dg wt create`, and is deliberately out of scope here.

### Why refuse rather than auto-create

Auto-creating a branch would move the user's `HEAD` as a side effect of asking for a
review. For a command that then runs unattended for minutes, that is a surprising mutation
to discover afterwards: the user asked to review their work, not to be relocated onto a new
branch whose name devgeta chose. A one-line refusal that names the exact fix costs one
round trip and leaves the user in control of their own branch topology — consistent with
[ADR-0010](ADR-0010-worktree-layout-is-a-setting-git-is-the-index.md), where git remains
the index of record and devgeta reads it rather than reshaping it.

The refusal is also the honest signal. The failure it prevents is not "the loop is
inconvenient here" but "a review of nothing can report APPROVE," and a user who sees the
error learns something true about how reviews are scoped.

## Consequences

### Positive

- **A review of an empty diff cannot report approval**, because it cannot start. The
  dangerous outcome is removed by construction rather than by a check downstream.
- **No expensive run is spent on a state that cannot be journaled.** Both refusals fire
  before the first reviewer launches, so the journal's own empty-branch error
  (`manager.go:48`) is never reached in practice — it stays a backstop instead of being
  the thing that stops you, minutes and several model calls too late.
- **No `main` journal is ever created**, so ADR-0012's cleanup-on-branch-deletion stays
  sufficient and nothing accumulates unbounded.
- **devgeta never silently changes which branch the user is on.** The one destructive-ish
  thing in this area — moving `HEAD` — stays a user action.
- **The error teaches the model.** "Reviews are diffed against the default branch, so run
  this from a feature branch: `git switch -c <name>`" explains the scoping rule at the
  moment it matters.

### Negative

- **A user on the default branch pays a round trip.** They must run `git switch -c` and
  re-invoke. Accepted: it is one command, it preserves their dirty files, and the
  alternative mutates their checkout without being asked.
- **"Keep coding while the loop runs" is not available yet.** That is the capability a
  worktree would buy, and it is deferred. Users who want it can create a worktree by hand
  with `dg wt create` today.

### Neutral

- The refusal is a property of `dg task review-run`, the Go layer, not of the
  `/review-loop` command file. A different caller of the same task command gets the same
  refusal for free, and the rule cannot be lost by an edit to an agent prompt.
- Nothing here deletes journals. Cleanup remains on branch teardown, unchanged.

## Alternatives Considered

### Auto-create a branch and continue

Detect the default branch, run `git switch -c review-loop-<timestamp>`, proceed.

Rejected: it relocates the user's `HEAD` as a side effect of a review request, and the user
does not find out until the long-running command finishes. It also invents a branch name,
which then leaks into the journal key, the eventual PR, and `dg ws` — naming the user's
work is not devgeta's call here. The mechanism is right (a branch, per the decision above);
doing it unasked is not.

### Auto-create a worktree and continue

Rejected on top of everything above: a worktree does not carry uncommitted changes, so the
loop would review a _different_ (clean) tree than the one the user is looking at — the
same class of wrong-target bug as reviewing an empty diff. It also needs merge-back
machinery that does not exist. Kept as a future explicit opt-in (`--worktree`), where the
user choosing it knows what they are choosing.

### Allow it, and diff against the previous commit instead

Change the diff base to `HEAD~1` when on the default branch.

Rejected: it silently redefines what "review this work" means, and the answer would be
wrong for the common case of several commits' worth of work. It also does nothing about the
un-cleanable `main` journal.

### Warn and proceed

Rejected: the outcome being prevented is a green review of nothing. A warning that scrolls
past above several minutes of reviewer output is not a control — the user would read
`APPROVE` at the bottom and believe it.
