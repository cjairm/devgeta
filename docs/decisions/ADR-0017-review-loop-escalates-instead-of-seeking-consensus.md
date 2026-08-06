# ADR-0017: The review loop escalates instead of seeking consensus

**Status:** PROPOSED
**Date:** 2026-08-06
**Deciders:** cjairm
**Related:** [ADR-0012](ADR-0012-review-knowledge-in-a-local-journal.md), [ADR-0018](ADR-0018-review-loop-refuses-the-default-branch.md), [cycle 2026-08-05-review-loop](../plans/cycles/2026-08-05-review-loop.md)

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
deliberately **settled-means-settled**: `manager.go:186` refuses to settle an
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

1. **Clean approval** — every reviewer APPROVEs _and_ no agent-authored rejection is still
   awaiting ratification.
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

**Visibility is frozen at the start of each round.** Every reviewer in round R reads the
journal as it stood when round R began. Findings raised during round R become visible to
reviewers only in round R+1, after an end-of-round merge that deduplicates entries the
reviewers raised independently.

The reason is the oracle gap in the Context. Every reviewer is required to read the journal
first — the guard test `TestReviewerAgentsReadTheJournalAndCanApprove` pins
`devgeta task review-notes` into all three reviewer agents — so without a frozen view,
reviewer N would necessarily read reviewer N−1's fresh findings and be placed in exactly
the position the literature measures a 32.3-point loss in: primed to treat covered ground
as settled, and to abandon its own correct-but-divergent finding. That erodes the
non-overlapping blind spots which are the entire reason for configuring a second model.

What this does and does not change:

- **Cross-round visibility is unaffected**, and must be. A later round _should_ see earlier
  findings — that is ADR-0012 working as intended, and it is what stops the re-raise circle.
- **Within-round isolation is now preserved**, so N reviewers are N independent samples of
  the same diff rather than one sample plus N−1 anchored follow-ups.
- **Reviewer agents need no change.** They keep calling `review-notes` and
  `review-note --open` exactly as today; what moves is when their writes become visible to
  a peer. This matters because their read-first contract is pinned by a guard test, so a
  design requiring new reviewer-side commands would have to fight that test.

The merge is where cross-reviewer deduplication happens. It is real work that
sequential-with-a-shared-journal got for free, and it is the honest price of this decision
— see Consequences. It is also the larger half of what parallel reviewers need, so it is
not throwaway.

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
  unblocked in the meantime, and parallelism is left as future work — now needing only a
  journal write lock, since §4's merge already covers the dedup half.
- **An end-of-round merge with deduplication is now required work, in v1.** This is the
  direct price of §4: the shared-journal design got cross-reviewer dedup for free, and
  freezing the read view means two reviewers can independently raise the same finding with
  no chance to notice. The merge has to decide what "the same finding" means (same path and
  line is the obvious key, but two reviewers will word the same defect differently), and a
  merge that is too aggressive silently drops a real finding. It is the highest-risk piece
  this decision introduces and needs its own tests.
- **A crash mid-round is messier than before.** With writes deferred to a merge, a loop
  killed partway through a round can lose that round's findings rather than having them
  already durably journaled. The merge must therefore be the only writer, and it must write
  atomically the way ADR-0012's writes already do.
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

### Parallel reviewers

Deferred, not rejected — and §4 makes it materially cheaper than it was. Parallelism needed
two additions: a file lock on journal writes, and a post-fan-out dedup pass. The dedup pass
is now built as part of §4's end-of-round merge, so **only the write lock remains**.

Note the two decisions also point the same way now, where before they conflicted:
§4 already forbids a reviewer from seeing a peer's in-round findings, which is exactly the
property parallel execution would impose anyway. Sequential-vs-parallel becomes purely a
question of write safety and wall-clock, not of what reviewers can observe.

### Keep a shared live journal within a round (the earlier draft of §4)

Let reviewer N read reviewer N−1's fresh findings, taking cross-reviewer dedup for free.

Rejected on the Context's oracle-gap evidence, after being the recorded decision earlier in
this cycle. It buys a convenience (no merge pass) by spending the property that justifies
multi-model review at all, and it does so silently — the loop would still print two reviewer
names and look like two opinions. The merge pass is the price of the second opinion being
real.
