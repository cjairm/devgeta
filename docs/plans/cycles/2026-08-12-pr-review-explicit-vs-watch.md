# Cycle: `/pr-review-loop` explicit vs watch tick — land ADR-0025's split

**Date:** 2026-08-12
**Estimated Duration:** ~3 hours
**Status:** Blocked — gated on
[ADR-0025](../../decisions/ADR-0025-an-invocation-reviews-the-request-gates-only-the-watch.md)
being accepted (it is PROPOSED). Nothing below is implemented.

---

## 1. Domain Context

`/pr-review-loop` watches one pull request's review state and, when a review is requested of
the running account, runs the cross-model reviewer agents over that PR's own diff and posts at
most one review or approval. It was built in
[cycle 2026-08-06-pr-review-loop](2026-08-06-pr-review-loop.md), whose scope is locked and
whose in-scope deliverables are all committed.

That cycle's Step 6 (manual end-to-end) was driven against a real PR on 2026-08-12 and turned
up two design defects plus one parsing defect. Each was the shipped command behaving exactly
as written:

1. `/pr-review-loop --reviewer=document <n>` on an open, unrequested PR printed
   `pr: open / requested: no / my-review: none` and took no action. The human had already
   asked for the review — by typing the command — and had to ask again in prose.
2. Nothing repeated afterwards. The handoff sits at step 0 and a bare invocation is defined as
   a single look, so step 11 only _named_ the `/loop` form, which reads as an instruction to
   go start the watch by hand.
3. `--reviewer=document` is not a spelling step 0 parses (the tick takes bare words; the
   sibling `/review-loop` takes `--reviewer`), so the reviewer type the human named reached
   nothing.

The decision that answers all three is
[ADR-0025](../../decisions/ADR-0025-an-invocation-reviews-the-request-gates-only-the-watch.md):
an explicit invocation reviews now, GitHub's `reviewRequests` field gates only the ticks a
driver fires, and the handoff moves to the end of the tick. It narrows
[ADR-0022](../../decisions/ADR-0022-pr-review-trigger-is-a-polled-state-read.md) §1 and §2.

**Why this is its own cycle.** The change reverses one of the 2026-08-06 cycle's
_Explicitly Out of Scope_ decisions ("Reviewing drafts. A draft never triggers, even when
requested") and adds deliverables that cycle's In Scope list does not carry (`--once`,
`--on-request`, the `--reviewer` flag spelling on the tick, a two-mode decision table). That
cycle's scope is locked, so per [docs/plans/TEMPLATE.md](../TEMPLATE.md) the work is documented
here and referenced there as deferred.

---

## 2. Engineer Context

- **Relevant files and their purposes:**
  - `configs/shared/commands/pr-review-loop.md` — the agent-side tick: usage line, step 0
    argument parsing, the step 2 decision table, step 7's pre-post gate, step 11's next-tick
    report, and the driver handoff. This is where nearly all of the change lands. It ships to
    both AI coders and to every user's other repos, so it may carry no devgeta-specific
    decision (CLAUDE.md §12).
  - `docs/spec.md` — the `/pr-review-loop` narrative and flag list.
  - `internal/apps/opencode/permissions_test.go` — the guard-test family that pins the shipped
    command's prose clauses. Ten tests there read `pr-review-loop.md`, each isolating one
    section by its **exact** heading text, so they are not only additive work: one of them,
    `TestPRReviewLoopStartsTheWatchItPromises`, asserts that the step 0 section contains the
    repeat-driver handoff, which is the sentence this cycle moves. Step 2 lists what has to be
    revised.
  - `docs/decisions/ADR-0025-…md` — the decision this cycle implements; §1 carries the two
    tables, §4 the handoff rule, §5 the flag spellings, §6 the mode-aware gate.
  - `docs/decisions/ADR-0022-…md` — carries two narrowing notes that say "proposed, not yet in
    force"; they flip when the behavior lands.

- **Key facts involved:**
  - No Go code changes outside the guard tests. The flags are parsed by the agent reading the
    command file, and `devgeta task review-run` already takes `--reviewer` exactly as
    ADR-0025 §5 forwards it.
  - The three reviewer types are `worktree.BuiltinReviewerChoices()`'s registry keys, forwarded
    verbatim; `doc` stays an error rather than an alias.
  - Command frontmatter is ignored by both agents, so any authorization must be in prose
    (CLAUDE.md §12).

- **Commands to run tests** (targeted — a `configs/` change is read by the root package's
  embedded-config tests, so that slow package is required here; CLAUDE.md §6):
  ```bash
  go test ./internal/apps/opencode/
  go test ./internal/tooling/task/   # the state read's decision-table test
  go test .                          # root package — embedded configs and command guards
  make lint
  ```

---

## 3. Objective

`/pr-review-loop <n>` typed by a human reviews that PR immediately whatever its request state
(short of merged or closed), posts, and then starts the watch itself; only driver-fired ticks
carrying `--on-request` are gated on GitHub's `reviewRequests` field — with the shipped command
file, `docs/spec.md`, and the guard tests all saying so.

---

## 4. Scope Boundary

### In Scope

- [ ] `configs/shared/commands/pr-review-loop.md`: parse `--once` / `--on-request` /
      `--reviewer[= ]<type>`; the two-mode decision table; the handoff moved out of step 0;
      the mode-aware pre-post gate; step 11's next-tick line
- [ ] `docs/spec.md`: the `/pr-review-loop` narrative — usage line, explicit vs watch tick,
      `--once`, the moved handoff
- [ ] One guard test per clause ADR-0025's Negative section names as able to rot silently
- [ ] ADR-0025 `Status: PROPOSED` → `ACCEPTED`, and ADR-0022's two narrowing notes flipped
      from "proposed, not yet in force" to in force — in the same change that lands the behavior
- [ ] The four end-to-end cases the 2026-08-06 cycle's Step 6 lacks, then a re-run of that
      sequence whole

### Explicitly Out of Scope

- **Any Go behavior change.** `review-run --reviewer`, the task commands, and the journal are
  unchanged; only the guard test file moves.
- **Multi-PR inbox watching**, **fixing the PR**, **`--repo` cross-repo targeting**, and **a
  daemon or scheduled driver** — all still out, for the reasons the 2026-08-06 cycle gives.
- **A friendlier reviewer-type vocabulary.** `doc` stays an error (ADR-0025 §5).
- **The two `dg configure --force` deploys.** They overwrite the maintainer's live agent
  configuration, so they belong after the branch merges, not to a step here.

**Scope is locked.** If you discover something out of scope is needed, document it for a future
cycle and reference here.

---

## 5. Implementation Plan

Landing ADR-0025's prose alone would be worse than landing nothing: the shipped command still
gates every tick on `requested: yes`, its step 0 rejects any word it does not recognize as a
reviewer type, and step 7 re-demands the request before posting — so an explicit review would
either stop at the parse, stop at the table, or run the whole cross-model review and post
nothing.

### File Changes

| Action | File Path                                                                           | Description                                                                                                                                                                                                                       |
| ------ | ----------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Modify | `configs/shared/commands/pr-review-loop.md`                                         | Parse `--once` / `--on-request` / `--reviewer[= ]<type>`; the two-mode table; handoff moved out of step 0; mode-aware step 7; step 11                                                                                             |
| Modify | `docs/spec.md`                                                                      | The `/pr-review-loop` narrative: usage line, explicit vs watch tick, `--once`, the moved handoff                                                                                                                                  |
| Modify | `internal/apps/opencode/permissions_test.go`                                        | Revise the existing PR-loop guards the moved handoff breaks or leaves stale (and any heading anchor whose step is renamed or renumbered), then add one guard per clause ADR-0025's Negative section names as able to rot silently |
| Modify | `internal/tooling/task/prreviewstate_test.go`                                       | Comment and per-row `action` labels only — they cite the single-mode table; no assertion changes                                                                                                                                  |
| Modify | `docs/decisions/ADR-0025-an-invocation-reviews-the-request-gates-only-the-watch.md` | `Status: PROPOSED` → `ACCEPTED` once approved                                                                                                                                                                                     |
| Modify | `docs/decisions/ADR-0022-pr-review-trigger-is-a-polled-state-read.md`               | Flip its two narrowing notes from "proposed, not yet in force" to in force, in the same change that lands the behavior                                                                                                            |
| Modify | `docs/plans/cycles/2026-08-06-pr-review-loop.md`                                    | Step 6's revised sequence and that cycle's close-out                                                                                                                                                                              |
| Modify | `docs/plans/cycles/2026-08-12-pr-review-explicit-vs-watch.md`                       | The check-offs here                                                                                                                                                                                                               |

No Go code outside those two test files: the flags are parsed by the agent reading the
command file, and `review-run` already takes `--reviewer` exactly as ADR-0025 §5 forwards it.

### Step-by-Step

#### Step 1: The command file

- [ ] Usage line gains the new spellings:
      `/pr-review-loop [PR_NUMBER] [code|document|skill ...] [--reviewer <type>] [--note <text>] [--once] [--on-request]`
- [ ] Step 0 accepts `--reviewer <type>` and `--reviewer=<type>` as a second spelling of a
      bare word — same three values, same verbatim forwarding to
      `review-run --reviewer`, still no `doc` alias (ADR-0025 §5)
- [ ] Step 0 recognizes `--once` and `--on-request` as flags rather than reviewer types.
      Without this the existing "anything else: stop before reading any state" rule is what
      swallows them, which is how `--reviewer=document` reached nothing on 2026-08-12
- [ ] Step 2's table splits into the explicit and watch columns of ADR-0025 §1, keeping the
      current top-to-bottom first-match-wins order and every row's full state
- [ ] Step 7's pre-post gate becomes mode-aware (ADR-0025 §6): explicit = still neither
      merged nor closed **and** `head` unchanged; watch = unchanged. This is the one edit
      whose omission fails silently — the review runs and nothing posts
- [ ] The handoff moves out of step 0 to its own step after the outcome is known: an explicit,
      non-terminal tick without `--once` starts
      `/loop <interval> /pr-review-loop <n> [types] [--note <text>] --on-request` and says it
      did; a `--on-request` tick starts nothing, whatever its outcome (ADR-0025 §4)
- [ ] Step 11's next-tick line follows the new rule: name the driver this tick just started,
      or — on `--once`, on OpenCode, or on a terminal exit — say plainly that nothing will
      run another
- Verify: read the file end to end as a stranger would; every flag in the usage line is parsed
  in step 0, and every table row still resolves to exactly one action

#### Step 2: Guard tests

`internal/apps/opencode/permissions_test.go` holds ten tests that read
`configs/shared/commands/pr-review-loop.md`. Each isolates one section by its exact heading
text (`markdownSection` does a `strings.Index` for that string and `t.Fatalf`s when it is
absent; `flowSection` is the same thing with whitespace collapsed). So this step has two
halves, and the first is not optional: Step 1 breaks one of these guards outright and leaves
others describing a handoff that has moved.

**2a. Revise the guards that already exist.**

- [ ] `TestPRReviewLoopStartsTheWatchItPromises` — **fails as soon as Step 1 lands.** It
      asserts that the step 0 section contains `repeat driver`, which is exactly the sentence
      the move deletes. After the change it must read the new handoff step instead and assert
      three things: the driver is started there, that step comes **after** the outcome is
      known (an index comparison of its heading against the posting step's is enough — the
      ordering is the ADR-0025 §4 property, and prose can claim it while sitting anywhere),
      and step 0 no longer starts one, so two places cannot both hand off and start two
      drivers. Its Usage half (`/loop <interval> /pr-review-loop`, `standing watch`) is
      unaffected and stays. Its step 11 half keeps `what will run the next tick` and gains the
      other branch: a tick that starts nothing — `--once`, OpenCode, a terminal exit — must
      say so.
- [ ] `TestPRReviewLoopForwardsTheNote` — still passes after Step 1, and that is the problem.
      It reads the driver form out of the **Usage** section only, and after the move the line
      the tick actually runs is the one in the handoff step. Extend the same span check to
      that step's form, so a `--note` dropped from the line that really starts the watch fails
      here rather than in a human's silence. Its wording names step 0 as the handoff site in
      two places — the doc comment's "steps 0 and 5 stay correct" and the failure message's
      "steps 0 and 5 look correct while it happens" — and both need rewording.
- [ ] `TestPRReviewLoopDescriptionCarriesTriggersNotTheHandoff` — its assertions hold (the
      frontmatter description must carry neither `/loop` nor a `step <n>` reference), but its
      failure message tells the author to "Keep the form in Usage and step 0" — after the move
      that points at the one step that must not carry it. Point it at the handoff step.
- [ ] `TestPRReviewLoopForwardsReviewerTypes` — assertions hold as written (`$ARGUMENTS` plus
      every `worktree.BuiltinReviewerChoices()` key in step 0); the new spellings are 2b's
      job. Only its anchor moves, and only if step 0's heading is renamed to mention the flags.
- [ ] `internal/tooling/task/prreviewstate_test.go`'s `TestPRReviewStateDecisionTable` — the
      three state lines it asserts do not change, because the state read does not. What goes
      stale is its documentation: the comment cites "cycle 2026-08-06-pr-review-loop §5" as
      _the_ decision table, and each row's `action` field (`wait` for a draft, for instance) is
      now the watch column only. That field is descriptive — it appears in the failure message,
      never in an assertion — so nothing fails; it simply records superseded behavior until it
      cites ADR-0025 §1 and says which mode each action belongs to.

**Anchors are matched literally, so renaming or renumbering a step is a test change in the
same commit.** The constants are `prReviewLoopParseHeading` (`### 0. …`),
`prReviewLoopTargetHeading` (3), `prReviewLoopScratchHeading` (4), `prReviewLoopRunHeading`
(5), `prReviewLoopAggregateHeading` (6), `prReviewLoopPostHeading` (8),
`prReviewLoopCleanupHeading` (10), `prReviewLoopReportHeading` (`### 11. Report the tick`),
plus `prReviewLoopUsageHeading` and `prReviewLoopNotesHeading`. A new numbered handoff step has
to sit before the report — step 11 names the driver this tick just started — so at minimum the
report's constant changes. Anything inserted before step 10 also shifts the cleanup constant
and the two cross-step phrases `TestPRReviewLoopCleansScratchOnEveryExitAfterAllocation`
asserts word for word ("on every exit taken after step 4 allocated it", and "goes through step
10" in Notes), which then have to change in the command file **and** in the test together. The
file's own prose carries more of these references (Notes still says "when step 0 hands the
command to a repeat driver"), and they move with the numbers whether or not a test reads them.
Folding the handoff into an existing step, or appending it so nothing below moves, avoids the
renumbering entirely; if a heading's text does change, update its constant in the same commit
or every guard reading that section fails on a missing anchor rather than on its own clause.

**2b. Add one guard per clause ADR-0025's Negative section names as able to rot silently:**

- [ ] The driver line the file tells the tick to start carries `--on-request`
- [ ] A `--on-request` tick starts no driver of its own (no second `/loop`)
- [ ] The explicit rows are not request-gated — draft, unrequested, and already-approved are
      all reviewed by an explicit tick
- [ ] The pre-post gate is mode-aware and does not require `requested: yes` on an explicit
      tick
- [ ] Both `--reviewer` spellings and `--once` appear in the usage line and step 0

Two of these have nothing to revise first: no test reads step 2's decision table or step 7's
pre-post gate today — neither section has an anchor constant — so the explicit-rows guard and
the mode-aware-gate guard are purely additive.

Verify: `go test ./internal/apps/opencode/` and `go test ./internal/tooling/task/`, plus
`go test .` because the change is under `configs/` (CLAUDE.md §6). Each new guard must fail
when its clause is deleted from the command file, and each revised guard must fail when the
clause it now points at is deleted — check both by deleting, not by assuming. A revised guard
that passes against the old wording as well as the new one is pinning nothing.

#### Step 3: End-to-end — the four cases the 2026-08-06 cycle's Step 6 lacks

That cycle's Step 6 points 1 and 2 describe watch ticks only, so they are run with
`--on-request`. These four are new, and come before the rest of that sequence:

- [ ] Explicit tick on an open, **unrequested** PR → reviews and posts (the exact 2026-08-12
      failure)
- [ ] Explicit tick on a **draft**, and on a PR this user has already **approved** → both
      review; the same two states under `--on-request` still wait and still stop
- [ ] Non-terminal explicit tick → a driver is started and reported, and its first firing
      reads `requested: no` and waits; a **terminal** explicit tick starts none; a
      `--on-request` tick starts none
- [ ] `--once` starts no driver, and `--reviewer document` and `--reviewer=document` both
      reach `review-run --reviewer document`

Then re-run [the 2026-08-06 cycle's Step 6](2026-08-06-pr-review-loop.md) whole, and its
Step 7's two `dg configure --force` deploys after merge.

#### Step 4: Close out the decisions

- [ ] ADR-0025 `Status: PROPOSED` → `ACCEPTED` (maintainer's call, not the implementer's)
- [ ] ADR-0025 §7's "nothing above is implemented yet" paragraph rewritten to describe what now
      ships — a docs-only landing must not have claimed the behavior existed, and an
      implemented one must not still say it does not
- [ ] ADR-0022's two narrowing notes flipped to in force
- [ ] Check off the boxes here and in the 2026-08-06 cycle's Step 6

---

## 6. Verification Plan

### Automated

```bash
go test ./internal/apps/opencode/
go test ./internal/tooling/task/   # the state read's decision-table test
go test .                          # root package — embedded configs and command guard shells
make lint
```

The root package is normally 4.8 minutes of pure cost and skipped; it is required here because
the change is under `configs/` (CLAUDE.md §6, "Which tests to run").

### Manual

The four cases in Step 3, then the 2026-08-06 cycle's fifteen-point Step 6 sequence whole.

### Regression Check

- A `--on-request` tick on an unrequested PR still waits and takes no action — the watch's
  behavior is the part ADR-0025 does **not** change.
- `/review-loop` (the sibling, same-branch loop) is untouched.
- The three reviewer types still reach `review-run --reviewer` verbatim, and `doc` still errors.

---

## 7. Risks & Trade-offs

| Risk                                                                     | Likelihood | Mitigation                                                                                              |
| ------------------------------------------------------------------------ | ---------- | ------------------------------------------------------------------------------------------------------- |
| The mode-aware pre-post gate is missed, so explicit reviews post nothing | Med        | It is called out as the one silent failure in Step 1, and Step 2 pins it with its own guard test        |
| Two tables drift apart as the file is edited later                       | Med        | The guards assert per-row behavior for both modes, so a row that loses its counterpart fails a test     |
| An explicit tick reviews a draft nobody wanted reviewed                  | Low        | Deliberate (ADR-0025): the human named the PR. Only merged/closed refuses.                              |
| `--on-request` typed by a human                                          | Low        | Accepted: it is documented as the driver's marker, and typing it just reproduces the old gated behavior |

### Trade-offs Made

- **A flag, not inference.** The tick could try to guess whether a human or a driver invoked
  it; ADR-0025 rejects that because an agent cannot check it and a guard test cannot pin it.
- **Prose, not code.** The mode split lives in a markdown command file, which is why every
  clause that can rot gets a guard test rather than a comment.

---

## 8. Cross-Model Review Notes

- [ ] Domain context clear?
- [ ] Engineer context sufficient?
- [ ] Objective unambiguous?
- [ ] Scope is actually locked?
- [ ] Steps are actionable?
- [ ] Verification is executable?
- [ ] Risks are realistic?

**Reviewer notes:**
(Fill in during review.)

---

## Notes for Implementers

- **Read ADR-0025 before touching the command file.** Its §1, §4, §5 and §6 are the
  specification; this document is only the file-by-file route.
- **Do not start until ADR-0025 is ACCEPTED.** While it is PROPOSED, the shipped artifacts
  correctly describe ADR-0022's behavior, and a half-landing is worse than neither.
- **Nothing here may become devgeta-specific.** `configs/shared/commands/pr-review-loop.md`
  runs in other people's repositories (CLAUDE.md §12).
- **Both agents, or neither.** Any rule added for Claude Code must hold for OpenCode; the
  permissions test fails the build on asymmetry either way.
