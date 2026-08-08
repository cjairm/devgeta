# Cycle: PR review loop — watch a PR's review button and run the cross-model review until approval

**Date:** 2026-08-06
**Estimated Duration:** ~8 hours (after the sibling cycle's review-run lands)
**Status:** Approved — awaiting implementation

---

## 1. Domain Context

Reviewing someone else's PR is a two-step flow today, and both steps are manual:

1. Run the reviewer agent(s) (`code-reviewer` / `document-reviewer` /
   `skill-reviewer`) to get findings and a verdict — the same **cross-model** review
   applied to local branches: each configured model (`review.reviewers`, e.g. GPT +
   Gemini) runs the reviewer agent headless through OpenCode, because different
   vendors have non-overlapping blind spots.
2. When every run says `APPROVE`, run `/approve-pr`; otherwise run `/review-pr` to
   post the findings as one cohesive review.

The human is also the trigger: they notice GitHub's "review requested" state, start
the flow, and — after posting — must notice a later **re-request** (the author fixed
things and pressed the re-review button) to run it again. This cycle automates the
trigger and the repetition: **watch one PR, run the unchanged two-step cross-model
review each time GitHub says a review is wanted from this user, and stop once this
user's standing review is an approval.**

This cycle **builds on** the local review loop
([2026-08-05-review-loop.md](2026-08-05-review-loop.md)) rather than duplicating it.
That cycle ships the cross-model machinery — `ReviewConfig` (`review.reviewers`), the
OpenCode headless-run wrapper method, and `dg task review-run` (sequential per-model
runs, five-outcome verdict parsing, `--reviewer` agent selection). This cycle extends
`review-run` to review an explicit range under an explicit journal key, and adds the
GitHub trigger around it. What the sibling does that this one never does: fix the
code. This loop reviews and posts; the author (or the sibling loop, on their side)
does the fixing.

**Sequencing: implementation waits for the sibling cycle's Steps 2–4** (config,
wrapper, `review-run`) — the same way that cycle waited for
[2026-08-05-shared-command-permissions.md](2026-08-05-shared-command-permissions.md)
(now Done). Only Step 4b (the `review-run` range mode) is gated; the ADRs, the two
GitHub-side commands, the revision-aware journal mode, and the posting-command
clause (Steps 1–3, 4a, 4c) have no dependency and can land first.

The trigger design leans on one fact, verified live (§ Step 0): **GitHub's
`reviewRequests` field is already the state machine this loop needs.** A user appears
in it exactly when the review button was pressed for them and they have not answered;
submitting any review removes them; the re-request button puts them back. Polling that
one field gives trigger, dedup, and re-trigger with no local event log — per CLAUDE.md
§6, we build on the existing source of truth instead of creating a parallel one.

## 2. Engineer Context

- **Relevant files and their purposes:**
  - [2026-08-05-review-loop.md](2026-08-05-review-loop.md) — the sibling cycle;
    its Step 3 (OpenCode wrapper headless run) and Step 4 (`review-run`: reviewer
    list resolution, sequential runs, `APPROVE`/`REQUEST CHANGES`/`NEEDS
DISCUSSION`/`NO VERDICT`/`ERROR` outcomes, `--reviewer` agent choice) are the
    machinery this cycle reuses and extends
  - `internal/tooling/task/pr.go` — `PRManager`; `resolveOwnerRepoPR` (line 44) is
    the single owner/repo/PR resolver behind every PR task command — the new
    commands join this family and reuse it
  - `internal/tooling/task/reviewpackage.go` — `ReviewPackage(base, head, file)`;
    **verifies refs with `rev-parse --verify` and never fetches** (calls `verifyRef`
    at lines 42/45; `verifyRef` itself, with the `rev-parse --verify` literal, is
    lines 57-66), and builds an endpoint range `base + ".." + head` (line 49). Both
    facts drive Step 2
  - `internal/tooling/task/scope.go` — `ReviewScope`'s bounded best-effort fetch
    (`tm.Git.FetchOriginTimeout`, line 89) is the read-only-fetch pattern to follow
  - `internal/apps/git/git.go` — git wrapper; `FetchOriginTimeout` (line 232) is the
    existing fetch entry point the PR-ref fetch extends
  - `internal/tooling/task/reviewnotes.go` — `journalBranch(branch)` (line 38):
    an explicit `--branch` overrides the current branch as the journal key. Both
    `review-notes` (read) and `review-note` (write) already expose that flag
  - `internal/tooling/reviewjournal/encoding.go` — `EncodeBranch` percent-encodes
    every non-`[A-Za-z0-9._-]` byte, so a PR-scoped key is safe as a filename and
    cannot escape the review directory
  - `internal/tooling/reviewjournal/manager.go` — `stamp` (func decl at line 175,
    spanning 169-190): cite validation is `os.Stat` (line 177) on the **working
    tree** and the blob comes from `HashObjectIn` (line 180) on the checkout file;
    head stamp is the checkout's `HEAD` (`ShortHeadIn`, line 186). This is what
    Step 4a's revision mode replaces for PR reviews
  - `internal/tooling/worktree/` — `BuiltinReviewerChoices`, the reviewer-agent
    registry `review-run --reviewer` validates against (code, document, skill)
  - `internal/tooling/terminal/dev_tools/githubcli/githubcli.go` — the gh app
    wrapper; per CLAUDE.md §6 every `gh` invocation goes through it
  - `configs/shared/commands/review-pr.md`, `approve-pr.md` — the posting step.
    `approve-pr.md` line 117: it **declines** and posts at most a comment when a
    real gate blocks — it does not always submit an approval
  - `internal/apps/opencode/permissions_test.go` — command-schema guard test (globs
    `configs/shared/commands/*.md`, so it covers the new file automatically)

- **Key facts already verified:** see Step 0.

- **Testing:** [docs/guides/testing-patterns.md](../../guides/testing-patterns.md) —
  `testutil.MockApp`, `VerifyNoRealCommands`, no real `gh`/`git`/`opencode` in any
  test. The `mockPRRunner` pattern in `cmd/task_test.go` covers the new subcommands.

- **Test commands:**

  ```bash
  go test ./internal/tooling/task/ ./internal/apps/git/ ./internal/tooling/terminal/dev_tools/githubcli/ ./cmd/
  go test ./internal/apps/opencode/
  go test ./...
  make lint
  ```

## 3. Objective

`/pr-review-loop [pr-number] [reviewer-types]`, driven on an interval, watches one PR
— resolved from the current branch when no number is passed, the family's existing
resolution — and, each time GitHub reports a review is requested from the
authenticated user, runs the cross-model review (`review-run`: every configured
model, under each selected reviewer agent) against the PR's **current** code over a
merge-base-correct range, then posts through the existing second step (`/approve-pr`
when every run approves, `/review-pr` otherwise), and goes idle. The loop ends in
exactly one of three terminal states, never silently:

1. **Approved** — the tick posted the approval (the loop stops the moment
   `/approve-pr` succeeds, first trigger included — it never keeps listening past an
   approval), or a state read shows the standing review is an approval with no new
   request pending.
2. **Closed** — the PR was merged or closed.
3. **Escalated** — any reviewer run ended `ERROR`/`NO VERDICT`, `/approve-pr` still
   declined after the one approve-only re-ask (below), or a tick could not complete.
   The loop stops and names why.

State 3 exists so a process failure can never be retried into an endless
review-and-comment cycle, and never counts as approval — the same
no-third-undocumented-outcome rule as the sibling cycle.

## 4. Scope Boundary

### In Scope

- [ ] ADR: trigger and driver — poll GitHub's `reviewRequests` state rather than
      webhooks/events; idempotent tick + external driver rather than a stateful
      watcher; personal-request-only filter (write before implementation, §11)
- [ ] ADR: review target for a PR that isn't checked out — read-only fetch of
      `refs/pull/<n>/head`, merge-base-correct range, immutable SHAs; the PR-scoped
      journal key; and `review-run`'s explicit-range mode (write before
      implementation, §11)
- [x] gh wrapper method: `AuthenticatedLogin()` (`gh api user --jq .login`) — the
      account the request must name. The PR side needed no new wrapper method: the
      existing `PRView(prNumber, fields...)` already selects fields, so the state read
      is `PRView(pr, "state", "isDraft", "reviewRequests", "reviews")` and the field
      choice lives beside its parsing in the task package (the `prBaseBranch`
      precedent from Step 2). **Corrected against this bullet as written:**
      `baseRefName`, `headRefName` and the fork owner are NOT queried — nothing in the
      three-line contract or the §5 table consumes them, `pr-review-target` already
      owns base/head resolution, and requesting them here would be unconsumed data
      plus a second spelling of a fact that already has one
- [x] git wrapper method: bounded read-only fetch of a PR ref (`refs/pull/<n>/head`)
      and a base ref, plus merge-base resolution
- [x] `dg task pr-review-state [--pr N]` — one tick's state read (task-design output)
- [x] `dg task pr-review-target [--pr N]` — fetch, then print the immutable
      `base:`/`head:` SHAs (merge-base and PR head), the journal key, and the
      noise-filtered changed-file list. This output is **the review target**: the one
      context every later step reads — reviewer runs, journal stamps, type
      selection, finding verification, and posting all key off it, never off the
      working tree
- [ ] Ref cleanup on a terminal tick: the tick command deletes
      `refs/devgeta/pr/<n>/head` and `.../base` when the loop reaches any terminal
      state (approved, closed, or escalated), mirroring how `review-notes --prune`
      cleans journals. The refs must survive the tick that created them — later
      steps read them and they pin the objects against a concurrent `git gc` — so
      the delete belongs to whoever owns the loop's end, not to `pr-review-target`
- [ ] Revision-aware journal mode: extend `reviewjournal.Manager` so stamping and
      freshness resolve against a given commit (`<rev>:<path>` blob lookup) instead
      of the working tree, exposed as `--rev <sha>` on `review-note`/`review-notes`
- [ ] Extend `review-run` (once the sibling lands it) with explicit-range mode:
      `--base <sha> --head <sha> --journal <key> --report-dir <dir>` — reviews that
      range instead of current-branch-vs-default, journals under the key **at the
      head revision**, persists each run's full report into the dir, and skips the
      default-branch refusal (which guards current-branch semantics this mode
      doesn't use). `--reviewer` stays as the sibling defined it and is what selects
      code/document/skill
- [ ] Target-aware posting: `review-pr.md` and `approve-pr.md` gain a clause — when
      the invocation supplies an explicit review target, every "read the cited
      file" verification uses `git show <head-sha>:<path>`, never the working tree,
      and the final submit passes the reviewed SHA as the review's commit anchor
- [ ] `--commit <sha>` on `dg task submit-review` and `dg task approve-pr` — plumbed
      to the REST review payload's `commit_id`, so the posted review is attributed
      to the SHA that was actually reviewed (the approve path routes through the
      existing `CreateReview` REST call when `--commit` is given)
- [ ] Target-aware reviewer agents: the three
      `configs/shared/agents/*-reviewer.md` files gain a scoped-journal clause —
      when the launch prompt supplies a journal key and revision, every
      `review-notes`/`review-note` call carries `--branch <key> --rev <sha>` — plus
      a guard test asserting the clause is present in all three
- [ ] Reviewer-type selection: the loop takes explicit types (`code`, `doc`,
      `skill`), repeatable — each selected type runs through `review-run` (so each
      type × each configured model is one reviewer run, all sequential). Omitted →
      the agent judges the type(s) from the diff, the default decided earlier
- [ ] `configs/shared/commands/pr-review-loop.md` — the agent-side tick: the §5
      decision table, then the two-step flow when triggered. Written **without** any
      dead frontmatter key, and **with** a standing-authorization section (running
      the command authorizes the whole unattended watch, posting included)
- [ ] `--note <text>` on the loop, forwarded verbatim to every `review-run` of every
      tick — the same emphasis-not-narrowing contract `/review-loop --note` has
- [ ] Guard tests for the new command in `internal/apps/opencode/permissions_test.go`,
      mirroring the `/review-loop` family: unattended authorization present, types
      forwarded as `--reviewer`, `--note` forwarded
- [ ] Docs: `docs/spec.md` feature entry, command documented

### Explicitly Out of Scope

- **Multi-PR inbox watching.** `gh search prs "user-review-requested:<login>"
--state=open` works (verified 2026-08-06) and is the future entry point for
  "watch everything assigned to me" — but it returns a months-deep stale backlog
  (dependabot PRs from April), so it needs an allowlist/baseline policy of its own.
  One PR per loop is v1 (its number inferred from the current branch when omitted).
- **Fixing the PR.** Author's job (`/address-feedback`), or the sibling loop's on
  their side. This loop reads and posts reviews only.
- **Rounds.** `review.rounds` belongs to the sibling's fix-and-re-review cycle. Here
  a "round" is externally triggered — the author's re-request — so the loop needs no
  round cap of its own; GitHub's button is the round counter.
- **`--repo` cross-repo targeting.** The loop runs from a local checkout of the
  target repo (any branch), because the fetch and merge-base in Step 2 need a git
  repo with an `origin` pointing at it. When a fully repo-less mode is wanted, the
  recorded path is one optional repo override inside `resolveOwnerRepoPR`, which
  upgrades every PR task command at once.
- **A daemon or scheduled driver.** The tick is driven by Claude Code's `/loop`
  (decided); it dies with the session, accepted. OpenCode has no loop equivalent —
  there the command is run manually per tick (accepted difference, same class as
  the lint feedback loop in CLAUDE.md's sync table).
- **Reviewing drafts.** A draft never triggers, even when requested (§5 table).
- **Auto-checkout / worktree creation for the target repo.** Precondition, not
  behavior.

**Scope is locked.**

## 5. Implementation Plan

### File Changes

| Action | File Path                                                            | Description                                           |
| ------ | -------------------------------------------------------------------- | ----------------------------------------------------- |
| Create | `docs/decisions/ADR-00NN-*.md` ×2                                    | Trigger/driver; review target (numbers at write time) |
| Modify | `internal/tooling/terminal/dev_tools/githubcli/githubcli.go` (+test) | Review-request state query for one PR                 |
| Modify | `internal/apps/git/git.go` (+test)                                   | Bounded PR-ref fetch + merge-base                     |
| Create | `internal/tooling/task/prreviewstate.go` (+test)                     | State probe                                           |
| Create | `internal/tooling/task/prreviewtarget.go` (+test)                    | Fetch + immutable SHAs + journal key + file list      |
| Modify | `internal/tooling/reviewjournal/manager.go` (+test)                  | Revision-aware stamping and freshness                 |
| Modify | `internal/tooling/task/reviewnotes.go` (+test)                       | `--rev <sha>` on `review-note`/`review-notes`         |
| Modify | `internal/tooling/task/reviewrun.go` (+test)                         | Range mode (`--base/--head/--journal/--report-dir`)   |
| Modify | `cmd/task_pr.go`, `cmd/task.go` (+test)                              | Register subcommands and flags; extend `prRunner`     |
| Modify | `internal/tooling/task/pr.go` + `githubcli.go` (+tests)              | `--commit <sha>` → REST `commit_id` on submit/approve |
| Modify | `configs/shared/commands/review-pr.md`, `approve-pr.md`              | Target-aware file verification + commit anchor        |
| Modify | `configs/shared/agents/*-reviewer.md` ×3                             | Scoped-journal clause (`--branch`/`--rev`)            |
| Modify | `internal/apps/opencode/permissions_test.go`                         | Guard: scoped-journal clause present in all three     |
| Create | `configs/shared/commands/pr-review-loop.md`                          | The tick command (both agents, per sync rule)         |
| Modify | `docs/spec.md`                                                       | Feature entry                                         |
| Modify | `docs/plans/cycles/2026-08-06-pr-review-loop.md`                     | Check off steps                                       |

### Step 0: Probes — DONE (2026-08-06, real `gh`, account `cjairm`)

- [x] **`reviewRequests` is per-user, direct, and current-state.**
      `gh pr view 213 --repo Employ-Inc/employ-agent --json reviewRequests,headRefOid,state,isDraft`
      returns the requested logins; only the named users match, so "requested from me
      and not someone else" is a field read, not a filter to build.
- [x] **Submitting a review clears the requester from the list.** Five PRs the user
      had reviewed (`gh search prs "reviewed-by:cjairm" --state=open`) all show the
      user absent from `reviewRequests` while other pending reviewers remain listed.
- [x] **A comment-only review clears it too.** PR #213's timeline: Copilot
      `review_requested` 18:17:19, submitted a `commented` review 18:20:49, and is
      no longer in `reviewRequests`. No review type leaves a user stuck in the list,
      so "posted anything" always de-triggers.
- [x] **Re-request re-adds** (GitHub-documented re-review semantics; the timeline
      API — `gh api repos/O/R/issues/N/timeline`, filtering
      `review_requested`/`reviewed` events — is the verified fallback if an exact
      event-ordering answer is ever needed; the state read is cheaper and equivalent).
- [x] **No SHA guard.** An earlier draft gated re-review on a new `headRefOid`; wrong —
      a re-request without a push (author replied to threads only) must still
      re-trigger. Presence in `reviewRequests` is the entire trigger.
- [x] **Authenticated login** is `gh api user --jq .login`; the user's latest review
      state per PR comes from the reviews list (`APPROVED` / `CHANGES_REQUESTED` /
      `COMMENTED`), verified against real PRs.
- [x] **`review-package` does not fetch and does not use a merge base.**
      `reviewpackage.go:42-66` — `verifyRef` (called at lines 42/45, its
      `rev-parse --verify` literal at line 62) verifies both refs, then
      `base + ".." + head` (line 49) builds the range. For `git diff` that is an
      endpoint comparison, so a base
      branch that advanced after the PR opened injects unrelated reverse-changes into
      the diff. Step 2 exists because of this; the command itself is left unchanged.
- [x] **The journal key is overridable.** `reviewnotes.go:38` `journalBranch` honors
      an explicit `--branch` on both `review-notes` and `review-note`, and
      `encoding.go` percent-encodes every non-`[A-Za-z0-9._-]` byte, so a PR-scoped
      key containing `/` is a legal, collision-free, escape-proof filename.
- [x] **`/approve-pr` can decline.** `approve-pr.md:117` — on a real blocking gate it
      does not approve and posts at most a terse comment. So a tick can end with
      nothing submitted and `requested:` still `yes`.
- [x] **Headless OpenCode runs work** — verified by the sibling cycle's Step 0
      (2026-08-05, real binary): `opencode run --agent code-reviewer --format json`
      completes without prompts, the `**Status:**` line is recoverable from the JSON
      events, and failures surface as a parseable error event. Not re-probed here.
- [x] **Journal stamping reads the working tree.** `reviewjournal/manager.go:169-190`
      (`stamp`, func decl at line 175) — it does `os.Stat` on the cited path in the working tree (a missing file
      fails the write) and hashes it via `HashObjectIn`; the head stamp is the
      checkout's current `HEAD`. On a foreign PR reviewed from an unrelated branch,
      the cited file is absent or carries unrelated content — so a PR-scoped journal
      _key_ alone is not enough; stamping and freshness must resolve against the
      reviewed head SHA (`git rev-parse <rev>:<path>` yields the blob directly, no
      hashing needed). This is the revision-aware mode in scope.
- [x] **`review-run`'s output contract drops the reports.** As shipped, its stdout
      is per-model verdict lines **only** — the trailing `open:` line was removed in
      `bea40a9`, so open ids now come from `review-notes` — and journal entries are
      one-line blocking findings. `/review-pr` would have nothing cohesive to post
      (no `[MINOR]`/nit findings, no strengths, no evidence). Range mode must
      persist the full per-run reports; hence `--report-dir`.

### Step 0b: Re-probe against the sibling as shipped — DONE (2026-08-07)

The sibling cycle landed with four changes made after this doc was approved. Each
was checked against this plan; all four are additive here, none reopens a decision:

- [x] **`review-run` gained `--note <text>`** (`reviewrun.go:113`,
      `ReviewRun(reviewer, note string)`; `reviewNoteHeader` frames it as emphasis
      that cannot narrow the review; a blank note is refused, not dropped).
      **Applies:** `/pr-review-loop` takes `--note` and forwards it to every
      `review-run` of every tick — Step 4b and Step 5.
- [x] **A review now covers the branch's working state** (ADR-0019), and
      `review-run`'s third refusal is "no commits ahead AND clean tree"
      (`reviewrun.go:338-408`, `checkBranchHasReviewableChanges`). **Applies:**
      range mode must skip that refusal too,
      and must state that it reviews the immutable SHAs _only_ — Step 4b.
- [x] **Progress is sampled to stderr by default** (`reviewprogress.go`, one
      heartbeat per 30s), full stream behind the existing root `--verbose`, carried
      by `CommandParams.OnStdoutLine`. **Applies:** range mode inherits both — it
      adds no progress mechanism of its own, and `--report-dir` persists from the
      same stdout capture the progress sampler already reads, not a second one.
- [x] **Posting authority now lives in each command's prose**, enforced by
      `TestPostingCommandsDeclareStandingAuthorization` (`## Authority to post`),
      `TestCommittingCommandsDeclareStandingAuthorization`, and
      `TestReviewLoopRunsUnattendedWithoutAsking`. **Applies:** `pr-review-loop.md`
      ships with its own standing-authorization section and guard tests — Step 5.
      Step 4c's `--target` edits must leave `review-pr.md` / `approve-pr.md`'s
      existing `## Authority to post` sections intact.
- [x] **Not adopted: the fresh-subagent-per-round pattern** (`e45ecb0`). It exists
      to keep `/review-loop`'s session small across many fix rounds. Here the loop
      does no fix work, most ticks are a three-line state read, and the watch stops
      at the first approval — so the posting step stays in the main session, where
      the verdicts must be read first-hand anyway. Maintainer decision, 2026-08-07.

### Step 1: ADRs

Write both ADRs (scope list above), get approval. Decisions already made in
discussion, to be recorded not re-litigated:

- **Poll GitHub state, not events.** Webhooks need a public endpoint and repo admin;
  GitHub Actions run in CI where the user's agents and config do not exist. Polling
  one JSON field costs nothing at these rates, and — because submitting clears the
  request and re-requesting restores it — needs no local event log or "already
  reviewed" bookkeeping.
- **Idempotent tick + external driver.** All repetition lives in the harness that
  already has it (`/loop`); the command itself is a single tick, runnable by hand on
  either agent. The alternative (a stateful long-running watcher) buys nothing and
  adds process management plus a second source of truth.
- **Personal request only.** Team-level requests that do not name the user do not
  trigger — the rule the user applies by hand.
- **Immutable SHAs over branch names.** The tick reviews `merge-base(base, head)..head`
  as resolved SHAs, fetched read-only on every triggered tick, so what gets reviewed
  is what GitHub shows and cannot drift mid-review.
- **PR-scoped journal key, not the checkout's branch.** ADR-0012 keys journals to the
  current branch, which is meaningless for a foreign PR and actively harmful (it would
  read another branch's settled decisions as if they applied here, and write this PR's
  findings under that branch's name).
- **Cross-model review reuses `review-run`, extended — never a parallel runner.**
  The sibling's command already owns reviewer-list resolution, sequential headless
  runs, and the five-outcome verdict parse. Bolting a second runner beside it for PR
  ranges would be the §6 DRY defect; instead `review-run` gains an explicit-range
  mode, which also generalizes it (any historical range, not just PRs). Its
  default-branch refusal is skipped only in that mode, because the thing it protects
  (a meaningful current-branch diff and a cleanable journal) is supplied explicitly.

Verify: ADRs merged into `docs/decisions/README.md` index; numbers assigned at write
time against the current highest ADR.

### Step 2: Review target — fetch, merge base, journal key

The gap `review-package` leaves. New `dg task pr-review-target [--pr N]`:

1. Resolve owner/repo/PR via `resolveOwnerRepoPR` (reuse).
2. Read `baseRefName`, `headRefName`, and fork owner from the PR.
3. **Read-only fetch, bounded** (following `ReviewScope`'s `FetchOriginTimeout`
   pattern): `refs/pull/<n>/head` into a local non-branch ref, and the base branch.
   `refs/pull/<n>/head` is served by the upstream repo for fork PRs too, which is why
   it is used instead of the head repository's ref — one code path, forks included.
   Fetching moves no local branch and touches no working tree.
4. Resolve `git merge-base <base> <head>` and print immutable SHAs, plus the range's
   noise-filtered changed-file list (reusing the task package's existing
   `fileChanges`/`partitionExcluded` helpers — no new filter logic).

Output (task-design contract):

```
base: 9f2c1ab8...
head: 2f38a274...
journal: pr/Employ-Inc/employ-agent/213
files:
- internal/tooling/task/pr.go
- docs/spec.md
```

This output is **the review target** — the one immutable context every later step
keys off. `base` is the **merge base**, not the base branch tip, so a two-dot
`base..head` diff yields exactly GitHub's PR diff. `journal` is the PR-scoped key
passed to `review-run --journal` and to `review-notes`/`review-note --branch`, so the
PR's review memory is per-PR, stable across ticks, and never mixed with the checkout
branch's journal. `files:` is what reviewer-type auto-judgment reads (Step 5.3) —
without it the tick would have only SHAs and PR metadata, nothing that says what kind
of change this is.

Tests: fork and non-fork PRs; base advanced after PR creation (merge base ≠ base tip);
fetch failure (report it, do not review a stale ref); file list filtered and present;
`VerifyNoRealCommands` throughout, with the fetch mocked.

Verify: `go test ./internal/tooling/task/ ./internal/apps/git/ ./cmd/`.

### Step 3: `dg task pr-review-state`

- Resolve owner/repo/PR through `resolveOwnerRepoPR`; `--pr` optional exactly like the
  rest of the family.
- Output, one state per line, nothing else:

  ```
  pr: open
  requested: yes
  my-review: none
  ```

  `pr:` is one of `open | draft | merged | closed`; `requested:` is whether the
  authenticated user is currently in `reviewRequests`; `my-review:` is that user's
  latest submitted review (`approved | changes-requested | commented | none`).

- Tests enumerate every row of the §5 decision table, `VerifyNoRealCommands`
  throughout.

Verify: `go test ./internal/tooling/task/ ./internal/tooling/terminal/dev_tools/githubcli/ ./cmd/`.

### Step 4a: Revision-aware journal mode

The journal's stamp and freshness currently resolve against the working tree
(`manager.go:169-190`, `stamp` at line 175: `os.Stat` + `HashObjectIn` on the checkout, head stamp from
the checkout's `HEAD`) — wrong for a foreign PR reviewed from an unrelated branch,
where a cited path is absent or carries unrelated content, so writes fail or the
staleness signal lies. Extend `reviewjournal.Manager` with a revision mode:

- **Stamp at a revision:** the cited path's blob comes from
  `git rev-parse <rev>:<path>` (git resolves the blob directly — no `os.Stat`, no
  hashing); a path missing **at that revision** fails the write, preserving
  ADR-0012 §3's typo guard; the head stamp is `<rev>` itself, not the checkout's
  `HEAD`
- **Freshness at a revision:** an entry is stale when its blob differs from the
  cited path's blob at the _current_ PR head — the next tick passes the new head
  SHA, so staleness means "the PR changed this file since", never "the checkout
  differs"
- Exposed as `--rev <sha>` on `review-note` and `review-notes`; absent → today's
  working-tree behavior, byte-identical

Tests: stamp at rev with the working tree dirty/absent/on another branch; missing
path at rev fails; freshness flips when the file changes between two revs and holds
when it doesn't; no `--rev` → existing behavior (existing tests untouched).

Verify: `go test ./internal/tooling/reviewjournal/ ./internal/tooling/task/ ./cmd/`.

### Step 4b: `review-run` explicit-range mode — after the sibling's Step 4 lands

Extend, don't fork: `--base <sha> --head <sha> --journal <key> --report-dir <dir>`
(the first three required together; mutually exclusive with running on a branch). In
this mode `review-run`:

- Reviews `base..head` (the caller passes the merge base — Step 2's output) instead
  of current-branch-vs-default; the reviewer prompt tells the agent to use
  `devgeta task review-package <base> <head>` and to pass `--branch <key>` and
  `--rev <head>` on every journal call, so entries stamp against the reviewed
  snapshot (Step 4a), never the checkout
- **Persists each run's full report** (the agent's final text — findings of every
  severity, strengths, evidence) to `<report-dir>/<reviewer>-<encoded-model>.md`,
  and adds a `report:` path per verdict line to the compact output. Model ids are
  `provider/model` — a path separator — so the model segment is percent-encoded
  with the existing `reviewjournal.EncodeBranch` helper (already collision-free and
  escape-proof; no second safe-filename encoder). It writes from the stdout the
  progress sampler already captures (`CommandParams.OnStdoutLine`) — no second
  capture path. Without report persistence the reports die with the headless runs:
  the shipped output contract is verdict lines only, and the journal holds one-line
  blocking entries — nothing `/review-pr` could compose a cohesive review from
- Skips **all three** HEAD-dependent refusals (default branch, detached/wrong
  HEAD, and ADR-0019's "no commits ahead AND clean tree" check) — each one guards
  current-branch semantics that explicit-range mode replaces. And unlike branch
  mode's ADR-0019 working-state semantics, range mode reviews the **immutable
  SHAs only** — the working tree is never part of the diff — stated explicitly
  and covered by a test
- Keeps everything else identical: reviewer list from `review.reviewers` (or the
  OpenCode default model when unset), `--reviewer` agent selection validated against
  `BuiltinReviewerChoices`, `--note` (composes with range mode unchanged — the
  note rides the prompt, not the range), sequential runs, the five outcomes, the
  compact per-model output contract, and the sampled stderr progress heartbeats
  (full stream behind the root `--verbose`)

Tests: range mode with/without configured models; flag-pairing validation (partial
`--base` without `--head`/`--journal` errors); journal writes land under the key at
the rev; reports written one per run with model ids containing `/` encoded in the
filename, and their paths printed; range mode runs from a default-branch checkout
(no refusal) and from a detached HEAD; range mode with `--note` carries the note in
the prompt; the range diff never includes working-tree changes. All mocked.

Verify: `go test ./internal/tooling/task/ ./internal/apps/opencode/ ./cmd/`.

### Step 4c: Target-aware posting commands

`review-pr.md` step 5 and `approve-pr.md` step 3 both verify by reading cited files —
from the working tree, which on any-branch operation is unrelated code: findings
would be dropped as "hallucinated" because the checkout lacks the PR's files, stale
threads kept, or an approval based on nothing.

The contract is an explicit argument, not an ambient hint. Both usage lines become:

```
/review-pr  [PR_NUMBER] [--base <merge-base-sha>] [--target <head-sha>]
/approve-pr [PR_NUMBER] [--target <head-sha>]
```

`/review-pr` takes the pair because it reads a diff and a diff needs both ends; its
`review-package <base> <head>` call has no way to derive a merge base from a head.
`/approve-pr` never reads a diff, so it takes `--target` alone. Given `--target`
without `--base`, `/review-pr` stops and says it needs the base — it never guesses a
base and never falls back to the checked-out branch's diff. **`--base` is the merge
base, not the base branch's tip**: a tip-based diff shows commits merged into the
base since the PR opened as if this PR reverted them, the reversed-changes failure
[ADR-0022](../../decisions/ADR-0022-a-pr-review-targets-immutable-shas.md) §2
rejects. `pr-review-target`'s `base:` line already prints a merge base.

(This supersedes the brief's original single-flag `/review-pr … [--target]` form.
That version left the command self-resolving the base with its own
`pr-review-target --pr <n>` call — an extra fetch, and a base resolved separately
from the reviewed head. Every caller already ran `pr-review-target` and already
holds the base, so it passes what it has. Maintainer decision, 2026-08-07.)

With `--target`: first resolve it — `git rev-parse --verify <sha>^{commit}` — and
**stop with an error if it doesn't resolve** (never fall back to the working tree
silently); then every file verification reads `git show <head-sha>:<path>` instead
of the file on disk — same checks, same dedup rules, different byte source; and the
final submit passes `--commit <head-sha>`. Without `--target` (the normal on-branch
use), behavior is word-for-word unchanged.

**The commit anchor.** `dg task submit-review` and `dg task approve-pr` gain
`--commit <sha>`, plumbed to the REST review payload's `commit_id` (`SubmitReview`
already posts through `CreateReview`, `pr.go:207`; the approve path routes through
that same REST call when `--commit` is given, since `gh pr review` cannot carry it).
Honest scoping of what this buys: GitHub does **not** reject a review whose
`commit_id` is older than the head — there is no atomic submit in this API. What it
does buy: the posted review is _attributed_ to the SHA that was actually reviewed —
inline comments anchor to the reviewed diff (marked outdated if the head moved), the
review record names the reviewed commit, and branch protection's
dismiss-stale-approvals keys off it. Combined with the pre-post gate (Step 5.7) the
race window shrinks to seconds, and anything that slips through is visibly stamped
with the old SHA instead of silently claiming the new head.

Verify: read both commands back; the usage line carries `--target`, the clause names
the resolve-or-stop rule, the byte source, and the `--commit` pass-through, and
nothing else changed. Wrapper/task tests cover `--commit` present and absent.

### Step 4d: Target-aware reviewer agents

All three reviewer agent files hardcode unscoped journal commands
(`code-reviewer.md:23,168`, `document-reviewer.md:23,138`,
`skill-reviewer.md:23,118`: `devgeta task review-notes` first, `review-note --open`
to record) — a launch prompt saying "use `--branch`/`--rev`" contradicts the agent's
own written instructions and loses. Add one scoped-journal clause to each file, next
to the existing journal instructions: **when the launch prompt supplies a journal
key and revision, append `--branch <key> --rev <sha>` to every `review-notes` and
`review-note` invocation in this file.** Unprompted runs (the normal branch review)
are unchanged.

Guard test: extend the reviewer-agent guard family in
`internal/apps/opencode/permissions_test.go` to assert all three files carry the
clause, so a future rewrite of one agent file cannot silently drop it back to
checkout-branch journaling.

Verify: `go test ./internal/apps/opencode/`; the guard fails when the clause is
removed from any one file.

### Step 5: `/pr-review-loop` command file

`configs/shared/commands/pr-review-loop.md`. Frontmatter: `description` only.

The file carries a standing-authorization section (per CLAUDE.md's posting/unattended
rule and `requireStandingAuthorization`'s wording contract): running the command IS
the authorization for the whole watch — state reads, fetches, reviewer runs, and the
posting step — and the agent must not ask before any of it. Guard tests in
`internal/apps/opencode/permissions_test.go`, mirroring the `/review-loop` family:
the unattended grant is present (analog of `TestReviewLoopRunsUnattendedWithoutAsking`),
the tick forwards the selected types as `--reviewer` (analog of
`TestReviewLoopForwardsReviewerSelector`), and the tick forwards `--note` (analog of
`TestReviewLoopForwardsTheNote`).

Usage: `/pr-review-loop [pr-number] [code|doc|skill ...] [--note <text>]` — the
`--note` text is the human's own emphasis, forwarded verbatim to every `review-run`
invocation of every tick (same semantics as `/review-loop --note`: extra context,
never a narrowing of the review). The PR number is optional
and resolves from the current branch's PR when omitted (`devgeta task current-pr`,
the same inference every PR command already does); pass it only when watching a PR
whose branch isn't the checkout. The types name which reviewer agents run; more than
one is allowed (`code doc` is two `review-run` invocations, one per type; each run
covers every configured model internally). Types omitted → the agent judges the
type(s) from what the PR changes.

**One invocation = one idempotent tick, and the review runs at most once per tick.**
Repetition belongs to the driver: on Claude Code, `/loop <interval> /pr-review-loop
[n] [types]`; on OpenCode, run per tick by hand.

The tick reads `pr-review-state` once, then takes **exactly one** row of this table.
Rows are evaluated top to bottom; the first match wins:

| `pr:`             | `requested:` | `my-review:`  | Action                                                                               |
| ----------------- | ------------ | ------------- | ------------------------------------------------------------------------------------ |
| `merged`/`closed` | any          | any           | **Terminal: closed.** Report, stop the loop                                          |
| `draft`           | any          | any           | Wait. A draft is unfinished work; a formal review on it is noise even when requested |
| `open`            | `yes`        | any           | **Review** (below)                                                                   |
| `open`            | `no`         | `approved`    | **Terminal: approved.** Report, stop the loop                                        |
| `open`            | `no`         | anything else | Wait — the ball is with the author                                                   |

Draft is checked **before** the request state on purpose: a requested draft waits.

The review action — every step reads the **review target** from step 1; nothing
reads the working tree:

1. `devgeta task pr-review-target --pr <n>` → immutable `base`/`head` SHAs, the
   journal key, and the changed-file list. A fetch failure ends the tick with a
   report — never review a possibly-stale ref.
2. `devgeta task pr-view --pr <n>` for the PR's purpose and linked ticket.
3. Resolve reviewer types: the ones passed to the loop, else judge from the target's
   `files:` list (`code` for code, `doc` for docs, `skill` for agent
   skills/commands; mixed → the matching set).
4. `SCRATCH=$(devgeta task scratch)` — the reports' home for this tick.
5. Per type, run the cross-model review:
   `devgeta task review-run --reviewer <type> --base <base> --head <head> --journal <key> --report-dir "$SCRATCH"`
   (plus `--note <text>` when the loop was given one). Every configured model runs
   the selected agent sequentially; verdicts and one `report:` path per run come
   back in the compact output. `review-run` prints no journal ids — the open ids
   come from `devgeta task review-notes --branch <key> --rev <head>` (step 8),
   exactly as `/review-loop` learns them.
6. Aggregate — the sibling's any-single-blocker rule across **all** runs (every type
   × every model). Any `ERROR` or `NO VERDICT` → **terminal: escalated.** Name the
   failing run; never approve, never auto-retry.
7. **Re-check state and head immediately before posting.** Re-run
   `devgeta task pr-review-state --pr <n>` and `devgeta task pr-review-target --pr <n>`:
   - The state must still land on the **Review** row (`open` + `requested: yes`).
     Anything else means the world changed during the long review — the PR merged,
     closed, or went draft, or another session already answered the request — and
     posting now would be a duplicate or unsolicited review. Take the row the fresh
     state selects (terminal or wait) instead of posting.
   - `head` must equal the reviewed SHA. If it moved (the author pushed
     mid-review), **post nothing** — the reviews describe code the PR no longer is,
     and an approval would cover commits no reviewer saw. End the tick reporting
     "head moved during review"; the still-pending request makes the next tick
     review the new head from scratch. (A fast-pushing author just defers the
     review — the correct outcome, not starvation.)
8. Post — both commands carry the target, and their submits stamp
   `--commit <head>` (Step 4c), so whatever lands on GitHub is attributed to the
   reviewed SHA even if a push races the submission itself:
   - Every run `APPROVE` → `/approve-pr <n> --target <head>` (its file
     verifications read `git show <head>:<path>`).
   - Otherwise → `/review-pr <n> --base <base> --target <head>` (the `base` from
     step 1, so the posted review's diff is the same merge-base range the reviewers
     read) — first read every `report:`
     file plus `devgeta task review-notes --branch <key> --rev <head>`, so the full
     cross-model findings (all severities, strengths, evidence) are in context, not
     just the one-line blocking entries. `/review-pr` dedups against the PR's
     existing threads and posts one review. Journal settles it performs use
     `--branch <key> --rev <head>`.
9. **Parse the second step's outcome.** `/approve-pr` prints
   `## PR #<num> — approved | not approved`:
   - `approved` → **terminal: approved.** The loop stops now — including on the
     first trigger; it never keeps listening past a posted approval.
   - `not approved` → **re-ask once, approve-only.** This branch is reached only
     when every reviewer run said `APPROVE` — that verdict is exactly the basis
     `approve-pr.md` itself names for approving over live comments (its line 67). So
     the tick invokes `/approve-pr` one more time, stating that the cross-model
     verdict in context is `APPROVE` and the expected outcome is an approval with an
     `LGWC; <who/what remains>` body naming the leftover non-blocking comments — not
     a re-review, not a comment. Approval on the re-ask → terminal: approved. Still
     `not approved` → **terminal: escalated** — it is standing on a blocker the
     reviewer runs missed; report what it named and stop. Never a third ask:
     re-asking forever would re-comment forever, because a decline leaves
     `requested:` at `yes`.
     `/review-pr`'s outcome needs no parsing — after it posts, the tick simply keeps
     listening (the author's re-request is the next trigger).
10. **Clean up on every completed exit of the review action** — approved, escalated
    (including `ERROR`/`NO VERDICT` at step 6), head-moved at step 7, review posted,
    or a failed submit: `devgeta task scratch --clean "$SCRATCH"` (idempotent; a
    failed submit prints the review into the tick report before cleaning, per
    `/review-pr`'s own rule). The one exit this cannot cover is the process being
    killed mid-tick — a dead process runs no cleanup; that directory is swept by the
    existing `dg configure --force` scratch sweep, the same recovery every scratch
    user has.

    On the exits that are also **terminal for the loop** — approved, closed, or
    escalated — the same step deletes the target refs `pr-review-target` fetched:
    `git update-ref -d refs/devgeta/pr/<n>/head` and `.../base`. They cannot go
    earlier: every later step of the tick reads them, and holding them is what keeps
    a concurrent `git gc` from collecting the reviewed commits. A non-terminal exit
    keeps them, because the next tick reviews the same PR. A mid-tick kill leaves
    them behind exactly as it leaves a scratch dir, and they are reused per PR
    number, so the leftovers are bounded by distinct PRs reviewed, not by ticks.

11. Report the tick's outcome in ≤3 lines (state read, runs and verdicts, next
    expectation).

The tick never edits code, never resolves threads, never re-requests reviewers, and
never posts more than the one review the two-step flow produces.

Verify: `go test ./internal/apps/opencode/` (schema guard + parity green with the new
file present).

### Step 6: Manual end-to-end

On a real PR in a work repo (from its checkout), with two configured models:

1. Not requested → tick waits, takes no action
2. Draft + requested → tick waits (the row that must not fall through to review)
3. Press the review button → next tick fetches, runs type × model reviewer runs over
   the merge-base range, posts one review
4. Posted review clears `requested:` (verify with `pr-review-state`)
5. Press re-request with no new commits → next tick reviews again
6. `/pr-review-loop <n> code doc` on a mixed PR → both agents run, both under each
   model; one blocking finding from either lens blocks approval
7. A first-trigger review where every run approves → `/approve-pr` posts, tick
   reports approved and stops immediately — the loop never listens past an approval
8. `dg config set review.reviewers bogus/model` → the run reports `ERROR(<reason>)`
   and the tick escalates, never approves
9. All runs `APPROVE` but `/approve-pr` declines over live comments → the one
   approve-only re-ask approves with an `LGWC;` body naming who left them
10. All runs `APPROVE` but `/approve-pr` stands on a real blocker through the
    re-ask → tick reports escalated, stops
11. Push a commit to the PR while a review is running → step 7's head re-check
    reports "head moved during review", posts nothing; the next tick reviews the
    new head
12. Answer the request from another session (or close the PR) while a review is
    running → step 7's state re-check posts nothing and takes the fresh state's row
13. Inspect a posted review on GitHub → it is anchored to the reviewed SHA
    (`commit_id`), and its journal entries stamp the same SHA
14. Dismiss your own approval on the PR → next tick reports `my-review: none`, not
    `approved`, and the loop keeps watching instead of stopping — this is the one
    wire fact the dismissed-approval rule rests on (gh reporting `DISMISSED`
    rather than `APPROVED`), and nothing else here probes it live
15. Merge the PR → tick reports closed, stops

### Step 7: Docs + close out

`docs/spec.md` entry, check off this doc, status → Done. Deploy both agents:
`dg configure claude --force` and `dg configure opencode --force`.

## 6. Verification Plan

### Automated

```bash
go build ./...
go test ./internal/tooling/task/ ./internal/apps/git/ ./internal/tooling/terminal/dev_tools/githubcli/ ./cmd/ ./internal/apps/opencode/
go test ./...
make lint
```

Every row of the §5 decision table has a test; `review-run` range-mode tests cover
flag pairing, journal targeting at the rev, report persistence, and the skipped
refusal; revision-mode journal tests cover stamping and freshness against revs with
the working tree elsewhere entirely.

### Manual

Step 6's fifteen-point live sequence, plus:

1. **Fork PR** — `pr-review-target` resolves head via `refs/pull/<n>/head` and the
   review shows the fork's changes
2. **Diverged base** — push to the base branch after the PR opened, then run a tick:
   the diff must contain only the PR's own changes (this is the merge-base check; the
   naive `base..head` range fails it)
3. `dg task pr-review-state` / `pr-review-target` on a branch with no PR and no
   `--pr` → the family's existing actionable error, unchanged
4. Journal isolation — after a tick, `review-notes` on the checkout branch shows
   nothing new; `review-notes --branch pr/<owner>/<repo>/<n> --rev <head>` shows the
   findings, stamped with the PR head (not the checkout's `HEAD`), citing files that
   don't exist in the working tree
5. **Working-tree independence** — run the whole tick from a checkout on an
   unrelated branch that lacks the PR's files: reviewer findings cite them, journal
   writes succeed, `/review-pr` re-verification reads them via `git show`, nothing
   is dropped as missing
6. Kill the `/loop` session mid-watch → no partial posts, no moved refs; restarting
   resumes from GitHub's state. A mid-tick kill may leave a scratch directory — a
   dead process runs no cleanup — and the existing `dg configure --force` scratch
   sweep removes it (verify it does)

### Regression

- `/review-pr` and `/approve-pr` without a review target — the normal on-branch
  use — word-for-word unchanged in behavior; `review-package` and every existing PR
  task command unchanged
- `review-note`/`review-notes` without `--rev` byte-identical (existing journal
  tests untouched)
- `review-run` branch mode (the sibling's contract) byte-identical when no range
  flags are passed — its tests untouched and green
- Command-schema guard and Claude/OpenCode parity tests green

## 7. Risks & Trade-offs

| Risk                                                     | Likelihood | Mitigation                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| -------------------------------------------------------- | ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Poll misses the moment (interval latency)                | High       | Inherent to polling; a review minutes late is fine. Interval is the driver's choice (`/loop 10m` typical); API budgets are orders of magnitude above this use                                                                                                                                                                                                                                                                                                        |
| Long wall-clock per trigger (types × models, sequential) | High       | Same deliberate trade as the sibling: sequential buys journal-write safety and cross-run dedup; the tick runs unattended, so wall-clock costs no human time                                                                                                                                                                                                                                                                                                          |
| `/approve-pr` declines and the loop retries forever      | Med        | One approve-only re-ask (backed by the all-`APPROVE` verdict, per `approve-pr.md`'s own basis rule), then terminal: escalated. Steps 6.9–6.10 force both paths live                                                                                                                                                                                                                                                                                                  |
| Stale or wrong diff reviewed                             | Med        | `pr-review-target` fetches on every triggered tick and returns immutable SHAs over a merge-base range; a fetch failure aborts the tick instead of reviewing stale                                                                                                                                                                                                                                                                                                    |
| Tick reads the checkout instead of the target somewhere  | Med        | The review target is the single context: journal stamps at `--rev <head>`, posting verifies via `git show <head>:<path>`, type selection reads `files:`; Manual tests 4–5 run the whole flow from a branch that lacks the PR's files                                                                                                                                                                                                                                 |
| Journal cross-contamination with the checkout branch     | Med        | PR-scoped `--journal`/`--branch` key on every journal call; Manual test 4 asserts both directions                                                                                                                                                                                                                                                                                                                                                                    |
| Sibling's `review-run` design shifts under this cycle    | Med        | Sequencing gate: Step 4b here starts only after the sibling's Step 4 merges; the range mode is additive, so drift surfaces as a rebase, not a redesign                                                                                                                                                                                                                                                                                                               |
| `/loop` session dies (reboot, close)                     | Med        | Accepted by design decision; GitHub keeps the state, so restarting the loop loses nothing                                                                                                                                                                                                                                                                                                                                                                            |
| Author pushes while a tick is mid-review                 | Med        | Step 5.7 re-checks state + head immediately before posting and posts nothing on any change; the still-pending request makes the next tick review the new head. The submit itself stamps `--commit <reviewed-sha>` — no atomic submit exists in GitHub's API, so a push racing the final call still lands, but attributed to the old SHA (outdated markers, stale-approval dismissal) instead of silently claiming the new head. Steps 6.11–6.13 force all of it live |
| Author re-requests while a tick is mid-review            | Low        | Next tick sees `requested: yes` again and re-fetches; the pre-post head check above covers the pushed-commits case                                                                                                                                                                                                                                                                                                                                                   |
| PR-scoped journals accumulate                            | Low        | `review-notes --prune` already deletes journals whose branch does not exist locally or on the remote — a `pr/...` key never will, so they prune on request                                                                                                                                                                                                                                                                                                           |
| Fetched `refs/devgeta/pr/<n>/*` accumulate               | Low        | Step 5.10 deletes both refs on every terminal tick (approved/closed/escalated). They are keyed by PR number and reused per tick, so growth tracks distinct PRs reviewed, not ticks; a mid-tick kill leaks one pair, removable with `git update-ref -d`                                                                                                                                                                                                               |
| Team review requests don't name the user                 | Low        | Deliberate: personal request only — the rule the user applies manually                                                                                                                                                                                                                                                                                                                                                                                               |

### Trade-offs Made

- **State read over event log.** `reviewRequests` cannot say _why_ you are in it
  (first request vs re-request) — and does not need to; the action is identical. The
  timeline API is the recorded fallback if event ordering ever matters.
- **One PR per loop.** Loses "watch my whole inbox"; buys zero backlog policy, zero
  repo-mapping config, and a trigger the user can reason about. The number is
  inferred from the current branch's PR when omitted — passed explicitly only when
  the checkout is on some other branch.
- **Extend `review-run`, not a parallel runner.** Waiting on the sibling cycle costs
  sequencing; a second runner would cost a permanent duplicate of reviewer-list
  resolution, verdict parsing, and output contract — the §6 DRY defect. The range
  mode also generalizes `review-run` beyond PRs (any historical range), which is the
  §3.8 direction.
- **Types × models is a matrix, all sequential.** "code doc" under two models is four
  runs. Accepted: the reviewers share one journal (so later runs see earlier
  entries), and the tick is unattended.
- **Stop at approval, not at merge.** Decided: after the user's `APPROVE` stands
  un-re-requested, the watch ends — a later re-request starts a new loop by hand.
  Merged/closed and escalated also terminate, whichever comes first.

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope locked?
- [ ] Steps actionable?
- [ ] Verification executable?
- [ ] Risks realistic?

**Reviewer notes:**

Round 1 (2026-08-06) — REQUEST CHANGES, six findings, all verified against the code
and all fixed:

- `n1` ADR step missing → Step 1 added, two ADRs, ahead of implementation
- `n2` requested drafts fell through to review → §5 decision table, draft row
  evaluated before request state, with a live test and a per-row unit test
- `n3` no fetch, so refs could be stale → Step 2 `pr-review-target` fetches
  `refs/pull/<n>/head` read-only on every triggered tick and returns immutable SHAs
- `n6` endpoint range ≠ GitHub PR diff → Step 2 returns the **merge base** as `base`,
  which makes the two-dot range correct without changing `review-package`; diverged
  base is a manual test
- `n4` journal keyed to the checkout branch → PR-scoped key
  (`pr/<owner>/<repo>/<n>`), verified legal via `EncodeBranch`
- `n5` `/approve-pr` can decline → terminal state 3 (escalated), parsed from its
  output, forced live in the manual plan

Round 2 (2026-08-06) — maintainer direction, superseding two earlier choices: the
review step is the **same cross-model review** used on local branches (reuse
`review-run` + `review.reviewers`, not a single Claude subagent), and reviewer
type(s) are **selectable at launch** (repeatable `code|doc|skill`, auto-judged only
when omitted). This reversed §8 of the previous revision — the `review-run`
convergence marked "speculative" there is now the design — and added the sequencing
gate on the sibling cycle's Steps 2–4 plus the explicit-range mode (Step 4).

Round 3 (2026-08-06) — maintainer walkthrough of the intended flow, three
corrections applied: the PR number is **optional** (inferred from the current
branch's PR, the resolution every PR command already has); a decline from
`/approve-pr` when every reviewer run said `APPROVE` gets **one approve-only
re-ask** (approve with `LGWC;` naming the leftover comments — the all-`APPROVE`
verdict is precisely the basis `approve-pr.md` requires) before escalating, replacing
the immediate escalate-on-decline from round 1's `n5` fix; and the terminal-approved
rule is explicit that the loop **stops the moment an approval posts**, first trigger
included, rather than waiting for the next state read to observe it.

Round 4 (2026-08-06) — REQUEST CHANGES, four findings, all verified against the code
and all fixed by making the immutable review target the single context threaded
through every step (the reviewer's own suggestion):

- `n7` journal stamps read the working tree (`manager.go:169-190`, `stamp` at 175, confirmed:
  `os.Stat` + `HashObjectIn` on the checkout, head stamp from checkout `HEAD`) →
  Step 4a revision-aware journal mode (`--rev <sha>`: blob from
  `git rev-parse <rev>:<path>`, freshness against the current PR head), used by
  every journal call in the flow
- `n8` `/review-pr`/`/approve-pr` verify cited files against the working tree →
  Step 4c target-aware clause: with an explicit target, file verification reads
  `git show <head-sha>:<path>`; without one, behavior word-for-word unchanged
- `n9` `review-run`'s compact output discards the full reports `/review-pr` needs →
  Step 4b `--report-dir`: each run's full report persists to a scratch file, its
  path printed per verdict line; the tick reads them into posting context and
  cleans the scratch on every exit path
- `n10` auto type-selection had no diff to inspect → `pr-review-target` output
  gains the noise-filtered `files:` list (reusing the existing
  `fileChanges`/`partitionExcluded` helpers); type judgment reads it

Round 5 (2026-08-06) — REQUEST CHANGES, four findings, all confirmed and fixed:

- `n11` head never re-checked before posting → tick step 7: re-run
  `pr-review-target` immediately before posting and compare heads; moved → post
  nothing (an approval would cover commits no reviewer saw), report, and let the
  still-pending request drive a fresh review of the new head next tick. New risk
  row + Step 6.11 live test
- `n12` target-aware posting had no invocation contract → Step 4c defines it:
  `--target <head-sha>` in both usage lines, resolved with
  `git rev-parse --verify <sha>^{commit}` and **stop on failure** — never a silent
  working-tree fallback
- `n13` report filenames broke on `provider/model` ids (a path separator) → the
  model segment is percent-encoded with the existing `reviewjournal.EncodeBranch`
  helper (no second safe-filename encoder); tested with `/` in the id
- `n14` scratch cleanup missed the approved and escalated exits, and a manual test
  claimed a killed session leaves no scratch files (impossible — a dead process
  runs no cleanup) → cleanup is now its own step 10 covering every completed exit,
  and the kill test states the truth: a mid-tick kill can leave a scratch dir, which
  the existing `dg configure --force` sweep removes

Round 6 (2026-08-06) — REQUEST CHANGES, three findings; all fixed, one with a
corrected premise:

- `n15` head check and submission aren't atomic → `--commit <sha>` on
  `submit-review`/`approve-pr`, plumbed to REST `commit_id` (`SubmitReview` already
  posts via `CreateReview`; the approve path routes through that call when
  `--commit` is given). **Premise corrected during verification:** GitHub does not
  reject a stale `commit_id` — no atomic submit exists in this API. What `commit_id`
  actually buys is attribution: the review is stamped with the reviewed SHA, inline
  comments anchor to the reviewed diff, and dismiss-stale-approvals keys off it. The
  doc claims exactly that, no more
- `n16` the pre-post gate only checked the head → Step 5.7 re-runs
  `pr-review-state` too and requires the **Review** row (`open` + `requested: yes`)
  to still hold; any other row (merged, closed, draft, answered elsewhere) takes
  that row's action instead of posting. Live test 6.12
- `n17` all three reviewer agent files hardcode unscoped journal commands
  (verified: `review-notes` / `review-note --open` with no `--branch`/`--rev` in
  code/document/skill reviewer files), which a launch prompt cannot reliably
  override → Step 4d adds the scoped-journal clause to each file plus a guard test
  in `permissions_test.go` asserting all three carry it

Round 7 (2026-08-06) — **APPROVE**, no concerns. Maintainer approved the doc the
same day. Suggested order confirmed: ADRs first, then Steps 2–3, 4a, 4c, 4d while
the sibling cycle's `review-run` lands, then 4b and the tick command.

Round 8 (2026-08-07) — pre-implementation re-probe against the sibling **as
shipped**, recorded as Step 0b. The sibling merged with four post-approval changes;
three carry into this cycle and one is deliberately not adopted. `--note` is now in
scope end to end (loop flag → every `review-run`); range mode must skip ADR-0019's
new empty-branch refusal and state that it reviews the immutable SHAs only, never
the working tree; the tick command ships a standing-authorization section with guard
tests, because posting authority now lives in prose rather than in the frontmatter
that was removed. Step 0's report-persistence finding was re-verified and its
premise tightened: `review-run`'s stdout is verdict lines only (the `open:` line is
gone), so the tick reads open ids from `review-notes --branch <key> --rev <head>`.
Not adopted: the fresh-subagent-per-round pattern — this loop does no fix work,
most ticks are a three-line state read, and verdicts must be read first-hand.
The sequencing gate is now satisfied — the sibling's Steps 2–4 are merged, so
Step 4b is unblocked along with the rest.
