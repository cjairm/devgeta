# ADR-0019: A review covers the branch's working state, not just its committed history

**Status:** ACCEPTED
**Date:** 2026-08-07
**Deciders:** cjairm
**Related:** [ADR-0017](ADR-0017-review-loop-escalates-instead-of-seeking-consensus.md), [ADR-0018](ADR-0018-review-loop-refuses-the-default-branch.md), [ADR-0012](ADR-0012-review-knowledge-in-a-local-journal.md), [ADR-0023](ADR-0023-a-pr-review-targets-immutable-shas.md) (its explicit-range mode reviews immutable SHAs only, never the working state this ADR established for branch mode)

---

## Context

`dg task review-run` refused to run on a branch with no commits ahead of the default
branch, and the two commands the reviewers read the change through — `review-scope` and
`branch-diff` — both compared `<merge-base>...HEAD`, which git resolves against **HEAD**,
not the working tree. Together that meant a review could only ever see committed work.

Running the loop for real showed why that is the wrong scope:

1. **It blocks the case reviews are most useful for.** Work in progress — edited but not
   yet committed — is exactly when a reviewer's opinion is cheapest to act on. The refusal
   turned "review this" into "commit something first", and a commit made only to satisfy a
   tool is a commit that has to be amended or squashed away afterwards.
2. **The refusal was self-inflicted, not inherent.** Nothing downstream needs a commit: the
   journal is keyed by branch name (ADR-0012 §5), not by SHA, and `git diff` can compare a
   commit against the working tree perfectly well.
3. **devgeta already showed the branch this way somewhere else.** `dg ws`'s diff pane calls
   `BranchDiffAt`, which diffs the merge-base against the **worktree** and lists untracked
   files by name. So the dashboard and the reviewers disagreed about what "this branch
   changes" meant, with the human-facing view being the more complete one.

The counter-argument is real and worth stating: the working tree is not what merges. A
reviewer's verdict is recorded against a branch whose uncommitted part can be edited or
reverted a second later, and nothing pins the reviewed bytes the way a SHA would.

## Decision

**A review covers everything the branch would merge: its commits AND its uncommitted work,
including untracked files.**

Concretely:

- **`branch-diff` and `review-scope` diff the merge-base against the working tree** —
  `git diff <base>`, two dots, not `<base>...HEAD`. Two dots is the whole mechanism: it
  compares a commit to the working tree, so staged and unstaged edits are included.
- **Untracked files are named, never counted.** `git diff` cannot see them at all, so they
  are listed by name (`untracked (…)` in review-scope, an `Untracked files` block in
  branch-diff) and are deliberately absent from the stat table, which has no counts to
  report for them. `branch-diff --file` on an untracked file says it is untracked rather
  than "no changes", because an empty diff there means "the whole file is the change" and
  the two answers point a reviewer in opposite directions.
- **review-scope names which changed files are not committed yet** (`uncommitted (…)`, from
  `git diff HEAD`). Without that line, a file in the table that no commit in the list above
  mentions looks like a bug in the report.
- **`review-run`'s guard becomes "does this branch change anything at all":** it refuses
  only when there are no commits ahead **and** the working tree is clean
  (`git status --porcelain`, which reports untracked files). Both halves fail **open** — an
  unresolvable comparison lets the round run, per the guard's cost-saving-not-safety role
  (the same reasoning as [ADR-0016](ADR-0016-inconclusive-tool-probe-fails-open.md)).
- **One gather, two renderings.** `collectWorktreeDiff` is the single place this is
  computed; `BranchDiffAt` (the `dg ws` pane, colored) and `BranchDiff` (the machine-read
  payload) both build on it, so the dashboard and the reviewers can no longer drift apart.
- **`review-package <base> <head>` is untouched.** An explicit historical range is a
  question about committed history by definition, and the working tree has no place in it.

On the "not what merges" objection: the reviewers' findings live in a journal that already
handles the code moving underneath them. Every entry carries a `[fresh]`/`[STALE]` marker
computed from the cited file's content hash (ADR-0012), so a finding raised against
uncommitted work that then changes is marked STALE and re-checked — the same machinery that
already covers a finding raised against a commit that gets amended. The risk this ADR
accepts was already a solved problem in the journal, which is what makes the trade
acceptable rather than merely convenient.

## Consequences

**Easier**

- Reviewing work in progress needs no commit, no `git stash`, no throwaway WIP commit.
- The `dg ws` diff pane and the reviewer agents describe the same change.
- A branch whose only work is a brand-new file is reviewable; before, it was refused
  twice over (no commits, and the file invisible to the diff).

**Harder / accepted trade-offs**

- **The reviewed bytes are not pinned.** A verdict is about the tree as it stood during the
  round. Mitigated, not eliminated, by the journal's staleness marking.
- **`review-scope` costs two more git calls** (`diff HEAD` numstat + name-status) and
  branch-diff one (`status --porcelain`). Accepted: they are local, and they buy the
  distinction between committed and uncommitted work that the report would otherwise
  present as one undifferentiated table. `branch-diff --file` deliberately does **not** run
  the full gather — a reviewer walks a large branch one file at a time, so that path stays
  at two git calls (three when the diff is empty and untracked status decides the answer).
- **Generated or scratch files in a dirty tree now reach the reviewers.** The existing
  lockfile-style exclusions apply unchanged, but anything else uncommitted is in scope.
  That is the honest reading of "what this branch would merge", and it is visible in the
  report rather than silent.
- **A reviewer must be told this.** All three reviewer agents' Scope sections now state
  that the branch includes uncommitted work and must never ask for a commit first; a
  reviewer that treats an uncommitted file as out of scope would silently review less than
  the round claims.
