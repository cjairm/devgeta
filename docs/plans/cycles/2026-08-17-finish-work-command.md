# Cycle: Finish-work — one entry point from unapproved docs to a landed merge

**Date:** 2026-08-17
**Estimated Duration:** ~5 hours (was ~4; the §8 Q3 resolution adds an ADR and a
merge-path journal gate, and the §8 Q5 resolution a second merge-path refusal
recorded in that same ADR)
**Status:** Complete — all six steps implemented, reviewed (task-scoped review
per task, plus a final whole-branch review), and merged (2026-08-18). Two
Important findings from the final review (an invalid `review-note --settle`
invocation copied into a refusal message, `docs/spec.md`, and ADR-0027; and
missing argv assertions pinning the local-vs-`origin/` anchoring) were fixed
before merge. Step 6's "decide whether `finish-work` belongs in CLAUDE.md's
command table" was answered: no such table exists in CLAUDE.md (the "Quick
Reference: Common Commands" table is `make`/`go` commands for contributors,
not slash commands; the "Documentation Index" maps docs, not commands) — no
CLAUDE.md edit was needed.

---

## 1. Domain Context

The maintainer repeatedly types a variant of one prompt at the end of a piece of
work:

> "please mark as approved… then merge to main… and say if this is ready to
> remove this worktree"

It means four things in sequence: **(1)** sweep the docs this branch touched and
flip the ones still sitting at a pre-approval status, **(2)** verify the work,
**(3)** land the branch on the default branch locally — no push, and **(4)**
report whether the worktree can go. Today that is a hand-driven sequence of
`git`, doc edits, and one `devgeta task` call, re-derived every time.

Most of step 3 already exists. `devgeta task worktree-finish --merge`
(`cmd/task.go:558` → `internal/tooling/task/worktree.go:202`) already:

- resolves the target deterministically (explicit name wins, else cwd's linked
  worktree, else it errors and lists what it found — it never guesses from a
  main checkout)
- refuses when the main checkout is not on the default branch
- rebases onto the default branch **only when diverged**, and on conflict stops
  with an actionable message naming both the resolve and the
  `git -C <path> rebase --abort` escape
- `git merge --ff-only` into the default branch from the main checkout, so no
  merge commit
- removes the worktree, deletes the branch, drops the branch's review journal
  (ADR-0012)
- **never pushes** — which is exactly the wanted shape: land locally, push in a
  later, separate act (CLAUDE.md §9)

So this cycle is not "build a merge command". It is: close the five gaps around
an existing command, and give the whole routine one entry point.

Related reading: [CLAUDE.md](../../../CLAUDE.md) §6 (which tests to run), §9
(release flow — tag before push), §10 (spec-driven development), §12 (anything
we ship is built for strangers);
[docs/guides/task-design.md](../../guides/task-design.md) (when a `dg task` is
justified, and its output principles).

### The five gaps

1. **No dirty check on the merge path — in either checkout it touches.**
   `worktreeFinishMerge` never calls `IsWorktreeDirty`. Only `WorktreeStart`
   (`worktree.go:43`) and `worktreeFinishDiscard` (`worktree.go:129`) do; the
   tests confirm the asymmetry (`worktree_test.go:55`, `:420` — nothing
   equivalent in `TestWorktreeFinish_Merge`, `worktree_test.go:613`). The merge
   path runs git in **two** directories — it rebases at `wtPath`
   (`worktree.go:228`) and runs `git merge --ff-only` in the main checkout
   (`worktree.go:237`) — and neither one is checked.

   Dirty at `wtPath`, two distinct bad outcomes:
   - dirty **and** diverged → `git rebase` fails on the unstaged changes, and
     the error we wrap it in tells the user to _resolve conflicts_ that do not
     exist. Misleading diagnosis.
   - dirty **and** not diverged → the rebase is skipped, the `--ff-only` merge
     **succeeds and moves the default branch**, then `RemoveWorktree` refuses
     (it deliberately never passes `--force`). Result: the commits landed, the
     uncommitted work is stranded in a worktree the message describes as
     "failed to remove". The default branch moved on a half-finished operation.

   Dirty in the **main checkout**, which is independent of the worktree's own
   state and equally unchecked — the main checkout is resolved at
   `worktree.go:205` and only its _branch_ is examined (`worktree.go:210-219`):
   - the dirty paths overlap what the fast-forward writes → `git merge --ff-only`
     refuses ("your local changes would be overwritten"), so a worktree
     `--check` called ready fails at the last step. That is precisely the
     check/merge disagreement §7 names as this cycle's top risk.
   - they do not overlap → the merge lands, the default branch moves, and the
     main checkout is left holding unrelated uncommitted work sitting on top of
     freshly merged commits. The worktree is then removed, so the branch's own
     copy of that state is gone before anyone looks at the mixture.

   This is a defect, not a missing convenience, and CLAUDE.md §4 wants it made
   structurally impossible rather than documented. The main-checkout half was
   found in review after the rest of this plan was written; see §8 Q5 for the
   fork it opened and how it was settled.

2. **No verification gate.** Deliberate today — the command's own doc says
   "verification is the caller's responsibility." Nothing runs build, lint, or
   tests before the merge. That judgment (which packages? §6's targeted set)
   belongs in a command's prose, not in the Go task.

3. **No doc-approval sweep.** Nothing reads `**Status:**`. At the time of
   writing, `docs/decisions/` holds six ADRs still `PROPOSED` and several cycle
   docs sit at `Draft` / `In Progress` — that backlog is precisely what the
   maintainer keeps clearing by hand at merge time.

4. **Open review-journal findings do not block a merge.** `review-notes` knows
   them (`cmd/task.go:269`); `worktree-finish` never asks. A branch with an
   unsettled blocking finding can land silently. Closed on both paths — see §8
   Q3 for which findings block and why staleness decides it.

5. **No way to ask "is this ready?" without doing it.** `--merge` is
   all-or-nothing, so the maintainer's actual question — _is this ready to
   remove?_ — has no cheap answer, and no answer at all short of attempting the
   merge.

---

## 2. Engineer Context

**Relevant files:**

- `internal/tooling/task/worktree.go` — `WorktreeStart`, `WorktreeFinish`,
  `worktreeFinishMerge` (line 202), `worktreeFinishDiscard` (line 127),
  `resolveWorktreeTarget` (line 302), `dropReviewJournal` (line 288)
- `cmd/task.go:558` — `taskWorktreeFinishCmd`, its `--merge/--discard/--force`
  flags (line 530), and the `TaskManagerInterface` entry at line 48
- `internal/apps/git/git.go` — the git wrapper every external `git` call must
  route through (CLAUDE.md §6, "Route external tools through their app
  wrappers"): `IsWorktreeDirty`, `ExecuteCommandAt`, `GetMainWorktree`,
  `CurrentBranchIn`, `DefaultBranchIn`, `RemoveWorktree`
- `internal/tooling/reviewjournal/` — the journal `review-notes` reads; the
  source for gap 4. The three pieces step 3 needs already exist: `Manager.Load`,
  `Entry.Open()` (`journal.go` — `Resolution == ""`), and `Manager.Verdict`
  (`manager.go:622` — returns `FreshnessStale` / `FreshnessFresh` /
  `FreshnessDateless`). Write no new staleness logic; `Verdict` is the one place
  that decides it, and its doc comment records why it fails toward stale.
- `configs/shared/commands/` — where the new slash command lands, shipped to
  both agents (`review-loop.md` is the closest precedent in size and shape)

**Key existing behaviors not to re-implement:**

- `resolveWorktreeTarget` — target resolution is solved; the new `--check` path
  must reuse it verbatim, not re-derive it
- The `merge-base --is-ancestor` divergence probe (`worktree.go:226`) — reuse
  the same test so `--check` and `--merge` can never disagree about whether a
  rebase is needed. The git call itself is unchanged; step 4 only lifts it into
  a `Git.IsAncestor` wrapper so both paths share one exit-status-aware reading
  of its result instead of two
- `errors.Is(err, git.ErrBranchDeleteFailed)` at `worktree.go:257` — the comment
  there records why substring matching on messages rotted; do not reintroduce it

**Testing patterns:** `testutil.MockApp`, `testutil.VerifyNoRealCommands`, and
never a real `git`. See
[docs/guides/testing-patterns.md](../../guides/testing-patterns.md). The
existing `TestWorktreeFinish_Merge` asserts the exact sequence of git calls, so
each dirty check step 1 inserts — one at `wtPath`, one at the main checkout —
**will** change its expected call count. That is the test telling the truth, not
a broken test.

**Commands to run tests** (targeted — from the `go list` query in CLAUDE.md §6;
the direct importers of `internal/tooling/task` are `cmd` and
`internal/tui/worktree`):

```bash
go test ./internal/tooling/task/ ./cmd/ ./internal/tui/worktree/
make lint
```

Step 5 adds a file under `configs/shared/commands/`, which the embedded-config
tests in the **root** package cover. That run is ~4.8 minutes on its own, so it
belongs at the end of step 5 only — not after every edit:

```bash
go test .    # only for the configs/ change in step 5
```

---

## 3. Objective

One command the maintainer runs at the end of a piece of work that sweeps
unapproved docs for approval, verifies, lands the branch on the default branch
locally without pushing, and reports the worktree's disposition — backed by a
`worktree-finish` that can answer "am I ready?" without acting, and that refuses
to merge when either checkout it touches — the worktree or the main checkout —
is dirty.

---

## 4. Scope Boundary

### In Scope

- [x] `worktreeFinishMerge` refuses a dirty worktree up front (gap 1)
- [x] `worktreeFinishMerge` refuses when the **main checkout** is dirty — any
      dirtiness, not only paths the fast-forward would touch (gap 1 — added by
      the §8 Q5 resolution; the original scope checked dirtiness at `wtPath`
      alone, which left the checkout `merge --ff-only` actually runs in
      unguarded)
- [x] `worktreeFinishMerge` refuses when the branch's review journal holds a
      **non-stale** open finding (gap 4 — added by the §8 Q3 resolution; the
      original scope closed gap 4 through `--check` alone)
- [x] `devgeta task worktree-finish --check` — a read-only readiness report,
      non-zero exit when blocked (gaps 4 and 5)
- [x] `--check`'s status-marker recognizer knows more than devgeta's own
      `**Status:**` rendering — a front-matter `status` key, a header-block label
      line in any of its common spellings, and a `Status` section — while the
      _vocabulary_ stays out of the binary (added by the review of this document;
      the original scope's literal rule would have made the sweep fire in this
      repo and nowhere else, which §12 forbids for a shipped command)
- [x] `configs/shared/commands/finish-work.md` — the single entry point (gaps 2
      and 3)
- [x] Tests for all six, mocked
- [x] Docs: `docs/spec.md` for the new flag, `CLAUDE.md`'s command table if the
      new command belongs there
- [x] One ADR covering **both** merge-refusal forks — the journal gate (§8 Q3)
      and the dirty main checkout (§8 Q5). §10 requires an ADR for each fork
      with lasting impact; they share one question ("what may block a
      `--merge`, and how does `--check` mirror it"), the same rejected
      warn-only shape, and the same no-`--force` consequence, so they are
      recorded together rather than split across two records that would each
      have to remember the other. See §8 Q5 for the reasoning.
- [x] A second ADR for the status-marker recognizer fork — shapes in the binary,
      vocabulary in the command — with the two rejected alternatives step 4
      names. A separate record because it answers a different question from the
      merge-refusal one, and §10 wants it before the code it governs.

### Explicitly Out of Scope

- Pushing, tagging, or any part of the §9 release chain. `finish-work` stops at
  a landed local merge and says so.
- A `--keep-worktree` mode. After a `--ff-only` merge the branch is deleted, so
  there is nothing for a kept worktree to be checked out on; the later push
  happens from the main checkout.
- Teaching `--check` to run tests. Verification stays in the command's prose
  where the repo's own conventions can be read; a Go task that hardcodes a test
  command would be bespoke by definition (§12).
- Editing `configs/shared/skills/finishing-a-development-branch/`. It is a
  vendored upstream Superpowers skill; local edits buy a merge conflict on every
  sync (ADR-0015 §7). It stays as the generic fallback for non-worktree repos.
- The three adjacent findings in the appendix.

**Scope is locked.** Anything discovered outside it gets documented for a future
cycle and referenced here.

---

## 5. Implementation Plan

### File Changes

| Action | File Path                                  | Description                                                                                                                                                                     |
| ------ | ------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Modify | `internal/tooling/task/worktree.go:202`    | Dirty refusal at the top of `worktreeFinishMerge`                                                                                                                               |
| Modify | `internal/tooling/task/worktree.go:214`    | Dirty-main-checkout refusal, beside the existing `mainBranch != defaultBranch` guard (§8 Q5)                                                                                    |
| Modify | `internal/tooling/task/worktree.go`        | Shared `openBlockingFindings` probe; merge refusal built on it                                                                                                                  |
| Modify | `internal/tooling/task/worktree.go`        | New `worktreeFinishCheck` + readiness formatter                                                                                                                                 |
| Modify | `internal/apps/git/git.go`                 | New `MergeTreeConflicts` and exit-status-aware `IsAncestor` — §6-mandated plumbing for step 4's `conflicts:` and `rebase:` lines, not a scope addition (see step 4)             |
| Modify | `internal/apps/git/git_test.go`            | `MergeTreeConflicts`: clean, conflict, and command-failure exit statuses; `IsAncestor`: exit 0 / 1 / 128                                                                        |
| Modify | `internal/tooling/task/worktree.go`        | Divergence probe routed through `Git.IsAncestor`; `--merge` refuses when the probe cannot be answered instead of rebasing blind                                                 |
| Modify | `internal/tooling/task/branchdiff.go`      | Extract `changedFiles(g, dir, base)` so `doc-status` and `collectWorktreeDiff` share one gather across two different bases — §6 DRY plumbing, not a scope addition (see step 4) |
| Modify | `internal/tooling/task/branchdiff_test.go` | `changedFiles` still produces `branch-diff`'s existing `origin/`-based file list unchanged                                                                                      |
| Create | `internal/tooling/task/docstatus.go`       | `statusMarker` — the status-marker recognizer: three shapes, no vocabulary (step 4)                                                                                             |
| Create | `internal/tooling/task/docstatus_test.go`  | One case per recognized shape and per rejected shape, each drawn from a real file                                                                                               |
| Modify | `cmd/task.go:530,558`                      | `--check` flag, mutually exclusive with `--merge`/`--discard`                                                                                                                   |
| Modify | `cmd/task.go:48`                           | `TaskManagerInterface.WorktreeFinish` signature gains `check`                                                                                                                   |
| Modify | `internal/tooling/task/worktree_test.go`   | Dirty-worktree and dirty-main-checkout merge cases; journal-block cases; `--check` cases per blocked reason                                                                     |
| Modify | `cmd/task_test.go`                         | Flag parsing and exclusivity                                                                                                                                                    |
| Create | `docs/decisions/ADR-00NN-...md`            | Both merge-refusal forks in one record: the journal gate (§8 Q3) and the dirty main checkout (§8 Q5), written before step 3                                                     |
| Create | `docs/decisions/ADR-00NN+1-...md`          | The status-marker recognizer fork: marker _shapes_ in the binary, vocabulary in the command, written before step 4                                                              |
| Modify | `docs/decisions/README.md`                 | Add both new ADRs to the index — step 3 of that file's own "How to Create an ADR", and how every existing ADR is reachable                                                      |
| Create | `configs/shared/commands/finish-work.md`   | The slash command                                                                                                                                                               |
| Modify | `docs/spec.md`                             | Document `--check` and the three new merge refusals                                                                                                                             |

### Step-by-Step

#### Step 1: Refuse a dirty worktree — and a dirty main checkout — on the merge path

Both checks use the same probe, `Git.IsWorktreeDirty(path)` (`git.go:1074` —
`git -C <path> status --porcelain`, non-empty output means dirty). They are two
calls to one existing helper, not two mechanisms; that reuse is half of why §8
Q5 chose this shape over path-overlap detection.

- **At `wtPath`:** in `worktreeFinishMerge`, before resolving the default
  branch, call `tm.Git.IsWorktreeDirty(wtPath)` and return a refusal naming the
  fix ("commit or stash your changes first") — mirror `WorktreeStart`'s wording
  at `worktree.go:49` so the two refusals read the same.
- **At the main checkout:** immediately after the existing
  `mainBranch != defaultBranch` guard (`worktree.go:214-219`), call
  `tm.Git.IsWorktreeDirty(mainWorktree)` and refuse, naming the path so the user
  knows which of the two trees to clean:
  `main checkout <path> has uncommitted changes; commit or stash them there first`.
  It goes there and not earlier for a mechanical reason — `mainWorktree` does
  not exist until `GetMainWorktree` resolves it at `worktree.go:205` — and it
  goes _beside_ the branch guard because both are checks on the same directory,
  and one of them is already there.
- **Any dirtiness blocks, not just paths the fast-forward would touch** (§8 Q5).
  We do not try to predict which paths `git merge --ff-only` will write and
  intersect them with the dirty set; over-refusing costs the user one commit or
  stash, and the narrower rule is fragile logic guarding a rare case.
- No `--force` escape on either check. `--discard --force` exists because
  throwing work away is a real intent; "merge while dirty" has no legitimate
  form — at `wtPath` the uncommitted half cannot land, and in the main checkout
  the merge either fails or buries unrelated work under the commits it just
  landed (§1, gap 1).
- Verify: `go test -run TestWorktreeFinish ./internal/tooling/task/`

#### Step 2: Test both dirty refusals

- Add a `refuses to merge a dirty worktree` subtest beside the existing
  `refuses on a dirty worktree without --force` (`worktree_test.go:420`), and a
  `refuses to merge into a dirty main checkout` subtest next to it — the second
  one's fixture must return a **clean** `wtPath` and a dirty `mainWorktree`, or
  it proves nothing beyond what the first already proves.
- Assert the call count in both: each refusal must happen **before** any rebase
  or merge is attempted, and a count assertion is what pins that ordering. For
  the main-checkout case the count also pins that the refusal lands after the
  branch guard, not before it.
- Update `TestWorktreeFinish_Merge`'s expected git-call sequence. Its two
  subtests feed `SetExecCommandResults` an **ordered** list consumed one entry
  per git call (`worktree_test.go:631` and `:676`), so each new check inserts a
  `status --porcelain` result at its own position: the `wtPath` check after
  `ListWorktreesAt` and before `DefaultBranchIn`, the main-checkout check after
  `branch --show-current` and before `merge-base --is-ancestor`. Two new calls
  each, so the asserted totals move from 10 → 12 (no rebase,
  `worktree_test.go:667`) and 11 → 13 (with rebase, `:720`). That is the test
  telling the truth, not a broken test — do not loosen the assertions to absorb
  it. Step 3's journal probe moves the same two totals again; expect that and
  update them there too rather than pre-emptively softening them here.
- Verify: `go test ./internal/tooling/task/`

#### Step 3: ADR + the journal gate shared by `--merge` and `--check`

Write the ADR first (§10: an approved fork is not done until the ADR exists).
One record answers one question — **what may block a `--merge`, and how does
`--check` mirror it** — and both of this cycle's forks are decisions under it
(see §4 and §8 Q5 for why they are not split into two ADRs):

- **The journal gate, settled in §8 Q3:** a non-stale open review-journal
  finding blocks a merge; a stale one is reported and does not. Its context is
  ADR-0012 (the journal), ADR-0017 (escalate, don't paper over), and the fact
  that the journal is deleted with the branch — so an open finding reaching
  merge time was never settled. Its rejected alternatives are "block on every
  open finding" and "warn only".
- **The dirty main checkout, settled in §8 Q5:** a merge refuses when the main
  checkout has any uncommitted changes. The ADR must record the full three-way
  fork it came from — refuse on any dirty main checkout / refuse only when the
  dirty paths overlap what the fast-forward writes / report it advisory-only —
  and why each of the two rejected branches was rejected. §8 Q5 carries that
  reasoning; the ADR is where it becomes the durable record, because a later
  reader who wants the narrower rule needs the argument, not just the verdict.

Both decisions share the shape the ADR's Consequences section should state once:
no `--force` escape, and `--check` reports each as a blocking `ready: no` rather
than an advisory line, so the two paths cannot disagree.

Writing the record is not finished until it is **listed in
`docs/decisions/README.md`'s index**. That is step 3 of that file's own "How to
Create an ADR" — copy the template, fill it in, add it to the index, reference it
from related code — and every one of ADR-0001 through ADR-0026 is listed there,
so an unlisted record is one nothing in the repo points at. The index line is the
ADR's number and its one-line title, matching the neighbours' wording. This is
also the ADR the sweep in step 5 will see: `--check` reports it as
`doc-status: docs/decisions/ADR-00NN-….md: PROPOSED`, and flipping it to
`ACCEPTED` is exactly the kind of proposal `finish-work` puts to the maintainer.

Then one probe, used by both paths — no second implementation:

- `openBlockingFindings(mainWorktree, wtPath, branch)` loads the branch's
  journal via `reviewjournal.New(tm.Git).Load(mainWorktree, branch)` — journal
  _location_ resolves through `Manager.PathFor` to the repo's common git dir
  (`manager.go`'s package comment: the main checkout and a linked worktree of
  the same repo share one journal), so `mainWorktree` finds the right file and
  stays consistent with `dropReviewJournal`, which is forced to use the main
  checkout because by the time it runs `wtPath` has already been removed.
  Freshness is a different question and must not reuse that same directory:
  `Manager.Verdict(repoDir, e)` judges an entry by hashing the cited file's
  **current on-disk content in `repoDir`** (`citeBlob` stats and hashes
  `filepath.Join(repoDir, file)`), so it has to run against the checkout that
  actually holds the branch's commits. Passing `mainWorktree` there would hash
  the file as it sits on the default branch — unrelated to what the feature
  branch changed — so a finding citing a file the branch just fixed would
  almost always compare against the old, pre-fix blob and read `stale`,
  silently letting a real, unresolved finding through. So: load the journal via
  `mainWorktree`, but call `jm.Verdict(wtPath, e)` for every open entry and keep
  those that are not `FreshnessStale`. `FreshnessDateless` (a pathless
  design-level question) **does** block: there is nothing mechanical to
  invalidate it, and an unanswered design question is exactly what must not be
  papered over.
- `worktreeFinishMerge` calls it after both step-1 dirty checks and before any
  rebase, and refuses naming the finding ids and the fix: settle them with
  `devgeta task review-note --settle <id>`. Settling as `rejected` with a reason
  is a legitimate exit — the gate demands an answer, not agreement.
- No `--force` escape, same reasoning as the dirty refusals.
- Tests: a blocking fresh finding refuses before any rebase/merge call (assert
  the call count, as in step 2); a stale open finding does not block; a settled
  finding does not block; a pathless open finding blocks; **a regression test
  that opens a finding citing a file the feature branch itself changed (so the
  blob at `wtPath` still matches the stamped blob) and asserts it still
  blocks** — this is the exact case that reads incorrectly stale if `Verdict`
  is ever judged against `mainWorktree` instead of `wtPath`.
- Verify: `go test -run TestWorktreeFinish ./internal/tooling/task/`

#### Step 4: Add `worktree-finish --check`

- New `worktreeFinishCheck(wtPath, branch)` reusing `resolveWorktreeTarget`, the
  same `merge-base --is-ancestor` probe as the merge path (through the shared
  wrapper specified below — not a second copy of the call), and step 3's
  `openBlockingFindings`.
- Read-only. It must not rebase, must not move any HEAD, and — settled in §8
  Q2 — **must not fetch.** The merge path is entirely local: divergence, rebase,
  and `merge --ff-only` all resolve against the local default branch, so a
  fetching `--check` would answer a question `--merge` never asks and the two
  could disagree (§7's top risk). `ahead`/`behind` are therefore against the
  local default branch. `review-scope` remains the deliberate fetch step, and
  `branch-diff`'s documented no-fetch rule is the precedent.
- **What "read-only" covers, precisely.** It means `--check` leaves behind
  nothing a maintainer would have to notice or undo, and asks nothing of the
  network: no ref moved, no HEAD moved, no index or working-tree change, no
  fetch. It does **not** mean "not one byte written under `.git`", and it must
  not be read that way, because the conflict prediction below cannot meet that
  bar — nor can any alternative that answers the same question (measured; see
  the `merge-tree` bullets). `git merge-tree` writes the merge-result tree, and
  a blob for each conflicted file, into the repository's object database. Those
  objects are reachable from no ref, so nothing any git command reports about
  the repository changes, and ordinary maintenance reclaims them: measured on
  git 2.51.1, one conflicted run left exactly two loose objects (the merged tree
  and the conflicted blob) and `git gc --prune=now` removed both. That write is
  inside this contract deliberately; the state changes the constraint exists to
  prevent are the ones named in the bullet above.
- **The divergence probe has to become exit-status-aware before `--check` can
  reuse it.** As written today it is
  `tm.Git.ExecuteCommandAt(wtPath, "merge-base", "--is-ancestor", defaultBranch, "HEAD")`
  (`worktree.go:226`), and `ExecuteCommandAt` (`git.go:189`) folds **every**
  non-zero exit into a `fmt.Errorf`, discarding the exit code (and, when stderr
  is non-empty, not even wrapping with `%w`). Exit 1 is this probe's normal
  answer — "not an ancestor", i.e. a rebase is needed — so through that API it
  is indistinguishable from git failing outright. `--merge` survives the
  collapse because it treats any error as "diverged" and the rebase it then runs
  re-surfaces the real failure with git's own message. `--check` has no second
  step: the same collapse would print `rebase: needed` and go on to report
  `ready: yes` for a worktree where `--merge` cannot run at all.
- **That failure is reachable, not hypothetical.** `Git.DefaultBranchIn`
  (`git.go:641`) resolves the default branch's _name_ from the remote —
  `refs/remotes/origin/HEAD`, then a probe of remote candidates, then the
  literal `"main"` — and never checks that a **local** branch of that name
  exists. In a clone that has only ever had feature branches checked out,
  `merge-base --is-ancestor main HEAD` fails with
  `fatal: Not a valid object name main`; it does not answer "not an ancestor".
- **Command form: a `Git.IsAncestor` wrapper.** Add
  `Git.IsAncestor(dir, ancestor, descendant string) (bool, error)`
  running `git -C <dir> merge-base --is-ancestor <ancestor> <descendant>`, and
  route **both** `worktreeFinishMerge` and `worktreeFinishCheck` through it —
  one probe, one implementation, so the two paths cannot drift apart (§7's top
  risk). The argv is byte-for-byte what `worktree.go:226` already runs, so the
  mocked call sequences at `worktree_test.go:644,693` keep matching; only the
  error branch changes. Nothing new is needed beneath it: it is the same raw
  `*exec.ExitError` from `BaseCommand.ExecCommand`, and the same
  `Git.IsPathIgnored` shape (`git.go:1010`), that the `MergeTreeConflicts`
  bullets below already establish — copy that, do not build a second mechanism.
- **Here the exit code alone _is_ the answer**, the one way this probe is
  simpler than `merge-tree`. Measured on git 2.51.1: is an ancestor → **0**; is
  not → **1**; unresolvable ref, or not a git repository → **128**, with the
  reason on stderr. stdout is empty in all three, so nothing has to be parsed,
  and git-merge-base(1)'s "Errors are signaled by a non-zero status that is not
  1" does hold — unlike git-merge-tree(1)'s equivalent promise (see the table
  below). So: 0 → `true, nil`; 1 → `false, nil`; anything else → `false, err`
  carrying stderr.
- **A probe that fails blocks, unlike a `merge-tree` that fails.** `--check`
  reports `rebase: unknown (<stderr>)` and
  `ready: no — cannot determine whether <branch> needs a rebase against <default>: <stderr>`,
  and `worktreeFinishMerge` refuses with that same reason instead of running a
  rebase whose precondition it could not read. `rebase: needed` on its own is
  never a blocker — `--merge` rebases. This keeps the `ready:` rule below
  exactly as it stands: the blocking set is still precisely what `--merge`
  refuses on, no more. The contrast with `conflicts: unknown` is deliberate, not
  an inconsistency: `merge-tree` is a prediction `--merge` never runs, so
  failing to compute it costs the merge nothing, whereas the divergence probe is
  a step `--merge` actually performs — if it cannot be answered, the merge path
  is already broken and `ready: yes` would be a promise `--check` cannot keep.
- Predict conflicts with `git merge-tree` against the default branch, which
  reads and writes neither the index nor a working tree — its only write is the
  unreferenced objects the read-only bullet above accounts for. **This is
  advisory only, never authoritative:** `git merge-tree` simulates a single
  three-way merge of the branch tip against the default branch tip, but
  `--merge` rebases
  commit-by-commit — replaying each commit individually can conflict even when
  the net three-way merge is clean (branch commits `0→1→2` that each touch a
  line main also changes, while main independently becomes `2`, is the
  canonical case). Actually rebasing to get the true answer is not available to
  `--check`: the read-only constraint above forbids it. So `conflicts:` is
  reported for information only and is excluded from `ready:` — the same
  "advisory, does not block" treatment already given to `journal-stale-open`.
- **Command form.** Run
  `git -C <wtPath> merge-tree --write-tree --name-only <defaultBranch> HEAD`
  through a new `Git.MergeTreeConflicts(dir, a, b string) ([]string, error)` on
  the git wrapper. The new method is not optional bookkeeping: CLAUDE.md §6
  ("Route external tools through their app wrappers") names "a new subcommand"
  as exactly the case where the wrapper is extended rather than reached around,
  and neither existing capture API can carry this call — `RunCapture`
  (`git.go:611`) takes no directory, `ExecuteCommandAt` (`git.go:189`) returns
  no stdout, and **both fold every non-zero exit into a `fmt.Errorf`, discarding
  the exit code** (and, when stderr is non-empty, not even wrapping with `%w`,
  so `errors.As` cannot recover it either).
- **Do not try to avoid the object write by dropping `--write-tree`.** It is not
  an opt-in extra: for the two-commit form it _is_ the mode. git-merge-tree(1)
  documents exactly two, "a modern `--write-tree` mode and a deprecated
  `--trivial-merge` mode", and measured on git 2.51.1 in a freshly-pruned repo,
  `git merge-tree --name-only <a> <b>` with the flag omitted wrote the same two
  loose objects and printed byte-identical output. Naming the flag is therefore
  documentation of what is happening, not the cause of it. Neither alternative
  can carry the `conflicts:` line either:
  - `--quiet` is rejected outright —
    `fatal: options '--quiet' and '--name-only' cannot be used together`, exit
    128 — and by design suppresses the stdout the discriminator below depends
    on. (It is the only form that avoids _most_ of the objects, which is why it
    looks tempting.)
  - `--trivial-merge` genuinely writes nothing, but it is deprecated, requires
    the merge base computed and passed separately, skips rename detection and
    recursive merge-base consolidation, has no `--name-only`, and reports every
    difference rather than every conflict — measured, it listed a cleanly-added
    file as `added in remote`. It would report conflicts `git merge` does not
    have, which is worse than an advisory line that is occasionally optimistic.
- **Nothing new has to be invented beneath that method.** The exit-status-aware
  path already exists end to end and step 4 only has to reuse it:
  `BaseCommand.ExecCommand` returns the raw error from `exec.Cmd.Wait()` —
  i.e. an `*exec.ExitError` on a non-zero exit — alongside trimmed stdout and
  stderr (`internal/commands/base.go:357,370`), and `Git.IsPathIgnored`
  (`git.go:1010`) is the established precedent for a wrapper method that reads
  `exitErr.ExitCode()` to tell an expected non-zero exit from a real failure.
  Copy its shape; the shared executor needs no change.
- **What today's divergence call does is _not_ the precedent to copy.** The
  `merge-base --is-ancestor` call at `worktree.go:226` treats any non-nil error
  as "diverged", which is safe there only because the rebase it then runs
  surfaces a genuine failure on its own — and that is exactly why the bullets
  above replace it with `Git.IsAncestor`. `conflicts:` has no such second step
  either, so both probes must read the exit code rather than the mere presence
  of an error.
- **Exit status alone is not enough to read the result.** Measured on git
  2.51.1 with the command form above:

  | Outcome                                                      | exit      | stdout                                                                                                                            |
  | ------------------------------------------------------------ | --------- | --------------------------------------------------------------------------------------------------------------------------------- |
  | clean merge                                                  | 0         | the merged tree's OID, nothing else                                                                                               |
  | conflicts                                                    | 1         | tree OID, then one conflicted path per line (that is what `--name-only` buys), a blank line, then git's `CONFLICT (…)` messages   |
  | unmergeable input (a ref that does not resolve)              | 1         | **empty**; stderr carries `merge-tree: <ref> - not something we can merge`                                                        |
  | not a repo, or git too old (`--write-tree` needs git ≥ 2.38) | 128 / 129 | empty                                                                                                                             |
  | object database not writable                                 | 128       | empty; stderr carries `error: insufficient permission for adding an object to repository database` then `fatal: failure to merge` |

  So **exit 1 is ambiguous**, and git-merge-tree(1)'s own EXIT STATUS section —
  which promises "something other than 0 or 1" for an error — does not hold for
  an unresolvable ref. The discriminator is the exit code **together with**
  stdout: exit 0 → clean; exit 1 with non-empty stdout → conflicts, whose paths
  are stdout's lines after the first, up to the blank line; anything else (exit
  1 with empty stdout, or any other exit code) → the command failed, and
  stderr is the reason to report. Do not add `--quiet`: alongside `--name-only`
  git rejects it outright (measured above), and on its own it would suppress the
  very stdout that resolves the ambiguity.

- **A failed `merge-tree` reports `conflicts: unknown` and never blocks.** This
  is not a new decision — it follows from `conflicts:` being excluded from
  `ready:` above. A prediction that could not be computed is still only
  advisory, so it must not flip `ready:` and must not fail the command; the
  three shapes of the line are:

  ```
  conflicts: none (advisory, does not block)
  conflicts: 2 (internal/apps/git/git.go, docs/spec.md — advisory, does not block)
  conflicts: unknown (git merge-tree failed: <stderr> — advisory, does not block)
  ```

  `unknown` is a stable sentinel in the same sense `no status marker` is, and
  the parenthesised detail mirrors the `journal-stale-open` line already in the
  draft output below.

  **A failing object write needs no handling of its own.** Measured with
  `.git/objects` made unwritable: exit **128**, empty stdout, the permission
  message on stderr — which is already the "anything else → the command failed"
  case above, so it lands on `conflicts: unknown` and leaves `ready:` untouched
  like every
  other `merge-tree` failure. The exit-128 case in step 4's test list below
  covers it; do not add a second mechanism for it.

- **The `main-checkout:` line carries the branch _and_ its cleanliness, and both
  halves block.** `--merge` runs `git merge --ff-only` in the main checkout
  (`worktree.go:237`), so `--check` has to inspect that directory the same way
  it inspects `wtPath` — reporting only the branch there would leave `--check`
  saying ready for a merge that cannot run (§8 Q5). The line becomes
  `main-checkout: <branch> (clean)` or `main-checkout: <branch> (dirty)`, built
  from the same `Git.IsWorktreeDirty(mainWorktree)` call step 1 adds to the
  merge path — one probe, one implementation, per §7's top risk. A dirty main
  checkout yields
  `ready: no — main checkout <path> has uncommitted changes; commit or stash them there first`,
  word-for-word step 1's refusal so the two paths cannot describe the same state
  differently. It is **blocking, not advisory**: `--merge` refuses on it, and
  `ready:` is exactly `--merge`'s refusal set (below). The worktree's own
  dirtiness keeps its existing `dirty:` line; the main checkout's rides on
  `main-checkout:` instead of a second `dirty:` line, because a repeated key
  would not say which of the two trees it describes.

- Output per `docs/guides/task-design.md`: labeled plain text, payload only, one
  `key: value` per line, a stable sentinel for the clean case. Draft shape:

  ```
  worktree: /path/to/wt
  branch: finish-work-command
  default: main
  dirty: no
  ahead: 3  behind: 0
  main-checkout: main (clean)
  rebase: not needed
  conflicts: none (advisory, does not block)
  journal-open: 0
  journal-stale-open: 1 (n2 — advisory, does not block)
  doc-status: docs/decisions/ADR-0027-....md: PROPOSED
  doc-status: docs/plans/cycles/2026-08-17-....md: Draft
  doc-status: docs/guides/task-design.md: no status marker
  ready: yes
  ```

  `ready: yes` / `ready: no — <first blocking reason>` is the sentinel the slash
  command matches; exit non-zero when `no`.

- **`ready:` is decided only by mechanically-decidable facts:** the worktree
  being dirty, the main checkout's branch, the main checkout being dirty,
  non-stale open findings, and a divergence probe that could not be answered at
  all (the `Git.IsAncestor` bullets above). This is **exactly** the set
  `--merge` refuses on — no more. Predicted conflicts are reported on their own
  `conflicts:` line but never feed `ready:`, because `git merge-tree`'s single
  three-way merge is not the same operation as `--merge`'s per-commit rebase
  and can read clean where the rebase would still conflict (see Step 4 above);
  treating it as authoritative would make `ready: yes` a promise `--check`
  cannot keep.
- **`doc-status:` is data, not a verdict** (settled in §8 Q4). Go emits one line
  per changed markdown file with its status marker **verbatim** (the shapes it
  recognizes are the next bullet's subject), and never
  judges which values are final and never rewrites a file. Judging finality needs
  a status vocabulary, and compiling devgeta's vocabulary into the shipped binary
  is the §12 violation step 5 guards against — so that judgment lives in the
  command's prose. `doc-status:` lines therefore never affect `ready:`.
- **What counts as a status marker: the recognizer is a set of _shapes_, never a
  vocabulary.** `**Status:** <value>` is one Markdown rendering of a status
  label, and it is devgeta's own. Compiling only that rendering into the binary
  would ship a document sweep that fires in this repo and nowhere else — the ADR
  conventions strangers actually use write the same fact differently: MADR 3.x
  puts `status: accepted` in a YAML front-matter block, MADR 2.x writes
  `* Status: accepted`, and Nygard's original template — the most widely copied
  ADR form there is — carries no label at all, only a `## Status` section whose
  body is the value. Under a literal rule every one of those lands on the
  `no status marker` sentinel below and step 5's sweep, the whole reason
  `/finish-work` exists, has nothing to propose. That is principle 8 and §12
  exactly: a general feature that only serves one repo's convention.

  This does not reopen §8 Q4. _Where_ a status lives is syntax; _which_ values
  count as final is a vocabulary, and only the second stays out of the binary.
  Go still emits the value verbatim, never compares it against anything, and
  never rewrites a file; step 5's walk still discovers the vocabulary from the
  repo it is running in.

  Recognize three shapes, in this order, first match wins:

  1. **A `status` key in a leading YAML front-matter block** — a `---` fence on
     line 1 and the `---` that closes it. Parse that block with
     `gopkg.in/yaml.v3`, already a direct dependency
     (`internal/config/fromFile.go`), rather than hand-rolling a scanner; the
     delimiter extraction has an in-repo shape to copy at
     `internal/apps/opencode/permissions_test.go:679`, though it is a test
     helper and so not importable. Top-level `status` key only,
     case-insensitive, scalar values only — a mapping or list value is not a
     status and falls through to the sentinel.
  2. **A label line in the document's header block** — the lines above the
     document's first section heading (the first `##`+ after the H1). The line
     is an optional list marker (`-`, `*`, `+`), optional emphasis (`*` or `_`,
     doubled or not) around the word `status` case-insensitively, a colon, then
     the value. One rule accepts `**Status:** X`, `**Status**: X`, `Status: X`,
     `* Status: X` and `_Status_: X`; it is not a list of literals.

     The header-block limit is load-bearing, not tidiness — three files in this
     repo break without it. `configs/shared/agents/code-reviewer.md:159` carries
     `**Status:** APPROVE | REQUEST CHANGES | NEEDS DISCUSSION` as part of a
     _report template_, and a whole-file scan would put that line in front of the
     maintainer for approval the next time a branch touches the reviewer agents.
     `docs/decisions/ADR-0014-…:344` holds a second `**Status:** ACCEPTED` inside
     an Amendment section, and
     `docs/plans/cycles/2026-07-29-hook-rescope-and-worktree-location.md:402`
     opens a paragraph with the prose sentence
     `**Status: an option to evaluate, not an approved direction.**`. All three
     sit below their file's first `##`; the document's real marker, where it has
     one, sits above it. Checked across every `*.md` under `docs/`: exactly one
     file — `docs/plans/cycles/2026-04-22-worktree-ux-improvements.md:513`, a
     superseded doc whose status rides in a footer — has its only label outside
     the header block, and it gets the sentinel. That is the honest answer,
     since nothing syntactic separates that footer from the three false
     positives above.

  3. **A `Status` section** at any heading level, where the heading text is
     exactly `status` once emphasis and trailing punctuation are stripped —
     `## Status Legend` (`ROADMAP.md:194`) must not match, which is why this is
     an exact test and not a prefix one. The value is the section's first
     non-blank line, and only when that line is plain text: a table row (`|…`),
     a list item, a fence or another heading means the section is a legend
     rather than a status, so no marker. `docs/decisions/README.md:5` is that
     case here — a `## Status` heading over a four-row meaning table — and it
     keeps the `no status marker` line step 3 already assumes for it when this
     cycle adds its ADRs to that index.

  Two rules all three shapes share:

  - **Fenced code blocks are skipped on every scan.** This plan quotes
    `doc-status: …: Draft` inside its own sample output above, and three shipped
    skills quote `**Status:** Approved | Issues Found` inside fences
    (`configs/shared/skills/writing-plans/plan-document-reviewer-prompt.md:40`,
    `…/brainstorming/spec-document-reviewer-prompt.md:40`,
    `…/subagent-driven-development/implementer-prompt.md:127`). A scan that reads
    fences reports an example as a fact.
  - **The value is that one line's remainder, trimmed, and never more.**
    `docs/guides/task-design.md`'s contract is one `key: value` per line, so a
    marker whose sentence wraps is reported truncated at its first line — six
    cycle docs wrap that way today (e.g.
    `docs/plans/cycles/2026-08-12-ws-pane-selection-width-and-refresh-load.md:5`,
    whose value runs on past "…(2026-08-17). Three"). Truncating is the right
    trade against embedding a newline in a line-oriented format, and it costs
    nothing: the value is data for the maintainer's eye, and step 5's flip
    rewrites the document, not this string.

  Anything else is the `no status marker` sentinel below, unchanged. That
  sentinel is where the honest limit sits: a repo that names the field something
  other than `status`, or hides it below its first section heading, is outside
  what the binary recognizes, and step 5's walk picks it up from there.

  **Where the code lives:** a self-contained `statusMarker(content string) string`
  in a new `internal/tooling/task/docstatus.go` with its own `docstatus_test.go`.
  It is a pure string function with no git in it, and `worktree.go` (428 lines)
  is already the file every other part of this step edits.

  **This is a fork, so §10 wants its own ADR** — written before this step's code,
  exactly as step 6's "if step 4 or 5 turns into a further fork, stop and write
  one first" clause says, and listed in `docs/decisions/README.md` like the
  other. It is a separate record from step 3's, which answers a different
  question (what may block a `--merge`). Two alternatives were live and both
  have to be in it: (a) keep the literal `**Status:**` rule and document the
  supported format as devgeta's own — rejected because it makes a _shipped_
  command's headline sweep a no-op in most of the repos it ships to, which
  principle 8 forbids; and (b) drop status parsing from Go entirely, emit only
  the changed markdown paths, and let the command read each file's status with
  the step 5 walk — rejected as the mechanism, because it deletes the
  `doc-status:` line §8 Q4 settled and leaves `--check`'s output unable to say
  what the sweep will act on, but kept as the fallback for shapes the recognizer
  does not know (step 5).

- **A changed markdown file with no status marker still gets a line —
  it is not omitted.** Most markdown in this repo carries no marker at all
  (`README.md`, `CLAUDE.md`, everything under `docs/guides/`); only
  `docs/plans/cycles/*` and `docs/decisions/*` follow a template that has one.
  A branch touching a guide or `README.md` is the common case, not the
  exception, so silently dropping those files would mean the sweep's line
  count no longer matches the changed-file count it was built from, and a
  future doc that adds a marker late would look identical to one the tool
  simply skipped — indistinguishable from a bug. Per
  `docs/guides/task-design.md`'s "stable sentinels" principle (empty results
  always get a sentinel; nothing is left ambiguous to the caller), a file whose
  status appears in none of the recognized shapes is reported as
  `doc-status: <path>: no status marker` — a fixed value that cannot be mistaken for a real one, because it is
  emitted only when the recognizer above matched nothing at all, and no template
  states a status _as_ the words "no status marker". Its safety comes from when
  it is emitted, not from real values being short: they are frequently prose
  (`Done — all nine steps implemented, reviewed, and green (2026-08-17)` is this
  repo's own), which is also why the value is never parsed further.
  Like every other `doc-status:` line, this is data only and never affects
  `ready:`.
- **"Changed" means the same _shape_ `branch-diff` computes — committed, plus
  uncommitted, plus the untracked files ADR-0019 already treats as branch work —
  but anchored on the LOCAL default branch, the same ref the merge path
  resolves against.** `collectWorktreeDiff`
  (`internal/tooling/task/branchdiff.go:71`) cannot be reused as it stands: its
  base is the merge-base of `HEAD` and **`origin/<default>`**
  (`branchdiff.go:73`). That origin anchor is deliberate for `branch-diff` and
  must not be changed — `Git.MergeBase`'s own doc comment (`git.go:273`) records
  why: the range has to equal what GitHub shows for a pull request. `--merge`
  asks a different question and never consults `origin` at all: both the
  divergence probe (`worktree.go:226`) and `merge --ff-only` (`worktree.go:237`)
  name the local `defaultBranch`, and §8 Q2 already settled that everything
  `--check` reports is measured locally for exactly that reason.
- **Why the origin base is wrong here, concretely.** A branch cut from an
  unpushed local default branch has its `origin/<default>` merge-base far behind
  its local one, so an origin-anchored diff attributes every unpushed commit's
  markdown to this worktree. That is this repo's normal state, not an edge case:
  the appendix records the local default branch sitting dozens of commits ahead
  of its remote by design, because §9 squashes unpushed commits at release time.
  Measured on this cycle's own worktree, the origin base lists **8** changed
  `*.md` files where the local base lists **1** — seven of them already merged
  into `main`, i.e. seven docs the sweep would put in front of the maintainer for
  approval that this worktree does not change at all.
- **So: `base = git -C <wtPath> merge-base <defaultBranch> HEAD`** — no
  `origin/`, built with `atDir` (`branchdiff.go:39`) like every other dir-aware
  git call in that file. Then a two-dot `git diff --numstat --no-renames <base>`
  at `wtPath` (not `<base>...HEAD`) so staged and unstaged edits are included
  exactly as ADR-0019 already decided for `branch-diff` — a doc edited but not
  yet committed must still show up, or the sweep would tell the maintainer a
  still-`Draft` doc is clean. Filter that changed-file list to `*.md`, and union
  it with `untrackedFiles(tm.Git, wtPath)` (`branchdiff.go:216`, reused verbatim)
  filtered to `*.md` — a brand-new ADR or plan doc that hasn't been `git add`ed
  yet is exactly the branch work ADR-0019 says must not be silently dropped, and
  skipping it here would contradict that decision. Untracked files are found
  without reference to any base, so that half of the union is identical either
  way.
- **One implementation of the changed-file read, not two** (CLAUDE.md §6, DRY).
  The numstat-plus-`parseNumstat` gather currently inlined in
  `collectWorktreeDiff` (`branchdiff.go:85` and `:121`) moves into a small
  `changedFiles(g, dir, base) ([]fileChange, error)` helper in the same file,
  called by `collectWorktreeDiff` with its `origin/` base (from inside its
  existing goroutine, unchanged) and by `doc-status` with the local base above.
  The two callers then differ in the one thing that is genuinely different
  between them: the base. Do **not** reach for `tm.mergeBase` (`scope.go:168`) or
  `tm.aheadBehind` (`scope.go:173`) — both hard-code `origin/` **and** take no
  directory, so both are wrong for this command twice over. That is also the trap
  to avoid when implementing the `ahead:`/`behind:` line, whose counts §8 Q2
  settled as against the local default branch.
- A path from the `*.md` union may no longer exist in the working tree —
  deleted by a commit on the branch,
  or deleted uncommitted — and a deleted file has no working-tree content to
  read a status marker from, so check the path exists at `wtPath` before
  opening it and emit no `doc-status:` line for one that doesn't. For every
  remaining path, hand its own working-tree content (not the blob at `base`) to
  `statusMarker`, since a doc can be edited more than once
  before the sweep runs and only the latest text is the one to report.
- Tests for `main-checkout:`, mocked like every other case here — the fixture
  varies `wtPath` and `mainWorktree` independently, since a single shared dirty
  answer would let a bug that probes the wrong directory pass:
  - both clean → `main-checkout: <branch> (clean)` and `ready: yes` when
    nothing else blocks;
  - `wtPath` clean, main checkout dirty → `main-checkout: <branch> (dirty)`,
    `ready: no` naming the main checkout's path, and a non-zero exit;
  - `wtPath` dirty, main checkout clean → `ready: no` for the **worktree**, so
    the two reasons are not interchangeable;
  - main checkout on a non-default branch **and** dirty → still one
    `ready: no — <first blocking reason>`, pinning that the reasons are ordered
    rather than concatenated.
- Tests for `doc-status:`: a markdown file changed by an uncommitted edit is
  listed with its live status; a markdown file changed only in an already-made
  commit on the branch is listed; a markdown file present on the branch but
  identical to the local default branch is not listed; **a markdown file changed
  only by a commit that is already on the local default branch while that branch
  sits ahead of `origin/<default>` is NOT listed** — the case an
  `origin/`-anchored base gets wrong, so the fixture has to put unpushed commits
  on the local default branch for the assertion to mean anything; a non-markdown
  file changed by
  the branch produces no `doc-status:` line; a brand-new markdown file that is
  untracked (never `git add`ed) is listed with its live status; a markdown
  file deleted by the branch (in a commit or uncommitted) produces no
  `doc-status:` line and no read error; a changed markdown file with no
  status marker at all (e.g. a `docs/guides/` edit or `README.md`) is
  still listed, with `no status marker` as the value.
- Tests for the marker recognizer live beside it in `docstatus_test.go` and are
  pure string cases — no git, no fixture files, no mocks needed. One per
  recognized shape: `**Status:** Draft`, `**Status**: Draft`, `Status: Draft`
  and `* Status: Draft` all yield `Draft`, which is what pins the rule as one
  shape-match rather than a list of literals; a leading front-matter block with
  `status: accepted` yields `accepted`, **and wins over a header-block label
  line when a document has both**, fixing the precedence in a test rather than
  in a comment; a Nygard-shaped document whose only marker is a `## Status`
  section yields that section's first line. Then one per rejected shape, each
  taken from a real file named in the recognizer bullet so the test says what it
  is protecting: `## Status Legend` over prose yields nothing (`ROADMAP.md`'s
  shape); a `## Status` heading over a `|`-table yields nothing
  (`docs/decisions/README.md`'s); a `**Status:**` line that appears only below
  the first section heading yields nothing (`code-reviewer.md`'s report
  template, ADR-0014's amendment); a `**Status:**` line inside a fenced block
  yields nothing; a value that wraps onto the next line comes back as its first
  line only; and a document with none of the shapes yields the empty result the
  `no status marker` sentinel is built from.
- Tests for `rebase:` and the shared `Git.IsAncestor` probe, injected through
  the same `commands.ExecCommandResult` + `exitError(t, code)` machinery the
  `conflicts:` cases use below, one per measured exit status:
  - exit 0 → `rebase: not needed`, and `--merge` runs no rebase;
  - exit 1 → `rebase: needed`, `ready: yes` still holds when nothing else
    blocks, and `--merge` does run the rebase;
  - **exit 128 with a stderr reason → `rebase: unknown (<stderr>)`,
    `ready: no`, and a non-zero exit; on the merge path the same status refuses
    before any rebase or merge call — assert the call count, as steps 2 and 3
    do.** Without this case the suite passes against an implementation that
    reads a missing local default branch as "needs a rebase" and then reports
    `ready: yes` for a worktree `--merge` cannot touch.
- Tests for `conflicts:`, one per row of the exit-status table above — all four,
  mocked, never a real `git` (the regression scenario at the end of this bullet
  is the manual counterpart, run by hand per scenario 7). Inject each through
  `commands.ExecCommandResult(stdout, stderr, err)`, using the existing
  `exitError(t, code)` helper (`internal/apps/git/git_test.go:1395`, already
  copied in `internal/tooling/worktree/move_test.go:63`) to build a **real**
  `*exec.ExitError` so the wrapper's exit-code branch is exercised against the
  same error shape `ExecCommand` really returns:
  - exit 0 with a lone tree OID → `conflicts: none`, `ready:` unaffected;
  - exit 1 with non-empty stdout → the conflicted paths appear on the
    `conflicts:` line and `ready: yes` still holds when nothing else blocks;
  - **exit 1 with _empty_ stdout and a stderr reason → `conflicts: unknown`,
    `ready:` unaffected, and the command still exits 0.** This case is the one
    that pins the whole mechanism: it fails identically to the conflict case on
    exit code alone, so a test suite without it would pass against an
    implementation that reports a broken ref, or a git older than 2.38, as
    "conflicts detected".
  - exit 128 → also `conflicts: unknown`, proving the branch keys off more than
    the literal value 1.

  Regression: construct a worktree where the branch's commits `0→1→2` each
  touch a line the default branch also changes, but the default branch
  independently ends up at the same content (`2`) — the three-way merge
  `git merge-tree` runs is clean, so `conflicts: none`, while a real rebase
  replaying `0`, then `1`, against the moved default branch conflicts on the
  intermediate step. Assert `--check` still reports `conflicts: none` /
  `ready: yes` on that worktree (advisory, not authoritative) and that
  `--merge` on the same worktree hits the rebase conflict and stops, matching
  scenario 7 in the manual plan below.

- `--check` is mutually exclusive with `--merge` and `--discard`; the existing
  "exactly one of" guard at `worktree.go:105` becomes a three-way check.
- Verify: `go test -run TestWorktreeFinishCheck ./internal/tooling/task/` then,
  because this step also touches `internal/apps/git`, that package plus its
  direct importers (the list `go list` gives per CLAUDE.md §6, "Which tests to
  run"):

  ```bash
  go test ./internal/tooling/task/ ./cmd/ ./internal/tui/worktree/ \
    ./internal/apps/git/ ./internal/apps/registry/ \
    ./internal/tooling/reviewjournal/ ./internal/tooling/terminal/ \
    ./internal/tooling/worktree/
  ```

#### Step 5: Write `configs/shared/commands/finish-work.md`

Sequence the command drives:

1. `devgeta task review-scope` to orient, then `worktree-finish --check`
2. For each `doc-status:` line, decide whether the marker is final (see the first
   constraint below). For each non-final one: propose the flip, show the
   maintainer the line, get an explicit yes, apply it
3. Verify — build, lint, and the **targeted** tests for what changed
4. Commit the doc edits (`git add` and `git commit` as two separate commands —
   the pre-commit secret hook can only scan what is already staged)
5. `devgeta task worktree-finish --merge`
6. Report: what landed, that **nothing was pushed**, and that the worktree is
   gone. Compute the ahead count in `mainWorktree`, not `wtPath` — step 5 has
   already removed the worktree, and `mainWorktree` is where the merge just
   landed. First check whether the default branch has a configured upstream
   (`git -C <mainWorktree> rev-parse --abbrev-ref <defaultBranch>@{upstream}`;
   `@{upstream}` resolves whatever remote the branch actually tracks, so this
   does not hardcode `origin` the way `internal/tooling/task/scope.go`'s
   `aheadBehind` does). If that resolves, report the local branch as that many
   commits ahead
   (`git -C <mainWorktree> rev-list --count <defaultBranch>@{upstream}..<defaultBranch>`).
   A repo need not have any remote at all for `--merge` to have already
   succeeded — `Git.DefaultBranchIn` (`internal/apps/git/git.go:641`) falls
   back to a literal `main` when no origin exists, so `worktree-finish` never
   required one — so when the upstream lookup fails, report the ahead count as
   **unknown, with the reason** ("no upstream configured") instead of a number.
   This is prose the command writes to the human, not a `key: value` line of
   `--check`'s own machine-oriented output (step 4), so it must not be worded as
   `ahead: unknown` — that would misread as the same field as `--check`'s
   `ahead:`/`behind:` line, which is a different comparison (local branch
   against the local default branch, §8 Q2), not this one (default branch
   against its own remote). The convention it borrows is only the _shape_ of
   `--check`'s `conflicts: unknown` and `rebase: unknown` sentinels: a stable,
   named "cannot answer" value instead of a guess or a crash. Either way this is
   reporting, not gating: the merge in step 5 already succeeded, and an
   unanswerable ahead count must never turn into a reported failure or a
   non-zero exit for the command.

If `--check` reports `ready: no`, the command stops at the reason and says what
would clear it — it never routes around a block. An open journal finding is
cleared by settling it (`review-note --settle`), which the command may propose
but must not decide alone.

**Authorization for steps 4 and 5, stated in the file's own prose.** Command
frontmatter is ignored by both agents (CLAUDE.md, "Keeping the two AI agents in
sync"), so the file must say this itself, in the same voice every other shipped
command already uses for an action that lands without a second prompt
(`address-feedback.md`, `create-pr.md`, `review-loop.md`): running `/finish-work`
**is** the authorization to commit the doc edits and to run
`worktree-finish --merge` — do not ask the user to confirm either one, and do
not stop at "shall I commit/merge?". This covers only steps 4 and 5. It does not
reach step 2: each document's status flip still needs its own explicit yes, and
that per-doc gate is not satisfied by the top-level invocation. And it does not
imply a push — `worktree-finish --merge` never pushes, and the report in step 6
says so — so nothing this authorizes leaves the machine, and a merge landed
locally is one `git reset` on the default branch away from undone if it turns
out to be wrong, unlike `address-feedback.md`'s own push, which the house style
already authorizes the same way for something less reversible than this.

**Two hard constraints, because this file ships to strangers' repos (§12):**

- **Do not hardcode `PROPOSED → ACCEPTED` or `Draft → Done`.** Those are
  devgeta's vocabulary, which is why `--check` hands over raw markers and no
  verdict (§8 Q4). The command **discovers** the vocabulary from whatever repo it
  is running in — and a sibling-only lookup is not enough, because a document's
  template usually sits _above_ it, not beside it. Both shapes exist in this very
  repo: an ADR (`docs/decisions/ADR-0027-….md`) does find
  `docs/decisions/TEMPLATE.md` beside it, but the cycle doc you are reading
  (`docs/plans/cycles/2026-08-17-….md`) has no template in
  `docs/plans/cycles/` at all — its template is one directory up, at
  `docs/plans/TEMPLATE.md`. A sibling-only rule would therefore drop every cycle
  doc into the ask path and the sweep would never fire on one of the two document
  types it exists for. So the rule is a **walk**, not a lookup. Try these sources
  in order and stop at the first that yields a vocabulary:

  1. **The nearest template at or above the document.** Start in the document's
     own directory and walk up to the repository root, taking the first directory
     that holds a template for documents of that kind — a file whose name marks
     it as one (`TEMPLATE.md`, `template.md`, `_template.md`,
     `0000-template.md`, and the like) — and read the marker's allowed values off
     that template's own status line. That resolves at step 0 for an ADR
     (`docs/decisions/TEMPLATE.md` spells
     `PROPOSED | ACCEPTED | SUPERSEDED by ADR-YYYY | DEPRECATED`) and at step 1
     for a cycle doc (`docs/plans/TEMPLATE.md` spells
     `Draft | In Progress | Complete | Blocked`).
  2. **A README or index encountered on that same walk**, where it documents what
     the statuses mean. `docs/decisions/README.md`'s Status table is this repo's
     instance; a status legend sitting next to a set of documents is a common
     shape in repos that keep RFCs or ADRs.
  3. **The values the document's siblings already use** for that same marker —
     the vocabulary a directory of RFCs, ADRs, or plans demonstrates by example
     when it ships no template and no legend.

  The walk stops at the repository root and never leaves it. Where none of the
  three yields a vocabulary — this repo's `docs/guides/` is the example: no
  template, no legend, and no status marker in any shape to begin with — the
  command
  asks rather than guesses, exactly as before. Discovery only supplies the
  candidate values; which of them counts as _final_ is still settled by the
  maintainer's explicit yes on the proposed flip, never inferred from the order
  the template happens to list them in.

- **A `no status marker` line is not always the end of the story.** `--check`
  recognizes three marker shapes (step 4): a front-matter `status` key, a label
  line in the document's header block, and a `Status` section. A repo can still
  keep its status somewhere else — a field it names differently, a marker below
  the first section heading — and every one of those arrives as the
  `no status marker` sentinel. When the walk above finds a template for that
  document's kind and the template's _own_ status line is in a shape `--check`
  did not report, read the document the way that template writes it and propose
  the flip from there. This is a fallback, not the mechanism: `doc-status:` is
  still where the sweep gets its list of documents, and this is the only place
  the command reads a status itself. Never invent a marker the template does not
  have — a changed file with no status concept at all (a guide, a README) is not
  a document-approval subject and is left alone.

- **Do not encode devgeta's test policy.** "Targeted tests" must be expressed
  as _the repo's own convention for which tests to run_, discovered from the
  repo, not as devgeta's `go list` recipe.

- Verify: `go test .` (the embedded-config tests — this is the one step that
  needs the slow root-package run), plus
  `dg configure claude --force && dg configure opencode --force` and one live
  end-to-end run on a throwaway worktree.

#### Step 6: Document

- `docs/spec.md`: the `--check` flag and its sentinel, and the three new
  `--merge` refusals (dirty worktree, dirty main checkout, non-stale open
  finding).
- Decide whether `finish-work` belongs in CLAUDE.md's command table.
- Both ADRs this cycle needs are written before the step that depends on them —
  the merge-refusal record in step 3, the status-marker recognizer record in
  step 4 — not here. The second one is this bullet's own clause firing: step 4's
  marker recognition turned out to be a fork with two defensible designs (a
  shape-matching recognizer versus the literal `**Status:**` rule with a
  narrowed, documented scope), and §10 is explicit that an approved design
  choice is not done until the ADR exists. Everything else in this cycle closes
  gaps in an existing design rather than choosing between approaches; if step 4
  or 5 throws up a further fork, stop and write a record for that one too.

---

## 6. Verification Plan

### Automated

```bash
# Steps 1-4 — changed package plus its direct importers
go test ./internal/tooling/task/ ./cmd/ ./internal/tui/worktree/

# Step 5 only — embedded configs live in the root package
go test .

make lint
```

### Manual

1. `devgeta task worktree-finish --check` in a clean, non-diverged worktree →
   `ready: yes`
2. Same with an uncommitted edit **in the worktree** → `ready: no` naming the
   worktree, exit non-zero (the main checkout's own dirtiness is scenario 12)
3. Same with a fresh open journal finding → `ready: no`, naming the finding
4. Same after editing the finding's cited file so it goes stale → `ready: yes`,
   with the stale finding still reported on its advisory line
5. `--merge` on a dirty worktree → refuses, **and** the default branch has not
   moved (this is the regression that matters; check
   `git rev-parse main` before and after)
6. `--merge` with a fresh open finding → refuses, default branch unmoved; then
   `review-note --settle` it and `--merge` succeeds
7. `--merge` on a diverged, conflicting branch → the existing rebase-conflict
   message, worktree left inspectable
8. `/finish-work` end to end on a throwaway worktree with one `PROPOSED` doc →
   doc flipped after an explicit yes, tests run, merge landed, nothing pushed
9. `--check` makes no network call — run it with the network off (or watch that
   no `fetch` reaches git) and confirm identical output
10. A worktree crafted so the branch's commits `0→1→2` each touch a line the
    default branch also changes, but the default branch independently arrives
    at the same content → `--check` reports `conflicts: none` and `ready: yes`
    (the three-way merge is clean), then `--merge` on that same worktree still
    hits a rebase conflict and stops, same as scenario 7 — proving `conflicts:`
    is advisory, not a guarantee
11. `--check` in this very repo, whose local default branch is dozens of commits
    ahead of `origin/main` by design: the `doc-status:` lines must name only the
    markdown this worktree changes, not the markdown of the unpushed commits
    already on `main`. Cross-check by hand with
    `git diff --name-only $(git merge-base HEAD main) -- '*.md'` — the two lists
    must match, and the `origin/main` variant of that command must be longer
12. A **clean** worktree whose main checkout has an unrelated uncommitted edit:
    `--check` reports `main-checkout: main (dirty)` and `ready: no` naming the
    main checkout, exit non-zero; then `--merge` refuses **and the default
    branch has not moved** — `git rev-parse main` in the main checkout is
    identical before and after, the same regression shape as scenario 5. Repeat
    with the dirty file being one the branch also touches and one it does not:
    both must refuse identically, which is what "any dirtiness, not path
    overlap" (§8 Q5) means in practice. Then commit or stash in the main
    checkout and confirm `--merge` succeeds.
13. `--check` against a throwaway repo that is **not** this one, holding three
    documents: one with a YAML front-matter `status:` key, one Nygard-shaped ADR
    whose only marker is a `## Status` section, and one whose `**Status:**` line
    sits below the first section heading. The first two must report their values;
    the third must report `no status marker`. This is the scenario that proves
    the sweep is not devgeta-shaped — the whole point of step 4's recognizer —
    and it cannot be shown from inside this repo, whose documents all use one
    spelling.

### Regression

- `devgeta task worktree-finish --merge` on a clean worktree, from a clean main
  checkout, with no open findings still merges and removes exactly as before
- `--discard` and `--discard --force` unchanged
- `dg wt create` / `list` / `rm` unaffected

---

## 7. Risks & Trade-offs

| Risk                                                                                                                                   | Likelihood | Mitigation                                                                                                                                                                                                                                                                |
| -------------------------------------------------------------------------------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--check` and `--merge` disagree about readiness                                                                                       | Med        | Both call the same probes and the same local comparison base; no second implementation of the divergence or dirty test                                                                                                                                                    |
| The command drifts into devgeta-specific policy                                                                                        | High       | The two constraints in step 5; review the file against §12 before committing                                                                                                                                                                                              |
| `git merge-tree`'s single three-way merge misses a conflict a per-commit rebase would hit (or behaves differently across git versions) | Med        | `conflicts:` is advisory only and excluded from `ready:`; the authoritative answer is still the rebase in `--merge`                                                                                                                                                       |
| Auto-flipping a doc status hides a real unresolved decision                                                                            | Med        | Never flip without an explicit per-doc yes; the command proposes, the human approves                                                                                                                                                                                      |
| The recognizer reads a status-shaped line that is not the document's status (a report template, an amendment, a quoted example)        | Med        | The shapes are constrained rather than greedy — header block only, fenced blocks skipped, `Status` heading matched exactly — and every rejected shape has a test drawn from a real file in this repo; a false positive still cannot flip anything without the per-doc yes |
| Inserting a dirty check breaks the merge sequence tests                                                                                | High       | Expected — step 2 updates them deliberately rather than loosening the assertions                                                                                                                                                                                          |
| Refusing on **any** dirty main checkout blocks merges the fast-forward would never have touched                                        | Med        | Accepted cost, not mitigated away: the user commits or stashes once. See the trade-off below and §8 Q5                                                                                                                                                                    |
| A stale journal entry stops a merge                                                                                                    | Low        | Resolved by design: stale open findings are advisory only (§8 Q3); `Manager.Verdict` decides it                                                                                                                                                                           |
| A pathless open question blocks with nothing to go stale                                                                               | Med        | Intended — it is cleared by answering it, one `review-note --settle` call                                                                                                                                                                                                 |

### Trade-offs

- **`--check` as a flag on `worktree-finish`, not a new subcommand.** It reuses
  target resolution and keeps one surface for "the end of a worktree's life".
  The cost is a three-way mutually-exclusive flag set, which is uglier than two
  booleans.
- **The doc sweep lives in prose, not in Go.** Judging which docs need flipping
  and to what wording is judgment, and encoding devgeta's status vocabulary in
  a shipped binary path would violate §12. The cost is that the sweep is only
  as reliable as the agent following the command.
- **No `--force` for merging dirty.** Deliberately no escape hatch, for either
  checkout; see step 1.
- **The dirty-main-checkout refusal over-refuses, and that is the cost we
  chose.** Most uncommitted work in a main checkout does not touch the paths a
  fast-forward writes, so most of the merges this blocks would in fact have
  succeeded. The user pays a commit or a stash for each one. We accept that
  rather than detecting path overlap, which would mean predicting which paths
  the fast-forward writes and intersecting them with the dirty set — fragile
  logic, guarding a rare case, in exchange for permitting a merge that leaves
  unrelated uncommitted work sitting on top of the commits it just landed.
  Stated plainly so a later reader can reopen it with the real numbers rather
  than rediscover the argument: §8 Q5 and the ADR hold the full fork.
- **`--check` reports more than `--merge` refuses on.** Neither `doc-status:`
  nor the advisory `conflicts:` prediction affects `ready:` — `ready:` is
  exactly the set `--merge` refuses on, no superset. The asymmetry is the
  point: `--merge` refuses what would make the operation wrong, `--check`
  answers "am I ready" using the same refusals plus extra informational
  lines, and the
  judgment in between lives in the command's prose.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope actually locked?
- [ ] Steps actionable (5-15 min each)?
- [ ] Verification executable?
- [ ] Risks realistic?

**Reviewer notes:**

The three open questions are settled (2026-08-17, maintainer), plus a fourth
found while settling them and a fifth raised by the cross-model review of this
document.

1. **Name: `finish-work`.** It reads closest to the maintainer's own phrasing,
   does not imply a push, and fits the verb-object convention every other file in
   `configs/shared/commands/` already follows (`address-feedback`, `approve-pr`,
   `create-pr`, `review-pr`, `smart-commit`, `explain-simply`). `/land` and
   `/wrap-up` were considered and rejected on both counts.
2. **`--check` does not fetch.** The decisive evidence is that
   `worktreeFinishMerge` never fetches either: the divergence probe, the rebase,
   and `merge --ff-only` all resolve against the **local** default branch. A
   fetching `--check` would answer a question `--merge` never asks, so the two
   could report different verdicts on the same worktree — §7's top risk.
   `ahead`/`behind` are against the local default branch. `review-scope` stays the
   deliberate fetch step; `branch-diff`'s documented no-fetch rule is the
   precedent.
3. **An open journal finding is a hard block, and staleness decides which ones.**
   A non-stale open finding blocks both `--check` (`ready: no`) and `--merge`; a
   stale one is reported on an advisory line and blocks nothing. That is the
   strong reading of ADR-0012 and ADR-0017 without the failure mode the risk table
   named: a stale entry's cited file has already changed, so blocking on it would
   stop a merge over code that no longer exists. `Manager.Verdict` already draws
   that line, so no new staleness logic is written. `FreshnessDateless` — a
   pathless design-level question — blocks: nothing can invalidate it mechanically,
   and an unanswered design question is precisely what must not be papered over.
   The exit is `review-note --settle`, and settling as `rejected` with a reason
   counts: the gate demands an answer, not agreement.
   - **Two consequences:** this adds a merge-path refusal that §4's original scope
     did not have (recorded there now), and it is a fork with lasting impact on
     what `--merge` refuses — so §10 requires an ADR, written in step 3 before the
     code. That ADR also carries Q5's dirty-main-checkout fork; see Q5 for why
     the two are recorded together.
4. **`doc-status:` is data, not a verdict.** The original `docs-pending` line was
   self-contradictory: it asked Go to list docs "whose status marker is not final"
   while also deferring the definition of "final" to the command's prose. Go
   cannot do both. Resolved in favour of Go emitting each changed markdown file's
   `**Status:**` marker verbatim, never judging finality and never rewriting a
   file, with those lines excluded from `ready:`. Judging finality needs a status
   vocabulary, and compiling devgeta's vocabulary into a shipped binary is exactly
   the §12 violation step 5's first constraint guards against.
   - **What the marker itself _is_ was left open here**, and reading it as the
     literal `**Status:**` would have put a different devgeta convention in the
     binary — the review of this document caught that. Step 4 now specifies the
     recognizer as a set of marker _shapes_ (front matter, header-block label,
     `Status` section) and requires its own ADR. That refines this answer
     without reopening it: the shape/vocabulary split is exactly the line Q4
     drew, and Go still emits the value verbatim and judges nothing.
5. **A dirty main checkout blocks a merge, and any dirtiness counts.** The
   review caught that this plan checked dirtiness only at `wtPath` while
   `--merge` runs `git merge --ff-only` in the main checkout
   (`worktree.go:237`), a directory whose state is entirely independent of the
   worktree's. Confirmed against the code: the main checkout is resolved at
   `worktree.go:205` and only its branch is examined
   (`CurrentBranchIn` at `:210`, the guard at `:214`) — nothing reads its
   working tree. So a `ready: yes` worktree can still fail at the last step, or
   land its commits and leave unrelated uncommitted work on top of them (§1,
   gap 1). Settled: `--merge` refuses, `--check` reports it as a blocking
   `ready: no` beside the dirty-worktree, main-checkout-branch and
   non-stale-open-finding blockers, and there is no `--force` escape —
   consistent with this plan's other two merge refusals.
   - **The fork was three-way** — refuse on any dirty main checkout / refuse
     only when the dirty paths overlap what the fast-forward writes / report it
     advisory-only — and **any-dirty-main won for three reasons**, recorded
     here because the verdict alone would not let anyone reopen it:
     1. `Git.IsWorktreeDirty` (`git.go:1074`) already exists and already backs
        the `wtPath` refusal, so this reuses the same probe against a second
        directory rather than inventing one (CLAUDE.md §6, prefer existing over
        new).
     2. Over-refusing is cheap for the user — one commit or stash — whereas
        path-overlap detection means predicting which paths an `--ff-only`
        merge will touch and intersecting them with the dirty set: fragile
        logic guarding a rare case.
     3. Advisory-only was ruled out because it knowingly leaves `--check`
        saying ready while `--merge` fails — precisely the check/merge
        disagreement §7 names as this cycle's top risk.
   - **Two consequences:** this adds a third merge-path refusal §4's original
     scope did not have (recorded there now), and it is a second fork with
     lasting impact on what `--merge` refuses. §10 therefore requires it in an
     ADR. It goes in the **same** ADR as Q3's journal gate rather than its own:
     the two answer one question — what may block a `--merge`, and how does
     `--check` mirror it — share the rejected warn-only/advisory-only shape and
     the same reason for rejecting it, and share the no-`--force` consequence.
     Split across two records, a later change to either would have to remember
     the other exists in order to keep `ready:` equal to `--merge`'s refusal
     set, which is the invariant both decisions serve. The ADR must carry both
     forks and both sets of rejected alternatives in full.

### Approved at sign-off (2026-08-17, maintainer)

Four items were added to this plan by review agents rather than by the
maintainer's own decision, and were flagged for an explicit accept-or-overrule.
All four were approved when the plan was approved:

1. **`internal/apps/git/git.go` + its test** — `MergeTreeConflicts` and the
   exit-status-aware `IsAncestor`. §6-mandated plumbing for `--check`'s
   `conflicts:` and `rebase:` lines, not new product scope.
2. **`internal/tooling/task/branchdiff.go` + its test** — extracting
   `changedFiles(g, dir, base)` so `doc-status` and `collectWorktreeDiff` share
   one gather across two bases. §6 DRY, not new product scope.
3. **A second ADR** for the status-marker recognizer fork (the §12 question of
   how a marker is recognized without putting a vocabulary in the shipped
   binary). Separate from the merge-refusal ADR because it answers an unrelated
   question.
4. **A third file on this branch** — `configs/shared/commands/review-loop.md`.
   Two real contradictions in its narrowing protocol were found while reviewing
   this plan and fixed here rather than deferred: a failed restore, and a key
   changed mid-round, each of which left `review.reviewers` narrowed while the
   file simultaneously claimed later rounds ran the full list — so the mandatory
   all-reviewer confirming round would have silently run a subset and could have
   returned a clean approval the dropped reviewers never gave. Unrelated to
   `finish-work`; recorded here because it is on the same branch.

---

## Appendix — Adjacent findings, out of scope

Recorded so they are not rediscovered. None are part of this cycle.

1. **`dg wt create` has no `--base`.** It goes through
   `Git.CreateWorktreeIn` (`internal/apps/git/git.go:684`), which bases a new
   branch on `origin/<default>` and falls back to `HEAD` only when there is no
   origin. `devgeta task worktree-start` _does_ take `--base`. So when a local
   default branch is ahead of its remote — this repo is currently 44 commits
   ahead, by design, because §9 squashes unpushed commits at release time — a
   new `dg wt create` worktree is cut from a base that is missing all of it.
   The workaround this cycle's own worktree used: pre-create the local branch,
   which `createWorktreeIn` then adopts (and `syncExistingBranch` correctly
   no-ops on, since there is no remote counterpart).
2. **`dg wt repair` has no `--prompt`.** `create` can deliver an opening prompt
   as a launch argument; `repair` takes only `--ai` and `--layout`
   (`cmd/worktree.go:491`). So a worktree that exists but has lost its window
   cannot be brought back with its prompt.
3. **`devgeta task worktree-start` hardcodes the shared worktree base path**
   (`taskWorktreePath`, `worktree.go:24`) while the layout is a configurable
   setting (ADR-0010) — this repo is configured in-repo, at
   `<repo>/.claude/worktrees/`. The doc comment claims `worktree-start` puts
   worktrees where `dg wt` looks; under the in-repo layout that appears not to
   hold. Worth confirming before trusting either command to see the other's
   worktrees.
