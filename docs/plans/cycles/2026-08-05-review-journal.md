# Cycle: Review journal — per-branch review memory (ADR-0012)

**Date:** 2026-08-05
**Estimated Duration:** ~6 hours
**Status:** Complete (pending commit — see §5)

---

## 1. Domain Context

Re-reviewing a branch goes in circles: a reviewer asks questions, they get answered in a
chat session, the session ends, and the next review — a fresh agent with empty context —
asks the same questions and never approves. The reviewers are told not to read prior PR
feedback (dedup was `/review-pr`'s job), and the pre-PR flow has no feedback to read
anyway.

[ADR-0012](../../decisions/ADR-0012-review-knowledge-in-a-local-journal.md) (ACCEPTED)
decides the fix: a per-branch markdown journal in the repo's common git directory
(`$(git rev-parse --git-common-dir)/devgeta/review/<encoded-branch>.md`), written and read
only through `devgeta task` commands, with per-entry staleness computed from **blob
identity** (`git hash-object`), and cleanup riding on `dg wt remove`'s existing branch
deletion. All design decisions live in the ADR; this cycle is the build plan.

Probes already done (recorded here so they aren't re-run):

- From a linked worktree, `git rev-parse --git-common-dir` resolves to the main `.git`;
  `--git-dir` does not. The main checkout returns a **relative** `.git`, so the wrapper
  must pass `--path-format=absolute`.
- A dirty edit under an unchanged HEAD is invisible to `git diff <sha>..HEAD -- <path>`
  but changes `git hash-object <path>` — the blob-staleness design is confirmed against
  real git.

## 2. Scope

**In:** journal core package, two task subcommands, `dg wt remove` cleanup hook, reviewer
prompt updates (including document-reviewer's missing verdict), guard test, docs.

**Out (per ADR):** seeding the journal from GitHub threads; cross-clone/machine sharing;
following branch renames; any cleanup triggered by approval.

## 3. Steps

- [x] **Step 0 — Probes.** `--git-common-dir` from a linked worktree; blob-vs-SHA on a
      dirty file. Both confirmed (see Domain Context).
- [x] **Step 1 — Git wrapper methods** (`internal/apps/git`): `CommonDirIn(dir)` using
      `--path-format=absolute --git-common-dir`; `HashObjectIn(dir, path)`; reuse the
      existing branch-name helper if one exists. Mock tests; no real git in tests.
- [x] **Step 2 — `internal/tooling/reviewjournal`** (new package; lives outside
      `tooling/task` because `task` already imports `tooling/worktree`, and the worktree
      teardown must call the journal — putting it in `task` would cycle):
  - Reversible percent-encoding of branch names (`/` and all bytes outside
    `[A-Za-z0-9._-]`), decode for listings, containment check before any write.
  - Journal model: frontmatter (branch, base, last_review), Settled/Open sections,
    stable ids `n<seq>`; parse + render round-trip.
  - Entry writes: open, settle-by-id (path carries over; no `--at`), direct settle
    (optional `--at`); blob + head stamped by the writer; nonexistent `--at` fails
    without writing; atomic temp+rename.
  - Staleness: blob compare per cited path; deleted cited file renders stale; pathless
    entries never mechanically stale.
  - Prune: drop journals whose branch exists neither locally nor on the remote.
  - Tests: hostile branch names (`fix/a-b` vs `fix/a/b`, `../../x`, unicode), dirty-file
    staleness, deleted path, crash-safety via rename, id stability, round-trip.
- [x] **Step 3 — Task commands** (`cmd`): `devgeta task review-notes
[--branch <b>] [--path] [--prune]` and `devgeta task review-note` per the ADR's
      contract. Output per [task-design.md](../../guides/task-design.md): labeled plain
      text, fixed sentinels (`No review notes for branch <b>.`,
      `No branch — review notes are keyed by branch.`), mutations echo `Noted n4` /
      `Settled n4 (answered)`.
- [x] **Step 4 — Teardown cleanup**: `dg wt remove` deletes the removed branch's journal
      in the same operation that deletes the branch. Best-effort (a failed journal delete
      warns, never fails the remove). Test: journal gone after remove; sibling journals
      untouched.
- [x] **Step 5 — Prompts**: all three reviewers read `review-notes` before reviewing and
      record open questions with `review-note --open`; document-reviewer gains the
      `**Status:** APPROVE | REQUEST CHANGES | NEEDS DISCUSSION` verdict and a
      questions-only-when-blocking rule; `/review-pr` and `/address-feedback` settle
      answered items (`--settle <id> --as ...`). Guard test in
      `internal/apps/opencode/permissions_test.go`: every `*-reviewer.md` must contain the
      verdict line and the `review-notes` read; verified to fail when either is removed.
- [x] **Step 6 — Docs + manual verification**: spec.md (task table + reviewer/R-picker
      section); end-to-end in the probe repo: open → notes → dirty edit → stale → settle →
      wt remove → journal gone; then delete probe repos. Mark this doc Done.

## 4. Verification (maps to the ADR's acceptance gate) — all met

| Gate item                                                                | How it was proven                                                                                                                                                                                                                            |
| ------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--git-common-dir` resolves to main `.git` from a linked worktree        | Real binary: `review-note --open` run **inside** `worktrees/probe2/teardown-probe` wrote `probe2/.git/devgeta/review/teardown-probe.md` in the main repo, next to another branch's journal                                                   |
| Blob staleness catches a dirty edit a SHA check misses                   | Real git in a probe repo (same HEAD, different blob), plus `TestVerdictCatchesDirtyEditThatHeadWouldMiss` — verified to FAIL when `Verdict` compares HEAD instead                                                                            |
| Hostile branch names: no collision, no escape                            | `TestEncodeBranchRoundTripsAndNeverCollides`, `TestHostileBranchNameStaysInsideTheReviewDir` (`../../etc/passwd`, `fix/a-b` vs `fix/a/b`, unicode)                                                                                           |
| Teardown deletes the journal                                             | Real binary: `worktree-finish --discard` deleted `teardown-probe.md` and left the sibling; plus `TestRemoveByRepoDeletesTheBranchsReviewJournal` and `TestWorktreeFinishDeletesTheBranchsJournal`, both verified to FAIL without the cleanup |
| Atomic writes; crash leaves the old journal                              | `TestWritesAreAtomicAndLeaveNoTempFiles` (temp+rename, no leftovers)                                                                                                                                                                         |
| Nonexistent `--at` fails without writing; deleted cited file reads stale | `TestOpenRejectsNonexistentCitedPathWithoutWriting`, `TestVerdictTreatsDeletedCitedFileAsStale`                                                                                                                                              |

## 5. Not committed — concurrent work in the tree

`internal/apps/git/git.go` currently carries **two sessions' work**: this cycle's
`ShortHeadIn`/`CommonDirIn`/`HashObjectIn`, and another session's
`CreateWorktreeAtBaseIn`/`NormalizeWorktreeGitfile`/`createWorktreeIn` for
[ADR-0013](../../decisions/ADR-0013-normalize-the-worktree-gitfile.md). `docs/decisions/README.md`
likewise holds both index entries, and `internal/tooling/task/worktree.go` and `tmpverify/`
are that session's. Committing this cycle would sweep in unfinished ADR-0013 work, so the
commit is deliberately left to the maintainer to sequence.

## 6. Scope discovered during implementation

`worktree-finish` deletes a worktree's branch through `Git.RemoveWorktree` **directly**, not
through `WorktreeManager.removeByRepo`, so it did not inherit Step 4's cleanup and needed its
own — three call sites (discard, force-discard fallback, merge). Found by checking which
removal path the task actually uses rather than assuming one; without it the ADR's "cleanup
rides on branch deletion" would have been true for `dg wt remove` only.

Fixing it added one git call to `worktree-finish`, which shifted several strict call-sequence
tests. One of them (`--force falls back to a direct removal…`) then passed for the **wrong
reason** — the shifted mock made `RemoveWorktree` fail at main-worktree resolution instead of
at the `worktree remove` refusal its comment describes. Its sequence was corrected rather
than its count bumped.
