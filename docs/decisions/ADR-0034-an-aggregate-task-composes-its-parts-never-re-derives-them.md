# ADR-0034 — An aggregate task composes its parts, never re-derives them

**Date:** 2026-08-26
**Status:** PROPOSED

## Context

"Where does this PR stand?" costs four separate `dg task` calls today:
`pr-view`, `pr-checks`, `review-threads --state unresolved`, and
`pr-review-state`. A review round opens with all four together, every round.
`pr-state` is meant to answer that in one call — but _how_ it answers it is
the decision this ADR exists to pin down, because the easy implementation is
also the wrong one.

The easy version re-queries GitHub from scratch inside `pr-state`: its own
`gh pr view`, its own checks fetch, its own thread count. It is less code to
write per call, but it gives the same fact — "is this PR still open," "how
many checks are failing" — two independent spellings that can silently
disagree the next time either one changes (task-design.md principle 7,
"never let a fact have two spellings"). Every future aggregate task would
face the same choice, and re-litigating it each time is how the two spellings
start to drift apart in practice, not just in theory.

The obstacle to composing instead of re-deriving is that the four existing
methods do not return data — they return **already-rendered display text**.
`PRView` returns `p.Jq.FormatPRView(raw)` (`pr.go:320-326`); `ReviewThreads`
returns joined markdown, resolved-count-and-all
(`pr.go:83-122`); `PRChecks` returns a one-line-per-check string with
appended log-digest lines (`pr.go:386-426`); `PRReviewState` returns three
fixed-vocabulary lines (`prreviewstate.go:100-142`). `pr-state` cannot count
unresolved threads by parsing Markdown bullet points, and it cannot re-derive
a pass/fail bucket tally from a formatted one-line-per-check string without
just re-implementing the parse it would rather not own.

## Decision

### 1. The mechanism is a structured gatherer, not a re-query and not a parse

Each of the four existing methods is split into a **structured gatherer** —
the raw-fetch-plus-reduce core, returning a small Go struct — and the
existing rendering, which becomes a thin wrapper: fetch via the gatherer,
render the struct through the same `jq`/formatter call as before. `pr-state`
calls the **same gatherers**, and renders its own compact three-part payload
(lifecycle, check-bucket counts, unresolved-thread count, my-review state)
from the same structs the original commands use. The gatherer is the new
seam; it is never a second query for the same fact, and never a parse of the
first command's rendered output.

The existing commands' output is required to stay **byte-identical** — their
golden fixtures pass unmodified. If a fixture needs editing to make this
split work, the split was done wrong, not the fixture stale.

### 2. No gatherer may trigger a sibling's incidental heavy work

`PRChecks` fetches a failing job's log digest per failing check
(`pr.go:386-426`, `p.Gh.RunFailedJobLog`) — necessary for the existing
command's answer, completely unnecessary for `pr-state`, which needs only the
pass/fail/pending **counts**. The checks gatherer therefore stops at the
bucket tally and makes **zero** `RunFailedJobLog` calls; the log-digest
enrichment stays a `PRChecks`-only concern layered on top of the gatherer's
result, not inside it. An aggregate that pulls in a sibling's expensive path
just to reach a cheap fact defeats the reason to build the aggregate at all.

The same rule drops `FetchPRDiscussion` (`pr.go:102-109`) from `pr-state`
entirely: the unresolved-thread **count** comes from the review-threads query
alone, and `pr-state`'s payload carries no thread bodies or discussion text
(task-design.md principle 7 — never more than the caller asked for).

### 3. Owner/repo/PR resolution happens exactly once per call

`resolveOwnerRepoPR` (`pr.go:52-75`) always calls `p.Gh.CurrentRepo()`, and —
when `--pr` is absent — `p.Gh.CurrentPRNumber()` as well. Every one of the
four existing methods calls it independently today, which is fine when each
runs as its own command but would multiply those calls if each gatherer
re-ran it inside one `pr-state` invocation. So gatherers accept an
**already-resolved** `(owner, name, pr)` triple; `pr-state` resolves once via
the existing helper and passes the result into every gatherer. The resulting
call budget is part of the decision, not an incidental detail, and is
asserted by the orchestration test for both input modes:

| Call                                      | With `--pr` | Inferred (no `--pr`) |
| ----------------------------------------- | ----------- | -------------------- |
| `CurrentRepo`                             | 1           | 1                    |
| `CurrentPRNumber`                         | 0           | 1                    |
| `PRView` (metadata + review-state fields) | 1           | 1                    |
| `PRChecks`                                | 1           | 1                    |
| `FetchReviewThreads` (paginated)          | 1           | 1                    |
| `AuthenticatedLogin`                      | 1           | 1                    |
| **Total**                                 | **5**       | **6**                |

### 4. Partial failure degrades one field, not the whole command

If one gatherer errors — checks unavailable, threads unreachable — that
field reports an explicit "unavailable" sentinel in the payload and the rest
of the fields still render from whichever gatherers succeeded. `pr-state`
does not fail the entire call because one of four independent reads had a
bad day; a caller asking "where does this PR stand" would rather get three
answered fields and one honest gap than nothing.

## Consequences

- Every future aggregate task has a precedent already decided: compose
  existing gatherers behind an already-resolved identifier, never re-query,
  never parse a sibling's rendered text, and count the gh calls the
  aggregate spends before shipping it.
- `pr-view`, `pr-checks`, `review-threads`, and `pr-review-state` keep their
  exact current output — their fixtures are the proof this ADR's mechanism
  did not change a shipped contract, only where the fetch-and-reduce logic
  lives.
- The `pr-state` payload cannot drift from what the four originals would say
  about the same PR at the same moment, because both read from the same
  gatherer call — there is structurally one spelling of each fact, not two
  that happen to agree today.
- `pr-state` costs a bounded, known number of `gh` calls (5 or 6, per the
  table above) — never the failing-job-log fetches `pr-checks` alone pays for,
  and never a second discussion fetch `review-threads` alone pays for.
- A gatherer signature change is a signature change like any other in this
  codebase: it updates every caller and every caller's tests in the same
  commit, and `go build ./...` is the first check on that step.
