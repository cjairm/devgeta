# Cycle: Finish-work — one entry point from unapproved docs to a landed merge

**Date:** 2026-08-17
**Estimated Duration:** ~4 hours
**Status:** Draft — awaiting approval

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

1. **No dirty check on the merge path.** `worktreeFinishMerge` never calls
   `IsWorktreeDirty`. Only `WorktreeStart` (`worktree.go:44`) and
   `worktreeFinishDiscard` (`worktree.go:127`) do; the tests confirm the
   asymmetry (`worktree_test.go:55`, `:421` — nothing equivalent in
   `TestWorktreeFinish_Merge`, `worktree_test.go:613`). Two distinct bad
   outcomes:
   - dirty **and** diverged → `git rebase` fails on the unstaged changes, and
     the error we wrap it in tells the user to _resolve conflicts_ that do not
     exist. Misleading diagnosis.
   - dirty **and** not diverged → the rebase is skipped, the `--ff-only` merge
     **succeeds and moves the default branch**, then `RemoveWorktree` refuses
     (it deliberately never passes `--force`). Result: the commits landed, the
     uncommitted work is stranded in a worktree the message describes as
     "failed to remove". The default branch moved on a half-finished operation.

   This is a defect, not a missing convenience, and CLAUDE.md §4 wants it made
   structurally impossible rather than documented.

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
   unsettled blocking finding can land silently.

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
  source for gap 4
- `configs/shared/commands/` — where the new slash command lands, shipped to
  both agents (`review-loop.md` is the closest precedent in size and shape)

**Key existing behaviors not to re-implement:**

- `resolveWorktreeTarget` — target resolution is solved; the new `--check` path
  must reuse it verbatim, not re-derive it
- The `merge-base --is-ancestor` divergence probe (`worktree.go:226`) — reuse
  the same test so `--check` and `--merge` can never disagree about whether a
  rebase is needed
- `errors.Is(err, git.ErrBranchDeleteFailed)` at `worktree.go:257` — the comment
  there records why substring matching on messages rotted; do not reintroduce it

**Testing patterns:** `testutil.MockApp`, `testutil.VerifyNoRealCommands`, and
never a real `git`. See
[docs/guides/testing-patterns.md](../../guides/testing-patterns.md). The
existing `TestWorktreeFinish_Merge` asserts the exact sequence of git calls, so
inserting a dirty check **will** change its expected call count — that is the
test telling the truth, not a broken test.

**Commands to run tests** (targeted — from the `go list` query in CLAUDE.md §6;
the direct importers of `internal/tooling/task` are `cmd` and
`internal/tui/worktree`):

```bash
go test ./internal/tooling/task/ ./cmd/ ./internal/tui/worktree/
make lint
```

Step 4 adds a file under `configs/shared/commands/`, which the embedded-config
tests in the **root** package cover. That run is ~4.8 minutes on its own, so it
belongs at the end of step 4 only — not after every edit:

```bash
go test .    # only for the configs/ change in step 4
```

---

## 3. Objective

One command the maintainer runs at the end of a piece of work that sweeps
unapproved docs for approval, verifies, lands the branch on the default branch
locally without pushing, and reports the worktree's disposition — backed by a
`worktree-finish` that can answer "am I ready?" without acting, and that refuses
to merge a dirty worktree.

---

## 4. Scope Boundary

### In Scope

- [ ] `worktreeFinishMerge` refuses a dirty worktree up front (gap 1)
- [ ] `devgeta task worktree-finish --check` — a read-only readiness report,
      non-zero exit when blocked (gaps 4 and 5)
- [ ] `configs/shared/commands/finish-work.md` — the single entry point (gaps 2
      and 3)
- [ ] Tests for all three, mocked
- [ ] Docs: `docs/spec.md` for the new flag, `CLAUDE.md`'s command table if the
      new command belongs there

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

| Action | File Path                                | Description                                                   |
| ------ | ---------------------------------------- | ------------------------------------------------------------- |
| Modify | `internal/tooling/task/worktree.go:202`  | Dirty refusal at the top of `worktreeFinishMerge`             |
| Modify | `internal/tooling/task/worktree.go`      | New `worktreeFinishCheck` + readiness formatter               |
| Modify | `cmd/task.go:530,558`                    | `--check` flag, mutually exclusive with `--merge`/`--discard` |
| Modify | `cmd/task.go:48`                         | `TaskManagerInterface.WorktreeFinish` signature gains `check` |
| Modify | `internal/tooling/task/worktree_test.go` | Dirty-merge case; `--check` cases incl. every blocked reason  |
| Modify | `cmd/task_test.go`                       | Flag parsing and exclusivity                                  |
| Create | `configs/shared/commands/finish-work.md` | The slash command                                             |
| Modify | `docs/spec.md`                           | Document `--check`                                            |

### Step-by-Step

#### Step 1: Refuse a dirty worktree on the merge path

- In `worktreeFinishMerge`, before resolving the default branch, call
  `tm.Git.IsWorktreeDirty(wtPath)` and return a refusal naming the fix
  ("commit or stash your changes first") — mirror `WorktreeStart`'s wording at
  `worktree.go:49` so the two refusals read the same.
- No `--force` escape here. `--discard --force` exists because throwing work
  away is a real intent; "merge while dirty" has no legitimate form, since the
  uncommitted half cannot land.
- Verify: `go test -run TestWorktreeFinish ./internal/tooling/task/`

#### Step 2: Test the dirty refusal

- Add a `refuses to merge a dirty worktree` subtest beside the existing
  `refuses on a dirty worktree without --force` (`worktree_test.go:421`).
- Assert the call count too: the refusal must happen **before** any rebase or
  merge is attempted, and a count assertion is what pins that ordering.
- Update `TestWorktreeFinish_Merge`'s expected git-call sequence for the new
  leading dirty check.
- Verify: `go test ./internal/tooling/task/`

#### Step 3: Add `worktree-finish --check`

- New `worktreeFinishCheck(wtPath, branch)` reusing `resolveWorktreeTarget` and
  the same `merge-base --is-ancestor` probe as the merge path.
- Read-only. It must not rebase, must not fetch, and must not move any HEAD.
  Predict conflicts with `git merge-tree` against the default branch, which
  writes nothing to a working tree.
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
  conflicts: none
  journal-open: 0
  docs-pending: docs/decisions/ADR-0027-....md (PROPOSED)
  ready: yes
  ```

  `ready: yes` / `ready: no — <first blocking reason>` is the sentinel the slash
  command matches; exit non-zero when `no`.

- `--check` is mutually exclusive with `--merge` and `--discard`; the existing
  "exactly one of" guard at `worktree.go:105` becomes a three-way check.
- `docs-pending` lists changed docs whose status marker is not final. Deriving
  _which_ values count as final is the generality question — see step 4's note;
  the Go side reports what it sees and never rewrites a file.
- Verify: `go test -run TestWorktreeFinishCheck ./internal/tooling/task/` then
  `go test ./internal/tooling/task/ ./cmd/ ./internal/tui/worktree/`

#### Step 4: Write `configs/shared/commands/finish-work.md`

Sequence the command drives:

1. `devgeta task review-scope` to orient, then `worktree-finish --check`
2. For each `docs-pending` entry: propose the flip, show the maintainer the
   line, get an explicit yes, apply it
3. Verify — build, lint, and the **targeted** tests for what changed
4. Commit the doc edits (`git add` and `git commit` as two separate commands —
   the pre-commit secret hook can only scan what is already staged)
5. `devgeta task worktree-finish --merge`
6. Report: what landed, that the default branch is now N commits ahead of its
   remote, that **nothing was pushed**, and that the worktree is gone

**Two hard constraints, because this file ships to strangers' repos (§12):**

- **Do not hardcode `PROPOSED → ACCEPTED` or `Draft → Done`.** Those are
  devgeta's vocabulary. Have the command read the status vocabulary from the
  doc's own sibling `TEMPLATE.md` — which works here
  (`docs/decisions/TEMPLATE.md` literally spells the enum
  `PROPOSED | ACCEPTED | SUPERSEDED by ADR-YYYY | DEPRECATED`) and generalizes
  to any repo with RFC, ADR, or plan docs. Where no vocabulary can be found,
  the command asks rather than guesses.
- **Do not encode devgeta's test policy.** "Targeted tests" must be expressed
  as _the repo's own convention for which tests to run_, discovered from the
  repo, not as devgeta's `go list` recipe.

- Verify: `go test .` (the embedded-config tests — this is the one step that
  needs the slow root-package run), plus
  `dg configure claude --force && dg configure opencode --force` and one live
  end-to-end run on a throwaway worktree.

#### Step 5: Document

- `docs/spec.md`: the `--check` flag and its sentinel.
- Decide whether `finish-work` belongs in CLAUDE.md's command table.
- No ADR expected: this closes gaps in an existing design rather than choosing
  between competing approaches. If step 3 or 4 turns into a real fork (e.g. the
  status-vocabulary discovery has two defensible designs), stop and write one
  first — §10 is explicit that an approved design choice is not done until the
  ADR exists.

---

## 6. Verification Plan

### Automated

```bash
# Steps 1-3 — changed package plus its direct importers
go test ./internal/tooling/task/ ./cmd/ ./internal/tui/worktree/

# Step 4 only — embedded configs live in the root package
go test .

make lint
```

### Manual

1. `devgeta task worktree-finish --check` in a clean, non-diverged worktree →
   `ready: yes`
2. Same with an uncommitted edit → `ready: no — dirty`, exit non-zero
3. Same with an open journal finding → `ready: no`, naming the finding
4. `--merge` on a dirty worktree → refuses, **and** the default branch has not
   moved (this is the regression that matters; check
   `git rev-parse main` before and after)
5. `--merge` on a diverged, conflicting branch → the existing rebase-conflict
   message, worktree left inspectable
6. `/finish-work` end to end on a throwaway worktree with one `PROPOSED` doc →
   doc flipped after an explicit yes, tests run, merge landed, nothing pushed

### Regression

- `devgeta task worktree-finish --merge` on a clean worktree still merges and
  removes exactly as before
- `--discard` and `--discard --force` unchanged
- `dg wt create` / `list` / `rm` unaffected

---

## 7. Risks & Trade-offs

| Risk                                                        | Likelihood | Mitigation                                                                                       |
| ----------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------ |
| `--check` and `--merge` disagree about readiness            | Med        | Both call the same probes; no second implementation of the divergence or dirty test              |
| The command drifts into devgeta-specific policy             | High       | The two constraints in step 4; review the file against §12 before committing                     |
| `git merge-tree` behaves differently across git versions    | Med        | Treat conflict prediction as advisory; the authoritative answer is still the rebase in `--merge` |
| Auto-flipping a doc status hides a real unresolved decision | Med        | Never flip without an explicit per-doc yes; the command proposes, the human approves             |
| Inserting a dirty check breaks the merge sequence tests     | High       | Expected — step 2 updates them deliberately rather than loosening the assertions                 |

### Trade-offs

- **`--check` as a flag on `worktree-finish`, not a new subcommand.** It reuses
  target resolution and keeps one surface for "the end of a worktree's life".
  The cost is a three-way mutually-exclusive flag set, which is uglier than two
  booleans.
- **The doc sweep lives in prose, not in Go.** Judging which docs need flipping
  and to what wording is judgment, and encoding devgeta's status vocabulary in
  a shipped binary path would violate §12. The cost is that the sweep is only
  as reliable as the agent following the command.
- **No `--force` for merging dirty.** Deliberately no escape hatch; see step 1.

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

Open questions worth settling before step 4:

1. Is `finish-work` the right name? Alternatives considered: `/land`, `/merge-work`,
   `/wrap-up`. `finish-work` reads closest to the maintainer's own phrasing and
   does not imply a push.
2. Should `--check` fetch first? `review-scope` is the deliberate fetch-and-orient
   step, and `branch-diff` deliberately does not re-fetch. `--check` following
   `branch-diff`'s no-fetch rule keeps a session's comparison base stable, which
   argues for no.
3. Should an open journal finding be a hard block or a warning? A hard block is
   the stronger reading of ADR-0012 and ADR-0017 (escalate, don't paper over),
   but it makes a stale journal entry able to stop a merge.

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
