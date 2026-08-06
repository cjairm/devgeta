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

**Isolation is achieved by narrowing what a reviewer READS, never by deferring what it
writes.** Writes stay exactly as they are today: `review-note --open` appends to the live
journal and saves immediately (`manager.go:143`). What changes is that during a round,
`review-notes` hides entries created in that same round.

The reason for isolation is the oracle gap in the Context. Every reviewer is required to
read the journal first — the guard test `TestReviewerAgentsReadTheJournalAndCanApprove`
pins `devgeta task review-notes` into all three reviewer agents — so on a live shared view,
reviewer N would necessarily read reviewer N−1's fresh findings and land in exactly the
position that literature measures a 32.3-point loss in: primed to treat covered ground as
settled, and to abandon its own correct-but-divergent finding.

**The mechanism is a read floor, and it works because ids are monotonic.** `nextID()` is
`max+1` and ids are never reused within a journal (`journal.go:69-81`, pinned by
`TestNextIDNeverReusesAfterDeletion`), so a higher sequence number always means "created
later." The loop therefore records the journal's highest id at the start of round R and
passes that floor to each reviewer's environment; `review-notes` shows entries at or below
the floor and hides the rest. One integer, read-side only.

How the floor reaches the reviewer is an implementation detail with one assumption worth
naming: it relies on the reviewer's `devgeta task review-notes` inheriting the environment
of the `opencode run` process that the loop spawned. That must be probed rather than assumed,
because it fails **open** — an absent floor means reviewers see everything, which is the
anchored behavior this section exists to remove, and it produces no error while doing so. If
the agent runtime does not pass the environment through, the floor moves to a devgeta-owned
channel (a small state file beside the journal) and this section is amended to say so; the
decision — isolate on the read side — is unaffected either way.

This is deliberately chosen over staging writes and merging them at round end. Everything
stays durable and identity-stable:

- **No staging area, no write redirection.** Writes go where they always went.
- **No temporary ids, and no id remapping.** A reviewer that is told `Noted n7` can print
  `review-note --settle --id n7` and that id is correct permanently. Under a staging design
  the merge would renumber, silently invalidating the id the reviewer just reported.
- **No crash cleanup.** A loop killed mid-round leaves every finding already journaled,
  because nothing was being held back. There is no partial-merge state to repair.
- **No deduplication in Go**, which keeps §5 intact — see below.

What this does and does not change:

- **Cross-round visibility is unaffected**, and must be. A later round _should_ see earlier
  findings — that is ADR-0012 working as intended, and it is what stops the re-raise circle.
- **Within-round isolation holds for findings**, so N reviewers are N independent samples of
  the same diff rather than one sample plus N−1 anchored follow-ups.
- **Reviewer agents need no change**, and this is now literally true rather than
  aspirational: they keep calling `review-notes` and `review-note --open` with the same
  arguments, and only `review-notes`' own output is filtered by the ambient floor.

### The limit of this isolation, stated rather than implied

The floor hides entries **created** during the round. It does not hide **state changes to
entries that already existed** — if a reviewer settles a pre-existing entry mid-round, the
next reviewer sees it settled.

Accepted, for two reasons. Substantively, a settled pre-existing exchange is round-start
knowledge: its resolution was reached before this round and is exactly what ADR-0012 exists
to carry forward. And mechanically, hiding state transitions would require snapshotting the
whole journal, which reintroduces every cost the read floor avoids. The anchoring this
decision is aimed at — "the previous reviewer already found the bugs here" — is about newly
raised findings, and those are hidden.

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
- **`review-notes` gains ambient behavior.** Its output depends on a floor supplied by the
  environment, so the same command prints different things inside and outside a loop round.
  That is a real readability cost and needs a test pinning both: with no floor set, output
  is unchanged from today.
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

Deferred, not rejected. Parallelism needs a **file lock on journal writes**; §4's read floor
is orthogonal to it and would carry over unchanged.

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
  read-floor design needs no write-side change at all.
- **It breaks id stability.** Two reviewers staging from the same base both compute the same
  `nextID()`, so the merge must renumber — which invalidates the id the reviewer already
  reported to the human in its own settle line.
- **It adds a crash-cleanup problem** that the read floor simply does not have: held-back
  findings can be lost or half-merged, so the merge has to become a transactional writer.
- **Its dedup step requires semantic judgment in Go**, violating §5. See "Duplicates are
  kept, not merged" in §4.
