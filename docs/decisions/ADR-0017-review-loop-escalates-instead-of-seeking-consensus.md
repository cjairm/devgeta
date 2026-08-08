# ADR-0017: The review loop escalates instead of seeking consensus

**Status:** ACCEPTED
**Date:** 2026-08-06
**Deciders:** cjairm
**Related:** [ADR-0012](ADR-0012-review-knowledge-in-a-local-journal.md), [ADR-0018](ADR-0018-review-loop-refuses-the-default-branch.md), [ADR-0020](ADR-0020-a-reviewer-that-reports-nothing-is-retried-once.md) (narrows the "Auto-retry a failed reviewer" alternative below), [cycle 2026-08-05-review-loop](../plans/cycles/2026-08-05-review-loop.md)

---

## Context

[ADR-0012](ADR-0012-review-knowledge-in-a-local-journal.md) gave review a memory: findings
and answers persist per branch, so a re-review no longer re-asks what was already settled.
It did not give review a **driver**. The round trip is still manual — launch a reviewer,
read its report, carry each finding to the coding agent, verify it, fix or push back,
re-launch the reviewer — with the human as the orchestrator on every hop.

Automating that loop means answering questions the manual flow never had to, because a
human standing in the middle answered them implicitly:

1. **When does it stop?** A loop with no bound either converges or runs forever.
2. **What counts as approval when reviewers disagree?** With more than one reviewer, "the
   reviewer approved" stops being a single fact.
3. **Do reviewers run at once or in turn?**
4. **What happens when the coding agent believes a finding is wrong?** A human could
   simply say so. An unattended agent needs a recorded mechanism, and ADR-0012's journal
   has no state that means "disputed."

Two constraints come from published multi-agent-review research, and they point the design
away from the obvious answer:

- **Models conform to each other.** Unanimous agreement between models is not evidence of
  correctness — it is partly an artifact of them agreeing. So a loop that runs until the
  reviewers agree is optimizing the wrong quantity. Cross-agent sycophancy is documented to
  suppress productive disagreement and drive premature consensus
  ([Too Polite to Disagree](https://arxiv.org/html/2604.02668)), with conformity past a
  threshold producing collectively biased norms
  ([Emergence of Biased Consensus](https://arxiv.org/html/2608.02827)).
- **Aggregating by majority discards correct minority findings.** Debate is reported not to
  improve correctness once the aggregation strategy is controlled for, and to suppress
  correct minority opinions through social pressure
  ([Minority Sentinel](https://arxiv.org/pdf/2606.29270)). Multiple reviewers are therefore
  worth having as independent samples, not as voters whose majority is the verdict.

**The sharpest finding in that literature is what shapes §4.**
[The Cost of Consensus](https://arxiv.org/html/2605.00914) reports that "isolated
self-correction consistently offers a more favorable cost-accuracy tradeoff" than unguided
homogeneous debate, and — most pointedly here — that "teams systematically generate, but
subsequently discard, correct answers due to peer-induced sycophancy." It quantifies this as
an _oracle gap_: in one reported configuration a correct answer appeared somewhere in the
team 53.0% of the time while final team accuracy was 20.7%, a 32.3-point gap of correct
answers found and then thrown away.

A reviewer that reads a peer's in-progress findings is in exactly that position. So
**isolation within a round is a requirement, not a nicety** — see §4.

These citations are supporting evidence for a design already chosen on other grounds; none
of them is about code review specifically, and none was replicated here.

One more constraint comes from ADR-0012 itself, and it is the sharp one. Its journal is
deliberately **settled-means-settled**: `manager.go:238` refuses to settle an
already-settled entry, and nothing reopens one. Reviewers are instructed to treat a
settled entry as closed. That is exactly what stops the re-raise circle — and it means
that if the coding agent settles a finding as `rejected`, the reviewer has no way to
sustain its disagreement. The agent gets the last word by construction.

## Decision

**The loop is bounded, any single blocking verdict blocks, and unresolved disagreement is
escalated to the human — never voted away. The loop ends in exactly two states.**

### 1. Bounded rounds, never a convergence condition

`review.rounds` defaults to **3** and is capped at **5**. The loop stops at the cap
regardless of what the reviewers say. There is no "keep going until they agree" branch,
because agreement is not the target.

### 2. Any single blocking verdict blocks

Each reviewer returns one of three verdicts (the contract already in
`configs/shared/agents/code-reviewer.md`): `APPROVE`, `REQUEST CHANGES`,
`NEEDS DISCUSSION`.

- One `REQUEST CHANGES` blocks, however many reviewers approved.
- `NEEDS DISCUSSION` is **treated as blocking**, and escalates if it survives a round.

This is the strictest possible consensus rule, chosen so that no finding is ever silently
outvoted.

### 3. Exactly two terminal states

1. **Clean approval** — every reviewer APPROVEs, the round's `open:` line reads `none`, _and_
   no agent-authored rejection is still awaiting ratification. An entry still under `open:`
   is an unanswered finding — allowing it to ride would make "clean approval" mean "approved
   with outstanding findings," which is exactly the false approval this contract exists to
   rule out.
2. **Report to the human** — everything else.

The report state covers: persistent disagreement, hitting the round cap, any reviewer
process failure (`ERROR` / `NO VERDICT`), and approval that rests on an unratified agent
rejection. A process failure can therefore never be mistaken for approval, and there is no
third, undocumented outcome. `ERROR` and `NO VERDICT` are not retried in v1.

### 4. Reviewers run sequentially, but each reads the round's opening state

Two separate properties, decided separately, because conflating them is what made an
earlier draft of this ADR incoherent.

**Execution is sequential.** The journal has no write lock, and serialized execution is
what makes concurrent writes a non-problem.

**Isolation is achieved by narrowing what a reviewer READS, never by deferring what it
writes.** Writes stay exactly as they are today: `review-note --open` appends to the live
journal and saves immediately (`manager.go:205-206`). What changes is that during a round,
`review-notes` hides entries created in that same round.

The reason for isolation is the oracle gap in the Context. Every reviewer is required to
read the journal first — the guard test `TestReviewerAgentsReadTheJournalAndCanApprove`
pins `devgeta task review-notes` into all three reviewer agents — so on a live shared view,
reviewer N would necessarily read reviewer N−1's fresh findings and land in exactly the
position that literature measures a 32.3-point loss in: primed to treat covered ground as
settled, and to abandon its own correct-but-divergent finding.

**The mechanism is a round-start snapshot of the journal file, read-only.** At the start of
round R, `review-run` copies the journal to a disposable snapshot. Each reviewer's
`review-notes` reads that snapshot; every write still goes to the live journal, immediately,
as today. At round end the snapshot is discarded, so round R+1 takes a fresh one.

**The snapshot is written unconditionally, including when no journal exists yet.** This is
not a detail — it is the difference between the mechanism working and not working on the
single most common case, a branch's first review. There, the journal file is absent at round
start and reviewer 1's first `--open` creates it; if an absent journal were treated as
"nothing to snapshot, no pointer," reviewer 2 would fall through to the live journal and read
reviewer 1's brand-new findings. Isolation would be missing exactly where a first review needs
it most. An absent journal is a real state to capture — "empty at round start" — not an
absence of state. `Load` already returns an empty journal rather than an error when the file
does not exist (`manager.go:70-72`), so the snapshot is simply whatever `Load` yields, and the
implementation needs no absent-journal special case at all.

**The snapshot must freeze entry state, not merely entry existence**, and this is the whole
reason it is a file copy rather than a cheaper "hide ids above N" filter. An entry that is
_open_ at round start has no settled conclusion to inherit, and the reviewer contract
explicitly tells a reviewer to close one: "An entry under `open:` is still unanswered. It
keeps its id: re-raise it in the report citing that id, or — if the code has since fixed it —
settle it `--as fixed`" (`configs/shared/agents/code-reviewer.md:31`). Paired with "An entry
marked `[fresh]` is settled. **Do not raise it again**" (line 28), an existence-only filter
leaks a fresh peer conclusion straight into the next reviewer's read: reviewer 1 settles a
round-start-open entry, and reviewer 2 is instructed not to evaluate it. Freezing the file
closes that path, because the snapshot still shows the entry open.

Writes are deliberately **not** snapshotted, which is what keeps this cheap: ids continue to
come from `nextID()` on the live journal (`max+1`, never reused —
`journal.go:109-118`, pinned by `TestNextIDNeverReusesAfterDeletion`), so two reviewers reading
the same snapshot still get distinct, final ids.

How the snapshot is pointed at needs one real capability that does not exist yet — a
child-only environment variable on the spawned reviewer process — and one assumption worth
probing, that the variable reaches the `devgeta task review-notes` the agent shells out to.
Both are specified in the cycle's Step 4. The pointer matters for safety, not just
plumbing: a snapshot at a well-known path would be read by any `review-notes` that happened
to find it, so a leftover file from a killed loop would silently freeze reads indefinitely.
Pointed-at-by-the-caller means a stale snapshot is simply never read, and an absent pointer
falls back to the live journal — today's behavior.

This is deliberately chosen over staging writes and merging them at round end. Everything
stays durable and identity-stable:

- **No staging area, no write redirection.** Writes go where they always went.
- **No temporary ids, and no id remapping.** A reviewer that is told `Noted n7` can print
  `review-note --settle --id n7` and that id is correct permanently. Under a staging design
  the merge would renumber, silently invalidating the id the reviewer just reported.
- **No crash cleanup that can lose data.** A loop killed mid-round leaves every finding
  already journaled, because nothing was being held back. The only debris is an orphan
  snapshot, which is disposable and — because it is read only when pointed at — inert.
- **No deduplication in Go**, which keeps §5 intact — see below.

What this does and does not change:

- **Cross-round visibility is unaffected**, and must be. A later round _should_ see earlier
  findings — that is ADR-0012 working as intended, and it is what stops the re-raise circle.
- **Within-round isolation covers both new findings and state changes**, so N reviewers are
  N independent samples of the same diff rather than one sample plus N−1 anchored follow-ups.
- **Reviewer agents need no change**, and this is literally true rather than aspirational:
  they keep calling `review-notes` and `review-note --open` with the same arguments. Only
  where `review-notes` reads from moves.

### Duplicates are kept, not merged

Two reviewers may independently raise the same defect in different words. Nothing in Go
tries to detect that.

This follows directly from §5: deciding whether two differently-worded findings are one
defect or two is a judgment call, and path-plus-line cannot answer it — two distinct defects
share a line often enough that location is not identity. Any Go-side heuristic here would be
mechanical code making a semantic call, and its failure mode is the worst available: a
dropped finding looks exactly like a clean review.

So both entries persist, both are visible to the human, and both are visible to the next
round. The coding agent — which is the component that holds judgment — verifies the defect
once and settles both, citing the same evidence. A duplicate costs a human one glance; a
silently dropped defect costs a defect.

### 5. Go does the mechanism, the agent does the judgment

`dg task review-run` fans out to reviewers, parses verdicts, and reads and writes the
journal. Whether a finding is real, and what fix it deserves, stays in the agent command
(`/review-loop`). No model-judgment call is hardcoded in Go where it cannot reason, and no
mechanical step is left to a model that might skip it.

### 6. An agent rejection is provisional, and only a human can retire it

When the coding agent judges a finding wrong, it settles the entry as `rejected` with its
disproving evidence, and the note is **prefixed `agent:`** so provenance is never
ambiguous. Such an entry is _provisional_:

- It stops the re-raise circle, which is why `rejected` is used at all.
- It is **always carried into the terminal report** for the human to ratify. Its mere
  existence downgrades "all reviewers approved" from clean approval to a report.
- Two transitions retire it, both human-only:
  `review-note --ratify --id <id>` (accept — strips the `agent:` provenance, and the entry
  becomes an ordinary human rejection under ADR-0012 semantics) and
  `review-note --reopen --id <id>` (refuse — the entry returns to open under the **same
  id**, with its original finding text and the agent's note dropped, so the next round
  re-raises it as ADR-0012 already specifies for an open entry: re-raised, never
  duplicated).

The loop never uses `rejected` to make a disagreement disappear; it uses it to stop the
circle while keeping the dispute visible at the end.

**This is enforced by prose, not by permissions, and that is a known limit.** The
permission model cannot tell who typed a `devgeta task` command, so nothing structurally
stops the loop from ratifying its own pushback. It is held the same way the reviewers'
settle step already is: the `/review-loop` instructions never invoke either flag, and a
guard test asserts the command file never mentions them outside the report template.

## Consequences

### Positive

- **A wrong finding cannot be buried, and a right one cannot be outvoted.** The
  any-single-blocker rule plus provisional rejections mean every disagreement ends either
  fixed or in front of the human.
- **The two terminal states are exhaustive and auditable.** "It approved" always means the
  same thing. A crashed reviewer reports as a crashed reviewer.
- **Sequential execution keeps journal writes safe with no lock**, and the frozen read view
  means adding a reviewer adds an independent sample rather than an anchored one — so
  `review.reviewers` can be trusted for _coverage_, not just for a second opinion.
- **The escalation report is the product.** Verdict table per reviewer per round, every
  agent rejection with its evidence, and the exact ratify/reopen command with the id
  filled in — so the human's remaining work is a decision, not an investigation.

### Negative

- **More escalations than a majority rule would produce.** One dissenting reviewer is
  enough to deny clean approval. Accepted deliberately: a false escalation costs a human
  glance, a silently outvoted finding costs a defect.
- **Wall-clock per round is the sum of the reviewers, not the max.** Sequential execution
  is a real cost on a multi-model configuration; subagent execution keeps the human
  unblocked in the meantime, and parallelism is left as future work needing a journal write
  lock.
- **Duplicate open entries are now possible and are not cleaned up.** Two reviewers that
  independently find the same defect produce two entries, and §4 deliberately keeps both.
  The human sees near-identical findings, and the coding agent settles each. This is the
  accepted cost of refusing to put semantic judgment in Go, and the direction of the error
  is chosen: noise rather than silence.
- **`review-notes` gains ambient behavior.** Its output depends on a snapshot pointer
  supplied by the environment, so the same command prints different things inside and
  outside a loop round. That is a real readability cost and needs a test pinning both ways:
  with no pointer set, output is byte-identical to today.
- **The shared executor has to grow an environment overlay.** `CommandParams` today carries
  `Args`, `Timeout`, `Dir`, `Stream` and no environment, and `ExecCommand` never sets
  `exec.Cmd.Env` (`internal/commands/base.go`, as of this decision), so there is currently
  no way to add a variable for one child process. That capability has to be added to the executor and
  exposed through the OpenCode wrapper — the sanctioned direction per CLAUDE.md §6, but real
  work in a shared, widely-used code path, and it must be an overlay on the inherited
  environment rather than a replacement.
- **The human-only rule is prose-level.** See §6. A guard test narrows it; it does not
  close it.
- **The loop's judgment step cannot be unit-tested in Go.** That is the direct cost of
  decision 5, accepted in exchange for not hardcoding model judgment.

### Neutral

- Two reviewers can both be wrong and both approve. That is inherent to AI review, not
  something this contract can fix; the bounded rounds and the human owning the merge
  decision are the mitigation. The loop never self-merges.
- `review.reviewers` entries are passed through to OpenCode unvalidated. A stale model
  string surfaces as an `ERROR(<reason>)` outcome in the report — visible, never a silent
  skip.

## Alternatives Considered

### Loop until the reviewers agree

Rejected on the research constraint above: models conform, so unanimity is a weak signal
that a loop can manufacture by simply running longer. It also has no natural bound, which
turns a review into an open-ended spend.

### Majority vote among reviewers

Rejected. It is the mechanism that silently discards a correct minority finding — exactly
the failure this contract is shaped to prevent. With three reviewers, a real bug spotted
by one is dropped 2–1 with no trace.

### Reserve `rejected` for humans, and leave disputed findings open

The tidier-looking option: never let an agent write a `rejected` entry, so ADR-0012's
"settled means settled" keeps its original meaning of "a human concluded this."

Rejected because an **open** entry carries no channel for the agent's counter-evidence.
Every false-positive finding would come back un-countered on the next round and escalate,
so the loop would escalate most on exactly the branches where it should have been able to
say "this finding is wrong, here is the proof." That defeats the automation. Provenance
marking plus mandatory ratification keeps the human's authority while giving the
disagreement somewhere to live.

### Auto-retry a failed reviewer

Rejected for v1. A retry hides the difference between a flaky provider and a
misconfigured one, and Step 0's probing found OpenCode's error text is generic
(`"Unexpected server error"`) even for an unusable model — so a retry loop would have
nothing reliable to decide on. Failures surface by name instead.

**Revisited 2026-08-07 and narrowed, not reversed — see
[ADR-0020](ADR-0020-a-reviewer-that-reports-nothing-is-retried-once.md).** A real round came
back as a bare `NO VERDICT` because a headless run auto-rejects a permission it cannot ask
about, writes that only to stderr, and exits 0 with the agent loop dead and zero text events.
`review-run` now retries **one** attempt, and only when the attempt produced **no report at
all** — a structural fact of the event stream, so no error text is matched and this
rejection's reasoning still stands for every failure that did produce output. A failed retry
never overwrites the first attempt's outcome, so a misconfigured provider is still surfaced
by name. The round layer is untouched: the loop still never re-runs a round.

### Parallel reviewers

Deferred, not rejected. Parallelism needs a **file lock on journal writes**; §4's round-start
snapshot is orthogonal to it and would carry over unchanged.

Note the two decisions point the same way, where an earlier draft had them conflicting: §4
already forbids a reviewer from seeing a peer's in-round findings, which is the property
parallel execution would impose anyway. Sequential-vs-parallel is purely a question of write
safety and wall-clock, not of what reviewers can observe. Parallelism no longer implies a
dedup pass either, because §4 keeps duplicates by design.

### Keep a shared live journal within a round (the first recorded decision)

Let reviewer N read reviewer N−1's fresh findings, taking cross-reviewer dedup for free.

Rejected on the Context's oracle-gap evidence. It buys a convenience by spending the
property that justifies multi-model review at all, and it does so silently — the loop would
still print two reviewer names and look like two opinions.

### Stage each reviewer's writes and merge them at round end (the second draft of §4)

Redirect `review-note --open` into per-reviewer staging state during a round, then merge
into the journal at round end with deduplication.

Rejected as more machinery for a worse result, once the details were worked through:

- **It needs write redirection that does not exist.** Each `review-note` call is a separate
  `devgeta` process that loads, appends, and saves the shared journal immediately; nothing
  in `reviewjournal` or `reviewnotes.go` accepts a path or destination override today. The
  snapshot design needs no write-side change at all.
- **It breaks id stability.** Two reviewers staging from the same base both compute the same
  `nextID()`, so the merge must renumber — which invalidates the id the reviewer already
  reported to the human in its own settle line.
- **It adds a crash-cleanup problem** the snapshot does not have: held-back findings can be
  lost or half-merged, so the merge has to become a transactional writer. A snapshot, by
  contrast, is disposable — the live journal is always the truth.

### Hide ids above a round-start floor instead of copying the journal (the third draft of §4)

Pass the highest id at round start and have `review-notes` hide anything above it. One
integer instead of a file.

Rejected because it freezes **existence** but not **state**. `code-reviewer.md:31` tells a
reviewer to settle a round-start-open entry `--as fixed` when the code now fixes it, and
line 28 tells the next reviewer not to re-raise anything already settled — so reviewer 1's
in-round conclusion reaches reviewer 2 through an entry whose id is below the floor and
therefore visible. The anchoring the section exists to prevent survives, on precisely the
findings most likely to matter (the ones already under discussion). A file copy costs
almost nothing more and closes the path completely.

- **Its dedup step requires semantic judgment in Go**, violating §5. See "Duplicates are
  kept, not merged" in §4.
