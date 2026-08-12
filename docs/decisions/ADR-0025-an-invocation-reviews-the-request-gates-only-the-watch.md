# ADR-0025: An invocation reviews now; the request field gates only the watch

**Status:** PROPOSED
**Date:** 2026-08-12
**Deciders:** cjairm
**Related:** [ADR-0022](ADR-0022-pr-review-trigger-is-a-polled-state-read.md) (narrows its §1 trigger rule and §2 handoff order), [ADR-0023](ADR-0023-a-pr-review-targets-immutable-shas.md), [cycle 2026-08-06-pr-review-loop](../plans/cycles/2026-08-06-pr-review-loop.md) (where the defects surfaced), [cycle 2026-08-12-pr-review-explicit-vs-watch](../plans/cycles/2026-08-12-pr-review-explicit-vs-watch.md) (the plan that lands this)

---

## Context

[ADR-0022](ADR-0022-pr-review-trigger-is-a-polled-state-read.md) made GitHub's
`reviewRequests` field the entire trigger for `/pr-review-loop` (§1) and put the repeat
driver's handoff at step 0, before the state read, with the tick exiting straight after it
(§2). Both were decided before the loop had ever been driven end to end by a human.

The cycle's Step 6 (manual end-to-end) ran on 2026-08-12 against a real PR in a work repo.
Two things happened. Each was the file behaving exactly as written, and each was wrong for
the person who had just typed the command:

1. **`/pr-review-loop --reviewer=document <n>` reviewed nothing.** The state read returned
   `pr: open`, `requested: no`, `my-review: none`, which is the table's last row — "wait, the
   ball is with the author". The tick printed those three lines and stopped. The human then
   said "start the review" and the document reviewer ran and posted. They had already said
   it: they typed the command.
2. **Nothing repeated.** A bare invocation is defined as a single look, so the handoff never
   fired and step 11 only _named_ the `/loop` form. Read from the outside, being told the
   form is being told to go start the watch by hand — the opposite of what invoking a command
   called `pr-review-loop` is for.

A third, smaller defect showed up in the same run and belongs here because it is why the
human's own words for "review the docs" reached no reviewer at all: `--reviewer=document` is
not a spelling step 0 parses. The tick takes bare words (`code document skill`); the sibling
`/review-loop` takes `--reviewer code|document|skill`. A human moving between the two types
the sibling's flag, and it silently resolves to nothing.

**What ADR-0022 got right, and this does not touch:** the field is the correct memory for an
_unattended_ watch. It is trigger, dedup, and re-trigger in one read, maintained by GitHub,
with no local record to go stale.

**Where it went wrong:** one signal was made to answer two different questions.

| Question                                                    | Correct answer         |
| ----------------------------------------------------------- | ---------------------- |
| Does this PR want a review from me, with nobody watching?   | GitHub's request field |
| Does the human who just ran this command want a review now? | They ran the command   |

A person typing the command is a request, and a stronger one than the button: it is addressed
to this tool, by the person running it, about the PR they named. Gating it on a GitHub field
makes the tool ask for a permission it already has.

The handoff order rests on a second broken premise. `/loop <interval>` is cron-backed, and a
cron job fires at its **next match**, never on creation. So "hand off, then exit" means the
human who asked for a review gets a three-line state read now and a review one whole interval
later — and the state read reads like the answer.

## Decision

**An explicit invocation reviews. GitHub's request field gates only the ticks a driver
fires.** And the handoff moves to the end of the tick, where it can know whether a watch is
still wanted.

### 1. Two kinds of tick, told apart by a flag and not by inference

A tick is **explicit** (a human typed it) or a **watch tick** (a repeat driver fired it).
Watch ticks carry `--on-request`, which only the handoff writes and no human ever types.

The marker is a flag rather than a judgment on purpose. The current file asserts that "a
driver-fired tick arrives … and carries no request to watch" — an inference about the
surrounding prompt, which an agent cannot check and a guard test cannot pin. A flag on the
command line is a fact both can read.

Rows are evaluated top to bottom and the first match wins, in the order the shipped command's
own table already uses — so every row below states the full state it matches, `requested:`
included. Reading `requested:` off a row is what makes the two request states mutually
exclusive: an author who re-requests a review on a PR this user already approved lands on the
`requested: yes` row and is reviewed, never on the approved row.

| PR state                                       | explicit tick | watch tick |
| ---------------------------------------------- | ------------- | ---------- |
| `merged` / `closed`                            | terminal      | terminal   |
| `draft` (requested or not)                     | **review**    | wait       |
| `open`, `requested: yes`                       | review        | review     |
| `open`, `requested: no`, `my-review:` approved | **review**    | terminal   |
| `open`, `requested: no`, anything else         | **review**    | wait       |

### 2. An explicit tick reviews unless the pull request is over

Only `merged` and `closed` stop it: there is nothing left to review, and a review posted on a
closed PR is noise nobody can act on.

The two rows it now overrides — a draft, and a PR this user has already approved — exist to
protect the _author_ from a review they did not ask to receive. Neither reason survives a
human asking for one deliberately. A draft is exactly when an author wants a private read,
and "review it again" is a normal ask after a rebase or a late doubt.

### 3. The watch keeps ADR-0022 exactly

A `--on-request` tick reads the same three lines and takes the same rows it takes today: a
draft waits, no request waits, a standing approval is terminal. No new state is introduced —
in particular no per-head record of what was already reviewed, which ADR-0022 rejected for
reasons that all still hold.

This is what keeps the loop from reposting: submitting any review removes the user from
`reviewRequests`, so the tick that follows a post waits, and the author's re-request is the
next trigger.

### 4. The handoff happens after the tick, and only when a watch is still wanted

**Only an explicit tick ever starts a driver.** On a non-terminal outcome, and unless `--once`
was passed, an explicit tick starts the driver on itself and reports that it did:

```
/loop <interval> /pr-review-loop <n> [types] [--note <text>] --on-request
```

**A `--on-request` tick never issues a `/loop` command, whatever its outcome.** That is the
marker's second job: the driver that fired the tick is already running, so a handoff here
would start a second driver on every tick and each of those would start another — a watch that
multiplies instead of repeating. A watch tick reports what it did and exits; starting and
stopping the driver stay outside this file, exactly as in ADR-0022 §2.

The interval is the one the human named, or the driver's default. Every argument still
travels verbatim — a driver re-runs the line it was handed, so anything dropped here is
dropped from every tick it ever fires.

Three things fall out of moving it to the end, and the first is the whole point:

- **The review the human asked for happens in the invocation they typed**, not one interval
  later.
- **A first-look approval starts nothing.** The tick is terminal, so there is no driver left
  running for the human to remember to stop — the shape ADR-0022 had, where the driver
  outlives the approval it was started for, disappears.
- **The driver's first tick is harmless whenever it fires.** It is request-gated, and the
  review just posted cleared the request, so an immediate first firing reads `requested: no`
  and waits.

Where the harness has no repeat driver (OpenCode), there is nothing to start: the tick
reviews and the report says plainly that nothing will run another.

### 5. `--once`, and `--reviewer` as a second spelling

`--once` reviews and starts no driver — a single look, which is what a bare invocation used
to be.

`--reviewer <type>` and `--reviewer=<type>` are accepted alongside bare words. This is two
spellings of one vocabulary, not a translation layer: the value is still forwarded to
`devgeta task review-run --reviewer` verbatim, and it is still the runner's own keys, so
`doc` remains an error rather than an alias. The flag spelling is added because it is what
the sibling command takes and therefore what a human types.

### 6. The pre-post gate becomes mode-aware

The re-check before posting keeps both of its jobs — nothing is posted onto a PR that moved
on, and nothing is posted for a head the reviewers never read — but it can no longer require
a review request:

- **explicit tick:** the PR is still neither merged nor closed, and `head` still equals the
  sha the reviewers read.
- **watch tick:** unchanged — the state still lands on the Review row, and `head` is
  unchanged.

Left as it is, this gate is where the whole change would die silently: an explicit tick would
run the full cross-model review, reach a gate demanding `requested: yes`, and post nothing.

### 7. Nothing above is implemented yet, and the plan for it lives in the cycle

This ADR records the decision and its rationale only; the file-by-file plan — the command
file's parser and tables, `docs/spec.md`, the command guard tests, and the revised end-to-end
cases — is **[cycle 2026-08-12-pr-review-explicit-vs-watch](../plans/cycles/2026-08-12-pr-review-explicit-vs-watch.md)**,
which is where implementation plans belong in this repo (CLAUDE.md §10). It is a cycle of its
own rather than a step of
[2026-08-06-pr-review-loop](../plans/cycles/2026-08-06-pr-review-loop.md) because that cycle's
scope is locked and this reverses one of its out-of-scope rows; that cycle references it as
deferred follow-up work.

While this ADR is PROPOSED, the shipped `configs/shared/commands/pr-review-loop.md`, the
`docs/spec.md` narrative, and the guard tests all still describe
[ADR-0022](ADR-0022-pr-review-trigger-is-a-polled-state-read.md)'s behavior: every tick is
request-gated, the handoff sits at step 0, and `--once` / `--on-request` / `--reviewer` are not
spellings the command parses. The split described above therefore exists in no artifact yet,
and landing the decision prose without that cycle would leave explicit reviews failing exactly as
they did on 2026-08-12.

## Consequences

### Positive

- **The command reviews when it is told to.** The failure that motivated this — three lines
  of state, then a human repeating themselves in prose — cannot happen: there is no state a
  PR can be in, short of merged or closed, where an explicit tick declines to review.
- **The watch is opt-out, not opt-in.** Firing the command once both reviews and keeps
  watching, which is what its name promises, and `--once` is there for the single look.
- **Still no local state.** The explicit/watch split is carried by a flag on the command
  line, and the watch's memory is still GitHub's field. Nothing persists between ticks, so a
  tick after a crash is still safe.
- **An approval ends everything.** No driver is started on a terminal tick, and a watch that
  reaches an approval reports that it is over.

### Negative

- **Two tables instead of one.** The tick now has a mode, and prose is the only carrier for
  it. Guard tests over the command file pin the parts that can silently rot: the marker
  appearing on the driver's line, the explicit rows not being request-gated, the
  mode-aware pre-post gate, and a `--on-request` tick starting no driver of its own.
- **An explicit tick is no longer idempotent.** Running it twice reviews twice and posts
  twice. Idempotency was ADR-0022's headline property and it is deliberately given up for
  explicit ticks only: typing the command again is a request to do it again. Watch ticks keep
  it.
- **An explicit tick can post a formal review on a draft.** That is what the draft row
  existed to prevent. Accepted, because the human asked on purpose — and the watch's own
  ticks still let drafts wait, so nothing unattended lands on unfinished work.
- **A watch can be running that the human did not think about.** Any non-terminal explicit
  invocation without `--once` starts one — a watch tick still starts nothing. Bounded by the driver's own limits (a cron loop expires after
  seven days, and it dies with the session) and by the terminal-exit rule, but it is a real
  change from "one invocation does one thing".
- **OpenCode gets the first half only.** It has no repeat driver, so the explicit review
  works identically and the auto-started watch does not exist there — the same asymmetry
  ADR-0022 accepted, in the same place.

### Neutral

- The trigger for an unattended re-review is unchanged, so nothing about the author's side of
  the flow moves: they press re-request, and the next watch tick reviews.
- Ticks stay cheap on the watch side. Most are one state read and a three-line report.

## Alternatives Considered

### Tell the human to request a review from themselves

Keep the gate and document that the way to start a review is to add yourself as a reviewer on
GitHub first.

Rejected. It routes a person already at the keyboard through a detour in a browser to give a
tool permission it was just handed directly, and it does not work at all on a PR whose
reviewer list they cannot edit. It also encodes the exact confusion this ADR is removing:
that the button, not the human, is the authority.

### Infer the caller from the prompt

Keep one table and let the tick decide whether it was human-fired by how the invocation looks
— which is what the file does today when it says a driver-fired tick "carries no request to
watch".

Rejected. It is not checkable at runtime and not pinnable in a test, so it is exactly the
class of convention CLAUDE.md §4 says to replace with something structural. The flag costs
one token on the driver's line.

### Start the driver first and let its first tick do the review

Keep the handoff at step 0 and rely on the driver's first firing to perform the review.

Rejected on how the driver actually works: `/loop <interval>` schedules cron, and cron fires
at the next match, so the human's own invocation would answer with a state read and nothing
else — the failure being fixed here. It also leaves the driver running after an approval,
because a tick that never ran cannot know the watch is already over.

### Review on every tick, request or not

Drop the gate entirely, for driver ticks too.

Rejected: the request field is the dedup. Without it, every tick re-reviews the same PR and
posts again, forever, and the only replacement is the per-head local record ADR-0022 rejected
— stale the moment a review is submitted from anywhere else, and wrong for a re-request that
follows a thread discussion rather than a push.

### Let an explicit tick review only when `my-review: none`

Allow the unrequested review, but stop short of re-reviewing a PR this user already answered.

Rejected as a smaller version of the same mistake. A human asking for a second look after a
rebase, or after changing their mind, is asking for something real, and the answer would be
another three-line refusal — the shape of failure this ADR exists to remove.
