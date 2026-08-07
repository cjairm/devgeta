# ADR-0020: A reviewer that reports nothing is retried once

**Status:** ACCEPTED
**Date:** 2026-08-07
**Deciders:** cjairm
**Related:** [ADR-0017](ADR-0017-review-loop-escalates-instead-of-seeking-consensus.md) (narrows its "Auto-retry a failed reviewer" alternative), [ADR-0012](ADR-0012-review-knowledge-in-a-local-journal.md)

---

## Context

[ADR-0017](ADR-0017-review-loop-escalates-instead-of-seeking-consensus.md) listed
**Auto-retry a failed reviewer** among its alternatives and rejected it for v1, on two
grounds:

> A retry hides the difference between a flaky provider and a misconfigured one, and Step
> 0's probing found OpenCode's error text is generic (`"Unexpected server error"`) even for
> an unusable model — so a retry loop would have nothing reliable to decide on.

Running v1 against real branches turned up a failure that rejection did not anticipate, and
that neither ground covers.

A headless `opencode run` auto-rejects any permission it cannot ask a human about. It says
so on **stderr only**:

```
! permission requested: external_directory (/Users/x/.claude/*); auto-rejecting
```

That line is never an event in the `--format json` stream, the process still **exits 0**,
and the agent loop dies mid-work having emitted zero text events. `OpenCode.Run` discarded
stderr, so the one line explaining it was thrown away. The result a human saw, after a paid
multi-minute round: a bare `NO VERDICT`, with no reason anywhere — nothing to read, nothing
to fix, and the whole round spent.

Two things changed between that rejection and this decision:

1. **The reason is no longer missing.** The wrapper now returns stderr alongside stdout
   (`opencode.RunResult`), and `noReportReason` reports OpenCode's own words — stderr first,
   the final `step_finish` reason as fallback. The premise "nothing reliable to decide on"
   held while the only signal was a generic error string; it does not hold for a class the
   run itself explains.
2. **This class is separable without reading any text.** A run that produced **no assistant
   text at all** is a structural fact of the event stream, not a guess at message wording.
   It is a different thing from "wrote a report that named no verdict": the loop ended
   before the model ever spoke, so there is no opinion to preserve and nothing for a human
   to act on.

The question this ADR answers is therefore narrower than the one ADR-0017 rejected: not
"should a failed reviewer be retried", but "should a reviewer that produced nothing at all
be launched once more".

## Decision

**`review-run` launches a reviewer at most twice: the run, plus one retry, and only when the
attempt produced no report at all.** `reviewerAttempts = 2` and `runReviewerWithRetry` in
`internal/tooling/task/reviewrun.go`.

The rules that keep it narrow:

- **The trigger is structural, never textual.** `classifyReviewerRun` returns a `reported`
  flag that is false only when the run emitted no assistant text whatsoever. No error string
  is matched to decide a retry — the thing ADR-0017 refused to build is still not built.
- **A reviewer that said anything is never retried.** Even a report with no `Status:` line in
  it is an opinion that was earned and paid for; re-running would pay again to overwrite it.
- **The retry is cause-agnostic.** Any cause of an empty run — auto-rejected permission,
  spawn failure, nonzero exit, timeout with nothing on stdout — gets the one extra attempt.
- **A failed retry never overwrites the first attempt's outcome.** The retry is a rescue, not
  a second opinion on the failure: it can replace the reported outcome only by producing a
  report. A first attempt that failed with `ERROR(Unexpected server error…)` followed by a
  retry that died with nothing on stderr reports the first — the vaguer of the two would
  otherwise throw away the only words anyone had.
- **It is announced, not silent.** `reviewerProgress.retrying` prints
  `<label>: <outcome> — no report, retrying once` to the progress stream (stderr), in quiet
  mode too, so a round that paid twice says so while it happens.
- **It lives at the attempt layer, not the round layer.** This is `review-run` re-launching
  one reviewer inside a single round. The `/review-loop` command still never re-runs a
  round: an `ERROR(<reason>)` or `NO VERDICT` outcome still stops the loop dead
  (`configs/shared/commands/review-loop.md` step 2). Nothing about ADR-0017's bounded-rounds
  contract changes.

### Options considered, and the one chosen

1. **Reason only, no retry.** Keeps ADR-0017 exactly as written; every empty run still costs
   the whole round and a human turn, even when a second launch would have worked.
2. **Retry only on a detected permission auto-reject.** Needs stderr text matching to detect
   the case — precisely the guessing ADR-0017 refused, and brittle against wording changes
   in a tool devgeta does not own.
3. **Retry once on any no-report cause.** Chosen. It needs no text match, it bounds the cost
   at exactly one extra attempt, and a permanent cause simply fails the same way twice and is
   reported with its reason.

## Consequences

### Positive

- The failure that motivated this costs one extra attempt instead of a dead round: the
  common shape of an empty run is transient, and the retry turns it into a normal verdict.
- A permanent cause is not hidden. It fails twice, and the first attempt's outcome — with
  OpenCode's own reason in it — is what gets reported. ADR-0017's "a retry hides the
  difference between a flaky provider and a misconfigured one" is answered by reporting the
  failure rather than by refusing to retry.
- No error-text matching enters the decision path, so the reason ADR-0017 gave for rejecting
  retries in the first place stays respected.

### Negative

- **A reviewer that never reports costs up to twice as much.** Worst case is
  `2 × reviewRunTimeout` = 60 minutes of wall clock for one reviewer, and an empty attempt
  that burned tokens burns them again. This is accepted knowingly: it is bounded at exactly
  one extra attempt (there is no chain), and it applies only to the no-report class — the
  generic-error case ADR-0017 cited (`"Unexpected server error"` after the model has written
  something) is reported the first time and never retried.
- A reviewer with a permanently broken configuration now takes two attempts to say so,
  delaying an error a human could have seen sooner.

### Neutral

- The retry is invisible in `review-run`'s stdout: the verdict line for a rescued reviewer
  looks exactly like a reviewer that worked first time, because the outcome contract is one
  line per reviewer and nothing else. The evidence lives in the progress stream and in the
  closing line's tool count and cost, which deliberately span both attempts.
- The loop's behavior is unchanged, but its contract prose had to say so explicitly, since
  "there is no retry in this version" would otherwise read as a statement about the whole
  system rather than about the round layer.
