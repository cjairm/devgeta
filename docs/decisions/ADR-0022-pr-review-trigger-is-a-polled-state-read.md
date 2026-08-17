# ADR-0022: The PR review trigger is a polled state read, and one invocation is one tick

**Status:** ACCEPTED
**Date:** 2026-08-07
**Deciders:** cjairm
**Related:** [ADR-0023](ADR-0023-a-pr-review-targets-immutable-shas.md), [ADR-0025](ADR-0025-an-invocation-reviews-the-request-gates-only-the-watch.md) (ACCEPTED — narrows §1's trigger rule and §2's handoff order to the watch's own ticks; both narrowings are in force, see the notes in those sections), [cycle 2026-08-06-pr-review-loop](../plans/cycles/2026-08-06-pr-review-loop.md)

---

## Context

Reviewing someone else's PR is already automated in its middle — the reviewer agents run
cross-model through `dg task review-run`, and `/review-pr` or `/approve-pr` posts the
result. What is still manual is the **beginning and the repetition**: a human notices that
GitHub is asking them for a review, starts the flow, and — after posting — has to notice a
later re-request (the author fixed things and pressed the re-review button) to run it
again.

Automating that needs three answers, and each one is a place where a design can invent
state it does not need:

1. **What tells the loop a review is wanted?**
2. **What makes it happen again, without re-reviewing what it already answered?**
3. **Whose request counts?** A PR can request a review from a person, from a team, or from
   both.

CLAUDE.md §6 says to investigate what already tracks a thing before designing something
that tracks it in parallel. So GitHub's own state was probed first, live, against real PRs
on 2026-08-06 (account `cjairm`, real `gh`):

- **`reviewRequests` is per-user, direct, and current-state.**
  `gh pr view <n> --json reviewRequests,state,isDraft` returns the requested logins.
  "Requested from me and not from someone else" is a field read, not a filter to build.
- **Submitting a review removes the user from it**, while other pending reviewers stay
  listed. Checked against five open PRs the user had already reviewed.
- **A comment-only review clears it too.** PR #213's timeline: Copilot was
  `review_requested` at 18:17:19, submitted a `commented` review at 18:20:49, and is no
  longer in `reviewRequests`. No review type leaves a user stuck in the list, so "posted
  anything" always de-triggers.
- **The re-request button puts the user back**, which is GitHub's documented re-review
  behavior.

That is a complete state machine for this problem, maintained by GitHub, already correct
across every client that touches the PR.

The second constraint is where this code runs. The loop drives the user's own agents, with
the user's model configuration, credentials, and local checkout. It is a laptop-side tool,
not a CI job.

## Decision

**The trigger is a poll of GitHub's `reviewRequests` field. One invocation of
`/pr-review-loop` is one idempotent tick; the repetition belongs to an external driver.
Only a request that names the authenticated user triggers a review.**

### 1. Poll GitHub state, not events

Each tick reads the PR's current state — `pr:` (`open | draft | merged | closed`),
`requested:` (is the authenticated user in `reviewRequests` right now), `my-review:` (that
user's latest submitted review) — and decides from that alone.

**This needs no local event log and no "already reviewed" bookkeeping**, and that is the
whole reason the field was chosen. The three things a trigger normally has to track fall
out of one read:

| What a watcher normally needs | Where it comes from here                        |
| ----------------------------- | ----------------------------------------------- |
| Trigger                       | the user is in `reviewRequests`                 |
| Dedup ("don't review twice")  | submitting removes them, so the next tick waits |
| Re-trigger                    | the re-request button puts them back            |

Any local record of "I already reviewed this" would be a second source of truth that can
drift from GitHub's — stale after a session dies, wrong after someone reviews from another
machine, and needing its own cleanup. There is none.

Polling one JSON field costs nothing at the intervals this runs at (minutes), and the read
is the same `gh` call the rest of the PR task family already makes.

**There is deliberately no head-SHA guard.** An earlier draft gated re-review on the head
commit having moved. That is wrong: an author who replies to review threads without pushing
and then re-requests is asking for a real re-review, and a SHA guard would silently ignore
them. Presence in `reviewRequests` is the entire trigger.

**This section is now narrowed to the watch's own ticks — see
[ADR-0025](ADR-0025-an-invocation-reviews-the-request-gates-only-the-watch.md) (ACCEPTED).**
The field answers "does this PR want a review from me, unattended", and this section made it
answer a second question it has no standing on: "does the human who just typed the command
want one now". The first live end-to-end run, on 2026-08-12, hit that — `requested: no` on a
PR the human had explicitly asked to have reviewed, so the tick printed three lines and did
nothing. **As shipped, an explicit invocation now reviews on its own authority, and the field
gates only the ticks a repeat driver fires** (they carry `--on-request`), which take the rows
below exactly as written. The narrowing adds no local record anywhere, so this section's
trigger, dedup, and re-trigger reasoning is unchanged for every unattended tick — it is simply
no longer the answer for a tick a human typed.

### 2. An idempotent tick, driven from outside

The command is a single tick — read state, take exactly one action, exit. It holds no
process, no timer, and no state between runs.

Repetition lives in the harness that already has it: on Claude Code,
`/loop <interval> /pr-review-loop [n] [types]`. On OpenCode there is no loop equivalent, so
the command is run per tick by hand — an accepted asymmetry of the same class as the
Claude-only lint feedback loop in CLAUDE.md's agent sync table, because the tick itself is
identical in both agents and loses nothing when a human presses it.

Owning no repetition does not mean leaving the handoff to the human's memory. When a standing
watch is what was asked for, the command's step 0 starts that driver on itself and exits, and
where the harness has none, the tick report says outright that nothing will run another. The
first draft only mentioned the driver in passing, and a lone tick then read as a watch: it
answered once and went quiet with no sign that nothing was listening.

**The handoff order in the paragraph above is superseded and no longer describes the shipped
command — see
[ADR-0025](ADR-0025-an-invocation-reviews-the-request-gates-only-the-watch.md) §4
(ACCEPTED).** The handoff-then-exit order rested on a premise about the driver that does not
hold: `/loop` is cron-backed, and cron fires at the next match rather than on creation, so
handing off first answers the human with a state read and defers their review by a whole
interval. **As shipped, the tick runs first and the driver is started at the end, only on a
non-terminal explicit tick that did not pass `--once`** — which also retired this section's
other cost, a driver still ticking after the approval it was started for. Under either order
the tick itself holds no timer and no state between runs, and repetition still belongs to the
harness rather than to this command.

Because a tick is idempotent, running it by hand, twice in a row, or after a crash is
always safe. There is no resume path to get wrong. That idempotency is now a property of
**watch** ticks specifically: ADR-0025 gives it up for an explicit tick on purpose, because
typing the command a second time is a request to review a second time. Nothing persists
between runs in either mode, so a tick after a crash is still safe.

### 3. Personal request only

A tick triggers only when the authenticated user's own login is in `reviewRequests`. A
team-level request that does not name them does not trigger.

This is the rule the user already applies by hand: a request addressed to a team is a
request for _someone_ on it, and deciding it is you is a judgment about staffing, not about
code. The loop makes no such judgment. When the user is added individually — by the author
or by themselves — the loop picks it up on the next tick.

## Consequences

### Positive

- **No local state to keep, corrupt, or clean up.** The loop's memory is GitHub's field.
  Killing the session, switching machines, or reviewing from the web UI all leave the
  trigger correct on the next tick, because none of them is a thing the loop is tracking.
- **The loop works with humans, not around them.** If a colleague answers the request
  first, the user leaves `reviewRequests` and the next tick simply waits — no duplicate
  review, no special case for it.
- **A tick is cheap and interruptible.** Most ticks are one state read and a three-line
  answer, so the interval can be short and stopping the loop costs nothing in flight.
- **It runs where the user's setup is.** The reviewer models, credentials, agent configs,
  and checkout are all the ones on the user's machine.

### Negative

- **The loop dies with the session.** `/loop` is a foreground harness feature, so closing
  the session stops the watch. Accepted for v1: restarting is one command, and a tick's
  idempotency means nothing is lost by the gap.
- **A re-request during the review window is not distinguishable from the one being
  answered.** Both are just "the user is in `reviewRequests`". The pre-post state re-check
  narrows this to a small race rather than closing it.
- **OpenCode has no driver.** There, `/pr-review-loop` is a manual per-tick command.
- **Latency is bounded by the interval**, not by the event. A review requested one second
  after a tick waits for the next one.

### Neutral

- Polling makes API calls the user's `gh` token pays for. At one PR and minute-scale
  intervals this is far inside any rate limit, and every call goes through the existing
  `githubcli` wrapper per CLAUDE.md §6.
- Watching more than one PR is a separate feature.
  `gh search prs "user-review-requested:<login>"` works (verified 2026-08-06) but returns a
  months-deep stale backlog, so it needs a baseline policy of its own before it can drive
  anything.

## Alternatives Considered

### Webhooks

Have GitHub push a `review_requested` event to us instead of polling for it.

Rejected on what it requires, not on elegance: a webhook needs a publicly reachable
endpoint and admin rights on every repo whose PRs the user reviews. The user reviewing a PR
frequently has neither — they are a reviewer, not the repo owner. It also puts a server in
the path of a tool whose whole job is to drive local agents, and the event it delivers
would still have to be reconciled against current state before acting, because an event can
arrive after the request has already been answered.

### A GitHub Action

Run the review in CI on the `review_requested` event.

Rejected because the thing being run does not exist there. The review is the user's own
agents, with the user's model configuration and credentials, reading the user's checkout.
A CI runner has none of that, and reproducing it would mean shipping the user's model
credentials into someone else's repo's Actions environment.

### A stateful long-running watcher

A `dg pr watch` process that holds a poll loop, remembers what it reviewed, and re-triggers
on change.

Rejected as more machinery for the same result. It buys nothing the tick does not already
have — GitHub's field already provides trigger, dedup, and re-trigger — and it adds process
management (start, stop, restart, "is it still running", what happens on crash) plus a
second source of truth that can disagree with GitHub. The tick is also hand-runnable and
works identically in both agents, which a daemon would not be.

### Keep a local record of reviewed PRs / head SHAs

Persist "I reviewed PR #213 at SHA abc123" so the loop can tell a new request from an old
one.

Rejected: it is exactly the parallel state CLAUDE.md §6 warns against, and it is strictly
worse than the field it duplicates. It goes stale when a review is submitted from anywhere
else, it needs cleanup nothing owns, and — per the no-SHA-guard point above — its most
obvious use, "only re-review if the code changed", produces the wrong answer for a
re-request that follows a thread discussion instead of a push.

### Trigger on team requests too

Treat a request to a team the user belongs to as a request to the user.

Rejected: it makes the loop claim work that was addressed to a group, which is a staffing
decision the tool has no basis for. The failure mode is also unpleasant — an automated
review posted on a PR nobody asked this particular person to review. Being named
individually is an unambiguous signal, and adding yourself takes one click.
