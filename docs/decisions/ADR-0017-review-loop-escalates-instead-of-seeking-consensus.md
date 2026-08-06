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

**A caution the same literature raises against part of this design.**
[The Cost of Consensus](https://arxiv.org/html/2605.00914) reports that "isolated
self-correction consistently offers a more favorable cost-accuracy tradeoff" than unguided
homogeneous debate, and — most pointedly here — that "teams systematically generate, but
subsequently discard, correct answers due to peer-induced sycophancy." It quantifies this as
an _oracle gap_: in one reported configuration a correct answer appeared somewhere in the
team 53.0% of the time while final team accuracy was 20.7%, a 32.3-point gap of correct
answers found and then thrown away.

That is a direct argument for keeping reviewers **isolated from each other's in-progress
findings**, which §4 below does not do. It is recorded as an open question rather than
silently resolved.

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

### 4. Reviewers run sequentially — and see each other's findings within a round

The execution order is settled: sequential, because the journal has no write lock and
serialized execution is what makes concurrent writes a non-problem.

**What reviewers can see is a separate question, and it is deliberately stated rather than
left implicit.** Every reviewer is required to read the journal first — the guard test
`TestReviewerAgentsReadTheJournalAndCanApprove` pins `devgeta task review-notes` into all
three reviewer agents — and reviewer N−1's findings are written to that same journal as
open entries during the round. So **reviewer N does read reviewer N−1's findings from the
same round.** This is structural, not incidental: given the read-first rule and a shared
journal, it cannot be avoided without changing one of the two.

That buys cross-reviewer dedup for free. It also costs within-round independence, which is
the property the multi-model configuration exists to provide, and the literature cited in
the Context is specifically unfriendly to it. The honest accounting:

- **Cross-round independence is unaffected.** A later round _should_ see earlier findings —
  that is ADR-0012 working as intended.
- **Within-round independence is given up.** Reviewer 2 may search less thoroughly in
  ground reviewer 1 appears to have covered, which directly erodes the non-overlapping
  blind spots that justify a second model.
- **The conformity failure mode is narrower here than in the debate literature**, because
  this design never treats agreement as evidence. Verdicts are not journaled, so reviewer 2
  never sees reviewer 1's verdict, and §2's any-single-blocker rule aggregates nothing. The
  residual risk is reduced _discovery_, not a manufactured consensus.

**Open question, flagged not resolved:** whether each reviewer should instead read a
snapshot of the journal taken at the start of the round, with a dedup pass afterward. That
preserves within-round independence and still needs no write lock (execution stays
sequential), at the cost of the dedup pass — which the cycle already lists as one of the two
prerequisites for parallel reviewers. Not adopted here because sequential-with-shared-journal
is the recorded decision; revisit before the multi-model path is relied on for coverage
rather than for a second opinion.

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
- **Sequential order buys journal safety and cross-reviewer dedup without any new
  machinery** — no lock, no post-fan-out merge pass. See §4 for what it costs.
- **The escalation report is the product.** Verdict table per reviewer per round, every
  agent rejection with its evidence, and the exact ratify/reopen command with the id
  filled in — so the human's remaining work is a decision, not an investigation.

### Negative

- **More escalations than a majority rule would produce.** One dissenting reviewer is
  enough to deny clean approval. Accepted deliberately: a false escalation costs a human
  glance, a silently outvoted finding costs a defect.
- **Wall-clock per round is the sum of the reviewers, not the max.** Sequential execution
  is a real cost on a multi-model configuration; subagent execution keeps the human
  unblocked in the meantime, and parallelism is left as future work with a known
  checklist (a journal write lock plus a dedup pass).
- **Within-round reviewer independence is given up to get dedup.** See §4. This is the one
  negative that partly contradicts the Context's own justification for multi-model review,
  and it is the reason §4 carries an open question instead of a settled rationale. It
  matters most if `review.reviewers` is ever configured for _coverage_ — trusting two models
  to find different things — rather than for a second opinion.
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

Deferred, not rejected. It needs exactly two additions — a file lock on journal writes and
a post-fan-out dedup pass — recorded here so the future flag has its checklist.
